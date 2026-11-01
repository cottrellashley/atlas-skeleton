package snowarrow

import (
	"fmt"
	"math"
	"math/big"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"
)

// Decode turns one chunk batch in Snowflake's encoding into the writer's
// Arrow schema. The planned SQL already aliases and casts every column to the
// contract (§2.2.1.5), so this only undoes Snowflake's encoding; a column
// that does not match the writer schema is an error, which the caller reports
// as invalid_document. The caller releases the result.
func Decode(mem memory.Allocator, rt RowType, rec arrow.RecordBatch, target *arrow.Schema) (arrow.RecordBatch, error) {
	if int(rec.NumCols()) != len(rt) {
		return nil, fmt.Errorf("chunk has %d columns, its row type %d", rec.NumCols(), len(rt))
	}
	out := make([]arrow.Array, target.NumFields())
	defer func() {
		for _, a := range out {
			if a != nil {
				a.Release()
			}
		}
	}()
	for i, f := range target.Fields() {
		j := -1
		for k, c := range rt {
			if c.Name == f.Name {
				j = k
				break
			}
		}
		if j < 0 {
			return nil, fmt.Errorf("the result has no column %s", f.Name)
		}
		a, err := decodeColumn(mem, rt[j], rec.Column(j), f.Type)
		if err != nil {
			return nil, fmt.Errorf("column %s: %w", f.Name, err)
		}
		out[i] = a
	}
	return array.NewRecordBatch(target, out, rec.NumRows()), nil
}

func decodeColumn(mem memory.Allocator, col Column, src arrow.Array, want arrow.DataType) (arrow.Array, error) {
	switch col.Type {
	case Fixed:
		return decodeFixed(mem, col, src, want)
	case Real, Text, Boolean, Date:
		if !arrow.TypeEqual(src.DataType(), want) {
			return nil, fmt.Errorf("Snowflake %s arrived as %s; the writer wants %s", col.Type, src.DataType(), want)
		}
		src.Retain()
		return src, nil
	case TimestampNTZ, TimestampLTZ, TimestampTZ:
		ts, ok := want.(*arrow.TimestampType)
		if !ok || ts.Unit != arrow.Microsecond {
			return nil, fmt.Errorf("Snowflake %s cannot be read as %s", col.Type, want)
		}
		return decodeTimestamp(mem, col, src, ts)
	}
	return nil, fmt.Errorf("Snowflake type %q is not in snowflake-arrow/1", col.Type)
}

// scaledValue reads a fixed column's value as its unscaled integer.
func scaledValue(src arrow.Array, i int) (*big.Int, error) {
	switch a := src.(type) {
	case *array.Int8:
		return big.NewInt(int64(a.Value(i))), nil
	case *array.Int16:
		return big.NewInt(int64(a.Value(i))), nil
	case *array.Int32:
		return big.NewInt(int64(a.Value(i))), nil
	case *array.Int64:
		return big.NewInt(a.Value(i)), nil
	case *array.Decimal128:
		return a.Value(i).BigInt(), nil
	}
	return nil, fmt.Errorf("a fixed column arrived as %s", src.DataType())
}

func decodeFixed(mem memory.Allocator, col Column, src arrow.Array, want arrow.DataType) (arrow.Array, error) {
	n := src.Len()
	switch w := want.(type) {
	case *arrow.Int64Type:
		if col.Scale != 0 {
			return nil, fmt.Errorf("NUMBER(%d,%d) cannot be read as an integer; the plan must cast it", col.Precision, col.Scale)
		}
		b := array.NewInt64Builder(mem)
		defer b.Release()
		b.Reserve(n)
		for i := range n {
			if src.IsNull(i) {
				b.AppendNull()
				continue
			}
			v, err := scaledValue(src, i)
			if err != nil {
				return nil, err
			}
			if !v.IsInt64() {
				return nil, fmt.Errorf("%s does not fit an integer", v)
			}
			b.Append(v.Int64())
		}
		return b.NewArray(), nil
	case *arrow.Decimal128Type:
		if int(w.Scale) < col.Scale {
			return nil, fmt.Errorf("NUMBER(%d,%d) cannot be read at the smaller scale %d", col.Precision, col.Scale, w.Scale)
		}
		factor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(int(w.Scale)-col.Scale)), nil)
		b := array.NewDecimal128Builder(mem, w)
		defer b.Release()
		b.Reserve(n)
		for i := range n {
			if src.IsNull(i) {
				b.AppendNull()
				continue
			}
			v, err := scaledValue(src, i)
			if err != nil {
				return nil, err
			}
			b.Append(decimal128.FromBigInt(v.Mul(v, factor)))
		}
		return b.NewArray(), nil
	case *arrow.Float64Type:
		// A decimal the profile declares as a number with a unit (§2.2.1.6):
		// one division by 10^scale, rounded to nearest.
		div := math.Pow10(col.Scale)
		b := array.NewFloat64Builder(mem)
		defer b.Release()
		b.Reserve(n)
		for i := range n {
			if src.IsNull(i) {
				b.AppendNull()
				continue
			}
			v, err := scaledValue(src, i)
			if err != nil {
				return nil, err
			}
			f, _ := new(big.Float).SetInt(v).Float64()
			b.Append(f / div)
		}
		return b.NewArray(), nil
	}
	return nil, fmt.Errorf("NUMBER cannot be read as %s", want)
}

func decodeTimestamp(mem memory.Allocator, col Column, src arrow.Array, want *arrow.TimestampType) (arrow.Array, error) {
	n := src.Len()
	b := array.NewTimestampBuilder(mem, want)
	defer b.Release()
	b.Reserve(n)
	switch a := src.(type) {
	case *array.Int64: // epoch times 10^scale
		for i := range n {
			if a.IsNull(i) {
				b.AppendNull()
				continue
			}
			b.Append(arrow.Timestamp(toMicros(a.Value(i), col.Scale)))
		}
	case *array.Struct:
		st := a.DataType().(*arrow.StructType)
		epochIdx, ok := st.FieldIdx("epoch")
		if !ok {
			return nil, fmt.Errorf("a timestamp struct has no epoch")
		}
		epoch, ok := a.Field(epochIdx).(*array.Int64)
		if !ok {
			return nil, fmt.Errorf("a timestamp epoch arrived as %s", a.Field(epochIdx).DataType())
		}
		var fraction *array.Int32
		if fi, ok := st.FieldIdx("fraction"); ok {
			if fraction, ok = a.Field(fi).(*array.Int32); !ok {
				return nil, fmt.Errorf("a timestamp fraction arrived as %s", a.Field(fi).DataType())
			}
		}
		for i := range n {
			if a.IsNull(i) {
				b.AppendNull()
				continue
			}
			if fraction != nil { // epoch in seconds, fraction in nanoseconds
				b.Append(arrow.Timestamp(epoch.Value(i)*1_000_000 + int64(fraction.Value(i))/1000))
			} else { // epoch scaled; the timezone does not change the instant
				b.Append(arrow.Timestamp(toMicros(epoch.Value(i), col.Scale)))
			}
		}
	default:
		return nil, fmt.Errorf("a timestamp arrived as %s", src.DataType())
	}
	return b.NewArray(), nil
}

// toMicros converts epoch times 10^scale to microseconds, truncating any finer
// fraction towards the earlier instant (§9.1.4).
func toMicros(v int64, scale int) int64 {
	switch {
	case scale == 6:
		return v
	case scale < 6:
		return v * int64(math.Pow10(6-scale))
	}
	d := int64(math.Pow10(scale - 6))
	q := v / d
	if v%d != 0 && v < 0 {
		q--
	}
	return q
}
