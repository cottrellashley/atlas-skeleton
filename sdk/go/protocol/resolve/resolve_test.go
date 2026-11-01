package resolve

import (
	"errors"
	"strings"
	"testing"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/grammar"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/schema"
)

const head = `"$schema": "https://json-schema.org/draft/2020-12/schema",
"$vocabulary": {"https://json-schema.org/draft/2020-12/vocab/core": true, "https://json-schema.org/draft/2020-12/vocab/applicator": true, "https://json-schema.org/draft/2020-12/vocab/validation": true, "https://json-schema.org/draft/2020-12/vocab/meta-data": true, "https://json-schema.org/draft/2020-12/vocab/format-assertion": true, "https://atlas.jhg.com/vocab/2026-10": false}`

func parse(t *testing.T, doc string) ijson.Value {
	t.Helper()
	v, err := ijson.Parse([]byte(doc))
	if err != nil {
		t.Fatalf("%v in %s", err, doc)
	}
	return v
}

// row compiles a record schema from its required list and properties.
func row(t *testing.T, kind schema.Kind, required, properties string) *schema.Schema {
	t.Helper()
	closed := ""
	if kind == schema.Request {
		closed = `"additionalProperties": false, `
	}
	s, err := schema.Compile(parse(t, "{"+head+`, "type": "object", `+closed+`"required": [`+required+`], "properties": {`+properties+"}}"), kind)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func lens(t *testing.T, doc string) Lens {
	t.Helper()
	l, err := ParseLens(parse(t, doc))
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func id(t *testing.T, s string) grammar.Exact {
	t.Helper()
	e, err := grammar.ParseExact(s)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

const status = `"status": {"type": "string", "enum": ["open", "%s", "UNKNOWN"], "x-atlas-enum": [{"value": "open"}, {"value": "%s"%s}, {"value": "UNKNOWN"}]}`

func statusOf(value, aliases string) string {
	a := ""
	if aliases != "" {
		a = `, "aliases": [` + aliases + "]"
	}
	return strings.Replace(strings.Replace(strings.Replace(status, "%s", value, 1), "%s", value, 1), "%s", a, 1)
}

// The chain of holdings@v1: P0, then P1 renames mv to weight and the status
// sold to closed, then P2 converts weight to bp, defaults region, drops note.
func chain(t *testing.T) []Profile {
	p0 := row(t, schema.Open, `"portfolio", "mv"`, `"portfolio": {"type": "string"}, "mv": {"type": "number", "x-atlas-unit": "fraction"}, `+statusOf("sold", "")+`, "note": {"type": "string"}`)
	p1 := row(t, schema.Open, `"portfolio", "weight"`, `"portfolio": {"type": "string"}, "weight": {"type": "number", "x-atlas-unit": "fraction", "x-atlas-aliases": ["mv"]}, `+statusOf("closed", `"sold"`)+`, "note": {"type": "string"}`)
	p2 := row(t, schema.Open, `"portfolio", "weight", "region"`, `"portfolio": {"type": "string"}, "weight": {"type": "number", "x-atlas-unit": "bp"}, `+statusOf("closed", "")+`, "region": {"type": "string"}`)
	return []Profile{
		{id(t, "portfolio.holdings@v1#aaaaaaaaaaaaaaaa"), nil, p0},
		{id(t, "portfolio.holdings@v1#bbbbbbbbbbbbbbbb"), lens(t, `[{"op": "rename", "from": "mv", "to": "weight"}, {"op": "alias_value", "field": "status", "from": "sold", "to": "closed"}]`), p1},
		{id(t, "portfolio.holdings@v1#cccccccccccccccc"), lens(t, `[{"op": "convert_unit", "field": "weight", "from": "fraction", "to": "bp"}, {"op": "default", "field": "region", "value": "EU"}, {"op": "drop", "field": "note"}]`), p2},
	}
}

func resolveOne(t *testing.T, ps []Profile, writer, reader grammar.Exact, opt Options, record string) (string, error) {
	t.Helper()
	plan, err := New(ps, writer, reader, opt)
	if err != nil {
		return "", err
	}
	x := parse(t, record).(*ijson.Object)
	before, _ := ijson.Marshal(x)
	z, err := plan.Record(x)
	if after, _ := ijson.Marshal(x); string(after) != string(before) {
		t.Errorf("resolution changed its input: %s → %s", before, after)
	}
	if err != nil {
		return "", err
	}
	b, _ := ijson.Marshal(z)
	return string(b), nil
}

func want(t *testing.T, got string, err error, expected string) {
	t.Helper()
	if err != nil {
		t.Fatalf("resolution failed: %v", err)
	}
	if got != expected {
		t.Fatalf("\n got %s\nwant %s", got, expected)
	}
}

func wantFailure(t *testing.T, err error, target string) {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Target != target {
		t.Fatalf("want resolution_failed at %q, got %v", target, err)
	}
}

// AP-RESOLVE-001, §9.5.8's cases.
func TestResolution(t *testing.T) {
	c := chain(t)
	p0, p1, p2 := c[0].Exact, c[1].Exact, c[2].Exact

	t.Run("identical profiles", func(t *testing.T) {
		got, err := resolveOne(t, c, p2, p2, Options{}, `{"portfolio": "A", "anything": 1}`)
		want(t, got, err, `{"portfolio":"A","anything":1}`)
	})
	t.Run("forward along the chain, an unknown field ignored", func(t *testing.T) {
		got, err := resolveOne(t, c, p0, p2, Options{}, `{"portfolio": "A", "mv": 0.0125, "status": "sold", "note": "x", "extra": 1}`)
		want(t, got, err, `{"portfolio":"A","weight":125,"status":"closed","region":"EU"}`)
	})
	t.Run("backward along the chain", func(t *testing.T) {
		got, err := resolveOne(t, c, p2, p0, Options{}, `{"portfolio": "A", "weight": 125, "status": "closed", "region": "EU"}`)
		want(t, got, err, `{"portfolio":"A","mv":0.0125,"status":"sold"}`)
	})
	t.Run("one step", func(t *testing.T) {
		got, err := resolveOne(t, c[:2], p1, p0, Options{}, `{"portfolio": "A", "weight": 0.5, "status": "open", "note": "n"}`)
		want(t, got, err, `{"portfolio":"A","mv":0.5,"status":"open","note":"n"}`)
	})
	t.Run("an unknown enum value", func(t *testing.T) {
		got, err := resolveOne(t, c, p0, p2, Options{}, `{"portfolio": "A", "mv": 0.5, "status": "suspended"}`)
		want(t, got, err, `{"portfolio":"A","weight":5000,"status":"UNKNOWN","region":"EU"}`)
	})
	t.Run("an enum value renamed, by the reader's x-atlas-enum aliases", func(t *testing.T) {
		older := row(t, schema.Open, `"status"`, statusOf("sold", ""))
		newer := row(t, schema.Open, `"status"`, statusOf("closed", `"sold"`))
		ps := []Profile{{id(t, "a.b@v1#1111111111111111"), nil, older}, {id(t, "a.b@v1#2222222222222222"), Lens{}, newer}}
		got, err := resolveOne(t, ps, ps[0].Exact, ps[1].Exact, Options{}, `{"status": "sold"}`)
		want(t, got, err, `{"status":"closed"}`)
	})
	t.Run("a required field missing", func(t *testing.T) {
		_, err := resolveOne(t, c, p0, p2, Options{}, `{"mv": 0.5}`)
		wantFailure(t, err, "/portfolio")
	})
	t.Run("a nullable field missing", func(t *testing.T) {
		older := row(t, schema.Open, `"a"`, `"a": {"type": "string"}`)
		newer := row(t, schema.Open, `"a", "b"`, `"a": {"type": "string"}, "b": {"type": ["string", "null"]}`)
		ps := []Profile{{id(t, "a.b@v1#1111111111111111"), nil, older}, {id(t, "a.b@v1#2222222222222222"), Lens{}, newer}}
		got, err := resolveOne(t, ps, ps[0].Exact, ps[1].Exact, Options{}, `{"a": "x"}`)
		want(t, got, err, `{"a":"x","b":null}`)
	})
	t.Run("a default from the reader's schema", func(t *testing.T) {
		older := row(t, schema.Open, `"a"`, `"a": {"type": "string"}`)
		newer := row(t, schema.Open, `"a", "n"`, `"a": {"type": "string"}, "n": {"type": "integer", "default": 10}`)
		ps := []Profile{{id(t, "a.b@v1#1111111111111111"), nil, older}, {id(t, "a.b@v1#2222222222222222"), Lens{}, newer}}
		got, err := resolveOne(t, ps, ps[0].Exact, ps[1].Exact, Options{}, `{"a": "x"}`)
		want(t, got, err, `{"a":"x","n":10}`)
	})
	t.Run("an old request resolved forward by the gateway", func(t *testing.T) {
		older := row(t, schema.Request, `"portfolios"`, `"portfolios": {"type": "array", "items": {"type": "string"}}`)
		newer := row(t, schema.Request, `"codes"`, `"codes": {"type": "array", "items": {"type": "string"}}, "as_of": {"type": ["string", "null"], "format": "date", "default": null}`)
		ps := []Profile{{id(t, "portfolio.holdings@v1#1111111111111111"), nil, older}, {id(t, "portfolio.holdings@v1#2222222222222222"), lens(t, `[{"op": "rename", "from": "portfolios", "to": "codes"}]`), newer}}
		got, err := resolveOne(t, ps, ps[0].Exact, ps[1].Exact, Options{}, `{"portfolios": ["GLOBAL-CREDIT"]}`)
		want(t, got, err, `{"codes":["GLOBAL-CREDIT"],"as_of":null}`)
	})
	t.Run("a projected row lacks the required columns it did not ask for", func(t *testing.T) {
		got, err := resolveOne(t, c, p0, p2, Options{Columns: []string{"weight"}}, `{"mv": 0.0125}`)
		want(t, got, err, `{"weight":125}`)
	})
	t.Run("a projected row whose requested column was renamed", func(t *testing.T) {
		got, err := resolveOne(t, c[:2], p0, p1, Options{Columns: []string{"weight", "portfolio"}}, `{"portfolio": "A", "mv": 0.25}`)
		want(t, got, err, `{"portfolio":"A","weight":0.25}`)
	})
	t.Run("a renamed column found by its alias, with no lens", func(t *testing.T) {
		older := row(t, schema.Open, `"mv"`, `"mv": {"type": "number"}`)
		newer := row(t, schema.Open, `"weight"`, `"weight": {"type": "number", "x-atlas-aliases": ["mv", "wt"]}`)
		ps := []Profile{{id(t, "a.b@v1#1111111111111111"), nil, older}, {id(t, "a.b@v1#2222222222222222"), Lens{}, newer}}
		got, err := resolveOne(t, ps, ps[0].Exact, ps[1].Exact, Options{}, `{"mv": 1, "wt": 2}`)
		want(t, got, err, `{"weight":2}`) // the last present alias in the list
	})
	t.Run("a column the reader does not have", func(t *testing.T) {
		_, err := New(c, p0, p2, Options{Columns: []string{"mv"}})
		wantFailure(t, err, "/mv")
	})
	t.Run("profiles of two different versions are refused", func(t *testing.T) {
		_, err := New(c, p0, id(t, "portfolio.holdings@v2#cccccccccccccccc"), Options{})
		wantFailure(t, err, "")
	})
	t.Run("a dynamic schema is never resolved", func(t *testing.T) {
		got, err := resolveOne(t, c, p0, p2, Options{Dynamic: true}, `{"whatever": [1, 2]}`)
		want(t, got, err, `{"whatever":[1,2]}`)
	})
}

func TestPromotions(t *testing.T) {
	older := row(t, schema.Open, `"qty", "as_of"`, `"qty": {"type": "integer"}, "as_of": {"type": "string", "format": "date"}`)
	newer := row(t, schema.Open, `"qty", "as_of"`, `"qty": {"type": "number"}, "as_of": {"type": "string", "format": "date-time"}`)
	l := lens(t, `[{"op": "promote", "field": "qty", "from": "integer", "to": "number"}, {"op": "promote", "field": "as_of", "from": "date", "to": "date-time"}]`)
	ps := []Profile{{id(t, "a.b@v1#1111111111111111"), nil, older}, {id(t, "a.b@v1#2222222222222222"), l, newer}}
	w, r := ps[0].Exact, ps[1].Exact
	got, err := resolveOne(t, ps, w, r, Options{}, `{"qty": 3, "as_of": "2026-10-31"}`)
	want(t, got, err, `{"qty":3,"as_of":"2026-10-31T00:00:00Z"}`)
	got, err = resolveOne(t, ps, r, w, Options{}, `{"qty": 3.0, "as_of": "2026-10-31T00:00:00Z"}`)
	want(t, got, err, `{"qty":3,"as_of":"2026-10-31"}`)
	got, err = resolveOne(t, ps, r, w, Options{}, `{"qty": 3, "as_of": "2026-10-31T01:00:00+01:00"}`)
	want(t, got, err, `{"qty":3,"as_of":"2026-10-31"}`)
	_, err = resolveOne(t, ps, r, w, Options{}, `{"qty": 3.5, "as_of": "2026-10-31T00:00:00Z"}`)
	wantFailure(t, err, "/qty")
	_, err = resolveOne(t, ps, r, w, Options{}, `{"qty": 3, "as_of": "2026-10-31T12:00:00Z"}`)
	wantFailure(t, err, "/as_of")
	got, err = resolveOne(t, ps, w, r, Options{}, `{"qty": null, "as_of": "2026-10-31"}`)
	if err == nil {
		t.Fatalf("null passes the promote but is no number for the reader: %s", got)
	}
}

// A unit on a decimal string is converted exactly and rounded once, to the
// reader's scale, ties to even (§9.5.4, assumption A-020). The lens names
// the units: x-atlas-unit stands only on numbers (§9.2.3).
func TestDecimalUnits(t *testing.T) {
	older := row(t, schema.Open, `"spread"`, `"spread": {"type": "string", "x-atlas-logical": "Decimal(18,7)"}`)
	newer := row(t, schema.Open, `"spread"`, `"spread": {"type": "string", "x-atlas-logical": "Decimal(18,2)"}`)
	ps := []Profile{{id(t, "a.b@v1#1111111111111111"), nil, older}, {id(t, "a.b@v1#2222222222222222"), lens(t, `[{"op": "convert_unit", "field": "spread", "from": "fraction", "to": "bp"}]`), newer}}
	w, r := ps[0].Exact, ps[1].Exact
	for in, out := range map[string]string{"0.012345": "123.45", "0.0123455": "123.46", "0.0123445": "123.44", "-0.0123445": "-123.44"} {
		got, err := resolveOne(t, ps, w, r, Options{}, `{"spread": "`+in+`"}`)
		want(t, got, err, `{"spread":"`+out+`"}`)
	}
	got, err := resolveOne(t, ps, r, w, Options{}, `{"spread": "123.45"}`)
	want(t, got, err, `{"spread":"0.0123450"}`)

	days := row(t, schema.Open, `"age"`, `"age": {"type": "string", "x-atlas-logical": "Decimal(18,6)"}`)
	secs := row(t, schema.Open, `"age"`, `"age": {"type": "string", "x-atlas-logical": "Decimal(18,0)"}`)
	ps = []Profile{{id(t, "a.b@v1#1111111111111111"), nil, days}, {id(t, "a.b@v1#2222222222222222"), lens(t, `[{"op": "convert_unit", "field": "age", "from": "days", "to": "seconds"}]`), secs}}
	got, err = resolveOne(t, ps, ps[1].Exact, ps[0].Exact, Options{}, `{"age": "1"}`)
	want(t, got, err, `{"age":"0.000012"}`) // 1/86400, at scale 6
	got, err = resolveOne(t, ps, ps[0].Exact, ps[1].Exact, Options{}, `{"age": "1.5"}`)
	want(t, got, err, `{"age":"129600"}`)
}

func TestArraysAndNulls(t *testing.T) {
	older := row(t, schema.Open, `"codes", "ratios"`, `"codes": {"type": "array", "items": {"type": "string"}}, "ratios": {"type": "array", "items": {"type": ["number", "null"], "x-atlas-unit": "percent"}}`)
	newer := row(t, schema.Open, `"codes", "ratios"`, `"codes": {"type": "array", "items": {"type": "string"}}, "ratios": {"type": "array", "items": {"type": ["number", "null"], "x-atlas-unit": "fraction"}}`)
	ps := []Profile{{id(t, "a.b@v1#1111111111111111"), nil, older}, {id(t, "a.b@v1#2222222222222222"), lens(t, `[{"op": "alias_value", "field": "codes", "from": "GC", "to": "GLOBAL-CREDIT"}, {"op": "convert_unit", "field": "ratios", "from": "percent", "to": "fraction"}]`), newer}}
	got, err := resolveOne(t, ps, ps[0].Exact, ps[1].Exact, Options{}, `{"codes": ["GC", "EM"], "ratios": [50, null, 12.5]}`)
	want(t, got, err, `{"codes":["GLOBAL-CREDIT","EM"],"ratios":[0.5,null,0.125]}`)
}

func TestNestedObjects(t *testing.T) {
	older := row(t, schema.Open, `"m"`, `"m": {"type": "object", "required": ["s"], "properties": {"s": `+strings.TrimPrefix(statusOf("sold", ""), `"status": `)+`}}`)
	newer := row(t, schema.Open, `"m"`, `"m": {"type": "object", "required": ["s", "n"], "properties": {"s": `+strings.TrimPrefix(statusOf("closed", `"sold"`), `"status": `)+`, "n": {"type": "integer", "default": 1}}}`)
	ps := []Profile{{id(t, "a.b@v1#1111111111111111"), nil, older}, {id(t, "a.b@v1#2222222222222222"), Lens{}, newer}}
	got, err := resolveOne(t, ps, ps[0].Exact, ps[1].Exact, Options{}, `{"m": {"s": "sold", "x": true}}`)
	want(t, got, err, `{"m":{"s":"closed","n":1}}`) // resolved by step 5, without lenses
	got, err = resolveOne(t, ps, ps[0].Exact, ps[1].Exact, Options{}, `{"m": {"s": "open", "n": 2, "x": true}}`)
	want(t, got, err, `{"m":{"s":"open","n":2,"x":true}}`) // valid as it is: unchanged
}

func TestParseLens(t *testing.T) {
	for _, bad := range []string{
		`[{"op": "rename", "from": "a"}]`,
		`[{"op": "rename", "from": "a", "to": "B"}]`,
		`[{"op": "rename", "from": "a", "to": "b", "field": "c"}]`,
		`[{"op": "convert_unit", "field": "a", "from": "bp", "to": "days"}]`,
		`[{"op": "convert_unit", "field": "a", "from": "years", "to": "days"}]`,
		`[{"op": "promote", "field": "a", "from": "number", "to": "integer"}]`,
		`[{"op": "promote", "field": "a", "from": "string", "to": "number"}]`,
		`[{"op": "default", "field": "a"}]`,
		`[{"op": "drop"}]`,
		`[{"op": "split", "field": "a"}]`,
		`{"op": "drop", "field": "a"}`,
	} {
		if _, err := ParseLens(parse(t, bad)); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}
