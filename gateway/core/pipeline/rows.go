package pipeline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"runtime/debug"
	"slices"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"

	"atlas.local/gateway/core/access"
	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/kit/clock"
	"atlas.local/sdk/go/kit/conc"
	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/arrowschema"
	"atlas.local/sdk/go/protocol/ijson"
	"atlas.local/sdk/go/protocol/schema"
)

// Rows is a table answer on its way to the caller (§9.3 rule 1, R05, R11).
// It reads its provider's rows, or its Arrow record batches; either way,
// every row is held to the result schema (projected to the columns asked
// for), keeps only what that schema declares (R106), and is inside the
// caller's scope (R05) before it is sent. The caller's output cap
// truncates it, exactly (R105); a grant revoked while it is open ends it
// (06-2 rule 3); and its end records the outcome. A Rows is read by one
// goroutine.
type Rows struct {
	ctx     context.Context
	src     ports.RowStream   // the answer as rows, or
	batches ports.BatchStream // as Arrow record batches
	arrow   *arrow.Schema     // the batches' columns, as the contract derives them
	schema  *schema.Schema    // what a row is checked against
	full    *schema.Schema    // the row's whole schema, for its scoped values
	pinned  string
	// scope is the caller's scope as it stands, and granted the scope the
	// provider was given: a row outside the first but inside the second is
	// access withdrawn, outside both a provider's fault.
	scope, granted ports.Scope
	max            int
	// recheck decides the caller's access again, under the current policy,
	// every so often: the scope it now gives, or the problem that ends the
	// answer.
	recheck func(context.Context) (ports.Scope, *protocol.Problem)
	every   time.Duration
	checked time.Time
	clock   clock.Clock
	fault   func(error) *protocol.Problem // a source's error, as its problem
	log     *slog.Logger
	end     func(ending)

	n         int64
	truncated bool
	over      bool // the source holds rows past the cap
	done      bool
	// ended is how the answer ended, recorded as the call's outcome only
	// once the caller has it or never will (Close, Fail, Gone): an answer
	// read whole and then not delivered is no success (R3-07).
	ended    ending
	recorded bool
	err      error
	pending  []*ijson.Object // held rows of the batch being read
	dropped  bool            // undeclared members were withheld (logged once)
}

// Next is the next row, or io.EOF after the last; any other error ends the
// answer, and is the problem its end line reports. An answer of Arrow
// batches is read a row at a time here too.
func (r *Rows) Next() (row *ijson.Object, err error) {
	defer r.recovering(&err)
	if r.done {
		return nil, r.final()
	}
	if prob := r.current(); prob != nil {
		return nil, r.stop(prob)
	}
	if r.capped() {
		return nil, r.cut()
	}
	if row, err = r.read(); err != nil {
		return nil, r.stopOn(err)
	}
	r.n++
	return row, nil
}

// Batched reports whether the answer is Arrow record batches, which an
// Arrow client is sent as they are, by NextBatch (R11).
func (r *Rows) Batched() bool { return r.batches != nil }

// Schema is the batches' schema: the contract's columns, as §9.2.7 derives
// them. It is nil when the answer is rows.
func (r *Rows) Schema() *arrow.Schema { return r.arrow }

// NextBatch is the next record batch of an answer that Batched reports,
// held as Next holds its rows and cut at the output cap, or io.EOF after
// the last. The caller releases each batch it is given. An answer is read
// by Next or by NextBatch, never by both.
func (r *Rows) NextBatch() (b arrow.RecordBatch, err error) {
	defer r.recovering(&err)
	if r.batches == nil {
		return nil, r.stop(protocol.NewProblem(protocol.CodeInternal, "the answer is rows, read by Next"))
	}
	if r.done {
		return nil, r.final()
	}
	if prob := r.current(); prob != nil {
		return nil, r.stop(prob)
	}
	if r.capped() {
		return nil, r.cut()
	}
	b, rows, err := r.take()
	if err != nil {
		return nil, r.stopOn(err)
	}
	r.n += int64(len(rows))
	return b, nil
}

// Count is how many rows were sent.
func (r *Rows) Count() int64 { return r.n }

// Truncated reports whether the caller's output cap cut the answer.
func (r *Rows) Truncated() bool { return r.truncated }

// Close ends the call once its answer is sent: its outcome is how the
// answer ended, or a cancellation when it was not read to its end.
func (r *Rows) Close() {
	if !r.done {
		r.finish(ending{step: ports.StepAnswer, code: ports.CodeCancelled})
	}
	r.record()
}

// Fail ends the answer with err, as an edge does that cannot send it: its
// outcome is err's problem, internal for an error that is none, even when
// every row had been read.
func (r *Rows) Fail(err error) {
	var prob *protocol.Problem
	if !errors.As(err, &prob) {
		r.log.Error("the answer could not be sent", "error", err)
		prob = protocol.NewProblem(protocol.CodeInternal, "an internal error occurred")
	}
	if !r.done {
		_ = r.stop(prob)
	} else if r.ended.code == "" {
		r.ended.step, r.ended.code = ports.StepAnswer, prob.Code
	}
	r.record()
}

// Gone ends the answer of a caller who went away: its outcome is a
// cancellation, even when every row had been read.
func (r *Rows) Gone() {
	if !r.done {
		r.finish(ending{step: ports.StepAnswer, code: ports.CodeCancelled})
	} else if r.ended.code == "" {
		r.ended.step, r.ended.code = ports.StepAnswer, ports.CodeCancelled
	}
	r.record()
}

// record writes the outcome, once.
func (r *Rows) record() {
	if !r.recorded {
		r.recorded = true
		r.end(r.ended)
	}
}

// final is what a read after the end answers.
func (r *Rows) final() error {
	if r.err != nil {
		return r.err
	}
	return io.EOF
}

// recovering ends the answer with internal when a panic, in its source or
// in a check, stops a read: the slot comes back and the outcome is written
// (R058, ATLAS-MECH-007).
func (r *Rows) recovering(err *error) {
	if v := recover(); v != nil {
		*err = r.stop(r.fault(&conc.PanicError{Value: v, Stack: debug.Stack()}))
	}
}

// capped reports whether the cap's rows have all been sent.
func (r *Rows) capped() bool { return r.max > 0 && r.n >= int64(r.max) }

// cut ends an answer at its cap: truncated only when the source holds a
// row past it (R105, R126), so an answer of exactly the cap is whole.
func (r *Rows) cut() error {
	more, err := r.more()
	if err != nil {
		return r.stopOn(err)
	}
	r.truncated = more
	return r.stop(nil)
}

// room is how many more rows the cap lets through, or -1 for no cap.
func (r *Rows) room() int64 {
	if r.max <= 0 {
		return -1
	}
	return int64(r.max) - r.n - int64(len(r.pending))
}

// read is the next held row, or io.EOF, or a problem.
func (r *Rows) read() (*ijson.Object, error) {
	if r.batches == nil {
		row, err := r.pull()
		if err != nil {
			return nil, err
		}
		return r.hold(row, true)
	}
	for len(r.pending) == 0 {
		b, rows, err := r.take()
		if err != nil {
			return nil, err
		}
		b.Release()
		r.pending = rows
	}
	row := r.pending[0]
	r.pending[0] = nil
	r.pending = r.pending[1:]
	return row, nil
}

// more reports whether the source holds a row past those sent.
func (r *Rows) more() (bool, error) {
	if r.over || len(r.pending) > 0 {
		return true, nil
	}
	if r.batches == nil {
		_, err := r.pull()
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		return err == nil, err
	}
	for {
		b, err := r.pullBatch()
		if errors.Is(err, io.EOF) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		n := b.NumRows()
		b.Release()
		if n > 0 {
			return true, nil
		}
	}
}

// pull is the source's next row.
func (r *Rows) pull() (*ijson.Object, error) {
	row, err := r.src.Next(r.ctx)
	switch {
	case errors.Is(err, io.EOF):
		return nil, io.EOF
	case err != nil:
		return nil, r.fault(err)
	case row == nil:
		return nil, r.fault(errors.New("pipeline: the source answered no row and no error"))
	}
	return row, nil
}

// pullBatch is the source's next batch.
func (r *Rows) pullBatch() (arrow.RecordBatch, error) {
	b, err := r.batches.Next(r.ctx)
	switch {
	case errors.Is(err, io.EOF):
		return nil, io.EOF
	case err != nil:
		return nil, r.fault(err)
	case b == nil:
		return nil, r.fault(errors.New("pipeline: the source answered no batch and no error"))
	}
	return b, nil
}

// take is the source's next batch that holds rows: its columns the
// contract's, cut to the room the cap leaves, and every row of it held as
// hold holds one. It gives the batch to send and its rows.
func (r *Rows) take() (arrow.RecordBatch, []*ijson.Object, error) {
	for {
		b, err := r.pullBatch()
		if err != nil {
			return nil, nil, err
		}
		if b.NumRows() == 0 {
			b.Release()
			continue
		}
		if prob := r.batchInScope(b); prob != nil {
			b.Release()
			return nil, nil, prob
		}
		out, prob := r.conform(b)
		b.Release()
		if prob != nil {
			return nil, nil, prob
		}
		if room := r.room(); room >= 0 && out.NumRows() > room {
			cut := out.NewSlice(0, room)
			out.Release()
			out, r.over = cut, true
		}
		rows, err := arrowschema.ToRows(out, r.schema)
		if err != nil {
			out.Release()
			r.log.Error("a batch of the capability's answer has no JSON form", "error", err)
			return nil, nil, protocol.NewProblem(protocol.CodeInternal, "a batch of the capability's answer does not match its contract")
		}
		for i, row := range rows {
			held, err := r.hold(row, false)
			if err != nil {
				out.Release()
				return nil, nil, err
			}
			rows[i] = held
		}
		return out, rows, nil
	}
}

// conform is a batch with the contract's columns, in its order, and no
// other: a column the contract does not declare is withheld (R106), and a
// column it declares that is missing or of another type is the provider's
// fault.
func (r *Rows) conform(b arrow.RecordBatch) (arrow.RecordBatch, *protocol.Problem) {
	in := b.Schema()
	if in.Equal(r.arrow) {
		b.Retain()
		return b, nil
	}
	cols := make([]arrow.Array, r.arrow.NumFields())
	for i, f := range r.arrow.Fields() {
		at := in.FieldIndices(f.Name)
		if len(at) != 1 || !arrow.TypeEqual(in.Field(at[0]).Type, f.Type) {
			r.log.Error("a batch of the capability's answer does not match its contract", "column", f.Name)
			return nil, protocol.NewProblem(protocol.CodeInternal, "a batch of the capability's answer does not match its contract")
		}
		cols[i] = b.Column(at[0])
	}
	if in.NumFields() > len(cols) {
		r.withheld()
	}
	return array.NewRecordBatch(r.arrow, cols, b.NumRows()), nil
}

// hold holds one row to the contract and the caller's scope: valid against
// its schema, rebuilt with only what the schema declares (R106), and, when
// scoped, every value it scopes inside the caller's scope (R05), its
// evidence whole (R3-01). A batch's rows are scoped on the batch as the
// provider gave it (batchInScope), before conform withholds the scoped
// columns the caller did not ask for. A row's values are never logged.
func (r *Rows) hold(row *ijson.Object, scoped bool) (*ijson.Object, error) {
	if r.schema != nil {
		if fs := r.schema.Validate(row); len(fs) > 0 {
			r.log.Error("a row of the capability's answer does not match its contract", "pointer", fs[0].Pointer, "keyword", fs[0].Keyword)
			return nil, protocol.NewProblem(protocol.CodeInternal, "a row of the capability's answer does not match its contract")
		}
	}
	// The scope is checked on the row as the provider gave it, the scoped
	// columns the caller did not ask for included (the pipeline asks for
	// them, withScoped), and only then is what was not asked for withheld.
	if scoped {
		vals, missing, ok := access.Evidence(r.full, row, true)
		if !ok {
			return nil, r.unscoped(missing)
		}
		if err := r.inScope(vals); err != nil {
			return nil, err
		}
	}
	if r.schema != nil {
		if kept, dropped := declared(r.schema, row); dropped {
			r.withheld()
			row = kept.(*ijson.Object)
		}
	}
	return row, nil
}

// inScope holds every value an answer scopes inside the caller's scope
// (R05): one outside it ends the answer, as not_granted when the caller's
// access was withdrawn while it was open, and as the provider's fault
// otherwise. A value is never logged, only its dimension and pointer.
func (r *Rows) inScope(vals []access.Value) *protocol.Problem {
	for _, v := range vals {
		if r.scope.Allows(v.Dimension, v.Value) {
			continue
		}
		if r.granted.Allows(v.Dimension, v.Value) {
			return protocol.NewProblem(protocol.CodeNotGranted, fmt.Sprintf("your access to the %s %q was withdrawn while the answer was open", v.Dimension, v.Value)).
				WithNext(accessNext("", r.pinned, "for the data you may read, and who to ask"))
		}
		r.log.Error("the capability returned a row outside the caller's scope, and the gateway withheld it", "dimension", v.Dimension, "pointer", v.Pointer)
		return protocol.NewProblem(protocol.CodeInternal, "the capability returned a row outside the caller's scope, and the gateway withheld it")
	}
	return nil
}

// batchInScope holds every value a batch scopes inside the caller's scope,
// on the batch as the provider gave it: before conform withholds the
// columns the caller did not ask for, which may be the scoped ones. Every
// scoped column must be there, and hold its evidence on every row (R3-01).
func (r *Rows) batchInScope(b arrow.RecordBatch) *protocol.Problem {
	if r.full == nil {
		return nil
	}
	sch := b.Schema()
	for _, name := range r.full.Properties() {
		p, _ := r.full.Property(name)
		if !access.Scoped(p) {
			continue
		}
		at := sch.FieldIndices(name)
		if len(at) != 1 {
			if !r.full.Required(name) {
				continue
			}
			return r.unscoped("/" + name)
		}
		col := b.Column(at[0])
		for i := 0; i < int(b.NumRows()); i++ {
			v, present, err := arrowschema.Value(col, i, p)
			if err != nil {
				r.log.Error("a batch of the capability's answer does not match its contract", "column", name)
				return protocol.NewProblem(protocol.CodeInternal, "a batch of the capability's answer does not match its contract")
			}
			if !present {
				v = nil
			}
			vals, missing, ok := access.Evidence(p, v, r.full.Required(name))
			if !ok {
				return r.unscoped("/" + name + missing)
			}
			for k := range vals {
				vals[k].Pointer = "/" + name + vals[k].Pointer
			}
			if prob := r.inScope(vals); prob != nil {
				return prob
			}
		}
	}
	return nil
}

// unscoped is the problem of an answer that lacks the evidence of its scope
// at a pointer (R3-01): the provider's fault, and nothing of it is sent.
func (r *Rows) unscoped(pointer string) *protocol.Problem {
	r.log.Error("the capability answered a row without the evidence of its scope, and the gateway withheld it", "pointer", pointer)
	return protocol.NewProblem(protocol.CodeInternal, "the capability answered a row without the evidence of its scope, and the gateway withheld it")
}

func (r *Rows) withheld() {
	if !r.dropped {
		r.dropped = true
		r.log.Warn("the capability answered members its contract does not declare; they were withheld")
	}
}

// current decides the caller's access again once every so often, under the
// policy as it now stands (06-2 rule 3; R13's active-connection bound).
func (r *Rows) current() *protocol.Problem {
	if r.recheck == nil {
		return nil
	}
	now := r.clock.Now()
	if now.Sub(r.checked) < r.every {
		return nil
	}
	r.checked = now
	scope, prob := r.recheck(r.ctx)
	if prob != nil {
		r.log.Info("the caller's access ended while the answer was open", "code", prob.Code)
		return prob
	}
	if scope != nil {
		r.scope = scope
	}
	return nil
}

// stopOn ends the answer on a read's error: io.EOF is its end.
func (r *Rows) stopOn(err error) error {
	if errors.Is(err, io.EOF) {
		return r.stop(nil)
	}
	var prob *protocol.Problem
	if !errors.As(err, &prob) {
		prob = r.fault(err)
	}
	return r.stop(prob)
}

// stop ends the answer, with prob or whole.
func (r *Rows) stop(prob *protocol.Problem) error {
	e := ending{step: ports.StepAnswer}
	if prob != nil {
		e.code = prob.Code
	}
	r.finish(e)
	if prob != nil {
		r.err = prob
		return prob
	}
	return io.EOF
}

// finish closes the source and ends the answer, once: a failure is
// recorded at once, and a success when Close, Fail or Gone says whether
// the caller received it.
func (r *Rows) finish(e ending) {
	if r.done {
		return
	}
	r.done = true
	r.pending = nil
	if err := r.closeSource(); err != nil {
		r.log.Warn("the capability's source did not close cleanly", "error", err)
	}
	e.rows, e.truncated = r.n, r.truncated
	r.ended = e
	if e.code != "" {
		r.record() // a failure is the outcome now; only a success awaits delivery
	}
}

// closeSource closes the stream the answer reads, a panic in it an error.
func (r *Rows) closeSource() (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = &conc.PanicError{Value: v, Stack: debug.Stack()}
		}
	}()
	if r.batches != nil {
		return r.batches.Close()
	}
	return r.src.Close()
}

// withScoped is the columns a provider is asked for: the caller's
// projection, and every property of the row that holds a scoped value
// (ports.DimensionOf) whether the caller asked for it or not, so the answer's
// scope can be checked before what the caller did not ask for is withheld
// (R05, R106). No projection asks for every column already.
func withScoped(columns []string, row *schema.Schema) []string {
	if columns == nil || row == nil {
		return columns
	}
	out := slices.Clone(columns)
	for _, name := range row.Properties() {
		if slices.Contains(out, name) {
			continue
		}
		if p, ok := row.Property(name); ok && access.Scoped(p) {
			out = append(out, name)
		}
	}
	return out
}
