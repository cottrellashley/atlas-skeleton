"""The engine process and the pipes to it.

The face and the engine speak the runner protocol (§9.8.3): one JSON command
per line on the engine's standard input, one JSON answer per line on its
standard output. The engine adds two things for the face (§5.1): a table can
come back as an Arrow IPC stream on a second pipe, ended by the final zero-row
batch that carries the end line (§9.6.5); and ``cancel`` stops a command's
work.

Only raw file descriptors are kept, never buffered file objects, so that a
forked child can let go of its parent's pipes without touching them.
"""

from __future__ import annotations

import atexit
import itertools
import json
import os
import shutil
import subprocess
import threading
import time
import weakref
from pathlib import Path
from typing import Any

from .errors import AtlasError, EngineError

#: How long a cancelled call may take to end before its engine is stopped
#: (ATLAS-PY-005: a call ends within one second).
GRACE = 1.0

#: The message metadata key of an Arrow stream's final batch.
META_END = b"atlas:end"

_live: weakref.WeakSet[Engine] = weakref.WeakSet()


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


class Engine:
    """One atlas-engine process, owned by the process that started it. A
    process forked after it starts its own (ATLAS-PY-004)."""

    def __init__(self, path: str):
        in_r, self._in = os.pipe()
        self._out, out_w = os.pipe()
        self._res, res_w = os.pipe()
        try:
            self._proc = subprocess.Popen(
                [path],
                stdin=in_r,
                stdout=out_w,
                pass_fds=(res_w,),
                env={**os.environ, "ATLAS_ENGINE_RESULTS_FD": str(res_w)},
            )
        except OSError as e:
            for fd in (self._in, self._out, self._res):
                os.close(fd)
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

    def usable(self) -> bool:
        return not self._broken and self.owner == os.getpid() and self._proc.poll() is None

    def run(self, op: str, args: dict[str, Any] | None = None, *, timeout: float | None = None, **options: Any) -> dict[str, Any]:
        """Send one command and wait for its answer, and for its stream when
        it names one. Ctrl+C cancels the command and raises KeyboardInterrupt
        once the engine has ended it."""
        with self._lock:
            cid = next(self._ids)
            opts = {k: v for k, v in options.items() if v is not None}
            if timeout is not None:
                opts["timeout"] = timeout
            self._send({"id": cid, "op": op, "args": args or {}, "options": opts})
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
                        raise EngineError("atlas-engine did not end the call at its deadline; it was stopped")
                self._reader.join()  # a call leaves no thread behind, so a fork after it copies none
            except KeyboardInterrupt:
                self._cancel(cid, done)
                raise
            if "error" in box:
                raise box["error"]
            return _checked(box["answer"])

    def _send(self, command: dict[str, Any]) -> None:
        view = memoryview((json.dumps(command, separators=(",", ":")) + "\n").encode())
        try:
            while view:
                view = view[os.write(self._in, view) :]
        except BaseException as e:
            # A command cut short would run into the next one.
            self._broken = True
            if isinstance(e, OSError):
                raise EngineError(f"atlas-engine is gone: {e}") from e
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
                    raise EngineError(f"atlas-engine ended ({self._proc.poll()})")
                answer = json.loads(line)
                if answer.get("id") == cid:
                    break
            result = answer.get("result")
            if answer.get("ok") and isinstance(result, dict) and "stream" in result:
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

    def stop(self) -> None:
        """End the engine: close its input, and kill it if it does not end at once."""
        if self._closed or self.owner != os.getpid():
            return  # stopped already, or a fork's copy of its parent's engine
        self._broken = self._closed = True
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

    def _forget(self) -> None:
        """In a forked child: let go of the parent's pipes without writing to
        them, so the parent's engine is untouched (§5.1, rule 2)."""
        if self._closed:
            return
        self._broken = self._closed = True
        for fd in (self._in, self._out, self._res):
            _close(fd)


def _close(fd: int) -> None:
    try:
        os.close(fd)
    except OSError:
        pass


def _checked(answer: dict[str, Any]) -> dict[str, Any]:
    """Raise the problem an answer carries, in its line or in its stream's end line."""
    if not answer.get("ok"):
        if answer.get("cancelled"):
            raise EngineError("the engine reports a cancellation the face did not ask for")
        raise AtlasError(answer.get("problem") or {})
    if "end" in answer:
        end = answer["end"]
        if end is None:
            raise EngineError("the engine's stream ended without its final batch")
        if end.get("problem"):
            raise AtlasError(end["problem"])
        if end.get("rows") != answer["table"].num_rows:
            raise EngineError(f"the engine sent {answer['table'].num_rows} rows; its end line counts {end.get('rows')}")
    return answer


def _after_fork_in_child() -> None:
    for e in list(_live):
        e._forget()


def _at_exit() -> None:
    for e in list(_live):
        e.stop()


os.register_at_fork(after_in_child=_after_fork_in_child)
atexit.register(_at_exit)
