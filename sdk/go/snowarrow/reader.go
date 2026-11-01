package snowarrow

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
)

// ErrLinkExpired means a chunk's link no longer works: ask for fresh links
// for the same result, which the source keeps without running it again.
var ErrLinkExpired = errors.New("a chunk link has expired or was refused")

// Chunks is everything needed to read one result: its row type, the first
// chunk inline, links to the rest and the headers every download sends.
type Chunks struct {
	RowType RowType
	First   []byte
	Links   []protocol.Chunk
	Headers map[string]string
}

// Reader reads one result's chunks in order, decoded into the writer schema,
// fetching at most a fixed number ahead so that memory stays flat whatever
// the result's size, and the reader's pace is the backpressure (§2.2.1.4).
// It implements array.RecordReader.
type Reader struct {
	refs    atomic.Int64
	ctx     context.Context
	cancel  context.CancelFunc
	target  *arrow.Schema
	results []chan chunkResult
	tokens  chan struct{}
	fetches sync.WaitGroup
	next    int
	pending []arrow.RecordBatch
	cur     arrow.RecordBatch
	err     error
	failed  int
}

type chunkResult struct {
	batches []arrow.RecordBatch
	err     error
}

// NewReader starts reading. parallel bounds the chunks fetched or held
// ahead of the reader. The caller releases the reader.
func NewReader(ctx context.Context, mem memory.Allocator, client *http.Client, c Chunks, target *arrow.Schema, parallel int) *Reader {
	ctx, cancel := context.WithCancel(ctx)
	n := len(c.Links)
	if len(c.First) > 0 {
		n++
	}
	r := &Reader{ctx: ctx, cancel: cancel, target: target, results: make([]chan chunkResult, n), tokens: make(chan struct{}, max(parallel, 1)), failed: -1}
	r.refs.Store(1)
	for i := range r.results {
		r.results[i] = make(chan chunkResult, 1)
	}
	for range cap(r.tokens) {
		r.tokens <- struct{}{}
	}
	load := func(i int) ([]byte, error) {
		if len(c.First) > 0 {
			if i == 0 {
				return c.First, nil
			}
			i--
		}
		return FetchChunk(ctx, client, c.Links[i], c.Headers)
	}
	r.fetches.Add(1)
	go func() {
		defer r.fetches.Done()
		for i := range r.results {
			select {
			case <-r.tokens:
			case <-ctx.Done():
				return
			}
			r.fetches.Add(1)
			go func() {
				defer r.fetches.Done()
				r.results[i] <- decodeChunk(ctx, mem, c.RowType, target, load, i)
			}()
		}
	}()
	return r
}

func decodeChunk(ctx context.Context, mem memory.Allocator, rt RowType, target *arrow.Schema, load func(int) ([]byte, error), i int) chunkResult {
	b, err := load(i)
	if err != nil {
		return chunkResult{err: err}
	}
	raw, err := ReadChunk(mem, bytes.NewReader(b))
	if err != nil {
		return chunkResult{err: fmt.Errorf("chunk %d: %w", i, err)}
	}
	var out []arrow.RecordBatch
	for _, rec := range raw {
		if err == nil && ctx.Err() == nil {
			var d arrow.RecordBatch
			if d, err = Decode(mem, rt, rec, target); err == nil {
				out = append(out, d)
			}
		}
		rec.Release()
	}
	if err == nil {
		err = ctx.Err()
	}
	if err != nil {
		for _, d := range out {
			d.Release()
		}
		return chunkResult{err: fmt.Errorf("chunk %d: %w", i, err)}
	}
	return chunkResult{batches: out}
}

// FetchChunk downloads one chunk with the headers the result requires.
func FetchChunk(ctx context.Context, client *http.Client, c protocol.Chunk, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized:
		return nil, ErrLinkExpired
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("result storage answered %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func (r *Reader) Retain() { r.refs.Add(1) }

func (r *Reader) Release() {
	if r.refs.Add(-1) != 0 {
		return
	}
	r.cancel()
	r.fetches.Wait()
	if r.cur != nil {
		r.cur.Release()
	}
	for _, p := range r.pending {
		p.Release()
	}
	for _, ch := range r.results {
		select {
		case res := <-ch:
			for _, b := range res.batches {
				b.Release()
			}
		default:
		}
	}
}

func (r *Reader) Schema() *arrow.Schema { return r.target }

func (r *Reader) Next() bool {
	if r.cur != nil {
		r.cur.Release()
		r.cur = nil
	}
	for len(r.pending) == 0 {
		if r.err != nil || r.next >= len(r.results) {
			return false
		}
		select {
		case res := <-r.results[r.next]:
			r.next++
			r.tokens <- struct{}{}
			if res.err != nil {
				r.err, r.failed = res.err, r.next-1
				return false
			}
			r.pending = res.batches
		case <-r.ctx.Done():
			r.err = r.ctx.Err()
			return false
		}
	}
	r.cur, r.pending = r.pending[0], r.pending[1:]
	return true
}

func (r *Reader) RecordBatch() arrow.RecordBatch { return r.cur }

// Record is RecordBatch under its older name.
func (r *Reader) Record() arrow.RecordBatch { return r.cur }

func (r *Reader) Err() error { return r.err }

// FailedChunk is the index of the chunk that failed, counting the inline
// first chunk as 0, or -1. Every earlier chunk was handed out whole, so a
// reader can resume from it with fresh links.
func (r *Reader) FailedChunk() int { return r.failed }
