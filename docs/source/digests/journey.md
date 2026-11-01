# Digest: the journey's capabilities (data surface, discovery, catalogue, handbook)

> Historical engineering study (2026-10-31). Retained for its requirements, counterexamples and design reasoning. Implementation observations and line references describe that review snapshot; use [current progress](../../HANDOFF-PROGRESS.md) and the named source symbols for current status. Paths are relative to the repository root unless a citation key says otherwise.

Reader: journey. Written 2026-10-31. Governing order: `docs/pass-2/REVIEW.md` > spec (§9 normative for the wire) > `PLAN.md`.

Abbreviations for citations:
- `DS` = `atlas-spec/02-2-data-surface.md`; `SF` = `atlas-spec/02-2-1-snowflake-data.md`; `DISC` = `atlas-spec/02-6-discovery.md`; `PLUG` = `atlas-spec/04-plugging-in.md`; `CAT` = `atlas-spec/07-the-catalogue.md`; `HB` = `atlas-spec/review/CAPABILITY-HANDBOOK.md`; `REV` = `docs/pass-2/REVIEW.md`; `AG` = `AGENTS.md`.
- `:N` is the line number in that file.
- Facts marked **[outside assignment, verified]** come from files other readers own (`02-1-capabilities.md`, `03-contracts.md`, `09-2`, `09-3`, `09-5`, `09-6`, `06-1`, `06-4`, `06-7`, `review/ATLAS-CORE.md`, `review/DESIGN-REVIEW.md`, `spec/protocol/references/**`). I read the cited lines; I did not read those files in full.

Markers: **IN SCOPE**, **DEFERRED**, **AMBIGUOUS:**, **OVERRIDDEN BY REVIEW:**, **PROPOSED FIXTURE DEFAULT** (my proposal for a fixture to fix; it is not in any source).

---

## 0. Scope frame (from REVIEW.md)

| Fact | Citation |
|---|---|
| The first build is one complete journey: holdings, its identifier lookup and its freshness, then a capability added after the clients are built | REV:11 |
| Journey capabilities: `portfolio.holdings@v1`; `portfolio.list@v1` and `portfolio.search@v1` (its identifier lookup); `data.status@v1` (its freshness) | REV:37-40 |
| Added after the clients are built: `portfolio.holdings-dates@v1` | REV:42 |
| "Their profiles are written to the handbook's page contract. Where an owner must decide something ... the profile says so and the fixture fixes a stated choice." | REV:42 |
| Step 2 done when: "The four journey profiles pass the gate and the readiness check. Fixtures cover two identities with different portfolio scope" | REV:30 |
| Clients: CLI, Python and Node, one request record in all three (R11) | REV:12 |
| One Go deployable, plus a worker where durable work needs one; provider boundary stays in process and serialisable (R20) | REV:13 |
| Conservative evolution: aliases, defaults, additive optional fields; unit or type change, or removal, is a new major (R03, R04) | REV:14 |
| Handoff, events, Service Bus: DEFERRED (R10, R16) | REV:15 |
| Building without Docker; DuckDB and sqlglot fake Snowflake; fakes behind real adapters; references are the independent check (D22) | REV:21 |
| Step 5 (out of reach from this machine): "the Development source, direct downloads, events, delegated identity" | REV:33 |
| Acceptance result, 6 items (the local shared kit and SQL library in the running path with integration tests and applicable notices; same person discovers and calls from CLI/Python/Node with identical request records; narrower scope -> narrower result or clear refusal; capability added after clients works without releasing any; invalid requests, permission changes, exact decimals, incomplete streams, idempotent retries, source failures behave the same in every client; the guide's example succeeds and its result can be checked independently against the source fixture) | REV:46-51 |
| Left to owners (fixtures state the choice): the holdings questions (null `as_of`, cash and instruments without an ISIN, aggregation, currency and signs, whether weights include cash, partial authorisation); org/tenant switching; minimum sunset and lock format; catalogue overlaps | REV:94-100 |

---

## 1. The data surface (§2.2)

### 1.1 Interactions (DS:63-68)

| Interaction | Effect | Result | Delivery | What it does | Journey |
|---|---|---|---|---|---|
| `fetch` | `read` | `table`, `record`, `collection` or `content` | `single`, `chunked`, `operation`, or a handoff | Returns the data for a request: streamed by the gateway, or handed off | **IN SCOPE** |
| `explain` | `read` | `record` | `single` | Takes the same request as `fetch`; returns the plan (for SQL, the exact statement), the columns that would come back, estimated rows, bytes and cost, and the execution it would choose (`explain.schema.json`, §9.3) | **IN SCOPE** (holdings, list, holdings-dates offer it per CAT:634,639,640) |
| `export` | `read` | `content` | `transfer` | Writes the whole result as Parquet or Arrow files, then short-lived download links | **DEFERRED**: DS:76 "After the first two"; REV:33 step 5 "direct downloads" out of reach. AMBIGUOUS: the catalogue lists `export` for holdings/list/holdings-dates (CAT:634,639,640). Readings: (a) profile omits `export` in v1 and adds it later (adding an interaction: check classification with the gate reader); (b) profile declares it and the gateway answers it with a fake Blob store. |
| `ticket` | `read` | `ticket` | `single` | The broker: plans the `fetch` and hands the caller a ticket | **DEFERRED** (O54, DS:77) |

`explain` schema (**[outside assignment, verified]** `references/schemas/explain.schema.json`): required `address`, `profile`, `plan`, `result_schema`, `execution`, `estimate`; optional `warnings` (each `{code, text}`); `plan.language` enum `sql`, `http`, `UNKNOWN`; `execution` enum `gateway`, `handoff`, `client`, `UNKNOWN`; `estimate` requires `rows`, `bytes`, `cost` (each nullable).

Requirements (DS:51-57), copy exact:
- **ATLAS-DATA-001.** A `fetch` with `columns` returns exactly those columns, in the writer profile's types, through every execution the capability offers. **IN SCOPE**
- **ATLAS-DATA-002.** In client execution, the statement the caller receives includes the requested columns and pushed-down filters; the caller runs it unchanged. **DEFERRED** (client execution)
- **ATLAS-DATA-003.** Settlement compares what the source recorded under the ticket's tag with the planned statement, and records a mismatch against the caller. **DEFERRED**
- **ATLAS-DATA-004.** `explain` returns the plan the same request's `fetch` runs, byte for byte for a SQL statement; a capability that cannot explain itself does not offer `explain`. **IN SCOPE**
- **ATLAS-DATA-005.** A ticket's credential can do nothing at the source that the caller's own identity could not. **DEFERRED**
- **ATLAS-DATA-006.** An export's files are readable only through its links, which expire, and only by the caller. **DEFERRED**
- **ATLAS-DATA-007.** `explain` is decided, validated and resolved exactly as the same request's `fetch` (the same problem for an invalid parameter), and runs nothing at the source except its estimate. **IN SCOPE**

Rules (DS:44-49):
1. A table capability SHOULD implement `Projectable`; every table capability whose source can filter SHOULD implement `Filterable`.
2. A planned statement carries a tag the source records (for Snowflake, `QUERY_TAG`); the gateway settles every handed-off, exported or brokered call from the source's own record. (Journey: tag IN SCOPE in the planned SQL is cheap; settlement of handoff/export DEFERRED with them.)
3. Ticket credential rules (DEFERRED).
4. Rows of a `Licensed` capability fetched by the client (handed off, or brokered) stay in memory on the caller's side.
5. A `fetch` whose answer may be large or slow SHOULD offer `chunked` delivery or return an operation.
6. Export links short-lived, bound to caller and operation; files deleted when the operation's retention ends (DEFERRED).

### 1.2 How a result travels (DS:72-79)

| Execution | Runs the work | Moves the bytes | Credential given out | Status (spec) | Journey |
|---|---|---|---|---|---|
| `gateway` (streamed) | Gateway | Gateway, streamed, never buffered (Arrow or NDJSON) | None | **Build first** | **IN SCOPE** |
| `handoff` | Gateway | Client, directly from source's result storage | Links to this one result only | **Build first** (Snowflake, Python) | **DEFERRED** per REV:15 ("Handoff ... Deferred, as separately gated proofs on real systems"). AMBIGUOUS: REV:15 "Handoff" may mean the §6.3 handover (R10) rather than data handoff; REV:33 defers "direct downloads". Pass 1 has fake-backed "handoff renewal" tests (DESIGN-REVIEW:182, **[outside assignment, verified]**). Reading A: holdings declares `execution: ["gateway"]` only in v1. Reading B: declares `["gateway","handoff"]` and the fake serves handoff. Recommend A for the first build: adding `handoff` later is a binding/execution addition; confirm classification with the gate reader. |
| `export` | Gateway (or source writing files) | Storage, presigned links | Links to files | After the first two | **DEFERRED** |
| `client` (broker) | Client | Client | A ticket | **Deferred** (O54) | **DEFERRED** |

- "the gateway chooses one per call from the client's levels, the expected size of the result, the caller's preference and policy. Whichever is chosen, the result is the same rows in the same writer schema" (DS:79).
- **OVERRIDDEN BY REVIEW (R11):** "Core is JSON, and Arrow is an optimisation of the same meaning" (REV:69). JSON must be a complete path for every journey capability.

### 1.3 The contract in the planned query; the sanctioned computation

- "Every column is aliased to its contract name and cast to its contract type, so the source itself produces the writer schema. A client still checks each batch's schema against the profile (one comparison per batch) and reports `invalid_document` on a mismatch" (DS:83). **IN SCOPE**
- "the caller supplies parameter values and its column selection; it never authors or edits the computation. Column selection and the filters the profile declares are pushed down **when the gateway plans**" (DS:87). Renaming, display derivations and further filtering happen on returned rows and never touch the statement (DS:87).
- Free-form SQL is its own capability (`snowflake.query`), named grant, `stability: upstream` (DS:89). Not in journey.

### 1.4 Table results, delivery `single` and `chunked`

| Fact | Citation |
|---|---|
| `table` = "Rows of typed columns"; generic behaviour "Column selection, filters, ordering, columnar transport (Arrow)" | 02-1:122 **[outside assignment, verified]** |
| `single`: one answer. `chunked`: one answer, sent as produced (Arrow record batches, NDJSON lines), ended by a final frame that carries the usage and any problem | PLUG:171-173 |
| Errors never in the data: a binding that cannot use status codes carries a problem frame | PLUG:110 |
| The gateway never buffers a whole chunked answer | PLUG:111 |
| HTTP `single` success `200`: "a `table` as a JSON array of row records"; `chunked` `200`: NDJSON or Arrow IPC stream | 09-6 §9.6.5 **[outside assignment, verified]** |
| NDJSON: one record per line, `LF`, no blank lines; last line is the end line `{ "$atlas": "end", "rows", "truncated", "usage", "problem" }`; a body without an end line is `upstream_failed` | 09-6 §9.6.5 **[outside assignment, verified]** |
| Arrow: IPC streaming format; schema message with `atlas:profile` and `atlas:protocol` metadata; record batches; final zero-row batch whose custom metadata `atlas:end` holds the end line JSON; then EOS. Without that final batch it is incomplete | 09-6 §9.6.5 **[outside assignment, verified]** |
| **AP-HTTP-002.** Every client treats a truncated NDJSON or Arrow answer as `upstream_failed`. | 09-6 **[outside assignment, verified]** |
| Handbook: "Chunked table: Consume batches. Do not equate received rows with a successfully completed answer until the completion marker is checked." | HB:138 |
| Media types: `application/json`, `application/vnd.apache.arrow.stream`, `application/x-ndjson`, `text/csv` (CSV display only, never read by a client) | PLUG:287; 09-2 §9.2.6 **[outside assignment, verified]** |

Catalogue delivery for the journey: holdings `table · chunked`; list, search, data.status, holdings-dates `table · single` (CAT:634-640, CAT:361).

### 1.5 Traits: AsOf, Projectable, Filterable (and the others the journey touches)

From 02-1 §2.1.E **[outside assignment, verified]** (02-1:153-164):

| Trait | A capability with it |
|---|---|
| `Projectable` | Accepts `columns` (or field paths) and returns only those |
| `Filterable` | Accepts the filters its profile declares, pushed down to its source |
| `Paged` | Pages by an opaque cursor |
| `AsOf` | Accepts `as_of`, answering as the data stood then; and `start_date` / `end_date` for a range |
| `Keyed` | Declares its keys and its references to other capabilities |
| `Searchable` | Accepts free-text search |
| `Handoff` | Offers `handoff` execution |
| `Licensed` | Returns licensed data: never persisted except a job's answer for its own caller; caches stay in memory |

- Wire form: "a trait is the object `{ "name": "Projectable", "critical": false }`; `critical: true` means a client that does not implement it must not call the capability (`critical_trait_unsupported`)" (09-3 §9.3.2 **[outside assignment, verified]**). `critical_trait_unsupported` is 422, not retryable (09-3 §9.3.3).
- Availability requires "the caller's client supports every trait it marks critical" (DISC:40).
- AMBIGUOUS: `AsOf` says "answering as the data stood then", while holdings' meaning is "the portfolio's last valuation on or before as_of" (§3.1.3). These differ: bitemporal "as the data stood" (knowledge time) vs valuation-date selection. Readings: (a) `as_of` is a valuation date only (the holdings profile's reading); (b) `as_of` is also a knowledge-time cut (no later corrections). Recommend (a) for v1 and say in the guide that corrections are not point-in-time (see §7, decision H10).

### 1.6 Row limits and truncation

| Fact | Citation |
|---|---|
| Governance output limit example: `{"interaction": "fetch", "kind": "output", "max_rows": 1000000, "max_bytes": 1073741824}` | 03-contracts §3.1.3 governance example **[outside assignment, verified]** |
| `atlas.describe` eligible binding example: `{ "binding": "https", "encoding": "json", "limits": { "max_rows": 100000 } }` | DISC:162 |
| `output_limit_exceeded`: status `200 (in the end line or final frame)`, not retryable, "The answer was truncated at the caller's cap" | 09-3 §9.3.3 **[outside assignment, verified]** |
| End line `truncated`: "Whether the answer was cut at the caller's output cap"; Q-DOC-15: truncated end line has `truncated: true` and problem `output_limit_exceeded`, which a client reports as a warning; any other problem in an end line is raised after the rows | `references/schemas/chunk-end.schema.json`; `references/QUESTIONS.md:70` **[outside assignment, verified]** |
| Bulk fallback: "`fetch` in pages with a row cap and a truncation flag" | 02-1:145 **[outside assignment, verified]** |
| Page contract requires "output caps, paging and truncation behavior" | HB:119 |
| Client warnings (deprecations, truncation) returned in `warnings`, each `{ "code", "text" }` | 09-8:53 **[outside assignment, verified]** |
| `conformance.rows@v1` tests framing, end lines, final batches, truncation, `columns`, encodings returning identical records | HB:574; 09-8:66 |

- AMBIGUOUS: truncation for `single` delivery (a plain JSON array, no end line). Readings: (a) `single` answers are never truncated; an answer above the cap is a problem; (b) truncation signalled by a warning header or member. No source fixes it. Consequence for the journey: list, search, data.status and holdings-dates are `single`. **PROPOSED FIXTURE DEFAULT:** `single` journey answers stay below their caps by construction (small fixtures); holdings (chunked) carries the truncation test.

### 1.7 Ordering

- Request part "Filters, order, cursor: Where the profile declares them" (PLUG:129).
- "Everything that can be is in the SQL: columns, filters, limits, order." (SF:131)
- Page contract requires "ordering" under What to expect (HB:116); ATLAS-CORE §4 "Selection: Filtering, ordering, projection and paging semantics where offered" **[outside assignment, verified]**.
- Holdings: "ordering and row limits" is an owner decision (HB:168). `portfolio.trades` is "newest first" (CAT:646), so ordering is stated per capability.
- No source defines a profile member that declares the ordering. AMBIGUOUS: where it lives. Readings: (a) in prose (`meaning` / description); (b) a new `meaning.order` member. `profile.schema.json` does not show one (its properties: `identity`, `properties`, `interactions`, `schemas`, `errors`, `meaning`, `compatibility`, `usage`, `evolution`, `fingerprint`, `revision`) **[outside assignment, verified]**. Recommend (a): state it in `meaning` prose and in the result schema's `description`.

### 1.8 Nulls and empty answers

| Fact | Citation |
|---|---|
| Arrow field nullable exactly when its property allows `null` or is not in `required` | 09-2 §9.2.7 **[outside assignment, verified]** |
| `x-atlas-key`: every key property is required and not nullable, "except where `x-atlas-logical` is an identifier that may be missing (then the key is the set of non-null key values)" | 09-2 §9.2.3 **[outside assignment, verified]** |
| `null` passes through every lens value operation unchanged | 09-5:52 **[outside assignment, verified]** |
| Clients preserve exact decimals, large integers, timestamps and nulls | ATLAS-CORE §6 **[outside assignment, verified]** |
| Page contract: "null/empty behavior" | HB:116 |
| "'portfolio holdings' is incomplete until the profile says ... whether an empty answer means no positions, unavailable data or an unsupported portfolio" | ATLAS-CORE §4 **[outside assignment, verified]** |
| Handbook: "'Latest' must be reported as an effective source date, not mistaken for the current wall-clock time." | HB:144 |

---

## 2. Snowflake data (§2.2.1)

### 2.1 What Snowflake gives (SF:61-72)

| Snowflake does | Gives Atlas |
|---|---|
| Returns results as **Arrow** chunks, first inline, rest behind pre-signed URLs | Arrow end to end; parallel download; handoff without a session |
| `gosnowflake` Arrow batches (`WithArrowBatches`); Python `get_result_batches()` | Basis of the handoff |
| Streams batches with exact decimals (`WithHigherPrecision`) | Bounded gateway memory |
| Keeps every result 24 hours (`WithFetchResultByID`, `RESULT_SCAN`) | Resume a broken stream / re-issue links at no compute cost |
| Async by query id (`WithAsyncMode`) | No request held open |
| `EXPLAIN` (partitions and bytes) | Choosing how the result travels |
| `COPY INTO <stage> FILE_FORMAT = (TYPE = PARQUET)` | `export` |
| `QUERY_TAG`, query history | Exact settlement |

### 2.2 The four executions (SF:99-108)

| | A. Streamed | B. Handoff | D. Export | C. Broker |
|---|---|---|---|---|
| Runs the SQL | Gateway | Gateway | Gateway (`COPY INTO`) | Client |
| SQL leaves the gateway | No | No | No | Yes |
| Statement guard applies | Yes | Yes | Yes | No |
| Settlement | Exact | Exact | Exact | By `QUERY_TAG` |
| Status | Build first | Build first | Next | Deferred (O54) |
| Journey | **IN SCOPE** (DuckDB fake) | **DEFERRED** (see 1.2) | **DEFERRED** | **DEFERRED** |

Rules (SF:50-52): offers `gateway` always; `handoff` where the spike confirms; `export` where results can be large; `client` deferred. "The contract is in the planned SQL; no execution passes Snowflake's own types to a client." Handoff document: short-lived, bound to caller and operation, never stored.

Requirements (SF:54-57):
- **ATLAS-SNOWFLAKE-001.** The same request through every execution a capability offers yields identical records. **IN SCOPE** (for the executions offered)
- **ATLAS-SNOWFLAKE-002.** Gateway memory stays flat while streaming a result larger than its memory limit. **IN SCOPE**
- **ATLAS-SNOWFLAKE-003.** A handoff gives the client nothing that reads anything but its one result. **DEFERRED**
- **ATLAS-SNOWFLAKE-004.** No decimal reaches a client as a float unless its profile declares a `number`. **IN SCOPE** (acceptance item 5, "exact decimals")

### 2.3 The stream, made fast (SF:126-131)

1. `gosnowflake` with `WithAsyncMode`, `WithArrowBatches`, `WithHigherPrecision`: Arrow batches, never rows.
2. Few chunk-prefetch threads; client's reading is the backpressure.
3. Each batch cast to the writer Arrow schema: renames; `NUMBER(p,0)` to `int64`; timestamps to UTC microseconds.
4. Arrow IPC with ZSTD buffer compression, or NDJSON for Core clients.
5. A broken stream resumes from batch *k* using the query id and the persisted result.
6. Columns, filters, limits, order in the SQL. Only requested columns move.

### 2.4 Contract in the SQL (SF:135)

"every column aliased to its contract name and cast to its contract type, every timestamp converted to UTC. So in every execution Snowflake itself produces the writer schema; the gateway's own cast (A) and the client's check (B, D) only confirm it." Rendered by "the Go SQL library (§6.4)". **IN SCOPE**: sqlglot renders for Snowflake and for DuckDB (the fake) (REV:21, REV:32; 06-4:11 **[outside assignment, verified]**).

### 2.5 Types (SF:139-150), copy exact

| Snowflake | Atlas (§9.2) | Note |
|---|---|---|
| `NUMBER(p,0)`, `p ≤ 18` | `integer` (`Int64`) | |
| `NUMBER(p,0)`, `p > 18` | `Decimal(p,0)` | |
| `NUMBER(p,s)`, `s > 0` | `Decimal(p,s)` (`Decimal128`), or `number` where the profile declares a unit (`Bp`, `Fraction`) | Exact decimals must be requested from the driver |
| `FLOAT` | `number` (`Float64`) | |
| `VARCHAR` | `string` | |
| `BOOLEAN`, `DATE`, `BINARY` | `boolean`, `date`, `bytes` | |
| `TIMESTAMP_TZ`, `TIMESTAMP_LTZ` | `date-time` (UTC, microseconds) | |
| `TIMESTAMP_NTZ` | `date-time` only where the profile states the zone it is in; converted to UTC in the SQL | Never assumed |
| `TIME` | `string` in `atlas/1.0` | |
| `VARIANT`, `OBJECT`, `ARRAY` | A declared `object` or `array` schema, or a JSON `string` | Never untyped in an official capability |

Related Arrow mapping (09-2 §9.2.7 **[outside assignment, verified]**): `integer`->`Int64`; `number`->`Float64`; `format: date`->`Date32`; `format: date-time`->`Timestamp(MICROSECOND, "UTC")`; `Decimal(p,s)`->`Decimal128(p, s)`; `Money`->`Struct<amount: Decimal128(38,6), currency: Utf8>`. `Decimal(p,s)` in JSON is `string`, `format: decimal` (09-2 §9.2.3).

AMBIGUOUS: `bytes` is named in SF:146 but §9.2's Arrow mapping table lists no `bytes` row. Not needed by the journey.

### 2.6 Choosing the execution (SF:154)

From client levels and storage reachability (`Atlas-Client-Features`), expected size (`EXPLAIN` or declared typical size), caller preference (`Atlas-Execution`), policy. Starting thresholds "to be set by measurement": "under about 50 MB, streamed; above it, a handoff where the client can take one, else streamed; files asked for, or above about 5 GB, export." Journey: always streamed (handoff/export deferred). **OVERRIDDEN BY REVIEW (R19):** "Heuristics are labelled, and output is measured" (REV:78).

### 2.7 The read-only guard (pointer)

Not in SF. **[outside assignment, verified]** 06-4:84: "Every statement is parsed by the library's Snowflake parser before it runs, and refused unless it is one read-only query over the objects its capability may read ... The guard is never the only line: the connection's role can only read." Vectors: `references/vectors/guard.json` (65 cases; refusal `reason` order: dml, ddl, copy, file_transfer, call, not_a_query, select_into, stage, table_function, system_function, dynamic_identifier, unqualified_object, unregistered_object). Guard applies to A, B, D (SF:105). **IN SCOPE**.

### 2.8 Views and freshness

- Views: SF names none. Holdings' source is "Harbour's portfolio holdings view" (§3.1.3 `meaning.source`). AMBIGUOUS/MISSING: no source names the Snowflake objects behind `portfolio.list`, `portfolio.search`, `data.status` or `portfolio.holdings-dates`. The fixture must name its own (fake) objects.
- Freshness: SF covers only result persistence ("the result persists for 24 hours", SF:39; "Keeps every query's result for 24 hours", SF:68). Data freshness is `data.status@v1` (§4.4 below). MISSING: no source says how a capability is linked to the source id `data.status` reports.

### 2.9 Spike questions (SF:158-166): all open; they gate handoff, not the journey's streamed path. "Do exact decimals and `TIMESTAMP_NTZ` behave as above on our connector version? If no: Cast in the SQL" (SF:164): with the DuckDB fake, always cast in the SQL.

---

## 3. Discovery (§2.6)

### 3.1 Rules and requirements

Rules (DISC:39-44), exact:
1. **The working view shows only what is usable.** Appears in `atlas.catalog`, `atlas.search`, CLI lists and completion, MCP tools, SDK attributes only when available to the caller, "and only with the interactions they may use."
2. **Availability.** All hold: the caller holds at least one of its interactions and no deny refuses it; it exists in the environment; lifecycle not `retired` (nor `preview`, unless the caller asked for previews); the client supports every trait marked critical.
3. **Only `atlas.access` shows what is not usable,** and only when called.
4. **Calling by name is explicit.** Refused "with the problem for its status (`not_granted`, `denied`, `not_in_environment`, `limit_exceeded`, `retired`), and a `next` step that names `atlas.access`." Confidential not held: `not_found`, identical to non-existent.
5. **House rules travel with the map.** `atlas.whoami` returns every house rule that applies; every catalogue and search page the rules naming a capability on it.
6. **Nothing about anyone else.**

Requirements (DISC:46-54), exact:
- **ATLAS-DISCOVERY-001.** `/.well-known/atlas` is served by the gateway in every environment and needs no sign-in. **IN SCOPE**
- **ATLAS-DISCOVERY-002.** The working view returns exactly the capabilities available to the caller; a conformance test varies each availability condition alone and checks the capability disappears. **IN SCOPE**
- **ATLAS-DISCOVERY-003.** A denied capability appears in no working-view response (catalogue, search, describe, completion, MCP tools), and appears in `atlas.access` with status `denied`. **IN SCOPE** (the `atlas.access` half: see AMBIGUOUS in 3.6)
- **ATLAS-DISCOVERY-004.** `atlas.access` returns, for every listed capability, one status from the table, its reason, how to obtain it, and the caller's limits and use where they hold it.
- **ATLAS-DISCOVERY-005.** A confidential capability answers `not_found` to every caller who does not hold it, in both views and on a direct call.
- **ATLAS-DISCOVERY-007.** A compatible profile change reaches a long-lived client within 60 seconds through the change feed, and within 15 minutes without it. (Change feed DEFERRED with events; the 15-minute poll is IN SCOPE.)
- **ATLAS-DISCOVERY-008.** `atlas.describe` lists, for the calling client, every binding it cannot use with what is missing. **IN SCOPE**
- **ATLAS-DISCOVERY-009.** `atlas.guide` answers, for every capability available to the caller, every section of the guide in order, within `max_tokens`, its examples in the calling client's syntax and each valid against the request schema. **IN SCOPE**
- **ATLAS-DISCOVERY-010.** No guide names a capability the caller cannot use. **IN SCOPE**

(There is no ATLAS-DISCOVERY-006 in DISC.)

### 3.2 The built-ins (DISC:93-105; shapes from 09-3 §9.3.2 **[outside assignment, verified]**)

| Capability | View | Request (09-3) | Result (09-3) | Journey |
|---|---|---|---|---|
| `atlas.whoami` | — | `{}` | `{ principal, name, kind ("person", "workload"), roles[], environment, available_domains[], house_rules }` | **IN SCOPE** |
| `atlas.catalog` | working | `{ domain?, family?, effect?, previews?: boolean, cursor?, page_size? (1–500, default 100) }` | Catalogue page | **IN SCOPE** |
| `atlas.search` | working | `{ words: string (1–200), cursor?, page_size? }` | Catalogue page, ranked (`score`, `why`) | **IN SCOPE** |
| `atlas.describe` | working | `{ by: "id", id }` or `{ by: "exact", exact }` | Describe answer | **IN SCOPE** |
| `atlas.guide` | working | `{ by: "id", id }` or `{ by: "exact", exact }`, with `client?` (`cli`, `python`, `node`, `go`, `mcp`) and `max_tokens?` (200–20000, default 4000) | `{ exact, client, markdown, tokens, truncated, revisions }` | **IN SCOPE** |
| `atlas.access` | access | `{ id?, domain?, cursor? }` | Page of `{ id, status, reason, next, held_by?, limits?, interactions? }` | AMBIGUOUS (3.6) |
| `atlas.usage`, `atlas.setup`, `atlas.clients`, `atlas.compare` | — / working | see 09-3 | | Not named by the journey; DEFERRED unless another digest says otherwise |
| `atlas.operation` | — | `{ name }`; `{ cursor?, state? }` | Operation; page | IN SCOPE only where durable work needs it (REV:13) |

- Catalogue page (`catalog-page.schema.json` **[outside assignment, verified]**): required `items`, `next_cursor` (string|null, null on the last page), `house_rules` (`{revision, rules: [{text, capabilities[]}]}`). Item required: `id`, `version` (integer ≥1), `exact` (exact-id), `summary`, `family`, `interactions` (map of interaction -> `{effect}`); optional `title`, `lifecycle`, `score` (number, "how well it matched"), `why` (string, "why it matched").
- `atlas.search` `page_size`: Q-BI-1 reading "atlas.catalog's, 1 to 500 with default 100" (`references/QUESTIONS.md:75`).
- Working view per item (DISC:67): "Its summary, the interactions the caller may use, their effect classes, its fingerprint".
- `atlas.whoami` (DISC:95; CAT:171): "Who the caller is to the gateway: identity, roles, environment, and a summary of what is available by domain". Today `/v1/me`; `atlas whoami`.

### 3.3 Search: ranking and reasons

- "Available capabilities ranked against words ('credit curves for US IG')" (DISC:97). Result: "As `atlas.catalog`, ranked, with why each matched" (CAT:174). Handbook: "ranked matches with reasons ... task words" (HB:213).
- MISSING: no ranking algorithm, score range or tie-break in any assigned source. ATLAS-CORE:13 **[outside assignment, verified]**: "Atlas does not need an LLM in its execution path. Discovery can rank descriptions and synonyms; domain owners curate authoritative routes."
- **PROPOSED FIXTURE DEFAULT:** deterministic lexical ranking over `identity.title`, `identity.summary`, `usage.when`, the domain, and owner-curated synonyms; `score` in [0,1]; ties broken by `id` ascending; `why` names the matched words and fields (e.g. `"matched 'holdings' in title; 'weights' in a result field"`). Determinism matters: ATLAS-VISION-003 / R22 (REV:80) test answers, and identical request records across clients (REV:47).
- Acceptance check from HB:73: `atlas search "holdings and weights for a portfolio"` must return `portfolio.holdings@v1` first for a caller who holds it.

### 3.4 Describe

- Request takes no parameters of the call; returns "the profile, the caller's governance, its availability, the calling client's eligible bindings. For programs; cached by fingerprint" (DISC:132).
- `describe.schema.json` **[outside assignment, verified]**: required `profile`, `governance`, `availability` (enum `available`, `unavailable`, `UNKNOWN`), `eligibility` (array, per interaction, from `Atlas-Client-Features`).
- Eligibility example shape (DISC:159-171): `{ "interaction": "fetch", "eligible": [ { "binding", "encoding", "limits": { "max_rows" } } ], "ineligible": [ { "binding", "missing": [...], "hint" } ] }`. "This is about the *client*, never about access" (DISC:174).
- **OVERRIDDEN BY REVIEW (R01):** "The contract (by fingerprint) is cached apart from the document revision. Describe's caller state (governance, availability) is private and uncached." (REV:59). DISC:132 "cached by fingerprint" and 09-6 §9.6.7 `Cache-Control: max-age=31536000, immutable` no longer apply to the caller-specific describe answer.

### 3.5 The guide: sections and identity

Sections, "always in this order" (DISC:142-150), exact:
1. **What it is:** the summary, the family, its stability and lifecycle.
2. **When to use it, and when not,** with the house rules that name it.
3. **What you may do:** each interaction the caller holds, with its effect class and what that means for asking the person first.
4. **Parameters:** each with its type, whether required, its default, its meaning and unit, its allowed values or the capability that lists them.
5. **What comes back:** for a table its grain and keys, then each column with its type, unit and meaning; for a record, its fields.
6. **Examples:** each of the profile's examples, written in the calling client's syntax (§9.7.1).
7. **Problems:** each problem it can return, with its next step.
8. **Your limits and its cost:** what is left for the caller, and the cost of a call where it is metered.
9. **Related:** the capabilities it refers to and those that refer to it.

- "It never names a capability the caller cannot use. It fits `max_tokens` by shortening sections from the last, saying so, and never drops sections 1 to 4." (DISC:152)
- Written "from the profile, the caller's governance and the managed context (`atlas.context`: the capability's skill text and the house rules)" (DISC:140).
- Identity: "The same profile, governance revision, context revision and client give the same guide." (DISC:152). **OVERRIDDEN BY REVIEW (R01):** "A guide's identity includes the prose revision" (REV:59; HB:547). `guide_result.revisions`: "the profile's prose, governance and context"; members not fixed (Q-BI-2, `references/QUESTIONS.md:76`). CORE-GUIDE-001 uses `{"governance": "g-1", "context": "c-1"}` and the client returns the Markdown unchanged.
- AMBIGUOUS: `max_tokens` counts tokens of which tokenizer. No source fixes it. Recommend a documented, deterministic counter in the gateway (e.g. a word/byte-based approximation) and report it in `tokens`.
- Guide's section 5 requires "ordering" and null/empty behaviour? DISC:146 does not; HB:116 does. Treat HB's page contract as additional content the guide carries (section 5 prose).

### 3.6 Access view

Statuses (DISC:75-83): `available`, `partial`, `not_granted`, `denied`, `not_in_environment`, `limited`, `deprecated`.

AMBIGUOUS: is `atlas.access` in the first build? For: discovery rule 4 makes every refusal's `next` name `atlas.access` (DISC:42); HB:57 "`atlas access` is the explicit route"; acceptance item 3 asks for "a clear refusal" (REV:48). Against: ATLAS-CORE §12 lists "bootstrap, identity, authorized catalogue/search, describe/guide, the call path, problems, data streaming and durable operations" **[outside assignment, verified]**; REV does not name it. Recommend IN SCOPE in its minimal form (one status per journey capability for the two identities), since every refusal points at it.

### 3.7 How relevance stays inside permission

| Rule | Citation |
|---|---|
| "'available to this caller' is the permission boundary; 'recommended for this task' is a ranking within that boundary. A role can inform recommendations, but it does not identify a person's current portfolio or silently narrow the result. If Atlas applies a default portfolio, date, currency, source or scope, the guide describes it and the answer reports the effective value. An explicit request overrides a default only within authorization." | HB:35 |
| **R05:** "Resource and returned-data scope is enforced from the first shared-credential source, by small typed rules. Relevance never grants access." | REV:63 |
| "Relevance is a ranking or default, never permission. Having a portfolio-related role does not imply permission to every portfolio. A capability grant does not itself establish row-level entitlement." | ATLAS-CORE:42 **[outside assignment, verified]** |
| "Where the gateway calls as a shared service identity, the profile and guide need to say how Atlas enforces the caller's permitted scope; recording a caller in an audit does not itself provide row-level restriction." | HB:45 |
| Three facts kept separate: holds an interaction / source is available / client supports a binding; each "does not prove" the others | HB:39-43 |
| "Guides and recommendations must not disclose capabilities that caller cannot use." | HB:57 |
| House rule reaches a caller only when the caller can use every capability it names | DISC:109; `catalog-page.schema.json` |
| "Discovery is an authorized view at a point in time ... Execution always rechecks the relevant facts." | ATLAS-CORE:30 **[outside assignment, verified]** |
| The served guide contains "only granted interactions, permitted references, effective context and current limits"; the static review inventory "must never be mistaken for a caller's authorization-filtered working view" | HB:593 |

Consequence for the journey: `portfolio.search` and `portfolio.list` return only portfolios inside the caller's scope; holdings refuses or narrows a portfolio outside scope (decision H8). A guide's "Related" section and `usage.related` must be filtered per caller (DISCOVERY-010).

### 3.8 Freshness of the catalogue (DISC:178)

ETag on catalogue and every profile; revalidate with `If-None-Match` -> `304`. Change feed `atlas.catalog.changed` on Service Bus: **DEFERRED** (events). Others "poll every 15 minutes". "A client otherwise caches the catalogue for at most an hour and revalidates at the start of every session." **IN SCOPE** (poll and revalidation).

### 3.9 Agent journey (DISC:115-122; HB:51-55)

Orient (`atlas.whoami`) -> Find (`atlas.search` / `atlas.catalog`) -> Understand (`atlas.guide`; `atlas.describe` for programs; optional `explain`) -> Act -> Recover (`next`) -> Ask about access only when needed. HB:55 adds step 5: "Inspect completion, effective dates, warnings and any partial/truncated indicator before interpreting the result."

---

## 4. The journey's capabilities

Common to all four data capabilities (CAT:49-63, CAT:652; HB:198): surface `data`; every interaction `read`; stability `official`; domain owner "QFI (to name)" (CAT:630, CAT:356). "Each capability's full contract is its profile" (CAT:63). Profile parts (03-contracts §3.1.2 **[outside assignment, verified]**): `identity`, `properties`, `interactions`, `schemas`, `errors`, `meaning`, `compatibility`, `usage`, `evolution`, `fingerprint`, `revision`. `profile.schema.json` requires `identity`, `properties`, `interactions`, `schemas`, `errors`, `meaning`, `fingerprint` **[outside assignment, verified]**.

Readiness check (§3.1.4a **[outside assignment, verified]**), every journey profile must pass:
1. `identity.summary` one sentence, at most 200 characters.
2. Every property at every depth has a `description`; every `number`/`integer` has `x-atlas-logical` or `x-atlas-unit` (`count` for plain counts).
3. `usage.when` set; `usage.examples` has at least one example whose `request` validates against the request schema of the interaction it names.
4. Every member of `errors` has `description` and `next`.
5. Data `meaning` has `grain`, `keys` (table), `as_of` (with `AsOf`), `source`.
Lint (§3.3.12 1a): request schemas closed; every enum with `UNKNOWN` (not accepted in requests, 09-2 §9.2.3); every reference resolvable; errors declared; every interaction with a deadline maximum and at least one binding with an HTTP fallback.

**OVERRIDDEN BY REVIEW (R03):** §3.1.3's `"compatibility": {"request": "BACKWARD_TRANSITIVE", "result": "FULL_TRANSITIVE"}` and lens evolution are replaced by: "Within a version: optional request fields; result fields only with a defined missing value; aliases for exact renames. A removal, a unit change, a type change, a field made non-null in a result, or a changed default is a new major version." (REV:61). `compatibility` is optional in `profile.schema.json`. AMBIGUOUS: whether journey profiles carry `compatibility` at all; the gate reader decides.
**OVERRIDDEN BY REVIEW (R04):** "unknown request fields are refused, never dropped" (REV:62), consistent with `additionalProperties: false`.

### 4.1 `portfolio.holdings@v1` — IN SCOPE

What each source says:

| Aspect | Source text (exact) | Citation |
|---|---|---|
| Description | "Positions in portfolios on a date; `history` for every end-of-day snapshot in a range.†" | CAT:639 |
| Returns | "Grain portfolio × security × date: market value, weight, analytics" | CAT:639 |
| Interactions | `fetch · explain · export` | CAT:639 |
| Result · delivery | `table · chunked` | CAT:639 |
| Execution | `gateway · handoff` | CAT:639 |
| Stability | `official` | CAT:639 |
| Today | `portfolio.holdings`, `portfolio.holdings-history` | CAT:639 |
| † | "whether each value returns the same result schema is to be confirmed (§7)" | CAT:652 |
| House rule `positions` | "What named portfolios hold: `portfolio.holdings`. Who holds one security: `security.holders`. Positions matching a security filter across portfolios: `portfolio.exposure`. (Proposed merge: `security.holders` into `portfolio.exposure`, filtered by one security.)" | CAT:85 |
| House rule `portfolio-codes` | "Turn loose words into portfolio codes with `portfolio.search`; list portfolios with `portfolio.list`." | CAT:87 |
| Handbook row | "Read positions → portfolio × security × date, value, weight, analytics" / "Portfolios and date; merged history/range semantics; chunked" | HB:242 |
| Promise | "`portfolio.holdings@v1` — positions in one or more portfolios on a valuation date" | HB:152 |
| Use it when | "You need what named portfolios hold, including weights, market values or holding analytics" | HB:153 |
| Use another when | "bucketed benchmark comparison, historical executed trades or positions selected by a security filter across portfolios" | HB:154 |
| Interactions | "`fetch`, `explain`, `export`, each a read; grant and limits are per interaction" | HB:156 |
| Inputs in sample | "Required `portfolios`: array of portfolio codes; optional `as_of`: date/null; optional `asset_class`: `credit`, `rates` or null; optional `columns`: string array/null" | HB:157 |
| Identifier lookup | "Resolve loose words with `portfolio.search`; use `portfolio.list` to obtain recognized codes, if granted" | HB:158 |
| Time | "The portfolio's last valuation on or before `as_of`; the meaning of null and multi-portfolio date alignment still need explicit prose" | HB:159 |
| Grain and keys | "Portfolio × security × date; sample keys are `portfolio`, `isin`, `as_of`" | HB:160 |
| Fields | "`portfolio`, nullable `isin`, `as_of`, `market_value` as Money, `weight` as Fraction, nullable `oas` as basis points" | HB:161 |
| Delivery | "`fetch` may stream through the gateway or use handoff; transport choice must preserve the same business answer. Export is a separate bulk result" | HB:162 |
| Changes and cost | "Reads do not change holdings. Warehouse costs, quotas and licensing may still apply" | HB:163 |
| Problems | "`portfolio_not_found` and `warehouse_suspended`; the abbreviated error definitions still need complete descriptions and next steps" | HB:164 |
| Weight | "A reader interprets `weight` as a fraction, not a percentage number." | HB:166 |
| Stale snapshot | "If the returned valuation date precedes the requested date, that follows the stated 'on or before' rule; the page must say how the caller detects and rejects an excessively old snapshot." | HB:166 |
| Owner list | see §7 H1-H12 | HB:168 |
| Key flaw | "a nullable `isin` cannot by itself distinguish every security within one portfolio/date" | HB:168; DESIGN-REVIEW:161 **[outside assignment, verified]** |
| Card example | ATLAS-CORE §4: row = security, tax lot or position?; `weight` a fraction?; which valuation date when omitted?; empty answer meaning? | ATLAS-CORE:80 **[outside assignment, verified]** |
| Shared request | `{"portfolios": ["147HYC"], "as_of": "2026-10-28", "columns": ["portfolio", "isin", "as_of", "weight"]}` | HB:63-69 |
| CLI | `atlas call portfolio.holdings@v1:fetch --request '{"portfolios":["147HYC"],"as_of":"2026-10-28","columns":["portfolio","isin","as_of","weight"]}'` | HB:75 |
| Python | `from fiatlas import FIAtlas`; `atlas = FIAtlas()`; `result = atlas.call("portfolio.holdings@v1:fetch", request)` | HB:79-87 |
| Node | `import { atlas } from "@atlas/sdk";` `const client = await atlas.connect();` `const result = await client.call("portfolio.holdings@v1:fetch", request);` | HB:91-99 |
| Options vs request | "`columns`, business filters and dates belong in the request. Deadlines, idempotency keys, transport preferences and local output destinations are call/client options." | HB:102 |
| Address | `/v1/data/portfolio.holdings@v1:fetch` (informative) | PLUG:283 |

Profile facts from §3.1.3 and the reference vector (`references/vectors/profiles/data.json`, case `portfolio.holdings@v1#8e61efdf45779059`) **[outside assignment, verified]**:
- traits `Projectable`, `Filterable`, `AsOf`, `Keyed`, `Handoff`, `Licensed`, each `critical: false`.
- `fetch`: `effect: read`, `result: table`, `delivery: chunked`, `execution: ["gateway","handoff"]`, `takes: request`, `returns: result`, `deadline {default: PT2M, max: PT30M}`, binding `https`/`gateway`/`POST`/`/v1/data/portfolio.holdings@v1:fetch`, encodings `arrow, json, ndjson, csv`, auth `bearer`.
- `explain` (vector): `deadline {default: PT30S, max: PT2M}`, encodings `json`. `export` (vector): `deadline {default: PT10M, max: PT2H}`, `encodings: ["parquet","arrow"]`.
- request: `portfolios` (array, `minItems: 1`, `maxItems: 500`, `uniqueItems: true`, items `PortfolioCode`, `x-atlas-reference: portfolio.list@v1`), `as_of` (string|null, `format: date`, `default: null`, "The valuation date; the latest when null."), `asset_class` (`credit`, `rates`, null; default null), `columns` (array|null, `uniqueItems`, default null). `additionalProperties: false`, `required: ["portfolios"]`.
- result row required (vector): `portfolio`, `as_of`, `title`, `market_value`, `weight`, `status`. Fields: `portfolio` (key), `isin` (nullable, key, "null for cash and instruments without one"), `as_of` (date, key), `title` (string|null), `market_value` (Money, alias `mv`, "in its currency"), `weight` (Fraction, "Its share of the portfolio's market value."), `oas` (Bp, nullable, alias `oas_bp`), `status` (`open`, `closed` (alias `sold`), `UNKNOWN`), `duration` (Years, nullable, deprecated, sunset `2027-03-01`, reason names `risk.duration@v1`).
- errors: `portfolio_not_found` 404 not retryable, next "Call portfolio.list@v1:fetch for the codes you may use."; `warehouse_suspended` 503 retryable, next "Wait retry_after seconds, then repeat the call."
- meaning: `grain` "portfolio × security × date", `keys` `["portfolio","isin","as_of"]`, `as_of` "the portfolio's last valuation on or before as_of", `source` "Harbour's portfolio holdings view".
- usage: `when` "You need the positions of named portfolios on a date."; `when_not` names `index.constituents@v1`; `related` `["portfolio.list@v1","index.constituents@v1"]`; `cost` "One warehouse query; large results are handed off."

Do NOT copy the vector into the journey profile unchanged:
- AMBIGUOUS: the vector is a protocol test vector (it exercises aliases, deprecation, enum aliases), not the owner's contract. `duration`'s deprecation names `risk.duration@v1` and `usage` names `index.constituents@v1`; neither is in the journey fixture, so a guide built from it would name a capability the caller cannot use (DISCOVERY-010) unless filtered.
- AMBIGUOUS: `history` (CAT:639 †) vs the `AsOf` trait's `start_date`/`end_date` (02-1:156) vs the sample, which omits both (HB:168). See decision H1.
- AMBIGUOUS: the vector's key includes nullable `isin`; §9.2.3 permits it ("the key is the set of non-null key values") but HB:168 and DESIGN-REVIEW:161 say it is not unique. See decision H4.
- AMBIGUOUS: `Filterable` is declared but the only declared filter is `asset_class`; HB:154 rules out security filters (that is `portfolio.exposure`).
- AMBIGUOUS: `market_value` "in its currency" (vector) vs holdings-dates' "base-currency market value" (CAT:640). See H6.

Proposed profile skeleton (fixture choices from §7 marked `H#`):

```text
identity:  id "portfolio.holdings", version 1, title "Portfolio holdings",
           summary "Positions in one or more portfolios on a valuation date.", domain "portfolio", owner "QFI"
properties: family data, stability official, lifecycle stable, visibility listed,
           traits [Projectable, Filterable, AsOf, Keyed, Licensed] (all critical:false)
           (Handoff added when handoff is built: see 1.2)
interactions:
  fetch   read, table, chunked, execution [gateway], deadline PT2M / PT30M,
          https POST /v1/data/portfolio.holdings@v1:fetch, encodings [arrow, json, ndjson, csv], auth bearer
  explain read, record, single, execution [gateway], deadline PT30S / PT2M, encodings [json]
  export  DEFERRED (see 1.1)
request (closed, required [portfolios]):
  portfolios  array<string PortfolioCode>, minItems 1, maxItems 500, uniqueItems,
              x-atlas-reference portfolio.list@v1
  as_of       string|null, format date, default null   -- H2: null = each portfolio's latest valuation
  asset_class string|null, enum [credit, rates, null], default null
  columns     array<string>|null, uniqueItems, default null (Projectable)
  (range: none in v1 -- H1)
result row (table; Arrow derived by §9.2.7):
  portfolio     string  PortfolioCode            key, required          Utf8
  security_id   string                           key, required (H4)     Utf8
  isin          string|null  Isin                not key (H4)           Utf8 nullable
  as_of         string  format date              key, required          Date32
  title         string|null                      description            Utf8 nullable
  market_value  object  Money {amount Decimal(38,6) string, currency Currency}
                                                 required (H6)          Struct<Decimal128(38,6), Utf8>
  weight        number  Fraction                 required (H7)          Float64
  oas           number|null  Bp                  nullable               Float64
  status        string enum [open, closed, UNKNOWN]  required           Utf8
errors (beyond the registry):
  portfolio_not_found  404  retryable false  next "Call portfolio.list@v1:fetch for the codes you may use."  (H8)
  warehouse_suspended  503  retryable true   next "Wait retry_after seconds, then repeat the call."
  no_valuation         404  retryable false  next "Call data.status@v1:fetch to see the latest landed date."  (H11, proposed)
meaning:
  grain  "portfolio × security × date"   keys [portfolio, security_id, as_of]
  as_of  "each portfolio's last valuation on or before as_of; the latest when as_of is null; rows carry their own as_of"
  source "Harbour's portfolio holdings view"
  (ordering, in prose: portfolio, as_of, security_id ascending -- H9)
usage: when / when_not (naming only journey capabilities), examples (the HB:63-69 request is one),
       related [portfolio.list@v1, portfolio.search@v1, data.status@v1], cost (words)
```
AMBIGUOUS in the skeleton: `security_id` is not in any source. It is the smallest change that gives a unique key (HB:168). The alternative keeps the vector's keys and has the owner accept a non-unique key for cash; that fails HB:168.

### 4.2 `portfolio.list@v1` — IN SCOPE

| Aspect | Source text (exact) | Citation |
|---|---|---|
| Description | "Current portfolios." | CAT:634 |
| Returns | "Per portfolio: code, name, region, asset class" | CAT:634 |
| Interactions | `fetch · explain · export` | CAT:634 |
| Result · delivery | `table · single` | CAT:634 |
| Execution | `gateway · handoff` | CAT:634 |
| Stability / Today | `official` / `portfolio.list` | CAT:634 |
| Handbook | "Enumerate current portfolios → code, name, region, asset class" / "Current catalogue; exact filters unspecified" | HB:237 |
| Role | Holdings' `portfolios.items` has `x-atlas-reference: portfolio.list@v1` (the capability that lists valid values) | §3.1.3; 09-2 §9.2.3 |
| Naming | "`list` and `search` count as nouns: a domain's catalogue and its lookup" | 02-1:187 **[outside assignment, verified]** |
| Overlap | Search vs browse vs describe: "These can be legitimate distinct discovery tasks. Describe the exact transition from found identifier to retrieval" | HB:540 |

MISSING: request fields, filters, ordering, keys, enum values of region and asset class, what "current" means.

Proposed profile skeleton:
```text
traits [Projectable, Filterable, Keyed]; fetch read, table, single, execution [gateway], deadline (proposed) PT30S / PT2M;
explain read, record, single; export DEFERRED
request (closed, no required members):
  asset_class string|null, enum [credit, rates, null], default null   (AMBIGUOUS: same domain as holdings' asset_class?)
  region      string|null, default null                              (values unspecified: free string)
  columns     array<string>|null, uniqueItems, default null
result row:
  code         string PortfolioCode  key, required
  name         string                required
  region       string|null
  asset_class  string enum [credit, rates, UNKNOWN] | null
meaning: grain "portfolio", keys [code], source "<fixture's portfolio master view>"
         "current" = open portfolios in the caller's scope (L1)
ordering: code ascending (L2)
```
`code` named for CAT:634; AMBIGUOUS: holdings calls the same value `portfolio`. Readings: (a) `code` (catalogue words); (b) `portfolio` (same name as holdings' key, easier to join). Recommend (b) only if the owner accepts renaming the catalogue's word; otherwise (a) with `x-atlas-logical: PortfolioCode` on both, which states they are the same identifier.

### 4.3 `portfolio.search@v1` — IN SCOPE

| Aspect | Source text (exact) | Citation |
|---|---|---|
| Description | "Loose text resolved to portfolios, ranked." | CAT:635 |
| Returns | "Per match: code, name, why it matched" | CAT:635 |
| Interactions | `fetch` | CAT:635 |
| Result · delivery | `table · single` | CAT:635 |
| Execution | `gateway` | CAT:635 |
| Stability / Today | `official` / `portfolio.search` | CAT:635 |
| Handbook | "`portfolio.search` has `fetch` only and gateway execution only." | HB:233 |
| Handbook row | "Resolve loose portfolio names → ranked codes and reasons" / "Search words; use returned codes in subsequent requests" | HB:238 |
| Agent rule | Atlas should not silently decide which portfolio "my fund" means; it "supplies candidates, documented defaults and evidence" | HB:15 |
| Division | Atlas owns "Resolving business identifiers through documented capabilities"; agent owns "Confirming an ambiguous business choice with the person" | HB:23 |
| ATLAS-CORE | "A capability declares ... how business identifiers are resolved through authorized lookups ... Ambiguous choices require selection." | ATLAS-CORE:44 **[outside assignment, verified]** |

MISSING: request member name, ranking, score, limits, empty result.

Proposed profile skeleton:
```text
traits [Searchable, Keyed]; fetch read, table, single, execution [gateway], deadline (proposed) PT10S / PT30S; no explain, no export
request (closed, required [words]):
  words  string, minLength 1, maxLength 200     (mirrors atlas.search's `words`, 09-3 §9.3.2)
result row (ordered best first):
  code   string PortfolioCode  key, required
  name   string                required
  why    string                required   ("matched 'high yield' in name")
  rank   integer x-atlas-unit count, required, 1 = best    (S2; catalogue lists only code, name, why)
meaning: grain "portfolio matching the words", keys [code]
behaviour: only portfolios in the caller's scope (R05; S1); no match = empty table (S3), never an error;
           at most 20 rows (S4)
```

### 4.4 `data.status@v1` — IN SCOPE

| Aspect | Source text (exact) | Citation |
|---|---|---|
| Domain purpose | "What the curated data behind capabilities is, and how fresh it is." | CAT:351 |
| Description | "Each dated source's latest date, live." | CAT:361 |
| Returns | "Per source: latest date, expected date, status" | CAT:361 |
| Interactions | `fetch` | CAT:361 |
| Result · delivery | `table · single` | CAT:361 |
| Execution | `gateway` | CAT:361 |
| Stability / Today | `official` / `snowflake.freshness` | CAT:361; HB:552 |
| House rule `freshness` | "Whether a source's data for a date has landed: `data.status`. To be told when it lands: `data.landed`." | CAT:89 |
| Handbook row | "Check freshness → source latest date, expected date and status" / "Data, `fetch`, single table; source/date behavior requires clarification" | HB:361 |
| Domain note | "This is the source-status domain; a source is not itself a license to invoke every capability reading it." | HB:356 |
| Overlap | "§7 describes latest date; event profile points `meaning.history` at data.status ... A latest snapshot is not a replayable landing history. Define whether fallback means latest-state reconciliation or event replay; do not promise both" | HB:544 |
| Event link | `data.landed@v1` `meaning.history: "data.status@v1"` | 03-contracts §3.1.3 **[outside assignment, verified]** |
| Common | "'Latest' must be reported as an effective source date, not mistaken for the current wall-clock time." | HB:144 |
| Page time row | "source freshness and expected publication; handling of weekends, missing snapshots, corrections and stale answers" | HB:117 |

MISSING: request fields, the `status` enum, source id format, how "expected date" is computed, calendar/weekends, how holdings names its source in `data.status`.
DEFERRED: the event-history role (`data.landed` history) with events (REV:15).

Proposed profile skeleton:
```text
traits [Filterable, Keyed]; fetch read, table, single, execution [gateway], deadline (proposed) PT10S / PT30S
request (closed):
  sources  array<string>|null, uniqueItems, default null   (every source visible to the caller when null)
           (no x-atlas-reference: data.sources@v1 is not in the journey -- D3)
result row:
  source    string                key, required   e.g. "harbour.portfolio-holdings" (D1)
  latest    string format date    required   the latest date whose data is complete
  expected  string|null format date          the date expected by now, per the source's calendar (D2)
  status    string enum [current, late, UNKNOWN] required (D2)
  checked   string format date-time  required   when the gateway read the state (D4)
meaning: grain "source", keys [source], source "<fixture's load log>"; "live": not cached (D4)
```

### 4.5 `portfolio.holdings-dates@v1` — IN SCOPE (added after the clients are built)

| Aspect | Source text (exact) | Citation |
|---|---|---|
| Description | "Valuation dates in a window." | CAT:640 |
| Returns | "Per portfolio and date: position count, base-currency market value" | CAT:640 |
| Interactions | `fetch · explain · export` | CAT:640 |
| Result · delivery | `table · single` | CAT:640 |
| Execution | `gateway · handoff` | CAT:640 |
| Stability / Today | `official` / `portfolio.holdings-dates` | CAT:640 |
| Handbook row | "Find valuation dates → portfolio/date, position count and total market value" / "Portfolio/date window" | HB:243 |
| Role | Capability added after the clients are built (acceptance item 4) | REV:42, REV:49 |

AMBIGUOUS: CAT says "base-currency market value", HB says "total market value". Same thing if the total is in the portfolio's base currency; base currency comes from `portfolio.reference` (CAT:636), which is not in the journey.
AMBIGUOUS: whether "position count" counts cash rows and `closed` rows (consistency with holdings' rows).

Proposed profile skeleton:
```text
traits [Projectable, AsOf, Keyed]; fetch read, table, single, execution [gateway], deadline (proposed) PT30S / PT2M;
explain read, record, single; export DEFERRED
request (closed, required [portfolios, start_date, end_date]):
  portfolios  array<string PortfolioCode>, minItems 1, maxItems 500, uniqueItems, x-atlas-reference portfolio.list@v1
  start_date  string format date        (AsOf's range names, 02-1:156)
  end_date    string format date        (inclusive; end_date >= start_date, else invalid_request -- HD1)
  columns     array<string>|null, uniqueItems, default null
result row:
  portfolio     string PortfolioCode  key, required
  as_of         string format date    key, required   (a valuation date)
  positions     integer x-atlas-unit count, required  (= holdings rows for that portfolio/date -- HD2)
  market_value  object Money (base currency), required  (= sum of holdings market_value -- HD3)
meaning: grain "portfolio × valuation date", keys [portfolio, as_of],
         as_of "every valuation date between start_date and end_date inclusive", source "Harbour's portfolio holdings view"
errors: portfolio_not_found as holdings
ordering: portfolio, as_of ascending
```
Independent check (acceptance item 6): the fixture can verify `positions` and `market_value` against holdings rows for the same portfolio/date.

---

## 5. The handbook's page contract and common behaviours, as checks on a guide

### 5.1 Page contract (HB:110-123), one check per row

| Part | Required content (exact) | Check on a journey guide |
|---|---|---|
| Promise | "One sentence explaining the question this capability answers; owner; canonical ID and version" | Section 1 has one sentence, owner, `id@vN` |
| Use / do not use | "Concrete examples, authoritative route and relevant alternatives" | Section 2 names only alternatives the caller can use |
| Behavior | "Data/resource/action/event; each offered interaction and its effect; stability and lifecycle separately" | Sections 1 and 3 |
| What to supply | "Exact field names, types, requiredness, defaults, allowed values, units and how to obtain valid identifiers" | Section 4; `portfolios` points at `portfolio.list`/`portfolio.search` |
| What to expect | "Result shape; table grain and keys or record fields; ordering; null/empty behavior; business meaning and units" | Section 5 states grain, keys, ordering, nulls, empty |
| Time | "Meaning of each date; source freshness and expected publication; handling of weekends, missing snapshots, corrections and stale answers" | Section 5 or 2; names `data.status` |
| Authority | "Identity used downstream; scope/entitlement checks; visibility and licensing obligations" | States service identity + scope rule (HB:45) and `Licensed` |
| Cost and size | "Metering unit; whether an estimate exists; caller-specific limits; output caps, paging and truncation behavior" | Section 8 |
| Execution | "Whether it may run as an operation, stream or export; what completion means; where results expire" | States the end line/final batch as completion |
| Change behavior | "Whether it changes anything; ETag and idempotency requirements; approval policy and retry behavior" | "Reads do not change holdings" (HB:163) |
| Examples | "A minimal valid request, a realistic request and the same request in CLI, Python and Node" | Section 6 has 2 examples, rendered per `client` |
| Failure and recovery | "Known business problems, retryability, precise next steps and an owner/support route" | Section 7 |

- "Generate the mechanical parameter, field and interaction tables; have the owner write the purpose, exclusions, business definitions and examples." (HB:108)
- "An owner-complete profile is therefore the acceptance criterion" (HB:125).
- Two views: "a review snapshot in this knowledge base ... and the served caller-specific guide, containing only granted interactions, permitted references, effective context and current limits" (HB:593).
- Readiness: "A capability is ready when a human can answer 'Is this the right question, what exactly will happen, what will I get, what might fail, and how do I call it?' and an agent can express that call unchanged in the CLI, Python and Node." (HB:595)
- REV:29 step 1: the capability card is ATLAS-CORE §4 plus this page contract. ATLAS-CORE §4's 13 parts (Identity, Purpose, Interactions, Inputs, Output, Time, Selection, Execution, Effects, Data handling, Reliability, Usage, Recovery) **[outside assignment, verified]** add: "omission versus null", "support contact", "timezone, update cadence, freshness evidence and correction policy", "partial-result and completion semantics".

### 5.2 Common behaviours (HB:129-144), as checks

| Interaction or result | Expected behavior (exact) | Journey check |
|---|---|---|
| Data `fetch` | "Read the answer described by the profile. A read can be licensed, metered, slow or large. It is not automatically free." | |
| Data `explain` | "Validate and plan a particular request, with estimates where supported. It is offered and granted separately; it is not an obligatory extra call before every read." | explain grant separate from fetch (DISC:136) |
| Data `export` | "Produce a downloadable result for bulk use. It has its own result/delivery contract; a catalogue table's 'table, single' usually describes `fetch`, not export." | DEFERRED |
| Chunked table | "Consume batches. Do not equate received rows with a successfully completed answer until the completion marker is checked." | Holdings incomplete-stream test in every client (acceptance item 5) |
| Operation | "An accepted task, followed by progress and a final result or problem. A `202` is not success." | Only where durable work needs it |
| Transfer/export | "An upload/download with completion and expiry. A short-lived URL is a delivery mechanism, not permanent storage." | DEFERRED |

- Effects `read`, `write`, `external`, `irreversible`; "Reads with a cost above the caller's confirmation threshold require confirmation. A generic retry must never evade a budget, approval, deny or confirmation." (HB:142)
- "The source spec keeps an operation at least 24 hours after completion and specifies a 24-hour idempotency window. The proposed core requires retention to cover active work and its promised recovery window" (HB:144).
- "A large table should be streamed or exported; materializing every row is an explicit client choice." (HB:144)
- Generic `call(address, request, options)`; "Language-specific convenience methods ... must not introduce different business parameters or hidden execution behavior." (HB:59). "Exact result-handle methods for the generic call should be settled consistently across clients before examples are published as runnable instructions." (HB:102)
- "For a durable application, pin the capability version and reviewed contract through its lock." (HB:104). **OVERRIDDEN BY REVIEW (R02):** "A lock pins semantic contracts. Routing is fetched as current metadata." (REV:60)

---

## 6. Plugging in (§4) and adding a capability end to end

### 6.1 What §4 says a client does

| Fact (exact or near-exact) | Citation |
|---|---|
| "Two halves of every contract: the gateway writes (describes, serves, guarantees); the client reads (resolves, formats). A client holds no code for any capability." | PLUG:11 |
| Finds the gateway: "given, `ATLAS_URL`, `ATLAS_ENV`, its configuration, else Production"; reads `/.well-known/atlas`; signs in from the shared token store | PLUG:12. **OVERRIDDEN BY REVIEW (R11, R26):** "Credentials come from each runtime's own provider" (REV:69); credentials bound to gateway, authority, audience, tenant/account, scopes; refresh serialised (REV:84) |
| "Every call is eight steps: find, understand, validate, call, travel, decide, answer, read." | PLUG:13 |
| Levels: "Core always; Streaming, Columnar, Bulk, Handoff as it implements them"; "served the next way down where it lacks one, never refused" | PLUG:14 |
| Rules: "Every client plugs into every surface the same way, as §9.7 defines." "A client never contains behaviour for one capability, other than the built-ins, and never builds an address." | PLUG:57-58 |
| Request parts: Address `id@vN:interaction` ("a bare `id` resolves to the current stable version"); Parameters; Selection (`columns`, where `Projectable`); Filters, order, cursor (where declared); Execution preference (`Atlas-Execution`); Idempotency key (every write); ETag; Deadline (absolute); Expected profile; Client | PLUG:124-135 |
| Result parts: data; Profile (writer fingerprint); Result schema (for tables, in the Arrow schema's metadata); Warnings; Usage (`Atlas-Limit-Remaining`); Request id and trace (`X-Request-Id`, `traceparent`) | PLUG:139-146 |
| Problem members: `type`, `title`, `status`, `detail`, `instance`; `code`; `retryable`; `retry_after`; `target`; `next`; `details` (each hop adds `{"hop": "service", …}`); `request_id`. Header `Atlas-Error-Code` = `code`. `type` is `https://atlas.jhg.com/problems/<code>` (09-3) | PLUG:150-163 |
| **ATLAS-PROTOCOL-001.** Every failure on every binding is a problem with a registered `code`, the `request_id` and a `next` step. | PLUG:113 |
| **ATLAS-PROTOCOL-002.** Every result names its writer profile's fingerprint and the schema of what it returned. | PLUG:114 |
| **ATLAS-PROTOCOL-003.** Work past its deadline stops in every downstream service, and a cancelled call is cancelled downstream. | PLUG:115 |
| **ATLAS-PROTOCOL-004.** Every long-running interaction returns an `atlas.operation`, which can be polled, watched and cancelled. | PLUG:118 |
| **ATLAS-PROTOCOL-005.** A problem marked `retryable` is never returned for a request that changed something and cannot be repeated safely; every problem's `Atlas-Error-Code` header equals its `code`. | PLUG:116 |
| **ATLAS-PROTOCOL-006.** No downstream service works past the caller's deadline (a test with a two-second deadline on a ten-second query). | PLUG:117 |
| Operation fields `name` (`operations/{id}`), `address`, `state` (`pending`, `running`, `succeeded`, `failed`, `cancelled`, `UNKNOWN`), `progress`, `result` or `problem`, `owner`, `created`, `updated`; `202 Accepted`, `Operation-Location`, `Retry-After`; kept at least 24 hours; idempotency key kept 24 hours; `idempotency_conflict`; `in_progress` | PLUG:187-198. **OVERRIDDEN BY REVIEW (R08, R17)** for key retention and cancellation (REV:66, REV:75) |
| Retries: only `retryable` and replayable; exponential backoff with jitter; honour `Retry-After`; budget "at most one in ten calls to a dependency in a minute"; one hop only | PLUG:206 |
| Request headers: `Atlas-Client`, `Atlas-Client-Features`, `Atlas-Profile-Expect`, `Atlas-Deadline`, `traceparent`, `Idempotency-Key` on any change, `If-Match` on update/delete. Answer headers: `Atlas-Profile`, `Atlas-Governance-Revision`, `X-Request-Id`, and where they apply `Atlas-Limit-Remaining`, `Atlas-Error-Code`, `Deprecation`, `Sunset`, `Retry-After`, `Operation-Location` | PLUG:210 |
| Bindings: "Clients never build addresses"; "No infrastructure parses a name"; "A binding may change within a version" (old binding served for a migration window) | PLUG:260-262 |
| **ATLAS-BINDING-001.** Every interaction in every profile has at least one binding whose server is named in the gateway's bootstrap. **ATLAS-BINDING-002.** The same request through any two bindings of one interaction returns the same result, in each binding's encoding. **ATLAS-BINDING-003.** OpenAPI/AsyncAPI regenerated by the gates | PLUG:264-266 |
| Binding fields: `protocol` (`https`, `amqp`, `blob`, `snowflake`, `mcp`; `sse`, `wss` reserved), `server`, `address`, `method`, `encodings` (`json`, `arrow`, `ndjson`, `csv`, `parquet`, `cloudevents+json`), `auth` (`bearer`, `entra-scope`, `ticket`); client picks the first it supports in profile order | PLUG:272-281 |
| Addresses (informative): `/v1/<family>/<id>@v<N>:<interaction>`, built-ins `/v1/atlas/<id>:<interaction>`, operations `/v1/operations/<id>` | PLUG:283 |
| HTTP: every interaction `POST` with JSON body (`GET` only for bootstrap and polling operations) | PLUG:287 |
| MCP fixed tools: `atlas_whoami`, `atlas_search`, `atlas_guide`, `atlas_describe`, `atlas_call`, `atlas_subscribe`, `atlas_operation`, `atlas_access`; "The set never changes with the catalogue (a new capability is a new address for `atlas_call`)"; bulk data never through MCP | PLUG:296. MCP not named in the journey's clients (REV:12): **DEFERRED** unless another digest says otherwise |

### 6.2 Adding a capability, end to end (what the spec describes; §4 itself has no section on it)

| Step | What | Citation |
|---|---|---|
| 1 | Write the profile first: `profiles/<id>@v<N>.json` in its service's package, "the profile without `fingerprint`, which the gateway computes" | AG:62; 03-contracts §3.1.5 **[outside assignment, verified]** |
| 2 | Lint it and classify it against the previous profile (new capability: no previous) | AG:62; §3.3.12 steps 1-2 |
| 3 | Generate: `go run ./gateway/cmd/atlas-new capability --pattern <P> <id>`; writes profile, service in its domain package registered in the deployable's list, battery and end-to-end cases named by requirement id, blast-radius row and a switch; `TODO`s fail the build | AG:64; 06-7 §6.7.18 **[outside assignment, verified]** |
| 4 | Fill in "the statement, the mapping, the meaning" | AG:65 |
| 5 | Pass the family's battery, in process and over loopback. **OVERRIDDEN BY REVIEW (R20):** "In-process provider conformance first; remote transport conformance when a provider is first extracted" | AG:66; REV:78 |
| 6 | Add its row to the blast-radius table, and its switch | AG:67 |
| 7 | Answer the 15-question review (§6.6.3) in the PR | AG:68 |
| 8 | Add it to its domain's table in §7 in the same change. **ATLAS-CATALOGUE-001**: the catalogue set equals the served set at the same commit. **OVERRIDDEN BY REVIEW (R23):** "Catalogue summaries and counts are generated; examples, registered profiles and deployed availability are kept distinct" | CAT:96, CAT:105; REV:81 |
| 9 | Registration: the gateway admits a service only when its profiles pass lint and compatibility, it claims only its own domain's ids, and it passes its family batteries; admission atomic; conditions `accepted`, `compatible`, `conformant` | 06-1 §6.1.6 **[outside assignment, verified]** |
| 10 | Gate step 8, "Callable by every supported client": refuse a profile that needs anything beyond `minimum_protocol` "(a delivery, encoding, lens operation, handover kind, connector or trait marked critical added later)" | §3.3.12 item 8 **[outside assignment, verified]** |
| 11 | Clients learn of it: catalogue ETag changes; clients poll every 15 minutes or revalidate at session start (change feed DEFERRED); DISCOVERY-007 | DISC:178, DISC:51 |
| 12 | Agents use it without change: skill, MCP instructions and prompts "name no capability" | DISC:124 |

Requirements bearing on "a capability added after the clients were built" (acceptance item 4, REV:49):
- **ATLAS-CORE-009.** "Deploying a gateway with a new capability needs no change anywhere else: no client release, and no change to any agent, skill, system prompt, MCP tool list or harness configuration. Every client in use finds, understands and calls it, and an agent completes a task with it, the moment it is served." (00-overview:91 **[outside assignment, verified]**)
- **ATLAS-CLIENTS-002.** "A capability added to the catalogue is callable from every client with no client release." (05-clients:72 **[outside assignment, verified]**)
- D46 (08-design-decisions:68 **[outside assignment, verified]**): clients "never refuse one they have not seen".

Test shape for `portfolio.holdings-dates@v1`: build and freeze CLI, Python, Node; deploy the gateway with the new profile and service; with the frozen clients run `whoami` -> `search "valuation dates for a portfolio"` -> `guide portfolio.holdings-dates@v1` -> `call portfolio.holdings-dates@v1:fetch` with the guide's example; identical request records in all three; result checked against the source fixture. Its profile must use only deliveries, encodings and traits the frozen clients already handle (`table · single`, `json`; no critical traits).

---

## 7. Owner decisions, with a stated default for the fixtures

Every default below is a **PROPOSED FIXTURE DEFAULT**: not in any source. Each profile must say "owner decision; fixture fixes: ...".

### Holdings (HB:168 lists these; REV:96 repeats the main ones)

| # | Decision (source words) | Citation | Proposed fixture default |
|---|---|---|---|
| H1 | "the history/range request described by §7 but omitted from the sample schema" | HB:168; CAT:639 † | v1 takes one `as_of` only. A range is added later as optional `start_date`/`end_date` (the `AsOf` names), which R03 allows within the version. `portfolio.holdings-dates` lists the dates |
| H2 | "what null `as_of` means" | HB:168; REV:96 | Each portfolio's latest valuation date with complete data (vector: "the latest when null"); each row carries its effective `as_of` (HB:35, "the answer reports the effective value") |
| H3 | "whether every portfolio uses the same snapshot date" | HB:168 | No: each portfolio independently takes its last valuation on or before `as_of`; rows show it |
| H4 | "keys for cash, derivatives and securities without ISINs" | HB:168; REV:96; DESIGN-REVIEW:161 | Add a required, non-null key `security_id` (the source's own security id; cash `CASH-<CCY>`); `isin` stays nullable and is not a key |
| H5 | "duplicate-lot aggregation" | HB:168; REV:96 | One row per portfolio × security × date: lots summed |
| H6 | "market-value currency and signs" | HB:168; REV:96 | `Money` in the portfolio's base currency (matches holdings-dates' "base-currency market value"); short positions negative |
| H7 | "whether weights include cash and sum to one" | HB:168; REV:96 | Include cash; per portfolio × `as_of`, weights sum to 1 (`weight` = `market_value.amount` / the portfolio's total) |
| H8 | "partial authorization or missing portfolios" | HB:168; REV:96; R05 | Refuse the whole request when any code is unknown OR outside the caller's scope: `portfolio_not_found` 404, `target` `/portfolios/<i>`, the same answer in both cases (no existence disclosure, like DISC:42's confidential rule). The second fixture identity sees only its portfolios in list/search and gets this refusal for the others (acceptance item 3) |
| H9 | "ordering and row limits" | HB:168 | Order `portfolio`, `as_of`, `security_id` ascending. Output cap from governance: `max_rows` 1000000, `max_bytes` 1073741824 (the §3.1.3 values); the truncation test lowers it for one identity |
| H10 | "source-correction/freshness behavior" | HB:168 | Answers as currently stored; corrections overwrite, no knowledge-time point-in-time in v1; freshness via `data.status` source `harbour.portfolio-holdings` |
| H11 | Empty answer meaning (ATLAS-CORE:80) and "how the caller detects and rejects an excessively old snapshot" (HB:166) | ATLAS-CORE:80; HB:166 | Zero rows for a portfolio = no positions on its effective date. A permitted portfolio with no valuation on or before `as_of` → declared error `no_valuation` 404, not retryable. No max-age parameter in v1: the caller compares the rows' `as_of` with `data.status` |
| H12 | "the `Licensed` sample trait's persistence rules" | HB:168 | Keep `Licensed`; no result cache (R18: results private to their caller); clients do not persist rows; no handoff in v1, so DS rule 4 does not arise |

### Other journey capabilities

| # | Decision | Citation | Proposed fixture default |
|---|---|---|---|
| L1 | `portfolio.list` "exact filters unspecified"; what "current" means | HB:237 | Filters `asset_class`, `region`; current = open portfolios; only portfolios in the caller's scope |
| L2 | `portfolio.list` ordering | HB:116 | `code` ascending |
| S1 | `portfolio.search` scope | R05; HB:35 | Only portfolios in the caller's scope |
| S2 | Ranking and reasons | CAT:635 | Deterministic lexical match on code and name (exact code first, then name prefix, then word overlap); ties by code; `rank` 1 = best; `why` names the matched words |
| S3 | No match | HB:116 | Empty table, not an error |
| S4 | Row cap | HB:119 | At most 20 rows |
| D1 | data.status source ids; how holdings names its source | HB:361 | `<system>.<object>` strings; holdings' source is `harbour.portfolio-holdings` |
| D2 | `status` values and "expected date" | HB:361; HB:117 | `current` (latest ≥ expected), `late` (latest < expected), `UNKNOWN`; expected = the previous business day on a Monday–Friday calendar, no holidays |
| D3 | Which sources a caller sees | HB:356, HB:360 | Sources read by at least one capability available to the caller |
| D4 | "live" | CAT:361 | Not cached; `checked` timestamp in each row |
| D5 | Latest state vs event history | HB:544 | Latest state only; event replay DEFERRED with events |
| HD1 | holdings-dates window rules | HB:243 | `start_date` ≤ `end_date`, inclusive; at most 366 days, else `invalid_request` |
| HD2 | Position count | CAT:640 | = holdings rows for that portfolio and date (cash included, H4/H5) |
| HD3 | Total market value | CAT:640; HB:243 | = sum of holdings `market_value` in base currency (H6) |

### Cross-cutting (REV:97-100)

| # | Decision | Citation | Proposed fixture default |
|---|---|---|---|
| X1 | Organisation/tenant switching | HB:33; REV:97 | One tenant and one environment per connection; `whoami` returns the effective context; no switching in the first build; caches keyed by that context |
| X2 | Minimum sunset | REV:98; §3.3.12 Open ("Recommended: 90 days") | 90 days |
| X3 | Lock format | REV:98; §3.3.12 Open ("`atlas.lock`, TOML, at the application's root") | As recommended |
| X4 | Catalogue overlaps (holdings vs exposure; others) | HB:537-548; REV:100 | Keep both documented. The journey serves only holdings; the guide's "Related" names only journey capabilities |
| X5 | Confidential capabilities | DISC:200 | None of the journey's four is confidential (`visibility: listed`) |
| X6 | Context rule for defaults | HB:35 | No default portfolio; `as_of` default per H2 and reported |
| X7 | Settlement mismatch enforcement | DS:102 | Record only (DS recommendation); DEFERRED with handoff/export |

---

## Ambiguities collected (each also marked above)

1. `export` listed in the catalogue for holdings, list, holdings-dates, but DS:76 says "After the first two" and REV:33 defers direct downloads.
2. `handoff`: "Build first" (DS:75) vs REV:15/REV:33 deferrals; pass 1 has fake-backed handoff.
3. `AsOf` "as the data stood then" vs holdings' valuation-date meaning.
4. Holdings `history` (†) vs `start_date`/`end_date` vs the sample schema's omission.
5. Nullable `isin` in the key (allowed by §9.2.3, rejected as insufficient by HB:168).
6. Market value currency: "in its currency" (vector) vs "base-currency" (holdings-dates).
7. Truncation signalling for `single` delivery.
8. Where ordering is declared in a profile.
9. Search ranking: no algorithm, score range or tie-break.
10. `max_tokens` tokenizer.
11. `atlas.access` in or out of the first build.
12. `code` (list/search) vs `portfolio` (holdings) for the same identifier.
13. `data.status` request, enum, source ids and calendar all missing; link from a capability to its source missing.
14. Whether journey profiles still carry `compatibility` after R03.
15. The reference vector's `related`/deprecation name capabilities outside the journey.
16. SF's `bytes` type has no row in §9.2.7's Arrow mapping.
17. "Position count" scope in holdings-dates.

## Files read

In full: `AGENTS.md`, `docs/pass-2/REVIEW.md`, `02-2-data-surface.md`, `02-2-1-snowflake-data.md`, `02-6-discovery.md`, `04-plugging-in.md`, `review/CAPABILITY-HANDBOOK.md`, `07-the-catalogue.md` (all 959 lines).
Read in part, to verify cited facts: `02-1-capabilities.md:110-175`; `03-contracts.md:130-350, 761-790`; `09-2-schemas.md:50-140`; `09-3-documents.md:25-110`; `09-5-resolution.md:52-72`; `09-6-http-binding.md:60-112`; `06-1-front-door-and-services.md:150-175`; `06-4-data-sources.md` (grep lines 5, 11, 66, 84, 95); `06-7-implementation-patterns.md:298-330`; `review/ATLAS-CORE.md:30-135`; `review/DESIGN-REVIEW.md` (lines 70-72, 161, 182, 189); `references/vectors/profiles/data.json` (whole case); `references/schemas/chunk-end`, `catalog-page`, `describe`, `explain`, `governance`, `profile` (top level), `builtins` (`$defs` named above); `references/QUESTIONS.md:70-79`; `references/scenarios/CORE-GUIDE-001.json` (first 60 lines); `references/vectors/guard.json` (description).
