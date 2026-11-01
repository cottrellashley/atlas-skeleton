package builtins

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jhgonline/fi-app-atlas/gateway/core/access"
	"github.com/jhgonline/fi-app-atlas/gateway/core/registry"
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/clock"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/documents"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/profile"
)

// World is a gateway's catalogue, policy and callers for the built-ins'
// tests. Its fund domain is modelled on the journey's portfolio
// capabilities (fund.positions is portfolio.holdings), with one capability
// for each availability condition of §2.6 rule 2: a preview, a critical
// trait, a confidential capability, a deprecated one and a retired one.
//
// The callers: Ana reads two funds; Bea one, with an output limit of her
// own; Cal holds nothing; Dan is denied positions; Job is a workload that
// reads every fund but holds only positions' and freshness' fetch; Adm
// also holds the confidential capability.
type World struct {
	T        testing.TB
	Registry *registry.Registry
	Policy   *ports.Policy
	Clock    *clock.Manual
	Provider *Provider
	Stub     *Stub
	Key      []byte
	// Reach is how the stub reaches the domain's systems: the gateway's own
	// identity unless the world says otherwise (06-3 §6.3.2).
	Reach registry.Reach
}

// The callers' principals.
const (
	Ana = "a0a00000-0000-4000-8000-000000000001"
	Bea = "bea00000-0000-4000-8000-000000000002"
	Cal = "ca100000-0000-4000-8000-000000000003"
	Dan = "da000000-0000-4000-8000-000000000004"
	Job = "70b00000-0000-4000-8000-000000000005"
	Adm = "ad000000-0000-4000-8000-000000000006"
)

// Identities are the callers, as authentication establishes them.
var Identities = map[string]ports.Identity{
	Ana: {Principal: Ana, Name: "Ana Analyst", Kind: "person", Tenant: "tenant-1", Application: "atlas-cli", Groups: []string{"g-analysts"}},
	Bea: {Principal: Bea, Name: "Bea Banks", Kind: "person", Tenant: "tenant-1", Application: "atlas-cli", Groups: []string{"g-narrow"}},
	Cal: {Principal: Cal, Name: "Cal Clark", Kind: "person", Tenant: "tenant-1", Application: "atlas-cli"},
	Dan: {Principal: Dan, Name: "Dan Dealer", Kind: "person", Tenant: "tenant-1", Application: "atlas-cli", Groups: []string{"g-analysts"}},
	Job: {Principal: Job, Name: "nightly-job", Kind: "workload", Tenant: "tenant-1", Application: "nightly", Groups: []string{"g-batch"}},
	Adm: {Principal: Adm, Name: "Adm Admin", Kind: "person", Tenant: "tenant-1", Application: "atlas-cli", Groups: []string{"g-analysts", "g-admins"}},
}

// WorldPolicy is the world's policy: every grant names its capabilities, so
// nothing here depends on how a family grant is read.
func WorldPolicy() *ports.Policy {
	return &ports.Policy{
		Revision: "pol-1",
		Roles:    map[string][]string{"g-analysts": {"fund-analyst"}, "g-narrow": {"fund-analyst"}, "g-batch": {"fund-batch"}, "g-admins": {"fund-admin"}},
		Grants: []ports.Grant{
			{ID: "grant-fund-read", Role: "fund-analyst", Capabilities: []string{
				"fund.positions@v1", "fund.list@v1", "fund.search@v1", "fund.positions-dates@v1", "fund.preview@v1",
				"exotic.curve@v1", "fund.old@v1", "fund.gone@v1", "fund.bare@v1", "data.freshness@v1",
			}},
			{ID: "grant-fund-batch", Role: "fund-batch", Capabilities: []string{"fund.positions@v1", "data.freshness@v1"}, Interactions: []string{"fetch"}},
			{ID: "grant-fund-admin", Role: "fund-admin", Capabilities: []string{"fund.secret@v1"}},
		},
		Denies: []ports.Deny{{ID: "deny-dan-positions", Subject: Dan, Capabilities: []string{"fund.positions@v1"}, Reason: "Dan is barred from positions while on the deal team."}},
		Scopes: map[string]ports.Scope{
			"g-analysts": {"portfolio": {"ALPHA", "BETA"}},
			"g-narrow":   {"portfolio": {"ALPHA"}},
			"g-batch":    {"portfolio": {"*"}},
		},
		Limits: []ports.Limit{
			{Subject: "*", Capability: "fund.*", PerMinute: 600, Concurrency: 16, MaxRows: 100000},
			{Subject: Bea, Capability: "fund.positions@v1", MaxRows: 1000},
		},
		HouseRules: []ports.HouseRule{
			{Text: "Positions are confidential: do not paste them outside the firm.", Capabilities: []string{"fund.positions@v1"}},
			{Text: "Ask the fund desk before you use the secret ledger.", Capabilities: []string{"fund.secret@v1"}},
		},
		HouseRulesRevision: "house-1",
	}
}

// Stub serves the world's domain capabilities, and reports whether it can
// serve them now.
type Stub struct{ down atomic.Bool }

// Serve answers every call with no rows.
func (s *Stub) Serve(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
	return &ports.Answer{Rows: ports.SliceRows(nil)}, nil
}

// Available is false while the stub is down.
func (s *Stub) Available(ctx context.Context, capability string) bool { return !s.down.Load() }

// Down marks the stub's source down, or up again.
func (s *Stub) Down(down bool) { s.down.Store(down) }

// NewWorld registers the built-ins and the fund domain, whose systems the
// stub reaches as the gateway's own identity.
func NewWorld(t testing.TB) *World {
	t.Helper()
	return NewWorldIn(t, registry.AsGateway)
}

// NewWorldIn is the world with its domain's systems reached as reach: as
// the caller, a family grant reaches their reads (ATLAS-ACCESS-002).
func NewWorldIn(t testing.TB, reach registry.Reach) *World {
	t.Helper()
	w := &World{T: t, Registry: registry.New(), Policy: WorldPolicy(), Clock: clock.NewManual(time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)), Stub: &Stub{}, Reach: reach}
	w.Key = []byte("0123456789abcdef0123456789abcdef")
	w.Provider = w.NewProvider(Options{CursorKey: w.Key})
	entries, err := w.Provider.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Registry.Register(append(entries, w.Entries(DomainProfiles()...)...)...); err != nil {
		t.Fatal(err)
	}
	return w
}

// NewProvider is another built-ins provider over the world's registry and
// clock: another replica.
func (w *World) NewProvider(opt Options) *Provider {
	w.T.Helper()
	opt.Registry, opt.Clock, opt.Environment = w.Registry, w.Clock, "test"
	p, err := New(opt)
	if err != nil {
		w.T.Fatal(err)
	}
	return p
}

// Entries are domain profiles, each served by the stub.
func (w *World) Entries(docs ...string) []registry.Entry {
	w.T.Helper()
	var out []registry.Entry
	for _, d := range docs {
		p, err := registry.Load([]byte(d))
		if err != nil {
			w.T.Fatalf("%v\n%s", err, d)
		}
		out = append(out, registry.Entry{Profiles: []*profile.Profile{p}, Provider: w.Stub, Reach: w.Reach})
	}
	return out
}

// Call is one built-in call, as the pipeline hands it over.
type Call struct {
	Who      string
	Address  string // atlas.catalog@v1:fetch
	Body     string
	Features *protocol.Features
	Provider *Provider
	// Subject is the caller as the pipeline resolved them, when not in the
	// catalogue as it stands now.
	Subject *access.Subject
}

// Serve answers a built-in call, its record or its problem.
func (w *World) Serve(c Call) (*ijson.Object, *protocol.Problem) {
	w.T.Helper()
	id, ok := Identities[c.Who]
	if !ok {
		w.T.Fatalf("no caller %s", c.Who)
	}
	pinned, interaction, _ := strings.Cut(c.Address, ":")
	body := c.Body
	if body == "" {
		body = "{}"
	}
	req, err := ijson.Parse([]byte(body))
	if err != nil {
		w.T.Fatal(err)
	}
	f := protocol.CoreFeatures()
	if c.Features != nil {
		f = *c.Features
	}
	p := w.Provider
	if c.Provider != nil {
		p = c.Provider
	}
	// As the pipeline resolves the caller (pipeline.go, step 1): in the
	// catalogue, so a family grant reaches what 06-3 rule 3 lets it.
	subject := c.Subject
	if subject == nil {
		subject = access.ResolveIn(w.Policy, id, w.Registry.Snapshot())
	}
	ctx := access.WithSubject(context.Background(), subject)
	a, err := p.Serve(ctx, &ports.Invocation{Capability: pinned, Interaction: interaction, Request: req.(*ijson.Object), Features: f, Caller: ports.Caller{Principal: id.Principal}})
	if err != nil {
		prob, ok := err.(*protocol.Problem)
		if !ok {
			w.T.Fatalf("%s %s: not a problem: %v", c.Who, c.Address, err)
		}
		if fs := documents.Validate("problem", mustProblem(w.T, prob)); len(fs) > 0 && prob.Next == "" {
			w.T.Fatalf("%s %s: a problem with no next: %+v", c.Who, c.Address, prob)
		}
		return nil, prob
	}
	if a == nil || a.Record == nil {
		w.T.Fatalf("%s %s: no record", c.Who, c.Address)
	}
	return a.Record, nil
}

// Must is a call that answers.
func (w *World) Must(c Call) *ijson.Object {
	w.T.Helper()
	o, prob := w.Serve(c)
	if prob != nil {
		w.T.Fatalf("%s %s %s: %v (next %q)", c.Who, c.Address, c.Body, prob, prob.Next)
	}
	return o
}

// Refused is a call that is refused, and its problem.
func (w *World) Refused(c Call) *protocol.Problem {
	w.T.Helper()
	o, prob := w.Serve(c)
	if prob == nil {
		b, _ := ijson.Marshal(o)
		w.T.Fatalf("%s %s %s answered: %s", c.Who, c.Address, c.Body, b)
	}
	return prob
}

func mustProblem(t testing.TB, p *protocol.Problem) ijson.Value {
	t.Helper()
	v, err := ijson.FromGo(p)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// get reads a member by path, and "" when it is absent.
func get(v ijson.Value, path ...string) ijson.Value {
	for _, p := range path {
		o, ok := v.(*ijson.Object)
		if !ok || o == nil {
			return nil
		}
		v, _ = o.Get(p)
	}
	return v
}

// str is a member as text: a string as itself, anything else as JSON.
func str(v ijson.Value, path ...string) string {
	v = get(v, path...)
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := ijson.Marshal(v)
	return string(b)
}

// pageIDs are a page's items' ids, in order.
func pageIDs(page *ijson.Object) []string {
	var out []string
	items, _ := get(page, "items").([]ijson.Value)
	for _, it := range items {
		out = append(out, str(it, "id"))
	}
	return out
}

// clientFeatures is a client's Atlas-Client-Features.
func clientFeatures(t *testing.T, field string) *protocol.Features {
	t.Helper()
	f, err := protocol.ParseFeatures(field)
	if err != nil {
		t.Fatal(err)
	}
	return &f
}
