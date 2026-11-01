package builtins

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"atlas.local/gateway/core/registry"
	"atlas.local/sdk/go/kit/clock"
	"atlas.local/sdk/go/protocol"
)

// §6.7.9, R076: replicas that share a key accept each other's cursors; one
// rotated to a new key still accepts the old key's for their hour; a short
// key is refused at start.
func TestR076ReplicasShareTheirCursorKey(t *testing.T) {
	w := NewWorld(t)
	next, _ := firstPage(t, w)
	body := `{"page_size": 2, "cursor": "` + next + `"}`
	other := w.NewProvider(Options{CursorKey: w.Key})
	pageOf(t, w, Call{Who: Ana, Address: "atlas.catalog@v1:fetch", Body: body, Provider: other})
	stranger := w.NewProvider(Options{CursorKey: []byte(strings.Repeat("k", 32))})
	refusedAtCursor(t, w, Call{Who: Ana, Address: "atlas.catalog@v1:fetch", Body: body, Provider: stranger}, "another key")
	rotated := w.NewProvider(Options{CursorKey: []byte(strings.Repeat("n", 32)), PreviousCursorKey: w.Key})
	pageOf(t, w, Call{Who: Ana, Address: "atlas.catalog@v1:fetch", Body: body, Provider: rotated})
	for _, bad := range []Options{
		{Registry: w.Registry, CursorKey: []byte("short")},
		{Registry: w.Registry, CursorKey: w.Key, PreviousCursorKey: []byte("short")},
		{Registry: w.Registry, PreviousCursorKey: w.Key},
		{CursorKey: w.Key},
	} {
		if _, err := New(bad); err == nil {
			t.Errorf("New accepted %+v", bad)
		}
	}
}

// AGENTS.md (fuzz every parser: cursors), R076: no token, however made, is
// opened as a position unless this provider signed it for this listing,
// and opening one never panics.
func FuzzCursor(f *testing.F) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	p, err := New(Options{Registry: registry.New(), CursorKey: []byte("0123456789abcdef0123456789abcdef"), Clock: clock.NewManual(now)})
	if err != nil {
		f.Fatal(err)
	}
	l := listing{principal: "p", request: `{"page_size":2}`, policy: "pol-1", catalog: "c", items: "i"}
	good, err := p.sign(l, position{Offset: 2, Expires: now.Add(time.Hour).UnixMilli()})
	if err != nil {
		f.Fatal(err)
	}
	body, _ := json.Marshal(position{Offset: 1, Expires: 1 << 62})
	for _, seed := range []string{good, "", ".", "x.y", good + "x", strings.Replace(good, ".", "..", 1), string(body)} {
		f.Add(seed, 5)
	}
	f.Fuzz(func(t *testing.T, token string, n int) {
		pos, err := p.open(l, token, n)
		if err != nil {
			if prob, ok := err.(*protocol.Problem); !ok || prob.Code != protocol.CodeInvalidRequest || prob.Target != "/cursor" {
				t.Fatalf("a refusal that is not invalid_request at /cursor: %v", err)
			}
			return
		}
		if token != good || pos.Offset != 2 || n < 2 {
			t.Fatalf("opened %q as %+v for %d items", token, pos, n)
		}
	})
}
