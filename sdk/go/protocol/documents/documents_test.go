package documents

import (
	"bytes"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jhgonline/fi-app-atlas/sdk/go/internal/refs"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
)

// The embedded documents are the references' own, byte for byte.
func TestTheDocumentsAreTheReferences(t *testing.T) {
	names, err := fs.Glob(Files(), "*.schema.json")
	if err != nil || len(names) != 21 {
		t.Fatalf("%d documents, %v", len(names), err)
	}
	for _, n := range names {
		got, _ := fs.ReadFile(Files(), n)
		if want := refs.Read(t, filepath.Join("schemas", n)); !bytes.Equal(got, want) {
			t.Errorf("%s has drifted from spec/protocol/references/schemas", n)
		}
	}
}

func parse(t *testing.T, s string) ijson.Value {
	t.Helper()
	v, err := ijson.Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// AP-DOC-001: the fixtures' bootstrap and the data profile vector are
// valid; what their schemas refuse is refused, through every keyword the
// documents use.
func TestValidate(t *testing.T) {
	fixture := parse(t, string(refs.Read(t, "scenarios/fixtures/gateway.json"))).(*ijson.Object)
	bootV, _ := fixture.Get("bootstrap")
	boot, _ := ijson.Marshal(bootV)
	bootstrap := strings.ReplaceAll(string(boot), "{gateway}", "https://gateway.example")
	if fs := Validate("bootstrap", parse(t, bootstrap)); len(fs) > 0 {
		t.Fatalf("the fixtures' bootstrap: %v", fs)
	}
	profiles := parse(t, string(refs.Read(t, "vectors/profiles/data.json"))).(*ijson.Object)
	cases, _ := profiles.Get("cases")
	p, _ := cases.([]ijson.Value)[0].(*ijson.Object).Get("profile")
	if fs := Validate("profile", p); len(fs) > 0 {
		t.Fatalf("the data profile: %v", fs)
	}
	for name, c := range map[string]struct{ doc, value, keyword string }{
		"https without a method (if/then)":       {"binding", `{"protocol": "https", "server": "gateway", "address": "/x", "auth": "bearer"}`, "required"},
		"a server name the grammar refuses":      {"bootstrap", strings.Replace(bootstrap, `"servers":{"gateway"`, `"servers":{"Gateway"`, 1), "propertyNames"},
		"a succeeded operation with no result":   {"operation", `{"name": "operations/a1", "address": "x.y@v1:fetch", "state": "succeeded", "created": "2026-10-31T00:00:00Z", "updated": "2026-10-31T00:00:00Z"}`, "anyOf"},
		"an end line without its problem member": {"chunk-end", `{"$atlas": "end", "rows": 0, "truncated": false, "usage": {}}`, "required"},
		"a problem missing its request id":       {"problem", `{"type": "https://atlas.jhg.com/problems/internal", "status": 500, "code": "internal", "retryable": false, "next": "Retry."}`, "required"},
	} {
		fs := Validate(c.doc, parse(t, c.value))
		if len(fs) == 0 || !strings.Contains(fs[0].Keyword, c.keyword) {
			t.Errorf("%s: %v", name, fs)
		}
	}
	if fs := Validate("builtins#search_request", parse(t, `{"words": "holdings", "page_size": 501}`)); len(fs) != 1 || fs[0].Pointer != "/page_size" {
		t.Errorf("a definition of a document: %v", fs)
	}
}
