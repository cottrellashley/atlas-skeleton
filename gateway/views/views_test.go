package views_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"atlas.local/gateway/core/registry"
	"atlas.local/gateway/fixtures"
	"atlas.local/gateway/ports"
	"atlas.local/gateway/svc/datastatus"
	"atlas.local/gateway/svc/portfolio"
	"atlas.local/gateway/views"
	"atlas.local/sdk/go/kit/clock"
	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/arrowschema"
	"atlas.local/sdk/go/protocol/documents"
	"atlas.local/sdk/go/protocol/ijson"
	"atlas.local/sdk/go/protocol/profile"
)

// The fixture's scopes: Alice reads four portfolios, Bob one, the batch
// workload every one (gateway/fixtures/policy.json).
var (
	alice = ports.Scope{"portfolio": {"147HYC", "GLOBAL-CREDIT", "EM-RATES", "EURO-IG"}}
	bob   = ports.Scope{"portfolio": {"147HYC"}}
	every = ports.Scope{"portfolio": {ports.AllValues}}
	none  = ports.Scope{}
)

const (
	positionsV = "HARBOUR.HOLDINGS.POSITIONS_V"
	holdings   = "portfolio.holdings@v1"
	dates      = "portfolio.holdings-dates@v1"
	list       = "portfolio.list@v1"
	search     = "portfolio.search@v1"
	status     = "data.status@v1"
)

// journey is the journey's capabilities, as their owners wrote them.
func journey(t testing.TB) (map[string]*profile.Profile, map[string]views.Model) {
	t.Helper()
	profiles, models := map[string]*profile.Profile{}, map[string]views.Model{}
	for _, domain := range []func() ([]views.Files, error){portfolio.Capabilities, datastatus.Capabilities} {
		fs, err := domain()
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range fs {
			p, err := registry.Load(f.Profile)
			if err != nil {
				t.Fatal(err)
			}
			m, err := views.ParseModel(f.Model)
			if err != nil {
				t.Fatal(err)
			}
			profiles[f.Name], models[f.Name] = p, m
		}
	}
	return profiles, models
}

func bindings(profiles map[string]*profile.Profile, models map[string]views.Model) []views.Binding {
	var out []views.Binding
	for _, name := range []string{holdings, dates, list, search, status} {
		out = append(out, views.Binding{Profile: profiles[name], Model: models[name]})
	}
	return out
}

func harbour(t testing.TB) *source {
	t.Helper()
	tables, err := fixtures.Harbour()
	if err != nil {
		t.Fatal(err)
	}
	return newSource(tables...)
}

// rig is the journey served from the fixture: through the data source,
// answering batches, and through the older warehouse port, answering rows.
type rig struct {
	t        *testing.T
	src      *source
	batches  *views.Provider
	rows     *views.Provider
	profiles map[string]*profile.Profile
	models   map[string]views.Model
	clock    *clock.Manual
}

func newRig(t *testing.T) *rig {
	t.Helper()
	r := &rig{t: t, src: harbour(t), clock: clock.NewManual(time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC))}
	r.profiles, r.models = journey(t)
	var err error
	if r.batches, err = views.Build(r.src, views.Options{Clock: r.clock}, bindings(r.profiles, r.models)...); err != nil {
		t.Fatal(err)
	}
	if r.rows, err = views.Build(r.src, views.Options{Clock: r.clock, Rows: true}, bindings(r.profiles, r.models)...); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r *rig) inv(capability, interaction, request string, scope ports.Scope, columns ...string) *ports.Invocation {
	r.t.Helper()
	v, err := ijson.Parse([]byte(request))
	if err != nil {
		r.t.Fatal(err)
	}
	p := r.profiles[capability]
	return &ports.Invocation{RequestID: "req-" + capability, Capability: capability, Interaction: interaction, Exact: p.Exact().String(),
		Request: v.(*ijson.Object), Columns: columns, Scope: scope, Deadline: r.clock.Now().Add(time.Minute)}
}

// read is every row an answer holds, or the failure it ended with.
func read(t *testing.T, p *profile.Profile, inv *ports.Invocation, ans *ports.Answer) ([]*ijson.Object, error) {
	t.Helper()
	ctx := context.Background()
	var out []*ijson.Object
	switch {
	case ans.Rows != nil:
		defer ans.Rows.Close()
		for {
			row, err := ans.Rows.Next(ctx)
			if errors.Is(err, io.EOF) {
				return out, nil
			}
			if err != nil {
				return out, err
			}
			out = append(out, row)
		}
	case ans.Batches != nil:
		defer ans.Batches.Close()
		row := p.Schemas[p.Interactions["fetch"].Returns].Items()
		if inv.Columns != nil {
			row = row.Project(inv.Columns)
		}
		for {
			b, err := ans.Batches.Next(ctx)
			if errors.Is(err, io.EOF) {
				return out, nil
			}
			if err != nil {
				return out, err
			}
			rows, err := arrowschema.ToRows(b, row)
			b.Release()
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, rows...)
		}
	}
	t.Fatalf("not a table: %+v", ans)
	return nil, nil
}

// both calls each provider and reads its answer.
func (r *rig) both(inv *ports.Invocation, each func(name string, rows []*ijson.Object, err error)) {
	r.t.Helper()
	for _, c := range []struct {
		name string
		p    *views.Provider
	}{{"batches", r.batches}, {"rows", r.rows}} {
		copied := *inv
		ans, err := c.p.Serve(context.Background(), &copied)
		if err != nil {
			each(c.name, nil, err)
			continue
		}
		rows, err := read(r.t, r.profiles[inv.Capability], inv, ans)
		each(c.name, rows, err)
	}
}

func problem(err error) *protocol.Problem {
	var p *protocol.Problem
	if errors.As(err, &p) {
		return p
	}
	return nil
}

func str(o *ijson.Object, path ...string) string {
	var v ijson.Value = o
	for _, p := range path {
		obj, ok := v.(*ijson.Object)
		if !ok {
			return ""
		}
		v, _ = obj.Get(p)
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := ijson.Marshal(v)
	return string(b)
}

func canonical(rows []*ijson.Object) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		b, _ := ijson.Canonical(r)
		out[i] = string(b)
	}
	return out
}

// The journey reads the same through either form: a batch is the rows'
// optimisation, never another meaning (R11), and both are held to the
// same contract.
func TestTheJourneyReadsTheSameAsBatchesOrRows(t *testing.T) {
	r := newRig(t)
	for _, inv := range []*ports.Invocation{
		r.inv(holdings, "fetch", `{"portfolios": ["147HYC", "GLOBAL-CREDIT"]}`, alice),
		r.inv(holdings, "fetch", `{"portfolios": ["147HYC"], "as_of": "2026-10-28"}`, alice, "portfolio", "isin", "as_of", "weight"),
		r.inv(holdings, "fetch", `{"portfolios": ["EURO-IG", "EM-RATES"], "asset_class": "credit"}`, alice),
		r.inv(dates, "fetch", `{"portfolios": ["147HYC", "GLOBAL-CREDIT"], "from": "2026-10-28", "to": "2026-10-30"}`, alice),
		r.inv(list, "fetch", `{}`, bob),
		r.inv(list, "fetch", `{"asset_class": "credit"}`, alice),
		r.inv(search, "fetch", `{"words": "global credit"}`, alice),
		r.inv(status, "fetch", `{}`, none),
	} {
		got := map[string][]string{}
		r.both(inv, func(name string, rows []*ijson.Object, err error) {
			if err != nil {
				t.Fatalf("%s %s: %v", name, inv.Capability, err)
			}
			got[name] = canonical(rows)
		})
		if !slices.Equal(got["batches"], got["rows"]) || len(got["rows"]) == 0 {
			t.Fatalf("%s:\n batches %v\n rows    %v", inv.Capability, got["batches"], got["rows"])
		}
	}
}

// R036, ATLAS-DATA-004, ATLAS-DATA-007: explain answers the plan the same
// request's fetch runs, byte for byte and with the same scope, runs nothing
// at the source, and validates against explain.schema.json.
func TestR036ExplainIsThePlanFetchRuns(t *testing.T) {
	r := newRig(t)
	for _, c := range []struct {
		capability, request string
		scope               ports.Scope
		columns             []string
	}{
		{holdings, `{"portfolios": ["147HYC", "GLOBAL-CREDIT"], "as_of": "2026-10-29", "asset_class": "credit"}`, alice, nil},
		{holdings, `{"portfolios": ["147HYC"]}`, bob, []string{"portfolio", "security_id", "weight"}},
		{dates, `{"portfolios": ["147HYC"], "from": "2026-10-29"}`, alice, nil},
		{list, `{"region": "emea"}`, bob, nil},
	} {
		inv := r.inv(c.capability, views.Explain, c.request, c.scope, c.columns...)
		before := len(r.src.Runs())
		ans, err := r.batches.Serve(context.Background(), inv)
		if err != nil {
			t.Fatalf("%s: explain: %v", c.capability, err)
		}
		if len(r.src.Runs()) != before || r.src.Explained() == 0 {
			t.Fatalf("%s: explain ran something at the source", c.capability)
		}
		if ans.Record == nil || ans.Rows != nil || ans.Batches != nil {
			t.Fatalf("%s: explain answers a record: %+v", c.capability, ans)
		}
		if fs := documents.Validate("explain", ans.Record); len(fs) > 0 {
			t.Fatalf("%s: %v\n%s", c.capability, fs, str(ans.Record))
		}
		fetch := r.inv(c.capability, "fetch", c.request, c.scope, c.columns...)
		if _, err := r.batches.Serve(context.Background(), fetch); err != nil {
			t.Fatal(err)
		}
		runs := r.src.Runs()
		ran := runs[len(runs)-1].statement
		if got := str(ans.Record, "plan", "statement"); got != ran {
			t.Fatalf("%s: explain's statement is not the one fetch ran:\n explain %s\n fetch   %s", c.capability, got, ran)
		}
		if str(ans.Record, "address") != c.capability+":fetch" || str(ans.Record, "profile") != r.profiles[c.capability].Exact().String() ||
			str(ans.Record, "execution") != "gateway" || str(ans.Record, "plan", "query_tag") == "" {
			t.Fatalf("%s: %s", c.capability, str(ans.Record))
		}
		if !strings.Contains(str(ans.Record, "warnings"), "estimate_heuristic") {
			t.Fatalf("%s: a heuristic estimate is labelled (R19): %s", c.capability, str(ans.Record))
		}
		if c.columns != nil {
			props := str(ans.Record, "result_schema", "items", "properties")
			if strings.Contains(props, "market_value") || !strings.Contains(props, "security_id") {
				t.Fatalf("the result schema is the columns asked for: %s", props)
			}
		}
	}
}

// R037: a portfolio with no valuation on or before as_of is refused with
// the declared no_valuation, never answered as no rows; a valued portfolio
// with nothing that matches answers empty, and the two are told apart.
func TestR037APortfolioNotYetValuedIsRefused(t *testing.T) {
	r := newRig(t)
	for _, c := range []struct {
		request, target string
	}{
		{`{"portfolios": ["147HYC", "EM-RATES"], "as_of": "2026-10-29"}`, "/portfolios/1"},
		{`{"portfolios": ["147HYC"], "as_of": "2026-10-01"}`, "/portfolios/0"},
	} {
		r.both(r.inv(holdings, "fetch", c.request, alice), func(name string, rows []*ijson.Object, err error) {
			p := problem(err)
			if p == nil || p.Code != "no_valuation" || p.Target != c.target || p.Status != 404 || p.Retryable || p.Next == "" {
				t.Fatalf("%s %s: %d rows, %v", name, c.request, len(rows), err)
			}
		})
	}
	r.both(r.inv(holdings, "fetch", `{"portfolios": ["EM-RATES"], "asset_class": "credit"}`, alice), func(name string, rows []*ijson.Object, err error) {
		if err != nil || len(rows) != 0 {
			t.Fatalf("%s: a valued portfolio holding no credit answers empty: %d rows, %v", name, len(rows), err)
		}
	})
	r.both(r.inv(holdings, "fetch", `{"portfolios": ["147HYC", "EM-RATES"], "as_of": "2026-10-30"}`, alice), func(name string, rows []*ijson.Object, err error) {
		if err != nil || len(rows) != 8 {
			t.Fatalf("%s: both valued by the 30th: %d rows, %v", name, len(rows), err)
		}
	})
}

// R038: a source at another grain than the contract's, a security's lots as
// rows, is refused as the source's fault, never answered as two positions
// with one key.
func TestR038ARepeatedKeyIsRefused(t *testing.T) {
	r := newRig(t)
	at := time.Date(2026, 10, 31, 5, 15, 0, 0, time.UTC)
	r.src.Add(positionsV, "147HYC", "US-HY-ACME-29", "US00001AA016", "2026-10-30", "Acme Corp 7.25% 2029", "CREDIT", "500.000000", "507.850000", "USD", 0.0001, 412.5, "OPEN", at)
	r.both(r.inv(holdings, "fetch", `{"portfolios": ["147HYC"]}`, alice), func(name string, rows []*ijson.Object, err error) {
		if p := problem(err); p == nil || p.Code != protocol.CodeInternal || p.Retryable {
			t.Fatalf("%s: %d rows, %v", name, len(rows), err)
		}
		for i := 1; i < len(rows); i++ {
			if str(rows[i], "security_id") == str(rows[i-1], "security_id") {
				t.Fatalf("%s: two rows with one key reached the caller", name)
			}
		}
	})
}

// R074, R099: a model that cannot answer its contract stops the gateway at
// start, never on a call: a scope missing or on another column, an object
// the source does not serve, an order that would hide a repeated key, a
// column of the wrong type.
func TestR074R099AModelThatCannotAnswerItsContractStopsAtStart(t *testing.T) {
	profiles, models := journey(t)
	src := harbour(t)
	for _, c := range []struct {
		name, capability, want string
		edit                   func(*views.Model)
	}{
		{"no scope", list, "the scope names the column that holds portfolio", func(m *views.Model) { m.Scope = map[string]string{} }},
		{"a scope on another column", list, "PORTFOLIO_NAME", func(m *views.Model) { m.Scope = map[string]string{"portfolio": "PORTFOLIO_NAME"} }},
		{"a request's dimension with no scope", holdings, "portfolio", func(m *views.Model) { m.Scope = nil }},
		{"an object the source does not serve", holdings, "HARBOUR.HOLDINGS.NOPE_V", func(m *views.Model) { m.Object = "HARBOUR.HOLDINGS.NOPE_V" }},
		{"a reference object it does not serve", holdings, "HARBOUR.REF.NOPE_V", func(m *views.Model) { e := *m.Exists; e.Object = "HARBOUR.REF.NOPE_V"; m.Exists = &e }},
		{"an order that hides a repeated key", holdings, "the order begins with the key's columns", func(m *views.Model) { m.Order = []string{"PORTFOLIO_CODE", "SECURITY_ID"} }},
		{"a column of the wrong type", holdings, "weight is read from a float column", func(m *views.Model) {
			cols := map[string]views.Source{}
			for k, v := range m.Columns {
				cols[k] = v
			}
			w := cols["weight"]
			w.Type = ports.Decimal
			cols["weight"] = w
			m.Columns = cols
		}},
		{"an undeclared error", holdings, "no_such_error", func(m *views.Model) { vd := *m.Valued; vd.Error = "no_such_error"; m.Valued = &vd }},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := models[c.capability]
			c.edit(&m)
			_, err := views.New(src, views.Binding{Profile: profiles[c.capability], Model: m})
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want a refusal naming %q, got %v", c.want, err)
			}
		})
	}
	if _, err := views.New(src, bindings(profiles, models)...); err != nil {
		t.Fatalf("the journey's own models: %v", err)
	}
}

// PR10, R101: a request that names its own portfolios reads only those,
// held to the scope, never the caller's whole scope; a listing pushes the
// whole scope to the source, which binds it.
func TestPR10R101AReadNamesOnlyTheRequestedPortfolios(t *testing.T) {
	r := newRig(t)
	wide := ports.Scope{"portfolio": slices.Clone(alice["portfolio"])}
	for i := range 1496 {
		wide["portfolio"] = append(wide["portfolio"], fmt.Sprintf("SYN-%04d", i))
	}
	before := len(r.src.Plans())
	r.both(r.inv(holdings, "fetch", `{"portfolios": ["147HYC", "GLOBAL-CREDIT"]}`, wide), func(name string, rows []*ijson.Object, err error) {
		if err != nil || len(rows) != 8 {
			t.Fatalf("%s: %d rows, %v", name, len(rows), err)
		}
	})
	var queries []ports.Query
	for _, p := range r.src.Plans()[before:] {
		queries = append(queries, p.Request.Query)
	}
	for _, q := range queries {
		ps := slices.Clone(q.Where)
		if q.Latest != nil {
			ps = append(ps, q.Latest.Within...)
		}
		for _, p := range ps {
			if p.Column == "PORTFOLIO_CODE" && len(p.Values) > 2 {
				t.Fatalf("a read of %s holds %d portfolios for a request of two", q.Object, len(p.Values))
			}
		}
	}
	before = len(r.src.Plans())
	if _, err := r.batches.Serve(context.Background(), r.inv(list, "fetch", `{}`, wide)); err != nil {
		t.Fatal(err)
	}
	q := r.src.Plans()[before].Request.Query
	if len(q.Where) != 1 || len(q.Where[0].Values) != 1500 {
		t.Fatalf("a listing is held to the whole scope: %+v", q.Where)
	}
}

// R110: a request member naming more values than one read may bind is the
// caller's invalid request, at the member, and never reaches the source;
// a source that cannot bind a read's values says so, and a request that
// caused it is refused at its member.
func TestR110AMemberNamingTooManyValuesIsInvalid(t *testing.T) {
	r := newRig(t)
	sources := make([]string, views.MaxValues+1)
	for i := range sources {
		sources[i] = fmt.Sprintf(`"s%d"`, i)
	}
	before := len(r.src.Plans())
	r.both(r.inv(status, "fetch", `{"sources": [`+strings.Join(sources, ",")+`]}`, none), func(name string, rows []*ijson.Object, err error) {
		if p := problem(err); p == nil || p.Code != protocol.CodeInvalidRequest || p.Target != "/sources" || p.Status != 400 || p.Next == "" {
			t.Fatalf("%s: %v", name, err)
		}
	})
	if len(r.src.Plans()) != before {
		t.Fatal("a refused request reached the source")
	}
	tooLarge := &bounded{source: r.src}
	profiles, models := journey(t)
	p, err := views.New(tooLarge, bindings(profiles, models)...)
	if err != nil {
		t.Fatal(err)
	}
	tooLarge.on = true
	_, err = p.Serve(context.Background(), r.inv(dates, "fetch", `{"portfolios": ["147HYC", "GLOBAL-CREDIT"]}`, alice))
	if pr := problem(err); pr == nil || pr.Code != protocol.CodeInvalidRequest || pr.Target != "/portfolios" {
		t.Fatalf("a request too large for the source: %v", err)
	}
	if _, err = p.Serve(context.Background(), r.inv(list, "fetch", `{}`, alice)); err == nil || problem(err) != nil || !errors.Is(err, ports.ErrQueryTooLarge) {
		t.Fatalf("a scope too large for the source is the deployment's fault: %v", err)
	}
}

// bounded is a source that, once on, cannot bind any read's values.
type bounded struct {
	*source
	on bool
}

func (b *bounded) Plan(ctx context.Context, r ports.PlanRequest) (*ports.Plan, error) {
	if b.on {
		return nil, fmt.Errorf("%w: %s", ports.ErrQueryTooLarge, "PORTFOLIO_CODE")
	}
	return b.source.Plan(ctx, r)
}

// R100: the contract's scale is the source's to write: a value with more
// decimals than the contract holds comes back at its scale, rounded by the
// source, and one that still does not fit is internal, never a retryable
// upstream failure.
func TestR100ADecimalPastTheContractScale(t *testing.T) {
	r := newRig(t)
	r.src.Edit(positionsV, func(row []any, col func(string) any) bool {
		return col("PORTFOLIO_CODE") == "147HYC" && col("SECURITY_ID") == "US-HY-ACME-29" && col("AS_OF_DATE") == "2026-10-30"
	}, map[string]any{"MARKET_VALUE": "1014852.17250051"})
	before := len(r.src.Plans())
	r.both(r.inv(holdings, "fetch", `{"portfolios": ["147HYC"]}`, alice), func(name string, rows []*ijson.Object, err error) {
		acme := rows[slices.IndexFunc(rows, func(o *ijson.Object) bool { return str(o, "security_id") == "US-HY-ACME-29" })]
		if err != nil || str(acme, "market_value", "amount") != "1014852.172501" {
			t.Fatalf("%s: rounded by the source to Money's scale: %s, %v", name, str(acme), err)
		}
	})
	for _, p := range r.src.Plans()[before:] {
		for _, c := range p.Request.Query.Columns {
			if c.Type == ports.Decimal && c.Precision == 0 {
				t.Fatalf("%s is read with no contract scale", c.Name)
			}
		}
	}
}

// R035: a status is judged when the call asks, from the dates and how
// recently they were checked: a register that stopped checking is never
// read as current.
func TestR035AStatusIsJudgedWhenTheCallAsks(t *testing.T) {
	r := newRig(t)
	statuses := func() map[string]string {
		out := map[string]map[string]string{}
		r.both(r.inv(status, "fetch", `{}`, none), func(name string, rows []*ijson.Object, err error) {
			if err != nil {
				t.Fatal(err)
			}
			out[name] = map[string]string{}
			for _, row := range rows {
				out[name][str(row, "source")] = str(row, "status")
			}
		})
		if fmt.Sprint(out["batches"]) != fmt.Sprint(out["rows"]) {
			t.Fatalf("%v != %v", out["batches"], out["rows"])
		}
		return out["rows"]
	}
	if s := statuses(); s["harbour.holdings"] != "current" || s["harbour.trades"] != "late" || s["harbour.valuations"] != "current" {
		t.Fatalf("checked six hours ago: %v", s)
	}
	r.clock.Set(time.Date(2026, 11, 1, 8, 0, 1, 0, time.UTC)) // 26h and a second after the check
	for source, s := range statuses() {
		if s != "unchecked" {
			t.Fatalf("a check over 26 hours old: %s is %s", source, s)
		}
	}
}

// R108, R09: an answer settles from the source's own record: its query id
// and rows, and the bytes and cost of every read the call made.
func TestR108AnAnswerSettlesFromTheSourcesRecord(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	before := len(r.src.Runs())
	ans, err := r.batches.Serve(ctx, r.inv(holdings, "fetch", `{"portfolios": ["147HYC"]}`, alice))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := read(t, r.profiles[holdings], &ports.Invocation{}, ans)
	if err != nil || len(rows) != 4 || ans.Settle == nil {
		t.Fatalf("%d rows, %v", len(rows), err)
	}
	runs := r.src.Runs()[before:]
	u, err := ans.Settle(ctx)
	again, err2 := ans.Settle(ctx)
	if err != nil || err2 != nil || len(runs) != 3 || u.QueryID != runs[2].id || u.Rows != 4 || fmt.Sprint(u) != fmt.Sprint(again) {
		t.Fatalf("usage %+v, %v; runs %+v", u, err, runs)
	}
	var bytes int64
	for _, run := range runs {
		bytes += 100 * run.rows
	}
	if u.Bytes != bytes || u.Cost["credits"] != "0.003" {
		t.Fatalf("the reads' bytes and cost: %+v", u)
	}
}

// R006: a read asks the source for one row more than the caller's cap, so
// the pipeline knows whether the cap cut the answer; a search ranks every
// row in scope before its own limit.
func TestR006AReadAsksForOneRowPastTheCap(t *testing.T) {
	r := newRig(t)
	inv := r.inv(dates, "fetch", `{"portfolios": ["147HYC"]}`, alice)
	inv.MaxRows = 2
	ans, err := r.batches.Serve(context.Background(), inv)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := read(t, r.profiles[dates], inv, ans)
	if err != nil || len(rows) != 3 || r.src.Plans()[len(r.src.Plans())-1].Request.Query.Limit != 3 {
		t.Fatalf("%d rows, %v", len(rows), err)
	}
	inv = r.inv(search, "fetch", `{"words": "credit", "limit": 1}`, alice)
	inv.MaxRows = 1
	before := len(r.src.Plans())
	if _, err := r.batches.Serve(context.Background(), inv); err != nil || r.src.Plans()[before].Request.Query.Limit != 0 {
		t.Fatalf("a search reads every row in scope: %v", err)
	}
}

// R097: each row says when Harbour wrote it, so two answers for one as_of
// that differ were restated between them.
func TestR097EachRowSaysWhenItWasWritten(t *testing.T) {
	r := newRig(t)
	inv := r.inv(holdings, "fetch", `{"portfolios": ["GLOBAL-CREDIT"], "as_of": "2026-10-28"}`, alice, "security_id", "market_value", "loaded_at")
	first := map[string]string{}
	r.both(inv, func(name string, rows []*ijson.Object, err error) {
		if err != nil || len(rows) != 4 || str(rows[0], "loaded_at") != "2026-10-30T09:30:00.000000Z" {
			t.Fatalf("%s: the fixture's correction: %v, %v", name, canonical(rows), err)
		}
		first[name] = str(rows[3], "market_value", "amount")
	})
	r.src.Edit(positionsV, func(row []any, col func(string) any) bool {
		return col("PORTFOLIO_CODE") == "GLOBAL-CREDIT" && col("AS_OF_DATE") == "2026-10-28"
	}, map[string]any{"LOADED_AT": time.Date(2026, 11, 2, 7, 0, 0, 0, time.UTC)})
	r.src.Edit(positionsV, func(row []any, col func(string) any) bool {
		return col("PORTFOLIO_CODE") == "GLOBAL-CREDIT" && col("AS_OF_DATE") == "2026-10-28" && col("SECURITY_ID") == "XS-CORP-BETA-28"
	}, map[string]any{"MARKET_VALUE": "-2020000.000000"})
	r.both(inv, func(name string, rows []*ijson.Object, err error) {
		if err != nil || str(rows[0], "loaded_at") != "2026-11-02T07:00:00.000000Z" || str(rows[3], "market_value", "amount") == first[name] {
			t.Fatalf("%s: a restated valuation says so: %v, %v", name, canonical(rows), err)
		}
	})
}

// R05: relevance ranks within the scope and never widens it; a search
// keeps only the best matches within its limit, in order, so its memory is
// bounded by the limit (AGENTS.md: no unbounded buffers).
func TestSearchKeepsItsBestMatches(t *testing.T) {
	r := newRig(t)
	r.both(r.inv(search, "fetch", `{"words": "credit 147", "limit": 2}`, alice), func(name string, rows []*ijson.Object, err error) {
		if err != nil || len(rows) != 2 || str(rows[0], "code") != "147HYC" || str(rows[1], "code") != "EURO-IG" {
			t.Fatalf("%s: %v, %v", name, canonical(rows), err)
		}
	})
	r.both(r.inv(search, "fetch", `{"words": "global credit"}`, bob), func(name string, rows []*ijson.Object, err error) {
		if err != nil || len(rows) != 1 || str(rows[0], "code") != "147HYC" {
			t.Fatalf("%s: relevance ranks within the scope, and never widens it: %v, %v", name, canonical(rows), err)
		}
	})
}

// R120, REVIEW.md acceptance item 5: a source that cannot answer now is
// the capability's declared error, with when to retry; a call's own
// deadline stays the call's; any other failure is wrapped, never swallowed.
func TestASourceFailureIsDeclared(t *testing.T) {
	r := newRig(t)
	r.src.Fail(fmt.Errorf("%w: suspended", ports.ErrSourceUnavailable))
	r.both(r.inv(list, "fetch", `{}`, alice), func(name string, rows []*ijson.Object, err error) {
		if p := problem(err); p == nil || p.Code != "warehouse_suspended" || p.RetryAfter != 5 || !p.Retryable {
			t.Fatalf("%s: %v", name, err)
		}
	})
	r.src.Fail(context.DeadlineExceeded)
	r.both(r.inv(list, "fetch", `{}`, alice), func(name string, rows []*ijson.Object, err error) {
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%s: %v", name, err)
		}
	})
	r.src.Fail(errBoom)
	r.both(r.inv(list, "fetch", `{}`, alice), func(name string, rows []*ijson.Object, err error) {
		if !errors.Is(err, errBoom) || problem(err) != nil || !strings.Contains(err.Error(), list) {
			t.Fatalf("%s: a failure is wrapped with where it happened, never swallowed: %v", name, err)
		}
	})
}

// §6.4 rule 2, R100: what a source answers outside the contract is the
// source's fault, never the caller's: internal, never retried, and never
// passed on. A value the contract's enum does not name is UNKNOWN (§9.5.5).
func TestAnAnswerOutsideItsContractIsInternal(t *testing.T) {
	acme := func(row []any, col func(string) any) bool {
		return col("PORTFOLIO_CODE") == "147HYC" && col("SECURITY_ID") == "US-HY-ACME-29" && col("AS_OF_DATE") == "2026-10-30"
	}
	for _, c := range []struct {
		name string
		set  map[string]any
	}{
		{"a null where the contract has none", map[string]any{"QUANTITY": nil}},
		{"a Money with no amount", map[string]any{"MARKET_VALUE": nil}},
		{"a number JSON cannot hold", map[string]any{"WEIGHT": math.NaN()}},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newRig(t)
			r.src.Edit(positionsV, acme, c.set)
			r.both(r.inv(holdings, "fetch", `{"portfolios": ["147HYC"]}`, alice), func(name string, rows []*ijson.Object, err error) {
				if p := problem(err); p == nil || p.Code != protocol.CodeInternal || p.Retryable {
					t.Fatalf("%s: %v", name, err)
				}
			})
		})
	}
	t.Run("an integer past what JSON holds exactly", func(t *testing.T) {
		r := newRig(t)
		r.src.Edit("HARBOUR.HOLDINGS.VALUATIONS_V", func(row []any, col func(string) any) bool { return col("PORTFOLIO_CODE") == "147HYC" },
			map[string]any{"POSITION_COUNT": int64(1<<53 + 1)})
		r.both(r.inv(dates, "fetch", `{"portfolios": ["147HYC"]}`, alice), func(name string, rows []*ijson.Object, err error) {
			if p := problem(err); p == nil || p.Code != protocol.CodeInternal {
				t.Fatalf("%s: %v", name, err)
			}
		})
	})
	t.Run("a value the enum does not name", func(t *testing.T) {
		r := newRig(t)
		r.src.Edit(positionsV, acme, map[string]any{"ASSET_CLASS": "EQUITY"})
		r.both(r.inv(holdings, "fetch", `{"portfolios": ["147HYC"]}`, alice), func(name string, rows []*ijson.Object, err error) {
			if err != nil || !slices.ContainsFunc(rows, func(o *ijson.Object) bool { return str(o, "asset_class") == "UNKNOWN" }) {
				t.Fatalf("%s: %v %v", name, canonical(rows), err)
			}
		})
	})
	t.Run("a batch outside its writer schema", func(t *testing.T) {
		r := newRig(t)
		profiles, models := journey(t)
		p, err := views.New(&skewed{r.src}, bindings(profiles, models)...)
		if err != nil {
			t.Fatal(err)
		}
		ans, err := p.Serve(context.Background(), r.inv(dates, "fetch", `{"portfolios": ["147HYC"]}`, alice))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := read(t, profiles[dates], &ports.Invocation{}, ans); problem(err) == nil || problem(err).Code != protocol.CodeInternal {
			t.Fatalf("%v", err)
		}
	})
}

// skewed is a source whose batches are not in the writer schema it was
// planned with: every Decimal(38,6) comes back as Decimal(38,8).
type skewed struct{ *source }

func (s *skewed) Execute(ctx context.Context, p *ports.Plan) (*ports.Result, error) {
	fields := slices.Clone(p.Writer.Fields())
	for i, f := range fields {
		if st, ok := f.Type.(*arrow.StructType); ok {
			fields[i].Type = arrow.StructOf(arrow.Field{Name: "amount", Type: &arrow.Decimal128Type{Precision: 38, Scale: 8}}, st.Field(1))
		}
	}
	altered := *p
	altered.Writer = arrow.NewSchema(fields, nil)
	altered.Request.Writer = altered.Writer
	return s.source.Execute(ctx, &altered)
}
