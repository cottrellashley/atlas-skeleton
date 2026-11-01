# Working digests (2026-10-31)

These were written by agents reading the sources in full, each fact cited to its source, while pass 2's build was planned. They are an index, not an authority. Where a digest and its source disagree, the source wins: the spec (`../atlas-spec/`), the review, the references (`spec/protocol/references/`) and the code.

**Paths.** The digests name four roots, written as absolute paths on the maintainer's machine:

| Name | Path in this repository |
|---|---|
| SPEC | `docs/source/atlas-spec` |
| REPO | the repository root |
| REF | `spec/protocol/references` |
| DIG | `docs/source/digests` |

**Staleness.** Most digests describe the spec, the review and the references, which are unchanged. Three describe something that has since changed:
- `code-api.md` describes the code before the build. The working tree has changed a great deal since; read the code.
- `pass1.md` describes branch `pass-1`.
- `CRITIC.md` and `gaps/1.md` describe a working tree that was changing as they were written.

**What is still useful for the handoff work in `docs/HANDOFF.md`:**

| Study | Use it for |
|---|---|
| `gaps/2.md` | The R01/R02 decisions D1 to D9: caching, revisions, routing. They are implemented |
| `gaps/4.md` | The DataSource port. It is implemented |
| `gaps/5.md` | The conformance gateway, the suite driver and the runners (C4) |
| `gaps/6.md` | gosnowflake v1.19.1 in detail (R3-02, R3-16) |
| `gaps/7.md` | The step-3 mechanisms, R06 to R09 and R17 (C5) |
| `gaps/8.md` | Node and I-JSON |
| `gaps/9.md` | Load, chaos and plug-in (C4, C6) |
| `gaps/10.md` | Adding a capability after the clients are built |

The remaining digests are cited summaries of their sources:
- `wire.md`
- `clients.md`
- `conformance.md`
- `gateway-front.md` and `gateway-access.md`
- `review.md`
- `contracts.md`
- `journey.md`
- `patterns.md`
- `rest.md`
