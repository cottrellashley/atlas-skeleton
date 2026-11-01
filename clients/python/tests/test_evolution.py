"""The heart of it, from Python: the contract changes behind a locked
application, and the application's DataFrame does not."""

from __future__ import annotations

import math

import pandas as pd

from conftest import HOLDINGS, TWO

V1 = ["portfolio", "security", "as_of", "mv", "weight", "oas", "sector"]


def test_a_locked_application_keeps_its_dataframe_while_the_contract_evolves(gateway, tmp_path):
    lock = tmp_path / "atlas.lock"

    # 1. Built against the first profile: mv, and oas in basis points.
    before = gateway("-pin", "portfolio.holdings@v1=1")
    a1 = before.atlas()
    pinned = a1.lock(["portfolio.holdings@v1"], lock)
    t1 = a1.fetch(HOLDINGS, TWO)
    df1 = t1.read_as(pd.DataFrame)
    assert t1.reader == pinned["portfolio.holdings@v1"]
    assert list(df1.columns) == V1

    # 2. The gateway is redeployed two profiles on. An application with no
    # lock sees the new contract: market_value, oas as a fraction, a new
    # sector and a new column.
    after = gateway()
    fresh = after.atlas().fetch(HOLDINGS, TWO)
    now = fresh.read_as(pd.DataFrame)
    assert fresh.writer != t1.reader
    assert list(now.columns) == ["portfolio", "security", "as_of", "market_value", "weight", "oas", "sector", "currency"]
    supranational = (now["sector"] == "Supranational").to_numpy()
    assert supranational.any()
    for bp, fraction in zip(column(t1, "oas"), column(fresh, "oas")):
        assert (bp is None) == (fraction is None) and (bp is None or math.isclose(bp / 10_000, fraction, rel_tol=1e-12))

    # 3. The locked application, unchanged, reads the same DataFrame as before,
    # streamed or handed off.
    for via in ("gateway", "handoff"):
        t2 = after.atlas(lock=lock).fetch(HOLDINGS, TWO, execution=via)
        assert t2.via == via and t2.reader == t1.reader and t2.writer == fresh.writer
        df2 = t2.read_as(pd.DataFrame)
        assert list(df2.columns) == V1
        assert list(df2.dtypes) == list(df1.dtypes)
        pd.testing.assert_frame_equal(df2.drop(columns="oas"), df1.drop(columns="oas"))
        # oas crossed a unit lens twice in binary64, bp to fraction at the
        # source and back for the lock: equal to the last bits or so (§9.5.4).
        for a, b in zip(column(t1, "oas"), column(t2, "oas")):
            assert (a is None) == (b is None) and (a is None or math.isclose(a, b, rel_tol=1e-9))
        assert (df2.loc[supranational, "sector"] == "UNKNOWN").all()  # a sector the lock does not know

    # 4. A request written against the old profile still means what it meant.
    old_names = after.atlas(lock=lock).fetch(HOLDINGS, {"portfolios": ["147HYC"], "columns": ["mv", "oas"]})
    assert old_names.columns == ["mv", "oas"]


def column(t, name):
    return t.read_as("arrow").column(name).to_pylist()


def test_a_capability_added_later_is_callable_with_no_client_change(gateway):
    old = gateway("-pin", "index.constituents@v1=0").atlas()
    assert "index.constituents" not in [e["id"] for e in old.search("index constituents")["items"]]

    atlas = gateway().atlas()
    found = atlas.search("index constituents")["items"]
    assert found and found[0]["id"] == "index.constituents"
    t = atlas.fetch(found[0]["id"] + "@v1:fetch", {"indices": ["H0A0"]})
    assert len(t) > 0 and set(t.read_as(pd.DataFrame)["index"]) == {"H0A0"}
