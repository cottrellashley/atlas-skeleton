package views_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/big"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"atlas.local/gateway/fakes"
	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/protocol/grammar"
)

// source is this package's test double of an engine: tables in memory,
// read through ports.DataSource as a real source reads them (a plan whose
// statement is rendered from the read, values bound, batches in the writer
// schema with decimals rounded to its scale, a record of each query for
// Settle).
type source struct {
	mu        sync.Mutex
	tables    map[string]*fakes.Table
	fail      error
	plans     []*ports.Plan
	explained int
	runs      []run
	batchRows int
	mem       memory.Allocator
}

type run struct {
	id, statement, tag string
	rows               int64
}

var _ ports.DataSource = (*source)(nil)

func newSource(tables ...fakes.Table) *source {
	s := &source{tables: map[string]*fakes.Table{}, batchRows: 2, mem: memory.NewGoAllocator()}
	for i := range tables {
		t := tables[i]
		t.Rows = slices.Clone(t.Rows)
		s.tables[strings.ToUpper(t.Name)] = &t
	}
	return s
}

// Fail makes every execution fail with err; nil heals it.
func (s *source) Fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail = err
}

// Edit changes the rows of a table that match, in place.
func (s *source) Edit(table string, match func(row []any, col func(string) any) bool, set map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tables[strings.ToUpper(table)]
	idx := map[string]int{}
	for i, c := range t.Columns {
		idx[c.Name] = i
	}
	for r, row := range t.Rows {
		if match(row, func(c string) any { return row[idx[c]] }) {
			copied := slices.Clone(row)
			for c, v := range set {
				copied[idx[c]] = v
			}
			t.Rows[r] = copied
		}
	}
}

// Add appends a row to a table.
func (s *source) Add(table string, row ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.tables[strings.ToUpper(table)]
	t.Rows = append(t.Rows, row)
}

func (s *source) Plans() []*ports.Plan {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.plans)
}

func (s *source) Runs() []run {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.runs)
}

func (s *source) Explained() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.explained
}

func (s *source) Capabilities() ports.SourceCapabilities {
	return ports.SourceCapabilities{Engine: "memory", Language: "sql", Dialect: "memory", Estimates: true}
}

func (s *source) table(object string) (*fakes.Table, error) {
	t, ok := s.tables[strings.ToUpper(object)]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ports.ErrUnknownObject, object)
	}
	return t, nil
}

// Plan renders the read as a statement, every value a bound parameter.
func (s *source) Plan(ctx context.Context, r ports.PlanRequest) (*ports.Plan, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.table(r.Query.Object)
	if err != nil {
		return nil, err
	}
	if r.Writer == nil || r.Writer.NumFields() != len(r.Columns) {
		return nil, fmt.Errorf("%w: a writer of %v for %d columns", ports.ErrPlanRefused, r.Writer, len(r.Columns))
	}
	has := map[string]bool{}
	for _, c := range t.Columns {
		has[c.Name] = true
	}
	for _, c := range r.Query.Columns {
		if !has[strings.ToUpper(c.Name)] {
			return nil, fmt.Errorf("%w: %s has no column %s", ports.ErrPlanRefused, t.Name, c.Name)
		}
	}
	var b strings.Builder
	var binds []any
	b.WriteString("SELECT ")
	for i, f := range r.Writer.Fields() {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s AS %q", strings.Join(r.Columns[i], "+"), f.Name)
	}
	fmt.Fprintf(&b, " FROM %s", strings.ToUpper(r.Query.Object))
	preds := func(ps []ports.Predicate) {
		for i, p := range ps {
			if i > 0 {
				b.WriteString(" AND ")
			}
			marks := make([]string, len(p.Values))
			for k, v := range p.Values {
				marks[k] = "?"
				binds = append(binds, v)
			}
			fmt.Fprintf(&b, "%s %s (%s)", p.Column, p.Op, strings.Join(marks, ", "))
		}
	}
	if l := r.Query.Latest; l != nil {
		fmt.Fprintf(&b, " LATEST %s PER %s", l.Date, strings.Join(l.Per, ", "))
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
		fmt.Fprintf(&b, " ORDER BY %s", strings.Join(r.Query.OrderBy, ", "))
	}
	if r.Query.Limit > 0 {
		fmt.Fprintf(&b, " LIMIT %d", r.Query.Limit)
	}
	p := &ports.Plan{Statement: b.String(), Dialect: "memory", Binds: binds, Objects: []string{strings.ToUpper(r.Query.Object)},
		Tag: "atlas:" + r.Capability + ":" + r.RequestID, Writer: r.Writer, Request: r}
	s.plans = append(s.plans, p)
	return p, nil
}

func (s *source) Explain(ctx context.Context, p *ports.Plan) (*ports.Explanation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.explained++
	t, err := s.table(p.Request.Query.Object)
	if err != nil {
		return nil, err
	}
	rows := int64(len(t.Rows))
	return &ports.Explanation{Language: "sql", Dialect: p.Dialect, Statement: p.Statement, Tag: p.Tag, Rows: &rows, Heuristic: true}, nil
}

func (s *source) Execute(ctx context.Context, p *ports.Plan) (*ports.Result, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return nil, s.fail
	}
	t, err := s.table(p.Request.Query.Object)
	if err != nil {
		return nil, err
	}
	rows, err := evaluate(t, p.Request.Query)
	if err != nil {
		return nil, err
	}
	at := map[string]int{}
	for i, c := range p.Request.Query.Columns {
		at[strings.ToUpper(c.Name)] = i
	}
	var batches []arrow.RecordBatch
	for start := 0; start < len(rows); start += s.batchRows {
		b, err := batch(s.mem, p.Request, at, rows[start:min(start+s.batchRows, len(rows))])
		if err != nil {
			for _, x := range batches {
				x.Release()
			}
			return nil, err
		}
		batches = append(batches, b)
	}
	id := fmt.Sprintf("q-%d", len(s.runs)+1)
	s.runs = append(s.runs, run{id: id, statement: p.Statement, tag: p.Tag, rows: int64(len(rows))})
	return &ports.Result{QueryID: id, Batches: &memBatches{schema: p.Writer, list: batches}}, nil
}

func (s *source) Settle(ctx context.Context, id string) (ports.Usage, error) {
	if err := ctx.Err(); err != nil {
		return ports.Usage{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.runs {
		if r.id == id {
			return ports.Usage{QueryID: id, Rows: r.rows, Bytes: 100 * r.rows, Cost: map[string]string{"credits": "0.001"}}, nil
		}
	}
	return ports.Usage{}, fmt.Errorf("%w: %s", ports.ErrUnknownQuery, id)
}

// batch writes rows (values in the query's column order) as the writer's
// fields, as a source casting to the contract would: a decimal rounded to
// its scale, half to even.
func batch(mem memory.Allocator, r ports.PlanRequest, at map[string]int, rows [][]any) (arrow.RecordBatch, error) {
	rb := array.NewRecordBuilder(mem, r.Writer)
	defer rb.Release()
	for _, row := range rows {
		for i, f := range r.Writer.Fields() {
			from := r.Columns[i]
			if st, ok := f.Type.(*arrow.StructType); ok {
				sb := rb.Field(i).(*array.StructBuilder)
				if row[at[strings.ToUpper(from[0])]] == nil && row[at[strings.ToUpper(from[1])]] == nil {
					sb.AppendNull()
					continue
				}
				sb.Append(true)
				for k := range from {
					if err := leaf(sb.FieldBuilder(k), st.Field(k).Type, row[at[strings.ToUpper(from[k])]]); err != nil {
						return nil, err
					}
				}
				continue
			}
			if err := leaf(rb.Field(i), f.Type, row[at[strings.ToUpper(from[0])]]); err != nil {
				return nil, fmt.Errorf("%s: %w", f.Name, err)
			}
		}
	}
	return rb.NewRecordBatch(), nil
}

func leaf(b array.Builder, t arrow.DataType, v any) error {
	if v == nil {
		b.AppendNull()
		return nil
	}
	switch bb := b.(type) {
	case *array.StringBuilder:
		bb.Append(v.(string))
	case *array.Int64Builder:
		bb.Append(v.(int64))
	case *array.Float64Builder:
		bb.Append(v.(float64))
	case *array.BooleanBuilder:
		bb.Append(v.(bool))
	case *array.Date32Builder:
		d, err := grammar.ParseDate(v.(string))
		if err != nil {
			return err
		}
		bb.Append(arrow.Date32FromTime(time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)))
	case *array.TimestampBuilder:
		bb.Append(arrow.Timestamp(v.(time.Time).UTC().UnixMicro()))
	case *array.Decimal128Builder:
		dt := t.(*arrow.Decimal128Type)
		x, ok := new(big.Rat).SetString(v.(string))
		if !ok {
			return fmt.Errorf("%v is not a decimal", v)
		}
		scaled := new(big.Rat).Mul(x, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(dt.Scale)), nil)))
		u := roundHalfEven(scaled)
		if len(new(big.Int).Abs(u).String()) > int(dt.Precision) {
			return fmt.Errorf("%v does not fit Decimal(%d,%d)", v, dt.Precision, dt.Scale)
		}
		bb.Append(decimal128.FromBigInt(u))
	default:
		return fmt.Errorf("no %s", t)
	}
	return nil
}

func roundHalfEven(r *big.Rat) *big.Int {
	q, m := new(big.Int).QuoRem(r.Num(), r.Denom(), new(big.Int))
	twice := new(big.Int).Mul(new(big.Int).Abs(m), big.NewInt(2))
	switch c := twice.Cmp(r.Denom()); {
	case c > 0, c == 0 && q.Bit(0) == 1:
		if r.Sign() < 0 {
			q.Sub(q, big.NewInt(1))
		} else {
			q.Add(q, big.NewInt(1))
		}
	}
	return q
}

type memBatches struct {
	schema *arrow.Schema
	list   []arrow.RecordBatch
	closed bool
}

func (b *memBatches) Schema() *arrow.Schema { return b.schema }

func (b *memBatches) Next(ctx context.Context) (arrow.RecordBatch, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b.closed || len(b.list) == 0 {
		return nil, io.EOF
	}
	next := b.list[0]
	b.list = b.list[1:]
	return next, nil
}

func (b *memBatches) Close() error {
	if !b.closed {
		b.closed = true
		for _, r := range b.list {
			r.Release()
		}
		b.list = nil
	}
	return nil
}

// evaluate is a query's rows, each its values in the query's column order:
// the latest of each group chosen before the query's own conditions, SQL's
// three-valued comparisons, nulls last.
func evaluate(t *fakes.Table, q ports.Query) ([][]any, error) {
	index := map[string]int{}
	for i, c := range t.Columns {
		index[strings.ToUpper(c.Name)] = i
	}
	col := func(name string) (int, ports.ColumnType, error) {
		i, ok := index[strings.ToUpper(name)]
		if !ok {
			return 0, "", fmt.Errorf("%s has no column %s", t.Name, name)
		}
		return i, t.Columns[i].Type, nil
	}
	match := func(row []any, ps []ports.Predicate) (bool, error) {
		for _, p := range ps {
			i, typ, err := col(p.Column)
			if err != nil {
				return false, err
			}
			if !holds(row[i], typ, p) {
				return false, nil
			}
		}
		return true, nil
	}
	rows := t.Rows
	if l := q.Latest; l != nil {
		di, _, err := col(l.Date)
		if err != nil {
			return nil, err
		}
		key := func(row []any) string {
			var b strings.Builder
			for _, p := range l.Per {
				i, _, _ := col(p)
				fmt.Fprintf(&b, "%v\x00", row[i])
			}
			return b.String()
		}
		latest := map[string]string{}
		for _, row := range rows {
			ok, err := match(row, l.Within)
			if err != nil {
				return nil, err
			}
			d, _ := row[di].(string)
			if ok && (l.OnOrBefore == "" || d <= l.OnOrBefore) && d > latest[key(row)] {
				latest[key(row)] = d
			}
		}
		var kept [][]any
		for _, row := range rows {
			if d, _ := row[di].(string); latest[key(row)] == d && d != "" {
				kept = append(kept, row)
			}
		}
		rows = kept
	}
	var out [][]any
	for _, row := range rows {
		ok, err := match(row, q.Where)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, row)
		}
	}
	var order []int
	for _, o := range q.OrderBy {
		i, _, err := col(o)
		if err != nil {
			return nil, err
		}
		order = append(order, i)
	}
	slices.SortStableFunc(out, func(a, b []any) int {
		for _, i := range order {
			if c := compare(a[i], b[i], t.Columns[i].Type); c != 0 {
				return c
			}
		}
		return 0
	})
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	result := make([][]any, len(out))
	for r, row := range out {
		for _, c := range q.Columns {
			i, _, err := col(c.Name)
			if err != nil {
				return nil, err
			}
			result[r] = append(result[r], row[i])
		}
	}
	return result, nil
}

func holds(v any, typ ports.ColumnType, p ports.Predicate) bool {
	if v == nil {
		return false
	}
	c := func(want any) int { return compare(v, want, typ) }
	switch p.Op {
	case ports.In:
		return slices.ContainsFunc(p.Values, func(w any) bool { return c(w) == 0 })
	case ports.Eq:
		return c(p.Values[0]) == 0
	case ports.Gte:
		return c(p.Values[0]) >= 0
	case ports.Lte:
		return c(p.Values[0]) <= 0
	}
	return false
}

// compare orders two values of a column's type, nulls last.
func compare(a, b any, typ ports.ColumnType) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return 1
	case b == nil:
		return -1
	}
	switch typ {
	case ports.Decimal, ports.Integer, ports.Float:
		x, _ := new(big.Rat).SetString(fmt.Sprint(a))
		y, _ := new(big.Rat).SetString(fmt.Sprint(b))
		if x != nil && y != nil {
			return x.Cmp(y)
		}
	case ports.Timestamp:
		x, _ := a.(time.Time)
		y, _ := b.(time.Time)
		return x.Compare(y)
	}
	return strings.Compare(fmt.Sprint(a), fmt.Sprint(b))
}

var errBoom = errors.New("the engine fell over")
