# Questions the text leaves open

Where the Atlas spec ([local specification](../../../docs/source/atlas-spec/09-atlas-protocol.md), §9 above all) is ambiguous or silent, the references take a reading and record it here, so nothing is guessed silently. Each entry gives the case, the reading the references take, and where it shows. A vector case that depends on one carries `"question": "<id>"`. The spec settles each one, then the references follow it, with a test vector, before any implementation changes (§9 Purpose).

## The ones that matter most

1. **Q-ARROW-1, property order.** §9.2.7 derives Arrow fields "in the order of `properties`", and §9.5.2 step 5 walks `S.properties` "in order". But I-JSON rule 5 (§9.1.2) says member order carries no meaning, and the fingerprint sorts members (RFC 8785). So two profiles with one fingerprint can derive different Arrow schemas. **Reading:** document order. A reader must parse `properties` keeping member order (Go's `map[string]any` does not). The vectors deliberately write properties out of sorted order. **Recommendation:** say so in §9.1.2, or derive in canonical order.
2. **Q-GRAM-1, `resource-name`.** §9.3.4 cites "`resource-name` in the grammar", but §9.1 has no such rule, and nothing defines `operations/{id}`. **Reading (provisional):** `resource-name = domain 1*( "/" segment "/" resource-id )` with `resource-id = ( %x61-7A / DIGIT ) *( %x61-7A / DIGIT / "-" )`, and `operation-name = %s"operations/" resource-id`. These are in grammar.abnf's last section, marked PROVISIONAL.
3. **Q-CLI-1, which `<env>` in `token-<env>.json`.** A conformance gateway "is named by its URL" (§9.7.2), but a URL is not a file name. **Reading:** `<env>` is the bootstrap's `environment`, so a client reads the bootstrap first, then `token-conformance.json`. Every sign-in scenario depends on this.
4. **Q-DOC-1, enums in protocol documents.** A newer gateway's document must still validate for an older client (§9.B, §9.7.10), but a closed `enum` would refuse a new delivery, shape, encoding or lens operation. **Reading:** every enum in a document the gateway sends includes `UNKNOWN`, and a client maps a value it does not know to it (§9.5.5) before validating. No request's enum includes it. This is the rule §9.2.3 sets for profiles, applied to the protocol's own documents.
5. **Q-SNOW-1, the assumed chunk encoding.** The synthetic handoff chunks use an assumed encoding not yet established by the protocol specification. One choice in it: NUMBER's `logicalType` is written `FIXED`, as Snowflake's own drivers name it, not `NUMBER`. Captured chunks replace these.
6. **Q-RE-1, two restrictions beyond RFC 9485.** `\p{C}` and `\p{Cn}` are refused, because they change with every Unicode version. Quantifier counts above 1000 are refused, because RE2-family engines, Go's among them, reject them. Neither is in RFC 9485. Both make the profile portable in the sense §9.2 wants. **Recommendation:** write them into §9.2.4.
7. **Q-VAL-1, which failure is "first".** §9.2.5 makes the first failure's pointer the `target` of `invalid_request`, but defines no order. **Reading:** sort by pointer, then by keyword, comparing code points, and give each pair once. So `/10` sorts before `/2`.
8. **Q-PROF-9, the built-ins and the profile.** §9.3.2 says the `atlas.*` profiles use exactly `builtins.schema.json`'s schemas, and §9.2 Rule 1 says no served profile leaves the Atlas Schema profile. But two built-in requests are free-form by nature (`atlas.compare`'s `request`, `atlas.settings`' `settings`), and the built-ins' results are protocol documents that use `propertyNames`, `if`/`then` and refer to other documents. **Reading:** the built-ins' profiles are exempt from the profile lint. Their schemas name `builtins.schema.json` and the protocol's documents by `$id`.

## Grammars (§9.1)

- **Q-GRAM-1**: see above.
- **Q-GRAM-2.** `protocol-ver = %s"atlas/" 1*DIGIT "." 1*DIGIT` accepts leading zeros (`atlas/01.00`). The vectors accept them, as the ABNF does.
- **Q-GRAM-3.** The duration rule allows `T` with no time component after a day (`P1DT`), and the prose forbids only "`P` alone". ISO 8601 would refuse it. The vectors accept it, as the text does.
- **Q-GRAM-4.** Whether year `0000` is a "real calendar date". RFC 3339's 4DIGIT allows it, and the proleptic Gregorian calendar has it. The references accept it, and no vector depends on it.
- **The two rules written differently.** grammar.abnf writes `segment` and `duration` so that their prose ("never ends with `-`, never contains `--`"; "at least one component") is in the ABNF itself. It quotes the text's own form beside each. They accept exactly the same strings.

## Decimals and integers (§9.1.5, §9.2.2)

- **Q-DEC-1, precision.** "At most p significant digits and at most s digits after the point" read literally lets `1234.5` be a `Decimal(5,2)`. Decimal128(5,2) cannot hold that. **Reading:** the SQL meaning. A value fits when it has at most s fractional digits, and its integer digits (without leading zeros) plus s are at most p. So `1234.5` does not fit and `0.123` is a `Decimal(3,3)`.
- **Q-DEC-2.** "`-0` is not allowed" is read as "no negative zero", which refuses `-0.0` and `-0.000` too.
- **Q-DEC-3.** The text forbids `-0` only for decimals, so `integer-str` and `format: int64` accept `-0`.

## Formats and times

- **Q-FMT-1.** "An absolute URI (RFC 3986)": RFC 3986's `absolute-URI` has no fragment. **Reading:** a URI with a scheme, which may have a fragment (JSON Schema's `uri`), and ASCII only.
- **Q-FMT-2.** "A lowercase RFC 9562 UUID" is read as the lowercase 8-4-4-4-12 form. Version and variant bits are not checked, so the Nil and Max UUIDs are accepted.
- **Q-TIME-1.** How a microsecond instant is written as JSON (the runner's rows, NDJSON rows, the vectors). **Reading:** RFC 3339 in UTC with `Z` and exactly six fractional digits, as a `Decimal(p,s)` is written with exactly s digits. Rows compare by instant and decimals by value; the strings agree too.

## Schemas and the profile (§9.2)

- **Q-PROF-1.** `$defs` only at the root, since `$ref` can only reach `#/$defs/<name>`. No recursion through `$ref` either, since a recursive schema has no Arrow type and no bounded depth.
- **Q-PROF-2.** A `$ref`'s siblings are limited to the meta-data keywords and `x-atlas-key`, `x-atlas-aliases`, `x-atlas-reference` and `x-atlas-deprecated`. A `type` beside a `$ref` either duplicates or contradicts its target.
- **Q-PROF-3.** Boolean schemas only as `items` or `additionalProperties`.
- **Q-PROF-4.** §9.2.1 rule 4 gives "every property" a type. **Reading:** every schema has one, except one with `$ref` (it takes its target's), `anyOf` or `oneOf`.
- **Q-PROF-5.** Every name in `required` is one of `properties` (or implied by `Money`), and every `default` is valid against its own schema.
- **Q-PROF-6.** Which logical types are "an identifier that may be missing" for a key: `Isin`, `Cusip`, `Sedol`, `Figi`, `Lei`. Such a key may be nullable and need not be required.
- **Q-PROF-7.** "Close every request schema (every object in it)" would forbid `labels`, which a resource's create and update must carry. **Reading:** a map (an object with no `properties` whose `additionalProperties` is a schema) is allowed in any schema. Every other object of a request is closed, and every other object outside one is open.
- **Q-PROF-8.** "`multipleOf` (integers only)" is read as: only on `type: integer`, with a positive integer value.
- **Q-PROF-9**: see above.
- **Q-PROF-10.** A string enum outside a request must have `x-atlas-enum`, since UNKNOWN must be in it. Wherever `x-atlas-enum` appears, its values are exactly the enum's string values.
- **Q-PROF-11.** `x-atlas-deprecated` requires all four members (`reason`, `since`, `sunset`, `replaced_by`), since §3.3.9 says a deprecation carries a reason, a replacement and a sunset; `replaced_by` may be null. `null` as the whole value means not deprecated (§3.2.7's example).
- **Q-PROF-12.** "Nest no deeper than 32 levels" counts schema levels, the root being 1; a `$ref` is not followed. "No larger than 1 MiB as UTF-8" measures the RFC 8785 form. The size rule is tested in the suite, not in a vector, since such a vector would itself pass 1 MiB.
- **Q-PROF-13.** A `Money` schema may declare no properties but `amount` and `currency`. This is a schema rule: outside a request, an instance stays open.
- **Q-PROF-14.** "oneOf only with a const discriminator in every branch": one property, required in every branch, with a distinct `const` in each.
- **Q-PROF-15** (an observation). §9.2.1 has every schema declare `$vocabulary`, but `$vocabulary` only means something in a meta-schema; validators ignore it elsewhere. The references check it as a declaration.

## Documents (§9.3)

- **Q-DOC-1**: see above.
- **Q-DOC-2.** The bootstrap's sign-in names every endpoint (`token_endpoint`, `authorization_endpoint`, `device_authorization_endpoint`), so a client builds none. The bootstrap's members are designed here from §9.3.1's sentence.
- **Q-DOC-3.** `profile.schema.json` does not apply the Atlas Schema meta-schema to `schemas.*`. A 1.0 client reading a profile that uses a 1.1 logical type must not refuse it. The gate checks the meta-schema.
- **Q-DOC-4.** §9.7.4 step 6 chooses the binding "whose protocol, encodings and delivery the client supports", which implies a delivery per binding. **Reading:** `delivery` is the interaction's, and a binding may override it (a fallback that answers an operation for a stream).
- **Q-DOC-5.** `properties.deprecated` (a date) is added so that a deprecated capability can report Deprecation (§3.3.9), beside `sunset` and `replaced_by`.
- **Q-DOC-6.** §3.1.3's example carries `result_arrow` in `schemas`; §9.2.7 says the Arrow schema is derived, never written. **Reading:** §9 governs, so profiles carry no Arrow schema.
- **Q-DOC-7.** The version-boundary lens has no member name. **Reading:** `evolution.previous_version` (named in §9.5.2) and `evolution.version_lens`.
- **Q-DOC-8.** The registry fixes each code's status, but `invalid_request` is also sent with 406 (§9.6.4), and `retryable` can be false for a write that cannot be repeated safely (ATLAS-PROTOCOL-005). So the schema documents the registry (`$defs/registry`) and does not enforce status or retryable.
- **Q-DOC-9.** The shape of `details` items: `{code, target, keyword}` for each validation failure, `{hop, ...}` for a hop, and `{challenge}` for an unauthenticated claims challenge (CORE-SIGNIN-004).
- **Q-DOC-10.** `estimate.amount` is a decimal string with at most 12 integer and 3 fractional digits, so that Atlas-Confirm (a Structured Field Decimal) can carry it exactly.
- **Q-DOC-11.** An operation's `result` needs its writer profile, but the poll answer's Atlas-Profile is `atlas.operation`'s own. **Reading:** the operation document carries `profile`, the exact-id `result` was written under.
- **Q-DOC-12.** The handoff's `operation` is an operation name. Asking it again (`atlas.operation:get`) answers an operation whose `result` is a fresh handoff document.
- **Q-DOC-13.** The emulator's credential member is named `connection_string`.
- **Q-DOC-14.** How a `content` result reaches a Core client, which has no transfer: the transfer document's file may carry `inline` bytes (base64) when under 1 MiB. This is the "inline body under 1 MiB" of §2.1.D, inside the operation's result.
- **Q-DOC-15.** A truncated answer's end line has `truncated: true` and the problem `output_limit_exceeded` (status 200), which a client reports as a warning. Any other problem in an end line is raised after the rows (STREAM-003, STREAM-004).
- **Q-DYN-1.** "In JSON, a first member `columns`" for a DynamicSchema answer is read as `{"columns": [{"name", "type"}], "rows": [...]}`. No vector depends on it yet.

## Built-ins (§9.3.2)

- **Q-BI-1.** atlas.search's `page_size` has no range. **Reading:** atlas.catalog's, 1 to 500 with default 100.
- **Q-BI-2.** atlas.guide's `revisions` is an object of the revisions it was written from (the profile's prose, governance, context). Its members are not fixed.
- **Q-BI-3.** atlas.operation's `cancel` and `watch` take `operation_get_request` (`{name}`); only two request `$defs` are named.
- **Q-BI-4.** §9.3.2 answers atlas.directory and atlas.audit with pages; §7 calls them tables. **Reading:** §9 governs, so pages.
- **Q-BI-5.** atlas.compare's `from` and `to` are the grammar's `version` (`v1`), while a profile's `identity.version` is an integer.

## Resolution (§9.5)

- **Q-RES-1.** "Fail with resolution_failed, target / p" at step 12 is read literally: the target is always the top-level member, even when the failure is inside it.
- **Q-RES-2.** "Rounded to the reader's scale" in a chain is read as the scale of the profile each lens operation converts into. A decimal whose target has no `Decimal(p,s)` cannot be rounded, so it fails.
- **Q-RES-3.** `promote` backward from `date-time` to `date` is exact only for midnight UTC.
- **Q-RES-4.** §9.5.5's first row leaves a nested object unchanged when it is valid, so it keeps members the reader does not know. One that needs converting is matched by step 5 and loses them.
- **Q-RES-5.** One lens per profile applies to every kind of record: a request and a row share its field names. This is why `rename(region → area)` reaches requests and `default(currency)` adds a member a request then drops.
- **Q-RES-6.** §9.5.5's third row ("only when converting backward a promote") can never apply after its first: `2.0` is a valid integer under §9.2.2.
- **Q-RES-7.** A `convert_unit` lens names units as logical types (`Bp`) or as `x-atlas-unit` values (`bp`). Both spellings are accepted and mean the same unit.
- **Q-RES-8.** `promote`'s types are `integer`, `number`, `date` and `date-time`, the conversions §9.5.5 defines.

## The HTTP binding (§9.6)

- **Q-HDR-1.** A value that parses as its Structured Field type but breaks the grammar §9.6.3 gives it is treated as absent: a String that is not an exact-id, a Token that is not a problem-code, an Atlas-Execution that is not gateway, handoff or client. The text says only "does not parse as its type".
- **Q-HDR-2.** Atlas-Deadline is a Date, so whole seconds. A client rounds its deadline down.
- **Q-HDR-3.** RFC 9110's rfc850 HTTP-date has a two-digit year, read relative to "now". The references fix the reference year at 2026, so the vectors stay deterministic (until 2044).
- **Q-HDR-4.** An Integer in Atlas-Confirm is not a Decimal, so it is absent; a sender always writes a decimal point (`1250.0`).
- **Q-HDR-5.** No Accept header, or `*/*`, takes the binding's first encoding.
- **Q-HDR-6.** Which problem the gateway answers for a required header that is missing or absent by Q-HDR-1 (`invalid_request` with the header name as target is the natural reading). No vector depends on it yet.
- **Q-SF-1.** For 6 corpus fields (11 readings), http_sf accepts base64 with surplus padding (`:aGk==:`), which RFC 4648 calls invalid; RFC 9651 says a failed base64 decoding fails the parse. Those cases follow the RFC and carry the question.

## Snowflake chunks (§2.2.1, assumed)

- **Q-SNOW-1**: see above.
- **Q-SNOW-2.** A negative instant's epoch is floored and its fraction is non-negative; every epoch is UTC (TIMESTAMP_NTZ converted to UTC in the SQL, §2.2.1.5). TIMESTAMP_TZ's offset is dropped for UTC.
- **Q-SNOW-3.** Truncating to microseconds floors, so `1969-12-31T23:59:59.9999995Z` becomes `.999999`: the written fraction digits are cut (§9.1.4), never rounded.
- **Q-SNOW-4.** NUMBER(38,0) where a profile has an integer is a mismatch, since §2.2.1.6 maps NUMBER(p,0), p > 18, to Decimal(p,0). A planned SQL that wants an integer casts to NUMBER(18,0).
- Not covered: Money from Snowflake (two columns cannot form the struct without a mapping), VARIANT, OBJECT, ARRAY, BINARY and TIME.

## The read-only guard (§6.4.3)

- **Q-GUARD-1.** An object must be named `database.schema.object`. One-part and two-part names (and `db..object`) are refused, because the session's defaults could point anywhere.
- **Q-GUARD-2.** One trailing semicolon is allowed. An empty statement after it (`;;`) counts as a second statement.
- **Q-GUARD-3.** Every table function is refused, including `LATERAL FLATTEN` over a registered table's column. User-defined functions in a SELECT are not checked; the read-only role underneath is the line there.

## The runner and the scenarios (§9.8)

- **Q-RUN-1.** §9.8.3's op list leaves out `guide` and `explain`, which §9.7.1 defines; the scenarios use them as ops.
- **Q-RUN-2.** How a CLI runner shows §9.7.6's exit code: the failed result carries `cli_exit`. Other runners leave it out, and the suite checks it only for a CLI.
- **Q-RUN-3.** A deprecation warning's `code` is `deprecated`.
- **Q-RUN-4.** Runner options beyond §9.8.3's: `path`, `max_memory`, `replace` (§9.7.13), `if_match`, `fields` and `set` (building an update from a record, CORE-ENV-001). Also the value forms of content (`{"content": {"base64", "metadata"}}` in memory, `{"manifest": ...}` on disk) and the manifest's shape (`{"files": [...]}`).
- **Q-RUN-5.** `call` on an `operation` delivery returns the operation document, and `operation.wait` its result. On a `transfer` delivery it waits and lands the result (§9.7.13). The end line's `usage` is reported in the result as `usage`.
- **Q-RUN-6.** A failed result may carry `value`: the rows read before the failure (STREAM-003).
- **Q-RUN-7.** `subscribe.open` returns a subscription named by its command's id. `subscribe.next` with a timeout answers `{"none": true}` when nothing arrives.
- **Q-EVT-1.** §3.1.3's event profile has `publish` take `payload`, while §9.7.8 has the client send the event itself. **Reading:** the publish call's body is the payload, which the gateway validates; the client then sends the same payload as a CloudEvent over the handover.
- **Q-SCN-1.** The scenario format adds `setup` (environment, files, working directory) and suite commands (`suite.restart`, `suite.file`, `suite.expect_file`) to §9.8.3's runner commands. The runner adds no logic, so the suite does these.
- **Q-SCN-2.** CORE-BOOT-002 needs several gateways. The conformance gateway serves four listeners ({gateway} to {gateway4}).
- **Q-SCN-3.** The conformance capabilities' behaviour when no scenario step overrides it is not in the text. It is designed in README.md, "The conformance capabilities".
- **Q-SCN-4.** §9.8.3's example answers `conformance.rows@v1:fetch {"n": 3}` with rows of `i` alone. The references' rows have six columns (to prove decimals, timestamps and nulls), and the example is read as abbreviated.

## The Go side's readings (from the parent session)

| # | Reading | Here |
|---|---|---|
| 1 | `estimate.amount` is a decimal string, at most 3 fractional and 12 integer digits | Agreed: Q-DOC-10, `problem.schema.json` `$defs/estimate` |
| 2 | An Integer in Atlas-Confirm is absent; senders write `1250.0` | Agreed: Q-HDR-4, headers.json |
| 3 | Atlas-Deadline rounded down to whole seconds | Agreed: Q-HDR-2 |
| 4 | No Accept, or `*/*`, takes the binding's first encoding | Agreed: Q-HDR-5 (no vector yet) |
| 5 | §9.2.5 failures as (pointer, keyword), sorted by pointer then keyword in code-point order; `required` at the missing member; `additionalProperties: false` and `items: false` at the extra one; `anyOf`/`oneOf` one failure at the instance; a logical type under the keyword it expands to | Agreed in every part: Q-VAL-1, schemas.json |
| 6 | Numbers compare as binary64 | Agreed: schemas.json (`instance_text` cases), resolution.json |
| 7 | Boolean schemas only as items/additionalProperties; no recursive $ref and $defs only at the root; $ref siblings only prose and key/aliases/reference/deprecated; maps allowed in any schema; required ⊆ properties; defaults valid; multipleOf only on integers; every schema typed unless $ref/anyOf/oneOf | Agreed in every part: Q-PROF-1, 2, 3, 4, 5, 7, 8 |
| 8 | Money implies amount and currency, both required, and has no other properties | Agreed as a schema rule (Q-PROF-13). An instance outside a request stays open (schemas.json "open outside a request") |
| 9 | Key identifiers that may be null: Isin, Cusip, Sedol, Figi, Lei | Agreed: Q-PROF-6. They may also be absent (not required), as §3.1.3's `isin` is |
| 10 | I-Regexp refuses `\p{C}` and `\p{Cn}`; counts at most 1000 | Agreed, as a reading beyond RFC 9485: Q-RE-1 |
| 11 | Every string enum's x-atlas-enum lists every value; x-atlas-deprecated requires reason and since | First part agreed (Q-PROF-10). **Second part differs:** the references require all four members, so sunset and replaced_by too (Q-PROF-11, schemas.json "a deprecation without its sunset") |
| 12 | format uri: RFC 3986, a scheme, may have a fragment, ASCII only | Agreed: Q-FMT-1 |
| 13 | The atlas.* schemas are fixed by builtins.schema.json and exempt from the lint | Agreed: Q-PROF-9. In schemas.json only the two free-form request members are exempted (`free_form`) |
| 14 | Arrow maps every other string format, and every string logical type, to Utf8 | Agreed: Q-ARROW-2 |

The defect the parent found in its draft profile (`x-atlas-deprecated.replaced_by` naming a capability) is fixed in `vectors/profiles/data.json` (`replaced_by: null`). It is also a refused case in schemas.json ("replaced_by a capability, not a property").

## Arrow (§9.2.7)

- **Q-ARROW-1**: see above.
- **Q-ARROW-2.** The table covers `type: string` with no format and no logical type. Strings with any other format (`uri`, `uuid`, `capability-id`, `decimal` without a logical type) and every string logical type are Utf8. Precedence: a logical type, then a format, then the JSON type.
- **Q-ARROW-3.** A property the table cannot map has no Arrow schema, and the table schema is refused with invalid_document at that property: an object without properties, an array without items, anyOf, oneOf, and a DynamicSchema row. A list's child field is named `item`.
- **Q-ARROW-4.** A field metadata value is "the keyword's JSON text", taken as its RFC 8785 form (`"Bp"` with its quotes, `true`, `["mv"]`). Schema metadata (`atlas:profile`, `atlas:protocol`) is plain text, as §9.2.7 writes it.
