package ports

import (
	"context"
	"errors"

	"github.com/apache/arrow-go/v18/arrow"
)

// DataSource is the one port through which services reach an engine (§6.4.1,
// D28): Snowflake in production, DuckDB running the Snowflake adapter's own
// statements in development, and memory in tests. A data service plans a
// read, explains or executes the plan, and settles what the engine recorded.
//
// A DataSource never sees SQL from a caller. The read is declarative (Query),
// and the source renders it as one statement in the writer contract, guards
// it, and binds every value as a parameter (§6.4, INTEGRATIONS "four
// important source-level details"). How it does that (the in-repo SQL parser, a driver,
// an engine-specific seam) is the adapter's own business; a second engine is
// a second implementation of this interface, proved by porttest.DataSource.
type DataSource interface {
	// Capabilities is what the engine supports, so the gateway never offers
	// what it cannot do (§6.4.1).
	Capabilities() SourceCapabilities
	// Plan renders a read as the writer contract: one statement, every
	// column aliased to its contract name and cast to its contract type,
	// filters and limits pushed down. The read-only guard checks the
	// statement before the plan is returned, and an object the deployment
	// did not register is refused before any engine is asked
	// (ErrUnknownObject). A plan is immutable and may be explained and
	// executed any number of times.
	Plan(ctx context.Context, r PlanRequest) (*Plan, error)
	// Explain is the plan as it would run, without running the read
	// (ATLAS-DATA-007). Its Statement is byte for byte the one Execute runs
	// (ATLAS-DATA-004). Estimates are heuristics, labelled as such, and nil
	// where the engine cannot give them (R19).
	Explain(ctx context.Context, p *Plan) (*Explanation, error)
	// Execute runs the plan. The context bounds it: its deadline and its
	// cancellation reach the engine, which stops the query (06-1, R17). The
	// answer is a stream of Arrow record batches already in the writer
	// schema (Plan.Writer), with the engine's id for the query. The tag is
	// sent out of band, never in the statement's text.
	Execute(ctx context.Context, p *Plan) (*Result, error)
	// Settle is what the engine itself recorded for a query it ran: rows,
	// bytes and cost where it knows them. It is idempotent (R09's
	// settlement).
	Settle(ctx context.Context, queryID string) (Usage, error)
}

// SourceCapabilities is what an engine supports (§6.4.1 capabilities()).
type SourceCapabilities struct {
	// Engine names the engine: "snowflake", "duckdb", "memory".
	Engine string
	// Language and Dialect are what plans are written in: "sql" and
	// "snowflake" (explain.schema.json's plan.language and plan.dialect).
	Language, Dialect string
	// Estimates reports whether Explain gives row or byte estimates.
	Estimates bool
	// Handoff, Export and Resume are deferred in the first build (R10,
	// REVIEW.md step 5): empty and false until a proof promotes them.
	Handoff []string
	Export  []string
	Resume  bool
}

// PlanRequest is one read to plan.
type PlanRequest struct {
	// Query is the read, as the capability's view model describes it.
	Query Query
	// Writer is the writer schema: the contract's result columns, in order,
	// as §9.2.7 derives them (arrowschema.Derive). Execute's batches have
	// exactly this schema.
	Writer *arrow.Schema
	// Columns says, for each field of Writer in order, which of Query's
	// columns it is made from: one column, or a Money's amount and currency.
	Columns [][]string
	// Capability is the pinned id the read serves, and RequestID the call:
	// both go into the tag the engine records (§6.4: query tags).
	Capability string
	RequestID  string
}

// Plan is a planned read: immutable, retained with its provenance
// (INTEGRATIONS: "the plan must be immutable and retained").
type Plan struct {
	// Statement is the exact statement Execute runs, in Dialect.
	Statement string
	// Dialect is the statement's dialect ("snowflake").
	Dialect string
	// Binds are the statement's parameters, in order: no request value is
	// ever written into Statement.
	Binds []any
	// Objects are the objects the statement reads, as the guard found them.
	Objects []string
	// Tag is what the engine records for the query, sent out of band.
	Tag string
	// Writer is the schema Execute's batches have.
	Writer *arrow.Schema
	// Request is what was planned, for the record.
	Request PlanRequest
}

// Explanation is a plan as it would run (explain.schema.json's plan and
// estimate).
type Explanation struct {
	Language, Dialect, Statement, Tag string
	// Rows and Bytes are estimates, nil where unknown.
	Rows, Bytes *int64
	// Cost is an estimate of cost by unit (an amount as a decimal string),
	// nil where unknown.
	Cost *Cost
	// Heuristic reports that the estimate is a heuristic rather than the
	// engine's own (R19), so the answer labels it.
	Heuristic bool
}

// Cost is an amount in a unit ("12.500" "credits").
type Cost struct {
	Amount, Unit string
}

// Result is an executing plan's answer.
type Result struct {
	// QueryID is the engine's own id for the query, for Settle.
	QueryID string
	// Batches are the answer, in Plan.Writer.
	Batches BatchStream
}

// Usage is what one piece of work used, from the source's own record.
type Usage struct {
	QueryID string
	Rows    int64
	Bytes   int64
	// Cost is by unit, each amount a decimal string ("credits": "0.012").
	Cost map[string]string
}

// BatchStream is a table's answer as Arrow record batches. Next returns
// io.EOF after the last batch. The caller releases every batch it is given.
// A failure after the first batch ends the answer with that failure. Close
// releases the source, stops the engine's work if it is still running, and
// is safe to call more than once.
type BatchStream interface {
	Schema() *arrow.Schema
	Next(ctx context.Context) (arrow.RecordBatch, error)
	Close() error
}

// ErrSourceUnavailable is an engine that cannot answer now: suspended,
// unreachable, out of capacity or shedding load. The caller may retry. It is
// the same failure ErrWarehouseUnavailable names, kept as one value so either
// name matches with errors.Is.
var ErrSourceUnavailable = ErrWarehouseUnavailable

// ErrPlanRefused is a read the source will not plan: the guard refused its
// statement, or its shape is one the engine cannot render. It is the
// deployment's defect, never the caller's, and is never retried.
var ErrPlanRefused = errors.New("ports: the source refused to plan the read")
