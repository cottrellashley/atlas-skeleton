package atlas

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"atlas.local/sdk/go/engine/fakegw"
	"atlas.local/sdk/go/protocol/grammar"
)

// R090, §9.7.2, CORE-BOOT-002: the gateway is the connect argument, then
// ATLAS_URL, then ATLAS_ENV's, then config.toml's env's; with none of them
// the client refuses to choose.
func TestR090TheGatewayIsChosenInOrder(t *testing.T) {
	isolate(t)
	net := fakegw.NewNetwork(t)
	gws := make([]*fakegw.Gateway, 4)
	for i := range gws {
		gws[i] = fakegw.New(t, fakegw.Options{Network: net})
	}
	home := t.TempDir()
	config := "# the environments\nenv = \"cfg\"\n\n[environments.cfg]\nurl = \"" + gws[3].URL + "\" # the fourth\n\n[environments.envc]\nurl = '" + gws[2].URL + "'\n\n[elsewhere]\nretries = 3\n"
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, tc := range []struct {
		name, url, env, atlasURL, atlasEnv string
		want                               int
	}{
		{"the connect argument", gws[0].URL, "", gws[1].URL, "envc", 0},
		{"ATLAS_URL", "", "", gws[1].URL, "envc", 1},
		{"ATLAS_ENV", "", "", "", "envc", 2},
		{"config.toml's env", "", "", "", "", 3},
		{"an environment given to connect", "", "envc", gws[1].URL, "", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ATLAS_URL", tc.atlasURL)
			t.Setenv("ATLAS_ENV", tc.atlasEnv)
			c, err := Connect(ctx, Options{URL: tc.url, Env: tc.env, Home: home, HTTP: net.Client()})
			if err != nil {
				t.Fatal(err)
			}
			if c.Gateway() != gws[tc.want].URL {
				t.Fatalf("connected to %s, want %s", c.Gateway(), gws[tc.want].URL)
			}
		})
	}
	_, err := Connect(ctx, Options{Home: t.TempDir(), HTTP: net.Client()})
	wantProblem(t, err, "invalid_request")
	_, err = Connect(ctx, Options{Env: "pd", Home: home, HTTP: net.Client()})
	wantProblem(t, err, "invalid_request")
}

// R090: config.toml is read as TOML, and what is not TOML is refused with
// its line.
func TestR090ConfigTOML(t *testing.T) {
	cfg, err := parseConfig([]byte("env = \"dv\"\nenvironments.ac.url = \"https://ac\"\n[environments.\"dv\"]\nurl = \"https://dv/\\u00e9\"\n"))
	if err != nil || cfg.Env != "dv" || cfg.Environments["ac"] != "https://ac" || cfg.Environments["dv"] != "https://dv/\u00e9" {
		t.Fatalf("%+v %v", cfg, err)
	}
	for _, bad := range []string{"env = dv", "env = \"Not An Env\"", "[environments.dv\nurl = \"x\"", "url", "list = [\n", "env = \"a\" junk", "[[t]]", "env = \"unterminated"} {
		if _, err := parseConfig([]byte(bad)); err == nil || !strings.Contains(err.Error(), "line") {
			t.Fatalf("%q: %v", bad, err)
		}
	}
}

// AGENTS.md: every parser is fuzzed. Whatever config.toml holds, the
// reader returns an error or environments with valid names, and never
// panics.
func FuzzConfigTOML(f *testing.F) {
	for _, seed := range []string{"env = \"dv\"\n[environments.dv]\nurl = \"https://x\"\n", "a.b.c = 'x' # c\n", "[x]\ny = 1\n", "env = \"\\U0001F600\"", "k = \"\\u00\""} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		cfg, err := parseConfig(b)
		if err != nil {
			return
		}
		if cfg.Env != "" && grammar.Segment(cfg.Env) != nil {
			t.Fatalf("env %q", cfg.Env)
		}
		for name := range cfg.Environments {
			if grammar.Segment(name) != nil {
				t.Fatalf("environment %q", name)
			}
		}
	})
}
