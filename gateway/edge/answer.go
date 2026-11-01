package edge

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/apache/arrow-go/v18/arrow"

	"github.com/jhgonline/fi-app-atlas/gateway/core/pipeline"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/schema"
)

// rowTable is a table answer read a row at a time, as pipeline.Rows hands
// it on: Next returns io.EOF after the last row, Count is how many rows it
// has handed on, and Truncated whether the caller's output cap cut it.
type rowTable interface {
	Next() (*ijson.Object, error)
	Count() int64
	Truncated() bool
}

// batchTable is a table answer as Arrow record batches in the writer schema
// (ports.Answer.Batches, as the pipeline holds them to the contract and the
// caller's scope). Next returns io.EOF after the last batch, and the caller
// releases every batch it is given. An Arrow client is sent the batches as
// they are; NDJSON and JSON read the same answer as rows, which the
// pipeline converts by §9.2.7's mapping (R11: one meaning, Arrow an
// optimisation of it).
type batchTable interface {
	Schema() *arrow.Schema
	Next() (arrow.RecordBatch, error)
	Count() int64
	Truncated() bool
}

// pipelineBatches is a table the pipeline holds as Arrow record batches
// (pipeline.Rows over ports.Answer.Batches): read as rows by Next, or as
// batches by NextBatch, never both.
type pipelineBatches interface {
	Batched() bool
	Schema() *arrow.Schema
	NextBatch() (arrow.RecordBatch, error)
	Count() int64
	Truncated() bool
}

// batchesOf reads a pipeline's batches as a batchTable.
type batchesOf struct{ pipelineBatches }

func (b batchesOf) Next() (arrow.RecordBatch, error) { return b.NextBatch() }

// tableOf is a result's table: its rows, and, when it arrived as Arrow
// record batches, those batches.
func tableOf(res *pipeline.Result) (rowTable, batchTable) {
	if res.Rows == nil {
		return nil, nil
	}
	if b, ok := any(res.Rows).(pipelineBatches); ok && b.Batched() {
		return res.Rows, batchesOf{b}
	}
	return res.Rows, nil
}

// rowSchema is the schema a table's rows are written by: its row schema,
// projected to the columns the call asked for.
func rowSchema(res *pipeline.Result) *schema.Schema {
	row := res.Writer.Schemas[res.Interaction.Returns]
	if row == nil {
		return nil
	}
	if it := row.Items(); it != nil {
		row = it
	}
	if res.Columns != nil {
		row = row.Project(res.Columns)
	}
	return row
}

// answer writes a result in the encoding chosen for it.
func (s *Server) answer(w http.ResponseWriter, r *http.Request, res *pipeline.Result, ch choice) {
	h := w.Header()
	if exact, err := protocol.FormatExact(res.Writer.Exact()); err == nil {
		h.Set(protocol.HeaderProfile, exact)
	}
	if rev, err := protocol.FormatGovernanceRevision(res.GovernanceRevision); err == nil {
		h.Set(protocol.HeaderGovernanceRevision, rev)
	}
	if len(res.LimitsRemaining) > 0 {
		var ls []protocol.Limit
		for _, k := range []string{"rate", "concurrency"} {
			if v, ok := res.LimitsRemaining[k]; ok {
				ls = append(ls, protocol.Limit{Name: k, Remaining: v})
			}
		}
		if f, err := protocol.FormatLimits(ls); err == nil && f != "" {
			h.Set(protocol.HeaderLimitRemaining, f)
		}
	}
	if res.Deprecated != nil {
		h.Set(protocol.HeaderDeprecation, protocol.FormatDeprecation(*res.Deprecated))
	}
	if res.Sunset != nil {
		h.Set(protocol.HeaderSunset, protocol.FormatSunset(*res.Sunset))
	}
	h.Set(protocol.HeaderCacheControl, protocol.NoStore)
	if res.Record != nil {
		body, err := ijson.Marshal(res.Record)
		if err != nil {
			exchangeOf(r).failed = protocol.NewProblem(protocol.CodeInternal, "an internal error occurred")
			s.problem(w, r, fmt.Errorf("edge: the record answer: %w", err))
			return
		}
		h.Set(protocol.HeaderContentType, protocol.MediaJSON)
		if _, err := w.Write(body); err != nil {
			s.broke(r, res, err)
		}
		return
	}
	rows, batches := tableOf(res)
	if rows == nil {
		exchangeOf(r).failed = protocol.NewProblem(protocol.CodeInternal, "an internal error occurred")
		s.problem(w, r, errors.New("edge: the result is neither a record nor a table"))
		return
	}
	switch ch.encoding {
	case protocol.EncodingNDJSON:
		s.ndjson(w, r, res, rows)
	case protocol.EncodingArrow:
		if batches != nil {
			s.arrowBatches(w, r, res, batches)
		} else {
			s.arrowRows(w, r, res, rows)
		}
	default:
		s.jsonArray(w, r, res, ch, rows)
	}
}

// endProblem is the problem a table's end reports: none on success, the
// failure that ended it, or output_limit_exceeded when the caller's cap cut
// it. A failure that is not a problem is logged, then reported as internal.
func (s *Server) endProblem(r *http.Request, t rowsOrBatches, err error) *protocol.Problem {
	x := exchangeOf(r)
	if err != nil && !errors.Is(err, io.EOF) {
		var p *protocol.Problem
		if !errors.As(err, &p) {
			s.opt.Log.Error("a table's answer failed", "request_id", x.id, "err", err)
			p = protocol.NewProblem(protocol.CodeInternal, "an internal error occurred")
		}
		return complete(p, x.id)
	}
	if t.Truncated() {
		return complete(protocol.NewProblem(protocol.CodeOutputLimitExceeded, fmt.Sprintf("the answer was cut at your output limit, %d rows", t.Count())), x.id)
	}
	return nil
}

// rowsOrBatches is what endProblem needs of either kind of table.
type rowsOrBatches interface {
	Count() int64
	Truncated() bool
}

func usage(t rowsOrBatches) []byte {
	return []byte(`{"rows":` + strconv.FormatInt(t.Count(), 10) + `}`)
}

// jsonArray writes a table as one JSON array. The rows are read to their
// end before anything is sent, so a failure is still a problem with its
// own status, and the array is held to MaxAnswer bytes. A JSON array has
// no end line, so it cannot say it was cut: a table cut at the caller's
// output cap, or larger than MaxAnswer, is never sent as a shortened 200
// array. It is refused, and the refusal says how to read it whole or
// marked (PR04, R007, R009, R011, A-150).
func (s *Server) jsonArray(w http.ResponseWriter, r *http.Request, res *pipeline.Result, ch choice, t rowTable) {
	x := exchangeOf(r)
	var buf bytes.Buffer
	buf.WriteByte('[')
	n := 0
	for {
		row, err := s.next(r, t)
		if err != nil {
			if p := s.endProblem(r, t, err); p != nil && p.Code != protocol.CodeOutputLimitExceeded {
				s.problem(w, r, p)
				return
			}
			break
		}
		b, err := ijson.Marshal(row)
		if err != nil {
			x.failed = protocol.NewProblem(protocol.CodeInternal, "an internal error occurred")
			s.problem(w, r, fmt.Errorf("edge: a row of the answer: %w", err))
			return
		}
		if buf.Len()+len(b)+2 > s.opt.MaxAnswer {
			x.failed = cut(ch, fmt.Sprintf("the answer is larger than %d bytes, the most this gateway sends as one JSON array", s.opt.MaxAnswer))
			s.problem(w, r, x.failed)
			return
		}
		if n > 0 {
			buf.WriteByte(',')
		}
		buf.Write(b)
		n++
	}
	if t.Truncated() {
		x.failed = cut(ch, fmt.Sprintf("the answer is longer than your output limit of %d rows", t.Count()))
		s.problem(w, r, x.failed)
		return
	}
	buf.WriteByte(']')
	w.Header().Set(protocol.HeaderContentType, protocol.MediaJSON)
	if _, err := w.Write(buf.Bytes()); err != nil {
		s.broke(r, res, err)
	}
}

// cut is the refusal of a table a JSON array cannot carry whole:
// invalid_request, with output_limit_exceeded behind it, and the way to
// read it: an NDJSON or Arrow answer, whose end says where it was cut, when
// the binding serves one the client reads; else a narrower request (A-150).
func cut(ch choice, why string) *protocol.Problem {
	p := protocol.NewProblem(protocol.CodeInvalidRequest, why+", and a JSON array cannot say that an answer was cut")
	p.WithDetails(*protocol.NewProblem(protocol.CodeOutputLimitExceeded, why))
	if len(ch.streams) > 0 {
		return p.WithNext("Ask for " + ch.streams[0].media + " (Accept), whose end says where the answer was cut, or narrow the request.")
	}
	return p.WithNext("Narrow the request, or ask the capability's owner for a higher output limit.")
}
