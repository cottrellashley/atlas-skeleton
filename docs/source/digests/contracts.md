# Digest: contracts and capabilities (profile, families, traits, readiness, evolution, errors)

> Historical engineering study (2026-10-31). Retained for its requirements, counterexamples and design reasoning. Implementation observations and line references describe that review snapshot; use [current progress](../../HANDOFF-PROGRESS.md) and the named source symbols for current status. Paths are relative to the repository root unless a citation key says otherwise.

Reader: contracts. Written 2026-10-31.

## Sources and how to read the citations

| Short name | File | Read |
|---|---|---|
| C | `docs/source/atlas-spec/03-contracts.md` (875 lines) | in full |
| CAP | `docs/source/atlas-spec/02-1-capabilities.md` (207 lines) | in full |
| SURF | `docs/source/atlas-spec/02-the-surface.md` (65 lines) | in full |
| TERMS | `docs/source/atlas-spec/01-terms-and-conventions.md` (76 lines) | in full |
| V-data, V-res, V-act, V-evt | `spec/protocol/references/vectors/profiles/{data,resource,action,event}.json` | in full |
| PS | `spec/protocol/references/schemas/profile.schema.json` | in full (needed for required-ness) |
| BS | `spec/protocol/references/schemas/binding.schema.json` | in full |
| ASM | `spec/protocol/references/schemas/atlas-schema.schema.json` | its Atlas keyword `$defs` and allowed-keyword list only |
| PROB | `spec/protocol/references/schemas/problem.schema.json` | members and registry only |
| Q | `spec/protocol/references/QUESTIONS.md` | in full |
| GR | `spec/protocol/references/grammar.abnf` lines 15-40 | identifiers only |
| FP | `atlas-spec/09-4-fingerprints.md` | in full (projection) |
| PC | `tools/spec/atlasref/profilecheck.py` lines 1-46 and 300-529 | readiness and lint functions |
| PY | `tools/spec/atlasref/profiles.py` | in full (the vectors' source) |
| REVIEW | `docs/pass-2/REVIEW.md` | in full; governs this build |
| CORE | `atlas-spec/review/ATLAS-CORE.md` | in full |
| HB | `atlas-spec/review/CAPABILITY-HANDBOOK.md` | lines 1-254, 354-363, 531-595 |
| DR | `atlas-spec/review/DESIGN-REVIEW.md` | in full |

"C:133" means 03-contracts.md line 133. "§x.y" is the spec's section number. Every vector path is under `spec/protocol/references/`.

Markers: **IN SCOPE** = the first build (the journey: `portfolio.holdings@v1`, `portfolio.list@v1`, `portfolio.search@v1`, `data.status@v1`, then `portfolio.holdings-dates@v1`) must implement it. **DEFERRED** = the review or REVIEW.md defers it. **OVERRIDDEN BY REVIEW: Rnn** = REVIEW.md or the review changes the spec text. **AMBIGUOUS:** = the sources leave it open or contradict each other, with each reading.

Precedence, as the sources set it:
1. REVIEW.md governs PLAN.md and records every departure from the spec (REVIEW:5).
2. §9 is normative for the wire; §2 to §6 explain it and never contradict it (TERMS:51).
3. The references take a reading where §9 is silent, and record it in Q (Q:3).
4. "The normative structure is `profile.schema.json` (§9.3), and complete, valid profiles are the test vectors in `references/vectors/profiles/`" (C:150).

---

## 1. The profile, member by member

### 1.0 What a profile is

- "A capability's contract is its profile and nothing else" (C:46). "The contract is the profile: properties, interactions and bindings, schemas, errors, meaning, compatibility, usage, evolution, with a fingerprint. Governance (who may, limits) and availability (live health) sit beside it" (C:11).
- Rule 1: "A profile is complete: a client MUST be able to discover, call and read the capability from its profile and the protocol alone" (C:105).
- Rule 2: "Every response and every event names the fingerprint of the profile it was produced under" (C:106).
- Rule 3: "A profile is served by fingerprint for as long as anything may hold it (at least until its version's sunset)" (C:107).
- Rule 4: "Prose may change at any time (a new revision); a change to the canonical form is a new fingerprint and is classified" (C:108).
- Rule 5: "Governance and availability are never part of a profile or its fingerprint" (C:109).
- **ATLAS-CONTRACT-001.** "Every profile carries the schemas its surface requires, and the gate refuses one that lacks any" (C:45).
- Where it lives (C:347): "a file in its service's package (`profiles/<id>@v<N>.json`: the profile without `fingerprint`, which the gateway computes) and served by the gateway (`atlas.describe`). Its schemas are written directly in the Atlas Schema profile; the service reads requests as records validated against them, never through a second model that could drift." AGENTS.md "Adding a capability" says the same: "Write its profile first, under `profiles/<id>@v<N>.json` in its service's package. Lint it and classify it against the previous profile."
  - Consequence: the file on disk lacks `fingerprint`, which PS requires. So the gate computes the fingerprint (§9.4) before it validates against PS. PY:4-5 does exactly this ("`fingerprint.with_fingerprint` adds it, as the gateway serves it").
- Two checks: "the gate, on every pull request, and the gateway itself, when a service registers (§3.3 3.3.12)" (C:350). **IN SCOPE** (REVIEW step 2: "The four journey profiles pass the gate and the readiness check").
- Catalogue snapshot: "a generated snapshot of every profile sits beside it, with each owner's words, so a change to any contract is a diff in the pull request that makes it" (C:349). **ATLAS-PROFILE-003** "The catalogue snapshot matches the profiles the gateway serves at the same commit; the gate fails otherwise" (C:113). **IN SCOPE**. R23 adds: "Catalogue summaries and counts are generated; examples, registered profiles and deployed availability are kept distinct" (REVIEW R23).
- The review keeps the profile as the one definition: "There should be one profile beside the implementation, not an independently maintained manual and a second schema language" (HB:108). The capability card is "documentation requirements, not a new schema language" (CORE:62).

### 1.1 Three documents, three revisions (§3.1.1, C:119-129)

| Document | Says | Changes when | Revision |
|---|---|---|---|
| **Profile** (the contract) | What the capability is, how to call it, what it returns, how it fails, what it means | Its owner changes the capability | The **fingerprint** of its canonical form |
| **Governance** | Who may use each interaction, their limits, budgets and approval policy | Administrators change access or limits | A **governance revision**, recorded in every audit record |
| **Availability** | `available`, or `unavailable` (its service is unhealthy) | Live state | None |

- "`atlas.describe` returns all three; a client caches the profile by fingerprint and never caches availability" (C:129).
- **ATLAS-PROFILE-001** "`atlas.describe` returns a capability's full profile, by `id@vN` or by fingerprint, with its governance for the caller and its availability" (C:111). **IN SCOPE**.
- **ATLAS-PROFILE-004** "A change to a limit, a grant or a service's health leaves every fingerprint unchanged" (C:114). **ATLAS-VERSION-008** "A change to governance never changes a fingerprint" (C:549). **IN SCOPE**.
- **OVERRIDDEN BY REVIEW: R01.** The spec says the fingerprint is the profile's strong ETag, profiles are cached forever, and describe can be immutable (REVIEW R01 "The spec says"). Pass 2: "The contract (by fingerprint) is cached apart from the document revision. Describe's caller state (governance, availability) is private and uncached. A guide's identity includes the prose revision" (REVIEW R01). CORE:154-160 gives three revisions:

  | Revision | What it identifies | Cache rule |
  |---|---|---|
  | Contract fingerprint | Immutable semantic and schema contract | May be retained and bundled with the program |
  | Document revision / HTTP validator | The actual served description or representation | Changes when the served content changes |
  | Policy and runtime information | Current access, limits, routes, supported delivery and health | Revalidated as appropriate; never authority to execute later |

  R01 acceptance (DR:38): "Change guide wording, revoke a grant and suspend a source without changing the contract digest. The guide updates, invocation is refused appropriately, and no cached access response authorizes the call."
  - AMBIGUOUS: the exact form of the new "document revision / HTTP validator" for a served profile. Readings: (a) an ETag built from fingerprint plus `revision`; (b) a hash of the full served document. Neither the spec nor the review fixes it. C:333 ("is the profile's HTTP `ETag`") and FP §9.4.3 (`"3f9a1c0b7d2e4a65"` as a strong ETag) no longer hold as written, because prose changes the representation without moving the fingerprint.

The governance example (C:315-328), shown under the event example but for `portfolio.holdings@v1`:

```json
{
  "capability": "portfolio.holdings@v1",
  "governance_revision": "g-41",
  "for_caller": {
    "interactions": {"fetch": {"held_by": "role:fi-analysts"}, "export": {"held_by": "role:fi-analysts"}},
    "limits": [{"interaction": "fetch", "kind": "rate", "per_minute": 60, "remaining": 58, "set_by": "role:fi-analysts"},
               {"interaction": "fetch", "kind": "concurrency", "max": 4},
               {"interaction": "fetch", "kind": "output", "max_rows": 1000000, "max_bytes": 1073741824}],
    "approval": null
  }
}
```

The narrative at C:95 uses fingerprint `bcaa5bbad97cc612`, role `fi-analysts`, "58 calls left this minute", revision `g-41`, availability `available`. Its normative shape is `schemas/governance.schema.json`, which another reader covers.

### 1.2 Top level (C:133-145; PS:7-15, 16-294)

The required list is from PS:7-15: `identity`, `properties`, `interactions`, `schemas`, `errors`, `meaning`, `fingerprint`. A member that is "In projection" is part of the fingerprint (FP §9.4.1).

| Member | Type (PS) | Required (PS) | In projection | Meaning (C:133-145) |
|---|---|---|---|---|
| `identity` | object | yes | yes, except `title`, `summary`, `owner` | `id`, `version`, `title`, `summary`, `domain`, `owner` |
| `properties` | object | yes | yes | `family`, `stability`, `lifecycle` (with `sunset`, `replaced_by`), `visibility`, `traits` (§2.1) |
| `interactions` | object; `minProperties: 1`; names `^[a-z](-?[a-z0-9])*$`; values `$defs/interaction` | yes | yes | "Per verb: `effect`, `result`, `delivery`, `execution`, `bindings`, the names of the schemas it takes and returns, and its `deadline` (`default`, `max`)" |
| `schemas` | object; names `^[a-z][a-z0-9_]*$`; values objects | yes | yes, after StripSchema | "The family's schemas (§3 B), in JSON Schema 2020-12 with the Atlas vocabulary (§3.2); for a table, its Arrow schema too" (but see Q-DOC-6 below) |
| `errors` | array of `$defs/error` | yes (may be `[]`: V-evt `"errors": []`) | yes, except each `description` and `next` | "Every problem code this capability can return beyond the protocol's own, each with its status, whether it is `retryable`, what it means (`description`) and the step to take (`next`) (§4.1)" |
| `meaning` | object (no fixed members in PS) | yes | yes, whole, prose included ("Leave everything else, including `meaning`, unchanged", FP step 7) | "What changes only by a new version: for data, the grain, keys, as-of rule and source; for a resource, its name pattern and immutable fields; for an action, its effects and policy; for an event, when it is emitted, its subject, ordering and retention" |
| `compatibility` | object: schema name to one of `BACKWARD`, `BACKWARD_TRANSITIVE`, `FORWARD`, `FORWARD_TRANSITIVE`, `FULL`, `FULL_TRANSITIVE`, `UNKNOWN` | no | yes | "The mode each schema is held to within the version (§3.3)" |
| `usage` | `$defs/usage` | no (but the readiness check needs `when` and an example) | **no**: whole member removed | "What `atlas guide` is written from (§2.6): `when` and `when_not` (words); `examples` (each a `title` and a request valid against the request schema; per-client code is generated from them); `related` capabilities; `cost`; `skill` (the agent's skill text, managed by administrators)" |
| `evolution` | `$defs/evolution` | no | yes, except `changelog` | "The profile it replaces within the version and the lens from it; for a version after `@v1`, the version it replaces and the lens from it; a changelog" |
| `fingerprint` | string `^[0-9a-f]{16}$` | yes (as served) | no (removed) | "Of the canonical form (3.1.4)". PS: "a client recomputes it and refuses a mismatch" |
| `revision` | integer, `minimum: 0` | no | no (removed) | "Of the prose (titles, descriptions, usage), which changes without changing the fingerprint" |

Fingerprint (C:333): "the first 16 lowercase hexadecimal characters of the SHA-256 of the profile's projection ... serialised by the JSON Canonicalization Scheme (RFC 8785)". Written `id@vN#<fingerprint>`; "recorded in every audit record. Two profiles with the same fingerprint behave identically." FP §9.4.1 gives the projection:
1. Remove top-level `fingerprint`, `revision`, `usage`.
2. Remove `identity.title`, `identity.summary`, `identity.owner`.
3. Remove `description` and `next` from every member of `errors`.
4. Remove `evolution.changelog`.
5. StripSchema on each `schemas` member: remove `title`, `description`, `$comment`, `examples`, recursing through `properties`, `$defs`, `items`, object `additionalProperties`, `prefixItems`, `anyOf`, `oneOf`; remove `description` from every `x-atlas-enum` element; remove `reason` from an object `x-atlas-deprecated`. "StripSchema removes keywords only at **schema positions**: a property *named* `title` under `properties` is kept."

FP §9.4.2: "a client recomputes it on every profile it receives and treats a mismatch as `invalid_document`." **ATLAS-PROFILE-002** "The gateway and every client compute the same fingerprint for the same profile" (C:112). **IN SCOPE.**

### 1.3 `identity` (PS:17-55; CAP:90-95)

| Member | Type | Required (PS) | Meaning / constraint |
|---|---|---|---|
| `id` | string, `format: capability-id` | yes | "`<domain>.<name>[.<name>]…`" (CAP:92). GR:23 `capability-id = segment 1*( "." segment )`, at most 128 characters. GR:19 `segment = %x61-7A *( %x61-7A / DIGIT / "-" %x61-7A / "-" DIGIT )`: a-z first, never ends with `-`, never contains `--` |
| `version` | integer, `minimum: 1` | yes | "Its major version; a new one only when its meaning changes (§3.3)" (CAP:93). PS: "1 for @v1" |
| `title` | string | no | Prose: removed by the projection |
| `summary` | string | no in PS; **yes for readiness** | "One sentence of at most 200 characters (§3.1.4a). Prose." |
| `domain` | string `^[a-z](-?[a-z0-9])*$` | yes | "the first segment of the id"; "Its namespace; each domain has one owner" (CAP:94) |
| `owner` | string, `minLength: 1` | yes | "Who answers for its meaning and approves its changes" (CAP:95). Prose: removed by the projection |

- **ATLAS-CAP-002** "An id matches `^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)+$`; only built-in capabilities have the domain `atlas`; a version is a positive integer; an interaction is one its family defines" (CAP:53).
  - AMBIGUOUS: CAP-002's regex accepts `a--b` and `a-`; GR:19-21 and PS's `domain` pattern refuse them (GR quotes §9.1.3: "a segment never ends with -, and never contains --"). Reading: §9 governs (TERMS:51), so use GR.
- **ATLAS-CAP-003** "Every request through every edge resolves to exactly one address before it is authorised" (CAP:54). The address is `<id>@v<N>:<interaction>` (CAP:71). GR:33 `address = ( capability-id / pinned-id ) ":" interaction`, at most 160 characters. GR:31 `pinned-id = capability-id "@" version`, `version = %s"v" nz-digits`. GR:32 `exact-id = pinned-id "#" fingerprint`.
- Names (CAP:171-190): `atlas.*` only for built-ins; subject domains named generically ("no `harbour.*`, `snowflake.*`, `gold.*`"); provider domains for what the team does not own. "It names a concept, never a verb, where it runs, or the system that happens to store the data" (CAP:171). For data, "`list` and `search` count as nouns: a domain's catalogue and its lookup" (CAP:187).
- Rule: "A name is for people, agents and governance. No router, edge or service MAY derive where or how to serve a call by parsing a name; routing follows bindings" (CAP:47).
- AMBIGUOUS / MISSING: CORE:66 requires the Identity part of a capability card to hold "Stable id and major version, title, owner, lifecycle and support contact". HB:123 asks for "an owner/support route". PS has no member for a support contact, and `identity` does not set `additionalProperties: false`. Readings:
  - (a) `owner` serves as the contact;
  - (b) add a member, such as `identity.support`. A new member would be in the fingerprint unless the §9.4.1 projection is extended to strip it, and that is a protocol change.
  Not decided anywhere.

### 1.4 `properties` (PS:56-202; CAP:96-108)

| Member | Type / values (PS) | Required | Meaning |
|---|---|---|---|
| `family` | `data`, `resource`, `action`, `event`, `UNKNOWN` | yes | "Its surface (§2.1.B)" |
| `stability` | `official`, `community`, `upstream`, `UNKNOWN` | yes | See the stability table below |
| `lifecycle` | `preview`, `stable`, `deprecated`, `retired`, `UNKNOWN` | yes | "Where it is in its life; `deprecated` carries a sunset date and a replacement" (CAP:98) |
| `deprecated` | string, `format: date` | no | "When it was deprecated: what Deprecation reports (§3.3.9, Q-DOC-5)". Added by the references (Q-DOC-5) |
| `sunset` | string, `format: date` | no | "When a deprecated version ends" |
| `replaced_by` | `capability-id` or `pinned-id` | no | "What replaces a deprecated version" |
| `visibility` | `listed`, `confidential`, `UNKNOWN` | yes | "Whether a caller who cannot use it may learn it exists (§2.6)" (CAP:99) |
| `traits` | array, `uniqueItems`, of `{name, critical}` | yes | "Uniform behaviours it implements" (CAP:100) |

- Trait object (PS:296-313): `name` string `^[A-Z][A-Za-z0-9]*$` ("a client declares the lowercased names it understands in Atlas-Client-Features"); `critical` boolean ("When true, a client that does not implement it must not call the capability (critical_trait_unsupported)"). Both are required.
  - C:159 shows traits as bare names (`"traits": ["Projectable", ...]`). C:150 says that example is abbreviated ("traits are shown by name"). Every vector uses objects with `"critical": false`.
- Stability (CAP:102-106):

  | Stability | Promises | Changes reviewed by |
  |---|---|---|
  | `official` | Full compatibility within a version; its meaning written by its owner | Its domain's owner, under the strict gates |
  | `community` | The same compatibility rules; may be withdrawn with notice | Its owning team, under lighter review |
  | `upstream` | A provider's own response in Atlas's envelope; its schema observed, fingerprinted and checked for drift, not promised | Its domain's owner |

  "Anything an application, dashboard, pipeline or job depends on SHOULD be `official` or `community`" (CAP:108).
- **ATLAS-CAP-001** "Every capability declares `id`, `version`, `domain`, `owner`, `family`, `stability`, `lifecycle`, `visibility` and `traits`, and every interaction declares `effect`, `result`, `delivery`, `execution` and at least one binding" (CAP:52).
- CAP rule 2: "A capability's family, stability, visibility and effect classes are part of its contract; changing one is classified by §3.3, and a change of effect is always a new version" (CAP:48).
- AMBIGUOUS: §3.1.2 (C:136) puts `sunset` and `replaced_by` "with" `lifecycle`. PS makes them siblings of `lifecycle` under `properties`. Reading: PS governs (C:150).

### 1.5 `interactions.<verb>` (PS:314-529; CAP:110-145)

| Member | Type / values (PS) | Required (PS) | Meaning |
|---|---|---|---|
| `effect` | `read`, `write`, `external`, `irreversible`, `UNKNOWN` | yes | "How careful a caller must be" |
| `result` | `table`, `record`, `collection`, `content`, `ticket`, `UNKNOWN` | yes | "The shape of what comes back" |
| `delivery` | `single`, `chunked`, `subscription`, `operation`, `transfer`, `UNKNOWN` | yes | "How it comes back" |
| `execution` | array, `minItems: 1`, unique, of `gateway`, `handoff`, `client`, `UNKNOWN` | yes | "Who does the work and moves the bytes" |
| `takes` | string `^[a-z][a-z0-9_]*$` | no | "The name of the schema in `schemas` its request is validated against" |
| `returns` | string `^[a-z][a-z0-9_]*$` | no | "The name of the schema in `schemas` its result is written in" |
| `deadline` | object `{default, max}`, both `format: duration`, both required | **yes** | `default`: "The deadline a call gets when it names none"; `max`: "The longest deadline a call may have" |
| `encodings` | array, unique, of `json`, `ndjson`, `arrow`, `csv`, `parquet`, `cloudevents+json`, `UNKNOWN` | no | "For a transfer or export: the file formats it writes" |
| `bindings` | array, `minItems: 1`, of `binding.schema.json` | yes | "Where it is served, in preference order" |

- **ATLAS-PROFILE-005** "Every interaction declares a deadline maximum, and every capability declares its errors" (C:115).
- In C:166-168 (`explain`, `export`), `deadline` is missing and bindings are `"…"`. C:150 says that example is abbreviated. V-data gives `explain` `{"default": "PT30S", "max": "PT2M"}` and `export` `{"default": "PT10M", "max": "PT2H"}`.
- The schema a request is validated against is the one named by `takes`. In PC:481-484 a schema's role is "request" exactly when some interaction `takes` it. That role decides closed or open (§9.2).
- The lint (PC:499-507) refuses a `takes` or `returns` that names no schema, a missing `deadline.max`, and an interaction with no `https` binding.

Binding (BS). Required: `protocol`, `server`, `address`, `auth`, plus `method` when `protocol` is `https`, and then `address` must match `^/`.

| Member | Values | Meaning |
|---|---|---|
| `protocol` | `https`, `amqp`, `blob`, `snowflake`, `mcp`, `UNKNOWN` | "`sse` and `wss` are reserved for a later minor version (§9.6.5)" |
| `server` | `^[a-z](-?[a-z0-9])*$` | "A server the bootstrap names in `servers`; the interaction's URL is that server's `url` followed by `address` (§9.6.1)" |
| `address` | string, `minLength: 1` | "A path (beginning with `/` for https), channel or topic" |
| `method` | `POST`, `GET`, `UNKNOWN` | "Every atlas/1.0 interaction binding declares POST (§9.6.2)" |
| `encodings` | `json`, `ndjson`, `arrow`, `csv`, `parquet`, `cloudevents+json`, `UNKNOWN` | "The encodings this binding serves, in its preference order (§9.6.4)" |
| `auth` | `bearer`, `entra-scope`, `ticket`, `UNKNOWN` | "its sign-in (`bearer`), its own token for the system (`entra-scope`), or a credential the gateway issued (`ticket`)" |
| `delivery` | the delivery enum | "The delivery this binding answers with, where it differs from its interaction's (a fallback that answers an operation for a stream, §2.1.D; Q-DOC-4)" |

- BS description: "A client reaches an interaction only through a binding its profile or the bootstrap serves, and picks the first it supports, in the order given; it never builds an address."
- Address forms in the vectors: `/v1/data/<id>@v1:<verb>`, `/v1/resource/...`, `/v1/action/...`, `/v1/event/...`; all `server: "gateway"`, `method: "POST"`, `auth: "bearer"`.
- **OVERRIDDEN BY REVIEW: R02.** The spec says "A lock's profile fixes the bindings". Pass 2: "A lock pins semantic contracts. Routing is fetched as current metadata. 'No discovery at runtime' means no new business-route selection" (REVIEW R02; CORE:162-164). Bindings remain in the profile, and so in the fingerprint, but a client must be able to fetch current routing without changing its reader contract.
  - AMBIGUOUS: bindings sit inside the fingerprinted profile, while §3.3.3 (C:594-595) classifies moving a binding as Safe, which gives a new fingerprint. Under R02, which document carries "current routing" apart from the contract is not specified. Readings: (a) the client refetches the current profile and takes only its bindings; (b) a separate routing document. **IN SCOPE** to decide in step 2 (REVIEW step 2: "split the contract fingerprint, the document revision and the routing (R01, R02)").

### 1.6 `schemas` (C:138, C:364-500; PS:214-223; ASM)

- Every request, result, resource, plan, receipt, payload and attributes schema is "a **JSON Schema 2020-12** document" (C:413). The vocabulary is declared in each schema's `$vocabulary` as `https://atlas.jhg.com/vocab/2026-10` (C:415). Each vector schema starts with `"$schema": "https://json-schema.org/draft/2020-12/schema"` and the `$vocabulary` map `core`, `applicator`, `validation`, `meta-data`, `format-assertion` set to `true`, and `"https://atlas.jhg.com/vocab/2026-10": false` (PC:14-22). Q-PROF-15: `$vocabulary` is checked "as a declaration".
- "A validator that does not know the Atlas vocabulary ignores these keywords and still validates correctly; a resolver that knows it uses them" (C:427).
- The Atlas Schema profile (C:490): "a fixed list of allowed keywords and formats, I-Regexp patterns, closed request schemas and open result schemas, limits on depth and size". Allowed keywords (PC:23-36): root only `$schema`, `$vocabulary`, `$id`, `$defs`; `$ref`, `$comment`; `properties`, `additionalProperties`, `items`, `prefixItems`, `anyOf`, `oneOf`; `type`, `enum`, `const`, `required`, `minimum`, `maximum`, `exclusiveMinimum`, `exclusiveMaximum`, `multipleOf`, `minLength`, `maxLength`, `pattern`, `minItems`, `maxItems`, `uniqueItems`, `minProperties`, `maxProperties`; `title`, `description`, `default`, `deprecated`, `examples`, `readOnly`, `writeOnly`; `format`; the seven `x-atlas-*` keywords. Max depth 32 levels; at most 1 MiB in RFC 8785 form (PC:43-44; Q-PROF-12). The detail is §9.2, which another reader covers.
- Naming: "Property names are `snake_case` and never encode a unit (`oas`, not `oas_bp`): the logical type or `x-atlas-unit` carries it" (C:490). GR:37 `property-name = %x61-7A *( %x61-7A / DIGIT / "_" )`.
- Per family, the required schemas (C:51-57):

  | Surface | Schemas in its contract | Meaning in its contract (changes only by a new version) |
  |---|---|---|
  | Data | `request`; `result` (columns, or fields for a record) | Grain, keys, as-of semantics, each column's unit and meaning, the source |
  | Resources | `resource` as served; standard methods' extras; each custom verb's request and result | Identity and name pattern, immutable fields, the stored version, what each custom verb does |
  | Actions | `request`; `plan`; `receipt` | The effects: system, environments, reversibility; the approval policy |
  | Events | `payload`; `attributes` (CloudEvents `subject` and extensions) | When it is emitted, what its subject identifies, ordering, retention |
  | All | `bindings`, `errors`, deadlines, enums, references, traits, lifecycle | Effect classes, surface |

- A table result's JSON Schema is "an array of row objects" (C:431): `{"type": "array", "items": {"type": "object", "required": [...], "properties": {...}}}` (V-data).
- Arrow: **ATLAS-TYPES-002** "A table result's Arrow schema and JSON Schema describe the same columns, by the mapping in 3.2.2" (C:406). Q-DOC-6: "§3.1.3's example carries `result_arrow` in `schemas`; §9.2.7 says the Arrow schema is derived, never written. **Reading:** §9 governs, so profiles carry no Arrow schema." PS:216 says the same. So C:138 ("for a table, its Arrow schema too") and C:196 (`result_arrow`) are not served members.
- **OVERRIDDEN BY REVIEW: R11.** "Core is JSON, and Arrow is an optimisation of the same meaning" (REVIEW R11). Arrow stays derivable, but no client is obliged to Arrow.
- Rules (C:401-407):
  1. "Every property has a type and nullability; a property with a unit, an identifier or a currency has its logical type."
  2. "Every enum in a result, resource, plan, receipt or payload includes `UNKNOWN`, and no enum in a request does (§9.2.3); every reference names a lookup capability that exists."
  3. "Every table result publishes both its JSON Schema and its Arrow schema, and they describe the same columns."
  - **ATLAS-TYPES-001** "Every schema in every served profile validates as JSON Schema 2020-12 and uses only the vocabularies in 3.2.8; the gate refuses one that does not."
  - **ATLAS-TYPES-003** "Every client validates a request against its schema before sending it."
- Nullability (C:474): "A request property not in `required` may be omitted; one with `"type": ["string", "null"]` may be null. A result property in `required` and not allowing `null` is always present and never null."
- Enums (C:466): "every enum a reader receives includes the fallback `UNKNOWN` ... A request's enum never includes it". Q-PROF-10: "A string enum outside a request must have `x-atlas-enum`, since UNKNOWN must be in it. Wherever `x-atlas-enum` appears, its values are exactly the enum's string values." A nullable request enum lists `null` in `enum` (V-data `asset_class`: `"enum": ["credit", "rates", null]`), and `x-atlas-enum` lists only the string values.
- References (C:470): "`x-atlas-reference` names the capability that lists its values (`index.list@v1`). Clients use it to complete and check input; the gateway resolves renamed values by alias."
- Q-PROF-7: "a map (an object with no `properties` whose `additionalProperties` is a schema) is allowed in any schema. Every other object of a request is closed, and every other object outside one is open."

#### Every Atlas keyword (C:417-425; ASM `properties`; PC:305-362; Q)

| Keyword | On | Holds (C) | Exact constraint (ASM / PC) |
|---|---|---|---|
| `x-atlas-logical` | a property | "Its logical type (3.2.3)" | one of `Bp`, `Country`, `Currency`, `Cusip`, `Figi`, `Fraction`, `IndexCode`, `Isin`, `Lei`, `Money`, `Path`, `Percent`, `PortfolioCode`, `Principal`, `Sedol`, `Tenor`, `Uri`, `Years`, or `^Decimal\(([1-9]\|[12][0-9]\|3[0-8]),([0-9]\|[12][0-9]\|3[0-8])\)$`. Its base type must match the logical type's base (PC:306-313). A `Decimal(p,s)` with a `format` needs `format: decimal`. `Money` may declare only `amount` and `currency` (Q-PROF-13) |
| `x-atlas-key` | a property | "`true` when the property is part of the result's key" | `const: true`. Refused in a request schema and off a property position (PC:319). "only on required, non-null result properties (Isin, Cusip, Sedol, Figi and Lei may be missing)" (ASM description; Q-PROF-6) |
| `x-atlas-aliases` | a property, an enum value | "Earlier names, within this version" | array, `minItems: 1`, unique, items `^[a-z][a-z0-9_]*$`. On an enum value it sits as `aliases` inside the `x-atlas-enum` entry |
| `x-atlas-reference` | a property | "The lookup capability that lists its values, `id@vN`" | string, `format: pinned-id` |
| `x-atlas-deprecated` | a property, an enum value | "`{reason, since, sunset, replaced_by}`" | `null` (not deprecated) or an object with exactly those four members, all required (Q-PROF-11): `reason` string; `since` date; `sunset` date; `replaced_by` a property name or `null` (an enum value, on a value) (PC:365-384). On an enum value it sits as `deprecated` inside the `x-atlas-enum` entry |
| `x-atlas-unit` | a numeric property | "Its unit, where no logical type names it" | one of `bp`, `percent`, `fraction`, `years`, `days`, `seconds`, `count`, `currency_unit`; only on `number` or `integer` (PC:336) |
| `x-atlas-enum` | a string property | "Its enum values with their descriptions, aliases and deprecations; `enum` holds the values themselves" | array, `minItems: 1`, of `{value (required, string), description, aliases (string array), deprecated (deprecation)}` with `additionalProperties: false`; values exactly equal `enum`'s string values (Q-PROF-10) |

- Q-PROF-2: "A `$ref`'s siblings are limited to the meta-data keywords and `x-atlas-key`, `x-atlas-aliases`, `x-atlas-reference` and `x-atlas-deprecated`."
- QUESTIONS.md Go-side row 11: an earlier Go reading required only `reason` and `since`. The references require all four, and the Go side must follow them (D22: the references are the independent check).
- Logical types (C:441-461):

  | Logical type | On | Specified by |
  |---|---|---|
  | `Bp` | `number` | Atlas: one basis point is 1/10,000 |
  | `Percent` | `number` | Atlas: 1.5 means 1.5% |
  | `Fraction` | `number` | Atlas: 0.015 means 1.5% |
  | `Years` | `number` | Atlas: a duration or tenor in years |
  | `Decimal(p,s)` | `string` | Atlas: an exact decimal of precision `p` and scale `s` |
  | `Currency` | `string` | ISO 4217 |
  | `Money` | `object(amount: Decimal(38,6), currency: Currency)` | Atlas |
  | `Country` | `string` | ISO 3166-1 alpha-2 |
  | `Isin` | `string` | ISO 6166 |
  | `Cusip` | `string` | CUSIP Global Services |
  | `Sedol` | `string` | London Stock Exchange |
  | `Figi` | `string` | OMG FIGI |
  | `Lei` | `string` | ISO 17442 |
  | `Tenor` | `string` | Atlas: `1M`, `6M`, `10Y` |
  | `PortfolioCode` | `string` | The firm's portfolio master |
  | `IndexCode` | `string` | Atlas's index catalogue (`index.list`) |
  | `Path` | `string` | A path in a file store a capability serves (a Workspace, SharePoint) |
  | `Principal` | `string` | An Entra object id |
  | `Uri` | `string` | RFC 3986 |

  "A domain MAY add its own; a type two domains use is defined once, here" (C:439). Open: "The first set of logical types. Recommended: as above, extended by domain owners" (C:496).
- The property written out (C:478-486):

  ```json
  "oas": {
    "type": ["number", "null"],
    "description": "Option-adjusted spread over the government curve.",
    "x-atlas-logical": "Bp",
    "x-atlas-aliases": ["oas_bp"],
    "x-atlas-deprecated": null
  }
  ```

- Open (C:496): "Renaming today's upper-case and unit-suffixed columns (`OAS_BP`, `MARKET_VALUE`) to this convention, by aliases, in `@v1`. Recommended: yes, as each profile is written."

### 1.7 `errors[]` (PS:530-562): see section 7.

### 1.8 `meaning` (C:140; PS:231-234)

PS gives no members. Each family's members come from §3.1.2, the readiness check (§3.1.4a item 5) and the vectors:

| Family | Members (vectors) | Required by readiness (C:343) |
|---|---|---|
| data | `grain` (string), `keys` (array of result property names), `as_of` (string), `source` (string) (V-data) | `grain`, `keys` (for a table), `as_of` (with the `AsOf` trait), `source` |
| resource | `name` (pattern string), `immutable` (array), `stored_version` (integer), `converges` (string) (V-res) | none |
| action | `effects {system, changes, environments, reversible, undo}`, `policy {approval, refused_unless_person}` (V-act) | `effects`, `policy` |
| event | `emitted`, `subject`, `ordering`, `retention` (ISO duration `P7D`), `history` (pinned-id `data.status@v1`) (V-evt) | `emitted`, `subject`, `ordering`, `retention`, `history` |

- `meaning` stays whole in the fingerprint (FP step 7), so rewording it gives a new fingerprint, which must then be classified.
- §3.3.4 (C:635): "The grain, the keys, the as-of rule, the source, or the method behind a column changed" is Breaking (`@v2`).
- **OVERRIDDEN BY REVIEW: R21.** "Business lineage and method are distinguished from the execution engine" (REVIEW R21). DR:148: "Changing valuation source may change meaning; moving the same computation from one engine to another need not." So `meaning.source` must name the business source or method, never the engine. Moving a computation between engines with the same result is not Breaking.
  - AMBIGUOUS: V-data's `"source": "Harbour's portfolio holdings view"` names a view, which looks physical. Readings: (a) it is the business lineage and fine; (b) it should name the owned dataset, not a view. CORE:80 says these facts "must come from the owner, not an inferred SQL view name".
- AMBIGUOUS: the review's capability card (CORE:64-78) and page contract (HB:110-123) need content with no profile member: timezone, update cadence, freshness evidence, correction policy, latency class, data handling and retention, reliability, partial-result semantics, and scope enforcement (HB:45: "the profile and guide need to say how Atlas enforces the caller's permitted scope"). Readings:
  - (a) put it in `meaning` (fingerprinted, so a rewording is a contract change);
  - (b) put it in `usage` and descriptions (prose, so it can be revised freely);
  - (c) split it: semantics in `meaning`, explanation in `usage`.
  CORE:62 says these "are documentation requirements, not a new schema language". No member names are given.

### 1.9 `compatibility` (C:141, C:577-586; PS:235-277)

- "Each schema in a profile declares the mode it is held to within its version" (C:579). Default modes (C:581-584):

  | Schema | Default mode | Why |
  |---|---|---|
  | A request (data, action, custom verb) | BACKWARD_TRANSITIVE | "The gateway must accept any request written under any earlier profile of the version: new optional parameters are allowed, new required ones never" |
  | A result, resource, plan, receipt or payload | FULL_TRANSITIVE | "Old readers read new data ... and new readers read old data" |

  "A capability may declare a stricter mode; never a looser one" (C:586).
- V-data: `{"request": "BACKWARD_TRANSITIVE", "result": "FULL_TRANSITIVE"}`. V-res declares only `resource`, although it also has seven request schemas.
  - AMBIGUOUS: whether every schema must appear in `compatibility`. Reading (from the "Default mode" column): an absent schema takes its default.
- AMBIGUOUS under R03: the gate now applies R03's fixed rule set (section 5). The declared mode no longer changes what the gate allows within a version. Readings:
  - (a) keep the member, informational only;
  - (b) the gate checks that each declared mode is at least the default, and otherwise ignores it.
  REVIEW does not say.

### 1.10 `usage` (C:142; PS:563-629)

| Member | Type (PS) | Required | Meaning |
|---|---|---|---|
| `when` | string | readiness | "When to use it. Prose." |
| `when_not` | string | no | "When not to. Prose." |
| `examples` | array of `{title, interaction, request}`, all three required | readiness (at least one valid) | `title` "What the example does"; `interaction` `^[a-z](-?[a-z0-9])*$` "The interaction it calls"; `request` object "A request valid against that interaction's request schema" |
| `related` | array of `capability-id` or `pinned-id` | no | "A related capability" |
| `cost` | string or object | no | "What a call costs; for Metered, its unit and price" |
| `skill` | string | no | "The agent's skill text, managed by administrators" |

- C:142 says examples are "each a `title` and a request"; PS also requires `interaction`, and every vector carries it.
- "per-client code is generated from them" (C:142). The review requires the same request rendered for the CLI, Python and Node (CORE:77; HB:122: "A minimal valid request, a realistic request and the same request in CLI, Python and Node").
- **OVERRIDDEN BY REVIEW: R01.** "A guide's identity includes the prose revision" (REVIEW R01). `usage` sits outside the fingerprint, so a guide cached by fingerprint alone would go stale (HB:547).
- AMBIGUOUS: whether `usage.related` must resolve to an existing, visible capability. V-data lists `index.constituents@v1`, which is not in the journey. Lint step 1a says "every reference resolvable" (C:767), which refers to `x-atlas-reference`. CORE:54: "Metadata and examples referring to other capabilities follow the same visibility rules." Readings: (a) `related` is unchecked prose; (b) `related` must resolve, and the guide filters it by the caller's visibility. If (b), the journey's holdings profile cannot keep `index.constituents@v1`.

### 1.11 `evolution` (C:143; PS:630-665)

| Member | Type (PS) | Meaning |
|---|---|---|
| `previous_profile` | string, `format: exact-id` | "The profile this one replaces within the version" |
| `lens` | array of `lens_op` | "The lens from previous_profile to this profile (§9.5.3)" |
| `previous_version` | string, `format: pinned-id` | "For a version after @v1: the version it replaces (§9.5.2 step 2)" (member name from Q-DOC-7) |
| `version_lens` | array of `lens_op` | "The lens from previous_version, where one can express it; only the gateway resolves across versions (Q-DOC-7)" |
| `changelog` | array of objects (free shape; V-data uses `{date, change}`) | "Why it changed. Prose: removed by the projection." |

- Lens operations (C:862-869; PS:666-976), each an object with `op`:

  | `op` | Members (PS) | Effect (C) |
  |---|---|---|
  | `rename` | `from`, `to` (property names) | "A field or parameter was renamed" |
  | `alias_value` | `field`, `from`, `to` | "An enum or reference value was renamed" |
  | `convert_unit` | `field`, `from`, `to` (from `Bp`, `Percent`, `Fraction`, `Years`, `bp`, `percent`, `fraction`, `years`, `days`, `seconds`, `count`, `currency_unit`, `UNKNOWN`) | "A field's unit changed between units of one dimension (`Bp` ↔ `Fraction`)" |
  | `promote` | `field`, `from`, `to` (from `integer`, `number`, `date`, `date-time`, `UNKNOWN`) | "A field's type was widened" |
  | `default` | `field`, `value` | "A field was added, with the value a reader that lacks it assumes" |
  | `drop` | `field` | "A field was removed (only after its deprecation's sunset)" |
  | `UNKNOWN` | none | "An operation a reader does not know reads as `UNKNOWN`, and the gateway resolves for that reader (§9.5.6)" |

  `field` is "A top-level property of the record (§9.5.1)". "A change a lens cannot express is not a lens: it is a new version" (C:871).
- **OVERRIDDEN BY REVIEW: R03, R04.** See section 5. REVIEW "What happens to what is built": `protocol/resolve` "becomes the experimental resolver ATLAS-CORE §7 allows. A client is obliged only to aliases, defaults, UNKNOWN and projection. Its vectors still pass, but no served profile may depend on `convert_unit`, `promote` or `drop` within a version."

---

## 2. Families, interactions, effects, results, deliveries, executions

### 2.1 The four families (CAP:75-86; SURF:9-14, 44-55; CORE:86-97)

| Family | What it is | Interactions (CAP:79-82) | Results travel (SURF:51-55) | Build |
|---|---|---|---|---|
| **Data** | "Reading what is" | `fetch`, `explain`, `export`, `ticket` (the broker) | "Streamed, handed off, exported" | **IN SCOPE** (every journey capability is data) |
| **Resources** | "Objects the platform owns and keeps" | `get`, `list`, `create`, `update`, `delete`, `watch`, and declared custom verbs | "Single answers; changes by watch" | DEFERRED for business resources ("Resources (after the journey)", REVIEW R14). `atlas.operation` only "where that journey needs" durable operations (CORE:238) |
| **Actions** | "Changing a system outside Atlas" | `plan`, `approve`, `run` (named by the action's own verb) | "An operation" | DEFERRED (no action in the journey; R06 and R08 are step 3) |
| **Events** | "Something that happened, for whoever subscribes" | `subscribe`, `publish`, each answered by a connect handover to Service Bus | "A subscription" | DEFERRED (REVIEW: "Handoff, events and Service Bus ... Deferred"; R15, R16, R25 are step 5) |
| Built-ins | `atlas.*` | fixed by the protocol | "Single answers" | `atlas.search`, `atlas.describe`, `atlas.access` and the rest are covered by other readers |

- "Every capability is on exactly one surface, or is a built-in" (SURF:44). "A surface's verbs and their meaning are part of the protocol; a capability's schemas are its own" (SURF:45).
- "What the protocol fixes (in every client, once): the surfaces and their verbs, result shapes, deliveries, the operation's lifecycle, the event envelope, the standard methods. What profiles evolve (in the gateway, any day): each capability's schemas, bindings, lenses, enums and references" (CAP:86).
- CORE:97: "Every capability need implement only the interactions it actually supports. No fake `explain`, universal filter language or meaningless CRUD method is required to make the interface look uniform."
- Journey interactions:
  - The curated data convention is "`fetch`, `explain`, `export`, all read; the fetched answer is a table; execution is gateway or handoff" (HB:198).
  - "All fetched tables are single except holdings and trades, which are chunked. `portfolio.search` has `fetch` only and gateway execution only" (HB:233).
  - `data.status@v1`: "Data, `fetch`, single table; source/date behavior requires clarification" (HB:361).
  - `export` (delivery `transfer`) is DEFERRED: direct downloads are step 5 (REVIEW step 5; R10).
  - AMBIGUOUS: whether `explain` is in the first build. CORE:238 lists "describe/guide, the call path, problems, data streaming and durable operations where that journey needs them", and does not list explain. CORE:97 permits omitting it. Readings: (a) declare `fetch` only for the journey (adding an interaction later is Safe, C:603); (b) declare and implement `explain` for holdings, as in V-data.

### 2.2 Effects (CAP:114, 136-141; CORE:95)

| Effect | Covers | Default policy in a harness |
|---|---|---|
| `read` | "Reading data, `get` and `list` on resources, subscribing" | "Run it; a metered call whose estimate passes the caller's threshold asks the person first (§6.2.6)" |
| `write` | "Changing a resource Atlas owns, reversibly" | "Confirm with the person" |
| `external` | "Changing a system outside Atlas, or publishing an event" | "Plan first, then confirm" |
| `irreversible` | "Anything that cannot be undone" | "Plan, then approval by someone other than the requester" |

- The effect is per interaction. "An interaction's effect is independent of its result shape. A read may cost money" (CORE:95). The journey has only `read`.
- "a change of effect is always a new version" (CAP:48). "Family, or an interaction's effect class changed: Breaking: a new version" (C:602).
- **OVERRIDDEN BY REVIEW: R06** for consent (`Atlas-Confirm` is the client's claim only). DEFERRED to step 3.

### 2.3 Result shapes (CAP:115, 120-126)

| Result | What it is | Generic behaviour |
|---|---|---|
| `table` | Rows of typed columns | Column selection, filters, ordering, columnar transport (Arrow) |
| `record` | One typed object, possibly nested | Field selection by path |
| `collection` | A paged list of records | Cursor paging, field selection |
| `content` | Bytes with a media type and a metadata record | Ranges, checksums, media type negotiation |
| `ticket` | A credential issued once, with the planned operation and its permit | Issued once, never cached, settled afterwards |

The journey uses `table` (and `record` for any `explain`). `ticket` is DEFERRED (broker; R10: "Client SQL tickets stay deferred").

### 2.4 Deliveries (CAP:116, 128-134)

| Delivery | What it is | Build |
|---|---|---|
| `single` | One answer | **IN SCOPE** |
| `chunked` | One answer, sent in chunks as it is produced | **IN SCOPE** (holdings; "Streaming is the default data path. A stream has an explicit completion marker; EOF alone is not success", CORE:206) |
| `subscription` | Events, for as long as the subscriber listens | DEFERRED |
| `operation` | An operation at once, then its progress and result (§4.1) | **IN SCOPE** where the journey needs one (CORE:238); it is also the fallback for a stream |
| `transfer` | "Bytes moved directly between the caller and storage, by byte range, resumable ... through a short-lived link the host issues" | DEFERRED |

Fallback (CAP:145): "Every interaction has a binding over plain HTTP and JSON that any client can use: a stream falls back to an operation polled for its pages; a transfer to an inline body under 1 MiB. ... The one exception is bulk data (`export`, `client` execution), whose fallback is `fetch` in pages with a row cap and a truncation flag." Lint (C:767): "every interaction with a deadline maximum and at least one binding with an HTTP fallback (except bulk exports, whose fallback is paged results with a row cap)". A binding may override `delivery` for such a fallback (BS `delivery`; Q-DOC-4).
- AMBIGUOUS: CAP:145 says events fall back to polling `meaning.history`. **OVERRIDDEN BY REVIEW: R16**: "Equivalence is claimed only with cursor-addressable retained history". This bears on `data.status@v1`. HB:544: "A latest snapshot is not a replayable landing history. Define whether fallback means latest-state reconciliation or event replay; do not promise both." Since events are deferred, the first build's `data.status@v1` need only be a freshness table, but its profile must not claim to be `data.landed`'s replayable history unless it is one.

### 2.5 Executions (CAP:117, 143; TERMS:29-30)

| Execution | Who does the work | Build |
|---|---|---|
| `gateway` (run) | "the gateway does the work and streams the result" | **IN SCOPE** |
| `handoff` | "the gateway does the work and the client downloads the result directly from the source's result storage" | DEFERRED ("Direct Snowflake chunks and Service Bus remain separately gated proofs", CORE:241; R10) |
| `client` (the broker) | "the client runs the planned statement from a ticket; deferred until its credential meets the credential rule (§6.3)" | DEFERRED (spec and R10) |

- "All MUST return the same rows in the same schema for the same request" (CAP:143). **ATLAS-CAP-004** makes that a conformance test (CAP:55).
- Evolution makes deferring safe: "An execution removed (`handoff`, `client`): Safe: the gateway serves the call by `gateway` execution instead" (C:606). Adding `handoff` later is "Safe" (C:636). A trait removed is Breaking "except `Handoff` and `Brokerable`, which follow their executions" (C:598). So a first-build holdings profile may declare `execution: ["gateway"]` without the `Handoff` trait, and both can be added later without a new version.

---

## 3. Traits (CAP:147-167; PS:296-313; C:598, C:830)

General rules:
- "A trait is a behaviour every client and test handles the same way in every domain. A profile MAY mark a trait **critical**: a client that does not understand it MUST NOT call that capability, and is told so" (CAP:149).
- **ATLAS-CAP-005** "A client that does not understand a trait marked critical is refused that capability with a problem naming the trait and the client version that understands it" (CAP:56). The code is `critical_trait_unsupported`, 422, `retryable: false` (PROB registry). Scenario `scenarios/CORE-CRIT-001.json` exists. **IN SCOPE** as protocol behaviour.
- Tolerant reader (C:830), for every reader, gateway or client: "1. ignore fields, headers, event types and traits it does not know, except a trait marked critical; 2. map enum values it does not know to `UNKNOWN`; 3. refuse a capability with a critical trait it does not understand, with a problem, rather than misread it." **IN SCOPE.**
- The client declares the lowercased trait names it understands in `Atlas-Client-Features` (PS:306; C:759: "`Atlas-Client-Features`: lens operations, shapes, deliveries, traits").
- Evolution: a non-critical trait added is Safe; one added and marked critical is Dangerous; a trait removed is Breaking, except `Handoff` and `Brokerable` (C:596-598). "A filter or a `Projectable` / `Filterable` trait added: Safe" (C:625).
- Open: "The trait list as the first set; a new trait is a protocol minor version. Recommended: yes" (CAP:200). A protocol minor version "adds a family verb, a shape, a delivery, a trait or a lens operation" (C:729).

Each trait. The "Spec text" column quotes CAP:153-167. "Obliges" lists only what my sources say; anything else is marked.

| Trait | Spec text (CAP) | Profile must | Gateway must | Client must | Build |
|---|---|---|---|---|---|
| `Projectable` | "Accepts `columns` (or field paths) and returns only those" | Have a request member `columns`. V-data: `{"type": ["array","null"], "items": {"type":"string","minLength":1}, "uniqueItems": true, "default": null}`, "every column when null (Projectable)" | Return only the named columns. Names are "as the reader names them" (V-data), so an alias such as `mv` resolves to `market_value` (C:646-648) | Name the columns it needs (CAP:36). Under R03/R04, projection is one of the four client obligations (REVIEW) | **IN SCOPE** (holdings) |
| `Filterable` | "Accepts the filters its profile declares, pushed down to its source" | Declare each filter as a request member (V-data: `asset_class`) | Push the filter down to the source | Send only the declared filters (closed request schema) | **IN SCOPE** (holdings) |
| `Paged` | "Pages by an opaque cursor" | V-res `list_request`: `cursor` (string, `minLength: 1`), `page_size` (integer 1..200, default 50, `x-atlas-unit: count`) | Issue opaque cursors | Treat the cursor as opaque | AMBIGUOUS for the journey: whether `portfolio.list`, `portfolio.search` or `data.status` are `Paged`. My sources do not say (see the catalogue reader) |
| `AsOf` | "Accepts `as_of`, answering as the data stood then; and `start_date` / `end_date` for a range" | Have `as_of` in the request; `meaning.as_of` is required by readiness item 5 | Answer as the data stood on `as_of` | — | **IN SCOPE** (holdings, and probably holdings-dates) |
| `Keyed` | "Declares its keys and its references to other capabilities" | `meaning.keys`; `x-atlas-key` on the key properties; `x-atlas-reference` on references | Every reference resolvable (lint) | Use references "to complete and check input" (C:470) | **IN SCOPE** (holdings) |
| `Searchable` | "Accepts free-text search" | — (no vector) | — | — | AMBIGUOUS: probably `portfolio.search@v1` ("Resolve loose portfolio names → ranked codes and reasons", HB:238), but my sources do not assign it |
| `Ranged` | "Serves byte ranges of content" | — | — | — | DEFERRED (content/transfer) |
| `Handoff` | "Offers `handoff` execution" | `handoff` in `execution` | Hand off | — | DEFERRED (R10; CORE:241) |
| `Brokerable` | "Offers `client` execution: plan, ticket, run, settle" | — | — | — | DEFERRED |
| `DryRunnable` | "Returns a plan without changing anything" | (V-act) | — | — | DEFERRED (actions) |
| `NeedsApproval` | "Runs only after someone other than the requester approves, where its policy says" | `meaning.policy` (V-act) | — | — | DEFERRED (actions; R06) |
| `Licensed` | "Returns licensed data: never persisted except a job's answer for its own caller; caches stay in memory" | Declare the trait | Never persist the result except "a job's answer for its own caller"; caches stay in memory | Same rule for client caches | **IN SCOPE** (holdings). R18: "Results are private to their caller; any reuse is keyed by authorisation, environment and data revision, and reauthorised". CORE:230: "A protocol flag cannot prevent a caller from saving data it already received". The handbook leaves the persistence rules to the owner (HB:168: "the `Licensed` sample trait's persistence rules") |
| `Ordered` | "Delivers events in order per subject" | (V-evt) | — | — | DEFERRED (events) |
| `DynamicSchema` | "Its result's columns are chosen by the caller's own query (`snowflake.query`, `azure.resource-graph`): no fixed result schema, each answer carries its own, no resolution, always `upstream` (§9.3)" | — | — | — | DEFERRED |
| `Metered` | "Costs money upstream per call; its profile states the unit and the price" | The unit and price in `usage.cost` (PS: "for Metered, its unit and price") | Budgets: R07, step 3 | — | DEFERRED (no metered capability in the journey; step 3: "Before metered or changing work") |

- AMBIGUOUS (AsOf range): CAP:156 includes "`start_date` / `end_date` for a range", but V-data's holdings request has only `as_of`. HB:168 lists "the history/range request described by §7 but omitted from the sample schema" as an owner question. Readings:
  - (a) the range is optional within `AsOf`, so a profile with `as_of` alone conforms;
  - (b) `AsOf` obliges both, so V-data is incomplete.
  REVIEW "Left to owners" leaves holdings questions to the owner, with the fixture fixing a stated choice.
- AMBIGUOUS (null `as_of`): V-data: "The valuation date; the latest when null." `meaning.as_of`: "the portfolio's last valuation on or before as_of". HB:159 says "the meaning of null and multi-portfolio date alignment still need explicit prose". This is an owner question (REVIEW "Left to owners": "null `as_of`"). CORE:200: "A retry of the same submitted request uses the original normalization snapshot; it must not choose a new 'latest' date". HB:144: "'Latest' must be reported as an effective source date".
- The spec gives no further obligations per trait. For `Projectable` with an unknown column name, `Filterable` operators, and the `AsOf` range, the sources say nothing more. Do not invent them.

---

## 4. The agent-readiness check (§3.1.4a, C:335-343)

"A profile is ready for agents (ATLAS-VISION-002) when every one of these holds; the gate and registration run it, and a profile that fails is served only as `preview`" (C:337). **IN SCOPE**: "The four journey profiles pass the gate and the readiness check" (REVIEW step 2).

| # | Spec text (C:339-343, exact) | Reference reading (PC:400-475) |
|---|---|---|
| 1 | "`identity.summary` is one sentence of at most 200 characters." | A non-empty string, `len ≤ 200`, ending in `.`, `!` or `?`, with no `". "`, `"! "` or `"? "` before the last character (PC:400-404). Failure pointer `/identity/summary`, rule `summary` |
| 2 | "Every property, at every depth of every schema, has a `description`; every `number` or `integer` property has `x-atlas-logical` or `x-atlas-unit` (`count` where it is a plain count)." | Walks each schema's `properties` recursively, through `items`, object `additionalProperties`, `prefixItems`, `anyOf`, `oneOf` and `$defs` (PC:407-427). Every entry under `properties` needs a non-empty `description` (rule `description`). If its base type is `number` or `integer`, it needs `x-atlas-logical` or `x-atlas-unit` (rule `x-atlas-unit`). Any unit is accepted; `count` is not enforced for counts |
| 3 | "`usage.when` is set, and `usage.examples` has at least one example whose `request` validates against the request schema of the interaction it names." | `usage.when` is a non-empty string. **Every** example that fails (its interaction has no `takes`, the schema is missing, or the request is invalid) is reported at `/usage/examples/<i>`, and `/usage/examples` is reported when none is valid (PC:441-452) |
| 4 | "Every member of `errors` has a `description` and a `next`." | Both non-empty strings (PC:453-456). An empty `errors` passes (V-evt) |
| 5 | "A data capability's `meaning` has `grain`, `keys` (for a table), `as_of` (where it has the `AsOf` trait) and `source`; an event's has `emitted`, `subject`, `ordering`, `retention` and `history`; an action's has `effects` and `policy`." | Presence only (`k not in meaning`). For data: `keys` is needed when any interaction has `result: "table"`; `as_of` when the trait set contains `AsOf`. Resources need nothing (PC:457-474) |

Output: a sorted list of `(pointer, rule)`; an empty list means ready (PC:430-431). Every profile vector expects `"agent_ready": true`.

- ATLAS-VISION-002 (00-vision.md:69, quoted by grep only): "Every capability's profile passes the agent-readiness check: a summary, a meaning and unit for every field, at least one example, and a next step for every problem it declares."
- AMBIGUOUS (item 3): the spec says "at least one example" validates. The reference check also fails a profile that has one valid and one invalid example (PC:449-450). Readings:
  - (a) spec-literal: one valid example suffices;
  - (b) reference: every example must be valid.
  Recommendation for the build: (b), because the Go side must agree with the references (D22) and HB:591 says "Validate the examples against its request schema". Record it as a reading.
- AMBIGUOUS (item 2, scope of "property"): the reference walk checks only members of `properties`, so:
  - the root schema's own `description` is not required (the vectors still give one);
  - an `items` schema of type `number` (an array of numbers) is not checked for a unit;
  - a map's `additionalProperties` value is checked only for its own `properties`.
  Readings: (a) "property" means a member of `properties`, as the reference has it; (b) every subschema. Neither source settles it.
- AMBIGUOUS ("one sentence"): the reference heuristic counts terminal punctuation. A summary such as "Positions in e.g. a portfolio." would fail it. There is no other definition.
- AMBIGUOUS ("served only as `preview`"):
  - (a) the gateway serves the profile with `properties.lifecycle` rewritten to `preview`. But `lifecycle` is in the projection, so the served fingerprint would differ from the file's.
  - (b) the profile is unchanged, and the catalogue treats it as preview. §2.6's working view excludes preview unless asked (covered by the discovery reader).
  - (c) the gate fails the PR outright, so a non-ready profile never ships.
  V-act is `lifecycle: "preview"` and agent-ready, so preview is also an authored state. REVIEW step 2 requires the four journey profiles to pass, so reading (c) is enough for the first build. Registration still needs a defined behaviour for a non-ready profile.
- The review's page contract asks for more than this check: "The agent-readiness gate in §3.1.4a already requires much of this" (HB:125). The extra content goes in prose (see 1.8, AMBIGUOUS). "An owner-complete profile is therefore the acceptance criterion" (HB:125).
- The lint (§3.3.12 step 1a, section 5.6) is a separate check from readiness. A profile must pass both.

---

## 5. Evolution (§3.3), as narrowed by R03

### 5.1 The four numbers (C:557-562)

| Number | Names | Changes when | Who sees it |
|---|---|---|---|
| **Fingerprint** (`portfolio.holdings@v1#3f9a1c0b7d2e4a65`) | One exact contract | Anything in the profile's canonical form changes | Clients, in `Atlas-Profile`; locks |
| **Capability version** (`@v1`) | One meaning | The meaning changes | Callers choose it; at most two are live |
| **Protocol version** (`/v1`; minors within it) | The rules the gateway and clients implement | The protocol changes | Clients declare it in `Atlas-Client` |
| **Client version** (`atlas 1.4.0`) | One build of a client | The client changes | Nobody else |

"Only a protocol change ever needs a client release; the gate measures it" (C:513). **ATLAS-VERSION-004** "The number of client releases caused by contract changes is reported for every release, and is zero" (C:545). Acceptance item 4: "A capability added after the clients were built works without releasing any of them" (REVIEW). **IN SCOPE.**

### 5.2 Classes (C:567-573)

| Class | Meaning | Within a version | Mechanism |
|---|---|---|---|
| Safe | "No correct reader or writer can notice" | Allowed | A new fingerprint |
| Dangerous | "Correct readers keep working, by the tolerant-reader rules and lenses, but behaviour can change" | Allowed, announced in the changelog | A new fingerprint, plus aliases or a lens |
| Breaking | "Some correct reader or writer would fail or misread" | Refused by the gate | A new version |

"Every change to a profile is classified against the last published profile of the same version" (C:567).

### 5.3 R03, the governing text

- **OVERRIDDEN BY REVIEW: R03.** The spec says "Full transitive compatibility within a version, by lenses". Pass 2: "Within a version: optional request fields; result fields only with a defined missing value; aliases for exact renames. A removal, a unit change, a type change, a field made non-null in a result, or a changed default is a new major version" (REVIEW R03, step 2 (gate)). Spec rule 1, "Within `@vN`, profiles are compatible fully and transitively ... by §3.4" (C:538), no longer holds as written.
- CORE:144-152:
  - "Keep the meaning of an existing version: grain, units, defaults, effects and time semantics."
  - "Add optional request fields without changing old defaults."
  - "Add result fields only when old readers ignore them and newer readers have a defined missing-value behavior for retained older data."
  - "Keep old accepted names as aliases when a rename is necessary."
  - "Use a new major version for field removal, incompatible type changes, unit changes or changed business meaning unless a specific, tested compatibility mechanism proves the old contract remains valid."
  - "Do not promise arbitrary reversible lenses. ... Existing experimental lenses can remain experiments without becoming universal client obligations."
- R03 acceptance (DR:56): "Compatibility tests contain those counterexamples and reject the change or retain a real old-version implementation. The gate never classifies an unrepresentable old value as safe." The counterexamples (DR:52):
  1. "A required `market_value` disappears; no default can recover its actual old value."
  2. "An integer becomes a floating-point field and the new producer sends `1.5`; an old integer reader cannot preserve that value."
  3. "A newer non-null reader encounters historical nulls."
- **OVERRIDDEN BY REVIEW: R04.** "Aliases carry their schema scope. A request is validated under its own contract before any transformation, and unknown request fields are refused, never dropped" (REVIEW R04). DR:62: "If lenses remain, name their target schema/interaction and define projection/mask/filter translation. Validate the original request under its declared contract before transformation, and the transformed request afterward." Acceptance (DR:64): "A misspelled field is rejected, and two unrelated fields with the same spelling in request and result are never both transformed accidentally." DR:60 notes that §3.3's rename example needs `columns: ["mv"]` translated as a value, not a top-level member renamed.
- Client obligations within a version: "aliases, defaults, UNKNOWN and projection" (REVIEW, "What happens to what is built").

### 5.4 Common changes (§3.3.3, C:588-611), with R03 applied

| Change | Spec class | Under R03 (first build) |
|---|---|---|
| Prose (titles, `doc`, usage, examples, skill text) | "Not a change to the contract: a new revision, the same fingerprint" | Same. But R01: the document revision and the guide identity move with it |
| A binding added | Safe | Safe (R02: routing is current metadata) |
| A binding moved (address, server, transport), the old one served through a migration window | Safe | Safe |
| A binding removed after its window | Safe ("every client follows served bindings") | Safe (R02 requires clients to refresh routing) |
| A trait added that is not critical | Safe | Safe |
| A trait added and marked critical | Dangerous ("clients without it are refused that capability, and told why") | Dangerous (not addressed by R03) |
| A trait removed | Breaking, except `Handoff` and `Brokerable` | Same |
| Lifecycle `preview` → `stable`; `stable` → `deprecated` with a sunset | Safe; dangerous | Same |
| Visibility `confidential` → `listed` | Safe | Same |
| Visibility `listed` → `confidential` | Dangerous | Same |
| Family, or an interaction's effect class changed | Breaking: a new version | Same |
| An interaction added | Safe | Same (how `holdings-dates` style growth stays release-free) |
| An interaction removed | Breaking | Same |
| An execution or delivery added to an interaction | Safe: "clients ignore what they do not support" | Same |
| An execution removed (`handoff`, `client`) | Safe: "the gateway serves the call by `gateway` execution instead" | Same |
| A delivery removed | "Safe only after its deprecation's sunset; otherwise breaking" | AMBIGUOUS under R03 ("A removal ... is a new major version"). Readings: (a) R03 covers fields only, so this row stands; (b) any removal is a new major. R03's text lists field-level items |
| An error code added | Dangerous: "clients treat an unknown code by its HTTP status and `retryable`" | Same |
| A deadline maximum lowered | Dangerous | Same |

Governance is not classified (C:611): "Granting or revoking access, tightening or loosening a limit, a budget or an approval policy changes the governance revision ..., never the fingerprint, and is never classified here. Tightening may happen at any time. Loosening a limit or an approval policy is reviewed by the capability's domain owner and recorded in the audit trail."

### 5.5 Data changes (§3.3.4, C:613-637), with R03 applied. IN SCOPE (the gate)

"**The contract:** the request schema, the result schema, the **meaning** (grain, keys, as-of rule, units, source and method), the execution options and bindings. **Not the contract:** the statement the gateway plans, which may change freely as long as the result for every request is unchanged" (C:615).

| Part | Change | Spec class / mechanism | Under R03 |
|---|---|---|---|
| Request | An optional parameter added (nullable, or with a default) | Safe | Safe ("optional request fields"; "without changing old defaults") |
| Request | A required parameter added | Breaking, `@v2` | Breaking |
| Request | A parameter renamed | Dangerous; "The new profile lists the old name in `aliases`; old requests resolve" | Allowed as an exact-rename alias, with schema scope (R04) |
| Request | Accepted values widened ("a constraint relaxed, a type promoted, an enum value added") | Safe | AMBIGUOUS for "a type promoted": REVIEW R03 says "a type change ... is a new major version"; CORE:150 says "incompatible type changes". Readings: (a) request widening is backward-compatible and stays Safe; (b) any type change is a new major. Constraint relaxed and enum value added are untouched by R03 |
| Request | Accepted values narrowed ("a constraint tightened, an enum value removed") | "Breaking, unless every removed value is an alias of a kept one"; `alias_value` lens, else `@v2` | Breaking. The exception survives only as an exact-rename alias (`x-atlas-enum` entry `aliases`). AMBIGUOUS whether the `alias_value` lens op itself remains allowed: REVIEW forbids only `convert_unit`, `promote` and `drop` |
| Request | A parameter's default changed | Breaking, `@v2` | Breaking ("a changed default is a new major version") |
| Request | A filter or a `Projectable` / `Filterable` trait added | Safe | Safe (an optional request field) |
| Result | A column added | "Safe: callers name their columns, and readers ignore what they did not ask for" | **OVERRIDDEN BY REVIEW: R03**: allowed "only with a defined missing value" for retained older data. AMBIGUOUS what counts as defined: (a) the column is nullable (missing = null); (b) a `default` (keyword or `default` lens; `default` is a client obligation per REVIEW); (c) either. A new required non-null column with no default fails (counterexample 3) |
| Result | A column renamed | Dangerous; `aliases` and a `rename` lens | Allowed as an exact-rename alias ("aliases for exact renames"). The `rename` lens is not on the forbidden list |
| Result | A column's unit changed within one dimension | Dangerous; `convert_unit` lens | **OVERRIDDEN BY REVIEW: R03**: new major version ("no served profile may depend on `convert_unit`") |
| Result | A column's type promoted (`int` → `long` → `double`) | Dangerous; resolution promotes | **OVERRIDDEN BY REVIEW: R03**: new major (counterexample 2; `promote` forbidden) |
| Result | A column made non-null | Safe | **OVERRIDDEN BY REVIEW: R03**: new major ("a field made non-null in a result"; counterexample 3) |
| Result | A column made nullable | Breaking, `@v2` | Breaking |
| Result | A column deprecated, then dropped after its sunset | "Dangerous, then safe"; `deprecated`, then a `drop` lens | Deprecating stays Dangerous. **OVERRIDDEN BY REVIEW: R03**: the drop is a new major ("A removal"; `drop` forbidden; counterexample 1) |
| Result | A column dropped before its sunset; a type changed other than by promotion | Breaking, `@v2` | Breaking |
| Result | An enum value added to a column | Dangerous; readers map it to `UNKNOWN` | Dangerous (UNKNOWN is a client obligation). C:586: "allowed under FULL_TRANSITIVE only because every enum carries UNKNOWN" |
| Meaning | The grain, the keys, the as-of rule, the source, or the method behind a column changed | Breaking, `@v2` | Breaking; R21 narrows "source" to business lineage and method, not the engine |
| Execution | `handoff` (or `client`) execution added | Safe | Safe |
| Execution | `handoff` (or `client`) execution withdrawn | "Safe: callers fall back to the gateway" | Safe |

- AMBIGUOUS (deprecated columns with no drop): if a column can never be dropped within `@v1`, the meaning of its `x-atlas-deprecated.sunset` is unclear. V-data's `duration` has `"sunset": "2027-03-01"`. Readings: (a) the sunset is the earliest date `@v2` may omit it, and `@v1` keeps serving it until `@v1` retires; (b) the deprecation is advisory, with `Deprecation`/`Sunset` headers only.

The worked examples (ATLAS-VERSION-006: "Each worked example in this section is a conformance test", C:547):

- Rename within `@v1` (C:639-656), still allowed under R03 as an exact alias:

  ```text
  → POST /v1/data/portfolio.holdings@v1:fetch
    Atlas-Client: name="fiatlas", version="2.3.0", protocol="atlas/1.0"
    Atlas-Client-Features: levels=(core streaming columnar), lens=(rename alias_value convert_unit promote default drop)
    Atlas-Profile-Expect: "portfolio.holdings@v1#A"
    { "portfolios": ["147HYC"], "columns": ["portfolio", "mv"] }

    gateway: resolves the request #A → #C: column "mv" is an alias of "market_value"; plans with market_value

  ← 200  Content-Type: application/vnd.apache.arrow.stream
    Atlas-Profile: portfolio.holdings@v1#C
    Deprecation: @1795046400       (the name "mv": in the profile's deprecation of the alias)
    (Arrow schema: portfolio, market_value)

    client: has #A locked; fetches #C once; resolves #C → #A: renames market_value back to mv; warns that "mv" is deprecated
  ```

  - AMBIGUOUS: the `Deprecation` here is "in the profile's deprecation of the alias", but `x-atlas-aliases` is a plain string array (ASM), with nowhere to put a per-alias deprecation. Readings: (a) using any alias triggers `Deprecation` from the target property's `x-atlas-deprecated`; (b) a per-alias deprecation is missing from the vocabulary. Also, under R11 the response may be JSON rather than Arrow.
  - Under R03/R04, the client-side "renames market_value back to mv" is an alias obligation. The `Atlas-Client-Features` line still advertises `convert_unit promote drop`, which served profiles may no longer depend on.
- Meaning change, so `@v2` (C:658): "Holdings move from end-of-day to intraday: the grain changes, so the owner publishes `portfolio.holdings@v2` ... `@v1` is deprecated with a sunset at least 90 days on, and keeps answering with its own meaning: the gateway either keeps `@v1`'s own plan, or computes `@v1` from `@v2`'s data where a lens can express it. ... At the sunset `@v1` is retired and answers `retired`, naming `@v2`." R03 prefers "retain a real old-version implementation" (DR:56). DEFERRED (no `@v2` in the first build).
- Resources (§3.3.5, C:660-687), actions (§3.3.6, C:689-706) and events (§3.3.7, C:708-725): DEFERRED with their families. Under R03 the same narrowing applies (a field "dropped after its sunset" is a new major). Two facts carry over:
  - "A plan records the fingerprint it was made under" (C:703).
  - Event type `com.jhg.fiquant.<id>.v<N>` (C:710); `dataschema` `urn:atlas:profile:<pinned-id>:<fingerprint>` (FP §9.4.3).

### 5.6 The gate and the registration check (§3.3.12, C:761-774). IN SCOPE

"The same check runs in two places: **the gate**, on every pull request, against the last published profiles; and **the gateway itself**, when a service registers (at start, or a remote service host on connecting), against the profiles it already serves. The gateway refuses to load a capability whose profile fails, and reports why in its readiness" (C:763).

For every capability whose profile changes:

1. "Validate the profile against the profile schema and the type system."
   - 1a. **Lint it:**
     - request schemas closed;
     - every enum with `UNKNOWN`;
     - every reference resolvable;
     - errors declared;
     - every interaction with a deadline maximum and at least one binding with an HTTP fallback (except bulk exports, whose fallback is paged results with a row cap);
     - no `$ref` leaving the profile;
     - within the depth and size limits;
     - no member that names a place on the caller's machine (ATLAS-CLIENTS-006);
     - for a `resource` profile, every record schema declaring the envelope's `name`, `metadata` and, if it converges, `status` exactly as `resource.schema.json` (§9.3 9.3.4).
2. "Find the last published profile of the same `id@vN`. If there is none, the change is a new capability or version: check that a new version names its previous version and, where expressible, the lens from it."
3. "Classify every difference by 3.3.3 to 3.3.7." (Under R03: the tables in 5.4 and 5.5.)
4. "Fail on any breaking difference within the version."
5. "Require a changelog entry for any dangerous difference."
6. "Check that every alias, lens and deprecation is consistent: an alias names a field that existed; a lens maps the previous profile onto this one; a sunset is at least 90 days on."
7. "Regenerate the catalogue snapshot and the published descriptions (OpenAPI, AsyncAPI), and fail if they differ from what is committed."
8. "**Callable by every supported client:** check the profile needs nothing beyond the gateway's `minimum_protocol` (a delivery, encoding, lens operation, handover kind, connector or trait marked critical added later), or that every interaction has a binding or fallback a client at `minimum_protocol` can use. Otherwise refuse it: it would need a client release, which only a protocol change may cause (ATLAS-CORE-009)."

The requirements:
- **ATLAS-VERSION-001** "The gate classifies every profile change by 3.3.3 to 3.3.7 and fails a breaking change within a version" (C:542).
- **ATLAS-VERSION-007** "A service whose profile breaks a profile the gateway already serves is refused at registration, with the field and the rule named (for example `request property 'pool' became required: declare @v2`)" (C:548).
- **ATLAS-VERSION-009** "A new capability that a client at the gateway's `minimum_protocol` could not call is refused at registration, naming what it needs" (C:550). `portfolio.holdings-dates@v1`, added after the clients are built, takes the step-2 "new capability" path and must pass step 8.

Notes:
- The reference lint (PC:487-518) implements: each schema in the Atlas Schema profile by its role; `takes`/`returns` naming existing schemas; `deadline.max`; at least one `https` binding; and the resource envelope check. The other step-1a items (references resolvable, caller-machine paths) are not in that function.
- AMBIGUOUS (step 7): whether OpenAPI generation is in the first build. AsyncAPI follows events, which are DEFERRED. The catalogue snapshot is IN SCOPE (PROFILE-003; R23).
- AMBIGUOUS (step 2 and V-data's evolution): V-data has `previous_profile: "portfolio.holdings@v1#2b71c0d4e9a35f18"` and a `rename` lens, but no published profile with that fingerprint exists in the references. If the journey's holdings profile copies V-data's `evolution`, step 2 "Find the last published profile" has nothing to compare with. Readings: (a) the journey's first holdings profile carries no `evolution` (a new capability); (b) the fixture also publishes `#2b71c0d4e9a35f18`, and the rename becomes the R03 alias acceptance test.
- AMBIGUOUS (step 6, "a lens maps the previous profile onto this one"): under R03 most lens ops are forbidden within a version, so this narrows to the allowed ops (alias, `default`, and possibly `rename` / `alias_value`).
- The registration problem codes and their form (for example how "refused, with the field and the rule named" is reported in readiness) belong to the gateway reader. Not specified here beyond C:548 and C:763.

### 5.7 Deprecation and sunset (§3.3.9, C:731-741)

- "Anything may be deprecated: a capability version, a parameter, a column, a field, an enum value, an event type, a binding. A deprecation carries a **reason**, a **replacement** and a **sunset** (at least 90 days after it is announced, unless the owner and the callers who use it agree sooner)."
  1. "The profile's `deprecated` says so, and the changelog says why."
  2. "Every call that uses it returns `Deprecation` (when it was deprecated) and `Sunset` (when it ends); every client shows a warning; `atlas.describe` and `atlas.access` show it."
  3. "Usage is tracked per element (from the columns callers select, the parameters they send, the subscriptions they hold) and per caller."
  4. "The owners of locks that hold it are told when it is deprecated, 30 days before its sunset, and at its sunset."
  5. "At its sunset it is dropped (a lens `drop`, or the version retired: `retired`, naming the replacement)." Under R03, a field drop means a new major (see 5.5); version retirement stays.
- "At most **two versions** of a capability are live, the current and the previous; publishing a third retires the oldest's sunset early only with the owner's agreement, otherwise the gate refuses it" (C:741). **ATLAS-VERSION-003** (C:544). DEFERRED in practice (the journey has only `@v1`).
- **ATLAS-VERSION-002** "A deprecated element returns `Deprecation` and `Sunset` on every call that uses it, and every client surfaces a warning" (C:543). **IN SCOPE** if a journey profile carries a deprecated element. V-data's `duration` column is one: `x-atlas-deprecated {reason: "Use risk.duration@v1, which states its curve.", since: "2026-09-01", sunset: "2027-03-01", replaced_by: null}`. Header example: `Deprecation: @1795046400` (C:652). The warning code is `deprecated` (Q-RUN-3). Scenario `scenarios/CORE-DEPR-001.json` exists.
- Where deprecation lives:
  - a capability version: `properties.lifecycle: "deprecated"` plus `properties.deprecated` (date), `sunset` (date), `replaced_by` (PS; Q-DOC-5);
  - a property: `x-atlas-deprecated`;
  - an enum value: `x-atlas-enum[].deprecated`.
- `retired` is 410, `retryable: false` (PROB registry: "The version was retired at its sunset").
- AMBIGUOUS: the 90-day minimum. §3.3.9 states "at least 90 days", but C:780 lists it as "**Open:** The minimum sunset. Recommended: 90 days", and REVIEW "Left to owners" keeps "The minimum sunset" open. Readings: (a) enforce 90 days in gate step 6 as the spec text says; (b) make it a configured value with 90 as the default. The fixture must state the choice.

### 5.8 Changelog

- The changelog lives in `evolution.changelog`, an array of objects; V-data uses `{"date": "2026-10-01", "change": "Renamed mv to market_value."}`. PS fixes no item shape.
- It is prose: removed by the projection (FP step 5).
- It is required "for any dangerous difference" (gate step 5). "the changelog says why" a deprecation was made (C:735).
- AMBIGUOUS: the item shape. V-data's `{date, change}` is the only example. Readings: (a) adopt `{date, change}` as the house shape; (b) leave it free-form, as PS does.

### 5.9 `previous_profile` and `previous_version`

- `previous_profile` (`exact-id`): "The profile this one replaces within the version" (PS). Its `lens` is "The lens from previous_profile to this profile" (PS).
- `previous_version` (`pinned-id`) and `version_lens`: for `@v2` and later (Q-DOC-7). Gate step 2: "a new version names its previous version and, where expressible, the lens from it." DEFERRED (no `@v2`).
- Resolution walks "the chain of profiles between writer and reader" (C:826). So `previous_profile` links form a chain per `id@vN`.
- **ATLAS-RESOLVE-003** "A request written under an earlier profile of `id@vN`, sent with its fingerprint, is accepted and means what it meant then" (C:841). **IN SCOPE** for aliases and defaults. The gateway resolves requests named in `Atlas-Profile-Expect`; the client resolves results named in `Atlas-Profile` (C:848-852).

### 5.10 Locks and negotiation (C:743-759), as they bear on contracts

- Lock example (C:747-753):

  ```toml
  # atlas.lock: written by `atlas lock`; read by every client and by the build
  [capabilities]
  "portfolio.holdings" = { version = 1, fingerprint = "3f9a1c0b7d2e4a65" }
  "curve.government-zero" = { version = 1, fingerprint = "0e02d4c1aa7b3f10" }
  "data.landed" = { version = 1, fingerprint = "51c7ee8b2d6f9a43" }
  ```

  "`atlas lock upgrade [id]` moves an entry to the newest fingerprint (or version), and checks that every call the application makes still resolves. A person exploring at a terminal MAY use a bare id, which resolves to the current stable version; programs MUST pin" (C:755).
- **OVERRIDDEN BY REVIEW: R02** (see 1.5). CORE:162: "A lock pins capability versions and reader contracts, includes the contracts needed to start, and records their checksums. ... It does not freeze permissions, backend addresses, source data or live availability."
- AMBIGUOUS: the lock's format and place, "which the spec still has open" (REVIEW "Left to owners"; C:780).
- Negotiation (C:759): "The gateway serves a client the old way where it lacks a feature: it applies the lenses itself before answering, and returns a shape the client does not know as plain JSON or bytes. A client below the protocol's minimum is answered `upgrade_required` (426) with how to upgrade; the CLI updates itself." **ATLAS-VERSION-005** "A client that declares no features is served correctly by the gateway (a conformance test)" (C:546). **IN SCOPE.**
- **ATLAS-VERSION-010** (`make evolution`, C:551): release N−1 reads and updates N's stored records. Whether it is in the first build depends on which stores exist; the gateway reader decides.

---

## 6. §3.1.3's example holdings profile, quoted in full (C:154-208)

C:150 introduces it: "These examples are abbreviated for reading (traits are shown by name, schemas shortened, `…` where parts are left out). The normative structure is `profile.schema.json` (§9.3), and complete, valid profiles are the test vectors in `references/vectors/profiles/`."

```json
{
  "identity": {"id": "portfolio.holdings", "version": 1, "title": "Portfolio holdings",
    "summary": "Positions in one or more portfolios on a date.", "domain": "portfolio", "owner": "QFI"},
  "properties": {"family": "data", "stability": "official", "lifecycle": "stable",
    "visibility": "listed", "traits": ["Projectable", "Filterable", "AsOf", "Keyed", "Handoff", "Licensed"]},
  "interactions": {
    "fetch": {"effect": "read", "result": "table", "delivery": "chunked", "execution": ["gateway", "handoff"],
      "takes": "request", "returns": "result", "deadline": {"default": "PT2M", "max": "PT30M"},
      "bindings": [{"protocol": "https", "server": "gateway", "method": "POST",
        "address": "/v1/data/portfolio.holdings@v1:fetch",
        "encodings": ["arrow", "json", "ndjson", "csv"], "auth": "bearer"}]},
    "explain": {"effect": "read", "result": "record", "delivery": "single", "execution": ["gateway"], "bindings": ["…"]},
    "export": {"effect": "read", "result": "content", "delivery": "transfer", "execution": ["gateway"],
      "encodings": ["parquet", "arrow"], "bindings": ["…"]}
  },
  "schemas": {
    "request": {
      "$schema": "https://json-schema.org/draft/2020-12/schema",
      "type": "object", "additionalProperties": false, "required": ["portfolios"],
      "properties": {
        "portfolios": {"type": "array", "items": {"type": "string", "x-atlas-logical": "PortfolioCode",
          "x-atlas-reference": "portfolio.list@v1"}},
        "as_of": {"type": ["string", "null"], "format": "date", "default": null},
        "asset_class": {"type": ["string", "null"], "enum": ["credit", "rates", null]},
        "columns": {"type": ["array", "null"], "items": {"type": "string"}}
      }
    },
    "result": {
      "type": "array",
      "items": {
        "type": "object", "required": ["portfolio", "as_of", "market_value", "weight"],
        "properties": {
          "portfolio": {"type": "string", "x-atlas-logical": "PortfolioCode", "x-atlas-key": true},
          "isin": {"type": ["string", "null"], "x-atlas-logical": "Isin", "x-atlas-key": true},
          "as_of": {"type": "string", "format": "date", "x-atlas-key": true},
          "market_value": {"type": "object", "x-atlas-logical": "Money", "x-atlas-aliases": ["mv"]},
          "weight": {"type": "number", "x-atlas-logical": "Fraction"},
          "oas": {"type": ["number", "null"], "x-atlas-logical": "Bp"}
        }
      }
    },
    "result_arrow": "(the same columns as an Arrow schema, with the Atlas keywords as field metadata)"
  },
  "errors": [{"code": "portfolio_not_found", "retryable": false},
             {"code": "warehouse_suspended", "retryable": true}],
  "meaning": {"grain": "portfolio × security × date", "keys": ["portfolio", "isin", "as_of"],
    "as_of": "the portfolio's last valuation on or before as_of",
    "source": "Harbour's portfolio holdings view"},
  "compatibility": {"request": "BACKWARD_TRANSITIVE", "result": "FULL_TRANSITIVE"},
  "evolution": {"previous_profile": "portfolio.holdings@v1#2b71c0d4e9a35f18",
    "lens": [{"op": "rename", "from": "mv", "to": "market_value"}]},
  "fingerprint": "3f9a1c0b7d2e4a65",
  "revision": 7
}
```

### 6.1 How the complete vector (V-data, `portfolio.holdings@v1#8e61efdf45779059`) differs

Build from V-data, not from C:154-208. V-data's expected values: `"fingerprint": "8e61efdf45779059"`, `"exact": "portfolio.holdings@v1#8e61efdf45779059"`, `"valid": true`, `"agent_ready": true`.

| Point | C:154-208 | V-data |
|---|---|---|
| Fingerprint | `3f9a1c0b7d2e4a65` (C:206). C:95 also says `bcaa5bbad97cc612`, and TERMS:25 says `bcaa5bbad97cc612` | `8e61efdf45779059`. The prose fingerprints are illustrative |
| `traits` | names | objects `{"name": ..., "critical": false}`, the same six |
| `explain` | no `takes`, no `deadline`, bindings `"…"` | `takes: "request"`, no `returns`, deadline `PT30S`/`PT2M`, binding `/v1/data/portfolio.holdings@v1:explain`, encodings `["json"]` |
| `export` | `encodings ["parquet","arrow"]`, bindings `"…"` | the same interaction `encodings` (file formats), `takes: "request"`, deadline `PT10M`/`PT2H`, binding `/v1/data/portfolio.holdings@v1:export` with encodings `["json"]` |
| Schema roots | `$schema` only on the request | `$schema` and `$vocabulary` on both, plus root `description`s |
| `portfolios` | array of strings | adds `description`, `minItems: 1`, `maxItems: 500`, `uniqueItems: true`, and an item `description` |
| `as_of` (request) | `["string","null"]`, date, default null | adds description "The valuation date; the latest when null." |
| `asset_class` | `enum ["credit","rates",null]` | adds `description`, `x-atlas-enum` (credit: "Credit instruments.", rates: "Rates instruments."), `default: null` |
| `columns` | items string | adds description, items `minLength: 1`, `uniqueItems: true`, `default: null` |
| Result `required` | `portfolio`, `as_of`, `market_value`, `weight` | `portfolio`, `as_of`, `title`, `market_value`, `weight`, `status` |
| New result columns | — | `title` (`["string","null"]`, "The security's name, as Harbour holds it."); `status` (enum `open`, `closed`, `UNKNOWN`; `closed` has alias `sold`); `duration` (`["number","null"]`, `Years`, deprecated, sunset `2027-03-01`) |
| `oas` | `Bp` | adds `x-atlas-aliases ["oas_bp"]`, `x-atlas-deprecated: null`, description |
| `result_arrow` | present | absent (Q-DOC-6) |
| `errors` | `code`, `retryable` | adds `status` (404, 503), `description`, `next` |
| `usage` | absent | `when`, `when_not`, two `fetch` examples, `related`, `cost` |
| `evolution.changelog` | absent | `[{"date": "2026-10-01", "change": "Renamed mv to market_value."}]` |
| Member order | `fingerprint` before `revision` | `revision` before `fingerprint`; result properties out of sorted order on purpose (Q-ARROW-1: Arrow order is document order, so a Go reader must keep member order) |

V-data's `usage`:

```json
"usage": {
  "when": "You need the positions of named portfolios on a date.",
  "when_not": "You need an index's constituents: use index.constituents@v1.",
  "examples": [
    {"title": "Global credit, latest", "interaction": "fetch", "request": {"portfolios": ["GLOBAL-CREDIT"]}},
    {"title": "Weights on a date", "interaction": "fetch",
     "request": {"portfolios": ["147HYC"], "as_of": "2026-10-30", "columns": ["portfolio", "isin", "weight"]}}
  ],
  "related": ["portfolio.list@v1", "index.constituents@v1"],
  "cost": "One warehouse query; large results are handed off."
}
```

What the first build must change from V-data:
- Remove `Handoff` and `handoff` execution. They are DEFERRED, and removing them is Safe (2.5).
- `export` is DEFERRED (transfer). Removing an interaction is Breaking, so decide before the first publish (AMBIGUOUS in 2.1).
- `"cost": "...large results are handed off."` would then be false; it is prose (`usage`).
- REVIEW "Left to owners" holdings questions the profile must state, and the fixture fix: "null `as_of`, cash and instruments without an ISIN, aggregation, currency and signs, whether weights include cash, and partial authorisation". HB:168 adds ordering, row limits, source correction and freshness, multi-portfolio date alignment, and "a nullable `isin` cannot by itself distinguish every security within one portfolio/date". DR:161 says the same of the grain.
- The handbook's shared request uses `"as_of": "2026-10-28"` and `"columns": ["portfolio", "isin", "as_of", "weight"]` (HB:63-69). CORE:106-127 uses `{"portfolios":["147HYC"],"columns":["isin","weight"]}`. Both validate against V-data's request schema.

---

## 7. Errors: how a capability declares its own, with next steps

### 7.1 In the profile (C:139; PS:224-230, 530-562)

`errors` is required and an array; it may be empty (V-evt). Each item:

| Member | Type | Required (PS) | In projection | Meaning |
|---|---|---|---|---|
| `code` | string `^[a-z][a-z0-9_]*$` | yes | yes | "The problem code." GR:38 `problem-code` |
| `status` | integer 100..599 | yes | yes | "Its HTTP status." |
| `retryable` | boolean | yes | yes | "Whether the same request may succeed if repeated." |
| `description` | string | no in PS; **yes for readiness** | no | "What it means. Prose." |
| `next` | string | no in PS; **yes for readiness** | no | "The step to take. Prose." |

- Scope: "Every problem code this capability can return beyond the protocol's own" (C:139). PS: "beyond the registry (§9.3.3)".
  - AMBIGUOUS: whether a profile may (or must not) list a registry code such as `not_found`. Reading: "beyond" means a profile declares only codes outside the registry.
- **ATLAS-PROFILE-005** "... every capability declares its errors" (C:115). Lint: "errors declared" (C:767).
- Evolution: "An error code added: Dangerous: clients treat an unknown code by its HTTP status and `retryable`" (C:608), so it needs a changelog entry (gate step 5). Rewording `description`/`next` is prose: same fingerprint, new revision.
- The vectors' declared errors, as examples of the house style:

  | Profile | `code` | `status` | `retryable` | `description` | `next` |
  |---|---|---|---|---|---|
  | V-data | `portfolio_not_found` | 404 | false | "No portfolio has one of the codes asked for." | "Call portfolio.list@v1:fetch for the codes you may use." |
  | V-data | `warehouse_suspended` | 503 | true | "The warehouse that serves the holdings is suspended." | "Wait retry_after seconds, then repeat the call." |
  | V-res | `dashboard_too_large` | 413 | false | "The bundle is larger than a dashboard may be." | "Remove data from the bundle, or fetch it at run time." |
  | V-act | `deployment_busy` | 409 | true | "The deployment is already restarting." | "Wait retry_after seconds, then plan again." |

  `next` names a concrete call or wait. Where it says "Wait retry_after seconds", it refers to the problem's `retry_after` member.

### 7.2 On the wire (PROB; the problems reader owns the detail)

- PROB: "Every failure (§4.1, §9.3.3): RFC 9457 with Atlas's members, sent as application/problem+json with Atlas-Error-Code equal to `code`. `type` is https://atlas.jhg.com/problems/<code>. A client treats a code it does not know by its status and retryable."
- Required members: `type`, `status`, `code`, `retryable`, `next`, `request_id`.
- Optional members: `title`, `detail`, `instance`, `retry_after` (integer ≥ 0, "as Retry-After"), `target`, `details`, `estimate`.
- `code`: "A code of the registry ($defs/registry_code), or one the capability's profile declares under `errors`." `retryable`: "True only when repeating the same request is safe and may succeed." `next`: "The step a person or agent can take, in words."
- AMBIGUOUS: the sources do not say that the gateway copies the profile's `next` (and `status`, `retryable`) into the problem it sends for that code. Reading: the gateway fills the problem's required `next` from the declared error's `next`, and its `status`/`retryable` from the declared entry, so profile and wire agree. Not stated in my files.
- Q-DOC-8: the schema "does not enforce status or retryable" for registry codes, because `invalid_request` is also sent with 406 and `retryable` can be false for an unsafe write.
- Registry codes relevant to this digest's rules: `critical_trait_unsupported` 422; `resolution_failed` 422; `retired` 410; `upgrade_required` 426; `invalid_request` 400; `invalid_document` 400 (also a fingerprint mismatch, FP §9.4.2); `not_found` 404 (also "a confidential one the caller does not hold"). All have `retryable: false` (PROB `$defs/registry`).

### 7.3 What the review adds

- CORE:78, card part "Recovery": "Specific problems with actionable next steps."
- HB:123, page part "Failure and recovery": "Known business problems, retryability, precise next steps and an owner/support route." See the support-contact gap in 1.3.
- HB:164: the abbreviated profile's errors "still need complete descriptions and next steps". V-data supplies them.
- CORE:80: "whether an empty answer means no positions, unavailable data or an unsupported portfolio": this is owner work that may need its own declared codes or documented empty-result meaning.
- Acceptance item 5: "Invalid requests, permission changes, exact decimals, incomplete streams, idempotent retries and source failures behave the same in every client" (REVIEW).

---

## Appendix A. Terms (TERMS:17-43) used above

Capability ("A named, versioned concept on one surface"); Surface ("a profile's `family`"); Interaction; Address (`capability@version:interaction`); Binding; Profile ("Everything a capability says about itself, with a fingerprint: its contract"); Fingerprint ("The 16-character hash of a profile's canonical form"); Writer profile; Reader profile ("for an application, the one in its lock"); Lens ("A declared, closed-form mapping between two profiles of one capability"); Execution; Available ("Granted and not denied, present in the environment, lifecycle allowing it"); Level ("Core, Streaming, Columnar, Bulk, Events, Handoff (Broker deferred)").

Requirement words are RFC 2119. Requirements are numbered `ATLAS-<AREA>-NNN` in §1-§8 and `AP-<AREA>-NNN` in §9, and a number is never reused (TERMS:47-48). AGENTS.md: a test that proves a requirement names its id in the test file.

## Appendix B. Every requirement id in this digest

ATLAS-CONTRACT-001; ATLAS-PROFILE-001..005; ATLAS-TYPES-001..003; ATLAS-VERSION-001..010; ATLAS-RESOLVE-001..004 (C:839-842; RESOLVE-002 "unless a field it reads was dropped after its sunset" is moot under R03 within a version); ATLAS-CAP-001..006 (CAP-006 "Each family has a conformance battery generated from this spec, run against every capability of that family", CAP:57); ATLAS-VISION-002; ATLAS-CLIENTS-006 (lint); ATLAS-CORE-009 (gate step 8); AP-FP-001 (FP).
