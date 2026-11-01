# Digest: the code already in Atlas (working tree, 2026-10-31)

> Historical engineering study (2026-10-31). Retained for its requirements, counterexamples and design reasoning. Implementation observations and line references describe that review snapshot; use [current progress](../../HANDOFF-PROGRESS.md) and the named source symbols for current status. Paths are relative to the repository root unless a citation key says otherwise.

Review scope: the protocol, gateway, shared kit and SQL modules during the second implementation. The API inventory below is historical; current source symbols and the handoff progress ledger take precedence.
Governing: `AGENTS.md`, `docs/pass-2/REVIEW.md`.

Paths below are relative to the repo root. `file:L` is a line in the file as it stands. Markers:
- **IN SCOPE**: the first build (holdings, list/search, data.status, then holdings-dates; CLI, Python, Node; one gateway plus a worker) uses or must finish it.
- **DEFERRED**: handoff, events, real systems, resources (REVIEW.md "What changes"; R10, R14, R15, R16, R25).
- **OVERRIDDEN BY REVIEW (Rnn)**: REVIEW.md changes what the code or the spec does.
- **AMBIGUOUS**: written out with each reading.

Module layout (`go.work` L3-9): `./gateway`, `./gateway/local`, `./sdk/go`, `./sqlglot`, `./tools`; `go 1.25.0`.

| Module | Path | go | Requires |
|---|---|---|---|
| `atlas.local/sdk/go` | `sdk/go/go.mod` | 1.25.0 | `github.com/apache/arrow-go/v18 v18.8.0` (and indirects) |
| `atlas.local/gateway` | `gateway/go.mod` | 1.25.0 | `sdk/go v0.0.0`, `sqlglot v0.0.0-00010101000000-000000000000`; `replace` to `../sdk/go`, `../sqlglot` |
| `atlas.local/gateway/local` | `gateway/local/go.mod` | 1.25.0 | `github.com/duckdb/duckdb-go/v2 v2.10506.0`, `gateway v0.0.0`; `replace` gateway `=> ..`, sdk, sqlglot |
| `atlas.local/sqlglot` | `sqlglot/go.mod` | 1.23 | none (stdlib only) |
| `atlas.local/tools` | `tools/go.mod` | 1.25.0 | `github.com/BurntSushi/toml v1.6.0`, `golang.org/x/tools v0.49.0`; `tool` directives for `govulncheck`, `staticcheck` (`honnef.co/go/tools v0.7.0`) |

Baseline run by this reader (no file in the repo changed): `go -C gateway vet ./...` clean; `go -C gateway vet -vettool=.bin/atlasvet ./...` clean; `staticcheck ./...` in `gateway` clean; `go -C gateway test -count=1 ./...` ok (adapter/snowflake, core/registry, fakes); `go -C gateway/local test -count=1 ./...` ok (duckwh). `make check` exits 0 (section 3.4, BASELINE).

---

## 1. Packages: purpose and exported API

### 1.1 `sdk/go/protocol` (root package `protocol`)  IN SCOPE

Purpose (`protocol.go` L1-13): atlas/1.0 in Go: the version, the words, media types, headers and their Structured Field types, the problem registry, fingerprints. Imports only stdlib, its subpackages and Arrow (layers.toml).

#### Constants (`protocol.go`)

| Name | Value | Line |
|---|---|---|
| `Version` | `"atlas/1.0"` | L16 |
| `WellKnownPath` | `"/.well-known/atlas"` | L19 |
| `LevelCore` `LevelStreaming` `LevelColumnar` `LevelBulk` `LevelEvents` `LevelHandoff` `LevelBroker` | `core` `streaming` `columnar` `bulk` `events` `handoff` `broker` ("reserved until O54 closes; never offered") | L23-31 |
| `DeliverySingle` `DeliveryChunked` `DeliverySubscription` `DeliveryOperation` `DeliveryTransfer` | `single` `chunked` `subscription` `operation` `transfer` | L34-40 |
| `EncodingJSON` `EncodingNDJSON` `EncodingArrow` `EncodingCSV` `EncodingParquet` | `json` `ndjson` `arrow` `csv` `parquet` | L43-49 |
| `LensRename` `LensAliasValue` `LensConvertUnit` `LensPromote` `LensDefault` `LensDrop`; `LensOperations` (that order) | `rename` `alias_value` `convert_unit` `promote` `default` `drop` | L52-63 |
| `ExecutionGateway` `ExecutionHandoff` `ExecutionClient` | `gateway` `handoff` `client` | L66-70 |
| `HandoffSnowflakeArrow1`, `ReachSnowflake` | `snowflake-arrow/1`, `reach-snowflake` (DEFERRED) | L74-77 |
| `MediaJSON` | `application/json` | L81 |
| `MediaProblem` | `application/problem+json` | L82 |
| `MediaNDJSON` | `application/x-ndjson` | L83 |
| `MediaArrowStream` | `application/vnd.apache.arrow.stream` | L84 |
| `MediaCSV` | `text/csv` | L85 |
| `MediaParquet` | `application/vnd.apache.parquet` | L86 |
| `MediaProfile` | `application/vnd.atlas.profile+json` | L87 |
| `MediaCloudEvent` | `application/cloudevents+json` (DEFERRED) | L88 |
| `MediaHandoff` | `application/vnd.atlas.handoff+json` (DEFERRED) | L89 |
| `MediaConnect` | `application/vnd.atlas.connect+json` | L90 |

`func MediaFor(encoding string) (string, bool)` (L94): the media type of an encoding, for the five encodings above.

#### The problem registry (`problem.go`)

`type Problem struct` (L15-30), JSON members exactly:

| Field | JSON | omitempty |
|---|---|---|
| Type | `type` | no |
| Title | `title` | no |
| Status | `status` | no |
| Detail | `detail` | yes |
| Instance | `instance` | yes |
| Code | `code` | no |
| Retryable | `retryable` | no |
| RetryAfter | `retry_after` | yes |
| Target | `target` | yes |
| Next | `next` | yes |
| Hop | `hop` | yes |
| Estimate | `estimate` (`*Estimate`) | yes |
| Details | `details` (`[]Problem`) | yes |
| RequestID | `request_id` | yes |

`type Estimate struct { Amount string "amount"; Unit string "unit" }` (L36-39): Amount is a decimal string, at most three fraction digits (assumption A-001). OVERRIDDEN BY REVIEW (R06): `Atlas-Confirm` with an estimate is the client's claim only; consent becomes a receipt (step 3).

Methods: `(*Problem) Error() string` (L41, `"<code>: <detail> (<target>)"`); `WithTarget(t)`, `WithNext(n)`, `WithRetryAfter(seconds int)`, `WithDetails(d ...Problem)`, `WithEstimate(e Estimate)` (L50-62), each returns `*Problem`; `(*Problem) Check(declared ...Declared) error` (L207-237): code matches `problem-code`, `type == ProblemTypeBase+code`, status and retryable equal the registry's (or the declared code's), recursively for `details`.

`const ProblemTypeBase = "https://atlas.jhg.com/problems/"` (L94). `type Registered struct{ Code string; Status int; Retryable bool; Title string }` (L97-102). `var Registry` (L107-132), §9.3.3 in order:

| Const | code | status | retryable | title | `ExitCode` |
|---|---|---|---|---|---|
| `CodeInvalidDocument` | `invalid_document` | 400 | false | Invalid document | 1 |
| `CodeInvalidRequest` | `invalid_request` | 400 | false | Invalid request | 2 |
| `CodeUnauthenticated` | `unauthenticated` | 401 | false | Unauthenticated | 3 |
| `CodeNotGranted` | `not_granted` | 403 | false | Not granted | 4 |
| `CodeDenied` | `denied` | 403 | false | Denied | 4 |
| `CodeBudgetExceeded` | `budget_exceeded` | 403 | false | Budget exceeded | 4 |
| `CodeApprovalRequired` | `approval_required` | 403 | false | Approval required | 4 |
| `CodeConfirmationRequired` | `confirmation_required` | 428 | false | Confirmation required | 4 |
| `CodeNotFound` | `not_found` | 404 | false | Not found | 5 |
| `CodeNotInEnvironment` | `not_in_environment` | 404 | false | Not in this environment | 5 |
| `CodeIdempotencyConflict` | `idempotency_conflict` | 409 | false | Idempotency key reused | 1 |
| `CodeInProgress` | `in_progress` | 409 | true | In progress | 6 |
| `CodeResumeGap` | `resume_gap` | 409 | false | Cannot resume | 1 |
| `CodeRetired` | `retired` | 410 | false | Retired | 5 |
| `CodePreconditionFailed` | `precondition_failed` | 412 | false | Precondition failed | 1 |
| `CodeCriticalTraitUnsupported` | `critical_trait_unsupported` | 422 | false | Critical trait unsupported | 8 |
| `CodeResolutionFailed` | `resolution_failed` | 422 | false | Resolution failed | 8 |
| `CodeUpgradeRequired` | `upgrade_required` | 426 | false | Upgrade required | 8 |
| `CodeLimitExceeded` | `limit_exceeded` | 429 | true | Limit exceeded | 6 |
| `CodeOutputLimitExceeded` | `output_limit_exceeded` | 200 (travels only in an end line or final frame) | false | Output limit exceeded | 1 |
| `CodeInternal` | `internal` | 500 | false | Internal error | 1 |
| `CodeUpstreamFailed` | `upstream_failed` | 502 | true | Upstream failed | 1 |
| `CodeUnavailable` | `unavailable` | 503 | true | Unavailable | 6 |
| `CodeDeadlineExceeded` | `deadline_exceeded` | 504 | false | Deadline exceeded | 7 |

`ExitCode(code string) int` (L168-186): every code not named, a capability's own included, is 1 (§9.7.6).

Functions: `Lookup(code) (Registered, bool)` (L143); `Known(code) bool` (L150); `NewProblem(code, detail string) *Problem` (L158-164): an unregistered code becomes `internal`; it fills type, title, status, code, retryable, detail, and NOT `next` or `request_id`. `type Declared struct{ Code "code"; Status int "status"; Retryable bool "retryable"; Description "description"; Next "next" }` (L190-196); `(Declared) Problem(detail) *Problem` (L199-201, title = Description). `InvalidRequest(failures []schema.Failure) *Problem` (L241-255): `invalid_request`, target = first failure's pointer, detail `"<keyword>: <message>"` plus `", and N more"`, one `details` entry per failure. `ResolutionFailed(err error) *Problem` (L259-265): `resolution_failed`, target from `*resolve.Error`.

AMBIGUOUS (problem.schema.json requires `next` and `request_id`): `spec/protocol/references/schemas/problem.schema.json` L7-14 requires `type, status, code, retryable, next, request_id`; `Problem` has `omitempty` on both (problem.go L25, L29) and `NewProblem` fills neither. Verified: `json.Marshal(NewProblem(CodeNotFound,"x"))` gives `{"type":...,"title":"Not found","status":404,"detail":"x","code":"not_found","retryable":false}`. Readings: (a) the gateway must always set `Next` and `RequestID` before writing (kit/problem fills RequestID only); (b) the structs should drop `omitempty` on those two. frame.marshalEnd already refuses an end-line problem without them (frame.go L73-75).

#### Fingerprints and exact ids (`fingerprint.go`)

- `type FP struct{ Value string; SHA256 string; Canonical []byte }` (L13-17): Value = first 16 hex chars of SHA-256 of the RFC 8785 projection.
- `Fingerprint(profile *ijson.Object) (FP, error)` (L21-29).
- `CheckFingerprint(profile *ijson.Object) (FP, error)` (L34-45): recompute and compare with the carried `fingerprint`; mismatch is `invalid_document` with target `/fingerprint`.
- `Project(profile *ijson.Object) *ijson.Object` (L50-78): a copy without top-level `fingerprint`, `revision`, `usage`; without `identity.title`, `identity.summary`, `identity.owner`; without `errors[].description` and `errors[].next`; without `evolution.changelog`; every schema under `schemas` stripped by `StripSchema`.
- `StripSchema(v ijson.Value)` (L83-122): in place, at schema positions only, removes `title`, `description`, `$comment`, `examples`; recurses into `properties`, `$defs`, `items`, `additionalProperties`, `prefixItems`, `anyOf`, `oneOf`; removes `x-atlas-enum[].description` and `x-atlas-deprecated.reason`.
- The exact id is `grammar.Exact` (`id@vN#fingerprint`), from `profile.Profile.Exact()`.

OVERRIDDEN BY REVIEW (R01): the contract (by fingerprint) is cached apart from the document revision; describe's caller state is private and uncached. The projection already drops `revision`, `usage` and prose, but there is no revision type or header in the code (no `Atlas-*-Revision` for documents; only `Atlas-Governance-Revision`). AMBIGUOUS (R02 and `bindings`): `Project` keeps `interactions.*.bindings`, so routing is inside the fingerprint. R02 says routing is fetched as current metadata. Readings: (a) bindings stay in the contract and a binding change is a new fingerprint; (b) bindings leave the projection, which changes every vector fingerprint in `spec/protocol/references/vectors/profiles`. Not decided in code.

#### Headers (`headers.go`)

Header name constants (L17-45), exactly:

| Const | Header |
|---|---|
| `HeaderAuthorization` | `Authorization` |
| `HeaderClient` | `Atlas-Client` |
| `HeaderClientFeatures` | `Atlas-Client-Features` |
| `HeaderProfileExpect` | `Atlas-Profile-Expect` |
| `HeaderDeadline` | `Atlas-Deadline` |
| `HeaderIdempotencyKey` | `Idempotency-Key` |
| `HeaderIfMatch` / `HeaderIfNoneMatch` | `If-Match` / `If-None-Match` |
| `HeaderExecution` | `Atlas-Execution` |
| `HeaderInteractive` | `X-Atlas-Interactive` |
| `HeaderConfirm` | `Atlas-Confirm` |
| `HeaderTraceparent` / `HeaderTracestate` | `Traceparent` / `Tracestate` |
| `HeaderProfile` | `Atlas-Profile` |
| `HeaderGovernanceRevision` | `Atlas-Governance-Revision` |
| `HeaderLimitRemaining` | `Atlas-Limit-Remaining` |
| `HeaderErrorCode` | `Atlas-Error-Code` |
| `HeaderRequestID` | `X-Request-Id` |
| `HeaderDeprecation` / `HeaderSunset` | `Deprecation` / `Sunset` |
| `HeaderRetryAfter` | `Retry-After` |
| `HeaderOperationLocation` | `Operation-Location` |
| `HeaderCacheControl` `HeaderETag` `HeaderWWWAuthenticate` `HeaderAccept` `HeaderContentType` | `Cache-Control` `ETag` `WWW-Authenticate` `Accept` `Content-Type` |

`const NoStore = "no-store"` (L50): Cache-Control of handoff, connect, ticket, transfer and of everything that is not the bootstrap or a profile by exact id. `const Immutable = "max-age=31536000, immutable"` (L53). OVERRIDDEN BY REVIEW (R01): describe can no longer be immutable; only the contract by fingerprint.

Types and functions:

| API | Line | Semantics |
|---|---|---|
| `Field(h http.Header, name string) (string, bool)` | L57 | field lines joined with `", "` (RFC 9651 §4.2) |
| `type Client struct{ Name, Version, Protocol string }`; `(Client) Format() (string, error)`; `ParseClient(field) (Client, error)` | L66-107 | sf Dictionary `name`, `version`, `protocol` (each String; protocol a `protocol-ver`); any failure = header absent (Q-HDR-1) |
| `type Features struct{ Levels, Handoff, Lens, Deliveries, Encodings, Traits []string }` | L122-129 | Atlas-Client-Features members `levels`, `handoff`, `lens`, `deliveries`, `encodings`, `traits` (Inner Lists of Tokens) |
| `CoreFeatures() Features` | L132 | Levels `[core]`, Deliveries `[single, operation]`, Encodings `[json]` |
| `Has(level)` (core always true), `Decodes(format)`, `Applies(op)`, `Accepts(delivery)`, `Reads(encoding)`, `Understands(trait)` | L137-152 | membership |
| `(Features) Format()`; `ParseFeatures(field) (Features, error)` | L165-208 | empty lists omitted; unknown members ignored; a known member not an Inner List of Tokens = absent |
| `FormatExact(e grammar.Exact) (string, error)`; `ParseExactHeader(field) (grammar.Exact, error)` | L212-221 | Atlas-Profile / Atlas-Profile-Expect as an sf String |
| `FormatDeadline(t time.Time) string`; `ParseDeadline(field) (time.Time, error)` | L225-241 | sf Date, whole seconds, rounded down; parsed to UTC |
| `MaxIdempotencyKey` = 255; `CheckIdempotencyKey(k) error`; `FormatIdempotencyKey(k) (string, error)`; `ParseIdempotencyKey(field) (string, error)` | L244-265 | sf String; 1 to 255 of `A-Z a-z 0-9 - _ .` |
| `ParseExecution(field) (string, error)` | L268 | sf Token: `gateway`, `handoff` or `client` |
| `FormatBoolean(b) string`; `ParseBoolean(field) (bool, error)` | L281-297 | X-Atlas-Interactive (`?1`/`?0`) |
| `FormatConfirm(amount) (string, error)`; `ParseConfirm(field) (sf.Decimal, error)`; `ParseAmount(amount) (sf.Decimal, error)`; `FormatConfirmDecimal(d) (string, error)` | L301-335, L796 | amount: at most 12 integer and 3 fraction digits, compared in thousandths. R06 (step 3, not first-journey reads) |
| `type Limit struct{ Name string; Remaining int64 }`; `FormatLimits([]Limit)`; `ParseLimits(field)` | L338-370 | Atlas-Limit-Remaining, sf Dictionary of Integers; parameters ignored |
| `ParseGovernanceRevision(field)`, `FormatGovernanceRevision(rev)` | L373-376 | sf String |
| `FormatErrorCode(code)`, `ParseErrorCode(field)` | L379-393 | Atlas-Error-Code, a Token that is a `problem-code` |
| `FormatDeprecation(t)`, `ParseDeprecation(field)` | L396-399 | sf Date (RFC 9745) |
| `FormatSunset(t) string`; `ParseSunset(field, now)`; `ParseHTTPDate(s, now)` | L402-445 | HTTP-date `http.TimeFormat`; parse accepts IMF-fixdate, rfc850, asctime; weekday checked; rfc850 2-digit year > now+50 years is the century before |
| `ParseBearer(field) (string, error)` | L449 | scheme case-insensitive `Bearer`, one space, b64token |
| `ParseCacheControl(field) []string` | L468 | directives in order |
| `ParseIfMatch(field) ([]string, error)` | L479 | `*` or entity-tags |
| `MaxRetryAfter` = 86400; `FormatRetryAfter(seconds int) string`; `ParseRetryAfter(field) (time.Duration, error)` | L496-519 | delay-seconds only (an HTTP-date is refused); clamped to 86400 s |
| `ParseOperationLocation(field) (*url.URL, error)` | L522 | absolute `http` or `https` URL with a host |
| `StrongETag(opaque) string` | L534 | `"` + opaque + `"` |
| `IfMatch(field, current) bool` | L539 | strong comparison; `*` matches when current != "" |
| `IfNoneMatch(field, current) bool` | L555 | weak comparison, for 304 |
| `type Traceparent struct{ Version, TraceID, ParentID string; Flags byte }`; `ParseTraceparent(field)`; `(Traceparent) String()` | L569-601 | W3C; `String` always writes version `00` |
| `type Challenge struct{ Realm, Error, Claims string }`; `(Challenge) Format() string`; `ParseChallenge(field)` | L617-659 | `Bearer realm="atlas"`, `error="invalid_token"` on expiry, `claims` (base64url) |
| `ChooseMedia(accept string, servable []string) (string, bool)` | L739 | first listed type the binding serves; empty Accept or `*/*` gives the first servable; q values ignored; false = 406 |

Tests: `protocol_test.go` (ATLAS-PROTOCOL-001, ATLAS-PROTOCOL-005 at L13), `vectors_test.go` (`TestProblemVectors` against `vectors/problems.json`, `TestFingerprintVectors`, `TestCheckFingerprint`, `TestHeaderVectors`).

### 1.2 `sdk/go/protocol/grammar`  IN SCOPE

Purpose (L1-4): §9.1.3-9.1.5 grammars; case-sensitive; mismatch refused.

| API | Line | Rule |
|---|---|---|
| `MaxIDLength = 128`, `MaxAddressLength = 160` | L17-18 | limits |
| `type Pinned struct{ ID string; Version int }`, `String()` = `id@vN` | L22-27 | |
| `type Exact struct{ Pinned; Fingerprint string }`, `String()` = `id@vN#fp` | L30-35 | |
| `type Address struct{ ID string; Version int; Interaction string }`, `Pinned() (Pinned, bool)`, `String()` | L39-55 | Version 0 = bare id |
| `type Error struct{ Rule, Value, Reason string }` | L58 | |
| `CapabilityID(s) error` | L90 | `segment 1*("." segment)`, ≤128; segment = `a-z *(a-z / DIGIT / "-")`, no trailing `-`, no `--` |
| `Domain(id) string`; `Builtin(id) bool` (domain `atlas`) | L107, L113 | |
| `ParsePinned(s)`, `ParseExact(s)`, `Fingerprint(s) error` (16 lowercase hex) | L137, L153, L169 | |
| `ParseAddress(s) (Address, error)` | L183 | `(capability-id / pinned-id) ":" interaction`, ≤160 |
| `Interaction(s)`, `Segment(s)`, `Version(s)` (`v` nz-digits), `NzDigits(s)` | L208, L561, L569, L577 | |
| `ParseProtocol(s) (major, minor int, err)` | L216 | `atlas/` digits `.` digits |
| `PropertyName(s)`, `ProblemCode(s)` | L231, L234 | `a-z *(a-z / DIGIT / "_")` |
| `type Date struct{ Year int; Month time.Month; Day int }`, `ParseDate(s)` | L261, L270 | `YYYY-MM-DD`, real calendar date |
| `ParseDateTime(s) (time.Time, error)` | L291 | offset required (`Z` or `±hh:mm`), fraction 1-9 digits, second 60 refused |
| `FormatDateTime(t) string` | L346 | UTC, always 6 fraction digits, truncated (`2006-01-02T15:04:05.000000Z`, Q-TIME-1) |
| `ParseDuration(s)`, `FormatDuration(d)` | L355, L523 | `P[nD][T[nH][nM][n[.f]S]]`; `P1DT` accepted, `PT` refused (Q-GRAM-3); format writes `PT0S` for zero |
| `type Decimal struct{ Negative bool; Integer, Fraction string }`; `ParseDecimal(s)`; `Fits(p, s) bool`; `Unscaled(scale) (*big.Int, bool)`; `FormatDecimal(unscaled *big.Int, scale int) string` | L446-506 | `[-](0/nz-digits)[.1*DIGIT]`; `-0` refused; format writes exactly `scale` fraction digits |
| `ParseIntegerString(s) (*big.Int, error)` | L510 | `-0` allowed (Q-DEC-3) |
| `MaxIdempotencyKey = 255`; `IdempotencyKey(s)` | L585-600 | |
| `ProblemTypeBase`, `ProblemType(s)` | L603-613 | |
| `DataSchema(s)` (`urn:atlas:profile:<pinned>:<fp>`), `EventType(s)` (`com.jhg.fiquant.<id>.v<N>`) | L617-641 | DEFERRED (events) |
| `ResourceID`, `ResourceName`, `OperationName` (`operations/<id>`) | L645-686 | provisional (Q-GRAM-1) |
| `EntityTag(s)` | L690 | `[W/]"etagc*"` |

### 1.3 `sdk/go/protocol/sf` (RFC 9651)  IN SCOPE

Bare item Go types (L6-7, L19-30): `int64` Integer, `Decimal` (int64 in thousandths: 1.5 = 1500), `string` String, `Token`, `[]byte` Byte Sequence, `bool` Boolean, `Date` (int64 epoch seconds, written `@1793318400`), `DisplayString`.
Containers: `Param{Key; Value any}`, `Params` (+`Get`), `Item{Value any; Params}`, `InnerList{Items []Item; Params}`, `Member any`, `List []Member`, `DictMember{Key; Value Member}`, `Dictionary []DictMember` (+`Get`), `Error{Field; Offset; Reason}` (L33-97).
Parse: `ParseItem(field) (Item, error)` L126; `ParseList(field) (List, error)` L139; `ParseDictionary(field) (Dictionary, error)` L160 (a key twice keeps its first place and last value). Serialize: `(Item) Serialize()` L508, `(List) Serialize()` L517, `(Dictionary) Serialize()` L531 (a `true` Item writes the bare key).
Limits: integer at most 15 digits (L390, L690); decimal at most 12 integer digits and 3 fraction digits (L382, L409); byte-sequence padding synthesised, excess `=` refused (Q-SF-1, A-018, L458-465); display-string escapes `%` + two lowercase hex.

### 1.4 `sdk/go/protocol/ijson` (I-JSON, RFC 8785)  IN SCOPE

`Value = any` (nil, bool, `Number`, string, `[]Value`, `*Object`) (L12, L27).

| API | Line | Semantics |
|---|---|---|
| `type Number string`; `Float64()`, `Rat()`, `IsInteger()`, `Int64() (int64, bool)`, `SafeInteger() bool` | L30-71 | exact text kept; integer = binary64 value with no fraction; safe = within ±(2^53−1) |
| `MaxSafeInteger = 1<<53 - 1` | L64 | |
| `type Object`; `NewObject()`, `Len()`, `Get(name)`, `Set(name, v)` (append or replace in place), `Delete(name) bool`, `Names() []string` (copy), `Each(fn func(name, v) bool)` | L74-124 | order preserved |
| `Clone(v) Value` | L127 | deep copy |
| `Equal(a, b) bool` | L150 | numbers by binary64 value; objects order-insensitive |
| `type SyntaxError struct{ Offset int; Reason string }` | L196 | |
| `type Limits struct{ MaxDepth int /*0 = 128*/; MaxBytes int /*0 = none*/ }` | L206-209 | |
| `Parse(b []byte) (Value, error)`; `ParseLimits(b, Limits)` | L212-236 | refuses duplicate names, invalid UTF-8, unpaired surrogates, noncharacters, BOM, numbers beyond binary64 (exponent > 4 digits refused, L412), trailing data |
| `Marshal(v) ([]byte, error)` | write.go L15 | wire form: compact, member order kept, numbers as written |
| `Canonical(v) ([]byte, error)` | write.go L27 | RFC 8785: names sorted by UTF-16 units, ECMAScript numbers |
| `ECMAScriptNumber(f float64) (string, error)` | write.go L143 | Number::toString; NaN/Inf refused; −0 is "0" |
| `FromGo(x any) (Value, error)` | write.go L185 | `json.Marshal` then `Parse` (struct field order kept) |

There is no Value-to-Go-struct decoder; the code does `ijson.Marshal` then `json.Unmarshal` (frame.go L167-173).

### 1.5 `sdk/go/protocol/schema` (Atlas Schema profile of JSON Schema 2020-12)  IN SCOPE

Constants: `Kind` = `Open` (0) or `Request` (1) (L25-31; a request schema is closed and accepts no UNKNOWN; every other is open and its string enums carry UNKNOWN). `MaxDepth = 32`, `MaxSize = 1 << 20` (bytes of RFC 8785 form, Q-PROF-12) (L35-36). `Dialect = "https://json-schema.org/draft/2020-12/schema"` (L40). `Vocabulary = "https://atlas.jhg.com/vocab/2026-10"` (L43). `$vocabulary` must equal exactly (L46-53): `.../vocab/core`, `applicator`, `validation`, `meta-data`, `format-assertion` all `true`, and the Atlas vocabulary `false`.

Keywords allowed (L56-67): `$schema $vocabulary $id $defs $ref $comment properties additionalProperties items prefixItems anyOf oneOf type enum const required minimum maximum exclusiveMinimum exclusiveMaximum multipleOf minLength maxLength pattern minItems maxItems uniqueItems minProperties maxProperties title description default deprecated examples readOnly writeOnly format x-atlas-logical x-atlas-key x-atlas-aliases x-atlas-reference x-atlas-deprecated x-atlas-unit x-atlas-enum`.
Siblings of `$ref` (L71-75, A-012): `$ref $comment title description default deprecated examples readOnly writeOnly x-atlas-key x-atlas-aliases x-atlas-reference x-atlas-deprecated`.
`$ref` must be `#/$defs/<name>` (L333-351); recursion refused (Q-PROF-1, L1100-1134). Root-only: `$schema $vocabulary $id $defs`. `type`: one type, or `[T, "null"]` (L355-399). Every schema has a type unless `$ref`, `anyOf` or `oneOf`. `oneOf` needs a discriminator: a required property with a distinct string `const` in every branch (L685-704). `multipleOf` is a positive integer on an integer (Q-PROF-8). Boolean schema only as `items` or `additionalProperties`. Closedness (L1079-1095): request objects `"additionalProperties": false`; other schemas open (absent or `true`); a map (no properties, schema-valued additionalProperties) exempt; `Options.FreeForm` pointers exempt. `x-atlas-key` refused in a request; a key property is required and non-nullable unless its logical type is an identifier (L723-734, A-011). String enums in open schemas must contain `UNKNOWN` and carry `x-atlas-enum` describing every value; request enums must not contain `UNKNOWN` (L991-1074). `x-atlas-enum` entries: `value`, `description`, `aliases`, `deprecated`. `x-atlas-deprecated`: null, or all of `reason`, `since` (date), `sunset` (date), `replaced_by` (property name or null) (Q-PROF-11). Defaults are validated against their own schema (L1138).

Formats (formats.go L14-18), each an assertion: `date`, `date-time`, `duration`, `decimal`, `int64`, `uri`, `uuid` (lowercase 8-4-4-4-12), `capability-id`, `pinned-id`, `exact-id`, `address`.
Logical types (logical.go L33-50): `Bp` `Percent` `Fraction` `Years` (number); `Decimal(p,s)` (string, format decimal, p 1-38, s 0-p, no spaces); `Money` (object: `amount` Decimal(38,6) and `currency` Currency, both required, nothing else, amount first); `Currency` `^[A-Z]{3}$`; `Country` `^[A-Z]{2}$`; `Isin` `^[A-Z]{2}[A-Z0-9]{9}[0-9]$`; `Cusip` `^[A-Z0-9]{8}[0-9]$`; `Sedol` `^[B-DF-HJ-NP-TV-Z0-9]{6}[0-9]$`; `Figi` `^BBG[B-DF-HJ-NP-TV-Z0-9]{8}[0-9]$`; `Lei` `^[A-Z0-9]{18}[0-9]{2}$`; `Tenor` `^[1-9][0-9]*[DWMY]$`; `PortfolioCode` `IndexCode` `Path` (minLength 1); `Principal` (format uuid); `Uri` (format uri). Identifiers (may be a null key): Isin, Cusip, Sedol, Figi, Lei. `MaxDecimalPrecision = 38` (L53).
Patterns (iregexp.go): `^R$` with R an I-Regexp (RFC 9485); quantifier counts ≤ `maxRepeat = 1000` (A-013); `\p{C}` and `\p{Cn}` refused.

API:

| API | File:Line | Semantics |
|---|---|---|
| `type Violation struct{ Pointer "pointer"; Rule "rule"; Message "message" }`; `type Error struct{ Violations []Violation }` | schema.go L80-102 | sorted by pointer |
| `type Options struct{ FreeForm []string; Unchecked bool }` | L166-174 | FreeForm: atlas.compare's request, atlas.settings' settings (Q-PROF-9) |
| `Compile(v ijson.Value, kind Kind) (*Schema, error)`; `CompileWith(v, kind, Options)`; `MustCompile(v, kind)` | L204-265 | |
| `type Failure struct{ Pointer "pointer"; Keyword "keyword"; Message "message" }` | validate.go L19 | sorted by pointer then keyword (A-009) |
| `(*Schema) Validate(v) []Failure`; `Valid(v) bool` | validate.go L26, L39 | |
| `Pointer()`, `Source()`, `Definition()`, `Type()` (without null; "" for a union), `Nullable()`, `Union()`, `Properties()`, `Property(name)`, `Required(name)`, `Items()`, `Default()`, `Aliases()`, `Enum()`, `EnumHas(v)`, `EnumAlias(spelling)`, `Format()`, `Logical() (name, precision, scale, ok)`, `Unit() (units.Unit, bool)`, `Key()`, `Deprecated()`, `Reference()` | access.go L40-153 | follow the `$ref` chain to its end |
| `(*Schema) Project(columns []string) *Schema` | access.go L158 | keeps named properties in `properties` and `required` |
| `type Keyword struct{ Name string; Value ijson.Value }`; `AtlasKeywords() []Keyword` | access.go L177-202 | the `x-atlas-*` keywords, own first, then along the chain |
| `SameValues(a, b *Schema) bool` | same.go L22 | same validating keywords, annotations ignored; any `$ref` gives false. Annotations (L12-16) include `x-atlas-unit`, so a unit change is not seen by it |

Tests: ATLAS-TYPES-001 (schema_test.go L65); vectors `TestSchemaVectors`; fuzzers `FuzzCompileAndValidate`, `FuzzPattern`.

### 1.6 `sdk/go/protocol/units`  IN SCOPE (aliases only)

`Unit` consts (L17-26): `bp`, `percent`, `fraction`, `years`, `days`, `seconds`, `count`, `currency_unit`. `Dimension` (L29-37): `ratio` (bp, percent, fraction), `time` (years, days, seconds), `none` (count), `money` (currency_unit). `All` (L46). `Parse(s)` L49; `DimensionOf(u)` L57; `OfLogical(name)` L61 (Bp, Percent, Fraction, Years imply theirs). Conversions (L81-90), the only eight: fraction→percent ×100, percent→fraction ÷100, fraction→bp ×10000, bp→fraction ÷10000, percent→bp ×100, bp→percent ÷100, days→seconds ×86400, seconds→days ÷86400. `Convertible(from, to)` L94; `Factor(from, to) (num, den int64, ok)` L101; `Float(v, from, to)` L117 (one operation, rounded to nearest); `Decimal(v string, from, to, scale)` L133 (exact, half-even); `RoundHalfEven(r *big.Rat, scale) *big.Int` L148.
OVERRIDDEN BY REVIEW (R03): a unit change within a version is a new major; conversion survives only in the experimental resolver.

### 1.7 `sdk/go/protocol/arrowschema` (§9.2.7)  IN SCOPE (Arrow is an optimisation, R11)

Constants: `MetaProfile = "atlas:profile"` (exact-id), `MetaProtocol = "atlas:protocol"`, `MetaEnd = "atlas:end"` (L16-20). Types: `Timestamp` = `timestamp[us, tz=UTC]`, `Duration` = `duration[us]`, `Money` = struct `amount` Decimal128(38,6), `currency` Utf8 (L23-30). `type Error struct{ Pointer; Message }` (invalid_document, Q-ARROW-3) L34.

| API | Line | Semantics |
|---|---|---|
| `Derive(table *schema.Schema, profile, protocol string) (*arrow.Schema, error)` | L47 | table = array of row objects with properties; schema metadata `atlas:profile`, `atlas:protocol` |
| `Fields(row *schema.Schema) ([]arrow.Field, error)` | L66 | nullable = property nullable or not required; field metadata `atlas:<keyword without x-atlas->` = RFC 8785 JSON (A-019, Q-ARROW-4) |
| `Type(s *schema.Schema) (arrow.DataType, error)` | L105 | Decimal(p,s)→Decimal128(p,s); Money→`Money`; boolean→Boolean; integer→Int64; number→Float64; string by format: date→Date32, date-time→Timestamp, duration→Duration, int64→Int64, else Utf8; array→List(item); object with properties→Struct; union or map → error (A-017, Q-ARROW-2) |
| `ToBatch(mem, sch *arrow.Schema, row *schema.Schema, rows []*ijson.Object) (arrow.RecordBatch, error)` | rows.go L25 | rows to one batch; caller releases |
| `Append(b array.Builder, t arrow.DataType, s *schema.Schema, v ijson.Value) error` | rows.go L45 | one JSON value; absent/null → Arrow null |
| `ToRows(batch, row *schema.Schema) ([]*ijson.Object, error)` | rows.go L158 | |
| `Value(a arrow.Array, i int, s *schema.Schema) (ijson.Value, bool, error)` | rows.go L184 | an Arrow null reads as null where nullable, absent otherwise (A-021); int64-format Int64 reads as a string |

Tests: AP-SCHEMA-002 (vectors_test.go L16; arrowschema_test.go L46 with ATLAS-TYPES-002), AP-SCHEMA-003 (L114).

### 1.8 `sdk/go/protocol/resolve` (§9.5)  OVERRIDDEN BY REVIEW (R03, R04): experimental

Package doc (resolve.go L8-13): clients are obliged only to aliases, defaults, UNKNOWN and projection; the gate refuses `convert_unit`, `promote` and `drop` within a version.

| API | Line | Semantics |
|---|---|---|
| `type Error struct{ Target, Message string }` | L28 | resolution_failed |
| `type Profile struct{ Exact grammar.Exact; Lens Lens; Schema *schema.Schema }` | L47 | one link of a chain |
| `type Options struct{ Columns []string; Dynamic bool }` | L70 | Columns: projection; Dynamic: trait DynamicSchema, no resolution |
| `New(chain []Profile, writer, reader grammar.Exact, opt Options) (*Plan, error)` | L83 | same exact or Dynamic → identity plan; different pinned ids refused ("only the gateway resolves across versions"); forward if writer older, backward if newer |
| `(*Plan) Identity() bool`, `Reader() *schema.Schema`, `Record(x *ijson.Object) (*ijson.Object, error)` | L136-159 | steps 3-15: lens steps, then match: name, last present alias, default, null if nullable and required, drop if optional, else fail; extra members dropped |
| `(*Plan) Batch(mem, b arrow.RecordBatch, md *arrow.Metadata) (arrow.RecordBatch, error)` | columns.go L26 | column-wise; equals row-wise (AP-RESOLVE-002) |
| `(Lens) Fields() []string` | L276 | |
| `type Lens []Op`; `type Op struct{ Op, Field string; From, To, Value ijson.Value }`; `ParseLens(v) (Lens, error)` | lens.go L15-64 | members per op: rename `from,to`; alias_value/convert_unit/promote `field,from,to`; default `field,value`; drop `field`. Promotions: integer→number, date→date-time (L43) |

OVERRIDDEN BY REVIEW (R04): "unknown request fields are refused, never dropped": `Record`'s match drops members no property takes (L206), so the gateway must validate a request against its own closed request schema before any resolution. Note that `New` returns an identity plan when writer == reader even with `Columns` set (L84), so projection is not applied in that case (section 5).

### 1.9 `sdk/go/protocol/frame` (§9.6.5)  IN SCOPE (NDJSON; Arrow optional)

`type End struct{ Rows int64; Truncated bool; Usage json.RawMessage; Problem *protocol.Problem }` (L29-34). End line members required (L37): `$atlas` (= `"end"`), `rows`, `truncated`, `usage` (object; nil written `{}`), `problem` (null or a problem). The end-line problem must have `type, status, code, retryable, next, request_id` (L40, L144-176); `marshalEnd` refuses one missing `next` or `request_id` (L73-75). `MaxLine = 16 << 20` (L43). An answer without its end is `upstream_failed` ("the answer is incomplete: ..."); a mis-framed one is `invalid_document` (L45-51).

| API | Line | Semantics |
|---|---|---|
| `NewNDJSONWriter(w io.Writer) *NDJSONWriter`; `Row(v ijson.Value) error`; `Flush() error`; `End(e End) error` | L179-222 | rows counted by the writer; End writes the line and flushes; second End refused |
| `NewNDJSONReader(r io.Reader) *NDJSONReader`; `Next() (*ijson.Object, error)`; `End() *End` | L225-318 | io.EOF after a good end; the end's problem as the error after a failed one; blank lines, data after the end, row-count mismatch: invalid_document; EOF without end: upstream_failed |
| `NewArrowWriter(w, schema *arrow.Schema, mem) (*ArrowWriter, error)`; `Write(b arrow.RecordBatch) error`; `End(e End) error` | L322-374 | schema must carry `atlas:profile` and `atlas:protocol`; End writes a zero-row batch with batch metadata `atlas:end` = end-line JSON, then EOS |
| `NewArrowReader(r, mem) (*ArrowReader, error)`; `Schema()`; `Next() (arrow.RecordBatch, error)` (caller releases); `End()`; `Release()` | L377-460 | same rules as NDJSON |

Tests: AP-HTTP-002 (frame_test.go L96, L195).

### 1.10 `sdk/go/protocol/profile` (§3.1)  IN SCOPE

`type Profile struct` (L21-41): `Doc *ijson.Object; ID string; Version int; FP protocol.FP; Family, Stability, Lifecycle, Visibility string; Traits []Trait; Interactions map[string]*Interaction; Order []string; Schemas map[string]*schema.Schema; Kinds map[string]schema.Kind; Errors []protocol.Declared; Meaning *ijson.Object; Compatibility map[string]string; Usage *ijson.Object; Evolution Evolution`.
`type Trait struct{ Name string; Critical bool }` L45. `type Interaction struct{ Name, Effect, Result, Delivery string; Execution []string; Takes, Returns string; Deadline Deadline; Bindings []Binding; Encodings []string }` L51-62. `type Deadline struct{ Default, Max time.Duration }` L65. `type Binding struct{ Protocol, Server, Method, Address, Auth string; Encodings []string }` L68-71. `type Evolution struct{ PreviousProfile *grammar.Exact; PreviousVersion string; Lens resolve.Lens; Changelog []ijson.Value }` L74-79.
`(*Profile) Exact() grammar.Exact` L82; `HasTrait(name) bool` L87; `type Error struct{ Pointer, Message }` L92.
`Parse(doc *ijson.Object) (*Profile, error)` (L169-321): required `identity` (`id`, `version` ≥1), `properties` (`family` required; `stability`, `lifecycle`, `visibility`, `traits[{name, critical}]` optional), `interactions` (each: `effect`, `delivery` required; `result`, `execution`, `takes`, `returns`, `encodings`, `deadline{default,max}` (ISO durations; default ≤ max), `bindings[{protocol, server, address required; method, auth, encodings}]`), `schemas` (compiled `Request` where an interaction `takes` them, else `Open`); optional `errors[{code, status 400-599, retryable, description, next}]` (a registry code is refused), `meaning`, `compatibility`, `usage`, `evolution{previous_profile, previous_version, lens, changelog}`. Returns only the first error. Computes FP; does not check the carried fingerprint (use `protocol.CheckFingerprint`).

### 1.11 `sdk/go/protocol/gate` (§3.3.12)  IN SCOPE  OVERRIDDEN BY REVIEW (R03): smaller model already applied

| API | Line | Semantics |
|---|---|---|
| `type Class int`: `Safe`, `Dangerous`, `Breaking`; `String()` | gate.go L25-34 | |
| `type Finding struct{ Class Class; Pointer, Rule string }` | L38 | |
| `type Report struct{ Findings []Finding }`; `Refused() bool` (any Breaking); `Worst() Class`; `Reason() string` | L47-75 | |
| `MinimumSunset = 90 * 24 * time.Hour` | L79 | REVIEW.md "Left to owners": the minimum sunset is still open |
| `type Catalogue interface{ Has(p grammar.Pinned) bool }` | L83 | `registry.Snapshot` implements it |
| `Check(p, previous *profile.Profile, cat Catalogue) Report` | L93-120 | Lint; if no previous and version > 1 requires `evolution.previous_version`; previous must be same id@vN; same FP = prose only, nothing classified; else Classify, require `evolution.previous_profile` = previous exact, require a new changelog entry for any Dangerous, lensConsistent |
| `Lint(p, cat) []Finding` | lint.go L26 | built-ins not linted (A-016). All findings Breaking: `identity.domain` = first segment; `errors` present (ATLAS-PROFILE-005); ≥1 interaction; `deadline.max` set; every address begins `/`; an `https` binding has method `POST`; an `https` binding exists unless delivery `transfer`; compatibility mode no looser than default (request `BACKWARD_TRANSITIVE`, others `FULL_TRANSITIVE`; modes L16); property names matching `^(path|file|filename|dir|directory|folder)$|_(path|file|filename|dir|directory|folder)$` must be logical `Path` or `Uri` (ATLAS-CLIENTS-006); `x-atlas-reference` must be served (when cat != nil); deprecation sunset ≥ 90 days; resource family get/create/update records declare `name` and `metadata`; a deprecated lifecycle needs `properties.deprecated` and `properties.sunset` ≥ 90 days apart |
| `Ready(p) []string` | ready.go L16 | agent-readiness (§3.1.4a, ATLAS-VISION-002): `identity.summary` one sentence ≤200 chars; every property has `description`; every number/integer has `x-atlas-logical` or `x-atlas-unit`; `usage.when`; a `usage.examples[].request` valid against its interaction's request schema (`interaction` member, or `fetch`, or the first that takes one); each declared error has `description` and `next`; meaning members by family: data `grain`, `source`, plus `keys` if any interaction's result is `table`, plus `as_of` with trait `AsOf`; event `emitted subject ordering retention history`; action `effects policy` |
| `Classify(old, new *profile.Profile) []Finding` | classify.go L24 | rules below |

Classify rules (classify.go): family change Breaking; lifecycle only preview→stable (safe) or stable→deprecated (Dangerous); visibility change Dangerous except confidential→listed; trait removed Breaking (except Handoff, Brokerable); trait becomes or is added critical Dangerous; interaction removed, effect/result/delivery changed Breaking; deadline max or default lowered Dangerous; error added Dangerous; error status/retryable changed Breaking. Schemas compared per interaction role: `takes` as request (BACKWARD), `returns` as result/resource/payload (FULL). Request: new required property Breaking; property removed Breaking; enum value removed Breaking unless now an alias; Decimal narrowed Breaking; bounds may only relax. Result: new required field needs a missing value (optional, default or nullable) or Breaking (R03); removed field Breaking (R03); field made non-null Breaking; field made nullable Breaking; promotion (integer→number, date→date-time) Breaking (R03); unit changed Breaking (R03); default changed or removed Breaking; Decimal(p,s) changed Breaking; key changed Breaking; any bound change Breaking; enum value added Dangerous (UNKNOWN); enum value removed Dangerous (or alias); rename Dangerous with aliases, and at top level of a result also needs a `rename` lens else Breaking; alias removed Breaking; `prefixItems`/`anyOf`/`oneOf` changes Breaking. Meaning: any change Breaking except event `retention` lengthened (safe) or shortened (Dangerous) and action `policy` (Dangerous). Lens: `drop`, `convert_unit`, `promote` Breaking (R03); `rename` must map existing fields; `default` on a new field with a valid value; `alias_value` on a field both have.
AMBIGUOUS (R03 vs enum removal): R03 says "a removal ... is a new major version"; classify.go L402 classes a result enum value removed as Dangerous. Readings: (a) R03's removal means fields only (current code); (b) it covers enum values too.

Tests: ATLAS-VERSION-007 (gate_test.go L129), ATLAS-VERSION-001 (L140), ATLAS-PROFILE-005 and ATLAS-CLIENTS-006 (L203), ATLAS-VISION-002 (L230), the review's R03 counterexamples (L249-296); vector profiles of all four families lint and are ready (vectors_test.go L17).

### 1.12 `sdk/go/kit` — shared gateway mechanisms (MIT notices retained)

Historical integration finding: `middleware.Deadline` turns a past client deadline into the route's maximum and reads the wall clock; to be fixed in step 3 with R17. Still to import (L35-44): auth middleware, idempotency, lro, ratelimit, resilience (retry), reconcile, registry, resource. AGENTS.md L46 names `kit/retry`, which does not exist yet.

| Package | API (file:line) | Semantics |
|---|---|---|
| `clock` | `type Clock interface{ Now() time.Time }` L14; `var System Clock` L19; `type Func func() time.Time` L26; `type Manual` L33, `NewManual(t)` L39, `Now()`, `Set(t)` L49, `Advance(d) time.Time` L56 | Atlas's own; timers use stdlib under `testing/synctest` |
| `conc` | `type PanicError struct{ Value any; Stack []byte }` L16 (+`Unwrap`); `Go(fn func() error) (wait func() error)` L42; `type Group` L59; `WithContext(ctx) (*Group, context.Context)` L69; `(*Group) SetLimit(n)` L77 (n<0 removes; panics if running); `Go(fn)` L90 (blocks for a slot); `TryGo(fn) bool` L99; `Wait() error` L134 | ATLAS-MECH-012: the only way to start a goroutine outside the kit |
| `health` | `type Check interface{ Name() string; Check(ctx) error }` L33; `NamedCheck(name, fn)` L47; `Ping` L52; `ReadinessCheck(r *lifecycle.Readiness)` L56; `WithTimeout(c, d)` L66; `Handler(name string, log *slog.Logger, checks ...Check) http.Handler` L83 | `/livez`, `/readyz`; body `ok`; `?verbose`; `?exclude=name`; 503 with `[-]<name> failed: reason withheld`; `Cache-Control: no-store` |
| `hot` | `type Hot[T]` L37; `New[T](initial, validate)` L45; `Load() T` L59; `CurrentVersion() string` L62; `Swap(next) (changed bool, err error)` L67; `OnChange(fn)` L89; `Version(v any) string` L97 (sha256 of JSON, 32 hex); `ErrPreconditionFailed` L107; `CheckIfMatch(ifMatch, current) error` L112; `Watch[T](ctx, h, every, load, log)` L126 | immutable config behind an atomic pointer |
| `httpserver` | `type Config struct{ Addr; ReadHeaderTimeout; ReadTimeout; WriteTimeout; IdleTimeout; MaxHeaderBytes; EnableH2C; ShutdownDelay; GracePeriod }` L33; `Defaults()` L61: `:8080`, 10s, 0, 0, 5m, 16 KiB, h2c true, 5s, 30s; `New(ctx, cfg, h, log) *http.Server` L77; `Run(ctx, srv, cfg, ready, log) error` L109 | drain: readiness off, sleep ShutdownDelay, Shutdown(grace), Close |
| `jwt` | `type JWK` L43, `(JWK) PublicKey()` L56 (RSA ≥2048 bits; EC P-256/384/521 on-curve via crypto/ecdh); `type KeyProvider interface{ Key(ctx, kid) (crypto.PublicKey, error) }` L118; `ErrUnknownKey` L123; `type KeySet` L126 (`URL`, `Client`, `MinRefreshInterval`, `MaxAge`); `NewKeySet(url)` L147 (5s client, 1m min refresh, 24h max age); `Key(ctx, kid)` L160; `Refresh(ctx)` L185; `StaticKeys` L280; `type Header` (jws.go L23); `ErrMalformedToken` L39; algorithms RS256/384/512, PS256/384/512, ES256/384/512 (L77-87); `type Audience []string` (verifier.go L18); `type Claims` L40-56 (`iss sub aud exp nbf iat jti scp scope roles tid oid preferred_username`, `Raw`); `Scopes()` L59; errors `ErrInvalidToken ErrExpired ErrNotYetValid ErrIssuer ErrAudience ErrAlgorithm ErrSignature` L72-80; `type Verifier struct{ Issuer; Audiences; Keys; Algorithms (default RS256); Leeway (default 5m, nbf only); MaxTokenAge; Now }` L83-98; `Verify(ctx, token) (*Claims, error)` L101 | no `crit`; `kid` required; exp required |
| `lifecycle` | `type Group` L33, `Add(execute, interrupt)` L40, `Run() error` L46; `type SignalError` L66; `IsSignal(err)` L71; `SignalActor(ctx, signals...)` L78 (default SIGINT, SIGTERM); `ContextActor(parent, run)` L101; `type Readiness` L110 (`Set`, `Ready`) | oklog/run style |
| `middleware` | `type Middleware func(http.Handler) http.Handler` L37; `Chain(h, mws...)` L41 (first is outermost); `type ResponseWriter` L51, `Wrap(w)` L59, `Status()`, `BytesWritten()`, `Written()`, `Unwrap()`, `Flush()`, `Hijack()`; `Recover(log)` L118 (500 `internal` problem); `RequestID(header)` L151 (default `X-Request-Id`; reuse if 1-128 of `0-9a-zA-Z-_.:`, else 32 hex); `RequestIDFrom(ctx)` L187; `RealIP(trusted []netip.Prefix, preferredHeader)` L200; `ClientIPFrom(ctx)` L273; `MaxBytes(n int64)` L281; `Deadline(header string, def, max time.Duration)` L304 (504 `deadline_exceeded` if unwritten at deadline) | order L12-13: Recover → RequestID → RealIP → AccessLog → Tracing → MaxBytes → Deadline → Authn → Admission → RateLimit → Authz. AGENTS.md L14: authentication, limits and idempotency are pipeline steps, not middleware |
| `problem` | `StatusClientClosedRequest = 499` L19; `From(err) *protocol.Problem` L24 (`*protocol.Problem` passes; `context.DeadlineExceeded` → `deadline_exceeded`; `*http.MaxBytesError` → `invalid_request` "the request body is larger than N bytes"; else `internal`); `Write(w, requestID string, err error)` L44 | sets `Content-Type: application/problem+json`, `X-Content-Type-Options: nosniff`, `Atlas-Error-Code`, `Retry-After` when > 0; fills `request_id` if empty |
| `telemetry` | `AccessLogOptions{ Skip; LogQuery }` L16; `AccessLog(log, opts)` L33; `WithRoute(r, pattern)` L90; `RouteFrom(ctx)` L100; `NewLogger(w, level, service, version, env)` (logging.go L26; `ts` RFC3339Nano UTC; durations as strings); `WithLogger`, `FromContext`, `With` L46-63; `type Recorder interface{ ObserveRequest(method, route string, status int, d time.Duration, reqBytes, respBytes int64); InFlight(delta int) }` (route.go L32); `NoopRecorder` L38; `RequestMetrics(rec)` L46 (label route, `r.Pattern`, or `unmatched`); `type TraceParent struct{ TraceID [16]byte; SpanID [8]byte; Sampled bool }` (traceparent.go L22); `ParseTraceParent`, `NewTraceParent`, `Child`, `String`, `TraceIDString`, `TraceFrom`, `Propagate(trustIncoming bool)` (sets `X-Trace-Id`), `Inject(ctx, h)` L32-123 | |

### 1.13 `sdk/go/snowarrow`  DEFERRED (handoff, R10) except as the fake Snowflake's codec

`Format = "snowflake-arrow/1"` (L39); Snowflake types `fixed real text boolean date timestamp_ntz timestamp_ltz timestamp_tz` (L42-51); `type Column struct{ Name "name"; Type "type"; Precision "precision,omitempty"; Scale "scale"; Nullable "nullable" }` L55; `type RowType []Column` L64; `MarshalSource() json.RawMessage` (`{"row_type":[...]}`) L67; `ParseSource(raw)` L75; `WriteChunk(rec) ([]byte, error)` L89; `ReadChunk(mem, r) ([]arrow.RecordBatch, error)` L102; `Decode(mem, rt RowType, rec, target *arrow.Schema) (arrow.RecordBatch, error)` (decode.go L19); `Encode(mem, rt, rows [][]any) (arrow.RecordBatch, error)` (encode.go L22; test and fake use). Every encoding fact is marked ASSUMPTION, to be checked in Proof 1 (L6-24).

### 1.14 `sdk/go/internal/refs`

`Dir(t testing.TB) string` (L15): walks up to `spec/protocol/references`, fails (never skips) without it. `Read(t, rel) []byte` (L35). The gateway has its own copy, `gateway/internal/refs` (`Dir`, `Read`).

---

## 2. sqlglot (`sqlglot/`, module `atlas.local/sqlglot`)  IN SCOPE (REVIEW.md step 4; acceptance result 1)

The in-repository Go SQL library implements SQLGlot v30.27.0 concepts, with MIT notices retained in its module. The reviewed implementation contained 207 Go files and 77,377 non-test lines; supported dialects were Snowflake, DuckDB and the base dialect. It used only the standard library and required Go ≥1.23. Consult `sqlglot/PROVENANCE.md` and the current module for integration details.
Integration rules (`kit/PROVENANCE.md` L52-53, from INTEGRATIONS): full-statement parsing (never `ParseOne` on text then executed); exactly one executable statement; refuse `exp.Command` and unknown nodes; unsupported transformations fail before any driver call; qualification is not authorisation; bounded input size, AST depth and compilation concurrency (parsing takes no context). The optimiser and executor stay out of production.

Packages: root `sqlglot` (parser, generator, dialect machinery, optimizer, schema, lineage, diff, transforms), `exp` (AST, kinds, builders), `tokens`, `dialects` (+ `dialects/snowflake`, `dialects/duckdb`), `errs`, `helper`, `trie`, `executor`, `cmd/sqlglot`, `internal/*`.

Dialects register in `init` (`dialects/snowflake/snowflake.go` L14, `dialects/duckdb/duckdb.go` L14). Import `_ "atlas.local/sqlglot/dialects"` (dialects.go). Without it only the base dialect `""` exists. The root package installs the AST backend in its own `init` (api.go L15).

### 2.1 Parsing

| API | Line | Notes |
|---|---|---|
| `Parse(sql string, dialect any) ([]*exp.Expr, error)` | api.go L105 | one tree per statement; a nil tree for an empty statement |
| `ParseOpts(sql, dialect, ParserOptions)` | api.go L110 | |
| `ParseOne(sql, dialect, into ...exp.Kind) (*exp.Expr, error)`; `MustParseOne`; `ParseOneOpts`; `ParseOneInto` | api.go L120-157 | first statement only: do not use on text that is executed |
| `Tokenize(sql, dialect) ([]*tokens.Token, error)` | api.go L95 | |
| `type ParserOptions struct{ ErrorLevel errs.ErrorLevel; ErrorMessageContext int /*0=100*/; MaxErrors int /*0=3*/; MaxNodes int /*0=unlimited*/ }` | dialect.go L865-871 | `errs.Unset` → `errs.Immediate` (parser_core.go L49-51). `MaxNodes` bounds the AST; the adapter does not use it |
| `GetDialect(d any) (*Dialect, error)` | dialect.go L612 | nil/"" base; `"snowflake"`; `"duckdb, normalization_strategy = lowercase"`; `*Dialect`; `*DialectClass` |
| `(*Dialect) Parse(sql, opts)`, `ParseInto`, `Tokenize`, `Generate(e, opts)`, `Transpile(sql, opts)` | dialect.go L860-922 | |
| `errs.ErrorLevel`: `Warn` (generator default), `Ignore`, `Raise` (collect, one error), `Immediate`; `Unset = -1` | errs/errs.go L12-26 | errors: `*errs.ParseError`, `*errs.TokenError`, `*errs.UnsupportedError`, `*errs.OptimizeError`, `*errs.SchemaError` |

```go
import (
    sqlglot "atlas.local/sqlglot"
    _ "atlas.local/sqlglot/dialects"
    "atlas.local/sqlglot/errs"
)
trees, err := sqlglot.ParseOpts(text, "snowflake", sqlglot.ParserOptions{ErrorLevel: errs.Immediate, MaxNodes: 10000})
if err != nil || len(trees) != 1 || trees[0] == nil { /* refuse */ }
stmt := trees[0]
```

### 2.2 Walking and building ASTs (`exp`)

Nodes are `*exp.Expr` tagged with a `Kind`. Inspection: `Kind()`, `Key()`, `ClassName()`, `Is(kind)` (follows SQLGlot's class hierarchy, e.g. `Is(exp.Func)`), `IsAny(kinds...)`, `IsKind(kind)` (exact), `Arg(key) any`, `Get(key) *Expr`, `List(key)`, `Str(key)`, `Bool(key)`, `This()`, `Expression()`, `Expressions()`, `Parent()`, `Name()`, `ThisStr()`, `Alias()`, `AliasOrName()`, `OutputName()`, `Selects()`, `NamedSelects()` (exp/expr.go L112-258, exp/props.go). Traversal: `DFS(prune)`, `BFS(prune)`, `Walk(bfs bool, prune func(*Expr) bool) iter.Seq[*Expr]`, `FindAll(kinds...) iter.Seq`, `FindAllList`, `Find`, `FindDFS`, `FindAncestor`, `ParentSelect`, `Root`, `Depth()` (expr.go L386-545). Mutation: `Set(key, v)`, `Append`, `Replace(n)`, `Transform(fn, copy)`, `Pop()`, `Copy()`, `Equal(o)`, `Hash()` (expr.go L272-1108).
Builders: `exp.New(kind, kv...)` (expr.go L40); `exp.NewSelect(args...)` (query.go L519) with chain `.From(x)`, `.Where(...)`, `.Join(e, exp.JoinOptions{...})`, `.GroupBy`, `.Having`, `.Qualify`, `.OrderBy`, `.Limit`, `.With(alias, as)` (query.go L162-506; panic on bad input); `exp.NewColumn(col, table, db, catalog)` (build.go L350); `exp.NewTable(table, db, catalog)` (L391); `exp.NewAlias(e, alias)` (L431); `exp.NewCast(e, to)` (L632); `exp.NewAnd/NewOr/NewNot` (L525-543); `exp.ToIdentifier(name)` (L309); `exp.ToTable(path, dialect)` (L711); `exp.ToColumn(path, quoted, dialect)` (L761); operators `e.Eq(x)`, `.Gte`, `.Lte`, `.IsIn(...)`, `.And`, `.As(alias)`, `.Asc(nullsFirst)`, `.Desc(nullsFirst)` (query.go L656-811). Builder options: `exp.WithDialect`, `WithCopy`, `WithQuoted`, ... (hooks.go L126-157). Kinds used by the adapter: `exp.Select Union Intersect Except Subquery Table Identifier Var DynamicIdentifier Anonymous Command TableFromRows Unnest Lateral Func Insert Update Delete Merge Create Drop Alter TruncateTable Copy With`, and `Placeholder`, `Parameter`, `Column`, `Literal`.

```go
for node := range stmt.Walk(true, nil) {          // BFS over every node
    if node.IsKind(exp.Table) { _ = node.This(); _ = node.Arg("db"); _ = node.Arg("catalog") }
}
for col := range stmt.FindAll(exp.Column) { _ = col.AliasOrName() }
q := exp.NewSelect("a", "b").From("x").Where("a > 1")   // "SELECT a, b FROM x WHERE a > 1"
```

### 2.3 Generating (Snowflake and DuckDB)

`(*exp.Expr) Generate(args ...any) (string, error)` and `SQL(args ...any) string` (panics) (exp/hooks.go L291-302). args: a dialect name and `exp.GenOption`s: `exp.Pretty(b)`, `Identify(v)` (bool or `"safe"`), `NormalizeIdentifiers(b)`, `UnsupportedLevel(errs.Raise)`, `Comments(b)`, `NoCopy()`, `NormalizeFunctions(v)`, `LeadingComma`, `MaxTextWidth`, `PadWidth`, `IndentWidth`, `MaxUnsupported` (hooks.go L227-271). Defaults (`DefaultGenerateOptions`, L55-66): Pretty false, Identify false, Pad 2, Indent 2, `UnsupportedLevel: errs.Warn`, MaxUnsupported 3, MaxTextWidth 80, Comments true. Strictness needs `exp.UnsupportedLevel(errs.Raise)`; the default only warns.

```go
sql, err := stmt.Generate("snowflake", exp.UnsupportedLevel(errs.Raise))
duck, err := stmt.Generate("duckdb", exp.UnsupportedLevel(errs.Raise))
```

### 2.4 Transpiling

`Transpile(sql string, read, write any, opts ...exp.GenOption) ([]string, error)`; `TranspileOpts(sql, read, write, ParserOptions, opts...)` (api.go L162-200): one string per statement (an empty string for a nil one); write nil = read.

```go
out, err := sqlglot.Transpile("SELECT IFF(x > 1, 'big', 'small') AS size FROM t", "snowflake", "duckdb",
    exp.UnsupportedLevel(errs.Raise))
// out[0] begins "SELECT CASE WHEN x > 1 THEN 'big' ELSE 'small' END AS size" (example_test.go L11-21)
```

### 2.5 Qualifying

`Qualify(e *exp.Expr, o QualifyOptions) (*exp.Expr, error)` (optimizer_qualify.go L123); `MustQualify`. `QualifyOptions` (L103-119): `Dialect, DB, Catalog, Schema any; NoExpandAliasRefs, NoExpandStars, IsolateTables, NoQualifyColumns, AllowPartialQualification, NoValidateQualifyColumns, NoQuoteIdentifiers, NoIdentify, CanonicalizeTableAliases bool; OnQualify func(table *exp.Expr); SQL string`. Steps: normalise identifiers, `QualifyTables` (L216, `QualifyTablesOptions{DB, Catalog, OnQualify, Dialect, CanonicalizeTableAliases}`), optional `IsolateTableSelects`, `QualifyColumns(e, schema, QualifyColumnsOptions{...})` (optimizer_qualify_columns.go L25), `QuoteIdentifiers`, `ValidateQualifyColumns`. Schema: `sqlglot.M(kv...)` (schema.go L22), `NewMappingSchema(schema, MappingSchemaOptions{Visible, Dialect, NoNormalize, UDFMapping})` (L105), `Schema` interface (L62-71). "Strict configuration" = keep validation on (`NoValidateQualifyColumns` false) and `AllowPartialQualification` false. Qualification is not authorisation (kit/PROVENANCE.md L53). The adapter does not call `Qualify` yet.

```go
schema := sqlglot.M("HARBOUR", sqlglot.M("HOLDINGS", sqlglot.M("POSITIONS_V", sqlglot.M("PORTFOLIO", "VARCHAR"))))
q, err := sqlglot.Qualify(stmt, sqlglot.QualifyOptions{Dialect: "snowflake", Schema: schema})
```

Others exported (not for production): `Optimize(expr, OptimizeOptions)` (optimizer.go L146), `DefaultRules()`, `Lineage`, `LineageAll`, `Diff`, `executor.Execute`.

---

## 3. Tooling

### 3.1 `Makefile`

`SHELL := /bin/bash`; `GOTOOLCHAIN := local`; `PRODUCT := sdk/go`; `LIBS := sqlglot tools`; `MODULES := $(PRODUCT) $(LIBS)`; `BIN := $(CURDIR)/.bin`. The current specification path is repository-local: `ATLAS_SPEC ?= $(CURDIR)/docs/source/atlas-spec`. The module lists and targets below describe this historical review snapshot; consult the current `Makefile` for the complete module inventory.

| Target | Runs |
|---|---|
| `check` | `fmt vet staticcheck test tidy` (L21) |
| `fmt` | `gofmt -l $(MODULES)` must print nothing |
| `vet` | for `sdk/go`: `go vet ./...` and `go vet -vettool=.bin/atlasvet ./...`; for `sqlglot`, `tools`: `go vet ./...` |
| `staticcheck` | `.bin/staticcheck ./...` in every MODULE |
| `test` | `go -C <m> test -race -count=1 ./...` for every MODULE (the layer check runs as a tools test) |
| `tidy` | `go -C <m> mod tidy -diff` (ATLAS-PACKAGES-004) |
| `vuln` | govulncheck on PRODUCT (needs network) |
| `requirements` | `go -C tools run ./reqs report -spec "$(ATLAS_SPEC)" -o docs/requirements.md` |
| `requirements-index` | `go -C tools run ./reqs index -spec "$(ATLAS_SPEC)"` |

`gateway` and `gateway/local` are in `go.work` but not in `MODULES`: `make check` does not format-check, vet, atlasvet, staticcheck, test or tidy-check them (section 5, D1).

### 3.2 `tools/vet` (atlasvet)

`Analyzers = []*analysis.Analyzer{NoInit, NoBareGo, NoGetenv, NoHTTPError}` (vet.go L17); run by `multichecker.Main` (cmd/atlasvet/main.go). Every check skips `_test.go` and generated files (L108-118).

| Analyzer | Rule | Message / exemption |
|---|---|---|
| `noinit` (L22) | no package-level `func init` (ATLAS-PACKAGES-008) | "init function: register in the deployable's one list instead (ATLAS-PACKAGES-008)"; exempt when its doc comment contains `atlas:allow-init` |
| `nobarego` (L42) | no `go` statement (ATLAS-MECH-012) | "bare go statement: start it with kit/conc.Go ..."; exempt: any package whose path contains `/sdk/go/kit/` |
| `nogetenv` (L64) | no `os.Getenv`, `os.LookupEnv`, `os.Environ`, `os.ExpandEnv` (L105) | exempt: path containing `/cmd/`, package named `config` or `settings`, file named `settings.go` |
| `nohttperror` (L89) | no `net/http.Error` | "http.Error writes text/plain: write a problem with kit/problem" |

### 3.3 `tools/layercheck`

Reads `layers.toml` (`Load`, L63; unknown keys are an error). Keys (Rules, L31-38): `[layers]` (`"<int>" = [paths]`), `independent` (`["gateway/adapter/*", ...]`), `adapters` (a path pattern), `adapter_importers` (patterns), `[[forbid]]` (`from`, `imports`, `why`), `[[external]]` (`from`, `allow`, `why`). Paths are relative to `Repo = "atlas.local/"` (L28); a path ending `/...` covers everything under it (L216-221); the most specific (longest) matching pattern decides a package's layer (L175-190). `List(dir)` runs `go list -json=ImportPath,GoFiles,Imports ./...` and skips test-only packages (L120). `Check` (L76-117) reports: a package with no layer; an import with no layer; `to > from` ("an import never points up"; same layer allowed, R24); members of one independent group importing each other; an adapter imported by a package not in `adapter_importers` (other than its own member); a forbidden import; a third-party import outside `external.allow` (stdlib always allowed). Run by `TestEveryModuleKeepsItsLayers` (ATLAS-PACKAGES-001) for every `*/layers.toml` and `*/*/layers.toml`: today only `sdk/go/layers.toml`.

`sdk/go/layers.toml`: layer 0 = `sdk/go/protocol/...`, `sdk/go/snowarrow/...`, `sdk/go/kit/...`, `sdk/go/internal/...`; 1 = `sdk/go` (package atlas, §5.4); 2 = `sdk/go/engine/...`, `sdk/go/conformance/...`; 3 = `sdk/go/cmd/...`. Forbid: protocol importing kit or snowarrow; kit importing snowarrow. External: protocol and snowarrow may import `github.com/apache/arrow-go/v18/...` only; kit and internal nothing.
There is no `gateway/layers.toml` (section 5, D3). The unit test (`TestTheCheckRefusesWhatItShould`, L45) shows the intended gateway shape: `independent = ["gateway/adapter/*", "gateway/svc/*"]`, `adapters = "gateway/adapter/..."`, `adapter_importers = ["gateway/cmd/..."]`, layers 1 `gateway/ports`, 2 `gateway/core/...`, 3 `gateway/adapter/...`, 4 `gateway/svc/...`, 6 `gateway/cmd/...`; forbid `gateway/svc/...` importing `gateway/core/...` and `sdk/go/kit/jwt`.

### 3.4 `tools/reqs`

`reqs index -spec <dir>` writes `spec/requirements.json` from every `*.md` in the spec, from lines matching `^\s*[-*]\s*\*\*((?:ATLAS|AP)(?:-[A-Z]+)*-\d{3})\.\*\*\s*(.*)$` (L35); a duplicate with different text is an error. Today: 229 requirements.
`reqs report [-strict] [-o file] [-spec dir]` maps each requirement to the test files that name it. A test proves a requirement by containing its id anywhere in a Go `_test.go` file or a Python `test_*.py`/`*_test.py` (comment or name), matched by `\b(?:ATLAS|AP)(?:-[A-Z]+)*-\d{3}\b` (L32). Convention (AGENTS.md L54): `// ATLAS-ACCESS-003` in the test file. Scenario files under `references/scenarios/` are listed but prove nothing until a runner runs them. Skips dot-dirs, `node_modules`, `tools/**/testdata`, and `tools/reqs` itself. `spec/review.toml` (`[ID] record = "..."`) lists review-proved requirements; it does not exist yet. Fails when a test names an id not in `requirements.json`; with `-strict` also when any requirement is open or a principle (§6.6.1 table in `06-6-principles.md`, "Tested by" column, ranges like `ATLAS-RESOURCE-001 to 007`) has no tested requirement (ATLAS-PRINCIPLES-002). R-numbers (R01-R26) are not requirement ids: an R acceptance test is not tracked by reqs unless it also names an ATLAS/AP id.

BASELINE (run by this reader on 2026-10-31, working tree as found): `make check` exits 0 (fmt, vet + atlasvet on sdk/go, staticcheck, `test -race` on sdk/go, sqlglot, tools; tidy). It covers neither `gateway` nor `gateway/local` (D1); those pass when run by hand (see the header).

---

## 4. The gateway module (`gateway/`, plus `gateway/local/`)

Not yet: a deployable (`gateway/cmd/...`), the HTTP pipeline, bootstrap, catalogue, describe, guide, call handling, the CLI, any provider, any profile of the journey, a `gateway/layers.toml`.

### 4.1 `gateway/ports` (interfaces and plain types; no logic, L1-7)

| Port | Types (file:line) | Contract |
|---|---|---|
| Identity | `type Identity struct{ Principal, Name, Kind /*"person" or "workload"*/, Tenant, Application string; Groups []string }` (identity.go L10-24); `ErrUnauthenticated` L28; `ErrTokenExpired` L32; `type Authenticator interface{ Authenticate(ctx, token string) (Identity, error) }` L37 | failure is ErrUnauthenticated or ErrTokenExpired (or wraps one); anything else means unavailable, not unauthenticated |
| Policy | `type Policy struct{ Revision "revision"; Roles map[string][]string "roles"; Grants []Grant "grants"; Denies []Deny "denies"; Scopes map[string]Scope "scopes"; Limits []Limit "limits"; HouseRules []HouseRule "house_rules"; HouseRulesRevision "house_rules_revision" }` (policy.go L8-28); `Grant{ID, Role, Capabilities, Interactions}` L31; `Deny{ID, Subject, Capabilities, Interactions, Reason}` L45; `type Scope map[string][]string` L54; `Limit{Subject, Capability, PerMinute, Concurrency, MaxRows}` L57; `HouseRule{Text, Capabilities}` L72; `type PolicySource interface{ Policy(ctx) (*Policy, error) }` L80 | a snapshot; a deny wins; `*` opens a dimension; R13 bounded staleness |
| Audit | `type Fact struct{ Kind FactKind; At time.Time; RequestID string; Attempt int; Operation, Principal, CallerKind, Application, Environment, PolicyRevision, Address, Exact, RequestDigest, Code string; Rows int64; Truncated bool; Usage map[string]string; Duration time.Duration }` (audit.go L13-42); `FactKind`: `admission`, `refusal`, `outcome`, `settlement` (L45-53); `type AuditSink interface{ Record(ctx, f Fact) error }` L57 | R09; no token, key, body or query string (MECH-014); an admission that cannot be recorded is not dispatched (ATLAS-CORE §8 step 7) |
| Provider | `type Provider interface{ Serve(ctx, inv *Invocation) (*Answer, error) }` (provider.go L21); `Invocation{RequestID, Capability (pinned), Interaction, Exact string; Request *ijson.Object; Columns []string; Caller Caller; Scope Scope; Deadline time.Time; MaxRows int}` L26-48; `AllValues = "*"` L51; `(Scope) Allows(dimension, value) bool` L54; `(Scope) Open(dimension) bool` L64; `Caller{Principal, Name, Kind, Application string; Roles []string}` L75; `Answer{Record *ijson.Object; Rows RowStream}` L84; `type RowStream interface{ Next(ctx) (*ijson.Object, error); Close() error }` L92; `SliceRows(rows) RowStream` L98 | in process and serialisable (R20); request validated, defaulted and scoped before the call; provider never repeats a pipeline step |
| Warehouse | `type Query struct{ Object string; Columns []Column; Where []Predicate; Latest *Latest; OrderBy []string; Limit int; Tag string }` (warehouse.go L12-30); `Column{Name; Type ColumnType}` L33; `ColumnType`: `string` (string), `integer` (int64), `float` (float64), `decimal` (string, exact), `boolean` (bool), `date` (string YYYY-MM-DD), `timestamp` (time.Time UTC); nil = null (L41-53); `Op`: `in`, `eq`, `gte`, `lte` (L56-64); `Predicate{Column; Op; Type; Values []any}` L68; `Latest{Date; Per []string; OnOrBefore string; Within []Predicate}` L77; `type Rows interface{ Next() ([]any, error); Close() error }` L89; `ErrUnknownObject` L96; `type Warehouse interface{ Query(ctx, q Query) (Rows, error) }` L100; `ErrWarehouseUnavailable` L105 | no SQL text; values always parameters; `Latest` keeps each group's latest date on or before the bound, chosen before `Where` |

Missing ports for the journey and its R-steps: idempotency store (R08), operation store / worker lease and fencing (R08, R17), budget authority and reservations (R07), approval or confirmation receipts (R06), credential binding (R26 is client side), a freshness or load-metadata source for `data.status@v1` (AMBIGUOUS: readings (a) a `Warehouse` query on a load-log object, (b) a new port), a result-cache key (R18), a policy snapshot holder outside the request path (see 4.7).

### 4.2 `gateway/fakes` (in memory, same seam)

| Fake | API | Notes / missing |
|---|---|---|
| `Audit` (audit.go L12) | `Record(ctx, f)`, `Fail(err)` (nil heals), `Facts() []ports.Fact`, `Of(requestID) []ports.Fact` | no porttest suite for AuditSink |
| `Policy` (policy.go L12) | `NewPolicy(p)`, `Set(p)`, `Fail(err)`, `Policy(ctx)` | no porttest suite |
| `Tokens` (tokens.go L14) | `NewTokens(c clock.Clock)`, `Issue(token, id, expires)` (zero expires = never), `Revoke(token)`, `Authenticate(ctx, token)` (expired when `!now.Before(expires)`) | no porttest suite; no real adapter over `kit/jwt` (Claims lacks `azp`/`appid`/`idtyp`, only in `Raw`) |
| `Table{Name; Columns []ports.Column; Rows [][]any}`; `Warehouse` (warehouse.go L18-91) | `NewWarehouse(tables...)` (names upper-cased), `Fail(err)`, `Block(on bool)` (waits for ctx), `Queries() []ports.Query`, `Query(ctx, q)` | SQL three-valued logic; nulls last; a `Latest` group with a null `Per` key is dropped (as the adapter's inner join does) |
| Provider | none | needed for the pipeline and journey |

`fakes_test.go`: `TestWarehouse` runs `porttest.Warehouse` on the fake.

### 4.3 `gateway/porttest`

Only one suite: `Warehouse(t, newWarehouse func(t, tables []fakes.Table) ports.Warehouse)` (warehouse.go L68). Data: `Holdings = "TESTDB.PUBLIC.HOLDINGS"` (L17); `WarehouseTables()` (L33): columns `PORTFOLIO` string, `SECURITY` string, `ISIN` string, `AS_OF` date, `MV` decimal, `CCY` string, `WEIGHT` float, `LOTS` integer, `OPEN` boolean, `LOADED_AT` timestamp; six rows including nulls, `2^53+1`, and `12345678901234567890.123456`. Subtests (L118-184): "columns in order, nulls last"; "in, eq and bounds"; "a decimal compares by value, past a float's precision"; "a null meets no condition"; "the latest of each group"; "the latest is chosen before the query's conditions"; "every type, exactly"; "a limit"; "an object it does not serve" (ErrUnknownObject); "a call whose context has ended". Run by the fake (fakes_test.go) and by the Snowflake adapter over DuckDB (`gateway/local/duckwh/duckwh_test.go`). Missing: suites for Authenticator, PolicySource, AuditSink, Provider (AGENTS.md L55: every port has a suite).

### 4.4 `gateway/adapter/snowflake`  IN SCOPE (sqlglot behind the compiler and guard, REVIEW.md step 4)

compile.go: `MaxStatement = 256 << 10` bytes, `MaxDepth = 64` AST levels, `MaxValues = 1000` per IN (L20-22); `type Statement struct{ SQL string; Args []any; Objects [][]string }` (L27); identifiers must match `^[A-Za-z_][A-Za-z0-9_$]{0,254}$` and are upper-cased (L35, L51); objects are exactly `database.schema.object` (L58); aliases `atlas_t`, `atlas_l`, `atlas_latest` (L38-42). `Compile(q ports.Query, registered [][]string, dialect string) (*Statement, error)` (L155): builds Snowflake text (decimals selected as `CAST(x AS VARCHAR)`; values `?`, dates `CAST(? AS DATE)`, timestamps `CAST(? AS TIMESTAMP)`, decimals `CAST(? AS NUMBER(p, s))`; `IN ()` empty → `1 = 0`; `Latest` = `JOIN (SELECT per..., MAX(date) ... GROUP BY per) ON ...`; `ORDER BY ... ASC NULLS LAST`; `LIMIT n`), runs `Guard`, then parses with sqlglot, checks depth and generates in `dialect` with `exp.UnsupportedLevel(errs.Raise)` (L263-277). An unregistered object returns `ports.ErrUnknownObject`.
guard.go: `Guard(sql string, registered [][]string) Verdict` (L65); `type Verdict struct{ Accepted bool; Reason string; Objects [][]string }` (L54). Reasons, in check order (L29-49): `parse_error`, `empty`, `multiple_statements`, `dml`, `ddl`, `copy`, `file_transfer`, `call`, `not_a_query`, `select_into`, `stage`, `table_function`, `system_function`, `dynamic_identifier`, `unqualified_object`, `unregistered_object`. Accepts only SELECT/UNION/INTERSECT/EXCEPT (or a Subquery of one) over `[db, schema]` or `[db, schema, object]` registered entries; unquoted identifiers upper-cased; CTE names in scope are not objects; a panic fails closed. Proved by every case of `references/vectors/guard.json` (guard_test.go L11-13, ATLAS-DATA-006).
warehouse.go: `type Options struct{ Dialect string /*"snowflake" default, or "duckdb"*/; Registered [][]string; MaxCompiles int /*default 8*/ }` (L21); `New(db *sql.DB, opt) *Warehouse` (L42); `(*Warehouse) Compile(ctx, q) (*Statement, error)` (L54; the explain path); `Query(ctx, q) (ports.Rows, error)` (L65); driver errors `driver.ErrBadConn`, `sql.ErrConnDone`, `net.Error` wrap `ErrWarehouseUnavailable` (L78-87); `convert` maps driver values to the port's Go types (L139). Imports no driver.

### 4.5 `gateway/local/duckwh` (separate module: DuckDB, cgo)

`Open(ctx, tables []fakes.Table) (*sql.DB, *snowflake.Warehouse, error)` (L24): in-memory DuckDB, `ATTACH ':memory:' AS <DB>`, `CREATE SCHEMA`, `CREATE TABLE`, inserts; registers every table; dialect `duckdb`. Types (L41-44): string `VARCHAR`, integer `BIGINT`, float `DOUBLE`, decimal `DECIMAL(38, 6)`, boolean `BOOLEAN`, date `DATE`, timestamp `TIMESTAMP`. `Load(ctx, db, tables) error` (L47). Passes `porttest.Warehouse`.

### 4.6 `gateway/core/access`  IN SCOPE (R05)

`var Dimensions = map[string]string{"PortfolioCode": "portfolio"}` (L24): the typed scope rule. `type Subject struct{ Identity; Policy; Roles []string; Scope ports.Scope }` (L30); `Resolve(policy, id) *Subject` (L39): roles of the principal and its groups; scopes of principal, groups and roles merged. `(*Subject) Caller() ports.Caller` (L64). `type Decision struct{ Allowed bool; Code, Reason, HeldBy string }` (L88). `Decide(p *profile.Profile, interaction) Decision` (L97): built-ins allowed (`HeldBy "atlas"`); confidential without any grant → `not_found`; a deny naming the principal, a group or a role → `denied` with its reason; no grant → `not_granted` ("no grant covers <id>@v<N>:<interaction>"). Capability patterns: `*`, id, pinned id, `domain.*` (L69-80). `Visible(p) bool` (L131); `Holds(p) bool` (L142); `Limit(p) ports.Limit` (L153, tightest non-zero); `ScopeFor(schemas...) ports.Scope` (L176: open → `["*"]`, none → `[]`); `type Value struct{ Pointer, Dimension, Value string }`; `Values(s *schema.Schema, v any) []Value` (L205): every scoped value in a request or row. No tests (`[no test files]`).

### 4.7 `gateway/core/pipeline` (unexported only)

`limiter` (limits.go): token bucket per key, rate `PerMinute/60` per second, burst `PerMinute`; concurrency count; idle buckets dropped after `maxIdle = 10 * time.Minute`, swept every 1024 calls; `take(key, lim, now) (release func(), retryAfter int, ok bool)`; `remaining(key, lim) map[string]int64` with keys `rate`, `concurrency` (for Atlas-Limit-Remaining). Protective only, not a budget (R07). `policyCache` (policy.go): `get(ctx)` rereads when older than `refresh`, keeps the last policy while the source fails up to `maxAge`, then `errStalePolicy` joined with the cause (R13). No pipeline type, no steps, no tests.

### 4.8 `gateway/core/registry`  IN SCOPE

`Load(doc []byte) (*profile.Profile, error)` (L32): parses, discards any carried `fingerprint`, parses the profile and sets `fingerprint` to the computed one. `type Entry struct{ Profiles []*profile.Profile /*oldest first; last served*/; Provider ports.Provider }` (L52). `type Capability struct{ Pinned grammar.Pinned; Current *profile.Profile; Chain []*profile.Profile; Provider ports.Provider }` (L58), `Builtin()`. `type Route struct{ Capability *Capability; Interaction string }` (L69). `type Snapshot struct{ Generation int; ETag string /*quoted, first 16 hex of sha256 over current exact ids*/ }` (L75), `Capabilities()`, `Lookup(pinned)`, `Profile(exact)` (every exact id ever served while running), `Route(path) (Route, bool)` (https binding address), `Has(p)` (gate.Catalogue). `New() *Registry` (L110); `Snapshot()` (L117); `type Refused struct{ Reasons []string }` (L121); `Register(entries ...Entry) error` (L132): one generation, admitted whole or refused with every reason; each profile passes `gate.Check` against the one before (the previously served Current is prepended); one https address per interaction. Tests: register and route, refusal of a breaking change (§3.3.12), hot replace keeping the old exact id, shared address refused.

### 4.9 `gateway/core/builtins/profiles`

Ten JSON profiles: `atlas.access`, `atlas.catalog`, `atlas.clients`, `atlas.describe`, `atlas.guide`, `atlas.operation` (interactions `get`, `list`, `cancel`, `watch`), `atlas.search`, `atlas.setup`, `atlas.usage`, `atlas.whoami`, each `@v1`, family `data`, `stability: "official"`, deadline `PT30S`/`PT2M`, binding `POST /v1/atlas/<id>:<interaction>`, `encodings ["json"]`, `auth "bearer"`. No Go code loads them. They cannot be loaded: each schema is `$ref` to `https://atlas.jhg.com/protocol/1.0/builtins.schema.json#/$defs/<name>` (whoami L56, L68), and `schema.Compile` accepts only `#/$defs/<name>`. Verified by this reader: `profile.Parse` of `atlas.whoami@v1.json` fails with `profile /schemas/whoami_request: not an Atlas schema: $ref at "/$ref": $ref is #/$defs/<name>; type at "/type": every schema has a type` (section 5, D2).

---

## 5. Defects and smells

Severity: H blocks the journey or a guarantee; M wrong behaviour in a case the journey can hit; L hygiene.

| # | Sev | File:Line | Finding |
|---|---|---|---|
| D1 | H | `Makefile` L10-12 | `gateway` and `gateway/local` are in `go.work` but not in `MODULES`, so `make check` (AGENTS.md "it must pass") never formats, vets, atlasvets, staticchecks, tests or tidy-checks the gateway. They pass today when run by hand (this reader). |
| D2 | H | `gateway/core/builtins/profiles/*.json` (e.g. whoami L56, L68) | Built-in schemas `$ref` an external URI; `schema.Compile` (schema.go L333-343) refuses anything but `#/$defs/<name>`, and a `$ref` with `$schema`/`$vocabulary` siblings. `profile.Parse` fails (verified). |
| D3 | H | `tools/layercheck/layercheck.go` L225-234 | `ours` lists only `sdk`, `gateway`, `clients`, `tools`; a trimmed `sqlglot/...` import has no dot and is not ours, so `stdlib` returns true and every sqlglot import passes unchecked. Also no `gateway/layers.toml`, so the gateway is not layer-checked at all (AGENTS.md L39). |
| D4 | H | `sdk/go/protocol/problem.go` L25, L29, L158-164; `kit/problem/problem.go` L44-62 | `next` and `request_id` are required by problem.schema.json L7-14, but `omitempty` and `NewProblem` leave `next` out; `problem.Write` fills only `request_id`. A served problem can fail the reference schema. |
| D5 | M | `kit/problem/problem.go` L53-61 | `Write` sets no `Cache-Control: no-store` (§9.6.7, headers.go L47-50) and does not `Check` the problem. |
| D6 | M | `kit/middleware/middleware.go` L304-319 | Carried defect (kit/PROVENANCE.md L29): a past `Atlas-Deadline` becomes the maximum instead of a refusal; `time.Until` reads the wall clock; reads `r.Header.Get`, not `protocol.Field`. Fix with R17 (step 3). |
| D7 | M | `gateway/core/pipeline/policy.go` L30-49 | `get` holds the mutex while calling `src.Policy(ctx)`: the store is read on the request path and every request waits behind one slow read (constitution rule 3). |
| D8 | M | `sdk/go/protocol/resolve/resolve.go` L84-86 | Identity plan when writer == reader ignores `Options.Columns`, so projection is skipped for the common case. |
| D9 | M | `sdk/go/kit/jwt/verifier.go` L141-155 | Empty `Issuer` or `Audiences` accepts any issuer or audience; no tenant (`tid`) check; `Now` defaults to `time.Now`, not `clock.Clock`. R26 needs bound issuer, audience and tenant. |
| D10 | M | `gateway/adapter/snowflake/warehouse.go` L65-75 | `Query.Tag` (ports/warehouse.go L29, Snowflake `QUERY_TAG`) is never applied. |
| D11 | M | `gateway/adapter/snowflake/guard.go` L88-137 | `exp.Command` is refused only as the top statement; nested Command or unknown nodes are not checked (INTEGRATIONS: refuse `exp.Command` and unknown nodes). |
| D12 | M | `gateway/adapter/snowflake/compile.go` L263-277 | `sqlglot.Parse` without `ParserOptions.MaxNodes`; depth checked only after the full parse; the text is parsed twice (Guard and generate). Inputs are compiler-built, so risk is low. |
| D13 | M | `gateway/adapter/snowflake/compile.go` L233-238 | `ASC NULLS LAST` is dropped by sqlglot; correctness relies on Snowflake's `DEFAULT_NULL_ORDERING` being `LAST` on the connection. |
| D14 | M | `gateway/adapter/snowflake/warehouse.go` L151-156 | Decimal values accepted by `big.Rat.SetString` (`1/3`, `1e5`), not `grammar.ParseDecimal`. |
| D15 | M | `sdk/go/protocol/arrowschema/rows.go` L25-40 | `ToBatch` silently drops row members the Arrow schema lacks; `Append` for Duration truncates sub-microsecond values (L110). |
| D16 | M | `sdk/go/protocol/gate/lint.go` L65, `ready.go` L26, `classify.go` L541, L574 | Iteration over Go maps (`p.Schemas`, meaning names): findings come out in a different order run to run; `lensConsistent.has` picks a schema nondeterministically when two share a field name. |
| D17 | M | `sdk/go/protocol/profile/profile.go` L312-314 | `Parse` returns only the first error; `errors` and `traits` that are not arrays are ignored silently (L195, L254); `errors[].status` absent gives 0. |
| D18 | L | `sdk/go/kit/telemetry/traceparent.go` L32-56 vs `protocol/headers.go` L578-596 | Two traceparent parsers with different rules (telemetry accepts uppercase hex and extra fields for version 00). `Propagate` doc (L96-100) says an untrusted incoming trace is recorded as parent; the code drops it. |
| D19 | L | `sdk/go/kit/telemetry/accesslog.go` L27-29, L98-105 | Doc names `principal`, `upstream` fields that were removed; `RouteFrom` doc says it falls back to `r.Pattern`, the code does not. |
| D20 | L | `sdk/go/kit/middleware/middleware.go` L278-280 | Doc says a MaxBytes error maps to 413 via `apierror`; `kit/problem.From` maps it to `invalid_request` 400. |
| D21 | L | `sdk/go/kit/jwt/jwks.go` L5 | Package doc says `Package auth`; the package is `jwt`. `KeySet.now` is unexported, so outside tests the clock cannot be injected. |
| D22 | L | `sdk/go/protocol/resolve/columns.go` L80 | Package-level `valueByValue atomic.Int64` test counter in production code. |
| D23 | L | `sdk/go/protocol/headers.go` L599-601 | `Traceparent.String` always writes `00`, ignoring `Version`. `ChooseMedia` ignores `q=0` (L739). |
| D24 | L | `gateway/ports/warehouse.go` L89-92 | `Rows.Next()` takes no context (AGENTS.md L44); cancellation relies on the Query's ctx. |
| D25 | L | `gateway/porttest/warehouse.go` L12 | The port suite imports `fakes` for its data type (`fakes.Table`), so every adapter test depends on the fakes package. |
| D26 | L | `sqlglot/exp/hooks.go` L29 vs `sqlglot/parser_core.go` L49-51 | Comment says `errs.Unset` means RAISE; the parser maps it to `Immediate`. |
| D27 | L | `sdk/go/snowarrow/encode.go` L200-202 | `scaled` divides by `int64(math.Pow10(9-scale))`, zero for scale > 9 (division panic); `decode.go` L113-131 does not check the result fits Decimal128's precision. |
| D28 | L | `tools/reqs/main.go` L31-32 | Comment says the regex handles ids split by a space; it does not. |
| D29 | L | `AGENTS.md` L46 | Names `kit/retry`; it does not exist. |
| D30 | L | `sdk/go/kit/health/health.go` L66-79 | `WithTimeout` leaves its goroutine running until a check that ignores ctx returns. |

---

## 6. For the first build

### 6.1 IN SCOPE (reuse, do not rewrite)

- Wire words, media types, header codecs, the problem registry and exit codes: `protocol` (1.1).
- Grammars and SF: `grammar`, `sf`; JSON: `ijson` (Parse with Limits on every body).
- Schemas: `schema.Compile` (Request kind for `takes`), `Validate` → `protocol.InvalidRequest` (R04: validate before any transformation; unknown request fields refused by closed schemas).
- Profiles: `profile.Parse`, `protocol.Fingerprint`, `CheckFingerprint`, `registry.Load`, `registry.Register` (gate at admission), `Snapshot.Route`.
- Gate: `gate.Check`, `Lint`, `Ready`, `Classify` (R03 model already in code) for the four journey profiles and holdings-dates.
- Results: `frame` NDJSON writer and reader with the end line; `arrowschema` + `frame` Arrow optional (R11).
- Resolution: aliases, defaults, UNKNOWN, projection (`resolve` restricted by R03; `convert_unit`, `promote`, `drop` refused by the gate).
- Access: `core/access` (`Resolve`, `Decide`, `ScopeFor`, `Values`) with `PortfolioCode` → `portfolio` (R05).
- Protective limits (`pipeline.limiter`), policy freshness (`pipeline.policyCache`, R13, fix D7).
- Warehouse: `ports.Warehouse`, `fakes.Warehouse`, `porttest.Warehouse`, `adapter/snowflake` (Compile, Guard), `local/duckwh` (no Docker).
- Kit: `clock`, `conc`, `health`, `httpserver`, `lifecycle`, `middleware` (Recover, RequestID, RealIP, MaxBytes; Deadline after D6), `problem` (after D4, D5), `telemetry`, `jwt` (behind an Authenticator adapter, R26 bindings), `hot`.
- Audit facts (R09): `ports.Fact`, `fakes.Audit`.

### 6.2 DEFERRED

- `snowarrow`, `HandoffSnowflakeArrow1`, `ReachSnowflake`, `MediaHandoff`, `ExecutionHandoff` (R10, step 5).
- Events: `MediaCloudEvent`, `grammar.DataSchema`, `grammar.EventType`, `LevelEvents` (R15, R16, R25).
- Resources: `grammar.ResourceName`, resource lint rules (R14, after the journey).
- The full lens algebra (`convert_unit`, `promote`, `drop`, `units` conversions) as an obligation (R03; stays as an experiment).
- `LevelBroker` (O54); bulk, CSV, Parquet encodings.
- sqlglot optimiser, executor, lineage, diff in production (kit/PROVENANCE.md L52).

### 6.3 Must be built (not in the code)

Gateway deployable and pipeline; journey profiles and fixtures (two identities, different portfolio scope); bootstrap, catalogue, describe (R01 private caller state), search; providers for the four capabilities; idempotency (R08), receipts (R06), budget authority (R07), operation store and worker (R08, R17); freshness source for `data.status`; port suites for Authenticator, PolicySource, AuditSink, Provider; `gateway/layers.toml`; gateway in `make check`; a loadable form of the built-in profiles; CLI, Python face (from the first implementation), Node client.

<!-- END -->
