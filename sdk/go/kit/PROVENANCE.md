# Gateway utilities: implementation and maintenance

These utilities live in the local `atlas.local/sdk/go` module. They provide reusable
mechanics for gateway code; the gateway pipeline decides policy and middleware order.
Keep their responsibilities narrow and use the existing packages when extending the
gateway. The copyright and permission notice is in
[THIRD_PARTY_NOTICES.md](../THIRD_PARTY_NOTICES.md).

## Package responsibilities

| Package | Responsibility and maintenance constraint |
|---|---|
| [clock](clock) | Injectable time for request budgets and deterministic tests. Do not introduce direct wall-clock reads into lifecycle decisions. |
| [conc](conc) | Shared goroutine helpers. Preserve explicit ownership, cancellation and bounded work. |
| [health](health) | Bounded health probes. Readiness should reflect the dependencies needed to serve the configured routes. |
| [hot](hot) | Immutable configuration snapshots. Publish a complete valid generation; discard a failed generation whole. |
| [httpserver](httpserver) | Explicit HTTP serving and shutdown ownership. |
| [jwt](jwt) | JWT verification against configured trust. Distinguish invalid credentials from verifier infrastructure failures. |
| [lifecycle](lifecycle) | Owned startup and cleanup. Drain existing users before cleaning up their resources. |
| [middleware](middleware) | Reusable HTTP mechanics. The gateway owns the ordering of authentication, authorization, deadlines, limits and idempotency. |
| [problem](problem) | Shared RFC 9457 problem responses. Avoid ad hoc error bodies at integration boundaries. |
| [telemetry](telemetry) | Bounded access logs and trace propagation. Keep credentials, request bodies and query text out of logs. |

The repository's `make check` includes these packages and their tests, along with
formatting, static analysis and module checks.

## Implementation lessons

- JWT elliptic-curve validation uses `crypto/ecdh` instead of the deprecated
  `IsOnCurve`. `ErrMalformedToken` wraps `ErrInvalidToken` so malformed credentials
  are treated as unauthenticated requests rather than verifier failures.
- The deadline header is `Atlas-Deadline`, encoded as an RFC 9651 Date. Use the
  protocol's deadline semantics consistently across clients, gateway and adapters.
- Response wrappers must preserve streaming and response-control interfaces and
  record the actual delivery outcome.
- Access logs and durable audit evidence have different responsibilities. A log
  entry is not proof that an upstream effect did or did not happen.

**Known utility caveat:** `middleware.Deadline` currently replaces an already-expired
client deadline with the route maximum and reads the wall clock directly. Do not
assume that helper provides the full gateway deadline policy. Follow the lifecycle
implementation and its current evidence in [HANDOFF-PROGRESS.md](../../../docs/HANDOFF-PROGRESS.md)
when changing deadline behavior, and cover the expired-deadline case explicitly.

## Responsibilities to implement at the owning boundary

These are implementation constraints, not a request to add a new framework or to
replace working adapters. The current status and order of work are in
[HANDOFF.md](../../../docs/HANDOFF.md) and [LESSONS.md](../../../docs/LESSONS.md).

| Responsibility | Required behavior |
|---|---|
| Upstream identity | Configured issuer, audience and tenant; stable caller and delegation context; explicit Entra and on-behalf-of handling where needed. |
| Idempotency | Caller-scoped canonical business requests. Preserve uncertain effects instead of dropping ownership after an upstream failure. |
| Durable operations | Persisted key-to-operation ownership, bounded workers, leases and fencing, and recovery after restart. |
| Budgets | Shared authority for reservations, settlement and concurrency. |
| Retries | Replay safety, remaining deadlines and operation recovery. A generic retry token budget does not establish the protocol's retry allowance. |
| Reconciliation | Durable ownership, leases and outbox handling for Atlas-owned state. Do not duplicate upstream control planes. |
| Registration | Explicit registration and atomic configuration generations; drain old users before cleanup. |
| Owned resources | Field masks, mandatory preconditions and a full-representation ETag distinct from generation. |

## Local SQL library

The SQL foundation is the local [sqlglot](../../../sqlglot) module,
`atlas.local/sqlglot`. Its packages, generated code, tests and fixtures are included
in this repository. See its [maintenance notes](../../../sqlglot/PROVENANCE.md).

The gateway uses full-statement parsing, the expression tree, strict Snowflake and
DuckDB generation, and qualification under a strict configuration. Keep the
in-memory executor separate from warehouse execution, and do not treat optimizer
output as a warehouse cost estimate.

At the integration boundary:

- Require exactly one executable statement; never validate only `ParseOne` and then
  execute the original text.
- Refuse `exp.Command` and unknown nodes.
- Fail unsupported transformations before calling a driver.
- Treat qualification and authorization as separate checks.
- Bound input size, expression-tree depth and compilation concurrency; parsing does
  not accept a cancellation context.

The [guard vectors](../../../spec/protocol/references/vectors/guard.json) and
[Snowflake adapter](../../../gateway/adapter/snowflake) anchor the boundary tests.
