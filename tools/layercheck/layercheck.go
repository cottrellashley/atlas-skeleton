// Package layercheck is the layer check (§6.5.9, ATLAS-PACKAGES-001). It reads
// every package's imports with go list, and refuses an import that does any of
// these, as a module's layers.toml declares:
//
//   - points up a layer
//   - crosses between the members of an independent group
//   - reaches an adapter from a package that is not allowed to choose one
//   - is forbidden outright
//   - brings in a third-party module the package's rules do not allow
//
// Only production code is checked; a test may assemble anything.
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

	"github.com/BurntSushi/toml"
)

// Repo is the module path prefix every rule's paths are relative to.
const Repo = "github.com/jhgonline/fi-app-atlas/"

// Rules is one layers.toml.
type Rules struct {
	Layers           map[string][]string `toml:"layers"`
	Independent      []string            `toml:"independent"`
	Adapters         string              `toml:"adapters"`
	AdapterImporters []string            `toml:"adapter_importers"`
	Forbid           []Forbid            `toml:"forbid"`
	External         []External          `toml:"external"`
}

// Forbid refuses the imports matching Imports from the packages matching From.
type Forbid struct {
	From    string   `toml:"from"`
	Imports []string `toml:"imports"`
	Why     string   `toml:"why"`
}

// External limits the third-party modules the packages matching From may
// import to those matching Allow (the standard library is always allowed).
type External struct {
	From  string   `toml:"from"`
	Allow []string `toml:"allow"`
	Why   string   `toml:"why"`
}

// Package is a package with production code. Its path, and its imports from
// this repository, are relative to Repo; other imports are full paths.
type Package struct {
	Path    string
	Imports []string
}

// Load reads a layers.toml.
func Load(path string) (Rules, error) {
	var r Rules
	md, err := toml.DecodeFile(path, &r)
	if err != nil {
		return r, err
	}
	if undecoded := md.Undecoded(); len(undecoded) > 0 {
		return r, fmt.Errorf("%s: unknown keys %v", path, undecoded)
	}
	return r, nil
}

// Check lists every import the rules refuse.
func Check(r Rules, pkgs []Package) []string {
	var out []string
	for _, p := range pkgs {
		from, ok := r.layer(p.Path)
		if !ok {
			out = append(out, fmt.Sprintf("%s has no layer", p.Path))
			continue
		}
		for _, imp := range p.Imports {
			if stdlib(imp) {
				continue
			}
			if !ours(imp) {
				if why := r.foreign(p.Path, imp); why != "" {
					out = append(out, fmt.Sprintf("%s imports %s: %s", p.Path, imp, why))
				}
				continue
			}
			if why := r.forbidden(p.Path, imp); why != "" {
				out = append(out, fmt.Sprintf("%s imports %s: %s", p.Path, imp, why))
				continue
			}
			to, ok := r.layer(imp)
			if !ok {
				out = append(out, fmt.Sprintf("%s imports %s, which has no layer", p.Path, imp))
				continue
			}
			if to > from {
				out = append(out, fmt.Sprintf("%s (L%d) imports %s (L%d): an import never points up", p.Path, from, imp, to))
			}
			mf, mt := r.member(p.Path), r.member(imp)
			if mf != "" && mt != "" && mf != mt && r.group(mf) == r.group(mt) {
				out = append(out, fmt.Sprintf("%s imports %s: the members of %s are independent", p.Path, imp, r.group(mf)))
			}
			if r.Adapters != "" && match(r.Adapters, imp) && (mt == "" || mf != mt) &&
				!slices.ContainsFunc(r.AdapterImporters, func(pat string) bool { return match(pat, p.Path) }) {
				out = append(out, fmt.Sprintf("%s imports the adapter %s: only a deployable chooses an adapter", p.Path, imp))
			}
		}
	}
	return out
}

// List runs go list in dir and returns its packages that have production code.
func List(dir string) ([]Package, error) {
	cmd := exec.Command("go", "list", "-json=ImportPath,GoFiles,Imports", "./...")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return nil, fmt.Errorf("go list in %s: %v: %s", dir, err, ee.Stderr)
		}
		return nil, err
	}
	var pkgs []Package
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
		q := Package{Path: strings.TrimPrefix(p.ImportPath, Repo)}
		for _, imp := range p.Imports {
			q.Imports = append(q.Imports, strings.TrimPrefix(imp, Repo))
		}
		pkgs = append(pkgs, q)
	}
}

// forbidden is why from may not import imp at all; "" when it may.
func (r Rules) forbidden(from, imp string) string {
	for _, f := range r.Forbid {
		if match(f.From, from) && slices.ContainsFunc(f.Imports, func(pat string) bool { return match(pat, imp) }) {
			return f.Why
		}
	}
	return ""
}

// foreign is why from may not import the third-party package imp; "" when it may.
func (r Rules) foreign(from, imp string) string {
	for _, e := range r.External {
		if match(e.From, from) && !slices.ContainsFunc(e.Allow, func(pat string) bool { return match(pat, imp) }) {
			return e.Why
		}
	}
	return ""
}

func (r Rules) layer(path string) (int, bool) {
	best, n := -1, 0
	for name, pats := range r.Layers {
		for _, pat := range pats {
			// The most specific pattern wins, so "sdk/go" and "sdk/go/kit/..." can sit in different layers.
			if match(pat, path) && len(pat) > best {
				v, err := strconv.Atoi(name)
				if err != nil {
					return 0, false
				}
				best, n = len(pat), v
			}
		}
	}
	return n, best >= 0
}

// member is the member of an independent group a path belongs to, such as
// gateway/adapter/snowflake for gateway/adapter/snowflake/duckwh; "" when it
// belongs to none.
func (r Rules) member(path string) string {
	for _, g := range r.Independent {
		base := strings.TrimSuffix(g, "/*")
		if rest, ok := strings.CutPrefix(path, base+"/"); ok {
			name, _, _ := strings.Cut(rest, "/")
			return base + "/" + name
		}
	}
	return ""
}

func (r Rules) group(member string) string {
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

// ours reports whether a (Repo-trimmed) import path is this repository's: its
// first element has no dot, and it is not the standard library.
func ours(imp string) bool {
	first, _, _ := strings.Cut(imp, "/")
	return !strings.Contains(first, ".") && (first == "sdk" || first == "gateway" || first == "clients" || first == "tools")
}

// stdlib reports whether imp is the standard library: no dot in its first element.
func stdlib(imp string) bool {
	first, _, _ := strings.Cut(imp, "/")
	return !strings.Contains(first, ".") && !ours(imp)
}
