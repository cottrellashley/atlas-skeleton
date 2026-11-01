# Provenance of the kit and the SQL library

The review's INTEGRATIONS asks that both required repositories enter the first implementation with a provenance manifest. The manifest records the upstream repository, the exact commit, the imported packages, the notices, the local changes and the tests carried. A release never tracks an unreviewed branch head. Licences are in [`../THIRD_PARTY_NOTICES.md`](../THIRD_PARTY_NOTICES.md).

## cloud-gateway-patterns-go

| | |
|---|---|
| Repository | https://github.com/cottrellashley/cloud-gateway-patterns-go |
| Commit | `44ba81aa92ee901c131c0b80dcadafa25de440a2` (2026-10-30), the commit the review inspected |
| Module | `github.com/cottrellashley/cloud-gateway-patterns-go`, Go 1.25 |
| Route | Copied into `sdk/go/kit`, each file with a header naming its source. A firm's production build then depends on no mutable personal repository; the proof plan chose this route, and INTEGRATIONS keeps it unless the team picks a pinned module |
| Atlas's edits | Marked `ATLAS` in the code |

### Imported

| `kit` package | From `patterns/` | Files | Tests carried | Local changes |
|---|---|---|---|---|
| `health` | `health` | `health.go` | `health_test.go` | Header. `http.Error` replaced with a status and body write (atlasvet) |
| `hot` | `config` | `config.go` | `config_test.go` | Header and package name |
| `httpserver` | `httpserver` | `httpserver.go` | `httpserver_test.go` | Header |
| `jwt` | `auth` | `jwks.go`, `jws.go`, `verifier.go` | `jwt_test.go` (from `auth_test.go`) | Header and package name. The EC on-curve check goes through `crypto/ecdh` instead of the deprecated `IsOnCurve` (staticcheck SA1019). `ErrMalformedToken` wraps `ErrInvalidToken`, so a malformed token is unauthenticated rather than a verifier failure. `TestMiddleware` is not carried, because the auth middleware is not imported yet (below) |
| `lifecycle` | `lifecycle` | `lifecycle.go` | `lifecycle_test.go` | Header |
| `middleware` | `middleware` | `middleware.go` | `middleware_test.go` | Header. Failures are RFC 9457 problems through `kit/problem`, not `apierror`. The deadline header is an RFC 9651 Date (`Atlas-Deadline`), not RFC 3339, and `X-Request-Deadline` is no longer sent |
| `telemetry` | `telemetry` | `accesslog.go`, `logging.go`, `route.go`, `traceparent.go`, `writer.go` | `telemetry_test.go` | Header. The access log no longer names the principal or the upstream, since `auth` middleware and `proxy` are not imported |

`kit/problem` is Atlas's own: the RFC 9457 writer that replaces `apierror`.

**Known defect, carried from upstream.** `middleware.Deadline` turns a client deadline already in the past into the route's maximum, instead of refusing the call. It also reads the wall clock rather than an injected one. Fixed in step 3, with R17.

### To import, and what Atlas adds

These follow the table in INTEGRATIONS, "Cloud patterns: reuse and adapt". Each enters with the step that needs it, and is recorded here when it does.

| From `patterns/` | Atlas's responsibility |
|---|---|
| `auth` (middleware) | A configured issuer, audience and tenant; stable identity and delegation context; Entra and OBO |
| `idempotency` | Caller-scoped canonical business requests instead of method, path and raw body. Uncertain effects are kept, never dropped on a 5xx (R08) |
| `lro` | Persisted key-to-operation ownership, bounded durable workers, leases and fencing, reconciliation on restart |
| `ratelimit` | The budget authority model, reservations, settlement and shared concurrency (R07) |
| `resilience` (retry) | Replay safety, deadlines and operation recovery. Its token budget is not claimed to be the spec's one-in-ten rule |
| `reconcile` | Durable ownership, distributed leases, outbox handling |
| `registry` | Explicit registration; a failed generation is discarded whole; old users drain before cleanup |
| `resource` (store) | Masks and mandatory preconditions; a full-representation ETag apart from `generation` (R14) |

## sqlglot-go

It is ported into this repository as its own module, [`sqlglot/`](../../../sqlglot) (`github.com/jhgonline/fi-app-atlas/sqlglot`), at commit `3d81e5393850e249136284859f6203a34db29f16`, with every test. It is not taken as a dependency. Its provenance, and the changes the port made, are in [`sqlglot/PROVENANCE.md`](../../../sqlglot/PROVENANCE.md).

| | |
|---|---|
| What is used | Full-statement parsing (never `ParseOne` on text that is then executed), the AST, strict generation in the Snowflake and DuckDB dialects, and qualification under a strict configuration. The optimiser and the in-memory executor stay out of production |
| Integration rules | Each comes from INTEGRATIONS, "four important source-level details":<br>- exactly one executable statement;<br>- refuse `exp.Command` and unknown nodes;<br>- unsupported transformations fail, and no driver call follows;<br>- qualification is not authorisation;<br>- bounded input size, AST depth and compilation concurrency, because parsing takes no cancellation context |
| Tests at the boundary | INTEGRATIONS, "what to test at the integration boundary". The references' `vectors/guard.json` is the first of them |
