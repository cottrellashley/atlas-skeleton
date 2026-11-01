package fakes_test

import (
	"errors"
	"testing"

	"atlas.local/gateway/fakes"
	"atlas.local/gateway/ports"
	"atlas.local/gateway/porttest"
)

type policyHarness struct{ src *fakes.Policy }

func (h policyHarness) Publish(t *testing.T, p *ports.Policy) { h.src.Set(p) }
func (h policyHarness) Break(t *testing.T)                    { h.src.Fail(errors.New("the control store is down")) }

// R057, R073, AGENTS.md (every implementation runs its port's suite): the
// fake keeps the PolicySource contract the file adapter does, so a test on
// the fake proves what the adapter would do.
func TestPolicy(t *testing.T) {
	porttest.PolicySource(t, func(t *testing.T, initial *ports.Policy) (ports.PolicySource, porttest.PolicyHarness) {
		src := fakes.NewPolicy(initial)
		return src, policyHarness{src}
	})
}
