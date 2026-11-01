# Digest: pass 1's assets (historical first implementation), and what to bring back

> Historical engineering study (2026-10-31). Retained for its requirements, counterexamples and design reasoning. Implementation observations and line references describe that review snapshot; use [current progress](../../HANDOFF-PROGRESS.md) and the named source symbols for current status. Paths are relative to the repository root unless a citation key says otherwise.

Written 2026-10-31 for the pass-2 build. Every fact cites its source.

## How to read this digest

- `pass-1:<path>:<line>` labels the historical first implementation discussed below. Its proof results are retained in `docs/pass-1/PROOF-0.md`.
- `pass-2:<path>:<line>` labels the second implementation at the time of this review; use current source symbols to locate surviving code.
- `spec:<file>:<line>` is `docs/source/atlas-spec/<file>`, and `review:<file>:<line>` is its `review/` folder.
- `refs:` is `spec/protocol/references/` in the repository.
- Labels:
  - **IN SCOPE**: the first build (the journey) must implement it.
  - **DEFERRED**: the first build defers it.
  - **OVERRIDDEN BY REVIEW (Rnn)**: the review, or REVIEW.md, changes what the spec or pass 1 did.
  - **AMBIGUOUS**: the sources leave it open. Each possible reading is given.
- Verdicts: **BRING BACK** (as it is), **ADAPT** (keep the design, change the code to the new protocol), **REWRITE** (keep the lessons or tests only), **DROP**.

### What governs

- REVIEW.md:92 says: "Pass 1's proof (historical first implementation) is an asset. Step 4 brings its engine, the Python face and the hermetic Snowflake fake back onto the new protocol."
- REVIEW.md:19 says pass 1 is "Kept as an asset ... rather than being rewritten" (DESIGN-REVIEW, "keep the existing Go/Python proof"; ATLAS-CORE §6).
- review:ATLAS-CORE.md:140 says: "The current Go engine used by the Python proof is worth retaining while measured ... A subprocess is an implementation choice behind Python's public contract".
- review:DESIGN-REVIEW.md:182 says to keep the proof. It "records useful fake-backed tests of exact types, evolution, handoff renewal, final batches and process lifetime. ... This review did not rerun those tests."
- review:DESIGN-REVIEW.md:176 defers "Snowflake chunk handoff until real encoding/access proofs pass". REVIEW.md:33 (step 5) defers "direct downloads". **So the whole handoff path of pass 1 is DEFERRED.** That covers chunk links, renewal, `Atlas-Renew`, the snowarrow reader in clients and the snowfake result storage.
- REVIEW.md:90 says that within a version no served profile may depend on `convert_unit`, `promote` or `drop`. A client is obliged only to "aliases, defaults, UNKNOWN and projection". **OVERRIDDEN BY REVIEW (R03, R04).**

### What pass 1 contains: the files and their sizes

Historical file inventory. The assigned files are marked **(A)**; sizes describe the reviewed first implementation.

| Path | Lines | |
|---|---|---|
| `sdk/go/client/client.go` | 401 | (A) |
| `sdk/go/client/lock.go` | 95 | (A) |
| `sdk/go/client/table.go` | 266 | (A) |
| `sdk/go/client/table_test.go` | 83 | (A) |
| `sdk/go/engine/engine.go` | 347 | (A) |
| `sdk/go/cmd/atlas-engine/main.go` | 52 | (A) |
| `clients/python/fiatlas/{__init__,_engine,client,errors}.py` | 12 / 280 / 122 / 21 | (A) |
| `clients/python/tests/{conftest,test_engine_process,test_evolution,test_tables}.py` | 70 / 141 / 72 / 87 | (A) |
| `clients/python/pyproject.toml` | 22 | (A) |
| `clients/cli/go.mod` | 3 | (A). This is the whole CLI: `module atlas.local/clients/cli` / `go 1.25.0`. **There is no CLI code** (pass-1:README.md:95 "Not built yet"; PROOF-0.md:101) |
| `gateway/adapter/snowflake/{snowflake,plan,snowflake_test}.go` | 162 / 181 / 157 | (A) |
| `gateway/adapter/snowflake/snowfake/{snowfake,harbour}.go` | 492 / 132 | (A) |
| `gateway/svc/harbour/{harbour,profiles}.go` | 189 / 155 | (A) |
| `gateway/core/{builtins/builtins,catalog/catalog,catalog/governance,pipeline/pipeline}.go` | 159 / 140 / 119 / 294 | (A). These four are all of `gateway/core/**` |
| `gateway/edge/http/{edge,edge_test}.go` | 205 / 312 | (A) |
| `gateway/e2e/e2e_test.go` | 315 | (A) |
| `gateway/ports/{call,ports}.go` | 32 / 196 | (A) |
| `gateway/fakes/{fakes,issuer}.go` | 91 / 102 | (A) |
| `gateway/porttest/datasource.go` | 116 | (A) |
| `PROOF-0.md`, `README.md` | 108 / 95 | (A) |
| `gateway/cmd/gateway/main.go` | 200 | Read too: the personas, `/.dev/token` and flags |
| `sdk/go/protocol/{protocol,end,documents,problem}.go` | 133 / 53 / 136 / 111 | Read too: pass 1's wire constants |
| `sdk/go/snowarrow/{reader,snowarrow}.go` | 218 / 121 | Read too: the client's handoff path depends on them |
| `sdk/go/kit/jwt/{verifier,jwks}.go`, `sdk/go/kit/problem/problem.go` | 166 / 279 / 62 | Read too: the issuer's verifier |

Already carried into pass 2, by diff against the pass-2 implementation:
- `sdk/go/snowarrow/snowarrow.go` is identical.
- `decode.go` differs by 12 diff-lines, and `encode.go` by 8.
- **`reader.go` is absent from the pass-2 implementation.**
- `kit/jwt/verifier.go`, `jws.go`, `kit/problem/problem.go` and `kit/middleware/middleware.go` are identical.
- `kit/jwt/jwks.go` differs by 34 diff-lines.

The module path is unchanged: `atlas.local/...` (pass-2:sdk/go/go.mod:1). Ported files keep their import prefixes.

---

## 1. Each component of pass 1

### 1.0 The verdicts at a glance

| Component | Verdict | Scope |
|---|---|---|
| `sdk/go/engine` (the runner) | **ADAPT**: keep the process design, the second pipe and cancel; change the line shapes to §9.8.3 | IN SCOPE |
| `sdk/go/cmd/atlas-engine` | **ADAPT**: keep the FD variable, ignoring SIGINT and the orphan check; go through `kit/conc` | IN SCOPE |
| `clients/python/fiatlas` | **ADAPT**: keep `_engine.py`'s process rules nearly verbatim; rewrite the public face to the §9.7.1 names and R11 | IN SCOPE |
| `clients/python/tests` | **ADAPT**: process tests nearly as they are; the evolution and added-later tests rewritten for R03 and the journey | IN SCOPE |
| `sdk/go/client` | **REWRITE** as package `atlas` (CRITIQUE #30). Port the lock walk, the fingerprint recheck and the resolving-reader shape | IN SCOPE (handoff part DEFERRED) |
| `snowfake` Harbour generator (`harbour.go`) | **ADAPT** as deterministic fixture data for DuckDB or the fake warehouse, reshaped to the journey's profiles | IN SCOPE |
| `snowfake` warehouse (`snowfake.go`): Go filter evaluation | **DROP** (CRITIQUE #25: "DuckDB runs the adapter's own statement") | — |
| `snowfake` result storage (signed expiring links, header key, 24 h results) | Retain the design for the handoff proof | DEFERRED |
| `adapter/snowflake` (`plan.go` string SQL) | **REWRITE**. The pass-2 implementation has `compile.go`, `guard.go` and `warehouse.go` on sqlglot. Port the test assertions | IN SCOPE (as tests) |
| `svc/harbour` | **REWRITE**: profiles become JSON files, the revisions follow R03, and the journey's five capabilities | IN SCOPE |
| `core/*` | **REWRITE**. The pass-2 implementation has `core/access`, `core/registry` and `core/pipeline/{limits,policy}.go`. Port the order and the tests | IN SCOPE |
| `edge/http` | **REWRITE** as a binding router (CRITIQUE #12) on `protocol/frame`. Port its test list | IN SCOPE |
| `e2e` | **ADAPT** the style: real binaries and a ready line (CRITIQUE "What survives") | IN SCOPE |
| `ports`, `porttest/datasource.go` | **DROP**: superseded by `pass-2:gateway/ports/*` and `pass-2:gateway/porttest/warehouse.go`. Port the suite's checks | — |
| `fakes.Limits`, `fakes.Audit` | **DROP**: superseded (`pass-2:gateway/fakes/audit.go`, `pass-2:gateway/core/pipeline/limits.go`; step 3 R07/R09) | — |
| `fakes.Issuer` | **ADAPT**. The plan requires a fake issuer running the real `kit/jwt` verifier (pass-2:docs/pass-2/PLAN.md:88). The pass-2 implementation has an opaque token table instead. See §3 and the ambiguities | IN SCOPE (AMBIGUOUS) |
| `cmd/gateway` | **ADAPT** the ready line, the flags and the personas. Two identities with different portfolio scope (REVIEW.md:30) | IN SCOPE |

### 1.1 `sdk/go/client`: the Go client (`client.go`, `lock.go`, `table.go`, `table_test.go`)

**What it does.** pass-1:sdk/go/client/client.go:1-7:
- It reads the bootstrap and calls the built-ins through the bindings the bootstrap names.
- It describes profiles, recomputes their fingerprints and caches them by exact id forever.
- It picks the reader profile: the lock's entry, else the current one.
- It validates the request in the client, POSTs to the binding and reads `Atlas-Profile`.
- It resolves every batch from the writer's profile to the reader's.
- It reads Arrow answers with the final batch, and handoffs with link renewal.

**API, exactly** (pass-1:sdk/go/client/client.go):

| Item | Value | Line |
|---|---|---|
| `Name`, `Version` | `"atlas-go"`, `"0.1.0"` | 30-33 |
| `Features` | `Levels: core, streaming, columnar, handoff`; `Handoff: [snowflake-arrow/1]` | 36-39 |
| `Options` | `URL`, `Token`, `Lock *Lock`, `HTTP *http.Client`, `Mem memory.Allocator`, `Parallel int` ("8 when zero") | 43-52, 84-86 |
| `Connect(ctx, Options) (*Client, error)` | URL = `o.URL` else `os.Getenv("ATLAS_URL")`; token = `o.Token` else `ATLAS_TOKEN`. No URL gives `invalid_request` "no gateway: pass a URL or set ATLAS_URL". GETs `url+"/.well-known/atlas"`. Unreachable gives `unavailable`. A non-200 or undecodable answer gives `invalid_document`. `boot.Protocol` must start with `"atlas/0."`, else `upgrade_required` | 69-103 |
| `post(ctx, Binding, body, headers)` | URL = `boot.Servers[b.Server].URL + b.Address` (follows the binding, never builds a path). Deadline = ctx deadline else `time.Now().Add(30*time.Second)`. A status `>= 400` goes to `readProblem` | 113-149 |
| Headers sent on every call | `Content-Type: application/json`; `Atlas-Client: name="atlas-go", version="0.1.0", protocol="atlas/0.1"` (built with `%q`); `Atlas-Client-Features: levels=(core streaming columnar handoff), handoff=(snowflake-arrow/1)`; `Atlas-Deadline: @<unix seconds>`; `Authorization: Bearer <token>` when set | 130-139; pass-1:sdk/go/protocol/protocol.go:74-83, 111 |
| `readProblem` | Reads at most `1<<20` bytes. If it is not a problem, or `code == ""`, it becomes `upstream_failed` "the gateway answered <status> without a problem document", with `Status` set to the HTTP status | 151-160 |
| `builtin(ctx, address, req, out)` | Looks up `boot.Builtins[address]`; a missing one is `not_found`. **No `Atlas-Profile-Expect` on built-ins**, which matches scenario CORE-CALL-001 (spec:09-8-conformance.md:96) | 163-177 |
| `WhoAmI`, `Catalog`, `Search(words)` | Request bodies `{}`, `{}`, `{"words": …}` | 180-195 |
| `Describe(ctx, ref)` | Sends `{"exact": ref}` if `ref` contains `#`, else `{"id": ref}`. Recomputes `protocol.ExactOf(&ans.Profile)` and refuses a mismatch with `invalid_document`. Caches by `ans.Exact` | 200-217 |
| `Profile(ctx, exact)` | Cache, else `Describe` ("cached for ever") | 221-233 |
| `reader(ctx, pinned)` | `lock.Capabilities[pinned]` gives that exact id; **else a `Describe` on every call** (no cache of the current profile) | 241-253 |
| `CallOptions{Execution}` | "what the client sends as headers, never as members of the request" (AP-CLIENT-003) | 257-260 |
| `Fetch(ctx, address, request, CallOptions) (*Table, error)` | Parse the address; find the reader; the interaction (missing, or `Result == nil`, is `not_found`); `validate`. Headers `Atlas-Profile-Expect: <readerExact>`, `Accept: application/vnd.apache.arrow.stream`, `Atlas-Execution` if set. Uses **`inter.Bindings[0]`** always. Reads `Atlas-Profile` and fetches the writer profile. `BuildChain(writer→reader)`. Projects the reader table to the requested `columns`. Branches on Content-Type: Arrow stream, `application/vnd.atlas.handoff+json`, or another, which is `invalid_document` | 264-335 |
| `Call(ctx, address, request) (json.RawMessage, error)` | The same up to the POST (with `Atlas-Profile-Expect`). Returns the raw body, **unvalidated** | 338-361 |
| `validate` | JSON round trip, then `protocol.DecodeRecord`, then `protocol.Validate(reader.Request, req)`. The first failure becomes `invalid_request`, with `WithTarget(pointer)` | 365-381 |

**`lock.go`** (pass-1:sdk/go/client/lock.go):
- `LockFile = "atlas.lock"` (18).
- `Lock{Path; Capabilities map[string]string toml:"capabilities"}` (24-27). It maps pinned to exact.
- `LoadLock` checks that each entry's exact id parses and names its key's pinned id, else `invalid_document` (30-42).
- `FindLock(dir)` walks the directory and its parents, and returns nil when there is none (45-59).
- `Save` writes `# The exact profiles this application was built against. Written by atlas lock.` then `[capabilities]` and `"k" = "v"` lines, sorted. It writes to `path+".tmp"` with mode `0o644`, then renames (62-78).
- `LockCurrent(ctx, pinned, path)` describes each entry and saves (82-95).

**`table.go`** (pass-1:sdk/go/client/table.go):
- `Table{Writer, Reader string; Execution string; array.RecordReader}` (22-29).
- `resolving` (33-103) checks each batch: `ArrowSchema(writer.Result.Project(names), writerExact)` against the batch, with `SameColumns`. A mismatch is `invalid_document` "a batch does not match …". Then `protocol.ResolveBatch(mem, chain, rec, readerTable, readerSchema)`.
- `arrowAnswer` (109-160):

  | Case | Code |
  |---|---|
  | The stream ends without a final batch | `upstream_failed` "the answer ended without its final batch" |
  | IPC error | `upstream_failed` "the answer broke off: …" |
  | Bad end metadata | `invalid_document` |
  | The end line carries a problem | that problem |
  | `end.Rows != rows` | `upstream_failed` "the end line counts %d rows; %d arrived" |
  | A batch after the final batch | `invalid_document` |

- `handoffAnswer` (165-266):
  - It refuses a format other than `snowflake-arrow/1`, with `invalid_document`.
  - It reads the chunks with `snowarrow.NewReader(ctx, mem, http, chunks, schema, parallel)`.
  - On `snowarrow.ErrLinkExpired` it renews, **at most 3 times** (`a.renewed == 3`, 225). It reposts the same request with `Atlas-Profile-Expect` and `Atlas-Renew: <query_id>` (243-256), and resumes at the failed chunk: `a.base + a.rd.FailedChunk()`, reopened with `h.Chunks[from-1:]` (200-210, 229-237).
  - Any other download error is `upstream_failed` "downloading the result: …".

**`table_test.go`** (16-52) is `TestAnArrowAnswerIsCompleteOnlyWithItsFinalBatch`. Its cases:

| Case | Expected code |
|---|---|
| complete | none |
| no final batch | `upstream_failed` |
| rows lost (end says 4, 3 arrived) | `upstream_failed` |
| a problem after the rows | `deadline_exceeded` |

It uses a checked allocator: `mem.AssertSize(t, 0)`.

**How it maps to the new protocol.**

| Pass 1 | Pass 2 (as built in `sdk/go/protocol`) | |
|---|---|---|
| `protocol.Version = "atlas/0.1"`; the client accepts `atlas/0.` | `Version = "atlas/1.0"` (pass-2:sdk/go/protocol/protocol.go:16). The bootstrap has `protocols` and `minimum_protocol` (refs:schemas/bootstrap.schema.json, required: `protocols, minimum_protocol, environment, servers, builtins, sign_in, catalog_etag`) | REWRITE the version check |
| Bootstrap `{protocol, environment, servers, builtins{"atlas.whoami@v1:fetch": Binding}, sign_in{issuer, audience, dev_token}}` (pass-1:sdk/go/protocol/documents.go:10-29, 33-38) | `builtins` keyed by **id** (pattern `^atlas(\.[a-z](-?[a-z0-9])*)+$`), each `{version, interactions{<name>: {bindings[]}}}`. `sign_in{authority, client_id, scopes, token_endpoint, authorization_endpoint?, device_authorization_endpoint?}` (refs:schemas/bootstrap.schema.json `$defs`) | REWRITE |
| `Atlas-Client` written by `fmt.Sprintf(%q)` | `protocol.Client.Format()` and `ParseClient`, as a Structured Field (pass-2:sdk/go/protocol/headers.go:66-108) | ADAPT |
| `Features.String()` by string joining | `Features{Levels, Handoff, Lens, Deliveries, Encodings, Traits}.Format()` as an SF dictionary of inner lists of tokens. `CoreFeatures()` = levels `core`, deliveries `single operation`, encodings `json` (pass-2:sdk/go/protocol/headers.go:122-183) | ADAPT. The first build declares no `handoff` level (DEFERRED) |
| `FormatDeadline` writes `"@"+unix` | `protocol.FormatDeadline` and `ParseDeadline` (pass-2:sdk/go/protocol/headers.go:225-231) | ADAPT |
| `Atlas-Profile` read as a raw string | An SF String: `FormatExact` and `ParseExactHeader` (pass-2:sdk/go/protocol/headers.go:212-215) | ADAPT |
| `Problem{Type:"about:blank", …}` (pass-1:sdk/go/protocol/problem.go:103) | `Problem` adds `instance`, `hop`, `estimate{amount, unit}`; `Type = "https://atlas.jhg.com/problems/" + code` (pass-2:sdk/go/protocol/problem.go:15-30, 94, 158-166) | REWRITE |
| `arrowAnswer` | `frame.ArrowReader` (pass-2:sdk/go/protocol/frame/frame.go:377-457). Its end line requires `$atlas, rows, truncated, usage, problem` (37). **A row-count mismatch is `invalid_document`** (444-446), not pass 1's `upstream_failed` | Use `frame`. AMBIGUOUS: see the ambiguities |
| NDJSON end `{"$atlas":"end","rows","truncated","problem"?}` (pass-1:sdk/go/protocol/documents.go:100-105) | `frame.End{Rows, Truncated, Usage, Problem}`. Every member is always written: `usage` is `{}` when unmeasured and `problem` is `null` (pass-2:sdk/go/protocol/frame/frame.go:25-34, 53-85) | Use `frame` |
| The lock is `"id@vN" = "id@vN#fp"` | `lock.schema.json`: `capabilities{<id>: {version (int ≥1), fingerprint ^[0-9a-f]{16}$, dynamic?}}`, plus `extras` (refs:schemas/lock.schema.json) | REWRITE (CRITIQUE #8). **OVERRIDDEN BY REVIEW (R02)**: "A lock pins semantic contracts. Routing is fetched as current metadata" (REVIEW.md:60). The lock's format is still open (REVIEW.md:98) |
| Handoff document `{kind, format, profile, rows, source{row_type}, first_chunk (bytes), chunks[{url, rows, bytes}], headers, expires, query_id}` (pass-1:sdk/go/protocol/documents.go:110-136) | `handoff.schema.json`: required `profile, format, source_schema, first_chunk, chunks, expires, operation`, plus `rows`. `first_chunk` is null or an inline chunk; `operation` matches `^operations/[a-z0-9][a-z0-9-]*$` | DEFERRED (R10, step 5) |
| Resolution by any lens chain (`BuildChain`, `ResolveBatch`) | `protocol/resolve` is an **experiment** (REVIEW.md:90) | **OVERRIDDEN BY REVIEW (R03, R04)**: the client applies only aliases, defaults, UNKNOWN and projection |

**Defects.**
- From CRITIQUE (pass-2:docs/pass-2/CRITIQUE.md):
  - #30: package `client`, not `atlas`.
  - #31: no environment order, `ATLAS_HOME`, token store, sign-in, disk profile cache, confirmation, deprecation hook, landing, transfers, export, events or wrappers.
  - #7: the address grammar.
  - #8: the lock format.
  - #3: no I-JSON (`encoding/json` everywhere).
  - #4: headers parsed by splitting strings.
  - #5: the validator is a subset.
  - #6: resolution misses steps.
  - #9: the documents miss members.
  - #10: hand-written document types.
- My own reading:
  1. The call algorithm (§9.7.4) is missing several steps. Retries: none, though spec:09-7-clients.md:124 asks for 3. No `Idempotency-Key`, no `traceparent` (09-7:93), no critical-trait check (09-7:89), no `Deprecation` warning (09-7:97).
  2. The deadline is a fixed 30 s, not the interaction's `deadline.default` capped at `deadline.max` (spec:09-7-clients.md:93).
  3. It always uses `Bindings[0]`, not "the first … whose protocol, encodings and delivery the client supports" (spec:09-7-clients.md:92).
  4. Without a lock, `reader()` describes on every `Fetch`, so there are two round trips per call. §9.7.4 step 2 says "the newest cached profile of that version".
  5. `Connect` calls `os.Getenv` inside the library. AGENTS.md:42 reads settings only in `cmd/`, a `config`/`settings` package or a `settings.go`.
  6. `&http.Client{}` has no timeout (client.go:79). It is bounded only by the context.
  7. `Call` returns the body unvalidated. `readProblem` does not validate.
  8. `arrowAnswer.Retain()` is a no-op, which is a reference-count hazard (table.go:117).
  9. The `Save` mode is `0o644`. Not wrong for a lock, but recorded.
  10. Projection reads a request member named `columns` (client.go:300, `columnNames(req["columns"])`) without checking the `Projectable` trait.
- **Verdict: REWRITE as package `atlas` at the root of `sdk/go`** (CRITIQUE #30 fix; spec:09-7-clients.md:42 `atlas.Connect(ctx, atlas.Options{URL, Env, Token})`). Concrete steps:
  1. Create `sdk/go/atlas.go` (package `atlas`) with `Connect`, `Whoami` (the spec spells it `.Whoami(ctx)`, 09-7:43), `Catalog`, `Search`, `Describe`, `Guide`, `Explain`, `Access` and `Call`. Data's `Fetch` is a shorthand for `Call(id:fetch)` (09-7:28).
  2. Port `FindLock`'s walk to the parents (lock.go:45-59) unchanged, but parse into the `lock.schema.json` shape (TOML, then validate).
  3. Port `Describe`'s fingerprint recheck (client.go:209-212) onto `protocol.Fingerprint`, which is JCS (pass-2:sdk/go/protocol/fingerprint.go).
  4. Port the resolving reader as a wrapper that checks each batch against the writer's derived Arrow schema (`arrowschema`) and applies only alias, default, UNKNOWN and projection.
  5. Replace `arrowAnswer` with `frame.ArrowReader`. Port `TestAnArrowAnswerIsCompleteOnlyWithItsFinalBatch` as a test of it. COL-002 is the matching scenario (refs:scenarios/COL-002.json).
  6. Keep `handoffAnswer` and the renewal logic as a deferred design for step 5 (DEFERRED).
  7. Keep the rule that built-ins carry no `Atlas-Profile-Expect`.
  8. Keep "the client follows the binding and never builds a path" (`server.URL + b.Address`). It is scenario CORE-CALL-003 (spec:09-8-conformance.md:98).

### 1.2 `sdk/go/engine` and `sdk/go/cmd/atlas-engine`

The runner protocol is set out in full in §2. Here, the summary.

**What they do.**
- `engine.go` is "atlas-engine's core: the runner protocol of §9.8.3 over the Go client, with the two additions the Python face needs (§5.1): Arrow results on a second pipe, and cancel" (pass-1:sdk/go/engine/engine.go:1-12).
- `main.go` wires stdin, stdout and an optional results file descriptor. It ignores SIGINT, and exits when it is orphaned (pass-1:sdk/go/cmd/atlas-engine/main.go:1-52).

**Mapping.**
- The command and answer shapes differ from §9.8.3. This is CRITIQUE #29, a Blocker: "`result`, not `value`; no `warnings` or `calls`; options outside `args`".
- The operations must be §9.7.1's. The journey needs `connect`, `whoami`, `catalog`, `search`, `describe`, `guide`, `explain`, `access`, `call`, `fetch` and `version` (spec:09-8-conformance.md:53; refs:QUESTIONS.md:118, Q-RUN-1 adds `guide` and `explain`).
- Pass 1's `lock` op is not a §9.7.1 operation. It is the CLI's `atlas lock` (09-7:144). AMBIGUOUS: keep it as an engine-only op for Python's `lock()`, or move it into the CLI.

**Defects (my own reading).**
1. Bare `go` statements:
   - engine.go:112, `go func()`;
   - main.go:35, `go endWhenOrphaned()`.

   AGENTS.md:41 allows a bare `go` only inside `sdk/go/kit`; everything else goes through `kit/conc.Go`. `kit/conc` provides `Go(fn func() error) (wait func() error)`, `Group`, `WithContext`, `SetLimit` and `TryGo` (pass-2:sdk/go/kit/conc/conc.go:39-97).
2. `time.Tick` in `endWhenOrphaned` (main.go:47) is a ticker never stopped. Harmless, since the process exits, but not clock-injected.
3. The queue holds 64 jobs (engine.go:110). When 64 commands are queued, the stdin scanner blocks on `queue <- job`, so **a `cancel` line cannot be read until a slot frees**.
4. A `cancel` of an unknown or finished id is silently ignored, and no cancel ever gets an answer line (engine.go:126-133).
5. A line that is not JSON is answered with `id: 0` (engine.go:122-124). The id cannot be known, so this is AMBIGUOUS.
6. The JSON form of `fetch` builds every row in memory (`rows = append`, engine.go:293-310), which is unbounded. AGENTS.md:45 forbids unbounded buffers. R11 and ATLAS-CORE §6 say large tables are iterators.
7. `timeout` is float seconds (engine.go:53, 155-158). §9.8.3 uses `options.deadline_ms` (spec:09-8-conformance.md:42).
8. `connect` reads the lock from the working directory with `os.Getwd()` (engine.go:259). That is acceptable for a runner, since §9.7.9 says "the current directory or the nearest parent".
9. Windows has no orphan detection. pass-1:PROOF-0.md:86-88 says "Windows needs a job object (not done)".

**Verdict: ADAPT.** Concrete steps are in §2.5.

### 1.3 `clients/python`: the Python face (`fiatlas`)

**Package** (pass-1:clients/python/pyproject.toml:1-22):

| Field | Value |
|---|---|
| Distribution name | Current local name: `fiatlas` (see `clients/python/pyproject.toml`); the remaining rows describe the historical pass-1 packaging. |
| `version` | `"0.1.0"` |
| `requires-python` | `">=3.11"` |
| `dependencies` | `[]` |
| Optional extras | `arrow = ["pyarrow>=16"]`; `pandas = ["pyarrow>=16", "pandas>=2.2"]`; `test = ["pyarrow>=16", "pandas>=2.2", "pytest>=8"]` |
| Build | `hatchling`, wheel `packages = ["fiatlas"]` |
| pytest | `testpaths = ["tests"]`, `pythonpath = ["."]` |

The import is `fiatlas`. Compared with the packaging goals in §5.1, pass 1 has **no `[polars]` or `[airflow]` extra** and **no per-platform wheel with `bin/atlas-engine`**. The current distribution is also named `fiatlas`; consult its local package declaration before building or installing it.

**Public API** (pass-1:clients/python/fiatlas/):
- `__init__.py:9-12` exports `FIAtlas`, `Table`, `AtlasError` and `EngineError`.
- `FIAtlas(url=None, token=None, *, lock=None, engine=None)` (client.py:61-65).
  - It stores `{"url": url or "", "token": token or "", "lock": fspath(lock) or ""}` as the connect arguments.
  - `.connected` is the connect result.
  - **The spec's signature is `FIAtlas(url=None, env=None, token=None)`** (spec:09-7-clients.md:42). Pass 1 has no `env`.
- Methods:

  | Method | Engine call | Line |
  |---|---|---|
  | `fetch(address, request=None, *, execution=None, timeout=None) -> Table` | op `fetch`, `results="arrow"` | client.py:81-89 |
  | `call(address, request=None, *, timeout=None)` | op `call`, returns `result` | 91-93 |
  | `describe(id)` | `describe` | 95-96 |
  | `search(words)` | `search` | 98-99 |
  | `catalog()` | `catalog` | 101-102 |
  | `whoami()` | `whoami` | 104-105 |
  | `lock(ids, path="atlas.lock")` | `lock` | 107-111 |
  | `close()`, `__enter__`, `__exit__` | — | 113-122 |

- `Table(arrow, *, writer, reader, via)` (client.py:11-49) has `.columns`, `__len__` and `__repr__`.
  - `read_as(kind="arrow")` accepts `"arrow"` or `pyarrow.lib.Table`, which return `self.arrow`.
  - It accepts `"pandas"`, `pandas.DataFrame` or `pandas.core.frame.DataFrame`, which return `to_pandas(types_mapper=pd.ArrowDtype)`.
  - It accepts `list` or `"records"`, which return `to_pylist()`. Anything else is `ValueError`.
- `AtlasError(problem: dict)` has `.problem`, `.code` (default `"internal"`), `.status`, `.retryable` and `.detail` (= `detail` or `title` or `""`). Its message is `"code: detail"` (errors.py:8-17).
- `EngineError(RuntimeError)`: "atlas-engine failed, or could not be started. The next call starts a new one." (errors.py:20-21).

How the face drives the engine is in §2.3.

**Defects.**
- From CRITIQUE #33, the face lacks:
  - `AtlasProblem` per code;
  - refs;
  - `read_as` for polars and DuckDB;
  - async;
  - operations;
  - subscribe and publish;
  - export and upload;
  - the attribute form;
  - wheels and a cross-compiled engine.
- My own reading:
  1. The exception is named `AtlasError`, but the spec says `AtlasProblem` (spec:09-7-clients.md:113).
  2. `fetch` materialises the whole table (`pa.Table.from_batches`, _engine.py:217). spec:05-clients.md:261 says "Memory stays bounded unless the caller asks for the whole result at once", and review:ATLAS-CORE.md:138 says "Large tables are iterators or batch readers by default".
  3. `fetch` returns an eager `Table`. R11 asks for one execution that yields a result handle, and reading it again must never re-execute (review:DESIGN-REVIEW.md:122). An eager `Table` meets "executes once". `ref.read_as` in §5.1 looks lazy (spec:05-clients.md:248). **OVERRIDDEN BY REVIEW (R11)**: "a call executes once and yields a result/operation handle; reading a result does not silently execute again" (review:DESIGN-REVIEW.md:122).
  4. Only one call runs at a time per process. The `with self._lock` covers the whole call (_engine.py:120), so threads serialise.
  5. The engine inherits the whole `os.environ` (_engine.py:94), so `ATLAS_TOKEN` reaches it. That is fine by §9.7.3.
  6. The token travels in the `connect` arguments over the pipe (client.py:62).

**Verdict: ADAPT.**
1. Keep `_engine.py` almost verbatim (§2.3). Change only the line shapes (`value`, options inside `args`, `deadline_ms`) and the cancel answer.
2. Rename `AtlasError` to `AtlasProblem`, with a subclass per registry code and an attribute per problem member (spec:09-7-clients.md:113).
3. Add `env` to `FIAtlas`.
4. Make `call` the canonical operation, returning a result handle (R11). `fetch` becomes the data shorthand.
5. `read_as` keeps pyarrow and pandas with `types_mapper=pd.ArrowDtype`. polars and DuckDB are AMBIGUOUS for the first build (see the ambiguities).
6. Add `guide`, `explain` and `access` (spec:09-7-clients.md:47-49).

### 1.4 `clients/python/tests`: the 16 Python tests

PROOF-0.md:6 counts 16. They break down as 9 in `test_engine_process`, 2 in `test_evolution` and 5 in `test_tables`.

| Test | What it proves | Spec id | Verdict |
|---|---|---|---|
| `test_ctrl_c_ends_a_call_within_a_second_and_the_engine_carries_on[before-the-answer / mid-stream / mid-download]` | It sends SIGINT to the main thread after 0.1 s (gateway), 0.8 s (gateway) or 0.8 s (handoff), and expects `KeyboardInterrupt` within 1.0 s. Then `whoami` answers in under 0.5 s **from the same engine pid**, and a fresh fetch works (pass-1:clients/python/tests/test_engine_process.py:37-57). Its slow gateway flags are `-chunk-rows 2 -chunk-latency 300ms` (22). | ATLAS-PY-005 | **BRING BACK** the gateway cases. The `mid-download` case is DEFERRED with handoff. The slow fixture needs a replacement, since the chunk-latency flag belonged to snowfake storage. A fake-warehouse delay hook would do; the AGENTS.md:56 clock-injection rule applies to expiry waits, and this is a latency fixture |
| `test_a_deadline_ends_a_call_with_deadline_exceeded[gateway / handoff]` | `timeout=0.5` raises `AtlasError` with code `deadline_exceeded` in under 1.5 s, and `whoami` still works (60-69) | ATLAS-PY-005 | **BRING BACK** `gateway`. `handoff` is DEFERRED |
| `test_a_forked_child_starts_its_own_engine_and_leaves_its_parents_alone` | After `os.fork`, the child fetches with a new engine pid and the same row count. The parent's engine is unchanged (72-92) | ATLAS-PY-004 | **BRING BACK** |
| `test_each_process_of_a_fork_pool_gets_an_engine_of_its_own` | A module-global client made before `multiprocessing.get_context("fork").Pool(2)`, with 6 tasks of `chunksize=1`. There is one engine per worker, never the parent's (95-112) | ATLAS-PY-004 | **BRING BACK**. The spec also names `spawn` and `forkserver`, Celery and gunicorn `--preload` (spec:05-clients.md:210), which are untested |
| `test_no_engine_outlives_the_python_process_that_started_it[idle / mid-call]` | A subprocess Python prints the engine pid and then `SIGKILL`s itself. The engine must be gone within 2.0 s, polled with `os.kill(pid, 0)` every 0.05 s (115-141) | ATLAS-PY-007 | **BRING BACK** |
| `test_a_locked_application_keeps_its_dataframe_while_the_contract_evolves` | Built against revision 1 (`-pin portfolio.holdings@v1=1`). After a redeploy, the locked application reads the same columns `V1 = [portfolio, security, as_of, mv, weight, oas, sector]`, the same dtypes and values (oas within `rel_tol=1e-9`), and `UNKNOWN` for Supranational, streamed or handed off. An old request with `columns: [mv, oas]` returns `["mv", "oas"]` (pass-1:clients/python/tests/test_evolution.py:15-57) | §3, §9.5 | **REWRITE**. It relies on `convert_unit` (oas bp → fraction) within v1. **OVERRIDDEN BY REVIEW (R03)**: "a unit change … is a new major version" (REVIEW.md:61). Keep: the rename through an alias (`mv` → `market_value`; the reference profile has `x-atlas-aliases: ["mv"]`, refs:vectors/profiles/data.json:264-270), a new enum value read as `UNKNOWN`, an added optional field, and the old request's columns |
| `test_a_capability_added_later_is_callable_with_no_client_change` | With `-pin index.constituents@v1=0`, search does not find it. After a redeploy, search finds it first, and `fetch(id+"@v1:fetch", {"indices": ["H0A0"]})` returns only `H0A0` rows (64-72) | ATLAS-CORE-009 | **ADAPT** to `portfolio.holdings-dates@v1` (REVIEW.md:42), from all three clients (REVIEW.md:49) |
| `test_every_column_keeps_the_type_its_profile_declares` | `market_value` is `pa.decimal128(18, 2)`, `weight` `decimal128(9, 6)`, `as_of` `date32`, `oas` `float64`, `portfolio` `string`. The pandas dtype is `"decimal128(18, 2)[pyarrow]"`, and the value is a `Decimal` with exponent -2 (pass-1:clients/python/tests/test_tables.py:18-33) | ATLAS-PY-002 | **ADAPT** to the new Arrow derivation. In pass 2, `Money` is `struct<amount: decimal128(38, 6), currency: string>` (pass-2:sdk/go/protocol/arrowschema/arrowschema.go:26-29), and the reference holdings profile's `market_value` is `x-atlas-logical: Money` (refs:vectors/profiles/data.json:264-267) |
| `test_streamed_and_handed_off_tables_are_identical[7 / 10000]` | The streamed and handed-off `pa.Table` are equal (36-43) | ATLAS-CORE-005 | DEFERRED with handoff. Keep the idea for "NDJSON = Arrow = JSON form" |
| `test_the_json_form_and_the_arrow_form_carry_the_same_values` | Through the raw `Engine`, the JSON-form rows equal the Arrow rows converted: decimals as `format(v, "f")`, dates by `isoformat()` (46-68) | §9.8.3 | **BRING BACK**. It proves the engine's two forms agree |
| `test_problems_arrive_as_exceptions_with_their_codes_and_the_engine_carries_on` | The outsider gets `not_granted`. `{"portfolios": "147HYC"}` gives `invalid_request` ("refused before it is sent"). `portfolio.missing@v1:fetch` gives `not_found`. The same engine pid follows (71-87) | §9.7.6 | **BRING BACK**. The rename is `AtlasProblem` |

**Fixtures** (pass-1:clients/python/tests/conftest.py):
- `HOLDINGS = "portfolio.holdings@v1:fetch"` (20) and `TWO = {"portfolios": ["147HYC", "200IG"]}` (21).
- `binaries` builds `gateway` (`./cmd/gateway`) and `atlas-engine` (`./cmd/atlas-engine`) with `go -C <module> build -o`, then sets `ATLAS_ENGINE` (24-30).
- `gateway(*args)` starts `[gateway, "-addr", "127.0.0.1:0", *args]` and reads one stdout line as JSON. `["gateway"]` is the URL (51-70).
- `Gateway.token(persona)` POSTs `{"persona": …}` to `/.dev/token` and takes `["access_token"]` (38-43).
- `Gateway.atlas(persona="pm", **kw)` (45-48).
- Teardown closes the clients, then `kill()`s and `wait()`s each gateway (66-70).

### 1.5 `gateway/adapter/snowflake` (`snowflake.go`, `plan.go`, `snowflake_test.go`)

**What it does.** pass-1:gateway/adapter/snowflake/snowflake.go:1-9 says it "plans every capability's read as one statement with the contract written in, runs it through a Warehouse, streams the result cast to the writer schema, hands off its chunks, and settles from the query history".

**API** (snowflake.go):

| Item | Definition | Line |
|---|---|---|
| `Warehouse` interface | `Execute(ctx, Statement) (*QueryResult, error)`; `Result(ctx, queryID)`; `Estimate(ctx, Statement) (rows, bytes int64, err)`; `History(ctx, queryID) (QueryRecord, error)` | 25-35 |
| `Statement` | `{SQL, Tag string; Select Select}` | 39-43 |
| `QueryResult` | `{QueryID; RowType snowarrow.RowType; First []byte; Chunks []protocol.Chunk; Headers map[string]string; Expires time.Time; Rows int64}` | 46-54 |
| `QueryRecord` | `{QueryID, Tag, SQL; Rows, Bytes int64; Started, Ended time.Time}` | 57-65 |
| `Source` | Implements `ports.DataSource`; `New(w, client, mem)` sets `prefetch: 4` | 68-80 |
| Methods | `Capabilities()` = `{Handoff: [snowflake-arrow/1]}`; `Plan`; `Explain`; `Execute` (a `snowarrow.NewReader` over the chunks, cast to the writer schema); `Handoff`; `Renew` (`warehouse.Result`, no rerun); `Settle` (`History`, giving `Usage{QueryID, Tag, Rows, Bytes}`) | 82-162 |

**Planning** (plan.go):
- `Select{From; Columns []Projection; Where []ports.Filter; OrderBy []string; Limit int}` (15-21).
- `Projection{Source; Divide int64; Alias; Cast snowarrow.Column; Enum []string}` (26-32).
- `castFor` (37-58):

  | Contract type | Snowflake type |
  |---|---|
  | string | `text` |
  | integer | `fixed`, precision 18 |
  | number | `real` |
  | decimal | `fixed`, with its precision and scale |
  | boolean | `boolean` |
  | date | `date` |
  | date-time | `timestamp_tz`, scale 6 |
  | anything else | error |

- `Render(tag)` (81-140) writes:
  - `CAST(<expr> AS <type>) AS "<alias>"`, where `expr` may be `SRC / <n>`;
  - for an enum, `CASE WHEN SRC IN ('a', …) THEN SRC ELSE 'UNKNOWN' END`;
  - `FROM <table>`;
  - `WHERE` with `=`, `IN (…)`, `BETWEEN … AND …`, and latest as `COL = (SELECT MAX(COL) FROM <table>)`;
  - `ORDER BY "<alias>"` (quoted with Go's `%q`);
  - `LIMIT n`;
  - **`/* QUERY_TAG=<tag> */` appended** (136-138).
- `sqlType`: `NUMBER(p,s)`, `FLOAT`, `VARCHAR`, `BOOLEAN`, `DATE`, `TIMESTAMP_TZ(s)` (142-158).
- `ident` accepts only `[A-Z0-9_.]`, else **it panics** (162-169).
- `literal`: strings are single-quoted with `''` escaping; `time.Time` becomes `'YYYY-MM-DD'::DATE`; ints and floats use `fmt.Sprint` (171-181).

**Tests** (snowflake_test.go):
- The fixture's clock starts at `2026-10-01 09:00:00 UTC` (69) and `ChunkRows = 7` (71).
- `TestSnowflakeSourcePassesTheDataSourceSuite` (78-84).
- `TestPlanWritesTheContractIntoTheStatement` (86-104) expects these substrings:
  - `CAST(MARKET_VALUE AS NUMBER(18,2)) AS "market_value"`
  - `CAST(OAS_BP / 10000 AS FLOAT) AS "oas"`
  - `CASE WHEN SECTOR IN ('Industrials', 'Financials', 'Utilities', 'Energy', 'Supranational') THEN SECTOR ELSE 'UNKNOWN' END`
  - `PORTFOLIO_CODE IN ('147HYC', '200IG')`
  - `AS_OF_DATE = (SELECT MAX(AS_OF_DATE) FROM HARBOUR.PORTFOLIO_HOLDINGS)`
  - `/* QUERY_TAG=req-1 */`
- `TestExpiredLinksAreRefusedAndRenewed` (106-132): with no headers, `ErrLinkExpired`. At +20 min, past the 15-minute links, expired. Renewal works, and the download count rises by exactly 1, so the query was not rerun.
- `TestOlderContractsSeeUnknownForNewEnumValues` (134-157).

**Defects.**
- From CRITIQUE: #13 (`QUERY_TAG` in the SQL text, so `explain` ≠ `fetch`), #14 (string SQL, home-made quoting, no guard), #25 (the fake evaluates in Go).
- My own reading:
  1. `ident` panics in the request path.
  2. The latest-date filter takes the **whole table's** `MAX(AS_OF_DATE)`, not each portfolio's. The reference profile's `as_of` is "the portfolio's last valuation on or before as_of" (refs:vectors/profiles/data.json:356).
  3. `ORDER BY` uses Go `%q` quoting.
  4. `TIMESTAMP_TZ(6)`, but §2.2.1.5 says "every timestamp converted to UTC" (spec:02-2-1-snowflake-data.md, "The contract in the planned SQL").
- **Verdict: REWRITE.** The pass-2 implementation already has `gateway/adapter/snowflake/compile.go` (289 lines), `guard.go` (246) and `warehouse.go` (219) with sqlglot, and `gateway/local/duckwh` (85). Bring back as tests on `compile.go`'s output:
  - the `TestPlanWritesTheContractIntoTheStatement` assertions, minus `QUERY_TAG`, which moves out of band (CRITIQUE #13);
  - the enum-to-UNKNOWN `CASE`;
  - `explain` showing byte for byte the statement that runs (ATLAS-DATA-004; scenario CORE-EXPLAIN-001, spec:09-8-conformance.md:95).

  Keep `QueryRecord` and settlement by query id as the shape of a future `Settle` (step 3, R09). `Renew` and `Result` are DEFERRED.

### 1.6 `gateway/adapter/snowflake/snowfake`

Detailed in §4. **Verdict:**
- the Harbour generator: ADAPT (IN SCOPE);
- the Go evaluator: DROP (CRITIQUE #25);
- the result storage and links: DEFERRED (handoff).

The pass-2 implementation's `gateway/fakes/warehouse.go` (328 lines) already holds tables in memory with "SQL's three-valued comparisons, nulls last, the latest of each group chosen before the query's own conditions" (pass-2:gateway/fakes/warehouse.go:24-27). The Harbour generator can feed it, or DuckDB.

### 1.7 `gateway/svc/harbour` (`harbour.go`, `profiles.go`)

**What it does.** "serves the portfolio and index capabilities from Harbour's views in Snowflake, through the data-source port … Each capability declares every profile it has had within its version, in order, with the lens from each to the next; the gateway serves the last as current" (pass-1:gateway/svc/harbour/harbour.go:1-6).

**API** (harbour.go):
- `New(source ports.DataSource, key ed25519.PublicKey)` declares `holdings()` and `constituents()` (40-49).
- `Domains()` = `["portfolio", "index"]` (53).
- `declare` chains `Evolution.Previous` to the previous exact id, and maps each exact id to its source columns (67-85).
- `Serve(ctx, call, req)` (87-138):
  1. `ports.VerifyCall(key, call, call.Address, now)` checks the Ed25519 signature, the address and the deadline.
  2. It finds the capability; a missing one is `not_found`. It finds the revision's source columns for `call.Writer`.
  3. If `call.Renew` is set, it goes to `renew`.
  4. It projects the writer to `columns`. Key columns become `OrderBy`. It builds the filters, then `Plan`.
  5. `explain` goes to `Explain`. `fetch` goes to `Handoff` when `call.Execution == "handoff"`, else to `Execute`.
- `renew` (142-149): `Settle(queryID)`; the tag must end with `"/"+subjectTag(subject)`, else `not_found` "no result <id> of yours to renew".
- `tag(call)` = `RequestID + "/" + hex(sha256(subject)[:6])` (156-161).
- `asOf` (179-189): a missing `as_of` gives the `OpLatest` filter. A non-date is `invalid_request` "as_of must be a date", target `/as_of`.

**Profiles** (profiles.go):
- `requestFor(listName, reference)` (16-37):

  ```json
  {"type":"object","additionalProperties":false,"required":["<listName>"],
   "properties":{
     "<listName>":{"type":"array","minItems":1,"reference":"<reference>","description":"Which to read, by code.","items":{"type":"string"}},
     "as_of":{"type":"string","format":"date","description":"The date to read; the latest date with data when omitted."},
     "columns":{"type":"array","description":"The columns to return; every column when omitted.","items":{"type":"string"}}}}
  ```

  This is pass 1's custom `protocol.Schema`, not Atlas Schema; `Reference` corresponds to `x-atlas-reference`.
- `dataProfile` (39-57):
  - `Version: 1`, `Owner: "QFI"`, `Family: data`, `Stability: "official"`, `Lifecycle: "stable"`, `Visibility: "listed"`.
  - Traits: `Projectable, Filterable, AsOf, Keyed, Handoff`.
  - Interactions: `fetch {Effect "read", Result "table", Delivery "chunked", Executions [gateway, handoff]}` and `explain {Effect "read", Result "record", Delivery "single", Executions [gateway]}`.
  - Bindings: `{Protocol "https", Server "gateway", Method "POST", Address "/v1/data/<id>@v1:<interaction>", Encodings [application/vnd.apache.arrow.stream, application/x-ndjson]}`.
- `holdings()` (66-123). `meaning`:

  ```json
  {"grain": "portfolio × security × date",
   "as_of": "the portfolio's end-of-day holdings on as_of; the latest date with data when omitted",
   "source": "Harbour's portfolio holdings view"}
  ```

  - Revision 1 has seven columns:
    - `portfolio` (string, key), `security` (string, key, "The security's ISIN."), `as_of` (date, key);
    - `mv` (decimal 18,2, unit `currency_unit`);
    - `weight` (decimal 9,6, logical `Fraction`);
    - `oas` (number, nullable, logical `Bp`);
    - `sector` (string, enum `Industrials, Financials, Utilities, Energy, UNKNOWN`).
  - Revision 2 renames `mv` to `market_value`, with alias `mv`. Lens `rename mv→market_value`; changelog "mv is now market_value.".
  - Revision 3 converts `oas` to logical `Fraction`, with source `OAS_BP` and `Divide: 10_000`. Lens `convert_unit oas Bp→Fraction`. It adds `Supranational` to the sector enum and a `currency` column (string, nullable, logical `Currency`). Changelog "oas is a fraction; Supranational is a sector; currency is new.".
- `constituents()` (126-147): `index.constituents@v1`, with columns `index`, `security`, `as_of` (the keys) and `weight` (decimal 9,6, `Fraction`). Its request uses `indices` with reference `index.list@v1`.

**Defects.**
- CRITIQUE #1 (the custom model), #12 (the name parsed from the URL path: the binding addresses embed the capability name), #24.
- My own reading:
  1. Profiles are Go values. AGENTS.md:62 asks for `profiles/<id>@v<N>.json` in the service's package.
  2. The journey's capabilities are missing: `portfolio.list@v1`, `portfolio.search@v1`, `data.status@v1` and `portfolio.holdings-dates@v1` (REVIEW.md:38-42).
  3. There is no scope enforcement. **R05 requires it from the first shared-credential source** (REVIEW.md:63).
  4. Revision 3 breaks R03.
  5. The column `security` differs from the reference profile's `isin` (refs:vectors/profiles/data.json:242).
- **Verdict: REWRITE.** Keep the separation of "the contract" from "the mapping to source columns per revision" ("that mapping is not part of the contract and may change freely", harbour.go:5-6). It becomes the provider's mapping. Drop the Ed25519 `VerifyCall` per call: R20 keeps providers in process with serialisable boundaries (REVIEW.md:78), and the pass-2 implementation's `ports/provider.go` defines the boundary.

### 1.8 `gateway/core` (`builtins`, `catalog`, `governance`, `pipeline`)

**catalog** (pass-1:gateway/core/catalog/catalog.go):
- `Entry{Exact; Profile; Service}` (18-22).
- `Snapshot{Governance; Revision; byExact; current}` (25-30).
- `Build(services, pins, gov)` (38-83):
  - It admits each profile.
  - It groups profiles by pinned id, in declaration order.
  - `pins[k]` truncates the chain to its first k; `0` leaves the capability out.
  - It checks that `Previous()` equals the previous exact id, and refuses duplicates.
- `admit` (87-108): `ValidID`; not built-in (`only the gateway serves atlas.*`); the domain must be in `svc.Domains()` and equal `Domain(p.ID)` (ATLAS-GATEWAY-007); the result maps to Arrow; every interaction has a binding (ATLAS-BINDING-001).
- `Source()` resolves an exact id from the snapshot, else `resolution_failed` (123-130).

**governance** (pass-1:gateway/core/catalog/governance.go):
- `Governance{Revision; Roles}` (16-19).
- `Role{Name; Members []string (principals or group ids); Grants; Denies; RatePerMinute}` (23-32). Grants name "a domain (portfolio), a capability version (portfolio.holdings@v1) or one interaction (portfolio.holdings@v1:fetch)".
- `Principal{Subject, Name, Groups}` (35-39).
- `Decide(p, addr, effect)` (64-85): **a deny always wins** (`denied`). Each grant that reaches the address adds the role to `HeldBy`. `Rate` is the maximum `RatePerMinute` over those roles. No grant gives `not_granted`.
- `reaches` (110-119): a pattern containing `:` must equal the address; one containing `@v` must equal the pinned id; otherwise it is a domain, which **reaches only `effect == "read"`** (ATLAS-ACCESS-001, -002).
- `RateWindow = time.Minute` (97).

**builtins** (pass-1:gateway/core/builtins/builtins.go):
- `WhoAmI` returns `{principal, name, environment, roles, available_domains: {domain: count}}` (17-23; pass-1:sdk/go/protocol/documents.go:89-95).
- `Catalog` lists the current entries the caller holds at least one interaction of, skipping `Lifecycle == "retired"`. Each item is `{id, version, exact, family, title, summary, interactions}` (27-40).
- `Search` (57-80) scores each term:
  - 3 × matches in the id tokens;
  - 2 × matches in the title;
  - 1 × matches in the summary;
  - 0.5 × matches in each column's name and description.

  A term matches a word that equals it, or a word it prefixes when the term is longer than 3 characters (`count`, 151-159). Tokens are lower-cased letter and digit runs. Items with score > 0 are sorted by score, descending and stable.
- `Describe(req{id|exact})` (86-123):
  - An exact id is validated (`invalid_request` target `/exact`); a missing one is `not_found`.
  - An id goes through `pinnedOrCurrent`, which allows a **bare id** to mean the highest current version.
  - Neither given: `invalid_request` "name a capability by id or exact" target `/id`.
  - Holding nothing: `not_granted` with `next` "call atlas.access for <pinned> to see who can grant it".
  - The answer is `{exact, current, profile, governance{revision, interactions, held_by}, availability: "available"}`.

**pipeline** (pass-1:gateway/core/pipeline/pipeline.go):
- `DefaultDeadline = 30 * time.Second` (29).
- `Handle` (69-172), in this order:

  | Step | What it does | Lines |
  |---|---|---|
  | 1. authenticate | No token: `unauthenticated` "sign in: the call carries no token". `Verify` failure: `unauthenticated` "the token is not valid for this gateway" | 83-92 |
  | 2. resolve | `ParseAddress` failure is `not_found`. A built-in goes to `builtin()`. An unknown capability or interaction is `not_found` | 94-110 |
  | 3. authorise | `Decide`; a refusal carries `next` "call atlas.access for <pinned> to see how to get it" | 112-117 |
  | 4. validate | `ResolveRequest` forward from `Atlas-Profile-Expect`, `ApplyDefaults`, `Validate` (every failure in `details`), then map `columns` by name or alias | 119-125, 174-218 |
  | 5. limit | `Limits.Reserve(subject+"|"+address, rate, 1m)` | 127-131 |
  | 6. route | `SignCall` with `{Address, Subject, RequestID, Deadline, Writer, Execution, Renew}` | 133-143 |
  | 7. execute | `Service.Serve` | 145-149 |
  | 8. settle and audit | In `Outcome.Finish(rows, err)`: `Limits.Settle` (error ignored), `Service.Settle(queryID)` (handoff rows from the source), then `Audit.Write` (**error ignored**, 169) | 153-170 |

- `refuse` writes the audit record, and fails the call with `internal` "the call could not be audited" if the audit write fails (73-81).
- `chooseExecution` (224-230) picks handoff only if the interaction offers it, the client declares the `handoff` level and a format, and the caller asked for it or is renewing.
- `principal(claims)` (268-277): Subject = `oid`, else `sub`; Name = `preferred_username`; Groups = `Raw["groups"]`.
- `hashRequest` (281-285) is the SHA-256 of the `encoding/json` bytes, first 8 bytes in hex.
- Built-ins (232-266) parse errors as `invalid_request` and are audited.

**Mapping and defects.**
- From CRITIQUE: #15 (per-replica buckets), #16 (no idempotency), #17 (in-memory audit), #18 (no controller, snapshot, LKG), #19, #20, #22, #23, #24 (4 of 20 built-ins).
- Pass 2's order is ATLAS-CORE §8 (review:ATLAS-CORE.md:170-180). After authorisation it adds:
  - "Validate and normalise the request, then enforce its resource and data scope" (step 4);
  - the idempotency lookup (step 5);
  - admission and reservation (step 6);
  - "Record durable admission" (step 7).
- The audit becomes admission, outcome and settlement facts. **OVERRIDDEN BY REVIEW (R09)** (REVIEW.md:67).
- **Verdict: REWRITE.** The pass-2 implementation has `core/access/access.go` (274), `core/registry/registry.go` (232) and `core/pipeline/{limits,policy}.go`. Bring back:
  1. authorisation before validation, and no limit spent on an invalid request (ATLAS-ACCESS-009, -010; the test is in §1.9);
  2. deny wins, and a domain grant reaches only reads;
  3. the working view shows only what the caller holds, and describe refuses `not_granted` with `next`;
  4. the search scoring, as a starting heuristic;
  5. pins, to "change a contract from the back" in tests (`-pin id@vN=k`, pass-1:README.md:70).

### 1.9 `gateway/edge/http` (`edge.go`, `edge_test.go`)

**What it does.** "a thin adapter that turns HTTP requests into pipeline calls and pipeline outcomes into HTTP answers. It decides nothing" (pass-1:gateway/edge/http/edge.go:1-4).

| Item | Value | Line |
|---|---|---|
| `MaxBody` | `1 << 20` | 28 |
| Routes | `GET /.well-known/atlas`; `POST /v1/atlas/{address}`; `POST /v1/data/{address}` | 41-43 |
| Middleware, in order | `Recover`, `logger`, `RequestID("X-Request-Id")`, `telemetry.Propagate(false)`, `AccessLog`, `MaxBytes(1<<20)`, `Deadline("Atlas-Deadline", 30s default, 5*time.Minute max)` | 44-52 |
| Unauthenticated | `WWW-Authenticate: Bearer realm="atlas"` | 88-90 |
| Profile | `Atlas-Profile: <writer exact>` | 94-96 |
| Handoff | `Cache-Control: no-store`; `Finish(0, nil)` before writing | 98-102 |
| Table | `Accept` containing `application/vnd.apache.arrow.stream` (by `strings.Contains`) streams Arrow, else NDJSON | 103-109 |
| NDJSON | One flush per batch. End line `{"$atlas":"end","rows":n,"problem"?}`. **`Finish` runs before the end line** | 119-154 |
| Arrow | IPC writer **`ipc.WithZstd()`**; the final batch via `EndBatch`. On an `EndBatch` error it returns without the final batch | 159-191 |
| `bearer` | `strings.CutPrefix(Authorization, "Bearer ")`, trimmed | 193-199 |

**Tests** (pass-1:gateway/edge/http/edge_test.go). They are worth porting as the acceptance checklist for the pass-2 edge.

- The setup:
  - `ChunkRows = 11` (46);
  - governance `g-test-1` with `fi-pm` (members `fi-pm`, grants `portfolio, index`) and `fi-analysts` (grants `portfolio.holdings@v1:fetch`, `RatePerMinute: 3`) (55-58);
  - issuer `https://issuer.test`, audience `api://atlas-gateway`, personas `pm` {`pm-0001`, [`fi-pm`]}, `analyst` {`analyst-0001`, [`fi-analysts`]}, `outsider` {`outsider-0001`} (63-67);
  - every problem must carry `Atlas-Error-Code` equal to its `code` (107-114).
- The tests:
  - `TestBootstrapNeedsNoSignIn` (118-124).
  - `TestNoTokenIsUnauthenticatedAndAudited` (126-135): 401, `unauthenticated`, `WWW-Authenticate` present, exactly one audit record with `RefusedAt == "authenticate"`.
  - `TestAuthorisationComesBeforeValidation` (139-167, ATLAS-ACCESS-009/010):
    1. The outsider's invalid body `{"portfolio":"147HYC"}` gives `not_granted` with `next`.
    2. The analyst's gives `invalid_request` with a `target`.
    3. Three invalid calls spend nothing: three valid calls then succeed, and the fourth is `limit_exceeded` with `retry_after > 0`.
    4. The analyst gets `not_granted` on `:explain`.
    5. The number of audit records equals the number of calls.
  - `TestStreamedAndHandedOffRowsAreIdentical` (169-220): handoff part DEFERRED. The NDJSON end-line count and the Arrow final batch are IN SCOPE.
  - `TestAnOldRequestIsResolvedForward` (222-236): `Atlas-Profile-Expect` names the first exact id, with `columns: [mv, oas]`. The rows carry `market_value` and not `sector`, so "only the requested columns travel".
  - `TestPinningServesAnEarlierProfile` (238-244).
  - `TestTheWorkingViewShowsOnlyWhatTheCallerCanUse` (246-276):
    - the outsider sees 0 capabilities;
    - the analyst sees `portfolio.holdings` with `["fetch"]` only;
    - the PM sees 2;
    - a search for "portfolio holdings market value" returns holdings first;
    - describe by an earlier exact id gives `Columns[3].Name == "mv"` and `Current != Exact`;
    - describe by the outsider is `not_granted`.

**Defects.**
- From CRITIQUE: #12 (the name is parsed from the path), #9 (no `usage` in the end line), #23, #28 ("`:8000`, header bytes 64 KiB, shutdown sequence 5 s then 30 s").
- My own reading:
  1. `Accept` is matched by substring.
  2. A problem written after the final-batch failure is lost.
  3. The edge carries none of `Atlas-Governance-Revision`, `Atlas-Limit-Remaining`, `Deprecation` or `Sunset` (pass-2:sdk/go/protocol/headers.go:31-37).
- AMBIGUOUS on ZSTD:
  - The spec says "Arrow IPC with ZSTD buffer compression, or NDJSON for Core clients" (spec:02-2-1-snowflake-data.md:129).
  - Pass 2's `frame.NewArrowWriter` sets no compression (pass-2:sdk/go/protocol/frame/frame.go:338).
  - Readings: (a) compress, as §2.2.1.4 says; (b) send uncompressed, for every reader including Node's Arrow JS. Whether Arrow JS reads ZSTD-compressed IPC buffers must be checked, not assumed.
- **Verdict: REWRITE** as a binding router. The request path is looked up in the snapshot's binding table (CRITIQUE #12 fix), and it writes through `protocol/frame`. Port the seven tests above against the journey.

### 1.10 `gateway/e2e/e2e_test.go`

- `TestMain` builds `../cmd/gateway` into a temp dir (28-42).
- `start(t, args...)` runs `[gatewayBin, "-addr", "127.0.0.1:0", args...]`. The first stdout line is JSON, and `ready["gateway"]` is the URL. The cleanup is `Kill` and `Wait` (49-70).
- `token(t, g, persona)` POSTs `/.dev/token` and reads `access_token` (72-84).
- `rows()` gives JSON records through `protocol.RowRecord` (96-114).
- The tests:
  - `TestTheClientReadsTheSameRowsStreamedOrHandedOff` (126-154): `-chunk-rows 13`. It asserts `market_value` is a string containing `.`, so it stays an exact decimal.
  - `TestALockedClientKeepsItsShapeWhileTheContractEvolves` (158-217), with `sameRow` comparing `oas` within `1e-9 × max(1, |a|)` (221-237).
  - `TestACapabilityAddedLaterIsCallableWithNoClientChange` (241-262).
  - `TestExpiredLinksAreRenewedMidRead` (264-292): `-chunk-rows 5 -link-ttl 1s`, `Parallel 1`, **`time.Sleep(2500 * time.Millisecond)`** (278).
  - `TestProblemsReachTheCallerWithTheirCodes` (294-308).

**Defects.** The `time.Sleep` breaks AGENTS.md:56 ("never sleep to wait for an expiry"), but that test is DEFERRED with handoff. Others: the build in `TestMain` panics on failure; `cmd.Process.Kill()` gives no graceful-shutdown check.

**Verdict: ADAPT the style.** CRITIQUE "What survives": "the end-to-end test style: real binaries, with ready lines on stdout". The pass-2 e2e should:
- build the gateway (and the worker if needed);
- start it on `127.0.0.1:0`;
- read the ready line;
- drive it with the Go `atlas` package, the CLI binary, the Python face and Node, with the same request records (REVIEW.md:48).

### 1.11 `gateway/ports`, `gateway/porttest`

- `ports.Call` has JSON members `address, subject, request_id, deadline, writer, execution, renew (omitempty), signature` (pass-1:gateway/ports/ports.go:18-31). `SignCall` and `VerifyCall` are Ed25519 over `json.Marshal(call with Signature=nil)`. A failure is `ErrCallRefused` "the call's token is unsigned, expired or for another address" (pass-1:gateway/ports/call.go:10-32).
- `Service{Domains(); Profiles(); Serve(ctx, Call, Request) (Answer, error); Settle(ctx, queryID) (Usage, error)}` (53-64).
- `Answer{Table array.RecordReader; Record any; Handoff *protocol.Handoff; QueryID}` (38-49).
- `DataSource{Capabilities, Plan, Explain, Execute, Handoff, Renew, Settle}` (68-84).
- `Query{source, columns, filters, order_by, limit, tag}` (95-103).
- `SourceColumn{writer, source, divide}` (107-111).
- Filter ops `eq`, `in`, `between`, `latest` (114-120).
- `Explanation` JSON: `{"statement", "estimated_rows", "estimated_bytes"}` (138-142).
- `Usage{query_id, tag, rows, bytes}` (152-157).
- `Limits{Reserve(ctx, key, limit, per) (Reservation, error); Settle}` (161-164).
- `AuditRecord` JSON members (180-196): `time, request_id, caller, address, expected_profile, writer_profile, governance_revision, decision, refused_at, execution, query_id, rows, duration_ms, request_hash, outcome`.
- `porttest.DataSource` (pass-1:gateway/porttest/datasource.go:28-80) checks:
  - `explain.Statement == plan.Statement`;
  - the streamed rows are not empty and `explain.Rows > 0`;
  - `Settle.Rows == len(streamed)`;
  - for a handoff: `profile`, `rows` and `format` match, the handed-off rows equal the streamed rows, and so do the renewed links.

  `ReadRows` renders each row as `ValueStr` joined with `|` (96-116).
- **Verdict: DROP** the types; the pass-2 implementation's `gateway/ports/{provider,warehouse,audit,identity,policy}.go` supersede them. **Port** the suite's checks into `pass-2:gateway/porttest/warehouse.go`, which exists: explain equals the statement, and settlement rows equal the rows streamed. The handoff checks are DEFERRED.

### 1.12 `gateway/fakes`

- `Limits` is a token bucket per key that refills at `limit/per`. When `limit <= 0` it is unlimited (`Remaining: -1`). It refuses with `limit_exceeded` "the rate limit for this capability is used up" and `WithRetryAfter(ceil((1-tokens)/rate))` (pass-1:gateway/fakes/fakes.go:19-61). **DROP.** CRITIQUE #15 calls it per-replica. **OVERRIDDEN BY REVIEW (R07)**: "One budget authority per metered provider" (REVIEW.md:65). Rate limits are kept apart (REVIEW.md:16).
- `Audit` keeps records in memory and, optionally, appends JSON lines (65-91). **DROP** (R09).
- `Issuer`: §3. **ADAPT.**

### 1.13 `gateway/cmd/gateway/main.go` (read for the personas and flags)

- Personas (46-50):

  | Persona | Subject | Name | Groups |
  |---|---|---|---|
  | `pm` | `pm-0001` | `pm@example.test` | `fi-pm` |
  | `analyst` | `analyst-0001` | `analyst@example.test` | `fi-analysts` |
  | `outsider` | `outsider-0001` | `outsider@example.test` | none |

- Development governance `g-dev-1` (52-57): `fi-pm` grants `portfolio, index`, 600 per minute; `fi-analysts` grants `portfolio.holdings@v1:fetch`, 60 per minute.
- Flags (82-95):

  | Flag | Default |
  |---|---|
  | `-addr` | `127.0.0.1:8070` |
  | `-storage-addr` | `127.0.0.1:0` |
  | `-env` | `local` |
  | `-dev` | `true`; anything else errors "only -dev is wired in this skeleton" (97-99) |
  | `-governance` | file path |
  | `-size` | `small` or `large` |
  | `-chunk-rows` | `10_000` |
  | `-link-ttl` | `15*time.Minute` |
  | `-chunk-latency` | `0` |
  | `-audit` | file, opened `0o600` and appended |
  | `-log` | `warn` |
  | `-pin` | `id@vN=position`, repeatable |

- The issuer is `base+"/.dev"`, audience `api://atlas-gateway` (113).
- The bootstrap is `{Protocol, Environment, Servers{"gateway": {URL: base}}, Builtins{4 addresses → {"https", "gateway", "POST", "/v1/atlas/<address>"}}, SignIn{Issuer, Audience, DevToken "/.dev/token"}}` (166-176, 198-200).
- The mux has `/` (the edge) and `POST /.dev/token` (177-179).
- `http.Server{ReadHeaderTimeout: 10s, IdleTimeout: 5m}`. On SIGINT or SIGTERM: `Shutdown`, with 30 s (181-189).
- The ready line is `{"gateway": base, "storage": "http://<storage addr>"}`, printed with `fmt.Println` (190-191).
- Defects: `go http.Serve(sln, …)` (130) and `go func()` (184) are bare `go`.
- **Verdict: ADAPT.**
  - Keep the ready line and `-addr 127.0.0.1:0`.
  - Keep `-pin`, for "a capability added later" (pin to 0) and contract changes from the back.
  - Replace the personas with **two identities with different portfolio scope** (REVIEW.md:30; acceptance result 3, REVIEW.md:48), plus an outsider.

### 1.14 `PROOF-0.md` and `README.md` (pass 1)

- PROOF-0.md:3-6 records the run: 2026-10-31, `./check.sh` passes, "every Go suite under the race detector" and "the 16 Python tests, which passed in five consecutive runs".
- The numbers, "best of five … every portfolio on the latest date" (PROOF-0.md:50-57):

  | Data | Gateway alone (NDJSON over HTTP) | Python face → engine → gateway |
  |---|---|---|
  | Small, 160 rows | 1.3 ms | 1.2 ms handed off, 1.6 ms streamed |
  | Large, 40,000 rows (3.6 MB of Arrow) | 1,064 ms | 1,033 ms handed off, 1,045 ms streamed |

  "The second at Large is the fake warehouse scanning 2.4 million synthetic rows per query."
- What was assumed (63-72): the chunk encoding (each chunk a whole IPC stream; `NUMBER` up to precision 18 as a scaled narrowest integer, above 18 as `Decimal128`; timestamps as scaled ints or structs), the chunk links, re-issuing from a persisted result, settlement by `QUERY_TAG`, and the fake tokens with no On-Behalf-Of.
- Findings for the spec (76-97):
  1. The final batch works across Go and Python: arrow-go 18.8 writes per-batch metadata, and pyarrow reads it with `read_next_batch_with_custom_metadata`. "The provisional `Atlas-End` HTTP trailer was removed. Proxies drop trailers".
  2. "A cancelled command has no problem code."
  3. "ATLAS-PY-007 can't rest on standard input closing" … "the engine also ends itself when its parent changes".
  4. Ctrl+C reaches the engine too; it ignores SIGINT, and the face sends `cancel`.
  5. "A unit lens is not bit-exact when applied twice to a binary64 column". Moot now: **OVERRIDDEN BY REVIEW (R03)**, since a unit change is a new major version.
  6. Imports within a layer. Now settled: **OVERRIDDEN BY REVIEW (R24)**, "same-layer imports are allowed" (REVIEW.md:82).
  7. "A lock of exact ids does not meet ATLAS-VISION-004 by itself". **OVERRIDDEN BY REVIEW (R02)**: "'No discovery at runtime' means no new business-route selection" (REVIEW.md:60).
  8. `sdk/go/go.mod` was not tidy.
- Not done (101-108): the CLI, async, polars and DuckDB, the attribute form, Windows engine lifetime, the exact `atlas/1.0` documents, OBO, Service Bus, Blob, Cosmos, MCP, and cancelling the warehouse query when a streaming client goes away.
- README.md:39-75 records hand use and the flag table. It says the personas are `pm`, `analyst` and `outsider`, and gives `curl -s -X POST http://127.0.0.1:8070/.dev/token -d '{"persona":"pm"}'`.

---

## 2. The engine's runner protocol, as pass 1 built it, and how Python drives it

### 2.1 Process wiring (pass-1:sdk/go/cmd/atlas-engine/main.go)

| Concern | Exactly what pass 1 does | Line |
|---|---|---|
| Commands in | `os.Stdin`, JSON lines | 36 |
| Answers out | `os.Stdout`, JSON lines. "Nothing else writes to standard output." | 4-5, 36 |
| The results pipe | The env `ATLAS_ENGINE_RESULTS_FD` = an fd number, opened with `os.NewFile(uintptr(fd), "results")`. A bad value prints `atlas-engine: ATLAS_ENGINE_RESULTS_FD is not a file descriptor` to stderr and **exits 2**. Unset means no results pipe (`results == nil`) | 21-31 |
| SIGINT | `signal.Ignore(os.Interrupt)`: "What it means for a call is the face's to decide, and the face sends cancel" | 32-34 |
| Orphan check | `endWhenOrphaned`: records `os.Getppid()`, checks every **200 ms** (`time.Tick`), and `os.Exit(1)` when the parent changes (ATLAS-PY-007) | 42-52 |
| End of stdin | `Run` returns once stdin closes **and** every queued command has finished. A scanner error prints `atlas-engine: <err>` and exits 1 | 36-39; engine.go:140-142 |

### 2.2 The line protocol (pass-1:sdk/go/engine/engine.go)

**The command** (one line on stdin), from engine.go:37-54 and 66-76:

```json
{"id": <int>, "op": "<op>", "args": { ... }, "options": {"results": "arrow"?, "execution": "gateway"|"handoff"?, "timeout": <float seconds>?}}
```

- The `args` members, from one struct that every op reads: `url`, `token`, `lock`, `words`, `id`, `ids`, `path`, `address`, `request`.
- For a cancel, `{"id": <id to cancel>, "op": "cancel"}`.

**The answer** (one line on stdout), engine.go:57-63:

```json
{"id": <int>, "ok": <bool>, "result": <any, omitempty>, "problem": <problem, omitempty>, "cancelled": <bool, omitempty>}
```

**The operations, and what each answers:**

| `op` | `args` | `result` on success | Line |
|---|---|---|---|
| `version` | — | `{"name": "atlas-go", "version": "0.1.0", "protocol": "atlas/0.1", "levels": ["core", "streaming", "columnar", "handoff"]}`. Works before `connect` | 207-211 |
| `connect` | `url`, `token`, `lock` | `{"protocol", "environment", "lock"?: <path>}`. The lock is: named and loaded (a missing file is no lock), else `FindLock(os.Getwd())`. Then `client.Connect(url, token, lock)`. Any op except `version` and `connect` before a connect fails `invalid_request` "connect first" | 252-278, 216-219 |
| `whoami` | — | The `atlas.whoami` answer | 221-223 |
| `catalog` | — | `{"items": [...]}` | 224-226 |
| `search` | `words` | `{"items": [...]}` with `score` | 227-229 |
| `describe` | `id` (id, pinned or exact) | `{exact, current, profile, governance, availability}` | 230-232 |
| `lock` | `ids`, `path` | Map pinned → exact (`LockCurrent`) | 233-239 |
| `call` | `address`, `request` | The raw JSON body, embedded as `json.RawMessage` | 240-242 |
| `fetch` | `address`, `request` | JSON form (`options.results != "arrow"`): `{"writer", "reader", "via", "rows": [row records]}`. Arrow form: `{"writer", "reader", "via", "stream": <command id>}`, then the stream on the results pipe | 280-316 |
| anything else | — | `invalid_request` `the engine has no operation "<op>"` | 245-246 |

Bad `args` fail `invalid_request` "the arguments of <op>: <err>" (199-205).

**Scheduling.**
- One reader goroutine scans stdin. The scanner buffer starts at `64<<10` and allows a line up to `64<<20` bytes (118-119).
- A non-JSON line is answered `{"id":0,"ok":false,"problem":{invalid_document "a command is not JSON: …"}}` (122-124).
- A `cancel` is acted on **as it arrives**: it calls the `CancelCauseFunc` registered for that id with `errCancelled`. It gets **no answer line** (126-133).
- Every other command gets `context.WithCancelCause(ctx)` and is queued on a channel of capacity **64** (110, 134-138).
- One worker goroutine runs the commands **one at a time, in order** (111-117).
- `run` applies `options.timeout` as `context.WithTimeout(t × 1s)` (155-159). A command already cancelled or expired while it waited is failed at once (160-163). The cancel func is deleted after the run (146-153).
- Answers are serialised by a mutex around one `json.Encoder` (167-171).

**Failure mapping** (175-196):
- The cause is `errCancelled`: `{"id":n,"ok":false,"cancelled":true}`, with no problem.
- The context deadline is exceeded: `deadline_exceeded` "the call's deadline passed".
- Otherwise `problem.From(err)`. A `*protocol.Problem` passes through, `context.DeadlineExceeded` becomes `deadline_exceeded`, a `MaxBytesError` becomes `invalid_request`, and anything else becomes `internal` "an internal error occurred" (pass-1:sdk/go/kit/problem/problem.go:24-40).

**Arrow over the second pipe** (`stream`, engine.go:320-347):
1. A fetch with `results: "arrow"` and no results pipe fails `invalid_request` "the engine was started without a results pipe" (281-284).
2. On success, the **answer line is written first**: `{"id":n,"ok":true,"result":{"writer","reader","via","stream":n}}` (313-314).
3. Then, on the results fd: `ipc.NewWriter(results, WithSchema(tbl.Schema()))`. The schema is the **reader** schema, from `protocol.ArrowSchema(readerTable, readerExact)` (client.go:301). Each resolved batch follows. Then the **final zero-row batch** from `protocol.EndBatch(mem, schema, EndLine{Rows: n, Problem?})`, whose message metadata is `atlas:end` = `{"$atlas":"end","rows":n,"truncated":false,"problem"?}` (pass-1:sdk/go/protocol/end.go:13-30; documents.go:100-105).
4. `defer w.Close()` **always** writes the IPC end-of-stream marker: "the face never waits on a stream" (322).
5. A failure after the first batch travels in the final batch's `problem` (337-340).
6. **Cancelled mid-stream**: the cause is `errCancelled`, so it returns **without the final batch**. EOS is still written (334-336).
7. If `EndBatch` fails, there is no final batch (341-344).
8. Only one command runs at a time, so the results pipe carries one stream at a time and needs no multiplexing. `stream: <id>` names it.

### 2.3 How the Python face drives it (pass-1:clients/python/fiatlas/_engine.py, client.py)

**Finding the binary**, `engine_path(explicit)` (_engine.py:40-52). The first that is set wins:
1. the explicit argument;
2. `ATLAS_ENGINE`;
3. `<package>/bin/atlas-engine` (`atlas-engine.exe` on `nt`);
4. `shutil.which`;
5. else `EngineError` "atlas-engine was not found: set ATLAS_ENGINE, or install the wheel that ships it".

**Starting it**, `Engine.__init__` (84-111):
- It makes three `os.pipe()`s: stdin (`in_r`, `self._in`), stdout (`self._out`, `out_w`) and results (`self._res`, `res_w`).
- It runs `subprocess.Popen([path], stdin=in_r, stdout=out_w, pass_fds=(res_w,), env={**os.environ, "ATLAS_ENGINE_RESULTS_FD": str(res_w)})`. stderr is inherited.
- It closes the child ends in `finally`. On an `OSError` it closes its own ends too and raises `EngineError` "atlas-engine could not be started: …".
- It records `owner = os.getpid()` and `pid`. It keeps only raw fds, "never buffered file objects, so that a forked child can let go of its parent's pipes without touching them" (10-11).
- It registers itself in `_live: weakref.WeakSet` (37, 111).

**Lifetime rules:**
- `usable()` = `not self._broken and self.owner == os.getpid() and self._proc.poll() is None` (113-114).
- `FIAtlas._run` starts an engine lazily on first use. It always sends `connect` first, with `{url, token, lock}`, and stores `.connected`. If the engine is not usable it stops it: "this process's broken engine ends; a parent's is left alone" (client.py:67-79).
- `os.register_at_fork(after_in_child=_after_fork_in_child)` calls `_forget()` on every live engine. That closes the parent's fds **without writing**, and marks the engine broken and closed, so the child's next call starts its own (ATLAS-PY-004; _engine.py:235-242, 269-279).
- `atexit.register(_at_exit)` calls `stop()` on every live engine (274-280).
- `stop()` (219-233):
  - it does nothing if already closed or `owner != getpid()`;
  - it closes stdin and waits `GRACE` (`1.0` s, line 32), then `kill()` and `wait()`;
  - it joins the reader thread for `GRACE`, then closes the stdout and results fds.

**Framing.**
- `_send` writes `json.dumps(command, separators=(",", ":")) + "\n"` by looping `os.write` on a memoryview. Any exception mid-write marks the engine broken, "A command cut short would run into the next one". An `OSError` becomes `EngineError` "atlas-engine is gone: …" (146-156).
- The command is `{"id": cid, "op": op, "args": args or {}, "options": opts}`. `opts` drops `None` values, and `timeout` is added when given (121-125). Ids come from `itertools.count(1)` (106).
- `_readline` buffers `os.read(self._out, 1 << 16)` and splits on `\n`. EOF returns `b""` (187-197).
- `_receive(cid)` runs in a **daemon thread per call**. It reads lines, **skipping any whose `id` ≠ cid**. If the answer is `ok` and its `result` holds `"stream"`, it calls `_read_stream()`. Any exception marks the engine broken and is boxed; `done` is set in `finally` (168-185). EOF on stdout raises `EngineError` "atlas-engine ended (<returncode>)".
- `_read_stream` (199-217) uses `pa.ipc.open_stream(pa.PythonFile(_Results(fd), mode="r"))`:
  - `_Results.read(n)` loops `os.read` until n bytes or EOF, with reads of at most `1 << 20` (55-77).
  - It loops `reader.read_next_batch_with_custom_metadata()` until `StopIteration`.
  - A batch whose metadata has `b"atlas:end"` (`META_END`, 35) sets `end = json.loads(meta[b"atlas:end"])`. Other batches are kept. A batch after the end raises `EngineError` "a batch followed the final batch".
  - It returns `(pa.Table.from_batches(batches, schema=reader.schema), end)`. `end` is `None` when there was no final batch.
- `_checked(answer)` (252-266):
  - not `ok` with `cancelled`: `EngineError` "the engine reports a cancellation the face did not ask for";
  - not `ok`: `AtlasError(problem)`;
  - `end` is None: `EngineError` "the engine's stream ended without its final batch";
  - `end.problem`: `AtlasError(end["problem"])`;
  - `end.rows != table.num_rows`: `EngineError` "the engine sent N rows; its end line counts M".

**Cancellation and deadlines**, `Engine.run` (116-144):
- It holds `self._lock` for the whole call, so there is **one call at a time per Python process**.
- It waits with `done.wait(0.05)` in a loop, "so Ctrl+C is seen whichever thread the signal lands on" (132-134).
- **Deadline backstop**: `limit = monotonic() + timeout + GRACE`. Past it, it calls `stop()` and raises `EngineError` "atlas-engine did not end the call at its deadline; it was stopped" (130, 135-137). Normally the engine answers `deadline_exceeded` first.
- **Ctrl+C**: `KeyboardInterrupt` calls `_cancel(cid, done)`. That sends `{"id": cid, "op": "cancel"}`, waits `done.wait(GRACE)`, and calls `stop()` if not done. Then it **re-raises `KeyboardInterrupt`** (139-141, 158-166). The cancelled answer, or the stream without its final batch, is read by the reader thread and discarded, because the exception path skips `_checked`.
- After a normal end it calls `self._reader.join()`: "a call leaves no thread behind, so a fork after it copies none" (138).

**The whole chain, Ctrl+C mid-call:**
1. The terminal sends SIGINT to the whole process group.
2. The engine ignores it (main.go:34).
3. Python raises `KeyboardInterrupt` in the main thread, and the face sends `cancel`.
4. The engine cancels that command's context with `errCancelled`.
5. The client's HTTP request and chunk downloads stop on the context, because both take `ctx`.
6. The engine answers `cancelled:true`, or ends the stream without a final batch.
7. The reader thread finishes, and the face re-raises.

The test requires the whole chain in under 1.0 s and the same engine pid afterwards (pass-1:clients/python/tests/test_engine_process.py:47-56). **Not done:** cancelling the warehouse query in the gateway when the client goes away (PROOF-0.md:108).

### 2.4 Pass 1 against §9.8.3 and §5.1: the differences

| Item | Pass 1 | Spec | Fix |
|---|---|---|---|
| Options location | Top-level `"options"` (engine.go:41) | Inside `args`: `"args": {"address", "request", "options": {"deadline_ms": 2000, "accept": [...]}}` (spec:09-8-conformance.md:42) | Move into `args.options` |
| Success member | `"result"` (engine.go:60) | `"value"`, plus `"warnings": []` (each `{code, text}`) and `"calls": <n>` (spec:09-8-conformance.md:48, 53) | Rename; add `warnings` and `calls`. AMBIGUOUS: what `calls` counts (see the ambiguities) |
| Deadline option | `timeout`, float seconds | `deadline_ms` (spec:09-8-conformance.md:42) | Use `deadline_ms` |
| Encodings option | `Accept` fixed to Arrow in the client | `accept: [media types]` (spec:09-8-conformance.md:42; refs:scenarios/STREAM-001.json uses `["application/x-ndjson"]`) | Add |
| Person and confirm | none | `options.person: true`, `options.confirm: "accept"|"decline"` (spec:09-8-conformance.md:53) | Add (CORE-CONFIRM-001 is DEFERRED with metered work; step 3) |
| A table's JSON form | `{"writer", "reader", "via", "rows": [...]}` | `value` = "arrays of row records (decimals as their strings, timestamps as RFC 3339)", bytes base64 (spec:09-8-conformance.md:53). The end line's usage is reported as `usage` (refs:QUESTIONS.md:122, Q-RUN-5). A failed result may carry `value`, the rows before the failure (Q-RUN-6, QUESTIONS.md:123) | `value` = rows; `usage` beside it. AMBIGUOUS: where `writer`, `reader` and `via` go for the face |
| Ops | `version, connect, whoami, catalog, search, describe, lock, call, fetch, cancel` | §9.7.1 ops plus `version` (spec:09-8-conformance.md:53); `guide` and `explain` too (Q-RUN-1, QUESTIONS.md:118); `access` | Add `guide`, `explain`, `access` (IN SCOPE). `operation.*` where the journey needs operations. `subscribe.*`, `publish`, `export`, `upload` are DEFERRED |
| Environment | `ATLAS_URL`, `ATLAS_TOKEN` (client.go:70, 75) | The suite starts the runner with `ATLAS_URL`, `ATLAS_HOME` (a fresh directory), `ATLAS_CONFORMANCE=1` (spec:09-8-conformance.md:37). The scenarios put the token in `{ATLAS_HOME}/state/token-conformance.json`, mode `0600`, dir `0700` (refs:scenarios/STREAM-001.json `setup.files`) | The token store (§9.7.3); R26 binds it |
| `version` answer | `name, version, protocol, levels` | "its client's name, version, protocol and levels" (spec:09-8-conformance.md:53) | Same members. Values become `atlas/1.0` and the declared levels |
| Arrow on the second pipe | `options.results = "arrow"`; the answer carries `stream: id` | "A command may ask for `"results": "arrow"`. A table then comes back as an answer naming a stream … on a second pipe the face passed in at start, ended by the protocol's final zero-row batch (§9.6.5)" (spec:05-clients.md:236) | Keep. AMBIGUOUS: where `results` sits now that options are inside `args` |
| Cancel | `{"op":"cancel","id":n}`; the answer is `cancelled:true`, or a stream without its final batch | "`{"op": "cancel", "id": n}` cancels the work of command `n`; its answer is the problem `deadline_exceeded` or the cancellation" (spec:05-clients.md:237) | AMBIGUOUS (PROOF-0.md:81-85). **OVERRIDDEN BY REVIEW (R17)**: "Cancellation is reported as requested or confirmed" (REVIEW.md:75) |
| The end line in the final batch | `{"$atlas", "rows", "truncated", "problem"?}` | `$atlas, rows, truncated, usage, problem` are all required; `problem` is null on success (refs:schemas/chunk-end.schema.json; pass-2:sdk/go/protocol/frame/frame.go:37) | Write with `frame.ArrowWriter.End`. It requires `atlas:profile` and `atlas:protocol` schema metadata (frame.go:333-337) |
| Failure | `{"id","ok":false,"problem"}` | the same (spec:09-8-conformance.md:51) | Same. The problem gets the pass-2 members |

### 2.5 Steps to bring the engine back

1. Move `sdk/go/engine/engine.go` onto the new `atlas` package. Keep:
   - the reader, cancel and worker split;
   - `CancelCauseFunc` per id;
   - one command at a time;
   - the answer-line-first, then results-pipe order;
   - EOS always.
2. Start the goroutines with `conc.Go` (pass-2:sdk/go/kit/conc/conc.go:42). Wait for them at the end of `Run`.
3. Make the queue unbounded for cancels. Two ways: read cancels on a separate path that never blocks behind a full queue, or refuse new commands with a problem while the queue is full. AGENTS.md:45 forbids unbounded queues, so the second is safer.
4. Change the shapes to §2.4: `args.options.{deadline_ms, accept, results, execution, person, confirm}`; answers `{"id", "ok", "value", "warnings", "calls"}` or `{"id", "ok": false, "problem"}`.
5. Write the JSON form batch by batch from a `frame` reader, without building every row in memory first. One answer line still holds every row, so either cap it (§9.7.13's `max_memory`) or document the bound.
6. Write the Arrow form with `frame.NewArrowWriter` and `End`. Check the end line's `rows` in the face, as `_checked` does.
7. Keep `ATLAS_ENGINE_RESULTS_FD`, `signal.Ignore(os.Interrupt)` and the orphan check. Inject the clock, or keep 200 ms. Add a Windows job object, or record the gap as pass 1 did.
8. The engine reads `ATLAS_URL`, `ATLAS_HOME` and `ATLAS_CONFORMANCE` only in `cmd/atlas-engine`, its composition root (AGENTS.md:42).
9. Prove it with the vectors and scenarios of its levels through this runner (spec:09-8-conformance.md:140). It is "also the Go reference runner, so the protocol is proven by the conformance suite" (spec:05-clients.md:230).
10. Keep `sdk/go` pure Go, so `atlas-engine` cross-compiles with no C compiler (ATLAS-GO-002, spec:05-clients.md:555). **DuckDB must never enter the engine's import graph.**

---

## 3. The fake issuer: its JWT, its JWKS, and how tests mint tokens

Source: pass-1:gateway/fakes/issuer.go.

| Item | Exactly | Line |
|---|---|---|
| `Persona` | `{Subject, Name string; Groups []string}` | 19-23 |
| `Issuer` | `{Issuer, Audience string; Personas map[string]Persona; key *rsa.PrivateKey; kid string}`. "a fake of Entra ID: it signs RS256 tokens for named personas, so that the real verifier runs against real signed tokens in every local run and test (§6.7.4)" | 25-34 |
| `NewIssuer(issuer, audience, personas)` | `rsa.GenerateKey(rand.Reader, 2048)`, `kid: "dev-1"` | 37-43 |
| `Verifier()` | `&jwt.Verifier{Issuer: d.Issuer, Audiences: []string{d.Audience}, Keys: jwt.StaticKeys{d.kid: &d.key.PublicKey}}` | 46-48 |
| `Token(persona)` | Signs for an hour; an unknown persona errors `unknown persona %q` | 51-58 |
| JOSE header | `{"alg":"RS256","kid":"dev-1","typ":"JWT"}` | 63 |
| Claims | `iss` = issuer; `aud` = audience (**a string**, not an array); `sub` = Subject; **`oid` = Subject**; `preferred_username` = Name; `groups` = Groups; `iat` = `nbf` = now; `exp` = now + ttl (1 h) | 64-67 |
| Signature | `base64.RawURLEncoding(header) + "." + base64.RawURLEncoding(claims)`, SHA-256, `rsa.SignPKCS1v15(…, crypto.SHA256, …)`, then appended base64url | 68-75 |
| `Handler` (`POST /.dev/token`) | **Loopback only.** A remote address that is not loopback gets 403 "development tokens are issued on loopback only". The body `{"persona": "pm"}` is read through `http.MaxBytesReader(w, r.Body, 1024)`. An unknown persona gets 404 "unknown persona". The answer is `Content-Type: application/json`, `Cache-Control: no-store`, body `{"access_token": <jwt>, "expires_at": <unix exp>}` | 79-102 |

**There is no JWKS in pass 1's issuer.** The verifier gets the public key through `jwt.StaticKeys` (issuer.go:47). No JWKS document, OIDC discovery, refresh token, device code or claims challenge is served. `kit/jwt` has a caching JWKS client, but the issuer does not use it:
- `KeySet`;
- `NewKeySet(url)` with `Client` timeout 5 s, `MinRefreshInterval: time.Minute` and `MaxAge: 24 * time.Hour`;
- refresh on an unknown kid, single-flight;
- honours `Cache-Control: max-age`;
- reads at most 1 MiB;
- skips keys with no `kid`, or with `use` ≠ `sig`;
- refuses RSA keys under 2048 bits.

(pass-1:sdk/go/kit/jwt/jwks.go:116-267, 54-69.)

**The real verifier** (pass-1:sdk/go/kit/jwt/verifier.go:83-166; identical in the pass-2 implementation):
- `Algorithms` default to `["RS256"]`. `alg` empty, `none` or unlisted is `ErrAlgorithm`.
- A non-empty `crit` is refused. An empty `kid` is refused.
- Key lookup failure, or a bad signature, is `ErrSignature`.
- `iss` must equal exactly. `aud` must contain one of `Audiences`; the claim may be a string or an array (`Audience.UnmarshalJSON`, 21-36).
- `exp` is required and checked with **no leeway**. `nbf` gets a leeway of `5*time.Minute` (default).
- `MaxTokenAge` is off by default. `Now` is injectable.
- `Claims` include `iss, sub, aud, exp, nbf, iat, jti, scp, scope, roles, tid, oid, preferred_username`, plus `Raw` (40-56).

**How the gateway maps a token** (pass-1:gateway/core/pipeline/pipeline.go:268-277): Subject = `oid`, else `sub`; Name = `preferred_username`; Groups = `json.Unmarshal(Raw["groups"])`. Governance roles match the subject, or a group id, against `Members` (governance.go:99-107).

**How tests mint tokens:**

| Where | How | Issuer and audience |
|---|---|---|
| Go edge tests | `issuer.Token(persona)` in process, then `Authorization: Bearer <token>` (pass-1:gateway/edge/http/edge_test.go:63-67, 88-94) | `https://issuer.test`, `api://atlas-gateway` |
| Go e2e | `http.Post(g.URL+"/.dev/token", "application/json", {"persona": …})`, read `access_token` (pass-1:gateway/e2e/e2e_test.go:72-84) | `<gateway base>/.dev`, `api://atlas-gateway` (pass-1:gateway/cmd/gateway/main.go:113) |
| Python | `urllib.request` POST `/.dev/token` with `{"persona": persona}`; `["access_token"]` goes to `FIAtlas(url, token)`, then over the pipe in `connect.args.token` (pass-1:clients/python/tests/conftest.py:38-48) | The same |
| By hand | `curl -s -X POST http://127.0.0.1:8070/.dev/token -d '{"persona":"pm"}'` (pass-1:README.md:49) | — |

The bootstrap advertises it as `sign_in: {"issuer", "audience", "dev_token": "/.dev/token"}` (pass-1:gateway/cmd/gateway/main.go:175; documents.go:25-29).

**Defects, and the mapping to pass 2:**
1. No clock injection: `sign` uses `time.Now()` (issuer.go:61). AGENTS.md:56 says "Inject the clock". The pass-2 implementation has `sdk/go/kit/clock`.
2. `http.Error` is used (issuer.go:82, 91, 96), which breaks the `nohttperror` analyzer (AGENTS.md:43).
3. There are no `tid`, `azp` or `appid`, and no person/workload marker. The pass-2 `ports.Identity` has `Principal` (Entra's oid), `Name`, `Kind` ("person" or "workload", "from the token, never from the caller's own claim"), `Tenant`, `Application` (azp or appid) and `Groups` (pass-2:gateway/ports/identity.go:10-24). AMBIGUOUS: which claim gives `Kind`. The files I read do not fix it, so check §6.2/§6.7.
4. `aud` is a single string, which is accepted.
5. There is no JWKS, OIDC discovery, refresh, device code or claims challenge. PLAN.md asks the fake issuer for "RS256, JWKS, OIDC discovery, device code, refresh, claims challenge", proven by "The real `kit/jwt` verifier in every test" (pass-2:docs/pass-2/PLAN.md:88). Scenarios CORE-SIGNIN-002/003/004 need refresh, `401 invalid_token` and a claims challenge (spec:09-8-conformance.md:88-90).
6. The development token endpoint sits on the gateway's own mux. pass-2 bootstrap `sign_in` needs `authority`, `client_id`, `scopes` and `token_endpoint` (refs:schemas/bootstrap.schema.json `$defs.sign_in`).

**Verdict: ADAPT.**
1. Keep RSA-2048, RS256, `kid` and personas mapped to claims, and keep the loopback-only token endpoint. The real `kit/jwt` verifier must run in every test.
2. Serve a JWKS (`{"keys":[{"kty":"RSA","kid","use":"sig","alg":"RS256","n","e"}]}`, the members `kit/jwt.JWK` reads, jwks.go:42-52). Verify through `kit/jwt.KeySet` rather than `StaticKeys`, so the production path runs.
3. Inject the clock. Write problems through `kit/problem`.
4. Add `tid` and `azp`, and a claim for `Kind`.
5. Add an adapter from `kit/jwt.Claims` to `ports.Identity` behind `ports.Authenticator` (pass-2:gateway/ports/identity.go:37-39). Map `jwt.ErrExpired` to `ports.ErrTokenExpired`, and other `ErrInvalidToken` to `ports.ErrUnauthenticated`.
6. Personas: **two identities with different portfolio scope** plus an outsider (REVIEW.md:30, 48). The groups carry the scope; the typed scope rules are R05.
7. **AMBIGUOUS**: the pass-2 implementation already has `gateway/fakes/tokens.go`, an opaque token table: `Issue(token, Identity, expires)`, `Revoke`, `Authenticate` (pass-2:gateway/fakes/tokens.go:12-57). It is a fake at the `Authenticator` port, which is *below* the JWT verifier. AGENTS.md:58 says "Never fake below the code you mean to test". Readings: (a) the opaque table for core unit tests, and the RS256 issuer with the real verifier for e2e and the clients; (b) the issuer only. The build lead must choose.

---

## 4. The snowfake and Harbour data: what synthetic data exists, and its shape

### 4.1 The sizes (pass-1:gateway/adapter/snowflake/snowfake/harbour.go:11-25)

| Size | Portfolios | Holdings (per portfolio per date) | Indices | Members (per index per date) | Dates (business days) | End |
|---|---|---|---|---|---|---|
| `Small` | 4 | 40 | 2 | 60 | 5 | 2026-09-30 |
| `Large` | 20 | 2000 | 2 | 3000 | 60 | 2026-09-30 |

The derived counts follow from `Rows: s.Portfolios * s.Dates * s.Holdings` (48) and `s.Indices * s.Dates * s.Members` (74):

| | `HARBOUR.PORTFOLIO_HOLDINGS` rows | `HARBOUR.INDEX_CONSTITUENTS` rows | One date, all portfolios |
|---|---|---|---|
| Small | 4 × 5 × 40 = **800** | 2 × 5 × 60 = **600** | 160 rows |
| Large | 20 × 60 × 2000 = **2,400,000** | 2 × 60 × 3000 = **360,000** | 40,000 rows |

This agrees with PROOF-0.md:54-57: Small 160 rows, Large 40,000 rows, and "2.4 million synthetic rows per query".

The Small dates are 2026-09-24 (Thu), 09-25 (Fri), 09-28 (Mon), 09-29 (Tue) and 09-30 (Wed). `businessDays` skips Saturdays and Sundays only, with no holidays (harbour.go:111-122; computed).

### 4.2 `HARBOUR.PORTFOLIO_HOLDINGS` (harbour.go:36-65)

| Column | Snowflake type | Nullable | Value |
|---|---|---|---|
| `PORTFOLIO_CODE` | `text` | no | `PortfolioCode(p)`: `147HYC`, `200IG`, `310EMD`, `420MUNI`, then `P%03d` (86-91) |
| `ISIN` | `text` | no | `ISIN(sec)` = `fmt.Sprintf("XS%010d", 1_000_000_000+sec*7919)` (102). Not check-digit-valid ISINs. **Never null** |
| `AS_OF_DATE` | `date` | no | `dates[d]` |
| `MARKET_VALUE` | `fixed(18,2)` | no | `(100_000 + h%9_900_000)*100 + (h>>8)%100`, scaled by 10^2. So 100,000.00 to 9,999,999.99 |
| `WEIGHT` | `fixed(9,6)` | no | `(h >> 12) % 20_000`, scaled by 10^6. So 0.000000 to 0.019999. **Weights do not sum to 1** |
| `OAS_BP` | `fixed(12,4)` | **yes** | `(50 + h%850)*10_000 + (h>>20)%10_000`, scaled by 10^4. So 50.0000 to 899.9999 bp. `nil` when `h%97 == 0` |
| `SECTOR` | `text` | no | `sector(sec)`: `Supranational` if `sec%8 == 7`, else `Sectors[sec%4]` from `Industrials, Financials, Utilities, Energy` (29, 104-109) |
| `CURRENCY` | `text` | no | `["USD", "EUR", "GBP"][sec%3]` |

Row `i` decomposes as:
- `p = i/(Dates*Holdings)`, `d = (i/Holdings)%Dates`, `k = i%Holdings`;
- `sec = (p*Holdings/2 + k) % universe`, where `universe = max(4*Holdings, Members)` (Small 160, Large 8000);
- `h = mix(p, sec, d)` (harbour.go:35, 50-53).

So consecutive portfolios share half their securities. The same security in the same portfolio has a new market value, weight and OAS on each date.

`mix(a, b, c)` is splitmix64 over three inputs (127-132):

```go
z := a*0x9E3779B97F4A7C15 ^ b*0xBF58476D1CE4E5B9 ^ c*0x94D049BB133111EB
z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
z = (z ^ (z >> 27)) * 0x94D049BB133111EB
return z ^ (z >> 31)
```

"The same Size always gives the same rows" (31-32). The rows are generated on demand by `Row func(i int) []any`, so a large table is never held in memory (snowfake.go:39-46, 246-247).

### 4.3 `HARBOUR.INDEX_CONSTITUENTS` (harbour.go:66-80)

| Column | Type | Value |
|---|---|---|
| `INDEX_CODE` | `text` | `IndexCode(x)`: `H0A0`, `C0A0`, then `IX%02d` (94-99) |
| `ISIN` | `text` | `ISIN((x*7 + k) % universe)` |
| `AS_OF_DATE` | `date` | `dates[d]` |
| `WEIGHT` | `fixed(9,6)` | `mix(x+100, sec, d) % 5_000`, scaled by 10^6. So 0 to 0.004999 |

### 4.4 The fake warehouse's behaviour (pass-1:gateway/adapter/snowflake/snowfake/snowfake.go)

These are kept for the DEFERRED handoff proof.

| Behaviour | Value | Line |
|---|---|---|
| Defaults | `LinkTTL: 15*time.Minute`, `ChunkRows: 10_000`, `Now: time.Now`, `Latency: 0`, a 32-byte random HMAC secret | 92-102 |
| Query id | `fmt.Sprintf("01b%013d-0000-fake-%04d", started.UnixMilli(), seq)` | 133 |
| History | `QueryRecord{QueryID, Tag, SQL, Rows, Bytes = rows × rowBytes, Started, Ended}` | 135-138 |
| Retention | Results kept 24 h; afterwards `the result of query %s is no longer kept` | 147-149 |
| Chunks | Chunk 0 is inline (`First`). Links cover chunks 1…n-1, each `{url, rows, bytes}` | 177-189 |
| Link | `<base>/results/<qid>/<n>?exp=<unix>&sig=<hex HMAC-SHA256 of "qid|n|exp">` | 191-203 |
| Download header | `X-Snowfake-Key: <per-result random 16 bytes as hex>` (`KeyHeader`) | 37, 126, 187 |
| Storage answers | A bad path or `n < 1`: 404 "not found". A bad sig or past `exp`: 403 "this link has expired or is not valid". A missing or wrong header: 403 "the download headers are missing or wrong". `Latency` waits, but honours the request context. 200 is `application/octet-stream` | 206-244 |
| Encoding | Each chunk goes through `snowarrow.Encode(rowType, rows)` and then `WriteChunk`, a whole IPC stream | 248-265 |
| Byte estimate (`rowBytes`) | text 16, date 4, boolean 1, timestamp_* 16, other 8 | 276-293 |
| Errors | `SQL compilation error: object %s does not exist or not authorized`; `SQL compilation error: invalid identifier %s` | 120, 160, 305, 318, 426 |
| Evaluation (DROP) | Go `match`: latest = MAX over the **whole table**, then filter, stable sort by the order-by aliases' source columns, limit. It checks `ctx.Err()` every 65,536 rows | 297-361 |
| Casts | To `real`: exact `big.Rat` from the scaled int, divided by `Divide`, then one `Float64()`. To `fixed`: scale up only; a smaller scale errors "cannot be cast to the smaller scale … without rounding". Enum: a string value outside the projection's enum becomes `UNKNOWN` | 421-473 |

### 4.5 What the journey needs, against what exists

Reference holdings profile: refs:vectors/profiles/data.json:220-356.
- Required row members: `portfolio, as_of, title, market_value, weight, status`.
- Properties:
  - `portfolio` (`PortfolioCode`, key);
  - `isin` (string or null, "null for cash and instruments without one", key);
  - `as_of` (date, key);
  - `title` (string or null);
  - `market_value` (`Money` object, alias `mv`);
  - `weight` (number, `Fraction`);
  - `oas` (number or null, `Bp`, alias `oas_bp`);
  - `status` (enum `open`, `closed` with alias `sold`, `UNKNOWN`);
  - `duration` (number or null, `Years`, deprecated).
- Meaning: `keys: [portfolio, isin, as_of]`, and `as_of` is "the portfolio's last valuation on or before as_of".

| Needed by the journey | In pass 1's synthetic data | Action |
|---|---|---|
| `portfolio.holdings@v1` rows with `isin` nullable (cash), `title`, `status`, `market_value` as `Money{amount, currency}` | No cash rows (the ISIN is never null), no `TITLE`, no `STATUS`; `MARKET_VALUE` is a bare `NUMBER(18,2)` with `CURRENCY` per security | ADAPT the generator: add cash rows with null ISIN, a title, a status, and Money's currency. The fixture must state the owner choices REVIEW.md:97 lists: null `as_of`, cash and instruments without an ISIN, aggregation, currency and signs, whether weights include cash, partial authorisation |
| `portfolio.list@v1` and `portfolio.search@v1` (the identifier lookup) | Only the codes `147HYC, 200IG, 310EMD, 420MUNI, P%03d`. No portfolio names or attributes | New table, for example a portfolio list with code and name. AMBIGUOUS: its columns come from the handbook (another digest) |
| `data.status@v1` (freshness) | Nothing | New fixture, for example the last load time and latest `as_of` per source. AMBIGUOUS: the shape comes from the handbook |
| `portfolio.holdings-dates@v1` | Derivable: `DISTINCT AS_OF_DATE` per portfolio from the holdings table (5 dates Small, 60 Large) | Derive in SQL over the same table |
| Two identities with different portfolio scope | Governance grants by domain or capability only; there is no row scope | The fixture assigns, for example, persona A all four named portfolios and persona B only `147HYC`. The acceptance check is "a correspondingly narrower result, or a clear refusal" (REVIEW.md:48) |
| Exact decimals | The values are scaled integers: exact | Keep. Prove that `market_value.amount` arrives as `decimal128(38,6)` in Arrow and as a decimal string in JSON |
| Determinism and an independent check | splitmix64, pure | Keep. "its result can be checked independently against the source fixture" (REVIEW.md:51) |
| Latest-date semantics | Global MAX over the table | Per portfolio, "last valuation on or before as_of". The pass-2 fake warehouse chooses "the latest of each group" (pass-2:gateway/fakes/warehouse.go:26) |
| Index constituents | Present | Not in the journey. Keep only as a second domain for "only my domain" tests, if wanted |

---

## 5. Every lesson in CRITIQUE.md, as a rule for pass 2

Each rule is stated as an instruction. In brackets: the defect number and its severity; then its scope under REVIEW.md. Source: pass-2:docs/pass-2/CRITIQUE.md:13-65.

### The protocol

1. **[#1 Blocker]** Use one profile model: the nested §9.3 profile, its schemas JSON Schema 2020-12 in the Atlas Schema profile, the Arrow schema *derived*. Never a custom Table/Column struct. IN SCOPE. Phase 1 has `protocol/profile`, `schema` and `arrowschema`.
2. **[#2 Blocker]** Compute fingerprints only by RFC 8785 (JCS: UTF-16 key order, ECMAScript numbers) over §9.4.1's exact projection, checked against `refs:vectors/fingerprints.json`. IN SCOPE. **OVERRIDDEN BY REVIEW (R01)**: the contract fingerprint is cached apart from the document revision (REVIEW.md:59).
3. **[#3 Blocker]** Decode every protocol document as I-JSON: reject duplicate members and integers beyond 2^53. `ijson` exists. IN SCOPE.
4. **[#4 Blocker]** Parse and serialise every Atlas header as an RFC 9651 Structured Field, fuzzed and checked against the header vectors. Never split strings. `protocol/sf` and `headers.go` exist. IN SCOPE.
5. **[#5 Major]** Validate with the Atlas Schema validator: the keyword list, the format assertions, I-Regexp, `oneOf` with const discriminators, the `x-atlas-logical` constraints and JSON Pointer failures. Run the meta-schema check in the gate. IN SCOPE.
6. **[#6 Major]** Implement resolution exactly in row and column forms, against the vectors. IN SCOPE as an experiment only. **OVERRIDDEN BY REVIEW (R03, R04)**: clients are obliged only to aliases, defaults, UNKNOWN and projection, and "no served profile may depend on `convert_unit`, `promote` or `drop` within a version" (REVIEW.md:90). A request is validated under its own contract first, and unknown fields are refused (REVIEW.md:62).
7. **[#7 Major]** Parse addresses with the exact grammar of §9.1.3. Allow a bare `id:interaction` only where §9.7.1 allows it, at a terminal or notebook (spec:09-7-clients.md:58). IN SCOPE.
8. **[#8 Major]** Write and read the lock as `lock.schema.json`: TOML parsed, then validated (`capabilities{<id>: {version, fingerprint, dynamic?}}`). IN SCOPE. **OVERRIDDEN BY REVIEW (R02)**: a lock pins semantic contracts, and routing is current metadata (REVIEW.md:60). The format is still open (REVIEW.md:98).
9. **[#9 Major]** Every end line carries `usage`. Every problem carries `instance`, the `type` URL, `estimate` where it applies and per-hop `details`. Every document is checked against its schema. IN SCOPE.
10. **[#10 Major]** Generate document types from `references/schemas/`; never write them by hand. IN SCOPE.
11. **[#11 Major]** Gate every profile with classify, lint and readiness, at the gate and at admission. IN SCOPE (step 2). **OVERRIDDEN BY REVIEW (R03)**: the gate takes the smaller compatibility model (REVIEW.md:91).

### The gateway

12. **[#12 Blocker]** Route by looking the request path up in the snapshot's binding table. Never parse a capability name out of the URL. IN SCOPE.
13. **[#13 Blocker]** Pass the query tag out of band, as session or statement metadata. The statement depends only on the request, so `explain` equals `fetch` byte for byte. IN SCOPE.
14. **[#14 Blocker]** Build SQL only as sqlglot typed trees rendered per dialect. Parse every statement with the Snowflake parser in the read-only guard before it runs, with guard vectors. IN SCOPE (step 4; REVIEW.md:32).
15. **[#15 Blocker]** Never label per-replica buckets exact. Reserve, then settle. IN SCOPE in reduced form. **OVERRIDDEN BY REVIEW (R07)**: "One budget authority per metered provider and an atomic reservation set. Unsupported combinations are refused" (REVIEW.md:65). Protective rate limits are kept apart (REVIEW.md:16).
16. **[#16 Blocker]** Make every write repeatable: an idempotency key scoped to principal and address, kept 24 h, and passed downstream. **OVERRIDDEN BY REVIEW (R08)**: "One logical operation per key for its whole retention, uncertain outcomes included. Workers are fenced after their lease and reconciled" (REVIEW.md:66). It is step 3, before metered or changing work. The journey is reads, so it is AMBIGUOUS whether the first build needs it; metered reads need it (review:ATLAS-CORE.md:198).
17. **[#17 Blocker]** Make audit durable before the answer ends, never an in-memory list. **OVERRIDDEN BY REVIEW (R09)**: "Immutable admission, outcome and settlement facts, joined for reading" (REVIEW.md:67). Every attempt, failed authentication included, leaves a fact (review:ATLAS-CORE.md:182). IN SCOPE (step 3).
18. **[#18 Major]** Build the catalogue as a checked snapshot, with a controller, last-known-good, generations and switches. IN SCOPE (the snapshot). **OVERRIDDEN BY REVIEW (R13)**: separate freshness bounds, replacing the 60-minute degraded mode (REVIEW.md:71).
19. **[#19 Major]** Every family with an exemplar. **OVERRIDDEN BY REVIEW**: one journey first (REVIEW.md:11). Resources, actions and events are DEFERRED (REVIEW.md:15, 72).
20. **[#20 Major]** Admit a service only if it is accepted, compatible and conformant, atomically as a generation. IN SCOPE in the R03 form.
21. **[#21 Major]** The service protocol over HTTP/2, with loopback batteries. **OVERRIDDEN BY REVIEW (R20)**: in-process provider conformance first, and remote transport conformance when a provider is first extracted. Keep the boundaries serialisable (REVIEW.md:13, 78). DEFERRED.
22. **[#22 Major]** Operations survive their replica: leases and takeover. IN SCOPE only "where that journey needs them" (review:ATLAS-CORE.md:238). **OVERRIDDEN BY REVIEW (R08, R17)**: see 16 and 34.
23. **[#23 Major]** Resilience: breakers, bulkheads, retry budgets, shedding, in-flight caps, a pre-authentication limit and drain. The edge middleware exactly as §6.7.2 lists it. IN SCOPE as far as the journey's one deployable needs.
24. **[#24 Major]** Serve the built-ins with the exact §9.3.2 schemas. IN SCOPE for those the journey uses: `whoami`, `catalog`, `search`, `describe`, `guide`, `access` (review:ATLAS-CORE.md:238). The rest are AMBIGUOUS: whether all 20 are first-build.
25. **[#25 Major]** The fake warehouse runs the adapter's own statement. Never evaluate filters in hand-written Go. DuckDB runs the Snowflake statement transpiled by sqlglot. IN SCOPE.
26. **[#26 Major]** Telemetry is the OTel API plus slog, with spans per step, redaction at one point and bounded labels. A test plants secrets and searches every sink for them. IN SCOPE (AGENTS.md:47-48).
27. **[#27 Major]** Enforce "services never import `core`". No registration in `init`, no bare `go`, no `os.Getenv` outside configuration: analyzers in `tools/vet`. Done in Phase 0 (REVIEW.md:88). **Applies to every ported file**: the engine, its main and the snowarrow reader all have bare `go`; the client has `os.Getenv`; the issuer has `http.Error`.
28. **[#28 Minor]** Use the spec's serving values (§6.7.15), not pass 1's `:8000`, 64 KiB header bytes, or 5 s then 30 s shutdown. IN SCOPE.

### The clients

29. **[#29 Blocker]** Speak the runner protocol's exact shape (§9.8.3): `value`, `warnings`, `calls`, and options inside `args`. The engine is the reference runner. IN SCOPE (§2.4).
30. **[#30 Major]** The Go client is package `atlas` at the root of `sdk/go`, implementing §9.7: `atlas.Connect`, `.Call`, `.Fetch`, `.Operation`, `.Subscribe`… Nothing capability-specific outside `call`. IN SCOPE, except Subscribe (DEFERRED).
31. **[#31 Major]** Implement the client's environment and local state:
    - the environment order;
    - `ATLAS_HOME`;
    - the shared token store (`0600`/`0700`, atomic writes, refresh within 60 s);
    - sign-in;
    - the profile disk cache;
    - the confirmation flow;
    - the deprecation hook;
    - landing with a manifest;
    - transfers, export, events and generated wrappers, at the declared levels.

    IN SCOPE: the environment order, the token store and the profile cache. **OVERRIDDEN BY REVIEW (R26)**: credentials are bound to the trusted gateway, authority, audience, tenant or account, and scopes; refresh is serialised; "an unauthenticated bootstrap cannot redirect a credential" (REVIEW.md:84). **OVERRIDDEN BY REVIEW (R11)**: "Credentials come from each runtime's own provider" (REVIEW.md:69); a browser needs no token file. Transfers, export and events are DEFERRED.
32. **[#32 Major]** Build the CLI (`clients/cli`), the conformance gateway (`sdk/go/conformance`), and the scenarios and vectors. IN SCOPE: the CLI (REVIEW.md:12) and the vectors. The references exist: 51 entries in `refs:scenarios/`, which is 50 scenario files and `fixtures/` (REVIEW.md:70, R12 "Done"). AMBIGUOUS: whether the conformance gateway is first-build.
33. **[#33 Major]** Give the Python face `AtlasProblem` per code, refs, `read_as` for polars and DuckDB, async, operations, subscribe and publish, export and upload, and the attribute form. Ship per-platform wheels tested on a machine with no Go. IN SCOPE: `AtlasProblem`, `call`, `fetch`, `read_as` (pyarrow and pandas at least), and a wheel with the engine. Subscribe, publish, export and upload are DEFERRED. Async, polars, DuckDB and the attribute form are AMBIGUOUS.

### Process

34. **[#34 Major]** Run load, chaos and fault-injection tests, and an N−1 evolution test. IN SCOPE: the journey's source-failure cases (REVIEW.md:50, "source failures behave the same in every client") and the evolution test in its R03 form. Load is AMBIGUOUS.
35. **[#35 Major]** Trace every requirement: `make requirements` maps every ATLAS- and AP- id to a test, or marks it checked by review. Tests name their ids (AGENTS.md:54). IN SCOPE.
36. **[#36 Minor]** Walk the paved road: `AGENTS.md`, `cmd/atlas-new`, review records per service. `AGENTS.md` exists. IN SCOPE: `atlas-new` (AGENTS.md:64) and the 15-question review.

### What survives from pass 1 (pass-2:docs/pass-2/CRITIQUE.md:67-78), as rules

37. Keep the `snowflake-arrow/1` codec and its list of `ASSUMPTION`s (pass-1:sdk/go/snowarrow/snowarrow.go:6-24). It is already in the pass-2 implementation. DEFERRED in clients with handoff.
38. Keep the final-batch framing: a zero-row batch with the end line as message metadata `atlas:end`. EOF alone is never success (review:ATLAS-CORE.md:206). IN SCOPE, through `protocol/frame`.
39. Keep the handoff reader, with renewal and resume: bounded prefetch, resume at the failed chunk, at most 3 renewals. `reader.go` is **not** in the pass-2 implementation yet. DEFERRED. When it returns, its bare `go` statements (reader.go:82, 91) move onto `kit/conc`, and renewal goes through the handoff's `operation` (spec:09-6-http-binding.md:75), not `Atlas-Renew`.
40. Keep the engine's pipe design: Arrow on a second pipe, cancel, and dying with its parent. IN SCOPE.
41. Keep the Python face's process rules: fork, Ctrl+C and exit. IN SCOPE.
42. Keep the copied kit packages as the start of `kit/serve`. They are carried, with provenance (`pass-2:sdk/go/kit/PROVENANCE.md`).
43. Keep the layer check's shape. Done (`tools/layercheck`; R24).
44. Keep the end-to-end test style: real binaries, with ready lines on stdout. IN SCOPE.

### Lessons from PROOF-0's findings, as rules

45. Never rely on HTTP trailers for completeness; proxies drop them (PROOF-0.md:78).
46. An engine must also end when its parent changes, not only when stdin closes (PROOF-0.md:86-88). On Windows it needs a job object (not done).
47. The engine ignores SIGINT. The face turns Ctrl+C into `cancel` (PROOF-0.md:89).
48. A cancelled command needs a decided report (PROOF-0.md:81-85). See the ambiguities; R17 decides "requested or confirmed".

---

## Ambiguities (collected)

1. **The cancel answer.**
   - §5.1 says the answer is "the problem `deadline_exceeded` or the cancellation" (spec:05-clients.md:237). The registry has no cancellation code (pass-2:sdk/go/protocol/problem.go:107-132).
   - Pass 1 answers `{"ok":false,"cancelled":true}` before the stream, and ends the stream without its final batch once it has begun (PROOF-0.md:81-85).
   - R17: "Cancellation is reported as requested or confirmed" (REVIEW.md:75).
   - Readings:
     - (a) keep `cancelled: true`, and add `cancellation: "requested"|"confirmed"`;
     - (b) answer `deadline_exceeded`;
     - (c) a new registry code. This one needs a spec change.
2. **Where `results: "arrow"` goes.** Pass 1 puts it in the top-level `options`. §9.8.3 moves options into `args.options`. §5.1 says only "A command may ask for `"results": "arrow"`" (spec:05-clients.md:236). Readings: `args.options.results`, or top level beside `args`.
3. **How the stream is named.** Pass 1 uses `result.stream = <id>`. §5.1 says "an answer naming a stream", with no member name. With `value`, readings: `value: {"stream": id}`, or a sibling member `stream`.
4. **The face's metadata in the JSON form.** §9.8.3 makes `value` an array of row records (spec:09-8-conformance.md:53). The face needs `writer`, `reader` and `via` (pass-1:clients/python/fiatlas/client.py:89). Readings: extra members beside `value` (the tolerant reader ignores them), or only in the Arrow-form answer.
5. **What `calls` counts** (spec:09-8-conformance.md:48, `"calls": 1`). It is undefined. Readings: HTTP requests sent, including retries and describes; or calls of the capability alone.
6. **The code for an end-line row-count mismatch.** Pass 1 used `upstream_failed` (pass-1:sdk/go/client/table.go:150-151). Pass 2's `frame` uses `invalid_document` (pass-2:sdk/go/protocol/frame/frame.go:444-446). §9.6.5 and AP-HTTP-002 are not re-read here.
7. **ZSTD for Arrow.** §2.2.1.4 says "Arrow IPC with ZSTD buffer compression" (spec:02-2-1-snowflake-data.md:129). Pass 1 used `ipc.WithZstd()` (edge.go:164). Pass 2's `frame` writes uncompressed (frame.go:338). Whether Arrow JS reads ZSTD-compressed IPC must be checked before Node is promised Columnar.
8. **The fake identity.** The pass-2 implementation has an opaque token table at the `Authenticator` port (pass-2:gateway/fakes/tokens.go). PLAN.md:88 asks for an RS256, JWKS and OIDC fake issuer verified by the real `kit/jwt`. AGENTS.md:58 says never to fake below the code under test. See §3, verdict 7.
9. **The claim for `Identity.Kind`** (person or workload). It is not fixed in the files read (pass-2:gateway/ports/identity.go:15-17).
10. **The token to the engine.** Pass 1 passes it in `connect.args.token`. §9.7.3 has workloads pass it to `connect` or `ATLAS_TOKEN`, and people use the store. R26 binds credentials. Scenarios provide the token file. Readings: support both, as §9.7.3 does; or the store only for people.
11. **The `lock` op.** It is not a §9.7.1 operation. Pass 1 exposes it to Python. Readings: keep it as an engine-only op, or leave locking to the CLI (`atlas lock`, spec:09-7-clients.md:144).
12. **Python features in the first build.** async (ATLAS-PY-005 names "an `asyncio` cancellation"), polars and DuckDB for `read_as`, and the attribute form. REVIEW.md does not list them. Readings: defer all but pyarrow and pandas; or implement PY-005 in full.
13. **The holdings fixture's shape.** Pass 1's data lacks cash rows (null ISIN), `title`, `status`, Money's currency, the portfolio list and freshness data. The owner choices are left to fixtures (REVIEW.md:97). The column names of the `portfolio.list`, `portfolio.search`, `data.status` and `holdings-dates` sources must come from the handbook digest.
14. **The latest-date semantics.** Pass 1 takes the global MAX. The reference profile says "the portfolio's last valuation on or before as_of" (refs:vectors/profiles/data.json:356).
15. **A non-JSON command line.** Pass 1 answers `id: 0`. The id cannot be known; §9.8.3 is silent.
16. **Windows engine lifetime** (PROOF-0.md:88). Readings: a job object now, or record the gap. The wheel targets Windows (spec:05-clients.md:226).
17. **The built-ins' scope.** Is all of CRITIQUE #24 (20 built-ins) first-build, or only the journey's? REVIEW.md:11 says one journey.
18. **The conformance gateway.** Is `sdk/go/conformance` part of the first build? REVIEW.md step 4 names "the journey through the CLI, Python and Node", not the conformance suite.
19. **Idempotency for a read-only journey** (R08). ATLAS-CORE §9 says metered reads need it too (review:ATLAS-CORE.md:198). Readings: build it now, or in step 3 before any metered work.

## IN SCOPE (from this assignment)

- The engine's runner on the exact §9.8.3 shape, with the Arrow results pipe, `cancel`, SIGINT ignored, the orphan exit and one command at a time (§2).
- `atlas-engine` cross-compiled in pure Go, with no DuckDB in its graph (ATLAS-GO-002).
- The Python face's process rules ported nearly verbatim: lazy start, connect first, `register_at_fork` forget, `atexit` stop, the `GRACE` 1.0 s backstop, Ctrl+C turned into cancel, raw fds only.
- The Python face's public interface rewritten to §9.7.1 and R11: `call` canonical, `fetch` shorthand, `AtlasProblem` per code, `read_as` with pandas `ArrowDtype`.
- The Python process tests (Ctrl+C, deadline, fork, fork pool, no orphan, JSON = Arrow, problems) on the journey, in their gateway-path form.
- The evolution test in its R03 form (alias, UNKNOWN enum, added optional field, old request columns), and the added-later test on `portfolio.holdings-dates@v1`, from the CLI, Python and Node.
- Package `atlas` (the Go client), using `protocol/frame`, `headers`, `ijson`, `arrowschema` and `schema`. Built-ins carry no `Atlas-Profile-Expect`. It follows bindings and never builds paths.
- The fake issuer (RS256, `kid`, JWKS, loopback token endpoint, clock injected) and two personas with different portfolio scope plus an outsider.
- The Harbour synthetic generator (splitmix64, deterministic) reshaped to the journey's profiles and owner choices, feeding DuckDB or the fake warehouse. Latest-date per portfolio.
- The adapter assertions ported to sqlglot output: casts, the enum→UNKNOWN `CASE`, `explain` = statement; `QUERY_TAG` out of band.
- The edge test checklist: bootstrap without sign-in, no token is 401 and audited, authorise before validate with no limit spent, the working view, an old request resolved forward through an alias, pinning.
- The e2e style: real binaries, `-addr 127.0.0.1:0`, a JSON ready line, `-pin` for changes from the back.

## DEFERRED (from this assignment)

- The handoff path, everywhere:
  - `handoffAnswer` and renewal (at most 3) and resume;
  - `snowarrow/reader.go`;
  - `Atlas-Renew`;
  - snowfake's signed expiring links, `X-Snowfake-Key` and 24 h results;
  - the `mid-download` and `handoff` cases of the Python tests;
  - `TestExpiredLinksAreRenewedMidRead`;
  - `TestStreamedAndHandedOffRowsAreIdentical`.

  (review:DESIGN-REVIEW.md:176; REVIEW.md:33; R10.)
- Events, subscribe and publish, export, upload and transfers (REVIEW.md:15).
- Ed25519-signed calls and the HTTP/2 service protocol (R20).
- Pass 1's token-bucket limits and in-memory audit (replaced by the R07 and R09 designs in step 3).
- `index.constituents@v1`: not in the journey.
- Confirmation (`options.person` and `confirm`) for metered work (step 3, R06).
- The Windows job object, unless it is decided otherwise (ambiguity 16).
- Cancelling the warehouse query when a streaming client goes away (PROOF-0.md:108; R17 in step 3).
