"""The development gateway, the engine and the CLI, built from this
repository, started once for the tests: the Python face meets the real
gateway, DuckDB and all. ATLAS_TEST_GATEWAY, ATLAS_TEST_ENGINE and
ATLAS_TEST_CLI name binaries built elsewhere instead (a gateway built from
another checkout). The face's own tests use a stand-in gateway serving the
references' fixture (standin.py)."""

from __future__ import annotations

import json
import os
import subprocess
import sys
import time
from pathlib import Path

import pytest

REPO = Path(__file__).resolve().parents[3]
sys.path.insert(0, str(Path(__file__).resolve().parent))

from standin import StandIn  # noqa: E402


def _build(tmp: Path, module: str, package: str, name: str, override: str) -> str:
    if os.environ.get(override):
        return os.environ[override]
    out = tmp / name
    subprocess.run(["go", "-C", str(REPO / module), "build", "-o", str(out), package], check=True,
                   env={**os.environ, "GOTOOLCHAIN": "local", "GOPRIVATE": "github.com/jhgonline", "GOVCS": "github.com/jhgonline:off"}, capture_output=True)
    return str(out)


@pytest.fixture(scope="session")
def bins(tmp_path_factory: pytest.TempPathFactory) -> dict[str, str]:
    tmp = tmp_path_factory.mktemp("bin")
    return {
        "gateway": _build(tmp, "gateway/local", "./cmd/gateway-local", "gateway-local", "ATLAS_TEST_GATEWAY"),
        "engine": _build(tmp, "sdk/go", "./cmd/atlas-engine", "atlas-engine", "ATLAS_TEST_ENGINE"),
        "atlas": _build(tmp, "clients/cli", "./cmd/atlas", "atlas", "ATLAS_TEST_CLI"),
    }


@pytest.fixture(scope="session")
def gateway(bins: dict[str, str], tmp_path_factory: pytest.TempPathFactory):
    tmp = tmp_path_factory.mktemp("gateway")
    addr = tmp / "addr"
    proc = subprocess.Popen([bins["gateway"], "-listen", "127.0.0.1:0", "-addr-file", str(addr), "-quiet"],
                            stderr=subprocess.DEVNULL)
    for _ in range(200):
        if addr.exists() and addr.read_text().strip():
            break
        time.sleep(0.05)
    else:
        proc.kill()
        raise RuntimeError("the development gateway did not start")
    yield addr.read_text().strip()
    proc.terminate()
    proc.wait(5)


@pytest.fixture(scope="session")
def sign_in(bins: dict[str, str], gateway: str, tmp_path_factory: pytest.TempPathFactory):
    """A home signed in as a development identity, by the CLI: the face reads
    the token store the CLI writes (ATLAS-CORE §3)."""
    homes: dict[str, str] = {}

    def home(token: str) -> str:
        if token not in homes:
            h = tmp_path_factory.mktemp(token)
            subprocess.run([bins["atlas"], "--gateway", gateway, "login", "--token", token], check=True,
                           env={**os.environ, "ATLAS_HOME": str(h)}, capture_output=True)
            homes[token] = str(h)
        return homes[token]

    return home


@pytest.fixture
def connect(bins: dict[str, str], gateway: str, sign_in, monkeypatch: pytest.MonkeyPatch):
    from fiatlas import FIAtlas

    opened: list[FIAtlas] = []

    def as_(token: str) -> FIAtlas:
        monkeypatch.setenv("ATLAS_HOME", sign_in(token))
        monkeypatch.setenv("ATLAS_URL", gateway)
        a = FIAtlas(engine=bins["engine"])
        opened.append(a)
        return a

    yield as_
    for a in opened:
        a.close()


@pytest.fixture
def standin():
    s = StandIn()
    yield s
    s.close()


@pytest.fixture
def face(bins: dict[str, str], standin: StandIn, tmp_path: Path, monkeypatch: pytest.MonkeyPatch):
    """A FIAtlas on the stand-in, signed in as the conformance suite signs
    in: an unbound token-conformance.json, which a conformance gateway on
    this machine may use (R26)."""
    from fiatlas import FIAtlas

    state = tmp_path / "home" / "state"
    state.mkdir(parents=True, mode=0o700)
    token = state / "token-conformance.json"
    token.write_text(json.dumps({"access_token": "ct-valid", "expires_at": int(time.time()) + 3600, "refresh_token": "rt-valid"}))
    token.chmod(0o600)
    monkeypatch.setenv("ATLAS_HOME", str(tmp_path / "home"))
    monkeypatch.delenv("ATLAS_URL", raising=False)
    a = FIAtlas(standin.url, engine=bins["engine"])
    yield a
    a.close()
