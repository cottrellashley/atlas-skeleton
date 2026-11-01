package edge

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/documents"
	"atlas.local/sdk/go/protocol/ijson"
)

// R127, R131, AGENTS.md ("fuzz every parser"): Accept and
// Atlas-Client-Features, as any client may send them. The choice is always
// an encoding the binding lists, the edge frames and the client reads, or
// a problem the registry knows.
func FuzzAccept(f *testing.F) {
	for _, seed := range []struct{ accept, features string }{
		{"", ""},
		{"*/*", fullFeatures},
		{"application/x-ndjson, application/json;q=0.5", "levels=(core streaming)"},
		{`application/vnd.apache.arrow.stream; x="a,b", text/csv`, fullFeatures},
		{"application/*", "levels=(core columnar), encodings=(arrow)"},
		{";;;,,,", "levels=core"},
		{"*/json", "levels=(core streaming columnar)"},
		{"TEXT/HTML, Application/JSON", ""},
	} {
		f.Add(seed.accept, seed.features)
	}
	ins := []struct {
		delivery, result string
		encodings        []string
	}{
		{"chunked", "table", []string{"ndjson", "json", "arrow"}},
		{"single", "table", []string{"json"}},
		{"chunked", "table", []string{"arrow", "csv"}},
		{"single", "record", []string{"json", "ndjson"}},
	}
	f.Fuzz(func(t *testing.T, accept, featureField string) {
		h := http.Header{}
		for _, line := range strings.Split(accept, "\n") {
			h.Add("Accept", line)
		}
		feats := protocol.CoreFeatures()
		if parsed, err := protocol.ParseFeatures(featureField); err == nil {
			feats = parsed
		}
		ranges, _ := accepts(h)
		if len(ranges) > maxRanges {
			t.Fatalf("%d ranges read, bound %d", len(ranges), maxRanges)
		}
		for _, c := range ins {
			in := interaction(c.delivery, c.result, c.encodings...)
			ch, prob := choose(h, feats, in, "/at")
			if prob != nil {
				if !protocol.Known(prob.Code) || prob.Check() != nil && prob.Status != http.StatusNotAcceptable {
					t.Fatalf("an unregistered problem: %+v", prob)
				}
				continue
			}
			if !slices.Contains(c.encodings, ch.encoding) || !frames(in, ch.encoding) || !reads(feats, ch.encoding) {
				t.Fatalf("%q, %q: chose %s for %+v", accept, featureField, ch.encoding, c)
			}
		}
	})
}

// R127, R137, §9.6.3 (a header that does not parse as its type is
// absent): the Atlas headers, as any client may send them. Whatever is
// read holds its grammar; nothing else is read.
func FuzzReadHeaders(f *testing.F) {
	f.Add("Bearer t-alice", client, fullFeatures, `"portfolio.list@v1#0123456789abcdef"`, "@1793318400", `"run-1"`, "?1")
	f.Add("bearer  x", `name=1`, "levels=core", "x", "1793318400", "run_1", "?0")
	f.Add("", "", "", "", "@-1", `"`+strings.Repeat("k", 300)+`"`, "1")
	r := newRig(f, rigOptions{noServer: true})
	f.Fuzz(func(t *testing.T, auth, clientField, features, expect, deadline, key, interactive string) {
		req := httptest.NewRequest(http.MethodPost, listAt, nil)
		for _, h := range []struct{ name, value string }{
			{protocol.HeaderAuthorization, auth}, {protocol.HeaderClient, clientField}, {protocol.HeaderClientFeatures, features},
			{protocol.HeaderProfileExpect, expect}, {protocol.HeaderDeadline, deadline}, {protocol.HeaderIdempotencyKey, key}, {protocol.HeaderInteractive, interactive},
		} {
			if h.value != "" {
				req.Header.Set(h.name, h.value)
			}
		}
		c := r.edge.read(req, "id", ijson.NewObject())
		if c.IdempotencyKey != "" && protocol.CheckIdempotencyKey(c.IdempotencyKey) != nil {
			t.Fatalf("an Idempotency-Key read that breaks its grammar: %q", c.IdempotencyKey)
		}
		if c.Client != nil && c.Client.Name == "" && c.Client.Version == "" && c.Client.Protocol == "" {
			t.Fatalf("an empty Atlas-Client read from %q", clientField)
		}
		if !c.Features.Has(protocol.LevelCore) {
			t.Fatal("every client is Core")
		}
		if c.Token != "" && strings.ContainsAny(c.Token, " \t\r\n") {
			t.Fatalf("a token with white space: %q", c.Token)
		}
	})
}

// R127, R130, ATLAS-MECH-002, AGENTS.md: the whole request path, as any
// caller may send a request. Every answer carries its request id; a
// failure is a problem document of the registry with that id, and never
// cached; nothing panics.
func FuzzRequest(f *testing.F) {
	f.Add("POST", listAt, "application/json", "*/*", "", "Bearer t-alice", []byte(`{}`))
	f.Add("POST", holdingsAt, "application/json", protocol.MediaNDJSON, "@1", "Bearer t-alice", []byte(`{"portfolios": ["147HYC"]}`))
	f.Add("GET", secretAt, "", "text/html", "", "", []byte(nil))
	f.Add("POST", "/v1/../data/portfolio.list@v1:fetch", "text/plain", "", "", "Bearer forged", []byte(`{"a":`))
	f.Add("OPTIONS", listAt, "", "", "", "", []byte(nil))
	f.Add("GET", protocol.WellKnownPath, "", "", "", "", []byte(nil))
	f.Add("POST", holdingsAt, "application/json; charset=utf-8", protocol.MediaArrowStream, "@4102444800", "Bearer t-alice", []byte(`{"portfolios": ["147HYC"], "portfolios": []}`))
	r := newRig(f, rigOptions{noServer: true})
	f.Fuzz(func(t *testing.T, method, path, contentType, accept, deadline, auth string, body []byte) {
		if !validMethod(method) || !strings.HasPrefix(path, "/") || strings.ContainsAny(path, " \r\n\t#?%") {
			return
		}
		req, err := http.NewRequest(method, "http://gateway.test"+path, bytes.NewReader(body))
		if err != nil {
			return
		}
		req.RemoteAddr = "192.0.2.1:1234"
		for _, h := range []struct{ name, value string }{
			{"Content-Type", contentType}, {"Accept", accept}, {protocol.HeaderDeadline, deadline}, {"Authorization", auth},
			{protocol.HeaderClient, client}, {protocol.HeaderClientFeatures, fullFeatures},
		} {
			if h.value != "" && !strings.ContainsAny(h.value, "\r\n\x00") {
				req.Header.Set(h.name, h.value)
			}
		}
		rec := httptest.NewRecorder()
		r.edge.ServeHTTP(rec, req)
		id := rec.Header().Get(protocol.HeaderRequestID)
		if id == "" {
			t.Fatalf("%s %s: no X-Request-Id", method, path)
		}
		if strings.Contains(r.log.String(), `"msg":"panic"`) {
			t.Fatalf("%s %s: a panic: %s", method, path, r.log.String())
		}
		if rec.Code < 400 {
			return
		}
		v, err := ijson.Parse(rec.Body.Bytes())
		if err != nil {
			t.Fatalf("%s %s: %d %q", method, path, rec.Code, rec.Body.Bytes())
		}
		if fs := documents.Validate("problem", v); len(fs) > 0 {
			t.Fatalf("%s %s: not a problem document: %v", method, path, fs)
		}
		var p protocol.Problem
		if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil || p.RequestID != id || !protocol.Known(p.Code) || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s %s: %d %v %s", method, path, rec.Code, rec.Header(), rec.Body.Bytes())
		}
	})
}

func validMethod(m string) bool {
	if m == "" {
		return false
	}
	for i := 0; i < len(m); i++ {
		if !(m[i] >= 'A' && m[i] <= 'Z') {
			return false
		}
	}
	return true
}
