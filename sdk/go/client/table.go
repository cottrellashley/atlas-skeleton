package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync/atomic"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/ipc"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/snowarrow"
)

// Table is a table answer, resolved to the reader's profile. Its batches are
// in the reader's Arrow schema. The caller releases it.
type Table struct {
	// Writer is the exact profile the gateway wrote under; Reader the one
	// the rows are resolved to.
	Writer, Reader string
	// Execution is how the rows travelled: gateway or handoff.
	Execution string
	array.RecordReader
}

// resolving checks every batch against the writer's profile, then resolves it
// to the reader's (§2.2, §9.5.7).
type resolving struct {
	refs         atomic.Int64
	mem          memory.Allocator
	inner        array.RecordReader
	chain        protocol.Chain
	writer       *protocol.Profile
	writerExact  string
	readerTable  *protocol.Table
	readerSchema *arrow.Schema
	cur          arrow.RecordBatch
	err          error
}

func newResolving(mem memory.Allocator, inner array.RecordReader, chain protocol.Chain, writer *protocol.Profile, writerExact string, readerTable *protocol.Table, readerSchema *arrow.Schema) *resolving {
	r := &resolving{mem: mem, inner: inner, chain: chain, writer: writer, writerExact: writerExact, readerTable: readerTable, readerSchema: readerSchema}
	r.refs.Store(1)
	return r
}

func (r *resolving) Retain() { r.refs.Add(1) }

func (r *resolving) Release() {
	if r.refs.Add(-1) == 0 {
		if r.cur != nil {
			r.cur.Release()
		}
		r.inner.Release()
	}
}

func (r *resolving) Schema() *arrow.Schema { return r.readerSchema }

func (r *resolving) Next() bool {
	if r.cur != nil {
		r.cur.Release()
		r.cur = nil
	}
	if r.err != nil || !r.inner.Next() {
		return false
	}
	rec := r.inner.RecordBatch()
	names := make([]string, rec.NumCols())
	for i := range names {
		names[i] = rec.ColumnName(i)
	}
	want, err := protocol.ArrowSchema(r.writer.Result.Project(names), r.writerExact)
	if err == nil {
		err = protocol.SameColumns(rec.Schema(), want)
	}
	if err != nil {
		// Never pass a caller wrong types (§2.2): the batch is not what
		// the writer's profile says.
		r.err = protocol.NewProblem(protocol.CodeInvalidDocument, "a batch does not match "+r.writerExact+": "+err.Error())
		return false
	}
	if r.cur, r.err = protocol.ResolveBatch(r.mem, r.chain, rec, r.readerTable, r.readerSchema); r.err != nil {
		return false
	}
	return true
}

func (r *resolving) RecordBatch() arrow.RecordBatch { return r.cur }

func (r *resolving) Record() arrow.RecordBatch { return r.cur }

func (r *resolving) Err() error {
	if r.err != nil {
		return r.err
	}
	return r.inner.Err()
}

// arrowAnswer reads an Arrow answer from the gateway: its batches, then the
// final zero-row batch carrying the end line (§9.6.5). An answer that ends
// without that batch, or whose end line counts other rows than arrived, is
// refused rather than passed on short.
type arrowAnswer struct {
	ipcr *ipc.Reader
	body io.ReadCloser
	rows int64
	err  error
	done bool
}

func (a *arrowAnswer) Retain()               {}
func (a *arrowAnswer) Schema() *arrow.Schema { return a.ipcr.Schema() }

func (a *arrowAnswer) Release() {
	a.ipcr.Release()
	a.body.Close()
}

func (a *arrowAnswer) Next() bool {
	if a.done {
		return false
	}
	if !a.ipcr.Next() {
		a.done = true
		if err := a.ipcr.Err(); err != nil {
			a.err = protocol.NewProblem(protocol.CodeUpstreamFailed, "the answer broke off: "+err.Error())
		} else {
			a.err = protocol.NewProblem(protocol.CodeUpstreamFailed, "the answer ended without its final batch")
		}
		return false
	}
	rec := a.ipcr.RecordBatch()
	end, final, err := protocol.EndOf(rec)
	if !final {
		a.rows += rec.NumRows()
		return true
	}
	a.done = true
	switch {
	case err != nil:
		a.err = protocol.NewProblem(protocol.CodeInvalidDocument, err.Error())
	case end.Problem != nil:
		a.err = end.Problem
	case end.Rows != a.rows:
		a.err = protocol.NewProblem(protocol.CodeUpstreamFailed, fmt.Sprintf("the end line counts %d rows; %d arrived", end.Rows, a.rows))
	case a.ipcr.Next():
		a.err = protocol.NewProblem(protocol.CodeInvalidDocument, "a batch follows the final batch")
	}
	return false
}

func (a *arrowAnswer) RecordBatch() arrow.RecordBatch { return a.ipcr.RecordBatch() }
func (a *arrowAnswer) Record() arrow.RecordBatch      { return a.ipcr.RecordBatch() }
func (a *arrowAnswer) Err() error                     { return a.err }

// handoffAnswer reads a handed-off result from its source's storage, asking
// the gateway for fresh links when they expire and resuming at the chunk that
// failed (§2.2.1.3, step 7).
type handoffAnswer struct {
	c       *Client
	ctx     context.Context
	binding protocol.Binding
	request map[string]any
	expect  string
	schema  *arrow.Schema
	h       *protocol.Handoff
	rd      *snowarrow.Reader
	base    int // chunk index rd starts at
	renewed int
	err     error
}

func (c *Client) handoff(ctx context.Context, b protocol.Binding, req map[string]any, expect string, writer *protocol.Profile, h *protocol.Handoff) (*handoffAnswer, error) {
	if h.Format != snowarrow.Format {
		return nil, protocol.NewProblem(protocol.CodeInvalidDocument, "this client does not decode "+h.Format)
	}
	rt, err := snowarrow.ParseSource(h.Source)
	if err != nil {
		return nil, protocol.NewProblem(protocol.CodeInvalidDocument, err.Error())
	}
	names := make([]string, len(rt))
	for i, col := range rt {
		names[i] = col.Name
	}
	schema, err := protocol.ArrowSchema(writer.Result.Project(names), h.Profile)
	if err != nil {
		return nil, err
	}
	a := &handoffAnswer{c: c, ctx: ctx, binding: b, request: req, expect: expect, schema: schema, h: h}
	a.open(h, 0)
	return a, nil
}

func (a *handoffAnswer) open(h *protocol.Handoff, from int) {
	rt, _ := snowarrow.ParseSource(h.Source)
	chunks := snowarrow.Chunks{RowType: rt, Links: h.Chunks, Headers: h.Headers}
	if from == 0 {
		chunks.First = h.First
	} else {
		chunks.Links = h.Chunks[from-1:]
	}
	a.h, a.base = h, from
	a.rd = snowarrow.NewReader(a.ctx, a.c.mem, a.c.http, chunks, a.schema, a.c.parallel)
}

func (a *handoffAnswer) Retain()               {}
func (a *handoffAnswer) Release()              { a.rd.Release() }
func (a *handoffAnswer) Schema() *arrow.Schema { return a.schema }

func (a *handoffAnswer) Next() bool {
	for {
		if a.rd.Next() {
			return true
		}
		err := a.rd.Err()
		if err == nil {
			return false
		}
		if !errors.Is(err, snowarrow.ErrLinkExpired) || a.renewed == 3 {
			a.err = protocol.NewProblem(protocol.CodeUpstreamFailed, "downloading the result: "+err.Error())
			return false
		}
		failed := a.base + a.rd.FailedChunk()
		fresh, rerr := a.renew()
		if rerr != nil {
			a.err = rerr
			return false
		}
		a.rd.Release()
		a.renewed++
		a.open(fresh, failed)
	}
}

// renew asks the gateway, which decides the call again, for fresh links to
// the same persisted result.
func (a *handoffAnswer) renew() (*protocol.Handoff, error) {
	resp, err := a.c.post(a.ctx, a.binding, a.request, map[string]string{
		protocol.HeaderProfileExpect: a.expect, protocol.HeaderRenew: a.h.QueryID,
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var h protocol.Handoff
	if err := json.NewDecoder(resp.Body).Decode(&h); err != nil {
		return nil, protocol.NewProblem(protocol.CodeInvalidDocument, "the renewed handoff: "+err.Error())
	}
	return &h, nil
}

func (a *handoffAnswer) RecordBatch() arrow.RecordBatch { return a.rd.RecordBatch() }
func (a *handoffAnswer) Record() arrow.RecordBatch      { return a.rd.RecordBatch() }

func (a *handoffAnswer) Err() error {
	if a.err != nil {
		return a.err
	}
	return a.rd.Err()
}
