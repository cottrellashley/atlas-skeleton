package assemble

import (
	"testing"
	"time"

	"atlas.local/gateway/core/pipeline"
	"atlas.local/gateway/edge"
	"atlas.local/gateway/fakes"
	"atlas.local/gateway/fixtures"
	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/kit/clock"
)

func portsLimit(subject, capability string, concurrency int) ports.Limit {
	return ports.Limit{Subject: subject, Capability: capability, Concurrency: concurrency}
}

// mustPipeline rebuilds the rig's pipeline over another policy source, and
// serves it.
func mustPipeline(t *testing.T, r *rig, src ports.PolicySource) *pipeline.Pipeline {
	t.Helper()
	ids, _ := fixtures.Identities()
	tokens := fakes.NewTokens(clock.System)
	for _, id := range ids {
		tokens.Issue(id.Token, id.Identity, timeZero)
	}
	p, err := pipeline.New(pipeline.Options{Environment: "local", Registry: r.gw.Registry, Authenticator: tokens, Policy: src, Audit: r.audit, Clock: r.clock})
	if err != nil {
		t.Fatal(err)
	}
	r.srv.Config.Handler = edge.New(edge.Options{Pipeline: p, URL: r.srv.URL, Clock: r.clock})
	return p
}

var timeZero time.Time
