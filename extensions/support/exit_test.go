package support_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"example.com/acme/atlas-extensions/support"
	"example.com/acme/atlas-extensions/support/fakedesk"

	"atlas.local/gateway/assemble"
	"atlas.local/gateway/edge"
	"atlas.local/gateway/fakes"
	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/kit/clock"
	"atlas.local/sdk/go/protocol"
)

// acme is Acme's policy: two teams of support agents, each scoped to its
// own workspace, and a grant of support.tickets by name.
func acme() *ports.Policy {
	return &ports.Policy{
		Revision: "acme-1",
		Roles:    map[string][]string{"eu-agents": {"support-agent"}, "us-agents": {"support-agent"}, "new-agents": {"support-agent"}},
		Grants:   []ports.Grant{{ID: "g-support", Role: "support-agent", Capabilities: []string{"support.tickets@v1"}, Interactions: []string{"fetch"}}},
		Scopes: map[string]ports.Scope{
			"eu-agents": {"workspace": {"acme-eu"}},
			"us-agents": {"workspace": {"acme-us"}},
		},
	}
}

// gateway is Atlas's standard assembly with Acme's module and dimension,
// over the fake desk: what Acme's composition root does, with fakes for
// identity and audit.
func gateway(t *testing.T, desk *fakedesk.Desk, policy *ports.Policy) (*httptest.Server, error) {
	t.Helper()
	tokens := fakes.NewTokens(clock.System)
	tokens.Issue("t-eve", ports.Identity{Principal: "e0e00000-0000-4000-8000-000000000001", Name: "Eve", Kind: "person", Groups: []string{"eu-agents"}}, time.Time{})
	tokens.Issue("t-uma", ports.Identity{Principal: "a0a00000-0000-4000-8000-000000000002", Name: "Uma", Kind: "person", Groups: []string{"us-agents"}}, time.Time{})
	tokens.Issue("t-nobody", ports.Identity{Principal: "b0b00000-0000-4000-8000-000000000003", Name: "Nobody", Kind: "person"}, time.Time{})
	tokens.Issue("t-new", ports.Identity{Principal: "c0c00000-0000-4000-8000-000000000004", Name: "New", Kind: "person", Groups: []string{"new-agents"}}, time.Time{})
	m, err := support.Module(support.Config{Desk: desk.URL, Concurrency: 4})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(nil)
	url := "http://" + srv.Listener.Addr().String()
	g, err := assemble.New(assemble.Parts{
		Environment: "local", URL: url,
		SignIn:        edge.SignIn{Authority: url, ClientID: "acme-cli", Scopes: []string{"api://acme-atlas/.default"}, TokenEndpoint: url + "/oauth/token"},
		Authenticator: tokens, Policy: fakes.NewPolicy(policy), Audit: &fakes.Audit{},
		Log:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		Modules:    []ports.Module{m},
		Dimensions: []ports.Dimension{support.Workspace},
	})
	if err != nil {
		srv.Close()
		return nil, err
	}
	t.Cleanup(func() { _ = g.Close() })
	srv.Config.Handler = g.Edge
	srv.Start()
	t.Cleanup(srv.Close)
	return srv, nil
}

type answer struct {
	status int
	body   []byte
}

func call(t *testing.T, srv *httptest.Server, token, path, body string) answer {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(protocol.HeaderClient, `name="acme-exit-test", version="1.0.0", protocol="atlas/1.0"`)
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return answer{res.StatusCode, b}
}

func (a answer) code() string {
	var p struct{ Code string }
	_ = json.Unmarshal(a.body, &p)
	return p.Code
}

const tickets = "/v1/data/support.tickets@v1:fetch"

// workspaces are the workspaces of an answer's tickets, and their ids.
func ids(t *testing.T, a answer) (ids, workspaces []string) {
	t.Helper()
	if a.status != http.StatusOK {
		t.Fatalf("%d %s", a.status, a.body)
	}
	var rows []struct{ ID, Workspace string }
	if err := json.Unmarshal(a.body, &rows); err != nil {
		t.Fatalf("%v: %s", err, a.body)
	}
	for _, r := range rows {
		ids = append(ids, r.ID)
		if !slices.Contains(workspaces, r.Workspace) {
			workspaces = append(workspaces, r.Workspace)
		}
	}
	return ids, workspaces
}

// C1's exit test (HANDOFF.md §5 C1): from a separate company module, Acme
// registers support.tickets, served by a fake HTTP desk and scoped by a new
// workspace dimension, through Atlas's public interface alone. The standard
// assembly starts with a policy that already names the capability; two
// callers granted it both discover it; each sees only their own workspaces;
// an out-of-scope request is refused before the desk is asked; and the core
// still holds the desk's answer to the scope when the desk ignores it.
func TestC1ACompanyModuleScopedByItsOwnDimension(t *testing.T) {
	desk := fakedesk.New(t)
	srv, err := gateway(t, desk, acme())
	if err != nil {
		t.Fatalf("the standard assembly with a policy naming support.tickets: %v", err)
	}

	for _, who := range []string{"t-eve", "t-uma"} {
		cat := call(t, srv, who, "/v1/atlas/atlas.catalog:fetch", `{}`)
		if cat.status != http.StatusOK || !strings.Contains(string(cat.body), `"support.tickets"`) {
			t.Fatalf("%s does not discover support.tickets: %d %s", who, cat.status, cat.body)
		}
	}
	if cat := call(t, srv, "t-nobody", "/v1/atlas/atlas.catalog:fetch", `{}`); strings.Contains(string(cat.body), `"support.tickets"`) {
		t.Fatalf("a caller with no grant discovers support.tickets: %s", cat.body)
	}

	got, ws := ids(t, call(t, srv, "t-eve", tickets, `{}`))
	if !slices.Equal(got, []string{"T-1001", "T-1003"}) || !slices.Equal(ws, []string{"acme-eu"}) {
		t.Fatalf("Eve sees %v in %v", got, ws)
	}
	got, ws = ids(t, call(t, srv, "t-uma", tickets, `{"status": "open"}`))
	if !slices.Equal(got, []string{"T-1005"}) || !slices.Equal(ws, []string{"acme-us"}) {
		t.Fatalf("Uma sees %v in %v", got, ws)
	}
	if asked := desk.Asked(); len(asked) != 2 || !slices.Equal(asked[0], []string{"acme-eu"}) || !slices.Equal(asked[1], []string{"acme-us"}) {
		t.Fatalf("the provider did not narrow the desk's reads to each caller's scope: %v", asked)
	}

	before := len(desk.Asked())
	if out := call(t, srv, "t-eve", tickets, `{"workspaces": ["acme-us"]}`); out.status != http.StatusForbidden || out.code() != "not_granted" {
		t.Fatalf("Eve asking for acme-us: %d %s", out.status, out.body)
	}
	if len(desk.Asked()) != before {
		t.Fatal("an out-of-scope request reached the desk")
	}

	who := call(t, srv, "t-eve", "/v1/atlas/atlas.whoami:fetch", `{}`)
	var me struct{ Scope map[string][]string }
	if err := json.Unmarshal(who.body, &me); err != nil || !slices.Equal(me.Scope["workspace"], []string{"acme-eu"}) || me.Scope["portfolio"] == nil {
		t.Fatalf("whoami's scope: %s", who.body)
	}

	// A desk that ignores the filter: the core's own result check withholds
	// the answer, and no other workspace's ticket reaches Eve.
	desk.IgnoreFilter(true)
	leak := call(t, srv, "t-eve", tickets, `{}`)
	if strings.Contains(string(leak.body), "acme-us") || strings.Contains(string(leak.body), "globex") || !strings.Contains(string(leak.body), `"internal"`) {
		t.Fatalf("a desk ignoring the scope was not withheld: %d %s", leak.status, leak.body)
	}
	desk.IgnoreFilter(false)

	// Granted, with no scope in the dimension: nothing to read, and the
	// desk is not asked. Registering the dimension granted nothing.
	before = len(desk.Asked())
	if got, _ := ids(t, call(t, srv, "t-new", tickets, `{}`)); len(got) != 0 || len(desk.Asked()) != before {
		t.Fatalf("a caller with no workspace scope sees %v (desk asked %d times)", got, len(desk.Asked())-before)
	}
	// Not granted: the capability is not there for them.
	if out := call(t, srv, "t-nobody", tickets, `{}`); out.status != http.StatusNotFound && out.status != http.StatusForbidden {
		t.Fatalf("a caller with no grant: %d %s", out.status, out.body)
	}

	desk.Fail(http.StatusBadGateway)
	if down := call(t, srv, "t-eve", tickets, `{}`); down.status != http.StatusServiceUnavailable || down.code() != "desk_unavailable" {
		t.Fatalf("a failing desk: %d %s", down.status, down.body)
	}
}

// C1, A-200: Acme's policy may scope only in a dimension the deployment
// defines; one it does not define stops the gateway at start, so a grant
// is never made that nothing enforces.
func TestC1APolicyInAnUndefinedDimensionStopsTheStart(t *testing.T) {
	p := acme()
	p.Scopes["eu-agents"]["desk"] = []string{"emea"}
	if _, err := gateway(t, fakedesk.New(t), p); err == nil || !strings.Contains(err.Error(), `"desk" is no dimension the deployment defines`) {
		t.Fatalf("a policy scoping an undefined dimension: %v", err)
	}
}
