"""§9.2.5: validating an instance against a schema in the Atlas Schema profile.

The result is the list of failures, each `(pointer, keyword)`: the instance's
JSON Pointer (RFC 6901) and the keyword that failed. Conventions where the
text is silent (Q-VAL-1 in QUESTIONS.md):

- failures are sorted by pointer, then keyword, comparing code points, and
  repeated pairs are given once; the first is the `target` of
  `invalid_request`;
- `required` points at the missing member; `additionalProperties: false` and
  `items: false` at the member or element they refuse;
- `anyOf` and `oneOf` give one failure, at the instance, and nothing from
  inside their branches;
- `$ref` gives the failures of the schema it names;
- a logical type's constraint (§9.2.3) is reported under the keyword it is
  written as: `pattern`, `minLength`, `format`, `required`, `type`;
- numbers compare as binary64 values (I-JSON).
"""

from . import formats, iregexp
from .jsonio import as_binary64, equal, is_integer, is_number, json_type

BS = chr(92)

# §9.2.3: each logical type's constraint, as the keywords it is written as.
_PATTERNS = {
    "Currency": "^[A-Z]{3}$",
    "Country": "^[A-Z]{2}$",
    "Isin": "^[A-Z]{2}[A-Z0-9]{9}[0-9]$",
    "Cusip": "^[A-Z0-9]{8}[0-9]$",
    "Sedol": "^[B-DF-HJ-NP-TV-Z0-9]{6}[0-9]$",
    "Figi": "^BBG[B-DF-HJ-NP-TV-Z0-9]{8}[0-9]$",
    "Lei": "^[A-Z0-9]{18}[0-9]{2}$",
    "Tenor": "^[1-9][0-9]*[DWMY]$",
}
LOGICAL_BASE = {
    "Bp": "number", "Percent": "number", "Fraction": "number", "Years": "number",
    "Money": "object",
    "Currency": "string", "Country": "string", "Isin": "string", "Cusip": "string", "Sedol": "string",
    "Figi": "string", "Lei": "string", "Tenor": "string", "PortfolioCode": "string", "IndexCode": "string",
    "Path": "string", "Principal": "string", "Uri": "string",
}
MONEY = {
    "type": "object",
    "required": ["amount", "currency"],
    "properties": {
        "amount": {"type": "string", "format": "decimal", "x-atlas-logical": "Decimal(38,6)"},
        "currency": {"type": "string", "x-atlas-logical": "Currency"},
    },
}


def logical_base(name):
    if formats.parse_decimal_logical(name):
        return "string"
    return LOGICAL_BASE.get(name)


def logical_constraint(name):
    """The keywords a logical type's constraint is applied as (§9.2.5 step 2),
    without its base type: a nullable property keeps its `null`."""
    if formats.parse_decimal_logical(name):
        return {"format": "decimal"}
    if name in _PATTERNS:
        return {"pattern": _PATTERNS[name]}
    if name in ("PortfolioCode", "IndexCode", "Path"):
        return {"minLength": 1}
    if name == "Principal":
        return {"format": "uuid"}
    if name == "Uri":
        return {"format": "uri"}
    if name == "Money":
        return {"required": MONEY["required"], "properties": MONEY["properties"]}
    return {}


def escape(token):
    return token.replace("~", "~0").replace("/", "~1")


def pointer(path):
    return "".join("/" + escape(str(t)) for t in path)


def _code_points(s):
    return [ord(c) for c in s]


def sort_failures(failures):
    unique = {(p, k) for p, k in failures}
    return sorted(unique, key=lambda f: (_code_points(f[0]), _code_points(f[1])))


def _type_ok(t, v):
    actual = json_type(v)
    if t == "integer":
        return is_integer(v)
    return actual == t


def _unique(items):
    for i in range(len(items)):
        for j in range(i + 1, len(items)):
            if equal(items[i], items[j]):
                return False
    return True


class Validator:
    def __init__(self, root):
        self.root = root

    def resolve(self, ref):
        if not (isinstance(ref, str) and ref.startswith("#/$defs/")):
            raise ValueError(f"$ref {ref!r} is outside the profile")
        name = ref[len("#/$defs/") :].replace("~1", "/").replace("~0", "~")
        return self.root["$defs"][name]

    def errors(self, instance):
        out = []
        self._validate(self.root, instance, [], out, 0)
        return sort_failures(out)

    def valid(self, schema, instance):
        out = []
        self._validate(schema, instance, [], out, 0)
        return not out

    def _validate(self, schema, v, path, out, depth):
        if depth > 200:
            raise RecursionError("schema recursion too deep")
        if schema is True:
            return
        if schema is False:
            out.append((pointer(path), "false"))
            return
        logical = schema.get("x-atlas-logical")
        if not isinstance(logical, str):
            logical = None
        for k, c in schema.items():
            self._apply(k, c, schema, v, path, out, depth, logical)
        if logical:
            implied = logical_constraint(logical)
            for k, c in implied.items():
                self._apply(k, c, implied, v, path, out, depth, logical)

    def _apply(self, k, c, s, v, path, out, depth, logical):
        here = pointer(path)
        t = json_type(v)
        if k == "type":
            types = c if isinstance(c, list) else [c]
            if not any(_type_ok(x, v) for x in types):
                out.append((here, "type"))
        elif k == "enum":
            if not any(equal(v, e) for e in c):
                out.append((here, "enum"))
        elif k == "const":
            if not equal(v, c):
                out.append((here, "const"))
        elif k == "$ref":
            self._validate(self.resolve(c), v, path, out, depth + 1)
        elif k == "anyOf":
            if not any(self.valid_at(b, v, depth) for b in c):
                out.append((here, "anyOf"))
        elif k == "oneOf":
            if sum(1 for b in c if self.valid_at(b, v, depth)) != 1:
                out.append((here, "oneOf"))
        elif t == "object":
            if k == "required":
                out.extend((pointer(path + [n]), "required") for n in c if n not in v)
            elif k == "properties":
                for n, sub in c.items():
                    if n in v:
                        self._validate(sub, v[n], path + [n], out, depth + 1)
            elif k == "additionalProperties":
                props = s.get("properties", {})
                if isinstance(logical, str) and logical == "Money":
                    props = {**MONEY["properties"], **props}
                for n in v:
                    if n not in props:
                        if c is False:
                            out.append((pointer(path + [n]), "additionalProperties"))
                        elif c is not True:
                            self._validate(c, v[n], path + [n], out, depth + 1)
            elif k == "minProperties" and len(v) < c:
                out.append((here, k))
            elif k == "maxProperties" and len(v) > c:
                out.append((here, k))
        elif t == "array":
            if k == "prefixItems":
                for i, sub in enumerate(c[: len(v)]):
                    self._validate(sub, v[i], path + [i], out, depth + 1)
            elif k == "items":
                start = len(s.get("prefixItems", []))
                for i in range(start, len(v)):
                    if c is False:
                        out.append((pointer(path + [i]), "items"))
                    else:
                        self._validate(c, v[i], path + [i], out, depth + 1)
            elif k == "minItems" and len(v) < c:
                out.append((here, k))
            elif k == "maxItems" and len(v) > c:
                out.append((here, k))
            elif k == "uniqueItems" and c is True and not _unique(v):
                out.append((here, k))
        elif t == "string":
            if k == "minLength" and len(v) < c:
                out.append((here, k))
            elif k == "maxLength" and len(v) > c:
                out.append((here, k))
            elif k == "pattern" and not iregexp.matches(c, v):
                out.append((here, k))
            elif k == "format" and not formats.check(c, v, logical):
                out.append((here, k))
        elif t == "number":
            x = as_binary64(v)
            if k == "minimum" and not x >= as_binary64(c):
                out.append((here, k))
            elif k == "maximum" and not x <= as_binary64(c):
                out.append((here, k))
            elif k == "exclusiveMinimum" and not x > as_binary64(c):
                out.append((here, k))
            elif k == "exclusiveMaximum" and not x < as_binary64(c):
                out.append((here, k))
            elif k == "multipleOf":
                if not (x.is_integer() and int(x) % int(c) == 0):
                    out.append((here, k))

    def valid_at(self, schema, v, depth):
        out = []
        self._validate(schema, v, [], out, depth + 1)
        return not out


def validate(schema, instance):
    """The sorted failures of `instance` against `schema` (a root schema)."""
    return Validator(schema).errors(instance)


def is_valid(schema, instance, root=None):
    v = Validator(root if root is not None else schema)
    return v.valid(schema, instance)


__all__ = ["validate", "is_valid", "Validator", "logical_constraint", "logical_base", "MONEY", "is_number"]
