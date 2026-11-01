package assemble

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jhgonline/fi-app-atlas/gateway/adapter/policyfile"
	"github.com/jhgonline/fi-app-atlas/gateway/edge"
	"github.com/jhgonline/fi-app-atlas/gateway/fakes"
	"github.com/jhgonline/fi-app-atlas/gateway/fixtures"
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/gateway/svc/datastatus"
	"github.com/jhgonline/fi-app-atlas/gateway/svc/portfolio"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/clock"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
)

// policyRig is the journey's gateway over a policy source the test
// chooses, assembled as a deployable assembles it.
func policyRig(t *testing.T, src ports.PolicySource, exclude ...string) (*rig, error) {
	t.Helper()
	tables, err := fixtures.Harbour()
	if err != nil {
		t.Fatal(err)
	}
	ids, err := fixtures.Identities()
	if err != nil {
		t.Fatal(err)
	}
	r := &rig{t: t, audit: &fakes.Audit{}, source: fakes.NewDataSource(tables...), clock: clock.NewManual(time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC))}
	tokens := fakes.NewTokens(r.clock)
	for _, id := range ids {
		tokens.Issue(id.Token, id.Identity, time.Time{})
	}
	r.srv = httptest.NewUnstartedServer(nil)
	url := "http://" + r.srv.Listener.Addr().String()
	r.gw, err = New(Parts{
		Environment: "local", URL: url,
		SignIn:        edge.SignIn{Authority: url, ClientID: "atlas-cli", Scopes: []string{"api://atlas/.default"}, TokenEndpoint: url + "/oauth/token"},
		Authenticator: tokens, Policy: src, Audit: r.audit, DataSource: r.source, Clock: r.clock,
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Domains: []Domain{portfolio.Capabilities, datastatus.Capabilities}, Exclude: exclude,
	})
	if err != nil {
		r.srv.Listener.Close()
		return nil, err
	}
	r.srv.Config.Handler = r.gw.Edge
	r.srv.Start()
	t.Cleanup(r.srv.Close)
	return r, nil
}

// R034, 06-3 rule 3: Dave's information barrier covers the holdings totals
// by date as well as the holdings; a family grant cannot hand him a later
// capability that exposes them.
func TestR034DaveIsDeniedTheHoldingsTotalsByDate(t *testing.T) {
	r := newRig(t)
	body := `{"portfolios": ["147HYC"]}`
	if code, _ := r.call("dev-dave", "/v1/data/portfolio.holdings-dates@v1:fetch", body).problem(t); code != protocol.CodeDenied {
		t.Fatalf("dave: %s", code)
	}
	if a := r.call("dev-alice", "/v1/data/portfolio.holdings-dates@v1:fetch", body); a.status != 200 {
		t.Fatalf("alice: %d %s", a.status, a.body)
	}
}

// R040, 06-7 §6.7.10: a gateway whose policy names what it does not serve
// does not start, and says what to fix; a capability the deployment
// withholds may already be named, so it is granted by name the moment it
// is added (06-3 rule 3, the acceptance result's fourth point).
func TestR040AGatewayWhosePolicyNamesNothingDoesNotStart(t *testing.T) {
	pol, err := fixtures.Policy()
	if err != nil {
		t.Fatal(err)
	}
	pol.Denies[0].Capabilities = []string{"portfolio.holding@v1"}
	if _, err := policyRig(t, fakes.NewPolicy(pol)); err == nil || !strings.Contains(err.Error(), "portfolio.holding@v1") {
		t.Fatalf("a misspelt deny: %v", err)
	}
	family, _ := fixtures.Policy()
	family.Grants[0].Capabilities = []string{"portfolio.*"}
	if _, err := policyRig(t, fakes.NewPolicy(family)); err == nil || !strings.Contains(err.Error(), "06-3 rule 3") {
		t.Fatalf("a family grant of gateway-identity capabilities: %v", err)
	}
	withheld, _ := fixtures.Policy()
	if _, err := policyRig(t, fakes.NewPolicy(withheld), "portfolio.holdings-dates@v1"); err != nil {
		t.Fatalf("the fixture names a capability the deployment withholds: %v", err)
	}
}

// R040, R13: a new revision that fails the check never decides. The last
// one that passed keeps deciding, so Dave stays denied, until the bound,
// and then the gateway refuses rather than decide by a policy it cannot
// renew.
func TestR040ARevisionThatFailsNeverDecides(t *testing.T) {
	pol, _ := fixtures.Policy()
	src := fakes.NewPolicy(pol)
	r, err := policyRig(t, src)
	if err != nil {
		t.Fatal(err)
	}
	holdings := `{"portfolios": ["147HYC"]}`
	if code, _ := r.call("dev-dave", "/v1/data/portfolio.holdings@v1:fetch", holdings).problem(t); code != protocol.CodeDenied {
		t.Fatalf("dave: %s", code)
	}
	misspelt, _ := fixtures.Policy()
	misspelt.Revision = "dev-2026-11-01.1"
	misspelt.Denies[0].Capabilities = []string{"portfolio.holding@v1"}
	src.Set(misspelt)
	for range 12 {
		r.clock.Advance(5 * time.Second)
		if code, _ := r.call("dev-dave", "/v1/data/portfolio.holdings@v1:fetch", holdings).problem(t); code != protocol.CodeDenied {
			t.Fatalf("a misspelt deny opened access: %s", code)
		}
	}
	r.clock.Advance(5 * time.Second)
	for range 3 {
		if code, _ := r.call("dev-alice", "/v1/data/portfolio.list@v1:fetch", `{}`).problem(t); code != protocol.CodeUnavailable {
			t.Fatalf("past its bound: %s", code)
		}
	}
	if s := r.gw.Pipeline.PolicyStatus(); s.Revision != "dev-2026-10-31.1" || s.Deciding || s.Err == nil || !strings.Contains(s.Err.Error(), "portfolio.holding@v1") {
		t.Fatalf("status %+v", s)
	}
}

// PR02, R014, R039, R13: the shipped policy-file adapter under the real
// pipeline. A file that is deleted, malformed or refused keeps the last
// good policy deciding only within the bound; then calls are refused, and
// the status says why.
func TestPR02APolicyFileThatBreaksIsBounded(t *testing.T) {
	for name, damage := range map[string]func(path string) error{
		"deleted": os.Remove,
		"malformed": func(path string) error {
			return os.WriteFile(path, []byte(`{"revision": "r-revoke", "grants": [],}`), 0o600)
		},
		"refused": func(path string) error {
			return os.WriteFile(path, []byte(`{"revision": "r-revoke", "grantz": []}`), 0o600)
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "policy.json")
			if err := os.WriteFile(path, fixtures.PolicyJSON(), 0o600); err != nil {
				t.Fatal(err)
			}
			src, err := policyfile.New(path)
			if err != nil {
				t.Fatal(err)
			}
			r, err := policyRig(t, src)
			if err != nil {
				t.Fatal(err)
			}
			list := func() answer { return r.call("dev-alice", "/v1/data/portfolio.list@v1:fetch", `{}`) }
			if a := list(); a.status != 200 {
				t.Fatalf("%d %s", a.status, a.body)
			}
			if err := damage(path); err != nil {
				t.Fatal(err)
			}
			r.clock.Advance(30 * time.Second)
			if a := list(); a.status != 200 || a.header.Get(protocol.HeaderGovernanceRevision) != `"dev-2026-10-31.1"` {
				t.Fatalf("within its bound the last good policy decides: %d %s", a.status, a.body)
			}
			r.clock.Advance(31 * time.Second)
			if code, _ := list().problem(t); code != protocol.CodeUnavailable {
				t.Fatalf("past its bound: %s", code)
			}
			if s := r.gw.Pipeline.PolicyStatus(); s.Deciding || s.Err == nil || !strings.Contains(s.Err.Error(), "policy") {
				t.Fatalf("status %+v", s)
			}
		})
	}
}
