# Document-review coverage

This ledger records the technical material covered by the historical editorial review of the specification. All 34 substantive chapters were reviewed. The entries describe documentation coverage, not conformance results, implemented capabilities or live-service verification. Later implementation evidence is recorded in the [handoff progress](../../HANDOFF-PROGRESS.md).

| Chapter | Recorded review coverage |
|---|---|
| [00-overview.md](00-overview.md) | Product overview, client and gateway boundaries, extras and direct handoff. |
| [00-vision.md](00-vision.md) | Product purpose, caller roles, client choices and agent workflows. |
| [01-terms-and-conventions.md](01-terms-and-conventions.md) | Terms, conventions, rules and architectural precedents. |
| [02-the-surface.md](02-the-surface.md) | Surface overview, capability seams and detail table. |
| [02-1-capabilities.md](02-1-capabilities.md) | Names, requirements, families, and concept/interaction/binding relationships. |
| [02-2-data-surface.md](02-2-data-surface.md) | Data requests and the three result-delivery paths. |
| [02-2-1-snowflake-data.md](02-2-1-snowflake-data.md) | Snowflake execution, type mappings, requirements and unresolved spikes. |
| [02-2-2-vendor-api-data.md](02-2-2-vendor-api-data.md) | Vendor flow, platform adapters, requirements and shared concerns. |
| [02-3-resources.md](02-3-resources.md) | Resource envelope, methods, preconditions and update sequence. |
| [02-4-actions.md](02-4-actions.md) | Plan, approve and run interactions, operation state diagram and rules. |
| [02-5-events.md](02-5-events.md) | Event rules, connect handover and identity/broker exchange. |
| [02-6-discovery.md](02-6-discovery.md) | Discovery requirements, examples and availability decisions. |
| [03-contracts.md](03-contracts.md) | Profiles, governance, availability, compatibility, locks, resolution and lens examples. |
| [04-plugging-in.md](04-plugging-in.md) | Call sequence, problems, operation lifecycle, MCP tools and binding encodings. |
| [05-clients.md](05-clients.md) | Four client families, Python engine, Node extras, CLI command matrix and Go surface. |
| [06-inside-the-gateway.md](06-inside-the-gateway.md) | Front door, services, state, audit, sources and event flow. |
| [06-1-front-door-and-services.md](06-1-front-door-and-services.md) | Nine-step lifecycle, edge matrix, deployables, ingress, observability and blast radius. |
| [06-2-access-limits-and-audit.md](06-2-access-limits-and-audit.md) | Identity paths, access requirements, decision order, limits, audit storage and attack matrix. |
| [06-3-reaching-systems.md](06-3-reaching-systems.md) | Run, handoff and broker routes; credentials, identities, system register and handover members. |
| [06-4-data-sources.md](06-4-data-sources.md) | DataSource port operations, adapters, tools and interface flow. |
| [06-5-packages.md](06-5-packages.md) | Package layers, dependency rules, lifecycle, state ownership and decisions. |
| [06-6-principles.md](06-6-principles.md) | Fifteen principles, deliberate differences, review questions and constitution. |
| [06-7-implementation-patterns.md](06-7-implementation-patterns.md) | 24 mechanism requirements, pipeline order and sections 6.7.1–6.7.18. |
| [07-the-catalogue.md](07-the-catalogue.md) | 27 domain sections and 138 unique capability rows; duplicate IDs checked. |
| [08-design-decisions.md](08-design-decisions.md) | 52 decided rows, 15 rejected alternatives, historical gaps and 52 open rows; identifier uniqueness checked. |
| [09-atlas-protocol.md](09-atlas-protocol.md) | Normative boundary, version rules, conformance levels, references and requirements. |
| [09-1-conventions-and-grammar.md](09-1-conventions-and-grammar.md) | Identifier/date/decimal grammar, seven JSON rules, ten media types and protocol requirements. |
| [09-2-schemas.md](09-2-schemas.md) | Allowed/excluded schema keywords, formats, logical types, validation and twelve Arrow mappings. |
| [09-3-documents.md](09-3-documents.md) | 21 documents, 16 built-in capabilities, 24 problem codes, resource envelope and rules. |
| [09-4-fingerprints.md](09-4-fingerprints.md) | Projection, StripSchema, fingerprint algorithm, uses, vectors and requirements. |
| [09-5-resolution.md](09-5-resolution.md) | Reader/writer algorithm, six lens operations, unit and value conversions, execution locations and Columnar behavior. |
| [09-6-http-binding.md](09-6-http-binding.md) | 21 headers, feature grammar, nine delivery rows, stream framing, operations, transfers, sign-in and caching. |
| [09-7-clients.md](09-7-clients.md) | 16-operation interface, four-language mapping, sign-in, call algorithm, caches, retry, subscription, lock, wrappers and landing. |
| [09-8-conformance.md](09-8-conformance.md) | Vectors, runner protocol, gateway capabilities, 50 scenarios, passing rules and decisions. |
