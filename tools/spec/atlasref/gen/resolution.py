"""references/vectors/resolution.json (§9.5.8, AP-RESOLVE-001): every case
§9.5.8 lists, and the edges of §9.5.3–§9.5.5.

Each expected record is written by hand from the text; `build` asserts that
resolution.py's row-wise resolution agrees, and that its column-wise
resolution (§9.5.7) equals it on a table of one row and of many."""

import copy

from .. import conformance, fingerprint, profiles, resolution
from ..jsonio import dumps

PATH = "vectors/resolution.json"


def evolving_chain(kind):
    """The #A → #B → #C chain, with each profile's row or request schema."""
    served = conformance.evolving()
    steps = conformance.evolving_steps()
    chain = []
    for p, (req, row, lens) in zip(served, steps):
        schema = row if kind == "row" else {k: v for k, v in req.items() if k not in ("$schema", "$vocabulary")}
        chain.append({"profile": fingerprint.exact_id(p), "schema": copy.deepcopy(schema),
                      "lens": copy.deepcopy(lens or [])})
    return chain


ROW_A = {"id": "p1", "mv": 1250.5, "weight": 1.5, "status": "sold", "count": 3, "legacy": "x"}
ROW_B = {"id": "p1", "market_value": 1250.5, "weight": 0.015, "currency": "EUR", "status": "sold", "count": 3,
         "legacy": None}
ROW_C = {"id": "p1", "market_value": 1250.5, "weight": 0.015, "currency": "EUR", "status": "closed", "count": 3.0,
         "note": None}


def small_chain(name, rows, lenses, traits=()):
    """A chain of small profiles, one row schema per step."""
    chain, previous = [], None
    for row, lens in zip(rows, lenses):
        p = profiles.minimal_data_profile(f"vectors.{name}", 1, row, lens=lens if previous else None,
                                          previous=previous, traits=traits)
        p = fingerprint.with_fingerprint(p)
        exact = fingerprint.exact_id(p)
        chain.append({"profile": exact, "schema": copy.deepcopy(row), "lens": copy.deepcopy(lens or [])})
        previous = exact
    return chain


def row(props, required=()):
    s = {"type": "object", "description": "A row.", "properties": props}
    if required:
        s["required"] = list(required)
    return s


def num(**kw):
    s = {"type": "number", "description": "A number."}
    s.update(kw)
    return s


def string(**kw):
    s = {"type": "string", "description": "A string."}
    s.update(kw)
    return s


def build_cases():
    rows = evolving_chain("row")
    reqs = evolving_chain("request")
    A, B, Cc = (s["profile"] for s in rows)
    V2 = fingerprint.exact_id(conformance.evolving_v2())
    cases = []

    def case(name, chain, writer, reader, record, expected=None, problem=None, columns=None, record_kind="row",
             dynamic=False, note=None, question=None):
        c = {"name": name, "record_kind": record_kind, "chain": copy.deepcopy(chain), "writer": writer,
             "reader": reader, "record": copy.deepcopy(record)}
        if columns is not None:
            c["columns"] = columns
        if dynamic:
            c["dynamic_schema"] = True
        if problem is not None:
            c["problem"] = problem
        else:
            c["expected"] = expected
        if note:
            c["note"] = note
        if question:
            c["question"] = question
        cases.append(c)

    # §9.5.8's list, in its order
    case("identical profiles: unchanged, unknown members and all", rows, Cc, Cc, dict(ROW_C, zzz=1),
         dict(ROW_C, zzz=1), note="§9.5.2 step 1 returns x unchanged.")
    case("an unknown field ignored", rows, B, Cc, dict(ROW_B, zzz=[1, 2]),
         {"id": "p1", "market_value": 1250.5, "weight": 0.015, "currency": "EUR", "status": "closed", "count": 3,
          "note": None}, note="Step 14: members no property of S took are dropped.")
    case("a rename backward", rows, B, A, ROW_B, {"id": "p1", "mv": 1250.5, "weight": 1.5, "status": "sold",
                                                  "count": 3, "legacy": None},
         note="#B's lens backward, in reverse order: region/area (absent), currency removed, weight × 100, "
              "market_value → mv.")
    case("a rename forward", rows, A, B, ROW_A, {"id": "p1", "market_value": 1250.5, "weight": 0.015,
                                                 "currency": "USD", "status": "sold", "count": 3, "legacy": "x"})
    case("a unit conversion: Percent to Fraction is one division by 100", rows, A, B,
         dict(ROW_A, weight=3.1), {"id": "p1", "market_value": 1250.5, "weight": 0.031, "currency": "USD",
                                   "status": "sold", "count": 3, "legacy": "x"},
         note="3.1 / 100 in binary64 is 0.031; 3.1 × 0.01 would be 0.031000000000000003: not the same operation "
              "(§9.5.4).")
    case("a unit conversion backward: Fraction to Percent is one multiplication by 100", rows, B, A,
         dict(ROW_B, weight=0.07), {"id": "p1", "mv": 1250.5, "weight": 7.000000000000001, "status": "sold",
                                    "count": 3, "legacy": None},
         note="0.07 × 100 in binary64 is 7.000000000000001, exactly as written: one multiplication.")
    case("an unknown enum value: UNKNOWN", rows, Cc, B, dict(ROW_C, status="frozen"),
         {"id": "p1", "market_value": 1250.5, "weight": 0.015, "currency": "EUR", "status": "UNKNOWN", "count": 3},
         note="A value #B's enum does not hold, with UNKNOWN in it. drop backward does nothing, so legacy stays "
              "absent; #B does not require it.")
    case("an enum value renamed, forward by the lens", rows, B, Cc, ROW_B, dict(ROW_C, count=3))
    case("an enum value renamed, backward by the lens", rows, Cc, B, ROW_C,
         {"id": "p1", "market_value": 1250.5, "weight": 0.015, "currency": "EUR", "status": "sold", "count": 3})
    case("a promotion forward: integer to number, the same value", rows, B, Cc, dict(ROW_B, count=7),
         dict(ROW_C, count=7))
    case("a promotion backward, exact", rows, Cc, B, dict(ROW_C, count=4.0),
         {"id": "p1", "market_value": 1250.5, "weight": 0.015, "currency": "EUR", "status": "sold", "count": 4})
    case("a promotion backward, not exact: refused", rows, Cc, B, dict(ROW_C, count=2.5),
         problem={"code": "resolution_failed", "target": "/count"})
    case("a default forward", rows, A, B, {k: v for k, v in ROW_A.items() if k != "legacy"},
         {"id": "p1", "market_value": 1250.5, "weight": 0.015, "currency": "USD", "status": "sold", "count": 3})
    case("a default backward removes the field", rows, B, A, dict(ROW_B, currency="GBP"),
         {"id": "p1", "mv": 1250.5, "weight": 1.5, "status": "sold", "count": 3, "legacy": None})
    case("a required field missing", rows, A, B, {k: v for k, v in ROW_A.items() if k != "id"},
         problem={"code": "resolution_failed", "target": "/id"})
    case("a nullable field missing: null", rows, B, Cc, dict(ROW_B, status="open"),
         dict(ROW_C, status="open", count=3), note="note is required and nullable in #C, and #B has none: step 9.")
    case("an old request resolved forward by the gateway", reqs, A, Cc,
         {"ids": ["p1", "p2"], "region": "EU", "columns": ["id", "mv"]},
         {"ids": ["p1", "p2"], "area": "EU", "columns": ["id", "mv"]}, record_kind="request",
         note="The rename region → area applies to the request; the default adds currency, which the request "
              "schema does not take (dropped). Mapping the value \"mv\" in columns is the gateway's planning, not "
              "§9.5.")
    case("a projected row lacks the required columns it did not ask for", rows, B, Cc,
         {"id": "p1", "weight": 0.015}, {"id": "p1", "weight": 0.015}, columns=["id", "weight"])
    case("a projected row whose requested column was renamed", rows, Cc, A,
         {"id": "p1", "market_value": 99.5}, {"id": "p1", "mv": 99.5}, columns=["id", "mv"],
         note="The reader names its columns; the writer's market_value is renamed back to mv.")
    case("profiles of two different versions: refused", rows + [{"profile": V2, "schema": {}, "lens": []}], V2, A,
         {"id": "p1"}, problem={"code": "resolution_failed"},
         note="Only the gateway resolves across versions (§9.5.2 step 2).")

    # §9.5.2–§9.5.5 at their edges
    case("through two lenses forward", rows, A, Cc, ROW_A,
         {"id": "p1", "market_value": 1250.5, "weight": 0.015, "currency": "USD", "status": "closed", "count": 3,
          "note": None})
    case("through two lenses backward", rows, Cc, A, ROW_C,
         {"id": "p1", "mv": 1250.5, "weight": 1.5, "status": "sold", "count": 3})
    case("a DynamicSchema capability: no resolution", rows, Cc, A, {"anything": [1]}, {"anything": [1]},
         dynamic=True)
    case("a value that cannot be converted", rows, A, B, dict(ROW_A, count="three"),
         problem={"code": "resolution_failed", "target": "/count"})
    case("null passes through a value operation", rows, B, Cc, dict(ROW_B, status=None),
         problem={"code": "resolution_failed", "target": "/status"},
         note="alias_value leaves null; status in #C allows no null, so step 12 fails.")
    case("a unit conversion of a value that is not a number", rows, A, B, dict(ROW_A, weight="1.5"),
         problem={"code": "resolution_failed", "target": "/weight"})

    alias = small_chain("aliases", [
        row({"mv": num(**{"x-atlas-unit": "currency_unit"})}),
        row({"market_value": num(**{"x-atlas-unit": "currency_unit", "x-atlas-aliases": ["mv", "value"]})},
            required=["market_value"]),
    ], [None, []])
    a0, a1 = alias[0]["profile"], alias[1]["profile"]
    case("an alias, with no lens (step 7)", alias, a0, a1, {"mv": 5}, {"market_value": 5})
    case("the last alias present wins (aliases oldest first)", alias, a0, a1, {"mv": 5, "value": 6},
         {"market_value": 6})
    case("the name itself wins over an alias", alias, a0, a1, {"mv": 5, "market_value": 7}, {"market_value": 7})

    dflt = small_chain("defaults", [
        row({"a": num(**{"x-atlas-unit": "count"})}),
        row({"a": num(**{"x-atlas-unit": "count"}),
             "b": {"type": ["number", "null"], "description": "B.", "x-atlas-unit": "count", "default": 2},
             "c": {"type": ["string", "null"], "description": "C."},
             "d": string()}, required=["a", "b", "c"]),
    ], [None, []])
    d0, d1 = dflt[0]["profile"], dflt[1]["profile"]
    case("a schema default (step 8) before null (step 9)", dflt, d0, d1, {"a": 1},
         {"a": 1, "b": 2, "c": None}, note="b takes its default although it allows null; c is required and "
                                           "nullable: null; d is not required: absent.")

    enums = small_chain("enums", [
        row({"rating": string(enum=["aa", "a", "UNKNOWN"], **{"x-atlas-enum": [
            {"value": "aa"}, {"value": "a"}, copy.deepcopy(profiles.UNKNOWN_ENTRY)]})}),
        row({"rating": string(enum=["double_a", "single_a", "UNKNOWN"], **{"x-atlas-enum": [
            {"value": "double_a", "aliases": ["aa"]}, {"value": "single_a", "aliases": ["a"]},
            copy.deepcopy(profiles.UNKNOWN_ENTRY)]})}, required=["rating"]),
        row({"rating": string(enum=["double_a", "single_a"])}, required=["rating"]),
    ], [None, [], []])
    e0, e1, e2 = (s["profile"] for s in enums)
    case("an enum value through x-atlas-enum's aliases, with no lens", enums[:2], e0, e1, {"rating": "aa"},
         {"rating": "double_a"})
    case("an unknown enum value without UNKNOWN: refused", enums[1:], e1, e2, {"rating": "b"},
         problem={"code": "resolution_failed", "target": "/rating"})

    dec = small_chain("decimals", [
        row({"rate": string(**{"x-atlas-logical": "Decimal(12,4)"})}, required=["rate"]),
        row({"rate": string(**{"x-atlas-logical": "Decimal(12,4)"})}, required=["rate"]),
    ], [None, [{"op": "convert_unit", "field": "rate", "from": "Percent", "to": "Fraction"}]])
    f0, f1 = dec[0]["profile"], dec[1]["profile"]
    case("a decimal converted exactly, then rounded to the reader's scale", dec, f0, f1, {"rate": "1.2345"},
         {"rate": "0.0123"}, note="1.2345 / 100 = 0.012345, exactly; to four digits, 0.0123.", question="Q-RES-2")
    case("a decimal tie rounds to even, up", dec, f0, f1, {"rate": "1.2350"}, {"rate": "0.0124"},
         note="0.012350: the tie goes to the even 0.0124.")
    case("a decimal tie rounds to even, down", dec, f0, f1, {"rate": "1.2250"}, {"rate": "0.0122"},
         note="0.012250: the tie goes to the even 0.0122.")
    case("a decimal that rounds to zero is never -0", dec, f0, f1, {"rate": "-0.0001"}, {"rate": "0.0000"},
         note="-0.000001 rounds to zero, written 0.0000: §9.1.5 allows no -0.")
    case("a decimal backward, multiplied exactly, written with s digits", dec, f1, f0, {"rate": "0.0124"},
         {"rate": "1.2400"})

    nested = small_chain("nested", [
        row({"leg": {"type": "object", "description": "A leg.", "properties": {
            "side": string(enum=["pay", "receive", "UNKNOWN"], **{"x-atlas-enum": [
                {"value": "pay"}, {"value": "receive"}, copy.deepcopy(profiles.UNKNOWN_ENTRY)]}),
            "rate": num(**{"x-atlas-logical": "Percent"})}}}),
        row({"leg": {"type": "object", "description": "A leg.", "required": ["side"], "properties": {
            "side": string(enum=["pay", "UNKNOWN"], **{"x-atlas-enum": [
                {"value": "pay"}, copy.deepcopy(profiles.UNKNOWN_ENTRY)]}),
            "rate": num(**{"x-atlas-logical": "Percent"})}}}, required=["leg"]),
    ], [None, []])
    n0, n1 = nested[0]["profile"], nested[1]["profile"]
    case("a nested object valid as it is: unchanged, its unknown members kept", nested, n0, n1,
         {"leg": {"side": "pay", "rate": 1.5, "extra": True}}, {"leg": {"side": "pay", "rate": 1.5, "extra": True}},
         note="§9.5.5's first row: a value valid against the reader's schema is unchanged.", question="Q-RES-4")
    case("a nested object that needs converting: resolved by step 5, its unknown members dropped", nested, n0, n1,
         {"leg": {"side": "receive", "rate": 1.5, "extra": True}}, {"leg": {"side": "UNKNOWN", "rate": 1.5}},
         question="Q-RES-4")
    case("a nested failure is reported at the top-level member", nested, n0, n1, {"leg": {"rate": 1.5}},
         problem={"code": "resolution_failed", "target": "/leg"}, question="Q-RES-1")

    arrays = small_chain("arrays", [
        row({"sides": {"type": "array", "description": "Sides.", "items": string(
            enum=["buy", "sell", "UNKNOWN"], **{"x-atlas-enum": [{"value": "buy"}, {"value": "sell"},
                                                                 copy.deepcopy(profiles.UNKNOWN_ENTRY)]})},
             "days": {"type": "array", "description": "Durations.",
                      "items": num(type=["number", "null"], **{"x-atlas-unit": "days"})},
             "on": string(format="date")}),
        row({"sides": {"type": "array", "description": "Sides.", "items": string(
            enum=["purchase", "sell", "UNKNOWN"], **{"x-atlas-enum": [{"value": "purchase"}, {"value": "sell"},
                                                                      copy.deepcopy(profiles.UNKNOWN_ENTRY)]})},
             "days": {"type": "array", "description": "Durations.",
                      "items": num(type=["number", "null"], **{"x-atlas-unit": "seconds"})},
             "on": string(format="date-time")}),
    ], [None, [{"op": "alias_value", "field": "sides", "from": "buy", "to": "purchase"},
               {"op": "convert_unit", "field": "days", "from": "days", "to": "seconds"}]])
    r0, r1 = arrays[0]["profile"], arrays[1]["profile"]
    case("value operations apply to each element of an array", arrays, r0, r1,
         {"sides": ["buy", "sell", "hold"], "days": [1, 0.5, None], "on": "2026-10-31"},
         {"sides": ["purchase", "sell", "UNKNOWN"], "days": [86400.0, 43200.0, None], "on": "2026-10-31T00:00:00Z"},
         note="alias_value and convert_unit per element, null passing through; an unknown element maps to UNKNOWN "
              "(§9.5.5, arrays); a date becomes the date at T00:00:00Z.")
    case("a date-time backward is not converted to a date by step 12", arrays, r1, r0,
         {"sides": [], "days": [86400], "on": "2026-10-31T00:00:00Z"}, problem={
             "code": "resolution_failed", "target": "/on"},
         note="§9.5.5 converts a date to a date-time, never back: only promote backward does.")
    return cases


def build():
    cases = build_cases()
    for c in cases:
        try:
            got = resolution.resolve(c["record"], c["writer"], c["reader"], c["chain"], c.get("columns"),
                                     c.get("dynamic_schema", False))
            got_problem = None
        except resolution.ResolutionFailed as e:
            got, got_problem = None, e.problem()
        if "problem" in c:
            if got_problem != c["problem"]:
                raise AssertionError(f"resolution.py gives {got_problem or got} for {c['name']!r}")
        elif got_problem is not None or got != c["expected"] or not _same_types(got, c["expected"]):
            raise AssertionError(f"resolution.py gives {got_problem or got} for {c['name']!r}")
        if c["record_kind"] == "row" and "problem" not in c and not c.get("dynamic_schema"):
            _check_columns(c, got)
    names = [c["name"] for c in cases]
    assert len(names) == len(set(names))
    doc = {
        "description": "§9.5 (AP-RESOLVE-001): each case gives the chain, oldest first (each profile's exact-id, "
                       "its schema for this kind of record and its lens from the profile before), the writer and "
                       "the reader, the record, the requested columns where there are any, and the expected record "
                       "or problem (resolution_failed, with the target where §9.5 gives one). Numbers compare as "
                       "binary64 values. A Columnar client's column-wise resolution of every row case, as a table "
                       "of one row and of many, gives the same records (AP-RESOLVE-002). `dynamic_schema` marks a "
                       "capability with the trait DynamicSchema.",
        "cases": cases,
    }
    return {PATH: dumps(doc)}


def _same_types(a, b):
    if isinstance(a, dict):
        return isinstance(b, dict) and a.keys() == b.keys() and all(_same_types(a[k], b[k]) for k in a)
    if isinstance(a, list):
        return isinstance(b, list) and len(a) == len(b) and all(_same_types(x, y) for x, y in zip(a, b))
    return a == b


def _check_columns(c, row_result):
    """§9.5.7: the column-wise result equals the row-wise one."""
    record = c["record"]
    others = []
    for k in range(3):
        r = copy.deepcopy(record)
        for name, value in r.items():
            if isinstance(value, (int, float)) and not isinstance(value, bool):
                r[name] = value  # the same values: a column of one value repeated
        others.append(r)
    table = [copy.deepcopy(record)] + others
    if any(set(t) != set(record) for t in table):
        return
    cols = resolution.rows_to_columns(table)
    out = resolution.resolve_columns(cols, c["writer"], c["reader"], c["chain"], c.get("columns"))
    rows_out = resolution.columns_to_rows(out)
    expected = [row_result] * len(table)
    if rows_out != expected:
        raise AssertionError(f"column-wise differs from row-wise for {c['name']!r}: {rows_out[0]} vs {row_result}")
    single = resolution.columns_to_rows(resolution.resolve_columns(
        resolution.rows_to_columns([record]), c["writer"], c["reader"], c["chain"], c.get("columns")))
    if single != [row_result]:
        raise AssertionError(f"column-wise (one row) differs for {c['name']!r}")
