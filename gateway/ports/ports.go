// Package ports declares every interface between the gateway's core, its
// services and its adapters (§6.5, layer L1). Everything that crosses a port
// is serialisable data, so any service or adapter can later move to a
// process of its own without its callers changing (§6.1.7).
package ports

import (
	"context"
	"time"

	"github.com/apache/arrow-go/v18/arrow/array"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
)

// Call is the front door's statement of one decided call. A service acts on
// a call and nothing else; it never sees the caller's token (§6.1.6).
type Call struct {
	Address   string    `json:"address"`
	Subject   string    `json:"subject"`
	RequestID string    `json:"request_id"`
	Deadline  time.Time `json:"deadline"`
	// Writer is the exact profile the answer must be written under.
	Writer string `json:"writer"`
	// Execution is how the front door chose the result travels.
	Execution string `json:"execution"`
	// Renew names a handed-off result whose links to re-issue.
	Renew string `json:"renew,omitempty"`
	// Signature is the front door's Ed25519 signature over the members above.
	Signature []byte `json:"signature"`
}

// Request is a validated request record, already resolved to the writer's
// request schema.
type Request map[string]any

// Answer is what a service returns: exactly one of Table, Record or Handoff.
type Answer struct {
	// Table streams a table result in the writer's Arrow schema. The front
	// door releases it.
	Table array.RecordReader
	// Record is a single answer, encoded as JSON.
	Record any
	// Handoff is a result the client fetches directly from its source.
	Handoff *protocol.Handoff
	// QueryID names the source's own record of the work, which the front
	// door settles from after the answer is sent.
	QueryID string
}

// Service does one upstream's or domain's work behind the front door
// (§6.1.6).
type Service interface {
	// Domains are the domains whose capabilities the service may serve; the
	// catalogue refuses any profile outside them (ATLAS-GATEWAY-007).
	Domains() []string
	// Profiles declares every profile the service serves, in each
	// capability's evolution order; the last of each is current.
	Profiles() []*protocol.Profile
	// Serve does the work of one decided call.
	Serve(ctx context.Context, call Call, req Request) (Answer, error)
	// Settle reports what a call actually used, from the source's record.
	Settle(ctx context.Context, queryID string) (Usage, error)
}

// DataSource is the one port in front of every engine (§6.4.1). Nothing about
// the engine leaves its adapter but its own handoff format.
type DataSource interface {
	// Capabilities says what the engine supports.
	Capabilities() SourceCapabilities
	// Plan renders a query as the writer contract: one statement with every
	// column aliased to its contract name and cast to its contract type.
	Plan(ctx context.Context, q Query, writer *protocol.Table) (*Plan, error)
	// Explain returns the statement as it would run and its estimated size.
	Explain(ctx context.Context, p *Plan) (Explanation, error)
	// Execute streams the result as Arrow batches already in the writer schema.
	Execute(ctx context.Context, p *Plan, writer string) (Result, error)
	// Handoff runs the plan and returns the result's chunk links.
	Handoff(ctx context.Context, p *Plan, writer string) (*protocol.Handoff, error)
	// Renew re-issues a persisted result's links without running it again.
	Renew(ctx context.Context, queryID, writer string) (*protocol.Handoff, error)
	// Settle reads rows, bytes and cost from the engine's own record.
	Settle(ctx context.Context, queryID string) (Usage, error)
}

// SourceCapabilities is what an engine supports.
type SourceCapabilities struct {
	// Handoff lists the chunk formats the engine can hand off.
	Handoff []string
}

// Query is a capability's read, in engine-neutral terms: the source object,
// each writer column's source expression, filters and limits. The adapter
// turns it into one statement in its own dialect.
type Query struct {
	Source  string         `json:"source"`
	Columns []SourceColumn `json:"columns"`
	Filters []Filter       `json:"filters,omitempty"`
	OrderBy []string       `json:"order_by,omitempty"`
	Limit   int            `json:"limit,omitempty"`
	// Tag is recorded by the source with the query, for settlement.
	Tag string `json:"tag"`
}

// SourceColumn maps one writer column to its source: a column, optionally
// divided by a factor (a unit change written into the plan).
type SourceColumn struct {
	Writer string `json:"writer"`
	Source string `json:"source"`
	Divide int64  `json:"divide,omitempty"`
}

// Filter operations.
const (
	OpEq      = "eq"
	OpIn      = "in"
	OpBetween = "between"
	// OpLatest keeps the rows whose source column holds its greatest value.
	OpLatest = "latest"
)

// Filter is one pushed-down predicate on a source column.
type Filter struct {
	Source string `json:"source"`
	Op     string `json:"op"`
	Values []any  `json:"values,omitempty"`
}

// Plan is a rendered query. Statement is exactly what explain shows and
// what runs.
type Plan struct {
	Statement string          `json:"statement"`
	Query     Query           `json:"query"`
	Writer    *protocol.Table `json:"writer"`
}

// Explanation is what a fetch would do, without doing it (§2.2).
type Explanation struct {
	Statement string `json:"statement"`
	Rows      int64  `json:"estimated_rows"`
	Bytes     int64  `json:"estimated_bytes"`
}

// Result is a streamed answer and the engine's id for the work.
type Result struct {
	Reader  array.RecordReader
	QueryID string
}

// Usage is what a call used, from the source's own record. Tag is the tag
// the query was recorded under.
type Usage struct {
	QueryID string `json:"query_id"`
	Tag     string `json:"tag"`
	Rows    int64  `json:"rows"`
	Bytes   int64  `json:"bytes"`
}

// Limits reserves before work and settles after, exactly across every copy of
// the front door (§6.2.6).
type Limits interface {
	Reserve(ctx context.Context, key string, limit int, per time.Duration) (Reservation, error)
	Settle(ctx context.Context, r Reservation) error
}

// Reservation is one reserved unit of a limit.
type Reservation struct {
	Key       string
	Remaining int
}

// AuditSink keeps one record per call, refused or not, durably before the
// call's answer ends (§6.2.8).
type AuditSink interface {
	Write(ctx context.Context, rec AuditRecord) error
}

// AuditRecord is the call's attestation: what ran, for whom, under which
// contract and which rules.
type AuditRecord struct {
	Time        time.Time `json:"time"`
	RequestID   string    `json:"request_id"`
	Caller      string    `json:"caller"`
	Address     string    `json:"address"`
	Expected    string    `json:"expected_profile,omitempty"`
	Writer      string    `json:"writer_profile,omitempty"`
	Governance  string    `json:"governance_revision"`
	Decision    string    `json:"decision"` // allowed, or the code that refused
	RefusedAt   string    `json:"refused_at,omitempty"`
	Execution   string    `json:"execution,omitempty"`
	QueryID     string    `json:"query_id,omitempty"`
	Rows        int64     `json:"rows"`
	DurationMS  float64   `json:"duration_ms"`
	RequestHash string    `json:"request_hash,omitempty"`
	Outcome     string    `json:"outcome"`
}
