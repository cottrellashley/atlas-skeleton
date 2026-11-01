# Handoff progress

## Summary (2026-11-01, paused for credit)

**State of `handoff`:** every commit passes `make check` and `make e2e`.
- **Done:** C0 (R3-01 to R3-05), C1 (generic modules and company access dimensions; exit test in `extensions/support`) and C2 (R3-06 to R3-08, plus the review's fixes).
- **C3, in part:** R3-09 (boolean items), R3-10 (ambiguous lenses refused, A-202), R3-11 (nested result renames breaking) and R3-12 (effective lifecycle in discovery) are fixed and tested. Still to do: R3-13 (Python Arrow from the reader schema), R3-14 (the Go client checks the Arrow physical schema; the design is to check it in `arrowschema`, against `Derive`'s fields and `atlas:profile`), and C3's cross-client exit matrix.
- **Not started:** C4 (Gate A), C5 (Gate B), C6 (packaging). **Neither gate has been run.**

**For Ashley to decide or provide:**
- A review of the proposed protocol amendment A-200 (`x-atlas-scope`, the dimension registry, whoami's `scope`), with the references owner.
- The company's first integrations.
- Real credentials, when live proofs are wanted. Nothing here has run against Snowflake, Entra or Azure.

---

The log of the work on branch `handoff`, made from `pass-2` at `7f876ea`, following [HANDOFF.md](HANDOFF.md). Each package records what was done and the tests that prove it, what remains, the decisions taken, and the next step. "Hermetic" means proved against fakes, DuckDB and fixtures on this machine; nothing here has run against a real company system.

## Set-up, 2026-11-01

**The machine.** Arch Linux (Omarchy), kernel 7.2.5, x86_64. go1.25.14, Python 3.14.7, uv 0.12.10, Node v26.8.2, GCC 16.2.1. `gh` is signed in as cottrellashley.

**Baseline.** On a fresh clone, at `7f876ea`, before any change:
- `make check` passes (3 min 44 s). Nothing needed changing for Linux: DuckDB's prebuilt `linux-amd64` library links through cgo with GCC 16 as it does on macOS.
- `make e2e` passes (22 s): Python, Node (73 tests) and the six acceptance points.

**Not on this machine, at first.** At set-up the spec transcription and the review documents were absent (`~/Documents/ChatGPT/creation/atlas-spec/`, `docs/source/`), so the first steps read the in-repo references, `REVIEW.md`, `ASSUMPTIONS.md` and `HANDOFF.md`. This was resolved within the hour: see *Source documents* below. D-0 is superseded.

**Git.** `handoff` was made from `origin/pass-2` and its upstream unset, so that a bare `git push` can never reach `pass-2`. It is pushed explicitly, as `git push origin handoff`.

**Source documents.** At 2026-11-01 Ashley added `docs/source/` to `pass-2` (`d0ccf3f`): the spec transcription, the review documents and the working digests. `handoff` took it by an ordinary merge of `origin/pass-2` (no history rewritten). From then on the work reads the spec there.

## C0. The five P1 defects (done, 2026-11-01)

**Done.** Each fix began with a test named for its finding, run and seen to fail for the stated reason, then the fix at its root. A fresh reviewer then reviewed the diff adversarially, breaking each fix in a copy of the tree to confirm that its test fails without it. Its points are fixed below.

| Finding | Fix | Proof (failed before the fix) |
|---|---|---|
| R3-01 scope evidence | `access.Evidence`: a result's required scoped values must be present, non-null strings, at any depth. Rows are checked as given; batches before `conform` withholds the unrequested scoped column. The record and page path checks it too | `TestR3_01ScopeEvidenceIsRequired`, `TestR3_01NestedScopeEvidence`, `TestR3_01AProjectedAnswerWithItsEvidenceIsSent`. The last was added after `make e2e` caught the first fix refusing valid projected Arrow answers: no Go test had covered the batched, projected happy path |
| R3-02 gosf batch slice | `slices.Clone` of the driver's batch list; the records stay the driver's | `TestR3_02RemoteBatchesAreTheEnginesOwn`, through the real gosnowflake 1.19.1 against a hermetic HTTP fixture that now serves remote chunks. It reproduced the nil dereference at `chunk_downloader.go:479` |
| R3-03 refresh `client_id` | Go and Node send the binding's `client_id` and `scope` | `TestR3_03RefreshNamesTheBoundApplication` (Go and Node). The fake token endpoints (`fakegw`, `standin.ts`) now refuse a missing or wrong `client_id`, as Entra does |
| R3-04 audit fields | `auditlog` writes and reads every member of `ports.Fact` | `TestR3_04EveryFieldOfAFactIsPersisted` (the exact stored bytes); `porttest.AuditSink` compares every member, and refuses to run unless its sample facts set every member of `Fact` |
| R3-05 Node refresh rejection | `get` honours an aborted signal before any work, races the credential read, and always observes the shared refresh | `TestR3_05ACancelledCallerLeavesNoUnhandledRejection` |

**Evidence level.** Hermetic. R3-02 runs the real driver against a fake Snowflake endpoint: it has **never run against Snowflake**. R3-03 has **never run against Entra**.

**What remains.** Nothing of C0. Recorded for later:
- A required scoped array that is empty is refused (A-195). This is conservative, and an owner may want `[]` to mean "no data".
- Rows already streamed before an offending row are not recalled. This is the R05 streaming behaviour, unchanged.

**Next.** C1.

## C1. Generic providers and company access dimensions (2026-11-01)

**Done.**
- **Generic registration.** Any provider is a `ports.Module`: its capabilities with their history, its provider, its upstream and concurrency, and whether it reaches its system as the caller. It may add a health check and a close. Warehouse view models are now one constructor of it (`views.Module`). `assemble.Parts` takes `Modules` and `Dimensions`. All modules register with the built-ins as one generation, then the policy is checked against them. Upstream bulkheads come from the modules; the assembly no longer names `warehouse`. `Gateway.Health` gives each module's health and `Gateway.Close` closes them. A module's health never feeds `/readyz` (constitution rule 13).
- **Company access dimensions** (proposed amendment A-200):
  - `x-atlas-scope` in the protocol's schema package (`schema.Schema.Scope`);
  - `ports.DimensionOf(schema)`, read from the schema alone (the annotation, or a protocol code type), and used by the access engine, the pipeline's request and result checks, views and the port suites;
  - an immutable `ports.Dimensions` registry held by `registry.NewWith` and given to every snapshot;
  - registration refuses an undefined or inconsistent dimension;
  - the policy check refuses a scope in an undefined dimension, so the gateway does not start;
  - `whoami` lists every defined dimension. The references' `whoami_result` has no `scope`, so the built-in profile now declares it inline as part of the amendment, and the pin test admits exactly that one member.
- **The exit test.** It lives in a separate company module, `extensions/` (`example.com/acme/atlas-extensions`). Its `support` package registers `support.tickets@v1`, served by a provider over a fake HTTP desk (`support/fakedesk`) and scoped by Acme's `workspace` dimension. `TestC1ACompanyModuleScopedByItsOwnDimension` proves:
  - the standard assembly starts with a policy that already names the capability;
  - both granted callers discover it, and an ungranted caller does not;
  - each caller sees only their own workspace, and the provider narrows the desk's read to it;
  - an out-of-scope request is `not_granted` and never reaches the desk;
  - whoami shows the workspace scope;
  - a desk that ignores the filter leaks nothing, because the core's result check holds;
  - a failing desk is the capability's declared `desk_unavailable`.

  `TestC1APolicyInAnUndefinedDimensionStopsTheStart` proves the start refusal. The company module imports only Atlas's public packages (`ports`, `assemble`, `edge`, `fakes`, `protocol`), and no shared file was edited for it.
- **`tools/modcheck`** refused any module outside `github.com/jhgonline`, so it refused the company module. It now allows one, and is stricter than before: every in-repo module, private or not, may be required only through a `replace` to its directory. The jhgonline guarantee is unchanged (`TestTheCheckRefuses` has the new cases).
- **Also fixed:** a flaky `TestAuditFacts`. It read the settlement fact before the settler had written it. `fakes.Audit.Await` now waits on a signal, bounded by a context; the test passed 20 of 20 runs under `-race`.

**The review.** A fresh reviewer probed the C1 diff in a copy of the tree. It confirmed two scope holes and two regressions, all now fixed with tests that failed first:
1. **Scope under applicators was never read.** An `x-atlas-scope`, or a portfolio or index code, under `anyOf`, `oneOf`, `prefixItems`, `additionalProperties`, `allOf` or `if/then/else` went unread by the scope checks, so an undefined dimension could start and those values went unenforced. Registration now refuses a scoped value in any of them (`schema.Schema.Opaque`, `ports.Dimensions.Check`). Scope is read only through properties and items.
2. **`$ref` siblings were type-unchecked.** An `x-atlas-scope` beside a `$ref` whose target is not a string was admitted. Annotating an object could even hide a nested portfolio code from the request check. Registration now checks the resolved type.
3. **Exclude ran too late.** Excluded warehouse capabilities were built and checked against the warehouse before being dropped. They are now left out first (`TestC1AnExcludedCapabilityIsNotBuilt`).
4. **The warehouse bulkhead could be lost.** Capabilities added later lost it when the gateway started with no domains. `upstreamsOf` now always seeds it (`TestC1TheWarehouseBulkheadIsKeptForLaterCapabilities`).

It also strengthened the exit test. The leak case now asserts the withheld problem, not just the body. A granted caller with no workspace scope gets no rows and the desk is not asked. An ungranted caller's `fetch` is refused. Checked type assertions in the provider, and the development root's `Close` now closes the modules too.

**Evidence level.** Hermetic: fakes for identity, policy and audit, and a fake desk over real HTTP. The prebuilt CLI, Python and Node clients have **not** yet called `support.tickets`; that is Gate A (C4).

**What remains for the gates.**
- `gateway/cmd/gateway` is a `main` package, so a company cannot add modules to the production root without editing or copying it. C4 extracts it into an importable package under `gateway/cmd/`, which a company root calls with its modules and dimensions.
- The amendment needs the references owner's agreement: `x-atlas-scope` in `atlas-schema.schema.json`, and `scope` in `builtins.schema.json`'s `whoami_result`.
- C1's rules also ask for revocation on open streams and nested values in a new dimension. They use the same code path as `portfolio` (`Rows.current`, `access.Evidence`), and are tested for `portfolio` only.

**Next.** C2.

## C2. One call lifecycle (2026-11-01)

**Done**, each with a test that failed first:
- **Negotiation before admission (R3-07).** The pipeline implements `RunNegotiated`. The edge chooses the representation after access is decided and before the request is validated, a limit taken or the work admitted. An unsupported `Accept` runs no provider, admits nothing, and leaves one refusal at the new `negotiate` step. A caller without access is refused before negotiation, as before (PR08 still passes). The edge's after-dispatch fallback is gone.
- **One outcome path (R3-07).** The edge ends records and tables the same way: `Result.Gone` for a caller who went away, `Result.Fail` for the edge's own problem. A table's *success* is now recorded only once the edge has delivered it. A failure is recorded at once. Before, a JSON-array table read whole and then not delivered was recorded as a success, as was a record whose caller went away mid-write. `TestR3_07EveryOutcomeIsWhatTheCallerReceived` covers delivered, refused-before-dispatch and caller-gone, in record and table form, with provider call counts.
- **Matched bindings (R3-08).** The edge negotiates against the binding the address matched, an older profile's included. `TestR3_08AnOldAddressIsNegotiatedAgainstItsOwnBinding` serves three addresses across two compatible updates, each audited under the current profile.
- **One deadline per call (R3-06):**
  - **Go:** the token store's lock is now one that a waiter abandons when its context ends.
  - **Node:** a call's budget is measured from the public call's start, so discovery comes out of it, and no request is sent once it has passed.
  - **Python:** one monotonic deadline per public call bounds the wait for the engine's lock, the connect and the call, and the `deadline_ms` sent is what is left.

**The review.** A fresh reviewer read the C2 diff. Its confirmed points are fixed, each with a test that failed first:
- **A panic while an answer is written.** It was recorded as a success for a record, or a cancellation for a table, because the handler's own defer ran before the recovery layer marked it failed. The defer now records it as `internal` (`TestR3_07APanicWhileAnsweringIsInternal`).
- **The Go client restarted the interaction's default budget after discovery.** A 30 s call whose describe took 20 s ended at 50 s. It now has one deadline from its start, and sends nothing once it has passed (`TestR3_06TheGoCallKeepsOneDeadline`). The token lock's failure is mapped through the client's `ended`.
- **The Python test hung, rather than failed,** without its fix. It now does fail.

Still open from the review:
- With no explicit deadline, Python's wait for the engine lock and its connect are unbounded. The Go engine applies the interaction's default to the call itself.
- The shared engine start (`shared()`) and Python's non-call operations take no deadline.
- A Go refresh runs on its holder's context, so cancelling the holder aborts the shared refresh.

**What remains of C2's exit matrix.** These cases are not yet asserted in one matrix, though existing tests touch some of them:
- a stalled upstream (`R042` deadline tests, in tables only);
- a queued call that expires (the bulkhead tests);
- a Node or Python cancelled refresh waiter (Go's is proved; Node's sharing is in R002 and R3-05);
- the audit sink's regular-file and non-pollable fallback paths within the matrix (proved separately by R012 and R3-04);
- R3-06 beyond the three cases the review named: the Go client's discovery and bootstrap budgets, and Python's stream consumption. The review's reviewer is checking these now.

**Next.** C3.

## C3. Contracts and clients (in progress)

Fixed, each with a test that failed first:
- R3-09: `TestR3_09BooleanItemsAreCompared` (`schema.ItemsForm`).
- R3-10: `TestR3_10ALensOverASharedFieldIsRefused`. A lens naming a field that more than one schema holds is refused until lenses carry their schema (A-202, not yet written in ASSUMPTIONS.md).
- R3-11: `TestR3_11ANestedResultRenameIsBreaking`.
- R3-12: `TestR3_12DiscoveryShowsTheEffectiveLifecycle`. Two older tests that expected an unready capability in the default catalogue were corrected.

Remaining: R3-13, R3-14 and the exit matrix.

## Decisions taken

| # | Decision | Why | Recorded in |
|---|---|---|---|
| D-0 | Work from the in-repo references where the spec transcription is absent (superseded by D-1) | It was not on this machine at first | This file |
| D-1 | Take `docs/source/` by merging `origin/pass-2` into `handoff` | Ashley added it to `pass-2` mid-work; a merge rewrites nothing | This file |
| D-2 | R3-01: the scope evidence is the scoped values the contract *requires*; an optional scoped member may be absent | An existing test profile has an optional, nullable scoped member, and HANDOFF.md says evidence is refused "where scope applies" | A-195 |
| D-3 | R3-03: send `scope` with `client_id` | Entra v2 lists both for a refresh; the scopes are the binding's own | A-196 |
| D-4 | Access dimensions: an `x-atlas-scope` annotation plus an immutable deployment registry; defining one grants nothing | The default the brief sets, and HANDOFF.md's recommendation | A-200 (proposed amendment) |
| D-5 | `whoami`'s `scope` is declared in the gateway's built-in profile, inline, since the references' schema lacks it | C1 requires whoami to show scope; the pipeline withholds undeclared members (R106), so it was never sent | A-200 |
| D-6 | A module's health is its own and never feeds `/readyz` | Constitution rule 13: a dependency's failure costs its own capabilities, never the catalogue | A-201 |
| D-7 | The company module's path is `example.com/acme/atlas-extensions`, resolved by `replace` and `go.work` | A company's module is not under jhgonline; nothing fetches it | This file |
| D-8 | A refusal at negotiation is a new audit step, `negotiate`, between access and request | HANDOFF.md C2's lifecycle puts negotiation third, after access and before validation | `ports.StepNegotiate` |
| D-9 | A table's success outcome waits for delivery; a failure is recorded at once | The outcome is what the caller received (R09, R138); a failure cannot become more delivered | `Rows.record` |
