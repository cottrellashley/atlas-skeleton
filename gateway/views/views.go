// Package views serves data capabilities from view models: a capability's
// profile says what it answers, and its view model says where the answer
// is (an object, its columns, the filters a request maps to, the data
// scope's columns), so a curated warehouse capability is added with two
// files and no code (§2.2.1, ATLAS-CORE §4: the owner's meaning, not an
// inferred view name).
//
// The provider reaches its engine only through ports.DataSource (§6.4.1,
// rule 1). For each call it builds one read: the declarative query, the
// writer schema derived from the result schema (§9.2.7), and which columns
// make each field. fetch plans and executes it and answers the engine's
// Arrow batches, held to the contract, with a settlement from the engine's
// own record (R09); explain plans the same read and answers the plan
// without running it (ATLAS-DATA-004, ATLAS-DATA-007, INTEGRATIONS: explain
// and execution share plan construction). The scope the pipeline gives it
// is a condition of every read (R05).
package views

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"slices"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"

	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/kit/clock"
	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/grammar"
	"atlas.local/sdk/go/protocol/ijson"
)

// Explain is the interaction that plans a read without running it.
const Explain = "explain"

// MaxValues is the most values one request member may hand a read: more
// is refused as the caller's invalid request, at the member (R110), never
// sent to the engine to fail there.
const MaxValues = 1000

// verifyFor bounds the start-time planning of every read (Build).
const verifyFor = 30 * time.Second

// Provider serves every interaction it holds a view model for.
type Provider struct {
	src   ports.DataSource
	clock clock.Clock
	rows  bool
	mem   memory.Allocator
	views map[string]*view
}

// Options configure a Provider.
type Options struct {
	// Clock is the time a status is judged at (Freshness); the system's
	// when nil.
	Clock clock.Clock
	// Rows answers tables as rows rather than Arrow batches: the form the
	// pipeline reads until it reads batches. It is the same answer, held to
	// the same contract.
	Rows bool
}

// New is a provider reading through src, answering Arrow batches
// in each answer's writer schema with the engine's settlement.
func New(src ports.DataSource, bindings ...Binding) (*Provider, error) {
	return Build(src, Options{}, bindings...)
}

// Build checks every view model against its profile and plans every read
// once, so a model that cannot answer its contract, or names an object the
// source does not serve, stops the gateway at start, never on a call
// (R074, R099).
func Build(src ports.DataSource, opt Options, bindings ...Binding) (*Provider, error) {
	if src == nil {
		return nil, errors.New("views: a provider reads through a data source")
	}
	if opt.Clock == nil {
		opt.Clock = clock.System
	}
	p := &Provider{src: src, clock: opt.Clock, rows: opt.Rows, mem: memory.DefaultAllocator, views: map[string]*view{}}
	var errs []error
	var checked []*view
	for _, b := range bindings {
		v, err := check(b)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, in := range v.interactions {
			key := v.pinned + ":" + in
			if p.views[key] != nil {
				errs = append(errs, fmt.Errorf("views: two view models serve %s", key))
				continue
			}
			p.views[key] = v
		}
		checked = append(checked, v)
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), verifyFor)
	defer cancel()
	for _, v := range checked {
		if err := p.verify(ctx, v); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return p, nil
}

// verify plans a view's reads with no request values, which proves each
// object is one the source serves without running anything.
func (p *Provider) verify(ctx context.Context, v *view) error {
	inv := v.probe()
	rd, err := v.read(inv)
	if err != nil {
		return fmt.Errorf("views: %s: %w", v.pinned, err)
	}
	reqs := []ports.PlanRequest{rd.req}
	if e := v.model.Exists; e != nil {
		reqs = append(reqs, v.lookup(inv, e.Object, e.Column, nil, nil))
	}
	if vd := v.model.Valued; vd != nil {
		reqs = append(reqs, v.lookup(inv, vd.Object, vd.Column, nil, &ports.Latest{Date: vd.Date, Per: []string{vd.Column}}))
	}
	for _, r := range reqs {
		if _, err := p.src.Plan(ctx, r); err != nil {
			return fmt.Errorf("views: %s: the source cannot plan a read of %s: %w", v.pinned, r.Query.Object, err)
		}
	}
	return nil
}

// Serve answers an invocation from its view.
func (p *Provider) Serve(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
	if inv == nil || inv.Request == nil {
		return nil, errors.New("views: an invocation carries its request")
	}
	v := p.views[inv.Capability+":"+inv.Interaction]
	if v == nil {
		return nil, fmt.Errorf("views: no view serves %s:%s", inv.Capability, inv.Interaction)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch {
	case inv.Interaction == Explain:
		return p.explain(ctx, v, inv)
	case v.model.Search != nil:
		return p.search(ctx, v, inv)
	}
	return p.fetch(ctx, v, inv)
}

// fetch checks the values the request names, then runs its read.
func (p *Provider) fetch(ctx context.Context, v *view, inv *ports.Invocation) (*ports.Answer, error) {
	rd, err := v.read(inv)
	if err != nil {
		return nil, err
	}
	var ids []string
	if e := v.model.Exists; e != nil {
		id, err := p.found(ctx, v, inv, e.Error, v.lookupOf(inv, e.Request, e.Object, e.Column, nil))
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if vd := v.model.Valued; vd != nil {
		latest := &ports.Latest{Date: vd.Date, Per: []string{vd.Column}}
		if d, _ := inv.Request.Get(vd.AsOf); d != nil {
			latest.OnOrBefore, _ = d.(string)
		}
		id, err := p.found(ctx, v, inv, vd.Error, v.lookupOf(inv, vd.Request, vd.Object, vd.Column, latest))
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	res, err := p.run(ctx, v, inv, rd.req)
	if err != nil {
		return nil, err
	}
	stream := &answer{p: p, v: v, rd: rd, src: res.Batches, now: p.clock.Now()}
	ans := &ports.Answer{}
	if p.rows {
		ans.Rows = &rowStream{a: stream}
	} else {
		ans.Batches = stream
	}
	if res.QueryID != "" {
		ans.Settle = p.settle(append(ids, res.QueryID))
	}
	return ans, nil
}

// run plans a read and executes it.
func (p *Provider) run(ctx context.Context, v *view, inv *ports.Invocation, req ports.PlanRequest) (*ports.Result, error) {
	plan, err := p.src.Plan(ctx, req)
	if errors.Is(err, ports.ErrQueryTooLarge) {
		return nil, v.tooLarge(inv, err)
	}
	if err != nil {
		return nil, p.failure(v, "planning "+req.Query.Object, err)
	}
	res, err := p.src.Execute(ctx, plan)
	if errors.Is(err, ports.ErrQueryTooLarge) {
		return nil, v.tooLarge(inv, err)
	}
	if err != nil {
		return nil, p.failure(v, "executing "+req.Query.Object, err)
	}
	if res == nil || res.Batches == nil {
		return nil, fmt.Errorf("views: %s: the source answered no batches for %s", v.pinned, req.Query.Object)
	}
	return res, nil
}

// failure is what a source's failure becomes: the capability's declared
// error for a source that cannot answer now, the call's own context error,
// a problem as it is, or the failure wrapped with where it happened, which
// the pipeline answers as internal without its detail.
func (p *Provider) failure(v *view, step string, err error) error {
	var prob *protocol.Problem
	switch {
	case errors.As(err, &prob), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return err
	case errors.Is(err, ports.ErrSourceUnavailable) && v.model.Unavailable != "":
		return v.declared(v.model.Unavailable).Problem("the warehouse that serves the capability is unavailable").WithRetryAfter(5)
	}
	return fmt.Errorf("views: %s: %s: %w", v.pinned, step, err)
}

// tooLarge is a read with more values than the source can bind (R110,
// R112): the caller's invalid request, at the member that named the most
// values, when the request named them; otherwise the caller's data scope
// is larger than the source can express, which is the deployment's fault.
func (v *view) tooLarge(inv *ports.Invocation, err error) error {
	member, most := "", 0
	for _, m := range v.members() {
		val, _ := inv.Request.Get(m)
		if arr, ok := val.([]ijson.Value); ok && len(arr) > most {
			member, most = m, len(arr)
		}
	}
	if member == "" {
		return fmt.Errorf("views: %s: the caller's data scope is more than the source binds in one read: %w", v.pinned, err)
	}
	return protocol.NewProblem(protocol.CodeInvalidRequest, fmt.Sprintf("%s holds %d values, more than the source binds in one read", member, most)).
		WithTarget("/" + member).WithNext("Name fewer values, in more calls if you need them all.")
}

// members are the request members a read takes values from.
func (v *view) members() []string {
	ms := sortedKeys(v.model.Filters)
	if e := v.model.Exists; e != nil {
		ms = append(ms, e.Request)
	}
	if vd := v.model.Valued; vd != nil {
		ms = append(ms, vd.Request)
	}
	return ms
}

// internal is a fault of the source or the deployment, found while holding
// an answer to its contract: never retried, never the caller's (R100). Its
// detail names what failed, never a value the source held.
func internal(format string, a ...any) *protocol.Problem {
	return protocol.NewProblem(protocol.CodeInternal, fmt.Sprintf(format, a...))
}

// fault is an internal problem with the failure behind it: the caller is
// answered the problem alone, and the cause stays on the error for the
// gateway's own record (errors.Is and errors.As reach both).
type fault struct {
	prob  *protocol.Problem
	cause error
}

func (f *fault) Error() string   { return f.prob.Error() + ": " + f.cause.Error() }
func (f *fault) Unwrap() []error { return []error{f.prob, f.cause} }

// internalBecause is internal, keeping its cause.
func internalBecause(cause error, format string, a ...any) error {
	return &fault{prob: internal(format, a...), cause: cause}
}

// settle is a call's settlement: the answer's rows, and the bytes and cost
// of every read the call made, each from the engine's own record (R09).
// The last id is the answer's read.
func (p *Provider) settle(ids []string) func(context.Context) (ports.Usage, error) {
	ids = slices.DeleteFunc(slices.Clone(ids), func(id string) bool { return id == "" })
	main := ids[len(ids)-1]
	return func(ctx context.Context) (ports.Usage, error) {
		total := ports.Usage{QueryID: main}
		for _, id := range ids {
			u, err := p.src.Settle(ctx, id)
			if err != nil {
				return ports.Usage{}, fmt.Errorf("views: settling query %s: %w", id, err)
			}
			if id == main {
				total.Rows = u.Rows
			}
			total.Bytes += u.Bytes
			if total.Cost, err = addCost(total.Cost, u.Cost); err != nil {
				return ports.Usage{}, fmt.Errorf("views: settling query %s: %w", id, err)
			}
		}
		return total, nil
	}
}

// addCost adds cost b to a, unit by unit, exactly.
func addCost(a, b map[string]string) (map[string]string, error) {
	if len(b) == 0 {
		return a, nil
	}
	out := map[string]string{}
	for unit, amount := range a {
		out[unit] = amount
	}
	for unit, amount := range b {
		sum, err := addDecimal(out[unit], amount)
		if err != nil {
			return nil, fmt.Errorf("the cost in %s: %w", unit, err)
		}
		out[unit] = sum
	}
	return out, nil
}

// addDecimal adds two decimal strings exactly, at the larger scale; "" is
// zero.
func addDecimal(a, b string) (string, error) {
	if a == "" {
		a = "0"
	}
	x, err := grammar.ParseDecimal(a)
	if err != nil {
		return "", err
	}
	y, err := grammar.ParseDecimal(b)
	if err != nil {
		return "", err
	}
	scale := max(len(x.Fraction), len(y.Fraction))
	ux, _ := x.Unscaled(scale)
	uy, _ := y.Unscaled(scale)
	return grammar.FormatDecimal(new(big.Int).Add(ux, uy), scale), nil
}

func (v *view) declared(code string) protocol.Declared {
	for _, d := range v.profile.Errors {
		if d.Code == code {
			return d
		}
	}
	return protocol.Declared{Code: code}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
