"""The engine process and the pipes to it.

The face and the engine speak the runner protocol (§9.8.3): one JSON command
per line on the engine's standard input, one JSON answer per line on its
standard output. The engine adds two things for the face (§5.1): a table can
come back as an Arrow IPC stream on a second pipe, the answer naming the
stream first and its batches after, ended by the final zero-row batch that
carries the end line (§9.6.5); and ``cancel`` stops a command's work.

There is one engine per process (§5.1 rule 2), shared by every FIAtlas in
it, each on a connection of its own; a process forked after it starts its
own on its first call and never writes to its parent's pipes. A broken
engine is stopped before another replaces it, so no process or pipe leaks.

Only raw file descriptors are kept, never buffered file objects, so that a
forked child can let go of its parent's pipes without touching them.
"""

from __future__ import annotations

import atexit
import itertools
import json
import math
import os
import shutil
import subprocess
import tempfile
import threading
import time
import weakref
from pathlib import Path
from typing import Any

from .errors import AtlasProblem, EngineError, problem

#: How long a cancelled call may take to end before its engine is stopped
#: (ATLAS-PY-005: a call ends within one second).
GRACE = 1.0

#: The message metadata key of an Arrow stream's final batch.
META_END = b"atlas:end"

#: How much of the engine's standard error an EngineError quotes.
STDERR_TAIL = 4096

_live: weakref.WeakSet[Engine] = weakref.WeakSet()
_shared: dict[str, Engine] = {}
_shared_lock = threading.Lock()


def engine_path(explicit: str | None = None) -> str:
    """The atlas-engine to run: the one named, ATLAS_ENGINE, the one the
    wheel ships, or one on the PATH."""
    for candidate in (explicit, os.environ.get("ATLAS_ENGINE")):
        if candidate:
            return candidate
    name = "atlas-engine.exe" if os.name == "nt" else "atlas-engine"
    shipped = Path(__file__).with_name("bin") / name
    if shipped.exists():
        return str(shipped)
    if found := shutil.which(name):
        return found
    raise EngineError("atlas-engine was not found: set ATLAS_ENGINE, or install the wheel that ships it")


def shared(path: str) -> Engine:
    """This process's engine at path, started on first use, and started
    again when the one it had is broken, which is stopped first."""
    with _shared_lock:
        e = _shared.get(path)
        if e is None or not e.usable():
            if e is not None:
                e.stop()
            e = _shared[path] = Engine(path)
        return e


class _Results:
    """The results pipe as pyarrow reads it: ``read(n)`` returns n bytes,
    fewer only where the pipe ends."""

    closed = False

    def __init__(self, fd: int):
        self._fd = fd

    def readable(self) -> bool:
        return True

    def seekable(self) -> bool:
        return False

    def read(self, n: int = -1) -> bytes:
        out = bytearray()
        while n < 0 or len(out) < n:
            chunk = os.read(self._fd, 1 << 20 if n < 0 else min(n - len(out), 1 << 20))
            if not chunk:
                break
            out += chunk
        return bytes(out)


def _pointer(value: Any, path: str = "") -> str | None:
    """The JSON Pointer of the first number JSON cannot carry (a NaN, an
    infinity), or None."""
    if isinstance(value, float) and not math.isfinite(value):
        return path
    if isinstance(value, dict):
        for k, v in value.items():
            found = _pointer(v, f"{path}/{str(k).replace('~', '~0').replace('/', '~1')}")
            if found is not None:
                return found
    if isinstance(value, (list, tuple)):
        for i, v in enumerate(value):
            found = _pointer(v, f"{path}/{i}")
            if found is not None:
                return found
    return None


class Engine:
    """One atlas-engine process, owned by the process that started it. A
    process forked after it starts its own (ATLAS-PY-004)."""

    def __init__(self, path: str):
        in_r, self._in = os.pipe()
        self._out, out_w = os.pipe()
        self._res, res_w = os.pipe()
        # Appending, so reading its tail never moves where the engine writes.
        self._stderr = tempfile.TemporaryFile(mode="a+b")
        env = {**os.environ, "ATLAS_ENGINE_RESULTS_FD": str(res_w)}
        extra: dict[str, Any] = {"pass_fds": (res_w,)}
        if os.name == "nt":  # pragma: no cover - Windows passes a handle, not a descriptor
            import msvcrt

            handle = msvcrt.get_osfhandle(res_w)
            os.set_handle_inheritable(handle, True)
            env["ATLAS_ENGINE_RESULTS_FD"] = str(handle)
            extra = {"startupinfo": subprocess.STARTUPINFO(lpAttributeList={"handle_list": [handle]}), "close_fds": True}
        try:
            self._proc = subprocess.Popen([path], stdin=in_r, stdout=out_w, stderr=self._stderr, env=env, **extra)
        except OSError as e:
            for fd in (self._in, self._out, self._res):
                os.close(fd)
            self._stderr.close()
            raise EngineError(f"atlas-engine could not be started: {e}") from e
        finally:
            for fd in (in_r, out_w, res_w):
                os.close(fd)
        self.owner = os.getpid()
        self.pid = self._proc.pid
        self._buf = bytearray()
        self._ids = itertools.count(1)
        self._lock = threading.Lock()
        self._reader: threading.Thread | None = None
        self._broken = False
        self._closed = False
        _live.add(self)
        self._finalizer = weakref.finalize(self, Engine._finalize, self._proc, self._in, self._out, self._res, self._stderr, self.owner)

    def usable(self) -> bool:
        return not self._broken and self.owner == os.getpid() and self._proc.poll() is None

    def run(self, op: str, args: dict[str, Any] | None = None, *, timeout: float | None = None, results: str | None = None) -> dict[str, Any]:
        """Send one command and wait for its answer, and for its stream when
        it names one. timeout is the call's deadline: past it and GRACE the
        engine is stopped. Ctrl+C cancels the command and raises
        KeyboardInterrupt once the engine has ended it."""
        with self._lock:
            if not self.usable():
                raise self._gone("atlas-engine is not running")
            cid = next(self._ids)
            command: dict[str, Any] = {"id": cid, "op": op, "args": args or {}}
            if results:
                command["results"] = results
            self._send(command)
            box: dict[str, Any] = {}
            done = threading.Event()
            self._reader = threading.Thread(target=self._receive, args=(cid, box, done), daemon=True)
            self._reader.start()
            limit = None if timeout is None else time.monotonic() + timeout + GRACE
            try:
                # Wait in short steps, so Ctrl+C is seen whichever thread the
                # signal lands on.
                while not done.wait(0.05):
                    if limit is not None and time.monotonic() > limit:
                        self.stop()
                        raise problem({"code": "deadline_exceeded", "detail": "atlas-engine did not end the call at its deadline, so it was stopped"})
                self._reader.join()  # a call leaves no thread behind, so a fork after it copies none
            except KeyboardInterrupt:
                self._cancel(cid, done)
                raise
            if "error" in box:
                raise box["error"]
            return _checked(box["answer"])

    def _send(self, command: dict[str, Any]) -> None:
        try:
            text = json.dumps(command, separators=(",", ":"), allow_nan=False)
        except (TypeError, ValueError) as e:
            # Refused before anything is sent (R026): JSON has no NaN.
            where = _pointer((command.get("args") or {}).get("request"))
            raise problem({"code": "invalid_request", "detail": f"the request is not JSON: {e}", "target": where if where is not None else ""}) from e
        view = memoryview((text + "\n").encode())
        try:
            while view:
                view = view[os.write(self._in, view) :]
        except BaseException as e:
            # A command cut short would run into the next one.
            self._broken = True
            if isinstance(e, OSError):
                raise self._gone(f"atlas-engine is gone: {e}") from e
            raise

    def _cancel(self, cid: int, done: threading.Event) -> None:
        """Cancel command cid and let the engine end it; stop the engine if
        it has not within GRACE."""
        try:
            self._send({"id": cid, "op": "cancel"})
            done.wait(GRACE)
        finally:
            if not done.is_set():
                self.stop()

    def _receive(self, cid: int, box: dict[str, Any], done: threading.Event) -> None:
        try:
            while True:
                line = self._readline()
                if not line:
                    raise self._gone("atlas-engine ended")
                answer = json.loads(line)
                # An answer to id 0 is to a line the engine could not read:
                # this one, the only command waiting.
                if answer.get("id") in (cid, 0):
                    break
            value = answer.get("value")
            if answer.get("ok") and isinstance(value, dict) and value.get("stream") == "arrow":
                answer["table"], answer["end"] = self._read_stream()
            box["answer"] = answer
        except BaseException as e:
            self._broken = True  # the pipes are no longer in step with the engine
            box["error"] = e
        finally:
            done.set()

    def _readline(self) -> bytes:
        while True:
            i = self._buf.find(b"\n")
            if i >= 0:
                line = bytes(self._buf[: i + 1])
                del self._buf[: i + 1]
                return line
            chunk = os.read(self._out, 1 << 16)
            if not chunk:
                return b""
            self._buf += chunk

    def _read_stream(self) -> tuple[Any, dict[str, Any] | None]:
        """Read one table from the results pipe. The end line is None when
        the stream ended without its final batch."""
        import pyarrow as pa

        reader = pa.ipc.open_stream(pa.PythonFile(_Results(self._res), mode="r"))
        batches, end = [], None
        while True:
            try:
                batch, meta = reader.read_next_batch_with_custom_metadata()
            except StopIteration:
                break
            if meta is not None and META_END in meta:
                end = json.loads(meta[META_END])
            elif end is None:
                batches.append(batch)
            else:
                raise EngineError("a batch followed the final batch")
        return pa.Table.from_batches(batches, schema=reader.schema), end

    def _gone(self, why: str) -> EngineError:
        """An EngineError saying why, with the engine's exit status and the
        end of what it wrote on standard error."""
        status = self._proc.poll()
        tail = ""
        try:
            self._stderr.seek(0, os.SEEK_END)
            size = self._stderr.tell()
            self._stderr.seek(max(0, size - STDERR_TAIL))
            tail = self._stderr.read().decode(errors="replace").strip()
        except (OSError, ValueError):
            pass
        parts = [why]
        if status is not None:
            parts.append(f"exit status {status}")
        if tail:
            parts.append(f"its last words: {tail}")
        return EngineError("; ".join(parts))

    def stop(self) -> None:
        """End the engine: close its input, and kill it if it does not end at once."""
        if self._closed or self.owner != os.getpid():
            return  # stopped already, or a fork's copy of its parent's engine
        self._broken = self._closed = True
        self._finalizer.detach()  # its descriptors are closed here, once
        _close(self._in)
        try:
            self._proc.wait(timeout=GRACE)
        except subprocess.TimeoutExpired:
            self._proc.kill()
            self._proc.wait()
        if self._reader is not None and self._reader is not threading.current_thread():
            self._reader.join(GRACE)  # it sees the pipes end; then they can close
        _close(self._out)
        _close(self._res)
        self._stderr.close()

    def _forget(self) -> None:
        """In a forked child: let go of the parent's pipes without writing to
        them, so the parent's engine is untouched (§5.1, rule 2)."""
        if self._closed:
            return
        self._broken = self._closed = True
        self._finalizer.detach()
        for fd in (self._in, self._out, self._res):
            _close(fd)

    @staticmethod
    def _finalize(proc: subprocess.Popen, fd_in: int, fd_out: int, fd_res: int, stderr: Any, owner: int) -> None:
        """An engine no one holds is stopped and its pipes closed (R089)."""
        if owner != os.getpid():
            return
        _close(fd_in)
        if proc.poll() is None:
            try:
                proc.wait(timeout=GRACE)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait()
        _close(fd_out)
        _close(fd_res)
        try:
            stderr.close()
        except OSError:
            pass


def _close(fd: int) -> None:
    try:
        os.close(fd)
    except OSError:
        pass


def _checked(answer: dict[str, Any]) -> dict[str, Any]:
    """Raise the problem an answer carries, in its line or in its stream's
    end line; a truncation is a warning, never a failure (STREAM-004)."""
    if not answer.get("ok"):
        if answer.get("cancelled"):
            raise EngineError("the engine reports a cancellation the face did not ask for")
        raise problem(answer.get("problem"))
    if "end" in answer:
        end = answer["end"]
        if end is None:
            raise EngineError("the engine's stream ended without its final batch")
        trouble = end.get("problem")
        if trouble and trouble.get("code") != "output_limit_exceeded":
            raise problem(trouble)
        if end.get("rows") != answer["table"].num_rows:
            raise EngineError(f"the engine sent {answer['table'].num_rows} rows; its end line counts {end.get('rows')}")
        answer["truncated"] = bool(end.get("truncated"))
        answer["usage"] = end.get("usage")
        if answer["truncated"]:
            text = (trouble or {}).get("detail") or "the gateway cut the table at the caller's limit"
            answer.setdefault("warnings", []).append({"code": "output_limit_exceeded", "text": text})
    return answer


def _after_fork_in_child() -> None:
    for e in list(_live):
        e._forget()
    _shared.clear()


def _at_exit() -> None:
    for e in list(_live):
        e.stop()


if hasattr(os, "register_at_fork"):  # not on Windows, which has no fork
    os.register_at_fork(after_in_child=_after_fork_in_child)
atexit.register(_at_exit)

__all__ = ["Engine", "engine_path", "shared", "GRACE", "AtlasProblem"]
