# atlas-skeleton

The Go core of Atlas, built from the transcribed spec (`~/Documents/ChatGPT/creation/atlas-spec`). Its packages are laid out as in `fi-app-atlas` (§6.5), so they can be carried over.

**Pass 2 is in progress** on the `pass-2` branch.

- It rebuilds the core to the rewritten spec (229 requirements), as if it were the last pass.
- Every external system sits behind a port, with a hermetic fake and a real adapter written against the vendor's SDK, so plugging a service in is configuration.
- The plan, its phases and its exit criteria are in [docs/pass-2/PLAN.md](docs/pass-2/PLAN.md), and the review of pass 1 is in [docs/pass-2/CRITIQUE.md](docs/pass-2/CRITIQUE.md).
- Pass 1, the hermetic proof of the pattern, is kept on the `pass-1` branch. Its record is [docs/pass-1/PROOF-0.md](docs/pass-1/PROOF-0.md).

## Commands

```bash
make check          # gofmt, go vet + atlasvet, staticcheck, tests under -race (layer check included), tidy
make requirements   # the requirement matrix: what proves each of the spec's 229 requirements
make help           # every target
```

The commands need Go 1.25. `GOTOOLCHAIN=local` is set, so Go never downloads a newer toolchain.

## Layout today

| Path | What it is |
|---|---|
| `sdk/go/protocol` | The protocol, being rebuilt in Phase 1 |
| `sdk/go/snowarrow` | `snowflake-arrow/1`: Snowflake's chunk encoding, decoded once for every client |
| `sdk/go/kit` | The serving mechanics from `cloud-gateway-patterns-go` (MIT; `sdk/go/THIRD_PARTY_NOTICES.md`), being completed in Phase 2 |
| `tools/vet` | `atlasvet`: the spec's vet checks (no `init` registration, no bare `go`, settings only in roots, one error envelope) |
| `tools/layercheck` | The layer check, run against every module's `layers.toml` |
| `tools/reqs` | The requirement matrix |
| `spec/requirements.json` | The spec's requirements, indexed by `make requirements-index` |
| `AGENTS.md` | The rules every person and agent working here follows |
