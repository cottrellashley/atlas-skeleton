# Assumptions

Each entry records where the spec could be read in more than one way, and the reading the Go build uses. Each one names the code that relies on it and the references' question that settles it (`spec/protocol/references/QUESTIONS.md`). The references were written from the text alone (D22), and where they differ, the vectors win: the Go code follows them, and every vector file passes.

**Status:** *agreed* means the references took the same reading. *Reversed* means the Go code changed to the references' reading. *Adopted* means the reading came from the references.

| Id | Where | Reading | Status | Code |
|---|---|---|---|---|
| A-001 | §9.3.3 `confirmation_required` | `estimate.amount` is a decimal string with at most 3 fraction and 12 integer digits, so it fits `Atlas-Confirm`, an SF Decimal | Agreed (Q-DOC-10) | `protocol.Estimate`, `ParseAmount` |
| A-002 | §9.1.5 `Decimal(p,s)` | SQL's meaning: integer digits ≤ p−s, fraction digits ≤ s | Agreed (Q-DEC-1) | `grammar.Decimal.Fits` |
| A-003 | §9.1.4 `duration` | `P1DT` is a duration, as the ABNF writes it, and `PT` alone is not. Only seconds may have a fraction, and a point needs a digit after it | Reversed (Q-GRAM-3) | `grammar.ParseDuration` |
| A-004 | §9.6.3 `Atlas-Deadline` | Written rounded down to whole seconds | Agreed (Q-HDR-2) | `protocol.FormatDeadline` |
| A-005 | §9.6.4 | No `Accept`, or `*/*`, takes the binding's first encoding | Agreed (Q-HDR-5) | `protocol.ChooseMedia` |
| A-006 | §3.1.2 "for a table, its Arrow schema too" | A profile carries no Arrow schema; §9.2.7 derives it | Agreed (Q-DOC-6) | `protocol/arrowschema` |
| A-007 | §9.6.3 Decimal headers | An sf-integer is not an sf-decimal: `Atlas-Confirm: 1250` is absent. Senders write `1250.0` | Agreed (Q-HDR-4) | `protocol.ParseConfirm` |
| A-008 | §9.6.3 `traits` | Trait tokens compare exactly with the lowercased names | Agreed | `protocol.Features.Understands` |
| A-009 | §9.2.5 step 3 | Failures are (pointer, keyword), sorted by pointer then keyword, by code points. `required` points at the missing member; `additionalProperties: false` and `items: false` at the extra one; `anyOf`/`oneOf` give one failure, at the instance; a logical type is reported under the keyword it expands to | Agreed (Q-VAL-1) | `schema.Validate` |
| A-010 | §9.2.2 `uri` | An RFC 3986 URI with a scheme and an optional fragment, ASCII only | Agreed (Q-FMT-1) | `schema.checkURI` |
| A-011 | §9.2.3 `x-atlas-key` | `Isin`, `Cusip`, `Sedol`, `Figi` and `Lei` keys may be null and need not be required. Every other key is required and not null, and no key stands in a request | Agreed (Q-PROF-6) | `schema.logicals`, `compiler.properties` |
| A-012 | §9.2.1 | Structural rules: boolean schemas only as `items` or `additionalProperties`; `$defs` only at the root; no recursion through `$ref`; `$ref`'s siblings only prose and key, aliases, reference and deprecated; every schema typed unless `$ref`/`anyOf`/`oneOf`; `required` ⊆ `properties`; defaults valid; `multipleOf` a positive integer on an integer; a map allowed in any schema; `Money` has only `amount` and `currency` | Agreed (Q-PROF-1–8, 13) | `schema.Compile` |
| A-013 | §9.2.4 I-Regexp | `\p{C}` and `\p{Cn}` refused; quantifier counts at most 1000 | Agreed, beyond RFC 9485 (Q-RE-1) | `schema.compilePattern` |
| A-014 | §9.1 numbers | Numbers compare as binary64 values | Agreed | `ijson.Equal` |
| A-015 | §9.2.3 `x-atlas-deprecated` | `null` means not deprecated. Otherwise all four of `reason`, `since`, `sunset` and `replaced_by` (a property name, or null) are required. `x-atlas-enum` describes every value of its enum | Reversed: all four, not two (Q-PROF-10, Q-PROF-11) | `compiler.deprecation`, `compiler.enums` |
| A-016 | §9.3.2 built-ins | The `atlas.*` profiles are exempt from the lint. Within a schema, only the two free-form request members (`atlas.compare`'s `request`, `atlas.settings`' `settings`) are exempt from closing | Agreed (Q-PROF-9) | `schema.Options.FreeForm`, `gate.Lint` |
| A-017 | §9.2.7 | A logical type first, then a format, then the JSON type. A string with any other format (`decimal` without `Decimal(p,s)` among them), or a string logical type, is Utf8. An object without properties, an array without items, a union, and a row without properties have no Arrow type, and are refused at that property | Reversed for decimals (Q-ARROW-2, Q-ARROW-3) | `arrowschema.Type` |
| A-018 | RFC 9651 §4.2.7 | Base64 padding that is missing, in part or in whole, is synthesised. An `=` beyond what base64 needs fails the parse | Reversed: surplus padding is refused (Q-SF-1) | `sf.parser.bytes` |
| A-019 | §9.2.7 field metadata | A keyword's value is its RFC 8785 text (`"Isin"` with its quotes). The keys follow the order the property writes them | Reversed: the property's order, not sorted (Q-ARROW-4) | `arrowschema.metadata` |
| A-020 | §9.5.4 decimals | A `convert_unit` on a decimal string is exact, and is rounded at each step, ties to even, to the scale of the property in the profile that step converts into. With no `Decimal(p,s)` there, it fails | Reversed: per step, not once at the end (Q-RES-2) | `resolve.convertUnit` |
| A-021 | §9.2.7, rows ↔ Arrow | An absent or null member is an Arrow null. An Arrow null reads back as `null` where the property allows null, and as absent where it does not | Not contradicted | `arrowschema.Value` |
| A-022 | §9.1.5 `integer-str` | `-0` is an integer string; only a decimal refuses it | Adopted (Q-DEC-3) | `grammar.ParseIntegerString` |
| A-023 | §9.1.4, JSON rows | A date-time is written in UTC with `Z` and exactly six fractional digits | Adopted (Q-TIME-1) | `grammar.FormatDateTime` |
| A-024 | §9.5.2 step 12 | A resolution failure's target is the top-level member it is in, even when the failure is deeper | Adopted (Q-RES-1) | `resolve.match` |
| A-025 | §9.5.3 `convert_unit` | A lens names a unit as an `x-atlas-unit` (`bp`) or as the logical type that implies it (`Bp`) | Adopted (Q-RES-7) | `resolve.unitOf` |
| A-026 | §9.6.3 | A header that parses as its Structured Field type but breaks its grammar is absent. That covers `Atlas-Client` without a String name, version or protocol-ver; `Atlas-Client-Features` with a known member that is not an Inner List of Tokens; and `Atlas-Limit-Remaining` with a member that is not an Integer | Adopted (Q-HDR-1) | `protocol.ParseClient`, `ParseFeatures`, `ParseLimits` |
| A-027 | §9.6.3 `Sunset` | An HTTP-date in any of RFC 9110's three forms, in GMT, with the right day of the week. An rfc850 two-digit year more than 50 years ahead of now is the century before | Adopted (Q-HDR-3) | `protocol.ParseHTTPDate` |
| A-028 | §9.5.1 the chain | A chain is a capability's profiles, oldest first. The writer and the reader may stand anywhere in it, and only the profiles between them take part | Adopted | `resolve.New` |
| A-029 | §9.2.5 | The validator applies 2020-12's semantics to any schema. The profile's rules are a separate check, and `Options.Unchecked` leaves them out | Adopted (the validation vectors) | `schema.CompileWith` |
