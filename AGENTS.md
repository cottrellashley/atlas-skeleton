# AGENTS.md: how to work in this repository

This is the Go core of Atlas: the gateway, the Go SDK, the CLI, the Python SDK's engine, and the conformance suite. These rules bind every person and agent who changes it.

## Read first

1. The spec, [`docs/source/atlas-spec/`](docs/source/atlas-spec), and its design review (`docs/source/atlas-spec/review/`, adopted in `docs/pass-2/REVIEW.md`). The plan for the handoff is [`docs/HANDOFF.md`](docs/HANDOFF.md); current evidence and remaining work are in [`docs/HANDOFF-PROGRESS.md`](docs/HANDOFF-PROGRESS.md). §9 is normative for everything on the wire. §6.6 holds the 15 principles, and §6.7 the mechanisms with their values. Where the spec and anything else disagree, the spec wins. Where the spec is ambiguous, write the case down; don't guess.
2. `docs/pass-2/PLAN.md` (what we are building, in which order) and `docs/pass-2/CRITIQUE.md` (what pass 1 got wrong).
3. The shared serving mechanisms live in [`sdk/go/kit`](sdk/go/kit). Their integration rules are:
   - every interaction is a POST to its binding
   - desired fields sit at the top of a record, not under `spec`
   - states are lowercase, with `UNKNOWN`
   - modules register in one list, never in `init`
   - authentication, limits and idempotency are pipeline steps, not middleware
   - the codes are `idempotency_conflict` 409 and `precondition_failed` 412

## Project modules resolve locally

Atlas's module paths (`atlas.local/...`) identify directories in this repository. They are not network locations:
- Never fetch or install a project module from a remote source. Build from the workspace: `go -C <module> install ./cmd/...`, with the Makefile's environment.
- A module that needs another in-repo module requires it at `v0.0.0`, with a `replace` to its relative directory, and every module is in `go.work`. `tools/modcheck` proves both.
- The separate example company module is also local, through `go.work` and relative replacements.
- The Makefile exports `GOPRIVATE=atlas.local` and `GOVCS=atlas.local:off`, so a missing replacement fails locally. Public third-party dependencies keep their published module paths and checksums.

## Keep changes bounded

Follow [the lessons and pattern table](docs/LESSONS.md). Extend existing providers and ports for a selected integration; do not interpret the vision or this handoff as permission to refactor working code or implement every planned feature. Preserve the shared client contract, record unsupported mechanisms in the handoff ledger, and prove each targeted change. All necessary project context is in this repository.

## The constitution (§6.6.4)

1. If it has a name and a lifecycle, it is a resource with the envelope.
2. Callers declare what they want; the gateway converges. No call means "do step three".
3. The request path reads a snapshot, never the store. Bytes take the shortest path.
4. New behaviour is a new module in a namespace, never a condition in the core.
5. The contract comes first, and every change is classified.
6. If a client has to hard-code it, the gateway has failed to describe it.
7. A domain owns its data. Others reach it through its capabilities.
8. Dependencies point inward.
9. A cross-cutting concern is a step at a written position. A handler never repeats it.
10. The front door authenticates and decides. Everything behind it trusts the signed token.
11. Every write can be repeated. Every update is conditional.
12. Anything slower than its deadline is an operation, with a name, an end and a retention.
13. A dependency's failure costs its own capabilities, never the catalogue.
14. Every hop carries the same ids. The audit record is a schema.
15. One generator, one review, one conformance suite, one register of decisions.

## Mechanism rules (checked where a machine can)

| Rule | Checked by |
|---|---|
| Layers: a package imports only its own layer or below; adapters and services never import each other; services never import `core`; only a deployable imports an adapter; `protocol` imports only the standard library and Arrow; `kit` only `protocol` and the standard library | `tools/layercheck` with each module's `layers.toml` |
| No `init` functions: modules register in the deployable's one list | `atlasvet` noinit (ATLAS-PACKAGES-008) |
| Every goroutine starts through `kit/conc.Go`; a bare `go` statement only inside `sdk/go/kit` | `atlasvet` nobarego (ATLAS-MECH-012) |
| Settings are read once, in a composition root (`cmd/`), a `config`/`settings` package or a `settings.go` | `atlasvet` nogetenv |
| One error envelope: every failure is a problem written by `kit/problem`; never `http.Error` | `atlasvet` nohttperror |
| Every outbound call takes the context and is bounded by the call's deadline | Review |
| No unbounded input: bodies, headers, cursors, keys, buffers, goroutines, maps, queues | Review; the port suites |
| Retries only of retryable, replayable work, at one hop, within a budget | `kit/retry`; MECH-006 |
| No token, key, body or query string in any log, span, metric or audit record | MECH-014, ACCESS-008 tests |
| Metric labels from bounded sets (the address, never the path; the code, never the message) | MECH-015 |
| What declared equals what is imported | `go mod tidy -diff` (ATLAS-PACKAGES-004) |
| Code adapted from elsewhere keeps its licence header and is listed in its module's `THIRD_PARTY_NOTICES.md` | Review |

## Tests

- Test the spec's requirement ids. A test that proves a requirement names its id (`// ATLAS-ACCESS-003`) in the test file. `make requirements` builds the matrix from those names.
- Every port has a suite in `porttest`. Every implementation runs it: the fakes, the adapters, the emulators.
- Use `-race` always. Use goleak where goroutines start. Inject the clock (`testing/synctest` where it fits); never sleep to wait for an expiry.
- Fuzz every parser: grammar, Structured Fields, JSON, cursors, tokens, SQL.
- Fakes stand in for real systems at the same seam the real adapter uses. Never fake below the code you mean to test.

## Adding a capability

Write its profile first, under `profiles/<id>@v<N>.json` in its service's package. Lint it and classify it against the previous profile. Then:

1. Follow the existing [company extension](extensions/support) or [warehouse service](gateway/svc/portfolio) pattern. The planned `atlas-new` generator is not implemented; do not assume it is available.
2. Fill in what only a person can write: the statement, the mapping, the meaning.
3. Pass the family's battery, in process and over loopback.
4. Add its row to the blast-radius table, and its switch.
5. Answer the 15-question review (§6.6.3) in the pull request.

## Before you commit

Run `make check` and `make e2e`. They must pass: gofmt, vet, `atlasvet`, staticcheck, every Go test under `-race`, the layer and module checks, tidy, and the client acceptance journey. Commit only when asked. The historical implementation phases and their lessons are documented locally; no branch or external checkout is needed to understand the current code.
