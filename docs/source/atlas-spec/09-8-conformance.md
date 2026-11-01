# §9.8 Conformance

`spec/protocol/conformance.md` · proposed

> Section 9.8 of the Atlas Protocol: how a client in any language, or the gateway, is proven conforming: the levels, the test vectors and what each covers, the scripted conformance gateway, the language-neutral runner protocol a client implements to be driven by the suite, what passing means, and how a new language's client is accepted.

## Purpose

To decide whether a client or the gateway conforms by running it, not by reading its code, and the same way for every language, as the protobuf, gRPC and JSON Schema projects test their implementations.

## 9.8.1 Two kinds of test

| Kind | Tests | How |
|---|---|---|
| Vectors | The pure algorithms: grammars, schema validation, fingerprints, resolution, header parsing, Arrow schema derivation | Each implementation reads `references/vectors/*.json` in its own unit tests and checks every case |
| Scenarios | Behaviour over a connection: the call algorithm, deliveries, problems, retries, deadlines, subscriptions, transfers, sign-in | The suite runs a conformance gateway and drives the client through the runner protocol (9.8.3) |

## 9.8.2 The vectors

| File | Covers | Status |
|---|---|---|
| `vectors/fingerprints.json` | §9.4 | Written |
| `vectors/profiles/*.json` | Complete, valid profiles, one per family | One written (data); resource, action and event to add |
| `vectors/resolution.json` | §9.5 | Written |
| `vectors/grammar.json` | §9.1: strings each grammar accepts and rejects | To write |
| `vectors/schemas.json` | §9.2: schemas, instances and the validation result with its pointers | To write |
| `vectors/headers.json` | §9.6.3: header field values and their parsed form, or rejection | To write |
| `vectors/arrow.json` | §9.2.7: table schemas and their Arrow schemas | To write |
| `vectors/handoff/snowflake-arrow-1/` | Snowflake result chunks, captured from Development, with their source schema and the expected Atlas Arrow batches | To capture in the Snowflake spike |
| `vectors/handoff/snowflake-arrow-1/synthetic/` | Chunks written to Snowflake's documented encoding (scaled integers for `NUMBER(p,s)`, struct timestamps) by the generator, with the expected Atlas batches | To write; replaced by the captured ones when the spike runs |
| `scenarios/*.json` | §9.8.4: one file per scenario: its id, level, the gateway's script, the runner's commands, the expected results and the expected recorded requests | To write, from the table in 9.8.4 |

Each vector file is I-JSON with a `description` and `cases`; each case has a `name` and either an expected value or an expected problem (`code`, and `target` where one applies).

## 9.8.3 The runner protocol

To be tested at the scenario level, a client provides a runner: a small program in its own language that reads commands as JSON lines on standard input and writes one JSON line per result on standard output, calling only the client's public interface (9.7.1). The suite starts the runner with the environment variables `ATLAS_URL` (the conformance gateway), `ATLAS_HOME` (a fresh directory) and `ATLAS_CONFORMANCE=1`.

Command (one line):

```json
{ "id": 7, "op": "call", "args": { "address": "conformance.rows@v1:fetch", "request": { "n": 3 }, "options": { "deadline_ms": 2000, "accept": ["application/x-ndjson"] } } }
```

Result (one line):

```json
{ "id": 7, "ok": true, "value": [ { "i": 0 }, { "i": 1 }, { "i": 2 } ], "warnings": [], "calls": 1 }
```

or `{ "id": 7, "ok": false, "problem": { …the problem document… } }`.

`op` is an operation of 9.7.1 (`connect`, `whoami`, `catalog`, `search`, `describe`, `access`, `call`, `fetch`, `operation.get`, `operation.wait`, `operation.cancel`, `subscribe.open`, `subscribe.next`, `subscribe.close`, `publish`, `export`, `upload`), or `version` (the runner answers its client's name, version, protocol and levels). Values are written as JSON; Arrow results as arrays of row records (decimals as their strings, timestamps as RFC 3339), bytes as base64. (The Go SDK's runner is also the Python SDK's engine, which adds Arrow results on a second pipe and `cancel` for its own face (§5.1); the suite uses only the JSON form.) `options` may carry `person: true` (a person is at the client) and `confirm: "accept"` or `"decline"` (what that person answers to a `confirmation_required`). Warnings the client emitted (deprecations, truncation) are returned in `warnings`, each `{ "code", "text" }`. The runner adds no logic: it translates commands into calls on the client and results into lines.

## 9.8.4 The conformance gateway

The suite's gateway is built in Go, in `sdk/go/conformance`, on `sdk/go/protocol`. It serves the `atlas.*` built-ins and the `conformance.*` capabilities below, and plays one scripted scenario at a time. Its bootstrap names the environment `conformance`, and its `sign_in.authority` is itself, with a token endpoint that accepts the suite's refresh tokens. It records every request it receives, and the suite checks the requests as well as the client's results: headers present and well formed, idempotency keys reused on retry, deadlines decreasing, bodies valid.

The suite drives it through a control interface on loopback only, which no client ever calls: `POST /_conformance/scenario {"scenario": "<id>"}` loads a script, `GET /_conformance/requests` returns what was recorded, `POST /_conformance/reset` clears both. Events go through the Service Bus emulator, which the suite starts beside it.

**The capabilities**, each with profiles in the fingerprints and versions the scenarios need:

| Capability | Family | Interactions | Exercises |
|---|---|---|---|
| `conformance.echo@v1` | data | `fetch` (record, single) | Validation, headers, the identity resolution, the tolerant reader |
| `conformance.rows@v1` | data | `fetch` (table, chunked: json, ndjson, arrow), `explain`, `export` | Framing, end lines, final batches, truncation, `columns`, encodings returning identical records |
| `conformance.items@v1` | data | `fetch` (collection, single, `Paged`) | Cursor paging |
| `conformance.slow@v1` | data | `fetch` (record, operation) | 202, polling, `Retry-After`, `result_location`, cancelling, deadlines |
| `conformance.evolving@v1` | data | `fetch` | Profiles `#A`, `#B`, `#C` joined by one lens of each operation; `Deprecation` and `Sunset`; a request under an old profile; the gateway resolving for a client without a lens operation |
| `conformance.evolving@v2` | data | `fetch` | Two live versions; `@v1` deprecated |
| `conformance.retired@v1` | data | `fetch` | `retired` |
| `conformance.critical@v1` | data | `fetch` | A trait marked critical that no client implements (`ConformanceOnly`) |
| `conformance.costly@v1` | data | `fetch` (`Metered`) | `confirmation_required`, `Atlas-Confirm` |
| `conformance.dynamic@v1` | data | `fetch` (`DynamicSchema`) | Answers that carry their own columns; no resolution |
| `conformance.handoff@v1` | data | `fetch` (`gateway`, `handoff`) | Chunks in `snowflake-arrow/1`, served from the conformance gateway's own result storage |
| `conformance.thing@v1` | resource | `get`, `list`, `create`, `update`, `delete`, custom `touch` | ETags and 412, field masks, idempotency keys replayed and in conflict; the envelope, with `status`, conditions and `deleting` |
| `conformance.file@v1` | resource | `publish` (transfer up), `get` (transfer down) | Parts, checksums, commit, expired links |
| `conformance.change@v1` | action | `plan`, `approve`, `run` (operation) | `approval_required`, the receipt, a repeated run |
| `conformance.tick@v1` | event | `subscribe`, `publish` | A connect handover (emulator auth), duplicates, an event under an older profile, a revoked grant |

**The scenarios.** Each is one file in `references/scenarios/`; its id names the level it belongs to.

| Scenario | Level | The gateway does | The suite asserts |
|---|---|---|---|
| `CORE-BOOT-001` | Core | Serves the bootstrap without a token, with an ETag | The client fetches it unsigned, revalidates with `If-None-Match`, and builds every URI from it |
| `CORE-BOOT-002` | Core | — | The gateway is chosen by 9.7.2's order (`connect` argument, `ATLAS_URL`, `ATLAS_ENV`, `config.toml`) |
| `CORE-SIGNIN-001` | Core | Requires a bearer token | The token comes from `<ATLAS_HOME>/state/token-<env>.json` |
| `CORE-SIGNIN-002` | Core | — | A token within 60 s of expiry is refreshed first; the file is rewritten atomically, mode `0600` |
| `CORE-SIGNIN-003` | Core | Answers `401 invalid_token` once | The client refreshes and repeats once; a second `401` is `unauthenticated` |
| `CORE-SIGNIN-004` | Core | Answers a claims challenge | Without a person: `unauthenticated`, with the challenge in `details` |
| `CORE-SIGNIN-005` | Core | — | A token file with wider permissions is refused |
| `CORE-DISC-001` | Core | Serves catalogue, search and describe | Pages are followed by cursor; a profile's fingerprint is recomputed; a wrong one is `invalid_document` |
| `CORE-DISC-002` | Core | — | A profile is fetched once per `exact-id`, then served from the cache |
| `CORE-GUIDE-001` | Core | Serves `atlas.guide` | The client asks for its own syntax (`client`) and returns the Markdown unchanged |
| `CORE-EXPLAIN-001` | Core | Serves `conformance.rows@v1:explain` | The request is validated as for `fetch`; the explain answer is returned whole, its statement byte for byte |
| `CORE-CALL-001` | Core | Records the call | Every header of §9.6.3 is present and parses; built-ins carry no `Atlas-Profile-Expect` |
| `CORE-CALL-002` | Core | — | An invalid request fails `invalid_request` with its pointer, and nothing is sent |
| `CORE-CALL-003` | Core | Serves an interaction at an unusual address | The client follows the binding; it never builds a path |
| `CORE-CALL-004` | Core | Fails a write with `503` once | The retry carries the same `Idempotency-Key` |
| `CORE-CALL-005` | Core | Records bodies | The same inputs give the same request record in every client (AP-CLIENT-003) |
| `CORE-PROB-001` | Core | Returns each registry code in turn | Each becomes the language's error with every member; the CLI's exit code matches 9.7.6 |
| `CORE-PROB-002` | Core | Returns a code no client knows | It is handled by its `status` and `retryable` |
| `CORE-RETRY-001` | Core | `429` and `503` with `Retry-After` | At most three retries, honouring `Retry-After`, never past the deadline; a non-retryable problem is not retried |
| `CORE-DEADLINE-001` | Core | Delays past the deadline | `Atlas-Deadline` is the default, capped at the maximum; the call ends `deadline_exceeded` |
| `CORE-TOL-001` | Core | Adds unknown members, headers, enum values and traits | Ignored; an unknown enum value reads `UNKNOWN` |
| `CORE-CRIT-001` | Core | Marks a trait critical | `critical_trait_unsupported`, and no call is made |
| `CORE-RES-001` | Core | Answers under a newer fingerprint | The client fetches the writer's profile by `exact`, applies each lens backward, and returns its own shape |
| `CORE-RES-002` | Core | Answers a projected request | Rows are valid against the projected schema (§9.5.2) |
| `CORE-RES-003` | Core | Expects a request under an older profile | `Atlas-Profile-Expect` names the locked profile; the gateway resolves it forward |
| `CORE-LOCK-001` | Core | — | `atlas.lock` is found in the nearest ancestor; its entries are the reader profiles; no discovery call is made for them |
| `CORE-DEPR-001` | Core | Sends `Deprecation` and `Sunset` | A warning naming what, its sunset and replacement |
| `CORE-OP-001` | Core | Answers `202` | Polls no sooner than `Retry-After`, doubling to 30 s; reads `result` or `result_location`; `cancel` reaches `cancelled` |
| `CORE-CONFIRM-001` | Core | Answers `confirmation_required` with an estimate | Without a person: the problem. With a person who accepts: one repeat with `Atlas-Confirm` and the same key |
| `CORE-CONTENT-001` | Core | Answers `conformance.file@v1:get` with small content, its name and its `sha256` | With no path: the bytes in memory with their metadata, nothing on disk. With a path: the file there, written atomically, with `atlas-manifest.json` |
| `CORE-CONTENT-002` | Core | Answers with content above `max_memory`, then once with a wrong `sha256` | The large content lands at `<ATLAS_HOME>/downloads/<date>/<request id>/<name>` with its manifest; the wrong checksum leaves no file and raises `invalid_document` |
| `CORE-ENV-001` | Core | Serves `conformance.thing` records with the envelope: a condition type and reason no client knows, a `status` that changes without the ETag, then a `delete` answered with `deleting` set | The client exposes `metadata` and `status` as typed members and keeps the unknown condition; an `update` built from the record sends its own fields and `labels` only, with `If-Match` from `metadata.etag`, and succeeds after the status change; the deleting record reads back |
| `CORE-UPGRADE-001` | Core | Answers `426` | `upgrade_required` |
| `STREAM-001` | Streaming | Streams NDJSON rows and the end line | All rows, then usage from the end line |
| `STREAM-002` | Streaming | Ends a stream without the end line | `upstream_failed` |
| `STREAM-003` | Streaming | Puts a problem in the end line after rows | The problem is raised after the rows |
| `STREAM-004` | Streaming | Truncates at the output limit | A warning `output_limit_exceeded`; the rows sent are returned |
| `COL-001` | Columnar | Streams Arrow with its metadata and the zero-row final batch | The same records as NDJSON; decimals exact; timestamps UTC microseconds |
| `COL-002` | Columnar | Omits the final batch | `upstream_failed` |
| `COL-003` | Columnar | Answers under a newer fingerprint in Arrow | Column-wise resolution equals row-wise |
| `BULK-001` | Bulk | Exports to parts, one failing once | Every part fetched and checked by `sha256`; the failed one again |
| `BULK-002` | Bulk | Issues upload parts | Parts of at most 8 MiB, then one commit with the same key |
| `BULK-003` | Bulk | Lets a link expire | The client asks again, and finishes |
| `EVT-001` | Events | Answers `subscribe` with an `emulator` connect handover; sends events, one twice, one under an older profile | Each event once, resolved |
| `EVT-002` | Events | Answers `publish` with a handover | The payload is validated first; the CloudEvent's attributes are as 9.7.8 |
| `EVT-003` | Events | Sends an `emulator` handover while its bootstrap says `dv` | Refused (AP-EVENTS-002) |
| `EVT-004` | Events | Revokes the grant | The iterator ends with `not_granted` |
| `HAND-001` | Handoff | Answers with a handoff document | Chunks fetched in parallel with their headers, decoded, checked; identical to the gateway's own execution |
| `HAND-002` | Handoff | Lets links expire | The operation is asked again for fresh links |
| `HAND-003` | Handoff | Serves a chunk whose schema differs | `invalid_document` |

**The gateway suite** runs the same scenarios the other way: a reference runner (the Go SDK's) drives the real gateway, which mounts the same `conformance.*` capabilities (the package `sdk/go/conformance`, written once) only where its environment is `local` or `conformance`, never in Development, Acceptance or Production. It adds, for the gateway alone: every document it sends validates (AP-DOC-001); the built-ins match 9.3.2 exactly (AP-DOC-002); a client lacking a lens operation is answered already resolved (§9.5.6); a client declaring no features is served correctly (ATLAS-VERSION-005); an invalid request from a caller without the grant is `not_granted`, and from one with it `invalid_request` with no limit reserved (ATLAS-ACCESS-009, 010).

## 9.8.5 What passing means

A client conforms at a level when it passes every vector and every scenario of that level and of every level it requires, against the conformance gateway of the protocol version it declares, with no case skipped. The gateway conforms when it passes the gateway suite (the same vectors, and scenarios driven by a reference runner against it). Results are published per client release: its name, version, protocol, levels and pass or fail per case.

## 9.8.6 A client in a new language

A client in a new language is accepted when:

1. it declares its levels;
2. it passes the vectors and scenarios of those levels;
3. its runner is committed beside it and runs in its CI against the conformance gateway;
4. its README names the levels it conforms to and the protocol version.

It need not resemble any other client internally. It may be written by hand, generated from §9 and its artifacts, or both; the suite decides.

## Rules

1. No client or the gateway is released without passing the suite at its declared levels.
2. A defect found in one client that the suite did not catch adds a vector or a scenario before it is fixed.

## Units

`references/vectors/`, `references/scenarios/`, the conformance gateway (to build, in Go, in `sdk/go/conformance`) and each client's runner (beside the client).

## Requirements

- **AP-CONF-001.** Every client's CI runs the vectors and the scenarios of its levels on every change.
- **AP-CONF-002.** The conformance gateway and the vectors are versioned with the protocol, and a protocol minor version adds cases without changing existing ones.

## Decisions

**Open:**

- Where the conformance gateway is built and in which language. Recommended: Go, in `sdk/go/conformance`, so the CLI, the Go SDK and the conformance gateway share `sdk/go/protocol`.
- The four vector files still to write, and the resource, action and event profile vectors. Recommended: before any client is changed to follow §9.
