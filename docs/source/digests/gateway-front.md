# Digest: inside the gateway, part 1 (structure, front door and services, packages, principles)

Reader: gateway-front. Written 2026-10-31. Sources read in full: listed at the end.

## 0. How to read this digest

### Source aliases (cite as `alias:line`)

| Alias | File |
|---|---|
| `G6` | `~/Documents/ChatGPT/creation/atlas-spec/06-inside-the-gateway.md` (§6) |
| `G61` | `~/Documents/ChatGPT/creation/atlas-spec/06-1-front-door-and-services.md` (§6.1) |
| `G65` | `~/Documents/ChatGPT/creation/atlas-spec/06-5-packages.md` (§6.5) |
| `G66` | `~/Documents/ChatGPT/creation/atlas-spec/06-6-principles.md` (§6.6) |
| `CORE` | `~/Documents/ChatGPT/creation/atlas-spec/review/ATLAS-CORE.md` (the review's proposed core) |
| `REV` | `~/repos/atlas-skeleton/docs/pass-2/REVIEW.md` (governs this build) |
| `AG` | `~/repos/atlas-skeleton/AGENTS.md` |
| `DR` | `~/Documents/ChatGPT/creation/atlas-spec/review/DESIGN-REVIEW.md` (the R findings; consulted, R05–R26 sections read) |
| `G67` | `~/Documents/ChatGPT/creation/atlas-spec/06-7-implementation-patterns.md` (§6.7; consulted for §6.7.2, 6.7.3, 6.7.4, 6.7.5, 6.7.10–6.7.17 only) |
| `G62` | `.../06-2-access-limits-and-audit.md` (§6.2.3 only, lines 91–104) |
| `D93` | `.../09-3-documents.md` (§9.3.2 built-ins lines 43–70, §9.3.3 problem registry) |
| `H96` | `.../09-6-http-binding.md` (§9.6.1–9.6.3, lines 11–60, and line 83) |
| `C97` | `.../09-7-clients.md` (§9.7.4–9.7.5) |
| `CAT` | `.../07-the-catalogue.md` (atlas domain, lines 154–188) |
| `HB` | `.../review/CAPABILITY-HANDBOOK.md` (Atlas, 20 capabilities, lines 204–227) |
| `D26` | `.../02-6-discovery.md` (built-ins, lines 86–136) |

Sources marked "consulted" were read only in the line ranges given, because my sections depend on them; other readers own them in full.

### Markers

- **IN SCOPE**: the first build (the journey of REV:35–42, plus REV steps 2–4) must implement it.
- **DEFERRED**: the first build does not implement it.
- **OVERRIDDEN BY REVIEW (Rnn)**: REV or CORE changes what the spec says.
- **AMBIGUOUS**: the sources conflict, are unclear or say nothing, with each possible reading.

### The governing frame (REV)

- The journey: `portfolio.holdings@v1`; `portfolio.list@v1` and `portfolio.search@v1` (identifier lookup); `data.status@v1` (freshness); then `portfolio.holdings-dates@v1`, added after the clients are built (REV:37–42).
- "One Go deployable, plus a worker where durable work needs one. The provider boundary stays in process and serialisable" (REV:13; CORE:186; R20).
- Handoff, events and Service Bus are deferred (REV:15; R10, R16).
- Conservative evolution (REV:14; R03, R04).
- Where the spec and anything else disagree, the spec wins; §9 is normative for the wire (AG:7). REV governs PLAN.md (REV:5).

---

## 1. The pipeline

### 1.1 Two layers of checks: the edge middleware, then the pipeline

The edge middleware is "transport only: nothing here decides access" (G67:98). The pipeline proper is in `core/pipeline` and "run for every edge" (G67:116). Rule: "Every call through every edge passes steps 1 to 9 of §6.1.2, in that order" (G61:47). AGENTS: "authentication, limits and idempotency are pipeline steps, not middleware" (AG:14).

CORE: "Transport-level size, syntax and timeout checks can reject a malformed request without exposing any capability-specific information; the authorization order governs schema-dependent diagnostics" (CORE:182). That is the middleware's licence.

### 1.2 The edge middleware, outermost first (G67:96–112). IN SCOPE (edge/http)

| # | Middleware | Requirement (exact) | Why here |
|---|---|---|---|
| 1 | Recover | A panic answers `internal`; after streaming began, the end line carries it | Outermost, so nothing escapes it |
| 2 | Request id | `X-Request-Id` on every answer, errors and `202` included (§9.6.3) | Before anything that logs |
| 3 | Real client address | From `X-Azure-ClientIP` only when the peer is in the ingress's ranges | Before the pre-auth limit, which keys on it |
| 4 | Trace context | Continue the caller's `traceparent` (§6.1.12) | Before the access log, which records the trace id |
| 5 | Access log | Request id, trace id, address, status, bytes, duration, the caller's hash; probes skipped | Wraps the writer to see status and bytes |
| 6 | Request metrics | Rate, errors, duration per address | As the access log |
| 7 | Draining | While shutting down: `unavailable`, `Retry-After: 2`, `Connection: close` | Before any work starts |
| 8 | Body size | 1 MiB (uploads go to transfer links); the bootstrap and probes exempt | Before reading the body |
| 9 | Outer deadline | `Atlas-Deadline` (an RFC 9651 Date), capped by the largest interaction maximum; narrowed by the pipeline after step 2 (§9.7.4) | Before any I/O |
| 10 | Pre-auth limit | Per client address, protecting token verification from unauthenticated floods; `limit_exceeded` | Before step 1's work |
| 11 | In flight | One limit per replica; a stream counts by connection; over it, `unavailable` with `Retry-After` | Last, so only admitted work counts |

- AMBIGUOUS: the problem for an oversize body (middleware 8). §9.3.3 has no 413 code and no "too large" code (D93 registry). Readings: (a) `invalid_request` 400 with `target` naming the body; (b) `invalid_document` 400; (c) a new code, which would be a §9 change. None is written.
- AMBIGUOUS: the values of the pre-auth limit (10) and the in-flight limit (11) are not given anywhere I read.
- AMBIGUOUS: whether CORE's "each attempt, including ... failed authentication, leaves an audit fact" (CORE:182) also covers refusals by middleware 7, 8, 10 and 11, which happen before step 1. Readings: (a) yes, every refusal is an audit fact; (b) only refusals from the pipeline onwards are audit facts, and middleware refusals reach only the access log and metrics.

### 1.3 The spec's nine steps (G61:76–104, G67:114–128, G62:91–104)

The spec's diagram (G61:78–102): steps 1–6 run in the FRONT DOOR ("edge + core pipeline; stateless; any number of copies"); step 7 runs in the DOWNSTREAM SERVICE ("per upstream or domain; scaled independently"); steps 8–9 run in the FRONT DOOR again.

Across the route hop the call carries: `subject · address · request id · traceparent · deadline · idempotency key` (G61:99).

| Step | Reads | Decides / does | Refuses with (status from §9.3.3) | Records |
|---|---|---|---|---|
| 1 authenticate | The bearer token (`Authorization: Bearer <access token>`, H96:30) | "who is calling — a validated token only" (G61:82). Verifies the token (§6.7.4) and builds the principal (G67:120). Principal is `oid`, never `sub`; `idtyp: app` means a workload (G67:141) | `unauthenticated` 401 with `WWW-Authenticate: Bearer realm="atlas"`, plus `error="invalid_token"` when expired (G67:143) | Identity headers on the request are "ignored and recorded" (ATLAS-ACCESS-007, G67:145) |
| 2 resolve | The snapshot (G67:121) | "capability@version:interaction, profile, governance and binding" (G61:83). Finds the address in the snapshot and narrows the deadline (G67:121) | `not_found` 404, `not_in_environment` 404, `retired` 410, `upgrade_required` 426, `critical_trait_unsupported` 422 (G67:121) | — |
| 3 authorise | Deny, grants and conditions in the snapshot (resolved grants cached per principal, G67:228) | "deny, grants, conditions — §6.2" (G61:84). Order inside: Deny, then Grant, then Conditions (G62:97–99) | `denied` 403, `not_granted` 403, `approval_required` 403 (G67:122). A confidential capability the caller lacks is `not_found` (G66:80) | — |
| 4 validate | The request body; the current profile | "resolve request, fill defaults, check schema and a write's idempotency key" (G61:85). Resolves the request to the current profile (§9.5), fills defaults, then checks the schema and the profile's invariants; for a write, begins its idempotency record (6.7.8) (G67:123) | `resolution_failed` 422, `invalid_request` 400, `idempotency_conflict` 409, `in_progress` 409 (retryable) (G67:123) | The idempotency record is begun (writes only) |
| 5 limit | Limit counters (Cosmos DB) | "reserve against caller limits and budgets" (G61:86). Reserves every window in one batch (6.7.13); applies the confirmation threshold (G67:124) | `limit_exceeded` 429 (retryable), `budget_exceeded` 403, `confirmation_required` 428 (G67:124) | The reservation |
| 6 route | The binding's admitted service | "to the binding's downstream service with a signed token" (G61:87). Signs the token for the service: address, subject, deadline, request id, idempotency key, trace (G67:125) | `unavailable` 503 (retryable) (G67:125) | — |
| 7 execute | The service's own upstream | "work against the licences and warehouses behind it" (G61:91) | Its declared errors; `upstream_failed` 502 (retryable), `deadline_exceeded` 504 (G67:126) | — |
| 8 settle and audit | The reservation; the outcome | "actual use, the record, any problem" (G61:94). Settles the reservation; writes the audit record (G67:127) | — | The audit record, through `AuditSink`, "written by step 8, never by a handler" (G67:285) |
| 9 respond | The service's answer | "passed on as it arrives" (G61:95). Streams the answer as it arrives (G67:128) | — | — |

Why this order (G62:101): "a step that needs the caller follows authentication; one that needs the profile follows resolution; validation follows authorisation, so a caller without a grant learns nothing about a request's shape; defaults are filled before validation, so the validator sees the final request; limits follow validation, so an invalid request never spends one; and the idempotency check comes before limits, so a replay spends nothing twice."

Also:

- Handoff and export: "A handed-off call ends at step 6 with a handoff document, and an export with transfer links; settlement happens when the source reports what ran" (G61:104). DEFERRED (REV:15).
- Problem construction: "A problem is built by `kit/problem` only: the body of §9.3, `Atlas-Error-Code` in the header, causes kept for the log and never in the body, `Retry-After` in seconds, and a problem after streaming began in the end line, never as a status. A caller that went away is logged as 499 and answered nothing" (G67:130). AG:43 adds: never `http.Error` (atlasvet nohttperror).
- Every step is named and timed, and the step that refuses is recorded (principle 09, G66:65).

AMBIGUOUS (inside the spec): G61:93–96 put "8 settle and audit" before "9 respond", yet respond is "passed on as it arrives". A streamed answer cannot be settled before it is sent. CORE §8 resolves this (below).

### 1.4 CORE §8's order (CORE:168–188). OVERRIDDEN BY REVIEW: this is the order to build

"Every gateway invocation follows the same logical order" (CORE:170):

| # | CORE step (exact) | Spec step it replaces | What changed | R |
|---|---|---|---|---|
| 1 | "Validate identity and establish trusted caller context." | 1 authenticate | Adds trusted caller context: the effective principal, the calling application or workload, the environment and the policy revision (CORE:46) | R05, R26 |
| 2 | "Resolve the declared address to an admitted capability and interaction." | 2 resolve | Same. Routing is current metadata, not lock-fixed (R02) | R02 |
| 3 | "Enforce visibility, deny and grant rules without exposing a confidential contract." | 3 authorise (deny, grant) | Approval moves out to CORE step 6 | R06 |
| 4 | "Validate and normalise the request, then enforce its resource and data scope." | 4 validate, plus the spec's deferred "conditions on the resource" (G62:99) | Resource and data scope enforced here, from the first shared-credential source. R04: validate under its own contract **before** any transformation; unknown request fields refused, never dropped | R04, R05 |
| 5 | "Look up any caller-scoped idempotency key. After current access checks, replay or reconcile its existing operation without a second dispatch or spending reservation; reject a conflicting request." | Part of 4 (the idempotency record) | Lookup is its own step, after access. A replay re-checks current access and spends nothing | R08 |
| 6 | "For a new execution, check current confirmation and approval evidence, claim its idempotency identity and reserve limits through a recoverable admission transaction." | 5 limit, plus 3's `approval_required` | Confirmation and approval evidence are receipts, not the header (R06). Claim plus reserve is one recoverable admission transaction. Budgets have one authority per metered provider (R07) | R06, R07, R08 |
| 7 | "Record durable admission and dispatch the work through its provider." | 6 route + 7 execute | An admission fact is durable **before** dispatch | R09 |
| 8 | "Return or stream the result, or return the durable operation that owns the work." | 9 respond | Respond comes before outcome and settle | R11, R17 |
| 9 | "Record outcome and settle usage, including later source reconciliation where required." | 8 settle and audit | Outcome and settlement are separate facts, after the response | R09 |

Further CORE §8 rules:

- "Each attempt, including a replay, refusal or failed authentication, leaves an audit fact." Failed authentication is recorded "without trusting the claimed identity or retaining credentials" (CORE:182, 226). OVERRIDDEN BY REVIEW (R09): "one audit record per call" (G6:14; G61:233; G67:285) becomes "Immutable admission, outcome and settlement facts, joined for reading" (REV:67). The facts are correlated "by request, attempt and operation id" (CORE:226).
- "The front door owns common policy. The provider owns business meaning and source work. The source adapter owns credentials, driver behavior and encoding conversions" (CORE:184).
- "Remote providers receive a narrowly scoped invocation credential bound to the approved request; provider authentication is separate from the effective user's identity" (CORE:184).
- "Do not implement an independent entitlement engine in every service. Do supply providers with the enforced scope they need, and retain source-level controls as an additional check" (CORE:188).
- Idempotency scope: "within tenant, principal, environment and capability-interaction scope", bound to "the normalized semantic request and execution-affecting options, not JSON whitespace or a mutable HTTP route"; metered reads with duplicate-charge risk too (CORE:198). Persist the submitted request identity and its effective defaults at admission; a retry uses the original normalisation snapshot (CORE:200).

Spec and review side by side:

| Concern | Spec | Review (build this) |
|---|---|---|
| Order of validate vs transform | "Resolves the request to the current profile (§9.5) and fills defaults, then checks it" (G67:123; G62:100) | R04: validate the original request under its declared contract before transformation, then the transformed one; unknown fields refused (REV:62; DR R04) |
| Idempotency | Begun inside validate, for a write (G67:123) | Lookup after access (step 5), claim inside the admission transaction (step 6). One logical operation per key for its whole retention, uncertain outcomes included (R08, REV:66) |
| Approval | Step 3 refuses `approval_required` (G67:122); conditions (G62:99) | Step 6 checks receipt evidence bound to "the principal, address, normalised request, plan digest, scope, maximum and expiry" (REV:18, 64; R06) |
| Confirmation | Step 5 threshold, `confirmation_required`, repeated with `Atlas-Confirm` (G67:124; D93 registry) | `Atlas-Confirm` is "the client's claim only" (REV:18); a trusted receipt is needed (R06) |
| Resource conditions | "later" (G62:99) | IN SCOPE from the first shared-credential source (R05; CORE:56) |
| Audit | One record at step 8 | Admission (step 7), outcome and settlement (step 9) facts (R09) |
| Settle vs respond | 8 settle, 9 respond | 8 respond, 9 outcome and settle |

### 1.5 First-build scope of each step

| Step (CORE order) | Scope | Note |
|---|---|---|
| 1 identity | IN SCOPE | A hermetic issuer that signs its own tokens and serves its own JWKS, "so the real verifier runs in every test" (G67:145). Two identities with different portfolio scope (REV:30, 48) |
| 2 resolve | IN SCOPE | From the snapshot |
| 3 visibility/deny/grant | IN SCOPE | `not_granted` vs `not_found` for confidential (G66:80) |
| 4 validate + scope | IN SCOPE | R04 and R05 are REV step 2 (REV:62–63) |
| 5 idempotency lookup | IN SCOPE (REV step 3, proved against fakes, REV:31) | AMBIGUOUS whether the journey's capabilities exercise it: `Idempotency-Key` is required only "on every non-`read` call" (H96:100), and the four journey capabilities are reads. CORE:198 extends keys to "metered reads that can incur duplicate charges". Readings: (a) build and test the mechanism against fakes, unused by the journey; (b) the journey uses it if `portfolio.holdings` starts a durable operation |
| 6 confirmation/approval + claim + reserve | IN SCOPE as mechanisms (REV step 3: R06, R07, R08) | AMBIGUOUS whether any journey capability is metered or has a confirmation threshold; the profiles must say (REV:96–97 leave the holdings questions to owners) |
| 7 admission + dispatch | IN SCOPE | In-process provider (R20) |
| 8 respond/stream | IN SCOPE | JSON core; Arrow is an optimisation (R11); explicit completion marker, "EOF alone is not success" (CORE:206) |
| 9 outcome + settle | IN SCOPE (outcome); settlement of handoffs DEFERRED | — |

---

## 2. The front door

### 2.1 What the front door is

- "a front door of edges over one pipeline that decides every call and routes it to downstream services by a signed token" (G61:7).
- "It holds no state that matters and decides access nowhere else" (G6:12).
- ATLAS-GATEWAY-001: "The front door holds no state that matters in its process: killing any copy mid-request loses nothing a retry with the same idempotency key does not recover" (G61:51). IN SCOPE.
- Rules: "Only the front door decides access; only services do domain work; only adapters know a source" (G6:53). "Nothing changes a contract without the gate and registration classifying it" (G6:54).

**Keeping it thin** (G61:120–125):

1. "Stream, never buffer: chunks pass through as they arrive."
2. "The deadline travels with the call; each hop subtracts its overhead; a cancelled caller cancels downstream."
3. "Backpressure: a saturated downstream answers at once (`limit_exceeded`, or 503 with `Retry-After`)."
4. "No domain logic: the front door applies the protocol (envelope, problems, resolution of requests, aliases); the work is downstream."

- OVERRIDDEN BY REVIEW (R17): item 2's "a cancelled caller cancels downstream" holds for a single answer, but not for a durable operation. "An HTTP disconnect during startup does not mean a successfully admitted durable operation was cancelled" (CORE:210). The start deadline, the execution deadline and the retention are separate. Cancellation is reported as requested or confirmed (REV:75).
- ATLAS-GATEWAY-005: "No edge buffers a chunked answer whole; gateway memory stays flat while a result larger than its memory limit streams or is exported" (G61:55). IN SCOPE for streaming; export DEFERRED.

### 2.2 Control plane and data plane (G61:71–74)

- **Control plane** (always the gateway): "the catalogue of profiles, identity, the access decision, limits, audit, tickets and scoped tokens. It carries small messages."
- **Data plane**: "the gateway's own streamed answers, a downstream service in its own resource, or the source and storage directly, through a ticket or a transfer link."
- The test for every new data path is Tailscale's rule: "the control plane carries keys and policy and the data takes the shortest path."
- First build: the gateway streams its own answers (IN SCOPE). Tickets and transfer links are DEFERRED (R10, REV:68).

### 2.3 Edges (G61:106–118)

"Every transport class has its own edge: a thin adapter in front of the same core pipeline (a library), against the same state. HTTP, server-sent events and WebSockets, MCP and events are all edges; none is a separate API" (G61:108).

| Transport class | Edge | Scales on | Needs | Scope |
|---|---|---|---|---|
| Request and response (HTTP, MCP) | `edge` | Requests and latency | Nothing in the process | HTTP IN SCOPE; MCP see below |
| Long-lived streams (SSE, WebSockets; not in `atlas/cli`) | `stream-edge` | Open connections and memory | Authorise at open and again on change; limits on time, messages and bytes; a shared backplane so any copy serves any subscriber; resume tokens and heartbeats; draining on deploy; a ceiling per copy and a bounded buffer per reader ending in a terminal problem | DEFERRED |
| Long-running work | `edge`, then `worker` | Queue length | Operations idempotent and kept in the store | IN SCOPE where the journey needs durable work (REV:13) |
| Events in (`publish`) and out (change feed) | `edge`; `events-publisher` | Message rate; change-feed leases | An idempotency id per event; leases | DEFERRED (REV:15) |
| Handed off and exported | `edge` (issues and settles) | Requests | The bytes are not the gateway's to carry | DEFERRED |

- "Until load says otherwise, the edges run in one deployable with separate worker pools and connection limits per transport class, so streams never starve requests" (G61:118).
- AMBIGUOUS (spec vs §9): G6:63 lists the gateway's bindings as "HTTPS (with SSE)", and §6.1.3 has an SSE `stream-edge`. But "Server-sent events are not part of `atlas/1.0` ... the gateway serves no subscription over HTTP" (H96:83). The binding schema reserves `sse` and `wss` for a later minor version. Resolution by AG:7 (§9 wins): no SSE in the first build. "Streamed answers" are chunked HTTP bodies (NDJSON/Arrow, §9.6.5).
- AMBIGUOUS: the MCP edge (`edge/mcp`, G65:117). The journey names CLI, Python and Node (REV:12) and does not name MCP. Readings: (a) DEFERRED; (b) in scope because §6.1.14 and §2.6 treat MCP as a first-class edge. The REV journey suggests (a).

### 2.4 Authentication (G67:132–145; G61:177; CORE:52)

| Setting | Value (exact, G67:136–143) |
|---|---|
| Issuer | Required; the tenant's v2.0 issuer (and v1 only if the gateway's app registration issues v1 tokens) |
| Audiences | The gateway's application id URI and its client id |
| Algorithms | `RS256` only |
| Keys | The tenant's JWKS from OIDC discovery, fetched at start, refreshed on an unknown `kid` (one fetch for many callers) |
| Leeway | 5 minutes |
| Principal | `oid`, never `sub`; `idtyp: app` means a workload (§6.2.1) |
| Groups | From the token; on overage, from Microsoft Graph, cached for 10 minutes |
| Refusal | `401`, `WWW-Authenticate: Bearer realm="atlas"` (with `error="invalid_token"` when expired), `unauthenticated` |

- The bootstrap `GET /.well-known/atlas` needs no sign-in (H96:14). `Authorization` is required on "Every call except the bootstrap" (H96:30).
- Workloads: "A scheduled job runs under an explicitly authorized workload identity. It does not retain the interactive user's token" (CORE:48).
- Agent vs person: the distinction "comes from trusted authentication context, not an `is_human` parameter supplied by the agent" (CORE:46). AMBIGUOUS: `X-Atlas-Interactive: ?1` (H96:38) is a client-sent header. Under CORE:46 it can only be a claim, never trusted context.
- Credential binding (R26, client side): "Credentials are bound to a trusted gateway, authority, audience, tenant/account and resource scopes. Concurrent refreshes are serialized. An unauthenticated bootstrap cannot direct a client to send an existing token or refresh credential to an untrusted server or authority" (CORE:52; REV:84). IN SCOPE (REV step 4).
- Entra itself is DEFERRED to real systems (REV step 5, "delegated identity"); the hermetic issuer is IN SCOPE.

### 2.5 Request ids and trace context

- `X-Request-Id`: response; "The call's id"; on "Every answer" (H96:45); "errors and `202` included" (G67:103).
- `traceparent`, `tracestate`: both directions; `traceparent` is required "on every call" (H96:40). The gateway continues the caller's `traceparent` (G67:105).
- "Every hop propagates W3C `traceparent` and adds a span carrying the address, the delivery, the fingerprint and the decision; tickets and transfer links carry the trace id, so direct fetches appear in the same trace. Logs are structured, with the trace id and a hash of the caller, never a token" (G61:231).
- The request id travels in the signed token to the service (G61:99; G67:125). Operations and handovers record the request that made them (G67:281).
- AMBIGUOUS: whether the gateway adopts an inbound `X-Request-Id` or always mints its own. §9.6.3 defines it as response-only (H96:45). Readings: (a) always mint (the safe reading under §9); (b) honour a client value. Neither file says which.
- AMBIGUOUS: CORE:226 correlates audit facts by "request, attempt and operation id". An "attempt id" is defined nowhere I read. Readings: (a) a per-HTTP-request id distinct from a logical request id that spans retries (the request id = the idempotency identity); (b) the `X-Request-Id`, with "request" meaning the logical execution.

### 2.6 Deadlines

- Header `Atlas-Deadline`: request; Structured Field type Date; "The absolute deadline, in seconds since the epoch"; required on "Every interaction call" (H96:34). Example: `Atlas-Deadline: @1793318400` (H96:58).
- Client default: "`options.deadline`, else now plus the interaction's `deadline.default`, never beyond `deadline.max`" (C97, §9.7.4 step 7).
- Middleware 9 caps it "by the largest interaction maximum"; the pipeline narrows it after step 2 (G67:110, 121).
- "The deadline travels with the call; each hop subtracts its overhead" (G61:123). "A deadline crosses as an absolute time" (G61:184).
- "Every outbound call takes the context and is bounded by the call's deadline" (AG:44).
- Server timeouts: "Read and write timeouts: None. Streams and transfers; every call is bounded by its deadline instead" (G67:265).
- OVERRIDDEN BY REVIEW (R17): "The start deadline, execution deadline and retention are separate" (REV:75).
- AMBIGUOUS: what the gateway does with a missing or unparseable `Atlas-Deadline`. "A receiver ... treats one that does not parse as its type as absent" (H96:26). Absent is not specified. Readings: (a) apply the interaction's `deadline.default`; (b) refuse with `invalid_request`. Reading (a) is consistent with H96:26.
- AMBIGUOUS: how much overhead each hop subtracts. No number is given.

### 2.7 Body and header limits

| Limit | Value | Source |
|---|---|---|
| Body | 1 MiB; the bootstrap and probes exempt; uploads go to transfer links | G67:109 |
| Header bytes | 64 KiB ("A person's bearer token with many groups") | G67:268 |
| Read-header timeout | 10 s | G67:266 |
| `Idempotency-Key` | 1–255 characters of `A-Z a-z 0-9 - _ .` | H96:35 |
| No unbounded input | "bodies, headers, cursors, keys, buffers, goroutines, maps, queues" | AG:45 |

### 2.8 Routing from binding to address

- "An interaction's URL is its binding's `server`'s `url` (from the bootstrap; no trailing `/`) followed by the binding's `address` (which begins with `/`). A client never constructs an address itself" (H96:15).
- Method: `POST` for every interaction. `GET` only for the bootstrap and for polling an operation (H96:20). "A resource is named by its `name` member in the body; addresses have no templates" (H96:21).
- The binding schema's required members are `protocol`, `server`, `address`, `auth`. `protocol` is one of `https`, `amqp`, `blob`, `snowflake`, `mcp`, `UNKNOWN`. `method` is one of `POST`, `GET`, `UNKNOWN` (`spec/protocol/references/schemas/binding.schema.json`).
- In the front door: step 2 resolves "capability@version:interaction, profile, governance and binding" (G61:83). Step 6 routes "to the binding's downstream service" (G61:87). The snapshot check requires that "every binding names an admitted service" (G67:218).
- OVERRIDDEN BY REVIEW (R02): "A lock pins semantic contracts. Routing is fetched as current metadata. 'No discovery at runtime' means no new business-route selection" (REV:60; CORE:164).
- AMBIGUOUS: how an inbound HTTP path maps back to `capability@version:interaction`. The binding `address` is a gateway-chosen path, and the files I read do not fix its form. Readings: (a) the edge looks the path up in the snapshot's binding table (an exact match from the address path to the interaction); (b) the path encodes the address grammar of §9.1.3. Reading (a) follows from "bindings, not URL templates" (G66:77, D4).

### 2.9 Generations (G65:200–212; G67:287–298)

- "A configuration's modules are loaded as one generation: all provisioned and validated, then activated together; if one fails, the ones already provisioned are cleaned up and the previous generation keeps serving" (G65:212).
- Lifecycle: "Constructed with no side effects; `Provision` (connections, credentials, its own goroutines); `Validate` (fast checks); used; `Cleanup` in reverse order, also when a later module fails" (G67:295).
- Ids: "`<namespace>.<name>`, lowercase; duplicates and reserved ids fail at start" (G67:293).
- ATLAS-PACKAGES-007: "A module failing `Provision` or `Validate` cleans up every module of its generation already provisioned, and the generation serving before keeps serving (or, at start, the deployable never becomes ready)" (G65:59).
- "A service's admission (6.1.6) is this lifecycle, with the profile checks as its validation" (G65:212).
- Listing: "An administrator's health view lists each namespace's modules with their generation and conditions" (G67:298).
- IN SCOPE.

### 2.10 Draining, shutdown and health

- Middleware 7, while shutting down: `unavailable`, `Retry-After: 2`, `Connection: close` (G67:108).
- Shutdown: "Readiness false, wait 5 s, stop accepting, let running requests finish for 30 s, then close" ("Make-before-break on every deploy") (G67:270).
- "Each deployable runs one process group whose first actor to stop stops the rest: the signal handler, the server, the controllers, the settlement loop, and in `events-publisher` the change feed" (G67:274).
- Health endpoints: `GET /healthz` (and `/livez`) is liveness. `GET /readyz` "is ready when a snapshot is loaded (live or last-known-good) and names each check: the snapshot and its source, Cosmos DB (2 s), the audit sink." "A service's health is not readiness: it sets its capabilities' availability (ATLAS-GATEWAY-006). Reasons are withheld from unauthenticated callers" (G67:272).
- Readiness: "The front door is ready when a snapshot is loaded, live or last-known-good (§6.1.14); a service when `describe` succeeds" (G61:201).

| Server setting (G67:262–270) | Value |
|---|---|
| Address | `:8000` |
| Read and write timeouts | None |
| Read-header timeout | 10 s |
| Idle timeout | 5 minutes |
| Header bytes | 64 KiB |
| HTTP/2 cleartext | On ("The ingress terminates TLS") |
| Shutdown | Readiness false, wait 5 s, stop accepting, let running requests finish for 30 s, then close |

- Rollout: "One revision at a time with traffic splitting; profiles and conformance checked before a service reaches an environment; make-before-break: the new revision ready before the old drains" (G61:203). Container Apps rollout is DEFERRED (real infrastructure).
- Version skew: "Service protocol N and N-1; clients below the protocol's minimum get 426" (G61:202). The 426 is `upgrade_required` (D93). The client half is IN SCOPE. The service-protocol N/N-1 half is DEFERRED with remote transport (R20).

---

## 3. Services and providers

### 3.1 What a service is (G61:153–155)

"A downstream service serves the capabilities of one upstream or domain. It runs in the gateway's process or in a service host of its own (§6.5); the front door cannot tell the difference" (G61:155). Services are `svc/...` at L4, "with their profiles embedded in the binary (`embed`)" (G65:115).

### 3.2 The service protocol (G61:165–177)

"between the front door and a service host; in process, the same calls are Go calls on the ports" (G61:165).

| Call | Purpose (exact) | First build |
|---|---|---|
| `describe` | The service's declaration and every profile; called at registration and when the service says it changed | IN SCOPE (in process) |
| `health` | Liveness, readiness, and availability per capability (a suspended warehouse) | IN SCOPE |
| `invoke` | A single answer, with the signed token and the request | IN SCOPE |
| `start`, `get`, `cancel` | An operation | IN SCOPE only where the journey needs durable work (CORE:238) |
| `stream` | A chunked answer or a subscription, with resumption | Chunked answer IN SCOPE (holdings is chunked, HB:233). Subscription DEFERRED |
| `plan` | A handoff document, or an export's links | DEFERRED (handoff/export) |
| `settle` | A handed-off or exported call's actual use, from the source's record | DEFERRED |

- "A service never authenticates a person: it verifies the front door's signed token (signature, audience, address, expiry) and acts. It holds its own credentials to its upstream, never another service's. The front door and service hosts speak protocol versions N and N-1, so they deploy independently" (G61:177).
- ATLAS-GATEWAY-002: "A service refuses a call whose token is unsigned, expired, or scoped to another address" (G61:52).
- Signed token claims (G67:125): address, subject, deadline, request id, idempotency key, trace.
- Spec open points (G61:319–320). Transport: "Recommended: HTTP/2 with Arrow and JSON (one toolchain); gRPC would add a second schema language beside profiles." Token: "Recommended: Entra between services (managed identities), with Atlas's scope (address, subject, deadline) as a signed claim."
- **OVERRIDDEN BY REVIEW (R20; REV:13, 78; CORE:186):** "Six deployables and an HTTP/2 service protocol" becomes "One Go deployable, plus a worker where durable work needs one. The provider boundary stays in process and serialisable." "In-process provider conformance first; remote transport conformance when a provider is first extracted." DR R20: "Preserve serializable boundaries now." The HTTP/2 transport and its N/N-1 skew are DEFERRED.
- AMBIGUOUS: whether an in-process provider still receives and verifies a signed token (GATEWAY-002). Readings: (a) yes: the in-process call carries the same signed invocation credential, so GATEWAY-002 is testable now and extraction changes only transport (CORE:184's "narrowly scoped invocation credential bound to the approved request"); (b) in process, a typed, serialisable invocation record without a signature, with the signature added at extraction (R20: "remote transport/authentication conformance on first extraction"). R20's wording ties authentication conformance to extraction, which favours (b) for the build. Reading (a) is the stricter one and keeps GATEWAY-002 meaningful. Decide and record it.

### 3.3 Staying movable: serialisable ports (G61:179–190; G65:155–157)

"Every downstream service, and every edge, MUST be able to run in a resource of its own at any time without a code change" (G61:181):

1. "Talk only through ports (§6.5)."
2. "Everything crossing a port is serialisable (typed records, Arrow, bytes); no live objects, callbacks or shared handles. A deadline crosses as an absolute time; identity crosses as a signed token; credentials never cross: each runtime fetches its own with its own identity."
3. "No state that matters lives in a process; caches are an optimisation."
4. "Access is decided once, at the front door; a service checks the token and never decides access itself."
5. "Every write is safe to repeat (idempotency keys, ETags); no transaction spans a boundary. The key travels downstream: a service passes the call's idempotency key, or its operation's name, to every system that accepts one (a Snowflake `QUERY_TAG` checked before a statement is run again; a Service Bus `MessageId` with duplicate detection; a vendor's request id; a blob written only if absent), so a repeated step does nothing twice there."
6. "Every runtime has its own health, settings, secrets and identity."

- "Every port is a Go interface in `ports`, taking a `context.Context` where it does I/O, with only serialisable arguments" (G65:157).
- "Movable is tested: every service's conformance batteries run twice, in process and through the service protocol on loopback. A service that passes both can be moved by configuration" (G61:190). Also ATLAS-GATEWAY-003 (G61:53), Rule 3 (G61:49) and the review's Q15 (G66:102). OVERRIDDEN BY REVIEW (R20): in process now; loopback through a remote transport on first extraction.
- ATLAS-GATEWAY-013: "A step repeated after a crash creates nothing twice in a system that accepts a key (a fake per system counting what it received)" (G61:63). IN SCOPE; with R08, the fake counts effects after a crash between upstream commit and receipt persistence (DR R08 acceptance; CORE:240).
- CORE §8 boundaries apply here too: the provider owns meaning and source work, and the adapter owns credentials, driver and encodings (CORE:184).

### 3.4 Registration in one list, and admission (G61:157–163; G65:159–169)

**Declaration.** "a service declares its domain, its version, where it is served (in process, or a service host's address and trust material), its health, and its profiles" (G61:157).

**Admission conditions** (G61:159–161). The gateway admits a service only when:

1. "its profiles pass the lint and the compatibility check against what is already served (§3.3, §3.3.12)". OVERRIDDEN BY REVIEW (R03): the gate uses the smaller compatibility model (REV:61, 91).
2. "it claims only its own domain's ids: no service may serve another's" (ATLAS-GATEWAY-007, G61:57).
3. "it passes its family conformance batteries (in its own CI, and for a service host before it is placed in an environment)".

**Atomicity.** "Admission is atomic: all of a service's capabilities are admitted, or none, and the reason is reported in the gateway's readiness and its audit" (G61:163). The catalogue controller checks the declaration "with the conditions `accepted` (its domain), `compatible` (lint and classification) and `conformant` (its batteries), and publishes it in the next snapshot only when all three are true" (G61:163).

- ATLAS-GATEWAY-014: "A service whose admission fails serves none of its capabilities, every capability admitted before keeps serving, and the failed condition and its reason are in readiness and the audit" (G61:64).
- Availability: "A service's availability condition (healthy, unhealthy, unreachable) sets its capabilities' availability (§3.1, §3.1.1)" (G61:163).
- ATLAS-GATEWAY-006: "A service that crashes makes its capabilities `unavailable` within five seconds, and nothing else" (G61:56).

**One list** (G65:166): the composition root "registers each module (services, data sources, encodings, audit sinks, controllers) explicitly, in one list, so the registered set is visible in one place, never from an `init` function". ATLAS-PACKAGES-008 is the vet check (G65:60), enforced by `atlasvet` noinit (AG:40). ATLAS-PACKAGES-006: "Every module a deployable registers implements its namespace's interface, checked at start and in CI; two modules with one id fail at start" (G65:58).

**Profiles as files.** "A service's profiles are files in its package, embedded in the binary, so a binary serves exactly the profiles it was built with" (G65:169). AGENTS: profiles live under `profiles/<id>@v<N>.json` in the service's package (AG:62).

**State by domain.** ATLAS-PACKAGES-009: "A service's `StateHandle` refuses every key outside its own domain, and no service imports another domain's service or reaches its containers" (G65:61; G65:216).

IN SCOPE, in process.

AMBIGUOUS: whether "admission" in the first build is a running catalogue controller reading declarations from a store (G61:163), or a static check over the deployable's one list at start. Readings: (a) the full controller over the fake store with the change feed (G67:217: "from Cosmos DB's change feed, never by polling"); (b) admission at start, from the in-binary list, through the generation lifecycle (G65:212). Both fit "in process". REV adds no service registration over the network.

### 3.5 Switches (G61:270–282; ATLAS-GATEWAY-011)

- "An administrator turns things off without a deploy, through `atlas.switch` (confidential, `Gateway.Admin`, §9.3, §9.3.2): one resource per switch, with the envelope, naming its `target`, its `state` (`on`, `off`), a `reason`, and an optional `until` after which it turns back on by itself. Switches travel in the snapshot, so one reaches every replica within 60 seconds, and each change is audited" (G61:272).

| Target | Turned off (exact) |
|---|---|
| A capability (`creditrisk.realised`), one interaction (`…:fetch`), a domain (`creditflow.*`) or a service (`service/yieldbook`) | Its calls are `unavailable` with the switch's reason; its availability is `unavailable`, with the reason in `describe`'s `availability.detail` |
| `execution.handoff`, `execution.export` | That execution is withdrawn from `describe`'s eligibility, so clients take the next (§9.7) |
| `edge.mcp` | The MCP edge refuses new sessions |
| `degraded_mode` | §6.1.14's degraded serving is off |
| `controller.<name>` | That controller pauses; resync resumes it when turned on |

- "Anything new that can be turned off ships with its switch: a new execution, edge, controller or service is `off` in Production until it has passed in Acceptance" (G61:282). AG:67: "Add its row to the blast-radius table, and its switch."
- ATLAS-GATEWAY-011: "A switch turned off reaches every replica within 60 seconds without a deploy: its target's calls are `unavailable` with the switch's reason, and its capabilities' availability says so; turned on, or past its `until`, they serve again" (G61:61).
- OVERRIDDEN BY REVIEW (R13): "A switch behind a failed control channel promises no immediate revocation" (REV:71; DR R13).
- AMBIGUOUS: the target naming is inconsistent. Service targets are written `service/yieldbook` (G61:276), while module ids are `service.yieldbook` (G65:206; G67:293, `<namespace>.<name>`). Readings: (a) the switch target uses `service/<name>`; (b) it uses the module id `service.<name>`. Choose one grammar and record it.
- Scope: switch evaluation in the snapshot and `unavailable` with reason are IN SCOPE (AG:67 makes a switch part of adding a capability). The `atlas.switch` admin resource is DEFERRED: it is a confidential resource, and REV puts resource work "after the journey" (REV:72). AMBIGUOUS: if `atlas.switch` is deferred, switches enter the snapshot as fixture state only.

### 3.6 Bulkheads, breakers, throttles, shedding (G61:192–205; G67:147–156)

| Concern | Rule (exact) |
|---|---|
| Retries | "Only `retryable` problems, only replayable requests, with backoff, jitter and `Retry-After`, within a retry budget, at one hop only (§4.1)" (G61:196) |
| Where the gateway retries | "At one hop only, nearest the upstream: in the adapter (the vendor client, the Snowflake adapter); never in the pipeline or an edge" (G67:152) |
| Retry budget | "One per dependency: retries at most one in ten calls to it in a minute (§4.1)" (G67:153) |
| Circuit breakers | "Per service and per upstream: open after five consecutive failures in 30 seconds, probe every 15 seconds; while open, the capabilities are `unavailable`" (G61:197). §6.7: "one probe after 15 s; only the upstream's own failures count; open sets the capabilities `unavailable`, closed sets them `available`, within five seconds" (G67:154) |
| Upstream throttle | "One concurrency and rate limit per upstream, shared across every copy of its service" (G61:198) |
| Bulkhead | "Each upstream its own connection pool, in-flight limit and bounded queue (twice its concurrency); never shared between a warehouse and a vendor" (G67:155) |
| Fail fast | "A full bulkhead answers at once (`limit_exceeded` or `unavailable` with `Retry-After`), naming the upstream in `detail`" (G67:156) |
| Load shedding | "A queue no deeper than twice the concurrency limit; beyond it, 503 with `Retry-After`" (G61:199) |
| Isolation | "Each service host with its own memory and CPU limits; restarted with backoff; re-registered on restart" (G61:200). DEFERRED (no service hosts) |
| Blast radius | "Written down per dependency and tested (§6.1.16); a dependency whose failure stops every call is a defect, listed in §8.4 with its fix" (G61:205) |

- AG:46: "Retries only of retryable, replayable work, at one hop, within a budget" (`kit/retry`; MECH-006).
- AMBIGUOUS: breaker probing. G61:197 says "probe every 15 seconds"; G67:154 says "one probe after 15 s". Readings: (a) half-open after 15 s with a single trial, re-opening on failure, which repeats every 15 s while failing (both are then true); (b) continuous probing every 15 s. Reading (a) reconciles them.
- AMBIGUOUS: the shared upstream throttle "across every copy" (G61:198) needs shared state. With one deployable it may be per-process; no mechanism is named in my files. For the first build: one replica, so local holds; record that it is not shared.
- AMBIGUOUS: "Upstreams" (G61:227) say capacity is "managed with its owner behind its service, invisible to callers, under the shared throttle and breaker above". No concurrency numbers are given for Snowflake or any vendor.
- First build: a breaker, bulkhead and retry budget for the Snowflake/DuckDB upstream are IN SCOPE (acceptance 5, "source failures behave the same in every client", REV:50).

### 3.7 State and events (G61:127–151)

Three tests place any state, applied in order (G61:131–135):

| Test | The state | Owned by |
|---|---|---|
| **Access:** does it say who a person is, which people belong together, or who requested, approved or reviewed access? | People; group membership; app roles; access requests and reviews | **Entra ID**. Atlas's policy names groups by object id and never copies their members |
| **Telemetry:** is it a record of something that already happened, kept to report, debug, analyse, bill or prove? | Traces, logs, metrics; the audit trail; usage history | **Azure Monitor** and **immutable Blob storage** for the audit. "Never the gateway's database" |
| **Decision:** does the gateway read it to admit, refuse, limit, resume or replay a call, exactly, under concurrency? | Limit counters and reservations; budgets; grants, denies and limit definitions; house rules and settings; operations; idempotency keys; durable subscriptions; registered services; the resources Atlas keeps | **Cosmos DB** |

How Cosmos DB is used (G61:141–147):

- One transactional batch of conditional patches reserves a call's limits. OVERRIDDEN BY REVIEW (R07): "One budget authority per metered provider and an atomic reservation set. Unsupported combinations are refused" (REV:65). Cosmos batch atomicity is one container and one logical partition (DR R07).
- Session consistency; exactness from conditional writes and ETags.
- The request path's containers have their own throughput.
- Grants, roles, profiles and house rules are held in memory and cleared by the change feed.
- Partition by what is read together: idempotency by principal and key, time-to-live on everything that expires.
- "Per call, the database sees one reserve, one settle, and an idempotency record for a write" (G61:146).

Ports and events:

- "Every store is behind a port (`Store`, `Limits`, `Operations`, `AuditSink`, `AuditReader`, §6.5)" (G61:151). IN SCOPE as ports and fakes. The real Cosmos adapter: see §5.6.
- "The change feed is the outbox" (G61:149) and ATLAS-GATEWAY-004 (G61:54). OVERRIDDEN BY REVIEW (R15): "Explicit immutable outbox facts where every event matters" (REV:73). DEFERRED (events, step 5).

### 3.8 Degraded mode and last-known-good (G61:253–268)

- "The request path reads one immutable snapshot (profiles, governance, house rules, switches, admitted services), built and checked by the `catalogue` controller." "Each published snapshot is also written to Blob storage (container `atlas-snapshots`, readable and writable by the gateway's identity only) as last-known-good, with its revision and SHA-256" (G61:255).
- Starting: "A replica waits up to 30 seconds for the live snapshot; if Cosmos DB does not answer, it loads last-known-good, checks its hash, and becomes ready, reporting the source and the snapshot's age in readiness. It keeps trying Cosmos DB and switches to the live snapshot when it can" (G61:257).
- ATLAS-GATEWAY-008: ready "within 45 seconds, serving the last-known-good snapshot, and says so in readiness with the snapshot's revision and age" (G61:58).
- ATLAS-GATEWAY-010: "A snapshot that fails its checks is never published; the one serving keeps serving, and the failure is in readiness and the audit" (G61:60).
- Snapshot checks (G67:218): "Every profile lints; every binding names an admitted service; every grant and switch names an existing capability, interaction or namespace; every house rule's capabilities exist; the revision is newer than the one serving."
- Snapshot version (G67:220): "The catalogue's ETag is its revision; a governance change moves the governance revision, never a fingerprint." Snapshot mechanics: "One immutable value, swapped atomically (`kit/hot`), read once per call" (G67:212).

Degraded serving while Cosmos DB is unreachable (G61:259–266; ATLAS-GATEWAY-009 at G61:59):

| Call | Answered (exact) |
|---|---|
| Built-ins answered from the snapshot (`atlas whoami`, `catalog`, `search`, `describe`, `guide`, `access`, `clients`, `setup`) | Served |
| A `read` interaction with no budget, not `Metered`, and below no confirmation threshold | Served, its rate and concurrency limits kept per replica (each limit divided by the replicas the snapshot last counted, rounded up); its audit record marked `degraded: ["limits_local"]`; not settled |
| A `read` with a budget, `Metered`, or subject to confirmation | `unavailable`, `detail` naming Cosmos DB; an exact limit cannot be reserved |
| Any write, action, operation start or poll, resource read, `subscribe` or `publish`, `atlas.usage` | `unavailable`: the state it needs is in Cosmos DB |

- "Degraded mode lasts at most 60 minutes from the last moment the snapshot was known current; after that every call but health and the bootstrap is `unavailable`, so a grant revoked during the outage is never honoured for long. The switch `degraded_mode` (§6.1.15) turns degraded serving off, so every call fails closed at once" (G61:268).
- OVERRIDDEN BY REVIEW (R13): "Separate freshness bounds for policy, groups, connections and links. A switch behind a failed control channel promises no immediate revocation" (REV:71). CORE:222: "Bound the staleness of authorization separately, and choose explicitly which calls may continue during a control-store outage. A mode using stale policy cannot simultaneously guarantee immediate revocation." CORE:224: "Do not label approximate per-replica limits exact." REV step 3.
- AMBIGUOUS: the review sets no new values. Readings: (a) keep 60 minutes as the policy bound and choose separate bounds for groups (the 10-minute overage cache, G67:142), connections and links, recorded as decisions; (b) leave all values open until an owner decides. Connections and links are DEFERRED (events and handoff), so the build needs only the policy and group bounds.
- AMBIGUOUS: whether `degraded: ["limits_local"]` survives R09. It is a member of the single audit record. Under R09 it would sit on the admission or outcome fact.
- Scope: the snapshot, the atomic swap and the checks are IN SCOPE. LKG plus degraded mode is IN SCOPE as REV step 3 (R13) against fakes. The real Blob container `atlas-snapshots` is DEFERRED to real systems (AMBIGUOUS, see §5.6).

### 3.9 Controllers, and the catalogue controller (G61:235–250)

"no call changes anything outside the gateway while the caller waits. The call writes the desired state to Cosmos DB and answers; a controller converges the outside world to it and reports what it observed ... A failure halfway is a state, retried by the controller, never an inconsistency left behind. Every controller follows the contract of §6.7, §6.7.6, runs in the worker deployable (today in the gateway's process, §6.1.9), and has one leader across replicas" (G61:237).

| Controller | Desired state | Converges | Woken by | Resync | First build |
|---|---|---|---|---|---|
| `catalogue` | Registered services and their profiles; governance; house rules; switches | The snapshot: each service admitted whole or not at all (§6.1.6), the snapshot checked, published and saved as last-known-good | The change feed; a service reporting it changed | 1 minute | IN SCOPE |
| `event-entities` | `event.topic`, `event.queue` and `event.subscription` records; registered event types | Service Bus topics, queues, subscriptions and their filters | The change feed | 10 minutes | DEFERRED |
| `event-rights` | The grants and denies reaching each event entity's `publish` and `subscribe`; each subscription's owner | The Service Bus data roles the gateway assigned: missing ones added, any no grant backs removed | `atlas.grant.changed`; a subscription created or deleted | 5 minutes (group membership changes in Entra have no event) | DEFERRED |
| `operations` | Each operation `pending` or `running` | Its work, on one replica under a lease (§6.7, §6.7.7) | An operation started; a lease lapsing | 30 seconds | IN SCOPE where durable work exists |
| `settlement` | Each handed-off or exported call's reservation | Settled from the source's own record of what ran (§6.2.6) | A handover issued | 5 minutes | DEFERRED |
| `publications` | Each `nautilus.dashboard`, its visibility and published revision | Its public copy in the static website's storage | The change feed | 10 minutes | DEFERRED |

Operations:

- "A call that needs the outcome waits for it, boundedly." `subscribe` waits up to 10 seconds for `ready` (G61:248). DEFERRED.
- "A `create` of a converged resource answers at once with the record, `ready` false until it converges" (G61:248). DEFERRED (resources).
- Leases: "A running operation's lease is renewed every 10 seconds and lapses after 30." On takeover, "if its service declares the work resumable ... it resumes from its last checkpoint, at most three times; otherwise it cancels the upstream work by its tag and ends the operation `failed` with `unavailable`, releasing its idempotency key so the caller's repeat starts afresh" (G61:250).
- ATLAS-OPERATION-001 (G61:65): within one minute another replica resumes or fails the operation.
- ATLAS-OPERATION-003 (G61:67): upstream work is cancelled by its tag before the operation ends.
- **OVERRIDDEN BY REVIEW (R08):** ATLAS-OPERATION-002 (G61:66), "An operation ended by a takeover releases its idempotency key", and "releasing its idempotency key" (G61:250) are reversed. Now: "One logical operation per key for its whole retention, uncertain outcomes included. Workers are fenced after their lease and reconciled" (REV:66). "An uncertain upstream outcome remains recorded and is reconciled before retrying" (CORE:202).
- Retention: "must cover the active operation and the promised retry/recovery window, even when work lasts more than 24 hours" (CORE:212). Cancellation: "reaches `cancelled` only when the provider knows work stopped" (CORE:212) (R17).

---

## 4. The built-ins: all 20 `atlas.*` capabilities

Rules for all of them:

- "The gateway MUST serve these, at the bindings its bootstrap names. They are the only capabilities a client calls without first reading a profile; their request and result schemas are fixed by `atlas/1.0`, as the `$defs` of `builtins.schema.json`. A call to a built-in carries no `Atlas-Profile-Expect`; its answer carries `Atlas-Profile` with the built-in's `exact-id`, which `atlas.describe` serves like any other" (D93:45).
- "The `atlas.*` profiles the gateway serves MUST use exactly these schemas" (D93:66).
- "only built-ins are `atlas.*`" (CAT:110).
- The domain holds "20 capabilities (data, event, resource)" (CAT:158), all execution `gateway`, stability `official` (CAT:171–190). "Every interaction is `read` unless it says otherwise" (CAT:192).
- `atlas.sql`, `atlas.portfolio.holdings`, `atlas.credit_risk.spread_duration`, `atlas.connect` and `atlas.lock` are NOT built-in ids (HB:566).

Request members below are copied from §9.3.2 (D93:49–64). `?` marks an optional member; members without `?` are required.

| # | Capability | Surface | Purpose (CAT) | Interactions | Request (§9.3.2) | Result (§9.3.2) | Degraded? (G61:263) | First build |
|---|---|---|---|---|---|---|---|---|
| 1 | `atlas.whoami@v1` | data | "Who the caller is to the gateway." | fetch | `{}` | `{ principal, name, kind ("person", "workload"), roles[], environment, available_domains[], house_rules }` | Served | IN SCOPE ("Orient"; CORE:19, 238) |
| 2 | `atlas.catalog@v1` | data | "The working view: what the caller can use here, now." | fetch | `{ domain?, family?, effect?, previews?: boolean, cursor?, page_size? (1–500, default 100) }` | Catalogue page | Served | IN SCOPE ("authorized catalogue/search", CORE:238) |
| 3 | `atlas.catalog.changed@v1` | event | "The caller's working view may have changed." | subscribe | `{}` | A connect handover; each event's data is `{ catalog_etag }` | Not listed | DEFERRED (events; no HTTP subscriptions in `atlas/1.0`, H96:83) |
| 4 | `atlas.search@v1` | data | "Available capabilities ranked against words." | fetch | `{ words: string (1–200), cursor?, page_size? }` | Catalogue page, ranked (`score`, `why`) | Served | IN SCOPE |
| 5 | `atlas.describe@v1` | data | "One available capability's full profile." | fetch | `{ by: "id", id }` (a capability-id or pinned-id) or `{ by: "exact", exact }` | Describe answer (`describe.schema.json`) | Served | IN SCOPE. Also the lookup for an unseen capability (C97 §9.7.5), which acceptance 4 needs (REV:49). OVERRIDDEN BY REVIEW (R01): caller state private and uncached; contract cached by fingerprint apart from the document revision (REV:59) |
| 6 | `atlas.guide@v1` | data | "How to use one available capability, written for an agent." | fetch | `{ by: "id", id }` or `{ by: "exact", exact }`, with `client?` (`cli`, `python`, `node`, `go`, `mcp`) and `max_tokens?` (200–20000, default 4000) | `{ exact, client, markdown, tokens, truncated, revisions }` | Served | IN SCOPE (CORE:238; acceptance 6 "The guide's example succeeds", REV:51). R01: a guide's identity includes the prose revision |
| 7 | `atlas.access@v1` | data | "The access view: every listed capability with the caller's status." | fetch | `{ id?, domain?, cursor? }` | Page of `{ id, status, reason, next, held_by?, limits?, interactions? }` | Served | AMBIGUOUS. (a) IN SCOPE: `not_granted` is "learnable through `atlas.access`" (G66:80), and acceptance 3 needs "a clear refusal" (REV:48); (b) DEFERRED: CORE:238 does not list it. Recommend (a) |
| 8 | `atlas.usage@v1` | data | "What the caller has used, and what is left." | fetch | `{ id?, period?, cursor? }` | Page of `{ id, interaction, kind, used, limit, remaining, period, pool? }` | `unavailable` | DEFERRED unless limits and budgets are built (AMBIGUOUS: REV step 3 builds R07 against fakes) |
| 9 | `atlas.setup@v1` | data | "How to set this machine and harness up for Atlas." | fetch | `{ harness? }` | `{ steps: [{ name, done, command?, detail }] }` | Served | AMBIGUOUS: not named by CORE:238 ("bootstrap" is `/.well-known/atlas`, not setup). Likely DEFERRED |
| 10 | `atlas.clients@v1` | data | "The client catalogue." | fetch | `{ cursor? }` | Page of `{ name, language, built_for, roles[], levels[], extras[], version, install }` | Served | Not needed by the journey; DEFERRED |
| 11 | `atlas.compare@v1` | data | "Two versions of one data capability, side by side." | fetch | `{ id: capability-id, from: version, to: version, request: object }` | Table: the two results joined on their keys (CAT: "with the lens applied"; table · chunked) | Not listed | DEFERRED. It depends on lenses, now experimental (R03, R04; REV:90) |
| 12 | `atlas.operation@v1` | resource | "The operations the caller started." | get · list · cancel (write) · watch | `{ name }`; `{ cursor?, state? }` | Operation; page of operations; `watch` is a connect handover | Operation start or poll `unavailable` | IN SCOPE only "where that journey needs" durable operations (CORE:238). `watch` DEFERRED (connect handover = events) |
| 13 | `atlas.settings@v1` | resource | "The caller's own settings." | get · update (write) | `{}`; `{ fields, settings }` with `If-Match` | The caller's own settings document | Resource read `unavailable` | DEFERRED (resources after the journey, REV:72) |
| 14 | `atlas.directory@v1` | data | "People and groups, to share with." | fetch | `{ words: string (2–100), kind?: "person" or "group", cursor? }` | Page of `{ principal, name, kind }` | Not listed | DEFERRED (Microsoft Graph, G61:301) |
| 15 | `atlas.grant@v1` | resource, confidential | "Individual grants and denies (`Gateway.Admin`)." | get · list · create (write) · delete (write) | Its resource schema, served in its profile | One grant: subject, address or family, allow or deny, set by | — | DEFERRED. The journey's grants are fixture data in the snapshot |
| 16 | `atlas.role@v1` | resource, confidential | "Roles: named sets of grants with limits (`Gateway.Admin`)." | get · list · update (write) | Its resource schema | One role: members, grants, limits. HB:225: "membership authority is Entra" | — | DEFERRED |
| 17 | `atlas.limit@v1` | resource, confidential | "Allowances, pools and ceilings (`Gateway.Admin`)." | get · list · update (write) | Its resource schema | One limit: subject or pool, interaction, amount, period, set by | — | DEFERRED |
| 18 | `atlas.context@v1` | resource, confidential | "Managed context: skills and system prompts every client receives (`Gateway.Admin`)." | get · list · update (write) · publish (write) · rollback (write) | Its resource schema | One scope: its published revision and drafts | — | DEFERRED. House rules then come from fixtures (AMBIGUOUS whether the journey fixture has any) |
| 19 | `atlas.switch@v1` | resource, confidential | "Kill switches: turn a capability, domain, service or gateway behaviour off without a deploy (`Gateway.Admin`)." | get · list · update (write) | Its resource schema (`target`, `state`, `reason`, `until`, G61:272) | One switch: target, state, reason, until | — | Resource DEFERRED; switch evaluation IN SCOPE (§3.5) |
| 20 | `atlas.audit@v1` | data, confidential | "The audit trail, a day at a time (`Gateway.Admin`)." | fetch | `{ day: date, before?: string, page_size? (1–1000, default 200) }` | Page of audit records | Not listed | Reading DEFERRED. Writing audit facts IN SCOPE (R09). R09 changes what a "row" is: "a joined summary" of facts (DR R09; CORE:226) |

Interactions between the built-ins:

- `atlas.whoami` and every catalogue page carry `house_rules`; administrators write them through `atlas.context` (D93 house rules paragraph; D26 House rules).
- A rule reaches a caller only when the caller can use every capability it names (D26 House rules).
- `atlas.catalog.changed` tells a long-lived client to refetch the catalogue (C97 §9.7.5).
- `atlas.describe` fills cache misses (C97 §9.7.5: "A capability the client has not seen is never refused by the client ... only the gateway says `not_found`").
- `atlas.access` explains `not_granted` (G66:80).
- `atlas.switch` writes the switches in the snapshot (G61:272). `atlas.grant`, `atlas.role` and `atlas.limit` write decision state (G61:135).

Discrepancies:

- AMBIGUOUS (count): §2.6's built-in table lists 11 (D26:95–105). It omits `catalog.changed`, `settings`, `directory` and the six admin capabilities. §9.3.2 groups the five admin resources in one row. §7 and the handbook list 20. Reading: §7's 20 is the set; §9.3.2 fixes their wire schemas.
- AMBIGUOUS (`atlas.operation` cancel): §9.6.1 says cancelling is `POST <Operation-Location>:cancel` (H96:16), while the catalogue lists `cancel (write)` as an interaction of `atlas.operation`. Readings: (a) the same interaction exposed at the operation URL; (b) two routes. Another reader owns §9.6 and §6.7.7.

---

## 5. Packages and layers

### 5.1 The layers (G65:9–19, 80–95)

| Layer | Contents (G65:84–90) |
|---|---|
| L0 protocol, kit | `sdk/go/protocol` · `sdk/go/kit` ("shared with Go SDK, CLI, conformance gateway and Python engine") |
| L1 ports | `ports` (interfaces) · `porttest` (each port's suite) · `fakes` (in-memory implementations) |
| L2 core | `core/pipeline` · `core/access` · `core/limits` · `core/operations` · `core/registry` · `core/context` |
| L3 adapters | `adapter/cosmos` · `entra` · `servicebus` · `keyvault` · `storage` · `monitor` · `snowflake` · `vendorhttp` ("independent of each other") |
| L4 services | `svc/harbour` · `svc/ids` · `svc/snowflake` · `svc/yieldbook` · `svc/nautilus` · … ("independent of each other") |
| L5 edges | `edge/http` (net/http) · `edge/mcp` (official MCP Go SDK) · remote service protocol later |
| L6 deployables | `cmd/gateway` · `cmd/worker` · `cmd/events-publisher` · `cmd/service-host` later ("composition roots") |

Layering rules:

- "A package may depend only on packages in lower layers. Packages marked independent never import each other" (G65:95).
- Rules (G65:49–51): "A package depends only on lower layers; adapters never import each other; services never import each other or `core`." "Only deployables import adapters." "A new package meets 6.5.5."
- **OVERRIDDEN BY REVIEW (R24):** "Six layers numbered L0–L6" becomes "`layers.toml` is the rule. The import check stays, and same-layer imports are allowed" (REV:82). DR R24: §6.5 "describes six layers but numbers L0–L6, and forbids same-layer imports its examples require". Status: "Done (`tools/layercheck`)".
- AGENTS' operative statement (AG:39): "Layers: a package imports only its own layer or below; adapters and services never import each other; services never import `core`; only a deployable imports an adapter; `protocol` imports only the standard library and Arrow; `kit` only `protocol` and the standard library". Checked by `tools/layercheck` with each module's `layers.toml`.
- AMBIGUOUS (apparent contradiction): G65:33 and the diagram L6→L5→L4→L3→L2→L1 suggest services (L4) sit above adapters (L3) and core (L2), yet services may not import core (G65:49). The "May depend on" column for `svc/...` reads "Ports, protocol, the L3 adapters they need, through ports only" (G65:115). Readings: (a) a service imports `ports` and `protocol`, and receives adapter implementations injected through ports by the deployable (consistent with "Only deployables import adapters", G65:50, and AG:39); (b) a service imports L3 adapters directly. (a) is the operative rule (AG:39), and "through ports only" supports it.
- AMBIGUOUS: `edge/http` may depend on "Core, ports, protocol, `net/http`" (G65:116). Edges import core. Services do not. Fine as written; noted because an edge must not import a service.
- Observed in the repository, read-only: only `sdk/go/layers.toml` exists. §6.5.9 names `gateway/layers.toml` (G65:173), which does not exist yet. `sdk/go/layers.toml` places `sdk/go/protocol/...`, `sdk/go/snowarrow/...`, `sdk/go/kit/...` and `sdk/go/internal/...` at layer 0; `sdk/go` (the Go client) at 1; `sdk/go/engine/...` and `sdk/go/conformance/...` at 2; `sdk/go/cmd/...` at 3. It forbids protocol→kit/snowarrow and kit→snowarrow, and allows protocol only `github.com/apache/arrow-go/v18/...`. The gateway module will need its own `layers.toml`. Existing gateway directories: `adapter/snowflake`, `cmd/gateway`, `cmd/gateway-local`, `core/{access,audit,builtins,operations,pipeline,registry}`, `edge/http`, `fakes`, `internal/refs`, `local/duckwh`, `ports`, `porttest`, `svc/views`.

### 5.2 The packages (G65:97–121)

| Package | Layer | Solves | May depend on |
|---|---:|---|---|
| `sdk/go/protocol` | L0 | The Go implementation of §9: documents (types generated from §9.3's JSON Schemas), grammars, validation in the Atlas Schema profile, canonical JSON and fingerprints, resolution and lenses, headers, framing, problems; change classification and the readiness check for the gate | The standard library; Apache Arrow for Go |
| `sdk/go/kit/...` | L0 | "problems, serving (lifecycle, server, health, middleware, telemetry), retries and breakers, operations, idempotency, paging, hot snapshots, caches, bounded concurrency, limit arithmetic, token verification, resource stores, reconciling controllers" ... "no Atlas decision (no access, no catalogue, no capability)" | `sdk/go/protocol`; the standard library |
| `ports` | L1 | Service ports `Adapter`, `Publisher`, `Operator`, `CredentialProvider`, `StateHandle`; infrastructure ports `Store`, `PolicyEngine`, `Limits`, `Operations`, `EventBus`, `TicketIssuer`, `TransferLinks`, `AuditSink`, `AuditReader`, `Secrets`, `IdentityVerifier`, `OnBehalfOf`, `ServiceRegistry`, `DataSource` (§6.4) | `sdk/go/protocol` |
| `porttest` | L1 | One suite per port, an exported function (`porttest.Store(t, newStore)`); the family batteries | `ports`, `testing` |
| `fakes` | L1 | In-memory implementations that pass the suites, "among them a `DataSource` that runs the planned SQL on DuckDB and a token issuer with its own keys" | `ports` |
| `core/...` | L2 | The pipeline, access engine, limit and budget logic, resilience (breaker, bulkhead, rate limit, deadline, retry budget), operations, the registry of modules and the catalogue, the controllers, switches, managed context and house rules | Ports, protocol, the OpenTelemetry API |
| `adapter/cosmos` | L3 | Store, limit counters, operations and idempotency keys, the registry, the change feed as outbox | Ports, `azcosmos` |
| `adapter/entra` | L3 | Token validation, group lookup, On-Behalf-Of, the signed token between front door and services | Ports, MSAL for Go, a JWT library |
| `adapter/servicebus` | L3 | Entities, role assignments, connect handovers; publishing the gateway's own events | Ports, `azservicebus`, `armservicebus`, `armauthorization` |
| `adapter/keyvault` | L3 | Secrets | Ports, `azsecrets` |
| `adapter/monitor` | L3 | `AuditSink`, `AuditReader`: immutable Blob by group commit, Logs Ingestion API, Logs Query API | Ports, `azblob`, `.../monitor/ingestion/azlogs`, `.../monitor/query/azlogs` |
| `adapter/storage` | L3 | Transfer links (user delegation SAS), export files, later immutable audit | Ports, `azblob` |
| `adapter/snowflake` | L3 | The Snowflake `DataSource`: planning through the Go SQL library, the read-only guard, Arrow batches in the writer schema, handoff documents, exports, settlement | Ports, `gosnowflake`, the Go SQL library |
| `adapter/vendorhttp` | L3 | The one HTTP client for vendors: retry, OAuth2, a shared throttle per vendor, redaction | Ports, `net/http` |
| `svc/...` | L4 | Each upstream's or domain's capabilities, profiles embedded (`embed`) | Ports, protocol, the L3 adapters they need, through ports only |
| `edge/http` | L5 | The HTTP binding of §9.6: the bootstrap, every interaction, streamed answers, problems, CORS | Core, ports, protocol, `net/http` |
| `edge/mcp` | L5 | The MCP edge's fixed tools (§4.2) | Core, ports, protocol, the official MCP Go SDK |
| `cmd/gateway`, `cmd/worker`, `cmd/events-publisher` | L6 | Composition roots: choose adapters, register modules, mount edges, configure telemetry, logging and settings | Anything below |
| `cmd/atlas-new` | L6 | The generator (§6.7.18): a developer tool, with no image and no pipeline | Its templates; `sdk/go/protocol` |

- AMBIGUOUS: the `ports` names in G65:103 (`Adapter`, `DataSource`, ...) differ from the repository's existing `gateway/ports` files (`provider.go`, `warehouse.go`, `policy.go`, `identity.go`, `audit.go`). Readings: (a) rename to the spec's port names; (b) keep the "provider" and "warehouse" terms (CORE §8 says "provider"). Not decided by REV.
- Sub-package names `kit/conc`, `kit/problem`, `kit/retry`, `kit/hot` and `kit/reconcile` come from AG:41–46, G67:212 and G65:210.
- Mapping from the earlier Python code (G65:121): `atlas_core/store/cosmos.py` → `adapter/cosmos`; `atlas_core/identity` → `adapter/entra`; `problem.py`, `transport/`, the fingerprint and resolution → `sdk/go/protocol`; `store/memory.py` → `fakes`; `access`, `admission`, `metering`, `jobs`, `managed`, `pipeline`, `registry` → `core/...`; each `svc_*` → `svc/...`. "it is read, never ported line by line".

### 5.3 What is not a package, and when one is made (G65:123–141)

| Not a package | Where it lives | Why |
|---|---|---|
| One interface package per concern | `ports` | "Interfaces with no drivers cost nothing to group; splitting decouples nothing" |
| One implementation package per port (`cosmosstore`, `cosmoslimits`, ...) | `adapter/cosmos` | "They share the Cosmos SDK, so they change together" |
| A Go copy of the protocol inside the gateway | `sdk/go/protocol` | One implementation, shared |
| An adapter for operational telemetry | Deployables configure the OpenTelemetry SDK | "Packages use only the API; the audit is not this" |
| Resilience | `core/...` | "No driver, one user; it becomes a package only if a client needs it alone" |

A new package is created only for "a new external system (an adapter, in L3)", "a new deployable (L6)", or "something an outside user needs without the rest of its package" (`sdk/go/kit` is one). "Anything else is a file in an existing package" (G65:135–141). There is no `common` or `util` package (G65:78).

### 5.4 Ports and conformance (G65:155–157); principles applied (G65:65–78)

- "Every port has a suite in `porttest`, and every implementation, the fakes included, runs it in its own tests: an implementation is accepted when it passes." ATLAS-PACKAGES-002 (G65:54). AG:55 repeats it.
- "Replacing a technology ... is a new adapter package that passes the suite, and one line in a deployable" (G65:157). Example: Cedar replacing the access engine (G65:38).
- "Wire in one place: Only deployables choose which implementation backs which interface, explicitly in code: no service locator or injection framework" (G65:75).
- "Libraries use APIs; applications choose implementations: Packages use the OpenTelemetry API and `log/slog`; only deployables configure exporters and handlers" (G65:76).
- "One `go.mod` per module, a `go.work` at the repository root joining `sdk/go`, `gateway` and `clients/cli`" (G65:77).
- ATLAS-PACKAGES-003: the gateway, conformance gateway, Go SDK, CLI and Python SDK's engine use `sdk/go/protocol` for every protocol rule; "nothing in Go implements one a second time" (G65:55).
- The shared protocol is "an implementation, not the reference". Three independent checks: vectors generated by `tools/spec/` (Python), the TypeScript implementation compared case by case, and a client written from §9 alone (AP-003) (G65:147–153). TypeScript `@atlas/protocol` in `clients/node` is the second implementation (G65:187). IN SCOPE: Node is in the first build (REV:12).

### 5.5 Composition roots (G65:159–169)

Each deployable's `main`:

1. "reads its settings (from the environment, with defaults)". AG:42: settings are read once, in `cmd/`, a `config`/`settings` package or a `settings.go` (atlasvet nogetenv).
2. "constructs the Azure credential (managed identity in Azure, the developer's sign-in locally)".
3. "creates each adapter".
4. "registers each module (services, data sources, encodings, audit sinks, controllers) explicitly, in one list ... never from an `init` function".
5. "passes them to the core and mounts its edges".

- "The `local` root wires the fakes and the emulators instead of Azure" (G65:169). In the repository: `gateway/cmd/gateway-local` and `gateway/local`.
- Enforcement (G65:173–178): `gateway/layers.toml`; `go test ./tools/layercheck` reads imports with `go list`; `go mod tidy` must change nothing (ATLAS-PACKAGES-004); minimal version selection; `go vet`, `staticcheck`, `gofmt`; licence headers and `THIRD_PARTY_NOTICES`. AG:72 makes `make check` the gate.
- ATLAS-PACKAGES-005: "Each deployable's image holds its one binary, the CA certificates and nothing else" (G65:57). Images are AMBIGUOUS for the first build: REV:21 builds "without Docker".

Module namespaces (G65:204–210):

| Namespace | Interface | Modules today | First build |
|---|---|---|---|
| `service` | The service ports (`Adapter`, `Publisher`, `Operator`) | `service.harbour`, `service.yieldbook`, `service.nautilus`, ... | IN SCOPE (the journey's services) |
| `datasource` | `DataSource` (§6.4) | `datasource.snowflake`, `datasource.duckdb` (tests) | IN SCOPE (DuckDB with sqlglot-go faking Snowflake, REV:21) |
| `encoding` | Writing an answer in one encoding | `encoding.json`, `encoding.ndjson`, `encoding.arrow`, `encoding.csv`, `encoding.parquet` | json and ndjson IN SCOPE. Arrow is an optimisation (R11). csv and parquet: AMBIGUOUS, probably DEFERRED |
| `auditsink` | `AuditSink` | `auditsink.monitor`, `auditsink.memory` (tests) | memory IN SCOPE; monitor DEFERRED (real Azure) |
| `controller` | A `kit/reconcile` controller | `controller.catalogue`, `controller.event-entities`, ... | catalogue IN SCOPE |

### 5.6 What each deployable contains (G61:207–219; REV:13)

| Deployable | Runs (G61:211–217) | Scales on | First build |
|---|---|---|---|
| `gateway` | "Every edge, the core pipeline, every service in process (today's shape)" | Requests | IN SCOPE: the one Go deployable (REV:13) |
| `service-host` | "The service protocol's server and chosen services" | That upstream's load, licence or isolation | DEFERRED (R20) |
| `stream-edge` | "The streaming edges and the core pipeline" | Connections | DEFERRED |
| `worker` | "Operations, jobs and the controllers (§6.1.13)" | Queue length | IN SCOPE only "where durable work needs one" (REV:13). AMBIGUOUS: controllers run "today in the gateway's process" (G61:237). Readings: (a) the catalogue controller in `gateway`, the worker only for operations; (b) all controllers in `worker` |
| `events-publisher` | "The change feed to Service Bus" | Writes to state | DEFERRED |

- "All share the store and the catalogue, so together they are one gateway" (G61:219).
- Compute "Azure Container Apps" and ingress "Azure Front Door or Application Gateway"; "Azure API Management is not in the request path" (G61:219). DEFERRED (infrastructure).
- When to split (G61:223): "a different scaling profile (long-lived connections), a different owner (Nautilus's services), a different runtime, a need for isolation (actions on Astronomer or Azure), or heavy compute (jobs)". CORE:186: "Split a service when its trust boundary, scale or release ownership justifies it."
- AMBIGUOUS: which adapters are real in the first build. REV step 5 lists "Real systems: the Development source, direct downloads, events, delegated identity" as out of reach (REV:33). Cosmos, Blob and Key Vault are not named. Memory guidance: fakes behind real adapters first, then real tech. §6.5.11 step 4 requires each adapter to pass "its port's suite against an emulator or a recorded exchange" (G65:196). Readings: (a) first build = ports + fakes + the Snowflake adapter over DuckDB/sqlglot-go; Azure adapters later; (b) Azure adapters built against recorded exchanges now. Reading (a) matches REV:21 ("building without Docker, DuckDB and sqlglot-go faking Snowflake, fakes behind real adapters").

### 5.7 Order of work (G65:189–198)

1. "L0 `sdk/go/protocol`, passing every vector in `spec/protocol/references/vectors/`." Done in Phase 1 (REV:70, 89).
2. "L1 `ports`, `porttest` and `fakes`, with an in-memory fake for every port, each asserted by its port's suite."
3. "L2 `core`: the pipeline, against the fakes only."
4. "L3 the adapters, each passing its port's suite against an emulator or a recorded exchange."
5. "L4 the services, each passing its family's battery in process."
6. "L5 the edges; L6 the deployables and the layer check."

REV's step order (REV:27–33) governs the overall sequencing; G65's order applies within it.

---

## 6. The 15 principles and the 15-question review, as checks

Precedence: "Where the pattern library and a principle differ, the principle wins; where a principle and §9 differ, §9 wins" (G66:44). "A change that would break a principle is first a decision in §8, with its reason" (G66:45).

- ATLAS-PRINCIPLES-001: "Every service, before it is admitted beyond `local`, and every profile, before it becomes `stable`, has the review of 6.6.3 recorded with each question answered yes, or answered no with the decision in §8 that allows it" (G66:48).
- ATLAS-PRINCIPLES-002: "Every principle in 6.6.1 names at least one requirement that a test checks (checked by `make requirements`)" (G66:49).
- Where answers are recorded: "in phase 1, in the work package's ledger notes; after it, in the pull request that admits the service" (G66:86). AG:68: "Answer the 15-question review (§6.6.3) in the pull request."

### 6.1 The principles (G66:55–71) with checks

| # | Principle | Atlas's rule (key clauses, exact) | Tested by | Check an implementer can apply | Review overrides / first build |
|---|---|---|---|---|---|
| 01 | One resource model | "one envelope (`name`, `metadata`, its desired fields, `status`), one problem shape, one page shape"; custom verb "only where none fits" | ATLAS-RESOURCE-001 to 007, AP-DOC-003 | Every failure goes through `kit/problem` (atlasvet nohttperror); every page uses the page schema; every resource has the envelope | Resources DEFERRED until after the journey (REV:72). R14: the ETag covers the full representation, with `generation` separate. Problems and pages IN SCOPE |
| 02 | Declare, then converge | Changes outside go "through a controller ... level-triggered, idempotent, one key at a time, retried with backoff, resynced; never inline in a call" | ATLAS-RECONCILE-001 to 006, ATLAS-EVENT-005, 006 | grep: no handler calls an external system's mutating API; each controller has a resync interval and one leader | The catalogue controller IN SCOPE |
| 03 | Control plane apart from data plane | "one immutable snapshot, checked before it is published, read with one atomic load per call, and saved as last-known-good; the request path never reads them from the store" | ATLAS-GATEWAY-005, 008 to 010 | The pipeline takes a snapshot value once per call; no store read for profiles, governance, switches or grants on the request path; a failing snapshot keeps the old one | R13 freshness bounds; R01 caching |
| 04 | Extension points, not forks | "a module in a named namespace (`service`, `datasource`, `encoding`, `auditsink`, `controller`) ... loaded as one generation; never a condition added to the core" | ATLAS-PACKAGES-006 to 008 | No `init`; one list per deployable; no `if id == "portfolio..."` in core | IN SCOPE |
| 05 | Contracts first, compatibility as rules | "The profile is written before the code; every change is classified by §3.3's rules; `preview`, `stable`, `deprecated`, `retired`, announced in-band; a previous gateway release reads what the new one stored" | ATLAS-VERSION-001 to 010 | The profile file exists and lints before the service code; the gate classifies it against the previous one | OVERRIDDEN BY REVIEW (R03): the smaller model; removal, unit change, type change, field made non-null, or changed default means a new major (REV:61) |
| 06 | Discovery and self-description | "Four levels: the working view, `atlas.describe`, `atlas.guide`, `explain`; all derived from profiles; ... nothing a client needs is hard-coded" | ATLAS-DISCOVERY-001 to 010 | A capability added after the clients are built works from all three clients unchanged (acceptance 4, REV:49) | R01, R23. `explain` scope: AMBIGUOUS, per profile |
| 07 | Bounded domains | "a service reaches only its own domain's state ... references cross by name, never by a join" | ATLAS-GATEWAY-007, ATLAS-PACKAGES-009 | Registration refuses a foreign id; `StateHandle` refuses foreign keys | IN SCOPE |
| 08 | Inward layering | "Six layers; dependencies point inward and down; adapters and services never import each other" | ATLAS-PACKAGES-001 | `tools/layercheck` passes | OVERRIDDEN BY REVIEW (R24): `layers.toml` is the rule; same-layer imports allowed |
| 09 | Cross-cutting concerns as one ordered chain | "one middleware of the edge or one step of the pipeline, at a written position with its reason; defaults before validation; no handler re-implements one; each step is named and timed, and the step that refuses is recorded" | ATLAS-ACCESS-009, 010, ATLAS-MECH-001 to 004 | Each step emits a timed span; the refusing step is in the audit fact; no service parses tokens or counts limits | OVERRIDDEN BY REVIEW: CORE §8's order (§1.4). "defaults before validation" is changed by R04: validate the original under its own contract first |
| 10 | Identity and policy as platform services | "Authenticate once; services trust the front door's signed token, never the caller's; deny first; authorise before validating; every decision audited; no credential crosses a boundary it should not" | ATLAS-ACCESS-001 to 010, ATLAS-REACH-001 to 004 | Refusal of `not_granted` happens before any schema diagnostic; services receive no caller token | R05 (data scope), R06 (receipts), R26 (credential binding) |
| 11 | Every write repeatable, every update conditional | "Idempotency keys on every write, ETags on every update and delete, the key passed downstream wherever the system accepts one; the server says what is retryable" | ATLAS-RESOURCE-001, 002, ATLAS-GATEWAY-013, ATLAS-MECH-008 | A counting fake per system proves no double effect after a crash | OVERRIDDEN BY REVIEW (R08): the key is never released by takeover |
| 12 | Asynchrony is explicit | "Anything that may outlive its deadline is an operation; a running operation is leased by one replica and heartbeated, and taken over or failed when the lease lapses" | ATLAS-OPERATION-001 to 003 | Lease 10 s renew / 30 s lapse; takeover test | R08 (fencing, no key release), R17 (deadlines vs retention; requested vs confirmed cancel) |
| 13 | Failure is isolated, its radius written down | "A bulkhead per dependency; fail fast; shed before collapse; budgeted retries; make-before-break; kill switches without a deploy; a table of what fails when each dependency is down for an hour, each row tested" | ATLAS-GATEWAY-006, 011, 012 | A blast-radius row plus a fault-injection test per dependency used (GATEWAY-012) | R13 |
| 14 | Observability is a contract | "Request id and trace context on every hop; the audit record a versioned schema written by the pipeline, not the handler; metrics with bounded labels; health that names its checks" | ATLAS-MECH-014, 015 | No token, key, body or query string in any log, span, metric or audit record (AG:47); metric labels from bounded sets (AG:48) | OVERRIDDEN BY REVIEW (R09): audit facts, not one record |
| 15 | One paved road | "One generator per pattern, one conformance suite, decisions in §8, rules agents read (`AGENTS.md`), exceptions recorded with an owner and a date" | ATLAS-MECH-018, ATLAS-PRINCIPLES-001 | The capability is generated by `go run ./gateway/cmd/atlas-new capability --pattern <P> <id>` (AG:64) | R20: loopback deferred |

### 6.2 Where Atlas differs on purpose (G66:75–82); all IN SCOPE

| Principle | Its sources do | Atlas does |
|---|---|---|
| 01 | Standard HTTP verbs on resource URLs; names in the path | Every interaction is a `POST` to its binding, with the name in the body (§9.6) (D4) |
| 01 | The desired state under `spec` | Desired fields at the top of the record, beside `name`, `metadata` and `status` (§9.3.4) |
| 01, 12 | States in Pascal case (`Succeeded`, `Canceled`) | Lowercase (`succeeded`, `cancelled`), and `UNKNOWN` for one a client does not know (§9.2) |
| 10 | Not found when the caller may not see it | A **listed** capability the caller lacks is `not_granted`, learnable through `atlas.access`; only a confidential one is `not_found` (D13) |
| 11 | A key reused with a different request is 422; a stale version 409 | `idempotency_conflict` (409); `precondition_failed` (412) |
| 04 | Modules register themselves at start-up (`init`) | Each deployable registers its modules in one list |

AG:9–15 lists these same six as the places the spec beats `cloud-gateway-patterns-go`.

### 6.3 The 15-question review (G66:88–102), verbatim, with a concrete check

| Q | Question (exact) | Concrete check for the first build |
|---|---|---|
| 1 | Is everything it keeps with a name and a lifecycle a resource with the envelope, and is every other operation a standard method or a declared custom verb? (01) | The journey's data capabilities keep nothing; operations (if any) use `operation.schema.json` |
| 2 | Does everything it changes outside the gateway converge through a controller, reporting `status` with `observed_generation` and conditions? (02) | Journey reads change nothing outside: the answer is yes by vacuity. The catalogue controller reports `accepted`, `compatible`, `conformant` |
| 3 | Do its capabilities keep their declared behaviour, or fail closed as 6.1.14 says, while Cosmos DB is down for an hour? (03) | A fault-injection test with the store fake failing: unbudgeted reads served with `limits_local`; others `unavailable` naming the store (subject to R13's bounds) |
| 4 | Is it a module in a namespace, with provision, validate and cleanup, listed by its namespace? (04) | It appears in `cmd/gateway`'s one list; a generation test |
| 5 | Was the profile written first, linted, and classified against the previous one? (05) | `profiles/<id>@v<N>.json` committed and gated before code (AG:62); classified by the R03 gate |
| 6 | Can an agent that has never seen it find it, read its contract and guide, and `explain` a call, from the working view alone? (06) | `atlas.search` → `atlas.guide` → call, from CLI/Python/Node; `explain` only if the profile declares it (CORE:97) |
| 7 | Does it claim only its own domain, and reach another domain's data only through that domain's capabilities? (07) | `portfolio.*` and `data.*` services claim only their own domain. AMBIGUOUS: whether `data.status` is served by its own `data` service or alongside portfolio |
| 8 | Does the layer check pass, with every vendor shape converted at its adapter? (08) | `make check` |
| 9 | Does it re-implement no step of the pipeline (no token parsing, no limits, no retries outside its adapter)? (09) | grep the service for JWT, limit or retry imports |
| 10 | Does it act only on the front door's signed token, and is every credential it holds its own? (10) | See the §3.2 AMBIGUOUS on in-process tokens |
| 11 | Can every request be repeated by a caller that lost the answer, and does each downstream step take the key? (11) | Reads repeat by nature; `QUERY_TAG` carries the request id or key to the source (G61:187) |
| 12 | Is everything that can exceed its deadline an operation, leased, cancellable and kept for its retention? (12) | Decide per profile whether holdings can exceed its `deadline.max` (AMBIGUOUS) |
| 13 | Is its row in the blast-radius table written and tested, does only its own domain fail when its upstream does, and can it be switched off without a deploy? (13) | A Snowflake/DuckDB row plus fault test; a switch per capability (AG:67) |
| 14 | Does every hop carry the request id and trace context, with no secret, body or query string in a log? (14) | MECH-014 tests |
| 15 | Was it generated from its pattern, and does it pass its conformance batteries in process and on loopback? (15) | OVERRIDDEN BY REVIEW (R20): in process now; loopback on first extraction |

### 6.4 The constitution (G66:104–122; AG:17–33)

The 15 lines are identical in G66 and AG. AG uses periods where G66 uses semicolons; the words are otherwise the same.

1. If it has a name and a lifecycle, it is a resource with the envelope.
2. Callers declare what they want; the gateway converges. No call means "do step three".
3. The request path reads a snapshot, never the store; bytes take the shortest path.
4. New behaviour is a new module in a namespace, never a condition in the core.
5. The contract comes first, and every change is classified.
6. If a client has to hard-code it, the gateway has failed to describe it.
7. A domain owns its data; others reach it through its capabilities.
8. Dependencies point inward.
9. A cross-cutting concern is a step at a written position; a handler never repeats it.
10. The front door authenticates and decides; everything behind it trusts the signed token.
11. Every write can be repeated; every update is conditional.
12. Anything slower than its deadline is an operation with a name, an end and a retention.
13. A dependency's failure costs its own capabilities, never the catalogue.
14. Every hop carries the same ids; the audit record is a schema.
15. One generator, one review, one conformance suite, one register of decisions.

### 6.5 Blast radius (G61:284–303); ATLAS-GATEWAY-012

"What fails when each dependency is down for an hour. A row is added, with its test (ATLAS-GATEWAY-012), before a new dependency is used" (G61:286). ATLAS-GATEWAY-012: "Every row of §6.1.16 has a fault-injection test (the dependency's fake made to fail) asserting what fails and what keeps working; a new dependency adds its row and its test before it is used" (G61:62).

| Dependency | What fails | What keeps working | Who notices | Mitigation | First build |
|---|---|---|---|---|---|
| Entra ID | New sign-ins and refreshes; On-Behalf-Of, so systems reached as the person; group overage lookups after their 10-minute cache | Calls with tokens already held (verified with cached keys) to systems reached as the gateway or a workload | Everyone, as their tokens expire | None: the firm's identity | Row + test against the token-issuer fake: IN SCOPE |
| Cosmos DB | Writes, budgeted and metered calls, operations, resources, events' handovers | Built-ins and unbudgeted reads for 60 minutes (§6.1.14) | Writers; vendor and warehouse-heavy callers | `degraded_mode` | IN SCOPE (store fake); R13 |
| Blob storage, the audit's WORM container | Every call: none may go unaudited (ATLAS-ACCESS-004) | Health | Everyone | A defect: a second audit container in another storage account (Q62) | IN SCOPE as the audit-sink fake failing |
| Blob storage, other containers | Transfers and exports; last-known-good not refreshed | Everything else | Uploaders, exporters | None needed | LKG part IN SCOPE if LKG is built |
| Log Analytics | `atlas.audit` and usage history; ingestion resumes from the blobs | Every call | Administrators | None needed | DEFERRED |
| Application Insights or the collector | Telemetry past the collector's buffer | Every call | Operators | None needed | DEFERRED |
| Service Bus | Events: connections, and the controllers converging entities and roles; the gateway's own events wait in the change feed | Everything not an event; clients poll each event's history capability (§2.5) | Event users | `controller.event-entities`, `controller.event-rights` | DEFERRED |
| Key Vault | New replicas start (no secrets); cursor key rotation | Running replicas, with secrets read at start | Nobody, unless scaling or restarting | Alert on it | AMBIGUOUS (cursor keys are needed for paging; a secrets fake?) |
| Snowflake | Every Snowflake-backed capability, including handoffs | Everything else | Data users | The domain's switch; breaker | IN SCOPE (DuckDB fake failing) |
| Each vendor API | That vendor's capabilities | Everything else | Its users | Its service's switch; breaker | DEFERRED |
| Astronomer, Azure management | Their actions and platform capabilities | Everything else | Developers, operators | Their switches | DEFERRED |
| Microsoft Graph | `sharepoint.*`, `atlas.directory`, group overage | Everything else | Their users | Their switches | DEFERRED |
| `tars-kb` | `kb.*` | Everything else | Knowledge-base users | Its switch | DEFERRED |
| Ingress, Container Apps | Everything | — | Everyone | None: the platform | DEFERRED |

AMBIGUOUS: the Cosmos row says unbudgeted reads keep working "for 60 minutes"; R13 replaces the single 60-minute bound (§3.8).

---

## 7. Every number in these sections

| Value | Meaning | Source |
|---|---|---|
| `dv`, `ac`, `pd` | The environments; one gateway per environment | G6:11, 60 |
| 9 | Pipeline steps in the spec (6 in the front door, 1 in the service, 2 back in the front door) | G61:47, 76–102 |
| 9 | CORE §8 steps (a different order) | CORE:172–180 |
| 11 | Edge middleware layers | G67:100–112 |
| 1 MiB | Body limit (bootstrap, probes exempt) | G67:109 |
| 64 KiB | Header bytes | G67:268 |
| 10 s | Read-header timeout | G67:266 |
| 5 minutes | Idle timeout | G67:267 |
| None | Read and write timeouts | G67:265 |
| `:8000` | Serving address | G67:264 |
| 2 | `Retry-After: 2` while draining | G67:108 |
| 5 s, 30 s | Shutdown: wait 5 s after readiness false; running requests finish for 30 s | G67:270 |
| 2 s | Cosmos DB readiness check | G67:272 |
| 5 minutes | Token leeway | G67:140 |
| `RS256` | Only algorithm | G67:138 |
| 10 minutes | Group overage cache (Graph) | G67:142; G61:290 |
| 1–255 chars, `A-Z a-z 0-9 - _ .` | `Idempotency-Key` | H96:35 |
| Seconds since the epoch, `@1793318400` example | `Atlas-Deadline` | H96:34, 58 |
| 5 s | A crashed service's capabilities `unavailable` within five seconds | G61:56 (GATEWAY-006) |
| 45 s | Replica ready with Cosmos down, serving LKG | G61:58 (GATEWAY-008) |
| 30 s | Replica waits up to 30 s for the live snapshot at start | G61:257 |
| 60 minutes | Degraded mode maximum (OVERRIDDEN BY REVIEW R13: separate bounds, values not set) | G61:59, 268 |
| ÷ replicas, rounded up | Per-replica limit in degraded mode | G61:264 |
| 60 seconds | A switch reaches every replica | G61:61, 272 |
| 60 seconds | A catalogue change reaches long-lived clients (objective) | G61:233 |
| 99.9% | Catalogue availability objective | G61:233 |
| p99 < 300 ms | Control-plane latency, excluding the service's own time | G61:233 |
| 5 consecutive failures in 30 s | Breaker opens | G61:197; G67:154 |
| 15 s | Breaker probe ("every 15 seconds" vs "one probe after 15 s": AMBIGUOUS) | G61:197; G67:154 |
| 5 s | Breaker open/closed reflected in availability | G67:154 |
| 1 in 10 per minute | Retry budget per dependency | G67:153 |
| 2× concurrency | Bulkhead queue depth; load-shedding queue depth | G67:155; G61:199 |
| N and N-1 | Service protocol versions (DEFERRED) | G61:177, 202 |
| 426 | Client below the protocol minimum (`upgrade_required`) | G61:202 |
| 1 minute | `catalogue` controller resync | G61:241 |
| 10 minutes | `event-entities` resync | G61:242 |
| 5 minutes | `event-rights` resync | G61:243 |
| 30 seconds | `operations` resync | G61:244 |
| 5 minutes | `settlement` resync | G61:245 |
| 10 minutes | `publications` resync | G61:246 |
| 10 s | `subscribe` waits for `ready` | G61:248 |
| 10 s / 30 s | Operation lease renewed / lapses | G61:250 |
| 3 | Maximum resumes of a taken-over operation | G61:250 |
| 1 minute | Takeover or failure after a replica is killed (OPERATION-001) | G61:65 |
| > 24 hours | Retention must cover work longer than 24 hours | CORE:212 |
| 1 hour | "down for an hour", the blast-radius horizon and review Q3 | G61:286; G66:90 |
| SHA-256 | LKG snapshot hash | G61:255 |
| `atlas-snapshots` | LKG Blob container | G61:255 |
| 2026-10-23 | The earlier Production database saturated when reporting shared its throughput | G61:145 |
| 499 | Logged for a caller that went away (answered nothing) | G67:130 |
| 6 (+L0 = 7 levels) | "six layers" L0–L6 (R24) | G65:9–16 |
| 9 | ATLAS-PACKAGES requirements 001–009 | G65:53–61 |
| 14 + 3 | ATLAS-GATEWAY-001–014, ATLAS-OPERATION-001–003 | G61:51–67 |
| 15 / 15 / 15 / 6 | Principles / review questions / constitution lines / deliberate differences | G66 |
| 20 | `atlas.*` built-ins | CAT:158; HB:204 |
| 1–500, default 100 | `atlas.catalog` `page_size` | D93:50 |
| 1–200 | `atlas.search` `words` length | D93:51 |
| 200–20000, default 4000 | `atlas.guide` `max_tokens` | D93:53 |
| 2–100 | `atlas.directory` `words` length | D93:62 |
| 1–1000, default 200 | `atlas.audit` `page_size` | D93:64 |
| 8 / 8 MiB | Client handoff chunk downloads at a time; transfer parts at a time and size per part (DEFERRED) | G67:237–238 |
| Unspecified | The pre-auth limit, the in-flight limit, per-hop deadline overhead, the upstream throttle values (AMBIGUOUS) | — |

---

## Appendix: overrides by R number (only those touching these sections)

| R | Affects | Change (REV) |
|---|---|---|
| R01 | `atlas.describe` caching | The contract by fingerprint is cached apart from the document revision; caller state is private and uncached (REV:59) |
| R02 | Binding routing | Routing is current metadata (REV:60) |
| R03 | The admission `compatible` condition, principle 05 | The smaller compatibility model (REV:61) |
| R04 | Pipeline validate step | Validate the original before transformation; unknown fields refused (REV:62) |
| R05 | Pipeline step 4 | Resource and data scope enforced now (REV:63) |
| R06 | Approval and confirmation | Trusted receipts (REV:64) |
| R07 | Limit reservation | Budget authority per metered provider (REV:65) |
| R08 | ATLAS-OPERATION-002, §6.1.13 takeover | The key is never released; fencing (REV:66) |
| R09 | Step 8 audit, §6.1.12 "one record per call", principle 14 | Admission, outcome and settlement facts (REV:67) |
| R13 | §6.1.14 degraded mode, switches | Separate freshness bounds (REV:71) |
| R14 | Resource ETags | Full-representation ETag; resources after the journey (REV:72) |
| R15 | GATEWAY-004, "change feed is the outbox" | Explicit outbox facts (REV:73) |
| R17 | §6.1.4 #2 cancellation | Deadlines and retention separate (REV:75) |
| R20 | §6.1.6, 6.1.7, GATEWAY-003, Rule 3, Q15 | In process first (REV:78) |
| R24 | §6.5 layering | `layers.toml`; same-layer imports allowed (REV:82) |
| R26 | Client credentials | Bound credentials, serialised refresh (REV:84) |

## Files read

In full:

- `/Users/ashleycottrell/repos/atlas-skeleton/AGENTS.md`
- `/Users/ashleycottrell/repos/atlas-skeleton/docs/pass-2/REVIEW.md`
- `.../atlas-spec/06-inside-the-gateway.md`
- `.../atlas-spec/06-1-front-door-and-services.md`
- `.../atlas-spec/06-5-packages.md`
- `.../atlas-spec/06-6-principles.md`
- `.../atlas-spec/review/ATLAS-CORE.md`. The whole file was read; §8 is the assigned part.
- `/Users/ashleycottrell/repos/atlas-skeleton/sdk/go/layers.toml`

In part, as cited: G67, G62, D93, H96, C97, CAT, HB, D26 and DR (lines 60–192), and `binding.schema.json` (first 80 lines).
