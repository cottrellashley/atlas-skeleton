"""Complete profiles, one per family (§3.1.3 made complete, §3.1.4a ready for
agents), and the helpers every generated profile is written with.

Each profile here is written without `fingerprint`, as a service declares it
(§3.1.5); `fingerprint.with_fingerprint` adds it, as the gateway serves it.
"""

import copy

from . import schemas
from .profilecheck import DRAFT, VOCABULARY

UNKNOWN_ENTRY = {"value": "UNKNOWN", "description": "A value this reader does not know (§9.5.5)."}


def root(body):
    """A schema in the Atlas Schema profile: $schema and $vocabulary first."""
    s = {"$schema": DRAFT, "$vocabulary": copy.deepcopy(VOCABULARY)}
    s.update(body)
    return s


def https(address, encodings=("json",)):
    return {"protocol": "https", "server": "gateway", "method": "POST", "address": address,
            "encodings": list(encodings), "auth": "bearer"}


def interaction(effect, result, delivery, address, takes=None, returns=None, execution=("gateway",),
                deadline=("PT30S", "PT2M"), encodings=("json",), files=None):
    """One interaction with one https binding; `files` lists the file formats
    an export or transfer writes."""
    i = {"effect": effect, "result": result, "delivery": delivery, "execution": list(execution)}
    if takes:
        i["takes"] = takes
    if returns:
        i["returns"] = returns
    i["deadline"] = {"default": deadline[0], "max": deadline[1]}
    if files:
        i["encodings"] = list(files)
    i["bindings"] = [https(address, encodings)]
    return i


def trait(name, critical=False):
    return {"name": name, "critical": critical}


def result_enum(values, descriptions, nullable=False, aliases=None):
    """A string enum outside a request: UNKNOWN in enum and in x-atlas-enum."""
    entries = []
    for v in values:
        e = {"value": v, "description": descriptions[v]}
        if aliases and v in aliases:
            e["aliases"] = aliases[v]
        entries.append(e)
    entries.append(copy.deepcopy(UNKNOWN_ENTRY))
    s = {"type": ["string", "null"] if nullable else "string",
         "enum": list(values) + ["UNKNOWN"] + ([None] if nullable else []), "x-atlas-enum": entries}
    return s


def request_enum(values, descriptions, nullable=False):
    s = {"type": ["string", "null"] if nullable else "string",
         "enum": list(values) + ([None] if nullable else []),
         "x-atlas-enum": [{"value": v, "description": descriptions[v]} for v in values]}
    return s


def with_desc(schema, text):
    s = {"description": text}
    s.update(schema)
    return s


# --- data: portfolio.holdings@v1 ---------------------------------------------

def holdings():
    A = "/v1/data/portfolio.holdings@v1:"
    request = root({
        "type": "object",
        "description": "Which portfolios, on which date, and which columns.",
        "additionalProperties": False,
        "required": ["portfolios"],
        "properties": {
            "portfolios": {
                "type": "array",
                "description": "Portfolio codes, as portfolio.list names them.",
                "minItems": 1,
                "maxItems": 500,
                "uniqueItems": True,
                "items": {"type": "string", "description": "A portfolio code.", "x-atlas-logical": "PortfolioCode",
                          "x-atlas-reference": "portfolio.list@v1"},
            },
            "as_of": {"type": ["string", "null"], "description": "The valuation date; the latest when null.",
                      "format": "date", "default": None},
            "asset_class": with_desc(request_enum(["credit", "rates"], {"credit": "Credit instruments.",
                                                                        "rates": "Rates instruments."}, nullable=True),
                                     "Only this asset class; every class when null."),
            "columns": {"type": ["array", "null"], "description": "The columns to return, as the reader names them; "
                                                                  "every column when null (Projectable).",
                        "items": {"type": "string", "description": "A column name.", "minLength": 1},
                        "uniqueItems": True, "default": None},
        },
    })
    request["properties"]["asset_class"]["default"] = None
    row = {
        "type": "object",
        "description": "One position: a security held in a portfolio on a date.",
        "required": ["portfolio", "as_of", "title", "market_value", "weight", "status"],
        "properties": {
            "portfolio": {"type": "string", "description": "The portfolio's code.", "x-atlas-logical": "PortfolioCode",
                          "x-atlas-key": True},
            "isin": {"type": ["string", "null"], "description": "The security's ISIN; null for cash and "
                                                                "instruments without one.",
                     "x-atlas-logical": "Isin", "x-atlas-key": True},
            "as_of": {"type": "string", "description": "The valuation date.", "format": "date", "x-atlas-key": True},
            "title": {"type": ["string", "null"], "description": "The security's name, as Harbour holds it."},
            "market_value": {"type": "object", "description": "The position's market value, in its currency.",
                             "x-atlas-logical": "Money", "x-atlas-aliases": ["mv"]},
            "weight": {"type": "number", "description": "Its share of the portfolio's market value.",
                       "x-atlas-logical": "Fraction"},
            "oas": {"type": ["number", "null"], "description": "Option-adjusted spread over the government curve.",
                    "x-atlas-logical": "Bp", "x-atlas-aliases": ["oas_bp"], "x-atlas-deprecated": None},
            "status": with_desc(result_enum(["open", "closed"], {"open": "Held at the as-of date.",
                                                                 "closed": "Sold that day, kept for the day."},
                                            aliases={"closed": ["sold"]}), "Whether the position is still held."),
            "duration": {"type": ["number", "null"], "description": "Modified duration.", "x-atlas-logical": "Years",
                         "x-atlas-deprecated": {"reason": "Use risk.duration@v1, which states its curve.",
                                                "since": "2026-09-01", "sunset": "2027-03-01", "replaced_by": None}},
        },
    }
    result = root({"type": "array", "description": "The holdings, one row per position.", "items": row})
    return {
        "identity": {"id": "portfolio.holdings", "version": 1, "title": "Portfolio holdings",
                     "summary": "Positions in one or more portfolios on a date.", "domain": "portfolio", "owner": "QFI"},
        "properties": {"family": "data", "stability": "official", "lifecycle": "stable", "visibility": "listed",
                       "traits": [trait("Projectable"), trait("Filterable"), trait("AsOf"), trait("Keyed"),
                                  trait("Handoff"), trait("Licensed")]},
        "interactions": {
            "fetch": interaction("read", "table", "chunked", A + "fetch", takes="request", returns="result",
                                 execution=("gateway", "handoff"), deadline=("PT2M", "PT30M"),
                                 encodings=("arrow", "json", "ndjson", "csv")),
            "explain": interaction("read", "record", "single", A + "explain", takes="request"),
            "export": interaction("read", "content", "transfer", A + "export", takes="request",
                                  deadline=("PT10M", "PT2H"), files=("parquet", "arrow")),
        },
        "schemas": {"request": request, "result": result},
        "errors": [
            {"code": "portfolio_not_found", "status": 404, "retryable": False,
             "description": "No portfolio has one of the codes asked for.",
             "next": "Call portfolio.list@v1:fetch for the codes you may use."},
            {"code": "warehouse_suspended", "status": 503, "retryable": True,
             "description": "The warehouse that serves the holdings is suspended.",
             "next": "Wait retry_after seconds, then repeat the call."},
        ],
        "meaning": {"grain": "portfolio × security × date", "keys": ["portfolio", "isin", "as_of"],
                    "as_of": "the portfolio's last valuation on or before as_of",
                    "source": "Harbour's portfolio holdings view"},
        "compatibility": {"request": "BACKWARD_TRANSITIVE", "result": "FULL_TRANSITIVE"},
        "usage": {
            "when": "You need the positions of named portfolios on a date.",
            "when_not": "You need an index's constituents: use index.constituents@v1.",
            "examples": [
                {"title": "Global credit, latest", "interaction": "fetch", "request": {"portfolios": ["GLOBAL-CREDIT"]}},
                {"title": "Weights on a date", "interaction": "fetch",
                 "request": {"portfolios": ["147HYC"], "as_of": "2026-10-30", "columns": ["portfolio", "isin", "weight"]}},
            ],
            "related": ["portfolio.list@v1", "index.constituents@v1"],
            "cost": "One warehouse query; large results are handed off.",
        },
        "evolution": {
            "previous_profile": "portfolio.holdings@v1#2b71c0d4e9a35f18",
            "lens": [{"op": "rename", "from": "mv", "to": "market_value"}],
            "changelog": [{"date": "2026-10-01", "change": "Renamed mv to market_value."}],
        },
        "revision": 7,
    }


# --- resource: nautilus.dashboard@v1 ------------------------------------------

def envelope_props():
    env = schemas.envelope_defs()
    return {"name": env["name"], "metadata": env["metadata"], "status": env["status"]}


def dashboard():
    A = "/v1/resource/nautilus.dashboard@v1:"
    env = envelope_props()
    vis = {"everyone": "Every signed-in person may open it.", "restricted": "Only its members may open it."}
    members = {"type": "array", "description": "The people who may open a restricted dashboard.",
               "uniqueItems": True,
               "items": {"type": "string", "description": "A member's Entra object id.", "x-atlas-logical": "Principal"}}
    record = root({
        "type": "object",
        "description": "A dashboard: its envelope, its own fields, and the status of its published copy.",
        "required": ["name", "metadata", "title", "visibility", "members"],
        "properties": {
            "name": env["name"],
            "metadata": env["metadata"],
            "title": {"type": "string", "description": "The dashboard's title.", "minLength": 1, "maxLength": 200},
            "visibility": with_desc(result_enum(["everyone", "restricted"], vis), "Who may open it."),
            "members": members,
            "status": env["status"],
        },
    })
    labels = {"type": "object", "description": "The caller's labels.",
              "additionalProperties": {"type": "string", "description": "A label's value.", "maxLength": 200}}
    req_meta = {"type": "object", "description": "Of metadata, a caller writes only labels.",
                "additionalProperties": False, "properties": {"labels": labels}}
    name = {"type": "string", "description": "The dashboard's name.", "pattern": schemas.P_RESOURCE}
    own = {
        "title": {"type": "string", "description": "The dashboard's title.", "minLength": 1, "maxLength": 200},
        "visibility": with_desc(request_enum(["everyone", "restricted"], vis), "Who may open it."),
        "members": copy.deepcopy(members),
    }
    create = root({"type": "object", "description": "A new dashboard.", "additionalProperties": False,
                   "required": ["title", "visibility"],
                   "properties": {"name": name, **copy.deepcopy(own), "metadata": req_meta}})
    update = root({"type": "object", "description": "A change to the fields the mask names, sent with If-Match.",
                   "additionalProperties": False, "required": ["name", "fields"],
                   "properties": {"name": name,
                                  "fields": {"type": "array", "description": "The field mask: the fields to change "
                                                                             "(title, visibility, members, "
                                                                             "metadata.labels).",
                                             "minItems": 1, "uniqueItems": True,
                                             "items": {"type": "string", "description": "A field path.",
                                                       "minLength": 1}},
                                  **copy.deepcopy(own), "metadata": copy.deepcopy(req_meta)}})
    by_name = root({"type": "object", "description": "One dashboard, by name.", "additionalProperties": False,
                    "required": ["name"], "properties": {"name": name}})
    listing = root({"type": "object", "description": "A page of the caller's dashboards.",
                    "additionalProperties": False,
                    "properties": {"cursor": {"type": "string", "description": "The page to read.", "minLength": 1},
                                   "page_size": {"type": "integer", "description": "Dashboards per page.", "minimum": 1,
                                                 "maximum": 200, "default": 50, "x-atlas-unit": "count"}}})
    publish = root({"type": "object", "description": "The dashboard's bundle to upload, then publish.",
                    "additionalProperties": False, "required": ["name", "bytes", "sha256"],
                    "properties": {"name": name,
                                   "bytes": {"type": "integer", "description": "The bundle's size.", "minimum": 1,
                                             "maximum": 1073741824, "x-atlas-unit": "count"},
                                   "sha256": {"type": "string", "description": "The bundle's SHA-256, lowercase hex.",
                                              "pattern": schemas.P_SHA256}}})
    share = root({"type": "object", "description": "Who may open the dashboard.", "additionalProperties": False,
                  "required": ["name", "visibility"],
                  "properties": {"name": name, "visibility": copy.deepcopy(own["visibility"]),
                                 "members": copy.deepcopy(members)}})
    return {
        "identity": {"id": "nautilus.dashboard", "version": 1, "title": "Dashboard",
                     "summary": "A Nautilus dashboard: its title, who may open it, and its published copy.",
                     "domain": "nautilus", "owner": "Nautilus"},
        "properties": {"family": "resource", "stability": "official", "lifecycle": "stable", "visibility": "listed",
                       "traits": [trait("Paged"), trait("Keyed")]},
        "interactions": {
            "get": interaction("read", "record", "single", A + "get", takes="get_request", returns="resource"),
            "list": interaction("read", "collection", "single", A + "list", takes="list_request", returns="resource"),
            "create": interaction("write", "record", "single", A + "create", takes="create_request", returns="resource"),
            "update": interaction("write", "record", "single", A + "update", takes="update_request", returns="resource"),
            "delete": interaction("irreversible", "record", "single", A + "delete", takes="get_request",
                                  returns="resource"),
            "publish": interaction("write", "record", "transfer", A + "publish", takes="publish_request",
                                   returns="resource", deadline=("PT5M", "PT30M")),
            "share": interaction("write", "record", "single", A + "share", takes="share_request", returns="resource"),
            "watch": interaction("read", "record", "subscription", A + "watch", takes="get_request", returns="resource"),
        },
        "schemas": {"resource": record, "get_request": by_name, "list_request": listing, "create_request": create,
                    "update_request": update, "publish_request": publish, "share_request": share},
        "errors": [{"code": "dashboard_too_large", "status": 413, "retryable": False,
                    "description": "The bundle is larger than a dashboard may be.",
                    "next": "Remove data from the bundle, or fetch it at run time."}],
        "meaning": {"name": "nautilus/dashboards/{id}", "immutable": ["name"], "stored_version": 1,
                    "converges": "a published copy of the bundle; its condition `published`"},
        "compatibility": {"resource": "FULL_TRANSITIVE"},
        "usage": {
            "when": "You build, share or publish a dashboard on Nautilus.",
            "examples": [{"title": "Read one dashboard", "interaction": "get",
                          "request": {"name": "nautilus/dashboards/d1"}},
                         {"title": "Rename a dashboard", "interaction": "update",
                          "request": {"name": "nautilus/dashboards/d1", "fields": ["title"], "title": "Credit risk"}}],
            "related": ["nautilus.dashboard.published@v1"],
        },
        "revision": 1,
    }


# --- action: astronomer.deployment@v1 -----------------------------------------

def deployment():
    A = "/v1/action/astronomer.deployment@v1:"
    request = root({"type": "object", "description": "The deployment to restart.", "additionalProperties": False,
                    "required": ["deployment"],
                    "properties": {"deployment": {"type": "string", "description": "The deployment's id in "
                                                                                    "Astronomer.", "minLength": 1}}})
    plan = root({"type": "object", "description": "What restarting will do, changing nothing yet.",
                 "required": ["plan", "deployment", "will", "expires"],
                 "properties": {
                     "plan": {"type": "string", "description": "The plan's id, which approve and restart name.",
                              "minLength": 1},
                     "deployment": {"type": "string", "description": "The deployment it restarts."},
                     "will": {"type": "array", "description": "What restarting will do, step by step.",
                              "items": {"type": "string", "description": "One step."}},
                     "expires": {"type": "string", "description": "When the plan must be planned again.",
                                 "format": "date-time"},
                 }})
    approve = root({"type": "object", "description": "Approve a stored plan, as someone other than its requester.",
                    "additionalProperties": False, "required": ["plan"],
                    "properties": {"plan": {"type": "string", "description": "The plan's id.", "minLength": 1}}})
    restart = root({"type": "object", "description": "Run an approved plan.", "additionalProperties": False,
                    "required": ["plan"],
                    "properties": {"plan": {"type": "string", "description": "The plan's id.", "minLength": 1}}})
    approval = root({"type": "object", "description": "The plan's approval.",
                     "required": ["plan", "approved_by", "approved"],
                     "properties": {"plan": {"type": "string", "description": "The plan's id."},
                                    "approved_by": {"type": "string", "description": "Who approved it.",
                                                    "x-atlas-logical": "Principal"},
                                    "approved": {"type": "string", "description": "When.", "format": "date-time"}}})
    receipt = root({"type": "object", "description": "What actually happened: the evidence the audit keeps.",
                    "required": ["restarted_at"],
                    "properties": {
                        "restarted_at": {"type": "string", "description": "When the scheduler came back.",
                                         "format": "date-time"},
                        "scheduler_pods": {"type": "integer", "description": "Scheduler pods running after.",
                                           "minimum": 0, "x-atlas-unit": "count"},
                    }})
    return {
        "identity": {"id": "astronomer.deployment", "version": 1, "title": "Astronomer deployment",
                     "summary": "Restart an Astronomer deployment's scheduler and workers.", "domain": "astronomer",
                     "owner": "Data Platform"},
        "properties": {"family": "action", "stability": "official", "lifecycle": "preview", "visibility": "listed",
                       "traits": [trait("DryRunnable"), trait("NeedsApproval")]},
        "interactions": {
            "plan": interaction("read", "record", "single", A + "plan", takes="request", returns="plan"),
            "approve": interaction("write", "record", "single", A + "approve", takes="approve_request",
                                   returns="approval"),
            "restart": interaction("external", "record", "operation", A + "restart", takes="restart_request",
                                   returns="receipt", deadline=("PT10M", "PT1H")),
        },
        "schemas": {"request": request, "plan": plan, "approve_request": approve, "approval": approval,
                    "restart_request": restart, "receipt": receipt},
        "errors": [{"code": "deployment_busy", "status": 409, "retryable": True,
                    "description": "The deployment is already restarting.",
                    "next": "Wait retry_after seconds, then plan again."}],
        "meaning": {"effects": {"system": "Astronomer", "changes": "restarts the deployment's scheduler and workers",
                                "environments": ["dv", "ac", "pd"], "reversible": False, "undo": None},
                    "policy": {"approval": ["ac", "pd"], "refused_unless_person": ["pd"]}},
        "compatibility": {"request": "BACKWARD_TRANSITIVE", "plan": "FULL_TRANSITIVE", "receipt": "FULL_TRANSITIVE"},
        "usage": {
            "when": "A deployment's scheduler is stuck and its owner asked for a restart.",
            "when_not": "Tasks fail on their own: read their logs instead.",
            "examples": [{"title": "Plan a restart", "interaction": "plan", "request": {"deployment": "dep-7f2"}}],
        },
        "revision": 1,
    }


# --- event: data.landed@v1 ------------------------------------------------

def landed():
    A = "/v1/event/data.landed@v1:"
    payload = root({"type": "object", "description": "A source's data for a date is complete.",
                    "required": ["source", "as_of"],
                    "properties": {
                        "source": {"type": "string", "description": "The source that landed.",
                                   "x-atlas-reference": "data.sources@v1", "minLength": 1},
                        "as_of": {"type": "string", "description": "The date whose data landed.", "format": "date"},
                        "rows": {"type": ["integer", "null"], "description": "Rows landed, where the source counts "
                                                                             "them.", "minimum": 0,
                                 "x-atlas-unit": "count"},
                    }})
    publish = copy.deepcopy(payload)
    publish["description"] = "The event to publish: its payload."
    publish["additionalProperties"] = False
    attributes = root({"type": "object", "description": "The event's CloudEvents attributes beyond the envelope.",
                       "required": ["subject"],
                       "properties": {"subject": {"type": "string", "description": "The source's id.",
                                                  "minLength": 1}}})
    subscribe = root({"type": "object", "description": "Which landings to receive, and whether to keep them while "
                                                       "away.", "additionalProperties": False,
                      "properties": {
                          "filter": {"type": "object", "description": "Receive only these, filtered at Service Bus.",
                                     "additionalProperties": False,
                                     "properties": {"subject": {"type": "string", "description": "Only this source.",
                                                                "minLength": 1}}},
                          "durable": {"type": "string", "description": "A durable subscription's name.",
                                      "pattern": "^[a-z][a-z0-9-]{0,49}$"},
                      }})
    return {
        "identity": {"id": "data.landed", "version": 1, "title": "Data landed",
                     "summary": "A source's data for a date has landed and is complete.", "domain": "data",
                     "owner": "Harbour"},
        "properties": {"family": "event", "stability": "official", "lifecycle": "stable", "visibility": "listed",
                       "traits": [trait("Ordered")]},
        "interactions": {
            "subscribe": interaction("read", "record", "subscription", A + "subscribe", takes="subscribe_request",
                                     returns="payload"),
            "publish": interaction("external", "record", "single", A + "publish", takes="publish_request"),
        },
        "schemas": {"payload": payload, "attributes": attributes, "subscribe_request": subscribe,
                    "publish_request": publish},
        "errors": [],
        "meaning": {"emitted": "when a source's data for a date is complete", "subject": "the source",
                    "ordering": "per subject", "retention": "P7D", "history": "data.status@v1"},
        "compatibility": {"payload": "FULL_TRANSITIVE"},
        "usage": {
            "when": "You refresh something when a source's data lands.",
            "when_not": "You need what landed in the past: read data.status@v1.",
            "examples": [{"title": "Index landings", "interaction": "subscribe",
                          "request": {"filter": {"subject": "index"}}}],
            "related": ["data.status@v1"],
        },
        "revision": 1,
    }


def complete_profiles():
    """name → profile without its fingerprint."""
    return {"data": holdings(), "resource": dashboard(), "action": deployment(), "event": landed()}


# --- small profiles for vectors ------------------------------------------------

def minimal_data_profile(cap_id, version, result_row, request=None, lens=None, previous=None, traits=(),
                         title="A capability of the reference vectors", delivery="chunked"):
    """A valid data profile around one table result: what the resolution and
    handoff vectors fingerprint their writer and reader profiles from."""
    A = f"/v1/data/{cap_id}@v{version}:"
    req = request or root({"type": "object", "description": "No parameters.", "additionalProperties": False,
                           "properties": {}})
    p = {
        "identity": {"id": cap_id, "version": version, "title": title, "summary": title + ".",
                     "domain": cap_id.split(".")[0], "owner": "Atlas"},
        "properties": {"family": "data", "stability": "official", "lifecycle": "stable", "visibility": "listed",
                       "traits": [trait(t) for t in traits]},
        "interactions": {"fetch": interaction("read", "table", delivery, A + "fetch", takes="request",
                                              returns="result", encodings=("arrow", "ndjson", "json"))},
        "schemas": {"request": req, "result": root({"type": "array", "description": "The rows.",
                                                    "items": copy.deepcopy(result_row)})},
        "errors": [],
        "meaning": {"grain": "one row per key", "keys": [], "source": "the reference vectors"},
    }
    if lens is not None or previous is not None:
        p["evolution"] = {}
        if previous is not None:
            p["evolution"]["previous_profile"] = previous
        if lens is not None:
            p["evolution"]["lens"] = copy.deepcopy(lens)
    return p
