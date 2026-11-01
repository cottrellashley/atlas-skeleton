#!/bin/sh
# Every check the skeleton has, all hermetic: formatting, vet, the Go suites
# under the race detector (the layer check among them), modules that tidy
# leaves alone (ATLAS-PACKAGES-004), and the Python face driving the real
# gateway and engine binaries.
set -eu
cd "$(dirname "$0")"

unformatted=$(gofmt -l gateway sdk/go)
if [ -n "$unformatted" ]; then
	echo "gofmt: these need formatting:"
	echo "$unformatted"
	exit 1
fi

for m in sdk/go gateway; do
	echo "== $m"
	go -C "$m" vet ./...
	go -C "$m" test -race -count=1 ./...
done

for m in sdk/go gateway clients/cli; do
	go -C "$m" mod tidy -diff >/dev/null || { echo "go mod tidy would change $m:"; go -C "$m" mod tidy -diff; exit 1; }
done

py=clients/python/.venv/bin/python
if [ ! -x "$py" ]; then
	echo "no Python environment; make one with:"
	echo "  uv venv --python 3.13 clients/python/.venv && uv pip install --python $py pyarrow pandas pytest"
	exit 1
fi
echo "== clients/python"
"$py" -m pytest -q -W error::DeprecationWarning -p no:cacheprovider clients/python/tests
