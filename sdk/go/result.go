package atlas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"

	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/arrowschema"
	"atlas.local/sdk/go/protocol/frame"
	"atlas.local/sdk/go/protocol/ijson"
	"atlas.local/sdk/go/protocol/profile"
	"atlas.local/sdk/go/protocol/resolve"
	"atlas.local/sdk/go/protocol/schema"
)

// Warning is what a call's answer says beside its result: a deprecation,
// a truncation.
type Warning struct {
	Code string `json:"code"`
	Text string `json:"text"`
}

// Result is one call's answer: a record, or a table's rows, read as they
// arrive, resolved to the reader's profile and held to its contract. A
// table is complete only when its end has been read and nothing was cut
// (ATLAS-CORE §10); its call's deadline bounds reading it, until it ends or
// is closed.
type Result struct {
	RequestID string
	// Exact is the profile the answer was written under (Atlas-Profile):
	// its provenance.
	Exact string
	// Profile is the profile the rows or record are delivered in: the
	// reader's, which the call validated its request under (§9.7.4 step
	// 10). It is nil for a built-in.
	Profile *profile.Profile
	// Writer is the profile named by Exact, fetched by its exact id.
	Writer      *profile.Profile
	Interaction *profile.Interaction
	Columns     []string
	Record      *ijson.Object
	Warnings    []Warning

	ctx    context.Context
	cancel context.CancelFunc
	hook   func(Warning)
	next   func() (*ijson.Object, error)
	close  func()
	schema *schema.Schema // a row is checked against it
	plan   *resolve.Plan  // a row written under another profile is resolved by it
	end    *frame.End
	rows   int64
	done   bool
	err    error
}

// Table reports whether the answer is a table's rows.
func (r *Result) Table() bool { return r.next != nil }

// Next is the next row of a table, io.EOF after the last. A table that
// ends without its end line is upstream_failed (AP-HTTP-002); the end
// line's problem is raised after the rows (STREAM-003); a truncation is a
// warning, and the rows sent are the result (STREAM-004).
func (r *Result) Next() (*ijson.Object, error) {
	if r.done {
		if r.err != nil {
			return nil, r.err
		}
		return nil, io.EOF
	}
	if r.next == nil {
		r.finish(nil)
		return nil, io.EOF
	}
	row, err := r.next()
	if err != nil {
		if e := ended(r.ctx); e != nil && !errors.Is(err, io.EOF) && r.end == nil {
			err = e // the deadline cut the body short
		}
		r.finish(err)
		if r.err != nil {
			return nil, r.err
		}
		return nil, io.EOF
	}
	if r.plan != nil {
		if row, err = r.plan.Record(row); err != nil {
			r.finish(protocol.ResolutionFailed(err))
			return nil, r.err
		}
	}
	if r.schema != nil {
		if fs := r.schema.Validate(row); len(fs) > 0 {
			r.finish(protocol.NewProblem(protocol.CodeInvalidDocument, fmt.Sprintf("row %d breaks the capability's contract at %s (%s)", r.rows+1, fs[0].Pointer, fs[0].Keyword)).WithTarget(fs[0].Pointer))
			return nil, r.err
		}
	}
	r.rows++
	return row, nil
}

func (r *Result) finish(err error) {
	if r.done {
		return
	}
	r.done = true
	if r.close != nil {
		r.close()
	}
	if r.cancel != nil {
		r.cancel()
	}
	if errors.Is(err, io.EOF) {
		err = nil
	}
	var p *protocol.Problem
	if errors.As(err, &p) && p.Code == protocol.CodeOutputLimitExceeded {
		r.warn(Warning{Code: p.Code, Text: p.Detail}) // a truncation is a warning (STREAM-004)
		err = nil
	} else if err == nil && r.end != nil && r.end.Truncated {
		r.warn(Warning{Code: protocol.CodeOutputLimitExceeded, Text: "the gateway cut the table at the caller's limit"})
	}
	r.err = err
}

func (r *Result) warn(w Warning) {
	r.Warnings = append(r.Warnings, w)
	if r.hook != nil {
		r.hook(w)
	}
}

// All reads every row. It holds the whole table: read a large one with
// Next.
func (r *Result) All() ([]*ijson.Object, error) {
	var out []*ijson.Object
	for {
		row, err := r.Next()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		out = append(out, row)
	}
}

// End is a table's end line, once its rows are read: nil for a table that
// came as one JSON array, which has none.
func (r *Result) End() *frame.End { return r.end }

// Rows is how many rows have been read.
func (r *Result) Rows() int64 { return r.rows }

// Complete reports whether the whole answer arrived: a record, or a table
// read to its end, without a failure, with nothing cut at the caller's
// limit. A truncated table is incomplete (ATLAS-CORE §10).
func (r *Result) Complete() bool {
	return r.done && r.err == nil && !r.Truncated()
}

// Truncated reports whether the gateway cut the table at the caller's
// limit (Q-DOC-15).
func (r *Result) Truncated() bool {
	if r.end != nil && r.end.Truncated {
		return true
	}
	for _, w := range r.Warnings {
		if w.Code == protocol.CodeOutputLimitExceeded {
			return true
		}
	}
	return false
}

// Usage is the end line's usage, once the rows are read: what the call
// cost, as the gateway measured it ({} when nothing was).
func (r *Result) Usage() json.RawMessage {
	if r.end == nil || len(r.end.Usage) == 0 {
		return nil
	}
	return r.end.Usage
}

// Close releases the answer, and its call's context.
func (r *Result) Close() {
	if !r.done {
		r.finish(protocol.NewProblem(protocol.CodeInternal, "the result was closed before its end"))
	}
}

// rowSchema is a table's row schema, projected to the columns asked for
// (§9.5.2 step 5).
func rowSchema(table *schema.Schema, columns []string) *schema.Schema {
	if table == nil || table.Items() == nil {
		return nil
	}
	row := table.Items()
	if columns != nil {
		row = row.Project(columns)
	}
	return row
}

// readerRow is the row schema rows are checked against: the reader's,
// projected.
func (r *Result) readerRow() *schema.Schema {
	if r.Profile == nil || r.Interaction == nil {
		return nil
	}
	return rowSchema(r.Profile.Schemas[r.Interaction.Returns], r.Columns)
}

// writerRow is the row schema an Arrow batch is written in: the writer's,
// whole, since a projected batch's fields are found by name.
func (r *Result) writerRow() *schema.Schema {
	if r.Writer == nil || r.Interaction == nil {
		return nil
	}
	in := r.Writer.Interactions[r.Interaction.Name]
	if in == nil {
		return nil
	}
	return rowSchema(r.Writer.Schemas[in.Returns], nil)
}

func (r *Result) ndjson(body io.ReadCloser) {
	rd := frame.NewNDJSONReader(body)
	r.schema = r.readerRow()
	r.close = func() { body.Close() }
	r.next = func() (*ijson.Object, error) {
		row, err := rd.Next()
		if err != nil {
			r.end = rd.End()
		}
		return row, err
	}
}

func (r *Result) arrow(body io.ReadCloser) error {
	mem := memory.NewGoAllocator()
	rd, err := frame.NewArrowReader(body, mem)
	if err != nil {
		body.Close()
		if e := ended(r.ctx); e != nil {
			return e
		}
		return err
	}
	decode := r.writerRow()
	if decode == nil {
		rd.Release()
		body.Close()
		return protocol.NewProblem(protocol.CodeInvalidDocument, "an Arrow answer to an interaction that returns no table")
	}
	r.schema = r.readerRow()
	var pending []*ijson.Object
	r.close = func() { rd.Release(); body.Close() }
	r.next = func() (*ijson.Object, error) {
		for len(pending) == 0 {
			b, err := rd.Next()
			if err != nil {
				r.end = rd.End()
				return nil, err
			}
			rows, err := arrowschema.ToRows(b, decode)
			b.Release()
			if err != nil {
				return nil, protocol.NewProblem(protocol.CodeInvalidDocument, "an Arrow batch does not hold the contract's rows: "+err.Error())
			}
			pending = rows
		}
		row := pending[0]
		pending = pending[1:]
		return row, nil
	}
	return nil
}

func (r *Result) memory(rows []*ijson.Object) {
	r.schema = r.readerRow()
	i := 0
	r.next = func() (*ijson.Object, error) {
		if i >= len(rows) {
			return nil, io.EOF
		}
		i++
		return rows[i-1], nil
	}
}

// deprecation reads Deprecation and Sunset (CORE-DEPR-001): a warning
// naming what is deprecated, its sunset and its replacement (§9.7.4 step
// 11), which p's properties name.
func (r *Result) deprecation(res *http.Response, now time.Time, p *profile.Profile) {
	f, ok := protocol.Field(res.Header, protocol.HeaderDeprecation)
	if !ok {
		return
	}
	since, err := protocol.ParseDeprecation(f)
	if err != nil {
		return
	}
	what := "this capability"
	if p != nil {
		what = p.ID + "@v" + strconv.Itoa(p.Version)
	}
	text := what + " is deprecated since " + since.Format(time.DateOnly)
	if s := res.Header.Get(protocol.HeaderSunset); s != "" {
		if t, err := protocol.ParseHTTPDate(s, now); err == nil {
			text += ", sunset " + t.Format(time.DateOnly)
		}
	}
	if p != nil {
		props, _ := p.Doc.Get("properties")
		if o, ok := props.(*ijson.Object); ok {
			if by, ok := o.Get("replaced_by"); ok {
				if s, ok := by.(string); ok && s != "" {
					text += "; use " + s
				}
			}
		}
	}
	r.warn(Warning{Code: "deprecated", Text: text})
}
