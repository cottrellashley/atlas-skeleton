package atlas

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/grammar"
	"atlas.local/sdk/go/protocol/ijson"
	"atlas.local/sdk/go/protocol/profile"
	"atlas.local/sdk/go/protocol/resolve"
)

// The bounds of what discovery reads.
const (
	maxPages    = 1000    // a listing's pages
	maxProfile  = 4 << 20 // a cached profile's file
	maxPointers = 1 << 20 // the current-profile pointer file
	maxChain    = 32      // the profiles between a writer and a reader
)

type describedProfile struct {
	profile *profile.Profile
	doc     *ijson.Object
}

// record is a built-in's record answer.
func (c *Client) record(ctx context.Context, address string, req any) (*ijson.Object, error) {
	r, err := c.Call(ctx, address, req)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	if r.Record == nil {
		return nil, protocol.NewProblem(protocol.CodeInvalidDocument, address+" answered no record")
	}
	return r.Record, nil
}

// Whoami is the caller's effective context: principal, roles, environment,
// domains, house rules (ATLAS-CORE §2, Orient).
func (c *Client) Whoami(ctx context.Context) (*ijson.Object, error) {
	return c.record(ctx, "atlas.whoami@v1:fetch", nil)
}

// Catalog is every capability in the caller's working view, every page
// followed (CORE-DISC-001). filter holds domain, family and effect.
func (c *Client) Catalog(ctx context.Context, filter map[string]any) ([]*ijson.Object, error) {
	return c.pages(ctx, "atlas.catalog@v1:fetch", filter)
}

// Search is the capabilities that match words, best first.
func (c *Client) Search(ctx context.Context, words string) ([]*ijson.Object, error) {
	return c.pages(ctx, "atlas.search@v1:fetch", map[string]any{"words": words})
}

// Access is why the caller can or cannot use each capability: id's, or
// those of a domain, or all.
func (c *Client) Access(ctx context.Context, id string) ([]*ijson.Object, error) {
	return c.AccessIn(ctx, id, "")
}

// AccessIn is Access, narrowed to an id or a domain.
func (c *Client) AccessIn(ctx context.Context, id, domain string) ([]*ijson.Object, error) {
	req := map[string]any{}
	if id != "" {
		req["id"] = id
	}
	if domain != "" {
		req["domain"] = domain
	}
	return c.pages(ctx, "atlas.access@v1:fetch", req)
}

// pages follows a listing's cursors. Each page was held to its document
// when it was read, so its items are objects (R032).
func (c *Client) pages(ctx context.Context, address string, req map[string]any) ([]*ijson.Object, error) {
	if req == nil {
		req = map[string]any{}
	}
	var out []*ijson.Object
	for page := 0; page < maxPages; page++ {
		rec, err := c.record(ctx, address, req)
		if err != nil {
			return nil, err
		}
		items, _ := rec.Get("items")
		list, ok := items.([]ijson.Value)
		if !ok {
			return nil, protocol.NewProblem(protocol.CodeInvalidDocument, address+" answered a page without items").WithTarget("/items")
		}
		for i, it := range list {
			o, ok := it.(*ijson.Object)
			if !ok {
				return nil, protocol.NewProblem(protocol.CodeInvalidDocument, fmt.Sprintf("%s answered an item that is not an object", address)).WithTarget(fmt.Sprintf("/items/%d", i))
			}
			out = append(out, o)
		}
		next, _ := rec.Get("next_cursor")
		if next == nil {
			return out, nil
		}
		cursor, ok := next.(string)
		if !ok || cursor == "" {
			return nil, protocol.NewProblem(protocol.CodeInvalidDocument, address+" answered a next_cursor that is not a cursor").WithTarget("/next_cursor")
		}
		req["cursor"] = cursor
	}
	return nil, protocol.NewProblem(protocol.CodeUpstreamFailed, fmt.Sprintf("the listing did not end after %d pages", maxPages))
}

// Describe is a capability's describe answer: its profile, the caller's
// governance of it, its availability and this client's eligibility. id is a
// capability id, a pinned id or an exact id. It always asks the gateway:
// the caller's governance and availability are never cached (R01).
func (c *Client) Describe(ctx context.Context, id string) (*ijson.Object, error) {
	req := map[string]any{"by": "id", "id": id}
	if _, err := grammar.ParseExact(id); err == nil {
		req = map[string]any{"by": "exact", "exact": id}
	}
	rec, err := c.record(ctx, "atlas.describe@v1:fetch", req)
	if err != nil {
		return nil, err
	}
	d, err := c.keep(rec)
	if err != nil {
		return nil, err
	}
	if pinned, err := grammar.ParsePinned(id); err == nil {
		c.point(pinned.String(), d.profile.Exact().String())
	}
	return rec, nil
}

// Guide is a capability's guide in this client's syntax, as the gateway
// writes it now: never cached, since its prose can change under one
// contract (R01, gaps/2.md D7).
func (c *Client) Guide(ctx context.Context, id, client string) (string, error) {
	return c.GuideWithin(ctx, id, client, 0)
}

// GuideWithin is Guide within a budget of maxTokens (0: the gateway's).
func (c *Client) GuideWithin(ctx context.Context, id, client string, maxTokens int) (string, error) {
	req := map[string]any{"by": "id", "id": id}
	if _, err := grammar.ParseExact(id); err == nil {
		req = map[string]any{"by": "exact", "exact": id}
	}
	if client != "" {
		req["client"] = client
	}
	if maxTokens > 0 {
		req["max_tokens"] = maxTokens
	}
	rec, err := c.record(ctx, "atlas.guide@v1:fetch", req)
	if err != nil {
		return "", err
	}
	md, _ := rec.Get("markdown")
	s, _ := md.(string)
	return s, nil
}

// keep checks a describe answer and caches its profile by exact id.
func (c *Client) keep(rec *ijson.Object) (*describedProfile, error) {
	if p := checkDocument("describe", rec); p != nil {
		return nil, p
	}
	doc, _ := rec.Get("profile")
	return c.admit(doc.(*ijson.Object), true)
}

// admit checks a profile's fingerprint (§9.4.2) and reads it; a profile
// whose fingerprint is wrong is invalid_document (CORE-DISC-001). From the
// gateway, it is also kept on disk, as served, forever (gaps/2.md D4.1).
func (c *Client) admit(doc *ijson.Object, fromGateway bool) (*describedProfile, error) {
	if _, err := protocol.CheckFingerprint(doc); err != nil {
		return nil, err
	}
	p, err := profile.Parse(doc)
	if err != nil {
		return nil, protocol.NewProblem(protocol.CodeInvalidDocument, "the profile cannot be read: "+err.Error())
	}
	d := &describedProfile{profile: p, doc: doc}
	exact := p.Exact().String()
	c.mu.Lock()
	c.profiles[exact] = d
	c.mu.Unlock()
	if fromGateway {
		if b, err := ijson.Marshal(doc); err == nil {
			_ = writeFileAtomic(c.profileFile(exact), b, 0o600) // a cache that cannot be written is only slower
		}
	}
	return d, nil
}

func (c *Client) profileFile(exact string) string {
	return filepath.Join(c.opt.Home, "cache", "profiles", exact+".json")
}

// describeExact is a profile by pinned id (the one the gateway last named
// current for it) or by exact id: from memory, then from the cache on
// disk, then from the gateway, each fetched once (CORE-DISC-002). A cached
// file whose fingerprint is wrong is removed and the profile fetched again
// by its exact id; a pointer to a profile the gateway no longer has is
// dropped (gaps/2.md D4.1, D4.3).
func (c *Client) describeExact(ctx context.Context, pinned, exact string) (*describedProfile, error) {
	fromPointer := false
	c.mu.Lock()
	if exact == "" {
		exact = c.current[pinned]
		fromPointer = exact != ""
	}
	d := c.profiles[exact]
	c.mu.Unlock()
	if d != nil {
		return d, nil
	}
	if exact != "" {
		if _, err := grammar.ParseExact(exact); err != nil {
			return nil, protocol.NewProblem(protocol.CodeInvalidDocument, "not an exact id: "+exact)
		}
		file := c.profileFile(exact)
		if b, err := readBounded(file, maxProfile); err == nil {
			v, perr := ijson.Parse(b)
			o, _ := v.(*ijson.Object)
			if perr == nil && o != nil {
				if d, err := c.admit(o, false); err == nil && d.profile.Exact().String() == exact {
					return d, nil
				}
			}
			_ = os.Remove(file) // a damaged copy: fetch it again
		}
		rec, err := c.record(ctx, "atlas.describe@v1:fetch", map[string]any{"by": "exact", "exact": exact})
		if err == nil {
			if d, err = c.keep(rec); err != nil {
				return nil, err
			}
			if d.profile.Exact().String() != exact {
				return nil, protocol.NewProblem(protocol.CodeInvalidDocument, "atlas.describe answered "+d.profile.Exact().String()+" for "+exact)
			}
			return d, nil
		}
		var p *protocol.Problem
		if !fromPointer || !asProblem(err, &p) || p.Code != protocol.CodeNotFound {
			return nil, err
		}
		c.unpoint(pinned) // the pointer names a profile the gateway no longer has
	}
	rec, err := c.record(ctx, "atlas.describe@v1:fetch", map[string]any{"by": "id", "id": pinned})
	if err != nil {
		return nil, err
	}
	if d, err = c.keep(rec); err != nil {
		return nil, err
	}
	if got := (grammar.Pinned{ID: d.profile.ID, Version: d.profile.Version}).String(); got != pinned {
		return nil, protocol.NewProblem(protocol.CodeInvalidDocument, "atlas.describe answered "+got+" for "+pinned)
	}
	c.point(pinned, d.profile.Exact().String())
	return d, nil
}

// The current-profile pointer (gaps/2.md D4.3): per environment, each
// pinned id's exact id as the gateway last named it current, kept at
// <home>/cache/current-<env>.json.
func (c *Client) pointerFile() string {
	return filepath.Join(c.opt.Home, "cache", "current-"+c.env+".json")
}

func (c *Client) loadPointers() {
	b, err := readBounded(c.pointerFile(), maxPointers)
	if err != nil {
		return
	}
	var m map[string]string
	if json.Unmarshal(b, &m) != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for pinned, exact := range m {
		p, err1 := grammar.ParsePinned(pinned)
		e, err2 := grammar.ParseExact(exact)
		if err1 == nil && err2 == nil && e.Pinned == p {
			c.current[pinned] = exact
		}
	}
}

// point records exact as pinned's current profile, in memory and on disk.
func (c *Client) point(pinned, exact string) {
	c.mu.Lock()
	if c.current[pinned] == exact {
		c.mu.Unlock()
		return
	}
	c.current[pinned] = exact
	c.mu.Unlock()
	c.savePointers(pinned, exact)
}

func (c *Client) unpoint(pinned string) {
	c.mu.Lock()
	delete(c.current, pinned)
	c.mu.Unlock()
	c.savePointers(pinned, "")
}

// savePointers writes one change over the file as it now is, since other
// programs share it; the last writer wins, and a lost entry costs one
// describe.
func (c *Client) savePointers(pinned, exact string) {
	m := map[string]string{}
	if b, err := readBounded(c.pointerFile(), maxPointers); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	if exact == "" {
		delete(m, pinned)
	} else {
		m[pinned] = exact
	}
	if b, err := json.Marshal(m); err == nil {
		_ = writeFileAtomic(c.pointerFile(), b, 0o600) // a pointer that cannot be written is only slower
	}
}

// plan resolves records the gateway wrote under writer for reader, the
// profile this client wrote its request under (§9.5): the chain between
// them, each profile fetched by its exact id, the reader's schema
// projected to the request's columns (§9.5.2 step 5). Either may be the
// newer one. Records are never delivered unresolved: a chain that cannot
// be built is resolution_failed.
func (c *Client) plan(ctx context.Context, reader, writer *profile.Profile, in *profile.Interaction, columns []string) (*resolve.Plan, error) {
	if reader.ID != writer.ID || reader.Version != writer.Version {
		return nil, protocol.NewProblem(protocol.CodeResolutionFailed, fmt.Sprintf("the answer was written under %s, and the request under %s: a client never resolves across versions", writer.Exact(), reader.Exact()))
	}
	chain, err := c.chain(ctx, writer, reader)
	if err != nil {
		return nil, err
	}
	if chain == nil {
		if chain, err = c.chain(ctx, reader, writer); err != nil {
			return nil, err
		}
	}
	if chain == nil {
		return nil, protocol.NewProblem(protocol.CodeResolutionFailed, fmt.Sprintf("neither of %s and %s follows the other", writer.Exact(), reader.Exact()))
	}
	rc := make([]resolve.Profile, len(chain))
	for i, p := range chain {
		pin := p.Interactions[in.Name]
		if pin == nil {
			return nil, protocol.NewProblem(protocol.CodeResolutionFailed, fmt.Sprintf("%s has no interaction %s", p.Exact(), in.Name))
		}
		s := p.Schemas[pin.Returns]
		if s != nil && s.Items() != nil {
			s = s.Items()
		}
		rc[i] = resolve.Profile{Exact: p.Exact(), Lens: p.Evolution.Lens, Schema: s}
	}
	plan, err := resolve.New(rc, writer.Exact(), reader.Exact(), resolve.Options{Columns: columns, Dynamic: reader.HasTrait("DynamicSchema")})
	if err != nil {
		return nil, protocol.ResolutionFailed(err)
	}
	return plan, nil
}

// chain is the profiles from older back to newer, oldest first, when older
// is newer's ancestor; nil when it is not.
func (c *Client) chain(ctx context.Context, newer, older *profile.Profile) ([]*profile.Profile, error) {
	chain := []*profile.Profile{newer}
	for p := newer; p.Exact() != older.Exact(); {
		if p.Evolution.PreviousProfile == nil || len(chain) >= maxChain {
			return nil, nil
		}
		d, err := c.describeExact(ctx, "", p.Evolution.PreviousProfile.String())
		if err != nil {
			return nil, err
		}
		p = d.profile
		chain = append([]*profile.Profile{p}, chain...)
	}
	return chain, nil
}

// Profile is a profile by exact id, as this client holds it.
func (c *Client) Profile(ctx context.Context, exact string) (*profile.Profile, error) {
	if _, err := grammar.ParseExact(exact); err != nil {
		return nil, protocol.NewProblem(protocol.CodeInvalidRequest, err.Error()).WithTarget("exact")
	}
	d, err := c.describeExact(ctx, "", exact)
	if err != nil {
		return nil, err
	}
	return d.profile, nil
}

// Current is the profile a pinned or bare id is called under now: the
// one the gateway last named current, fetched if this client has none.
func (c *Client) Current(ctx context.Context, id string) (*profile.Profile, error) {
	ctx, cancel := context.WithTimeout(ctx, BuiltinDeadline)
	defer cancel()
	pinned, err := grammar.ParsePinned(id)
	if err != nil {
		if grammar.CapabilityID(id) != nil {
			return nil, protocol.NewProblem(protocol.CodeInvalidRequest, err.Error()).WithTarget("id")
		}
		if pinned, err = c.resolveBare(ctx, id); err != nil {
			return nil, err
		}
	}
	d, err := c.describeExact(ctx, pinned.String(), "")
	if err != nil {
		return nil, err
	}
	return d.profile, nil
}

func asProblem(err error, p **protocol.Problem) bool { return errors.As(err, p) }
