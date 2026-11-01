# Atlas

Atlas gives agents and applications one interface to discover, understand and call capabilities backed by warehouses, APIs and other services. The gateway owns identity, access, contracts and call accounting. Providers use the upstream platform's SDK or API and preserve its native behavior.

This repository contains the implementation, specification, reference fixtures, design decisions, lessons and remaining work. Every project-owned Go module resolves to a directory in this workspace. Public third-party dependencies remain declared in the module and package manifests; building requires normal package downloads and installed toolchains.

## Start here

| Read | Purpose |
|---|---|
| [Vision](docs/source/atlas-spec/00-vision.md) and [platform overview](docs/source/atlas-spec/00-overview.md) | The domain and intended experience |
| [Handoff plan](docs/HANDOFF.md) | Architecture, extension boundaries and acceptance gates |
| [Implementation progress](docs/HANDOFF-PROGRESS.md) | What is implemented, what is proven and what remains |
| [Lessons](docs/LESSONS.md) | Engineering rules and explicit pattern choices; preserve working implementations |
| [Integration guide](docs/pass-2/PLUGGING-IN.md) | Run locally, configure adapters and add a capability |
| [Protocol](docs/source/atlas-spec/09-atlas-protocol.md) and [reference fixtures](spec/protocol/references/README.md) | The contract shared by every client |
| [Agent instructions](AGENTS.md) | How to extend this code and verify changes |

The gateway, CLI, Go client, Python client and Node client are implemented for the documented read-only journey. The current progress ledger records remaining client correctness work and the unfinished handoff gates, durable operations and packaging milestones. Included design documents describe both implemented and planned behavior; they are not evidence that every proposed capability is available.

## Build and verify

Use Go 1.25, Python 3.12 or newer, `uv`, Node 23.6 or newer, and a C compiler for the local DuckDB gateway. The production gateway builds without cgo. Run from the repository root:

```bash
make check          # formatting, vet, static analysis, race tests, module checks and production builds
make e2e            # Python, Node and cross-client acceptance against the local fixture gateway
make requirements   # map the local specification's requirements to test evidence
make handoff        # source archive with a file manifest and checksum in dist/
make help
```

Use `make` for Go commands so the workspace namespace is kept local. No account, production credential or external project checkout is needed for the fixture tests. Public dependency downloads are permitted. Tests against real upstream systems require separately configured credentials and are not implied by local test results.

## Repository map

| Path | Responsibility |
|---|---|
| [gateway](gateway) | Ports, pipeline, access, registry, providers, adapters and HTTP edge |
| [gateway/cmd/gateway](gateway/cmd/gateway) | Production composition root |
| [gateway/local](gateway/local) | Development gateway using DuckDB and synthetic fixtures |
| [sdk/go](sdk/go) | Go client, protocol, shared serving kit and Python's engine |
| [clients/cli](clients/cli) | Agent-facing command-line client |
| [clients/python](clients/python) | `fiatlas` Python client |
| [clients/node](clients/node) | `@atlas/sdk` Node client |
| [extensions/support](extensions/support) | Separate company-module integration example |
| [sqlglot](sqlglot) | In-repository SQL parsing and generation module |
| [spec](spec) | Requirements and independent protocol fixtures |
| [docs](docs) | Vision, specification, design, implementation evidence and lessons |
| [tools](tools) | Architecture checks, requirements reporting and source packaging |

## Handoff contents

`make handoff` packages the project code, all Markdown guidance, manifests, tests and license notices under one `atlas/` directory. It excludes Git metadata, local environments, caches and compiled output. Extract it anywhere and follow this README; no original checkout location is required. The archive is a source handoff with public dependencies, not an offline dependency bundle.

Third-party terms remain in [the SDK notices](sdk/go/THIRD_PARTY_NOTICES.md), [the SQL module license](sqlglot/LICENSE), and the notices beside their applicable fixtures. Protocol schema URIs are stable contract identifiers resolved from the included schemas; they are not instructions to fetch another repository.
