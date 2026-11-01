package views

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/arrowschema"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
)

// answer is a table's answer: the source's batches, each held to the
// writer schema it was planned with, its enum values mapped, its computed
// properties written, and its rows held to their key, then projected to
// the properties asked for (§6.4 rule 2: a batch outside its writer schema
// is the adapter's fault, never the caller's).
type answer struct {
	p   *Provider
	v   *view
	rd  *read
	src ports.BatchStream
	now time.Time

	prev   []string // the last row's key
	err    error
	done   bool
	closed bool
}

// Schema is the answer's Arrow schema.
func (a *answer) Schema() *arrow.Schema { return a.rd.answer }

// Next is the answer's next batch, or io.EOF after the last.
func (a *answer) Next(ctx context.Context) (arrow.RecordBatch, error) {
	if a.err != nil {
		return nil, a.err
	}
	if a.done {
		return nil, io.EOF
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	b, err := a.src.Next(ctx)
	if errors.Is(err, io.EOF) {
		a.done = true
		return nil, io.EOF
	}
	if err != nil {
		a.err = a.p.failure(a.v, "reading "+a.rd.req.Query.Object, err)
		return nil, a.err
	}
	defer b.Release()
	rec, err := a.shape(b)
	if err != nil {
		a.err = err
		return nil, err
	}
	return rec, nil
}

// Close releases the source, and is safe to call more than once.
func (a *answer) Close() error {
	if a.closed {
		return nil
	}
	a.closed = true
	if err := a.src.Close(); err != nil {
		return fmt.Errorf("views: closing the read of %s: %w", a.rd.req.Query.Object, err)
	}
	return nil
}

// shape is one source batch as the answer's batch.
func (a *answer) shape(b arrow.RecordBatch) (arrow.RecordBatch, error) {
	writer := a.rd.req.Writer
	if got := b.Schema(); got.NumFields() != writer.NumFields() {
		return nil, internal("the source answered %d columns of %s for %d", got.NumFields(), a.v.pinned, writer.NumFields())
	}
	n := int(b.NumRows())
	cols := map[string]arrow.Array{}
	var owned []arrow.Array
	defer func() {
		for _, c := range owned {
			c.Release()
		}
	}()
	for i, f := range writer.Fields() {
		got := b.Schema().Field(i)
		if got.Name != f.Name || !arrow.TypeEqual(got.Type, f.Type) {
			return nil, internal("the source answered %s of %s as %s %s, not the writer schema's %s", f.Name, a.v.pinned, got.Name, got.Type, f.Type)
		}
		col := b.Column(i)
		if err := a.v.held(f, col); err != nil {
			return nil, err
		}
		if mapped, err := a.v.enum(a.p, f.Name, col); err != nil {
			return nil, err
		} else if mapped != nil {
			owned = append(owned, mapped)
			col = mapped
		}
		cols[f.Name] = col
	}
	if err := a.keyed(cols, n); err != nil {
		return nil, err
	}
	if f := a.v.model.Freshness; f != nil && slices.Contains(a.rd.props, f.Status) {
		status, err := a.status(cols, n)
		if err != nil {
			return nil, err
		}
		owned = append(owned, status)
		cols[f.Status] = status
	}
	out := make([]arrow.Array, len(a.rd.props))
	for i, p := range a.rd.props {
		out[i] = cols[p]
		if out[i] == nil {
			return nil, internal("%s of %s has no value to answer", p, a.v.pinned)
		}
	}
	return array.NewRecordBatch(a.rd.answer, out, int64(n)), nil
}

// held holds one column to what its property allows: no null where the
// contract has none, an integer within what I-JSON carries, a number JSON
// can hold.
func (v *view) held(f arrow.Field, col arrow.Array) error {
	if !f.Nullable && col.NullN() > 0 {
		return internal("the source answered a null %s for %s, which the contract does not allow", f.Name, v.pinned)
	}
	ps, _ := v.row.Property(f.Name)
	switch x := col.(type) {
	case *array.Int64:
		if ps.Format() == "int64" {
			return nil
		}
		for i := 0; i < x.Len(); i++ {
			if x.IsValid(i) && (x.Value(i) > ijson.MaxSafeInteger || x.Value(i) < -ijson.MaxSafeInteger) {
				return internal("the source answered a %s for %s past what a JSON number holds exactly", f.Name, v.pinned)
			}
		}
	case *array.Float64:
		for i := 0; i < x.Len(); i++ {
			if x.IsValid(i) && (math.IsNaN(x.Value(i)) || math.IsInf(x.Value(i), 0)) {
				return internal("the source answered a %s for %s that JSON cannot hold", f.Name, v.pinned)
			}
		}
	case *array.Struct:
		st := x.DataType().(*arrow.StructType)
		for k, m := range st.Fields() {
			if m.Nullable {
				continue
			}
			for i := 0; i < x.Len(); i++ {
				if x.IsValid(i) && x.Field(k).IsNull(i) {
					return internal("the source answered a %s for %s with no %s, which the contract does not allow", f.Name, v.pinned, m.Name)
				}
			}
		}
	}
	return nil
}

// enum is a column of an enum property with its values as the contract
// names them: mapped where the view model maps them, UNKNOWN where the
// contract has no such value and allows it, refused otherwise (§9.5.5). It
// is nil for a column that is no enum's.
func (v *view) enum(p *Provider, prop string, col arrow.Array) (arrow.Array, error) {
	ps, _ := v.row.Property(prop)
	if ps.Enum() == nil {
		return nil, nil
	}
	in, ok := col.(*array.String)
	if !ok {
		return nil, internal("%s of %s is an enum read from a %s column", prop, v.pinned, col.DataType())
	}
	values := v.model.Columns[prop].Values
	b := array.NewStringBuilder(p.mem)
	defer b.Release()
	b.Reserve(in.Len())
	for i := 0; i < in.Len(); i++ {
		if in.IsNull(i) {
			b.AppendNull()
			continue
		}
		s := in.Value(i)
		switch mapped, ok := values[s]; {
		case ok:
			b.Append(mapped)
		case values == nil && ps.EnumHas(s):
			b.Append(s)
		case ps.EnumHas("UNKNOWN"):
			b.Append("UNKNOWN")
		default:
			return nil, internal("the source answered a %s for %s that maps to none of the enum's values", prop, v.pinned)
		}
	}
	return b.NewArray(), nil
}

// keyed refuses a row that repeats the key of the row before it: the
// order begins with the key's columns, so repeated keys are next to each
// other (R038). A source at another grain (a security's lots as rows) is
// refused as the source's fault, never answered as two positions.
func (a *answer) keyed(cols map[string]arrow.Array, n int) error {
	if len(a.v.keys) == 0 {
		return nil
	}
	for i := 0; i < n; i++ {
		key := make([]string, len(a.v.keys))
		null := false
		for k, p := range a.v.keys {
			c := cols[p]
			if c.IsNull(i) {
				null = true
				break
			}
			key[k] = c.ValueStr(i)
		}
		if null {
			a.prev = nil // a null key part equals nothing, as in SQL
			continue
		}
		if a.prev != nil && slices.Equal(a.prev, key) {
			return internal("the source answered two rows of %s with one key: it is not at the contract's grain", a.v.pinned)
		}
		a.prev = key
	}
	return nil
}

// status is the Freshness status of each row, as of the call (R035).
func (a *answer) status(cols map[string]arrow.Array, n int) (arrow.Array, error) {
	f := a.v.model.Freshness
	latest, ok1 := cols[f.Latest].(*array.Date32)
	expected, ok2 := cols[f.Expected].(*array.Date32)
	checked, ok3 := cols[f.Checked].(*array.Timestamp)
	if !ok1 || !ok2 || !ok3 {
		return nil, internal("the freshness of %s reads two dates and a timestamp", a.v.pinned)
	}
	unit := checked.DataType().(*arrow.TimestampType).Unit
	b := array.NewStringBuilder(a.p.mem)
	defer b.Release()
	b.Reserve(n)
	for i := 0; i < n; i++ {
		switch {
		case checked.IsNull(i) || expected.IsNull(i) || a.now.Sub(checked.Value(i).ToTime(unit)) > a.v.within:
			b.Append(f.Values.Unchecked)
		case latest.IsNull(i) || latest.Value(i) < expected.Value(i):
			b.Append(f.Values.Late)
		default:
			b.Append(f.Values.Current)
		}
	}
	return b.NewArray(), nil
}

// rowStream is an answer read as rows: the form the pipeline reads until it
// reads batches (Options.Rows).
type rowStream struct {
	a   *answer
	buf []*ijson.Object
	i   int
}

func (r *rowStream) Next(ctx context.Context) (*ijson.Object, error) {
	for r.i >= len(r.buf) {
		b, err := r.a.Next(ctx)
		if err != nil {
			return nil, err
		}
		rows, err := arrowschema.ToRows(b, r.a.rd.row)
		b.Release()
		if err != nil {
			r.a.err = internalBecause(err, "a row of %s has no JSON form", r.a.v.pinned)
			return nil, r.a.err
		}
		r.buf, r.i = rows, 0
	}
	r.i++
	return r.buf[r.i-1], nil
}

func (r *rowStream) Close() error { return r.a.Close() }

// found runs a lookup and refuses the first value the request names that it
// does not find, with the capability's declared error at the value
// (portfolio_not_found, no_valuation). It answers the lookup's query id.
func (p *Provider) found(ctx context.Context, v *view, inv *ports.Invocation, code string, l lookup) (string, error) {
	if len(l.asked) == 0 {
		return "", nil
	}
	res, err := p.run(ctx, v, inv, l.req)
	if err != nil {
		return "", err
	}
	defer res.Batches.Close() // on a path that already fails; the end closes it and says so
	asked := map[string]bool{}
	for _, s := range l.asked {
		asked[s] = true
	}
	seen := map[string]bool{}
	for {
		b, err := res.Batches.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", p.failure(v, "reading "+l.req.Query.Object, err)
		}
		col, ok := b.Column(0).(*array.String)
		if b.NumCols() != 1 || !ok {
			b.Release()
			return "", internal("the source answered a lookup of %s outside its writer schema", l.req.Query.Object)
		}
		for i := 0; i < col.Len(); i++ {
			if col.IsValid(i) && asked[col.Value(i)] {
				seen[col.Value(i)] = true // bounded by what the request asked
			}
		}
		b.Release()
	}
	if err := res.Batches.Close(); err != nil {
		return "", fmt.Errorf("views: %s: closing the read of %s: %w", v.pinned, l.req.Query.Object, err)
	}
	for i, s := range l.asked {
		if !seen[s] {
			return "", v.declared(code).Problem(fmt.Sprintf("the value %q of %s", s, l.member)).WithTarget(fmt.Sprintf("/%s/%d", l.member, i))
		}
	}
	return res.QueryID, nil
}
