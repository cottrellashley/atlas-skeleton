"""`snowflake-arrow/1`: Snowflake's result chunks, in the encoding ASSUMED here
until chunks are captured from Development (§9.8.2), written with pyarrow, and
a reference decoder into the writer profile's Arrow schema (§9.2.7).

The assumed encoding (QUESTIONS.md, Q-SNOW-1): each chunk is a complete Arrow
IPC stream with its own schema; each field carries the metadata `logicalType`,
`precision` and `scale` (strings), named as Snowflake's drivers name them:

- FIXED (NUMBER(p,s)): p <= 18, the narrowest of Int8/16/32/64 that holds
  every value of that chunk, holding value x 10^s; p > 18, Decimal128(p,s).
- REAL: Float64. TEXT: Utf8. BOOLEAN: Bool. DATE: Date32.
- TIMESTAMP_NTZ, TIMESTAMP_LTZ: scale <= 7, Int64 of epoch seconds x 10^scale;
  scale > 7, Struct{epoch: Int64 seconds, fraction: Int32 nanoseconds}.
- TIMESTAMP_TZ: scale <= 3, Struct{epoch: Int64 x 10^scale, timezone: Int32 =
  offset minutes + 1440}; scale > 3, Struct{epoch: Int64 seconds, fraction:
  Int32 nanoseconds, timezone: Int32}.

Epochs are UTC; a negative instant's epoch is floored and its fraction is
non-negative (Q-SNOW-2). Decoding truncates to microseconds by flooring, so
the written fraction digits are cut, never rounded (§9.1.4, Q-SNOW-3).
"""

import decimal
import io

import pyarrow as pa
import pyarrow.ipc as ipc

from . import arrowmap, grammar

FORMAT = "snowflake-arrow/1"


class Mismatch(ValueError):
    """The chunk does not decode into the writer profile's schema:
    invalid_document (§9.7.4 step 10)."""


# --- writing chunks (the generator's side) -----------------------------------


def _narrow_int(values):
    nums = [v for v in values if v is not None]
    for t, lo, hi in ((pa.int8(), -2**7, 2**7 - 1), (pa.int16(), -2**15, 2**15 - 1),
                      (pa.int32(), -2**31, 2**31 - 1), (pa.int64(), -2**63, 2**63 - 1)):
        if all(lo <= v <= hi for v in nums):
            return t
    raise ValueError("a FIXED value beyond Int64")


def _meta(col):
    md = {"logicalType": col["type"]}
    if "precision" in col:
        md["precision"] = str(col["precision"])
    if "scale" in col:
        md["scale"] = str(col["scale"])
    return md


def encode_column(col, values):
    """One column of one chunk: (pyarrow field, array). `values` are Python
    values in the source's terms: Decimal for FIXED, float for REAL, str, bool,
    days since the epoch for DATE, and (epoch nanoseconds, offset minutes) for
    timestamps (offset only for TIMESTAMP_TZ)."""
    t, name, md = col["type"], col["name"], _meta(col)
    if t == "FIXED":
        p, s = col["precision"], col["scale"]
        if p <= 18:
            scaled = [None if v is None else int(decimal.Decimal(v).scaleb(s)) for v in values]
            typ = _narrow_int(scaled)
            return pa.field(name, typ, metadata=md), pa.array(scaled, type=typ)
        typ = pa.decimal128(p, s)
        return pa.field(name, typ, metadata=md), pa.array([None if v is None else decimal.Decimal(v)
                                                           for v in values], type=typ)
    if t == "REAL":
        return pa.field(name, pa.float64(), metadata=md), pa.array(values, type=pa.float64())
    if t == "TEXT":
        return pa.field(name, pa.utf8(), metadata=md), pa.array(values, type=pa.utf8())
    if t == "BOOLEAN":
        return pa.field(name, pa.bool_(), metadata=md), pa.array(values, type=pa.bool_())
    if t == "DATE":
        return pa.field(name, pa.date32(), metadata=md), pa.array(values, type=pa.int32()).cast(pa.date32())
    if t in ("TIMESTAMP_NTZ", "TIMESTAMP_LTZ", "TIMESTAMP_TZ"):
        scale = col["scale"]
        unit = 10 ** (9 - scale)
        if t != "TIMESTAMP_TZ" and scale <= 7:
            arr = [None if v is None else v[0] // unit for v in values]  # epoch × 10^scale, floored
            return pa.field(name, pa.int64(), metadata=md), pa.array(arr, type=pa.int64())
        if t == "TIMESTAMP_TZ" and scale <= 3:
            fields = [pa.field("epoch", pa.int64()), pa.field("timezone", pa.int32())]
            rows = [None if v is None else {"epoch": v[0] // unit, "timezone": v[1] + 1440} for v in values]
        else:
            fields = [pa.field("epoch", pa.int64()), pa.field("fraction", pa.int32())]
            if t == "TIMESTAMP_TZ":
                fields.append(pa.field("timezone", pa.int32()))
            rows = []
            for v in values:
                if v is None:
                    rows.append(None)
                    continue
                secs, ns = divmod(v[0], 10**9)
                ns -= ns % unit  # the column's own scale
                r = {"epoch": secs, "fraction": ns}
                if t == "TIMESTAMP_TZ":
                    r["timezone"] = v[1] + 1440
                rows.append(r)
        typ = pa.struct(fields)
        return pa.field(name, typ, metadata=md), pa.array(rows, type=typ)
    raise ValueError(f"no assumed encoding for {t}")


def write_chunk(columns, rows):
    """A complete Arrow IPC stream with its own schema and one record batch."""
    fields, arrays = [], []
    for i, col in enumerate(columns):
        f, a = encode_column(col, [r[i] for r in rows])
        fields.append(f)
        arrays.append(a)
    schema = pa.schema(fields)
    batch = pa.record_batch(arrays, schema=schema)
    sink = io.BytesIO()
    with ipc.new_stream(sink, schema) as w:
        w.write_batch(batch)
    return sink.getvalue()


# --- decoding chunks (the client's side, §9.7.4 step 10) --------------------


def _floor_div(a, b):
    return a // b


def _ts_micros(col_md, arr, name):
    """Microseconds since the epoch (UTC) of one timestamp column."""
    t = col_md["logicalType"]
    scale = int(col_md.get("scale", "9"))
    out = []
    if pa.types.is_int64(arr.type) and t in ("TIMESTAMP_NTZ", "TIMESTAMP_LTZ") and scale <= 7:
        for v in arr.to_pylist():
            out.append(None if v is None else _floor_div(v * 10 ** 6, 10 ** scale))
        return out
    if pa.types.is_struct(arr.type):
        names = [arr.type.field(i).name for i in range(arr.type.num_fields)]
        expected = (["epoch", "timezone"] if (t == "TIMESTAMP_TZ" and scale <= 3) else
                    ["epoch", "fraction", "timezone"] if t == "TIMESTAMP_TZ" else ["epoch", "fraction"])
        if names != expected:
            raise Mismatch(f"{name}: struct fields {names}, expected {expected}")
        for v in arr.to_pylist():
            if v is None:
                out.append(None)
            elif "fraction" in v:
                out.append(v["epoch"] * 10**6 + _floor_div(v["fraction"], 1000))
            else:
                out.append(_floor_div(v["epoch"] * 10**6, 10**scale))
        return out
    raise Mismatch(f"{name}: a {t} column encoded as {arr.type}")


def _decimal_unscaled(col_md, arr, name):
    """FIXED values as (unscaled ints, scale)."""
    p, s = int(col_md["precision"]), int(col_md["scale"])
    if p <= 18:
        if not pa.types.is_integer(arr.type):
            raise Mismatch(f"{name}: NUMBER({p},{s}) encoded as {arr.type}")
        return arr.to_pylist(), p, s
    if not pa.types.is_decimal(arr.type) or (arr.type.precision, arr.type.scale) != (p, s):
        raise Mismatch(f"{name}: NUMBER({p},{s}) encoded as {arr.type}")
    return [None if v is None else int(v.scaleb(s)) for v in arr.to_pylist()], p, s


def decode_chunk(data, writer_arrow, source_schema=None):
    """Decodes one chunk into a record batch in the writer's derived Arrow
    schema (`writer_arrow`, arrowmap.derive's form), or raises Mismatch."""
    reader = ipc.open_stream(io.BytesIO(data))
    table = reader.read_all()
    wfields = writer_arrow["fields"]
    names = table.schema.names
    if names != [f["name"] for f in wfields]:
        raise Mismatch(f"columns {names}, the writer profile has {[f['name'] for f in wfields]}")
    target = arrowmap.to_pa_schema(writer_arrow)
    arrays = []
    for i, wf in enumerate(wfields):
        field = table.schema.field(i)
        md = {k.decode(): v.decode() for k, v in (field.metadata or {}).items()}
        if "logicalType" not in md:
            raise Mismatch(f"{wf['name']}: no logicalType")
        if source_schema is not None:
            sc = source_schema[i]
            if sc["type"] != md["logicalType"] or any(
                    str(sc[k]) != md.get(k) for k in ("precision", "scale") if k in sc):
                raise Mismatch(f"{wf['name']}: the chunk's {md} differs from the handoff's source schema")
        col = table.column(i).combine_chunks()
        lt, wt = md["logicalType"], wf["type"]["name"]
        tt = target.field(i).type
        if lt == "FIXED":
            unscaled, p, s = _decimal_unscaled(md, col, wf["name"])
            if wt == "int" and s == 0 and p <= 18:
                values = unscaled
            elif wt == "decimal" and wf["type"]["scale"] == s and p <= wf["type"]["precision"]:
                values = [None if v is None else decimal.Decimal(v).scaleb(-s) for v in unscaled]
            elif wt == "floatingpoint":
                values = [None if v is None else float(decimal.Decimal(v).scaleb(-s)) for v in unscaled]
            else:
                raise Mismatch(f"{wf['name']}: NUMBER({p},{s}) does not decode to {wf['type']}")
        elif lt == "REAL" and wt == "floatingpoint" and pa.types.is_float64(col.type):
            values = col.to_pylist()
        elif lt == "TEXT" and wt == "utf8" and pa.types.is_string(col.type):
            values = col.to_pylist()
        elif lt == "BOOLEAN" and wt == "bool" and pa.types.is_boolean(col.type):
            values = col.to_pylist()
        elif lt == "DATE" and wt == "date" and pa.types.is_date32(col.type):
            values = col.cast(pa.int32()).to_pylist()
            arrays.append(pa.array(values, type=pa.int32()).cast(pa.date32()))
            _nulls(wf, values)
            continue
        elif lt in ("TIMESTAMP_NTZ", "TIMESTAMP_LTZ", "TIMESTAMP_TZ") and wt == "timestamp":
            values = _ts_micros(md, col, wf["name"])
            _nulls(wf, values)
            arrays.append(pa.array(values, type=pa.int64()).cast(tt))
            continue
        else:
            raise Mismatch(f"{wf['name']}: a {lt} column ({col.type}) does not decode to {wf['type']}")
        _nulls(wf, values)
        arrays.append(pa.array(values, type=tt))
    return pa.record_batch(arrays, schema=target)


def _nulls(wf, values):
    if not wf["nullable"] and any(v is None for v in values):
        raise Mismatch(f"{wf['name']}: a null in a column the writer profile says is never null")


# --- the JSON form of rows (the runner's, §9.8.3) ---------------------------


def rows_json(batch):
    """Arrow rows as JSON records: decimals as their strings (exactly s
    fractional digits), timestamps as RFC 3339 with six fractional digits
    (Q-TIME-1), dates as YYYY-MM-DD."""
    out = []
    cols = []
    for i, f in enumerate(batch.schema):
        arr = batch.column(i)
        if pa.types.is_timestamp(f.type):
            cols.append([None if v is None else grammar.format_date_time_us(v)
                         for v in arr.cast(pa.int64()).to_pylist()])
        elif pa.types.is_date32(f.type):
            cols.append([None if v is None else grammar.format_date(v) for v in arr.cast(pa.int32()).to_pylist()])
        elif pa.types.is_decimal(f.type):
            s = f.type.scale
            cols.append([None if v is None else decimal_text(v, s) for v in arr.to_pylist()])
        else:
            cols.append(arr.to_pylist())
    for r in range(batch.num_rows):
        out.append({batch.schema.field(i).name: cols[i][r] for i in range(len(cols))})
    return out


def decimal_text(d, s):
    q = d.quantize(decimal.Decimal(1).scaleb(-s)) if s > 0 else d.quantize(decimal.Decimal(1))
    text = format(q, "f")
    return text[1:] if text.startswith("-") and set(text[1:]) <= {"0", "."} else text
