# Atlas design review

**Review date: 2026-10-31.** Findings concern the supplied Markdown specification and the repository revisions in [Integrations](INTEGRATIONS.md). Recommendations are proposed changes, not silently amended source requirements. The clean result is [The proposed Atlas core](ATLAS-CORE.md).

## The assessment

The product idea is strong. Domain capabilities let the company explain its knowledge once, centrally, instead of making every agent rediscover tables, APIs and conventions. The CLI-to-SDK progression makes a successful experiment reusable. A common access, limit and audit path makes that interface viable across the company.

The spec becomes cumbersome where it tries to guarantee all of the following at once: arbitrary schema evolution, every client calling every interaction, direct data delivery, exact distributed budgets, immutable final audit records, immediate cancellation, rapid revocation, offline-compatible locks and independently movable services. Some pairs conflict; others depend on technology proofs that the text sometimes describes as established facts.

The recommended simplification is to keep the product promise and reduce the universal guarantees. Start with explicit capability contracts, a small shared client contract, a reliable invocation path and conservative evolution. Add each advanced mechanism when a real capability needs it and its acceptance test is clear.

## What is particularly good

| Existing design | Why it matters | Keep it by doing this |
|---|---|---|
| Domain names rather than physical tables | Agents ask business questions; infrastructure can move | Make subject capabilities the normal route; keep raw SQL explicitly privileged |
| One working view scoped to the caller | Agents spend less context and time exploring unusable operations | Return short capability cards with only visible, permitted interactions |
| Profiles include meaning and examples | Syntax alone cannot prevent a plausible but wrong analysis | Require grain, units, time semantics, exclusions and valid examples |
| Same request across CLI, Python and Node | Exploration transfers directly into maintainable programs | Test the same address and JSON request through all three |
| Central policy and generic clients | New capabilities need no SDK-specific implementation | Admit a new capability after client builds and exercise it unchanged |
| Writer schema enforced before delivery | A provider change need not surprise every consumer | Check planned types and actual returned batches |
| Explicit effects and operations | Agents can reason about changes and long-running work | Describe cost, approval, retry and uncertain outcome behavior together |
| Independent Go and TypeScript conformance | Shared bugs are less likely to define the protocol accidentally | Keep independent expected results and cross-client comparisons |

These strengths are already visible in [Vision](../00-vision.md), [Discovery](../02-6-discovery.md), [Contracts](../03-contracts.md) and [Clients](../05-clients.md). The proposed core brings them to the front.

## Decisions needed before implementation contracts are frozen

### R01 — A contract fingerprint cannot validate mutable documentation and live access

**Evidence.** [§9.4](../09-4-fingerprints.md#L18) excludes prose and usage from the fingerprint, while calling that fingerprint a strong ETag. [§9.7](../09-7-clients.md#L102) caches profiles forever. [§9.6](../09-6-http-binding.md#L95) permits immutable exact-profile describe responses even though describe includes current governance and availability. [§3.1](../03-contracts.md#L129) says availability is never cached.

**Consequence.** Corrected instructions can remain stale indefinitely; cached access and health can be mistaken for current truth. A strong HTTP validator must change with observable representation changes. [RFC 9110 §8.8.1](https://www.rfc-editor.org/rfc/rfc9110.html#section-8.8.1)

**Proposed decision.** Cache the immutable contract separately. Give the full description its own document revision/validator; keep current describe caller state private and uncached initially. Include prose revision in guide generation/cache identity. Reconcile the catalogue's one-hour client cache with the HTTP `no-store` rule.

**Acceptance.** Change guide wording, revoke a grant and suspend a source without changing the contract digest. The guide updates, invocation is refused appropriately, and no cached access response authorizes the call.

### R02 — The lock freezes routing that the spec calls safely movable

**Evidence.** [§9.7's call algorithm](../09-7-clients.md#L88) chooses bindings from the locked/cached profile. [§3.3](../03-contracts.md#L595) allows an old binding to disappear after a window because clients follow served bindings. An unchanged locked profile has no specified way to learn the new binding. Exact ids alone also fail the literal “no discovery at runtime” rule; the [existing proof](../../atlas-proof-plan.md) records this.

**Proposed decision.** Pin semantic contracts, not network addresses. Package reader contracts with the lock; refresh current routing independently. Define “no discovery at runtime” as no new business-route selection. A lock does not pin the source data or confer access.

**Acceptance.** Move a provider and update the guide. A locked program still calls the same business operation with the same request meaning, uses the new route and obeys current grants.

### R03 — Full transitive compatibility is not delivered by the stated lens rules

**Evidence.** [§3.3](../03-contracts.md#L538) promises old and new readers can read every result within a version. Its change table nevertheless allows field removal after sunset, nullable-to-non-null changes and numeric promotion. [§9.5](../09-5-resolution.md#L45) allows reverse conversion to fail or to substitute null/default for removed information.

**Counterexamples.** A required `market_value` disappears; no default can recover its actual old value. An integer becomes a floating-point field and the new producer sends `1.5`; an old integer reader cannot preserve that value. A newer non-null reader encounters historical nulls. These are contract issues, not implementation bugs solved by more lenses.

**Proposed decision.** Begin with backward-compatible requests, additive optional/defaulted results and stable meanings. Use a new major version for incompatible removals, unit changes or type changes. Keep aliases where their meaning is exact. Introduce more transforms only for demonstrated migrations with explicit information-loss rules.

**Acceptance.** Compatibility tests contain those counterexamples and reject the change or retain a real old-version implementation. The gate never classifies an unrepresentable old value as safe.

### R04 — The transformation language does not express its own examples

**Evidence.** [§9.5](../09-5-resolution.md#L17) has one lens list for a whole profile, with top-level field operations. [§3.3's rename example](../03-contracts.md#L646) requires translating values inside `columns: ["mv"]`, not renaming a top-level request member. Field masks, sort keys, filters, request schemas and result schemas need distinct interpretation.

**Proposed decision.** Under the smaller evolution model, keep explicit accepted aliases and their schema scope. If lenses remain, name their target schema/interaction and define projection/mask/filter translation. Validate the original request under its declared contract before transformation, and the transformed request afterward; do not silently discard unknown request fields.

**Acceptance.** A misspelled field is rejected, and two unrelated fields with the same spelling in request and result are never both transformed accidentally.

### R05 — Capability access is not sufficient data scope

**Evidence.** [§6.2.5](../06-2-access-limits-and-audit.md#L115) defers resource conditions, while [§6.3](../06-3-reaching-systems.md#L80) acknowledges sources reached as the gateway have only Atlas's grant as a limit.

**Consequence.** A caller allowed to invoke `portfolio.holdings` might supply a portfolio outside their entitlement if the source connection uses a broader service identity. This would violate the very thing the magic-wand interface is supposed to handle.

**Proposed decision.** Enforce resource and returned-data scope from the first shared-credential integration. Small, typed rules and curated queries are sufficient; a general policy language can wait. Do not fall back silently from failed user delegation to a service identity. Keep relevance/defaults distinct from permission.

**Acceptance.** Two users with access to the same capability but different portfolio scope receive only their permitted data, through every delivery path and any result cache.

### R06 — A person's token and a confirmation header do not prove human approval

**Evidence.** [§6.2](../06-2-access-limits-and-audit.md#L71) gives the agent its person's sign-in; the same section requires a person for Production actions and uses `Atlas-Confirm` for costly calls. A caller-controlled assertion cannot establish who approved an execution.

**Proposed decision.** Distinguish authenticated caller context from approval evidence. Bind an approval or confirmation receipt from a trusted interaction to the principal, action, canonical request/plan, scope, approved maximum and expiry. Define what policy tightening does to outstanding plans. Treat approval as live policy, resolving its duplicate placement in [contract meaning](../03-contracts.md#L699) and [governance](../03-contracts.md#L611).

**Acceptance.** An agent cannot authorize a new plan by changing a header, copying another user's receipt, increasing the amount or changing the request after approval.

### R07 — Exact shared budgets do not fit the proposed partition keys

**Evidence.** [§6.1.5](../06-1-front-door-and-services.md#L143) reserves all counters in one transaction and partitions by caller plus capability. [§6.2.6](../06-2-access-limits-and-audit.md#L129) adds shared and firm-wide pools. Cosmos batch atomicity is limited to one container and logical partition, so the described layout does not atomically cover those shared pools. [Microsoft's transactional batch documentation](https://learn.microsoft.com/en-us/azure/cosmos-db/transactional-batch)

**Proposed decision.** Separate protective rate limits from hard spending admission. Choose a transaction boundary per shared budget authority, co-locate its atomic reservation set or allocate bounded sub-budgets. Reject unsupported combinations initially. Prove any broader cross-budget scheme before promising exactness.

**Acceptance.** Different users on different replicas contend for one shared hard budget; combined admissions never exceed it. Abandoned reservations recover, and settlement is idempotent.

### R08 — A crash must not release permission to repeat an uncertain effect

**Evidence.** [§6.1 operation takeover](../06-1-front-door-and-services.md#L250) releases the idempotency key so a start can run again. [§6.7](../06-7-implementation-patterns.md#L202) promises the same key returns the same operation. The upstream could already have committed when the receipt was lost.

**Proposed decision.** Retain one logical operation per key throughout its retention, including uncertain outcomes. Use provider deduplication and reconciliation. Fence workers after lease expiry. Include metered reads when a retry can duplicate a charge. Do not promise exactly-once external effects solely from HTTP deduplication.

**Acceptance.** Kill the worker after upstream commit but before receipt persistence; retry the original key. It returns/reconciles the original operation and creates no second effect.

### R09 — One immutable final audit record cannot contain later settlement

**Evidence.** [§6.2.8](../06-2-access-limits-and-audit.md#L146) requires final usage and source history in one immutable record durable before the response ends. [§6.1](../06-1-front-door-and-services.md#L104) settles handoffs later; streaming completion likewise occurs after its headers and initial rows.

**Proposed decision.** One logical invocation history, represented by immutable admission, outcome and settlement facts. Present a joined summary when people want one row. Specify the acknowledgement point and recovery when an effect succeeds but recording completion fails.

**Acceptance.** Crash at each boundary. The history preserves what is known, correlates later settlement and exposes an unresolved outcome rather than inventing a completed one.

### R10 — Delivery credentials require technology-specific guarantees

**Evidence.** [§6.3](../06-3-reaching-systems.md#L57) says OBO tokens never leave the gateway, but its [ticket remediation](../06-3-reaching-systems.md#L96) sends the person's OBO token. A read-only role can still authorize more than the one approved statement and bypass Atlas's per-call limits.

The spec also promises that direct links are usable only by their caller. That is a property to prove for the chosen mechanism, not a consequence of calling a URL “bound.” Current Azure documentation provides a **user-bound user delegation SAS** mode using `sduoid` and an end-user bearer token, from service version `2025-07-05`; ordinary SAS and Snowflake chunk links must not be assumed to have that behavior. [Azure user-bound SAS documentation](https://learn.microsoft.com/en-us/rest/api/storageservices/create-user-delegation-sas#user-bound-user-delegation-sas)

**Proposed decision.** Keep client SQL tickets deferred. For each offered handover record its actual target, rights, identity binding, expiry, renewal and revocation limits. Prove the SDK/network path for any stronger guarantee.

**Acceptance.** Exercise intended use, copied credentials, changed targets, expired links, renewed links and revoked access for the actual Development system and each supported client.

### R11 — Client parity is about behavior, not a shared filesystem or universal Arrow

**Evidence.** [§9's Core level](../09-atlas-protocol.md#L45) is JSON with independent optional levels, while [§9.7](../09-7-clients.md#L198) requires every table to land as Arrow. The browser client cannot implement the mandatory local token file. Python's lazy-looking reference and Node's eager-looking fetch also leave the execution point unclear.

**Proposed decision.** Define one public lifecycle: a call executes once and yields a result/operation handle; reading a result does not silently execute again. Explain is a separate operation. Use runtime-appropriate credential providers and idiomatic values. Core can return JSON; Arrow and direct downloads are optimizations with identical logical meaning.

**Acceptance.** Compare identity, canonical requests, values, errors, deadlines, completeness and usage across CLI/Python/Node. Count source executions when reading a result twice. A browser client needs neither a token file nor local disk output.

### R12 — The protocol still needs implementable schemas and unambiguous encodings

**Evidence.** [§9.8](../09-8-conformance.md#L20) marks much of the vector corpus and all scenario files as to write. The specification text alone does not establish the completeness of the [protocol reference corpus](../../../../spec/protocol/references/) or its implementation. Coverage must be demonstrated by the conformance suite.

Other concrete encoding conflicts: JSON property order is immaterial but determines Arrow order; dynamic JSON tables are both arrays and objects with `columns`; optional-field absence becomes null in Arrow; timestamp precision varies; ignored Atlas annotations are also required validation assertions. See [§9.1](../09-1-conventions-and-grammar.md), [§9.2](../09-2-schemas.md), [§9.3](../09-3-documents.md) and [§9.6](../09-6-http-binding.md).

**Proposed decision.** Publish a small executable schema corpus with the first slice. Define explicit table column order, one timestamp precision, null/absence normalization and a table schema subset the encodings can actually carry. Define dynamic result metadata framing. Generate standard schema constraints or require an explicit Atlas validation layer.

**Acceptance.** Independently decode the same fixtures in Go and TypeScript, including large integers, exact decimals, null/absence, reordered JSON properties, dynamic tables and incomplete streams. A client built from the delivered artifacts can pass that initial scope.

## Further repairs that should stay visible

| ID | Finding and source | Recommended resolution |
|---|---|---|
| R13 | [§6.1 degraded mode](../06-1-front-door-and-services.md) may use policy for 60 minutes; [§6.7](../06-7-implementation-patterns.md) also has a 60-second grant target and a 10-minute group lookup cache | Separate policy, identity-group, active-connection and issued-link freshness bounds. A switch stored behind the same failed control channel cannot guarantee immediate emergency revocation. |
| R14 | [§9.3 resource ETags](../09-3-documents.md) do not change when the returned `status` changes | Use a full-representation ETag initially; keep desired-state generation separate. Do not lose status changes behind a 304 response. |
| R15 | [§6.1](../06-1-front-door-and-services.md) calls the change feed the outbox for domain events and invalidation | Write explicit immutable outbox facts where every event matters; coalesce only notifications that mean “re-read current state.” Latest-version change feed can omit intermediate versions and deletes. [Cosmos change feed modes](https://learn.microsoft.com/en-us/azure/cosmos-db/change-feed) |
| R16 | [§2.5](../02-5-events.md) treats polling history as a universal event fallback and relies on client validation for publishers | Require cursor-addressable retained history for equivalent polling. Define explicit acknowledgement after handling. A direct sender's authorization/validation must be enforced beyond an SDK convention. |
| R17 | [§4.1](../04-plugging-in.md) couples disconnect to cancellation, while operations survive their caller and cancellation is described as cooperative yet bounded | Separate start-request deadline, execution deadline and operation retention. Report requested versus confirmed cancellation; preserve uncertain committed effects. |
| R18 | [§5 result cache](../05-clients.md) keys only by address, request and fingerprint | Include authorization context, environment and data revision; reauthorize reuse. Initially avoid automatic persistent or cross-user reuse, especially licensed data. |
| R19 | [§2.2.1](../02-2-1-snowflake-data.md) uses EXPLAIN bytes to choose result transport | Label heuristics and measure output. Snowflake's `bytesAssigned` describes partitions that may be scanned, not output size or an exact bill. [Snowflake EXPLAIN](https://docs.snowflake.com/en/sql-reference/sql/explain) |
| R20 | [§6.1](../06-1-front-door-and-services.md) requires remote-loopback admission while its service transport remains open | Keep in-process provider conformance first; require remote transport/authentication conformance on first extraction. Preserve serializable boundaries now. |
| R21 | [§3 meaning](../03-contracts.md) makes any source change breaking, while [§6.4](../06-4-data-sources.md) promises identical fingerprints across engines | Distinguish business lineage/method from the physical execution engine. Changing valuation source may change meaning; moving the same computation from one engine to another need not. |
| R22 | [Vision](../00-vision.md) requires an exploratory agent's exact call sequence to repeat | Test answer correctness, authorized scope, useful recovery and cost. Require deterministic replay of the built artifact; exploratory agents need not make identical harmless probes. |
| R23 | [§2.4](../02-4-actions.md) says no actions are catalogued, but [§7](../07-the-catalogue.md) includes preview `astronomer.deployment` | Generate catalogue summaries and readiness counts. Distinguish illustrative examples, registered profiles and deployed availability. |
| R24 | [§6.5](../06-5-packages.md) describes six layers but numbers L0–L6, and forbids same-layer imports its examples require | Correct the dependency rule and diagrams; retain the useful import check. Keep tunable operational defaults separate from normative protocol semantics. |
| R25 | [§2.5](../02-5-events.md) makes every event type a capability; [§7](../07-the-catalogue.md) says derived resource/operation events are not capabilities | Define one registration and authorization model. Keep derived change payloads under the resource's watch contract, or catalogue them independently; do not leave clients to infer a second naming system. |
| R26 | [§9.7](../09-7-clients.md) selects arbitrary gateway URLs but keys the shared token file only by environment and refreshes at the bootstrap's authority | Bind credential providers to trusted gateway/authority/audience plus tenant/account/scopes; serialize refresh. An unauthenticated bootstrap cannot redirect existing credentials to an untrusted destination. |

## What makes the current capability documentation hard to use

The catalogue does a good job of collecting the surface area. It does not yet give every capability a complete consumer contract. It mixes source views, proposed profile-generator patterns, business concepts, old route aliases and examples. A reader can often tell where data comes from but not exactly what a row means or what inputs are required.

Three examples illustrate the remaining authoring work:

- `portfolio.holdings`: the example uses a nullable ISIN as part of the key. Cash, derivatives and instruments without an ISIN need an unambiguous row identity, and the owner must settle the grain. The requested columns `isin` and `weight` do not themselves establish uniqueness.
- `signal.fvcm.curves`: selecting the last available curve and reading curve history may imply different time behavior. The capability must state what omitted dates select and whether a range has the same grain.
- `dataset.upload`: it appears in the reaching-systems example but is absent from the 138-entry catalogue. It must be declared an illustration, given a real profile or renamed to an existing resource operation; consumers should not guess.

The [handbook](CAPABILITY-HANDBOOK.md) preserves every catalogue entry and identifies these gaps. It does not invent 138 exact schemas from catalogue names alone. A completed capability card should be generated from a real definition whose examples are validated and executed.

## How to simplify without reducing Atlas's usefulness

| Keep from the first slice | Defer or narrow until justified |
|---|---|
| Both required Go integrations | Additional SQL dialects and engines |
| CLI, Python and Node using identical domain contracts | Universal native representations and one filesystem layout |
| Central identity, resource scope, limits and audit | A general policy language where typed scope rules suffice |
| Profiles with meaning, guide and examples | Arbitrary bidirectional lens algebra |
| Stable operations and uncertain-outcome recovery | Automatic retry of every upstream failure |
| Streaming with explicit completeness | Snowflake chunk handoff until real encoding/access proofs pass |
| Provider interfaces and clear ownership | A fully dynamic remote-provider platform before the first external team |
| The operation/resource semantics a real journey needs | Every family, transport and client extra implemented simultaneously |
| Current policy on execution | A promise that discovery guarantees future success |
| Accurate metadata and provenance | An agent-facing requirement to understand SQL, Arrow or Service Bus |

Keep the existing Go/Python proof as an implementation asset. It records useful fake-backed tests of exact types, evolution, handoff renewal, final batches and process lifetime. It explicitly leaves real source behavior and several SDK features unproved. This review did not rerun those tests. The next proof should complete the CLI and Node path for the same business journey and connect the real Development source, rather than restarting the architecture.

## A practical decision order

1. Agree on the capability card, the common call/result lifecycle and the identity/scope model. These directly determine what the agent can understand and build.
2. Settle contract/document/routing revisions and the smaller compatibility policy. Then write the first complete executable profiles and shared fixtures.
3. Settle idempotency, uncertain outcomes, approval evidence, budget transaction boundaries and audit lifecycle before enabling metered or changing work.
4. Integrate the existing libraries and prove the complete CLI → Python/Node journey with two identities and a newly added capability.
5. Measure direct downloads, events and source delegation on the real systems. Promote only the guarantees those proofs establish.

All steps are proposed implementation work. This review itself changes documentation only.
