package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"atlas.local/gateway/core/builtins"
	"atlas.local/gateway/edge"
	"atlas.local/sdk/go/kit/httpserver"
)

// Settings are the gateway's configuration: one JSON file, its secrets
// named by the environment variables that hold them, never written in it.
// They are read once, at start, and every one is checked then: a gateway
// that would serve a bootstrap no client accepts, sign cursors no other
// replica accepts, keep no audit, or open an unbounded warehouse pool does
// not start (R069, R124, R125, R047).
type Settings struct {
	Environment string    `json:"environment"`
	Listen      string    `json:"listen"`
	URL         string    `json:"url"`
	SignIn      signIn    `json:"sign_in"`
	Entra       entraConf `json:"entra"`
	PolicyFile  string    `json:"policy_file"`
	AuditFile   string    `json:"audit_file"`
	Warehouse   warehouse `json:"warehouse"`
	Domains     []string  `json:"domains"`
	Exclude     []string  `json:"exclude"`
	CursorKey   envSecret `json:"cursor_key"`
	// CursorKeyPrevious is the key cursor_key replaced, accepted for the
	// hour its cursors live after a rotation (§6.7.9).
	CursorKeyPrevious envSecret  `json:"cursor_key_previous"`
	Server            serverConf `json:"server"`

	// The secrets, read from the environment once the file is read.
	cursorKey, previousCursorKey, dsn string
}

type signIn struct {
	Authority                   string   `json:"authority"`
	ClientID                    string   `json:"client_id"`
	Scopes                      []string `json:"scopes"`
	TokenEndpoint               string   `json:"token_endpoint"`
	AuthorizationEndpoint       string   `json:"authorization_endpoint"`
	DeviceAuthorizationEndpoint string   `json:"device_authorization_endpoint"`
}

func (s signIn) edge() edge.SignIn {
	return edge.SignIn{Authority: s.Authority, ClientID: s.ClientID, Scopes: s.Scopes, TokenEndpoint: s.TokenEndpoint,
		AuthorizationEndpoint: s.AuthorizationEndpoint, DeviceAuthorizationEndpoint: s.DeviceAuthorizationEndpoint}
}

type entraConf struct {
	Issuer    string   `json:"issuer"`
	Audiences []string `json:"audiences"`
	JWKSURL   string   `json:"jwks_url"`
	Tenant    string   `json:"tenant"`
}

type warehouse struct {
	// Driver is the warehouse engine: "snowflake", the one this build
	// links (gosnowflake, through gateway/adapter/snowflake/gosf).
	Driver string `json:"driver"`
	// DSN names the environment variable holding the connection string.
	DSN        envSecret  `json:"dsn"`
	Dialect    string     `json:"dialect"`
	Registered [][]string `json:"registered"`
	// The pool, which is the warehouse's bulkhead (§6.7.5: its own pool,
	// in-flight limit and a queue twice its concurrency): MaxOpen sessions
	// at most (default 8, Snowflake's default MAX_CONCURRENCY_LEVEL), as
	// many calls reaching the warehouse at once, MaxIdle sessions kept idle
	// (default MaxOpen, so a burst is not a logout and a login per call),
	// each closed after MaxIdleSeconds idle (default 30 minutes, under
	// Snowflake's 4-hour session timeout) or MaxLifetimeSeconds open
	// (default an hour). R054, R069.
	MaxOpen            int `json:"max_open"`
	MaxIdle            int `json:"max_idle"`
	MaxIdleSeconds     int `json:"max_idle_seconds"`
	MaxLifetimeSeconds int `json:"max_lifetime_seconds"`
}

// pool is the warehouse pool's settings with their defaults applied.
type pool struct {
	maxOpen, maxIdle         int
	maxIdleTime, maxLifetime time.Duration
}

// snowflakeSessionTimeout is how long Snowflake keeps an idle session
// (CLIENT_SESSION_KEEP_ALIVE false): a connection idle longer has lost it.
const snowflakeSessionTimeout = 4 * time.Hour

func (w warehouse) pool() pool {
	p := pool{maxOpen: w.MaxOpen, maxIdle: w.MaxIdle,
		maxIdleTime: time.Duration(w.MaxIdleSeconds) * time.Second, maxLifetime: time.Duration(w.MaxLifetimeSeconds) * time.Second}
	if p.maxOpen == 0 {
		p.maxOpen = 8
	}
	if p.maxIdle == 0 {
		p.maxIdle = p.maxOpen
	}
	if p.maxIdleTime == 0 {
		p.maxIdleTime = 30 * time.Minute
	}
	if p.maxLifetime == 0 {
		p.maxLifetime = time.Hour
	}
	return p
}

type serverConf struct {
	// ShutdownDelaySeconds and GraceSeconds replace §6.7.15's 5 s and 30 s
	// when they are set. The deployment's own stop timeout (Kubernetes'
	// terminationGracePeriodSeconds) must be longer than their sum.
	ShutdownDelaySeconds int `json:"shutdown_delay_seconds"`
	GraceSeconds         int `json:"grace_seconds"`
}

// serving is the server's configuration: §6.7.15's values, with the
// settings' listen address, shutdown delay and grace period.
func (s *Settings) serving() httpserver.Config {
	cfg := httpserver.Defaults()
	cfg.Addr = s.Listen
	if s.Server.ShutdownDelaySeconds > 0 {
		cfg.ShutdownDelay = time.Duration(s.Server.ShutdownDelaySeconds) * time.Second
	}
	if s.Server.GraceSeconds > 0 {
		cfg.GracePeriod = time.Duration(s.Server.GraceSeconds) * time.Second
	}
	return cfg
}

// envSecret is a secret named by its environment variable: {"env": "NAME"}.
type envSecret struct {
	Env string `json:"env"`
}

// value is the secret, read once at start (atlasvet: settings are read in
// a settings file, never on the request path).
func (e envSecret) value() (string, error) {
	if e.Env == "" {
		return "", nil
	}
	v, ok := os.LookupEnv(e.Env)
	if !ok || v == "" {
		return "", fmt.Errorf("the environment variable %s is not set", e.Env)
	}
	return v, nil
}

// configPath is the settings file: the -config flag, else
// ATLAS_GATEWAY_CONFIG.
func configPath(flag string) string {
	if flag != "" {
		return flag
	}
	return os.Getenv("ATLAS_GATEWAY_CONFIG")
}

// load reads and checks the settings file, and the secrets it names.
func load(path string) (*Settings, error) {
	if path == "" {
		return nil, errors.New("settings: name the settings file with -config or ATLAS_GATEWAY_CONFIG")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("settings: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var s Settings
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("settings: %s: %w", path, err)
	}
	if dec.More() {
		return nil, fmt.Errorf("settings: %s holds more than one JSON value", path)
	}
	if s.Listen == "" {
		s.Listen = httpserver.Defaults().Addr
	}
	if err := s.check(); err != nil {
		return nil, fmt.Errorf("settings: %s: %w", path, err)
	}
	return &s, nil
}

// environmentName is the bootstrap's environment grammar (bootstrap.schema.json).
var environmentName = regexp.MustCompile(`^[a-z](-?[a-z0-9])*$`)

// check is every problem with the settings, each naming its member, or nil.
func (s *Settings) check() error {
	var errs []error
	bad := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	switch {
	case s.Environment == "":
		bad("environment is required")
	case !environmentName.MatchString(s.Environment):
		bad("environment %q is not an environment name: lowercase letters and digits, single dashes, starting with a letter", s.Environment)
	case s.Environment == "local" || s.Environment == "conformance":
		bad("environment %q is the development or conformance gateway's, whose clients accept emulator handovers (§9.7.8): this is the production gateway", s.Environment)
	}
	if host, port, err := net.SplitHostPort(s.Listen); err != nil {
		bad("listen %q is not host:port: %v", s.Listen, err)
	} else if n, err := strconv.Atoi(port); err != nil || n < 0 || n > 65535 || strings.ContainsAny(host, "/ ") {
		bad("listen %q has no port the gateway can listen on", s.Listen)
	}
	if err := httpsURL(s.URL); err != nil {
		bad("url: %v", err)
	}
	if err := httpsURL(s.SignIn.Authority); err != nil {
		bad("sign_in.authority: %v", err)
	}
	if strings.TrimSpace(s.SignIn.ClientID) == "" {
		bad("sign_in.client_id is required: the public client every Atlas client signs in as")
	}
	if len(s.SignIn.Scopes) == 0 {
		bad("sign_in.scopes is required: the scopes a client asks for, whose audience is this gateway")
	}
	for i, sc := range s.SignIn.Scopes {
		if strings.TrimSpace(sc) == "" {
			bad("sign_in.scopes[%d] is empty", i)
		}
	}
	if err := httpsURL(s.SignIn.TokenEndpoint); err != nil {
		bad("sign_in.token_endpoint: %v", err)
	}
	for name, u := range map[string]string{"sign_in.authorization_endpoint": s.SignIn.AuthorizationEndpoint, "sign_in.device_authorization_endpoint": s.SignIn.DeviceAuthorizationEndpoint} {
		if u != "" {
			if err := httpsURL(u); err != nil {
				bad("%s: %v", name, err)
			}
		}
	}
	if err := httpsURL(s.Entra.Issuer); err != nil {
		bad("entra.issuer: %v", err)
	}
	if len(s.Entra.Audiences) == 0 {
		bad("entra.audiences is required: the gateway's application id URI and client id")
	}
	for i, a := range s.Entra.Audiences {
		if strings.TrimSpace(a) == "" {
			bad("entra.audiences[%d] is empty", i)
		}
	}
	if err := httpsURL(s.Entra.JWKSURL); err != nil {
		bad("entra.jwks_url: %v", err)
	}
	if s.PolicyFile == "" {
		bad("policy_file is required")
	}
	if s.AuditFile == "" {
		bad(`audit_file is required: a file the gateway appends each fact to and syncs, or "-" for standard output read by a collector`)
	}
	seen := map[string]bool{}
	for _, d := range s.Domains {
		if seen[d] {
			bad("domains names %q twice", d)
		}
		seen[d] = true
	}
	for i, x := range s.Exclude {
		if strings.TrimSpace(x) == "" {
			bad("exclude[%d] is empty", i)
		}
	}
	if s.Server.ShutdownDelaySeconds < 0 || s.Server.GraceSeconds < 0 {
		bad("server: a shutdown delay or grace period is not negative")
	}
	errs = append(errs, s.Warehouse.check()...)

	var err error
	if s.CursorKey.Env == "" {
		bad(`cursor_key is required: {"env": NAME}, the variable holding the key every replica signs listing cursors with (§6.7.9), at least %d bytes`, builtins.MinCursorKey)
	} else if s.cursorKey, err = s.CursorKey.value(); err != nil {
		bad("cursor_key: %v", err)
	} else if len(s.cursorKey) < builtins.MinCursorKey {
		bad("cursor_key: the key in %s is %d bytes; it must be at least %d", s.CursorKey.Env, len(s.cursorKey), builtins.MinCursorKey)
	}
	if s.CursorKeyPrevious.Env != "" {
		if s.previousCursorKey, err = s.CursorKeyPrevious.value(); err != nil {
			bad("cursor_key_previous: %v", err)
		} else if len(s.previousCursorKey) < builtins.MinCursorKey {
			bad("cursor_key_previous: the key in %s is %d bytes; it must be at least %d", s.CursorKeyPrevious.Env, len(s.previousCursorKey), builtins.MinCursorKey)
		}
	}
	if s.Warehouse.Driver != "" {
		if s.dsn, err = s.Warehouse.DSN.value(); err != nil {
			bad("warehouse.dsn: %v", err)
		}
	}
	return errors.Join(errs...)
}

func (w warehouse) check() []error {
	var errs []error
	bad := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	if w.Driver == "" {
		if w.DSN.Env != "" || w.Dialect != "" || len(w.Registered) > 0 || w.MaxOpen != 0 || w.MaxIdle != 0 || w.MaxIdleSeconds != 0 || w.MaxLifetimeSeconds != 0 {
			bad("warehouse: its settings are given, but no driver")
		}
		return errs
	}
	if w.DSN.Env == "" {
		bad(`warehouse.dsn is required: {"env": NAME}, the variable holding the connection string`)
	}
	if d := strings.ToLower(w.Dialect); d != "" && d != "snowflake" {
		bad("warehouse.dialect %q: this gateway's warehouse speaks snowflake", w.Dialect)
	}
	if len(w.Registered) == 0 {
		bad("warehouse.registered is required: the objects capabilities may read, [database, schema] or [database, schema, object]")
	}
	for i, r := range w.Registered {
		if len(r) < 2 || len(r) > 3 || slices.ContainsFunc(r, func(name string) bool { return strings.TrimSpace(name) == "" }) {
			bad("warehouse.registered[%d] %q is not [database, schema] or [database, schema, object]", i, r)
		}
	}
	if w.MaxOpen < 0 || w.MaxIdle < 0 || w.MaxIdleSeconds < 0 || w.MaxLifetimeSeconds < 0 {
		bad("warehouse: a pool setting is not negative")
	}
	p := w.pool()
	if p.maxIdle > p.maxOpen {
		bad("warehouse.max_idle %d is more than max_open %d", p.maxIdle, p.maxOpen)
	}
	if p.maxIdleTime >= snowflakeSessionTimeout {
		bad("warehouse.max_idle_seconds %d is not under Snowflake's %v session timeout: a connection idle that long has lost its session", w.MaxIdleSeconds, snowflakeSessionTimeout)
	}
	return errs
}

// httpsURL checks an absolute https URL with a host, and no user, query or
// fragment (§9.6.1: HTTPS only).
func httpsURL(s string) error {
	if s == "" {
		return errors.New("is required")
	}
	u, err := url.Parse(s)
	switch {
	case err != nil:
		return fmt.Errorf("%q is not a URL: %w", s, err)
	case u.Scheme != "https":
		return fmt.Errorf("%q is not an https URL (§9.6.1: HTTPS only)", s)
	case u.Host == "" || u.Hostname() == "":
		return fmt.Errorf("%q names no host", s)
	case u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "":
		return fmt.Errorf("%q has a user, a query or a fragment", s)
	}
	return nil
}
