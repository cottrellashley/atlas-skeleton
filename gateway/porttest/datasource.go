// Package porttest holds one suite per port. Every implementation, the fakes
// included, passes its port's suite in its own tests, and a technology is
// replaced by a new adapter that passes the same suite (§6.5.7).
package porttest

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/snowarrow"
)

// DataSource checks the data-source contract on one query:
//   - explain shows exactly the statement that runs (ATLAS-DATA-004);
//   - every streamed batch is in the writer schema (ATLAS-DATA-001);
//   - settlement reads the source's own record (§2.2, rule 2);
//   - a handoff, decoded as a client decodes it, gives identical rows, and so
//     do its renewed links (ATLAS-SNOWFLAKE-001, ATLAS-CORE-005).
func DataSource(t *testing.T, ds ports.DataSource, q ports.Query, writer *protocol.Table, exact string, client *http.Client, mem memory.Allocator) {
	t.Helper()
	ctx := context.Background()
	schema, err := protocol.ArrowSchema(writer, exact)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := ds.Plan(ctx, q, writer)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	ex, err := ds.Explain(ctx, plan)
	if err != nil || ex.Statement != plan.Statement {
		t.Fatalf("explain must show the statement that runs: %v\n explain %q\n    plan %q", err, ex.Statement, plan.Statement)
	}
	res, err := ds.Execute(ctx, plan, exact)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	streamed := ReadRows(t, res.Reader, schema)
	res.Reader.Release()
	if len(streamed) == 0 {
		t.Fatal("the query should return rows")
	}
	if ex.Rows <= 0 {
		t.Errorf("explain estimated %d rows for a result of %d", ex.Rows, len(streamed))
	}
	u, err := ds.Settle(ctx, res.QueryID)
	if err != nil || u.Rows != int64(len(streamed)) {
		t.Fatalf("settle: %v, rows %d, streamed %d", err, u.Rows, len(streamed))
	}
	if !slices.Contains(ds.Capabilities().Handoff, snowarrow.Format) {
		return
	}
	h, err := ds.Handoff(ctx, plan, exact)
	if err != nil {
		t.Fatalf("handoff: %v", err)
	}
	if h.Profile != exact || h.Rows != int64(len(streamed)) || h.Format != snowarrow.Format {
		t.Fatalf("handoff document: profile %s rows %d format %s", h.Profile, h.Rows, h.Format)
	}
	handed := ReadHandoff(t, ctx, mem, client, h, schema)
	if !slices.Equal(streamed, handed) {
		t.Fatalf("handed-off rows differ from streamed rows (%d vs %d)", len(handed), len(streamed))
	}
	renewed, err := ds.Renew(ctx, h.QueryID, exact)
	if err != nil {
		t.Fatalf("renew: %v", err)
	}
	if again := ReadHandoff(t, ctx, mem, client, renewed, schema); !slices.Equal(streamed, again) {
		t.Fatal("renewed links give different rows")
	}
}

// ReadHandoff downloads and decodes a handoff as a client does.
func ReadHandoff(t testing.TB, ctx context.Context, mem memory.Allocator, client *http.Client, h *protocol.Handoff, schema *arrow.Schema) []string {
	t.Helper()
	rt, err := snowarrow.ParseSource(h.Source)
	if err != nil {
		t.Fatal(err)
	}
	rd := snowarrow.NewReader(ctx, mem, client, snowarrow.Chunks{RowType: rt, First: h.First, Links: h.Chunks, Headers: h.Headers}, schema, 8)
	defer rd.Release()
	return ReadRows(t, rd, schema)
}

// ReadRows reads every batch, checks each is in the schema, and renders each
// row as text in a canonical form for comparison.
func ReadRows(t testing.TB, rd array.RecordReader, schema *arrow.Schema) []string {
	t.Helper()
	var rows []string
	for rd.Next() {
		rec := rd.RecordBatch()
		if err := protocol.SameColumns(rec.Schema(), schema); err != nil {
			t.Fatalf("batch schema: %v", err)
		}
		for i := range int(rec.NumRows()) {
			vals := make([]string, rec.NumCols())
			for c := range vals {
				vals[c] = rec.Column(c).ValueStr(i)
			}
			rows = append(rows, strings.Join(vals, "|"))
		}
	}
	if err := rd.Err(); err != nil {
		t.Fatalf("reading: %v", err)
	}
	return rows
}
