package ijson

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// esc writes JSON unicode escapes as %u in a test document; the backslash is
// built at run time, because tools that write files decode a literal one.
func esc(doc string) string { return strings.ReplaceAll(doc, "%u", string([]byte{'\\', 'u'})) }

func TestParseKeepsOrderAndExactNumbers(t *testing.T) {
	v, err := Parse([]byte(` {"z": 1, "a": [0.10, -2e3, true, null], "m": {"y": "x", "b": 3.50}} `))
	if err != nil {
		t.Fatal(err)
	}
	o := v.(*Object)
	if got := strings.Join(o.Names(), ","); got != "z,a,m" {
		t.Fatalf("member order lost: %s", got)
	}
	arr, _ := o.Get("a")
	if n := arr.([]Value)[0].(Number); n != "0.10" {
		t.Fatalf("a number's text is kept: %q", n)
	}
	b, _ := Marshal(v)
	if string(b) != `{"z":1,"a":[0.10,-2e3,true,null],"m":{"y":"x","b":3.50}}` {
		t.Fatalf("Marshal: %s", b)
	}
}

// AP-CONV-001: a receiver rejects a document with duplicate member names.
func TestParseRefusesWhatIJSONRefuses(t *testing.T) {
	for _, c := range []struct{ name, doc string }{
		{"duplicate member", `{"a":1,"a":2}`},
		{"duplicate member, nested", `{"x":{"a":1,"b":2,"a":3}}`},
		{"duplicate member, one escaped", esc(`{"a":1,"%u0061":2}`)},
		{"unpaired high surrogate", esc(`"%ud800"`)},
		{"unpaired low surrogate", esc(`"%udc00"`)},
		{"high surrogate then not a low one", esc(`"%ud800%u0041"`)},
		{"escaped noncharacter", esc(`"%ufffe"`)},
		{"escaped noncharacter in the FDD0 block", esc(`"%ufdd0"`)},
		{"raw noncharacter", "\"" + string(rune(0xFFFF)) + "\""},
		{"invalid UTF-8", "\"\xff\xfe\""},
		{"a UTF-8 encoded surrogate", "\"\xed\xa0\x80\""},
		{"byte-order mark", "\xEF\xBB\xBF{}"},
		{"raw control character", "\"a\x01b\""},
		{"trailing data", `{} {}`},
		{"beyond binary64", `1e400`},
		{"leading zero", `01`},
		{"lone minus", `-`},
		{"fraction without digits", `1.`},
		{"exponent without digits", `1e`},
		{"leading point", `.5`},
		{"leading plus", `+1`},
		{"NaN", `NaN`},
		{"unterminated string", `"abc`},
		{"unknown escape", `"\x"`},
		{"single quotes", `{'a':1}`},
		{"trailing comma", `[1,]`},
		{"bare word", `nul`},
		{"empty", ``},
	} {
		if _, err := Parse([]byte(c.doc)); err == nil {
			t.Errorf("%s: %q was accepted", c.name, c.doc)
		}
	}
}

func TestParseLimitsDepth(t *testing.T) {
	deep := strings.Repeat("[", 10) + strings.Repeat("]", 10)
	if _, err := ParseLimits([]byte(deep), Limits{MaxDepth: 9}); err == nil {
		t.Fatal("ten levels must exceed a limit of nine")
	}
	if _, err := ParseLimits([]byte(deep), Limits{MaxDepth: 10}); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse([]byte(strings.Repeat("[", 100000))); err == nil {
		t.Fatal("a hostile depth must be refused, not crash")
	}
}

func TestEqualIsJSONSchemaEquality(t *testing.T) {
	a, _ := Parse([]byte(`{"x":1,"y":[1.0,"s"]}`))
	b, _ := Parse([]byte(`{"y":[1,"s"],"x":1.000}`))
	c, _ := Parse([]byte(`{"y":["s",1],"x":1}`))
	if !Equal(a, b) {
		t.Fatal("1 equals 1.0, and member order does not matter")
	}
	if Equal(a, c) {
		t.Fatal("array order matters")
	}
}

func TestNumbers(t *testing.T) {
	for _, c := range []struct {
		n             Number
		integer, safe bool
	}{
		{"1", true, true},
		{"1.0", true, true},
		{"1e2", true, true},
		{"1.5", false, false},
		{"9007199254740991", true, true},
		{"9007199254740992", true, false},
		{"-9007199254740991", true, true},
		{"1e300", true, false},
	} {
		if got := c.n.IsInteger(); got != c.integer {
			t.Errorf("%s IsInteger = %v", c.n, got)
		}
		if got := c.n.SafeInteger(); got != c.safe {
			t.Errorf("%s SafeInteger = %v", c.n, got)
		}
	}
}

// The example of RFC 8785 §3.2.4.
func TestCanonicalRFC8785Example(t *testing.T) {
	v, err := Parse([]byte(esc(`{
  "numbers": [333333333.33333329, 1E30, 4.50, 2e-3, 0.000000000000000000000000001],
  "string": "%u20ac$%u000F%u000aA'%u0042%u0022%u005c\\\"\/",
  "literals": [null, true, false]
}`)))
	if err != nil {
		t.Fatal(err)
	}
	got, err := Canonical(v)
	if err != nil {
		t.Fatal(err)
	}
	want := esc(`{"literals":[null,true,false],"numbers":[333333333.3333333,1e+30,4.5,0.002,1e-27],"string":"€$%u000f\nA'B\"\\\\\"/"}`)
	if string(got) != want {
		t.Fatalf("canonical form\n got %s\nwant %s", got, want)
	}
}

// RFC 8785 §3.2.3 sorts by UTF-16 code units, not code points: U+1F600 (a
// surrogate pair, D83D DE00) sorts before U+FB33.
func TestCanonicalSortsByUTF16(t *testing.T) {
	v, err := Parse([]byte(esc(`{"%u20ac":"Euro Sign","\r":"Carriage Return","%ufb33":"Hebrew Letter Dalet With Dagesh","1":"One","%ud83d%ude00":"Emoji: Grinning Face","%u0080":"Control","%u00f6":"Latin Small Letter O With Diaeresis"}`)))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := Canonical(v)
	var order []string
	for _, part := range strings.Split(string(got), `",`) {
		_, value, _ := strings.Cut(part, `":"`)
		order = append(order, strings.TrimSuffix(value, `"}`))
	}
	want := []string{"Carriage Return", "One", "Control", "Latin Small Letter O With Diaeresis", "Euro Sign", "Emoji: Grinning Face", "Hebrew Letter Dalet With Dagesh"}
	if strings.Join(order, "|") != strings.Join(want, "|") {
		t.Fatalf("order\n got %v\nwant %v", order, want)
	}
}

// Number serialization per ECMA-262, as RFC 8785 Appendix B samples it.
func TestECMAScriptNumber(t *testing.T) {
	for _, c := range []struct {
		f    float64
		want string
	}{
		{0, "0"},
		{math.Copysign(0, -1), "0"},
		{5e-324, "5e-324"},
		{-5e-324, "-5e-324"},
		{math.MaxFloat64, "1.7976931348623157e+308"},
		{9007199254740992, "9007199254740992"},
		{295147905179352830000, "295147905179352830000"},
		{1e21, "1e+21"},
		{1e20, "100000000000000000000"},
		{9.999999999999997e22, "9.999999999999997e+22"},
		{1e23, "1e+23"},
		{0.000001, "0.000001"},
		{1e-7, "1e-7"},
		{9.999999999999997e-7, "9.999999999999997e-7"},
		{333333333.3333333, "333333333.3333333"},
		{4.5, "4.5"},
		{0.002, "0.002"},
		{-1.5, "-1.5"},
		{123456789012345680000, "123456789012345680000"},
	} {
		got, err := ECMAScriptNumber(c.f)
		if err != nil || got != c.want {
			t.Errorf("%v: got %q (%v), want %q", c.f, got, err, c.want)
		}
	}
	if _, err := ECMAScriptNumber(math.NaN()); err == nil {
		t.Error("NaN has no JSON form")
	}
}

func TestObjectEditing(t *testing.T) {
	o := NewObject()
	o.Set("b", 1)
	o.Set("a", 2)
	o.Set("b", 3)
	if strings.Join(o.Names(), ",") != "b,a" {
		t.Fatalf("Set keeps a member's place: %v", o.Names())
	}
	if !o.Delete("b") || o.Delete("b") || o.Len() != 1 {
		t.Fatal("Delete")
	}
	c := Clone(o).(*Object)
	c.Set("z", 1)
	if o.Len() != 1 {
		t.Fatal("Clone shares nothing")
	}
}

func FuzzParse(f *testing.F) {
	for _, seed := range []string{`{"a":[1,2.5e3,"xé",true,null]}`, `"😀"`, `[]`, `{"a":{"b":{"c":-0.0}}}`, `1e-7`} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		v, err := Parse(b)
		if err != nil {
			return
		}
		w, err := Marshal(v)
		if err != nil {
			t.Fatalf("a parsed value always marshals: %v", err)
		}
		v2, err := Parse(w)
		if err != nil || !Equal(v, v2) {
			t.Fatalf("Marshal does not round-trip: %s → %s (%v)", b, w, err)
		}
		c1, err := Canonical(v)
		if err != nil {
			t.Fatalf("a parsed value always canonicalises: %v", err)
		}
		v3, err := Parse(c1)
		if err != nil {
			t.Fatalf("the canonical form parses: %s: %v", c1, err)
		}
		c2, _ := Canonical(v3)
		if string(c1) != string(c2) {
			t.Fatalf("canonicalisation is idempotent: %s → %s", c1, c2)
		}
	})
}

// An *Object or a Number inside a Go value marshals as itself: members in
// their order, the number's own text.
func TestMarshalJSONKeepsOrderAndText(t *testing.T) {
	v, err := Parse([]byte(`{"z":1,"a":{"y":12345678901234567890.5,"b":[1e2]}}`))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(map[string]any{"doc": v, "n": Number("0.10")})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"doc":{"z":1,"a":{"y":12345678901234567890.5,"b":[1e2]}},"n":0.10}`; string(b) != want {
		t.Fatalf("got %s", b)
	}
	if _, err := json.Marshal(Number("1;")); err == nil {
		t.Fatal("a number that is not one was marshalled")
	}
}
