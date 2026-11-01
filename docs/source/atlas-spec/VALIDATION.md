# Documentation validation lessons

An earlier documentation draft failed 12 of 14 sampled editorial checks. Missing table columns, repeated fragments, identifier punctuation and diagrams flattened into unordered text changed technical meaning. Those failures triggered a complete chapter review. The table preserves the findings and corrections so future edits can avoid the same mistakes.

These are documentation checks, not passing implementation tests. Current implementation evidence belongs in the [handoff progress](../../HANDOFF-PROGRESS.md); chapter coverage is listed in the [document-review ledger](VERIFICATION-LEDGER.md).

| Material checked | Earlier draft result | Correction |
|---|---|---|
| Agent flow: “An agent” → “find, understand, call, combine” | Fail | Rebuilt the overview flows as Mermaid rather than detached labels. |
| “Agents build tools on the gateway” and the five-row SDK table | Fail | Restored the missing **Built on** column and all row relationships as a Markdown table. |
| “Names by family” table | Fail | Restored all four families, descriptions, and examples. |
| §2.2 data-surface summary | Fail | Corrected `bytes`, `handed off`, `ticket`, `SQL`, and the paragraph order. |
| Snowflake type mapping and execution-spike questions | Fail | Rebuilt both tables; restored `TIMESTAMP_NTZ`, `gosnowflake`, ADBC and the missing fallback text. |
| Discovery requirements `ATLAS-DISCOVERY-001`–`010` | Fail | Corrected requirement identifiers and `/.well-known/atlas`. |
| `data.landed` event profile | Fail | Reconstructed the JSON object, including bindings, schemas, meaning and compatibility. |
| §6.4 DataSource operations and adapter table | Fail | Rebuilt the eight-operation port table and both adapter rows. |
| Six-layer package diagram | Fail | Reconstructed the dependency flow from L6 deployables through L0 protocol and kit. |
| `model-portfolio` capability table | Fail | Restored both capability rows and all nine columns. |
| `Research · research` heading, purpose and requirement transition | Pass | No correction needed for the reviewed prose. |
| Design decisions D13–D22 | Fail | Corrected the `atlas.access` identifier; the decision table remains a table rather than a diagram. |
| §9.2 schema nullability, rules and AP-SCHEMA requirements | Pass | No correction needed for the reviewed requirements. |
| AP-DOC-003 and §9.4 Fingerprints opening | Fail | Corrected `resource.schema.json`, `ETag`, the source path, section summary and purpose. |

## Diagram checks

The architecture diagrams preserve these flows as editable Mermaid:

1. Atlas overview: clients, gateway, services, sources, extras and direct handoff.
2. Capability surfaces and data sub-profiles.
3. Data execution and delivery: streamed, handoff and export paths.
4. Action lifecycle: plan, approval, run and terminal states.
5. Contract declaration, governance, availability, gateway and reader resolution.
6. Version-change classification: safe, dangerous and breaking.
7. Writer-to-reader resolution and lens direction.
8. Caller/client/gateway call sequence.
9. Client families, shared Go engine, extras and gateway paths.
10. Gateway front door, state, audit, services, adapters and event feed.
11. Gateway package layers L6 through L0.
12. The remaining chapter-level architecture, sequence, state, decision and dependency flows listed in the document-review coverage.

Mermaid source is used so the arrows and grouping remain editable and render consistently in Markdown viewers that support Mermaid.
