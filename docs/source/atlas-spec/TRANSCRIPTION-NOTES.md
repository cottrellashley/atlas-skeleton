# Specification maintenance notes

This directory is the repository-local specification. Its 34 substantive chapters preserve the product vision, domain concepts, requirement identifiers, contract examples, architecture diagrams, design decisions and open questions needed to understand Atlas.

## Document structure

- The overview, vision and surface chapters explain what callers can discover and do.
- Contracts and clients define the shared capability interface and language-specific behavior.
- Sections 6.1–6.7 describe gateway ownership, access, adapters, package boundaries, principles and mechanisms.
- Section 7 is the domain catalogue; a catalogue entry is a planned capability, not proof of an implemented profile.
- Section 8 records decisions and open questions.
- Sections 9.1–9.8 define the wire protocol and conformance requirements.

Keep headings, numbered rules, requirement IDs, code punctuation, table columns and diagram edges intact when editing. A change to formatting must not silently change a contract, ownership boundary or delivery path. Preserve uncertain requirements as explicit questions rather than guessing a resolution.

## Evidence boundaries

The [document-review coverage](VERIFICATION-LEDGER.md) and [validation lessons](VALIDATION.md) describe editorial checks. They do not establish executable correctness or live-service behavior. The [review documents](review/README.md) record dated analyses; the [handoff progress](../../HANDOFF-PROGRESS.md) records implementation and test evidence.

Maintain relative links to the local specification, code and protocol references. Planned package paths and commands must be labelled as planned until the implementation exists. Keep public standards and required third-party license notices where they substantiate the design or reused code.
