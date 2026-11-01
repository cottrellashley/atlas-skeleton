package pipeline

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"
	"unicode/utf8"

	"atlas.local/gateway/core/registry"
	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/kit/conc"
	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/profile"
)

// attempt is one call on its way through the steps: what it holds, the
// facts it leaves, and its one ending. Whatever ends it (a refusal, a
// failure, the end of its answer, its caller going away, a panic) ends it
// once: everything it holds is released, its context is cancelled, its
// last fact is written, and its settlement begins (R043, R09).
type attempt struct {
	p    *Pipeline
	c    *Call
	base context.Context // the caller's
	// ctx is the call's: bounded by its deadline from the first step
	// (R042), and cancelled when the attempt ends.
	ctx    context.Context
	cancel context.CancelFunc
	start  time.Time
	step   ports.Step // the step it is at, which a fact names
	fact   ports.Fact // what every fact of the call shares
	log    *slog.Logger
	// negotiate, when set, chooses the call's representation once its access
	// is decided (RunNegotiated).
	negotiate func(registry.Route) *protocol.Problem

	holding  []func() // released when it ends, last first
	admitted time.Time
	settle   func(context.Context) (ports.Usage, error)
	once     sync.Once
}

// ending is how an attempt ended.
type ending struct {
	step      ports.Step
	code      string // "" for an answer sent whole
	rows      int64
	truncated bool
	// silent leaves no fact: the audit sink is what failed.
	silent bool
}

// maxClaim bounds what a fact keeps of a value the caller chose.
const maxClaim = 256

// clip is s cut to maxClaim bytes, at a character's boundary.
func clip(s string) string {
	if len(s) <= maxClaim {
		return s
	}
	n := maxClaim
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func (p *Pipeline) begin(ctx context.Context, c *Call, release func()) *attempt {
	a := &attempt{p: p, c: c, base: ctx, ctx: ctx, cancel: func() {}, start: p.opt.Clock.Now(), step: ports.StepTransport, holding: []func(){release}}
	a.fact = ports.Fact{At: a.start, RequestID: c.RequestID, Attempt: max(c.Attempt, 1), Environment: p.opt.Environment, Expected: clip(c.Expect), Claimed: clip(c.Claimed)}
	if c.Client != nil {
		a.fact.Client = protocol.Client{Name: clip(c.Client.Name), Version: clip(c.Client.Version), Protocol: clip(c.Client.Protocol)}
	}
	a.log = p.opt.Log.With("request_id", c.RequestID)
	if c.Trace != "" {
		a.log = a.log.With("trace_id", clip(c.Trace))
	}
	return a
}

// bound puts deadline on the call's context: every step from here waits
// for nothing longer, and a provider has only what is left (R042). The
// deadline is the injected clock's; the context runs for what is left of
// it now, so the two never disagree.
func (a *attempt) bound(deadline time.Time) {
	ctx, cancel := context.WithTimeout(a.ctx, deadline.Sub(a.p.opt.Clock.Now()))
	outer := a.cancel
	a.ctx, a.cancel = ctx, func() { cancel(); outer() }
}

// keep holds release until the attempt ends.
func (a *attempt) keep(release func()) { a.holding = append(a.holding, release) }

// gone reports whether the caller went away: its context was cancelled.
func (a *attempt) gone() bool { return errors.Is(a.base.Err(), context.Canceled) }

// interrupted is the problem of a call whose context ended before its
// step did: its caller went away, or its deadline passed. It is nil while
// the call runs.
func (a *attempt) interrupted() *protocol.Problem {
	switch {
	case a.gone():
		// Nobody reads it: the outcome is ports.CodeCancelled.
		return protocol.NewProblem(protocol.CodeDeadlineExceeded, "the call was cancelled by its caller; its work was stopped")
	case a.ctx.Err() != nil:
		return protocol.NewProblem(protocol.CodeDeadlineExceeded, "the call reached its deadline; its work was stopped").WithNext("Repeat the call with a later deadline, or narrow it.")
	}
	return nil
}

// stop ends a call before its answer: refused at step, or, once admitted,
// failed there.
func (a *attempt) stop(step ports.Step, prob *protocol.Problem) (*Result, error) {
	a.end(ending{step: step, code: prob.Code})
	return nil, prob
}

// drop ends a call whose fact cannot be written: the sink failed.
func (a *attempt) drop(prob *protocol.Problem) (*Result, error) {
	a.end(ending{silent: true})
	return nil, prob
}

// end is the attempt's one ending.
func (a *attempt) end(e ending) {
	a.once.Do(func() {
		for i := len(a.holding) - 1; i >= 0; i-- {
			a.holding[i]()
		}
		a.cancel()
		if !e.silent {
			f := a.fact
			f.At = a.p.opt.Clock.Now()
			f.Kind = ports.Refusal
			if !a.admitted.IsZero() {
				f.Kind, f.Duration = ports.Outcome, f.At.Sub(a.admitted)
			}
			f.Rows, f.Truncated = e.rows, e.truncated
			if e.code != "" {
				f.Step, f.Code = e.step, e.code
				if a.gone() {
					f.Code = ports.CodeCancelled
				}
			}
			a.record(f)
		}
		if a.settle != nil {
			settle := a.settle
			if !a.p.settler.start(func() { a.settleNow(settle) }) {
				a.unsettled()
			}
		}
	})
}

// record writes a fact that no call's deadline bounds: within AuditTimeout,
// and whether or not its caller is still there. A fact that cannot be
// written is logged, never lost in silence (R09).
func (a *attempt) record(f ports.Fact) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(a.base), a.p.opt.AuditTimeout)
	defer cancel()
	if err := a.p.write(ctx, f); err != nil {
		a.log.Error("an audit fact was not recorded", "kind", string(f.Kind), "code", f.Code, "error", err)
	}
}

// write is the audit sink's Record, a panic in it an error.
func (p *Pipeline) write(ctx context.Context, f ports.Fact) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = fmt.Errorf("pipeline: the audit sink panicked: %v", v)
		}
	}()
	return p.opt.Audit.Record(ctx, f)
}

// panicked ends an attempt a panic stopped: the call answers internal, and
// the process lives (ATLAS-MECH-007).
func (a *attempt) panicked(v any) error {
	a.log.Error("a panic stopped the call", "step", string(a.step), "panic", fmt.Sprint(v), "stack", string(debug.Stack()))
	prob := protocol.NewProblem(protocol.CodeInternal, "an internal error occurred")
	a.end(ending{step: a.step, code: prob.Code})
	return prob
}

// serve is the provider's Serve, a panic in it an error.
func (a *attempt) serve(pr ports.Provider, ctx context.Context, inv *ports.Invocation) (answer *ports.Answer, err error) {
	defer func() {
		if v := recover(); v != nil {
			answer, err = nil, &conc.PanicError{Value: v, Stack: debug.Stack()}
		}
	}()
	return pr.Serve(ctx, inv)
}

// failure is the problem a provider's or a source's error becomes: one its
// capability declares, a registered one, or internal, so no detail of a
// fault reaches the caller; and its cause is logged, once, with the call's
// request id (R017, 06-7 §6.7.3: causes are kept for the log).
func (a *attempt) failure(prof *profile.Profile, err error) *protocol.Problem {
	var prob *protocol.Problem
	var panicked *conc.PanicError
	switch {
	case errors.As(err, &panicked):
		a.log.Error("the capability's provider panicked", "panic", fmt.Sprint(panicked.Value), "stack", string(panicked.Stack))
		return protocol.NewProblem(protocol.CodeInternal, "an internal error occurred")
	case errors.As(err, &prob):
		if prob.Check(prof.Errors...) == nil {
			return prob
		}
		a.log.Error("the capability answered a problem it does not declare", "code", prob.Code, "error", err)
		return protocol.NewProblem(protocol.CodeInternal, "the capability answered a problem it does not declare")
	}
	if prob := a.interrupted(); prob != nil {
		a.log.Info("the call ended before its work did", "error", err)
		return prob
	}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		a.log.Warn("the capability's work timed out", "error", err)
		return protocol.NewProblem(protocol.CodeDeadlineExceeded, "the call did not complete before its deadline; its work was stopped").WithNext("Repeat the call with a later deadline, or narrow it.")
	case errors.Is(err, ports.ErrWarehouseUnavailable):
		a.log.Warn("the capability's source is unavailable", "error", err)
		return protocol.NewProblem(protocol.CodeUpstreamFailed, "the warehouse that serves the capability is unavailable").WithRetryAfter(5).WithNext("Wait retry_after seconds, then repeat the call.")
	}
	a.log.Error("the capability failed", "error", err)
	return protocol.NewProblem(protocol.CodeInternal, "an internal error occurred")
}

// unsent is the problem of an answer the edge could not send, its cause
// logged.
func (a *attempt) unsent(err error) *protocol.Problem {
	var prob *protocol.Problem
	if errors.As(err, &prob) {
		return prob
	}
	a.log.Error("the answer could not be sent", "error", err)
	return protocol.NewProblem(protocol.CodeInternal, "an internal error occurred")
}

// fault is a pipeline's own check of an answer failing: logged with what
// failed, never with the data, and answered internal.
func (a *attempt) fault(msg string, args ...any) *protocol.Problem {
	a.log.Error(msg, args...)
	return protocol.NewProblem(protocol.CodeInternal, msg)
}

// settleNow settles the call from its source's own record, and writes the
// settlement fact (R09, §6.2.8: the source's record, copied in at
// settlement). It runs after the answer, off the call's path.
func (a *attempt) settleNow(settle func(context.Context) (ports.Usage, error)) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(a.base), a.p.opt.SettleTimeout)
	defer cancel()
	usage, err := func() (u ports.Usage, err error) {
		defer func() {
			if v := recover(); v != nil {
				err = &conc.PanicError{Value: v, Stack: debug.Stack()}
			}
		}()
		return settle(ctx)
	}()
	f := a.fact
	f.Kind, f.At = ports.Settlement, a.p.opt.Clock.Now()
	if err != nil {
		f.Step, f.Code = ports.StepSettle, a.settleFailure(err)
	} else {
		f.Source, f.Usage = &usage, usage.Cost
	}
	a.record(f)
}

// settleFailure is the code a failed settlement is recorded with, its cause
// logged: the call stays unsettled until its source is reconciled.
func (a *attempt) settleFailure(err error) string {
	var panicked *conc.PanicError
	switch {
	case errors.As(err, &panicked):
		a.log.Error("the capability's settlement panicked", "panic", fmt.Sprint(panicked.Value), "stack", string(panicked.Stack))
		return protocol.CodeInternal
	case errors.Is(err, context.DeadlineExceeded):
		a.log.Warn("the source's record was not read in time; the call is unsettled", "error", err)
		return protocol.CodeDeadlineExceeded
	case errors.Is(err, ports.ErrWarehouseUnavailable):
		a.log.Warn("the source cannot give its record; the call is unsettled", "error", err)
		return protocol.CodeUpstreamFailed
	}
	a.log.Error("the call could not be settled", "error", err)
	return protocol.CodeInternal
}

// unsettled records a settlement that could not begin: the gateway is
// draining, or as many settlements as it allows are running.
func (a *attempt) unsettled() {
	a.log.Warn("the call's settlement could not begin; it is unsettled")
	f := a.fact
	f.Kind, f.At, f.Step, f.Code = ports.Settlement, a.p.opt.Clock.Now(), ports.StepSettle, protocol.CodeUnavailable
	a.record(f)
}
