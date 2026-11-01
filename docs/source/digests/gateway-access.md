# Digest: inside the gateway, part 2 (access, limits, audit, reaching systems, data sources, implementation patterns)

> Historical engineering study (2026-10-31). Retained for its requirements, counterexamples and design reasoning. Implementation observations and line references describe that review snapshot; use [current progress](../../HANDOFF-PROGRESS.md) and the named source symbols for current status. Paths are relative to the repository root unless a citation key says otherwise.

Reader 2026-10-31. Every fact carries its source. Copied text is in `code` or quotes. Notation:

| Short | File (all read in full unless marked) |
|---|---|
| S62 | `docs/source/atlas-spec/06-2-access-limits-and-audit.md` (184 lines) |
| S63 | `docs/source/atlas-spec/06-3-reaching-systems.md` (156 lines) |
| S64 | `docs/source/atlas-spec/06-4-data-sources.md` (103 lines) |
| S67 | `docs/source/atlas-spec/06-7-implementation-patterns.md` (321 lines) |
| CORE | `docs/source/atlas-spec/review/ATLAS-CORE.md` (245 lines, read in full; §3 L32-56, §9 L190-202, §11 L218-230 assigned) |
| DR | `docs/source/atlas-spec/review/DESIGN-REVIEW.md` (192 lines, read in full; R05 L66-74, R06 L76-82, R07 L84-90, R08 L92-98, R09 L100-106, R13 L140, R17 L144 assigned) |
| RV | `docs/pass-2/REVIEW.md` (100 lines, read in full; governs the build) |
| AG | `AGENTS.md` (72 lines, read in full) |
| S221 | `docs/source/atlas-spec/02-2-1-snowflake-data.md` (178 lines, read in full as a cross-check; not assigned) |
| S61, S03, S96, S97, S41, INT | Cross-checks only, excerpts read: `06-1-front-door-and-services.md`, `03-contracts.md`, `09-6-http-binding.md`, `09-7-clients.md`, `04-plugging-in.md`, `review/INTEGRATIONS.md` |
| REF | `spec/protocol/references/` (`schemas/governance.schema.json` read in full; `schemas/builtins.schema.json` L928-1157 `audit_request`/`audit_record`; `schemas/problem.schema.json` code table; `README.md` L170-190; `QUESTIONS.md` grep) |

Markers: **IN SCOPE** = the first build (the journey `portfolio.holdings@v1`, `portfolio.list@v1`, `portfolio.search@v1`, `data.status@v1`, then `portfolio.holdings-dates@v1`; RV L37-42). **DEFERRED** = not in the first build. **OVERRIDDEN BY REVIEW: Rnn** = RV/DR/CORE changes the spec. **AMBIGUOUS:** = readings listed. **MISSING:** = no source states it.

Build context that frames every section (RV):
- One Go deployable, plus a worker where durable work needs one; provider boundary in process and serialisable (RV L13; CORE L186).
- Building without Docker; DuckDB and sqlglot fake Snowflake; fakes behind real adapters (RV L21).
- Step 3 ("before metered or changing work"): R08, R06, R07, R09 — "Each R's acceptance test passes against the fakes" (RV L31).
- Step 5 ("real systems"): "the Development source, direct downloads, events, delegated identity" are out of reach; "plug-in points and gated proofs are ready" (RV L33). So: **OBO, real Entra, real Cosmos, WORM blob, Log Analytics, real Snowflake = DEFERRED; their ports and fakes = IN SCOPE.**
- Acceptance #3: "A second person with narrower data scope receives a correspondingly narrower result, or a clear refusal." (RV L48). #5: "Invalid requests, permission changes, exact decimals, incomplete streams, idempotent retries and source failures behave the same in every client." (RV L50).

---

## 1. Identity

### 1.1 Kinds of identity (S62 §6.2.1 L66-74)

| Kind | Who | Sign-in (copied) |
|---|---|---|
| Person | "A member of the firm" | "Entra ID, through a client's browser sign-in (PKCE) or device code, shared by every client on the machine; in a Nautilus page, the application's own Entra sign-in" |
| Agent for a person | "An agent acting in a person's session" | "The person's sign-in; an agent never holds more than its person" |
| Workload | "A service, pipeline or Astronomer deployment" | "An Entra service principal or managed identity" |

- Entra app roles are coarse: `Gateway.User`, `Gateway.Admin`; "everything finer is Atlas's" (S62 L74). `atlas.audit:fetch` needs `Gateway.Admin` (REF builtins L930).
- Principal = token's `oid`, "never `sub`"; "`idtyp: app` means a workload" (S67 §6.7.4 L141; ATLAS-MECH-005 S67 L50).
- Audit `caller.kind` enum: `person`, `workload`, `UNKNOWN` (REF builtins audit_record).
- CORE §3 L46: for each invocation record "the effective principal, calling application or workload, environment and relevant policy revision". "Where an agent is distinguishable from an interactive person, that distinction comes from trusted authentication context, not an `is_human` parameter supplied by the agent."
- CORE §3 L48: "A scheduled job runs under an explicitly authorized workload identity. It does not retain the interactive user's token or silently inherit the user's personal grants. The same capability contract applies, but its available scope can differ." **IN SCOPE** (identity model is RV step 1, L29).
- AMBIGUOUS: how an agent is told apart from its person. S62 L71 gives the agent the person's own sign-in, so the token is the same. DR R06 L78 names this problem. Readings: (a) not distinguishable in the first build; record as `person`. (b) a distinct client id or app claim in the token marks an agent. No source defines that claim.

### 1.2 How each hop proves who is calling (S62 §6.2.2 L76-89)

1. Caller to Entra: every client is a *public client*: system browser with PKCE and loopback redirect (RFC 8252), or device code; token "whose **audience is the Atlas Gateway**". Shared token store (§9.7.3). A workload "gets its own token from its managed identity or service principal and passes it to the client" (S62 L86).
2. Client to gateway: "The gateway validates issuer, audience, signature and expiry and builds the caller from the token's claims alone. **No identity header is trusted:** a header naming another identity is ignored and recorded." (S62 L87). The named header: `x-atlas-on-behalf-of` "is ignored and recorded" (S62 L35). ATLAS-ACCESS-007: "An identity header on a request changes nothing and is recorded." (S62 L59). **IN SCOPE.**
3. Gateway to a system: as the person by **On-Behalf-Of**, or as itself with the person recorded in audit (and in e.g. a DAG run's `conf`). "A claims challenge from Entra (conditional access) is returned to the client as `401` with `WWW-Authenticate`" (S62 L88). OBO: **DEFERRED** (RV L33).
4. Caller's own credentials (git credential store, `gh`, `az`) never reach the gateway (S62 L89; ATLAS-ACCESS-006 L58).

### 1.3 Token verification values (S67 §6.7.4 L132-145) — IN SCOPE (fake issuer runs the real verifier: S67 L145)

| Setting | Value (copied) |
|---|---|
| Issuer | "Required; the tenant's v2.0 issuer (and v1 only if the gateway's app registration issues v1 tokens)" |
| Audiences | "The gateway's application id URI and its client id" |
| Algorithms | `RS256` only |
| Keys | "The tenant's JWKS from OIDC discovery, fetched at start, refreshed on an unknown `kid` (one fetch for many callers)" |
| Leeway | 5 minutes |
| Principal | `oid`, never `sub`; `idtyp: app` means a workload |
| Groups | "From the token; on overage, from Microsoft Graph, cached for 10 minutes" |
| Refusal | `401`, `WWW-Authenticate: Bearer realm="atlas"` (with `error="invalid_token"` when expired), `unauthenticated` |

ATLAS-MECH-005 (S67 L50): "The gateway refuses to start without an issuer; a token with `alg: none`, an unlisted algorithm, an unknown `crit` header or no `kid` is `unauthenticated`; the principal is the token's `oid`."
Problem `unauthenticated` = 401, not retryable (REF problem.schema.json).

### 1.4 Groups and roles
- "A role's members are **Entra security groups**, named by object id; Atlas never stores who is in a group" (S62 L107).
- A grant to one person is `user:<oid>`, or one workload: "the named exception (§6.1.5)" (S62 L107).
- Groups come from the token; on overage from Graph, 10 minutes cache (S67 L142, L227).

### 1.5 Tenants and context
- Credentials "are bound to a trusted gateway, authority, audience, tenant/account and resource scopes. Concurrent refreshes are serialized. An unauthenticated bootstrap cannot direct a client to send an existing token or refresh credential to an untrusted server or authority." (CORE §3 L52; RV R26 L84, step 4).
- "If several accounts, tenants or environments could apply, the client requires a deliberate selection rather than silently choosing one." (CORE §3 L50).
- Idempotency scope includes tenant (CORE §9 L198; see §4).
- Left to owners: "The organisation and tenant switching contract" (RV L98).
- MISSING: which token claim carries the tenant (no `tid` named in any assigned file). The issuer is "the tenant's" (S67 L136), so a single-tenant gateway reads the tenant from its configured issuer. AMBIGUOUS: single tenant per deployment (issuer fixed), or multi-tenant (tenant from the token). §6.7.4 reads as a single tenant.

### 1.6 What comes from the token and what from the request

| From the validated token only | From the request (client's claim, validated) |
|---|---|
| Principal `oid` (S67 L141) | Address `id@vN:interaction` (S62 L96) |
| Kind: `idtyp: app` → workload (S67 L141) | Request body (validated at step 4) |
| Groups (overage → Graph) (S67 L142) | `Idempotency-Key` (S96 L35) |
| Issuer/tenant, audience, expiry (S62 L87) | `Atlas-Deadline` (S96 L34; S67 L110) |
| | `Atlas-Confirm`: "the client's claim only" (RV L18) — never consent (R06) |
| | Identity headers such as `x-atlas-on-behalf-of`: ignored and recorded (S62 L35, L59) |
| | Contextual defaults resolved by the gateway, provenance recorded (CORE §3 L44) |

### 1.7 Delegation (OBO) (S63 §6.3.5 L115-131)
- "built once, as one identity port, and each system adds only its scope and one admin consent" (S63 L117).
- Scopes: Azure Resource Manager/Resource Graph `https://management.azure.com/user_impersonation`; Storage `https://storage.azure.com/user_impersonation`; Service Bus `https://servicebus.azure.net/user_impersonation`; Microsoft Graph `Files.ReadWrite.All` "or narrower"; Snowflake `api://<Snowflake's app>/SESSION:ROLE-ANY` "(a read-only scope to request)" (S63 L121-125).
- "It gives identity, not rights" (S63 L131). Conditional access → the exchange fails with a challenge, returned to the client (S63 L131). "An exchanged token lasts about an hour, so long operations refresh it, or run as a workload with the person recorded." (S63 L131).
- Rule 2: "Tokens the gateway obtains by OBO never leave it." (S63 L57); ATLAS-REACH-003 (S63 L63).
- Open: "Whether the gateway executes as the person by default (rather than its service identity) is open. Recommended: as the person, for data capabilities." (S62 L177).
- OVERRIDDEN BY REVIEW: R05 — "Do not fall back silently from failed user delegation to a service identity." (DR L72).
- **DEFERRED:** the real OBO exchange (RV L33 "delegated identity"). **IN SCOPE:** the identity port shape, so it can be plugged in (RV L33 "The plug-in points ... are ready").

---

## 2. Authorisation

### 2.1 Grants, roles, denies (S62 §6.2.4 L105-113)
- "**Roles** are named sets of grants with limits." (S62 L107).
- "A **grant** names a capability (every interaction), one interaction (`nautilus.dashboard:delete`), or a domain or family by namespace (`portfolio.*`)." (S62 L108).
- "A deny always wins over a grant." (S62 L109). ATLAS-ACCESS-001: "A deny on a capability or interaction refuses it whatever grants the caller holds." (S62 L53). A deny can sit on "the capability, the interaction or its family" (S62 L97).
- "Family grants and the `everyone` role reach only interactions whose effect class is `read`. Anything else is granted by exact address." (S62 L110). ATLAS-ACCESS-002 (S62 L54).
- Named-grant only: "free-form SQL, holding a durable subscription, tickets" — "never reached by a family grant or a role, only by a grant naming them" (S62 L111).
- Systems reached as the gateway's own identity: "its capabilities are granted by name only, never by a family grant or `everyone`" (S63 L58, rule 3).
- How held: `held_by` is "`role:<name>`, `user:<oid>` or `everyone`" (REF governance.schema.json L44).
- Open: grants per interaction; "Recommended: yes; a grant on a capability keeps meaning every interaction." (S62 L179). The governance schema already keys `for_caller.interactions` by interaction name (REF governance L30-47), and S62 L108 already lets a grant name one interaction. IN SCOPE reading: grants by capability, by interaction and by namespace.

### 2.2 Visibility
- "A caller is told how they hold each capability and who set each limit, and nothing about anyone else." (S62 L112).
- "What a caller cannot use is hidden from everything they see while working; only `atlas.access` shows what they lack, why, and how to get it (§2.6)." (S62 L113).
- Rule 4: "Every refusal names what is missing and what to do next; a refusal of a listed capability names `atlas.access`." (S62 L50).
- CORE §3 L54: "The normal catalogue contains only visible, authorized interactions. The access view explains missing access to listed capabilities. Confidential capabilities remain undisclosed. Metadata and examples referring to other capabilities follow the same visibility rules."
- CORE §8 step 3: "Enforce visibility, deny and grant rules without exposing a confidential contract." (CORE L174).
- House rules reach a caller only when the caller can use every capability the rule names (S02-6 L109, cross-check). The snapshot holds "house rules with their revision" (S67 L216).
- AMBIGUOUS: the problem code for calling a confidential capability. Readings: (a) `not_found` 404, so it stays undisclosed (consistent with CORE L54 and ATLAS-MECH-007's use of `not_found` for another principal's operation, S67 L52); (b) `not_granted` 403. No assigned file states it.

### 2.3 The decision order

Spec order (S62 §6.2.3 L93-101) — refuse at the first step that fails:

| # | Step | Content (copied/condensed) |
|---|---|---|
| 1 | Authenticate | "the identity, from a validated token only" |
| 2 | Resolve | "the address `id@vN:interaction`, its profile and its governance" |
| 3 | Deny | "a deny on the capability, the interaction or its family refuses it" |
| 4 | Grant | "a role, the caller's own grant, or `everyone`, reaching this interaction" |
| 5 | Conditions | "the action's environment policy (refused in Production unless a person runs it); approval required and not yet given; later, conditions on the resource the request names (§6.2.5)" |
| 6 | Validate | "resolved to the current profile (§9.5) and its defaults filled, then checked against its schema and the profile's invariants; for a write, its idempotency key is checked" |
| 7 | Limits | "rate, concurrency, output caps and budgets (§6.2.6), reserved before the work starts" |

Why (S62 L103): validation after authorisation, "so a caller without a grant learns nothing about a request's shape"; defaults before validation; "limits follow validation, so an invalid request never spends one; and the idempotency check comes before limits, so a replay spends nothing twice."

- ATLAS-ACCESS-009: "An invalid request from a caller who holds no grant for its interaction is refused with `not_granted` (or `denied`), never `invalid_request` or `resolution_failed`." (S62 L61).
- ATLAS-ACCESS-010: "An invalid request from a caller who holds the grant is refused with `invalid_request` (or `resolution_failed`), reserves no limit, and leaves nothing to settle." (S62 L62).

The pipeline (S67 §6.7.3 L114-128), with refusal codes:

| Step | Refuses with |
|---|---|
| 1 authenticate | `unauthenticated` |
| 2 resolve (snapshot: profile, governance, binding; narrows the deadline) | `not_found`, `not_in_environment`, `retired`, `upgrade_required`, `critical_trait_unsupported` |
| 3 authorise (deny, grants, conditions) | `denied`, `not_granted`, `approval_required` |
| 4 validate (+ begins the idempotency record for a write) | `resolution_failed`, `invalid_request`, `idempotency_conflict`, `in_progress` |
| 5 limit ("Reserves every window in one batch"; the confirmation threshold) | `limit_exceeded`, `budget_exceeded`, `confirmation_required` |
| 6 route ("Signs the token for the service (address, subject, deadline, request id, idempotency key, trace)") | `unavailable` |
| 7 execute | declared errors; `upstream_failed`, `deadline_exceeded` |
| 8 settle and audit | — |
| 9 respond ("Streams the answer as it arrives") | — |

Status codes (REF problem.schema.json registry): `unauthenticated` 401; `not_granted` 403; `denied` 403; `approval_required` 403; `budget_exceeded` 403; `confirmation_required` 428 (requires `estimate`); `not_found` 404; `not_in_environment` 404; `idempotency_conflict` 409; `in_progress` 409 retryable; `retired` 410; `precondition_failed` 412; `critical_trait_unsupported` 422; `resolution_failed` 422; `upgrade_required` 426; `limit_exceeded` 429 retryable; `output_limit_exceeded` 200 (end line only); `internal` 500; `upstream_failed` 502 retryable; `unavailable` 503 retryable; `deadline_exceeded` 504; `invalid_request` 400. `in_progress`, `limit_exceeded`, `unavailable` require `retry_after` (REF problem.schema.json L91-111).

OVERRIDDEN BY REVIEW (CORE §8 L170-180; R05, R06, R08, R09). The review's order:
1. "Validate identity and establish trusted caller context."
2. "Resolve the declared address to an admitted capability and interaction."
3. "Enforce visibility, deny and grant rules without exposing a confidential contract."
4. "Validate and normalise the request, then enforce its resource and data scope."
5. "Look up any caller-scoped idempotency key. After current access checks, replay or reconcile its existing operation without a second dispatch or spending reservation; reject a conflicting request."
6. "For a new execution, check current confirmation and approval evidence, claim its idempotency identity and reserve limits through a recoverable admission transaction."
7. "Record durable admission and dispatch the work through its provider."
8. "Return or stream the result, or return the durable operation that owns the work."
9. "Record outcome and settle usage, including later source reconciliation where required."

Plus (CORE L182): "Each attempt, including a replay, refusal or failed authentication, leaves an audit fact. Transport-level size, syntax and timeout checks can reject a malformed request without exposing any capability-specific information; the authorization order governs schema-dependent diagnostics."

Differences to implement:
- Resource/data scope is a new check after validation (CORE step 4; R05).
- Approval/confirmation evidence moves from spec step 3/5 to CORE step 6, after the idempotency lookup. AMBIGUOUS: where `approval_required` is raised. Readings: (a) spec §6.7.3: step 3 (authorise); (b) CORE §8: step 6, after validation and idempotency lookup, so a replay of an approved run is not re-asked. The review governs (RV L5): take (b), but keep the code `approval_required`.
- Spec "begins its idempotency record" at step 4; CORE splits into lookup (step 5) and claim together with the reservation in one "recoverable admission transaction" (step 6). A replay is re-authorised: "After current access checks" (CORE L176) and "Current authorization can still refuse access to the retained result" (CORE L200).
- Admission fact is durable before dispatch (CORE step 7); audit is no longer only at step 8 (R09).
- Environment condition kept: "refused in Production unless a person runs it" (S62 L99). The journey is reads, so this is not exercised. AMBIGUOUS: how "a person runs it" is proved, given R06 (an agent holds the person's token).

### 2.4 Scope of returned data (R05) — IN SCOPE (RV steps 2 and 4; acceptance #3)
- Spec: resource conditions "later" (S62 §6.2.5 L115-117): a capability "may declare how its request names a resource (`resource_scope`)"; "the source's own controls are the final authority"; Atlas's checks "are set at or below them"; a source refusal is "recorded as a policy mismatch"; Cedar is "the chosen direction". Decided: "A policy engine in the request path is not added now." (S62 L174).
- OVERRIDDEN BY REVIEW: R05 — "Resource and returned-data scope is enforced from the first shared-credential source, by small typed rules. Relevance never grants access" (RV L63). DR L72: "Enforce resource and returned-data scope from the first shared-credential integration. Small, typed rules and curated queries are sufficient; a general policy language can wait. Do not fall back silently from failed user delegation to a service identity. Keep relevance/defaults distinct from permission."
- CORE §3 L42: "Relevance is a ranking or default, never permission. Having a portfolio-related role does not imply permission to every portfolio. A capability grant does not itself establish row-level entitlement."
- CORE §3 L56: "When Atlas connects as a service identity, it must enforce the caller's resource and data scope before using that broader identity. This is required for the first such integration; it cannot wait for a future general-purpose policy language."
- CORE §8 L188: "Do not implement an independent entitlement engine in every service. Do supply providers with the enforced scope they need, and retain source-level controls as an additional check."
- INT L64: "Validate the business request and authorized data scope before planning." INT L86: object/function allowlists cannot enforce row or column entitlements under a broad service credential; "refuse the capability where those controls cannot enforce its authorized data boundary."
- R05 acceptance (DR L74): "Two users with access to the same capability but different portfolio scope receive only their permitted data, through every delivery path and any result cache."
- RV step 2 "Done when": "Fixtures cover two identities with different portfolio scope" (RV L30).
- How the narrower identity sees less, or is refused: RV L48 allows either "a correspondingly narrower result, or a clear refusal". Which one is an owner's choice: "partial authorisation" is in RV "Left to owners" (L97); the handbook lists "partial authorization or missing portfolios" (CAPABILITY-HANDBOOK L168). "Each fixture states the choice it fixes" (RV L96).
- AMBIGUOUS: behaviour per request shape. Readings for `portfolio.holdings` with `portfolios: [A, B]` where the caller may read only A: (a) refuse the whole call (code `not_granted` 403 or `denied` 403; neither is defined for scope); (b) return A's rows and report B as excluded (a warning; no warning code is defined in assigned files); (c) for `portfolio.list`/`portfolio.search`: return only in-scope portfolios (filtering is the natural reading for a list). For a missing `portfolios` default: CORE L44 says defaults come from "authorized lookups" and "Ambiguous choices require selection". The fixture must state the choice.
- MISSING: the data shape of a "small typed rule" (e.g. principal/group → allowed portfolio codes) and where it lives. Readings: (a) in governance, beside grants (moves `governance_revision`); (b) a provider-side scope table fed by the gateway (CORE L188 "supply providers with the enforced scope"). Either way it must be in the snapshot (S67 L216: the request path never reads the store, ATLAS-MECH-010).
- Result reuse: R18 — "Results are private to their caller; any reuse is keyed by authorisation, environment and data revision, and reauthorised" (RV L76; CORE L230). Spec: "No result is cached until O60 is decided" (ATLAS-MECH-011 S67 L56; S67 L229).

### 2.5 The governance document and its revision — IN SCOPE
- Three documents each with its own revision: Profile (fingerprint), **Governance** ("Who may use each interaction, their limits, budgets and approval policy" — "A **governance revision**, recorded in every audit record"), Availability (no revision) (S03 L122-126).
- Schema (REF `governance.schema.json`, `$id` `https://atlas.jhg.com/protocol/1.0/governance.schema.json`): required `capability` (format `pinned-id`), `governance_revision` (string, minLength 1, "Recorded in every audit record; Atlas-Governance-Revision."), `for_caller` (required `interactions`, `limits`, `approval`).
  - `for_caller.interactions`: object keyed by interaction name (pattern `^[a-z](-?[a-z0-9])*$`), each `{held_by}` (required).
  - `for_caller.limits[]`: `$defs/limit`: required `kind` ∈ `rate`, `concurrency`, `output`, `budget`, `confirmation`, `UNKNOWN`; optional `interaction`, `per_minute` (int ≥0), `max` (int ≥0), `max_rows` (int ≥0), `max_bytes` (int ≥0), `remaining` (number), `amount` (string, format `decimal`), `unit`, `period`, `set_by` ("`role:<name>`, `user:<oid>`"), `resets` (date-time).
  - `for_caller.approval`: object or null ("The approval policy, or null.").
- Example (S03 L315-328): `"governance_revision": "g-41"`, fetch held by `role:fi-analysts`, rate `per_minute` 60 `remaining` 58, concurrency `max` 4, output `max_rows` 1000000 `max_bytes` 1073741824, `approval: null`.
- Header: `Atlas-Governance-Revision` — response, String, "The governance revision the call was decided under", "Every interaction answer" (S96 L42).
- A governance change "moves the governance revision, never a fingerprint" (S67 L220; S03 L611). "Tightening may happen at any time. Loosening a limit or an approval policy is reviewed by the capability's domain owner and recorded in the audit trail." (S03 L611).
- The audit record holds `governance_revision` (REF builtins audit_record; S62 L146).
- OVERRIDDEN BY REVIEW: R01 — "Describe's caller state (governance, availability) is private and uncached." (RV L59). R06 — approval is "live policy, resolving its duplicate placement in contract meaning and governance" (DR L80).

---

## 3. Limits, budgets, confirmation, receipts

### 3.1 The five limits (S62 §6.2.6 L121-127)

| Limit | Holds (copied) | Refusal (copied) |
|---|---|---|
| **Rate** | "Calls per period per caller and interaction (a token bucket)" | `limit_exceeded` (429) with `Retry-After` |
| **Concurrency** | "Calls in flight per caller, interaction and upstream" | `limit_exceeded` (429), or `503` with `Retry-After` when a service sheds load |
| **Output** | "Rows and bytes per answer" | "The answer is truncated and says so (`output_limit_exceeded`)" |
| **Budget** | "A metered quantity (Snowflake credits, a vendor's billed calls) per caller, pool or the firm, per period" | `budget_exceeded` (403) |
| **Confirmation** | "A threshold on one call's estimated cost, per caller and metered capability, set like any limit" | `confirmation_required` (428) with the estimate, until the person confirms it (`Atlas-Confirm`) |

- Allowances (per caller), pools (shared, firm-wide), ceilings (hard maxima); inherited firm → domain → capability → interaction; "an override may only tighten" (S62 L129).
- "A call **reserves** its upper bound before it starts, acts, then **settles** its actual use (for Snowflake, from the warehouse's own record)" (S62 L129).
- "Streams and subscriptions count connection time, messages and bytes." (S62 L129).
- "Every answer reports what is left (`Atlas-Limit-Remaining`)." (S62 L129). Wire: response, Dictionary of Integers, "One member per limit (`rate=58, concurrency=3`)", "Every metered answer" (S96 L43). AMBIGUOUS: "Every answer" (S62 L129) vs "Every metered answer" (S96 L43). §9 is normative for the wire: "Every metered answer".
- `atlas.usage`: "what is left" from the live counters; "what has been used over time" from the audit's settled records, "minutes behind" (S62 L131).
- Output truncation wire: end line `truncated: true` with `output_limit_exceeded` (status 200), reported as a warning (REF QUESTIONS Q-DOC-15).
- ATLAS-ACCESS-003: "Limits hold exactly when many copies of the front door serve the same caller at once; a burst of twenty parallel calls starts at most what the budget allows." (S62 L55).
- ATLAS-MECH-013: "A rate limit is a token bucket on the stored counter: a burst of its size passes, the next call is `limit_exceeded` with `retry_after` equal to the time to one token." (S67 L58).
- Mechanics (S67 §6.7.13 L244): `adapter/cosmos` "reserves every window a call draws on in one transactional batch of conditional patches, and settles in another"; "A per-replica bucket is used only for the pre-auth limit (6.7.2) and for degraded mode (§6.1.14)." S61 L143: each patch "adds `n` only if its counter stays within its limit; if one fails, none applies"; "no read first". S61 L147: partition "a caller's counters for one capability (every window of it) in one partition".

### 3.2 Budget authority — OVERRIDDEN BY REVIEW: R07
- RV L16: "A declared budget authority per metered provider. Unsupported combinations of hard budgets are refused. Protective rate limits are kept apart."
- RV L65: "One budget authority per metered provider and an atomic reservation set. Unsupported combinations are refused" (step 3).
- DR L86 evidence: "Cosmos batch atomicity is limited to one container and logical partition, so the described layout does not atomically cover those shared pools."
- DR L88: "Separate protective rate limits from hard spending admission. Choose a transaction boundary per shared budget authority, co-locate its atomic reservation set or allocate bounded sub-budgets. Reject unsupported combinations initially. Prove any broader cross-budget scheme before promising exactness."
- CORE §11 L224: "Separate operational rate limiting from monetary or licensed-use admission. Exact reservations require a transaction design that actually covers the shared budget. For the first implementation, use a declared budget authority per metered provider; reject unsupported combinations of independently partitioned hard budgets until an atomic design is proved. Do not label approximate per-replica limits exact."
- R07 acceptance (DR L90): "Different users on different replicas contend for one shared hard budget; combined admissions never exceed it. Abandoned reservations recover, and settlement is idempotent." CORE §12.5 L240: "test two callers contending for one shared budget".
- Estimates (CORE §9 L194): "An estimate says what was estimated, its unit, basis and uncertainty. Unknown cost remains unknown. Scanned bytes, output bytes and money are distinct quantities. A price or runtime estimate is not a hard cap unless the execution mechanism can enforce it."
- MISSING: how a budget authority is declared (profile? governance? provider registration?); "refused" — at registration/snapshot check or at call time, and with which problem code. Readings: (a) the snapshot check (S67 L218) refuses a governance revision that combines hard budgets across authorities; (b) the call is refused at step 5 with `budget_exceeded` or `unavailable`. Reading (a) fits "Reject unsupported combinations initially".
- MISSING: reservation recovery TTL ("Abandoned reservations recover"). S61 L245: `settlement` controller resync 5 minutes for handed-off/exported calls.
- AMBIGUOUS: whether the journey has a metered provider. None of the four journey capabilities is stated as metered in assigned files. Reading: build the budget authority and its acceptance tests on a conformance capability (REF README L184 `conformance.costly@v1`) and fakes (RV L31).

### 3.3 Confirmation and approval receipts — OVERRIDDEN BY REVIEW: R06
- Spec wire: `Atlas-Confirm` — request, Decimal, "The highest estimated cost the person accepted for this call, in the unit of the problem's `estimate`", sent "After `confirmation_required`" (S96 L39). Senders always write a decimal point (`1250.0`); an Integer is treated as absent (REF QUESTIONS Q-HDR-4). `estimate.amount` is a decimal string, at most 12 integer and 3 fractional digits (Q-DOC-10). Client: on `confirmation_required` "a client with a person present shows them the problem's `estimate` and, if they accept, repeats the call once with `Atlas-Confirm` set to that amount and the same `Idempotency-Key`; without a person, it raises the problem." (S97 L120).
- RV L18: "A confirmation or approval receipt bound to the principal, address, normalised request, plan digest, scope, maximum and expiry. The header is the client's claim only."
- RV L64: "A receipt from a trusted interaction, bound to the principal, address, normalised request, plan digest, scope, maximum and expiry" (step 3).
- DR L80: "Distinguish authenticated caller context from approval evidence. Bind an approval or confirmation receipt from a trusted interaction to the principal, action, canonical request/plan, scope, approved maximum and expiry. Define what policy tightening does to outstanding plans. Treat approval as live policy".
- CORE §9 L196: binds "to the effective principal, address, normalized request, plan digest, scope, limits, expiry and relevant policy. A changed plan or increased approved maximum needs a new decision. A request header saying “confirmed” is not evidence that a person approved it."
- R06 acceptance (DR L82): "An agent cannot authorize a new plan by changing a header, copying another user's receipt, increasing the amount or changing the request after approval."

Receipt binding, union of the three sources:

| Bound member | RV L18/L64 | DR L80 | CORE L196 |
|---|---|---|---|
| principal | yes | yes | "effective principal" |
| address / action | address | action | address |
| normalised request | yes | "canonical request/plan" | "normalized request" |
| plan digest | yes | (in "request/plan") | yes |
| scope | yes | yes | yes |
| maximum | yes | "approved maximum" | "limits" |
| expiry | yes | yes | yes |
| relevant policy | — | — | yes |

- MISSING: the receipt's wire format, the "trusted interaction" that issues it, its signing, and how the call presents it. Readings: (a) a server-held record keyed by id, presented by id; (b) a signed token. Neither is defined. The audit record has `receipt` (object) (REF builtins).
- MISSING: what policy tightening does to outstanding plans ("Define", DR L80). S03 L699: "Approval required in more environments | Dangerous | Announced; runs already approved stand".
- CONFLICT: the reference scenario says `conformance.costly@v1` answers `confirmation_required` (estimate 12.500 credits) "unless Atlas-Confirm is at least 12.5, then `{ok: true}`" (REF README L184; scenario `CORE-CONFIRM-001`). Under R06 the header alone is not evidence. Readings: (a) the conformance gateway keeps header semantics as a client-behaviour test; the real gateway requires a receipt and treats the header as the client's acceptance claim; (b) the vector is amended. Write the case down; do not resolve silently.
- First build: the journey is reads; confirmation/approval are built at step 3 against fakes (RV L31). IN SCOPE as mechanism + acceptance test; DEFERRED as a journey feature.

---

## 4. Idempotency and operations

### 4.1 Idempotency (S67 §6.7.8 L193-204)

| Part | Spec (copied) | Review |
|---|---|---|
| Where | "Step 4, for every call whose effect is not `read`; a missing key is `invalid_request`, target `Idempotency-Key`" | OVERRIDDEN BY REVIEW: R08 — "Include metered reads when a retry can duplicate a charge" (DR L96); "Metered reads that can incur duplicate charges need this treatment too." (CORE L198). CORE §8 L176: lookup at step 5, claim at step 6 |
| Scope | "The principal (`oid`) and the address" (also ATLAS-MECH-008) | OVERRIDDEN BY REVIEW (CORE §9 L198): "within tenant, principal, environment and capability-interaction scope" |
| Fingerprint | "SHA-256 of the request's JCS form (§9.4), so member order and whitespace never matter" | CORE L198: bind "to the normalized semantic request and execution-affecting options, not JSON whitespace or a mutable HTTP route" |
| Same key, other request | `idempotency_conflict` (409) | same (AG L15) |
| Same key, first still running | `in_progress` (409, retryable, `retry_after`) | same |
| Replay | "The first answer, stored up to 1 MiB; an operation's start answers the same operation" | CORE L176: replay "After current access checks"; L200 "Current authorization can still refuse access to the retained result" |
| Kept | "24 hours, in the `Idempotency` port's store, with a time-to-live" | OVERRIDDEN BY REVIEW: R08 — "One logical operation per key for its whole retention, uncertain outcomes included" (RV L66); CORE L212 retention "even when work lasts more than 24 hours" |
| Downstream | "The key, or the operation's name, goes to every system that accepts one (§6.1.7)" | DR L96 "Use provider deduplication and reconciliation" |

- Key format (S96 L35): `Idempotency-Key`, request, String, "1–255 characters of `A-Z a-z 0-9 - _ .`, unique per intended change", "Every call whose effect is not `read`". Client: `options.idempotency_key`, "else a new random UUID, kept for every retry of this call" (S97 L93).
- CORE §9 L200: "Persist both the submitted request identity and its effective defaults at admission. A retry of the same submitted request uses the original normalization snapshot; it must not choose a new “latest” date or portfolio default. Transport-only changes do not create a new business execution."
- INT L44 (cloud pattern adaptation): "Replace method/path/raw-body identity with caller-scoped canonical business requests; activate by semantic execution needs; retain uncertain effects instead of dropping keys on 5xx or response overflow".
- AMBIGUOUS: the conflict fingerprint input. Readings: (a) JCS of the submitted request (spec S67 L199); (b) the normalised request (CORE L198). CORE L200 says persist both. Reading: compare on the submitted request identity (so a later default change does not raise a conflict) and replay the stored normalisation. Write the case down.
- AMBIGUOUS: the wire for a metered read's key. §9 asks for `Idempotency-Key` only on non-`read` calls (S96 L35, L100). Readings: (a) clients also send it on reads (a protocol change); (b) the gateway derives an identity from the caller scope and canonical request. No source decides this. Not needed for the journey unless one of its reads is metered.
- AMBIGUOUS: "environment" in the scope. A deployment serves one environment (`not_in_environment` exists), so it is a constant per deployment. Tenant: see §1.5.
- AG L15: codes are `idempotency_conflict` 409 and `precondition_failed` 412 (these win over the pattern library).
- Atlas-Deadline and other transport options must not create a new business execution (CORE L200 "Transport-only changes").

### 4.2 Operations (S67 §6.7.7 L178-191) — IN SCOPE where the journey needs durable work (CORE §12.3 L238); the worker deployable only "where durable work needs one" (RV L13)

| Part | Requirement (copied) |
|---|---|
| The document | `operation.schema.json`; states lowercase (§9.3) |
| Start | "Idempotent by key: a repeat returns the same operation (§4.1)" |
| The worker | "a running operation carries its replica's lease, 30 s, renewed every 10 s, with its progress checkpointed in its record" |
| Pools | "Bounded, per service and overall (`kit/conc`); a full pool leaves the operation `pending`, never a goroutine waiting" |
| Deadline | "The interaction's `deadline.max`; past it the work is cancelled and the operation `failed` with `deadline_exceeded`" |
| Cancel | "`POST <Operation-Location>:cancel`; cooperative: forwarded to every hop (a Snowflake query cancelled by its tag); `cancelled` within one poll interval" |
| Results | "In the record up to 1 MiB; beyond it, `result_location`" |
| Owner | "Checked on `get`, `list` and `cancel`: a caller sees only their own" |
| Polling | "`ETag` and `304` on `get`; `Retry-After` from the poll interval" |
| Retention | "24 hours after it ends (§4.1)" |

- States: `pending`, `running`, `succeeded`, `failed`, `cancelled`, `UNKNOWN` (S41 L191).
- ATLAS-MECH-007: "A caller asking for another principal's operation gets `not_found`; a panic in an operation's work becomes its problem `internal`, and the process lives." (S67 L52).
- Start answers `202 Accepted`, `Operation-Location`, `Retry-After` (S41 L198).

### 4.3 Leases, takeover and fencing
- Spec (S61 L250): lease "renewed every 10 seconds and lapses after 30". Takeover: if resumable, "resumes from its last checkpoint, at most three times; otherwise it cancels the upstream work by its tag and ends the operation `failed` with `unavailable`, releasing its idempotency key so the caller's repeat starts afresh." ATLAS-OPERATION-001 (S61 L65): "within one minute another replica resumes it ... or ends it `failed` with `unavailable`". ATLAS-OPERATION-002 (S61 L66): takeover "releases its idempotency key". ATLAS-OPERATION-003 (S61 L67): upstream cancelled by tag (`QUERY_TAG`) before the operation ends.
- OVERRIDDEN BY REVIEW: R08 — ATLAS-OPERATION-002 is withdrawn. RV L66: "One logical operation per key for its whole retention, uncertain outcomes included. Workers are fenced after their lease and reconciled". DR L96: "Retain one logical operation per key throughout its retention, including uncertain outcomes. Use provider deduplication and reconciliation. Fence workers after lease expiry. ... Do not promise exactly-once external effects solely from HTTP deduplication."
- CORE §9 L202: "An uncertain upstream outcome remains recorded and is reconciled before retrying. A timeout or worker crash must not discard the key and authorize a second change. Exactly-once effects are promised only where the upstream mechanism supports them; otherwise the capability describes its reconciliation behavior."
- CORE §11 L220: "do not erase an upstream system's native run identity. An Astronomer run remains an Astronomer run".
- R08 acceptance (DR L98): "Kill the worker after upstream commit but before receipt persistence; retry the original key. It returns/reconciles the original operation and creates no second effect." (also CORE §12.5 L240).
- MISSING: the fencing mechanism. Reading consistent with the store contract (S67 L255 "every write is conditional on the store's own version"): each lease grant carries a monotonic epoch; every operation-record write by a worker is conditional on its epoch; a fenced worker's writes fail. Not stated in any source; write it down as a design choice.
- MISSING: the state name for an uncertain outcome. The states list has no "reconciling" state (S41 L191). Readings: (a) stay `running` until reconciled; (b) `UNKNOWN`. CORE L212 "Unknown outcomes stay pending reconciliation".

### 4.4 Cancellation and deadlines — OVERRIDDEN BY REVIEW: R17
- Spec: "A caller that disconnects or cancels cancels the work downstream." (S41 L202). ATLAS-PROTOCOL-003 (S41 L115). Cancel is cooperative, `cancelled` within one poll interval (S67 L187; S41 L198).
- RV L75: "The start deadline, execution deadline and retention are separate. Cancellation is reported as requested or confirmed" (step 3).
- DR L144: "Separate start-request deadline, execution deadline and operation retention. Report requested versus confirmed cancellation; preserve uncertain committed effects."
- CORE §10 L210: "An HTTP disconnect during startup does not mean a successfully admitted durable operation was cancelled. Clients can recover the operation using the same idempotency key." L212: "The operation reaches `cancelled` only when the provider knows work stopped; an already committed effect is reported honestly. Unknown outcomes stay pending reconciliation. Retention must cover the active operation and the promised retry/recovery window, even when work lasts more than 24 hours."
- MISSING: the wire for "requested versus confirmed" cancellation (no member or state named). Readings: (a) a `cancel_requested` flag/condition on the operation while `state` stays `running`; (b) a new state. Adding a state is a protocol change; (a) is additive.
- Deadlines, wire: `Atlas-Deadline` — request, Date, "The absolute deadline, in seconds since the epoch", every interaction call (S96 L34); example `Atlas-Deadline: @1793318400` (S96 L58); whole seconds, client rounds down (REF QUESTIONS Q-HDR-2). Client: `options.deadline`, "else now plus the interaction's `deadline.default`, never beyond `deadline.max`" (S97 L93).
- Edge: outer deadline "`Atlas-Deadline` (an RFC 9651 Date), capped by the largest interaction maximum; narrowed by the pipeline after step 2 (§9.7.4)" (S67 L110). Step 2 "narrows the deadline" (S67 L121). Step 6 signs the deadline into the service token (S67 L125).
- "Each hop subtracts its own overhead before passing it on" (S41 L202). AG L44: "Every outbound call takes the context and is bounded by the call's deadline".
- A caller that went away "is logged as 499 and answered nothing" (S67 L130). Under R17 a dropped synchronous stream still cancels its own query; a dropped start of a durable operation does not cancel the operation.

---

## 5. Audit — OVERRIDDEN BY REVIEW: R09

### 5.1 Spec (S62 §6.2.8 L144-155; ATLAS-ACCESS-004 L56)
- "Every call, allowed or refused, leaves one record (the `audit_record` of §9.3.2)." (S62 L146).
- Content (S62 L146): "the address, the caller, the client and edge, each hop; the decision and its reason; the writer and expected fingerprints, the governance revision; the limits it reserved and settled; the execution, rows, bytes and duration; the source's own record of the work (query id, warehouse time, bytes scanned, cost, whether the source's result cache answered), copied in at settlement; the data version the answer was computed from; the outcome and, for actions, the receipt."
- "The request itself is recorded as a hash of its canonical form unless its capability's profile says otherwise; results are never recorded." (S62 L148).
- ATLAS-ACCESS-004: "exactly one audit record, durable before the call's answer ends, in the immutable store, and readable in Log Analytics within minutes" (S62 L56).
- ATLAS-MECH-003: "every refused call's audit record names the step that refused it" (S67 L48).
- Written by step 8 "never by a handler", through `AuditSink` (S67 L285). "Audit: Not telemetry" (S67 L285).
- Storage (S62 L152-153): one JSON line appended to an append blob in an "**immutable (WORM) container**: one blob per copy of the gateway per hour"; group commit; then shipped to "a **Log Analytics table** through the Logs Ingestion API, retried, resuming from the blobs after an outage"; reads "de-duplicated by `request_id`"; "An outage of Log Analytics never slows or fails a call." Retention and access apart from operational logs (S62 L155). **DEFERRED** (real Azure, RV L33). **IN SCOPE:** the `AuditSink` port and a durable fake.

### 5.2 `audit_record` members (REF `schemas/builtins.schema.json` `$defs/audit_record`, "One call's record (§6.2.8)")
Required: `request_id`, `time`, `address`, `caller`, `decision`, `outcome`.

| Member | Type |
|---|---|
| `request_id` | string, minLength 1 |
| `time` | string, `date-time` |
| `address` | string, format `address` |
| `caller` | `{principal, kind: person/workload/UNKNOWN, name}` |
| `client` | `{name, version, protocol: ^atlas/[0-9]+\.[0-9]+$}` |
| `edge` | string |
| `hops` | array of objects |
| `decision` | `{result: allowed/refused/UNKNOWN, reason, code: ^[a-z][a-z0-9_]*$}` |
| `profiles` | `{writer, expected}`, each `exact-id` or null |
| `governance_revision` | string |
| `limits` | object, "What it reserved and settled." |
| `execution` | `gateway`/`handoff`/`client`/`UNKNOWN` |
| `rows`, `bytes`, `duration_ms` | integer ≥0 |
| `source` | object, "The source's own record, copied at settlement." |
| `data_version` | string |
| `outcome` | `{status: 100-599, code}` |
| `receipt` | object |
| `request_hash` | `^[0-9a-f]{64}$`, "SHA-256 of the request's canonical form (RFC 8785)." |

Read through `atlas.audit:fetch` (`Gateway.Admin`): `audit_request` `{day (date, required), before, page_size 1–1000 default 200}` (REF builtins L928-955); answered as pages (QUESTIONS Q-BI-4).
Degraded-mode marker (cross-check): `degraded: ["limits_local"]` (S61 L264). It is not in the reference schema (not checked further).

### 5.3 The review's model
- RV L17: "An invocation history of immutable admission, outcome and settlement facts". RV L67: "Immutable admission, outcome and settlement facts, joined for reading" (step 3).
- DR L104: "One logical invocation history, represented by immutable admission, outcome and settlement facts. Present a joined summary when people want one row. Specify the acknowledgement point and recovery when an effect succeeds but recording completion fails."
- CORE §11 L226: "durable admission or refusal, outcome and settlement facts correlated by request, attempt and operation id. Failed authentication is recorded without trusting the claimed identity or retaining credentials. A summary can present one row per invocation. This accommodates streaming and delayed settlement without rewriting an immutable “final” record after it was returned."
- CORE §8 L182: "Each attempt, including a replay, refusal or failed authentication, leaves an audit fact."
- R09 acceptance (DR L106): "Crash at each boundary. The history preserves what is known, correlates later settlement and exposes an unresolved outcome rather than inventing a completed one."

When each fact is written (from CORE §8 L170-182):

| Fact | When | Source |
|---|---|---|
| Refusal (incl. failed authentication) | at the refusing step; claimed identity not trusted; no credentials | CORE L182, L226 |
| Admission | durable before dispatch (step 7) | CORE L178 |
| Replay | at the idempotency lookup (step 5) | CORE L182 ("including a replay") |
| Outcome | after the result is returned/streamed or the operation ends (step 9) | CORE L180 |
| Settlement | when usage is known, "including later source reconciliation" (step 9; handoff later) | CORE L180; S61 L104 |

- Correlation ids: request id, attempt id, operation id (CORE L226). MISSING: what "attempt id" is and its wire (no header named). Reading: one per HTTP request; `request_id` stays the invocation key. AMBIGUOUS: whether a client retry with the same `Idempotency-Key` is a new attempt of the same invocation (reading: yes; same key = same logical execution, CORE L198).
- MISSING: fact schemas. `audit_record` is the single-record shape; reading: keep `audit_record` as the joined summary view and define three fact records that share `request_id`. The "acknowledgement point" must be specified (DR L104).
- ATLAS-ACCESS-004 "exactly one audit record, durable before the call's answer ends" is replaced: the admission (or refusal) fact is durable before dispatch/answer; outcome and settlement follow (R09).

### 5.4 Redaction
- ATLAS-ACCESS-008: "No token appears in any log, trace or audit record (a test searching every sink for token prefixes)." (S62 L60).
- ATLAS-MECH-014: no log, span or metric contains "a token, a key, a request body or a query string planted in it" (S67 L59).
- Telemetry redaction "In one place, before anything is written: no token, key, body or query string" (S67 L282).
- AG L47: "No token, key, body or query string in any log, span, metric or audit record".
- Request recorded only as a hash (`request_hash`, S62 L148). Results never recorded (S62 L148).
- ATLAS-REACH-003: no OBO token in any answer, log or trace (S63 L63).
- An identity header is recorded (ATLAS-ACCESS-007). AMBIGUOUS: record its value or only its presence. Reading: the claimed identity value is not a credential, so it can be recorded; any bearer-looking value must be redacted.
- Access log: "the caller's hash" (S67 L106), not the principal.

---

## 6. Reaching systems (S63)

### 6.1 Patterns (S63 §6.3.1 L68-74)

| Pattern | Profile says | Used when |
|---|---|---|
| **Run** | `execution: gateway` | "The credential must stay (vendor keys); every call is metered or reshaped; the answer is small or streamed" |
| **Hand off** | `execution: handoff`; a `transfer` delivery | "Results or files of any size, where a link can be narrowed to one object" |
| **Broker** | `execution: client` (a ticket); an `amqp` binding (events) | "The system can check the client itself, per message, as the person" |

First build: **Run (streamed) IN SCOPE.** Hand off and broker **DEFERRED** (RV L15, L33; DR R10 L114 "Keep client SQL tickets deferred"; CORE §12.6 L241 "Direct Snowflake chunks and Service Bus remain separately gated proofs").

### 6.2 Identities (S63 §6.3.2 L76-82)

| Identity | What | Narrowed by person? |
|---|---|---|
| **The gateway** | "Its managed identity, or a credential it holds (a vendor key, an Astronomer API token)" | "No: Atlas's grant is the only limit (rule 3)" |
| **A workload** | "A pipeline's or service's own managed identity, when it is the caller" | "Not a person: the workload's own roles" |
| **The person** | "OBO in the gateway (§6.3.5), or the client's own token for the system from the same sign-in" | "Yes: the system's own permissions for that person apply, after Atlas's" |

### 6.3 The register (S63 §6.3.3 L84-96) — Snowflake row
- Pattern now: "Run (streamed); hand off (result files); broker (a ticket ...)". As whom now: "The gateway's service identity". Target: "Every connection as the person, by OBO". Ticket credential: "**the gateway's own token, with every role of its identity** (`SESSION:ROLE-ANY`)" — "**Ticket: no, a listed exception**". 92 roles in Development (S63 L96).
- Shared service credential: the first build's source (DuckDB fake for Snowflake) is reached as the gateway's service identity → R05 data scope applies (§2.4). Rule 3 (S63 L58): capabilities of a system reached as the gateway "are granted by name only, never by a family grant or `everyone`, and the scope of the gateway's identity there is recorded beside the system." AMBIGUOUS: rule 3 would forbid `portfolio.*` family grants for the journey while Snowflake runs as the service identity. Readings: (a) apply rule 3 literally: journey capabilities granted by exact name only; (b) R05's typed scope rules narrow by person, so rule 3's premise ("cannot narrow by person") no longer holds. The review does not say. Reading (a) is the safe default.
- Other systems (Azure Resource Graph, Astronomer, Service Bus, Blob, SharePoint, vendors): **DEFERRED** (not in the journey).

### 6.4 The credential rule (S63 L56)
"A credential that leaves the gateway lets its holder do nothing they could not do through the gateway at that moment. It is narrowed by identity (the person's own wherever possible, never the gateway's), scope (one object or entity), right (only the granted interaction), time (minutes, never longer than its operation) and revocation (a stated bound)." ATLAS-REACH-001/002/004 (S63 L61-64). R10: each handover records "its real target, rights, identity binding, expiry, renewal and revocation" (RV L68). First build: no credential leaves the gateway (no handoff) — **DEFERRED**.

### 6.5 The handover document (S63 §6.3.4 L98-113) — DEFERRED
Members: `kind` (`download`, `upload`, `connect`, `ticket`); `protocol` (`azure-blob`, `http-range`, `graph-upload-session`, `amqp`, `snowflake`); `target`; `auth` (`sas-url`, `preauthenticated-url`, `entra-scope`, `token`); `rights`, `expires`; `chunking`; `renew`, `commit`; `planned` ("For a ticket: the statement, its writer schema and its query tag").

### 6.6 The Snowflake connection, query tags, timeouts, cancellation
- Driver: `gosnowflake` "with `WithAsyncMode`, `WithArrowBatches` and `WithHigherPrecision` (exact decimals): Arrow batches, never rows" (S221 L126); "Few chunk-prefetch threads" (S221 L127). Adapter `adapter/snowflake` (on `gosnowflake`) (S64 L78). Real connection **DEFERRED** (RV L33); the DuckDB fake through the same port **IN SCOPE** (RV L21; S64 L83).
- Role: "the connection's role can only read" (S64 L84); "a read-only role sits underneath" (S64 L11).
- Query tag: "A planned statement carries a tag the source records (for Snowflake, `QUERY_TAG`)" (S02-2 L45, cross-check). The key or operation's name goes downstream: "a Snowflake `QUERY_TAG` checked before a statement is run again" (S61 L187; S67 L204). Used for settlement ("By `QUERY_TAG`", S221 L106), cancellation ("a Snowflake query cancelled by its tag", S67 L187; ATLAS-OPERATION-003 S61 L67), and resume after takeover ("a Snowflake query found again by its tag", S61 L250). MISSING: the tag's format/content. Reading: it carries the request id and the idempotency key or operation name (S61 L187).
- Statement timeouts: MISSING in all assigned files and the spec (grep for `STATEMENT_TIMEOUT`/"statement timeout" finds nothing). Sources that bound it: `execute(plan, deadline)` (S64 L26, L68); AG L44 every outbound call bounded by the call's deadline; operation deadline `deadline.max` (S67 L186). Reading: set the statement timeout from the remaining deadline. Write it down as a choice.
- Cancellation: cooperative, by tag, "forwarded to every hop" (S67 L187). INT L69: "Let the Snowflake adapter own driver execution, source estimates, cancellation and returned Arrow/type checks."
- Retries: only in the adapter (S67 L152); see §8.
- Settlement: `settle(query_id)` "Rows, bytes and cost, from the engine's own record" (S64 L72).

---

## 7. Data sources (S64)

### 7.1 The `DataSource` port (S64 §6.4.1 L59-72)

| Operation | Does (copied) | First build |
|---|---|---|
| `capabilities()` | "native Arrow results, handoff (and its chunk format version), export, estimates, client execution and its driver kind" | IN SCOPE |
| `plan(request, writer_schema)` | "Renders the request as the writer contract: one statement, every column aliased to its contract name and cast to its contract type, filters and limits pushed down; checks it with the read-only guard" | IN SCOPE |
| `explain(plan)` | "The plan as it would run (the statement) and its estimated size" | AMBIGUOUS: in scope only if a journey profile declares `explain` (CORE §5 L97 "No fake `explain`") |
| `execute(plan, deadline)` | "A stream of Arrow record batches **already in the writer schema**, with the engine's query id" | IN SCOPE |
| `handoff(plan)` | "Where supported" | DEFERRED |
| `export(plan, format, destination)` | "Writes the result as files and returns their links" | DEFERRED |
| `resume(query_id, from_batch)` | "Where supported: the rest of a stream, from the engine's persisted result" | AMBIGUOUS: incomplete streams are in acceptance #5 (RV L50), resume is not named; CORE L206: "Resuming must not silently rerun a changing query" |
| `settle(query_id)` | "Rows, bytes and cost, from the engine's own record" | IN SCOPE (audit settlement fact) |

Rules (S64 L52-55): services reach engines only through the port; "a batch that is not [in the writer schema] is a fault of the adapter"; ATLAS-SOURCE-001: battery passes against every adapter and "its profile's fingerprint is identical under each". R21 (RV L79): business lineage and method are distinguished from the execution engine.

### 7.2 The SQL library and the read-only guard
- Spec (S64 L83): the Go SQL library "builds each plan as one typed syntax tree and renders it in the engine's dialect"; "Snowflake is the engine's dialect; DuckDB is rendered for the hermetic `DataSource` fake". It is `sqlglot` (INT L14; D28 S64 L95). The tested local `sqlglot` module supplies it.
- Guard (S64 L84): "Every statement is parsed by the library's Snowflake parser before it runs, and refused unless it is one read-only query over the objects its capability may read (`snowflake.query`: the registered schemas). The guard is never the only line: the connection's role can only read. The guard's cases are language-neutral vectors (accepted and refused statements), generated in `tools/spec/`."
- INT planned-query path (INT L64-69): (1) validate the request and authorised data scope before planning; (2) "Build a typed AST from reviewed source objects and the capability's declared columns and filter operators. Bind scalar user values through the driver; never construct SQL by string concatenation."; (3) qualify names; walk "every relevant nested scope, source and function against Atlas's allowlists"; (4) "Generate strictly in the target dialect and validate the generated statement again"; (5) return "an immutable plan containing SQL, parameter bindings, referenced objects, output contract, request identity and provenance. Both `explain` and execution use this compiler path"; (6) the adapter owns execution, estimates, cancellation, Arrow/type checks.
- INT rules (L75-82): require exactly one executable statement (not `ParseOneOpts`'s first); reject `exp.Command`, raw and unknown nodes; unsupported generation must fail, "and test that no driver call occurs on failure"; strict qualification ("Qualification is not resource authorization"); a SELECT root is not enough (inspect CTEs, nested queries, functions, identifiers, sources); bound SQL input size, AST depth/nodes and compilation concurrency (the parse API takes no cancellation context).
- Views: S64 names "the objects its capability may read (`snowflake.query`: the registered schemas)" (L84) and INT L65 "reviewed source objects". MISSING: a rule that capabilities read only views. INT L86 mentions "secured views" as one way to enforce scope for raw SQL.

### 7.3 Statement, pushdown, typing
- "one statement, every column aliased to its contract name and cast to its contract type, filters and limits pushed down" (S64 L67).
- S221 L131: "Everything that can be is in the SQL: columns, filters, limits, order. Only requested columns ever move."
- S221 L135: "every column aliased to its contract name and cast to its contract type, every timestamp converted to UTC. So in every execution Snowflake itself produces the writer schema".
- Nothing about an engine leaves its adapter except `snowflake.query`, `snowflake.schema` and a handoff's chunk format (S64 L89).

### 7.4 Types, decimals, timestamps (S221 §2.2.1.6 L139-150, cross-check; not in S64)

| Snowflake | Atlas |
|---|---|
| `NUMBER(p,0)`, `p ≤ 18` | `integer` (`Int64`) |
| `NUMBER(p,0)`, `p > 18` | `Decimal(p,0)` |
| `NUMBER(p,s)`, `s > 0` | `Decimal(p,s)` (`Decimal128`), or `number` where the profile declares a unit (`Bp`, `Fraction`); "Exact decimals must be requested from the driver" |
| `FLOAT` | `number` (`Float64`) |
| `VARCHAR` | `string` |
| `BOOLEAN`, `DATE`, `BINARY` | `boolean`, `date`, `bytes` |
| `TIMESTAMP_TZ`, `TIMESTAMP_LTZ` | `date-time` (UTC, microseconds) |
| `TIMESTAMP_NTZ` | `date-time` "only where the profile states the zone it is in; converted to UTC in the SQL"; "Never assumed" |
| `TIME` | `string` in `atlas/1.0` |
| `VARIANT`, `OBJECT`, `ARRAY` | declared `object`/`array` schema, or a JSON `string`; "Never untyped in an official capability" |

- ATLAS-SNOWFLAKE-004: "No decimal reaches a client as a float unless its profile declares a `number`." (S221 L57).
- CORE §6 L134: "Clients preserve exact decimals, large integers, timestamps and nulls. ... They must not silently turn an exact amount into a binary floating-point value."
- AMBIGUOUS: S221 L143 allows `number` for a `NUMBER(p,s)` with a unit (`Bp`, `Fraction`) — lossy. For holdings `weight`, owners decide (RV L97). The fixture must state it.
- DuckDB fake: INT L95 "DuckDB fixtures agree with the intended logical results; real Snowflake tests cover decimals, timestamps, nulls, identifiers, functions". The DuckDB type mapping is MISSING from the spec.

---

## 8. §6.7's mechanisms, with every value

Rules (S67 L42-44): one home per mechanism; `kit` holds no Atlas decision; copied patterns keep the MIT notice in `THIRD_PARTY_NOTICES` and their tests. AG L9-15: the pattern library loses to the spec in six places (POST per binding; desired fields at the top; lowercase states with `UNKNOWN`; registration in one list; "authentication, limits and idempotency are pipeline steps, not middleware"; `idempotency_conflict` 409 and `precondition_failed` 412). AG L39: layers from `layers.toml` (R24).

### 8.1 Package homes (S67 §6.7.1 L76-94)
`kit/problem` (apierror), `kit/serve` (lifecycle, httpserver, health, middleware, telemetry), `kit/jwt` (auth), `kit/retry` (resilience), `kit/reconcile`, `kit/ops` (lro), `kit/idem` (idempotency), `kit/page` (pagination), `kit/hot` (config), `kit/cache`, `kit/conc` (concurrency), `kit/limit` (ratelimit), `kit/resource`, `core/registry`, `cmd/atlas-new`. Not used in `atlas/1.0`: `proxy` and `lb` (until a service host, O21), `sse` (D41), `session` (tokens never in a cookie) (S67 L94).
- ATLAS-MECH-004: "No service imports `kit/jwt`, `kit/limit` or `kit/idem`, and only adapters import `kit/retry`" (S67 L49).
- INT L47: the pattern library's retry token budget is not "the spec's exact 10%-per-minute rule".

### 8.2 Edge middleware, outermost first (S67 §6.7.2 L96-112; ATLAS-MECH-001: one list, order tested) — IN SCOPE

| # | Middleware | Requirement (copied) |
|---|---|---|
| 1 | Recover | "A panic answers `internal`; after streaming began, the end line carries it" |
| 2 | Request id | "`X-Request-Id` on every answer, errors and `202` included (§9.6.3)" |
| 3 | Real client address | "From `X-Azure-ClientIP` only when the peer is in the ingress's ranges" |
| 4 | Trace context | "Continue the caller's `traceparent` (§6.1.12)" |
| 5 | Access log | "Request id, trace id, address, status, bytes, duration, the caller's hash; probes skipped" |
| 6 | Request metrics | "Rate, errors, duration per address" |
| 7 | Draining | "While shutting down: `unavailable`, `Retry-After: 2`, `Connection: close`" |
| 8 | Body size | "1 MiB (uploads go to transfer links); the bootstrap and probes exempt" |
| 9 | Outer deadline | "`Atlas-Deadline` (an RFC 9651 Date), capped by the largest interaction maximum; narrowed by the pipeline after step 2 (§9.7.4)" |
| 10 | Pre-auth limit | "Per client address, protecting token verification from unauthenticated floods; `limit_exceeded`" |
| 11 | In flight | "One limit per replica; a stream counts by connection; over it, `unavailable` with `Retry-After`" |

- ATLAS-MECH-002: "A request body over 1 MiB is refused with `invalid_request` before step 1 runs." The worked example: "`MaxBytes` refuses a 3 MiB body as `invalid_request` before any token is read" (S67 L31).
- MISSING: numeric values for the pre-auth limit and the per-replica in-flight limit.
- Problem rules (S67 L130): built by `kit/problem` only; body of §9.3; `Atlas-Error-Code` header; "causes kept for the log and never in the body"; `Retry-After` in seconds; a problem after streaming began goes in the end line, "never as a status"; a departed caller is logged as 499. AG L43: never `http.Error`.
- CORS (S62 L165): "Only the Nautilus applications' origins; tokens never in a cookie; no wildcard CORS".

### 8.3 Retries, breakers, bulkheads (S67 §6.7.5 L147-156)

| Mechanism | Value (copied) |
|---|---|
| Client retries | §9.7.7 |
| Where the gateway retries | "At one hop only, nearest the upstream: in the adapter (the vendor client, the Snowflake adapter); never in the pipeline or an edge" |
| Retry budget | "One per dependency: retries at most one in ten calls to it in a minute (§4.1)" |
| Breaker | "One per service and one per upstream: open after five consecutive failures within 30 s; one probe after 15 s; only the upstream's own failures count; open sets the capabilities `unavailable`, closed sets them `available`, within five seconds (§6.1.8)" |
| Bulkhead | "Each upstream its own connection pool, in-flight limit and bounded queue (twice its concurrency); never shared between a warehouse and a vendor" |
| Fail fast | "A full bulkhead answers at once (`limit_exceeded` or `unavailable` with `Retry-After`), naming the upstream in `detail`" |

- ATLAS-MECH-006: "Of 1,000 calls to a dependency with half failing, retries never exceed one in ten calls in any minute; a caller's fault (`invalid_request`, `not_found`) never counts toward a breaker." (S67 L51).
- AG L46: "Retries only of retryable, replayable work, at one hop, within a budget". R08: a metered or changing call is retried only when replay is safe (DR L96).
- MISSING: the adapter's backoff schedule for upstream retries. The worked example retries a vendor `503` "once, within the budget" (S67 L31). Controllers' backoff is in 8.4.

### 8.4 Controllers (S67 §6.7.6 L158-176) — controllers for events/resources DEFERRED; the operations controller IN SCOPE if the journey uses operations

| Part | Value (copied) |
|---|---|
| Queue | "Keys, not events ... A key is never processed by two workers at once; a key added while it is processed is processed again after" |
| Sync | level-triggered |
| Idempotent | "status is compared before it is written" |
| Errors | "per-key backoff from 50 ms doubling to 5 minutes, under an overall 10 per second (burst 100); a known wait (`Requeue after`) is not a failure" |
| Resync | "10 minutes unless 6.1.13 says otherwise" |
| Status | own path; `observed_generation`; conditions as §9.3.4 |
| Deletion | finalizer holds `deleting` |
| Ownership | outside objects carry the owner's name |
| One leader | "by a lease document in Cosmos DB (15 s, renewed every 5 s); another takes over when it lapses" |
| Panics | "a failed sync, retried with backoff" |
| Observed | "queue depth, sync duration, retries, and keys failing for over an hour (an alert)" |

ATLAS-RECONCILE-001..006 (S67 L65-70): -006 "killing that one moves it to the other within 20 seconds". Controller resyncs (S61 L240-245, cross-check): `catalogue` 1 minute; `event-entities` 10 minutes; `event-rights` 5 minutes; `operations` 30 seconds; `settlement` 5 minutes; `publications` 10 minutes.

### 8.5 Operations and idempotency
See §4.2 (operations: lease 30 s / 10 s, results 1 MiB, retention 24 hours → R08/R17) and §4.1 (idempotency: SHA-256 of JCS, 409s, 1 MiB replay, 24 hours → R08).

### 8.6 Paging (S67 §6.7.9 L208) — IN SCOPE (`portfolio.list`, `portfolio.search` if paged)
"Cursors are opaque and signed (HMAC, key from Key Vault; the previous key accepted for one lifetime after rotation), bound to the request without its cursor and the principal, hold the store's own continuation, and live one hour. Pages are `page.schema.json`: `items`, and `next_cursor`, `null` at the end. Page sizes are `builtins.schema.json`'s."
ATLAS-MECH-009: "A cursor used by another principal, with another request, or after an hour is `invalid_request`, target `/cursor`." (S67 L54). Key Vault is DEFERRED; key from settings (AG L42).

### 8.7 The snapshot (S67 §6.7.10 L210-220) — IN SCOPE
"One immutable value, swapped atomically (`kit/hot`), read once per call". Holds: "Every served profile by `exact-id`; governance with its revision; house rules with their revision; switches; admitted services and their bindings". Built by the catalogue controller "from Cosmos DB's change feed, never by polling". Checked before publish: every profile lints; every binding names an admitted service; every grant and switch names an existing capability, interaction or namespace; every house rule's capabilities exist; "the revision is newer than the one serving"; a failing snapshot is refused, reported in readiness and the audit, previous keeps serving. Saved to Blob storage as last-known-good with revision and SHA-256. "The catalogue's ETag is its revision; a governance change moves the governance revision, never a fingerprint". ATLAS-MECH-010 (S67 L55): the request path never reads profiles, governance, house rules or switches from the store after start. Cross-check: a replica waits up to 30 seconds for the live snapshot, then loads last-known-good (S61 §6.1.14).

### 8.8 Caches (S67 §6.7.11 L222-231)

| Where | What | For |
|---|---|---|
| The client | A profile by `exact-id`; the catalogue | "On disk forever; one hour (§9.7.5)" |
| `adapter/entra` | JWKS; group overage | "Until an unknown `kid`; 10 minutes" |
| `core/access` | Resolved grants per principal | "Until the change feed clears them" |
| Never | Results | "Until O60 is decided" |

"Concurrent misses for one key make one fetch (single flight)." ATLAS-MECH-011: "a grant change clears every copy's cached grants within 60 seconds" (S67 L56).
- OVERRIDDEN BY REVIEW: R01 — the profile is cached by contract fingerprint, apart from the document revision; describe caller state uncached; "Reconcile the catalogue's one-hour client cache with the HTTP `no-store` rule" (DR L36; RV L59).
- OVERRIDDEN BY REVIEW: R13 — "Separate freshness bounds for policy, groups, connections and links. A switch behind a failed control channel promises no immediate revocation" (RV L71). DR L140: the spec's values conflict: degraded-mode policy 60 minutes (S61 L268), grants 60 seconds (ATLAS-MECH-011), groups 10 minutes (S67 L142). CORE §11 L222: "Bound the staleness of authorization separately, and choose explicitly which calls may continue during a control-store outage. A mode using stale policy cannot simultaneously guarantee immediate revocation." MISSING: the new values for each bound. Reading: keep the spec values as the declared bounds (policy ≤ 60 minutes in degraded mode, grants ≤ 60 seconds, groups ≤ 10 minutes) and state each one separately. Do not claim an immediate kill switch while the control store is down.

### 8.9 Concurrency (S67 §6.7.12 L233-240)
Client handoff: "Eight chunk downloads at a time"; client transfers: "Eight parts at a time, each at most 8 MiB"; operation workers per 6.7.7; every goroutine via `kit/conc`'s `Go` (ATLAS-MECH-012; AG L41).

### 8.10 Limits (S67 §6.7.13 L244)
See §3.1. The R07 change applies (§3.2).

### 8.11 The resource store (S67 §6.7.14 L246-258) — DEFERRED (resources after the journey, RV R14 L72)
Create fails on a name clash, same key returns first result; Update by mask, needs the ETag (`precondition_failed` when stale); Generation moves on desired fields/labels, never on status; ETag "Moves with the generation, never on a status write" → OVERRIDDEN BY REVIEW: R14 ("The ETag covers the full representation; `generation` is separate", RV L72); Delete with finalizers sets `metadata.deleting`; server-owned members ignored on input, a mask naming them is `invalid_request`.

### 8.12 Serving, health, draining (S67 §6.7.15 L260-274) — IN SCOPE

| Setting | Value |
|---|---|
| Address | `:8000` |
| Read and write timeouts | None |
| Read-header timeout | 10 s |
| Idle timeout | 5 minutes |
| Header bytes | 64 KiB |
| HTTP/2 cleartext | On |
| Shutdown | "Readiness false, wait 5 s, stop accepting, let running requests finish for 30 s, then close" |

`GET /healthz` (and `/livez`) liveness; `GET /readyz` ready when a snapshot is loaded (live or last-known-good), names each check: snapshot and source, Cosmos DB (2 s), the audit sink; reasons withheld from unauthenticated callers. ATLAS-MECH-016: running requests finish within 30 seconds; new ones get `unavailable` with `Retry-After: 2`. ATLAS-MECH-015: `/readyz` names each check and its result. One process group per deployable: signal handler, server, controllers, settlement loop (S67 L274).

### 8.13 Telemetry (S67 §6.7.16 L276-285)
OpenTelemetry traces/metrics, `log/slog` logs, OTLP to a collector; request id and trace id on every log line and span; redaction in one place; metrics per address and per upstream, saturation per bulkhead, queue depth per controller, bounded labels (ATLAS-MECH-015; AG L48); audit not telemetry.

### 8.14 Modules (S67 §6.7.17 L287-298)
Ids `<namespace>.<name>`, lowercase, duplicates and reserved ids fail at start; registration in one list, never at `init` (AG L40); lifecycle: constructed with no side effects → `Provision` → `Validate` → used → `Cleanup` in reverse order; generations activated together; conformance at start and in CI. ATLAS-MECH-017 (S67 L62).

### 8.15 Generator (S67 §6.7.18 L300-309)
`cmd/atlas-new` (never deployed): `go run ./gateway/cmd/atlas-new capability --pattern P05 creditflow.deals` writes the profile, the service, battery and end-to-end cases named by requirement id, blast-radius row and switch; `TODO`s fail the build. AG L64: `go run ./gateway/cmd/atlas-new capability --pattern <P> <id>`. ATLAS-MECH-018: P01 to P19 build, lint and run in CI. AMBIGUOUS: the full P01–P19 generator vs the narrow journey (RV L11). Reading: generate only the patterns the journey uses.

---

## 9. Open decisions carried from the spec (not settled by the review)
- Execute as the person by default for data capabilities (S62 L177). Recommended: as the person.
- Astronomer `conf` attribution (S62 L178); Astronomer delegated identity (S63 L149, O55).
- Grants per interaction (S62 L179). Recommended: yes.
- Who reviews a loosened limit or approval policy; two people for `irreversible` in Production (S62 L180).
- Snowflake ticket credential (S63 L148, O54); one admin consent request (O42).
- ADBC vs `gosnowflake` (S64 L99).
