package pipeline

import (
	"sync"

	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/conc"
)

// settler runs settlements (ATLAS-CORE §8 step 9: usage settled from the
// source's own record) after their answers, off every call's path, at most
// a bounded number at once: a slow source never holds a caller's answer,
// nor more of the replica than its share (06-7 §6.7.15's settlement loop).
type settler struct {
	mu     sync.Mutex
	group  conc.Group
	closed bool
}

func newSettler(n int) *settler {
	s := &settler{}
	s.group.SetLimit(n)
	return s
}

// start runs fn, and reports false when it cannot: the settler is
// draining, or full.
func (s *settler) start(fn func()) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed && s.group.TryGo(func() error {
		fn()
		return nil
	})
}

// drain stops new settlements and waits for those running. Each is bounded
// by SettleTimeout, and so is the wait. Its error is a settlement's panic
// that escaped every recover, which a settlement's own fact cannot hold.
func (s *settler) drain() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return s.group.Wait()
}
