"""§9.5: resolving a record written under one profile for a reader that knows
another, row by row (§9.5.2–§9.5.5) and column by column (§9.5.7).

A chain is a list of steps, oldest first: {"profile": exact-id, "schema": the
reader-relevant record schema of that profile, "lens": its lens from the step
before}. Readings where the text is silent are named Q-RES-n (QUESTIONS.md).
"""

import copy
import decimal

from . import formats, grammar
from .jsonio import as_binary64, equal, is_integer, is_number
from .validator import Validator, escape


class ResolutionFailed(Exception):
    """resolution_failed, with the target pointer where §9.5 gives one."""

    def __init__(self, target=None, why=""):
        super().__init__(why or "resolution_failed")
        self.target = target

    def problem(self):
        p = {"code": "resolution_failed"}
        if self.target is not None:
            p["target"] = self.target
        return p


# §9.5.4: the unit table and the only conversions that exist.
UNIT_ALIASES = {"Bp": "bp", "Percent": "percent", "Fraction": "fraction", "Years": "years"}
CONVERSIONS = {
    ("fraction", "percent"): ("mul", 100),
    ("percent", "fraction"): ("div", 100),
    ("fraction", "bp"): ("mul", 10000),
    ("bp", "fraction"): ("div", 10000),
    ("percent", "bp"): ("mul", 100),
    ("bp", "percent"): ("div", 100),
    ("days", "seconds"): ("mul", 86400),
    ("seconds", "days"): ("div", 86400),
}
PROMOTIONS = {("integer", "number"), ("date", "date-time")}
OPS = ("rename", "alias_value", "convert_unit", "promote", "default", "drop")


def unit(name):
    return UNIT_ALIASES.get(name, name)


def pinned(exact):
    return exact.split("#", 1)[0]


def _ptr(name):
    return "/" + escape(name)


def _types(s):
    t = s.get("type")
    return [t] if isinstance(t, str) else list(t or [])


def _deref(schema, root):
    seen = 0
    while isinstance(schema, dict) and "$ref" in schema and seen < 64:
        schema = Validator(root).resolve(schema["$ref"])
        seen += 1
    return schema


def _decimal_scale(prop, root):
    """The scale s of the Decimal(p,s) a property (or its items) holds, else None."""
    s = _deref(prop, root)
    if not isinstance(s, dict):
        return None
    if "array" in _types(s) and isinstance(s.get("items"), dict):
        s = _deref(s["items"], root)
    ps = formats.parse_decimal_logical(s.get("x-atlas-logical")) if isinstance(s, dict) else None
    return ps[1] if ps else None


# --- §9.5.3, value operations -------------------------------------------------


def _each(value, fn):
    """A value operation applies to each element of an array; null passes."""
    if value is None:
        return None
    if isinstance(value, list):
        return [_each(v, fn) for v in value]
    return fn(value)


def convert_number(value, src, dst, scale, target):
    """§9.5.4: exactly one multiplication or one division, as written; on a
    Decimal(p,s) string, exact arithmetic rounded to the reader's scale, ties to
    even."""
    key = (unit(src), unit(dst))
    if key not in CONVERSIONS:
        raise ResolutionFailed(target, f"no conversion from {src} to {dst}")
    op, factor = CONVERSIONS[key]

    def one(v):
        if is_number(v):
            x = as_binary64(v)
            return x * float(factor) if op == "mul" else x / float(factor)
        if isinstance(v, str) and grammar.matches("decimal", v):
            if scale is None:
                raise ResolutionFailed(target, "a decimal with no Decimal(p,s) to round to")
            with decimal.localcontext() as ctx:
                ctx.prec = 200
                ctx.rounding = decimal.ROUND_HALF_EVEN
                d = decimal.Decimal(v)
                r = d * factor if op == "mul" else d / factor
                q = r.quantize(decimal.Decimal(1).scaleb(-scale), rounding=decimal.ROUND_HALF_EVEN)
            text = format(q, "f")
            if text.startswith("-") and set(text[1:]) <= {"0", "."}:
                text = text[1:]  # §9.1.5: no negative zero
            return text
        raise ResolutionFailed(target, "not a number")

    return _each(value, one)


def promote_forward(value, src, dst, target):
    if (src, dst) not in PROMOTIONS:
        raise ResolutionFailed(target, f"no promotion from {src} to {dst}")

    def one(v):
        if src == "integer":
            if not is_number(v):
                raise ResolutionFailed(target, "not a number")
            return v
        if isinstance(v, str) and grammar.matches("date", v):
            return v + "T00:00:00Z"
        if isinstance(v, str) and grammar.matches("date-time", v):
            return v
        raise ResolutionFailed(target, "not a date")

    return _each(value, one)


def promote_backward(value, src, dst, target):
    """From `dst` back to `src`, only if exact."""
    if (src, dst) not in PROMOTIONS:
        raise ResolutionFailed(target, f"no promotion from {src} to {dst}")

    def one(v):
        if src == "integer":
            if is_integer(v):
                return int(as_binary64(v))
            raise ResolutionFailed(target, "not exact as an integer")
        if isinstance(v, str) and grammar.matches("date", v):
            return v
        ns = grammar.parse_date_time_ns(v) if isinstance(v, str) else None
        if ns is None or ns % (86400 * 10**9) != 0:
            raise ResolutionFailed(target, "not exact as a date")
        return grammar.format_date(ns // (86400 * 10**9))  # Q-RES-3: midnight UTC only

    return _each(value, one)


def apply_lens(y, lens, forward, into_schema, root):
    """Applies one profile's lens to record y (a dict, changed in place):
    forward in the list's order, backward in reverse."""
    ops = lens if forward else list(reversed(lens))
    for op in ops:
        kind = op.get("op")
        if kind not in OPS:
            raise ResolutionFailed(None, f"lens operation {kind!r} unknown to this reader")
        if kind == "rename":
            a, b = (op["from"], op["to"]) if forward else (op["to"], op["from"])
            if a in y:
                y[b] = y.pop(a)
            continue
        field = op["field"]
        if kind == "default":
            if forward and field not in y:
                y[field] = copy.deepcopy(op["value"])
            elif not forward:
                y.pop(field, None)
            continue
        if kind == "drop":
            if forward:
                y.pop(field, None)
            continue
        if field not in y:
            continue
        target = _ptr(field)
        if kind == "alias_value":
            a, b = (op["from"], op["to"]) if forward else (op["to"], op["from"])
            y[field] = _each(y[field], lambda v: copy.deepcopy(b) if equal(v, a) else v)
        elif kind == "convert_unit":
            src, dst = (op["from"], op["to"]) if forward else (op["to"], op["from"])
            prop = (into_schema.get("properties") or {}).get(field, {})
            y[field] = convert_number(y[field], src, dst, _decimal_scale(prop, root), target)  # Q-RES-2
        elif kind == "promote":
            if forward:
                y[field] = promote_forward(y[field], op["from"], op["to"], target)
            else:
                y[field] = promote_backward(y[field], op["from"], op["to"], target)


# --- §9.5.5, converting a value to the reader's type ------------------------


def convert(v, schema, root, target):
    s = _deref(schema, root)
    if s is True or s == {}:
        return v
    val = Validator(root)
    if val.valid(schema, v):
        return v  # row 1: unchanged
    types = _types(s) if isinstance(s, dict) else []
    if is_integer(v) and "number" in types:
        return v  # row 2
    if isinstance(v, str) and grammar.matches("date", v) and s.get("format") == "date-time":
        return v + "T00:00:00Z"  # row 4
    if isinstance(v, str) and isinstance(s.get("enum"), list) and not any(equal(v, e) for e in s["enum"]):
        for entry in s.get("x-atlas-enum") or []:  # row 5
            if v in (entry.get("aliases") or []):
                return entry["value"]
        if "UNKNOWN" in s["enum"]:
            return "UNKNOWN"
        raise ResolutionFailed(target, "not in the reader's enum")
    if isinstance(v, dict) and "object" in types and isinstance(s.get("properties"), dict):
        return match(v, s, root, target_prefix=target)  # row 6: no lenses
    if isinstance(v, list) and "array" in types:
        items = s.get("items", True)
        return [convert(e, items, root, target) for e in v]  # row 7
    if v is None and "null" in types:
        return None  # row 8
    raise ResolutionFailed(target, "cannot be converted")


def project(schema, columns):
    """§9.5.2 step 5: the schema kept to the properties `columns` names."""
    if columns is None:
        return schema
    s = dict(schema)
    s["properties"] = {k: v for k, v in (schema.get("properties") or {}).items() if k in columns}
    if "required" in schema:
        s["required"] = [n for n in schema["required"] if n in columns]
    return s


def match(y, s, root, target_prefix=None):
    """§9.5.2 steps 5 to 15 against schema s (already projected)."""
    z = {}
    required = s.get("required") or []
    for p, ps in (s.get("properties") or {}).items():
        target = target_prefix if target_prefix is not None else _ptr(p)  # Q-RES-1: the top-level member
        prop = _deref(ps, root)
        if p in y:  # 6
            value = y[p]
        else:
            aliases = ps.get("x-atlas-aliases") or (prop.get("x-atlas-aliases") if isinstance(prop, dict) else None) or []
            present = [a for a in aliases if a in y]
            if present:  # 7: the last such alias in the list
                value = y[present[-1]]
            elif isinstance(ps, dict) and "default" in ps:  # 8
                value = copy.deepcopy(ps["default"])
            elif isinstance(prop, dict) and "null" in _types(prop) and p in required:  # 9
                value = None
            elif p not in required:  # 10
                continue
            else:  # 11
                raise ResolutionFailed(target, f"required {p} is missing")
        z[p] = convert(value, ps, root, target)  # 12, 13
    return z  # 14, 15


def resolve(record, writer, reader, chain, columns=None, dynamic=False):
    """§9.5.2: record x, written under `writer`, for `reader` (exact-ids)."""
    if writer == reader or dynamic:  # 1
        return copy.deepcopy(record)
    if pinned(writer) != pinned(reader):  # 2
        raise ResolutionFailed(None, "writer and reader are not profiles of one id@vN")
    index = {step["profile"]: i for i, step in enumerate(chain)}
    if writer not in index or reader not in index:
        raise ResolutionFailed(None, "the chain does not hold the writer and the reader")
    iw, ir = index[writer], index[reader]
    y = copy.deepcopy(record)  # 3
    if iw > ir:  # 4: the writer is newer: backward, from n down to 1
        for k in range(iw, ir, -1):
            apply_lens(y, chain[k].get("lens") or [], False, chain[k - 1]["schema"], chain[k - 1]["schema"])
    else:  # the writer is older: forward, from 1 up to n
        for k in range(iw + 1, ir + 1):
            apply_lens(y, chain[k].get("lens") or [], True, chain[k]["schema"], chain[k]["schema"])
    rs = chain[ir]["schema"]
    return match(y, project(rs, columns), rs)


# --- §9.5.7, column by column -----------------------------------------------


def resolve_columns(table, writer, reader, chain, columns=None, dynamic=False):
    """Resolves a table held as columns {name: [values]} (every row has every
    column, as in an Arrow batch), applying each lens operation to whole
    columns. §9.5.7: the result MUST equal resolving each row by §9.5.2."""
    n = len(next(iter(table.values()))) if table else 0
    if writer == reader or dynamic:
        return copy.deepcopy(table)
    if pinned(writer) != pinned(reader):
        raise ResolutionFailed(None, "writer and reader are not profiles of one id@vN")
    index = {step["profile"]: i for i, step in enumerate(chain)}
    iw, ir = index[writer], index[reader]
    cols = copy.deepcopy(table)
    steps = range(iw, ir, -1) if iw > ir else range(iw + 1, ir + 1)
    for k in steps:
        forward = iw < ir
        lens = chain[k].get("lens") or []
        into = chain[k]["schema"] if forward else chain[k - 1]["schema"]
        for op in lens if forward else list(reversed(lens)):
            kind = op.get("op")
            if kind not in OPS:
                raise ResolutionFailed(None, f"lens operation {kind!r} unknown to this reader")
            if kind == "rename":
                a, b = (op["from"], op["to"]) if forward else (op["to"], op["from"])
                if a in cols:
                    cols[b] = cols.pop(a)  # a rename renames the column
                continue
            field = op["field"]
            if kind == "default":
                if forward and field not in cols:
                    cols[field] = [copy.deepcopy(op["value"]) for _ in range(n)]
                elif not forward:
                    cols.pop(field, None)
                continue
            if kind == "drop":
                if forward:
                    cols.pop(field, None)
                continue
            if field not in cols:
                continue
            target = _ptr(field)
            if kind == "alias_value":
                a, b = (op["from"], op["to"]) if forward else (op["to"], op["from"])
                cols[field] = [_each(v, lambda x: copy.deepcopy(b) if equal(x, a) else x) for v in cols[field]]
            elif kind == "convert_unit":
                src, dst = (op["from"], op["to"]) if forward else (op["to"], op["from"])
                prop = (into.get("properties") or {}).get(field, {})
                # one vectorised operation over the column
                cols[field] = convert_number(cols[field], src, dst, _decimal_scale(prop, into), target)
            elif kind == "promote":
                fn = promote_forward if forward else promote_backward
                cols[field] = fn(cols[field], op["from"], op["to"], target)
    rs = chain[ir]["schema"]
    s = project(rs, columns)
    required = s.get("required") or []
    out = {}
    for p, ps in (s.get("properties") or {}).items():
        target = _ptr(p)
        prop = _deref(ps, rs)
        if p in cols:
            col = cols[p]
        else:
            aliases = [a for a in (ps.get("x-atlas-aliases") or []) if a in cols]
            if aliases:
                col = cols[aliases[-1]]
            elif "default" in ps:
                col = [copy.deepcopy(ps["default"]) for _ in range(n)]
            elif "null" in _types(prop) and p in required:
                col = [None] * n
            elif p not in required:
                continue
            else:
                raise ResolutionFailed(target, f"required {p} is missing")
        out[p] = [convert(v, ps, rs, target) for v in col]
    return out


def rows_to_columns(rows):
    names = list(rows[0]) if rows else []
    return {k: [r[k] for r in rows] for k in names}


def columns_to_rows(cols):
    n = len(next(iter(cols.values()))) if cols else 0
    return [{k: cols[k][i] for k in cols} for i in range(n)]
