"""§9.2.7's mapping, the Snowflake decoding of §2.2.1, and §6.4.3's guard."""

import decimal

from atlasref import arrowmap, grammar, guard, profiles, snowarrow


def test_section_3_2_oas_is_a_nullable_float64_with_its_logical_type():
    s = profiles.root({"type": "array", "items": {"type": "object", "properties": {
        "oas": {"type": ["number", "null"], "x-atlas-logical": "Bp"}}}})
    f = arrowmap.derive(s, "a.b@v1#0123456789abcdef")["fields"][0]
    assert f["type"] == {"name": "floatingpoint", "precision": "DOUBLE"} and f["nullable"]
    assert f["metadata"] == [{"key": "atlas:logical", "value": '"Bp"'}]


def test_section_2_2_1_number_18_6_is_an_exact_decimal_and_timestamps_utc_microseconds():
    cols = [{"name": "price", "type": "FIXED", "precision": 18, "scale": 6, "nullable": False},
            {"name": "at", "type": "TIMESTAMP_TZ", "scale": 9, "nullable": False}]
    t = grammar.parse_date_time_ns("2026-10-30T14:02:11.123456789Z")
    chunk = snowarrow.write_chunk(cols, [[decimal.Decimal("101.234567"), (t, 60)]])
    row = profiles.root({"type": "array", "items": {"type": "object", "required": ["price", "at"], "properties": {
        "price": {"type": "string", "x-atlas-logical": "Decimal(18,6)"},
        "at": {"type": "string", "format": "date-time"}}}})
    batch = snowarrow.decode_chunk(chunk, arrowmap.derive(row, "a.b@v1#0123456789abcdef"), cols)
    assert snowarrow.rows_json(batch) == [{"price": "101.234567", "at": "2026-10-30T14:02:11.123456Z"}]


def test_the_guard():
    reg = [["HARBOUR", "GOLD"]]
    assert guard.check("SELECT * FROM HARBOUR.GOLD.HOLDINGS", reg)[0]
    assert guard.check("DELETE FROM HARBOUR.GOLD.HOLDINGS", reg)[1] == "dml"
    assert guard.check("SELECT * FROM TABLE(RESULT_SCAN(LAST_QUERY_ID()))", reg)[1] == "table_function"
    assert guard.check("WITH x AS (SELECT * FROM HARBOUR.GOLD.H) SELECT * FROM x", reg)[0]
