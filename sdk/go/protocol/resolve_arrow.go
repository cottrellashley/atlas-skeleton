package protocol

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// ResolveBatch resolves a record batch written under the chain's writer to
// the reader's table, column by column (§9.5.7): a rename renames a field, a
// unit conversion is one pass over a column. It gives the same answer as
// ResolveRow on every row. The caller releases the result.
func ResolveBatch(mem memory.Allocator, c Chain, rec arrow.RecordBatch, reader *Table, readerSchema *arrow.Schema) (arrow.RecordBatch, error) {
	n := rec.NumRows()
	cols := make([]namedArray, rec.NumCols())
	for i := range cols {
		a := rec.Column(i)
		a.Retain()
		cols[i] = namedArray{rec.ColumnName(i), a}
	}
	defer func() {
		for _, c := range cols {
			c.arr.Release()
		}
	}()
	if !c.Identity() {
		for _, s := range c.steps(SchemaResult) {
			var err error
			if cols, err = applyToColumns(mem, s, cols, n); err != nil {
				return nil, err
			}
		}
	}
	out := make([]arrow.Array, len(reader.Columns))
	defer func() {
		for _, a := range out {
			if a != nil {
				a.Release()
			}
		}
	}()
	for i, col := range reader.Columns {
		want := readerSchema.Field(i).Type
		src := findColumn(cols, col.Name, col.Aliases)
		if src == nil {
			if !col.Nullable {
				return nil, resolutionFailed("the writer's batch has no column for %s", col.Name).WithTarget("/" + col.Name)
			}
			out[i] = array.MakeArrayOfNull(mem, want, int(n))
			continue
		}
		converted, err := readerArray(mem, src, col, want)
		if err != nil {
			return nil, err
		}
		out[i] = converted
	}
	return array.NewRecordBatch(readerSchema, out, n), nil
}

type namedArray struct {
	name string
	arr  arrow.Array
}

func findColumn(cols []namedArray, name string, aliases []string) arrow.Array {
	for _, c := range cols {
		if c.name == name {
			return c.arr
		}
	}
	for i := len(aliases) - 1; i >= 0; i-- {
		for _, c := range cols {
			if c.name == aliases[i] {
				return c.arr
			}
		}
	}
	return nil
}

func indexOf(cols []namedArray, name string) int {
	for i, c := range cols {
		if c.name == name {
			return i
		}
	}
	return -1
}

func applyToColumns(mem memory.Allocator, s step, cols []namedArray, n int64) ([]namedArray, error) {
	op := s.op
	from, to := op.From, op.To
	if !s.forward {
		from, to = to, from
	}
	replace := func(i int, a arrow.Array) {
		cols[i].arr.Release()
		cols[i].arr = a
	}
	switch op.Op {
	case LensRename:
		if i := indexOf(cols, from); i >= 0 {
			cols[i].name = to
		}
	case LensAliasValue:
		i := indexOf(cols, op.Field)
		if i < 0 {
			return cols, nil
		}
		strs, ok := cols[i].arr.(*array.String)
		if !ok {
			return nil, resolutionFailed("alias_value on %s needs a string column", op.Field)
		}
		replace(i, mapStrings(mem, strs, func(v string) (string, error) {
			if v == from {
				return to, nil
			}
			return v, nil
		}))
	case LensConvertUnit:
		i := indexOf(cols, op.Field)
		if i < 0 {
			return cols, nil
		}
		conv, err := unitConversion(from, to)
		if err != nil {
			return nil, resolutionFailed("%v", err).WithTarget("/" + op.Field)
		}
		converted, err := convertArray(mem, cols[i].arr, conv)
		if err != nil {
			return nil, resolutionFailed("%s: %v", op.Field, err).WithTarget("/" + op.Field)
		}
		replace(i, converted)
	case LensPromote:
		i := indexOf(cols, op.Field)
		if i < 0 {
			return cols, nil
		}
		promoted, err := promoteArray(mem, cols[i].arr, s.forward)
		if err != nil {
			return nil, resolutionFailed("%s: %v", op.Field, err).WithTarget("/" + op.Field)
		}
		replace(i, promoted)
	case LensDefault:
		i := indexOf(cols, op.Field)
		if !s.forward {
			if i >= 0 {
				cols[i].arr.Release()
				cols = append(cols[:i], cols[i+1:]...)
			}
			return cols, nil
		}
		if i >= 0 {
			return cols, nil
		}
		col, ok := s.at.Result.Column(op.Field)
		if !ok {
			return nil, resolutionFailed("default for %s names no column of %s", op.Field, s.at.Pinned())
		}
		a, err := constantArray(mem, col, op.Value, int(n))
		if err != nil {
			return nil, resolutionFailed("default for %s: %v", op.Field, err)
		}
		cols = append(cols, namedArray{op.Field, a})
	case LensDrop:
		if i := indexOf(cols, op.Field); s.forward && i >= 0 {
			cols[i].arr.Release()
			cols = append(cols[:i], cols[i+1:]...)
		}
	default:
		return nil, resolutionFailed("lens operation %q is not one this client implements", op.Op)
	}
	return cols, nil
}

// readerArray converts a column to the reader's type (§9.5.5): the same
// array when nothing differs, UNKNOWN for enum values the reader does not
// know, integer to number, and a decimal to a larger scale.
func readerArray(mem memory.Allocator, src arrow.Array, col Column, want arrow.DataType) (arrow.Array, error) {
	if strs, ok := src.(*array.String); ok && len(col.Enum) > 0 && arrow.TypeEqual(src.DataType(), want) {
		return mapStrings(mem, strs, func(v string) (string, error) {
			if contains(col.Enum, v) {
				return v, nil
			}
			if contains(col.Enum, Unknown) {
				return Unknown, nil
			}
			return "", resolutionFailed("%s: %q is not a value the reader knows", col.Name, v).WithTarget("/" + col.Name)
		}), nil
	}
	if arrow.TypeEqual(src.DataType(), want) {
		src.Retain()
		return src, nil
	}
	switch s := src.(type) {
	case *array.Int64:
		if want.ID() == arrow.FLOAT64 {
			return promoteArray(mem, s, true)
		}
	case *array.Decimal128:
		if w, ok := want.(*arrow.Decimal128Type); ok {
			return rescaleDecimals(mem, s, w)
		}
	}
	return nil, resolutionFailed("%s: the writer's %s cannot be read as the reader's %s", col.Name, src.DataType(), want).WithTarget("/" + col.Name)
}

func mapStrings(mem memory.Allocator, a *array.String, f func(string) (string, error)) arrow.Array {
	b := array.NewStringBuilder(mem)
	defer b.Release()
	b.Reserve(a.Len())
	for i := range a.Len() {
		if a.IsNull(i) {
			b.AppendNull()
			continue
		}
		v, _ := f(a.Value(i))
		b.Append(v)
	}
	return b.NewArray()
}

func convertArray(mem memory.Allocator, a arrow.Array, conv conversion) (arrow.Array, error) {
	switch x := a.(type) {
	case *array.Float64:
		b := array.NewFloat64Builder(mem)
		defer b.Release()
		b.Reserve(x.Len())
		for i := range x.Len() {
			if x.IsNull(i) {
				b.AppendNull()
				continue
			}
			b.Append(conv.float(x.Value(i)))
		}
		return b.NewArray(), nil
	case *array.Decimal128:
		dt := x.DataType().(*arrow.Decimal128Type)
		b := array.NewDecimal128Builder(mem, dt)
		defer b.Release()
		b.Reserve(x.Len())
		for i := range x.Len() {
			if x.IsNull(i) {
				b.AppendNull()
				continue
			}
			b.Append(decimal128.FromBigInt(conv.unscaled(x.Value(i).BigInt())))
		}
		return b.NewArray(), nil
	}
	return nil, fmt.Errorf("unit conversion needs a number or decimal column, not %s", a.DataType())
}

func promoteArray(mem memory.Allocator, a arrow.Array, forward bool) (arrow.Array, error) {
	if forward {
		x, ok := a.(*array.Int64)
		if !ok {
			a.Retain()
			return a, nil
		}
		b := array.NewFloat64Builder(mem)
		defer b.Release()
		for i := range x.Len() {
			if x.IsNull(i) {
				b.AppendNull()
			} else {
				b.Append(float64(x.Value(i)))
			}
		}
		return b.NewArray(), nil
	}
	x, ok := a.(*array.Float64)
	if !ok {
		a.Retain()
		return a, nil
	}
	b := array.NewInt64Builder(mem)
	defer b.Release()
	for i := range x.Len() {
		if x.IsNull(i) {
			b.AppendNull()
			continue
		}
		v := x.Value(i)
		if v != math.Trunc(v) || math.Abs(v) > 1<<53-1 {
			return nil, fmt.Errorf("%v is not an exact integer", v)
		}
		b.Append(int64(v))
	}
	return b.NewArray(), nil
}

func rescaleDecimals(mem memory.Allocator, a *array.Decimal128, want *arrow.Decimal128Type) (arrow.Array, error) {
	have := a.DataType().(*arrow.Decimal128Type)
	if want.Scale < have.Scale {
		return nil, fmt.Errorf("decimal scale %d cannot be read at the smaller scale %d without rounding", have.Scale, want.Scale)
	}
	factor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(want.Scale-have.Scale)), nil)
	b := array.NewDecimal128Builder(mem, want)
	defer b.Release()
	for i := range a.Len() {
		if a.IsNull(i) {
			b.AppendNull()
			continue
		}
		b.Append(decimal128.FromBigInt(new(big.Int).Mul(a.Value(i).BigInt(), factor)))
	}
	return b.NewArray(), nil
}

// constantArray builds a column of n copies of a JSON value, typed by col.
func constantArray(mem memory.Allocator, col Column, raw json.RawMessage, n int) (arrow.Array, error) {
	dt, err := ArrowType(col)
	if err != nil {
		return nil, err
	}
	v, err := decodeValue(raw)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return array.MakeArrayOfNull(mem, dt, n), nil
	}
	b := array.NewBuilder(mem, dt)
	defer b.Release()
	for range n {
		if err := appendJSON(b, col, v); err != nil {
			return nil, err
		}
	}
	return b.NewArray(), nil
}

// appendJSON appends one JSON value to a builder of the column's Arrow type.
func appendJSON(b array.Builder, col Column, v any) error {
	if v == nil {
		b.AppendNull()
		return nil
	}
	bad := fmt.Errorf("%v is not a %s", v, col.Type)
	switch bb := b.(type) {
	case *array.StringBuilder:
		s, ok := v.(string)
		if !ok {
			return bad
		}
		bb.Append(s)
	case *array.Int64Builder:
		n, ok := v.(json.Number)
		i, err := strconv.ParseInt(n.String(), 10, 64)
		if !ok || err != nil {
			return bad
		}
		bb.Append(i)
	case *array.Float64Builder:
		n, ok := v.(json.Number)
		f, err := n.Float64()
		if !ok || err != nil {
			return bad
		}
		bb.Append(f)
	case *array.BooleanBuilder:
		t, ok := v.(bool)
		if !ok {
			return bad
		}
		bb.Append(t)
	case *array.Date32Builder:
		s, ok := v.(string)
		t, err := time.Parse(time.DateOnly, s)
		if !ok || err != nil {
			return bad
		}
		bb.Append(arrow.Date32FromTime(t))
	case *array.TimestampBuilder:
		s, ok := v.(string)
		t, err := time.Parse(time.RFC3339Nano, s)
		if !ok || err != nil {
			return bad
		}
		bb.Append(arrow.Timestamp(t.UTC().UnixMicro()))
	case *array.Decimal128Builder:
		s, ok := v.(string)
		if !ok {
			return bad
		}
		unscaled, scale, err := parseDecimal(s)
		if err != nil || scale > col.Scale {
			return bad
		}
		unscaled.Mul(unscaled, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(col.Scale-scale)), nil))
		bb.Append(decimal128.FromBigInt(unscaled))
	default:
		return fmt.Errorf("no builder for %s", col.Type)
	}
	return nil
}
