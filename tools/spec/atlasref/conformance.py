"""The conformance gateway's capabilities (§9.8.4), as complete profiles with
real fingerprints, and the built-ins' profiles: what every scenario's gateway
serves, and what the resolution vectors' chains are taken from.

Each capability's behaviour when no scenario step overrides it is written in
README.md ("The conformance capabilities").
"""

import copy

from . import fingerprint, schemas
from .profiles import UNKNOWN_ENTRY, interaction, request_enum, result_enum, root, trait, with_desc

C = "conformance"


def _profile(cap, version, family, interactions, schemas_, meaning, summary, traits=(), lifecycle="stable",
             evolution=None, errors=(), usage_examples=None, extra_props=None):
    p = {
        "identity": {"id": cap, "version": version, "title": cap, "summary": summary, "domain": cap.split(".")[0],
                     "owner": "Atlas"},
        "properties": {"family": family, "stability": "official", "lifecycle": lifecycle, "visibility": "listed",
                       "traits": [t if isinstance(t, dict) else trait(t) for t in traits]},
        "interactions": interactions,
        "schemas": schemas_,
        "errors": list(errors),
        "meaning": meaning,
    }
    if extra_props:
        p["properties"].update(extra_props)
    if usage_examples:
        p["usage"] = {"when": "In the conformance suite only.", "examples": usage_examples}
    if evolution:
        p["evolution"] = evolution
    return p


def _addr(family, cap, version, inter):
    return f"/v1/{family}/{cap}@v{version}:{inter}"


def _closed(desc, props, required=()):
    s = {"type": "object", "description": desc, "additionalProperties": False}
    if required:
        s["required"] = list(required)
    s["properties"] = props
    return root(s)


def _rows(desc, row):
    return root({"type": "array", "description": desc, "items": row})


COLUMNS = {"type": ["array", "null"], "description": "The columns to return, as the reader names them.",
           "items": {"type": "string", "description": "A column name.", "minLength": 1}, "uniqueItems": True,
           "default": None}

# --- conformance.echo@v1 -----------------------------------------------------

ECHO_KINDS = {"plain": "An ordinary echo.", "loud": "An echo in capitals."}


def echo():
    req = _closed("What to echo.", {
        "text": {"type": "string", "description": "The text to echo.", "minLength": 1, "maxLength": 200},
        "count": {"type": "integer", "description": "How many times.", "minimum": 0, "maximum": 100,
                  "default": 1, "x-atlas-unit": "count"},
        "ratio": {"type": "number", "description": "A ratio, echoed.", "x-atlas-logical": "Fraction"},
        "when": {"type": "string", "description": "A date, echoed.", "format": "date"},
        "kind": with_desc(request_enum(list(ECHO_KINDS), ECHO_KINDS), "How to echo."),
        "tags": {"type": "array", "description": "Tags, echoed.", "maxItems": 10,
                 "items": {"type": "string", "description": "A tag.", "minLength": 1}},
    }, required=["text"])
    res = root({"type": "object", "description": "The request, echoed.", "required": ["text", "count", "kind"],
                "properties": {
                    "text": {"type": "string", "description": "The text."},
                    "count": {"type": "integer", "description": "How many times.", "x-atlas-unit": "count"},
                    "ratio": {"type": ["number", "null"], "description": "The ratio.", "x-atlas-logical": "Fraction"},
                    "when": {"type": ["string", "null"], "description": "The date.", "format": "date"},
                    "kind": with_desc(result_enum(list(ECHO_KINDS), ECHO_KINDS), "How it was echoed."),
                    "tags": {"type": "array", "description": "The tags.",
                             "items": {"type": "string", "description": "A tag."}},
                }})
    return _profile(f"{C}.echo", 1, "data", {
        "fetch": interaction("read", "record", "single", _addr("data", f"{C}.echo", 1, "fetch"), takes="request",
                             returns="result")},
        {"request": req, "result": res},
        {"grain": "one echo", "source": "the conformance gateway"},
        "Echo a request back, to prove validation, headers and resolution.",
        usage_examples=[{"title": "Echo", "interaction": "fetch", "request": {"text": "hello"}}])


# --- conformance.rows@v1 -----------------------------------------------------

def rows_row():
    return {"type": "object", "description": "Row i of n.", "required": ["i", "label", "amount", "at", "day"],
            "properties": {
                "i": {"type": "integer", "description": "The row's index.", "minimum": 0, "x-atlas-unit": "count",
                      "x-atlas-key": True},
                "label": {"type": "string", "description": "row-<i>."},
                "amount": {"type": "string", "description": "i × 1.5, exactly.", "x-atlas-logical": "Decimal(18,6)"},
                "at": {"type": "string", "description": "2026-01-01T00:00:00Z plus i seconds and i microseconds.",
                       "format": "date-time"},
                "day": {"type": "string", "description": "2026-01-01 plus i days.", "format": "date"},
                "share": {"type": ["number", "null"], "description": "i / 100, or null when i % 5 = 4.",
                          "x-atlas-logical": "Fraction"},
            }}


def rows():
    cap = f"{C}.rows"
    req = _closed("How many rows, and which columns.", {
        "n": {"type": "integer", "description": "How many rows.", "minimum": 0, "maximum": 1000000,
              "x-atlas-unit": "count"},
        "columns": copy.deepcopy(COLUMNS),
    }, required=["n"])
    return _profile(cap, 1, "data", {
        "fetch": interaction("read", "table", "chunked", _addr("data", cap, 1, "fetch"), takes="request",
                             returns="result", encodings=("arrow", "ndjson", "json")),
        "explain": interaction("read", "record", "single", _addr("data", cap, 1, "explain"), takes="request"),
        "export": interaction("read", "content", "transfer", _addr("data", cap, 1, "export"), takes="request",
                              deadline=("PT5M", "PT30M"), files=("parquet", "arrow")),
    }, {"request": req, "result": _rows("n rows.", rows_row())},
        {"grain": "one row per i", "keys": ["i"], "source": "the conformance gateway"},
        "Rows 0 to n-1, to prove framing, end lines, truncation, columns and encodings.",
        traits=("Projectable",),
        usage_examples=[{"title": "Three rows", "interaction": "fetch", "request": {"n": 3}}])


# --- conformance.items@v1 ----------------------------------------------------

def items():
    cap = f"{C}.items"
    req = _closed("A page of items.", {
        "n": {"type": "integer", "description": "How many items in all.", "minimum": 0, "maximum": 10000,
              "x-atlas-unit": "count"},
        "page_size": {"type": "integer", "description": "Items per page.", "minimum": 1, "maximum": 100,
                      "default": 10, "x-atlas-unit": "count"},
        "cursor": {"type": "string", "description": "The page to read.", "minLength": 1},
    }, required=["n"])
    item = root({"type": "object", "description": "Item k.", "required": ["k", "name"], "properties": {
        "k": {"type": "integer", "description": "The item's index.", "x-atlas-unit": "count", "x-atlas-key": True},
        "name": {"type": "string", "description": "item-<k>."}}})
    return _profile(cap, 1, "data", {
        "fetch": interaction("read", "collection", "single", _addr("data", cap, 1, "fetch"), takes="request",
                             returns="item")},
        {"request": req, "item": item}, {"grain": "one item per k", "source": "the conformance gateway"},
        "Items 0 to n-1 a page at a time, to prove cursor paging.", traits=("Paged",))


# --- conformance.slow@v1 -----------------------------------------------------

def slow():
    cap = f"{C}.slow"
    req = _closed("How long the work takes, and how large its result is.", {
        "polls": {"type": "integer", "description": "How many polls before it succeeds.", "minimum": 0,
                  "maximum": 20, "default": 2, "x-atlas-unit": "count"},
        "result_bytes": {"type": "integer", "description": "The result's size; above 1 MiB it is given by "
                                                           "result_location.", "minimum": 0,
                         "maximum": 4194304, "default": 16, "x-atlas-unit": "count"},
    })
    res = root({"type": "object", "description": "The work's result.", "required": ["done", "padding"],
                "properties": {"done": {"type": "boolean", "description": "Always true."},
                               "padding": {"type": "string", "description": "result_bytes of x."}}})
    return _profile(cap, 1, "data", {
        "fetch": interaction("read", "record", "operation", _addr("data", cap, 1, "fetch"), takes="request",
                             returns="result", deadline=("PT1M", "PT10M"))},
        {"request": req, "result": res}, {"grain": "one result", "source": "the conformance gateway"},
        "Work that answers 202 and an operation, to prove polling and cancelling.")


# --- conformance.evolving@v1 #A #B #C, and @v2 --------------------------------

STATUS_A = {"open": "Held.", "sold": "Sold that day."}
STATUS_C = {"open": "Held.", "closed": "Sold that day."}


def evolving_steps():
    """(request schema, row schema, lens) of #A, #B and #C, oldest first.
    The two lenses hold one of each operation of §9.5.3."""
    req_a = _closed("Which ids, and which columns.", {
        "ids": {"type": "array", "description": "The ids to read.", "minItems": 1,
                "items": {"type": "string", "description": "An id.", "minLength": 1}},
        "region": {"type": "string", "description": "Only this region.", "minLength": 1},
        "columns": copy.deepcopy(COLUMNS),
    }, required=["ids"])
    req_b = copy.deepcopy(req_a)
    req_b["properties"] = {"ids": req_a["properties"]["ids"],
                           "area": {"type": "string", "description": "Only this area.", "minLength": 1,
                                    "x-atlas-aliases": ["region"]},
                           "columns": req_a["properties"]["columns"]}
    req_c = copy.deepcopy(req_b)
    row_a = {"type": "object", "description": "One position.", "required": ["id", "mv", "weight", "status", "count"],
             "properties": {
                 "id": {"type": "string", "description": "The id.", "x-atlas-key": True},
                 "mv": {"type": "number", "description": "Market value.", "x-atlas-unit": "currency_unit"},
                 "weight": {"type": "number", "description": "Weight.", "x-atlas-logical": "Percent"},
                 "status": with_desc(result_enum(list(STATUS_A), STATUS_A), "Status."),
                 "count": {"type": "integer", "description": "Lots.", "x-atlas-unit": "count"},
                 "legacy": {"type": ["string", "null"], "description": "A field later dropped."},
             }}
    row_b = {"type": "object", "description": "One position.",
             "required": ["id", "market_value", "weight", "currency", "status", "count"],
             "properties": {
                 "id": row_a["properties"]["id"],
                 "market_value": {"type": "number", "description": "Market value.", "x-atlas-unit": "currency_unit",
                                  "x-atlas-aliases": ["mv"]},
                 "weight": {"type": "number", "description": "Weight.", "x-atlas-logical": "Fraction"},
                 "currency": {"type": "string", "description": "The value's currency.", "x-atlas-logical": "Currency"},
                 "status": row_a["properties"]["status"],
                 "count": row_a["properties"]["count"],
                 "legacy": dict(row_a["properties"]["legacy"], **{"x-atlas-deprecated": {
                     "reason": "No reader needs it.", "since": "2026-07-01", "sunset": "2026-10-01",
                     "replaced_by": None}}),
             }}
    row_c = {"type": "object", "description": "One position.",
             "required": ["id", "market_value", "weight", "currency", "status", "count", "note"],
             "properties": {
                 "id": row_a["properties"]["id"],
                 "market_value": row_b["properties"]["market_value"],
                 "weight": row_b["properties"]["weight"],
                 "currency": row_b["properties"]["currency"],
                 "status": with_desc(result_enum(list(STATUS_C), STATUS_C, aliases={"closed": ["sold"]}), "Status."),
                 "count": {"type": "number", "description": "Lots, now fractional.", "x-atlas-unit": "count"},
                 "note": {"type": ["string", "null"], "description": "A note; null when there is none."},
             }}
    lens_b = [{"op": "rename", "from": "mv", "to": "market_value"},
              {"op": "convert_unit", "field": "weight", "from": "Percent", "to": "Fraction"},
              {"op": "default", "field": "currency", "value": "USD"},
              {"op": "rename", "from": "region", "to": "area"}]
    lens_c = [{"op": "alias_value", "field": "status", "from": "sold", "to": "closed"},
              {"op": "promote", "field": "count", "from": "integer", "to": "number"},
              {"op": "drop", "field": "legacy"}]
    return [(req_a, row_a, None), (req_b, row_b, lens_b), (req_c, row_c, lens_c)]


def evolving():
    """[#A, #B, #C] as served, each with its fingerprint."""
    cap = f"{C}.evolving"
    out, previous = [], None
    for label, (req, row, lens) in zip("ABC", evolving_steps()):
        evolution = None
        if lens is not None:
            evolution = {"previous_profile": previous, "lens": copy.deepcopy(lens),
                         "changelog": [{"change": f"Profile #{label}."}]}
        p = _profile(cap, 1, "data", {
            "fetch": interaction("read", "table", "chunked", _addr("data", cap, 1, "fetch"), takes="request",
                                 returns="result", encodings=("arrow", "ndjson", "json"))},
            {"request": copy.deepcopy(req), "result": _rows("Positions.", copy.deepcopy(row))},
            {"grain": "one position per id", "keys": ["id"], "source": "the conformance gateway"},
            "Positions whose contract evolves within @v1, to prove resolution.", traits=("Projectable",),
            evolution=evolution, lifecycle="deprecated",
            extra_props={"deprecated": "2026-10-01", "sunset": "2027-01-31",
                         "replaced_by": "conformance.evolving@v2"})
        p = fingerprint.with_fingerprint(p)
        out.append(p)
        previous = fingerprint.exact_id(p)
    return out


def evolving_v2():
    cap = f"{C}.evolving"
    row = {"type": "object", "description": "One position, intraday.", "required": ["id", "value", "at"],
           "properties": {
               "id": {"type": "string", "description": "The id.", "x-atlas-key": True},
               "value": {"type": "object", "description": "Market value.", "x-atlas-logical": "Money"},
               "at": {"type": "string", "description": "When.", "format": "date-time", "x-atlas-key": True},
           }}
    req = _closed("Which ids.", {"ids": {"type": "array", "description": "The ids to read.", "minItems": 1,
                                         "items": {"type": "string", "description": "An id.", "minLength": 1}}},
                  required=["ids"])
    p = _profile(cap, 2, "data", {
        "fetch": interaction("read", "table", "chunked", _addr("data", cap, 2, "fetch"), takes="request",
                             returns="result", encodings=("arrow", "ndjson", "json"))},
        {"request": req, "result": _rows("Positions.", row)},
        {"grain": "one position per id and instant", "keys": ["id", "at"], "source": "the conformance gateway"},
        "Positions intraday: a new meaning, so a new version.", traits=("Projectable",),
        evolution={"previous_version": f"{C}.evolving@v1"})
    return fingerprint.with_fingerprint(p)


# --- the rest of §9.8.4's capabilities ---------------------------------------

def simple_record(cap, summary, traits=(), lifecycle="stable", extra_props=None, request=None, result=None):
    req = request or _closed("A request.", {"text": {"type": "string", "description": "Anything.", "minLength": 1}})
    res = result or root({"type": "object", "description": "The answer.", "required": ["ok"],
                          "properties": {"ok": {"type": "boolean", "description": "Always true."}}})
    return _profile(cap, 1, "data", {
        "fetch": interaction("read", "record", "single", _addr("data", cap, 1, "fetch"), takes="request",
                             returns="result")},
        {"request": req, "result": res}, {"grain": "one answer", "source": "the conformance gateway"}, summary,
        traits=traits, lifecycle=lifecycle, extra_props=extra_props)


def retired():
    return simple_record(f"{C}.retired", "A version retired at its sunset: every call is retired.",
                         lifecycle="retired", extra_props={"deprecated": "2026-01-01", "sunset": "2026-04-01",
                                                           "replaced_by": f"{C}.echo@v1"})


def critical():
    return simple_record(f"{C}.critical", "A capability with a critical trait no client implements.",
                         traits=(trait("ConformanceOnly", critical=True),))


def costly():
    p = simple_record(f"{C}.costly", "A metered call whose estimate needs a person's confirmation.",
                      traits=("Metered",))
    p["usage"] = {"when": "In the conformance suite only.", "cost": {"unit": "credits", "price": "12.500"}}
    return p


def dynamic():
    cap = f"{C}.dynamic"
    req = _closed("A query.", {"query": {"type": "string", "description": "The caller's own query.", "minLength": 1}},
                  required=["query"])
    res = root({"type": "array", "description": "Rows whose columns the query chose.",
                "items": {"type": "object", "description": "A row."}})
    return _profile(cap, 1, "data", {
        "fetch": interaction("read", "table", "chunked", _addr("data", cap, 1, "fetch"), takes="request",
                             returns="result", encodings=("arrow", "ndjson", "json"))},
        {"request": req, "result": res}, {"grain": "whatever the query returns", "source": "the conformance gateway"},
        "Rows whose columns the caller's query chooses: no fixed schema, no resolution.", traits=("DynamicSchema",),
        extra_props={"stability": "upstream"})


def handoff():
    from .gen.handoff import EVERY_ROW, writer

    return writer("every-type", EVERY_ROW)


def thing():
    cap = f"{C}.thing"
    from .profiles import envelope_props
    env = envelope_props()
    colours = {"red": "Red.", "blue": "Blue."}
    record = root({"type": "object", "description": "A thing: its envelope and its own fields.",
                   "required": ["name", "metadata", "colour", "size"],
                   "properties": {"name": env["name"], "metadata": env["metadata"],
                                  "colour": with_desc(result_enum(list(colours), colours), "Its colour."),
                                  "size": {"type": "integer", "description": "Its size.", "minimum": 0,
                                           "x-atlas-unit": "count"},
                                  "status": env["status"]}})
    name = {"type": "string", "description": "The thing's name.", "pattern": schemas.P_RESOURCE}
    labels = {"type": "object", "description": "Of metadata, a caller writes only labels.",
              "additionalProperties": False,
              "properties": {"labels": {"type": "object", "description": "The caller's labels.",
                                        "additionalProperties": {"type": "string", "description": "A value."}}}}
    own = {"colour": with_desc(request_enum(list(colours), colours), "Its colour."),
           "size": {"type": "integer", "description": "Its size.", "minimum": 0, "x-atlas-unit": "count"}}
    by_name = _closed("One thing, by name.", {"name": name}, required=["name"])
    sch = {
        "resource": record,
        "get_request": by_name,
        "list_request": _closed("A page of things.", {"cursor": {"type": "string", "description": "The page.",
                                                                 "minLength": 1}}),
        "create_request": _closed("A new thing.", {"name": name, **copy.deepcopy(own), "metadata": labels},
                                  required=["name", "colour", "size"]),
        "update_request": _closed("A change to the fields the mask names, with If-Match.", {
            "name": name,
            "fields": {"type": "array", "description": "The field mask: colour, size, metadata.labels.",
                       "minItems": 1, "uniqueItems": True,
                       "items": {"type": "string", "description": "A field path.", "minLength": 1}},
            **copy.deepcopy(own), "metadata": copy.deepcopy(labels)}, required=["name", "fields"]),
    }
    A = lambda i: _addr("resource", cap, 1, i)
    return _profile(cap, 1, "resource", {
        "get": interaction("read", "record", "single", A("get"), takes="get_request", returns="resource"),
        "list": interaction("read", "collection", "single", A("list"), takes="list_request", returns="resource"),
        "create": interaction("write", "record", "single", A("create"), takes="create_request", returns="resource"),
        "update": interaction("write", "record", "single", A("update"), takes="update_request", returns="resource"),
        "delete": interaction("write", "record", "single", A("delete"), takes="get_request", returns="resource"),
        "touch": interaction("write", "record", "single", A("touch"), takes="get_request", returns="resource"),
    }, sch, {"name": "conformance/things/{id}", "immutable": ["name"], "stored_version": 1,
             "converges": "a fake controller sets the ready condition"},
        "Things the gateway keeps and converges, to prove the envelope, ETags and idempotency.",
        traits=("Paged", "Keyed"))


def file_():
    cap = f"{C}.file"
    record = root({"type": "object", "description": "A file's metadata record.",
                   "required": ["name", "media_type", "bytes", "sha256"],
                   "properties": {
                       "name": {"type": "string", "description": "The file's name.", "minLength": 1},
                       "media_type": {"type": "string", "description": "Its media type.", "minLength": 1},
                       "bytes": {"type": "integer", "description": "Its size.", "minimum": 0, "x-atlas-unit": "count"},
                       "sha256": {"type": "string", "description": "Its SHA-256.", "pattern": schemas.P_SHA256}}})
    get = _closed("A file to download, by name.", {"name": {"type": "string", "description": "The file's name.",
                                                            "minLength": 1}}, required=["name"])
    publish = _closed("A file to upload.", {
        "name": {"type": "string", "description": "The file's name.", "minLength": 1},
        "media_type": {"type": "string", "description": "Its media type.", "minLength": 1},
        "bytes": {"type": "integer", "description": "Its size.", "minimum": 0, "maximum": 1073741824,
                  "x-atlas-unit": "count"},
        "sha256": {"type": "string", "description": "Its SHA-256.", "pattern": schemas.P_SHA256}},
        required=["name", "media_type", "bytes", "sha256"])
    A = lambda i: _addr("resource", cap, 1, i)
    return _profile(cap, 1, "resource", {
        "publish": interaction("write", "record", "transfer", A("publish"), takes="publish_request",
                               returns="record", deadline=("PT5M", "PT30M")),
        "get": interaction("read", "content", "transfer", A("get"), takes="get_request", returns="record",
                           deadline=("PT5M", "PT30M")),
    }, {"record": record, "get_request": get, "publish_request": publish},
        {"name": "files by name", "immutable": ["name"], "stored_version": 1},
        "Files moved by transfer, to prove parts, checksums, commit and expired links.")


def change():
    cap = f"{C}.change"
    req = _closed("The change to make.", {"target": {"type": "string", "description": "What to change.",
                                                     "minLength": 1}}, required=["target"])
    plan = root({"type": "object", "description": "What run will do.", "required": ["plan", "will"],
                 "properties": {"plan": {"type": "string", "description": "The plan's id."},
                                "will": {"type": "array", "description": "The steps.",
                                         "items": {"type": "string", "description": "A step."}}}})
    by_plan = _closed("A plan, by id.", {"plan": {"type": "string", "description": "The plan's id.", "minLength": 1}},
                      required=["plan"])
    approval = root({"type": "object", "description": "An approval.", "required": ["plan", "approved"],
                     "properties": {"plan": {"type": "string", "description": "The plan."},
                                    "approved": {"type": "boolean", "description": "True."}}})
    receipt = root({"type": "object", "description": "What happened.", "required": ["plan", "changed"],
                    "properties": {"plan": {"type": "string", "description": "The plan run."},
                                   "changed": {"type": "boolean", "description": "Whether anything changed."}}})
    A = lambda i: _addr("action", cap, 1, i)
    return _profile(cap, 1, "action", {
        "plan": interaction("read", "record", "single", A("plan"), takes="request", returns="plan"),
        "approve": interaction("write", "record", "single", A("approve"), takes="by_plan", returns="approval"),
        "run": interaction("external", "record", "operation", A("run"), takes="by_plan", returns="receipt",
                           deadline=("PT1M", "PT10M")),
    }, {"request": req, "plan": plan, "by_plan": by_plan, "approval": approval, "receipt": receipt},
        {"effects": {"system": "the conformance gateway", "changes": "nothing outside it", "reversible": True},
         "policy": {"approval": ["conformance"]}},
        "An action whose run needs another principal's approval.", traits=("DryRunnable", "NeedsApproval"))


def tick(label="B"):
    """conformance.tick@v1: #A (count) and #B (count renamed n, with a lens)."""
    cap = f"{C}.tick"
    payload_a = root({"type": "object", "description": "A tick.", "required": ["seq", "count"],
                      "properties": {"seq": {"type": "integer", "description": "Its sequence.", "minimum": 0,
                                             "x-atlas-unit": "count"},
                                     "count": {"type": "integer", "description": "A count.",
                                               "x-atlas-unit": "count"}}})
    payload_b = root({"type": "object", "description": "A tick.", "required": ["seq", "n"],
                      "properties": {"seq": payload_a["properties"]["seq"],
                                     "n": {"type": "integer", "description": "A count.", "x-atlas-unit": "count",
                                           "x-atlas-aliases": ["count"]}}})
    publish = copy.deepcopy(payload_b if label == "B" else payload_a)
    publish["additionalProperties"] = False
    publish["description"] = "The tick to publish."
    subscribe = _closed("Which ticks.", {"durable": {"type": "string", "description": "A durable name.",
                                                     "pattern": "^[a-z][a-z0-9-]{0,49}$"}})
    attributes = root({"type": "object", "description": "Attributes.", "required": ["subject"],
                       "properties": {"subject": {"type": "string", "description": "The ticker.", "minLength": 1}}})
    A = lambda i: _addr("event", cap, 1, i)
    evolution = None
    if label == "B":
        evolution = {"previous_profile": fingerprint.exact_id(fingerprint.with_fingerprint(tick("A"))),
                     "lens": [{"op": "rename", "from": "count", "to": "n"}]}
    p = _profile(cap, 1, "event", {
        "subscribe": interaction("read", "record", "subscription", A("subscribe"), takes="subscribe_request",
                                 returns="payload"),
        "publish": interaction("external", "record", "single", A("publish"), takes="publish_request"),
    }, {"payload": payload_b if label == "B" else payload_a, "attributes": attributes,
        "subscribe_request": subscribe, "publish_request": publish},
        {"emitted": "on the conformance gateway's command", "subject": "the ticker", "ordering": "none",
         "retention": "P1D", "history": f"{C}.rows@v1"},
        "Ticks through the Service Bus emulator, to prove the Events level.", evolution=evolution)
    return fingerprint.with_fingerprint(p)


def served():
    """Every conformance profile, exact-id → profile."""
    out = {}
    plain = [echo(), rows(), items(), slow(), retired(), critical(), costly(), dynamic(), thing(), file_(), change()]
    for p in [fingerprint.with_fingerprint(x) for x in plain] + evolving() + [evolving_v2(), handoff(), tick("A"),
                                                                              tick("B")]:
        out[fingerprint.exact_id(p)] = p
    return out


def current():
    """The profile each pinned id is served under by default: the newest."""
    out = {}
    for exact, p in served().items():
        out[exact.split("#")[0]] = exact
    return out


__all__ = ["served", "evolving", "evolving_steps", "evolving_v2", "schemas", "UNKNOWN_ENTRY"]


# --- variants that scenarios serve, and the built-ins' profiles ------------------

def echo_moved():
    """CORE-CALL-003: conformance.echo@v1 served at an unusual address."""
    p = echo()
    p["interactions"]["fetch"]["bindings"][0]["address"] = "/elsewhere/7/echo;v=1"
    return fingerprint.with_fingerprint(p)


def echo_newer():
    """CORE-TOL-001: a newer profile of conformance.echo@v1: a kind and a result
    member added, and a trait no client knows (not critical)."""
    p = echo()
    kinds = dict(ECHO_KINDS, whisper="An echo, quietly.")
    p["schemas"]["result"]["properties"]["kind"] = with_desc(result_enum(list(kinds), kinds), "How it was echoed.")
    p["schemas"]["result"]["properties"]["echoed_at"] = {"type": ["string", "null"], "description": "When.",
                                                         "format": "date-time"}
    p["properties"]["traits"].append(trait("FutureTrait"))
    p["evolution"] = {"previous_profile": fingerprint.exact_id(fingerprint.with_fingerprint(echo())), "lens": []}
    return fingerprint.with_fingerprint(p)


BUILTINS = [
    # (id, family, {interaction: (effect, result, delivery, takes def, returns doc or def)})
    ("atlas.whoami", "data", {"fetch": ("read", "record", "single", "whoami_request", "#whoami_result")}),
    ("atlas.catalog", "data", {"fetch": ("read", "collection", "single", "catalog_request", "catalog-page.schema.json")}),
    ("atlas.search", "data", {"fetch": ("read", "collection", "single", "search_request", "catalog-page.schema.json")}),
    ("atlas.describe", "data", {"fetch": ("read", "record", "single", "describe_request", "describe.schema.json")}),
    ("atlas.guide", "data", {"fetch": ("read", "record", "single", "guide_request", "#guide_result")}),
    ("atlas.access", "data", {"fetch": ("read", "collection", "single", "access_request", "#access_entry")}),
    ("atlas.usage", "data", {"fetch": ("read", "collection", "single", "usage_request", "#usage_entry")}),
    ("atlas.setup", "data", {"fetch": ("read", "record", "single", "setup_request", "#setup_result")}),
    ("atlas.clients", "data", {"fetch": ("read", "collection", "single", "clients_request", "#client_entry")}),
    ("atlas.compare", "data", {"fetch": ("read", "table", "chunked", "compare_request", None)}),
    ("atlas.operation", "resource", {
        "get": ("read", "record", "single", "operation_get_request", "operation.schema.json"),
        "list": ("read", "collection", "single", "operation_list_request", "operation.schema.json"),
        "cancel": ("write", "record", "single", "operation_get_request", "operation.schema.json"),
        "watch": ("read", "record", "subscription", "operation_get_request", "operation.schema.json")}),
    ("atlas.catalog.changed", "event", {"subscribe": ("read", "record", "subscription", "whoami_request",
                                                      "#catalog_changed_payload")}),
    ("atlas.settings", "resource", {"get": ("read", "record", "single", "settings_get_request", None),
                                    "update": ("write", "record", "single", "settings_update_request", None)}),
    ("atlas.directory", "data", {"fetch": ("read", "collection", "single", "directory_request", "#directory_entry")}),
    ("atlas.audit", "data", {"fetch": ("read", "collection", "single", "audit_request", "#audit_record")}),
]


def builtin_profiles():
    """The atlas.* profiles atlas.describe serves (§9.3.2): their schemas name
    builtins.schema.json's $defs and the protocol's documents, which atlas/1.0
    fixes; they are exempt from the profile lint (Q-PROF-9)."""
    out = {}
    for cap, family, inters in BUILTINS:
        interactions, sch = {}, {}
        for name, (effect, result, delivery, takes, returns) in inters.items():
            i = interaction(effect, result, delivery, f"/v1/atlas/{cap}:{name}", takes=takes, returns=None)
            sch[takes] = root({"$ref": schemas.BASE + "builtins.schema.json#/$defs/" + takes})
            if returns:
                rname = returns.lstrip("#").replace(".schema.json", "").replace("-", "_")
                ref = (schemas.BASE + "builtins.schema.json#/$defs/" + returns[1:] if returns.startswith("#")
                       else schemas.BASE + returns)
                sch[rname] = root({"$ref": ref})
                i = interaction(effect, result, delivery, f"/v1/atlas/{cap}:{name}", takes=takes, returns=rname)
            interactions[name] = i
        meaning = {"grain": "fixed by atlas/1.0", "source": "the gateway"} if family == "data" else (
            {"emitted": "when the caller's working view may have changed", "subject": "the caller",
             "ordering": "none", "retention": "PT1H", "history": "atlas.catalog@v1"} if family == "event" else
            {"name": "fixed by atlas/1.0"})
        p = _profile(cap, 1, family, interactions, sch, meaning, f"The built-in {cap} (§9.3.2).")
        p = fingerprint.with_fingerprint(p)
        out[fingerprint.exact_id(p)] = p
    return out
