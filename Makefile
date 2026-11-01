# The one entry point: `make check` proves the tree (docs/pass-2/PLAN.md §1).
# Everything here is hermetic: no network beyond the module proxy, no Azure.

SHELL := /bin/bash
GO ?= go
export GOTOOLCHAIN := local

# Modules with product code, checked by atlasvet; tools is checked by go vet only.
PRODUCT := sdk/go
MODULES := $(PRODUCT) tools
BIN := $(CURDIR)/.bin
ATLAS_SPEC ?= $(HOME)/Documents/ChatGPT/creation/atlas-spec

.PHONY: help check fmt vet staticcheck test tidy vuln requirements requirements-index

help: ## List the targets
	@grep -E '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | sed -E 's/:.*## /\t/' | expand -t 22

check: fmt vet staticcheck test tidy ## Everything: gofmt, vet and atlasvet, staticcheck, tests under -race, tidy modules

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
	@$(GO) -C tools vet ./...

staticcheck: $(BIN)/staticcheck ## staticcheck on every module
	@for m in $(MODULES); do (cd $$m && $(BIN)/staticcheck ./...) || exit 1; done

test: ## Every module's tests under the race detector (the layer check among them)
	@for m in $(MODULES); do $(GO) -C $$m test -race -count=1 ./... || exit 1; done

tidy: ## go mod tidy changes nothing (ATLAS-PACKAGES-004)
	@for m in $(MODULES); do $(GO) -C $$m mod tidy -diff >/dev/null || { echo "go mod tidy would change $$m:"; $(GO) -C $$m mod tidy -diff; exit 1; }; done

vuln: $(BIN)/govulncheck ## Known vulnerabilities in what the product modules call (needs the network)
	@for m in $(PRODUCT); do (cd $$m && $(BIN)/govulncheck ./...) || exit 1; done

requirements: ## The requirement matrix: what proves each of the spec's requirements
	@$(GO) -C tools run ./reqs report -spec "$(ATLAS_SPEC)" -o $(CURDIR)/docs/requirements.md

requirements-index: ## Re-read the spec's requirements into spec/requirements.json
	@$(GO) -C tools run ./reqs index -spec "$(ATLAS_SPEC)"
