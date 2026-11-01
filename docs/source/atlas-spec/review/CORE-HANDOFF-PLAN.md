# Atlas plan for a stable, extensible core

**Proposed 2026-11-01, following the [fresh peer review](PEER-REVIEW-2026-11-01.md).** This plan is for the builder. It changes no implementation by itself and makes no claim that its acceptance checks have already passed.

The destination is a company-owned Atlas deployment where an engineer adds a service integration without changing the shared framework. Agents discover authorized capabilities, understand their behavior, try them through the CLI, and use the same contracts from Python or Node.

The company will still supply service credentials, business semantics, source mappings, grants and deployment configuration. Atlas must supply the common identity, access enforcement, execution, contract, audit and client behavior. A new connector must not become another implementation of those mechanisms.

## 1. Define the boundary before freezing it

| A company may add or change | Shared modules that must remain unchanged during the inheritance exercise |
|---|---|
| Domain provider implementations and their business logic | Registry, access engine, execution pipeline and built-ins |
| Capability profiles, history, examples and human guidance | Protocol schemas, validation, compatibility gate and resolution |
| View definitions, source mappings and service adapters | Shared view engine and SQL planning/guard foundations |
| Registered access-dimension definitions and policy configuration | Generic scope enforcement and policy evaluation |
| Identity, storage, audit and transport adapters behind supported ports | Shared authentication/credential lifecycle and client semantics |
| Composition-root imports, registration calls and dependency manifests | HTTP edge, Go engine, CLI, Python and Node implementations |
| Secrets references, infrastructure and deployment settings | SQLGlot standard library and shared kit |

Explicit compiled registration is sufficient. A company composition root may import modules and wire providers. No runtime plug-in loader is required. The test is whether it can do this through a supported public interface, without copying the framework's assembly logic or patching shared code.

Keep SQLGlot in-repo as the common SQL library. Keep the shared gateway kit and its required license notices. Backtesting research remains outside this work.

There are two release gates:

- **Gate A: read-only company handoff.** Warehouse and HTTP capabilities, including a new company access dimension, work through all three public interfaces without framework changes.
- **Gate B: the requested broader core handoff.** A changing, long-running capability also works through shared approval, idempotency, durable operation and audit mechanisms; metered capabilities use a supported budget authority. Gate A is an interim milestone, not completion of this broader goal.

Freeze the framework and client builds separately for each gate, after its shared functionality is complete. Framework development may continue between gates; the inheriting company must not need framework or client changes during either integration exercise.

## 2. Ordered work packages

The order below reflects the inheritance requirement. Correctness fixes can proceed in parallel with extension design. Do not freeze the public extension interfaces until their independent integration exercises pass.

| Order | Package | Owner | Depends on | Completion evidence |
|---|---|---|---|---|
| 1 | C0 — Close immediate correctness defects | Core, client and adapter maintainers | Current source | Targeted regressions fail before each fix and pass after it |
| 2 | C1 — Make providers and access dimensions genuinely extensible | Core/protocol maintainers | Boundary decisions | Independent HTTP and warehouse registrations; new dimension enforced centrally |
| 3 | C2 — Finish the common call lifecycle | Pipeline/edge/audit maintainers | C0; C1 for extension coverage | Every supported call has consistent admission, cancellation and persisted outcome |
| 4 | C3 — Make contracts and clients dependable | Protocol/SDK maintainers | C0 | Compatibility and representation parity against independent fixtures |
| 5 | C4 — Prove the read-only handoff | Integration/test maintainers and an independent adopter | C1–C3; adapter repairs | Gate A, signed identity fixtures, conformance and bounded-failure evidence |
| 6 | C5 — Supply shared mechanisms for changing and durable work | Core/ports/storage/SDK maintainers | C1–C3; can begin before C4 finishes | Restart, retry, concurrency and uncertain-outcome tests; Gate B exercise |
| 7 | C6 — Package the inheritance contract | Documentation/release maintainers | C4 and C5 for the full release | Reproducible release, usable handbook and independently repeated handoff |

### C0 — Fix the issues that invalidate ordinary use

Close these first:

| Review finding | Required repair | Regression that proves it |
|---|---|---|
| R3-01: missing scope evidence | Validate augmented provider results before scope checks and caller projection | Omitted and malformed required scope in JSON and Arrow is refused before the offending data is yielded |
| R3-02: Snowflake batch panic | Correct ownership of the driver's batch slice and records | Remote-only chunks through the real driver fixture, plus mixed and interrupted responses |
| R3-03: Entra refresh parameters | Build refresh requests from the trusted credential binding | A realistic endpoint rejects missing/wrong application identity; renewal succeeds with the proper binding |
| R3-04: lost audit fields | Persist and read the complete required fact | Actual JSONL round-trip retains claimed identity, client, expected profile, step and source settlement |
| R3-05: unhandled Node refresh rejection | Observe background refresh failures and respect independently cancelled waiters | Pre-aborted sole caller plus later refresh failure does not produce an unhandled rejection |

These are shared implementation repairs, not company configuration tasks. Do not push compensating work into individual providers or tell consumers to avoid affected standard behavior.

### C1 — Complete the supported extension path

**Provider registration.** Generalize the standard assembler to accept normal provider entries with their profiles and history. Keep warehouse views as one supported provider construction path. Register all entries before validating policy references or publishing the serving snapshot. Expose explicit upstream limits, lifecycle/cleanup and health wiring without requiring a domain switch in the pipeline.

The minimal contract should cover a provider, its capability definitions, its upstream dependencies and the resources the composition root owns. Preserve atomic registration, duplicate detection, profile-history validation and startup failure on invalid configuration. Adding an HTTP provider must not require pretending it is a warehouse view.

**Access dimensions.** Make one explicit protocol/core amendment now. Recommend a stable scope annotation or descriptor over existing schema types, backed by an immutable deployment registry of named dimensions. This is a design change from the current fixed `PortfolioCode`/`IndexCode` scheme, and must be recorded in the spec and assumptions rather than introduced implicitly.

After that amendment, a company must be able to register `workspace` or `project` without adding another built-in logical type, editing a switch, or releasing new clients. The registration must define how scope is represented and found; it must not grant anyone access by itself. Grants continue to come from trusted policy.

Required behavior:

- Reject unknown dimensions, unresolved schema references and inconsistent bindings at startup.
- Treat absent grants as no access; define absent/null scope evidence explicitly and refuse it where required.
- Use the same registered dimensions for request validation, provider invocation, result checks, discovery and `whoami`.
- Enforce nested and projected scope values and revocation on active streams.
- Let providers push the supplied scope into their source queries while retaining the core's independent result checks.

Keep this a small typed extension mechanism. A general policy language or arbitrary plug-in authorization code is unnecessary.

**Exit test:** from a separate company module, register `support.tickets` backed by a fake HTTP service and scoped to a new `workspace` dimension. Two callers granted the capability can both discover it, while each receives only rows from their permitted workspaces. The standard assembler starts with the initial policy already referring to this capability. No shared source file changes.

### C2 — Use one complete execution and outcome path

Resolve R3-06, R3-07 and R3-08. The lifecycle should establish identity and current access, resolve the matched binding, negotiate a supported representation, validate and normalize the request, admit work, dispatch, enforce returned data, and record the actual outcome. Admission must follow every refusal that can be decided without provider work.

Keep negotiation after authorization so confidential capability bindings remain undisclosed. An unsupported `Accept` must produce no provider invocation or charge. Historical URLs must carry the binding that matched, rather than being renegotiated solely against current addresses.

Make failure reporting common to records and streams. Cancellation, a failed write, an invalid result, a refused representation and truncation must be visible consistently to the caller and in the persisted audit record. Separate source settlement from the delivery outcome; delivery failure does not prove that no source work occurred.

Clients need one absolute deadline per public operation. The remaining budget must cover cold bootstrap, discovery, credential locks/refresh, engine queueing, retries and consumption. Shared refresh may have its own bounded lifetime, but a waiting caller must leave at its own deadline without failing other callers. Document any bounded cleanup grace separately.

**Exit tests:** record and table variants of successful delivery, pre-dispatch refusal, failed serialization, disconnected client, stalled upstream, expired queued call and cancelled refresh waiter. Assert provider invocation counts, released resources, user-visible problems and persisted admission/outcome/settlement facts. Exercise audit stalls and shutdown, including pollable-setup failure and the regular-file path; do not reopen the already-fixed request-owner hang under a new label.

### C3 — Make compatibility and cross-language meaning reliable

Resolve R3-09 through R3-14.

Keep compatibility deliberately conservative. Explicitly compare boolean item schemas. Refuse nested or ambiguous cross-interaction transformations until the resolver can prove them in both directions. Historical requests, profiles and bindings must stay usable through the HTTP edge and released clients, not merely remain in a registry map.

Use effective lifecycle everywhere agents discover capabilities. An incomplete stable-declared profile must be presented as preview and excluded when previews are not requested.

Build Python Arrow results from the resolved, projected reader schema. Validate incoming Arrow physical schemas and protocol metadata before interpreting values. JSON, NDJSON and Arrow must preserve the same logical rows, including optional fields, empty results, exact decimals, int64 extrema, timestamp units and nullability.

**Exit matrix:** CLI, Python and Node against a common fixture gateway with independently authored expected outcomes. Cover both directions of compatible updates, two updates in sequence, unrelated interactions sharing field names, nested rename refusal, boolean `items`, old URLs, projected results, empty tables, malformed streams and unknown critical traits. A face may explicitly decline an unsupported encoding; it may not change business meaning when converting a supported result.

### C4 — Prove that the read-only platform can be inherited

Repair the remaining reference-adapter defects: R3-15's int64 cast and R3-16's hidden Ping implementation. Keep these repairs inside adapters. Extend the shared DataSource suite where behavior is universal, and retain driver-specific ownership and pool tests in `gosf`.

Complete the acknowledged proof work: the fixture gateway and conformance runners, signed RS256 local identity using the real verifier, and a requirement-to-test matrix. Retain token-table fakes for focused tests where useful; they cannot substitute for the end-to-end identity path. Token refresh, key rotation, unknown keys and issuer failure need explicit expected behavior.

Define load/failure pass criteria before running them: deadline bounds, bounded queues and memory, admission under saturation, cancellation latency, audit availability behavior, policy staleness limits and isolation between unrelated upstreams. Run deterministic fault tests separately from load measurements. A Snowflake outage must not consume every slot needed by a healthy HTTP provider.

For each shipped port, publish an adapter authoring contract, fake and behavioral suite. Suites must assert required semantics, not just method shape or that a call returns. The production sink's persistence and the production driver's chunk handling require tests of those implementations.

**Gate A exercise:** freeze framework sources and build the CLI/Python/Node packages once. An engineer who did not implement the seams adds a warehouse capability and an HTTP capability in a separate company module. At least one uses a new scope dimension. They configure identity, policy, secrets and adapter bindings, then discover, describe and execute both with the already-built clients. Record all changed files and verify hashes of the protected framework modules are unchanged.

A failure of that exercise identifies unfinished framework work. It is not closed by copying assembly internals into the company module or granting an exception to the protected-file list.

### C5 — Finish shared support for changes, metering and durable operations

This package is necessary for the broader handoff. The existing build plan already acknowledges it; it is not a newly discovered regression. Do it before advertising job-triggering, changing or metered families as supported.

Use small conformance capabilities such as `conformance.change`, `.costly` and `.slow` to drive the shared mechanisms. An Astronomer-style job trigger is an appropriate integration exercise after those contracts work.

| Shared mechanism | Required contract and failure evidence |
|---|---|
| Idempotency | Scope keys to the correct principal/address/request; normalize before digesting; enforce retention and fencing; concurrent duplicate requests produce one source effect; changed requests under the same key are refused |
| Uncertain outcomes | A timeout after dispatch must not invite an unsafe automatic replay; record uncertainty and reconcile with the source's operation identifier or supported idempotency mechanism |
| Confirmation and approval | Server-verifiable evidence bound to principal, capability, normalized request, plan, scope, limits and expiry; a client header alone is insufficient; recheck before execution |
| Metering | Declare one budget authority for each supported reservation set; atomically reserve, settle and release under its contract; refuse unsupported combinations of authorities |
| Durable operations | Persist identity, request, state and source reference; separate start and execution deadlines; recover after restart; fence concurrent workers; authorize polling/cancellation and apply current policy at the defined lifecycle points |
| Audit and settlement | Preserve admission, execution outcome and source settlement across retries and restart; make delivery failure and uncertain source outcome distinguishable |

Specify these as shared ports with fakes and suites, and supply at least one durable adapter suitable for the intended deployment. Process memory alone cannot demonstrate restart or multiple-replica guarantees. Azure-specific storage or messaging belongs in adapters; its presence must not alter common operation semantics.

Complete the corresponding generic CLI, Python and Node support for confirmation/approval, operation handles, polling, cancellation and recovery. Prove parity before freezing the Gate B client builds. None of these flows should require capability-specific client code.

Avoid claims of universal exactly-once execution across arbitrary external APIs. Where a source cannot deduplicate or reconcile, expose a documented uncertain state and a controlled recovery path. Connector authors implement the source-specific action and reconciliation, while the framework owns the common lifecycle.

**Gate B exercise:** freeze the framework and clients after C5 is complete, then add a company job capability through the public extension interface. Trigger it twice with the same key, interrupt the gateway after source dispatch, restart, poll and reconcile its status, attempt unauthorized polling/cancellation, and complete settlement. Add a metered variant with a bounded reservation. Shared modules and client builds remain unchanged during the exercise. Unsupported combinations fail explicitly before source work.

### C6 — Deliver a usable contract to the inheriting company

Publish a tagged, reproducible release and an exact supported-feature manifest. Record which claims have source coverage, executed hermetic tests, executed integration tests and live-system evidence. Keep unknown or untested environments visible. Do not label a connector production-proven because its fake or compiled driver passed.

Generate or validate the human capability guide from the same authored definitions used by discovery. Each available capability must explain:

- What task it performs, when to use it and its effect.
- Who can use it and how data scope changes the answer.
- Required inputs, defaults and examples in CLI, Python and Node.
- Output shape, grain, keys, ordering, freshness and completeness/truncation behavior.
- Latency/deadline expectations, source limitations and metering where applicable.
- Retry, idempotency, cancellation and uncertain-outcome behavior where applicable.
- Errors, useful recovery steps, owner and contract/version policy.

Validate guide examples against a fixture with known outcomes. Keep contract documentation distinct from current operational health, and make both discoverable. A catalogue entry with a name and source table is not a complete capability guide.

Supply one reference warehouse extension, one HTTP extension and one durable job extension, each in the company-owned layout. Include adapter suite commands, deployment wiring, secrets references and troubleshooting that follows the supported public interfaces.

**Final exit:** a second engineer repeats the Gate A and Gate B exercises from the release and documentation. Their changes are confined to the allowed extension/deployment files. The resulting capabilities work through unchanged clients, and the recorded evidence is sufficient to reproduce both success and failure behavior.

## 3. Keep the plan focused

Do not build every catalogue capability, every enterprise connector or a general distributed plug-in runtime to finish the core. Do not expand schema transformation support merely to avoid conservative compatibility refusals. Do not shift shared identity, retry, authorization or operation-state logic into company providers.

The core is ready when representative new company capabilities exercise every supported common mechanism without shared-code edits. Real Snowflake, Entra and selected Azure/Astronomer integrations then establish deployment-specific confidence; they do not replace that inheritance proof.

## 4. Review-to-work mapping

| Work package | Review items closed |
|---|---|
| C0 | R3-01, R3-02, R3-03, R3-04, R3-05 |
| C1 | Generic-provider assembly and company scope-dimension gaps |
| C2 | R3-06, R3-07, R3-08; negotiation before dispatch and complete lifecycle evidence |
| C3 | R3-09, R3-10, R3-11, R3-12, R3-13, R3-14 |
| C4 | R3-15, R3-16; conformance, identity, load/failure and Gate A proof |
| C5 | Acknowledged changing/metered/durable mechanism gap and Gate B proof |
| C6 | Reproducible handoff, capability documentation and explicit evidence/feature status |
