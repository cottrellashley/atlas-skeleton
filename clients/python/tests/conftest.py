"""Hermetic fixtures: the gateway and atlas-engine built from this repository,
the gateway on a free port with synthetic Harbour data behind its fake
Snowflake, and tokens from its development issuer."""

from __future__ import annotations

import json
import os
import subprocess
import urllib.request
from pathlib import Path

import pytest

from fiatlas import FIAtlas

PYTHON = Path(__file__).resolve().parents[1]
ROOT = PYTHON.parents[1]

HOLDINGS = "portfolio.holdings@v1:fetch"
TWO = {"portfolios": ["147HYC", "200IG"]}


@pytest.fixture(scope="session")
def binaries(tmp_path_factory: pytest.TempPathFactory) -> Path:
    out = tmp_path_factory.mktemp("bin")
    for module, pkg, name in (("gateway", "./cmd/gateway", "gateway"), ("sdk/go", "./cmd/atlas-engine", "atlas-engine")):
        subprocess.run(["go", "-C", str(ROOT / module), "build", "-o", str(out / name), pkg], check=True)
    os.environ["ATLAS_ENGINE"] = str(out / "atlas-engine")
    return out


class Gateway:
    def __init__(self, url: str, clients: list[FIAtlas]):
        self.url = url
        self._clients = clients

    def token(self, persona: str = "pm") -> str:
        req = urllib.request.Request(
            self.url + "/.dev/token", data=json.dumps({"persona": persona}).encode(), headers={"Content-Type": "application/json"}
        )
        with urllib.request.urlopen(req) as resp:
            return json.load(resp)["access_token"]

    def atlas(self, persona: str = "pm", **kw) -> FIAtlas:
        a = FIAtlas(self.url, self.token(persona), **kw)
        self._clients.append(a)
        return a


@pytest.fixture
def gateway(binaries: Path):
    """Start a gateway with the given flags; each test may start several."""
    procs: list[subprocess.Popen] = []
    clients: list[FIAtlas] = []

    def start(*args: str) -> Gateway:
        proc = subprocess.Popen([str(binaries / "gateway"), "-addr", "127.0.0.1:0", *args], stdout=subprocess.PIPE, text=True)
        procs.append(proc)
        line = proc.stdout.readline()
        if not line:
            raise RuntimeError(f"the gateway did not start: {proc.wait()}")
        return Gateway(json.loads(line)["gateway"], clients)

    yield start
    for a in clients:
        a.close()
    for proc in procs:
        proc.kill()
        proc.wait()
