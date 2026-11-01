package protocol

import (
	"encoding/json"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
)

// Metadata keys carried on Arrow schemas and fields (§9.2.7).
const (
	MetaProfile  = "atlas:profile"
	MetaProtocol = "atlas:protocol"
	MetaUnit     = "atlas:unit"
	MetaLogical  = "atlas:logical"
	MetaKey      = "atlas:key"
	MetaAliases  = "atlas:aliases"
)

// ArrowType maps a column to its Arrow type: the provisional form of §9.2.7's
// mapping. Decimals stay exact; date-times are UTC microseconds.
func ArrowType(c Column) (arrow.DataType, error) {
	switch c.Type {
	case TypeString:
		return arrow.BinaryTypes.String, nil
	case TypeInteger:
		return arrow.PrimitiveTypes.Int64, nil
	case TypeNumber:
		return arrow.PrimitiveTypes.Float64, nil
	case TypeBoolean:
		return arrow.FixedWidthTypes.Boolean, nil
	case TypeDate:
		return arrow.FixedWidthTypes.Date32, nil
	case TypeDateTime:
		return &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}, nil
	case TypeDecimal:
		if c.Precision < 1 || c.Precision > 38 || c.Scale < 0 || c.Scale > c.Precision {
			return nil, fmt.Errorf("column %q: decimal(%d,%d) is outside precision 1 to 38", c.Name, c.Precision, c.Scale)
		}
		return &arrow.Decimal128Type{Precision: int32(c.Precision), Scale: int32(c.Scale)}, nil
	}
	return nil, fmt.Errorf("column %q has an unknown type %q", c.Name, c.Type)
}

// ArrowSchema derives a table's Arrow schema from its columns, never written
// by hand (§3.2.2). The schema's metadata names the exact profile it was
// written under.
func ArrowSchema(t *Table, exact string) (*arrow.Schema, error) {
	fields := make([]arrow.Field, 0, len(t.Columns))
	for _, c := range t.Columns {
		dt, err := ArrowType(c)
		if err != nil {
			return nil, err
		}
		var keys, vals []string
		if c.Unit != "" {
			keys, vals = append(keys, MetaUnit), append(vals, c.Unit)
		}
		if c.Logical != "" {
			keys, vals = append(keys, MetaLogical), append(vals, c.Logical)
		}
		if c.Key {
			keys, vals = append(keys, MetaKey), append(vals, "true")
		}
		if len(c.Aliases) > 0 {
			b, _ := json.Marshal(c.Aliases)
			keys, vals = append(keys, MetaAliases), append(vals, string(b))
		}
		fields = append(fields, arrow.Field{Name: c.Name, Type: dt, Nullable: c.Nullable, Metadata: arrow.NewMetadata(keys, vals)})
	}
	md := arrow.NewMetadata([]string{MetaProfile, MetaProtocol}, []string{exact, Version})
	return arrow.NewSchema(fields, &md), nil
}

// SameColumns reports whether got has exactly want's column names and types,
// in order. A client checks every batch this way and reports invalid_document
// on a mismatch rather than pass wrong types to its caller (§2.2).
func SameColumns(got, want *arrow.Schema) error {
	if got.NumFields() != want.NumFields() {
		return fmt.Errorf("batch has %d columns, the writer profile has %d", got.NumFields(), want.NumFields())
	}
	for i := range want.NumFields() {
		g, w := got.Field(i), want.Field(i)
		if g.Name != w.Name || !arrow.TypeEqual(g.Type, w.Type) {
			return fmt.Errorf("column %d is %s %s, the writer profile says %s %s", i, g.Name, g.Type, w.Name, w.Type)
		}
	}
	return nil
}
