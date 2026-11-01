package builtins

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/ijson"
)

// A cursor is a position in one listing (§6.7.9, page.schema.json):
// opaque, signed, and good for an hour. It is bound to who asked, to what
// they asked (the request without its cursor), to the policy and the
// catalogue the listing was computed from, and to the listing itself, item
// by item. A listing is recomputed for every page, so a cursor from before
// any change to it would resume at the wrong place, skipping an item or
// repeating one; it is refused instead, as is one that is anyone else's or
// older than its hour: invalid_request at /cursor (ATLAS-MECH-009), and
// the client starts the listing again.

const (
	cursorLife = time.Hour
	// maxCursor is the longest cursor read, in bytes: one New's provider
	// signs is under 100.
	maxCursor = 256
	// maxPage is the largest page (builtins.schema.json page_size).
	maxPage = 500
)

var strictBase64 = base64.RawURLEncoding.Strict()

// position is what a cursor carries.
type position struct {
	Offset  int   `json:"o"`
	Expires int64 `json:"x"` // Unix milliseconds
}

// listing is what a cursor is bound to beside its position.
type listing struct {
	principal string
	request   string // the request without its cursor, in RFC 8785 form
	policy    string // the policy's revision
	catalog   string // the catalogue's revision, its entity tag
	items     string // the digest of the whole listing, in order
}

func errCursor() *protocol.Problem {
	return protocol.NewProblem(protocol.CodeInvalidRequest, "the cursor is not one this listing issued to you, it has expired, or what it lists has changed since").
		WithTarget("/cursor").WithNext("Start the listing again without a cursor.")
}

// noCursor refuses a cursor on a listing that never issues one.
func noCursor(req *ijson.Object) error {
	if _, ok := req.Get("cursor"); ok {
		return errCursor()
	}
	return nil
}

// listingOf is the listing a request reads: ids are its items, in order.
func listingOf(v *view, req *ijson.Object, ids []string) (listing, error) {
	o, ok := ijson.Clone(req).(*ijson.Object)
	if !ok {
		return listing{}, fmt.Errorf("builtins: a request is an object")
	}
	o.Delete("cursor")
	key, err := ijson.Canonical(o)
	if err != nil {
		return listing{}, fmt.Errorf("builtins: a listing's request: %w", err)
	}
	h := sha256.New()
	for _, id := range ids {
		h.Write([]byte(id))
		h.Write([]byte{'\n'})
	}
	return listing{principal: v.s.Identity.Principal, request: string(key), policy: v.s.Policy.Revision, catalog: v.snap.ETag, items: string(h.Sum(nil))}, nil
}

func (p *Provider) sign(l listing, pos position) (string, error) {
	body, err := json.Marshal(pos)
	if err != nil {
		return "", fmt.Errorf("builtins: a cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(body) + "." + base64.RawURLEncoding.EncodeToString(mac(p.opt.CursorKey, l, body)), nil
}

// open is the position a cursor holds in a listing of n items, if this
// gateway signed it for that listing within the hour.
func (p *Provider) open(l listing, token string, n int) (position, error) {
	var pos position
	if len(token) > maxCursor || strings.IndexFunc(token, notCursor) >= 0 {
		return pos, errCursor()
	}
	body64, sig64, ok := strings.Cut(token, ".")
	if !ok {
		return pos, errCursor()
	}
	// Strictly: a cursor has one spelling, never another that decodes to
	// the same bytes.
	body, err := strictBase64.DecodeString(body64)
	if err != nil {
		return pos, errCursor()
	}
	sig, err := strictBase64.DecodeString(sig64)
	if err != nil {
		return pos, errCursor()
	}
	signed := hmac.Equal(sig, mac(p.opt.CursorKey, l, body))
	if !signed && len(p.opt.PreviousCursorKey) > 0 {
		signed = hmac.Equal(sig, mac(p.opt.PreviousCursorKey, l, body))
	}
	if !signed || json.Unmarshal(body, &pos) != nil {
		return position{}, errCursor()
	}
	if !p.opt.Clock.Now().Before(time.UnixMilli(pos.Expires)) || pos.Offset < 0 || pos.Offset > n {
		return position{}, errCursor()
	}
	return pos, nil
}

// notCursor reports whether a character is outside a cursor's: base64url
// and the dot between its parts. The decoder would skip a line break.
func notCursor(r rune) bool {
	return !('A' <= r && r <= 'Z' || 'a' <= r && r <= 'z' || '0' <= r && r <= '9' || r == '-' || r == '_' || r == '.')
}

// mac signs a cursor's body and everything it is bound to. Each part is
// written with its length, so no two bindings write the same bytes.
func mac(key []byte, l listing, body []byte) []byte {
	h := hmac.New(sha256.New, key)
	for _, part := range []string{"atlas-cursor/1", l.principal, l.request, l.policy, l.catalog, l.items, string(body)} {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(part)))
		h.Write(n[:])
		h.Write([]byte(part))
	}
	return h.Sum(nil)
}

// window is one page of a listing whose items ids name in order: from the
// request's cursor, at most size items, and the cursor of the next page.
func (p *Provider) window(v *view, req *ijson.Object, ids []string, size int) (from, to int, next *string, err error) {
	l, err := listingOf(v, req, ids)
	if err != nil {
		return 0, 0, nil, err
	}
	if raw, ok := req.Get("cursor"); ok {
		token, isString := raw.(string)
		if !isString {
			return 0, 0, nil, errCursor()
		}
		pos, err := p.open(l, token, len(ids))
		if err != nil {
			return 0, 0, nil, err
		}
		from = pos.Offset
	}
	size = min(max(size, 1), maxPage)
	to = min(len(ids), from+size)
	if to < len(ids) {
		t, err := p.sign(l, position{Offset: to, Expires: p.opt.Clock.Now().Add(cursorLife).UnixMilli()})
		if err != nil {
			return 0, 0, nil, err
		}
		next = &t
	}
	return from, to, next, nil
}
