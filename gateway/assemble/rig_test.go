package assemble

import (
	"testing"
	"time"

	"github.com/jhgonline/fi-app-atlas/gateway/core/pipeline"
	"github.com/jhgonline/fi-app-atlas/gateway/edge"
	"github.com/jhgonline/fi-app-atlas/gateway/fakes"
	"github.com/jhgonline/fi-app-atlas/gateway/fixtures"
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/clock"
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
