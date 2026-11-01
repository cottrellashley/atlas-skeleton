package builtins

import (
	"encoding/json"
	"maps"
	"reflect"
	"testing"

	"atlas.local/gateway/core/registry"
	"atlas.local/gateway/internal/refs"
	"atlas.local/sdk/go/protocol/ijson"
)

// AP-DOC-002: the built-ins' profiles are atlas/1.0's: the exact ids the
// references' conformance gateway serves, so a client's lock and cache
// agree with every conforming gateway.
func TestTheBuiltinsAreTheProtocols(t *testing.T) {
	var fixture struct {
		Profiles map[string]json.RawMessage `json:"profiles"`
	}
	if err := json.Unmarshal(refs.Read(t, "scenarios/fixtures/gateway.json"), &fixture); err != nil {
		t.Fatal(err)
	}
	ps, err := Profiles()
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 10 {
		t.Fatalf("%d built-ins", len(ps))
	}
	for _, p := range ps {
		if _, ok := fixture.Profiles[p.Exact().String()]; ok {
			continue
		}
		ref, ok := amended[p.ID]
		if !ok {
			t.Errorf("%s is not the references' %s", p.Exact(), p.ID)
			continue
		}
		var got, fx map[string]any
		b, err := ijson.Marshal(p.Doc)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &got); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(fixture.Profiles[ref.exact], &fx); err != nil {
			t.Fatal(err)
		}
		var defs struct {
			Defs map[string]any `json:"$defs"`
		}
		if err := json.Unmarshal(refs.Read(t, "schemas/builtins.schema.json"), &defs); err != nil {
			t.Fatal(err)
		}
		// The schema, its amendment's member and its description out, is
		// the references' definition written out.
		gs := got["schemas"].(map[string]any)[ref.schema].(map[string]any)
		inline := maps.Clone(gs)
		inline["properties"] = maps.Clone(gs["properties"].(map[string]any))
		delete(inline["properties"].(map[string]any), ref.member)
		def := maps.Clone(defs.Defs[ref.schema].(map[string]any))
		for _, k := range []string{"$schema", "$vocabulary"} {
			def[k] = inline[k]
		}
		delete(inline, "description")
		delete(def, "description")
		if !reflect.DeepEqual(inline, def) {
			t.Errorf("%s's %s, its amendment's %s out, is not the references' %s", p.Exact(), ref.schema, ref.member, ref.schema)
		}
		// With the references' schema back, the profile is the references'
		// own: the same fingerprint.
		got["schemas"].(map[string]any)[ref.schema] = fx["schemas"].(map[string]any)[ref.schema]
		back, err := json.Marshal(got)
		if err != nil {
			t.Fatal(err)
		}
		q, err := registry.Load(back)
		if err != nil {
			t.Fatal(err)
		}
		if q.Exact().String() != ref.exact {
			t.Errorf("%s, its amendment undone, is %s, not the references' %s", p.Exact(), q.Exact(), ref.exact)
		}
	}
}

// amended are the built-ins whose profile carries a proposed amendment to
// the protocol (ASSUMPTIONS.md): the references' profile, and the one
// member of one schema the amendment adds. Each holds until the references
// take the amendment or refuse it.
var amended = map[string]struct{ exact, schema, member string }{
	// A-200: whoami says the caller's scope in every access dimension.
	"atlas.whoami": {"atlas.whoami@v1#76e4cb9bb2dc565d", "whoami_result", "scope"},
}
