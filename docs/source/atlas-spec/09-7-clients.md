# §9.7 Clients

`spec/protocol/clients.md` · proposed

> Section 9.7 of the Atlas Protocol: what every client in every language MUST do: the abstract client interface and how its names map to each language; choosing the gateway and environment; the shared sign-in store; the call algorithm step by step; the profile cache and the lock; problems as errors; retries, deadlines and idempotency; subscriptions; generated typed wrappers; and the extras a client may offer without the gateway.

## Purpose

To define a client so exactly that two clients in two languages behave the same for the same inputs, and so that a new language's client can be generated from this section, the documents' schemas and the conformance suite.

## Concepts

### 9.7.1 The abstract client interface

Every client exposes these operations (the Core level; others where its levels include them). Names follow the language's conventions by fixed rules: Python `snake_case`, TypeScript `camelCase`, Go exported `PascalCase`, a CLI `kebab-case` subcommands; the semantics are identical.

| Operation | Inputs | Output | Level |
|---|---|---|---|
| `connect` | Optional gateway URL or environment; optional token | A connected client | Core |
| `whoami` | — | The `atlas.whoami` result | Core |
| `catalog` | Optional domain, family, effect | An iterator over catalogue items | Core |
| `search` | Words | An iterator over catalogue items | Core |
| `describe` | An id, pinned id or exact id | The describe answer | Core |
| `guide` | An id, pinned id or exact id; optional `max_tokens` | The guide, in the calling client's syntax | Core |
| `explain` | Data shorthand for `call(id:explain, request)` | The explain answer | Core |
| `access` | Optional id or domain | An iterator over access entries | Core |
| `call` | An address, a request record, options (deadline, idempotency key, execution, accept) | The resolved result: a record, a page iterator, an operation handle, a ticket or a transfer | Core |
| `fetch` | Data shorthand for `call(id:fetch)` | Rows in the language's native form | Core (JSON); Columnar (Arrow) |
| `operation` | An operation name | A handle: `get`, `wait` (with timeout), `cancel` | Core |
| `subscribe` | An event capability, filter, optional durable name | An iterator (or channel) of resolved events | Events |
| `publish` | An event capability and a payload | The sent event's `id` | Events |
| `export` | A data address and request, a format | Downloaded files, or a local path | Bulk |
| `upload` | A transfer, bytes or a file | The committed result | Bulk |
| `run_ticket` | A ticket | Rows, run at the source, then settled | Broker |

`call` is the only operation that touches capabilities other than the built-ins; every other is defined in terms of `call` and the built-ins. A client MUST NOT offer an operation written for one capability, except as a generated wrapper (9.7.11). It MAY offer extras that need no gateway (9.7.12).

**One shape in every language.** The request is the same JSON record in every client, member for member: `columns`, `filters`, `as_of` and every other parameter the request schema declares are members of the request, never options. Options carry only what §9.6 sends as headers or what the client does itself (deadline, idempotency key, execution preference, accepted encodings, the output form). The names, by language:

| Operation | Python (`fiatlas`) | TypeScript (`@atlas/sdk`) | Go (`sdk/go`) | CLI (`atlas`) |
|---|---|---|---|---|
| `connect` | `FIAtlas(url=None, env=None, token=None)` | `atlas.connect({ url?, env?, token? })` | `atlas.Connect(ctx, atlas.Options{URL, Env, Token})` | `--env`, `ATLAS_URL`, `ATLAS_ENV` on any command |
| `whoami` | `.whoami()` | `.whoami()` | `.Whoami(ctx)` | `atlas whoami` |
| `catalog` | `.catalog(domain=None, family=None, effect=None)` | `.catalog({ domain?, family?, effect? })` | `.Catalog(ctx, atlas.CatalogQuery{})` | `atlas catalog [--domain d] [--family f] [--effect e]` |
| `search` | `.search(words)` | `.search(words)` | `.Search(ctx, words)` | `atlas search <words>` |
| `describe` | `.describe(id)` | `.describe(id)` | `.Describe(ctx, id)` | `atlas describe <id>` |
| `guide` | `.guide(id, max_tokens=None)` | `.guide(id, { maxTokens? })` | `.Guide(ctx, id, opts...)` | `atlas guide <id>` |
| `explain` | `.explain(id, request)`; and `ref.explain()` on a reference | `.explain(id, request)` | `.Explain(ctx, id, request)` | `atlas explain <id> [--<param> v …]` |
| `access` | `.access(id=None, domain=None)` | `.access({ id?, domain? })` | `.Access(ctx, atlas.AccessQuery{})` | `atlas access [<id>] [--domain d]` |
| `call` | `.call(address, request, *, deadline=None, idempotency_key=None, execution=None, accept=None)` | `.call(address, request, { deadline?, idempotencyKey?, execution?, accept? })` | `.Call(ctx, address, request, opts...)` | `atlas call <address> --request <json>`; and per family `atlas fetch|list|create|update|delete|plan|run <id>` with flags from the request schema |
| `fetch` | `.fetch(id, request)` → a reference with `.read_as(kind)`; and `.via`; and `atlas.<domain>.<name>(**request)` | `.fetch(id, request, options?)` → an Arrow Table or rows | `.Fetch(ctx, id, request, opts...)` → an Arrow record reader | `atlas fetch <id> [--<param> v …] [--format f] [--out file]` |
| `operation` | `.operation(name)` → `.get()`, `.wait(timeout)`, `.cancel()` | `.operation(name)` → `get()`, `wait(timeout)`, `cancel()` | `.Operation(name)` → `Get`, `Wait`, `Cancel` | `atlas operation get|watch|cancel <name>` |
| `subscribe` | `async for e in .subscribe(id, filter=None, durable=None)` | `for await (const e of .subscribe(id, { filter?, durable? }))` | `.Subscribe(ctx, id, atlas.Filter{}, opts...)` → a channel | `atlas subscribe <id> [--filter key] ` (NDJSON) |
| `publish` | `.publish(id, payload)` | `.publish(id, payload)` | `.Publish(ctx, id, payload)` | `atlas publish <id> --payload <json>` |
| `export` | `.export(id, request, format, path)` | `.export(id, request, { format, path })` | `.Export(ctx, id, request, format, dir)` | `atlas export <id> --format f --out dir/` |
| `upload` | `.upload(transfer, source)` | `.upload(transfer, source)` | `.Upload(ctx, transfer, source)` | Used by commands whose interaction has `transfer` delivery: `--file` |

A bare `id` (no `@vN`) is allowed at a terminal and in a notebook; a program pins through its lock (9.7.9). Python's attribute form `atlas.portfolio.holdings(...)` is `fetch` (or the family's one read interaction) of the id it spells, with `_` for `-`; it adds nothing else.

### 9.7.2 Choosing the gateway and environment

In this order, the first that is set wins:

1. The gateway URL or environment given to `connect`.
2. The environment variable `ATLAS_URL` (a gateway URL).
3. The environment variable `ATLAS_ENV` (an environment name).
4. `env` in the configuration file `<ATLAS_HOME>/config.toml`.
5. Production.

`ATLAS_HOME` is the environment variable of that name if set, else `~/.atlas`. An environment name maps to its gateway URL through `config.toml`'s `[environments.<name>] url`, or the built-in names `dv`, `ac` and `pd`. A gateway run for development or conformance is named by its URL.

### 9.7.3 The shared sign-in store

Every client in every language uses one store, so one sign-in serves them all:

- One file per environment: `<ATLAS_HOME>/state/token-<env>.json`, valid against `token-store.schema.json` (`access_token`; `expires_at`, seconds since the epoch; `refresh_token`).
- The file and its directory are readable and writable only by the person (mode `0600` and `0700`); a client refuses to use a file with wider permissions.
- A client writes the file atomically (to a temporary file in the same directory, then renamed).
- A token within 60 seconds of `expires_at` is refreshed first, with the `refresh_token`, at the bootstrap's `sign_in` authority.
- When there is no usable token, a client with a person present runs the sign-in of §6.2 (browser with PKCE, or device code); without a person, it fails with `unauthenticated`.
- A workload passes a token to `connect`, or sets `ATLAS_TOKEN`; it never writes the store.

### 9.7.4 The call algorithm

To `call(address, request, options)`:

1. Parse `address` (§9.1.3); fail with `invalid_request` if it does not match.
2. Find the profile: if the application has a lock with this capability, its `exact-id`; else, if the address is pinned, the newest cached profile of that version, else of the current stable version. Fetch it with `atlas.describe` if not cached (9.7.5).
3. If the capability has a trait marked critical that the client does not implement, fail with `critical_trait_unsupported` without calling.
4. Find the interaction; fail with `not_found` if the profile has none of that name.
5. Validate `request` against the interaction's request schema (§9.2.5); fail with `invalid_request` if invalid.
6. Choose the binding: the first of the interaction's bindings whose protocol, encodings and delivery the client supports.
7. Build the call (§9.6 or §9.7): the body, `Atlas-Client`, `Atlas-Client-Features`, `Atlas-Profile-Expect` (the profile's `exact-id`), `Atlas-Deadline` (`options.deadline`, else now plus the interaction's `deadline.default`, never beyond `deadline.max`), `traceparent`, and for a non-`read` effect `Idempotency-Key` (`options.idempotency_key`, else a new random UUID, kept for every retry of this call).
8. Send it. On a problem: if `retryable` and the request is replayable (it has no streamed body), retry by 9.7.7; otherwise raise it (9.7.6).
9. Read `Atlas-Profile`. If its fingerprint is not cached, fetch that profile.
10. Read the answer by its delivery (§9.6.5), validating each document. For a handoff: download every chunk, decode it by the handoff's `format` into the writer profile's Arrow schema, and check each batch's schema against the writer profile (`invalid_document` on a mismatch). Resolve every record from the writer's profile to the profile of step 2 (§9.5).
11. If `Deprecation` was sent, emit the language's warning (Python `DeprecationWarning`, Node `process.emitWarning`, Go the client's warning hook, the CLI on standard error) naming what is deprecated, its sunset and replacement.
12. Return the result in the language's native form.

### 9.7.5 Profiles and the catalogue in the client

- Profiles are cached by `exact-id`, forever: a fingerprint's profile never changes. The cache is at `<ATLAS_HOME>/cache/profiles/` for clients with a file system, in memory otherwise.
- The catalogue is cached per environment and principal for at most one hour, revalidated with its ETag at every `connect`, and refreshed when a long-lived client's subscription to `atlas.catalog.changed` reports a change.
- A client recomputes every profile's fingerprint (§9.4) and refuses a mismatch.
- **A capability the client has not seen is never refused by the client.** An address missing from the cached catalogue is looked up with `atlas.describe` before anything is answered; only the gateway says `not_found`. A long session sees a capability added mid-session on its next search or call, with no restart.

### 9.7.6 Problems as errors

Each problem becomes the language's error, carrying every member of the problem document:

| Language | Form |
|---|---|
| Python | An exception `AtlasProblem` (subclass per `code` where the client defines one), with attributes for each member |
| TypeScript | A thrown `AtlasProblem extends Error`, with each member as a property |
| Go | An `*atlas.Problem` implementing `error`, matchable with `errors.As`, with each member as a field |
| CLI | The problem on standard error (as JSON with `--json`), and the exit code below |

CLI exit codes: `0` success; `2` `invalid_request`; `3` `unauthenticated`; `4` `not_granted`, `denied`, `budget_exceeded`, `approval_required`, `confirmation_required`; `5` `not_found`, `not_in_environment`, `retired`; `6` `limit_exceeded`, `unavailable`, `in_progress`; `7` `deadline_exceeded`; `8` `resolution_failed`, `critical_trait_unsupported`, `upgrade_required`; `1` anything else.

On `confirmation_required`, a client with a person present shows them the problem's `estimate` and, if they accept, repeats the call once with `Atlas-Confirm` set to that amount and the same `Idempotency-Key`; without a person, it raises the problem.

### 9.7.7 Retries

A client retries only a `retryable` problem on a replayable request, at most 3 times, waiting `retry_after` when given, else `min(30, 0.5 × 2^attempt)` seconds plus a random jitter of up to 25%, and never past the call's deadline.

### 9.7.8 Subscriptions and publishing (the Events level)

A subscription is an iterator (Python `async for`, TypeScript `for await`, Go a channel, the CLI NDJSON lines) that:

1. calls `subscribe` (with its filter and durable name) and receives a connect handover (`connect.schema.json`);
2. obtains its credential for it: for `auth.kind entra-scope`, a token for `auth.scope` from the shared sign-in (§9.7.3), silently, never sent to the gateway; for `emulator`, the connection string, which a client accepts only when the bootstrap's `environment` is `conformance` or `local`, and otherwise refuses with `invalid_document`;
3. opens the `entity` on `namespace` over AMQP with the language's Azure Service Bus library, in `settle` mode;
4. for each message: parses the CloudEvent (`event.schema.json`); drops one whose `id` it has seen in the last 10,000 events; resolves its `data` from the profile named by `dataschema` to the reader profile (§9.5); yields it; and completes the message after the caller has taken it (abandons it if the caller raised);
5. renews its token before it expires, and asks the gateway again before the handover's `expires`;
6. on a dropped connection, reconnects with backoff (as 9.7.7, without a retry limit); on a refusal by Service Bus, asks the gateway again, which answers the subscription's current status (`not_granted` once a grant is revoked) and ends the iterator with that problem;
7. ignores event types it does not know, and ends when the caller closes it.

To `publish(id, payload)`: validate `payload` against the event profile's `payload` schema (refusing an invalid one with `invalid_request`, ATLAS-EVENT-001); call `publish` for a connect handover with rights `send`; wrap the payload as a CloudEvent (`id` a new UUID, `type` from §2.5, `source` the client's name, `dataschema` from the profile, `time` now); send it; return its `id`.

A client without the Events level, or a page that cannot reach Service Bus, polls instead (§9.C).

### 9.7.9 The lock

A client reads `atlas.lock` (TOML, valid against `lock.schema.json` once parsed) from the application's root (the current directory or the nearest parent containing one, or a path given to `connect`). Its entries are the reader profiles of step 2. `atlas lock` writes it; `atlas lock upgrade` moves entries forward after checking resolution.

### 9.7.10 The tolerant reader

A client MUST ignore unknown members of every document, unknown headers, unknown event types and unknown traits not marked critical, and map unknown enum values by §9.5.5. It MUST NOT fail because the gateway is newer than it.

### 9.7.11 Generated wrappers

A generator MAY produce typed wrappers from an application's lock: one function or method per locked capability and interaction, with typed parameters and results generated from the profile's schemas (JSON Schema to the language's types; table rows to records or data frames). A wrapper MUST call `call` of 9.7.4, and nothing else; it adds types, never behaviour. Wrappers are generated at the application's build, never shipped in a client library.

### 9.7.12 Extras

A client MAY offer **extras**: features it runs itself, without the gateway, because its language and the people it serves need them (the Node SDK reading the person's files and git checkouts for a dashboard on Nautilus Desktop; the Python SDK writing Parquet or caching a result in memory). An extra:

1. is part of its client's own interface, never named or listed as an Atlas capability, and never in `atlas.catalog`;
2. never reaches the platform's data or work except through `call`, so every such call is still decided and audited by the gateway;
3. runs as the caller, with only the caller's own sign-ins on their machine, and never sends those credentials to the gateway;
4. is documented on its client's page in §5, with what it needs to run (a file system, a signed-in `gh`), and tested by its client's own tests (it is not part of the protocol's conformance suite);
5. is declared by any application or dashboard that uses it, so the application says where it can run.

### 9.7.13 Where results land

Every result lands in one agreed form, so the client package and its extras always know where to find it. Most results land in **memory**; a result lands on disk only when it is files by nature, or the caller asks.

| Result | Lands | The agreed form |
|---|---|---|
| A `record`, a `collection`'s items, an operation, any protocol document | Memory | The language's own values, decoded from JSON |
| A `table`, streamed or handed off | Memory | Arrow record batches in the writer's schema, resolved to the reader's, yielded batch by batch (a whole table only when the caller asks for one). Within one process the batches are shared with no copy (the Arrow C Data Interface); from the engine to the Python face they cross as an Arrow IPC stream, read straight into the language's Arrow with no per-value conversion |
| `content` (bytes with a media type and its metadata record) | **Memory by default:** a byte stream the caller reads, with its metadata. **Disk** when the caller gives a path (`options.path`), asks to save without one, or the content is larger than the client's memory threshold (`options.max_memory`, default 256 MiB) | On disk: the place, writing and manifest below |
| A transfer's download, an export's files, a handoff kept as files on request | **Disk** | The place, writing and manifest below |

**The disk place**, for whatever lands on disk:

1. **The place.** The path the caller gave (a file, or a directory for several files), or else `<ATLAS_HOME>/downloads/<YYYY-MM-DD>/<request id>/`. The file's name is the content's `name` from its metadata record when it has one, else `content`, with the extension of its media type.
2. **Written safely.** Each file is written to a temporary name in the same directory and renamed when complete; its `sha256` is checked where the answer or transfer gives one, and a mismatch is `invalid_document` with the partial file removed. An existing file is never overwritten unless the caller asked (`options.replace`).
3. **Described.** Beside the files, `atlas-manifest.json` records, for each file: `name`, `path`, `media_type`, `bytes`, `sha256`, the `address` and the writer's `exact-id` it came from, the `request_id`, and the content's metadata record unchanged. The client returns the manifest; the CLI prints it with `--json`.
4. **Left there.** A client never deletes what it landed; the caller, or an extra acting for them, does.

A client with no file system (a page) keeps everything in memory, and refuses a disk landing with `invalid_request`.

## Rules

1. A client implements 9.7.1 to 9.7.10 and 9.7.13 at the levels it declares, exactly.
2. A client has no behaviour for any one capability beyond the built-ins.

## Units

`references/schemas/token-store.schema.json`, `lock.schema.json`.

## Requirements

- **AP-CLIENT-001.** Two clients at the same levels, given the same gateway, inputs and conformance scenario, make the same calls and return the same results and problems.
- **AP-CLIENT-002.** A token written by any client is used by every other client on the same machine.
- **AP-CLIENT-004.** A client given an address added to the gateway after its catalogue was cached calls it successfully, with no restart and no cache cleared by hand.
- **AP-CLIENT-005.** Every client lands the same result in the same form for the same call and options: a table as Arrow in memory, content in memory under the threshold and at the same disk place with the same manifest when saved or larger (the conformance scenarios `CORE-CONTENT-001` and `-002`).
- **AP-CLIENT-003.** The same request record, built from the same inputs, is byte-for-byte the same JSON from every client (canonicalised by RFC 8785): no client moves a request member into its options.
- **AP-EVENTS-001.** A client at the Events level receives every event sent to its subscription at least once, drops duplicates by `id`, resolves each to its reader profile, and never sends its Service Bus credential to the gateway.
- **AP-EVENTS-002.** A client refuses an `emulator` connect handover from a gateway whose environment is not `conformance` or `local`.
