# §9.4 Fingerprints

`spec/protocol/fingerprints.md` · proposed

> Section 9.4 of the Atlas Protocol: the exact algorithm for a profile's fingerprint (which members are removed as prose, at which positions in its schemas; RFC 8785 canonical JSON; SHA-256; the first 16 lowercase hexadecimal characters), its uses, and its test vectors.

## Purpose

To give every profile one identity that every language computes identically, so that a client and the gateway agree, byte for byte, on whether they mean the same contract.

## Concepts

### 9.4.1 The projection

The fingerprint is computed over the profile's **projection**: the profile with every member removed that does not change how a client calls, resolves or reads it. To compute the projection of profile *P*:

1. Copy *P*.
2. Remove the top-level members `fingerprint`, `revision` and `usage`.
3. Remove `identity.title`, `identity.summary` and `identity.owner`.
4. Remove `description` and `next` from every member of `errors`.
5. Remove `evolution.changelog`.
6. For each member of `schemas`, apply **StripSchema** to its value, where StripSchema(*S*), for an object *S*:
7. remove the members `title`, `description`, `$comment` and `examples` of *S*;
8. apply StripSchema to every value of `S.properties` and of `S.$defs`, to `S.items`, to `S.additionalProperties` if it is an object, and to every element of `S.prefixItems`, `S.anyOf` and `S.oneOf`;
9. remove `description` from every element of `S.x-atlas-enum`;
10. remove `reason` from `S.x-atlas-deprecated`, if it is an object.

StripSchema removes keywords only at **schema positions**: a property *named* `title` under `properties` is kept. 7. Leave everything else, including `meaning`, unchanged.

### 9.4.2 The fingerprint

1. Serialise the projection by the **JSON Canonicalization Scheme** (RFC 8785): members sorted by their names' UTF-16 code units, no whitespace, strings with only the escapes RFC 8785 requires, numbers in ECMAScript's shortest form.
2. Encode it as UTF-8.
3. Compute its SHA-256 (FIPS 180-4).
4. Write the digest as 64 lowercase hexadecimal characters; the **fingerprint** is the first 16.

The gateway computes the fingerprint when it registers a profile and serves it in the profile's `fingerprint` member; a client recomputes it on every profile it receives and treats a mismatch as `invalid_document`.

### 9.4.3 Where fingerprints are used

| Use | Form |
|---|---|
| Naming one exact contract | `exact-id`, `portfolio.holdings@v1#3f9a1c0b7d2e4a65` |
| A profile's HTTP `ETag` | `"3f9a1c0b7d2e4a65"` (a strong ETag) |
| `Atlas-Profile`, `Atlas-Profile-Expect` headers | An `exact-id` as a Structured Field string (§9.6) |
| An event's `dataschema` | `urn:atlas:profile:portfolio.holdings@v1:3f9a1c0b7d2e4a65` |
| An Arrow stream's `atlas:profile` metadata | An `exact-id` |
| A lock's entry | `fingerprint` |
| An audit record | The writer's and the expected `exact-id` |

### 9.4.4 Test vectors

`references/vectors/fingerprints.json` gives profiles with their canonical form, SHA-256 and fingerprint, including: a complete data profile; the same profile with its prose changed and members reordered (the same fingerprint); a column added (a new fingerprint); non-ASCII text, a quote and a tab in a kept value. `references/vectors/profiles/` holds the complete profiles, each valid against `profile.schema.json` and each of its schemas valid against `atlas-schema.schema.json`.

## Rules

1. The gateway and every client computes fingerprints by exactly this algorithm.
2. The gateway never serves two different projections under one fingerprint; were two to collide, the newer is refused at registration.

## Units

`references/vectors/fingerprints.json`, `references/vectors/profiles/`.

## Requirements

- **AP-FP-001.** Every client and the gateway produces, for every case in the fingerprint vectors, exactly the canonical form, SHA-256 and fingerprint given.
