# The Atlas Protocol's references (atlas/1.0)

The machine-readable part of §9 (`spec/protocol.md`): the JSON Schemas of its documents, its grammar, the test vectors of its algorithms, and the conformance scenarios. Together with the text, these are what a client in any language is written or generated from (§9, Purpose).

Everything here is written by `tools/spec/` (Python) from the spec's text, with its own small reference implementations of the algorithms. Nothing comes from an implementation's output: these files are the independent check of every implementation, Go's first (D22). Where the text is ambiguous or silent, the reading taken is in [QUESTIONS.md](QUESTIONS.md), and every vector case that depends on one names it (`"question": "Q-..."`).

## The one command

From the repository's root:

```bash
uv run --project tools/spec python -m atlasref check
```

It regenerates every file below in memory. It fails unless each is byte-identical to the file on disk, and unless every check of "What check proves" passes. To regenerate after changing a generator:

```bash
uv run --project tools/spec python -m atlasref write
```

The reference implementations' own tests, each proving an algorithm against cases hand-worked from the spec's examples (and running `check` itself):

```bash
uv run --project tools/spec pytest tools/spec
```

The environment is `tools/spec/.venv`. The packages are pinned in `tools/spec/uv.lock`: rfc8785, jsonschema, pyarrow, sqlglot 30.27.0, http-sf (and pytest).

## The files

| Path | Covers | Written by |
|---|---|---|
| `schemas/*.schema.json` | §9.3.1: the 21 documents, each with `$id` `https://atlas.jhg.com/protocol/1.0/<file>` | `atlasref/schemas.py` |
| `grammar.abnf` | §9.1.3–§9.1.5: every rule, case-sensitive with `%s`, plus the strings §9 defines elsewhere | `atlasref/grammar.py` |
| `vectors/fingerprints.json` | §9.4.4: profiles, their canonical form, SHA-256 and fingerprint | `atlasref/gen/fingerprints.py` |
| `vectors/profiles/{data,resource,action,event}.json` | Complete, agent-ready profiles, one per family (§3.1.3, §3.1.4a) | `atlasref/profiles.py` |
| `vectors/problems.json` | §9.3.3's registry and §9.7.6's CLI exit codes | `atlasref/problems.py` |
| `vectors/grammar.json` | §9.1: strings each rule accepts and rejects, and why | `atlasref/gen/grammar.py` |
| `vectors/schemas.json` | §9.2: validation results with pointers and keywords; schemas the profile refuses | `atlasref/gen/schemavectors.py` |
| `vectors/headers.json` | §9.6.3: every Atlas header, its Structured Field parse and Atlas reading, or why it is absent | `atlasref/gen/headers.py` |
| `vectors/structured-fields.json` | RFC 9651: 4,089 field values read as Item, List and Dictionary | `atlasref/gen/sf.py` |
| `vectors/arrow.json` | §9.2.7: table schemas and their derived Arrow schemas | `atlasref/gen/arrow.py` |
| `vectors/resolution.json` | §9.5.8: every case it lists, and §9.5.3–§9.5.5's edges | `atlasref/gen/resolution.py` |
| `vectors/handoff/snowflake-arrow-1/synthetic/` | Snowflake chunks in the assumed encoding, their handoff documents, the expected Atlas batches | `atlasref/gen/handoff.py` |
| `vectors/guard.json` | §6.4.3: statements the read-only guard accepts or refuses | `atlasref/gen/guard.py` |
| `scenarios/<ID>.json` | §9.8.4: the 50 scenarios, one file each | `atlasref/gen/scenarios.py` |
| `scenarios/fixtures/gateway.json` | What every scenario's conformance gateway serves | `atlasref/conformance.py` |
| `README.md`, `QUESTIONS.md` | This page; the readings the text leaves open | By hand |

### Schemas

The 21 documents: bootstrap, binding, profile, catalog-page, governance, describe, explain, problem, operation, page, resource, handoff, connect, ticket, transfer, event, chunk-end, lock, token-store, builtins (one `$defs` entry per built-in request and result of §9.3.2), and atlas-schema (the meta-schema for every schema inside a profile, §9.2.1–§9.2.5). Each is valid JSON Schema 2020-12. Their conventions:

- **Open and closed.** Every document the gateway sends is open: a receiver ignores members it does not know (§9.1.2 rule 7). Only request bodies (the built-ins' `*_request`, the transfer's `commit_request`) are closed with `"additionalProperties": false`.
- **UNKNOWN.** Every enum in a document the gateway sends includes `UNKNOWN`. A client maps a value it does not know to it before validating (Q-DOC-1), so a newer gateway's documents validate for an older client. No request's enum includes it.
- **Strings.** The grammars that §9.2.2 names as formats (`date`, `date-time`, `duration`, `decimal`, `int64`, `uri`, `uuid`, `capability-id`, `pinned-id`, `exact-id`, `address`) are written as `format`. Validate them as assertions, as §9.2.2 defines them. The other grammars of `grammar.abnf` are written as `pattern`, each an I-Regexp anchored `^ R $`.
- **References.** Documents refer to each other by `$id`; load all 21 into one registry.
- **What profiles embed.** `resource.schema.json`'s `$defs` (`name`, `metadata`, `status`) are written in the Atlas Schema profile itself. A resource profile's record schema declares them exactly so (§9.3.4 rule 5). The problem registry is `problem.schema.json`'s `$defs/registry_code` (the codes) and `$defs/registry` (each code's status and retryable).
- **atlas-schema.schema.json** checks everything a meta-schema can: the allowed keywords and where, `$schema` and `$vocabulary`, the formats, `type`'s forms, the Atlas keywords' shapes, the logical types and units. What it cannot check is listed in its `description` and tested in `vectors/schemas.json`: I-Regexp validity, depth and size, requests closed and others open, UNKNOWN, `$ref` targets and recursion, discriminated `oneOf`, keys.

### Vectors

Every vector file is I-JSON: `{ "description", "cases": [ { "name", ...inputs, "expected" | "problem": { "code", "target"? } } ] }` (§9.8.2). Numbers compare as binary64 values. An object's member order carries no meaning, except a schema's `properties`, whose order is the Arrow field order and §9.5.2's matching order (Q-ARROW-1); read them keeping member order.

- **fingerprints.json.** Each case gives a profile and its `expected` `canonical` (UTF-8 text), `sha256` and `fingerprint`. The cases are: a complete data profile; the same profile with every removed member changed and its members reordered (the same fingerprint); a column added; non-ASCII text, a quote and a tab; every control-character escape; UTF-16 member order; ECMAScript numbers; and prose at every schema position StripSchema walks.
- **profiles/*.json.** One case each: the profile as served (with `fingerprint`), and its `exact` id. It validates against `profile.schema.json`, and each schema against `atlas-schema.schema.json` in its role (one an interaction takes is a request). It passes the lint and the agent-readiness check.
- **grammar.json.** `rule`, `input`, and `expected.valid`. A rejection's `why` is `"abnf"` (grammar.abnf refuses the string) or the sentence of the text that does: a length, `-0`, a real date or time.
- **schemas.json.** Kind `validate` gives a `schema` and an `instance` (or `instance_text`, the exact JSON text, where its spelling is the point). Its `expected` is `{valid, errors: [{pointer, keyword}]}`, sorted by pointer then keyword (Q-VAL-1); the first error is invalid_request's target. Kind `profile` gives a `schema` in a `role`. It is accepted, or refused with `invalid_document` (target the offending member's pointer, `rule` the rule it breaks); each refused case breaks exactly one rule. `meta_schema` says whether atlas-schema.schema.json alone refuses it.
- **headers.json.** `header` and its field `lines`. `expected.sf` is the RFC 9651 parse in the form of [httpwg/structured-field-tests](https://github.com/httpwg/structured-field-tests). `expected.value` is what Atlas reads, or `expected.absent` (with `why`) when a receiver treats the header as absent.
- **structured-fields.json.** `field`, and for each of item, list and dictionary `{ok, serialised}`. The serialisation is `""` for an empty list or dictionary (the field is not sent).
- **arrow.json.** A table `schema`, the `exact` id for `atlas:profile`, and the derived Arrow schema in the JSON form of Apache Arrow's integration tests (`fields`: name, nullable, type, children, metadata as key/value pairs). A schema the table cannot map is refused, at its property.
- **resolution.json.** `chain`, oldest first, each step a profile's exact-id, its schema for this kind of record (`record_kind`), and its lens from the step before. Then `writer`, `reader`, `record` and `columns`, and the expected record, or resolution_failed with its target. Every row case also holds column by column (§9.5.7).
- **handoff/snowflake-arrow-1/synthetic/cases.json.** Each case gives the writer profile (`writer`, `result_schema`, the derived `arrow_schema`), the `source_schema`, the chunk files with their SHA-256 and the `handoff` document. Then the expected batches, one per chunk, as JSON rows and as `expected.arrows` (an Arrow IPC stream), or invalid_document with `why`. The encoding is assumed (Q-SNOW-1): captured chunks replace these when the Snowflake spike runs.
- **guard.json.** A `registered` set (identifiers as Snowflake resolves them), and for each `statement`, `accepted` with the `objects` it reads, or the `reason` it is refused (the first rule that refuses it, in guard.py's order).
- **problems.json.** One case per code, in the registry's order: its status, retryable, type URI and CLI exit code.

## Scenarios

Each `scenarios/<ID>.json` is one scenario of §9.8.4's table:

```json
{ "id", "level", "description",
  "setup":    { "env": {...}, "files": [...], "cwd": "..." },
  "gateway":  { ...the conformance gateway's script... },
  "commands": [ { "id", "op", "args" } ],
  "expected": [ { "id", "ok", ... } ],
  "expected_requests": [ { "assert", ... } ] }
```

The suite starts the conformance gateway, loads the scenario (`POST /_conformance/scenario {"scenario": "<id>"}`), prepares the client's environment from `setup`, starts the client's runner, and sends `commands` one line at a time. It compares each result line with `expected`, and finally `GET /_conformance/requests` against `expected_requests`. `note` and `questions` explain a scenario; they are not checked.

### Placeholders

Strings may hold these placeholders, which the suite substitutes:

- `{gateway}`: the conformance gateway's base URL. `{gateway2}` to `{gateway4}` are the further listeners CORE-BOOT-002 needs.
- `{ATLAS_HOME}`: the fresh directory ATLAS_HOME names. `{WORKDIR}`: the fresh directory the runner starts in.
- `{today}`: the UTC date, `YYYY-MM-DD`.
- `{operation:N}`: the operation name command N returned. `{value:N}`: command N's value, and `{value:N/<pointer>}` a member of it.
- `{sha256:<path>}`: a file's SHA-256.
- `{vectors:<file>#<case>}`: that vector case's expected rows, every batch in order.

Two objects stand for times: `{"$now_plus_s": n}` (epoch seconds) and `{"$now_plus_iso": n}` (RFC 3339).

### setup

- `env` sets the runner's environment: ATLAS_URL, ATLAS_HOME, ATLAS_CONFORMANCE=1, and what the scenario adds; `null` unsets.
- `files` are written before the runner starts: `path`, `mode` (and `dir_mode` for its directory), with `json`, `text` or `base64` content.
- `cwd` is the runner's working directory.

### commands

`op` is a runner operation of §9.8.3 (connect, whoami, catalog, search, describe, guide, explain, access, call, fetch, operation.get, operation.wait, operation.cancel, subscribe.open, subscribe.next, subscribe.close, publish, export, upload, version), or a suite command the runner never sees:

- `suite.restart`: stop the runner and start it again, with `env` changes; ATLAS_HOME is kept.
- `suite.file`: write a file (`path`, `size`, `pattern`, or content).
- `suite.expect_file`: assert a file exists (`mode`, `dir_mode`, `json_contains`, `base64`), or is `absent`; `*` matches one path segment.

A suite command's expected result is `{"id", "ok": true}`.

Beyond §9.8.3, `call`'s options may carry `path`, `max_memory`, `replace` (§9.7.13), `if_match`, `fields` and `set` (Q-RUN-4). `call` on an operation delivery returns the operation document; operation.wait returns its result. On a transfer delivery, `call` returns what landed: `{"content": {"base64", "metadata"}}` in memory, or `{"manifest": {"files": [...]}}` on disk (Q-RUN-5). `subscribe.open` names its subscription by its command's id (Q-RUN-7).

### expected

- `{"id", "ok": true}` matches any success; `value` must equal the result's value.
- `value_contains` must be contained in it: objects by the members given, arrays element by element. The special members are `{"$count": n}` (items yielded), `{"$length": {"/pointer": n}}`, `{"$in": [...]}`, `{"$present": true}` and `{"$uuid": true}`.
- `warnings`: each `{code, text_contains}` must match one the client emitted.
- `usage`: the end line's usage.
- `{"id", "ok": false, "problem": {...}}`: the problem must contain the members given (`code`, `target`, `details`, ...). A CLI runner reports `cli_exit` (Q-RUN-2).

Values compare as JSON with numbers as binary64. Date-times compare by instant to the microsecond, and decimals by value at their scale.

### gateway, the script

Members, all optional; anything not scripted is served by the default behaviour below:

- `bootstrap`: deep-merged into the fixtures' bootstrap.
- `current`: which profile a pinned id is served under (e.g. `{"conformance.echo@v1": "<exact-id>"}`). `serve`: further fixture profiles to serve.
- `tokens`: `{access: [accepted tokens], refresh: {refresh_token: {access_token, refresh_token, expires_in}}}`, replacing the fixtures'.
- `headers`: extra response headers, by request path or by address.
- `state`: initial state for a capability (`things`, `files`, `catalog_page_size`, `export_parts`, `handoff_case`).
- `steps`: response overrides, tried in order. Each is `{match, respond, times}`.
  - `match` takes `address` (an interaction's), `path`, `method`, `nth` (the request's ordinal among those to that address or path, from 1), `body_contains`, `header_equals`, `listener`, or a storage request: `storage_part` k, `storage_chunk` k, `storage_upload`.
  - `respond` takes `status`, `headers`, and one of: `problem` (members; the gateway fills type, status, request_id and next), `body` (JSON, or null for none), `serve: true` (the default behaviour), `stream` (`encoding` ndjson or arrow, `rows`, and `end`: an end line, `"default"`, or null for none), `describe_profile` (atlas.describe answering this profile document), or `profile` (answer under this writer exact-id).
  - Modifiers: `delay_ms`; `expire_links` (expire every link the current transfer or handoff issued); `before` (`set_status` on a thing); `deleting`.
  - A step answers `times` matching requests (default 1).
- `bus`: what the Service Bus emulator does. Each entry is `{after: "connected" | "delivered:<event id>", send: [CloudEvents]}` or `{after, revoke: true}`.

### expected_requests

Assertions on what the gateway recorded. A selector `{address | path | method | nth | body_contains | header_equals | listener | storage_* | path_prefix | path_suffix}` picks requests.

| assert | Holds when |
|---|---|
| `count` | `equals` requests match `select` |
| `header` | on the selected request, `name` is `absent`, or `parses` as its §9.6.3 type (headers.json's reading), or its Atlas reading `equals` a value, or `contains` a substring |
| `body` | the body `equals` (as JSON), `contains`, has the RFC 8785 form `jcs`, or (a form body) `form_contains` |
| `body_member_in` | the body's `member` (a pointer) is one of `values` |
| `same_header` | header `name` is the same on every request of `selects` |
| `interval` | at least `min_ms` passed between request `from` and request `to` |
| `order` | `selects` arrived in that order |
| `deadline` | Atlas-Deadline minus the request's arrival is between `min_s` and `max_s` |
| `describe_exact` | atlas.describe was asked for `exact` `count` times |
| `poll_intervals` | the polls of `operation` came at least `min_ms[i]` after the previous answer |
| `cursor_chain` | each selected request carries the `next_cursor` of the previous answer |
| `max_body` | every selected request's body is at most `bytes` |
| `header_features` | Atlas-Client-Features holds the tokens given |
| `bus`, `bus_connections` | the emulator received `count` events containing `contains` (with `uuid`, `recent` and `equals_client_name` pointers), or this many connections |

### The conformance capabilities

`scenarios/fixtures/gateway.json` holds every profile the conformance gateway serves by exact-id: the 15 capabilities of §9.8.4 (conformance.evolving@v1 as #A, #B and #C, whose two lenses hold one of each lens operation), two variants of conformance.echo@v1 (CORE-CALL-003 and CORE-TOL-001), and the atlas.* built-ins. It also holds which profile is current for each pinned id, the bootstrap, and the tokens. Their default behaviour (Q-SCN-3), each answer under the current profile:

| Capability | Answers |
|---|---|
| `conformance.echo@v1:fetch` | The request, with `count` 1, `kind` `plain`, `ratio` and `when` null and `tags` [] when absent |
| `conformance.rows@v1:fetch` | Rows 0 to n-1: `i`, `label` `row-<i>`, `amount` i × 1.5 (Decimal(18,6)), `at` 2026-01-01T00:00:00Z + i s + i µs, `day` 2026-01-01 + i days, `share` i/100 or null when i mod 5 = 4. Projected to `columns`; NDJSON, Arrow or a JSON array by Accept; date-times with six fractional digits |
| `conformance.rows@v1:explain`, `:export` | The plan of that fetch; Parquet files in `export_parts` parts (default 1) at `{gateway}/storage/...` |
| `conformance.items@v1:fetch` | Items `{k, name: item-<k>}`, `page_size` to a page, with opaque cursors |
| `conformance.slow@v1:fetch` | 202 with Retry-After 1; `polls` polls answer `running`, then `succeeded` with `{done: true, padding: "x" × result_bytes}`, by `result_location` when it is over 1 MiB; `:cancel` makes it `cancelled` |
| `conformance.evolving@v1:fetch` (current #C), `@v2` | Positions for `ids`; a request under an older profile is resolved forward first; @v1 is deprecated, with Deprecation and Sunset |
| `conformance.retired@v1` | `retired` |
| `conformance.critical@v1` | A profile with the critical trait `ConformanceOnly`; its interaction answers `{ok: true}`, and no conforming client calls it |
| `conformance.costly@v1` | `confirmation_required` (estimate 12.500 credits) unless Atlas-Confirm is at least 12.5, then `{ok: true}` |
| `conformance.dynamic@v1` | `{columns, rows}` for its query (Q-DYN-1) |
| `conformance.handoff@v1` | With `execution: handoff` and a client that declares it, the handoff document of `handoff_case` (default every-type) of the synthetic vectors, its chunks served from the gateway's storage; otherwise the same rows streamed |
| `conformance.thing@v1` | An in-memory store with the envelope; a fake controller sets `ready`; `If-Match` checked (412), field masks checked (`invalid_request` on a server-owned member), idempotency keys replayed and in conflict (409) |
| `conformance.file@v1` | `get`: 202, then a transfer download (inline under 1 MiB, else parts); `publish`: 202, then a transfer upload in parts of 8 MiB and a commit binding |
| `conformance.change@v1` | `plan`, then `run` answers `approval_required` until `approve` |
| `conformance.tick@v1` | `subscribe` and `publish` answer an emulator connect handover; `bus` sends the events |

## What check proves

1. Every file regenerates byte-identically. No generator writes a file twice, and nothing under this directory is left that no generator makes, except this page and QUESTIONS.md.
2. Every `.json` file is I-JSON (no duplicate members, no lone surrogates, no noncharacters).
3. **Schemas.** The 21 schemas are valid JSON Schema 2020-12 with the right `$id`. The problem enum is §9.3.3's, and when the spec is on this machine (ATLAS_SPEC, else `~/Documents/ChatGPT/creation/atlas-spec`) it equals 09-3-documents.md's table. The resource envelope is in the Atlas Schema profile.
4. **Grammar.** grammar.abnf parses (by `atlasref/abnf.py`, an ABNF engine) and defines every rule. Every grammar vector holds both by the hand matchers and by the ABNF itself, wherever the ABNF is the reason.
5. **Fingerprints.** Every case's canonical form, SHA-256 and fingerprint, computed by tools/spec's RFC 8785 and by the rfc8785 package alike.
6. **Profiles.** Every profile vector is valid against profile.schema.json, each schema against atlas-schema.schema.json and the profile's rules in its role. Each passes the lint and the agent-readiness check, its fingerprint is right, and its examples are valid requests.
7. **Schema vectors.** Every validation result, by validator.py and, as a second evaluator, by jsonschema extended with the profile's I-Regexp, formats, logical types and binary64 integers. Every profile verdict, by profilecheck.py and the meta-schema.
8. **Headers and Structured Fields.** Every header case by headers.py. Every Structured Field case by sfv.py and, case by case, by http_sf; every disagreement is marked Q-SF-1.
9. **Arrow.** Every derived schema, round-tripped through pyarrow.
10. **Resolution.** Every case, row by row and column by column.
11. **Handoff.** Every chunk's SHA-256, every handoff document against handoff.schema.json, every decoding, and expected.arrows.
12. **Guard.** Every case, on sqlglot 30.27.0.
13. **Scenarios.** Exactly §9.8.4's 50 ids, each with its level, one expected result per command, known ops and assertions, and only exact-ids the fixtures serve. The fixtures' profiles have their exact-ids, and the bootstrap is valid.
