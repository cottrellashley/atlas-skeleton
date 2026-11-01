# Phase 1: the protocol

`sdk/go/protocol` is rebuilt to §9 (atlas/1.0). It is pure Go, depends only on the standard library and Apache Arrow, and every other package of Atlas builds on it.

## What there is

| Package | §  | What it does | Proved by |
|---|---|---|---|
| `protocol/ijson` | 9.1 | An I-JSON parser that keeps member order and exact number text and refuses duplicates, surrogates, noncharacters and a BOM. RFC 8785 canonical form. Numbers compare as binary64 | 10.9M fuzz runs; RFC 8785's own examples; AP-CONV-001 |
| `protocol/grammar` | 9.1 | Every ABNF of §9.1: ids, addresses, dates, date-times, durations, decimals, integer strings | Fuzzers; AP-CONV-002 |
| `protocol/sf` | 9.6.3 | RFC 9651 Structured Fields, with Dates and Display Strings | Agrees with http-sf on all 12,267 readings of 4,089 fields; 38M fuzz runs |
| `protocol` | 9.3.3, 9.4, 9.6 | The 24-code problem registry, every header with its type, Accept negotiation, ETags, traceparent, Bearer challenges, fingerprints | The registry is read from the spec's table; fingerprints come from rfc8785 vectors; AP-FP-001, AP-HTTP-001, ATLAS-PROTOCOL-001/005 |
| `protocol/schema` | 9.2 | The Atlas Schema profile: every rule of §9.2.1 checked at compile time; 2020-12 validation with formats as assertions and logical types as their own keywords; I-Regexp translated to Go's engine | ~70 refusal cases; 15M + 5.7M fuzz runs; ATLAS-TYPES-001 |
| `protocol/units` | 9.5.4 | The unit table: one multiplication or division in binary64, exact decimal arithmetic rounded ties-to-even | Tests on every conversion and every tie |
| `protocol/arrowschema` | 9.2.7 | A table's Arrow schema, derived from its JSON Schema, with Atlas keywords as field metadata; rows ↔ Arrow batches | AP-SCHEMA-002/003, ATLAS-TYPES-002 |
| `protocol/resolve` | 9.5 | The resolution algorithm, row by row and column by column; every lens operation both ways | Every case §9.5.8 lists; column-wise equals row-wise on every case as one row and many (AP-RESOLVE-001/002). **A million rows resolve in ~15 ms with no value resolved on its own; row by row would take ~1.5 s** (ATLAS-RESOLVE-004) |
| `protocol/frame` | 9.6.5 | The NDJSON end line and the Arrow final zero-row batch. A missing end is `upstream_failed`; a miscount or data after the end is `invalid_document` | A cut at every byte offset of both kinds of answer (AP-HTTP-002) |
| `protocol/profile` | 3.1 | A profile read and its schemas compiled, each to its kind (what an interaction takes is a request) | Through the gate's tests |
| `protocol/gate` | 3.3.12, 3.1.4a | The lint, the agent-readiness check and the change classifier of §3.3.3–3.3.7, run by the gate and by the gateway at registration | §3.3's worked examples; every row of its tables implemented; ATLAS-VERSION-001/007, ATLAS-PROFILE-005, ATLAS-CLIENTS-006, ATLAS-VISION-002 |

The requirement matrix (`make requirements`) now maps 22 requirements to tests; at the start of the phase it mapped 3.

## What the tests found

- **Fuzzers:**
  - `0E10000000000000000000` broke equality, and its big-number arithmetic is a denial of service. Exponents are now limited to four digits.
  - `a=@` panicked the Structured Fields parser.
  - `PT1.5M` was accepted as a duration.
- **Conformance with an independent implementation:** http-sf disagreed on 10 of 12,267 readings, all of them base64 padding and empty containers. Both are now resolved by RFC 9651's text (A-018).
- **The spec's own example, against my reading:** §3.1.3's `isin` is a key that is neither required nor non-null, so identifier keys are exempt from both rules (A-011).
- **A memory leak on the failure path** of column-wise resolution, found by Arrow's checked allocator.

## Readings of the spec

`ASSUMPTIONS.md` holds 21 of them (A-001 to A-021), each with the code that relies on it. The independently generated protocol vectors confirm or contradict each reading.

## Against the references

The Python generator in `tools/spec` derives `spec/protocol/references` from the protocol text, independently of the Go implementation, using third-party implementations of the standards. Every vector file that covers Phase 1 passed in Go in this historical phase:

| Vectors | Cases | Proves |
|---|---:|---|
| `grammar.json` | 229 | AP-CONV-002: every rule of `grammar.abnf` |
| `structured-fields.json` | 4,089 × 3 | RFC 9651, as Item, List and Dictionary |
| `headers.json` | 98 | AP-HTTP-001: every Atlas header, read or absent |
| `problems.json` | 25 | The registry and §9.7.6's exit codes |
| `fingerprints.json` | 9 | AP-FP-001 |
| `profiles/*.json` | 4 | One profile per family: read, fingerprinted, linted, ready for agents |
| `schemas.json` | 190 | AP-SCHEMA-001: 119 validations; 71 profile checks, each refused at the member that breaks its rule |
| `arrow.json` | 17 | AP-SCHEMA-002 |
| `resolution.json` | 42 | AP-RESOLVE-001, and AP-RESOLVE-002 column by column on every row case a typed batch can hold |

Where the references read the text differently, the Go code now follows them. That changed eight readings:
- `P1DT` is a duration.
- `-0` is an `integer-str`.
- A date-time is always written with six fractional digits.
- Surplus base64 padding is refused.
- `x-atlas-deprecated` needs all four members, and `null` means not deprecated.
- A decimal unit conversion rounds at each step.
- A resolution failure targets its top-level member.
- Arrow field metadata keeps the property's own order.

They also added rules: headers whose known members break their type are absent; HTTP-dates are strict; keys are refused in requests; the free-form built-in members are exempt. `ASSUMPTIONS.md` marks each entry agreed, reversed or adopted.

The validator now applies 2020-12's semantics to any schema, and the profile's rules are a check apart (`Options.Unchecked`). Resolution takes a capability's whole chain and uses the part between writer and reader. `make check` is green.

## Not done in this phase

- **Generated document types** (`tools/schemagen`). They are generated from the 21 document schemas, which now exist.
- **`guard.json`** (the read-only SQL guard) and **`handoff/`** (Snowflake chunks). Both are Phase 5, where sqlglot and `snowarrow` meet them.
- **The 50 scenarios.** They need the conformance runners of Phase 9. The requirement matrix lists each beside its requirement, which stays open until a runner runs it.

## Carried into Phase 2

- **The kit's `Deadline` middleware turns a deadline already past into the route's maximum,** rather than refusing it with `deadline_exceeded`. It also reads the wall clock rather than an injected one. Phase 2 rebuilds it.
