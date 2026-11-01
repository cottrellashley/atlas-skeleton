package portfolio_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/jhgonline/fi-app-atlas/gateway/svc/datastatus"
	"github.com/jhgonline/fi-app-atlas/gateway/svc/portfolio"
	"github.com/jhgonline/fi-app-atlas/gateway/views"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/gate"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/grammar"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/profile"
)

// load reads a profile as the gateway serves it (registry.Load).
func load(t *testing.T, doc []byte) *profile.Profile {
	t.Helper()
	v, err := ijson.Parse(doc)
	if err != nil {
		t.Fatal(err)
	}
	o := v.(*ijson.Object)
	o.Delete("fingerprint")
	p, err := profile.Parse(o)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

type catalogue map[string]bool

func (c catalogue) Has(p grammar.Pinned) bool { return c[p.String()] }

// chains are the journey's capabilities, each its profiles oldest first.
func chains(t *testing.T) map[string][]*profile.Profile {
	t.Helper()
	out := map[string][]*profile.Profile{}
	for _, domain := range []func() ([]views.Files, error){portfolio.Capabilities, datastatus.Capabilities} {
		fs, err := domain()
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range fs {
			var chain []*profile.Profile
			for _, doc := range append(slices.Clone(f.Previous), f.Profile) {
				chain = append(chain, load(t, doc))
			}
			out[f.Name] = chain
		}
	}
	return out
}

func current(t *testing.T, name string) *profile.Profile {
	t.Helper()
	c := chains(t)[name]
	if c == nil {
		t.Fatalf("no capability %s", name)
	}
	return c[len(c)-1]
}

// R03, §3.3.12: every change to a served profile passes the gate against
// the profile it replaced: nothing breaking within the version, the
// previous profile named, a dangerous change announced, and every changed
// contract with a changelog entry and a new prose revision. Each served
// profile is ready for agents (§3.1.4a).
func TestR03EveryChangeToAProfilePassesTheGate(t *testing.T) {
	all := chains(t)
	cat := catalogue{}
	for name := range all {
		cat[name] = true
	}
	for name, chain := range all {
		if r := gate.Check(chain[0], nil, cat); r.Refused() {
			t.Errorf("%s: %s", chain[0].Exact(), r.Reason())
		}
		for i := 1; i < len(chain); i++ {
			old, now := chain[i-1], chain[i]
			r := gate.Check(now, old, cat)
			if r.Refused() {
				t.Errorf("%s against %s: %s", now.Exact(), old.Exact(), r.Reason())
			}
			if old.FP.Value == now.FP.Value {
				t.Errorf("%s: the history holds a profile with the served one's fingerprint", name)
			}
			if len(now.Evolution.Changelog) <= len(old.Evolution.Changelog) || revision(now) <= revision(old) {
				t.Errorf("%s: a changed contract has a changelog entry and a new revision", name)
			}
		}
		if missing := gate.Ready(chain[len(chain)-1]); len(missing) > 0 {
			t.Errorf("%s is not ready for agents: %v", name, missing)
		}
	}
	for _, name := range []string{"portfolio.holdings@v1", "portfolio.holdings-dates@v1", "portfolio.list@v1", "data.status@v1"} {
		if len(all[name]) != 2 {
			t.Errorf("%s keeps the profile it replaced, so a request written under it still resolves: %d profiles", name, len(all[name]))
		}
	}
}

func revision(p *profile.Profile) int64 {
	v, _ := p.Doc.Get("revision")
	n, _ := v.(ijson.Number)
	r, _ := n.Int64()
	return r
}

// R036, ATLAS-DATA-004, ATLAS-CAT-PORTFOLIO-001: the capabilities the
// catalogue lists with explain offer it, a read answering a record, taking
// what fetch takes; search does not (07-the-catalogue).
func TestR036TheJourneysDataCapabilitiesOfferExplain(t *testing.T) {
	for _, name := range []string{"portfolio.holdings@v1", "portfolio.holdings-dates@v1", "portfolio.list@v1"} {
		p := current(t, name)
		ex, fetch := p.Interactions["explain"], p.Interactions["fetch"]
		if ex == nil || ex.Effect != "read" || ex.Result != "record" || ex.Delivery != "single" || ex.Takes != fetch.Takes || ex.Returns != "" ||
			len(ex.Bindings) != 1 || ex.Bindings[0].Address != "/v1/data/"+name+":explain" || !slices.Equal(ex.Bindings[0].Encodings, []string{"json"}) {
			t.Errorf("%s: explain %+v", name, ex)
		}
	}
	if current(t, "portfolio.search@v1").Interactions["explain"] != nil {
		t.Error("the catalogue lists search with fetch only")
	}
}

// text is everything a profile says in words: its meaning, its usage, and
// every schema's description.
func text(p *profile.Profile) string {
	var b strings.Builder
	for _, part := range []string{"meaning", "usage", "schemas", "errors"} {
		v, _ := p.Doc.Get(part)
		bs, _ := ijson.Marshal(v)
		b.Write(bs)
	}
	return b.String()
}

func description(p *profile.Profile, schema string, path ...string) string {
	var v ijson.Value = p.Schemas[schema].Source()
	for _, name := range path {
		o, _ := v.(*ijson.Object)
		if o == nil {
			return ""
		}
		v, _ = o.Get(name)
	}
	o, _ := v.(*ijson.Object)
	if o == nil {
		return ""
	}
	d, _ := o.Get("description")
	s, _ := d.(string)
	return s
}

// R037, R038, R095, R096, R097; REVIEW.md "Left to owners",
// CAPABILITY-HANDBOOK:168: every decision the handbook lists for holdings
// is stated in the profile, and the fixture fixes the same choice
// (gateway/fixtures' tests).
func TestEveryHoldingsOwnerDecisionIsStated(t *testing.T) {
	p := current(t, "portfolio.holdings@v1")
	all := text(p)
	for _, c := range []struct{ decision, says string }{
		{"the history or range request", "One date per call: for a range, call once for each date the portfolio was valued"},
		{"what null as_of means", "each portfolio's latest valuation"},
		{"whether every portfolio uses the same snapshot date", "chosen per portfolio: two portfolios may answer from different dates"},
		{"the key of cash", "cash is CASH-<currency>"},
		{"the key of a derivative", "a derivative is its contract's id"},
		{"the key of a security without an ISIN", "a loan is its facility's id"},
		{"duplicate-lot aggregation", "a security's lots are summed into one row"},
		{"market-value currency", "market_value is in the position's own currency"},
		{"signs", "negative when short"},
		{"whether weights include cash and sum to one", "weights include cash and sum to one"},
		{"partial authorisation", "never silently left out"},
		{"missing portfolios", "no_valuation"},
		{"an empty answer", "an empty answer means nothing matched"},
		{"ordering", "by portfolio, then security_id"},
		{"row limits", "never cut silently"},
		{"source correction", "Harbour overwrites a valuation it corrects"},
		{"freshness", "freshness is per source"},
		{"an old snapshot", "read each row's as_of"},
		{"licensing and persistence (the sample's Licensed trait)", "licensed from no vendor, and confidential under the firm's house rule"},
		{"the identity used downstream", "the gateway's own service identity"},
		{"a closed position", "its quantity, market value and weight are zero"},
	} {
		if !strings.Contains(all, c.says) {
			t.Errorf("%s: the profile does not say %q", c.decision, c.says)
		}
	}
}

// R095: every journey data capability over Harbour states its data
// handling and the identity it reads Harbour as, where the guide renders
// it (the result's description).
func TestR095DataHandlingAndIdentityAreStated(t *testing.T) {
	for _, name := range []string{"portfolio.holdings@v1", "portfolio.holdings-dates@v1"} {
		d := description(current(t, name), "result")
		if !strings.Contains(d, "licensed from no vendor") || !strings.Contains(d, "house rule") || !strings.Contains(d, "service identity") {
			t.Errorf("%s: %q", name, d)
		}
	}
	if d := description(current(t, "portfolio.list@v1"), "result"); !strings.Contains(d, "service identity") {
		t.Errorf("list: %q", d)
	}
}

// R096: holdings states that it takes one date per call; holdings-dates
// states what a window whose from is after its to holds.
func TestR096OneDateAndAnEmptyWindowAreStated(t *testing.T) {
	if d := description(current(t, "portfolio.holdings@v1"), "request", "properties", "as_of"); !strings.Contains(d, "One date per call") {
		t.Errorf("holdings' as_of: %q", d)
	}
	if d := description(current(t, "portfolio.holdings-dates@v1"), "request", "properties", "from"); !strings.Contains(d, "A from after to is an empty window") {
		t.Errorf("holdings-dates' from: %q", d)
	}
}

// R097: both holdings capabilities say a correction overwrites, and every
// row says when Harbour wrote it.
func TestR097TheCorrectionPolicyIsStated(t *testing.T) {
	for _, name := range []string{"portfolio.holdings@v1", "portfolio.holdings-dates@v1"} {
		p := current(t, name)
		row := p.Schemas["result"].Items()
		at, ok := row.Property("loaded_at")
		if !ok || at.Format() != "date-time" || row.Required("loaded_at") || !strings.Contains(description(p, "result", "items", "properties", "loaded_at"), "correction") {
			t.Errorf("%s: loaded_at", name)
		}
	}
}

// R098, ATLAS-CORE:54: holdings' prose names no other capability, so a
// capability held back is never announced by it; the cross-references are
// in related, which the guide holds to the caller's view.
func TestR098HoldingsProseNamesNoOtherCapability(t *testing.T) {
	p := current(t, "portfolio.holdings@v1")
	for _, member := range []string{"when_not", "cost"} {
		v, _ := p.Usage.Get(member)
		if s, _ := v.(string); strings.Contains(s, "@v") {
			t.Errorf("usage.%s names a capability: %q", member, s)
		}
	}
	related, _ := p.Usage.Get("related")
	if !slices.Contains(related.([]ijson.Value), ijson.Value("portfolio.holdings-dates@v1")) {
		t.Error("holdings' related names holdings-dates")
	}
	for _, e := range p.Errors {
		if strings.Contains(e.Next, "holdings-dates") {
			t.Errorf("%s's next names holdings-dates", e.Code)
		}
	}
}

// R037: holdings declares the error for a portfolio not yet valued.
func TestR037HoldingsDeclaresNoValuation(t *testing.T) {
	p := current(t, "portfolio.holdings@v1")
	i := slices.IndexFunc(p.Errors, func(d protocol.Declared) bool { return d.Code == "no_valuation" })
	if i < 0 || p.Errors[i].Status != 404 || p.Errors[i].Retryable || p.Errors[i].Next == "" {
		t.Fatalf("errors %+v", p.Errors)
	}
}
