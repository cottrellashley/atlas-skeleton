package arrowschema

import (
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/decimal128"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"atlas.local/sdk/go/protocol/grammar"
	"atlas.local/sdk/go/protocol/ijson"
	"atlas.local/sdk/go/protocol/schema"
)

// Arrow has no absent value: a member that is absent or null is an Arrow
// null, and an Arrow null reads back as null where the property allows null
// and as absent where it does not (assumption A-021).

// ToBatch writes rows, each a record of the row schema, as one record batch
// of the Arrow schema derived from it. The caller releases the batch.
func ToBatch(mem memory.Allocator, sch *arrow.Schema, row *schema.Schema, rows []*ijson.Object) (arrow.RecordBatch, error) {
	b := array.NewRecordBuilder(mem, sch)
	defer b.Release()
	for i, r := range rows {
		for j, f := range sch.Fields() {
			p, ok := row.Property(f.Name)
			if !ok {
				return nil, fmt.Errorf("the row schema has no %q", f.Name)
			}
			v, _ := r.Get(f.Name)
			if err := Append(b.Field(j), f.Type, p, v); err != nil {
				return nil, fmt.Errorf("row %d, %s: %w", i, f.Name, err)
			}
		}
	}
	return b.NewRecordBatch(), nil
}

// Append writes one JSON value of its property to a builder of the
// property's Arrow type.
func Append(b array.Builder, t arrow.DataType, s *schema.Schema, v ijson.Value) error {
	if v == nil {
		b.AppendNull()
		return nil
	}
	bad := func() error { return fmt.Errorf("%T is not a value of %s", v, t) }
	switch bb := b.(type) {
	case *array.BooleanBuilder:
		x, ok := v.(bool)
		if !ok {
			return bad()
		}
		bb.Append(x)
	case *array.Int64Builder:
		var n int64
		switch x := v.(type) {
		case ijson.Number:
			i, ok := x.Int64()
			if !ok || !x.SafeInteger() {
				return fmt.Errorf("%s is not an integer of I-JSON", x)
			}
			n = i
		case string:
			i, err := grammar.ParseIntegerString(x)
			if err != nil || !i.IsInt64() {
				return fmt.Errorf("%q is not an int64", x)
			}
			n = i.Int64()
		default:
			return bad()
		}
		bb.Append(n)
	case *array.Float64Builder:
		x, ok := v.(ijson.Number)
		f, err := x.Float64()
		if !ok || err != nil {
			return bad()
		}
		bb.Append(f)
	case *array.StringBuilder:
		x, ok := v.(string)
		if !ok {
			return bad()
		}
		bb.Append(x)
	case *array.Date32Builder:
		x, _ := v.(string)
		d, err := grammar.ParseDate(x)
		if err != nil {
			return bad()
		}
		bb.Append(arrow.Date32FromTime(time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)))
	case *array.TimestampBuilder:
		x, _ := v.(string)
		ts, err := grammar.ParseDateTime(x)
		if err != nil {
			return bad()
		}
		bb.Append(arrow.Timestamp(ts.UTC().UnixMicro())) // exact to the microsecond (§9.2.6)
	case *array.DurationBuilder:
		x, _ := v.(string)
		d, err := grammar.ParseDuration(x)
		if err != nil {
			return bad()
		}
		bb.Append(arrow.Duration(d.Microseconds()))
	case *array.Decimal128Builder:
		x, _ := v.(string)
		dt := t.(*arrow.Decimal128Type)
		d, err := grammar.ParseDecimal(x)
		if err != nil || !d.Fits(int(dt.Precision), int(dt.Scale)) {
			return fmt.Errorf("%q is not a Decimal(%d,%d)", x, dt.Precision, dt.Scale)
		}
		u, _ := d.Unscaled(int(dt.Scale))
		bb.Append(decimal128.FromBigInt(u))
	case *array.ListBuilder:
		x, ok := v.([]ijson.Value)
		if !ok {
			return bad()
		}
		bb.Append(true)
		et := t.(*arrow.ListType).Elem()
		items := s.Items()
		for _, e := range x {
			if err := Append(bb.ValueBuilder(), et, items, e); err != nil {
				return err
			}
		}
	case *array.StructBuilder:
		x, ok := v.(*ijson.Object)
		if !ok {
			return bad()
		}
		bb.Append(true)
		st := t.(*arrow.StructType)
		for i, f := range st.Fields() {
			p, ok := s.Property(f.Name)
			if !ok {
				return fmt.Errorf("the schema has no %q", f.Name)
			}
			m, _ := x.Get(f.Name)
			if err := Append(bb.FieldBuilder(i), f.Type, p, m); err != nil {
				return fmt.Errorf("%s: %w", f.Name, err)
			}
		}
	default:
		return fmt.Errorf("no JSON form for %s", t)
	}
	return nil
}

// ToRows reads a record batch of a table back into rows, records of the row
// schema.
func ToRows(batch arrow.RecordBatch, row *schema.Schema) ([]*ijson.Object, error) {
	n := int(batch.NumRows())
	rows := make([]*ijson.Object, n)
	for i := range rows {
		rows[i] = ijson.NewObject()
	}
	for j, f := range batch.Schema().Fields() {
		p, ok := row.Property(f.Name)
		if !ok {
			return nil, fmt.Errorf("the row schema has no %q", f.Name)
		}
		col := batch.Column(j)
		for i := 0; i < n; i++ {
			v, present, err := Value(col, i, p)
			if err != nil {
				return nil, fmt.Errorf("row %d, %s: %w", i, f.Name, err)
			}
			if present {
				rows[i].Set(f.Name, v)
			}
		}
	}
	return rows, nil
}

// Value reads one element of an array as the JSON value of its property.
func Value(a arrow.Array, i int, s *schema.Schema) (ijson.Value, bool, error) {
	if a.IsNull(i) {
		return nil, s.Nullable(), nil
	}
	switch x := a.(type) {
	case *array.Boolean:
		return x.Value(i), true, nil
	case *array.Int64:
		if s.Format() == "int64" {
			return strconv.FormatInt(x.Value(i), 10), true, nil
		}
		return ijson.Number(strconv.FormatInt(x.Value(i), 10)), true, nil
	case *array.Float64:
		f := x.Value(i)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, false, fmt.Errorf("%v has no JSON form", f)
		}
		text, err := ijson.ECMAScriptNumber(f)
		return ijson.Number(text), true, err
	case *array.String:
		return x.Value(i), true, nil
	case *array.Date32:
		return x.Value(i).ToTime().UTC().Format("2006-01-02"), true, nil
	case *array.Timestamp:
		return grammar.FormatDateTime(time.UnixMicro(int64(x.Value(i))).UTC()), true, nil
	case *array.Duration:
		d, err := grammar.FormatDuration(time.Duration(x.Value(i)) * time.Microsecond)
		return d, true, err
	case *array.Decimal128:
		scale := int(x.DataType().(*arrow.Decimal128Type).Scale)
		return grammar.FormatDecimal(x.Value(i).BigInt(), scale), true, nil
	case *array.List:
		start, end := x.ValueOffsets(i)
		items := s.Items()
		out := make([]ijson.Value, 0, end-start)
		for k := int(start); k < int(end); k++ {
			v, _, err := Value(x.ListValues(), k, items)
			if err != nil {
				return nil, false, err
			}
			out = append(out, v)
		}
		return out, true, nil
	case *array.Struct:
		o := ijson.NewObject()
		st := x.DataType().(*arrow.StructType)
		for k, f := range st.Fields() {
			p, ok := s.Property(f.Name)
			if !ok {
				return nil, false, fmt.Errorf("the schema has no %q", f.Name)
			}
			v, present, err := Value(x.Field(k), i, p)
			if err != nil {
				return nil, false, err
			}
			if present {
				o.Set(f.Name, v)
			}
		}
		return o, true, nil
	}
	return nil, false, fmt.Errorf("no JSON form for %s", a.DataType())
}
