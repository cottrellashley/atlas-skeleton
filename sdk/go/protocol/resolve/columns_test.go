package resolve

import (
	"errors"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/arrowschema"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/grammar"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/schema"
)

// parity resolves records as a batch, column by column, and row by row, and
// requires the same answer: the same batch, or a failure at the same target
// (AP-RESOLVE-002). It checks each record as a table of one row, and all of
// them as a table of many.
func parity(t *testing.T, ps []Profile, writer, reader grammar.Exact, opt Options, records ...string) {
	t.Helper()
	plan, err := New(ps, writer, reader, opt)
	if err != nil {
		t.Fatal(err)
	}
	var ws *schema.Schema
	for _, p := range ps {
		if p.Exact == writer {
			ws = p.Schema
		}
	}
	fields, err := arrowschema.Fields(ws)
	if err != nil {
		t.Fatal(err)
	}
	var rows []*ijson.Object
	for _, r := range records {
		rows = append(rows, parse(t, r).(*ijson.Object))
	}
	check := func(label string, rows []*ijson.Object) {
		t.Helper()
		mem := memory.NewCheckedAllocator(memory.NewGoAllocator())
		defer mem.AssertSize(t, 0)
		in, err := arrowschema.ToBatch(mem, arrow.NewSchema(fields, nil), ws, rows)
		if err != nil {
			// A batch's types are the writer's: a record its own schema refuses
			// (a string where it says integer) never reaches a column.
			for _, r := range rows {
				if !ws.Valid(r) {
					t.Logf("%s: no Arrow batch holds it, so it has no column form: %v", label, err)
					return
				}
			}
			t.Fatalf("%s: writing the batch: %v", label, err)
		}
		defer in.Release()
		got, colErr := plan.Batch(mem, in, nil)
		if got != nil {
			defer got.Release()
		}
		// Row by row, from the rows exactly as the batch holds them.
		back, err := arrowschema.ToRows(in, ws)
		if err != nil {
			t.Fatal(err)
		}
		var resolved []*ijson.Object
		var rowErr error
		for _, r := range back {
			z, err := plan.Record(r)
			if err != nil {
				rowErr = err
				break
			}
			resolved = append(resolved, z)
		}
		if rowErr != nil || colErr != nil {
			var re, ce *Error
			if !errors.As(rowErr, &re) || !errors.As(colErr, &ce) || re.Target != ce.Target {
				t.Fatalf("%s: row by row %v, column by column %v", label, rowErr, colErr)
			}
			return
		}
		rs := plan.Reader()
		if rs == nil {
			rs = ws // an identity plan reads as the writer wrote
		}
		rf, err := arrowschema.Fields(rs)
		if err != nil {
			t.Fatal(err)
		}
		want, err := arrowschema.ToBatch(mem, arrow.NewSchema(rf, nil), rs, resolved)
		if err != nil {
			t.Fatalf("%s: writing the rows' answer: %v", label, err)
		}
		defer want.Release()
		if !array.RecordEqual(got, want) {
			t.Fatalf("%s: column by column\n%v\nrow by row\n%v", label, got, want)
		}
	}
	for i, r := range rows {
		check("row "+records[i], []*ijson.Object{r})
	}
	check("all rows", rows)
}

func TestColumnsEqualRows(t *testing.T) {
	c := chain(t)
	p0, p1, p2 := c[0].Exact, c[1].Exact, c[2].Exact
	recordsP0 := []string{
		`{"portfolio": "A", "mv": 0.0125, "status": "sold", "note": "x"}`,
		`{"portfolio": "B", "mv": 0.5, "status": "suspended"}`,
		`{"portfolio": "C", "mv": 1, "status": "open"}`,
	}
	parity(t, c, p0, p2, Options{}, recordsP0...)
	parity(t, c[:2], p0, p1, Options{}, recordsP0...)
	parity(t, c, p0, p2, Options{Columns: []string{"weight"}}, recordsP0...)
	parity(t, c, p0, p2, Options{Columns: []string{"status", "portfolio"}}, recordsP0...)
	parity(t, c, p2, p0, Options{}, `{"portfolio": "A", "weight": 125, "status": "closed", "region": "EU"}`, `{"portfolio": "B", "weight": 1, "status": "open", "region": "US"}`)
	parity(t, c[:2], p1, p0, Options{}, `{"portfolio": "A", "weight": 0.5, "status": "open", "note": "n"}`, `{"portfolio": "B", "weight": 0.25, "status": "closed"}`)
	parity(t, c, p0, p2, Options{}, `{"mv": 0.5}`) // a required field missing: the same failure

	t.Run("aliases and defaults", func(t *testing.T) {
		older := row(t, schema.Open, `"a"`, `"a": {"type": "string"}, "mv": {"type": "number"}, "wt": {"type": "number"}`)
		newer := row(t, schema.Open, `"a", "b", "n", "weight"`, `"a": {"type": "string"}, "b": {"type": ["string", "null"]}, "n": {"type": "integer", "default": 10}, "weight": {"type": "number", "x-atlas-aliases": ["mv", "wt"], "default": 0}`)
		ps := []Profile{{id(t, "a.b@v1#1111111111111111"), nil, older}, {id(t, "a.b@v1#2222222222222222"), Lens{}, newer}}
		parity(t, ps, ps[0].Exact, ps[1].Exact, Options{}, `{"a": "x", "mv": 1, "wt": 2}`, `{"a": "y", "mv": 3}`, `{"a": "z"}`)
	})
	t.Run("enum renamed by the reader's aliases", func(t *testing.T) {
		older := row(t, schema.Open, `"status"`, statusOf("sold", ""))
		newer := row(t, schema.Open, `"status"`, statusOf("closed", `"sold"`))
		ps := []Profile{{id(t, "a.b@v1#1111111111111111"), nil, older}, {id(t, "a.b@v1#2222222222222222"), Lens{}, newer}}
		parity(t, ps, ps[0].Exact, ps[1].Exact, Options{}, `{"status": "sold"}`, `{"status": "open"}`, `{"status": "UNKNOWN"}`)
	})
	t.Run("promotions", func(t *testing.T) {
		older := row(t, schema.Open, `"qty", "as_of"`, `"qty": {"type": "integer"}, "as_of": {"type": "string", "format": "date"}`)
		newer := row(t, schema.Open, `"qty", "as_of"`, `"qty": {"type": "number"}, "as_of": {"type": "string", "format": "date-time"}`)
		l := lens(t, `[{"op": "promote", "field": "qty", "from": "integer", "to": "number"}, {"op": "promote", "field": "as_of", "from": "date", "to": "date-time"}]`)
		ps := []Profile{{id(t, "a.b@v1#1111111111111111"), nil, older}, {id(t, "a.b@v1#2222222222222222"), l, newer}}
		parity(t, ps, ps[0].Exact, ps[1].Exact, Options{}, `{"qty": 3, "as_of": "2026-10-31"}`, `{"qty": -7, "as_of": "1970-01-01"}`)
		parity(t, ps, ps[1].Exact, ps[0].Exact, Options{}, `{"qty": 3, "as_of": "2026-10-31T00:00:00Z"}`)
		parity(t, ps, ps[1].Exact, ps[0].Exact, Options{}, `{"qty": 3.5, "as_of": "2026-10-31T00:00:00Z"}`)
		parity(t, ps, ps[1].Exact, ps[0].Exact, Options{}, `{"qty": 3, "as_of": "2026-10-31T12:00:00Z"}`)
	})
	t.Run("decimal units", func(t *testing.T) {
		older := row(t, schema.Open, `"spread"`, `"spread": {"type": "string", "x-atlas-logical": "Decimal(18,7)"}`)
		newer := row(t, schema.Open, `"spread"`, `"spread": {"type": "string", "x-atlas-logical": "Decimal(18,2)"}`)
		ps := []Profile{{id(t, "a.b@v1#1111111111111111"), nil, older}, {id(t, "a.b@v1#2222222222222222"), lens(t, `[{"op": "convert_unit", "field": "spread", "from": "fraction", "to": "bp"}]`), newer}}
		parity(t, ps, ps[0].Exact, ps[1].Exact, Options{}, `{"spread": "0.012345"}`, `{"spread": "0.0123455"}`, `{"spread": "0.0123445"}`, `{"spread": "-0.0123445"}`)
		parity(t, ps, ps[1].Exact, ps[0].Exact, Options{}, `{"spread": "123.45"}`)
	})
	t.Run("lists and nulls", func(t *testing.T) {
		older := row(t, schema.Open, `"codes", "ratios"`, `"codes": {"type": "array", "items": {"type": "string"}}, "ratios": {"type": "array", "items": {"type": ["number", "null"], "x-atlas-unit": "percent"}}`)
		newer := row(t, schema.Open, `"codes", "ratios"`, `"codes": {"type": "array", "items": {"type": "string"}}, "ratios": {"type": "array", "items": {"type": ["number", "null"], "x-atlas-unit": "fraction"}}`)
		ps := []Profile{{id(t, "a.b@v1#1111111111111111"), nil, older}, {id(t, "a.b@v1#2222222222222222"), lens(t, `[{"op": "alias_value", "field": "codes", "from": "GC", "to": "GLOBAL-CREDIT"}, {"op": "convert_unit", "field": "ratios", "from": "percent", "to": "fraction"}]`), newer}}
		parity(t, ps, ps[0].Exact, ps[1].Exact, Options{}, `{"codes": ["GC", "EM"], "ratios": [50, null, 12.5]}`, `{"codes": [], "ratios": []}`)
	})
	t.Run("nested objects", func(t *testing.T) {
		older := row(t, schema.Open, `"m"`, `"m": {"type": "object", "required": ["s"], "properties": {"s": {"type": "string", "enum": ["open", "sold", "UNKNOWN"], "x-atlas-enum": [{"value": "open"}, {"value": "sold"}, {"value": "UNKNOWN"}]}, "x": {"type": "boolean"}}}`)
		newer := row(t, schema.Open, `"m"`, `"m": {"type": "object", "required": ["s", "n"], "properties": {"s": {"type": "string", "enum": ["open", "closed", "UNKNOWN"], "x-atlas-enum": [{"value": "open"}, {"value": "closed", "aliases": ["sold"]}, {"value": "UNKNOWN"}]}, "n": {"type": "integer", "default": 1}}}`)
		ps := []Profile{{id(t, "a.b@v1#1111111111111111"), nil, older}, {id(t, "a.b@v1#2222222222222222"), Lens{}, newer}}
		parity(t, ps, ps[0].Exact, ps[1].Exact, Options{}, `{"m": {"s": "sold", "x": true}}`, `{"m": {"s": "open"}}`)
	})
	t.Run("float units vectorised", func(t *testing.T) {
		older := row(t, schema.Open, `"days"`, `"days": {"type": ["number", "null"], "x-atlas-unit": "days"}`)
		newer := row(t, schema.Open, `"days"`, `"days": {"type": ["number", "null"], "x-atlas-unit": "seconds"}`)
		ps := []Profile{{id(t, "a.b@v1#1111111111111111"), nil, older}, {id(t, "a.b@v1#2222222222222222"), lens(t, `[{"op": "convert_unit", "field": "days", "from": "days", "to": "seconds"}]`), newer}}
		parity(t, ps, ps[0].Exact, ps[1].Exact, Options{}, `{"days": 1.5}`, `{"days": null}`, `{"days": 0.1}`)
		parity(t, ps, ps[1].Exact, ps[0].Exact, Options{}, `{"days": 3}`, `{"days": 86400}`)
	})
}

// A column that needs nothing is the writer's own array, not a copy.
func TestColumnsPassThrough(t *testing.T) {
	older := row(t, schema.Open, `"a", "x"`, `"a": {"type": "string"}, "x": {"type": "number"}`)
	newer := row(t, schema.Open, `"b", "x"`, `"b": {"type": "string"}, "x": {"type": "number"}`)
	ps := []Profile{{id(t, "a.b@v1#1111111111111111"), nil, older}, {id(t, "a.b@v1#2222222222222222"), lens(t, `[{"op": "rename", "from": "a", "to": "b"}]`), newer}}
	plan, err := New(ps, ps[0].Exact, ps[1].Exact, Options{})
	if err != nil {
		t.Fatal(err)
	}
	fields, _ := arrowschema.Fields(older)
	mem := memory.NewGoAllocator()
	in, err := arrowschema.ToBatch(mem, arrow.NewSchema(fields, nil), older, []*ijson.Object{parse(t, `{"a": "p", "x": 1.5}`).(*ijson.Object)})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Release()
	out, err := plan.Batch(mem, in, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Release()
	if out.Schema().Field(0).Name != "b" || out.Column(0).Data() != in.Column(0).Data() || out.Column(1).Data() != in.Column(1).Data() {
		t.Fatal("a renamed column and an unchanged one are passed through")
	}
}
