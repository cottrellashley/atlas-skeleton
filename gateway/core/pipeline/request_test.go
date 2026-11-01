package pipeline

import (
	"context"
	"testing"

	"atlas.local/gateway/core/registry"
	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/protocol/ijson"
	"atlas.local/sdk/go/protocol/profile"
)

// said answers demo.echo with what it was told.
func said(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
	rec := ijson.NewObject()
	text, _ := inv.Request.Get("text")
	rec.Set("text", text)
	return &ports.Answer{Record: rec}, nil
}

// PR07, R04 (a request is validated under its own contract), ATLAS-RESOLVE-003:
// a request written under an earlier profile is read through that
// profile's own interaction, whatever the later one calls its schema. A
// rename the gate admits never breaks the requests written before it.
func TestPR07ARenamedSchemaKeepsEarlierRequests(t *testing.T) {
	r := newRig(t, serve(said), Options{})
	v1 := r.reg.Snapshot().Lookup("demo.echo@v1").Current
	v2 := loadDoc(t, echo("request2", "", v1.Exact().String()))
	if err := r.reg.Register(registry.Entry{Profiles: []*profile.Profile{v1, v2}, Provider: serve(said)}); err != nil {
		t.Fatalf("the gate admits a rename: %v", err)
	}
	for _, expect := range []string{v1.Exact().String(), v2.Exact().String(), ""} {
		c := r.call("t-alice", echoAt, `{"text": "hi"}`)
		c.Expect = expect
		res, err := r.run(c)
		if err != nil {
			t.Fatalf("written under %q: %v", expect, err)
		}
		if text, _ := res.Record.Get("text"); text != "hi" {
			t.Fatalf("written under %q: %v", expect, text)
		}
		res.Close()
	}
}
