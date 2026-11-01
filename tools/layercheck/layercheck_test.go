package layercheck

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestEveryModuleKeepsItsLayers is ATLAS-PACKAGES-001: every layers.toml in
// the repository, at any depth (a nested module such as
// gateway/adapter/snowflake/gosf has its own), passes its rules, and what
// its pure packages link needs no cgo (PLAN.md P2-4, R070, R116).
func TestEveryModuleKeepsItsLayers(t *testing.T) {
	root := filepath.Join("..", "..")
	files, err := Find(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no layers.toml in the repository")
	}
	for _, f := range files {
		t.Run(filepath.Dir(f), func(t *testing.T) {
			r, err := Load(f)
			if err != nil {
				t.Fatal(err)
			}
			pkgs, err := List(filepath.Dir(f))
			if err != nil {
				t.Fatal(err)
			}
			if len(pkgs) == 0 {
				t.Fatal("go list found no packages")
			}
			for _, v := range Check(r, pkgs) {
				t.Error(v)
			}
			impure, err := CheckPure(r, filepath.Dir(f))
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range impure {
				t.Error(v)
			}
		})
	}
}

// TestTheCheckRefusesWhatItShould proves the check itself: each rule catches
// the import it is there to catch, and lets through what it should.
func TestTheCheckRefusesWhatItShould(t *testing.T) {
	r, err := parse(t, `
independent = ["gateway/adapter/*", "gateway/svc/*"]
adapters = "gateway/adapter/..."
adapter_importers = ["gateway/cmd/..."]

[layers]
0 = ["sdk/go/protocol/...", "sdk/go/kit/..."]
1 = ["sdk/go", "gateway/ports"]
2 = ["gateway/core/..."]
3 = ["gateway/adapter/..."]
4 = ["gateway/svc/..."]
6 = ["gateway/cmd/..."]

[[forbid]]
from = "gateway/svc/..."
imports = ["gateway/core/...", "sdk/go/kit/jwt"]
why = "services never import core (§6.5 rule 1); no service verifies tokens (ATLAS-MECH-004)"

[[external]]
from = "sdk/go/protocol/..."
allow = ["github.com/apache/arrow-go/v18/..."]
why = "stdlib and Arrow only"
`)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ from, imports, want string }{
		{"gateway/core/pipeline", "gateway/svc/harbour", "points up"},
		{"gateway/svc/harbour", "gateway/svc/ids", "independent"},
		{"gateway/adapter/cosmos", "gateway/adapter/snowflake", "independent"},
		{"gateway/svc/harbour", "gateway/adapter/snowflake", "only a deployable"},
		{"gateway/svc/harbour", "gateway/core/pipeline", "services never import core"},
		{"gateway/svc/harbour", "sdk/go/kit/jwt", "ATLAS-MECH-004"},
		{"sdk/go/protocol", "github.com/google/uuid", "stdlib and Arrow only"},
		{"gateway/core/pipeline", "gateway/unheard/of", "no layer"},
		{"gateway/unheard/of", "gateway/ports", "no layer"},
		{"sdk/go/kit/conc", "sdk/go", "points up"}, // the most specific pattern decides: sdk/go/kit/... is L0
		{"gateway/adapter/snowflake/duckwh", "gateway/adapter/snowflake", ""},
		{"gateway/cmd/gateway", "gateway/adapter/snowflake", ""},
		{"gateway/svc/harbour", "gateway/ports", ""},
		{"sdk/go/protocol", "github.com/apache/arrow-go/v18/arrow/ipc", ""},
		{"sdk/go/protocol", "encoding/json", ""},
	} {
		expect(t, r, c.from, c.imports, c.want)
	}
}

// R070, §6.5.3: an adapter brings the vendor SDK its system needs, named in
// a rule of its own, and nothing else; the most specific rule decides, as
// it does for layers. Without that, every SDK §6.5.3 gives the adapters
// (azcosmos, MSAL, gosnowflake) was refused.
func TestR070AnAdaptersOwnSDKIsAllowedByName(t *testing.T) {
	r, err := parse(t, `
[layers]
3 = ["gateway/adapter/..."]

[[external]]
from = "gateway/adapter/..."
allow = []
why = "an adapter brings no third-party module the rules do not name"

[[external]]
from = "gateway/adapter/cosmos/..."
allow = ["github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos/..."]
why = "the Cosmos DB adapter is written against azcosmos (§6.5.3)"
`)
	if err != nil {
		t.Fatal(err)
	}
	expect(t, r, "gateway/adapter/cosmos", "github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos", "")
	expect(t, r, "gateway/adapter/cosmos/batch", "github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos/internal", "")
	expect(t, r, "gateway/adapter/entra", "github.com/Azure/azure-sdk-for-go/sdk/data/azcosmos", "no third-party module")
	expect(t, r, "gateway/adapter/cosmos", "github.com/snowflakedb/gosnowflake", "written against azcosmos")
}

// R070, PLAN.md P2-4: the production root has real adapters only. With the
// repository's own gateway/layers.toml, cmd/gateway linking a fake, the
// fixtures, a port suite, a test helper or anything of the development
// gateway is refused; before, a production binary could serve the
// development tokens and policy, and make check stayed green.
func TestR070TheProductionRootLinksNoFakes(t *testing.T) {
	r, err := Load(filepath.Join("..", "..", "gateway", "layers.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, imp := range []string{"gateway/fakes", "gateway/fixtures", "gateway/porttest", "gateway/internal/refs", "gateway/local/devgateway", "gateway/local/duckwh"} {
		expect(t, r, "gateway/cmd/gateway", imp, "real adapters only")
	}
	expect(t, r, "gateway/cmd/gateway", "gateway/adapter/entra", "")
}

// R070: every layers.toml is found, at any depth, and only where a module
// can live: never under a hidden directory, node_modules, a Python cache or
// testdata.
func TestFindReachesEveryDepth(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{
		"gateway/layers.toml", "gateway/adapter/snowflake/gosf/layers.toml", "a/b/c/d/e/layers.toml",
		".git/layers.toml", "clients/node/node_modules/x/layers.toml", "tools/spec/.venv/layers.toml",
		"x/__pycache__/layers.toml", "gateway/testdata/layers.toml", "gateway/notlayers.toml",
	} {
		p := filepath.Join(root, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Find(root)
	if err != nil {
		t.Fatal(err)
	}
	for i := range got {
		got[i], _ = filepath.Rel(root, got[i])
	}
	want := []string{"a/b/c/d/e/layers.toml", "gateway/adapter/snowflake/gosf/layers.toml", "gateway/layers.toml"}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("found %v, want %v", got, want)
	}
}

// R116, PLAN.md P2-4: production stays pure Go. A package a pure rule's
// packages link that has cgo files on a platform it ships for is refused,
// with its build tags applied: so a driver whose cgo a tag removes (as
// minicore_disabled removes gosnowflake's) passes only when built with it.
// The proof runs go list on a module of its own.
func TestR116APureRootLinksNoCgo(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"go.mod":            "module " + strings.TrimSuffix(Repo, "/") + "/gateway\n\ngo 1.25\n",
		"cmd/gateway/m.go":  "package main\n\nimport _ \"" + Repo + "gateway/driver\"\n\nfunc main() {}\n",
		"driver/driver.go":  "package driver\n",
		"driver/native.go":  "//go:build !minicore_disabled\n\npackage driver\n\n// #include <stdlib.h>\nimport \"C\"\n",
		"cmd/other/main.go": "package main\n\nimport \"net\"\n\nvar _ = net.Dial\n\nfunc main() {}\n",
	}
	for name, text := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GOWORK", "off")
	t.Setenv("GOPROXY", "off")
	rule := func(packages string, tags string) Rules {
		r, err := parse(t, `
[[pure]]
packages = [`+packages+`]
platforms = ["linux/amd64", "linux/arm64"]
tags = [`+tags+`]
why = "production is pure Go (PLAN.md P2-4)"
`)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	got, err := CheckPure(rule(`"gateway/cmd/gateway"`, ""), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !strings.Contains(got[0], "gateway/driver") || !strings.Contains(got[0], "native.go") || !strings.Contains(got[0], "pure Go") {
		t.Fatalf("a cgo driver: %v", got)
	}
	if got, err := CheckPure(rule(`"gateway/cmd/gateway"`, `"minicore_disabled"`), dir); err != nil || len(got) != 0 {
		t.Fatalf("the driver with its cgo tagged out: %v %v", got, err)
	}
	// The standard library's cgo (net's resolver) is optional: CGO_ENABLED=0
	// builds it without.
	if got, err := CheckPure(rule(`"gateway/cmd/other"`, ""), dir); err != nil || len(got) != 0 {
		t.Fatalf("the standard library: %v %v", got, err)
	}
	if _, err := CheckPure(rule(`"gateway/cmd/missing"`, ""), dir); err == nil {
		t.Fatal("a pure package that does not exist passed")
	}
}

func expect(t *testing.T, r Rules, from, imports, want string) {
	t.Helper()
	got := Check(r, []Package{{Path: from, Imports: []string{imports}}})
	switch {
	case want == "" && len(got) > 0:
		t.Errorf("%s importing %s should be allowed: %v", from, imports, got)
	case want != "" && (len(got) == 0 || !strings.Contains(strings.Join(got, "\n"), want)):
		t.Errorf("%s importing %s should be refused (%s): %v", from, imports, want, got)
	}
}

func parse(t *testing.T, text string) (Rules, error) {
	f := filepath.Join(t.TempDir(), "layers.toml")
	if err := os.WriteFile(f, []byte(text), 0o600); err != nil {
		return Rules{}, err
	}
	return Load(f)
}
