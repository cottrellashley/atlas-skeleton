package atlas

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"atlas.local/sdk/go/engine/fakegw"
	"atlas.local/sdk/go/protocol"
)

func describes(gw *fakegw.Gateway, by string) int {
	n := 0
	for _, r := range gw.To("atlas.describe:fetch") {
		if strings.Contains(string(r.Body), `"by":"`+by+`"`) {
			n++
		}
	}
	return n
}

func expect(r fakegw.Request) string {
	e, _ := protocol.ParseExactHeader(r.Header.Get(protocol.HeaderProfileExpect))
	return e.String()
}

// D4 (gaps/2.md §3), CORE-DISC-002: a contract is fetched once per exact
// id, then served from memory and, after a restart, from the cache on
// disk, which the current-profile pointer finds without a describe.
func TestD4AContractIsFetchedOncePerExactID(t *testing.T) {
	isolate(t)
	gw := fakegw.New(t, fakegw.Options{})
	home := signedIn(t)
	ctx := context.Background()
	for range 2 { // the second time, a restart: a new client
		c := connect(t, gw, home)
		for range 2 {
			if _, err := c.Call(ctx, "conformance.echo@v1:fetch", map[string]any{"text": "hi"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if n := len(gw.To("atlas.describe:fetch")); n != 1 {
		t.Fatalf("%d describes for one contract, want 1", n)
	}
	b, err := os.ReadFile(filepath.Join(home, "cache", "current-conformance.json"))
	var ptr map[string]string
	if err != nil || json.Unmarshal(b, &ptr) != nil || ptr["conformance.echo@v1"] != "conformance.echo@v1#f17339273f0617c2" {
		t.Fatalf("the pointer file: %s %v", b, err)
	}
	if st, err := os.Stat(filepath.Join(home, "cache", "profiles", "conformance.echo@v1#f17339273f0617c2.json")); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("the cached contract: %v %v", st, err)
	}
}

// D4.3, R02: routing follows the gateway. An answer written under a newer
// profile of the same version moves the pointer: that profile is fetched
// by exact id, and the next call, in this process or after a restart, is
// written under it.
func TestD4ThePointerFollowsAtlasProfile(t *testing.T) {
	isolate(t)
	gw := fakegw.New(t, fakegw.Options{})
	gw.WriteUnder("conformance.echo@v1:fetch", "conformance.echo@v1#4fe7738a97223902")
	home := signedIn(t)
	ctx := context.Background()
	c := connect(t, gw, home)
	r, err := c.Call(ctx, "conformance.echo@v1:fetch", map[string]any{"text": "a"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Exact != "conformance.echo@v1#4fe7738a97223902" || r.Profile.Exact().String() != "conformance.echo@v1#f17339273f0617c2" {
		t.Fatalf("written under %s, delivered in %s", r.Exact, r.Profile.Exact())
	}
	if _, err := c.Call(ctx, "conformance.echo@v1:fetch", map[string]any{"text": "b"}); err != nil {
		t.Fatal(err)
	}
	if _, err := connect(t, gw, home).Call(ctx, "conformance.echo@v1:fetch", map[string]any{"text": "c"}); err != nil {
		t.Fatal(err)
	}
	calls := gw.To("conformance.echo@v1:fetch")
	if expect(calls[0]) != "conformance.echo@v1#f17339273f0617c2" || expect(calls[1]) != "conformance.echo@v1#4fe7738a97223902" || expect(calls[2]) != "conformance.echo@v1#4fe7738a97223902" {
		t.Fatalf("Atlas-Profile-Expect: %s, %s, %s", expect(calls[0]), expect(calls[1]), expect(calls[2]))
	}
	if describes(gw, "id") != 1 || describes(gw, "exact") != 1 {
		t.Fatalf("describes: %d by id, %d by exact; want 1 and 1", describes(gw, "id"), describes(gw, "exact"))
	}
}

// D4.2, R01: describe and guide are live caller state and prose: they
// always ask the gateway.
func TestD4DescribeAndGuideAreNeverCached(t *testing.T) {
	isolate(t)
	gw := fakegw.New(t, fakegw.Options{})
	c := connect(t, gw, signedIn(t))
	ctx := context.Background()
	for range 2 {
		if _, err := c.Describe(ctx, "conformance.echo@v1"); err != nil {
			t.Fatal(err)
		}
		if _, err := c.Guide(ctx, "conformance.echo@v1", "go"); err != nil {
			t.Fatal(err)
		}
	}
	if d, g := len(gw.To("atlas.describe:fetch")), len(gw.To("atlas.guide:fetch")); d != 2 || g != 2 {
		t.Fatalf("%d describes and %d guides, want 2 and 2", d, g)
	}
}

// D4.1: a cached contract whose fingerprint is wrong is removed and
// fetched again by its exact id; D4.3: a pointer to a profile the gateway
// no longer has is dropped, and the call goes on by describe by id.
func TestD4ABadCacheIsFetchedAgain(t *testing.T) {
	isolate(t)
	gw := fakegw.New(t, fakegw.Options{})
	home := signedIn(t)
	ctx := context.Background()
	if _, err := connect(t, gw, home).Call(ctx, "conformance.echo@v1:fetch", map[string]any{"text": "a"}); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(home, "cache", "profiles", "conformance.echo@v1#f17339273f0617c2.json")
	b, _ := os.ReadFile(file)
	// A contract member changed (the binding's address), so its fingerprint
	// no longer matches; prose alone would not (gaps/2.md D2).
	if err := os.WriteFile(file, []byte(strings.Replace(string(b), "/v1/data/conformance.echo@v1:fetch", "/v1/data/conformance.echo@v1:fetcx", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := connect(t, gw, home).Call(ctx, "conformance.echo@v1:fetch", map[string]any{"text": "b"}); err != nil {
		t.Fatal(err)
	}
	if describes(gw, "exact") != 1 || describes(gw, "id") != 1 {
		t.Fatalf("describes: %d by exact, %d by id; want 1 and 1", describes(gw, "exact"), describes(gw, "id"))
	}
	ptr := filepath.Join(home, "cache", "current-conformance.json")
	if err := os.WriteFile(ptr, []byte(`{"conformance.echo@v1": "conformance.echo@v1#0000000000000000"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := connect(t, gw, home).Call(ctx, "conformance.echo@v1:fetch", map[string]any{"text": "c"}); err != nil {
		t.Fatal(err)
	}
	if describes(gw, "id") != 2 {
		t.Fatalf("a pointer to a gone profile was not dropped: %d describes by id", describes(gw, "id"))
	}
}

// D4.3, §9.7.1, R085: a bare id is the capability's current stable
// version, resolved by describe by id once per process and never
// persisted.
func TestD4ABareIDIsTheCurrentVersion(t *testing.T) {
	isolate(t)
	gw := fakegw.New(t, fakegw.Options{})
	home := signedIn(t)
	c := connect(t, gw, home)
	ctx := context.Background()
	for range 2 {
		r, err := c.Call(ctx, "conformance.echo:fetch", map[string]any{"text": "bare"})
		if err != nil {
			t.Fatal(err)
		}
		if r.Profile.Exact().String() != "conformance.echo@v1#f17339273f0617c2" {
			t.Fatalf("a bare id called %s", r.Profile.Exact())
		}
	}
	if describes(gw, "id") != 1 {
		t.Fatalf("%d describes by id, want 1", describes(gw, "id"))
	}
	b, _ := os.ReadFile(filepath.Join(home, "cache", "current-conformance.json"))
	if strings.Contains(string(b), `"conformance.echo"`) {
		t.Fatalf("a bare id was persisted: %s", b)
	}
}
