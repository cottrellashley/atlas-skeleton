# Critic: what the twelve digests leave the build lacking

Written 2026-10-31, 22:15, after reading AGENTS.md, docs/pass-2/REVIEW.md and all twelve digests in full, and checking 45 cited facts against the sources.

Paths:
- SPEC = `/Users/ashleycottrell/Documents/ChatGPT/creation/atlas-spec`
- REPO = `/Users/ashleycottrell/repos/atlas-skeleton`
- REFS = `REPO/spec/protocol/references`

## Verdict

The digests are careful and almost always cited, and nearly every cited fact I checked holds. Five things weaken them as a basis for steps 4 to 11:

1. **The repository moved under them.** Between 21:46 and 22:13, files were written into the tree that the code digest says do not exist: the pipeline, the edge, the built-ins, the journey profiles, the fixtures, adapters and both composition roots. The inventory is stale.
2. **No digest read `docs/pass-2/PLAN.md`.** AGENTS.md lists it as required reading, and REVIEW.md governs it only where the two disagree. It settles several points the digests leave open (the two roots, the plug-in guide, the load SLOs, the conformance phase), and it raises others (DataSource as the port).
3. **One decision is owned by no step.** REVIEW.md step 2's split of fingerprint, document revision and routing (R01, R02) is flagged by eight digests and decided by none, and steps 4 to 11 do not include step 2.
4. **Three "ambiguities" are settled in the spec.** ATLAS-MECH-002 settles the oversize body, ATLAS-MECH-009 the bad cursor, and §9.6.1 the operation polling address. One pass-1 rule is stated inverted (#28).
5. **The step-3 mechanisms have no source shape.** Receipts, budget authority, fencing and audit facts are undefined in the sources, and no journey capability exercises them. They must be designed and recorded before step 6.

The register for recording decisions already exists: `REPO/docs/pass-2/ASSUMPTIONS.md` (A-001 to A-030). No digest mentions it. AGENTS.md:7 says "write the case down; don't guess". Every reading the build adopts belongs there as A-031 onward.

---

## Gaps, most important first

### 1. The working tree moved after the digests were written (all steps)

**What changed.** Files were created or changed between 21:46 and 22:13, and the newest change I saw was at 22:13:04. The code digest (code-api) lists most of these as "Not yet":

| Area | Files |
|---|---|
| Core | `gateway/core/pipeline/pipeline.go` (497 lines, "nine steps at written positions"); `gateway/core/builtins/{builtins,catalog,describe,guide,access}.go` |
| Edge, assembly, views | `gateway/edge/{edge,stream}.go`; `gateway/assemble/{assemble.go,journey_test.go}`; `gateway/views/views.go` (718 lines, "a curated warehouse capability is added with two files and no code") |
| Profiles and fixtures | all five journey profiles under `gateway/svc/portfolio/profiles` and `gateway/svc/datastatus/profiles`, `holdings-dates` included; `gateway/fixtures/{harbour,identities,policy}.json` |
| Protocol | `sdk/go/protocol/documents/` |
| Since 22:09 | `gateway/adapter/{entra,auditlog,policyfile}`, `gateway/porttest/authenticator.go`, `gateway/layers.toml`, `gateway/local/layers.toml`, `gateway/cmd/gateway/{main,settings,drivers}.go`, `gateway/local/cmd/gateway-local/main.go` |
| Makefile | `PRODUCT` now includes `gateway` and `gateway/local`, so code-api's defect D1 no longer holds |

**Why it matters.** Steps 4 to 8 would duplicate or overwrite this work if they were built from the digest. Some of code-api's defects may already be fixed (D1, D2, D3); others may not. ListAgents shows a busy peer session. Whoever is writing, coordinate before editing.

**To close it:**
- run `git -C REPO status --short` and `find REPO/gateway REPO/sdk/go -newermt '2026-10-31 21:30' -type f`;
- inventory the live `gateway/{assemble,core,edge,views,fixtures,svc,adapter,cmd,local}`, `sdk/go/protocol/documents` and the Makefile again, using code-api's method;
- read `REPO/docs/pass-2/ASSUMPTIONS.md` (40 lines) and the modified `REPO/docs/pass-2/PLAN.md`.

### 2. REVIEW.md step 2's split of revisions and routing (R01, R02) has no owner among steps 4 to 11 (blocks steps 6, 8, 9, 10)

**The open choice.** Every digest records it, and none decides:
- §9.4.1 keeps `interactions.*.bindings` in the fingerprint, while R02 makes routing current metadata.
- R01 says describe's caller state is private and uncached, while §9.6:95 makes describe by exact immutable.
- DR:36 asks to reconcile the catalogue's one-hour client cache (§9.7:103) with `no-store` (§9.6:95); 02-6:178 adds a catalogue ETag with 304 revalidation.

**What it decides:**
- what `atlas.describe` and `atlas.catalog` send (step 6);
- the `Cache-Control` and `ETag` headers the edge writes (step 8);
- how the Go client, the engine, the CLI and Node cache and route (steps 9 and 10).

**Constraints any reading must meet** (both scenarios checked in REFS):
- CORE-LOCK-001 asserts zero `atlas.describe` calls with `by: id`;
- CORE-DISC-002 asserts exactly one describe across three calls and a restart.

**A reading that keeps both scenarios and every fingerprint vector:**
- keep §9.4 unchanged;
- cache the contract by exact-id;
- route from the newest profile of the same `id@vN` that the client has fetched by exact, for example after `Atlas-Profile` names a newer writer (AC:164 calls this "metadata resolution");
- send describe answers `no-store`.

Decide it and record it before step 6.

**To close it, read:**
- `SPEC/09-4-fingerprints.md` §9.4.1 (L15-35);
- `SPEC/09-6-http-binding.md:95`;
- `SPEC/09-7-clients.md:88-105`;
- `SPEC/04-plugging-in.md:260-262, :283`;
- `SPEC/03-contracts.md:594-595`;
- `SPEC/02-6-discovery.md:178`;
- `SPEC/review/ATLAS-CORE.md` §7 (L154-166);
- `SPEC/review/DESIGN-REVIEW.md` R01, R02 (L30-46);
- `REFS/scenarios/CORE-LOCK-001.json`, `CORE-DISC-002.json`, `CORE-BOOT-001.json`;
- `REFS/vectors/fingerprints.json`.

### 3. The composition roots: what `cmd/gateway` wires when no Azure adapter exists (steps 4, 8)

**What PLAN.md says.**
- P2-4: `cmd/gateway` has "real adapters only: no fakes, no cgo"; `cmd/gateway-local` has the fakes and DuckDB; "the layer check enforces the split".
- P2-8: the Cosmos DB, Blob and Key Vault adapters are written against the Azure SDKs.

**The gap.** Steps 4 to 11 build no Azure adapter. REVIEW.md does not cancel P2-8 explicitly: REVIEW.md:21 keeps "fakes behind real adapters", and REVIEW.md:33 defers only the Development source, direct downloads, events and delegated identity. No digest read PLAN.md, and gateway-front leaves "which adapters are real" ambiguous.

**What step 8 must decide.** For each port, `cmd/gateway` needs a real adapter or an explicit refusal to start:
- the policy source and the audit sink;
- the idempotency and operation store;
- the budget counters;
- the cursor-signing key;
- the identity verifier.

The live tree has begun answering this (`adapter/policyfile`, `adapter/auditlog`, `adapter/entra`, `cmd/gateway/drivers.go`). Check those against P2-4 and record the decision.

**To close it, read:**
- `REPO/docs/pass-2/PLAN.md` §2 (P2-4, P2-6, P2-8, P2-9), §3 and the §4 table;
- `REPO/docs/pass-2/REVIEW.md:13, :21, :33`;
- `SPEC/08-design-decisions.md` O26 (L140);
- `SPEC/06-5-packages.md` §6.5.9-6.5.11 (L159-198);
- the gateway-front digest §5.5-5.6.

### 4. The data and provider ports: DataSource, or the tree's Warehouse and Provider (steps 4, 5, 6)

**The conflict.** §6.4.1 and PLAN.md §4 make `DataSource` the port and `Warehouse` "an adapter-internal seam". The DataSource operations are `capabilities`, `plan`, `explain`, `execute` (Arrow batches in the writer schema, with the engine's query id) and `settle`. The tree has it the other way round:
- `ports.Warehouse` is the port, with a suite. It takes a declarative query and returns rows of `[]any`, with no query id, estimate or settle.
- `ports.Provider` has `Serve`, which returns a record or a JSON row stream.

**What the ports do not carry:**
- explain's plan and estimate (ATLAS-DATA-004/007; the INTEGRATIONS:94 boundary test);
- settlement usage for R09's settlement fact (query id, rows, bytes);
- availability and health (ATLAS-GATEWAY-006, the breaker);
- cancellation by tag (ATLAS-OPERATION-003);
- operation start, get and cancel (§6.1.5).

**Why it matters.** Step 4 defines the ports and suites, step 5 implements them twice (DuckDB and gosnowflake), and step 6 consumes them.

**To close it, read:**
- `SPEC/06-4-data-sources.md` §6.4.1-6.4.3 (L52-99);
- `SPEC/06-1-front-door-and-services.md` §6.1.5 (L165-177);
- `SPEC/00-vision.md:162-168`;
- `SPEC/review/ATLAS-CORE.md` §8 (L184-188);
- `SPEC/review/INTEGRATIONS.md:60-69`;
- `REPO/docs/pass-2/PLAN.md` §4 (Snowflake row);
- the live `REPO/gateway/ports/{warehouse,provider}.go`;
- code-api §4.1, and pass1 §1.11 (pass 1's DataSource port and its suite checks).

### 5. The conformance gateway and the client runners have no step (steps 9, 10, 11)

**Why it matters.** The spec proves parity through the runner protocol against the scripted conformance gateway: AP-CLIENT-001, and §9.8:140 ("with no case skipped"). Parity is acceptance items 2 and 5, and step 11.
- REVIEW.md:21 keeps "the references as the independent check (D22)" unchanged.
- PLAN.md Phase 9 builds `sdk/go/conformance` and `cmd/conformance-gateway`: the 15 conformance capabilities, scripted steps, request recording, the loopback control API, four listeners and a token endpoint. It also builds runners for Go, the CLI and Python. Node needs one too now.
- The conformance digest describes all of this, but no digest decides who builds it or when.

**A complication.** The gateway suite (§9.8:136) has the real gateway mount `conformance.*` in `local`. CORE-CALL-001 and CORE-CALL-004 need `conformance.thing@v1:create`, a resource, while resources are deferred.

**To close it, read:**
- `REPO/docs/pass-2/PLAN.md` §1 (items 1 and 5), §3, §6 Phase 9;
- `SPEC/09-8-conformance.md` §9.8.2-9.8.5 (L35-165);
- `REFS/README.md:85-206`;
- `REFS/QUESTIONS.md` (Q-RUN-1..7, Q-SCN-1..4);
- `REFS/scenarios/fixtures/gateway.json`;
- `SPEC/08-design-decisions.md` O33 (L145);
- `REPO/sdk/go/layers.toml`, which already places `sdk/go/conformance` at layer 2.

### 6. gosnowflake itself (step 5)

**The gap.** No digest reads the driver. Step 5 needs:
- the connection settings: account, warehouse, read-only role and authentication (OBO is deferred);
- the Arrow batch path (`WithArrowBatches`) rather than `database/sql` rows; the live `cmd/gateway/drivers.go` plans a `database/sql` driver registration;
- `WithHigherPrecision` for exact decimals (ATLAS-SNOWFLAKE-004);
- `WithAsyncMode` and query ids, for settle, resume and cancel by tag;
- `QUERY_TAG` set out of band: CRITIQUE #13, and code-api D10 (`Query.Tag` is never applied);
- a statement timeout. No source names one; take it from the remaining deadline;
- `DEFAULT_NULL_ORDERING` (code-api D13);
- `TIMESTAMP_NTZ` and the cast to UTC (§2.2.1.6);
- the licence notice, and the layer rule: only `cmd/gateway` imports it, with no cgo (P2-4).

gosnowflake v1.19.1 is in the module cache. `gateway/go.mod` does not require it yet.

**To close it, read:**
- `SPEC/02-2-1-snowflake-data.md` §2.2.1.4-2.2.1.6 (L120-150) and the spike list (L158-166);
- `SPEC/06-4-data-sources.md:66-99`;
- `REPO/docs/pass-2/PLAN.md` §4 (Snowflake row) and Phase 5;
- `REFS/QUESTIONS.md` (Q-SNOW-1..4, L11, L105-107);
- `SPEC/review/INTEGRATIONS.md:62-86`;
- the source of `$(go env GOMODCACHE)/github.com/snowflakedb/gosnowflake@v1.19.1` (its doc.go, Arrow batches, query tag, async mode, higher precision);
- code-api §4.4, D10, D13, D14.

### 7. The step-3 mechanisms have no source shape and no journey capability to run on (step 6)

**What step 6 must build** ("as REVIEW.md step 3 requires"):
- R06: approval and confirmation receipts;
- R07: one budget authority per metered provider, with atomic reservations;
- R08: idempotency keys kept for the whole retention, fencing, and the uncertain outcome;
- R09: the admission, outcome and settlement facts, and the acknowledgement point;
- R17: separate start and execution deadlines, and cancellation reported as requested or confirmed.

**What is missing.** gateway-access marks these MISSING in every source:
- the receipt's format and the interaction that issues it;
- how a budget authority is declared;
- the fencing mechanism;
- the name of the uncertain state;
- the fact schemas;
- the acknowledgement point.

No journey capability is metered or changing. R07's two-caller contention test and R08's crash-after-commit test therefore need a test capability and a counting fake (ATLAS-GATEWAY-013). The digests suggest `conformance.costly` and `conformance.slow`, but nobody chooses. Design each mechanism, record it, and pick the capability it is tested on before step 6.

**To close it, read:**
- `SPEC/review/DESIGN-REVIEW.md` R06-R09 (L76-106) and R17 (L144);
- `SPEC/review/ATLAS-CORE.md` §9-§11 (L190-230) and §12.5 (L240);
- `SPEC/06-7-implementation-patterns.md` §6.7.7-6.7.8 (L178-204);
- `SPEC/06-1-front-door-and-services.md:250`;
- `REFS/schemas/operation.schema.json` and `REFS/schemas/builtins.schema.json` (audit_record, L956-1159);
- `REPO/gateway/ports/audit.go`;
- gateway-access §3-§5, and patterns §2.1-§2.3 (library defects D1-D5).

### 8. Node's own protocol implementation (step 10)

**The gap.** The clients digest lists what `@atlas/protocol` must do. No digest covers how:
- `JSON.parse` cannot refuse duplicate members, and loses `int64` and `Decimal(p,s)` values. I-JSON, R12's TypeScript half and acceptance item 5 all need exact values;
- RFC 8785 canonical form, for fingerprints and CORE-CALL-005;
- an Atlas Schema validator, with I-Regexp patterns, format assertions and the (pointer, keyword) failure order. No stock validator matches;
- RFC 9651 headers;
- the NDJSON end line;
- a credential store that meets R26;
- the §9.8.3 runner;
- a harness that reads the same `REFS/vectors/*.json`.

**The environment.** Node 25.6.1 and npm 11.9 are installed. Whether npm can fetch packages (such as `apache-arrow`) is untested. ZSTD compression is not a §9 rule, so Node can stay with JSON and NDJSON. No package layout, test runner or dependency policy is recorded.

**To close it, read:**
- `SPEC/05-clients.md` §5.3 (L287-400);
- `SPEC/09-7-clients.md` (all of it);
- `SPEC/09-8-conformance.md:144-149` (accepting a client in a new language);
- `SPEC/review/DESIGN-REVIEW.md` R12 (L126-134);
- `SPEC/review/ATLAS-CORE.md` §6 (L134-140);
- `REFS/README.md:54-63` and `REFS/vectors/`;
- `SPEC/08-design-decisions.md` D20, D22 (L43-44) and O48 (L158);
- the clients digest §6 and §8.

### 9. Step 11's load, chaos and plug-in guide are specified in PLAN.md, which no digest read (step 11)

**Load.** PLAN.md §7 gives the scenarios and their SLOs:
- p99 < 300 ms on the control-plane hot path;
- flat memory while streaming under `GOMEMLIMIT`;
- 1M-row client resolution;
- a burst against one budget across two replicas;
- 1,000 calls with half the upstream calls failing;
- overload at twice an upstream's concurrency;
- replica kills, Cosmos DB down, and switch propagation.

They run against `gateway-local` with production-sized Harbour data.

**Chaos and the guide.**
- Chaos: §1 item 4 and Phase 10 give the set, with a fault-injection test per blast-radius row (§6.1.16, ATLAS-GATEWAY-012).
- The plug-in guide is `PLUG-IN.md`: one line per system saying "exactly what plugging it in takes" (§1 item 2; the §4 column "Plugging in takes"). Phase 11 adds `RESULT.md` and `make evolution`.

**Why it matters.** REVIEW.md narrows this set: handoff rows are deferred, the Cosmos and budget rows depend on the R07 and R13 designs, and the Azure systems are out of reach. No digest records which rows survive.

**To close it, read:**
- `REPO/docs/pass-2/PLAN.md` §1 (items 2, 4, 5), §4, §6 Phases 10-11, §7;
- `REPO/docs/pass-2/REVIEW.md:11-19, :33, :44-51`;
- `SPEC/06-1-front-door-and-services.md` §6.1.16 (L284-303), ATLAS-GATEWAY-012 (L62) and the objectives at L233;
- `SPEC/08-design-decisions.md` O61 (L170);
- `git show pass-1:PROOF-0.md` (L50-57, the measured numbers).

### 10. Adding `holdings-dates` after the clients are built: the procedure and the staging (steps 7, 11)

**What AGENTS.md requires** ("Adding a capability"):
- the profile first;
- `go run ./gateway/cmd/atlas-new capability --pattern <P> <id>`;
- the family battery;
- a blast-radius row and a switch;
- the 15-question review.

ATLAS-MECH-018 requires a generator for patterns P01 to P19, whose ids are not in the transcription (PLAN.md P2-13; §5 gives a provisional list).

**What the live tree does.** It adds a capability as a profile plus a view model, and it already serves `portfolio.holdings-dates@v1` from `svc/portfolio`.

**What no digest says:**
- whether the generator is built;
- how `holdings-dates` is held back until the clients are frozen, then deployed with no client release (pass 1 used `-pin id@vN=0`);
- what the frozen clients must already support: `table · single`, `json`, and no critical traits (gate step 8, ATLAS-VERSION-009).

**To close it, read:**
- `REPO/AGENTS.md` ("Adding a capability");
- `SPEC/06-7-implementation-patterns.md` §6.7.18 (L300-309) and ATLAS-MECH-018 (L63);
- `REPO/docs/pass-2/PLAN.md` P2-12, P2-13, §5;
- `SPEC/03-contracts.md` §3.3.12 step 8 (L761-774) and ATLAS-VERSION-009 (L550);
- `SPEC/00-overview.md:91` (ATLAS-CORE-009);
- pass1 §1.4 and §1.13 (`-pin`), and journey §6.2.

---

## Contradictions, and how each is settled

| # | Between | Issue | Settled? |
|---|---|---|---|
| 1 | gateway-front §1.2 (ambiguous: "None is written") vs gateway-access §8.2 | The problem for a body over 1 MiB | **Yes.** ATLAS-MECH-002 (`06-7-implementation-patterns.md:47`, and the walk-through at :31): `invalid_request` 400, refused at the edge before step 1. `kit/problem.From` already maps `*http.MaxBytesError` to `invalid_request`. gateway-front read §6.7 only in part |
| 2 | wire §3.2 and §9 item 14 ("no registry code") vs gateway-access §8.6 and patterns §2.8 | An expired or foreign cursor | **Yes.** ATLAS-MECH-009 (`06-7:54`): `invalid_request`, target `/cursor` |
| 3 | wire §2.1 (Q-HDR-6 applied to every missing required header) vs gateway-access §4.1 and contracts §5.10 | What a missing required header answers | **In part.** A missing `Idempotency-Key` on a non-read call is `invalid_request`, target `Idempotency-Key` (`06-7:199`). A client that declares no features is served, never refused (ATLAS-VERSION-005, `03-contracts.md:546`; `09-8:136`), so an absent `Atlas-Client-Features` reads as Core. A missing `Atlas-Deadline` or `Atlas-Client` is still open: record it |
| 4 | conformance §2.1 (polls go through `atlas.operation:get`) vs wire §5.4 | How a client polls an operation | **Yes, against the conformance digest.** `09-6:16, :20, :85`: polls are `GET <Operation-Location>` and cancel is `POST <Operation-Location>:cancel`. BULK-003 and HAND-002 count `atlas.operation:get` for re-asking for links (Q-DOC-12), not for polling. The CORE-OP-001 note also settles wire's "doubling from what": the client waits 1 s (the 202's `Retry-After`), then 1 s, then 2 s |
| 5 | pass1 §5 rule 28 vs CRITIQUE.md:47 and `06-7:262-270` | The serving values | **Yes.** The rule is inverted: pass 1 lacked the spec's values. Use `:8000`, 64 KiB of header bytes, a 10 s read-header timeout, a 5-minute idle timeout, no read or write timeouts, h2c on, and on shutdown readiness false, then 5 s, then a 30 s grace. `kit/httpserver.Defaults()` (`:8080`, 16 KiB, httpserver.go:63-71) must be overridden |
| 6 | pass1 §3 and code-api §4.2 (ambiguous) vs gateway-front §2.4; the live fixtures use opaque `dev-alice` tokens | The fake identity | **Yes.** `06-7:145`: "The hermetic suite's issuer signs its own tokens and serves its own JWKS, so the real verifier runs in every test". AGENTS.md says never fake below the code you mean to test, and PLAN.md §4's Entra row agrees. The gateway binaries, end-to-end tests and client tests use RS256 JWTs verified by `kit/jwt` through a JWKS (`KeySet`). The token endpoint serves refresh (CORE-SIGNIN-002/003) and a claims challenge (CORE-SIGNIN-004). The opaque table is only for core unit tests that do not test authentication |
| 7 | gateway-access §2.1 and §6.3 (ambiguous) vs the live `gateway/fixtures/policy.json` (a `portfolio.*` grant) | May the journey be granted by family? | **No.** Rule 3 (`06-3-reaching-systems.md:58`): a system reached as the gateway's own identity is "granted by name only, never by a family grant or `everyone`". `06-3:88` says Snowflake is reached as the gateway's service identity. REVIEW.md:5 changes no requirement by implication, and R05's row does not amend rule 3. Grant each capability by exact name |
| 8 | contracts §2.1 (`explain` ambiguous) vs journey §1.1 and review §4 (in scope); the live holdings profile has `fetch` only | Does holdings offer `explain`? | **Yes, it should.** `INTEGRATIONS.md:94` makes explain sharing the execution's plan construction an integration-boundary test. ATLAS-DATA-004 (`02-2-data-surface.md:56`) withdraws `explain` only from "a capability that cannot explain itself", and a planned Snowflake read can explain itself. `07-the-catalogue.md:639` lists it. CORE:97's permission to omit an interaction does not apply here |
| 9 | wire §1.6 and review C3.11 (ambiguous) vs gateway-front, journey, pass1 and clients (in scope) | Is `atlas.access` in the first build? | **Yes, it is.** `02-6-discovery.md:42`: every refusal of a listed capability carries a `next` naming `atlas.access`. `06-6-principles.md:80` (D13): `not_granted` is "learnable through `atlas.access`". ATLAS-CORE L54 says the same |
| 10 | gateway-front §3.2 (does an in-process provider get a signed token?) vs PLAN.md P2-9 | ATLAS-GATEWAY-002 now or at extraction | **Yes, reading (a).** P2-9 puts a JWS behind a `TokenSigner` port, with a local key now. REVIEW.md's R20 row overrides P2-10's HTTP/2 transport, not P2-9, and REVIEW.md:5 governs PLAN only where they disagree. Mint and verify the token in process |
| 11 | code-api §4.1 and the live tree vs PLAN.md §4 | Which is the port: `Warehouse` or `DataSource`? | **No decision in REVIEW.md, so PLAN governs** unless a departure is recorded. See gap 4 |
| 12 | pass1 ambiguity 6: pass 1 used `upstream_failed`, pass 2's `frame.go:444-445` uses `invalid_document` | The code for an end-line row-count mismatch | **No.** `09-6:79, :81` give `upstream_failed` only for a missing end line or final batch. `chunk-end.schema.json` says `rows` is "Rows sent". The choice changes retry behaviour: `upstream_failed` is retryable and `invalid_document` is not. Decide it once for every client |
| 13 | pass1 §1.9 and journey §2.3 (ZSTD) vs §9 | Must Arrow IPC be ZSTD-compressed? | **No.** Compression appears only in `02-2-1-snowflake-data.md:80, :129` and nowhere in `09-*.md`, and §9 is normative for the wire (AGENTS.md:7). No `Atlas-Client-Features` token declares that a reader can decompress, so send uncompressed or record the decision |
| 14 | The spec with itself: `09-6:95` vs `09-7:103` and `02-6:178` | Is the catalogue `no-store`, or cached for an hour and revalidated? | **No.** This is DR:36. See gap 2 |

## Facts stated without a citation, or beyond their source

Citation discipline is good. The assertive lines without a nearby citation are almost all quotations under a citation given for the whole table or section, scope judgments, or readings labelled as such. Four go beyond their source:
- **journey §3.3** says `atlas search "holdings and weights for a portfolio"` "must return `portfolio.holdings@v1` first". HB:73-74 shows only the command sequence. "First" is the digest's inference.
- **clients §1.2, obligation 3** says to serialise refresh "in one process or across processes". AC:52 says only "Concurrent refreshes are serialized". Across processes is a reasonable inference, not the source's words.
- **gateway-front §1.2** says the problem for an oversize body is "none written", and **wire §3.2** says a bad cursor has no code. Both are false absences: see contradictions 1 and 2.
- **pass1 §5 rule 28** states its source inverted: see contradiction 5.

## Citation errors (the substance is right)

- **clients** cites R11 as `RV:76` at its lines 39, 119, 214 and 829. R11 is REVIEW.md:69; line 76 is R18.
- **journey** cites R19 as `REV:78`; it is :77. It cites the tenant-switching contract as `REV:97` (it is :98) and the minimum sunset and lock format as `REV:98` (they are :99).
- **conformance** (lines 674 and 826) and **pass1** (lines 176 and 1087) cite the lock format as `REV:98`; it is :99.

## Spot checks (45, against the source files)

| Fact (digest) | Source | Verdict |
|---|---|---|
| Only the bootstrap and describe are cacheable; everything else is `no-store` (wire) | `SPEC/09-6-http-binding.md:95` | Confirmed |
| A body without an end line is `upstream_failed` (wire, journey) | `09-6:79, :81, :101` | Confirmed |
| CLI exit codes (wire, clients) | `09-7-clients.md:118` | Confirmed |
| Retry: at most 3, `min(30, 0.5 × 2^attempt)`, jitter 25% (clients) | `09-7:124` | Confirmed; the attempt base is not fixed |
| `connect` takes url, env and token in every language (clients, pass1) | `09-7:42` | Confirmed |
| Polls are `GET <Operation-Location>`, starting at 1 s and doubling (wire) | `09-6:16, :20, :85` | Confirmed; contradicts conformance §2.1 |
| CORE-OP-001 `poll_intervals` `[1000,1000,2000]` (conformance) | `REFS/scenarios/CORE-OP-001.json` | Confirmed |
| CORE-SIGNIN-003 has a step with `nth` 3 and `times` 2 (conformance) | `REFS/scenarios/CORE-SIGNIN-003.json` | Confirmed |
| guard.json has 65 cases: 15 accepted, then the per-reason counts (conformance) | `REFS/vectors/guard.json` | Confirmed exactly |
| The fixtures hold 35 profiles and 15 built-ins; there are 50 scenarios (conformance) | `REFS/scenarios/fixtures/gateway.json`, `scenarios/` | Confirmed |
| A problem requires six members, plus `retry_after` and `estimate` by code (wire, clients) | `REFS/schemas/problem.schema.json` | Confirmed |
| ATLAS-MECH-002: a body over 1 MiB is `invalid_request` (gateway-access) | `06-7:47, :31` | Confirmed; gateway-front's "none" is wrong |
| ATLAS-MECH-009: a bad cursor is `invalid_request` at `/cursor` (gateway-access) | `06-7:54` | Confirmed; wire's "no code" is wrong |
| A missing Idempotency-Key is `invalid_request` with that target (gateway-access, patterns) | `06-7:199` | Confirmed |
| The hermetic issuer signs its own tokens and serves a JWKS (gateway-front) | `06-7:145` | Confirmed |
| Serving values `:8000`, 64 KiB, 10 s, 5 min, 5 s then 30 s (gateway-front) | `06-7:262-270`; `CRITIQUE.md:47` | Confirmed; pass1 rule 28 is inverted |
| `kit/httpserver` defaults `:8080` and 16 KiB (code-api) | `sdk/go/kit/httpserver/httpserver.go:63-71` | Confirmed; differs from the spec |
| Rule 3: grant by name only, never by family (gateway-access) | `06-3-reaching-systems.md:58`; register `:88` | Confirmed |
| Family grants reach only reads (gateway-access) | `06-2-access-limits-and-audit.md:110` | Confirmed |
| A refusal's `next` names `atlas.access` (journey) | `02-6-discovery.md:42` | Confirmed |
| CORE §12.3's first-scope list omits access and explain (review) | `review/ATLAS-CORE.md:238` | Confirmed |
| Explain uses the same plan construction as execution (review) | `review/INTEGRATIONS.md:94` | Confirmed |
| ATLAS-DATA-004 and ATLAS-DATA-007 (journey) | `02-2-data-surface.md:56-57` | Confirmed |
| ATLAS-DATA-006 is about exports, so the guard test's label is wrong (conformance) | `02-2-data-surface.md:55`; `gateway/adapter/snowflake/guard_test.go:11` | Confirmed |
| `make check` skips the gateway (code-api D1) | `REPO/Makefile:10` | Stale: `PRODUCT` now includes `gateway gateway/local` (changed 22:12) |
| `Problem.Next` and `RequestID` are `omitempty` (code-api D4) | `sdk/go/protocol/problem.go:25, :29` | Confirmed |
| The built-in profiles `$ref` an external URI (code-api D2) | `gateway/core/builtins/profiles/atlas.whoami@v1.json:56, :68` | Confirmed; whether the new `protocol/documents` now loads them is unchecked |
| `layercheck`'s `ours()` omits sqlglot (code-api D3) | `tools/layercheck/layercheck.go:225-234` | Confirmed; `gateway/layers.toml` now exists |
| `gate.MinimumSunset` is 90 days (code-api) | `sdk/go/protocol/gate/gate.go:79` | Confirmed |
| A row-count mismatch is `invalid_document` (pass1) | `sdk/go/protocol/frame/frame.go:444-445` | Confirmed |
| Pass 1's engine queue holds 64 commands (pass1) | `pass-1:sdk/go/engine/engine.go:110` | Confirmed |
| The library answers a reused key 422, or 409 while in progress (patterns) | `scratchpad/cgp/repo/patterns/idempotency/idempotency.go:161-169` | Confirmed |
| The readiness check's one-sentence heuristic (contracts) | `tools/spec/atlasref/profilecheck.py:400-404` | Confirmed |
| The catalogue rows for the journey (journey) | `07-the-catalogue.md:634, :635, :639, :640` | Confirmed |
| Only holdings and trades are chunked; search is `fetch` only (journey) | `review/CAPABILITY-HANDBOOK.md:233` | Confirmed |
| D17's evaluation order (rest) | `08-design-decisions.md:40` | Confirmed |
| Takeover releases the idempotency key (gateway-front, review) | `06-1-front-door-and-services.md:250` | Confirmed (overridden by R08) |
| A listed capability the caller lacks is `not_granted` (gateway-front) | `06-6-principles.md:80` | Confirmed |
| The cancel answer is "`deadline_exceeded` or the cancellation" (clients, pass1) | `05-clients.md:237` | Confirmed |
| No case may be skipped at a level (conformance) | `09-8-conformance.md:140` | Confirmed |
| `Atlas-Limit-Remaining` on every metered answer (gateway-access) | `09-6:43` | Confirmed |
| `Atlas-Client-Features`: five members at :32, `handoff` added at :52 (wire) | `09-6:32, :52` | Confirmed |
| ZSTD appears only in §2.2.1 (pass1) | `02-2-1-snowflake-data.md:80, :129`; no hit in `09-*.md` | Confirmed |
| CORE-LOCK-001 allows no describe by id; CORE-DISC-002 one describe in all (clients, conformance) | `REFS/scenarios/CORE-LOCK-001.json`, `CORE-DISC-002.json` | Confirmed |
| The REVIEW.md line numbers for R11, R19, the switching contract and the lock format | `REPO/docs/pass-2/REVIEW.md:69, :77, :98, :99` | Off by one in clients, journey, conformance and pass1 (see above) |
