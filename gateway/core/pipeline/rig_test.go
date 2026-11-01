package pipeline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jhgonline/fi-app-atlas/gateway/core/registry"
	"github.com/jhgonline/fi-app-atlas/gateway/fakes"
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/clock"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/profile"
)

// The rig is a pipeline over the journey's own profiles, read from their
// services' files, every one served by a provider the test scripts: the
// real contracts, with the provider's seam in the test's hands.

// serve is a provider written as a function.
type serve func(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error)

func (f serve) Serve(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
	return f(ctx, inv)
}

// journey are the capabilities the rig serves, by their services' files.
var journey = []struct{ service, name string }{
	{"svc/portfolio", "portfolio.holdings@v1"},
	{"svc/portfolio", "portfolio.list@v1"},
	{"svc/portfolio", "portfolio.search@v1"},
	{"svc/datastatus", "data.status@v1"},
}

// loadChain is a capability's profiles within its version, oldest first,
// as its service keeps them: the one served, and before it each profile
// its history holds that the served one's chain of previous profiles
// reaches.
func loadChain(t testing.TB, service, name string) []*profile.Profile {
	t.Helper()
	cur := loadProfile(t, filepath.Join(service, "profiles", name+".json"))
	files, err := filepath.Glob(filepath.Join("..", "..", service, "history", name, "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	held := map[string]*profile.Profile{}
	for _, f := range files {
		rel, _ := filepath.Rel(filepath.Join("..", ".."), f)
		p := loadProfile(t, rel)
		held[p.Exact().String()] = p
	}
	chain := []*profile.Profile{cur}
	for p := cur; p.Evolution.PreviousProfile != nil; {
		prev := held[p.Evolution.PreviousProfile.String()]
		if prev == nil {
			t.Fatalf("%s names %s, which its history does not hold", p.Exact(), p.Evolution.PreviousProfile)
		}
		chain = append([]*profile.Profile{prev}, chain...)
		p = prev
	}
	return chain
}

func loadProfile(t testing.TB, rel string) *profile.Profile {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatal(err)
	}
	p, err := registry.Load(b)
	if err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	return p
}

const voc = `"$schema": "https://json-schema.org/draft/2020-12/schema", "$vocabulary": {"https://json-schema.org/draft/2020-12/vocab/core": true, "https://json-schema.org/draft/2020-12/vocab/applicator": true, "https://json-schema.org/draft/2020-12/vocab/validation": true, "https://json-schema.org/draft/2020-12/vocab/meta-data": true, "https://json-schema.org/draft/2020-12/vocab/format-assertion": true, "https://atlas.jhg.com/vocab/2026-10": false}`

// echo is a record capability: what it is told, and the portfolio it names.
// takes names its request schema, so a later profile can rename it.
func echo(takes, extra, previous string) string {
	evolution := `"lens": [], "changelog": [{"date": "2026-10-01", "change": "First."}]`
	if previous != "" {
		evolution = `"previous_profile": "` + previous + `", "lens": [], "changelog": [{"date": "2026-10-01", "change": "First."}, {"date": "2026-10-02", "change": "More."}]`
	}
	return `{
 "identity": {"id": "demo.echo", "version": 1, "title": "Echo", "summary": "Says what it is told.", "domain": "demo", "owner": "QFI"},
 "properties": {"family": "data", "stability": "official", "lifecycle": "stable", "visibility": "listed", "traits": []},
 "interactions": {"fetch": {"effect": "read", "result": "record", "delivery": "single", "execution": ["gateway"], "takes": "` + takes + `", "returns": "result",
  "deadline": {"default": "PT30S", "max": "PT2M"},
  "bindings": [{"protocol": "https", "server": "gateway", "method": "POST", "address": "/v1/demo/demo.echo@v1:fetch", "encodings": ["json"], "auth": "bearer"}]}},
 "schemas": {
  "` + takes + `": {` + voc + `, "type": "object", "additionalProperties": false, "required": ["text"], "properties": {"text": {"type": "string", "description": "What to say."}` + extra + `}},
  "result": {` + voc + `, "type": "object", "required": ["text"], "properties": {"text": {"type": "string", "description": "What was said."}, "portfolio": {"type": ["string", "null"], "description": "A portfolio it names.", "x-atlas-logical": "PortfolioCode"}}}},
 "errors": [],
 "meaning": {"grain": "one answer", "source": "the request"},
 "usage": {"when": "You need an echo.", "examples": [{"title": "Hello", "interaction": "fetch", "request": {"text": "hello"}}]},
 "evolution": {` + evolution + `}
}`
}

func loadDoc(t testing.TB, doc string) *profile.Profile {
	t.Helper()
	p, err := registry.Load([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// The rig's people: two analysts of different scope, one barred from
// holdings, and one who holds nothing.
var people = map[string]ports.Identity{
	"t-alice": {Principal: "alice", Name: "Alice", Kind: "person", Groups: []string{"credit"}},
	"t-bob":   {Principal: "bob", Name: "Bob", Kind: "person", Groups: []string{"hy"}},
	"t-dave":  {Principal: "dave", Name: "Dave", Kind: "person", Groups: []string{"credit"}},
	"t-carol": {Principal: "carol", Name: "Carol", Kind: "person"},
}

func testPolicy(revision string) *ports.Policy {
	return &ports.Policy{
		Revision: revision,
		Roles:    map[string][]string{"credit": {"analyst"}, "hy": {"analyst"}},
		Grants:   []ports.Grant{{ID: "grant-read", Role: "analyst", Capabilities: []string{"portfolio.holdings@v1", "portfolio.list@v1", "portfolio.search@v1", "data.status@v1", "demo.echo@v1"}}},
		Denies:   []ports.Deny{{ID: "deny-dave", Subject: "dave", Capabilities: []string{"portfolio.holdings@v1"}, Reason: "Dave is barred from holdings."}},
		Scopes: map[string]ports.Scope{
			"credit": {"portfolio": {"147HYC", "GLOBAL-CREDIT", "EM-RATES", "EURO-IG"}},
			"hy":     {"portfolio": {"147HYC"}},
		},
	}
}

type rig struct {
	t      testing.TB
	p      *Pipeline
	reg    *registry.Registry
	audit  *fakes.Audit
	policy *fakes.Policy
	tokens *fakes.Tokens
	clock  clock.Clock
	ids    atomic.Int64
}

// newRig serves the journey's capabilities and demo.echo by provider, under
// opt (its registry, authenticator, policy, audit and clock filled in when
// they are unset).
func newRig(t testing.TB, provider ports.Provider, opt Options) *rig {
	t.Helper()
	r := &rig{t: t, reg: registry.New(), audit: &fakes.Audit{}, policy: fakes.NewPolicy(testPolicy("r1"))}
	if opt.Clock == nil {
		opt.Clock = clock.NewManual(time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC))
	}
	r.clock = opt.Clock
	r.tokens = fakes.NewTokens(opt.Clock)
	for token, id := range people {
		r.tokens.Issue(token, id, time.Time{})
	}
	var entries []registry.Entry
	for _, c := range journey {
		entries = append(entries, registry.Entry{Profiles: loadChain(t, c.service, c.name), Provider: provider})
	}
	entries = append(entries, registry.Entry{Profiles: []*profile.Profile{loadDoc(t, echo("request", "", ""))}, Provider: provider})
	if err := r.reg.Register(entries...); err != nil {
		t.Fatal(err)
	}
	opt.Environment = "test"
	opt.Registry = r.reg
	if opt.Authenticator == nil {
		opt.Authenticator = r.tokens
	}
	if opt.Policy == nil {
		opt.Policy = r.policy
	}
	if opt.Audit == nil {
		opt.Audit = r.audit
	}
	p, err := New(opt)
	if err != nil {
		t.Fatal(err)
	}
	r.p = p
	return r
}

// The bindings the rig's calls go to.
const (
	holdingsAt = "/v1/data/portfolio.holdings@v1:fetch"
	listAt     = "/v1/data/portfolio.list@v1:fetch"
	statusAt   = "/v1/data/data.status@v1:fetch"
	echoAt     = "/v1/demo/demo.echo@v1:fetch"
)

// call is a call as the edge reads it, with its own request id.
func (r *rig) call(token, path, body string) *Call {
	r.t.Helper()
	v, err := ijson.Parse([]byte(body))
	if err != nil {
		r.t.Fatal(err)
	}
	return r.callOf(token, path, v)
}

// callOf is a call of a body already read.
func (r *rig) callOf(token, path string, body ijson.Value) *Call {
	return &Call{
		RequestID: fmt.Sprintf("req-%d", r.ids.Add(1)), Path: path,
		Token: token, Client: &protocol.Client{Name: "rig", Version: "1.0.0", Protocol: "atlas/1.0"},
		Features: protocol.Features{Levels: []string{"core", "streaming", "columnar"}, Deliveries: []string{"single", "chunked"}, Encodings: []string{"json", "ndjson", "arrow"}},
		Body:     body,
	}
}

func (r *rig) run(c *Call) (*Result, error) {
	return r.p.Run(context.Background(), c)
}

// drain reads a table answer to its end: its rows, and the error it ended
// with (nil for a whole answer).
func drain(res *Result) ([]*ijson.Object, error) {
	defer res.Close()
	var rows []*ijson.Object
	for {
		row, err := res.Rows.Next()
		if errors.Is(err, io.EOF) {
			return rows, nil
		}
		if err != nil {
			return rows, err
		}
		rows = append(rows, row)
	}
}

func code(err error) string {
	var p *protocol.Problem
	if errors.As(err, &p) {
		return p.Code
	}
	if err == nil {
		return ""
	}
	return "not a problem: " + err.Error()
}

func problemOf(t testing.TB, err error) *protocol.Problem {
	t.Helper()
	var p *protocol.Problem
	if !errors.As(err, &p) {
		t.Fatalf("not a problem: %v", err)
	}
	return p
}

// listRow is a row of portfolio.list.
func listRow(code string) *ijson.Object {
	o := ijson.NewObject()
	o.Set("code", code)
	o.Set("name", "Portfolio "+code)
	o.Set("region", "emea")
	o.Set("asset_class", "credit")
	o.Set("base_currency", "USD")
	o.Set("status", "active")
	return o
}

func listRows(codes ...string) []*ijson.Object {
	out := make([]*ijson.Object, len(codes))
	for i, c := range codes {
		out[i] = listRow(c)
	}
	return out
}

// rows answers every table call with these rows.
func rows(rs ...*ijson.Object) serve {
	return func(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
		return &ports.Answer{Rows: ports.SliceRows(rs)}, nil
	}
}

// outcomes are the outcome facts of one call.
func (r *rig) facts(requestID string, kind ports.FactKind) []ports.Fact {
	var out []ports.Fact
	for _, f := range r.audit.Of(requestID) {
		if f.Kind == kind {
			out = append(out, f)
		}
	}
	return out
}
