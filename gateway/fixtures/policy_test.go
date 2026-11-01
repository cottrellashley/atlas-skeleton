package fixtures

import (
	"strings"
	"testing"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
)

// R134, AGENTS.md ("fakes stand in for real systems at the same seam"):
// the fixture policy is read by the strict reader production reads its
// policy file with, so a misspelt member fails here exactly as it would
// in production, and never silently drops a deny in every test.
func TestR134TheFixturePolicyIsReadStrictly(t *testing.T) {
	if _, err := Policy(); err != nil {
		t.Fatalf("the fixture: %v", err)
	}
	for _, doc := range []string{
		strings.Replace(string(PolicyJSON()), `"denies"`, `"deny"`, 1),
		strings.Replace(string(PolicyJSON()), `"revision": "dev-2026-10-31.1",`, ``, 1),
	} {
		p, err := parsePolicy([]byte(doc))
		_, want := ports.ParsePolicy([]byte(doc))
		if p != nil || err == nil || want == nil || err.Error() != want.Error() {
			t.Fatalf("read as production reads it: %v, production %v", err, want)
		}
	}
}

// R034, R084, 06-3 rule 3: every journey capability is served from
// Snowflake as the gateway's own identity, so the fixture grants each by
// its pinned id, never by family, and Dave's information barrier covers
// the holdings totals by date as well as the holdings.
func TestR034TheFixtureGrantsByName(t *testing.T) {
	p, err := Policy()
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range p.Grants {
		for _, c := range g.Capabilities {
			if strings.Contains(c, "*") || !strings.Contains(c, "@v") {
				t.Fatalf("%s grants %q, not a pinned id", g.ID, c)
			}
		}
	}
	var dave *ports.Deny
	for i := range p.Denies {
		if p.Denies[i].ID == "deny-dave-holdings" {
			dave = &p.Denies[i]
		}
	}
	if dave == nil || strings.Join(dave.Capabilities, " ") != "portfolio.holdings@v1 portfolio.holdings-dates@v1" {
		t.Fatalf("Dave's deny: %+v", dave)
	}
}
