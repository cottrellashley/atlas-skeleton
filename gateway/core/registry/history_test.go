package registry

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol/profile"
)

const firstEvolution = `"evolution": {"lens": [], "changelog": [{"date": "2026-10-01", "change": "First."}]}`

// after is echo's next profile within @v1: the profile before it named,
// one more changelog entry, and the request members extra adds, at address.
func after(t *testing.T, prev *profile.Profile, address, extra string, n int) *profile.Profile {
	t.Helper()
	changelog := `{"date": "2026-10-01", "change": "First."}`
	for i := 2; i <= n; i++ {
		day := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, i-1).Format(time.DateOnly)
		changelog += fmt.Sprintf(`, {"date": "%s", "change": "Change %d."}`, day, i)
	}
	doc := strings.Replace(echo(address, extra), firstEvolution, `"evolution": {"previous_profile": "`+prev.Exact().String()+`", "lens": [], "changelog": [`+changelog+`]}`, 1)
	return load(t, doc)
}

func optional(name string) string {
	return `, "` + name + `": {"type": ["boolean", "null"], "default": null, "description": "` + name + `."}`
}

const at = "/v1/demo/demo.echo@v1:fetch"

func chainOf(t *testing.T, r *Registry) []string {
	t.Helper()
	c := r.Snapshot().Lookup("demo.echo@v1")
	if c == nil {
		t.Fatal("demo.echo@v1 is not served")
	}
	var out []string
	for _, p := range c.Chain {
		out = append(out, p.Exact().String())
	}
	return out
}

func exacts(ps ...*profile.Profile) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Exact().String())
	}
	return out
}

// PR05, R045, ATLAS-RESOLVE-003, §3.1 ("a profile is served by fingerprint
// for as long as anything may hold it"): every profile a version served
// stays in its chain, however many replace it, so a request written under
// the first still resolves after the third.
func TestPR05EveryProfileOfAVersionStaysInItsChain(t *testing.T) {
	r := New()
	v1 := load(t, echo(at, ""))
	v2 := after(t, v1, at, optional("loud"), 2)
	v3 := after(t, v2, at, optional("loud")+optional("quiet"), 3)
	for _, p := range []*profile.Profile{v1, v2, v3} {
		if err := r.Register(Entry{Profiles: []*profile.Profile{p}, Provider: nobody{}}); err != nil {
			t.Fatal(err)
		}
	}
	if got, want := chainOf(t, r), exacts(v1, v2, v3); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("chain %v, want %v", got, want)
	}
	s := r.Snapshot()
	if c := s.Lookup("demo.echo@v1"); c.Current != v3 || s.Profile(v1.Exact().String()) != v1 {
		t.Fatalf("current %s", c.Current.Exact())
	}
	// A registration of the whole chain, as a restart gives it, changes
	// nothing.
	if err := r.Register(Entry{Profiles: []*profile.Profile{v1, v2, v3}, Provider: nobody{}}); err != nil {
		t.Fatal(err)
	}
	if got := chainOf(t, r); len(got) != 3 {
		t.Fatalf("a repeat registration: %v", got)
	}
}

// R045, R03, ATLAS-VERSION-001: a deploy is held to the gate exactly as a
// hot replace is. A fresh registry given a version's history classifies
// each profile against the one before it, so a breaking change is refused
// whether or not the gateway was running when it was made.
func TestR045ADeployIsHeldToTheGate(t *testing.T) {
	v1 := load(t, echo(at, ""))
	breaking := load(t, strings.Replace(strings.Replace(echo(at, `, "loud": {"type": "boolean", "description": "Shout."}`),
		`"required": ["text"]`, `"required": ["text", "loud"]`, 1),
		firstEvolution, `"evolution": {"previous_profile": "`+v1.Exact().String()+`", "lens": [], "changelog": [{"date": "2026-10-01", "change": "First."}, {"date": "2026-10-02", "change": "Loud."}]}`, 1))
	var refused *Refused
	if err := New().Register(Entry{Profiles: []*profile.Profile{v1, breaking}, Provider: nobody{}}); !errors.As(err, &refused) || !strings.Contains(err.Error(), "became required") {
		t.Fatalf("a breaking change at start: %v", err)
	}
	additive := after(t, v1, at, optional("loud"), 2)
	r := New()
	if err := r.Register(Entry{Profiles: []*profile.Profile{v1, additive}, Provider: nobody{}}); err != nil {
		t.Fatal(err)
	}
	if got := chainOf(t, r); fmt.Sprint(got) != fmt.Sprint(exacts(v1, additive)) {
		t.Fatalf("chain %v", got)
	}
}

// R045: a profile that names its previous profile is admitted only with it.
// A deploy that ships the new profile without the version's history would
// otherwise skip the classification R03 depends on.
func TestR045APreviousProfileTheGatewayDoesNotHoldIsRefused(t *testing.T) {
	v1 := load(t, echo(at, ""))
	v2 := after(t, v1, at, optional("loud"), 2)
	err := New().Register(Entry{Profiles: []*profile.Profile{v2}, Provider: nobody{}})
	if err == nil || !strings.Contains(err.Error(), "names its previous profile "+v1.Exact().String()) {
		t.Fatalf("got %v", err)
	}
}

// R03: the profile a version serves only moves forward. Serving an earlier
// one again would withdraw what later callers were promised.
func TestR045AVersionsProfileOnlyMovesForward(t *testing.T) {
	r := New()
	v1 := load(t, echo(at, ""))
	v2 := after(t, v1, at, optional("loud"), 2)
	if err := r.Register(Entry{Profiles: []*profile.Profile{v1, v2}, Provider: nobody{}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Register(Entry{Profiles: []*profile.Profile{v1}, Provider: nobody{}}); err == nil || !strings.Contains(err.Error(), "only moves forward") {
		t.Fatalf("got %v", err)
	}
	// A fork, a profile that replaces v1 while v2 is served, is refused by
	// the gate: it must name v2.
	fork := after(t, v1, at, optional("quiet"), 2)
	if err := r.Register(Entry{Profiles: []*profile.Profile{fork}, Provider: nobody{}}); err == nil || !strings.Contains(err.Error(), "previous_profile") {
		t.Fatalf("a fork: %v", err)
	}
}

// D2 (gaps/2.md), §9.4.2 rule 2: under one exact id the newest document is
// served, by exact and by id. Two projections under one fingerprint are
// refused.
func TestD2TheNewestDocumentOfAnExactIdIsServed(t *testing.T) {
	r := New()
	v1 := load(t, echo(at, ""))
	reworded := load(t, strings.Replace(echo(at, ""), "Says what it is told.", "Repeats what it is told.", 1))
	if reworded.FP.Value != v1.FP.Value {
		t.Fatal("prose is outside the fingerprint")
	}
	for _, p := range []*profile.Profile{v1, reworded} {
		if err := r.Register(Entry{Profiles: []*profile.Profile{p}, Provider: nobody{}}); err != nil {
			t.Fatal(err)
		}
	}
	s := r.Snapshot()
	if c := s.Lookup("demo.echo@v1"); c.Current != reworded || len(c.Chain) != 1 || s.Profile(v1.Exact().String()) != reworded {
		t.Fatalf("current %v, chain %d", c.Current == reworded, len(c.Chain))
	}
	collision := load(t, echo(at, optional("loud")))
	collision.FP = v1.FP
	if err := r.Register(Entry{Profiles: []*profile.Profile{collision}, Provider: nobody{}}); err == nil || !strings.Contains(err.Error(), "two contracts") {
		t.Fatalf("two projections under one fingerprint: %v", err)
	}
}

// D5 (gaps/2.md), R02, §4.2: every binding of every profile of a live
// version stays routed, so a client holding an older profile still reaches
// the interaction; and no other capability or interaction may take one.
func TestD5EveryBindingOfEveryProfileIsRouted(t *testing.T) {
	r := New()
	v1 := load(t, echo(at, ""))
	moved := "/v1/demo/echo/v1/fetch"
	v2 := after(t, v1, moved, "", 2)
	for _, p := range []*profile.Profile{v1, v2} {
		if err := r.Register(Entry{Profiles: []*profile.Profile{p}, Provider: nobody{}}); err != nil {
			t.Fatal(err)
		}
	}
	s := r.Snapshot()
	for _, path := range []string{at, moved} {
		rt, ok := s.Route(path)
		if !ok || rt.Capability.Current != v2 || rt.Interaction != "fetch" || rt.Binding.Address != path {
			t.Fatalf("%s: %+v %v", path, rt, ok)
		}
	}
	other := load(t, strings.Replace(echo(at, ""), `"id": "demo.echo"`, `"id": "demo.other"`, 1))
	if err := r.Register(Entry{Profiles: []*profile.Profile{other}, Provider: nobody{}}); err == nil || !strings.Contains(err.Error(), "already") {
		t.Fatalf("an old binding taken by another capability: %v", err)
	}
}

// R102, §9.3 rule 1: a profile the gateway serves is a valid profile
// document. A visibility, lifecycle or effect outside its enum, or a
// missing visibility, is refused at registration, never read as listed.
func TestR102RegistrationHoldsAProfileToItsDocument(t *testing.T) {
	for _, c := range []struct{ name, from, to, why string }{
		{"a visibility in the wrong case", `"visibility": "listed"`, `"visibility": "Confidential"`, "/properties/visibility"},
		{"no visibility", `, "visibility": "listed"`, ``, "/properties/visibility"},
		{"an effect no client knows", `"effect": "read"`, `"effect": "reads"`, "/interactions/fetch/effect"},
		{"a lifecycle no client knows", `"lifecycle": "stable"`, `"lifecycle": "live"`, "/properties/lifecycle"},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := load(t, strings.Replace(echo(at, ""), c.from, c.to, 1))
			err := New().Register(Entry{Profiles: []*profile.Profile{p}, Provider: nobody{}})
			if err == nil || !strings.Contains(err.Error(), c.why) || !strings.Contains(err.Error(), "profile.schema.json") {
				t.Fatalf("got %v", err)
			}
		})
	}
}

// R082, §3.1.4a, ATLAS-VISION-002: registration runs the agent-readiness
// check, and a profile that fails it is served only as preview, with every
// reason. A built-in is the protocol's, exempt as from the lint (A-016,
// A-183).
func TestR082AProfileThatIsNotReadyIsServedAsPreview(t *testing.T) {
	r := New()
	ready := load(t, echo(at, ""))
	unready := load(t, strings.Replace(strings.Replace(echo("/v1/demo/demo.bare@v1:fetch", ""), `"id": "demo.echo"`, `"id": "demo.bare"`, 1),
		`"usage": {"when": "You need an echo.", "examples": [{"title": "Hello", "interaction": "fetch", "request": {"text": "hello"}}]},`, ``, 1))
	if err := r.Register(Entry{Profiles: []*profile.Profile{ready}, Provider: nobody{}}, Entry{Profiles: []*profile.Profile{unready}, Provider: nobody{}}); err != nil {
		t.Fatal(err)
	}
	s := r.Snapshot()
	if c := s.Lookup("demo.echo@v1"); len(c.Unready) != 0 || c.Lifecycle() != "stable" {
		t.Fatalf("a ready profile: %v %s", c.Unready, c.Lifecycle())
	}
	if c := s.Lookup("demo.bare@v1"); len(c.Unready) == 0 || !strings.Contains(strings.Join(c.Unready, "\n"), "usage.when") || c.Lifecycle() != "preview" {
		t.Fatalf("an unready profile: %v %s", c.Unready, c.Lifecycle())
	}
	builtin := load(t, strings.Replace(strings.Replace(strings.Replace(echo("/v1/atlas/atlas.echo:fetch", ""), `"id": "demo.echo"`, `"id": "atlas.echo"`, 1), `"domain": "demo"`, `"domain": "atlas"`, 1),
		`"usage": {"when": "You need an echo.", "examples": [{"title": "Hello", "interaction": "fetch", "request": {"text": "hello"}}]},`, ``, 1))
	if err := r.Register(Entry{Profiles: []*profile.Profile{builtin}, Provider: nobody{}}); err != nil {
		t.Fatal(err)
	}
	if c := r.Snapshot().Lookup("atlas.echo@v1"); len(c.Unready) != 0 || c.Lifecycle() != "stable" {
		t.Fatalf("a built-in: %v %s", c.Unready, c.Lifecycle())
	}
}

// Bounded resources: a version holds at most MaxChain profiles. Past that,
// a change is a new version, which is what so many changes are.
func TestAVersionsChainIsBounded(t *testing.T) {
	r := New()
	prev := load(t, echo(at, ""))
	if err := r.Register(Entry{Profiles: []*profile.Profile{prev}, Provider: nobody{}}); err != nil {
		t.Fatal(err)
	}
	extra := ""
	var err error
	for i := 2; i <= MaxChain+1 && err == nil; i++ {
		extra += optional(fmt.Sprintf("f%d", i))
		next := after(t, prev, at, extra, i)
		err = r.Register(Entry{Profiles: []*profile.Profile{next}, Provider: nobody{}})
		prev = next
	}
	if err == nil || !strings.Contains(err.Error(), "new version") || len(chainOf(t, r)) != MaxChain {
		t.Fatalf("got %v, a chain of %d", err, len(chainOf(t, r)))
	}
}

// A capability's reach is what its registration says; a registration that
// says nothing is held to rule 3 (06-3), as the gateway's own identity.
func TestAnEntrysReachIsKept(t *testing.T) {
	r := New()
	if err := r.Register(Entry{Profiles: []*profile.Profile{load(t, echo(at, ""))}, Provider: nobody{}}); err != nil {
		t.Fatal(err)
	}
	if c := r.Snapshot().Lookup("demo.echo@v1"); c.Reach != AsGateway {
		t.Fatalf("reach %v", c.Reach)
	}
	if err := r.Register(Entry{Profiles: []*profile.Profile{load(t, echo(at, ""))}, Provider: nobody{}, Reach: AsCaller}); err != nil {
		t.Fatal(err)
	}
	if c := r.Snapshot().Lookup("demo.echo@v1"); c.Reach != AsCaller {
		t.Fatalf("reach %v", c.Reach)
	}
}
