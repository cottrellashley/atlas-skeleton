// Includes MIT-licensed serving utilities; see sdk/go/THIRD_PARTY_NOTICES.md.
// Changes: import paths; later edits are marked ATLAS.

package lifecycle

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"atlas.local/sdk/go/kit/conc"
)

func TestGroupInterruptsEveryoneWhenOneReturns(t *testing.T) {
	var g Group
	var interrupted atomic.Int32
	stop := make(chan struct{})
	for i := 0; i < 3; i++ {
		g.Add(func() error { <-stop; return nil }, func(error) { interrupted.Add(1) })
	}
	want := errors.New("first")
	g.Add(func() error { return want }, func(error) { interrupted.Add(1) })
	// closing stop from an interrupt simulates actors honouring interrupt
	g.actors[0].interrupt = func(error) { interrupted.Add(1); close(stop) }

	err := g.Run()
	if !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
	if interrupted.Load() != 4 {
		t.Fatalf("interrupted %d actors, want 4", interrupted.Load())
	}
}

func TestContextActorCancels(t *testing.T) {
	execute, interrupt := ContextActor(context.Background(), func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	done := make(chan error, 1)
	go func() { done <- execute() }()
	interrupt(nil)
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("actor did not stop")
	}
}

func TestReadiness(t *testing.T) {
	var r Readiness
	if r.Ready() {
		t.Fatal("zero value must be not ready")
	}
	r.Set(true)
	if !r.Ready() {
		t.Fatal("expected ready")
	}
}

// ATLAS-MECH-012 (ATLAS): an actor that panics stops the group as an
// error, with the panic and its stack, instead of crashing the process
// past every other actor's drain.
func TestGroupTurnsAnActorsPanicIntoItsError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var g Group
		stop := make(chan struct{})
		var drained atomic.Bool
		g.Add(func() error { <-stop; drained.Store(true); return nil }, func(error) { close(stop) })
		g.Add(func() error { panic("an actor's defect") }, func(error) {})
		var p *conc.PanicError
		if err := g.Run(); !errors.As(err, &p) || p.Value != "an actor's defect" || len(p.Stack) == 0 {
			t.Fatalf("got %v", err)
		}
		if !drained.Load() {
			t.Fatal("the other actor was not interrupted and waited for")
		}
	})
}
