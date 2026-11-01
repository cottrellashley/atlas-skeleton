package gosf

import (
	"fmt"
	"unicode/utf8"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// convert is one record of Snowflake's answer, in the encodings
// gosnowflake leaves with higher precision and microsecond timestamps, as
// a batch of the statement's columns (schema). Everything is exact: a
// NUMBER arrives as the smallest integer type that holds the batch's
// unscaled values, or as a Decimal128, with its scale in the row type, and
// is rescaled to the writer's decimal without rounding; a timestamp
// arrives as microseconds since the epoch of the instant (TIMESTAMP_TZ,
// TIMESTAMP_LTZ) or of the UTC wall-clock time the statement converted it
// to (TIMESTAMP_NTZ), and is labelled UTC. What is already the writer's
// type is passed on without a copy.
func convert(mem memory.Allocator, schema *arrow.Schema, cols []column, rec arrow.RecordBatch) (arrow.RecordBatch, error) {
	if int(rec.NumCols()) != len(cols) {
		return nil, fmt.Errorf("a record of %d columns for %d", rec.NumCols(), len(cols))
	}
	out := make([]arrow.Array, len(cols))
	defer func() {
		for _, a := range out {
			if a != nil {
				a.Release()
			}
		}
	}()
	for i, c := range cols {
		a, err := cast(mem, c, rec.Column(i))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", c.out.Name, err)
		}
		out[i] = a
	}
	return array.NewRecordBatch(schema, out, rec.NumRows()), nil
}

// cast is one column as its output's type.
func cast(mem memory.Allocator, c column, a arrow.Array) (arrow.Array, error) {
	bad := fmt.Errorf("a %s %s is not a %s", c.typ, a.DataType(), c.out.Type)
	switch t := c.out.Type.(type) {
	case *arrow.StringType:
		s, ok := a.(*array.String)
		if !ok {
			return nil, bad
		}
		if !utf8.Valid(s.ValueBytes()) {
			return nil, fmt.Errorf("text that is not UTF-8")
		}
		a.Retain()
		return a, nil
	case *arrow.Float64Type, *arrow.BooleanType, *arrow.Date32Type:
		if !arrow.TypeEqual(a.DataType(), t) {
			return nil, bad
		}
		a.Retain()
		return a, nil
	case *arrow.Int64Type:
		return integers(mem, a, bad)
	case *arrow.Decimal128Type:
		return decimals(mem, a, int32(c.scale), t, bad)
	case *arrow.TimestampType:
		ts, ok := a.DataType().(*arrow.TimestampType)
		if !ok || ts.Unit != arrow.Microsecond || t.Unit != arrow.Microsecond || t.TimeZone != "UTC" {
			return nil, bad
		}
		if c.typ == "TIMESTAMP_NTZ" && !c.out.WallUTC {
			return nil, fmt.Errorf("a TIMESTAMP_NTZ in no declared zone is never read as UTC")
		}
		// The same microseconds, labelled UTC: an instant either way.
		d := array.NewData(t, a.Len(), a.Data().Buffers(), nil, a.NullN(), a.Data().Offset())
		defer d.Release()
		return array.MakeFromData(d), nil
	}
	return nil, bad
}

// integers widens a NUMBER(p, 0) to Int64.
func integers(mem memory.Allocator, a arrow.Array, bad error) (arrow.Array, error) {
	if a.DataType().ID() == arrow.INT64 {
		a.Retain()
		return a, nil
	}
	b := array.NewInt64Builder(mem)
	defer b.Release()
	b.Reserve(a.Len())
	for i := range a.Len() {
		if a.IsNull(i) {
			b.AppendNull()
			continue
		}
		switch x := a.(type) {
		case *array.Int8:
			b.Append(int64(x.Value(i)))
		case *array.Int16:
			b.Append(int64(x.Value(i)))
		case *array.Int32:
			b.Append(int64(x.Value(i)))
		case *array.Decimal128:
			n := x.Value(i).BigInt()
			if x.DataType().(*arrow.Decimal128Type).Scale != 0 || !n.IsInt64() {
				return nil, fmt.Errorf("%s is not an int64", x.ValueStr(i))
			}
			b.Append(n.Int64())
		default:
			return nil, bad
		}
	}
	return b.NewArray(), nil
}

// decimals writes a NUMBER at the writer's scale: an unscaled integer of
// any width, or a Decimal128, at scale from, multiplied up to the writer's
// scale or divided down only when no digit is lost, and refused when it
// does not fit the writer's precision. The statement casts every decimal
// to the writer's type, so a value that needs rounding here is a fault.
func decimals(mem memory.Allocator, a arrow.Array, from int32, t *arrow.Decimal128Type, bad error) (arrow.Array, error) {
	if d, ok := a.DataType().(*arrow.Decimal128Type); ok {
		if arrow.TypeEqual(d, t) {
			a.Retain()
			return a, nil
		}
		from = d.Scale
	}
	b := array.NewDecimal128Builder(mem, t)
	defer b.Release()
	b.Reserve(a.Len())
	for i := range a.Len() {
		if a.IsNull(i) {
			b.AppendNull()
			continue
		}
		var n decimal128.Num
		switch x := a.(type) {
		case *array.Int8:
			n = decimal128.FromI64(int64(x.Value(i)))
		case *array.Int16:
			n = decimal128.FromI64(int64(x.Value(i)))
		case *array.Int32:
			n = decimal128.FromI64(int64(x.Value(i)))
		case *array.Int64:
			n = decimal128.FromI64(x.Value(i))
		case *array.Decimal128:
			n = x.Value(i)
		default:
			return nil, bad
		}
		v, err := rescale(n, from, t)
		if err != nil {
			return nil, err
		}
		b.Append(v)
	}
	return b.NewArray(), nil
}

// rescale is n, unscaled at scale from, unscaled at t's scale, exactly.
func rescale(n decimal128.Num, from int32, t *arrow.Decimal128Type) (decimal128.Num, error) {
	if from != t.Scale {
		v, err := n.Rescale(from, t.Scale)
		if err != nil {
			return decimal128.Num{}, fmt.Errorf("%s at scale %d does not fit scale %d exactly: %w", n.ToString(from), from, t.Scale, err)
		}
		n = v
	}
	if !n.FitsInPrecision(t.Precision) {
		return decimal128.Num{}, fmt.Errorf("%s is past Decimal(%d,%d)", n.ToString(t.Scale), t.Precision, t.Scale)
	}
	return n, nil
}
