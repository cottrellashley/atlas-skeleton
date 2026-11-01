package edge

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"atlas.local/gateway/core/registry"
	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/ijson"
	"atlas.local/sdk/go/protocol/profile"
)

// counting scripts the rig's provider to answer as standard does, and
// counts the calls that reach it.
func counting(r *rig) *atomic.Int64 {
	var n atomic.Int64
	r.provide(func(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
		n.Add(1)
		return r.standard(ctx, inv)
	})
	return &n
}

// R3-07, HANDOFF.md C2 (the lifecycle's step 3, "negotiate the
// representation, after authorization"): a call whose Accept its binding
// cannot answer is refused once its access is decided and before it is
// admitted. No provider runs, nothing is admitted, and its one fact is a
// refusal at the negotiation step. A caller without access is answered as
// before, without the binding's encodings.
func TestR3_07NegotiationComesBeforeAdmission(t *testing.T) {
	r := newRig(t, rigOptions{})
	calls := counting(r)
	a := r.call("t-alice", listAt, `{}`, "Accept", "text/csv")
	if a.status != http.StatusNotAcceptable || a.header.Get(protocol.HeaderErrorCode) != protocol.CodeInvalidRequest {
		t.Fatalf("an Accept the binding does not serve: %d %s", a.status, a.body)
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("the provider ran %d times for a call refused at negotiation", n)
	}
	facts := r.audit.Of(a.header.Get(protocol.HeaderRequestID))
	if len(facts) != 1 || facts[0].Kind != ports.Refusal || facts[0].Step != ports.StepNegotiate || facts[0].Code != protocol.CodeInvalidRequest {
		t.Fatalf("the facts of a call refused at negotiation: %+v", facts)
	}
	if hidden := r.call("t-carol", secretAt, `{}`, "Accept", "text/csv"); hidden.status != http.StatusNotFound {
		t.Fatalf("a confidential capability negotiated for a caller without access: %d %s", hidden.status, hidden.body)
	}
	if ok := r.call("t-alice", listAt, `{}`); ok.status != http.StatusOK || calls.Load() != 1 {
		t.Fatalf("a call that negotiates: %d %s (%d provider calls)", ok.status, ok.body, calls.Load())
	}
}

// moved is portfolio.moved@v1, made from portfolio.list@v1's first profile:
// its fetch moved address in two compatible updates, each also adding an
// optional request member, so a version holds three profiles at three
// addresses.
func moved(t *testing.T) ([]*profile.Profile, []string) {
	t.Helper()
	base := loadChain(t, "portfolio.list@v1", rename("portfolio.moved", func(*ijson.Object) {}))[0]
	addresses := []string{"/v1/data/portfolio.moved@v1:fetch", "/v1/data/portfolio.moved-b@v1:fetch", "/v1/data/portfolio.moved-c@v1:fetch"}
	chain := []*profile.Profile{base}
	for i, at := range addresses[1:] {
		doc := ijson.Clone(chain[len(chain)-1].Doc).(*ijson.Object)
		doc.Delete("fingerprint")
		ins, _ := doc.Get("interactions")
		fetch, _ := ins.(*ijson.Object).Get("fetch")
		bs, _ := fetch.(*ijson.Object).Get("bindings")
		bs.([]ijson.Value)[0].(*ijson.Object).Set("address", at)
		schemas, _ := doc.Get("schemas")
		req, _ := schemas.(*ijson.Object).Get("request")
		props, _ := req.(*ijson.Object).Get("properties")
		opt := ijson.NewObject()
		opt.Set("type", []ijson.Value{"string", "null"})
		opt.Set("description", "An optional note, added in an update.")
		opt.Set("default", nil)
		props.(*ijson.Object).Set("note_"+string(rune('a'+i)), opt)
		ev, _ := doc.Get("evolution")
		ev.(*ijson.Object).Set("previous_profile", chain[len(chain)-1].Exact().String())
		b, err := ijson.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		p, err := registry.Load(b)
		if err != nil {
			t.Fatal(err)
		}
		chain = append(chain, p)
	}
	return chain, addresses
}

// R3-08: an address an older profile bound, which the registry still
// routes, is negotiated against that profile's own binding, so a client
// pinned to it is answered after two compatible updates moved the
// address, and its outcome is recorded under the current profile.
func TestR3_08AnOldAddressIsNegotiatedAgainstItsOwnBinding(t *testing.T) {
	r := newRig(t, rigOptions{})
	chain, addresses := moved(t)
	if err := r.reg.Register(registry.Entry{Profiles: chain, Provider: serve(r.standard)}); err != nil {
		t.Fatalf("two compatible moves of an address: %v", err)
	}
	current := chain[len(chain)-1].Exact().String()
	for _, at := range addresses {
		a := r.call("t-alice", at, `{}`)
		if a.status != http.StatusOK {
			t.Fatalf("%s: %d %s", at, a.status, a.body)
		}
		facts := r.audit.Of(a.header.Get(protocol.HeaderRequestID))
		if len(facts) < 2 || facts[1].Kind != ports.Outcome || facts[1].Code != "" || facts[1].Exact != current {
			t.Fatalf("%s: the facts %+v", at, facts)
		}
	}
}

// echoAt is portfolio.echo@v1: a record capability, for the lifecycle's
// record cases.
const echoAt = "/v1/data/portfolio.echo@v1:fetch"

const voc = `"$schema": "https://json-schema.org/draft/2020-12/schema", "$vocabulary": {"https://json-schema.org/draft/2020-12/vocab/core": true, "https://json-schema.org/draft/2020-12/vocab/applicator": true, "https://json-schema.org/draft/2020-12/vocab/validation": true, "https://json-schema.org/draft/2020-12/vocab/meta-data": true, "https://json-schema.org/draft/2020-12/vocab/format-assertion": true, "https://atlas.jhg.com/vocab/2026-10": false}`

// withEcho registers portfolio.echo@v1, whose provider answers the record
// it is asked for, counting the calls that reach it.
func withEcho(t *testing.T, r *rig) *atomic.Int64 {
	t.Helper()
	p, err := registry.Load([]byte(`{
	 "identity": {"id": "portfolio.echo", "version": 1, "title": "Echo", "summary": "Says what it is told.", "domain": "portfolio", "owner": "QFI"},
	 "properties": {"family": "data", "stability": "official", "lifecycle": "stable", "visibility": "listed", "traits": []},
	 "interactions": {"fetch": {"effect": "read", "result": "record", "delivery": "single", "execution": ["gateway"], "takes": "request", "returns": "result",
	  "deadline": {"default": "PT30S", "max": "PT2M"},
	  "bindings": [{"protocol": "https", "server": "gateway", "method": "POST", "address": "` + echoAt + `", "encodings": ["json"], "auth": "bearer"}]}},
	 "schemas": {
	  "request": {` + voc + `, "type": "object", "additionalProperties": false, "required": ["text"], "properties": {"text": {"type": "string", "description": "What to say."}}},
	  "result": {` + voc + `, "type": "object", "required": ["text"], "properties": {"text": {"type": "string", "description": "What was said."}}}},
	 "errors": [], "meaning": {"grain": "one answer", "source": "the request"},
	 "usage": {"when": "You need an echo.", "examples": [{"title": "Hello", "interaction": "fetch", "request": {"text": "hello"}}]},
	 "evolution": {"lens": [], "changelog": [{"date": "2026-10-01", "change": "First."}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	var n atomic.Int64
	echo := serve(func(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
		n.Add(1)
		text, _ := inv.Request.Get("text")
		o := ijson.NewObject()
		o.Set("text", text)
		return &ports.Answer{Record: o}, nil
	})
	if err := r.reg.Register(registry.Entry{Profiles: []*profile.Profile{p}, Provider: echo}); err != nil {
		t.Fatal(err)
	}
	return &n
}

// brokenWriter is a response whose body cannot be written: a caller that
// went away as its answer was sent.
type brokenWriter struct{ h http.Header }

func (w *brokenWriter) Header() http.Header       { return w.h }
func (w *brokenWriter) WriteHeader(int)           {}
func (w *brokenWriter) Write([]byte) (int, error) { return 0, errors.New("write: broken pipe") }

// R3-07, HANDOFF.md C2's exit matrix: the persisted outcome is what the
// caller received, for records as for tables. A delivered answer is a
// success; a call refused before dispatch runs no work and leaves one
// refusal; and an answer the caller went away from as it was sent is a
// cancellation, never a success.
func TestR3_07EveryOutcomeIsWhatTheCallerReceived(t *testing.T) {
	r := newRig(t, rigOptions{})
	echoes := withEcho(t, r)
	lists := counting(r)
	outcome := func(id string) ports.Fact {
		t.Helper()
		for _, f := range r.audit.Of(id) {
			if f.Kind == ports.Outcome || f.Kind == ports.Refusal {
				return f
			}
		}
		t.Fatalf("no outcome for %s: %+v", id, r.audit.Of(id))
		return ports.Fact{}
	}
	for _, c := range []struct {
		name, path, body string
		calls            *atomic.Int64
	}{
		{"record", echoAt, `{"text": "hello"}`, echoes},
		{"table", listAt, `{}`, lists},
	} {
		before := c.calls.Load()
		ok := r.call("t-alice", c.path, c.body)
		if f := outcome(ok.header.Get(protocol.HeaderRequestID)); ok.status != http.StatusOK || f.Kind != ports.Outcome || f.Code != "" {
			t.Errorf("%s delivered: %d, outcome %+v", c.name, ok.status, f)
		}
		bad := r.call("t-alice", c.path, `{"nope": 1}`)
		if f := outcome(bad.header.Get(protocol.HeaderRequestID)); bad.status != http.StatusBadRequest || f.Kind != ports.Refusal || f.Step != ports.StepRequest {
			t.Errorf("%s refused before dispatch: %d, %+v", c.name, bad.status, f)
		}
		req := httptest.NewRequest(http.MethodPost, c.path, strings.NewReader(c.body))
		req.Header = r.request("t-alice", c.path, c.body).Header
		w := &brokenWriter{h: http.Header{}}
		r.edge.ServeHTTP(w, req)
		f := outcome(w.h.Get(protocol.HeaderRequestID))
		if f.Kind != ports.Outcome || f.Code != ports.CodeCancelled {
			t.Errorf("%s the caller went away from: outcome %+v", c.name, f)
		}
		if got := c.calls.Load() - before; got != 2 {
			t.Errorf("%s: the provider ran %d times for two dispatched calls", c.name, got)
		}
	}
}

// panicWriter is a response whose first body write panics, as a fault in
// writing an answer would.
type panicWriter struct {
	h      http.Header
	status int
	writes int
}

func (w *panicWriter) Header() http.Header { return w.h }
func (w *panicWriter) WriteHeader(s int)   { w.status = s }
func (w *panicWriter) Write(b []byte) (int, error) {
	w.writes++
	if w.writes == 1 {
		panic("the answer could not be written")
	}
	return len(b), nil
}

// The review of C2 (R3-07): a panic while an answer is written is the
// call's internal failure, for a record as for a table, never a success or
// a cancellation, and the caller is answered internal.
func TestR3_07APanicWhileAnsweringIsInternal(t *testing.T) {
	r := newRig(t, rigOptions{})
	withEcho(t, r)
	for _, c := range []struct{ name, path, body string }{
		{"record", echoAt, `{"text": "hello"}`},
		{"table", listAt, `{}`},
	} {
		req := httptest.NewRequest(http.MethodPost, c.path, strings.NewReader(c.body))
		req.Header = r.request("t-alice", c.path, c.body).Header
		w := &panicWriter{h: http.Header{}}
		r.edge.ServeHTTP(w, req)
		var out ports.Fact
		for _, f := range r.audit.Of(w.h.Get(protocol.HeaderRequestID)) {
			if f.Kind == ports.Outcome {
				out = f
			}
		}
		if out.Code != protocol.CodeInternal {
			t.Errorf("%s: a panic while answering was recorded as %+v", c.name, out)
		}
	}
}
