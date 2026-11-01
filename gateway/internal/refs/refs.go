// Package refs finds the protocol's references (spec/protocol/references)
// for the gateway's tests, which prove the gateway against them as the SDK's
// tests prove the SDK.
package refs

import (
	"os"
	"path/filepath"
	"testing"
)

// Dir is spec/protocol/references, found from the working directory up.
func Dir(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		p := filepath.Join(dir, "spec", "protocol", "references")
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no spec/protocol/references above the working directory")
		}
		dir = parent
	}
}

// Read is one file of the references.
func Read(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(Dir(t), filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return b
}
