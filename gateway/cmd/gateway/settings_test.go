package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/httpserver"
)

// goodSettings are settings the production gateway starts with, as
// PLUGGING-IN.md gives them, with the cursor key's variable set.
func goodSettings(t *testing.T) map[string]any {
	t.Helper()
	t.Setenv("ATLAS_TEST_CURSOR_KEY", strings.Repeat("k", 32))
	t.Setenv("ATLAS_TEST_DSN", "warehouse")
	return map[string]any{
		"environment": "dv",
		"url":         "https://atlas-dv.example.com",
		"sign_in": map[string]any{
			"authority": "https://login.microsoftonline.com/t1/v2.0", "client_id": "atlas-cli", "scopes": []string{"api://atlas/.default"},
			"token_endpoint":                "https://login.microsoftonline.com/t1/oauth2/v2.0/token",
			"device_authorization_endpoint": "https://login.microsoftonline.com/t1/oauth2/v2.0/devicecode",
		},
		"entra": map[string]any{
			"issuer": "https://login.microsoftonline.com/t1/v2.0", "audiences": []string{"api://atlas"},
			"jwks_url": "https://login.microsoftonline.com/t1/discovery/v2.0/keys", "tenant": "t1",
		},
		"policy_file": "/etc/atlas/policy.json",
		"audit_file":  "/var/log/atlas/audit.jsonl",
		"cursor_key":  map[string]any{"env": "ATLAS_TEST_CURSOR_KEY"},
	}
}

func writeSettings(t *testing.T, s map[string]any) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func withWarehouse(s map[string]any, w map[string]any) map[string]any {
	base := map[string]any{"driver": testDriver, "dsn": map[string]any{"env": "ATLAS_TEST_DSN"}, "dialect": "snowflake",
		"registered": [][]string{{"HARBOUR", "HOLDINGS"}, {"HARBOUR", "REF"}, {"HARBOUR", "OPS"}}}
	for k, v := range w {
		if v == nil {
			delete(base, k)
		} else {
			base[k] = v
		}
	}
	s["warehouse"] = base
	return s
}

// R125, R069, R124, R047: the production gateway's settings are checked
// when it starts, and each unsafe or broken value is refused, named, with
// what to give instead; every problem at once.
func TestR125UnsafeSettingsAreRefusedAndNamed(t *testing.T) {
	if _, err := load(writeSettings(t, goodSettings(t))); err != nil {
		t.Fatalf("good settings: %v", err)
	}
	for name, c := range map[string]struct {
		change func(map[string]any)
		names  []string
	}{
		// R125: a bootstrap every client would refuse.
		"an environment that is not a name": {func(s map[string]any) { s["environment"] = "Dev" }, []string{`environment "Dev"`}},
		"no environment":                    {func(s map[string]any) { delete(s, "environment") }, []string{"environment is required"}},
		"a URL with no scheme":              {func(s map[string]any) { s["url"] = "atlas-dv.example.com/" }, []string{"url:"}},
		"no sign-in but a token endpoint": {func(s map[string]any) {
			s["sign_in"] = map[string]any{"token_endpoint": "https://login.microsoftonline.com/t1/oauth2/v2.0/token"}
		}, []string{"sign_in.authority", "sign_in.client_id", "sign_in.scopes"}},
		"an empty scope": {func(s map[string]any) { s["sign_in"].(map[string]any)["scopes"] = []string{" "} }, []string{"sign_in.scopes[0]"}},
		// §9.6.1: HTTPS only.
		"a plain-http URL":    {func(s map[string]any) { s["url"] = "http://atlas-dv.example.com" }, []string{"url:", "https"}},
		"a plain-http issuer": {func(s map[string]any) { s["entra"].(map[string]any)["issuer"] = "http://login.example.com/t1/v2.0" }, []string{"entra.issuer"}},
		"a JWKS URL with a query": {func(s map[string]any) {
			s["entra"].(map[string]any)["jwks_url"] = "https://login.microsoftonline.com/keys?x=1"
		}, []string{"entra.jwks_url"}},
		"no audience": {func(s map[string]any) { s["entra"].(map[string]any)["audiences"] = []string{} }, []string{"entra.audiences"}},
		// A production gateway that calls itself the development one.
		"the local environment":       {func(s map[string]any) { s["environment"] = "local" }, []string{"emulator handovers"}},
		"the conformance environment": {func(s map[string]any) { s["environment"] = "conformance" }, []string{"emulator handovers"}},
		// R047: no audit, never a silent default.
		"no audit file": {func(s map[string]any) { delete(s, "audit_file") }, []string{"audit_file is required"}},
		// R124: cursors every replica accepts.
		"no cursor key": {func(s map[string]any) { delete(s, "cursor_key") }, []string{"cursor_key is required"}},
		"a cursor key variable that is not set": {func(s map[string]any) {
			s["cursor_key"] = map[string]any{"env": "ATLAS_TEST_UNSET"}
		}, []string{"cursor_key: the environment variable ATLAS_TEST_UNSET is not set"}},
		"a short previous cursor key": {func(s map[string]any) {
			s["cursor_key_previous"] = map[string]any{"env": "ATLAS_TEST_SHORT"}
		}, []string{"cursor_key_previous", "at least 32"}},
		// R069: the warehouse is bounded, Snowflake's, and given whole.
		"a dialect this gateway does not speak": {func(s map[string]any) { withWarehouse(s, map[string]any{"dialect": "duckdb"}) }, []string{`warehouse.dialect "duckdb"`}},
		"no registered objects":                 {func(s map[string]any) { withWarehouse(s, map[string]any{"registered": nil}) }, []string{"warehouse.registered is required"}},
		"a registered object of one part": {func(s map[string]any) {
			withWarehouse(s, map[string]any{"registered": [][]string{{"HARBOUR"}}})
		}, []string{"warehouse.registered[0]"}},
		"sessions idle past Snowflake's timeout": {func(s map[string]any) { withWarehouse(s, map[string]any{"max_idle_seconds": 4 * 3600}) }, []string{"warehouse.max_idle_seconds"}},
		"more idle sessions than open ones":      {func(s map[string]any) { withWarehouse(s, map[string]any{"max_open": 4, "max_idle": 5}) }, []string{"warehouse.max_idle 5"}},
		"a negative pool":                        {func(s map[string]any) { withWarehouse(s, map[string]any{"max_open": -1}) }, []string{"pool setting"}},
		"no DSN":                                 {func(s map[string]any) { withWarehouse(s, map[string]any{"dsn": nil}) }, []string{"warehouse.dsn is required"}},
		"warehouse settings but no driver":       {func(s map[string]any) { withWarehouse(s, map[string]any{"driver": nil}) }, []string{"no driver"}},
		// The rest.
		"a listen address with no port": {func(s map[string]any) { s["listen"] = "localhost" }, []string{"listen"}},
		"a domain twice":                {func(s map[string]any) { s["domains"] = []string{"data", "data"} }, []string{`domains names "data" twice`}},
		"an unknown member":             {func(s map[string]any) { s["audit_files"] = "x" }, []string{"unknown field"}},
		"many problems at once": {func(s map[string]any) {
			s["environment"], s["url"] = "Dev", "http://x"
			delete(s, "cursor_key")
		}, []string{"environment", "url:", "cursor_key"}},
	} {
		t.Run(name, func(t *testing.T) {
			s := goodSettings(t)
			t.Setenv("ATLAS_TEST_SHORT", "short")
			c.change(s)
			_, err := load(writeSettings(t, s))
			if err == nil {
				t.Fatal("loaded")
			}
			for _, n := range c.names {
				if !strings.Contains(err.Error(), n) {
					t.Errorf("the error does not name %q:\n%v", n, err)
				}
			}
		})
	}
}

// R124, §6.7.9: the cursor key and the key it replaced are read once, from
// the variables the settings name.
func TestR124TheCursorKeysAreReadOnce(t *testing.T) {
	s := goodSettings(t)
	t.Setenv("ATLAS_TEST_PREVIOUS", strings.Repeat("p", 40))
	s["cursor_key_previous"] = map[string]any{"env": "ATLAS_TEST_PREVIOUS"}
	got, err := load(writeSettings(t, s))
	if err != nil {
		t.Fatal(err)
	}
	if got.cursorKey != strings.Repeat("k", 32) || got.previousCursorKey != strings.Repeat("p", 40) {
		t.Fatalf("%q %q", got.cursorKey, got.previousCursorKey)
	}
}

// R123, §6.7.15: the server is §6.7.15's, on :8000 unless the settings say
// otherwise, with the settings' shutdown delay and grace.
func TestR123TheServerIsTheSpecs(t *testing.T) {
	s, err := load(writeSettings(t, goodSettings(t)))
	if err != nil {
		t.Fatal(err)
	}
	want := httpserver.Defaults()
	if got := s.serving(); got != want || got.Addr != ":8000" || got.MaxHeaderBytes != 64<<10 {
		t.Fatalf("%+v", got)
	}
	in := goodSettings(t)
	in["listen"], in["server"] = "127.0.0.1:9000", map[string]any{"shutdown_delay_seconds": 10, "grace_seconds": 60}
	if s, err = load(writeSettings(t, in)); err != nil {
		t.Fatal(err)
	}
	want.Addr, want.ShutdownDelay, want.GracePeriod = "127.0.0.1:9000", 10*time.Second, time.Minute
	if got := s.serving(); got != want {
		t.Fatalf("%+v", got)
	}
}

// R054, R069 (the settings' part): the warehouse pool is bounded, keeps as
// many sessions idle as it may open, and retires a session well before
// Snowflake's 4-hour idle timeout, whether or not the settings say so.
func TestR054ThePoolHasSafeDefaults(t *testing.T) {
	got := warehouse{}.pool()
	if got.maxOpen != 8 || got.maxIdle != 8 || got.maxIdleTime != 30*time.Minute || got.maxIdleTime >= snowflakeSessionTimeout || got.maxLifetime != time.Hour {
		t.Fatalf("defaults %+v", got)
	}
	got = warehouse{MaxOpen: 20, MaxIdle: 5, MaxIdleSeconds: 600, MaxLifetimeSeconds: 1800}.pool()
	if got.maxOpen != 20 || got.maxIdle != 5 || got.maxIdleTime != 10*time.Minute || got.maxLifetime != 30*time.Minute {
		t.Fatalf("given %+v", got)
	}
	if got := (warehouse{MaxOpen: 3}).pool(); got.maxIdle != 3 {
		t.Fatalf("idle follows open: %+v", got)
	}
}
