package builtins_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/jhgonline/fi-app-atlas/gateway/core/builtins"
	"github.com/jhgonline/fi-app-atlas/gateway/core/pipeline"
	"github.com/jhgonline/fi-app-atlas/gateway/edge"
	"github.com/jhgonline/fi-app-atlas/gateway/fakes"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/documents"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
)

// gateway is the world behind the real pipeline and edge, called in
// process: every built-in answer passes the pipeline's own check against
// its contract (§9.3 rule 1) on its way out.
type gateway struct {
	t   *testing.T
	srv *edge.Server
}

func newGateway(t *testing.T) (*builtins.World, *gateway) {
	t.Helper()
	w := builtins.NewWorld(t)
	tokens := fakes.NewTokens(w.Clock)
	for principal, id := range builtins.Identities {
		tokens.Issue("token-"+principal, id, w.Clock.Now().Add(24*time.Hour))
	}
	p, err := pipeline.New(pipeline.Options{Environment: "test", Registry: w.Registry, Authenticator: tokens, Policy: fakes.NewPolicy(w.Policy), Audit: &fakes.Audit{}, Clock: w.Clock})
	if err != nil {
		t.Fatal(err)
	}
	return w, &gateway{t: t, srv: edge.New(edge.Options{Pipeline: p, URL: "http://gateway.test", Clock: w.Clock, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})}
}

type answer struct {
	status int
	header http.Header
	body   *ijson.Object
}

func (g *gateway) call(who, address, body string) answer {
	g.t.Helper()
	id, interaction, _ := strings.Cut(address, ":")
	req := httptest.NewRequest(http.MethodPost, "http://gateway.test/v1/atlas/"+strings.TrimSuffix(id, "@v1")+":"+interaction, strings.NewReader(body))
	req.Header.Set("Content-Type", protocol.MediaJSON)
	req.Header.Set(protocol.HeaderClient, `name="builtins-test", version="1.0.0", protocol="atlas/1.0"`)
	req.Header.Set("Authorization", "Bearer token-"+who)
	rec := httptest.NewRecorder()
	g.srv.ServeHTTP(rec, req)
	v, err := ijson.Parse(rec.Body.Bytes())
	if err != nil {
		g.t.Fatalf("%s %s: %d %s", who, address, rec.Code, rec.Body)
	}
	return answer{rec.Code, rec.Header(), v.(*ijson.Object)}
}

func member(o *ijson.Object, name string) string {
	v, _ := o.Get(name)
	s, _ := v.(string)
	return s
}

// ATLAS-DISCOVERY-003, §2.6 rule 4, R021, over HTTP: describe and guide
// refuse a capability the caller may not use with its status and a next
// step naming atlas.access, exactly as a call of it is refused.
func TestR021TheWorkingViewOverHTTP(t *testing.T) {
	_, g := newGateway(t)
	for _, c := range []struct {
		who, body, code string
		status          int
	}{
		{builtins.Dan, `{"by": "id", "id": "fund.positions@v1"}`, protocol.CodeDenied, 403},
		{builtins.Cal, `{"by": "id", "id": "fund.positions@v1"}`, protocol.CodeNotGranted, 403},
		{builtins.Job, `{"by": "id", "id": "fund.list@v1"}`, protocol.CodeNotGranted, 403},
		{builtins.Ana, `{"by": "id", "id": "fund.secret@v1"}`, protocol.CodeNotFound, 404},
	} {
		for _, address := range []string{"atlas.describe@v1:fetch", "atlas.guide@v1:fetch"} {
			a := g.call(c.who, address, c.body)
			if fs := documents.Validate("problem", a.body); len(fs) > 0 {
				t.Fatalf("not a problem: %v", fs)
			}
			if a.status != c.status || member(a.body, "code") != c.code || c.code != protocol.CodeNotFound && !strings.Contains(member(a.body, "next"), "atlas.access@v1") {
				t.Errorf("%s %s %s: %d %s", c.who, address, c.body, a.status, member(a.body, "next"))
			}
		}
	}
}

// R01 (D3, D7, D8 of gaps/2.md): describe and guide carry the caller's
// own state, so neither is cached: no-store, and no entity tag. The
// answer names the built-in's own profile (§9.3.2), and passes the
// pipeline's check of its contract.
func TestD7DescribeAndGuideAreNeverCached(t *testing.T) {
	_, g := newGateway(t)
	for address, doc := range map[string]string{"atlas.describe@v1:fetch": "describe", "atlas.guide@v1:fetch": "builtins#guide_result"} {
		a := g.call(builtins.Ana, address, `{"by": "id", "id": "fund.positions@v1"}`)
		if a.status != 200 {
			t.Fatalf("%s: %d %v", address, a.status, a.body)
		}
		if fs := documents.Validate(doc, a.body); len(fs) > 0 {
			t.Errorf("%s: %v", address, fs)
		}
		if a.header.Get("Cache-Control") != "no-store" || a.header.Get("ETag") != "" || !strings.HasPrefix(a.header.Get(protocol.HeaderProfile), `"`+strings.TrimSuffix(address, ":fetch")+"#") {
			t.Errorf("%s: %v", address, a.header)
		}
	}
}

// R000, ATLAS-DISCOVERY-009, over HTTP: every built-in's guide answers
// 200 for a caller who holds nothing else, valid as a guide_result.
func TestR000EveryBuiltinsGuideOverHTTP(t *testing.T) {
	_, g := newGateway(t)
	ps, err := builtins.Profiles()
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range ps {
		a := g.call(builtins.Cal, "atlas.guide@v1:fetch", `{"by": "id", "id": "`+p.ID+`@v1"}`)
		if a.status != 200 {
			t.Errorf("%s: %d %v", p.ID, a.status, a.body)
			continue
		}
		if fs := documents.Validate("builtins#guide_result", a.body); len(fs) > 0 {
			t.Errorf("%s: %v", p.ID, fs)
		}
	}
}

// ATLAS-MECH-009 over HTTP: a cursor that is not the caller's is 400
// invalid_request at /cursor.
func TestR076AForeignCursorOverHTTP(t *testing.T) {
	_, g := newGateway(t)
	page := g.call(builtins.Ana, "atlas.catalog@v1:fetch", `{"page_size": 2}`)
	next := member(page.body, "next_cursor")
	if next == "" {
		t.Fatalf("one page: %v", page.body)
	}
	a := g.call(builtins.Bea, "atlas.catalog@v1:fetch", `{"page_size": 2, "cursor": "`+next+`"}`)
	if a.status != 400 || member(a.body, "code") != protocol.CodeInvalidRequest || member(a.body, "target") != "/cursor" {
		t.Fatalf("%d %s %s", a.status, member(a.body, "code"), member(a.body, "target"))
	}
}
