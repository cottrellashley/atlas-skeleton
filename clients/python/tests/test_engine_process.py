"""The face and its engine as processes: Ctrl+C, deadlines, forks and exits
(ATLAS-PY-004, ATLAS-PY-005, ATLAS-PY-007)."""

from __future__ import annotations

import json
import multiprocessing
import os
import signal
import subprocess
import sys
import textwrap
import threading
import time

import pytest

from conftest import HOLDINGS, PYTHON, TWO
from fiatlas import AtlasError

# Storage far away: every chunk download waits 300ms, and there are many.
SLOW = ("-chunk-rows", "2", "-chunk-latency", "300ms")


def interrupt_after(seconds: float) -> dict[str, float]:
    """Ctrl+C for the main thread, after seconds."""
    main, sent = threading.main_thread().ident, {}

    def fire():
        sent["at"] = time.monotonic()
        signal.pthread_kill(main, signal.SIGINT)

    threading.Timer(seconds, fire).start()
    return sent


@pytest.mark.parametrize(
    "via, after",
    [("gateway", 0.1), ("gateway", 0.8), ("handoff", 0.8)],
    ids=["before-the-answer", "mid-stream", "mid-download"],
)
def test_ctrl_c_ends_a_call_within_a_second_and_the_engine_carries_on(gateway, via, after):
    atlas = gateway(*SLOW).atlas()
    atlas.whoami()
    engine = atlas._engine.pid
    sent = interrupt_after(after)
    with pytest.raises(KeyboardInterrupt):
        atlas.fetch(HOLDINGS, TWO, execution=via)
    assert time.monotonic() - sent["at"] < 1.0

    # Cancelled, not killed: the same engine answers at once, so the work
    # of the cancelled call is over (commands run one at a time).
    start = time.monotonic()
    atlas.whoami()
    assert time.monotonic() - start < 0.5
    assert atlas._engine.pid == engine
    assert len(atlas.fetch(HOLDINGS, {"portfolios": ["147HYC"]}, execution=via)) > 0


@pytest.mark.parametrize("via", ["gateway", "handoff"])
def test_a_deadline_ends_a_call_with_deadline_exceeded(gateway, via):
    atlas = gateway(*SLOW).atlas()
    atlas.whoami()
    start = time.monotonic()
    with pytest.raises(AtlasError) as e:
        atlas.fetch(HOLDINGS, TWO, execution=via, timeout=0.5)
    assert e.value.code == "deadline_exceeded"
    assert time.monotonic() - start < 1.5
    assert atlas.whoami()["principal"]


@pytest.mark.skipif(not hasattr(os, "fork"), reason="no fork here")
def test_a_forked_child_starts_its_own_engine_and_leaves_its_parents_alone(gateway):
    atlas = gateway().atlas()
    want = len(atlas.fetch(HOLDINGS, TWO))
    parent = atlas._engine.pid
    r, w = os.pipe()
    child = os.fork()
    if child == 0:
        try:
            os.write(w, json.dumps({"rows": len(atlas.fetch(HOLDINGS, TWO)), "engine": atlas._engine.pid}).encode())
        except BaseException as e:
            os.write(w, json.dumps({"error": repr(e)}).encode())
        finally:
            os._exit(0)
    os.close(w)
    got = json.loads(os.read(r, 1 << 16))
    os.close(r)
    os.waitpid(child, 0)
    assert got.get("rows") == want, got
    assert got["engine"] != parent
    assert atlas._engine.pid == parent and len(atlas.fetch(HOLDINGS, TWO)) == want


shared = None  # a client made before the pool forks, as gunicorn --preload or Celery's prefork pool does


def count_in_worker(_):
    return len(shared.fetch(HOLDINGS, TWO)), shared._engine.pid, os.getpid()


@pytest.mark.skipif(not hasattr(os, "fork"), reason="no fork here")
def test_each_process_of_a_fork_pool_gets_an_engine_of_its_own(gateway):
    global shared
    shared = gateway().atlas()
    want = len(shared.fetch(HOLDINGS, TWO))
    with multiprocessing.get_context("fork").Pool(2) as pool:
        results = pool.map(count_in_worker, range(6), chunksize=1)
    assert all(n == want for n, _, _ in results)
    engines = {engine for _, engine, _ in results}
    assert shared._engine.pid not in engines
    assert len(engines) == len({worker for _, _, worker in results})  # one engine per worker


@pytest.mark.parametrize("busy", [False, True], ids=["idle", "mid-call"])
def test_no_engine_outlives_the_python_process_that_started_it(gateway, busy):
    g = gateway(*SLOW)
    script = textwrap.dedent(
        f"""
        import os, signal, threading, time
        from fiatlas import FIAtlas
        atlas = FIAtlas({g.url!r}, {g.token()!r})
        atlas.whoami()
        if {busy!r}:
            threading.Thread(target=atlas.fetch, args=({HOLDINGS!r}, {TWO!r}), daemon=True).start()
            time.sleep(0.5)
        print(atlas._engine.pid, flush=True)
        os.kill(os.getpid(), signal.SIGKILL)
        """
    )
    env = {**os.environ, "PYTHONPATH": str(PYTHON)}
    out = subprocess.run([sys.executable, "-c", script], stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True, env=env, timeout=30)
    engine = int(out.stdout.split()[0])
    gone_by = time.monotonic() + 2.0
    while time.monotonic() < gone_by:
        try:
            os.kill(engine, 0)
        except ProcessLookupError:
            return
        time.sleep(0.05)
    pytest.fail(f"atlas-engine {engine} outlived the Python process that started it")
