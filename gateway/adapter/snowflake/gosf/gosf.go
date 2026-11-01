// Package gosf is the Snowflake adapter's Engine over Snowflake's own Go
// driver, gosnowflake. It is a module of its own so that only a deployment
// that links Snowflake brings in the driver and its dependencies: the
// gateway module stays free of them.
//
// Every statement runs synchronously on a session from a bounded pool, so a
// cancelled or expired context aborts the query in Snowflake, not only the
// wait for it (gosnowflake sends no abort in async mode). Its tag goes as
// the statement's QUERY_TAG, out of band. Its answer is Arrow batches, with
// higher precision, so a NUMBER arrives as Snowflake sent it, a scaled
// integer or a Decimal128, and is converted exactly to the writer's type;
// timestamps arrive in microseconds. A TIMESTAMP_NTZ is read only where the
// statement converted it to UTC itself. Settle reads the query's own record
// (GetQueryStatus).
//
// Build a gateway that links it with CGO_ENABLED=0 -tags minicore_disabled,
// so the driver neither needs cgo nor writes and loads its native library
// (gaps/6 §3.3, R116).
package gosf

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"maps"
	"strings"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	sf "github.com/snowflakedb/gosnowflake"

	"github.com/jhgonline/fi-app-atlas/gateway/adapter/snowflake"
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
)

// The pool's defaults (R054): as many sessions as a Source runs statements
// at once by default, each retired after half an hour idle, well inside the
// four hours after which Snowflake ends a session that is not kept alive,
// and after an hour in any case.
const (
	DefaultMaxOpen     = 8
	DefaultMaxIdleTime = 30 * time.Minute
	DefaultMaxLifetime = time.Hour
)

// Config is how the engine reaches Snowflake and bounds what it holds.
type Config struct {
	// Driver is gosnowflake's configuration: the account, the user and how
	// it authenticates, the warehouse, database, schema and read-only role.
	// Its Params are the session's parameters; the engine sets TIMEZONE to
	// UTC unless they say otherwise.
	Driver sf.Config
	// MaxOpen bounds the sessions (default DefaultMaxOpen). Every session
	// is kept idle rather than logged out and in again.
	MaxOpen int
	// MaxIdleTime and MaxLifetime retire a session that has been idle, or
	// open, that long (defaults DefaultMaxIdleTime, DefaultMaxLifetime).
	MaxIdleTime, MaxLifetime time.Duration
	// Connector, when set, opens the sessions in place of gosnowflake's
	// connector over Driver: a deployment's own wrapper, or a test's fake.
	Connector driver.Connector
	// Allocator is the memory answers are built in (Go's when nil).
	Allocator memory.Allocator
}

// Engine is the Snowflake adapter's Engine over gosnowflake.
type Engine struct {
	db  *sql.DB
	cfg Config
	mem memory.Allocator
}

var _ snowflake.Engine = (*Engine)(nil)

// New is an Engine over cfg. It opens no session until the first
// statement.
func New(cfg Config) (*Engine, error) {
	if cfg.MaxOpen < 0 || cfg.MaxIdleTime < 0 || cfg.MaxLifetime < 0 {
		return nil, errors.New("gosf: the pool's bounds are not negative")
	}
	if cfg.MaxOpen == 0 {
		cfg.MaxOpen = DefaultMaxOpen
	}
	if cfg.MaxIdleTime == 0 {
		cfg.MaxIdleTime = DefaultMaxIdleTime
	}
	if cfg.MaxLifetime == 0 {
		cfg.MaxLifetime = DefaultMaxLifetime
	}
	connector := cfg.Connector
	if connector == nil {
		if cfg.Driver.Account == "" {
			return nil, errors.New("gosf: the driver's configuration names no account")
		}
		d := cfg.Driver
		d.Params = maps.Clone(d.Params)
		if d.Params == nil {
			d.Params = map[string]*string{}
		}
		if _, set := d.Params["timezone"]; !set {
			utc := "UTC"
			d.Params["timezone"] = &utc // a TIMESTAMP_LTZ in UTC, whatever the account's default
		}
		connector = sf.NewConnector(sf.SnowflakeDriver{}, d)
	}
	db := sql.OpenDB(&sessions{inner: connector})
	db.SetMaxOpenConns(cfg.MaxOpen)
	db.SetMaxIdleConns(cfg.MaxOpen)
	db.SetConnMaxIdleTime(cfg.MaxIdleTime)
	db.SetConnMaxLifetime(cfg.MaxLifetime)
	mem := cfg.Allocator
	if mem == nil {
		mem = memory.DefaultAllocator
	}
	return &Engine{db: db, cfg: cfg, mem: mem}, nil
}

// Config is the engine's configuration, with its defaults applied.
func (e *Engine) Config() Config { return e.cfg }

// Stats are the pool's.
func (e *Engine) Stats() sql.DBStats { return e.db.Stats() }

// Ping reaches the warehouse once, opening a session if none is idle. A
// failure is classified as Execute's are: one the caller may retry wraps
// ports.ErrSourceUnavailable.
func (e *Engine) Ping(ctx context.Context) error { return classify(e.db.PingContext(ctx)) }

// ConfigFromDSN is a Config whose driver settings come from a gosnowflake
// DSN, the form a deployment keeps in its secret store: account, user and
// authenticator, warehouse, database, schema and role. The DSN itself is
// never quoted in the error, since it may hold a credential.
func ConfigFromDSN(dsn string) (Config, error) {
	c, err := sf.ParseDSN(dsn)
	if err != nil {
		return Config{}, errors.New("gosf: the DSN does not parse as a Snowflake DSN")
	}
	return Config{Driver: *c}, nil
}

// Close logs every session out.
func (e *Engine) Close() error { return e.db.Close() }

// Name is "snowflake".
func (e *Engine) Name() string { return "snowflake" }

// Execute runs st on a session of its own, held until its answer is
// closed. A session Snowflake has ended is retired and the statement run
// once more on a new one: Snowflake refused it before running it.
func (e *Engine) Execute(ctx context.Context, st *snowflake.Statement) (*snowflake.Answer, error) {
	var err error
	for range 2 {
		var a *snowflake.Answer
		if a, err = e.execute(ctx, st); err == nil {
			return a, nil
		}
		if ctx.Err() != nil || !errors.Is(err, errDeadSession) {
			break
		}
	}
	return nil, classify(err)
}

// errDeadSession marks a statement refused because its session has ended.
var errDeadSession = errors.New("gosf: the session has ended")

func (e *Engine) execute(ctx context.Context, st *snowflake.Statement) (*snowflake.Answer, error) {
	conn, err := e.db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	ids := make(chan string, 1)
	qctx := sf.WithQueryIDChan(options(ctx, st.Tag), ids)
	args := make([]driver.NamedValue, len(st.Args))
	for i, a := range st.Args {
		args[i] = driver.NamedValue{Ordinal: i + 1, Value: a}
	}
	var rows driver.Rows
	err = conn.Raw(func(dc any) error {
		s, ok := dc.(*session)
		if !ok {
			return fmt.Errorf("gosf: a session of %T", dc)
		}
		q, ok := s.inner.(driver.QueryerContext)
		if !ok {
			return fmt.Errorf("gosf: the driver's session %T runs no queries", s.inner)
		}
		r, err := q.QueryContext(qctx, st.SQL, args)
		if err != nil {
			if dead(err) {
				s.bad.Store(true) // database/sql discards it, and the pool opens another
				return fmt.Errorf("%w: %w: %w", errDeadSession, driver.ErrBadConn, err)
			}
			return err
		}
		rows = r
		return nil
	})
	if err != nil {
		return nil, errors.Join(withID(err, ids), conn.Close())
	}
	sr, ok := rows.(sf.SnowflakeRows)
	if !ok {
		return nil, errors.Join(fmt.Errorf("gosf: the driver answered %T, not Snowflake's rows", rows), rows.Close(), conn.Close())
	}
	cols, err := describe(rows, st.Columns)
	if err != nil {
		return nil, errors.Join(err, rows.Close(), conn.Close())
	}
	batches, err := sr.GetArrowBatches()
	if err != nil {
		return nil, errors.Join(fmt.Errorf("gosf: the query %s: %w", sr.GetQueryID(), err), rows.Close(), conn.Close())
	}
	fields := make([]arrow.Field, len(st.Columns))
	for i, o := range st.Columns {
		fields[i] = arrow.Field{Name: o.Name, Type: o.Type, Nullable: true}
	}
	return &snowflake.Answer{QueryID: sr.GetQueryID(), Batches: &stream{conn: conn, rows: rows, batches: batches, cols: cols, mem: e.mem,
		schema: arrow.NewSchema(fields, nil), queryID: sr.GetQueryID()}}, nil
}

// options are the context a statement runs under: synchronous (never
// WithAsyncMode, so a cancel aborts it), its tag as QUERY_TAG, its answer
// as Arrow batches with every NUMBER as Snowflake sent it, timestamps in
// microseconds.
func options(ctx context.Context, tag string) context.Context {
	ctx = sf.WithQueryTag(ctx, tag)
	ctx = sf.WithArrowBatches(ctx)
	ctx = sf.WithHigherPrecision(ctx)
	return sf.WithArrowBatchesTimestampOption(ctx, sf.UseMicrosecondTimestamp)
}

// withID names the query a failure belongs to, when Snowflake had given
// its id before it failed, so the failure can be matched to its record.
func withID(err error, ids <-chan string) error {
	select {
	case id := <-ids:
		if id != "" {
			return fmt.Errorf("gosf: the query %s: %w", id, err)
		}
	default:
	}
	return err
}

// column is one column of an answer: what the statement asked for, and
// what Snowflake says it sent.
type column struct {
	out   snowflake.Output
	typ   string // Snowflake's type: FIXED, REAL, TEXT, BOOLEAN, DATE, TIMESTAMP_TZ, ...
	scale int64  // a FIXED's scale
}

// describe checks the answer's columns against the statement's: the
// types Snowflake gives each output, and above all that a TIMESTAMP_NTZ
// arrives only where the statement converted it to UTC (R053).
func describe(rows driver.Rows, outs []snowflake.Output) ([]column, error) {
	if n := len(rows.Columns()); n != len(outs) {
		return nil, fmt.Errorf("gosf: the statement answered %d columns for %d", n, len(outs))
	}
	names, _ := rows.(driver.RowsColumnTypeDatabaseTypeName)
	scales, _ := rows.(driver.RowsColumnTypePrecisionScale)
	if names == nil || scales == nil {
		return nil, fmt.Errorf("gosf: the driver's rows %T do not describe their columns", rows)
	}
	cols := make([]column, len(outs))
	for i, o := range outs {
		c := column{out: o, typ: strings.ToUpper(names.ColumnTypeDatabaseTypeName(i))}
		if c.typ == "FIXED" {
			_, c.scale, _ = scales.ColumnTypePrecisionScale(i)
		}
		want := map[arrow.Type][]string{
			arrow.STRING: {"TEXT"}, arrow.INT64: {"FIXED"}, arrow.FLOAT64: {"REAL"}, arrow.DECIMAL128: {"FIXED"},
			arrow.BOOL: {"BOOLEAN"}, arrow.DATE32: {"DATE"}, arrow.TIMESTAMP: {"TIMESTAMP_TZ", "TIMESTAMP_LTZ", "TIMESTAMP_NTZ"},
		}[o.Type.ID()]
		switch {
		case !contains(want, c.typ):
			return nil, fmt.Errorf("gosf: %s arrived as a %s, which is not a %s", o.Name, c.typ, o.Type)
		case c.typ == "TIMESTAMP_NTZ" && !o.WallUTC:
			return nil, fmt.Errorf("gosf: %s is a TIMESTAMP_NTZ, wall-clock time in no declared zone: declare the column's zone (§2.2.1.6)", o.Name)
		case o.Type.ID() == arrow.INT64 && c.scale != 0:
			return nil, fmt.Errorf("gosf: %s arrived as a NUMBER of scale %d, not an integer", o.Name, c.scale)
		}
		cols[i] = c
	}
	return cols, nil
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if x == y {
			return true
		}
	}
	return false
}

// stream is a query's Arrow batches, fetched one at a time as they are
// read, so the client's reading is the backpressure and memory stays flat:
// each record is released once converted, and each batch dropped once
// fetched.
type stream struct {
	conn    *sql.Conn
	rows    driver.Rows
	batches []*sf.ArrowBatch
	next    int
	recs    []arrow.RecordBatch
	cols    []column
	mem     memory.Allocator
	schema  *arrow.Schema
	queryID string
	closed  bool
}

func (s *stream) Schema() *arrow.Schema { return s.schema }

func (s *stream) Next(ctx context.Context) (arrow.RecordBatch, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(s.recs) > 0 {
			rec := s.recs[0]
			s.recs = s.recs[1:]
			out, err := convert(s.mem, s.schema, s.cols, rec)
			rec.Release()
			if err != nil {
				return nil, fmt.Errorf("gosf: the query %s: %w", s.queryID, err)
			}
			if out.NumRows() == 0 {
				out.Release()
				continue
			}
			return out, nil
		}
		if s.next >= len(s.batches) {
			return nil, io.EOF
		}
		b := s.batches[s.next]
		s.batches[s.next] = nil
		s.next++
		recs, err := b.WithContext(ctx).Fetch()
		if err != nil {
			return nil, classify(fmt.Errorf("gosf: the query %s: %w", s.queryID, err))
		}
		if recs != nil {
			s.recs = *recs
		}
	}
}

// Close releases what is left of the answer and gives the session back.
func (s *stream) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	for _, r := range s.recs {
		r.Release()
	}
	s.recs, s.batches = nil, nil
	return errors.Join(s.rows.Close(), s.conn.Close())
}

// Record is Snowflake's own record of a query (GetQueryStatus): the rows
// it produced and the bytes it scanned. Snowflake reports no cost there.
func (e *Engine) Record(ctx context.Context, queryID string) (*snowflake.Record, error) {
	conn, err := e.db.Conn(ctx)
	if err != nil {
		return nil, classify(err)
	}
	defer conn.Close()
	var st *sf.SnowflakeQueryStatus
	err = conn.Raw(func(dc any) error {
		s, ok := dc.(*session)
		if !ok {
			return fmt.Errorf("gosf: a session of %T", dc)
		}
		c, ok := s.inner.(sf.SnowflakeConnection)
		if !ok {
			return fmt.Errorf("gosf: the driver's session %T reports no query status", s.inner)
		}
		st, err = c.GetQueryStatus(ctx, queryID)
		return err
	})
	var se *sf.SnowflakeError
	switch {
	case err == nil:
		return &snowflake.Record{QueryID: queryID, State: snowflake.Succeeded, Rows: st.ProducedRows, Bytes: st.ScanBytes,
			Started: time.UnixMilli(st.StartTime).UTC(), Ended: time.UnixMilli(st.EndTime).UTC()}, nil
	case errors.As(err, &se) && se.Number == sf.ErrQueryIsRunning:
		return &snowflake.Record{QueryID: queryID, State: snowflake.Running}, nil
	case errors.As(err, &se) && se.Number == sf.ErrQueryReportedError:
		state := snowflake.Failed
		if strings.Contains(se.Error(), "[ABORTED]") {
			state = snowflake.Cancelled // Snowflake confirms the query stopped (R17)
		}
		return &snowflake.Record{QueryID: queryID, State: state}, nil
	case errors.As(err, &se) && se.Number == sf.ErrQueryStatus:
		// No status yet, or none Snowflake will give now: settle later.
		return nil, fmt.Errorf("%w: gosf: the query %s: %w", ports.ErrSourceUnavailable, queryID, err)
	}
	return nil, classify(err)
}

// sessions open the pool's sessions through the driver's connector, each
// wrapped so a session Snowflake has ended is never handed out again.
type sessions struct{ inner driver.Connector }

func (c *sessions) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.inner.Connect(ctx)
	if err != nil {
		return nil, classify(err)
	}
	return &session{inner: conn}, nil
}

func (c *sessions) Driver() driver.Driver { return c.inner.Driver() }

// session is one of the pool's: database/sql retires it once it is bad
// (R054). gosnowflake implements neither Validator nor SessionResetter,
// and reports an ended session as its own error, not driver.ErrBadConn, so
// without this a dead session goes back on top of the idle list and fails
// every call after it until the gateway restarts.
type session struct {
	inner driver.Conn
	bad   atomic.Bool
}

func (s *session) Prepare(query string) (driver.Stmt, error) { return s.inner.Prepare(query) }
func (s *session) Close() error                              { return s.inner.Close() }
func (s *session) Begin() (driver.Tx, error) {
	return nil, errors.New("gosf: a read-only engine opens no transactions")
}

// IsValid is false for a session Snowflake has ended, which database/sql
// then closes instead of keeping (driver.Validator).
func (s *session) IsValid() bool { return !s.bad.Load() }

// ResetSession refuses a session Snowflake has ended, so database/sql
// opens another (driver.SessionResetter).
func (s *session) ResetSession(context.Context) error {
	if s.bad.Load() {
		return driver.ErrBadConn
	}
	return nil
}

// dead reports a failure that means the session is over: gone, expired,
// or its renewal refused.
func dead(err error) bool {
	var se *sf.SnowflakeError
	if !errors.As(err, &se) {
		return errors.Is(err, driver.ErrBadConn)
	}
	switch se.Number {
	case sf.ErrSessionGone, 390112, 390114, sf.ErrFailedToRenewSession:
		return true
	}
	return false
}

// classify marks a failure the caller may retry as ports.ErrSourceUnavailable.
func classify(err error) error {
	if err == nil || errors.Is(err, ports.ErrSourceUnavailable) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if Unavailable(err) {
		return fmt.Errorf("%w: %w", ports.ErrSourceUnavailable, err)
	}
	return err
}

// Unavailable reports a gosnowflake failure the caller may retry (R052):
// Snowflake unreachable or answering 5xx until the driver's own retries ran
// out, a session that ended, a chunk that would not download, a warehouse
// that is suspended, queued past its timeout or stopped by a resource
// monitor (SQLSTATE class 08, connection, or 57, operator intervention).
// A deployment may give it to a deprecated snowflake.Warehouse as
// Options.Unavailable.
func Unavailable(err error) bool {
	if dead(err) {
		return true
	}
	var se *sf.SnowflakeError
	if errors.As(err, &se) {
		switch se.Number {
		case sf.ErrCodeServiceUnavailable, sf.ErrCodeFailedToConnect, sf.ErrFailedToPostQuery, sf.ErrFailedToGetChunk,
			606, 625, 630: // no active warehouse; a statement or warehouse timeout
			return true
		}
		return strings.HasPrefix(se.SQLState, "08") || strings.HasPrefix(se.SQLState, "57")
	}
	// The driver's retries of 5xx and 429 ran out (retry.go).
	return strings.Contains(err.Error(), "timeout after ") && strings.Contains(err.Error(), "Hanging?")
}
