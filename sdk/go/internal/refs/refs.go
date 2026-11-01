// Package refs finds the protocol's references, spec/protocol/references,
// for tests: the schemas and vectors every implementation is checked
// against (§9).
package refs

import (
	"os"
	"path/filepath"
	"testing"
)

// Dir returns spec/protocol/references, found by walking up from the
// working directory. A test that needs the references fails without them;
// it never passes by skipping.
func Dir(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		refs := filepath.Join(dir, "spec", "protocol", "references")
		if fi, err := os.Stat(refs); err == nil && fi.IsDir() {
			return refs
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("spec/protocol/references is not above " + dir)
		}
		dir = parent
	}
}

// Read returns one file of the references, by its path inside them.
func Read(t testing.TB, rel string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(Dir(t), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return b
}
