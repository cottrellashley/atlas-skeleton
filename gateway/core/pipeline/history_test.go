package pipeline_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jhgonline/fi-app-atlas/gateway/core/pipeline"
	"github.com/jhgonline/fi-app-atlas/gateway/core/registry"
	"github.com/jhgonline/fi-app-atlas/gateway/fakes"
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/clock"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/profile"
)

const historyVoc = `"$schema": "https://json-schema.org/draft/2020-12/schema", "$vocabulary": {"https://json-schema.org/draft/2020-12/vocab/core": true, "https://json-schema.org/draft/2020-12/vocab/applicator": true, "https://json-schema.org/draft/2020-12/vocab/validation": true, "https://json-schema.org/draft/2020-12/vocab/meta-data": true, "https://json-schema.org/draft/2020-12/vocab/format-assertion": true, "https://atlas.jhg.com/vocab/2026-10": false}`

// historyEcho is a profile of demo.echo@v1 whose fetch takes the schema
// named takes, with more request members, after the profile previous names
// (none for the first), with changes changelog entries.
func historyEcho(t *testing.T, takes, more, previous string, changes int) *profile.Profile {
	t.Helper()
	changelog := `{"date": "2026-10-01", "change": "First."}`
	for i := 2; i <= changes; i++ {
		changelog += `, {"date": "2026-10-0` + string(rune('0'+i)) + `", "change": "More."}`
	}
	prev := ""
	if previous != "" {
		prev = `"previous_profile": "` + previous + `", `
	}
	doc := `{
	 "identity": {"id": "demo.echo", "version": 1, "title": "Echo", "summary": "Says what it is told.", "domain": "demo", "owner": "QFI"},
	 "properties": {"family": "data", "stability": "official", "lifecycle": "stable", "visibility": "listed", "traits": []},
	 "interactions": {"fetch": {"effect": "read", "result": "record", "delivery": "single", "execution": ["gateway"], "takes": "` + takes + `", "returns": "result",
	  "deadline": {"default": "PT30S", "max": "PT2M"},
	  "bindings": [{"protocol": "https", "server": "gateway", "method": "POST", "address": "/v1/demo/demo.echo@v1:fetch", "encodings": ["json"], "auth": "bearer"}]}},
	 "schemas": {
	  "` + takes + `": {` + historyVoc + `, "type": "object", "additionalProperties": false, "required": ["text"], "properties": {"text": {"type": "string", "description": "What to say."}` + more + `}},
	  "result": {` + historyVoc + `, "type": "object", "required": ["text"], "properties": {"text": {"type": "string", "description": "What was said."}, "loud": {"type": ["boolean", "null"], "description": "Whether it was shouted."}}}},
	 "errors": [],
	 "meaning": {"grain": "one answer", "source": "the request"},
	 "usage": {"when": "You need an echo.", "examples": [{"title": "Hello", "interaction": "fetch", "request": {"text": "hello"}}]},
	 "evolution": {` + prev + `"lens": [], "changelog": [` + changelog + `]}
	}`
	p, err := registry.Load([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// echoed answers what it was told, and whether loud was asked for.
type echoed struct{}

func (echoed) Serve(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
	rec := ijson.NewObject()
	text, _ := inv.Request.Get("text")
	loud, _ := inv.Request.Get("loud")
	rec.Set("text", text)
	rec.Set("loud", loud)
	return &ports.Answer{Record: rec}, nil
}

// historyPipeline serves reg to one caller who holds demo.echo@v1 by name.
func historyPipeline(t *testing.T, reg *registry.Registry) *pipeline.Pipeline {
	t.Helper()
	tokens := fakes.NewTokens(clock.System)
	tokens.Issue("t-alice", ports.Identity{Principal: "alice", Kind: "person", Groups: []string{"analysts"}}, time.Time{})
	policy := fakes.NewPolicy(&ports.Policy{
		Revision: "r1",
		Roles:    map[string][]string{"analysts": {"analyst"}},
		Grants:   []ports.Grant{{ID: "grant-echo", Role: "analyst", Capabilities: []string{"demo.echo@v1"}}},
	})
	p, err := pipeline.New(pipeline.Options{Environment: "test", Registry: reg, Authenticator: tokens, Policy: policy, Audit: &fakes.Audit{}})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func echoCall(t *testing.T, p *pipeline.Pipeline, expect, body string) (*ijson.Object, error) {
	t.Helper()
	v, err := ijson.Parse([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.Run(context.Background(), &pipeline.Call{
		RequestID: "req-history", Path: "/v1/demo/demo.echo@v1:fetch", Token: "t-alice", Expect: expect, Body: v,
		Client: &protocol.Client{Name: "history", Version: "1.0.0", Protocol: "atlas/1.0"}, Features: protocol.CoreFeatures(),
	})
	if err != nil {
		return nil, err
	}
	defer res.Close()
	return res.Record, nil
}

// R045, PR05, ATLAS-RESOLVE-003, §9.5.6: a request written under any
// earlier profile of the version is accepted and means what it meant then,
// after any number of compatible replacements, at start as after a hot
// replace.
func TestR045ARequestUnderAnEarlierProfileResolvesForward(t *testing.T) {
	v1 := historyEcho(t, "request", "", "", 1)
	v2 := historyEcho(t, "request", `, "loud": {"type": ["boolean", "null"], "default": null, "description": "Shout."}`, v1.Exact().String(), 2)
	v3 := historyEcho(t, "request", `, "loud": {"type": ["boolean", "null"], "default": null, "description": "Shout."}, "quiet": {"type": ["boolean", "null"], "default": null, "description": "Whisper."}`, v2.Exact().String(), 3)
	started := registry.New()
	if err := started.Register(registry.Entry{Profiles: []*profile.Profile{v1, v2, v3}, Provider: echoed{}}); err != nil {
		t.Fatal(err)
	}
	replaced := registry.New()
	for _, p := range []*profile.Profile{v1, v2, v3} {
		if err := replaced.Register(registry.Entry{Profiles: []*profile.Profile{p}, Provider: echoed{}}); err != nil {
			t.Fatal(err)
		}
	}
	for name, reg := range map[string]*registry.Registry{"at start": started, "after hot replaces": replaced} {
		p := historyPipeline(t, reg)
		for _, expect := range []*profile.Profile{v1, v2, v3} {
			rec, err := echoCall(t, p, expect.Exact().String(), `{"text": "hi"}`)
			if err != nil {
				t.Fatalf("%s, written under %s: %v", name, expect.Exact(), err)
			}
			if text, _ := rec.Get("text"); text != "hi" {
				t.Fatalf("%s: %v", name, text)
			}
		}
		if _, err := echoCall(t, p, v1.Exact().String(), `{"text": "hi", "loud": true}`); err == nil || !strings.Contains(err.Error(), "loud") {
			t.Fatalf("%s: a member v1 did not have is refused under v1's own contract (R04): %v", name, err)
		}
	}
}

// PR07, R04 ("a request is validated under its own contract"): a request
// written under an earlier profile is read through that profile's own
// interaction, whatever a later profile names its schema. A rename the gate
// admits never breaks what was written before it.
func TestPR07AnEarlierProfileIsReadThroughItsOwnInteraction(t *testing.T) {
	v1 := historyEcho(t, "request", "", "", 1)
	v2 := historyEcho(t, "request2", "", v1.Exact().String(), 2)
	reg := registry.New()
	if err := reg.Register(registry.Entry{Profiles: []*profile.Profile{v1, v2}, Provider: echoed{}}); err != nil {
		t.Fatalf("the gate admits a rename: %v", err)
	}
	p := historyPipeline(t, reg)
	for _, expect := range []string{v1.Exact().String(), v2.Exact().String(), ""} {
		rec, err := echoCall(t, p, expect, `{"text": "hi"}`)
		if err != nil {
			t.Fatalf("written under %q: %v", expect, err)
		}
		if text, _ := rec.Get("text"); text != "hi" {
			t.Fatalf("written under %q: %v", expect, text)
		}
	}
	if _, err := echoCall(t, p, v1.Exact().String(), `{"txt": "hi"}`); err == nil || !strings.Contains(err.Error(), "invalid_request") {
		t.Fatalf("an invalid request under v1 is invalid under v1's schema: %v", err)
	}
}
