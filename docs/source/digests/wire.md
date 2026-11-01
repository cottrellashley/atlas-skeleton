# Digest: the wire protocol (gateway HTTP edge and every client)

> Historical engineering study (2026-10-31). Retained for its requirements, counterexamples and design reasoning. Implementation observations and line references describe that review snapshot; use [current progress](../../HANDOFF-PROGRESS.md) and the named source symbols for current status. Paths are relative to the repository root unless a citation key says otherwise.

Reader: wire. Written 2026-10-31. Governing order: `docs/pass-2/REVIEW.md` (REVIEW) > the review (`atlas-spec/review/`) > §9 > the references. §9 is normative for the wire (09-atlas-protocol.md:81, AGENTS.md:7).

Citation keys:
- `§9` = `atlas-spec/09-atlas-protocol.md`; `§9.1` = `09-1-conventions-and-grammar.md`; `§9.3` = `09-3-documents.md`; `§9.4` = `09-4-fingerprints.md`; `§9.6` = `09-6-http-binding.md`; `§9.7` = `09-7-clients.md` (read in full for the walk); `§9.2` = `09-2-schemas.md` (only lines 93-152 read).
- `S:<name>` = `spec/protocol/references/schemas/<name>.schema.json`, with line numbers.
- `V:headers`, `V:problems` = `references/vectors/headers.json`, `problems.json`.
- `Q-…` = `references/QUESTIONS.md` (the references' recorded readings; not spec text).
- `R01`…`R26` = REVIEW.md:57-84 table; `DR` = `review/DESIGN-REVIEW.md`; `AC` = `review/ATLAS-CORE.md`.
- `fx:gateway` = `references/scenarios/fixtures/gateway.json`; `SCN:<id>` = `references/scenarios/<id>.json`.

Markers: **IN SCOPE** (first build must implement), **DEFERRED**, **AMBIGUOUS:**, **OVERRIDDEN BY REVIEW:**.

Scope basis: AC §12 item 3 (AC:238): "Implement bootstrap, identity, authorized catalogue/search, describe/guide, the call path, problems, data streaming and durable operations where that journey needs them." Deferred by REVIEW:15 and REVIEW:33: handoff, events, Service Bus, direct downloads, delegated identity, the Development source.

---

## 0. Protocol version, levels, and what applies to the first build

| Fact | Value | Cite |
|---|---|---|
| Protocol id grammar | `protocol-ver = %s"atlas/" 1*DIGIT "." 1*DIGIT` (e.g. `atlas/1.0`) | §9.1:45 |
| This text defines | `atlas/1.0` | §9:35 |
| Minor version | only adds: a header, an optional document member, a delivery, a trait, a lens operation, a problem code | §9:37 |
| Major version | may change anything; previous major served beside it for at least two years | §9:38 |
| Who declares | gateway in bootstrap (`protocols`, `minimum_protocol`); client in every request (`Atlas-Client` `protocol`) | §9:39; S:bootstrap:17-31; §9.6:31 |
| Leading zeros | `atlas/01.00` accepted (as the ABNF) | Q-GRAM-2 |

Levels (§9:45-53). The gateway serves a client lacking a level "the next way down, never refusing it: a handoff becomes a stream, Arrow becomes NDJSON, a subscription becomes polling" (§9:43).

| Level | Token in `Atlas-Client-Features` `levels` | First build |
|---|---|---|
| Core: bootstrap, token store sign-in, discover, describe, validate, call with `single` and `operation` delivery in JSON, resolve JSON results, problems, deprecations, deadlines, idempotency, retries | `core` | **IN SCOPE** (every client; AC:238) |
| Streaming: `chunked` as NDJSON | `streaming` | **IN SCOPE** ("data streaming", AC:238; "incomplete streams", REVIEW:50) |
| Columnar: Arrow IPC, column-wise resolution | `columnar` | **AMBIGUOUS:** R11 (REVIEW:69) makes "Core JSON, and Arrow an optimisation of the same meaning". Readings: (a) build Arrow now because pass-1's Go engine hands Python Arrow IPC (§9.7:171; REVIEW:19); (b) defer Arrow and ship JSON + NDJSON only. Either way Arrow must give the same logical values (AC:136) |
| Bulk: `transfer`, `export` | `bulk` | **DEFERRED** ("direct downloads", REVIEW:33) |
| Events: `subscribe`, `publish`, connect handover, AMQP | `events` | **DEFERRED** (REVIEW:15, R10/R16) |
| Handoff | `handoff` | **DEFERRED** (REVIEW:15; R10) |
| Broker (`client` execution, tickets) | `broker` (reserved) | **DEFERRED**: no client declares it in `atlas/1.0` (§9:53; §9.6:52) |

`atlas/1.0` has no subscription over HTTP; `sse`/`wss` are reserved for a later minor (§9:55; §9.6:83).

---

## 1. Endpoints and bindings

### 1.1 Fixed endpoint: the bootstrap
- `GET https://<gateway>/.well-known/atlas` (RFC 8615). No sign-in, `application/json`, a strong `ETag`, revalidated with `If-None-Match` (§9.6:14). **IN SCOPE**.
- The conformance gateway answers the second, conditional bootstrap `304 Not Modified` (SCN:CORE-BOOT-001 note). The first bootstrap request carries no `Authorization` (same scenario's assertion).
- It is the only URL a client knows in advance. Every other URL comes from a binding (§9.6:15).

### 1.2 How every other URL is formed
- URL = the binding's `server`'s `url` (from the bootstrap's `servers`; no trailing `/`) followed by the binding's `address` (which begins with `/`). "A client never constructs an address itself." (§9.6:15; S:binding:47-56)
- An operation's URL is its `Operation-Location`; cancel is `POST <Operation-Location>:cancel` (§9.6:16).
- Method: `POST` for every interaction; `atlas/1.0` bindings declare `"method": "POST"`. `GET` only for the bootstrap and for polling an operation (§9.6:20; S:binding:76).
- Body: the request record, `Content-Type: application/json`, valid against the interaction's request schema. No parameters means `{}`. A resource is named by its `name` member in the body; "addresses have no templates" (§9.6:21).
- Protocol options travel in headers, never in the body (§9.6:22; §9.7:38).
- The client picks the first binding it supports (protocol, encodings and delivery), in the order given (§9.7:92 step 6; S:binding:5).
- Proof scenarios: SCN:CORE-BOOT-001 moves `atlas.whoami` to `/bootstrapped/who-am-i` and expects 0 requests to `/v1/atlas/atlas.whoami:fetch`; SCN:CORE-CALL-003 serves a profile binding at an unusual address.

### 1.3 The gateway's address layout (informative; the gateway may choose it, clients never build it)
From `04-plugging-in.md:283` (informative; not one of my assigned files, quoted because the task asks for addresses):

| What | Path template | Example |
|---|---|---|
| Capability interaction | `/v1/<family>/<id>@v<N>:<interaction>` on server `gateway` | `/v1/data/portfolio.holdings@v1:fetch` |
| Built-in | `/v1/atlas/<id>:<interaction>` "as the bootstrap lists them" | `/v1/atlas/atlas.whoami:fetch` |
| Operation | `/v1/operations/<id>` | `https://atlas.example/v1/operations/op-91` (V:headers, Operation-Location case) |

- The references use exactly these: fx:gateway bootstrap `"address": "/v1/atlas/atlas.whoami:fetch"` (fx:gateway:42), and the profile vectors `/v1/data/portfolio.holdings@v1:fetch|explain|export`.
- **AMBIGUOUS:** `07-the-catalogue.md:171-190` lists `/v1/me`, `/v1/catalog`, `/v1/me/usage`, `/v1/setup`, `/v1/clients`, `/v1/me/config`, `/v1/directory`, `/v1/admin` under a column headed "Today". Reading: these are the existing routes, not `atlas/1.0` bindings. The references follow `/v1/atlas/<id>:<interaction>`. Note that built-in addresses carry no `@v1` (they are `capability-id ":" interaction`, allowed by the `address` rule, §9.1:44).

### 1.4 Interactions by family (what a binding serves)

| Family | Interactions | Delivery and result (example profile) | First build |
|---|---|---|---|
| data | `fetch` | `effect: read`, `result: table`, `delivery: chunked`, `execution: [gateway, handoff]`, deadline `PT2M`/`PT30M`, binding encodings `arrow, json, ndjson, csv` (vectors/profiles/data.json:47-77) | **IN SCOPE** |
| data | `explain` | `read`, `record`, `single`, `[gateway]`, `PT30S`/`PT2M`, encodings `json`; answer = S:explain (data.json:78-100) | **IN SCOPE** (Core client op, §9.7:25; SCN:CORE-EXPLAIN-001) |
| data | `export` | `read`, `content`, `transfer`, `[gateway]`, `PT10M`/`PT2H`, interaction `encodings: [parquet, arrow]`, binding encodings `json` (data.json:101-130) | **DEFERRED** (Bulk; direct downloads, REVIEW:33) |
| resource | `get`, `list`, `create`, `update`, `delete`, custom verbs | envelope S:resource | **DEFERRED**: "Resources (after the journey)" (R14, REVIEW:72) |
| action | `plan`, optional `approve`, the verb | operation + receipt | **DEFERRED** (not in the journey) |
| event | `subscribe`, `publish` | connect handover | **DEFERRED** |

The journey's capabilities (`portfolio.holdings@v1`, `portfolio.list@v1`, `portfolio.search@v1`, `data.status@v1`, then `portfolio.holdings-dates@v1`) are all served through bindings in their profiles (REVIEW:37-42). Note that `portfolio.search@v1` is a capability (the identifier lookup), not the built-in `atlas.search`.

### 1.5 The binding document (S:binding)
Required: `protocol`, `server`, `address`, `auth` (S:binding:7-12). `additionalProperties` not closed.

| Member | Type / values | Cite |
|---|---|---|
| `protocol` | enum `https`, `amqp`, `blob`, `snowflake`, `mcp`, `UNKNOWN`. `sse`, `wss` reserved | :14-46 |
| `server` | string `^[a-z](-?[a-z0-9])*$`, a key of the bootstrap's `servers` | :47-51 |
| `address` | string, minLength 1; for `https` MUST match `^/` | :52-56, :178-200 |
| `method` | enum `POST`, `GET`, `UNKNOWN`; **required when `protocol` is `https`** | :57-77, :190-193 |
| `encodings` | array, unique, of `json`, `ndjson`, `arrow`, `csv`, `parquet`, `cloudevents+json`, `UNKNOWN`, in preference order | :78-118 |
| `auth` | enum `bearer` (the caller's sign-in), `entra-scope` (the client's own token for the system), `ticket` (a gateway-issued credential), `UNKNOWN` | :119-143 |
| `delivery` | optional enum `single`, `chunked`, `subscription`, `operation`, `transfer`, `UNKNOWN`: overrides the interaction's delivery (a fallback answering an operation for a stream) | :144-176; Q-DOC-4 |

Every enum in a gateway-sent document includes `UNKNOWN`; a client maps an unknown value to it (§9.5.5) before validating. No request enum includes it (Q-DOC-1).

### 1.6 The built-ins (§9.3:43-66; S:builtins)
"The gateway MUST serve these, at the bindings its bootstrap names." They are the only capabilities called without first reading a profile. A built-in call carries **no** `Atlas-Profile-Expect`; its answer carries `Atlas-Profile` with the built-in's `exact-id`, and `atlas.describe` serves that profile like any other (§9.3:45; SCN:CORE-CALL-001 asserts Atlas-Profile-Expect absent on `atlas.whoami:fetch`). Requests are closed (`additionalProperties: false`); results are open (S:builtins:5). Members without `?` are required (§9.3:66).

| Built-in | Interaction(s) | Request | Result | First build |
|---|---|---|---|---|
| `atlas.whoami` | `fetch` | `{}` (whoami_request, S:builtins:7-12) | whoami_result: required `principal` (string, `x-atlas-logical: Principal`, Entra object id), `name`, `kind` (`person`, `workload`, `UNKNOWN`), `roles` (string[]), `environment` (`^[a-z](-?[a-z0-9])*$`), `available_domains` (string[] of that pattern), `house_rules` (catalog-page house_rules) (S:builtins:13-79) | **IN SCOPE** |
| `atlas.catalog` | `fetch` | catalog_request: `domain?` (pattern), `family?` (`data`,`resource`,`action`,`event`), `effect?` (`read`,`write`,`external`,`irreversible`), `previews?` boolean default `false`, `cursor?` (minLength 1; absent for the first page), `page_size?` integer 1-500 default 100 (S:builtins:80-129) | Catalogue page (§4.3) | **IN SCOPE** |
| `atlas.search` | `fetch` | search_request: `words` required string 1-200 chars, `cursor?`, `page_size?` 1-500 default 100 (S:builtins:130-158; Q-BI-1: §9.3 gives no range, references take catalog's) | Catalogue page, ranked, items with `score`, `why` | **IN SCOPE** |
| `atlas.describe` | `fetch` | describe_request oneOf: `{ "by": "id", "id": <capability-id or pinned-id> }` or `{ "by": "exact", "exact": <exact-id> }`, each closed (S:builtins:159-213) | Describe answer (§4.2) | **IN SCOPE** |
| `atlas.guide` | `fetch` | guide_request oneOf `{by:"id", id}` or `{by:"exact", exact}`, each with `client?` (`cli`,`python`,`node`,`go`,`mcp`) and `max_tokens?` integer 200-20000 default 4000 (S:builtins:214-306) | guide_result: required `exact` (exact-id), `client` (enum + `UNKNOWN`), `markdown` ("returned unchanged"), `tokens` (int ≥0), `truncated` (bool: sections shortened to fit), `revisions` (object: profile prose, governance, context; members not fixed, Q-BI-2) (S:builtins:307-372) | **IN SCOPE** |
| `atlas.access` | `fetch` | access_request: `id?` (capability-id or pinned-id), `domain?`, `cursor?` (S:builtins:373-402) | Page of access_entry: required `id`, `status` (`available`,`partial`,`not_granted`,`denied`,`not_in_environment`,`limited`,`deprecated`,`UNKNOWN`), `reason`, `next`; optional `held_by`, `limits` (governance limit[]), `interactions` (object) (S:builtins:403-489) | **AMBIGUOUS:** not named in AC:238, but it is a Core client operation (§9.7:26) and AC:54 says "The access view explains missing access". Reading: build it with the refusals of acceptance item 3 (REVIEW:48) |
| `atlas.usage` | `fetch` | `id?`, `period?` (minLength 1), `cursor?` (S:builtins:490-519) | Page of usage_entry: required `id`, `interaction`, `kind` (`rate`,`concurrency`,`output`,`budget`,`confirmation`,`UNKNOWN`), `used`, `limit`, `remaining` (numbers), `period`; optional `pool` (S:builtins:520-604) | Not named in AC:238; tied to budgets (step 3). Treat as optional |
| `atlas.setup` | `fetch` | `harness?` (S:builtins:605-616) | `{ steps: [{ name, done, command?, detail }] }` (S:builtins:617-649) | Not named; optional |
| `atlas.clients` | `fetch` | `cursor?` (S:builtins:650-661) | Page of client_entry: all required `name`, `language`, `built_for`, `roles[]`, `levels[]` (level enum + `UNKNOWN`), `extras[]`, `version`, `install` (S:builtins:662-746) | Not named; optional |
| `atlas.compare` | `fetch` | required `id` (capability-id), `from`, `to` (`^v[1-9][0-9]*$`), `request` (free-form object) (S:builtins:747-778) | "Table: the two results joined on their keys" | Not named; optional |
| `atlas.operation` | `get`, `list`, `cancel`, `watch` | get/cancel/watch: `{ name }`, `^operations/[a-z0-9][a-z0-9-]*$` (S:builtins:779-792; Q-BI-3); list: `cursor?`, `state?` (`pending`,`running`,`succeeded`,`failed`,`cancelled`) (S:builtins:793-815) | Operation; page of operations; `watch` = a connect handover | `get`/`list`/`cancel`: **IN SCOPE** where the journey has durable work (AC:238). `watch`: **DEFERRED** (connect handover = Events) |
| `atlas.catalog.changed` | `subscribe` | `{}` | connect handover; event data `{ catalog_etag }` (S:builtins:816-829) | **DEFERRED** (Events) |
| `atlas.settings` | `get`, `update` | `{}`; `{ fields (string[] minItems 1, unique), settings (free-form) }` with `If-Match` (S:builtins:830-860) | The caller's settings document | **DEFERRED** (a resource; R14) |
| `atlas.directory` | `fetch` | `words` 2-100 chars, `kind?` (`person`,`group`), `cursor?` (S:builtins:861-889) | Page of `{ principal, name, kind }` (kind + `UNKNOWN`) (S:builtins:890-927; Q-BI-4: pages, not tables) | Not named; optional |
| `atlas.grant`, `atlas.role`, `atlas.limit`, `atlas.context`, `atlas.switch` | standard resource methods; `atlas.context` adds `publish`, `rollback` | Their resource schemas, in their profiles | Administration; confidential; `Gateway.Admin` only (§9.3:63) | **DEFERRED** |
| `atlas.audit` | `fetch` | required `day` (date), `before?` (minLength 1), `page_size?` 1-1000 default 200 (S:builtins:928-955) | Page of audit_record (S:builtins:956-1159); confidential, `Gateway.Admin` | **DEFERRED** as a wire endpoint. **OVERRIDDEN BY REVIEW:** R09 replaces "one immutable final audit record" with admission, outcome and settlement facts (REVIEW:67), so `audit_record`'s shape may change in step 3 |

The `atlas.*` profiles MUST use exactly these schemas (§9.3:66). They are exempt from the profile lint (Q-PROF-9).

---

## 2. Headers

Rule: "Every Atlas header is a Structured Field (RFC 9651); its type is given. A receiver ignores an Atlas header it does not know, and treats one that does not parse as its type as absent." (§9.6:26). The references add: a value that parses as its type but breaks the §9.6.3 grammar is also absent (Q-HDR-1; V:headers description). The gateway never sends a header from §9.6.3 that does not parse as its type (§9.6:99). Several field lines are joined with `", "` (RFC 9110 §5.3) before parsing (V:headers description).

### 2.1 Request headers

| Header | SF type | Value / grammar | Required (§9.6:28-40; rule §9.6:100) | Gateway does | Client does | Cite / vector |
|---|---|---|---|---|---|---|
| `Authorization` | — (not SF) | `Bearer <access token>` (RFC 6750). Scheme case-insensitive (`bearer abc` → `abc`); `Bearer` alone, `Basic …`, `Bearer a b` are absent | Every call except the bootstrap | Validates the token; missing or invalid → 401 `unauthenticated` with `WWW-Authenticate` (§2.3) | Sends the token from its credential provider. Never on the bootstrap | §9.6:30; V:headers Authorization cases; SCN:CORE-BOOT-001 |
| `Atlas-Client` | Dictionary | `name` (String), `version` (String), `protocol` (String, `protocol-ver`). Example `name="fiatlas", version="2.3.0", protocol="atlas/1.0"` | Every call | Reads `protocol`; below `minimum_protocol` → 426 `upgrade_required`. Records `client` in audit (S:builtins:1011-1025) | Sends its name, version, `atlas/1.0` | §9.6:31, :55. Vectors: member order free; unknown members and parameters ignored; repeated key → last wins; `name=fiatlas` (Token) → absent; no `protocol` → absent; `protocol="atlas/1"` → absent; trailing comma → absent; upper-case key → absent |
| `Atlas-Client-Features` | Dictionary of Inner Lists of Tokens | members `levels`, `lens`, `deliveries`, `encodings`, `traits`, and `handoff`. Tokens: `levels` from `core streaming columnar bulk events handoff` (`broker` reserved); `handoff`: chunk formats (`snowflake-arrow/1`) plus `reach-snowflake`; `lens` from §9.5.3 ops; `deliveries` from `single chunked subscription operation transfer`; `encodings` from `json ndjson arrow csv parquet`; `traits`: trait names, lowercased | Every call | Chooses delivery, encoding and resolution by it; serves "the next way down" (§9:43). Computes describe `eligibility` from it (S:describe:46). If the client lacks a needed lens op, resolves the result itself and sets `Atlas-Profile` to the client's expected exact-id (§9.5:106) | Declares what it implements | §9.6:32, :52, :56. Vectors: a member left out reads as empty; `levels=()` empty; unknown token kept and ignored; unknown member ignored; `levels=core` (Token, not Inner List) → absent; a String in the list → absent; unterminated list → absent; tokens case-sensitive (`Core` is not `core`) |
| `Atlas-Profile-Expect` | String | an `exact-id` (the profile the request was written under) | Every interaction call, **except to a built-in** | Resolves the request from that profile forward to its current request schema before validating (§9.5:102). Records it as `profiles.expected` in audit (S:builtins:1067-1085) | Sends the profile of call step 2 | §9.6:33, :57. Vectors: unquoted → absent; pinned-id `"portfolio.holdings@v1"` → absent; upper-case fingerprint → absent |
| `Atlas-Deadline` | Date | absolute deadline, seconds since the epoch, e.g. `@1793318400` | Every interaction call (and every call, §9.6:100; SCN:CORE-CALL-001 asserts it on `atlas.whoami`) | Stops work at the deadline → 504 `deadline_exceeded` | `options.deadline`, else now + interaction `deadline.default`, never beyond `deadline.max` (§9.7:93). Whole seconds, rounded down (Q-HDR-2) | §9.6:34, :58. Vectors: Integer `1793318400` → absent; `@1793318400.5` → absent; `@-1` parses (and has passed). SCN:CORE-DEADLINE-001: default PT30S gives 28-31 s; a 600 s request is capped to PT2M (118-121 s) |
| `Idempotency-Key` | String | 1-255 characters of `A-Z a-z 0-9 - _ .`, unique per intended change | Every call whose effect is not `read` | Same key + same request → same answer/operation; different request → 409 `idempotency_conflict`; still running → 409 `in_progress` (retryable, `retry_after`) (§9.3:90-91) | `options.idempotency_key`, else a new random UUID, kept for every retry of this call (§9.7:93). Reused for `Atlas-Confirm` repeat (§9.7:120) and transfer commit (§9.6:87) | §9.6:35. Vectors: UUID and `run_2026.10.31` accepted; 255 chars accepted; 256 chars, empty, a space, or an unquoted Token → absent. SCN:CORE-CALL-001: "only a call whose effect is not read carries an Idempotency-Key"; SCN:CORE-CALL-004: a 503 write is retried with the same key |
| `If-Match` | — (RFC 9110 entity-tag list) | the resource's ETag | `update` and `delete` | Mismatch → 412 `precondition_failed` | Sends `metadata.etag` | §9.6:36. Vectors: `"e8"`, `"3f9a1c0b7d2e4a65"`, `*` accepted (as a list); `e8` unquoted → absent. **DEFERRED** with resources, except `atlas.settings:update` |
| `Atlas-Execution` | Token | `gateway`, `handoff` or `client` (a preference) | Optional | Treats it as a preference | `options.execution` | §9.6:37. Vectors: `"handoff"` (String) → absent; `broker` → absent ("an unknown preference is ignored"). First build: only `gateway` is served (handoff DEFERRED) |
| `X-Atlas-Interactive` | Boolean | `?1` when a person is at the client | Optional | — | Sends `?1` with a person present | §9.6:38. Vectors: `?1` true, `?0` false; `1` and `?true` absent. **AMBIGUOUS:** AC:46 says the agent-or-person distinction must come "from trusted authentication context, not an `is_human` parameter supplied by the agent". Reading: the gateway may record the header but never grant anything on it |
| `Atlas-Confirm` | Decimal | the highest estimated cost the person accepted, in the unit of the problem's `estimate`. At most 12 integer and 3 fractional digits (SF Decimal) | After `confirmation_required` | Re-evaluates the call | With a person present: show `estimate`; if accepted, repeat once with `Atlas-Confirm` = that amount and the same `Idempotency-Key`; without a person, raise (§9.7:120) | §9.6:39. Vectors: `1250.5`, `1250.0`, `0.001`, `999999999999.999` accepted; `1250` (Integer) absent (Q-HDR-4: senders always write a decimal point); `1.2345`, `1234567890123.1`, `"1250.5"` absent. **OVERRIDDEN BY REVIEW:** R06 (REVIEW:18, :64): the header is "the client's claim only"; consent needs a receipt bound to principal, address, normalised request, plan digest, scope, maximum and expiry (step 3). The receipt's wire form is not defined |
| `traceparent`, `tracestate` | — (W3C Trace Context) | `00-<32 hex trace-id>-<16 hex parent-id>-<2 hex flags>` | `traceparent` on every call | Propagates it to every hop (AGENTS.md:32 principle 14) | Sends one on every call | §9.6:40. Vectors: W3C example parses to `{version, trace_id, parent_id, flags}`; all-zero trace-id, all-zero parent-id, version `ff`, upper-case hex → absent |
| `Accept` | — (RFC 9110) | media types (§6) from the binding's `encodings`, in preference order; no `q` values | Sent by the client; required-ness not stated | Answers in the first type it can serve, else **406** with problem `invalid_request`, `target` `Accept`; ignores `q` | Lists what it reads | §9.6:63. No Accept, or `*/*`, takes the binding's first encoding (Q-HDR-5; no vector) |
| `If-None-Match` | — | the bootstrap's ETag | Bootstrap revalidation | 304 when unchanged | Sends the cached ETag | §9.6:14; SCN:CORE-BOOT-001 |
| `Content-Type` | — | `application/json` | Every POST body | — | — | §9.6:21 |

**AMBIGUOUS:** which problem answers a required header that is missing or absent by Q-HDR-1. The references' reading is `invalid_request` with `target` = the header name (Q-HDR-6, no vector). A missing or unparseable `Atlas-Client` (so no protocol) could also be `upgrade_required`. Not stated.

**AMBIGUOUS:** whether the gateway refuses a call with a missing `Atlas-Profile-Expect` on a non-built-in, or one naming an exact-id it does not serve. Not stated in §9.6 or §9.3.

### 2.2 Response headers

| Header | SF type | Value | When (§9.6:41-50) | Client does | Cite / vector |
|---|---|---|---|---|---|
| `Atlas-Profile` | String | the `exact-id` the result was written under | Every interaction answer (built-ins: the built-in's exact-id, §9.3:45) | If the fingerprint is not cached, fetch that profile by exact; resolve the answer from it to the reader profile (§9.7:95-96) | §9.6:41. Vectors: `"conformance.echo@v1#0123456789abcdef"`; a parameter (`;source=cache`) ignored |
| `Atlas-Governance-Revision` | String | the governance revision the call was decided under | Every interaction answer | Records it; compares with describe's `governance_revision` | §9.6:42; S:governance:17-21. Vectors: `"g-41"` accepted; `g-41` (Token) absent |
| `Atlas-Limit-Remaining` | Dictionary of Integers | one member per limit, e.g. `rate=58, concurrency=3` | Every metered answer | Shows or uses it | §9.6:43. Vectors: a parameter (`rate=58;window=60`) ignored; `budget=1.5` (Decimal) → absent; `rate=(58)` → absent; bare `rate` (Boolean true) → absent |
| `Atlas-Error-Code` | Token | the problem's `code` (`problem-code` grammar) | Every problem | Matches body `code` | §9.6:44; S:problem:5. Vectors: `limit_exceeded`, `warehouse_suspended` (a capability's own code) accepted; `"limit_exceeded"` (String) and `LimitExceeded` absent |
| `X-Request-Id` | — (not SF) | the call's id | Every answer | Shows it; equals the problem's `request_id` (S:problem:65-69) | §9.6:45. No vector. Used in the download place `<request id>` (§9.7:177) |
| `Deprecation` | Date (RFC 9745) | when something this call used was deprecated, e.g. `@1795046400` | When applicable | Emits the language's warning (Python `DeprecationWarning`, Node `process.emitWarning`, Go the warning hook, CLI stderr) naming what, its sunset and its replacement (§9.7:97); warning code `deprecated` (Q-RUN-3) | §9.6:46; source: profile `properties.deprecated` (date) (S:profile:149-153; Q-DOC-5). Vectors: an HTTP-date is absent |
| `Sunset` | HTTP-date (RFC 8594) | when it ends | When applicable | As above | §9.6:47; profile `properties.sunset` (date) (S:profile:154-158). Vectors: IMF-fixdate, obsolete rfc850 and asctime forms accepted (rfc850 two-digit year fixed to the 2026 reference, Q-HDR-3); wrong weekday, `@1809129600`, `UTC` instead of `GMT` → absent |
| `Retry-After` | delay-seconds only | seconds to wait | `202`, `429`, `503` | Waits before polling or retrying | §9.6:48. Vectors: `30`, `0` accepted; an HTTP-date, `1.5`, `-1` absent |
| `Operation-Location` | — | absolute URL of the operation | Every `202` | Polls it with `GET`; cancels with `POST <Operation-Location>:cancel` | §9.6:49, :16. Vectors: absolute URL accepted; relative `/v1/operations/op-91` absent |
| `Cache-Control` | — | `no-store` | Every handoff, connect, ticket and transfer answer. `max-age=31536000, immutable` on `atlas.describe` by `exact` (§9.6:95, but see §2.4) | Obeys it | §9.6:50. Vectors: `no-store`; `max-age=31536000, immutable` (as a list) |
| `ETag` | — | strong ETag | Bootstrap; profiles (`"<fingerprint>"`, §9.4:44); resources (`metadata.etag`, S:resource:48-52) | Revalidates with `If-None-Match`; writes with `If-Match` | §9.6:14; §9.4:44 |
| `WWW-Authenticate` | — | `Bearer realm="atlas"`; plus `error="invalid_token"` when expired; a conditional-access claims challenge adds `claims="<base64url>"` | Every 401 | Refresh or re-acquire the token, repeat once (§9.6:91). With a claims challenge and no person: `unauthenticated`, challenge in `details[].challenge` (S:problem:470-473; SCN:CORE-SIGNIN-004) | §9.6:91. Vectors: three cases parse to `{scheme, params}` |
| `traceparent` | — | W3C | "both" directions (§9.6:40) | — | **AMBIGUOUS:** §9.6:40 says "both" but "Required" names only "every call". Reading: the gateway MAY echo; no client depends on it |

**AMBIGUOUS:** `Retry-After` is required on `202`, `429`, `503`, but the problem schema also requires `retry_after` for `in_progress` (409) (S:problem:92-112). Reading: on 409 `in_progress`, the body member is required and the header is optional. Send both.

**AMBIGUOUS:** whether `Atlas-Governance-Revision` and `Atlas-Profile` go on problem answers. "Every interaction answer" reads as success and failure alike. Not stated.

### 2.3 Authentication challenges (§9.6.6)
- Missing or invalid token → `401`, problem `unauthenticated`, `WWW-Authenticate: Bearer realm="atlas"`; expired adds `error="invalid_token"`; claims challenge adds `claims="<base64url>"` (§9.6:91).
- The client refreshes or re-acquires the token (§9.7) "and repeats the call once" (§9.6:91). SCN:CORE-SIGNIN-003: `error="invalid_token"` → refresh and repeat once; a second 401 is `unauthenticated`.
- No usable token and a person present: the sign-in of §6.2 (browser with PKCE, or device code); without a person: `unauthenticated` (§9.7:80).

### 2.4 Caching (§9.6.7) and ETags
- Spec: "Only the bootstrap and `atlas.describe` answers are cacheable: the bootstrap by `ETag`, a profile by its fingerprint (immutable; `Cache-Control: max-age=31536000, immutable` when fetched by `exact`). Everything else is `no-store`." (§9.6:95)
- Client side: profiles cached by `exact-id` forever at `<ATLAS_HOME>/cache/profiles/` (or in memory); the catalogue cached per environment and principal for at most one hour, revalidated with its ETag at every `connect` (§9.7:102-103). SCN:CORE-DISC-002: one fetch per exact-id, then from cache, and after a restart from disk.
- **OVERRIDDEN BY REVIEW:** R01 (REVIEW:59): "The contract (by fingerprint) is cached apart from the document revision. Describe's caller state (governance, availability) is private and uncached. A guide's identity includes the prose revision." The describe answer (S:describe) holds `governance`, `availability` and `eligibility`, so it **must not** be sent `immutable`. DR:36 also says: "Reconcile the catalogue's one-hour client cache with the HTTP `no-store` rule."
  - **AMBIGUOUS** (the wire form is not decided; step 2, REVIEW:30): (a) the describe answer is `Cache-Control: no-store` (or `private, no-store`) and the client caches only the `profile` member by exact-id; (b) a separate contract fetch is added. The bootstrap's `catalog_etag` remains the catalogue validator.
- **OVERRIDDEN BY REVIEW:** R14 (REVIEW:72): a resource ETag covers the full representation; `generation` is separate. This reverses §9.3:118 ("Neither moves when only `status` changes"). **DEFERRED** with resources.
- **OVERRIDDEN BY REVIEW:** R18 (REVIEW:76): results are private to their caller; any reuse is keyed by authorisation, environment and data revision, and reauthorised. Consistent with `no-store` on results.

---

## 3. Status codes and the problem registry

### 3.1 Success and other codes

| Status | When | Cite |
|---|---|---|
| `200` | `single`, `chunked`, `subscription`/`publish` (connect handover), `fetch` answered by a handoff, a ticket | §9.6:69-76 |
| `202` | `operation` delivery (body = operation document, with `Operation-Location` and `Retry-After`); `transfer` delivery (an operation whose result is a transfer document) | §9.6:73-74 |
| `304` | Bootstrap revalidated with `If-None-Match` and unchanged | SCN:CORE-BOOT-001 note; §9.6:14 |
| `406` | No `Accept` type can be served: problem `invalid_request`, `target` `Accept` | §9.6:63; Q-DOC-8 |
| problem status | Every failure: problem document, `application/problem+json` | §9.6:77 |

### 3.2 The registry (§9.3:78-103; S:problem:115-437; V:problems)

`type` is `https://atlas.jhg.com/problems/<code>` (§9.3:105; S:problem:16-20). CLI exit codes: §9.7:118 and V:problems (they agree).

| Code | Status | Retryable | CLI exit | When (§9.3 text, verbatim) | First build |
|---|---:|---|---:|---|---|
| `invalid_document` | 400 | no | 1 | A body is not I-JSON, or has duplicate members | IN SCOPE |
| `invalid_request` | 400 (406 for Accept) | no | 2 | A request fails its schema; `target` is the first failing pointer | IN SCOPE |
| `unauthenticated` | 401 | no | 3 | No valid token; `WWW-Authenticate` says how to sign in | IN SCOPE |
| `not_granted` | 403 | no | 4 | A listed capability or interaction the caller holds no grant for | IN SCOPE |
| `denied` | 403 | no | 4 | A deny refuses it | IN SCOPE |
| `budget_exceeded` | 403 | no | 4 | Reserving the call would pass a budget | Step 3 (R07) |
| `approval_required` | 403 | no | 4 | An action awaits another principal's approval | DEFERRED (actions) |
| `confirmation_required` | 428 | no | 4 | The call's estimated cost passes the caller's confirmation threshold; `estimate` holds it (`amount`, `unit`), and the call is repeated with `Atlas-Confirm` | Step 3 (R06) |
| `not_found` | 404 | no | 5 | No such capability, version or resource, or a confidential one the caller does not hold | IN SCOPE |
| `not_in_environment` | 404 | no | 5 | It exists, but not in this environment | IN SCOPE |
| `idempotency_conflict` | 409 | no | 1 | The key was used for a different request | IN SCOPE (step 3, R08) |
| `in_progress` | 409 | yes | 6 | A request with this `Idempotency-Key` is still running; repeat it after `retry_after` to get its answer | IN SCOPE (step 3, R08) |
| `resume_gap` | 409 | no | 1 | A stream or subscription can no longer resume from the position given; the client reports the gap and starts again | DEFERRED unless resumable streams are built (AC:206) |
| `retired` | 410 | no | 5 | The version was retired at its sunset | IN SCOPE (Core) |
| `precondition_failed` | 412 | no | 1 | The ETag no longer matches | DEFERRED with resources |
| `critical_trait_unsupported` | 422 | no | 8 | The client lacks a trait the capability marks critical | IN SCOPE (client-side, no call made, §9.7:89) |
| `resolution_failed` | 422 | no | 8 | A payload could not be resolved (§9.5) | IN SCOPE |
| `upgrade_required` | 426 | no | 8 | The client's protocol is below the gateway's minimum | IN SCOPE |
| `limit_exceeded` | 429 | yes | 6 | A rate or concurrency limit; `retry_after` | IN SCOPE |
| `output_limit_exceeded` | 200 (in the end line or final frame) | no | 1 | The answer was truncated at the caller's cap | IN SCOPE (streaming) |
| `internal` | 500 | no | 1 | A fault in the gateway | IN SCOPE |
| `upstream_failed` | 502 | yes | 1 | The service's upstream failed in a way that may pass | IN SCOPE (also the client's reading of an incomplete stream) |
| `unavailable` | 503 | yes | 6 | The service is unhealthy (its circuit is open); `retry_after` | IN SCOPE |
| `deadline_exceeded` | 504 | no | 7 | The call's deadline passed; its work was stopped | IN SCOPE |
| any other code (e.g. `warehouse_suspended`) | its own | its own | 1 | A capability's declared `errors` code; handled by `status` and `retryable` | IN SCOPE (§9.3:76; SCN:CORE-PROB-002) |

Rules:
- The gateway never sends a code outside §9.3.3 and the capability's declared `errors` (§9.3:129). A profile's `errors[]` items: required `code`, `status` (100-599), `retryable`; prose `description`, `next` (S:profile:530-562).
- The schema documents the registry in `$defs/registry` and does not enforce status or retryable, because `invalid_request` also goes with 406 and `retryable` can be false for a write that cannot be repeated safely (Q-DOC-8).
- CLI exit `0` = success (§9.7:118).
- Problems become the language's error carrying every member: Python `AtlasProblem` exception (subclass per `code` where defined), TypeScript `AtlasProblem extends Error`, Go `*atlas.Problem` (`errors.As`), CLI: the problem on stderr (JSON with `--json`) and the exit code (§9.7:109-118).
- **AMBIGUOUS:** which code refuses a request for data outside the caller's data scope (R05, REVIEW:63; acceptance 3, REVIEW:48 "a correspondingly narrower result, or a clear refusal"). Partial authorisation is left to owners (REVIEW:97). Readings: `not_granted` (no grant for that resource), `denied`, or a capability-declared code. The fixture must state the choice.
- **AMBIGUOUS:** an expired or foreign cursor (cursors "live an hour", S:page:5) has no registry code. Readings: `invalid_request` with `target` `/cursor`, or a declared code.

### 3.3 Retries (§9.7.7)
"A client retries only a `retryable` problem on a replayable request, at most 3 times, waiting `retry_after` when given, else `min(30, 0.5 × 2^attempt)` seconds plus a random jitter of up to 25%, and never past the call's deadline." (§9.7:124). Replayable means "it has no streamed body" (§9.7:94). SCN:CORE-RETRY-001: 429 and 503 retried at most three times, each no sooner than Retry-After; non-retryable not retried. 401 is handled by §2.3, not by this rule.

---

## 4. Documents

General rules:
- Every JSON document is I-JSON (RFC 7493): UTF-8 without BOM; duplicate member names rejected (`invalid_document`); no unpaired surrogates or noncharacters; integers within `[-(2^53)+1, (2^53)-1]`, larger integers and every exact decimal as strings; member order carries no meaning except for a canonical form; strings compared by code point, never normalised; unknown members ignored unless the schema closes the object (`additionalProperties: false`), which only request bodies do (§9.1:22-30).
- Every document the gateway sends validates against its schema; every client validates every document before use and treats a failure as `invalid_document` (§9.3:128).
- Schemas have `$id` `https://atlas.jhg.com/protocol/1.0/<file>` (§9.3:13). "The schema is normative for structure; this text is normative for meaning."
- Null vs absent: in a request, an absent optional parameter takes its default; `null` only where the schema allows. In a result, a nullable field is always present (`null` when no value); a field not required MAY be absent (§9.1:77).
- **AMBIGUOUS:** Q-ARROW-1: `properties` order drives the Arrow field order (§9.2:119) while I-JSON says order has no meaning. The references read document order, so a reader must parse `properties` keeping member order (Go's `map[string]any` does not). R12 (DR:132) asks for an explicit column order.

### 4.1 Bootstrap (S:bootstrap) — `GET /.well-known/atlas`. IN SCOPE
Required: `protocols`, `minimum_protocol`, `environment`, `servers`, `builtins`, `sign_in`, `catalog_etag` (:7-15).

| Member | Type | Cite |
|---|---|---|
| `protocols` | array (minItems 1, unique) of `^atlas/[0-9]+\.[0-9]+$` | :17-26 |
| `minimum_protocol` | string, same pattern; below it a call is `upgrade_required` (426) | :27-31 |
| `environment` | string `^[a-z](-?[a-z0-9])*$`: `dv`, `ac`, `pd`, `local` or `conformance`. Names the token file `token-<environment>.json` (Q-CLI-1); an emulator connect handover is accepted only when it is `conformance` or `local` | :32-36 |
| `servers` | object, minProperties 1, keys `^[a-z](-?[a-z0-9])*$`; each `{ url (required, format uri, pattern [^/]$), description? }` | :37-62 |
| `builtins` | object keyed by built-in id (`^atlas(\.[a-z](-?[a-z0-9])*)+$`); each `builtin` | :63-72 |
| `builtin` | required `version` (integer ≥1: `atlas.whoami@v1` is 1), `interactions` (object, minProperties 1, keys interaction names; each `{ bindings: [binding] }`, minItems 1, in preference order) | :83-119 |
| `sign_in` | required `authority` (uri), `client_id` (minLength 1), `scopes` (array minItems 1 of non-empty strings; audience is the Atlas Gateway), `token_endpoint` (uri). Optional `authorization_endpoint` (PKCE), `device_authorization_endpoint` (RFC 8628). Every endpoint is given; a client builds none (Q-DOC-2) | :120-165 |
| `catalog_etag` | string `^(W/)?"[!#-~]*"$`, an entity-tag with its quotes | :76-80 |

Example: fx:gateway:19-388 (`servers.gateway.url` = `{gateway}`, `sign_in.authority` = `{gateway}/oauth`, `token_endpoint` `{gateway}/oauth/token`, `catalog_etag` `"catalog-1"`).

**OVERRIDDEN BY REVIEW:** R26 (REVIEW:84): "an unauthenticated bootstrap cannot redirect a credential". §9.7:79 says a token is refreshed "at the bootstrap's `sign_in` authority". Under R26 a client must not send an existing token or refresh token to an authority or token endpoint just because an unauthenticated bootstrap names it; credentials are bound to the trusted gateway, authority, audience, tenant or account, and scopes, and refresh is serialised (AC:52). **AMBIGUOUS:** how the client learns the trusted binding (config, first-use pinning, or provider). Step 4.

### 4.2 Describe answer (S:describe). IN SCOPE
Required: `profile` (S:profile), `governance` (S:governance), `availability` (`available`, `unavailable`, `UNKNOWN`), `eligibility` (array) (:7-48).
- `eligibility[]`: required `interaction` (name pattern), `eligible` (array of `{ binding (string, required), encoding?, limits? (object) }`, "in preference order"), `ineligible` (array of `{ binding (required), missing (string[], required), hint? }`) (:49-109). Computed "for the calling client, from its Atlas-Client-Features" (:46).
- **AMBIGUOUS:** `eligible[].binding` is a string, but a binding has no id. Readings: the binding's `address`; or its index in the interaction's `bindings`. Not stated.
- Caching: see §2.4 (R01).

The profile (S:profile; structure only). Required: `identity`, `properties`, `interactions`, `schemas`, `errors`, `meaning`, `fingerprint` (:7-15).
- `identity`: required `id` (capability-id), `version` (int ≥1), `domain`, `owner`; prose `title`, `summary` (≤200 chars) (:17-55).
- `properties`: required `family` (`data`,`resource`,`action`,`event`,`UNKNOWN`), `stability` (`official`,`community`,`upstream`,`UNKNOWN`), `lifecycle` (`preview`,`stable`,`deprecated`,`retired`,`UNKNOWN`), `visibility` (`listed`,`confidential`,`UNKNOWN`), `traits` (unique `{name: ^[A-Z][A-Za-z0-9]*$, critical: boolean}`); optional `deprecated` (date), `sunset` (date), `replaced_by` (capability-id or pinned-id) (:56-202, :296-313).
- `interactions.<name>`: required `effect` (`read`,`write`,`external`,`irreversible`,`UNKNOWN`), `result` (`table`,`record`,`collection`,`content`,`ticket`,`UNKNOWN`), `delivery` (`single`,`chunked`,`subscription`,`operation`,`transfer`,`UNKNOWN`), `execution` (array minItems 1 of `gateway`,`handoff`,`client`,`UNKNOWN`), `deadline` (`{default, max}`, both `format: duration`), `bindings` (minItems 1); optional `takes`, `returns` (schema names `^[a-z][a-z0-9_]*$`), `encodings` (file formats for transfer/export) (:314-529).
- `schemas` (object of schemas by name; profiles carry no Arrow schema, Q-DOC-6), `errors` (array), `meaning` (object), `compatibility` (schema → `BACKWARD`…`FULL_TRANSITIVE`,`UNKNOWN`), `usage` (prose, removed by projection), `evolution` (`previous_profile` exact-id, `lens`, `previous_version` pinned-id, `version_lens`, `changelog`), `fingerprint` (`^[0-9a-f]{16}$`), `revision` (int ≥0: "The prose's revision: it moves without the fingerprint") (:214-293, :563-665).
- A trait with `critical: true` means a client that does not implement it must not call the capability (§9.3:72; S:profile:308-311).
- A client validates the profile's structure and does not refuse a newer profile's schemas (Q-DOC-3; S:profile:5).
- **OVERRIDDEN BY REVIEW:** R03/R04 (REVIEW:61-62, :90): a client is obliged only to aliases, defaults, UNKNOWN and projection; "no served profile may depend on `convert_unit`, `promote` or `drop` within a version". A request is validated under its own contract before any transformation, and unknown request fields are refused, never dropped.

### 4.3 Catalogue page (S:catalog-page) — `atlas.catalog`, `atlas.search`. IN SCOPE
Required: `items`, `next_cursor`, `house_rules` (:7-11).

| Member | Type | Cite |
|---|---|---|
| `items[]` | `item` | :13-18 |
| `next_cursor` | string (minLength 1) or `null` (null on the last page) | :19-26 |
| `house_rules` | required `revision` (minLength 1), `rules[]` of `{ text (minLength 1), capabilities (capability-id[]) }`; a rule reaches a caller only when the caller can use every capability it names | :171-210 |
| `item` required | `id` (capability-id), `version` (int ≥1), `exact` (exact-id: "Its current profile, with the fingerprint"), `summary`, `family` (enum + `UNKNOWN`), `interactions` (object: interaction name → `{ effect }` (required; `read`,`write`,`external`,`irreversible`,`UNKNOWN`)): "The interactions the caller may use" | :32-41, :118-160 |
| `item` optional | `title`, `lifecycle` (`preview`,`stable`,`deprecated`,`retired`,`UNKNOWN`), `score` (number, search), `why` (string, search) | :56-117, :161-168 |

SCN:CORE-DISC-001: pages are followed by cursor; every profile's fingerprint is recomputed and a wrong one is `invalid_document`.

### 4.4 Page (S:page) — every collection result. IN SCOPE
Required `items` (array of objects, each in the collection's item schema) and `next_cursor` (string minLength 1, or null at the end). "Cursors are opaque, bound to the request and the caller, and live an hour (§6.7)." (:5-27)

### 4.5 NDJSON end line (S:chunk-end). IN SCOPE
Required: `$atlas` (const `"end"`), `rows` (int ≥0, rows sent), `truncated` (boolean: cut at the caller's output cap), `usage` (object: what the call used), `problem` (`null` or a problem) (:7-41). It is also the JSON of an Arrow stream's final batch's `atlas:end` metadata.

### 4.6 Problem (S:problem). IN SCOPE
RFC 9457 + Atlas members; media type `application/problem+json`; `Atlas-Error-Code` equal to `code` (:5).
Required: `type`, `status`, `code`, `retryable`, `next`, `request_id` (:7-14).

| Member | Type | Cite |
|---|---|---|
| `type` | `^https://atlas\.jhg\.com/problems/[a-z][a-z0-9_]*$` | :16-20 |
| `title` | string, optional | :21-23 |
| `status` | integer 100-599 | :24-29 |
| `detail` | string, optional | :30-32 |
| `instance` | string, optional | :33-35 |
| `code` | `^[a-z][a-z0-9_]*$`: a registry code or a profile-declared one | :36-40 |
| `retryable` | boolean: "True only when repeating the same request is safe and may succeed" | :41-44 |
| `retry_after` | integer ≥0, seconds; **required** when `code` is `in_progress`, `limit_exceeded` or `unavailable` | :45-49, :92-112 |
| `target` | string: a JSON Pointer into the request, a header name, a field-mask entry, a resource name | :50-53 |
| `next` | string: "The step a person or agent can take, in words" | :54-57 |
| `details[]` | `detail` objects: one per failure for `invalid_request`, one per hop | :58-64 |
| `request_id` | string minLength 1, as `X-Request-Id` | :65-69 |
| `estimate` | **required** when `code` is `confirmation_required`: `{ amount, unit }`, both required; `amount` decimal string `^[0-9]{1,12}(\.[0-9]{1,3})?$`; `unit` minLength 1 (credits, a vendor's billed calls, USD) | :70-91, :476-496; Q-DOC-10 |
| `detail` item | all optional: `code`, `target` (validation: the instance's JSON Pointer), `keyword` (the failing keyword), `hop`, `status`, `title`, `detail`, `challenge` (unauthenticated's `WWW-Authenticate` challenge) | :439-475; Q-DOC-9 |

`invalid_request` from validation: every failure in `details` as (pointer, keyword), the first one's pointer as `target` (§9.2:99). Order: sort by pointer, then keyword, comparing code points; so `/10` sorts before `/2` (Q-VAL-1). A client validates before sending and fails with nothing sent (§9.2:141; SCN:CORE-CALL-002).

### 4.7 Operation (S:operation). IN SCOPE (where the journey needs durable work; Core level)
Required: `name`, `address`, `state`, `created`, `updated` (:7-13).

| Member | Type | Cite |
|---|---|---|
| `name` | `^operations/[a-z0-9][a-z0-9-]*$` | :15-19 |
| `address` | format `address`: the `<id>@vN:<interaction>` it runs | :20-24 |
| `state` | `pending`, `running`, `succeeded`, `failed`, `cancelled`, `UNKNOWN` | :25-57 |
| `progress` | any, optional, in the profile's terms | :58-60 |
| `result` | when succeeded: the result in writer profile `profile`; for transfer, a transfer document | :61-63 |
| `result_location` | uri: when the result is larger than 1 MiB, `GET` it with the same `Authorization` | :64-68 |
| `profile` | exact-id: the writer profile of `result` (the poll's own `Atlas-Profile` is `atlas.operation`'s) | :69-73; Q-DOC-11 |
| `problem` | problem; **required** when `state` is `failed` | :74-76, :117-133 |
| `owner` | string: who started it | :77-80 |
| `created`, `updated` | date-time | :81-88 |
| constraint | `state: succeeded` requires `result` or `result_location` | :91-116 |

Kept at least 24 hours after it ends (S:operation:5; §9.6:85).
- **OVERRIDDEN BY REVIEW:** R17 (REVIEW:75; AC:210-212): the start deadline, execution deadline and retention are separate; an HTTP disconnect during startup does not cancel an admitted operation; "Cancellation is reported as requested or confirmed"; `cancelled` only when the provider knows work stopped; retention covers the retry/recovery window, even past 24 hours. **AMBIGUOUS:** the operation schema has no member for "cancellation requested" and only one `Atlas-Deadline`. Readings: (a) `state` stays `running` after a cancel request until confirmed, with `progress` carrying the request; (b) a new optional member (a minor-version addition, §9:37). Step 3.
- **OVERRIDDEN BY REVIEW:** R08 (REVIEW:66): one logical operation per idempotency key for its whole retention, uncertain outcomes included (step 3).

### 4.8 Explain answer (S:explain). IN SCOPE
Required: `address` (format address, `<id>@vN:fetch`), `profile` (exact-id, the writer profile the plan is made under), `plan`, `result_schema`, `execution`, `estimate` (:7-14).
- `plan`: required `language` (`sql`, `http`, `UNKNOWN`); optional `dialect` (e.g. `snowflake`), `statement` ("The exact statement `fetch` would run, every column aliased and cast"), `query_tag` (:26-66).
- `result_schema`: object, the schema of what would come back (requested columns), in the Atlas Schema profile (:67-70).
- `execution`: `gateway`, `handoff`, `client`, `UNKNOWN` (:71-95).
- `estimate`: required `rows` (int ≥0 or null), `bytes` (int ≥0 or null), `cost` (null or problem `estimate` `{amount, unit}`) (:96-129).
- `warnings[]`: optional `{ code (problem-code pattern), text }`, both required (:130-148).
- SCN:CORE-EXPLAIN-001: explain is validated as fetch is, and its answer returned whole, the statement byte for byte.
- R19 (REVIEW:77) and AC:194: estimates say what was estimated, its unit, basis and uncertainty; "Unknown cost remains unknown". `null` members carry unknown.

### 4.9 Governance (S:governance). IN SCOPE
"Served beside its profile by atlas.describe, never part of it or its fingerprint." (:5)
Required: `capability` (pinned-id), `governance_revision` (minLength 1; recorded in every audit record; the `Atlas-Governance-Revision` value), `for_caller` (:7-21).
- `for_caller` required: `interactions` (name → `{ held_by }` required: `role:<name>`, `user:<oid>` or `everyone`), `limits` (array of `limit`), `approval` (object or null) (:22-63).
- `limit`: required `kind` (`rate`,`concurrency`,`output`,`budget`,`confirmation`,`UNKNOWN`); optional `interaction`, `per_minute`, `max`, `max_rows`, `max_bytes` (ints ≥0), `remaining` (number), `amount` (decimal string), `unit`, `period`, `set_by` (`role:<name>`, `user:<oid>`), `resets` (date-time) (:66-147).

### 4.10 Lock (S:lock). IN SCOPE (`atlas lock`, CORE-LOCK-001), format still open
`atlas.lock`, TOML, parsed to JSON then validated (:5; §9.7:144). Found in the current directory or nearest parent containing one, or a path given to `connect`.
- Required `capabilities`: object keyed by capability-id; each required `version` (int ≥1) and `fingerprint` (`^[0-9a-f]{16}$`); optional `dynamic` (boolean: types not promised; a DynamicSchema capability is held only as `dynamic = true`) (:11-37; §9.3:70).
- Optional `extras`: string[] (minLength 1), the client extras the application uses (:38-45).
- The exact-id of an entry is `<key>@v<version>#<fingerprint>`. SCN:CORE-LOCK-001: entries are the reader profiles, fetched by exact, with no discovery call for them.
- **OVERRIDDEN BY REVIEW:** R02 (REVIEW:60): "A lock pins semantic contracts. Routing is fetched as current metadata. 'No discovery at runtime' means no new business-route selection." See §7.4.
- Left to owners: "The minimum sunset and the lock's format, which the spec still has open" (REVIEW:99).

### 4.11 Token store entry (S:token-store). Contested by the review
`<ATLAS_HOME>/state/token-<env>.json` (:5; §9.7:76). Required `access_token` (minLength 1), `expires_at` (integer, seconds since the epoch); optional `refresh_token` (minLength 1) (:7-24).
Rules (§9.7:76-81; :5): mode `0600` in a `0700` directory, and a client refuses a file with wider permissions (SCN:CORE-SIGNIN-005: mode 0644 refused, token never sent); written atomically (temporary file in the same directory, then renamed); refreshed within 60 seconds of `expires_at`; a workload passes a token to `connect` or sets `ATLAS_TOKEN` and never writes the store. `ATLAS_HOME` defaults to `~/.atlas` (§9.7:70). `<env>` is the bootstrap's `environment` (Q-CLI-1).
- **OVERRIDDEN BY REVIEW:** R11 (REVIEW:69): "Credentials come from each runtime's own provider" (a browser needs no token file, DR:124). R26 (REVIEW:84): "The token file is keyed by environment" is replaced by credentials bound to the trusted gateway, authority, audience, tenant or account, and scopes; refresh serialised. **AMBIGUOUS:** the new entry's members and file key are not defined. Readings: (a) keep the schema and add binding members (minor-compatible); (b) a provider interface per runtime with the file as the CLI/Python/Go provider's store. Step 4.

### 4.12 Resource envelope (S:resource). DEFERRED (R14, REVIEW:72)
Required `name` (`^[a-z](-?[a-z0-9])*(/[a-z](-?[a-z0-9])*/[a-z0-9][a-z0-9-]*)+$`, `x-atlas-key`), `metadata`; `status` only for converged resources (:7-21). `metadata` required: `uid`, `etag` (entity-tag), `generation` (int ≥1), `created`, `updated`, `owner` (Principal), `labels` (string map; the one member a caller writes), `deleting` (date-time or null) (:29-91). `status` required: `observed_generation` (int ≥0), `conditions[]` each required `type`, `status` (`true`,`false`,`unknown`,`UNKNOWN`), `reason`, `message`, `observed_generation`, `last_transition` (:92-178). Rules §9.3:118-122 (ETag rule overridden by R14).

### 4.13 Transfer (S:transfer). DEFERRED (Bulk; direct downloads)
Required `kind` (`download`,`upload`,`UNKNOWN`), `operation` (operation name), `expires` (date-time) (:7-11). Optional `protocol` (`azure-blob`,`http-range`,`graph-upload-session`,`UNKNOWN`), `files` (required for download), `parts` and `commit` (binding) (required for upload), `chunking` (`part_bytes` 1-8388608, `parallelism` ≥1, `checksum` `sha256`) (:12-152). `file`: required `name`, `media_type`, `bytes`, `sha256` (64 hex); optional `metadata`, `parts` (minItems 1), `inline` (base64, under 1 MiB: the Core client's fallback, Q-DOC-14) (:191-270). `commit_request` (closed): `operation`, `parts[]` of `{ url, sha256 }` (:271-307). Flow: §9.6:87.
- Note: if the journey needs a Core-level `content` result, the `inline` path is the only one without direct links (Q-DOC-14). Not obviously needed by the journey.

---

## 5. Delivery and framing (§9.6.5)

| Delivery | Status | Body | Media type | First build |
|---|---:|---|---|---|
| `single` | 200 | the result as JSON: a `record` → object; a `collection` → a page (S:page); a `table` → a JSON array of row records | `application/json` | IN SCOPE |
| `chunked` | 200 | NDJSON or an Arrow IPC stream, framed as below | `application/x-ndjson` / `application/vnd.apache.arrow.stream` | NDJSON IN SCOPE; Arrow AMBIGUOUS (§0) |
| `subscription` | 200 | a connect handover, rights `listen`; `Cache-Control: no-store` | `application/vnd.atlas.connect+json` | DEFERRED |
| `publish` of an event capability | 200 | a connect handover, rights `send`; `no-store` | same | DEFERRED |
| `operation` | 202 | the operation document; `Operation-Location`, `Retry-After` | `application/json` | IN SCOPE |
| `transfer` | 202 | an operation whose result, when `succeeded`, is a transfer document | `application/json` | DEFERRED |
| `fetch` answered by a handoff | 200 | the handoff document; `no-store` | `application/vnd.atlas.handoff+json` | DEFERRED |
| a ticket (`:ticket`) | 200 | the ticket document; `no-store` | — | DEFERRED (O54) |
| a failure | the problem's status | the problem document | `application/problem+json` | IN SCOPE |

### 5.1 NDJSON (§9.6:79)
1. One JSON record per line: a row (table) or an item (collection).
2. Each line ends with a single `LF`; no blank lines.
3. The **last** line is the end line `{ "$atlas": "end", "rows", "truncated", "usage", "problem" }` (S:chunk-end).
4. A failure after the first row is reported in the end line's `problem`, not by the status (already sent).
5. "A body that ends without an end line is incomplete: the client treats it as the problem `upstream_failed`." (§9.6:79; AP-HTTP-002 §9.6:110; SCN:STREAM-002)
6. "An answer without an end line or final batch is never treated as complete." (§9.6:101)

### 5.2 Arrow (§9.6:81; §9.2:117-136)
1. The Arrow IPC **streaming** format.
2. The schema message, with schema metadata `atlas:profile` (the exact-id it was written under) and `atlas:protocol` (`atlas/1.0`), as plain text (Q-ARROW-4).
3. Record batches.
4. A final record batch of **zero rows** whose message custom metadata `atlas:end` holds the end line's JSON.
5. The end-of-stream marker.
6. A stream without that final batch is incomplete: `upstream_failed` (SCN:COL-002).
- Arrow schema derived from the table result schema, one field per row property in `properties` order: boolean→`Bool`, integer→`Int64`, number→`Float64`, string→`Utf8`, `format: date`→`Date32`, `format: date-time`→`Timestamp(MICROSECOND, "UTC")`, `format: duration`→`Duration(MICROSECOND)`, `format: int64`→`Int64`, `Decimal(p,s)`→`Decimal128(p, s)`, `Money`→`Struct<amount: Decimal128(38,6), currency: Utf8>`, array→`List<T>`, object with properties→`Struct` (§9.2:121-134). A field is nullable exactly when its property allows `null` or is not `required`. Atlas keywords are carried as field metadata `atlas:<keyword without x-atlas->` with their JSON text (§9.2:136). SCN:COL-001: Arrow gives the same records as NDJSON, decimals exact, timestamps UTC microseconds.

### 5.3 Usage and truncation
- `usage` is in the end line (object; members not fixed) (S:chunk-end:27-30). Q-RUN-5: the end line's `usage` is reported in the result as `usage` (SCN:STREAM-001).
- Truncation at the caller's output cap: `truncated: true` and `problem` = `output_limit_exceeded` (status 200), which a client reports as a **warning**; any other problem in an end line is **raised after the rows** (S:chunk-end:5; Q-DOC-15; SCN:STREAM-003, STREAM-004). A failed result may carry the rows read before the failure (Q-RUN-6).
- Metered answers also carry `Atlas-Limit-Remaining` (§9.6:43).
- AC:206: "A stream has an explicit completion marker; EOF alone is not success. A truncated result must be marked incomplete, with any resumable position tied to the same result and scope. Resuming must not silently rerun a changing query and concatenate different snapshots."

### 5.4 Operations (§9.6:85)
- Poll `GET <Operation-Location>` no sooner than `Retry-After` (default 1 second, doubling to at most 30), until `state` is `succeeded`, `failed` or `cancelled`.
- A `result` larger than 1 MiB is given by `result_location`, fetched with `GET` and the same `Authorization`.
- Kept at least 24 hours after it ends.
- Cancel: `POST <Operation-Location>:cancel` (§9.6:16), or `atlas.operation:cancel { name }` (§9.3:59).
- SCN:CORE-OP-001: 202 and an operation; polls no sooner than Retry-After, then the default doubling from 1 s to at most 30 s; reads `result`, or `result_location` above 1 MiB; cancel reaches `cancelled`.
- Q-RUN-5: `call` on an `operation` delivery returns the operation document, and `operation.wait` its result.
- **AMBIGUOUS:** two cancel routes exist (`<Operation-Location>:cancel` and the built-in). Reading: the gateway serves both; a client uses the operation's URL.
- **AMBIGUOUS:** "doubling" from what baseline when `Retry-After` is given on the first 202 but not on later polls. Reading: honour each answer's `Retry-After`; without it, 1, 2, 4 … 30 s.

### 5.5 Paging
- A `collection` in `single` delivery is a page: `items`, `next_cursor`; follow `next_cursor` in the request's `cursor` member until `null` (S:page; S:builtins cursor members). SCN:CORE-DISC-001 asserts the cursor chain.
- In NDJSON, each line is an item (§9.6:79). **AMBIGUOUS:** how a chunked collection reports its next cursor (the end line has no `next_cursor`). Reading: a chunked collection is the whole collection; paging is a `single` concern.

### 5.6 A `chunked` interaction asked for `application/json`
- **AMBIGUOUS:** §9.6.5 gives `single` a JSON array for a table but says `chunked` is "NDJSON or an Arrow IPC stream". The references read: `conformance.rows@v1:fetch` (delivery `chunked`, binding encodings `arrow, ndjson, json`) answers "NDJSON, Arrow or a JSON array by Accept" (references/README.md:177). A Core-only client therefore receives a JSON array.
  - Consequence: a JSON array has no end line, so truncation, usage and an incomplete answer cannot be signalled the same way. Readings: (a) the gateway buffers and sends a JSON array only when complete, and fails with a problem otherwise; (b) a wrapper object. R12 (DR:130-132) lists this kind of framing gap. ATLAS-CORE says completeness must have parity across clients (AC:136; REVIEW:50 "incomplete streams … behave the same in every client"). This must be decided before the clients are built.
- DynamicSchema in JSON: "a first member `columns` listing each column's name and type" (§9.3:70), read as `{"columns": [{"name", "type"}], "rows": [...]}` (Q-DYN-1). Not in the journey.

---

## 6. Content negotiation (§9.6.4; §9.1.7; §9.2.6)

| Encoding token (binding `encodings`) | Media type | For | Level |
|---|---|---|---|
| `json` | `application/json` | JSON documents and single answers | Core |
| `ndjson` | `application/x-ndjson` | chunked answers as JSON lines | Streaming |
| `arrow` | `application/vnd.apache.arrow.stream` | chunked answers as an Arrow IPC stream | Columnar |
| `csv` | `text/csv` | tables for people, never parsed by a client | — |
| `parquet` | `application/vnd.apache.parquet` | export files | Bulk |
| `cloudevents+json` | `application/cloudevents+json` | an event, alone | Events |
| — | `application/problem+json` | problems (always, whatever Accept says) | Core |
| — | `application/vnd.atlas.profile+json` | a profile | — |
| — | `application/vnd.atlas.handoff+json` | a handoff document | Handoff |
| — | `application/vnd.atlas.connect+json` | a connect handover | Events |

Rules:
1. The client sends `Accept` listing the types it reads, chosen from the binding's `encodings`, in preference order (§9.6:63).
2. The gateway answers in the first it can serve, or `406` with `invalid_request`, `target` `Accept` (§9.6:63).
3. No `q` values; the gateway ignores them (§9.6:63).
4. No Accept, or `*/*`: the binding's first encoding (Q-HDR-5, a reading).
5. `Atlas-Client-Features` `encodings` and `levels` also bound what is served: "Arrow becomes NDJSON" for a client without Columnar (§9:43).
- **AMBIGUOUS:** precedence between `Accept` and `Atlas-Client-Features`. With Q-HDR-5, a Core client that sends no Accept to `portfolio.holdings@v1:fetch` (first encoding `arrow`) would get Arrow, which contradicts §9:43. Reading: the gateway intersects Accept with the declared features, then takes the first. Clients should always send Accept.
- **AMBIGUOUS:** `csv` framing (end, truncation) is not defined; it is display-only (§9.1:87).
- Which answer each delivery gives in each type: §5 table. The `profile` media type is listed in §9.1.7, but `atlas.describe` returns the describe answer as `application/json` (§9.3:22; its binding encodings are `json` in fx:gateway). **AMBIGUOUS:** where `application/vnd.atlas.profile+json` is used. No delivery in §9.6.5 names it.

---

## 7. Fingerprints and exact ids

### 7.1 Grammar (§9.1:34-50)
```abnf
segment       = %x61-7A *( %x61-7A / DIGIT / "-" )
capability-id = segment 1*( "." segment )
version       = %s"v" nz-digits
nz-digits     = %x31-39 *DIGIT
interaction   = segment
fingerprint   = 16( DIGIT / %x61-66 )
pinned-id     = capability-id "@" version
exact-id      = pinned-id "#" fingerprint
address       = ( capability-id / pinned-id ) ":" interaction
property-name = %x61-7A *( %x61-7A / DIGIT / "_" )
problem-code  = %x61-7A *( %x61-7A / DIGIT / "_" )
```
- A capability id is at most 128 characters; an address at most 160. A segment never ends with `-` and never contains `--`. A domain of `atlas` names a built-in (§9.1:50).
- Every Atlas grammar is case-sensitive (§9.1:16); a sender MUST NOT send, and a receiver MUST reject, a value that does not match (§9.1:96).
- Examples: `portfolio.holdings@v1`, `portfolio.holdings@v1#3f9a1c0b7d2e4a65`, `portfolio.holdings@v1:fetch` (§9.1:42-44).
- Dates, times, durations (§9.1:55-62): a `date-time` MUST carry an offset and the gateway MUST send `Z`; fractions truncated, never rounded, to the receiver's precision; no leap second `60`; `duration` uses only D, H, M, S (no years, months, weeks); `P` alone not allowed. Q-TIME-1: microsecond instants written with `Z` and exactly six fractional digits.
- Decimals (§9.1:66-73): JSON string `[ "-" ] ( "0" / nz-digits ) [ "." 1*DIGIT ]`; at most `p` significant digits and `s` after the point; a sender writes exactly `s` digits when `s > 0`; `-0` not allowed. Q-DEC-1: SQL meaning of precision (integer digits + s ≤ p). Clients must not turn an exact amount into binary floating point (AC:134).

### 7.2 The algorithm (§9.4.1-9.4.2)
Projection of profile P:
1. Copy P.
2. Remove top-level `fingerprint`, `revision`, `usage`.
3. Remove `identity.title`, `identity.summary`, `identity.owner`.
4. Remove `description` and `next` from every member of `errors`.
5. Remove `evolution.changelog`.
6. For each member of `schemas`, apply StripSchema: remove `title`, `description`, `$comment`, `examples`; recurse into every value of `properties` and `$defs`, into `items`, into `additionalProperties` if an object, and every element of `prefixItems`, `anyOf`, `oneOf`; remove `description` from every element of `x-atlas-enum`; remove `reason` from `x-atlas-deprecated` if an object. Only at schema positions (a property *named* `title` is kept). Everything else, including `meaning`, unchanged.
Fingerprint:
1. Serialise the projection by RFC 8785 (JCS): members sorted by UTF-16 code units, no whitespace, minimal escapes, ECMAScript shortest numbers.
2. UTF-8.
3. SHA-256.
4. 64 lowercase hex; the fingerprint is the first 16.
(§9.4:15-35)

- The gateway computes it at registration and serves it in `fingerprint`; a client recomputes it on every profile it receives and treats a mismatch as `invalid_document` (§9.4:37; §9.7:104).
- The gateway never serves two projections under one fingerprint; a colliding newer one is refused at registration (§9.4:58).
- Vectors: `references/vectors/fingerprints.json` and `vectors/profiles/` (§9.4:53; AP-FP-001 §9.4:66).

### 7.3 Where fingerprints appear (§9.4:41-49)

| Use | Form |
|---|---|
| One exact contract | `exact-id` `portfolio.holdings@v1#3f9a1c0b7d2e4a65` |
| A profile's HTTP `ETag` | `"3f9a1c0b7d2e4a65"` (strong) |
| `Atlas-Profile`, `Atlas-Profile-Expect` | an exact-id as an SF String |
| An event's `dataschema` | `urn:atlas:profile:portfolio.holdings@v1:3f9a1c0b7d2e4a65` |
| An Arrow stream's `atlas:profile` metadata | an exact-id |
| A lock's entry | `fingerprint` |
| An audit record | the writer's and the expected exact-id |

### 7.4 Pinning and resolving (§9.7:88, :95-96, :102-105; §9.5:100-106)
1. **Which profile is the reader's** (call step 2): if the application's lock has the capability, its exact-id; else, if the address is pinned (`@vN`), the newest cached profile of that version; else the current stable version. Fetch with `atlas.describe` if not cached. A bare id is allowed at a terminal and in a notebook; a program pins through its lock (§9.7:58).
2. **Fetch by exact:** `atlas.describe { "by": "exact", "exact": "<exact-id>" }`. By id: `{ "by": "id", "id": "<capability-id or pinned-id>" }`.
3. **Send** `Atlas-Profile-Expect: "<reader exact-id>"`; the request body is written under it. The gateway resolves the request forward (SCN:CORE-RES-003).
4. **Read** `Atlas-Profile`; if that fingerprint is not cached, fetch that profile (and the chain's) by exact; resolve every record from the writer's profile to the reader's (SCN:CORE-RES-001). If the client lacks a lens op, the gateway resolves for it and sets `Atlas-Profile` to the client's expected exact-id (§9.5:106).
5. A capability the client has not seen is never refused by the client: an address missing from the cached catalogue is looked up with `atlas.describe`; only the gateway says `not_found` (§9.7:105; AP-CLIENT-004). This is acceptance item 4 (REVIEW:49).
6. Rows answering a request that named `columns` are validated against the result schema projected to those columns (§9.3:128; SCN:CORE-RES-002).
- **OVERRIDDEN BY REVIEW:** R01 + R02 (REVIEW:30, :59-60): step 2 must "split the contract fingerprint, the document revision and the routing". Today the projection keeps `interactions.*.bindings` (only the members in §7.2 are removed), so moving a binding changes the fingerprint, while `04-plugging-in.md:283` calls a moved binding a safe change and R02 says routing is fetched as current metadata.
  - **AMBIGUOUS** (step 2 decides): (a) keep §9.4 unchanged, so the fingerprint vectors still pass, and have clients take bindings from the *current* describe (by id) while pinning the semantic contract by exact-id; (b) remove bindings from the projection. That changes every fingerprint in `fingerprints.json`, and by §9 Purpose (§9:15) the text and its vectors change first. Either way a locked program must use the new route and current grants (DR:46 acceptance).
- R03 (REVIEW:61): within a version only optional request fields, result fields with a defined missing value, and aliases for exact renames. The client's resolution obligation is aliases, defaults, UNKNOWN and projection (REVIEW:90).

---

## 8. The order a client walks the protocol

| # | Step | Wire | Cite |
|---|---|---|---|
| 1 | Choose the gateway | first set wins: `connect` URL or env → `ATLAS_URL` → `ATLAS_ENV` → `env` in `<ATLAS_HOME>/config.toml` → Production. Env name → URL via `[environments.<name>] url` or built-ins `dv`, `ac`, `pd` | §9.7:62-70; SCN:CORE-BOOT-002 |
| 2 | Bootstrap | `GET <gateway>/.well-known/atlas`, no `Authorization`; cache by `ETag`, revalidate with `If-None-Match` (304). Check `minimum_protocol` (a client may fail at connect or at the first call with `upgrade_required`; the suite accepts either, SCN:CORE-UPGRADE-001 note). Take `servers`, `builtins`, `sign_in`, `environment`, `catalog_etag` | §9.6:14; S:bootstrap |
| 3 | Credentials | the token from `<ATLAS_HOME>/state/token-<environment>.json`; refresh within 60 s of `expires_at` at the token endpoint; on none: interactive sign-in or `unauthenticated` | §9.7:76-81; Q-CLI-1. **OVERRIDDEN BY REVIEW:** R11, R26 (§4.11) |
| 4 | `whoami` | `POST <builtins["atlas.whoami"] binding>` body `{}`; headers: `Authorization`, `Atlas-Client`, `Atlas-Client-Features`, `Atlas-Deadline`, `traceparent`; **no** `Atlas-Profile-Expect` | §9.3:49; SCN:CORE-CALL-001 |
| 5 | Find | `atlas.catalog` `{domain?, family?, effect?, previews?, cursor?, page_size?}` or `atlas.search` `{words, cursor?, page_size?}`; follow `next_cursor`; each item gives `exact`. Catalogue cached ≤1 h per environment and principal, revalidated by ETag | §9.3:50-51; §9.7:103 |
| 6 | Understand | `atlas.guide {by, id|exact, client?, max_tokens?}` (for people: Markdown returned unchanged, in the calling client's syntax; SCN:CORE-GUIDE-001); `atlas.describe {by, id|exact}` (machine contract + governance + availability + eligibility). Recompute the fingerprint | §9.3:52-53; §9.4:37 |
| 7 | Inspect (optional) | `call(<id>@vN:explain, request)`: a separate, non-fetching interaction | §9.7:25; AC:132 |
| 8 | Call | §9.7.4 steps: parse address (`invalid_request`) → find profile (lock / pinned / current) → critical traits (`critical_trait_unsupported`, no call) → find interaction (`not_found`) → validate request (`invalid_request`, nothing sent) → choose binding (first supported) → build: body, `Atlas-Client`, `Atlas-Client-Features`, `Atlas-Profile-Expect`, `Atlas-Deadline` (default, capped at max), `traceparent`, `Idempotency-Key` for non-read, `Accept` → send; retry only retryable + replayable (≤3) → read `Atlas-Profile`, fetch writer profile if needed → read by delivery, validate, resolve → warn on `Deprecation` → return | §9.7:85-98 |
| 9 | Follow | read the stream to its end line or final batch; or poll the operation; or follow page cursors | §9.6:79-85 |
| 10 | Recover | problem → language error with every member; CLI exit code | §9.7:107-118 |
| 11 | Build | `atlas lock` writes `atlas.lock`; `atlas lock upgrade` moves entries forward | §9.7:144 |

- **OVERRIDDEN BY REVIEW:** R11 (REVIEW:69; AC:132): "`call` executes once and returns a result or operation handle. Reading that handle again never silently submits another source query." Count source executions when reading a result twice (DR:124). §9.7:51's Python `.fetch(…)` returning a lazy reference must be named and documented apart from execution.
- AP-CLIENT-003 (§9.7:199): the same request record is byte-for-byte the same JSON from every client, once canonicalised by RFC 8785 (SCN:CORE-CALL-005). Acceptance 2 (REVIEW:47) requires identical request records in CLI, Python and Node.
- CLI names (§9.7:43-55): `atlas whoami`, `atlas catalog [--domain d] [--family f] [--effect e]`, `atlas search <words>`, `atlas describe <id>`, `atlas guide <id>`, `atlas explain <id> [--<param> v …]`, `atlas access [<id>] [--domain d]`, `atlas call <address> --request <json>`, `atlas fetch <id> [--<param> v …] [--format f] [--out file]`, `atlas operation get|watch|cancel <name>`; `--env`, `ATLAS_URL`, `ATLAS_ENV` on any command. Python `FIAtlas(url=None, env=None, token=None)`, `.call(address, request, *, deadline=None, idempotency_key=None, execution=None, accept=None)`. TypeScript `atlas.connect({ url?, env?, token? })`, `.call(address, request, { deadline?, idempotencyKey?, execution?, accept? })`.
- Tolerant reader: ignore unknown members, headers, event types and non-critical traits; map unknown enum values to `UNKNOWN`; never fail because the gateway is newer (§9.7:148; SCN:CORE-TOL-001).

---

## 9. Contradictions and gaps found (collected)

1. Built-in paths: `/v1/atlas/<id>:<interaction>` (04-plugging-in.md:283; references) versus the "Today" column of 07-the-catalogue.md:171-190. Reading: follow the references.
2. Fingerprint covers bindings, while R01/R02 say routing is current metadata (§7.4).
3. `atlas.describe` by exact is `immutable` (§9.6:95), but it carries live governance and availability (R01).
4. Catalogue answers are `no-store` (§9.6:95), but clients cache the catalogue for an hour (§9.7:103) (DR:36).
5. `chunked` + `application/json` framing, truncation and completeness are undefined (§5.6).
6. Accept versus `Atlas-Client-Features` precedence; Q-HDR-5's "first encoding" can pick Arrow for a Core client (§6).
7. The problem for a missing required header (Q-HDR-6).
8. Gateway behaviour for an unknown or missing `Atlas-Profile-Expect`.
9. `in_progress` 409 requires `retry_after` in the body, but the `Retry-After` header is only listed for 202/429/503.
10. `describe.eligibility[].binding` is a string with no defined id.
11. Two cancel routes; R17's "requested versus confirmed" cancellation has no wire member.
12. One `Atlas-Deadline` versus R17's separate start and execution deadlines.
13. `X-Atlas-Interactive` versus AC:46 (no human flag from the agent).
14. No registry code for a data-scope refusal (R05) or an expired cursor.
15. Token store keyed by environment versus R26's binding to gateway, authority, audience, tenant and scopes.
16. `Sunset` is an HTTP-date while the profile's `sunset` is a date. Reading: midnight GMT of that date (the vector `Sat, 01 May 2027 00:00:00 GMT` is consistent but not normative).
17. `application/vnd.atlas.profile+json` is listed (§9.1:89) but no delivery uses it.
18. `Atlas-Client-Features`: §9.6:32's table lists five members; the token paragraph (§9.6:52) and the vectors add a sixth, `handoff`.
19. Not covered by header vectors: `X-Request-Id`, `Accept`, `tracestate`, `If-None-Match`, `Content-Type`.
