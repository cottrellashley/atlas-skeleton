# atlas-skeleton

The hermetic core of Atlas, built only from the transcribed spec (`~/Documents/ChatGPT/creation/atlas-spec`). Its packages are laid out as in `fi-app-atlas` (§6.5), so they can be carried over.

The skeleton answers one question before any outside system is involved: does the pattern hold end to end? It has:

- one front door that decides every call
- a service behind a port
- the Snowflake adapter in front of a fake Snowflake
- one Go client that streams or hands off
- a Python face over that client, which it runs as a program

Contracts change "from the back" while clients stay as they are. Everything runs on a laptop over loopback only.

[PROOF-0.md](PROOF-0.md) records what this proves, what it only assumes, and what it found.

## Run the checks

```bash
./check.sh
```

`check.sh` runs these, in order:

1. `gofmt` and `go vet`
2. every Go test under the race detector, the layer check among them
3. `go mod tidy -diff` on each module
4. the Python tests, against the real gateway and engine binaries

It needs Go 1.25 and a Python environment, set up once:

```bash
uv venv --python 3.13 clients/python/.venv
uv pip install --python clients/python/.venv/bin/python pyarrow pandas pytest
```

## Use it by hand

Start the gateway. It prints one ready line, `{"gateway": "http://127.0.0.1:8070", "storage": "…"}`.

```bash
go -C gateway run ./cmd/gateway
```

Build the engine, and get a development token. The personas are `pm`, `analyst` and `outsider`.

```bash
go -C sdk/go build -o "$PWD/atlas-engine" ./cmd/atlas-engine
curl -s -X POST http://127.0.0.1:8070/.dev/token -d '{"persona":"pm"}'
```

Then, in Python:

```python
import os
from fiatlas import FIAtlas

os.environ["ATLAS_ENGINE"] = "./atlas-engine"
atlas = FIAtlas("http://127.0.0.1:8070", token)
t = atlas.fetch("portfolio.holdings@v1:fetch", {"portfolios": ["147HYC"]}, execution="handoff")
df = t.read_as("pandas")       # Arrow-backed dtypes: decimals stay exact
t.via, t.writer, t.reader      # how the rows travelled, and under which profiles
atlas.lock(["portfolio.holdings@v1"], "atlas.lock")
```

### Gateway flags that change the world behind the client

| Flag | What it does |
|---|---|
| `-pin id@vN=k` | Serve the k-th declared profile as current, as if the later ones weren't deployed yet. `=0` leaves the capability out. Repeatable. |
| `-chunk-rows n` | The number of rows in each result chunk. Small values make many chunks. |
| `-link-ttl d` | How long chunk links work, so they can expire mid-read. |
| `-chunk-latency d` | How long each chunk download waits, like storage far away. |
| `-size small\|large` | The amount of synthetic Harbour data. |
| `-audit file` | Append audit records to a JSONL file. |

## Layout

| Path | Layer | What it is |
|---|---|---|
| `sdk/go/protocol` | L0 | `atlas/0.1`: addresses, profiles, fingerprints, resolution and lenses, problems, Arrow schemas, the final batch |
| `sdk/go/kit` | L0 | Serving mechanics copied from `cloud-gateway-patterns-go` at `44ba81a` (MIT). Adapted lines are marked `ATLAS`. |
| `sdk/go/snowarrow` | L0 | `snowflake-arrow/1`: Snowflake's chunk encoding, decoded once for every client |
| `sdk/go/client` | client | The Go client, the reader side: locks, resolution, streamed and handed-off reads, link renewal |
| `sdk/go/engine`, `sdk/go/cmd/atlas-engine` | client | The client as a program: the runner protocol, Arrow results on a second pipe, cancel |
| `gateway/ports`, `porttest`, `fakes` | L1 | Ports, their suites, and fakes (limits, audit, a token issuer) |
| `gateway/core` | L2 | The catalogue snapshot and governance, the built-ins, the nine-step pipeline |
| `gateway/adapter/snowflake` | L3 | The Snowflake data source. The fake warehouse, `snowfake`, sits behind its `Warehouse` interface. |
| `gateway/svc/harbour` | L4 | `portfolio.holdings@v1`, with three revisions, and `index.constituents@v1` |
| `gateway/edge/http` | L5 | The HTTP binding: bootstrap, calls, NDJSON and Arrow streams, handoffs |
| `gateway/cmd/gateway` | L6 | The composition root, with the development personas |
| `gateway/layers.toml`, `gateway/tools/layercheck` | | The layers and the check that enforces them (§6.5.9) |
| `gateway/e2e` | | End-to-end tests, each against a freshly started gateway binary |
| `clients/python` | | `fiatlas`, the Python face over `atlas-engine` (§5.1) |
| `clients/cli` | | Not built yet |
