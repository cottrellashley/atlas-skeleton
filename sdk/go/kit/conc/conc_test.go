package conc

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// Every test runs in a synctest bubble, which fails if a goroutine started
// in it is still running when it ends: nothing here leaks.

func TestGoReturnsTheError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		want := errors.New("failed")
		wait := Go(func() error { return want })
		if err := wait(); err != want {
			t.Fatalf("got %v", err)
		}
		if err := wait(); err != want {
			t.Fatalf("a second wait: %v", err)
		}
	})
}

// ATLAS-MECH-012: a panic is an error for its waiter, not a crash.
func TestGoRecoversAPanic(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		cause := errors.New("boom")
		err := Go(func() error { panic(cause) })()
		var p *PanicError
		if !errors.As(err, &p) || p.Value != cause || len(p.Stack) == 0 || !errors.Is(err, cause) {
			t.Fatalf("got %v", err)
		}
	})
}

func TestGroupCancelsOnTheFirstError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g, ctx := WithContext(context.Background())
		first := errors.New("first")
		g.Go(func() error { return first })
		g.Go(func() error {
			<-ctx.Done()
			return errors.New("second")
		})
		if err := g.Wait(); err != first {
			t.Fatalf("got %v", err)
		}
		if context.Cause(ctx) != first {
			t.Fatalf("cause %v", context.Cause(ctx))
		}
	})
}

func TestGroupPanicIsItsError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var g Group
		g.Go(func() error { panic("in a worker") })
		var p *PanicError
		if err := g.Wait(); !errors.As(err, &p) || p.Value != "in a worker" {
			t.Fatalf("got %v", err)
		}
	})
}

// The limit bounds how many run at once.
func TestGroupLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var g Group
		g.SetLimit(2)
		var running, most atomic.Int32
		for range 10 {
			g.Go(func() error {
				n := running.Add(1)
				for {
					m := most.Load()
					if n <= m || most.CompareAndSwap(m, n) {
						break
					}
				}
				time.Sleep(time.Second)
				running.Add(-1)
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			t.Fatal(err)
		}
		if most.Load() != 2 {
			t.Fatalf("%d ran at once", most.Load())
		}
		block := make(chan struct{})
		g.Go(func() error { <-block; return nil })
		g.Go(func() error { <-block; return nil })
		if g.TryGo(func() error { return nil }) {
			t.Fatal("TryGo ran past the limit")
		}
		close(block)
		if err := g.Wait(); err != nil {
			t.Fatal(err)
		}
		if !g.TryGo(func() error { return nil }) {
			t.Fatal("TryGo with room")
		}
		_ = g.Wait()
	})
}
