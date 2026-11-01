package arrowschema

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/jhgonline/fi-app-atlas/sdk/go/internal/refs"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/schema"
)

// AP-SCHEMA-002: every client and the gateway derives the vectors' Arrow
// schema from each table schema, or refuses it at the property the vectors
// name.
func TestArrowVectors(t *testing.T) {
	doc, err := ijson.Parse(refs.Read(t, "vectors/arrow.json"))
	if err != nil {
		t.Fatal(err)
	}
	cases, _ := doc.(*ijson.Object).Get("cases")
	if len(cases.([]ijson.Value)) < 15 {
		t.Fatal("the Arrow vectors are missing")
	}
	for _, cv := range cases.([]ijson.Value) {
		c := cv.(*ijson.Object)
		name, _ := c.Get("name")
		exact, _ := c.Get("exact")
		sch, _ := c.Get("schema")
		compiled, err := schema.CompileWith(sch, schema.Open, schema.Options{Unchecked: true})
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		got, err := Derive(compiled, exact.(string), "atlas/1.0")
		if problem, refused := c.Get("problem"); refused {
			target, _ := problem.(*ijson.Object).Get("target")
			var e *Error
			if !errors.As(err, &e) || e.Pointer != target {
				t.Errorf("%s: %v; the vectors refuse it at %v", name, err, target)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		e, _ := c.Get("expected")
		a, _ := e.(*ijson.Object).Get("arrow")
		b, _ := ijson.Marshal(a)
		var want, have any
		_ = json.Unmarshal(b, &want)
		hb, _ := json.Marshal(integration(got))
		_ = json.Unmarshal(hb, &have)
		if !reflect.DeepEqual(have, want) {
			wb, _ := json.Marshal(want)
			t.Errorf("%s\n got %s\nwant %s", name, hb, wb)
		}
	}
}

// integration writes a schema in the JSON form of Apache Arrow's
// integration tests.
func integration(s *arrow.Schema) map[string]any {
	fields := []any{}
	for _, f := range s.Fields() {
		fields = append(fields, field(f))
	}
	return map[string]any{"fields": fields, "metadata": pairs(s.Metadata())}
}

func pairs(md arrow.Metadata) []any {
	out := []any{}
	for i, k := range md.Keys() {
		out = append(out, map[string]any{"key": k, "value": md.Values()[i]})
	}
	return out
}

func field(f arrow.Field) map[string]any {
	children := []any{}
	var typ map[string]any
	switch t := f.Type.(type) {
	case *arrow.BooleanType:
		typ = map[string]any{"name": "bool"}
	case *arrow.Int64Type:
		typ = map[string]any{"name": "int", "bitWidth": 64, "isSigned": true}
	case *arrow.Float64Type:
		typ = map[string]any{"name": "floatingpoint", "precision": "DOUBLE"}
	case *arrow.StringType:
		typ = map[string]any{"name": "utf8"}
	case *arrow.Date32Type:
		typ = map[string]any{"name": "date", "unit": "DAY"}
	case *arrow.TimestampType:
		typ = map[string]any{"name": "timestamp", "unit": "MICROSECOND", "timezone": t.TimeZone}
	case *arrow.DurationType:
		typ = map[string]any{"name": "duration", "unit": "MICROSECOND"}
	case *arrow.Decimal128Type:
		typ = map[string]any{"name": "decimal", "precision": t.Precision, "scale": t.Scale, "bitWidth": 128}
	case *arrow.ListType:
		typ = map[string]any{"name": "list"}
		children = append(children, field(t.ElemField()))
	case *arrow.StructType:
		typ = map[string]any{"name": "struct"}
		for _, c := range t.Fields() {
			children = append(children, field(c))
		}
	default:
		typ = map[string]any{"name": t.String()}
	}
	return map[string]any{"name": f.Name, "nullable": f.Nullable, "type": typ, "children": children, "metadata": pairs(f.Metadata)}
}
