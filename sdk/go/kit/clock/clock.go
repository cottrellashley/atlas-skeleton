// Package clock gives code the time through an interface, so a test can
// hold it still or move it on (AGENTS.md: inject the clock; never sleep to
// wait for an expiry). Code that needs timers uses the standard library's,
// and its tests run under testing/synctest, whose bubbles give them a fake
// clock of their own.
package clock

import (
	"sync"
	"time"
)

// Clock tells the time.
type Clock interface {
	Now() time.Time
}

// System is the machine's clock.
var System Clock = system{}

type system struct{}

func (system) Now() time.Time { return time.Now() }

// Func is a function used as a Clock.
type Func func() time.Time

// Now calls the function.
func (f Func) Now() time.Time { return f() }

// Manual is a clock that moves only when it is told to. The zero value
// stands at the zero time.
type Manual struct {
	mu sync.Mutex
	t  time.Time
}

// NewManual is a Manual clock at t.
func NewManual(t time.Time) *Manual { return &Manual{t: t} }

// Now is the clock's time.
func (m *Manual) Now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.t
}

// Set moves the clock to t.
func (m *Manual) Set(t time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.t = t
}

// Advance moves the clock on by d and returns the new time.
func (m *Manual) Advance(d time.Duration) time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.t = m.t.Add(d)
	return m.t
}
