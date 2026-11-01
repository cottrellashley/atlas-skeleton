# Digest: cloud-gateway-patterns-go at 44ba81a, for the Atlas pass-2 first build

Reader: patterns. Written 2026-10-31.

## 0. Sources, method, legend

| What | Where |
|---|---|
| Library clone | `/private/tmp/claude-501/-Users-ashleycottrell/45a5c432-d2c8-4875-a3e2-9d5fbe25f062/scratchpad/cgp/repo`, HEAD `44ba81a` "Add the architectural principles layer, kernel packages and control-plane reference". Module `github.com/cottrellashley/cloud-gateway-patterns-go`, `go 1.25` (go.mod L1-3). Zero third-party deps (README L45) |
| Pinned commit in Atlas | `44ba81aa92ee901c131c0b80dcadafa25de440a2` (2026-10-30), sdk/go/kit/PROVENANCE.md L10; INTEGRATIONS.md L9 |
| Governing | atlas-skeleton AGENTS.md; docs/pass-2/REVIEW.md; review/INTEGRATIONS.md "Cloud patterns: reuse and adapt" (L37-54); spec §6.7 (06-7-implementation-patterns.md, read in full because it is the spec's own pattern-to-kit mapping); §9.3.3 problem registry (09-3-documents.md L76-104); §9.6.3 headers (09-6-http-binding.md L24-50); references schemas operation/page/problem |
| Verification I ran | `go test -race -count=1 -cover ./...` in the clone: every package `ok`, race-clean (coverage per package in section 1). `go vet ./...`: clean. Atlas's own `staticcheck` (`atlas-skeleton/.bin/staticcheck`): 3 findings (section 3, P-SC). A separate repro module `/private/tmp/claude-501/-Users-ashleycottrell/45a5c432-d2c8-4875-a3e2-9d5fbe25f062/scratchpad/build/repro/repro_test.go` (replace-directive to the clone; nothing in the clone or repo modified): 21 tests, each asserting a defective behaviour, all PASS under `-race`, i.e. all 21 defects reproduce |

Legend:
- **IN SCOPE** = the first build (the journey: `portfolio.holdings@v1`, `portfolio.list@v1`, `portfolio.search@v1`, `data.status@v1`, then `portfolio.holdings-dates@v1`; REVIEW.md L37-42) must implement it. **DEFERRED** = not in the first build.
- **OVERRIDDEN BY REVIEW (Rnn)** = the review / REVIEW.md changes the spec or the library's behaviour.
- **AMBIGUOUS** = the sources allow more than one reading; each reading listed.
- **CONFIRMED (Dn)** = reproduced by test Dn in the repro module. **BY READING** = from the code, not executed.
- Line numbers are the clone's files unless another path is given.

### The six places the library loses to the spec (atlas-skeleton AGENTS.md L9-15)

| # | Rule (verbatim) | Library behaviour it overrides |
|---|---|---|
| 1 | "every interaction is a POST to its binding" | `lro` GET/POST `/v1/operations/{id}[/cancel]` (lro.go L302-356); `idempotency` keyed on method (L121-126); `shedding.ClassifyByMethod` read/write split (shedding.go L104-111); kernel PUT/GET/DELETE (cmd/kernel L261-339). §9.6.2: "`POST` for every interaction ... `GET` only for the bootstrap and for polling an operation" (09-6 L20) |
| 2 | "desired fields sit at the top of a record, not under `spec`" | `resource.Object{APIVersion, Kind, Metadata, Spec, Status}` (resource.go L55-61) |
| 3 | "states are lowercase, with `UNKNOWN`" | `lro.Status` `Accepted/Running/Succeeded/Failed/Canceled` (lro.go L32-38); `resource.ConditionStatus` `True/False/Unknown` (resource.go L105-109) |
| 4 | "modules register in one list, never in `init`" | `registry.Default` + `Register` (registry.go L127-157); `init()` in cmd/kernel L130-133; principle 04 rule 2 "registered at init" (04-extension-points L54) and its anti-pattern L117 |
| 5 | "authentication, limits and idempotency are pipeline steps, not middleware" | `auth.Middleware`, `ratelimit.Middleware`, `idempotency.Middleware` in cmd/gateway chain L136-139 |
| 6 | "the codes are `idempotency_conflict` 409 and `precondition_failed` 412" | `IdempotencyKeyReused` 422 (idempotency.go L162-163); `PreconditionFailed` literal (resource.go L209) |

## 1. Per pattern

Common to every package: errors are written with the sibling `apierror` (Azure-style `{"error":{code,message,target,retryable,retryAfter,hop,details,innererror}}`, `X-Error-Code` header, CamelCase codes; apierror.go L23-40, L155-171). Atlas replaces it with `kit/problem` (RFC 9457, `Atlas-Error-Code`, snake_case codes of §9.3.3; PROVENANCE.md L27; atlas-skeleton sdk/go/kit/problem/problem.go L44). No package injects a clock through an exported option except where noted; most hold an unexported `now func() time.Time` that only in-package tests can set.

### 1.0 apierror (dependency of most packages; DEFERRED as such: replaced by kit/problem)

- Reserved codes (apierror.go L23-40): `MalformedRequest`, `InvalidInput`, `Unauthenticated`, `PolicyDenied`, `NotFound`, `Conflict`, `IdempotencyKeyReused`, `RateLimited`, `Overloaded`, `PayloadTooLarge`, `DeadlineExceeded`, `Canceled`, `UpstreamUnavailable`, `UpstreamError`, `ShuttingDown`, `InternalError`. `StatusClientClosedRequest = 499` (L46).
- API: `type Error struct{Status int "-"; Code; Message; Target; Retryable bool; RetryAfter int; Hop; Details []Detail; Inner *Inner; cause error}` (L64-76); `New(status int, code, message string) *Error` (L90); `Wrap(cause error, status int, code, message string) *Error` (L95); `(*Error).WithDetail(code, target, message string) *Error` (L102); `(*Error).WithRetryAfter(seconds int) *Error` (L108); `From(err error) *Error` (L125: DeadlineExceeded→504, Canceled→499, MaxBytesError→413, net timeout→504, else 500 generic); `Write(w http.ResponseWriter, err error)` (L155); `HTTPStatusForRPCCode(code string) int` (L176). Default retryable for 429/502/503/504 (L114-120).
- Tests (apierror_test.go): From mapping table; internal cause not leaked; Write headers and envelope; two RPC codes. Coverage 66.1%.

### 1.1 idempotency — IN SCOPE (as kit/idem + pipeline step 4)

- Purpose: Idempotency-Key replay / reuse / in-progress for unsafe methods (idempotency.go L1-15; doc 04-identity/idempotency.md).
- API (idempotency.go):
  - `type Record struct{Fingerprint string; InProgress bool; Status int; Header http.Header; Body []byte; ExpiresAt time.Time}` (L33-40)
  - `type Store interface{ Begin(ctx, key string, rec Record) (existing Record, created bool, err error); Complete(ctx, key string, rec Record) error; Delete(ctx, key string) error }` (L44-50)
  - `type MemoryStore`; `NewMemoryStore() *MemoryStore` (L53-84)
  - `type Options struct{Store Store; Header string; Retention time.Duration; MaxResponseBytes int64; MaxBodyBytes int64; Required bool; Scope func(*http.Request) string}` (L87-102)
  - `Middleware(o Options) func(http.Handler) http.Handler` (L106)
  - `var ErrInProgress` (L253, unused)
- Defaults: Header `Idempotency-Key`, Retention 24h, MaxResponseBytes 1 MiB (`1 << 20`), MaxBodyBytes 4 MiB (`4 << 20`) (L107-118). Key length > 255 → 400 `InvalidInput` (L136-139); no character check.
- Semantics table (doc idempotency.md L11-21; code L120-194): methods POST/PATCH/DELETE only (L121-126); missing key: pass, or 400 `InvalidInput` detail `MissingHeader` if Required (L128-134); scoped key = `Scope(r) + ":" + key` (L140-142); body read up to MaxBodyBytes+1, over → 413 (L143-151); fingerprint `sha256(method \0 path \0 body)` hex (L198-206); store error → 503 `Overloaded` Retry-After 1 (L156-159); fingerprint differs → 422 `IdempotencyKeyReused` (L161-164); in progress → 409 `Conflict` Retry-After 1 (L166-169); replay → stored headers + `Idempotent-Replayed: true` + stored status/body (L171-177); handler 5xx or capture overflow → `Store.Delete` (L185-188); else `Complete` with `context.WithoutCancel` (L190-193). Replayed header list: `Content-Type`, `Location`, `Operation-Location`, `ETag`, `X-Error-Code` (L240).
- Concurrency: MemoryStore one mutex (L54-84); middleware stateless; captureWriter not goroutine-safe (single request).
- Persistence: Store port; doc says Begin must be atomic across replicas (Redis `SET NX EX`, Cosmos `CreateItem` 409→read, Postgres `ON CONFLICT DO NOTHING RETURNING`; idempotency.md L35); TTL 24h. MemoryStore never sweeps (unbounded map) and its clock is unexported.
- Tests (idempotency_test.go): replay with headers/body and single execution; 422 on body change; per-user scope independence; 409 while in progress (busy-wait spin L58-59); 5xx not memoised; GET bypass; Required → 400. Not tested: overflow, store errors, retention expiry, panic, query strings. Coverage 84.7%.

### 1.2 lro — IN SCOPE only if the journey has an operation delivery (AMBIGUOUS A11); kit/ops

- Purpose: 202 + Operation-Location, status monitor, cooperative cancel, idempotent start, retention (lro.go L1-13; doc 06-data-and-async/long-running-operations.md).
- API (lro.go):
  - `type Status string`; `Accepted, Running, Succeeded, Failed, Canceled` (L30-38); `(Status).Terminal() bool` (L41)
  - `type Operation struct{ID "id"; Kind "kind"; Status "status"; CreatedAt "createdAt"; UpdatedAt "updatedAt"; EndedAt *time.Time "endedAt,omitempty"; PercentComplete float64 "percentComplete,omitempty"; Message "message,omitempty"; Result json.RawMessage "result,omitempty"; Error *apierror.Error "error,omitempty"; PollIntervalMs int "pollIntervalMs"; Version int64 "-"}` (L44-57)
  - `type Store interface{ Create(ctx, op *Operation) error; Get(ctx, id string) (*Operation, error); Update(ctx, id string, fn func(op *Operation) error) (*Operation, error); Sweep(ctx, cutoff time.Time) (int, error) }` (L61-67); `ErrNotFound` (L70); `MemoryStore`, `NewMemoryStore()` (L73-132)
  - `type Progress func(percent float64, message string)` (L135); `type Worker func(ctx context.Context, progress Progress) (json.RawMessage, error)` (L140)
  - `type Manager struct{Store Store; Retention, PollInterval time.Duration; NewID func() string; Deadline time.Duration; ...}` (L143-155); `NewManager(store Store) *Manager` (L158: Retention 24h, PollInterval 1s, ID `"op_" + base36(UnixNano) + base36(seq)`)
  - `(*Manager).Start(ctx, kind, idempotencyKey string, w Worker) (*Operation, bool, error)` (L173); `Cancel(ctx, id string) (*Operation, error)` (L250); `Wait()` (L261); `SweepLoop(ctx, every time.Duration)` (L264)
  - `WriteAccepted(w, op *Operation, operationsPath string, replayed bool)` (L287: 202, `Operation-Location: <path>/<id>`, `Retry-After: max(1, PollIntervalMs/1000)`, `Operation-Id`, optional `Idempotent-Replayed: true`, body `{"operation":...}`)
  - `GetHandler(store Store) http.Handler` (L303: weak ETag `W/"<id>:<version>"`, `Cache-Control: no-cache`, Retry-After while non-terminal, 304 on exact If-None-Match, 404 `NotFound`)
  - `CancelHandler(m *Manager) http.Handler` (L335: 200 if terminal, 202 otherwise)
- Concurrency: one goroutine per operation (L201-202, bare `go`); `Manager.mu` guards `cancels` and `keys` maps and is held across `Store.Create` (L174-199); worker ctx = `context.WithCancelCause(context.WithoutCancel(ctx))` (L192); cancel cause `errCanceledByClient` (L166, L255); panic recovered into Failed (L223-230).
- Persistence: Store port (doc: Cosmos id as partition key + TTL, or Postgres; CAS on Version, L59-60). The key→operation map and the cancel functions are process memory only (L152-153): no restart safety (INTEGRATIONS L45 says exactly this). Sweep deletes only terminal records (L126), so a crash leaves Accepted/Running records forever.
- Tests (lro_test.go): success + same key returns same op without running; cooperative cancel → Canceled with code `Canceled`; error → Failed `InternalError` with message hidden; panic → Failed; HTTP surface 202 headers, GET 200 + Retry-After + ETag, 304, terminal GET without Retry-After, cancel of terminal = 200, 404; Sweep. All use real time polling (`time.Sleep(5ms)` up to 2 s, L13-28). Not tested: Deadline, key after sweep, store errors, progress. Coverage 84.2%.

### 1.3 ratelimit — IN SCOPE (kit/limit arithmetic; pre-auth edge limit; step 5 through adapter)

- Purpose: token bucket (x/time/rate semantics), keyed TTL map (Traefik ttlmap), fixed window on a Counter (Envoy ratelimit), IETF headers (bucket.go L1-18).
- API:
  - bucket.go: `type Bucket`; `NewBucket(rate float64, burst int) *Bucket` (L41, starts full); `(*Bucket).Reserve(now time.Time, n int, maxWait time.Duration) (delay time.Duration, ok bool)` (L64); `Allow(now) bool` (L84); `Tokens(now) float64` (L90); `type Decision struct{Allowed bool; Limit, Remaining int; Window, ResetAfter, RetryAfter time.Duration}` (L97-104); `type Limiter interface{ Allow(ctx, key string) (Decision, error) }` (L107-109); `type Keyed`; `NewKeyed(rate float64, burst, capacity int) *Keyed` (L135: ttl = burst/rate seconds + 1 s); `(*Keyed).Len() int` (L160); `(*Keyed).Allow(ctx, key) (Decision, error)` (L167: RetryAfter = ceil((1 - tokens)/rate))
  - window.go: `type Counter interface{ Increment(ctx, key string, n int64, ttl time.Duration) (int64, error) }` (L13-15); `MemoryCounter`, `NewMemoryCounter()` (L18-54; sweeps expired keys only when > 10000 keys, L46-52); `type FixedWindow struct{Counter Counter; Limit int64; Unit time.Duration; Policy string; now}` (L59-65); `(*FixedWindow).Allow(ctx, key)` (L68: key `key + ":" + windowStart.Unix()`, TTL Unit + 1 s)
  - middleware.go: `type KeyFunc func(*http.Request) string` (L14); `Middleware(l Limiter, key KeyFunc, policy string, failOpen bool) func(http.Handler) http.Handler` (L20: key "" skips; limiter error → pass if failOpen else 503 `Overloaded` Retry-After 1; over → 429 `RateLimited` Retry-After ≥ 1); `SetHeaders(h http.Header, policy string, d Decision)` (L53: `RateLimit: "<policy>";r=<remaining>;t=<secs>`, `RateLimit-Policy: "<policy>";q=<limit>;w=<secs>`); unused `wait` (L60)
- Concurrency: Bucket mutex; Keyed one mutex + expiry heap; Keyed.Allow takes the map lock, then the bucket lock twice (Reserve then Tokens), not atomic as a pair (L169-171).
- Persistence: Keyed is per replica, never persisted; FixedWindow assumes an atomic INCR+EXPIRE Counter (Redis in production, window.go L10-12).
- Tests (ratelimit_test.go): bucket burst/refill/reserve wait; keyed eviction, isolation, refill (injected now); fixed window rollover (injected now); middleware headers, 429, Retry-After, unkeyed pass. Not tested: fail-open/closed, rate 0, MemoryCounter sweep. Coverage 85.0%.

### 1.4 resilience — IN SCOPE (kit/retry, in adapters only)

- Purpose: backoff, retry with budget and Retry-After, gobreaker state machine (backoff.go L1-8).
- API:
  - backoff.go: `type Backoff struct{Base, Max time.Duration; Multiplier, Jitter float64}` (L20-25); `DefaultBackoff()` = Base 1s, Max 120s, Multiplier 1.6, Jitter 0.2 (L28-30); `(Backoff).Delay(retries int) time.Duration` (L33); `ParseRetryAfter(h http.Header, now time.Time) (time.Duration, bool)` (L64: seconds or HTTP-date; past date → (0,true))
  - retry.go: `type RetryAfterError struct{Delay time.Duration; Err error}` (L13-19); `type Budget struct{MaxTokens, TokenRatio float64; ...}` (L26-33; defaults 10 and 0.1, L35-45); `(*Budget).Allow() bool` (tokens > MaxTokens/2, L48-53); `Failure()` (−1, L56); `Success()` (+TokenRatio, L64); `type Policy struct{MaxAttempts int; Backoff Backoff; MaxDelay, PerAttemptTimeout time.Duration; Retryable func(error) bool; Budget *Budget}` (L72-86); `DefaultPolicy()` = 3 attempts, DefaultBackoff, MaxDelay 30s (L89-91); `Retry[T any](ctx, p Policy, fn func(ctx) (T, error)) (T, error)` (L97)
  - breaker.go: `type State int` `Closed=0, HalfOpen=1, Open=2` (L10-16); `ErrOpen`, `ErrTooManyRequests` (L29-34); `type Counts` (L37-43); `type BreakerSettings struct{Name string; MaxRequests uint32; Interval, Timeout time.Duration; ReadyToTrip func(Counts) bool; IsSuccessful func(error) bool; OnStateChange func(name string, from, to State)}` (L46-59); `NewBreaker(s BreakerSettings) *Breaker` (L78: MaxRequests 1, Timeout 60s, ReadyToTrip `ConsecutiveFailures > 5`, IsSuccessful `err == nil`); `(*Breaker).Execute(fn func() error) error` (L97); `Do[T any](b *Breaker, fn func() (T, error)) (T, error)` (L114); `(*Breaker).State() State` (L125)
- Retry loop (retry.go L97-153): stops on parent ctx done (returns `errors.Join(err, ctx.Err())`), on last attempt, on `!Retryable(err)`, on `!Budget.Allow()`; `RetryAfterError.Delay > MaxDelay` aborts; delay capped at MaxDelay; real `time.NewTimer`.
- Concurrency: Budget and Breaker each one mutex; `OnStateChange` is called with `b.mu` held (breaker.go L191-201).
- Persistence: none (per process).
- Tests (resilience_test.go): backoff growth/cap/jitter range; Retry-After seconds and date; stop on non-retryable and on cancel; RetryAfter honoured (real time, < 200 ms); budget stops after 2 calls; breaker closed→open→half-open→closed with injected now; Do. Not tested: PerAttemptTimeout, MaxDelay abort, Interval, generation discard, half-open overflow. Coverage 81.5%.

### 1.5 reconcile — DEFERRED in its durable form (leases, ownership); usable in-process if a controller is needed

- Purpose: Kubernetes workqueue (dirty/processing sets, per-key exponential retry) + level-triggered Controller + Resync (queue.go L1-11).
- API: queue.go `type RateLimiter interface{ When(failures int) time.Duration }` (L22-24); `type Exponential struct{Base, Max time.Duration}` (L28-31; defaults 5 ms, 1000 s, L39-44); `DefaultRateLimiter()` (L54); `type Timer interface{ Stop() bool }` (L59); `type Options struct{Limiter RateLimiter; AfterFunc func(d time.Duration, f func()) Timer; Now func() time.Time}` (L62-69); `New[T comparable](opts Options) *Queue[T]` (L98); methods `Add`, `AddAfter(item, d)`, `AddRateLimited`, `Forget`, `NumRequeues`, `Get() (item T, shutdown bool)`, `Done`, `Len`, `ShutDown`, `ShuttingDown`, `WaitIdle` (L124-273). controller.go `type Requeue struct{After time.Duration}` (L16, implements error); `type Controller[T comparable] struct{Queue *Queue[T]; Sync func(ctx, key T) error; Workers int; Log *slog.Logger; OnError func(key T, err error)}` (L26-34); `(*Controller[T]).Run(ctx) error` (L38); `Resync[T](ctx, q, every time.Duration, list func(ctx) ([]T, error)) error` (L105).
- Concurrency: one mutex + cond; Workers bare goroutines (controller.go L47-54); per-key exclusivity via `processing` set.
- Persistence: none; no leader election, no leases (§6.7.6 L172 requires a Cosmos lease 15 s renewed every 5 s; INTEGRATIONS L49).
- Tests (reconcile_test.go): Exponential; dedupe and re-queue after Done; rate-limited retry, Forget, earlier deadline wins, ShutDown stops timers (fake timers); Get blocks/wakes (sleep 10 ms); controller retries then forgets, Requeue not a failure; panic recovered (weak assertion L251); Resync (real ticker). Coverage 90.3%.

### 1.6 registry — IN SCOPE (module lifecycle, at `core/registry` per §6.7.1 L91)

- Purpose: namespaced module registry `<namespace>.<name>`, Provision → Validate → use → Cleanup, generations (registry.go L1-16).
- API: `type ID string` with `Namespace()`, `Name()`, `Validate()` (lowercase letters, digits, `_`, dot-separated non-empty labels; L32-70); `type Module interface{ ModuleInfo() Info }` (L74); `type Info struct{ID ID; New func() Module}` (L81-84); `Provisioner{Provision(ctx *Context) error}`, `Validator{Validate() error}`, `Cleaner{Cleanup() error}` (L90-107); `type Registry`; `New(reserved ...ID) *Registry` (L119); `var Default = New()` (L128); `(*Registry).Register(m Module)` panics on invalid/reserved/duplicate ID, nil constructor or nil instance (L134-154); `Register(m)` on Default (L157); `Lookup`, `Namespace(ns) []Info` (direct children, sorted), `IDs()` (L160-195); `Conforms[T any](r *Registry, ns string) error` (L200); `ErrUnknownModule`, `ErrWrongType`, `ErrClosed` (L215-219); `type Context`; `NewContext(parent context.Context, reg *Registry) *Context` (L237, nil reg → Default); `Context()`, `Registry()`, `Loaded()`; `(*Context).Load(id ID, raw json.RawMessage) (Module, error)` (L262: `DisallowUnknownFields`, Provision then Validate, Cleanup on failure); `LoadAs[T]`, `LoadInline(c, ns, key, raw)`, `LoadInlineAs[T]` (L321-372); `(*Context).Close() error` (cancel then cleanups in reverse order, joined errors, idempotent; L393-412).
- Concurrency: Registry RWMutex; Context mutex; Load is not atomic with respect to a concurrent Load (unload assumes "most recent", L375-389).
- Persistence: none.
- Tests (registry_test.go): ID parts and invalid IDs; five Register panics; namespace listing and Conforms naming the offender; load lifecycle (provision ran, unknown field, unknown module, validate phase in error, Close once, ErrClosed, ctx cancelled); LoadAs wrong type cleans up (Cleaner case only); reverse cleanup order; inline load; make-before-break. `failProv` field defined but never exercised (L23, L30-32). Coverage 84.8%.

### 1.7 resource — DEFERRED (REVIEW.md R14 row: "Resources (after the journey)")

- Purpose: Kubernetes-style envelope, conditions, generations, finalizers, optimistic concurrency, RFC 9110 preconditions (resource.go L1-10).
- API: `type Meta struct{Name; UID; ResourceVersion; Generation int64; CreatedAt time.Time "omitzero"; DeletionAt *time.Time; Finalizers []string; Labels, Annotations map[string]string}` (L24-50); `type Object[S, T any] struct{APIVersion; Kind; Metadata Meta; Spec S; Status T}` (L55-61); `Deleting`, `HasFinalizer`, `AddFinalizer`, `RemoveFinalizer` (L64-99); `ConditionTrue/False/Unknown` (L105-109); `type Condition` (L114-121); `FindCondition`, `IsConditionTrue`, `SetCondition(conds *[]Condition, c Condition, now time.Time) bool`, `RemoveCondition` (L124-184); errors `ErrNotFound, ErrAlreadyExists, ErrConflict, ErrInvalid, ErrPreconditionFailed, ErrNotModified` (L187-194); `APIError(err) *apierror.Error` (L198); `ETag(rv string) string` (strong, `%q`), `SetETag`, `CheckPreconditions(r *http.Request, current string) error` (L218-249). store.go `type Store[S, T any] interface{Create; Get; List; Update; UpdateStatus; Delete(ctx, name, resourceVersion string)}` (L31-38); `MemoryStore[S,T]`, `NewMemoryStore`, `SetClock`, `Validate` hook (L43-58); names 1-253 chars of `a-z 0-9 - . _`, not starting `-`/`.` (L90-105); ResourceVersion is a per-store counter that moves on every write including status (L60-63, L224); Generation moves only when spec JSON changes (L192-195).
- Tests: create/get/conflict, finalizers and graceful delete, conditions, preconditions/ETag, APIError mapping. Coverage 89.3%.

### 1.8 auth — IN SCOPE (verifier already in kit/jwt; middleware becomes pipeline step 1)

- Purpose: bearer JWT verification (RS/PS/ES), JWKS cache with throttled refresh, Principal, scope guard (jwks.go L1-15).
- API:
  - jws.go: `type Header struct{Alg "alg"; Kid "kid"; Typ "typ"; Crit []string "crit"}` (L19-24); `ErrMalformedToken` (L35); algorithms RS256/384/512, PS256/384/512, ES256/384/512 (L73-83).
  - jwks.go: `type JWK` (L38-48); `(JWK).PublicKey() (crypto.PublicKey, error)` (RSA ≥ 2048 bits, EC P-256/384/521 with on-curve check; L51-93); `type KeyProvider interface{ Key(ctx, kid string) (crypto.PublicKey, error) }` (L104-106); `ErrUnknownKey` (L109); `type KeySet struct{URL string; Client *http.Client; MinRefreshInterval, MaxAge time.Duration; ...}` (L112-130); `NewKeySet(url string) *KeySet` (L133: Client timeout 5 s, MinRefreshInterval 1 min, MaxAge 24 h); `(*KeySet).Key(ctx, kid)` (L146); `(*KeySet).Refresh(ctx) error` (L171); body limit 1 MiB (L226); keys with `use` other than `sig` or no `kid` skipped (L238-240); `Cache-Control: max-age` honoured (L253-263); `type StaticKeys map[string]crypto.PublicKey` (L266).
  - verifier.go: `type Audience []string` (string or array; L14-32); `type Claims struct{Issuer "iss"; Subject "sub"; Audience "aud"; ExpiresAt "exp"; NotBefore "nbf"; IssuedAt "iat" (int64); ID "jti"; Scope "scp"; Scope2 "scope"; Roles []string "roles"; TenantID "tid"; ObjectID "oid"; Email "preferred_username"; Raw map[string]json.RawMessage}` (L36-52); `(*Claims).Scopes()` (L55); errors `ErrInvalidToken`, `ErrExpired`, `ErrNotYetValid`, `ErrIssuer`, `ErrAudience`, `ErrAlgorithm`, `ErrSignature` (L68-76); `type Verifier struct{Issuer string; Audiences []string; Keys KeyProvider; Algorithms []string; Leeway, MaxTokenAge time.Duration; Now func() time.Time}` (L79-94; default RS256 only, Leeway 5 min for nbf, none for exp); `(*Verifier).Verify(ctx, token string) (*Claims, error)` (L97: order alg allowlist / not `none` → no `crit` → `kid` required → key → signature → iss → aud → exp → nbf → max age).
  - middleware.go (NOT imported into Atlas): `type Principal struct{Subject, Issuer, TenantID string; Scopes, Roles []string; Claims *Claims; TokenHash string}` (L18-26; TokenHash = hex of first 8 bytes of SHA-256 of the token, L91, L99); `WithPrincipal`, `PrincipalFrom`, `BearerToken` (L42-60); `Middleware(v *Verifier, realm string)` (L69: 401 `WWW-Authenticate: Bearer realm="<realm>"` without token; with `error="invalid_token", error_description="..."` when invalid; deletes `Authorization` on success); `RequireScope(scope string)` (L109: 403 `PolicyDenied` + `WWW-Authenticate: Bearer error="insufficient_scope", scope="..."`).
- Concurrency: KeySet mutex with one in-flight fetch shared by waiters (refreshLocked releases the lock during the HTTP call, L179-211).
- Persistence: in-memory key cache only.
- Tests (auth_test.go): RS256 and ES256 verify; expired, wrong aud, wrong iss, alg none, no kid, tampered, garbage; nbf within leeway; rotation causes exactly one fetch; unknown kid right after a fetch does not refetch (throttle); middleware 401 challenge, Authorization stripped, principal set, 403 scope. Not tested: PS*, ES384/512, crit, MaxTokenAge, JWKS outage, concurrent refresh, small RSA key, off-curve point. Coverage 73.8%. In Atlas: `kit/jwt/jwt_test.go` carries all but `TestMiddleware` (PROVENANCE.md L22).

### 1.9 shedding — IN SCOPE (edge middleware rows 7 and 11 of §6.7.2)

- Purpose: Admission (chi Throttle: slots + bounded backlog), InFlight (apiserver max-in-flight), ShutdownRetryAfter (apiserver WithRetryAfter) (shedding.go L1-16).
- API: `type Admission`; `NewAdmission(limit, backlog int, backlogTimeout time.Duration, status int) *Admission` (L38; panics if limit < 1; backlog chan holds limit+backlog); `(*Admission).Middleware(next) http.Handler` (L59; rejection `Overloaded` with `Retry-After: 1`); `type Class int` `ReadOnly, Mutating` (L97-102); `ClassifyByMethod(r) Class` (GET/HEAD/OPTIONS read-only; L105); `type InFlight struct{Classify func(*http.Request) Class; LongRunning func(*http.Request) bool; Exempt func(*http.Request) bool; Status int; RetryAfter int}` (L114-127); `NewInFlight(readOnlyLimit, mutatingLimit int) *InFlight` (L130: limit 0 = unlimited; Status 429; RetryAfter 1); `(*InFlight).Middleware` (L142; LongRunning not counted; Exempt consulted only when full); `ShutdownRetryAfter(draining func() bool, retryAfterSeconds int, exempt func(*http.Request) bool) func(http.Handler) http.Handler` (L175: 429 `ShuttingDown`, `Connection: close`, `Retry-After`).
- Concurrency: buffered channels as semaphores.
- Persistence: none.
- Tests (shedding_test.go): admission 1 ok + 2 rejected (sleeps 10/100 ms to order arrivals); in-flight class separation and exempt (sleeps 20 ms; goroutines not joined); draining 429 + Connection: close + Retry-After, probe exempt. Coverage 80.3%.

### 1.10 concurrency — IN SCOPE only for what kit/conc lacks (Semaphore for bulkheads; AMBIGUOUS A6)

- API (concurrency.go): `type Group`; `NewGroup(ctx, limit int) (*Group, context.Context)` (L33; limit ≤ 0 unlimited; ctx cancelled with cause on first error); `(*Group).Go(fn func() error)` (L44; blocks for a slot; panic → error with stack); `(*Group).Wait() error` (L73); `ForEach[T any](ctx, items []T, limit int, fn func(ctx, item T) error) error` (L81); `type Semaphore`; `NewSemaphore(n int)`; `Acquire(ctx) error`; `TryAcquire() bool`; `Release()` (L99-125); `Go(fn func(), onPanic func(v any, stack []byte))` (L129: fire-and-forget).
- Atlas already has its own `kit/conc` (`Go(fn func() error) (wait func() error)`, `Group`, `WithContext`, `SetLimit`, `TryGo`, `Wait`, `PanicError`; atlas-skeleton sdk/go/kit/conc/conc.go L16-134), not listed in PROVENANCE (Atlas-owned).
- Tests (concurrency_test.go): limit 2 and cancel cause; panic → error; ForEach sum; semaphore TryAcquire/Acquire timeout; Go recovers. Coverage 95.7%.

### 1.11 cache — DEFERRED (results never cached, ATLAS-MECH-011; JWKS single flight is inside kit/jwt)

- API (cache.go): `type Flight[K comparable, V any]`; `(*Flight).Do(key K, fn func() (V, error)) (v V, err error, shared bool)` (L34); `type TTL[K, V]`; `NewTTL[K, V](capacity int, ttl time.Duration)` (L80); `Get`, `Set`, `SetTTL`, `Delete`, `Len`, `GetOrLoad(ctx, key, load func(ctx) (V, error)) (V, error)` (L85-160). Mutex + container/list; lazy expiry.
- Tests: flight coalescing (sleep 20 ms), LRU eviction and expiry (injected now), GetOrLoad stampede (one load of 50). Coverage 77.6%.

### 1.12 pagination — IN SCOPE (kit/page; `portfolio.list` and `portfolio.search` page)

- API (pagination.go): `ErrCursorInvalid`, `ErrCursorMismatch`, `ErrCursorExpired` (L27-31); `type Codec`; `NewCodec(key []byte) *Codec` (L48, no key check); `InputHash(input any) string` (L53: SHA-256 of `json.Marshal(input)`, first 8 bytes hex = 16 hex chars, marshal error ignored); `(*Codec).Encode(input, position any, ttl time.Duration) (string, error)` (L61); `(*Codec).Decode(cursor string, input any, position any) error` (L76: > 4096 bytes invalid; HMAC-SHA256 constant-time compare; `v` must be 1; hash mismatch; `now >= exp` expired); `type Page[T any] struct{Items []T "items"; NextCursor string "nextCursor,omitempty"; Truncated bool "truncated,omitempty"}` (L111-115); `ClampPageSize(requested, def, max int) int` (L118).
- Wire format: `base64url(JSON{"v":1,"h":"<16 hex>","p":<position>,"exp":<unix>}) "." base64url(HMAC)` (L9; doc pagination.md L27).
- Tests: round trip, mismatch, forged, wrong key, expired (injected now), clamp. Coverage 87.2%.

### 1.13 session — DEFERRED (§6.7.1 L94: "tokens are never in a cookie")

- API: `NewCodec(key []byte) (*Codec, error)` (32-byte AES-256-GCM; L36); `Seal(plaintext, ad []byte) string`; `Open(s string, ad []byte) ([]byte, error)`; `type Session`; `type Store interface{Get; Put; Delete}`; `ErrNoSession`, `ErrExpired`; `MemoryStore`; `type CookieOptions`; `DefaultCookieOptions(name)` (HttpOnly, Secure, SameSite Lax, 8 h; L150); `NewManager`, `NewID()` (256-bit), `Load`, `Save`, `Rotate`, `Clear` (L163-251). Tests: codec tamper/AD; save/load/rotate/clear; expiry. Coverage 84.3%.

### 1.14 sse — DEFERRED (§6.7.1 L94: "`sse` (D41)")

- API: `type Event struct{ID, Type string; Data []byte; Retry time.Duration}`; `(Event).WriteTo(w io.Writer) (int64, error)`; `LastEventID(r)`; `Upgrade(w, r) (*Stream, error)` (sets `Content-Type: text/event-stream`, `Cache-Control: no-store`, `X-Accel-Buffering: no`, clears write deadline, 200, flush; L77-93); `(*Stream).Send(e)`, `Comment(text)`; `ErrResumeExpired`; `type Ring`; `NewRing(size int)`; `FormatID(n uint64)` (`%020d`); `Put`, `After(lastID)`; `type Options struct{Heartbeat time.Duration; Replay *Ring; OnResumeExpired func(lastID string) Event}`; `Serve(w, r, src <-chan Event, o Options) error`; `ErrClientTooSlow`; `type Sink`; `NewSink(depth int, slowTimeout time.Duration)`; `C()`, `Send(ctx, e)`, `Close()`. Doc heartbeat 15 s (streaming doc L49). Tests: wire format; ring replay/expiry; serve with replay and heartbeat (real server, sleeps); sink backpressure. Coverage 81.3%.

### 1.15 lb — DEFERRED (§6.7.1 L94: "`proxy` and `lb` (until a service host, O21)")

- API: `ErrNoAvailableUpstream`; `type Upstream struct{Name; URL *url.URL; Weight float64; MaxRequests int64; ...}`; `NewUpstream(name, u, weight)` (healthy, weight ≥ 1); `SetHealthy`, `Healthy`, `Full`, `Available(maxFails int64)`, `Acquire`, `Release`, `InFlight`, `Fails`, `RecordFailure(failDuration)` (`time.AfterFunc` per failure); `type Balancer interface{ Pick() (*Upstream, error) }`; `WeightedRoundRobin{MaxFails}` (EDF heap) with `Add`, `Pick`; `PowerOfTwoChoices{MaxFails}` with `Add`, `Pick`. Tests: WRR 300:100 for weights 3:1 over 400 picks; skip unavailable; P2C prefers less loaded; passive failure forgotten (sleep 40 ms); Full. Coverage 90.5%.

### 1.16 proxy — DEFERRED (same as lb)

- API: `DefaultTransport() *http.Transport` (Dial 3 s, KeepAlive 30 s, MaxIdleConnsPerHost 32, IdleConnTimeout 90 s, TLSHandshake 10 s, ExpectContinue 1 s, buffers 64 KiB, `Proxy: nil`, HTTP/2; L43-57); `type Options struct{Balancer lb.Balancer; Transport http.RoundTripper; TrustForwarded bool; FlushInterval, FailDuration time.Duration; MaxRetryBodyBytes int64; Attempts int; Hop string; Log *slog.Logger}` (L60-83); `UpstreamFrom(ctx)`; `New(o Options) http.Handler`; `ParseURL`. Retries only idempotent methods (GET, HEAD, OPTIONS, PUT, DELETE, TRACE; L224-230), with body buffered under MaxRetryBodyBytes, nothing written, not cancelled, not past deadline. Errors: no upstream 503 `UpstreamUnavailable` Retry-After 1; deadline 504; else 502 `UpstreamError`. Tests: X-Forwarded-For replaced when untrusted, Host kept; PUT retried with body; POST not retried; dead upstream removed by passive health; no upstream 503. Coverage 71.4%.

### 1.17 How cmd/gateway and cmd/kernel compose them (reference only; INTEGRATIONS L54: "useful references, not deployable Atlas replacements")

cmd/gateway/main.go chain, outermost first (L126-140): `Recover` → `RequestID("X-Request-Id")` → `RealIP(cidrs, "X-Azure-ClientIP")` → `telemetry.Propagate(false)` → `AccessLog` (probes skipped) → `RequestMetrics(NoopRecorder)` → `ShutdownRetryAfter(!ready, 2, isProbe)` → `MaxBytes(8<<20)` → `Deadline("X-Request-Deadline", 30s, 5m)` → `authn` (`auth.Middleware`, or identity pass-through when `-jwks` is empty, L86-93) → `inflight.Middleware` (`NewInFlight(400, 100)`, SSE `Accept` long-running, L106-107) → `ratelimit.Middleware(NewKeyed(50, 100, 100_000), userKey, "per-user", failOpen=true)` (L96, L138) → `idempotency.Middleware{MemoryStore, Scope: userKey}` (L139). `userKey` = principal Subject, else client IP, "" for probes (L97-105). Mux: `GET /livez`, `GET /readyz`, `GET /v1/operations/{id}`, `POST /v1/operations/{id}/cancel`, `/` → proxy (L118-123). Lifecycle group: signal actor, `httpserver.Run`, `ops.SweepLoop(ctx, time.Minute)` (L147-155); `ops.Wait()` after (L160).

cmd/kernel/main.go: providers registered in `init()` (L130-133); `registry.Conforms[Provider](registry.Default, "provider")` then one generation loading every module of the namespace (L165-179); `resource.MemoryStore` with a Validate hook (L182-191); `config.Hot[Snapshot]` validated by `consistent(loaded)` (L194); `reconcile.Controller{Workers: 2}` converging capabilities into the snapshot and writing `Accepted` / `Ready` conditions (L201-255); routes `GET/PUT/DELETE /v1/capabilities[/{name}]`, `POST /v1/capabilities/{name}/execute` (snapshot only), `GET /v1/catalog`, `GET /v1/providers` (L261-384); chain Recover, RequestID, Propagate(false), AccessLog, MaxBytes(1 MiB), Deadline(10s, 1m) (L386-393).

Differences from Atlas's edge order (§6.7.2 L100-112: Recover, Request id, Real client address, Trace context, Access log, Request metrics, Draining, Body size, Outer deadline, Pre-auth limit, In flight): cmd/gateway puts authn/in-flight/rate limit/idempotency in the chain (Atlas: pipeline steps 1, 4, 5); body 8 MiB (Atlas 1 MiB, ATLAS-MECH-002); `X-Request-Deadline` (Atlas `Atlas-Deadline`, an RFC 9651 Date, §6.7.2 row 9); `Propagate(false)` starts a new root (Atlas row 4: "Continue the caller's `traceparent`"); draining 429 (Atlas `unavailable`); no pre-auth limit (Atlas row 10).

### 1.18 The docs, in brief (background; principles are not normative for Atlas)

- 15 principles + constitution (docs/00-principles/README.md L9-43) map one-to-one onto Atlas's constitution (atlas-skeleton AGENTS.md L17-33). Where Atlas's wording differs, Atlas's wins.
- Values from pattern docs that Atlas also states: server ReadHeaderTimeout 10 s, IdleTimeout 5 m, WriteTimeout 0, MaxHeaderBytes 16 KiB (http-server-lifecycle.md L26-32; Atlas header bytes are 64 KiB, §6.7.15 L268); shutdown readiness off → delay → drain → close (L98-107); JWKS rules: fetch at startup, honour max-age (24 h default), refresh on unknown kid at most once per 1 min, one in-flight fetch, keep serving stale keys if a refresh fails, bound document 1 MiB and client 5 s (bearer-tokens-and-jwks.md L66-73); retry checklist base 100 ms–1 s, multiplier 1.6–2, jitter ≥ 0.2 (retries doc L128); composition order `timeout → retry(budget, backoff) → breaker → per-attempt timeout → call` (retries doc L114-118).
- Doc statements Atlas overrides: registration "at init" (04 L54, L93, L117; overridden by AGENTS.md L13); `nextCursor` "absent ... never null" and `410` on expiry (pagination.md L46, L52; Atlas `next_cursor: null` at end and `invalid_request` target `/cursor`, page.schema.json L19-26, ATLAS-MECH-009); same key different body → 422 (11-idempotency L27; Atlas 409 `idempotency_conflict`); envelope with `spec` (01 L48; AGENTS.md L11); CamelCase terminal states (01 L54; Atlas lowercase); `Repeatability-*` and RateLimit IETF headers (Atlas: `Idempotency-Key` and `Atlas-Limit-Remaining`, §9.6.3).

## 2. Adapting the patterns this build needs

### 2.0 Common copy procedure (every package below)

1. Work on branch `pass-2` (AGENTS.md L72). Commit only when asked.
2. Target: the package named in §6.7.1 (06-7 L76-92): `kit/idem` ← `idempotency`; `kit/ops` ← `lro`; `kit/limit` ← `ratelimit`; `kit/retry` ← `resilience`; `kit/page` ← `pagination`; `kit/conc` ← `concurrency` (exists); `kit/jwt` ← `auth` (exists, verifier only); `core/registry` ← `registry` (gateway module, not kit). Shedding has no §6.7.1 row of its own: it is part of the edge middleware under `kit/serve` in the spec, which the existing kit spells `kit/middleware`/`kit/httpserver` (AMBIGUOUS A6).
3. Copy each `.go` and `_test.go` file verbatim, then prepend the header in the exact form the kit already uses (atlas-skeleton sdk/go/kit/health/health.go L1-3, kit/jwt/verifier.go L1-3):
   ```
   // Adapted from github.com/cottrellashley/cloud-gateway-patterns-go at 44ba81a
   // (patterns/<pkg>/<file>.go), MIT licence: see sdk/go/THIRD_PARTY_NOTICES.md.
   // Changes: import paths[, package name]; later edits are marked ATLAS.
   ```
4. Change the import path and, if renamed, the package clause. Replace every `apierror` use by `kit/problem` (`problem.Write(w, requestID, err)`, kit/problem/problem.go L44) with a `protocol` problem code from §9.3.3. Mark each Atlas edit with `// ATLAS:` and its reason (kit/jwt/jwks.go L92 is the model).
5. Make time injectable through `kit/clock.Clock` (kit/clock/clock.go L14-56) wherever the package calls `time.Now` or holds an unexported `now`; convert sleeping tests to `testing/synctest` or the manual clock (AGENTS.md L56). Replace bare `go` with `kit/conc` only outside the kit (inside `sdk/go/kit` a bare `go` is allowed, AGENTS.md L41).
6. Carry the tests; add a test per requirement id the package proves, naming the id in a comment (AGENTS.md L54); add the repro cases from section 3 as regression tests that assert the fixed behaviour; fuzz every parser (cursor decode, Idempotency-Key, JWS; AGENTS.md L57).
7. Add each file to the table in `sdk/go/THIRD_PARTY_NOTICES.md` ("File here | Adapted from"), and a row to PROVENANCE.md "Imported" (`kit` package | From `patterns/` | Files | Tests carried | Local changes), moving it out of "To import" (PROVENANCE.md L31-44).
8. `make check` must pass: gofmt, vet, atlasvet (noinit, nobarego, nogetenv, nohttperror), staticcheck, `-race` tests, layer check (kit imports only `sdk/go/protocol` and the standard library; layers.toml `[[external]] from = "sdk/go/kit/..." allow = []`), tidy (AGENTS.md L35-50, L70-72). Staticcheck findings to clear on import: section 3 P-SC.

### 2.1 idempotency → `kit/idem` (mechanics) + pipeline step 4 (decision) — IN SCOPE

| Aspect | Library | Atlas requirement | Source |
|---|---|---|---|
| Position | HTTP middleware after authn and MaxBytes | Pipeline step 4 "validate", after resolve (2) and authorise (3), for a write | AGENTS.md L14; §6.7.3 L123; §6.7.8 L197 |
| Applies to | POST/PATCH/DELETE | "every call whose effect is not `read`" (all calls are POST) | §6.7.8 L197; §9.6.3 L35 |
| Missing key | pass, or 400 `InvalidInput` if `Required` | `invalid_request`, target `Idempotency-Key` | §6.7.8 L197 |
| Key syntax | any bytes, ≤ 255 | 1–255 characters of `A-Z a-z 0-9 - _ .` | §9.6.3 L35 |
| Scope | `Scope(r) + ":" + key` | the principal (`oid`) and the address | §6.7.8 L198; ATLAS-MECH-008 |
| Fingerprint | `sha256(method \0 path \0 raw body)` | SHA-256 of the request's JCS form (§9.4); member order and whitespace never matter; "caller-scoped canonical business requests" | §6.7.8 L199; ATLAS-MECH-008; INTEGRATIONS L44 |
| Same key, other request | 422 `IdempotencyKeyReused` | `idempotency_conflict` 409, not retryable | AGENTS.md L15; §9.3.3 L90 |
| Same key, still running | 409 `Conflict`, Retry-After 1 | `in_progress` 409, retryable, `retry_after` | §9.3.3 L91; §6.7.8 L201 |
| Replay | stored status + 5 headers + body ≤ 1 MiB; `Idempotent-Replayed: true` | "The first answer, stored up to 1 MiB; an operation's start answers the same operation" | §6.7.8 L202 |
| 5xx / overflow | record deleted, retry runs again | OVERRIDDEN BY REVIEW (R08): one logical operation per key for its whole retention, uncertain outcomes included; "retain uncertain effects instead of dropping keys on 5xx or response overflow" | REVIEW.md L66; INTEGRATIONS L44; DESIGN-REVIEW L92-98 |
| Activation | always for unsafe methods | "activate by semantic execution needs" | INTEGRATIONS L44 |
| Retention | 24 h | 24 hours, in the `Idempotency` port's store, with a time-to-live | §6.7.8 L203 |
| Downstream | proxy forwards header | the key, or the operation's name, goes to every system that accepts one | §6.7.8 L204 |
| Crash in flight | record stays `InProgress` until Retention (D1) | R08: fence workers after their lease and reconcile; acceptance: kill after upstream commit before receipt persistence, retry the key, get the original, no second effect | REVIEW.md L66; DESIGN-REVIEW L98 |

Steps: (1) keep `Store` as the port shape but add a lease/owner and an outcome state (`in_progress`, `completed`, `uncertain`) to `Record`; Begin stays atomic create-if-absent; (2) take the fingerprint input from the pipeline (JCS bytes from `sdk/go/protocol`), not from `*http.Request`; (3) drop `Delete` on 5xx/overflow; (4) problem codes as above; (5) clock injection (idempotency.go L155, L192); (6) memory fake runs the `porttest` suite (AGENTS.md L55); (7) ATLAS-MECH-008 test: member order and whitespace give the same fingerprint, keys scoped by principal and address.

### 2.2 lro → `kit/ops` (+ `core/operations`, a worker deployable where durable) — IN SCOPE if the journey has operation deliveries (A11)

| Aspect | Library | Atlas requirement | Source |
|---|---|---|---|
| Document | `{id, kind, status, createdAt, updatedAt, endedAt, percentComplete, message, result, error, pollIntervalMs}` | `operation.schema.json`: required `name` (`^operations/[a-z0-9][a-z0-9-]*$`), `address`, `state`, `created`, `updated`; optional `progress`, `result`, `result_location` (> 1 MiB), `profile`, `problem` (required when `failed`), `owner`; `succeeded` requires `result` or `result_location` | references/schemas/operation.schema.json L7-134 |
| States | `Accepted, Running, Succeeded, Failed, Canceled` | `pending`, `running`, `succeeded`, `failed`, `cancelled`, `UNKNOWN` (note `cancelled`, two l) | operation.schema.json L25-34; AGENTS.md L12 |
| IDs | `op_` + base36 nanos + seq: underscore fails the pattern, and guessable | random, `[a-z0-9][a-z0-9-]*` | operation.schema.json L17 |
| Start | in-memory key map | idempotent by key, persisted key-to-operation ownership | §6.7.7 L183; INTEGRATIONS L45 |
| Worker | one goroutine per op | a controller over operation names; running op carries its replica's lease, 30 s, renewed every 10 s, progress checkpointed in its record; bounded pools per service and overall; a full pool leaves the operation `pending`, never a goroutine waiting | §6.7.7 L184-185 |
| Restart | orphans stay Running forever | reconciliation on restart; workers fenced after lease | INTEGRATIONS L45; REVIEW.md R08 |
| Deadline | `Manager.Deadline` → Failed `DeadlineExceeded` | the interaction's `deadline.max`; past it the work is cancelled and the operation `failed` with `deadline_exceeded` | §6.7.7 L186 |
| Deadlines and cancel | request ctx detached | OVERRIDDEN BY REVIEW (R17): start deadline, execution deadline and retention are separate; cancellation reported as requested or confirmed | REVIEW.md L74 |
| Cancel | `POST /v1/operations/{id}/cancel` | `POST <Operation-Location>:cancel`; cooperative, forwarded to every hop (Snowflake by tag); `cancelled` within one poll interval | §6.7.7 L187 |
| Poll | `GET`, weak ETag, 304, Retry-After | `GET` allowed only for polling an operation; `ETag` and `304`; `Retry-After` from the poll interval | §9.6.2 (09-6 L20); §6.7.7 L190 |
| 202 headers | relative `Operation-Location`, `Operation-Id` | `Operation-Location` absolute URL on every 202; `Retry-After` delay-seconds on 202 | §9.6.3 rows `Operation-Location`, `Retry-After` |
| Owner | none | checked on `get`, `list`, `cancel`; another principal's operation → `not_found` | §6.7.7 L189; ATLAS-MECH-007 |
| Panic | Failed `InternalError` | problem `internal`, process lives | ATLAS-MECH-007 |
| Results | in record, unbounded | in the record up to 1 MiB; beyond it `result_location` | §6.7.7 L188 |
| Retention | 24 h, `SweepLoop` | 24 hours after it ends | §6.7.7 L191 |

Steps: rewrite `Operation`/`Status` to the schema (generated types from `sdk/go/protocol` if it has them); keep `Store` CAS shape but add `Owner`, `Lease{Holder, Until}`, idempotency fingerprint; replace `Manager.Start` "spawn goroutine" with "persist then enqueue" (long-running-operations.md L37-39 already describes this change); map cancellation explicitly to `cancelled` (kit/problem.From has no `context.Canceled` case, problem.go L24-41, so a raw cancel would become `internal`); every Store error handled, not discarded (lro.go L214, L216, L232).

### 2.3 ratelimit → `kit/limit` (arithmetic) + edge pre-auth limit + step 5 via `adapter/cosmos` — IN SCOPE

| Aspect | Library | Atlas requirement | Source |
|---|---|---|---|
| Position | middleware after authn | step 5 "limit" in the pipeline; a per-replica bucket only for the pre-auth limit (edge row 10, per client address) and degraded mode | AGENTS.md L14; §6.7.3 L124; §6.7.13 L244; §6.7.2 L111 |
| Model | Keyed bucket per replica; FixedWindow counter | reserved before the work, settled after, on shared counters; one transactional batch of conditional patches; rate limit arithmetic = token bucket on stored state | §6.7.13 L244 |
| Budgets | none | OVERRIDDEN BY REVIEW (R07): one budget authority per metered provider and an atomic reservation set; unsupported combinations of hard budgets refused; protective rate limits kept apart | REVIEW.md L16, L65; DESIGN-REVIEW L84-90 |
| Refusal | 429 `RateLimited`, Retry-After ≥ 1 | `limit_exceeded` 429 retryable with `retry_after` equal to the time to one token; `budget_exceeded` 403 | ATLAS-MECH-013; §9.3.3 L98, budget row |
| Headers | `RateLimit`, `RateLimit-Policy` (IETF draft) | `Atlas-Limit-Remaining`: Dictionary of Integers, one member per limit (`rate=58, concurrency=3`), every metered answer | §9.6.3 |
| Limiter outage | fail-open flag | AMBIGUOUS A15 (not stated for step 5 in what I read) | — |

Steps: keep `Bucket.Reserve` arithmetic (it is x/time/rate's), parameterise it on a stored state `{tokens, last}` so `adapter/cosmos` can do conditional patches; drop `Middleware`/`SetHeaders` (or keep only for the pre-auth edge limit with `limit_exceeded`); delete unused `wait` (staticcheck U1000); inject clock into `Keyed` and `FixedWindow`; ATLAS-MECH-013 test.

### 2.4 resilience → `kit/retry` — IN SCOPE (adapters only)

| Aspect | Library | Atlas requirement | Source |
|---|---|---|---|
| Where | anywhere | at one hop only, nearest the upstream, in the adapter; never in the pipeline or an edge; only adapters import `kit/retry` | §6.7.5 L152; ATLAS-MECH-004 |
| Budget | token bucket: −1 per failure, +0.1 per success, allowed while tokens > MaxTokens/2 | "retries at most one in ten calls to it in a minute"; ATLAS-MECH-006: of 1,000 calls with half failing, retries never exceed one in ten in any minute. INTEGRATIONS L47: "do not claim its token budget is the spec's exact 10%-per-minute rule" | §6.7.5 L153 |
| Breaker | trip on > 5 consecutive (6th) failure, open 60 s, 1 probe | open after five consecutive failures within 30 s; one probe after 15 s; only the upstream's own failures count; open sets the capabilities `unavailable`, closed `available`, within five seconds | §6.7.5 L154 |
| Caller faults | counted (D17, D18) | `invalid_request`, `not_found` never count toward a breaker | ATLAS-MECH-006 |
| Replay safety | caller's predicate | retries only of retryable, replayable work, at one hop, within a budget; add replay safety, deadline and operation recovery rules | AGENTS.md L46; INTEGRATIONS L47 |
| Bulkhead | not in the package (only principle 13 L57-77 sketch) | each upstream its own pool, in-flight limit and bounded queue (twice its concurrency); full → `limit_exceeded` or `unavailable` with `Retry-After`, upstream named in `detail` | §6.7.5 L155-156 |

Steps: keep `Backoff`, `ParseRetryAfter`, `RetryAfterError`, `Retry` loop and `Breaker` state machine; replace `Budget` with a per-dependency sliding count of calls vs retries over 60 s (AMBIGUOUS A10); breaker settings ReadyToTrip `ConsecutiveFailures >= 5`, `Timeout 15s`, `MaxRequests 1`, Interval 30 s (A5); `IsSuccessful` classifies caller faults and caller cancellation as success; don't sleep a backoff past the remaining deadline; inject clock (`Breaker.now` unexported, breaker.go L73); tests to synctest.

### 2.5 registry → `core/registry` — IN SCOPE

| Aspect | Library | Atlas requirement | Source |
|---|---|---|---|
| Registration | `Default` global + `Register` from `init` | in the deployable's one list; never at `init` | AGENTS.md L13, L40 (atlasvet noinit, ATLAS-PACKAGES-008); §6.7.17 L294 |
| IDs | lowercase, digits, `_`, dots; reserved and duplicate panic | `<namespace>.<name>`, lowercase; duplicates and reserved ids fail at start | §6.7.17 L293; ATLAS-MECH-017 |
| Lifecycle | Provision → Validate per module, Cleanup on failure | constructed with no side effects; Provision; Validate; used; Cleanup in reverse order, also when a later module fails | §6.7.17 L295 |
| Generations | caller builds a Context and must Close it on any failure | all modules provisioned and validated, then activated together; a failure cleans up the new generation and leaves the previous one serving; "discard a partially failed new generation completely" | §6.7.17 L296; INTEGRATIONS L50 |
| Old generation | `Close` runs cleanups at once | "drain old users before cleanup" | INTEGRATIONS L50 |
| Conformance | `Conforms[T]` | at start and in CI | §6.7.17 L297 |
| Listing | `Namespace(ns)` IDs | each namespace's modules with their generation and conditions | §6.7.17 L298 |

Steps: delete `Default`, free `Register`/`Lookup`, and nil→Default in `NewContext`; add `LoadGeneration(list)` that loads all and on any error closes the whole new Context and returns every reason; add a reference count or wait group so `Close` waits for in-flight users; fix `unload` (D7). AMBIGUOUS A7: `gateway/core/registry/registry.go` already exists as the capability catalogue registry (L1-9), a different thing.

### 2.6 shedding → edge middleware — IN SCOPE

| Aspect | Library | Atlas requirement | Source |
|---|---|---|---|
| Draining | 429 `ShuttingDown`, `Connection: close`, Retry-After | `unavailable` (503), `Retry-After: 2`, `Connection: close`; edge position 7, before any work | §6.7.2 L108; ATLAS-MECH-016 |
| In flight | read/mutating classes by method; default 429 | one limit per replica; a stream counts by connection; over it `unavailable` with `Retry-After`; last in the edge list | §6.7.2 L112 |
| Classification | GET/HEAD/OPTIONS read-only | every interaction is a POST (AGENTS.md L10), so the method split puts every call in `Mutating`; use one limit | AGENTS.md L10 |
| Order test | — | the edge middleware is one list in §6.7.2's order, and a test asserts it | ATLAS-MECH-001 |
| Admission (backlog) | chi Throttle | not in §6.7.2 (DEFERRED); its shape may serve the bulkhead's bounded queue (§6.7.5 L155) | §6.7.5 L155 |

### 2.7 concurrency → `kit/conc` — IN SCOPE as needed

`kit/conc` already exists (Atlas's own). Add only `Semaphore` (for bulkheads, §6.7.5 L155) and `ForEach` if wanted, with provenance; do not import `concurrency.Go(fn, onPanic)` (fire-and-forget; conflicts with `kit/conc.Go`'s returned wait, ATLAS-MECH-012). Bounds from §6.7.12 L235-240: client handoff 8 chunk downloads at a time; transfers 8 parts at a time, each ≤ 8 MiB; operation worker pools per §6.7.7.

### 2.8 pagination → `kit/page` — IN SCOPE

| Aspect | Library | Atlas requirement | Source |
|---|---|---|---|
| Binding | input hash only | bound to the request without its cursor **and the principal** | §6.7.9 L208; ATLAS-MECH-009 |
| Lifetime | caller's ttl (doc default 10 min) | one hour | §6.7.9 L208 |
| Keys | one key, no length check | HMAC, key from Key Vault; previous key accepted for one lifetime after rotation | §6.7.9 L208 |
| Position | any JSON | holds the store's own continuation | §6.7.9 L208 |
| Refusals | `ErrCursorInvalid`/`Mismatch` (doc 400), `Expired` (doc 410) | another principal, another request, or after an hour → `invalid_request`, target `/cursor` | ATLAS-MECH-009 |
| Page | `{"items", "nextCursor" omitempty, "truncated"}` | `page.schema.json`: required `items` and `next_cursor` (string, minLength 1, or `null` on the last page) | page.schema.json L7-26; §6.7.9 L208 |
| Page size | `ClampPageSize` | sizes from `builtins.schema.json` | §6.7.9 L208 |

Steps: add principal (oid) and address to the bound input; canonicalise with JCS (`sdk/go/protocol`) instead of `encoding/json`; add a key id and accept-previous; map every decode failure (including the position's JSON, D20) to one invalid error; reject keys shorter than 32 bytes; inject clock; fuzz `Decode`.

### 2.9 auth → `kit/jwt` (verifier, already imported) + pipeline step 1 — IN SCOPE

| Aspect | Library | Atlas requirement | Source |
|---|---|---|---|
| Position | `auth.Middleware` | step 1 "authenticate"; middleware not imported | AGENTS.md L14; §6.7.3 L120; PROVENANCE.md L22 |
| Issuer | empty disables the check | required; the gateway refuses to start without an issuer; the tenant's v2.0 issuer (v1 only if the app issues v1) | ATLAS-MECH-005; §6.7.4 L136 |
| Audiences | empty disables the check | the gateway's application id URI and its client id | §6.7.4 L137 |
| Tenant | not checked | require configured tenant | INTEGRATIONS L43 |
| Algorithms | default RS256; allowlist | `RS256` only | §6.7.4 L138 |
| Keys | JWKS URL; refresh on unknown kid, throttled | JWKS from OIDC discovery, fetched at start, refreshed on an unknown `kid` (one fetch for many callers) | §6.7.4 L139 |
| Leeway | 5 min nbf | 5 minutes | §6.7.4 L140 |
| Principal | `Subject` = `sub` | `oid`, never `sub`; `idtyp: app` means a workload | §6.7.4 L141; ATLAS-MECH-005 |
| Refused tokens | `alg none`, unlisted alg, `crit`, no `kid` | same set → `unauthenticated` | ATLAS-MECH-005 |
| Refusal | 401 `WWW-Authenticate: Bearer realm="<realm>"`, `error="invalid_token"` on every invalid token | `401`, `WWW-Authenticate: Bearer realm="atlas"` (with `error="invalid_token"` when expired), `unauthenticated` | §6.7.4 L143 (A8) |
| Identity headers | — | ignored and recorded | ATLAS-ACCESS-007 (§6.7.4 L145) |
| Groups | `roles` | from the token; on overage from Microsoft Graph, cached 10 minutes | §6.7.4 L142 (adapter/entra; DEFERRED unless the fixtures need groups) |
| Delegation | none | stable identity and delegation context; Entra-specific behaviour and OBO where required | INTEGRATIONS L43 (OBO DEFERRED, REVIEW.md step 5 L33) |
| Test issuer | httptest JWKS in tests | the hermetic suite's issuer signs its own tokens and serves its own JWKS, so the real verifier runs in every test | §6.7.4 L145 |

Steps: in the pipeline (not kit, since it is a decision), build the principal from `Claims.ObjectID`, `TenantID` and `Raw["idtyp"]`; make empty Issuer/Audiences a construction error; fix stale-key behaviour (D8) and the exp boundary (D9) in kit/jwt with `ATLAS` markers; run the JWKS fetch on a detached context with its own timeout so one cancelled caller does not fail the shared refresh.

## 3. Pitfalls and defects

### 3.1 Reproduced (repro module, all PASS = defect present)

| Id | File:line | Defect | Matters for Atlas because |
|---|---|---|---|
| D1 | idempotency/idempotency.go L155, L180-193 | A handler panic leaves the record `InProgress` (expires at Retention, 24 h): every retry gets 409 until then; `Complete`/`Delete` errors also ignored (L187, L190) | R08 lease and reconcile; one panic blocks a key for a day |
| D2 | idempotency/idempotency.go L153, L198-206 | Fingerprint ignores the query string (and headers): `POST /x?a=2` with the key of `/x?a=1` replays the first answer | fingerprint must be JCS of the request record (§6.7.8 L199) |
| D3 | lro/lro.go L153, L175-180, L190 | `keys` map is never cleared; after Sweep the same key returns `(nil, true, ErrNotFound)` forever (also unbounded memory) | persisted ownership with retention (§6.7.7, INTEGRATIONS L45) |
| D4 | lro/lro.go L137-139 vs L234-243 | Doc: an error after cancel ends Failed; code: any error with client-cancel cause ends Canceled | R17 requested vs confirmed cancellation |
| D5 | lro/lro.go L175-180 | The start key is not fingerprinted: another kind/request under the same key silently returns the first operation | `idempotency_conflict` must apply to operation starts |
| D6 | cache/cache.go L50-59 | A panic in `Flight.Do`'s fn gives every waiter the zero value and `nil` error (`shared == true`) | silent wrong answer; docs/06-data-and-async/caching.md L20 admits the adaptation is "without that corner" |
| D7 | registry/registry.go L375-379 | `unload` of a wrong-type module that is not a `Cleaner` returns before trimming `loaded`: `Loaded()` lists a module that was refused | generation listing (§6.7.17 L298) |
| D8 | auth/jwks.go L149-160 | After the cache expires (`expiresAt`), a refresh failure rejects even a known kid; doc rule 5 says keep serving stale keys (bearer-tokens-and-jwks.md L72). Also unthrottled once expired (L154 needs `now.Before(expiresAt)`) | an IdP outage after 24 h turns into 401 for everyone |
| D9 | auth/verifier.go L152 | `now.After(exp)` accepts a token at exactly `exp` (RFC 7519 4.1.4: "on or after ... MUST NOT be accepted") | boundary test in the hermetic issuer |
| D10 | ratelimit/bucket.go L149-152 | At capacity, eviction pops the soonest-expiring key even if not expired, resetting its debt: spraying keys lets an exhausted key pass. Comment L132-134 ("an evicted key loses nothing") holds only for expired keys | the pre-auth limit is keyed by client address |
| D11 | sse/sse.go L280-299 | `Sink.Send` after `Close` panics (send on closed channel) | DEFERRED package |
| D12 | sse/sse.go L39-58 | `ID`/`Type` not sanitised and `Data` split only on `\n` (not `\r`): field injection | DEFERRED package |
| D13 | sse/sse.go L165-184 | A malformed `Last-Event-ID` (e.g. `"5"`) compares greater than every 20-digit id: nothing replayed, no error | DEFERRED package; `resume_gap` (§9.3.3) |
| D14 | resource/resource.go L251-262 | `W/` stripped for `If-Match` too: weak comparison where RFC 9110 13.1.1 requires strong | `precondition_failed` correctness (resources DEFERRED) |
| D15 | resource/resource.go L198-215 | `APIError(ErrNotModified)` falls to `apierror.From` → 500 | handler must special-case 304 |
| D16 | resilience/retry.go L81-83 vs L117-133 | Doc: `Retryable == nil` retries every error "except context errors"; code retries a per-attempt `DeadlineExceeded` (only the parent ctx is checked, L121) | explicit `Retryable` required |
| D17 | resilience/breaker.go L88-90, L97-110 | Default `IsSuccessful` counts a caller's `context.Canceled` as a failure: cancelling callers trip the breaker | ATLAS-MECH-006 "only the upstream's own failures count" |
| D18 | resilience/retry.go L117-120 | `Budget.Failure()` is charged before the `Retryable` check, so caller faults (400s) drain the retry budget | ATLAS-MECH-006 |
| D19 | reconcile/queue.go L165-175 | `AddAfter` unlocks to create the timer and re-locks to store the waiter; if the timer fires in between (its callback deletes `waiting[item]` first), a stale waiter stays and a later `AddAfter`/`AddRateLimited` for that key with a later deadline is dropped (L158-161): the key is never retried | ATLAS-RECONCILE-003 |
| D20 | pagination/pagination.go L48, L100 | `NewCodec(nil)` accepted (HMAC with empty key); a position that fails to unmarshal returns a raw `json` error, not `ErrCursorInvalid` (likely 500 instead of `invalid_request`) | ATLAS-MECH-009 |
| D21 | concurrency/concurrency.go L23-29, L65-69 | Zero-value `Group` panics on the first error (`cancel` nil) | use only via `NewGroup` |

### 3.2 By reading (not executed)

| Id | File:line | Defect or pitfall |
|---|---|---|
| P1 | cmd/kernel/main.go L424-433 with `Workers: 2` L203 | `updateRoutes` is Load → copy → mutate → `Swap` with no compare-and-swap: two concurrent syncs lose one route. INTEGRATIONS L48: "Serialize catalogue construction or use correct compare-and-swap" |
| P2 | lro/lro.go L174-199 | `Manager.mu` held across `Store.Create` (I/O under a process-wide lock) |
| P3 | lro/lro.go L195-196 | `context.WithTimeout` cancel discarded (`_ = c`); harmless only because the parent is cancelled in `run`'s defer (L212); vet's lostcancel is silenced |
| P4 | lro/lro.go L290 | `Operation-Location` is a relative path; §9.6.3 requires an absolute URL |
| P5 | lro/lro.go L302-330 | No owner check: anyone with an id can get or cancel; ids are guessable (L161, L277-281) |
| P6 | lro/lro.go L126 | Sweep never removes non-terminal records: orphans after a crash live forever |
| P7 | idempotency/idempotency.go L53-70 | MemoryStore never sweeps expired records (unbounded); clock unexported |
| P8 | idempotency/idempotency.go L141 | scope join with `":"` can collide if scope values contain `:` (`"u1:x"+":"+"y"` = `"u1"+":"+"x:y"`) |
| P9 | auth/verifier.go L137, L140 | Empty `Issuer` or `Audiences` silently disables the check (cmd/gateway L44-46, L92 passes flags that may be empty; empty `-audience` yields `[]string{""}`, which rejects every token) |
| P10 | auth/verifier.go L36-52 | `exp`/`nbf`/`iat` are `int64`: a non-integer NumericDate (allowed by RFC 7519) → `ErrMalformedToken`; `typ` never checked (an ID token with the right `aud` passes) |
| P11 | auth/jwks.go L196, L214 | The shared refresh runs on the first caller's ctx: if that caller is cancelled, every joined waiter fails |
| P12 | auth/verifier.go L115-117 | A JWKS fetch failure is reported as `ErrSignature` (401), not as an upstream outage |
| P13 | auth/middleware.go L93 | Principal is `sub`; Atlas requires `oid` (ATLAS-MECH-005) |
| P14 | ratelimit/window.go L46-52 | MemoryCounter sweeps the whole map on every increment once > 10,000 live keys: O(n) per call |
| P15 | ratelimit/bucket.go L136, L73 | No validation of `rate > 0` or `capacity > 0`: rate 0 divides by zero into a Duration |
| P16 | ratelimit/middleware.go L54-55 | Policy rendered with Go `%q`, which is not RFC 9651 sf-string escaping |
| P17 | resilience/breaker.go L198-200 | `OnStateChange` runs with `b.mu` held: a callback that calls `b.State()` deadlocks |
| P18 | resilience/retry.go L144-150 | Sleeps the backoff even when it exceeds the remaining deadline (waits until ctx ends) |
| P19 | registry/registry.go L276-279 | `json.Decoder.Decode` reads only the first value: trailing data after the module config is accepted |
| P20 | registry/registry.go L393-412 | `Close` cleans up immediately; no drain of in-flight users (INTEGRATIONS L50) |
| P21 | shedding/shedding.go L130-131 | `InFlight` defaults to 429; the shedding doc recommends 503 for capacity behind an edge (load-shedding doc L55) and Atlas requires `unavailable` |
| P22 | shedding/shedding.go L149 | A struct-literal `InFlight` with nil `Classify` panics |
| P23 | concurrency/concurrency.go L125 | `Semaphore.Release` without `Acquire` blocks forever instead of panicking |
| P24 | concurrency/concurrency.go L129-140 | `Go(fn, onPanic)` is fire-and-forget, contrary to the package's own rule (L2-3) |
| P25 | pagination/pagination.go L53-57 | `InputHash` truncates to 64 bits and ignores marshal errors (every unmarshalable input hashes alike) |
| P26 | proxy/proxy.go L195-197 | `up.Release()` not deferred: a panic in `ReverseProxy.ServeHTTP` (e.g. `http.ErrAbortHandler`) leaks the in-flight count (DEFERRED package) |
| P27 | proxy/proxy.go L137-141 | With `TrustForwarded`, `SetXForwarded` overwrites the copied `X-Forwarded-Host`/`-Proto`, contrary to the comment (DEFERRED) |
| P28 | lb/lb.go L32-42 | A literal `Upstream` (not `NewUpstream`) is unhealthy and weight 0 (EDF deadline +Inf) (DEFERRED) |
| P29 | session/session.go L227 | Cookie Max-Age uses `time.Until` (wall clock), not the injected clock (DEFERRED) |
| P30 | cmd/kernel/main.go L291 | `If-Match: *` on a missing resource is allowed to create; RFC 9110 says `*` fails when no representation exists |
| P31 | cmd/kernel/main.go L348-354 | Parameters decoded into `map[string]any`: numbers become float64 (exact decimals lost; acceptance item 5 "exact decimals") |
| P32 | tests: idempotency_test.go L58-59; lro_test.go L13-28; shedding_test.go L31, L34, L57, L65; reconcile_test.go L151, L217, L266; cache_test.go L30, L68; sse_test.go L69; lb_test.go L75; resilience_test.go L74 | Tests sleep or spin on real time; several leave goroutines unjoined (shedding_test.go L56, L64; reconcile_test.go L244). Violates AGENTS.md L56 (inject the clock, goleak, never sleep for expiry) when carried |
| P33 | reconcile_test.go L251 | Weak assertion: `len(delays) == 0 && NumRequeues != 1` passes if either holds |

### 3.3 Staticcheck (P-SC) — must be cleared on import (Atlas's `.bin/staticcheck`)

- `patterns/auth/jwks.go:87:7` SA1019 `curve.IsOnCurve` deprecated (already fixed in kit/jwt, PROVENANCE.md L22).
- `patterns/ratelimit/middleware.go:60:6` U1000 `func wait is unused`.
- `patterns/resource/resource_test.go:107:2` SA4006 value of `obj` never used.

## 4. Ambiguities (also in the structured result)

- A1: The journey's capabilities appear to be reads; `Idempotency-Key` is required only "on every call whose effect is not `read`" (§9.6.3 L35), yet acceptance item 5 lists "idempotent retries" (REVIEW.md L50). Readings: (a) idempotency is built and tested now through a non-read path (e.g. operation start or `holdings-dates` if it is a write); (b) only R08's "metered reads when a retry can duplicate a charge" (DESIGN-REVIEW L96) brings reads in; (c) item 5 refers to the clients' retry behaviour, not the key.
- A2: Fingerprint input. (a) JCS of the request as sent, validated under its own contract before any transformation (R04, REVIEW.md L62); (b) JCS after step 4 resolves to the current profile and fills defaults (§6.7.3 L123). Whether the address and `Atlas-Profile-Expect` are inside the fingerprint or only in the scope is not stated beyond "scope: principal and address" (§6.7.8 L198).
- A3: Which response headers a replay restores (library: Content-Type, Location, Operation-Location, ETag, X-Error-Code). Atlas lists none; candidates `Atlas-Profile`, `Atlas-Governance-Revision`, `Atlas-Error-Code`, `Operation-Location`, `Retry-After`; `X-Request-Id` presumably the new call's.
- A4: A first answer larger than 1 MiB: §6.7.8 stores "up to 1 MiB"; R08 forbids dropping the key. Readings: store a digest/pointer; or force such calls to be operations (result_location); or refuse.
- A5: Breaker "five consecutive failures within 30 s" (§6.7.5 L154): tumbling (gobreaker `Interval` 30 s) or sliding window.
- A6: Package homes: §6.7.1 names `kit/serve` for lifecycle/httpserver/health/middleware/telemetry, but the kit already has `kit/health`, `kit/httpserver`, `kit/lifecycle`, `kit/middleware`, `kit/telemetry` (PROVENANCE.md L19-25). Shedding and pre-auth limit: `kit/middleware`, a new `kit/shed`, or `kit/serve`. `concurrency`: merge into existing Atlas-owned `kit/conc` or skip.
- A7: The task says copy into `sdk/go/kit`; §6.7.1 L91 puts the module registry in `core/registry`, which already exists as the capability catalogue registry (gateway/core/registry/registry.go L1-9). Readings: extend it; or a separate module-lifecycle package beside it.
- A8: `WWW-Authenticate` `error="invalid_token"`: §6.7.4 L143 says "when expired"; RFC 6750 and the library send it for every invalid token.
- A9: Draining answers `unavailable` (503) per §6.7.2 and ATLAS-MECH-016, while the library and Kubernetes use 429; not ambiguous for Atlas (spec wins), listed so nobody copies the 429.
- A10: Retry budget "at most one in ten calls to it in a minute": a per-dependency sliding 60 s window, or a fixed minute; counted per replica or shared.
- A11: Whether the first build needs `kit/ops` at all: only if a journey interaction has an operation delivery ("plus a worker where durable work needs one", task text / REVIEW.md L13). Not determinable from my files.
- A12: "a stream counts by connection" for the in-flight limit (§6.7.2 L112) with streaming deliveries in scope or not.
- A13: Pagination: does the bound "request" include the address/profile, and is the hash JCS (protocol's) or any canonical JSON.
- A14: §6.7.10 says the snapshot is built "from Cosmos DB's change feed, never by polling", while the library's `config.Watch` polls (dynamic-configuration.md L66) and REVIEW.md keeps building without real systems. Out of my packages; noted for the hot/registry reader.
- A15: Step-5 limiter outage behaviour (fail open vs closed) is not stated in what I read; the library offers a flag; the rate-limiting doc says fail open for soft limits, closed for billing-grade (rate-limiting doc L82).
