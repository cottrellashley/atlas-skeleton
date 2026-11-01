// Command gateway is the Atlas gateway for real systems. It reads its
// settings from one JSON file (-config, or ATLAS_GATEWAY_CONFIG), chooses
// its adapters from them (Entra for sign-in, a policy file, a JSON-lines
// audit, a warehouse through the driver this build links), checks each one
// as it starts, and serves until it is signalled to stop.
//
// It serves as §6.7.15 says: on :8000 unless told otherwise, h2c behind the
// ingress, 64 KiB of headers, and on SIGTERM readiness false, 5 s for the
// load balancer to notice, then 30 s for the calls running to finish, then
// close. The deployment's own stop timeout (Kubernetes'
// terminationGracePeriodSeconds, Container Apps' termination grace period)
// must be longer than the two together: 40 s.
//
// Its logs go to standard error as JSON. Standard output is the audit's,
// when the settings give "-" for audit_file, and nothing else's, so a
// collector reading it reads facts only (R012).
//
// It is built pure Go, as the release builds it:
//
//	CGO_ENABLED=0 go -C gateway build -tags minicore_disabled -o bin/gateway ./cmd/gateway
//
// (cmd/gateway/layers.toml's [[pure]] rule proves that nothing it links
// needs cgo; R116.)
package main

import (
	"context"
	"database/sql/driver"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"slices"
	"syscall"
	"time"

	"atlas.local/gateway/adapter/auditlog"
	"atlas.local/gateway/adapter/entra"
	"atlas.local/gateway/adapter/policyfile"
	"atlas.local/gateway/adapter/snowflake"
	"atlas.local/gateway/adapter/snowflake/gosf"
	"atlas.local/gateway/assemble"
	"atlas.local/gateway/edge"
	"atlas.local/gateway/svc/datastatus"
	"atlas.local/gateway/svc/portfolio"
	"atlas.local/sdk/go/kit/health"
	"atlas.local/sdk/go/kit/httpserver"
	"atlas.local/sdk/go/kit/jwt"
	"atlas.local/sdk/go/kit/lifecycle"
)

// domains are every domain this build can serve: the one list, never an
// init (constitution rule 4).
var domains = map[string]assemble.Domain{
	"portfolio": portfolio.Capabilities,
	"data":      datastatus.Capabilities,
}

// startTimeout bounds each check a dependency gets at start: the tenant's
// keys fetched, the warehouse reached.
const startTimeout = 10 * time.Second

// entraTimeout bounds each request to Entra's key endpoint, as the kit's
// KeySet does: a refresh for an unknown kid runs on a call's path, within
// its deadline too.
const entraTimeout = 5 * time.Second

func main() {
	h := host{stdout: os.Stdout, stderr: os.Stderr, http: &http.Client{Timeout: entraTimeout}}
	if err := run(context.Background(), os.Args[1:], h); err != nil && !lifecycle.IsSignal(err) {
		fmt.Fprintln(os.Stderr, "gateway:", err)
		os.Exit(1)
	}
}

// host is what the gateway reaches beyond its settings: the standard output
// a collector reads, where it logs, and the HTTP client it reaches Entra
// with. A test gives its own.
type host struct {
	stdout *os.File
	stderr io.Writer
	http   *http.Client
	// warehouse, when set, opens the warehouse's sessions in place of
	// gosnowflake's connector: a test's. Production leaves it nil.
	warehouse driver.Connector
}

// run is the gateway's life: its settings read and checked, its parts
// opened and assembled, then served on the settings' address until a
// signal, drained, and closed.
func run(ctx context.Context, args []string, h host) error {
	fs := flag.NewFlagSet("gateway", flag.ContinueOnError)
	config := fs.String("config", "", "the settings file")
	if err := fs.Parse(args); err != nil {
		return err
	}
	s, err := load(configPath(*config))
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(h.stderr, nil))
	d, err := open(ctx, s, h, log)
	if err != nil {
		return err
	}
	defer func() {
		if err := d.Close(); err != nil {
			log.Error("gateway: closing", "err", err)
		}
	}()
	ln, err := net.Listen("tcp", s.Listen)
	if err != nil {
		return fmt.Errorf("gateway: listen on %s: %w", s.Listen, err)
	}
	log.Info("atlas gateway", "environment", s.Environment, "url", s.URL, "listen", ln.Addr().String(), "capabilities", len(d.gateway.Registry.Snapshot().Capabilities()))
	return serve(ctx, d.gateway.Edge, ln, s.serving(), d.ready, log)
}

// serve is the process group of §6.7.15: the signal handler and the server,
// the first to stop stopping the other. A signal starts the drain: the
// server's requests keep their own contexts, so the calls running finish
// (R016, ATLAS-MECH-016).
func serve(ctx context.Context, h http.Handler, ln net.Listener, cfg httpserver.Config, ready *lifecycle.Readiness, log *slog.Logger) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	srv := httpserver.New(ctx, cfg, h, log)
	var group lifecycle.Group
	group.Add(lifecycle.SignalActor(ctx, os.Interrupt, syscall.SIGTERM))
	group.Add(func() error { return httpserver.Serve(ctx, srv, ln, cfg, ready, log) }, func(error) { stop() })
	return group.Run()
}

// deployment is the gateway and the parts it was assembled from, open.
type deployment struct {
	gateway *assemble.Gateway
	auth    *entra.Authenticator
	audit   *auditlog.Sink
	ready   *lifecycle.Readiness
	// closers are closed in reverse: the warehouse before the audit, which
	// stays open for any call that outlived the drain.
	closers []io.Closer
}

// Close closes what open opened.
func (d *deployment) Close() error {
	var errs []error
	for _, c := range slices.Backward(d.closers) {
		if err := c.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	d.closers = nil
	return errors.Join(errs...)
}

// open opens the adapters the settings choose, checks each as it starts,
// and assembles the gateway. A part that cannot work refuses the start with
// what is wrong; a dependency that is only unreachable now is logged, and
// costs only its own capabilities (constitution rule 13).
func open(ctx context.Context, s *Settings, h host, log *slog.Logger) (_ *deployment, err error) {
	d := &deployment{ready: &lifecycle.Readiness{}}
	defer func() {
		if err != nil {
			err = errors.Join(err, d.Close())
		}
	}()
	if d.audit, err = openAudit(s.AuditFile, h.stdout); err != nil {
		return nil, err
	}
	d.closers = append(d.closers, d.audit)

	keys := jwt.NewKeySet(s.Entra.JWKSURL)
	keys.Client = h.http
	d.auth, err = entra.New(entra.Options{Issuer: s.Entra.Issuer, Audiences: s.Entra.Audiences, Keys: keys, Tenant: s.Entra.Tenant, Log: log})
	if err != nil {
		return nil, err
	}
	// §6.7.4: the tenant's keys are fetched at start. A gateway that cannot
	// fetch them still starts, unready, and answers unavailable until it can
	// (R013).
	start, cancel := context.WithTimeout(ctx, startTimeout)
	keysErr := d.auth.Refresh(start)
	cancel()
	if keysErr != nil {
		log.Warn("gateway: the tenant's signing keys cannot be fetched yet: calls are unavailable, and /readyz fails, until they are", "err", keysErr)
	}
	log.Warn("gateway: no group source is configured: a person whose token has group overage is refused, never read as in no groups (R068)")

	policy, err := policyfile.New(s.PolicyFile)
	if err != nil {
		return nil, err
	}
	parts := assemble.Parts{
		Environment: s.Environment, URL: s.URL, SignIn: s.SignIn.edge(),
		Authenticator: d.auth, Policy: policy, Audit: d.audit, Log: log, Exclude: s.Exclude,
		CursorKey: []byte(s.cursorKey), PreviousCursorKey: []byte(s.previousCursorKey),
		Serving: edge.Serving{Ready: d.ready, Checks: []health.Check{
			health.NamedCheck("entra", d.auth.Check),
			health.NamedCheck("audit", d.audit.Check),
		}},
	}
	served := map[string]bool{}
	for _, name := range s.Domains {
		dom, ok := domains[name]
		if !ok {
			return nil, fmt.Errorf("settings: no domain %q in this build", name)
		}
		files, err := dom()
		if err != nil {
			return nil, fmt.Errorf("gateway: the domain %s: %w", name, err)
		}
		for _, f := range files {
			served[f.Name] = true
		}
		parts.Domains = append(parts.Domains, dom)
	}
	for _, x := range s.Exclude {
		if !served[x] {
			return nil, fmt.Errorf("settings: exclude names %q, which no domain of this build serves: a typo would serve what it means to withhold", x)
		}
	}
	if s.Warehouse.Driver != "" {
		w, err := openWarehouse(ctx, s.Warehouse, s.dsn, log, h.warehouse)
		if err != nil {
			return nil, err
		}
		d.closers = append(d.closers, w.engine)
		parts.DataSource = w.source
		parts.WarehouseConcurrency = w.pool.maxOpen
	} else if len(parts.Domains) > 0 {
		return nil, errors.New("settings: the domains serve warehouse capabilities: configure a warehouse")
	}
	if d.gateway, err = assemble.New(parts); err != nil {
		return nil, err
	}
	return d, nil
}

// openAudit is the audit sink audit_file names: a file appended to and
// synced, or "-" for standard output, read by a collector. Either is
// checked now, so a gateway whose audit cannot keep a fact does not start,
// rather than refusing every call (R047, R056).
func openAudit(path string, stdout *os.File) (*auditlog.Sink, error) {
	if path == "-" {
		s, err := auditlog.NewFile(stdout)
		if err != nil {
			return nil, fmt.Errorf("settings: audit_file \"-\": %w", err)
		}
		return s, nil
	}
	s, err := auditlog.Open(path)
	if err != nil {
		return nil, fmt.Errorf("settings: audit_file: %w", err)
	}
	return s, nil
}

// openedWarehouse is the Snowflake data source over its engine and pool.
type openedWarehouse struct {
	source *snowflake.Source
	engine *gosf.Engine
	pool   pool
}

// openWarehouse opens the Snowflake data source with the settings' bounds:
// the gosnowflake engine (pure Go, built with -tags minicore_disabled) under
// the adapter that plans, guards and settles every read. It reaches the
// warehouse once. A warehouse it cannot reach now is logged: its
// capabilities answer unavailable until it can, and nothing else is
// affected (rule 13). connector, when set, opens the sessions in place of
// gosnowflake's own: a test's.
func openWarehouse(ctx context.Context, w warehouse, dsn string, log *slog.Logger, connector driver.Connector) (*openedWarehouse, error) {
	if w.Driver != engineDriver {
		return nil, fmt.Errorf("settings: warehouse.driver %q: this build's warehouse engine is %q (gosnowflake)", w.Driver, engineDriver)
	}
	p := w.pool()
	cfg := gosf.Config{Connector: connector}
	if connector == nil {
		var err error
		if cfg, err = gosf.ConfigFromDSN(dsn); err != nil {
			return nil, fmt.Errorf("settings: warehouse.dsn: %w", err)
		}
	}
	cfg.MaxOpen, cfg.MaxIdleTime, cfg.MaxLifetime = p.maxOpen, p.maxIdleTime, p.maxLifetime
	engine, err := gosf.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("settings: warehouse: %w", err)
	}
	source, err := snowflake.NewSource(engine, snowflake.Options{Registered: w.Registered, MaxInFlight: p.maxOpen, Unavailable: gosf.Unavailable})
	if err != nil {
		return nil, errors.Join(fmt.Errorf("settings: warehouse: %w", err), engine.Close())
	}
	start, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()
	if err := engine.Ping(start); err != nil {
		log.Error("gateway: the warehouse cannot be reached yet: its capabilities are unavailable until it can be", "err", err)
	}
	return &openedWarehouse{source: source, engine: engine, pool: p}, nil
}

// engineDriver is the one warehouse engine this build links.
const engineDriver = "snowflake"
