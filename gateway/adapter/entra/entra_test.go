package entra

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"atlas.local/gateway/ports"
	"atlas.local/gateway/porttest"
	"atlas.local/sdk/go/kit/conc"
	"atlas.local/sdk/go/kit/jwt"
)

const issuer = "https://login.microsoftonline.com/t1/v2.0"

var (
	keyOnce    sync.Once
	key, other *rsa.PrivateKey
)

// keys are two RSA keys, made once: the tenant's, and one it never issued.
func keys(t *testing.T) (*rsa.PrivateKey, *rsa.PrivateKey) {
	keyOnce.Do(func() {
		key, _ = rsa.GenerateKey(rand.Reader, 2048)
		other, _ = rsa.GenerateKey(rand.Reader, 2048)
	})
	if key == nil || other == nil {
		t.Fatal("no RSA keys")
	}
	return key, other
}

// issuer_ signs tokens as Entra does, with kid k1 unless told otherwise.
type issuer_ struct {
	key, other *rsa.PrivateKey
}

func (i issuer_) sign(t *testing.T, key *rsa.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signing := enc(map[string]any{"alg": "RS256", "kid": kid, "typ": "JWT"}) + "." + enc(claims)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func (i issuer_) claims(aud string, id ports.Identity, exp time.Time) map[string]any {
	c := map[string]any{"iss": issuer, "aud": aud, "exp": exp.Unix(), "iat": time.Now().Add(-time.Minute).Unix(), "nbf": time.Now().Add(-time.Minute).Unix(),
		"oid": id.Principal, "tid": id.Tenant, "name": id.Name, "azp": id.Application, "groups": id.Groups}
	if id.Kind == "workload" {
		c["idtyp"] = "app"
	}
	return c
}

func (i issuer_) token(t *testing.T, key *rsa.PrivateKey, aud string, id ports.Identity, exp time.Time) string {
	return i.sign(t, key, "k1", i.claims(aud, id, exp))
}

func (i issuer_) Valid(t *testing.T, id ports.Identity) string {
	return i.token(t, i.key, "api://atlas", id, time.Now().Add(time.Hour))
}
func (i issuer_) Expired(t *testing.T, id ports.Identity) string {
	return i.token(t, i.key, "api://atlas", id, time.Now().Add(-time.Minute))
}
func (i issuer_) Forged(t *testing.T, id ports.Identity) string {
	return i.token(t, i.other, "api://atlas", id, time.Now().Add(time.Hour))
}

func TestAuthenticator(t *testing.T) {
	key, other := keys(t)
	a, err := New(Options{Issuer: issuer, Audiences: []string{"api://atlas"}, Keys: jwt.StaticKeys{"k1": &key.PublicKey}, Tenant: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	iss := issuer_{key: key, other: other}
	porttest.Authenticator(t, a, iss)
	wrongAudience := iss.token(t, key, "api://other", porttest.People[0], time.Now().Add(time.Hour))
	if _, err := a.Authenticate(t.Context(), wrongAudience); err == nil {
		t.Fatal("a token for another audience proved an identity")
	}
}

// The authenticator suite over the tenant's real key endpoint shape: the
// kit's KeySet fetching a JWKS document.
func TestAuthenticatorOverAKeySet(t *testing.T) {
	key, other := keys(t)
	end := newEndpoint(map[string]*rsa.PublicKey{"k1": &key.PublicKey})
	a, err := New(Options{Issuer: issuer, Audiences: []string{"api://atlas"}, Keys: end.keySet(), Tenant: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	porttest.Authenticator(t, a, issuer_{key: key, other: other})
}

// R013, R064: when the tenant's keys cannot be fetched (the endpoint answers
// 503, or the connection is refused), a valid token is the gateway's
// problem, never the caller's: neither unauthenticated nor expired, so the
// pipeline answers unavailable with Retry-After instead of "sign in again"
// (ports/identity.go). A replica that starts while Entra is unreachable is
// the case.
func TestR013AKeyOutageIsNeverTheCallersFault(t *testing.T) {
	key, other := keys(t)
	iss := issuer_{key: key, other: other}
	for name, fault := range map[string]func(*endpoint){
		"the endpoint answers 503":        func(e *endpoint) { e.status = http.StatusServiceUnavailable },
		"the connection is refused":       func(e *endpoint) { e.down = errors.New("dial tcp 20.190.0.1:443: connect: connection refused") },
		"the endpoint answers no keys":    func(e *endpoint) { e.keys = nil },
		"the endpoint answers garbage":    func(e *endpoint) { e.garbage = true },
		"the endpoint answers a 404 page": func(e *endpoint) { e.status = http.StatusNotFound },
	} {
		t.Run(name, func(t *testing.T) {
			end := newEndpoint(map[string]*rsa.PublicKey{"k1": &key.PublicKey})
			fault(end)
			a, err := New(Options{Issuer: issuer, Audiences: []string{"api://atlas"}, Keys: end.keySet(), Tenant: "t1"})
			if err != nil {
				t.Fatal(err)
			}
			_, err = a.Authenticate(t.Context(), iss.Valid(t, porttest.People[0]))
			if err == nil || errors.Is(err, ports.ErrUnauthenticated) || errors.Is(err, ports.ErrTokenExpired) {
				t.Fatalf("a valid token while the keys cannot be fetched: %v", err)
			}
			if err := a.Check(t.Context()); err == nil {
				t.Fatal("ready without the tenant's keys")
			}
		})
	}
}

// R064, GATEWAY-012 (§6.1.16's Entra row): while Entra's key endpoint is
// down, calls with tokens already held keep working, verified with the keys
// the gateway holds, even after the key document's max-age has passed. A
// token under a key the gateway never held cannot be verified, which is the
// gateway's fault, not the caller's; a held key serves only for MaxStale
// after the endpoint last confirmed it; and a key the tenant stops listing
// is dropped once the endpoint answers without it.
func TestR064HeldTokensVerifyWhileTheKeysAreDown(t *testing.T) {
	key, other := keys(t)
	iss := issuer_{key: key, other: other}
	synctest.Test(t, func(t *testing.T) {
		end := newEndpoint(map[string]*rsa.PublicKey{"k1": &key.PublicKey})
		a, err := New(Options{Issuer: issuer, Audiences: []string{"api://atlas"}, Keys: end.keySet(), Tenant: "t1", MaxStale: 6 * time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		if err := a.Refresh(t.Context()); err != nil {
			t.Fatal(err)
		}
		k1 := func() string { return iss.Valid(t, porttest.People[0]) }
		k2 := func() string {
			return iss.sign(t, key, "k2", iss.claims("api://atlas", porttest.People[0], time.Now().Add(time.Hour)))
		}
		unavailable := func(what string, token string) {
			t.Helper()
			if _, err := a.Authenticate(t.Context(), token); err == nil || errors.Is(err, ports.ErrUnauthenticated) || errors.Is(err, ports.ErrTokenExpired) {
				t.Fatalf("%s: %v, want the gateway's own fault", what, err)
			}
		}
		if _, err := a.Authenticate(t.Context(), k1()); err != nil {
			t.Fatal(err)
		}
		end.fail(http.StatusServiceUnavailable)
		time.Sleep(2 * time.Hour) // the document's max-age (1 h) has passed
		if _, err := a.Authenticate(t.Context(), k1()); err != nil {
			t.Fatalf("a held token during the outage: %v", err)
		}
		if err := a.Check(t.Context()); err != nil {
			t.Fatalf("not ready while it can verify held tokens: %v", err)
		}
		unavailable("a token under a key never held, during the outage", k2())

		// The endpoint answers again, and has rotated k1 out for k2.
		end.heal(map[string]*rsa.PublicKey{"k2": &key.PublicKey})
		if _, err := a.Authenticate(t.Context(), k2()); err != nil {
			t.Fatalf("a token under the new key: %v", err)
		}
		if _, err := a.Authenticate(t.Context(), k1()); !errors.Is(err, ports.ErrUnauthenticated) {
			t.Fatalf("a key the tenant stopped listing, while it answers: %v", err)
		}
		end.fail(http.StatusServiceUnavailable)
		time.Sleep(2 * time.Hour)
		unavailable("a key the tenant stopped listing, in a later outage", k1())
		if _, err := a.Authenticate(t.Context(), k2()); err != nil {
			t.Fatalf("a held token in the later outage: %v", err)
		}
		time.Sleep(5 * time.Hour) // k2 was last confirmed 7 h ago
		unavailable("a held key past MaxStale", k2())
	})
}

// R064: a token naming a key the tenant does not list, while the endpoint
// answers, is not proof of anything: unauthenticated.
func TestR064AnUnknownKeyIsStillUnauthenticated(t *testing.T) {
	key, other := keys(t)
	iss := issuer_{key: key, other: other}
	end := newEndpoint(map[string]*rsa.PublicKey{"k1": &key.PublicKey})
	a, err := New(Options{Issuer: issuer, Audiences: []string{"api://atlas"}, Keys: end.keySet(), Tenant: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	bogus := iss.sign(t, other, "k9", iss.claims("api://atlas", porttest.People[0], time.Now().Add(time.Hour)))
	if _, err := a.Authenticate(t.Context(), bogus); !errors.Is(err, ports.ErrUnauthenticated) {
		t.Fatalf("a token under a key the tenant does not list: %v", err)
	}
}

// R013: the authenticator is ready once it holds the tenant's keys, and its
// readiness fetches them until it does (§6.7.4: fetched at start).
func TestR013ReadyOnceTheKeysAreFetched(t *testing.T) {
	key, _ := keys(t)
	end := newEndpoint(map[string]*rsa.PublicKey{"k1": &key.PublicKey})
	end.fail(http.StatusBadGateway)
	a, err := New(Options{Issuer: issuer, Audiences: []string{"api://atlas"}, Keys: end.keySet(), Tenant: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Check(t.Context()); err == nil {
		t.Fatal("ready with no keys")
	}
	end.heal(map[string]*rsa.PublicKey{"k1": &key.PublicKey})
	if err := a.Check(t.Context()); err != nil {
		t.Fatalf("not ready once the keys can be fetched: %v", err)
	}
}

// R068: a person in more groups than a token can carry (group overage) is
// never read as a person in no groups. Their token names Graph instead of
// listing the groups (_claim_names, or hasgroups); without a group source
// the authenticator refuses it as its own limitation, never as no groups
// and never as the caller's fault.
func TestR068GroupOverageIsNeverReadAsNoGroups(t *testing.T) {
	key, other := keys(t)
	iss := issuer_{key: key, other: other}
	a, err := New(Options{Issuer: issuer, Audiences: []string{"api://atlas"}, Keys: jwt.StaticKeys{"k1": &key.PublicKey}, Tenant: "t1", Log: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	for name, token := range overageTokens(t, iss) {
		_, err := a.Authenticate(t.Context(), token)
		if !errors.Is(err, ErrGroupOverage) || errors.Is(err, ports.ErrUnauthenticated) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// R068, §6.7.4 and §6.7.11: with a group source (Microsoft Graph in
// production), an overage token's groups are resolved, kept for 10
// minutes, and fetched once for many callers at once.
func TestR068OverageGroupsAreResolvedAndCached(t *testing.T) {
	key, other := keys(t)
	iss := issuer_{key: key, other: other}
	synctest.Test(t, func(t *testing.T) {
		src := &groupSource{groups: []string{"credit-analysts", "risk"}, hold: make(chan struct{})}
		a, err := New(Options{Issuer: issuer, Audiences: []string{"api://atlas"}, Keys: jwt.StaticKeys{"k1": &key.PublicKey}, Tenant: "t1", Groups: src})
		if err != nil {
			t.Fatal(err)
		}
		token := overageTokens(t, iss)["_claim_names"]
		var g conc.Group
		for range 10 {
			g.Go(func() error {
				id, err := a.Authenticate(t.Context(), token)
				if err != nil {
					return err
				}
				if !slices.Contains(id.Groups, "credit-analysts") || !slices.Contains(id.Groups, "Holdings.Reader") {
					return errors.New("groups " + strings.Join(id.Groups, ","))
				}
				return nil
			})
		}
		synctest.Wait()
		close(src.hold)
		if err := g.Wait(); err != nil {
			t.Fatal(err)
		}
		if n := src.calls.Load(); n != 1 {
			t.Fatalf("%d lookups for ten callers at once", n)
		}
		time.Sleep(9 * time.Minute)
		if _, err := a.Authenticate(t.Context(), token); err != nil || src.calls.Load() != 1 {
			t.Fatalf("within 10 minutes: %v, %d lookups", err, src.calls.Load())
		}
		time.Sleep(2 * time.Minute)
		if _, err := a.Authenticate(t.Context(), token); err != nil || src.calls.Load() != 2 {
			t.Fatalf("after 10 minutes: %v, %d lookups", err, src.calls.Load())
		}
		src.err = errors.New("graph: 503")
		time.Sleep(11 * time.Minute)
		if _, err := a.Authenticate(t.Context(), token); err == nil || errors.Is(err, ports.ErrUnauthenticated) {
			t.Fatalf("groups that cannot be resolved: %v", err)
		}
	})
}

// A genuine token whose claims are not what Entra issues is refused, never
// read as an identity with what could be parsed (no claim error is
// swallowed).
func TestAClaimOfTheWrongShapeIsRefused(t *testing.T) {
	key, other := keys(t)
	iss := issuer_{key: key, other: other}
	a, err := New(Options{Issuer: issuer, Audiences: []string{"api://atlas"}, Keys: jwt.StaticKeys{"k1": &key.PublicKey}, Tenant: "t1"})
	if err != nil {
		t.Fatal(err)
	}
	c := iss.claims("api://atlas", porttest.People[0], time.Now().Add(time.Hour))
	c["groups"] = "credit-analysts"
	if _, err := a.Authenticate(t.Context(), iss.sign(t, key, "k1", c)); err == nil {
		t.Fatal("a groups claim that is not a list was read")
	}
}

// overageTokens are the two ways Entra marks group overage.
func overageTokens(t *testing.T, iss issuer_) map[string]string {
	id := porttest.People[0]
	id.Groups = nil
	names := iss.claims("api://atlas", id, time.Now().Add(time.Hour))
	delete(names, "groups")
	names["roles"] = []string{"Holdings.Reader"}
	names["_claim_names"] = map[string]string{"groups": "src1"}
	names["_claim_sources"] = map[string]any{"src1": map[string]string{"endpoint": "https://graph.windows.net/t1/users/" + id.Principal + "/getMemberObjects"}}
	has := iss.claims("api://atlas", id, time.Now().Add(time.Hour))
	delete(has, "groups")
	has["hasgroups"] = true
	return map[string]string{"_claim_names": iss.sign(t, iss.key, "k1", names), "hasgroups": iss.sign(t, iss.key, "k1", has)}
}

// groupSource is Microsoft Graph's stand-in: it counts lookups, and can
// hold them until released, or fail.
type groupSource struct {
	groups []string
	hold   chan struct{}
	err    error
	calls  atomic.Int32
}

func (g *groupSource) Groups(ctx context.Context, tenant, principal string) ([]string, error) {
	g.calls.Add(1)
	if g.hold != nil {
		select {
		case <-g.hold:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if g.err != nil {
		return nil, g.err
	}
	if tenant != "t1" || principal != porttest.People[0].Principal {
		return nil, errors.New("asked about someone else")
	}
	return slices.Clone(g.groups), nil
}

// endpoint is the tenant's JWKS endpoint, in memory: a transport for the
// kit's KeySet, so an outage and the passing of time are the test's to
// decide.
type endpoint struct {
	mu      sync.Mutex
	keys    map[string]*rsa.PublicKey
	status  int
	down    error
	garbage bool
	hang    bool
	asked   int
}

func newEndpoint(keys map[string]*rsa.PublicKey) *endpoint { return &endpoint{keys: keys} }

func (e *endpoint) keySet() *jwt.KeySet {
	ks := jwt.NewKeySet("https://login.microsoftonline.com/t1/discovery/v2.0/keys")
	ks.Client = &http.Client{Transport: e}
	return ks
}

func (e *endpoint) fail(status int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.status = status
}

func (e *endpoint) heal(keys map[string]*rsa.PublicKey) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.status, e.down, e.keys = 0, nil, keys
}

// blackhole makes every request hang until its context ends, as an
// endpoint behind dropped packets does; asked counts the requests.
func (e *endpoint) blackhole() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.hang = true
}

func (e *endpoint) RoundTrip(r *http.Request) (*http.Response, error) {
	e.mu.Lock()
	e.asked++
	if e.hang {
		e.mu.Unlock()
		<-r.Context().Done()
		return nil, r.Context().Err()
	}
	defer e.mu.Unlock()
	if e.down != nil {
		return nil, e.down
	}
	status, body := http.StatusOK, []byte("<html>not the keys</html>")
	if e.status != 0 {
		status = e.status
	} else if !e.garbage {
		type jwk struct {
			Kty string `json:"kty"`
			Use string `json:"use"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		}
		doc := struct {
			Keys []jwk `json:"keys"`
		}{Keys: []jwk{}}
		for kid, k := range e.keys {
			doc.Keys = append(doc.Keys, jwk{Kty: "RSA", Use: "sig", Kid: kid, N: base64.RawURLEncoding.EncodeToString(k.N.Bytes()), E: base64.RawURLEncoding.EncodeToString(big.NewInt(int64(k.E)).Bytes())})
		}
		body, _ = json.Marshal(doc)
	}
	h := http.Header{"Content-Type": {"application/json"}, "Cache-Control": {"max-age=3600"}}
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
}

// R064, §6.1.16's Entra row (the review's regression): while the key
// endpoint is blackholed, a held token still verifies at once. The first
// call after the document's max-age asks the endpoint for no longer than
// heldProbe, and the calls after it, for heldRetry, do not ask at all; and a
// key the start-up refresh fetched is held though no call has used it.
func TestR064ABlackholedEndpointCostsNoCallItsDeadline(t *testing.T) {
	key, other := keys(t)
	iss := issuer_{key: key, other: other}
	synctest.Test(t, func(t *testing.T) {
		end := newEndpoint(map[string]*rsa.PublicKey{"k1": &key.PublicKey})
		a, err := New(Options{Issuer: issuer, Audiences: []string{"api://atlas"}, Keys: end.keySet(), Tenant: "t1", MaxStale: 6 * time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		if err := a.Refresh(t.Context()); err != nil {
			t.Fatal(err)
		}
		end.blackhole()
		time.Sleep(2 * time.Hour) // past the document's max-age; no call has used k1
		start := time.Now()
		if _, err := a.Authenticate(t.Context(), iss.Valid(t, porttest.People[0])); err != nil {
			t.Fatalf("a token under a key the refresh fetched, during the outage: %v", err)
		}
		if took := time.Since(start); took > heldProbe {
			t.Fatalf("the first call waited %v on the blackholed endpoint, more than %v", took, heldProbe)
		}
		end.mu.Lock()
		asked := end.asked
		end.mu.Unlock()
		start = time.Now()
		for range 50 {
			if _, err := a.Authenticate(t.Context(), iss.Valid(t, porttest.People[0])); err != nil {
				t.Fatal(err)
			}
		}
		end.mu.Lock()
		defer end.mu.Unlock()
		if took := time.Since(start); took != 0 || end.asked != asked {
			t.Fatalf("50 calls after a failure waited %v and asked the endpoint %d more times", took, end.asked-asked)
		}
	})
}
