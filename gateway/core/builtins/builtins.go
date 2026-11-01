// Package builtins serves the atlas.* capabilities every gateway serves at
// the bindings its bootstrap names (§9.3.2): who the caller is, the
// catalogue of what they may use and the search over it, a capability's
// machine contract and its human guide, why access is missing, and the
// client's setup. They answer through the same pipeline as every other
// capability, and from the same access resolver, so what discovery shows
// and what a call is allowed never disagree (ATLAS-CORE §3).
//
// Every answer is about the caller, now: none is cached (R01). The working
// view (catalog, search, describe, guide) shows only what the caller can
// use (§2.6), by one availability rule (view.go); only atlas.access shows
// the rest, with how to get it.
package builtins

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"errors"
	"fmt"
	"io/fs"

	"github.com/jhgonline/fi-app-atlas/gateway/core/access"
	"github.com/jhgonline/fi-app-atlas/gateway/core/registry"
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/clock"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/ijson"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/profile"
)

//go:embed profiles
var files embed.FS

// Profiles are the built-ins' profiles, exactly as atlas/1.0 fixes them,
// with their owner's usage beside them (prose, outside the fingerprint).
func Profiles() ([]*profile.Profile, error) {
	names, err := fs.Glob(files, "profiles/*.json")
	if err != nil {
		return nil, fmt.Errorf("builtins: the profiles: %w", err)
	}
	var out []*profile.Profile
	for _, n := range names {
		b, err := fs.ReadFile(files, n)
		if err != nil {
			return nil, fmt.Errorf("builtins: %s: %w", n, err)
		}
		p, err := registry.Load(b)
		if err != nil {
			return nil, fmt.Errorf("builtins: %s: %w", n, err)
		}
		out = append(out, p)
	}
	return out, nil
}

// Client is one Atlas client, as atlas.clients lists it.
type Client struct {
	Name, Language, BuiltFor, Version, Install string
	Roles, Levels, Extras                      []string
}

// MinCursorKey is the shortest cursor key New accepts, in bytes: an
// HMAC-SHA-256 key as long as the hash it keys.
const MinCursorKey = 32

// Options configure the built-ins.
type Options struct {
	Registry    *registry.Registry
	Environment string
	Clock       clock.Clock
	// CursorKey signs listing cursors (§6.7.9): at least MinCursorKey
	// bytes, the same in every replica, so a cursor one replica issued
	// another accepts. Without one, New makes a random key, which serves
	// a single process only: a deployment with replicas passes its own.
	CursorKey []byte
	// PreviousCursorKey is the key CursorKey replaced. Cursors it signed
	// are still accepted, for the hour they live, after a rotation
	// (§6.7.9); once that hour has passed it can be dropped.
	PreviousCursorKey []byte
	// Clients are what atlas.clients lists.
	Clients []Client
}

// Provider serves every atlas.* interaction.
type Provider struct {
	opt Options
}

// New is the built-ins' provider. It refuses a cursor key too short to
// sign with, and a previous key without a current one.
func New(opt Options) (*Provider, error) {
	if opt.Registry == nil {
		return nil, errors.New("builtins: a registry is required")
	}
	if opt.Clock == nil {
		opt.Clock = clock.System
	}
	switch n := len(opt.CursorKey); {
	case n == 0 && len(opt.PreviousCursorKey) > 0:
		return nil, errors.New("builtins: a previous cursor key is accepted only beside a current one")
	case n == 0:
		opt.CursorKey = make([]byte, MinCursorKey)
		if _, err := rand.Read(opt.CursorKey); err != nil {
			return nil, fmt.Errorf("builtins: a cursor key: %w", err)
		}
	case n < MinCursorKey:
		return nil, fmt.Errorf("builtins: the cursor key is %d bytes; it needs at least %d", n, MinCursorKey)
	default:
		opt.CursorKey = bytes.Clone(opt.CursorKey)
	}
	if n := len(opt.PreviousCursorKey); n > 0 && n < MinCursorKey {
		return nil, fmt.Errorf("builtins: the previous cursor key is %d bytes; it needs at least %d", n, MinCursorKey)
	}
	opt.PreviousCursorKey = bytes.Clone(opt.PreviousCursorKey)
	return &Provider{opt: opt}, nil
}

// Entries are the built-ins, for the deployment's registration.
func (p *Provider) Entries() ([]registry.Entry, error) {
	profiles, err := Profiles()
	if err != nil {
		return nil, err
	}
	out := make([]registry.Entry, len(profiles))
	for i, pr := range profiles {
		out[i] = registry.Entry{Profiles: []*profile.Profile{pr}, Provider: p}
	}
	return out, nil
}

// Serve answers a built-in.
func (p *Provider) Serve(ctx context.Context, inv *ports.Invocation) (*ports.Answer, error) {
	s := access.SubjectFrom(ctx)
	if s == nil || s.Policy == nil {
		return nil, errors.New("builtins: no caller in the call's context")
	}
	req := inv.Request
	if req == nil {
		req = ijson.NewObject()
	}
	// One call reads one catalogue. The caller is resolved again, under
	// the policy and identity the pipeline decided by, in the snapshot this
	// answer is written from: so the caller's decisions, the roles that
	// would grant what they lack, the listing and its cursor never
	// disagree, even when a registration lands during the call.
	snap := p.opt.Registry.Snapshot()
	v := &view{s: access.ResolveIn(s.Policy, s.Identity, snap), snap: snap, features: features(inv)}
	switch inv.Capability + ":" + inv.Interaction {
	case "atlas.whoami@v1:fetch":
		return record(p.whoami(v))
	case "atlas.catalog@v1:fetch":
		return p.catalog(v, req)
	case "atlas.search@v1:fetch":
		return p.search(v, req)
	case "atlas.describe@v1:fetch":
		return p.describe(ctx, v, req)
	case "atlas.guide@v1:fetch":
		return p.guide(v, req)
	case "atlas.access@v1:fetch":
		return p.access(v, req)
	case "atlas.setup@v1:fetch":
		return record(p.setup(v))
	case "atlas.clients@v1:fetch":
		if err := noCursor(req); err != nil {
			return nil, err
		}
		return record(p.clients())
	case "atlas.usage@v1:fetch", "atlas.operation@v1:list":
		// Usage and operations come with the limiter's counters and the
		// operation store; until then each is one empty page, which issues
		// no cursor and so accepts none.
		if err := noCursor(req); err != nil {
			return nil, err
		}
		return record(page(nil, nil))
	case "atlas.operation@v1:get", "atlas.operation@v1:cancel":
		return nil, protocol.NewProblem(protocol.CodeNotFound, "no such operation").WithNext("Call atlas.operation@v1:list for your operations.")
	case "atlas.operation@v1:watch":
		return nil, protocol.NewProblem(protocol.CodeNotInEnvironment, "operation events are not served in this environment").WithNext("Poll the operation with atlas.operation@v1:get.")
	}
	return nil, fmt.Errorf("builtins: no built-in %s:%s", inv.Capability, inv.Interaction)
}

// features are what the calling client declared it implements; a client
// that declared nothing is Core (§9.6.3).
func features(inv *ports.Invocation) protocol.Features {
	f := inv.Features
	if len(f.Levels) == 0 && len(f.Deliveries) == 0 && len(f.Encodings) == 0 && len(f.Traits) == 0 {
		return protocol.CoreFeatures()
	}
	return f
}

func record(v any) (*ports.Answer, error) {
	o, err := ijson.FromGo(v)
	if err != nil {
		return nil, fmt.Errorf("builtins: an answer: %w", err)
	}
	obj, ok := o.(*ijson.Object)
	if !ok {
		return nil, errors.New("builtins: an answer is an object")
	}
	return &ports.Answer{Record: obj}, nil
}

// page is a collection answer (page.schema.json).
func page(items []any, next *string) map[string]any {
	if items == nil {
		items = []any{}
	}
	var n any
	if next != nil {
		n = *next
	}
	return map[string]any{"items": items, "next_cursor": n}
}

// intMember is an integer member of a request, or def when it is absent
// or not an integer.
func intMember(req *ijson.Object, name string, def int) int {
	v, _ := req.Get(name)
	if n, ok := v.(ijson.Number); ok {
		if i, isInt := n.Int64(); isInt {
			return int(i)
		}
	}
	return def
}

func strMember(req *ijson.Object, name string) string {
	v, _ := req.Get(name)
	s, _ := v.(string)
	return s
}

func boolMember(req *ijson.Object, name string) bool {
	v, _ := req.Get(name)
	b, _ := v.(bool)
	return b
}
