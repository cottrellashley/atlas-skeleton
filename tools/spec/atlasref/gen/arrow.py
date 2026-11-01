"""references/vectors/arrow.json (AP-SCHEMA-002): table schemas and the Arrow
schema §9.2.7 derives from each, in Apache Arrow's integration-test JSON form.
`build` round-trips every derived schema through pyarrow."""

import copy

from .. import arrowmap, fingerprint, profiles
from ..jsonio import dumps

PATH = "vectors/arrow.json"
EXACT = "conformance.rows@v1#0123456789abcdef"


def table(row_props, required=(), defs=None):
    row = {"type": "object", "required": list(required), "properties": row_props} if required else {
        "type": "object", "properties": row_props}
    s = profiles.root({"type": "array", "items": row})
    if defs:
        s["$defs"] = defs
    return s


CASES = [
    ("every scalar row of the table", table({
        "flag": {"type": "boolean"}, "n": {"type": "integer"}, "x": {"type": "number"}, "s": {"type": "string"},
        "day": {"type": "string", "format": "date"}, "at": {"type": "string", "format": "date-time"},
        "took": {"type": "string", "format": "duration"}, "big": {"type": "string", "format": "int64"},
        "price": {"type": "string", "format": "decimal", "x-atlas-logical": "Decimal(18,6)"},
    }, required=["flag", "n", "x", "s", "day", "at", "took", "big", "price"]), None),
    ("nullable: a null type, or not required", table({
        "a": {"type": ["string", "null"]}, "b": {"type": "string"}, "c": {"type": "integer"},
    }, required=["a", "b"]), None),
    ("the order of properties, not sorted", table({
        "zeta": {"type": "integer"}, "alpha": {"type": "integer"}, "mid": {"type": "integer"},
    }, required=["zeta", "alpha", "mid"]), "Q-ARROW-1"),
    ("Decimal(p,s) without format", table({"d": {"type": "string", "x-atlas-logical": "Decimal(38,0)"}},
                                          required=["d"]), None),
    ("Money", table({"mv": {"type": ["object", "null"], "x-atlas-logical": "Money"}}, required=["mv"]), None),
    ("a list of strings, its items nullable", table({
        "tags": {"type": "array", "items": {"type": ["string", "null"]}},
    }, required=["tags"]), None),
    ("a list of structs", table({
        "legs": {"type": "array", "items": {"type": "object", "required": ["tenor"], "properties": {
            "tenor": {"type": "string", "x-atlas-logical": "Tenor"}, "rate": {"type": ["number", "null"],
                                                                             "x-atlas-logical": "Percent"}}}},
    }, required=["legs"]), None),
    ("a struct of the mapped properties", table({
        "issuer": {"type": "object", "required": ["lei"], "properties": {
            "lei": {"type": "string", "x-atlas-logical": "Lei"}, "name": {"type": "string"}}},
    }), None),
    ("every Atlas keyword as metadata, its value the JSON text", table({
        "portfolio": {"type": "string", "x-atlas-logical": "PortfolioCode", "x-atlas-key": True,
                      "x-atlas-reference": "portfolio.list@v1"},
        "market_value": {"type": "object", "x-atlas-logical": "Money", "x-atlas-aliases": ["mv"]},
        "oas": {"type": ["number", "null"], "x-atlas-logical": "Bp", "x-atlas-deprecated": None},
        "weight": {"type": "number", "x-atlas-unit": "fraction"},
        "status": {"type": "string", "enum": ["open", "UNKNOWN"],
                   "x-atlas-enum": [{"value": "open", "description": "Held."}, {"value": "UNKNOWN"}]},
        "duration": {"type": ["number", "null"], "x-atlas-logical": "Years", "x-atlas-deprecated": {
            "reason": "Use risk.duration@v1.", "since": "2026-09-01", "sunset": "2027-03-01", "replaced_by": None}},
    }, required=["portfolio", "market_value", "weight", "status"]), "Q-ARROW-4"),
    ("strings with other formats and string logical types are Utf8", table({
        "u": {"type": "string", "format": "uri"}, "id": {"type": "string", "format": "uuid"},
        "cap": {"type": "string", "format": "capability-id"}, "dec": {"type": "string", "format": "decimal"},
        "isin": {"type": "string", "x-atlas-logical": "Isin"}, "ccy": {"type": "string", "x-atlas-logical": "Currency"},
    }, required=["u", "id", "cap", "dec", "isin", "ccy"]), "Q-ARROW-2"),
    ("a property through $ref", table({"p": {"$ref": "#/$defs/price", "description": "A price."}}, required=["p"],
                                      defs={"price": {"type": "string", "x-atlas-logical": "Decimal(12,4)"}}), None),
    ("the data profile's result", profiles.holdings()["schemas"]["result"], None),
]

REFUSED = [
    ("an object without properties", table({"blob": {"type": "object"}}), "/items/properties/blob", "Q-ARROW-3"),
    ("an array without items", table({"xs": {"type": "array"}}), "/items/properties/xs/items", "Q-ARROW-3"),
    ("anyOf", table({"e": {"anyOf": [{"type": "string"}, {"type": "number"}]}}), "/items/properties/e", "Q-ARROW-3"),
    ("a DynamicSchema result: an open array of objects", profiles.root({"type": "array", "items": {"type": "object"}}),
     "/items", "Q-ARROW-3"),
    ("a record result is not a table", profiles.root({"type": "object", "properties": {"a": {"type": "string"}}}), "",
     None),
]


def build():
    import pyarrow as pa

    cases = []
    for name, schema, q in CASES:
        exact = EXACT
        if name == "the data profile's result":
            exact = fingerprint.exact_id(fingerprint.with_fingerprint(profiles.holdings()))
        derived = arrowmap.derive(schema, exact)
        pas = arrowmap.to_pa_schema(derived)
        back = [arrowmap.from_pa_field(pas.field(i)) for i in range(len(pas))]
        assert back == derived["fields"], name
        assert isinstance(pas, pa.Schema)
        c = {"name": name, "schema": copy.deepcopy(schema), "exact": exact, "expected": {"arrow": derived}}
        if q:
            c["question"] = q
        cases.append(c)
    for name, schema, target, q in REFUSED:
        try:
            arrowmap.derive(schema, EXACT)
        except arrowmap.NotDerivable as e:
            assert e.pointer == target, (name, e.pointer)
        else:
            raise AssertionError(f"{name} derived")
        c = {"name": "refused: " + name, "schema": copy.deepcopy(schema), "exact": EXACT,
             "problem": {"code": "invalid_document", "target": target}}
        if q:
            c["question"] = q
        cases.append(c)
    doc = {
        "description": "§9.2.7 (AP-SCHEMA-002): a table result's schema (a JSON Schema of an array of row objects) "
                       "and its Arrow schema, derived: one field per row property in the order of `properties` "
                       "(Q-ARROW-1), nullable when the property allows null or is not required, each Atlas keyword "
                       "as field metadata atlas:<keyword> whose value is the keyword's JSON text in RFC 8785 form "
                       "(Q-ARROW-4), and the schema metadata atlas:profile (the exact-id, here `exact`) and "
                       "atlas:protocol. Arrow schemas are in the JSON form of Apache Arrow's integration tests "
                       "(fields of name, nullable, type, children, metadata). A schema the table cannot map is "
                       "refused: invalid_document, target the property.",
        "cases": cases,
    }
    return {PATH: dumps(doc)}
