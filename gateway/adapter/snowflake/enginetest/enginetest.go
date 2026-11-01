// Package enginetest holds fakes of the Snowflake adapter's Engine seam for
// tests: Counting, which records every query an engine is given and faults
// it on demand, and Memory, an engine that answers from tables in memory.
// The Source above them is the adapter's real code; only the engine is
// fake (AGENTS.md: fakes stand in at the seam the real adapter uses).
package enginetest

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/jhgonline/fi-app-atlas/gateway/adapter/snowflake"
	"github.com/jhgonline/fi-app-atlas/gateway/fakes"
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/gateway/porttest"
)

// Counting is an engine that records every statement it is given (its
// text, its tag, the rows it answered, whether it was stopped) and can be
// made to fail or to hold, around the engine it wraps.
type Counting struct {
	inner snowflake.Engine
	mu    sync.Mutex
	runs  []*fakes.Run
	calls int
	fail  error
	hold  func()
}

var _ snowflake.Engine = (*Counting)(nil)

// Count wraps inner.
func Count(inner snowflake.Engine) *Counting { return &Counting{inner: inner} }

// Rig is source, over this engine, as porttest.DataSource drives it.
func (c *Counting) Rig(source ports.DataSource) *porttest.Rig {
	return &porttest.Rig{Source: source, Runs: c.Runs, Calls: c.Calls, Hold: c.Hold, Fail: c.Fail}
}

// Name is the inner engine's.
func (c *Counting) Name() string { return c.inner.Name() }

// Fail makes every statement fail with err; nil heals it.
func (c *Counting) Fail(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.fail = err
}

// Hold makes the engine hold each statement until its context ends,
// calling reached once it holds one; nil lets statements run.
func (c *Counting) Hold(reached func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.hold = reached
}

// Runs are the statements the engine was given, in order.
func (c *Counting) Runs() []fakes.Run {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]fakes.Run, len(c.runs))
	for i, r := range c.runs {
		out[i] = *r
	}
	return out
}

// Calls counts the statements that reached the engine.
func (c *Counting) Calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

func (c *Counting) end(run *fakes.Run, stopped bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !run.Done {
		run.Done, run.Stopped = true, stopped
	}
}

// Execute records st, then holds, fails, or runs it on the inner engine.
func (c *Counting) Execute(ctx context.Context, st *snowflake.Statement) (*snowflake.Answer, error) {
	c.mu.Lock()
	c.calls++
	run := &fakes.Run{Statement: st.SQL, Tag: st.Tag}
	c.runs = append(c.runs, run)
	fail, hold := c.fail, c.hold
	c.mu.Unlock()
	if hold != nil {
		hold()
		<-ctx.Done()
		c.end(run, true)
		return nil, ctx.Err()
	}
	if fail != nil {
		c.end(run, false)
		return nil, fail
	}
	a, err := c.inner.Execute(ctx, st)
	if err != nil {
		c.end(run, ctx.Err() != nil)
		return nil, err
	}
	c.mu.Lock()
	run.QueryID = a.QueryID
	c.mu.Unlock()
	return &snowflake.Answer{QueryID: a.QueryID, Batches: &counted{c: c, run: run, inner: a.Batches}}, nil
}

// Record is the inner engine's.
func (c *Counting) Record(ctx context.Context, queryID string) (*snowflake.Record, error) {
	return c.inner.Record(ctx, queryID)
}

type counted struct {
	c     *Counting
	run   *fakes.Run
	inner ports.BatchStream
}

func (s *counted) Schema() *arrow.Schema { return s.inner.Schema() }

func (s *counted) Next(ctx context.Context) (arrow.RecordBatch, error) {
	b, err := s.inner.Next(ctx)
	switch {
	case errors.Is(err, io.EOF):
		s.c.end(s.run, false)
	case err != nil:
		s.c.end(s.run, true)
	default:
		s.c.mu.Lock()
		s.run.Rows += b.NumRows()
		s.c.mu.Unlock()
	}
	return b, err
}

func (s *counted) Close() error {
	s.c.end(s.run, true) // a close before the last batch stops the query
	return s.inner.Close()
}

// Memory is an engine that answers a statement from tables in memory by
// evaluating the plan it was made from, as fakes.DataSource does, and
// answering in the statement's columns.
type Memory struct {
	ds *fakes.DataSource
}

var _ snowflake.Engine = (*Memory)(nil)

// NewMemory holds tables.
func NewMemory(tables ...fakes.Table) *Memory { return &Memory{ds: fakes.NewDataSource(tables...)} }

// Name is "memory".
func (m *Memory) Name() string { return "memory" }

// Execute evaluates st's plan and answers its rows as st's columns.
func (m *Memory) Execute(ctx context.Context, st *snowflake.Statement) (*snowflake.Answer, error) {
	if st.Plan == nil {
		return nil, errors.New("enginetest: the statement carries no plan to evaluate")
	}
	p, err := m.ds.Plan(ctx, st.Plan.Request)
	if err != nil {
		return nil, err
	}
	res, err := m.ds.Execute(ctx, p)
	if err != nil {
		return nil, err
	}
	fields := make([]arrow.Field, len(st.Columns))
	for i, o := range st.Columns {
		fields[i] = arrow.Field{Name: o.Name, Type: o.Type, Nullable: true}
	}
	return &snowflake.Answer{QueryID: res.QueryID, Batches: &flat{inner: res.Batches, schema: arrow.NewSchema(fields, nil)}}, nil
}

// Record is the evaluated query's record.
func (m *Memory) Record(ctx context.Context, queryID string) (*snowflake.Record, error) {
	u, err := m.ds.Settle(ctx, queryID)
	switch {
	case errors.Is(err, ports.ErrSourceUnavailable):
		return &snowflake.Record{QueryID: queryID, State: snowflake.Running}, nil
	case err != nil:
		return nil, err
	}
	return &snowflake.Record{QueryID: u.QueryID, State: snowflake.Succeeded, Rows: u.Rows, Bytes: u.Bytes}, nil
}

// flat is a writer schema's batches as a statement's columns: a struct
// field's members side by side.
type flat struct {
	inner  ports.BatchStream
	schema *arrow.Schema
}

func (f *flat) Schema() *arrow.Schema { return f.schema }

func (f *flat) Next(ctx context.Context) (arrow.RecordBatch, error) {
	b, err := f.inner.Next(ctx)
	if err != nil {
		return nil, err
	}
	defer b.Release()
	var cols []arrow.Array
	for _, c := range b.Columns() {
		if s, ok := c.(*array.Struct); ok {
			for j := range s.NumField() {
				cols = append(cols, s.Field(j))
			}
			continue
		}
		cols = append(cols, c)
	}
	if len(cols) != f.schema.NumFields() {
		return nil, fmt.Errorf("enginetest: %d columns for %d", len(cols), f.schema.NumFields())
	}
	return array.NewRecordBatch(f.schema, cols, b.NumRows()), nil
}

func (f *flat) Close() error { return f.inner.Close() }
