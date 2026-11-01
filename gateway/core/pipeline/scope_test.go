package pipeline

import (
	"context"
	"slices"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/arrowschema"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
)

// projectedHoldings answers portfolio.holdings with a row for each of
// portfolios, whatever the caller's scope, holding exactly the columns the
// invocation asks for, as a provider that honours projection strictly does:
// a column it is not asked for is not in its answer.
func projectedHoldings(t *testing.T, batched bool, asked *[]string, portfolios ...string) func(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
	return func(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
		*asked = slices.Clone(inv.Columns)
		p := loadProfile(t, "svc/portfolio/profiles/portfolio.holdings@v1.json")
		row := p.Schemas["result"].Items()
		if inv.Columns != nil {
			row = row.Project(inv.Columns)
		}
		var rows []*ijson.Object
		for _, pf := range portfolios {
			full := holdingsRow(pf, "US-HY-ACME-29")
			o := ijson.NewObject()
			for _, name := range row.Properties() {
				if v, ok := full.Get(name); ok {
					o.Set(name, v)
				}
			}
			rows = append(rows, o)
		}
		if !batched {
			return &ports.Answer{Rows: ports.SliceRows(rows)}, nil
		}
		fields, err := arrowschema.Fields(row)
		if err != nil {
			return nil, err
		}
		sch := arrow.NewSchema(fields, nil)
		b, err := arrowschema.ToBatch(memory.DefaultAllocator, sch, row, rows)
		if err != nil {
			return nil, err
		}
		return &ports.Answer{Batches: &batchStream{schema: sch, batches: []arrow.RecordBatch{b}}}, nil
	}
}

// R05, R106 (the review's regression): a caller's projection that leaves
// out the scoped column never lets another portfolio's rows through. The
// pipeline asks the provider for every scoped property, checks the answer's
// scope on what the provider returned, and only then withholds what the
// caller did not ask for; by rows and by batches alike.
func TestR05AProjectionCannotHideTheScopedColumn(t *testing.T) {
	for _, batched := range []bool{false, true} {
		var asked []string
		r := newRig(t, serve(projectedHoldings(t, batched, &asked, "147HYC", "GLOBAL-CREDIT")), Options{})
		res, err := r.run(r.call("t-bob", holdingsAt, `{"portfolios": ["147HYC"], "columns": ["security_id", "as_of"]}`))
		if err != nil {
			t.Fatal(err)
		}
		got, err := drain(res)
		if code(err) != protocol.CodeInternal {
			t.Errorf("batched %v: GLOBAL-CREDIT, outside Bob's scope, reached him: %d rows, %v", batched, len(got), err)
		}
		if !slices.Contains(asked, "portfolio") {
			t.Errorf("batched %v: the provider was not asked for the scoped column: %v", batched, asked)
		}
		for _, row := range got {
			if _, has := row.Get("portfolio"); has {
				t.Errorf("batched %v: a column the caller did not ask for was sent: %v", batched, row.Names())
			}
		}
	}
}

// omittingHoldings answers portfolio.holdings with a row for portfolio,
// holding only the columns the caller projected: it drops the scoped
// columns the pipeline added (withScoped), as a faulty provider may. Each
// row's portfolio is then set to scope, or removed when scope is absent.
func omittingHoldings(t *testing.T, batched bool, caller []string, scope ijson.Value, absent bool) func(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
	return func(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
		p := loadProfile(t, "svc/portfolio/profiles/portfolio.holdings@v1.json")
		row := p.Schemas["result"].Items()
		keep := caller
		if !absent {
			keep = append(slices.Clone(caller), "portfolio")
		}
		full := holdingsRow("147HYC", "US-HY-ACME-29")
		o := ijson.NewObject()
		for _, name := range keep {
			v, _ := full.Get(name)
			o.Set(name, v)
		}
		if !absent {
			o.Set("portfolio", scope)
		}
		if !batched {
			return &ports.Answer{Rows: ports.SliceRows([]*ijson.Object{o})}, nil
		}
		fields, err := arrowschema.Fields(row.Project(keep))
		if err != nil {
			return nil, err
		}
		if !absent {
			for i := range fields {
				if fields[i].Name == "portfolio" {
					fields[i].Nullable = true
				}
			}
		}
		sch := arrow.NewSchema(fields, nil)
		b, err := arrowschema.ToBatch(memory.DefaultAllocator, sch, row.Project(keep), []*ijson.Object{o})
		if err != nil {
			return nil, err
		}
		return &ports.Answer{Batches: &batchStream{schema: sch, batches: []arrow.RecordBatch{b}}}, nil
	}
}

// R3-01: an answer that lacks the evidence of its scope yields nothing to
// the caller. A provider asked for the scoped column that leaves it out,
// or gives it as null or as a value of another type, has its rows and its
// batches withheld, rather than passed because nothing scoped was there to
// check.
func TestR3_01ScopeEvidenceIsRequired(t *testing.T) {
	caller := []string{"security_id", "as_of"}
	cases := []struct {
		name    string
		batched bool
		value   ijson.Value
		absent  bool
	}{
		{"rows, the scoped column omitted", false, nil, true},
		{"batches, the scoped column omitted", true, nil, true},
		{"rows, the scoped value null", false, nil, false},
		{"batches, the scoped value null", true, nil, false},
		{"rows, the scoped value a number", false, ijson.Number("147"), false},
		{"rows, the scoped value an object", false, ijson.NewObject(), false},
	}
	for _, tc := range cases {
		r := newRig(t, serve(omittingHoldings(t, tc.batched, caller, tc.value, tc.absent)), Options{})
		res, err := r.run(r.call("t-bob", holdingsAt, `{"portfolios": ["147HYC"], "columns": ["security_id", "as_of"]}`))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		got, err := drain(res)
		if code(err) != protocol.CodeInternal || len(got) != 0 {
			t.Errorf("%s: an answer without its scope evidence reached the caller: %d rows, %v", tc.name, len(got), err)
		}
	}
	// The same, with no projection: the whole row is asked for.
	r := newRig(t, serve(omittingHoldings(t, false, []string{"security_id", "as_of"}, nil, true)), Options{})
	res, err := r.run(r.call("t-bob", holdingsAt, `{"portfolios": ["147HYC"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := drain(res); code(err) != protocol.CodeInternal || len(got) != 0 {
		t.Errorf("unprojected: an answer without its scope evidence reached the caller: %d rows, %v", len(got), err)
	}
}

// R3-01, R05: a projected answer whose provider gives its scope evidence is
// sent, by rows and by batches, without the scoped column the caller did not
// ask for. The evidence is checked on the answer as the provider gave it.
func TestR3_01AProjectedAnswerWithItsEvidenceIsSent(t *testing.T) {
	for _, batched := range []bool{false, true} {
		var asked []string
		r := newRig(t, serve(projectedHoldings(t, batched, &asked, "147HYC")), Options{})
		res, err := r.run(r.call("t-bob", holdingsAt, `{"portfolios": ["147HYC"], "columns": ["security_id", "as_of"]}`))
		if err != nil {
			t.Fatal(err)
		}
		got, err := drain(res)
		if err != nil || len(got) != 1 {
			t.Fatalf("batched %v: %d rows, %v", batched, len(got), err)
		}
		if names := got[0].Names(); !slices.Equal(names, []string{"security_id", "as_of"}) {
			t.Errorf("batched %v: the row holds %v", batched, names)
		}
	}
}
