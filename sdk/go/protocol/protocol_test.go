package protocol

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/grammar"
)

// ATLAS-PROTOCOL-001, ATLAS-PROTOCOL-005: every problem has a registered code, its type
// names it, and a code the registry does not hold is a defect.
func TestProblems(t *testing.T) {
	p := NewProblem(CodeLimitExceeded, "60 calls a minute").WithRetryAfter(30).WithTarget("rate")
	if p.Type != "https://atlas.jhg.com/problems/limit_exceeded" || p.Status != 429 || !p.Retryable {
		t.Fatalf("limit_exceeded: %+v", p)
	}
	if err := p.Check(); err != nil {
		t.Fatal(err)
	}
	if q := NewProblem("no_such_code", "x"); q.Code != CodeInternal || q.Status != 500 {
		t.Fatalf("an unregistered code becomes internal: %+v", q)
	}
	declared := Declared{Code: "portfolio_not_found", Status: 404, Description: "No portfolio has this code.", Next: "Call portfolio.list@v1:fetch."}
	q := declared.Problem("GLOBAL-X")
	if err := q.Check(declared); err != nil {
		t.Fatal(err)
	}
	if err := q.Check(); err == nil {
		t.Fatal("a code neither registered nor declared is refused")
	}
	bad := *NewProblem(CodeInternal, "x")
	bad.Retryable = true
	if err := bad.Check(); err == nil {
		t.Fatal("internal is not retryable")
	}
	nested := NewProblem(CodeUpstreamFailed, "the vendor failed").WithDetails(Problem{Type: ProblemTypeBase + "x", Code: "x", Status: 500})
	if err := nested.Check(); err == nil {
		t.Fatal("details are checked too")
	}
	b, _ := json.Marshal(NewProblem(CodeConfirmationRequired, "costly").WithEstimate(Estimate{Amount: "1250.5", Unit: "credits"}))
	if !strings.Contains(string(b), `"estimate":{"amount":"1250.5","unit":"credits"}`) {
		t.Fatalf("estimate: %s", b)
	}
}

// AP-HTTP-001: every header of §9.6.3 reads and writes as its type.
func TestHeaders(t *testing.T) {
	h := http.Header{}
	c := Client{Name: "fiatlas", Version: "2.3.0", Protocol: Version}
	s, err := c.Format()
	if err != nil || s != `name="fiatlas", version="2.3.0", protocol="atlas/1.0"` {
		t.Fatalf("Atlas-Client: %s %v", s, err)
	}
	h.Set(HeaderClient, s)
	field, _ := Field(h, HeaderClient)
	if back, err := ParseClient(field); err != nil || back != c {
		t.Fatalf("Atlas-Client round trip: %+v %v", back, err)
	}
	if back, _ := ParseClient(`name="x", protocol="atlas/one"`); back.Protocol != "" {
		t.Fatal("a protocol that is not a protocol-ver is absent")
	}

	example := "levels=(core streaming columnar bulk events handoff), handoff=(snowflake-arrow/1 reach-snowflake), lens=(rename alias_value convert_unit promote default drop), deliveries=(single chunked subscription operation transfer), encodings=(json ndjson arrow csv parquet)"
	f, err := ParseFeatures(example)
	if err != nil {
		t.Fatal(err)
	}
	if !f.Has(LevelHandoff) || !f.Decodes(HandoffSnowflakeArrow1) || !f.Decodes(ReachSnowflake) || !f.Applies(LensPromote) || !f.Reads(EncodingParquet) || !f.Accepts(DeliveryTransfer) {
		t.Fatalf("features: %+v", f)
	}
	if out, _ := f.Format(); out != example {
		t.Fatalf("features serialise\n got %s\nwant %s", out, example)
	}
	if _, err := ParseFeatures(`levels=(streaming "x" 1)`); err == nil {
		t.Fatal("a known member holding a non-token makes the header absent (Q-HDR-1)")
	}
	if _, err := ParseFeatures(`traits=projectable`); err == nil {
		t.Fatal("a known member that is not an Inner List makes the header absent")
	}
	if f, err := ParseFeatures(`levels=(streaming), future=(a b)`); err != nil || !f.Has(LevelCore) || !f.Has(LevelStreaming) || len(f.Levels) != 1 {
		t.Fatalf("an unknown member is ignored, and every client is Core: %+v %v", f, err)
	}
	h2 := http.Header{}
	h2.Add(HeaderClientFeatures, "levels=(core columnar)")
	h2.Add(HeaderClientFeatures, "encodings=(json arrow)")
	field, _ = Field(h2, HeaderClientFeatures)
	if f, err := ParseFeatures(field); err != nil || !f.Has(LevelColumnar) || !f.Reads(EncodingArrow) {
		t.Fatalf("field lines combine (RFC 9651 §4.2): %+v %v", f, err)
	}

	e, _ := grammar.ParseExact("portfolio.holdings@v1#bcaa5bbad97cc612")
	if s, _ := FormatExact(e); s != `"portfolio.holdings@v1#bcaa5bbad97cc612"` {
		t.Fatalf("Atlas-Profile-Expect: %s", s)
	}
	if back, err := ParseExactHeader(`"portfolio.holdings@v1#bcaa5bbad97cc612"`); err != nil || back != e {
		t.Fatalf("Atlas-Profile-Expect read: %v %v", back, err)
	}
	for _, bad := range []string{`portfolio.holdings@v1#bcaa5bbad97cc612`, `"portfolio.holdings@v1"`, `"x"`} {
		if _, err := ParseExactHeader(bad); err == nil {
			t.Errorf("%s accepted as an exact-id header", bad)
		}
	}

	at := time.Date(2026, 10, 31, 12, 0, 0, 999_000_000, time.UTC)
	if s := FormatDeadline(at); s != "@1793448000" {
		t.Fatalf("Atlas-Deadline rounds down to the second: %s", s)
	}
	if d, err := ParseDeadline("@1793318400"); err != nil || !d.Equal(time.Unix(1793318400, 0)) {
		t.Fatalf("Atlas-Deadline: %v %v", d, err)
	}
	for _, bad := range []string{"1793318400", "@1.5", `"@1"`, ""} {
		if _, err := ParseDeadline(bad); err == nil {
			t.Errorf("%q accepted as a deadline", bad)
		}
	}

	if k, err := ParseIdempotencyKey(`"8e03978e-40d5-43e8.bc93_6894a57f9324"`); err != nil || k != "8e03978e-40d5-43e8.bc93_6894a57f9324" {
		t.Fatalf("Idempotency-Key: %q %v", k, err)
	}
	for _, bad := range []string{`""`, `"a b"`, `"a/b"`, `abc`, `"` + strings.Repeat("a", 256) + `"`} {
		if _, err := ParseIdempotencyKey(bad); err == nil {
			t.Errorf("%q accepted as an idempotency key", bad)
		}
	}
	if s, _ := FormatIdempotencyKey(strings.Repeat("a", 255)); len(s) != 257 {
		t.Fatal("255 characters is the longest key")
	}

	if x, err := ParseExecution("handoff"); err != nil || x != ExecutionHandoff {
		t.Fatalf("Atlas-Execution: %q %v", x, err)
	}
	if _, err := ParseExecution(`"handoff"`); err == nil {
		t.Fatal("Atlas-Execution is a token, not a string")
	}
	if _, err := ParseExecution("broker"); err == nil {
		t.Fatal("Atlas-Execution is gateway, handoff or client")
	}
	if b, err := ParseBoolean(FormatBoolean(true)); err != nil || !b {
		t.Fatal("X-Atlas-Interactive")
	}

	if s, err := FormatConfirm("1250.5"); err != nil || s != "1250.5" {
		t.Fatalf("Atlas-Confirm: %s %v", s, err)
	}
	if s, _ := FormatConfirm("1250"); s != "1250.0" {
		t.Fatalf("Atlas-Confirm is always a decimal: %s", s)
	}
	if _, err := FormatConfirm("1.2345"); err == nil {
		t.Fatal("an amount with four fraction digits does not fit Atlas-Confirm")
	}
	confirm, _ := ParseConfirm("1250.5")
	amount, _ := ParseAmount("1250.500")
	if confirm != amount {
		t.Fatal("Atlas-Confirm compares exactly with the estimate's amount")
	}
	if _, err := ParseConfirm("1250"); err == nil {
		t.Fatal("an integer is not a decimal (assumption A-007)")
	}

	if s, _ := FormatLimits([]Limit{{"rate", 58}, {"concurrency", 3}}); s != "rate=58, concurrency=3" {
		t.Fatalf("Atlas-Limit-Remaining: %s", s)
	}
	if ls, err := ParseLimits("rate=58;window=60, concurrency=3"); err != nil || len(ls) != 2 || ls[1] != (Limit{"concurrency", 3}) {
		t.Fatalf("Atlas-Limit-Remaining read, its parameters ignored: %+v %v", ls, err)
	}
	if _, err := ParseLimits("rate=58, x=?1"); err == nil {
		t.Fatal("a member that is not an Integer makes the header absent (Q-HDR-1)")
	}
	if c, err := ParseErrorCode("limit_exceeded"); err != nil || c != CodeLimitExceeded {
		t.Fatalf("Atlas-Error-Code: %q %v", c, err)
	}
	if _, err := ParseErrorCode(`"limit_exceeded"`); err == nil {
		t.Fatal("Atlas-Error-Code is a token")
	}

	sunset := time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC)
	if s := FormatSunset(sunset); s != "Mon, 01 Mar 2027 00:00:00 GMT" {
		t.Fatalf("Sunset is an HTTP-date: %s", s)
	}
	if back, err := ParseSunset("Mon, 01 Mar 2027 00:00:00 GMT", sunset); err != nil || !back.Equal(sunset) {
		t.Fatalf("Sunset read: %v %v", back, err)
	}
	if d, err := ParseDeprecation(FormatDeprecation(sunset)); err != nil || !d.Equal(sunset) {
		t.Fatalf("Deprecation: %v %v", d, err)
	}

	for in, want := range map[string]time.Duration{"0": 0, "2": 2 * time.Second, "30": 30 * time.Second, "99999999999": MaxRetryAfter * time.Second} {
		if got, err := ParseRetryAfter(in); err != nil || got != want {
			t.Errorf("Retry-After %q = %v %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "-1", "1.5", "Fri, 31 Dec 1999 23:59:59 GMT"} {
		if _, err := ParseRetryAfter(bad); err == nil {
			t.Errorf("Retry-After %q accepted", bad)
		}
	}
	if u, err := ParseOperationLocation("https://atlas.jhg.com/v1/operations/op-91"); err != nil || u.Path != "/v1/operations/op-91" {
		t.Fatalf("Operation-Location: %v %v", u, err)
	}
	if _, err := ParseOperationLocation("/v1/operations/op-91"); err == nil {
		t.Fatal("Operation-Location is absolute")
	}
}

func TestETags(t *testing.T) {
	cur := StrongETag("3f9a1c0b7d2e4a65")
	for field, want := range map[string]bool{
		`"3f9a1c0b7d2e4a65"`:        true,
		`"x", "3f9a1c0b7d2e4a65"`:   true,
		`*`:                         true,
		`W/"3f9a1c0b7d2e4a65"`:      false, // If-Match compares strongly
		`"3f9a1c0b7d2e4a6"`:         false,
		`3f9a1c0b7d2e4a65`:          false,
		``:                          false,
		`"a", W/"3f9a1c0b7d2e4a65"`: false,
	} {
		if got := IfMatch(field, cur); got != want {
			t.Errorf("If-Match %q = %v", field, got)
		}
	}
	if !IfNoneMatch(`W/"3f9a1c0b7d2e4a65"`, cur) || IfNoneMatch(`"other"`, cur) {
		t.Fatal("If-None-Match compares weakly")
	}
}

func TestTraceparent(t *testing.T) {
	tp, err := ParseTraceparent("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	if err != nil || tp.Flags != 1 || tp.String() != "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01" {
		t.Fatalf("traceparent: %+v %v", tp, err)
	}
	if _, err := ParseTraceparent("01-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01-future"); err != nil {
		t.Fatal("a later version is read as 00, its trailing fields ignored")
	}
	for _, bad := range []string{
		"00-00000000000000000000000000000000-00f067aa0ba902b7-01",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01",
		"00-4BF92F3577B34DA6A3CE929D0E0E4736-00f067aa0ba902b7-01",
		"ff-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01-extra",
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7",
	} {
		if _, err := ParseTraceparent(bad); err == nil {
			t.Errorf("%q accepted as a traceparent", bad)
		}
	}
}

func TestChallenge(t *testing.T) {
	c := Challenge{Realm: "atlas", Error: "invalid_token", Claims: "eyJhY2Nlc3NfdG9rZW4iOnt9fQ"}
	s := c.Format()
	if s != `Bearer realm="atlas", error="invalid_token", claims="eyJhY2Nlc3NfdG9rZW4iOnt9fQ"` {
		t.Fatalf("WWW-Authenticate: %s", s)
	}
	if back, err := ParseChallenge(s); err != nil || back != c {
		t.Fatalf("read back: %+v %v", back, err)
	}
	back, err := ParseChallenge(`Negotiate abc==, Basic realm="x, y", Bearer realm="atlas"`)
	if err != nil || back.Realm != "atlas" || back.Error != "" {
		t.Fatalf("another scheme's challenge is skipped: %+v %v", back, err)
	}
	if _, err := ParseChallenge(`Basic realm="x"`); err == nil {
		t.Fatal("no Bearer challenge")
	}
}

func TestChooseMedia(t *testing.T) {
	served := []string{MediaArrowStream, MediaJSON, MediaNDJSON}
	for accept, want := range map[string]string{
		"application/vnd.apache.arrow.stream, application/json": MediaArrowStream,
		"application/x-ndjson, application/json":                MediaNDJSON,
		"application/json;q=0.1, application/x-ndjson;q=1":      MediaJSON, // q values are ignored
		"text/csv, */*": MediaArrowStream,
		"":              MediaArrowStream,
	} {
		if got, ok := ChooseMedia(accept, served); !ok || got != want {
			t.Errorf("Accept %q chose %q", accept, got)
		}
	}
	if _, ok := ChooseMedia("text/csv", served); ok {
		t.Fatal("a type the binding does not serve is a 406")
	}
}
