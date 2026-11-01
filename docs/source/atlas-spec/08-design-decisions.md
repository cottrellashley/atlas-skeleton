# §8 Design decisions

`spec/decisions.md` · proposed

> Why Atlas is as it is: the decisions it rests on with their reasons, the alternatives rejected and the narrower role each keeps, the known gaps between this spec and the code, and every open decision with the recommendation made.

## In short

- Atlas rests on 52 decisions (8.1): one host, the Atlas Gateway, with clients built for their users; Avro's model with GraphQL's practices; JSON Schema with an Atlas vocabulary, encoding-neutral; streaming and handoff first; a front door over movable services; engines behind one port; one normative protocol for every language.
- Fifteen alternatives were rejected for a central role (8.2); most keep a narrower one (Envoy as ingress, MCP as an edge, Dapr inside a service, gRPC and Flight as later bindings).
- The code is behind the spec in known ways (8.3); 52 decisions are open (8.4), each with a recommendation.

## Rules

1. A change that overturns a decision in 8.1, or adopts an alternative in 8.2, updates this page in the same change and says why.
2. An open decision, once made, moves from 8.4 to 8.1.

## Detail

### 8.1 Decided

| # | Decision | Why | Where |
|---|---|---|---|
| D1 | One host, the Atlas Gateway, serves every capability to every client; four clients (Python, Node, the CLI, Go), each built for its language and the people who use it. | One door that decides, audits and evolves every call; each client fits its users. | §1, §5, §6 |
| D2 | Work on the caller's machine (files, git, GitHub, output, Parquet, the platform's adapters) is a **client extra**, never a capability: in its client's own namespace, never calling the gateway except through the client's calls, never sending the caller's credentials to it. | The catalogue holds only what the gateway serves; dashboards on Nautilus Desktop stay fast where the work is local. | §5, §9.7.12 |
| D3 | The gateway owns every contract; clients interpret profiles; a client is released only for a protocol change. | The API can change weekly without client rollouts. | §3, §4, §5 |
| D4 | A capability is a concept (`domain.name@vN`) with interactions (`:fetch`, `:run`, ...), each reached through bindings; names never dictate endpoints. | Agents and governance work with concepts; infrastructure is free to differ. | §2.1 |
| D5 | Four surfaces (a profile's `family`): data, resources, actions, events. | Each fixes its verbs, so clients implement each once. | §2.2–§2.5 |
| D6 | Only built-ins are `atlas.*`; what the team owns is named for its subject, generically (never `harbour.*`); what it does not own is named for its provider or platform; where a capability runs is never in a name. | Names that describe the concept, not where it is stored or run. | §2.1, §7 |
| D7 | Avro's model: writer and reader profiles, fingerprints, resolution, with declared lenses; GraphQL's practices for the surface; not a GraphQL server. | The capability call already is the query; resolution lets old and new meet without releases. | §3.4 |
| D8 | Schemas are JSON Schema 2020-12 with an Atlas vocabulary; tables also carry an Arrow schema. | Widely validated, already produced by the gateway; the vocabulary carries meaning and renames. | §3.2 |
| D9 | Requests `BACKWARD_TRANSITIVE`, everything else `FULL_TRANSITIVE`; checked in the gate and at registration. | A breaking change can never reach callers. | §3.3 |
| D10 | Governance (grants, limits, budgets, approval) and availability are served beside the profile, with their own revision or none. | A tightened limit or a suspended warehouse is never a contract change, and a contract change never changes security. | §3.1 |
| D11 | Data results are run by the gateway (streamed), handed off, or brokered by a ticket, as the register in §6.3 sets per system; export next. | Large results never need the gateway to carry them; the sanctioned computation is kept in every path. | §2.2 |
| D12 | Events join the model: CloudEvents, ephemeral or durable subscriptions, at-least-once; AsyncAPI generated from profiles. | Events get the same contracts, access and versioning. | §2.5 |
| D13 | The working view shows only what the caller can use; only `atlas.access` shows the rest, with reasons; confidential capabilities are `not_found` to non-holders. | Agents never waste a step on what they cannot call. | §2.6 |
| D14 | Agents get a small fixed set of MCP tools, not one per capability. | Hundreds of tools would flood an agent's context. | §4.2 |
| D15 | Problems are RFC 9457 with Azure's `code`, `target`, `details`, plus `retryable`; long-running work uses `202` and `Operation-Location` on one operation resource. | The most proven conventions, on the problem format the gateway already serves. | §4.1 |
| D16 | Identity: an Entra token for the Atlas Gateway from every client; each system reached as itself, a workload or the person, as §6.3 records; identity headers never trusted; credentials on the caller's machine never leave it. | One door decides; each system's own grants are the last word wherever it can know the person. | §6.2, §6.3 |
| D17 | Evaluation order deny, grants, conditions, validation, limits (as Azure RBAC, with validation after authorisation as Kubernetes admits after it authorises); limits reserved before work and settled after (amended 2026-10-30: validation placed after authorisation and before limits). | Exact limits under concurrency; a caller without a grant learns nothing of a request's shape, and an invalid request spends no limit. | §6.2 |
| D18 | The gateway is a front door over services; every service is movable, and moves only when a split criterion is met. | One team runs it today; any service can leave tomorrow. | §6.1 |
| D19 | The gateway is one Go module in six layers; ports as Go interfaces in one package; adapters by external system; deployables as composition roots; a package only by the rule in §6.5.5. (Decided 2026-10-28 as a Python uv workspace; changed 2026-10-30 to Go.) | Reusable logic without a mess of dependencies; one language for the gateway and the Go clients, so the protocol is written once; one static binary per deployable. | §6.5 |
| D20 | The protocol is implemented once in Go, `sdk/go/protocol`, and shared by the gateway, the conformance gateway, the Go SDK, the CLI and the Python SDK's engine; TypeScript is the second implementation. (Decided 2026-10-28 as a shared Python package; changed 2026-10-30.) | The gateway and its clients compute the same fingerprints and resolutions because they run the same code; the plan writes the protocol twice, not three times. | §6.5, §9 |
| D22 | The Atlas Protocol (§9) is the one normative, language-neutral definition of what crosses the wire; no implementation is the reference; conformance is decided by running the vectors and scenarios; three checks stay independent of the Go implementation (the vectors from `tools/spec/`, the TypeScript implementation, AP-003). | A client can be written or generated in any language, and every language's client behaves the same; a mistake written once in Go is still caught. | §9 |
| D23 | The protocol's strict choices: I-JSON; JSON Canonicalization Scheme (RFC 8785) and SHA-256 for fingerprints; the Atlas Schema profile of JSON Schema (fixed keywords and formats, I-Regexp patterns); RFC 9651 Structured Fields for every Atlas header; `POST` with a JSON body for every interaction; NDJSON ending in an end line and Arrow ending in a zero-row batch; server-sent events with fixed event names; a shared sign-in store; fixed CLI exit codes. | Each removes a choice two languages could make differently. | §9.1–§9.7 |
| D24 | Conformance levels (Core required; Streaming, Columnar, Bulk, Events, Handoff, Broker independent); a client declares its levels and is tested at them, through a stdin/stdout runner driven against a scripted conformance gateway. | Thin clients are first-class; capability differences are explicit. | §9.8 |
| D25 | The contract is encoding-neutral: JSON, NDJSON, Arrow and Parquet are encodings derived from the profile's schemas; Atlas's types are its own, chosen to be carried exactly by every encoding. | Arrow is chosen for speed, not depended on; an engine or encoding can change without a contract changing. | §3.2, §9.2.6 |
| D26 | Data results travel streamed by the gateway, **handed off** (the client downloads one result's chunks directly from the source's storage, with no session at the source), or brokered by a ticket (the planned SQL, its writer schema and a credential); export next. | The handoff keeps the SQL and the credential in the gateway and still moves no bytes through it; the ticket serves clients that run at the source. | §2.2, §2.2.1 |
| D27 | The gateway writes the contract into the query it plans (every column aliased and cast), so the source itself produces the writer schema in every execution. | The writer side holds even where the gateway does not carry the bytes. | §2.2, §2.2.1 |
| D28 | Engines are reached through one `DataSource` port; nothing about an engine leaves its adapter; the Go SQL library (Atlas's own, replacing sqlglot) builds every plan as a typed tree, renders each dialect (Snowflake; DuckDB for tests) and guards every statement, with a read-only role underneath; ADBC where it serves. (Changed 2026-10-30 from sqlglot.) | Replacing Snowflake changes one adapter and no client; the planner lives in the gateway's own language. | §6.4 |
| D29 | The spec reads from the outside in, and every page in layers (the sentence, in short, a picture, a trace, the seams, the rules, the detail): an OKF bundle with index files and Mermaid diagrams. | Vision and detail on one page; each reader stops at the depth they need. | Writing and presenting the spec |
| D30 | Azure and Astronomer are capabilities of the Atlas Gateway; Azure only through **Resource Graph** (`azure.resource-graph`); both run by the gateway as itself now, as the person in the target. | Every client, wherever it runs, gets them; Resource Graph is read-only. | §2.2.2, §6.3 |
| D31 | A capability whose columns are chosen by the caller's own query has the trait `DynamicSchema`: each answer carries its schema, no resolution applies, always `upstream`. | Free-form queries (`snowflake.query`, `azure.resource-graph`) cannot promise a schema, and must say so. | §2.1, §9.3 |
| D32 | Atlas's long-term direction: one central gateway to which any team deploys its own backend services dynamically, each tied to its own domain and profiles, with every client and all access managed centrally by the front door. | Generic surfaces and types, complete profiles and strict per-family endpoints make every team's capabilities reach every client with no client release. | Vision |
| D33 | Every system is reached in one of three patterns (run, hand off, broker) as one of three identities (the gateway, a workload, the person), recorded per system now and in the target: Snowflake run, handed off and brokered, as the gateway now, as the person by OBO in the target; Resource Graph and Astronomer run as the gateway now, as the person in the target; Service Bus brokered, as the person; Blob storage handed off by a SAS the gateway signs, never seeing the data; vendor APIs run as the gateway always. | Each system is reached the safest way it allows today, with a stated path to the person's own identity. | §6.3 |
| D34 | The credential rule: a credential that leaves the gateway lets its holder do nothing they could not do through the gateway at that moment; every exception is listed and blocks release beyond Development. | A handed-out credential must never be a way around Atlas's decision. | §6.3 |
| D35 | One On-Behalf-Of exchange in the gateway, built once, a scope and a consent per system; its tokens never leave the gateway. | Acting as the person is one pathway for every system that accepts Entra tokens. | §6.3 |
| D36 | Atlas's goal is that an agent can build whatever anyone in the firm needs through the CLI and SDKs alone; it is judged and tested by role journeys. | The interface people use can be anything; what makes it useful is what an agent can find, understand and call. | Vision |
| D37 | Events are brokered: the gateway registers event types, creates topics, queues and subscriptions and assigns roles as itself; clients publish and listen directly on Service Bus as the person; server-sent events through the gateway are not built first. | Less logic and no message path in the gateway; Service Bus checks every message as the person. | §2.5 |
| D38 | One route per question: no two listed capabilities answer the same question; overlapping sources are one capability's parameter, or one is authoritative; house rules, served with the working view, say which is the truth for what. | An agent goes straight to the answer instead of trying overlapping sources; the same question takes the same route. | §7, §2.6, Vision |
| D39 | What an agent builds runs on its own: the agent chooses the route while building, and the artefact replays it from its lock, with no discovery at run time. | A dashboard, a backtest or a scheduled job needs the same route every time, which an agent choosing afresh cannot promise. | Vision, §9.7.9 |
| D40 | A metered call whose estimate passes the caller's threshold is confirmed by the person first (`confirmation_required`, `Atlas-Confirm`). | Large vendor-billed or warehouse-heavy requests are a person's decision, not an agent's. | §6.2.6, §9.3, §9.6 |
| D41 | `atlas/1.0` has no subscription over HTTP: every event, the gateway's own included (`atlas.catalog.changed`, resource changes, `atlas.operation.*`), is reached by a connect handover to Service Bus at the Events level; a client that cannot reach Service Bus polls (the event's history capability, the catalogue's ETag, the operation). | One way to receive events, no stream edge to build or scale, and D37 holds for every event. | §2.5, §9.C, §9.7.8 |
| D42 | The `client` execution (the ticket, the Broker level) is deferred until its credential meets the credential rule (O54); no client declares Broker in `atlas/1.0`. | Nothing handed out may let its holder do more than through the gateway; streaming and the handoff cover the need. | §2.2, §2.2.1, §6.3 |
| D43 | The Python SDK is a Python face over the Go client engine (`atlas-engine`, the Go SDK as a program) that its wheel ships: they speak the runner protocol (§9.8.3) over pipes, with Arrow results on a second pipe; one engine per process, never shared across a fork. | Every protocol rule written once, in Go; no Go runtime inside Python; pure Go builds every platform's wheel. | §5.1 |
| D44 | Three ways to understand a capability, each its own call: `atlas.describe` (what it is: the contract, no parameters), the built-in `atlas.guide` (how to use it: written for an agent, in its client's syntax, within a token budget) and the data interaction `explain` with the request (what this call would do: the exact SQL, columns, estimate, execution). | Each answers a different question; what the catalogue answers alone is a built-in, what needs the service to plan is an interaction; one route per question. | §2.6, §2.2, §9.3 |
| D45 | State has three owners: **Entra ID** for access; **Azure Monitor and immutable Blob storage** for telemetry; **Cosmos DB** for state the gateway decides with; every store behind a port. | Each owner fits its access pattern, and calls never wait on reporting. | §6.1.5, §6.2.4, §6.2.8 |
| D46 | Adding a capability changes nothing outside the gateway (ATLAS-CORE-009): clients hold no capability's code and never refuse one they have not seen; the MCP tool set is fixed; skills and harness prompts teach the generic loop and name no capability; registration refuses a capability a client at the minimum protocol could not call. | A deployment of the gateway reaches every client, agent and harness at once; only a protocol change ever needs a client release. | §2.6, §3.3.12, §4.2, §5, §9.7 |
| D47 | Three parts, three jobs: the capability holds the state and rules out there and returns the data or bytes; the engine lands them here in one agreed form per kind of result; the client package and its extras turn what landed into what the context needs. The line is where the destination lives: out there is a capability, here is the client. | A new capability never needs a client release; a new local use needs a release of that one client only; nothing is written to disk that did not need to be. | §5, §9.7.13 |
| D48 | Fifteen architectural principles bind the gateway and every service behind it; each mechanism is built once from a tested pattern, with the values §6.7 sets; a service or stable profile passes the review of §6.6.3. (Decided 2026-10-30.) | They are how Kubernetes, Azure Resource Manager, Envoy, Caddy, Dapr and Terraform add the two-hundredth service at the cost of the second, which is Atlas's own aim. | §6.6, §6.7 |
| D49 | One resource envelope on every record of the resource family: `name`, `metadata` (`uid`, `etag`, `generation`, `created`, `updated`, `owner`, `labels`, `deleting`), its own desired fields at the top, and `status` (`observed_generation`, conditions) written only by a controller, for a resource the gateway converges. (Decided 2026-10-30, a protocol addition to `atlas/1.0` before its release.) | Clients, tools, access and audit are written once against one shape; a caller reads why something is not ready instead of guessing. | §9.3, §3.4, §2.3 |
| D50 | The gateway changes nothing outside itself inside a call: the call writes the desired state and controllers converge Service Bus entities and roles, public copies, admitted services, settlements and running operations, level-triggered, with backoff, resync and finalizers, one leader each; operations are leased and taken over when their replica stops. (Decided 2026-10-30.) | A failure halfway becomes a state that heals, not an inconsistency; a revoke cannot be missed because one call or one event failed, and a crash strands no work. | §6.1, §6.7 |
| D51 | The request path reads one checked, immutable snapshot, saved as last-known-good; a replica starts from it when Cosmos DB is down, and while it is down unbudgeted reads are served on per-replica limits and everything else fails closed, for at most 60 minutes. (Decided 2026-10-30.) | A store outage costs writes and exact limits, not the catalogue; a revoked grant is never honoured for long. | §6.1, §6.1.14 |
| D52 | Kill switches (`atlas.switch`) turn a capability, domain, service or gateway behaviour off without a deploy, through the snapshot; what fails when each dependency is down for an hour is written down and tested. (Decided 2026-10-30.) | A bad change or a failing upstream is contained in a minute, and nobody learns a dependency's blast radius in an outage. | §6.1, §6.1.15, §6.1.16 |
| D21 | Patterns may be taken from any open-source project; code only from permissively licensed ones, with notices. | Learn from the best implementations, within their licences. | Third-party code |

### 8.2 Considered and rejected

Each was considered for a central role and rejected for it; several keep a narrower role.

| Alternative | Rejected as | Why | The role it keeps |
|---|---|---|---|
| **GraphQL** | The gateway's protocol, or a client-side query layer | No native jobs, streams with resumption or bulk binary data; one query spanning many capabilities makes access, limits and audit a calculation over a tree; the capability call already is the query. | Its practices: introspection, deprecation of fields and values, nullability, errors with paths, change classification, how its spec is written. |
| **gRPC alone** | The single protocol for clients | Browser pages and the thinnest clients would need a translation proxy; its reflection would be a second discovery; protobuf schemas would duplicate the JSON Schema clients validate. | A possible later binding (duplex streams, Arrow Flight). |
| **MCP** | The core protocol | Built for small JSON tool calls: no bulk or binary data, no row streams, only cooperative cancellation, names unique per server. | An edge for agents (D14), with its tasks extension for operations. |
| **A service mesh** (Istio, Consul Connect) | The backbone (architecture C) | Secures and routes traffic but knows nothing of capabilities, profiles or limits; a second platform to run, and a sidecar on every laptop. | None. |
| **Dapr** | The framework services are built on | Standardises plumbing between services, not contracts; no catalogue or schemas. | Optionally inside one cloud service (pub/sub, state); its rule that only replayable requests are retried. |
| **Envoy** | The gateway itself, with its authorisation hook deciding access | Proven enforcement per request, but no notion of a capability, contract or interaction. | Ingress (or Front Door, Application Gateway) in front of the gateway; the shape of a bounded, fail-closed authorisation call. |
| **An overlay network** (Tailscale, Consul) between laptops and Azure | How clients reach the gateway | HTTPS with a token for the gateway meets the need; an overlay could let clients bypass the front door. | Its principle: the control plane carries keys and policy, never data. |
| **Azure API Management** | The gateway, or a policy layer in the request path | No federated catalogue, interactions or tickets; two policy layers double where a limit can be wrong. | Its developer portal, importing the published OpenAPI. |
| **One out-of-process provider per capability from day one** | The starting shape | Every service already sits behind serialisable ports and a loopback battery, which is the condition for moving it; running all of them apart now adds operations without need. | Service hosts, used when a split criterion is met. |
| **TypeSpec or another authoring language** | Where contracts are written | Contracts are declared beside the gateway's code; a second language would be a second source of truth. | Possibly for third-party providers later. |
| **Our own schema language** | The schema language | JSON Schema is validated everywhere; Atlas adds only a vocabulary. | — |
| **A policy engine in the request path now** (Cedar, OPA) | Today's access decision | Today's rules (roles, grants, deny wins, read-only families) are met by the existing engine. | Cedar as the direction for conditions on resources. |
| **Listing every capability with `invocable: false`** | Discovery's default | Agents should see only what they can use. | The `missing` structure, in `atlas.access` and in `atlas.describe`'s client eligibility. |
| **A host on the person's machine** (a daemon every client reaches over a socket, serving local capabilities and forwarding the rest) | Where clients call Atlas, and where local work runs | Two hosts mean two catalogues, two enforcement points, a second wire protocol and a daemon to install and keep running; every client can reach the gateway over HTTPS, and local work needs no host. | Client extras, in each client's own process. |
| **A separate events repository** (`fi-spec-events`) or an event catalogue site now | Where events are defined | Events are profiles in this spec; the gateway serves them. | EventCatalog or a schema registry later, reading the generated AsyncAPI. |

### 8.3 Historical gaps between the specification and implementation (2026-10-28)

These findings describe the implementation reviewed on that date. They retain migration lessons and unresolved design concerns; use the [handoff progress](../../HANDOFF-PROGRESS.md) for current completion and evidence.

- Capabilities have one mode each (serve, broker, publish, operate) and some ids end in a verb; the spec has surfaces and interactions.
- No profiles with fingerprints, no `Atlas-Profile` headers, no resolution or lenses, no locks, no client features; `describe` and its JSON Schema are the nearest.
- `columns` exists on some request models, not as a trait on every table.
- Jobs, runs, publications and permits are separate; the spec has one operation resource.
- Clients build routes; the spec has them follow served bindings from `/.well-known/atlas`.
- No events family, change-feed publisher, export, transfers, service protocol or deployables; the earlier gateway is Python, and the gateway this spec describes is Go (D19).
- **The Snowflake ticket's credential is the gateway's own token, with every role of its identity** (`SESSION:ROLE-ANY`; 92 roles in Development): read-only rests on the client and on reconciliation. It is the listed exception to the credential rule (D34), blocking tickets beyond Development until it is closed (O54).
- The gateway runs on App Service; the direction is Container Apps.
- The code has a gateway on the person's machine that this spec does not include (`sdk/go/localgateway`, the CLI's `atlas serve` and `internal/localgw`, `qfi`'s server), with its protobuf frames (`clients/proto/`) and the old Desktop's `nautilus.gateway.v1` contract. It is removed once the Node SDK's extras serve Nautilus Desktop.
- `azure.resource-graph` and the `astronomer.*` capabilities are not in the gateway yet.
- SharePoint is reached as a desk service account, by a password grant, with a broad token (`AllSites.Manage MyFiles.Read MyFiles.Write`, measured 2026-10-07); read-only is a property of the service's code. The register (§6.3.3) has the person, by OBO.
- The catalogue uses today's names until the renames are applied by aliases.
- The gateway streams Snowflake results through Python rows (`fetchmany` in `atlas_snowflake/stream.py`), and Atlas Python reads whole results into memory (`fetch_arrow_all`); neither uses Snowflake's Arrow batches. There is no handoff yet. On-Behalf-Of to Snowflake exists (`atlas_snowflake/obo.py`, verified against the tenant on 2026-09-08).
- No implementation of §9 exists yet as such: the gateway, the CLI, Atlas Python and the Node SDK each implement parts of today's protocol their own way. Four vector files, three profile vectors, the conformance gateway and the runners are still to write.

### 8.4 Open

| # | Decision | Recommended | Where |
|---|---|---|---|
| O1 | Actions move out of ids (`astronomer.deployment:restart`). | Yes, by aliases. | §2.1 |
| O2 | Which Snowflake data capabilities offer `handoff`. | Every one planned as one statement, once the spike confirms it. | §2.2.1 |
| O3 | Settlement mismatch: recorded only, or enforced. | Recorded now. | §2.2 |
| O5 | Which routes become resources first. | The dashboards, with Nautilus V3. | §2.3 |
| O6 | Event catalogue at the platform level (`spec/events/`). | Yes. | §2.5 |
| O8 | Triggers (subscriptions that start actions). | Later. | §2.5 |
| O10 | The first logical types. | As in §3.2. | §3.2 |
| O11 | Renaming today's columns to `snake_case` without units, by aliases. | Yes, as each profile is written. | §3.2 |
| O12 | Minimum sunset; the lock's format. | 90 days; `atlas.lock` in TOML. | §3.3 |
| O13 | Which capabilities are confidential. | Administration, and those whose existence is sensitive. | §2.6 |
| O14 | Requesting access from `atlas.access`. | Later. | §2.6 |
| O15 | One operation resource replacing jobs, runs, publications and permits. | Yes, now, before third parties. | §4.1 |
| O17 | Astronomer attribution: the run's `conf` alone, or a verifying plugin. | To decide. | §6.2 |
| O19 | Grants per interaction. | Yes. | §6.2 |
| O20 | Who reviews loosened limits; two people for `irreversible`. | The domain's owner; two people in Production. | §6.2 |
| O21 | The service protocol's transport. | HTTP/2 with Arrow and JSON. | §6.1 |
| O22 | The signed token between front door and services. | Entra between services, with Atlas's scope as a signed claim. | §6.1 |
| O23 | Jobs, operations and controllers out of the gateway's process. | Check first; then into `worker` (controllers and operation workers are already leased, so they move by configuration, D50). | §6.1 |
| O24 | When to move to Container Apps. | With the first separate deployable. | §6.1 |
| O26 | Where deployables live; fakes at runtime. | `gateway/cmd/<name>`; the `local` root runs the fakes and emulators, no other root does. | §6.5 |
| O28 | Every name in the catalogue. | For review. | §7 |
| O29 | Merged capabilities: keep only where sources share one result schema. | Yes. | §7 |
| O30 | The owner of each domain. | To name. | §7 |
| O31 | Mondrian, Quantum, QuantumLeap: own systems or sources. | Sources. | §7 |
| O33 | Where the conformance gateway is built. | Go, in `sdk/go`, on `sdk/go/protocol`. | §9.8 |
| O34 | The remaining vectors (grammar, schemas, headers, Arrow) and profile vectors (resource, action, event). | Before any client is changed to follow §9. | §9.8 |
| O36 | The Snowflake spike: reachability of Snowflake's result storage (laptops, Nautilus Cloud), chunk URL lifetime, Azure download headers, decoding fed from the handoff document, exact decimals, `TIMESTAMP_NTZ`, ADBC against the native connector, throughput of each path. | Run before building the handoff. | §2.2.1 |
| O37 | An Azure storage integration and external stage for `COPY INTO` exports. | Yes, in `fi-infra`, after the first two paths. | §2.2.1 |
| O38 | Calling the four kinds of capability “surfaces” in the protocol too (renaming `family`). | Not now: “surface” in prose, `family` on the wire. | §2 |
| O39 | The first set of traits; a new trait as a protocol minor version. | Yes. | §2.1 |
| O40 | The CLI's Go module name, and a future harness command `catalog` colliding with `atlas catalog`. | The module is already `atlas.local/clients/cli` in `clients/cli`. Reserve `package catalog` for a future harness catalogue; `atlas catalog` discovers capabilities. | §5.3 |
| O42 | One admin request for every delegated permission and consent: Azure Resource Manager (Resource Graph), Service Bus and Storage for the clients' and the gateway's apps, Microsoft Graph for SharePoint. | One request, sent once, explaining each is delegated and narrowed by the person's own roles. | §6.3.5 |
| O43 | Astronomer's credential per environment (a Workspace or Organization API token in Key Vault). | Yes, read-only scope. | §2.2.2 |
| O41 | Replacing the Node SDK's part generated from the old Desktop's `ui.json`. | Yes, with the profile-driven core. | §5.2 |
| O45 | Extras that read what a capability also serves (the Go SDK's `azure` and `astro` adapters, for `qfi`). | Allowed for a person's own tools with their own access; an application for other people calls the capability. | §5, §5.4 |
| O46 | Nautilus Desktop embedding the Node SDK for its dashboards and their extras. | Yes; to confirm with Nautilus. | §5.2 |
| O47 | Which Airflow helpers the Python SDK carries. | The hook and operator only. | §5.1 |
| O48 | When the Node SDK takes handoffs. | After Python's; streaming and `export` first. | §2.2, §5.2 |
| O49 | A normative provider protocol per family, with schemas, vectors and a conformance runner any team's service can be driven by. | Yes: a part beside §9, written once the service protocol's transport and token (O21, O22) are decided. | Vision, §6.1 |
| O50 | A registry of domains: who may claim one, and its owner. | A registry the front door reads at registration; a claim approved by Atlas's owners. | Vision, §2.1 |
| O51 | Administration delegated to each domain's owner (grants and limits within the domain). | Yes, never above the platform's ceilings. | Vision, §6.2 |
| O52 | How a service in a team's own subscription is trusted and reached. | Its own Entra identity, admitted at registration; reached by private endpoint; the front door's signed token on every call. | Vision, §6.1 |
| O53 | The general Atlas spec apart from one instance's (QFI's catalogue, environments and sources). | Yes, once a second team is near. | Vision |
| O54 | The Snowflake ticket's credential (today the gateway's own token: the listed exception). | The person's own token by OBO now; a read-only scope on Snowflake's app next. | §6.3 |
| O55 | Whether Astronomer accepts a delegated identity. | Ask Astronomer; until then, run as the gateway. | §6.3 |
| O56 | The role journeys, their expected answers, and the agent-readiness check for profiles. | One journey per role first, run on every release against Development. | Vision |
| O57 | Names for files and uploads (`file.*`, `dataset.upload`) and SharePoint's upload. | For review, with the files and uploads part. | §6.3 |
| O58 | Atlas beside Accio (the firm's MCP router): third-party tools through Accio for speed; the firm's data and workflows through Atlas; a tool graduates to Atlas on its own API when its agent experience matters, and leaves Accio. | As stated; confirm with the Accio team how it describes tools and acts for the person. | Vision |
| O59 | The first ten role-journey questions, their expected answers, and the working view's token budget. | Ten everyday questions from the desks; a budget measured from the first catalogue. | Vision |
| O61 | The client engine in the cloud: the gateway shapes every answer for its caller (version, types, format) with the same `sdk/go` engine code, statelessly per request; very large results go out as Parquet links, never through the engine; the SDKs become thin (§5). | After Phase 2, decided on the M9.5 load test and the same test against Development. | §5, §5.1, §9.5.6 |
| O60 | Caching results in the gateway. | Per caller and in memory for `Licensed` capabilities; shared across callers only for unlicensed data, keyed by address, request, fingerprint and as-of date. | §6.2, §2.2.2 |
| O62 | A second audit container, in another storage account, written when the first cannot be: today an hour's outage of the audit's storage stops every call. | Yes: written by the same `AuditSink`, merged by `request_id` when shipped; an infrastructure change in `fi-infra`. | §6.1, §6.1.16, §6.2, §6.2.8 |

## Go deeper

- The design session's record: `kb/sessions/2026-10-28-repository-reset/atlas-design.md`.
