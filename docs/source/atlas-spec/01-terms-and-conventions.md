# §1 Terms and conventions

Source: `IMG_2138.MOV` 00:02:04–00:02:22. Manually verified from overlapping full-resolution frames at 02:06, 02:10, 02:14, 02:18 and 02:22.

The words the Atlas spec uses with one meaning each, how the spec is written (requirement words, numbering, algorithms, one home per fact, normative text in §9), and the specifications Atlas takes its patterns from.

### In short

- Every term below has one meaning across the Atlas spec.
- Pages are laid out in layers (writing and presenting the spec); normative text uses RFC 2119 words; anything that crosses the wire is defined exactly in §9.
- Atlas borrows proven patterns and says where it departs from them.

### Detail

#### Terms

| Term | Meaning |
|---|---|
| Capability | A named, versioned concept on one surface: `portfolio.holdings@v1` |
| Surface | What a capability is: data, resource, action or event (a profile's `family`) |
| Interaction | A verb a capability offers: `fetch`, `get`, `restart`, `subscribe` |
| Address | `capability@version:interaction`, the one thing every call resolves to: `portfolio.holdings@v1:fetch` |
| Binding | Where and how one interaction is served: protocol, server, address, encodings, authentication |
| Profile | Everything a capability says about itself, with a fingerprint: its contract |
| Fingerprint | The 16-character hash of a profile's canonical form: `portfolio.holdings@v1#bcaa5bbad97cc612` |
| Writer profile | The profile a payload was written under |
| Reader profile | The profile the reader knows (for an application, the one in its lock) |
| Lens | A declared, closed-form mapping between two profiles of one capability |
| Execution | Who does the work and moves the bytes of a data result: `gateway` (run), `handoff`, `client` (the broker) |
| Run, hand off, broker | The three patterns by which the gateway reaches a system: it does the work; it does the handshake and the bytes go direct; it hands over what the client needs to connect (§6.3) |
| Handover | What a hand-off or broker answer carries: what the client needs, and no broader credential; on the wire a transfer, a handoff, a connect or a ticket (§9.3) |
| On-Behalf-Of (OBO) | The gateway exchanging the caller's token for one, as the person, for another system |
| Handoff | A data result fetched by the client directly from its source's result storage, with no session at the source |
| Atlas Gateway | Atlas's one host: it serves every capability, in Azure, one per environment |
| Client | A library or command that calls the Atlas Gateway, optimised for its language and the roles it serves |
| Client extra | Work a client does on the caller's machine without the gateway (files, git, Parquet, output); never a capability |
| Context | Who calls, in which environment, through which client |
| Available | Granted and not denied, present in the environment, lifecycle allowing it |
| Operation | The resource a long-running interaction returns at once, to poll, watch or cancel |
| Front door | The gateway's edges over one pipeline, which decide every call |
| Service | What does an interaction's work behind the front door, in the gateway's process or a service of its own |
| Edge | An adapter at the front door for one transport class (HTTP, streams, MCP, events) |
| Level | A part of the protocol a client implements and declares: Core, Streaming, Columnar, Bulk, Events, Handoff (Broker deferred) |

#### How the Atlas spec is written

1. **Words.** MUST, MUST NOT, SHOULD, SHOULD NOT and MAY as RFC 2119 defines them; anything else is explanation.
2. **Requirements** are numbered `ATLAS-<AREA>-NNN` in §1 to §8 and `AP-<AREA>-NNN` in the protocol (§9); each is testable or marked as checked by review; a number is never reused.
3. **Algorithms** are numbered steps, as GraphQL's specification writes them.
4. **One home per fact.** A rule or table is stated once; other pages summarise it in a line and link to it.
5. **Normative text.** Everything that crosses the wire is defined in §9; §2 to §6 explain it and never contradict it.
6. **The pattern and the contracts.** The spec owns the pattern; each capability's contract is declared beside the gateway's code and served as its profile, with a generated snapshot in the catalogue.

#### Precedents

| From | Atlas takes |
|---|---|
| Apache Avro | Writer and reader schemas, schema resolution, aliases, enum defaults, canonical form and fingerprints |
| GraphQL | Introspection, deprecation of fields and values, nullability as contract, validation before execution, errors with a path, change classification, how its specification is written |
| Cambria (Ink & Switch) | Lenses |
| Open Knowledge Format | The spec's format, and progressive disclosure through descriptions and index files; the Attested Computation, which a sanctioned capability is |
| Google API Improvement Proposals | Resources and standard methods, field masks, ETags, paging, long-running operations |
| Kubernetes | Served versions of one stored resource; one discovery across aggregated servers |
| AsyncAPI, CloudEvents | Event descriptions and the event envelope |
| RFC 9457, Azure REST guidelines | Problem documents and their members; `202` and `Operation-Location`; the order of access evaluation |
| JSON Schema 2020-12, Confluent's compatibility modes | The schema language; BACKWARD, FORWARD, FULL TRANSITIVE |
| Apache Arrow, Snowflake | Columnar results; result chunks fetched directly (the handoff) |
| Envoy, Dapr, Tailscale | Fail-closed decisions; retries only of replayable requests; a control plane that carries no data |
| Hexagonal architecture, Martin's package principles | The gateway's ports, adapters and layers |

What was rejected, and why: §8.

### Go deeper

- Writing and presenting the spec
- §9 The Atlas Protocol
