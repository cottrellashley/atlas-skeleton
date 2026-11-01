package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	atlas "atlas.local/sdk/go"
	"atlas.local/sdk/go/engine/fakegw"
	"atlas.local/sdk/go/protocol"
	"atlas.local/sdk/go/protocol/frame"
)

func isolate(t *testing.T) {
	for _, k := range []string{"ATLAS_URL", "ATLAS_ENV", "ATLAS_TOKEN", "ATLAS_HOME"} {
		t.Setenv(k, "")
	}
}

type outcome struct {
	code           int
	stdout, stderr string
}

// atlasCLI runs the CLI against gw, signed in by atlas login as ct-valid.
func atlasCLI(t *testing.T, gw *fakegw.Gateway, home string, args ...string) outcome {
	t.Helper()
	var out, errs bytes.Buffer
	code := run(append([]string{"--gateway", gw.URL}, args...), strings.NewReader(""), &out, &errs, atlas.Options{Home: home, HTTP: gw.Client()})
	return outcome{code, out.String(), errs.String()}
}

func signedIn(t *testing.T, gw *fakegw.Gateway) string {
	t.Helper()
	home := t.TempDir()
	if o := atlasCLI(t, gw, home, "login", "--token", "ct-valid"); o.code != 0 {
		t.Fatalf("login: %+v", o)
	}
	return home
}

// lastProblem is the problem the CLI printed last on standard error.
func lastProblem(t *testing.T, stderr string) protocol.Problem {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	var p protocol.Problem
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &p); err != nil {
		t.Fatalf("standard error's last line is no problem: %q", stderr)
	}
	return p
}

// R085, §5.3, §9.7.6: flags stand anywhere; a flag or argument error is
// invalid_request with its target and exit 2, never internal; a bare id is
// the current version; catalog takes --effect and access --domain.
func TestR085TheCLIsArguments(t *testing.T) {
	isolate(t)
	gw := fakegw.New(t, fakegw.Options{})
	home := signedIn(t, gw)
	for _, tc := range []struct {
		name   string
		args   []string
		code   int
		target string
		out    string
	}{
		{"an unknown flag", []string{"call", "conformance.echo@v1:fetch", "--requets", "{}"}, 2, "--requets", ""},
		{"a missing @file", []string{"call", "conformance.echo@v1:fetch", "--request", "@" + filepath.Join(home, "missing.json")}, 2, "--request", ""},
		{"a bad --deadline", []string{"call", "conformance.echo@v1:fetch", "--deadline", "soon"}, 2, "--deadline", ""},
		{"no address", []string{"call"}, 2, "call", ""},
		{"an unknown command", []string{"fly"}, 2, "fly", ""},
		{"flags after a guide's id", []string{"guide", "conformance.echo", "--client", "python"}, 0, "", "from python"},
		{"--json after a describe's id", []string{"describe", "conformance.echo", "--json"}, 0, "", `"availability":"available"`},
		{"--json after the words", []string{"search", "an", "echo", "--json"}, 0, "", `"id":"conformance.echo"`},
		{"a bare id", []string{"call", "conformance.echo:fetch", "--request", `{"text":"bare"}`}, 0, "", `"text":"bare"`},
		{"catalog --effect", []string{"catalog", "--effect", "read"}, 0, "", `"id":"conformance.rows"`},
		{"access --domain", []string{"access", "--domain", "conformance"}, 0, "", ""},
		{"an invalid request", []string{"call", "conformance.echo@v1:fetch", "--request", `{"text":""}`}, 2, "/text", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := atlasCLI(t, gw, home, tc.args...)
			if o.code != tc.code {
				t.Fatalf("exit %d, want %d: %+v", o.code, tc.code, o)
			}
			if tc.code != 0 {
				if p := lastProblem(t, o.stderr); p.Code != "invalid_request" || p.Target != tc.target {
					t.Fatalf("the problem %s at %q, want invalid_request at %q", p.Code, p.Target, tc.target)
				}
			}
			if !strings.Contains(o.stdout, tc.out) {
				t.Fatalf("standard output %q lacks %q", o.stdout, tc.out)
			}
		})
	}
}

// R085, ATLAS-CLI-002, §5.3 rule 1: fetch's flags come from the
// capability's request schema when the command runs: --n, a comma list for
// --columns; its --help is built from the profile.
func TestR085FetchFlagsComeFromTheRequestSchema(t *testing.T) {
	isolate(t)
	gw := fakegw.New(t, fakegw.Options{})
	home := signedIn(t, gw)
	o := atlasCLI(t, gw, home, "fetch", "conformance.rows", "--n", "3", "--columns", "i,label", "--json")
	lines := strings.Split(strings.TrimSpace(o.stdout), "\n")
	if o.code != 0 || len(lines) != 4 || lines[0] != `{"i":0,"label":"row-0"}` || !strings.HasPrefix(lines[3], `{"$atlas":"end","rows":3,"truncated":false`) {
		t.Fatalf("%+v", o)
	}
	o = atlasCLI(t, gw, home, "fetch", "conformance.rows", "--help")
	if o.code != 0 || !strings.Contains(o.stderr, "--n (integer, required)") || !strings.Contains(o.stderr, "--columns") {
		t.Fatalf("help: %+v", o)
	}
}

// R001, STREAM-004, §5.3 rule 2: a truncated table is visible in the CLI's
// JSON output, in both its forms: its end line says truncated, and the
// warning output_limit_exceeded is a JSON line on standard error; the exit
// code is 0, since a truncation is a warning.
func TestR001TruncationIsVisibleInTheJSONOutput(t *testing.T) {
	isolate(t)
	gw := fakegw.New(t, fakegw.Options{})
	gw.Handle("conformance.rows@v1:fetch", func(c *fakegw.Call) {
		p := protocol.NewProblem(protocol.CodeOutputLimitExceeded, "max_rows is 2")
		p.Next, p.RequestID = "Ask for fewer rows.", "r1"
		c.Rows(2, fakegw.Row, frame.End{Truncated: true, Usage: []byte(`{"rows":2}`), Problem: p})
	})
	home := signedIn(t, gw)
	for _, form := range [][]string{nil, {"--json"}} {
		o := atlasCLI(t, gw, home, append([]string{"call", "conformance.rows@v1:fetch", "--request", `{"n":5}`}, form...)...)
		lines := strings.Split(strings.TrimSpace(o.stdout), "\n")
		if o.code != 0 || len(lines) != 3 || !strings.Contains(lines[2], `"truncated":true`) || !strings.Contains(lines[2], `"output_limit_exceeded"`) {
			t.Fatalf("%v: %+v", form, o)
		}
		var w struct{ Warning atlas.Warning }
		if err := json.Unmarshal([]byte(strings.TrimSpace(o.stderr)), &w); err != nil || w.Warning.Code != "output_limit_exceeded" {
			t.Fatalf("%v: standard error %q", form, o.stderr)
		}
	}
}

// R001, CORE-DEPR-001: a deprecation is a warning on standard error for a
// record too.
func TestR001ADeprecationIsAWarningOnEveryPath(t *testing.T) {
	isolate(t)
	gw := fakegw.New(t, fakegw.Options{})
	gw.Handle("conformance.echo@v1:fetch", func(c *fakegw.Call) {
		c.W.Header().Set("Deprecation", protocol.FormatDeprecation(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)))
		c.JSON(fakegw.Echo(c.Body))
	})
	home := signedIn(t, gw)
	o := atlasCLI(t, gw, home, "call", "conformance.echo@v1:fetch", "--request", `{"text":"x"}`, "--json")
	if o.code != 0 || !strings.Contains(o.stderr, `{"warning":{"code":"deprecated"`) {
		t.Fatalf("%+v", o)
	}
}

// R085, STREAM-003: a table that fails after its first rows prints those
// rows and its end line with the problem, and exits with the problem's code.
func TestR085AFailedStreamEndsWithItsProblem(t *testing.T) {
	isolate(t)
	gw := fakegw.New(t, fakegw.Options{})
	gw.Handle("conformance.rows@v1:fetch", func(c *fakegw.Call) {
		p := protocol.NewProblem(protocol.CodeUpstreamFailed, "the warehouse went away")
		p.Next, p.RequestID = "Try again.", "r1"
		c.Rows(2, fakegw.Row, frame.End{Problem: p})
	})
	home := signedIn(t, gw)
	o := atlasCLI(t, gw, home, "call", "conformance.rows@v1:fetch", "--request", `{"n":2}`)
	lines := strings.Split(strings.TrimSpace(o.stdout), "\n")
	if o.code != 1 || len(lines) != 3 || !strings.Contains(lines[2], `"upstream_failed"`) || lastProblem(t, o.stderr).Code != "upstream_failed" {
		t.Fatalf("%+v", o)
	}
}

// PR06, R026, CORE-DEADLINE-001: --deadline bounds the call in the CLI:
// a gateway that never answers ends it at its deadline, exit 7.
func TestPR06TheCLIEndsAtItsDeadline(t *testing.T) {
	isolate(t)
	synctest.Test(t, func(t *testing.T) {
		gw := fakegw.New(t, fakegw.Options{})
		release := make(chan struct{})
		t.Cleanup(func() { close(release) })
		gw.Handle("conformance.echo@v1:fetch", func(c *fakegw.Call) { c.Hold(release) })
		home := signedIn(t, gw)
		start := time.Now()
		o := atlasCLI(t, gw, home, "call", "conformance.echo@v1:fetch", "--request", `{"text":"x"}`, "--deadline", "2s")
		if o.code != 7 || time.Since(start) != 2*time.Second || lastProblem(t, o.stderr).Code != "deadline_exceeded" {
			t.Fatalf("exit %d after %v: %+v", o.code, time.Since(start), o)
		}
	})
}

// R26: atlas login binds the sign-in to the gateway it was given, and
// logout forgets it; a sign-in bound elsewhere is kept.
func TestR26LoginBindsAndLogoutForgets(t *testing.T) {
	isolate(t)
	net := fakegw.NewNetwork(t)
	gw := fakegw.New(t, fakegw.Options{Network: net})
	other := fakegw.New(t, fakegw.Options{Network: net})
	home := signedIn(t, gw)
	b, err := os.ReadFile(filepath.Join(home, "state", "token-conformance.json"))
	if err != nil || !strings.Contains(string(b), `"gateway":"`+gw.URL+`"`) {
		t.Fatalf("the sign-in: %s %v", b, err)
	}
	if o := atlasCLI(t, other, home, "whoami"); o.code != 3 {
		t.Fatalf("another gateway used the sign-in: %+v", o)
	}
	if o := atlasCLI(t, other, home, "logout"); o.code != 2 {
		t.Fatalf("logout of another gateway: %+v", o)
	}
	if o := atlasCLI(t, gw, home, "logout"); o.code != 0 {
		t.Fatalf("logout: %+v", o)
	}
	if o := atlasCLI(t, gw, home, "whoami"); o.code != 3 {
		t.Fatalf("after logout: %+v", o)
	}
}
