# §9 The Atlas Protocol

`spec/protocol.md` · proposed

> Part 9 of the Atlas spec, and its only normative definition of what crosses the wire: the language-neutral, versioned protocol between every Atlas client and the Atlas Gateway, strict enough that a conforming client can be written or generated in any language from this text, its machine-readable schemas and its test vectors alone, and proven by the conformance suite.

## Purpose

Atlas has clients in Python, Go and TypeScript today and will have more. This part defines the protocol so exactly that a client **can be implemented, or generated, in any language** from three things alone:

1. **this text** (§9.1 to §9.8);
2. **its machine-readable artifacts:** the JSON Schemas of every protocol document and the grammar, in `references/`;
3. **its test vectors and conformance suite:** inputs and expected outputs for every algorithm, and a suite that drives any client through a language-neutral runner protocol (§9.8).

No implementation is the reference. There are two: `sdk/go/protocol`, in Go, used by the gateway, the conformance gateway, the Go SDK, the CLI and the Python SDK's engine; and `@atlas/protocol`, in TypeScript, used by the Node SDK in Node and in the browser. Both are **implementations** of this part, equal to any other; where one disagrees with this text, the implementation is wrong. Where this text is ambiguous, that is a defect in this text, fixed here first, with a test vector, before any implementation changes.

Because the Go implementation serves the gateway and most clients, a mistake in it would reach all of them at once. Three checks are kept independent of it, and none may be weakened to make it pass:

1. **The vectors and scenarios**, generated from this text by `tools/spec/` (Python, with its own small reference implementations of the algorithms), never from an implementation's output.
2. **The TypeScript implementation**, written from this text and compared with Go case by case (the differential test).
3. **AP-003:** a client written from this part alone, by someone who has not read either implementation, passes the suite at Core.

## Concepts

### 9.A What is normative

| Normative (here) | Explanatory (elsewhere in the Atlas spec) |
|---|---|
| Every document's structure (§9.3 and its JSON Schemas), every grammar (§9.1), every algorithm (§9.4, §9.5), every binding's exact framing (§9.6, §9.7), every client obligation (§9.7), conformance (§9.8) | Why the model is as it is: the surface (§2), contracts and versioning (§3), how clients plug in (§4), the clients (§5), the gateway's internals (§6) |

Where §2 to §6 and this part differ on anything that crosses the wire, this part governs.

### 9.B The protocol's version

The protocol is versioned `atlas/MAJOR.MINOR`, independently of every capability and every client. This text defines `atlas/1.0`.

- A **minor** version only adds: a header, an optional document member, a delivery, a trait, a lens operation, a problem code. A conforming `atlas/1.N` client works unchanged against an `atlas/1.M` gateway for any N and M: it ignores what it does not know (§9.7), and the gateway serves it only what its declared features allow (§9.6).
- A **major** version may change anything; the gateway serves the previous major beside it for at least two years.
- The gateway declares the versions it serves in its bootstrap document (§9.3); a client declares its own in every request.

### 9.C Conformance levels

A client implements one or more levels, declares them (§9.6, `Atlas-Client-Features`), and is tested at those levels. **Core** is required of every client; every other level is independent and is declared on its own. The gateway serves a client that lacks a level the next way down, never refusing it: a handoff becomes a stream, Arrow becomes NDJSON, a subscription becomes polling.

| Level | A client at this level can | Required of |
|---|---|---|
| **Core** | Bootstrap, sign in from the shared token store, discover, describe, validate, call any interaction with `single` and `operation` delivery in JSON, resolve JSON results, report problems, follow deprecations, honour deadlines, idempotency and retries | Every client |
| **Streaming** | `chunked` delivery as NDJSON | The CLI, Python, Node, Go |
| **Columnar** | Arrow IPC results, and resolution on whole columns | The CLI, Python, Node, Go |
| **Bulk** | `transfer` delivery (upload and download by range) and `export` | The CLI, Python, Node, Go |
| **Events** | `subscribe` and `publish` of event capabilities: a connect handover (§9.3), then AMQP to Service Bus directly, as the person | The CLI, Python, Node (in a Node process), Go |
| **Handoff** | `handoff` execution: fetching one result's chunks directly from its source's storage, decoding the declared chunk format, and checking it against the writer profile | Python (Snowflake) first |
| **Broker** | `client` execution: running a ticket's planned statement at its source and reporting it. **Deferred** until its credential meets the credential rule (O54); no client declares it in `atlas/1.0` | — |

`atlas/1.0` has no subscription over HTTP. A client without the Events level polls instead: an event capability's history through the data capability its profile names (`meaning.history`), the catalogue by its ETag, an operation by `get`.

### 9.D Normative references

| Reference | Used for |
|---|---|
| RFC 2119, RFC 8174 | Requirement words |
| RFC 5234 (ABNF) | Grammars |
| RFC 8259 (JSON), RFC 7493 (I-JSON) | Every JSON document |
| RFC 8785 (JSON Canonicalization Scheme) | Fingerprints |
| FIPS 180-4 (SHA-256) | Fingerprints |
| JSON Schema 2020-12 (Core, Validation) | Schemas, in the profile of §9.2 |
| RFC 9485 (I-Regexp) | Every `pattern` |
| RFC 3339 | Dates and times |
| RFC 9110, RFC 9112, RFC 9113 | HTTP semantics, HTTP/1.1, HTTP/2 |
| RFC 9651 (Structured Field Values for HTTP) | Every Atlas header |
| RFC 9457 | Problem documents |
| RFC 8615 | The well-known URI |
| AMQP 1.0, Azure Service Bus | Events, after a connect handover |
| Apache Arrow IPC streaming format, columnar format 1.0 or later | Columnar results |
| CloudEvents 1.0.2, its JSON format | Events |
| RFC 6749, RFC 7636, RFC 8252 | Sign-in |
| W3C Trace Context | Tracing |

## Rules

1. Everything that crosses the wire between an Atlas client and the Atlas Gateway is defined in this part, and nowhere else.
2. Every algorithm in this part has test vectors; an algorithm without them is not finished.
3. Every document has a JSON Schema in `references/schemas/`; the text and the schema agree, and where they do not, the schema is fixed to match the text.
4. A change to this part states the protocol version it belongs to and is listed in the Atlas spec's log.

## Units

The sections §9.1 to §9.8 above.

## Requirements

- **AP-001.** Every Atlas client, in every language, passes the conformance suite at every level it declares.
- **AP-002.** The Atlas Gateway passes the gateway conformance suite.
- **AP-003.** A client written only from this part, its artifacts and its test vectors, by someone who has not read any implementation, passes the conformance suite at Core (the test of this part's completeness).

## Reference sections

- [§9.1 Conventions and grammar](09-1-conventions-and-grammar.md)
- [§9.2 Schemas](09-2-schemas.md)
- [§9.3 Documents](09-3-documents.md)
- [§9.4 Fingerprints](09-4-fingerprints.md)
- [§9.5 Resolution](09-5-resolution.md)
- [§9.6 The HTTP binding](09-6-http-binding.md)
- [§9.7 Clients](09-7-clients.md)
- [§9.8 Conformance](09-8-conformance.md)
