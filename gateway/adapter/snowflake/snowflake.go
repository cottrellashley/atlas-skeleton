// Package snowflake is the Snowflake data source (§6.4). It plans every
// capability's read as one statement with the contract written in, runs it
// through a Warehouse, streams the result cast to the writer schema, hands
// off its chunks, and settles from the query history.
//
// Warehouse is the seam to Snowflake itself. Proof 1 puts the real driver
// behind it; until then the fake in package snowfake stands in, writing
// Snowflake's encoding as package snowarrow assumes it.
package snowflake

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/snowarrow"
)

// Warehouse is what the adapter needs from Snowflake.
type Warehouse interface {
	// Execute runs a statement and returns its result's chunks.
	Execute(ctx context.Context, st Statement) (*QueryResult, error)
	// Result returns a persisted result with fresh links, without running
	// the statement again.
	Result(ctx context.Context, queryID string) (*QueryResult, error)
	// Estimate is EXPLAIN: the rows and bytes a statement would return.
	Estimate(ctx context.Context, st Statement) (rows, bytes int64, err error)
	// History is the query's own record, found by its id.
	History(ctx context.Context, queryID string) (QueryRecord, error)
}

// Statement is one planned statement. The real driver runs SQL; the fake
// evaluates Select. Tag is recorded as the query's QUERY_TAG.
type Statement struct {
	SQL    string
	Tag    string
	Select Select
}

// QueryResult is a result as Snowflake returns it to its drivers.
type QueryResult struct {
	QueryID string
	RowType snowarrow.RowType
	First   []byte
	Chunks  []protocol.Chunk
	Headers map[string]string
	Expires time.Time
	Rows    int64
}

// QueryRecord is Snowflake's query history row.
type QueryRecord struct {
	QueryID string
	Tag     string
	SQL     string
	Rows    int64
	Bytes   int64
	Started time.Time
	Ended   time.Time
}

// Source implements ports.DataSource over a Warehouse.
type Source struct {
	warehouse Warehouse
	client    *http.Client
	mem       memory.Allocator
	prefetch  int
}

// New returns the data source. client downloads chunks for the streamed path.
func New(w Warehouse, client *http.Client, mem memory.Allocator) *Source {
	return &Source{warehouse: w, client: client, mem: mem, prefetch: 4}
}

var _ ports.DataSource = (*Source)(nil)

func (s *Source) Capabilities() ports.SourceCapabilities {
	return ports.SourceCapabilities{Handoff: []string{snowarrow.Format}}
}

func (s *Source) Plan(_ context.Context, q ports.Query, writer *protocol.Table) (*ports.Plan, error) {
	sel, err := buildSelect(q, writer)
	if err != nil {
		return nil, err
	}
	return &ports.Plan{Statement: sel.Render(q.Tag), Query: q, Writer: writer}, nil
}

func (s *Source) statement(p *ports.Plan) (Statement, error) {
	sel, err := buildSelect(p.Query, p.Writer)
	if err != nil {
		return Statement{}, err
	}
	return Statement{SQL: p.Statement, Tag: p.Query.Tag, Select: sel}, nil
}

func (s *Source) Explain(ctx context.Context, p *ports.Plan) (ports.Explanation, error) {
	st, err := s.statement(p)
	if err != nil {
		return ports.Explanation{}, err
	}
	rows, bytes, err := s.warehouse.Estimate(ctx, st)
	return ports.Explanation{Statement: p.Statement, Rows: rows, Bytes: bytes}, err
}

func (s *Source) Execute(ctx context.Context, p *ports.Plan, writer string) (ports.Result, error) {
	st, err := s.statement(p)
	if err != nil {
		return ports.Result{}, err
	}
	schema, err := protocol.ArrowSchema(p.Writer, writer)
	if err != nil {
		return ports.Result{}, err
	}
	res, err := s.warehouse.Execute(ctx, st)
	if err != nil {
		return ports.Result{}, err
	}
	c := snowarrow.Chunks{RowType: res.RowType, First: res.First, Links: res.Chunks, Headers: res.Headers}
	return ports.Result{Reader: snowarrow.NewReader(ctx, s.mem, s.client, c, schema, s.prefetch), QueryID: res.QueryID}, nil
}

func (s *Source) Handoff(ctx context.Context, p *ports.Plan, writer string) (*protocol.Handoff, error) {
	st, err := s.statement(p)
	if err != nil {
		return nil, err
	}
	res, err := s.warehouse.Execute(ctx, st)
	if err != nil {
		return nil, err
	}
	return handoff(res, writer), nil
}

func (s *Source) Renew(ctx context.Context, queryID, writer string) (*protocol.Handoff, error) {
	res, err := s.warehouse.Result(ctx, queryID)
	if err != nil {
		return nil, err
	}
	return handoff(res, writer), nil
}

func (s *Source) Settle(ctx context.Context, queryID string) (ports.Usage, error) {
	rec, err := s.warehouse.History(ctx, queryID)
	if err != nil {
		return ports.Usage{}, fmt.Errorf("settle %s: %w", queryID, err)
	}
	return ports.Usage{QueryID: rec.QueryID, Tag: rec.Tag, Rows: rec.Rows, Bytes: rec.Bytes}, nil
}

func handoff(res *QueryResult, writer string) *protocol.Handoff {
	return &protocol.Handoff{
		Kind: "handoff", Format: snowarrow.Format, Profile: writer, Rows: res.Rows,
		Source: res.RowType.MarshalSource(), First: res.First, Chunks: res.Chunks,
		Headers: res.Headers, Expires: res.Expires, QueryID: res.QueryID,
	}
}
