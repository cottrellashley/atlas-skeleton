// Package pipeline is the one execution path of every interaction call
// (ATLAS-CORE §8): nine steps at written positions (constitution rule 9),
// so no provider ever repeats one. It authenticates, routes, decides
// access, validates and normalises the request and enforces its data scope,
// applies the protective limits, records a durable admission, dispatches to
// the capability's provider through its upstream's bulkhead, holds what the
// provider returns to the capability's contract and the caller's scope,
// and records the outcome and the settlement.
//
// The call's deadline bounds every step (R042). Every attempt, refusals
// included, leaves an audit fact that names the step it ended at. A
// provider's fault, a panic among them, costs its own call only: the call
// answers its problem, and everything it held comes back (R043). Every
// failure's cause is logged with the call's request id, and never reaches
// the caller (R017).
package pipeline

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync/atomic"
	"time"

	"github.com/apache/arrow-go/v18/arrow"

	"atlas.local/gateway/core/access"
	"atlas.local/gateway/core/registry"
	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/kit/clock"
	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/arrowschema"
	"atlas.local/sdk/go/protocol/grammar"
	"atlas.local/sdk/go/protocol/ijson"
	"atlas.local/sdk/go/protocol/profile"
	"atlas.local/sdk/go/protocol/resolve"
	"atlas.local/sdk/go/protocol/schema"
)

// Options assemble a pipeline. Every bound has a default, so a pipeline is
// bounded however it is assembled.
type Options struct {
	Environment   string
	Registry      *registry.Registry
	Authenticator ports.Authenticator
	Policy        ports.PolicySource
	Audit         ports.AuditSink
	Clock         clock.Clock
	// Log receives every failure's cause, once, with its call's request id
	// (06-7 §6.7.3: causes are kept for the log, and never put in the
	// body). Nil discards them.
	Log *slog.Logger
	// MinimumProtocol is the lowest protocol a client may declare
	// (default atlas/1.0).
	MinimumProtocol string
	// PolicyRefresh is how often the policy is reread (default 5s), and
	// PolicyMaxAge how long a policy may still decide calls once its source
	// stops answering (default 60s, R13).
	PolicyRefresh, PolicyMaxAge time.Duration
	// StreamRecheck is how often an open table answer decides its caller's
	// access again, under the policy as it then stands (default
	// PolicyRefresh): a grant revoked while an answer is open ends it with a
	// problem (06-2 rule 3; R13's active-connection bound).
	StreamRecheck time.Duration
	// MaxRows is the output cap of a table answer that no policy limit caps
	// (default 100000), so every answer has one (§6.2.6).
	MaxRows int
	// MaxInFlight is how many calls the replica runs at once, from their
	// arrival to the end of their answer (default 1024). A call past it is
	// shed at once, before any work: unavailable, with Retry-After (06-7
	// §6.7.2's in-flight limit).
	MaxInFlight int
	// Upstreams are the bulkheads (06-7 §6.7.5): how many calls may reach
	// each upstream at once, by the name its providers give it with
	// Through. UpstreamConcurrency is that of an upstream not named here
	// (default 32). A call past an upstream's concurrency waits for a place,
	// at most twice as many deep, until its deadline; past the queue it is
	// shed at once: unavailable, with Retry-After, naming the upstream.
	Upstreams           map[string]int
	UpstreamConcurrency int
	// AuditTimeout bounds each fact written for a call that has ended or
	// been refused, which no call's deadline bounds: a refusal, an outcome,
	// a settlement (default 5s).
	AuditTimeout time.Duration
	// Settlements is how many settlements run at once (default 64), and
	// SettleTimeout how long each may take (default 30s).
	Settlements   int
	SettleTimeout time.Duration
}

// Pipeline runs calls.
type Pipeline struct {
	opt       Options
	policy    *policyCache
	limits    limiter
	inflight  *gate
	bulkheads bulkheads
	settler   *settler
	minimum   [2]int
	longest   atomic.Pointer[longest]
}

// longest is how long any interaction of one snapshot may run.
type longest struct {
	generation int
	d          time.Duration
}

// New is a pipeline.
func New(opt Options) (*Pipeline, error) {
	if opt.Clock == nil {
		opt.Clock = clock.System
	}
	if opt.Log == nil {
		opt.Log = slog.New(slog.DiscardHandler)
	}
	if opt.MinimumProtocol == "" {
		opt.MinimumProtocol = protocol.Version
	}
	if opt.PolicyRefresh <= 0 {
		opt.PolicyRefresh = 5 * time.Second
	}
	if opt.PolicyMaxAge <= 0 {
		opt.PolicyMaxAge = 60 * time.Second
	}
	if opt.StreamRecheck <= 0 {
		opt.StreamRecheck = opt.PolicyRefresh
	}
	if opt.MaxRows <= 0 {
		opt.MaxRows = 100_000
	}
	if opt.MaxInFlight <= 0 {
		opt.MaxInFlight = 1024
	}
	if opt.UpstreamConcurrency <= 0 {
		opt.UpstreamConcurrency = 32
	}
	if opt.AuditTimeout <= 0 {
		opt.AuditTimeout = 5 * time.Second
	}
	if opt.Settlements <= 0 {
		opt.Settlements = 64
	}
	if opt.SettleTimeout <= 0 {
		opt.SettleTimeout = 30 * time.Second
	}
	if opt.Registry == nil || opt.Authenticator == nil || opt.Policy == nil || opt.Audit == nil {
		return nil, errors.New("pipeline: a registry, an authenticator, a policy source and an audit sink are required")
	}
	for name, n := range opt.Upstreams {
		if n <= 0 {
			return nil, fmt.Errorf("pipeline: the upstream %q needs a concurrency of at least 1, not %d", name, n)
		}
	}
	major, minor, err := grammar.ParseProtocol(opt.MinimumProtocol)
	if err != nil {
		return nil, fmt.Errorf("pipeline: the minimum protocol: %w", err)
	}
	return &Pipeline{
		opt:       opt,
		policy:    &policyCache{src: opt.Policy, clock: opt.Clock, refresh: opt.PolicyRefresh, maxAge: opt.PolicyMaxAge},
		inflight:  newGate(opt.MaxInFlight),
		bulkheads: bulkheads{concurrency: opt.Upstreams, fallback: opt.UpstreamConcurrency},
		settler:   newSettler(opt.Settlements),
		minimum:   [2]int{major, minor},
	}, nil
}

// Environment is the environment the pipeline serves.
func (p *Pipeline) Environment() string { return p.opt.Environment }

// Registry is the catalogue the pipeline routes by.
func (p *Pipeline) Registry() *registry.Registry { return p.opt.Registry }

// Drain stops starting settlements and waits for those running, each
// bounded by SettleTimeout; a call that ends after it records its
// settlement as unsettled. A deployable drains its pipeline once its
// server has stopped (06-7 §6.7.15's settlement loop).
func (p *Pipeline) Drain() error { return p.settler.drain() }

// Call is one interaction call, as the edge read it from the wire.
type Call struct {
	RequestID string
	// Trace is the call's W3C trace id, from its traceparent, which every
	// log line of the call carries with its request id (06-7 §6.7.16).
	Trace    string
	Attempt  int
	Path     string // the binding's address
	Token    string // the bearer token
	Client   *protocol.Client
	Features protocol.Features
	// Expect is Atlas-Profile-Expect: the exact id the request is written
	// under, or "" (a built-in).
	Expect string
	// Deadline is Atlas-Deadline, or zero for the interaction's default.
	Deadline       time.Time
	IdempotencyKey string
	Interactive    bool
	// Claimed is an identity a header on the request names
	// (x-atlas-on-behalf-of): every fact of the call records it, and
	// nothing trusts it (ATLAS-ACCESS-007).
	Claimed string
	Body    ijson.Value
}

// Result is a call's answer, ready to send.
type Result struct {
	Capability  *registry.Capability
	Interaction *profile.Interaction
	Writer      *profile.Profile
	// Record is a record answer; Rows a table's, read until it ends.
	Record *ijson.Object
	Rows   *Rows
	// Columns are the columns a table answer holds (Projectable), or nil.
	Columns []string
	// Deadline is when the call's work stops.
	Deadline           time.Time
	GovernanceRevision string
	LimitsRemaining    map[string]int64
	// Deprecated and Sunset are set when the version is deprecated.
	Deprecated, Sunset *time.Time
	end                func(ending)
	unsent             func(error) *protocol.Problem
}

// Close ends the call: its rows, its context, its outcome fact, and the
// places it held.
func (r *Result) Close() {
	if r.Rows != nil {
		r.Rows.Close()
		return
	}
	if r.end != nil {
		r.end(ending{})
	}
}

// Gone ends the call of a caller who went away before its answer was
// delivered: the outcome is a cancellation, for a record as for a table
// (R3-07).
func (r *Result) Gone() {
	if r.Rows != nil {
		r.Rows.Gone()
		return
	}
	if r.end != nil {
		r.end(ending{step: ports.StepAnswer, code: ports.CodeCancelled})
	}
}

// Fail ends the call with err, as an edge does that cannot send its
// answer: the outcome is err's problem, internal for an error that is
// none, its cause logged.
func (r *Result) Fail(err error) {
	if r.Rows != nil {
		r.Rows.Fail(err)
		return
	}
	if r.end != nil {
		r.end(ending{step: ports.StepAnswer, code: r.unsent(err).Code})
	}
}

// Run takes a call through the nine steps. Its error is a
// *protocol.Problem; when it returns a Result, the caller sends it and then
// calls Close.
func (p *Pipeline) Run(ctx context.Context, c *Call) (*Result, error) {
	return p.RunNegotiated(ctx, c, nil)
}

// RunNegotiated is Run, with the call's representation chosen by
// negotiate once its access is decided and before its request is
// validated, a limit taken or its work admitted (HANDOFF.md C2, R3-07):
// with the route the address matched, its binding the one the address is,
// an older profile's included (R3-08). A caller without access is refused
// before negotiate runs, so a confidential binding is never disclosed; a
// problem negotiate returns is the call's refusal, and no work runs. A nil
// negotiate negotiates nothing.
func (p *Pipeline) RunNegotiated(ctx context.Context, c *Call, negotiate func(registry.Route) *protocol.Problem) (res *Result, err error) {
	// The replica's room for another call (06-7 §6.7.2): a call past it is
	// shed before any work, and leaves no fact, so overload cannot feed on
	// the audit sink.
	release, ok := p.inflight.take()
	if !ok {
		return nil, protocol.NewProblem(protocol.CodeUnavailable, fmt.Sprintf("the gateway is running as many calls as it allows (%d)", p.opt.MaxInFlight)).WithRetryAfter(shedRetryAfter).WithNext("Wait retry_after seconds, then repeat the call.")
	}
	a := p.begin(ctx, c, release)
	a.negotiate = negotiate
	defer func() {
		if v := recover(); v != nil {
			res, err = nil, a.panicked(v)
		}
	}()
	return a.run()
}

func (a *attempt) run() (*Result, error) {
	p, c := a.p, a.c
	snap := p.opt.Registry.Snapshot()
	// Transport: what a client declares, and its deadline, before anything
	// is known of who it is (ATLAS-CORE §8: no capability-specific detail
	// yet). From here the deadline bounds every step (R042): Atlas-Deadline,
	// within the longest any interaction may run, narrowed after step 2.
	if prob := p.protocol(c.Client); prob != nil {
		return a.stop(ports.StepTransport, prob)
	}
	outer := a.start.Add(p.longestOf(snap))
	if !c.Deadline.IsZero() {
		if !c.Deadline.After(a.start) {
			return a.stop(ports.StepTransport, protocol.NewProblem(protocol.CodeDeadlineExceeded, "the call's deadline had passed when it arrived").WithTarget("Atlas-Deadline").WithNext("Repeat the call with a later deadline."))
		}
		if c.Deadline.Before(outer) {
			outer = c.Deadline
		}
	}
	a.bound(outer)

	// Step 1: who is calling, and the policy that gives their context.
	a.step = ports.StepIdentity
	id, err := p.opt.Authenticator.Authenticate(a.ctx, c.Token)
	if err != nil {
		if prob := a.interrupted(); prob != nil {
			return a.stop(ports.StepIdentity, prob)
		}
		prob := authProblem(err)
		if prob.Code != protocol.CodeUnauthenticated {
			a.log.Error("the authenticator failed", "error", err)
		}
		return a.stop(ports.StepIdentity, prob)
	}
	a.fact.Principal, a.fact.CallerKind, a.fact.Application = id.Principal, id.Kind, id.Application
	policy, err := p.policy.get(a.ctx)
	if err != nil {
		if prob := a.interrupted(); prob != nil {
			return a.stop(ports.StepIdentity, prob)
		}
		a.log.Error("the access policy cannot be read", "error", err)
		return a.stop(ports.StepIdentity, protocol.NewProblem(protocol.CodeUnavailable, "the gateway cannot read its access policy now").WithRetryAfter(5).WithNext("Repeat the call after retry_after seconds."))
	}
	a.fact.PolicyRevision = policy.Revision
	subject := access.ResolveIn(policy, id, snap)

	// Step 2: the address, to an admitted capability and interaction, and
	// the interaction's deadline.
	a.step = ports.StepAddress
	route, ok := snap.Route(c.Path)
	if !ok {
		return a.stop(ports.StepAddress, protocol.NewProblem(protocol.CodeNotFound, "no capability is served at this address").WithNext("Call atlas.catalog@v1:fetch for what you may use."))
	}
	cap, in := route.Capability, route.Capability.Current.Interactions[route.Interaction]
	pinned := cap.Pinned.String()
	a.fact.Address, a.fact.Exact = pinned+":"+in.Name, cap.Current.Exact().String()
	a.log = a.log.With("address", a.fact.Address)
	deadline := a.start.Add(in.Deadline.Default)
	if in.Deadline.Default <= 0 {
		deadline = outer
	}
	if !c.Deadline.IsZero() {
		deadline = c.Deadline
	}
	if in.Deadline.Max > 0 && deadline.After(a.start.Add(in.Deadline.Max)) {
		deadline = a.start.Add(in.Deadline.Max)
	}
	if !deadline.After(p.opt.Clock.Now()) {
		return a.stop(ports.StepAddress, protocol.NewProblem(protocol.CodeDeadlineExceeded, "the call reached its deadline before it was admitted").WithNext("Repeat the call with a later deadline."))
	}
	a.bound(deadline)

	// Step 3: visibility, denies and grants.
	a.step = ports.StepAccess
	if d := subject.Decide(cap.Current, in.Name); !d.Allowed {
		return a.stop(ports.StepAccess, decision(d, pinned))
	}
	if cap.Current.Lifecycle == "retired" {
		return a.stop(ports.StepAddress, protocol.NewProblem(protocol.CodeRetired, pinned+" was retired at its sunset").WithNext(accessNext("Use the version that replaced it, which atlas.describe@v1 names", pinned, "says why")))
	}
	for _, t := range cap.Current.Traits {
		if t.Critical && !c.Features.Understands(lower(t.Name)) {
			return a.stop(ports.StepAddress, protocol.NewProblem(protocol.CodeCriticalTraitUnsupported, "the capability's trait "+t.Name+" is critical, and the client does not implement it").WithTarget("Atlas-Client-Features").WithNext("Use a client that implements "+t.Name+"."))
		}
	}

	// The representation, once access is decided and before any work.
	if a.negotiate != nil {
		a.step = ports.StepNegotiate
		if prob := a.negotiate(route); prob != nil {
			return a.stop(ports.StepNegotiate, prob)
		}
	}

	// Step 4: the request, valid under the profile it was written for,
	// resolved forward, its defaults filled, then held to the scope.
	a.step = ports.StepRequest
	req, columns, prob := p.request(cap, in, c)
	if prob != nil {
		return a.stop(ports.StepRequest, prob)
	}
	digest, err := ijson.Canonical(req)
	if err != nil {
		return a.stop(ports.StepRequest, a.fault("the normalised request has no canonical form", "error", err))
	}
	sum := sha256.Sum256(digest)
	a.fact.RequestDigest = hex.EncodeToString(sum[:])
	var takes, row *schema.Schema
	if in.Takes != "" {
		takes = cap.Current.Schemas[in.Takes]
	}
	if s := cap.Current.Schemas[in.Returns]; s != nil {
		row = s
		if it := s.Items(); it != nil {
			row = it
		}
	}
	a.step = ports.StepScope
	scope := subject.ScopeFor(takes, row)
	if prob := outside(scope, takes, req, pinned, "is outside the data you may read"); prob != nil {
		return a.stop(ports.StepScope, prob)
	}

	// Steps 5 and 6: protective limits. (Idempotency claims and approval
	// evidence come with the first capability whose effect is not read.)
	a.step = ports.StepLimits
	limit := subject.Limit(cap.Current)
	maxRows := limit.MaxRows
	if maxRows <= 0 {
		maxRows = p.opt.MaxRows
	}
	key := id.Principal + "\x00" + pinned
	release, retry, ok := p.limits.take(key, limit, a.start)
	if !ok {
		return a.stop(ports.StepLimits, protocol.NewProblem(protocol.CodeLimitExceeded, "a rate or concurrency limit on this capability is reached").WithRetryAfter(retry).WithNext(accessNext("Wait retry_after seconds, then repeat the call", pinned, "shows your limits")))
	}
	a.keep(release)

	// Step 7: a durable admission, within the call's deadline; then the
	// upstream's bulkhead, and the provider.
	a.step = ports.StepDispatch
	admitted := a.fact
	admitted.Kind, admitted.At = ports.Admission, p.opt.Clock.Now()
	if err := p.write(a.ctx, admitted); err != nil {
		if prob := a.interrupted(); prob != nil {
			a.log.Warn("the admission was not recorded before the call ended; it is not run", "error", err)
			return a.drop(prob)
		}
		a.log.Error("the admission cannot be recorded, so the call is not run", "error", err)
		return a.drop(protocol.NewProblem(protocol.CodeUnavailable, "the gateway cannot record the call's admission, so it does not run it").WithRetryAfter(5).WithNext("Repeat the call after retry_after seconds."))
	}
	a.admitted = admitted.At
	if name := upstreamOf(cap.Provider); name != "" {
		b := p.bulkheads.of(name)
		release, err := b.enter(a.ctx)
		if err != nil {
			prob := a.interrupted()
			if errors.Is(err, errFull) || prob == nil {
				a.log.Warn("the upstream's bulkhead is full; the call is shed", "upstream", name)
				prob = b.full()
			}
			return a.stop(ports.StepDispatch, prob)
		}
		a.keep(release)
	}
	work := access.WithSubject(a.ctx, subject)
	inv := &ports.Invocation{
		RequestID: c.RequestID, Capability: pinned, Interaction: in.Name, Exact: cap.Current.Exact().String(),
		Request: req, Columns: withScoped(columns, row), Caller: subject.Caller(), Scope: scope, Deadline: deadline, MaxRows: maxRows, Features: c.Features,
	}
	answer, err := a.serve(cap.Provider, work, inv)
	if answer != nil {
		a.settle = answer.Settle
	}
	if err != nil {
		return a.stop(ports.StepDispatch, a.failure(cap.Current, err))
	}

	// Step 8: the answer, held to the contract and the scope.
	a.step = ports.StepAnswer
	res := &Result{
		Capability: cap, Interaction: in, Writer: cap.Current, Columns: columns, Deadline: deadline,
		GovernanceRevision: policy.Revision, LimitsRemaining: p.limits.remaining(key, limit), end: a.end, unsent: a.unsent,
	}
	res.Deprecated, res.Sunset = lifecycleDates(cap.Current)
	if prob := a.shape(answer, in); prob != nil {
		return a.stop(ports.StepAnswer, prob)
	}
	if answer.Record != nil {
		rec, prob := a.checked(cap.Current.Schemas[in.Returns], in.Result, answer.Record, scope)
		if prob != nil {
			return a.stop(ports.StepAnswer, prob)
		}
		res.Record = rec
		return res, nil
	}
	check := row
	if columns != nil && row != nil {
		check = row.Project(columns)
	}
	rows := &Rows{
		ctx: work, src: answer.Rows, batches: answer.Batches, schema: check, full: row, pinned: pinned,
		scope: scope, granted: scope, max: maxRows, every: p.opt.StreamRecheck, checked: p.opt.Clock.Now(), clock: p.opt.Clock,
		fault: func(err error) *protocol.Problem { return a.failure(cap.Current, err) }, log: a.log, end: a.end,
		recheck: a.recheck(policy, id, cap.Current, in.Name, takes, row, req),
	}
	if answer.Batches != nil {
		if rows.arrow, prob = a.batchSchema(check, answer.Batches); prob != nil {
			return nil, rows.stop(prob)
		}
	}
	res.Rows = rows
	return res, nil
}

// shape refuses an answer that is not one of what its interaction returns:
// a record, or a table as rows or as batches (ports.Answer).
func (a *attempt) shape(answer *ports.Answer, in *profile.Interaction) *protocol.Problem {
	if answer == nil {
		return a.fault("the capability's provider answered nothing")
	}
	n := 0
	for _, set := range []bool{answer.Record != nil, answer.Rows != nil, answer.Batches != nil} {
		if set {
			n++
		}
	}
	switch table := in.Result == "table"; {
	case n != 1:
		return a.fault("the capability's provider answered other than one of a record, rows and batches", "answers", n)
	case table && answer.Record != nil:
		return a.fault("the capability's provider answered a record for a table")
	case !table && answer.Record == nil:
		return a.fault("the capability's provider answered a table for a record", "result", in.Result)
	}
	return nil
}

// batchSchema is the Arrow schema a table's batches carry: its columns as
// §9.2.7 derives them from the row's schema, which the stream must hold.
func (a *attempt) batchSchema(row *schema.Schema, batches ports.BatchStream) (*arrow.Schema, *protocol.Problem) {
	fields, err := arrowschema.Fields(row)
	if err != nil {
		return nil, a.fault("the capability's table has no Arrow form, so it cannot answer batches", "error", err)
	}
	want := arrow.NewSchema(fields, nil)
	got := batches.Schema()
	if got == nil {
		return nil, a.fault("the capability's batches have no schema")
	}
	for _, f := range fields {
		at := got.FieldIndices(f.Name)
		if len(at) != 1 || !arrow.TypeEqual(got.Field(at[0]).Type, f.Type) {
			return nil, a.fault("the capability's batches do not match its contract", "column", f.Name)
		}
	}
	return want, nil
}

// recheck decides the caller's access again under the policy as it now
// stands, for an answer that is open (06-2 rule 3): the scope it now gives,
// or the problem that ends the answer. A policy the gateway cannot renew
// ends it too, since it can no longer promise that a revocation took hold
// (R13).
func (a *attempt) recheck(at *ports.Policy, id ports.Identity, prof *profile.Profile, interaction string, takes, row *schema.Schema, req *ijson.Object) func(context.Context) (ports.Scope, *protocol.Problem) {
	pinned := grammar.Pinned{ID: prof.ID, Version: prof.Version}.String()
	return func(ctx context.Context) (ports.Scope, *protocol.Problem) {
		policy, err := a.p.policy.get(ctx)
		if err != nil {
			a.log.Error("the access policy cannot be renewed, so an open answer is stopped", "error", err)
			return nil, protocol.NewProblem(protocol.CodeUnavailable, "the gateway cannot renew its access policy, so it stopped the answer").WithRetryAfter(5).WithNext("Repeat the call after retry_after seconds.")
		}
		if policy.Revision == at.Revision {
			return nil, nil
		}
		subject := access.ResolveIn(policy, id, a.p.opt.Registry.Snapshot())
		if d := subject.Decide(prof, interaction); !d.Allowed {
			return nil, decision(d, pinned)
		}
		scope := subject.ScopeFor(takes, row)
		if prob := outside(scope, takes, req, pinned, "was withdrawn from the data you may read while the answer was open"); prob != nil {
			return nil, prob
		}
		return scope, nil
	}
}

// decision is the problem of a refusal at step 3: not_found keeps a
// confidential capability undisclosed; any other names atlas.access (02-6
// rule 4, R081).
func decision(d access.Decision, pinned string) *protocol.Problem {
	prob := protocol.NewProblem(d.Code, d.Reason)
	switch d.Code {
	case protocol.CodeNotGranted:
		prob.WithNext(accessNext("", pinned, "for what you hold, and who grants it"))
	case protocol.CodeDenied:
		prob.WithNext(accessNext("A deny refuses this call", pinned, "for its reason, and who to ask"))
	default:
		prob.Detail = "no capability is served at this address"
		prob.WithNext("Call atlas.catalog@v1:fetch for what you may use.")
	}
	return prob
}

// outside is the refusal of a request that names data outside scope.
func outside(scope ports.Scope, takes *schema.Schema, req *ijson.Object, pinned, what string) *protocol.Problem {
	for _, v := range access.Values(takes, req) {
		if !scope.Allows(v.Dimension, v.Value) {
			return protocol.NewProblem(protocol.CodeNotGranted, fmt.Sprintf("the %s %q %s", v.Dimension, v.Value, what)).WithTarget(v.Pointer).
				WithNext(accessNext("", pinned, "for the data you may read, and who to ask; atlas.whoami@v1:fetch lists your scope"))
		}
	}
	return nil
}

// longestOf is how long any interaction the snapshot serves may run: a
// call's bound before its address is known (06-7 §6.7.2's outer
// deadline).
func (p *Pipeline) longestOf(s *registry.Snapshot) time.Duration {
	if l := p.longest.Load(); l != nil && l.generation == s.Generation {
		return l.d
	}
	var d time.Duration
	for _, c := range s.Capabilities() {
		for _, in := range c.Current.Interactions {
			d = max(d, in.Deadline.Default, in.Deadline.Max)
		}
	}
	if d <= 0 {
		d = 2 * time.Minute
	}
	p.longest.Store(&longest{generation: s.Generation, d: d})
	return d
}

func (p *Pipeline) protocol(c *protocol.Client) *protocol.Problem {
	if c == nil {
		return protocol.NewProblem(protocol.CodeInvalidRequest, "the call carries no valid Atlas-Client (§9.6.3)").WithTarget("Atlas-Client").WithNext("Send Atlas-Client with name, version and protocol.")
	}
	major, minor, err := grammar.ParseProtocol(c.Protocol)
	if err != nil || major < p.minimum[0] || major == p.minimum[0] && minor < p.minimum[1] {
		return protocol.NewProblem(protocol.CodeUpgradeRequired, fmt.Sprintf("the client speaks %s, below the gateway's minimum %s", c.Protocol, p.opt.MinimumProtocol)).WithTarget("Atlas-Client").WithNext("Upgrade the client.")
	}
	return nil
}

func authProblem(err error) *protocol.Problem {
	switch {
	case errors.Is(err, ports.ErrTokenExpired):
		return protocol.NewProblem(protocol.CodeUnauthenticated, expired).WithNext("Refresh the token and repeat the call once.")
	case errors.Is(err, ports.ErrUnauthenticated):
		return protocol.NewProblem(protocol.CodeUnauthenticated, "the call carries no valid token").WithNext("Sign in, then repeat the call.")
	}
	return protocol.NewProblem(protocol.CodeUnavailable, "the gateway cannot verify tokens now").WithRetryAfter(5).WithNext("Repeat the call after retry_after seconds.")
}

// Expired reports whether a refusal was for an expired token: the edge adds
// error="invalid_token" to its challenge (§9.6.6).
func Expired(prob *protocol.Problem) bool {
	return prob.Code == protocol.CodeUnauthenticated && prob.Detail == expired
}

const expired = "the token has expired"

// request is step 4: the body validated under the profile the client wrote
// it for, through that profile's own interaction (R04: a request is
// validated under its own contract before any transformation), resolved
// forward to the current profile, its defaults filled in, and the columns
// it names checked.
func (p *Pipeline) request(cap *registry.Capability, in *profile.Interaction, c *Call) (*ijson.Object, []string, *protocol.Problem) {
	body, ok := c.Body.(*ijson.Object)
	if !ok {
		return nil, nil, protocol.NewProblem(protocol.CodeInvalidRequest, "the request is a JSON object").WithTarget("").WithNext("Send the request record as an object; {} when it has no members.")
	}
	writer, wi := cap.Current, slices.IndexFunc(cap.Chain, func(q *profile.Profile) bool { return q == cap.Current })
	if c.Expect != "" && c.Expect != cap.Current.Exact().String() {
		wi = slices.IndexFunc(cap.Chain, func(q *profile.Profile) bool { return q.Exact().String() == c.Expect })
		if wi < 0 {
			return nil, nil, protocol.NewProblem(protocol.CodeResolutionFailed, "the request was written under "+c.Expect+", a profile this gateway never served for "+cap.Pinned.String()).WithTarget("Atlas-Profile-Expect").WithNext("Describe the capability again and write the request under its current profile.")
		}
		writer = cap.Chain[wi]
	}
	if in.Takes == "" {
		if body.Len() > 0 {
			return nil, nil, protocol.NewProblem(protocol.CodeInvalidRequest, "this interaction takes no request members").WithTarget("/" + body.Names()[0])
		}
		return body, nil, nil
	}
	ws := takesOf(writer, in.Name)
	if ws == nil {
		return nil, nil, protocol.NewProblem(protocol.CodeResolutionFailed, "the profile the request was written under has no request for "+in.Name).WithTarget("Atlas-Profile-Expect")
	}
	if fs := ws.Validate(body); len(fs) > 0 {
		return nil, nil, protocol.InvalidRequest(fs).WithNext("Correct the request at target; atlas.describe@v1 gives its schema.")
	}
	req := body
	cs := cap.Current.Schemas[in.Takes]
	if writer != cap.Current {
		chain := make([]resolve.Profile, len(cap.Chain))
		for i, q := range cap.Chain {
			s := takesOf(q, in.Name)
			if s == nil && i >= wi {
				return nil, nil, protocol.NewProblem(protocol.CodeResolutionFailed, q.Exact().String()+" has no request for "+in.Name).WithTarget("Atlas-Profile-Expect")
			}
			chain[i] = resolve.Profile{Exact: q.Exact(), Lens: q.Evolution.Lens, Schema: s}
		}
		plan, err := resolve.New(chain, writer.Exact(), cap.Current.Exact(), resolve.Options{})
		if err != nil {
			return nil, nil, protocol.ResolutionFailed(err)
		}
		if req, err = plan.Record(body); err != nil {
			return nil, nil, protocol.ResolutionFailed(err)
		}
	} else {
		req = ijson.Clone(body).(*ijson.Object)
	}
	for _, name := range cs.Properties() {
		if _, present := req.Get(name); !present {
			if ps, _ := cs.Property(name); ps != nil {
				if d, ok := ps.Default(); ok {
					req.Set(name, ijson.Clone(d))
				}
			}
		}
	}
	if fs := cs.Validate(req); len(fs) > 0 {
		return nil, nil, protocol.InvalidRequest(fs)
	}
	var columns []string
	if v, ok := req.Get("columns"); ok && v != nil && cap.Current.HasTrait("Projectable") {
		row := cap.Current.Schemas[in.Returns]
		if it := row.Items(); it != nil {
			row = it
		}
		for i, name := range v.([]ijson.Value) {
			s := name.(string)
			if _, ok := row.Property(s); ok {
				columns = append(columns, s)
				continue
			}
			found := ""
			for _, prop := range row.Properties() {
				ps, _ := row.Property(prop)
				if slices.Contains(ps.Aliases(), s) {
					found = prop
				}
			}
			if found == "" {
				return nil, nil, protocol.NewProblem(protocol.CodeInvalidRequest, fmt.Sprintf("the result has no column %q", s)).WithTarget(fmt.Sprintf("/columns/%d", i)).WithNext("Name columns the result schema has; atlas.describe@v1 lists them.")
			}
			columns = append(columns, found)
		}
	}
	return req, columns, nil
}

// takesOf is the request schema of a profile's own interaction of that
// name, by the name that profile gives it (PR07), or nil.
func takesOf(q *profile.Profile, interaction string) *schema.Schema {
	in := q.Interactions[interaction]
	if in == nil || in.Takes == "" {
		return nil
	}
	return q.Schemas[in.Takes]
}

func lifecycleDates(p *profile.Profile) (deprecated, sunset *time.Time) {
	if p.Lifecycle != "deprecated" {
		return nil, nil
	}
	props, _ := p.Doc.Get("properties")
	o, _ := props.(*ijson.Object)
	date := func(name string) *time.Time {
		v, _ := o.Get(name)
		s, _ := v.(string)
		d, err := grammar.ParseDate(s)
		if err != nil {
			return nil
		}
		t := time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC)
		return &t
	}
	return date("deprecated"), date("sunset")
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}
