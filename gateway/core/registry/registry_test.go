package registry

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/profile"
)

type nobody struct{}

func (nobody) Serve(context.Context, *ports.Invocation) (*ports.Answer, error) {
	return nil, errors.New("not called")
}

const voc = `"$schema": "https://json-schema.org/draft/2020-12/schema", "$vocabulary": {"https://json-schema.org/draft/2020-12/vocab/core": true, "https://json-schema.org/draft/2020-12/vocab/applicator": true, "https://json-schema.org/draft/2020-12/vocab/validation": true, "https://json-schema.org/draft/2020-12/vocab/meta-data": true, "https://json-schema.org/draft/2020-12/vocab/format-assertion": true, "https://atlas.jhg.com/vocab/2026-10": false}`

// echo is a small, complete data capability.
func echo(address, extra string) string {
	return `{
 "identity": {"id": "demo.echo", "version": 1, "title": "Echo", "summary": "Says what it is told.", "domain": "demo", "owner": "QFI"},
 "properties": {"family": "data", "stability": "official", "lifecycle": "stable", "visibility": "listed", "traits": []},
 "interactions": {"fetch": {"effect": "read", "result": "record", "delivery": "single", "execution": ["gateway"], "takes": "request", "returns": "result",
  "deadline": {"default": "PT30S", "max": "PT2M"},
  "bindings": [{"protocol": "https", "server": "gateway", "method": "POST", "address": "` + address + `", "encodings": ["json"], "auth": "bearer"}]}},
 "schemas": {
  "request": {` + voc + `, "type": "object", "additionalProperties": false, "required": ["text"], "properties": {"text": {"type": "string", "description": "What to say."}` + extra + `}},
  "result": {` + voc + `, "type": "object", "required": ["text"], "properties": {"text": {"type": "string", "description": "What was said."}}}},
 "errors": [],
 "meaning": {"grain": "one answer", "source": "the request"},
 "usage": {"when": "You need an echo.", "examples": [{"title": "Hello", "interaction": "fetch", "request": {"text": "hello"}}]},
 "evolution": {"lens": [], "changelog": [{"date": "2026-10-01", "change": "First."}]}
}`
}

func load(t *testing.T, doc string) *profile.Profile {
	t.Helper()
	p, err := Load([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRegisterAndRoute(t *testing.T) {
	r := New()
	empty := r.Snapshot()
	p := load(t, echo("/v1/demo/demo.echo@v1:fetch", ""))
	if err := r.Register(Entry{Profiles: []*profile.Profile{p}, Provider: nobody{}}); err != nil {
		t.Fatal(err)
	}
	s := r.Snapshot()
	if s.Generation != 1 || s.ETag == empty.ETag || s.Lookup("demo.echo@v1") == nil || s.Profile(p.Exact().String()) != p {
		t.Fatalf("snapshot %+v", s)
	}
	route, ok := s.Route("/v1/demo/demo.echo@v1:fetch")
	if !ok || route.Interaction != "fetch" || route.Capability.Current != p {
		t.Fatalf("route %+v", route)
	}
	if fp, _ := p.Doc.Get("fingerprint"); fp != p.FP.Value {
		t.Fatalf("served with its fingerprint: %v", fp)
	}
}

// §3.3.12: a breaking change within a version is refused at registration,
// and the generation it was part of is discarded whole.
func TestRegisterRefusesABreakingChange(t *testing.T) {
	r := New()
	v1 := load(t, echo("/v1/demo/demo.echo@v1:fetch", ""))
	if err := r.Register(Entry{Profiles: []*profile.Profile{v1}, Provider: nobody{}}); err != nil {
		t.Fatal(err)
	}
	before := r.Snapshot()
	breaking := load(t, strings.Replace(echo("/v1/demo/demo.echo@v1:fetch", `, "loud": {"type": "boolean", "description": "Shout."}`), `"required": ["text"], "properties": {"text": {"type": "string", "description": "What to say."}`, `"required": ["text", "loud"], "properties": {"text": {"type": "string", "description": "What to say."}`, 1))
	other := load(t, strings.Replace(echo("/v1/demo/demo.other@v1:fetch", ""), `"id": "demo.echo"`, `"id": "demo.other"`, 1))
	err := r.Register(Entry{Profiles: []*profile.Profile{breaking}, Provider: nobody{}}, Entry{Profiles: []*profile.Profile{other}, Provider: nobody{}})
	var refused *Refused
	if !errors.As(err, &refused) || !strings.Contains(err.Error(), "became required") {
		t.Fatalf("got %v", err)
	}
	if r.Snapshot() != before || r.Snapshot().Lookup("demo.other@v1") != nil {
		t.Fatal("a refused generation changed the catalogue")
	}
}

// A safe change replaces the served profile, and the one before stays
// readable by its exact id.
func TestRegisterHotReplace(t *testing.T) {
	r := New()
	v1 := load(t, echo("/v1/demo/demo.echo@v1:fetch", ""))
	if err := r.Register(Entry{Profiles: []*profile.Profile{v1}, Provider: nobody{}}); err != nil {
		t.Fatal(err)
	}
	doc := echo("/v1/demo/demo.echo@v1:fetch", `, "loud": {"type": ["boolean", "null"], "default": null, "description": "Shout."}`)
	doc = strings.Replace(doc, `"evolution": {"lens": [], "changelog": [{"date": "2026-10-01", "change": "First."}]}`, `"evolution": {"previous_profile": "`+v1.Exact().String()+`", "lens": [], "changelog": [{"date": "2026-10-01", "change": "First."}, {"date": "2026-10-31", "change": "loud."}]}`, 1)
	v2 := load(t, doc)
	if err := r.Register(Entry{Profiles: []*profile.Profile{v2}, Provider: nobody{}}); err != nil {
		t.Fatal(err)
	}
	s := r.Snapshot()
	if c := s.Lookup("demo.echo@v1"); c.Current != v2 || len(c.Chain) != 2 || s.Profile(v1.Exact().String()) != v1 {
		t.Fatalf("after a hot replace: %+v", c)
	}
}

func TestRegisterRefusesASharedAddress(t *testing.T) {
	r := New()
	a := load(t, echo("/v1/demo/x:fetch", ""))
	b := load(t, strings.Replace(echo("/v1/demo/x:fetch", ""), `"id": "demo.echo"`, `"id": "demo.other"`, 1))
	if err := r.Register(Entry{Profiles: []*profile.Profile{a}, Provider: nobody{}}, Entry{Profiles: []*profile.Profile{b}, Provider: nobody{}}); err == nil || !strings.Contains(err.Error(), "already") {
		t.Fatalf("got %v", err)
	}
}
