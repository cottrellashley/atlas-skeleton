// Package entra is the Authenticator for Microsoft Entra ID access tokens
// (§6.2.2): it verifies a token's signature against the tenant's keys, its
// issuer, audience and lifetime, and reads the identity from its claims.
// Who the caller is comes only from the verified token: oid is the
// principal, and a token Entra issued to an application (idtyp app) is a
// workload, never a person (ATLAS-CORE §3).
//
// The tenant's keys are its own dependency (§6.1.16's Entra row). When they
// cannot be fetched, a token is not the caller's fault: the call is
// unavailable, never unauthenticated (ports.Authenticator). Keys the tenant
// served stay in use while its endpoint is down, so tokens already held keep
// working, for MaxStale (§6.7.11: the JWKS is kept until an unknown kid).
// And a person in more groups than a token carries (group overage) is never
// read as a person in no groups (§6.7.4).
package entra

import (
	"context"
	"crypto"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/clock"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/jwt"
)

// Options configure the authenticator.
type Options struct {
	// Issuer is the tenant's v2 issuer:
	// https://login.microsoftonline.com/<tenant>/v2.0.
	Issuer string
	// Audiences are the gateway's: its application id URI or client id.
	Audiences []string
	// Keys are the tenant's signing keys: jwt.NewKeySet of its JWKS URL.
	Keys jwt.KeyProvider
	// Tenant, when set, refuses tokens of any other tenant.
	Tenant string
	// MaxStale is how long a key stays in use after the tenant's key
	// endpoint last confirmed it, while the endpoint cannot be reached
	// (default 24 hours). A key the endpoint stops listing is dropped as
	// soon as it answers without it.
	MaxStale time.Duration
	// Groups gives a person's groups when their token cannot list them
	// (group overage): Microsoft Graph in production. With none, such a
	// token is refused as the gateway's own limitation (ErrGroupOverage).
	Groups GroupSource
	// GroupsMaxAge is how long groups from Groups are kept (default 10
	// minutes, §6.7.4), and MaxGroupEntries how many people's (default
	// 10000).
	GroupsMaxAge    time.Duration
	MaxGroupEntries int
	// Clock tells the time for the caches; Log, when set, records each token
	// refused for group overage.
	Clock clock.Clock
	Log   *slog.Logger
}

// GroupSource gives the directory groups of a principal of a tenant, as
// Microsoft Graph's getMemberGroups does. It is safe for concurrent use.
type GroupSource interface {
	Groups(ctx context.Context, tenant, principal string) ([]string, error)
}

// ErrGroupOverage is a token whose groups are too many to carry (Entra
// lists none, and points at Graph) on an authenticator with no GroupSource.
// It is the gateway's limitation, never the caller's fault, so it does not
// wrap ports.ErrUnauthenticated: the call is unavailable.
var ErrGroupOverage = errors.New("entra: the token's groups are too many for it to carry (group overage), and no group source is configured")

// Authenticator verifies Entra access tokens. It is safe for concurrent use.
type Authenticator struct {
	v      jwt.Verifier
	keys   *heldKeys
	groups *groupCache
	tenant string
	log    *slog.Logger
}

// New is an Authenticator.
func New(opt Options) (*Authenticator, error) {
	if opt.Issuer == "" || len(opt.Audiences) == 0 || opt.Keys == nil {
		return nil, errors.New("entra: an issuer, an audience and the tenant's keys are required")
	}
	if opt.MaxStale <= 0 {
		opt.MaxStale = 24 * time.Hour
	}
	if opt.GroupsMaxAge <= 0 {
		opt.GroupsMaxAge = 10 * time.Minute
	}
	if opt.MaxGroupEntries <= 0 {
		opt.MaxGroupEntries = 10000
	}
	if opt.Clock == nil {
		opt.Clock = clock.System
	}
	keys := &heldKeys{src: opt.Keys, clock: opt.Clock, maxStale: opt.MaxStale, held: map[string]heldKey{}}
	a := &Authenticator{v: jwt.Verifier{Issuer: opt.Issuer, Audiences: opt.Audiences, Keys: keys, Now: opt.Clock.Now}, keys: keys, tenant: opt.Tenant, log: opt.Log}
	if opt.Groups != nil {
		a.groups = &groupCache{src: opt.Groups, clock: opt.Clock, maxAge: opt.GroupsMaxAge, max: opt.MaxGroupEntries, entries: map[string]*groupEntry{}}
	}
	return a, nil
}

// Refresh fetches the tenant's keys now, as the gateway does at start
// (§6.7.4), when the key provider can (jwt.KeySet can).
func (a *Authenticator) Refresh(ctx context.Context) error { return a.keys.refresh(ctx) }

// Check is the authenticator's readiness: nil once it has held the tenant's
// keys, which it fetches until it has. A replica that cannot verify a token
// is not ready; one that holds keys through an outage still is.
func (a *Authenticator) Check(ctx context.Context) error {
	if a.keys.fetched() {
		return nil
	}
	return a.keys.refresh(ctx)
}

// Authenticate is the identity a verified token proves.
func (a *Authenticator) Authenticate(ctx context.Context, token string) (ports.Identity, error) {
	if token == "" {
		return ports.Identity{}, ports.ErrUnauthenticated
	}
	f := &keyFault{}
	c, err := a.v.Verify(context.WithValue(ctx, keyFaultKey{}, f), token)
	switch {
	case f.err != nil:
		return ports.Identity{}, fmt.Errorf("entra: the tenant's signing keys cannot be fetched now: %w", f.err)
	case errors.Is(err, jwt.ErrExpired):
		return ports.Identity{}, fmt.Errorf("%w: %v", ports.ErrTokenExpired, err)
	case errors.Is(err, jwt.ErrInvalidToken):
		return ports.Identity{}, fmt.Errorf("%w: %v", ports.ErrUnauthenticated, err)
	case err != nil:
		return ports.Identity{}, fmt.Errorf("entra: %w", err)
	}
	if a.tenant != "" && c.TenantID != a.tenant {
		return ports.Identity{}, fmt.Errorf("%w: a token of another tenant", ports.ErrUnauthenticated)
	}
	principal := c.ObjectID
	if principal == "" {
		return ports.Identity{}, fmt.Errorf("%w: the token names no object id", ports.ErrUnauthenticated)
	}
	var extra struct {
		name, idtyp, azp, appid string
		groups                  []string
		hasGroups               bool
		claimNames              map[string]string
	}
	for claim, into := range map[string]any{
		"name": &extra.name, "idtyp": &extra.idtyp, "azp": &extra.azp, "appid": &extra.appid,
		"groups": &extra.groups, "hasgroups": &extra.hasGroups, "_claim_names": &extra.claimNames,
	} {
		if raw, ok := c.Raw[claim]; ok {
			if err := json.Unmarshal(raw, into); err != nil {
				return ports.Identity{}, fmt.Errorf("entra: the token's %s claim is not as Entra issues it: %w", claim, err)
			}
		}
	}
	id := ports.Identity{Principal: principal, Tenant: c.TenantID, Kind: "person", Name: c.Email}
	if extra.name != "" {
		id.Name = extra.name
	}
	if extra.idtyp == "app" {
		id.Kind = "workload"
	}
	id.Application = extra.azp
	if id.Application == "" {
		id.Application = extra.appid
	}
	groups := extra.groups
	if _, overage := extra.claimNames["groups"]; overage || extra.hasGroups {
		if groups, err = a.overage(ctx, c.TenantID, principal); err != nil {
			return ports.Identity{}, err
		}
	}
	id.Groups = append(append([]string(nil), groups...), c.Roles...)
	if id.Name == "" {
		id.Name = principal
	}
	return id, nil
}

// overage is the groups of a person whose token could not carry them.
func (a *Authenticator) overage(ctx context.Context, tenant, principal string) ([]string, error) {
	if a.groups == nil {
		if a.log != nil {
			a.log.WarnContext(ctx, "entra: a token with group overage was refused: configure a group source", "principal", principal)
		}
		return nil, ErrGroupOverage
	}
	groups, err := a.groups.get(ctx, tenant, principal)
	if err != nil {
		return nil, fmt.Errorf("entra: the groups of a token with group overage cannot be read now: %w", err)
	}
	return groups, nil
}

// keyFault carries, out of the verifier, why the tenant's keys could not be
// had during one Authenticate: the verifier reports every key failure as a
// bad signature, and a key source that fails is no fault of the token.
type keyFault struct{ err error }

type keyFaultKey struct{}

// heldKeys are the tenant's keys as the authenticator uses them: from the
// provider while it answers, and as it last gave them, for maxStale, while
// it does not.
type heldKeys struct {
	src      jwt.KeyProvider
	clock    clock.Clock
	maxStale time.Duration

	mu   sync.Mutex
	held map[string]heldKey // bounded: the kids the provider gave within maxStale
	got  bool               // the provider has answered once
	// failedAt is when the provider last failed to answer (not a kid it no
	// longer lists). For heldRetry after it, a call whose key is held uses
	// it without asking the provider again: an outage costs the provider's
	// timeout once, never on every call (§6.1.16: tokens already held keep
	// working).
	failedAt time.Time
}

// heldRetry is how long after a failure a held key is used without asking
// the provider; heldProbe bounds the provider's answer while a held key
// could answer instead.
const (
	heldRetry = 30 * time.Second
	heldProbe = 500 * time.Millisecond
)

type heldKey struct {
	key  crypto.PublicKey
	seen time.Time // when the provider last gave it
}

// Key implements jwt.KeyProvider.
func (k *heldKeys) Key(ctx context.Context, kid string) (crypto.PublicKey, error) {
	k.mu.Lock()
	now := k.clock.Now()
	h, held := k.held[kid]
	held = held && now.Sub(h.seen) <= k.maxStale
	recent := !k.failedAt.IsZero() && now.Sub(k.failedAt) < heldRetry
	k.mu.Unlock()
	if held && recent {
		return h.key, nil // the provider failed moments ago: do not wait on it again
	}
	pctx := ctx
	if held {
		var cancel context.CancelFunc
		pctx, cancel = context.WithTimeout(ctx, heldProbe)
		defer cancel()
	}
	key, err := k.src.Key(pctx, kid)
	now = k.clock.Now()
	k.mu.Lock()
	defer k.mu.Unlock()
	switch {
	case err == nil:
		for id, h := range k.held {
			if now.Sub(h.seen) > k.maxStale {
				delete(k.held, id)
			}
		}
		k.held[kid], k.got, k.failedAt = heldKey{key: key, seen: now}, true, time.Time{}
		return key, nil
	case errors.Is(err, jwt.ErrUnknownKey):
		delete(k.held, kid) // the tenant no longer lists it
		return nil, err
	}
	k.failedAt = now
	if h, ok := k.held[kid]; ok && now.Sub(h.seen) <= k.maxStale {
		return h.key, nil
	}
	if f, ok := ctx.Value(keyFaultKey{}).(*keyFault); ok {
		f.err = err
	}
	return nil, err
}

func (k *heldKeys) refresh(ctx context.Context) error {
	r, ok := k.src.(interface{ Refresh(context.Context) error })
	if !ok {
		k.mu.Lock()
		k.got = true // fixed keys: nothing to fetch
		k.mu.Unlock()
		return nil
	}
	if err := r.Refresh(ctx); err != nil {
		return fmt.Errorf("entra: the tenant's signing keys cannot be fetched: %w", err)
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.got, k.failedAt = true, time.Time{}
	// Every key the document lists is held at once, so a key no call has
	// used yet still verifies through an outage that follows.
	if ks, ok := k.src.(interface {
		Keys() map[string]crypto.PublicKey
	}); ok {
		now := k.clock.Now()
		for kid, key := range ks.Keys() {
			k.held[kid] = heldKey{key: key, seen: now}
		}
	}
	return nil
}

func (k *heldKeys) fetched() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.got
}

// groupCache keeps resolved groups for maxAge, and makes one lookup for many
// callers who miss at once (§6.7.11).
type groupCache struct {
	src    GroupSource
	clock  clock.Clock
	maxAge time.Duration
	max    int

	mu      sync.Mutex
	entries map[string]*groupEntry
}

type groupEntry struct {
	ready  chan struct{} // closed once the lookup ends
	groups []string
	err    error
	at     time.Time
}

func (c *groupCache) get(ctx context.Context, tenant, principal string) ([]string, error) {
	key := tenant + "/" + principal
	c.mu.Lock()
	e, ok := c.entries[key]
	if ok {
		select {
		case <-e.ready:
			if c.clock.Now().Sub(e.at) > c.maxAge {
				ok = false
			}
		default: // a lookup is under way: wait for it
		}
	}
	if !ok {
		c.evict()
		e = &groupEntry{ready: make(chan struct{})}
		c.entries[key] = e
		c.mu.Unlock()
		e.groups, e.err = c.src.Groups(ctx, tenant, principal)
		e.at = c.clock.Now()
		c.mu.Lock()
		if e.err != nil && c.entries[key] == e {
			delete(c.entries, key) // the next caller asks again
		}
		c.mu.Unlock()
		close(e.ready)
	} else {
		c.mu.Unlock()
	}
	select {
	case <-e.ready:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if e.err != nil {
		return nil, e.err
	}
	return e.groups, nil
}

// evict makes room for one more entry: expired ones first, then any whose
// lookup has ended. Its caller holds mu.
func (c *groupCache) evict() {
	if len(c.entries) < c.max {
		return
	}
	now := c.clock.Now()
	for k, e := range c.entries {
		select {
		case <-e.ready:
			if now.Sub(e.at) > c.maxAge {
				delete(c.entries, k)
			}
		default:
		}
	}
	for k, e := range c.entries {
		if len(c.entries) < c.max {
			return
		}
		select {
		case <-e.ready:
			delete(c.entries, k)
		default:
		}
	}
}
