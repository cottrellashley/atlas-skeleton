package atlas

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"atlas.local/sdk/go/engine/fakegw"
	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/frame"
	"atlas.local/sdk/go/protocol/ijson"
)

// R001, STREAM-004, Q-DOC-15, ATLAS-CORE §10: a truncated table returns
// the rows sent with the warning output_limit_exceeded, and is marked
// incomplete; the warning reaches the client's hook (§9.7.4 step 11).
func TestR001ATruncatedTableIsMarkedIncomplete(t *testing.T) {
	isolate(t)
	for _, accept := range []string{protocol.MediaNDJSON, protocol.MediaArrowStream} {
		gw := fakegw.New(t, fakegw.Options{})
		gw.Handle("conformance.rows@v1:fetch", func(c *fakegw.Call) {
			p := protocol.NewProblem(protocol.CodeOutputLimitExceeded, "the caller's max_rows is 2")
			p.Next, p.RequestID = "Ask for fewer rows.", "req-1"
			c.Rows(2, fakegw.Row, frame.End{Truncated: true, Usage: []byte(`{"rows":2}`), Problem: p})
		})
		var hooked []Warning
		c := connect(t, gw, signedIn(t), func(o *Options) { o.Warn = func(w Warning) { hooked = append(hooked, w) } })
		r, err := c.Call(context.Background(), "conformance.rows@v1:fetch", map[string]any{"n": 5}, WithAccept(accept))
		if err != nil {
			t.Fatal(err)
		}
		rows, err := r.All()
		if err != nil || len(rows) != 2 {
			t.Fatalf("%s: %d rows, %v", accept, len(rows), err)
		}
		if !r.Truncated() || r.Complete() || len(r.Warnings) != 1 || r.Warnings[0].Code != "output_limit_exceeded" || len(hooked) != 1 {
			t.Fatalf("%s: truncated %v, complete %v, warnings %v, hooked %v", accept, r.Truncated(), r.Complete(), r.Warnings, hooked)
		}
		if string(r.Usage()) != `{"rows":2}` {
			t.Fatalf("%s: usage %s", accept, r.Usage())
		}
	}
}

// R001, AP-HTTP-002, STREAM-002: a table read to its end is complete, and
// one that stops without its end line is upstream_failed, never complete.
func TestR001CompletionNeedsTheEnd(t *testing.T) {
	isolate(t)
	gw := fakegw.New(t, fakegw.Options{})
	gw.Handle("conformance.rows@v1:fetch", func(c *fakegw.Call) {
		if n, _ := c.Body.Get("n"); n == ijson.Number("3") {
			c.Serve()
			return
		}
		c.W.Header().Set("Content-Type", protocol.MediaNDJSON)
		c.W.WriteHeader(http.StatusOK)
		b, _ := ijson.Marshal(fakegw.Row(0))
		_, _ = c.W.Write(append(b, '\n'))
	})
	c := connect(t, gw, signedIn(t))
	r, err := c.Call(context.Background(), "conformance.rows@v1:fetch", map[string]any{"n": 3})
	if err != nil {
		t.Fatal(err)
	}
	if rows, err := r.All(); err != nil || len(rows) != 3 || !r.Complete() || r.Truncated() {
		t.Fatalf("%d rows, %v, complete %v", len(rows), err, r.Complete())
	}
	r, err = c.Call(context.Background(), "conformance.rows@v1:fetch", map[string]any{"n": 4})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := r.All()
	wantProblem(t, err, "upstream_failed")
	if len(rows) != 1 || r.Complete() {
		t.Fatalf("%d rows, complete %v", len(rows), r.Complete())
	}
}

// R001, CORE-DEPR-001: Deprecation and Sunset become a warning naming what
// is deprecated, its sunset and its replacement.
func TestR001ADeprecationNamesItsSunsetAndReplacement(t *testing.T) {
	isolate(t)
	gw := fakegw.New(t, fakegw.Options{})
	gw.Current("conformance.evolving@v1#bc7fde7c0bac077d")
	gw.Handle("conformance.evolving@v1:fetch", func(c *fakegw.Call) {
		c.W.Header().Set("Deprecation", protocol.FormatDeprecation(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)))
		c.W.Header().Set("Sunset", protocol.FormatSunset(time.Date(2027, 1, 31, 0, 0, 0, 0, time.UTC)))
		c.Rows(0, func(int64) *ijson.Object { return nil }, frame.End{})
	})
	c := connect(t, gw, signedIn(t))
	r, err := c.Call(context.Background(), "conformance.evolving@v1:fetch", map[string]any{"ids": []string{"p0"}}, WithAccept(protocol.MediaNDJSON))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.All(); err != nil {
		t.Fatal(err)
	}
	if len(r.Warnings) != 1 || r.Warnings[0].Code != "deprecated" {
		t.Fatalf("warnings: %v", r.Warnings)
	}
	for _, want := range []string{"conformance.evolving@v1", "2027-01-31", "conformance.evolving@v2"} {
		if !strings.Contains(r.Warnings[0].Text, want) {
			t.Fatalf("the warning %q does not name %s", r.Warnings[0].Text, want)
		}
	}
}

// R032, R063, §9.3 rule 1: a built-in's answer is held to its document
// before it is used; a catalogue page without items is invalid_document,
// never a panic, and the client goes on working.
func TestR032ABuiltinAnswerIsCheckedBeforeUse(t *testing.T) {
	isolate(t)
	gw := fakegw.New(t, fakegw.Options{})
	gw.Handle("atlas.catalog:fetch", func(c *fakegw.Call) { c.JSON(mustObject(t, `{"message": "maintenance"}`)) })
	gw.Handle("atlas.search:fetch", func(c *fakegw.Call) {
		c.JSON(mustObject(t, `{"items": [7], "next_cursor": null, "house_rules": {"revision": "h1", "rules": []}}`))
	})
	c := connect(t, gw, signedIn(t))
	ctx := context.Background()
	_, err := c.Catalog(ctx, nil)
	wantProblem(t, err, "invalid_document")
	_, err = c.Search(ctx, "anything")
	wantProblem(t, err, "invalid_document")
	if _, err := c.Whoami(ctx); err != nil {
		t.Fatalf("the client after a bad page: %v", err)
	}
}

// §9.7.10, Q-DOC-1, CORE-TOL-001: an enum value this client does not know
// reads UNKNOWN, and a document from a newer gateway never fails.
func TestR032ATolerantReader(t *testing.T) {
	isolate(t)
	gw := fakegw.New(t, fakegw.Options{})
	gw.Handle("atlas.whoami:fetch", func(c *fakegw.Call) {
		who := mustObject(t, `{"principal": "00000000-0000-4000-8000-000000000001", "name": "A", "kind": "agent", "roles": [], "environment": "conformance", "available_domains": [], "house_rules": {"revision": "h1", "rules": []}, "from_the_future": true}`)
		c.JSON(who)
	})
	c := connect(t, gw, signedIn(t))
	who, err := c.Whoami(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if kind, _ := who.Get("kind"); kind != "UNKNOWN" {
		t.Fatalf("kind %v, want UNKNOWN", kind)
	}
}
