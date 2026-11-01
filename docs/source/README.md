# Atlas specification and engineering studies

These documents are part of this repository. They describe the product vision, protocol, implementation patterns, reviews and the evidence behind the implementation plan.

| Folder | What it contains |
|---|---|
| [`atlas-spec/`](atlas-spec) | The Atlas specification, chapter by chapter (`00-overview.md` to `09-8-conformance.md`). §9 is normative for the wire. The validation and verification notes distinguish documentation coverage from implementation proof. |
| [`atlas-spec/review/`](atlas-spec/review) | The design review, capability handbook, integration boundaries, peer findings and core handoff plan. [`../pass-2/REVIEW.md`](../pass-2/REVIEW.md) records the adopted review decisions. |
| [`atlas-proof-plan.md`](atlas-proof-plan.md) | The historical proof plan: hypotheses, acceptance criteria and the distinction between hermetic and live-service evidence. |
| [`digests/`](digests) | Technical summaries and gap studies, including historical implementation findings. Their README explains which parts remain useful and where to find current status. |

For current implementation status, read [`../HANDOFF-PROGRESS.md`](../HANDOFF-PROGRESS.md). Historical studies and proposed architecture do not establish that a feature is implemented. Build instructions use repository-local modules and tools; public third-party dependencies and their notices remain in their owning modules.
