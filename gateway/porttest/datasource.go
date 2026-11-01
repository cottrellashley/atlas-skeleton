package porttest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"atlas.local/gateway/fakes"
	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/kit/conc"
)

// Rig is a DataSource under test and the hooks of the engine behind it:
// what the suite needs to see that the engine, not only the port, behaves.
type Rig struct {
	Source ports.DataSource
	// Runs are the queries the engine began, in order.
	Runs func() []fakes.Run
	// Calls counts the calls that reached the engine.
	Calls func() int
	// Hold makes the engine hold each query until the query's context
	// ends, calling reached once it holds one; nil lets queries run.
	Hold func(reached func())
	// Fail makes the engine fail every query with err; nil heals it.
	Fail func(err error)
}

// Prices is the second object SourceTables holds: decimals to round, and
// wall-clock times in a declared zone.
const Prices = "TESTDB.PUBLIC.PRICES"

// wall is a wall-clock time: its clock reading, in no particular zone.
func wall(y int, mo time.Month, d, h, mi int) time.Time {
	return time.Date(y, mo, d, h, mi, 0, 0, time.UTC)
}

// SourceTables is the data the DataSource suite reads: WarehouseTables'
// holdings, and Prices, whose LOCAL_AT holds wall-clock times in
// Europe/London (a TIMESTAMP_NTZ) and whose PX holds decimals with more
// fraction digits than the contracts that read them.
func SourceTables() []fakes.Table {
	return append(WarehouseTables(), fakes.Table{
		Name: Prices,
		Columns: []ports.Column{
			{Name: "ID", Type: ports.String}, {Name: "PX", Type: ports.Decimal, Precision: 38, Scale: 10},
			{Name: "LOCAL_AT", Type: ports.Timestamp, Zone: "Europe/London"}, {Name: "N", Type: ports.Integer},
		},
		Rows: [][]any{
			{"A", "0.125", wall(2026, 7, 1, 7, 0), int64(1)},
			{"B", "-0.125", wall(2026, 1, 15, 7, 0), int64(2)},
			{"C", "1.0812345678", nil, int64(3)},
			{"D", "2.005", wall(2026, 7, 1, 8, 30), int64(4)},
		},
	})
}

// leafOf is a column's contract type, as §9.2.7 derives it for the
// property such a column answers: a decimal at Decimal(38,6).
func leafOf(t ports.ColumnType) arrow.DataType {
	switch t {
	case ports.Integer:
		return arrow.PrimitiveTypes.Int64
	case ports.Float:
		return arrow.PrimitiveTypes.Float64
	case ports.Decimal:
		return &arrow.Decimal128Type{Precision: 38, Scale: 6}
	case ports.Boolean:
		return arrow.FixedWidthTypes.Boolean
	case ports.Date:
		return arrow.FixedWidthTypes.Date32
	case ports.Timestamp:
		return &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}
	}
	return arrow.BinaryTypes.String
}

// read is a query whose writer has one field per column, named for it in
// lower case.
func read(q ports.Query) ports.PlanRequest {
	fields := make([]arrow.Field, len(q.Columns))
	from := make([][]string, len(q.Columns))
	for i, c := range q.Columns {
		fields[i] = arrow.Field{Name: strings.ToLower(c.Name), Type: leafOf(c.Type), Nullable: true}
		from[i] = []string{c.Name}
	}
	md := arrow.NewMetadata([]string{"atlas:profile"}, []string{"test.read@v1"})
	return ports.PlanRequest{Query: q, Writer: arrow.NewSchema(fields, &md), Columns: from, Capability: "test.read@v1", RequestID: "req-1"}
}

// DataSource is the suite of ports.DataSource (§6.4.1, gaps/4 §7.1):
// newRig returns an implementation holding tables, and its engine's hooks.
func DataSource(t *testing.T, newRig func(t *testing.T, tables []fakes.Table) *Rig) {
	rig := newRig(t, SourceTables())
	s := rig.Source
	ctx := context.Background()
	all := columns("PORTFOLIO", "SECURITY", "ISIN", "AS_OF", "MV", "CCY", "WEIGHT", "LOTS", "OPEN", "LOADED_AT")
	keys := columns("PORTFOLIO", "SECURITY", "AS_OF")
	order := []string{"PORTFOLIO", "AS_OF", "SECURITY"}
	plan := func(t *testing.T, r ports.PlanRequest) *ports.Plan {
		t.Helper()
		if r.Query.Object == "" {
			r.Query.Object = Holdings
		}
		p, err := s.Plan(ctx, r)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	// run executes a plan and reads every batch, checking each against the
	// writer schema (06-4 rule 2).
	run := func(t *testing.T, p *ports.Plan) (string, [][]any) {
		t.Helper()
		res, err := s.Execute(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Batches.Close()
		if res.QueryID == "" {
			t.Fatal("no query id")
		}
		if !res.Batches.Schema().Equal(p.Writer) {
			t.Fatalf("the answer's schema is %s, not the writer's %s", res.Batches.Schema(), p.Writer)
		}
		var out [][]any
		for {
			b, err := res.Batches.Next(ctx)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if !b.Schema().Equal(p.Writer) {
				b.Release()
				t.Fatalf("a batch of %s, not the writer's %s", b.Schema(), p.Writer)
			}
			out = append(out, rowsOf(b)...)
			b.Release()
		}
		if _, err := res.Batches.Next(ctx); !errors.Is(err, io.EOF) {
			t.Fatalf("after the last batch: %v", err)
		}
		if err := res.Batches.Close(); err != nil {
			t.Fatalf("a second close: %v", err)
		}
		return res.QueryID, out
	}
	expect := func(t *testing.T, q ports.Query, want ...[]any) {
		t.Helper()
		_, got := run(t, plan(t, read(q)))
		if len(got) != len(want) {
			t.Fatalf("%d rows, want %d:\n got %v\nwant %v", len(got), len(want), got, want)
		}
		for i := range want {
			for k := range want[i] {
				if !same(got[i][k], want[i][k], q.Columns[k].Type) {
					t.Fatalf("row %d, %s: got %#v (%T), want %#v", i, q.Columns[k].Name, got[i][k], got[i][k], want[i][k])
				}
			}
		}
	}
	last := func(t *testing.T) fakes.Run {
		t.Helper()
		runs := rig.Runs()
		if len(runs) == 0 {
			t.Fatal("the engine recorded no query")
		}
		return runs[len(runs)-1]
	}

	// ATLAS-DATA-004, R118: explain's statement is the one execute runs, byte
	// for byte, and the tag goes beside it, never in it (CRITIQUE #13, R051).
	t.Run("explain shows the statement execute runs, and the tag is out of band", func(t *testing.T) {
		r := read(ports.Query{Columns: keys, OrderBy: order, Where: []ports.Predicate{{Column: "PORTFOLIO", Op: ports.In, Type: ports.String, Values: []any{"P1"}}}})
		r.Capability, r.RequestID = "test.holdings@v1", "req-7f3c"
		p := plan(t, r)
		e, err := s.Explain(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		if e.Statement != p.Statement || e.Tag != p.Tag || e.Dialect != p.Dialect || e.Language != "sql" {
			t.Fatalf("explain %+v of plan %+v", e, p)
		}
		calls := rig.Calls()
		if _, err := s.Explain(ctx, p); err != nil || rig.Calls() != calls {
			t.Fatalf("explain reached the engine (ATLAS-DATA-007): %v", err)
		}
		run(t, p)
		got := last(t)
		if got.Statement != e.Statement {
			t.Fatalf("explain shows\n%s\nexecute ran\n%s", e.Statement, got.Statement)
		}
		if got.Tag != p.Tag || !strings.Contains(p.Tag, "test.holdings@v1") || !strings.Contains(p.Tag, "req-7f3c") {
			t.Fatalf("the engine recorded the tag %q for the plan's %q", got.Tag, p.Tag)
		}
		if strings.Contains(p.Statement, p.Tag) || strings.Contains(p.Statement, "req-7f3c") {
			t.Fatalf("the tag is in the statement: %s", p.Statement)
		}
		again := plan(t, r)
		if again.Statement != p.Statement || fmt.Sprint(again.Binds) != fmt.Sprint(p.Binds) {
			t.Fatalf("the same read planned twice: %q %v, then %q %v", p.Statement, p.Binds, again.Statement, again.Binds)
		}
	})
	// R19: an estimate is a heuristic or the engine's own, null when unknown.
	t.Run("an estimate is null or at least zero", func(t *testing.T) {
		e, err := s.Explain(ctx, plan(t, read(ports.Query{Columns: keys})))
		if err != nil {
			t.Fatal(err)
		}
		for name, v := range map[string]*int64{"rows": e.Rows, "bytes": e.Bytes} {
			if v != nil && *v < 0 {
				t.Errorf("an estimate of %d %s", *v, name)
			}
		}
		if e.Cost != nil {
			if r, ok := new(big.Rat).SetString(e.Cost.Amount); !ok || r.Sign() < 0 || e.Cost.Unit == "" {
				t.Errorf("a cost of %+v", e.Cost)
			}
		}
	})
	// R09, R118: settlement is the engine's own record, and its rows are the
	// rows streamed. Settling again gives the same.
	t.Run("settle's rows are the rows streamed", func(t *testing.T) {
		id, rows := run(t, plan(t, read(ports.Query{Columns: keys, OrderBy: order})))
		for range 2 {
			u, err := s.Settle(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			if u.QueryID != id || u.Rows != int64(len(rows)) || u.Bytes < 0 {
				t.Fatalf("settled %+v for %d rows of %s", u, len(rows), id)
			}
		}
		if _, err := s.Settle(ctx, "no-such-query"); !errors.Is(err, ports.ErrUnknownQuery) {
			t.Fatalf("a query it never ran: %v", err)
		}
	})
	// 06-1 (a cancelled caller cancels downstream), R17, R119: the engine's
	// query stops when the context ends.
	t.Run("a cancelled context stops the engine's query", func(t *testing.T) {
		p := plan(t, read(ports.Query{Columns: keys, OrderBy: order}))
		done, cancel := context.WithCancel(ctx)
		defer cancel()
		rig.Hold(cancel)
		_, err := s.Execute(done, p)
		rig.Hold(nil)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("an execution whose context ended while the engine held it: %v", err)
		}
		if got := last(t); !got.Stopped || got.Statement != p.Statement {
			t.Fatalf("the engine's record of it: %+v", got)
		}
	})
	t.Run("cancelled mid-stream, the answer ends with an error, never io.EOF", func(t *testing.T) {
		p := plan(t, read(ports.Query{Columns: keys, OrderBy: order}))
		done, cancel := context.WithCancel(ctx)
		defer cancel()
		res, err := s.Execute(done, p)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Batches.Close()
		b, err := res.Batches.Next(done)
		if err != nil {
			t.Fatal(err)
		}
		b.Release()
		cancel()
		if _, err := res.Batches.Next(done); err == nil || errors.Is(err, io.EOF) || !errors.Is(err, context.Canceled) {
			t.Fatalf("after the context ended: %v", err)
		}
	})
	// R052, R119: an engine that cannot answer now is ErrSourceUnavailable,
	// which a caller may retry; any other failure is the engine's own,
	// wrapped, never swallowed.
	t.Run("an engine's failure keeps its kind", func(t *testing.T) {
		p := plan(t, read(ports.Query{Columns: keys}))
		boom := errors.New("boom")
		rig.Fail(fmt.Errorf("the warehouse is suspended: %w", ports.ErrSourceUnavailable))
		_, err := s.Execute(ctx, p)
		rig.Fail(boom)
		_, err2 := s.Execute(ctx, p)
		rig.Fail(nil)
		if !errors.Is(err, ports.ErrSourceUnavailable) || !errors.Is(err, ports.ErrWarehouseUnavailable) {
			t.Fatalf("an unavailable engine: %v", err)
		}
		if !errors.Is(err2, boom) || errors.Is(err2, ports.ErrSourceUnavailable) {
			t.Fatalf("a failing engine: %v", err2)
		}
		run(t, p) // healed
	})
	// INTEGRATIONS ("excessive inputs are refused before driver execution"),
	// R119: what the source will not read never reaches its engine.
	t.Run("a read it refuses never reaches the engine", func(t *testing.T) {
		calls := rig.Calls()
		if _, err := s.Plan(ctx, read(ports.Query{Object: "TESTDB.PUBLIC.SECRETS", Columns: columns("PORTFOLIO")})); !errors.Is(err, ports.ErrUnknownObject) {
			t.Fatalf("an object it does not serve: %v", err)
		}
		r := read(ports.Query{Object: Holdings, Columns: columns("MV")})
		r.Writer = arrow.NewSchema([]arrow.Field{{Name: "mv", Type: arrow.FixedWidthTypes.Boolean, Nullable: true}}, nil)
		if _, err := s.Plan(ctx, r); !errors.Is(err, ports.ErrPlanRefused) {
			t.Fatalf("a decimal read as a boolean: %v", err)
		}
		r = read(ports.Query{Object: Holdings, Columns: columns("MV")})
		r.Columns = [][]string{{"NOT_READ"}}
		if _, err := s.Plan(ctx, r); !errors.Is(err, ports.ErrPlanRefused) {
			t.Fatalf("a field made from a column the query does not read: %v", err)
		}
		if got := rig.Calls(); got != calls {
			t.Fatalf("%d calls reached the engine", got-calls)
		}
	})
	t.Run("columns in order, nulls last", func(t *testing.T) {
		expect(t, ports.Query{Columns: keys, OrderBy: order},
			[]any{"P1", "S1", "2026-10-29"}, []any{"P1", "S2", "2026-10-29"}, []any{"P1", "S1", "2026-10-30"},
			[]any{"P2", "S3", "2026-10-28"}, []any{"P2", "S3", "2026-10-31"}, []any{nil, "S9", "2026-10-30"})
	})
	// R113, R119: nulls last is the statement's, so a limit cuts the same
	// rows whatever the session's DEFAULT_NULL_ORDERING.
	t.Run("nulls last under a limit", func(t *testing.T) {
		expect(t, ports.Query{Columns: columns("ISIN", "AS_OF"), OrderBy: []string{"ISIN", "AS_OF"}, Limit: 4},
			[]any{"GB0000000003", "2026-10-28"}, []any{"GB0000000003", "2026-10-31"}, []any{"US0000000001", "2026-10-29"}, []any{"US0000000001", "2026-10-30"})
	})
	t.Run("in, eq and bounds", func(t *testing.T) {
		expect(t, ports.Query{Columns: keys, OrderBy: order, Where: []ports.Predicate{{Column: "PORTFOLIO", Op: ports.In, Type: ports.String, Values: []any{"P1", "P9"}}, {Column: "CCY", Op: ports.Eq, Type: ports.String, Values: []any{"USD"}}}},
			[]any{"P1", "S1", "2026-10-29"}, []any{"P1", "S1", "2026-10-30"})
		expect(t, ports.Query{Columns: keys, OrderBy: order, Where: []ports.Predicate{{Column: "AS_OF", Op: ports.Gte, Type: ports.Date, Values: []any{"2026-10-29"}}, {Column: "AS_OF", Op: ports.Lte, Type: ports.Date, Values: []any{"2026-10-30"}}}},
			[]any{"P1", "S1", "2026-10-29"}, []any{"P1", "S2", "2026-10-29"}, []any{"P1", "S1", "2026-10-30"}, []any{nil, "S9", "2026-10-30"})
	})
	// R112, PR10: an IN longer than one group of values reads the same rows.
	t.Run("an IN past one group of values", func(t *testing.T) {
		values := []any{"P1"}
		for i := range 1000 {
			values = append(values, fmt.Sprintf("Q%04d", i))
		}
		values = append(values, "P2")
		expect(t, ports.Query{Columns: keys, OrderBy: order, Where: []ports.Predicate{{Column: "PORTFOLIO", Op: ports.In, Type: ports.String, Values: values}}},
			[]any{"P1", "S1", "2026-10-29"}, []any{"P1", "S2", "2026-10-29"}, []any{"P1", "S1", "2026-10-30"},
			[]any{"P2", "S3", "2026-10-28"}, []any{"P2", "S3", "2026-10-31"})
	})
	t.Run("a decimal compares by value, past a float's precision", func(t *testing.T) {
		expect(t, ports.Query{Columns: keys, OrderBy: order, Where: []ports.Predicate{{Column: "MV", Op: ports.Gte, Type: ports.Decimal, Values: []any{"1000.123456"}}}},
			[]any{"P1", "S1", "2026-10-29"}, []any{"P1", "S1", "2026-10-30"}, []any{"P2", "S3", "2026-10-28"})
	})
	// R119: an integer past 2^53 compares exactly, never as a float.
	t.Run("an integer past 2^53 compares exactly", func(t *testing.T) {
		expect(t, ports.Query{Columns: keys, Where: []ports.Predicate{{Column: "LOTS", Op: ports.Eq, Type: ports.Integer, Values: []any{bigInt}}}},
			[]any{"P1", "S1", "2026-10-30"})
		expect(t, ports.Query{Columns: keys, Where: []ports.Predicate{{Column: "LOTS", Op: ports.Eq, Type: ports.Integer, Values: []any{bigInt - 1}}}})
	})
	// R111, R119: a timestamp compares as an instant, whatever its offset:
	// 23:00+05:00 is 18:00Z, before S1's 18:00:00.123456Z.
	t.Run("a timestamp compares as an instant, whatever its offset", func(t *testing.T) {
		expect(t, ports.Query{Columns: keys, OrderBy: order, Where: []ports.Predicate{{Column: "LOADED_AT", Op: ports.Lte, Type: ports.Timestamp, Values: []any{"2026-10-29T23:00:00+05:00"}}}},
			[]any{"P1", "S2", "2026-10-29"}, []any{"P2", "S3", "2026-10-28"})
	})
	t.Run("a null meets no condition", func(t *testing.T) {
		expect(t, ports.Query{Columns: keys, OrderBy: order, Where: []ports.Predicate{{Column: "ISIN", Op: ports.In, Type: ports.String, Values: []any{"US0000000001", "GB0000000003"}}, {Column: "OPEN", Op: ports.Eq, Type: ports.Boolean, Values: []any{true}}}},
			[]any{"P1", "S1", "2026-10-29"}, []any{"P1", "S1", "2026-10-30"}, []any{"P2", "S3", "2026-10-28"})
	})
	t.Run("the latest of each group", func(t *testing.T) {
		expect(t, ports.Query{Columns: keys, OrderBy: order, Latest: &ports.Latest{Date: "AS_OF", Per: []string{"PORTFOLIO"}}},
			[]any{"P1", "S1", "2026-10-30"}, []any{"P2", "S3", "2026-10-31"})
		expect(t, ports.Query{Columns: keys, OrderBy: order, Latest: &ports.Latest{Date: "AS_OF", Per: []string{"PORTFOLIO"}, OnOrBefore: "2026-10-30"}},
			[]any{"P1", "S1", "2026-10-30"}, []any{"P2", "S3", "2026-10-28"})
	})
	t.Run("the latest is chosen before the query's conditions", func(t *testing.T) {
		expect(t, ports.Query{Columns: keys, OrderBy: order,
			Latest: &ports.Latest{Date: "AS_OF", Per: []string{"PORTFOLIO"}, Within: []ports.Predicate{{Column: "CCY", Op: ports.Eq, Type: ports.String, Values: []any{"EUR"}}}},
			Where:  []ports.Predicate{{Column: "PORTFOLIO", Op: ports.In, Type: ports.String, Values: []any{"P1"}}}},
			[]any{"P1", "S1", "2026-10-29"}, []any{"P1", "S2", "2026-10-29"})
	})
	t.Run("every type, exactly", func(t *testing.T) {
		expect(t, ports.Query{Columns: all, OrderBy: order, Where: []ports.Predicate{{Column: "SECURITY", Op: ports.In, Type: ports.String, Values: []any{"S3"}}}},
			[]any{"P2", "S3", "GB0000000003", "2026-10-28", "12345678901234567890.123456", "GBP", 1.0, nil, true, ts("2026-10-28T18:00:00.000001Z")},
			[]any{"P2", "S3", "GB0000000003", "2026-10-31", "2.5", "GBP", 1.0, int64(2), nil, nil})
		expect(t, ports.Query{Columns: all, OrderBy: order, Where: []ports.Predicate{{Column: "PORTFOLIO", Op: ports.Eq, Type: ports.String, Values: []any{"P1"}}}},
			[]any{"P1", "S1", "US0000000001", "2026-10-29", "1000.123456", "USD", 0.5, int64(3), true, ts("2026-10-29T18:00:00.123456Z")},
			[]any{"P1", "S2", nil, "2026-10-29", "-200.000001", "EUR", 0.5, int64(1), false, ts("2026-10-29T18:00:00Z")},
			[]any{"P1", "S1", "US0000000001", "2026-10-30", "1010", "USD", 1.0, bigInt, true, ts("2026-10-30T18:00:00Z")})
	})
	t.Run("a limit", func(t *testing.T) {
		expect(t, ports.Query{Columns: keys, OrderBy: order, Limit: 2}, []any{"P1", "S1", "2026-10-29"}, []any{"P1", "S2", "2026-10-29"})
	})
	// R100: the statement casts to the contract's decimal scale, so the
	// source rounds, half away from zero; an exact value at its own scale is
	// read exactly (R117).
	t.Run("a decimal is read at its contract's scale", func(t *testing.T) {
		at := func(scale int32) [][]any {
			r := read(ports.Query{Object: Prices, Columns: []ports.Column{{Name: "ID", Type: ports.String}, {Name: "PX", Type: ports.Decimal}}, OrderBy: []string{"ID"}})
			r.Writer = arrow.NewSchema([]arrow.Field{{Name: "id", Type: arrow.BinaryTypes.String}, {Name: "px", Type: &arrow.Decimal128Type{Precision: 38, Scale: scale}, Nullable: true}}, nil)
			_, rows := run(t, plan(t, r))
			return rows
		}
		if got, want := fmt.Sprint(at(2)), "[[A 0.13] [B -0.13] [C 1.08] [D 2.01]]"; got != want {
			t.Errorf("at scale 2: %s, want %s", got, want)
		}
		if got, want := fmt.Sprint(at(10)), "[[A 0.1250000000] [B -0.1250000000] [C 1.0812345678] [D 2.0050000000]]"; got != want {
			t.Errorf("at scale 10: %s, want %s", got, want)
		}
	})
	// §9.2.7: a Money field is its amount, at Decimal(38,6), and currency.
	t.Run("a Money is its amount and its currency", func(t *testing.T) {
		money := arrow.StructOf(arrow.Field{Name: "amount", Type: &arrow.Decimal128Type{Precision: 38, Scale: 6}}, arrow.Field{Name: "currency", Type: arrow.BinaryTypes.String})
		r := ports.PlanRequest{Query: ports.Query{Object: Holdings, Columns: columns("SECURITY", "MV", "CCY", "AS_OF"), OrderBy: order, Where: []ports.Predicate{{Column: "PORTFOLIO", Op: ports.Eq, Type: ports.String, Values: []any{"P1"}}}},
			Writer:  arrow.NewSchema([]arrow.Field{{Name: "security", Type: arrow.BinaryTypes.String}, {Name: "market_value", Type: money, Nullable: true}}, nil),
			Columns: [][]string{{"SECURITY"}, {"MV", "CCY"}}, Capability: "test.money@v1", RequestID: "req-2"}
		_, rows := run(t, plan(t, r))
		if got, want := fmt.Sprint(rows), "[[S1 [1000.123456 USD]] [S2 [-200.000001 EUR]] [S1 [1010.000000 USD]]]"; got != want {
			t.Fatalf("%s, want %s", got, want)
		}
	})
	// §2.2.1.6, R053: a wall-clock time in a declared zone is converted to
	// UTC by the statement; one in no declared zone is refused, never
	// assumed to be UTC.
	t.Run("a wall-clock time is read in its declared zone", func(t *testing.T) {
		local := []ports.Column{{Name: "ID", Type: ports.String}, {Name: "LOCAL_AT", Type: ports.Timestamp, Zone: "Europe/London"}}
		expect(t, ports.Query{Object: Prices, Columns: local, OrderBy: []string{"ID"}},
			[]any{"A", ts("2026-07-01T06:00:00Z")}, []any{"B", ts("2026-01-15T07:00:00Z")}, []any{"C", nil}, []any{"D", ts("2026-07-01T07:30:00Z")})
		expect(t, ports.Query{Object: Prices, Columns: local, OrderBy: []string{"ID"},
			Where: []ports.Predicate{{Column: "LOCAL_AT", Op: ports.Gte, Type: ports.Timestamp, Zone: "Europe/London", Values: []any{"2026-07-01T06:30:00Z"}}}},
			[]any{"D", ts("2026-07-01T07:30:00Z")})
		p := plan(t, read(ports.Query{Object: Prices, Columns: []ports.Column{{Name: "ID", Type: ports.String}, {Name: "LOCAL_AT", Type: ports.Timestamp}}, OrderBy: []string{"ID"}}))
		res, err := s.Execute(ctx, p)
		if err == nil {
			defer res.Batches.Close()
			for err == nil {
				var b arrow.RecordBatch
				if b, err = res.Batches.Next(ctx); err == nil {
					b.Release()
				}
			}
		}
		if err == nil || errors.Is(err, io.EOF) || errors.Is(err, ports.ErrSourceUnavailable) {
			t.Fatalf("a wall-clock time in no declared zone was read: %v", err)
		}
	})
	// R073: a source is called from every request's goroutine at once.
	t.Run("reads at once", func(t *testing.T) {
		g, gctx := conc.WithContext(ctx)
		for range 8 {
			g.Go(func() error {
				p, err := s.Plan(gctx, read(ports.Query{Object: Holdings, Columns: keys, OrderBy: order}))
				if err != nil {
					return err
				}
				res, err := s.Execute(gctx, p)
				if err != nil {
					return err
				}
				defer res.Batches.Close()
				n := 0
				for {
					b, err := res.Batches.Next(gctx)
					if errors.Is(err, io.EOF) {
						break
					}
					if err != nil {
						return err
					}
					n += int(b.NumRows())
					b.Release()
				}
				if err := res.Batches.Close(); err != nil {
					return err
				}
				u, err := s.Settle(gctx, res.QueryID)
				if err != nil {
					return err
				}
				if n != 6 || u.Rows != 6 {
					return fmt.Errorf("%d rows streamed, %d settled", n, u.Rows)
				}
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			t.Fatal(err)
		}
	})
}

// rowsOf are a batch's rows: a string, int64, float64, bool, date
// (YYYY-MM-DD), time (UTC), decimal (its text at its type's scale) or a
// struct's members, for each field.
func rowsOf(b arrow.RecordBatch) [][]any {
	out := make([][]any, b.NumRows())
	for i := range out {
		out[i] = make([]any, b.NumCols())
		for k := range out[i] {
			out[i][k] = cell(b.Column(k), i)
		}
	}
	return out
}

func cell(a arrow.Array, i int) any {
	if a.IsNull(i) {
		return nil
	}
	switch x := a.(type) {
	case *array.String:
		return x.Value(i)
	case *array.Int64:
		return x.Value(i)
	case *array.Float64:
		return x.Value(i)
	case *array.Boolean:
		return x.Value(i)
	case *array.Date32:
		return x.Value(i).ToTime().Format(time.DateOnly)
	case *array.Timestamp:
		return time.UnixMicro(int64(x.Value(i))).UTC()
	case *array.Decimal128:
		return x.Value(i).ToString(x.DataType().(*arrow.Decimal128Type).Scale)
	case *array.Struct:
		members := make([]any, x.NumField())
		for j := range members {
			members[j] = cell(x.Field(j), i)
		}
		return members
	}
	return fmt.Sprintf("<%s>", a.DataType())
}
