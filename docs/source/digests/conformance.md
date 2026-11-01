# Digest: conformance (§9.8) and the protocol references

> Historical engineering study (2026-10-31). Retained for its requirements, counterexamples and design reasoning. Implementation observations and line references describe that review snapshot; use [current progress](../../HANDOFF-PROGRESS.md) and the named source symbols for current status. Paths are relative to the repository root unless a citation key says otherwise.

Reader: conformance and the protocol references. Written 2026-10-31.

## 0. Sources, citation keys, verdict

| Key | File |
|---|---|
| `§9.8:N` | `docs/source/atlas-spec/09-8-conformance.md`, line N (172 lines, read in full) |
| `RD:N` | `spec/protocol/references/README.md`, line N (206 lines, read in full) |
| `Q:N` | `spec/protocol/references/QUESTIONS.md`, line N (157 lines, read in full) |
| `<ID>` | `spec/protocol/references/scenarios/<ID>.json` (all 50, read in full) |
| `GW` | `.../references/scenarios/fixtures/gateway.json` (5611 lines, read in full; every member, all 35 profiles). In my reading the 81 `$schema`/`$vocabulary` pairs were collapsed after I checked they are all identical (see 2.5) |
| `GV` | `.../references/vectors/guard.json` (65 cases, read in full) |
| `GPY:N` | `tools/spec/atlasref/guard.py` (160 lines, read in full: the reference guard) |
| `REV:N` | `docs/pass-2/REVIEW.md` (100 lines, read in full) |
| `AG:N` | `AGENTS.md` (72 lines, read in full) |
| `AC:N` | `docs/source/atlas-spec/review/ATLAS-CORE.md` (lines 99–164 and 160–245 read, for scope checks) |
| `DR:N` | `docs/source/atlas-spec/review/DESIGN-REVIEW.md` (lines 28–190 read, for R findings) |

Markers: **IN SCOPE**, **DEFERRED**, **AMBIGUOUS:**, **OVERRIDDEN BY REVIEW (Rnn):**, **MISSING:**.

**Verdict in one paragraph.** The references give 50 scenarios (33 Core, 4 Streaming, 3 Columnar, 3 Bulk, 4 Events, 3 Handoff; RD:206) and a fixture gateway of 35 profiles (20 conformance, 15 `atlas.*`). The first build needs 25 of the 33 Core scenarios outright and all 4 Streaming scenarios: 29 IN SCOPE, plus 4 whose scope is AMBIGUOUS. Five scenarios conflict with the review: R03 (CORE-RES-001, CORE-RES-003, COL-003), R14 (CORE-ENV-001) and R06/R08 (CORE-CONFIRM-001). R11/R26 change what the sign-in, cache and download scenarios may assert about files. BULK, EVT and HAND are deferred. **§9.8.5 says no case may be skipped at a level** (§9.8:140). So the first build cannot honestly claim "Core" while any Core scenario is deferred. **The fixtures have no identities or scopes**: the two-identity acceptance test (REV:30, REV:48) has no fixture or scenario yet.

---

## 1. How conformance is meant to run

### 1.1 Two kinds of test (§9.8:11–16)

| Kind | Tests | How |
|---|---|---|
| Vectors | "The pure algorithms: grammars, schema validation, fingerprints, resolution, header parsing, Arrow schema derivation" | "Each implementation reads `references/vectors/*.json` in its own unit tests and checks every case" |
| Scenarios | "Behaviour over a connection: the call algorithm, deliveries, problems, retries, deadlines, subscriptions, transfers, sign-in" | "The suite runs a conformance gateway and drives the client through the runner protocol (9.8.3)" |

Vector file shape (§9.8:33, RD:63): I-JSON `{ "description", "cases": [ { "name", ...inputs, "expected" | "problem": { "code", "target"? } } ] }`. Numbers compare as binary64. Member order carries no meaning, **except** a schema's `properties`, whose order is the Arrow field order and §9.5.2's matching order (Q-ARROW-1). "read them keeping member order" (RD:63).

### 1.2 The suite's run of one scenario (RD:90; §9.8:57–59)

1. Start the conformance gateway (one process; four listeners `{gateway}`..`{gateway4}`, Q-SCN-2, Q:127). For the Events level, also start the Service Bus emulator beside it (§9.8:59).
2. Load the scenario: `POST /_conformance/scenario {"scenario": "<id>"}` (§9.8:59, RD:90).
3. Prepare the client's environment from `setup` (env, files, cwd).
4. Start the client's runner with env `ATLAS_URL` (the conformance gateway), `ATLAS_HOME` (a fresh directory) and `ATLAS_CONFORMANCE=1` (§9.8:37). Add or unset whatever `setup.env` says (RD:107).
5. Send `commands` **one line at a time** (RD:90). Compare each result line with `expected`, matched by `id`.
6. Finally, `GET /_conformance/requests` and check it against `expected_requests` (RD:90).
7. `POST /_conformance/reset` "clears both" (the script and the recording) (§9.8:59).

The control interface is "on loopback only, which no client ever calls" (§9.8:59). `note` and `questions` members "explain a scenario; they are not checked" (RD:90).

What the gateway records and the suite checks (§9.8:57): "headers present and well formed, idempotency keys reused on retry, deadlines decreasing, bodies valid".

### 1.3 The runner protocol (§9.8:35–53)

- A runner is "a small program in its own language that reads commands as JSON lines on standard input and writes one JSON line per result on standard output, calling only the client's public interface (9.7.1)" (§9.8:37).
- Command line: `{ "id": 7, "op": "call", "args": { "address": "conformance.rows@v1:fetch", "request": { "n": 3 }, "options": { "deadline_ms": 2000, "accept": ["application/x-ndjson"] } } }` (§9.8:42).
- Success line: `{ "id": 7, "ok": true, "value": [ { "i": 0 }, { "i": 1 }, { "i": 2 } ], "warnings": [], "calls": 1 }` (§9.8:48). Q-SCN-4 (Q:129) treats the example's one-column rows as abbreviated; the real rows have six columns.
  - **AMBIGUOUS:** the result member `calls` (§9.8:48) is defined nowhere: not in §9.8, not in RD, not in any scenario. Readings: (a) the number of HTTP requests the call made; (b) the number of source executions (compare R11's "Count source executions when reading a result twice", DR:124); (c) leftover example text. No scenario checks it.
- Failure line: `{ "id": 7, "ok": false, "problem": { …the problem document… } }` (§9.8:51). A failed result may also carry `value`: the rows read before the failure (Q-RUN-6, Q:123; STREAM-003).
- `op` is one of `connect`, `whoami`, `catalog`, `search`, `describe`, `access`, `call`, `fetch`, `operation.get`, `operation.wait`, `operation.cancel`, `subscribe.open`, `subscribe.next`, `subscribe.close`, `publish`, `export`, `upload`, or `version` ("the runner answers its client's name, version, protocol and levels") (§9.8:53). The references add `guide` and `explain` (Q-RUN-1, Q:118; RD:113).
- Value encoding (§9.8:53): values as JSON. Arrow results as "arrays of row records (decimals as their strings, timestamps as RFC 3339)". Bytes as base64. A date-time is "RFC 3339 in UTC with `Z` and exactly six fractional digits" (Q-TIME-1, Q:34).
- The Go SDK's runner "is also the Python SDK's engine, which adds Arrow results on a second pipe and `cancel` for its own face (§5.1); the suite uses only the JSON form" (§9.8:53).
- `options` may carry `person: true` ("a person is at the client") and `confirm: "accept"` or `"decline"` (§9.8:53).
- Warnings: "Warnings the client emitted (deprecations, truncation) are returned in `warnings`, each `{ "code", "text" }`" (§9.8:53). A deprecation warning's code is `deprecated` (Q-RUN-3, Q:120).
- "The runner adds no logic: it translates commands into calls on the client and results into lines" (§9.8:53). So the suite, not the runner, handles `setup` and the `suite.*` commands (Q-SCN-1, Q:126).

### 1.4 Command vocabulary: every op and its args as the 50 scenarios use them

Counts are uses across all scenarios. The arg keys come from all command lines.

| op | Uses | Args used | Notes / where |
|---|---|---|---|
| `connect` | 55 | `{}` or `{"url": "{gateway}"}` | `url` only in CORE-BOOT-002 |
| `whoami` | 14 | `{}` | value has `environment` (CORE-BOOT-001) |
| `call` | 75 | `address`, `request`, `options` | `request` may be a placeholder string `"{value:2}"` (CORE-ENV-001) |
| `catalog` | 1 | `domain` | CORE-DISC-001 `{"domain":"conformance"}`; the value is iterated (`$count`) |
| `describe` | 2 | `id` | the value has `profile` (CORE-DISC-001) |
| `guide` | 1 | `id` | CORE-GUIDE-001 |
| `explain` | 2 | `id`, `request` | CORE-EXPLAIN-001 |
| `export` | 2 | `id`, `request`, `format`, `path` | BULK-001/003 `"format":"parquet"`, `"path":"{WORKDIR}/export/"` |
| `upload` | 1 | `address`, `request`, `source` | BULK-002 |
| `operation.get` | 1 | `name` | CORE-OP-001 |
| `operation.wait` | 2 | `name`, `timeout_ms` | `timeout_ms: 30000`; returns the operation's **result** (Q-RUN-5) |
| `operation.cancel` | 1 | `name` | |
| `subscribe.open` | 3 | `id` | the subscription is named by the command's id (Q-RUN-7) |
| `subscribe.next` | 6 | `subscription`, optional `timeout_ms` | `{"none": true}` when nothing arrives within the timeout (Q-RUN-7, Q:124) |
| `subscribe.close` | 1 | `subscription` | |
| `publish` | 2 | `id`, `payload` | EVT-002 |
| `suite.restart` | 4 | `{}` or `env` (map; `null` unsets) | "stop the runner and start it again, with `env` changes; ATLAS_HOME is kept" (RD:115) |
| `suite.file` | 1 | `path`, `size`, `pattern` (or content) | RD:116; BULK-002 `size: 20971520`, `pattern: "0123456789abcdef"` |
| `suite.expect_file` | 7 | `path` + one of `absent`, `base64`, `json_contains`, `mode`, `dir_mode` | "`*` matches one path segment" (RD:117) |

Never used by any scenario: `search`, `access`, `fetch`, `version` (§9.8:53 lists them). **MISSING:** no scenario drives `search` (the journey has `portfolio.search@v1` and the authorised catalogue/search, AC:238), `access` or `version`.

`call` options used (counts): `accept` 17, `execution` 4, `deadline_ms` 3, `person` 2, `confirm` 2, `max_memory` 2, `path` 1, `if_match` 1, `fields` 1, `set` 1. Beyond §9.8.3: `path`, `max_memory`, `replace` (§9.7.13), `if_match`, `fields`, `set` (Q-RUN-4, Q:121; RD:121). `execution` (`"handoff"`/`"gateway"`, HAND-*) is in neither RD:121 nor Q-RUN-4. **AMBIGUOUS:** `execution` is used as an option but never defined.

Delivery results (RD:121; Q-RUN-5, Q:122):
- `call` on an **operation** delivery returns the operation document. `operation.wait` returns its result.
- `call` on a **transfer** delivery waits and returns what landed: `{"content": {"base64", "metadata"}}` in memory, or `{"manifest": {"files": [...]}}` on disk.
- The end line's `usage` is reported in the result as `usage`.

### 1.5 Placeholders (RD:92–103)

| Placeholder | Means |
|---|---|
| `{gateway}`; `{gateway2}`..`{gateway4}` | Base URL; the further listeners for CORE-BOOT-002 |
| `{ATLAS_HOME}`, `{WORKDIR}` | The fresh ATLAS_HOME; the fresh directory the runner starts in |
| `{today}` | UTC date `YYYY-MM-DD` |
| `{operation:N}` | The operation name command N returned |
| `{value:N}`, `{value:N/<pointer>}` | Command N's value, or a member of it |
| `{sha256:<path>}` | A file's SHA-256 |
| `{vectors:<file>#<case>}` | "that vector case's expected rows, every batch in order" |
| `{"$now_plus_s": n}` | epoch seconds |
| `{"$now_plus_iso": n}` | RFC 3339 |

### 1.6 `setup` (RD:105–109)

- `env`: the runner's environment (ATLAS_URL, ATLAS_HOME, ATLAS_CONFORMANCE=1, and what the scenario adds); `null` unsets.
- `files`: written before the runner starts: `path`, `mode` (and `dir_mode` for its directory), with `json`, `text` or `base64` content.
- `cwd`: the runner's working directory.

Standard setup in 49 of 50 scenarios: `{ATLAS_HOME}/state/token-conformance.json`, `mode` `"0600"`, `dir_mode` `"0700"`, `json` `{"access_token":"ct-valid","expires_at":{"$now_plus_s":3600},"refresh_token":"rt-valid"}`. The deviations:

| Scenario | Token file |
|---|---|
| CORE-SIGNIN-001 | `ct-from-file` |
| CORE-SIGNIN-002 | `ct-old`, expires `$now_plus_s` 30, refresh `rt-1` |
| CORE-SIGNIN-005 | mode `"0644"` |
| EVT-003 | the file is `token-dv.json` |

Token file shape: `{access_token, expires_at (epoch seconds), refresh_token}`.

### 1.7 `expected`: result format and matching rules (RD:123–131)

| Form | Rule |
|---|---|
| `{"id", "ok": true}` | Matches any success |
| `value` | "must equal the result's value" (exact) |
| `value_contains` | "must be contained in it: objects by the members given, arrays element by element". Special members: `{"$count": n}` (items yielded), `{"$length": {"/pointer": n}}`, `{"$in": [...]}`, `{"$present": true}`, `{"$uuid": true}` |
| `warnings` | "each `{code, text_contains}` must match one the client emitted" |
| `usage` | "the end line's usage" |
| `{"id", "ok": false, "problem": {...}}` | "the problem must contain the members given (`code`, `target`, `details`, ...)". A CLI runner reports `cli_exit` (Q-RUN-2) |

Comparison (RD:131): "Values compare as JSON with numbers as binary64. Date-times compare by instant to the microsecond, and decimals by value at their scale."

Every scenario has exactly one expected result per command (RD:206). Results come back in command order: commands are sent "one line at a time" (RD:90).

- **AMBIGUOUS:** "arrays element by element" (RD:126). Readings: (a) positional, with expected length = actual length; (b) positional prefix, actual may be longer; (c) each expected element matches some actual element. CORE-ENV-001 expects `conditions:[{"type":"ready"},{"type":"quantum",...}]` against the fixture's two conditions in the same order, which fits (a) and (b).
- **AMBIGUOUS:** how the comparer knows a string is a date-time or a decimal (RD:131). Readings: (a) from the reader profile's schema (`format: date-time`, `x-atlas-logical: Decimal(p,s)`); (b) by pattern-sniffing strings. Also "decimals by value at their scale": (a) equal value **and** equal scale (`"1.500000"` ≠ `"1.5"`); (b) equal after rescaling to the schema's scale.
- **AMBIGUOUS:** `text_contains` is a JSON **array** of substrings in CORE-DEPR-001 (`["conformance.evolving@v1","2027-01-31","conformance.evolving@v2"]`), but RD:127 writes it as if singular. Reading: every listed substring must appear in the warning's `text`.
- `cli_exit` is checked "only for a CLI" (Q-RUN-2, Q:119).

### 1.8 `expected_requests`: assertions on the recording (RD:149–168)

Selector keys (RD:151): `address | path | method | nth | body_contains | header_equals | listener | storage_* | path_prefix | path_suffix`. Used: `address` 111, `body_contains` 44, `nth` 18, `method` 13, `path` 11, `path_suffix` 5, `listener` 4, `storage_part` 4, `header_equals` 2, `path_prefix` 2, `storage_upload` 2, `storage_chunk` 1.

| assert | Holds when (RD:155–168) | Uses |
|---|---|---|
| `count` | `equals` requests match `select` | 70 |
| `header` | on the selected request, `name` is `absent`, or `parses` as its §9.6.3 type (headers.json's reading), or its Atlas reading `equals` a value, or `contains` a substring | 40 |
| `body` | the body `equals` (as JSON), `contains`, has the RFC 8785 form `jcs`, or (a form body) `form_contains` | 5 |
| `body_member_in` | the body's `member` (a pointer) is one of `values` | 1 |
| `same_header` | header `name` is the same on every request of `selects` | 3 |
| `interval` | at least `min_ms` passed between request `from` and request `to` | 3 |
| `order` | `selects` arrived in that order | 1 |
| `deadline` | Atlas-Deadline minus the request's arrival is between `min_s` and `max_s` | 3 |
| `describe_exact` | atlas.describe was asked for `exact` `count` times | 3 |
| `poll_intervals` | the polls of `operation` came at least `min_ms[i]` after the previous answer | 1 |
| `cursor_chain` | each selected request carries the `next_cursor` of the previous answer | 1 |
| `max_body` | every selected request's body is at most `bytes` | 1 |
| `header_features` | Atlas-Client-Features holds the tokens given | 1 |
| `bus`, `bus_connections` | the emulator received `count` events containing `contains` (with `uuid`, `recent` and `equals_client_name` pointers), or this many connections | 1 + 1 |

- **AMBIGUOUS:** HAND-001 uses `header` with `"present": true` (`x-ms-encryption-key-sha256`), a form RD:156 does not list. Reading: the header is sent with any value. (Deferred scenario.)
- **AMBIGUOUS:** `header_features` takes `"Atlas-Client-Features": {"levels_contains": [...], "handoff_contains": [...]}` (HAND-001). The member names are not defined in RD:167.

### 1.9 `gateway`: the scenario's script (RD:133–147)

All members are optional. "anything not scripted is served by the default behaviour" (RD:135).

| Member | Meaning |
|---|---|
| `bootstrap` | deep-merged into the fixtures' bootstrap |
| `current` | which profile a pinned id is served under |
| `serve` | further fixture profiles to serve |
| `tokens` | `{access: [accepted tokens], refresh: {refresh_token: {access_token, refresh_token, expires_in}}}`, **replacing** the fixtures' |
| `headers` | extra response headers, by request path or by address |
| `state` | initial state for a capability: `things`, `files`, `catalog_page_size`, `export_parts`, `handoff_case` |
| `steps` | response overrides, tried in order. Each is `{match, respond, times}` (`times` default 1) |
| `bus` | `{after: "connected" \| "delivered:<event id>", send: [CloudEvents]}` or `{after, revoke: true}` |

- `match` keys (RD:143): `address` (an interaction's), `path`, `method`, `nth` ("the request's ordinal among those to that address or path, from 1"), `body_contains`, `header_equals`, `listener`, `storage_part` k, `storage_chunk` k, `storage_upload`.
- `respond` (RD:144) takes `status`, `headers`, and one of:
  - `problem`: members; "the gateway fills type, status, request_id and next";
  - `body`: JSON, or null for none;
  - `serve: true`: the default behaviour;
  - `stream`: `encoding` ndjson or arrow, `rows`, and `end` (an end line, `"default"`, or null for none);
  - `describe_profile`: atlas.describe answering this profile document;
  - `profile`: answer under this writer exact-id.
- Modifiers (RD:145): `delay_ms`; `expire_links`; `before` (`set_status` on a thing); `deleting`.

Ambiguities in the script format:
- **AMBIGUOUS: `nth` with `times` > 1.** CORE-SIGNIN-003 has `{"match":{"address":"atlas.whoami:fetch","nth":3}, ..., "times":2}` and expects whoami requests 3 **and 4** both refused. Requests 3 and 4 are both refused, so the step must also match request 4. Readings: (a) `nth: k` matches ordinal ≥ k until `times` is used up; (b) a step with `nth` matches only ordinal k, and `times` > 1 is then meaningless. The scenario only works under (a). Likewise CORE-UPGRADE-001 and EVT-004 (`nth:2`, `times:5`).
- **AMBIGUOUS: does `nth` count all requests to the address, or only those that also match `body_contains`?** CORE-RETRY-001 combines `nth: 1/2` with `body_contains {"text":"x"}`. The "x" call is the first, so both readings give the same answer there. CORE-CONFIRM-001's selector `{"nth":2,"body_contains":{"text":"c"}}` only works if `nth` counts among the requests matching the body (c's are the 3rd and 4th costly requests overall).
- **AMBIGUOUS:** is `body_contains` subset-equality of JSON members, or a substring match? Every use is `{"text": "<value>"}`: the registry codes in CORE-PROB-001, and single letters elsewhere. None of these values is a substring of another in the same scenario, so no scenario distinguishes the readings. Reading taken: subset of members, each member equal (as `value_contains` for objects, RD:126).

### 1.10 Passing, new languages, rules (§9.8:138–165)

- A client conforms at a level "when it passes every vector and every scenario of that level and of every level it requires, against the conformance gateway of the protocol version it declares, **with no case skipped**" (§9.8:140).
- "The gateway conforms when it passes the gateway suite (the same vectors, and scenarios driven by a reference runner against it)" (§9.8:140).
- "Results are published per client release: its name, version, protocol, levels and pass or fail per case" (§9.8:140).
- A new language's client is accepted when (§9.8:144–149):
  1. it declares its levels;
  2. it passes the vectors and scenarios of those levels;
  3. its runner is committed beside it and runs in its CI against the conformance gateway;
  4. its README names the levels it conforms to and the protocol version.
- Rules (§9.8:155–156): "No client or the gateway is released without passing the suite at its declared levels." "A defect found in one client that the suite did not catch adds a vector or a scenario before it is fixed."
- **AP-CONF-001.** "Every client's CI runs the vectors and the scenarios of its levels on every change." (§9.8:164)
- **AP-CONF-002.** "The conformance gateway and the vectors are versioned with the protocol, and a protocol minor version adds cases without changing existing ones." (§9.8:165)
- **AMBIGUOUS / scope conflict:** §9.8:140 forbids skipping a case at a level, but REV/R03/R06/R14 defer or override several Core scenarios (section 3). Readings: (a) the first build declares no Core level yet and reports per-case results; (b) the deferred Core scenarios are amended in the references (Q entries) to the review's model before the claim. AG:7 says "Where the spec is ambiguous, write the case down; don't guess". The decision belongs to the product owner.

### 1.11 The gateway suite (§9.8:136)

- "a reference runner (the Go SDK's) drives the real gateway, which mounts the same `conformance.*` capabilities (the package `sdk/go/conformance`, written once) only where its environment is `local` or `conformance`, never in Development, Acceptance or Production."
- It adds, for the gateway alone:
  - every document it sends validates (**AP-DOC-001**);
  - the built-ins match 9.3.2 exactly (**AP-DOC-002**);
  - "a client lacking a lens operation is answered already resolved (§9.5.6)";
  - "a client declaring no features is served correctly (**ATLAS-VERSION-005**)";
  - "an invalid request from a caller without the grant is `not_granted`, and from one with it `invalid_request` with no limit reserved (**ATLAS-ACCESS-009, 010**)".
- **IN SCOPE:** AP-DOC-001, AP-DOC-002, ATLAS-VERSION-005, ATLAS-ACCESS-009/010.
- **OVERRIDDEN BY REVIEW (R03):** the §9.5.6 item (gateway resolving for a lens-less client) now rests on the experimental resolver. "A client is obliged only to aliases, defaults, UNKNOWN and projection. Its vectors still pass, but no served profile may depend on `convert_unit`, `promote` or `drop` within a version" (REV:90).
- Location: §9.8:57 and §9.8:171 recommend Go, in `sdk/go/conformance`, on `sdk/go/protocol` (still listed as an open decision, §9.8:171). Repo today: `sdk/go/protocol` exists; there is no `sdk/go/conformance` yet.

### 1.12 How a client runner is meant to be written: Go, CLI, Python, Node

What the sources fix:
- The runner is in the client's own language. It calls only the public interface. It adds no logic. Each command gives exactly one result line (§9.8:37, §9.8:53).
- It reads env `ATLAS_URL`, `ATLAS_HOME`, `ATLAS_CONFORMANCE=1` (§9.8:37).
- It is committed beside its client and runs in that client's CI (§9.8:148).
- Go: "The Go SDK's runner is also the Python SDK's engine" (§9.8:53). The Go SDK's runner is the reference runner for the gateway suite (§9.8:136).
- CLI: on failure, the result carries `cli_exit`, the §9.7.6 exit code (Q-RUN-2). The CLI exit codes are in `vectors/problems.json` (table 1.13).
- Python: the face over the Go engine. "A subprocess is an implementation choice behind Python's public contract" (AC:140).
- Node: "The TypeScript implementation must pass the same semantic fixtures" (AC:140). Keep "Independent Go and TypeScript conformance" (DR:24).
- **R11 acceptance** (DR:124): "Compare identity, canonical requests, values, errors, deadlines, completeness and usage across CLI/Python/Node. Count source executions when reading a result twice. A browser client needs neither a token file nor local disk output."
- Parity "does not require every runtime to use the same in-memory container or to write files at the same location" (AC:136). **OVERRIDDEN BY REVIEW (R11)** for every scenario that asserts a file layout (section 3).

Not fixed (each **AMBIGUOUS:**):
- **CLI runner shape.** The CLI is a process per invocation. Readings: (a) the runner spawns `atlas …` once per command and keeps state (bootstrap cache, token, profile cache) only through `ATLAS_HOME`/cwd; `connect` is then a no-op or `atlas connect`; (b) the CLI has a long-lived mode the runner drives. Under (a), CORE-DISC-002's "served from the cache, in memory" (2 calls, one describe) holds only through the disk cache.
- **Python runner.** Readings: (a) a Python program over the Python public face (`FIAtlas().call(...)`), which really tests the face; (b) reusing the Go engine's runner, which tests only Go. §9.8:148 ("its runner is committed beside it") and AC:140 favour (a).
- **Node runner.** TypeScript over `@atlas/sdk` (AC:121–127): `atlas.connect()`, `client.call(address, request)`. Credentials come from "each runtime's own provider" (REV:69, R11). So a Node runner may not get its token from `token-conformance.json`, and CORE-SIGNIN-001/002/005 do not apply to a browser runtime.
- **The suite's language and home** are not stated. AG:3 lists "the conformance suite" as part of this Go repo.

### 1.13 Problem registry and CLI exit codes (vectors/problems.json, 25 cases; also asserted by CORE-PROB-001)

| code | status | retryable | cli_exit |
|---|---|---|---|
| `invalid_document` | 400 | false | 1 |
| `invalid_request` | 400 | false | 2 |
| `unauthenticated` | 401 | false | 3 |
| `not_granted` | 403 | false | 4 |
| `denied` | 403 | false | 4 |
| `budget_exceeded` | 403 | false | 4 |
| `approval_required` | 403 | false | 4 |
| `confirmation_required` | 428 | false | 4 |
| `not_found` | 404 | false | 5 |
| `not_in_environment` | 404 | false | 5 |
| `idempotency_conflict` | 409 | false | 1 |
| `in_progress` | 409 | true | 6 |
| `resume_gap` | 409 | false | 1 |
| `retired` | 410 | false | 5 |
| `precondition_failed` | 412 | false | 1 |
| `critical_trait_unsupported` | 422 | false | 8 |
| `resolution_failed` | 422 | false | 8 |
| `upgrade_required` | 426 | false | 8 |
| `limit_exceeded` | 429 | true | 6 |
| `output_limit_exceeded` | 200 | false | 1 |
| `internal` | 500 | false | 1 |
| `upstream_failed` | 502 | true | 1 |
| `unavailable` | 503 | true | 6 |
| `deadline_exceeded` | 504 | false | 7 |
| unknown code (e.g. `warehouse_suspended`) | its own | its own | 1 |

- Type URI: `https://atlas.jhg.com/problems/<code>`.
- "output_limit_exceeded travels with status 200, in an end line or final frame" (problems.json description).
- `invalid_request` is also sent with 406 (§9.6.4). `retryable` can be false for a write that cannot safely repeat (Q-DOC-8, Q:63). So the schema does not enforce status or retryable.

---

## 2. The fixture gateway (`scenarios/fixtures/gateway.json`)

Top-level members: `description`, `current`, `bootstrap`, `tokens`, `profiles` (GW).

GW `description`: "What every scenario's conformance gateway serves unless its script says otherwise: every profile by exact-id (the conformance capabilities, the variants scenarios name, and the atlas.* built-ins), which profile is current for each pinned id, and the bootstrap, with {gateway} standing for the gateway's own URL."

### 2.1 Environment and paths

| Item | Value | Source |
|---|---|---|
| Environment name | `conformance` | GW bootstrap.environment; §9.8:57 |
| Mounted only where | env is `local` or `conformance` | §9.8:136 |
| Bootstrap path | `/.well-known/atlas` (GET, no token, ETag; 304 to `If-None-Match`) | CORE-BOOT-001, CORE-SIGNIN-001 |
| Built-in bindings | `POST /v1/atlas/<id>:<interaction>` | GW bootstrap.builtins |
| Data / resource / action / event bindings | `POST /v1/data/<id>@v<N>:<i>`, `/v1/resource/...`, `/v1/action/...`, `/v1/event/...` | GW profiles |
| Token endpoint | `POST {gateway}/oauth/token`, form body `grant_type=refresh_token&refresh_token=...` | CORE-SIGNIN-002 |
| Operation result | `GET /v1/operations/<…>/result`, with `Authorization: Bearer` | CORE-OP-001 (`path_prefix` `/v1/operations/`, `path_suffix` `/result`) |
| Cancel | `POST …:cancel` (atlas.operation:cancel binding `/v1/atlas/atlas.operation:cancel`) | CORE-OP-001 |
| Transfer commit | `POST …:commit` | BULK-002 |
| Storage | `{gateway}/storage/...` (export parts, upload parts, handoff chunks) | RD:178 |
| Control | `POST /_conformance/scenario`, `GET /_conformance/requests`, `POST /_conformance/reset`, loopback only | §9.8:59 |
| Listeners | `{gateway}`..`{gateway4}`; each serves its own bootstrap, environment `conformance` | Q-SCN-2; CORE-BOOT-002 note |

**AMBIGUOUS:** the operation document's own polling address (where `atlas.operation:get` and the `/v1/operations/<id>` polls go) is not written in the fixtures. CORE-OP-001 only fixes the `/result` GET and `:cancel`. Reading: polls go through `atlas.operation:get` at `/v1/atlas/atlas.operation:get`, as BULK-003 and HAND-002 count `atlas.operation:get`.

### 2.2 Bootstrap (GW `bootstrap`, verbatim members)

- `protocols`: `["atlas/1.0"]`; `minimum_protocol`: `"atlas/1.0"`; `environment`: `"conformance"`
- `servers`: `{"gateway": {"url": "{gateway}", "description": "The conformance gateway."}}`
- `builtins`: 15 entries, each `{"version": 1, "interactions": {...: {"bindings": [{"protocol":"https","server":"gateway","method":"POST","address":"/v1/atlas/<id>:<i>","encodings":["json"],"auth":"bearer"}]}}}`. The interactions per built-in:

| Built-in | Interactions |
|---|---|
| `atlas.whoami` | fetch |
| `atlas.catalog` | fetch |
| `atlas.search` | fetch |
| `atlas.describe` | fetch |
| `atlas.guide` | fetch |
| `atlas.access` | fetch |
| `atlas.usage` | fetch |
| `atlas.setup` | fetch |
| `atlas.clients` | fetch |
| `atlas.compare` | fetch |
| `atlas.operation` | get, list, cancel, watch |
| `atlas.catalog.changed` | subscribe |
| `atlas.settings` | get, update |
| `atlas.directory` | fetch |
| `atlas.audit` | fetch |

- `sign_in`:
  - `authority` `"{gateway}/oauth"`
  - `client_id` `"atlas-conformance"`
  - `scopes` `["{gateway}/.default"]`
  - `token_endpoint` `"{gateway}/oauth/token"`
  - `authorization_endpoint` `"{gateway}/oauth/authorize"`
  - `device_authorization_endpoint` `"{gateway}/oauth/device"`
  - Q-DOC-2 (Q:57): the bootstrap names every endpoint, so a client builds none.
- `catalog_etag`: `"\"catalog-1\""`
- **OVERRIDDEN BY REVIEW (R26):** credentials must be "bound to the trusted gateway, authority, audience, tenant or account, and scopes. Refresh is serialised, and an unauthenticated bootstrap cannot redirect a credential" (REV:84; DR:153). The fixture's design has an unauthenticated bootstrap naming the authority, with one token file keyed by `environment` (Q-CLI-1). That is the pattern R26 rejects as a trust root.

### 2.3 Identities and tokens

- GW `tokens`: `{"access": ["ct-valid", "ct-refreshed"], "refresh": {"rt-valid": {"access_token": "ct-refreshed", "refresh_token": "rt-next", "expires_in": 3600}}}`.
- A scenario's `tokens` **replaces** these (RD:139):

| Scenario | `tokens` |
|---|---|
| CORE-SIGNIN-001 | access `["ct-from-file"]`, refresh `{}` |
| CORE-SIGNIN-002 | access `["ct-new"]`, `rt-1` → `{ct-new, rt-2, 3600}` |
| CORE-SIGNIN-003 | access `["ct-valid","ct-refreshed"]`, `rt-valid` → `{ct-refreshed, rt-2, 3600}`, `rt-2` → `{ct-refreshed, rt-3, 3600}` |

- `whoami` answers with at least `environment: "conformance"` (CORE-BOOT-001).
- **MISSING:** the fixtures have **no identities, principals, groups, grants or data scopes**: tokens only. REV:30 requires "Fixtures cover two identities with different portfolio scope". Acceptance item 3 (REV:48) and R05 (REV:63) need a narrower identity that gets a narrower result or a clear refusal. The gateway-suite items ATLAS-ACCESS-009/010 (§9.8:136) also need a caller without the grant. None exists in GW.

### 2.4 `current`: the profile served for each pinned id (GW)

| Pinned id | Current exact-id |
|---|---|
| `conformance.echo@v1` | `conformance.echo@v1#f17339273f0617c2` |
| `conformance.rows@v1` | `conformance.rows@v1#c121b005057331c7` |
| `conformance.items@v1` | `conformance.items@v1#05354431b1d8a446` |
| `conformance.slow@v1` | `conformance.slow@v1#739867d227780756` |
| `conformance.retired@v1` | `conformance.retired@v1#6d24f084a2a2d7a5` |
| `conformance.critical@v1` | `conformance.critical@v1#4274db333936328d` |
| `conformance.costly@v1` | `conformance.costly@v1#bf862d27e2fe69aa` |
| `conformance.dynamic@v1` | `conformance.dynamic@v1#e401fa5670a50d55` |
| `conformance.thing@v1` | `conformance.thing@v1#677bf5325b4e72c3` |
| `conformance.file@v1` | `conformance.file@v1#364cf4144dd8837e` |
| `conformance.change@v1` | `conformance.change@v1#b04dbbdc02bb6ba4` |
| `conformance.evolving@v1` | `conformance.evolving@v1#73def445ee90c57a` (#C) |
| `conformance.evolving@v2` | `conformance.evolving@v2#eb84229aaeb7ca6b` |
| `conformance.handoff@v1` | `conformance.handoff@v1#f06c3819d34aadf7` |
| `conformance.tick@v1` | `conformance.tick@v1#61948f2a0e7d7132` (#B) |

Extra served variants (not current):
- `conformance.echo@v1#d0a02ef751a8d8e7` (CORE-CALL-003)
- `conformance.echo@v1#4fe7738a97223902` (CORE-TOL-001)
- `conformance.evolving@v1#bc7fde7c0bac077d` (#A) and `#5f4ce7e7661ebd4c` (#B)
- `conformance.tick@v1#1acba4963d7b5097` (#A)

Built-in exact-ids:

| Built-in | Exact-id |
|---|---|
| `atlas.access` | `atlas.access@v1#2eb8103a0126e827` |
| `atlas.audit` | `atlas.audit@v1#cbab892faa6bfca3` |
| `atlas.catalog.changed` | `atlas.catalog.changed@v1#846839fbe8181da1` |
| `atlas.catalog` | `atlas.catalog@v1#9f65e429000acafd` |
| `atlas.clients` | `atlas.clients@v1#9be2fe1c3dfb3c45` |
| `atlas.compare` | `atlas.compare@v1#7f42197359868c95` |
| `atlas.describe` | `atlas.describe@v1#ff036976dde88885` |
| `atlas.directory` | `atlas.directory@v1#f8b2b83fb6a9a756` |
| `atlas.guide` | `atlas.guide@v1#75b4df7b1f716f29` |
| `atlas.operation` | `atlas.operation@v1#554cc68c9eafdb0a` |
| `atlas.search` | `atlas.search@v1#2443a0bd8f2e7eb0` |
| `atlas.settings` | `atlas.settings@v1#702da574e3c4210f` |
| `atlas.setup` | `atlas.setup@v1#43c899648a357c51` |
| `atlas.usage` | `atlas.usage@v1#b26ce11aa113a4cd` |
| `atlas.whoami` | `atlas.whoami@v1#76e4cb9bb2dc565d` |

### 2.5 Facts common to every fixture profile (GW)

- Every schema has `$schema` `https://json-schema.org/draft/2020-12/schema` and the same `$vocabulary` (81 occurrences, one distinct value):
  - `https://json-schema.org/draft/2020-12/vocab/core`: true
  - `https://json-schema.org/draft/2020-12/vocab/applicator`: true
  - `https://json-schema.org/draft/2020-12/vocab/validation`: true
  - `https://json-schema.org/draft/2020-12/vocab/meta-data`: true
  - `https://json-schema.org/draft/2020-12/vocab/format-assertion`: true
  - `https://atlas.jhg.com/vocab/2026-10`: false
- Conformance profiles: `identity.domain` `"conformance"`, `owner` `"Atlas"`. Each binding: `protocol "https"`, `server "gateway"`, `method "POST"`, `auth "bearer"`. `errors: []`.
- Built-in profiles:
  - `identity.summary` "The built-in <id> (§9.3.2)." and `domain "atlas"`;
  - schemas are `$ref`s to `https://atlas.jhg.com/protocol/1.0/builtins.schema.json#/$defs/<name>`, or to `catalog-page.schema.json`, `describe.schema.json`, `operation.schema.json`;
  - deadlines `PT30S`/`PT2M`;
  - `meaning.grain "fixed by atlas/1.0"`, `source "the gateway"` (operation and settings: `meaning.name "fixed by atlas/1.0"`).
- Built-in profiles are exempt from the profile lint (Q-PROF-9, Q:14).

### 2.6 Every conformance capability: interactions, schemas, behaviour, knobs

Default behaviour is designed in RD:174–190 (Q-SCN-3, Q:128). "each answer under the current profile" (RD:172).

#### `conformance.echo@v1` (data; current #f17339273f0617c2)

- Purpose (§9.8:65): "Validation, headers, the identity resolution, the tolerant reader".
- `fetch`:
  - effect read, result record, delivery single, execution ["gateway"];
  - takes `request`, returns `result`;
  - deadline default `PT30S`, max `PT2M`;
  - binding `/v1/data/conformance.echo@v1:fetch`, encodings ["json"].
- `request` (closed, required ["text"]):

  | Field | Schema |
  |---|---|
  | `text` | string, minLength 1, maxLength 200 |
  | `count` | integer, minimum 0, maximum 100, default 1, `x-atlas-unit` count |
  | `ratio` | number, `x-atlas-logical` Fraction |
  | `when` | string, format date |
  | `kind` | enum ["plain","loud"] with `x-atlas-enum` |
  | `tags` | array, maxItems 10, items string minLength 1 |

- `result` (required ["text","count","kind"]): `text`, `count`, `ratio` ["number","null"], `when` ["string","null"] date, `kind` enum ["plain","loud","UNKNOWN"], `tags`.
- `traits` []; `usage.examples` [{"title":"Echo","interaction":"fetch","request":{"text":"hello"}}].
- Default answer: "The request, with `count` 1, `kind` `plain`, `ratio` and `when` null and `tags` [] when absent" (RD:176). So `{"text":"hello"}` gives `{"text":"hello","count":1,"ratio":null,"when":null,"kind":"plain","tags":[]}` (CORE-CALL-001).
- Variant **#d0a02ef751a8d8e7**: identical except the binding address is `/elsewhere/7/echo;v=1` (CORE-CALL-003).
- Variant **#4fe7738a97223902**:
  - traits [{"name":"FutureTrait","critical":false}];
  - result `kind` enum adds `"whisper"`;
  - adds optional result `echoed_at` ["string","null"] date-time;
  - `evolution {"previous_profile":"conformance.echo@v1#f17339273f0617c2","lens":[]}` (CORE-TOL-001).

#### `conformance.rows@v1` (data; #c121b005057331c7)

- Purpose (§9.8:66): "Framing, end lines, final batches, truncation, `columns`, encodings returning identical records".
- traits [{"name":"Projectable","critical":false}].

| Interaction | Effect / result / delivery | Deadline | Binding | Encodings |
|---|---|---|---|---|
| `fetch` | read, table, **chunked**, returns `result` | PT30S/PT2M | `/v1/data/conformance.rows@v1:fetch` | ["arrow","ndjson","json"] |
| `explain` | read, record, single (no `returns`) | PT30S/PT2M | `/v1/data/conformance.rows@v1:explain` | ["json"] |
| `export` | read, content, **transfer**, interaction `encodings` ["parquet","arrow"] | **PT5M/PT30M** | `/v1/data/conformance.rows@v1:export` | ["json"] |

- `request` (closed, required ["n"]): `n` integer 0..1000000 count; `columns` ["array","null"] of strings minLength 1, uniqueItems, default null.
- `result`: array of rows, required ["i","label","amount","at","day"]:

  | Field | Schema |
  |---|---|
  | `i` | integer ≥0, `x-atlas-key` true |
  | `label` | string |
  | `amount` | string, `x-atlas-logical` `Decimal(18,6)` |
  | `at` | string date-time |
  | `day` | string date |
  | `share` | ["number","null"], Fraction |

- `meaning.keys` ["i"].
- Default fetch (RD:177): "Rows 0 to n-1: `i`, `label` `row-<i>`, `amount` i × 1.5 (Decimal(18,6)), `at` 2026-01-01T00:00:00Z + i s + i µs, `day` 2026-01-01 + i days, `share` i/100 or null when i mod 5 = 4. Projected to `columns`; NDJSON, Arrow or a JSON array by Accept; date-times with six fractional digits."
  - Row 1 is `{"i":1,"label":"row-1","amount":"1.500000","at":"2026-01-01T00:00:01.000001Z","day":"2026-01-02","share":0.01}`. Row 4 has `"share":null`.
  - No Accept or `*/*` takes the binding's first encoding (Q-HDR-5, Q:98): `arrow`.
  - Accept tokens: `application/json`, `application/x-ndjson`, `application/vnd.apache.arrow.stream`.
- explain and export (RD:178): "The plan of that fetch; Parquet files in `export_parts` parts (default 1) at `{gateway}/storage/...`". The export manifest's file `media_type` is `application/vnd.apache.parquet` (BULK-001).
- Knobs: `state.export_parts`; storage steps `storage_part` k.

#### `conformance.items@v1` (data; #05354431b1d8a446)

- traits [{"name":"Paged","critical":false}].
- `fetch`: read, **collection**, single, returns `item`, PT30S/PT2M, `/v1/data/conformance.items@v1:fetch`, ["json"].
- `request` (closed, required ["n"]): `n` 0..10000; `page_size` 1..100 default 10; `cursor` string minLength 1.
- `item` (required ["k","name"]): `k` integer key, `name` string.
- Default (RD:179): "Items `{k, name: item-<k>}`, `page_size` to a page, with opaque cursors".
- **No scenario uses it.**

#### `conformance.slow@v1` (data; #739867d227780756)

- `fetch`: read, record, **operation**, deadline **PT1M/PT10M**, `/v1/data/conformance.slow@v1:fetch`, ["json"].
- `request` (closed, nothing required):
  - `polls` integer 0..20 default 2;
  - `result_bytes` integer 0..4194304 default 16 ("above 1 MiB it is given by result_location").
- `result` (required ["done","padding"]): `done` boolean, `padding` string.
- Default (RD:180): "202 with Retry-After 1; `polls` polls answer `running`, then `succeeded` with `{done: true, padding: "x" × result_bytes}`, by `result_location` when it is over 1 MiB; `:cancel` makes it `cancelled`".
- CORE-OP-001 note: "The gateway answers 202 with Retry-After: 1 and each poll before the last without one, so the client waits 1, 1, 2 s."

#### `conformance.evolving@v1` (data; three profiles; current #C)

Common: lifecycle `deprecated`, `deprecated` "2026-10-01", `sunset` "2027-01-31", `replaced_by` "conformance.evolving@v2"; Projectable; `fetch` read, table, chunked, PT30S/PT2M, `/v1/data/conformance.evolving@v1:fetch`, ["arrow","ndjson","json"].

| Profile | Request | Result row (required) | evolution |
|---|---|---|---|
| **#A** `bc7fde7c0bac077d` | `ids` (array minItems 1), `region`, `columns` | `id` (key), `mv` number currency_unit, `weight` number **Percent**, `status` enum ["open","sold","UNKNOWN"], `count` integer; optional `legacy` | none |
| **#B** `5f4ce7e7661ebd4c` | `ids`, `area` (`x-atlas-aliases` ["region"]), `columns` | `id`, `market_value` (`x-atlas-aliases` ["mv"]), `weight` **Fraction**, `currency` (Currency), `status` ["open","sold","UNKNOWN"], `count` integer; `legacy` with `x-atlas-deprecated {"reason":"No reader needs it.","since":"2026-07-01","sunset":"2026-10-01","replaced_by":null}` | previous #A; lens `rename mv→market_value`, `convert_unit weight Percent→Fraction`, `default currency "USD"`, `rename region→area` |
| **#C** `73def445ee90c57a` | same as #B | `id`, `market_value`, `weight`, `currency`, `status` enum ["open","closed","UNKNOWN"] (`closed` `aliases` ["sold"]), `count` **number**, `note` ["string","null"]; all seven required | previous #B; lens `alias_value status sold→closed`, `promote count integer→number`, `drop legacy` |

- Default (RD:181): "Positions for `ids`; a request under an older profile is resolved forward first; @v1 is deprecated, with Deprecation and Sunset".
- **OVERRIDDEN BY REVIEW (R03):** #B's `convert_unit` and #C's `promote` and `drop` are exactly what "no served profile may depend on … within a version" forbids (REV:90; REV:61: "A removal, a unit change, a type change … is a new major version"). Under the R03 gate, #B and #C would be refused as same-version successors. Their resolution vectors still pass (REV:90). The renames and alias are allowed (REV:61, "aliases for exact renames"); `default currency` is allowed only as an additive result field "with a defined missing value".

#### `conformance.evolving@v2` (data; #eb84229aaeb7ca6b)

- lifecycle stable.
- `request` `ids` only.
- `result` rows required ["id","value","at"]: `value` object `x-atlas-logical` Money; `at` date-time key; `meaning.keys` ["id","at"].
- `evolution {"previous_version":"conformance.evolving@v1"}` (no `version_lens`; compare Q-DOC-7, Q:62).
- Binding `/v1/data/conformance.evolving@v2:fetch`. No scenario calls it.

#### `conformance.retired@v1` (#6d24f084a2a2d7a5)

- lifecycle `retired`, deprecated "2026-01-01", sunset "2026-04-01", replaced_by "conformance.echo@v1".
- request `{text}` (nothing required); result `{ok}`.
- Default: "`retired`" (RD:182): every call is answered `retired` (410).
- Excluded from the working view (CORE-DISC-001 note).

#### `conformance.critical@v1` (#4274db333936328d)

- traits [{"name":"ConformanceOnly","critical":true}].
- Default: "its interaction answers `{ok: true}`, and no conforming client calls it" (RD:183).

#### `conformance.costly@v1` (#bf862d27e2fe69aa)

- traits [{"name":"Metered","critical":false}]; `usage.cost {"unit":"credits","price":"12.500"}`.
- `fetch` is effect **read**.
- Default (RD:184): "`confirmation_required` (estimate 12.500 credits) unless Atlas-Confirm is at least 12.5, then `{ok: true}`".
- **OVERRIDDEN BY REVIEW (R06):** "`Atlas-Confirm` as consent" becomes "A confirmation or approval receipt bound to the principal, address, normalised request, plan digest, scope, maximum and expiry. The header is the client's claim only" (REV:18, REV:64). Step 3.

#### `conformance.dynamic@v1` (#e401fa5670a50d55)

- stability `upstream`; traits [{"name":"DynamicSchema","critical":false}].
- `fetch`: table, chunked, ["arrow","ndjson","json"].
- request `{query}` required; result: array of untyped objects.
- Default: "`{columns, rows}` for its query (Q-DYN-1)" (RD:185). Q-DYN-1 (Q:71): `{"columns": [{"name", "type"}], "rows": [...]}`, and "No vector depends on it yet".
- **No scenario uses it.**

#### `conformance.handoff@v1` (#f06c3819d34aadf7) — DEFERRED

- `fetch`: execution ["gateway","handoff"], ["arrow","ndjson","json"].
- 13 typed columns: `i`, `amount` Decimal(18,6), `notional` Decimal(38,6), `spread` Bp, `ratio`, `label`, `open`, `day`, `at_ntz`, `at_ltz`, `at_tz`, `at_ntz9`, `at_tz9`.
- Default (RD:186): with `execution: handoff` and a client that declares it, the handoff document of `handoff_case` (default every-type), its chunks served from the gateway's storage; otherwise the same rows streamed.

#### `conformance.thing@v1` (resource; #677bf5325b4e72c3)

- traits Paged, Keyed.

| Interaction | Effect | Takes | Returns |
|---|---|---|---|
| `get` | read | `get_request` | `resource` |
| `list` | read, collection | `list_request` | `resource` |
| `create` | **write** | `create_request` | `resource` |
| `update` | **write** | `update_request` | `resource` |
| `delete` | **write** | `get_request` | `resource` |
| `touch` | **write** | `get_request` | `resource` |

  All are single delivery, PT30S/PT2M, at `/v1/resource/conformance.thing@v1:<i>`.
- `resource` (required ["name","metadata","colour","size"]):
  - `name` with pattern `^[a-z](-?[a-z0-9])*(/[a-z](-?[a-z0-9])*/[a-z0-9][a-z0-9-]*)+$` (key);
  - `metadata` required ["uid","etag","generation","created","updated","owner","labels","deleting"]:
    - `etag` pattern `^(W/)?\"[!#-~]*\"$`, "Moves with generation, never with status";
    - `generation` ≥1;
    - `owner` Principal;
    - `labels` map;
    - `deleting` ["string","null"] date-time;
  - `colour` enum ["red","blue","UNKNOWN"]; `size` ≥0;
  - `status` {`observed_generation`, `conditions`: [{`type`, `status` enum ["true","false","unknown","UNKNOWN"], `reason`, `message`, `observed_generation`, `last_transition`}]}.
- `create_request` (closed): `name`, `colour` ["red","blue"], `size`, `metadata` {`labels`} only.
- `update_request` (closed, required ["name","fields"]): `fields` (mask: "colour, size, metadata.labels", minItems 1), `colour`, `size`, `metadata.labels`.
- `meaning {"name":"conformance/things/{id}","immutable":["name"],"stored_version":1,"converges":"a fake controller sets the ready condition"}`.
- Default (RD:187): "An in-memory store with the envelope; a fake controller sets `ready`; `If-Match` checked (412), field masks checked (`invalid_request` on a server-owned member), idempotency keys replayed and in conflict (409)".
- Knobs: `state.things`; step modifiers `before.set_status`, `deleting`.
- **OVERRIDDEN BY REVIEW (R14):** "The ETag covers the full representation; `generation` is separate". Step: "Resources (after the journey)" (REV:72; AC:228). The profile's "never with status" and CORE-ENV-001's status-without-ETag premise contradict it.

#### `conformance.file@v1` (resource; #364cf4144dd8837e)

| Interaction | Effect / result / delivery | Takes | Returns | Deadline | Binding |
|---|---|---|---|---|---|
| `publish` | write, record, **transfer** | `publish_request` | `record` | PT5M/PT30M | `/v1/resource/conformance.file@v1:publish` |
| `get` | read, **content**, **transfer** | `get_request` | `record` | PT5M/PT30M | `/v1/resource/conformance.file@v1:get` |

- `record`: `name`, `media_type`, `bytes`, `sha256` (pattern `^[0-9a-f]{64}$`).
- `publish_request` adds `bytes` maximum 1073741824.
- Default (RD:188): "`get`: 202, then a transfer download (inline under 1 MiB, else parts); `publish`: 202, then a transfer upload in parts of 8 MiB and a commit binding". Inline bytes are the transfer document's file `inline` (base64) under 1 MiB (Q-DOC-14, Q:69).
- Knobs: `state.files` `{<name>: {"base64", "media_type", "sha256"?}}`. A given `sha256` that is wrong makes the checksum fail (CORE-CONTENT-002 `bad.bin` `"0000…0000"`).

#### `conformance.change@v1` (action; #b04dbbdc02bb6ba4) — no scenario uses it

- traits DryRunnable, NeedsApproval.
- `plan`: read, returns `plan`. `approve`: write, takes `by_plan`, returns `approval`. `run`: effect **external**, **operation**, PT1M/PT10M, returns `receipt`.
- `meaning.policy.approval` ["conformance"].
- Default: "`plan`, then `run` answers `approval_required` until `approve`" (RD:189).

#### `conformance.tick@v1` (event; #A `1acba4963d7b5097` payload {seq,count}; #B `61948f2a0e7d7132` payload {seq,n} with `n` aliases ["count"] and lens `rename count→n`) — DEFERRED

- `subscribe`: delivery subscription, `/v1/event/conformance.tick@v1:subscribe`.
- `publish`: effect external, single.
- `meaning.retention` "P1D".
- Default: "`subscribe` and `publish` answer an emulator connect handover; `bus` sends the events" (RD:190).
- CloudEvent type `com.jhg.fiquant.conformance.tick.v1`, `dataschema` `urn:atlas:profile:conformance.tick@v1:<fp>` (EVT-001).

### 2.7 Knobs, summarised

| Knob | Where | Values |
|---|---|---|
| `state.catalog_page_size` | atlas.catalog | CORE-DISC-001: 4 |
| `state.export_parts` | rows:export | default 1; BULK-001: 3, BULK-003: 2 |
| `state.files` | file:get | name → base64, media_type, optional (wrong) sha256 |
| `state.things` | thing | records with `etag`, `generation`, `labels`, `conditions` |
| `state.handoff_case` | handoff | every-type (default), widths-change, mismatch-second-chunk, … |
| request `polls`, `result_bytes` | slow | 0..20 (2); 0..4194304 (16) |
| request `n`, `columns` | rows | 0..1000000 |
| `delay_ms` | any step | CORE-DEADLINE-001: 5000 |
| failure injection | `steps[].respond.status`/`problem`/`stream.end: null` | see each scenario |
| `expire_links` | storage step | BULK-003, HAND-002 |
| `headers` | by path or address | CORE-BOOT-001 `{"/.well-known/atlas":{"ETag":"\"boot-1\""}}`; CORE-DEPR-001 by address |
| Threshold | costly | Atlas-Confirm ≥ 12.5; estimate `{"amount":"12.500","unit":"credits"}` |
| Size thresholds | slow, file | 1 MiB (1048576) for `result_location` and for inline; upload parts 8 MiB (8388608) |

---

## 3. The 50 scenarios

The only requirement ids that scenario files cite are **AP-CLIENT-003** (CORE-CALL-005) and **AP-EVENTS-002** (EVT-003). The others cite § sections and Q-ids, shown in the "Cites" column. Scope is judged against REV and AC:232–244 ("durable operations where that journey needs them", AC:238).

### 3.1 The table

| ID | What it proves | Cites | Capabilities · ops | Expected outcome (one line) | Scope |
|---|---|---|---|---|---|
| CORE-BOOT-001 | Bootstrap fetched unsigned, revalidated with If-None-Match; every URI built from it | §9.8:85 | bootstrap; whoami moved to `/bootstrapped/who-am-i` · connect, whoami ×2 | whoami value_contains `{"environment":"conformance"}`; 2nd bootstrap has `If-None-Match: "boot-1"` (answered 304); 2 POSTs to `/bootstrapped/who-am-i`, 0 to `/v1/atlas/atlas.whoami:fetch`; 1st bootstrap has no Authorization | IN SCOPE |
| CORE-BOOT-002 | Gateway chosen in order: connect arg, ATLAS_URL, ATLAS_ENV, config.toml | Q-CLI-1 | atlas.whoami · connect(url), whoami, suite.restart ×3 | one whoami on each of listener gateway, gateway2, gateway3, gateway4 | IN SCOPE (order). **OVERRIDDEN BY REVIEW (R26)** for the "one token file serves all four" premise |
| CORE-SIGNIN-001 | Token from `<ATLAS_HOME>/state/token-<env>.json`, env = bootstrap's environment | Q-CLI-1 | whoami | whoami `Authorization: Bearer ct-from-file`; bootstrap GET without Authorization | IN SCOPE, **AMBIGUOUS (R11/R26)** on the file location |
| CORE-SIGNIN-002 | Token within 60 s of expiry refreshed first; file rewritten atomically 0600 in 0700 | — | /oauth/token, whoami · suite.expect_file | POST /oauth/token before whoami; form `grant_type=refresh_token`, `refresh_token=rt-1`; whoami `Bearer ct-new`; file has `ct-new`/`rt-2`, mode 0600, dir 0700; no request with `Bearer ct-old` | IN SCOPE, **AMBIGUOUS (R11/R26)** |
| CORE-SIGNIN-003 | 401 `invalid_token` → refresh and repeat once; second 401 → unauthenticated | — | whoami ×2 | cmd 2 ok, cmd 3 `unauthenticated`; 4 whoami requests, 2 token requests; whoami nth 2 `Bearer ct-refreshed` | IN SCOPE |
| CORE-SIGNIN-004 | Claims challenge without a person → unauthenticated with challenge in details | Q-DOC-9 | whoami | `unauthenticated`, `details:[{"challenge":"Bearer realm=\"atlas\", claims=\"eyJhY2Nlc3NfdG9rZW4iOnsiYWNycyI6eyJ2YWx1ZXMiOlsiYzEiXX19fQ\""}]`; 1 whoami, 0 token requests | IN SCOPE |
| CORE-SIGNIN-005 | Token file mode 0644 refused; never sent | — | whoami | connect ok; whoami `unauthenticated`; 0 requests with `Bearer ct-valid` | IN SCOPE, **AMBIGUOUS (R11)** |
| CORE-DISC-001 | Catalogue pages by cursor; fingerprint recomputed; wrong one → invalid_document | §2.6 | atlas.catalog, atlas.describe · catalog, describe ×2 | catalog `$count` 13; describe echo profile fingerprint `f17339273f0617c2`; describe rows → `invalid_document`; 4 catalog requests, `cursor_chain` | IN SCOPE |
| CORE-DISC-002 | Profile fetched once per exact-id; then from cache, in memory and after restart from `<ATLAS_HOME>/cache/profiles/` | — | echo · call ×3, suite.restart | 3 echo values; exactly 1 `atlas.describe:fetch`, 3 echo fetches | IN SCOPE; **OVERRIDDEN BY REVIEW (R11, R01)** for the disk path |
| CORE-GUIDE-001 | Asks atlas.guide for its own syntax; returns Markdown unchanged | Q-RUN-1 | atlas.guide · guide | markdown byte-identical; body contains `{"by":"id","id":"conformance.echo@v1"}`; `/client` ∈ ["cli","python","node","go","mcp"]; no Atlas-Profile-Expect | IN SCOPE |
| CORE-EXPLAIN-001 | explain validated as fetch; answer returned whole, statement byte for byte | Q-RUN-1 | rows:explain · explain ×2 | `{"n":-1}` → `invalid_request` target `/n` (nothing sent); 2nd returns the scripted body exactly; 1 explain request | IN SCOPE |
| CORE-CALL-001 | Every §9.6.3 request header present and parses; built-in has no Atlas-Profile-Expect; only a non-read carries Idempotency-Key | §9.6.3 | whoami, echo:fetch, thing:create | see 3.2 | IN SCOPE (needs thing:create) |
| CORE-CALL-002 | Invalid request → invalid_request, first pointer as target, all failures in details, nothing sent | Q-VAL-1, Q-DOC-9 | echo · call | `target "/count"`, details `[{"target":"/count","keyword":"maximum"},{"target":"/extra","keyword":"additionalProperties"},{"target":"/text","keyword":"minLength"}]`; 0 echo requests | IN SCOPE |
| CORE-CALL-003 | Client follows the served binding; never builds a path | — | echo #d0a0… · call | 1 POST `/elsewhere/7/echo;v=1`; 0 to `/v1/data/conformance.echo@v1:fetch` | IN SCOPE |
| CORE-CALL-004 | Write refused once 503 → retried with same Idempotency-Key | — | thing:create · call | ok; 2 creates, same Idempotency-Key, ≥1000 ms apart | IN SCOPE (REV:50 "idempotent retries") |
| CORE-CALL-005 | Same inputs → same request record in every client, byte-for-byte after RFC 8785 | **AP-CLIENT-003** | echo · call | body jcs `{"count":2,"kind":"loud","ratio":0.5,"tags":["b","a"],"text":"naïve é","when":"2026-10-31"}` | IN SCOPE (REV:47) |
| CORE-PROB-001 | Each registry code → the language's error with every member; CLI exit codes | §9.7.6, Q-RUN-2 | echo · call ×23 | each problem with code, target `/text`, status, retryable, detail `conformance: <code>`, type, cli_exit (table 1.13); retryable codes 4 requests, unauthenticated 2, others 1 | IN SCOPE |
| CORE-PROB-002 | Unknown code handled by status and retryable | — | echo · call ×2 | `conformance_strange` 503 retryable (4 requests), `conformance_odd` 422 (1 request); cli_exit 1 | IN SCOPE |
| CORE-RETRY-001 | 429/503 with Retry-After: ≤3 retries, honour Retry-After, never past deadline; non-retryable not retried | — | echo · call ×4 | x ok after 3 requests (≥1000, ≥2000 ms); y `unavailable` after 4; z `unavailable` after 2 (deadline 3000) with the same Atlas-Deadline; w `denied` after 1 | IN SCOPE |
| CORE-DEADLINE-001 | Atlas-Deadline = default when unnamed, capped at max; deadline_exceeded | Q-HDR-2 | echo · call ×3 | a: 28–31 s; b (600000 ms): 118–121 s; slow (1000 ms, gateway delays 5000): 0–2 s and `deadline_exceeded` | IN SCOPE |
| CORE-TOL-001 | Unknown members, headers, enum values, traits ignored; unknown enum → UNKNOWN | — | echo #4fe7… · call | value `{"text":"hello","count":1,"ratio":null,"when":null,"kind":"UNKNOWN","tags":[]}`; describe exact `conformance.echo@v1#4fe7738a97223902` once | IN SCOPE |
| CORE-CRIT-001 | Critical trait unsupported → critical_trait_unsupported, no call | — | critical · call | `critical_trait_unsupported`; 0 requests | IN SCOPE |
| CORE-RES-001 | Answer under newer fingerprint: fetch writer chain by exact, apply lenses backward | — | evolving #C→#A · call (lock #A) | value `[{"id":"p1","mv":1250.5,"weight":1.5,"status":"sold","count":3}]`; describe exact #73de… once, #5f4c… once | **DEFERRED / OVERRIDDEN BY REVIEW (R03)** |
| CORE-RES-002 | Rows for a request naming columns valid against the projected schema (§9.5.2 step 5) | §9.5.2 | evolving #C (lock #C) · call | value `[{"id":"p1","weight":0.015},{"id":"p2","weight":0.25}]` | IN SCOPE (projection; relies on atlas.lock) |
| CORE-RES-003 | Request under older locked profile: Atlas-Profile-Expect names it; body under it; gateway resolves forward | — | evolving #A (lock) · call | ok; Atlas-Profile-Expect `conformance.evolving@v1#bc7fde7c0bac077d`; body equals `{"ids":["p1"],"region":"EU"}` | **DEFERRED as written (R03)**; the request-alias half is compatible with R03/R04 |
| CORE-LOCK-001 | atlas.lock found in nearest ancestor; entries are reader profiles; no discovery call | — | echo, evolving · call ×2 | 0 catalog, 0 search, 0 describe `by:id`; Atlas-Profile-Expect `…evolving@v1#bc7fde7c0bac077d` and `…echo@v1#f17339273f0617c2` | **AMBIGUOUS** (R02, R03, lock format open REV:98) |
| CORE-DEPR-001 | Deprecation and Sunset → warning naming what, its sunset and replacement | Q-RUN-3 | evolving@v1 · call | warning `code "deprecated"`, text contains `conformance.evolving@v1`, `2027-01-31`, `conformance.evolving@v2` | IN SCOPE |
| CORE-OP-001 | 202: polls ≥ Retry-After, then doubling 1 s → 30 s; result or result_location (>1 MiB); cancel → cancelled | Q-RUN-5 | slow · call ×3, operation.wait ×2, .cancel, .get | see 3.2 | **AMBIGUOUS** (in scope only if a journey capability is an operation, AC:238) |
| CORE-CONFIRM-001 | confirmation_required: no person → problem; declines → problem; accepts → one repeat with Atlas-Confirm | Q-DOC-10, §9.6.3 | costly · call ×3 | estimate `{"amount":"12.500","unit":"credits"}`; accept → `{"ok":true}`; c sent twice, 2nd with `Atlas-Confirm: 12.5` | **DEFERRED** (REV:31 step 3); **OVERRIDDEN BY REVIEW (R06, R08)** |
| CORE-CONTENT-001 | Small content: no path → bytes in memory, nothing on disk; with path → file + `atlas-manifest.json` | Q-DOC-14, Q-RUN-4 | file:get · call ×2, suite.expect_file ×3 | in-memory content and metadata; `{ATLAS_HOME}/downloads` absent; `{WORKDIR}/out/small.txt` and manifest | **DEFERRED** (no content capability in the journey); R11 |
| CORE-CONTENT-002 | Content > max_memory lands at `<ATLAS_HOME>/downloads/<date>/<request id>/<name>` with manifest; wrong sha256 → no file, invalid_document | Q-RUN-4 | file:get · call ×2, expect_file ×3 | manifest files `[{"name":"big.bin","bytes":4096}]`; file at `…/downloads/{today}/req-content-2/big.bin`; bad.bin → `invalid_document`, absent | **DEFERRED**; **OVERRIDDEN BY REVIEW (R11)** on the path |
| CORE-ENV-001 | Resource envelope: unknown condition kept; status changes without ETag; update from record; deleting reads back | Q-RUN-4 | thing get/update/delete · call ×4 | see 3.2 | **DEFERRED** (REV:72 "Resources (after the journey)"); **OVERRIDDEN BY REVIEW (R14)** |
| CORE-UPGRADE-001 | Client below minimum protocol → 426 upgrade_required | — | whoami | whoami `upgrade_required`; 1 whoami | IN SCOPE (**AMBIGUOUS** connect, 3.3) |
| STREAM-001 | NDJSON rows then end line: every row, then usage | Q-RUN-5 | rows:fetch ndjson | 7 rows (i=0..6, i=4 share null), `usage {"rows":7}`; Accept contains `application/x-ndjson` | IN SCOPE (AC:206) |
| STREAM-002 | Stream ends without end line → upstream_failed | — | rows | `upstream_failed` | IN SCOPE |
| STREAM-003 | Problem in end line after rows → raised after rows | Q-RUN-6 | rows | ok false, `internal`, value = the 2 rows | IN SCOPE |
| STREAM-004 | Truncated at output limit → warning output_limit_exceeded, rows returned | Q-DOC-15 | rows | ok true, 4 rows, warnings `[{"code":"output_limit_exceeded"}]` | IN SCOPE |
| COL-001 | Arrow with metadata and zero-row final batch = same records as NDJSON; decimals exact; timestamps UTC µs | Q-TIME-1 | rows arrow + ndjson | both give the same 6 rows; Accept contains `application/vnd.apache.arrow.stream` | **AMBIGUOUS** (R11: Arrow optional; needed where Arrow is offered) |
| COL-002 | Arrow without the zero-row final batch → upstream_failed | — | rows arrow | `upstream_failed` | as COL-001 |
| COL-003 | Arrow under newer fingerprint: column-wise resolution = row-wise | — | evolving #C→#A arrow | rows `[{"id":"p1","mv":1250.5,"weight":1.5,"status":"sold","count":3},{"id":"p2",…,"status":"open",…}]` | **DEFERRED (R03)** |
| BULK-001 | Export to parts, one failing once: every part fetched and sha256-checked, failed one again | — | rows:export · export | manifest file media_type `application/vnd.apache.parquet`; part1 GET 1, part2 GET 2, part3 GET 1 | **DEFERRED** (AC:208, REV:33) |
| BULK-002 | Upload parts ≤ 8 MiB, then one commit with the call's Idempotency-Key | Q-RUN-4 | file:publish · suite.file, upload | value `{name:"bundle.bin",bytes:20971520}`; 3 PUTs, each ≤ 8388608; 1 `:commit` POST; same Idempotency-Key | **DEFERRED** |
| BULK-003 | Expired link: client asks again for fresh links, finishes | — | rows:export · export | ok; 1 `atlas.operation:get`; part 1 GET twice | **DEFERRED** |
| EVT-001 | Emulator handover; one event twice, one under older profile → each once, resolved | Q-RUN-7 | tick · subscribe.* | seq1 n10, seq2 n20, seq3 n30, then `{"none":true}` | **DEFERRED** (REV:15, R16) |
| EVT-002 | publish validates payload first; CloudEvent attributes per 9.7.8 | Q-EVT-1 | tick:publish | `{"n":"ten"}` → `invalid_request` target `/n`; then id `$uuid` | **DEFERRED** |
| EVT-003 | Emulator handover from env `dv` refused | **AP-EVENTS-002** | tick | `invalid_document`; 0 bus connections | **DEFERRED** |
| EVT-004 | Grant revoked while subscribed → iterator ends not_granted | — | tick | seq 1 then `not_granted`; 2 subscribe requests | **DEFERRED** |
| HAND-001 | Handoff chunks fetched in parallel, decoded, checked; same as gateway execution | — | handoff | both = `{vectors:handoff/snowflake-arrow-1/synthetic/cases.json#every-type}` | **DEFERRED** (REV:15, AC:241) |
| HAND-002 | Links expire mid-download → operation asked again | — | handoff | = `#widths-change`; 1 `atlas.operation:get` | **DEFERRED** |
| HAND-003 | Chunk schema differs → invalid_document | — | handoff | `invalid_document` | **DEFERRED** |

In-scope count: 29 IN SCOPE outright (25 Core + 4 Streaming), plus 4 AMBIGUOUS (CORE-OP-001, CORE-LOCK-001, COL-001, COL-002). The 17 DEFERRED are CORE-RES-001, CORE-RES-003, CORE-CONFIRM-001, CORE-CONTENT-001, CORE-CONTENT-002, CORE-ENV-001, COL-003, BULK ×3, EVT ×4 and HAND ×3.

Why each scope call holds:
- **BULK** is DEFERRED. "Direct downloads are an optional delivery implementation" (AC:208). REV:33 step 5 lists "direct downloads" among real systems. The journey has no upload. Export is in the handbook's standard data set, but it is a separate bulk contract.
- **COL** is conditional. R11: "Core is JSON, and Arrow is an optimisation of the same meaning" (REV:69). The Python face from pass 1 is Arrow-based (REV:19, REV:92). Reading: any client that negotiates Arrow must pass COL-001/002. COL-003 is DEFERRED under R03.
- **STREAM** is IN SCOPE. "Streaming with explicit completeness" is in the first slice (DR:176). "A stream has an explicit completion marker; EOF alone is not success" (AC:206). Acceptance item 5 covers "incomplete streams" (REV:50).
- **CORE-*:** see each row. The decisive overrides:
  - R03 (REV:61, REV:90): CORE-RES-001/003, COL-003, CORE-LOCK-001;
  - R14 (REV:72): CORE-ENV-001;
  - R06/R08 (REV:64, REV:66; AC:198): CORE-CONFIRM-001;
  - R11 (REV:69; AC:136): file-location assertions;
  - R26 (REV:84): token binding;
  - R02 (REV:60): lock semantics;
  - R01 (REV:59): caching.

### 3.2 Exact assertions of the larger in-scope scenarios

**CORE-BOOT-001**
- The script replaces the bootstrap: `atlas.whoami` binding `/bootstrapped/who-am-i`; the other 14 built-ins at `/v1/atlas/...`.
- Bootstrap header `ETag: "boot-1"`. The note says the gateway answers the second bootstrap 304.

**CORE-BOOT-002**
- `setup.env.ATLAS_URL` = `{gateway2}`.
- `{ATLAS_HOME}/config.toml` (mode 0600) holds exactly:
  `env = "cfg"\n\n[environments.cfg]\nurl = "{gateway4}"\n\n[environments.envc]\nurl = "{gateway3}"\n`
- Sequence:

  | Commands | Setup | Expected listener |
  |---|---|---|
  | 1–2 | connect url `{gateway}` | `gateway` |
  | 3–5 | restart with ATLAS_URL `{gateway2}`, ATLAS_ENV `envc` | `gateway2` (ATLAS_URL beats ATLAS_ENV) |
  | 6–8 | restart with ATLAS_URL null, ATLAS_ENV `envc` | `gateway3` |
  | 9–11 | restart with both null | `gateway4` (config.toml env `cfg`) |

- Each count is 1, keyed by `listener`.

**CORE-CALL-001**
- Header asserts on `atlas.whoami:fetch`: `Atlas-Client`, `Atlas-Client-Features`, `Atlas-Deadline`, `traceparent` parse; `Atlas-Profile-Expect` absent; `Authorization` equals `Bearer ct-valid`.
- On `conformance.echo@v1:fetch` and `conformance.thing@v1:create`: `Atlas-Client`, `Atlas-Client-Features`, `Atlas-Profile-Expect`, `Atlas-Deadline`, `traceparent` all parse.
- Echo `Atlas-Profile-Expect` equals `conformance.echo@v1#f17339273f0617c2`.
- Echo `Idempotency-Key` absent; thing:create `Idempotency-Key` parses.
- Echo `Accept` contains `application/json`.
- Values: echo `{"text":"hello","count":1,"ratio":null,"when":null,"kind":"plain","tags":[]}`; create value_contains `{"name":"conformance/things/t1","colour":"red"}`.

**CORE-CALL-004**
- Step: thing:create nth 1 → 503, problem `{"code":"unavailable","retryable":true,"retry_after":1}`, header `Retry-After: 1`.

**CORE-PROB-001**
- Script: echo with `body_contains {"text":"<code>"}` → that code's status. Problem `{code, retryable, target:"/text", detail:"conformance: <code>"}`.
- `unauthenticated` also sends `WWW-Authenticate: Bearer realm="atlas", error="invalid_token"`, `times 2`.
- `confirmation_required` adds `estimate {"amount":"1.000","unit":"credits"}`.
- Retryable codes (`in_progress` 409, `limit_exceeded` 429, `upstream_failed` 502, `unavailable` 503) carry `retry_after: 0` and `Retry-After: 0`, `times 4`.
- Expected: each problem with `code`, `target "/text"`, `status`, `retryable`, `detail`, `type "https://atlas.jhg.com/problems/<code>"`, `cli_exit`.
- Note: "Retryable codes are answered four times (the call and its three retries, with Retry-After 0); unauthenticated twice (the call and its one repeat after a refresh). output_limit_exceeded only travels in an end line (STREAM-004)."
- **AMBIGUOUS:** `unauthenticated` is repeated once "after a refresh". The script's 401 carries `error="invalid_token"`, so the client refreshes with `rt-valid` (GW tokens) and repeats. The count is 2.

**CORE-RETRY-001**
- Steps:

  | Text | Answers | Times |
  |---|---|---|
  | x | nth 1 → 429 `limit_exceeded` retry_after 1 (`Retry-After: 1`); nth 2 → 503 `unavailable` retry_after 2 (`Retry-After: 2`); then default | 1 each |
  | y | 503 retry_after 0 | 10 |
  | z | 503 retry_after 2 | 10 |
  | w | 403 `denied` retryable false | 10 |

- Command 4 has `options.deadline_ms 3000`.
- Note: "z: the deadline is 3 s away; the first retry is at 2 s, a second would be at 4 s, past it, so the client raises the problem it holds. A retry keeps the call's deadline."

**CORE-DEADLINE-001**
- Note: "conformance.echo@v1:fetch has deadline default PT30S and max PT2M; Atlas-Deadline is in whole seconds, rounded down (Q-HDR-2)."

**CORE-DISC-001**
- Steps: `atlas.describe:fetch` with body_contains `{"id":"conformance.rows@v1"}` → 200 `describe_profile` = the rows profile with `"fingerprint":"0000000000000000"`.
- `state.catalog_page_size` 4.
- Note: "13 of the 15 conformance capabilities are in the working view (retired@v1 is retired; critical@v1 has a critical trait no client supports, §2.6 rule 2), four to a page: four pages, each request carrying the previous page's next_cursor. `$count` in value_contains is the number of items the iterator yields."
- **AMBIGUOUS:** who excludes `critical@v1` from the working view? Readings: (a) the gateway, from the caller's declared features (Atlas-Client-Features); (b) the client's iterator. Four pages arise either way (13 → 4+4+4+1; 15 → 4+4+4+3).

**CORE-OP-001**
- Commands:

  | Cmd | Op | Expected |
  |---|---|---|
  | 2 | call `{"polls":2}` | value_contains `{"address":"conformance.slow@v1:fetch","state":{"$in":["pending","running"]}}` |
  | 3 | operation.wait `{"name":"{operation:2}","timeout_ms":30000}` | value `{"done":true,"padding":"xxxxxxxxxxxxxxxx"}` (16 x) |
  | 4 | call `{"polls":0,"result_bytes":1100000}` | ok |
  | 5 | operation.wait | value_contains `{"done":true,"$length":{"/padding":1100000}}` |
  | 6 | call `{"polls":20}` | ok |
  | 7 | operation.cancel | ok |
  | 8 | operation.get | value_contains `{"state":"cancelled"}` |

- Requests: `poll_intervals` for `{operation:2}` `min_ms` `[1000,1000,2000]`; exactly 1 GET with prefix `/v1/operations/` and suffix `/result`, carrying `Authorization: Bearer ct-valid`; exactly 1 POST with suffix `:cancel`.
- R17 (REV:75): "Cancellation is reported as requested or confirmed". The fixture's cancel makes it `cancelled`, the confirmed case (AC:212).

**CORE-UPGRADE-001**
- bootstrap merge `{"protocols":["atlas/1.0","atlas/2.0"],"minimum_protocol":"atlas/2.0"}`; whoami → 426 `upgrade_required`, times 5.

**CORE-ENV-001** (deferred, recorded for later)
- `state.things[0]`: `{"name":"conformance/things/t1","colour":"red","size":3,"labels":{"desk":"credit"},"etag":"\"e1\"","generation":1,"conditions":[{"type":"ready","status":"true","reason":"converged"},{"type":"quantum","status":"unknown","reason":"entangled"}]}`.
- The update step has `before.set_status` `{"name":"conformance/things/t1","observed_generation":1,"conditions":[{"type":"ready","status":"false","reason":"reconverging"}]}`. The delete step has `deleting: true`.
- Command 3: request `"{value:2}"`, options `{"fields":["colour"],"set":{"colour":"blue"}}`. Expected body exactly `{"name":"conformance/things/t1","fields":["colour"],"colour":"blue","size":3,"metadata":{"labels":{"desk":"credit"}}}`, `If-Match: "e1"`; value_contains `{"colour":"blue","metadata":{"generation":2}}`.
- Command 4: delete with options `if_match "{value:3/metadata/etag}"` → `metadata.deleting` `$present`.

**CORE-EXPLAIN-001**
- Scripted explain body (returned byte for byte):
  - `"address":"conformance.rows@v1:fetch"`, `"profile":"conformance.rows@v1#c121b005057331c7"`;
  - `plan {"language":"sql","dialect":"snowflake","statement":"SELECT\n  CAST(\"I\" AS NUMBER(18,0)) AS \"i\"  -- every column aliased and cast\nFROM\tTABLE(GENERATOR(ROWCOUNT => 3))\nWHERE 1 = 1 /* naïve */","query_tag":"atlas:x"}`;
  - `result_schema`;
  - `"execution":"gateway"`;
  - `estimate {"rows":3,"bytes":96,"cost":null}`.

**CORE-GUIDE-001**
- Guide answer: `{"exact":"conformance.echo@v1#f17339273f0617c2","client":"python","markdown":…,"tokens":37,"truncated":false,"revisions":{"governance":"g-1","context":"c-1"}}`.
- R01: "A guide's identity includes the prose revision" (REV:59).

**STREAM end lines** (STREAM-003/004)
- `{"$atlas":"end","rows":N,"truncated":false|true,"usage":{"rows":N},"problem":{...}}`.
- STREAM-004 problem `{"code":"output_limit_exceeded","status":200,"retryable":false}`. STREAM-003 problem `{"code":"internal","retryable":false}`.

**CORE-RES-001 / COL-003** (deferred)
- Writer body: `{"id":"p1","market_value":1250.5,"weight":0.015,"currency":"EUR","status":"closed","count":3.0,"note":null}`. Reader #A sees `{"id":"p1","mv":1250.5,"weight":1.5,"status":"sold","count":3}`.
- CORE-RES-001 note: "The same record as resolution.json's case 'through two lenses backward'."

**Lock and config formats**
- atlas.lock text: `[capabilities]\n"conformance.evolving" = { version = 1, fingerprint = "bc7fde7c0bac077d" }\n` (CORE-RES-001/003, COL-003). CORE-RES-002 uses fingerprint `73def445ee90c57a`.
- CORE-LOCK-001 adds `"conformance.echo" = { version = 1, fingerprint = "f17339273f0617c2" }`, at `{WORKDIR}/app/atlas.lock`, cwd `{WORKDIR}/app/src/deep`.
- **AMBIGUOUS:** "The minimum sunset and the lock's format, which the spec still has open" (REV:98).

### 3.3 Contradictions and gaps found in the scenarios

1. **CORE-UPGRADE-001.** The expectation is `{"id":1,"ok":true}` for connect and `count whoami = 1`. The note says "the suite accepts either connect failing upgrade_required, or whoami doing so". **AMBIGUOUS:** the file encodes only the second reading. A client whose connect refuses (reading 1) fails both the cmd-1 expectation and the count.
2. **CORE-CONFIRM-001 vs §9.8:113.** §9.8:113 says the accepted repeat carries "`Atlas-Confirm` and the same key". The scenario note says "it is a read, so no Idempotency-Key is sent (§9.6.3)". AC:198 (R08) says "Metered reads that can incur duplicate charges need this treatment too". **AMBIGUOUS / OVERRIDDEN BY REVIEW (R08):** under the review, a metered read carries an idempotency key.
3. **CORE-CALL-001's rule** "only a call whose effect is not read carries an Idempotency-Key" conflicts with R08/AC:198 for metered reads. It holds for echo, which is not metered.
4. **CORE-BOOT-002 note.** "one token file serves all four" listeners contradicts R26 (REV:84).
5. **CORE-ENV-001** premise "a `status` that changes without the ETag" (§9.8:116) is reversed by R14 (AC:228: "a strong ETag that changes whenever the returned representation changes").
6. **MISSING scenarios** the review's acceptance needs (REV:44–51; DR:124):
   - two identities with different scope;
   - denied versus invalid for an ungranted caller (only the gateway-suite sentence, §9.8:136);
   - `search`, `access` and `version` ops;
   - paging through `conformance.items` (no scenario at all);
   - "Count source executions when reading a result twice" (R11);
   - result privacy (R18);
   - a capability added after the clients were built (REV:49);
   - cross-client comparison of the same request record. CORE-CALL-005 checks one client against a fixed JCS string, which is equivalent if every client passes.
7. **Unused fixtures:** `conformance.items@v1`, `conformance.dynamic@v1`, `conformance.change@v1`, `conformance.evolving@v2` and `thing:list`/`thing:touch` are served but no scenario drives them.

---

## 4. QUESTIONS.md readings that change gateway or client behaviour

| Q | Reading (copied or near-verbatim) | Who | Scope / review |
|---|---|---|---|
| Q-ARROW-1 (Q:7) | Arrow field order and §9.5.2 matching order = **document order of `properties`**; "A reader must parse `properties` keeping member order (Go's `map[string]any` does not)" | both | IN SCOPE (R12, DR:132 "explicit table column order") |
| Q-GRAM-1 (Q:8) | `resource-name = domain 1*( "/" segment "/" resource-id )`, `resource-id = ( %x61-7A / DIGIT ) *( %x61-7A / DIGIT / "-" )`, `operation-name = %s"operations/" resource-id` (PROVISIONAL) | both | operation names IN SCOPE if operations are |
| Q-CLI-1 (Q:9) | `<env>` in `token-<env>.json` is the bootstrap's `environment`, so the client reads the bootstrap first, then `token-conformance.json` | client | **OVERRIDDEN BY REVIEW (R26, R11)**: credential binding by trusted gateway/authority, not by bootstrap-declared name |
| Q-DOC-1 (Q:10) | Every enum in a gateway-sent document includes `UNKNOWN`; a client maps an unknown value to it **before validating**; no request enum has it | both | IN SCOPE (REV:90 "UNKNOWN") |
| Q-RE-1 (Q:12) | I-Regexp refuses `\p{C}`, `\p{Cn}`, quantifier counts > 1000 | gate | IN SCOPE (Phase 1) |
| Q-VAL-1 (Q:13) | Failures sorted by pointer then keyword (code points), each pair once; `/10` before `/2`; the first is invalid_request's `target` | both | IN SCOPE (CORE-CALL-002) |
| Q-PROF-9 (Q:14) | Built-in profiles exempt from the lint; schemas name `builtins.schema.json` by `$id` | gateway | IN SCOPE |
| Q-GRAM-2/3/4 (Q:19–21) | Accept `atlas/01.00`; accept `P1DT`; accept year `0000` | parsers | IN SCOPE |
| Q-DEC-1 (Q:26) | SQL meaning: ≤ s fractional digits and integer digits + s ≤ p; `1234.5` ∉ Decimal(5,2); `0.123` ∈ Decimal(3,3) | both | IN SCOPE (exact decimals, REV:50) |
| Q-DEC-2 (Q:27) | No negative zero in decimals: refuses `-0.0`, `-0.000` | both | IN SCOPE |
| Q-DEC-3 (Q:28) | `integer-str` and `format: int64` accept `-0` | both | IN SCOPE |
| Q-FMT-1 (Q:32) | `uri`: has a scheme, may have a fragment, ASCII only | both | IN SCOPE |
| Q-FMT-2 (Q:33) | uuid = lowercase 8-4-4-4-12; version and variant not checked | both | IN SCOPE |
| Q-TIME-1 (Q:34) | JSON instants: RFC 3339 UTC `Z`, exactly six fractional digits; decimals with exactly s digits | both | IN SCOPE (R12 "one timestamp precision") |
| Q-PROF-1..15 (Q:38–52) | Profile lint rules (`$defs` root only, no recursion; `$ref` siblings limited; boolean schemas only as items/additionalProperties; every schema typed unless `$ref`/`anyOf`/`oneOf`; `required` ⊆ properties; defaults valid; identifiers that may be null: `Isin`, `Cusip`, `Sedol`, `Figi`, `Lei`; maps allowed in requests; `multipleOf` integers only; `x-atlas-enum` lists every value; `x-atlas-deprecated` needs `reason`, `since`, `sunset`, `replaced_by`; depth ≤ 32 levels counting root 1, size ≤ 1 MiB of RFC 8785 form; Money only `amount` and `currency`; `oneOf` with a const discriminator) | gate | IN SCOPE (journey profiles pass the gate, REV:30) |
| Q-DOC-2 (Q:57) | Bootstrap sign-in names every endpoint; client builds none | client | IN SCOPE (with R26) |
| Q-DOC-3 (Q:58) | `profile.schema.json` does not apply the meta-schema to `schemas.*`; a 1.0 client must not refuse a 1.1 logical type; the gate checks | client, gate | IN SCOPE |
| Q-DOC-4 (Q:59) | `delivery` is the interaction's; a binding may override it | both | IN SCOPE |
| Q-DOC-5 (Q:60) | `properties.deprecated` (a date) is added beside `sunset`, `replaced_by` | both | IN SCOPE (CORE-DEPR-001) |
| Q-DOC-6 (Q:61) | Profiles carry no Arrow schema | gateway | IN SCOPE |
| Q-DOC-7 (Q:62) | Version-boundary lens: `evolution.previous_version` and `evolution.version_lens` | gate | experimental (R03) |
| Q-DOC-8 (Q:63) | Schema documents the registry's status and retryable but does not enforce them | client | IN SCOPE |
| Q-DOC-9 (Q:64) | `details` items: `{code, target, keyword}` per validation failure; `{hop, ...}`; `{challenge}` for a claims challenge | both | IN SCOPE. **AMBIGUOUS:** CORE-CALL-002's details carry `{target, keyword}` with no `code` (problem matching is "contains the members given", so `code` may be present) |
| Q-DOC-10 (Q:65) | `estimate.amount`: a decimal string, ≤ 12 integer and 3 fractional digits | both | DEFERRED with R06 |
| Q-DOC-11 (Q:66) | The operation document carries `profile`, the exact-id `result` was written under | both | IN SCOPE if operations are |
| Q-DOC-12 (Q:67) | A handoff's `operation` is an operation name; re-asking answers a fresh handoff document | — | DEFERRED |
| Q-DOC-13 (Q:68) | Emulator credential member `connection_string` | — | DEFERRED |
| Q-DOC-14 (Q:69) | Core content: transfer file may carry `inline` base64 under 1 MiB | both | DEFERRED (content) |
| Q-DOC-15 (Q:70) | Truncated end line: `truncated: true`, problem `output_limit_exceeded` (status 200) → a client **warning**; any other end-line problem raised after the rows | both | IN SCOPE (STREAM-003/004; AC:206) |
| Q-DYN-1 (Q:71) | DynamicSchema JSON answer `{"columns": [{"name","type"}], "rows": [...]}` | both | not needed by the journey |
| Q-BI-1 (Q:75) | atlas.search `page_size` 1..500, default 100 | gateway | IN SCOPE (search) |
| Q-BI-2 (Q:76) | atlas.guide `revisions` is an object, members not fixed | both | IN SCOPE (R01) |
| Q-BI-3 (Q:77) | atlas.operation `cancel`/`watch` take `operation_get_request` `{name}` | both | IN SCOPE if operations are |
| Q-BI-4 (Q:78) | atlas.directory and atlas.audit answer pages | gateway | later |
| Q-BI-5 (Q:79) | atlas.compare `from`/`to` are grammar `version` (`v1`) | gateway | later |
| Q-RES-1..8 (Q:83–90) | Resolution edge readings (target top-level; rounding scale; promote date-time→date only at midnight UTC; nested unknown members kept unless converted; one lens per profile spans request and rows; `convert_unit` spellings `Bp`/`bp`; promote types) | both | Experimental resolver (REV:90). Q-RES-5 (one lens reaching requests and rows) is **OVERRIDDEN BY REVIEW (R04)**: "Aliases carry their schema scope" (REV:62) |
| Q-HDR-1 (Q:94) | A value that parses as its SF type but breaks the §9.6.3 grammar is treated as **absent** | both | IN SCOPE |
| Q-HDR-2 (Q:95) | Atlas-Deadline is whole seconds; client **rounds down** | client | IN SCOPE (CORE-DEADLINE-001) |
| Q-HDR-3 (Q:96) | rfc850 two-digit year read with reference year 2026 | both | IN SCOPE |
| Q-HDR-4 (Q:97) | An Integer in Atlas-Confirm is absent; senders write `1250.0` | both | DEFERRED with R06 |
| Q-HDR-5 (Q:98) | No Accept or `*/*` → the binding's first encoding | gateway | IN SCOPE |
| Q-HDR-6 (Q:99) | Missing or absent required header → `invalid_request` with the header name as target ("natural reading"; no vector) | gateway | IN SCOPE, **AMBIGUOUS** (unsettled) |
| Q-SF-1 (Q:100) | Base64 with surplus padding fails the parse (follow RFC, not http_sf) | both | IN SCOPE (Phase 1) |
| Q-SNOW-1..4 (Q:11, Q:105–107) | Assumed chunk encoding; floored epochs; truncate to µs; NUMBER(38,0) vs integer is a mismatch (cast to NUMBER(18,0)) | adapter | Q-SNOW-4 IN SCOPE for planned SQL casts; the rest DEFERRED with handoff |
| Q-GUARD-1..3 (Q:112–114) | see section 5 | gateway | IN SCOPE |
| Q-RUN-1 (Q:118) | `guide` and `explain` are runner ops | runner | IN SCOPE |
| Q-RUN-2 (Q:119) | CLI runner's failed result carries `cli_exit`; checked only for a CLI | CLI | IN SCOPE |
| Q-RUN-3 (Q:120) | Deprecation warning code `deprecated` | client | IN SCOPE |
| Q-RUN-4 (Q:121) | Options `path`, `max_memory`, `replace`, `if_match`, `fields`, `set`; content value forms; manifest `{"files": [...]}` | runner | partly IN SCOPE (`if_match`/`fields`/`set` with resources: DEFERRED) |
| Q-RUN-5 (Q:122) | `call` on operation → operation doc; `operation.wait` → result; transfer → waits and lands; end-line `usage` → result `usage` | runner | IN SCOPE (usage); operations conditional |
| Q-RUN-6 (Q:123) | A failed result may carry `value` (rows before the failure) | runner | IN SCOPE |
| Q-RUN-7 (Q:124) | Subscription named by command id; `subscribe.next` timeout → `{"none": true}` | runner | DEFERRED |
| Q-EVT-1 (Q:125) | publish body is the payload; gateway validates; client then sends the same payload as a CloudEvent | both | DEFERRED |
| Q-SCN-1..4 (Q:126–129) | setup and suite commands; four listeners; default behaviour designed in README; §9.8.3 example rows abbreviated | suite | IN SCOPE |
| Q-ARROW-2 (Q:155) | Other string formats and every string logical type → Utf8; precedence: logical type, then format, then JSON type | both | where Arrow is offered |
| Q-ARROW-3 (Q:156) | Unmappable property → table schema refused `invalid_document` at that property; list child field named `item` | both | where Arrow is offered |
| Q-ARROW-4 (Q:157) | Field metadata value = RFC 8785 form of the keyword's JSON text (`"Bp"` with quotes); schema metadata `atlas:profile`, `atlas:protocol` plain text | both | where Arrow is offered |
| Go-side table #11 (Q:145) | References require all four `x-atlas-deprecated` members (differs from the Go draft, which required only `reason` and `since`) | gate | IN SCOPE: follow the references |

---

## 5. guard.json: the read-only guard as the vectors encode it (§6.4.3)

**Status in repo.** `gateway/adapter/snowflake/guard.go` (246 lines) and `guard_test.go` exist. The test reads `vectors/guard.json` and compares `Accepted`, `Reason` and `Objects`, **in order** (`slices.EqualFunc(..., slices.Equal)`). It also fuzzes the guard ("fails closed").
- **AMBIGUOUS / wrong id:** that test is labelled `// ATLAS-DATA-006`. ATLAS-DATA-006 is "An export's files are readable only through its links, which expire, and only by the caller" (`02-2-data-surface.md:55`). §6.4.3 (`06-4-data-sources.md:84`) has no requirement id for the guard; I found none by searching the spec's requirement lines. Needs a correct id, or one recorded as missing.

**Spec text** (06-4-data-sources.md:84): "Every statement is parsed by the library's Snowflake parser before it runs, and refused unless it is one read-only query over the objects its capability may read (`snowflake.query`: the registered schemas). The guard is never the only line: the connection's role can only read. The guard's cases are language-neutral vectors (accepted and refused statements), generated in `tools/spec/`." REV:21 and REV:32: sqlglot sits behind the Atlas compiler and guard (step 4).

**Vector file** (GV):
- `description`: "every statement is refused unless it is exactly one read-only query (SELECT, UNION, INTERSECT, EXCEPT, WITH) over the registered objects…". Generated with sqlglot 30.27.0's Snowflake parser.
- `registered`: `[["HARBOUR","GOLD"],["HARBOUR","SILVER","PRICES"]]`. A `[database, schema]` pair registers every object in the schema; a triple registers one object. Identifiers are compared "as Snowflake resolves them (unquoted upper-cased, quoted exact)".
- Case shape: `{name, statement, expected: {accepted: true, objects: [[db,schema,object],...]} | {accepted: false, reason}, note?, question?}`.

**Rule order.** The first refusing rule wins (GPY:5–23; `ORDER` GPY:34–36). The GV description lists only from `dml` onward. The three pre-checks `parse_error`, `empty`, `multiple_statements` come first (GPY:34).

| # | reason | Rule (GPY) |
|---|---|---|
| 1 | `parse_error` | "the parser cannot read it" |
| 2 | `empty` | "no statement" |
| 2 | `multiple_statements` | "more than one (a single trailing semicolon is allowed; an empty statement after it is not)" (Q-GUARD-2) |
| 3 | `dml` | INSERT, UPDATE, DELETE, MERGE |
| 3 | `ddl` | CREATE, ALTER, DROP, TRUNCATE |
| 3 | `copy` | COPY INTO |
| 3 | `file_transfer` | PUT, GET |
| 3 | `call` | CALL |
| 3 | `not_a_query` | "everything else: SHOW, DESCRIBE, USE, GRANT, BEGIN, EXPLAIN, EXECUTE IMMEDIATE ..." |
| 3 | `select_into` | "SELECT ... INTO" writes |
| 4 | `stage` | "a stage reference (@name, @~, @%table), wherever it appears" |
| 5 | `table_function` | "TABLE(...), LATERAL FLATTEN and every other table function (RESULT_SCAN among them)" (Q-GUARD-3) |
| 6 | `system_function` | "a function whose name begins SYSTEM$" (any case, quoted, qualified) |
| 7 | `dynamic_identifier` | "IDENTIFIER(...), whose object cannot be checked" |
| 8 | `unqualified_object` | "a table that is not a CTE in scope and is not named database.schema.object" (Q-GUARD-1) |
| 9 | `unregistered_object` | "an object neither registered itself nor in a registered schema … CTE names are not tables" |

Mechanics in the reference (GPY:97–160):
- The statement kind (rule 3) returns at once.
- Rules `select_into` .. `unregistered_object` are **collected over the whole tree**, then the first in `ORDER` is reported.
- Stage detection uses the tokenizer (a `PARAMETER` token with text `@`) plus `Table` nodes whose name starts `@`.
- CTEs in scope are every `WITH` on the way up from the table node. A CTE defined inside a subquery is not visible outside it (case "a CTE name out of scope" → `unqualified_object`).
- `objects` are unique triples in **first-encounter order of `stmt.walk()`** (GPY:156–160).
- **AMBIGUOUS:** is the order of `objects` significant? The Go test compares in order. The vector description does not say. Readings: (a) order is significant, so the Go guard must match sqlglot's walk order, which is breadth-first by default in sqlglot; (b) compare as a set. Every multi-object vector lists HOLDINGS before PRICES in source order, so both readings pass the current cases.

**Case counts by category** (65 total; counted from GV):

| Outcome | Count | Cases (by `name`) |
|---|---|---|
| accepted | 15 | a select; a registered object; unquoted names are upper-cased; a trailing semicolon; a join and a subquery; WITH: a CTE name is not a table; a CTE that reads another CTE; a recursive CTE (`objects` []); UNION ALL; INTERSECT; EXCEPT (parenthesised); no table at all (`SELECT 1`, []); VALUES ([]); time travel reads only (`AT(OFFSET => -60)`); a comment hides nothing (`-- ; DROP TABLE x`) |
| parse_error | 1 | unparsable (`SELEC 1`) |
| empty | 1 | `"   "` |
| multiple_statements | 3 | two statements; an empty second statement (`SELECT 1;;`, Q-GUARD-2); a query then DML |
| dml | 5 | INSERT; UPDATE; DELETE; MERGE; WITH, then INSERT |
| ddl | 4 | CREATE TABLE AS SELECT; DROP; ALTER; TRUNCATE |
| copy | 2 | COPY INTO a stage; COPY INTO a table |
| file_transfer | 2 | PUT; GET |
| call | 1 | CALL |
| not_a_query | 7 | SHOW; DESCRIBE; USE; GRANT; BEGIN; EXPLAIN ("read-only, but not a query"); EXECUTE IMMEDIATE |
| select_into | 1 | SELECT INTO writes |
| stage | 5 | a stage in FROM (`@in`); the user's stage (`@~/staged/f.csv`); a table's stage (`@%holdings`); a stage in a subquery; a stage as a function's argument (`GET_PRESIGNED_URL(@out, 'f.csv')`) |
| table_function | 4 | TABLE(RESULT_SCAN(...)); TABLE(GENERATOR(...)); a registered schema's table function; LATERAL FLATTEN (Q-GUARD-3) |
| system_function | 4 | `SYSTEM$WHITELIST()`; `system$typeof(1)`; quoted `"SYSTEM$TYPEOF"(1)`; qualified `HARBOUR.GOLD.SYSTEM$X(1)` |
| dynamic_identifier | 1 | `IDENTIFIER('HARBOUR.GOLD.HOLDINGS')` |
| unqualified_object | 4 | `GOLD.HOLDINGS` (Q-GUARD-1); `HOLDINGS` (Q-GUARD-1); `HARBOUR..HOLDINGS` (Q-GUARD-1, "the default schema"); a CTE name out of scope |
| unregistered_object | 5 | `HARBOUR.BRONZE.RAW`; `HARBOUR.SILVER.TRADES` ("SILVER is registered object by object"); a join with `SNOWFLAKE.ACCOUNT_USAGE.QUERY_HISTORY`; `"harbour".GOLD.HOLDINGS` ("\"harbour\" is not HARBOUR"); `HARBOUR.INFORMATION_SCHEMA.TABLES` |

QUESTIONS readings (Q:112–114):
- **Q-GUARD-1:** "An object must be named `database.schema.object`. One-part and two-part names (and `db..object`) are refused, because the session's defaults could point anywhere."
- **Q-GUARD-2:** "One trailing semicolon is allowed. An empty statement after it (`;;`) counts as a second statement."
- **Q-GUARD-3:** "Every table function is refused, including `LATERAL FLATTEN` over a registered table's column. User-defined functions in a SELECT are not checked; the read-only role underneath is the line there."

**Gap (not a vector):** `EXPLAIN SELECT 1` is refused as `not_a_query`. The rows explain plan in CORE-EXPLAIN-001 contains `TABLE(GENERATOR(ROWCOUNT => 3))`, which this guard refuses as `table_function`. That plan is a scripted body only, never guarded. So a real `conformance.rows` provider cannot render rows that way through the guard. Noted only so nobody "fixes" the guard to accept it.

---

## 6. The vectors (listing; Phase 1 already passes them, REV:70)

| File | What it holds | Cases |
|---|---|---|
| `vectors/arrow.json` | §9.2.7: table schemas → derived Arrow schemas (integration-test JSON form), field order = `properties` order, Atlas keywords as `atlas:<keyword>` metadata (RFC 8785 values); unmappable refused at the property | 17 |
| `vectors/fingerprints.json` | §9.4.4: profile → RFC 8785 canonical text, SHA-256, fingerprint (first 16 hex) | 9 |
| `vectors/grammar.json` | §9.1.3–§9.1.5: `rule`, `input`, `expected.valid`, `why` (`"abnf"` or the sentence) | 229 |
| `vectors/guard.json` | §6.4.3 read-only guard (section 5) | 65 |
| `vectors/headers.json` | §9.6.3: every Atlas header's field lines → SF parse, Atlas `value`, or `absent` with `why` | 98 |
| `vectors/problems.json` | §9.3.3 registry: status, retryable, type URI, CLI exit (table 1.13) | 25 |
| `vectors/resolution.json` | §9.5: chain, writer, reader, record, columns → expected record or `resolution_failed` + target; row-wise and column-wise | 42 |
| `vectors/schemas.json` | §9.2: `validate` (119: errors as `{pointer, keyword}` sorted) and `profile` (71: accepted or `invalid_document` with `target` and `rule`) | 190 |
| `vectors/structured-fields.json` | RFC 9651 corpus, each as Item, List, Dictionary: `{ok, serialised}` | 4089 |
| `vectors/profiles/action.json` | Agent-ready action profile `astronomer.deployment@v1#5bfc6de6fd4716cb` | 1 |
| `vectors/profiles/data.json` | Agent-ready data profile **`portfolio.holdings@v1#8e61efdf45779059`** (`valid: true`, `agent_ready: true`); the journey's capability, a reference for its profile | 1 |
| `vectors/profiles/event.json` | Agent-ready event profile `data.landed@v1#e2c614c989692ac3` | 1 |
| `vectors/profiles/resource.json` | Agent-ready resource profile `nautilus.dashboard@v1#d2089e11eaf694e1` | 1 |
| `vectors/handoff/snowflake-arrow-1/synthetic/` | `cases.json` + `.arrows` chunks for 11 cases: every-type, widths-change, timestamp-scales, mismatch-type, mismatch-name, mismatch-missing-column, mismatch-number-38-0, mismatch-scale, mismatch-null, mismatch-source-schema, mismatch-second-chunk (18 files). DEFERRED (handoff) | 11 |

Not in my assignment and not read: `schemas/*.schema.json` (21) and `grammar.abnf` (RD:33–34).

---

## 7. Ambiguities, collected

Each is detailed above at the cited place.

1. `calls` in the §9.8.3 result example is undefined (1.3).
2. `execution` option undefined (1.4).
3. "arrays element by element" matching (1.7).
4. How date-times and decimals are recognised for comparison, and what "by value at their scale" means (1.7).
5. `text_contains` is an array (1.7).
6. `header … present` and the `header_features` member names (1.8).
7. `nth` with `times` > 1 (1.9).
8. Does `nth` count only body-matching requests (1.9)?
9. `body_contains` semantics (1.9).
10. §9.8.5's no-skip rule versus deferred Core scenarios (1.10).
11. CLI, Python and Node runner shapes (1.12).
12. The operation polling address (2.1).
13. The R26 conflict with the bootstrap-named authority and the token file keyed by environment (2.2, 2.3).
14. Who excludes `critical@v1` from the working view (3.2).
15. CORE-UPGRADE-001's note versus its expectations (3.3).
16. CORE-CONFIRM-001's idempotency key: spec vs scenario vs R08 (3.3).
17. CORE-LOCK-001: R02, R03 and the open lock format (3.1).
18. CORE-OP-001 and COL-001/002 scope (3.1).
19. Q-DOC-9 `details.code` absent in CORE-CALL-002 (4).
20. Q-HDR-6 unsettled (4).
21. The order of guard `objects` (5).
22. The guard test's requirement id ATLAS-DATA-006 is wrong (5).
