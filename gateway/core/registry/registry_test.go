package registry

import (
	"context"
	"errors"
	"strings"
	"testing"

	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/protocol/profile"
)

type nobody struct{}

func (nobody) Serve(context.Context, *ports.Invocation) (*ports.Answer, error) {
	return nil, errors.New("not called")
}

const voc = `"$schema": "https://json-schema.org/draft/2020-12/schema", "$vocabulary": {"https://json-schema.org/draft/2020-12/vocab/core": true, "https://json-schema.org/draft/2020-12/vocab/applicator": true, "https://json-schema.org/draft/2020-12/vocab/validation": true, "https://json-schema.org/draft/2020-12/vocab/meta-data": true, "https://json-schema.org/draft/2020-12/vocab/format-assertion": true, "https://atlas.jhg.com/vocab/2026-10": false}`

// echo is a small, complete data capability.
func echo(address, extra string) string {
	defs := ""
	if strings.Contains(extra, "$defs") {
		defs = `"$defs": {"number": {"type": "integer", "description": "N."}, "record": {"type": "object", "additionalProperties": false, "properties": {"p": {"type": "string", "x-atlas-logical": "PortfolioCode", "description": "P."}}, "description": "R."}}, `
	}
	result := ""
	if r, ok := strings.CutPrefix(extra, "result:"); ok {
		extra, result = "", r
	}
	return `{
 "identity": {"id": "demo.echo", "version": 1, "title": "Echo", "summary": "Says what it is told.", "domain": "demo", "owner": "QFI"},
 "properties": {"family": "data", "stability": "official", "lifecycle": "stable", "visibility": "listed", "traits": []},
 "interactions": {"fetch": {"effect": "read", "result": "record", "delivery": "single", "execution": ["gateway"], "takes": "request", "returns": "result",
  "deadline": {"default": "PT30S", "max": "PT2M"},
  "bindings": [{"protocol": "https", "server": "gateway", "method": "POST", "address": "` + address + `", "encodings": ["json"], "auth": "bearer"}]}},
 "schemas": {
  "request": {` + voc + `, ` + defs + `"type": "object", "additionalProperties": false, "required": ["text"], "properties": {"text": {"type": "string", "description": "What to say."}` + extra + `}},
  "result": {` + voc + `, "type": "object", "required": ["text"], "properties": {"text": {"type": "string", "description": "What was said."}` + result + `}}},
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

// C1, A-200: a profile's x-atlas-scope names a dimension the deployment
// defines, consistently with the protocol's code types, or the whole
// generation is refused at registration, so nothing scoped is served that
// the gateway cannot enforce.
func TestC1ScopeNamesADefinedDimension(t *testing.T) {
	dims, err := ports.NewDimensions(ports.Dimension{Name: "workspace", Description: "A support workspace."})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		extra, why string
	}{
		{`, "workspace": {"type": "string", "x-atlas-scope": "workspace", "description": "W."}`, ""},
		{`, "desk": {"type": "string", "x-atlas-scope": "desk", "description": "D."}`, `/schemas/request/properties/desk: x-atlas-scope names "desk", which the deployment does not define (it defines index, portfolio, workspace)`},
		{`, "book": {"type": "string", "x-atlas-logical": "PortfolioCode", "x-atlas-scope": "workspace", "description": "B."}`, `/schemas/request/properties/book: x-atlas-scope names "workspace", but a PortfolioCode is a code of "portfolio"`},
		// The review of C1: scope is enforced through properties and items
		// only, so a scoped value anywhere else is refused, never served
		// unenforced; and an x-atlas-scope beside a $ref is a string's.
		{`, "either": {"anyOf": [{"type": "string", "x-atlas-scope": "nosuch", "description": "N."}, {"type": "integer", "description": "I."}], "description": "E."}`, `/schemas/request/properties/either/anyOf/0: a scoped value under anyOf`},
		{`, "pair": {"type": "array", "prefixItems": [{"type": "string", "x-atlas-scope": "workspace", "description": "W."}], "items": false, "description": "P."}`, `/schemas/request/properties/pair/prefixItems/0: a scoped value under prefixItems`},
		{`result:, "map": {"type": "object", "additionalProperties": {"type": "string", "x-atlas-logical": "PortfolioCode", "description": "P."}, "description": "M."}`, `/schemas/result/properties/map/additionalProperties: a scoped value under additionalProperties`},
		{`, "n": {"$ref": "#/$defs/number", "x-atlas-scope": "workspace", "description": "N."}`, `/schemas/request/properties/n: x-atlas-scope stands only on a string`},
		{`, "o": {"$ref": "#/$defs/record", "x-atlas-scope": "workspace", "description": "O."}`, `/schemas/request/properties/o: x-atlas-scope stands only on a string`},
	} {
		r := NewWith(dims)
		err := r.Register(Entry{Profiles: []*profile.Profile{load(t, echo("/v1/demo/demo.echo@v1:fetch", c.extra))}, Provider: nobody{}})
		switch {
		case c.why == "" && err != nil:
			t.Errorf("%s: %v", c.extra, err)
		case c.why != "" && (err == nil || !strings.Contains(err.Error(), c.why)):
			t.Errorf("%s: %v, want %q", c.extra, err, c.why)
		}
		if r.Snapshot().Dimensions() != dims {
			t.Error("the snapshot does not carry the registry's dimensions")
		}
	}
	if err := New().Register(Entry{Profiles: []*profile.Profile{load(t, echo("/v1/demo/demo.echo@v1:fetch", `, "workspace": {"type": "string", "x-atlas-scope": "workspace", "description": "W."}`))}, Provider: nobody{}}); err == nil {
		t.Error("a registry of the protocol's dimensions alone admitted a workspace")
	}
	if _, err := ports.NewDimensions(ports.Dimension{Name: "portfolio", Description: "Again."}); err == nil {
		t.Error("the protocol's own dimension was defined again")
	}
	if _, err := ports.NewDimensions(ports.Dimension{Name: "Desk", Description: "D."}); err == nil {
		t.Error("a dimension that is no name was defined")
	}
}
