package access

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/jhgonline/fi-app-atlas/gateway/core/registry"
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/profile"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/schema"
)

const voc = `"$schema": "https://json-schema.org/draft/2020-12/schema", "$vocabulary": {"https://json-schema.org/draft/2020-12/vocab/core": true, "https://json-schema.org/draft/2020-12/vocab/applicator": true, "https://json-schema.org/draft/2020-12/vocab/validation": true, "https://json-schema.org/draft/2020-12/vocab/meta-data": true, "https://json-schema.org/draft/2020-12/vocab/format-assertion": true, "https://atlas.jhg.com/vocab/2026-10": false}`

// capability is a small profile of id@v1 with one interaction of each
// effect it is given, and a visibility.
func capability(t *testing.T, id, visibility string, effects ...string) *profile.Profile {
	t.Helper()
	var ins []string
	for _, e := range effects {
		name := map[string]string{"read": "fetch", "write": "update", "external": "notify", "irreversible": "purge"}[e]
		ins = append(ins, `"`+name+`": {"effect": "`+e+`", "result": "record", "delivery": "single", "execution": ["gateway"], "takes": "request", "returns": "result",
		  "deadline": {"default": "PT30S", "max": "PT2M"},
		  "bindings": [{"protocol": "https", "server": "gateway", "method": "POST", "address": "/v1/x/`+id+`@v1:`+name+`", "encodings": ["json"], "auth": "bearer"}]}`)
	}
	vis := ""
	if visibility != "" {
		vis = `, "visibility": "` + visibility + `"`
	}
	doc := `{
	 "identity": {"id": "` + id + `", "version": 1, "title": "T", "summary": "S.", "domain": "` + strings.Split(id, ".")[0] + `", "owner": "QFI"},
	 "properties": {"family": "data", "stability": "official", "lifecycle": "stable"` + vis + `, "traits": []},
	 "interactions": {` + strings.Join(ins, ",") + `},
	 "schemas": {
	  "request": {` + voc + `, "type": "object", "additionalProperties": false, "properties": {"portfolio": {"type": "string", "x-atlas-logical": "PortfolioCode", "description": "P."}, "index": {"type": "string", "x-atlas-logical": "IndexCode", "description": "I."}}},
	  "result": {` + voc + `, "type": "object", "properties": {"portfolio": {"type": "string", "x-atlas-logical": "PortfolioCode", "description": "P."}}}},
	 "errors": [], "meaning": {"grain": "g", "source": "s"}
	}`
	p, err := registry.Load([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

var alice = ports.Identity{Principal: "alice", Kind: "person", Groups: []string{"analysts"}}

func policyOf(grants []ports.Grant, denies ...ports.Deny) *ports.Policy {
	return &ports.Policy{Revision: "r1", Roles: map[string][]string{"analysts": {"analyst"}}, Grants: grants, Denies: denies}
}

func grant(entries ...string) ports.Grant {
	return ports.Grant{ID: "g", Role: "analyst", Capabilities: entries}
}

// catalogue serves each profile, reached as the gateway's own identity
// unless asCaller names it.
func catalogue(t *testing.T, asCaller []string, ps ...*profile.Profile) *registry.Snapshot {
	t.Helper()
	r := registry.New()
	var entries []registry.Entry
	for _, p := range ps {
		reach := registry.AsGateway
		for _, id := range asCaller {
			if id == p.ID {
				reach = registry.AsCaller
			}
		}
		entries = append(entries, registry.Entry{Profiles: []*profile.Profile{p}, Provider: nobody{}, Reach: reach})
	}
	if err := r.Register(entries...); err != nil {
		t.Fatal(err)
	}
	return r.Snapshot()
}

// R041, R034, 06-3 rule 3: a system reached as the gateway's own identity
// cannot narrow by person, so its capabilities are granted by name only.
// A family grant or "*" never reaches one; its pinned id or id does.
func TestR041AGatewayIdentityCapabilityIsGrantedByNameOnly(t *testing.T) {
	holdings := capability(t, "portfolio.holdings", "listed", "read")
	snap := catalogue(t, nil, holdings)
	for _, c := range []struct {
		entry string
		want  bool
	}{
		{"portfolio.*", false},
		{"*", false},
		{"portfolio.holdings@v1", true},
		{"portfolio.holdings", true},
		{"portfolio.holdings@v2", false},
		{"portfolio.holding@v1", false},
	} {
		for _, s := range []*Subject{Resolve(policyOf([]ports.Grant{grant(c.entry)}), alice), ResolveIn(policyOf([]ports.Grant{grant(c.entry)}), alice, snap)} {
			d := s.Decide(holdings, "fetch")
			if d.Allowed != c.want {
				t.Fatalf("a grant of %q: %+v", c.entry, d)
			}
			if !c.want && d.Code != protocol.CodeNotGranted {
				t.Fatalf("a grant of %q that does not reach it: %s", c.entry, d.Code)
			}
			if c.want && d.HeldBy != "g" {
				t.Fatalf("held by %q", d.HeldBy)
			}
		}
	}
}

// ATLAS-ACCESS-002, 06-2 §6.2.4: where a family grant may reach a
// capability at all (its system narrows by the caller), it reaches only
// interactions whose effect is read; anything else is granted by name.
func TestACCESS002AFamilyGrantReachesOnlyReads(t *testing.T) {
	dash := capability(t, "nautilus.dashboard", "listed", "read", "write", "external", "irreversible")
	snap := catalogue(t, []string{"nautilus.dashboard"}, dash)
	family := ResolveIn(policyOf([]ports.Grant{grant("nautilus.*")}), alice, snap)
	everything := ResolveIn(policyOf([]ports.Grant{grant("*")}), alice, snap)
	named := ResolveIn(policyOf([]ports.Grant{grant("nautilus.dashboard@v1")}), alice, snap)
	for name, effect := range map[string]string{"fetch": "read", "update": "write", "notify": "external", "purge": "irreversible"} {
		for who, s := range map[string]*Subject{"nautilus.*": family, "*": everything} {
			if got := s.Decide(dash, name).Allowed; got != (effect == "read") {
				t.Fatalf("%s reaches %s (%s): %v", who, name, effect, got)
			}
		}
		if !named.Decide(dash, name).Allowed {
			t.Fatalf("a grant by name reaches %s (%s)", name, effect)
		}
	}
	// Without a catalogue to say how its system is reached, a capability
	// is taken to be reached as the gateway's own identity (06-3 rule 3).
	if Resolve(policyOf([]ports.Grant{grant("nautilus.*")}), alice).Decide(dash, "fetch").Allowed {
		t.Fatal("a family grant reached a capability it cannot know narrows by person")
	}
}

// R102, ATLAS-CORE §3: visibility fails closed. Anything but listed is
// confidential: a caller who holds no grant of it learns nothing, not even
// that it exists.
func TestR102VisibilityFailsClosed(t *testing.T) {
	for _, vis := range []string{"confidential", "Confidential", "UNKNOWN", "", "secret"} {
		p := capability(t, "admin.keys", vis, "read")
		none := Resolve(policyOf(nil), alice)
		if d := none.Decide(p, "fetch"); d.Allowed || d.Code != protocol.CodeNotFound || none.Visible(p) {
			t.Fatalf("visibility %q to a caller without a grant: %+v, visible %v", vis, d, none.Visible(p))
		}
		holder := Resolve(policyOf([]ports.Grant{grant("admin.keys@v1")}), alice)
		if !holder.Decide(p, "fetch").Allowed || !holder.Visible(p) {
			t.Fatalf("visibility %q to its holder", vis)
		}
		family := Resolve(policyOf([]ports.Grant{grant("admin.*")}), alice)
		if family.Visible(p) {
			t.Fatalf("visibility %q: a family grant that reaches nothing discloses it", vis)
		}
	}
	listed := capability(t, "portfolio.list", "listed", "read")
	if s := Resolve(policyOf(nil), alice); !s.Visible(listed) || s.Decide(listed, "fetch").Code != protocol.CodeNotGranted {
		t.Fatal("a listed capability is shown, and refused not_granted")
	}
}

// ATLAS-ACCESS-001, A-182: a deny always wins, by name or by family, and
// a deny of "*" refuses every caller.
func TestACCESS001ADenyAlwaysWins(t *testing.T) {
	holdings := capability(t, "portfolio.holdings", "listed", "read")
	g := []ports.Grant{grant("portfolio.holdings@v1")}
	for _, d := range []ports.Deny{
		{ID: "d", Subject: "alice", Capabilities: []string{"portfolio.holdings@v1"}, Reason: "No."},
		{ID: "d", Subject: "analysts", Capabilities: []string{"portfolio.*"}, Reason: "No."},
		{ID: "d", Subject: "analyst", Capabilities: []string{"*"}, Reason: "No."},
		{ID: "d", Subject: "*", Capabilities: []string{"portfolio.holdings"}, Reason: "No."},
		{ID: "d", Subject: "*", Capabilities: []string{"portfolio.holdings@v1"}, Interactions: []string{"fetch"}, Reason: "No."},
	} {
		if got := Resolve(policyOf(g, d), alice).Decide(holdings, "fetch"); got.Allowed || got.Code != protocol.CodeDenied || got.Reason != "No." {
			t.Fatalf("deny %+v: %+v", d, got)
		}
	}
	other := ports.Deny{ID: "d", Subject: "bob", Capabilities: []string{"portfolio.holdings@v1"}, Reason: "No."}
	if !Resolve(policyOf(g, other), alice).Decide(holdings, "fetch").Allowed {
		t.Fatal("a deny of someone else")
	}
}

// R071: scope is enforced in each dimension a schema's logical types name:
// a PortfolioCode a portfolio, an IndexCode an index. No table outside
// the protocol has to be changed for a capability scoped by index.
func TestR071ScopeComesFromTheLogicalTypes(t *testing.T) {
	p := capability(t, "index.constituents", "listed", "read")
	pol := policyOf(nil)
	pol.Scopes = map[string]ports.Scope{"analysts": {"index": {"SPX"}, "portfolio": {"*"}}}
	s := Resolve(pol, alice)
	scope := s.ScopeFor(p.Schemas["request"])
	if !scope.Allows("index", "SPX") || scope.Allows("index", "NDX") || !scope.Open("portfolio") {
		t.Fatalf("scope %v", scope)
	}
	req := ijson.NewObject()
	req.Set("index", "NDX")
	req.Set("portfolio", "147HYC")
	vs := Values(p.Schemas["request"], req)
	if len(vs) != 2 || !slices.Contains(vs, Value{"/index", "index", "NDX"}) || !slices.Contains(vs, Value{"/portfolio", "portfolio", "147HYC"}) {
		t.Fatalf("values %+v", vs)
	}
	var none *schema.Schema
	if got := s.ScopeFor(none); len(got) != 0 {
		t.Fatalf("no schema, no dimensions: %v", got)
	}
}

type nobody struct{}

func (nobody) Serve(context.Context, *ports.Invocation) (*ports.Answer, error) {
	return nil, errors.New("not called")
}
