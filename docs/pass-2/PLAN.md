# Pass 2: the plan

Pass 2 rebuilds the skeleton from the rewritten spec (230 requirements) as if it were the last pass. By the end, each real system plugs in through configuration and an adapter that is already written and tested.

[CRITIQUE.md](CRITIQUE.md) lists the 36 defects in pass 1, of which 14 are blockers. This plan fixes all of them.

## 1. What "done" means

Pass 2 is done when all of these are true:

1. **Every surface is proven end to end, in every client.**
   - The surfaces are data, resources, actions and events, plus all 20 built-ins.
   - There is at least one real exemplar per pattern (§5), called from the Go SDK, the CLI and Python.
   - Each family's battery and the conformance scenarios pass.
2. **Every external system sits behind a port**, with three things around it:
   - a hermetic fake that passes the port's suite
   - a real adapter, written against the vendor's SDK and tested as far as this machine allows
   - a line in [PLUG-IN.md](PLUG-IN.md) (written at the end) saying exactly what plugging it in takes
3. **Every one of the 230 requirements maps to a test**, or is marked as checked by review (`make requirements`).
4. **It holds under load and failure:**
   - load tests with SLOs as pass criteria
   - a fault-injection test per blast-radius row
   - replica kills, a Cosmos DB outage, switch propagation, and N−1 reading N's store
5. **One command proves it:** `make check`, which runs gofmt, vet, staticcheck and the custom analyzers, `-race` with goleak, the layer check, `go mod tidy -diff`, the vectors, the batteries and the scenarios. `make load` and `make evolution` run on their own.

## 2. Decisions for pass 2

| # | Decision | Why |
|---|---|---|
| P2-1 | Rebuild in place in `~/repos/atlas-skeleton`, after committing pass 1 as the baseline | One history, and pass 1 is kept |
| P2-2 | §9 is normative. `spec/protocol/references/` holds the JSON Schemas of all 21 documents, the grammar, the vectors and the scenarios. They are authored from the text in Python (`tools/spec/`), with its own reference implementations (`rfc8785`, `jsonschema` plus the profile's rules, `pyarrow`, Python `sqlglot` 30.27.0), never from Go's output | D22: the vectors are an independent check, so Go can't be both the implementation and the oracle |
| P2-3 | Go document types are generated from those schemas (`tools/schemagen`) | §6.5.6: never written by hand |
| P2-4 | Two composition roots. `cmd/gateway` has real adapters only: no fakes, no cgo. `cmd/gateway-local` has the fakes, DuckDB, Azurite and the fake upstreams. The layer check enforces the split | O26: only the local root runs fakes. Production stays pure Go |
| P2-5 | `sdk/go` stays pure Go. CI cross-compiles `atlas-engine` for the five wheel platforms | GO-002 |
| P2-6 | **The fake Snowflake is DuckDB running the adapter's own SQL.** The Snowflake adapter plans a typed tree with sqlglot-go and renders it in the Snowflake dialect. The fake warehouse transpiles that statement to DuckDB, runs it, and serves `snowflake-arrow/1` chunks from fake result storage behind signed, expiring links | Planning, the guard, chunking, decoding, renewal, resume and settlement are all the adapter's real code. Only the warehouse is fake |
| P2-7 | **Events, hermetically:** an in-memory broker with Service Bus semantics, and a client connector that exists only in builds tagged `atlas_hermetic` and is accepted only when the bootstrap's environment is `local` or `conformance`, the same rule as `emulator`. The real path is AMQP through `azservicebus`. With Docker running, the official Service Bus emulator is used too | No AMQP fake to write. Release clients carry no test protocol |
| P2-8 | The Cosmos DB, Blob, Service Bus, Entra, Key Vault and Log Analytics adapters are written now against the Azure SDKs. Blob is tested against Azurite, run here with `npx`. Cosmos DB and Service Bus are tested against their emulators when Docker runs (build tag `integration`); otherwise they are compiled and unit-tested | Plugging in is configuration, not new code |
| P2-9 | The token from the front door to a service is a JWS behind a `TokenSigner` port: a local key now, Entra in the target (O22). It carries the address, subject, request id, deadline, idempotency key and trace | GATEWAY-002. Replaceable without touching services |
| P2-10 | The service protocol runs over HTTP/2 with JSON and Arrow (O21). Every service's battery runs in process and over loopback | GATEWAY-003: proves every service movable |
| P2-11 | Telemetry uses the OpenTelemetry API and slog. Redaction happens in one place. The exporter is configured only in the deployables | §6.1.12, §6.7.16 |
| P2-12 | Exemplars per pattern, not all 138 capabilities. The remaining capabilities come from `cmd/atlas-new` once each pattern is proven | Prove each shape once. The generator makes the rest cheap and the same |
| P2-13 | The spec's pattern ids (P01–P19, in `plan/exemplars.md`) aren't in the transcription, so §5 defines a provisional list, to be reconciled at work | The spec's own patterns aren't readable here |

## 3. The shape

```
AGENTS.md  Makefile  go.work (sdk/go, gateway, clients/cli)
spec/protocol/references/   schemas/ · grammar.abnf · vectors/ · scenarios/      ← tools/spec (Python)
sdk/go/                     package atlas: the client (§9.7)
  protocol/                 L0: generated documents, grammar, Structured Fields, I-JSON, Atlas Schema validator,
                            JCS + fingerprints, Arrow derivation, resolution (rows, columns), units, problems,
                            framing, classify (the gate), lint, readiness
  snowarrow/                snowflake-arrow/1
  kit/                      problem serve jwt retry reconcile ops idem page hot cache conc limit resource
  engine/ cmd/atlas-engine  runner protocol + Arrow pipe + cancel
  conformance/ cmd/conformance-gateway
gateway/
  ports/ porttest/ fakes/   L1: every port, its suite, a fake that passes it (fault hooks, counters)
  core/                     L2: pipeline access limits operations registry catalog context switches builtins
                            guide controllers audit resilience
  adapter/                  L3: snowflake (+ duckwh, local only) cosmos storage servicebus monitor entra keyvault vendorhttp
  svc/                      L4: harbour data snowflake creditflow yieldbook azure astronomer nautilus event
  edge/                     L5: http (binding router) mcp service
  cmd/                      L6: gateway gateway-local service-host worker events-publisher atlas-new
  fakes/upstream/           fake vendor and platform APIs (CreditFlow, Yield Book, Bloomberg, Resource Graph, Astronomer)
  layers.toml tools/layercheck tools/vet
clients/cli/                atlas
clients/python/             fiatlas, one wheel per platform with atlas-engine inside
tools/load/                 load generator, scenarios, report
```

## 4. How each real system plugs in

| System | Port(s) | Hermetic now | Real adapter, written in pass 2 | Proven now by | Plugging in takes |
|---|---|---|---|---|---|
| Snowflake | `DataSource` (adapter-internal `Warehouse` seam) | DuckDB + sqlglot-go; fake result storage | `gosnowflake`: async, Arrow batches, higher precision, query tag, fetch by id, query history, cancel by tag | The port suite and every Snowflake battery, run against DuckDB | Account, warehouse, read-only role, OBO scope; the O36 spike for chunk links (handoff ships with its switch off until then) |
| Cosmos DB | `Store`, `Limits`, `Operations`, `Idempotency`, `Lease`, `ChangeFeed`, `ServiceRegistry` | Memory fakes passing the suites | `azcosmos`: transactional batches of conditional patches, ETags, TTL, change feed, leases | Suites against fakes; against the emulator when Docker runs | Endpoint, managed identity, containers and partition keys (`fi-infra`) |
| Blob storage | `TransferLinks`, `BlobStore` (LKG, exports), `AuditSink` (WORM append) | Azurite | `azblob`: user delegation SAS, append blobs, immutability | Suites against Azurite | Account, containers, immutability policy, roles |
| Service Bus | `EventBus` (entities, rights, publish) | In-memory broker | `azservicebus`, `armservicebus`, `armauthorization` | Suites against the fake; the emulator when Docker runs | Namespace, scoped RBAC admin, SAS off |
| Entra ID | `IdentityVerifier`, `OnBehalfOf`, `TokenSigner` | Fake issuer: RS256, JWKS, OIDC discovery, device code, refresh, claims challenge | MSAL Go (OBO), Graph group overage | The real `kit/jwt` verifier in every test | Tenant, app registrations, consents (O42) |
| Log Analytics | `AuditReader` (and shipping) | Reads back from the audit blobs | `azlogs`: ingestion and query | Suite | DCR/DCE, workspace |
| Key Vault | `Secrets` | Env/file fake | `azsecrets` | Suite | Vault, managed identity |
| Vendors | Through `adapter/vendorhttp` (retry at one hop, OAuth2, shared throttle, redaction) | Fake vendor servers: quotas, latency, 503s, slow jobs | Mappings in `svc/<vendor>` | Batteries and fault injection | Credentials in Key Vault, base URLs, quotas |
| OTLP | — | stdout / no-op | The OTel SDK in the deployables | — | Collector endpoint |

## 5. The exemplars

Each pattern gets one capability (two where marked) from the spec's catalogue (§7). The generator learns each pattern from its exemplar.

| Pattern | Exemplar | Proves |
|---|---|---|
| P01 Snowflake table, streamed and handed off | `portfolio.holdings@v1` (`history`†), `index.constituents@v1` (`source`†) | AsOf, Projectable, Filterable, Licensed; the contract in SQL; handoff, renewal and resume; settlement; evolution with a lock |
| P02 Lookup behind references | `portfolio.list@v1`, `index.list@v1` | `x-atlas-reference`, CLI completion |
| P03 Free SQL | `snowflake.query@v1`, `snowflake.schema@v1` | The guard; DynamicSchema; named grant; curated-first house rule |
| P04 Freshness and its event | `data.status@v1` + `data.landed@v1` | `meaning.history`; polling fallback |
| P05 Vendor table | `creditflow.deals@v1` | Upstream; Metered; shared throttle; breaker; drift report |
| P06 Vendor long call | `yieldbook.scenarios@v1` | Operation delivery; leases; cancel; budget; confirmation |
| P07 Platform dynamic table | `azure.resource-graph@v1` | Skip-token paging; per-caller throttle; granted by name only |
| P08 Content | `astronomer.log@v1` | Content in memory, or on disk with a manifest |
| P09 Platform table | `astronomer.runs@v1` | Gateway identity, caller recorded |
| P10 Resource with versions | `nautilus.dashboard@v1` and `@v2` | The envelope; ETags and masks; publish by transfer; watch; one stored form served at two versions (§3.3.5's worked example) |
| P11 Converged resource | `event.topic`, `event.subscription@v1` | Controllers to Service Bus entities and roles; finalizers; `ready` within 10 s |
| P12 Action | `astronomer.deployment@v1` | Plan, approve, restart; receipts; Production policy |
| P13 Event type | `data.landed@v1`, `atlas.catalog.changed@v1` | Connect handover; CloudEvents; dedupe; resolution of old events |
| P14 Export | `index.constituents@v1:export` | `COPY INTO` Parquet (DuckDB `COPY TO`); transfer parts with checksums |
| P15 Built-ins | All 20 `atlas.*` | Working and access views; guide; usage; admin resources; audit |
| P16 Conformance | All 15 `conformance.*` | Mounted only in `local` and `conformance` |

## 6. Phases

The work is built bottom-up, as §6.5.11 orders it. Each phase ends green on `make check`, with a short report, so you can redirect at any boundary. The sizes are relative.

| Phase | Builds | Exit (requirements proven) | Size |
|---|---|---|---|
| **0 Ground** | Commit pass 1; the layout; `AGENTS.md` (constitution, review, layers); `Makefile`; analyzers in `tools/vet` (no `init` registration, no bare `go`, no `os.Getenv` outside configuration, no `http.Error`); staticcheck; goleak; the requirement matrix tool | PACKAGES-004/008, MECH-012, PRINCIPLES-002 tooling | S |
| **1 Protocol** | `references/` (Python, independent) and `sdk/go/protocol`: generated documents, I-JSON, grammars, RFC 9651, the Atlas Schema validator and meta-schema, JCS fingerprints, Arrow derivation, §9.5 resolution as rows and columns, units, problems, framing, the §3.3 classifier, lint, the agent-readiness check, `snowarrow` | AP-CONV, AP-SCHEMA, AP-FP, AP-RESOLVE, AP-HTTP, AP-DOC-003; TYPES-001–003; RESOLVE-001–004 (1M-row benchmark); fuzzers on every parser | L |
| **2 Kit** | The 13 `kit` packages, adapted from the patterns repository with notices and tests, at §6.7's values | MECH-005/006/008/009/013/016; RECONCILE-001–006 (unit); goleak clean | M |
| **3 Ports** | Every port of §6.5.3, plus `Lease`, `ChangeFeed`, `BlobStore` and `Clock`. A suite per port: concurrency, burst exactness, store semantics, takeover. Fakes with fault hooks and counters. The fake issuer | PACKAGES-002; ACCESS-003 at the port; RESOURCE-001–007 at the port | M |
| **4 Core** | The pipeline (nine steps, spans, the refusing step); access; limits (reserve, settle, inherit, budgets, caps, confirmation); operations (leases, takeover, cancel by tag); registry and generations; the catalogue controller (admission, a checked snapshot, LKG, change feed); switches; degraded mode; house rules; all 20 built-ins with the guide writer; the settlement, publications, event-entities and event-rights controllers; audit with group commit; bulkheads and shedding | ACCESS-001–010; DISCOVERY-001–010; GATEWAY-001/006/008–014; OPERATION-001–003; MECH-003/007/010/011/017; VERSION-007/009; PROFILE-001–005; a fault-injection test per blast-radius row | L |
| **5 Adapters** | Snowflake: the sqlglot-go planner (alias, cast, UTC, enum conformance, pushdown); the guard and its vectors; the tag out of band; `Warehouse` on gosnowflake and on DuckDB; handoff, resume, settle, export. Then Cosmos DB, Blob, Service Bus, Log Analytics, Entra, Key Vault and `vendorhttp` | PACKAGES-002 against fakes and emulators; SOURCE-001; DATA-004 (explain equals fetch byte for byte); SNOWFLAKE-001/003/004 | L |
| **6 Services** | Each exemplar: its profile files (agent-ready), its service, its fake upstream, and its family battery, run in process and over loopback | GATEWAY-003/007; CAP-001–006; DATA-001–007; RESOURCE-001–007; ACTION-001–003; EVENT-001–006; VENDOR-001/002; CATALOGUE-001 | L |
| **7 Edges and roots** | `edge/http`: binding router, every delivery's framing, `Accept`, caching, CORS. `edge/mcp`: the fixed tools. `edge/service`. All six deployables. The generator with every pattern. Generated OpenAPI and AsyncAPI. An image with one binary and the CA certificates | MECH-001/002/015/016; PACKAGES-005–007; BINDING-001–003; MECH-018 | M |
| **8 Clients** | `atlas` (§9.7 in full); `atlas-engine` (the exact runner protocol); `clients/cli` (commands from profiles at run time, exit codes, sign-in, locks); `fiatlas` (the §9.7.1 names, refs, `read_as` for pandas, polars, Arrow and DuckDB, `AtlasProblem` per code, async, operations, events, export, upload, the core extras); wheels per platform, installed and tested with no Go present | CLIENTS-001–006; GO-001/002; CLI-001/002; PY-001–007; AP-CLIENT-001–005; AP-EVENTS-001/002; CORE-009 end to end | L |
| **9 Conformance** | `sdk/go/conformance` (the 15 capabilities, scripted scenarios, request recording, the loopback control API); all 50 scenarios; runners for Go, the CLI and Python; the gateway suite with the reference runner against the real local root | AP-001/002/003 (Core, by a client written from §9 alone if time allows); AP-CONF-001/002; VERSION-005/006 | M |
| **10 Load and chaos** | `tools/load` (§7), replica kills, a Cosmos DB outage, slow and failing upstreams | §7's SLOs; SNOWFLAKE-002; GATEWAY-005; ACCESS-003 under load; MECH-006 | M |
| **11 Record** | Security tests (ACCESS-006–008, REACH-002 per credential kind, MECH-014); `make evolution`; requirement coverage; review answers per service; `RESULT.md`; `PLUG-IN.md`; the assumptions register | VERSION-010; REACH-001–004; PRINCIPLES-001; every requirement mapped | M |

## 7. Load tests

Each scenario has an SLO as its pass criterion. The tests run against `gateway-local`, with DuckDB holding production-sized synthetic Harbour data: 30 million constituent rows, and a holdings table at the scale the spec measured.

| Scenario | Measures | Pass |
|---|---|---|
| The control-plane hot path (whoami, catalog, describe, small fetch) | p50, p99, at a rising arrival rate | p99 < 300 ms, excluding the service's own time (§6.1.12) |
| A 5-row fetch at high RPS (half of all queries return 5 rows or fewer) | Throughput per core; allocations per call | Recorded; no regression between releases |
| 3k rows streamed, Arrow and NDJSON | Throughput; time to first batch | Recorded |
| 30M rows handed off; 30M rows streamed under `GOMEMLIMIT` | Gateway memory over time | Memory flat (SNOWFLAKE-002, GATEWAY-005) |
| 1M-row client resolution | Time against column size | Proportional (RESOLVE-004) |
| A parallel burst against one budget, across two replicas | Calls started | Never more than the budget allows (ACCESS-003) |
| 1,000 calls with half the upstream calls failing | Retries | At most 1 in 10 per minute (MECH-006) |
| Overload at twice an upstream's concurrency | Shedding | 503 with `Retry-After`; other domains unaffected |
| A replica killed mid-request and mid-operation | Recovery | The retry recovers it (GATEWAY-001); takeover within 1 minute (OPERATION-001); controllers move within 20 s (RECONCILE-006) |
| Cosmos DB down | Degraded mode | Ready within 45 s from LKG; the §6.1.14 table holds (GATEWAY-008/009) |
| A switch turned off | Propagation | Every replica within 60 s (GATEWAY-011) |

## 8. Following the patterns repository

`AGENTS.md` carries the constitution, the 15-question review and the patterns' 12 mechanism rules. Each rule is mapped to the package that applies it and the test that checks it. Where the patterns repository and the spec differ, the spec wins:

- every interaction is a POST to its binding
- there is no `spec` member
- states are lowercase
- registration happens in one list, not `init`
- authentication, limits and idempotency are pipeline steps, not middleware
- the codes are 409 and 412

Every service is admitted only after its review answers are recorded (PRINCIPLES-001).

## 9. Running it

- **Split the references into their own session.** Writing the schemas, vectors and scenarios is independent of the Go code, and the spec requires that independence. A separate session, in a disjoint folder (`spec/`, `tools/spec/`), writes them from the text while this session builds Phase 0 and the Go protocol against them. Mismatches are settled by the spec's text.
- **Split more once the ports are frozen** (end of Phase 3): the Azure adapters, the CLI and the Python face can each run in a session of their own.
- **Stop or redirect at any phase boundary.** Each phase ends with a short report.

## 10. Decisions for you

Starting means accepting these defaults:

1. **Commit pass 1 now** as the baseline. *Default: yes.*
2. **Docker.** Starting Docker Desktop lets the Cosmos DB and Service Bus adapters be tested against their emulators. *Default: build without it, and run those tests when it is up.*
3. **Node SDK** (TypeScript) is out of scope. *Default: yes.*
4. **Exemplars per pattern** (§5), not all 138 capabilities. *Default: yes.*
5. **The hermetic event connector** behind a build tag (P2-7). *Default: yes.*
