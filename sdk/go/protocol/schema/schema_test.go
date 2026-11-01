package schema

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"atlas.local/sdk/go/protocol/ijson"
)

const head = `"$schema": "https://json-schema.org/draft/2020-12/schema",
"$vocabulary": {"https://json-schema.org/draft/2020-12/vocab/core": true, "https://json-schema.org/draft/2020-12/vocab/applicator": true, "https://json-schema.org/draft/2020-12/vocab/validation": true, "https://json-schema.org/draft/2020-12/vocab/meta-data": true, "https://json-schema.org/draft/2020-12/vocab/format-assertion": true, "https://atlas.jhg.com/vocab/2026-10": false}`

func parse(t testing.TB, doc string) ijson.Value {
	t.Helper()
	v, err := ijson.Parse([]byte(doc))
	if err != nil {
		t.Fatalf("%v in %s", err, doc)
	}
	return v
}

// schemaOf builds a root schema from its members after $schema and
// $vocabulary.
func schemaOf(t testing.TB, members string) ijson.Value {
	return parse(t, "{"+head+", "+members+"}")
}

func compile(t *testing.T, kind Kind, members string) *Schema {
	t.Helper()
	s, err := Compile(schemaOf(t, members), kind)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// refused compiles a schema that must leave the profile, and returns its
// violations.
func refused(t *testing.T, kind Kind, members string) []Violation {
	t.Helper()
	_, err := Compile(schemaOf(t, members), kind)
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("accepted: %s", members)
	}
	return e.Violations
}

func TestTheProfileIsRequired(t *testing.T) {
	if _, err := Compile(parse(t, `{"type": "object"}`), Open); err == nil {
		t.Fatal("a schema without $schema and $vocabulary is refused")
	}
	vocab := strings.Replace(head, `"https://atlas.jhg.com/vocab/2026-10": false`, `"https://atlas.jhg.com/vocab/2026-10": true`, 1)
	if _, err := Compile(parse(t, "{"+vocab+`, "type": "object"}`), Open); err == nil {
		t.Fatal("the Atlas vocabulary is declared false")
	}
	draft7 := strings.Replace(head, "draft/2020-12/schema", "draft-07/schema#", 1)
	if _, err := Compile(parse(t, "{"+draft7+`, "type": "object"}`), Open); err == nil {
		t.Fatal("only 2020-12")
	}
}

// ATLAS-TYPES-001: only the profile's keywords and vocabularies.
func TestOnlyTheProfilesKeywords(t *testing.T) {
	for _, members := range []string{
		`"type": "object", "allOf": [{"type": "object"}]`,
		`"type": "object", "not": {"type": "string"}`,
		`"type": "object", "if": {"type": "object"}, "then": {"type": "object"}`,
		`"type": "object", "patternProperties": {"^a$": {"type": "string"}}`,
		`"type": "object", "propertyNames": {"type": "string"}`,
		`"type": "object", "dependentRequired": {"a": ["b"]}`,
		`"type": "array", "contains": {"type": "string"}`,
		`"type": "object", "unevaluatedProperties": false`,
		`"type": "object", "$anchor": "a"`,
		`"type": "object", "x-other": 1`,
		`"type": "object", "properties": {"a": {"type": "string", "$id": "https://x.example/a"}}`,
		`"type": "object", "properties": {"a": {"type": "string", "$defs": {}}}`,
		`"type": "object", "properties": {"a": {"$ref": "https://x.example/s.json"}}`,
		`"type": "object", "properties": {"a": {"$ref": "#/$defs/missing"}}`,
		`"type": "object", "properties": {"a": {"$ref": "#/properties/b"}}`,
	} {
		refused(t, Open, members)
	}
}

func TestTypes(t *testing.T) {
	compile(t, Open, `"type": "object", "properties": {"a": {"type": ["string", "null"]}, "b": {"type": "integer"}}`)
	for _, members := range []string{
		`"type": "object", "properties": {"a": {"type": ["null", "string"]}}`,
		`"type": "object", "properties": {"a": {"type": ["string", "integer"]}}`,
		`"type": "object", "properties": {"a": {"type": "null"}}`,
		`"type": "object", "properties": {"a": {"minLength": 1}}`,
		`"type": "object", "properties": {"a": {"type": "string", "minimum": 1}}`,
		`"type": "object", "properties": {"a": {"type": "number", "pattern": "^a$"}}`,
		`"type": "object", "properties": {"a": {"type": "number", "format": "date"}}`,
		`"type": "object", "properties": {"a": {"type": "string", "format": "email"}}`,
		`"type": "object", "properties": {"a": {"type": "number", "multipleOf": 2}}`,
		`"type": "object", "properties": {"a": {"type": "integer", "multipleOf": 0.5}}`,
		`"type": "object", "properties": {"a": {"type": "string", "enum": ["x", 1, "UNKNOWN"], "x-atlas-enum": [{"value": "x"}, {"value": "UNKNOWN"}]}}`,
		`"type": "object", "required": ["b"], "properties": {"a": {"type": "string"}}`,
		`"type": "object", "properties": {"a": {"type": "string", "default": 1}}`,
		`"type": "object", "properties": {"a": true}`,
		`"type": "object", "properties": {"Bad-Name": {"type": "string"}}`,
	} {
		refused(t, Open, members)
	}
}

// §9.2.1 rule 6: a request's objects are closed; every other schema is open.
func TestClosedAndOpen(t *testing.T) {
	closed := `"type": "object", "additionalProperties": false, "properties": {"a": {"type": "object", "additionalProperties": false, "properties": {"b": {"type": "string"}}}}`
	compile(t, Request, closed)
	refused(t, Open, closed)
	refused(t, Request, `"type": "object", "additionalProperties": false, "properties": {"a": {"type": "object", "properties": {"b": {"type": "string"}}}}`)
	compile(t, Open, `"type": "object", "properties": {"a": {"type": "object", "additionalProperties": true, "properties": {"b": {"type": "string"}}}}`)
	labels := `"type": "object", "additionalProperties": false, "properties": {"labels": {"type": "object", "additionalProperties": {"type": "string"}}}`
	compile(t, Request, labels) // a map
	compile(t, Open, `"type": "object", "properties": {"labels": {"type": "object", "additionalProperties": {"type": "string"}}}`)
}

// §9.2.3: a string enum has UNKNOWN, except in a request.
func TestUnknown(t *testing.T) {
	open := `"type": "object", "properties": {"s": {"type": "string", "enum": ["open", "UNKNOWN"], "x-atlas-enum": [{"value": "open", "description": "Open"}, {"value": "UNKNOWN", "description": "Unknown"}]}}`
	compile(t, Open, open)
	refused(t, Open, `"type": "object", "properties": {"s": {"type": "string", "enum": ["open"], "x-atlas-enum": [{"value": "open"}]}}`)
	refused(t, Open, `"type": "object", "properties": {"s": {"type": "string", "enum": ["open", "UNKNOWN"]}}`)
	refused(t, Open, `"type": "object", "properties": {"s": {"type": "string", "enum": ["open", "UNKNOWN"], "x-atlas-enum": [{"value": "open"}]}}`)
	compile(t, Request, `"type": "object", "additionalProperties": false, "properties": {"s": {"type": ["string", "null"], "enum": ["a", "b", null]}}`)
	refused(t, Request, `"type": "object", "additionalProperties": false, "properties": {"s": {"type": "string", "enum": ["a", "UNKNOWN"]}}`)
	refused(t, Open, `"type": "object", "properties": {"s": {"type": "string", "enum": ["a", "UNKNOWN"], "x-atlas-enum": [{"value": "a", "aliases": ["UNKNOWN"]}, {"value": "UNKNOWN"}]}}`)
}

func TestAtlasKeywords(t *testing.T) {
	compile(t, Open, `"type": "array", "items": {"type": "object", "required": ["portfolio"], "properties": {
		"portfolio": {"type": "string", "x-atlas-logical": "PortfolioCode", "x-atlas-key": true},
		"isin": {"type": ["string", "null"], "x-atlas-logical": "Isin", "x-atlas-key": true},
		"mv": {"type": "object", "x-atlas-logical": "Money", "x-atlas-aliases": ["market_value_old"]},
		"oas": {"type": ["number", "null"], "x-atlas-logical": "Bp", "x-atlas-unit": "bp"},
		"age": {"type": "number", "x-atlas-unit": "days"},
		"price": {"type": "string", "x-atlas-logical": "Decimal(18,6)"},
		"old": {"type": ["number", "null"], "x-atlas-deprecated": {"reason": "Use oas.", "since": "2026-09-01", "sunset": "2027-03-01", "replaced_by": "oas"}},
		"ref": {"type": "string", "x-atlas-reference": "portfolio.list@v1"},
		"current": {"type": "string", "x-atlas-deprecated": null}}}`) // null: not deprecated
	for _, members := range []string{
		// A key is required and not nullable, unless an identifier.
		`"type": "object", "properties": {"p": {"type": "string", "x-atlas-key": true}}`,
		`"type": "object", "properties": {"p": {"type": "string", "x-atlas-logical": "PortfolioCode", "x-atlas-key": true}}`,
		`"type": "object", "required": ["p"], "properties": {"p": {"type": ["string", "null"], "x-atlas-logical": "PortfolioCode", "x-atlas-key": true}}`,
		`"type": "object", "required": ["p"], "properties": {"p": {"type": "string", "x-atlas-key": false}}`,
		// Logical types.
		`"type": "object", "properties": {"x": {"type": "string", "x-atlas-logical": "Bp"}}`,
		`"type": "object", "properties": {"x": {"type": "string", "x-atlas-logical": "Decimal(39,2)"}}`,
		`"type": "object", "properties": {"x": {"type": "string", "x-atlas-logical": "Decimal(5,6)"}}`,
		`"type": "object", "properties": {"x": {"type": "string", "x-atlas-logical": "Decimal(5, 2)"}}`,
		`"type": "object", "properties": {"x": {"type": "string", "x-atlas-logical": "Decimal"}}`,
		`"type": "object", "properties": {"x": {"type": "string", "x-atlas-logical": "Decimal(18,2)", "format": "int64"}}`,
		`"type": "object", "properties": {"x": {"type": "string", "x-atlas-logical": "Ticker"}}`,
		`"type": "object", "properties": {"x": {"type": "object", "x-atlas-logical": "Money", "properties": {"fx": {"type": "number"}}}}`,
		// Units.
		`"type": "object", "properties": {"x": {"type": "number", "x-atlas-unit": "bps"}}`,
		`"type": "object", "properties": {"x": {"type": "string", "x-atlas-unit": "bp"}}`,
		`"type": "object", "properties": {"x": {"type": "number", "x-atlas-logical": "Bp", "x-atlas-unit": "percent"}}`,
		// Aliases.
		`"type": "object", "properties": {"a": {"type": "string", "x-atlas-aliases": ["b"]}, "b": {"type": "string"}}`,
		`"type": "object", "properties": {"a": {"type": "string", "x-atlas-aliases": ["c"]}, "b": {"type": "string", "x-atlas-aliases": ["c"]}}`,
		`"type": "object", "properties": {"a": {"type": "string", "x-atlas-aliases": ["Old"]}}`,
		// Deprecation.
		`"type": "object", "properties": {"a": {"type": "string", "x-atlas-deprecated": {"since": "2026-09-01"}}}`,
		`"type": "object", "properties": {"a": {"type": "string", "x-atlas-deprecated": {"reason": "x", "since": "2026-09-01", "replaced_by": null}}}`, // no sunset (Q-PROF-11)
		`"type": "object", "properties": {"a": {"type": "string", "x-atlas-deprecated": {"reason": "x", "since": "2026-09-01", "replaced_by": "risk.duration@v1"}}}`,
		`"type": "object", "properties": {"a": {"type": "string", "x-atlas-deprecated": {"reason": "x", "since": "1 Sept"}}}`,
		// References.
		`"type": "object", "properties": {"a": {"type": "string", "x-atlas-reference": "portfolio.list"}}`,
	} {
		refused(t, Open, members)
	}
}

func TestOneOfNeedsADiscriminator(t *testing.T) {
	compile(t, Open, `"type": "object", "properties": {"by": {"oneOf": [
		{"type": "object", "required": ["by", "id"], "properties": {"by": {"type": "string", "const": "id"}, "id": {"type": "string"}}},
		{"type": "object", "required": ["by", "exact"], "properties": {"by": {"type": "string", "const": "exact"}, "exact": {"type": "string"}}}]}}`)
	refused(t, Open, `"type": "object", "properties": {"x": {"oneOf": [{"type": "string"}, {"type": "number"}]}}`)
	refused(t, Open, `"type": "object", "properties": {"by": {"oneOf": [
		{"type": "object", "required": ["by"], "properties": {"by": {"type": "string", "const": "a"}}},
		{"type": "object", "required": ["by"], "properties": {"by": {"type": "string", "const": "a"}}}]}}`)
}

func TestRefs(t *testing.T) {
	s := compile(t, Open, `"type": "object", "$defs": {"money": {"type": "object", "x-atlas-logical": "Money"}},
		"properties": {"mv": {"$ref": "#/$defs/money", "description": "Market value", "x-atlas-aliases": ["market_value"]}}`)
	mv, _ := s.Property("mv")
	if mv.Type() != "object" || !slices.Equal(mv.Aliases(), []string{"market_value"}) || !slices.Equal(mv.Properties(), []string{"amount", "currency"}) {
		t.Fatalf("a reference answers with its definition: %v %v %v", mv.Type(), mv.Aliases(), mv.Properties())
	}
	refused(t, Open, `"type": "object", "$defs": {"node": {"type": "object", "properties": {"child": {"$ref": "#/$defs/node"}}}}, "properties": {"root": {"$ref": "#/$defs/node"}}`)
	refused(t, Open, `"type": "object", "$defs": {"a": {"$ref": "#/$defs/b"}, "b": {"$ref": "#/$defs/a"}}, "properties": {"x": {"$ref": "#/$defs/a"}}`)
	refused(t, Open, `"type": "object", "$defs": {"s": {"type": "string"}}, "properties": {"x": {"$ref": "#/$defs/s", "minLength": 2}}`)
}

func TestLimits(t *testing.T) {
	deep := `"type": "object", "properties": {"a": ` + strings.Repeat(`{"type": "object", "properties": {"a": `, 31) + `{"type": "string"}` + strings.Repeat("}}", 31) + "}"
	refused(t, Open, deep)
	ok := `"type": "object", "properties": {"a": ` + strings.Repeat(`{"type": "object", "properties": {"a": `, 29) + `{"type": "string"}` + strings.Repeat("}}", 29) + "}"
	compile(t, Open, ok)
	big := `"type": "object", "description": "` + strings.Repeat("x", MaxSize) + `"`
	refused(t, Open, big)
}

func TestPatterns(t *testing.T) {
	for _, c := range []struct {
		pattern string
		match   []string
		miss    []string
	}{
		{`^[A-Z]{3}$`, []string{"USD"}, []string{"usd", "USDX", "US", "USD\n"}},
		{`^a.c$`, []string{"abc", "aéc"}, []string{"a\nc", "a\rc", "ac"}},
		{`^a^b$c$`, []string{"a^b$c"}, []string{"abc"}}, // I-Regexp has no anchors: ^ and $ inside are literals
		{`^(ab|cd)+$`, []string{"ab", "abcd"}, []string{"", "abc"}},
		{`^\p{Lu}\p{Ll}*$`, []string{"Zurich", "École"}, []string{"zurich"}},
		{`^[^0-9]+$`, []string{"ab", "a\nb"}, []string{"a1"}},
		{`^[-a-c]$`, []string{"-", "b"}, []string{"d"}},
		{`^[a-c-]$`, []string{"-", "a"}, []string{"d"}},
		{`^\.\*\\$`, []string{`.*\`}, []string{"a*\\"}},
		{`^x{2,3}$`, []string{"xx", "xxx"}, []string{"x", "xxxx"}},
		{`^x{2,}$`, []string{"xx", "xxxxxx"}, []string{"x"}},
		{`^$`, []string{""}, []string{"a"}},
	} {
		re, err := compilePattern(c.pattern)
		if err != nil {
			t.Errorf("%s: %v", c.pattern, err)
			continue
		}
		for _, s := range c.match {
			if !re.MatchString(s) {
				t.Errorf("%s does not match %q", c.pattern, s)
			}
		}
		for _, s := range c.miss {
			if re.MatchString(s) {
				t.Errorf("%s matches %q", c.pattern, s)
			}
		}
	}
	for _, bad := range []string{
		`[A-Z]{3}`, `^[A-Z]{3}`, `^(a$`, `^a)$`, `^a**$`, `^*a$`, `^a{2,1}$`, `^a{1001}$`, `^\d$`, `^\w+$`, `^\s$`,
		`^\b$`, `^(?:a)$`, `^(?i)a$`, `^[]$`, `^[a-]b]$`, `^[z-a]$`, `^\p{IsBasicLatin}$`, `^\p{Cn}$`, `^\p{C}$`,
		`^a{,3}$`, `^[a-b-c]$`, `^a|*$`, `^\$`,
	} {
		if _, err := compilePattern(bad); err == nil {
			t.Errorf("%q accepted as ^I-Regexp$", bad)
		}
	}
}

func failures(fs []Failure) string {
	var parts []string
	for _, f := range fs {
		parts = append(parts, f.Pointer+" "+f.Keyword)
	}
	return strings.Join(parts, "; ")
}

func expect(t *testing.T, s *Schema, instance string, want string) {
	t.Helper()
	got := failures(s.Validate(parse(t, instance)))
	if got != want {
		t.Errorf("%s\n got: %s\nwant: %s", instance, got, want)
	}
}

// §9.2.5: every failure, with its pointer and keyword (assumption A-009).
func TestValidate(t *testing.T) {
	request := compile(t, Request, `"type": "object", "additionalProperties": false, "required": ["portfolios"],
		"properties": {
			"portfolios": {"type": "array", "minItems": 1, "maxItems": 3, "uniqueItems": true, "items": {"type": "string", "x-atlas-logical": "PortfolioCode"}},
			"as_of": {"type": ["string", "null"], "format": "date", "default": null},
			"asset_class": {"type": ["string", "null"], "enum": ["credit", "rates", null], "default": null},
			"limit": {"type": "integer", "minimum": 1, "maximum": 1000, "multipleOf": 10},
			"ratio": {"type": "number", "exclusiveMinimum": 0, "exclusiveMaximum": 1}}`)
	expect(t, request, `{"portfolios": ["GLOBAL-CREDIT"]}`, "")
	expect(t, request, `{"portfolios": ["A"], "as_of": null, "asset_class": null, "limit": 20, "ratio": 0.5}`, "")
	expect(t, request, `{}`, "/portfolios required")
	expect(t, request, `{"portfolios": [], "x": 1, "y": 2}`, "/portfolios minItems; /x additionalProperties; /y additionalProperties")
	expect(t, request, `{"portfolios": ["A", "A", "B", "C"]}`, "/portfolios maxItems; /portfolios uniqueItems")
	expect(t, request, `{"portfolios": [""]}`, "/portfolios/0 minLength")
	expect(t, request, `{"portfolios": [7]}`, "/portfolios/0 type")
	expect(t, request, `{"portfolios": ["A"], "as_of": "2026-02-30"}`, "/as_of format")
	expect(t, request, `{"portfolios": ["A"], "asset_class": "equity"}`, "/asset_class enum")
	expect(t, request, `{"portfolios": ["A"], "limit": 0}`, "/limit minimum") // 0 is a multiple of 10
	expect(t, request, `{"portfolios": ["A"], "limit": 1001}`, "/limit maximum; /limit multipleOf")
	expect(t, request, `{"portfolios": ["A"], "limit": 20.0}`, "")                               // 20.0 is an integer
	expect(t, request, `{"portfolios": ["A"], "limit": 20.5}`, "/limit multipleOf; /limit type") // a number with a fraction is no integer
	expect(t, request, `{"portfolios": ["A"], "limit": 9007199254740992}`, "/limit maximum; /limit multipleOf; /limit type")
	expect(t, request, `{"portfolios": ["A"], "ratio": 0}`, "/ratio exclusiveMinimum")
	expect(t, request, `{"portfolios": ["A"], "ratio": 1}`, "/ratio exclusiveMaximum")
	expect(t, request, `[]`, " type")

	rows := compile(t, Open, `"type": "array", "items": {"type": "object", "required": ["isin", "mv", "status"], "properties": {
		"isin": {"type": ["string", "null"], "x-atlas-logical": "Isin", "x-atlas-key": true},
		"mv": {"type": "object", "x-atlas-logical": "Money"},
		"status": {"type": "string", "enum": ["open", "UNKNOWN"], "x-atlas-enum": [{"value": "open"}, {"value": "UNKNOWN"}]},
		"price": {"type": ["string", "null"], "x-atlas-logical": "Decimal(5,2)"},
		"owner": {"type": "string", "x-atlas-logical": "Principal"},
		"at": {"type": "string", "format": "date-time"},
		"id": {"type": "string", "format": "int64"}}}`)
	good := `{"isin": "US0378331005", "mv": {"amount": "1250.500000", "currency": "USD"}, "status": "open"}`
	expect(t, rows, `[`+good+`]`, "")
	expect(t, rows, `[{"isin": null, "mv": {"amount": "1", "currency": "USD", "note": "open object"}, "status": "UNKNOWN", "extra": true}]`, "")
	expect(t, rows, `[{"isin": "us0378331005", "mv": {"amount": "1.1234567", "currency": "usd"}, "status": "shut"}]`,
		"/0/isin pattern; /0/mv/amount format; /0/mv/currency pattern; /0/status enum")
	expect(t, rows, `[{"isin": "US0378331005", "mv": {"currency": "USD"}, "status": "open"}]`, "/0/mv/amount required")
	expect(t, rows, `[{"isin": "US0378331005", "mv": {"amount": "1", "currency": "USD"}, "status": "open", "price": "1234.5"}]`, "/0/price format")
	expect(t, rows, `[{"isin": "US0378331005", "mv": {"amount": "1", "currency": "USD"}, "status": "open", "price": "999.99", "owner": "8E03978E-40D5-43E8-BC93-6894A57F9324"}]`, "/0/owner format")
	expect(t, rows, `[{"isin": "US0378331005", "mv": {"amount": "1", "currency": "USD"}, "status": "open", "at": "2026-10-31T12:00:00"}]`, "/0/at format")
	expect(t, rows, `[{"isin": "US0378331005", "mv": {"amount": "1", "currency": "USD"}, "status": "open", "id": "9223372036854775808"}]`, "/0/id format")
	expect(t, rows, `[{"isin": "US0378331005", "mv": {"amount": "1", "currency": "USD"}, "status": "open", "id": "-9223372036854775808"}]`, "")

	union := compile(t, Open, `"type": "object", "properties": {
		"by": {"oneOf": [
			{"type": "object", "required": ["by", "id"], "properties": {"by": {"type": "string", "const": "id"}, "id": {"type": "string"}}},
			{"type": "object", "required": ["by", "exact"], "properties": {"by": {"type": "string", "const": "exact"}, "exact": {"type": "string", "format": "exact-id"}}}]},
		"either": {"anyOf": [{"type": "string"}, {"type": "integer"}]},
		"pair": {"type": "array", "prefixItems": [{"type": "string"}, {"type": "number"}], "items": false},
		"labels": {"type": "object", "additionalProperties": {"type": "string"}}}`)
	expect(t, union, `{"by": {"by": "exact", "exact": "portfolio.holdings@v1#3f9a1c0b7d2e4a65"}}`, "")
	expect(t, union, `{"by": {"by": "exact", "exact": "portfolio.holdings"}}`, "/by oneOf")
	expect(t, union, `{"either": 1.5}`, "/either anyOf")
	expect(t, union, `{"pair": ["a", 1, true, null]}`, "/pair/2 items; /pair/3 items")
	expect(t, union, `{"labels": {"team": "QFI", "n": 1}}`, "/labels/n type")
}

// I-JSON numbers are binary64 values (assumption A-014).
func TestNumbersAreBinary64(t *testing.T) {
	s := compile(t, Open, `"type": "object", "properties": {"x": {"type": "number", "enum": [0.1], "maximum": 0.1}}`)
	expect(t, s, `{"x": 0.10000000000000000001}`, "")
	expect(t, s, `{"x": 0.1000000000000001}`, "/x enum; /x maximum")
}

func TestDefaultsValidate(t *testing.T) {
	refused(t, Request, `"type": "object", "additionalProperties": false, "properties": {"n": {"type": "integer", "minimum": 1, "default": 0}}`)
	refused(t, Open, `"type": "object", "properties": {"s": {"type": "string", "enum": ["a", "UNKNOWN"], "x-atlas-enum": [{"value": "a"}, {"value": "UNKNOWN"}], "default": "b"}}`)
}

func TestAccessors(t *testing.T) {
	rows := compile(t, Open, `"type": "array", "items": {"type": "object", "required": ["portfolio", "weight"], "properties": {
		"portfolio": {"type": "string", "x-atlas-logical": "PortfolioCode", "x-atlas-key": true},
		"weight": {"type": "number", "x-atlas-logical": "Fraction"},
		"price": {"type": ["string", "null"], "x-atlas-logical": "Decimal(18,6)", "default": null},
		"status": {"type": "string", "enum": ["closed", "UNKNOWN"], "x-atlas-enum": [{"value": "closed", "aliases": ["sold"]}, {"value": "UNKNOWN"}]},
		"mv": {"type": "object", "x-atlas-logical": "Money", "x-atlas-aliases": ["market_value"]}}}`)
	row := rows.Items()
	if row == nil || row.Type() != "object" || !slices.Equal(row.Properties(), []string{"portfolio", "weight", "price", "status", "mv"}) {
		t.Fatal("items and properties, in order")
	}
	p, _ := row.Property("portfolio")
	if !p.Key() || !row.Required("portfolio") || row.Required("price") {
		t.Fatal("keys and required")
	}
	w, _ := row.Property("weight")
	if u, ok := w.Unit(); !ok || u != "fraction" {
		t.Fatal("a Fraction is in fraction")
	}
	price, _ := row.Property("price")
	if name, prec, scale, ok := price.Logical(); !ok || name != "Decimal" || prec != 18 || scale != 6 || !price.Nullable() {
		t.Fatal("Decimal(18,6), nullable")
	}
	if d, ok := price.Default(); !ok || d != nil {
		t.Fatal("default null")
	}
	st, _ := row.Property("status")
	if v, ok := st.EnumAlias("sold"); !ok || v != "closed" || !st.EnumHas("UNKNOWN") {
		t.Fatal("enum aliases")
	}
	mv, _ := row.Property("mv")
	if !slices.Equal(mv.Aliases(), []string{"market_value"}) || !slices.Equal(mv.Properties(), []string{"amount", "currency"}) || !mv.Required("currency") {
		t.Fatal("Money's implied members")
	}
	projected := row.Project([]string{"weight", "portfolio", "nope"})
	if !slices.Equal(projected.Properties(), []string{"portfolio", "weight"}) || !projected.Required("weight") || projected.Required("status") {
		t.Fatalf("a projection keeps properties order: %v", projected.Properties())
	}
	if !slices.Equal(row.Properties(), []string{"portfolio", "weight", "price", "status", "mv"}) {
		t.Fatal("projecting leaves the schema as it was")
	}
}

// Whatever schema and instance arrive, compiling and validating never
// panic, and an accepted pattern always compiles in Go's engine.
func FuzzCompileAndValidate(f *testing.F) {
	f.Add(`"type": "object", "properties": {"a": {"type": "string", "pattern": "^[A-Z]{3}$"}}`, `{"a": "USD"}`)
	f.Add(`"type": "array", "items": {"type": "object", "x-atlas-logical": "Money"}`, `[{"amount": "1", "currency": "USD"}]`)
	f.Add(`"type": "object", "$defs": {"d": {"type": "integer", "multipleOf": 3}}, "properties": {"n": {"$ref": "#/$defs/d"}}`, `{"n": 9}`)
	f.Fuzz(func(t *testing.T, members, instance string) {
		v, err := ijson.Parse([]byte("{" + head + ", " + members + "}"))
		if err != nil {
			return
		}
		for _, kind := range []Kind{Open, Request} {
			s, err := Compile(v, kind)
			if err != nil {
				continue
			}
			if in, err := ijson.Parse([]byte(instance)); err == nil {
				s.Validate(in)
			}
		}
	})
}

func FuzzPattern(f *testing.F) {
	for _, s := range []string{`^[A-Z]{3}$`, `^a.c$`, `^(ab|cd)+$`, `^\p{Lu}[^0-9-]*$`, `^x{2,}$`} {
		f.Add(s, "USD")
	}
	f.Fuzz(func(t *testing.T, pattern, s string) {
		re, err := compilePattern(pattern)
		if err != nil {
			return
		}
		re.MatchString(s)
	})
}

// $vocabulary names exactly §9.2.1's vocabularies: none missing, none
// added, whatever the count.
func TestVocabularyIsExact(t *testing.T) {
	const core = `"https://json-schema.org/draft/2020-12/vocab/core": true`
	five := core + `, "https://json-schema.org/draft/2020-12/vocab/applicator": true, "https://json-schema.org/draft/2020-12/vocab/validation": true, "https://json-schema.org/draft/2020-12/vocab/meta-data": true, "https://json-schema.org/draft/2020-12/vocab/format-assertion": true`
	for name, vocab := range map[string]string{
		"empty":       ``,
		"one":         core,
		"five":        five,
		"seven":       five + `, "https://atlas.jhg.com/vocab/2026-10": false, "https://example.com/vocab": true`,
		"one changed": five + `, "https://atlas.jhg.com/vocab/2026-10": true`,
	} {
		_, err := Compile(parse(t, `{"$schema": "https://json-schema.org/draft/2020-12/schema", "$vocabulary": {`+vocab+`}, "type": "object"}`), Open)
		var e *Error
		if !errors.As(err, &e) || e.Violations[0].Pointer != "/$vocabulary" {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Money's amount is a decimal string and its currency a Currency: a
// declaration of either that says otherwise is refused, so nothing
// validates that the Arrow mapping cannot hold.
func TestMoneyMembersAgree(t *testing.T) {
	money := func(members string) string {
		return `"type": "object", "properties": {"mv": {"type": "object", "x-atlas-logical": "Money", "properties": {` + members + `}}}`
	}
	for name, members := range map[string]string{
		"booleans":             `"amount": {"type": "boolean"}, "currency": {"type": "boolean"}`,
		"a nullable amount":    `"amount": {"type": ["string", "null"]}`,
		"a number amount":      `"amount": {"type": "number"}`,
		"a dated currency":     `"currency": {"type": "string", "format": "date"}`,
		"a basis-point amount": `"amount": {"type": "number", "x-atlas-logical": "Bp"}`,
		"a wide amount":        `"amount": {"type": "string", "x-atlas-logical": "Decimal(40,10)"}`,
	} {
		vs := refused(t, Open, money(members))
		if !strings.HasPrefix(vs[0].Pointer, "/properties/mv/properties/") {
			t.Errorf("%s: %v", name, vs)
		}
	}
	for name, members := range map[string]string{
		"a definition that is not a string": `"amount": {"$ref": "#/$defs/flag"}`,
	} {
		vs := refused(t, Open, `"$defs": {"flag": {"type": "boolean"}}, `+money(members))
		if vs[0].Pointer != "/properties/mv/properties/amount/type" {
			t.Errorf("%s: %v", name, vs)
		}
	}
	narrow := compile(t, Open, money(`"amount": {"type": "string", "x-atlas-logical": "Decimal(18,2)"}`))
	expect(t, narrow, `{"mv": {"amount": "1.23", "currency": "USD"}}`, "")
	expect(t, narrow, `{"mv": {"amount": "1.234", "currency": "USD"}}`, "/mv/amount format")
	expect(t, compile(t, Open, money(`"amount": {"type": "string", "description": "The amount."}`)), `{"mv": {"amount": false, "currency": false}}`, "/mv/amount type; /mv/currency type")
}

// A definition may refer to another: every accessor follows the whole
// chain, as validation does.
func TestRefChains(t *testing.T) {
	s := compile(t, Open, `"$defs": {"price": {"type": ["string", "null"], "x-atlas-logical": "Decimal(18,6)", "default": null}, "px": {"$ref": "#/$defs/price", "x-atlas-aliases": ["p"]}},
		"type": "object", "properties": {"close": {"$ref": "#/$defs/px", "description": "The close."}}`)
	close, _ := s.Property("close")
	name, p, sc, ok := close.Logical()
	if !ok || name != "Decimal" || p != 18 || sc != 6 || close.Type() != "string" || close.Format() != "decimal" || !close.Nullable() {
		t.Fatalf("a chain answers with its last definition: %v %d %d %v %q %q %v", name, p, sc, ok, close.Type(), close.Format(), close.Nullable())
	}
	if d, has := close.Default(); !has || d != nil || !slices.Equal(close.Aliases(), []string{"p"}) {
		t.Fatalf("a chain's default and aliases: %v %v %v", d, has, close.Aliases())
	}
	expect(t, s, `{"close": "1.0000001"}`, "/close format")
	expect(t, s, `{"close": "1.5"}`, "")
	// A key through a chain to a nullable definition is refused.
	vs := refused(t, Open, `"$defs": {"code": {"type": ["string", "null"]}, "c": {"$ref": "#/$defs/code"}},
		"type": "object", "required": ["k"], "properties": {"k": {"$ref": "#/$defs/c", "x-atlas-key": true}}`)
	if vs[0].Pointer != "/properties/k/x-atlas-key" {
		t.Fatalf("a nullable key through a chain: %v", vs)
	}
}
