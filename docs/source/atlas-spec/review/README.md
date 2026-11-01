# Atlas: design review and proposed simplification

Atlas should let an agent discover what a person can do, understand the result to expect, and perform the work through one interface. The agent explores with the CLI; the program it builds uses the same capability, request and result contract through Python or Node.

That is the right centre for Atlas. The strongest parts of the existing spec are its domain capabilities, permission-aware discovery, self-describing contracts, consistent clients and central enforcement. The main difficulty is that it also commits to a large protocol and distributed runtime before several of their guarantees are consistent or proven.

This review proposes a smaller implementation around that same product. It is a proposal dated **2026-10-31**, separate from the video transcription. It does not change the existing requirements by implication.

## Latest implementation review and handoff plan

- [Peer review — 1 November 2026](PEER-REVIEW-2026-11-01.md): a fresh read-only check of the local Atlas implementation, including prior-finding status, new defects and the remaining extension gaps.
- [Core handoff plan](CORE-HANDOFF-PLAN.md): ordered work and acceptance gates for companies to add services without changing Atlas's shared core or clients.

These additions assess the implementation after the original design proposal. The proposal and its original evidence below retain their 31 October date and scope.

## Read in this order

1. [The proposed Atlas core](ATLAS-CORE.md): a clean description of the product, capability contract, client behavior and implementation boundaries.
2. [The capability handbook](CAPABILITY-HANDBOOK.md): all 138 catalogued capabilities, what they are for, common usage patterns, and the details still needed before their profiles are complete.
3. [The design review](DESIGN-REVIEW.md): concrete contradictions, consequences, recommended resolutions and evidence.
4. [The required integrations](INTEGRATIONS.md): how the two existing Go repositories enter the first implementation, what they already supply, and what Atlas must add.

## Constraints carried into the proposal

- [cloud-gateway-patterns-go](https://github.com/cottrellashley/cloud-gateway-patterns-go) is a required integration from the first implementation.
- [sqlglot-go](https://github.com/cottrellashley/sqlglot-go) is a required integration from the first implementation. It is the Go SQL library behind Atlas planning and validation.
- [bt-research](https://github.com/cottrellashley/bt-research) is a future capability implementation. Its internals, interface and requirements are deliberately outside this review.
- CLI, Python and Node share capability names, request fields, logical types, effects, operation semantics and problems. Differences in language syntax and optional transport acceleration must not change business meaning.
- The handbook must say what a capability does, when to use it, what to supply, what to expect and how to recover. A source table name alone is insufficient documentation.

## The decisions with the greatest impact

| Decision | Recommendation |
|---|---|
| Where complexity belongs | Put domain and source knowledge in capability providers; keep the agent's workflow and the three public interfaces small. |
| First working release | Prove a complete user journey through CLI, Python and Node, with both required repositories integrated. |
| Compatibility | Start with conservative additive evolution; keep old meanings available. Do not promise arbitrary reversible schema transformations. |
| Identity and relevance | Resolve identity and authorized scope centrally. Describe defaults and relevance separately from authorization. |
| Contract versus live policy | Cache immutable contracts independently from descriptions, routing, access, usage and health. Enforce current policy on every execution. |
| Delivery | Stream by default; add a direct result path only after its access, decoding and expiry behavior is demonstrated. |
| Distributed services | Keep a provider boundary from the beginning; start in one deployable unless a concrete integration requires isolation. |
| Capability documentation | Generate the catalogue, human guide and client examples from the same authored capability definition. |

## What this review establishes

The review covered all 34 substantive Markdown chapters in `atlas-spec/`, including the complete catalogue, implementation sections, decision register and protocol. It also examined source code and documentation in the two required repositories at the commits recorded in [Integrations](INTEGRATIONS.md).

The existing [proof plan](../../atlas-proof-plan.md) and local [Proof 0 report](../../../../docs/pass-1/PROOF-0.md) supply additional context. The reported hermetic results are useful evidence; they were not rerun during this review and are not proof of real Snowflake, Entra, Cosmos DB or Service Bus behavior. This review does not revalidate the video transcription.

The supplied specification directory contains Markdown, not the referenced protocol schemas and conformance vectors. The catalogue contains 138 entries, not 138 complete executable profiles. Those are concrete remaining deliverables, rather than details this review can safely fill in from names.
