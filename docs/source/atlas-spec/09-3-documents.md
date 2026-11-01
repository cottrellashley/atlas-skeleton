# §9.3 Documents

`spec/protocol/documents.md` · proposed

> Section 9.3 of the Atlas Protocol: every document that crosses the wire (bootstrap, binding, profile, catalogue page, governance, describe, problem, operation, the resource envelope, page, handoff, connect, ticket, transfer, event, the NDJSON end line, the lock, the token store and the built-ins' requests and results), each with its normative JSON Schema; the built-in capabilities the gateway serves; and the protocol's problem registry.

## Purpose

To define every document of the protocol exactly, as a JSON Schema a code generator can read, so every language's client gets the same types.

## 9.3.1 The documents and their schemas

Each schema is in `spec/protocol/references/schemas/`, with `$id` `https://atlas.jhg.com/protocol/1.0/<file>`. The schema is normative for structure; this text is normative for meaning.

| Document | Schema | Sent | Meaning |
|---|---|---|---|
| Bootstrap | `bootstrap.schema.json` | `GET /.well-known/atlas` | The gateway's protocol versions and minimum, its environment, its named servers, the bindings of every built-in capability, how to sign in, the catalogue's current ETag |
| Binding | `binding.schema.json` | In profiles and the bootstrap | Where and how one interaction is served |
| Profile | `profile.schema.json` | `atlas.describe` | A capability's contract (§3.1) |
| Catalogue page | `catalog-page.schema.json` | `atlas.catalog`, `atlas.search` | Capabilities in the working view, a page at a time |
| Governance | `governance.schema.json` | `atlas.describe` | The caller's access, limits and approvals for one capability |
| Describe answer | `describe.schema.json` | `atlas.describe` | Profile, governance, availability, and the calling client's eligibility per interaction |
| Explain answer | `explain.schema.json` | Every data capability's `explain` | What `fetch` would do with this request: the plan, the result schema, the execution, the estimate |
| Problem | `problem.schema.json` | Every failure | RFC 9457 with Atlas's members (§4.1) |
| Operation | `operation.schema.json` | Every operation delivery; `atlas.operation` | Long-running work |
| Page | `page.schema.json` | Every collection result | `items` and `next_cursor` |
| Resource envelope | `resource.schema.json` | Every record of a resource-family capability | `name`, `metadata`, and `status` for a resource the gateway converges (9.3.4) |
| Handoff | `handoff.schema.json` | `fetch` with `handoff` execution | One result's chunk links, their format and version, the source's schema, the writer profile, an expiry (§2.2.1) |
| Connect | `connect.schema.json` | `subscribe` and `publish` of an event capability | What the client needs to listen on, or send to, one Service Bus entity itself: namespace, entity, scope it gets its own token for, its rights, event's profile; no secret (§2.5) |
| Ticket | `ticket.schema.json` | `:ticket` (deferred until O54 closes: the broker) | A credential issued once and its planned work |
| Transfer | `transfer.schema.json` | `transfer` delivery, `:export` | Links to upload to or download from |
| Event | `event.schema.json` | Received over AMQP, after a `connect` handover | A CloudEvent 1.0.2 in its JSON format |
| NDJSON end line | `chunk-end.schema.json` | The last line of every NDJSON answer | Rows sent, truncation, usage, problem |
| Lock | `lock.schema.json` | `atlas.lock`, parsed from TOML | An application's pinned profiles |
| Token store entry | `token-store.schema.json` | The shared sign-in files | One environment's sign-in |
| Built-ins | `builtins.schema.json` | Every built-in call | One `$defs` entry per built-in request and result (9.3.2) |
| Schema meta-schema | `atlas-schema.schema.json` | — | Every schema inside a profile (§9.2) |

The handover of §6.3.4 is, on the wire, one of three documents: a transfer (files, by links), a handoff (a data result's chunks) or a connect (an event entity); the ticket is its fourth kind, deferred.

The grammar of every string is `references/grammar.abnf` (§9.1).

## 9.3.2 The built-in capabilities

The gateway MUST serve these, at the bindings its bootstrap names. They are the only capabilities a client calls without first reading a profile; their request and result schemas are fixed by `atlas/1.0`, as the `$defs` of `builtins.schema.json` named in the last column. A call to a built-in carries no `Atlas-Profile-Expect`; its answer carries `Atlas-Profile` with the built-in's `exact-id`, which `atlas.describe` serves like any other.

| Capability | Interaction | Request | Result | `$defs` |
|---|---|---|---|---|
| `atlas.whoami` | `fetch` | `{}` | `{ principal, name, kind ("person", "workload"), roles[], environment, available_domains[], house_rules }` | `whoami_request`, `whoami_result` |
| `atlas.catalog` | `fetch` | `{ domain?, family?, effect?, previews?: boolean, cursor?, page_size? (1–500, default 100) }` | Catalogue page | `catalog_request`; `catalog-page.schema.json` |
| `atlas.search` | `fetch` | `{ words: string (1–200), cursor?, page_size? }` | Catalogue page, ranked (`score`, `why`) | `search_request`; `catalog-page.schema.json` |
| `atlas.describe` | `fetch` | `{ by: "id", id }` (a capability-id or pinned-id) or `{ by: "exact", exact }` | Describe answer | `describe_request`; `describe.schema.json` |
| `atlas.guide` | `fetch` | `{ by: "id", id }` or `{ by: "exact", exact }`, with `client?` (`cli`, `python`, `node`, `go`, `mcp`) and `max_tokens?` (200–20000, default 4000) | `{ exact, client, markdown, tokens, truncated, revisions }`: the guide of §2.6 | `guide_request`, `guide_result` |
| `atlas.access` | `fetch` | `{ id?, domain?, cursor? }` | Page of `{ id, status, reason, next, held_by?, limits?, interactions? }`, status from §2.6's table | `access_request`, `access_entry` |
| `atlas.usage` | `fetch` | `{ id?, period?, cursor? }` | Page of `{ id, interaction, kind, used, limit, remaining, period, pool? }` | `usage_request`, `usage_entry` |
| `atlas.setup` | `fetch` | `{ harness? }` | `{ steps: [{ name, done, command?, detail }] }` | `setup_request`, `setup_result` |
| `atlas.clients` | `fetch` | `{ cursor? }` | Page of `{ name, language, built_for, roles[], levels[], extras[], version, install }` | `clients_request`, `client_entry` |
| `atlas.compare` | `fetch` | `{ id: capability-id, from: version, to: version, request: object }` | Table: the two results joined on their keys | `compare_request` |
| `atlas.operation` | `get`, `list`, `cancel`, `watch` | `{ name }`; `{ cursor?, state? }` | Operation; page of operations; `watch` is a connect handover to the caller's operation events | `operation_get_request`, `operation_list_request`; `operation.schema.json` |
| `atlas.catalog.changed` | `subscribe` | `{}` | A connect handover; each event's data is `{ catalog_etag }`, one each time the caller's working view may have changed | `catalog_changed_payload`; `connect.schema.json` |
| `atlas.settings` | `get`, `update` | `{}`; `{ fields, settings }` with `If-Match` | The caller's own settings document | `settings_get_request`, `settings_update_request` |
| `atlas.directory` | `fetch` | `{ words: string (2–100), kind?: "person" or "group", cursor? }` | Page of `{ principal, name, kind }` | `directory_request`, `directory_entry` |
| `atlas.grant`, `atlas.role`, `atlas.limit`, `atlas.context`, `atlas.switch` | the standard methods of §2.3, and `atlas.context`'s `publish` and `rollback` | Their resource schemas, served in their profiles | Administration: confidential, held only by `Gateway.Admin` | — |
| `atlas.audit` | `fetch` | `{ day: date, before?: string, page_size? (1–1000, default 200) }` | Page of audit records (confidential, `Gateway.Admin`) | `audit_request`, `audit_record` |

Every member listed without `?` is required; every member with `?` is optional. The `atlas.*` profiles the gateway serves MUST use exactly these schemas.

**House rules** (§2.6) reach every caller through `atlas.whoami`'s `house_rules` (every rule whose capabilities the caller can all use, with the rules' revision) and through each catalogue page's `house_rules` (the rules that name a capability on that page). `atlas.context` is where administrators write them; no ordinary caller reads it.

**A dynamic schema.** A capability whose result's columns are chosen by the caller's own query (`snowflake.query`, `azure.resource-graph`) has the trait `DynamicSchema`: its profile's result schema is an open array of objects; every answer carries the schema of what it returned (the Arrow stream's schema, or, in JSON, a first member `columns` listing each column's name and type); no resolution applies to it (§9.5); its stability is `upstream`; and an application's lock may hold it only as `dynamic = true`, so the build knows its types are not promised.

In every profile, a trait is the object `{ "name": "Projectable", "critical": false }`; `critical: true` means a client that does not implement it must not call the capability (`critical_trait_unsupported`).

## 9.3.3 The problem registry

Every problem's `code` is one of these, or one its capability's profile declares under `errors`. A client that receives a code it does not know treats it by its `status` and `retryable`.

| Code | Status | Retryable | When |
|---|---:|---|---|
| `invalid_document` | 400 | no | A body is not I-JSON, or has duplicate members |
| `invalid_request` | 400 | no | A request fails its schema; `target` is the first failing pointer |
| `unauthenticated` | 401 | no | No valid token; `WWW-Authenticate` says how to sign in |
| `not_granted` | 403 | no | A listed capability or interaction the caller holds no grant for |
| `denied` | 403 | no | A deny refuses it |
| `budget_exceeded` | 403 | no | Reserving the call would pass a budget |
| `approval_required` | 403 | no | An action awaits another principal's approval |
| `confirmation_required` | 428 | no | The call's estimated cost passes the caller's confirmation threshold; `estimate` holds it (`amount`, `unit`), and the call is repeated with `Atlas-Confirm` |
| `not_found` | 404 | no | No such capability, version or resource, or a confidential one the caller does not hold |
| `not_in_environment` | 404 | no | It exists, but not in this environment |
| `idempotency_conflict` | 409 | no | The key was used for a different request |
| `in_progress` | 409 | yes | A request with this `Idempotency-Key` is still running; repeat it after `retry_after` to get its answer |
| `resume_gap` | 409 | no | A stream or subscription can no longer resume from the position given; the client reports the gap and starts again |
| `retired` | 410 | no | The version was retired at its sunset |
| `precondition_failed` | 412 | no | The ETag no longer matches |
| `critical_trait_unsupported` | 422 | no | The client lacks a trait the capability marks critical |
| `resolution_failed` | 422 | no | A payload could not be resolved (§9.5) |
| `upgrade_required` | 426 | no | The client's protocol is below the gateway's minimum |
| `limit_exceeded` | 429 | yes | A rate or concurrency limit; `retry_after` |
| `output_limit_exceeded` | 200 (in the end line or final frame) | no | The answer was truncated at the caller's cap |
| `internal` | 500 | no | A fault in the gateway |
| `upstream_failed` | 502 | yes | The service's upstream failed in a way that may pass |
| `unavailable` | 503 | yes | The service is unhealthy (its circuit is open); `retry_after` |
| `deadline_exceeded` | 504 | no | The call's deadline passed; its work was stopped |

`type` is `https://atlas.jhg.com/problems/<code>`.

## 9.3.4 The resource envelope

Every record of a capability whose family is `resource` carries the envelope of `resource.schema.json` beside its own fields, as Kubernetes and Azure Resource Manager give every object one shape:

| Member | Holds | Written by |
|---|---|---|
| `name` | The resource name (`nautilus/dashboards/d1`; `resource-name` in the grammar): its domain, then collection and id pairs, stable for its life | The caller, at `create`, or the gateway when the profile says it names them |
| `metadata` | `uid`, `etag`, `generation`, `created`, `updated`, `owner`, `labels`, and `deleting` while a delete is finishing | The gateway, except `labels` |
| Its own fields | The desired state: what the caller asks for, as its profile's schema defines | The caller |
| `status` | Only on a resource the gateway converges (it acts on it outside itself): `observed_generation` and `conditions` | The gateway's controller only |

1. **Generation and ETag.** `metadata.generation` moves on every change to the record's own fields or labels, and `metadata.etag` (the same value as the `ETag` header) moves with it. Neither moves when only `status` changes, so a controller's report never fails a caller's `update`.
2. **Server-owned members.** `metadata` (except `labels`) and `status` are ignored in a `create` or `update` body; a field mask that names them is `invalid_request`, `target` the mask entry.
3. **Conditions.** One per `type`; each with `status` (`true`, `false`, `unknown`), a `reason` code, a `message`, the `observed_generation` it was observed for, and `last_transition` (when `status` last changed). Every converged resource has the condition `ready`. A condition whose `observed_generation` is below `metadata.generation` is out of date. Condition types and reasons are open sets: a client keeps one it does not know and never fails on it.
4. **Deleting.** A `delete` of a resource that made something outside the gateway answers at once with `metadata.deleting` set; the record stays, readable, until the gateway has removed what it made, then is gone. A `get` while deleting answers the record; an `update` is `precondition_failed`.
5. **Owned by its profile.** A resource profile's record schema declares `name`, `metadata` and, if it converges, `status`, exactly as `resource.schema.json` (the gate's lint, §3.3.12); a client's generated types take them from this schema.

Operations (`operation.schema.json`) keep their own shape (D15), which already names, owns and dates them.

## Rules

1. Every document the gateway sends validates against its schema; every client validates every document it receives before using it, and treats a document that fails as `invalid_document`. A table's rows answering a request that named `columns` are validated against the result schema projected to those columns (§9.5.2, step 5).
2. The gateway never sends a problem `code` outside 9.3.3 and its capability's declared errors.

## Units

`references/schemas/*.schema.json`, `references/grammar.abnf`, `references/vectors/`.

## Requirements

- **AP-DOC-001.** Every document the gateway sends in the gateway conformance suite validates against its schema.
- **AP-DOC-002.** The gateway serves the built-ins of 9.3.2 with exactly these requests and results.
- **AP-DOC-003.** Every record of a `resource`-family capability validates against `resource.schema.json`; its ETag and generation do not move when only its `status` changes; a field mask naming a server-owned member is `invalid_request`.
