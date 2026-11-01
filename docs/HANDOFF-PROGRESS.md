# Handoff progress

The log of the work on branch `handoff`, made from `pass-2` at `7f876ea`, following [HANDOFF.md](HANDOFF.md). Each package records what was done and the tests that prove it, what remains, the decisions taken, and the next step. "Hermetic" means proved against fakes, DuckDB and fixtures on this machine; nothing here has run against a real company system.

## Set-up, 2026-11-01

**The machine.** Arch Linux (Omarchy), kernel 7.2.5, x86_64. go1.25.14, Python 3.14.7, uv 0.12.10, Node v26.8.2, GCC 16.2.1. `gh` is signed in as cottrellashley.

**Baseline.** On a fresh clone, at `7f876ea`, before any change:
- `make check` passes (3 min 44 s). Nothing needed changing for Linux: DuckDB's prebuilt `linux-amd64` library links through cgo with GCC 16 as it does on macOS.
- `make e2e` passes (22 s): Python, Node (73 tests) and the six acceptance points.

**Not on this machine.** The spec transcription and the review documents (`~/Documents/ChatGPT/creation/atlas-spec/`, `docs/source/`) are absent. The work reads the spec through the in-repo references (`spec/protocol/references/`), `REVIEW.md`, `ASSUMPTIONS.md` and `HANDOFF.md`. Where a decision would need the spec's own text, the most conservative reading the references support is taken and recorded below. `make requirements` needs the spec, and is not run.

**Git.** `handoff` was made from `origin/pass-2` and its upstream unset, so that a bare `git push` can never reach `pass-2`. It is pushed explicitly, as `git push origin handoff`.

## Decisions taken

| # | Decision | Why | Recorded in |
|---|---|---|---|
| D-0 | Work from the in-repo references where the spec transcription is absent | It is not on this machine | This file |
