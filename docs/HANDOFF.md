# Atlas: the handoff plan

**As of 2026-11-01, branch `pass-2`, at the commit that adds this file.** This plan is written for the people who will finish Atlas and hand it to a company: us, and then the company's engineers working with us. It says:
- what is being handed off;
- where the line runs between Atlas and the company;
- what the repository holds today;
- what still has to be done, in order, with the files to change and the tests that prove each step.

It adopts the core handoff plan and the peer review of 2026-11-01 (section 9, *External sources*), and turns them into work on this repository. File links are relative to this file. Line numbers are those of this commit. They will move as the work proceeds, so each link also names the function or type it points at.

---

## 1. The goal

A company receives this repository and, with us, builds its own integrations into it. An engineer adds a service integration without changing Atlas's shared framework. Agents then:
- discover what each person may do;
- understand what a capability does and what it returns;
- try it from the CLI;
- use exactly the same contract from Python or Node in the programs they build.

**The company supplies:**
- adapters to its own systems (warehouse, APIs, identity provider, audit store);
- providers with its business logic;
- capability definitions;
- the kinds of data scope its policy uses (`workspace`, `desk`, `region`);
- policy, secrets references and deployment wiring.

**Atlas supplies:**
- identity;
- access and scope enforcement;
- the call pipeline and its lifecycle;
- audit;
- the protocol and the compatibility gate;
- discovery and guides;
- the CLI, Python and Node clients.

A new connector inherits all of these. It never re-implements one.

There are two release gates. **Gate A is the first handoff. Gate B completes it.**
- **Gate A, read-only handoff.** A warehouse capability and an HTTP capability, at least one scoped by a new company access dimension, work through all three clients. No change is made to the framework or the clients.
- **Gate B, changing and long-running work.** A job-triggering capability works through shared approval, idempotency, durable operation and audit mechanisms. A metered one works through a declared budget authority. Again, the framework and the clients are not changed.

## 2. The boundary

During each gate's exercise, the Atlas side is frozen. The company changes only its side. The test of the boundary is mechanical: hash the Atlas side before and after the exercise, and the hashes must match (section 5, C4 and C6).

| The company adds or changes | Atlas owns: unchanged during an integration exercise |
|---|---|
| Domain services: `svc/<domain>/` with its profiles, view models and history, or its own provider code | The registry, access engine, pipeline and built-ins: [`gateway/core/`](../gateway/core) |
| Adapters behind the supported ports, in its own packages or modules | The ports and their suites: [`gateway/ports/`](../gateway/ports), [`gateway/porttest/`](../gateway/porttest) |
| Capability profiles, history, examples and guidance | The protocol, schema validation, compatibility gate and resolution: [`sdk/go/protocol/`](../sdk/go/protocol) |
| Registered access-dimension definitions (once C1 exists) and policy | The view engine and the SQL planner and guard: [`gateway/views/`](../gateway/views), [`gateway/adapter/snowflake/`](../gateway/adapter/snowflake), [`sqlglot/`](../sqlglot) |
| Its composition root: a module like [`gateway/cmd/gateway/`](../gateway/cmd/gateway) that imports its modules and wires them | The HTTP edge: [`gateway/edge/`](../gateway/edge). The assembly: [`gateway/assemble/`](../gateway/assemble) |
| Settings, secret references, infrastructure | The Go client, the engine and the CLI: [`sdk/go/`](../sdk/go), [`clients/cli/`](../clients/cli). The Python and Node clients: [`clients/python/`](../clients/python), [`clients/node/`](../clients/node). The kit: [`sdk/go/kit/`](../sdk/go/kit) |

Registration is compiled and explicit: a composition root imports packages and lists them. No runtime plug-in loader is planned or needed. What the handoff requires is a supported public way to do that registration, without copying assembly internals or patching shared code. Section 4 explains why that does not exist yet.

## 3. What the repository holds today

### 3.1 Modules

Each directory with a `go.mod` is its own module. In-repo modules require each other at `v0.0.0`, with a `replace` to their directory, and all are listed in [`go.work`](../go.work). [`docs/pass-2/PLUGGING-IN.md`](pass-2/PLUGGING-IN.md) has the full table. In short:

| Module | Holds |
|---|---|
| [`sdk/go`](../sdk/go) | The protocol (grammar, Structured Fields, schemas, profiles, the gate, resolution, framing, the 21 protocol documents); the kit, from cloud-gateway-patterns-go ([`sdk/go/kit/PROVENANCE.md`](../sdk/go/kit/PROVENANCE.md)); the Go client `atlas`; the engine (`sdk/go/engine`, `cmd/atlas-engine`) that the Python client drives |
| [`sqlglot`](../sqlglot) | The in-repo port of SQLGlot ([`sqlglot/PROVENANCE.md`](../sqlglot/PROVENANCE.md)) |
| [`gateway`](../gateway) | Ports, fakes and port suites; the core; the view engine; adapters with no vendor SDK; domain services; the edge; the assembly. No vendor SDK and no cgo |
| [`gateway/adapter/snowflake/gosf`](../gateway/adapter/snowflake/gosf) | The Snowflake engine over gosnowflake v1.19.1 |
| [`gateway/cmd/gateway`](../gateway/cmd/gateway) | The production composition root. `make release` builds it with `CGO_ENABLED=0 -tags minicore_disabled`, for linux/amd64 and arm64 |
| [`gateway/local`](../gateway/local) | The development gateway: DuckDB runs the Snowflake adapter's own statements, over the fixture |
| [`clients/cli`](../clients/cli) | The CLI, `atlas` |
| [`tools`](../tools) | atlasvet, the layer check, the private-organisation guard, the requirement matrix |

The other two clients are not Go modules. `clients/python` is `fiatlas`, over the engine, and `clients/node` is `@atlas/sdk`, TypeScript with no dependencies.

### 3.2 How a call flows

1. **Edge.** [`gateway/edge/`](../gateway/edge) receives a POST at a binding address. It bounds the body and the headers, reads the deadline and the trace, and hands the call to the pipeline. It writes the answer as JSON, NDJSON or Arrow.
2. **Pipeline.** [`gateway/core/pipeline/pipeline.go`](../gateway/core/pipeline/pipeline.go) runs ATLAS-CORE §8's steps:
   - authenticate (the `Authenticator` port);
   - resolve the address against the registry ([`gateway/core/registry/`](../gateway/core/registry));
   - check visibility, denies and grants ([`gateway/core/access/`](../gateway/core/access));
   - validate and normalise the request under its own contract;
   - check the request's data scope;
   - apply limits and the upstream's bulkhead;
   - record the admission (the `AuditSink` port), then dispatch to the `Provider`.

   It holds the answer to the contract and to the scope ([`rows.go`](../gateway/core/pipeline/rows.go)). It records the outcome, and settles from the source's own record.
3. **Provider.** A data capability's provider is the view engine ([`gateway/views/`](../gateway/views)). It turns the request into a declarative read (`ports.Query`) and a writer schema, then plans and executes it through the `DataSource` port.
4. **DataSource.** [`gateway/adapter/snowflake/source.go`](../gateway/adapter/snowflake/source.go) plans one Snowflake statement through sqlglot. Every column is aliased and cast, every value bound, and the read-only guard holds it. It executes on an `Engine`: [`gosf`](../gateway/adapter/snowflake/gosf) in production, [`local/duckwh`](../gateway/local/duckwh) in development. It answers Arrow batches in the writer schema.
5. **Built-ins** are providers too: [`gateway/core/builtins/`](../gateway/core/builtins) serves whoami, catalog, search, describe, guide, access, setup, usage and clients.

### 3.3 The extension points that exist

The [`ports` package](../gateway/ports) defines each port. Each has a suite in [`porttest`](../gateway/porttest), run by every implementation.

| Port | Implementations | Suite |
|---|---|---|
| [`DataSource`](../gateway/ports/datasource.go) (plan, explain, execute, settle) | `snowflake.Source` over gosf or DuckDB; [`fakes.DataSource`](../gateway/fakes/datasource.go) | [`porttest.DataSource`](../gateway/porttest/datasource.go) |
| [`Authenticator`](../gateway/ports/identity.go) | [`adapter/entra`](../gateway/adapter/entra) (Entra ID tokens through `kit/jwt`); [`fakes.Tokens`](../gateway/fakes/tokens.go) | [`porttest.Authenticator`](../gateway/porttest/authenticator.go) |
| [`PolicySource`](../gateway/ports/policy.go) | [`adapter/policyfile`](../gateway/adapter/policyfile); [`fakes.Policy`](../gateway/fakes/policy.go) | [`porttest.PolicySource`](../gateway/porttest/policy.go) |
| [`AuditSink`](../gateway/ports/audit.go) | [`adapter/auditlog`](../gateway/adapter/auditlog) (JSON lines); [`fakes.Audit`](../gateway/fakes/audit.go) | [`porttest.AuditSink`](../gateway/porttest/audit.go) |
| [`Provider`](../gateway/ports/provider.go) | [`views.Provider`](../gateway/views/views.go); the built-ins | [`porttest.Provider`](../gateway/porttest/provider.go) |

### 3.4 Adding a capability today

Only warehouse capabilities have a supported path. A capability takes three kinds of file, and no Go code:
- `svc/<domain>/profiles/<id>@v<N>.json`, its contract;
- `svc/<domain>/views/<id>@v<N>.json`, where its answer comes from;
- `svc/<domain>/history/<id>@v<N>/`, the profiles it replaced.

The examples are in [`gateway/svc/portfolio/`](../gateway/svc/portfolio) and [`gateway/svc/datastatus/`](../gateway/svc/datastatus).

A domain registers in the composition root's one list: `domains` in [`gateway/cmd/gateway/main.go:58`](../gateway/cmd/gateway/main.go#L58). At registration, every profile passes the gate against its version's history (R03), and every view model is checked against its profile. A capability that fails stops the start. The development gateway also adds a directory of such files while it runs (`gateway-local -catalog DIR`). The acceptance test does exactly that.

### 3.5 What is proven, and how

| Claim | Evidence | Level |
|---|---|---|
| The protocol matches §9 | Every vector in [`spec/protocol/references/vectors`](../spec/protocol/references/vectors), run in `make check` | Hermetic tests, against independent references |
| The journey works end to end through three clients | [`tests/e2e/acceptance.py`](../tests/e2e/acceptance.py) and `make e2e`: the CLI, Python and Node give identical rows. A narrower scope is refused or narrowed. A capability added later works. Exact decimals are checked against the fixture independently | Hermetic, against DuckDB |
| Each port's contract | The `porttest` suites, on the fakes and on the adapters | Hermetic |
| The SQL guard | 65 guard vectors | Hermetic |
| The production build is pure Go | `make release`; `tools/layercheck`'s `[[pure]]` rule | Built here |
| Snowflake (gosf) | Unit tests, with synthetic Arrow in Snowflake's encodings and a fake endpoint | Compiled and unit-tested. **Never run against Snowflake** |
| Entra | Tested with a scripted key endpoint; the real verifier runs | Hermetic. **Never run against a tenant** |
| The 50 conformance scenarios | Not run | **Not proven** |
| Load, saturation, chaos | Not run | **Not proven** |

[`docs/pass-2/HARDENING.md`](pass-2/HARDENING.md) is the ledger of 150 review findings from 2026-10-31: 126 fixed, each with a test that failed first; 20 partly done; 3 open; 1 not real. [`docs/pass-2/ASSUMPTIONS.md`](pass-2/ASSUMPTIONS.md) holds every reading of the spec the code relies on.

---

## 4. What blocks the handoff

### B1. Only warehouse capabilities have a supported registration path

[`assemble.Domain`](../gateway/assemble/assemble.go#L27) is `func() ([]views.Files, error)`. [`capabilities`](../gateway/assemble/assemble.go#L174) turns view files into registry entries, and [`Gateway.Add`](../gateway/assemble/assemble.go#L162) takes only `views.Files`. The startup checks run over that path: policy references, view models against profiles.

An HTTP provider (a vendor API, an internal service, a job scheduler) has no equivalent input. The generic `ports.Provider` and the registry do exist, so a company could compose one by hand, but only by copying assembly logic. That breaks the boundary.

### B2. Access dimensions are fixed in the framework

The scope dimensions are a table compiled into ports: [`ports/provider.go:63`](../gateway/ports/provider.go#L63) maps `PortfolioCode` to `portfolio` and `IndexCode` to `index`, through [`DimensionOf`](../gateway/ports/provider.go#L70). These read that table:
- the access engine ([`access.Values`](../gateway/core/access/access.go#L273));
- the pipeline's scope checks ([`rows.go`](../gateway/core/pipeline/rows.go#L504));
- the built-ins ([`builtins/catalog.go:51`](../gateway/core/builtins/catalog.go#L51), which whoami uses).

A company with `workspace` or `project` would have to edit the framework. Allowing arbitrary policy keys alone would be unsafe, because enforcement would not follow them.

### B3. No shared mechanisms for changing, metered or long-running work

There are no ports or pipeline steps for any of these:
- idempotency retention and fencing (R08);
- confirmation and approval receipts (R06);
- budget authority (R07);
- durable operations;
- separate start and execution deadlines (R17).

`atlas.operation` answers `not_found` and an empty list ([`builtins.go:170`](../gateway/core/builtins/builtins.go#L170)). A company triggering jobs must not implement these per connector.

### B4. The proof is narrower than the handoff claims

These remain unproven:
- **Conformance.** No conformance gateway or client runner exists, so the 50 scenarios in [`spec/protocol/references/scenarios`](../spec/protocol/references/scenarios) have never run.
- **Identity.** The development gateway signs in through a token table ([`devgateway.go:60`](../gateway/local/devgateway/devgateway.go#L60)) instead of signed tokens checked by the real verifier.
- **Load and failure.** Neither has been measured.
- **The requirement matrix** (`make requirements`). It cannot see the journey's tests yet.

### B5. Known defects

The peer review of 2026-11-01 found 16, R3-01 to R3-16, listed in section 5. It was a static review, with no tests run. I confirmed six of them against the code at this commit: R3-01, R3-03, R3-04, R3-05, R3-15 and R3-16. R3-02 is plausible from the code, and not yet reproduced.

---

## 5. The work, in order

Each package says what to change, with references, and how to prove it. Every fix starts with a test that fails for the reason given, named for its finding, then the fix at its root (AGENTS.md). Correctness work (C0) can run beside the design of C1. Do not freeze any public extension interface until its exercise passes.

| Order | Package | Depends on | Done when |
|---|---|---|---|
| 1 | **C0** Fix the defects that break ordinary use | none | Each regression fails before its fix and passes after |
| 2 | **C1** Generic providers and company access dimensions | the boundary (section 2) | A separate company module registers an HTTP capability on a new dimension, with no shared change |
| 3 | **C2** One complete call lifecycle | C0 | Every call has consistent admission, negotiation, cancellation and a persisted outcome |
| 4 | **C3** Dependable contracts and clients | C0 | Compatibility and representation parity against independent fixtures |
| 5 | **C4** Prove the read-only handoff | C1 to C3 | **Gate A** passes |
| 6 | **C5** Shared mechanisms for changing and durable work | C1 to C3; may start before C4 ends | **Gate B** passes |
| 7 | **C6** Package the handoff | C4 for Gate A; C5 for Gate B | A second engineer repeats both gates from the release and the docs |

### C0. Fix the defects that break ordinary use

| Finding | Where | What is wrong | The fix | The regression that proves it |
|---|---|---|---|---|
| **R3-01** P1 | [`pipeline.go:463`](../gateway/core/pipeline/pipeline.go#L463), the projected check schema; [`access.go:279`](../gateway/core/access/access.go#L279), `walk`, which skips absent values; [`rows.go:386`](../gateway/core/pipeline/rows.go#L386), `batchInScope` | The pipeline asks the provider for the scoped columns (`withScoped`), but validates rows against the caller's projection. A provider that leaves out `portfolio` returns rows with nothing scoped to check. Non-string scope values are skipped too | Validate the provider's answer against the augmented schema: the projection plus the scoped properties, which are required. Then check scope, then project. A row or batch without its scope evidence yields nothing | A provider that omits the scoped column, by rows and by batches. A malformed scope value. Nested scope. Extends [`scope_test.go`](../gateway/core/pipeline/scope_test.go) |
| **R3-02** P1 | [`gosf.go:215`](../gateway/adapter/snowflake/gosf/gosf.go#L215), which keeps the driver's batch slice; [`gosf.go:345`](../gateway/adapter/snowflake/gosf/gosf.go#L345), which clears an entry before `Fetch` | gosnowflake's downloader writes through the same slice. With no inline first batch, clearing the entry leaves a nil dereference | Copy the batch slice, so the engine owns its own, keeping each Arrow record's lifetime | Remote-only, inline-plus-remote, multi-record and cancelled downloads, through the real driver over a hermetic HTTP fixture |
| **R3-03** P1 | Go [`client.go:724`](../sdk/go/client.go#L724); Node [`credentials.ts:328`](../clients/node/src/credentials.ts#L328) | Refresh requests send only `grant_type` and `refresh_token`. Entra requires `client_id`, so a token works until it is renewed | Send the trusted binding's `client_id`, and its scopes where the endpoint requires them, keeping the origin and endpoint binding | A token endpoint that rejects a missing or wrong `client_id`. Renewal succeeds with the bound one |
| **R3-04** P1 | [`auditlog.go:169`](../gateway/adapter/auditlog/auditlog.go#L169), `line` and `encode` | The persisted line drops `Claimed`, `Client`, `Expected`, `Step` and `Source` from `ports.Fact` | Write and read every field of the fact | A round trip through the real sink and a reader, asserting the stored bytes and the decoded fact. Make [`porttest.AuditSink`](../gateway/porttest/audit.go) compare every field |
| **R3-05** P1 | [`credentials.ts:271`](../clients/node/src/credentials.ts#L271), `get`; [`credentials.ts:383`](../clients/node/src/credentials.ts#L383), `raceSignal` | A caller whose signal is already aborted starts a refresh and attaches no rejection handler. If that refresh later fails, Node can end the process | Honour an aborted signal before starting work, and always observe the shared refresh's rejection | A pre-aborted sole caller and a failing refresh produce no unhandled rejection, while another caller continues |

### C1. Generic providers and company access dimensions

**Provider registration.**
- **Generalise the assembly.** It should take provider entries: a provider, its capabilities with their history, the upstreams it needs (bulkheads, health), and what it owns for cleanup. A sketch:

  ```go
  // A Module is what a company registers: its capabilities and what serves them.
  type Module struct {
      Name         string
      Capabilities []Capability   // each its profile document and its version's history
      Provider     ports.Provider
      Upstreams    map[string]int // bulkheads by name: how many calls may reach each
      Health       func(context.Context) error
      Close        func() error
  }
  ```

- **Keep warehouse views as one constructor of a `Module`.** For example, `views.Module(src, files...)`. The view checks stay where they are.
- **Keep registration atomic.** Register every module before validating policy references or publishing the snapshot. Keep duplicate detection, history validation, and failure at start on any invalid entry.
- **Leave the pipeline unchanged.** Upstream limits come from the modules, not from the `warehouse` name the pipeline knows today.
- **Changes:** [`gateway/assemble/assemble.go`](../gateway/assemble/assemble.go) (`Domain`, `Parts`, `capabilities`, `Add`), [`gateway/views/`](../gateway/views), and the composition roots ([`gateway/cmd/gateway/main.go`](../gateway/cmd/gateway/main.go) and [`gateway/local/devgateway/devgateway.go`](../gateway/local/devgateway/devgateway.go)).

**Access dimensions.**
- **This is a protocol and core amendment, decided once and recorded.** Record it in [`ASSUMPTIONS.md`](pass-2/ASSUMPTIONS.md) and the spec. The references in `spec/` are owned by the independent references work, so the reading is agreed there (its `QUESTIONS.md`), not changed by the build.
- **The recommendation has two parts:**
  - an explicit scope annotation in the profile: a keyword naming the dimension a property carries;
  - an immutable deployment registry of named dimensions.

  The annotation is the scope evidence R3-01 needs.
- **Registering a dimension grants nobody anything.** Grants still come only from trusted policy.
- **Rules that hold:**
  - unknown dimensions, unresolved references and inconsistent bindings fail at start;
  - an absent grant is no access;
  - absent or null scope evidence is refused where scope applies;
  - one registry serves request checks, provider invocation, result checks, discovery and whoami;
  - nested and projected values and revocation on open streams are enforced;
  - providers may push scope into their source queries, and the core still checks the results.
- **Changes:**
  - [`ports/provider.go`](../gateway/ports/provider.go) (`dimensions`, `DimensionOf`, `Scope`);
  - [`core/access/access.go`](../gateway/core/access/access.go);
  - [`core/pipeline/rows.go`](../gateway/core/pipeline/rows.go) and `pipeline.go`;
  - [`core/builtins/catalog.go`](../gateway/core/builtins/catalog.go) (`dimensions`, whoami);
  - [`adapter/policyfile/`](../gateway/adapter/policyfile) (validating scope keys);
  - the protocol's schema keywords ([`sdk/go/protocol/schema/schema.go`](../sdk/go/protocol/schema/schema.go), the `keywords` table) for the annotation.

**Exit test.** From a separate company module, register `support.tickets`, served by a fake HTTP service and scoped by a new `workspace` dimension. Then:
- two callers granted it both discover it;
- each sees only their own workspaces;
- an out-of-scope request is refused;
- the standard assembly starts with a policy that already names the capability.

No shared file changes.

### C2. One complete call lifecycle

**Order of the lifecycle:**
1. identity and current access;
2. resolve the matched binding;
3. negotiate the representation, after authorization, so a confidential binding stays undisclosed;
4. validate and normalise the request;
5. admit;
6. dispatch;
7. hold the returned data to its contract and scope;
8. record the actual outcome.

An unsupported `Accept` causes no provider call and no charge. Source settlement is recorded apart from the delivery's outcome.

| Finding | Where | The fix |
|---|---|---|
| **R3-06** deadlines | Node: [`index.ts`](../clients/node/src/index.ts) runs discovery before a fresh execution budget. Python: connect without the call's timeout, and the engine lock taken before the timer ([`_engine.py:170`](../clients/python/fiatlas/_engine.py#L170)). Go: a mutex held through a refresh ([`client.go:541`](../sdk/go/client.go#L541)) | One absolute deadline per public call. It carries through bootstrap, discovery, credential locks and refresh, engine queueing, retries and consumption. Waiting on shared work is cancellable alone. Any cleanup grace is documented apart |
| **R3-07** record failures audited as success | [`edge/call.go:208`](../gateway/edge/call.go#L208) forwards a failure only to `res.Rows`. [`edge/call.go:166`](../gateway/edge/call.go#L166) declares `RunNegotiated`, which the pipeline does not implement, so negotiation falls back to after dispatch | One result-level failure and outcome for records and tables. Implement `RunNegotiated` in the pipeline: negotiate after access and before admission |
| **R3-08** old URLs fail negotiation | [`edge/call.go:176`](../gateway/edge/call.go#L176) negotiates against the current interaction's bindings, while the registry matched an older profile's | Carry the matched binding through the call, and negotiate against it |

**Exit tests.** Each in record and table form:
- a successful delivery;
- a refusal before dispatch;
- a serialisation failure;
- a client that disconnects;
- a stalled upstream;
- a queued call that expires;
- a cancelled refresh waiter.

Each asserts provider-call counts, released resources, the caller's problem, and the persisted admission, outcome and settlement facts. Include the audit sink's regular-file path and the fallback where a pollable descriptor cannot be made.

### C3. Dependable contracts and clients

| Finding | Where | The fix |
|---|---|---|
| **R3-09** boolean `items` skip the gate | [`classify.go:180`](../sdk/go/protocol/gate/classify.go#L180) compares items only when both are schemas | Compare absent, boolean and object item schemas explicitly, in both directions |
| **R3-10** a profile's lens reaches unrelated interactions | [`sdk/go/discover.go:363`](../sdk/go/discover.go#L363) gives every interaction the whole lens. The gate checks a field in any schema | Scope transformations to their schema and interaction, or refuse ambiguous profiles |
| **R3-11** nested renames are admitted without a reverse transform | [`classify.go:282`](../sdk/go/protocol/gate/classify.go#L282) requires a rename lens only at the top level | Class a nested result rename as breaking unless both directions are proved. Refusal is enough |
| **R3-12** discovery shows unready capabilities as stable | [`registry.go:118`](../gateway/core/registry/registry.go#L118), `Capability.Lifecycle`, gives the effective lifecycle, but [`builtins/view.go:52`](../gateway/core/builtins/view.go#L52) and [`catalog.go:94`](../gateway/core/builtins/catalog.go#L94) read the raw one | Use the effective lifecycle wherever capabilities are listed or described |
| **R3-13** Python's Arrow drops later optional columns | [`client.py:115`](../clients/python/fiatlas/client.py#L115), `Table.from_pylist` with no schema | Build the table from the resolved, projected reader schema |
| **R3-14** Arrow physical types are not checked | [`sdk/go/result.go:275`](../sdk/go/result.go#L275) decodes without checking the received schema. [`arrowschema/rows.go`](../sdk/go/protocol/arrowschema/rows.go) reads every timestamp as microseconds | Check the physical schema, nested types and protocol metadata before decoding |

**Exit matrix.** The CLI, Python and Node run against one fixture gateway, with expected outcomes written independently. It covers:
- compatible updates in both directions, and two in a row;
- unrelated interactions sharing a field name;
- nested rename refusal;
- boolean `items`;
- old URLs;
- projections;
- empty tables;
- malformed streams;
- unknown critical traits.

### C4. Prove the read-only handoff (Gate A)

- **R3-15.** [`batches.go:97`](../gateway/adapter/snowflake/batches.go#L97) casts every integer to `NUMBER(18,0)`, which cannot hold int64's extremes as 64-bit integer strings. The cast follows the references' reading Q-SNOW-4, so the reading has to be refined, not just the cast. Prove both signed extremes through `porttest.DataSource` and through gosf's conversion.
- **R3-16.** [`gosf.go:132`](../gateway/adapter/snowflake/gosf/gosf.go#L132): `Engine.Ping` calls `database/sql`'s `PingContext`. The session wrapper ([`gosf.go:430`](../gateway/adapter/snowflake/gosf/gosf.go#L430)) does not forward `driver.Pinger`, so an idle pooled session is reported reachable without a probe. Forward it, with context and classification, and test it with a session whose probe fails.
- **The conformance gateway and runners.** Write `sdk/go/conformance`. It serves [`scenarios/fixtures/gateway.json`](../spec/protocol/references/scenarios/fixtures/gateway.json), scripted per scenario, recording every request, with the loopback control API. Add a suite driver, and runners for the CLI, Python and Node. The Go runner is the engine, which already speaks §9.8.3. Run the in-scope Core and Streaming scenarios in CI.
- **A hermetic RS256 issuer**, with JWKS and refresh, used by the development gateway and the end-to-end tests, so the real `kit/jwt` verifier runs everywhere. Token tables stay for focused unit tests.
- **Load and failure, with their pass criteria written first:**
  - deadlines;
  - bounded queues and memory;
  - admission under saturation;
  - cancellation latency;
  - audit availability;
  - policy staleness;
  - isolation between upstreams: a Snowflake outage never consumes the slots a healthy HTTP provider needs.

  Run deterministic fault tests apart from load measurements.
- **An adapter authoring contract for each port.** That is the contract's text, the fake, and a suite that asserts semantics, not just shape.
- **The Gate A exercise.**
  1. Freeze the framework.
  2. Build the CLI, Python and Node packages once.
  3. An engineer who did not build the seams adds a warehouse capability and an HTTP capability, in a separate company module. At least one uses a new dimension.
  4. They configure identity, policy, secrets and adapter bindings.
  5. They discover, describe and call both, with the prebuilt clients.
  6. Record every changed file, and check that the framework's hashes are unchanged.

  A failure means framework work is unfinished. It is never closed by copying internals into the company module.

### C5. Shared mechanisms for changing, metered and durable work (Gate B)

Drive these with the fixture's `conformance.change`, `conformance.costly` and `conformance.slow`, as REVIEW.md's step 3 sets out. Each is a port with a fake, a suite, and at least one durable adapter: process memory cannot prove restart or multi-replica behaviour. On this machine, without Docker, a file-backed store is the hermetic durable adapter. Cosmos DB is tested through its emulator where Docker is available.

| Mechanism | The contract, and the failure evidence |
|---|---|
| Idempotency (R08) | Keys scoped to principal, address and normalised request. Retention and fencing. Concurrent duplicates make one source effect. A changed request under the same key is refused |
| Uncertain outcomes | A timeout after dispatch never invites an unsafe replay. The uncertainty is recorded, and reconciled through the source's operation id |
| Confirmation and approval (R06) | Server-held evidence bound to principal, capability, normalised request, plan, scope, limits and expiry. A client header alone is never consent. It is rechecked before execution |
| Metering (R07) | One budget authority per reservation set: reserve, settle and release atomically. Unsupported combinations are refused before any work |
| Durable operations, deadlines (R17) | Identity, request, state and source reference persisted. Start and execution deadlines kept apart. Recovery after restart. Concurrent workers fenced. Polling and cancellation authorised |
| Audit and settlement (R09) | Admission, outcome and settlement survive retries and restarts. A delivery failure and an uncertain source outcome are distinguishable |

The clients get generic support for each:
- confirmation and approval;
- operation handles;
- polling and cancellation;
- recovery.

No capability-specific client code.

**The Gate B exercise.**
1. Freeze the framework and the clients.
2. Add a company job capability (an Astronomer-style trigger) through the public interface.
3. Trigger it twice with one key.
4. Kill the gateway after dispatch, restart it, then poll and reconcile.
5. Attempt polling and cancellation without authorisation.
6. Settle.
7. Add a metered variant with a bounded reservation.

### C6. Package the handoff

- **A tagged, reproducible release.** It comes with a supported-feature manifest stating each claim's evidence: source coverage, hermetic tests, integration tests, live systems. Nothing is labelled production-proven on the strength of a fake.
- **A guide for every capability, generated from its own definition**, and validated against a fixture with known outcomes. It covers:
  - the task, and when to use it;
  - who can use it, and how scope changes the answer;
  - inputs and defaults, with examples in all three clients;
  - the output: shape, grain, keys, ordering, freshness and truncation;
  - deadlines and limits;
  - retry, idempotency and cancellation;
  - errors, with their next steps;
  - its owner and its version policy.
- **Three reference extensions, in the company's layout:** warehouse, HTTP, and durable job. Each has its suite commands, its deployment wiring, its secret references, and troubleshooting.
- **A boundary-check script** that hashes the Atlas side, for the gate exercises.
- **Final exit.** A second engineer repeats Gates A and B from the release and the documentation. Their changes stay in the allowed files.

---

## 6. Working with the company

**What the company brings, per environment:**
- an identity tenant and its app registrations (API audience, CLI client, workload identities);
- for each source: a read-only service identity, the objects it may read, and its network path;
- the owners of policy, and the grants;
- an owner for each capability, who decides what the capability handbook leaves to owners;
- the deployment platform: secrets, logs, audit retention.

**How an integration is built, once C1 exists:**
1. Pick the port. A new engine is a `DataSource`. A new kind of service is a `Provider`, with adapters behind the ports it needs.
2. Write the adapter in a company package or module, and run its `porttest` suite against it.
3. Write the capability's profile to the handbook's page contract, with its history. Run `make check`, and the gate classifies it.
4. Register the module in the company's composition root. Write the policy, and any new access dimension.
5. Run the development gateway with the company's fakes, then the company's Development source. Call it from the unchanged CLI, Python and Node.

**When an integration finds a framework gap,** the gap is fixed in Atlas, with its test and an entry in the ledger. It is never patched inside the company's module. The framework is versioned, and the company moves to the new version deliberately.

**The first integrations decide the reference extensions.** Ask the company early which systems come first: REST APIs, a warehouse, a job scheduler, Azure services.

## 7. Rules that bind every change

[`AGENTS.md`](../AGENTS.md) holds them in full. The ones that matter most here:
- **github.com/jhgonline is a private organisation.** Atlas's module paths are names only. Nothing fetches from it or pushes to it. The Makefile fences it off (`GOPRIVATE`, and `GOVCS=github.com/jhgonline:off`), and [`tools/modcheck`](../tools/modcheck) proves that every in-repo module resolves locally.
- `make check` must pass before a commit, and `make e2e` for client or edge changes.
- Every fix starts with a failing test named for its finding or requirement id.
- Layers are enforced by `layers.toml` in each module, and the production root links no fakes and no cgo.
- No `init`. Goroutines only through `kit/conc`. Settings only in settings files. Failures only as problems through `kit/problem`.
- Each reading of the spec the code relies on goes in `ASSUMPTIONS.md`.

## 8. Decisions needed before C1 and C5

1. **The access-dimension amendment (C1):** the annotation, plus a deployment registry. It must be agreed with the references owner, because it changes the protocol.
2. **The durable store for C5:** a file-backed hermetic adapter now, plus the company's own store (Cosmos DB or another) behind the same ports.
3. **The company's first integrations,** which choose the reference extensions and their order.

---

## 9. References

**In this repository:**
- [`AGENTS.md`](../AGENTS.md): how to work here.
- [`docs/pass-2/REVIEW.md`](pass-2/REVIEW.md): the design review adopted, and the R01–R26 decisions.
- [`docs/pass-2/PLAN.md`](pass-2/PLAN.md): the original pass-2 plan, superseded in part by REVIEW.md.
- [`docs/pass-2/PLUGGING-IN.md`](pass-2/PLUGGING-IN.md): modules, running locally, adding a capability, the ports, settings.
- [`docs/pass-2/HARDENING.md`](pass-2/HARDENING.md): the ledger of the 150 findings of 2026-10-31.
- [`docs/pass-2/ASSUMPTIONS.md`](pass-2/ASSUMPTIONS.md): the spec readings the code relies on.
- [`docs/pass-2/PHASE-1.md`](pass-2/PHASE-1.md) and [`docs/pass-2/CRITIQUE.md`](pass-2/CRITIQUE.md): the protocol phase, and pass 1's defects.
- [`spec/protocol/references/`](../spec/protocol/references): the independent protocol references. These are the document schemas, the grammar, the vectors, the 50 scenarios and their fixture gateway, with `README.md` and `QUESTIONS.md`.
- [`sdk/go/kit/PROVENANCE.md`](../sdk/go/kit/PROVENANCE.md) and [`sqlglot/PROVENANCE.md`](../sqlglot/PROVENANCE.md): the integrated repositories, at their pinned commits.

**External sources.** These are not in this repository; they are kept with the spec transcription on the maintainer's machine:
- **The Atlas spec**, a transcription in Markdown. The section numbers here (for example §6.4.1, §9.8.3) are its.
- **The 2026-10-31 design review:** `ATLAS-CORE.md`, `CAPABILITY-HANDBOOK.md`, `DESIGN-REVIEW.md` and `INTEGRATIONS.md`. Adopted in REVIEW.md.
- **The peer review of 2026-11-01** (`PEER-REVIEW-2026-11-01.md`): findings R3-01 to R3-16, and the architecture gaps B1 to B4 above.
- **The core handoff plan of 2026-11-01** (`CORE-HANDOFF-PLAN.md`): the boundary, packages C0 to C6, and Gates A and B, which this plan adopts and maps onto the repository.
