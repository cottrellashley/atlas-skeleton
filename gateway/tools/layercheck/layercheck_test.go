// Package layercheck is the layer check (§6.5.9, ATLAS-PACKAGES-001): it
// reads every package's imports with go list and fails on any import that
// points up a layer, crosses an independent group, reaches an adapter from
// anything but a deployable, or is forbidden outright, as layers.toml
// declares.
package layercheck

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

const repo = "github.com/jhgonline/fi-app-atlas/"

type rules struct {
	Layers           map[string][]string `toml:"layers"`
	Independent      []string            `toml:"independent"`
	Adapters         string              `toml:"adapters"`
	AdapterImporters []string            `toml:"adapter_importers"`
	Forbid           []struct {
		From    string   `toml:"from"`
		Imports []string `toml:"imports"`
		Why     string   `toml:"why"`
	} `toml:"forbid"`
}

// pkg is a package as go list describes it, paths relative to the repository.
type pkg struct {
	Path    string
	Imports []string
}

func TestEveryImportOfTheGatewayIsAllowed(t *testing.T) {
	var r rules
	if _, err := toml.DecodeFile("../../layers.toml", &r); err != nil {
		t.Fatal(err)
	}
	pkgs, err := list("../..")
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) == 0 {
		t.Fatal("go list found no packages")
	}
	for _, v := range violations(r, pkgs) {
		t.Error(v)
	}
}

// TestTheCheckRefusesWhatItShould proves the check itself: each rule catches
// the import it is there to catch.
func TestTheCheckRefusesWhatItShould(t *testing.T) {
	var r rules
	if _, err := toml.DecodeFile("../../layers.toml", &r); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		from, imports, want string
	}{
		{"gateway/core/pipeline", "gateway/edge/http", "points up"},
		{"gateway/svc/harbour", "gateway/svc/ids", "independent"},
		{"gateway/adapter/cosmos", "gateway/adapter/snowflake", "independent"},
		{"gateway/svc/harbour", "gateway/adapter/snowflake", "only a deployable"},
		{"gateway/edge/http", "gateway/adapter/snowflake", "only a deployable"},
		{"gateway/svc/harbour", "sdk/go/kit/jwt", "ATLAS-MECH-004"},
		{"gateway/core/pipeline", "sdk/go/client", "reader side"},
		{"gateway/core/pipeline", "gateway/unheard/of", "no layer"},
		{"gateway/unheard/of", "gateway/ports", "no layer"},
		{"gateway/adapter/snowflake/snowfake", "gateway/adapter/snowflake", ""},
		{"gateway/cmd/gateway", "gateway/adapter/snowflake", ""},
		{"gateway/core/builtins", "gateway/core/catalog", ""},
		{"gateway/svc/harbour", "gateway/ports", ""},
	} {
		got := violations(r, []pkg{{Path: c.from, Imports: []string{c.imports}}})
		switch {
		case c.want == "" && len(got) > 0:
			t.Errorf("%s importing %s should be allowed: %v", c.from, c.imports, got)
		case c.want != "" && (len(got) == 0 || !strings.Contains(strings.Join(got, "\n"), c.want)):
			t.Errorf("%s importing %s should be refused (%s): %v", c.from, c.imports, c.want, got)
		}
	}
}

// violations lists every import the rules refuse.
func violations(r rules, pkgs []pkg) []string {
	var out []string
	for _, p := range pkgs {
		from, ok := r.layer(p.Path)
		if !ok {
			out = append(out, fmt.Sprintf("%s has no layer in layers.toml", p.Path))
			continue
		}
		for _, imp := range p.Imports {
			if why := r.forbidden(p.Path, imp); why != "" {
				out = append(out, fmt.Sprintf("%s imports %s: %s", p.Path, imp, why))
				continue
			}
			to, ok := r.layer(imp)
			if !ok {
				out = append(out, fmt.Sprintf("%s imports %s, which has no layer in layers.toml", p.Path, imp))
				continue
			}
			if to > from {
				out = append(out, fmt.Sprintf("%s (L%d) imports %s (L%d): an import never points up", p.Path, from, imp, to))
			}
			mf, mt := r.member(p.Path), r.member(imp)
			if mf != "" && mt != "" && mf != mt && r.group(mf) == r.group(mt) {
				out = append(out, fmt.Sprintf("%s imports %s: the members of %s are independent", p.Path, imp, r.group(mf)))
			}
			if match(r.Adapters, imp) && (mt == "" || mf != mt) && !slices.ContainsFunc(r.AdapterImporters, func(pat string) bool { return match(pat, p.Path) }) {
				out = append(out, fmt.Sprintf("%s imports the adapter %s: only a deployable chooses an adapter", p.Path, imp))
			}
		}
	}
	return out
}

// forbidden is why from may not import imp at all; "" when it may.
func (r rules) forbidden(from, imp string) string {
	for _, f := range r.Forbid {
		if match(f.From, from) && slices.ContainsFunc(f.Imports, func(pat string) bool { return match(pat, imp) }) {
			return f.Why
		}
	}
	return ""
}

func (r rules) layer(path string) (int, bool) {
	for name, pats := range r.Layers {
		for _, pat := range pats {
			if match(pat, path) {
				n, err := strconv.Atoi(name)
				return n, err == nil
			}
		}
	}
	return 0, false
}

// member is the member of an independent group a path belongs to, such as
// gateway/adapter/snowflake for gateway/adapter/snowflake/snowfake; "" when
// it belongs to none.
func (r rules) member(path string) string {
	for _, g := range r.Independent {
		base := strings.TrimSuffix(g, "/*")
		if rest, ok := strings.CutPrefix(path, base+"/"); ok {
			name, _, _ := strings.Cut(rest, "/")
			return base + "/" + name
		}
	}
	return ""
}

func (r rules) group(member string) string {
	for _, g := range r.Independent {
		if strings.HasPrefix(member, strings.TrimSuffix(g, "*")) {
			return g
		}
	}
	return ""
}

// match reports whether path is pat, or under it when pat ends in /....
func match(pat, path string) bool {
	if base, ok := strings.CutSuffix(pat, "/..."); ok {
		return path == base || strings.HasPrefix(path, base+"/")
	}
	return path == pat
}

// list runs go list in dir and returns its packages that have production
// code, with their imports from this repository.
func list(dir string) ([]pkg, error) {
	cmd := exec.Command("go", "list", "-json=ImportPath,GoFiles,Imports", "./...")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, fmt.Errorf("go list: %v: %s", err, ee.Stderr)
		}
		return nil, err
	}
	var pkgs []pkg
	dec := json.NewDecoder(strings.NewReader(string(out)))
	for {
		var p struct {
			ImportPath string
			GoFiles    []string
			Imports    []string
		}
		if err := dec.Decode(&p); errors.Is(err, io.EOF) {
			return pkgs, nil
		} else if err != nil {
			return nil, err
		}
		if len(p.GoFiles) == 0 {
			continue // tests only
		}
		q := pkg{Path: strings.TrimPrefix(p.ImportPath, repo)}
		for _, imp := range p.Imports {
			if rel, ok := strings.CutPrefix(imp, repo); ok {
				q.Imports = append(q.Imports, rel)
			}
		}
		pkgs = append(pkgs, q)
	}
}
