# Atlas peer review — 1 November 2026

Atlas has a useful foundation, but the shared framework is not ready to freeze for a company handoff. The current implementation still has correctness defects, and the supported assembly and authorization paths still encode the initial warehouse journey. The supported path for adding an ordinary API or a new company access dimension is incomplete.

The accompanying [core handoff plan](CORE-HANDOFF-PLAN.md) orders the work needed to change that. The target is that a company adds providers, adapters, capability definitions, configuration and deployment wiring, while the shared core, protocol and clients remain unchanged.

## Review basis

| Item | Reviewed state |
|---|---|
| Implementation | Repository-local gateway, clients, adapters and protocol packages |
| Historical scope | Findings describe the implementation observed on the review date; later closure is tracked in the [handoff progress](../../../HANDOFF-PROGRESS.md) |
| Review date | 2026-11-01; source observations collected through approximately 18:38 UTC |
| Method | Read-only inspection of implementation, regression-test source, pinned driver source, and the current hardening and plug-in documents |
| Independent execution | No tests, builds, generators, or live service calls were run |
| Changes made by this review | This report, the plan and their index in `atlas-spec/review/`; no implementation changes |

This is a targeted peer review of the handoff boundaries and previously reported defects, not a certification of every file. Source locations are historical evidence and can move as the implementation changes. Finding IDs `R3-01` through `R3-16` belong to this review, not the implementation's existing `R001` or `PR01` ledgers.

The builder's [HARDENING.md](../../../../docs/pass-2/HARDENING.md) records green `make check` and `make e2e` runs and 126 fixed findings out of 150. Those are reported results, not independently reproduced results here. The source findings below identify cases those results do not establish.

## What improved

The previous audit writer-owner deadline defect is addressed: `Record` now commits asynchronously and can return when the caller's context ends. Pipe/socket writes have a bounded path. I am closing that specific previous finding on source inspection. Regular-file sync/shutdown bounds and the fallback when pollable setup fails still need failure testing before a production claim.

The plug-in guide now correctly documents `DataSource`, the separate production `gosf` module, historical profiles and pure-Go release builds. Its obsolete driver instructions are fixed. The integration also adds explicit regression coverage for projected scope columns and stalled policy reads. The projection repair is useful, but incomplete at the provider boundary, as R3-01 explains.

Of the previous 13 findings, **12 remain open and one is addressed in source**. This pass adds **four findings**, producing 16 actionable findings below. This does not mean every test failed; no test execution was performed in this review.

## Findings, ordered by impact

### R3-01 — P1 — Missing scope evidence passes the projected-result guard

**New. Owner: shared pipeline/access.**

The pipeline asks the provider for hidden scoped columns, but builds its validation schema from the caller's original projection at [pipeline.go:463](../../../../gateway/core/pipeline/pipeline.go#L463). A provider that accidentally omits required `portfolio` can return only `security_id` and `as_of`; those values pass the projected schema. [access.go:302](../../../../gateway/core/access/access.go#L302) skips absent properties, so there is no portfolio value to reject. Non-string hidden scope values are also skipped. The [batch check](../../../../gateway/core/pipeline/rows.go#L386) only examines columns actually supplied.

This requires a faulty provider response; it is not a claim that a caller can choose arbitrary source rows directly. It matters because the core promises to enforce scope independently of provider mistakes. The new [regression fixture](../../../../gateway/core/pipeline/scope_test.go#L24) always honors the augmented column list and misses this case.

**Closure:** validate the provider response against the augmented schema, including required scope evidence, then check scope, then project. Exercise omitted scope in JSON and Arrow, malformed JSON scope values, and nested scope. An offending row or batch must yield no data to the caller.

### R3-02 — P1 — Remote-only Snowflake batches can panic

**Previously reported; open. Owner: Snowflake adapter.**

[gosf.go:223](../../../../gateway/adapter/snowflake/gosf/gosf.go#L223) retains the driver's batch slice directly. [gosf.go:344](../../../../gateway/adapter/snowflake/gosf/gosf.go#L344) clears an entry before calling `Fetch`. In the pinned gosnowflake 1.19.1 driver, a response without an inline first batch returns its internal slice; the downloader later assigns through that same entry. Clearing it creates a nil dereference. The fixture with inline data takes a different ownership path.

**Closure:** establish independent slice ownership without breaking Arrow record lifetimes. Exercise remote-only, inline-plus-remote, multiple-record and cancelled-download responses through the actual driver with a hermetic HTTP fixture.

### R3-03 — P1 — Entra refresh requests omit the application ID

**Previously reported; open. Owner: shared Go and Node credentials.**

The forms in [Go client.go:724](../../../../sdk/go/client.go#L724) and [Node credentials.ts:328](../../../../clients/node/src/credentials.ts#L328) contain only `grant_type` and `refresh_token`. The saved binding already carries `client_id`, but it is not sent. Microsoft lists it as required for refresh requests. A valid initial token can therefore work until renewal fails. [Microsoft refresh-token contract](https://learn.microsoft.com/en-us/entra/identity-platform/v2-oauth2-auth-code-flow#refresh-the-access-token).

**Closure:** use the trusted binding's application identity and test against a token fixture that rejects missing or incorrect parameters. Keep the existing origin and endpoint-binding protections.

### R3-04 — P1 — Persisted audit records discard required context

**Previously reported; open. Owner: audit adapter and its port suite.**

The persisted [line structure and encoder](../../../../gateway/adapter/auditlog/auditlog.go#L169) omit `Claimed`, `Client`, `Expected`, `Step` and `Source` from `ports.Fact`. The pipeline can populate those fields and the fake can retain them, while the production JSONL record silently loses them. This leaves claimed delegation, client attribution, expected contract, failure step and source settlement details unavailable for investigation.

**Closure:** round-trip every required fact field through the actual sink and reader. Assert on stored bytes and decoded records, not only facts captured by the fake. The hardening ledger's pipeline-level audit coverage is insufficient evidence of persistence.

### R3-05 — P1 — A cancelled Node caller can leave a rejected refresh unhandled

**New. Owner: Node credentials.**

[credentials.ts:279](../../../../clients/node/src/credentials.ts#L279) starts an independent refresh before [raceSignal:384](../../../../clients/node/src/credentials.ts#L384) checks whether the caller's signal is already aborted. That early return attaches no rejection handler to the refresh promise. If there is no other waiter and refresh later fails, catching the call's cancellation does not handle that rejection. Under Node's default rejection handling this can terminate the process. [Node process documentation](https://nodejs.org/api/process.html#event-uncaughtexception).

**Closure:** honor cancellation before starting unnecessary work and observe every independent refresh's rejection. Test a pre-aborted caller, cancellation during credential-file reading, refresh failure with no remaining waiters, and another caller that must continue successfully.

### R3-06 — P2 — A call's deadline does not cover its whole lifetime

**Previously reported; open, with an additional Go contention case. Owner: all client faces.**

Node performs [discovery](../../../../clients/node/src/index.ts#L405) before starting a fresh full [execution budget](../../../../clients/node/src/index.ts#L430). Two 900 ms phases can therefore exceed a requested 1,000 ms deadline. Python performs [connect without the call timeout](../../../../clients/python/fiatlas/client.py#L204), and acquires the [engine lock before starting its timer](../../../../clients/python/fiatlas/_engine.py#L170). Go holds a [plain mutex throughout refresh](../../../../sdk/go/client.go#L563), so a short-deadline caller can wait behind another caller's much longer refresh.

**Closure:** establish one deadline at public API entry and carry the remaining budget through connection, discovery, queueing, refresh, retries and response consumption. Waiting on shared work must be cancellable independently. Define any cleanup grace separately from the promised call deadline.

### R3-07 — P2 — Record-response failures are audited as success

**Previously reported; open. Owner: shared edge/pipeline lifecycle.**

[call.go:206](../../../../gateway/edge/call.go#L206) forwards an edge failure only to `res.Rows`. Record results then close normally. For example, an authorized `atlas.whoami` call with an unsupported `Accept` can execute, receive a refusal from the edge, and still record a successful outcome. Record serialization or delivery failures have the same reporting gap.

The edge also declares `RunNegotiated`, but the pipeline does not implement it. Negotiation falls back to after provider execution. This must be connected before changing or metered work is enabled.

**Closure:** use a common result-level failure/outcome mechanism for records and tables. Negotiate after authorization but before admission and provider dispatch; retain non-disclosure for unauthorized callers. Verify actual persisted outcomes for refusal, serialization failure, disconnection and timeout.

### R3-08 — P2 — Retained historical URLs fail negotiation

**Previously reported; open. Owner: edge/registry integration.**

The registry retains the binding that matched an old URL, but [call.go:179](../../../../gateway/edge/call.go#L179) negotiates using only the current interaction's bindings. When a compatible profile moves an address, an older client's URL can resolve in the registry and then fail internally at the edge.

**Closure:** negotiate against the matched historical binding and carry it through the call consistently. Prove the old URL over HTTP, through response decoding and audit, after two compatible updates.

### R3-09 — P2 — Boolean array-item changes bypass compatibility checks

**Previously reported; open. Owner: protocol gate.**

[classify.go:179](../../../../sdk/go/protocol/gate/classify.go#L179) compares items only when both `Items()` results are non-nil. [schema/access.go:77](../../../../sdk/go/protocol/schema/access.go#L77) hides boolean item schemas, while `items` is excluded from the generic keyword comparison. Narrowing a request from `items: true` to `items: false` or a restrictive object schema can therefore evade the breaking-change check.

**Closure:** compare absent, boolean and object item schemas explicitly. Prove both directions with requests that were valid under the previous profile.

### R3-10 — P2 — Profile-wide lenses alter unrelated interactions

**Previously reported; open. Owner: protocol gate/resolution and Go client.**

[discover.go:363](../../../../sdk/go/discover.go#L363) gives every interaction the entire profile lens. [classify.go:573](../../../../sdk/go/protocol/gate/classify.go#L573) checks field existence across any schema. A rename of `mv` in one result can consequently rename `mv` in another interaction that still requires that name. In particular, a historical writer resolved forward to a current reader can lose a required field in the unrelated interaction.

**Closure:** scope transformations to their precise schema/interaction, or refuse ambiguous profiles until that is supported. Test two interactions sharing a property name but evolving independently.

### R3-11 — P2 — Nested result renames are admitted without a working reverse transform

**Previously reported; open. Owner: protocol gate/resolution.**

[classify.go:282](../../../../sdk/go/protocol/gate/classify.go#L282) requires a reverse-capable rename lens only at the top level. A nested required `details.code` renamed to `details.id` with an alias can be admitted, while an old reader still cannot recover `code` from the new writer's result. The current transform operates on the top-level record.

**Closure:** classify unsupported nested result renames as breaking. Only admit them when both reader/writer directions have executable proof. A conservative refusal is sufficient; a general transformation language is not required for handoff.

### R3-12 — P2 — Discovery exposes unready capabilities as stable

**Previously reported; open. Owner: built-ins.**

[Capability.Lifecycle()](../../../../gateway/core/registry/registry.go#L118) downgrades an unready profile to preview, but [discovery filtering](../../../../gateway/core/builtins/view.go#L50) and [catalogue serialization](../../../../gateway/core/builtins/catalog.go#L94) use the raw profile lifecycle. Agents can therefore discover an incomplete capability as stable despite the readiness gate.

**Closure:** consistently use the effective lifecycle in filtering and output. Exercise the public catalogue and description with an incomplete stable-declared profile, with and without previews requested.

### R3-13 — P2 — Python's Arrow conversion loses later optional columns

**Previously reported; open. Owner: Python result conversion.**

[client.py:115](../../../../clients/python/fiatlas/client.py#L115) calls `Table.from_pylist(rows)` without a schema. Arrow infers from the first row: an optional field present only in a later row is omitted from the table. Empty results also lose their contract shape. [Apache Arrow API contract](https://arrow.apache.org/docs/python/generated/pyarrow.Table.html#pyarrow.Table.from_pylist).

**Closure:** construct the table from the resolved, projected reader schema. Test a late optional field, an empty result, decimals, nullability and full-range int64 values.

### R3-14 — P2 — Arrow physical types can silently change decoded values

**New. Owner: Go result decoder/shared Arrow protocol.**

[result.go:249](../../../../sdk/go/result.go#L249) does not verify the received physical Arrow schema against the profile before conversion. [arrowschema/rows.go:207](../../../../sdk/go/protocol/arrowschema/rows.go#L207) interprets every timestamp as microseconds. A received `timestamp[ms]` value of `1000`, representing one second after the epoch, becomes one millisecond after the epoch. The resulting date-time remains valid JSON and passes ordinary schema validation. This requires a non-conforming gateway/stream; the client should reject it instead of silently changing its meaning.

**Closure:** check the physical schema, nested types and required profile/protocol metadata before decoding data. Include timestamp and duration unit mismatches, decimal parameters and nullability in negative conformance cases.

### R3-15 — P2 — SQL casts cannot represent supported int64 extrema

**Previously reported; open. Owner: shared Snowflake/DuckDB planning adapter.**

[batches.go:96](../../../../gateway/adapter/snowflake/batches.go#L96) casts every Arrow Int64 to `NUMBER(18,0)`. An Atlas string with `format: int64`, such as `"9223372036854775807"`, is valid but needs 19 digits. Selecting it through this cast overflows. This concerns full-range int64 strings, not ordinary JSON integers restricted to safe numeric precision.

**Closure:** preserve the full declared int64 range in generated SQL. Exercise both signed extrema through the shared DataSource suite and the production conversion path.

### R3-16 — P2 — Snowflake Ping does not probe an existing connection

**New. Owner: Snowflake driver wrapper.**

[Engine.Ping](../../../../gateway/adapter/snowflake/gosf/gosf.go#L132) delegates to `database/sql.PingContext`, but the [session wrapper](../../../../gateway/adapter/snowflake/gosf/gosf.go#L430) omits `driver.Pinger`. The underlying Snowflake driver's probe is hidden. With an existing pooled connection, `database/sql` can return success without sending a statement, including after the server becomes unreachable. The method's documented reachability guarantee is therefore not met.

**Closure:** forward the probe with context, error classification and dead-session handling. A fixture must establish a working connection, make the underlying probe fail, and assert that `Engine.Ping` reports that failure.

## Architecture gaps against the requested handoff

These are not newly introduced regressions. They are deliberate limitations of the initial journey that must change for the user's broader goal.

| Gap | Evidence | Consequence |
|---|---|---|
| Standard assembly accepts only warehouse view definitions | [assemble.go:27](../../../../gateway/assemble/assemble.go#L27): `Domain` returns `[]views.Files`; registration and startup policy validation follow that path | A normal HTTP/API provider has no equivalent supported registration input before policy validation. The generic Provider port and public registry exist, so custom composition is possible, but the normal handoff path is incomplete. |
| Access dimensions are protocol/domain-specific | [provider.go:55](../../../../gateway/ports/provider.go#L55), [policy.go:209](../../../../gateway/ports/policy.go#L209), and built-in scope enumeration | `portfolio` and `index` work; `workspace`, `project` or another company dimension requires framework changes. Merely allowing arbitrary policy keys would be unsafe because enforcement would not automatically follow. |
| Shared mechanisms for changes, metering and durable work remain planned | [HARDENING.md:28](../../../../docs/pass-2/HARDENING.md#L28) and [PLUGGING-IN.md:164](../../../../docs/pass-2/PLUGGING-IN.md#L164) explicitly list them | A company triggering jobs cannot be expected to implement idempotency, approvals, budget authority or operation lifecycle separately in each connector. These must be shared before the broad handoff. |
| Proof is narrower than the intended platform claim | The same hardening document leaves protocol conformance, signed local identity, load/chaos and live services open | Green local checks establish useful progress, but do not establish independent extension, cross-client parity or enterprise-service operation. |

SQLGlot belongs in the repository as a shared foundational library, as requested. Its availability to modules is not a layering defect. Preserve that integration, the shared gateway mechanisms and their required license notices. Backtesting research remains outside this review and plan.

## Handoff recommendation

Continue development against the [ordered plan](CORE-HANDOFF-PLAN.md). Close the high-impact defects immediately, complete generic provider and scope extension, and then prove the boundary with a separately authored warehouse capability and HTTP capability using unchanged clients. That is the first useful handoff gate.

The full requested destination also requires a changing, long-running capability through shared durable mechanisms. An Astronomer-style job trigger is a suitable proof case; a specific vendor deployment is not necessary to prove the core contract. Keep live adapter validation and core readiness as separate evidence, and do not call either complete on the strength of the other.
