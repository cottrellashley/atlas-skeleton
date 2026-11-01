# §9.6 The HTTP binding

`spec/protocol/http.md` · proposed

> Section 9.6 of the Atlas Protocol: exactly how a call travels over HTTP: URLs, methods, bodies, every header and its Structured Field grammar, the status codes, and the framing of each delivery (single, chunked as NDJSON or Arrow, operation, transfer, a handoff, and the connect handover a subscription answers with), sign-in challenges and caching.

## Purpose

To fix every detail of a call over HTTP, so a client in any language with an HTTP library can make it.

### 9.6.1 Connections and URLs

- HTTPS only (TLS 1.2 or later), over HTTP/1.1 (RFC 9112) or HTTP/2 (RFC 9113).
- The bootstrap is `GET https://<gateway>/.well-known/atlas` (RFC 8615): no sign-in, `application/json`, a strong `ETag`, revalidated with `If-None-Match`.
- An interaction's URL is its binding's `server`'s `url` (from the bootstrap; no trailing `/`) followed by the binding's `address` (which begins with `/`). A client never constructs an address itself.
- An operation's URL is its `Operation-Location`; cancelling it is `POST <Operation-Location>:cancel`.

### 9.6.2 Requests

- **Method:** `POST` for every interaction (`atlas/1.0` bindings declare `"method": "POST"`). `GET` only for the bootstrap and for polling an operation.
- **Body:** the request record, `Content-Type: application/json`, valid against the interaction's request schema (§9.2). A request with no parameters sends `{}`. A resource is named by its `name` member in the body; addresses have no templates.
- **Protocol options** travel in headers, never in the body, so request schemas stay the capability's own.

### 9.6.3 Headers

Every Atlas header is a Structured Field (RFC 9651); its type is given. A receiver ignores an Atlas header it does not know, and treats one that does not parse as its type as absent.

| Header | Direction | Type | Value | Required |
|---|---|---|---|---|
| `Authorization` | request | — | `Bearer <access token>` (RFC 6750) | Every call except the bootstrap |
| `Atlas-Client` | request | Dictionary | `name` (String), `version` (String), `protocol` (String, `protocol-ver`) | Every call |
| `Atlas-Client-Features` | request | Dictionary of Inner Lists of Tokens | `levels`, `lens`, `deliveries`, `encodings`, `traits` | Every call |
| `Atlas-Profile-Expect` | request | String | An `exact-id`: the profile the request was written under | Every interaction call, except to a built-in (§9.3.2) |
| `Atlas-Deadline` | request | Date | The absolute deadline, in seconds since the epoch | Every interaction call |
| `Idempotency-Key` | request | String | 1–255 characters of `A-Z a-z 0-9 - _ .`, unique per intended change | Every call whose effect is not `read` |
| `If-Match` | request | — | The resource's ETag (RFC 9110) | `update` and `delete` |
| `Atlas-Execution` | request | Token | `gateway`, `handoff` or `client` (a preference) | Optional |
| `X-Atlas-Interactive` | request | Boolean | `?1` when a person is at the client | Optional |
| `Atlas-Confirm` | request | Decimal | The highest estimated cost the person accepted for this call, in the unit of the problem's `estimate` | After `confirmation_required` |
| `traceparent`, `tracestate` | both | — | W3C Trace Context | `traceparent` on every call |
| `Atlas-Profile` | response | String | The `exact-id` the result was written under | Every interaction answer |
| `Atlas-Governance-Revision` | response | String | The governance revision the call was decided under | Every interaction answer |
| `Atlas-Limit-Remaining` | response | Dictionary of Integers | One member per limit (`rate=58, concurrency=3`) | Every metered answer |
| `Atlas-Error-Code` | response | Token | The problem's `code` | Every problem |
| `X-Request-Id` | response | — | The call's id | Every answer |
| `Deprecation` | response | Date (RFC 9745) | When something this call used was deprecated | When applicable |
| `Sunset` | response | HTTP-date (RFC 8594) | When it ends | When applicable |
| `Retry-After` | response | delay-seconds only | Seconds to wait | `202`, `429`, `503` |
| `Operation-Location` | response | — | Absolute URL of the operation | Every `202` |
| `Cache-Control` | response | — | `no-store` | Every handoff, connect, ticket and transfer answer |

`Atlas-Client-Features` tokens: `levels` from `core streaming columnar bulk events handoff` (and `broker`, reserved); `handoff` the chunk formats the client decodes (`snowflake-arrow/1`), plus `reach-snowflake` when it can reach Snowflake's result storage; `lens` from the operations of §9.5.3; `deliveries` from `single chunked subscription operation transfer`; `encodings` from `json ndjson arrow csv parquet`; `traits` the trait names the client understands, lowercased. Example:

```http
Atlas-Client: name="fiatlas", version="2.3.0", protocol="atlas/1.0"
Atlas-Client-Features: levels=(core streaming columnar bulk events handoff), handoff=(snowflake-arrow/1 reach-snowflake), lens=(rename alias_value convert_unit promote default drop), deliveries=(single chunked subscription operation transfer), encodings=(json ndjson arrow csv parquet)
Atlas-Profile-Expect: "portfolio.holdings@v1#bcaa5bbad97cc612"
Atlas-Deadline: @1793318400
```

### 9.6.4 Choosing the encoding

The client sends `Accept` listing the media types it reads (§9.1.7), from the interaction's binding's `encodings`, in preference order. The gateway answers in the first it can serve, or `406` with the problem `invalid_request` (target `Accept`). A client SHOULD NOT send `q` values; the gateway ignores them.

### 9.6.5 Answers by delivery

| Delivery | Success | Body |
|---|---:|---|
| `single` | `200` | The result as JSON: a record; a `collection` as a page; a `table` as a JSON array of row records |
| `chunked` | `200` | NDJSON or an Arrow IPC stream, framed as below |
| `subscription` | `200` | A connect handover (`connect.schema.json`, `application/vnd.atlas.connect+json`), rights `listen`; `Cache-Control: no-store`. The client then connects to Service Bus itself (§9.7.8) |
| `publish` of an event capability | `200` | A connect handover, rights `send`; `Cache-Control: no-store` |
| `operation` | `202` | The operation document; `Operation-Location`, `Retry-After` |
| `transfer` | `202` | An operation whose result, when `succeeded`, is a transfer document |
| `fetch` answered by a handoff | `200` | The handoff document, `application/vnd.atlas.handoff+json`; `Cache-Control: no-store`. The client then `GET`s each chunk `url` with its `headers`, in parallel, retrying a failed chunk; when links expire, it asks the handoff's `operation` again for fresh ones |
| A ticket (`:ticket`, deferred) | `200` | The ticket document; `Cache-Control: no-store` |
| A failure | the problem's status | The problem document, `application/problem+json` |

**NDJSON.** One JSON record per line, each a row (for a table) or an item (for a collection), each line ended by a single `LF`, no blank lines. The **last** line is the end line (`chunk-end.schema.json`): `{ "$atlas": "end", "rows", "truncated", "usage", "problem" }`. A failure after the first row is reported in the end line's `problem`, not by the status, which was already sent. A body that ends without an end line is incomplete: the client treats it as the problem `upstream_failed`.

**Arrow.** The Arrow IPC **streaming** format: the schema message (§9.2.7, with `atlas:profile` and `atlas:protocol` metadata), record batches, then a final record batch of **zero rows** whose message custom metadata `atlas:end` holds the end line's JSON, then the end-of-stream marker. A stream without that final batch is incomplete, as for NDJSON.

**Server-sent events** are not part of `atlas/1.0`: the binding protocols `sse` and `wss` are reserved for a later minor version, and the gateway serves no subscription over HTTP.

**Operations.** The client polls `GET <Operation-Location>` no sooner than `Retry-After` (default 1 second, doubling to at most 30), until `state` is `succeeded`, `failed` or `cancelled`. A `result` larger than 1 MiB is given by `result_location`, fetched with `GET` and the same `Authorization`. An operation is kept at least 24 hours after it ends.

**Transfers.** A download's `parts` are fetched with `GET`, each verified by its `sha256`; a failed part is fetched again. An upload's `parts` are each sent with `PUT` (at most 8 MiB each, the order free); a failed part is sent again; then the client calls the transfer's `commit` binding with `{ "operation", "parts": [ { "url", "sha256" } ] }` and the same `Idempotency-Key`. Links are used only until `expires`.

### 9.6.6 Signing in, and challenges

A missing or invalid token is `401` with `WWW-Authenticate: Bearer realm="atlas"`, plus `error="invalid_token"` when it is expired; a conditional-access claims challenge adds `claims="<base64url>"`. A client refreshes or re-acquires the token (§9.7) and repeats the call once.

### 9.6.7 Caching

Only the bootstrap and `atlas.describe` answers are cacheable: the bootstrap by `ETag`, a profile by its fingerprint (immutable; `Cache-Control: max-age=31536000, immutable` when fetched by `exact`). Everything else is `no-store`.

## Rules

1. The gateway never sends a header from 9.6.3 that does not parse as its type.
2. A client sends `Atlas-Client`, `Atlas-Client-Features`, `Atlas-Profile-Expect` (for interactions other than built-ins), `Atlas-Deadline` and `traceparent` on every call, and `Idempotency-Key` on every non-`read` call.
3. An answer without an end line or final batch is never treated as complete.

## Units

`references/schemas/chunk-end.schema.json`, `connect.schema.json`, `handoff.schema.json`, `operation.schema.json`, `transfer.schema.json`.

## Requirements

- **AP-HTTP-001.** Every client parses every Atlas header in the header vectors exactly, and rejects the malformed ones.
- **AP-HTTP-002.** Every client treats a truncated NDJSON or Arrow answer as `upstream_failed`.
