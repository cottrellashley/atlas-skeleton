# Handoff progress

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

## Decisions taken

| # | Decision | Why | Recorded in |
|---|---|---|---|
| D-0 | Work from the in-repo references where the spec transcription is absent (superseded by D-1) | It was not on this machine at first | This file |
| D-1 | Take `docs/source/` by merging `origin/pass-2` into `handoff` | Ashley added it to `pass-2` mid-work; a merge rewrites nothing | This file |
| D-2 | R3-01: the scope evidence is the scoped values the contract *requires*; an optional scoped member may be absent | An existing test profile has an optional, nullable scoped member, and HANDOFF.md says evidence is refused "where scope applies" | A-195 |
| D-3 | R3-03: send `scope` with `client_id` | Entra v2 lists both for a refresh; the scopes are the binding's own | A-196 |
