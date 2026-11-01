// Package arrowschema is §9.2.7: a table's Arrow schema, derived from its
// JSON Schema and never written by hand, and the conversion of rows between
// JSON records and Arrow record batches by that mapping.
package arrowschema

import (
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/schema"
)

// Metadata keys of an Atlas Arrow stream (§9.2.7, §9.6.5).
const (
	MetaProfile  = "atlas:profile"  // the exact-id the stream was written under
	MetaProtocol = "atlas:protocol" // atlas/1.0
	MetaEnd      = "atlas:end"      // the final batch's end line
)

// The Arrow types of §9.2.7 that are not Arrow's own singletons.
var (
	Timestamp = &arrow.TimestampType{Unit: arrow.Microsecond, TimeZone: "UTC"}
	Duration  = &arrow.DurationType{Unit: arrow.Microsecond}
	Money     = arrow.StructOf(
		arrow.Field{Name: "amount", Type: &arrow.Decimal128Type{Precision: 38, Scale: 6}},
		arrow.Field{Name: "currency", Type: arrow.BinaryTypes.String},
	)
)

// Error is a table schema with no Arrow form: the property, by its pointer
// in the schema, and why (Q-ARROW-3). It is invalid_document.
type Error struct {
	Pointer string
	Message string
}

func (e *Error) Error() string { return fmt.Sprintf("no Arrow type at %s: %s", e.Pointer, e.Message) }

func fail(s *schema.Schema, suffix, format string, args ...any) error {
	return &Error{s.Pointer() + suffix, fmt.Sprintf(format, args...)}
}

// Derive returns a table's Arrow schema: one field per property of its rows,
// in order, with the stream's metadata (atlas:profile, atlas:protocol).
func Derive(table *schema.Schema, profile, protocol string) (*arrow.Schema, error) {
	if table.Type() != "array" || table.Items() == nil || table.Items().Type() != "object" {
		return nil, fail(table, "", "a table's schema is an array of row objects")
	}
	if len(table.Items().Properties()) == 0 {
		// A DynamicSchema result: each answer carries its own schema.
		return nil, fail(table.Items(), "", "a row without properties has no Arrow schema")
	}
	fields, err := Fields(table.Items())
	if err != nil {
		return nil, err
	}
	md := arrow.NewMetadata([]string{MetaProfile, MetaProtocol}, []string{profile, protocol})
	return arrow.NewSchema(fields, &md), nil
}

// Fields maps a row object's properties to Arrow fields. A field is nullable
// exactly when its property allows null or is not required, and carries the
// property's Atlas keywords as metadata.
func Fields(row *schema.Schema) ([]arrow.Field, error) {
	var fields []arrow.Field
	for _, name := range row.Properties() {
		p, _ := row.Property(name)
		t, err := Type(p)
		if err != nil {
			return nil, err
		}
		md, err := metadata(p)
		if err != nil {
			return nil, fail(p, "", "%v", err)
		}
		fields = append(fields, arrow.Field{Name: name, Type: t, Nullable: p.Nullable() || !row.Required(name), Metadata: md})
	}
	return fields, nil
}

// metadata carries every Atlas keyword as atlas:<keyword without x-atlas->,
// its value the keyword's JSON text in RFC 8785's canonical form, so every
// language writes the same bytes, in the order the property writes them
// (assumption A-019, Q-ARROW-4).
func metadata(p *schema.Schema) (arrow.Metadata, error) {
	var keys, values []string
	for _, k := range p.AtlasKeywords() {
		b, err := ijson.Canonical(k.Value)
		if err != nil {
			return arrow.Metadata{}, err
		}
		keys = append(keys, "atlas:"+k.Name[len("x-atlas-"):])
		values = append(values, string(b))
	}
	return arrow.NewMetadata(keys, values), nil
}

// Type maps one property schema to its Arrow type (§9.2.7's table): a
// logical type first, then a format, then the JSON type. A string with any
// other format (decimal without Decimal(p,s) among them), or a string
// logical type, is Utf8; a union and a map have no Arrow type in atlas/1.0
// (assumption A-017, Q-ARROW-2, Q-ARROW-3).
func Type(s *schema.Schema) (arrow.DataType, error) {
	if s.Union() {
		return nil, fail(s, "", "anyOf and oneOf have no Arrow type")
	}
	if name, p, sc, ok := s.Logical(); ok {
		switch name {
		case "Decimal":
			return &arrow.Decimal128Type{Precision: int32(p), Scale: int32(sc)}, nil
		case "Money":
			return Money, nil
		}
	}
	switch s.Type() {
	case "boolean":
		return arrow.FixedWidthTypes.Boolean, nil
	case "integer":
		return arrow.PrimitiveTypes.Int64, nil
	case "number":
		return arrow.PrimitiveTypes.Float64, nil
	case "string":
		switch s.Format() {
		case "date":
			return arrow.FixedWidthTypes.Date32, nil
		case "date-time":
			return Timestamp, nil
		case "duration":
			return Duration, nil
		case "int64":
			return arrow.PrimitiveTypes.Int64, nil
		}
		return arrow.BinaryTypes.String, nil
	case "array":
		items := s.Items()
		if items == nil {
			return nil, fail(s, "/items", "an array has items")
		}
		t, err := Type(items)
		if err != nil {
			return nil, err
		}
		return arrow.ListOfField(arrow.Field{Name: "item", Type: t, Nullable: items.Nullable()}), nil
	case "object":
		if len(s.Properties()) == 0 {
			return nil, fail(s, "", "an object without properties (a map) has no Arrow type")
		}
		fields, err := Fields(s)
		if err != nil {
			return nil, err
		}
		return arrow.StructOf(fields...), nil
	}
	return nil, fail(s, "", "a schema without a type has no Arrow type")
}
