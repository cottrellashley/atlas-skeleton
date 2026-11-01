package ports

import (
	"context"
	"errors"
)

// Identity is who is calling, as trusted authentication establishes it
// (ATLAS-CORE §3). Nothing in a request can change it.
type Identity struct {
	// Principal is the stable id of the person or workload (Entra's oid).
	Principal string
	// Name is how the principal is shown.
	Name string
	// Kind is "person" or "workload": from the token, never from the
	// caller's own claim.
	Kind string
	// Tenant is the directory the token was issued in.
	Tenant string
	// Application is the calling client's id (azp or appid).
	Application string
	// Groups are the directory groups and app roles the token carries.
	Groups []string
}

// ErrUnauthenticated is a token that does not prove an identity: missing,
// malformed, expired, or issued for another audience.
var ErrUnauthenticated = errors.New("ports: the token does not prove an identity")

// ErrTokenExpired is an expired token: the client refreshes it and repeats
// the call once (§9.6.6).
var ErrTokenExpired = errors.New("ports: the token has expired")

// Authenticator verifies a bearer token. A failure is ErrUnauthenticated or
// ErrTokenExpired (or wraps one); anything else is the authenticator's own
// fault, and the call is unavailable rather than unauthenticated.
// Authenticate honours ctx, which the call's deadline bounds: once ctx
// ends it returns ctx's error, and the call answers by its deadline (R042).
// It is called from every call's goroutine at once.
type Authenticator interface {
	Authenticate(ctx context.Context, token string) (Identity, error)
}
