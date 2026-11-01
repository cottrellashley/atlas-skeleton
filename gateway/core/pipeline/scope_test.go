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
