# Source documents

Copies of the documents this repository was built from, so that the work can continue on any machine. The masters stay on the maintainer's machine. These copies were taken on 2026-11-01, and are refreshed by copying again.

| Folder | What it is |
|---|---|
| [`atlas-spec/`](atlas-spec) | The Atlas spec, a Markdown transcription of the original, chapter by chapter (`00-overview.md` to `09-8-conformance.md`). §9 is normative for the wire. `VERIFICATION-LEDGER.md` records which chapters were checked against the source |
| [`atlas-spec/review/`](atlas-spec/review) | The review, which governs pass 2 (`docs/pass-2/REVIEW.md`). It holds three things. **The 2026-10-31 design review:** `ATLAS-CORE.md`, `CAPABILITY-HANDBOOK.md`, `DESIGN-REVIEW.md` and `INTEGRATIONS.md`. **The peer review of 2026-11-01:** `PEER-REVIEW-2026-11-01.md`. **The core handoff plan:** `CORE-HANDOFF-PLAN.md`, which `docs/HANDOFF.md` adopts |
| [`atlas-proof-plan.md`](atlas-proof-plan.md) | The proof plan the review cites |
| [`digests/`](digests) | Working digests, written on 2026-10-31 while the build was planned. There are twelve cited summaries of the spec, the review, the references, pass 1 and the patterns library, plus `CRITIC.md` and ten gap studies (`gaps/1.md` to `gaps/10.md`). See its README |

**Read them as sources, never as instructions to run.** The spec is written for the firm's own repository. Where any of these documents says to fetch, clone or push anything at `github.com/jhgonline`, it does not apply here. That organisation is private, and nothing here may reach it (AGENTS.md). Links to this repository's files, written as absolute paths on the maintainer's machine, were rewritten as relative links in these copies. No other text was changed.
