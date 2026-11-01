// Command atlasvet runs Atlas's own vet checks. Use it as go vet's tool:
//
//	go vet -vettool=$(go env GOPATH)/bin/atlasvet ./...
package main

import (
	"golang.org/x/tools/go/analysis/multichecker"

	"github.com/jhgonline/fi-app-atlas/tools/vet"
)

func main() {
	multichecker.Main(vet.Analyzers...)
}
