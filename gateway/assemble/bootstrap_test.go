package assemble

import (
	"strings"
	"testing"

	"atlas.local/gateway/edge"
	"atlas.local/gateway/fakes"
	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/kit/clock"
)

// R125, §9.3 rule 1, AP-DOC-001: a gateway whose settings would serve a
// bootstrap that fails its schema does not start, and its error names every
// member that fails; before, it started and served a bootstrap every client
// refuses, while /healthz reported it healthy.
func TestR125ABootstrapThatFailsItsSchemaDoesNotStart(t *testing.T) {
	good := func() Parts {
		// Only the built-ins are served, so the policy grants nothing else.
		pol := &ports.Policy{Revision: "r125"}
		return Parts{
			Environment: "dv", URL: "https://atlas-dv.example.com",
			SignIn: edge.SignIn{
				Authority: "https://login.microsoftonline.com/t1/v2.0", ClientID: "atlas-cli", Scopes: []string{"api://atlas/.default"},
				TokenEndpoint: "https://login.microsoftonline.com/t1/oauth2/v2.0/token",
			},
			Authenticator: fakes.NewTokens(clock.System), Policy: fakes.NewPolicy(pol), Audit: &fakes.Audit{},
		}
	}
	if _, err := New(good()); err != nil {
		t.Fatalf("good settings: %v", err)
	}
	for name, c := range map[string]struct {
		change func(*Parts)
		names  []string
	}{
		"an environment that is not a name": {func(p *Parts) { p.Environment = "Dev" }, []string{"/environment"}},
		"no environment":                    {func(p *Parts) { p.Environment = "" }, []string{"/environment"}},
		"a URL with no scheme":              {func(p *Parts) { p.URL = "atlas.example.com/" }, []string{"/servers/gateway/url"}},
		"no sign-in":                        {func(p *Parts) { p.SignIn = edge.SignIn{TokenEndpoint: "https://login.example.com/token"} }, []string{"/sign_in/authority", "/sign_in/client_id", "/sign_in/scopes"}},
		"an empty scope":                    {func(p *Parts) { p.SignIn.Scopes = []string{""} }, []string{"/sign_in/scopes/0"}},
		"a token endpoint that is no URI":   {func(p *Parts) { p.SignIn.TokenEndpoint = "token endpoint" }, []string{"/sign_in/token_endpoint"}},
		"everything at once": {func(p *Parts) {
			p.Environment, p.URL, p.SignIn.ClientID = "Dev", "atlas", ""
		}, []string{"/environment", "/servers/gateway/url", "/sign_in/client_id"}},
	} {
		t.Run(name, func(t *testing.T) {
			p := good()
			c.change(&p)
			_, err := New(p)
			if err == nil {
				t.Fatal("assembled")
			}
			for _, n := range c.names {
				if !strings.Contains(err.Error(), n) {
					t.Errorf("the error does not name %s: %v", n, err)
				}
			}
		})
	}
}
