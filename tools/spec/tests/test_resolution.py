"""§9.5 against the spec's worked examples."""

import copy

import pytest

from atlasref import fingerprint, profiles, resolution


def chain(*steps):
    out, prev = [], None
    for row, lens in steps:
        p = fingerprint.with_fingerprint(profiles.minimal_data_profile("portfolio.holdings", 1, row, lens=lens,
                                                                       previous=prev))
        out.append({"profile": fingerprint.exact_id(p), "schema": row, "lens": lens or []})
        prev = fingerprint.exact_id(p)
    return out


def test_section_3_3_4_mv_renamed_market_value():
    """#A has mv; #C lists aliases [mv] on market_value and the lens rename(mv → market_value). An answer
    written under #C is resolved by a client locked to #A back to mv."""
    a = {"type": "object", "properties": {"portfolio": {"type": "string"}, "mv": {"type": "number"}}}
    c = {"type": "object", "properties": {"portfolio": {"type": "string"},
                                          "market_value": {"type": "number", "x-atlas-aliases": ["mv"]}}}
    ch = chain((a, None), (c, [{"op": "rename", "from": "mv", "to": "market_value"}]))
    A, C = ch[0]["profile"], ch[1]["profile"]
    assert resolution.resolve({"portfolio": "147HYC", "market_value": 12.5}, C, A, ch,
                              columns=["portfolio", "mv"]) == {"portfolio": "147HYC", "mv": 12.5}
    # and an old request names mv, which the newer reader takes by its alias
    assert resolution.resolve({"portfolio": "147HYC", "mv": 12.5}, A, C, ch) == {"portfolio": "147HYC",
                                                                                 "market_value": 12.5}


def test_section_3_2_oas_in_bp_read_as_a_fraction():
    """A reader whose profile says Fraction gets Bp divided by 10,000."""
    w = {"type": "object", "properties": {"oas": {"type": ["number", "null"], "x-atlas-logical": "Fraction"}}}
    r = {"type": "object", "properties": {"oas": {"type": ["number", "null"], "x-atlas-logical": "Bp"}}}
    ch = chain((w, None), (r, [{"op": "convert_unit", "field": "oas", "from": "Fraction", "to": "Bp"}]))
    assert resolution.resolve({"oas": 125.0}, ch[1]["profile"], ch[0]["profile"], ch) == {"oas": 0.0125}
    assert resolution.resolve({"oas": None}, ch[1]["profile"], ch[0]["profile"], ch) == {"oas": None}


def test_section_3_4_an_old_event_gets_the_default():
    """data.landed@v1 gains duration with a default lens; a six-month-old event lacks it."""
    old = {"type": "object", "properties": {"source": {"type": "string"}}}
    new = {"type": "object", "required": ["source", "duration"],
           "properties": {"source": {"type": "string"}, "duration": {"type": "string", "format": "duration"}}}
    ch = chain((old, None), (new, [{"op": "default", "field": "duration", "value": "PT0S"}]))
    assert resolution.resolve({"source": "index"}, ch[0]["profile"], ch[1]["profile"], ch) == {
        "source": "index", "duration": "PT0S"}
    # an old subscriber reading today's event simply ignores duration
    assert resolution.resolve({"source": "index", "duration": "PT5M"}, ch[1]["profile"], ch[0]["profile"], ch) == {
        "source": "index"}


def test_two_versions_are_refused():
    with pytest.raises(resolution.ResolutionFailed):
        resolution.resolve({}, "a.b@v2#0000000000000000", "a.b@v1#0000000000000000", [])


def test_column_wise_equals_row_wise():
    w = {"type": "object", "properties": {"w": {"type": "number"}}}
    r = {"type": "object", "properties": {"w": {"type": "number"}}}
    ch = chain((w, None), (r, [{"op": "convert_unit", "field": "w", "from": "Percent", "to": "Fraction"}]))
    rows = [{"w": x} for x in (3.1, 50.0, 100.0)]
    by_row = [resolution.resolve(x, ch[0]["profile"], ch[1]["profile"], ch) for x in rows]
    by_col = resolution.columns_to_rows(resolution.resolve_columns(resolution.rows_to_columns(copy.deepcopy(rows)),
                                                                   ch[0]["profile"], ch[1]["profile"], ch))
    assert by_row == by_col == [{"w": 0.031}, {"w": 0.5}, {"w": 1.0}]  # 3.1 / 100 is 0.031 in binary64
