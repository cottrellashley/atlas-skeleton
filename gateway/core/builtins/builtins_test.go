package builtins

import (
	"encoding/json"
	"testing"

	"github.com/jhgonline/fi-app-atlas/gateway/internal/refs"
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
		if _, ok := fixture.Profiles[p.Exact().String()]; !ok {
			t.Errorf("%s is not the references' %s", p.Exact(), p.ID)
		}
	}
}
