# Digest: the rest of the spec (overview, vision, §2.2.2–§2.5, §8, §9.2, transcription)

Reader: "rest of the spec". Date: 2026-10-31.

Spec root: `~/Documents/ChatGPT/creation/atlas-spec/` (abbreviated `spec/` below; NOT the repo's `spec/` dir).
Review root: `spec/review/`. Repo: `~/repos/atlas-skeleton`.
Citations: `file:line` (line numbers as `cat -n` prints them) plus § where the file has one.

Files read in full for this digest: `README.md`, `00-overview.md`, `00-vision.md`, `02-2-2-vendor-api-data.md`, `02-3-resources.md`, `02-4-actions.md`, `02-5-events.md`, `08-design-decisions.md`, `09-2-schemas.md`, `TRANSCRIPTION-NOTES.md`, `VALIDATION.md`, `VERIFICATION-LEDGER.md`. To find overrides I also read in full: repo `AGENTS.md`, repo `docs/pass-2/REVIEW.md`, `review/README.md`, `review/ATLAS-CORE.md`, `review/DESIGN-REVIEW.md`, `review/INTEGRATIONS.md`. I read parts of: the repo's `spec/protocol/references/QUESTIONS.md` (lines 1–53, 131–157), `sdk/go/protocol/schema/*.go` and `arrowschema/*.go` (exported API only), `07-the-catalogue.md` (grep hits only) and `review/CAPABILITY-HANDBOOK.md` (lines 225–245, 355–365, plus grep hits).

Markers used:
- **IN SCOPE**: the first build (the journey) must implement it.
- **DEFERRED**: the first build leaves room for it but does not build it.
- **OVERRIDDEN BY REVIEW (Rnn)**: REVIEW.md or the review changes what the spec says.
- **AMBIGUOUS**: the text allows more than one reading; each reading is listed.

The journey (REVIEW.md:37–42): `portfolio.holdings@v1`; `portfolio.list@v1` and `portfolio.search@v1` (identifier lookup); `data.status@v1` (freshness); then `portfolio.holdings-dates@v1`, added after the clients are built. All five are `data` family (07-the-catalogue.md:361, 634, 635, 639, 640).

---

## 1. Promises in the overview and vision that the first journey must keep

### 1.1 The overview's "In short" (00-overview.md:11–20)

| # | Promise (quoted or closely quoted) | Cite | First build |
|---|---|---|---|
| 1 | "One host: the Atlas Gateway. Every client, in every language, calls the Atlas Gateway over HTTPS, for every capability" | 00-overview.md:11 | IN SCOPE. One Go gateway deployable, plus a worker where durable work needs one (REVIEW.md:13; ATLAS-CORE.md:186) |
| 2 | "Every capability describes itself in a profile the gateway serves and evolves; the gateway guarantees that what it sends matches it, even when it does not carry the bytes." | :12 | IN SCOPE for streamed results. "Does not carry the bytes" (handoff) is DEFERRED (ATLAS-CORE.md:241; DESIGN-REVIEW.md:176) |
| 3 | "A client contains no code for any capability: it reads profiles, resolves what arrives to the contract it knows... A contract change never needs a client release." | :13 | IN SCOPE. Acceptance #4 (REVIEW.md:49). OVERRIDDEN BY REVIEW (R03): "resolves" now means only aliases, defaults, UNKNOWN and projection (REVIEW.md:90) |
| 4 | Each client is built for its users: "Python for researchers and data engineers, Node for analysts' dashboards in Nautilus, the CLI for Nautilus and agents, Go for Go programs." Extras are "never a capability" | :14 | IN SCOPE for CLI, Python and Node (REVIEW.md:12; R11). The Go SDK is what the CLI is built on (D20). Extras are DEFERRED |
| 5 | "Four surfaces. Data..., resources..., actions..., events..., plus the built-ins (`atlas.*`)." | :15 | IN SCOPE: data and the built-ins the journey needs. Resources, actions and events are DEFERRED (REVIEW.md:15, 72) |
| 6 | "`portfolio.holdings@v1:fetch` is what people, agents and access rules see; where it is served is a binding the client follows." | :16 | IN SCOPE. OVERRIDDEN BY REVIEW (R02): routing is fetched as current metadata, not fixed by a lock (REVIEW.md:60) |
| 7 | "Everything a caller sees while working is what they may use, here, now; only `atlas.access` shows the rest, with reasons." | :17 | IN SCOPE (D13; acceptance #3, REVIEW.md:48) |
| 8 | "the gateway runs the work, hands the bytes off, or brokers a connection the client makes, as the person wherever the system accepts it (§6.3)" | :18 | "Run" is IN SCOPE. Hand-off and broker are DEFERRED (D42; R10). Acting "as the person" is DEFERRED (REVIEW.md:33, step 5 "delegated identity") |
| 9 | "Every call is decided once (authenticate, resolve, authorise, validate, limit), routed to a service that can be scaled and moved on its own, settled and audited." | :19 | IN SCOPE: the order and decide-once. OVERRIDDEN BY REVIEW (R20): one deployable with an in-process provider boundary that can be serialised (REVIEW.md:13, 78). OVERRIDDEN BY REVIEW (R09): audit is admission, outcome and settlement facts (REVIEW.md:67) |
| 10 | "One protocol for every language, strict enough to write a client from its text, schemas and test vectors, with a conformance suite that decides." | :20 | IN SCOPE. R12 is done (REVIEW.md:70) |

### 1.2 The traced call: holdings (00-overview.md:58–67)

| Step | Spec text | Cite | First build |
|---|---|---|---|
| Find | `atlas.search("portfolio holdings")` "returns only what this person may use here (§2.6)" | :60 | IN SCOPE |
| Understand | `atlas.describe` returns "`portfolio.holdings@v1#bcaa5bbad97cc612`: its request and result schemas, its executions (`gateway`, `handoff`), the person's limits (§3.1)" | :61 | IN SCOPE. OVERRIDDEN BY REVIEW (R01): describe's caller state (governance, availability) is private and never cached (REVIEW.md:59). Execution `handoff` is DEFERRED |
| Call | "The Python SDK validates `{"portfolios": ["147HYC"], "columns": [...]}` and sends it with `Atlas-Profile-Expect`, `Atlas-Deadline` and its features: it can take a handoff (§4)." | :62 | IN SCOPE: client-side validation, the `Atlas-Profile-Expect` and `Atlas-Deadline` headers, and the features header. The handoff feature is DEFERRED. The exact header grammar is in §9.6 (another reader's) |
| Decide | "validates the token, resolves the address, finds a grant, checks the request, reserves the limit (§6.2)" | :63 | IN SCOPE. See D17 below for the order the review sets |
| Plan and run | "plans the writer contract as SQL (every column aliased and cast), the guard passes it, and Snowflake runs it (§2.2.1)" | :64 | IN SCOPE, with DuckDB plus sqlglot-go standing in for Snowflake (REVIEW.md:21; D27; D28) |
| Hand off | "The answer is a handoff document: the result's chunk links and the writer profile." | :65 | DEFERRED. The journey streams (ATLAS-CORE.md:206, 241) |
| Read | "decodes each chunk into the writer's Arrow schema, checks it, resolves it to the profile its notebook knows (§3.4), and returns a `pandas.DataFrame`" | :66 | IN SCOPE as "resolve to the reader profile". OVERRIDDEN BY REVIEW (R11): Core is JSON, and Arrow is an optimisation with the same meaning. Large tables are iterators or batch readers by default, and materialising a DataFrame is the caller's explicit choice (ATLAS-CORE.md:138) |
| Settle | "settles the call from Snowflake's own record and audits it" | :67 | IN SCOPE as settlement facts (R09) |

### 1.3 The overview's rules (00-overview.md:83–93). All IN SCOPE.

| Id | Text (exact) | Cite | Notes for this build |
|---|---|---|---|
| ATLAS-CORE-001 | "Every capability has a profile, served by the gateway, from which a conforming client can discover, call and read it without code written for that capability." | :83 | Test with the five journey profiles |
| ATLAS-CORE-002 | "A change to a capability's contract that versioning classifies as safe or dangerous requires no client release." | :84 | OVERRIDDEN BY REVIEW (R03): the gate's smaller model decides what counts as safe (REVIEW.md:61, 91) |
| ATLAS-CORE-003 | "Every call, through every edge, passes the same pipeline and resolves to exactly one `capability@version:interaction`." | :85 | |
| ATLAS-CORE-004 | "The Atlas Gateway passes the gateway conformance suite; every client passes the client conformance suite at the levels it declares." | :86 | The levels are in D24 |
| ATLAS-CORE-005 | "The same request yields identical records through every execution, binding and encoding a capability offers." | :87 | Applies across JSON, NDJSON and (where offered) Arrow. Also AP-SCHEMA-003 |
| ATLAS-CORE-006 | "A deny always wins; what a caller cannot use never appears in their working view." | :88 | |
| ATLAS-CORE-007 | "Identity comes only from validated tokens; credentials on the caller's machine never leave it." | :89 | Plus R26: credentials are bound to the trusted gateway, authority, audience, tenant and scopes (REVIEW.md:84) |
| ATLAS-CORE-008 | "Every capability is served by the Atlas Gateway, and only by it; a client extra is never a capability and never calls the gateway except through the client's ordinary calls." | :90 | |
| ATLAS-CORE-009 | "Deploying a gateway with a new capability needs no change anywhere else: no client release, and no change to any agent, skill, system prompt, MCP tool list or harness configuration. Every client in use finds, understands and calls it, and an agent completes a task with it, the moment it is served." | :91 | The `portfolio.holdings-dates@v1` step proves it (REVIEW.md:42, 49) |
| "Must never" | "Atlas must never let a client call what its context does not allow, persist a vendor-licensed row except a job's answer for its own caller, let a client edit the computation behind a capability, or need a client release for a contract change." | :93 | No vendor data in the journey. Result reuse is private to the caller (R18, REVIEW.md:76) |

### 1.4 The overview's seams (00-overview.md:71–79)

The journey crosses these seams:
- "Caller and client" (§5, §9.7).
- "Client and gateway" (the Atlas Protocol, §9).
- "Front door and services": "The service protocol, a signed token" (§6.1). OVERRIDDEN BY REVIEW (R20): it runs in process now.
- "Services and engines": "The `DataSource` port" (§6.4).
- "Profile and code": "The gate and registration" (§3.3).

"The gateway and each system: ... The register of patterns and identities" (§6.3) applies to Snowflake/DuckDB only.

### 1.5 The overview's decisions summary (00-overview.md:99–104)

- "Avro's model (writer and reader profiles, resolution), not a GraphQL server". OVERRIDDEN BY REVIEW (R03, R04): `protocol/resolve` becomes an experiment, and "no served profile may depend on `convert_unit`, `promote` or `drop` within a version" (REVIEW.md:90).
- "JSON Schema 2020-12 with an Atlas vocabulary; Arrow, NDJSON and Parquet as encodings derived from it." Phase 1 has done this.
- "Results streamed by the gateway or handed off first, export next; the broker only if needed." Streaming is IN SCOPE. Handoff is DEFERRED. Export: see the AMBIGUOUS note under D11.
- "A front door over movable services; engines behind one `DataSource` port; one Go module in six layers." R20 and R24 override this: `layers.toml` is the rule, and same-layer imports are allowed (REVIEW.md:82).
- "One host...; clients optimised per language and role, with extras that never need the gateway."
- "One normative, language-neutral protocol; no implementation is the reference."

Repository layout the spec names (00-overview.md:7): "Built in `jhgonline/fi-app-atlas`: the Atlas Gateway in `gateway/`, the clients in `clients/python/`, `clients/node/` and `clients/cli/`, and the Go SDK in `sdk/go/`." The repo has `gateway/`, `clients/` and `sdk/go/`. The Go module paths are `github.com/jhgonline/fi-app-atlas/...` (sdk/go/protocol/schema/schema.go imports).

### 1.6 The vision's promises (00-vision.md)

| Promise | Exact text / content | Cite | First build |
|---|---|---|---|
| An assistant's experience | "a one-line map of what the person may use, the manual only when needed, house rules for which source is the truth for what, one route per question, and errors that name the fix" | 00-vision.md:9 | IN SCOPE as the working view, guide, problems with next steps, and house rules (D38) |
| Find, understand, call, combine | "let an agent **find, understand, call and combine** anything ... through the CLI and the SDKs, without help" | :11 | IN SCOPE |
| Explore with the CLI, build on an SDK | "It explores with the CLI, then builds on the SDK that fits..., all one shape; what it builds replays the same route every time, with no agent in the loop." | :13 | IN SCOPE for CLI, then Python and Node |
| PM trace commands | `atlas search "portfolio holdings benchmark duration" --json`; `atlas guide`; `atlas explain`; "`read`: safe to run without asking"; "both are keyed by ISIN and date"; "the SDK returning exact decimals"; a failure "with `not_granted` ... the problem names the role to ask for" | :38–43 | IN SCOPE: `search --json`, `guide`, `explain`, `not_granted` with a next step, exact decimals. The events part (subscribing to `data.landed@v1`, :42) is DEFERRED |
| Never seen by the caller | "The PM never saw a table name, a URL, a credential or a format. Every call was decided, limited and audited." | :45 | IN SCOPE |
| Rule 1 | "**Everything an agent needs is in Atlas.** A capability is finished only when an agent can find it, understand it and call it from its profile alone: a summary, every parameter and column with its meaning and unit, examples, the effect of each interaction, and the problems it can return with their next steps." | :58 | IN SCOPE. The journey profiles must pass the readiness check (REVIEW.md:30) |
| Rule 2 | "**One shape everywhere.** The same names, parameters, columns, problems and operations in the CLI, Python, Node and Go" | :59 | IN SCOPE (acceptance #2 and #5, REVIEW.md:47, 50) |
| Rule 3 | "**Only what the caller can use appears while working,**" | :60 | IN SCOPE |
| Rule 4 | "**Every failure says what to do next.**" | :61 | IN SCOPE |
| Rule 5 | "**No role is special-cased.** What a role can build follows from its grants, never from code written for it." | :62 | IN SCOPE. The two identities in the fixtures differ only by grants and scope (REVIEW.md:30) |
| Rule 6 | "**One route per question.** No two capabilities answer the same question, and the house rules say which source is the truth for what (§7, §2.6)." | :63 | IN SCOPE. `portfolio.list` and `portfolio.search` are distinct tasks (CAPABILITY-HANDBOOK.md:540) |
| Rule 7 | "...replays it from its lock: the same addresses, the same profiles, no discovery at run time (§9.7.9)." | :64 | OVERRIDDEN BY REVIEW (R02): "No discovery at runtime" means "no new business-route selection", and routing is current metadata (REVIEW.md:60; ATLAS-CORE.md:164) |
| Rule 8 | "MCP and the CLI for assistants; the SDKs for programs; a client in a new language ... accepted by the conformance suite." | :65 | The CLI is IN SCOPE. MCP: see D14 |
| Rule 9 | "Access and limits per person on every capability, every call on the person's behalf, a confirmation before a costly call (§6.2.6), every call audited." | :66 | IN SCOPE. The confirmation is OVERRIDDEN BY REVIEW (R06), see D40 |
| ATLAS-VISION-001 | "Every role journey (below) is completed by an agent using only the CLI and SDKs, in every release." | :68 | DEFERRED as a release gate, since the journeys are open (O56). The build's own journey is the acceptance result |
| ATLAS-VISION-002 | "Every capability's profile passes the agent-readiness check: a summary, a meaning and unit for every field, at least one example, and a next step for every problem it declares." | :69 | IN SCOPE (REVIEW.md:30, "pass the gate and the readiness check") |
| ATLAS-VISION-003 | "A role journey run twice calls the same capabilities, in the same order." | :70 | OVERRIDDEN BY REVIEW (R22): test answers, scope, recovery and cost; "the built artifact replays deterministically" (REVIEW.md:80; ATLAS-CORE.md:243) |
| ATLAS-VISION-004 | "An artefact an agent built ... pins its addresses and profiles in a lock, and makes no discovery call when it runs." | :71 | OVERRIDDEN BY REVIEW (R02). The lock's format is left to owners (REVIEW.md:99; O12) |
| Journey measures | "Right answer the first time: Every journey"; "Attempts: ... None"; "Time to answer: Recorded"; "Cost: tokens and metered use: Recorded, with a budget for the working view's tokens (O59)"; "The same route on a second run: Every journey (ATLAS-VISION-003)" | :118–124 | The last row is overridden by R22 |
| Provider families | Data provider implements "`plan` (with the writer contract in the statement), a stream of Arrow under the writer schema or a handoff, `settle`"; proven by "Batches match the profile; every execution returns the same rows". Every family: "`describe`, `health`, verifying the front door's signed token, problems from the registry" | :162–168 | The provider interface should leave room for plan, stream, settle, describe and health. In-process now (R20). Verifying the signed token is DEFERRED with the service protocol (O21, O22) |
| What it needs | provider protocol per family; provider conformance runner; registry of domains; profile check as a tool; delegated administration; trust and network model; general spec separate from QFI's | :172–178 | DEFERRED, except the profile check (gate plus lint), which is IN SCOPE (AGENTS.md:62) |
| Today | "Every service is the gateway's own Python code. Registration is designed (§6.1.6) but not built, and the service protocol's transport and signed token are open." | :180 | Historical |
| Central vs owned | Central: sign-in and identity; discovery (catalogue, search, describe); access (grants, denies, conditions); limits, budgets and approvals; audit and telemetry; versioning checks at registration; routing. Owned: meaning and names; profiles; service; credentials to own sources; granting own domain; releases | :148–156 | The central column is IN SCOPE as front-door steps |

---

## 2. Every design decision in §8.1 (08-design-decisions.md:22–75)

There are 52 rows, D1–D52. D21 is written last, at :75. The count matches "52 decisions" (:9) and the ledger (VERIFICATION-LEDGER.md:31).

| D | Decision (key text, exact where it matters) | Line | What it constrains in this build |
|---|---|---|---|
| D1 | "One host, the Atlas Gateway, serves every capability to every client; four clients (Python, Node, the CLI, Go)" | :24 | IN SCOPE: one gateway, plus CLI, Python and Node (REVIEW.md:12). The Go SDK underlies the CLI and the engine |
| D2 | Work on the caller's machine is a "**client extra**, never a capability: in its client's own namespace, never calling the gateway except through the client's calls, never sending the caller's credentials to it" | :25 | DEFERRED (no extras). Constraint: nothing local is put in the catalogue |
| D3 | "The gateway owns every contract; clients interpret profiles; a client is released only for a protocol change." | :26 | IN SCOPE (acceptance #4) |
| D4 | "A capability is a concept (`domain.name@vN`) with interactions (`:fetch`, `:run`, ...), each reached through bindings; names never dictate endpoints." | :27 | IN SCOPE. AGENTS.md:10: "every interaction is a POST to its binding" |
| D5 | "Four surfaces (a profile's `family`): data, resources, actions, events." | :28 | The journey is `data` only. O38: "“surface” in prose, `family` on the wire" (:149) |
| D6 | "Only built-ins are `atlas.*`; what the team owns is named for its subject, generically (never `harbour.*`); what it does not own is named for its provider or platform; where a capability runs is never in a name." | :29 | IN SCOPE: `portfolio.*`, `data.*` |
| D7 | "Avro's model: writer and reader profiles, fingerprints, resolution, with declared lenses; GraphQL's practices for the surface; not a GraphQL server." | :30 | OVERRIDDEN BY REVIEW (R03, R04): lenses are experimental; aliases, defaults, UNKNOWN and projection only (REVIEW.md:90) |
| D8 | "Schemas are JSON Schema 2020-12 with an Atlas vocabulary; tables also carry an Arrow schema." | :31 | Done in Phase 1 (`protocol/schema`, `protocol/arrowschema`) |
| D9 | "Requests `BACKWARD_TRANSITIVE`, everything else `FULL_TRANSITIVE`; checked in the gate and at registration." | :32 | OVERRIDDEN BY REVIEW (R03). Within a version: optional request fields; result fields only with a defined missing value; aliases for exact renames. A removal, unit change, type change, result field made non-null, or changed default needs a new major version (REVIEW.md:61). The check stays "in the gate and at registration" |
| D10 | "Governance (grants, limits, budgets, approval) and availability are served beside the profile, with their own revision or none." | :33 | IN SCOPE. R01 extends it: describe's caller state is private and uncached, and the contract is cached apart from the document revision (REVIEW.md:59) |
| D11 | "Data results are run by the gateway (streamed), handed off, or brokered by a ticket, as the register in §6.3 sets per system; export next." | :34 | Streaming is IN SCOPE. Handoff and ticket are DEFERRED. AMBIGUOUS: export. (a) DEFERRED, because ATLAS-CORE.md:238 lists only "data streaming and durable operations where that journey needs them"; or (b) IN SCOPE, because the catalogue lists `fetch · explain · export` for holdings, list and holdings-dates (07-the-catalogue.md:634, 639, 640). The profiles may declare only the interactions built (ATLAS-CORE.md:97, "Every capability need implement only the interactions it actually supports") |
| D12 | "Events join the model: CloudEvents, ephemeral or durable subscriptions, at-least-once; AsyncAPI generated from profiles." | :35 | DEFERRED (REVIEW.md:15) |
| D13 | "The working view shows only what the caller can use; only `atlas.access` shows the rest, with reasons; confidential capabilities are `not_found` to non-holders." | :36 | IN SCOPE. AGENTS/principles: a listed capability without a grant is `not_granted`; only a confidential one is `not_found` |
| D14 | "Agents get a small fixed set of MCP tools, not one per capability." | :37 | AMBIGUOUS: REVIEW.md names CLI, Python and Node only, and never MCP. Reading (a): the MCP edge is DEFERRED. Reading (b): it is IN SCOPE only as ATLAS-CORE-009's "no change to any ... MCP tool list". Either way, no per-capability tool |
| D15 | "Problems are RFC 9457 with Azure's `code`, `target`, `details`, plus `retryable`; long-running work uses `202` and `Operation-Location` on one operation resource." | :38 | IN SCOPE: every failure via `kit/problem` (AGENTS.md:43). `202` plus `Operation-Location` is IN SCOPE "where that journey needs" durable operations (ATLAS-CORE.md:238) |
| D16 | "Identity: an Entra token for the Atlas Gateway from every client; each system reached as itself, a workload or the person...; identity headers never trusted; credentials on the caller's machine never leave it." | :39 | IN SCOPE, with a fake Entra behind the real adapter seam (memory: prove-pattern-first). R26 adds credential binding and serialised refresh (REVIEW.md:84) |
| D17 | "Evaluation order deny, grants, conditions, validation, limits ...; limits reserved before work and settled after (amended 2026-10-30: validation placed after authorisation and before limits)." Why: "a caller without a grant learns nothing of a request's shape, and an invalid request spends no limit." | :40 | IN SCOPE. OVERRIDDEN BY REVIEW (R05, ATLAS-CORE §8): the order is (1) identity; (2) resolve address; (3) visibility, deny, grant; (4) "Validate and normalise the request, then enforce its resource and data scope"; (5) idempotency lookup; (6) confirmation/approval evidence, claim the key, reserve limits; (7) record admission and dispatch; (8) return/stream/operation; (9) record outcome and settle (ATLAS-CORE.md:170–180). So data-scope "conditions" now come AFTER validation, where D17 put them before. AMBIGUOUS only if a capability-level condition must still precede validation; the review's wording places all resource/data scope after normalisation |
| D18 | "The gateway is a front door over services; every service is movable, and moves only when a split criterion is met." | :41 | OVERRIDDEN BY REVIEW (R20): one deployable; in-process provider conformance first (REVIEW.md:13, 78) |
| D19 | "The gateway is one Go module in six layers; ports as Go interfaces in one package; adapters by external system; deployables as composition roots; a package only by the rule in §6.5.5." | :42 | OVERRIDDEN BY REVIEW (R24): `layers.toml` is the rule; same-layer imports are allowed; `tools/layercheck` (REVIEW.md:82; AGENTS.md:39) |
| D20 | "The protocol is implemented once in Go, `sdk/go/protocol`, and shared by the gateway, the conformance gateway, the Go SDK, the CLI and the Python SDK's engine; TypeScript is the second implementation." | :43 | IN SCOPE. The Node client needs a TypeScript implementation that passes the same fixtures (ATLAS-CORE.md:140). AMBIGUOUS: how much of `@atlas/protocol` (validation, grammar, framing, resolution) Node needs for the journey. REVIEW.md does not say |
| D22 | "The Atlas Protocol (§9) is the one normative, language-neutral definition of what crosses the wire; no implementation is the reference; conformance is decided by running the vectors and scenarios; three checks stay independent of the Go implementation (the vectors from `tools/spec/`, the TypeScript implementation, AP-003)." | :44 | IN SCOPE. The references are the independent check (REVIEW.md:21). AMBIGUOUS (path only): the spec says `tools/spec/`; the repo keeps the references at `spec/protocol/references/` |
| D23 | Strict choices: "I-JSON; JSON Canonicalization Scheme (RFC 8785) and SHA-256 for fingerprints; the Atlas Schema profile of JSON Schema (fixed keywords and formats, I-Regexp patterns); RFC 9651 Structured Fields for every Atlas header; `POST` with a JSON body for every interaction; NDJSON ending in an end line and Arrow ending in a zero-row batch; server-sent events with fixed event names; a shared sign-in store; fixed CLI exit codes." | :45 | IN SCOPE: I-JSON, RFC 8785 + SHA-256, the schema profile, SF headers, POST+JSON, the NDJSON end line, fixed CLI exit codes. "Arrow ending in a zero-row batch" applies only where Arrow is offered. OVERRIDDEN BY REVIEW (R11, R26): "a shared sign-in store" becomes credentials from each runtime's own provider, bound to gateway, authority, audience, tenant and scopes (REVIEW.md:69, 84). AMBIGUOUS: "server-sent events with fixed event names" against D41 ("no subscription over HTTP"). Where SSE is still used is in §9.6, another reader's |
| D24 | "Conformance levels (Core required; Streaming, Columnar, Bulk, Events, Handoff, Broker independent); a client declares its levels and is tested at them, through a stdin/stdout runner driven against a scripted conformance gateway." | :46 | IN SCOPE: Core and Streaming. Columnar is optional (R11: Arrow an optimisation), but Python's engine pipe carries Arrow (D43). Bulk, Events, Handoff and Broker are DEFERRED |
| D25 | "The contract is encoding-neutral: JSON, NDJSON, Arrow and Parquet are encodings derived from the profile's schemas; Atlas's types are its own" | :47 | IN SCOPE (§9.2.6) |
| D26 | "Data results travel streamed by the gateway, **handed off** (...no session at the source), or brokered by a ticket (...); export next." | :48 | Handoff and ticket DEFERRED (ATLAS-CORE.md:241; R10) |
| D27 | "The gateway writes the contract into the query it plans (every column aliased and cast), so the source itself produces the writer schema in every execution." | :49 | IN SCOPE: the planner's output contract (INTEGRATIONS.md:68) |
| D28 | "Engines are reached through one `DataSource` port; nothing about an engine leaves its adapter; the Go SQL library (Atlas's own, replacing sqlglot) builds every plan as a typed tree, renders each dialect (Snowflake; DuckDB for tests) and guards every statement, with a read-only role underneath; ADBC where it serves." | :50 | IN SCOPE. The review settles which library: "**`sqlglot-go` is that Go SQL dependency**" (INTEGRATIONS.md:14), pinned at `3d81e5393850e249136284859f6203a34db29f16` (INTEGRATIONS.md:10). Ported in-repo (memory: in-repo modules; the repo has `sqlglot/`) |
| D29 | Spec writing style (layers; OKF bundle; Mermaid) | :51 | No build constraint |
| D30 | "Azure and Astronomer are capabilities of the Atlas Gateway; Azure only through **Resource Graph** (`azure.resource-graph`)" | :52 | DEFERRED |
| D31 | "A capability whose columns are chosen by the caller's own query has the trait `DynamicSchema`: each answer carries its schema, no resolution applies, always `upstream`." | :53 | DEFERRED (no dynamic schema in the journey). Leave room in the result framing (R12 "dynamic result metadata framing", DESIGN-REVIEW.md:132) |
| D32 | Long-term: "one central gateway to which any team deploys its own backend services dynamically" | :54 | DEFERRED. Keep the provider boundary serialisable (R20) |
| D33 | Three patterns (run, hand off, broker) × three identities (gateway, workload, person). "Snowflake run, handed off and brokered, as the gateway now, as the person by OBO in the target"; "vendor APIs run as the gateway always" | :55 | IN SCOPE: Snowflake (DuckDB fake) "run, as the gateway". Because that credential is shared, R05 applies: resource and returned-data scope is enforced by typed rules (REVIEW.md:63; ATLAS-CORE.md:56) |
| D34 | "The credential rule: a credential that leaves the gateway lets its holder do nothing they could not do through the gateway at that moment; every exception is listed and blocks release beyond Development." | :56 | IN SCOPE as a constraint. The journey hands out no credentials (streaming only) |
| D35 | "One On-Behalf-Of exchange in the gateway, built once, a scope and a consent per system; its tokens never leave the gateway." | :57 | DEFERRED ("delegated identity", REVIEW.md:33). R05: "Do not fall back silently from failed user delegation to a service identity" (DESIGN-REVIEW.md:72) |
| D36 | "Atlas's goal is that an agent can build whatever anyone in the firm needs through the CLI and SDKs alone; it is judged and tested by role journeys." | :58 | IN SCOPE as the acceptance journey. R22 overrides how it is tested |
| D37 | "Events are brokered ... clients publish and listen directly on Service Bus as the person; server-sent events through the gateway are not built first." | :59 | DEFERRED |
| D38 | "One route per question: no two listed capabilities answer the same question; overlapping sources are one capability's parameter, or one is authoritative; house rules, served with the working view, say which is the truth for what." | :60 | IN SCOPE: the house rules for the journey's topics. The catalogue states "portfolio-codes: Turn loose words into portfolio codes with `portfolio.search`; list portfolios with `portfolio.list`." and "freshness: Whether a source's data for a date has landed: `data.status`. To be told when it lands: `data.landed`." (07-the-catalogue.md:87, 89) |
| D39 | "What an agent builds runs on its own: the agent chooses the route while building, and the artefact replays it from its lock, with no discovery at run time." | :61 | OVERRIDDEN BY REVIEW (R02). The lock format is left to owners (REVIEW.md:99) |
| D40 | "A metered call whose estimate passes the caller's threshold is confirmed by the person first (`confirmation_required`, `Atlas-Confirm`)." | :62 | OVERRIDDEN BY REVIEW (R06): consent is a receipt "bound to the principal, address, normalised request, plan digest, scope, maximum and expiry. The header is the client's claim only" (REVIEW.md:18, 64). Built in step 3 against the fakes (REVIEW.md:31). AMBIGUOUS: whether any journey capability is metered; the profiles say |
| D41 | "`atlas/1.0` has no subscription over HTTP: every event ... is reached by a connect handover to Service Bus at the Events level; a client that cannot reach Service Bus polls (the event's history capability, the catalogue's ETag, the operation)." | :63 | Events DEFERRED. IN SCOPE: operations are followed by polling. A client watches for a new capability by polling the catalogue's ETag (relevant to acceptance #4). OVERRIDDEN BY REVIEW (R16): polling is equivalent to events only with cursor-addressable retained history (REVIEW.md:74) |
| D42 | "The `client` execution (the ticket, the Broker level) is deferred until its credential meets the credential rule (O54); no client declares Broker in `atlas/1.0`." | :64 | DEFERRED (R10: "Client SQL tickets stay deferred", REVIEW.md:68) |
| D43 | "The Python SDK is a Python face over the Go client engine (`atlas-engine`, the Go SDK as a program) that its wheel ships: they speak the runner protocol (§9.8.3) over pipes, with Arrow results on a second pipe; one engine per process, never shared across a fork." | :65 | IN SCOPE. The engine and Python face come back from branch `pass-1` onto the new protocol (REVIEW.md:19, 92). R11: Arrow is an optimisation, and "A subprocess is an implementation choice behind Python's public contract" (ATLAS-CORE.md:140) |
| D44 | "Three ways to understand a capability, each its own call: `atlas.describe` (what it is: the contract, no parameters), the built-in `atlas.guide` (how to use it: written for an agent, in its client's syntax, within a token budget) and the data interaction `explain` with the request (what this call would do: the exact SQL, columns, estimate, execution)." | :66 | IN SCOPE: describe and guide (ATLAS-CORE.md:238). `explain` is IN SCOPE where a journey profile declares it. "Both `explain` and execution use this compiler path" (INTEGRATIONS.md:68). R01: a guide's identity includes the prose revision (REVIEW.md:59) |
| D45 | "State has three owners: **Entra ID** for access; **Azure Monitor and immutable Blob storage** for telemetry; **Cosmos DB** for state the gateway decides with; every store behind a port." | :67 | IN SCOPE as ports with hermetic fakes. The real technology comes later (memory: prove-pattern-first) |
| D46 | "Adding a capability changes nothing outside the gateway (ATLAS-CORE-009): clients hold no capability's code and never refuse one they have not seen; the MCP tool set is fixed; skills and harness prompts teach the generic loop and name no capability; registration refuses a capability a client at the minimum protocol could not call." | :68 | IN SCOPE (acceptance #4). Registration/startup must refuse a profile a minimum-protocol client could not call |
| D47 | "Three parts, three jobs: the capability holds the state and rules out there and returns the data or bytes; the engine lands them here in one agreed form per kind of result; the client package and its extras turn what landed into what the context needs." | :69 | IN SCOPE. R11 adds that the landed form need not be one filesystem layout or universal Arrow |
| D48 | "Fifteen architectural principles bind the gateway and every service behind it; each mechanism is built once from a tested pattern, with the values §6.7 sets; a service or stable profile passes the review of §6.6.3." | :70 | IN SCOPE (AGENTS.md:17–33, 68) |
| D49 | "One resource envelope on every record of the resource family: `name`, `metadata` (`uid`, `etag`, `generation`, `created`, `updated`, `owner`, `labels`, `deleting`), its own desired fields at the top, and `status` (`observed_generation`, conditions) written only by a controller" | :71 | DEFERRED ("Resources (after the journey)", REVIEW.md:72). AMBIGUOUS: whether the journey's operation document (if one is needed) carries this envelope. §9.3 decides (another reader's) |
| D50 | "The gateway changes nothing outside itself inside a call: the call writes the desired state and controllers converge ... level-triggered, with backoff, resync and finalizers, one leader each; operations are leased and taken over when their replica stops." | :72 | IN SCOPE for durable operations (the worker). OVERRIDDEN BY REVIEW (R08): "One logical operation per key for its whole retention, uncertain outcomes included. Workers are fenced after their lease and reconciled" (REVIEW.md:66) |
| D51 | "The request path reads one checked, immutable snapshot, saved as last-known-good; a replica starts from it when Cosmos DB is down, and while it is down unbudgeted reads are served on per-replica limits and everything else fails closed, for at most 60 minutes." | :73 | Snapshot IN SCOPE (constitution 3, AGENTS.md:21). OVERRIDDEN BY REVIEW (R13): "Separate freshness bounds for policy, groups, connections and links. A switch behind a failed control channel promises no immediate revocation" (REVIEW.md:71) |
| D52 | "Kill switches (`atlas.switch`) turn a capability, domain, service or gateway behaviour off without a deploy, through the snapshot; what fails when each dependency is down for an hour is written down and tested." | :74 | IN SCOPE: each capability gets its blast-radius row and its switch (AGENTS.md:67) |
| D21 | "Patterns may be taken from any open-source project; code only from permissively licensed ones, with notices." | :75 | IN SCOPE (AGENTS.md:50; provenance manifest, INTEGRATIONS.md:35) |

### 2.1 Rejected alternatives that bound the build (§8.2, 08-design-decisions.md:81–97)

| Alternative | Rejected as | Role it keeps | Line |
|---|---|---|---|
| GraphQL | "The gateway's protocol, or a client-side query layer" | "Its practices: introspection, deprecation..., nullability, errors with paths, change classification" | :83 |
| gRPC alone | "The single protocol for clients" | "A possible later binding (duplex streams, Arrow Flight)." | :84 |
| MCP | "The core protocol" | "An edge for agents (D14), with its tasks extension for operations." | :85 |
| Service mesh | backbone | "None." | :86 |
| Dapr | framework | "Optionally inside one cloud service...; its rule that only replayable requests are retried." | :87 |
| Envoy | the gateway itself | "Ingress ... in front of the gateway; the shape of a bounded, fail-closed authorisation call." | :88 |
| Overlay network | how clients reach the gateway | "control plane carries keys and policy, never data" | :89 |
| Azure API Management | gateway / policy layer | "Its developer portal, importing the published OpenAPI." | :90 |
| One out-of-process provider per capability from day one | "The starting shape" | "Service hosts, used when a split criterion is met." | :91 (matches R20) |
| TypeSpec etc. | where contracts are written | "Possibly for third-party providers later." | :92 |
| Own schema language | schema language | — | :93 |
| Policy engine in the request path now (Cedar, OPA) | "Today's access decision" | "Cedar as the direction for conditions on resources." | :94 (matches R05: small typed rules now) |
| Listing every capability with `invocable: false` | "Discovery's default" | "The `missing` structure, in `atlas.access` and in `atlas.describe`'s client eligibility." | :95 |
| A host on the person's machine | where clients call Atlas | "Client extras, in each client's own process." | :96 |
| A separate events repository | where events are defined | "EventCatalog or a schema registry later" | :97 |

### 2.2 Known gaps (§8.3, :101–115). Historical, about the earlier Python gateway.

Points that still matter:
- "Clients build routes; the spec has them follow served bindings from `/.well-known/atlas`." (:105). The journey's clients follow served bindings from bootstrap.
- "The Snowflake ticket's credential is the gateway's own token, with every role of its identity (`SESSION:ROLE-ANY`; 92 roles in Development)..." (:107). This is why tickets are deferred (D42, O54).
- "No implementation of §9 exists yet as such... Four vector files, three profile vectors, the conformance gateway and the runners are still to write." (:115). R12 says this is done for the journey's protocol (REVIEW.md:70).
- "The CLI's Go module is still named `github.com/jhgonline/fi-app-atlas/cli`." (:112). O40 recommends `…/fi-app-atlas/clients/cli` (:151).

### 2.3 Open decisions (§8.4, :119–172) that touch the first build

There are 52 open rows. The numbers are not contiguous (no O4, O7, O9, O16, O18, O25, O27, O32, O35, O44), and O41 is printed after O43, O61 before O60.

| O | Recommended (exact) | Line | Relevance |
|---|---|---|---|
| O3 | Settlement mismatch: "Recorded now." | :123 | IN SCOPE as a settlement fact (R09) |
| O10 | First logical types: "As in §3.2." | :127 | Done (§9.2.3 registry) |
| O11 | snake_case renames: "Yes, as each profile is written." (by aliases) | :128 | Journey profiles use snake_case names, with aliases for today's names if needed |
| O12 | "Minimum sunset; the lock's format." Recommended "90 days; `atlas.lock` in TOML." | :129 | REVIEW.md:99 leaves this to owners ("which the spec still has open"). A fixture states its choice |
| O13 | Confidential capabilities: "Administration, and those whose existence is sensitive." | :130 | None in the journey |
| O14 | Requesting access from `atlas.access`: "Later." | :131 | DEFERRED |
| O15 | "One operation resource replacing jobs, runs, publications and permits." "Yes, now, before third parties." | :132 | IN SCOPE where durable work is needed |
| O19 | "Grants per interaction." "Yes." | :134 | IN SCOPE: grants are keyed by `capability@version:interaction` |
| O21 / O22 | Service protocol transport "HTTP/2 with Arrow and JSON"; signed token "Entra between services, with Atlas's scope as a signed claim." | :136–137 | DEFERRED (R20; REVIEW.md:13) |
| O23 | Jobs, operations and controllers "into `worker` (controllers and operation workers are already leased, so they move by configuration, D50)" | :138 | IN SCOPE: "plus a worker where durable work needs one" |
| O26 | "`gateway/cmd/<name>`; the `local` root runs the fakes and emulators, no other root does." | :140 | IN SCOPE: the deployable layout. Fakes are wired only in the `local` root |
| O29 | Merged capabilities "only where sources share one result schema". "Yes." | :142 | `portfolio.holdings@v1` is a merged († ) capability: holdings plus history (07-the-catalogue.md:639) |
| O33 | Conformance gateway "Go, in `sdk/go`, on `sdk/go/protocol`." | :145 | IN SCOPE |
| O34 | Remaining vectors "Before any client is changed to follow §9." | :146 | Done (R12) |
| O36 | Snowflake spike "Run before building the handoff." | :147 | DEFERRED with the handoff |
| O38 | "“surface” in prose, `family` on the wire" | :149 | IN SCOPE (wire member `family`) |
| O39 | "The first set of traits; a new trait as a protocol minor version." "Yes." | :150 | |
| O40 | Rename the CLI module to `…/fi-app-atlas/clients/cli`; rename the harness command `package catalog` | :151 | AMBIGUOUS whether the repo has already applied it; check `clients/cli/go.mod` |
| O48 | Node takes handoffs "After Python's; streaming and `export` first." | :158 | Node streams |
| O56 | "One journey per role first, run on every release against Development." | :166 | DEFERRED beyond the build's own journey |
| O60 | Caching: "Per caller and in memory for `Licensed` capabilities; shared across callers only for unlicensed data, keyed by address, request, fingerprint and as-of date." | :171 | OVERRIDDEN BY REVIEW (R18): results are private to their caller; reuse is keyed by authorisation, environment and data revision, and reauthorised (REVIEW.md:76) |
| O61 | Client engine in the cloud: "After Phase 2, decided on the M9.5 load test" | :170 | DEFERRED |
| O62 | Second audit container "Yes: written by the same `AuditSink`, merged by `request_id`" | :172 | DEFERRED. The `AuditSink` port name is useful |

---

## 3. Resources, actions, events and vendor data: what the first build must leave room for

All four surfaces are **DEFERRED** in the first build (REVIEW.md:15, 72). The journey uses only the data family. What the journey itself needs from them is collected in §3.5.

### 3.1 Resources (§2.3, 02-3-resources.md). DEFERRED.

Status line: "`spec/surface/resources.md` · proposed" (:4).

**Standard methods** (:61–69):

| Interaction | Effect | Result | Semantics |
|---|---|---|---|
| `get` | `read` | `record` | "One resource by its name (`nautilus/dashboards/{id}`)" |
| `list` | `read` | `collection` | "Paged, filtered, ordered" |
| `create` | `write` | `record` | "Requires an idempotency key; returns the resource with its ETag" |
| `update` | `write` | `record` | "Takes a field mask: only the named fields change; requires the ETag it read" |
| `delete` | `write` or `irreversible` | `record` | "Requires the ETag; its profile says whether it can be undone" |
| `watch` | `read` | `record` | "`subscription` of changes to one resource or a filtered list" |
| custom (`:publish`, `:share`) | `declared` | `declared` | "The exception: an operation that is not one of the above" |

"These follow Google's API Improvement Proposals (resource names, standard methods, field masks, ETags, paging)." (:71)

**Rules** (:42–47):
1. "A resource is named by a path of collection and id segments (`nautilus/dashboards/{id}`); the name is stable for the resource's life."
2. "Every write carries an idempotency key, and every `update` and `delete` the ETag it read; there is no unconditional overwrite."
   - 2.1: an upload write "uses `transfer` delivery: the caller uploads to a short-lived link by byte range, resumably, then commits; the commit is idempotent."
3. "A new served version of a resource type requires its lens to and from the stored form; a resource type changes its stored form only by a migration recorded in its profile's evolution."
4. "Every record carries the envelope of §9.3 §9.3.4; the caller writes its own fields and `labels`, the gateway everything else, and only a controller writes `status`."
5. A resource the gateway acts on outside itself "is converged by a controller (§6.1 §6.1.13), never inside the call that wrote it: the call answers with the record, and `status` says when it is `ready`."

**Requirements** (:49–55):
- ATLAS-RESOURCE-001: "`update` changes only the fields in its mask, and fails with `412` when the ETag no longer matches."
- ATLAS-RESOURCE-002: "A `create` repeated with the same idempotency key returns the first result and creates nothing."
- ATLAS-RESOURCE-003: "A resource written through one served version reads back through every other served version, converted by the declared lenses; a conformance test round-trips each pair."
- ATLAS-RESOURCE-004: "Every resource record the gateway sends validates against `resource.schema.json`, and its profile's record schema declares the envelope exactly."
- ATLAS-RESOURCE-005: "A `create` or `update` that sets `metadata` (other than `labels`) or `status` changes neither; a field mask naming them is `invalid_request`."
- ATLAS-RESOURCE-006: "`metadata.generation` and the ETag move on every change to the record's own fields or labels, and on nothing else." OVERRIDDEN BY REVIEW (R14): "The ETag covers the full representation; `generation` is separate" (REVIEW.md:72; ATLAS-CORE.md:228; DESIGN-REVIEW.md:141, "Do not lose status changes behind a 304 response").
- ATLAS-RESOURCE-007: "A converged resource's `status.observed_generation` reaches its `metadata.generation`, with `ready` true, once its controller has converged it; while it cannot, `ready` is false with a `reason`."

**The sequence** (:19–28): `get` returns "dashboard, ETag e7". Then "update mask title, If-Match e7, Idempotency-Key" returns "dashboard, ETag e8". Then "update mask title, If-Match e7" returns "412 precondition_failed". Header names shown: `If-Match`, `Idempotency-Key`. Problem: `precondition_failed` 412. AGENTS.md:15: "the codes are `idempotency_conflict` 409 and `precondition_failed` 412".

**The envelope, exactly** (:75–111):
```json
{
  "name": "nautilus/dashboards/d1",
  "metadata": {
    "uid": "6f1c0e9a2b7d4c3e8f5a1b2c3d4e5f60",
    "etag": "\"e8\"",
    "generation": 8,
    "created": "2026-10-01T09:00:00Z",
    "updated": "2026-10-30T14:02:11Z",
    "owner": "3f2b…",
    "labels": { "desk": "credit" },
    "deleting": null
  },
  "title": "Credit risk",
  "visibility": "public",
  "status": {
    "observed_generation": 8,
    "conditions": [
      { "type": "published", "status": "true", "reason": "copied", "message": "", "observed_generation": 8, "last_transition": "2026-10-30T14:02:15Z" },
      { "type": "ready", "status": "true", "reason": "converged", "message": "", "observed_generation": 8, "last_transition": "2026-10-30T14:02:15Z" }
    ]
  }
}
```
(The source prints the conditions one member per line. They are condensed here; the members and values are unchanged.)

Notes:
- `etag` carries the quotes of a strong ETag (`"\"e8\""`).
- `uid` is 32 lowercase hex characters with no hyphens. Compare `Principal` = `format: uuid` (§9.2.3).
- AMBIGUOUS: `owner` is printed "`3f2b…`", truncated with an ellipsis. It is not a valid value, so don't use it as a vector.
- Condition `status` is the string `"true"`, not a boolean.
- "Atlas keeps the desired fields at the top of the record rather than under a `spec` member" (:114; AGENTS.md:11).

**Versions** (:118): "one stored form and MAY be served in several versions ... converted ... with the lens the newer version declares". In short: "served at up to two versions" (:13).
AMBIGUOUS under R03: the conservative model forbids arbitrary lenses within a version. Resource served versions are across major versions, so lenses could still convert between them. Reading (a): resource version lenses survive, because they are across major versions. Reading (b): they become a later decision. REVIEW.md does not address this.

**Events** (:122): "Every resource emits change events (`<id>.created`, `<id>.updated`, `<id>.deleted`) from the gateway's state (`gateway`); `watch` is a subscription to them." OVERRIDDEN BY REVIEW (R25): "derived change events live under the resource's watch contract" (REVIEW.md:83).

**Open** (:126): "Which of today's routes become resources first... Recommended: the dashboards, with Nautilus V3." (O5)

**Leave room for:** effects `read`/`write`/`irreversible`/`declared`; result shapes `record`/`collection`; the `If-Match` precondition step and the `Idempotency-Key` step as pipeline steps (AGENTS.md:14); `412 precondition_failed` and `409 idempotency_conflict` in the problem registry; the `transfer` delivery; field masks.

### 3.2 Actions (§2.4, 02-4-actions.md). DEFERRED.

- "`plan` changes nothing and returns what would happen; `approve` is by someone other than the requester, where the policy says; the action's own verb runs it as an **operation** and ends with a **receipt**." (:10)
- "Effects (which system, which environments, whether it can be undone) are part of the contract; changing them is always a new version." (:11)
- "Every run carries an idempotency key; Production refuses an action unless a person runs it." (:12). OVERRIDDEN BY REVIEW (R06; ATLAS-CORE.md:46): whether a person acts "comes from trusted authentication context, not an `is_human` parameter".
- State diagram (:17–27): `[*]→Planned (plan)`; `Planned→Approved (approve (another person))`; `Planned→Running (run (no approval needed))`; `Approved→Running (run)`; `Running→Succeeded|Failed|Cancelled`; each terminal → `[*]`.
- Example (:32): `run` answers `approval_required` until a colleague approves. It returns an operation (`pending`, `running`, `succeeded`). "No action is catalogued yet; this example is illustrative." AMBIGUOUS / contradicted: §7 has preview `astronomer.deployment` (R23, DESIGN-REVIEW.md:150). OVERRIDDEN BY REVIEW (R23): distinguish illustrative examples, registered profiles and deployed availability (REVIEW.md:81).

**Rules** (:42–45):
1. "Every `run` requires an idempotency key; a repeated key returns the first run and changes nothing twice."
2. "A plan expires; running an expired plan plans again."
3. "A change to an action's effects is always a new version, never a lens"
4. "An `irreversible` action is approved by someone other than its requester, always."

**Requirements:**
- ATLAS-ACTION-001: "`plan` changes nothing in any system; a conformance test checks the target before and after." (:47)
- ATLAS-ACTION-002: "A `run` whose policy needs approval is refused with `approval_required` until approved by another principal." (:48)
- ATLAS-ACTION-003: "Every finished run has a receipt in its profile's schema, linked from the audit record." (:49)

**Interactions** (:55–60):

| Interaction | Effect | Result | Delivery | What it does |
|---|---|---|---|---|
| `plan` | `read` | `record` | `single` | "What `run` would do, changing nothing (like Terraform's plan); stored, with an id" |
| `approve` | `write` | `record` | `single` | "Approves a stored plan, by someone other than its requester, where the policy requires it" |
| `run` | the action's own (`external` or `irreversible`) | `record` | `operation` | "Carries out a plan (or plans and runs, where no approval is needed); returns its operation" |
| the action's verb (`:restart`, `:trigger`) | as `run` | as `run` | as `run` | "it is `run` with its own verb" |

- The profile adds Effects, the plan's schema and the receipt's schema, and Policy ("the environments where it is refused unless a person runs it (Production always)") (:64–66).
- **Operation states** (:70): "`pending`, `running`, `succeeded`, `failed`, `cancelled`, and `UNKNOWN` for a state a client does not know". "It emits its lifecycle as events." This state set is the same one the journey's operations use (see §3.5).
- R06 (approval receipt), R08 (uncertain outcomes; fencing) and R17 (cancellation "requested or confirmed"; separate start deadline, execution deadline and retention) apply here. They are built in step 3 against the fakes (REVIEW.md:31, 64, 66, 75).

**Leave room for:** effects `external` and `irreversible`; delivery `operation`; the problem `approval_required`; the receipt; the plan digest in the confirmation receipt (R06).

### 3.3 Events (§2.5, 02-5-events.md). DEFERRED.

DEFERRED as "separately gated proofs on real systems" (REVIEW.md:15). Steps R15, R16 and R25 are step 5 (REVIEW.md:73–83).

- The gateway is "the broker, not the pipe"; the client publishes and listens "directly on Service Bus, as the person" (:10).
- "Every event type is a capability with a profile and an owner, its publisher" (:11). OVERRIDDEN BY REVIEW (R25): one registration model (REVIEW.md:83).
- Rights: "`event.topic@v1:create` for those who may create topics; sending to or listening on one topic is a grant on that entity, mirrored exactly by a Service Bus role on it." (:12)
- "Every event is a CloudEvent whose `dataschema` names its profile... At least once; duplicates dropped by `id`; unknown types ignored." (:13)

**Rules 1–9** (:45–53):
1. Brokered, a handover "with no secret".
2. "Rights mirror grants exactly"; Data Sender or Data Receiver; "Shared-access keys are off"; entities and roles converged by controllers.
3. "At least once... handlers MUST be idempotent."
4. "Unknown event types are ignored; adding an event type is a dangerous change, not a breaking one."
5. "Within a version, payloads are fully and transitively compatible; a breaking change is a new type (`…v2`)". This is OVERRIDDEN BY REVIEW (R03) in spirit; the review gives no event-specific text.
6. "Filters are applied at the source".
7. "Order is guaranteed only per subject, for capabilities with the `Ordered` trait (Service Bus sessions)."
8. "History is read from a data capability, never by replaying events: every event profile names it in `meaning.history`, and a client that cannot reach Service Bus polls it instead of subscribing." OVERRIDDEN BY REVIEW (R16): equivalence holds only with cursor-addressable retained history (REVIEW.md:74; ATLAS-CORE.md:214).
9. "No event travels through the gateway, the gateway's own included (`atlas.catalog.changed`, resource changes, `atlas.operation.*`)".

**Requirements:**
- ATLAS-EVENT-001: the client checks before publishing; a "gateway-held sampling subscription reports non-conforming official events" (:55). R16: "A direct sender's authorization/validation must be enforced beyond an SDK convention" (DESIGN-REVIEW.md:143).
- ATLAS-EVENT-002: a durable subscriber that reconnects receives every event delivered while away, "within the entity's retention" (:56).
- ATLAS-EVENT-003: "Every event's `dataschema` names a profile fingerprint the catalogue serves." (:57)
- ATLAS-EVENT-004: AsyncAPI generated and validated in the gates (:58).
- ATLAS-EVENT-005: role iff grant (a reconciliation test) (:59).
- ATLAS-EVENT-006: "A role no grant backs is removed within 60 seconds of the grant's change, and within five minutes when the change's event is lost or the change was a group membership in Entra; a role or entity the gateway made and no record owns is removed by the next resync." (:60)

**Capabilities** (:66–71):
- an event type: `publish` (`external`), `subscribe` (`read`).
- `event.topic@v1`: `get`, `list`, `create` (`write`), `delete` (`irreversible`).
- `event.queue@v1`: same as `event.topic@v1`.
- `event.subscription@v1`: `get`, `list`, `create` (`write`), `delete` (`write`).

**CloudEvent attributes** (:79–84):
- `type`: e.g. "`com.jhg.fiquant.data.landed.v1` for `data.landed@v1`".
- `dataschema`: "The writer profile's URI and fingerprint".
- `id`, `source`, `time`, `subject`: as CloudEvents; `id` is unique, for dedup.
- `data`: a record in the profile's schema.

**Connect handover** (:90–104), exactly:
```json
{
  "kind": "connect",
  "protocol": "amqp",
  "namespace": "dv-eus2-fiqdataevents-sbus.servicebus.windows.net",
  "entity": "data-landed/subscriptions/ana-7f2",
  "auth": { "kind": "entra-scope", "scope": "https://servicebus.azure.net/.default" },
  "rights": ["listen"],
  "profile": "data.landed@v1#51c7e08b2d6f9a43",
  "format": "cloudevents+json",
  "settle": "peek-lock",
  "durable": true,
  "expires": "2026-10-30T18:00:00Z"
}
```
Its structure is `connect.schema.json` (§9.3), and the client's behaviour is §9.7.8 (:106). The schema exists in the repo at `spec/protocol/references/schemas/connect.schema.json`.

- **Client loop** (:110): ask for the handover; get its own token for the scope; connect; receive, resolve, yield, complete; renew the token before expiry; "on refusal, ask the gateway again (a revoked grant answers `not_granted`)".
- **AsyncAPI 3.0** is generated, "never the source" (:114).
- **Decided:** D37, D41 (:118, 120).
- **Open** (:124–126): event catalogue at the platform level; browser AMQP over WebSockets; triggers "later".

**Journey impact:** `data.status@v1` is the history capability that `data.landed@v1` names in `meaning.history` (CAPABILITY-HANDBOOK.md:544). The handbook warns: "A latest snapshot is not a replayable landing history. Define whether fallback means latest-state reconciliation or event replay; do not promise both". So the `data.status@v1` profile must not claim to be equivalent to event history unless it offers cursor-addressable history (R16). AMBIGUOUS (owner decision): data.status's "source/date behavior requires clarification" (CAPABILITY-HANDBOOK.md:361).

### 3.4 Vendor API data (§2.2.2, 02-2-2-vendor-api-data.md). DEFERRED.

No vendor capability is in the journey.

- Naming: "Named for the vendor (`yieldbook.price-yield`), because the team does not own it." (:9)
- Stability: "`upstream` while the vendor's response is passed through; `official` once the service maps it into an Atlas schema, which anything relied on must be." (:10, :57). The journey's capabilities are all `official` (07-the-catalogue.md:361, 634, 635, 639, 640).
- "Metered and licensed: budgets reserved before the call; results never persisted." (:11)
- "Long calls (scenario grids, cash-flow projections) return an operation. Never handed off." (:12)
- Flow (:15–20): "Front door: budget reserved" → "Vendor service" → "Shared throttle and circuit breaker" → "Vendor API" → "Mapped into the writer schema" → "Streamed, or an operation's result".

**Shared concerns** (:56–63):

| Concern | Rule |
|---|---|
| Domain | named for the vendor, as a provider domain (§2.1) |
| Stability | `upstream` while passed through, "its schema observed and checked for drift"; `official` once normalised |
| Normalising | "when the vendor changes, only that mapping changes, and the profile does not" |
| Metering | "the `Metered` trait, with its unit and price; budgets reserved before the call (§6.2)" |
| Licences | Results are `Licensed`: "never persisted, except a job's answer for its own caller" |
| Long calls | return an operation |
| Travel | "Always `gateway` execution, streamed or as an operation's result; never handoff (there is no vendor result storage to hand off)" |
| Throttle | "One shared rate and concurrency limit per vendor, across every copy of its service, and a circuit breaker (§6.1)" |

**Platform APIs** (:37–40):
- `azure`: "**Azure Resource Graph only** (`azure.resource-graph`)". Today the gateway's identity; the target is OBO. "Read-only by construction; its columns are the query's (`DynamicSchema`); paged by Resource Graph's skip token; throttled per caller".
- `astronomer`: "Deployments, DAGs, runs, logs"; "an Astronomer API token per environment, the caller recorded in the audit".

**Rules** (:44–48):
1. "A vendor capability never returns the vendor's credential, or a link that carries it."
2. "An `upstream` capability is normalised before an application, dashboard, pipeline or job depends on it."
- ATLAS-VENDOR-001: "A change in a vendor's response that the service's mapping absorbs changes no fingerprint."
- ATLAS-VENDOR-002: "Drift in an `upstream` capability's observed schema is reported to its owner within a day."

**Review overrides:**
- R07: "One budget authority per metered provider and an atomic reservation set. Unsupported combinations are refused." Protective rate limits are kept apart (REVIEW.md:16, 65).
- ATLAS-CORE.md:82: a vendor adapter must explain "coverage, identifier mapping, metering, freshness, failure behavior and permitted use".
- R18: results are private to their caller (REVIEW.md:76).

**Leave room for:** traits `Metered` (unit and price) and `Licensed`; the stability values `upstream`/`official`; a per-vendor shared throttle and breaker in the adapter (one hop); a budget authority per metered provider (R07).

### 3.5 What the journey itself needs from these sections

| Need | Source | Status |
|---|---|---|
| Operation states `pending`, `running`, `succeeded`, `failed`, `cancelled`, `UNKNOWN` (lowercase plus `UNKNOWN`, AGENTS.md:12) | 02-4-actions.md:70 | IN SCOPE wherever a journey interaction returns an operation ("durable operations where that journey needs them", ATLAS-CORE.md:238). AMBIGUOUS: whether any journey capability needs one. Holdings is "table · chunked" (07-the-catalogue.md:639); the others are "table · single" |
| Idempotent retries behave the same in every client | REVIEW.md:50 (acceptance #5); ATLAS-CORE.md:198 ("Metered reads that can incur duplicate charges need this treatment too") | IN SCOPE. The key is scoped to "tenant, principal, environment and capability-interaction" and bound to the normalised semantic request (ATLAS-CORE.md:198). Effective defaults are persisted at admission, and "A retry ... must not choose a new “latest” date" (ATLAS-CORE.md:200) |
| `409 idempotency_conflict`, `412 precondition_failed` | AGENTS.md:15; 02-3-resources.md:27 | `idempotency_conflict` is IN SCOPE (retries). `precondition_failed` is DEFERRED with resources |
| Provider boundary: plan, stream, settle, describe, health | 00-vision.md:164, 168 | IN SCOPE in process (R20) |
| `official` stability on every journey profile | 07-the-catalogue.md rows | IN SCOPE |
| No vendor credential or link ever returned | 02-2-2:44 | Trivially kept |
| `data.status` must not promise event-history equivalence | R16; CAPABILITY-HANDBOOK.md:544 | IN SCOPE (profile wording) |

---

## 4. §9.2's rules that a gateway or client applies at run time

Phase 1's `sdk/go/protocol/schema` already does the compile-time work:
- `Compile(…, Kind)` with `Open`/`Request` kinds holds a schema to the profile: keywords, depth `MaxDepth = 32`, size `MaxSize = 1 << 20` measured on the RFC 8785 form (Q-PROF-12), the key rule, the UNKNOWN rule, and the oneOf discriminator.
- `Validate` returns `[]Failure{Pointer, Keyword}`, sorted by pointer then keyword and deduplicated (validate.go:15–35).
- `arrowschema.Derive(table, profile, protocol)` sets `atlas:profile`/`atlas:protocol` (arrowschema.go:17–18, 47). `ToBatch`/`ToRows` convert rows (rows.go:25, 158).

What follows is what the running gateway, CLI, engine and Node client must still DO with those parts.

### 4.1 At the gateway

| # | Rule (exact) | Cite | Runtime obligation |
|---|---|---|---|
| G1 | "The gateway MUST NOT serve a profile whose schemas leave this profile; the gate and registration refuse it." | 09-2-schemas.md:140 (Rule 1) | IN SCOPE. At registration/startup (the snapshot build), compile every request schema with kind `Request` and every other schema with `Open`. Refuse the whole profile if any fails. The built-in `atlas.*` profiles are exempt from the lint (references reading Q-PROF-9, QUESTIONS.md:14) |
| G2 | Validation after authorisation and before limits | D17 (08:40); ATLAS-CORE.md:174 | IN SCOPE. The gateway validates the request again. It never trusts the client's check. A caller without a grant "learns nothing of a request's shape" |
| G3 | "The problem for an invalid request is `invalid_request`, with each failure in `details` and the first one's pointer as `target`." | 09-2:99 (§9.2.5 step 3) | IN SCOPE. One `details` entry per `Failure`. `target` = the pointer of the first `Failure` in the order Q-VAL-1 sets: "sort by pointer, then by keyword, comparing code points... So `/10` sorts before `/2`" (QUESTIONS.md:13). That order is already `Validate`'s. The detail member names and the HTTP status are in §9.3/§9.6 (another reader's) |
| G4 | Request schemas are closed: `"additionalProperties": false` on every object | 09-2:34 (§9.2.1 rule 6) | IN SCOPE. OVERRIDDEN BY REVIEW (R04): "unknown request fields are refused, never dropped", and "A request is validated under its own contract before any transformation" (REVIEW.md:62). DESIGN-REVIEW.md:62 adds "and the transformed request afterward". Maps are allowed (`labels`, Q-PROF-7) |
| G5 | "A string property with `enum` MUST include the value `"UNKNOWN"` ... except in a request schema, where `UNKNOWN` MUST NOT be accepted." | 09-2:65 | Compile-time is done. At run time, a request carrying `"UNKNOWN"` fails `enum` validation |
| G6 | `format` is an assertion | 09-2:38 | Done by `Validate`. Remember that it applies to requests at the gateway too |
| G7 | "Check digits are not validated by the protocol; a capability may refuse an identifier with its own problem." | 09-2:87 | IN SCOPE as a choice. An `Isin` or `Cusip` with a bad check digit passes protocol validation. `portfolio.search`/`holdings` may refuse it with their own declared problem (that problem then needs a next step, VISION-002) |
| G8 | Arrow streams: "The stream's schema metadata carries `atlas:profile` (the `exact-id` it was written under) and `atlas:protocol` (`atlas/1.0`)." Field metadata `atlas:<keyword without x-atlas->` with "its value the keyword's JSON text" | 09-2:136 | IN SCOPE wherever Arrow is sent (Python engine pipe; Columnar). Use `arrowschema.Derive` with the served exact-id, so the schema is never hand-written (Rule 3, :142). Field metadata values use the RFC 8785 form; schema metadata is plain text (Q-ARROW-4, QUESTIONS.md:157) |
| G9 | Field order = order of `properties` | 09-2:119 | IN SCOPE. The profile store must keep the document's member order. A Go `map[string]any` loses it (Q-ARROW-1, QUESTIONS.md:7: "A reader must parse `properties` keeping member order"). Phase 1's `ijson.Object` keeps it |
| G10 | Nullability: "an Arrow field is nullable exactly when its property allows `null` or is not in `required`" | 09-2:136 | IN SCOPE. R12 flags that "optional-field absence becomes null in Arrow" (DESIGN-REVIEW.md:130). Absent and null therefore become the same in Arrow; see AP-SCHEMA-003 below |
| G11 | Writer schema enforced before delivery | 00-vision.md:164 ("Batches match the profile"); DESIGN-REVIEW.md:22 ("Check planned types and actual returned batches") | IN SCOPE. The gateway checks the source's batches against the writer schema. AMBIGUOUS: §9.2 itself never says the gateway validates results; the duty comes from the vision, D27 and the review |
| G12 | Encodings and their levels: JSON `application/json` Core; NDJSON `application/x-ndjson` Streaming; Arrow IPC `application/vnd.apache.arrow.stream` Columnar; Parquet `application/vnd.apache.parquet` Bulk; CSV `text/csv` "Display only, never read by a client" | 09-2:105–111 | IN SCOPE: JSON and NDJSON. Arrow optional (R11). Parquet (Bulk) DEFERRED. CSV, if offered, only for display (e.g. CLI output) |
| G13 | "A new encoding ... is a protocol minor version: a media type, a mapping and a framing; no profile, fingerprint or capability changes." | 09-2:113 | The encoding choice stays out of the profile and the fingerprint |

### 4.2 At a client (CLI, Python engine, Node)

| # | Rule | Cite | Runtime obligation |
|---|---|---|---|
| C1 | "A client MUST validate every request against its schema before sending it, by 9.2.5." | 09-2:141 (Rule 2) | IN SCOPE in all three clients. CLI and the Python engine use `protocol/schema`. Node needs the TypeScript implementation (D20), which must give identical results on the schema vectors (AP-SCHEMA-001) |
| C2 | "Produce either `valid`, or every failure, each with the instance's JSON Pointer (RFC 6901) and the keyword that failed." | 09-2:99 | IN SCOPE. A client-side failure must surface the same `invalid_request` shape as the gateway's, so that "Invalid requests ... behave the same in every client" (REVIEW.md:50) |
| C3 | Meta-data and Atlas keywords never make an instance invalid, "except `x-atlas-logical`, whose constraint in 9.2.3 is applied as if written as its own keywords" | 09-2:98 | Done in `Validate` (logical.go) |
| C4 | `date-time` "is exact to the microsecond in every encoding (the grammar allows more digits, which a receiver truncates, §9.1.4)" | 09-2:115 | IN SCOPE on receive in every client. Truncate, never round. Writing: "RFC 3339 in UTC with `Z` and exactly six fractional digits" (Q-TIME-1, QUESTIONS.md:34) |
| C5 | `Decimal(p,s)`, `p` 1–38 and `s` 0–`p`, carried as a `string` with `format: decimal`. `Money` = `amount` (`Decimal(38,6)`) + `currency` (`Currency`), both required | 09-2:72–73 | IN SCOPE. Exact decimals in every client (acceptance #5). Python `Decimal`; Node an exact representation; "must not silently turn an exact amount into a binary floating-point value" (ATLAS-CORE.md:134). Fit check uses the SQL meaning (Q-DEC-1) |
| C6 | `integer` = "a JSON number with no fractional part within the I-JSON range"; `int64` is a string format | 09-2:46, 51 | IN SCOPE. Node must keep `int64` strings out of `number` (large integers, R12 acceptance) |
| C7 | Patterns: `^ R $` where R is an I-Regexp (RFC 9485), matched against the whole value's code points; "Patterns are case-sensitive." | 09-2:91 | Done (`iregexp.go`). The TypeScript side must match, including the two extra refusals in Q-RE-1 (QUESTIONS.md:12) |
| C8 | Enum values unknown to the reader | 09-2:65 (UNKNOWN in result enums) | IN SCOPE in resolution: a client maps an unknown enum value to `UNKNOWN` (§9.5; Q-DOC-1 extends this to protocol documents, QUESTIONS.md:10). R03 keeps UNKNOWN as a client obligation (REVIEW.md:90) |
| C9 | `x-atlas-aliases`: "Earlier names of this property within the capability's version, oldest first" | 09-2:59 | IN SCOPE. Aliases are one of the four client obligations (REVIEW.md:90). R04: aliases carry their schema scope, so a request alias and a result alias with the same spelling are never both transformed (DESIGN-REVIEW.md:64) |
| C10 | `x-atlas-reference`: "A `pinned-id`": "The data capability that lists the property's valid values" | 09-2:60 | IN SCOPE as guidance. A holdings `portfolios` property can reference `portfolio.list@v1` (the identifier lookup). Validation does not enforce it. AMBIGUOUS: whether a client or guide follows the reference automatically; §9.2 does not say |
| C11 | `x-atlas-deprecated`: `{ "reason", "since", "sunset", "replaced_by" }` | 09-2:61 | All four members are required; `replaced_by` may be null (Q-PROF-11). What a client does at run time (warn?) is not in §9.2. AMBIGUOUS: see §3.3 or §9.7 |
| C12 | `x-atlas-key`: "every key property is required and not nullable, except where `x-atlas-logical` is an identifier that may be missing (then the key is the set of non-null key values)" | 09-2:58 | Compile-time is done; the identifiers are `Isin`, `Cusip`, `Sedol`, `Figi`, `Lei` (Q-PROF-6). For holdings, DESIGN-REVIEW.md:161 says "the example uses a nullable ISIN as part of the key. Cash, derivatives and instruments without an ISIN need an unambiguous row identity, and the owner must settle the grain" (left to owners, REVIEW.md:97) |
| C13 | `x-atlas-unit` on `number`/`integer` where "no logical type implies one". `Bp`, `Percent`, `Fraction`, `Years` take their units from §9.5 | 09-2:62, 71 | Guide and readiness check: every field needs a meaning and a unit (VISION-002). Holdings `weight`: "whether `weight` is a fraction" must come from the owner (ATLAS-CORE.md:80) |
| C14 | AP-SCHEMA-002: "Every client and the gateway derives the same Arrow schema from the same table schema (the Arrow test vectors)." | 09-2:151 | IN SCOPE where Arrow is used (Go engine; Node only if it decodes Arrow) |
| C15 | AP-SCHEMA-003: "The same result, sent in every encoding its capability offers, resolves to identical records." | 09-2:152 | IN SCOPE. Cross-encoding test of JSON, NDJSON and Arrow for each journey capability. Normalise null against absent per the references |

### 4.3 The Atlas Schema profile (reference for writing the journey profiles; Phase 1 enforces it)

- Root: `"$schema": "https://json-schema.org/draft/2020-12/schema"` (:17). `$vocabulary` exactly (:18): core, applicator, validation, meta-data and format-assertion `true`; `"https://atlas.jhg.com/vocab/2026-10": false`.
- Allowed keywords (:23–28):
  - Core: `$schema`, `$vocabulary`, `$id` (root only), `$defs`, `$ref` (only `#/$defs/<name>`), `$comment`.
  - Applicator: `properties`, `additionalProperties`, `items`, `prefixItems`, `anyOf`, `oneOf` (const discriminator).
  - Validation: `type`, `enum`, `const`, `required`, `minimum`, `maximum`, `exclusiveMinimum`, `exclusiveMaximum`, `multipleOf` (integers only), `minLength`, `maxLength`, `pattern`, `minItems`, `maxItems`, `uniqueItems`, `minProperties`, `maxProperties`.
  - Meta-data: `title`, `description`, `default`, `deprecated`, `examples`, `readOnly`, `writeOnly`.
  - Format: `format`.
  - Atlas: §9.2.3.
- Not allowed (:30): `$dynamicRef`, `$dynamicAnchor`, `$anchor`, `$recursiveRef`, `allOf`, `not`, `if`, `then`, `else`, `dependentSchemas`, `dependentRequired`, `patternProperties`, `propertyNames`, `contains`, `minContains`, `maxContains`, `unevaluatedProperties`, `unevaluatedItems`, `contentSchema`, a `$ref` to another document, and any unlisted keyword.
- Rules 4–6 (:32–34): one `type`, or `[T, "null"]`; ≤ 32 levels and ≤ 1 MiB; request schemas closed, others open.
- Formats (:42–49): `date`, `date-time`, `duration`, `decimal`, `int64`, `uri`, `uuid` (lowercase RFC 9562), `capability-id`, `pinned-id`, `exact-id`, `address`.
- Logical types (:71–85), the complete `atlas/1.0` list:

  | Type | Base / constraint |
  |---|---|
  | `Bp`, `Percent`, `Fraction`, `Years` | `number`, units §9.5 |
  | `Decimal(p,s)` | `string`, `format: decimal` |
  | `Money` | `amount` `Decimal(38,6)` + `currency` |
  | `Currency` | `^[A-Z]{3}$` |
  | `Country` | `^[A-Z]{2}$` |
  | `Isin` | `^[A-Z]{2}[A-Z0-9]{9}[0-9]$` |
  | `Cusip` | `^[A-Z0-9]{8}[0-9]$` |
  | `Sedol` | `^[B-DF-HJ-NP-TV-Z0-9]{6}[0-9]$` |
  | `Figi` | `^BBG[B-DF-HJ-NP-TV-Z0-9]{8}[0-9]$` |
  | `Lei` | `^[A-Z0-9]{18}[0-9]{2}$` |
  | `Tenor` | `^[1-9][0-9]*[DWMY]$` |
  | `PortfolioCode`, `IndexCode` | `minLength: 1` |
  | `Path` | `minLength: 1` |
  | `Principal` | `format: uuid` |
  | `Uri` | `format: uri` |

  "a domain's own types are added by a protocol minor version" (:67).
- Arrow mapping (:123–134):
  - `boolean` → `Bool`
  - `integer` → `Int64`
  - `number` → `Float64`
  - plain `string` → `Utf8`
  - `date` → `Date32`
  - `date-time` → `Timestamp(MICROSECOND, "UTC")`
  - `duration` → `Duration(MICROSECOND)`
  - `int64` → `Int64`
  - `Decimal(p,s)` → `Decimal128(p, s)`
  - `Money` → `Struct<amount: Decimal128(38,6), currency: Utf8>`
  - array → `List<T>`
  - object with properties → `Struct`
  - The references add: other formats and string logical types → `Utf8`; precedence logical > format > JSON type (Q-ARROW-2); an unmappable property → `invalid_document`; list child named `item` (Q-ARROW-3).
- Meta-schema: `references/schemas/atlas-schema.schema.json` (:146). It is in the repo at `spec/protocol/references/schemas/atlas-schema.schema.json`.

### 4.4 AMBIGUOUS in §9.2 (with the reading the references take, which Phase 1 follows)

- "the first one's pointer as `target`" has no defined order. Reading: sorted by pointer, then keyword (Q-VAL-1).
- Arrow order follows `properties` order, while I-JSON says member order is meaningless and the fingerprint sorts members. So two profiles with one fingerprint can derive different Arrow schemas. Reading: document order (Q-ARROW-1). Recommendation: say so in §9.1.2.
- "1 MiB as UTF-8". Reading: measured on the RFC 8785 form (Q-PROF-12).
- "Close every request schema (every object in it)" would forbid `labels`. Reading: maps are allowed (Q-PROF-7).
- Built-ins against Rule 1. Reading: the built-ins are exempt (Q-PROF-9).
- "Ignored Atlas annotations are also required validation assertions" (R12, DESIGN-REVIEW.md:130). Resolved by §9.2.5 step 2's exception for `x-atlas-logical`.
- Whether the client validates *results* against the reader schema. §9.2 is silent: Rule 2 covers only requests. Readings: (a) results are not validated by schema, only resolved (§9.5); (b) the client checks landed batches (00-overview.md:66, "checks it").
- "Integers ... within the I-JSON range". The range is in §9.1, another reader's.

---

## 5. The transcription's reliability

### 5.1 Sources and method

- `IMG_2138.MOV` (00:16:46) covers "the overview through §6.1". `IMG_2139.MOV` (00:17:53) covers "§6.2 and ending after §9.8 and the specification's reference material" (TRANSCRIPTION-NOTES.md:5–6).
- Frames were "sampled at one- or two-second intervals"; "The initial generated draft was used only as a navigation aid. Every target file was then reviewed against its complete full-resolution source interval and rebuilt where needed." (:10, :12)
- "The source is a handheld recording rather than the original document." The risk is "small monospace punctuation and densely packed table cells" (:16).
- Diagrams are reconstructions as Mermaid (VALIDATION.md:26–43; README.md:39–41). Arrows and labels in a diagram are a reading of the frames, not text from the source.
- The review "does not revalidate the video transcription" (review/README.md:41). "The supplied specification directory contains Markdown, not the referenced protocol schemas and conformance vectors." (review/README.md:43). The schemas and vectors in `spec/protocol/references/` were written for the build (R12, Phase 1). They are not transcribed.

### 5.2 Verification status (VERIFICATION-LEDGER.md:5–40)

"Verified" means "the file was rebuilt or checked against the full-resolution laptop-screen frames for the complete source interval" (:3). **All 34 files listed are "Verified". None is "In progress" or "Pending".**

| File | Interval | Evidence | Line |
|---|---|---|---|
| `00-overview.md` | 2138 00:00:00–00:00:54 | frames at 00:00–00:50 every 10 s | :7 |
| `00-vision.md` | 2138 00:00:54–00:02:04 | every four seconds | :8 |
| `01-terms-and-conventions.md` | 2138 02:04–02:22 | 5 frames | :9 |
| `02-the-surface.md` | 2138 02:22–02:48 | | :10 |
| `02-1-capabilities.md` | 2138 02:48–03:32 | | :11 |
| `02-2-data-surface.md` | 2138 03:32–04:08 | every two seconds | :12 |
| `02-2-1-snowflake-data.md` | 2138 04:08–04:42 | every two seconds | :13 |
| `02-2-2-vendor-api-data.md` | 2138 04:42–05:08 | every two seconds | :14 |
| `02-3-resources.md` | 2138 05:08–05:32 | every two seconds; "the exact record envelope" | :15 |
| `02-4-actions.md` | 2138 05:32–05:54 | every two seconds | :16 |
| `02-5-events.md` | 2138 05:54–06:20 | every two seconds; "the exact connect handover" | :17 |
| `02-6-discovery.md` | 2138 06:20–07:06 | | :18 |
| `03-contracts.md` | 2138 07:06–10:53 | random checks incl. `atlas.lock` fingerprints | :19 |
| `04-plugging-in.md` | 2138 10:53–12:04 | | :20 |
| `05-clients.md` | 2138 12:04–14:48 | | :21 |
| `06-inside-the-gateway.md` | 2138 14:48–15:12 | | :22 |
| `06-1-front-door-and-services.md` | 2138 15:12–16:47 | | :23 |
| `06-2-access-limits-and-audit.md` | 2139 00:00–00:46 | | :24 |
| `06-3-reaching-systems.md` | 2139 00:46–01:28 | | :25 |
| `06-4-data-sources.md` | 2139 01:28–02:01 | frames 001–017 | :26 |
| `06-5-packages.md` | 2139 02:01–03:38 | frames 001–049 | :27 |
| `06-6-principles.md` | 2139 03:38–04:30 | frames 001–026 | :28 |
| `06-7-implementation-patterns.md` | 2139 04:30–05:58 | frames 001–044; "24 requirements" | :29 |
| `07-the-catalogue.md` | 2139 05:58–10:42 | frames 001–142; "27 domain headings, 138 rows and no duplicate capability ids" | :30 |
| `08-design-decisions.md` | 2139 10:42–11:59 | frames 001–039; "52 decided rows, 15 rejected alternatives, known gaps, and 52 open rows; checked unique decision/open identifiers and sampled the beginning, middle, and end" | :31 |
| `09-atlas-protocol.md` | 2139 11:59–12:23 | 24 frames | :32 |
| `09-1-conventions-and-grammar.md` | 2139 12:23–12:44 | frames 001–021 | :33 |
| `09-2-schemas.md` | 2139 12:44–13:20 | frames 001–036; "twelve Arrow mappings ... three AP-SCHEMA requirements" | :34 |
| `09-3-documents.md` | 2139 13:20–14:05 | 001–045 at 1 s; "21 document rows, 16 built-in capability rows, 24 problem codes" | :35 |
| `09-4-fingerprints.md` | 2139 14:05–14:19 | 001–015 at 1 s | :36 |
| `09-5-resolution.md` | 2139 14:19–14:55 | 001–037 at 1 s; "the 15-step algorithm, six lens operations" | :37 |
| `09-6-http-binding.md` | 2139 14:55–15:28 | 001–034 at 1 s; "21 header rows" | :38 |
| `09-7-clients.md` | 2139 15:28–16:22 | 001–054 at 1 s; "12-step call algorithm" | :39 |
| `09-8-conformance.md` | 2139 16:22–17:53 | "all 50 scenario rows" (no frame count given) | :40 |

Weaker evidence:
- The §8 ledger entry says the content was "sampled the beginning, middle, and end against the screen" (:31). Treat individual D/O cell wording as slightly less certain than the 1-second-spaced §9 files.
- The §9.8 ledger entry gives no frame numbers (:40).
- The review files, `README.md`, `TRANSCRIPTION-NOTES.md`, `VALIDATION.md` and `VERIFICATION-LEDGER.md` are not transcriptions and have no ledger rows.

### 5.3 Known transcription errors (all corrected, per VALIDATION.md)

The original draft "did **not** pass validation: 12 of 14 sampled frames contained substantive errors" (VALIDATION.md:7). The failure kinds were "missing table columns, duplicated OCR fragments, incorrect punctuation in identifiers, and diagrams flattened into unordered text".

| Frame | Material | Fix | Line |
|---|---|---|---|
| 2138 00:01:17 | Agent flow | rebuilt as Mermaid | :11 |
| 2138 00:01:40 | SDK table | "Restored the missing **Built on** column" | :12 |
| 2138 00:03:27 | "Names by family" | restored all four families | :13 |
| 2138 00:03:30 | §2.2 summary | "Corrected `bytes`, `handed off`, `ticket`, `SQL`, and the paragraph order." | :14 |
| 2138 00:04:39 | Snowflake type mapping and spike | "restored `TIMESTAMP_NTZ`, `gosnowflake`, ADBC and the missing fallback text" | :15 |
| 2138 00:06:39 | `ATLAS-DISCOVERY-001`–`010` | "Corrected requirement identifiers and `/.well-known/atlas`." | :16 |
| 2138 00:08:14 | `data.landed` event profile | reconstructed the JSON | :17 |
| 2139 00:01:52 | §6.4 DataSource | "Rebuilt the eight-operation port table and both adapter rows." | :18 |
| 2139 00:02:36 | Six-layer diagram | reconstructed L6 → L0 | :19 |
| 2139 00:08:43 | `model-portfolio` table | "Restored both capability rows and all nine columns." | :20 |
| 2139 00:09:22 | Research heading | **Pass** | :21 |
| 2139 00:10:57 | D13–D22 | "Corrected the sampled `atlas.access` identifier" | :22 |
| 2139 00:13:20 | §9.2 nullability, rules, AP-SCHEMA | **Pass** | :23 |
| 2139 00:14:07 | AP-DOC-003, §9.4 opening | "Corrected `resource.schema.json`, `ETag`, the source path, section summary and purpose." | :24 |

### 5.4 Residual oddities in my files

These may be transcription artifacts or source inconsistencies. None is marked as an error by the ledger.

- **09-2-schemas.md:18**: the `$vocabulary` value is printed inline as "`json { ... }`". The leading `json` is almost certainly a code-fence language tag that leaked into the inline code. The value is the JSON object only. Phase 1's meta-schema settles it.
- **02-3-resources.md:84**: `"owner": "3f2b…"` is truncated, either on screen or in transcription. It is not a valid `Principal` (uuid).
- **Doubled section references**: "§9.3 §9.3.4" (02-3:46, :114), "§6.1 §6.1.13" (02-3:47; 02-5:46), "§6.6 §6.6.2" (02-3:114). These are probably a link label followed by its section. Read them as §9.3.4, §6.1.13 and §6.6.2.
- **08-design-decisions.md**: D21 printed after D52 (:75); open numbers not contiguous; O41 after O43 (:153–154); O61 before O60 (:170–171). The counts (52/52) match the text and the ledger, so this is the source's order, not lost rows.
- **Source inconsistencies the review records (not transcription):**
  - §2.4 "No action is catalogued yet" against §7's preview `astronomer.deployment` (R23).
  - §2.5 "Every event type is a capability" against §7's derived events "not capabilities of their own" (07-the-catalogue.md:71; R25).
  - "Six layers" against L0–L6 (R24).
  - D9/§3.3 full transitivity against the change table (R03).
- **00-vision.md:3** says "Diagram and visible prose/tables manually reconstructed from full-resolution frames". The vision's Mermaid diagram is a reconstruction.
- **VERIFICATION-LEDGER.md:23** gives the `06-1` interval as ending at 00:16:47, one second after the stated video length 00:16:46 (TRANSCRIPTION-NOTES.md:5). Trivial.

---

## Appendix: exact names to copy

- Headers: `Atlas-Profile-Expect`, `Atlas-Deadline` (00-overview.md:62); `Atlas-Confirm` (08:62); `If-Match`, `Idempotency-Key` (02-3:24); `Operation-Location` (08:38).
- Problem codes: `invalid_request` (09-2:99; 02-3:53); `precondition_failed` 412 (02-3:27, :49); `idempotency_conflict` 409 (AGENTS.md:15); `not_granted` (00-vision.md:43; 02-5:110); `not_found` (D13); `approval_required` (02-4:48); `confirmation_required` (D40); `invalid_document` (Q-ARROW-3).
- HTTP: `202` + `Operation-Location` (D15); `412` (ATLAS-RESOURCE-001).
- Effects: `read`, `write`, `irreversible`, `external`, `declared`. Results: `record`, `collection`, `subscription`. Deliveries: `single`, `operation`, `transfer`. Executions: `gateway`, `handoff`, `client`. Stability: `upstream`, `official`. Traits: `Metered`, `Licensed`, `DynamicSchema`, `Ordered`.
- Operation states: `pending`, `running`, `succeeded`, `failed`, `cancelled`, `UNKNOWN`.
- Exact-id examples: `portfolio.holdings@v1#bcaa5bbad97cc612` (00-overview.md:61); `data.landed@v1#51c7e08b2d6f9a43` (02-5:98).
- Protocol: `atlas/1.0` (09-2:136). Vocabulary URI: `https://atlas.jhg.com/vocab/2026-10`.
- Arrow metadata keys: `atlas:profile`, `atlas:protocol`, `atlas:logical`, `atlas:key`, `atlas:aliases`.
- CLI and SDK examples: `atlas search "…" --json`, `atlas guide`, `atlas explain` (00-vision.md:38–39); `atlas call portfolio.holdings@v1:fetch --request '{"portfolios":["147HYC"],"columns":["isin","weight"]}'`; Python `from fiatlas import FIAtlas` / `atlas.call(...)`; Node `import { atlas } from '@atlas/sdk'` / `await atlas.connect()` / `client.call(...)` (ATLAS-CORE.md:105–127).
