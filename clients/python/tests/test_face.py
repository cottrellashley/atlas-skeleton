"""The face's own behaviour, against the stand-in gateway: deadlines,
streams of any size, truncation, warnings, problems, the engine's life."""

from __future__ import annotations

import _thread
import os
import sys
import threading
import time
import warnings
from decimal import Decimal

import pytest

import fiatlas
from fiatlas import AtlasProblem, FIAtlas, TruncatedWarning
from fiatlas._engine import shared


# PR03, R003, R005, §5.1: an Arrow table far past the pipe's capacity
# streams: the engine names the stream, then sends its batches.
def test_pr03_a_large_arrow_table_streams(face):
    pytest.importorskip("pyarrow")
    started = time.monotonic()
    table = face.fetch("conformance.rows@v1:fetch", {"n": 20000}, deadline=30)
    assert len(table) == 20000 and table.complete and not table.truncated
    assert table.arrow.column("label")[19999].as_py() == "row-19999"
    assert time.monotonic() - started < 10


# R026, PR06, ATLAS-PY-005: a call ends at its deadline against a gateway
# that never answers, deadline_exceeded, and the engine stays usable.
def test_r026_a_call_ends_at_its_deadline(face, standin):
    face.whoami()
    standin.mode = "hold"
    started = time.monotonic()
    with pytest.raises(fiatlas.DeadlineExceeded) as e:
        face.call("conformance.echo@v1:fetch", {"text": "x"}, deadline=2)
    assert 1.9 <= time.monotonic() - started < 3 and e.value.code == "deadline_exceeded"
    standin.mode = ""
    assert face.call("conformance.echo@v1:fetch", {"text": "y"}).record["text"] == "y"


# R026: a NaN in a request is refused before anything is sent, at its
# pointer, and the next call succeeds.
def test_r026_a_nan_is_refused_before_sending(face, standin):
    face.whoami()
    before = list(standin.seen)
    with pytest.raises(fiatlas.InvalidRequest) as e:
        face.call("conformance.echo@v1:fetch", {"text": "x", "tags": [float("nan")]})
    assert e.value.target == "/tags/0" and standin.seen == before
    assert face.call("conformance.echo@v1:fetch", {"text": "y"}).record["text"] == "y"


# R001, STREAM-004: a truncated table is marked incomplete, carries the
# warning, and emits it as a Python warning, as JSON rows and as Arrow.
def test_r001_truncation_is_marked_and_warned(face, standin):
    standin.mode = "truncate"
    with pytest.warns(TruncatedWarning):
        r = face.call("conformance.rows@v1:fetch", {"n": 5})
    assert len(r) == 2 and r.truncated and not r.complete
    assert [w["code"] for w in r.warnings] == ["output_limit_exceeded"] and r.usage == {"rows": 2} and r.request_id
    if "pyarrow" in sys.modules or _has("pyarrow"):
        with pytest.warns(TruncatedWarning):
            t = face.fetch("conformance.rows@v1:fetch", {"n": 5})
        assert len(t) == 2 and t.truncated and not t.complete


# R001, §9.7.4 step 11, CORE-DEPR-001: a deprecation is a DeprecationWarning.
def test_r001_a_deprecation_is_a_deprecation_warning(face, standin):
    standin.mode = "deprecated"
    with pytest.warns(DeprecationWarning, match="2027-01-31"):
        face.call("conformance.echo@v1:fetch", {"text": "x"})


# R090, §9.7.6, CORE-PROB-001: a problem is AtlasProblem, of its code's
# class, every member an attribute, an unknown member kept.
def test_r090_a_problem_is_its_codes_class():
    p = fiatlas.errors.problem({"code": "not_granted", "status": 403, "retryable": False, "detail": "no", "target": "/portfolios/1",
                                "next": "Ask.", "request_id": "r1", "details": [{"target": "/x", "keyword": "enum"}], "from_the_future": 7})
    assert isinstance(p, fiatlas.NotGranted) and isinstance(p, AtlasProblem) and isinstance(p, fiatlas.AtlasError)
    assert (p.code, p.status, p.target, p.next, p.request_id, p.details[0]["keyword"], p.from_the_future) == ("not_granted", 403, "/portfolios/1", "Ask.", "r1", "enum", 7)
    q = fiatlas.errors.problem({"code": "warehouse_suspended", "status": 503, "retryable": True})
    assert type(q) is AtlasProblem and q.status == 503 and q.retryable


# R090, §9.7.1, ATLAS-PY-003: FIAtlas(url, env, token): a workload's token
# is used and the store is not; the attribute form fetches the id it spells.
def test_r090_the_constructor_and_the_attribute_form(bins, standin, tmp_path, monkeypatch):
    monkeypatch.setenv("ATLAS_HOME", str(tmp_path))  # an empty store
    monkeypatch.delenv("ATLAS_URL", raising=False)
    with FIAtlas(url=standin.url, token="ct-valid", engine=bins["engine"]) as a:
        assert a.whoami()["environment"] == "conformance"
        rows = a.conformance.rows(n=3)
        assert [r["i"] for r in rows] == [0, 1, 2] and isinstance(rows[1]["amount"], Decimal)
        assert rows.read_as(dict)["label"] == ["row-0", "row-1", "row-2"]


# R089, §5.1 rule 2: one engine per process, shared by every FIAtlas; a
# broken one is stopped before another starts, so none leaks.
def test_r089_one_engine_per_process(face, bins, standin):
    face.whoami()
    with FIAtlas(standin.url, engine=bins["engine"]) as other:
        other.whoami()
        first = shared(bins["engine"])
        assert first.pid == shared(bins["engine"]).pid
        first._proc.kill()
        first._proc.wait()
        assert face.whoami()["environment"] == "conformance"  # a new engine, connected again
        assert shared(bins["engine"]).pid != first.pid and first._closed


# R089: fetch without pyarrow fails before anything is asked of the engine.
def test_r089_fetch_without_pyarrow_asks_nothing(face, standin, monkeypatch):
    face.whoami()
    before = list(standin.seen)
    monkeypatch.setitem(sys.modules, "pyarrow", None)
    with pytest.raises(ImportError, match="pyarrow"):
        face.fetch("conformance.rows@v1:fetch", {"n": 3})
    assert standin.seen == before and shared(face._path).usable()


# R089: an engine that dies says how, with its exit status.
def test_r089_an_engine_error_says_why(face, standin):
    face.whoami()
    standin.mode = "hold"
    engine = shared(face._path)
    threading.Timer(0.3, engine._proc.kill).start()
    with pytest.raises(fiatlas.EngineError, match="atlas-engine ended"):
        face.call("conformance.echo@v1:fetch", {"text": "x"}, deadline=30)


# ATLAS-PY-005, R063: Ctrl+C ends a call within one second, by cancelling
# it in the engine, which goes on serving.
def test_ctrl_c_ends_a_call_within_a_second(face, standin):
    face.whoami()
    standin.mode = "hold"
    engine = shared(face._path)
    threading.Timer(0.3, _thread.interrupt_main).start()
    started = time.monotonic()
    with pytest.raises(KeyboardInterrupt):
        face.call("conformance.echo@v1:fetch", {"text": "x"}, deadline=60)
    assert time.monotonic() - started < 1.5
    standin.mode = ""
    assert shared(face._path) is engine and face.whoami()["name"] == "Stand-in"


def _has(module: str) -> bool:
    try:
        __import__(module)
        return True
    except ImportError:
        return False


warnings.filterwarnings("default")
os.environ.setdefault("PYTHONWARNINGS", "default")


def _work(args):
    """In a forked worker: a call of its own, on an engine of its own."""
    url, engine = args
    with FIAtlas(url, token="ct-valid", engine=engine) as a:
        return a.whoami()["name"], os.getpid(), shared(engine).pid


# ATLAS-PY-004, R063, §5.1 rule 2: a process forked after its engine
# started gets an engine of its own on its first call, no call hangs, and
# the parent's engine is untouched.
def test_r063_a_fork_pool_gets_engines_of_its_own(face, bins, standin):
    import multiprocessing

    face.whoami()
    parent = shared(bins["engine"]).pid
    with warnings.catch_warnings():
        warnings.simplefilter("ignore", DeprecationWarning)  # fork in a process with threads (the stand-in's)
        with multiprocessing.get_context("fork").Pool(2) as pool:
            got = pool.map_async(_work, [(standin.url, bins["engine"])] * 4).get(timeout=30)
    assert {name for name, _, _ in got} == {"Stand-in"}
    assert all(engine not in (parent,) for _, _, engine in got)
    assert shared(bins["engine"]).pid == parent and face.whoami()["name"] == "Stand-in"


def _wait_exit(pid: int, timeout: float) -> bool:
    """Whether process pid ends within timeout: woken by the kernel when it
    does (kqueue, or a pidfd), never by polling."""
    import select

    if hasattr(select, "kqueue"):
        kq = select.kqueue()
        try:
            ev = select.kevent(pid, filter=select.KQ_FILTER_PROC, flags=select.KQ_EV_ADD | select.KQ_EV_ONESHOT, fflags=select.KQ_NOTE_EXIT)
            return bool(kq.control([ev], 1, timeout))
        except ProcessLookupError:
            return True  # already gone
        finally:
            kq.close()
    fd = os.pidfd_open(pid)
    try:
        return bool(select.select([fd], [], [], timeout)[0])
    finally:
        os.close(fd)


# ATLAS-PY-007, R063: no engine outlives the Python process that started
# it: killed outright with a call in flight, its engine sees its input end,
# cancels the call and exits.
@pytest.mark.skipif(not (hasattr(__import__("select"), "kqueue") or hasattr(os, "pidfd_open")), reason="needs kqueue or a pidfd to wait for a process")
def test_r063_no_engine_outlives_its_python(bins, standin, tmp_path):
    import json as _json
    import signal
    import subprocess

    state = tmp_path / "state"
    state.mkdir(mode=0o700)
    (state / "token-conformance.json").write_text(_json.dumps({"access_token": "ct-valid", "expires_at": int(time.time()) + 3600}))
    (state / "token-conformance.json").chmod(0o600)
    standin.mode = "hold"
    script = ("import os, sys, threading; sys.path.insert(0, sys.argv[1]); from fiatlas import FIAtlas; from fiatlas._engine import shared;"
              "a = FIAtlas(sys.argv[2], engine=sys.argv[3]); a.whoami(); print(shared(sys.argv[3]).pid, flush=True);"
              "threading.Thread(target=a.call, args=('conformance.echo@v1:fetch', {'text': 'x'}), kwargs={'deadline': 120}, daemon=True).start();"
              "sys.stdin.read()")
    child = subprocess.Popen([sys.executable, "-c", script, os.path.dirname(os.path.dirname(fiatlas.__file__)), standin.url, bins["engine"]],
                             stdin=subprocess.PIPE, stdout=subprocess.PIPE, env={**os.environ, "ATLAS_HOME": str(tmp_path)}, text=True)
    engine = int(child.stdout.readline())
    assert standin.holding.wait(10), "the call never reached the gateway"
    child.send_signal(signal.SIGKILL)
    child.wait()
    assert _wait_exit(engine, 5), "the engine outlived its Python process"


def _pipes() -> int:
    """How many pipes this process holds open (an engine holds three)."""
    import stat

    n = 0
    for name in os.listdir("/dev/fd"):
        try:
            n += stat.S_ISFIFO(os.fstat(int(name)).st_mode)
        except OSError:
            pass
    return n


# R089: a broken engine is stopped before another replaces it: ten engines
# killed in turn leave no process and no pipe behind.
@pytest.mark.skipif(not os.path.isdir("/dev/fd"), reason="counts /dev/fd")
def test_r089_broken_engines_leak_nothing(face, bins):
    face.whoami()
    pipes = _pipes()
    pids = []
    for _ in range(10):
        engine = shared(bins["engine"])
        pids.append(engine.pid)
        engine._proc.kill()
        engine._proc.wait()
        assert face.whoami()["name"] == "Stand-in"
    assert len(set(pids)) == 10 and _pipes() == pipes
    for pid in pids:
        with pytest.raises(ChildProcessError):
            os.waitpid(pid, os.WNOHANG)  # every one reaped
