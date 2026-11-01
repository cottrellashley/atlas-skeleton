package edge

import (
	"net/http"
	"testing"

	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/ijson"
)

// sameAnswer reports how two answers differ, setting aside what differs
// for every request: its id, its trace, its date.
func sameAnswer(t testing.TB, a, b answer) string {
	t.Helper()
	if a.status != b.status {
		return "status"
	}
	for _, h := range []string{protocol.HeaderErrorCode, protocol.HeaderWWWAuthenticate, "Allow", "Content-Type", "Cache-Control", "Retry-After"} {
		if a.header.Get(h) != b.header.Get(h) {
			return h
		}
	}
	strip := func(body []byte) string {
		v, err := ijson.Parse(body)
		if err != nil {
			return string(body)
		}
		if o, ok := v.(*ijson.Object); ok {
			o.Delete("request_id")
		}
		out, _ := ijson.Marshal(v)
		return string(out)
	}
	if strip(a.body) != strip(b.body) {
		return "body: " + string(a.body) + " | " + string(b.body)
	}
	return ""
}

// PR08, R107, R130; ATLAS-CORE §8 step 3 ("enforce visibility ... without
// exposing a confidential contract"), §9.3.3 not_found, ATLAS-DISCOVERY-005,
// ATLAS-ACCESS-009: before the pipeline has decided a call's access, the
// edge answers a bound address exactly as an unbound one, for any method,
// any Accept, any caller. A caller without the access to know the address
// learns nothing from it; one who holds the capability is told about its
// binding.
func TestPR08NoDisclosureBeforeAuthorisation(t *testing.T) {
	r := newRig(t, rigOptions{})
	get := func(token, path string, headers ...string) answer {
		req := r.request(token, path, "", headers...)
		req.Method = http.MethodGet
		return r.do(req)
	}
	for _, c := range []struct {
		name    string
		token   string
		bound   string
		headers []string
		get     bool
	}{
		{"an anonymous GET of a listed address", "", holdingsAt, nil, true},
		{"an anonymous GET of a confidential address", "", secretAt, nil, true},
		{"a signed-in GET of a confidential address", "t-carol", secretAt, nil, true},
		{"an anonymous POST with an Accept nothing serves", "", holdingsAt, []string{"Accept", "text/plain"}, false},
		{"an anonymous POST with Accept the binding does not list", "", listAt, []string{"Accept", protocol.MediaArrowStream}, false},
		{"an anonymous POST to a confidential address", "", secretAt, []string{"Accept", "text/html"}, false},
		{"a forged token", "forged", secretAt, []string{"Accept", "text/plain"}, false},
		{"no grant to a confidential address, an Accept nothing serves", "t-carol", secretAt, []string{"Accept", "text/plain"}, false},
		{"no grant to a confidential address", "t-carol", secretAt, nil, false},
		{"no grant to a confidential address, Arrow asked of JSON", "t-carol", secretAt, []string{"Accept", protocol.MediaArrowStream}, false},
	} {
		var bound, unbound answer
		if c.get {
			bound, unbound = get(c.token, c.bound, c.headers...), get(c.token, nowhereAt, c.headers...)
		} else {
			bound, unbound = r.call(c.token, c.bound, `{}`, c.headers...), r.call(c.token, nowhereAt, `{}`, c.headers...)
		}
		if diff := sameAnswer(t, bound, unbound); diff != "" {
			t.Errorf("%s: the bound address is told apart by its %s", c.name, diff)
		}
		bound.problem(t)
	}

	// Listed but not held: not_granted, never invalid_request, whatever
	// the Accept (ATLAS-ACCESS-009).
	if p := r.call("t-carol", holdingsAt, `{"portfolios": ["147HYC"]}`, "Accept", "text/plain").problem(t); p.Code != protocol.CodeNotGranted {
		t.Fatalf("a listed capability the caller does not hold: %s", p.Code)
	}
	// A caller who holds it is told what its binding serves.
	a := r.call("t-alice", holdingsAt, `{"portfolios": ["147HYC"]}`, "Accept", "text/csv")
	if p := a.problem(t); a.status != http.StatusNotAcceptable || p.Code != protocol.CodeInvalidRequest || p.Target != "Accept" {
		t.Fatalf("a holder's unacceptable Accept: %d %s", a.status, a.body)
	}
	// And a GET is not_found everywhere, the holder's own address included.
	if p := get("t-alice", holdingsAt).problem(t); p.Code != protocol.CodeNotFound {
		t.Fatalf("GET: %s", p.Code)
	}
}
