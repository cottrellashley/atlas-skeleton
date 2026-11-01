package porttest

import (
	"context"
	"errors"
	"slices"
	"testing"

	"atlas.local/gateway/ports"
)

// Issuer makes the tokens an Authenticator under test is given.
type Issuer interface {
	// Valid is a token that proves id.
	Valid(t *testing.T, id ports.Identity) string
	// Expired is a token that proved id until a moment ago.
	Expired(t *testing.T, id ports.Identity) string
	// Forged is a token for id that the authenticator must not trust:
	// signed by another key, or issued to another audience.
	Forged(t *testing.T, id ports.Identity) string
}

// People is who the Authenticator suite signs in as.
var People = []ports.Identity{
	{Principal: "a11ce000-0000-4000-8000-000000000001", Name: "Alice Analyst", Kind: "person", Tenant: "t1", Application: "atlas-cli", Groups: []string{"credit-analysts"}},
	{Principal: "7a5c0000-0000-4000-8000-000000000005", Name: "nightly-risk", Kind: "workload", Tenant: "t1", Application: "risk-batch", Groups: []string{"risk-batch"}},
}

// Authenticator is the suite of ports.Authenticator: only a token it can
// verify proves an identity, and the identity is the one the token states,
// person or workload (ATLAS-CORE §3).
func Authenticator(t *testing.T, a ports.Authenticator, iss Issuer) {
	ctx := context.Background()
	for _, want := range People {
		t.Run(want.Name, func(t *testing.T) {
			got, err := a.Authenticate(ctx, iss.Valid(t, want))
			if err != nil {
				t.Fatal(err)
			}
			if got.Principal != want.Principal || got.Kind != want.Kind || got.Name != want.Name || got.Tenant != want.Tenant || got.Application != want.Application {
				t.Fatalf("got %+v, want %+v", got, want)
			}
			for _, g := range want.Groups {
				if !slices.Contains(got.Groups, g) {
					t.Fatalf("groups %v lack %s", got.Groups, g)
				}
			}
			if _, err := a.Authenticate(ctx, iss.Expired(t, want)); !errors.Is(err, ports.ErrTokenExpired) {
				t.Fatalf("an expired token: %v", err)
			}
			if _, err := a.Authenticate(ctx, iss.Forged(t, want)); !errors.Is(err, ports.ErrUnauthenticated) {
				t.Fatalf("a forged token: %v", err)
			}
		})
	}
	for _, bad := range []string{"", "x", "a.b.c", "Bearer x"} {
		if _, err := a.Authenticate(ctx, bad); !errors.Is(err, ports.ErrUnauthenticated) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}
