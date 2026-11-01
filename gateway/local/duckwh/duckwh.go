// Package duckwh is the development gateway's warehouse: DuckDB, in
// process, holding the fixture's tables under their Snowflake names, and
// reached through the Snowflake adapter. Engine is the adapter's Engine
// over DuckDB: it transpiles each Snowflake statement the Source planned to
// DuckDB with sqlglot, binds its values, answers Arrow in the statement's
// columns with exact decimals, and keeps the record Settle reads. It is how
// the local gateway runs the same planned SQL as production without Docker
// or an account (PLAN.md P2-6: only the warehouse is fake).
package duckwh

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/duckdb/duckdb-go/v2"

	"atlas.local/gateway/adapter/snowflake"
	"atlas.local/gateway/fakes"
	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/protocol/grammar"
)

// The engine's bounds: rows in one answer batch, and query records kept.
const (
	BatchRows  = 1024
	MaxRecords = 4096
)

// open is an in-memory DuckDB whose every session is in UTC, so a cast
// between wall-clock time and instants never takes the machine's zone.
func open() (*sql.DB, error) {
	c, err := duckdb.NewConnector("", func(execer driver.ExecerContext) error {
		_, err := execer.ExecContext(context.Background(), "SET TimeZone = 'UTC'", nil)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("duckwh: %w", err)
	}
	return sql.OpenDB(c), nil
}

// registered are the tables' objects, as the guard compares them.
func registered(tables []fakes.Table) [][]string {
	var out [][]string
	for _, t := range tables {
		out = append(out, strings.Split(strings.ToUpper(t.Name), "."))
	}
	return out
}

// OpenSource is the Snowflake Source over an Engine holding tables, every
// table a registered object, with the Source's other options as opt gives
// them. Closing the engine closes the database.
func OpenSource(ctx context.Context, tables []fakes.Table, opt snowflake.Options) (*snowflake.Source, *Engine, error) {
	e, err := OpenEngine(ctx, tables)
	if err != nil {
		return nil, nil, err
	}
	opt.Registered = append(opt.Registered, registered(tables)...)
	s, err := snowflake.NewSource(e, opt)
	if err != nil {
		return nil, nil, errors.Join(err, e.Close())
	}
	return s, e, nil
}

// Engine is the Snowflake adapter's Engine over DuckDB.
type Engine struct {
	db  *sql.DB
	mem memory.Allocator

	mu      sync.Mutex
	records map[string]*snowflake.Record
	order   []string // the records' ids, oldest first, at most MaxRecords
	next    int
	closed  bool
}

var _ snowflake.Engine = (*Engine)(nil)

// OpenEngine is an in-memory DuckDB holding tables, and the Engine over it.
func OpenEngine(ctx context.Context, tables []fakes.Table) (*Engine, error) {
	db, err := open()
	if err != nil {
		return nil, err
	}
	if err := Load(ctx, db, tables); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return &Engine{db: db, mem: memory.DefaultAllocator, records: map[string]*snowflake.Record{}}, nil
}

// DB is the engine's database, for a test that changes its settings.
func (e *Engine) DB() *sql.DB { return e.db }

// Close closes the database; a later statement is refused as unavailable.
func (e *Engine) Close() error {
	e.mu.Lock()
	e.closed = true
	e.mu.Unlock()
	return e.db.Close()
}

// Name is "duckdb".
func (e *Engine) Name() string { return "duckdb" }

// Execute transpiles st to DuckDB, runs it with its values bound, and
// answers its rows as Arrow batches of st's columns. The tag is kept in the
// query's record, never in the statement.
func (e *Engine) Execute(ctx context.Context, st *snowflake.Statement) (*snowflake.Answer, error) {
	text, err := snowflake.Transpile(st.SQL, "duckdb")
	if err != nil {
		return nil, err
	}
	rec, err := e.begin(st.Tag)
	if err != nil {
		return nil, err
	}
	rs, err := e.db.QueryContext(ctx, text, st.Args...)
	if err != nil {
		e.end(rec, ctx, err)
		return nil, e.classify(ctx, err)
	}
	types, err := rs.ColumnTypes()
	if err == nil && len(types) != len(st.Columns) {
		err = fmt.Errorf("duckwh: the statement answered %d columns for %d", len(types), len(st.Columns))
	}
	for i := 0; err == nil && i < len(types); i++ {
		o := st.Columns[i]
		if _, isTime := o.Type.(*arrow.TimestampType); isTime && !o.WallUTC && snowflake.WallClock(types[i].DatabaseTypeName()) {
			err = fmt.Errorf("duckwh: %s is a %s, wall-clock time in no declared zone: declare the column's zone (§2.2.1.6)", o.Name, types[i].DatabaseTypeName())
		}
	}
	if err != nil {
		e.end(rec, ctx, err)
		return nil, errors.Join(err, rs.Close())
	}
	fields := make([]arrow.Field, len(st.Columns))
	for i, o := range st.Columns {
		fields[i] = arrow.Field{Name: o.Name, Type: o.Type, Nullable: true}
	}
	return &snowflake.Answer{QueryID: rec.QueryID, Batches: &batches{e: e, rec: rec, ctx: ctx, rs: rs, cols: st.Columns,
		schema: arrow.NewSchema(fields, nil), dest: make([]any, len(st.Columns))}}, nil
}

func (e *Engine) begin(tag string) (*snowflake.Record, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, fmt.Errorf("%w: the development warehouse is closed", ports.ErrSourceUnavailable)
	}
	e.next++
	rec := &snowflake.Record{QueryID: "duckdb-" + strconv.Itoa(e.next), Tag: tag, State: snowflake.Running, Started: time.Now().UTC()}
	e.records[rec.QueryID] = rec
	e.order = append(e.order, rec.QueryID)
	if len(e.order) > MaxRecords {
		delete(e.records, e.order[0])
		e.order = e.order[1:]
	}
	return rec, nil
}

// end closes a query's record: cancelled when its context ended, failed on
// any other error, succeeded when it answered every row.
func (e *Engine) end(rec *snowflake.Record, ctx context.Context, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if rec.State != snowflake.Running {
		return
	}
	switch {
	case ctx.Err() != nil:
		rec.State = snowflake.Cancelled
	case err != nil:
		rec.State = snowflake.Failed
	default:
		rec.State = snowflake.Succeeded
	}
	rec.Ended = time.Now().UTC()
}

func (e *Engine) count(rec *snowflake.Record, b arrow.RecordBatch) {
	var bytes int64
	for _, c := range b.Columns() {
		bytes += size(c.Data())
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	rec.Rows += b.NumRows()
	rec.Bytes += bytes
}

func size(d arrow.ArrayData) int64 {
	var n int64
	for _, b := range d.Buffers() {
		if b != nil {
			n += int64(b.Len())
		}
	}
	for _, c := range d.Children() {
		n += size(c)
	}
	return n
}

// Record is a query's record: its rows, and its answer's size as its bytes.
func (e *Engine) Record(ctx context.Context, queryID string) (*snowflake.Record, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	rec, ok := e.records[queryID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ports.ErrUnknownQuery, queryID)
	}
	out := *rec
	return &out, nil
}

// classify marks a failure the caller may retry: a database that is closed
// is unavailable, as a warehouse that has gone away is.
func (e *Engine) classify(ctx context.Context, err error) error {
	e.mu.Lock()
	closed := e.closed
	e.mu.Unlock()
	if closed && ctx.Err() == nil && !errors.Is(err, ports.ErrSourceUnavailable) {
		return fmt.Errorf("%w: %w", ports.ErrSourceUnavailable, err)
	}
	return err
}

// batches are a query's rows as Arrow batches of its statement's columns.
type batches struct {
	e      *Engine
	rec    *snowflake.Record
	ctx    context.Context
	rs     *sql.Rows
	cols   []snowflake.Output
	schema *arrow.Schema
	dest   []any
	done   bool
}

func (b *batches) Schema() *arrow.Schema { return b.schema }

func (b *batches) Next(ctx context.Context) (arrow.RecordBatch, error) {
	if b.done {
		return nil, io.EOF
	}
	rb := array.NewRecordBuilder(b.e.mem, b.schema)
	defer rb.Release()
	ptrs := make([]any, len(b.dest))
	for i := range b.dest {
		ptrs[i] = &b.dest[i]
	}
	n, finished := 0, false
	for n < BatchRows {
		for _, c := range []context.Context{ctx, b.ctx} {
			if err := c.Err(); err != nil {
				return nil, b.fail(c, err)
			}
		}
		if !b.rs.Next() {
			err := b.rs.Err()
			if err == nil {
				err = b.ctx.Err()
			}
			if err != nil {
				return nil, b.fail(b.ctx, err)
			}
			finished = true
			break
		}
		if err := b.rs.Scan(ptrs...); err != nil {
			return nil, b.fail(b.ctx, err)
		}
		for i, o := range b.cols {
			if err := appendValue(rb.Field(i), o, b.dest[i]); err != nil {
				return nil, b.fail(b.ctx, fmt.Errorf("duckwh: %s: %w", o.Name, err))
			}
		}
		n++
	}
	var out arrow.RecordBatch
	if n > 0 {
		out = rb.NewRecordBatch()
		b.e.count(b.rec, out)
	}
	if finished {
		b.done = true
		b.e.end(b.rec, b.ctx, nil) // every row answered and counted
	}
	if out == nil {
		return nil, io.EOF
	}
	return out, nil
}

func (b *batches) fail(ctx context.Context, err error) error {
	b.done = true
	b.e.end(b.rec, ctx, err)
	return b.e.classify(ctx, err)
}

// Close closes the rows; an answer closed before its last row is a query
// stopped, and its record says cancelled.
func (b *batches) Close() error {
	if !b.done {
		b.done = true
		b.e.mu.Lock()
		if b.rec.State == snowflake.Running {
			b.rec.State, b.rec.Ended = snowflake.Cancelled, time.Now().UTC()
		}
		b.e.mu.Unlock()
	}
	return b.rs.Close()
}

// appendValue writes a DuckDB value as its column's type, exactly: a
// DECIMAL keeps every digit, an integer is a DECIMAL(18, 0) as the statement
// casts it, a timestamp is an instant to the microsecond.
func appendValue(bb array.Builder, o snowflake.Output, v any) error {
	if v == nil {
		bb.AppendNull()
		return nil
	}
	bad := fmt.Errorf("a %T is not a %s", v, o.Type)
	switch b := bb.(type) {
	case *array.StringBuilder:
		s, ok := v.(string)
		if !ok {
			return bad
		}
		b.Append(s)
	case *array.Int64Builder:
		switch x := v.(type) {
		case int64:
			b.Append(x)
		case int32:
			b.Append(int64(x))
		case duckdb.Decimal:
			if x.Scale != 0 || x.Value == nil || !x.Value.IsInt64() {
				return fmt.Errorf("%s is not an int64", x.String())
			}
			b.Append(x.Value.Int64())
		default:
			return bad
		}
	case *array.Float64Builder:
		f, ok := v.(float64)
		if !ok {
			return bad
		}
		b.Append(f)
	case *array.Decimal128Builder:
		t := o.Type.(*arrow.Decimal128Type)
		var unscaled *big.Int
		var scale int32
		switch x := v.(type) {
		case duckdb.Decimal:
			unscaled, scale = x.Value, int32(x.Scale)
		case int64:
			unscaled = big.NewInt(x)
		default:
			return bad
		}
		n, err := rescale(unscaled, scale, t)
		if err != nil {
			return err
		}
		b.Append(n)
	case *array.BooleanBuilder:
		x, ok := v.(bool)
		if !ok {
			return bad
		}
		b.Append(x)
	case *array.Date32Builder:
		x, ok := v.(time.Time)
		if !ok {
			return bad
		}
		b.Append(arrow.Date32FromTime(time.Date(x.Year(), x.Month(), x.Day(), 0, 0, 0, 0, time.UTC)))
	case *array.TimestampBuilder:
		x, ok := v.(time.Time)
		if !ok {
			return bad
		}
		// A TIMESTAMPTZ is an instant; a TIMESTAMP is here only as the
		// statement's UTC wall-clock time, which DuckDB answers in UTC.
		b.Append(arrow.Timestamp(x.UnixMicro()))
	default:
		return fmt.Errorf("no DuckDB value of %s", o.Type)
	}
	return nil
}

// rescale is an unscaled value at its scale, written at the type's scale
// without rounding, and refused when it does not fit.
func rescale(unscaled *big.Int, scale int32, t *arrow.Decimal128Type) (decimal128.Num, error) {
	if unscaled == nil {
		return decimal128.Num{}, errors.New("a decimal with no value")
	}
	v := new(big.Int).Set(unscaled)
	switch {
	case scale < t.Scale:
		v.Mul(v, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(t.Scale-scale)), nil))
	case scale > t.Scale:
		unit := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale-t.Scale)), nil)
		q, r := new(big.Int).QuoRem(v, unit, new(big.Int))
		if r.Sign() != 0 {
			return decimal128.Num{}, fmt.Errorf("a value at scale %d does not fit scale %d without rounding", scale, t.Scale)
		}
		v = q
	}
	if v.BitLen() > 127 || !decimal128.FromBigInt(v).FitsInPrecision(t.Precision) {
		return decimal128.Num{}, fmt.Errorf("%s at scale %d is past Decimal(%d,%d)", v, t.Scale, t.Precision, t.Scale)
	}
	return decimal128.FromBigInt(v), nil
}

// sqlType is the DuckDB type a fixture column is created as: a decimal at
// its declared type, or at the most fraction digits its values have, so a
// fixture is never rounded on the way in (R117); a timestamp an instant
// (TIMESTAMPTZ), or wall-clock time (TIMESTAMP) when its column declares
// the zone it is in.
func sqlType(t fakes.Table, i int) (string, error) {
	c := t.Columns[i]
	switch c.Type {
	case ports.String:
		return "VARCHAR", nil
	case ports.Integer:
		return "BIGINT", nil
	case ports.Float:
		return "DOUBLE", nil
	case ports.Boolean:
		return "BOOLEAN", nil
	case ports.Date:
		return "DATE", nil
	case ports.Timestamp:
		if c.Zone != "" {
			return "TIMESTAMP", nil
		}
		return "TIMESTAMPTZ", nil
	case ports.Decimal:
		if c.Precision > 0 {
			if c.Precision > 38 || c.Scale < 0 || c.Scale > c.Precision {
				return "", fmt.Errorf("duckwh: %s.%s is no Decimal(%d,%d)", t.Name, c.Name, c.Precision, c.Scale)
			}
			return fmt.Sprintf("DECIMAL(%d, %d)", c.Precision, c.Scale), nil
		}
		scale := 0
		for _, row := range t.Rows {
			if s, ok := row[i].(string); ok {
				d, err := grammar.ParseDecimal(s)
				if err != nil {
					return "", fmt.Errorf("duckwh: %s.%s: %q is not a decimal", t.Name, c.Name, s)
				}
				scale = max(scale, len(d.Fraction))
			}
		}
		if scale > 38 {
			return "", fmt.Errorf("duckwh: %s.%s has %d fraction digits, more than DuckDB holds", t.Name, c.Name, scale)
		}
		return fmt.Sprintf("DECIMAL(38, %d)", scale), nil
	}
	return "", fmt.Errorf("duckwh: %s.%s has no type %q", t.Name, c.Name, c.Type)
}

// Load creates the tables and inserts their rows, refusing a decimal that
// does not fit its column's type rather than rounding it (R117).
func Load(ctx context.Context, db *sql.DB, tables []fakes.Table) error {
	attached := map[string]bool{}
	for _, t := range tables {
		parts := strings.Split(strings.ToUpper(t.Name), ".")
		if len(parts) != 3 {
			return fmt.Errorf("duckwh: %s is not database.schema.object", t.Name)
		}
		if !attached[parts[0]] {
			if _, err := db.ExecContext(ctx, fmt.Sprintf("ATTACH ':memory:' AS %s", parts[0])); err != nil {
				return fmt.Errorf("duckwh: %w", err)
			}
			attached[parts[0]] = true
		}
		if _, err := db.ExecContext(ctx, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s.%s", parts[0], parts[1])); err != nil {
			return fmt.Errorf("duckwh: %w", err)
		}
		cols := make([]string, len(t.Columns))
		marks := make([]string, len(t.Columns))
		types := make([]string, len(t.Columns))
		for i, c := range t.Columns {
			typ, err := sqlType(t, i)
			if err != nil {
				return err
			}
			types[i] = typ
			cols[i] = c.Name + " " + typ
			marks[i] = "CAST(? AS " + typ + ")"
		}
		name := strings.Join(parts, ".")
		if _, err := db.ExecContext(ctx, fmt.Sprintf("CREATE TABLE %s (%s)", name, strings.Join(cols, ", "))); err != nil {
			return fmt.Errorf("duckwh: %w", err)
		}
		insert := fmt.Sprintf("INSERT INTO %s VALUES (%s)", name, strings.Join(marks, ", "))
		for r, row := range t.Rows {
			args := make([]any, len(row))
			for i, v := range row {
				a, err := loadValue(t.Columns[i], types[i], v)
				if err != nil {
					return fmt.Errorf("duckwh: %s row %d, %s: %w", name, r, t.Columns[i].Name, err)
				}
				args[i] = a
			}
			if _, err := db.ExecContext(ctx, insert, args...); err != nil {
				return fmt.Errorf("duckwh: %s row %d: %w", name, r, err)
			}
		}
	}
	return nil
}

// loadValue is a fixture value as Load binds it: a timestamp as text with
// its offset, or as wall-clock text for a zoned column, so DuckDB's session
// zone plays no part; a decimal checked against its column's type.
func loadValue(c ports.Column, typ string, v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	switch c.Type {
	case ports.Timestamp:
		t, ok := v.(time.Time)
		if !ok {
			return nil, fmt.Errorf("a %T is not a time", v)
		}
		if c.Zone != "" {
			return t.Format("2006-01-02 15:04:05.999999999"), nil
		}
		return t.UTC().Format(time.RFC3339Nano), nil
	case ports.Decimal:
		s, ok := v.(string)
		d, err := grammar.ParseDecimal(s)
		if !ok || err != nil {
			return nil, fmt.Errorf("%v is not a decimal", v)
		}
		var p, sc int
		if _, err := fmt.Sscanf(typ, "DECIMAL(%d, %d)", &p, &sc); err != nil {
			return nil, fmt.Errorf("the type %s: %w", typ, err)
		}
		if !d.Fits(p, sc) {
			return nil, fmt.Errorf("%s does not fit DECIMAL(%d, %d): refused, never rounded", s, p, sc)
		}
	}
	return v, nil
}
