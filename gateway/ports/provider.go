package ports

import (
	"context"
	"io"
	"time"

	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/ijson"
)

// Provider serves the interactions of capabilities: their business meaning
// and their source work (ATLAS-CORE §8). The pipeline calls it only once
// every common step has passed, with a request that is validated,
// normalised and inside the caller's scope. A provider never repeats a
// step the pipeline owns (constitution rule 9): it applies the scope it is
// handed, and its source's own controls stay as a further check.
//
// The boundary is in process and serialisable (R20): an Invocation holds
// only JSON values, and an Answer holds JSON values or Arrow record batches
// (whose IPC form is their serialisation), so a provider can move out of
// process without its capabilities changing.
type Provider interface {
	Serve(ctx context.Context, inv *Invocation) (*Answer, error)
}

// Invocation is one call of one interaction.
type Invocation struct {
	RequestID   string
	Capability  string // the pinned id: portfolio.holdings@v1
	Interaction string // fetch
	// Exact is the profile the request is written under: the capability's
	// current profile, to which an older request was resolved forward.
	Exact string
	// Request is the validated request, with every default filled in: its
	// effective values (ATLAS-CORE §9).
	Request *ijson.Object
	// Columns are the result columns asked for (Projectable), or nil.
	Columns []string
	// Caller is who is calling.
	Caller Caller
	// Scope is the data the caller may read, for each dimension the
	// capability's schemas use: AllValues, or the values allowed (none
	// means nothing).
	Scope Scope
	// Deadline is when the call's work must stop.
	Deadline time.Time
	// MaxRows is the caller's output cap for a table, or 0.
	MaxRows int
	// Features are what the calling client declared it implements.
	Features protocol.Features
}

// AllValues is a dimension of a Scope that is open whole.
const AllValues = "*"

// Allows reports whether a scope lets the caller read value in dimension.
func (s Scope) Allows(dimension, value string) bool {
	for _, v := range s[dimension] {
		if v == AllValues || v == value {
			return true
		}
	}
	return false
}

// Open reports whether a dimension is open whole.
func (s Scope) Open(dimension string) bool {
	for _, v := range s[dimension] {
		if v == AllValues {
			return true
		}
	}
	return false
}

// Caller is the effective identity of a call, and its roles under the
// policy it was decided by.
type Caller struct {
	Principal   string
	Name        string
	Kind        string
	Application string
	Roles       []string
}

// Answer is what an interaction returns: one record, or a table, as rows or
// as Arrow record batches. At most one of Record, Rows and Batches is set.
type Answer struct {
	Record *ijson.Object
	Rows   RowStream
	// Batches is a table's answer as Arrow record batches in the writer
	// schema of the answer's profile (§6.4.1, §9.2.7). The edge streams them
	// to an Arrow client as they are, and converts them to rows for NDJSON
	// and JSON: one meaning, with Arrow an optimisation of it (R11).
	Batches BatchStream
	// Settle, when set, reports what the source recorded for the work. The
	// pipeline calls it once, after the answer is delivered or abandoned,
	// and records the settlement fact (R09).
	Settle func(ctx context.Context) (Usage, error)
}

// RowStream is a table's rows, one at a time: Next returns io.EOF after the
// last row, and a failure after the first row ends the answer with that
// failure. Close releases the source and is safe to call more than once.
type RowStream interface {
	Next(ctx context.Context) (*ijson.Object, error)
	Close() error
}

// SliceRows is a RowStream of rows already in memory.
func SliceRows(rows []*ijson.Object) RowStream { return &sliceRows{rows: rows} }

type sliceRows struct {
	rows []*ijson.Object
	i    int
}

func (s *sliceRows) Next(ctx context.Context) (*ijson.Object, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.i >= len(s.rows) {
		return nil, io.EOF
	}
	s.i++
	return s.rows[s.i-1], nil
}

func (s *sliceRows) Close() error { return nil }
