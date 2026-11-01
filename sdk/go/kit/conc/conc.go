// Package conc starts goroutines (ATLAS-MECH-012). Outside the kit no
// package writes a go statement: it calls Go or a Group, so every goroutine
// is waited for by someone, and a panic in one becomes an error that its
// waiter returns instead of a crash of the whole process.
package conc

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"
)

// PanicError is a panic recovered in a goroutine started here, with the
// stack where it happened.
type PanicError struct {
	Value any
	Stack []byte
}

func (p *PanicError) Error() string { return fmt.Sprintf("panic: %v", p.Value) }

// Unwrap returns the panic's value when it was an error.
func (p *PanicError) Unwrap() error {
	err, _ := p.Value.(error)
	return err
}

// run calls fn, turning a panic into a *PanicError.
func run(fn func() error) (err error) {
	defer func() {
		if v := recover(); v != nil {
			err = &PanicError{Value: v, Stack: debug.Stack()}
		}
	}()
	return fn()
}

// Go runs fn in a new goroutine. The function it returns waits for fn to
// return and gives its error, or a *PanicError if fn panicked; it may be
// called any number of times, from any goroutine.
func Go(fn func() error) (wait func() error) {
	done := make(chan struct{})
	var err error
	go func() {
		defer close(done)
		err = run(fn)
	}()
	return func() error {
		<-done
		return err
	}
}

// Group runs goroutines and waits for them all, as errgroup does: the
// first error (or panic) is the one Wait returns, and with WithContext it
// cancels the group's context. SetLimit bounds how many run at once, so
// fan-out is never unbounded.
type Group struct {
	cancel context.CancelCauseFunc
	wg     sync.WaitGroup
	sem    chan struct{}
	once   sync.Once
	err    error
}

// WithContext returns a Group and a context it cancels when a goroutine
// fails or Wait returns.
func WithContext(ctx context.Context) (*Group, context.Context) {
	ctx, cancel := context.WithCancelCause(ctx)
	return &Group{cancel: cancel}, ctx
}

// SetLimit lets at most n goroutines of the group run at once; Go blocks
// until one finishes. A negative n removes the limit. It must not be
// called while goroutines of the group are running.
func (g *Group) SetLimit(n int) {
	if n < 0 {
		g.sem = nil
		return
	}
	if len(g.sem) != 0 {
		panic(fmt.Errorf("conc: SetLimit while %d goroutines of the group run", len(g.sem)))
	}
	g.sem = make(chan struct{}, n)
}

// Go runs fn in a new goroutine of the group, waiting first for a free
// place when the group has a limit.
func (g *Group) Go(fn func() error) {
	if g.sem != nil {
		g.sem <- struct{}{}
	}
	g.start(fn)
}

// TryGo runs fn only if the group's limit has room, and reports whether it
// did.
func (g *Group) TryGo(fn func() error) bool {
	if g.sem != nil {
		select {
		case g.sem <- struct{}{}:
		default:
			return false
		}
	}
	g.start(fn)
	return true
}

func (g *Group) start(fn func() error) {
	g.wg.Add(1)
	go func() {
		defer g.done()
		if err := run(fn); err != nil {
			g.once.Do(func() {
				g.err = err
				if g.cancel != nil {
					g.cancel(err)
				}
			})
		}
	}()
}

func (g *Group) done() {
	if g.sem != nil {
		<-g.sem
	}
	g.wg.Done()
}

// Wait waits for every goroutine of the group and returns the first error.
func (g *Group) Wait() error {
	g.wg.Wait()
	if g.cancel != nil {
		g.cancel(g.err)
	}
	return g.err
}
