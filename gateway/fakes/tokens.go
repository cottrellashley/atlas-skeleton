package fakes

import (
	"context"
	"sync"
	"time"

	"atlas.local/gateway/ports"
	"atlas.local/sdk/go/kit/clock"
)

// Tokens is an Authenticator over a table of tokens it was told about: the
// development gateway's sign-in, and the tests'.
type Tokens struct {
	clock clock.Clock
	mu    sync.RWMutex
	byKey map[string]issued
}

type issued struct {
	id      ports.Identity
	expires time.Time
}

// NewTokens is an empty table, its expiries read from c.
func NewTokens(c clock.Clock) *Tokens { return &Tokens{clock: c, byKey: map[string]issued{}} }

// Issue makes token prove id until expires (never, when it is zero).
func (t *Tokens) Issue(token string, id ports.Identity, expires time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.byKey[token] = issued{id, expires}
}

// Revoke makes token prove nothing.
func (t *Tokens) Revoke(token string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.byKey, token)
}

// Authenticate is the identity the token was issued for.
func (t *Tokens) Authenticate(ctx context.Context, token string) (ports.Identity, error) {
	if err := ctx.Err(); err != nil {
		return ports.Identity{}, err
	}
	t.mu.RLock()
	e, ok := t.byKey[token]
	t.mu.RUnlock()
	switch {
	case !ok || token == "":
		return ports.Identity{}, ports.ErrUnauthenticated
	case !e.expires.IsZero() && !t.clock.Now().Before(e.expires):
		return ports.Identity{}, ports.ErrTokenExpired
	}
	return e.id, nil
}
