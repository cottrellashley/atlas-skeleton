"""references/vectors/handoff/snowflake-arrow-1/synthetic/: chunks written by
pyarrow in Snowflake's assumed encoding (snowarrow.py), with their source row
type, the handoff document that names them, and the expected Atlas batches.
To be replaced by chunks captured from Development (§9.8.2)."""

import base64
import copy
import decimal
import hashlib
import io

import pyarrow.ipc as ipc

from .. import arrowmap, fingerprint, grammar, profiles, snowarrow
from ..jsonio import dumps

DIR = "vectors/handoff/snowflake-arrow-1/synthetic/"
D = decimal.Decimal
STORAGE = "https://results.conformance.test/synthetic/"
EXPIRES = "2026-11-01T12:00:00Z"


def ns(text):
    return grammar.parse_date_time_ns(text)


def days(text):
    return grammar.days_from_civil(*grammar.parse_date(text))


def col(name, type_, precision=None, scale=None, nullable=True):
    c = {"name": name, "type": type_}
    if precision is not None:
        c["precision"] = precision
    if scale is not None:
        c["scale"] = scale
    c["nullable"] = nullable
    return c


def prop(desc, schema):
    s = {"description": desc}
    s.update(schema)
    return s


EVERY_COLUMNS = [
    col("i", "FIXED", 9, 0, False), col("amount", "FIXED", 18, 6), col("notional", "FIXED", 38, 6),
    col("spread", "FIXED", 9, 4), col("ratio", "REAL"), col("label", "TEXT"), col("open", "BOOLEAN"),
    col("day", "DATE"), col("at_ntz", "TIMESTAMP_NTZ", scale=6), col("at_ltz", "TIMESTAMP_LTZ", scale=3),
    col("at_tz", "TIMESTAMP_TZ", scale=3), col("at_ntz9", "TIMESTAMP_NTZ", scale=9),
    col("at_tz9", "TIMESTAMP_TZ", scale=9),
]
EVERY_ROW = {
    "type": "object", "description": "One row of every type.", "required": ["i"],
    "properties": {
        "i": prop("The row's key.", {"type": "integer", "x-atlas-unit": "count", "x-atlas-key": True}),
        "amount": prop("An exact amount.", {"type": ["string", "null"], "x-atlas-logical": "Decimal(18,6)"}),
        "notional": prop("A wide exact amount.", {"type": ["string", "null"], "x-atlas-logical": "Decimal(38,6)"}),
        "spread": prop("A spread, as a float.", {"type": ["number", "null"], "x-atlas-logical": "Bp"}),
        "ratio": prop("A ratio.", {"type": ["number", "null"], "x-atlas-logical": "Fraction"}),
        "label": prop("A label.", {"type": ["string", "null"]}),
        "open": prop("Whether it is open.", {"type": ["boolean", "null"]}),
        "day": prop("A date.", {"type": ["string", "null"], "format": "date"}),
        "at_ntz": prop("TIMESTAMP_NTZ(6), converted to UTC in the SQL.", {"type": ["string", "null"],
                                                                         "format": "date-time"}),
        "at_ltz": prop("TIMESTAMP_LTZ(3).", {"type": ["string", "null"], "format": "date-time"}),
        "at_tz": prop("TIMESTAMP_TZ(3).", {"type": ["string", "null"], "format": "date-time"}),
        "at_ntz9": prop("TIMESTAMP_NTZ(9).", {"type": ["string", "null"], "format": "date-time"}),
        "at_tz9": prop("TIMESTAMP_TZ(9).", {"type": ["string", "null"], "format": "date-time"}),
    },
}
T1 = ns("2026-10-30T14:02:11.123456789Z")
T0 = ns("1969-12-31T23:59:59.999999500Z")  # before the epoch, below a microsecond
EVERY_ROWS = [
    [0, D("1250.500000"), D("12345678901234567890.123456"), D("123.4567"), 0.015, "Z" + chr(0xFC) + "rich", True,
     days("2026-10-30"), (T1, 0), (T1, 0), (T1, 330), (T1, 0), (T1, -480)],
    [1, D("-0.000001"), D("-1.000000"), D("-0.0001"), -2.5, "", False, days("1969-12-31"), (T0, 0), (T0, 0),
     (T0, -60), (T0, 0), (T0, 840)],
    [2, None, None, None, None, None, None, None, None, None, None, None, None],
    [3, D("0"), D("0"), D("0"), 0.0, "a\tb", True, days("2000-02-29"), (ns("2000-02-29T00:00:00Z"), 0),
     (ns("2000-02-29T00:00:00Z"), 0), (ns("2000-02-29T00:00:00Z"), 0), (ns("2000-02-29T00:00:00Z"), 0),
     (ns("2000-02-29T00:00:00Z"), 0)],
]

WIDTH_COLUMNS = [col("i", "FIXED", 18, 0, False), col("price", "FIXED", 10, 2, False)]
WIDTH_ROW = {
    "type": "object", "description": "Integers whose encoded width changes from chunk to chunk.",
    "required": ["i", "price"],
    "properties": {
        "i": prop("The key.", {"type": "integer", "x-atlas-unit": "count", "x-atlas-key": True}),
        "price": prop("A price.", {"type": "string", "x-atlas-logical": "Decimal(10,2)"}),
    },
}
WIDTH_CHUNKS = [
    [[1, D("1.25")], [2, D("-1.28")]],                     # Int8, Int8
    [[300, D("327.67")], [-40000, D("0.01")]],             # Int32, Int16
    [[9007199254740991, D("99999999.99")]],                # Int64 (2^53-1, the largest integer), Int32
]

SCALE_COLUMNS = [col("i", "FIXED", 4, 0, False), col("s0", "TIMESTAMP_NTZ", scale=0),
                 col("s7", "TIMESTAMP_NTZ", scale=7), col("tz6", "TIMESTAMP_TZ", scale=6)]
SCALE_ROW = {
    "type": "object", "description": "Timestamp scales at the encoding's edges.", "required": ["i"],
    "properties": {
        "i": prop("The key.", {"type": "integer", "x-atlas-unit": "count", "x-atlas-key": True}),
        "s0": prop("Seconds.", {"type": ["string", "null"], "format": "date-time"}),
        "s7": prop("100 ns, truncated to microseconds.", {"type": ["string", "null"], "format": "date-time"}),
        "tz6": prop("TIMESTAMP_TZ(6): a struct with its fraction.", {"type": ["string", "null"],
                                                                     "format": "date-time"}),
    },
}
SCALE_ROWS = [
    [1, (ns("2026-10-30T14:02:11Z"), 0), (ns("2026-10-30T14:02:11.9999999Z"), 0),
     (ns("2026-10-30T19:32:11.000001Z"), 330)],
    [2, (ns("1969-12-31T23:59:59Z"), 0), (ns("1969-12-31T23:59:59.9999999Z"), 0),
     (ns("1969-12-31T23:59:59.999999Z"), -720)],
]


def writer(name, row):
    p = profiles.minimal_data_profile("conformance.handoff", 1, row, title="Handed-off rows: " + name,
                                      traits=("Handoff",))
    p["interactions"]["fetch"]["execution"] = ["gateway", "handoff"]
    return fingerprint.with_fingerprint(p)


def handoff_doc(case, exact, columns, chunk_bytes, rows_per_chunk):
    return {
        "profile": exact,
        "format": snowarrow.FORMAT,
        "source_schema": copy.deepcopy(columns),
        "first_chunk": {"rows": rows_per_chunk[0], "data": base64.b64encode(chunk_bytes[0]).decode("ascii")},
        "chunks": [{"url": f"{STORAGE}{case}/chunk-{i}.arrows", "rows": rows_per_chunk[i], "bytes": len(b),
                    "headers": {"x-ms-encryption-key-sha256": hashlib.sha256(b"synthetic").hexdigest()}}
                   for i, b in enumerate(chunk_bytes) if i > 0],
        "rows": sum(rows_per_chunk),
        "expires": EXPIRES,
        "operation": f"operations/handoff-{case}",
    }


def build():
    out, cases = {}, []

    def emit(case, note, columns, row, chunks, source=None, chunk_columns=None, question=None, expect_ok=True):
        prof = writer(case, row)
        exact = fingerprint.exact_id(prof)
        derived = arrowmap.derive(prof["schemas"]["result"], exact)
        chunk_bytes = []
        for i, rows in enumerate(chunks):
            cols = chunk_columns[i] if chunk_columns else columns
            chunk_bytes.append(snowarrow.write_chunk(cols, rows))
        files = []
        for i, b in enumerate(chunk_bytes):
            path = f"{case}/chunk-{i}.arrows"
            out[DIR + path] = b
            files.append({"file": path, "rows": len(chunks[i]), "bytes": len(b), "sha256": hashlib.sha256(b).hexdigest()})
        src = source if source is not None else columns
        doc = handoff_doc(case, exact, src, chunk_bytes, [len(c) for c in chunks])
        entry = {"name": case, "note": note, "writer": exact, "result_schema": prof["schemas"]["result"],
                 "arrow_schema": derived, "source_schema": src, "chunks": files, "handoff": doc}
        if question:
            entry["question"] = question
        try:
            batches = [snowarrow.decode_chunk(b, derived, src) for b in chunk_bytes]
        except snowarrow.Mismatch as e:
            if expect_ok:
                raise
            entry["problem"] = {"code": "invalid_document"}
            entry["why"] = str(e)
        else:
            if not expect_ok:
                raise AssertionError(f"{case} decoded")
            sink = io.BytesIO()
            with ipc.new_stream(sink, batches[0].schema) as w:
                for b in batches:
                    w.write_batch(b)
            out[DIR + f"{case}/expected.arrows"] = sink.getvalue()
            entry["expected"] = {"batches": [{"rows": snowarrow.rows_json(b)} for b in batches],
                                 "file": f"{case}/expected.arrows"}
        cases.append(entry)

    emit("every-type", "One chunk holding every type of the assumed encoding: FIXED as narrow integers, as "
         "Decimal128 (p > 18) and into a float column; REAL, TEXT, BOOLEAN, DATE; timestamps as scaled integers "
         "and as structs, with offsets; nulls; and instants before the epoch, truncated to the microsecond below.",
         EVERY_COLUMNS, EVERY_ROW, [EVERY_ROWS], question="Q-SNOW-2")
    emit("widths-change", "Three chunks of one result whose integers are encoded narrower or wider chunk by chunk "
         "(Int8, Int16, Int32, Int64): every batch decodes to the same Int64 and Decimal128 schema.",
         WIDTH_COLUMNS, WIDTH_ROW, WIDTH_CHUNKS)
    emit("timestamp-scales", "Timestamps at scale 0 and 7 (scaled Int64) and a TIMESTAMP_TZ(6) struct with its "
         "fraction, its offset dropped for UTC.", SCALE_COLUMNS, SCALE_ROW, [SCALE_ROWS], question="Q-SNOW-3")
    # HAND-003 and its relatives: the chunk does not decode into the writer's schema.
    text_for_int = [col("i", "TEXT", nullable=False), col("price", "FIXED", 10, 2, False)]
    emit("mismatch-type", "A TEXT column where the writer profile has an integer.", WIDTH_COLUMNS, WIDTH_ROW,
         [[["1", D("1.00")]]], source=text_for_int, chunk_columns=[text_for_int], expect_ok=False)
    renamed = [col("id", "FIXED", 18, 0, False), col("price", "FIXED", 10, 2, False)]
    emit("mismatch-name", "A column named otherwise than the writer profile names it.", WIDTH_COLUMNS, WIDTH_ROW,
         [[[1, D("1.00")]]], source=renamed, chunk_columns=[renamed], expect_ok=False)
    short = [col("i", "FIXED", 18, 0, False)]
    emit("mismatch-missing-column", "A chunk missing a column.", WIDTH_COLUMNS, WIDTH_ROW, [[[1]]], source=short,
         chunk_columns=[short], expect_ok=False)
    wide = [col("i", "FIXED", 38, 0, False), col("price", "FIXED", 10, 2, False)]
    emit("mismatch-number-38-0", "NUMBER(38,0) where the writer profile has an integer: §2.2.1.6 maps NUMBER(p,0), "
         "p > 18, to Decimal(p,0), not to an integer.", WIDTH_COLUMNS, WIDTH_ROW, [[[1, D("1.00")]]], source=wide,
         chunk_columns=[wide], expect_ok=False, question="Q-SNOW-4")
    rescaled = [col("i", "FIXED", 18, 0, False), col("price", "FIXED", 10, 4, False)]
    emit("mismatch-scale", "NUMBER(10,4) where the writer profile has Decimal(10,2).", WIDTH_COLUMNS, WIDTH_ROW,
         [[[1, D("1.0000")]]], source=rescaled, chunk_columns=[rescaled], expect_ok=False)
    emit("mismatch-null", "A null in a column the writer profile says is never null.", WIDTH_COLUMNS, WIDTH_ROW,
         [[[1, None]]], expect_ok=False)
    emit("mismatch-source-schema", "A chunk whose metadata differs from the handoff document's source_schema.",
         WIDTH_COLUMNS, WIDTH_ROW, [[[1, D("1.00")]]],
         source=[col("i", "FIXED", 18, 0, False), col("price", "FIXED", 12, 2, False)], expect_ok=False)
    emit("mismatch-second-chunk", "A second chunk whose schema differs from the first (HAND-003).", WIDTH_COLUMNS,
         WIDTH_ROW, [[[1, D("1.00")]], [["2", D("2.00")]]],
         chunk_columns=[WIDTH_COLUMNS, [col("i", "TEXT", nullable=False), col("price", "FIXED", 10, 2, False)]],
         expect_ok=False)
    doc = {
        "description": "snowflake-arrow/1, synthetic (§9.8.2): result chunks written by tools/spec with pyarrow in "
                       "the encoding ASSUMED until chunks are captured from Development (QUESTIONS.md, Q-SNOW-1): "
                       "each chunk a complete Arrow IPC stream whose fields carry logicalType, precision and scale; "
                       "FIXED with p <= 18 as the narrowest integer holding value x 10^s, p > 18 as Decimal128; "
                       "REAL, TEXT, BOOLEAN, DATE as Float64, Utf8, Bool, Date32; TIMESTAMP_NTZ/LTZ as Int64 "
                       "x 10^scale up to scale 7, else Struct{epoch, fraction}; TIMESTAMP_TZ as Struct{epoch "
                       "scaled, timezone} up to scale 3, else Struct{epoch, fraction, timezone}, timezone being "
                       "the offset in minutes + 1440. Each case gives the writer profile (its result schema and "
                       "derived Arrow schema, §9.2.7), the source schema, the chunk files with their SHA-256, the "
                       "handoff document that names them (first_chunk inline; links under "
                       "https://results.conformance.test/, which the conformance gateway serves from its own "
                       "storage), and the expected Atlas batches: one per chunk, as JSON rows (decimals as strings "
                       "with exactly s digits, timestamps RFC 3339 UTC with six digits) and as an Arrow IPC stream. "
                       "A chunk that does not decode into the writer's schema is invalid_document.",
        "format": snowarrow.FORMAT,
        "cases": cases,
    }
    out[DIR + "cases.json"] = dumps(doc)
    return out
