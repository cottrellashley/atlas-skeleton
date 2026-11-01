package atlas

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/documents"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/grammar"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/profile"
)

// Request is a request record as JSON text, for a call written in Go
// source: atlas.Request(`{"portfolios": ["147HYC"]}`).
type Request string

// CallOption is a transport option of one call: never part of the request
// record (§9.6.2).
type CallOption func(*callOptions)

type callOptions struct {
	deadline       time.Duration
	accept         []string
	idempotencyKey string
}

// WithDeadline bounds the call (Atlas-Deadline), from the moment Call is
// called to the end of its answer; the interaction's default otherwise,
// and never past its maximum.
func WithDeadline(d time.Duration) CallOption { return func(o *callOptions) { o.deadline = d } }

// WithAccept are the media types to ask for, in preference order.
func WithAccept(media ...string) CallOption { return func(o *callOptions) { o.accept = media } }

// WithIdempotencyKey names the logical execution of a call whose effect is
// not read; the client makes one when none is given, and keeps it for
// every retry.
func WithIdempotencyKey(k string) CallOption { return func(o *callOptions) { o.idempotencyKey = k } }

// MaxRetries is how many times a retryable problem is repeated (§9.7.7).
const MaxRetries = 3

// builtinSchemas are each built-in's request, by capability and interaction.
var builtinSchemas = map[string]string{
	"atlas.whoami:fetch": "whoami_request", "atlas.catalog:fetch": "catalog_request", "atlas.search:fetch": "search_request",
	"atlas.describe:fetch": "describe_request", "atlas.guide:fetch": "guide_request", "atlas.access:fetch": "access_request",
	"atlas.usage:fetch": "usage_request", "atlas.setup:fetch": "setup_request", "atlas.clients:fetch": "clients_request",
	"atlas.operation:get": "operation_get_request", "atlas.operation:list": "operation_list_request",
}

// builtinAnswers are the documents each built-in answers (§9.3.2): a
// record's, or for a page, its items'. Every answer is checked against its
// document before it is used (§9.3 rule 1).
var builtinAnswers = map[string]struct{ record, item string }{
	"atlas.whoami:fetch": {record: "builtins#whoami_result"}, "atlas.catalog:fetch": {record: "catalog-page"},
	"atlas.search:fetch": {record: "catalog-page"}, "atlas.describe:fetch": {record: "describe"},
	"atlas.guide:fetch": {record: "builtins#guide_result"}, "atlas.access:fetch": {record: "page", item: "builtins#access_entry"},
	"atlas.usage:fetch": {record: "page", item: "builtins#usage_entry"}, "atlas.setup:fetch": {record: "builtins#setup_result"},
	"atlas.clients:fetch": {record: "page", item: "builtins#client_entry"}, "atlas.operation:get": {record: "operation"},
	"atlas.operation:list": {record: "page", item: "operation"}, "atlas.operation:cancel": {record: "operation"},
}

// Call executes one interaction once (ATLAS-CORE §6): the request is
// checked against the interaction's contract before anything is sent, and
// the answer against it as it is read. request is a map, a struct, an
// *ijson.Object, or a Request. The call's deadline bounds everything it
// does, the profile it fetches first and the answer's whole body included.
func (c *Client) Call(ctx context.Context, address string, request any, opts ...CallOption) (*Result, error) {
	var o callOptions
	for _, f := range opts {
		f(&o)
	}
	addr, err := grammar.ParseAddress(address)
	if err != nil {
		return nil, protocol.NewProblem(protocol.CodeInvalidRequest, err.Error()).WithTarget("address").WithNext("Name an interaction: <id>@v<N>:<interaction>.")
	}
	body, err := requestRecord(request)
	if err != nil {
		return nil, err
	}
	// The call's context ends with its result: when it is read, closed, or
	// its deadline passes. Its deadline counts from now (R061).
	start := c.opt.Clock.Now()
	var end context.CancelFunc
	if o.deadline > 0 {
		ctx, end = context.WithTimeout(ctx, o.deadline)
	} else {
		ctx, end = context.WithCancel(ctx)
	}
	handed := false
	defer func() {
		if !handed {
			end()
		}
	}()
	if grammar.Builtin(addr.ID) {
		r, err := c.callBuiltin(ctx, addr, body, o, start, end)
		handed = err == nil
		return r, err
	}
	pinned, ok := addr.Pinned()
	if !ok {
		// A bare id is the capability's current stable version, as the
		// gateway names it, for this process only (§9.7.1; gaps/2.md D4.3).
		if pinned, err = c.resolveBare(ctx, addr.ID); err != nil {
			return nil, err
		}
	}
	d, err := c.describeExact(ctx, pinned.String(), "")
	if err != nil {
		return nil, err
	}
	p := d.profile // the reader, and the routing profile (gaps/2.md D4.4)
	in := p.Interactions[addr.Interaction]
	if in == nil {
		return nil, protocol.NewProblem(protocol.CodeNotFound, pinned.String()+" has no interaction "+addr.Interaction).WithTarget("address").WithNext("atlas describe " + pinned.String() + " lists its interactions.")
	}
	features, _ := protocol.ParseFeatures(c.features)
	for _, t := range p.Traits {
		if t.Critical && !features.Understands(strings.ToLower(t.Name)) {
			return nil, protocol.NewProblem(protocol.CodeCriticalTraitUnsupported, "the capability's trait "+t.Name+" is critical, and this client does not implement it").WithNext("Use a client that implements " + t.Name + ".")
		}
	}
	if s := p.Schemas[in.Takes]; s != nil {
		if fs := s.Validate(body); len(fs) > 0 {
			return nil, protocol.InvalidRequest(fs).WithNext("Correct the request at target; atlas describe gives its schema.")
		}
	}
	var columns []string
	if v, ok := body.Get("columns"); ok && v != nil && p.HasTrait("Projectable") {
		for _, x := range v.([]ijson.Value) {
			columns = append(columns, x.(string))
		}
	}
	b, err := binding(in)
	if err != nil {
		return nil, err
	}
	deadline := in.Deadline.Default
	if o.deadline > 0 {
		deadline = o.deadline
	}
	if in.Deadline.Max > 0 && deadline > in.Deadline.Max {
		deadline = in.Deadline.Max // CORE-DEADLINE-001: never past the interaction's maximum
	}
	if deadline <= 0 {
		deadline = BuiltinDeadline
	}
	h := http.Header{}
	exact, _ := protocol.FormatExact(p.Exact())
	h.Set(protocol.HeaderProfileExpect, exact)
	if in.Effect != "read" {
		k := o.idempotencyKey
		if k == "" {
			k = newID()
		}
		v, err := protocol.FormatIdempotencyKey(k)
		if err != nil {
			return nil, protocol.NewProblem(protocol.CodeInvalidRequest, "the idempotency key: "+err.Error()).WithTarget(protocol.HeaderIdempotencyKey)
		}
		h.Set(protocol.HeaderIdempotencyKey, v)
	}
	accept := o.accept
	if accept == nil {
		for _, e := range []string{"ndjson", "json", "arrow"} {
			if slices.Contains(b.Encodings, e) {
				m, _ := protocol.MediaFor(e)
				accept = append(accept, m)
			}
		}
	}
	h.Set(protocol.HeaderAccept, strings.Join(accept, ", "))
	// One deadline for the whole call, from its start (R3-06): discovery has
	// come out of it, and the request is not sent once it has passed.
	at := start.Add(deadline)
	if !c.opt.Clock.Now().Before(at) {
		return nil, protocol.NewProblem(protocol.CodeDeadlineExceeded, "the call reached its deadline before its request was sent").WithNext("Repeat the call with a later deadline.")
	}
	callCtx, cancel := context.WithTimeout(ctx, at.Sub(c.opt.Clock.Now()))
	done := func() { cancel(); end() }
	res, err := c.send(callCtx, c.servers[b.Server]+b.Address, body, h, at)
	if err != nil {
		done()
		return nil, err
	}
	r, err := c.result(callCtx, done, res, p, in, columns, "")
	handed = err == nil
	return r, err
}

// resolveBare is a bare id's current pinned id: describe by id, once per
// process (gaps/2.md D4.3: never persisted).
func (c *Client) resolveBare(ctx context.Context, id string) (grammar.Pinned, error) {
	c.mu.Lock()
	pinned := c.bare[id]
	c.mu.Unlock()
	if pinned == "" {
		rec, err := c.record(ctx, "atlas.describe@v1:fetch", map[string]any{"by": "id", "id": id})
		if err != nil {
			return grammar.Pinned{}, err
		}
		d, err := c.keep(rec)
		if err != nil {
			return grammar.Pinned{}, err
		}
		pinned = grammar.Pinned{ID: d.profile.ID, Version: d.profile.Version}.String()
		c.mu.Lock()
		c.bare[id] = pinned
		c.mu.Unlock()
		c.point(pinned, d.profile.Exact().String())
	}
	return grammar.ParsePinned(pinned)
}

// callBuiltin calls an atlas.* interaction at the binding the bootstrap
// names: no profile is read first, and no Atlas-Profile-Expect is sent.
func (c *Client) callBuiltin(ctx context.Context, addr grammar.Address, body *ijson.Object, o callOptions, start time.Time, end func()) (*Result, error) {
	key := addr.ID + ":" + addr.Interaction
	if def, ok := builtinSchemas[key]; ok {
		if fs := documents.Validate("builtins#"+def, body); len(fs) > 0 {
			return nil, protocol.InvalidRequest(fs)
		}
	}
	builtins, _ := c.bootstrap.Get("builtins")
	entry, _ := builtins.(*ijson.Object).Get(addr.ID)
	var bindings []ijson.Value
	if e, ok := entry.(*ijson.Object); ok {
		in, _ := e.Get("interactions")
		if i, ok := in.(*ijson.Object); ok {
			if x, ok := i.Get(addr.Interaction); ok {
				bs, _ := x.(*ijson.Object).Get("bindings")
				bindings, _ = bs.([]ijson.Value)
			}
		}
	}
	for _, b := range bindings {
		bo := b.(*ijson.Object)
		if p, _ := bo.Get("protocol"); p != "https" {
			continue
		}
		server, _ := bo.Get("server")
		address, _ := bo.Get("address")
		base, ok := c.servers[server.(string)]
		if !ok {
			return nil, protocol.NewProblem(protocol.CodeInvalidDocument, "the bootstrap binds "+key+" to a server it does not name: "+server.(string))
		}
		h := http.Header{}
		h.Set(protocol.HeaderAccept, protocol.MediaJSON)
		deadline := BuiltinDeadline
		if o.deadline > 0 {
			deadline = o.deadline
		}
		callCtx, cancel := context.WithTimeout(ctx, deadline)
		done := func() { cancel(); end() }
		res, err := c.send(callCtx, base+address.(string), body, h, start.Add(deadline))
		if err != nil {
			done()
			return nil, err
		}
		return c.result(callCtx, done, res, nil, nil, nil, key)
	}
	return nil, protocol.NewProblem(protocol.CodeNotFound, "the gateway's bootstrap names no binding for "+key)
}

func binding(in *profile.Interaction) (profile.Binding, error) {
	for _, b := range in.Bindings {
		if b.Protocol == "https" {
			return b, nil
		}
	}
	return profile.Binding{}, protocol.NewProblem(protocol.CodeNotFound, "the interaction has no binding this client speaks").WithNext("atlas describe shows its bindings and what each needs.")
}

// requestRecord is the request as an I-JSON object. A value JSON cannot
// carry (a NaN, an infinity) is refused before anything is sent.
func requestRecord(request any) (*ijson.Object, error) {
	var v ijson.Value
	var err error
	switch r := request.(type) {
	case nil:
		return ijson.NewObject(), nil
	case *ijson.Object:
		return r, nil
	case Request:
		v, err = ijson.Parse([]byte(r))
	case string:
		v, err = ijson.Parse([]byte(r))
	case []byte:
		v, err = ijson.Parse(r)
	default:
		if v, err = ijson.FromGo(r); err != nil {
			return nil, protocol.NewProblem(protocol.CodeInvalidRequest, "the request is not a JSON record: "+err.Error()).WithTarget("")
		}
	}
	if err != nil {
		return nil, protocol.NewProblem(protocol.CodeInvalidRequest, "the request is not I-JSON: "+err.Error()).WithTarget("")
	}
	o, ok := v.(*ijson.Object)
	if !ok {
		return nil, protocol.NewProblem(protocol.CodeInvalidRequest, "a request record is a JSON object").WithTarget("")
	}
	return o, nil
}

// send POSTs a call, bounded by ctx, whose deadline is at (by the
// client's clock), which Atlas-Deadline carries: the protocol's headers on
// every attempt; a token refreshed and
// the call repeated once after invalid_token (§9.6.6); and a retryable
// problem repeated at most MaxRetries times, after its retry_after or a
// backoff with jitter, never past the deadline (§9.7.7). Every request is
// replayable: its body is in memory. A problem answer is returned as its
// error.
func (c *Client) send(ctx context.Context, url string, body *ijson.Object, extra http.Header, at time.Time) (*http.Response, error) {
	payload, err := ijson.Marshal(body)
	if err != nil {
		return nil, protocol.NewProblem(protocol.CodeInvalidRequest, "the request is not I-JSON: "+err.Error()).WithTarget("")
	}
	if err := c.reaches(url); err != nil {
		return nil, err
	}
	failed := "" // the token the gateway refused, once
	for retries := 0; ; {
		token, err := c.tokens.token(ctx, failed)
		if err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			return nil, protocol.NewProblem(protocol.CodeInvalidDocument, "the bootstrap's binding is not a URL: "+err.Error())
		}
		req.Header = extra.Clone()
		req.Header.Set(protocol.HeaderContentType, protocol.MediaJSON)
		req.Header.Set(protocol.HeaderAuthorization, "Bearer "+token)
		req.Header.Set(protocol.HeaderClient, c.clientHdr)
		req.Header.Set(protocol.HeaderClientFeatures, c.features)
		req.Header.Set(protocol.HeaderDeadline, protocol.FormatDeadline(at))
		req.Header.Set("traceparent", traceparent())
		if c.opt.Interactive {
			req.Header.Set(protocol.HeaderInteractive, protocol.FormatBoolean(true))
		}
		res, err := c.opt.HTTP.Do(req)
		var p *protocol.Problem
		wait := time.Duration(-1)
		switch {
		case err != nil:
			if e := ended(ctx); e != nil {
				return nil, e
			}
			p = protocol.NewProblem(protocol.CodeUnavailable, "the gateway cannot be reached: "+err.Error()).WithRetryAfter(1)
		case res.StatusCode == http.StatusUnauthorized && failed == "" && invalidToken(res.Header.Get(protocol.HeaderWWWAuthenticate)):
			drain(res)
			failed = token // refresh it, and repeat once (CORE-SIGNIN-003); a claims challenge is not refreshed (CORE-SIGNIN-004)
			continue
		case res.StatusCode >= 300 && res.StatusCode < 400:
			drain(res)
			return nil, protocol.NewProblem(protocol.CodeUpstreamFailed, fmt.Sprintf("the gateway redirected the call (%d), and a call goes only to its binding", res.StatusCode))
		case res.StatusCode >= 400:
			if d, err := protocol.ParseRetryAfter(res.Header.Get(protocol.HeaderRetryAfter)); err == nil {
				wait = d
			}
			p = problemOf(res)
		default:
			return res, nil
		}
		if !p.Retryable || retries >= MaxRetries {
			return nil, p
		}
		if wait < 0 {
			if p.RetryAfter > 0 {
				wait = time.Duration(p.RetryAfter) * time.Second
			} else {
				wait = c.backoff(retries)
			}
		}
		if c.opt.Clock.Now().Add(wait).After(at) {
			return nil, p // no time to repeat it: its problem is the answer
		}
		if err := sleep(ctx, wait); err != nil {
			return nil, err
		}
		retries++
	}
}

// invalidToken reports whether a 401's challenge says the token is not
// valid (RFC 6750 §3.1), which a refreshed token can cure; a claims
// challenge only a person signing in again can (CORE-SIGNIN-004).
func invalidToken(field string) bool {
	c, err := protocol.ParseChallenge(field)
	return err == nil && c.Error == "invalid_token"
}

// backoff is §9.7.7's wait before retry n (from 0) when the problem names
// none: min(30, 0.5 × 2^n) seconds, plus a random jitter of up to 25%.
func (c *Client) backoff(n int) time.Duration {
	base := math.Min(30, 0.5*math.Pow(2, float64(n)))
	return time.Duration(base * (1 + 0.25*c.jitter()) * float64(time.Second))
}

// sleep waits d, or until ctx ends.
func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ended(ctx)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ended(ctx)
	}
}

// ended is the problem for a call whose context is over, or nil.
func ended(ctx context.Context) error {
	switch {
	case ctx == nil:
		return nil
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return protocol.NewProblem(protocol.CodeDeadlineExceeded, "the call did not complete before its deadline")
	case ctx.Err() != nil:
		return protocol.NewProblem(protocol.CodeDeadlineExceeded, "the call was cancelled")
	}
	return nil
}

// reaches refuses to send a credential anywhere but the gateway connected
// to (R26): a bootstrap that names a server on another origin cannot
// redirect a token there.
func (c *Client) reaches(target string) error {
	o, err := Origin(target)
	if err != nil {
		return protocol.NewProblem(protocol.CodeInvalidDocument, "the bootstrap's binding is not an http or https URL")
	}
	if o != c.origin {
		return protocol.NewProblem(protocol.CodeUnauthenticated, fmt.Sprintf("the gateway's bootstrap sends this call to %s, which is not the gateway connected to (%s), so no sign-in is sent there", o, c.origin)).WithNext("Connect to " + o + " itself, if you trust it.")
	}
	if !secure(o) {
		return protocol.NewProblem(protocol.CodeUnauthenticated, o+" is plain HTTP and not on this machine, so no sign-in is sent to it").WithNext("Use the gateway's https URL.")
	}
	return nil
}

func drain(res *http.Response) {
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, maxProblem))
	res.Body.Close()
}

// problemOf reads a problem answer: its document, as the gateway wrote it,
// or, without one, the problem its status implies.
func problemOf(res *http.Response) *protocol.Problem {
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, maxProblem))
	var p protocol.Problem
	if err == nil {
		if _, perr := ijson.Parse(b); perr == nil && json.Unmarshal(b, &p) == nil && p.Code != "" && grammar.ProblemCode(p.Code) == nil {
			if p.Status == 0 {
				p.Status = res.StatusCode
			}
			return &p
		}
	}
	code := map[int]string{400: protocol.CodeInvalidRequest, 401: protocol.CodeUnauthenticated, 403: protocol.CodeNotGranted, 404: protocol.CodeNotFound, 409: protocol.CodeInProgress, 429: protocol.CodeLimitExceeded, 503: protocol.CodeUnavailable, 504: protocol.CodeDeadlineExceeded}[res.StatusCode]
	if code == "" {
		code = protocol.CodeUpstreamFailed
	}
	q := protocol.NewProblem(code, fmt.Sprintf("the gateway answered %d without a problem document", res.StatusCode))
	q.RequestID = res.Header.Get(protocol.HeaderRequestID)
	return q
}

// result reads an answer, bounded by ctx, which cancel ends: a record, a
// table's rows as they arrive, or a problem. reader is nil for a built-in,
// which names itself in builtin.
func (c *Client) result(ctx context.Context, cancel context.CancelFunc, res *http.Response, reader *profile.Profile, in *profile.Interaction, columns []string, builtin string) (*Result, error) {
	fail := func(err error) (*Result, error) {
		res.Body.Close()
		cancel()
		return nil, err
	}
	media, _, _ := mime.ParseMediaType(res.Header.Get(protocol.HeaderContentType))
	if media == protocol.MediaProblem {
		p := problemOf(res)
		cancel()
		return nil, p
	}
	r := &Result{RequestID: res.Header.Get(protocol.HeaderRequestID), Profile: reader, Interaction: in, Columns: columns, ctx: ctx, cancel: cancel, hook: c.opt.Warn}
	if f, ok := protocol.Field(res.Header, protocol.HeaderProfile); ok {
		if e, err := protocol.ParseExactHeader(f); err == nil {
			r.Exact = e.String()
		}
	}
	writer := reader
	if reader != nil && r.Exact != "" && r.Exact != reader.Exact().String() {
		d, err := c.describeExact(ctx, "", r.Exact)
		if err != nil {
			return fail(err)
		}
		writer = d.profile
		if r.plan, err = c.plan(ctx, reader, writer, in, columns); err != nil {
			return fail(err)
		}
		c.point(grammar.Pinned{ID: writer.ID, Version: writer.Version}.String(), r.Exact) // the pointer follows Atlas-Profile (gaps/2.md D4.3)
	}
	r.Writer = writer
	r.deprecation(res, c.opt.Clock.Now(), writer)
	switch media {
	case protocol.MediaNDJSON:
		r.ndjson(res.Body)
		return r, nil
	case protocol.MediaArrowStream:
		if err := r.arrow(res.Body); err != nil {
			cancel()
			return nil, err
		}
		return r, nil
	}
	defer cancel()
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, maxAnswer+1))
	if err != nil {
		if e := ended(ctx); e != nil {
			return nil, e
		}
		return nil, protocol.NewProblem(protocol.CodeUpstreamFailed, "the answer was cut short: "+err.Error())
	}
	if len(b) > maxAnswer {
		return nil, protocol.NewProblem(protocol.CodeInvalidDocument, fmt.Sprintf("the answer is larger than %d bytes", maxAnswer))
	}
	v, err := ijson.Parse(b)
	if err != nil {
		return nil, protocol.NewProblem(protocol.CodeInvalidDocument, "the answer is not I-JSON: "+err.Error())
	}
	if builtin != "" {
		if err := checkBuiltin(builtin, v); err != nil {
			return nil, err
		}
	}
	switch x := v.(type) {
	case *ijson.Object:
		if reader != nil && in.Result != "collection" {
			if r.plan != nil {
				if x, err = r.plan.Record(x); err != nil {
					return nil, protocol.ResolutionFailed(err)
				}
			}
			if s := reader.Schemas[in.Returns]; s != nil {
				if fs := s.Validate(x); len(fs) > 0 {
					return nil, protocol.NewProblem(protocol.CodeInvalidDocument, fmt.Sprintf("the answer breaks the capability's contract at %s (%s)", fs[0].Pointer, fs[0].Keyword)).WithTarget(fs[0].Pointer)
				}
			}
		}
		r.Record = x
		r.done = true
	case []ijson.Value:
		rows := make([]*ijson.Object, 0, len(x))
		for _, e := range x {
			o, ok := e.(*ijson.Object)
			if !ok {
				return nil, protocol.NewProblem(protocol.CodeInvalidDocument, "a table's row is an object")
			}
			rows = append(rows, o)
		}
		r.memory(rows)
	default:
		return nil, protocol.NewProblem(protocol.CodeInvalidDocument, "an answer is a record or a table")
	}
	return r, nil
}

// checkBuiltin holds a built-in's answer to its document (§9.3.2): a
// page's items each to theirs.
func checkBuiltin(key string, v ijson.Value) error {
	doc, ok := builtinAnswers[key]
	if !ok {
		return nil
	}
	if p := checkDocument(doc.record, v); p != nil {
		return p
	}
	if doc.item == "" {
		return nil
	}
	items, _ := v.(*ijson.Object).Get("items")
	for i, it := range items.([]ijson.Value) {
		if p := checkDocument(doc.item, it); p != nil {
			p.Target = fmt.Sprintf("/items/%d%s", i, p.Target)
			return p
		}
	}
	return nil
}
