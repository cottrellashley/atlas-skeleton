# Digest: the clients (Go client `atlas`, `atlas-engine`, CLI, Python SDK, Node SDK)

Written 2026-10-31 for the pass-2 build of `/Users/ashleycottrell/repos/atlas-skeleton`. REVIEW.md governs it.

## 0. Sources, citation keys and how to read this

| Key | File (read in full) |
|---|---|
| C | `~/Documents/ChatGPT/creation/atlas-spec/05-clients.md` (608 lines) |
| P7 | `~/Documents/ChatGPT/creation/atlas-spec/09-7-clients.md` (201 lines) |
| P5 | `~/Documents/ChatGPT/creation/atlas-spec/09-5-resolution.md` (128 lines) |
| P8 | `~/Documents/ChatGPT/creation/atlas-spec/09-8-conformance.md` (172 lines), read because C:230 and P7 delegate the runner protocol to §9.8.3 |
| AC | `.../atlas-spec/review/ATLAS-CORE.md` (245 lines, read in full) |
| DR | `.../atlas-spec/review/DESIGN-REVIEW.md` (192 lines, read in full) |
| HB | `.../atlas-spec/review/CAPABILITY-HANDBOOK.md` lines 1-110 |
| RV | `/Users/ashleycottrell/repos/atlas-skeleton/docs/pass-2/REVIEW.md` (100 lines) |
| AG | `/Users/ashleycottrell/repos/atlas-skeleton/AGENTS.md` (72 lines) |
| REF | `/Users/ashleycottrell/repos/atlas-skeleton/spec/protocol/references/` (paths below are relative to it) |
| Q | `REF/QUESTIONS.md` (157 lines): the readings the references take where the spec is silent |

The references I read in full: `schemas/token-store.schema.json`, `schemas/lock.schema.json`, `schemas/bootstrap.schema.json`, `schemas/problem.schema.json`, `README.md`, `QUESTIONS.md`, `vectors/problems.json` (all cases), and the scenarios `CORE-BOOT-002`, `CORE-SIGNIN-001` to `-005`, `CORE-CALL-005` and `CORE-LOCK-001`. I also read pass 1's engine and Python face from branch `pass-1` (`sdk/go/engine/engine.go`, `sdk/go/cmd/atlas-engine/main.go`, `clients/python/fiatlas/_engine.py`, `client.py`, `errors.py`), because RV:19 and RV:92 bring them back in step 4. I give them only as pointers. They are not the spec.

The markers used:
- **IN SCOPE**: the first build (the journey, through CLI, Python and Node) must implement it.
- **DEFERRED**: the first build does not implement it.
- **OVERRIDDEN BY REVIEW (Rnn)**: the review, or RV, changes what the spec says.
- **AMBIGUOUS**: the sources are unclear, contradict each other, or are silent. Each possible reading is given.
- **LEFT TO OWNERS**: RV:94-100 leaves the decision to the owners. A fixture states the choice it fixes.

### 0.1 The first build's client scope at a glance

| Item | Status | Source |
|---|---|---|
| CLI, Python and Node from the first release, one request record in all three | IN SCOPE | RV:12; AC §6 (AC:99-140); R11 |
| The Go client (`sdk/go`), which the CLI and the engine use | IN SCOPE | C:550 rule 1; RV:3-19 |
| `atlas-engine`, brought back from `pass-1` onto the new protocol, not rewritten | IN SCOPE | RV:19, RV:92; AC:140 |
| Core level (JSON) | IN SCOPE | R11 (DR:122): "Core can return JSON; Arrow and direct downloads are optimizations with identical logical meaning" |
| Streaming (NDJSON plus end line), with explicit completion | IN SCOPE | AC:206 "Streaming is the default data path"; RV:50 point 5 "incomplete streams" |
| Columnar (Arrow), as an optimisation of the same meaning | IN SCOPE as an optimisation; never the only path | R11 (RV:76) |
| Durable operations: `call` returns an operation handle; `operation` get, wait, cancel | IN SCOPE "where that journey needs them" | AC:238; AC:132 |
| Handoff (Snowflake chunks) | DEFERRED | RV:15; AC:241 "Direct Snowflake chunks and Service Bus remain separately gated proofs"; DR:176 |
| Events (`subscribe`, `publish`, §9.7.8) | DEFERRED | RV:15 "Handoff, events and Service Bus ... Deferred" |
| Bulk (`export`, `upload`), transfers | DEFERRED (no journey capability needs them) | RV:35-42, the journey list |
| Broker (`run_ticket`) | DEFERRED | P7:34; R10 (RV:68) "Client SQL tickets stay deferred" |
| Generated typed wrappers (`atlas lock gen go\|python\|ts`, `.pyi` stubs) | DEFERRED; "MAY" | P7:152; C:583 |
| Client extras (Parquet, cache, Airflow, files, git, GitHub, Go adapters, harness commands) | DEFERRED, except the CLI's output formats and sign-in | AC:245 "harness lifecycle tools" not required; DR:178 "Every ... client extra implemented simultaneously" is deferred |
| `atlas admin`, `atlas update`, `atlas setup`, `atlas doctor` | DEFERRED | not on the journey (RV:35-42) |

AMBIGUOUS: whether any journey capability uses an `operation` delivery. Readings: (a) none of `portfolio.holdings`, `.list`, `.search` or `data.status` does, so operation handles are built only to the Core contract and tested with `conformance.slow`; (b) the profile authors give one (for example a large holdings call) an operation. The profiles, written in step 2, decide.

---

## 1. The client's obligations

### 1.1 Connection: choosing the gateway and environment (§9.7.2) — IN SCOPE

The order, first set wins (P7:62-68):

1. The gateway URL or environment given to `connect`.
2. The environment variable `ATLAS_URL` (a gateway URL).
3. The environment variable `ATLAS_ENV` (an environment name).
4. `env` in the configuration file `<ATLAS_HOME>/config.toml`.
5. Production.

- `ATLAS_HOME` is the environment variable of that name if it is set, else `~/.atlas` (P7:70).
- An environment name maps to its gateway URL through `config.toml`'s `[environments.<name>] url`, or the built-in names `dv`, `ac` and `pd` (P7:70).
- "A gateway run for development or conformance is named by its URL" (P7:70).
- The bootstrap is `GET /.well-known/atlas`. It is served without sign-in, with a strong ETag (`bootstrap.schema.json`:5). The client fetches it unsigned, revalidates it with `If-None-Match`, and builds every URI from it (P8:85, CORE-BOOT-001). CORE-SIGNIN-001 asserts that the bootstrap GET carries no `Authorization` header.
- The bootstrap's required members (`bootstrap.schema.json`:7-15) are `protocols`, `minimum_protocol`, `environment`, `servers`, `builtins`, `sign_in` and `catalog_etag`.
  - `environment` pattern `^[a-z](-?[a-z0-9])*$`. Its values are "dv, ac, pd, local or conformance" (:32-36).
  - `sign_in` requires `authority`, `client_id`, `scopes` and `token_endpoint`. `authorization_endpoint` and `device_authorization_endpoint` are optional (:120-164). "Every endpoint is given, so a client never builds one" (Q-DOC-2).
- CORE-BOOT-002's `config.toml` form, verbatim: `env = "cfg"\n\n[environments.cfg]\nurl = "{gateway4}"\n\n[environments.envc]\nurl = "{gateway3}"\n`. The scenario proves: a `connect` argument beats `ATLAS_URL`; `ATLAS_URL` beats `ATLAS_ENV`; `ATLAS_ENV` (`envc`) beats `config.toml`'s `env` (`cfg`).
- The CLI takes `--env`, `ATLAS_URL` and `ATLAS_ENV` "on any command" (P7:42).

**The context rule (the review's proposal).**
- HB:33: "one configured organization/tenant and environment are the default for a connection. The gateway derives the authenticated tenant and principal from trusted authentication, verifies the selected organization, and returns the effective context in `whoami`. A request payload cannot impersonate a principal or choose an arbitrary tenant. Switching organization, account or environment is explicit. Caches and saved connection state are isolated by that effective context."
- AC:50: "One configured context may be selected automatically. If several accounts, tenants or environments could apply, the client requires a deliberate selection rather than silently choosing one. CLI and SDK credential providers share the selected context where their runtime permits; browser authentication uses its host application's supported identity flow."
- HB:31: "The CLI, Python SDK and Node SDK should resolve the same connection context and use the same sign-in for a person's session. A scheduled workload uses its own identity."
- LEFT TO OWNERS: "The organisation and tenant switching contract (handbook, 'establish the context once')" (RV:98). HB:33 adds that §9.7 does "not yet define a complete organization/tenant switching contract".

AMBIGUOUS, step 5 "Production": for a client with no configuration, the default sends a person's sign-in to Production. R26 requires the gateway to be trusted for the credential. Readings: (a) keep the default, with `pd` a built-in, trusted name; (b) under HB:33 and AC:50, refuse to choose silently when no context is configured. The spec says (a). The review does not override step 5 explicitly.

### 1.2 Sign-in and the shared store (§9.7.3) — IN SCOPE, OVERRIDDEN BY REVIEW (R26, R11)

The spec's rules (P7:74-81):
- "One file per environment: `<ATLAS_HOME>/state/token-<env>.json`, valid against `token-store.schema.json` (`access_token`; `expires_at`, seconds since the epoch; `refresh_token`)."
- "The file and its directory are readable and writable only by the person (mode `0600` and `0700`); a client refuses to use a file with wider permissions."
- "A client writes the file atomically (to a temporary file in the same directory, then renamed)."
- "A token within 60 seconds of `expires_at` is refreshed first, with the `refresh_token`, at the bootstrap's `sign_in` authority."
- "When there is no usable token, a client with a person present runs the sign-in of §6.2 (browser with PKCE, or device code); without a person, it fails with `unauthenticated`."
- "A workload passes a token to `connect`, or sets `ATLAS_TOKEN`; it never writes the store."

`token-store.schema.json` (REF/schemas, lines 1-25):

| Member | Type | Required | Constraint |
|---|---|---|---|
| `access_token` | string | yes | `minLength` 1 |
| `expires_at` | integer | yes | "Seconds since the epoch." |
| `refresh_token` | string | no | `minLength` 1 |

The schema does not set `additionalProperties` (it is open). `$id` `https://atlas.jhg.com/protocol/1.0/token-store.schema.json`.

What the scenarios add (REF/scenarios):

| Scenario | Exact behaviour |
|---|---|
| CORE-SIGNIN-001 | `<env>` is the bootstrap's `environment`, so the conformance file is `token-conformance.json`. The call carries `Authorization: Bearer ct-from-file`. The bootstrap GET carries no `Authorization`. |
| CORE-SIGNIN-002 | `expires_at` now+30 s: the client refreshes **before** the call. It POSTs a form to the token endpoint (`/oauth/token` on the conformance gateway) containing `grant_type=refresh_token` and `refresh_token=rt-1`. It rewrites the file with the new `access_token` and `refresh_token`, mode `0600` in a `0700` directory, and never sends the old token. |
| CORE-SIGNIN-003 | A `401` with `WWW-Authenticate: Bearer realm="atlas", error="invalid_token"`: refresh, then repeat **once**. A second `401` is `unauthenticated`. Expected: 4 whoami requests and 2 token requests in total. |
| CORE-SIGNIN-004 | A claims challenge (`WWW-Authenticate: Bearer realm="atlas", claims="..."`) with no person: `unauthenticated`, with `details: [{"challenge": "<the whole WWW-Authenticate value>"}]`. **No refresh** (0 token requests). One whoami request. "A refreshed token cannot satisfy a claims challenge; only a person signing in again can." |
| CORE-SIGNIN-005 | File mode `0644`: refused. With no person the call is `unauthenticated`, and the token is never sent. |
| P8:88 CORE-SIGNIN-002 | "the file is rewritten atomically, mode `0600`" |

- The refresh goes to `sign_in.token_endpoint` (RFC 6749 §6), per `bootstrap.schema.json`:149-153.
- The conformance gateway's `sign_in.authority` "is itself, with a token endpoint that accepts the suite's refresh tokens" (P8:57).

**OVERRIDDEN BY REVIEW (R26)**: RV:84: "Credentials are bound to the trusted gateway, authority, audience, tenant or account, and scopes. Refresh is serialised, and an unauthenticated bootstrap cannot redirect a credential." DR:153 gives the evidence: "§9.7 selects arbitrary gateway URLs but keys the shared token file only by environment and refreshes at the bootstrap's authority". AC:52: "Credentials are bound to a trusted gateway, authority, audience, tenant/account and resource scopes. Concurrent refreshes are serialized. An unauthenticated bootstrap cannot direct a client to send an existing token or refresh credential to an untrusted server or authority." Step 4.

**OVERRIDDEN BY REVIEW (R11)**: RV:76: "Credentials come from each runtime's own provider". DR:122: "Use runtime-appropriate credential providers". DR:124: "A browser client needs neither a token file nor local disk output". AP-CLIENT-002 ("A token written by any client is used by every other client on the same machine", P7:196) therefore holds only "where their runtime permits" (AC:50).

What the build must do to honour R26 (the obligations; the mechanism is not specified):
1. Bind each stored credential to the trusted gateway, `authority`, audience, tenant or account, and scopes, not to the environment name alone.
2. Never send a stored token or refresh token to a gateway, or to a `token_endpoint` or `authority`, that a bootstrap fetched without authentication names, unless that destination is already trusted for that credential.
3. Serialise refresh: two concurrent refreshes of one credential, in one process or across processes, must not both redeem the refresh token.

AMBIGUOUS, the R26 store format. Readings:
- (a) Keep `token-<env>.json`, valid against the open `token-store.schema.json`, and add binding members (gateway URL, authority, audience, tenant, scopes). The schema allows extra members.
- (b) Key the file name by something beyond `<env>`.

The references still use (a)'s file name (Q-CLI-1). Changing it breaks every CORE-SIGNIN vector's path.

AMBIGUOUS: R26 conflicts with CORE-BOOT-002. In that scenario one `token-conformance.json` is sent to four different listeners ({gateway} to {gateway4}) "because each listener serves its own bootstrap, whose environment is conformance". Under R26 the same credential may go to four gateway URLs only if each is trusted for it. Readings: (a) the trust binding is the `authority` plus audience, which the four share, so the scenario still passes; (b) the binding includes the gateway URL, so the scenario must change. Decide in step 4 and record it in Q.

AMBIGUOUS: how to serialise refresh across processes (CLI, engine, Node on one machine). R26 does not say. Readings: an OS file lock beside the token file, or a lock file with atomic create. This is a design choice; record it.

AMBIGUOUS: what "a person present" means per client. The runner protocol uses `options.person: true` (P8:53). For the CLI, a terminal is the natural reading (C:445 rule 2 keys output on "when standard output is not a terminal"). For the Python engine, a notebook is not a terminal to the engine. Not specified.

Interactive sign-in (`atlas login`: browser with PKCE, RFC 7636/8252, or device code, RFC 8628): IN SCOPE for the CLI (C:493). For the hermetic build, the token comes from fixtures and the conformance token endpoint. Real Entra is step 5 (RV:33).

### 1.3 Discovery and the client's caches (§9.7.5) — IN SCOPE, OVERRIDDEN BY REVIEW (R01)

The spec's rules (P7:102-105):
- "Profiles are cached by `exact-id`, forever: a fingerprint's profile never changes. The cache is at `<ATLAS_HOME>/cache/profiles/` for clients with a file system, in memory otherwise."
- "The catalogue is cached per environment and principal for at most one hour, revalidated with its ETag at every `connect`, and refreshed when a long-lived client's subscription to `atlas.catalog.changed` reports a change." The subscription is part of Events, which is DEFERRED.
- "A client recomputes every profile's fingerprint (§9.4) and refuses a mismatch." CORE-DISC-001 (P8:92): "a wrong one is `invalid_document`".
- "**A capability the client has not seen is never refused by the client.** An address missing from the cached catalogue is looked up with `atlas.describe` before anything is answered; only the gateway says `not_found`. A long session sees a capability added mid-session on its next search or call, with no restart." This is AP-CLIENT-004 (P7:197) and ATLAS-CLIENTS-002 (C:72). It is the basis of acceptance point 4 (RV:49).
- CORE-DISC-001: catalogue and search "Pages are followed by cursor". CORE-DISC-002: "A profile is fetched once per `exact-id`, then served from the cache" (P8:92-93).
- CORE-GUIDE-001: "The client asks for its own syntax (`client`) and returns the Markdown unchanged" (P8:94).

**OVERRIDDEN BY REVIEW (R01)**: RV:59: "The contract (by fingerprint) is cached apart from the document revision. Describe's caller state (governance, availability) is private and uncached. A guide's identity includes the prose revision." DR:36 adds: "Reconcile the catalogue's one-hour client cache with the HTTP `no-store` rule." AC:156-160 gives the three revisions:

| Revision | What it identifies | Cache rule |
|---|---|---|
| Contract fingerprint | Immutable semantic and schema contract | May be retained and bundled with the program |
| Document revision / HTTP validator | The actual served description or representation | Changes when the served content changes |
| Policy and runtime information | Current access, limits, routes, supported delivery and health | Revalidated as appropriate; never authority to execute later |

For the client this means:
- The `<ATLAS_HOME>/cache/profiles/` cache may keep only the fingerprinted contract.
- It must not keep describe's governance or availability, nor any access answer as authority to execute (DR:38 acceptance: "no cached access response authorizes the call").
- A cached guide is keyed by its prose revision as well.

AMBIGUOUS: the catalogue's one-hour cache. DR:36 says to reconcile it with `no-store` but does not say how. Readings: (a) keep it, revalidated with `catalog_etag` at every `connect` (P7:103), as a ranking aid only and never as permission; (b) do not cache the catalogue (no-store). Under (a) the client must still never refuse an uncached address (P7:105).

The HB caveat (HB:33): "Caches and saved connection state are isolated by that effective context." So the cache key includes the environment and the principal at least (P7:103 already says "per environment and principal").

### 1.4 The call (§9.7.4) — IN SCOPE, with R02, R04 and R11 overrides

`call(address, request, options)`, verbatim steps (P7:85-98):

1. "Parse `address` (§9.1.3); fail with `invalid_request` if it does not match."
2. "Find the profile: if the application has a lock with this capability, its `exact-id`; else, if the address is pinned, the newest cached profile of that version, else of the current stable version. Fetch it with `atlas.describe` if not cached (9.7.5)."
3. "If the capability has a trait marked critical that the client does not implement, fail with `critical_trait_unsupported` without calling."
4. "Find the interaction; fail with `not_found` if the profile has none of that name."
5. "Validate `request` against the interaction's request schema (§9.2.5); fail with `invalid_request` if invalid."
6. "Choose the binding: the first of the interaction's bindings whose protocol, encodings and delivery the client supports."
7. "Build the call (§9.6 or §9.7): the body, `Atlas-Client`, `Atlas-Client-Features`, `Atlas-Profile-Expect` (the profile's `exact-id`), `Atlas-Deadline` (`options.deadline`, else now plus the interaction's `deadline.default`, never beyond `deadline.max`), `traceparent`, and for a non-`read` effect `Idempotency-Key` (`options.idempotency_key`, else a new random UUID, kept for every retry of this call)."
8. "Send it. On a problem: if `retryable` and the request is replayable (it has no streamed body), retry by 9.7.7; otherwise raise it (9.7.6)."
9. "Read `Atlas-Profile`. If its fingerprint is not cached, fetch that profile."
10. "Read the answer by its delivery (§9.6.5), validating each document. For a handoff: download every chunk, decode it by the handoff's `format` into the writer profile's Arrow schema, and check each batch's schema against the writer profile (`invalid_document` on a mismatch). Resolve every record from the writer's profile to the profile of step 2 (§9.5)." (The handoff part is DEFERRED.)
11. "If `Deprecation` was sent, emit the language's warning (Python `DeprecationWarning`, Node `process.emitWarning`, Go the client's warning hook, the CLI on standard error) naming what is deprecated, its sunset and replacement."
12. "Return the result in the language's native form."

The headers a client sends (from §9.6, which I checked only for these lines; another digest owns §9.6):
- `09-6-http-binding.md`:100: "A client sends `Atlas-Client`, `Atlas-Client-Features`, `Atlas-Profile-Expect` (for interactions other than built-ins), `Atlas-Deadline` and `traceparent` on every call, and `Idempotency-Key` on every non-`read` call."
- `Atlas-Client` (`09-6`:31) is a Dictionary: `name` (String), `version` (String), `protocol` (String, `protocol-ver`). Example (`09-6`:55): `Atlas-Client: name="fiatlas", version="2.3.0", protocol="atlas/1.0"`.
- `Atlas-Client-Features` (`09-6`:32, :52) is a Dictionary of Inner Lists of Tokens: `levels`, `lens`, `deliveries`, `encodings`, `traits` (and `handoff`). Values:
  - `levels` from `core streaming columnar bulk events handoff` (and `broker`, reserved);
  - `lens` from §9.5.3's operations;
  - `deliveries` from `single chunked subscription operation transfer`;
  - `encodings` from `json ndjson arrow csv parquet`;
  - `traits` lowercased.
- CORE-CALL-001 (P8:96): "Every header of §9.6.3 is present and parses; built-ins carry no `Atlas-Profile-Expect`."
- CORE-CALL-002: "An invalid request fails `invalid_request` with its pointer, and nothing is sent."
- CORE-CALL-003: "The client follows the binding; it never builds a path."
- CORE-CALL-004: "Fails a write with `503` once. The retry carries the same `Idempotency-Key`."

The header encodings, from Q (read them as the references do):

| Q | Reading |
|---|---|
| Q-HDR-2 | "Atlas-Deadline is a Date, so whole seconds. A client rounds its deadline down." |
| Q-HDR-4 | "An Integer in Atlas-Confirm is not a Decimal, so it is absent; a sender always writes a decimal point (`1250.0`)." |
| Q-HDR-5 | "No Accept header, or `*/*`, takes the binding's first encoding." |
| Q-DOC-4 | "`delivery` is the interaction's, and a binding may override it" |
| Q-VAL-1 | invalid_request's `target` is the first failure, sorting by pointer then keyword, by code point (`/10` before `/2`) |

**One shape** (P7:38): "The request is the same JSON record in every client, member for member: `columns`, `filters`, `as_of` and every other parameter the request schema declares are members of the request, never options. Options carry only what §9.6 sends as headers or what the client does itself (deadline, idempotency key, execution preference, accepted encodings, the output form)."
- Python names the options `deadline`, `idempotency_key`, `execution` and `accept`. TypeScript names them `deadline`, `idempotencyKey`, `execution` and `accept`. Go uses `opts...` (P7:50).
- HB:102: "Deadlines, idempotency keys, transport preferences and local output destinations are call/client options."

A bare id (no `@vN`) "is allowed at a terminal and in a notebook; a program pins through its lock (9.7.9)" (P7:58).

**OVERRIDDEN BY REVIEW (R11), the call lifecycle**. RV:76: "`call` executes once and returns a result or operation handle. Core is JSON, and Arrow is an optimisation of the same meaning." AC:132: "The canonical `call` executes once and returns a result or operation handle. Reading that handle again never silently submits another source query: it either re-reads a retained result or reports that the result cannot be replayed. `explain` is a separate, non-fetching interaction. A lazy request builder may exist as a convenience, but must be named and documented separately from execution." DR:124 gives the acceptance test: "Count source executions when reading a result twice."

**OVERRIDDEN BY REVIEW (R02), step 2 and step 6**. RV:60: "A lock pins semantic contracts. Routing is fetched as current metadata. 'No discovery at runtime' means no new business-route selection." DR:44 adds: "Package reader contracts with the lock; refresh current routing independently." DR:46 gives the acceptance test: "Move a provider and update the guide. A locked program still calls the same business operation with the same request meaning, uses the new route and obeys current grants." AC:164: "Fetching an updated binding or required writer contract is metadata resolution."

AMBIGUOUS, R02 against §9.4: a profile's `bindings` are inside the fingerprint. §9.4.1 removes only `fingerprint`, `revision`, `usage`, `identity.title/summary/owner`, `errors[].description/next`, `evolution.changelog` and schema prose (`09-4-fingerprints.md`:18-26). So "routing fetched as current metadata" needs either:
- (a) bindings removed from the contract fingerprint, a step-2 change under R01/R02 that alters every fingerprint vector; or
- (b) the client takes step 6's bindings from the **current** served profile (`atlas.describe` by id, uncached) while it resolves records against the **locked** contract.

CORE-LOCK-001 asserts zero `atlas.describe` calls with `by: id` for locked entries, so (b) breaks that scenario as written. Step 2 must decide.

**OVERRIDDEN BY REVIEW (R04), step 5**. RV:63: "A request is validated under its own contract before any transformation, and unknown request fields are refused, never dropped." The client's step 5 already refuses unknown members, because request objects are closed (Q-PROF-7: "Every other object of a request is closed"). The client must never strip an unknown member to make a request pass. DR:64 acceptance: "A misspelled field is rejected".

### 1.5 Resolution (§9.5): what a client must do — IN SCOPE, narrowed by RV

**OVERRIDDEN BY REVIEW (R03, R04; RV:90)**: "`protocol/resolve` becomes the experimental resolver ATLAS-CORE §7 allows. A client is obliged only to aliases, defaults, UNKNOWN and projection. Its vectors still pass, but no served profile may depend on `convert_unit`, `promote` or `drop` within a version." AC:152: "Existing experimental lenses can remain experiments without becoming universal client obligations."

So the client's obligation is P5's step 1, step 2's version check, step 5's projection, steps 6-15 (matching, aliases, defaults, null, required) and §9.5.5's conversions (UNKNOWN). The lens application of step 4 is experimental.

P5 §9.5.2, the algorithm (P5:22-38), verbatim where it binds:
1. "If `fingerprint(W) = fingerprint(R)`, or the capability has the trait `DynamicSchema` (§9.3), return *x* unchanged."
2. "If *R* and *W* are not profiles of the same `id@vN`, fail with `resolution_failed`." And: "A client never resolves across versions: a reader pinned to `@v1` calls `@v1`."
3. "Let *y* be a copy of *x*."
4. Apply the lenses: backward from newer *W*, in reverse order; forward from older *W*, in order. EXPERIMENTAL per RV:90.
5. "**Match against the reader's schema.** Let *S* be *R*'s schema for this record, **projected**: when the record is a row of a table answer to a request that named `columns`, *S* keeps only the properties named there (as the reader names them), in `properties` and in `required`." Then build `z = {}`, and for each property *p* of `S.properties`, **in order**:
6. "If *y* has a member named *p*, take its value."
7. "Else, if *y* has a member named one of `S.properties[p].x-atlas-aliases`, take the value of the last such alias in that list that is present."
8. "Else, if `S.properties[p]` has `default`, take the default."
9. "Else, if `S.properties[p]` allows `null` and *p* is in `S.required`, take `null`."
10. "Else, if *p* is not in `S.required`, leave *p* absent and continue."
11. "Else, fail with `resolution_failed`, `target / p`."
12. "**Convert** the value to `S.properties[p]` by 9.5.5, failing with `resolution_failed`, `target / p`, if it cannot be."
13. "Set `z[p]` to it."
14. "Members of *y* that no property of *S* took are dropped."
15. "Return *z*."

§9.5.5, conversion to the reader's type (P5:86-96):

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

§9.5.3, the lens table (EXPERIMENTAL; P5:44-53):
- `rename {from, to}`, `alias_value {field, from, to}`, `convert_unit {field, from, to}`, `promote {field, from, to}`, `default {field, value}`, `drop {field}`.
- "A lens operation whose `field` is absent from the record does nothing, except `default` forward. `null` passes through every value operation unchanged."
- §9.5.4's units: `bp`, `percent`, `fraction`, `years`, `days`, `seconds`, `count`, `currency_unit`. Exactly one multiplication or division. A `Decimal(p,s)` uses exact decimal arithmetic, ties to even.

§9.5.6, where resolution runs (P5:100-106):
- The gateway resolves a request, which the client wrote under `Atlas-Profile-Expect`.
- The client resolves a result, resource, plan, receipt or operation result (written under `Atlas-Profile`) "against its lock or its newest fetched profile".
- "When the gateway knows from `Atlas-Client-Features` that the client lacks a lens operation needed for the chain, it resolves the result to the client's expected profile itself before sending it, and says so with `Atlas-Profile` set to the client's expected `exact-id`."

AMBIGUOUS: must a client implement step 4's `rename`, `alias_value` and `default` lens operations? Readings:
- (a) No. RV:90 lists only "aliases, defaults, UNKNOWN and projection" (schema-level `x-atlas-aliases`, `default`, `UNKNOWN`, `columns` projection). A client sends `lens=()` in `Atlas-Client-Features`, and §9.5.6 makes the gateway resolve for it.
- (b) Yes for the exact-meaning operations (`rename`, `alias_value`, `default`), since R03 keeps "aliases for exact renames" (RV:61).

(a) is the smallest client and keeps Node simple, but the gateway must then implement §9.5.6. The Go client keeps `protocol/resolve`, whose vectors still pass (RV:90). So Go can declare the lenses it has, and the Python engine inherits them. Node may declare none. Parity of results still holds, because §9.5.6 makes the gateway answer already resolved.

§9.5.7, columns: "A client at the Columnar level resolves an Arrow record batch column by column ... the result MUST equal resolving each row by 9.5.2" (P5:110). AP-RESOLVE-002 (P5:128): column-wise equals row-wise "as a table of one row and as a table of many".

The readings that bind clients:
- Q-ARROW-1: the order of `properties` is document order. "A reader must parse `properties` keeping member order (Go's `map[string]any` does not)." This matters for step 5's "in order" and for Arrow field order.
- Q-RES-1: the target is always the top-level member.
- Q-RES-4: a valid nested object is kept unchanged, unknown members and all.
- Q-DOC-1: "every enum in a document the gateway sends includes `UNKNOWN`, and a client maps a value it does not know to it (§9.5.5) before validating."

The vectors are `REF/vectors/resolution.json`. AP-RESOLVE-001: "Every client and the gateway produces, for every case in the resolution vectors, exactly the expected record, or a problem with the expected `code` and `target`" (P5:127). AMBIGUOUS: does Node have to pass the lens cases (convert_unit, promote, drop), given RV:90? Reading: Node passes the alias, default, UNKNOWN and projection cases, and declares no lens operations. Its lens cases are then covered by the gateway (§9.5.6). Record it.

Python's face does no resolution: the engine does it (C:159, C:200 rule 1).

### 1.6 Problems as errors (§9.7.6) — IN SCOPE

"Each problem becomes the language's error, carrying every member of the problem document" (P7:109):

| Language | Form (P7:111-116) |
|---|---|
| Python | "An exception `AtlasProblem` (subclass per `code` where the client defines one), with attributes for each member" |
| TypeScript | "A thrown `AtlasProblem extends Error`, with each member as a property" |
| Go | "An `*atlas.Problem` implementing `error`, matchable with `errors.As`, with each member as a field" |
| CLI | "The problem on standard error (as JSON with `--json`), and the exit code below" |

The problem document (`problem.schema.json`):
- Required: `type`, `status`, `code`, `retryable`, `next`, `request_id`.
- Optional: `title`, `detail`, `instance`, `retry_after` (integer ≥ 0, seconds), `target`, `details` (array), `estimate` (`{amount, unit}`).
- `type` is `https://atlas.jhg.com/problems/<code>`.
- `code` pattern `^[a-z][a-z0-9_]*$`.
- `confirmation_required` requires `estimate`. `in_progress`, `limit_exceeded` and `unavailable` require `retry_after`.
- A `details` item has `code`, `target`, `keyword`, `hop`, `status`, `title`, `detail` and `challenge`.
- It is sent as `application/problem+json` with `Atlas-Error-Code` equal to `code`.
- "A client treats a code it does not know by its status and retryable" (:5). CORE-PROB-002: "It is handled by its `status` and `retryable`".

The registry with CLI exit codes (`REF/vectors/problems.json`; exit codes from P7:118):

| code | status | retryable | CLI exit |
|---|---|---|---|
| `invalid_document` | 400 | false | 1 |
| `invalid_request` | 400 (also 406, Q-DOC-8) | false | 2 |
| `unauthenticated` | 401 | false | 3 |
| `not_granted` | 403 | false | 4 |
| `denied` | 403 | false | 4 |
| `budget_exceeded` | 403 | false | 4 |
| `approval_required` | 403 | false | 4 |
| `confirmation_required` | 428 | false | 4 |
| `not_found` | 404 | false | 5 |
| `not_in_environment` | 404 | false | 5 |
| `idempotency_conflict` | 409 | false | 1 |
| `in_progress` | 409 | true | 6 |
| `resume_gap` | 409 | false | 1 |
| `retired` | 410 | false | 5 |
| `precondition_failed` | 412 | false | 1 |
| `critical_trait_unsupported` | 422 | false | 8 |
| `resolution_failed` | 422 | false | 8 |
| `upgrade_required` | 426 | false | 8 |
| `limit_exceeded` | 429 | true | 6 |
| `output_limit_exceeded` | 200 (end line only) | false | 1 |
| `internal` | 500 | false | 1 |
| `upstream_failed` | 502 | true | 1 |
| `unavailable` | 503 | true | 6 |
| `deadline_exceeded` | 504 | false | 7 |
| any unknown code (e.g. `warehouse_suspended`) | its own | its own | 1 |

P7:118, verbatim: "`0` success; `2` `invalid_request`; `3` `unauthenticated`; `4` `not_granted`, `denied`, `budget_exceeded`, `approval_required`, `confirmation_required`; `5` `not_found`, `not_in_environment`, `retired`; `6` `limit_exceeded`, `unavailable`, `in_progress`; `7` `deadline_exceeded`; `8` `resolution_failed`, `critical_trait_unsupported`, `upgrade_required`; `1` anything else."

**Confirmation** (P7:120): "On `confirmation_required`, a client with a person present shows them the problem's `estimate` and, if they accept, repeats the call once with `Atlas-Confirm` set to that amount and the same `Idempotency-Key`; without a person, it raises the problem." CORE-CONFIRM-001 tests it. `estimate.amount` matches `^[0-9]{1,12}(\.[0-9]{1,3})?$` (Q-DOC-10). Runner: `options.confirm: "accept"` or `"decline"` (P8:53).
- OVERRIDDEN BY REVIEW (R06): RV:64: "A receipt from a trusted interaction, bound to the principal, address, normalised request, plan digest, scope, maximum and expiry." RV:18: "The header is the client's claim only". The client may still send `Atlas-Confirm`, but the gateway must not treat it as consent. That is step 3 and the gateway's concern.
- Clients: the CLI/SDK flow of P7:120 stays the client's side. It is needed only for metered work.
- AMBIGUOUS: whether any journey capability is metered. If none is, the flow is tested only with `conformance.costly`.

### 1.7 Retries (§9.7.7) — IN SCOPE

P7:124, verbatim: "A client retries only a `retryable` problem on a replayable request, at most 3 times, waiting `retry_after` when given, else `min(30, 0.5 × 2^attempt)` seconds plus a random jitter of up to 25%, and never past the call's deadline."
- Replayable means "it has no streamed body" (P7:94, step 8).
- The same `Idempotency-Key` on every retry (P7:93; CORE-CALL-004).
- CORE-RETRY-001 (P8:103): "`429` and `503` with `Retry-After`. At most three retries, honouring `Retry-After`, never past the deadline; a non-retryable problem is not retried."
- The 401 `invalid_token` refresh-and-repeat-once of CORE-SIGNIN-003 is separate from §9.7.7, because `unauthenticated` is not retryable.
- AG:46: "Retries only of retryable, replayable work, at one hop, within a budget" (`kit/retry`; MECH-006).
- R08 (RV:66): "One logical operation per key for its whole retention, uncertain outcomes included." The client's part is to keep the key for every retry and for recovery (AC:210: "Clients can recover the operation using the same idempotency key").
- AMBIGUOUS: `attempt`'s base, 0 or 1, in `0.5 × 2^attempt`. Not specified. Readings: the first retry waits 0.5 s (attempt 0), or 1 s (attempt 1). Record the choice; all clients must use the same one for parity (RV:50 point 5, "idempotent retries").
- AMBIGUOUS: a retry of a `read` that is metered. AC:198 says "Metered reads that can incur duplicate charges need this treatment too" (an idempotency key). §9.7.4 step 7 sends `Idempotency-Key` only for a non-`read` effect. Readings: (a) unchanged for the journey, whose reads are not metered; (b) send a key on every metered read. That is step 3.

### 1.8 Deadlines and cancellation — IN SCOPE, OVERRIDDEN BY REVIEW (R17)

- `Atlas-Deadline` is `options.deadline`, else now plus `deadline.default`, "never beyond `deadline.max`" (P7:93).
- Retries are never past the deadline (P7:124).
- CORE-DEADLINE-001 (P8:104): "`Atlas-Deadline` is the default, capped at the maximum; the call ends `deadline_exceeded`."
- AG:44: "Every outbound call takes the context and is bounded by the call's deadline".
- Python: "Ctrl+C, a deadline or an `asyncio` cancellation cancels the engine's work for that call, at once" (C:202 rule 3). ATLAS-PY-005: "ends a call within one second, and the engine stops its work" (C:211).
- OVERRIDDEN BY REVIEW (R17): RV:75: "The start deadline, execution deadline and retention are separate. Cancellation is reported as requested or confirmed." AC:210: "An HTTP disconnect during startup does not mean a successfully admitted durable operation was cancelled." AC:212: "The operation reaches `cancelled` only when the provider knows work stopped".
  - For the client, a cancelled `call` that has already received an operation handle must report the cancellation as **requested**, not as `cancelled`, until the operation says so.
  - The operation's `cancel` reports requested or confirmed.
  - AMBIGUOUS: no document member for "requested" is named. Readings: (a) `operation.cancel` returns the operation document, whose state shows whether it is `cancelled` yet; (b) a new member. Step 3 decides.

### 1.9 The lock and pinning (§9.7.9) — IN SCOPE, OVERRIDDEN BY REVIEW (R02)

P7:144, verbatim: "A client reads `atlas.lock` (TOML, valid against `lock.schema.json` once parsed) from the application's root (the current directory or the nearest parent containing one, or a path given to `connect`). Its entries are the reader profiles of step 2. `atlas lock` writes it; `atlas lock upgrade` moves entries forward after checking resolution."

`lock.schema.json` (REF/schemas, lines 1-47):
- Required `capabilities`: an object keyed by `capability-id` (format). Each value is an object with:
  - `version`: integer, `minimum` 1, required;
  - `fingerprint`: string, `^[0-9a-f]{16}$`, required;
  - `dynamic`: boolean, "Its types are not promised (§9.3.2)".
- Optional `extras`: an array of strings, `minLength` 1, "The client extras the application uses (§9.7.12)."
- Description: "A DynamicSchema capability is held only as dynamic = true."
- The schema does not set `additionalProperties`.

CORE-LOCK-001's TOML, verbatim:
```
[capabilities]
"conformance.evolving" = { version = 1, fingerprint = "bc7fde7c0bac077d" }
"conformance.echo" = { version = 1, fingerprint = "f17339273f0617c2" }
```
- It is found from `cwd` `{WORKDIR}/app/src/deep`, in the nearest ancestor `{WORKDIR}/app/atlas.lock`.
- It is asserted that the client makes zero `atlas.catalog`, zero `atlas.search` and zero `atlas.describe` calls with `by: id`.
- `Atlas-Profile-Expect` equals `conformance.evolving@v1#bc7fde7c0bac077d`, so the exact-id is `<id>@v<version>#<fingerprint>`.

**OVERRIDDEN BY REVIEW (R02)**, from AC:162-166:
- "A lock pins capability versions and reader contracts, includes the contracts needed to start, and records their checksums. It fixes the program's selected route in business terms. It does not freeze permissions, backend addresses, source data or live availability."
- "'No discovery at runtime' should mean no search and no new business-route selection."
- "For reproducible data, a lock is insufficient."

LEFT TO OWNERS: "The minimum sunset and the lock's format, which the spec still has open" (RV:99).

AMBIGUOUS: "includes the contracts needed to start" (AC:162). Readings: (a) the lock file embeds the contracts themselves, which changes `lock.schema.json`; (b) the contracts are bundled beside the lock (the profile cache) and the lock records their checksums (the fingerprints). The current schema fits (b).

### 1.10 Caching of results — OVERRIDDEN BY REVIEW (R18); persistent result cache DEFERRED

- The spec: Python's `extras.cache` "Keeps results in memory or on disk for a session, keyed by address, request and fingerprint" (C:268).
- OVERRIDDEN BY REVIEW (R18): RV:76: "Results are private to their caller; any reuse is keyed by authorisation, environment and data revision, and reauthorised." DR:145: "Initially avoid automatic persistent or cross-user reuse, especially licensed data." AC:230: "For the first release, keep results private to their caller unless a reviewed contract proves safe sharing."
- So the first build has no client result cache. R11's "re-reads a retained result" (AC:132) is the in-memory handle of one call, not a cache.

### 1.11 The tolerant reader (§9.7.10) — IN SCOPE

P7:148: "A client MUST ignore unknown members of every document, unknown headers, unknown event types and unknown traits not marked critical, and map unknown enum values by §9.5.5. It MUST NOT fail because the gateway is newer than it." CORE-TOL-001 tests it: "Ignored; an unknown enum value reads `UNKNOWN`" (P8:105). Every gateway document's schema is open, and only request bodies are closed (REF README:54).

### 1.12 Where results land (§9.7.13) — IN SCOPE in part, OVERRIDDEN BY REVIEW (R11)

The table (P7:168-173):
- A `record`, a `collection`'s items, an operation or any protocol document lands in memory, as the language's own values.
- A `table` lands in memory as "Arrow record batches in the writer's schema, resolved to the reader's, yielded batch by batch (a whole table only when the caller asks for one)". Between the engine and the Python face, "they cross as an Arrow IPC stream".
- `content` lands in memory by default, and on disk with `options.path`, on a save, or above `options.max_memory`, "default 256 MiB". DEFERRED: no journey capability returns content.
- Transfers, exports and handoff files land on disk, at `<ATLAS_HOME>/downloads/<YYYY-MM-DD>/<request id>/`, with `atlas-manifest.json`. DEFERRED.

OVERRIDDEN BY REVIEW (R11):
- DR:120: §9.7 "requires every table to land as Arrow" against Core's JSON. AC:136: "parity covers the logical value, schema, warnings, request identity, data provenance and completion status. It does not require every runtime to use the same in-memory container or to write files at the same location."
- AC:138: "Large tables are iterators or batch readers by default. Materialising a whole DataFrame or array is an explicit caller choice."
- So AP-CLIENT-005's "a table as Arrow in memory ... at the same disk place" (P7:198) is relaxed to the same logical value.

Exact values (AC:134): "Clients preserve exact decimals, large integers, timestamps and nulls. A Python `Decimal`, a TypeScript exact representation and a JSON decimal string may differ locally while representing the same value. They must not silently turn an exact amount into a binary floating-point value." This is IN SCOPE and is acceptance point 5's "exact decimals" (RV:50). Q-TIME-1: a microsecond instant is written "RFC 3339 in UTC with `Z` and exactly six fractional digits".

**Completion** (AC:206): "A stream has an explicit completion marker; EOF alone is not success. A truncated result must be marked incomplete". The scenarios:
- STREAM-002 / COL-002: no end line, or no final batch, is `upstream_failed`.
- STREAM-003: a problem in the end line is raised after the rows. Q-RUN-6: the failed result may carry the rows read.
- STREAM-004 / Q-DOC-15: truncation is a **warning** `output_limit_exceeded`, and the rows sent are returned.

HB:55 adds: "Inspect completion, effective dates, warnings and any partial/truncated indicator before interpreting the result." So every client must expose the completion status and warnings on the result handle.

**Deprecation**: the warning per language is as in step 11. Q-RUN-3: the warning `code` is `deprecated`. CORE-DEPR-001 (P8:111): "A warning naming what, its sunset and replacement."

### 1.13 Operations — IN SCOPE (Core), to the extent the journey needs them

- §9.7.1: `operation(name)` gives "A handle: `get`, `wait` (with timeout), `cancel`" (P7:29).
- CORE-OP-001 (P8:112): "Answers `202`. Polls no sooner than `Retry-After`, doubling to 30 s; reads `result` or `result_location`; `cancel` reaches `cancelled`."
- Q-RUN-5: "`call` on an `operation` delivery returns the operation document, and `operation.wait` its result."
- Q-DOC-11: the operation document carries `profile`, "the exact-id `result` was written under". The client resolves `result` from it.
- R11, R17: see 1.4 and 1.8.

---

## 2. The Go client API (package `atlas`, module `sdk/go`)

- Module `github.com/jhgonline/fi-app-atlas/sdk/go`; "Lives in `sdk/go`" (C:564).
- The protocol is `sdk/go/protocol`, "its document types generated from §9.3's JSON Schemas; shared with the gateway (§6.5.6)" (C:565).
- "`sdk/go/protocol` imports nothing from the gateway; the gateway imports it. It changes only by the protocol" (C:551 rule 2).
- C:550 rule 1: "The gateway, the CLI, `qfi`, the conformance gateway and `atlas-engine` use `sdk/go/protocol` for every protocol rule and never a second implementation; the CLI, `qfi` and `atlas-engine` make every Atlas call through the Go SDK."
- C:600, open: "The client half (`Connect`, `Call`, `Fetch`) and `sdk/go/protocol` are still to build."
- AG:39 layers: "`protocol` imports only the standard library and Arrow".

The API, as the spec names it (P7:42-56 Go column; C:574-583):

| Operation | Go signature as written | Returns | Status |
|---|---|---|---|
| connect | `atlas.Connect(ctx, atlas.Options{URL, Env, Token})` | a connected client (`c`) | IN SCOPE |
| whoami | `.Whoami(ctx)` | the `atlas.whoami` result | IN SCOPE |
| catalog | `.Catalog(ctx, atlas.CatalogQuery{})` | an iterator over catalogue items | IN SCOPE |
| search | `.Search(ctx, words)` | an iterator over catalogue items. C:575 writes `items, err := c.Search(ctx, "credit curves")` | IN SCOPE |
| describe | `.Describe(ctx, id)` | the describe answer | IN SCOPE |
| guide | `.Guide(ctx, id, opts...)` | the guide in the client's syntax | IN SCOPE |
| explain | `.Explain(ctx, id, request)` | the explain answer | IN SCOPE if the journey declares `:explain` |
| access | `.Access(ctx, atlas.AccessQuery{})` | an iterator over access entries | IN SCOPE |
| call | `.Call(ctx, address, request, opts...)` | "The resolved result: a record, a page iterator, an operation handle, a ticket or a transfer" | IN SCOPE |
| fetch | `.Fetch(ctx, id, request, opts...)` | "an Arrow record reader" | IN SCOPE |
| operation | `.Operation(name)`, giving `Get`, `Wait`, `Cancel` | a handle | IN SCOPE (Core) |
| subscribe | `.Subscribe(ctx, id, atlas.Filter{}, opts...)` | a channel | DEFERRED |
| publish | `.Publish(ctx, id, payload)` | the event `id` | DEFERRED |
| export | `.Export(ctx, id, request, format, dir)` | files | DEFERRED |
| upload | `.Upload(ctx, transfer, source)` | the committed result | DEFERRED |

The named types:
- `atlas.Options{URL, Env, Token}` (P7:42). C:574 writes `atlas.Options{}`.
- `atlas.Request`, a map: C:539 writes `atlas.Request{"portfolios": []string{"147HYC"}, "columns": []string{"isin", "weight"}}`.
- `atlas.CatalogQuery{}`, `atlas.AccessQuery{}` and `atlas.Filter{}`. C:579 writes `atlas.Filter{"source": "index"}`, a map.
- `*atlas.Problem`: C:580 writes `var p *atlas.Problem; if errors.As(err, &p) { _ = p.Code, p.Next }`. It has every problem member as a field (P7:115).
- Deprecation goes to "the client's warning hook" (P7:97). The hook's name and type are not given: AMBIGUOUS. Reading: an option on `atlas.Options` (for example a function field). Record the choice.
- `opts...` are variadic options: the deadline, the idempotency key, the execution and accept (P7:50). Their Go names are not given: AMBIGUOUS. Reading: functional options. Record the choice.

AMBIGUOUS, `.Operation(name)` takes no `ctx` in the table (P7:52), while every other method does. Readings: (a) `Operation(name)` only builds a handle, and `Get(ctx)`, `Wait(ctx, timeout)` and `Cancel(ctx)` take the context (AG:44 requires every outbound call to take the context); (b) a typo for `.Operation(ctx, name)`. Reading (a) fits both.

AMBIGUOUS, `Fetch` returning an Arrow record reader. This fits R11 for Go, a Columnar client: Arrow is an optimisation, with the same meaning. But Go must still work against a binding that offers JSON or NDJSON only. Reading: Fetch builds Arrow batches from JSON rows using the derived Arrow schema (§9.2.7, `vectors/arrow.json`). Record it.

Levels: "Core, Streaming, Columnar (Apache Arrow for Go), Bulk, Events, Handoff" (C:568). Under RV, Handoff, Events and Bulk are DEFERRED (0.1).

The Go client's extras: the read-only adapters `auth`, `azure`, `azdo`, `github` and `astro` (C:587-593). DEFERRED. C:552 rule 3: "The adapters never import the client and never call the gateway".

Typed structs: "Typed structs MAY be generated from an application's lock (`atlas lock gen go`); they call `Call` and add only types" (C:583). DEFERRED.

Requirements:
- ATLAS-GO-001: "The Go SDK passes the conformance suite at its levels" (C:554).
- ATLAS-GO-002: "`atlas-engine` is built for every platform the Python SDK ships, by pure Go cross-compilation (no C compiler), from one Linux machine" (C:555). So nothing in the engine's import graph may need cgo. That is an inference; the requirement text says only "no C compiler".

Pass-1 pointer (`git show pass-1:sdk/go/engine/engine.go`): the package was `sdk/go/client` (not `atlas`). It had `client.Connect(ctx, client.Options{URL, Token, Lock, Mem})`, `c.Fetch(ctx, address, request, client.CallOptions{Execution})` returning a `*client.Table`, `c.Call(ctx, address, request)`, `c.WhoAmI`, `c.Catalog(ctx)`, `c.Search`, `c.Describe`, `c.LockCurrent`, `client.LoadLock` and `client.FindLock`, and `client.Name`, `client.Version`, `client.Features.Levels`. Pass 1 has no `Env`, and spells `WhoAmI` where the spec has `Whoami`. Rename it to the spec's names when it comes back.

---

## 3. The engine (`atlas-engine`)

### 3.1 What it is

- "The wheel carries `atlas-engine`, the Go SDK built as a small program (§5.4). Python starts it on first use and gives it every protocol job: sign-in, discovery, validation, fingerprints, calls, retries, resolution, handoffs, events, transfers" (C:159).
- It lives at `sdk/go/cmd/atlas-engine` (C:223).
- "The engine is also the Go reference runner, so the protocol is proven by the conformance suite" (C:230).
- It is "one per Python process" (C:168).
- C:566: "the client behind the runner protocol of §9.8.3, with Arrow results on a second pipe".
- D43 (C:275): "a separate process speaking the runner protocol with Arrow results on a second pipe".
- Review: AC:140: "The current Go engine used by the Python proof is worth retaining while measured. ... A subprocess is an implementation choice behind Python's public contract; neither moving it into the gateway nor rewriting it is a prerequisite". RV:19: "In step 4 the engine and the Python face come back from branch `pass-1` onto the new protocol rather than being rewritten."
- O61 (the engine in the cloud; C:122-133): DEFERRED (C:278 "after Phase 2").

### 3.2 The runner protocol (§9.8.3) — IN SCOPE

- The framing: "reads commands as JSON lines on standard input and writes one JSON line per result on standard output, calling only the client's public interface (9.7.1)" (P8:37).
- The environment: "The suite starts the runner with the environment variables `ATLAS_URL` (the conformance gateway), `ATLAS_HOME` (a fresh directory) and `ATLAS_CONFORMANCE=1`."

The command, verbatim (P8:42):
```json
{ "id": 7, "op": "call", "args": { "address": "conformance.rows@v1:fetch", "request": { "n": 3 }, "options": { "deadline_ms": 2000, "accept": ["application/x-ndjson"] } } }
```
The result, verbatim (P8:48):
```json
{ "id": 7, "ok": true, "value": [ { "i": 0 }, { "i": 1 }, { "i": 2 } ], "warnings": [], "calls": 1 }
```
or `{ "id": 7, "ok": false, "problem": { …the problem document… } }` (P8:51).

- The ops (P8:53): `connect`, `whoami`, `catalog`, `search`, `describe`, `access`, `call`, `fetch`, `operation.get`, `operation.wait`, `operation.cancel`, `subscribe.open`, `subscribe.next`, `subscribe.close`, `publish`, `export`, `upload`, and `version`. Q-RUN-1 adds `guide` and `explain`, which the scenarios use.
- `version`: "the runner answers its client's name, version, protocol and levels".
- Values: "Values are written as JSON; Arrow results as arrays of row records (decimals as their strings, timestamps as RFC 3339), bytes as base64."
- `options` "may carry `person: true` (a person is at the client) and `confirm: "accept"` or `"decline"`".
- Warnings: "Warnings the client emitted (deprecations, truncation) are returned in `warnings`, each `{ "code", "text" }`."
- "The runner adds no logic: it translates commands into calls on the client and results into lines."
- The engine and the suite: "(The Go SDK's runner is also the Python SDK's engine, which adds Arrow results on a second pipe and `cancel` for its own face (§5.1); the suite uses only the JSON form.)"
- The references' extra options and value forms (REF README:121; Q-RUN-4 to -7):
  - `call` options `path`, `max_memory`, `replace`, `if_match`, `fields` and `set`;
  - content returns `{"content": {"base64", "metadata"}}` in memory, or `{"manifest": {"files": [...]}}` on disk;
  - `subscribe.open` is named by its command's id, and `subscribe.next` with a timeout answers `{"none": true}`;
  - a CLI runner's failed result carries `cli_exit` (Q-RUN-2);
  - the end line's `usage` is reported as `usage` (Q-RUN-5).
- `calls` in the result line (P8:48) is not defined in the text. AMBIGUOUS. Reading: the number of HTTP calls the client made for the command. That fits R11's acceptance, "Count source executions when reading a result twice" (DR:124).

### 3.3 The engine's two additions for the face (C:234-237) — IN SCOPE

| Addition | Verbatim |
|---|---|
| Arrow results | "A command may ask for `"results": "arrow"`. A table then comes back as an answer naming a stream, and the batches follow as an Arrow IPC stream on a second pipe the face passed in at start, ended by the protocol's final zero-row batch (§9.6.5). Records, documents and problems stay JSON" |
| Cancel | "`{"op": "cancel", "id": n}` cancels the work of command `n`; its answer is the problem `deadline_exceeded` or the cancellation" |

The rules (C:200-205; the requirements at C:207-213):
1. "The face holds no protocol logic: every rule of §9 runs in the engine. The face may convert, name and present, never decide."
2. "**One engine per process, never shared across a fork.** A process starts its engine on first use; a child forked after that starts its own, and never writes to its parent's pipes. No engine outlives its Python process."
3. "Ctrl+C, a deadline or an `asyncio` cancellation cancels the engine's work for that call, at once."
4. "The engine and the face speak only through inherited pipes, never a network port."

- ATLAS-PY-004: "A process forked after its engine started gets an engine of its own on its first call, and no call hangs, in `fork`, `spawn` and `forkserver` pools, Celery's prefork pool, Airflow's executors and gunicorn with `--preload`."
- ATLAS-PY-005: "within one second".
- ATLAS-PY-007: "No engine process outlives the Python process that started it."

AMBIGUOUS: the cancel answer. C:237 says "the problem `deadline_exceeded` or the cancellation", but does not give the cancellation's form. Pass 1 answered `{"id": n, "ok": false, "cancelled": true}`. R17 adds "requested or confirmed". Readings: keep pass 1's form; or a problem document. Record it.

AMBIGUOUS: "the second pipe the face passed in at start". The fd's name is not specified. Pass 1 used the environment variable `ATLAS_ENGINE_RESULTS_FD`.

### 3.4 How Python drives it (C:180-185), and pass 1's mechanics as pointers

The spec's flow:
1. "The face starts `atlas-engine` (a few milliseconds, once per process) and sends one command: the address, the request, the options."
2. "The engine reads the token store, finds the profile, validates the request, and calls the gateway."
3. Handoff (DEFERRED).
4. "The engine writes Arrow batches to the result pipe; the face reads them with pyarrow, a batch at a time, and builds the DataFrame with Arrow-backed dtypes."

A pipeline on Astronomer: "the task's token is passed to `FIAtlas(token=…)` (or `ATLAS_TOKEN`), and the engine uses it" (C:187).

Pass 1's framing (branch `pass-1`) **differs from §9.8.3**. Reconcile it when it comes back in step 4:

| Point | §9.8.3 | Pass 1 (`sdk/go/engine/engine.go`, `clients/python/fiatlas/_engine.py`) |
|---|---|---|
| The success member | `value` | `result` (engine.go:60) |
| Where `options` sits | inside `args` (`args.options`) | at the command's top level (engine.go:41) |
| The deadline option | `deadline_ms` | `timeout`, in seconds (engine.go:53) |
| Warnings and `calls` | `warnings`, `calls` in every result | absent |
| Tables in JSON | `value` is an array of row records | `result.rows`, plus `writer`, `reader`, `via` (engine.go:291-310) |
| An Arrow table | "an answer naming a stream" | `result.stream` = the command id (engine.go:313) |
| The end of the stream | the final zero-row batch (§9.6.5) | the final batch carries the end line under the custom metadata key `atlas:end` (`_engine.py`:35) |
| Cancelled | not specified | `{"id": n, "ok": false, "cancelled": true}` (engine.go:177) |
| Extra ops | — | `lock` (engine.go:233) |
| The connect answer | not specified | `{protocol, environment, lock}` (engine.go:273) |
| The results fd | "a second pipe the face passed in at start" | env `ATLAS_ENGINE_RESULTS_FD` (main.go:22) |
| Concurrency | not specified | commands run one at a time, in order; `cancel` acts as it arrives; the scanner buffer is 64 MiB (engine.go:110-120) |
| Ctrl+C | the face cancels | the engine ignores `os.Interrupt` and the face sends `cancel` (main.go:34) |
| Orphaned | "No engine outlives its Python process" | exits when `os.Getppid()` changes, polled every 200 ms (main.go:45-51) |
| Locating the engine | the wheel carries it | an explicit path, then `ATLAS_ENGINE`, then `fiatlas/bin/atlas-engine[.exe]`, then `PATH` (`_engine.py`:40-52) |
| Grace | "within one second" | `GRACE = 1.0`; the engine is killed if a cancel does not end within it (`_engine.py`:32, :158-166) |
| Fork | the child gets its own engine | `os.register_at_fork(after_in_child=...)` drops the parent's fds without writing (`_engine.py`:235-279) |

---

## 4. The CLI (`atlas`)

- It is "`clients/cli` (Go; the binary `atlas`)" and "Built on the Go SDK (§5.4)" (C:457-458).
- Levels: "Core, Streaming, Columnar, Bulk, Events" (C:460).
- AMBIGUOUS: C:96's side-by-side table gives the CLI "Core, Streaming, Columnar, Bulk, Events, Handoff", but C:460 gives no Handoff. Handoff is DEFERRED either way.
- Released as "Signed builds published by the gateway; `atlas update` verifies and installs them" (C:461). DEFERRED.
- Open: the module is still `github.com/jhgonline/fi-app-atlas/cli`. Recommended: `github.com/jhgonline/fi-app-atlas/clients/cli` (C:140, C:504).

### 4.1 Rules

- C:444 rule 1: "No command, flag or completion is written for one capability: `atlas <verb> <id>`'s flags and `--help` are built from the capability's profile when the command runs, so a capability deployed a minute ago has them."
- C:445 rule 2: "With `--json`, or when standard output is not a terminal, output is the protocol's documents unchanged (NDJSON for streams), so an agent never parses a table."
- ATLAS-CLI-001: conformance at its levels. ATLAS-CLI-002: "A capability added to the catalogue is callable with `atlas fetch|get|run` and its flags, with no release" (C:447-448).
- AC:130: "Conveniences such as `atlas fetch`, Python attributes and generated typed methods call this same operation. JSON input remains the complete CLI interface for nested objects, arrays, nulls and future request shapes; generated flags are a convenience."
- C:482: "Each command's flags are built from the capability's request schema; enums and references complete from their lookup capabilities. Helpers such as `atlas sql` are aliases over a capability's interaction, never separate logic."

### 4.2 Every command

| Command (verbatim) | Does | Source | Status |
|---|---|---|---|
| `atlas whoami` | the `atlas.whoami` result | C:469; P7:43 | IN SCOPE |
| `atlas search <words>` | discovery | C:469; P7:45 | IN SCOPE |
| `atlas catalog [--domain d]` (C:469); `atlas catalog [--domain d] [--family f] [--effect e]` (P7:44) | discovery | | IN SCOPE (use P7's full form) |
| `atlas describe <id>` | discovery | C:469; P7:46 | IN SCOPE |
| `atlas guide <id>` | discovery | C:469; P7:47 | IN SCOPE |
| `atlas explain <id> [--param v …]` (C:470); `atlas explain <id> [--<param> v …]` (P7:48) | "the exact SQL, the columns, the estimate" | | IN SCOPE if the journey declares `:explain` |
| `atlas access [<id>]` (C:471); `atlas access [<id>] [--domain d]` (P7:49) | the access view | | IN SCOPE |
| `atlas usage` | "usage and limits" | C:471 | AMBIGUOUS: in §5.3 but not in §9.7.1's table. Reading: a built-in (C:411 lists `usage` as a built-in). Not on the journey |
| `atlas fetch <id> [--param v …] [--columns a,b] [--format table\|json\|ndjson\|csv\|arrow\|parquet] [--out file]` (C:472); `atlas fetch <id> [--<param> v …] [--format f] [--out file]` (P7:51) | data | | IN SCOPE (formats: see 4.3) |
| `atlas export <id> --format parquet --out dir/` | data as files | C:473; P7:55 | DEFERRED |
| `atlas get\|list\|create\|update\|delete <id> …` | resources | C:474; P7:50 | `list` IN SCOPE only if a journey capability is a resource; otherwise DEFERRED |
| `atlas plan <id>:<verb> …`, `atlas run <id>:<verb> …`, `atlas approve <operation>` | actions | C:475 | DEFERRED |
| `atlas subscribe <id> [--filter k=v]` (C:476); `atlas subscribe <id> [--filter key]` (P7:53) | events, NDJSON | | DEFERRED |
| `atlas publish <id> --payload <json>` | events | C:476; P7:54 | DEFERRED |
| `atlas call <address> --request <json>` | "Any interaction, by address, with the request as JSON" | C:477; P7:50 | IN SCOPE (the canonical form; AC:106-107, HB:75) |
| `atlas operation get\|watch\|cancel <name>` | operations | C:478; P7:52 | IN SCOPE (Core) |
| `atlas lock`, `atlas lock upgrade` | an application's lock | C:479; P7:144 | IN SCOPE (the lock is Core: CORE-LOCK-001) |
| `atlas lock gen go\|python\|ts` | wrappers | C:566 | DEFERRED |
| `atlas admin …` | "Administration (`Gateway.Admin`): grants, roles, limits, managed context" | C:480 | DEFERRED |
| `atlas login`, `atlas logout`, `atlas env` | "Signs in to Entra and keeps the shared token store; chooses the environment" | C:493 | IN SCOPE (hermetic: against the conformance token endpoint) |
| `atlas setup`, `atlas doctor` | set up this machine and check it | C:494 | DEFERRED |
| `atlas update`, `atlas version` | "Verifies and installs a signed build" | C:495 | `version` IN SCOPE as trivial; `update` DEFERRED |
| harness commands `new`, `resume`, `session`, `init`, `agent`, `skill`, `mcp`, `package`, `harness`, `exec`; `atlas skill install` | coding-harness sessions | C:496; C:110 | DEFERRED ("specified with the developer tools, not here", C:498) |
| `atlas sql` | an alias over `snowflake.query` | C:482; C:204 | DEFERRED |
| `invoke` | kept as an alias of the surface verbs | C:503 (open, recommended) | AMBIGUOUS: recommended, not decided |

Global: `--env`, `ATLAS_URL` and `ATLAS_ENV` "on any command" (P7:42). `--json` (C:412, C:486).

AMBIGUOUS, the flag spelling for request members:
- (a) Snake-case members: `as_of` could be `--as_of` or `--as-of`. P7:15 says "a CLI `kebab-case` subcommands", which covers subcommands, not flags.
- (b) Array members: C:43 writes `--portfolios 147HYC` and `--columns isin,weight`, which suggests comma-separated values. Repeating the flag is not ruled out.
- (c) Typed values: how a decimal, a null or a nested object is given as a flag. AC:130 makes `--request <json>` the complete interface, so (c) may be left to `--request`.

Record a rule that every capability follows identically. Rule 1 forbids per-capability flags.

AMBIGUOUS: `atlas fetch <id>` versus `atlas call <id>@v1:fetch`. A bare id at a terminal is allowed (P7:58), and C:431 writes `atlas fetch portfolio.holdings` without a version.

### 4.3 Output formats and files

- C:486: "`--json` (or a pipe) gives the documents unchanged, NDJSON for streams; a table for people on a terminal. Problems go to standard error, as JSON with `--json`, and set the exit codes of §9.7.6. Deprecations print a warning on standard error."
- `--format table|json|ndjson|csv|arrow|parquet` and `--out file` (C:472). The extras row (C:492): "Writes a result as a table, CSV, Arrow or Parquet, to standard output or a file". IN SCOPE: `table`, `json` and `ndjson`.
- AMBIGUOUS: whether `csv`, `arrow` and `parquet` output are in the first build. The journey example (C:43) uses `--format parquet --out h.parquet`. Readings: (a) build them as the CLI's extras; (b) defer them (DR:178 defers "every ... client extra implemented simultaneously"). If built, Parquet writing needs a pure-Go writer: this is a CLI, not the engine, so ATLAS-GO-002 does not bind it.
- AMBIGUOUS: what `--json` prints for a `fetch` table. Readings: "the protocol's documents unchanged (NDJSON for streams)" means the rows as NDJSON lines. What happens to the end line (usage, truncation) on stdout is not specified. Reading: the rows on stdout, and the truncation warning on stderr.
- `atlas-manifest.json` is printed with `--json` when something lands on disk (P7:179). DEFERRED with disk landing.

### 4.4 Exit codes

As in 1.6 (P7:118; `vectors/problems.json`). A CLI runner reports `cli_exit` (Q-RUN-2). The agent "reads the code, not the words" (C:433: `not_granted` with its `next`, exit 4).

AMBIGUOUS: the exit code after a truncated stream. `output_limit_exceeded` is a warning (Q-DOC-15), so the reading is exit `0` with a warning on stderr. `problems.json` lists `cli_exit: 1` for the code, but the code arrives as a warning, not a problem.

### 4.5 Sign-in, completion and locks in the CLI

- Sign-in: `atlas login` (browser with PKCE, or device code), `atlas logout` and `atlas env`. They keep the shared token store (C:493; P7:80). R26 applies (1.2).
- Completion: "enums and references complete from their lookup capabilities" (C:482); "Flags, completion from references" (C:422). The lookup is `x-atlas-reference` (Q-PROF-2 names the keyword). For the journey, a `portfolios` flag would complete from `portfolio.search@v1` or `portfolio.list@v1`.
  - AMBIGUOUS: which shell completion mechanism, and whether completion calls the gateway (it must, to stay generic). Rule 1 forbids per-capability completion code.
  - Completion must also respect visibility: only what the caller can use (C:469).
- Locks: `atlas lock` writes `atlas.lock`. `atlas lock upgrade` "moves entries forward after checking resolution" (P7:144). The format is LEFT TO OWNERS (RV:99).

---

## 5. The Python SDK (`fiatlas`)

- It lives in `clients/python`: package `fi-lib-atlas`, import `fiatlas` (C:158, C:222).
- "One wheel per platform, with no Python dependencies; `pyarrow`, `pandas` and `polars` are optional extras" (C:161).
- Released "to the FIQ feed: one wheel per platform (`py3-none-<platform>`, every Python 3)", the engine built "for Linux (x86 and ARM), macOS (Intel and Apple silicon) and Windows". The optional dependencies are `[arrow]`, `[pandas]`, `[polars]` and `[airflow]` (C:226).
- ATLAS-PY-006: "The wheel installs and passes the vectors on Python 3.11 to 3.14 on every platform it is built for, with no Go and no compiler present."
- Levels: "the engine's. Core, Streaming, Columnar, Bulk, Events, **Handoff**; Broker deferred (O54)" (C:160). Narrowed by 0.1.

### 5.1 The §9.7.1 names (P7:42-56, the Python column)

| Operation | Python | Status |
|---|---|---|
| connect | `FIAtlas(url=None, env=None, token=None)` | IN SCOPE |
| whoami | `.whoami()` | IN SCOPE |
| catalog | `.catalog(domain=None, family=None, effect=None)` | IN SCOPE |
| search | `.search(words)` | IN SCOPE |
| describe | `.describe(id)` | IN SCOPE |
| guide | `.guide(id, max_tokens=None)` | IN SCOPE |
| explain | `.explain(id, request)`; and `ref.explain()` on a reference | IN SCOPE (see the R11 note below) |
| access | `.access(id=None, domain=None)` | IN SCOPE |
| call | `.call(address, request, *, deadline=None, idempotency_key=None, execution=None, accept=None)` | IN SCOPE |
| fetch | `.fetch(id, request)`, which gives a reference with `.read_as(kind)` and `.via`; and `atlas.<domain>.<name>(**request)` | IN SCOPE |
| operation | `.operation(name)`, giving `.get()`, `.wait(timeout)`, `.cancel()` | IN SCOPE (Core) |
| subscribe | `async for e in .subscribe(id, filter=None, durable=None)` | DEFERRED |
| publish | `.publish(id, payload)` | DEFERRED |
| export | `.export(id, request, format, path)` | DEFERRED |
| upload | `.upload(transfer, source)` | DEFERRED |

- The attribute form: "`atlas.portfolio.holdings(...)` is `fetch` (or the family's one read interaction) of the id it spells, with `_` for `-`; it adds nothing else" (P7:58).
- "`atlas.<domain>.<name>` attributes come from the working view; a lock makes them typed (`.pyi` stubs that `atlas lock gen python` writes)" (C:255). The stubs are DEFERRED.
- For `portfolio.holdings-dates@v1`, the attribute is `atlas.portfolio.holdings_dates(...)`. This is the step that proves acceptance point 4 (RV:49): it must work with no release of `fiatlas`, because the attributes come from the working view at run time.
- `ref.via` is `"gateway"` or `"handoff"` (C:249).

**OVERRIDDEN BY REVIEW (R11)**: DR:120: "Python's lazy-looking reference and Node's eager-looking fetch also leave the execution point unclear." AC:132: "A lazy request builder may exist as a convenience, but must be named and documented separately from execution." HB:102: "Exact result-handle methods for the generic call should be settled consistently across clients before examples are published as runnable instructions."

AMBIGUOUS, when `fiatlas` executes. C:246-248 builds `ref = atlas.portfolio.holdings(...)`, calls `ref.explain()`, then `df = ref.read_as(pd.DataFrame)`. That is a lazy builder: `explain` before execution. Readings:
- (a) `.fetch(...)` and the attribute form execute at once (as `call` does) and return a result handle. `read_as` reads the retained result, and reading it twice never re-executes. `ref.explain()` is removed, or becomes a separately named lazy builder (AC:132). `explain` stays `atlas.explain(id, request)`.
- (b) Keep the lazy reference, but name it as a builder and document that `read_as` executes, once.

R11's acceptance counts source executions on a double read, so either way a second `read_as` must not query again. It re-reads the retained batches, or raises "cannot be replayed". Decide one rule for CLI, Python and Node together (HB:102).

### 5.2 The problem classes per code

- P7:113: "An exception `AtlasProblem` (subclass per `code` where the client defines one), with attributes for each member".
- The spec names no subclasses: AMBIGUOUS. Reading: one subclass per registry code (the 24 codes in 1.6). Each carries every member as an attribute: `type`, `title`, `status`, `detail`, `instance`, `code`, `retryable`, `retry_after`, `target`, `next`, `details`, `request_id`, `estimate`. An unknown code is a bare `AtlasProblem`, handled by `status` and `retryable` (CORE-PROB-002). The subclass naming rule (for example `NotGranted` from `not_granted`) is not specified. Record it.
- Pass 1 used `AtlasError(Exception)` with `code`, `status`, `retryable`, `detail`, and `EngineError(RuntimeError)` for an engine failure (`clients/python/fiatlas/errors.py`). Rename to `AtlasProblem`, and carry every member.
- AMBIGUOUS: the engine's own failures (it cannot start, or it dies) have no problem code in the spec. Pass 1 raised `EngineError`, outside `AtlasProblem`. Keep that reading, and record it.

### 5.3 `read_as` (C:259-261) — IN SCOPE

- "`read_as(kind)` turns the Arrow batches into: a `pyarrow.Table` or a batch iterator; a `pandas.DataFrame`, with Arrow-backed dtypes so decimals and nullable integers stay exact; a `polars.DataFrame`, with no copy; a DuckDB relation; a `list` of records, or a `dict` of columns."
- "Memory stays bounded unless the caller asks for the whole result at once."
- ATLAS-PY-002: "`read_as` returns every column with the type its profile declares (no decimal becomes a float)."
- AC:134: exact decimals must stay exact. With no pyarrow, `list` records must carry `Decimal`, never `float`.
- AMBIGUOUS: `read_as(list)` or `read_as(dict)` without `pyarrow` installed. C:161 makes pyarrow optional, and R11 makes Core JSON. Reading: the face asks the engine for JSON rows (no `"results": "arrow"`) and converts decimal strings to `decimal.Decimal` and timestamps to `datetime`. The conversion rule per logical type is not specified. Record it.
- The DuckDB relation: `read_as` needs `duckdb` in the Python process. C:226's optional dependency list has no `[duckdb]`: AMBIGUOUS whether there is an extra for it.
- Pass 1's `read_as` accepted `pa.Table`/`"arrow"`, `pd.DataFrame`/`"pandas"` (`types_mapper=pd.ArrowDtype`) and `list`/`"records"` (`client.py`:33-46). There was no polars, DuckDB or dict.

### 5.4 Async — operations

- The only async form in the spec is `async for e in .subscribe(...)` (DEFERRED). "`async`" is listed among the face's jobs (C:159), and "an `asyncio` cancellation" must cancel within 1 s (C:202; ATLAS-PY-005).
- AMBIGUOUS: async variants of `call`, `fetch` and `read_as` are not named. Readings: (a) none in the first build; (b) an `AsyncFIAtlas`, or `await`-able methods.
- Operations: `.operation(name)` gives `.get()`, `.wait(timeout)` and `.cancel()` (P7:52). `wait`'s timeout unit is not specified: AMBIGUOUS. Reading: seconds, as a Python float.

### 5.5 The "core extras" and `fiatlas.extras`

- The optional dependencies (pip extras): `[arrow]`, `[pandas]`, `[polars]` and `[airflow]` (C:226). The base wheel has "no Python dependencies" (C:161). IN SCOPE: the base wheel plus `[arrow]` and `[pandas]` for the journey.
- `fiatlas.extras` (C:265-271), all DEFERRED:

| Extra | Does | Needs | Review |
|---|---|---|---|
| `extras.write_parquet(ref, path)`, `extras.write_csv(ref, path)` | "Writes a result to a file as its batches arrive, with the profile's types" | A file system | DEFERRED |
| `extras.cache` | "Keeps results ... keyed by address, request and fingerprint" | A file system for the disk cache | OVERRIDDEN BY REVIEW (R18); DEFERRED |
| `extras.airflow` | "An Atlas hook and operator for DAGs: the task's workload token, deadlines from the task's timeout, results to XCom or files" | Airflow (`[airflow]`) | DEFERRED; O47 "the hook and operator only" |

- The rules: "Extras live under `fiatlas.extras` and never call the gateway except through `fiatlas`'s ordinary calls" (C:205).
- §9.7.12's five rules (P7:156-162) bind every extra. An extra starts "from what the engine landed ... never from its own call to a source" (C:120).
- Other Python rules: rule 5, "Free-form SQL (`atlas.sql`) is the capability `snowflake.query`, behind its named grant" (C:204), DEFERRED. ATLAS-PY-003: "A pipeline task calls Atlas with its workload's token and never with a person's" (C:209). With AC:48, a workload "does not retain the interactive user's token".

---

## 6. The Node SDK (`@atlas/sdk`)

The review puts it IN SCOPE: RV:12, "CLI, Python and Node from the first release, one request record in all three" (ATLAS-CORE §6, §12.4; R11). AC:140: "The TypeScript implementation must pass the same semantic fixtures." DR:182: "The next proof should complete the CLI and Node path for the same business journey". DR:24: "Independent Go and TypeScript conformance ... Keep independent expected results and cross-client comparisons."

What the spec says (C:287-400):
- It lives in `clients/node`, package `@atlas/sdk`. "its protocol is `@atlas/protocol`, types generated from §9.3's schemas" (C:296, C:352-353).
- It calls the gateway over HTTPS "from a Node process (Nautilus Desktop) or a page (Nautilus Cloud)". The gateway allows the Nautilus applications' origins (C:354).
- Levels: "Core, Streaming, Columnar (Apache Arrow for JavaScript), Bulk, Events (in a Node process: a page polls); Handoff later" (C:355). For the first build: Core and Streaming IN SCOPE, Columnar as an optimisation, the rest DEFERRED.
- Released to the internal npm registry (C:356). The rule numbers are C:338-343:
  - Rule 1: "The SDK never holds a Snowflake or vendor credential."
  - ATLAS-NODE-001: conformance at its levels.
  - ATLAS-NODE-002: panels with extras degrade. DEFERRED.

The names (P7:42-56, the TypeScript column):

| Operation | TypeScript | Status |
|---|---|---|
| connect | `atlas.connect({ url?, env?, token? })` (`import { atlas } from '@atlas/sdk'`) | IN SCOPE |
| whoami | `.whoami()` | IN SCOPE |
| catalog | `.catalog({ domain?, family?, effect? })` | IN SCOPE |
| search | `.search(words)` | IN SCOPE |
| describe | `.describe(id)` | IN SCOPE |
| guide | `.guide(id, { maxTokens? })` | IN SCOPE |
| explain | `.explain(id, request)` | IN SCOPE if declared |
| access | `.access({ id?, domain? })` | IN SCOPE |
| call | `.call(address, request, { deadline?, idempotencyKey?, execution?, accept? })` | IN SCOPE |
| fetch | `.fetch(id, request, options?)`, giving "an Arrow Table or rows" | IN SCOPE |
| operation | `.operation(name)`, giving `get()`, `wait(timeout)`, `cancel()` | IN SCOPE (Core) |
| subscribe | `for await (const e of .subscribe(id, { filter?, durable? }))` | DEFERRED |
| publish | `.publish(id, payload)` | DEFERRED |
| export | `.export(id, request, { format, path })` | DEFERRED |
| upload | `.upload(transfer, source)` | DEFERRED |

- Examples: `const a = await atlas.connect();` and `await a.fetch('portfolio.holdings@v1', { portfolios: ['147HYC'], columns: ['isin', 'weight'] })` (C:51, C:362-363). AC:121-127 uses `client.call('portfolio.holdings@v1:fetch', {...})`.
- Note that the option is camelCase (`idempotencyKey`), but request members keep their own spelling (`as_of`, HB:96). Request members are never renamed to camelCase. That is AP-CLIENT-003.
- Errors: "A thrown `AtlasProblem extends Error`, with each member as a property" (P7:114).
- Deprecation: `process.emitWarning` (P7:97).
- Exact values: "a TypeScript exact representation" for decimals (AC:134). The representation is not named: AMBIGUOUS. Readings: a decimal string, or a decimal library type. Large integers (`int64`) need `bigint` or a string. Never `number` for a decimal.
- Credentials: OVERRIDDEN BY REVIEW (R11, R26). A page uses "its host application's supported identity flow" (AC:50), and "A browser client needs neither a token file nor local disk output" (DR:124). A Node process may use the shared store "where their runtime permits" (AC:50). For the first build, the reading is a Node process: read the shared store (with R26's binding), or `token` given to `connect`. Whether Node reads `ATLAS_TOKEN` is not stated in §9.7.3, which says "A workload ... sets `ATLAS_TOKEN`" for any client.
- Types: "Types are generated from an application's lock at its build; they call `call` and add only types" (C:368). DEFERRED.
- Extras: `@atlas/sdk/extras` gives `extras.files`, `extras.git` and `extras.github`, in a Node process only (C:372-385). DEFERRED.
- Open: "Part of the SDK is generated from `gateway/openapi/ui.json`; the profile-driven core replaces it" (C:394). Nautilus Desktop embedding is to be confirmed (C:395).
- The runner: a Node runner beside the client, speaking §9.8.3 (P8:148), so that the scenarios drive Node too. That is AP-CLIENT-001 (P7:195).

AMBIGUOUS, the Node SDK's own protocol implementation. `@atlas/protocol` is a second, independent TypeScript implementation of §9 (C:95, C:353). The review values that independence (DR:24). C:550's "never a second implementation" binds only the Go programs. So Node must implement the canonical request (RFC 8785 for parity checks), schema validation, fingerprints, header Structured Fields, the NDJSON/Arrow readers and the narrowed resolution, all passing the same vectors. Pass 1 had no Node client.

---

## 7. Credentials, token storage and environment rules (summary)

| Rule | Source | Status |
|---|---|---|
| Gateway order: connect arg, `ATLAS_URL`, `ATLAS_ENV`, `config.toml` `env`, Production | P7:62-68 | IN SCOPE |
| `ATLAS_HOME` or `~/.atlas`; `config.toml` `[environments.<name>] url`; built-ins `dv`, `ac`, `pd` | P7:70 | IN SCOPE |
| `<ATLAS_HOME>/state/token-<env>.json`, `<env>` = the bootstrap's `environment` | P7:76; Q-CLI-1 | IN SCOPE, to be extended by R26 |
| Mode `0600` file, `0700` directory; refuse wider | P7:77; CORE-SIGNIN-005 | IN SCOPE |
| Atomic write: a temporary file in the same directory, then rename | P7:78 | IN SCOPE |
| Refresh within 60 s of `expires_at`, at `sign_in.token_endpoint` (`grant_type=refresh_token`) | P7:79; CORE-SIGNIN-002 | IN SCOPE |
| 401 `error="invalid_token"`: refresh and repeat once | CORE-SIGNIN-003 | IN SCOPE |
| A claims challenge without a person: `unauthenticated` + `details[{challenge}]`, no refresh | CORE-SIGNIN-004 | IN SCOPE |
| No usable token: a person signs in (PKCE or device code); without a person, `unauthenticated` | P7:80 | IN SCOPE (hermetic) |
| A workload passes `token` to `connect` or sets `ATLAS_TOKEN`; it never writes the store | P7:81; C:187 | IN SCOPE |
| Bind each credential to the trusted gateway, authority, audience, tenant/account and scopes | R26 (RV:84; AC:52) | IN SCOPE, OVERRIDDEN BY REVIEW |
| Serialise refresh | R26 | IN SCOPE, OVERRIDDEN BY REVIEW |
| An unauthenticated bootstrap cannot redirect a credential | R26 | IN SCOPE, OVERRIDDEN BY REVIEW |
| Credentials from each runtime's own provider; a browser needs no token file | R11 (RV:76; DR:122-124) | OVERRIDDEN BY REVIEW |
| A workload never inherits the person's grants or token | AC:48; HB:31; ATLAS-PY-003 | IN SCOPE |
| One default org/tenant + environment; explicit switching; caches isolated by effective context; `whoami` returns the effective context | HB:33; AC:50 | IN SCOPE as far as decided; the switching contract is LEFT TO OWNERS (RV:98) |
| An agent and a person are told apart by trusted authentication context, not an `is_human` parameter | AC:46 | Gateway's; the client sends no such member |
| No token, key, body or query string in any log, span, metric or audit record | AG:47 | IN SCOPE (clients too) |
| An extra never sends the caller's own credentials (`gh`, `az`, git) to the gateway | P7:160; C:118 | DEFERRED with extras |
| Settings read once, in a composition root (`cmd/`), a `config`/`settings` package or `settings.go` | AG:42 (`atlasvet` nogetenv) | IN SCOPE: `ATLAS_URL`, `ATLAS_ENV`, `ATLAS_HOME`, `ATLAS_TOKEN` are read there |

---

## 8. The parity rules: what must be identical across clients

### 8.1 The request record (AP-CLIENT-003) — IN SCOPE

- AP-CLIENT-003 (P7:199): "The same request record, built from the same inputs, is byte-for-byte the same JSON from every client (canonicalised by RFC 8785): no client moves a request member into its options."
- CORE-CALL-005: the input `{"text": "naïve é", "count": 2, "ratio": 0.5, "when": "2026-10-31", "kind": "loud", "tags": ["b", "a"]}` must arrive with the RFC 8785 form, verbatim: `{"count":2,"kind":"loud","ratio":0.5,"tags":["b","a"],"text":"naïve é","when":"2026-10-31"}`.
  - Members are sorted.
  - Array order is kept (`["b","a"]`).
  - Non-ASCII is unescaped UTF-8.
  - `0.5` is an ECMAScript number.
  - The client adds no defaults. CORE-LOCK-001's echo shows that the defaults (`count` 1, `kind` `plain`, `ratio`/`when` null, `tags` []) come back from the gateway, not from the client's request.
- P7:38's one-shape rule, and HB:63-69's journey request:
  ```json
  { "portfolios": ["147HYC"], "as_of": "2026-10-28", "columns": ["portfolio", "isin", "as_of", "weight"] }
  ```
  This request is the same member for member in the CLI (`--request '{...}'`, HB:75), Python (HB:82-87) and TypeScript (HB:94-99).
- HB:59: "Language-specific convenience methods can sit above it, but must not introduce different business parameters or hidden execution behavior."
- AMBIGUOUS: the gateway's side ("Persist both the submitted request identity and its effective defaults at admission", AC:200) belongs to the gateway digest. The client's obligation is only to send the record unchanged.

### 8.2 Acceptance point 2 (RV:47)

"The same person discovers and calls the same capability from the CLI, Python and Node with identical request records."

What it takes:
- The same `whoami`, `search` and `guide`/`describe` path in each (HB:51-55).
- The same address.
- RFC 8785-identical bodies. The suite's `body` assertion is `jcs` (REF README:157).
- The same sign-in for the person (HB:31). Under R26 / R11, one identity, through each runtime's provider.

### 8.3 Acceptance point 5 (RV:50)

"Invalid requests, permission changes, exact decimals, incomplete streams, idempotent retries and source failures behave the same in every client."

| Behaviour | What "the same" is pinned by |
|---|---|
| Invalid requests | Client-side validation before sending: `invalid_request`, with `target` the first failing pointer (Q-VAL-1), and nothing sent (CORE-CALL-002). An unknown member is refused, never dropped (R04) |
| Permission changes | No cached access authorises a call (R01, DR:38). The problem `not_granted`/`denied` (exit 4) carries every member. AC:30: "Execution always rechecks the relevant facts" |
| Exact decimals | No float, ever (AC:134; ATLAS-PY-002). JSON decimal strings; Arrow `Decimal128` (COL-001 "decimals exact"); Python `Decimal`; TypeScript an exact form |
| Incomplete streams | A missing end line or final batch is `upstream_failed` (STREAM-002, COL-002). An end-line problem is raised after the rows (STREAM-003). Truncation is a warning `output_limit_exceeded` with the rows (STREAM-004). "EOF alone is not success" (AC:206) |
| Idempotent retries | §9.7.7's exact policy: at most 3, `retry_after` or `min(30, 0.5 × 2^attempt)` + ≤25% jitter, never past the deadline, the same `Idempotency-Key` (CORE-CALL-004, CORE-RETRY-001) |
| Source failures | `upstream_failed` (502, retryable) and `unavailable` (503, retryable, `retry_after`) are retried. Others are raised with every member. The CLI exit codes are as in 1.6 |

### 8.4 The other parity statements

- AC:136: "For each result shape, parity covers the logical value, schema, warnings, request identity, data provenance and completion status. It does not require every runtime to use the same in-memory container or to write files at the same location." This is OVERRIDDEN BY REVIEW (R11) against AP-CLIENT-005's same container and same disk place (P7:198).
- DR:124, R11 acceptance: "Compare identity, canonical requests, values, errors, deadlines, completeness and usage across CLI/Python/Node. Count source executions when reading a result twice."
- AP-CLIENT-001 (P7:195): "Two clients at the same levels, given the same gateway, inputs and conformance scenario, make the same calls and return the same results and problems."
- §9.7.1's naming rule (P7:15): "Python `snake_case`, TypeScript `camelCase`, Go exported `PascalCase`, a CLI `kebab-case` subcommands; the semantics are identical."
- P7:36: "A client MUST NOT offer an operation written for one capability, except as a generated wrapper (9.7.11)." P7:187 rule 2: "A client has no behaviour for any one capability beyond the built-ins."
- R22 (RV:80): "Answers, scope, recovery and cost are tested; the built artifact replays deterministically." DR:149 and AC:243: do not require an exploratory agent to repeat an identical tool sequence.
- Acceptance point 4 (RV:49): "A capability added after the clients were built works without releasing any of them." That is `portfolio.holdings-dates@v1` through `atlas call`/`atlas fetch`, `FIAtlas().call`, the attribute `atlas.portfolio.holdings_dates`, and Node's `call`/`fetch` (AP-CLIENT-004, ATLAS-CLIENTS-002, ATLAS-CLI-002).
- Acceptance point 3 (RV:48), for clients: a narrower result or "a clear refusal". The client shows it the same way in all three.
