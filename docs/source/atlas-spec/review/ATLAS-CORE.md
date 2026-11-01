# Atlas: the proposed core

**Status: design proposal, 2026-10-31.** This document describes the recommended next specification. It is not a claim that the interfaces or capabilities below are deployed. The [review](DESIGN-REVIEW.md) records the changes from the current spec; the [handbook](CAPABILITY-HANDBOOK.md) accounts for every catalogued capability.

## 1. The promise

Atlas is the company's map and execution interface for agents. An agent asks what its user can do, finds a capability expressed in business terms, learns its contract, and uses it. Atlas handles the identity, access decision, source selection, connection, request validation and execution mechanics.

An agent should not need to investigate which Snowflake view, API endpoint, credential or internal team implements a capability. It should need to understand the person's question, choose an appropriate capability and supply meaningful business parameters.

The CLI is how the agent explores and tries the work. Python and Node are how the resulting program repeats it. All three use the same addresses, request records, result meanings and failure semantics.

Atlas does not need an LLM in its execution path. Discovery can rank descriptions and synonyms; domain owners curate authoritative routes. Once chosen, a call is an ordinary validated programmatic request. Ambiguous business intent is made visible before execution.

## 2. The user's route through Atlas

| Step | What the caller learns or does | Public operation |
|---|---|---|
| Orient | Effective identity, environment, roles, authorized scope and relevant defaults | `whoami` |
| Find | Short descriptions of available interactions, ranked for the task | `search`, `catalog` |
| Understand | Purpose, inputs, output meaning, examples, effects, cost and limitations | `guide`; `describe` for the machine contract |
| Inspect when useful | What a particular request would do; an estimate with its uncertainty | The capability's `explain` or `plan`, if declared |
| Execute | Perform the selected interaction | `call(address, request, options)` |
| Follow | Read a stream or page, or follow a durable operation | Shared result and operation helpers |
| Recover | Receive a stable problem code and a useful next step | The same error model in every client |
| Build | Save the addresses and contracts the program depends on | A lock and optionally generated types |

`guide` should be enough for a person to use the capability. `describe` should be enough for a generic client to construct and validate a call. They are two renderings of the same maintained definition, supplemented by current caller information.

Discovery is an authorized view at a point in time. It cannot guarantee that a later call will succeed: limits, source health, credentials and policy can change. Execution always rechecks the relevant facts.

## 3. Identity, access and relevance

Atlas distinguishes three questions:

| Question | Answer comes from |
|---|---|
| Who is acting, and for whom? | Validated identity and delegation credentials |
| What may this caller do and read? | Capability grants, denies, resource/data scope, source controls and current limits |
| What is useful to this caller's task? | Task words, curated guidance, declared defaults and permitted context |

Relevance is a ranking or default, never permission. Having a portfolio-related role does not imply permission to every portfolio. A capability grant does not itself establish row-level entitlement.

Use one caller-context resolver shared by discovery, guides and execution. A capability declares which contextual defaults it accepts and how business identifiers are resolved through authorized lookups. The gateway records the provenance and freshness of those defaults, resolves them into the normalized request and returns their effective values. Ambiguous choices require selection. A program can then pin an explicit choice or deliberately request a documented dynamic default; moving from CLI to SDK must not silently change it.

For each invocation the gateway records the effective principal, calling application or workload, environment and relevant policy revision. Where an agent is distinguishable from an interactive person, that distinction comes from trusted authentication context, not an `is_human` parameter supplied by the agent.

A scheduled job runs under an explicitly authorized workload identity. It does not retain the interactive user's token or silently inherit the user's personal grants. The same capability contract applies, but its available scope can differ.

One configured context may be selected automatically. If several accounts, tenants or environments could apply, the client requires a deliberate selection rather than silently choosing one. CLI and SDK credential providers share the selected context where their runtime permits; browser authentication uses its host application's supported identity flow.

Credentials are bound to a trusted gateway, authority, audience, tenant/account and resource scopes. Concurrent refreshes are serialized. An unauthenticated bootstrap cannot direct a client to send an existing token or refresh credential to an untrusted server or authority.

The normal catalogue contains only visible, authorized interactions. The access view explains missing access to listed capabilities. Confidential capabilities remain undisclosed. Metadata and examples referring to other capabilities follow the same visibility rules.

When Atlas connects as a service identity, it must enforce the caller's resource and data scope before using that broader identity. This is required for the first such integration; it cannot wait for a future general-purpose policy language.

## 4. What a capability must explain

A capability is a named domain concept with an accountable owner. Each interaction is an operation with a declared effect and result. Infrastructure and source details belong in its implementation and diagnostic provenance.

The capability definition must answer the following questions. They are documentation requirements, not a new schema language.

| Part | Required content |
|---|---|
| Identity | Stable id and major version, title, owner, lifecycle and support contact |
| Purpose | The business question answered; when to use it; significant exclusions; authoritative-source guidance |
| Interactions | Supported verbs, each effect, required permission and expected result shape |
| Inputs | Names, types, requiredness, omission versus null, defaults, units, constraints and permitted-value lookups |
| Output | Fields and logical types; units, null meanings and coverage; for tables, row grain and keys |
| Time | Meaning of `as_of`, latest-date selection, timezone, update cadence, freshness evidence and correction policy |
| Selection | Filtering, ordering, projection and paging semantics where offered |
| Execution | Synchronous or durable operation; expected latency class, limits, estimates and any approval requirement |
| Effects | What changes, where, reversibility and how uncertain outcomes are reconciled |
| Data handling | Storage/export restrictions, scope of permitted reuse and retention |
| Reliability | Retry and idempotency behavior; cancellation limits; partial-result and completion semantics |
| Usage | Valid example request and expected result explanation, rendered for CLI, Python and Node |
| Recovery | Specific problems with actionable next steps |

For example, “portfolio holdings” is incomplete until the profile says whether a row represents a security, tax lot or position; whether `weight` is a fraction; which valuation date is selected when omitted; and whether an empty answer means no positions, unavailable data or an unsupported portfolio. These facts must come from the owner, not an inferred SQL view name.

The same standard applies to external data. A vendor adapter must explain coverage, identifier mapping, metering, freshness, failure behavior and permitted use. A normalised vendor capability should keep its Atlas contract when the vendor changes its response.

`upstream` and dynamic-schema access remain explicit escape hatches. They are not substitutes for curated, documented capabilities on which ordinary applications depend.

## 5. Four families; one calling convention

| Family | What it represents | Common interactions | Important expectation |
|---|---|---|---|
| Data | Read a named domain result | `fetch`, optionally `explain` and `export` | Typed records with explicit data semantics and completeness |
| Resource | An object Atlas owns and maintains | `get`, `list`, `create`, `update`, `delete`; declared custom verbs | Versioned state, concurrency control, idempotent changes |
| Action | A change to another system | `plan`, optional `approve`, the action verb | A bounded plan and a durable operation with a receipt |
| Event | A fact that happened | `subscribe`, `publish` where authorized | Declared delivery, retention, ordering and acknowledgement semantics |

An interaction's effect is independent of its result shape. A read may cost money. Publishing an event changes an external system. A reversible resource update and an irreversible deletion need different policies even though both belong to one resource capability.

Every capability need implement only the interactions it actually supports. No fake `explain`, universal filter language or meaningless CRUD method is required to make the interface look uniform.

## 6. CLI, Python and Node parity

The canonical operation is `call(address, request, options)`. Request fields are the same in every language. Options contain transport and client concerns such as deadlines, accepted formats and idempotency keys.

The following illustrates the proposed shared contract using request fields already shown in the existing spec. It is not a claim that all three implementations currently run.

```sh
atlas call portfolio.holdings@v1:fetch \
  --request '{"portfolios":["147HYC"],"columns":["isin","weight"]}'
```

```python
from fiatlas import FIAtlas

atlas = FIAtlas()
result = atlas.call(
    "portfolio.holdings@v1:fetch",
    {"portfolios": ["147HYC"], "columns": ["isin", "weight"]},
)
```

```typescript
import { atlas } from '@atlas/sdk';

const client = await atlas.connect();
const result = await client.call(
  'portfolio.holdings@v1:fetch',
  { portfolios: ['147HYC'], columns: ['isin', 'weight'] },
);
```

Conveniences such as `atlas fetch`, Python attributes and generated typed methods call this same operation. JSON input remains the complete CLI interface for nested objects, arrays, nulls and future request shapes; generated flags are a convenience.

The canonical `call` executes once and returns a result or operation handle. Reading that handle again never silently submits another source query: it either re-reads a retained result or reports that the result cannot be replayed. `explain` is a separate, non-fetching interaction. A lazy request builder may exist as a convenience, but must be named and documented separately from execution.

Clients preserve exact decimals, large integers, timestamps and nulls. A Python `Decimal`, a TypeScript exact representation and a JSON decimal string may differ locally while representing the same value. They must not silently turn an exact amount into a binary floating-point value.

For each result shape, parity covers the logical value, schema, warnings, request identity, data provenance and completion status. It does not require every runtime to use the same in-memory container or to write files at the same location.

Large tables are iterators or batch readers by default. Materialising a whole DataFrame or array is an explicit caller choice. Local file output, dataframe conversion and other extras remain outside capability definitions.

The current Go engine used by the Python proof is worth retaining while measured. The TypeScript implementation must pass the same semantic fixtures. A subprocess is an implementation choice behind Python's public contract; neither moving it into the gateway nor rewriting it is a prerequisite for this simplification.

## 7. Contract evolution and a program's lock

The first release should support a small, dependable compatibility model:

- Keep the meaning of an existing version: grain, units, defaults, effects and time semantics.
- Add optional request fields without changing old defaults.
- Add result fields only when old readers ignore them and newer readers have a defined missing-value behavior for retained older data.
- Keep old accepted names as aliases when a rename is necessary.
- Use a new major version for field removal, incompatible type changes, unit changes or changed business meaning unless a specific, tested compatibility mechanism proves the old contract remains valid.

Do not promise arbitrary reversible lenses. A removed value cannot be reconstructed by a rename or default, and a new floating-point value need not fit an old integer field. Existing experimental lenses can remain experiments without becoming universal client obligations.

Separate three kinds of revision:

| Revision | What it identifies | Cache rule |
|---|---|---|
| Contract fingerprint | Immutable semantic and schema contract | May be retained and bundled with the program |
| Document revision / HTTP validator | The actual served description or representation | Changes when the served content changes |
| Policy and runtime information | Current access, limits, routes, supported delivery and health | Revalidated as appropriate; never authority to execute later |

A lock pins capability versions and reader contracts, includes the contracts needed to start, and records their checksums. It fixes the program's selected route in business terms. It does not freeze permissions, backend addresses, source data or live availability.

“No discovery at runtime” should mean no search and no new business-route selection. Fetching an updated binding or required writer contract is metadata resolution. If absolutely no metadata request is desired, the deployment bundle must contain all necessary metadata and use an explicitly supported stable dispatch route.

For reproducible data, a lock is insufficient. A capability must separately offer a data revision or reproducible snapshot. Where it cannot, the result must disclose its observation and valuation times.

## 8. One execution path

Every gateway invocation follows the same logical order:

1. Validate identity and establish trusted caller context.
2. Resolve the declared address to an admitted capability and interaction.
3. Enforce visibility, deny and grant rules without exposing a confidential contract.
4. Validate and normalise the request, then enforce its resource and data scope.
5. Look up any caller-scoped idempotency key. After current access checks, replay or reconcile its existing operation without a second dispatch or spending reservation; reject a conflicting request.
6. For a new execution, check current confirmation and approval evidence, claim its idempotency identity and reserve limits through a recoverable admission transaction.
7. Record durable admission and dispatch the work through its provider.
8. Return or stream the result, or return the durable operation that owns the work.
9. Record outcome and settle usage, including later source reconciliation where required.

Each attempt, including a replay, refusal or failed authentication, leaves an audit fact. Transport-level size, syntax and timeout checks can reject a malformed request without exposing any capability-specific information; the authorization order governs schema-dependent diagnostics.

The front door owns common policy. The provider owns business meaning and source work. The source adapter owns credentials, driver behavior and encoding conversions. Remote providers receive a narrowly scoped invocation credential bound to the approved request; provider authentication is separate from the effective user's identity.

Start with these boundaries in one Go deployable and a worker where durable work needs it. Split a service when its trust boundary, scale or release ownership justifies it. Use the required [cloud patterns and SQL integrations](INTEGRATIONS.md) from the first working path.

Do not implement an independent entitlement engine in every service. Do supply providers with the enforced scope they need, and retain source-level controls as an additional check.

## 9. Planning, cost and changes

`explain` helps a caller understand a read. `plan` describes a change. Both use the same validation and scope rules as the execution they describe.

An estimate says what was estimated, its unit, basis and uncertainty. Unknown cost remains unknown. Scanned bytes, output bytes and money are distinct quantities. A price or runtime estimate is not a hard cap unless the execution mechanism can enforce it.

For a change or costly execution needing confirmation, the gateway binds the authorization to the effective principal, address, normalized request, plan digest, scope, limits, expiry and relevant policy. A changed plan or increased approved maximum needs a new decision. A request header saying “confirmed” is not evidence that a person approved it.

Idempotency keys identify one logical execution within tenant, principal, environment and capability-interaction scope. They bind to the normalized semantic request and execution-affecting options, not JSON whitespace or a mutable HTTP route. Request normalization must preserve the caller's contract meaning. Metered reads that can incur duplicate charges need this treatment too.

Persist both the submitted request identity and its effective defaults at admission. A retry of the same submitted request uses the original normalization snapshot; it must not choose a new “latest” date or portfolio default. Transport-only changes do not create a new business execution. Current authorization can still refuse access to the retained result.

An uncertain upstream outcome remains recorded and is reconciled before retrying. A timeout or worker crash must not discard the key and authorize a second change. Exactly-once effects are promised only where the upstream mechanism supports them; otherwise the capability describes its reconciliation behavior.

## 10. Results, operations and events

Streaming is the default data path. A stream has an explicit completion marker; EOF alone is not success. A truncated result must be marked incomplete, with any resumable position tied to the same result and scope. Resuming must not silently rerun a changing query and concatenate different snapshots.

Direct downloads are an optional delivery implementation. They must preserve the same logical result, declared types and permitted use. Link issuance and renewal recheck access; the design states whether a link is a bearer credential, its expiry, and what revocation can actually stop. Client support and network reachability determine eligibility.

A durable operation separates the start request from the work it starts. An HTTP disconnect during startup does not mean a successfully admitted durable operation was cancelled. Clients can recover the operation using the same idempotency key.

Cancellation means a request to stop. The operation reaches `cancelled` only when the provider knows work stopped; an already committed effect is reported honestly. Unknown outcomes stay pending reconciliation. Retention must cover the active operation and the promised retry/recovery window, even when work lasts more than 24 hours.

Events remain a useful optional family. A polling view of current state is not equivalent to an ordered history of every event. A fallback may claim equivalence only if it supplies a retained, cursor-addressable history with defined ordering and deduplication semantics. Otherwise clients expose the narrower polling behavior explicitly.

Direct Service Bus delivery is a transport implementation with its own identity, revocation and acknowledgement constraints. Before enabling official publication, demonstrate how invalid messages, unauthorized event types, retention and replay are handled. Client-side validation alone cannot enforce what an independent publisher sends.

## 11. State, limits and audit

Use one durable operation model for gateway work, but do not erase an upstream system's native run identity. An Astronomer run remains an Astronomer run even if Atlas created an operation to trigger it.

Keep immutable catalogue snapshots for efficient reads. Bound the staleness of authorization separately, and choose explicitly which calls may continue during a control-store outage. A mode using stale policy cannot simultaneously guarantee immediate revocation.

Separate operational rate limiting from monetary or licensed-use admission. Exact reservations require a transaction design that actually covers the shared budget. For the first implementation, use a declared budget authority per metered provider; reject unsupported combinations of independently partitioned hard budgets until an atomic design is proved. Do not label approximate per-replica limits exact.

Audit is one logical invocation history: durable admission or refusal, outcome and settlement facts correlated by request, attempt and operation id. Failed authentication is recorded without trusting the claimed identity or retaining credentials. A summary can present one row per invocation. This accommodates streaming and delayed settlement without rewriting an immutable “final” record after it was returned.

For resources, start with a strong ETag that changes whenever the returned representation changes. Keep desired-state `generation` separate. This may cause an occasional refresh before a concurrent update; it is simpler and correct. Introduce a distinct desired-state concurrency token later only if status churn makes that necessary.

Result reuse must include environment, effective authorization scope, request, contract and data revision. Recheck authorization before serving reused data. For the first release, keep results private to their caller unless a reviewed contract proves safe sharing. A protocol flag cannot prevent a caller from saving data it already received; permitted-use restrictions require the appropriate product and organizational controls as well.

## 12. What to deliver first

The initial scope is a complete narrow journey, not a fraction of every subsystem:

1. Integrate the pinned cloud pattern packages and `sqlglot-go` behind Atlas-owned interfaces.
2. Author complete profiles for a small, coherent domain journey, including its identifier lookups and freshness/status capability.
3. Implement bootstrap, identity, authorized catalogue/search, describe/guide, the call path, problems, data streaming and durable operations where that journey needs them.
4. Prove the exact same requests and logical results through CLI, Python and Node against a fixed fixture or declared data revision. Add a capability after the clients are built and use it from all three.
5. Test two identities with different portfolio/data scope, denied and invalid calls, decimal precision, incomplete streams and stale metadata. For metered/changing work, test two callers contending for one shared budget and a repeat after a crash following upstream commit but before receipt persistence.
6. Connect the real Development source and measure the assumptions that fakes cannot prove. Direct Snowflake chunks and Service Bus remain separately gated proofs.

Keep the first successful agent journey early. Its measure is a correct, authorized result with understandable recovery and reasonable cost. Require deterministic route replay from the built artifact; do not require a nondeterministic exploratory agent to issue exactly the same tool sequence on every run.

The first release does not require arbitrary remote provider hosting, a universal bidirectional lens language, multiple SQL backends, every transport, harness lifecycle tools or a design for `bt-research`. The public capability boundary leaves room for those when concrete needs justify them.
