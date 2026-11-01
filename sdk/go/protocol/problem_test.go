package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"atlas.local/sdk/go/internal/refs"
	"atlas.local/sdk/go/protocol/documents"
	"atlas.local/sdk/go/protocol/ijson"
	"atlas.local/sdk/go/protocol/schema"
)

// holdingsRequest is portfolio.holdings@v1's request, as its profile
// writes it.
const holdingsRequest = `{"$schema": "https://json-schema.org/draft/2020-12/schema",
	"$vocabulary": {"https://json-schema.org/draft/2020-12/vocab/core": true, "https://json-schema.org/draft/2020-12/vocab/applicator": true, "https://json-schema.org/draft/2020-12/vocab/validation": true, "https://json-schema.org/draft/2020-12/vocab/meta-data": true, "https://json-schema.org/draft/2020-12/vocab/format-assertion": true, "https://atlas.jhg.com/vocab/2026-10": false},
	"type": "object", "additionalProperties": false, "required": ["portfolios"], "properties": {
	"portfolios": {"type": "array", "minItems": 1, "maxItems": 100, "uniqueItems": true, "items": {"type": "string", "x-atlas-logical": "PortfolioCode"}},
	"as_of": {"type": ["string", "null"], "format": "date", "default": null}}}`

func mustParse(t *testing.T, s string) ijson.Value {
	t.Helper()
	v, err := ijson.Parse([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// wireDetails is a problem as the wire carries it: its target and each
// detail's members.
func wireDetails(t *testing.T, p *Problem) (string, []map[string]any) {
	t.Helper()
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var w struct {
		Target  string           `json:"target"`
		Details []map[string]any `json:"details"`
	}
	if err := json.Unmarshal(b, &w); err != nil {
		t.Fatal(err)
	}
	return w.Target, w.Details
}

// R139: each detail of invalid_request carries the instance's pointer as
// target and the keyword that failed as keyword (§9.2.5 step 3; Q-DOC-9,
// problem.schema.json $defs/detail), in the validator's order, so an agent
// reads which keyword failed without parsing prose.
func TestR139DetailsCarryTheKeyword(t *testing.T) {
	s, err := schema.Compile(mustParse(t, holdingsRequest), schema.Request)
	if err != nil {
		t.Fatal(err)
	}
	fs := s.Validate(mustParse(t, `{"portfolios": [1, "x", "x"], "nope": true}`))
	if len(fs) != 3 {
		t.Fatalf("three failures: %v", fs)
	}
	target, details := wireDetails(t, InvalidRequest(fs))
	if target != fs[0].Pointer || len(details) != len(fs) {
		t.Fatalf("target %q, %d details, for %v", target, len(details), fs)
	}
	detail := documents.Schema("problem#detail")
	for i, f := range fs {
		d := details[i]
		if d["code"] != CodeInvalidRequest || d["target"] != f.Pointer || d["keyword"] != f.Keyword {
			t.Errorf("detail %d is %v; the failure is %s %s", i, d, f.Pointer, f.Keyword)
		}
		v, _ := ijson.FromGo(d)
		if bad := detail.Validate(v); len(bad) > 0 {
			t.Errorf("detail %d leaves problem.schema.json's detail: %v", i, bad)
		}
	}
}

// CORE-CALL-002, R139: the scenario's invalid request to conformance.echo
// gives exactly the problem the scenario expects: invalid_request, the
// first pointer as target, and every failure in details as its target and
// keyword (Q-VAL-1, Q-DOC-9).
func TestR139CoreCall002(t *testing.T) {
	var fixture struct {
		Current  map[string]string          `json:"current"`
		Profiles map[string]json.RawMessage `json:"profiles"`
	}
	if err := json.Unmarshal(refs.Read(t, "scenarios/fixtures/gateway.json"), &fixture); err != nil {
		t.Fatal(err)
	}
	profile := mustParse(t, string(fixture.Profiles[fixture.Current["conformance.echo@v1"]]))
	schemas, _ := profile.(*ijson.Object).Get("schemas")
	request, _ := schemas.(*ijson.Object).Get("request")
	s, err := schema.Compile(request, schema.Request)
	if err != nil {
		t.Fatal(err)
	}
	var scenario struct {
		Commands []struct {
			Op   string `json:"op"`
			Args struct {
				Request json.RawMessage `json:"request"`
			} `json:"args"`
		} `json:"commands"`
		Expected []struct {
			Problem *struct {
				Code    string              `json:"code"`
				Target  string              `json:"target"`
				Details []map[string]string `json:"details"`
			} `json:"problem"`
		} `json:"expected"`
	}
	if err := json.Unmarshal(refs.Read(t, "scenarios/CORE-CALL-002.json"), &scenario); err != nil {
		t.Fatal(err)
	}
	want := scenario.Expected[1].Problem
	p := InvalidRequest(s.Validate(mustParse(t, string(scenario.Commands[1].Args.Request))))
	target, details := wireDetails(t, p)
	if p.Code != want.Code || target != want.Target || len(details) != len(want.Details) {
		t.Fatalf("%s at %q with %d details; the scenario expects %s at %q with %d", p.Code, target, len(details), want.Code, want.Target, len(want.Details))
	}
	for i, w := range want.Details {
		for k, v := range w {
			if details[i][k] != v {
				t.Errorf("detail %d: %s is %v, the scenario's %q", i, k, details[i][k], v)
			}
		}
	}
}

// R049: however many failures a request has (the finding's sixteen
// thousand), its problem stays small: at most schema.MaxFailures details,
// the first failure's pointer still the target and the first detail.
func TestR049InvalidRequestIsBounded(t *testing.T) {
	fs := make([]schema.Failure, 16001)
	for i := range fs {
		fs[i] = schema.Failure{Pointer: "/portfolios/" + strconv.Itoa(i), Keyword: "type", Message: strconv.Itoa(i) + " is not a string"}
	}
	p := InvalidRequest(fs)
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Details) > 100 || len(b) > 64<<10 {
		t.Fatalf("%d details, %d bytes", len(p.Details), len(b))
	}
	if p.Target != "/portfolios/0" || p.Details[0].Target != "/portfolios/0" {
		t.Fatalf("the first failure is the target and the first detail: %q, %q", p.Target, p.Details[0].Target)
	}
	if !strings.Contains(p.Detail, "and more than 99 more") || p.Check() != nil {
		t.Fatalf("the detail says failures were left out, and the problem is one the gateway may send: %q, %v", p.Detail, p.Check())
	}
}

// R049: a request whose failures sit under long member names keeps its
// problem small too. A pointer longer than schema.MaxPointerBytes is
// reported at its deepest ancestor within the bound (A-190), and failures
// of one keyword beneath that ancestor are one detail.
func TestR049InvalidRequestBoundsItsBytes(t *testing.T) {
	long := "/" + strings.Repeat("x", 40<<10)
	fs := []schema.Failure{{Pointer: long + "/0", Keyword: "type"}, {Pointer: long + "/1", Keyword: "type"}, {Pointer: long + "/2", Keyword: "type"}}
	p := InvalidRequest(fs)
	want := schema.ReportedPointer(fs[0].Pointer)
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(want) > schema.MaxPointerBytes || len(p.Details) != 1 || p.Target != want || p.Details[0].Target != want || p.Check() != nil || len(b) > 4<<10 {
		t.Fatalf("%d details, target of %d bytes (want %d), %d bytes in all, check %v", len(p.Details), len(p.Target), len(want), len(b), p.Check())
	}
}

// R049: what schema.Schema.Failures reports reaches the problem: with more
// failures than it returns, the detail says so; with every one, it counts
// them.
func TestR049InvalidRequestOfSaysWhenThereAreMore(t *testing.T) {
	s, err := schema.Compile(mustParse(t, holdingsRequest), schema.Request)
	if err != nil {
		t.Fatal(err)
	}
	items := make([]string, 16000)
	for i := range items {
		items[i] = strconv.Itoa(i)
	}
	fs, more := s.Failures(mustParse(t, `{"portfolios": [`+strings.Join(items, ",")+`]}`))
	p := InvalidRequestOf(fs, more)
	if !more || len(p.Details) != schema.MaxFailures || p.Target != "/portfolios" || p.Details[0].Keyword != "maxItems" ||
		!strings.HasPrefix(p.Detail, "maxItems: ") || !strings.HasSuffix(p.Detail, ", and more than 99 more") {
		t.Fatalf("more %v, %d details, target %q: %s", more, len(p.Details), p.Target, p.Detail)
	}
	fs, more = s.Failures(mustParse(t, `{"portfolios": [1, 2]}`))
	if p := InvalidRequestOf(fs, more); more || len(p.Details) != 2 || !strings.HasSuffix(p.Detail, ", and 1 more") {
		t.Fatalf("more %v, %d details: %s", more, len(p.Details), p.Detail)
	}
}

// pointerBound is schema.MaxPointerBytes.
const pointerBound = 1 << 10

// encoded is a problem as kit/problem writes it: encoding/json's encoder,
// which writes each of < > & as six bytes.
func encoded(t testing.TB, p *Problem) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := json.NewEncoder(&b).Encode(p); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// ancestorOf reports whether target is pointer or a pointer above it.
func ancestorOf(target, pointer string) bool {
	return target == pointer || strings.HasPrefix(pointer, target+"/")
}

// R049, assumption A-193: a request inside the body limit with one
// failure, a member whose name is as long as the body allows, gets a
// small problem. Before, the member's pointer was both the target and the
// first detail's, and RFC 6901 and JSON lengthen it further: a 1 MiB body
// gave a problem of 2 to 12 MiB, past the 1 MiB the Go client reads of
// one (sdk/go maxProblem), which then lost the target and the details. A
// pointer is now at most schema.MaxPointerBytes; past them a failure is
// reported at the deepest value above it within them.
func TestR049InvalidRequestBoundsTheFirstPointer(t *testing.T) {
	s, err := schema.Compile(mustParse(t, holdingsRequest), schema.Request)
	if err != nil {
		t.Fatal(err)
	}
	escape := strings.NewReplacer("~", "~0", "/", "~1")
	for _, c := range []string{"x", "/", "~", "<", "é", string(rune(0x2028))} {
		head, tail := `{"portfolios": ["A"], "`, `": 0}`
		name := strings.Repeat(c, (1<<20-len(head)-len(tail))/len(c))
		v, err := ijson.ParseLimits([]byte(head+name+tail), ijson.Limits{MaxBytes: 1 << 20, MaxDepth: 64})
		if err != nil {
			t.Fatal(err)
		}
		pointer := "/" + escape.Replace(name)
		fs, more := s.Failures(v)
		for _, p := range []*Problem{InvalidRequestOf(fs, more), InvalidRequest(s.Validate(v))} {
			b := encoded(t, p)
			if len(b) > 128<<10 || len(p.Target) > pointerBound || !ancestorOf(p.Target, pointer) || len(p.Details) != 1 ||
				p.Details[0].Target != p.Target || p.Details[0].Keyword != "additionalProperties" {
				t.Fatalf("a name of %q: a problem of %d bytes, target of %d bytes (an ancestor: %v), %d details", c, len(b), len(p.Target), ancestorOf(p.Target, pointer), len(p.Details))
			}
		}
	}
	// The same bound holds for a list the validator did not write.
	long := "/" + strings.Repeat("<", 1<<20) + "/0"
	p := InvalidRequest([]schema.Failure{{Pointer: long, Keyword: "type", Message: "0 is not a string"}})
	if b := encoded(t, p); len(b) > 16<<10 || p.Target != "" || p.Details[0].Target != "" {
		t.Fatalf("a problem of %d bytes, target of %d bytes", len(b), len(p.Target))
	}
}

// R049: the gateway and the Go SDK build invalid_request from Validate,
// which gives the first failures within the bounds and does not say
// whether it left any out. The problem never claims a count it cannot
// know: from a list Validate may have cut, by the count or by the bytes,
// it says "at least". Built from Failures, which knows, it says "more
// than"; from a list Validate did not cut, the exact count.
func TestR049InvalidRequestNeverClaimsACountItCannotKnow(t *testing.T) {
	s, err := schema.Compile(mustParse(t, holdingsRequest), schema.Request)
	if err != nil {
		t.Fatal(err)
	}
	items := make([]string, 16000)
	for i := range items {
		items[i] = strconv.Itoa(i)
	}
	byCount := mustParse(t, `{"portfolios": [`+strings.Join(items, ",")+`]}`)
	members := make([]string, 1000)
	for i := range members {
		members[i] = fmt.Sprintf(`"%0990d": 0`, i)
	}
	byBytes := mustParse(t, `{"portfolios": ["A"], `+strings.Join(members, ",")+`}`)
	for _, c := range []struct {
		name string
		v    ijson.Value
	}{{"cut by the count", byCount}, {"cut by the bytes", byBytes}} {
		fs := s.Validate(c.v)
		if p := InvalidRequest(fs); !strings.HasSuffix(p.Detail, fmt.Sprintf(", and at least %d more", len(fs)-1)) {
			t.Errorf("%s: %d failures: %.200q", c.name, len(fs), p.Detail)
		}
		fs, more := s.Failures(c.v)
		if p := InvalidRequestOf(fs, more); !more || !strings.HasSuffix(p.Detail, fmt.Sprintf(", and more than %d more", len(fs)-1)) {
			t.Errorf("%s, by Failures: %d failures, more %v: %.200q", c.name, len(fs), more, p.Detail)
		}
	}
	if fs := s.Validate(byBytes); len(fs) >= schema.MaxFailures {
		t.Fatalf("the bytes should cut the list before the count does: %d failures", len(fs))
	}
	if p := InvalidRequest(s.Validate(mustParse(t, `{"portfolios": [1, 2, 3]}`))); !strings.HasSuffix(p.Detail, ", and 2 more") {
		t.Fatalf("%q", p.Detail)
	}
}
