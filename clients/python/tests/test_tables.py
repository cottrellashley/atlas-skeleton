"""Tables in Python: every column arrives with its declared type, the same
whichever way the rows travelled."""

from __future__ import annotations

import datetime as dt
from decimal import Decimal

import pandas as pd
import pyarrow as pa
import pytest

from conftest import HOLDINGS, TWO
from fiatlas import AtlasError
from fiatlas._engine import Engine, engine_path


def test_every_column_keeps_the_type_its_profile_declares(gateway):
    t = gateway().atlas().fetch(HOLDINGS, TWO)
    assert t.via == "gateway" and t.writer == t.reader
    schema = t.read_as(pa.Table).schema
    assert schema.field("market_value").type == pa.decimal128(18, 2)
    assert schema.field("weight").type == pa.decimal128(9, 6)
    assert schema.field("as_of").type == pa.date32()
    assert schema.field("oas").type == pa.float64()
    assert schema.field("portfolio").type == pa.string()

    df = t.read_as(pd.DataFrame)
    assert str(df["market_value"].dtype) == "decimal128(18, 2)[pyarrow]"
    assert str(df["as_of"].dtype) == "date32[day][pyarrow]"
    mv = df["market_value"].iloc[0]
    assert isinstance(mv, Decimal) and mv.as_tuple().exponent == -2  # no decimal becomes a float (ATLAS-PY-002)
    assert set(df["portfolio"]) == {"147HYC", "200IG"}


@pytest.mark.parametrize("chunk_rows", ["7", "10000"])
def test_streamed_and_handed_off_tables_are_identical(gateway, chunk_rows):
    atlas = gateway("-chunk-rows", chunk_rows).atlas()
    streamed = atlas.fetch(HOLDINGS, TWO, execution="gateway")
    handed = atlas.fetch(HOLDINGS, TWO, execution="handoff")
    assert (streamed.via, handed.via) == ("gateway", "handoff")
    assert len(streamed) > 0
    assert streamed.read_as(pa.Table).equals(handed.read_as(pa.Table))


def test_the_json_form_and_the_arrow_form_carry_the_same_values(gateway):
    """The conformance suite reads a table as JSON row records (§9.8.3), the
    face as Arrow: the one engine writes both, and they must agree."""
    g = gateway()
    e = Engine(engine_path())
    try:
        e.run("connect", {"url": g.url, "token": g.token()})
        as_json = e.run("fetch", {"address": HOLDINGS, "request": TWO})["result"]["rows"]
        as_arrow = e.run("fetch", {"address": HOLDINGS, "request": TWO}, results="arrow")["table"].to_pylist()
    finally:
        e.stop()
    assert len(as_json) == len(as_arrow) > 0
    for j, a in zip(as_json, as_arrow):
        assert j == {k: json_form(v) for k, v in a.items()}


def json_form(v):
    """A value as §9.8.3 writes it: decimals as their strings, dates as ISO dates."""
    if isinstance(v, Decimal):
        return format(v, "f")
    if isinstance(v, dt.date):
        return v.isoformat()
    return v


def test_problems_arrive_as_exceptions_with_their_codes_and_the_engine_carries_on(gateway):
    g = gateway()
    with pytest.raises(AtlasError) as e:
        g.atlas("outsider").fetch(HOLDINGS, TWO)
    assert e.value.code == "not_granted"

    atlas = g.atlas()
    with pytest.raises(AtlasError) as e:
        atlas.fetch(HOLDINGS, {"portfolios": "147HYC"})
    assert e.value.code == "invalid_request"  # refused before it is sent
    with pytest.raises(AtlasError) as e:
        atlas.fetch("portfolio.missing@v1:fetch")
    assert e.value.code == "not_found"

    engine = atlas._engine.pid
    assert len(atlas.fetch(HOLDINGS, TWO)) > 0
    assert atlas._engine.pid == engine  # one engine throughout: a problem is an answer, not a failure
