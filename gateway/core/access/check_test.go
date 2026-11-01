package access

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
)

// journeyCatalogue serves four portfolio and data capabilities, each
// reached as the gateway's own identity, and withholds a fifth.
func journeyCatalogue(t *testing.T) Catalogue {
	t.Helper()
	return Catalogue{
		Served: catalogue(t, nil,
			capability(t, "portfolio.holdings", "listed", "read"),
			capability(t, "portfolio.list", "listed", "read"),
			capability(t, "data.status", "listed", "read"),
			capability(t, "nautilus.dashboard", "listed", "read", "write"),
		),
		Withheld: []string{"portfolio.holdings-dates@v1"},
	}
}

func goodPolicy() *ports.Policy {
	return &ports.Policy{
		Revision: "r1",
		Roles:    map[string][]string{"analysts": {"analyst"}},
		Grants: []ports.Grant{
			{ID: "grant-read", Role: "analyst", Capabilities: []string{"portfolio.holdings@v1", "portfolio.list", "portfolio.holdings-dates@v1", "data.status@v1"}, Interactions: []string{"fetch"}},
		},
		Denies: []ports.Deny{{ID: "deny-dave", Subject: "dave", Capabilities: []string{"portfolio.holdings@v1", "portfolio.holdings-dates@v1"}, Reason: "Dave is on the deal team."}},
		Limits: []ports.Limit{
			{Subject: "*", Capability: "*", MaxRows: 100000},
			{Subject: "*", Capability: "portfolio.*", PerMinute: 600},
		},
		HouseRules: []ports.HouseRule{{Text: "Holdings stay inside the firm.", Capabilities: []string{"portfolio.holdings@v1"}}},
	}
}

// R040, 06-7 §6.7.10: every grant and deny names a capability the gateway
// serves (or withholds for now), an interaction it has, or a namespace with
// one; every limit and house rule names what exists. A policy that names
// nothing is refused with its reason, so a misspelt deny can never leave
// the access it meant to remove.
func TestR040APolicyIsHeldToItsCatalogue(t *testing.T) {
	cat := journeyCatalogue(t)
	if reasons := Check(goodPolicy(), cat); len(reasons) > 0 {
		t.Fatalf("a policy that names what is served: %v", reasons)
	}
	for _, c := range []struct {
		name   string
		change func(p *ports.Policy)
		why    string
	}{
		{"a misspelt deny", func(p *ports.Policy) { p.Denies[0].Capabilities[0] = "portfolio.holding@v1" }, `denies[0] (deny-dave): "portfolio.holding@v1" names no capability`},
		{"a misspelt grant", func(p *ports.Policy) { p.Grants[0].Capabilities[1] = "portfolio.lists" }, `grants[0] (grant-read): "portfolio.lists" names no capability`},
		{"another version", func(p *ports.Policy) { p.Grants[0].Capabilities[0] = "portfolio.holdings@v2" }, `"portfolio.holdings@v2" names no capability`},
		{"an interaction it does not have", func(p *ports.Policy) { p.Grants[0].Interactions = []string{"fecth"} }, `"fecth" is not an interaction of portfolio.holdings@v1`},
		{"a deny of an interaction it does not have", func(p *ports.Policy) { p.Denies[0].Interactions = []string{"explain"} }, `"explain" is not an interaction`},
		{"a namespace with nothing in it", func(p *ports.Policy) { p.Denies[0].Capabilities = []string{"risk.*"} }, `"risk.*" names no capability`},
		{"a limit of nothing", func(p *ports.Policy) { p.Limits[1].Capability = "portfolios.*" }, `limits[1]: "portfolios.*" names no capability`},
		{"a house rule of nothing", func(p *ports.Policy) { p.HouseRules[0].Capabilities = []string{"portfolio.holding"} }, `house_rules[0]: "portfolio.holding" names no capability`},
		{"a family grant rule 3 forbids", func(p *ports.Policy) { p.Grants[0].Capabilities = []string{"portfolio.*"} }, "granted by name only (06-3 rule 3)"},
		{"everything, by family", func(p *ports.Policy) { p.Grants[0].Capabilities = []string{"*"} }, "granted by name only (06-3 rule 3)"},
		{"a family grant of a write", func(p *ports.Policy) {
			p.Grants[0].Capabilities, p.Grants[0].Interactions = []string{"nautilus.*"}, []string{"update"}
		}, "granted by name only"},
		{"a grant of a built-in", func(p *ports.Policy) { p.Grants[0].Capabilities = []string{"atlas.whoami@v1"} }, "every authenticated caller's"},
		{"a deny of a built-in", func(p *ports.Policy) { p.Denies[0].Capabilities = []string{"atlas.*"} }, "every authenticated caller's"},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := goodPolicy()
			c.change(p)
			reasons := Check(p, cat)
			if !strings.Contains(strings.Join(reasons, "\n"), c.why) {
				t.Fatalf("reasons %q, want %q", reasons, c.why)
			}
		})
	}
}

// R040, 06-3 rule 3: a family grant is accepted where it reaches something
// rule 3 lets it: the read interactions of a capability whose system is
// reached as the caller.
func TestR040AFamilyGrantThatReachesSomethingIsAccepted(t *testing.T) {
	dash := capability(t, "nautilus.dashboard", "listed", "read", "write")
	cat := Catalogue{Served: catalogue(t, []string{"nautilus.dashboard"}, dash)}
	p := &ports.Policy{Revision: "r1", Grants: []ports.Grant{{ID: "g", Role: "r", Capabilities: []string{"nautilus.*"}}}}
	if reasons := Check(p, cat); len(reasons) > 0 {
		t.Fatalf("%v", reasons)
	}
}

type script struct {
	answers []*ports.Policy
	err     error
	n       int
}

func (s *script) Policy(ctx context.Context) (*ports.Policy, error) {
	if s.err != nil {
		return nil, s.err
	}
	p := s.answers[min(s.n, len(s.answers)-1)]
	s.n++
	return p, nil
}

// R040, R13: a checked source answers a policy that passes, and an error
// for one that does not, so the gateway keeps deciding by the last that
// passed only within its bound. A revision that changes its content
// without a new name is refused: the audit would name two policies alike.
func TestR040TheCheckedSourceRefusesWhatFails(t *testing.T) {
	cat := journeyCatalogue(t)
	misspelt := goodPolicy()
	misspelt.Revision = "r2"
	misspelt.Denies[0].Capabilities = []string{"portfolio.holding@v1"}
	silent := goodPolicy()
	silent.Denies = nil
	src := &script{answers: []*ports.Policy{goodPolicy(), misspelt, silent, goodPolicy()}}
	checked := Checked(src, func() Catalogue { return cat }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	ctx := context.Background()
	if p, err := checked.Policy(ctx); err != nil || p.Revision != "r1" {
		t.Fatalf("%v %v", p, err)
	}
	p, err := checked.Policy(ctx)
	var refused *Refused
	if p != nil || !errors.As(err, &refused) || refused.Revision != "r2" || !strings.Contains(err.Error(), "portfolio.holding@v1") {
		t.Fatalf("a misspelt deny: %v %v", p, err)
	}
	if p, err := checked.Policy(ctx); p != nil || err == nil || !strings.Contains(err.Error(), `"r1" changed`) {
		t.Fatalf("the same revision, other content: %v %v", p, err)
	}
	if p, err := checked.Policy(ctx); err != nil || p.Revision != "r1" {
		t.Fatalf("the policy it accepted, again: %v %v", p, err)
	}
	src.err = errors.New("the store is down")
	if p, err := checked.Policy(ctx); p != nil || !errors.Is(err, src.err) {
		t.Fatalf("a failed read: %v %v", p, err)
	}
	if Checked(nil, func() Catalogue { return cat }, nil) != nil {
		t.Fatal("no source is still no source")
	}
}
