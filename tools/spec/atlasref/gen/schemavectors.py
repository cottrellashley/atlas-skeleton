"""references/vectors/schemas.json (AP-SCHEMA-001): §9.2.5's validation
results, and the schemas the Atlas Schema profile (§9.2.1–§9.2.4) refuses.

Every expected result is written by hand; `build` asserts that validator.py
and profilecheck.py agree, and `check` that jsonschema (extended with the
profile's I-Regexp, formats and logical types) gives the same verdicts.
"""

import copy

from .. import profiles, schemas
from ..jsonio import dumps
from ..profilecheck import check_schema
from ..validator import validate

PATH = "vectors/schemas.json"
root = profiles.root
E = profiles.UNKNOWN_ENTRY
BS = chr(92)


def obj(props, required=(), **kw):
    s = {"type": "object"}
    if required:
        s["required"] = list(required)
    s["properties"] = props
    s.update(kw)
    return s


S = {"type": "string"}
I = {"type": "integer"}
NUM = {"type": "number"}


def v(name, schema, instance=None, errors=(), instance_text=None, note=None, question=None):
    """A validation case; `errors` are (pointer, keyword), in sorted order."""
    case = {"name": name, "kind": "validate", "schema": root(copy.deepcopy(schema))}
    if instance_text is not None:
        case["instance_text"] = instance_text
    else:
        case["instance"] = instance
    case["expected"] = {"valid": not errors, "errors": [{"pointer": p, "keyword": k} for p, k in errors]}
    if note:
        case["note"] = note
    if question:
        case["question"] = question
    return case


money = {"type": "object", "x-atlas-logical": "Money"}
VALIDATE = [
    v("type string: a string", S, "a"),
    v("type string: a number", S, 1, [("", "type")]),
    v("type integer: 1", I, 1),
    v("type integer: 1.0 has no fractional part", I, 1.0),
    v("type integer: 1.5", I, 1.5, [("", "type")]),
    v("type integer: 2^53-1, the largest I-JSON integer", I, 9007199254740991),
    v("type integer: 2^53 is outside I-JSON", I, 9007199254740992, [("", "type")]),
    v("type integer: -(2^53)+1", I, -9007199254740991),
    v("type integer: 1e300 is outside I-JSON", I, 1e300, [("", "type")]),
    v("type integer: a boolean is not a number", I, True, [("", "type")]),
    v("type number: 1e300", NUM, 1e300),
    v("type number: a string", NUM, "1", [("", "type")]),
    v("type boolean: true", {"type": "boolean"}, True),
    v("type boolean: 0", {"type": "boolean"}, 0, [("", "type")]),
    v("type null: null", {"type": "null"}, None),
    v("type [string, null]: null", {"type": ["string", "null"]}, None),
    v("type [string, null]: 1", {"type": ["string", "null"]}, 1, [("", "type")]),
    v("type array: an object", {"type": "array"}, {}, [("", "type")]),
    v("type object: an array", {"type": "object"}, [], [("", "type")]),
    v("enum: a member", {"type": ["string", "null"], "enum": ["a", "b", None]}, None),
    v("enum: not a member", {"type": "string", "enum": ["a", "b"]}, "c", [("", "enum")]),
    v("enum: 1.0 equals 1", {"type": "number", "enum": [1, 2]}, 1.0),
    v("enum: true is not 1", {"type": ["number", "boolean"], "enum": [1]}, True, [("", "enum")]),
    v("enum: numbers compare as binary64", {"type": "number", "enum": [0.1]}, instance_text="0.10000000000000000001",
      note="Both texts are the same binary64 value."),
    v("const: an exponent", {"type": "number", "const": 100}, instance_text="1E2"),
    v("const: -0 equals 0", {"type": "number", "const": 0}, instance_text="-0"),
    v("const: objects regardless of member order", {"type": "object", "const": {"a": [1, {"b": None}], "c": "x"}},
      {"c": "x", "a": [1.0, {"b": None}]}),
    v("const: an array's order matters", {"type": "array", "const": [1, 2]}, [2, 1], [("", "const")]),
    v("maximum: 2^53+1 is read as 2^53", {"type": "number", "maximum": 9007199254740992},
      instance_text="9007199254740993", note="9007199254740993 has no binary64 value; it reads as 9007199254740992."),
    v("required: the pointer is the missing member",
      obj({"a": S, "b": S}, required=["a", "b"]), {"a": "x"}, [("/b", "required")]),
    v("required: every missing member, sorted", obj({"b": S, "a": S, "c": S}, required=["c", "b", "a"]), {},
      [("/a", "required"), ("/b", "required"), ("/c", "required")]),
    v("properties: a nested failure", obj({"a": obj({"b": I})}), {"a": {"b": "x"}}, [("/a/b", "type")]),
    v("properties: an absent property is not checked", obj({"a": I}), {}),
    v("additionalProperties false: the extra member", obj({"a": S}, additionalProperties=False), {"a": "x", "z": 1},
      [("/z", "additionalProperties")]),
    v("additionalProperties false: names escaped as RFC 6901", obj({}, additionalProperties=False),
      {"a/b": 1, "m~n": 2}, [("/a~1b", "additionalProperties"), ("/m~0n", "additionalProperties")]),
    v("additionalProperties: a map's values", {"type": "object", "additionalProperties": S}, {"k": "v", "n": 1},
      [("/n", "type")]),
    v("open schema: an unknown member is valid (the tolerant reader)", obj({"a": S}), {"a": "x", "zzz": [1]}),
    v("items: each element", {"type": "array", "items": I}, [1, "x", 3, "y"], [("/1", "type"), ("/3", "type")]),
    v("items: pointers sort by code point, so /10 before /2", {"type": "array", "items": I},
      [0, 1, "x", 3, 4, 5, 6, 7, 8, 9, "y"], [("/10", "type"), ("/2", "type")], question="Q-VAL-1"),
    v("prefixItems with items false: the extra element",
      {"type": "array", "prefixItems": [S, I], "items": False}, ["a", 1, True], [("/2", "items")]),
    v("prefixItems: a shorter array is valid", {"type": "array", "prefixItems": [S, I], "items": False}, ["a"]),
    v("minItems", {"type": "array", "minItems": 2}, [1], [("", "minItems")]),
    v("maxItems", {"type": "array", "maxItems": 1}, [1, 2], [("", "maxItems")]),
    v("uniqueItems: 1 and 1.0 are equal", {"type": "array", "uniqueItems": True}, [1, 1.0], [("", "uniqueItems")]),
    v("uniqueItems: objects regardless of member order", {"type": "array", "uniqueItems": True},
      [{"a": 1, "b": 2}, {"b": 2, "a": 1}], [("", "uniqueItems")]),
    v("uniqueItems: \"1\" and 1 differ", {"type": "array", "uniqueItems": True}, ["1", 1]),
    v("minProperties", {"type": "object", "minProperties": 1}, {}, [("", "minProperties")]),
    v("maxProperties", {"type": "object", "maxProperties": 1}, {"a": 1, "b": 2}, [("", "maxProperties")]),
    v("minLength counts code points: U+1F600 is one", {"type": "string", "minLength": 2}, chr(0x1F600),
      [("", "minLength")]),
    v("maxLength: no normalisation, e and U+0301 are two", {"type": "string", "maxLength": 1}, "e" + chr(0x301),
      [("", "maxLength")]),
    v("maxLength: precomposed U+00E9 is one", {"type": "string", "maxLength": 1}, chr(0xE9)),
    v("pattern: matched against the whole value", {"type": "string", "pattern": "^[A-Z]{3}$"}, "USDX",
      [("", "pattern")]),
    v("pattern: . does not match LF", {"type": "string", "pattern": "^a.c$"}, "a\nc", [("", "pattern")]),
    v("pattern: . does not match CR", {"type": "string", "pattern": "^a.c$"}, "a\rc", [("", "pattern")]),
    v("pattern: . matches one code point", {"type": "string", "pattern": "^a.c$"}, "a" + chr(0x1F600) + "c"),
    v("pattern: a negated class matches LF", {"type": "string", "pattern": "^[^a]$"}, "\n"),
    v("pattern: $ inside R is a character", {"type": "string", "pattern": "^$[0-9]+$"}, "$100",
      note="In I-Regexp $ and ^ are ordinary characters; only the outer ^ and $ anchor (§9.2.4)."),
    v("pattern: \\p{Lu}", {"type": "string", "pattern": "^" + BS + "p{Lu}+$"}, chr(0xC0) + "B"),
    v("pattern: \\p{Lu} refuses lower case", {"type": "string", "pattern": "^" + BS + "p{Lu}+$"}, "ab",
      [("", "pattern")]),
    v("pattern: an escaped metacharacter", {"type": "string", "pattern": "^a" + BS + ".b$"}, "a.b"),
    v("pattern: case-sensitive", {"type": "string", "pattern": "^abc$"}, "ABC", [("", "pattern")]),
    v("pattern: only on strings", {"type": ["string", "number"], "pattern": "^a$"}, 5),
    v("format date: a leap day", {"type": "string", "format": "date"}, "2024-02-29"),
    v("format date: not a real date", {"type": "string", "format": "date"}, "2023-02-29", [("", "format")]),
    v("format date-time", {"type": "string", "format": "date-time"}, "2026-10-30T14:02:11.123456789Z"),
    v("format date-time: no offset", {"type": "string", "format": "date-time"}, "2026-10-30T14:02:11",
      [("", "format")]),
    v("format date-time: the leap second", {"type": "string", "format": "date-time"}, "2026-12-31T23:59:60Z",
      [("", "format")]),
    v("format duration: P alone", {"type": "string", "format": "duration"}, "P", [("", "format")]),
    v("format decimal: -0", {"type": "string", "format": "decimal"}, "-0", [("", "format")]),
    v("format int64: the largest", {"type": "string", "format": "int64"}, "9223372036854775807"),
    v("format int64: beyond it", {"type": "string", "format": "int64"}, "9223372036854775808", [("", "format")]),
    v("format uri: absolute, with a query and a fragment", {"type": "string", "format": "uri"},
      "https://atlas.example/a/b?c=d#e", question="Q-FMT-1"),
    v("format uri: relative", {"type": "string", "format": "uri"}, "/a/b", [("", "format")]),
    v("format uri: not ASCII", {"type": "string", "format": "uri"}, "https://ex" + chr(0xE4) + "mple.com/",
      [("", "format")]),
    v("format uuid: lower case", {"type": "string", "format": "uuid"}, "8e03978e-40d5-43e8-bc93-6894a57f9324"),
    v("format uuid: upper case", {"type": "string", "format": "uuid"}, "8E03978E-40D5-43E8-BC93-6894A57F9324",
      [("", "format")]),
    v("format capability-id", {"type": "string", "format": "capability-id"}, "portfolio.hold--ings",
      [("", "format")]),
    v("format exact-id", {"type": "string", "format": "exact-id"}, "portfolio.holdings@v1#3f9a1c0b7d2e4a65"),
    v("format address", {"type": "string", "format": "address"}, "portfolio.holdings@v1:fetch"),
    v("format: only on strings", {"type": ["string", "integer"], "format": "date"}, 20261031),
    v("x-atlas-logical Isin", {"type": "string", "x-atlas-logical": "Isin"}, "US0378331005"),
    v("x-atlas-logical Isin: lower case", {"type": "string", "x-atlas-logical": "Isin"}, "us0378331005",
      [("", "pattern")]),
    v("x-atlas-logical Isin: null when nullable", {"type": ["string", "null"], "x-atlas-logical": "Isin"}, None),
    v("x-atlas-logical Currency", {"type": "string", "x-atlas-logical": "Currency"}, "usd", [("", "pattern")]),
    v("x-atlas-logical Tenor", {"type": "string", "x-atlas-logical": "Tenor"}, "0M", [("", "pattern")]),
    v("x-atlas-logical Sedol: no vowels", {"type": "string", "x-atlas-logical": "Sedol"}, "B0YBKJ7"),
    v("x-atlas-logical Sedol: a vowel", {"type": "string", "x-atlas-logical": "Sedol"}, "A0YBKJ7", [("", "pattern")]),
    v("x-atlas-logical PortfolioCode: not empty", {"type": "string", "x-atlas-logical": "PortfolioCode"}, "",
      [("", "minLength")]),
    v("x-atlas-logical Principal: a uuid", {"type": "string", "x-atlas-logical": "Principal"}, "not-a-uuid",
      [("", "format")]),
    v("x-atlas-logical Uri", {"type": "string", "x-atlas-logical": "Uri"}, "mailto:a@b.example"),
    v("x-atlas-logical Bp: no constraint beyond its type", {"type": "number", "x-atlas-logical": "Bp"}, 1e9),
    v("x-atlas-logical Decimal(5,2): within precision and scale",
      {"type": "string", "x-atlas-logical": "Decimal(5,2)"}, "123.45"),
    v("x-atlas-logical Decimal(5,2): fewer fractional digits are within it",
      {"type": "string", "x-atlas-logical": "Decimal(5,2)"}, "123.4"),
    v("x-atlas-logical Decimal(5,2): too many integer digits",
      {"type": "string", "x-atlas-logical": "Decimal(5,2)"}, "1234.5", [("", "format")], question="Q-DEC-1"),
    v("x-atlas-logical Decimal(5,2): too many fractional digits",
      {"type": "string", "x-atlas-logical": "Decimal(5,2)"}, "1.234", [("", "format")]),
    v("x-atlas-logical Decimal(3,3): a leading 0 is not a digit",
      {"type": "string", "x-atlas-logical": "Decimal(3,3)"}, "0.123", question="Q-DEC-1"),
    v("x-atlas-logical Decimal(38,0): 38 digits",
      {"type": "string", "x-atlas-logical": "Decimal(38,0)"}, "9" * 38),
    v("x-atlas-logical Decimal(38,0): 39 digits",
      {"type": "string", "x-atlas-logical": "Decimal(38,0)"}, "9" * 39, [("", "format")]),
    v("x-atlas-logical Money", money, {"amount": "1250.500000", "currency": "USD"}),
    v("x-atlas-logical Money: an amount is a string", money, {"amount": 1250.5, "currency": "USD"},
      [("/amount", "type")]),
    v("x-atlas-logical Money: both required", money, {"amount": "1.000000"}, [("/currency", "required")]),
    v("x-atlas-logical Money: Decimal(38,6)", money, {"amount": "1.0000001", "currency": "USD"},
      [("/amount", "format")]),
    v("x-atlas-logical Money: a currency code", money, {"amount": "1", "currency": "usd"}, [("/currency", "pattern")]),
    v("x-atlas-logical Money: open outside a request", money, {"amount": "1", "currency": "USD", "fx": "spot"}),
    v("minimum", {"type": "number", "minimum": 0}, -0.5, [("", "minimum")]),
    v("exclusiveMinimum", {"type": "number", "exclusiveMinimum": 0}, 0, [("", "exclusiveMinimum")]),
    v("maximum", {"type": "number", "maximum": 1}, 1),
    v("exclusiveMaximum", {"type": "number", "exclusiveMaximum": 1}, 1, [("", "exclusiveMaximum")]),
    v("multipleOf: an integer multiple", {"type": "integer", "multipleOf": 5}, 10),
    v("multipleOf: 10.0 is a multiple of 5", {"type": "integer", "multipleOf": 5}, 10.0),
    v("multipleOf: not a multiple", {"type": "integer", "multipleOf": 5}, 12, [("", "multipleOf")]),
    v("anyOf: one failure, at the instance", {"anyOf": [S, I]}, True, [("", "anyOf")]),
    v("anyOf: one branch is enough", {"anyOf": [S, I]}, 3),
    v("oneOf: the discriminated branch",
      {"type": "object", "oneOf": [obj({"by": {"type": "string", "const": "id"}, "id": S}, required=["by", "id"]),
                                   obj({"by": {"type": "string", "const": "exact"}, "exact": S},
                                       required=["by", "exact"])]},
      {"by": "exact", "exact": "x"}),
    v("oneOf: no branch, one failure at the instance",
      {"type": "object", "oneOf": [obj({"by": {"type": "string", "const": "id"}, "id": S}, required=["by", "id"]),
                                   obj({"by": {"type": "string", "const": "exact"}, "exact": S},
                                       required=["by", "exact"])]},
      {"by": "id"}, [("", "oneOf")]),
    v("$ref: the referenced schema's keyword",
      {"type": "object", "properties": {"a": {"$ref": "#/$defs/code"}}, "$defs": {"code": {"type": "string",
                                                                                          "pattern": "^[a-z]+$"}}},
      {"a": "ABC"}, [("/a", "pattern")]),
    v("meta-data never fails: default, deprecated, readOnly, examples",
      obj({"a": {"type": "integer", "default": 5, "deprecated": True, "readOnly": True, "examples": [1]}}), {}),
    v("Atlas keywords never fail: a value missing from x-atlas-enum is still in enum",
      {"type": "string", "enum": ["a", "b", "UNKNOWN"], "x-atlas-enum": [{"value": "a"}, {"value": "UNKNOWN"}],
       "x-atlas-key": True, "x-atlas-unit": "count", "x-atlas-aliases": ["old"]}, "b"),
    v("many failures, sorted by pointer then keyword",
      obj({"z": {"type": "integer", "minimum": 10}, "a": obj({"b": S}, required=["c"]), "m": S},
          required=["m", "z"], additionalProperties=False),
      {"z": 5.5, "a": {"b": 1}, "q": None},
      [("/a/b", "type"), ("/a/c", "required"), ("/m", "required"), ("/q", "additionalProperties"),
       ("/z", "minimum"), ("/z", "type")]),
]


def p(name, schema, role="other", violation=None, note=None, question=None, wrap=True, free_form=()):
    """A profile case: accepted, or refused for exactly one (pointer, rule)."""
    body = root(copy.deepcopy(schema)) if wrap else copy.deepcopy(schema)
    case = {"name": name, "kind": "profile", "role": role, "schema": body}
    if violation is None:
        case["expected"] = {"in_profile": True}
    else:
        case["problem"] = {"code": "invalid_document", "target": violation[0]}
        case["rule"] = violation[1]
    if note:
        case["note"] = note
    if question:
        case["question"] = question
    case["_free_form"] = list(free_form)
    return case


def enum_r(values):
    return {"type": "string", "enum": values + ["UNKNOWN"],
            "x-atlas-enum": [{"value": x} for x in values] + [copy.deepcopy(E)]}


closed = lambda props, required=(): obj(props, required=required, additionalProperties=False)
holdings = profiles.holdings()
deep = {"type": "string"}
for _ in range(31):
    deep = {"type": "array", "items": deep}
deeper = {"type": "array", "items": copy.deepcopy(deep)}
PROFILE = [
    p("accepted: the data profile's request, as a request", holdings["schemas"]["request"], "request", wrap=False),
    p("accepted: the data profile's result", holdings["schemas"]["result"], wrap=False),
    p("accepted: a map in a request (labels)", closed({"labels": {"type": "object", "additionalProperties": S}}),
      "request", question="Q-PROF-7"),
    p("accepted: nesting of 32 levels", deep, question="Q-PROF-12"),
    p("accepted: items false after prefixItems", {"type": "array", "prefixItems": [S], "items": False}),
    p("accepted: a nullable key whose identifier may be missing",
      {"type": "array", "items": obj({"isin": {"type": ["string", "null"], "x-atlas-logical": "Isin",
                                              "x-atlas-key": True}})}, question="Q-PROF-6"),
    p("refused: no $schema", {"$vocabulary": copy.deepcopy(profiles.VOCABULARY), "type": "string"},
      violation=("/$schema", "$schema"), wrap=False),
    p("refused: $schema draft-07", {"$schema": "http://json-schema.org/draft-07/schema#",
                                    "$vocabulary": copy.deepcopy(profiles.VOCABULARY), "type": "string"},
      violation=("/$schema", "$schema"), wrap=False),
    p("refused: the Atlas vocabulary true", dict(root({"type": "string"}), **{"$vocabulary": dict(
        profiles.VOCABULARY, **{"https://atlas.jhg.com/vocab/2026-10": True})}),
      violation=("/$vocabulary", "$vocabulary"), wrap=False),
    p("refused: allOf", obj({"a": {"type": "string", "allOf": [S]}}), violation=("/properties/a/allOf", "allOf")),
    p("refused: not", obj({"a": {"type": "string", "not": I}}), violation=("/properties/a/not", "not")),
    p("refused: if", obj({"a": {"type": "string", "if": S}}), violation=("/properties/a/if", "if")),
    p("refused: patternProperties", {"type": "object", "patternProperties": {"^a": S}},
      violation=("/patternProperties", "patternProperties")),
    p("refused: propertyNames", {"type": "object", "propertyNames": {"type": "string"}},
      violation=("/propertyNames", "propertyNames")),
    p("refused: contains", {"type": "array", "contains": S}, violation=("/contains", "contains")),
    p("refused: unevaluatedProperties", {"type": "object", "unevaluatedProperties": True},
      violation=("/unevaluatedProperties", "unevaluatedProperties")),
    p("refused: dependentRequired", {"type": "object", "dependentRequired": {"a": ["b"]}},
      violation=("/dependentRequired", "dependentRequired")),
    p("refused: $anchor", obj({"a": {"type": "string", "$anchor": "x"}}), violation=("/properties/a/$anchor", "$anchor")),
    p("refused: $dynamicRef", obj({"a": {"type": "string", "$dynamicRef": "#x"}}),
      violation=("/properties/a/$dynamicRef", "$dynamicRef")),
    p("refused: an unknown keyword", {"type": "string", "x-foo": 1}, violation=("/x-foo", "x-foo")),
    p("refused: $ref to another document", obj({"a": {"$ref": "https://atlas.jhg.com/protocol/1.0/page.schema.json"}}),
      violation=("/properties/a/$ref", "$ref")),
    p("refused: $ref outside $defs", obj({"a": {"$ref": "#/properties/b"}, "b": S}),
      violation=("/properties/a/$ref", "$ref")),
    p("refused: $ref to a missing $defs entry", obj({"a": {"$ref": "#/$defs/none"}}),
      violation=("/properties/a/$ref", "$ref")),
    p("refused: a type beside $ref", dict(obj({"a": {"type": "string", "$ref": "#/$defs/c"}}), **{"$defs": {"c": S}}),
      violation=("/properties/a/type", "$ref"), question="Q-PROF-2"),
    p("refused: recursion through $ref", dict(obj({"n": {"$ref": "#/$defs/node"}}), **{"$defs": {
        "node": obj({"next": {"$ref": "#/$defs/node"}})}}), violation=("/$defs/node", "$ref"), question="Q-PROF-1"),
    p("refused: $id below the root", obj({"a": {"type": "string", "$id": "https://x.example/a"}}),
      violation=("/properties/a/$id", "$id")),
    p("refused: $defs below the root", obj({"a": {"type": "object", "$defs": {"b": S}}}),
      violation=("/properties/a/$defs", "$defs"), question="Q-PROF-1"),
    p("refused: a property with no type", obj({"a": {"description": "anything"}}),
      violation=("/properties/a/type", "type"), question="Q-PROF-4"),
    p("refused: null first in a type pair", obj({"a": {"type": ["null", "string"]}}),
      violation=("/properties/a/type", "type")),
    p("refused: two non-null types", obj({"a": {"type": ["string", "integer"]}}),
      violation=("/properties/a/type", "type")),
    p("refused: a three-element type", obj({"a": {"type": ["string", "integer", "null"]}}),
      violation=("/properties/a/type", "type")),
    p("refused: a boolean schema as a property", obj({"a": True}), violation=("/properties/a", "boolean-schema"),
      question="Q-PROF-3"),
    p("refused: oneOf without a const discriminator", obj({"a": {"type": "object", "oneOf": [
        obj({"x": S}, required=["x"]), obj({"y": I}, required=["y"])]}}), violation=("/properties/a/oneOf", "oneOf")),
    p("refused: oneOf whose discriminator is not required", obj({"a": {"type": "object", "oneOf": [
        obj({"k": {"type": "string", "const": "a"}}), obj({"k": {"type": "string", "const": "b"}})]}}),
      violation=("/properties/a/oneOf", "oneOf")),
    p("refused: multipleOf 0.5", obj({"a": {"type": "number", "multipleOf": 0.5}}),
      violation=("/properties/a/multipleOf", "multipleOf"), question="Q-PROF-8"),
    p("refused: multipleOf on a number", obj({"a": {"type": "number", "multipleOf": 5}}),
      violation=("/properties/a/multipleOf", "multipleOf"), question="Q-PROF-8"),
    p("refused: format email", obj({"a": {"type": "string", "format": "email"}}),
      violation=("/properties/a/format", "format")),
    p("refused: a pattern without ^ and $", obj({"a": {"type": "string", "pattern": "[A-Z]+"}}),
      violation=("/properties/a/pattern", "pattern")),
    p("refused: \\d is not I-Regexp", obj({"a": {"type": "string", "pattern": "^" + BS + "d+$"}}),
      violation=("/properties/a/pattern", "pattern")),
    p("refused: a lookahead", obj({"a": {"type": "string", "pattern": "^(?=a)a$"}}),
      violation=("/properties/a/pattern", "pattern")),
    p("refused: a backreference", obj({"a": {"type": "string", "pattern": "^(a)" + BS + "1$"}}),
      violation=("/properties/a/pattern", "pattern")),
    p("refused: an unescaped {", obj({"a": {"type": "string", "pattern": "^a{$"}}),
      violation=("/properties/a/pattern", "pattern")),
    p("refused: a count above 1000", obj({"a": {"type": "string", "pattern": "^a{1001}$"}}),
      violation=("/properties/a/pattern", "pattern"), question="Q-RE-1"),
    p("refused: \\p{Cn}", obj({"a": {"type": "string", "pattern": "^" + BS + "p{Cn}$"}}),
      violation=("/properties/a/pattern", "pattern"), question="Q-RE-1"),
    p("refused: nesting of 33 levels", deeper, violation=("/items" * 32, "depth"), question="Q-PROF-12"),
    p("refused: a request object left open", obj({"a": S}), "request",
      violation=("/additionalProperties", "additionalProperties")),
    p("refused: a nested request object left open", closed({"a": obj({"b": S})}), "request",
      violation=("/properties/a/additionalProperties", "additionalProperties")),
    p("refused: a result object closed", closed({"a": S}), violation=("/additionalProperties", "additionalProperties")),
    p("refused: a result enum without UNKNOWN", obj({"s": {"type": "string", "enum": ["a"],
                                                          "x-atlas-enum": [{"value": "a"}]}}),
      violation=("/properties/s/enum", "enum")),
    p("refused: a request enum with UNKNOWN", closed({"s": {"type": "string", "enum": ["a", "UNKNOWN"]}}), "request",
      violation=("/properties/s/enum", "enum")),
    p("refused: a result enum without x-atlas-enum", obj({"s": {"type": "string", "enum": ["a", "UNKNOWN"]}}),
      violation=("/properties/s/x-atlas-enum", "x-atlas-enum")),
    p("refused: x-atlas-enum missing a value", obj({"s": {"type": "string", "enum": ["a", "b", "UNKNOWN"],
                                                          "x-atlas-enum": [{"value": "a"}, copy.deepcopy(E)]}}),
      violation=("/properties/s/x-atlas-enum", "x-atlas-enum"), question="Q-PROF-10"),
    p("refused: an enum with a repeated value", obj({"s": {"type": "integer", "enum": [1, 1.0]}}),
      violation=("/properties/s/enum", "enum")),
    p("refused: a nullable key", {"type": "array", "items": obj({"k": {"type": ["string", "null"],
                                                                      "x-atlas-key": True}}, required=["k"])},
      violation=("/items/properties/k/x-atlas-key", "x-atlas-key")),
    p("refused: a key that is not required", {"type": "array", "items": obj({"k": {"type": "string",
                                                                                  "x-atlas-key": True}})},
      violation=("/items/properties/k/x-atlas-key", "x-atlas-key")),
    p("refused: a key in a request", closed({"k": {"type": "string", "x-atlas-key": True}}, required=["k"]), "request",
      violation=("/properties/k/x-atlas-key", "x-atlas-key")),
    p("refused: an unknown logical type", obj({"a": {"type": "string", "x-atlas-logical": "Duration"}}),
      violation=("/properties/a/x-atlas-logical", "x-atlas-logical")),
    p("refused: Decimal(39,0)", obj({"a": {"type": "string", "x-atlas-logical": "Decimal(39,0)"}}),
      violation=("/properties/a/x-atlas-logical", "x-atlas-logical")),
    p("refused: Decimal(5,6), a scale above its precision", obj({"a": {"type": "string",
                                                                       "x-atlas-logical": "Decimal(5,6)"}}),
      violation=("/properties/a/x-atlas-logical", "x-atlas-logical")),
    p("refused: Isin on a number", obj({"a": {"type": "number", "x-atlas-logical": "Isin"}}),
      violation=("/properties/a/x-atlas-logical", "x-atlas-logical")),
    p("refused: Money with a third property", obj({"m": {"type": "object", "x-atlas-logical": "Money", "properties": {
        "amount": S, "currency": S, "fx": S}}}), violation=("/properties/m/properties/fx", "x-atlas-logical"),
      question="Q-PROF-13"),
    p("refused: an unknown unit", obj({"a": {"type": "number", "x-atlas-unit": "kg"}}),
      violation=("/properties/a/x-atlas-unit", "x-atlas-unit")),
    p("refused: a unit on a string", obj({"a": {"type": "string", "x-atlas-unit": "count"}}),
      violation=("/properties/a/x-atlas-unit", "x-atlas-unit")),
    p("refused: an alias that is not snake_case", obj({"a": {"type": "string", "x-atlas-aliases": ["oldName"]}}),
      violation=("/properties/a/x-atlas-aliases", "x-atlas-aliases")),
    p("refused: a reference that is not pinned", obj({"a": {"type": "string", "x-atlas-reference": "portfolio.list"}}),
      violation=("/properties/a/x-atlas-reference", "x-atlas-reference")),
    p("refused: a deprecation without its sunset", obj({"a": {"type": "string", "x-atlas-deprecated": {
        "reason": "r", "since": "2026-01-01", "replaced_by": None}}}),
      violation=("/properties/a/x-atlas-deprecated", "x-atlas-deprecated"), question="Q-PROF-11"),
    p("refused: replaced_by a capability, not a property", obj({"a": {"type": "string", "x-atlas-deprecated": {
        "reason": "r", "since": "2026-01-01", "sunset": "2026-06-01", "replaced_by": "risk.duration@v1"}}}),
      violation=("/properties/a/x-atlas-deprecated", "x-atlas-deprecated")),
    p("refused: a property name that is not snake_case", obj({"marketValue": S}),
      violation=("/properties/marketValue", "properties")),
    p("refused: required names a property that is not there", obj({"a": S}, required=["a", "b"]),
      violation=("/required/1", "required"), question="Q-PROF-5"),
    p("refused: a default its own schema refuses", obj({"a": {"type": "integer", "default": "x"}}),
      violation=("/properties/a/default", "default"), question="Q-PROF-5"),
    p("accepted: compare's free-form request member (a built-in)",
      closed({"request": {"type": "object", "description": "free-form"}}), "request",
      free_form=("/properties/request",), question="Q-PROF-9"),
]


def meta_schema_verdict(schema):
    """Whether atlas-schema.schema.json alone accepts the schema (the profile's
    other rules are beyond what a meta-schema can say)."""
    from ..docvalidate import validator_for

    return "accepts" if not list(validator_for("atlas-schema.schema.json").iter_errors(schema)) else "refuses"


def build():
    from ..jsonio import loads

    cases = []
    for c in VALIDATE:
        instance = loads(c["instance_text"]) if "instance_text" in c else c["instance"]
        got = [{"pointer": a, "keyword": b} for a, b in validate(c["schema"], instance)]
        if got != c["expected"]["errors"]:
            raise AssertionError(f"validator.py disagrees with {c['name']!r}: {got}")
        cases.append(c)
    for c in PROFILE:
        free = c.pop("_free_form")
        got = check_schema(c["schema"], c["role"], free_form=free)
        want = [] if "expected" in c else [(c["problem"]["target"], c["rule"])]
        if got != want:
            raise AssertionError(f"profilecheck.py disagrees with {c['name']!r}: {got}")
        if free:
            c["free_form"] = free
        c["meta_schema"] = meta_schema_verdict(c["schema"])
        cases.append(c)
    names = [c["name"] for c in cases]
    assert len(names) == len(set(names))
    doc = {
        "description": "The Atlas Schema profile (§9.2). Cases of kind `validate` (AP-SCHEMA-001): a schema, an "
                       "instance (or `instance_text`, the instance's exact JSON text, where its spelling is the "
                       "point), and §9.2.5's result: valid, or every failure as {pointer, keyword}, sorted by "
                       "pointer then keyword by code points, each pair once; the problem for an invalid request is "
                       "invalid_request with these as details and the first one's pointer as target. Cases of kind "
                       "`profile`: a schema in a role (`request`: a schema an interaction takes; `other`: every "
                       "other) that the profile accepts, or refuses with invalid_document, target the pointer of "
                       "the offending member and `rule` the keyword or rule it breaks; each refused case breaks "
                       "exactly one rule; `meta_schema` says whether atlas-schema.schema.json alone refuses it (the "
                       "other rules are beyond a meta-schema). `free_form` lists the built-ins' free-form request members (Q-PROF-9). A "
                       "schema larger than 1 MiB is refused too (the size rule, tested in tools/spec's suite, not "
                       "here). Cases marked with a question follow QUESTIONS.md.",
        "cases": cases,
    }
    return {PATH: dumps(doc)}
