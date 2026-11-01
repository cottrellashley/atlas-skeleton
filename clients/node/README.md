# @atlas/sdk

Atlas from Node: `const client = await atlas.connect(); await client.call('portfolio.holdings@v1:fetch', { portfolios: ['147HYC'] })`.

- **Protocol:** `atlas/1.0` (§9).
- **Levels:** Core and Streaming (NDJSON with its end line). It declares no lens operation, so a gateway resolves an answer that needs one (§9.5.6); it resolves aliases, defaults, `UNKNOWN` and projection itself (docs/pass-2/REVIEW.md, R03, R04). Columnar, Bulk, Events and Handoff are not implemented.
- **No runtime dependencies.** Every rule of §9 it needs is here, written from the text and held to `spec/protocol/references/vectors`: strict I-JSON (`src/ijson.ts`), the grammars, RFC 9651 headers, the Atlas Schema validator with I-Regexp, the protocol's 21 documents (`src/schemas/`, the references' own files), RFC 8785 fingerprints, NDJSON framing and resolution.
- **Sign-in:** the store every client on the machine shares, `<ATLAS_HOME>/state/token-<environment>.json`, each sign-in bound to the gateway it was made at (R26). `signIn({ url, accessToken, refreshToken })` writes one, as `atlas login --token` does; `connect({ token })` or `ATLAS_TOKEN` gives a workload's own token instead.
- **A result** is read once as it streams, or kept by `rows()` and read as often as wanted; `complete`, `truncated`, `usage` and `warnings` say what it is. A problem is a thrown `AtlasProblem` with every member of its document.

Tests: `node --test test/*.test.ts` (the journey builds `gateway-local` and the CLI, or takes them from `ATLAS_NODE_TEST_BIN`). `ATLAS_FUZZ_ITERATIONS` and `ATLAS_FUZZ_SEED` run the fuzz targets longer or on another sequence.
