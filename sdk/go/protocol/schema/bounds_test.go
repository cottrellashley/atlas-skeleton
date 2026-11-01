package schema

import (
	"cmp"
	"fmt"
	"math"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"atlas.local/sdk/go/protocol/grammar"
	"atlas.local/sdk/go/protocol/ijson"
)

// The body limit and nesting the edge parses a request within
// (ATLAS-MECH-002, gateway/edge).
const (
	bodyLimit = 1 << 20
	bodyDepth = 64
)

// holdings is portfolio.holdings@v1's request as its profile writes it: a
// closed object whose portfolios are one to a hundred codes, each once,
// and whose columns are unbounded, each once.
const holdings = `"type": "object", "additionalProperties": false, "required": ["portfolios"], "properties": {
	"portfolios": {"type": "array", "minItems": 1, "maxItems": 100, "uniqueItems": true, "items": {"type": "string", "x-atlas-logical": "PortfolioCode"}},
	"as_of": {"type": ["string", "null"], "format": "date", "default": null},
	"columns": {"type": ["array", "null"], "uniqueItems": true, "default": null, "items": {"type": "string", "minLength": 1}}}`

// Requests with maps, which a request schema may hold (Q-PROF-7): their
// member names are the caller's, as long as the body allows.
const (
	mapOfLists   = `"type": "object", "additionalProperties": false, "properties": {"m": {"type": "object", "additionalProperties": {"type": "array", "items": {"type": "string"}}}}`
	mapOfStrings = `"type": "object", "additionalProperties": false, "properties": {"m": {"type": "object", "additionalProperties": {"type": "string"}}}`
)

// fill writes head, then at most limit items while the body stays inside
// the body limit, then tail; it returns the body and the items written.
func fill(head string, limit int, item func(i int) string, tail string) ([]byte, int) {
	b := []byte(head)
	n := 0
	for ; n < limit; n++ {
		next := item(n)
		if len(b)+1+len(next)+len(tail) > bodyLimit {
			break
		}
		if n > 0 {
			b = append(b, ',')
		}
		b = append(b, next...)
	}
	return append(b, tail...), n
}

// list is {"portfolios": ["A"], "<member>": [item(0), …]}, or for
// portfolios itself {"portfolios": [item(0), …]}, parsed.
func list(t testing.TB, member string, n int, item func(i int) string) ijson.Value {
	t.Helper()
	head := `{"portfolios": ["A"], ` + strconv.Quote(member) + `: [`
	if member == "portfolios" {
		head = `{"portfolios": [`
	}
	b, _ := fill(head, n, item, "]}")
	v, err := ijson.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func pairs(fs []Failure) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Pointer + " " + f.Keyword
	}
	return out
}

// work validates in full within the bounds and returns the work done.
func work(s *Schema, v ijson.Value) ([]Failure, bool, int) {
	return s.n.failures(v, MaxFailures, MaxFailureBytes, MaxPointerBytes)
}

// unbounded is every failure, sorted, as validation finds them with no
// bound but on a pointer's length: what a bounded validation's result
// must begin with.
func unbounded(s *Schema, v ijson.Value, pointer int) []Failure {
	fs, _, _ := s.n.failures(v, math.MaxInt, math.MaxInt, pointer)
	return fs
}

// values counts the JSON values of an instance.
func values(v ijson.Value) int {
	n := 1
	switch x := v.(type) {
	case []ijson.Value:
		for _, e := range x {
			n += values(e)
		}
	case *ijson.Object:
		x.Each(func(_ string, m ijson.Value) bool { n += values(m); return true })
	}
	return n
}

// written is what a validation's failures count against its byte budget:
// what the contract carries, their pointers and keywords.
func written(fs []Failure) int {
	n := 0
	for _, f := range fs {
		n += len(f.Pointer) + len(f.Keyword)
	}
	return n
}

func before(a, b Failure) int {
	return cmp.Or(strings.Compare(a.Pointer, b.Pointer), strings.Compare(a.Keyword, b.Keyword))
}

// R049: a request inside the body limit can fail in every value it holds:
// sixteen thousand portfolios that are numbers are sixteen thousand type
// failures and a maxItems. Validation reports the first hundred by pointer
// then keyword (Q-VAL-1, assumption A-009): exactly what sorting every
// failure would put first.
func TestR049ReportsTheFirstHundred(t *testing.T) {
	s := compile(t, Request, holdings)
	const n = 16000
	got, more := s.Failures(list(t, "portfolios", n, strconv.Itoa))
	items := make([]string, n)
	for i := range items {
		items[i] = "/portfolios/" + strconv.Itoa(i)
	}
	sort.Strings(items) // /portfolios/0, /portfolios/1, /portfolios/10, …
	want := []string{"/portfolios maxItems"}
	for _, p := range items[:99] {
		want = append(want, p+" type")
	}
	if !slices.Equal(pairs(got), want) || !more {
		t.Fatalf("%d failures (more: %v); the first hundred by pointer are wanted\n got: %.300s\nwant: %.300s", len(got), more, strings.Join(pairs(got), "; "), strings.Join(want, "; "))
	}
	if !slices.Equal(s.Validate(list(t, "portfolios", n, strconv.Itoa)), got) {
		t.Fatal("Validate is Failures' failures")
	}
}

// R049, assumption A-193: the worst requests the body limit admits, each
// a different way to make validation work: a failure in every value,
// every item the same, a hundred thousand distinct items for uniqueItems,
// a repeat at the very end, one long member name above a quarter of a
// million values (reported once, at the deepest value above it whose
// pointer is within MaxPointerBytes), members whose names are as long as
// the pointer bound allows (the byte budget cuts the list), a name of a
// megabyte of slashes (each written "~1"), members in falling order (each
// failure sorts before all those kept), and a hundred thousand unknown
// members. For each: the first failures by (pointer, keyword), at most
// MaxFailures of them, within MaxFailureBytes past the first, at pointers
// within MaxPointerBytes; work linear in the values the request holds;
// memory a small multiple of the body.
func TestR049WorstCasesInsideTheBodyLimit(t *testing.T) {
	long := strings.Repeat("x", bodyLimit/2)
	sortedItems := func(prefix string, n int, keyword string) []string {
		ps := make([]string, n)
		for i := range ps {
			ps[i] = prefix + strconv.Itoa(i)
		}
		sort.Strings(ps)
		for i := range ps {
			ps[i] += " " + keyword
		}
		return ps
	}
	cases := []struct {
		name, schema, head, tail string
		limit                    int
		item                     func(i int) string
		// first are the leading failures wanted, failing how many there
		// are, of n items.
		first   func(n int) []string
		failing func(n int) int
	}{{
		name: "every portfolio a number", schema: holdings, head: `{"portfolios": [`, tail: `]}`, limit: math.MaxInt,
		item: strconv.Itoa,
		first: func(n int) []string {
			return append([]string{"/portfolios maxItems"}, sortedItems("/portfolios/", n, "type")[:99]...)
		},
		failing: func(n int) int { return n + 1 },
	}, {
		name: "every portfolio the same number", schema: holdings, head: `{"portfolios": [`, tail: `]}`, limit: math.MaxInt,
		item: func(int) string { return "0" },
		first: func(n int) []string {
			return append([]string{"/portfolios maxItems", "/portfolios uniqueItems"}, sortedItems("/portfolios/", n, "type")[:98]...)
		},
		failing: func(n int) int { return n + 2 },
	}, {
		name: "a hundred thousand distinct columns", schema: holdings, head: `{"portfolios": ["A"], "columns": [`, tail: `]}`, limit: 100000,
		item:    func(i int) string { return `"c` + strconv.Itoa(i) + `"` },
		first:   func(int) []string { return nil },
		failing: func(int) int { return 0 },
	}, {
		name: "distinct columns but the last", schema: holdings, head: `{"portfolios": ["A"], "columns": [`, tail: `]}`, limit: 100000,
		item: func(i int) string {
			if i == 99999 {
				return `"c0"`
			}
			return `"c` + strconv.Itoa(i) + `"`
		},
		first:   func(int) []string { return []string{"/columns uniqueItems"} },
		failing: func(int) int { return 1 },
	}, {
		name: "a long name above many values", schema: mapOfLists, head: `{"m": {"` + long + `": [`, tail: `]}}`, limit: math.MaxInt,
		item:    func(int) string { return "0" },
		first:   func(int) []string { return []string{"/m type"} }, // every one is reported at /m, once
		failing: func(int) int { return 1 },
	}, {
		name: "names as long as the pointer bound allows", schema: mapOfStrings, head: `{"m": {`, tail: `}}`, limit: math.MaxInt,
		item: func(i int) string { return fmt.Sprintf(`"%0*d": 0`, MaxPointerBytes-len("/m/"), i) },
		first: func(int) []string {
			var out []string
			for i := range MaxFailureBytes / (MaxPointerBytes + len("type")) { // 63 fit
				out = append(out, fmt.Sprintf("/m/%0*d type", MaxPointerBytes-len("/m/"), i))
			}
			return out
		},
		failing: func(n int) int { return n },
	}, {
		name: "a megabyte name of slashes", schema: holdings, head: `{"portfolios": ["A"], `, tail: `}`, limit: 1,
		item:    func(int) string { return `"` + strings.Repeat("/", bodyLimit-64) + `": 0` },
		first:   func(int) []string { return []string{" additionalProperties"} }, // at the instance itself
		failing: func(int) int { return 1 },
	}, {
		name: "members in falling order", schema: mapOfStrings, head: `{"m": {`, tail: `}}`, limit: 70000,
		item: func(i int) string { return fmt.Sprintf(`"k%07d": 0`, 70000-1-i) },
		first: func(int) []string {
			var out []string
			for i := range MaxFailures {
				out = append(out, fmt.Sprintf("/m/k%07d type", i))
			}
			return out
		},
		failing: func(n int) int { return n },
	}, {
		name: "a hundred thousand unknown members", schema: holdings, head: `{"portfolios": ["A"], `, tail: `}`, limit: 100000,
		item: func(i int) string { return fmt.Sprintf(`"x%07d": 0`, i) },
		first: func(int) []string {
			var out []string
			for i := range MaxFailures {
				out = append(out, fmt.Sprintf("/x%07d additionalProperties", i))
			}
			return out
		},
		failing: func(n int) int { return n },
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := compile(t, Request, c.schema)
			body, n := fill(c.head, c.limit, c.item, c.tail)
			v, err := ijson.ParseLimits(body, ijson.Limits{MaxBytes: bodyLimit, MaxDepth: bodyDepth})
			if err != nil {
				t.Fatal(err)
			}
			var m0, m1 runtime.MemStats
			runtime.ReadMemStats(&m0)
			fs, more, steps := work(s, v)
			runtime.ReadMemStats(&m1)
			want, failing := c.first(n), c.failing(n)
			if !slices.Equal(pairs(fs), want) {
				t.Errorf("%d failures of %d values\n got: %.300s\nwant: %.300s", len(fs), n, strings.Join(pairs(fs), "; "), strings.Join(want, "; "))
			}
			if more != (failing > len(fs)) || s.Valid(v) != (failing == 0) {
				t.Errorf("more %v and valid %v, with %d failures of which %d are reported", more, s.Valid(v), failing, len(fs))
			}
			if len(fs) > MaxFailures || len(fs) > 1 && written(fs) > MaxFailureBytes {
				t.Errorf("%d failures, %d bytes", len(fs), written(fs))
			}
			for _, f := range fs {
				if len(f.Pointer) > MaxPointerBytes || len(f.Message) > MaxMessageBytes+len("…") {
					t.Errorf("a pointer of %d bytes, a message of %d", len(f.Pointer), len(f.Message))
				}
			}
			if values := values(v); steps > 16*values {
				t.Errorf("%d steps for %d values: validation is not linear in the request", steps, values)
			}
			if alloc := m1.TotalAlloc - m0.TotalAlloc; alloc > 32*uint64(len(body)) {
				t.Errorf("%d bytes allocated for a body of %d", alloc, len(body))
			}
		})
	}
}

// R049: the work grows as the request does, never as its square: four
// times the items is at most a little more than four times the steps, for
// a failure in every item, for uniqueItems over distinct items, and for
// failures that each sort before all those kept.
func TestR049WorkIsLinear(t *testing.T) {
	shapes := []struct {
		name, schema, member string
		item                 func(n int) func(i int) string
	}{
		{"a failure in every item", holdings, "portfolios", func(int) func(int) string { return strconv.Itoa }},
		{"distinct items, each once", holdings, "columns", func(int) func(int) string {
			return func(i int) string { return `"c` + strconv.Itoa(i) + `"` }
		}},
		{"failures in falling order", holdings, "columns", func(n int) func(int) string {
			return func(i int) string { return strconv.Itoa(n - i) }
		}},
	}
	for _, sh := range shapes {
		s := compile(t, Request, sh.schema)
		steps := func(n int) int {
			_, _, st := work(s, list(t, sh.member, n, sh.item(n)))
			return st
		}
		if small, large := steps(5000), steps(20000); float64(large) > 4.5*float64(small) {
			t.Errorf("%s: %d steps for 5000 items, %d for 20000", sh.name, small, large)
		}
	}
}

// R049: Valid stops at the first failure, however many follow it.
func TestR049ValidStopsAtTheFirstFailure(t *testing.T) {
	s := compile(t, Request, holdings)
	v := list(t, "portfolios", 100000, strconv.Itoa)
	c := collector{first: true}
	s.n.validate(v, nil, &c)
	if len(c.kept) != 1 || c.steps > 10 || s.Valid(v) {
		t.Fatalf("%d failures kept in %d steps", len(c.kept), c.steps)
	}
}

// pathOf extends base by segments: an int is an item, a string a member.
func pathOf(base *path, segments ...any) *path {
	p := base
	for _, s := range segments {
		var q path
		switch x := s.(type) {
		case int:
			q = p.next("", x)
		case string:
			q = p.next(x, -1)
		}
		p = &q
	}
	return p
}

// R049, Q-VAL-1: comparing paths without writing them orders them exactly
// as their pointers' code points do: /10 before /2, a member before what
// is beneath it, and a name's characters as RFC 6901 escapes them, which
// puts "a!" and "a-b" between "a" and "a/0", and "~" after "a".
func TestR049PathsOrderAsTheirPointers(t *testing.T) {
	a := pathOf(nil, "a")
	paths := []*path{
		nil, a, pathOf(a, 0), pathOf(a, 1), pathOf(a, 10), pathOf(a, 2), pathOf(a, "b"), pathOf(a, "~0"), pathOf(a, 10, "x"),
		pathOf(nil, "a", 0), pathOf(nil, "a!"), pathOf(nil, "a-b"), pathOf(nil, "a~"), pathOf(nil, "a~1"), pathOf(nil, "a/b"),
		pathOf(nil, "a.b"), pathOf(nil, "a b"), pathOf(nil, "a\x00"), pathOf(nil, "ab"), pathOf(nil, "~"), pathOf(nil, "/"),
		pathOf(nil, ""), pathOf(nil, "", ""), pathOf(nil, 0), pathOf(nil, 1), pathOf(nil, 10), pathOf(nil, 2), pathOf(nil, 1, 0),
		pathOf(nil, 10, 1), pathOf(nil, 100), pathOf(nil, 19), pathOf(nil, "é"), pathOf(nil, "z"), pathOf(nil, "1"),
	}
	for _, p := range paths {
		for _, q := range paths {
			if got, want := comparePaths(p, q), strings.Compare(p.pointer(), q.pointer()); got != want {
				t.Errorf("%q against %q: %d, the pointers %d", p.pointer(), q.pointer(), got, want)
			}
		}
	}
}

// segmentsOf reads a fuzzed path: segments split at "|", "#<digits>" an
// item's index and anything else a member's name.
func segmentsOf(s string) []any {
	var out []any
	for _, seg := range strings.Split(s, "|") {
		if d, ok := strings.CutPrefix(seg, "#"); ok && d != "" && len(d) < 7 && (d == "0" || d[0] != '0') && strings.Trim(d, "0123456789") == "" {
			i, _ := strconv.Atoi(d)
			out = append(out, i)
			continue
		}
		out = append(out, seg)
	}
	return out
}

// R049, Q-VAL-1: whatever the segments, comparing paths agrees with
// comparing their pointers, whether the paths share their common part or
// each holds a copy of it.
func FuzzPathOrder(f *testing.F) {
	f.Add("a", "a!|x", "")
	f.Add("#1|#0", "#10", "root")
	f.Add("a~", "a/b|#2", "m|#3")
	f.Add("", "|", "~1")
	f.Fuzz(func(t *testing.T, a, b, shared string) {
		common := pathOf(nil, segmentsOf(shared)...)
		pa, pb := pathOf(common, segmentsOf(a)...), pathOf(common, segmentsOf(b)...)
		copied := pathOf(pathOf(nil, segmentsOf(shared)...), segmentsOf(b)...)
		for _, q := range []*path{pb, copied} {
			if got, want := comparePaths(pa, q), strings.Compare(pa.pointer(), q.pointer()); got != want {
				t.Fatalf("%q against %q: %d, the pointers %d", pa.pointer(), q.pointer(), got, want)
			}
		}
		if comparePaths(pb, copied) != 0 || pb.size() != len(pb.pointer()) {
			t.Fatalf("%q: a copy compares equal, and size is the pointer's length", pb.pointer())
		}
	})
}

// firstRepeat is uniqueItems by every pair, as before R049.
func firstRepeat(a []ijson.Value) (int, int, bool) {
	for i := 1; i < len(a); i++ {
		for j := range i {
			if ijson.Equal(a[j], a[i]) {
				return j, i, true
			}
		}
	}
	return 0, 0, false
}

// R049, assumption A-014: uniqueItems hashes items as ijson.Equal compares
// them: 1 and 1.0, 0 and -0, 0.1 and its longer spelling, objects in any
// member order, arrays of equal items are each a repeat; 1 and "1", a
// missing member and a null one, [] and {} are not. It finds the same
// first repeat as comparing every pair does.
func TestR049UniqueItemsHashesAsEqualCompares(t *testing.T) {
	equal := [][2]string{{`1`, `1.0`}, {`0`, `-0`}, {`0.1`, `0.10000000000000000001`}, {`{"a": 1, "b": [2]}`, `{"b": [2.0], "a": 1}`},
		{`[1, [2, {}]]`, `[1.0, [2e0, {}]]`}, {`"x"`, `"x"`}, {`null`, `null`}, {`1e-400`, `0`}}
	distinct := [][2]string{{`1`, `"1"`}, {`{"a": 1}`, `{"a": 1, "b": null}`}, {`[]`, `{}`}, {`null`, `false`}, {`[1, 2]`, `[2, 1]`}, {`"é"`, `"e"`}}
	for _, pair := range append(equal, distinct...) {
		var items []string
		for i := range 3 * smallArray {
			items = append(items, fmt.Sprintf(`{"filler": %d}`, i))
		}
		items[5], items[2*smallArray] = pair[0], pair[1]
		v, err := ijson.Parse([]byte("[" + strings.Join(items, ",") + "]"))
		if err != nil {
			t.Fatal(err)
		}
		a := v.([]ijson.Value)
		j, i, ok := repeated(a, &collector{})
		wj, wi, wok := firstRepeat(a)
		if j != wj || i != wi || ok != wok || ok != slices.Contains(equal, pair) {
			t.Errorf("%s and %s: repeated gives %d, %d, %v; every pair gives %d, %d, %v", pair[0], pair[1], j, i, ok, wj, wi, wok)
		}
	}
}

// R049: whatever the array, hashing finds the first repeat comparing every
// pair would.
func FuzzUniqueItems(f *testing.F) {
	f.Add(`[1, 2, 1.0]`)
	f.Add(`[{"a": [1, {"b": null}]}, {"a": [1.0, {"b": null}]}, -0, 0]`)
	f.Add(`["a", "b", ["a"], {"a": "a"}, ["a"]]`)
	f.Fuzz(func(t *testing.T, instance string) {
		v, err := ijson.Parse([]byte(instance))
		a, isArray := v.([]ijson.Value)
		if err != nil || !isArray {
			return
		}
		for i := range smallArray + 1 { // past smallArray, so the items are hashed
			a = append(a, "filler "+strconv.Itoa(i))
		}
		j, i, ok := repeated(a, &collector{})
		wj, wi, wok := firstRepeat(a)
		if j != wj || i != wi || ok != wok {
			t.Fatalf("%s: repeated gives %d, %d, %v; every pair gives %d, %d, %v", instance, j, i, ok, wj, wi, wok)
		}
	})
}

// R049: an int64 and a duration are read with big.Int, whose time grows
// with the square of a number's digits: a megabyte of digits took half a
// second. The validator refuses what is longer than either can be before
// the grammar reads it, and still accepts every long spelling the grammar
// does, such as leading zeros.
func TestR049FormatsReadLongNumbersInLinearTime(t *testing.T) {
	digits := strings.Repeat("7", bodyLimit)
	cases := []struct{ format, value, why string }{
		{"int64", digits, "more than 20 characters"},
		{"int64", "-" + strings.Repeat("1", 20), "more than 20 characters"},
		{"int64", "-9223372036854775808", ""},
		{"int64", "9223372036854775807", ""},
		{"int64", "9223372036854775808", "outside a signed 64-bit integer"},
		{"duration", "P" + digits + "D", "more than 19 significant digits"},
		{"duration", "PT1." + digits + "S", "more than 19 significant digits"},
		{"duration", "P" + strings.Repeat("0", bodyLimit) + "1D", ""},
		{"duration", "PT" + strings.Repeat("9", 19) + "S", "too long"},
	}
	for _, c := range cases {
		why := checkFormat(c.format, c.value, nil)
		if c.why == "" && why != "" || !strings.Contains(why, c.why) || len(why) > 2*MaxMessageBytes {
			t.Errorf("%s %.30s…: %q, wanted %q", c.format, c.value, why, c.why)
		}
	}
	// The duration's bound is the grammar's own: a number of more than 19
	// significant digits is past time.Duration, which the grammar refuses.
	for _, d := range []string{"P" + strings.Repeat("9", 20) + "D", "PT1" + strings.Repeat("0", 19) + "S", "PT1." + strings.Repeat("1", 20) + "S", "P1DT" + strings.Repeat("9", 20) + "H"} {
		if _, err := grammar.ParseDuration(d); err == nil || significantDigits(d) <= maxDurationDigit {
			t.Errorf("%s: the grammar accepts what the validator refuses unread", d)
		}
	}
}

// R049: a message quotes at most a few characters of what it is about,
// cut between characters, however long the member name, string or number:
// only the pointer and the keyword are the contract.
func TestR049MessagesAreClipped(t *testing.T) {
	s := compile(t, Request, `"type": "object", "additionalProperties": false, "properties": {
		"s": {"type": "string", "pattern": "^[a-z]$", "format": "date", "maxLength": 3},
		"n": {"type": "string"}, "i": {"type": "string", "format": "int64"}}`)
	long := strings.Repeat("é", 1000)
	v := parse(t, `{"`+long+`": 1, "s": "`+long+`", "n": 1.`+strings.Repeat("0", 10000)+`, "i": "`+long+`"}`)
	fs := s.Validate(v)
	if len(fs) != 6 {
		t.Fatalf("%v", pairs(fs))
	}
	for _, f := range fs {
		if len(f.Message) > MaxMessageBytes+len("…") || !utf8.ValidString(f.Message) {
			t.Errorf("%s %s: a message of %d bytes: %.80s", f.Pointer[:min(len(f.Pointer), 20)], f.Keyword, len(f.Message), f.Message)
		}
	}
}

// R049, Q-VAL-1: whatever the schema and the instance, the bounded result
// begins every failure sorted, holds at most its bounds, says there is
// more exactly when it leaves a failure out, and agrees with Valid. Every
// failure sorted is itself in order, each pair once: comparing paths is
// comparing pointers. Small bounds make the walk pass values over.
func FuzzValidateBounded(f *testing.F) {
	f.Add(`"type": "object", "properties": {"a": {"type": "array", "items": {"type": "string"}}, "a!": {"type": "string"}, "a-b": {"type": "integer"}}`,
		`{"a": [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12], "a!": 1, "a-b": "x"}`)
	f.Add(`"type": "array", "uniqueItems": true, "maxItems": 3, "items": {"type": "number", "maximum": 5}`,
		`[1, 1.0, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 2.0]`)
	f.Add(`"type": "object", "additionalProperties": false, "properties": {"a": {"type": "string"}}`,
		`{"~": 1, "/": 2, "~1": 3, "a/b": 4, "a~b": 5, "ab": 6, "a": 7}`)
	f.Add(`"$defs": {"d": {"type": "string", "minLength": 3}}, "type": "object", "properties": {"x": {"$ref": "#/$defs/d"}, "y": {"allOf": [{"$ref": "#/$defs/d"}, {"type": "string", "pattern": "^a$"}]}}`,
		`{"x": "a", "y": 1}`)
	f.Add(holdings, `{"portfolios": [0, 0, "", 1], "columns": ["", "", 3], "as_of": "x", "z": null}`)
	// Two subschemas reach /x, the second after a later failure fills the
	// bound: /x maximum still comes first, though /x type was kept first.
	f.Add(`"type": "object", "allOf": [{"type": "object", "properties": {"x": {"type": "string"}, "y": {"type": "string"}}}, {"type": "object", "properties": {"x": {"type": "number", "maximum": 1}}}]`,
		`{"x": 5, "y": 1}`)
	// /b type is found again after /c type: once, though no longer last.
	f.Add(`"type": "object", "allOf": [{"type": "object", "properties": {"b": {"type": "string"}, "c": {"type": "string"}}}, {"type": "object", "properties": {"b": {"type": "string"}}}]`,
		`{"b": 1, "c": 1}`)
	f.Fuzz(func(t *testing.T, members, instance string) {
		sv, err := ijson.Parse([]byte("{" + head + ", " + members + "}"))
		if err != nil {
			return
		}
		in, err := ijson.Parse([]byte(instance))
		if err != nil {
			return
		}
		for _, o := range []struct {
			kind Kind
			opt  Options
		}{{Open, Options{Unchecked: true}}, {Request, Options{}}, {Open, Options{}}} {
			s, err := CompileWith(sv, o.kind, o.opt)
			if err != nil {
				continue
			}
			all := unbounded(s, in, MaxPointerBytes)
			for i := 1; i < len(all); i++ {
				if before(all[i-1], all[i]) >= 0 {
					t.Fatalf("out of order: %s %s, then %s %s", all[i-1].Pointer, all[i-1].Keyword, all[i].Pointer, all[i].Keyword)
				}
			}
			if s.Valid(in) != (len(all) == 0) {
				t.Fatalf("Valid is %v with %d failures", s.Valid(in), len(all))
			}
			for _, b := range []struct{ max, budget int }{{MaxFailures, MaxFailureBytes}, {1, MaxFailureBytes}, {3, MaxFailureBytes}, {MaxFailures, 40}} {
				got, more, _ := s.n.failures(in, b.max, b.budget, MaxPointerBytes)
				switch {
				case len(got) > b.max, len(got) > 1 && written(got) > b.budget:
					t.Fatalf("%d failures, %d bytes, past %d and %d", len(got), written(got), b.max, b.budget)
				case len(all) > 0 && len(got) == 0, len(got) > len(all) || !slices.Equal(got, all[:len(got)]):
					t.Fatalf("not the first of every failure:\n got: %v\nevery: %v", got, all)
				case more != (len(got) < len(all)):
					t.Fatalf("more is %v, with %d of %d failures", more, len(got), len(all))
				}
			}
		}
	})
}

// R049: what the worst requests cost, for reading rather than asserting:
// a megabyte of numbers each failing, and of distinct columns for
// uniqueItems. Before R049 a sixteenth of the first took three seconds.
func BenchmarkR049WorstCases(b *testing.B) {
	s, err := Compile(schemaOf(b, holdings), Request)
	if err != nil {
		b.Fatal(err)
	}
	for _, c := range []struct{ name, head string }{
		{"numbers", `{"portfolios": [`},
		{"columns", `{"portfolios": ["A"], "columns": [`},
	} {
		body, _ := fill(c.head, math.MaxInt, func(i int) string { return `"c` + strconv.Itoa(i) + `"` }, "]}")
		if c.name == "numbers" {
			body, _ = fill(c.head, math.MaxInt, strconv.Itoa, "]}")
		}
		v, err := ijson.Parse(body)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(c.name, func(b *testing.B) {
			b.SetBytes(int64(len(body)))
			for b.Loop() {
				s.Failures(v)
			}
		})
	}
}
