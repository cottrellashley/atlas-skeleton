# Manual visual validation

This audit compares full-resolution frames from the two source videos directly with the Markdown. The timestamps were selected before viewing, across both videos. A pass means the sampled visible text is represented accurately; a fail means the sample contained a substantive transcription or layout error.

## Result

The original generated transcription did **not** pass validation: 12 of 14 sampled frames contained substantive errors. The failures included missing table columns, duplicated OCR fragments, incorrect punctuation in identifiers, and diagrams flattened into unordered text. Those failures triggered a complete manual pass over every target file and its full source interval. The table below preserves the original audit; current completion evidence is in the [verification ledger](VERIFICATION-LEDGER.md).

| Source frame | Visible material checked | Original result | Manual action |
|---|---|---:|---|
| `IMG_2138.MOV` 00:01:17 | Agent flow: “An agent” → “find, understand, call, combine” | Fail | Rebuilt the overview flows as Mermaid rather than detached labels. |
| `IMG_2138.MOV` 00:01:40 | “Agents build tools on the gateway” and the five-row SDK table | Fail | Restored the missing **Built on** column and all row relationships as a Markdown table. |
| `IMG_2138.MOV` 00:03:27 | “Names by family” table | Fail | Restored all four families, descriptions, and examples. |
| `IMG_2138.MOV` 00:03:30 | §2.2 data-surface summary | Fail | Corrected `bytes`, `handed off`, `ticket`, `SQL`, and the paragraph order. |
| `IMG_2138.MOV` 00:04:39 | Snowflake type mapping and execution-spike questions | Fail | Rebuilt both tables; restored `TIMESTAMP_NTZ`, `gosnowflake`, ADBC and the missing fallback text. |
| `IMG_2138.MOV` 00:06:39 | Discovery requirements `ATLAS-DISCOVERY-001`–`010` | Fail | Corrected requirement identifiers and `/.well-known/atlas`. |
| `IMG_2138.MOV` 00:08:14 | `data.landed` event profile | Fail | Reconstructed the visible JSON object, including bindings, schemas, meaning and compatibility. |
| `IMG_2139.MOV` 00:01:52 | §6.4 DataSource operations and adapter table | Fail | Rebuilt the eight-operation port table and both adapter rows. |
| `IMG_2139.MOV` 00:02:36 | Six-layer package diagram | Fail | Reconstructed the dependency flow from L6 deployables through L0 protocol and kit. |
| `IMG_2139.MOV` 00:08:43 | `model-portfolio` capability table | Fail | Restored both capability rows and all nine columns. |
| `IMG_2139.MOV` 00:09:22 | `Research · research` heading, purpose and requirement transition | Pass | No correction needed for the sampled prose. |
| `IMG_2139.MOV` 00:10:57 | Design decisions D13–D22 | Fail | Corrected the sampled `atlas.access` identifier; the decision table remains transcription content rather than a diagram. |
| `IMG_2139.MOV` 00:13:20 | §9.2 schema nullability, rules and AP-SCHEMA requirements | Pass | No correction needed for the sampled requirements. |
| `IMG_2139.MOV` 00:14:07 | AP-DOC-003 and §9.4 Fingerprints opening | Fail | Corrected `resource.schema.json`, `ETag`, the source path, section summary and purpose. |

## Diagram reconstruction

The visual flows were manually read from the video and rebuilt inline as Mermaid, including:

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
12. The remaining chapter-level architecture, sequence, state, decision and dependency flows listed in the verification ledger.

Mermaid source is used so the arrows and grouping remain editable and render consistently in Markdown viewers that support Mermaid.
