package gate

import (
	"os"
	"path/filepath"
	"testing"

	"atlas.local/sdk/go/internal/refs"
	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/ijson"
	"atlas.local/sdk/go/protocol/profile"
)

// The complete profile of each family (§3.1.3) reads, carries its own
// fingerprint and exact id, passes the lint and is ready for agents
// (§3.1.4a).
func TestVectorProfiles(t *testing.T) {
	paths, _ := filepath.Glob(filepath.Join(refs.Dir(t), "vectors", "profiles", "*.json"))
	if len(paths) < 4 {
		t.Fatalf("%d vector profiles", len(paths))
	}
	families := map[string]bool{}
	for _, path := range paths {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		v, err := ijson.Parse(b)
		if err != nil {
			t.Fatal(err)
		}
		cases, _ := v.(*ijson.Object).Get("cases")
		for _, c := range cases.([]ijson.Value) {
			c := c.(*ijson.Object)
			name, _ := c.Get("name")
			doc, _ := c.Get("profile")
			e, _ := c.Get("expected")
			expected := e.(*ijson.Object)
			p, err := profile.Parse(doc.(*ijson.Object))
			if err != nil {
				t.Errorf("%s: %v", name, err)
				continue
			}
			families[p.Family] = true
			if fp, _ := expected.Get("fingerprint"); p.FP.Value != fp {
				t.Errorf("%s: fingerprint %s, the vector's %v", name, p.FP.Value, fp)
			}
			if exact, _ := expected.Get("exact"); p.Exact().String() != exact {
				t.Errorf("%s: exact %s, the vector's %v", name, p.Exact(), exact)
			}
			if _, err := protocol.CheckFingerprint(p.Doc); err != nil {
				t.Errorf("%s: %v", name, err)
			}
			if fs := Lint(p, nil); len(fs) > 0 {
				t.Errorf("%s: the lint refuses it: %v", name, fs)
			}
			if rs := Ready(p); len(rs) > 0 {
				t.Errorf("%s: not ready for agents: %v", name, rs)
			}
		}
	}
	for _, f := range []string{"data", "resource", "action", "event"} {
		if !families[f] {
			t.Errorf("no %s profile", f)
		}
	}
}
