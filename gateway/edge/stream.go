package edge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"runtime/debug"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/jhgonline/fi-app-atlas/gateway/core/pipeline"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/arrowschema"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/frame"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
)

// flushEvery is how many rows go out before a flush, so a slow stream
// reaches the client as it is produced, and the edge holds at most this
// many rows of it.
const flushEvery = 256

// arrowBatch is how many rows go in one record batch made from rows.
const arrowBatch = 4096

// push sends what the answer has buffered to the connection. It reports
// false, having recorded why, when the answer cannot go on.
func (s *Server) push(w http.ResponseWriter, r *http.Request, res *pipeline.Result) bool {
	if err := http.NewResponseController(w).Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		s.broke(r, res, err)
		return false
	}
	return true
}

// guarded runs one step of reading an answer. A panic in it, in a
// provider's stream or in the edge's own conversion, becomes the problem
// internal, so the stream still ends with its end line or final batch
// (§6.7.2 row 1, R129) and the client reads internal, not a cut stream it
// would take for upstream_failed and retry.
func guarded[T any](s *Server, r *http.Request, step func() (T, error)) (v T, err error) {
	defer func() {
		p := recover()
		if p == nil {
			return
		}
		if p == http.ErrAbortHandler { //nolint:errorlint // net/http's sentinel, compared by identity
			panic(p)
		}
		x := exchangeOf(r)
		s.opt.Log.Error("panic", "request_id", x.id, "panic", fmt.Sprint(p), "stack", string(debug.Stack()))
		x.failed = protocol.NewProblem(protocol.CodeInternal, "an internal error occurred")
		var zero T
		v, err = zero, x.failed
	}()
	return step()
}

// next is a table's next row, guarded.
func (s *Server) next(r *http.Request, t rowTable) (*ijson.Object, error) {
	return guarded(s, r, t.Next)
}

// broke records why an answer could not be written to its end: the caller
// stopped reading until the call's deadline passed (deadline_exceeded), or
// went away, which the access log records as 499 (§6.7.3, R138).
func (s *Server) broke(r *http.Request, res *pipeline.Result, err error) {
	x := exchangeOf(r)
	if errors.Is(err, os.ErrDeadlineExceeded) || !res.Deadline.IsZero() && !s.opt.Clock.Now().Before(res.Deadline) {
		x.code = protocol.CodeDeadlineExceeded
		x.failed = protocol.NewProblem(protocol.CodeDeadlineExceeded, "the caller did not read its answer before the call's deadline")
	} else {
		x.gone = true
	}
	s.opt.Log.Info("the answer was not written to its end", "request_id", x.id, "err", err)
}

// left reports a caller who went away while its answer was being made: its
// context ended before the call's deadline did.
func (s *Server) left(r *http.Request) bool {
	if errors.Is(r.Context().Err(), context.Canceled) {
		exchangeOf(r).gone = true
		return true
	}
	return false
}

// ndjson streams a table as NDJSON, ended by its end line (§9.6.5). A
// failure before the first row is a problem with its own status; after
// it, the end line's problem, since the status was already sent.
func (s *Server) ndjson(w http.ResponseWriter, r *http.Request, res *pipeline.Result, t rowTable) {
	x := exchangeOf(r)
	first, err := s.next(r, t)
	if p := s.endProblem(r, t, err); first == nil && p != nil && p.Code != protocol.CodeOutputLimitExceeded {
		s.problem(w, r, p)
		return
	}
	w.Header().Set(protocol.HeaderContentType, protocol.MediaNDJSON)
	nw := frame.NewNDJSONWriter(w)
	sent := 0
	for row := first; row != nil; {
		if werr := nw.Row(row); werr != nil {
			s.broke(r, res, werr)
			return
		}
		if sent++; sent%flushEvery == 0 {
			if werr := nw.Flush(); werr != nil {
				s.broke(r, res, werr)
				return
			}
			if !s.push(w, r, res) {
				return
			}
		}
		row, err = s.next(r, t)
	}
	if s.left(r) {
		return
	}
	end := frame.End{Truncated: t.Truncated(), Usage: usage(t), Problem: s.endProblem(r, t, err)}
	if end.Problem != nil {
		x.code = end.Problem.Code
	}
	if werr := nw.End(end); werr != nil {
		s.broke(r, res, werr)
		return
	}
	s.push(w, r, res)
}

// arrowSchema is the stream's schema: the row's fields, with atlas:profile
// and atlas:protocol (§9.2.7).
func arrowSchema(res *pipeline.Result, fields []arrow.Field) *arrow.Schema {
	md := arrow.NewMetadata([]string{arrowschema.MetaProfile, arrowschema.MetaProtocol}, []string{res.Writer.Exact().String(), protocol.Version})
	return arrow.NewSchema(fields, &md)
}

// arrowRows streams a table of rows as Arrow IPC: the schema, batches of
// arrowBatch rows, then the final batch of zero rows whose atlas:end holds
// the end line (§9.6.5). A row the batch cannot hold ends the stream with
// internal in its final batch (R129).
func (s *Server) arrowRows(w http.ResponseWriter, r *http.Request, res *pipeline.Result, t rowTable) {
	x := exchangeOf(r)
	row := rowSchema(res)
	fields, err := arrowschema.Fields(row)
	if err != nil {
		x.failed = protocol.NewProblem(protocol.CodeInternal, "an internal error occurred")
		s.problem(w, r, fmt.Errorf("edge: the table's Arrow schema: %w", err))
		return
	}
	sch := arrowSchema(res, fields)
	first, err := s.next(r, t)
	if p := s.endProblem(r, t, err); first == nil && p != nil && p.Code != protocol.CodeOutputLimitExceeded {
		s.problem(w, r, p)
		return
	}
	w.Header().Set(protocol.HeaderContentType, protocol.MediaArrowStream)
	mem := memory.NewGoAllocator()
	aw, err := frame.NewArrowWriter(w, sch, mem)
	if err != nil {
		s.opt.Log.Error("the Arrow stream could not start", "request_id", x.id, "err", err)
		x.failed = protocol.NewProblem(protocol.CodeInternal, "an internal error occurred")
		return
	}
	batch := make([]*ijson.Object, 0, arrowBatch)
	var fail *protocol.Problem
	send := func() bool {
		if len(batch) == 0 {
			return true
		}
		b, err := guarded(s, r, func() (arrow.RecordBatch, error) { return arrowschema.ToBatch(mem, sch, row, batch) })
		batch = batch[:0]
		if err != nil {
			s.opt.Log.Error("rows of the answer have no Arrow form", "request_id", x.id, "err", err)
			x.failed = protocol.NewProblem(protocol.CodeInternal, "an internal error occurred")
			fail = complete(x.failed, x.id)
			return true
		}
		werr := aw.Write(b)
		b.Release()
		if werr != nil {
			s.broke(r, res, werr)
			return false
		}
		return s.push(w, r, res)
	}
	for rec := first; rec != nil && fail == nil; {
		batch = append(batch, rec)
		if len(batch) == arrowBatch && !send() {
			return
		}
		rec, err = s.next(r, t)
	}
	if fail == nil && !send() {
		return
	}
	s.endArrow(w, r, res, aw, t, err, fail)
}

// arrowBatches streams a table of Arrow record batches as they are: the
// schema, every batch, then the final batch of zero rows whose atlas:end
// holds the end line (§9.6.5, R11).
func (s *Server) arrowBatches(w http.ResponseWriter, r *http.Request, res *pipeline.Result, t batchTable) {
	x := exchangeOf(r)
	sch := arrowSchema(res, t.Schema().Fields())
	nextBatch := func() (arrow.RecordBatch, error) { return guarded(s, r, t.Next) }
	first, err := nextBatch()
	if p := s.endProblem(r, t, err); first == nil && p != nil && p.Code != protocol.CodeOutputLimitExceeded {
		s.problem(w, r, p)
		return
	}
	w.Header().Set(protocol.HeaderContentType, protocol.MediaArrowStream)
	aw, err2 := frame.NewArrowWriter(w, sch, memory.NewGoAllocator())
	if err2 != nil {
		if first != nil {
			first.Release()
		}
		s.opt.Log.Error("the Arrow stream could not start", "request_id", x.id, "err", err2)
		x.failed = protocol.NewProblem(protocol.CodeInternal, "an internal error occurred")
		return
	}
	for b := first; b != nil; {
		werr := aw.Write(b)
		b.Release()
		if werr != nil {
			s.broke(r, res, werr)
			return
		}
		if !s.push(w, r, res) {
			return
		}
		b, err = nextBatch()
	}
	s.endArrow(w, r, res, aw, t, err, nil)
}

// endArrow writes the final batch: the end line in its atlas:end.
func (s *Server) endArrow(w http.ResponseWriter, r *http.Request, res *pipeline.Result, aw *frame.ArrowWriter, t rowsOrBatches, err error, fail *protocol.Problem) {
	if s.left(r) {
		return
	}
	end := frame.End{Truncated: t.Truncated(), Usage: usage(t), Problem: fail}
	if fail == nil {
		end.Problem = s.endProblem(r, t, err)
	}
	if end.Problem != nil {
		exchangeOf(r).code = end.Problem.Code
	}
	if werr := aw.End(end); werr != nil {
		s.broke(r, res, werr)
		return
	}
	s.push(w, r, res)
}

// The tables the edge reads.
var (
	_ rowTable        = (*pipeline.Rows)(nil)
	_ pipelineBatches = (*pipeline.Rows)(nil)
	_ batchTable      = batchesOf{}
)
