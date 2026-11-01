"""The journey through the Python face (ATLAS-CORE §12), its results
checked against the fixture the gateway serves, read independently."""

from __future__ import annotations

import json
import os
from collections import defaultdict
from decimal import Decimal
from pathlib import Path

import pytest

from fiatlas import AtlasError

# The fixture the gateway serves: ATLAS_TEST_FIXTURE when the gateway was
# built from another checkout (conftest.py's ATLAS_TEST_GATEWAY).
FIXTURE = Path(os.environ.get("ATLAS_TEST_FIXTURE") or Path(__file__).resolve().parents[3] / "gateway" / "fixtures" / "harbour.json")


def expected_holdings(portfolios: list[str]) -> list[dict]:
    """What portfolio.holdings@v1 means, computed from the fixture itself:
    each portfolio's latest valuation, rows by portfolio and security."""
    doc = json.loads(FIXTURE.read_text())
    table = next(t for t in doc["tables"] if t["name"] == "HARBOUR.HOLDINGS.POSITIONS_V")
    cols = [c[0] for c in table["columns"]]
    rows = [dict(zip(cols, r)) for r in table["rows"] if r[0] in portfolios]
    latest: dict[str, str] = defaultdict(str)
    for r in rows:
        latest[r["PORTFOLIO_CODE"]] = max(latest[r["PORTFOLIO_CODE"]], r["AS_OF_DATE"])
    kept = [r for r in rows if r["AS_OF_DATE"] == latest[r["PORTFOLIO_CODE"]]]
    return sorted(kept, key=lambda r: (r["PORTFOLIO_CODE"], r["SECURITY_ID"]))


def test_orient_and_find(connect):
    alice = connect("dev-alice")
    who = alice.whoami()
    assert who["name"] == "Alice Analyst" and who["kind"] == "person"
    assert who["available_domains"] == ["data", "portfolio"]
    ids = [c["id"] for c in alice.catalog()]
    assert ids == ["data.status", "portfolio.holdings", "portfolio.holdings-dates", "portfolio.list", "portfolio.search"]
    assert alice.search("find a portfolio by name")[0]["id"] == "portfolio.search"
    guide = alice.guide("portfolio.holdings")
    assert 'atlas.call("portfolio.holdings@v1:fetch", {"portfolios": ["147HYC"]})' in guide


def test_holdings_exact_and_independently_checked(connect):
    alice = connect("dev-alice")
    result = alice.call("portfolio.holdings@v1:fetch", {"portfolios": ["147HYC", "GLOBAL-CREDIT"]})
    assert result.table and len(result) == 8
    want = expected_holdings(["147HYC", "GLOBAL-CREDIT"])
    for got, row in zip(result, want):
        assert got["portfolio"] == row["PORTFOLIO_CODE"] and got["security_id"] == row["SECURITY_ID"]
        assert got["as_of"] == row["AS_OF_DATE"]
        # Exact: a Decimal equal to the fixture's text, never a float.
        assert isinstance(got["market_value"]["amount"], Decimal)
        assert got["market_value"]["amount"] == Decimal(row["MARKET_VALUE"])
        assert got["quantity"] == Decimal(row["QUANTITY"])
        assert got["weight"] == row["WEIGHT"]
    short = next(r for r in result if r["security_id"] == "XS-CORP-BETA-28")
    assert short["quantity"] < 0 and short["market_value"]["amount"] < 0


def test_two_identities(connect):
    bob = connect("dev-bob")
    with pytest.raises(AtlasError) as e:
        bob.call("portfolio.holdings@v1:fetch", {"portfolios": ["147HYC", "GLOBAL-CREDIT"]})
    assert e.value.code == "not_granted" and e.value.target == "/portfolios/1" and e.value.status == 403
    assert [r["code"] for r in bob.call("portfolio.list@v1:fetch")] == ["147HYC"]


def test_invalid_request_is_refused_before_it_is_sent(connect):
    alice = connect("dev-alice")
    with pytest.raises(AtlasError) as e:
        alice.call("portfolio.holdings@v1:fetch", {"portfolios": [], "as_of": "yesterday"})
    assert e.value.code == "invalid_request" and e.value.target == "/as_of" and len(e.value.details) == 2


def test_arrow_is_the_same_answer(connect):
    pytest.importorskip("pyarrow")
    alice = connect("dev-alice")
    table = alice.fetch("portfolio.holdings@v1:fetch", {"portfolios": ["147HYC"], "columns": ["security_id", "market_value"]})
    rows = alice.call("portfolio.holdings@v1:fetch", {"portfolios": ["147HYC"], "columns": ["security_id", "market_value"]})
    assert table.columns == ["security_id", "market_value"] and len(table) == len(rows)
    arrow_rows = table.arrow.to_pylist()
    for a, b in zip(arrow_rows, rows):
        assert a["security_id"] == b["security_id"]
        assert a["market_value"]["amount"] == b["market_value"]["amount"]


def test_r3_06_a_call_waiting_for_the_engine_keeps_its_deadline(connect):
    """R3-06, HANDOFF.md C2: one deadline for the whole call. A call waiting
    for the engine, which another call holds, waits no longer than its own
    deadline: the wait comes out of it, and the call is deadline_exceeded."""
    import time

    from fiatlas._engine import shared
    from fiatlas.errors import AtlasProblem

    alice = connect("dev-alice")
    alice.call("portfolio.list@v1:fetch", {})  # connected
    engine = shared(alice._path)
    import threading

    engine._lock.acquire()  # another call has the engine, for 5s
    held = threading.Timer(5, engine._lock.release)
    held.start()
    try:
        start = time.monotonic()
        with pytest.raises(AtlasProblem) as e:
            alice.call("portfolio.list@v1:fetch", {}, deadline=0.3)
        assert e.value.code == "deadline_exceeded"
        assert time.monotonic() - start < 2, "the call waited past its deadline for the engine"
    finally:
        held.join()
    assert alice.call("portfolio.list@v1:fetch", {}).rows
