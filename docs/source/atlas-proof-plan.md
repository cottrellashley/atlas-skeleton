# Proving the Atlas pattern in Development

A historical plan for proving the decisions the Atlas architecture rests on. It records the hypotheses and staged evidence requirements that preceded the exact wire contract. The current specification is in [`atlas-spec/`](atlas-spec), and current implementation status is in [`../HANDOFF-PROGRESS.md`](../HANDOFF-PROGRESS.md).

## In short

- **Hermetic first (2026-10-31).** Proof 0 ran locally with no outside systems; its result is [PROOF-0.md](../pass-1/PROOF-0.md). The gateway, Go client and Python face ran against a fake Snowflake behind the real adapter. Each proof below starts with a hermetic acceptance test and then calls for separate live-service evidence.
- Seven proofs, riskiest first. The spine runs on fakes. Then come Snowflake handoff and Service Bus brokering, the two most likely to change the design. Then streaming as the person, file handoff and shared state. Last, an agent completes the PM's journey.
- Each proof tests one or more load-bearing decisions on the real technology, and ends with a written result that updates the spec.
- Wire formats stay provisional until the proofs are done. The exact formats are then designed from what each proof turned out to need.
- The Go gateway lives in `gateway/`, with the shared Go SDK in `sdk/go`, following the specification's package layers. The proof plan selects a Development deployment through `ATLAS_URL` while existing clients continue using their configured gateway.
- Before starting, a few requests go out at work: one Entra consent request, plus Snowflake, Azure and hosting set-up. They're listed at the end, by owner.

## What the proofs settle, and what waits

| Load-bearing decision | Settled by proof |
|---|---|
| One front door decides every call once; services act only on its signed token | 0 |
| Large results never pass through the gateway | 1, 4 |
| Three ways to reach a system (run, hand off, broker), as the person wherever the system allows, under the credential rule | 1, 2, 3, 4 |
| When the gateway does stream, its memory stays flat | 3 |
| Clients hold no code for any capability | 0, 6 |
| Engines sit behind one data-source interface, with the contract written into the planned query | 1, 3 |
| Outside systems change only through controllers; the request path reads a snapshot | 2, 5 |

These details wait until after the proofs, and are then designed from them:

| Detail | Designed from |
|---|---|
| The profile's exact format, and what `atlas.guide` contains | Proof 6: what the agent actually needed |
| The handoff document | Proof 1 |
| The connect handover | Proof 2 |
| Stream framing (the NDJSON end line, the Arrow final batch) | Proof 3 |
| The transfer document | Proof 4 |
| Limit arithmetic and degraded mode | Proof 5 |
| Fingerprints, lenses and resolution, conformance vectors, header grammars, error codes | After all the proofs, once the formats exist |

## Ground rules for the skeleton

1. **Where.** In this repository's `gateway/`, using the shared `sdk/go` implementation and the Go architecture described by D19.
2. **Layers from day one.** L0–L6 with `gateway/layers.toml` and the layer check (§6.5.9). This is cheap now and expensive to add later.
3. **The kit.** Reuse the shared mechanisms in `sdk/go/kit`, preserving applicable notices and tests (§6.7, rule 3). The specification governs their use. Historical reference patterns differ from Atlas in these ways:
   - **Handoff:** the kit's `handoff` gives the client SQL and a ticket. The spec calls that the broker and defers it (O54).
   - **Slow queries:** the kit runs them as background operations that write result files. The spec streams by default.
   - **Streaming:** the kit uses SSE or Arrow Flight. `atlas/1.0` uses neither.
   - **Shared rate limits:** the kit keeps them in Redis. The spec uses Cosmos DB batches.
   - **Resources:** the kit wraps them in a `spec` member. The spec keeps desired fields at the top level (D49).
   - **Module registration:** the kit registers modules in `init()`, which ATLAS-PACKAGES-008 forbids.
4. **Ports first.** Every port gets an in-memory fake and a `porttest` suite. Each real adapter must pass the same suite against Development, behind an `integration` build tag.
5. **Provisional formats, real invariants.** Version the wire as `atlas/0.x`. For now a profile holds only:
   - its id, version and family
   - its interactions, each with effect, result, delivery, executions and binding
   - the request schema
   - the result columns, with units and descriptions

   The hermetic core went further than this, because contract evolution had to be proven: it has provisional fingerprints and lenses. Their exact algorithms (RFC 8785, and §9.4's projection) still wait, and so do vectors. The invariants hold anyway: every answer names the profile it was written under, clients learn everything from `atlas.describe`, and every call goes through the one pipeline and leaves one audit record.
6. **Beside, not instead.** Deploy the skeleton as its own app in Development and reach it by setting `ATLAS_URL` (§9.7.2). The Python gateway keeps serving everyone else.
7. **Every proof ends with a result.** Record what was tried, the numbers, pass or fail for each criterion, and the spec sections and decisions the result changes.

## The proofs

### Proof 0 · The spine

**Hypothesis.** One pipeline in front of one service interface, with a generic CLI, can serve a capability that was added after the CLI was built.

**Setup.** A local test environment, then Development. In-memory fakes for state, limits and audit. DuckDB as the data source (§6.4.3). The hermetic token issuer locally (§6.7.4), and Entra in Development.

**Build.**
- `edge/http` with the middleware chain of §6.7.2.
- `core/pipeline` with the nine steps of §6.1.2.
- One service with one data capability shaped like `portfolio.holdings@v1`.
- `atlas.describe` and a simple `atlas.search`.
- A CLI with `describe` and `fetch`, with flags built from the request schema and NDJSON output.

**Steps.**
1. Call the capability from the CLI.
2. Add a second capability (a profile and a service function) and redeploy only the gateway.
3. Call the new capability from the same CLI binary.

**Passes when:**
- the second capability works with no change to the CLI (ATLAS-CORE-009, ATLAS-CLI-002)
- an invalid request from someone with no grant gets `not_granted`, and from someone with the grant gets `invalid_request` with no limit reserved (ATLAS-ACCESS-009, -010)
- every call, refused or not, leaves one audit record (ATLAS-ACCESS-004)
- the layer check passes (ATLAS-PACKAGES-001)

**If it fails.** The pipeline's step contract or the service interface is wrong. Fix them before writing any real adapter.

**Status (2026-10-31).** Passed hermetically, with a fake Snowflake instead of DuckDB. Two gaps remain:

- the CLI isn't built, so ATLAS-CLI-002 is untested
- a fake issuer stands in for Entra

For Development, what's left is to deploy the same binary beside the Python gateway, sign in through Entra, and run the end-to-end suites against it. The run also proved, with fakes:

- contract evolution behind a lock
- streamed rows equal to handed-off rows
- link renewal
- the Python face's Ctrl+C, deadline, fork and exit rules

See `PROOF-0.md`.

### Proof 1 · Hand off a Snowflake result

**Hypothesis.** The gateway can run a query and give the client the chunk links for that one result. Laptops and cloud workers can download and decode the chunks with no Snowflake session, and get the same rows as the stream. The links open nothing else. This is the spike §2.2.1.8 asks for before building.

**Setup.** The Development Snowflake account, with a warehouse for the spike. A read-only role on one large Harbour table; §2.2's example is ten years of `index.constituents`, about 30 million rows. `gosnowflake` with `WithArrowBatches`. Test clients: a laptop on VPN, a laptop off VPN, a Nautilus Cloud worker and an Astronomer worker.

**Steps.**
1. In Go, run the query with Arrow batches. Record what the driver exposes for each chunk: URL, row count, sizes and required headers (on Azure, possibly an encryption key). Note whether a small vendored part of the driver is needed (§2.2.1.1).
2. Put those into a provisional handoff document. From each client location, download the chunks eight at a time.
3. Decode Snowflake's encoding (scaled `NUMBER`, struct timestamps) into the writer schema. Compare a hash of every row with the streamed result.
4. Let the links expire. Re-issue them from the persisted result, and check this uses no warehouse time.
5. Try a link on another result, with its URL edited, after it expires, and from another person's machine.

**Measure.** Which client locations can reach the links, how long links last, chunk count and sizes, download throughput, decode time and the cost of re-issuing.

**Passes when** ATLAS-SNOWFLAKE-001, -003 and -004 hold, and every question in §2.2.1.8 has an answer.

**Start from the skeleton.** Implement the Snowflake adapter's `Warehouse` interface over `gosnowflake`, in place of the fake. Then run `porttest.DataSource` and the decoder tests against real chunks.

Every line marked `ASSUMPTION` in `sdk/go/snowarrow` is checked by those tests. A difference shows up as a failing test, not as wrong data. The client's renewal and resuming are already proven against the fake.

**If it fails.** Where links can't be reached, large results go out as exports instead (`COPY INTO` plus Blob links), and §2.2.1.7's thresholds change. If the driver won't expose the links, vendor more of it or keep streaming (§2.2.1.8's own fallbacks).

### Proof 2 · Broker events through Service Bus

**Hypothesis.** Acting as itself, the gateway creates a person's subscription and gives that person Data Receiver on it. The client gets its own Service Bus token from the same sign-in and receives events directly. Revoking the grant stops delivery within the spec's bound, and no secret ever leaves the gateway.

**Setup.**
- The Development namespace (§2.5's example names `dv-eus2-fiqdataevents-sbus`), with SAS keys off.
- The gateway's identity as Role Based Access Control Administrator, limited to Data Sender and Data Receiver (§6.3.6).
- A `data-landed` topic and two test people.
- The clients' app with delegated permission for Service Bus (O42).

**Steps.**
1. Subscribe. The gateway writes the desired subscription, a controller creates it with its filter and role, and the call answers within 10 seconds or says to retry (§6.1.13). Time each part.
2. A Go client gets a Service Bus token silently from the shared sign-in, connects over AMQP and receives events from a test publisher.
3. Disconnect for ten minutes, reconnect, and check that the backlog arrives (ATLAS-EVENT-002).
4. Revoke the grant. Time how long until a new connection is refused. Check whether an already-open connection keeps receiving, and for how long.
5. Count the role assignments this creates per person and per subscription, and compare against Azure's per-subscription limit.
6. Try a Nautilus Cloud page over AMQP on WebSockets inside the firm's network (open in §2.5).

**Measure.** Time to ready, delivery latency, how long role grants and removals take to apply, how an open connection behaves after revocation, and role-assignment counts.

**Passes when** ATLAS-EVENT-002, -005 and -006 hold, and the handover carries no secret.

**If it fails.**
- If role changes are slow, or open connections outlive revocation, the controller also deletes or disables the person's subscription on revocation, and ATLAS-EVENT-006's 60-second bound is revised.
- If role counts don't scale, assign roles to groups instead.
- If pages can't connect, they poll, which D41 already allows.

### Proof 3 · Run and stream from Snowflake as the person

**Hypothesis.** The gateway exchanges the caller's token for a Snowflake one (On-Behalf-Of), runs the query as the person, and streams Arrow batches cast to the writer schema, with flat memory.

**Setup.** Snowflake's Entra integration in Development (§6.2 records On-Behalf-Of verified from Python on 2026-09-08). A test person with known Snowflake roles. The gateway capped at a small memory limit.

**Steps.**
1. Port the On-Behalf-Of exchange to Go. Query a table the person may read and one they may not, and check the refusal reaches the client as a problem.
2. Time opening a Snowflake session as the person, both per call and pooled per person. The spec doesn't say how sessions are pooled when every call runs as its caller.
3. Stream a result ten times the memory limit, as Arrow (ZSTD) and as NDJSON. Record the gateway's memory over time, and the throughput.
4. Disconnect the client mid-stream, and check that the query is cancelled at Snowflake (found by `QUERY_TAG`).
5. Run a query that outlives the exchanged token, which lasts about an hour (§6.3.5).

**Measure.** Memory over time, throughput by encoding, time to the first batch, token exchange and session set-up time (cold and warm) and cancellation time.

**Passes when** ATLAS-SNOWFLAKE-002, ATLAS-GATEWAY-005, ATLAS-PROTOCOL-003 and ATLAS-DATA-001 hold, and `QUERY_TAG` appears in query history for every call.

**From the skeleton.** It streams Arrow with ZSTD and puts `QUERY_TAG` into every statement. It does not yet cancel the warehouse query when a streaming client goes away, so step 4 is new work.

**If it fails.** If sessions as the person are too slow, pool them per person. Or run small reads as the gateway with the person recorded (today's model), and keep the person's own identity for large or sensitive reads. Either way, this settles §6.2's open question of whether data calls run as the person by default.

### Proof 4 · Hand off files through Blob storage

**Hypothesis.** The gateway issues upload and download links for exactly one blob, one right and a few minutes. The links are signed with a user delegation key, so no storage key exists anywhere. Large uploads resume after a dropped connection.

**Setup.** A Development storage account with shared keys off, and a staging container that deletes stale files. The gateway's identity as Storage Blob Data Contributor and Storage Blob Delegator (§6.3.6).

**Steps.**
1. Follow §6.3's worked example: upload a 40 GB file in 100 MB blocks, eight at a time, and drop the connection partway.
2. Commit, checking sizes and checksums.
3. Download with byte ranges.
4. Run the escalation tests of ATLAS-REACH-002.

**Passes when** ATLAS-REACH-001 and -002 hold, apart from the case in the note below.

**Note.** A SAS link, like a Snowflake chunk link, works for whoever holds it until it expires. Unless it can be bound to the person, ATLAS-REACH-002's "used … by another person, it fails every time" can't hold for either kind of link. This proof should find out what binding exists, and the requirement should be rewritten to match.

### Proof 5 · Shared state in Cosmos DB

**Hypothesis.** Limits reserved in one transactional batch of conditional patches hold exactly across replicas, with no read-modify-write and within the latency budget. The gateway keeps serving from its last good snapshot when Cosmos DB can't be reached.

**Setup.** Development Cosmos DB, with the request path's containers on their own throughput (§6.1.5). Two gateway replicas.

**Steps.**
1. Send twenty parallel calls against a budget that allows fewer. Exactly the allowed number should start (ATLAS-ACCESS-003).
2. Measure reserve-and-settle latency and request units per call, at today's load (§5: about 130,000 queries a day) and at peaks.
3. Make Cosmos DB unreachable. A fresh replica should start from last-known-good within 45 seconds (ATLAS-GATEWAY-008) and serve as §6.1.14's table says (ATLAS-GATEWAY-009).
4. Change a grant. Every replica should apply it within 60 seconds through the change feed (ATLAS-MECH-011).

**Passes when** those requirements hold, and the control path stays under 300 ms at the 99th percentile (§6.1.12).

**If it fails.** Keep exact reservation only for budgets and metered calls, and use per-replica limits for everything else, as degraded mode already does.

### Proof 6 · An agent does the PM's journey

**Hypothesis.** An agent with only the CLI and the generic skill builds what the PM asked for, within the PM's grants.

**Setup.**
- Proofs 0, 1 and 3 deployed in Development.
- Four real capabilities with provisional profiles, written with real descriptions and units: `portfolio.holdings`, `portfolio.benchmark-comparison`, `security.analytics` and `data.landed`.
- A test PM identity holding only those.
- Claude Code at work, with the generic skill.

**Steps.** Give the agent §0's words: "Show me where 147HYC's duration differs from its benchmark by sector, and tell me when new holdings land." Record every call. Then run the whole journey again from scratch.

**Passes when** §0's tests hold:
- it built what was asked
- every call was within the PM's grants
- the answer matches an independent check
- the second run took the same route (ATLAS-VISION-003)

**Then.** What the agent actually needed from the profiles and the guide is the evidence for the profile's exact format (§3.1, §9.3).

## Order

- Proof 0 first; it needs nothing from the firm.
- Then proofs 1 and 2 together. They involve different owners and systems, and they're the two most likely to change the design.
- Proof 3 alongside proof 1, since both use Snowflake. Then proofs 4 and 5, and proof 6 last.

## What to request at work before starting

| From | What | For proofs |
|---|---|---|
| Entra administrators | One admin consent request for the delegated permissions in §6.3.5 (Service Bus, Storage, Azure Resource Manager, Graph) on the clients' and the gateway's apps (O42), plus two or three test identities | 2, 3, 4, 6 |
| Entra administrators | An app registration the skeleton can use for On-Behalf-Of (the existing gateway's, or a separate one for the spike), with a federated credential for its managed identity | 3, 6 |
| Snowflake owners | A read-only test role and a spike warehouse in Development, the Entra integration for On-Behalf-Of, and read access to query history | 1, 3 |
| Infrastructure owners (`fi-infra` Terraform) | Service Bus roles with keys off; a storage account with keys off and a staging container; Cosmos DB containers with their own throughput | 2, 4, 5 |
| Platform | Somewhere to run the skeleton in Development beside the Python gateway, with its own managed identity (O24 recommends Container Apps for the first separate deployable) | All |
| Network | Whether Snowflake's result storage is reachable from laptops (on and off VPN), Nautilus Cloud and Astronomer workers | 1 |
| Harbour owners | One large table readable in Development, such as ten years of index constituents | 1, 3 |

## Open decisions to carry along

- **O21 and O22** (how the front door talks to services, and how its token is signed): not needed while services run in the gateway's process. The in-process token still carries the address, the subject and the deadline.
- **Whether data calls run as the person by default (§6.2):** settled by proof 3.
- **O54** (the Snowflake ticket): out of scope. The broker for data stays deferred.
- **D37 and D41** (events are brokered; no subscriptions over HTTP): confirmed or revised by proof 2.
- **What a cancelled command is answered with.** §9.3 has no code for a cancellation. The skeleton answers `cancelled: true` before the stream begins; after that, it ends the stream without the final batch. (`PROOF-0.md`, finding 2.)
- **What a lock holds.** ATLAS-VISION-004 asks for no discovery call at run time, so the lock must carry the profiles, or their generated types, not only exact ids. (Finding 7.)
- **The engine's lifetime on Windows.** On Linux and macOS, the engine ends itself when its parent changes. Windows needs a job object. (Finding 3.)
