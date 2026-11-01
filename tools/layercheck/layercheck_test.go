package layercheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryModuleKeepsItsLayers is ATLAS-PACKAGES-001: every module with a
// layers.toml passes its rules.
func TestEveryModuleKeepsItsLayers(t *testing.T) {
	root := filepath.Join("..", "..")
	files, err := filepath.Glob(filepath.Join(root, "*", "layers.toml"))
	if err != nil {
		t.Fatal(err)
	}
	more, _ := filepath.Glob(filepath.Join(root, "*", "*", "layers.toml"))
	files = append(files, more...)
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
		})
	}
}

// TestTheCheckRefusesWhatItShould proves the check itself: each rule catches
// the import it is there to catch, and lets through what it should.
func TestTheCheckRefusesWhatItShould(t *testing.T) {
	r, err := parse(`
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
		got := Check(r, []Package{{Path: c.from, Imports: []string{c.imports}}})
		switch {
		case c.want == "" && len(got) > 0:
			t.Errorf("%s importing %s should be allowed: %v", c.from, c.imports, got)
		case c.want != "" && (len(got) == 0 || !strings.Contains(strings.Join(got, "\n"), c.want)):
			t.Errorf("%s importing %s should be refused (%s): %v", c.from, c.imports, c.want, got)
		}
	}
}

func parse(text string) (Rules, error) {
	f := filepath.Join(os.TempDir(), "layers-test.toml")
	if err := os.WriteFile(f, []byte(text), 0o600); err != nil {
		return Rules{}, err
	}
	defer os.Remove(f)
	return Load(f)
}
