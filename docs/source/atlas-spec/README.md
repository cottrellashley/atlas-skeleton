# Atlas specification knowledge base

- [Design review, proposed core and complete capability handbook](review/README.md) — analysis and proposals dated 2026-10-31, separate from the transcription below.
- [Frame-by-frame verification ledger](VERIFICATION-LEDGER.md)
- [Original random-sample audit](VALIDATION.md)

This knowledge base was rebuilt by comparing every section directly with
full-resolution frames from `IMG_2138.MOV` and `IMG_2139.MOV`. The
[verification ledger](VERIFICATION-LEDGER.md) records the source interval and
manual evidence for every completed file.

## Contents

1. [Overview](00-overview.md)
2. [Vision](00-vision.md)
3. [Terms and conventions](01-terms-and-conventions.md)
4. [The surface](02-the-surface.md)
   - [§2.1 Capabilities](02-1-capabilities.md)
   - [§2.2 The data surface](02-2-data-surface.md)
   - [§2.2.1 Snowflake data](02-2-1-snowflake-data.md)
   - [§2.2.2 Vendor API data](02-2-2-vendor-api-data.md)
   - [§2.3 Resources](02-3-resources.md)
   - [§2.4 Actions](02-4-actions.md)
   - [§2.5 Events](02-5-events.md)
   - [§2.6 Discovery](02-6-discovery.md)
5. [Contracts](03-contracts.md)
6. [Plugging in](04-plugging-in.md)
7. [Clients](05-clients.md)
8. [Inside the gateway](06-inside-the-gateway.md)
9. [The catalogue](07-the-catalogue.md)
10. [Design decisions](08-design-decisions.md)
11. [The Atlas Protocol](09-atlas-protocol.md)

The two largest chapters are split into linked subsection files:

- §6 has one file for each of §§6.1–6.7.
- §9 has one file for each of §§9.1–9.8.

The diagrams are reconstructed as Mermaid so their arrows, branches, feedback
paths and grouping remain visible. Tables use native Markdown. The verification
ledger names the source video interval used for every file.

See [Transcription notes](TRANSCRIPTION-NOTES.md) for scope and recovery details.
