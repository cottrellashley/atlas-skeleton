package fakes

import (
	"context"
	"errors"
	"sync"

	"atlas.local/gateway/ports"
)

// Policy is a PolicySource that answers the policy it was given, or the
// failure it was told to give. It keeps the port's contract as the file
// adapter does (porttest.PolicySource): a policy with no revision, or none
// at all, is an error, never an answer.
type Policy struct {
	mu  sync.RWMutex
	p   *ports.Policy
	err error
}

// NewPolicy answers p.
func NewPolicy(p *ports.Policy) *Policy { return &Policy{p: p} }

// Set replaces the policy: a new revision. The caller does not change p
// afterwards, as a store never changes a policy it has answered.
func (s *Policy) Set(p *ports.Policy) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.p, s.err = p, nil
}

// Fail makes every read fail with err, as a control store that is down.
func (s *Policy) Fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

// Policy is the current policy.
func (s *Policy) Policy(ctx context.Context) (*ports.Policy, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	switch {
	case s.err != nil:
		return nil, s.err
	case s.p == nil:
		return nil, errors.New("fakes: the store holds no policy")
	case s.p.Revision == "":
		return nil, errors.New("fakes: the policy names no revision")
	}
	return s.p, nil
}
