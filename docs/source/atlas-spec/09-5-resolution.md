# §9.5 Resolution

`spec/protocol/resolution.md` · proposed

> Section 9.5 of the Atlas Protocol: the exact algorithm by which a reader turns a payload written under one profile into the shape of the profile it knows, in either direction along a chain of profiles; the exact forward and backward meaning of every lens operation; the unit table and its arithmetic; promotions; enums; and the test vectors.

## Purpose

To let a reader read anything written under another profile of the same capability, the same way in every language.

## Concepts

### 9.5.1 Terms

- A **record** is a JSON object: a request, a result row, a `record` result, a resource, a plan, a receipt, an event's `data`. Resolution acts on one record at a time; a `table` is resolved row by row (or column by column, 9.5.7); a `collection` item by item.
- The **writer profile** *W* is the one the record was written under (named by `Atlas-Profile`, `Atlas-Profile-Expect`, `dataschema` or `atlas:profile`). The **reader profile** *R* is the one the reader knows.
- The **chain** is the sequence of profiles *P₀ … Pₙ* of the capability, oldest first, from whichever of *R* and *W* is older to whichever is newer, each *Pₖ* declaring in `evolution.lens` its lens from *Pₖ₋₁* (an empty lens when only safe changes separate them). The reader obtains every profile in the chain by fingerprint (`atlas.describe` with `exact`), and caches them.
- A lens operation names a `field`: a **top-level** property of the record. (`atlas/1.0` has no lens operations on nested properties; a change to a nested property that needs one is a new version.)

### 9.5.2 The algorithm

To resolve record *x*, written under *W*, for reader *R*:

1. If `fingerprint(W) = fingerprint(R)`, or the capability has the trait `DynamicSchema` (§9.3), return *x* unchanged.
2. Build the chain *P₀ … Pₙ*. If *R* and *W* are not profiles of the same `id@vN`, fail with `resolution_failed`. (Across a version boundary only the gateway resolves, by the lens a version declares from the one before it (`evolution.previous_version`): to answer a pinned `@v1` call from `@v2`'s data, and in `atlas.compare`. A client never resolves across versions: a reader pinned to `@v1` calls `@v1`.)
3. Let *y* be a copy of *x*.
4. **Apply the lenses.** If *W* is newer (*W = Pₙ, R = P₀*): for *k* from *n* down to 1, apply every operation of *Pₖ*'s lens to *y* **backward**, in reverse order of the lens's list. If *W* is older (*W = P₀, R = Pₙ*): for *k* from 1 up to *n*, apply every operation of *Pₖ*'s lens to *y* **forward**, in the lens's order.
5. **Match against the reader's schema.** Let *S* be *R*'s schema for this record, **projected**: when the record is a row of a table answer to a request that named `columns`, *S* keeps only the properties named there (as the reader names them), in `properties` and in `required`. Build the result `z = {}`; for each property *p* of `S.properties`, in order:
6. If *y* has a member named *p*, take its value.
7. Else, if *y* has a member named one of `S.properties[p].x-atlas-aliases`, take the value of the last such alias in that list that is present.
8. Else, if `S.properties[p]` has `default`, take the default.
9. Else, if `S.properties[p]` allows `null` and *p* is in `S.required`, take `null`.
10. Else, if *p* is not in `S.required`, leave *p* absent and continue.
11. Else, fail with `resolution_failed`, `target / p`.
12. **Convert** the value to `S.properties[p]` by 9.5.5, failing with `resolution_failed`, `target / p`, if it cannot be.
13. Set `z[p]` to it.
14. Members of *y* that no property of *S* took are dropped.
15. Return *z*.

### 9.5.3 Lens operations, forward and backward

Each operation is declared by profile *Pₖ*, describing the change from *Pₖ₋₁* to *Pₖ*. “Forward” turns a record of *Pₖ₋₁* into one of *Pₖ*; “backward” turns a record of *Pₖ* into one of *Pₖ₋₁*. Where a field's value is an array, a value operation (`alias_value`, `convert_unit`, `promote`) applies to each element.

| Operation | Forward | Backward |
|---|---|---|
| `rename {from, to}` | If `from` is present: move its value to `to`, remove `from` | If `to` is present: move its value to `from`, remove `to` |
| `alias_value {field, from, to}` | Where `field`'s value equals `from`: replace it with `to` | Where it equals `to`: replace it with `from` |
| `convert_unit {field, from, to}` | Convert `field`'s value from unit `from` to `to` (9.5.4) | Convert it from `to` to `from` |
| `promote {field, from, to}` | Convert `field`'s value from type `from` to `to` (9.5.5) | Convert it from `to` to `from`, only if exact; otherwise fail with `resolution_failed` |
| `default {field, value}` | If `field` is absent: set it to `value` | Remove `field` |
| `drop {field}` | Remove `field` | Nothing (step 5 then supplies a default, `null`, or fails) |

A lens operation whose `field` is absent from the record does nothing, except `default` forward. `null` passes through every value operation unchanged.

### 9.5.4 Units and their arithmetic

A number's unit is given by its logical type (`Bp`, `Percent`, `Fraction`, `Years`) or by `x-atlas-unit`:

| `x-atlas-unit` | Same as | Dimension |
|---|---|---|
| `bp` | `Bp` | ratio |
| `percent` | `Percent` | ratio |
| `fraction` | `Fraction` | ratio |
| `years` | `Years` | time (converts to nothing) |
| `days`, `seconds` | — | time: one day is 86400 seconds exactly |
| `count` | — | none (converts to nothing) |
| `currency_unit` | — | money in the currency its row names (converts to nothing) |

Conversions exist only within a dimension, and only these:

| From | To | Operation on a `number` (IEEE 754 binary64, round to nearest, ties to even) |
|---|---|---|
| `Fraction` | `Percent` | multiply by 100 |
| `Percent` | `Fraction` | divide by 100 |
| `Fraction` | `Bp` | multiply by 10000 |
| `Bp` | `Fraction` | divide by 10000 |
| `Percent` | `Bp` | multiply by 100 |
| `Bp` | `Percent` | divide by 100 |
| `days` | `seconds` | multiply by 86400 |
| `seconds` | `days` | divide by 86400 |

Exactly one multiplication or one division, as written: dividing by 10000 is not the same operation as multiplying by 0.0001 in binary64. On a `Decimal(p,s)` string, the same factor is applied in exact decimal arithmetic and the result rounded to the reader's scale, ties to even. No other conversion between units exists in `atlas/1.0` (`Years` converts to nothing).

### 9.5.5 Converting a value to the reader's type

| Writer's value | Reader's schema | Result |
|---|---|---|
| Any value valid against the reader's property schema | — | Unchanged |
| A JSON integer | `type: number` | The same value |
| A JSON number with no fractional part, within I-JSON | `type: integer` | The same value (only when converting backward a `promote`) |
| A `date` string | `format: date-time` | The date at `T00:00:00Z` |
| A `string` not in the reader's `enum` | `enum` | If it equals an alias in the reader's `x-atlas-enum`, that entry's `value`; else, if the reader's `enum` has `UNKNOWN`, `"UNKNOWN"`; else fail |
| An object | `type: object` with `properties` | Resolved recursively by step 5 with that property's schema (no lenses) |
| An array | `type: array` | Each element converted by this table against `items` |
| `null` | A schema allowing `null` | `null` |
| Anything else | — | Fail with `resolution_failed` |

### 9.5.6 Where resolution runs

| Payload | Writer | Resolved by |
|---|---|---|
| A request | The client, under `Atlas-Profile-Expect` | The gateway, before validating it against its current request schema |
| A result, resource, plan, receipt, operation result | The gateway, under `Atlas-Profile` | The client, against its lock or its newest fetched profile |
| An event's `data` | The publisher, under `dataschema` | Each subscriber |

When the gateway knows from `Atlas-Client-Features` that the client lacks a lens operation needed for the chain, it resolves the result to the client's expected profile itself before sending it, and says so with `Atlas-Profile` set to the client's expected `exact-id`.

### 9.5.7 Columns

A client at the Columnar level resolves an Arrow record batch column by column: a lens operation is applied to a whole column (a rename renames the field; a unit conversion is one vectorised multiplication or division); the result MUST equal resolving each row by 9.5.2.

### 9.5.8 Test vectors

`references/vectors/resolution.json`: each case gives the chain (each profile's schema and lens), the reader and the writer, the input, the requested `columns` where there are any, and the expected record or problem. The cases cover: identical profiles; an unknown field ignored; a rename backward and forward; a unit conversion; an unknown enum value; an enum value renamed; a promotion; a default; a required field missing; a nullable field missing; an old request resolved forward by the gateway; a projected row that lacks the required columns it did not ask for; a projected row whose requested column was renamed; profiles of two different versions (refused).

## Rules

1. Resolution never consults anything but the profiles in the chain and the record.
2. Resolution never changes a value it does not have to.

## Units

`references/vectors/resolution.json`.

## Requirements

- **AP-RESOLVE-001.** Every client and the gateway produces, for every case in the resolution vectors, exactly the expected record, or a problem with the expected `code` and `target`.
- **AP-RESOLVE-002.** A Columnar client's column-wise resolution equals its row-wise resolution on every vector, as a table of one row and as a table of many.
