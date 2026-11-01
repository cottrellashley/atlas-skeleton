package fakes

import (
	"context"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/protocol/grammar"
)

// Run is one query an engine began, as its record keeps it.
type Run struct {
	QueryID string
	// Statement is the statement the engine was given, and Tag what it
	// recorded with the query, out of band.
	Statement, Tag string
	// Rows are the rows the query produced.
	Rows int64
	// Stopped reports a query that ended before its last row, because its
	// context ended or its answer was closed.
	Stopped bool
	// Done reports a query that has ended, one way or another.
	Done bool
}

// BatchRows is the most rows a fake puts in one record batch, so even a
// small answer comes in more than one.
const BatchRows = 3

// DataSource answers reads from tables in memory with the meaning
// porttest.DataSource proves every data source gives them, and keeps the
// record of each query an engine would. Its hooks fault the engine: Fail
// fails every query, Hold holds each until its context ends.
type DataSource struct {
	mu     sync.Mutex
	tables map[string]*Table
	err    error
	hold   func()
	runs   []*Run
	calls  int
	mem    memory.Allocator
}

var _ ports.DataSource = (*DataSource)(nil)

// NewDataSource holds the tables, by their names, upper-cased as Snowflake
// resolves unquoted identifiers. Every table is a registered object.
func NewDataSource(tables ...Table) *DataSource {
	d := &DataSource{tables: map[string]*Table{}, mem: memory.DefaultAllocator}
	for i := range tables {
		t := tables[i]
		d.tables[strings.ToUpper(t.Name)] = &t
	}
	return d
}

// Fail makes every query fail with err; nil heals it.
func (d *DataSource) Fail(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.err = err
}

// Hold makes the engine hold each query it is given until the query's
// context ends, calling reached once it holds one; nil lets queries run.
func (d *DataSource) Hold(reached func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.hold = reached
}

// Runs are the queries the engine began, in order.
func (d *DataSource) Runs() []Run {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]Run, len(d.runs))
	for i, r := range d.runs {
		out[i] = *r
	}
	return out
}

// Calls counts the queries that reached the engine.
func (d *DataSource) Calls() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

// Capabilities are SQL-like statements in the fake's own dialect, with
// row estimates from the size of the object.
func (d *DataSource) Capabilities() ports.SourceCapabilities {
	return ports.SourceCapabilities{Engine: "memory", Language: "sql", Dialect: "memory", Estimates: true}
}

var (
	tagChars      = regexp.MustCompile(`^[A-Za-z0-9._@:/-]{1,256}$`)
	contractName  = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,127}$`)
	memoryQueryID = regexp.MustCompile(`^memory-[0-9]+$`)
)

// Plan checks the read as a real source does, every value a parameter and
// every field castable to its contract type, and renders it as a
// statement in the fake's dialect. It reaches no engine.
func (d *DataSource) Plan(ctx context.Context, r ports.PlanRequest) (*ports.Plan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d.mu.Lock()
	t, ok := d.tables[strings.ToUpper(r.Query.Object)]
	d.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ports.ErrUnknownObject, r.Query.Object)
	}
	if _, err := fields(t, r); err != nil {
		return nil, err
	}
	capability := r.Capability
	if capability == "" {
		capability = r.Query.Tag
	}
	tag := "atlas"
	for _, part := range []string{capability, r.RequestID} {
		if part != "" {
			tag += ":" + part
		}
	}
	if !tagChars.MatchString(tag) {
		return nil, fmt.Errorf("%w: the tag %q holds more than names and ids", ports.ErrPlanRefused, tag)
	}
	text, binds := render(r)
	return &ports.Plan{Statement: text, Dialect: "memory", Binds: binds, Objects: []string{strings.ToUpper(r.Query.Object)}, Tag: tag, Writer: r.Writer, Request: r}, nil
}

// field is one writer field and where its values come from.
type field struct {
	f       arrow.Field
	members []source // one, or a struct's members in order
}

type source struct {
	t     arrow.DataType
	at    int          // the query column's position
	query ports.Column // as the query reads it
	table ports.Column // as the table holds it
}

// fields check a plan's writer against the query and the table: each field
// made from query columns the table has, each castable to its contract
// type, as the Snowflake adapter casts.
func fields(t *Table, r ports.PlanRequest) ([]field, error) {
	if r.Writer == nil || r.Writer.NumFields() == 0 || len(r.Columns) != r.Writer.NumFields() {
		return nil, fmt.Errorf("%w: the writer schema and its columns do not match", ports.ErrPlanRefused)
	}
	at := map[string]int{}
	for i, c := range r.Query.Columns {
		at[strings.ToUpper(c.Name)] = i
	}
	inTable := map[string]ports.Column{}
	for _, c := range t.Columns {
		inTable[strings.ToUpper(c.Name)] = c
	}
	member := func(name string, typ arrow.DataType, from string) (source, error) {
		i, ok := at[strings.ToUpper(from)]
		if !ok {
			return source{}, fmt.Errorf("%w: %s is made from %s, which the query does not read", ports.ErrPlanRefused, name, from)
		}
		q := r.Query.Columns[i]
		tc, ok := inTable[strings.ToUpper(from)]
		if !ok {
			return source{}, fmt.Errorf("%w: %s has no column %s", ports.ErrPlanRefused, t.Name, from)
		}
		if !castable(q.Type, typ) {
			return source{}, fmt.Errorf("%w: the %s column %s cannot be read as %s", ports.ErrPlanRefused, q.Type, q.Name, typ)
		}
		if q.Zone != "" {
			if _, err := time.LoadLocation(q.Zone); err != nil {
				return source{}, fmt.Errorf("%w: %s: %w", ports.ErrPlanRefused, q.Name, err)
			}
		}
		return source{t: typ, at: i, query: q, table: tc}, nil
	}
	out := make([]field, r.Writer.NumFields())
	for i, f := range r.Writer.Fields() {
		if !contractName.MatchString(f.Name) {
			return nil, fmt.Errorf("%w: %q is not a contract name", ports.ErrPlanRefused, f.Name)
		}
		out[i].f = f
		st, isStruct := f.Type.(*arrow.StructType)
		switch {
		case isStruct && len(r.Columns[i]) == st.NumFields():
			for j, m := range st.Fields() {
				s, err := member(f.Name+"."+m.Name, m.Type, r.Columns[i][j])
				if err != nil {
					return nil, err
				}
				out[i].members = append(out[i].members, s)
			}
		case !isStruct && len(r.Columns[i]) == 1:
			s, err := member(f.Name, f.Type, r.Columns[i][0])
			if err != nil {
				return nil, err
			}
			out[i].members = []source{s}
		default:
			return nil, fmt.Errorf("%w: %s is made from %d columns", ports.ErrPlanRefused, f.Name, len(r.Columns[i]))
		}
	}
	return out, nil
}

// castable is the Snowflake adapter's rule: only a cast that keeps a
// value's meaning.
func castable(c ports.ColumnType, t arrow.DataType) bool {
	switch t := t.(type) {
	case *arrow.StringType:
		return c == ports.String
	case *arrow.Int64Type:
		return c == ports.Integer
	case *arrow.Float64Type:
		return c == ports.Float || c == ports.Decimal || c == ports.Integer
	case *arrow.Decimal128Type:
		return (c == ports.Decimal || c == ports.Integer) && t.Precision >= 1 && t.Precision <= 38 && t.Scale >= 0 && t.Scale <= t.Precision
	case *arrow.BooleanType:
		return c == ports.Boolean
	case *arrow.Date32Type:
		return c == ports.Date
	case *arrow.TimestampType:
		return c == ports.Timestamp && t.Unit == arrow.Microsecond && t.TimeZone == "UTC"
	}
	return false
}

// render writes a read as the fake's statement: every value a parameter,
// in order.
func render(r ports.PlanRequest) (string, []any) {
	var b strings.Builder
	var binds []any
	preds := func(ps []ports.Predicate) {
		for i, p := range ps {
			if i > 0 {
				b.WriteString(" AND ")
			}
			fmt.Fprintf(&b, "%s %s (", strings.ToUpper(p.Column), strings.ToUpper(string(p.Op)))
			for j, v := range p.Values {
				if j > 0 {
					b.WriteString(", ")
				}
				b.WriteString("?")
				binds = append(binds, v)
			}
			b.WriteString(")")
		}
	}
	b.WriteString("SELECT ")
	for i, f := range r.Writer.Fields() {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s AS %q (%s)", strings.ToUpper(strings.Join(r.Columns[i], ", ")), f.Name, f.Type)
	}
	fmt.Fprintf(&b, " FROM %s", strings.ToUpper(r.Query.Object))
	if l := r.Query.Latest; l != nil {
		fmt.Fprintf(&b, " LATEST %s PER %s", strings.ToUpper(l.Date), strings.ToUpper(strings.Join(l.Per, ", ")))
		if l.OnOrBefore != "" {
			b.WriteString(" ON OR BEFORE ?")
			binds = append(binds, l.OnOrBefore)
		}
		if len(l.Within) > 0 {
			b.WriteString(" WITHIN ")
			preds(l.Within)
		}
	}
	if len(r.Query.Where) > 0 {
		b.WriteString(" WHERE ")
		preds(r.Query.Where)
	}
	if len(r.Query.OrderBy) > 0 {
		fmt.Fprintf(&b, " ORDER BY %s NULLS LAST", strings.ToUpper(strings.Join(r.Query.OrderBy, ", ")))
	}
	if r.Query.Limit > 0 {
		fmt.Fprintf(&b, " LIMIT %d", r.Query.Limit)
	}
	return b.String(), binds
}

// Explain is the plan's statement and an estimate of its rows: every row
// of its object, a heuristic.
func (d *DataSource) Explain(ctx context.Context, p *ports.Plan) (*ports.Explanation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("%w: no plan to explain", ports.ErrPlanRefused)
	}
	d.mu.Lock()
	t, ok := d.tables[strings.ToUpper(p.Request.Query.Object)]
	d.mu.Unlock()
	e := &ports.Explanation{Language: "sql", Dialect: p.Dialect, Statement: p.Statement, Tag: p.Tag, Heuristic: true}
	if ok {
		n := int64(len(t.Rows))
		e.Rows = &n
	}
	return e, nil
}

// Execute runs a plan: the engine is reached, records the query, and
// answers the rows in batches of BatchRows.
func (d *DataSource) Execute(ctx context.Context, p *ports.Plan) (*ports.Result, error) {
	if p == nil || p.Writer == nil {
		return nil, fmt.Errorf("%w: no plan to execute", ports.ErrPlanRefused)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d.mu.Lock()
	t, ok := d.tables[strings.ToUpper(p.Request.Query.Object)]
	d.calls++
	run := &Run{QueryID: "memory-" + strconv.Itoa(len(d.runs)+1), Statement: p.Statement, Tag: p.Tag}
	d.runs = append(d.runs, run)
	fail, hold := d.err, d.hold
	d.mu.Unlock()
	end := func(stopped bool) {
		d.mu.Lock()
		defer d.mu.Unlock()
		run.Stopped, run.Done = stopped, true
	}
	if hold != nil {
		hold()
		<-ctx.Done()
		end(true)
		return nil, ctx.Err()
	}
	if fail != nil {
		end(false)
		return nil, fmt.Errorf("fakes: the engine failed: %w", fail)
	}
	if !ok {
		end(false)
		return nil, fmt.Errorf("%w: %s", ports.ErrUnknownObject, p.Request.Query.Object)
	}
	fs, err := fields(t, p.Request)
	if err != nil {
		end(false)
		return nil, err
	}
	rows, err := evaluate(t, p.Request.Query)
	if err != nil {
		end(false)
		return nil, err
	}
	d.mu.Lock()
	run.Rows = int64(len(rows))
	d.mu.Unlock()
	return &ports.Result{QueryID: run.QueryID, Batches: &batches{d: d, run: run, end: end, writer: p.Writer, fields: fs, rows: rows, ctx: ctx}}, nil
}

// Settle is the query's record: the rows it produced.
func (d *DataSource) Settle(ctx context.Context, queryID string) (ports.Usage, error) {
	if err := ctx.Err(); err != nil {
		return ports.Usage{}, err
	}
	if !memoryQueryID.MatchString(queryID) {
		return ports.Usage{}, fmt.Errorf("%w: %s", ports.ErrUnknownQuery, queryID)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, r := range d.runs {
		if r.QueryID == queryID {
			if !r.Done {
				return ports.Usage{}, fmt.Errorf("%w: the query %s has not ended", ports.ErrSourceUnavailable, queryID)
			}
			return ports.Usage{QueryID: r.QueryID, Rows: r.Rows}, nil
		}
	}
	return ports.Usage{}, fmt.Errorf("%w: %s", ports.ErrUnknownQuery, queryID)
}

// batches are a query's rows as writer batches.
type batches struct {
	d      *DataSource
	run    *Run
	end    func(stopped bool)
	writer *arrow.Schema
	fields []field
	rows   [][]any
	ctx    context.Context
	next   int
	ended  bool
}

func (b *batches) Schema() *arrow.Schema { return b.writer }

func (b *batches) Next(ctx context.Context) (arrow.RecordBatch, error) {
	if b.ended {
		return nil, io.EOF
	}
	for _, c := range []context.Context{ctx, b.ctx} {
		if err := c.Err(); err != nil {
			b.stop(true)
			return nil, err
		}
	}
	if b.next >= len(b.rows) {
		b.stop(false)
		return nil, io.EOF
	}
	chunk := b.rows[b.next:min(b.next+BatchRows, len(b.rows))]
	rb := array.NewRecordBuilder(b.d.mem, b.writer)
	defer rb.Release()
	for _, row := range chunk {
		for i, f := range b.fields {
			if err := appendField(rb.Field(i), f, row); err != nil {
				b.stop(false)
				return nil, err
			}
		}
	}
	b.next += len(chunk)
	return rb.NewRecordBatch(), nil
}

func (b *batches) stop(stopped bool) {
	if !b.ended {
		b.ended = true
		b.end(stopped)
	}
}

func (b *batches) Close() error {
	b.stop(b.next < len(b.rows))
	return nil
}

// appendField writes one row's value of a writer field.
func appendField(bb array.Builder, f field, row []any) error {
	if sb, ok := bb.(*array.StructBuilder); ok {
		nulls := 0
		for _, m := range f.members {
			if row[m.at] == nil {
				nulls++
			}
		}
		switch {
		case nulls == len(f.members):
			if !f.f.Nullable {
				return fmt.Errorf("fakes: %s is null, and its contract does not allow null", f.f.Name)
			}
			sb.AppendNull()
			return nil
		case nulls > 0:
			return fmt.Errorf("fakes: %s has only some of its members", f.f.Name)
		}
		sb.Append(true)
		for j, m := range f.members {
			if err := appendLeaf(sb.FieldBuilder(j), m, row[m.at]); err != nil {
				return fmt.Errorf("fakes: %s: %w", f.f.Name, err)
			}
		}
		return nil
	}
	v := row[f.members[0].at]
	if v == nil && !f.f.Nullable {
		return fmt.Errorf("fakes: %s is null, and its contract does not allow null", f.f.Name)
	}
	if err := appendLeaf(bb, f.members[0], v); err != nil {
		return fmt.Errorf("fakes: %s: %w", f.f.Name, err)
	}
	return nil
}

// appendLeaf writes a value as its contract type, cast as the engines cast
// it: a decimal rounded half away from zero to the contract's scale, and
// refused when its integer digits do not fit (R100).
func appendLeaf(bb array.Builder, s source, v any) error {
	if v == nil {
		bb.AppendNull()
		return nil
	}
	bad := fmt.Errorf("a %T is not a %s value", v, s.query.Type)
	switch b := bb.(type) {
	case *array.StringBuilder:
		x, ok := v.(string)
		if !ok {
			return bad
		}
		b.Append(x)
	case *array.Int64Builder:
		x, ok := v.(int64)
		if !ok {
			return bad
		}
		b.Append(x)
	case *array.Float64Builder:
		switch x := v.(type) {
		case float64:
			b.Append(x)
		case int64:
			b.Append(float64(x))
		case string:
			f, err := strconv.ParseFloat(x, 64)
			if err != nil {
				return bad
			}
			b.Append(f)
		default:
			return bad
		}
	case *array.Decimal128Builder:
		t := s.t.(*arrow.Decimal128Type)
		var r *big.Rat
		switch x := v.(type) {
		case string:
			var ok bool
			if r, ok = new(big.Rat).SetString(x); !ok {
				return bad
			}
		case int64:
			r = new(big.Rat).SetInt64(x)
		default:
			return bad
		}
		n, err := Rescale(r, t.Precision, t.Scale)
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
		x, _ := v.(string)
		d, err := grammar.ParseDate(x)
		if err != nil {
			return bad
		}
		b.Append(arrow.Date32FromTime(time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)))
	case *array.TimestampBuilder:
		x, ok := v.(time.Time)
		if !ok {
			return bad
		}
		b.Append(arrow.Timestamp(x.UTC().UnixMicro()))
	default:
		return fmt.Errorf("no fake value of %s", s.t)
	}
	return nil
}

// Rescale is an exact value at a decimal's scale, rounded half away from
// zero as Snowflake's and DuckDB's casts round, and refused when it has
// more integer digits than the decimal's precision leaves.
func Rescale(r *big.Rat, precision, scale int32) (decimal128.Num, error) {
	unit := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(scale)), nil)
	scaled := new(big.Rat).Mul(r, new(big.Rat).SetInt(unit))
	q, m := new(big.Int).QuoRem(scaled.Num(), scaled.Denom(), new(big.Int))
	// |remainder| / denominator ≥ 1/2 rounds away from zero.
	if twice := new(big.Int).Mul(new(big.Int).Abs(m), big.NewInt(2)); twice.Cmp(scaled.Denom()) >= 0 {
		q.Add(q, big.NewInt(int64(scaled.Num().Sign())))
	}
	if q.BitLen() > 127 || !decimal128.FromBigInt(q).FitsInPrecision(precision) {
		return decimal128.Num{}, fmt.Errorf("%s is past Decimal(%d,%d)", r.FloatString(int(scale)), precision, scale)
	}
	return decimal128.FromBigInt(q), nil
}
