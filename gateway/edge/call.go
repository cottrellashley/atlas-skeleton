package edge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"time"

	"github.com/jhgonline/fi-app-atlas/gateway/core/pipeline"
	"github.com/jhgonline/fi-app-atlas/gateway/core/registry"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
)

// maxDepth is the deepest a request body may nest.
const maxDepth = 64

// call is a POST to an interaction's binding. Everything it answers before
// the pipeline runs is the same for every address, bound or not: the
// method, the body's size, time and syntax, and the headers. What depends
// on the capability (its encodings above all) is decided only after the
// pipeline has decided the call's access (ATLAS-CORE §8; PR08, R107, R130).
func (s *Server) call(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.problem(w, r, protocol.NewProblem(protocol.CodeNotFound, "no such resource").WithNext("Call an interaction with POST at the address its binding names (§9.6.2)."))
		return
	}
	if ct := r.Header.Get(protocol.HeaderContentType); ct != "" {
		if mt, _, err := mime.ParseMediaType(ct); err != nil || mt != protocol.MediaJSON {
			s.problem(w, r, protocol.NewProblem(protocol.CodeInvalidRequest, "a request body is application/json").WithTarget(protocol.HeaderContentType))
			return
		}
	}
	body, prob := s.body(w, r)
	if prob != nil {
		s.problem(w, r, prob)
		return
	}
	value, err := ijson.ParseLimits(body, ijson.Limits{MaxBytes: int(s.opt.MaxBody), MaxDepth: maxDepth})
	if err != nil {
		s.problem(w, r, protocol.NewProblem(protocol.CodeInvalidDocument, "the body is not I-JSON: "+err.Error()))
		return
	}
	x := exchangeOf(r)
	res, ch, err := s.run(r, s.read(r, x.id, value))
	if err != nil {
		s.problem(w, r, err)
		return
	}
	defer s.finish(res, x)
	s.bound(w, r, res)
	s.answer(w, r, res, ch)
}

// body reads the request body: at most MaxBody bytes (MECH-002, before step
// 1), arriving within BodyTimeout and before the call's deadline (R066), so
// no caller, signed in or not, holds a connection by trickling a body.
func (s *Server) body(w http.ResponseWriter, r *http.Request) ([]byte, *protocol.Problem) {
	if r.ContentLength > s.opt.MaxBody {
		return nil, protocol.NewProblem(protocol.CodeInvalidRequest, fmt.Sprintf("the request body is larger than %d bytes", s.opt.MaxBody))
	}
	if r.Body == nil || r.Body == http.NoBody {
		return []byte("{}"), nil
	}
	x := exchangeOf(r)
	now := s.opt.Clock.Now()
	limit, byDeadline := now.Add(s.opt.BodyTimeout), false
	if !x.outer.IsZero() && x.outer.Before(limit) {
		limit, byDeadline = x.outer, true
	}
	// Set only while a body remains: net/http clears the read deadline
	// for its own read once the body ends, and a deadline left on that
	// read would cancel the call when it passed.
	rc := http.NewResponseController(w)
	if err := rc.SetReadDeadline(s.real(limit)); err != nil && !errors.Is(err, http.ErrNotSupported) {
		s.opt.Log.Warn("the request body's read deadline could not be set", "request_id", x.id, "err", err)
	}
	b, err := io.ReadAll(r.Body)
	if err == nil {
		x.read = true
		if err := rc.SetReadDeadline(time.Time{}); err != nil && !errors.Is(err, http.ErrNotSupported) {
			s.opt.Log.Warn("the request body's read deadline could not be cleared", "request_id", x.id, "err", err)
		}
		if len(bytes.TrimSpace(b)) == 0 {
			b = []byte("{}")
		}
		return b, nil
	}
	x.unread = true // and the writer closes the connection: what is left is never read
	var mbe *http.MaxBytesError
	switch {
	case errors.As(err, &mbe):
		return nil, protocol.NewProblem(protocol.CodeInvalidRequest, fmt.Sprintf("the request body is larger than %d bytes", mbe.Limit))
	case errors.Is(err, os.ErrDeadlineExceeded) && byDeadline:
		return nil, protocol.NewProblem(protocol.CodeDeadlineExceeded, "the request body had not arrived when the call's deadline passed").WithTarget(protocol.HeaderDeadline)
	case errors.Is(err, os.ErrDeadlineExceeded):
		return nil, protocol.NewProblem(protocol.CodeInvalidRequest, fmt.Sprintf("the request body did not arrive within %s", s.opt.BodyTimeout)).WithNext("Send the whole body at once; a request body is at most 1 MiB.")
	}
	// The connection ended before the body did: the caller went away.
	s.opt.Log.Info("the request body was cut short", "request_id", x.id, "err", err)
	x.gone = true
	return nil, protocol.NewProblem(protocol.CodeInvalidRequest, "the request body was cut short")
}

// read is the call's Atlas headers. One that does not parse as its type,
// or breaks the grammar its type is given, is absent (§9.6.3, Q-HDR-1): a
// call is refused for a header only by the pipeline, which knows whether
// the call can go on without it (an Idempotency-Key is needed only when
// the effect is not read, §6.7.8; R137).
func (s *Server) read(r *http.Request, id string, body ijson.Value) *pipeline.Call {
	c := &pipeline.Call{RequestID: id, Path: r.URL.Path, Body: body, Features: protocol.CoreFeatures()}
	if vs := r.Header.Values(protocol.HeaderAuthorization); len(vs) == 1 {
		c.Token, _ = protocol.ParseBearer(vs[0])
	}
	if f, ok := protocol.Field(r.Header, protocol.HeaderClient); ok {
		if client, err := protocol.ParseClient(f); err == nil {
			c.Client = &client
		}
	}
	if f, ok := protocol.Field(r.Header, protocol.HeaderClientFeatures); ok {
		if features, err := protocol.ParseFeatures(f); err == nil {
			c.Features = features
		}
	}
	if f, ok := protocol.Field(r.Header, protocol.HeaderProfileExpect); ok {
		if e, err := protocol.ParseExactHeader(f); err == nil {
			c.Expect = e.String()
		}
	}
	if f, ok := protocol.Field(r.Header, protocol.HeaderDeadline); ok {
		if d, err := protocol.ParseDeadline(f); err == nil {
			c.Deadline = d
		}
	}
	if f, ok := protocol.Field(r.Header, protocol.HeaderIdempotencyKey); ok {
		if k, err := protocol.ParseIdempotencyKey(f); err == nil {
			c.IdempotencyKey = k
		}
	}
	if f, ok := protocol.Field(r.Header, protocol.HeaderInteractive); ok {
		c.Interactive, _ = protocol.ParseBoolean(f)
	}
	// An identity a header claims changes nothing; the pipeline records it
	// (ATLAS-ACCESS-007).
	c.Claimed = r.Header.Get(HeaderOnBehalfOf)
	return c
}

// HeaderOnBehalfOf is the identity a caller may claim to act for: ignored,
// and recorded (06-2 §6.2, ATLAS-ACCESS-007).
const HeaderOnBehalfOf = "X-Atlas-On-Behalf-Of"

// negotiating is a pipeline that calls the edge back once a call's access
// is decided and before its request is validated, a limit taken or its
// work admitted: with the route it resolved from the snapshot it loaded
// for the call. A problem negotiate returns is the call's refusal. The
// edge then chooses the encoding without telling a caller who may not know
// the capability anything about its binding, and without running work it
// would refuse (PR08; needs: lane P1).
type negotiating interface {
	RunNegotiated(ctx context.Context, c *pipeline.Call, negotiate func(registry.Route) *protocol.Problem) (*pipeline.Result, error)
}

// run takes a call through the pipeline, and chooses its encoding once the
// pipeline has decided its access. Until the pipeline calls back, the edge
// chooses after Run: as confidential, but a call the edge cannot answer in
// any type it accepts has run its work first.
func (s *Server) run(r *http.Request, c *pipeline.Call) (*pipeline.Result, choice, error) {
	x := exchangeOf(r)
	var ch choice
	negotiate := func(rt registry.Route) *protocol.Problem {
		x.address = rt.Capability.Pinned.String() + ":" + rt.Interaction
		var prob *protocol.Problem
		ch, prob = choose(r.Header, c.Features, rt.Capability.Current.Interactions[rt.Interaction], r.URL.Path)
		return prob
	}
	if np, ok := any(s.opt.Pipeline).(negotiating); ok {
		res, err := np.RunNegotiated(r.Context(), c, negotiate)
		return res, ch, err
	}
	res, err := s.opt.Pipeline.Run(r.Context(), c)
	if err != nil {
		return nil, choice{}, err
	}
	if prob := negotiate(registry.Route{Capability: res.Capability, Interaction: res.Interaction.Name}); prob != nil {
		x.failed = prob
		s.finish(res, x)
		return nil, choice{}, prob
	}
	return res, ch, nil
}

// failing is an answer the edge can end with its own problem, which the
// pipeline records as the call's outcome (pipeline.Rows.Fail).
type failing interface{ Fail(err error) }

// finish ends the call: its rows, its context, its outcome. The outcome is
// what the caller received (R09, R138): the answer as the pipeline gave
// it; the problem the edge ended it with instead; or, for a caller that
// went away, a cancellation, which Close records.
func (s *Server) finish(res *pipeline.Result, x *exchange) {
	if x.failed != nil && !x.gone && res.Rows != nil {
		if f, ok := any(res.Rows).(failing); ok {
			f.Fail(x.failed)
		}
	}
	res.Close()
}

// bound puts the call's deadline on the answer's writes: a caller that
// stops reading holds the connection, its goroutine and its concurrency
// slot only until the deadline, and a grace to write the end that reports
// it (R066).
func (s *Server) bound(w http.ResponseWriter, r *http.Request, res *pipeline.Result) {
	if res.Deadline.IsZero() {
		return
	}
	if err := http.NewResponseController(w).SetWriteDeadline(s.real(res.Deadline.Add(s.opt.WriteGrace))); err != nil && !errors.Is(err, http.ErrNotSupported) {
		s.opt.Log.Warn("the answer's write deadline could not be set", "request_id", exchangeOf(r).id, "err", err)
	}
}
