package vet_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/jhgonline/fi-app-atlas/tools/vet"
)

// The testdata packages carry `// want` comments where a check must report.

func TestNoInit(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), vet.NoInit, "example.com/noinit")
}

func TestNoBareGo(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), vet.NoBareGo,
		"example.com/barego", "github.com/jhgonline/fi-app-atlas/sdk/go/kit/conc")
}

func TestNoGetenv(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), vet.NoGetenv,
		"example.com/getenv", "example.com/app/cmd/gateway", "example.com/settings")
}

func TestNoHTTPError(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), vet.NoHTTPError, "example.com/httperror")
}
