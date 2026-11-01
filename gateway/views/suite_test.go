package views_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"atlas.local/gateway/porttest"
	"atlas.local/gateway/views"
)

// R20: the provider runs the port's suite in process, answering batches
// (through a DataSource) and answering rows (through the older warehouse
// port), every journey interaction it serves, refusals included.
func TestR20TheProviderPassesThePortSuite(t *testing.T) {
	r := newRig(t)
	calls := []porttest.ProviderCall{
		{Name: "holdings", Profile: r.profiles[holdings], Invocation: *r.inv(holdings, "fetch", `{"portfolios": ["147HYC", "GLOBAL-CREDIT"]}`, alice), Rows: 8},
		{Name: "holdings projected on a date", Profile: r.profiles[holdings], Invocation: *r.inv(holdings, "fetch", `{"portfolios": ["147HYC"], "as_of": "2026-10-28"}`, bob, "portfolio", "isin", "as_of", "weight"), Rows: 3},
		{Name: "holdings of a credit class a portfolio does not hold", Profile: r.profiles[holdings], Invocation: *r.inv(holdings, "fetch", `{"portfolios": ["EM-RATES"], "asset_class": "credit"}`, alice), Rows: 0},
		{Name: "holdings not yet valued", Profile: r.profiles[holdings], Invocation: *r.inv(holdings, "fetch", `{"portfolios": ["EM-RATES"], "as_of": "2026-10-29"}`, alice), Problem: "no_valuation"},
		{Name: "holdings of no portfolio", Profile: r.profiles[holdings], Invocation: *r.inv(holdings, "fetch", `{"portfolios": ["NOPE"]}`, every), Problem: "portfolio_not_found"},
		{Name: "valuation dates", Profile: r.profiles[dates], Invocation: *r.inv(dates, "fetch", `{"portfolios": ["147HYC", "GLOBAL-CREDIT"]}`, alice), Rows: 5},
		{Name: "an empty window", Profile: r.profiles[dates], Invocation: *r.inv(dates, "fetch", `{"portfolios": ["147HYC"], "from": "2026-10-30", "to": "2026-10-28"}`, alice), Rows: 0},
		{Name: "Bob's list", Profile: r.profiles[list], Invocation: *r.inv(list, "fetch", `{}`, bob), Rows: 1},
		{Name: "every portfolio", Profile: r.profiles[list], Invocation: *r.inv(list, "fetch", `{}`, every), Rows: 4},
		{Name: "a search", Profile: r.profiles[search], Invocation: *r.inv(search, "fetch", `{"words": "credit"}`, alice), Rows: 3},
		{Name: "freshness", Profile: r.profiles[status], Invocation: *r.inv(status, "fetch", `{}`, none), Rows: 5},
		{Name: "freshness of two sources", Profile: r.profiles[status], Invocation: *r.inv(status, "fetch", `{"sources": ["harbour.holdings", "harbour.nothing"]}`, none), Rows: 1},
	}
	t.Run("rows", func(t *testing.T) { porttest.Provider(t, r.rows, calls...) })
	calls = append(calls,
		porttest.ProviderCall{Name: "explain holdings", Profile: r.profiles[holdings], Invocation: *r.inv(holdings, views.Explain, `{"portfolios": ["147HYC"]}`, alice), Rows: -1},
		porttest.ProviderCall{Name: "explain a window", Profile: r.profiles[dates], Invocation: *r.inv(dates, views.Explain, `{"portfolios": ["147HYC"], "to": "2026-10-29"}`, bob), Rows: -1},
		porttest.ProviderCall{Name: "explain a list", Profile: r.profiles[list], Invocation: *r.inv(list, views.Explain, `{"asset_class": "rates"}`, alice), Rows: -1},
	)
	t.Run("batches", func(t *testing.T) { porttest.Provider(t, r.batches, calls...) })
}

// R03, R04, R045 (A-141): a capability's earlier profiles are read in the
// order its chain gives, oldest first; one the chain does not reach is
// refused; and a directory with no history reads its profile alone, which
// the registry then refuses if it names a previous profile.
func TestReadFollowsTheHistoryChain(t *testing.T) {
	fs, err := views.Read(os.DirFS("../svc/portfolio"))
	if err != nil {
		t.Fatal(err)
	}
	published, err := os.ReadFile("../svc/portfolio/history/portfolio.holdings@v1/eec361cece1d6f2f.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if f.Name == holdings && (len(f.Previous) != 1 || string(f.Previous[0]) != string(published)) {
			t.Fatalf("holdings' history: %d profiles", len(f.Previous))
		}
		if f.Name == search && f.Previous != nil {
			t.Fatal("search was never replaced")
		}
	}
	files := fstest.MapFS{}
	for _, f := range fs {
		files["profiles/"+f.Name+".json"] = &fstest.MapFile{Data: f.Profile}
		files["views/"+f.Name+".json"] = &fstest.MapFile{Data: f.Model}
	}
	if fresh, err := views.Read(files); err != nil || len(fresh) != len(fs) || fresh[0].Previous != nil {
		t.Fatalf("no history: %v", err)
	}
	stray := fs[0].Profile
	for _, f := range fs {
		if f.Name == list {
			stray = f.Profile
		}
	}
	files["history/"+holdings+"/stray.json"] = &fstest.MapFile{Data: stray}
	if _, err := views.Read(files); err == nil || !strings.Contains(err.Error(), "does not reach") {
		t.Fatalf("a stray history file: %v", err)
	}
}

// R074 (a model is read as written, or refused at start): ParseModel
// refuses what is not one view model, a member in another case included.
func TestParseModelRefusesWhatIsNotAViewModel(t *testing.T) {
	for _, s := range []string{`{"interaction": "fetch"} {}`, `{"interaction": "fetch", "interaction": "explain"}`, `{"Object": "A.B.C"}`,
		`{"columns": {"a": {"Column": "X"}}}`, `{"nope": 1}`, `[`, `{"order": [1]}`} {
		if _, err := views.ParseModel([]byte(s)); err == nil {
			t.Errorf("%s was read", s)
		}
	}
}

// R074; AGENTS.md (fuzz every parser): ParseModel reads any input without
// panicking, and what it reads it reads back the same.
func FuzzParseModel(f *testing.F) {
	_, models := journey(f)
	for _, m := range models {
		b, _ := json.Marshal(m)
		f.Add(b)
	}
	for _, s := range []string{`{}`, `{"interaction": "fetch"} {}`, `{"interaction": "fetch", "interaction": "explain"}`, `{"columns": {"a": {"money": {}}}}`, `[`, `{"nope": 1}`, `{"Object": "A.B.C"}`} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		m, err := views.ParseModel(b)
		if err != nil {
			return
		}
		again, err := json.Marshal(m)
		if err != nil {
			t.Fatal(err)
		}
		back, err := views.ParseModel(again)
		if err != nil {
			t.Fatalf("%s does not read back: %v", again, err)
		}
		if twice, _ := json.Marshal(back); string(twice) != string(again) {
			t.Fatalf("%s read back as %s", again, twice)
		}
	})
}
