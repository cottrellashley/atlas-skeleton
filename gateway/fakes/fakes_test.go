package fakes_test

import (
	"testing"
	"time"

	"github.com/jhgonline/fi-app-atlas/gateway/fakes"
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/gateway/porttest"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/clock"
)

type tokenIssuer struct{ tokens *fakes.Tokens }

func (i tokenIssuer) Valid(t *testing.T, id ports.Identity) string {
	i.tokens.Issue("valid-"+id.Principal, id, time.Now().Add(time.Hour))
	return "valid-" + id.Principal
}
func (i tokenIssuer) Expired(t *testing.T, id ports.Identity) string {
	i.tokens.Issue("expired-"+id.Principal, id, time.Now().Add(-time.Minute))
	return "expired-" + id.Principal
}
func (i tokenIssuer) Forged(t *testing.T, id ports.Identity) string { return "forged-" + id.Principal }

func TestTokens(t *testing.T) {
	tokens := fakes.NewTokens(clock.System)
	porttest.Authenticator(t, tokens, tokenIssuer{tokens})
}
