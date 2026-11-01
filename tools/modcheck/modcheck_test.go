package modcheck

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
)

const root = "../.."

// Every module this repository holds resolves locally, so nothing ever asks
// a remote source for a project module.
func TestProjectModulesResolveLocally(t *testing.T) {
	mods, err := Modules(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(mods) < 2 {
		t.Fatalf("found %d modules", len(mods))
	}
	work, err := Work(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range Check(root, mods, work) {
		t.Error(v)
	}
}

// The Makefile fences the local namespace off, so that a replace that is
// missing fails on this machine.
func TestTheMakefileFencesTheLocalNamespace(t *testing.T) {
	b, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"export GOPRIVATE := atlas.local", "export GOVCS := atlas.local:off"} {
		if !strings.Contains(string(b), line+"\n") {
			t.Errorf("the Makefile does not say %q", line)
		}
	}
}

// Nothing tells a reader, a caller or an agent to fetch a local module path:
// no document, and no string in code or data either (atlas.clients once
// answered every caller with a go install from it).
// fetch matches an instruction that would treat the local namespace as remote.
var fetch = regexp.MustCompile(`go (get|install|run)\s+[^\s"'\x60]*atlas\.local|git (clone|pull|fetch|push|remote add)[^\n]*atlas\.local|atlas\.local[:/][^\s"'\x60]*\.git`)

// The pattern catches what atlas.clients once told every caller, and lets
// through building from a checkout and plain import paths.
func TestTheFetchPatternItself(t *testing.T) {
	for _, bad := range []string{
		"go install atlas.local/clients/cli/cmd/atlas@latest",
		"go get atlas.local/sdk/go",
		"go run atlas.local/tools/reqs@v0.1.0",
		"git clone https://atlas.local/atlas.git",
		"git remote add origin git@atlas.local:atlas.git",
	} {
		if !fetch.MatchString(bad) {
			t.Errorf("not caught: %q", bad)
		}
	}
	for _, good := range []string{
		"From a checkout of the Atlas repository: go -C clients/cli install ./cmd/atlas",
		`import "atlas.local/sdk/go"`,
		"module atlas.local/gateway",
		"replace atlas.local/sdk/go => ../sdk/go",
	} {
		if fetch.MatchString(good) {
			t.Errorf("refused: %q", good)
		}
	}
}

func TestNothingTellsAnyoneToFetchLocalModules(t *testing.T) {
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (skipped[d.Name()] || d.Name() == "spec") {
			return filepath.SkipDir
		}
		scanned := d.Name() == "Makefile" || slices.ContainsFunc([]string{".md", ".sh", ".go", ".py", ".ts", ".js", ".json", ".toml"}, func(ext string) bool { return strings.HasSuffix(path, ext) })
		if d.IsDir() || !scanned || strings.HasSuffix(path, "modcheck_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if m := fetch.Find(b); m != nil {
			t.Errorf("%s tells someone to fetch a local module: %q", path, m)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// The check itself refuses what it is there to refuse.
func TestTheCheckRefuses(t *testing.T) {
	parse := func(dir, text string) Module {
		f, err := modfile.Parse(dir+"/go.mod", []byte(text), nil)
		if err != nil {
			t.Fatal(err)
		}
		return Module{Dir: dir, Path: f.Module.Mod.Path, File: f}
	}
	a := parse("a", "module atlas.local/a\n")
	work, err := modfile.ParseWork("go.work", []byte("go 1.25.0\n\nuse (\n\t./a\n\t./b\n)\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, b := range map[string]string{
		"no replace":        "module atlas.local/b\nrequire atlas.local/a v0.0.0\n",
		"a module version":  "module atlas.local/b\nrequire atlas.local/a v0.0.0\nreplace atlas.local/a => atlas.local/a v1.0.0\n",
		"the wrong place":   "module atlas.local/b\nrequire atlas.local/a v0.0.0\nreplace atlas.local/a => ../c\n",
		"an unknown module": "module atlas.local/b\nrequire atlas.local/zzz v0.0.0\nreplace atlas.local/zzz => ../zzz\n",
		// A company's module outside the local namespace (HANDOFF.md
		// C1) is held to the same rules: it reaches an in-repo module only
		// through a replace to its directory.
		"outside, requiring with no replace": "module example.com/b\nrequire atlas.local/a v0.0.0\n",
	} {
		if got := Check(".", []Module{a, parse("b", b)}, work); len(got) == 0 {
			t.Errorf("%s: accepted", name)
		}
	}
	good := parse("b", "module atlas.local/b\nrequire atlas.local/a v0.0.0\nreplace atlas.local/a => ../a\n")
	if got := Check(".", []Module{a, good}, work); len(got) != 0 {
		t.Errorf("a module replaced by its directory: %v", got)
	}
	company := parse("b", "module example.com/b\nrequire atlas.local/a v0.0.0\nreplace atlas.local/a => ../a\n")
	if got := Check(".", []Module{a, company}, work); len(got) != 0 {
		t.Errorf("a company module that reaches in-repo modules by their directories: %v", got)
	}
	// Any in-repo module, under another namespace too, is required only
	// through a replace, so the go command asks no one for it.
	needsCompany := parse("a", "module atlas.local/a\nrequire example.com/b v0.0.0\n")
	if got := Check(".", []Module{needsCompany, parse("b", "module example.com/b\n")}, work); len(got) == 0 {
		t.Error("an in-repo company module required with no replace: accepted")
	}
	lacking, _ := modfile.ParseWork("go.work", []byte("go 1.25.0\n\nuse ./a\n"), nil)
	if got := Check(".", []Module{a, good}, lacking); len(got) != 1 || !strings.Contains(got[0], "leaves out b") {
		t.Errorf("a module go.work leaves out: %v", got)
	}
}
