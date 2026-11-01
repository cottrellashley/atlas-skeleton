# The one entry point: `make check` proves the tree (docs/pass-2/PLAN.md §1).
# Everything here is hermetic: no network beyond the public module proxy, no Azure,
# and never the private github.com/jhgonline (below).

SHELL := /bin/bash
GO ?= go
export GOTOOLCHAIN := local

# jhgonline is a private organisation, and its paths here are names only.
# Nothing may fetch them. Every in-repo module resolves through go.work and a
# replace (tools/modcheck proves it). If one ever does not, the go command fails
# here instead of asking the public proxy, the checksum database or GitHub.
export GOPRIVATE := github.com/jhgonline
export GOVCS := github.com/jhgonline:off

# gosnowflake's minicore is cgo, and at start writes a native library to disk
# and loads it. Every build and test here leaves it out, as the release does.
export GOFLAGS := -tags=minicore_disabled

# Modules with product code, checked by atlasvet as well. Libraries (sqlglot,
# a port of SQLGlot that keeps its own idioms) and tools get go vet only.
PRODUCT := sdk/go gateway gateway/local gateway/adapter/snowflake/gosf gateway/cmd/gateway clients/cli extensions
LIBS := sqlglot tools
MODULES := $(PRODUCT) $(LIBS)
BIN := $(CURDIR)/.bin
# The spec the requirement tools read: the copy kept with the repository.
export ATLAS_SPEC ?= $(CURDIR)/docs/source/atlas-spec

.PHONY: help check release fmt vet staticcheck test tidy e2e e2e-python e2e-node vuln requirements requirements-index

help: ## List the targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | sed -E 's/:.*## /\t/' | expand -t 22

check: fmt vet staticcheck test tidy release ## Everything: gofmt, vet and atlasvet, staticcheck, tests under -race, tidy modules

fmt: ## gofmt changes nothing
	@out=$$(gofmt -l $(MODULES)); if [ -n "$$out" ]; then echo "gofmt: these need formatting:"; echo "$$out"; exit 1; fi

$(BIN)/atlasvet: $(wildcard tools/vet/*.go tools/vet/cmd/atlasvet/*.go) tools/go.mod
	@mkdir -p $(BIN) && $(GO) -C tools build -o $(BIN)/atlasvet ./vet/cmd/atlasvet

$(BIN)/staticcheck: tools/go.mod
	@mkdir -p $(BIN) && $(GO) -C tools build -o $(BIN)/staticcheck honnef.co/go/tools/cmd/staticcheck

$(BIN)/govulncheck: tools/go.mod
	@mkdir -p $(BIN) && $(GO) -C tools build -o $(BIN)/govulncheck golang.org/x/vuln/cmd/govulncheck

vet: $(BIN)/atlasvet ## go vet, and Atlas's own checks on product code
	@for m in $(PRODUCT); do $(GO) -C $$m vet ./... && $(GO) -C $$m vet -vettool=$(BIN)/atlasvet ./... || exit 1; done
	@for m in $(LIBS); do $(GO) -C $$m vet ./... || exit 1; done

staticcheck: $(BIN)/staticcheck ## staticcheck on every module
	@for m in $(MODULES); do (cd $$m && $(BIN)/staticcheck ./...) || exit 1; done

test: ## Every module's tests under the race detector (the layer check among them)
	@for m in $(MODULES); do $(GO) -C $$m test -race -count=1 ./... || exit 1; done

release: ## The production gateway, pure Go as it ships (PLAN.md P2-4): CGO_ENABLED=0, linux/amd64 and linux/arm64, into .bin
	@mkdir -p $(BIN) && for arch in amd64 arm64; do CGO_ENABLED=0 GOOS=linux GOARCH=$$arch $(GO) -C gateway/cmd/gateway build -trimpath -o $(BIN)/gateway-linux-$$arch . || exit 1; done

tidy: ## go mod tidy changes nothing (ATLAS-PACKAGES-004)
	@for m in $(MODULES); do $(GO) -C $$m mod tidy -diff >/dev/null || { echo "go mod tidy would change $$m:"; $(GO) -C $$m mod tidy -diff; exit 1; }; done

e2e: e2e-python e2e-node ## The journey through the CLI, Python and Node against the development gateway, and the acceptance result
	@python3 -I tests/e2e/acceptance.py

e2e-python: ## The Python face's tests, against the development gateway
	@uv run --project clients/python --with pytest --with pyarrow python -I -m pytest -q clients/python/tests

e2e-node: ## The Node SDK's tests, against the development gateway
	@node --test clients/node/test/*.test.ts

vuln: $(BIN)/govulncheck ## Known vulnerabilities in what the product modules call (needs the network)
	@for m in $(PRODUCT); do (cd $$m && $(BIN)/govulncheck ./...) || exit 1; done

requirements: ## The requirement matrix: what proves each of the spec's requirements
	@$(GO) -C tools run ./reqs report -spec "$(ATLAS_SPEC)" -o $(CURDIR)/docs/requirements.md

requirements-index: ## Re-read the spec's requirements into spec/requirements.json
	@$(GO) -C tools run ./reqs index -spec "$(ATLAS_SPEC)"
