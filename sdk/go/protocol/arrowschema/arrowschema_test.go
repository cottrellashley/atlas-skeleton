package arrowschema

import (
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/schema"
)

const head = `"$schema": "https://json-schema.org/draft/2020-12/schema",
"$vocabulary": {"https://json-schema.org/draft/2020-12/vocab/core": true, "https://json-schema.org/draft/2020-12/vocab/applicator": true, "https://json-schema.org/draft/2020-12/vocab/validation": true, "https://json-schema.org/draft/2020-12/vocab/meta-data": true, "https://json-schema.org/draft/2020-12/vocab/format-assertion": true, "https://atlas.jhg.com/vocab/2026-10": false}`

func table(t *testing.T, required, properties string) *schema.Schema {
	t.Helper()
	v, err := ijson.Parse([]byte("{" + head + `, "type": "array", "items": {"type": "object", "required": [` + required + `], "properties": {` + properties + "}}}"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := schema.Compile(v, schema.Open)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

const holdings = `
	"portfolio": {"type": "string", "x-atlas-logical": "PortfolioCode", "x-atlas-key": true},
	"isin": {"type": ["string", "null"], "x-atlas-logical": "Isin", "x-atlas-key": true},
	"as_of": {"type": "string", "format": "date", "x-atlas-key": true},
	"at": {"type": "string", "format": "date-time"},
	"held_for": {"type": ["string", "null"], "format": "duration"},
	"id": {"type": "string", "format": "int64"},
	"lots": {"type": "integer"},
	"weight": {"type": "number", "x-atlas-logical": "Fraction"},
	"open": {"type": "boolean"},
	"price": {"type": ["string", "null"], "x-atlas-logical": "Decimal(18,6)"},
	"market_value": {"type": "object", "x-atlas-logical": "Money", "x-atlas-aliases": ["mv"]},
	"tags": {"type": "array", "items": {"type": "string"}},
	"risk": {"type": "object", "required": ["oas"], "properties": {"oas": {"type": ["number", "null"], "x-atlas-logical": "Bp"}, "note": {"type": "string"}}},
	"owner": {"type": "string", "x-atlas-logical": "Principal"}`

// AP-SCHEMA-002, ATLAS-TYPES-002: the derivation of §9.2.7.
func TestDerive(t *testing.T) {
	s, err := Derive(table(t, `"portfolio", "as_of", "at", "id", "lots", "weight", "open", "market_value", "tags", "risk"`, holdings), "portfolio.holdings@v1#3f9a1c0b7d2e4a65", "atlas/1.0")
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		name     string
		typ      string
		nullable bool
	}{
		{"portfolio", "utf8", false},
		{"isin", "utf8", true},
		{"as_of", "date32", false},
		{"at", "timestamp[us, tz=UTC]", false},
		{"held_for", "duration[us]", true},
		{"id", "int64", false},
		{"lots", "int64", false},
		{"weight", "float64", false},
		{"open", "bool", false},
		{"price", "decimal(18, 6)", true},
		{"market_value", "struct<amount: decimal(38, 6), currency: utf8>", false},
		{"tags", "list<item: utf8>", false}, // items do not allow null
		{"risk", "struct<oas: float64 nullable, note: utf8 nullable>", false},
		{"owner", "utf8", true},
	}
	if s.NumFields() != len(want) {
		t.Fatalf("%d fields: %s", s.NumFields(), s)
	}
	for i, w := range want {
		f := s.Field(i)
		if f.Name != w.name || f.Type.String() != w.typ || f.Nullable != w.nullable {
			t.Errorf("field %d: %s %s nullable=%v, want %s %s nullable=%v", i, f.Name, f.Type, f.Nullable, w.name, w.typ, w.nullable)
		}
	}
	if v, _ := s.Metadata().GetValue(MetaProfile); v != "portfolio.holdings@v1#3f9a1c0b7d2e4a65" {
		t.Fatalf("atlas:profile: %q", v)
	}
	if v, _ := s.Metadata().GetValue(MetaProtocol); v != "atlas/1.0" {
		t.Fatalf("atlas:protocol: %q", v)
	}
	isin := s.Field(1).Metadata
	if got := strings.Join(isin.Keys(), ","); got != "atlas:logical,atlas:key" {
		t.Fatalf("metadata keys keep the property's order: %s", got)
	}
	if v, _ := isin.GetValue("atlas:logical"); v != `"Isin"` {
		t.Fatalf("a keyword's value is its JSON text: %s", v)
	}
	if v, _ := s.Field(10).Metadata.GetValue("atlas:aliases"); v != `["mv"]` {
		t.Fatalf("atlas:aliases: %s", v)
	}
	if v, _ := isin.GetValue("atlas:key"); v != "true" {
		t.Fatalf("atlas:key: %s", v)
	}
}

func TestNoArrowType(t *testing.T) {
	for _, props := range []string{
		`"x": {"anyOf": [{"type": "string"}, {"type": "integer"}]}`,
		`"x": {"type": "object", "additionalProperties": {"type": "string"}}`,
		`"x": {"type": "array"}`,
	} {
		if _, err := Derive(table(t, "", props), "a.b@v1#3f9a1c0b7d2e4a65", "atlas/1.0"); err == nil {
			t.Errorf("%s derived", props)
		}
	}
}

// Rows written as a batch read back as the same records (AP-SCHEMA-003).
func TestRowsRoundTrip(t *testing.T) {
	tbl := table(t, `"portfolio", "as_of", "at", "id", "lots", "weight", "open", "market_value", "tags", "risk"`, holdings)
	s, err := Derive(tbl, "portfolio.holdings@v1#3f9a1c0b7d2e4a65", "atlas/1.0")
	if err != nil {
		t.Fatal(err)
	}
	docs := []string{
		`{"portfolio":"GC","isin":"US0378331005","as_of":"2026-10-30","at":"2026-10-30T14:02:11.123456Z","held_for":"P1DT2H","id":"9223372036854775807","lots":3,"weight":0.125,"open":true,"price":"101.250000","market_value":{"amount":"1250.500000","currency":"USD"},"tags":["core","ig"],"risk":{"oas":125.5,"note":"n"},"owner":"8e03978e-40d5-43e8-bc93-6894a57f9324"}`,
		`{"portfolio":"GC","isin":null,"as_of":"2026-10-30","at":"2026-10-30T14:02:11Z","held_for":null,"id":"-1","lots":0,"weight":-0.5,"open":false,"price":null,"market_value":{"amount":"-0.000001","currency":"EUR"},"tags":[],"risk":{"oas":null}}`,
	}
	var rows []*ijson.Object
	for _, d := range docs {
		v, err := ijson.Parse([]byte(d))
		if err != nil {
			t.Fatal(err)
		}
		if fs := tbl.Items().Validate(v); len(fs) > 0 {
			t.Fatalf("the test row is invalid: %+v", fs)
		}
		rows = append(rows, v.(*ijson.Object))
	}
	mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
	defer mem.AssertSize(t, 0)
	batch, err := ToBatch(mem, s, tbl.Items(), rows)
	if err != nil {
		t.Fatal(err)
	}
	defer batch.Release()
	if batch.NumRows() != 2 {
		t.Fatalf("%d rows", batch.NumRows())
	}
	back, err := ToRows(batch, tbl.Items())
	if err != nil {
		t.Fatal(err)
	}
	wantText := []string{
		`{"portfolio":"GC","isin":"US0378331005","as_of":"2026-10-30","at":"2026-10-30T14:02:11.123456Z","held_for":"P1DT2H","id":"9223372036854775807","lots":3,"weight":0.125,"open":true,"price":"101.250000","market_value":{"amount":"1250.500000","currency":"USD"},"tags":["core","ig"],"risk":{"oas":125.5,"note":"n"},"owner":"8e03978e-40d5-43e8-bc93-6894a57f9324"}`,
		`{"portfolio":"GC","isin":null,"as_of":"2026-10-30","at":"2026-10-30T14:02:11.000000Z","held_for":null,"id":"-1","lots":0,"weight":-0.5,"open":false,"price":null,"market_value":{"amount":"-0.000001","currency":"EUR"},"tags":[],"risk":{"oas":null}}`,
	}
	for i, r := range back {
		b, _ := ijson.Marshal(r)
		if string(b) != wantText[i] {
			t.Errorf("row %d\n got %s\nwant %s", i, b, wantText[i])
		}
		if fs := tbl.Items().Validate(r); len(fs) > 0 {
			t.Errorf("row %d read back is invalid: %+v", i, fs)
		}
	}
	if _, ok := s.FieldsByName("owner"); !ok {
		t.Fatal("owner")
	}
	_ = arrow.Null
}

func TestToBatchRefusesWhatTheSchemaRefuses(t *testing.T) {
	tbl := table(t, `"price"`, `"price": {"type": "string", "x-atlas-logical": "Decimal(5,2)"}`)
	s, _ := Derive(tbl, "a.b@v1#3f9a1c0b7d2e4a65", "atlas/1.0")
	for _, d := range []string{`{"price":"1234.5"}`, `{"price":"1.234"}`, `{"price":1.5}`} {
		v, _ := ijson.Parse([]byte(d))
		if b, err := ToBatch(memory.NewGoAllocator(), s, tbl.Items(), []*ijson.Object{v.(*ijson.Object)}); err == nil {
			b.Release()
			t.Errorf("%s written", d)
		}
	}
}
