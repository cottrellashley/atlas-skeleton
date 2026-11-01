# Digest: the 2026-10-31 design review, and pass 2's adoption of it

> Historical engineering study (2026-10-31). Retained for its requirements, counterexamples and design reasoning. Implementation observations and line references describe that review snapshot; use [current progress](../../HANDOFF-PROGRESS.md) and the named source symbols for current status. Paths are relative to the repository root unless a citation key says otherwise.

Read in full, every line: the review's README, ATLAS-CORE, DESIGN-REVIEW and INTEGRATIONS; `docs/pass-2/REVIEW.md`; `sdk/go/kit/PROVENANCE.md`. AGENTS.md was also read in full. The handbook was read in part (lines 1-254, 354-365 and 531-596), because section 5's three rules and the journey's catalogue rows are there. It is not counted as read in full.

## Citation keys

| Key | File |
|---|---|
| RM | `docs/source/atlas-spec/review/README.md` (43 lines) |
| CORE | `docs/source/atlas-spec/review/ATLAS-CORE.md` (245 lines) |
| DR | `docs/source/atlas-spec/review/DESIGN-REVIEW.md` (192 lines) |
| INT | `docs/source/atlas-spec/review/INTEGRATIONS.md` (117 lines) |
| HB | `docs/source/atlas-spec/review/CAPABILITY-HANDBOOK.md` (595 lines; read in part, as above) |
| P2 | `docs/pass-2/REVIEW.md` (100 lines). **It governs `PLAN.md` where they disagree (P2:5)** |
| PROV | `sdk/go/kit/PROVENANCE.md` (54 lines) |
| AG | `AGENTS.md` (72 lines) |

A citation reads `CORE §8 L170-180`, meaning ATLAS-CORE, section 8, lines 170 to 180.

**Status of the sources.**
- The review is "a proposal dated **2026-10-31**… It does not change the existing requirements by implication" (RM L7). It is "design proposal… not a claim that the interfaces or capabilities below are deployed" (CORE L3). Its recommendations are "proposed changes, not silently amended source requirements" (DR L3).
- P2 adopts it by the maintainer's instruction: "the product owner asked that the build follow it" (P2 L3). "Where pass 2 now departs from the spec or from `PLAN.md`, this file records the change and the review finding (R01–R26) behind it" (P2 L5).
- So, for this build: **spec §9 is normative for the wire, except where P2 records an R-numbered departure.** P2 governs `PLAN.md`.

---

## 1. ATLAS-CORE, section by section, as testable statements

Each statement is labelled C<section>.<n>. Scope tags: **IN SCOPE** means the first build implements it; **DEFERRED** means it does not; **CONTEXT** means it is background, not a test.

### §1 The promise (CORE L5-13)

| # | Statement | Cite | Scope |
|---|---|---|---|
| C1.1 | Atlas handles "the identity, access decision, source selection, connection, request validation and execution mechanics". The caller supplies only business parameters | L7, L9 | IN SCOPE |
| C1.2 | "All three [CLI, Python, Node] use the same addresses, request records, result meanings and failure semantics" | L11 | IN SCOPE |
| C1.3 | No LLM in the execution path. Once chosen, a call "is an ordinary validated programmatic request". "Ambiguous business intent is made visible before execution" | L13 | IN SCOPE (test: no ranking or model step runs inside `call`; an ambiguous default yields a selection requirement, see C3.5) |

### §2 The user's route (CORE L15-30)

The steps and their public operations (L17-26), verbatim:

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

| # | Statement | Cite | Scope |
|---|---|---|---|
| C2.1 | `guide` alone suffices for a person to use the capability. `describe` alone suffices for a generic client to construct and validate a call. Both are "two renderings of the same maintained definition, supplemented by current caller information" | L28 | IN SCOPE |
| C2.2 | Discovery cannot guarantee that a later call succeeds: "Execution always rechecks the relevant facts". Test: revoke a grant after `describe` or `catalog`; the call is refused | L30 | IN SCOPE |
| C2.3 | `whoami` returns effective identity, environment, roles, authorized scope and relevant defaults | L19 | IN SCOPE |

### §3 Identity, access and relevance (CORE L32-56)

| # | Statement | Cite | Scope |
|---|---|---|---|
| C3.1 | Three questions, three sources: "Who is acting, and for whom?" comes from validated identity and delegation credentials. "What may this caller do and read?" comes from capability grants, denies, resource/data scope, source controls and current limits. "What is useful to this caller's task?" comes from task words, curated guidance, declared defaults and permitted context | L34-40 | IN SCOPE |
| C3.2 | "Relevance is a ranking or default, never permission." A portfolio-related role does not grant every portfolio. "A capability grant does not itself establish row-level entitlement" | L42 | IN SCOPE (§5 below) |
| C3.3 | "Use one caller-context resolver shared by discovery, guides and execution." Test: the same identity yields the same effective defaults in `whoami`, `guide` and `call` | L44 | IN SCOPE |
| C3.4 | A capability declares the contextual defaults it accepts, and how business identifiers are resolved through authorized lookups. The gateway records the provenance and freshness of those defaults, resolves them into the normalized request, and **returns their effective values** | L44 | IN SCOPE |
| C3.5 | "Ambiguous choices require selection." A program can pin an explicit choice, or deliberately request a documented dynamic default. "moving from CLI to SDK must not silently change it" | L44 | IN SCOPE |
| C3.6 | Each invocation records the effective principal, calling application or workload, environment and relevant policy revision | L46 | IN SCOPE |
| C3.7 | The agent-versus-person distinction "comes from trusted authentication context, not an `is_human` parameter supplied by the agent". Test: a request member `is_human` (or any similar member) has no effect on policy | L46 | IN SCOPE |
| C3.8 | A scheduled job runs under an explicitly authorized workload identity. It does not keep the user's token or inherit the user's personal grants. The contract is the same; the available scope can differ | L48 | IN SCOPE as fixture identities. AMBIGUOUS: whether the journey's second identity must be a workload or a person. P2 L30 asks only for "two identities with different portfolio scope" |
| C3.9 | One configured context may be chosen automatically. If several accounts, tenants or environments could apply, the client requires a deliberate selection. CLI and SDK providers share the selected context where the runtime permits. Browser authentication uses the host application's supported identity flow | L50 | IN SCOPE (single context). DEFERRED: the multi-org switching contract (P2 L98) |
| C3.10 | Credentials are bound to a trusted gateway, authority, audience, tenant/account and resource scopes. Concurrent refreshes are serialized. "An unauthenticated bootstrap cannot direct a client to send an existing token or refresh credential to an untrusted server or authority" | L52 | IN SCOPE, R26, step 4 |
| C3.11 | The normal catalogue holds only visible, authorized interactions. The access view explains missing access to listed capabilities. "Confidential capabilities remain undisclosed." Metadata and examples that refer to other capabilities follow the same visibility rules | L54 | IN SCOPE (catalogue and search). AMBIGUOUS: whether `atlas.access` is in the first build. CORE §12.3 L238 lists "bootstrap, identity, authorized catalogue/search, describe/guide, the call path, problems, data streaming and durable operations", without access |
| C3.12 | When Atlas connects as a service identity, it enforces the caller's resource and data scope before using that broader identity. "This is required for the first such integration; it cannot wait for a future general-purpose policy language" | L56 | IN SCOPE, R05 |

### §4 What a capability must explain (CORE L58-84)

The definition must answer these. They "are documentation requirements, not a new schema language" (L62). The table, verbatim (L64-78):

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

| # | Statement | Cite | Scope |
|---|---|---|---|
| C4.1 | Every journey profile answers all 13 parts. Test: a lint or readiness check fails a profile that lacks any part | L64-78 | IN SCOPE (P2 step 2, "pass the gate and the readiness check", L30) |
| C4.2 | Holdings is "incomplete until the profile says": whether a row is a security, tax lot or position; whether `weight` is a fraction; which valuation date is selected when omitted; whether an empty answer means no positions, unavailable data or an unsupported portfolio. "These facts must come from the owner, not an inferred SQL view name" | L80 | IN SCOPE. The fixture fixes a stated choice (P2 L42, L96-97) |
| C4.3 | A vendor adapter explains coverage, identifier mapping, metering, freshness, failure behavior and permitted use | L82 | DEFERRED (no vendor in the journey) |
| C4.4 | `upstream` and dynamic-schema access are explicit escape hatches, not substitutes for curated capabilities | L84 | CONTEXT (`snowflake.query` is not in the journey) |

### §5 Four families, one calling convention (CORE L86-97)

| Family | Represents | Common interactions | Expectation |
|---|---|---|---|
| Data | Read a named domain result | `fetch`, optionally `explain` and `export` | Typed records with explicit data semantics and completeness |
| Resource | An object Atlas owns and maintains | `get`, `list`, `create`, `update`, `delete`; declared custom verbs | Versioned state, concurrency control, idempotent changes |
| Action | A change to another system | `plan`, optional `approve`, the action verb | A bounded plan and a durable operation with a receipt |
| Event | A fact that happened | `subscribe`, `publish` where authorized | Declared delivery, retention, ordering and acknowledgement semantics |

(CORE L88-93)

| # | Statement | Cite | Scope |
|---|---|---|---|
| C5.1 | An interaction's effect is independent of its result shape: "A read may cost money" | L95 | IN SCOPE |
| C5.2 | "Every capability need implement only the interactions it actually supports. No fake `explain`, universal filter language or meaningless CRUD method is required" | L97 | IN SCOPE (`portfolio.search` is `fetch` only, HB L233) |
| C5.3 | All four journey capabilities are the Data family. Resource, Action and Event are not in the journey | HB L231-243, L354-361 | Data IN SCOPE. Event DEFERRED (P2 L15). Action DEFERRED. Resource: see AMBIGUOUS R14 in §2 |

### §6 CLI, Python and Node parity (CORE L99-140)

| # | Statement | Cite | Scope |
|---|---|---|---|
| C6.1 | The canonical operation is `call(address, request, options)`. Request fields are identical in every language. Options carry transport and client concerns: "deadlines, accepted formats and idempotency keys" | L101 | IN SCOPE |
| C6.2 | The illustrative forms (L105-128). They are "not a claim that all three implementations currently run" (L103):<br>CLI `atlas call portfolio.holdings@v1:fetch --request '{"portfolios":["147HYC"],"columns":["isin","weight"]}'`<br>Python `from fiatlas import FIAtlas`; `atlas = FIAtlas()`; `atlas.call("portfolio.holdings@v1:fetch", {...})`<br>Node `import { atlas } from '@atlas/sdk'`; `const client = await atlas.connect()`; `await client.call('portfolio.holdings@v1:fetch', {...})` | L105-128 | IN SCOPE as the target shape. AMBIGUOUS: package names and constructors are "target interfaces… not instructions verified" (HB L59) |
| C6.3 | Conveniences (`atlas fetch`, Python attributes, generated typed methods) call this same operation. "JSON input remains the complete CLI interface for nested objects, arrays, nulls and future request shapes; generated flags are a convenience" | L130 | IN SCOPE: `--request` JSON is mandatory. Conveniences are optional |
| C6.4 | "The canonical `call` executes once and returns a result or operation handle. Reading that handle again never silently submits another source query: it either re-reads a retained result or reports that the result cannot be replayed." Test: count source executions when a result is read twice; the count is 1 (R11 acceptance) | L132 | IN SCOPE |
| C6.5 | "`explain` is a separate, non-fetching interaction." A lazy request builder may exist, but it must be "named and documented separately from execution" | L132 | IN SCOPE (explain is non-fetching: zero source executions) |
| C6.6 | Clients keep exact decimals, large integers, timestamps and nulls. A local representation may differ (Python `Decimal`, a TS exact type, a JSON decimal string), but a client "must not silently turn an exact amount into a binary floating-point value" | L134 | IN SCOPE |
| C6.7 | Parity per result shape covers: "the logical value, schema, warnings, request identity, data provenance and completion status". It does not require the same in-memory container or the same file location | L136 | IN SCOPE |
| C6.8 | "Large tables are iterators or batch readers by default. Materialising a whole DataFrame or array is an explicit caller choice." File output and dataframe conversion stay outside capability definitions | L138 | IN SCOPE |
| C6.9 | The Go engine behind the Python proof is retained while it is measured. "The TypeScript implementation must pass the same semantic fixtures." A subprocess is an implementation choice behind Python's contract | L140 | IN SCOPE. P2 L19, L92: the engine and the Python face come back from the first implementation in step 4 |

### §7 Contract evolution and a program's lock (CORE L142-166)

| # | Statement | Cite | Scope |
|---|---|---|---|
| C7.1 | Within a version, keep its meaning: "grain, units, defaults, effects and time semantics" | L146 | IN SCOPE (gate, R03) |
| C7.2 | "Add optional request fields without changing old defaults" | L147 | IN SCOPE |
| C7.3 | "Add result fields only when old readers ignore them and newer readers have a defined missing-value behavior for retained older data" | L148 | IN SCOPE |
| C7.4 | "Keep old accepted names as aliases when a rename is necessary" | L149 | IN SCOPE (R04) |
| C7.5 | "Use a new major version for field removal, incompatible type changes, unit changes or changed business meaning unless a specific, tested compatibility mechanism proves the old contract remains valid" | L150 | IN SCOPE. OVERRIDDEN BY REVIEW (R03 against spec §3.3 rule 1, `03-contracts.md:538`) |
| C7.6 | "Do not promise arbitrary reversible lenses." Existing lenses "can remain experiments without becoming universal client obligations" | L152 | IN SCOPE. P2 L90: `protocol/resolve` becomes the experimental resolver. A client is obliged only to "aliases, defaults, UNKNOWN and projection". "no served profile may depend on `convert_unit`, `promote` or `drop` within a version" |
| C7.7 | Three kinds of revision (L156-160), verbatim: the **contract fingerprint** identifies the "Immutable semantic and schema contract" and "May be retained and bundled with the program". The **document revision / HTTP validator** identifies "The actual served description or representation" and "Changes when the served content changes". **Policy and runtime information** covers "Current access, limits, routes, supported delivery and health" and is "Revalidated as appropriate; never authority to execute later" | L154-160 | IN SCOPE. OVERRIDDEN BY REVIEW R01 (spec §9.4, §9.6 L95, §9.7 L102) |
| C7.8 | "A lock pins capability versions and reader contracts, includes the contracts needed to start, and records their checksums. It fixes the program's selected route in business terms. It does not freeze permissions, backend addresses, source data or live availability" | L162 | IN SCOPE. OVERRIDDEN BY REVIEW R02. AMBIGUOUS: the lock's format is still open (P2 L99) |
| C7.9 | "'No discovery at runtime' should mean no search and no new business-route selection. Fetching an updated binding or required writer contract is metadata resolution." With no metadata request at all, "the deployment bundle must contain all necessary metadata and use an explicitly supported stable dispatch route" | L164 | IN SCOPE (R02) |
| C7.10 | "For reproducible data, a lock is insufficient." A capability separately offers a data revision or a reproducible snapshot; where it cannot, "the result must disclose its observation and valuation times" | L166 | IN SCOPE: journey results disclose observation and valuation times. AMBIGUOUS: P2 does not say whether the journey offers a data revision; CORE §12.4 says "against a fixed fixture or declared data revision" (L239) |

### §8 One execution path (CORE L168-188)

The order of every invocation, verbatim (L170-180). **IN SCOPE: the pipeline steps are tested in this order.**

1. "Validate identity and establish trusted caller context."
2. "Resolve the declared address to an admitted capability and interaction."
3. "Enforce visibility, deny and grant rules without exposing a confidential contract."
4. "Validate and normalise the request, then enforce its resource and data scope."
5. "Look up any caller-scoped idempotency key. After current access checks, replay or reconcile its existing operation without a second dispatch or spending reservation; reject a conflicting request."
6. "For a new execution, check current confirmation and approval evidence, claim its idempotency identity and reserve limits through a recoverable admission transaction."
7. "Record durable admission and dispatch the work through its provider."
8. "Return or stream the result, or return the durable operation that owns the work."
9. "Record outcome and settle usage, including later source reconciliation where required."

| # | Statement | Cite | Scope |
|---|---|---|---|
| C8.1 | A replay (step 5) happens after the current access checks (steps 3-4). Test: revoke a grant, then repeat an idempotent call with the same key; it is refused, not replayed | L176, and §9 L200 | IN SCOPE |
| C8.2 | A replay makes no second dispatch and spends no reservation. Test: count dispatches and reservations | L176 | IN SCOPE |
| C8.3 | Approval and confirmation evidence is checked only for a new execution (step 6), after the idempotency lookup | L178 | IN SCOPE |
| C8.4 | "Each attempt, including a replay, refusal or failed authentication, leaves an audit fact" | L182 | IN SCOPE (R09) |
| C8.5 | Transport-level size, syntax and timeout checks may reject a malformed request "without exposing any capability-specific information; the authorization order governs schema-dependent diagnostics". Test: an unauthorized caller sending an invalid body to a confidential capability gets no schema diagnostics | L182 | IN SCOPE |
| C8.6 | Ownership. The front door owns common policy. The provider owns business meaning and source work. The source adapter owns credentials, driver behavior and encoding conversions | L184 | IN SCOPE |
| C8.7 | "Remote providers receive a narrowly scoped invocation credential bound to the approved request; provider authentication is separate from the effective user's identity" | L184 | DEFERRED (no remote provider; R20) |
| C8.8 | "Start with these boundaries in one Go deployable and a worker where durable work needs it. Split a service when its trust boundary, scale or release ownership justifies it." Use the cloud patterns and SQL integrations "from the first working path" | L186 | IN SCOPE |
| C8.9 | "Do not implement an independent entitlement engine in every service. Do supply providers with the enforced scope they need, and retain source-level controls as an additional check" | L188 | IN SCOPE |

### §9 Planning, cost and changes (CORE L190-202)

| # | Statement | Cite | Scope |
|---|---|---|---|
| C9.1 | `explain` (for a read) and `plan` (for a change) "use the same validation and scope rules as the execution they describe" | L192 | `explain` IN SCOPE. `plan` DEFERRED (no action) |
| C9.2 | An estimate states what was estimated, its unit, basis and uncertainty. "Unknown cost remains unknown. Scanned bytes, output bytes and money are distinct quantities. A price or runtime estimate is not a hard cap unless the execution mechanism can enforce it" | L194 | IN SCOPE for `explain` |
| C9.3 | A change, or a costly execution needing confirmation, binds its authorization to "the effective principal, address, normalized request, plan digest, scope, limits, expiry and relevant policy". "A changed plan or increased approved maximum needs a new decision. A request header saying 'confirmed' is not evidence that a person approved it" | L196 | IN SCOPE (step 3, R06). OVERRIDDEN BY REVIEW R06 (spec `Atlas-Confirm` as consent) |
| C9.4 | Idempotency keys identify "one logical execution within tenant, principal, environment and capability-interaction scope". They bind to "the normalized semantic request and execution-affecting options, not JSON whitespace or a mutable HTTP route". "Metered reads that can incur duplicate charges need this treatment too" | L198 | IN SCOPE (R08) |
| C9.5 | Admission persists both the submitted request identity and its effective defaults. "A retry of the same submitted request uses the original normalization snapshot; it must not choose a new 'latest' date or portfolio default." Transport-only changes create no new business execution. "Current authorization can still refuse access to the retained result." Test: admit with as_of=null (resolved to D1), advance the fixture so latest is D2, retry with the same key; the result is still for D1 | L200 | IN SCOPE |
| C9.6 | "An uncertain upstream outcome remains recorded and is reconciled before retrying. A timeout or worker crash must not discard the key and authorize a second change." Exactly-once effects are promised only where the upstream mechanism supports them | L202 | IN SCOPE (R08) |

### §10 Results, operations and events (CORE L204-216)

| # | Statement | Cite | Scope |
|---|---|---|---|
| C10.1 | "Streaming is the default data path. A stream has an explicit completion marker; EOF alone is not success." Test: cut a stream before its end marker; every client reports incomplete | L206 | IN SCOPE |
| C10.2 | "A truncated result must be marked incomplete, with any resumable position tied to the same result and scope. Resuming must not silently rerun a changing query and concatenate different snapshots" | L206 | IN SCOPE |
| C10.3 | Direct downloads are an optional delivery. They keep the same logical result, declared types and permitted use. Link issuance and renewal recheck access; the design states bearer-ness, expiry and what revocation stops | L208 | DEFERRED (P2 L15, step 5; R10) |
| C10.4 | "An HTTP disconnect during startup does not mean a successfully admitted durable operation was cancelled. Clients can recover the operation using the same idempotency key" | L210 | IN SCOPE where the journey uses operations (R17) |
| C10.5 | "The operation reaches `cancelled` only when the provider knows work stopped; an already committed effect is reported honestly. Unknown outcomes stay pending reconciliation" | L212 | IN SCOPE (R17) |
| C10.6 | "Retention must cover the active operation and the promised retry/recovery window, even when work lasts more than 24 hours" | L212 | IN SCOPE. AMBIGUOUS: no number is given. HB L144 says the source spec keeps an operation "at least 24 hours after completion" with "a 24-hour idempotency window" |
| C10.7 | Events: polling is equivalent to history only with "a retained, cursor-addressable history with defined ordering and deduplication semantics" | L214 | DEFERRED (R16) |
| C10.8 | Direct Service Bus delivery: before official publication, demonstrate handling of invalid messages, unauthorized event types, retention and replay | L216 | DEFERRED |

### §11 State, limits and audit (CORE L218-230)

| # | Statement | Cite | Scope |
|---|---|---|---|
| C11.1 | One durable operation model for gateway work, without erasing an upstream system's native run id | L220 | IN SCOPE (operations); the Astronomer example is DEFERRED |
| C11.2 | Keep immutable catalogue snapshots. "Bound the staleness of authorization separately." Choose explicitly which calls may continue during a control-store outage. "A mode using stale policy cannot simultaneously guarantee immediate revocation" | L222 | IN SCOPE (R13, step 3) |
| C11.3 | "Separate operational rate limiting from monetary or licensed-use admission" | L224 | IN SCOPE |
| C11.4 | "For the first implementation, use a declared budget authority per metered provider; reject unsupported combinations of independently partitioned hard budgets until an atomic design is proved. Do not label approximate per-replica limits exact" | L224 | IN SCOPE (R07) |
| C11.5 | Audit is "one logical invocation history: durable admission or refusal, outcome and settlement facts correlated by request, attempt and operation id." Failed authentication is recorded "without trusting the claimed identity or retaining credentials". A summary may show one row per invocation | L226 | IN SCOPE (R09) |
| C11.6 | Resources: "start with a strong ETag that changes whenever the returned representation changes. Keep desired-state `generation` separate" | L228 | Per P2 L72: "Resources (after the journey)". See AMBIGUOUS R14 |
| C11.7 | Result reuse must include "environment, effective authorization scope, request, contract and data revision". Recheck authorization before serving reused data. "For the first release, keep results private to their caller unless a reviewed contract proves safe sharing" | L230 | IN SCOPE (R18) |

### §12 What to deliver first (CORE L232-245)

| # | Statement | Cite | Scope |
|---|---|---|---|
| C12.1 | "Integrate the pinned cloud pattern packages and `sqlglot` behind Atlas-owned interfaces" | L236 | IN SCOPE |
| C12.2 | "Author complete profiles for a small, coherent domain journey, including its identifier lookups and freshness/status capability" | L237 | IN SCOPE |
| C12.3 | "Implement bootstrap, identity, authorized catalogue/search, describe/guide, the call path, problems, data streaming and durable operations where that journey needs them" | L238 | IN SCOPE |
| C12.4 | "Prove the exact same requests and logical results through CLI, Python and Node against a fixed fixture or declared data revision. Add a capability after the clients are built and use it from all three" | L239 | IN SCOPE |
| C12.5 | "Test two identities with different portfolio/data scope, denied and invalid calls, decimal precision, incomplete streams and stale metadata. For metered/changing work, test two callers contending for one shared budget and a repeat after a crash following upstream commit but before receipt persistence" | L240 | IN SCOPE |
| C12.6 | "Connect the real Development source and measure the assumptions that fakes cannot prove. Direct Snowflake chunks and Service Bus remain separately gated proofs" | L241 | DEFERRED (P2 step 5, L33) |
| C12.7 | The measure of the first agent journey is "a correct, authorized result with understandable recovery and reasonable cost". "Require deterministic route replay from the built artifact"; do not require an exploratory agent to repeat its exact tool sequence | L243 | IN SCOPE (R22) |
| C12.8 | Not required in the first release: "arbitrary remote provider hosting, a universal bidirectional lens language, multiple SQL backends, every transport, harness lifecycle tools or a design for `backtesting integration`" | L245 | DEFERRED |

---

## 2. R01 to R26: finding, decision, acceptance and build step

The finding, decision and acceptance text is verbatim from DR. "P2 now" is verbatim from the P2 table at L57-84. R01-R12 have full write-ups (DR L30-134). R13-R26 are table rows (DR L138-153) and have **no separate acceptance test**; "derived test" in those rows is my restatement, marked as such.

### R01: a contract fingerprint cannot validate mutable documentation and live access (DR L30-38)
- **Evidence:** §9.4 (`09-4-fingerprints.md:18`) excludes prose and usage from the fingerprint, yet calls it a strong ETag. §9.7 (`09-7-clients.md:102`) caches profiles "by `exact-id`, forever". §9.6 (`09-6-http-binding.md:95`) allows `Cache-Control: max-age=31536000, immutable` on a describe fetched by `exact`. §3.1 (`03-contracts.md:129`) says "never caches availability". (These lines were spot-checked against the spec and match.)
- **Decision:** "Cache the immutable contract separately. Give the full description its own document revision/validator; keep current describe caller state private and uncached initially. Include prose revision in guide generation/cache identity. Reconcile the catalogue's one-hour client cache with the HTTP `no-store` rule."
- **Acceptance (verbatim):** "Change guide wording, revoke a grant and suspend a source without changing the contract digest. The guide updates, invocation is refused appropriately, and no cached access response authorizes the call."
- **P2 now:** "The contract (by fingerprint) is cached apart from the document revision. Describe's caller state (governance, availability) is private and uncached. A guide's identity includes the prose revision". **Step 2.**
- OVERRIDDEN BY REVIEW (R01): spec §9.6 L95 immutable describe caching, and §9.7 L102 profile caching.
- AMBIGUOUS: P2 omits "Reconcile the catalogue's one-hour client cache with the HTTP `no-store` rule". Readings: (a) the catalogue stays `no-store` and clients keep no one-hour cache; (b) the client cache stays and is revalidated. Not decided.

### R02: the lock freezes routing the spec calls safely movable (DR L40-46)
- **Evidence:** §9.7's call algorithm (`09-7-clients.md:88`) picks bindings from the locked or cached profile. §3.3 (`03-contracts.md:595`): "A binding removed after its window | Safe (every client follows served bindings)".
- **Decision:** "Pin semantic contracts, not network addresses. Package reader contracts with the lock; refresh current routing independently. Define 'no discovery at runtime' as no new business-route selection. A lock does not pin the source data or confer access."
- **Acceptance (verbatim):** "Move a provider and update the guide. A locked program still calls the same business operation with the same request meaning, uses the new route and obeys current grants."
- **P2 now:** "A lock pins semantic contracts. Routing is fetched as current metadata. 'No discovery at runtime' means no new business-route selection". **Step 2.**
- OVERRIDDEN BY REVIEW (R02): spec §9.7 step 2 (binding from locked profile) and D39's literal "no discovery at run time".

### R03: full transitive compatibility is not delivered by the lens rules (DR L48-56)
- **Evidence:** §3.3 rule 1 (`03-contracts.md:538`): "Within `@vN`, profiles are compatible fully and transitively". The change table still allows removal after sunset, nullable-to-non-null, and numeric promotion. §9.5.3 (`09-5-resolution.md` L41-52, the lens table) allows backward `promote` to fail with `resolution_failed`, and `drop` backward to be "Nothing (step 5 then supplies a default, `null`, or fails)".
- **Counterexamples (verbatim):** "A required `market_value` disappears; no default can recover its actual old value. An integer becomes a floating-point field and the new producer sends `1.5`; an old integer reader cannot preserve that value. A newer non-null reader encounters historical nulls."
- **Decision:** "Begin with backward-compatible requests, additive optional/defaulted results and stable meanings. Use a new major version for incompatible removals, unit changes or type changes. Keep aliases where their meaning is exact. Introduce more transforms only for demonstrated migrations with explicit information-loss rules."
- **Acceptance (verbatim):** "Compatibility tests contain those counterexamples and reject the change or retain a real old-version implementation. The gate never classifies an unrepresentable old value as safe."
- **P2 now:** "Within a version: optional request fields; result fields only with a defined missing value; aliases for exact renames. A removal, a unit change, a type change, a field made non-null in a result, or a changed default is a new major version". **Step 2 (gate).**
- OVERRIDDEN BY REVIEW (R03): spec §3.3 rule 1, and the change-table rows for removal after sunset, nullable to non-null and promotion.
- Observation, not from the sources: the reviewed implementation contains R03 edits in `sdk/go/protocol/gate/classify.go` (messages cite R03 at L17, L234-334, L600-609) and in `gate_test.go` L249 ("R03's counterexamples").

### R04: the transformation language does not express its own examples (DR L58-64)
- **Evidence:** §9.5 (`09-5-resolution.md:17`) has one lens chain per profile with top-level field operations. The §3.3 rename example (`03-contracts.md:646`) is `{ "portfolios": ["147HYC"], "columns": ["portfolio", "mv"] }`, which needs value translation inside `columns`.
- **Decision:** "keep explicit accepted aliases and their schema scope. If lenses remain, name their target schema/interaction and define projection/mask/filter translation. Validate the original request under its declared contract before transformation, and the transformed request afterward; do not silently discard unknown request fields."
- **Acceptance (verbatim):** "A misspelled field is rejected, and two unrelated fields with the same spelling in request and result are never both transformed accidentally."
- **P2 now:** "Aliases carry their schema scope. A request is validated under its own contract before any transformation, and unknown request fields are refused, never dropped". **Step 2.**
- Difference: P2 omits "and the transformed request afterward". The review requires validation both before and after; build both.

### R05: capability access is not sufficient data scope (DR L66-74)
- **Evidence:** §6.2.5 (`06-2-access-limits-and-audit.md:115`) is "Conditions on the resource (later)". §6.3 (`06-3-reaching-systems.md:80`): for the gateway identity, "Atlas's grant is the only limit (rule 3)".
- **Decision:** "Enforce resource and returned-data scope from the first shared-credential integration. Small, typed rules and curated queries are sufficient; a general policy language can wait. Do not fall back silently from failed user delegation to a service identity. Keep relevance/defaults distinct from permission."
- **Acceptance (verbatim):** "Two users with access to the same capability but different portfolio scope receive only their permitted data, through every delivery path and any result cache."
- **P2 now:** "Resource and returned-data scope is enforced from the first shared-credential source, by small typed rules. Relevance never grants access". **Steps 2 and 4.**
- OVERRIDDEN BY REVIEW (R05): spec §6.2.5 "(later)". Resource conditions are now IN SCOPE.
- P2 omits "Do not fall back silently from failed user delegation to a service identity". It stays a review rule; build it as a test.

### R06: a person's token and a confirmation header do not prove human approval (DR L76-82)
- **Evidence:** §6.2 (`06-2-access-limits-and-audit.md:71`): "Agent for a person… The person's sign-in". `Atlas-Confirm` is used for costly calls.
- **Decision:** "Distinguish authenticated caller context from approval evidence. Bind an approval or confirmation receipt from a trusted interaction to the principal, action, canonical request/plan, scope, approved maximum and expiry. Define what policy tightening does to outstanding plans. Treat approval as live policy, resolving its duplicate placement in contract meaning (`03-contracts.md:699`) and governance (`03-contracts.md:611`)."
- **Acceptance (verbatim):** "An agent cannot authorize a new plan by changing a header, copying another user's receipt, increasing the amount or changing the request after approval."
- **P2 now:** "A receipt from a trusted interaction, bound to the principal, address, normalised request, plan digest, scope, maximum and expiry". P2 L18: "The header is the client's claim only". **Step 3.**
- OVERRIDDEN BY REVIEW (R06): spec D40 / `Atlas-Confirm` as consent.
- AMBIGUOUS: the bound fields differ across the three texts:
  - CORE §9 L196: principal, address, normalized request, plan digest, scope, **limits**, expiry, **relevant policy**.
  - DR R06: principal, **action**, canonical request/plan, scope, approved maximum, expiry.
  - P2 L18/L64: principal, address, normalised request, plan digest, scope, maximum, expiry.
  - Safest reading: bind the union, i.e. P2's set plus the relevant policy revision.
- Not decided anywhere: "what policy tightening does to outstanding plans". Readings: (a) tightening invalidates outstanding receipts; (b) runs already approved stand, per the spec (`03-contracts.md:699`: "Announced; runs already approved stand").

### R07: exact shared budgets do not fit the proposed partition keys (DR L84-90)
- **Evidence:** §6.1.5 (`06-1-front-door-and-services.md:143`): "one transactional batch of conditional patches". §6.2.6 (`06-2-access-limits-and-audit.md:129`): allowances, "pools (shared, firm-wide)" and ceilings. Cosmos batch atomicity covers one container and one logical partition.
- **Decision:** "Separate protective rate limits from hard spending admission. Choose a transaction boundary per shared budget authority, co-locate its atomic reservation set or allocate bounded sub-budgets. Reject unsupported combinations initially. Prove any broader cross-budget scheme before promising exactness."
- **Acceptance (verbatim):** "Different users on different replicas contend for one shared hard budget; combined admissions never exceed it. Abandoned reservations recover, and settlement is idempotent."
- **P2 now:** "One budget authority per metered provider and an atomic reservation set. Unsupported combinations are refused". **Step 3.**
- OVERRIDDEN BY REVIEW (R07): spec §6.1.5 "every window… reserved at once… exactly across every copy" (ATLAS-ACCESS-003) for cross-partition pools.
- P2 drops the "bounded sub-budgets" alternative.
- AMBIGUOUS: which problem code and status the refusal of "unsupported combinations" uses. It is a registration or configuration-time refusal, not a call-time one. Neither source names a code.

### R08: a crash must not release permission to repeat an uncertain effect (DR L92-98)
- **Evidence:** §6.1 (`06-1-front-door-and-services.md:250`): after a lapsed lease the operation ends "`failed` with `unavailable`, releasing its idempotency key so the caller's repeat starts afresh". §6.7 (`06-7-implementation-patterns.md:202`): "Replay | The first answer, stored up to 1 MiB; an operation's start answers the same operation".
- **Decision:** "Retain one logical operation per key throughout its retention, including uncertain outcomes. Use provider deduplication and reconciliation. Fence workers after lease expiry. Include metered reads when a retry can duplicate a charge. Do not promise exactly-once external effects solely from HTTP deduplication."
- **Acceptance (verbatim):** "Kill the worker after upstream commit but before receipt persistence; retry the original key. It returns/reconciles the original operation and creates no second effect."
- **P2 now:** "One logical operation per key for its whole retention, uncertain outcomes included. Workers are fenced after their lease and reconciled". **Step 3.**
- OVERRIDDEN BY REVIEW (R08): spec §6.1 L250's key release on takeover. The lease numbers in that line (renewed every 10 seconds, lapsed after 30, at most three resumes) are not changed by the review.
- AMBIGUOUS: the review does not say which problem code a still-uncertain operation returns on retry.

### R09: one immutable final audit record cannot contain later settlement (DR L100-106)
- **Evidence:** §6.2.8 (`06-2-access-limits-and-audit.md:146`): "Every call, allowed or refused, leaves one record". §6.1 settles handoffs later.
- **Decision:** "One logical invocation history, represented by immutable admission, outcome and settlement facts. Present a joined summary when people want one row. Specify the acknowledgement point and recovery when an effect succeeds but recording completion fails."
- **Acceptance (verbatim):** "Crash at each boundary. The history preserves what is known, correlates later settlement and exposes an unresolved outcome rather than inventing a completed one."
- **P2 now:** "Immutable admission, outcome and settlement facts, joined for reading". **Step 3.**
- OVERRIDDEN BY REVIEW (R09): spec §6.2.8 single record, and the `audit_record` of §9.3.2 as the only shape.
- P2 omits "Specify the acknowledgement point". AMBIGUOUS: the acknowledgement point is not specified anywhere.
- Correlation keys: "request, attempt and operation id" (CORE L226).

### R10: delivery credentials require technology-specific guarantees (DR L108-116)
- **Evidence:** §6.3 says OBO tokens never leave the gateway, yet its ticket remediation sends them. Azure user-bound user delegation SAS uses `sduoid` from service version `2025-07-05`.
- **Decision:** "Keep client SQL tickets deferred. For each offered handover record its actual target, rights, identity binding, expiry, renewal and revocation limits. Prove the SDK/network path for any stronger guarantee."
- **Acceptance (verbatim):** "Exercise intended use, copied credentials, changed targets, expired links, renewed links and revoked access for the actual Development system and each supported client."
- **P2 now:** "Each handover records its real target, rights, identity binding, expiry, renewal and revocation. Client SQL tickets stay deferred". **Step 5. DEFERRED.**

### R11: client parity is behavior, not a shared filesystem or universal Arrow (DR L118-124)
- **Evidence:** §9 Core level (`09-atlas-protocol.md:45`, the levels table) is JSON. AP-CLIENT-005 (`09-7-clients.md:198`) says "a table as Arrow in memory". A browser cannot hold the token file.
- **Decision:** "Define one public lifecycle: a call executes once and yields a result/operation handle; reading a result does not silently execute again. Explain is a separate operation. Use runtime-appropriate credential providers and idiomatic values. Core can return JSON; Arrow and direct downloads are optimizations with identical logical meaning."
- **Acceptance (verbatim):** "Compare identity, canonical requests, values, errors, deadlines, completeness and usage across CLI/Python/Node. Count source executions when reading a result twice. A browser client needs neither a token file nor local disk output."
- **P2 now:** "`call` executes once and returns a result or operation handle. Core is JSON, and Arrow is an optimisation of the same meaning. Credentials come from each runtime's own provider". **Step 4.**
- OVERRIDDEN BY REVIEW (R11): AP-CLIENT-005's "a table as Arrow in memory" and the mandatory token file for every client. P2 L12: Node is in scope ("CLI, Python and Node from the first release, one request record in all three").
- AMBIGUOUS: the exact result-handle methods "should be settled consistently across clients before examples are published" (HB L102). No source names them.

### R12: implementable schemas and unambiguous encodings (DR L126-134)
- **Evidence:** §9.8 (`09-8-conformance.md:20`) marks much of the corpus "to write". The encoding conflicts are: JSON property order versus Arrow order; dynamic tables as arrays and as `columns` objects; absence becoming null in Arrow; varying timestamp precision; Atlas annotations both ignored and required.
- **Decision:** "Publish a small executable schema corpus with the first slice. Define explicit table column order, one timestamp precision, null/absence normalization and a table schema subset the encodings can actually carry. Define dynamic result metadata framing. Generate standard schema constraints or require an explicit Atlas validation layer."
- **Acceptance (verbatim):** "Independently decode the same fixtures in Go and TypeScript, including large integers, exact decimals, null/absence, reordered JSON properties, dynamic tables and incomplete streams. A client built from the delivered artifacts can pass that initial scope."
- **P2 now:** "Done in Phase 1 for the journey's protocol: the 21 schemas and every vector pass". **Done.**
- Verified: `spec/protocol/references/schemas/` holds 21 `*.schema.json`. `vectors/` holds `arrow.json`, `fingerprints.json`, `grammar.json`, `guard.json`, `handoff/`, `headers.json`, `problems.json`, `profiles/`, `resolution.json`, `schemas.json` and `structured-fields.json`.
- AMBIGUOUS / PARTIAL: R12's acceptance needs **TypeScript** to decode the same fixtures independently. Phase 1 is Go. With Node now in scope, the TS decoding half is outstanding and belongs to step 4 (Node client). Treat "Done" as covering the Go half only.

### R13 to R26 (DR L138-153; P2 L71-84)

| R | Finding (DR) | Resolution (DR, verbatim) | P2 now (verbatim) | Step / scope | Derived test (my restatement) |
|---|---|---|---|---|---|
| R13 | §6.1 degraded mode may use policy for 60 minutes. §6.7 has a 60-second grant target and a 10-minute group lookup cache | "Separate policy, identity-group, active-connection and issued-link freshness bounds. A switch stored behind the same failed control channel cannot guarantee immediate emergency revocation." | "Separate freshness bounds for policy, groups, connections and links. A switch behind a failed control channel promises no immediate revocation" | 3, IN SCOPE | Each bound is configured and tested on its own. Docs do not claim immediate revocation in degraded mode. AMBIGUOUS: no values are given for the new bounds; the spec's 60 min, 60 s and 10 min remain the only numbers |
| R14 | §9.3 resource ETags do not change when the returned `status` changes | "Use a full-representation ETag initially; keep desired-state generation separate. Do not lose status changes behind a 304 response." | "The ETag covers the full representation; `generation` is separate" | "Resources (after the journey)". DEFERRED per P2 | AMBIGUOUS: `atlas.operation@v1` is a Resource (`get`, `list`, `watch`; `cancel`; HB L221). If the journey uses durable operations (C12.3), its operation resource may need R14 now. Readings: (a) operations are read without ETag/304 in the journey, so deferral is safe; (b) apply R14 to operations now |
| R15 | §6.1 calls the change feed the outbox | "Write explicit immutable outbox facts where every event matters; coalesce only notifications that mean 're-read current state.'" | "Explicit immutable outbox facts where every event matters" | 5, DEFERRED | n/a |
| R16 | §2.5 polling is a universal fallback; relies on client validation of publishers | "Require cursor-addressable retained history for equivalent polling. Define explicit acknowledgement after handling. A direct sender's authorization/validation must be enforced beyond an SDK convention." | "Equivalence is claimed only with cursor-addressable retained history" | 5, DEFERRED | n/a |
| R17 | §4.1 couples disconnect to cancellation | "Separate start-request deadline, execution deadline and operation retention. Report requested versus confirmed cancellation; preserve uncertain committed effects." | "The start deadline, execution deadline and retention are separate. Cancellation is reported as requested or confirmed" | 3, IN SCOPE | A disconnect after admission leaves the operation running. Cancel shows requested, then `cancelled` only when confirmed. Also fixes the kit's deadline defect (PROV L29) |
| R18 | §5 result cache keyed by address, request and fingerprint only | "Include authorization context, environment and data revision; reauthorize reuse. Initially avoid automatic persistent or cross-user reuse, especially licensed data." | "Results are private to their caller; any reuse is keyed by authorisation, environment and data revision, and reauthorised" | 4, IN SCOPE | User B never gets user A's cached rows. A revoked user gets no cached result |
| R19 | §2.2.1 uses EXPLAIN bytes to choose transport | "Label heuristics and measure output. Snowflake's `bytesAssigned` describes partitions that may be scanned, not output size or an exact bill." | "Heuristics are labelled, and output is measured" | 5, DEFERRED (transport choice). `explain` estimates still follow C9.2 | n/a |
| R20 | §6.1 requires remote-loopback admission while the transport is open | "Keep in-process provider conformance first; require remote transport/authentication conformance on first extraction. Preserve serializable boundaries now." | "In-process provider conformance first; remote transport conformance when a provider is first extracted" | 4, IN SCOPE (in-process only) | Provider requests and responses round-trip through a serializer in tests. P2 L13: "The provider boundary stays in process and serialisable". AGENTS.md L66 still says "in process and over loopback"; P2 governs |
| R21 | §3 makes any source change breaking; §6.4 promises identical fingerprints across engines | "Distinguish business lineage/method from the physical execution engine. Changing valuation source may change meaning; moving the same computation from one engine to another need not." | "Business lineage and method are distinguished from the execution engine" | 2, IN SCOPE | Swapping DuckDB for Snowflake leaves the fingerprint unchanged. Changing the business source or method is classified as a meaning change |
| R22 | Vision requires an exploratory agent's exact call sequence to repeat | "Test answer correctness, authorized scope, useful recovery and cost. Require deterministic replay of the built artifact; exploratory agents need not make identical harmless probes." | "Answers, scope, recovery and cost are tested; the built artifact replays deterministically" | 4, IN SCOPE | The built program replays the same calls deterministically |
| R23 | §2.4 says no actions are catalogued; §7 includes preview `astronomer.deployment` | "Generate catalogue summaries and readiness counts. Distinguish illustrative examples, registered profiles and deployed availability." | "Catalogue summaries and counts are generated; examples, registered profiles and deployed availability are kept distinct" | 2, IN SCOPE | Catalogue counts come from registered profiles, never hand-written. Examples are labelled |
| R24 | §6.5 describes six layers but numbers L0-L6, and forbids same-layer imports | "Correct the dependency rule and diagrams; retain the useful import check. Keep tunable operational defaults separate from normative protocol semantics." | "`layers.toml` is the rule. The import check stays, and same-layer imports are allowed" | Done (`tools/layercheck`; `sdk/go/layers.toml` exists) | P2 omits "Keep tunable operational defaults separate from normative protocol semantics"; it still applies |
| R25 | §2.5 makes every event type a capability; §7 says derived events are not | "Define one registration and authorization model. Keep derived change payloads under the resource's watch contract, or catalogue them independently; do not leave clients to infer a second naming system." | "One registration model; derived change events live under the resource's watch contract" | 5, DEFERRED | P2 picks the first of the review's two options |
| R26 | §9.7 selects arbitrary gateway URLs but keys the token file by environment only | "Bind credential providers to trusted gateway/authority/audience plus tenant/account/scopes; serialize refresh. An unauthenticated bootstrap cannot redirect existing credentials to an untrusted destination." | "Credentials are bound to the trusted gateway, authority, audience, tenant or account, and scopes. Refresh is serialised, and an unauthenticated bootstrap cannot redirect a credential" | 4, IN SCOPE | A bootstrap from gateway X naming authority Y (untrusted) causes no token to be sent. N concurrent refreshes cause exactly 1 refresh request. OVERRIDDEN BY REVIEW (R26): §9.7 token file keyed by environment |

---

## 3. INTEGRATIONS in full detail (INT)

### 3.1 Local integration modules

| Module | Role |
|---|---|
| `sdk/go/kit` (part of `atlas.local/sdk/go`) | Reusable gateway mechanisms behind the central invocation path |
| `sqlglot` (`atlas.local/sqlglot`) | SQL AST construction, parsing, qualification and strict generation behind the data adapter |

Build these with a compatible toolchain. The SQL library stays behind Atlas's typed planning boundary. A backtesting integration remains a future capability; its internal design is deferred.

### 3.2 The one path (INT L18-35)

The flow is: Clients (CLI, Python, Node), then the Atlas invocation pipeline (the cloud patterns supply mechanisms), then the domain capability provider, then the Atlas SQL compiler and guard (sqlglot supplies AST, parsing and generation), then the Snowflake adapter, then Snowflake with a constrained identity, then a checked result under the capability contract, back to the clients (L20-31).

- "The public profile exposes business parameters and types. It does not expose Go AST types or the cloud kit's internal models. Atlas owns the integration interfaces" (L33). Test: no profile, describe or guide output contains a kit or sqlglot type name.
- Reuse the local shared kit and SQL module. Preserve third-party notices and regression coverage; release dependencies must be reviewed and pinned.

### 3.3 Shared patterns: integration responsibilities

| Mechanism | Existing value | Atlas responsibility |
|---|---|---|
| JWT verifier the `auth` pattern and middleware the `auth` pattern | Signature/claim verification and authentication plumbing | Require configured issuer, audience and tenant; establish stable identity/delegation context; add Entra-specific behavior and OBO where required |
| Idempotency the `idempotency` pattern | Request ownership and response replay mechanism | Replace method/path/raw-body identity with caller-scoped canonical business requests; activate by semantic execution needs; retain uncertain effects instead of dropping keys on 5xx or response overflow |
| Long operations the `lro` pattern | An operation manager and lifecycle foundation | Persist key-to-operation ownership, use bounded durable workers and leases/fencing, reconcile on restart; process-local maps and goroutines do not provide restart safety |
| Rate limiting the `ratelimit` pattern | Fixed-window counters and local protective primitives | Add the selected hard-budget transaction model, reservations, settlement and shared concurrency semantics |
| Retries the `resilience` pattern | Backoff and retry throttling | Add replay safety, deadline and operation recovery rules; do not claim its token budget is the spec's exact 10%-per-minute rule |
| Configuration snapshots the `config` pattern | Validated atomic publication | Serialize catalogue construction or use correct compare-and-swap; add revision monotonicity, persistence and freshness policy |
| Reconciliation the `reconcile` pattern | Queue and resync model | Supply durable ownership, distributed leases and event/outbox handling where needed |
| Registry the `registry` pattern | Provisioning and module lifecycle | Use explicit registration; discard a partially failed new generation completely; drain old users before cleanup |
| Resource store the `resource` pattern | State/version management | Add Atlas masks and mandatory preconditions; adapt its envelope; settle representation ETags versus desired generation first |
| Audit guidance (telemetry versus audit design) | Separation of operational telemetry and audit concerns | Implement durable invocation lifecycle facts, source reconciliation and a queryable audit view |

- Reference gateway/kernel designs use in-memory state and authentication arrangements that differ from Atlas. Their example defaults are not production guarantees.

**Shared kit components reviewed:** the following table records their responsibilities, useful tests and adaptation decisions.

| kit pkg | Pattern | Files | Tests carried | Local changes |
|---|---|---|---|---|
| `health` | `health` | `health.go` | `health_test.go` | Header. `http.Error` replaced with a status and body write (atlasvet) |
| `hot` | `config` | `config.go` | `config_test.go` | Header and package name |
| `httpserver` | `httpserver` | `httpserver.go` | `httpserver_test.go` | Header |
| `jwt` | `auth` | `jwks.go`, `jws.go`, `verifier.go` | `jwt_test.go` (from `auth_test.go`) | Header and package name. The EC on-curve check uses `crypto/ecdh`, not deprecated `IsOnCurve` (staticcheck SA1019). `TestMiddleware` is not carried (auth middleware not imported yet) |
| `lifecycle` | `lifecycle` | `lifecycle.go` | `lifecycle_test.go` | Header |
| `middleware` | `middleware` | `middleware.go` | `middleware_test.go` | Header. Failures are RFC 9457 problems through `kit/problem`, not `apierror`. The deadline header is an RFC 9651 Date (`Atlas-Deadline`), not RFC 3339, and `X-Request-Deadline` is no longer sent |
| `telemetry` | `telemetry` | `accesslog.go`, `logging.go`, `route.go`, `traceparent.go`, `writer.go` | `telemetry_test.go` | Header. The access log no longer names the principal or the upstream |

- `kit/problem` is "Atlas's own: the RFC 9457 writer that replaces `apierror`" (PROV L27).
- `kit/clock` and `kit/conc` exist in the tree but are not in PROV. Their package docs show they are Atlas's own (clock: "inject the clock"; conc: "ATLAS-MECH-012"). There are no upstream headers. Observed by `head` on the files.
- **Known defect** (PROV L29): "`middleware.Deadline` turns a client deadline already in the past into the route's maximum, instead of refusing the call. It also reads the wall clock rather than an injected one. Fixed in step 3, with R17."
  - Verified in `sdk/go/kit/middleware/middleware.go` L304-322: `until := time.Until(t)` uses the wall clock. A negative `until` makes `d` negative, and then `if max > 0 && (d <= 0 || d > max) { d = max }`. Also, when `max == 0` and `d <= 0`, the handler runs with no deadline at all (L319-321).
- **Mechanisms still required in the historical plan**, each with the step that needs it (PROV L33-44): `auth` (middleware), `idempotency`, `lro`, `ratelimit`, `resilience` (retry), `reconcile`, `registry`, `resource` (store). Their Atlas responsibilities match the table above. PROV adds R08 to idempotency, R07 to ratelimit and R14 to resource, and says of retry: "Its token budget is not claimed to be the spec's one-in-ten rule".
- Licences: `sdk/go/THIRD_PARTY_NOTICES.md` (PROV L3). It records the applicable notices for adapted files.

### 3.4 sqlglot integration rules (INT L56-98; PROV L46-54)

**What it is** (INT L58): base, Snowflake and DuckDB dialects; AST construction, parsing, generation, optimization and qualification. "Its logical planner and in-memory executor are not a Snowflake driver or a warehouse cost estimator. SQL Server and Postgres are not already available."

**Local implementation.** The SQL library is the `sqlglot/` module (`atlas.local/sqlglot`). Its tests and third-party notices stay with it; Atlas uses it through the compiler and guard rather than exposing AST types to callers.

**What is used** (PROV L52): "Full-statement parsing (never `ParseOne` on text that is then executed), the AST, strict generation in the Snowflake and DuckDB dialects, and qualification under a strict configuration. The optimiser and the in-memory executor stay out of production". INT L98: "Do not bring the library's entire optimizer or in-memory executor into production solely because they exist."

**The Atlas SQL compiler and guard interface** (INT L60): "accepts the capability's approved model, request, scope and schema metadata, and returns a validated execution plan. Keep the library's AST internal to that integration."

**The planned-query path** (INT L62-69), verbatim and in order:
1. "Validate the business request and authorized data scope before planning."
2. "Build a typed AST from reviewed source objects and the capability's declared columns and filter operators. Bind scalar user values through the driver; never construct SQL by string concatenation."
3. "Qualify names using known catalog/database/schema metadata. Walk every relevant nested scope, source and function against Atlas's allowlists."
4. "Generate strictly in the target dialect and validate the generated statement again. Refuse unsupported forms."
5. "Return an immutable plan containing SQL, parameter bindings, referenced objects, output contract, request identity and provenance. Both `explain` and execution use this compiler path. A byte-identical execution promise requires the same retained plan and bindings, not two independent compilations after metadata changes."
6. "Let the Snowflake adapter own driver execution, source estimates, cancellation and returned Arrow/type checks. Test the actual source's semantics rather than treating a successful transpilation as proof."

**Four source-level details** (INT L71-78), verbatim:

| Inspected behavior | Integration rule |
|---|---|
| `ParseOneOpts` (`api.go#L148`) returns the first parsed expression | Use full-statement parsing and require exactly one executable statement. Do not validate the first statement and execute unchecked original text. |
| Unsupported statement parsing (`parser_statements.go#L57`) can produce `exp.Command` | Parse success is insufficient. Reject raw, command and unknown nodes that the Atlas policy does not explicitly support. |
| Default generation (`exp/hooks.go#L54`) uses warning behavior for unsupported features | Configure unsupported transformations to fail, and test that no driver call occurs on failure. |
| Qualification options (`optimizer_qualify.go#L117`) expose mechanisms, including configurable validation | Atlas chooses a strict reviewed configuration. Qualification is not resource authorization. |

**Further rules:**
- "A SELECT root is not enough to establish safety. The guard must inspect CTEs, nested queries, functions, identifiers and sources. Unapproved dynamic identifiers, stage/file access, external functions and UDFs need an explicit policy. A constrained source identity remains a separate enforcement layer" (L80).
- "Bound SQL input size, AST depth/nodes and compilation concurrency. The inspected parse/optimization API does not accept a cancellation context; abandoning a goroutine does not stop its computation. Use bounded transformations and isolate work if hard execution limits require it" (L82). AMBIGUOUS: no numeric bounds are given. The build must choose them and record the choice.
- `snowflake.query` uses the same strict guard but "remains an explicitly privileged dynamic-schema capability" (L84). DEFERRED (not in the journey).
- Raw SQL: "object/function allowlists alone cannot enforce row or column entitlements under a broad service credential. Use source-enforced caller scope, secured views or a proven restricted rewrite, and refuse the capability where those controls cannot enforce its authorized data boundary. Arbitrary subqueries or UDFs must not bypass that boundary" (L86).

### 3.5 Boundary tests (INT L88-96), verbatim. IN SCOPE (step 4)

- "Accepted sanctioned queries produce the expected fields, units, nulls and types."
- "Multiple statements, unsupported commands, inaccessible objects, unapproved functions, dynamic identifiers and excessive inputs are refused before driver execution."
- "Quoted names, case handling, CTEs and subqueries do not evade scope checks."
- "Driver parameter binding handles values independently from identifiers and SQL syntax."
- "`explain` and execution use the same plan construction and policy context."
- "DuckDB fixtures agree with the intended logical results; real Snowflake tests cover decimals, timestamps, nulls, identifiers, functions and any dialect differences." The DuckDB half is IN SCOPE; the real Snowflake half is DEFERRED to step 5.
- "A partial stream, cancellation or repeated metered call obeys Atlas's result and operation contract."

PROV L54 adds: "The references' `vectors/guard.json` is the first of them" (the file exists at `spec/protocol/references/vectors/guard.json`).

### 3.6 The first end-to-end acceptance result (INT L100-113; P2 L44-51)

"Choose a coherent domain journey already in the catalogue, such as holdings plus its identifier lookup and freshness information. Author complete profiles rather than mechanically exposing all source views" (INT L102).

| # | INT L106-111 (verbatim) | P2 L46-51 wording difference |
|---|---|---|
| 1 | The shared gateway kit and SQL library participate in the running path, with integration tests and applicable notices. | Same integration obligation for the local modules. |
| 2 | "The same person discovers and calls the same capability from CLI, Python and Node using identical request records." | same |
| 3 | "A second person with narrower data scope receives a correspondingly narrower result or a clear refusal." | same |
| 4 | "A capability added after the clients were built works without releasing any of those clients." | same |
| 5 | "Invalid requests, permission changes, exact decimals, incomplete streams, idempotent retries and source failures have matching logical behavior across clients." | "behave the same in every client" |
| 6 | "The guide's example succeeds and its result can be checked independently against the source." | "**against the source fixture**". P2 narrows "the source" to the fixture, since the Development source is step 5 |

- "The existing Go/Python hermetic proof reduces the work needed to start. It does not remove the need for CLI/Node parity or live source proofs. Its report explicitly identifies assumed Snowflake chunk behavior, missing Windows lifetime handling and unimplemented client features" (INT L113).
- Evidence limit: this historical design review did not run the kit or SQL library tests. Library-level claims do not establish Atlas integration behavior; the local module tests and end-to-end journey must verify the actual running path.

### 3.7 The Development source

INTEGRATIONS has no section of this name. The review's statements about it, in full:
- CORE §12.6 L241: "Connect the real Development source and measure the assumptions that fakes cannot prove. Direct Snowflake chunks and Service Bus remain separately gated proofs."
- INT L69: "Test the actual source's semantics rather than treating a successful transpilation as proof."
- INT L95: "real Snowflake tests cover decimals, timestamps, nulls, identifiers, functions and any dialect differences."
- INT L113: the hermetic proof "does not remove the need for… live source proofs".
- DR R10 acceptance L116: "for the actual Development system and each supported client".
- DR L182: "connect the real Development source, rather than restarting the architecture".
- DR L190 (decision 5): "Measure direct downloads, events and source delegation on the real systems. Promote only the guarantees those proofs establish."
- RM L41: hermetic results "are not proof of real Snowflake, Entra, Cosmos DB or Service Bus behavior".

Adoption: P2 step 5 L33: "**Real systems:** the Development source, direct downloads, events, delegated identity | Out of reach from this machine. The plug-in points and gated proofs are ready, and only the guarantees a proof establishes are promoted". **DEFERRED.** The first build must leave the plug-in points (the Snowflake adapter behind the same seam as the DuckDB fake) and gated proofs ready.

---

## 4. The journey: what "complete" means for each capability

Sources: P2 L35-42; CORE §4 (13 parts), §12; HB page contract L106-125 (12 parts), holdings page L146-168, catalogue rows L231-243 and L354-361. P2 L42: "Their profiles are written to the handbook's page contract. Where an owner must decide something… the profile says so and the fixture fixes a stated choice."

**Common completeness criteria for all five (IN SCOPE):**
1. The profile answers CORE §4's 13 parts and HB's 12 page parts: Promise, Use / do not use, Behavior, What to supply, What to expect, Time, Authority, Cost and size, Execution, Change behavior, Examples, Failure and recovery (HB L110-123).
2. It passes the gate and the readiness check (P2 L30).
3. Its examples are validated against the request schema and executed. The same request is generated for CLI, Python and Node (HB L591; CORE §4 "Usage").
4. It is discoverable via `catalog` and `search` by an authorized identity, and invisible (or explained by access) for one without the grant (CORE §3 L54).
5. `guide` and `describe` are rendered from one definition (CORE §2 L28; HB L108).
6. Fixtures cover two identities with different portfolio scope (P2 L30).
7. Each owner question is answered by a stated fixture choice (P2 L96-97).
8. "A capability is ready when a human can answer 'Is this the right question, what exactly will happen, what will I get, what might fail, and how do I call it?' and an agent can express that call unchanged in the CLI, Python and Node" (HB L595).

| Capability | Catalogue facts (HB) | Interactions and delivery | Must be settled (fixture fixes a stated choice) | Order |
|---|---|---|---|---|
| `portfolio.holdings@v1` | "Read positions → portfolio × security × date, value, weight, analytics", "Portfolios and date; merged history/range semantics; chunked" (L242). Sample inputs: required `portfolios` (array of codes); optional `as_of` (date/null); optional `asset_class` (`credit`, `rates` or null); optional `columns` (string array/null) (L157). Time: "The portfolio's last valuation on or before `as_of`" (L159). Grain "Portfolio × security × date"; sample keys `portfolio`, `isin`, `as_of` (L160). Fields `portfolio`, nullable `isin`, `as_of`, `market_value` as Money, `weight` as Fraction, nullable `oas` as basis points (L161). Problems `portfolio_not_found`, `warehouse_suspended` (L164). Identifier lookup via `portfolio.search`/`portfolio.list` (L158) | `fetch`, `explain`, `export`, each a read (L156). Chunked fetch (L233). The handbook request example (L63-68): `{"portfolios":["147HYC"],"as_of":"2026-10-28","columns":["portfolio","isin","as_of","weight"]}` | HB L168: history/range request; null `as_of`; same snapshot date across portfolios; keys for cash, derivatives and no-ISIN securities; duplicate-lot aggregation; market-value currency and signs; whether weights include cash and sum to one; partial authorization or missing portfolios; ordering and row limits; source-correction/freshness; `Licensed` trait persistence. CORE L80: row = security, tax lot or position; empty-answer meaning. HB L166: "how the caller detects and rejects an excessively old snapshot". DR L161: "a nullable `isin` cannot by itself distinguish every security" | First |
| `portfolio.list@v1` | "Enumerate current portfolios → code, name, region, asset class", "Current catalogue; exact filters unspecified" (L237) | Portfolio curated convention: `fetch`, `explain`, `export` read; single table (L198, L233) | Filters; scope filtering (each identity sees only its permitted portfolios, R05); ordering and paging | First |
| `portfolio.search@v1` | "Resolve loose portfolio names → ranked codes and reasons", "Search words; use returned codes in subsequent requests" (L238) | "`fetch` only and gateway execution only" (L233) | Ranking explained ("reasons"); results only within the caller's authorization (relevance rule); ambiguous matches surface as candidates, never auto-chosen (HB L15, CORE L44) | First |
| `data.status@v1` | "Check freshness → source latest date, expected date and status", "Data, `fetch`, single table; source/date behavior requires clarification" (L361). Domain: "Official, gateway execution… a source is not itself a license to invoke every capability reading it" (L356). Old name `snowflake.freshness` maps to `data.status` (L552) | `fetch`, single table | Source/date behavior. HB L544: "A latest snapshot is not a replayable landing history. Define whether fallback means latest-state reconciliation or event replay; do not promise both". Three-facts rule: a healthy status does not prove "the date has landed" for a caller (L42). Status enum values: AMBIGUOUS (none given in the review) | First |
| `portfolio.holdings-dates@v1` | "Find valuation dates → portfolio/date, position count and total market value", "Portfolio/date window" (L243) | Curated convention; single table (L233) | The date-window semantics; consistency with holdings' "on or before" rule; scope per identity | **Added after the clients are built.** Acceptance #4: works from CLI, Python and Node "without releasing any of those clients" (INT L109) |

AMBIGUOUS, holdings `export` (and the `explain`/`export` of the curated convention):
- `explain` is IN SCOPE. It is the compiler-path boundary test (INT L94) and is part of CORE §2.
- `export` is unresolved. Readings: (a) deferred, because direct downloads, handoff and transfer are deferred (P2 L15; DR L176 "Snowflake chunk handoff until real encoding/access proofs pass"); (b) offered via gateway streaming only. P2 does not decide. CORE §5 L97 allows declaring only the interactions supported, so a profile may omit `export`.

AMBIGUOUS, whether journey reads are metered. HB L163 says "Warehouse costs, quotas and licensing may still apply". P2 step 3 builds R06, R07 and R08 against fakes regardless ("Each R's acceptance test passes against the fakes", L31). CORE §12.5's budget-contention and crash tests apply "For metered/changing work".

Built-ins the journey needs (CORE §12.3 L238; HB L51-57, L208-216): `atlas whoami`, `atlas search "…"`, `atlas guide <id>`, `describe`, `catalog`, and `call`. The operation helper is needed where durable work occurs. `atlas access` is AMBIGUOUS (see C3.11).

---

## 5. The three-facts rule, the relevance rule and the context rule, as tests

Source: HB "Establish the context once" L29-45. The handbook labels the context and relevance rules "Proposed" (L33, L35). They are adopted through P2 step 1 (L29: "the identity and scope model (ATLAS-CORE §3, R05, R26)… Agreed as written in the review"). Exception: the org/tenant switching contract is "Left to owners" (P2 L98). Backing in CORE §3 L42-56.

### 5.1 Context rule (HB L31-33)

The rule text: "one configured organization/tenant and environment are the default for a connection. The gateway derives the authenticated tenant and principal from trusted authentication, verifies the selected organization, and returns the effective context in `whoami`. A request payload cannot impersonate a principal or choose an arbitrary tenant. Switching organization, account or environment is explicit. Caches and saved connection state are isolated by that effective context."

| Test | Expectation |
|---|---|
| X1 | With one configured context, CLI, Python and Node `whoami` return the same effective principal, tenant and environment (HB L31; CORE L50) |
| X2 | A request body containing a principal or tenant member does not change the effective principal or tenant. It is refused as an unknown field, by R04 |
| X3 | Selecting an org the authenticated tenant does not verify is refused |
| X4 | With several candidate contexts configured, the client refuses to proceed without a deliberate selection (CORE L50) |
| X5 | Caches (profiles, results, saved connection state) under context A are not read under context B |
| X6 | A workload identity running the same request does not receive the person's grants: "Moving a request from the CLI into a pipeline does not transfer the person's grants to the workload" (HB L31; CORE L48) |
| X7 | No `is_human` request member affects policy (CORE L46) |

DEFERRED / AMBIGUOUS: the full org/tenant switching contract. HB L33: "they do not yet define a complete organization/tenant switching contract. That omission needs a decision before company-wide rollout". P2 L98 leaves it to owners. HB L33 also says the current client rules "default to Production". AMBIGUOUS whether the first build keeps that default.

### 5.2 Relevance rule (HB L35; CORE L42)

The rule text: "'available to this caller' is the permission boundary; 'recommended for this task' is a ranking within that boundary. A role can inform recommendations, but it does not identify a person's current portfolio or silently narrow the result. If Atlas applies a default portfolio, date, currency, source or scope, the guide describes it and the answer reports the effective value. An explicit request overrides a default only within authorization."

| Test | Expectation |
|---|---|
| V1 | `search` never returns a capability the caller cannot invoke, however relevant (HB L57) |
| V2 | A caller with a portfolio-related role but scope {A} requesting portfolio B is refused or receives no B rows (R05). The role does not grant B (CORE L42) |
| V3 | A role does not silently narrow a result: with no default declared, no portfolio filter is inferred from the role |
| V4 | When a default is applied (for example null `as_of` resolved to a date), the guide documents it and the result reports the effective value (CORE L44) |
| V5 | An explicit request value overrides the default only within authorization; an explicit out-of-scope value is refused, never silently replaced by the default |
| V6 | A retry with the same idempotency key keeps the original effective defaults (CORE §9 L200) |
| V7 | Guides and recommendations never mention a capability the caller cannot use (HB L57; CORE L54) |

### 5.3 Three-facts rule (HB L37-45)

"Keep these three facts separate when presenting a capability". The table, verbatim:

| Fact | What it proves | What it does not prove |
|---|---|---|
| The caller holds an Atlas interaction | Atlas permits an attempt in this environment | The upstream provider accepts the identity, the licence covers an instrument, or a requested row exists |
| The source and service are currently available | A current health check is satisfactory | The next request cannot fail, the date has landed, or the caller has the relevant data entitlement |
| This client supports an interaction's binding | This client can speak the required protocol | The caller may invoke it or its result fits in memory |

| Test | Expectation |
|---|---|
| F1 | Grant held, source refuses or row missing: `call` returns a specific problem, distinct from a grant refusal. Catalogue presence is not reported as success |
| F2 | `data.status` reports the source healthy, but the requested date has not landed: holdings returns the "on or before" date and discloses it, or a specific problem. Health is never presented as entitlement or landing |
| F3 | A client that supports the binding but whose caller lacks the grant is refused at call time |
| F4 | describe and guide present grant, availability and client support as separate items, never merged into one "available" flag (consistent with R01: availability private and uncached) |
| F5 | "Recording a caller in an audit does not itself provide row-level restriction" (HB L45). The R05 scope rule is tested on its own |

---

## 6. What P2 (REVIEW.md) did not adopt, or adopted differently

| # | Review says | P2 | Effect on the build |
|---|---|---|---|
| D1 | R01: "Reconcile the catalogue's one-hour client cache with the HTTP `no-store` rule" (DR L36) | Not mentioned (P2 L59) | AMBIGUOUS. Decide and record before the step 2 client caching work |
| D2 | R04: validate "the transformed request afterward" too (DR L62) | Only "before any transformation" (P2 L62) | Build both validations |
| D3 | R05: "Do not fall back silently from failed user delegation to a service identity" (DR L72) | Omitted (P2 L63) | Still a review rule. Delegation is step 5, but the no-silent-fallback test belongs wherever a service identity is used |
| D4 | R06: bind fields include "action" (DR), and "limits" and "relevant policy" (CORE L196). Also "Define what policy tightening does to outstanding plans"; resolve the duplicate placement of approval | P2 lists principal, address, normalised request, plan digest, scope, maximum, expiry | Bind the union. Policy-tightening behavior is undecided |
| D5 | R07: "co-locate its atomic reservation set **or allocate bounded sub-budgets**" (DR L88) | Atomic reservation set only | The sub-budget alternative is dropped |
| D6 | R08: "Include metered reads when a retry can duplicate a charge. Do not promise exactly-once external effects solely from HTTP deduplication." (DR L96) | Omitted from the row (P2 L66) | Still applies, via CORE §9 L198 and L202 |
| D7 | R09: "Specify the acknowledgement point and recovery when an effect succeeds but recording completion fails" (DR L104) | Omitted | AMBIGUOUS. The acknowledgement point must be chosen in step 3 |
| D8 | R12 acceptance: decode independently "in Go and TypeScript" (DR L134) | "Done" (P2 L70) | Go half done. The TS half is outstanding with the Node client (step 4) |
| D9 | R14: full-representation ETag "initially" (DR L141; CORE L228 "start with") | "Resources (after the journey)" (P2 L72) | Deferred. See the AMBIGUOUS note on `atlas.operation` |
| D10 | R24: "Keep tunable operational defaults separate from normative protocol semantics" (DR L151) | Only the layer rule (P2 L82) | Still applies |
| D11 | R25: two options, "under the resource's watch contract, or catalogue them independently" (DR L152) | P2 picks the watch contract (P2 L83) | P2 decides. DEFERRED anyway |
| D12 | INT acceptance #6: "checked independently against the source" (INT L111) | "against the source fixture" (P2 L51) | Narrowed, because the Development source is step 5 |
| D13 | CORE §12.5 tests "stale metadata", "denied" calls, budget contention and crash-after-commit (L240) | P2's acceptance list (L46-51) omits "stale metadata" and "denied" | Covered elsewhere: stale metadata by the R01/R02 acceptance tests; budget contention by R07; crash-after-commit by R08 (step 3); denied by acceptance #3. Include all of them |
| D14 | CORE §12 order (integrate first, L236) | P2 uses DR's "practical decision order" (L25, L27-33) | Integration is step 4, not step 1 |
| D15 | Shared gateway mechanics and SQL planning must remain behind Atlas-owned interfaces | Local modules `sdk/go/kit` and `sqlglot` provide those boundaries; preserve their tests and applicable notices | The module layout and integration boundaries are repository-local |
| D16 | DR R20 "remote transport/authentication conformance on first extraction" | P2 L78 adopts it. AGENTS.md L66 still says "Pass the family's battery, in process and over loopback" | P2 governs (P2 L5 governs PLAN.md). AGENTS.md is not amended. AMBIGUOUS whether loopback tests are still wanted |
| D17 | CORE §6 L140: retain the Go engine "while measured"; the subprocess is an implementation choice | P2 L19, L92: the engine, the Python face and the hermetic Snowflake fake come back from `pass-1` onto the new protocol | Consistent |
| D18 | Review leaves open: holdings questions; org/tenant switching; catalogue overlaps (HB L531-548) | P2 L94-100 "Left to owners", adding "The minimum sunset and the lock's format, which the spec still has open" | The fixture states each choice |
| D19 | P2 "These carry over unchanged": building without Docker, DuckDB and sqlglot faking Snowflake, fakes behind real adapters, references as the independent check (D22), committing only when asked (P2 L21) | Not from the review; carried from PLAN.md | Binding |
| D20 | Phase 0 layout/checks and Phase 1 protocol/reference work are retained | Historical implementation findings in this digest may have later fixes | Consult the current code and handoff progress before extending them |

### Spec rules the review overrides, per P2 (quick list)

| Spec rule | Overridden by |
|---|---|
| §9.4 fingerprint as strong ETag; §9.6 L95 immutable describe; §9.7 L102 profiles cached forever | R01 |
| §9.7 L88 binding taken from the locked profile; D39 literal | R02 |
| §3.3 rule 1 full transitive compatibility; lens `convert_unit`/`promote`/`drop` within a version | R03; P2 L90 |
| §9.5 one lens list, top-level fields | R04 |
| §6.2.5 resource conditions "(later)" | R05 |
| `Atlas-Confirm` as consent | R06 |
| §6.1.5 one transaction across all counters | R07 |
| §6.1 L250 key released on takeover | R08 |
| §6.2.8 one immutable audit record | R09 |
| AP-CLIENT-005 Arrow for every table; token file in every client | R11 |
| §6.1 degraded mode 60 min as a single bound | R13 |
| §9.3 resource ETag ignores `status` | R14 (deferred) |
| §4.1 disconnect cancels | R17 |
| §5 result cache key | R18 |
| §6.1 remote loopback at admission; six deployables and an HTTP/2 service protocol | R20; P2 L13 |
| §6.5 L0-L6 layering | R24 |
| §9.7 token file keyed by environment | R26 |
| PLAN: Node out of scope | P2 L12 (R11) |
| PLAN: handoff, events and Service Bus in the first build | P2 L15 (R10, R16) |
