# Pass 1, criticised

This reviews pass 1 against the rewritten spec (`atlas-spec/`, 2026-10-31: 229 requirements) and the shared gateway patterns documented in the specification. Pass 1 proved the pattern in the hermetic setting: one front door, the Go client wrapped for Python, schema evolution behind a lock, streamed rows equal to handed-off rows. It is not the product.

Severity is what the defect would cost in production:

- **Blocker:** wrong on the wire, or unsafe.
- **Major:** missing robustness the spec requires.
- **Minor:** shape or naming.

## The protocol (`sdk/go/protocol`)

| # | Severity | Defect | Spec | Fix |
|---|---|---|---|---|
| 1 | Blocker | The profile is a flat struct with a custom `Table`/`Column` model. The spec's profile is nested and its schemas are JSON Schema 2020-12. The custom model cannot express `Money`, objects, arrays, `x-atlas-enum`, references, deprecations or closed request schemas. | §3.1.2, §9.2, §9.3 | One model: the nested profile, its schemas as JSON Schema in the Atlas Schema profile, the Arrow schema derived from it |
| 2 | Blocker | The fingerprint is SHA-256 of sorted-key JSON over an ad-hoc projection, not RFC 8785 over §9.4.1's projection. Another implementation would compute a different fingerprint for the same profile. | §9.4, PROFILE-002, AP-FP-001 | JCS (UTF-16 key order, ECMAScript numbers) and the exact StripSchema projection; fingerprint vectors |
| 3 | Blocker | No I-JSON. `encoding/json` accepts duplicate members, and integers beyond 2^53 lose precision. | §9.1.2, AP-CONV-001 | A decoder that rejects duplicates and out-of-range integers, used for every protocol document |
| 4 | Blocker | Headers are parsed by string splitting, not RFC 9651. A valid Structured Field, such as one with parameters or a quoted string, would be misread. | §9.6.3, D23, AP-HTTP-001 | A full Structured Fields parser and serialiser, fuzzed, checked against header vectors |
| 5 | Major | The validator covers a custom subset of JSON Schema. It doesn't enforce the profile's keyword list, format assertions, I-Regexp, `oneOf` with const discriminators, `x-atlas-logical` constraints, or JSON Pointer failure reports. | §9.2.1–9.2.5, AP-SCHEMA-001 | The Atlas Schema validator, plus the meta-schema check the gate runs |
| 6 | Major | Resolution follows the outline of §9.5 but misses several steps: projection to the requested columns, the absent-versus-null rules, the full convert table (date → date-time, recursive objects, arrays, enum aliases), and the exact `promote` backward. | §9.5.2–9.5.5, AP-RESOLVE-001/002 | The exact algorithm, row and column forms, with resolution vectors |
| 7 | Major | Addresses must be pinned in pass 1, but the grammar also allows a bare `id:interaction`. | §9.1.3 | Grammar-exact parsers |
| 8 | Major | The lock format differs from the spec's (`"id" = { version, fingerprint }` and `dynamic = true`). | §3.3.10, §9.3 | `lock.schema.json`; TOML parsed, then validated |
| 9 | Major | The NDJSON end line has no `usage`. The problem document lacks `instance`, the `type` URL, `estimate` and per-hop `details`. | §4.1, §9.3.3, §9.6.5 | Generated document types, checked against schemas |
| 10 | Major | Document types are written by hand. | §6.5.6 | Types generated from `references/schemas/` |
| 11 | Major | There is no change classifier, lint or agent-readiness check. The gate and registration have nothing to run. | §3.1.4a, §3.3.12, VERSION-001/007/009 | `protocol/classify`, `protocol/lint` and `protocol/readiness`, run by the gate and by admission |

## The gateway

| # | Severity | Defect | Spec | Fix |
|---|---|---|---|---|
| 12 | Blocker | The edge parses the capability name out of the URL path (`/v1/data/{address}`). The spec forbids routing by parsing a name. | §2.1 rule 1, §4.2 rule 2 | A binding router: the request path is looked up in the snapshot's binding table to find the address |
| 13 | Blocker | `QUERY_TAG` is rendered into the SQL text. `explain` then can't equal `fetch` byte for byte, because the tag changes per call. | DATA-004, §2.2.1 | The tag passed out of band, as session or statement metadata. The statement depends only on the request |
| 14 | Blocker | SQL is built by string concatenation with home-made quoting, and there is no statement guard. | §6.4.3, D28 | sqlglot typed trees rendered per dialect. The read-only guard parses with the Snowflake parser. Guard vectors |
| 15 | Blocker | Limits are per-replica token buckets. They aren't reserved before the call and settled after, so they wouldn't hold across copies, and there are no budgets, output caps, concurrency release or confirmation threshold. | §6.2.6, §6.7.13, ACCESS-003 | Reserve every window in one conditional batch, then settle. Allowances, pools and ceilings, inherited |
| 16 | Blocker | There is no idempotency anywhere. A retried write would act twice. | §6.7.8, MECH-008, GATEWAY-013 | Pipeline step 4: the JCS fingerprint, scoped to principal and address, kept 24 h. The key passed downstream |
| 17 | Blocker | The audit is an in-memory list. Nothing is durable before the answer ends, and there's no WORM container or group commit. | ACCESS-004, §6.2.8 | An `AuditSink` doing group commit to an append blob (Azurite locally), shipped to Log Analytics by `AuditReader` |
| 18 | Major | The catalogue is built once at start. There's no controller, change feed, checked snapshot, last-known-good, generations, switches, house rules or degraded mode. | §6.1.13–6.1.15, §6.7.10, GATEWAY-008–011 | The catalogue controller, `kit/hot`, LKG in Blob, switches, and degraded mode for at most 60 minutes |
| 19 | Major | Only the data surface exists, and only fetch and explain. There are no resources, actions, events, operations, exports or transfers. | §2.3–2.5, §4.1 | Every family, each with an exemplar capability and a battery |
| 20 | Major | Admission is a domain check. It has no lint, no compatibility check against what's served, no conformance batteries, and isn't atomic per service. | §6.1.6, GATEWAY-007/014 | Admission conditions `accepted`, `compatible` and `conformant`, as a generation |
| 21 | Major | Services are only called in process. There's no service protocol and no loopback battery, so "movable" is unproven. | §6.1.6–6.1.7, GATEWAY-003 | A service protocol over HTTP/2 (describe, health, invoke, start/get/cancel, stream, plan, settle), with batteries run both in process and over loopback |
| 22 | Major | There are no controllers or leases, so operations don't survive their replica. | §6.1.13, §6.7.6–6.7.7, OPERATION-001–003, RECONCILE-001–006 | `kit/reconcile` with a leader lease, and `kit/ops` with operation leases, checkpoints and takeover |
| 23 | Major | Resilience is missing: no breakers, bulkheads, retry budgets, shedding, in-flight caps, pre-auth limit or drain behaviour. | §6.1.8, §6.7.2, §6.7.5 | `kit/retry` and per-upstream bulkheads. The edge's middleware exactly as §6.7.2 lists it |
| 24 | Major | Built-ins: 4 of 20 exist. | §2.6, §9.3.2, AP-DOC-002 | All 20, with the exact request and result schemas |
| 25 | Major | The fake warehouse evaluates filters with hand-written Go, so the SQL the adapter plans is never run. | §6.4.3 | DuckDB runs the adapter's own statement, transpiled from Snowflake to DuckDB by sqlglot |
| 26 | Major | Telemetry is slog only: no OpenTelemetry, no spans per step, no redaction in one place, no bounded labels. | §6.1.12, §6.7.16, MECH-003/014/015 | The OTel API and slog, with redaction at one point. A test plants secrets and searches every sink for them |
| 27 | Major | The layer check misses the spec's rule that services never import `core`. There are no vet checks for registration in `init`, bare `go` statements or `os.Getenv` outside configuration. | §6.5 rule 1, PACKAGES-008, MECH-012 | Extra layer rules, plus analyzers in `tools/vet` |
| 28 | Minor | Serving values differ from the spec's (`:8000`, header bytes 64 KiB, shutdown sequence 5 s then 30 s). | §6.7.15 | The spec's values |

## The clients

| # | Severity | Defect | Spec | Fix |
|---|---|---|---|---|
| 29 | Blocker | The runner protocol's shape differs from §9.8.3: `result`, not `value`; no `warnings` or `calls`; options outside `args`. | §9.8.3 | The exact shape. The engine is the reference runner |
| 30 | Major | The Go client is package `client`. The spec's interface is `atlas.Connect`, `.Call`, `.Fetch`, `.Operation`, `.Subscribe` and the rest, with nothing capability-specific outside `call`. | §9.7.1, §5.4 | The `atlas` package at the root of `sdk/go`, implementing §9.7 |
| 31 | Major | Missing from the client: the environment order, `ATLAS_HOME`, the shared token store (0600/0700, atomic writes, refresh), sign-in, the profile disk cache, the confirmation flow, the deprecation hook, landing with a manifest, transfers, export, events and generated wrappers. | §9.7.2–9.7.13 | All of them, at the levels declared |
| 32 | Major | There is no CLI, conformance gateway, scenarios or vectors. | §5.3, §9.8 | `clients/cli`, `sdk/go/conformance`, and `references/` authored from the spec |
| 33 | Major | The Python face lacks `AtlasProblem` per code, refs, `read_as` for polars and DuckDB, async, operations, subscribe/publish, export/upload and the attribute form. There is no wheel, and no cross-compiled engine. | §5.1, PY-002/006, GO-002 | All of these, with per-platform wheels tested on a machine with no Go |

## Process

| # | Severity | Defect | Fix |
|---|---|---|---|
| 34 | Major | There are no load, chaos or fault-injection tests, and no evolution (N−1) test. | `tools/load`, a fault-injection row per dependency, `make evolution` |
| 35 | Major | There's no requirement traceability: no way to show that each of the 229 requirements is tested. | `make requirements`: every ATLAS- and AP- id mapped to a test, or marked as checked by review |
| 36 | Minor | There's no `AGENTS.md`, decision records, generator or review checklist answers. | The paved road: `AGENTS.md`, `cmd/atlas-new`, review records per service |

## What survives

These parts of pass 1 survive into pass 2:

- the `snowflake-arrow/1` codec and its assumption list
- the final-batch framing
- the handoff reader, with renewal and resume
- the engine's pipe design: Arrow on a second pipe, cancel, and dying with its parent
- the Python face's process rules: fork, Ctrl+C, exit
- the copied kit packages, as the start of `kit/serve`
- the layer check's shape
- the end-to-end test style: real binaries, with ready lines on stdout
