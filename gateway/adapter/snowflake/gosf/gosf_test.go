package gosf

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"
	sf "github.com/snowflakedb/gosnowflake"
	"go.uber.org/goleak"

	"atlas.local/gateway/adapter/snowflake"
	"atlas.local/gateway/ports"
)

// AGENTS.md: goleak where goroutines start; the driver and the pool start
// them, and every one must end with its test. The one exception is the
// driver's own: on its first connection gosnowflake detects, once for the
// process, which cloud it runs in, probing the cloud metadata servers with
// a 200 ms timeout (platform_detection.go), and the AWS SDK's instance
// metadata client it uses fetches its token on a timeout of its own. A
// deployment turns the probe off with SNOWFLAKE_DISABLE_PLATFORM_DETECTION=true.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		goleak.IgnoreAnyFunction("github.com/aws/aws-sdk-go-v2/feature/ec2/imds.(*tokenProvider).updateToken"),
		goleak.IgnoreAnyFunction("github.com/snowflakedb/gosnowflake.detectPlatforms.func1"),
		goleak.IgnoreAnyFunction("github.com/snowflakedb/gosnowflake.initPlatformDetection.func1.1"),
		goleak.IgnoreAnyFunction("github.com/snowflakedb/gosnowflake.initPlatformDetection.func1"))
}

var (
	utc     = &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}
	money6  = &arrow.Decimal128Type{Precision: 38, Scale: 6}
	allocOf = memory.NewGoAllocator()
)

// convertOne casts one array of Snowflake's to an output's type.
func convertOne(t *testing.T, c column, a arrow.Array) (arrow.Array, error) {
	t.Helper()
	defer a.Release()
	schema := arrow.NewSchema([]arrow.Field{{Name: c.out.Name, Type: c.out.Type, Nullable: true}}, nil)
	rec := array.NewRecordBatch(arrow.NewSchema([]arrow.Field{{Name: "x", Type: a.DataType(), Nullable: true}}, nil), []arrow.Array{a}, int64(a.Len()))
	defer rec.Release()
	out, err := convert(allocOf, schema, []column{c}, rec)
	if err != nil {
		return nil, err
	}
	col := out.Column(0)
	col.Retain()
	out.Release()
	return col, nil
}

func ints16(vals []int16, valid []bool) arrow.Array {
	b := array.NewInt16Builder(allocOf)
	defer b.Release()
	b.AppendValues(vals, valid)
	return b.NewArray()
}

func decimalsOf(t *arrow.Decimal128Type, vals ...string) arrow.Array {
	b := array.NewDecimal128Builder(allocOf, t)
	defer b.Release()
	for _, v := range vals {
		n, err := decimal128.FromString(v, t.Precision, t.Scale)
		if err != nil {
			panic(err)
		}
		b.Append(n)
	}
	return b.NewArray()
}

func strs(a arrow.Array) string {
	var out []string
	for i := range a.Len() {
		if a.IsNull(i) {
			out = append(out, "null")
			continue
		}
		switch x := a.(type) {
		case *array.Decimal128:
			out = append(out, x.Value(i).ToString(x.DataType().(*arrow.Decimal128Type).Scale))
		default:
			out = append(out, a.ValueStr(i))
		}
	}
	return strings.Join(out, " ")
}

// ATLAS-SNOWFLAKE-004, §2.2.1.6, gaps/6 §3.7: with higher precision a
// NUMBER arrives as Snowflake sent it, the smallest integer type holding
// the batch's unscaled values, or a Decimal128, its scale in the row type.
// Each is the writer's decimal exactly, never through a float; an integer
// is an Int64; and nothing that would need rounding or overflow is read.
func TestNumbersAreExact(t *testing.T) {
	cases := []struct {
		name string
		c    column
		a    arrow.Array
		want string
	}{
		{"a NUMBER(9,2) as Int16", column{out: snowflake.Output{Name: "mv", Type: money6}, typ: "FIXED", scale: 2}, ints16([]int16{12345, -5, 0}, []bool{true, true, false}), "123.450000 -0.050000 null"},
		{"a NUMBER(38,6) as Int64", column{out: snowflake.Output{Name: "mv", Type: money6}, typ: "FIXED", scale: 6}, func() arrow.Array {
			b := array.NewInt64Builder(allocOf)
			defer b.Release()
			b.AppendValues([]int64{1014852172500, 9007199254740993}, nil)
			return b.NewArray()
		}(), "1014852.172500 9007199254.740993"},
		{"a NUMBER(38,6) as its Decimal128", column{out: snowflake.Output{Name: "mv", Type: money6}, typ: "FIXED", scale: 6}, decimalsOf(money6, "12345678901234567890.123456"), "12345678901234567890.123456"},
		{"a NUMBER(38,2) as a Decimal128 of scale 2", column{out: snowflake.Output{Name: "mv", Type: money6}, typ: "FIXED", scale: 2}, decimalsOf(&arrow.Decimal128Type{Precision: 38, Scale: 2}, "-200.01"), "-200.010000"},
		{"a NUMBER(18,0) as Int8", column{out: snowflake.Output{Name: "n", Type: arrow.PrimitiveTypes.Int64}, typ: "FIXED"}, func() arrow.Array {
			b := array.NewInt8Builder(allocOf)
			defer b.Release()
			b.AppendValues([]int8{1, -2}, nil)
			return b.NewArray()
		}(), "1 -2"},
		{"a NUMBER(38,0) past 2^53 as a Decimal128", column{out: snowflake.Output{Name: "n", Type: arrow.PrimitiveTypes.Int64}, typ: "FIXED"}, decimalsOf(&arrow.Decimal128Type{Precision: 38, Scale: 0}, "9007199254740993"), "9007199254740993"},
	}
	for _, c := range cases {
		got, err := convertOne(t, c.c, c.a)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if s := strs(got); s != c.want || !arrow.TypeEqual(got.DataType(), c.c.out.Type) {
			t.Errorf("%s: %s as %s, want %s", c.name, s, got.DataType(), c.want)
		}
		got.Release()
	}
	for _, c := range []struct {
		name string
		c    column
		a    arrow.Array
	}{
		{"digits lost to a smaller scale", column{out: snowflake.Output{Name: "mv", Type: money6}, typ: "FIXED", scale: 8}, decimalsOf(&arrow.Decimal128Type{Precision: 38, Scale: 8}, "1.00000001")},
		{"past the writer's precision", column{out: snowflake.Output{Name: "mv", Type: &arrow.Decimal128Type{Precision: 4, Scale: 2}}, typ: "FIXED", scale: 2}, ints16([]int16{12345}, nil)},
		{"a NUMBER(38,0) past an int64", column{out: snowflake.Output{Name: "n", Type: arrow.PrimitiveTypes.Int64}, typ: "FIXED"}, decimalsOf(&arrow.Decimal128Type{Precision: 38, Scale: 0}, "92233720368547758070")},
		{"a float for a decimal", column{out: snowflake.Output{Name: "mv", Type: money6}, typ: "REAL"}, func() arrow.Array {
			b := array.NewFloat64Builder(allocOf)
			defer b.Release()
			b.Append(1.5)
			return b.NewArray()
		}()},
	} {
		if got, err := convertOne(t, c.c, c.a); err == nil {
			got.Release()
			t.Errorf("%s: converted", c.name)
		}
	}
}

// R053, §2.2.1.6, Q-SNOW-2/3: with microsecond timestamps, an instant
// (TIMESTAMP_TZ, TIMESTAMP_LTZ in any session zone) keeps its microseconds
// and is labelled UTC; a TIMESTAMP_NTZ is read only where the statement
// converted it to UTC wall-clock time, and refused otherwise.
func TestTimestamps(t *testing.T) {
	micros := time.Date(2026, 10, 31, 6, 0, 0, 123456000, time.UTC).UnixMicro()
	stamps := func(tz string) arrow.Array {
		b := array.NewTimestampBuilder(allocOf, &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: tz})
		defer b.Release()
		b.AppendValues([]arrow.Timestamp{arrow.Timestamp(micros)}, nil)
		b.AppendNull()
		return b.NewArray()
	}
	for _, c := range []struct {
		typ, tz string
		wall    bool
	}{{"TIMESTAMP_TZ", "", false}, {"TIMESTAMP_LTZ", "America/Los_Angeles", false}, {"TIMESTAMP_NTZ", "", true}} {
		got, err := convertOne(t, column{out: snowflake.Output{Name: "at", Type: utc, WallUTC: c.wall}, typ: c.typ}, stamps(c.tz))
		if err != nil {
			t.Fatalf("%s: %v", c.typ, err)
		}
		ts := got.(*array.Timestamp)
		if !arrow.TypeEqual(ts.DataType(), utc) || int64(ts.Value(0)) != micros || !ts.IsNull(1) {
			t.Errorf("%s: %s %v", c.typ, ts.DataType(), ts.Value(0))
		}
		got.Release()
	}
	if got, err := convertOne(t, column{out: snowflake.Output{Name: "at", Type: utc}, typ: "TIMESTAMP_NTZ"}, stamps("")); err == nil {
		got.Release()
		t.Fatal("a TIMESTAMP_NTZ in no declared zone was read")
	}
	b := array.NewTimestampBuilder(allocOf, &arrow.TimestampType{Unit: arrow.Nanosecond})
	b.Append(1)
	ns := b.NewArray()
	b.Release()
	if got, err := convertOne(t, column{out: snowflake.Output{Name: "at", Type: utc}, typ: "TIMESTAMP_TZ"}, ns); err == nil {
		got.Release()
		t.Fatal("nanoseconds were read as microseconds")
	}
}

// Text, floats, booleans and dates pass as they are, without a copy; text
// that is not UTF-8 is refused.
func TestOtherTypesPassThrough(t *testing.T) {
	sb := array.NewStringBuilder(allocOf)
	sb.AppendValues([]string{"147HYC", "Ünïcode"}, nil)
	s := sb.NewArray()
	sb.Release()
	got, err := convertOne(t, column{out: snowflake.Output{Name: "s", Type: arrow.BinaryTypes.String}, typ: "TEXT"}, s)
	if err != nil || strs(got) != "147HYC Ünïcode" {
		t.Fatalf("%v %v", got, err)
	}
	got.Release()
	sb = array.NewStringBuilder(allocOf)
	sb.Append(string([]byte{0xff, 0xfe}))
	bad := sb.NewArray()
	sb.Release()
	if got, err := convertOne(t, column{out: snowflake.Output{Name: "s", Type: arrow.BinaryTypes.String}, typ: "TEXT"}, bad); err == nil {
		got.Release()
		t.Fatal("text that is not UTF-8 was read")
	}
	db := array.NewDate32Builder(allocOf)
	db.Append(arrow.Date32FromTime(time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)))
	d := db.NewArray()
	db.Release()
	if got, err := convertOne(t, column{out: snowflake.Output{Name: "d", Type: arrow.FixedWidthTypes.Date32}, typ: "DATE"}, d); err != nil || strs(got) != "2026-10-30" {
		t.Fatalf("a date: %v %v", got, err)
	} else {
		got.Release()
	}
}

// fakeConnector stands where gosnowflake's connector does: its sessions
// answer with query, report status, and can be told their session ended.
type fakeConnector struct {
	mu     sync.Mutex
	conns  []*fakeConn
	query  func(c *fakeConn, ctx context.Context, sql string, args []driver.NamedValue) (driver.Rows, error)
	status func(ctx context.Context, id string) (*sf.SnowflakeQueryStatus, error)
}

func (f *fakeConnector) Connect(context.Context) (driver.Conn, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := &fakeConn{f: f, n: len(f.conns) + 1}
	f.conns = append(f.conns, c)
	return c, nil
}

func (f *fakeConnector) Driver() driver.Driver { return nil }

func (f *fakeConnector) expire() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.conns {
		c.ended = true
	}
}

type fakeConn struct {
	f      *fakeConnector
	n      int
	ended  bool
	closed bool
}

func (c *fakeConn) Prepare(string) (driver.Stmt, error) { return nil, errors.New("fake: no prepare") }
func (c *fakeConn) Begin() (driver.Tx, error)           { return nil, errors.New("fake: no transactions") }
func (c *fakeConn) Close() error {
	c.f.mu.Lock()
	defer c.f.mu.Unlock()
	c.closed = true
	return nil
}
func (c *fakeConn) QueryContext(ctx context.Context, sql string, args []driver.NamedValue) (driver.Rows, error) {
	c.f.mu.Lock()
	ended := c.ended
	c.f.mu.Unlock()
	if ended {
		return nil, &sf.SnowflakeError{Number: 390112, Message: "Your session has expired. Please login again."}
	}
	return c.f.query(c, ctx, sql, args)
}
func (c *fakeConn) GetQueryStatus(ctx context.Context, id string) (*sf.SnowflakeQueryStatus, error) {
	return c.f.status(ctx, id)
}
func (c *fakeConn) AddTelemetryData(context.Context, time.Time, map[string]string) error { return nil }

// fakeRows are an answer with no batches, describing its columns as
// Snowflake's row type does.
type fakeRows struct {
	id     string
	types  []string
	scales []int64
}

func (r *fakeRows) Columns() []string                       { return make([]string, len(r.types)) }
func (r *fakeRows) Close() error                            { return nil }
func (r *fakeRows) Next([]driver.Value) error               { return io.EOF }
func (r *fakeRows) ColumnTypeDatabaseTypeName(i int) string { return r.types[i] }
func (r *fakeRows) ColumnTypePrecisionScale(i int) (int64, int64, bool) {
	return 38, r.scales[i], true
}
func (r *fakeRows) GetQueryID() string                         { return r.id }
func (r *fakeRows) GetStatus() sf.QueryStatus                  { return sf.QueryStatusComplete }
func (r *fakeRows) GetArrowBatches() ([]*sf.ArrowBatch, error) { return nil, nil }
func (r *fakeRows) NextResultSet() error                       { return io.EOF }

func statement(outs ...snowflake.Output) *snowflake.Statement {
	return &snowflake.Statement{SQL: `SELECT "atlas_t"."X" AS "x" FROM "A"."B"."C" AS "atlas_t"`, Tag: "atlas:test@v1:req-1", Columns: outs}
}

func answering(types []string, scales []int64) func(*fakeConn, context.Context, string, []driver.NamedValue) (driver.Rows, error) {
	return func(c *fakeConn, ctx context.Context, sql string, args []driver.NamedValue) (driver.Rows, error) {
		return &fakeRows{id: fmt.Sprintf("01b0-%d", c.n), types: types, scales: scales}, nil
	}
}

// R054: a session Snowflake has ended is retired, not handed out again: the
// statement it refused runs once more on a new session, and the dead one is
// closed. The pool is bounded, keeps every session idle rather than logging
// it out, and retires sessions long before Snowflake's four hours.
func TestR054DeadSessionsAreRetired(t *testing.T) {
	f := &fakeConnector{query: answering([]string{"TEXT"}, []int64{0})}
	e, err := New(Config{Connector: f})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	ctx := context.Background()
	st := statement(snowflake.Output{Name: "x", Type: arrow.BinaryTypes.String})
	read := func() string {
		t.Helper()
		a, err := e.Execute(ctx, st)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := a.Batches.Next(ctx); !errors.Is(err, io.EOF) {
			t.Fatal(err)
		}
		if err := a.Batches.Close(); err != nil {
			t.Fatal(err)
		}
		return a.QueryID
	}
	if id := read(); id != "01b0-1" {
		t.Fatalf("the first statement ran as %s", id)
	}
	f.expire()
	if id := read(); id != "01b0-2" {
		t.Fatalf("after the session ended, the statement ran as %s", id)
	}
	if id := read(); id != "01b0-2" {
		t.Fatalf("the new session is reused: %s", id)
	}
	f.mu.Lock()
	if len(f.conns) != 2 || !f.conns[0].closed || f.conns[1].closed {
		t.Fatalf("sessions: %d opened, the first closed %v", len(f.conns), f.conns[0].closed)
	}
	f.mu.Unlock()
	cfg := e.Config()
	if cfg.MaxOpen != DefaultMaxOpen || cfg.MaxIdleTime != 30*time.Minute || cfg.MaxLifetime != time.Hour || e.Stats().MaxOpenConnections != DefaultMaxOpen {
		t.Fatalf("the pool: %+v, %+v", cfg, e.Stats())
	}
	if _, err := New(Config{}); err == nil {
		t.Fatal("an engine with no account")
	}
	// Every session ended, and none comes back: the failure is
	// unavailable, which a caller may retry, not internal.
	f.query = func(*fakeConn, context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		return nil, &sf.SnowflakeError{Number: sf.ErrSessionGone, Message: "Session no longer exists."}
	}
	if _, err := e.Execute(ctx, st); !errors.Is(err, ports.ErrSourceUnavailable) {
		t.Fatalf("no live session: %v", err)
	}
}

// R053: a TIMESTAMP_NTZ that the statement did not convert to UTC is
// refused before any of it is read; one it did is read.
func TestR053NoWallClockWithoutAZone(t *testing.T) {
	f := &fakeConnector{query: answering([]string{"TIMESTAMP_NTZ"}, []int64{0})}
	e, err := New(Config{Connector: f})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	ctx := context.Background()
	if _, err := e.Execute(ctx, statement(snowflake.Output{Name: "checked_at", Type: utc})); err == nil || !strings.Contains(err.Error(), "TIMESTAMP_NTZ") {
		t.Fatalf("a TIMESTAMP_NTZ in no declared zone: %v", err)
	}
	a, err := e.Execute(ctx, statement(snowflake.Output{Name: "checked_at", Type: utc, WallUTC: true}))
	if err != nil {
		t.Fatal(err)
	}
	a.Batches.Close()
	f.query = answering([]string{"REAL"}, []int64{0})
	if _, err := e.Execute(ctx, statement(snowflake.Output{Name: "mv", Type: money6})); err == nil {
		t.Fatal("a REAL read as a decimal")
	}
}

// R052, R120: Snowflake's own failures that a caller may retry are
// unavailable: a 5xx past the driver's retries, a failed request, a chunk
// that would not download, an ended session, a suspended or timed-out
// warehouse (SQLSTATE 08 or 57). A compilation error is not.
func TestR052SnowflakeFailuresAreClassified(t *testing.T) {
	for _, c := range []struct {
		err       error
		retryable bool
	}{
		{&sf.SnowflakeError{Number: sf.ErrFailedToPostQuery, SQLState: sf.SQLStateConnectionFailure, Message: "failed to POST. HTTP: 503"}, true},
		{&sf.SnowflakeError{Number: sf.ErrFailedToGetChunk, Message: "failed to get a chunk of result sets"}, true},
		{&sf.SnowflakeError{Number: sf.ErrCodeServiceUnavailable, SQLState: sf.SQLStateConnectionWasNotEstablished}, true},
		{&sf.SnowflakeError{Number: 606, SQLState: "57P03", Message: "No active warehouse selected in the current session."}, true},
		{&sf.SnowflakeError{Number: 625, SQLState: "57014", Message: "Statement reached its statement or warehouse timeout"}, true},
		{&sf.SnowflakeError{Number: 390112, Message: "Your session has expired."}, true},
		{fmt.Errorf("gosf: the query x: %w", errors.New("timeout after 5m0s and 7 attempts. HTTP Status: 503. Hanging?")), true},
		{fmt.Errorf("wrapped: %w", driver.ErrBadConn), true},
		{&sf.SnowflakeError{Number: 2003, SQLState: "42S02", Message: "SQL compilation error: Object does not exist"}, false},
		{errors.New("boom"), false},
	} {
		if got := Unavailable(c.err); got != c.retryable {
			t.Errorf("%v: unavailable %v", c.err, got)
		}
		if got := errors.Is(classify(c.err), ports.ErrSourceUnavailable); got != c.retryable {
			t.Errorf("%v classified retryable %v", c.err, got)
		}
	}
	f := &fakeConnector{query: func(*fakeConn, context.Context, string, []driver.NamedValue) (driver.Rows, error) {
		return nil, &sf.SnowflakeError{Number: 606, SQLState: "57P03", Message: "No active warehouse"}
	}}
	e, err := New(Config{Connector: f})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if _, err := e.Execute(context.Background(), statement(snowflake.Output{Name: "x", Type: arrow.BinaryTypes.String})); !errors.Is(err, ports.ErrSourceUnavailable) {
		t.Fatalf("a suspended warehouse: %v", err)
	}
}

// R09, R17: Settle reads Snowflake's own record of the query: its rows and
// scanned bytes when it succeeded, running while it runs, and cancelled
// only when Snowflake confirms it aborted.
func TestRecordIsSnowflakesOwn(t *testing.T) {
	f := &fakeConnector{status: func(ctx context.Context, id string) (*sf.SnowflakeQueryStatus, error) {
		switch id {
		case "done":
			return &sf.SnowflakeQueryStatus{ProducedRows: 4, ScanBytes: 2048, StartTime: 1761890400000, EndTime: 1761890401000}, nil
		case "running":
			return nil, &sf.SnowflakeError{Number: sf.ErrQueryIsRunning, Message: ": status from server: [RUNNING]"}
		case "aborted":
			return nil, &sf.SnowflakeError{Number: sf.ErrQueryReportedError, Message: "SQL execution canceled: status from server: [ABORTED]"}
		case "failed":
			return nil, &sf.SnowflakeError{Number: sf.ErrQueryReportedError, Message: "Division by zero: status from server: [FAILED_WITH_ERROR]"}
		}
		return nil, &sf.SnowflakeError{Number: sf.ErrQueryStatus, Message: "status query returned not-success or no status returned. Please retry"}
	}}
	e, err := New(Config{Connector: f})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	ctx := context.Background()
	for id, want := range map[string]snowflake.State{"done": snowflake.Succeeded, "running": snowflake.Running, "aborted": snowflake.Cancelled, "failed": snowflake.Failed} {
		r, err := e.Record(ctx, id)
		if err != nil || r.State != want {
			t.Errorf("%s: %+v %v", id, r, err)
		}
	}
	if r, _ := e.Record(ctx, "done"); r.Rows != 4 || r.Bytes != 2048 || !r.Ended.Equal(time.UnixMilli(1761890401000)) {
		t.Errorf("the record %+v", r)
	}
	if _, err := e.Record(ctx, "unknown"); !errors.Is(err, ports.ErrSourceUnavailable) {
		t.Errorf("no status yet: %v", err)
	}
}
