package policyfile

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"atlas.local/gateway/fixtures"
	"atlas.local/gateway/ports"
	"atlas.local/gateway/porttest"
)

func write(t *testing.T, path string, b []byte) {
	t.Helper()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

func TestSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	write(t, path, fixtures.PolicyJSON())
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	p, err := s.Policy(ctx)
	if err != nil || p.Revision != "dev-2026-10-31.1" || len(p.Grants) != 2 {
		t.Fatalf("%+v %v", p, err)
	}
	write(t, path, []byte(`{"revision": "r2", "roles": {}, "grants": [{"id": "g", "role": "r", "capabilities": ["portfolio.list@v1"]}]}`))
	if p, err := s.Policy(ctx); err != nil || p.Revision != "r2" {
		t.Fatalf("a changed file is read again: %v %v", p, err)
	}
	if _, err := New(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Fatal("a gateway with no policy started")
	}
}

// PR02, R014, R039, R13: a file the source cannot read or parse is an
// error, every time it is asked, so the gateway's bound on a stale policy
// applies (ports.PolicySource). Answering the last good policy as current
// would let a revocation that never took hold go unnoticed for ever.
func TestPR02AFileThatCannotBeReadIsAnError(t *testing.T) {
	for _, c := range []struct {
		name   string
		damage func(t *testing.T, path string)
		why    string
	}{
		{"deleted", func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}, "no such file"},
		{"malformed", func(t *testing.T, path string) {
			write(t, path, []byte(`{"revision": "r-revoke", "grants": [],}`))
		}, "invalid character"},
		{"an unknown member", func(t *testing.T, path string) {
			write(t, path, []byte(`{"revision": "r-revoke", "grantz": []}`))
		}, "grantz"},
		{"no revision", func(t *testing.T, path string) {
			write(t, path, []byte(`{"grants": []}`))
		}, "revision"},
		{"larger than a policy may be", func(t *testing.T, path string) {
			write(t, path, []byte(`{"revision": "big", "house_rules_revision": "`+strings.Repeat("x", MaxSize)+`"}`))
		}, "larger than"},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "policy.json")
			write(t, path, fixtures.PolicyJSON())
			s, err := New(path)
			if err != nil {
				t.Fatal(err)
			}
			c.damage(t, path)
			for range 3 {
				p, err := s.Policy(context.Background())
				if err == nil || p != nil {
					t.Fatalf("a %s file answered %v, %v: the last good policy as if it were current", c.name, p, err)
				}
				if !strings.Contains(err.Error(), c.why) || !strings.Contains(err.Error(), "policy") {
					t.Fatalf("the error says what is wrong: %v", err)
				}
			}
			write(t, path, []byte(`{"revision": "r-fixed"}`))
			if p, err := s.Policy(context.Background()); err != nil || p.Revision != "r-fixed" {
				t.Fatalf("a mended file is read again: %v %v", p, err)
			}
		})
	}
}

// PR02, R039: an unreadable file (its permissions, a failing disk) is an
// error too.
func TestPR02AnUnreadableFileIsAnError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a file whatever its mode")
	}
	path := filepath.Join(t.TempDir(), "policy.json")
	write(t, path, fixtures.PolicyJSON())
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	if p, err := s.Policy(context.Background()); err == nil || p != nil || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("an unreadable file answered %v, %v", p, err)
	}
}

// R039: a change is found by the file's content, never by its time or
// size alone, so an edit within one tick of the file system's clock that
// keeps the size is still read.
func TestR039AChangeIsFoundByItsContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.json")
	write(t, path, []byte(`{"revision": "r1"}`))
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(path)
	write(t, path, []byte(`{"revision": "r2"}`))
	if err := os.Chtimes(path, st.ModTime(), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	if p, err := s.Policy(context.Background()); err != nil || p.Revision != "r2" {
		t.Fatalf("the same size and time, other content: %v %v", p, err)
	}
}

// fileHarness is the suite's hold on a policy file: what an operator, a
// deploy or a failing disk does to it.
type fileHarness struct {
	path   string
	damage func(t *testing.T, path string)
}

func (h fileHarness) Publish(t *testing.T, p *ports.Policy) {
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	write(t, h.path, b)
}

func (h fileHarness) Break(t *testing.T) { h.damage(t, h.path) }

// R057, R073, AGENTS.md (every implementation runs its port's suite): the
// file adapter keeps the PolicySource contract however its file breaks.
func TestPolicySourceSuite(t *testing.T) {
	for name, breaks := range map[string]func(t *testing.T, path string){
		"deleted":   func(t *testing.T, path string) { _ = os.Remove(path) },
		"malformed": func(t *testing.T, path string) { write(t, path, []byte(`{"revision": `)) },
		"refused":   func(t *testing.T, path string) { write(t, path, []byte(`{"revision": "r9", "grantz": []}`)) },
	} {
		t.Run(name, func(t *testing.T) {
			porttest.PolicySource(t, func(t *testing.T, initial *ports.Policy) (ports.PolicySource, porttest.PolicyHarness) {
				h := fileHarness{path: filepath.Join(t.TempDir(), "policy.json"), damage: breaks}
				h.Publish(t, initial)
				s, err := New(h.path)
				if err != nil {
					t.Fatal(err)
				}
				return s, h
			})
		})
	}
}

// R134, A-181: the file is read by the one strict reader every policy is
// read by, so the fixture and the file can never be read differently.
func TestParseIsTheStrictReader(t *testing.T) {
	for _, b := range []string{`{"revision": "r1", "grantz": []}`, `{"grants": []}`, `{"revision": "r1"} {}`} {
		_, want := ports.ParsePolicy([]byte(b))
		_, got := Parse([]byte(b))
		if want == nil || got == nil || !strings.Contains(got.Error(), strings.TrimPrefix(want.Error(), "ports: ")) {
			t.Fatalf("%s: Parse %v, ports.ParsePolicy %v", b, got, want)
		}
	}
}

func FuzzParse(f *testing.F) {
	f.Add(fixtures.PolicyJSON())
	f.Add([]byte(`{"revision": "r1", "grants": [{"id": "g", "role": "r", "capabilities": ["portfolio.*"]}]}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		p, err := Parse(b)
		if (p == nil) == (err == nil) {
			t.Fatalf("a policy or an error, never both or neither: %v %v", p, err)
		}
	})
}
