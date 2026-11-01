"""§9.2.7: a table schema's Arrow schema, derived, never written by hand.

The derivation yields the Arrow schema in the JSON form of Apache Arrow's
integration tests (format/integration_json.rst: fields with name, nullable,
type, children, metadata as key/value pairs), which every Arrow
implementation can read, and as a pyarrow schema built from that form.

Readings where the table is silent are named Q-ARROW-n (QUESTIONS.md):
which row applies when several could (a logical type first, then a format,
then the JSON type); strings with any other format, and every string logical
type, are Utf8; a property the table cannot map (an object without
properties, an array without items, anyOf, oneOf) has no Arrow type, and the
schema is refused; a list's child is named `item`; field metadata values are
the keyword's JSON text in RFC 8785 form, schema metadata values plain
strings.
"""

import pyarrow as pa

from . import formats, jcs
from .validator import Validator

ATLAS_KEYWORDS = ("x-atlas-logical", "x-atlas-key", "x-atlas-aliases", "x-atlas-reference", "x-atlas-deprecated",
                  "x-atlas-unit", "x-atlas-enum")


class NotDerivable(ValueError):
    def __init__(self, pointer, why):
        super().__init__(why)
        self.pointer = pointer


def _types(s):
    t = s.get("type")
    return [t] if isinstance(t, str) else list(t or [])


def _metadata(s):
    """Every Atlas keyword on the property, as atlas:<keyword without x-atlas->."""
    return [{"key": "atlas:" + k[len("x-atlas-"):], "value": jcs.canonical(s[k])} for k in ATLAS_KEYWORDS if k in s]


def _type(s, root, ptr):
    """The Arrow type (integration JSON) and children of one property schema."""
    s = _deref(s, root)
    if any(k in s for k in ("anyOf", "oneOf")):
        raise NotDerivable(ptr, "anyOf and oneOf have no Arrow type (Q-ARROW-3)")
    base = [t for t in _types(s) if t != "null"]
    if len(base) != 1:
        raise NotDerivable(ptr + "/type", "no single JSON type")
    base = base[0]
    logical = s.get("x-atlas-logical")
    ps = formats.parse_decimal_logical(logical) if isinstance(logical, str) else None
    if ps:
        return {"name": "decimal", "precision": ps[0], "scale": ps[1], "bitWidth": 128}, []
    if logical == "Money":
        return {"name": "struct"}, [
            field("amount", {"name": "decimal", "precision": 38, "scale": 6, "bitWidth": 128}, False, [], []),
            field("currency", {"name": "utf8"}, False, [], []),
        ]
    fmt = s.get("format")
    if base == "string":
        if fmt == "date":
            return {"name": "date", "unit": "DAY"}, []
        if fmt == "date-time":
            return {"name": "timestamp", "unit": "MICROSECOND", "timezone": "UTC"}, []
        if fmt == "duration":
            return {"name": "duration", "unit": "MICROSECOND"}, []
        if fmt == "int64":
            return {"name": "int", "bitWidth": 64, "isSigned": True}, []
        return {"name": "utf8"}, []  # no format, any other format, any string logical type (Q-ARROW-2)
    if base == "boolean":
        return {"name": "bool"}, []
    if base == "integer":
        return {"name": "int", "bitWidth": 64, "isSigned": True}, []
    if base == "number":
        return {"name": "floatingpoint", "precision": "DOUBLE"}, []
    if base == "array":
        items = s.get("items")
        if not isinstance(items, dict):
            raise NotDerivable(ptr + "/items", "an array without an items schema has no Arrow type (Q-ARROW-3)")
        it = _deref(items, root)
        t, children = _type(items, root, ptr + "/items")
        return {"name": "list"}, [field("item", t, "null" in _types(it), children, _metadata(it))]
    if base == "object":
        props = s.get("properties")
        if not isinstance(props, dict):
            raise NotDerivable(ptr, "an object without properties has no Arrow type (Q-ARROW-3)")
        return {"name": "struct"}, struct_fields(s, root, ptr)
    raise NotDerivable(ptr, f"no Arrow type for {base}")


def _deref(s, root):
    n = 0
    while isinstance(s, dict) and "$ref" in s and n < 64:
        target = Validator(root).resolve(s["$ref"])
        merged = dict(target)
        merged.update({k: v for k, v in s.items() if k != "$ref"})
        s = merged
        n += 1
    return s


def field(name, type_, nullable, children, metadata):
    return {"name": name, "nullable": nullable, "type": type_, "children": children, "metadata": metadata}


def struct_fields(s, root, ptr):
    required = s.get("required") or []
    out = []
    for name, ps in s["properties"].items():
        prop = _deref(ps, root)
        t, children = _type(ps, root, ptr + "/properties/" + name)
        nullable = "null" in _types(prop) or name not in required
        out.append(field(name, t, nullable, children, _metadata(prop)))
    return out


def derive(result_schema, exact_id, protocol="atlas/1.0"):
    """The Arrow schema of a table result's schema (an array of row objects),
    in document order of `properties` (Q-ARROW-1)."""
    if "array" not in _types(result_schema) or not isinstance(result_schema.get("items"), dict):
        raise NotDerivable("", "a table result's schema is an array of row objects")
    row = _deref(result_schema["items"], result_schema)
    if "object" not in _types(row) or not isinstance(row.get("properties"), dict):
        raise NotDerivable("/items", "a row is an object with properties")
    return {
        "fields": struct_fields(row, result_schema, "/items"),
        "metadata": [{"key": "atlas:profile", "value": exact_id}, {"key": "atlas:protocol", "value": protocol}],
    }


# --- the same schema in pyarrow ------------------------------------------------


def _pa_type(t, children):
    n = t["name"]
    if n == "bool":
        return pa.bool_()
    if n == "int":
        return pa.int64()
    if n == "floatingpoint":
        return pa.float64()
    if n == "utf8":
        return pa.utf8()
    if n == "date":
        return pa.date32()
    if n == "timestamp":
        return pa.timestamp("us", tz="UTC")
    if n == "duration":
        return pa.duration("us")
    if n == "decimal":
        return pa.decimal128(t["precision"], t["scale"])
    if n == "list":
        return pa.list_(to_pa_field(children[0]))
    if n == "struct":
        return pa.struct([to_pa_field(c) for c in children])
    raise ValueError(n)


def to_pa_field(f):
    md = {m["key"]: m["value"] for m in f["metadata"]} or None
    return pa.field(f["name"], _pa_type(f["type"], f["children"]), nullable=f["nullable"], metadata=md)


def to_pa_schema(derived):
    md = {m["key"]: m["value"] for m in derived["metadata"]}
    return pa.schema([to_pa_field(f) for f in derived["fields"]], metadata=md)


def from_pa_field(f):
    """A pyarrow field back in the integration JSON form (the round trip `check` uses)."""
    t = f.type
    children = []
    if pa.types.is_boolean(t):
        jt = {"name": "bool"}
    elif pa.types.is_int64(t):
        jt = {"name": "int", "bitWidth": 64, "isSigned": True}
    elif pa.types.is_float64(t):
        jt = {"name": "floatingpoint", "precision": "DOUBLE"}
    elif pa.types.is_string(t):
        jt = {"name": "utf8"}
    elif pa.types.is_date32(t):
        jt = {"name": "date", "unit": "DAY"}
    elif pa.types.is_timestamp(t):
        jt = {"name": "timestamp", "unit": {"us": "MICROSECOND"}[t.unit], "timezone": t.tz}
    elif pa.types.is_duration(t):
        jt = {"name": "duration", "unit": {"us": "MICROSECOND"}[t.unit]}
    elif pa.types.is_decimal(t):
        jt = {"name": "decimal", "precision": t.precision, "scale": t.scale, "bitWidth": 128}
    elif pa.types.is_list(t):
        jt = {"name": "list"}
        children = [from_pa_field(t.value_field)]
    elif pa.types.is_struct(t):
        jt = {"name": "struct"}
        children = [from_pa_field(t.field(i)) for i in range(t.num_fields)]
    else:
        raise ValueError(t)
    md = [{"key": k.decode(), "value": v.decode()} for k, v in (f.metadata or {}).items()]
    return field(f.name, jt, f.nullable, children, md)
