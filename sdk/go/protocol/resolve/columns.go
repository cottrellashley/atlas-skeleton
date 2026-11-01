package resolve

import (
	"math/big"
	"slices"
	"sync/atomic"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"atlas.local/sdk/go/protocol/arrowschema"
	"atlas.local/sdk/go/protocol/ijson"
	"atlas.local/sdk/go/protocol/schema"
	"atlas.local/sdk/go/protocol/units"
)

// Batch resolves a record batch, written under the plan's writer, column by
// column (§9.5.7): a rename renames a field, a unit conversion is one pass
// over a column, and a column that needs nothing is passed through without
// a copy. Where an operation has no columnar form for a column, that column
// is resolved value by value by the same rules as Record, so the result
// always equals resolving each row (AP-RESOLVE-002). The result carries md
// as its schema's metadata; the caller releases it.
func (p *Plan) Batch(mem memory.Allocator, b arrow.RecordBatch, md *arrow.Metadata) (arrow.RecordBatch, error) {
	if p.identity {
		b.Retain()
		return b, nil
	}
	n := int(b.NumRows())
	var cols []*col
	defer func() {
		for _, c := range cols {
			if c != nil {
				c.release()
			}
		}
	}()
	for i, f := range b.Schema().Fields() {
		ws, ok := p.writer.Property(f.Name)
		if !ok {
			return nil, failf("/"+escape(f.Name), "the writer's schema has no %q", f.Name)
		}
		a := b.Column(i)
		a.Retain()
		cols = append(cols, &col{name: f.Name, arr: a, schema: ws, nullPresent: ws.Nullable()})
	}
	for _, s := range p.steps {
		var err error
		// columns returns the columns as they stand even when it fails, so
		// every array is released.
		if cols, err = s.columns(mem, cols, n); err != nil {
			return nil, err
		}
	}
	fields, err := arrowschema.Fields(p.reader)
	if err != nil {
		return nil, failf("", "the reader's schema has no Arrow form: %v", err)
	}
	out := make([]arrow.Array, 0, len(fields))
	defer func() {
		for _, a := range out {
			a.Release()
		}
	}()
	for _, f := range fields {
		a, err := matchColumn(mem, cols, p.reader, f, n)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return array.NewRecordBatch(arrow.NewSchema(fields, md), out, int64(n)), nil
}

// valueByValue counts the cells resolved one at a time, so a test can show
// that a table needing no conversion is resolved column by column
// (ATLAS-RESOLVE-004).
var valueByValue atomic.Int64

// col is one column while a batch is resolved: an Arrow array, or, once an
// operation has no columnar form for it, its values read as Record reads
// them.
type col struct {
	name        string
	arr         arrow.Array // nil once the column is cells
	cells       []cell
	schema      *schema.Schema // the writer's schema for the column's values
	nullPresent bool           // whether an Arrow null is a member with null, or an absent one (A-021)
}

type cell struct {
	v       ijson.Value
	present bool
}

func (c *col) release() {
	if c.arr != nil {
		c.arr.Release()
		c.arr = nil
	}
}

// at reads row i as Record would see it.
func (c *col) at(i int) (ijson.Value, bool, error) {
	if c.arr == nil {
		return c.cells[i].v, c.cells[i].present, nil
	}
	if c.arr.IsNull(i) {
		return nil, c.nullPresent, nil
	}
	v, _, err := arrowschema.Value(c.arr, i, c.schema)
	if err != nil {
		return nil, false, err
	}
	return v, true, nil
}

// cellsOf turns the column into cells.
func (c *col) cellsOf(n int) error {
	if c.arr == nil {
		return nil
	}
	cells := make([]cell, n)
	for i := range cells {
		v, present, err := c.at(i)
		if err != nil {
			return err
		}
		cells[i] = cell{v, present}
	}
	c.release()
	c.cells = cells
	return nil
}

func find(cols []*col, name string) int {
	return slices.IndexFunc(cols, func(c *col) bool { return c.name == name })
}

func remove(cols []*col, i int) []*col {
	cols[i].release()
	return slices.Delete(cols, i, i+1)
}

// columns applies one lens operation to every row at once.
func (s step) columns(mem memory.Allocator, cols []*col, n int) ([]*col, error) {
	op := s.op
	switch op.Op {
	case "rename":
		from, to := op.from(), op.to()
		if !s.forward {
			from, to = to, from
		}
		i := find(cols, from)
		if i < 0 {
			return cols, nil
		}
		if j := find(cols, to); j >= 0 {
			// Both are there: row by row, from moves only where it is present.
			src, dst := cols[i], cols[j]
			if err := src.cellsOf(n); err != nil {
				return cols, err
			}
			if err := dst.cellsOf(n); err != nil {
				return cols, err
			}
			for r := range n {
				if src.cells[r].present {
					dst.cells[r] = src.cells[r]
				}
			}
			return remove(cols, i), nil
		}
		cols[i].name = to
		return cols, nil
	case "default":
		i := find(cols, op.Field)
		if !s.forward {
			if i >= 0 {
				cols = remove(cols, i)
			}
			return cols, nil
		}
		if i < 0 {
			cells := make([]cell, n)
			for r := range cells {
				cells[r] = cell{op.Value, true}
			}
			return append(cols, &col{name: op.Field, cells: cells}), nil
		}
		c := cols[i]
		if c.arr != nil && (c.arr.NullN() == 0 || c.nullPresent) {
			return cols, nil // present on every row
		}
		if err := c.cellsOf(n); err != nil {
			return cols, err
		}
		for r := range c.cells {
			if !c.cells[r].present {
				c.cells[r] = cell{op.Value, true}
			}
		}
		return cols, nil
	case "drop":
		if i := find(cols, op.Field); s.forward && i >= 0 {
			cols = remove(cols, i)
		}
		return cols, nil
	}
	i := find(cols, op.Field)
	if i < 0 {
		return cols, nil
	}
	c := cols[i]
	if done, err := s.vectorised(mem, c); done || err != nil {
		return cols, err
	}
	if err := c.cellsOf(n); err != nil {
		return cols, err
	}
	for r := range c.cells {
		if !c.cells[r].present {
			continue
		}
		v, err := op.value(c.cells[r].v, s.forward, "/"+escape(op.Field), s.into)
		if err != nil {
			return cols, err
		}
		c.cells[r].v = v
	}
	return cols, nil
}

// vectorised applies a value operation to a whole Arrow column, where it
// has a columnar form that gives exactly what op.value gives on each value.
func (s step) vectorised(mem memory.Allocator, c *col) (bool, error) {
	if c.arr == nil {
		return false, nil
	}
	op := s.op
	from, to := op.From, op.To
	if !s.forward {
		from, to = to, from
	}
	ptr := "/" + escape(op.Field)
	switch a := c.arr.(type) {
	case *array.Float64:
		if op.Op != "convert_unit" {
			return false, nil
		}
		f, t := units.Unit(from.(string)), units.Unit(to.(string))
		b := array.NewFloat64Builder(mem)
		defer b.Release()
		for i := range a.Len() {
			if a.IsNull(i) {
				b.AppendNull()
				continue
			}
			v, err := units.Float(a.Value(i), f, t)
			if err != nil {
				return false, failf(ptr, "%v", err)
			}
			b.Append(v)
		}
		c.replace(b.NewArray())
		return true, nil
	case *array.Decimal128:
		if op.Op != "convert_unit" {
			return false, nil
		}
		scale := decimalScale(s.into, op.Field)
		if scale < 0 {
			if a.NullN() < a.Len() {
				return false, failf(ptr, "a decimal converted into a profile with no Decimal(p,s) to round to")
			}
			return true, nil // only nulls, which pass through
		}
		num, den, _ := units.Factor(units.Unit(from.(string)), units.Unit(to.(string)))
		src := a.DataType().(*arrow.Decimal128Type).Scale
		f := big.NewRat(num, den)
		f.Mul(f, new(big.Rat).SetFrac(big.NewInt(1), new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(src)), nil)))
		b := array.NewDecimal128Builder(mem, &arrow.Decimal128Type{Precision: 38, Scale: int32(scale)})
		defer b.Release()
		limit := new(big.Int).Exp(big.NewInt(10), big.NewInt(38), nil)
		for i := range a.Len() {
			if a.IsNull(i) {
				b.AppendNull()
				continue
			}
			u := units.RoundHalfEven(new(big.Rat).Mul(new(big.Rat).SetInt(a.Value(i).BigInt()), f), scale)
			if new(big.Int).Abs(u).Cmp(limit) >= 0 {
				return false, failf(ptr, "the converted decimal does not fit 38 digits")
			}
			b.Append(decimal128.FromBigInt(u))
		}
		c.replace(b.NewArray())
		return true, nil
	case *array.Int64:
		if op.Op != "promote" || from != "integer" || to != "number" || c.schema.Format() == "int64" {
			return false, nil
		}
		b := array.NewFloat64Builder(mem)
		defer b.Release()
		for i := range a.Len() {
			if a.IsNull(i) {
				b.AppendNull()
			} else {
				b.Append(float64(a.Value(i)))
			}
		}
		c.replace(b.NewArray())
		return true, nil
	case *array.Date32:
		if op.Op != "promote" || from != "date" || to != "date-time" {
			return false, nil
		}
		b := array.NewTimestampBuilder(mem, arrowschema.Timestamp)
		defer b.Release()
		for i := range a.Len() {
			if a.IsNull(i) {
				b.AppendNull()
			} else {
				b.Append(arrow.Timestamp(int64(a.Value(i)) * 86400 * 1_000_000))
			}
		}
		c.replace(b.NewArray())
		return true, nil
	case *array.String:
		if op.Op != "alias_value" {
			return false, nil
		}
		f, fok := from.(string)
		t, tok := to.(string)
		if !fok || !tok {
			return false, nil
		}
		b := array.NewStringBuilder(mem)
		defer b.Release()
		for i := range a.Len() {
			switch {
			case a.IsNull(i):
				b.AppendNull()
			case a.Value(i) == f:
				b.Append(t)
			default:
				b.Append(a.Value(i))
			}
		}
		c.replace(b.NewArray())
		return true, nil
	}
	return false, nil
}

func (c *col) replace(a arrow.Array) {
	c.arr.Release()
	c.arr = a
}

// matchColumn is steps 5 to 13 for one property of the reader, on every row.
func matchColumn(mem memory.Allocator, cols []*col, s *schema.Schema, f arrow.Field, n int) (arrow.Array, error) {
	name := f.Name
	ps, _ := s.Property(name)
	required := s.Required(name)
	ptr := "/" + escape(name)
	var cands []*col
	if i := find(cols, name); i >= 0 {
		cands = append(cands, cols[i])
	}
	aliases := ps.Aliases()
	for k := len(aliases) - 1; k >= 0; k-- {
		if i := find(cols, aliases[k]); i >= 0 {
			cands = append(cands, cols[i])
		}
	}
	if len(cands) == 1 {
		if a, ok, err := passThrough(mem, cands[0], ps, required, f); ok || err != nil {
			return a, err
		}
	}
	b := array.NewBuilder(mem, f.Type)
	defer b.Release()
	_, hasDefault := ps.Default()
	valueByValue.Add(int64(n))
	for r := range n {
		var v ijson.Value
		present := false
		for _, c := range cands {
			var err error
			if v, present, err = c.at(r); err != nil {
				return nil, failf(ptr, "%v", err)
			}
			if present {
				break
			}
		}
		if !present && hasDefault {
			v, _ = ps.Default()
			present = true
		}
		if !present && ps.Nullable() && required {
			v, present = nil, true
		}
		if !present {
			if !required {
				b.AppendNull() // absent
				continue
			}
			return nil, failf(ptr, "%q is required, and the record has no value for it", name)
		}
		cv, err := convert(v, ps, ptr)
		if err != nil {
			return nil, err
		}
		if err := arrowschema.Append(b, f.Type, ps, cv); err != nil {
			return nil, failf(ptr, "%v", err)
		}
	}
	return b.NewArray(), nil
}

// passThrough serves a column that needs nothing: the same Arrow type, every
// value valid for the reader as it stands, and its nulls meaning what the
// reader would make of them. ok is false where the column must be resolved
// value by value.
func passThrough(mem memory.Allocator, c *col, ps *schema.Schema, required bool, f arrow.Field) (arrow.Array, bool, error) {
	if c.arr == nil {
		return nil, false, nil
	}
	// A writer's column is valid against its own schema; the reader
	// validates what resolution gives it (§9.3 rule 1). So a column whose
	// two schemas admit the same values needs no look at its values.
	if !arrow.TypeEqual(c.arr.DataType(), f.Type) || !schema.SameValues(c.schema, ps) && !unconstrained(ps, f.Type) {
		return nil, false, nil
	}
	if c.arr.NullN() > 0 {
		_, hasDefault := ps.Default()
		ok := c.nullPresent && ps.Nullable() || !c.nullPresent && !hasDefault && (!required || ps.Nullable())
		if !ok {
			return nil, false, nil
		}
	}
	c.arr.Retain()
	return c.arr, true, nil
}

// unconstrained reports whether every value an Arrow type holds is valid
// against the reader's property: no enum, pattern, bound or format the
// type does not already guarantee.
func unconstrained(ps *schema.Schema, t arrow.DataType) bool {
	src, _ := ps.Source().(*ijson.Object)
	if src == nil || ps.Enum() != nil {
		return false
	}
	for _, k := range []string{"enum", "const", "pattern", "minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf", "minLength", "maxLength", "$ref"} {
		if _, ok := src.Get(k); ok {
			return false
		}
	}
	name, _, _, logical := ps.Logical()
	switch t.ID() {
	case arrow.BOOL, arrow.FLOAT64, arrow.DATE32, arrow.TIMESTAMP, arrow.DURATION:
		return !logical || name == "Bp" || name == "Percent" || name == "Fraction" || name == "Years"
	case arrow.INT64:
		return !logical && ps.Type() == "integer"
	case arrow.DECIMAL128:
		return name == "Decimal"
	case arrow.STRING:
		return !logical && ps.Format() == ""
	}
	return false
}
