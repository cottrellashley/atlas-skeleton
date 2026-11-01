# Plugging in

How a team runs Atlas, adds a capability, and connects its own systems. Everything here is built and tested in this repository: `make check` proves the Go modules, and `make e2e` proves the journey through the CLI, Python and Node against the development gateway.

## The modules, and what each builds

Each directory with a `go.mod` is a module of its own, so a build pulls in only what its binary needs. Modules inside the repository require each other at `v0.0.0`, with a `replace` to their directory.

| Module | Holds | Builds |
|---|---|---|
| `sdk/go` | Several parts:<br>- the protocol: grammar, Structured Fields, schemas, profiles, the gate, resolution, framing, and the 21 protocol documents;<br>- the shared gateway kit;<br>- the Go client, package `atlas`;<br>- the engine. | `atlas-engine`: the runner protocol, and the Python SDK's engine |
| `sqlglot` | The in-repo port of SQLGlot 30.27.0, with every test (`sqlglot/PROVENANCE.md`) | `sqlglot` (CLI) |
| `gateway` | Ports, fakes and port suites; the core (registry, access, pipeline, built-ins); the view-model engine; adapters with no vendor SDK (the Snowflake adapter's planner and guard, Entra, the policy file, the audit log); domain services; the HTTP edge; assembly. No vendor SDK, no cgo | a library |
| `gateway/adapter/snowflake/gosf` | The Snowflake engine over gosnowflake: Arrow batches, exact decimals, the query tag out of band, synchronous cancellation, settlement from the query's own record | a library |
| `gateway/cmd/gateway` | The production composition root: real adapters only, never a fake (its `layers.toml` forbids them) | `gateway`: production, pure Go (`make release`: `CGO_ENABLED=0`, `-tags minicore_disabled`, linux/amd64 and arm64) |
| `gateway/local` | DuckDB running the Snowflake adapter's own statements, the development assembly, the end-to-end tests | `gateway-local`: development, cgo |
| `clients/cli` | The CLI | `atlas` |
| `clients/python` | `fiatlas`, the Python face over the engine | a wheel |
| `clients/node` | `@atlas/sdk`, TypeScript with no dependencies | none: Node 23.6+ runs it as written |
| `tools` | atlasvet, the layer check, the requirement matrix | build and test tools |

```bash
make release   # .bin/gateway-linux-amd64 and -arm64: CGO_ENABLED=0 -tags minicore_disabled
```

Every Go command here builds with `-tags minicore_disabled`, which the Makefile exports in `GOFLAGS`. Without it, gosnowflake links cgo and writes a native library to disk at start. `tools/layercheck`'s `[[pure]]` rule refuses any cgo in what `cmd/gateway` links.

```bash
go -C gateway/local build -o bin/gateway-local ./cmd/gateway-local
```

```bash
go -C clients/cli build -o bin/atlas ./cmd/atlas
```

```bash
go -C sdk/go build -o bin/atlas-engine ./cmd/atlas-engine
```

## Run it locally

```bash
bin/gateway-local -listen 127.0.0.1:8740
```

```bash
bin/atlas --gateway http://127.0.0.1:8740 login --token dev-alice
```

```bash
ATLAS_URL=http://127.0.0.1:8740 bin/atlas call portfolio.holdings@v1:fetch --request '{"portfolios":["147HYC"]}'
```

The development identities are in `gateway/fixtures/identities.json`:

| Token | Who | What they can see |
|---|---|---|
| `dev-alice` | Alice | Four portfolios |
| `dev-bob` | Bob | One portfolio |
| `dev-carol` | Carol | No grants |
| `dev-dave` | Dave | Denied holdings |
| `dev-nightly-risk` | A workload | Every portfolio |

What they may do is in `policy.json`, and the data is in `harbour.json`. Python reads the same sign-in:

```python
from fiatlas import FIAtlas
result = FIAtlas().call("portfolio.holdings@v1:fetch", {"portfolios": ["147HYC"]})
```

The amounts arrive as `decimal.Decimal`. Node reads it too:

```ts
import { atlas } from '@atlas/sdk';
const client = await atlas.connect();
const rows = await (await client.call('portfolio.holdings@v1:fetch', { portfolios: ['147HYC'] })).rows();
```

## Add a data capability: two files, no code

A curated warehouse capability takes two files from its owner, and no Go code:

1. **A profile.** `svc/<domain>/profiles/<id>@v<N>.json` holds its contract, written to the handbook's page contract: identity, interactions, request and result schemas, errors with next steps, its meaning (grain, keys, time, scope, ordering, source), usage with examples, and evolution.
2. **A view model.** `svc/<domain>/views/<id>@v<N>.json` says where the answer is:
   - the object;
   - each result property's column (Money takes two columns, an enum takes a value map);
   - the filters each request member makes;
   - an optional "latest on or before" rule;
   - the data scope's column;
   - the order;
   - an optional existence check that answers the capability's declared error.

   `gateway/svc/portfolio/` holds four examples.
3. **Its history, when it replaces a profile.** `svc/<domain>/history/<id>@v<N>/<fingerprint>.json` holds every profile of the version it replaced. The registry holds a new profile to the gate against its version's history (R03, PR05), and refuses one whose previous profile does not travel with it. It serves every binding of every profile of a live version, so a client pinned to an older one still reaches it.

When the gateway registers a capability:
- **The gate.** Its profile passes the gate again: lint, agent readiness, and the R03 classification against what it served before. A breaking change within a version is refused, and the whole generation with it.
- **The view model.** It is checked against its profile: every result property has a source, every filter names a request member, every declared error exists. A model that cannot answer its contract stops the gateway at start, never on a call.

A new domain is a package of three lines (`svc/datastatus/datastatus.go`) and one entry in `cmd/gateway/main.go`'s `domains` list. Modules register in that one list, never in `init`.

The development gateway also serves a directory as it changes:
- `-catalog DIR` adds each new `profiles/` and `views/` pair it finds, with `history/` beside them, with no restart and no client released.
- `tests/e2e/acceptance.py` adds `portfolio.holdings-dates@v1` this way.

## Add any other integration: a module

A capability that is not a warehouse view (a vendor API, an internal service, a job scheduler) is a `ports.Module`, written in the company's own package or Go module. Nothing in Atlas changes. The worked example is `extensions/support/`: `support.tickets@v1` over a support desk's HTTP API, scoped by Acme's own `workspace` dimension.

```go
m := ports.Module{
	Name:         "support-desk",
	Capabilities: []ports.Capability{{Name: "support.tickets@v1", Profile: profileJSON}},
	Provider:     provider,      // a ports.Provider: Serve(ctx, *ports.Invocation)
	Concurrency:  4,             // the desk's bulkhead; Upstream defaults to Name
	Health:       provider.ping, // the module's own; never the gateway's readiness
}
g, err := assemble.New(assemble.Parts{ /* identity, policy, audit, ... */
	Modules:    []ports.Module{m},
	Dimensions: []ports.Dimension{{Name: "workspace", Description: "An Acme support workspace, by its code."}},
})
```

- **The profile** is written to the handbook's page contract, as any capability's is. It passes the gate when it is registered.
- **The provider** gets a request that is already validated, normalised and inside the caller's scope. `inv.Scope` holds the caller's scope in each dimension, so the provider can narrow its source's read. The pipeline still holds every row it returns to the contract and to the scope.
- **Every outbound call** takes the call's context, which carries its deadline. Every answer read is bounded. A failure the profile declares is returned as `protocol.Declared{...}.Problem(detail)`.
- **Registration is atomic.** Every module's capabilities register with the built-ins as one generation, then the policy is checked against them all. A policy may already name a module's capability.
- Warehouse view models are one constructor of a module, `views.Module`, which the assembly builds from `Parts.Domains`.

### A company's own access dimension

A property whose values are codes of a company dimension says so with `x-atlas-scope`:

```json
"workspace": {"type": "string", "description": "The ticket's workspace.", "x-atlas-scope": "workspace"}
```

The deployment defines the dimension in `Parts.Dimensions`, and its policy grants values of it:

```json
"scopes": {"eu-agents": {"workspace": ["acme-eu"]}}
```

- **Defining a dimension grants nobody anything.** A caller with no scope in a dimension can read none of it.
- **The gateway refuses to start** if a profile names an undefined dimension, or a policy scopes one.
- **Enforcement is the same as for `portfolio`:** request values outside the scope are refused before the provider runs, a result row without its required scope value is withheld, and `atlas.whoami` lists the caller's scope in every dimension.

This is a proposed amendment to the protocol (`ASSUMPTIONS.md` A-200).

## Plug in a real system: implement a port, pass its suite

The core needs five things of the world. Each is an interface in `gateway/ports`, and each has a suite in `gateway/porttest` that every implementation runs: the fake, the adapter, and yours.

| Port | Shipped | Suite |
|---|---|---|
| `DataSource` (§6.4.1) | `adapter/snowflake`'s `Source`, over an `Engine`: gosnowflake in production (`gosf`), DuckDB in development (`local/duckwh`). It works in four steps:<br>- **plans** a declarative read as one Snowflake statement through sqlglot, every column aliased and cast to its contract type, every value bound, and the read-only guard holding it (all 65 guard vectors pass);<br>- **explains** the plan with that exact statement, running nothing;<br>- **executes** it as Arrow batches in the writer schema, with the tag out of band, a bounded bulkhead, and failures classified;<br>- **settles** from the engine's own record of the query.<br>A second engine is a second implementation of this interface | `porttest.DataSource`, passed by `fakes.DataSource`, by the Snowflake source over DuckDB, and over a counting engine |
| `Authenticator` | `adapter/entra`, Entra ID access tokens through the kit's JWT verifier. `oid` is the principal; `idtyp=app` is a workload. | `porttest.Authenticator` |
| `PolicySource` | `adapter/policyfile`, a JSON policy, reread when it changes. A failed read is reported as a failure, never answered with the last good policy. The pipeline keeps that policy for at most `PolicyMaxAge` (R13), and gives up a read that ignores its deadline | `porttest.PolicySource`, passed by the file and the fake |
| `AuditSink` | `adapter/auditlog`, one JSON line per fact, synced on a file. A pipe or socket's writes are bounded (`WriteTimeout`). Admission, refusal, outcome and settlement are facts (R09); no token, body or query is recorded | `porttest.AuditSink` |
| `Provider` | `views` (data), `core/builtins` (atlas.*) | `porttest.Provider`, passed by views |

Snowflake is linked already: `cmd/gateway` builds the `DataSource` from `gosf.New` and `snowflake.NewSource`. To connect it, set its settings:

```json
{
 "environment": "dv",
 "listen": ":8080",
 "url": "https://atlas-dv.example.com",
 "sign_in": {"authority": "https://login.microsoftonline.com/<tenant>/v2.0", "client_id": "<cli app id>", "scopes": ["api://atlas/.default"], "token_endpoint": "https://login.microsoftonline.com/<tenant>/oauth2/v2.0/token", "device_authorization_endpoint": "https://login.microsoftonline.com/<tenant>/oauth2/v2.0/devicecode"},
 "entra": {"issuer": "https://login.microsoftonline.com/<tenant>/v2.0", "audiences": ["api://atlas"], "jwks_url": "https://login.microsoftonline.com/<tenant>/discovery/v2.0/keys", "tenant": "<tenant>"},
 "policy_file": "/etc/atlas/policy.json",
 "audit_file": "/var/log/atlas/audit.jsonl",
 "warehouse": {"driver": "snowflake", "dsn": {"env": "ATLAS_SNOWFLAKE_DSN"}, "registered": [["HARBOUR", "HOLDINGS"], ["HARBOUR", "REF"], ["HARBOUR", "OPS"]], "max_open": 8},
 "domains": ["portfolio", "data"],
 "cursor_key": {"env": "ATLAS_CURSOR_KEY"}
}
```

Start it with `bin/gateway -config settings.json`.
- Secrets are named by the environment variables that hold them, never written in the file.
- `warehouse.dsn` names a gosnowflake DSN: account, user and authenticator, warehouse, database, schema, and a read-only role. It is never quoted in an error.
- `audit_file` is a file, or `-` for standard output read by a collector. A terminal or `/dev/null` keeps nothing, and is refused at start.
- `cursor_key` is at least 32 bytes and shared by every replica. Give `cursor_key_previous` when rotating it.
- `url` is the exact URL clients connect with. A client sends a saved credential only to the origin, authority and audience it was bound to (R26).
- Settings that would make the bootstrap fail its schema, or are unsafe outside `local`, stop the start, with the reason.
- The Snowflake connection keeps `DEFAULT_NULL_ORDERING` at `LAST`. The port suite proves null order on each engine.

## The rules that keep it plug-and-play

These are checked by machine, in `make check`:
- **Layers.** Each module's `layers.toml` sets them:
  - adapters and services are independent of each other;
  - a service never imports the core;
  - only a deployable chooses an adapter;
  - the core imports no third-party module.
- **atlasvet.** No `init`, no bare `go` outside the kit (use `kit/conc`), settings read only in `settings.go`, and no `http.Error`.
- **The gate at registration.** It refuses breaking changes within a version, measured by R03, in both directions for results.
- **Every document checked against the protocol's own schemas.** The 21 schemas are embedded in `sdk/go/protocol/documents`, and a test pins them byte for byte to the references.

## How it is proved

| Command | Proves |
|---|---|
| `make check` | gofmt, vet, atlasvet, staticcheck, every Go test under `-race`, the layer and pure-Go checks, the private-organisation guard (`tools/modcheck`), `go mod tidy`, and the release build, over every Go module. That includes every protocol vector, the 65 guard vectors, every port suite on its implementations, the journey over HTTP, and the concurrency and failure tests |
| `make e2e-python`, `make e2e-node` | Each SDK against the development gateway, with results checked against the fixture independently |
| `make e2e` | Both, plus `tests/e2e/acceptance.py`: the review's six-point acceptance result across the CLI, Python and Node |

## Not built yet

`docs/pass-2/HARDENING.md` is the ledger of every review finding: what was fixed, with its proof, and what is open, with its owner. In the review's order of work, the open items are:
- **The conformance suite (R062).** The scenario gateway that serves `scenarios/fixtures/gateway.json`, the suite driver, and the CLI, Python and Node runners. The engine already speaks the runner protocol (§9.8.3).
- **Idempotency claims, approval receipts, budget reservations and durable operations (R06–R08), and separate deadlines (R17).** No capability in the journey changes anything, is metered, or runs longer than its deadline. `atlas.operation` answers `not_found` and an empty list.
- **A hermetic RS256 issuer**, so the real token verifier runs in every development test. The development gateway signs in with a token table.
- **Load and chaos tests**, with SLOs as pass criteria (PLAN.md §7).
- **Handoff, events, transfers and exports.** The review defers them to gated proofs on real systems.
- **Interactive sign-in (PKCE, device code).** `atlas login` takes a token, and every client refreshes it at the token endpoint the token was bound to.
