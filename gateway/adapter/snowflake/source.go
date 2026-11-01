package snowflake

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
)

// Engine is what a Source runs its statements on: Snowflake through its
// driver (the nested module gosf), or DuckDB standing in for it
// (gateway/local/duckwh). It is the seam inside the adapter, below
// ports.DataSource (PLAN.md §4: DataSource, with an adapter-internal
// Warehouse seam). An Engine runs what the Source planned and guarded, and
// keeps the record Settle reads. It never plans, and never sees a statement
// the guard did not accept. It is safe for concurrent use.
type Engine interface {
	// Name is the engine's, for SourceCapabilities: "snowflake", "duckdb".
	Name() string
	// Execute runs one statement: st.SQL, in Snowflake's dialect, with
	// st.Args bound to its parameters in order. The engine records st.Tag
	// with the query, out of band (Snowflake's QUERY_TAG), never in the
	// text. The context bounds the query: when it ends, the engine stops
	// the query, and the answer ends with the context's error. The answer
	// is the engine's id for the query and its record batches, each with
	// exactly st.Columns: one Arrow array per column, in order, of the
	// column's type. Converting the engine's own encodings to those types
	// is the engine's work, and it is exact: a decimal is never a float on
	// the way.
	Execute(ctx context.Context, st *Statement) (*Answer, error)
	// Record is the engine's own record of a query it ran
	// (ports.ErrUnknownQuery when it keeps none).
	Record(ctx context.Context, queryID string) (*Record, error)
}

// Output is one column of a statement's answer: the name the statement
// gives it, and its type in the answer, a leaf of the writer schema
// (§9.2.7): Utf8, Int64, Float64, Decimal128(p,s), Boolean, Date32 or
// Timestamp(µs, UTC).
type Output struct {
	Name string
	Type arrow.DataType
	// WallUTC reports a timestamp the statement converts to UTC wall-clock
	// time itself (CONVERT_TIMEZONE(zone, 'UTC', ...), a TIMESTAMP_NTZ in
	// UTC), which the engine reads as UTC. Any other timestamp must arrive
	// as an instant (TIMESTAMP_TZ or TIMESTAMP_LTZ): a wall-clock time is
	// refused, never assumed to be UTC (§2.2.1.6, R053).
	WallUTC bool
}

// Answer is an engine's answer to a statement.
type Answer struct {
	QueryID string
	Batches ports.BatchStream
}

// State is where a query is, as its engine records it.
type State string

// The states of a query.
const (
	Running   State = "running"
	Succeeded State = "succeeded"
	Failed    State = "failed"
	Cancelled State = "cancelled" // the engine confirms the query stopped (R17)
)

// Record is an engine's own record of a query: what Settle reports.
type Record struct {
	QueryID string
	// Tag is the tag the query ran with, where the engine reports it.
	Tag   string
	State State
	// Rows are the rows the query produced, and Bytes the bytes it
	// scanned, or the size of its answer where the engine measures
	// nothing else.
	Rows, Bytes    int64
	Started, Ended time.Time
	// Cost is what the query cost, by unit, where the engine knows it.
	Cost map[string]string
}

// Options configure a Source, and the deprecated Warehouse.
type Options struct {
	// Dialect is the deprecated Warehouse's engine dialect: "snowflake"
	// (the default), or "duckdb" for the development gateway. A Source
	// always plans in Snowflake's dialect, and its Engine runs that.
	Dialect string
	// Registered are the objects the deployment lets capabilities read,
	// [database, schema] or [database, schema, object]: the guard's list.
	// Names are upper-cased, as Snowflake resolves unquoted ones.
	Registered [][]string
	// MaxCompiles bounds the compilations running at once (default 8).
	MaxCompiles int
	// MaxInFlight bounds the statements running on the engine at once
	// (default 8), and MaxQueue the calls waiting for one (default twice
	// MaxInFlight; negative for none). A call past both is refused at once
	// with ports.ErrSourceUnavailable (§6.7 bulkhead, R050).
	MaxInFlight, MaxQueue int
	// Unavailable, when set, reports an engine's failure the caller may
	// retry: a suspended warehouse, a lost session, a throttled request
	// (R052). The driver's own connection failures are recognised without
	// it.
	Unavailable func(error) bool
	// Allocator is the memory batches are built in (the default Go
	// allocator when nil).
	Allocator memory.Allocator
}

// Source is the Snowflake data source (§6.4): it plans every read as one
// Snowflake statement in the writer contract, guards it, runs it on its
// Engine and settles from the engine's own record. It implements
// ports.DataSource.
type Source struct {
	engine     Engine
	opt        Options
	registered [][]string
	compiles   chan struct{}
	inflight   *bulkhead
	mem        memory.Allocator
}

var _ ports.DataSource = (*Source)(nil)

// NewSource is a Source over engine. It refuses a registered entry that is
// not two or three identifiers, so a misconfigured deployment fails when it
// starts, not on every call (R136).
func NewSource(engine Engine, opt Options) (*Source, error) {
	if engine == nil {
		return nil, errors.New("snowflake: a source needs an engine")
	}
	registered, err := normalise(opt.Registered)
	if err != nil {
		return nil, err
	}
	opt = defaults(opt)
	mem := opt.Allocator
	if mem == nil {
		mem = memory.DefaultAllocator
	}
	return &Source{engine: engine, opt: opt, registered: registered, compiles: make(chan struct{}, opt.MaxCompiles),
		inflight: newBulkhead(opt.MaxInFlight, opt.MaxQueue), mem: mem}, nil
}

func defaults(opt Options) Options {
	if opt.MaxCompiles <= 0 {
		opt.MaxCompiles = 8
	}
	if opt.MaxInFlight <= 0 {
		opt.MaxInFlight = 8
	}
	switch {
	case opt.MaxQueue == 0:
		opt.MaxQueue = 2 * opt.MaxInFlight
	case opt.MaxQueue < 0:
		opt.MaxQueue = 0
	}
	return opt
}

// normalise is a registered list as the guard compares it: each entry two
// or three identifiers, upper-cased.
func normalise(registered [][]string) ([][]string, error) {
	out := make([][]string, 0, len(registered))
	for _, r := range registered {
		if len(r) != 2 && len(r) != 3 {
			return nil, fmt.Errorf("snowflake: the registered entry %v is not [database, schema] or [database, schema, object]", r)
		}
		entry := make([]string, len(r))
		for i, name := range r {
			c, err := column(name)
			if err != nil {
				return nil, fmt.Errorf("snowflake: the registered entry %v: %w", r, err)
			}
			entry[i] = c
		}
		out = append(out, entry)
	}
	return out, nil
}

// Serves reports whether the deployment registered object, so a gateway can
// refuse at start a capability whose object it may not read (R074).
func (s *Source) Serves(obj string) error { return serves(s.registered, obj) }

func serves(registered [][]string, obj string) error {
	parts, err := object(obj)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(registered, func(r []string) bool { return slices.Equal(r, parts) || slices.Equal(r, parts[:2]) }) {
		return fmt.Errorf("%w: %s", ports.ErrUnknownObject, obj)
	}
	return nil
}

// Capabilities are a Snowflake source's: SQL in Snowflake's dialect, no
// estimates, and none of the deferred executions (R10).
func (s *Source) Capabilities() ports.SourceCapabilities {
	return ports.SourceCapabilities{Engine: s.engine.Name(), Language: "sql", Dialect: "snowflake"}
}

// Plan renders r as one Snowflake statement in the writer contract (§6.4.1):
// each writer field is selected under its contract name and cast to its
// contract type, a decimal at its scale (R100), a timestamp in UTC; every
// value is a parameter; the statement is generated by sqlglot and guarded
// as it will run. Nothing reaches the engine.
func (s *Source) Plan(ctx context.Context, r ports.PlanRequest) (*ports.Plan, error) {
	if err := s.compile(ctx); err != nil {
		return nil, err
	}
	defer func() { <-s.compiles }()
	tag, err := tagOf(r)
	if err != nil {
		return nil, err
	}
	outs, items, err := outputs(r)
	if err != nil {
		return nil, err
	}
	obj, err := object(r.Query.Object)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ports.ErrPlanRefused, err)
	}
	var b builder
	b.add("SELECT ")
	for i, it := range items {
		if i > 0 {
			b.add(", ")
		}
		b.add(`%s AS "%s"`, it, outs[i].Name)
	}
	if err := b.from(r.Query, obj); err != nil {
		return nil, refused(err)
	}
	out, objects, err := finish(b.sql.String(), s.registered, obj)
	if err != nil {
		return nil, refused(err)
	}
	names := make([]string, len(objects))
	for i, o := range objects {
		names[i] = strings.Join(o, ".")
	}
	return &ports.Plan{Statement: out[0], Dialect: "snowflake", Binds: b.args, Objects: names, Tag: tag, Writer: r.Writer, Request: r}, nil
}

// refused marks a planning failure as the deployment's (ErrPlanRefused),
// unless it is one the port names on its own: an unregistered object, or a
// read too large to bind.
func refused(err error) error {
	if errors.Is(err, ports.ErrUnknownObject) || errors.Is(err, ports.ErrQueryTooLarge) || errors.Is(err, ports.ErrPlanRefused) {
		return err
	}
	return fmt.Errorf("%w: %w", ports.ErrPlanRefused, err)
}

func (s *Source) compile(ctx context.Context) error {
	select {
	case s.compiles <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// tags hold names and ids only: never a token, a key or a request's data
// (MECH-014).
var tagChars = regexp.MustCompile(`^[A-Za-z0-9._@:/-]{1,256}$`)

// tagOf is what the engine records for a plan's query: atlas, the
// capability and the call (§6.4 query tags, R051). It is never written in
// the statement, so explain's statement is fetch's, byte for byte.
func tagOf(r ports.PlanRequest) (string, error) {
	capability := r.Capability
	if capability == "" {
		capability = r.Query.Tag
	}
	tag := "atlas"
	for _, part := range []string{capability, r.RequestID} {
		if part != "" {
			tag += ":" + part
		}
	}
	if !tagChars.MatchString(tag) {
		return "", fmt.Errorf("%w: the tag %q holds more than names and ids", ports.ErrPlanRefused, tag)
	}
	return tag, nil
}

// Explain is the plan as it would run, without running anything: its
// statement, byte for byte, and its tag. The source gives no estimates.
func (s *Source) Explain(ctx context.Context, p *ports.Plan) (*ports.Explanation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if p == nil || p.Statement == "" {
		return nil, fmt.Errorf("%w: no plan to explain", ports.ErrPlanRefused)
	}
	return &ports.Explanation{Language: "sql", Dialect: p.Dialect, Statement: p.Statement, Tag: p.Tag}, nil
}

// Execute runs a plan on the engine, its statement exactly as planned and
// guarded again, so no statement runs that the guard did not see. It holds
// one of the bulkhead's places until the answer is closed.
func (s *Source) Execute(ctx context.Context, p *ports.Plan) (*ports.Result, error) {
	st, err := s.statement(ctx, p)
	if err != nil {
		return nil, err
	}
	if err := s.inflight.acquire(ctx); err != nil {
		return nil, err
	}
	a, err := s.engine.Execute(ctx, st)
	if err != nil {
		s.inflight.release()
		return nil, s.classify(ctx, err)
	}
	if a == nil || a.Batches == nil || a.QueryID == "" {
		if a != nil && a.Batches != nil {
			_ = a.Batches.Close()
		}
		s.inflight.release()
		return nil, errors.New("snowflake: the engine answered without a query id or batches")
	}
	return &ports.Result{QueryID: a.QueryID, Batches: &stream{source: s, writer: p.Writer, outputs: st.Columns, engine: a.Batches}}, nil
}

// statement is what the engine runs for a plan: checked as a Source's own
// plan, its statement guarded as it stands.
func (s *Source) statement(ctx context.Context, p *ports.Plan) (*Statement, error) {
	if p == nil || p.Writer == nil || p.Dialect != "snowflake" {
		return nil, fmt.Errorf("%w: not a Snowflake plan", ports.ErrPlanRefused)
	}
	if len(p.Statement) > MaxStatement || len(p.Binds) > MaxBinds {
		return nil, fmt.Errorf("%w: the plan is larger than a statement may be", ports.ErrQueryTooLarge)
	}
	if !tagChars.MatchString(p.Tag) {
		return nil, fmt.Errorf("%w: the plan's tag %q", ports.ErrPlanRefused, p.Tag)
	}
	outs, _, err := outputs(ports.PlanRequest{Query: p.Request.Query, Writer: p.Writer, Columns: p.Request.Columns})
	if err != nil {
		return nil, err
	}
	args := slices.Clone(p.Binds)
	for _, a := range args {
		switch a.(type) {
		case string, int64, float64, bool:
		default:
			return nil, fmt.Errorf("%w: a %T is not a value a statement binds", ports.ErrPlanRefused, a)
		}
	}
	if err := s.compile(ctx); err != nil {
		return nil, err
	}
	v := Guard(p.Statement, s.registered)
	<-s.compiles
	if !v.Accepted {
		return nil, fmt.Errorf("%w: %w", ports.ErrPlanRefused, refuse(v))
	}
	return &Statement{SQL: p.Statement, Args: args, Objects: v.Objects, Tag: p.Tag, Columns: outs, Plan: p}, nil
}

// queryID is an engine's id for a query: Snowflake's are UUIDs.
var queryID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// Settle is what the engine recorded for a query it ran: its rows, the
// bytes it scanned and its cost where the engine knows it (R09). A query
// that has not ended is not settled yet, and may be settled again later.
func (s *Source) Settle(ctx context.Context, id string) (ports.Usage, error) {
	if !queryID.MatchString(id) {
		return ports.Usage{}, fmt.Errorf("%w: %q is not a query id", ports.ErrUnknownQuery, id)
	}
	if err := s.inflight.acquire(ctx); err != nil {
		return ports.Usage{}, err
	}
	defer s.inflight.release()
	rec, err := s.engine.Record(ctx, id)
	if err != nil {
		return ports.Usage{}, s.classify(ctx, err)
	}
	if rec.State == Running {
		return ports.Usage{}, fmt.Errorf("%w: the query %s has not ended", ports.ErrSourceUnavailable, id)
	}
	return ports.Usage{QueryID: rec.QueryID, Rows: rec.Rows, Bytes: rec.Bytes, Cost: maps.Clone(rec.Cost)}, nil
}

func (s *Source) classify(ctx context.Context, err error) error {
	return classify(ctx, err, s.opt.Unavailable)
}
