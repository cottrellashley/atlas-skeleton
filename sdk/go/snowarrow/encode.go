package snowarrow

import (
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

// Encode writes rows in Snowflake's encoding, as the package comment states
// it. The fake Snowflake and the tests use it; no client does.
//
// Row values by type: fixed takes the unscaled value as int64 or *big.Int;
// real a float64; text a string; boolean a bool; date and timestamps a
// time.Time; nil is null.
func Encode(mem memory.Allocator, rt RowType, rows [][]any) (arrow.RecordBatch, error) {
	fields := make([]arrow.Field, len(rt))
	cols := make([]arrow.Array, len(rt))
	defer func() {
		for _, a := range cols {
			if a != nil {
				a.Release()
			}
		}
	}()
	for j, c := range rt {
		values := make([]any, len(rows))
		for i, r := range rows {
			if len(r) != len(rt) {
				return nil, fmt.Errorf("row %d has %d values, the row type %d", i, len(r), len(rt))
			}
			values[i] = r[j]
		}
		a, err := encodeColumn(mem, c, values)
		if err != nil {
			return nil, fmt.Errorf("column %s: %w", c.Name, err)
		}
		cols[j] = a
		md := arrow.NewMetadata(
			[]string{"logicalType", "precision", "scale"},
			[]string{c.Type, strconv.Itoa(c.Precision), strconv.Itoa(c.Scale)},
		)
		fields[j] = arrow.Field{Name: c.Name, Type: a.DataType(), Nullable: c.Nullable, Metadata: md}
	}
	return array.NewRecordBatch(arrow.NewSchema(fields, nil), cols, int64(len(rows))), nil
}

func encodeColumn(mem memory.Allocator, c Column, values []any) (arrow.Array, error) {
	switch c.Type {
	case Fixed:
		return encodeFixed(mem, c, values)
	case Real:
		b := array.NewFloat64Builder(mem)
		defer b.Release()
		for _, v := range values {
			if v == nil {
				b.AppendNull()
			} else if f, ok := v.(float64); ok {
				b.Append(f)
			} else {
				return nil, fmt.Errorf("%v is not a float64", v)
			}
		}
		return b.NewArray(), nil
	case Text:
		b := array.NewStringBuilder(mem)
		defer b.Release()
		for _, v := range values {
			if v == nil {
				b.AppendNull()
			} else if s, ok := v.(string); ok {
				b.Append(s)
			} else {
				return nil, fmt.Errorf("%v is not a string", v)
			}
		}
		return b.NewArray(), nil
	case Boolean:
		b := array.NewBooleanBuilder(mem)
		defer b.Release()
		for _, v := range values {
			if v == nil {
				b.AppendNull()
			} else if t, ok := v.(bool); ok {
				b.Append(t)
			} else {
				return nil, fmt.Errorf("%v is not a bool", v)
			}
		}
		return b.NewArray(), nil
	case Date:
		b := array.NewDate32Builder(mem)
		defer b.Release()
		for _, v := range values {
			if v == nil {
				b.AppendNull()
			} else if t, ok := v.(time.Time); ok {
				b.Append(arrow.Date32FromTime(t))
			} else {
				return nil, fmt.Errorf("%v is not a time.Time", v)
			}
		}
		return b.NewArray(), nil
	case TimestampNTZ, TimestampLTZ, TimestampTZ:
		return encodeTimestamp(mem, c, values)
	}
	return nil, fmt.Errorf("Snowflake type %q is not in snowflake-arrow/1", c.Type)
}

func unscaledOf(v any) (*big.Int, error) {
	switch x := v.(type) {
	case int64:
		return big.NewInt(x), nil
	case int:
		return big.NewInt(int64(x)), nil
	case *big.Int:
		return x, nil
	}
	return nil, fmt.Errorf("%v (%T) is not an unscaled integer", v, v)
}

// encodeFixed picks the narrowest integer width that holds every value of
// the chunk, as the package comment assumes Snowflake does.
func encodeFixed(mem memory.Allocator, c Column, values []any) (arrow.Array, error) {
	ints := make([]*big.Int, len(values))
	lo, hi := int64(0), int64(0)
	for i, v := range values {
		if v == nil {
			continue
		}
		n, err := unscaledOf(v)
		if err != nil {
			return nil, err
		}
		ints[i] = n
		if c.Precision <= 18 {
			if !n.IsInt64() {
				return nil, fmt.Errorf("%s does not fit NUMBER(%d,%d)", n, c.Precision, c.Scale)
			}
			lo, hi = min(lo, n.Int64()), max(hi, n.Int64())
		}
	}
	if c.Precision > 18 {
		b := array.NewDecimal128Builder(mem, &arrow.Decimal128Type{Precision: int32(c.Precision), Scale: int32(c.Scale)})
		defer b.Release()
		for _, n := range ints {
			if n == nil {
				b.AppendNull()
			} else {
				b.Append(decimal128.FromBigInt(n))
			}
		}
		return b.NewArray(), nil
	}
	var b array.Builder
	appendInt := func(int64) {}
	switch {
	case lo >= math.MinInt8 && hi <= math.MaxInt8:
		x := array.NewInt8Builder(mem)
		b, appendInt = x, func(v int64) { x.Append(int8(v)) }
	case lo >= math.MinInt16 && hi <= math.MaxInt16:
		x := array.NewInt16Builder(mem)
		b, appendInt = x, func(v int64) { x.Append(int16(v)) }
	case lo >= math.MinInt32 && hi <= math.MaxInt32:
		x := array.NewInt32Builder(mem)
		b, appendInt = x, func(v int64) { x.Append(int32(v)) }
	default:
		x := array.NewInt64Builder(mem)
		b, appendInt = x, x.Append
	}
	defer b.Release()
	for _, n := range ints {
		if n == nil {
			b.AppendNull()
		} else {
			appendInt(n.Int64())
		}
	}
	return b.NewArray(), nil
}

func encodeTimestamp(mem memory.Allocator, c Column, values []any) (arrow.Array, error) {
	times := make([]*time.Time, len(values))
	for i, v := range values {
		if v == nil {
			continue
		}
		t, ok := v.(time.Time)
		if !ok {
			return nil, fmt.Errorf("%v is not a time.Time", v)
		}
		times[i] = &t
	}
	scaled := func(t time.Time) int64 {
		return t.Unix()*int64(math.Pow10(c.Scale)) + int64(t.Nanosecond())/int64(math.Pow10(9-c.Scale))
	}
	offset := func(t time.Time) int32 {
		_, secs := t.Zone()
		return int32(secs/60 + 1440)
	}
	if c.Type != TimestampTZ && c.Scale <= 7 {
		b := array.NewInt64Builder(mem)
		defer b.Release()
		for _, t := range times {
			if t == nil {
				b.AppendNull()
			} else {
				b.Append(scaled(*t))
			}
		}
		return b.NewArray(), nil
	}
	var fields []arrow.Field
	wide := c.Scale > 3 || c.Type != TimestampTZ
	fields = append(fields, arrow.Field{Name: "epoch", Type: arrow.PrimitiveTypes.Int64})
	if wide {
		fields = append(fields, arrow.Field{Name: "fraction", Type: arrow.PrimitiveTypes.Int32})
	}
	if c.Type == TimestampTZ {
		fields = append(fields, arrow.Field{Name: "timezone", Type: arrow.PrimitiveTypes.Int32})
	}
	b := array.NewStructBuilder(mem, arrow.StructOf(fields...))
	defer b.Release()
	for _, t := range times {
		if t == nil {
			b.AppendNull()
			continue
		}
		b.Append(true)
		if wide {
			b.FieldBuilder(0).(*array.Int64Builder).Append(t.Unix())
			b.FieldBuilder(1).(*array.Int32Builder).Append(int32(t.Nanosecond()))
		} else {
			b.FieldBuilder(0).(*array.Int64Builder).Append(scaled(*t))
		}
		if c.Type == TimestampTZ {
			b.FieldBuilder(len(fields) - 1).(*array.Int32Builder).Append(offset(*t))
		}
	}
	return b.NewArray(), nil
}
