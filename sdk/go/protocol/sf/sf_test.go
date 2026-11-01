package sf

import (
	"bytes"
	"reflect"
	"testing"
)

// The Atlas headers of §9.6.3, as their own example writes them.
func TestAtlasHeaders(t *testing.T) {
	client, err := ParseDictionary(`name="fiatlas", version="2.3.0", protocol="atlas/1.0"`)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := client.Get("protocol"); v.(Item).Value != "atlas/1.0" {
		t.Fatalf("Atlas-Client protocol: %#v", v)
	}
	feats, err := ParseDictionary(`levels=(core streaming columnar bulk events handoff), handoff=(snowflake-arrow/1 reach-snowflake), lens=(rename alias_value convert_unit promote default drop), deliveries=(single chunked subscription operation transfer), encodings=(json ndjson arrow csv parquet)`)
	if err != nil {
		t.Fatal(err)
	}
	h, _ := feats.Get("handoff")
	if il := h.(InnerList); len(il.Items) != 2 || il.Items[0].Value != Token("snowflake-arrow/1") {
		t.Fatalf("handoff formats: %#v", h)
	}
	expect, err := ParseItem(`"portfolio.holdings@v1#bcaa5bbad97cc612"`)
	if err != nil || expect.Value != "portfolio.holdings@v1#bcaa5bbad97cc612" {
		t.Fatalf("Atlas-Profile-Expect: %#v %v", expect, err)
	}
	deadline, err := ParseItem(`@1793318400`)
	if err != nil || deadline.Value != Date(1793318400) {
		t.Fatalf("Atlas-Deadline: %#v %v", deadline, err)
	}
	left, err := ParseDictionary(`rate=58, concurrency=3`)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := left.Get("rate"); v.(Item).Value != int64(58) {
		t.Fatalf("Atlas-Limit-Remaining: %#v", left)
	}
	if it, err := ParseItem(`?1`); err != nil || it.Value != true {
		t.Fatalf("X-Atlas-Interactive: %#v %v", it, err)
	}
	if it, err := ParseItem(`1250.5`); err != nil || it.Value != Decimal(1250500) {
		t.Fatalf("Atlas-Confirm: %#v %v", it, err)
	}
	if it, err := ParseItem(`limit_exceeded`); err != nil || it.Value != Token("limit_exceeded") {
		t.Fatalf("Atlas-Error-Code: %#v %v", it, err)
	}
}

func TestRFC9651Examples(t *testing.T) {
	l, err := ParseList(`sugar, tea, rum`)
	if err != nil || len(l) != 3 {
		t.Fatalf("a list: %v %v", l, err)
	}
	l, err = ParseList(`("foo" "bar"), ("baz"), ("bat" "one"), ()`)
	if err != nil || len(l) != 4 || len(l[3].(InnerList).Items) != 0 {
		t.Fatalf("inner lists: %#v %v", l, err)
	}
	l, err = ParseList(`("foo"; a=1;b=2);lvl=5, ("bar" "baz");lvl=1`)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := l[0].(InnerList).Params.Get("lvl"); v != int64(5) {
		t.Fatalf("inner-list parameters: %#v", l[0])
	}
	d, err := ParseDictionary(`en="Applepie", da=:w4ZibGV0w6ZydGU=:`)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := d.Get("da"); !bytes.Equal(v.(Item).Value.([]byte), []byte("\xc3\x86blet\xc3\xa6rte")) {
		t.Fatalf("a byte sequence: %#v", v)
	}
	d, err = ParseDictionary(`a=?0, b, c; foo=bar`)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := d.Get("b"); v.(Item).Value != true {
		t.Fatalf("a bare key is true: %#v", v)
	}
	if v, _ := d.Get("c"); v.(Item).Value != true || len(v.(Item).Params) != 1 {
		t.Fatalf("a bare key with parameters: %#v", v)
	}
	ds, err := ParseItem(`%"This is intended for display to %c3%bcsers."`)
	if err != nil || ds.Value != DisplayString("This is intended for display to üsers.") {
		t.Fatalf("a display string: %#v %v", ds, err)
	}
	if d, err := ParseDictionary(`a=1, b=2, a=3`); err != nil || len(d) != 2 || d[0].Key != "a" {
		t.Fatalf("a repeated key keeps its place and its last value: %#v %v", d, err)
	} else if v, _ := d.Get("a"); v.(Item).Value != int64(3) {
		t.Fatalf("a repeated key keeps its last value: %#v", d)
	}
}

// Every one of these is refused whether it is read as an Item, a List or a
// Dictionary.
func TestRefusals(t *testing.T) {
	for _, c := range []struct{ name, field string }{
		{"integer too long", `1234567890123456`},
		{"decimal with four fraction digits", `1.2345`},
		{"decimal with thirteen integer digits", `1234567890123.1`},
		{"decimal ending in a point", `1.`},
		{"lone minus", `-`},
		{"date without a number", `@`},
		{"string with a bad escape", `"a\b"`},
		{"unterminated string", `"abc`},
		{"non-ASCII", "\"caf\xc3\xa9\""},
		{"token starting with a digit", `1abc;x`},
		{"boolean other than 0 and 1", `?2`},
		{"date with a fraction", `@1.5`},
		{"display string with an uppercase escape", `%"%C3%BC"`},
		{"display string that is not UTF-8", `%"%ff"`},
		{"display string with a short escape", `%"%c"`},
		{"key with an uppercase letter", `A=1`},
		{"trailing comma", `a, b,`},
		{"inner list unterminated", `(a b`},
		{"inner list items run together", `(a"b")`},
		{"data after an item", `a b`},
		{"byte sequence that is not base64", `:a*b:`},
		{"unterminated byte sequence", `:aGk=`},
		{"surplus base64 padding", `:aGk==:`},
	} {
		if _, err := ParseItem(c.field); err == nil {
			t.Errorf("%s: %q accepted as an item", c.name, c.field)
		}
		if _, err := ParseList(c.field); err == nil {
			t.Errorf("%s: %q accepted as a list", c.name, c.field)
		}
		if _, err := ParseDictionary(c.field); err == nil {
			t.Errorf("%s: %q accepted as a dictionary", c.name, c.field)
		}
	}
}

func TestSerialize(t *testing.T) {
	d := Dictionary{
		{"levels", InnerList{Items: []Item{{Value: Token("core")}, {Value: Token("columnar")}}}},
		{"interactive", Item{Value: true}},
		{"confirm", Item{Value: Decimal(1250500)}},
		{"name", Item{Value: `fi"atlas`}},
		{"deadline", Item{Value: Date(1793318400), Params: Params{{"strict", true}}}},
		{"label", Item{Value: DisplayString("üsers 100%")}},
	}
	got, err := d.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	want := `levels=(core columnar), interactive, confirm=1250.5, name="fi\"atlas", deadline=@1793318400;strict, label=%"%c3%bcsers 100%25"`
	if got != want {
		t.Fatalf("serialised\n got %s\nwant %s", got, want)
	}
	back, err := ParseDictionary(got)
	if err != nil || !reflect.DeepEqual(back, d) {
		t.Fatalf("round trip: %#v %v", back, err)
	}
}

func TestSerializeItemsAndLists(t *testing.T) {
	got, err := Item{Value: Date(1793318400)}.Serialize()
	if err != nil || got != "@1793318400" {
		t.Fatalf("an item: %q %v", got, err)
	}
	got, err = List{Item{Value: Token("core")}, InnerList{Items: []Item{{Value: int64(1)}, {Value: Decimal(-500)}}, Params: Params{{"q", Decimal(9)}}}}.Serialize()
	if err != nil || got != "core, (1 -0.5);q=0.009" {
		t.Fatalf("a list: %q %v", got, err)
	}
	for _, bad := range []Item{
		{Value: int64(1_000_000_000_000_000)},
		{Value: Decimal(1_000_000_000_000_000)},
		{Value: "caf\xc3\xa9"},
		{Value: Token("1abc")},
		{Value: DisplayString("\xff")},
		{Value: 1.5},
		{Value: true, Params: Params{{"Bad", true}}},
	} {
		if _, err := bad.Serialize(); err == nil {
			t.Errorf("%#v serialised", bad)
		}
	}
}

// FuzzFields checks that whatever parses serialises, and that serialising is
// canonical: the serialisation parses back to itself.
func FuzzFields(f *testing.F) {
	for _, s := range []string{
		`levels=(core columnar), handoff=(snowflake-arrow/1)`,
		`a=?0, b, c; foo=bar`,
		`x=@1793318400, y=%"%c3%bc", z=:aGk=:`,
		`rate=58, concurrency=3`,
		`("foo"; a=1;b=2);lvl=5, ("bar" "baz");lvl=1`,
		`"portfolio.holdings@v1#bcaa5bbad97cc612"`,
		`-12.250;q`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		type field interface{ Serialize() (string, error) }
		parsers := map[string]func(string) (field, error){
			"item":       func(s string) (field, error) { return ParseItem(s) },
			"list":       func(s string) (field, error) { return ParseList(s) },
			"dictionary": func(s string) (field, error) { return ParseDictionary(s) },
		}
		for kind, parse := range parsers {
			v, err := parse(s)
			if err != nil {
				continue
			}
			out, err := v.Serialize()
			if err != nil {
				t.Fatalf("a parsed %s always serialises: %q: %v", kind, s, err)
			}
			again, err := parse(out)
			if err != nil {
				t.Fatalf("a serialised %s parses: %q: %v", kind, out, err)
			}
			if out2, _ := again.Serialize(); out != out2 {
				t.Fatalf("serialising a %s is canonical: %q then %q", kind, out, out2)
			}
		}
	})
}
