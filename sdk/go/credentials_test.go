package atlas

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/jhgonline/fi-app-atlas/sdk/go/engine/fakegw"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/conc"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/documents"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
)

// R26, PR01, R004, R008: the refuter's rogue gateway. A person signed in
// to dv. Another gateway's unauthenticated bootstrap claims environment
// dv and names itself as server, authority and token endpoint, and it
// refuses the first call with invalid_token so that a client would refresh.
// It receives neither the access token nor the refresh token, and the call
// is unauthenticated.
func TestPR01SavedCredentialsGoToNoGatewayThatOnlyClaimsTheEnvironment(t *testing.T) {
	isolate(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name    string
		signIn  func(t *testing.T, home string, net *fakegw.Network)
		trusted bool
	}{
		{"an unbound sign-in, as SignIn writes", func(t *testing.T, home string, _ *fakegw.Network) {
			if err := SignIn(home, "dv", "REAL-DV-ACCESS", "REAL-DV-REFRESH", time.Hour); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"a sign-in bound to the real dv gateway, as atlas login writes", func(t *testing.T, home string, net *fakegw.Network) {
			real := fakegw.New(t, fakegw.Options{Host: "127.0.0.2:8443", Environment: "dv", Network: net})
			real.Tokens([]string{"REAL-DV-ACCESS"}, nil)
			c := connect(t, real, home)
			if err := c.SignIn("REAL-DV-ACCESS", "REAL-DV-REFRESH", time.Hour); err != nil {
				t.Fatal(err)
			}
			if _, err := c.Whoami(ctx); err != nil {
				t.Fatalf("the real gateway refused its own sign-in: %v", err)
			}
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			net := fakegw.NewNetwork(t)
			tc.signIn(t, home, net)
			rogue := fakegw.New(t, fakegw.Options{Environment: "dv", Network: net})
			rogue.Tokens([]string{"rogue-minted"}, map[string]fakegw.Grant{"REAL-DV-REFRESH": {AccessToken: "rogue-minted", RefreshToken: "rogue-next", ExpiresIn: 3600}})
			c := connect(t, rogue, home)
			_, err := c.Whoami(ctx)
			wantProblem(t, err, "unauthenticated")
			if carried(rogue.Requests(), "REAL-DV-ACCESS") || carried(rogue.Requests(), "REAL-DV-REFRESH") {
				t.Fatalf("the rogue gateway received the saved credential: %+v", rogue.Requests())
			}
			if n := len(rogue.To("/oauth/token")); n != 0 {
				t.Fatalf("%d refreshes reached the rogue's token endpoint", n)
			}
			if n := len(rogue.To("atlas.whoami:fetch")); n != 0 {
				t.Fatalf("the rogue received %d calls", n)
			}
		})
	}
}

// R26, R010: a trusted gateway's bootstrap cannot redirect a credential.
// When it names a server on another origin, nothing is sent there.
func TestR010ABootstrapCannotSendACredentialToAnotherServer(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	net := fakegw.NewNetwork(t)
	trusted := fakegw.New(t, fakegw.Options{Environment: "dv", Network: net})
	trusted.Tokens([]string{"dv-access"}, nil)
	elsewhere := fakegw.New(t, fakegw.Options{Environment: "dv", Network: net})
	c := connect(t, trusted, home)
	if err := c.SignIn("dv-access", "dv-refresh", time.Hour); err != nil {
		t.Fatal(err)
	}
	trusted.Bootstrap(func(b *ijson.Object) {
		servers, _ := b.Get("servers")
		gw, _ := servers.(*ijson.Object).Get("gateway")
		gw.(*ijson.Object).Set("url", elsewhere.URL)
	})
	c = connect(t, trusted, home)
	_, err := c.Whoami(context.Background())
	wantProblem(t, err, "unauthenticated")
	if n := len(elsewhere.Requests()); n != 0 {
		t.Fatalf("the other server received %d requests", n)
	}
	if carried(trusted.Requests(), "dv-access") {
		t.Fatal("the trusted gateway was sent the token after its bootstrap redirected the call")
	}
}

// R26: a token_endpoint is never taken from a bootstrap whose sign_in is not
// the one the credential was issued under: the authority, the client_id,
// the scopes, or the token endpoint itself changed.
func TestR26ASignInChangedSinceSigningInIsNotUsed(t *testing.T) {
	isolate(t)
	for _, member := range []string{"authority", "client_id", "token_endpoint", "scopes"} {
		t.Run(member, func(t *testing.T) {
			home := t.TempDir()
			net := fakegw.NewNetwork(t)
			gw := fakegw.New(t, fakegw.Options{Environment: "dv", Network: net})
			gw.Tokens([]string{"dv-access"}, map[string]fakegw.Grant{"dv-refresh": {AccessToken: "dv-next", ExpiresIn: 3600}})
			thief := fakegw.New(t, fakegw.Options{Environment: "dv", Network: net})
			c := connect(t, gw, home)
			if err := c.SignIn("dv-access", "dv-refresh", 30*time.Second); err != nil { // within a minute of its end: due for refresh
				t.Fatal(err)
			}
			gw.Bootstrap(func(b *ijson.Object) {
				si, _ := b.Get("sign_in")
				o := si.(*ijson.Object)
				switch member {
				case "scopes":
					o.Set("scopes", []ijson.Value{thief.URL + "/.default"})
				case "token_endpoint", "authority":
					o.Set(member, thief.URL+"/oauth/token")
				default:
					o.Set(member, "someone-else")
				}
			})
			c = connect(t, gw, home)
			_, err := c.Whoami(context.Background())
			wantProblem(t, err, "unauthenticated")
			if len(thief.Requests()) != 0 || len(gw.To("/oauth/token")) != 0 {
				t.Fatalf("a refresh was sent: thief %d, gateway %d", len(thief.Requests()), len(gw.To("/oauth/token")))
			}
			if carried(gw.Requests(), "dv-access") {
				t.Fatal("the token was sent under a changed sign-in")
			}
		})
	}
}

// R26: a credential never travels in the clear off this machine. atlas
// login refuses a plain-HTTP gateway elsewhere, and a sign-in bound to one
// is not sent to it.
func TestR26NoCredentialOverPlainHTTPOffThisMachine(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	gw := fakegw.New(t, fakegw.Options{Host: "atlas.example:80", Environment: "dv"})
	c := connect(t, gw, home)
	wantProblem(t, c.SignIn("dv-access", "", time.Hour), "invalid_request")
	b, _ := json.Marshal(tokenFile{AccessToken: "dv-access", ExpiresAt: time.Now().Add(time.Hour).Unix(), Binding: c.signInOf().binding(c.origin, "dv")})
	if err := writeFileAtomic(filepath.Join(home, "state", "token-dv.json"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := c.Whoami(context.Background())
	wantProblem(t, err, "unauthenticated")
	if carried(gw.Requests(), "dv-access") {
		t.Fatal("the token went over plain HTTP")
	}
}

// R26: the sign-in an earlier client or the conformance suite wrote, with
// no binding, still serves a development or conformance gateway on this
// machine (CORE-SIGNIN-001), and is refreshed only at that gateway's own
// token endpoint (CORE-SIGNIN-002).
func TestR26AnUnboundSignInServesOnlyALocalDevelopmentGateway(t *testing.T) {
	isolate(t)
	ctx := context.Background()
	home := t.TempDir()
	if err := SignIn(home, "conformance", "ct-old", "rt-1", 30*time.Second); err != nil {
		t.Fatal(err)
	}
	gw := fakegw.New(t, fakegw.Options{})
	gw.Tokens([]string{"ct-new"}, map[string]fakegw.Grant{"rt-1": {AccessToken: "ct-new", RefreshToken: "rt-2", ExpiresIn: 3600}})
	c := connect(t, gw, home)
	if _, err := c.Whoami(ctx); err != nil {
		t.Fatal(err)
	}
	if carried(gw.To("atlas.whoami:fetch"), "ct-old") || len(gw.To("/oauth/token")) != 1 {
		t.Fatalf("want one refresh before the call and the old token never sent: %+v", gw.Requests())
	}
	f, err := (&tokenStore{dir: filepath.Join(home, "state"), env: "conformance"}).read()
	if err != nil || f.AccessToken != "ct-new" || f.RefreshToken != "rt-2" {
		t.Fatalf("the rewritten file: %+v %v", f, err)
	}
	gw.Bootstrap(func(b *ijson.Object) {
		si, _ := b.Get("sign_in")
		si.(*ijson.Object).Set("token_endpoint", "http://127.0.0.9:1/oauth/token")
	})
	if err := SignIn(home, "conformance", "ct-old", "rt-2", 30*time.Second); err != nil {
		t.Fatal(err)
	}
	c = connect(t, gw, home)
	_, err = c.Whoami(ctx)
	wantProblem(t, err, "unauthenticated") // an unbound refresh token goes only to the gateway's own origin
}

// R26: refreshes are serialised between programs sharing the store. A
// program that finds another's lock waits, then reads the token the other
// wrote, and does not redeem the refresh token again.
func TestR26RefreshIsSerialisedBetweenPrograms(t *testing.T) {
	isolate(t)
	synctest.Test(t, func(t *testing.T) {
		home := t.TempDir()
		gw := fakegw.New(t, fakegw.Options{})
		gw.Tokens([]string{"ct-other"}, map[string]fakegw.Grant{"rt-1": {AccessToken: "ct-mine", RefreshToken: "rt-2", ExpiresIn: 3600}})
		if err := SignIn(home, "conformance", "ct-old", "rt-1", 30*time.Second); err != nil {
			t.Fatal(err)
		}
		lock := filepath.Join(home, "state", "token-conformance.json.lock")
		if err := os.WriteFile(lock, []byte("1\n"), 0o600); err != nil { // the other program is refreshing
			t.Fatal(err)
		}
		c := connect(t, gw, home)
		wait := conc.Go(func() error { _, err := c.Whoami(context.Background()); return err })
		synctest.Wait() // the call waits for the lock
		if err := SignIn(home, "conformance", "ct-other", "rt-other", time.Hour); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(lock); err != nil {
			t.Fatal(err)
		}
		if err := wait(); err != nil {
			t.Fatal(err)
		}
		if n := len(gw.To("/oauth/token")); n != 0 {
			t.Fatalf("the refresh token was redeemed %d times after the other program had renewed it", n)
		}
		if who := gw.To("atlas.whoami:fetch"); len(who) != 1 || who[0].Header.Get("Authorization") != "Bearer ct-other" {
			t.Fatalf("the call did not carry the other program's token: %+v", who)
		}
	})
}

// §9.7.3, token-store.schema.json: every sign-in written carries
// expires_at, and its binding beside it, in a 0600 file in a 0700
// directory.
func TestR26TheStoreIsValidAndPrivate(t *testing.T) {
	isolate(t)
	home := t.TempDir()
	gw := fakegw.New(t, fakegw.Options{})
	c := connect(t, gw, home)
	if err := c.SignIn("ct-valid", "rt-valid", 0); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "state", "token-conformance.json")
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("the file: %v %v", st, err)
	}
	if dir, _ := os.Stat(filepath.Dir(path)); dir.Mode().Perm() != 0o700 {
		t.Fatalf("the directory's mode is %v", dir.Mode().Perm())
	}
	b, _ := os.ReadFile(path)
	v, err := ijson.Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if fs := documents.Validate("token-store", v); len(fs) > 0 {
		t.Fatalf("the token file breaks token-store.schema.json: %v", fs)
	}
	var f tokenFile
	_ = json.Unmarshal(b, &f)
	if f.Binding == nil || f.Binding.Gateway != gw.URL || f.Binding.ClientID != "atlas-conformance" || f.Binding.Audience != gw.URL || f.Binding.TokenEndpoint != gw.URL+"/oauth/token" {
		t.Fatalf("the binding: %+v", f.Binding)
	}
}

// R26, CORE-CALL-003: a call goes to its binding and nowhere else. A
// redirect is not followed, so it cannot carry the token or the body to
// another place: the call is upstream_failed, and the redirect's target
// receives nothing.
func TestR26ARedirectIsNotFollowed(t *testing.T) {
	isolate(t)
	net := fakegw.NewNetwork(t)
	gw := fakegw.New(t, fakegw.Options{Network: net})
	target := fakegw.New(t, fakegw.Options{Network: net})
	gw.Handle("conformance.echo@v1:fetch", func(c *fakegw.Call) {
		c.W.Header().Set("Location", target.URL+"/v1/data/conformance.echo@v1:fetch")
		c.W.WriteHeader(http.StatusTemporaryRedirect)
	})
	c := connect(t, gw, signedIn(t))
	_, err := c.Call(context.Background(), "conformance.echo@v1:fetch", map[string]any{"text": "x"})
	wantProblem(t, err, "upstream_failed")
	if n := len(target.Requests()); n != 0 {
		t.Fatalf("the redirect's target received %d requests", n)
	}
}

// R3-03: a refresh names the application the sign-in was bound to, as
// Entra requires (client_id), with the scopes it was bound to. A token
// endpoint that redeems refresh tokens for that application renews the
// sign-in; one registered for another application refuses it, and the
// call is unauthenticated.
func TestR3_03RefreshNamesTheBoundApplication(t *testing.T) {
	isolate(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name, registered string
		renewed          bool
	}{
		{"the bound application", "atlas-conformance", true},
		{"another application", "someone-else", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			gw := fakegw.New(t, fakegw.Options{})
			gw.Tokens([]string{"ct-old"}, map[string]fakegw.Grant{"rt-1": {AccessToken: "ct-new", RefreshToken: "rt-2", ExpiresIn: 3600}})
			gw.Application(tc.registered)
			c := connect(t, gw, home)
			if err := c.SignIn("ct-old", "rt-1", 30*time.Second); err != nil {
				t.Fatal(err)
			}
			_, err := c.Whoami(ctx)
			if !tc.renewed {
				wantProblem(t, err, "unauthenticated")
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			sent := gw.To("/oauth/token")
			if len(sent) != 1 {
				t.Fatalf("%d refreshes", len(sent))
			}
			form, err := url.ParseQuery(string(sent[0].Body))
			if err != nil {
				t.Fatal(err)
			}
			if form.Get("client_id") != "atlas-conformance" || form.Get("scope") != gw.URL+"/.default" || form.Get("grant_type") != "refresh_token" || form.Get("refresh_token") != "rt-1" {
				t.Fatalf("the refresh form: %v", form)
			}
		})
	}
}
