"""§9.3.1: the JSON Schemas of the protocol's 21 documents, built here so that
`references/schemas/*.schema.json` regenerate byte-identically.

Conventions (README.md, "Schemas"):

- Every schema is JSON Schema 2020-12 with `$id`
  `https://atlas.jhg.com/protocol/1.0/<file>`; documents refer to each other
  by those `$id`s.
- Every document the gateway sends is open (tolerant reader, §9.1.2 rule 7);
  only request bodies close themselves with `"additionalProperties": false`.
- Every enum in a document the gateway sends includes `UNKNOWN`, to which a
  client maps a value it does not know (§9.7.10 by §9.5.5), so a newer
  gateway's document still validates; no request's enum includes it (the
  rule §9.2.3 sets for profiles, applied to the protocol's own documents,
  Q-DOC-1).
- Strings use `format` for the formats of §9.2.2, validated as assertions,
  and `pattern` (written as I-Regexps) for the other grammars of
  `grammar.abnf`.
- Schemas that profiles must embed exactly (the resource envelope, the
  built-ins) are written in the Atlas Schema profile itself.
"""

import copy

from . import problems
from .profilecheck import DRAFT, VOCABULARY

BASE = "https://atlas.jhg.com/protocol/1.0/"

# Grammars of grammar.abnf without a §9.2.2 format, as I-Regexp patterns.
SEG = "[a-z](-?[a-z0-9])*"
CAP = SEG + "(\\." + SEG + ")+"
P_SEGMENT = "^" + SEG + "$"
P_FINGERPRINT = "^[0-9a-f]{16}$"
P_PROTOCOL = "^atlas/[0-9]+\\.[0-9]+$"
P_PROPERTY = "^[a-z][a-z0-9_]*$"
P_CODE = "^[a-z][a-z0-9_]*$"
P_VERSION = "^v[1-9][0-9]*$"
P_ETAG = '^(W/)?"[!#-~]*"$'
P_RESOURCE = "^" + SEG + "(/" + SEG + "/[a-z0-9][a-z0-9-]*)+$"
P_OPERATION = "^operations/[a-z0-9][a-z0-9-]*$"
P_DATASCHEMA = "^urn:atlas:profile:" + CAP + "@v[1-9][0-9]*:[0-9a-f]{16}$"
P_EVENT_TYPE = "^com\\.jhg\\.fiquant\\." + CAP + "\\.v[1-9][0-9]*$"
P_PROBLEM_TYPE = "^https://atlas\\.jhg\\.com/problems/[a-z][a-z0-9_]*$"
P_SHA256 = "^[0-9a-f]{64}$"
P_BASE64 = "^([A-Za-z0-9+/]{4})*([A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=)?$"
P_TRAIT = "^[A-Z][A-Za-z0-9]*$"
P_BUILTIN = "^atlas(\\." + SEG + ")+$"
P_ESTIMATE = "^[0-9]{1,12}(\\.[0-9]{1,3})?$"

FAMILIES = ["data", "resource", "action", "event"]
STABILITIES = ["official", "community", "upstream"]
LIFECYCLES = ["preview", "stable", "deprecated", "retired"]
VISIBILITIES = ["listed", "confidential"]
EFFECTS = ["read", "write", "external", "irreversible"]
RESULTS = ["table", "record", "collection", "content", "ticket"]
DELIVERIES = ["single", "chunked", "subscription", "operation", "transfer"]
EXECUTIONS = ["gateway", "handoff", "client"]
ENCODINGS = ["json", "ndjson", "arrow", "csv", "parquet", "cloudevents+json"]
PROTOCOLS = ["https", "amqp", "blob", "snowflake", "mcp"]
AUTHS = ["bearer", "entra-scope", "ticket"]
STATES = ["pending", "running", "succeeded", "failed", "cancelled"]
MODES = ["BACKWARD", "BACKWARD_TRANSITIVE", "FORWARD", "FORWARD_TRANSITIVE", "FULL", "FULL_TRANSITIVE"]
LENS_OPS = ["rename", "alias_value", "convert_unit", "promote", "default", "drop"]
UNIT_NAMES = ["Bp", "Percent", "Fraction", "Years", "bp", "percent", "fraction", "years", "days", "seconds", "count",
              "currency_unit"]
PROMOTE_TYPES = ["integer", "number", "date", "date-time"]
LEVELS = ["core", "streaming", "columnar", "bulk", "events", "handoff", "broker"]
ACCESS_STATUSES = ["available", "partial", "not_granted", "denied", "not_in_environment", "limited", "deprecated"]
LIMIT_KINDS = ["rate", "concurrency", "output", "budget", "confirmation"]
CLIENT_SYNTAXES = ["cli", "python", "node", "go", "mcp"]
SNOWFLAKE_TYPES = ["FIXED", "REAL", "TEXT", "BOOLEAN", "DATE", "TIME", "TIMESTAMP_NTZ", "TIMESTAMP_LTZ", "TIMESTAMP_TZ",
                   "BINARY", "VARIANT", "OBJECT", "ARRAY"]


def ref(file, name=None):
    return {"$ref": BASE + file + ("#/$defs/" + name if name else "")}


def local(name):
    return {"$ref": "#/$defs/" + name}


def string(desc=None, **kw):
    s = {"type": "string"}
    s.update(kw)
    if desc:
        s["description"] = desc
    return s


def fmt(f, desc=None, nullable=False):
    s = {"type": ["string", "null"] if nullable else "string", "format": f}
    if desc:
        s["description"] = desc
    return s


def pat(p, desc=None, nullable=False):
    s = {"type": ["string", "null"] if nullable else "string", "pattern": p}
    if desc:
        s["description"] = desc
    return s


def integer(desc=None, minimum=None, maximum=None, nullable=False, unit=None, default=None):
    s = {"type": ["integer", "null"] if nullable else "integer"}
    if minimum is not None:
        s["minimum"] = minimum
    if maximum is not None:
        s["maximum"] = maximum
    if default is not None:
        s["default"] = default
    if unit:
        s["x-atlas-unit"] = unit
    if desc:
        s["description"] = desc
    return s


def boolean(desc=None, default=None):
    s = {"type": "boolean"}
    if default is not None:
        s["default"] = default
    if desc:
        s["description"] = desc
    return s


def open_enum(values, desc=None, nullable=False, describe=None):
    """An enum a gateway-sent document carries: UNKNOWN included (Q-DOC-1),
    with x-atlas-enum giving each value's meaning."""
    s = {"type": ["string", "null"] if nullable else "string", "enum": list(values) + ["UNKNOWN"] + ([None] if nullable else [])}
    entries = [{"value": v, **({"description": describe[v]} if describe and v in describe else {})} for v in values]
    entries.append({"value": "UNKNOWN", "description": "A value this reader does not know (§9.5.5)."})
    s["x-atlas-enum"] = entries
    if desc:
        s["description"] = desc
    return s


def closed_enum(values, desc=None):
    """An enum a request carries: never UNKNOWN (§9.2.3)."""
    s = {"type": "string", "enum": list(values)}
    if desc:
        s["description"] = desc
    return s


def obj(props, required=(), desc=None, closed=False, title=None):
    s = {"type": "object"}
    if title:
        s["title"] = title
    if desc:
        s["description"] = desc
    if closed:
        s["additionalProperties"] = False
    if required:
        s["required"] = list(required)
    s["properties"] = props
    return s


def array(items, desc=None, min_items=None, max_items=None, unique=False, nullable=False):
    s = {"type": ["array", "null"] if nullable else "array"}
    if min_items is not None:
        s["minItems"] = min_items
    if max_items is not None:
        s["maxItems"] = max_items
    if unique:
        s["uniqueItems"] = True
    s["items"] = items
    if desc:
        s["description"] = desc
    return s


def cursor(desc="The opaque cursor of the page to read; absent for the first page."):
    return string(desc, minLength=1)


def document(file, title, description, body, defs=None):
    doc = {"$schema": DRAFT, "$id": BASE + file, "title": title, "description": description}
    doc.update(body)
    if defs:
        doc["$defs"] = defs
    return doc


def id_or_pinned(desc):
    return {"anyOf": [fmt("capability-id"), fmt("pinned-id")], "description": desc}


# --- binding ---------------------------------------------------------------

def binding():
    return document(
        "binding.schema.json", "Binding",
        "Where and how one interaction is served (§4.2, §9.6.1). A client reaches an interaction only through a "
        "binding its profile or the bootstrap serves, and picks the first it supports, in the order given; it "
        "never builds an address.",
        {
            "type": "object",
            "required": ["protocol", "server", "address", "auth"],
            "properties": {
                "protocol": open_enum(PROTOCOLS, "How the interaction travels. `sse` and `wss` are reserved for a later "
                                      "minor version (§9.6.5)."),
                "server": pat(P_SEGMENT, "A server the bootstrap names in `servers`; the interaction's URL is that "
                              "server's `url` followed by `address` (§9.6.1)."),
                "address": string("A path (beginning with `/` for https), channel or topic.", minLength=1),
                "method": open_enum(["POST", "GET"], "For https: the HTTP method. Every atlas/1.0 interaction binding "
                                    "declares POST (§9.6.2)."),
                "encodings": array(open_enum(ENCODINGS), "The encodings this binding serves, in its preference order "
                                   "(§9.6.4).", unique=True),
                "auth": open_enum(AUTHS, "How the caller authenticates: its sign-in (`bearer`), its own token for the "
                                  "system (`entra-scope`), or a credential the gateway issued (`ticket`)."),
                "delivery": open_enum(DELIVERIES, "The delivery this binding answers with, where it differs from its "
                                      "interaction's (a fallback that answers an operation for a stream, §2.1.D; "
                                      "Q-DOC-4)."),
            },
            "allOf": [
                {
                    "if": {"required": ["protocol"], "properties": {"protocol": {"const": "https"}}},
                    "then": {"required": ["method"], "properties": {"address": {"pattern": "^/"}}},
                }
            ],
        },
    )


# --- bootstrap ---------------------------------------------------------------

def bootstrap():
    return document(
        "bootstrap.schema.json", "Bootstrap",
        "GET /.well-known/atlas (§9.6.1): the gateway's protocol versions and minimum, its environment, its named "
        "servers, the bindings of every built-in capability, how to sign in, and the catalogue's current ETag. "
        "Served without sign-in, with a strong ETag.",
        {
            "type": "object",
            "required": ["protocols", "minimum_protocol", "environment", "servers", "builtins", "sign_in",
                         "catalog_etag"],
            "properties": {
                "protocols": array(pat(P_PROTOCOL), "Every protocol version the gateway serves (§9.B).", min_items=1,
                                   unique=True),
                "minimum_protocol": pat(P_PROTOCOL, "The lowest protocol a client may declare; below it a call is "
                                        "upgrade_required (426)."),
                "environment": pat(P_SEGMENT, "The environment's name: dv, ac, pd, local or conformance. A client "
                                   "accepts an emulator connect handover only when it is conformance or local "
                                   "(§9.7.8). It also names the sign-in file token-<environment>.json (Q-CLI-1)."),
                "servers": {
                    "type": "object",
                    "description": "Each named server's base URL, with no trailing `/` (§9.6.1).",
                    "minProperties": 1,
                    "propertyNames": {"pattern": P_SEGMENT},
                    "additionalProperties": obj(
                        {
                            "url": {"type": "string", "format": "uri", "pattern": "[^/]$",
                                    "description": "The server's base URL."},
                            "description": string("What the server is."),
                        },
                        required=["url"],
                    ),
                },
                "builtins": {
                    "type": "object",
                    "description": "The bindings of every built-in capability of §9.3.2, keyed by its id.",
                    "propertyNames": {"pattern": P_BUILTIN},
                    "additionalProperties": local("builtin"),
                },
                "sign_in": local("sign_in"),
                "catalog_etag": pat(P_ETAG, "The catalogue's current ETag, as an entity-tag (with its quotes)."),
            },
        },
        defs={
            "builtin": obj(
                {
                    "version": integer("The built-in's version: `atlas.whoami@v1` is version 1.", minimum=1),
                    "interactions": {
                        "type": "object",
                        "description": "Each interaction's bindings, in preference order.",
                        "minProperties": 1,
                        "propertyNames": {"pattern": P_SEGMENT},
                        "additionalProperties": obj(
                            {"bindings": array(ref("binding.schema.json"), min_items=1)}, required=["bindings"]
                        ),
                    },
                },
                required=["version", "interactions"],
            ),
            "sign_in": obj(
                {
                    "authority": fmt("uri", "The token issuer: Entra's authority, or the conformance gateway itself "
                                     "(§9.8.4)."),
                    "client_id": string("The public client's id every Atlas client signs in as (§6.2.2).", minLength=1),
                    "scopes": array(string(minLength=1), "The scopes to ask for; their audience is the Atlas Gateway.",
                                    min_items=1),
                    "token_endpoint": fmt("uri", "Where a refresh token is redeemed (RFC 6749 section 6, §9.7.3)."),
                    "authorization_endpoint": fmt("uri", "Where a person signs in with PKCE (RFC 7636, RFC 8252)."),
                    "device_authorization_endpoint": fmt("uri", "Where a device-code sign-in starts (RFC 8628)."),
                },
                required=["authority", "client_id", "scopes", "token_endpoint"],
                desc="How to sign in (§6.2.2, §9.7.3). Every endpoint is given, so a client never builds one (Q-DOC-2).",
            ),
        },
    )


# --- profile -----------------------------------------------------------------

def _lens_op(op, props, required):
    body = {"op": {"const": op}}
    body.update(props)
    return obj(body, required=["op"] + required)


def profile():
    field = pat(P_PROPERTY, "A top-level property of the record (§9.5.1).")
    return document(
        "profile.schema.json", "Profile",
        "A capability's contract (§3.1), served by atlas.describe and cached forever by its fingerprint (§9.4). "
        "Governance and availability are served beside it, never in it. The schemas it holds are in the Atlas "
        "Schema profile (atlas-schema.schema.json), which the gate and registration check; a client validates the "
        "profile's structure here and does not refuse a newer profile's schemas (Q-DOC-3).",
        {
            "type": "object",
            "required": ["identity", "properties", "interactions", "schemas", "errors", "meaning", "fingerprint"],
            "properties": {
                "identity": obj(
                    {
                        "id": fmt("capability-id", "The capability's id."),
                        "version": integer("Its major version: 1 for @v1.", minimum=1),
                        "title": string("Prose: removed by the projection (§9.4.1)."),
                        "summary": string("One sentence of at most 200 characters (§3.1.4a). Prose."),
                        "domain": pat(P_SEGMENT, "The first segment of `id`."),
                        "owner": string("Who answers for its meaning. Prose.", minLength=1),
                    },
                    required=["id", "version", "domain", "owner"],
                ),
                "properties": obj(
                    {
                        "family": open_enum(FAMILIES, "Its surface (§2.1.B)."),
                        "stability": open_enum(STABILITIES, "What it promises (§2.1.C)."),
                        "lifecycle": open_enum(LIFECYCLES, "Where it is in its life."),
                        "deprecated": fmt("date", "When it was deprecated: what Deprecation reports (§3.3.9, Q-DOC-5)."),
                        "sunset": fmt("date", "When a deprecated version ends."),
                        "replaced_by": id_or_pinned("What replaces a deprecated version."),
                        "visibility": open_enum(VISIBILITIES, "Whether a caller who cannot use it may learn it exists."),
                        "traits": array(local("trait"), "The uniform behaviours it implements (§2.1.E).", unique=True),
                    },
                    required=["family", "stability", "lifecycle", "visibility", "traits"],
                ),
                "interactions": {
                    "type": "object",
                    "description": "Each interaction, by name.",
                    "minProperties": 1,
                    "propertyNames": {"pattern": P_SEGMENT},
                    "additionalProperties": local("interaction"),
                },
                "schemas": {
                    "type": "object",
                    "description": "The family's schemas by name, each in the Atlas Schema profile (§9.2). A table's "
                                   "Arrow schema is derived from its result schema (§9.2.7), not written here "
                                   "(Q-DOC-6).",
                    "propertyNames": {"pattern": P_PROPERTY},
                    "additionalProperties": {"type": "object"},
                },
                "errors": array(local("error"), "Every problem code it can return beyond the registry (§9.3.3)."),
                "meaning": {
                    "type": "object",
                    "description": "What changes only by a new version: for data its grain, keys, as-of rule and "
                                   "source; for a resource its name pattern and immutable fields; for an action its "
                                   "effects and policy; for an event when it is emitted, its subject, ordering, "
                                   "retention and history (§3.1.2).",
                },
                "compatibility": {
                    "type": "object",
                    "description": "The mode each schema is held to within the version (§3.3.2a).",
                    "propertyNames": {"pattern": P_PROPERTY},
                    "additionalProperties": open_enum(MODES),
                },
                "usage": local("usage"),
                "evolution": local("evolution"),
                "fingerprint": pat(P_FINGERPRINT, "§9.4.2; a client recomputes it and refuses a mismatch."),
                "revision": integer("The prose's revision: it moves without the fingerprint.", minimum=0),
            },
        },
        defs={
            "trait": obj(
                {
                    "name": pat(P_TRAIT, "The trait's name (§2.1.E); a client declares the lowercased names it "
                                "understands in Atlas-Client-Features."),
                    "critical": boolean("When true, a client that does not implement it must not call the capability "
                                        "(critical_trait_unsupported)."),
                },
                required=["name", "critical"],
            ),
            "interaction": obj(
                {
                    "effect": open_enum(EFFECTS, "How careful a caller must be (§2.1.D)."),
                    "result": open_enum(RESULTS, "The shape of what comes back."),
                    "delivery": open_enum(DELIVERIES, "How it comes back."),
                    "execution": array(open_enum(EXECUTIONS), "Who does the work and moves the bytes.", min_items=1,
                                       unique=True),
                    "takes": pat(P_PROPERTY, "The name of the schema in `schemas` its request is validated against."),
                    "returns": pat(P_PROPERTY, "The name of the schema in `schemas` its result is written in."),
                    "deadline": obj(
                        {
                            "default": fmt("duration", "The deadline a call gets when it names none."),
                            "max": fmt("duration", "The longest deadline a call may have."),
                        },
                        required=["default", "max"],
                    ),
                    "encodings": array(open_enum(ENCODINGS), "For a transfer or export: the file formats it writes.",
                                       unique=True),
                    "bindings": array(ref("binding.schema.json"), "Where it is served, in preference order.",
                                      min_items=1),
                },
                required=["effect", "result", "delivery", "execution", "deadline", "bindings"],
            ),
            "error": obj(
                {
                    "code": pat(P_CODE, "The problem code."),
                    "status": integer("Its HTTP status.", minimum=100, maximum=599),
                    "retryable": boolean("Whether the same request may succeed if repeated."),
                    "description": string("What it means. Prose."),
                    "next": string("The step to take. Prose."),
                },
                required=["code", "status", "retryable"],
            ),
            "usage": obj(
                {
                    "when": string("When to use it. Prose."),
                    "when_not": string("When not to. Prose."),
                    "examples": array(
                        obj(
                            {
                                "title": string("What the example does."),
                                "interaction": pat(P_SEGMENT, "The interaction it calls."),
                                "request": {"type": "object",
                                            "description": "A request valid against that interaction's request schema."},
                            },
                            required=["title", "interaction", "request"],
                        )
                    ),
                    "related": array(id_or_pinned("A related capability.")),
                    "cost": {"type": ["string", "object"], "description": "What a call costs; for Metered, its unit and "
                                                                          "price."},
                    "skill": string("The agent's skill text, managed by administrators."),
                },
                desc="What atlas.guide is written from (§2.6); removed by the projection.",
            ),
            "evolution": obj(
                {
                    "previous_profile": fmt("exact-id", "The profile this one replaces within the version."),
                    "lens": array(local("lens_op"), "The lens from previous_profile to this profile (§9.5.3)."),
                    "previous_version": fmt("pinned-id", "For a version after @v1: the version it replaces "
                                            "(§9.5.2 step 2)."),
                    "version_lens": array(local("lens_op"), "The lens from previous_version, where one can express "
                                          "it; only the gateway resolves across versions (Q-DOC-7)."),
                    "changelog": array({"type": "object"}, "Why it changed. Prose: removed by the projection."),
                },
            ),
            "lens_op": {
                "description": "One lens operation (§9.5.3). An operation a reader does not know reads as `UNKNOWN`, "
                               "and the gateway resolves for that reader (§9.5.6).",
                "type": "object",
                "required": ["op"],
                "oneOf": [
                    _lens_op("rename", {"from": pat(P_PROPERTY), "to": pat(P_PROPERTY)}, ["from", "to"]),
                    _lens_op("alias_value", {"field": field, "from": {}, "to": {}}, ["field", "from", "to"]),
                    _lens_op("convert_unit", {"field": field, "from": open_enum(UNIT_NAMES), "to": open_enum(UNIT_NAMES)},
                             ["field", "from", "to"]),
                    _lens_op("promote", {"field": field, "from": open_enum(PROMOTE_TYPES), "to": open_enum(PROMOTE_TYPES)},
                             ["field", "from", "to"]),
                    _lens_op("default", {"field": field, "value": {}}, ["field", "value"]),
                    _lens_op("drop", {"field": field}, ["field"]),
                    _lens_op("UNKNOWN", {}, []),
                ],
            },
        },
    )


# --- catalogue page, governance, describe, explain ------------------------

def house_rules():
    return obj(
        {
            "revision": string("The rules' revision.", minLength=1),
            "rules": array(
                obj(
                    {
                        "text": string("The rule, in words.", minLength=1),
                        "capabilities": array(fmt("capability-id"), "The capabilities it names; it reaches a caller "
                                              "only when the caller can use every one."),
                    },
                    required=["text", "capabilities"],
                )
            ),
        },
        required=["revision", "rules"],
        desc="House rules (§2.6, §9.3.2).",
    )


def catalog_page():
    return document(
        "catalog-page.schema.json", "Catalogue page",
        "atlas.catalog and atlas.search (§9.3.2): the capabilities in the caller's working view, a page at a time, "
        "with the house rules that name a capability on the page. atlas.search's items carry `score` and `why`.",
        {
            "type": "object",
            "required": ["items", "next_cursor", "house_rules"],
            "properties": {
                "items": array(local("item")),
                "next_cursor": {"type": ["string", "null"], "minLength": 1,
                                "description": "The cursor of the next page; null on the last."},
                "house_rules": local("house_rules"),
            },
        },
        defs={
            "item": obj(
                {
                    "id": fmt("capability-id"),
                    "version": integer(minimum=1),
                    "exact": fmt("exact-id", "Its current profile, with the fingerprint."),
                    "title": string(),
                    "summary": string(),
                    "family": open_enum(FAMILIES),
                    "lifecycle": open_enum(LIFECYCLES),
                    "interactions": {
                        "type": "object",
                        "description": "The interactions the caller may use, with their effect classes.",
                        "propertyNames": {"pattern": P_SEGMENT},
                        "additionalProperties": obj({"effect": open_enum(EFFECTS)}, required=["effect"]),
                    },
                    "score": {"type": "number", "description": "atlas.search: how well it matched."},
                    "why": string("atlas.search: why it matched."),
                },
                required=["id", "version", "exact", "summary", "family", "interactions"],
            ),
            "house_rules": house_rules(),
        },
    )


def limit_def():
    return obj(
        {
            "interaction": pat(P_SEGMENT),
            "kind": open_enum(LIMIT_KINDS, "§6.2.6."),
            "per_minute": integer(minimum=0),
            "max": integer(minimum=0),
            "max_rows": integer(minimum=0),
            "max_bytes": integer(minimum=0),
            "remaining": {"type": "number"},
            "amount": fmt("decimal"),
            "unit": string(),
            "period": string(),
            "set_by": string("Who set it: `role:<name>`, `user:<oid>`."),
            "resets": fmt("date-time"),
        },
        required=["kind"],
    )


def governance():
    return document(
        "governance.schema.json", "Governance",
        "The caller's access, limits and approvals for one capability (§3.1.1), served beside its profile by "
        "atlas.describe, never part of it or its fingerprint.",
        {
            "type": "object",
            "required": ["capability", "governance_revision", "for_caller"],
            "properties": {
                "capability": fmt("pinned-id"),
                "governance_revision": string("Recorded in every audit record; Atlas-Governance-Revision.", minLength=1),
                "for_caller": obj(
                    {
                        "interactions": {
                            "type": "object",
                            "description": "How the caller holds each interaction it may use.",
                            "propertyNames": {"pattern": P_SEGMENT},
                            "additionalProperties": obj({"held_by": string("`role:<name>`, `user:<oid>` or "
                                                                           "`everyone`.")}, required=["held_by"]),
                        },
                        "limits": array(local("limit")),
                        "approval": {"type": ["object", "null"], "description": "The approval policy, or null."},
                    },
                    required=["interactions", "limits", "approval"],
                ),
            },
        },
        defs={"limit": limit_def()},
    )


def describe():
    return document(
        "describe.schema.json", "Describe answer",
        "atlas.describe (§9.3.2, §2.6): the profile, the caller's governance, its availability, and the calling "
        "client's eligibility per interaction.",
        {
            "type": "object",
            "required": ["profile", "governance", "availability", "eligibility"],
            "properties": {
                "profile": ref("profile.schema.json"),
                "governance": ref("governance.schema.json"),
                "availability": open_enum(["available", "unavailable"], "Whether it can be served now (§3.1.1)."),
                "eligibility": array(local("eligibility"), "For the calling client, from its Atlas-Client-Features."),
            },
        },
        defs={
            "eligibility": obj(
                {
                    "interaction": pat(P_SEGMENT),
                    "eligible": array(obj({"binding": string(), "encoding": string(), "limits": {"type": "object"}},
                                          required=["binding"]), "Bindings it can use, in preference order."),
                    "ineligible": array(obj({"binding": string(), "missing": array(string()), "hint": string()},
                                            required=["binding", "missing"]), "Bindings it cannot, and what is "
                                        "missing."),
                },
                required=["interaction", "eligible", "ineligible"],
            )
        },
    )


def explain():
    return document(
        "explain.schema.json", "Explain answer",
        "Every data capability's `explain` (§2.2, §2.6): what `fetch` would do with this request, without doing "
        "it: the plan (for a SQL source the exact statement, byte for byte), the result schema of what would come "
        "back, the execution it would choose, and the estimate.",
        {
            "type": "object",
            "required": ["address", "profile", "plan", "result_schema", "execution", "estimate"],
            "properties": {
                "address": fmt("address", "The interaction it explains: `<id>@vN:fetch`."),
                "profile": fmt("exact-id", "The writer profile the plan is made under."),
                "plan": obj(
                    {
                        "language": open_enum(["sql", "http"], "What the plan is written in."),
                        "dialect": string("For SQL: the engine's dialect (`snowflake`)."),
                        "statement": string("The exact statement `fetch` would run, every column aliased and cast."),
                        "query_tag": string("The tag the source records."),
                    },
                    required=["language"],
                ),
                "result_schema": {"type": "object", "description": "The schema of what would come back (the "
                                                                   "requested columns), in the Atlas Schema profile."},
                "execution": open_enum(EXECUTIONS, "The execution it would choose."),
                "estimate": obj(
                    {
                        "rows": integer(minimum=0, nullable=True),
                        "bytes": integer(minimum=0, nullable=True),
                        "cost": {"anyOf": [{"type": "null"}, ref("problem.schema.json", "estimate")]},
                    },
                    required=["rows", "bytes", "cost"],
                ),
                "warnings": array(obj({"code": pat(P_CODE), "text": string()}, required=["code", "text"])),
            },
        },
    )


# --- problem -----------------------------------------------------------------

def problem():
    codes = [c["code"] for c in problems.REGISTRY]
    registry = {
        c["code"]: obj({"status": {"const": c["status"]}, "retryable": {"const": c["retryable"]}},
                       desc=c["when"])
        for c in problems.REGISTRY
    }
    return document(
        "problem.schema.json", "Problem",
        "Every failure (§4.1, §9.3.3): RFC 9457 with Atlas's members, sent as application/problem+json with "
        "Atlas-Error-Code equal to `code`. `type` is https://atlas.jhg.com/problems/<code>. A client treats a code "
        "it does not know by its status and retryable.",
        {
            "type": "object",
            "required": ["type", "status", "code", "retryable", "next", "request_id"],
            "properties": {
                "type": pat(P_PROBLEM_TYPE, "https://atlas.jhg.com/problems/<code>."),
                "title": string(),
                "status": integer("The HTTP status.", minimum=100, maximum=599),
                "detail": string(),
                "instance": string(),
                "code": pat(P_CODE, "A code of the registry ($defs/registry_code), or one the capability's profile "
                            "declares under `errors`."),
                "retryable": boolean("True only when repeating the same request is safe and may succeed."),
                "retry_after": integer("Seconds to wait, as Retry-After.", minimum=0),
                "target": string("What it is about: a JSON Pointer into the request, a header name, a field mask "
                                 "entry, a resource name."),
                "next": string("The step a person or agent can take, in words."),
                "details": array(local("detail"), "Problems behind this one: one per failure for invalid_request "
                                 "(§9.2.5), one per hop the call passed (§4.1)."),
                "request_id": string("The call's id, as X-Request-Id.", minLength=1),
                "estimate": local("estimate"),
            },
            "allOf": [
                {"if": {"required": ["code"], "properties": {"code": {"const": "confirmation_required"}}},
                 "then": {"required": ["estimate"]}},
                {"if": {"required": ["code"], "properties": {"code": {"enum": ["in_progress", "limit_exceeded",
                                                                                "unavailable"]}}},
                 "then": {"required": ["retry_after"]}},
            ],
        },
        defs={
            "registry_code": closed_enum(codes, "The problem registry of §9.3.3."),
            "registry": {
                "type": "object",
                "description": "The registry's table (§9.3.3): each code's status and whether it is retryable. "
                               "invalid_request is also sent with 406 for an Accept no binding serves (§9.6.4), and "
                               "output_limit_exceeded only inside an end line or final frame (Q-DOC-8).",
                "properties": registry,
            },
            "detail": obj(
                {
                    "code": pat(P_CODE),
                    "target": string("For a validation failure: the instance's JSON Pointer."),
                    "keyword": string("For a validation failure: the keyword that failed (§9.2.5)."),
                    "hop": string("The hop that added this problem."),
                    "status": integer(minimum=100, maximum=599),
                    "title": string(),
                    "detail": string(),
                    "challenge": string("For unauthenticated: the WWW-Authenticate challenge (CORE-SIGNIN-004)."),
                },
                desc="One problem behind this one (Q-DOC-9).",
            ),
            "estimate": obj(
                {
                    "amount": {"type": "string", "format": "decimal", "pattern": P_ESTIMATE,
                               "description": "The estimated cost: a decimal Atlas-Confirm can carry (at most 12 "
                                              "integer and 3 fractional digits, Q-DOC-10)."},
                    "unit": string("Its unit (credits, a vendor's billed calls, USD).", minLength=1),
                },
                required=["amount", "unit"],
                desc="confirmation_required: the call's estimated cost.",
            ),
        },
    )


# --- operation, page, resource ---------------------------------------------

def operation():
    return document(
        "operation.schema.json", "Operation",
        "Long-running work (§4.1, D15): every operation delivery answers one at once (202, Operation-Location); "
        "atlas.operation gets, lists, cancels and watches them. Kept at least 24 hours after it ends.",
        {
            "type": "object",
            "required": ["name", "address", "state", "created", "updated"],
            "properties": {
                "name": pat(P_OPERATION, "operations/<id>."),
                "address": fmt("address", "The `<id>@vN:<interaction>` it runs."),
                "state": open_enum(STATES, "pending, running, then succeeded, failed or cancelled."),
                "progress": {"description": "Optional, in the profile's terms."},
                "result": {"description": "When succeeded: the result, in the writer profile `profile`; for a "
                                          "transfer delivery, a transfer document."},
                "result_location": fmt("uri", "When the result is larger than 1 MiB: where to GET it, with the same "
                                       "Authorization (§9.6.5)."),
                "profile": fmt("exact-id", "The writer profile of `result` (Q-DOC-11)."),
                "problem": ref("problem.schema.json"),
                "owner": string("Who started it."),
                "created": fmt("date-time"),
                "updated": fmt("date-time"),
            },
            "allOf": [
                {"if": {"required": ["state"], "properties": {"state": {"const": "succeeded"}}},
                 "then": {"anyOf": [{"required": ["result"]}, {"required": ["result_location"]}]}},
                {"if": {"required": ["state"], "properties": {"state": {"const": "failed"}}},
                 "then": {"required": ["problem"]}},
            ],
        },
    )


def page():
    return document(
        "page.schema.json", "Page",
        "Every collection result (§9.6.5): `items` and `next_cursor`, null at the end. Cursors are opaque, bound to "
        "the request and the caller, and live an hour (§6.7).",
        {
            "type": "object",
            "required": ["items", "next_cursor"],
            "properties": {
                "items": array({"type": "object"}, "This page's records, each in the collection's item schema."),
                "next_cursor": {"type": ["string", "null"], "minLength": 1,
                                "description": "The cursor of the next page; null on the last."},
            },
        },
    )


def envelope_defs():
    """The resource envelope (§9.3.4), written in the Atlas Schema profile so a
    resource profile's record schema declares it exactly as given here."""
    return {
        "name": {
            "type": "string",
            "description": "The resource name: its domain, then collection and id pairs, stable for its life "
                           "(resource-name in grammar.abnf).",
            "pattern": P_RESOURCE,
            "x-atlas-key": True,
        },
        "metadata": {
            "type": "object",
            "description": "Written by the gateway, except labels; ignored in a create or update body.",
            "required": ["uid", "etag", "generation", "created", "updated", "owner", "labels", "deleting"],
            "properties": {
                "uid": {"type": "string", "description": "The resource's unique id, never reused.", "minLength": 1},
                "etag": {"type": "string", "description": "As the ETag header: an entity-tag with its quotes. Moves "
                                                         "with generation, never with status.", "pattern": P_ETAG},
                "generation": {"type": "integer", "description": "Moves on every change to the record's own fields "
                                                                 "or labels, never on a status change.",
                               "minimum": 1, "x-atlas-unit": "count"},
                "created": {"type": "string", "description": "When it was created.", "format": "date-time"},
                "updated": {"type": "string", "description": "When its own fields or labels last changed.",
                            "format": "date-time"},
                "owner": {"type": "string", "description": "Who owns it.", "x-atlas-logical": "Principal"},
                "labels": {"type": "object", "description": "The caller's labels: the one member of metadata a "
                                                            "caller writes.",
                           "additionalProperties": {"type": "string", "description": "A label's value."}},
                "deleting": {"type": ["string", "null"], "description": "When a delete began that is still removing "
                                                                         "what the resource made; null otherwise.",
                             "format": "date-time"},
            },
        },
        "status": {
            "type": "object",
            "description": "Only on a resource the gateway converges; written by its controller only.",
            "required": ["observed_generation", "conditions"],
            "properties": {
                "observed_generation": {"type": "integer", "description": "The generation the controller last "
                                                                          "acted on.", "minimum": 0,
                                        "x-atlas-unit": "count"},
                "conditions": {
                    "type": "array",
                    "description": "One per type; every converged resource has `ready`.",
                    "items": {
                        "type": "object",
                        "description": "A condition. Types and reasons are open sets: a client keeps one it does "
                                       "not know.",
                        "required": ["type", "status", "reason", "message", "observed_generation", "last_transition"],
                        "properties": {
                            "type": {"type": "string", "description": "The condition's type: ready, or one of the "
                                                                      "resource's own.", "minLength": 1},
                            "status": {
                                "type": "string",
                                "description": "Whether the condition holds.",
                                "enum": ["true", "false", "unknown", "UNKNOWN"],
                                "x-atlas-enum": [
                                    {"value": "true", "description": "It holds."},
                                    {"value": "false", "description": "It does not hold."},
                                    {"value": "unknown", "description": "The controller cannot tell."},
                                    {"value": "UNKNOWN", "description": "A status this reader does not know (§9.5.5)."},
                                ],
                            },
                            "reason": {"type": "string", "description": "A reason code; an open set.", "minLength": 1},
                            "message": {"type": "string", "description": "In words; may be empty."},
                            "observed_generation": {"type": "integer", "description": "The generation it was observed "
                                                                                      "for; below metadata.generation "
                                                                                      "it is out of date.",
                                                    "minimum": 0, "x-atlas-unit": "count"},
                            "last_transition": {"type": "string", "description": "When status last changed.",
                                                "format": "date-time"},
                        },
                    },
                },
            },
        },
    }


def resource():
    return document(
        "resource.schema.json", "Resource envelope",
        "Every record of a resource-family capability carries this envelope beside its own fields (§9.3.4): "
        "`name`, `metadata`, and `status` for a resource the gateway converges. A resource profile's record schema "
        "declares name, metadata and (if it converges) status exactly as $defs here; its own fields sit at the top "
        "of the record.",
        {
            "type": "object",
            "required": ["name", "metadata"],
            "properties": {"name": local("name"), "metadata": local("metadata"), "status": local("status")},
        },
        defs=envelope_defs(),
    )


# --- handoff, connect, ticket, transfer, event, end line -------------------

def handoff():
    return document(
        "handoff.schema.json", "Handoff",
        "`fetch` answered by a handoff (§2.2.1.3, §9.6.5): one result's chunks, to download directly from the "
        "source's result storage. application/vnd.atlas.handoff+json, Cache-Control: no-store. The client GETs each "
        "chunk `url` with its `headers`, in parallel, decodes it by `format` into the writer profile's Arrow schema "
        "(§9.2.7), checks each batch against it (invalid_document on a mismatch), and when links expire asks "
        "`operation` again for fresh ones.",
        {
            "type": "object",
            "required": ["profile", "format", "source_schema", "first_chunk", "chunks", "expires", "operation"],
            "properties": {
                "profile": fmt("exact-id", "The writer profile."),
                "format": open_enum(["snowflake-arrow/1"], "The chunks' format and its version; a client that does "
                                    "not declare it in Atlas-Client-Features is streamed instead."),
                "source_schema": array(local("source_column"), "The source's result schema, in result column order."),
                "first_chunk": {
                    "description": "The first chunk, inline, or null when it travels as the first of `chunks`.",
                    "anyOf": [{"type": "null"}, local("inline_chunk")],
                },
                "chunks": array(local("chunk"), "The chunks to download, in result order."),
                "rows": integer("Rows in the whole result.", minimum=0, unit="count"),
                "expires": fmt("date-time", "When the links stop working."),
                "operation": pat(P_OPERATION, "The operation to ask again (atlas.operation:get) for fresh links; its "
                                 "result is a handoff document (Q-DOC-12)."),
            },
        },
        defs={
            "source_column": obj(
                {
                    "name": string("The column's name as the result carries it (the contract name)."),
                    "type": open_enum(SNOWFLAKE_TYPES, "Snowflake's logical type (the chunk field's logicalType)."),
                    "precision": integer(minimum=0),
                    "scale": integer(minimum=0),
                    "nullable": boolean(),
                },
                required=["name", "type", "nullable"],
            ),
            "inline_chunk": obj(
                {"rows": integer(minimum=0, unit="count"),
                 "data": pat(P_BASE64, "The chunk's bytes (an Arrow IPC stream), base64 (RFC 4648 section 4).")},
                required=["rows", "data"],
            ),
            "chunk": obj(
                {
                    "url": fmt("uri", "A pre-signed link to the chunk; the link is the credential."),
                    "rows": integer(minimum=0, unit="count"),
                    "bytes": integer("The chunk's size as downloaded.", minimum=0, unit="count"),
                    "uncompressed_bytes": integer(minimum=0, unit="count"),
                    "headers": {"type": "object", "description": "Headers the download must send (on some clouds, "
                                                                 "the result's encryption key).",
                                "additionalProperties": {"type": "string"}},
                },
                required=["url", "rows", "bytes", "headers"],
            ),
        },
    )


def connect():
    return document(
        "connect.schema.json", "Connect handover",
        "`subscribe` and `publish` of an event capability (§2.5, §9.6.5, §9.7.8): what the client needs to listen "
        "on, or send to, one Service Bus entity itself, as the person. No secret, except an emulator's connection "
        "string, which a client accepts only from a gateway whose environment is conformance or local. "
        "application/vnd.atlas.connect+json, Cache-Control: no-store.",
        {
            "type": "object",
            "required": ["kind", "protocol", "namespace", "entity", "auth", "rights", "profile", "format", "expires"],
            "properties": {
                "kind": {"const": "connect"},
                "protocol": open_enum(["amqp"]),
                "namespace": string("The Service Bus namespace's host name.", minLength=1),
                "entity": string("The topic (send) or subscription (listen): `topic/subscriptions/<name>`.",
                                 minLength=1),
                "auth": {
                    "type": "object",
                    "required": ["kind"],
                    "properties": {
                        "kind": open_enum(["entra-scope", "emulator"], "entra-scope: get a token for `scope` from the "
                                          "shared sign-in, silently, never sent to the gateway. emulator: use "
                                          "`connection_string` (conformance and local only)."),
                        "scope": fmt("uri", "The scope to get a token for."),
                        "connection_string": string("The emulator's connection string (Q-DOC-13).", minLength=1),
                    },
                    "allOf": [
                        {"if": {"required": ["kind"], "properties": {"kind": {"const": "entra-scope"}}},
                         "then": {"required": ["scope"]}},
                        {"if": {"required": ["kind"], "properties": {"kind": {"const": "emulator"}}},
                         "then": {"required": ["connection_string"]}},
                    ],
                },
                "rights": array(open_enum(["listen", "send"]), min_items=1, unique=True),
                "profile": fmt("exact-id", "The event's profile."),
                "format": open_enum(["cloudevents+json"]),
                "settle": open_enum(["peek-lock", "receive-and-delete"], "How received messages are settled."),
                "durable": boolean("Whether the subscription outlives the connection."),
                "expires": fmt("date-time", "When to ask the gateway again."),
            },
        },
    )


def ticket():
    return document(
        "ticket.schema.json", "Ticket",
        "`:ticket` (§2.2, §6.3.4): a credential issued once and its planned work, for `client` execution. Deferred "
        "until its credential meets the credential rule (O54): no atlas/1.0 client declares Broker. Cache-Control: "
        "no-store.",
        {
            "type": "object",
            "required": ["kind", "protocol", "target", "auth", "rights", "expires", "profile", "planned", "operation"],
            "properties": {
                "kind": {"const": "ticket"},
                "protocol": open_enum(["snowflake"]),
                "target": obj({"account": string(minLength=1), "warehouse": string(), "role": string()},
                              required=["account"], desc="The one account the client connects to."),
                "auth": obj({"kind": open_enum(["token"]), "token": string(minLength=1), "expires": fmt("date-time")},
                            required=["kind", "token"], desc="The ticket's credential, carried."),
                "rights": array(open_enum(["read"]), min_items=1, unique=True),
                "expires": fmt("date-time"),
                "profile": fmt("exact-id", "The writer profile the statement produces."),
                "planned": obj(
                    {
                        "statement": string("The planned statement, run unchanged.", minLength=1),
                        "dialect": string(),
                        "query_tag": string("The tag settlement matches.", minLength=1),
                        "result_schema": {"type": "object", "description": "Its writer schema."},
                    },
                    required=["statement", "query_tag", "result_schema"],
                ),
                "operation": pat(P_OPERATION, "The operation the run is settled against."),
            },
        },
    )


def transfer():
    part = obj(
        {
            "url": fmt("uri", "A short-lived link; the link is the credential."),
            "offset": integer("The part's first byte in its file.", minimum=0, unit="count"),
            "bytes": integer(minimum=0, unit="count"),
            "sha256": pat(P_SHA256, "Lowercase hexadecimal SHA-256 of the part's bytes."),
            "headers": {"type": "object", "additionalProperties": {"type": "string"}},
        },
        required=["url", "offset", "bytes"],
    )
    return document(
        "transfer.schema.json", "Transfer",
        "`transfer` delivery and `:export` (§2.1.D, §9.6.5): the result, when succeeded, of the operation a "
        "transfer answers with. A download lists its files: each fetched by its parts' links with GET and checked "
        "by `sha256`, or, under 1 MiB, inline (the fallback every client can read, Q-DOC-14). An upload lists the "
        "parts to PUT (at most 8 MiB each, in any order), then the client calls `commit` with $defs/commit_request "
        "and the same Idempotency-Key. Links are used only until `expires`.",
        {
            "type": "object",
            "required": ["kind", "operation", "expires"],
            "properties": {
                "kind": open_enum(["download", "upload"]),
                "protocol": open_enum(["azure-blob", "http-range", "graph-upload-session"]),
                "operation": pat(P_OPERATION, "The operation the transfer belongs to; asked again for fresh links."),
                "expires": fmt("date-time"),
                "files": array(local("file"), "A download's files: one for content, several for an export."),
                "parts": array(local("part"), "An upload's parts."),
                "commit": ref("binding.schema.json"),
                "chunking": obj({"part_bytes": integer(minimum=1, maximum=8388608, unit="count"),
                                 "parallelism": integer(minimum=1, unit="count"),
                                 "checksum": open_enum(["sha256"])}),
            },
            "allOf": [
                {"if": {"required": ["kind"], "properties": {"kind": {"const": "download"}}},
                 "then": {"required": ["files"]}},
                {"if": {"required": ["kind"], "properties": {"kind": {"const": "upload"}}},
                 "then": {"required": ["parts", "commit"]}},
            ],
        },
        defs={
            "part": part,
            "file": obj(
                {
                    "name": string("The file's name: the content's `name` from its metadata, else `content`.",
                                   minLength=1),
                    "media_type": string(minLength=1),
                    "bytes": integer(minimum=0, unit="count"),
                    "sha256": pat(P_SHA256, "Of the whole file."),
                    "metadata": {"type": "object", "description": "The content's metadata record, unchanged."},
                    "parts": array(part, min_items=1),
                    "inline": pat(P_BASE64, "The file's bytes, base64, when under 1 MiB."),
                },
                required=["name", "media_type", "bytes", "sha256"],
            ),
            "commit_request": obj(
                {
                    "operation": pat(P_OPERATION),
                    "parts": array(obj({"url": fmt("uri"), "sha256": pat(P_SHA256)}, required=["url", "sha256"],
                                       closed=True), min_items=1),
                },
                required=["operation", "parts"],
                closed=True,
                desc="The body of the transfer's `commit` call (§9.6.5): a request, so closed.",
            ),
        },
    )


def event():
    return document(
        "event.schema.json", "Event",
        "A CloudEvent 1.0.2 in its JSON format (§2.5), received over AMQP after a connect handover. `dataschema` "
        "names the writer profile; a subscriber resolves `data` from it (§9.5) and drops one whose `id` it has seen "
        "in its last 10,000.",
        {
            "type": "object",
            "required": ["specversion", "id", "source", "type", "time", "dataschema", "data"],
            "properties": {
                "specversion": {"const": "1.0"},
                "id": string("Unique per event: the duplicate key.", minLength=1),
                "source": string("A URI-reference: the publisher (a client's name when it publishes, §9.7.8).",
                                 minLength=1),
                "type": pat(P_EVENT_TYPE, "com.jhg.fiquant.<id>.v<N>."),
                "subject": string(),
                "time": fmt("date-time"),
                "datacontenttype": {"const": "application/json"},
                "dataschema": pat(P_DATASCHEMA, "urn:atlas:profile:<id>@vN:<fingerprint> (§9.4.3)."),
                "data": {"type": "object", "description": "The payload, a record in the profile's payload schema."},
            },
        },
    )


def chunk_end():
    return document(
        "chunk-end.schema.json", "NDJSON end line",
        "The last line of every NDJSON answer, and the JSON of the `atlas:end` metadata of an Arrow stream's final "
        "zero-row batch (§9.6.5). A body that ends without it is incomplete: upstream_failed. A problem after the "
        "first row is reported here; output_limit_exceeded with `truncated` is a warning, any other problem is "
        "raised after the rows (Q-DOC-15).",
        {
            "type": "object",
            "required": ["$atlas", "rows", "truncated", "usage", "problem"],
            "properties": {
                "$atlas": {"const": "end"},
                "rows": integer("Rows sent.", minimum=0),
                "truncated": boolean("Whether the answer was cut at the caller's output cap."),
                "usage": {"type": "object", "description": "What the call used."},
                "problem": {"anyOf": [{"type": "null"}, ref("problem.schema.json")]},
            },
        },
    )


# --- lock, token store -----------------------------------------------------

def lock():
    return document(
        "lock.schema.json", "Lock",
        "atlas.lock, TOML parsed to JSON (§3.3.10, §9.7.9): an application's reader profiles. A DynamicSchema "
        "capability is held only as dynamic = true.",
        {
            "type": "object",
            "required": ["capabilities"],
            "properties": {
                "capabilities": {
                    "type": "object",
                    "propertyNames": {"format": "capability-id"},
                    "additionalProperties": obj(
                        {
                            "version": integer(minimum=1),
                            "fingerprint": pat(P_FINGERPRINT),
                            "dynamic": boolean("Its types are not promised (§9.3.2)."),
                        },
                        required=["version", "fingerprint"],
                    ),
                },
                "extras": array(string(minLength=1), "The client extras the application uses (§9.7.12)."),
            },
        },
    )


def token_store():
    return document(
        "token-store.schema.json", "Token store entry",
        "<ATLAS_HOME>/state/token-<env>.json (§9.7.3): one environment's sign-in, shared by every client. Mode 0600 "
        "in a 0700 directory; written atomically; refreshed within 60 seconds of expires_at.",
        {
            "type": "object",
            "required": ["access_token", "expires_at"],
            "properties": {
                "access_token": string(minLength=1),
                "expires_at": integer("Seconds since the epoch."),
                "refresh_token": string(minLength=1),
            },
        },
    )


# --- built-ins ---------------------------------------------------------------

def _by(extra=None):
    """atlas.describe's two forms (atlas.guide adds `client` and `max_tokens`)."""
    extra = extra or {}
    branches = []
    for by, member, desc, f in (
        ("id", "id", "A capability-id or pinned-id.", None),
        ("exact", "exact", "An exact-id.", "exact-id"),
    ):
        props = {"by": {"type": "string", "const": by, "description": "How the capability is named."}}
        if f:
            props[member] = fmt(f, desc)
        else:
            props[member] = {"type": "string", "description": desc,
                             "anyOf": [{"type": "string", "format": "capability-id"},
                                       {"type": "string", "format": "pinned-id"}]}
        props.update(copy.deepcopy(extra))
        branches.append(obj(props, required=["by", member], closed=True))
    return {"type": "object", "oneOf": branches}


def builtin_defs():
    page_size = lambda lo, hi, d: integer(f"Items per page, {lo} to {hi}.", minimum=lo, maximum=hi, default=d,
                                          unit="count")
    req = lambda props, required=(), desc=None: obj(props, required=required, closed=True, desc=desc)
    defs = {
        "whoami_request": req({}, desc="atlas.whoami:fetch."),
        "whoami_result": obj(
            {
                "principal": {"type": "string", "description": "The caller's Entra object id.",
                              "x-atlas-logical": "Principal"},
                "name": string("The caller's name."),
                "kind": open_enum(["person", "workload"], "Who the caller is."),
                "roles": array(string(), "The roles the caller holds."),
                "environment": pat(P_SEGMENT, "The environment."),
                "available_domains": array(pat(P_SEGMENT), "The domains with something available to the caller."),
                "house_rules": ref("catalog-page.schema.json", "house_rules"),
            },
            required=["principal", "name", "kind", "roles", "environment", "available_domains", "house_rules"],
        ),
        "catalog_request": req(
            {
                "domain": pat(P_SEGMENT, "Only this domain."),
                "family": closed_enum(FAMILIES, "Only this family."),
                "effect": closed_enum(EFFECTS, "Only capabilities with an interaction of this effect."),
                "previews": boolean("Include preview capabilities.", default=False),
                "cursor": cursor(),
                "page_size": page_size(1, 500, 100),
            },
            desc="atlas.catalog:fetch; answered by catalog-page.schema.json.",
        ),
        "search_request": req(
            {
                "words": string("What to look for.", minLength=1, maxLength=200),
                "cursor": cursor(),
                "page_size": page_size(1, 500, 100),
            },
            required=["words"],
            desc="atlas.search:fetch; answered by catalog-page.schema.json, ranked. page_size as atlas.catalog's "
                 "(Q-BI-1).",
        ),
        "describe_request": dict(_by(), description="atlas.describe:fetch; answered by describe.schema.json."),
        "guide_request": dict(
            _by({
                "client": closed_enum(CLIENT_SYNTAXES, "Whose syntax the examples are written in."),
                "max_tokens": integer("The guide's budget.", minimum=200, maximum=20000, default=4000, unit="count"),
            }),
            description="atlas.guide:fetch.",
        ),
        "guide_result": obj(
            {
                "exact": fmt("exact-id"),
                "client": open_enum(CLIENT_SYNTAXES),
                "markdown": string("The guide (§2.6), returned unchanged."),
                "tokens": integer(minimum=0, unit="count"),
                "truncated": boolean("Whether sections were shortened to fit max_tokens."),
                "revisions": {"type": "object", "description": "The revisions it was written from: the profile's "
                                                                "prose, governance and context (Q-BI-2)."},
            },
            required=["exact", "client", "markdown", "tokens", "truncated", "revisions"],
        ),
        "access_request": req(
            {"id": id_or_pinned("Only this capability."), "domain": pat(P_SEGMENT, "Only this domain."),
             "cursor": cursor()},
            desc="atlas.access:fetch; answered by a page of access_entry.",
        ),
        "access_entry": obj(
            {
                "id": id_or_pinned("The capability."),
                "status": open_enum(ACCESS_STATUSES, "The caller's status (§2.6)."),
                "reason": string("Why."),
                "next": string("How to get it."),
                "held_by": string("How the caller holds it."),
                "limits": array(ref("governance.schema.json", "limit")),
                "interactions": {"type": "object", "description": "Per interaction: how it is held, its limits."},
            },
            required=["id", "status", "reason", "next"],
        ),
        "usage_request": req(
            {"id": id_or_pinned("Only this capability."), "period": string("Only this period.", minLength=1),
             "cursor": cursor()},
            desc="atlas.usage:fetch; answered by a page of usage_entry.",
        ),
        "usage_entry": obj(
            {
                "id": id_or_pinned("The capability."),
                "interaction": pat(P_SEGMENT),
                "kind": open_enum(LIMIT_KINDS),
                "used": {"type": "number", "description": "What was used.", "x-atlas-unit": "count"},
                "limit": {"type": "number", "description": "The limit.", "x-atlas-unit": "count"},
                "remaining": {"type": "number", "description": "What is left.", "x-atlas-unit": "count"},
                "period": string(),
                "pool": string("The pool it draws on."),
            },
            required=["id", "interaction", "kind", "used", "limit", "remaining", "period"],
        ),
        "setup_request": req({"harness": string("The harness to set up.", minLength=1)}, desc="atlas.setup:fetch."),
        "setup_result": obj(
            {"steps": array(obj({"name": string(), "done": boolean(), "command": string(), "detail": string()},
                                required=["name", "done", "detail"]))},
            required=["steps"],
        ),
        "clients_request": req({"cursor": cursor()}, desc="atlas.clients:fetch; answered by a page of client_entry."),
        "client_entry": obj(
            {
                "name": string(), "language": string(), "built_for": string(),
                "roles": array(string()), "levels": array(open_enum(LEVELS)), "extras": array(string()),
                "version": string(), "install": string(),
            },
            required=["name", "language", "built_for", "roles", "levels", "extras", "version", "install"],
        ),
        "compare_request": req(
            {
                "id": fmt("capability-id", "The data capability."),
                "from": pat(P_VERSION, "One version (`v1`)."),
                "to": pat(P_VERSION, "The other (`v2`)."),
                "request": {"type": "object", "description": "A request both versions take; free-form here "
                                                             "(Q-PROF-9)."},
            },
            required=["id", "from", "to", "request"],
            desc="atlas.compare:fetch; answered by a table of the two results joined on their keys.",
        ),
        "operation_get_request": req({"name": pat(P_OPERATION)}, required=["name"],
                                     desc="atlas.operation:get, :cancel and :watch (Q-BI-3)."),
        "operation_list_request": req(
            {"cursor": cursor(), "state": closed_enum(STATES, "Only operations in this state.")},
            desc="atlas.operation:list; answered by a page of operations.",
        ),
        "catalog_changed_payload": obj({"catalog_etag": pat(P_ETAG, "The catalogue's ETag after the change.")},
                                       required=["catalog_etag"], desc="atlas.catalog.changed: each event's data."),
        "settings_get_request": req({}, desc="atlas.settings:get."),
        "settings_update_request": req(
            {
                "fields": array(string(minLength=1), "The field mask: the settings members to change.", min_items=1,
                                unique=True),
                "settings": {"type": "object", "description": "The new values; free-form here (Q-PROF-9)."},
            },
            required=["fields", "settings"],
            desc="atlas.settings:update, sent with If-Match.",
        ),
        "directory_request": req(
            {"words": string("Whom to look for.", minLength=2, maxLength=100),
             "kind": closed_enum(["person", "group"], "Only people or only groups."), "cursor": cursor()},
            required=["words"],
            desc="atlas.directory:fetch; answered by a page of directory_entry (Q-BI-4).",
        ),
        "directory_entry": obj(
            {"principal": {"type": "string", "description": "The Entra object id.", "x-atlas-logical": "Principal"},
             "name": string(), "kind": open_enum(["person", "group"])},
            required=["principal", "name", "kind"],
        ),
        "audit_request": req(
            {"day": fmt("date", "The day to read."), "before": string("Read records before this one.", minLength=1),
             "page_size": page_size(1, 1000, 200)},
            required=["day"],
            desc="atlas.audit:fetch (Gateway.Admin).",
        ),
        "audit_record": obj(
            {
                "request_id": string(minLength=1),
                "time": fmt("date-time"),
                "address": fmt("address"),
                "caller": obj({"principal": string(), "kind": open_enum(["person", "workload"]), "name": string()}),
                "client": obj({"name": string(), "version": string(), "protocol": pat(P_PROTOCOL)}),
                "edge": string(),
                "hops": array({"type": "object"}),
                "decision": obj({"result": open_enum(["allowed", "refused"]), "reason": string(), "code": pat(P_CODE)}),
                "profiles": obj({"writer": fmt("exact-id", nullable=True), "expected": fmt("exact-id", nullable=True)}),
                "governance_revision": string(),
                "limits": {"type": "object", "description": "What it reserved and settled."},
                "execution": open_enum(EXECUTIONS),
                "rows": integer(minimum=0),
                "bytes": integer(minimum=0),
                "duration_ms": integer(minimum=0),
                "source": {"type": "object", "description": "The source's own record, copied at settlement."},
                "data_version": string(),
                "outcome": obj({"status": integer(minimum=100, maximum=599), "code": pat(P_CODE)}),
                "receipt": {"type": "object"},
                "request_hash": pat(P_SHA256, "SHA-256 of the request's canonical form (RFC 8785)."),
            },
            required=["request_id", "time", "address", "caller", "decision", "outcome"],
            desc="One call's record (§6.2.8).",
        ),
    }
    return defs


def builtins():
    return document(
        "builtins.schema.json", "Built-ins",
        "One $defs entry per built-in request and result of §9.3.2. Requests are closed; results are open. The "
        "atlas.* profiles atlas.describe serves use exactly these schemas. Every member listed without `?` in "
        "§9.3.2 is required.",
        {},
        defs=builtin_defs(),
    )


# --- the meta-schema ---------------------------------------------------------

def atlas_schema():
    """§9.2.1–§9.2.5 as a meta-schema. What a meta-schema cannot say (I-Regexp
    validity, depth and size, the request/other rule, UNKNOWN, $ref targets,
    discriminated oneOf) is checked by profilecheck.check_schema and listed in
    the description; the schema vectors test both."""
    from .profilecheck import UNITS
    from .validator import LOGICAL_BASE

    schema_or_ref = local("schema")
    keyword = {
        "$ref": {"type": "string", "pattern": "^#/\\$defs/[^/]+$"},
        "$comment": {"type": "string"},
        "properties": {"type": "object", "propertyNames": {"pattern": P_PROPERTY},
                       "additionalProperties": schema_or_ref},
        "additionalProperties": {"anyOf": [{"type": "boolean"}, schema_or_ref]},
        "items": {"anyOf": [{"type": "boolean"}, schema_or_ref]},
        "prefixItems": {"type": "array", "minItems": 1, "items": schema_or_ref},
        "anyOf": {"type": "array", "minItems": 1, "items": schema_or_ref},
        "oneOf": {"type": "array", "minItems": 1,
                  "items": {"allOf": [schema_or_ref, {"type": "object", "required": ["properties", "required"]}]}},
        "type": {"anyOf": [
            {"enum": ["null", "boolean", "object", "array", "number", "integer", "string"]},
            {"type": "array", "prefixItems": [{"enum": ["boolean", "object", "array", "number", "integer", "string"]},
                                              {"const": "null"}], "items": False, "minItems": 2},
        ]},
        "enum": {"type": "array", "minItems": 1, "uniqueItems": True},
        "const": {},
        "required": {"type": "array", "uniqueItems": True, "items": {"type": "string", "pattern": P_PROPERTY}},
        "minimum": {"type": "number"},
        "maximum": {"type": "number"},
        "exclusiveMinimum": {"type": "number"},
        "exclusiveMaximum": {"type": "number"},
        "multipleOf": {"type": "integer", "exclusiveMinimum": 0},
        "minLength": {"type": "integer", "minimum": 0},
        "maxLength": {"type": "integer", "minimum": 0},
        "pattern": {"type": "string", "pattern": "^\\^.*\\$$"},
        "minItems": {"type": "integer", "minimum": 0},
        "maxItems": {"type": "integer", "minimum": 0},
        "uniqueItems": {"type": "boolean"},
        "minProperties": {"type": "integer", "minimum": 0},
        "maxProperties": {"type": "integer", "minimum": 0},
        "title": {"type": "string"},
        "description": {"type": "string"},
        "default": {},
        "deprecated": {"type": "boolean"},
        "examples": {"type": "array"},
        "readOnly": {"type": "boolean"},
        "writeOnly": {"type": "boolean"},
        "format": {"enum": ["date", "date-time", "duration", "decimal", "int64", "uri", "uuid", "capability-id",
                            "pinned-id", "exact-id", "address"]},
        "x-atlas-logical": {"anyOf": [
            {"enum": sorted(LOGICAL_BASE)},
            {"type": "string", "pattern": "^Decimal\\(([1-9]|[12][0-9]|3[0-8]),([0-9]|[12][0-9]|3[0-8])\\)$"},
        ]},
        "x-atlas-key": {"const": True},
        "x-atlas-aliases": {"type": "array", "minItems": 1, "uniqueItems": True,
                            "items": {"type": "string", "pattern": P_PROPERTY}},
        "x-atlas-reference": {"type": "string", "format": "pinned-id"},
        "x-atlas-deprecated": local("deprecation"),
        "x-atlas-unit": {"enum": list(UNITS)},
        "x-atlas-enum": {"type": "array", "minItems": 1, "items": local("enum_entry")},
    }
    sub = {"type": "object", "additionalProperties": False, "properties": keyword}
    root_props = dict(keyword)
    root_props.update({
        "$schema": {"const": DRAFT},
        "$vocabulary": {"const": VOCABULARY},
        "$id": {"type": "string", "format": "uri"},
        "$defs": {"type": "object", "additionalProperties": schema_or_ref},
    })
    return document(
        "atlas-schema.schema.json", "The Atlas Schema profile",
        "The meta-schema for every schema inside a profile (§9.2): JSON Schema 2020-12 restricted to the keywords "
        "and formats of §9.2.1–§9.2.2, with the Atlas vocabulary of §9.2.3. Beyond what a meta-schema can express, "
        "a schema in the profile also: has `^ R $` patterns whose R is an I-Regexp (§9.2.4); nests no deeper than "
        "32 levels and is at most 1 MiB as UTF-8 (canonical form); closes every object of a request schema and "
        "leaves every other open; has UNKNOWN in every string enum outside requests and never in one; uses $ref "
        "only to an existing #/$defs/<name>, with no recursion; uses oneOf only with a const discriminator required "
        "in every branch; puts x-atlas-key only on required, non-null result properties (Isin, Cusip, Sedol, Figi "
        "and Lei may be missing). references/vectors/schemas.json tests every rule; QUESTIONS.md records the "
        "readings (Q-PROF-n).",
        {
            "type": "object",
            "required": ["$schema", "$vocabulary"],
            "additionalProperties": False,
            "properties": root_props,
        },
        defs={
            "schema": sub,
            "deprecation": {
                "anyOf": [
                    {"type": "null"},
                    {"type": "object", "additionalProperties": False,
                     "required": ["reason", "since", "sunset", "replaced_by"],
                     "properties": {"reason": {"type": "string"}, "since": {"type": "string", "format": "date"},
                                    "sunset": {"type": "string", "format": "date"},
                                    "replaced_by": {"type": ["string", "null"]}}},
                ]
            },
            "enum_entry": {
                "type": "object",
                "additionalProperties": False,
                "required": ["value"],
                "properties": {
                    "value": {"type": "string"},
                    "description": {"type": "string"},
                    "aliases": {"type": "array", "items": {"type": "string"}},
                    "deprecated": local("deprecation"),
                },
            },
        },
    )


DOCUMENTS = {
    "bootstrap.schema.json": bootstrap,
    "binding.schema.json": binding,
    "profile.schema.json": profile,
    "catalog-page.schema.json": catalog_page,
    "governance.schema.json": governance,
    "describe.schema.json": describe,
    "explain.schema.json": explain,
    "problem.schema.json": problem,
    "operation.schema.json": operation,
    "page.schema.json": page,
    "resource.schema.json": resource,
    "handoff.schema.json": handoff,
    "connect.schema.json": connect,
    "ticket.schema.json": ticket,
    "transfer.schema.json": transfer,
    "event.schema.json": event,
    "chunk-end.schema.json": chunk_end,
    "lock.schema.json": lock,
    "token-store.schema.json": token_store,
    "builtins.schema.json": builtins,
    "atlas-schema.schema.json": atlas_schema,
}


def all_documents():
    return {name: build() for name, build in DOCUMENTS.items()}
