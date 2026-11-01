# §9.2 Schemas

`spec/protocol/schemas.md` · proposed

> Section 9.2 of the Atlas Protocol: the Atlas Schema profile of JSON Schema 2020-12 (exactly which keywords, formats and regular expressions are allowed, so that every language's validator agrees), the Atlas vocabulary's keywords and their meaning, validation results, and the exact mapping of a table schema to an Arrow schema.

## Purpose

JSON Schema validators differ: in which keywords they support, which formats they check, which regular-expression dialect they use, how they treat unknown keywords. This section fixes a small **profile** of JSON Schema 2020-12 that every language's validator implements identically, so a request valid in one client is valid in all of them and at the gateway.

## Concepts

### 9.2.1 The Atlas Schema profile

Every schema in a profile (§9.3) MUST:

1. declare `"$schema": "https://json-schema.org/draft/2020-12/schema"` at its root;
2. declare `"$vocabulary"` at its root as exactly: `json { "https://json-schema.org/draft/2020-12/vocab/core": true, "https://json-schema.org/draft/2020-12/vocab/applicator": true, "https://json-schema.org/draft/2020-12/vocab/validation": true, "https://json-schema.org/draft/2020-12/vocab/meta-data": true, "https://json-schema.org/draft/2020-12/vocab/format-assertion": true, "https://atlas.jhg.com/vocab/2026-10": false }` (the Atlas vocabulary is `false`: a validator that does not know it still validates correctly, §9.2.4);
3. use only these keywords:

| Vocabulary | Allowed keywords |
|---|---|
| Core | `$schema`, `$vocabulary`, `$id` (root only), `$defs`, `$ref` (only `#/$defs/<name>`), `$comment` |
| Applicator | `properties`, `additionalProperties` (boolean or schema), `items`, `prefixItems`, `anyOf`, `oneOf` (only with a `const` discriminator in every branch) |
| Validation | `type`, `enum`, `const`, `required`, `minimum`, `maximum`, `exclusiveMinimum`, `exclusiveMaximum`, `multipleOf` (integers only), `minLength`, `maxLength`, `pattern`, `minItems`, `maxItems`, `uniqueItems`, `minProperties`, `maxProperties` |
| Meta-data | `title`, `description`, `default`, `deprecated`, `examples`, `readOnly`, `writeOnly` |
| Format | `format`, with only the formats in 9.2.2 |
| Atlas | the keywords in 9.2.3 |

**Not allowed:** `$dynamicRef`, `$dynamicAnchor`, `$anchor`, `$recursiveRef`, `allOf`, `not`, `if`, `then`, `else`, `dependentSchemas`, `dependentRequired`, `patternProperties`, `propertyNames`, `contains`, `minContains`, `maxContains`, `unevaluatedProperties`, `unevaluatedItems`, `contentSchema`, any `$ref` to another document, and any keyword not listed. They are excluded because validators disagree on them, because they defeat resolution (§9.5), or because they make a schema's meaning depend on evaluation order.

4. Give every property exactly one `type`, or a two-element `type` array whose second element is `"null"`.
5. Nest no deeper than 32 levels; be no larger than 1 MiB as UTF-8.
6. Close every request schema (`"additionalProperties": false` on every object in it); leave every other schema open (no `additionalProperties`, or `true`).

### 9.2.2 Formats

`format` is an **assertion** in this profile: a value that does not match its format is invalid.

| Format | On | Valid exactly when |
|---|---|---|
| `date` | string | It matches `date` (§9.1.4) and is a real calendar date |
| `date-time` | string | It matches `date-time` (§9.1.4) and is a real instant |
| `duration` | string | It matches `duration` (§9.1.4) |
| `decimal` | string | It matches `decimal` (§9.1.5); with `x-atlas-logical: Decimal(p,s)`, within its precision and scale |
| `int64` | string | It matches `integer-str` (§9.1.5) and lies within a signed 64-bit integer |
| `uri` | string | It is an absolute URI (RFC 3986) |
| `uuid` | string | It is a lowercase RFC 9562 UUID |
| `capability-id`, `pinned-id`, `exact-id`, `address` | string | It matches the grammar of that name (§9.1.3) |

Integers and floats use `type`, never `format`: `integer` is a JSON number with no fractional part within the I-JSON range; `number` is any JSON number.

### 9.2.3 The Atlas vocabulary

| Keyword | On | Value | Meaning |
|---|---|---|---|
| `x-atlas-logical` | a property schema | A logical type name (below) | Its meaning and unit |
| `x-atlas-key` | a property of a result row | `true` | Part of the row's key; every key property is required and not nullable, except where `x-atlas-logical` is an identifier that may be missing (then the key is the set of non-null key values) |
| `x-atlas-aliases` | a property schema | Array of `property-name` | Earlier names of this property within the capability's version, oldest first |
| `x-atlas-reference` | a property schema | A `pinned-id` | The data capability that lists the property's valid values |
| `x-atlas-deprecated` | a property schema | `{ "reason", "since", "sunset", "replaced_by" }` (strings; `since` and `sunset` dates; `replaced_by` a property name, or null) | Deprecated, and when it ends |
| `x-atlas-unit` | a `number` or `integer` property | A unit from §9.5's unit table | Its unit, where no logical type implies one |
| `x-atlas-enum` | a `string` property with `enum` | Array of `{ "value", "description", "aliases", "deprecated" }` | Each value's meaning, its earlier spellings, its deprecation |

A string property with `enum` MUST include the value `"UNKNOWN"` in `enum` and in `x-atlas-enum`, except in a request schema, where `UNKNOWN` MUST NOT be accepted.

**Logical types** (the complete list for `atlas/1.0`; a domain's own types are added by a protocol minor version):

| Name | Base type | Constraint |
|---|---|---|
| `Bp`, `Percent`, `Fraction`, `Years` | `number` | Units, §9.5 |
| `Decimal(p,s)` (`p` 1–38, `s` 0–`p`) | `string`, `format: decimal` | Precision and scale |
| `Money` | `object` with `amount` (`Decimal(38,6)`) and `currency` (`Currency`), both required | — |
| `Currency` | `string` | `pattern: ^[A-Z]{3}$` |
| `Country` | `string` | `pattern: ^[A-Z]{2}$` |
| `Isin` | `string` | `pattern: ^[A-Z]{2}[A-Z0-9]{9}[0-9]$` |
| `Cusip` | `string` | `pattern: ^[A-Z0-9]{8}[0-9]$` |
| `Sedol` | `string` | `pattern: ^[B-DF-HJ-NP-TV-Z0-9]{6}[0-9]$` |
| `Figi` | `string` | `pattern: ^BBG[B-DF-HJ-NP-TV-Z0-9]{8}[0-9]$` |
| `Lei` | `string` | `pattern: ^[A-Z0-9]{18}[0-9]{2}$` |
| `Tenor` | `string` | `pattern: ^[1-9][0-9]*[DWMY]$` |
| `PortfolioCode`, `IndexCode` | `string` | `minLength: 1` |
| `Path` | `string` | `minLength: 1` |
| `Principal` | `string` | `format: uuid` |
| `Uri` | `string` | `format: uri` |

Check digits are not validated by the protocol; a capability may refuse an identifier with its own problem.

### 9.2.4 Regular expressions

Every `pattern` MUST have the form `^ R $`, where *R* is an **I-Regexp** (RFC 9485), and is matched against the whole value's Unicode code points. I-Regexp is the subset of regular expressions every language's engine agrees on, and has no anchors of its own; the `^` and `$` here only make the whole-value match explicit to engines that search. A validator translates *R* to its own engine as RFC 9485's section 5 describes. Patterns are case-sensitive.

### 9.2.5 Validating

To validate an instance against a schema in this profile:

1. Apply the 2020-12 Validation and Applicator semantics to the keywords of 9.2.1, and the format assertions of 9.2.2.
2. Ignore the Meta-data and Atlas keywords for the result (they never make an instance invalid), except `x-atlas-logical`, whose constraint in 9.2.3 is applied as if written as its own keywords.
3. Produce either `valid`, or every failure, each with the instance's JSON Pointer (RFC 6901) and the keyword that failed. The problem for an invalid request is `invalid_request`, with each failure in `details` and the first one's pointer as `target`.

### 9.2.6 The contract is encoding-neutral

A profile's schemas are the contract; every encoding a result travels in is derived from them by a fixed mapping, and none is the contract. The writer and reader sides of every contract (who declares, who resolves) do not depend on any encoding.

| Encoding | Media type | Mapping | Level |
|---|---|---|---|
| JSON | `application/json` | The schema itself | Core |
| NDJSON | `application/x-ndjson` | One JSON record per line (§9.6.5) | Streaming |
| Arrow IPC | `application/vnd.apache.arrow.stream` | 9.2.7 | Columnar |
| Parquet | `application/vnd.apache.parquet` | Parquet's logical types for the Arrow types of 9.2.7 | Bulk |
| CSV | `text/csv` | Display only, never read by a client | — |

A new encoding (Avro, should a consumer ever need it) is a protocol minor version: a media type, a mapping and a framing; no profile, fingerprint or capability changes.

The Atlas types are Atlas's own, chosen to be carried exactly by every encoding above; they are not borrowed from any encoding or engine. Two choices are recorded so they are not mistaken for inherited ones: `Decimal(p,s)` allows a precision up to 38 because that is the largest the firm's data uses and every encoding above carries it exactly; and a `date-time` is exact to the microsecond in every encoding (the grammar allows more digits, which a receiver truncates, §9.1.4).

### 9.2.7 Tables as Arrow

A `table` result's schema is a JSON Schema of an `array` of row `object`s. Its **Arrow schema is derived** from it, not written separately, by this exact mapping (one Arrow field per row property, in the order of `properties`):

| Row property schema | Arrow type |
|---|---|
| `type: boolean` | `Bool` |
| `type: integer` | `Int64` |
| `type: number` | `Float64` |
| `type: string` (no format, no logical type) | `Utf8` |
| `format: date` | `Date32` (days since the epoch) |
| `format: date-time` | `Timestamp(MICROSECOND, "UTC")` |
| `format: duration` | `Duration(MICROSECOND)` |
| `format: int64` | `Int64` |
| `x-atlas-logical: Decimal(p,s)` | `Decimal128(p, s)` |
| `x-atlas-logical: Money` | `Struct<amount: Decimal128(38,6), currency: Utf8>` |
| `type: array`, `items: S` | `List<T>`, where `T` is the Arrow type `S` maps to by this table |
| `type: object` with `properties` | `Struct` of the mapped properties |

Nullability: an Arrow field is nullable exactly when its property allows `null` or is not in `required`. Every Atlas keyword on the property is carried as the field's Arrow metadata, under the key `atlas:<keyword without x-atlas->` (`atlas:logical`, `atlas:key`, `atlas:aliases`), its value the keyword's JSON text. The stream's schema metadata carries `atlas:profile` (the `exact-id` it was written under) and `atlas:protocol` (`atlas/1.0`).

## Rules

1. The gateway MUST NOT serve a profile whose schemas leave this profile; the gate and registration refuse it.
2. A client MUST validate every request against its schema before sending it, by 9.2.5.
3. Every table result's Arrow schema is derived by 9.2.7, never written by hand.

## Units

The meta-schema for schemas in this profile: `references/schemas/atlas-schema.schema.json` (§9.3).

## Requirements

- **AP-SCHEMA-001.** Every client's validator gives the same result as the schema test vectors for every case.
- **AP-SCHEMA-002.** Every client and the gateway derives the same Arrow schema from the same table schema (the Arrow test vectors).
- **AP-SCHEMA-003.** The same result, sent in every encoding its capability offers, resolves to identical records.
