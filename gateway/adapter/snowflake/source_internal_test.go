package snowflake

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/kit/conc"
)

// holdingsRead is a read of the journey's positions, in the writer
// contract: a code, a Money and a date.
func holdingsRead() ports.PlanRequest {
	money := arrow.StructOf(arrow.Field{Name: "amount", Type: &arrow.Decimal128Type{Precision: 38, Scale: 6}}, arrow.Field{Name: "currency", Type: arrow.BinaryTypes.String})
	return ports.PlanRequest{
		Query: ports.Query{Object: "HARBOUR.HOLDINGS.POSITIONS_V",
			Columns: []ports.Column{{Name: "PORTFOLIO_CODE", Type: ports.String}, {Name: "MARKET_VALUE", Type: ports.Decimal}, {Name: "CURRENCY", Type: ports.String}, {Name: "AS_OF_DATE", Type: ports.Date}},
			Where:   []ports.Predicate{{Column: "PORTFOLIO_CODE", Op: ports.In, Type: ports.String, Values: []any{"147HYC"}}}, OrderBy: []string{"PORTFOLIO_CODE"}},
		Writer: arrow.NewSchema([]arrow.Field{{Name: "portfolio", Type: arrow.BinaryTypes.String}, {Name: "market_value", Type: money, Nullable: true},
			{Name: "as_of", Type: arrow.FixedWidthTypes.Date32}}, nil),
		Columns:    [][]string{{"PORTFOLIO_CODE"}, {"MARKET_VALUE", "CURRENCY"}, {"AS_OF_DATE"}},
		Capability: "portfolio.holdings@v1", RequestID: "req-1",
	}
}

// engineFunc is an engine whose Execute is a function.
type engineFunc func(ctx context.Context, st *Statement) (*Answer, error)

func (f engineFunc) Name() string { return "test" }
func (f engineFunc) Execute(ctx context.Context, st *Statement) (*Answer, error) {
	return f(ctx, st)
}
func (f engineFunc) Record(context.Context, string) (*Record, error) {
	return &Record{State: Succeeded}, nil
}

// batchesFunc is an engine's answer whose Next is a function.
type batchesFunc func(ctx context.Context) (arrow.RecordBatch, error)

func (f batchesFunc) Schema() *arrow.Schema { return nil }
func (f batchesFunc) Next(ctx context.Context) (arrow.RecordBatch, error) {
	return f(ctx)
}
func (f batchesFunc) Close() error { return nil }

func eof(context.Context) (arrow.RecordBatch, error) { return nil, io.EOF }

func newSource(t *testing.T, e Engine, opt Options) *Source {
	t.Helper()
	if opt.Registered == nil {
		opt.Registered = [][]string{{"HARBOUR", "HOLDINGS"}}
	}
	s, err := NewSource(e, opt)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// R050 (§6.7 bulkhead: an in-flight limit and a bounded queue, twice its
// concurrency; a full bulkhead answers at once): with two places and a
// queue of four, two statements reach the engine, four wait, and a seventh
// is refused at once with ErrSourceUnavailable. Closing the answers lets
// the waiting ones through.
func TestR050SourceBulkhead(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		var reached atomic.Int32
		s := newSource(t, engineFunc(func(ctx context.Context, st *Statement) (*Answer, error) {
			reached.Add(1)
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return &Answer{QueryID: "q", Batches: batchesFunc(eof)}, nil
		}), Options{MaxInFlight: 2, MaxQueue: 4})
		ctx := context.Background()
		p, err := s.Plan(ctx, holdingsRead())
		if err != nil {
			t.Fatal(err)
		}
		var waits []func() error
		for range 6 {
			waits = append(waits, conc.Go(func() error {
				res, err := s.Execute(ctx, p)
				if err != nil {
					return err
				}
				return res.Batches.Close()
			}))
		}
		synctest.Wait()
		if n := reached.Load(); n != 2 {
			t.Fatalf("%d statements reached the engine, want 2", n)
		}
		start := time.Now()
		if _, err := s.Execute(ctx, p); !errors.Is(err, ports.ErrSourceUnavailable) || time.Since(start) != 0 {
			t.Fatalf("the seventh call: %v, after %v", err, time.Since(start))
		}
		close(release)
		for _, w := range waits {
			if err := w(); err != nil {
				t.Fatal(err)
			}
		}
		if n := reached.Load(); n != 6 {
			t.Fatalf("%d statements reached the engine, want 6", n)
		}
		// A waiting call leaves the queue when its context ends.
		done, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		held := make(chan struct{})
		s2 := newSource(t, engineFunc(func(ctx context.Context, st *Statement) (*Answer, error) {
			<-held
			return &Answer{QueryID: "q", Batches: batchesFunc(eof)}, nil
		}), Options{MaxInFlight: 1, MaxQueue: 1})
		first := conc.Go(func() error { _, err := s2.Execute(ctx, p); return err })
		synctest.Wait()
		if _, err := s2.Execute(done, p); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("a queued call past its deadline: %v", err)
		}
		close(held)
		if err := first(); err != nil {
			t.Fatal(err)
		}
	})
}

// R052, R120: an engine's failure the caller may retry (a broken
// connection, or what the deployment's hook recognises, such as a
// suspended warehouse) is ErrSourceUnavailable, from Execute and from a
// batch after the first; any other failure is the engine's own, wrapped.
func TestR052EngineFailuresAreClassified(t *testing.T) {
	suspended := errors.New("000606 (57P03): no active warehouse")
	permanent := errors.New("002003 (42S02): SQL compilation error")
	hook := func(err error) bool { return errors.Is(err, suspended) }
	ctx := context.Background()
	for _, c := range []struct {
		name      string
		err       error
		retryable bool
	}{
		{"a broken connection", fmt.Errorf("post: %w", driver.ErrBadConn), true},
		{"a suspended warehouse, by the hook", fmt.Errorf("query: %w", suspended), true},
		{"a refused connection", &netError{}, true},
		{"a compilation error", permanent, false},
	} {
		s := newSource(t, engineFunc(func(ctx context.Context, st *Statement) (*Answer, error) { return nil, c.err }), Options{Unavailable: hook})
		p, err := s.Plan(ctx, holdingsRead())
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.Execute(ctx, p)
		if errors.Is(err, ports.ErrSourceUnavailable) != c.retryable || !errors.Is(err, c.err) {
			t.Errorf("%s at execute: %v", c.name, err)
		}
		mid := newSource(t, engineFunc(func(ctx context.Context, st *Statement) (*Answer, error) {
			return &Answer{QueryID: "q", Batches: batchesFunc(func(context.Context) (arrow.RecordBatch, error) { return nil, c.err })}, nil
		}), Options{Unavailable: hook})
		res, err := mid.Execute(ctx, p)
		if err != nil {
			t.Fatal(err)
		}
		_, err = res.Batches.Next(ctx)
		res.Batches.Close()
		if errors.Is(err, ports.ErrSourceUnavailable) != c.retryable || !errors.Is(err, c.err) {
			t.Errorf("%s mid-stream: %v", c.name, err)
		}
	}
}

// netError is a refused connection, as net reports one.
type netError struct{}

func (*netError) Error() string   { return "dial tcp: connection refused" }
func (*netError) Timeout() bool   { return false }
func (*netError) Temporary() bool { return true }

// R051, CRITIQUE #13: the engine is given the tag beside the statement,
// never in it; the tag names the capability and the call, and holds
// nothing else.
func TestR051TheTagIsOutOfBand(t *testing.T) {
	var got *Statement
	s := newSource(t, engineFunc(func(ctx context.Context, st *Statement) (*Answer, error) {
		got = st
		return &Answer{QueryID: "q", Batches: batchesFunc(eof)}, nil
	}), Options{})
	ctx := context.Background()
	p, err := s.Plan(ctx, holdingsRead())
	if err != nil {
		t.Fatal(err)
	}
	if p.Tag != "atlas:portfolio.holdings@v1:req-1" {
		t.Fatalf("the tag %q", p.Tag)
	}
	res, err := s.Execute(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	res.Batches.Close()
	if got.Tag != p.Tag || got.SQL != p.Statement || strings.Contains(got.SQL, "req-1") || strings.Contains(got.SQL, "atlas:") {
		t.Fatalf("the engine was given %q with the tag %q", got.SQL, got.Tag)
	}
	for _, id := range []string{"req 1", "req'1", strings.Repeat("r", 300), "x\n"} {
		r := holdingsRead()
		r.RequestID = id
		if _, err := s.Plan(ctx, r); !errors.Is(err, ports.ErrPlanRefused) {
			t.Errorf("the request id %q: %v", id, err)
		}
	}
	// A plan whose tag was changed after it was made is not run.
	forged := *p
	forged.Tag = "atlas:x' OR 1=1"
	if _, err := s.Execute(ctx, &forged); !errors.Is(err, ports.ErrPlanRefused) {
		t.Fatalf("a forged tag: %v", err)
	}
}

// R100, §2.2.1.5: every field is selected under its contract name and cast
// to its contract type, a decimal at its contract's precision and scale; a
// Money is its two members. The deprecated Compile casts a decimal column
// that declares its type the same way, before writing it as text.
func TestR100TheStatementCastsToTheContract(t *testing.T) {
	s := newSource(t, engineFunc(nil), Options{})
	p, err := s.Plan(context.Background(), holdingsRead())
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{`SELECT CAST("atlas_t"."PORTFOLIO_CODE" AS VARCHAR) AS "portfolio"`, `CAST("atlas_t"."MARKET_VALUE" AS DECIMAL(38, 6)) AS "market_value.amount"`,
		`CAST("atlas_t"."CURRENCY" AS VARCHAR) AS "market_value.currency"`, `CAST("atlas_t"."AS_OF_DATE" AS DATE) AS "as_of"`} {
		if !strings.Contains(p.Statement, part) {
			t.Errorf("no %s in\n%s", part, p.Statement)
		}
	}
	q := ports.Query{Object: "HARBOUR.HOLDINGS.POSITIONS_V", Columns: []ports.Column{{Name: "MARKET_VALUE", Type: ports.Decimal, Precision: 38, Scale: 6}}}
	st, err := Compile(q, registered, "snowflake")
	if err != nil || !strings.Contains(st.SQL, `CAST(CAST("atlas_t"."MARKET_VALUE" AS DECIMAL(38, 6)) AS VARCHAR)`) {
		t.Fatalf("%v %v", st, err)
	}
	q.Columns[0].Precision, q.Columns[0].Scale = 39, 0
	if _, err := Compile(q, registered, "snowflake"); err == nil {
		t.Fatal("a Decimal(39,0) compiled")
	}
}

// R053, §2.2.1.6: a timestamp held as wall-clock time is converted to UTC
// in the statement from the zone its column declares, and the engine is
// told so; and a zone is a name, never SQL.
func TestR053WallClockTimes(t *testing.T) {
	ctx := context.Background()
	var got *Statement
	s := newSource(t, engineFunc(func(ctx context.Context, st *Statement) (*Answer, error) {
		got = st
		return &Answer{QueryID: "q", Batches: batchesFunc(eof)}, nil
	}), Options{Registered: [][]string{{"HARBOUR", "OPS"}}})
	r := ports.PlanRequest{Query: ports.Query{Object: "HARBOUR.OPS.LOADS_V", Columns: []ports.Column{{Name: "CHECKED_AT", Type: ports.Timestamp, Zone: "America/Los_Angeles"}, {Name: "LOADED_AT", Type: ports.Timestamp}}},
		Writer:  arrow.NewSchema([]arrow.Field{{Name: "checked_at", Type: &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}, Nullable: true}, {Name: "loaded_at", Type: &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}, Nullable: true}}, nil),
		Columns: [][]string{{"CHECKED_AT"}, {"LOADED_AT"}}, Capability: "data.status@v1", RequestID: "req-1"}
	p, err := s.Plan(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Statement, `CONVERT_TIMEZONE('America/Los_Angeles', 'UTC', "atlas_t"."CHECKED_AT") AS "checked_at", "atlas_t"."LOADED_AT" AS "loaded_at"`) {
		t.Fatalf("%s", p.Statement)
	}
	res, err := s.Execute(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	res.Batches.Close()
	if !got.Columns[0].WallUTC || got.Columns[1].WallUTC {
		t.Fatalf("the engine was told %+v", got.Columns)
	}
	r.Query.Columns[0].Zone = "UTC'), 'x"
	if _, err := s.Plan(ctx, r); !errors.Is(err, ports.ErrPlanRefused) {
		t.Fatalf("a zone that is SQL: %v", err)
	}
}

// R136, R074: a registered entry is two or three identifiers, upper-cased
// as Snowflake resolves them, and a malformed one stops the source at
// start; Serves says whether an object may be read, so a gateway can refuse
// at start a view whose object it may not.
func TestR136RegisteredNamesAreCheckedAtStart(t *testing.T) {
	for _, bad := range [][][]string{{{"HARBOUR"}}, {{"HARBOUR", "OPS", "LOADS_V", "X"}}, {{"HARBOUR", "OPS;"}}} {
		if _, err := NewSource(engineFunc(nil), Options{Registered: bad}); err == nil {
			t.Errorf("%v was accepted", bad)
		}
	}
	s, err := NewSource(engineFunc(nil), Options{Registered: [][]string{{"harbour", "ops"}, {"Harbour", "Ref", "Portfolios_V"}}})
	if err != nil {
		t.Fatal(err)
	}
	for obj, ok := range map[string]bool{"HARBOUR.OPS.LOADS_V": true, "harbour.ref.portfolios_v": true, "HARBOUR.REF.OTHER": false, "HARBOUR.SECRET.X": false} {
		if err := s.Serves(obj); (err == nil) != ok || !ok && !errors.Is(err, ports.ErrUnknownObject) {
			t.Errorf("Serves(%s): %v", obj, err)
		}
	}
}

// R119, INTEGRATIONS ("test that no driver call occurs on failure"): a
// read the compiler or the guard refuses is refused by Plan, and the engine
// is never asked.
func TestR119NoDriverCallOnRefusal(t *testing.T) {
	var calls atomic.Int32
	s := newSource(t, engineFunc(func(ctx context.Context, st *Statement) (*Answer, error) {
		calls.Add(1)
		return &Answer{QueryID: "q", Batches: batchesFunc(eof)}, nil
	}), Options{Registered: [][]string{{"HARBOUR", "HOLDINGS"}}})
	ctx := context.Background()
	writer := arrow.NewSchema([]arrow.Field{{Name: "x", Type: arrow.BinaryTypes.String, Nullable: true}}, nil)
	for name, q := range map[string]ports.Query{
		"an object not registered":  {Object: "HARBOUR.SECRET.SALARIES", Columns: []ports.Column{{Name: "X", Type: ports.String}}},
		"a name that is SQL":        {Object: "HARBOUR.HOLDINGS.T", Columns: []ports.Column{{Name: "x; DROP TABLE y", Type: ports.String}}},
		"a timestamp with no zone":  {Object: "HARBOUR.HOLDINGS.T", Columns: []ports.Column{{Name: "X", Type: ports.String}}, Where: []ports.Predicate{{Column: "T", Op: ports.Lte, Type: ports.Timestamp, Values: []any{"2026-10-29T23:00:00"}}}},
		"more values than it binds": {Object: "HARBOUR.HOLDINGS.T", Columns: []ports.Column{{Name: "X", Type: ports.String}}, Where: []ports.Predicate{{Column: "X", Op: ports.In, Type: ports.String, Values: make([]any, MaxBinds+1)}}},
	} {
		for i := range q.Where {
			for j := range q.Where[i].Values {
				if q.Where[i].Values[j] == nil {
					q.Where[i].Values[j] = "v"
				}
			}
		}
		if _, err := s.Plan(ctx, ports.PlanRequest{Query: q, Writer: writer, Columns: [][]string{{"X"}}, Capability: "portfolio.holdings@v1", RequestID: "req-1"}); err == nil {
			t.Errorf("%s: no refusal", name)
		}
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("%d refused reads reached the engine", n)
	}
}
