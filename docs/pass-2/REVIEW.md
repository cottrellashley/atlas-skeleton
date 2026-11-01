# Pass 2 follows the design review

**Source:** the 2026-10-31 design review, [`docs/source/atlas-spec/review/`](../source/atlas-spec/review). It was read on 2026-10-31 in the order its README sets: [ATLAS-CORE](../source/atlas-spec/review/ATLAS-CORE.md), CAPABILITY-HANDBOOK, DESIGN-REVIEW, INTEGRATIONS. the product owner asked that the build follow it. The review keeps Atlas's product promise and reduces its universal guarantees, so the first build is one complete, narrow journey rather than a fraction of every subsystem.

The review does not change requirements by implication. Where pass 2 now departs from the spec or from `PLAN.md`, this file records the change and the review finding (R01–R26) behind it. Where the two disagree, this file governs `PLAN.md`.

## What changes

| Before (`PLAN.md`) | Now (the review) | Why |
|---|---|---|
| Every §9 mechanism, 16 exemplar patterns, 12 phases | One complete journey first: holdings, its identifier lookup and its freshness. Then a capability added after the clients are built | ATLAS-CORE §12; INTEGRATIONS, "the first end-to-end acceptance result" |
| Node out of scope | CLI, Python and Node from the first release, one request record in all three | ATLAS-CORE §6, §12.4; R11 |
| Six deployables and an HTTP/2 service protocol | One Go deployable, plus a worker where durable work needs one. The provider boundary stays in process and serialisable | ATLAS-CORE §8; R20 |
| Full lens algebra, transitive compatibility | Conservative evolution: aliases, defaults, additive optional fields. A unit or type change, or a removal, is a new major version. The resolver stays as an experiment | ATLAS-CORE §7; R03, R04 |
| Handoff, events and Service Bus in the first build | Deferred, as separately gated proofs on real systems | ATLAS-CORE §10, §12.6; R10, R16 |
| Exact budgets per caller × capability | A declared budget authority per metered provider. Unsupported combinations of hard budgets are refused. Protective rate limits are kept apart | ATLAS-CORE §11; R07 |
| One immutable final audit record | An invocation history of immutable admission, outcome and settlement facts | ATLAS-CORE §11; R09 |
| `Atlas-Confirm` as consent | A confirmation or approval receipt bound to the principal, address, normalised request, plan digest, scope, maximum and expiry. The header is the client's claim only | ATLAS-CORE §9; R06 |
| Pass 1's gateway, clients and engine removed in Phase 0 | Kept as an asset. In step 4 the engine and the Python face come back from the first implementation onto the new protocol rather than being rewritten | DESIGN-REVIEW, "keep the existing Go/Python proof"; ATLAS-CORE §6 |

These carry over unchanged: building without Docker, DuckDB and sqlglot faking Snowflake, fakes behind real adapters, the references as the independent check (D22), and committing only when asked.

## The order of work

The review's "practical decision order" is now pass 2's.

| Step | What | Done when |
|---|---|---|
| 1 | **Agree** the capability card (ATLAS-CORE §4, the handbook's page contract), the call and result lifecycle (ATLAS-CORE §6, R11) and the identity and scope model (ATLAS-CORE §3, R05, R26) | Agreed as written in the review, by the recorded design decision |
| 2 | **Revisions and compatibility:** split the contract fingerprint, the document revision and the routing (R01, R02); apply the smaller compatibility policy in the gate (R03, R04); write the first complete executable profiles and the shared fixtures | The four journey profiles pass the gate and the readiness check. Fixtures cover two identities with different portfolio scope |
| 3 | **Before metered or changing work:** idempotency scope and uncertain outcomes (R08), approval evidence (R06), budget transaction boundaries (R07), the audit lifecycle (R09) | Each R's acceptance test passes against the fakes |
| 4 | **Integrate and prove:** the cloud kit with provenance, sqlglot behind the Atlas compiler and guard, and the journey through the CLI, Python and Node | The acceptance result below |
| 5 | **Real systems:** the Development source, direct downloads, events, delegated identity | Out of reach from this machine. The plug-in points and gated proofs are ready, and only the guarantees a proof establishes are promoted |

### The journey

The journey's capabilities come from the catalogue:
- `portfolio.holdings@v1`;
- `portfolio.list@v1` and `portfolio.search@v1`, its identifier lookup;
- `data.status@v1`, its freshness.

The capability added after the clients are built is `portfolio.holdings-dates@v1`. Their profiles are written to the handbook's page contract. Where an owner must decide something, such as the holdings questions the handbook lists, the profile says so and the fixture fixes a stated choice.

### The acceptance result (INTEGRATIONS)

1. The shared gateway kit and SQL library take part in the running path, with their integration tests and applicable notices.
2. The same person discovers and calls the same capability from the CLI, Python and Node with identical request records.
3. A second person with narrower data scope receives a correspondingly narrower result, or a clear refusal.
4. A capability added after the clients were built works without releasing any of them.
5. Invalid requests, permission changes, exact decimals, incomplete streams, idempotent retries and source failures behave the same in every client.
6. The guide's example succeeds, and its result can be checked independently against the source fixture.

## The amendments, one per finding

Each row is a decision the review proposes. Each acceptance test becomes a named test in the step that builds the decision.

| R | The spec says | Pass 2 now | Step |
|---|---|---|---|
| R01 | The fingerprint is a profile's strong ETag; profiles are cached forever; describe can be immutable | The contract (by fingerprint) is cached apart from the document revision. Describe's caller state (governance, availability) is private and uncached. A guide's identity includes the prose revision | 2 |
| R02 | A lock's profile fixes the bindings | A lock pins semantic contracts. Routing is fetched as current metadata. "No discovery at runtime" means no new business-route selection | 2 |
| R03 | Full transitive compatibility within a version, by lenses | Within a version: optional request fields; result fields only with a defined missing value; aliases for exact renames. A removal, a unit change, a type change, a field made non-null in a result, or a changed default is a new major version | 2 (gate) |
| R04 | One lens per profile acts on top-level fields | Aliases carry their schema scope. A request is validated under its own contract before any transformation, and unknown request fields are refused, never dropped | 2 |
| R05 | Resource conditions are deferred | Resource and returned-data scope is enforced from the first shared-credential source, by small typed rules. Relevance never grants access | 2, 4 |
| R06 | `Atlas-Confirm` and the caller's token are consent | A receipt from a trusted interaction, bound to the principal, address, normalised request, plan digest, scope, maximum and expiry | 3 |
| R07 | All counters reserve in one transaction | One budget authority per metered provider and an atomic reservation set. Unsupported combinations are refused | 3 |
| R08 | Takeover releases the idempotency key | One logical operation per key for its whole retention, uncertain outcomes included. Workers are fenced after their lease and reconciled | 3 |
| R09 | One immutable final audit record | Immutable admission, outcome and settlement facts, joined for reading | 3 |
| R10 | Direct links are usable only by their caller | Each handover records its real target, rights, identity binding, expiry, renewal and revocation. Client SQL tickets stay deferred | 5 |
| R11 | Every table lands as Arrow; a token file in every client | `call` executes once and returns a result or operation handle. Core is JSON, and Arrow is an optimisation of the same meaning. Credentials come from each runtime's own provider | 4 |
| R12 | Schemas and vectors to be written | Done in Phase 1 for the journey's protocol: the 21 schemas and every vector pass | Done |
| R13 | Degraded mode keeps policy for 60 minutes | Separate freshness bounds for policy, groups, connections and links. A switch behind a failed control channel promises no immediate revocation | 3 |
| R14 | A resource's ETag does not move when only its status changes | The ETag covers the full representation; `generation` is separate | Resources (after the journey) |
| R15 | The change feed is the event outbox | Explicit immutable outbox facts where every event matters | 5 |
| R16 | Polling is a universal event fallback | Equivalence is claimed only with cursor-addressable retained history | 5 |
| R17 | A disconnect cancels | The start deadline, execution deadline and retention are separate. Cancellation is reported as requested or confirmed | 3 |
| R18 | The result cache is keyed by address, request and fingerprint | Results are private to their caller; any reuse is keyed by authorisation, environment and data revision, and reauthorised | 4 |
| R19 | EXPLAIN bytes choose the transport | Heuristics are labelled, and output is measured | 5 |
| R20 | Remote loopback is required at admission | In-process provider conformance first; remote transport conformance when a provider is first extracted | 4 |
| R21 | Any source change breaks meaning | Business lineage and method are distinguished from the execution engine | 2 |
| R22 | An exploratory agent's calls repeat exactly | Answers, scope, recovery and cost are tested; the built artifact replays deterministically | 4 |
| R23 | Actions both catalogued and not | Catalogue summaries and counts are generated; examples, registered profiles and deployed availability are kept distinct | 2 |
| R24 | Six layers numbered L0–L6 | `layers.toml` is the rule. The import check stays, and same-layer imports are allowed | Done (`tools/layercheck`) |
| R25 | Every event type is a capability | One registration model; derived change events live under the resource's watch contract | 5 |
| R26 | The token file is keyed by environment | Credentials are bound to the trusted gateway, authority, audience, tenant or account, and scopes. Refresh is serialised, and an unauthenticated bootstrap cannot redirect a credential | 4 |

## What happens to what is built

- **Phase 0** stands: the layout, `make check`, the analyzers, the layer and requirement tools.
- **Phase 1** stands, because R12 asks for exactly what it supplies: executable schemas and unambiguous encodings, proved against independent vectors. Two parts change role:
  - **`protocol/resolve`** becomes the experimental resolver ATLAS-CORE §7 allows. A client is obliged only to aliases, defaults, UNKNOWN and projection. Its vectors still pass, but no served profile may depend on `convert_unit`, `promote` or `drop` within a version.
  - **`protocol/gate`** takes the smaller compatibility model of R03.
- **Pass 1's proof** (historical first implementation) is an asset. Step 4 brings its engine, the Python face and the hermetic Snowflake fake back onto the new protocol.

## Left to owners

The review does not decide these, and the build does not either. Each fixture states the choice it fixes:
- The holdings questions the handbook lists: null `as_of`, cash and instruments without an ISIN, aggregation, currency and signs, whether weights include cash, and partial authorisation.
- The organisation and tenant switching contract (handbook, "establish the context once").
- The minimum sunset and the lock's format, which the spec still has open.
- The catalogue overlaps the handbook names (holdings and exposure, deals and new issues, and others).
