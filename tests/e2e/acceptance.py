"""The acceptance result (INTEGRATIONS; docs/pass-2/REVIEW.md), proved
across the three clients against one development gateway.

  1. Both pinned repositories take part in the running path (the kit serves
     and verifies; sqlglot compiles, guards and transpiles every read).
  2. The same person calls the same capability from the CLI, Python and
     Node with identical request records and gets identical results.
  3. A second person with narrower scope gets a narrower result, or a clear
     refusal, the same in every client.
  4. A capability added after the clients were built works in all three.
  5. Invalid requests, refusals and exact decimals behave the same in every
     client.
  6. The guide's example succeeds, and its result checks against the
     source fixture independently.

Run by `make e2e`; it builds what it runs.
"""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import sys
import tempfile
import time
from decimal import Decimal
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(REPO / "clients" / "python"))
from fiatlas import AtlasError, FIAtlas  # noqa: E402

ENV = {**os.environ, "GOTOOLCHAIN": "local", "GOPRIVATE": "atlas.local", "GOVCS": "atlas.local:off"}


def build(tmp: Path, module: str, pkg: str, name: str) -> str:
    out = tmp / name
    subprocess.run(["go", "-C", str(REPO / module), "build", "-o", str(out), pkg], check=True, env=ENV, capture_output=True)
    return str(out)


def canon(v) -> str:
    return json.dumps(v, sort_keys=True, separators=(",", ":"), ensure_ascii=False, default=str)


def main() -> int:
    tmp = Path(tempfile.mkdtemp(prefix="atlas-acceptance-"))
    bins = {n: build(tmp, m, p, n) for n, m, p in [
        ("gateway-local", "gateway/local", "./cmd/gateway-local"),
        ("atlas", "clients/cli", "./cmd/atlas"),
        ("atlas-engine", "sdk/go", "./cmd/atlas-engine")]}
    catalog = tmp / "catalog"
    (catalog / "profiles").mkdir(parents=True)
    (catalog / "views").mkdir()
    addr = tmp / "addr"
    gw = subprocess.Popen([bins["gateway-local"], "-listen", "127.0.0.1:0", "-addr-file", str(addr), "-quiet",
                           "-exclude", "portfolio.holdings-dates@v1", "-catalog", str(catalog)], stderr=subprocess.DEVNULL)
    try:
        for _ in range(200):
            if addr.exists() and addr.read_text().strip():
                break
            time.sleep(0.05)
        url = addr.read_text().strip()
        homes = {}
        for who in ("dev-alice", "dev-bob"):
            h = tmp / who
            subprocess.run([bins["atlas"], "--gateway", url, "login", "--token", who], check=True, env={**ENV, "ATLAS_HOME": str(h)}, capture_output=True)
            homes[who] = str(h)

        def cli(who: str, address: str, request: dict):
            r = subprocess.run([bins["atlas"], "--gateway", url, "call", address, "--request", json.dumps(request)],
                               env={**ENV, "ATLAS_HOME": homes[who]}, capture_output=True, text=True)
            if r.returncode:
                return ("problem", json.loads(r.stderr.strip().splitlines()[-1]), r.returncode)
            # A table's NDJSON ends with its end line (§5.3 rule 2), the completion marker, not a row.
            return ("rows", [canon(json.loads(l)) for l in r.stdout.splitlines() if not l.startswith('{"$atlas"')], 0)

        def node(who: str, address: str, request: dict):
            r = subprocess.run(["node", str(REPO / "clients" / "node" / "bin" / "call.ts"), address, json.dumps(request)],
                               env={**ENV, "ATLAS_HOME": homes[who], "ATLAS_URL": url}, capture_output=True, text=True)
            if r.returncode:
                return ("problem", json.loads(r.stderr.strip().splitlines()[-1]), None)
            return ("rows", [canon(json.loads(l)) for l in r.stdout.splitlines()], 0)

        def py(who: str, address: str, request: dict):
            os.environ["ATLAS_HOME"], os.environ["ATLAS_URL"] = homes[who], url
            with FIAtlas(engine=bins["atlas-engine"]) as a:
                try:
                    return ("rows", [canon(r) for r in a.call(address, request)], 0)
                except AtlasError as e:
                    return ("problem", e.problem, None)

        def same(name: str, who: str, address: str, request: dict, *, rows: int | None = None,
                 problem: tuple[str, str | None, int] | None = None):
            """Calls address in all three clients and checks the outcome each one
            states: that many rows, identical in every client, or that problem
            (code, target, CLI exit), the same in every client. Agreement alone
            proves nothing: three clients can agree on a failure."""
            assert (rows is None) != (problem is None), name
            got = {"cli": cli(who, address, request), "python": py(who, address, request), "node": node(who, address, request)}
            kinds = {k: v[0] for k, v in got.items()}
            want = "rows" if rows is not None else "problem"
            assert set(kinds.values()) == {want}, f"{name}: expected {want} in every client, got {kinds}: {got}"
            if want == "rows":
                assert got["cli"][1] == got["python"][1] == got["node"][1], f"{name}: the clients disagree"
                assert len(got["cli"][1]) == rows, f"{name}: {len(got['cli'][1])} rows, expected {rows}"
                print(f"ok  {name}: {rows} identical rows in the CLI, Python and Node")
                return got["cli"][1]
            codes = {k: (v[1].get("code"), v[1].get("target")) for k, v in got.items()}
            assert set(codes.values()) == {problem[:2]}, f"{name}: expected {problem[:2]} in every client, got {codes}"
            assert got["cli"][2] == problem[2], f"{name}: CLI exit {got['cli'][2]}, expected {problem[2]}"
            print(f"ok  {name}: {problem[0]} at {problem[1]!r} in all three; CLI exit {problem[2]}")
            return codes["cli"]

        holdings = {"portfolios": ["147HYC", "GLOBAL-CREDIT"], "columns": ["portfolio", "security_id", "as_of", "quantity", "market_value", "weight"]}
        rows = same("2. the same request record, three clients", "dev-alice", "portfolio.holdings@v1:fetch", holdings, rows=8)
        fixture = json.loads((REPO / "gateway" / "fixtures" / "harbour.json").read_text())
        table = next(t for t in fixture["tables"] if t["name"] == "HARBOUR.HOLDINGS.POSITIONS_V")
        cols = [c[0] for c in table["columns"]]
        source = [dict(zip(cols, r)) for r in table["rows"]]
        latest = {p: max(r["AS_OF_DATE"] for r in source if r["PORTFOLIO_CODE"] == p) for p in holdings["portfolios"]}
        want = sorted((r for r in source if r["PORTFOLIO_CODE"] in latest and r["AS_OF_DATE"] == latest[r["PORTFOLIO_CODE"]]), key=lambda r: (r["PORTFOLIO_CODE"], r["SECURITY_ID"]))
        for line, w in zip(rows, want):
            r = json.loads(line)
            assert (r["portfolio"], r["security_id"], r["as_of"]) == (w["PORTFOLIO_CODE"], w["SECURITY_ID"], w["AS_OF_DATE"])
            assert Decimal(r["market_value"]["amount"]) == Decimal(w["MARKET_VALUE"]) and Decimal(r["quantity"]) == Decimal(w["QUANTITY"])
        assert len(rows) == len(want)
        print(f"ok  6. the result checks against the fixture, read independently: {len(want)} rows, decimals exact")

        same("3. a narrower scope is refused, not narrowed silently", "dev-bob", "portfolio.holdings@v1:fetch", {"portfolios": ["GLOBAL-CREDIT"]},
             problem=("not_granted", "/portfolios/0", 4))
        bob = same("3. a narrower scope gives a narrower list", "dev-bob", "portfolio.list@v1:fetch", {}, rows=1)
        assert [json.loads(l)["code"] for l in bob] == ["147HYC"]
        same("5. an invalid request is the same problem everywhere", "dev-alice", "portfolio.holdings@v1:fetch", {"portfolios": [], "as_of": "yesterday"},
             problem=("invalid_request", "/as_of", 2))
        same("5. a guide example succeeds", "dev-alice", "portfolio.holdings@v1:fetch", {"portfolios": ["147HYC"], "as_of": "2026-10-28", "columns": ["portfolio", "isin", "as_of", "weight"]},
             rows=3)

        dates = {"portfolios": ["147HYC"], "from": "2026-10-29"}
        before = cli("dev-alice", "portfolio.holdings-dates@v1:fetch", dates)
        assert before[0] == "problem" and before[1]["code"] == "not_found", before
        for kind in ("profiles", "views"):
            shutil.copy(REPO / "gateway" / "svc" / "portfolio" / kind / "portfolio.holdings-dates@v1.json", catalog / kind)
        # The profiles it replaced travel with it: the registry holds a new
        # profile to the gate against its version's history (R03, PR05).
        shutil.copytree(REPO / "gateway" / "svc" / "portfolio" / "history" / "portfolio.holdings-dates@v1",
                        catalog / "history" / "portfolio.holdings-dates@v1")
        for _ in range(100):
            if cli("dev-alice", "portfolio.holdings-dates@v1:fetch", dates)[0] == "rows":
                break
            time.sleep(0.1)
        same("4. a capability added after the clients were built", "dev-alice", "portfolio.holdings-dates@v1:fetch", dates, rows=2)

        # 1. sqlglot in the running path: the plan fetch would run is the
        # Snowflake statement sqlglot compiled and guarded, every column
        # aliased, the request's values bound, and the tag out of band.
        kind, plan, _ = cli("dev-alice", "portfolio.holdings@v1:explain", {"portfolios": ["147HYC"]})
        assert kind == "rows" and len(plan) == 1, (kind, plan)
        plan = json.loads(plan[0])["plan"]
        assert plan["language"] == "sql" and plan["dialect"] == "snowflake", plan
        assert plan["statement"].startswith("SELECT ") and "147HYC" not in plan["statement"], plan["statement"]
        assert plan.get("query_tag", "").startswith("atlas:portfolio.holdings@v1:") and plan["query_tag"] not in plan["statement"], plan
        print("ok  1. sqlglot in the running path: explain shows the Snowflake statement it compiled and guarded, values bound and the tag out of band")
        return 0
    finally:
        gw.terminate()
        gw.wait(5)
        shutil.rmtree(tmp, ignore_errors=True)


if __name__ == "__main__":
    sys.exit(main())
