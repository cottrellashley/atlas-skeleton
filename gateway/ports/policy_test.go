package ports_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
)

// R134, R040, 06-7 §6.7.10: a policy is read strictly. A member it does not
// know, a policy with no revision, and an entry in a form the gateway does
// not decide by are refused with every reason, never dropped, so a typo can
// never silently remove a deny or a limit.
func TestR134APolicyIsReadStrictly(t *testing.T) {
	for _, c := range []struct{ name, doc, why string }{
		{"an unknown member", `{"revision": "r1", "deny": []}`, `unknown field "deny"`},
		{"an unknown member of a grant", `{"revision": "r1", "grants": [{"id": "g", "role": "r", "capabilities": ["a.b@v1"], "interaction": ["fetch"]}]}`, `unknown field "interaction"`},
		{"no revision", `{"grants": []}`, "names its revision"},
		{"a revision a header cannot carry", `{"revision": "r1é"}`, "revision"},
		{"more after the policy", `{"revision": "r1"} {"revision": "r2"}`, "one JSON object"},
		{"not an object", `[]`, "cannot unmarshal"},
		{"a grant with no id", `{"revision": "r1", "grants": [{"role": "r", "capabilities": ["a.b@v1"]}]}`, "grants[0]: has an id"},
		{"two grants with one id", `{"revision": "r1", "grants": [{"id": "g", "role": "r", "capabilities": ["a.b@v1"]}, {"id": "g", "role": "r", "capabilities": ["a.c@v1"]}]}`, `grants[1]: the id "g" is grants[0]'s`},
		{"a grant of nothing", `{"revision": "r1", "grants": [{"id": "g", "role": "r", "capabilities": []}]}`, "grants[0]: names its capabilities"},
		{"an interaction in the capability", `{"revision": "r1", "grants": [{"id": "g", "role": "r", "capabilities": ["portfolio.holdings@v1:fetch"]}]}`, `name the interaction in "interactions"`},
		{"a capability that is no id", `{"revision": "r1", "grants": [{"id": "g", "role": "r", "capabilities": ["Portfolio.Holdings"]}]}`, `"Portfolio.Holdings" is not a capability id`},
		{"a family that is no namespace", `{"revision": "r1", "grants": [{"id": "g", "role": "r", "capabilities": ["portfolio*"]}]}`, `"portfolio*"`},
		{"an interaction that is no name", `{"revision": "r1", "grants": [{"id": "g", "role": "r", "capabilities": ["a.b@v1"], "interactions": ["Fetch"]}]}`, `"Fetch" is not an interaction`},
		{"a deny with no subject", `{"revision": "r1", "denies": [{"id": "d", "capabilities": ["a.b@v1"], "reason": "No."}]}`, "denies[0]: names its subject"},
		{"a deny that says nothing to its subject", `{"revision": "r1", "denies": [{"id": "d", "subject": "dave", "capabilities": ["a.b@v1"]}]}`, "denies[0]: gives its reason"},
		{"a deny with no id", `{"revision": "r1", "denies": [{"subject": "dave", "capabilities": ["a.b@v1"], "reason": "No."}]}`, "denies[0]: has an id"},
		{"a scope in no dimension", `{"revision": "r1", "scopes": {"hy": {"portfolios": ["147HYC"]}}}`, `"portfolios" is not a dimension`},
		{"a scope with an empty value", `{"revision": "r1", "scopes": {"hy": {"portfolio": [""]}}}`, `scopes["hy"]["portfolio"]`},
		{"a limit that limits nothing", `{"revision": "r1", "limits": [{"subject": "*", "capability": "a.*"}]}`, "limits[0]: sets per_minute"},
		{"a negative limit", `{"revision": "r1", "limits": [{"subject": "*", "capability": "a.*", "max_rows": -1}]}`, "limits[0]: per_minute"},
		{"a limit on an interaction", `{"revision": "r1", "limits": [{"subject": "*", "capability": "a.b@v1:fetch", "max_rows": 10}]}`, `limits[0]: "a.b@v1:fetch" names an interaction`},
		{"a house rule by family", `{"revision": "r1", "house_rules": [{"text": "Careful.", "capabilities": ["portfolio.*"]}]}`, "house_rules[0]"},
		{"a house rule that says nothing", `{"revision": "r1", "house_rules": [{"text": "", "capabilities": ["a.b@v1"]}]}`, "house_rules[0]: has its text"},
		{"a role held by no one", `{"revision": "r1", "roles": {"": ["analyst"]}}`, "roles"},
		{"a role named *", `{"revision": "r1", "roles": {"hy": ["*"]}}`, `roles["hy"]: * is not a role`},
		{"a grant to every role", `{"revision": "r1", "grants": [{"id": "g", "role": "*", "capabilities": ["a.b@v1"]}]}`, "grants[0]: is held by a role, and * is not one"},
	} {
		t.Run(c.name, func(t *testing.T) {
			p, err := ports.ParsePolicy([]byte(c.doc))
			if err == nil || p != nil {
				t.Fatalf("%s was read: %+v", c.doc, p)
			}
			if !strings.Contains(err.Error(), c.why) {
				t.Fatalf("the reason names what to fix: %v (want %q)", err, c.why)
			}
		})
	}
}

// R134: every reason is given at once, so one attempt shows the author
// everything to fix.
func TestR134EveryReasonAtOnce(t *testing.T) {
	_, err := ports.ParsePolicy([]byte(`{"revision": "r1", "grants": [{"id": "g", "role": "", "capabilities": ["a.b@v1"]}], "denies": [{"id": "d", "subject": "", "capabilities": ["a.b@v1"], "reason": "No."}]}`))
	var pe *ports.PolicyError
	if !asPolicyError(err, &pe) || len(pe.Problems) != 2 {
		t.Fatalf("got %v", err)
	}
}

func asPolicyError(err error, target **ports.PolicyError) bool {
	pe, ok := err.(*ports.PolicyError)
	if ok {
		*target = pe
	}
	return ok
}

// A-182: the forms the gateway decides by are read: a pinned id, an id
// (every version), a namespace ("portfolio.*"), every capability ("*"), and
// a deny of every caller ("*").
func TestTheFormsAPolicyMayTake(t *testing.T) {
	doc := `{
	 "revision": "r1", "house_rules_revision": "h1",
	 "roles": {"analysts": ["analyst"]},
	 "grants": [{"id": "g", "role": "analyst", "capabilities": ["portfolio.holdings@v1", "portfolio.list", "portfolio.*", "*"], "interactions": ["fetch"]}],
	 "denies": [{"id": "d", "subject": "*", "capabilities": ["portfolio.holdings@v1"], "reason": "Holdings are closed today."}],
	 "scopes": {"analysts": {"portfolio": ["147HYC"], "index": ["*"]}},
	 "limits": [{"subject": "*", "capability": "*", "max_rows": 100000}],
	 "house_rules": [{"text": "Careful.", "capabilities": ["portfolio.holdings@v1", "portfolio.list"]}]
	}`
	p, err := ports.ParsePolicy([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(p)
	again, err := ports.ParsePolicy(b)
	if err != nil {
		t.Fatalf("a policy reads back as it is written: %v\n%s", err, b)
	}
	if b2, _ := json.Marshal(again); string(b2) != string(b) {
		t.Fatalf("round trip:\n %s\n %s", b, b2)
	}
}

// R071: the dimensions are the protocol's two code types, fixed: a
// PortfolioCode names a portfolio and an IndexCode an index (§9.2.3), and
// no other logical type is a dimension.
func TestR071DimensionsComeFromTheLogicalTypes(t *testing.T) {
	for logical, want := range map[string]string{"PortfolioCode": "portfolio", "IndexCode": "index"} {
		if dim, ok := ports.DimensionOf(logical); !ok || dim != want || !ports.IsDimension(want) {
			t.Fatalf("%s: %q %v", logical, dim, ok)
		}
	}
	for _, logical := range []string{"Isin", "Money", "Currency", ""} {
		if dim, ok := ports.DimensionOf(logical); ok {
			t.Fatalf("%s is no dimension, got %q", logical, dim)
		}
	}
	if ports.IsDimension("portfolios") {
		t.Fatal("a typo is no dimension")
	}
}

func FuzzParsePolicy(f *testing.F) {
	f.Add([]byte(`{"revision": "r1", "grants": [{"id": "g", "role": "r", "capabilities": ["portfolio.*", "a.b@v1"], "interactions": ["fetch"]}]}`))
	f.Add([]byte(`{"revision": "r1", "denies": [{"id": "d", "subject": "*", "capabilities": ["a.b"], "reason": "x"}], "scopes": {"s": {"portfolio": ["*"]}}}`))
	f.Add([]byte(`{"revision": "r1", "limits": [{"subject": "*", "capability": "*", "max_rows": 1}], "house_rules": [{"text": "t", "capabilities": ["a.b@v2"]}]}`))
	f.Fuzz(func(t *testing.T, b []byte) {
		p, err := ports.ParsePolicy(b)
		if (p == nil) == (err == nil) {
			t.Fatalf("a policy or an error: %v %v", p, err)
		}
		if p == nil {
			return
		}
		// What is read is what is written: a policy read once reads again
		// the same.
		out, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		again, err := ports.ParsePolicy(out)
		if err != nil {
			t.Fatalf("%s reads, but its own form does not: %v", b, err)
		}
		if out2, _ := json.Marshal(again); string(out2) != string(out) {
			t.Fatalf("round trip:\n %s\n %s", out, out2)
		}
	})
}
