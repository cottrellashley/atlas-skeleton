package duckwh

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/jhgonline/fi-app-atlas/gateway/adapter/snowflake"
	"github.com/jhgonline/fi-app-atlas/gateway/adapter/snowflake/enginetest"
	"github.com/jhgonline/fi-app-atlas/gateway/fakes"
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/gateway/porttest"
)

// The deprecated warehouse, its statements transpiled for DuckDB, keeps the
// R118, R072, PLAN.md P2-6: the Snowflake Source over DuckDB keeps the
// DataSource port's contract. The Source plans, guards and settles as in
// production; DuckDB runs its Snowflake statements, transpiled. The
// counting decorator only records and faults what the engine is given.
func TestSourceOverDuckDB(t *testing.T) {
	porttest.DataSource(t, func(t *testing.T, tables []fakes.Table) *porttest.Rig {
		e, err := OpenEngine(context.Background(), tables)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { e.Close() })
		counted := enginetest.Count(e)
		s, err := snowflake.NewSource(counted, snowflake.Options{Registered: registered(tables)})
		if err != nil {
			t.Fatal(err)
		}
		return counted.Rig(s)
	})
}

func plan(t *testing.T, s *snowflake.Source, q ports.Query, writer *arrow.Schema, cols [][]string) *ports.Plan {
	t.Helper()
	p, err := s.Plan(context.Background(), ports.PlanRequest{Query: q, Writer: writer, Columns: cols, Capability: "test.read@v1", RequestID: "req-1"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// R113: on a DuckDB whose default null order is nulls first, as a
// Snowflake account's DEFAULT_NULL_ORDERING may be, the statement's own null
// key still puts nulls last, so a limit cuts the same rows.
func TestR113NullsLastWhateverTheSessionSays(t *testing.T) {
	ctx := context.Background()
	s, e, err := OpenSource(ctx, porttest.SourceTables(), snowflake.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if _, err := e.DB().ExecContext(ctx, "SET GLOBAL default_null_order = 'nulls_first'"); err != nil {
		t.Fatal(err)
	}
	p := plan(t, s, ports.Query{Object: porttest.Holdings, Columns: []ports.Column{{Name: "ISIN", Type: ports.String}}, OrderBy: []string{"ISIN"}, Limit: 1},
		arrow.NewSchema([]arrow.Field{{Name: "isin", Type: arrow.BinaryTypes.String, Nullable: true}}, nil), [][]string{{"ISIN"}})
	if got := strings.Join(read(t, s, p), ","); got != "GB0000000003" {
		t.Fatalf("the first ISIN, nulls last: %q", got)
	}
}

// R114: a column named by a word Snowflake reserves is read, because every
// identifier the statement holds is quoted.
func TestR114ReservedWordColumns(t *testing.T) {
	ctx := context.Background()
	tables := []fakes.Table{{Name: "TESTDB.PUBLIC.WORDS", Columns: []ports.Column{{Name: "START", Type: ports.String}, {Name: "ROWS", Type: ports.String}, {Name: "SAMPLE", Type: ports.String}, {Name: "CURRENT", Type: ports.String}},
		Rows: [][]any{{"a", "b", "c", "d"}}}}
	s, e, err := OpenSource(ctx, tables, snowflake.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	var fields []arrow.Field
	var cols [][]string
	var qcols []ports.Column
	for _, c := range tables[0].Columns {
		fields = append(fields, arrow.Field{Name: strings.ToLower(c.Name), Type: arrow.BinaryTypes.String, Nullable: true})
		cols = append(cols, []string{c.Name})
		qcols = append(qcols, c)
	}
	p := plan(t, s, ports.Query{Object: "TESTDB.PUBLIC.WORDS", Columns: qcols, OrderBy: []string{"START"}, Where: []ports.Predicate{{Column: "ROWS", Op: ports.Eq, Type: ports.String, Values: []any{"b"}}}},
		arrow.NewSchema(fields, nil), cols)
	if got := strings.Join(read(t, s, p), ","); got != "a,b,c,d" {
		t.Fatalf("%q from %s", got, p.Statement)
	}
}

// R117 (REVIEW.md acceptance 6: a result can be checked against the
// fixture): a decimal fixture is loaded at the scale its values have, or at
// its column's declared type, so nothing is rounded on the way in; a value
// past its declared type is refused, never rounded.
func TestR117FixtureDecimalsAreExact(t *testing.T) {
	ctx := context.Background()
	tables := []fakes.Table{{Name: "TESTDB.PUBLIC.FX", Columns: []ports.Column{{Name: "PAIR", Type: ports.String}, {Name: "RATE", Type: ports.Decimal}},
		Rows: [][]any{{"EURUSD", "1.0812345678"}, {"GBPUSD", "1.27"}}}}
	s, e, err := OpenSource(ctx, tables, snowflake.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	p := plan(t, s, ports.Query{Object: "TESTDB.PUBLIC.FX", Columns: tables[0].Columns, OrderBy: []string{"PAIR"}},
		arrow.NewSchema([]arrow.Field{{Name: "pair", Type: arrow.BinaryTypes.String}, {Name: "rate", Type: &arrow.Decimal128Type{Precision: 18, Scale: 10}, Nullable: true}}, nil), [][]string{{"PAIR"}, {"RATE"}})
	if got := strings.Join(read(t, s, p), ","); got != "EURUSD,1.0812345678,GBPUSD,1.2700000000" {
		t.Fatalf("%q", got)
	}
	tables[0].Columns[1].Precision, tables[0].Columns[1].Scale = 38, 6
	if _, err := OpenEngine(ctx, tables); err == nil || !strings.Contains(err.Error(), "never rounded") {
		t.Fatalf("a value past its declared scale was loaded: %v", err)
	}
}

// R117, R111: a fixture's instants are loaded as instants and its wall-clock
// times as wall-clock times, whatever the machine's zone, because every
// session is in UTC.
func TestSessionsAreInUTC(t *testing.T) {
	e, err := OpenEngine(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	for range 3 { // each connection of the pool
		var tz string
		if err := e.DB().QueryRowContext(context.Background(), "SELECT current_setting('TimeZone')").Scan(&tz); err != nil || tz != "UTC" {
			t.Fatalf("a session in %q (%v)", tz, err)
		}
	}
}

// R17, INTEGRATIONS ("a partial stream, cancellation ... obeys Atlas's
// result contract"): a statement still running in DuckDB stops when its
// context's deadline passes, long before it would finish, and the engine's
// record says it was cancelled.
func TestACancelledStatementStopsInDuckDB(t *testing.T) {
	e, err := OpenEngine(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	st := &snowflake.Statement{SQL: "SELECT COUNT(*) AS n FROM TABLE(GENERATOR(ROWCOUNT => 100000)) AS a, TABLE(GENERATOR(ROWCOUNT => 100000)) AS b",
		Tag: "atlas:test", Columns: []snowflake.Output{{Name: "n", Type: arrow.PrimitiveTypes.Int64}}}
	a, err := e.Execute(ctx, st)
	if err == nil {
		defer a.Batches.Close()
		_, err = a.Batches.Next(ctx)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a statement past its deadline: %v", err)
	}
	if took := time.Since(start); took > 20*time.Second {
		t.Fatalf("it stopped after %v", took)
	}
	e.mu.Lock()
	var states []snowflake.State
	for _, r := range e.records {
		states = append(states, r.State)
	}
	e.mu.Unlock()
	if len(states) != 1 || states[0] != snowflake.Cancelled {
		t.Fatalf("the records say %v", states)
	}
}

// R120, R052: a statement on a database that has closed is unavailable,
// which a caller may retry elsewhere, not an internal error.
func TestAClosedWarehouseIsUnavailable(t *testing.T) {
	ctx := context.Background()
	s, e, err := OpenSource(ctx, porttest.SourceTables(), snowflake.Options{})
	if err != nil {
		t.Fatal(err)
	}
	p := plan(t, s, ports.Query{Object: porttest.Holdings, Columns: []ports.Column{{Name: "ISIN", Type: ports.String}}},
		arrow.NewSchema([]arrow.Field{{Name: "isin", Type: arrow.BinaryTypes.String, Nullable: true}}, nil), [][]string{{"ISIN"}})
	e.Close()
	if _, err := s.Execute(ctx, p); !errors.Is(err, ports.ErrSourceUnavailable) {
		t.Fatalf("after the database closed: %v", err)
	}
}

// read is every value of every row of a plan's answer, as text.
func read(t *testing.T, s *snowflake.Source, p *ports.Plan) []string {
	t.Helper()
	ctx := context.Background()
	res, err := s.Execute(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Batches.Close()
	var out []string
	for {
		b, err := res.Batches.Next(ctx)
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		for i := range int(b.NumRows()) {
			for _, c := range b.Columns() {
				switch x := c.(type) {
				case *array.String:
					out = append(out, x.Value(i))
				case *array.Decimal128:
					out = append(out, x.Value(i).ToString(x.DataType().(*arrow.Decimal128Type).Scale))
				default:
					out = append(out, c.ValueStr(i))
				}
			}
		}
		b.Release()
	}
}
