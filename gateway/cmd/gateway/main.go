// Command gateway is the skeleton's composition root (§6.5.8): it chooses the
// adapters, registers the services, builds the snapshot and mounts the edge.
// It has no logic of its own. With -dev it runs hermetically: a fake
// Snowflake over synthetic Harbour data, in-memory limits and audit, and its
// own token issuer.
//
// The first line it prints is JSON naming where it listens, so that tests and
// scripts can start it on a free port:
//
//	{"gateway":"http://127.0.0.1:54321","storage":"http://127.0.0.1:54322"}
package main

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/jhgonline/fi-app-atlas/gateway/adapter/snowflake"
	"github.com/jhgonline/fi-app-atlas/gateway/adapter/snowflake/snowfake"
	"github.com/jhgonline/fi-app-atlas/gateway/core/catalog"
	"github.com/jhgonline/fi-app-atlas/gateway/core/pipeline"
	edgehttp "github.com/jhgonline/fi-app-atlas/gateway/edge/http"
	"github.com/jhgonline/fi-app-atlas/gateway/fakes"
	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/gateway/svc/harbour"
	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/telemetry"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
)

// Development personas and the roles they hold.
var personas = map[string]fakes.Persona{
	"pm":       {Subject: "pm-0001", Name: "pm@example.test", Groups: []string{"fi-pm"}},
	"analyst":  {Subject: "analyst-0001", Name: "analyst@example.test", Groups: []string{"fi-analysts"}},
	"outsider": {Subject: "outsider-0001", Name: "outsider@example.test"},
}

func devGovernance() *catalog.Governance {
	return &catalog.Governance{Revision: "g-dev-1", Roles: []catalog.Role{
		{Name: "fi-pm", Members: []string{"fi-pm"}, Grants: []string{"portfolio", "index"}, RatePerMinute: 600},
		{Name: "fi-analysts", Members: []string{"fi-analysts"}, Grants: []string{"portfolio.holdings@v1:fetch"}, RatePerMinute: 60},
	}}
}

type pinFlags map[string]int

func (p pinFlags) String() string { return fmt.Sprint(map[string]int(p)) }

func (p pinFlags) Set(v string) error {
	k, n, ok := strings.Cut(v, "=")
	pos, err := strconv.Atoi(n)
	if !ok || err != nil {
		return errors.New("a pin is id@vN=position")
	}
	p[k] = pos
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gateway:", err)
		os.Exit(1)
	}
}

func run() error {
	pins := pinFlags{}
	var (
		addr        = flag.String("addr", "127.0.0.1:8070", "listen address")
		storageAddr = flag.String("storage-addr", "127.0.0.1:0", "the fake result storage's listen address")
		env         = flag.String("env", "local", "environment name")
		dev         = flag.Bool("dev", true, "run hermetically with development tokens; local only")
		govPath     = flag.String("governance", "", "governance JSON file; the development roles when empty")
		size        = flag.String("size", "small", "synthetic Harbour size: small or large")
		chunkRows   = flag.Int("chunk-rows", 10_000, "rows in each result chunk")
		linkTTL     = flag.Duration("link-ttl", 15*time.Minute, "how long chunk links work")
		latency     = flag.Duration("chunk-latency", 0, "how long each result chunk download waits")
		auditPath   = flag.String("audit", "", "append audit records to this file")
		logLevel    = flag.String("log", "warn", "log level")
	)
	flag.Var(pins, "pin", "serve an earlier profile as current: id@vN=position (repeatable)")
	flag.Parse()
	if !*dev {
		return errors.New("only -dev is wired in this skeleton; Development adapters come with the proofs")
	}

	var level slog.Level
	_ = level.UnmarshalText([]byte(*logLevel))
	log := telemetry.NewLogger(os.Stderr, level, "gateway", "skeleton", *env)
	mem := memory.NewGoAllocator()

	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	base := "http://" + ln.Addr().String()

	// Identity: the development issuer, verified by the real verifier.
	issuer, err := fakes.NewIssuer(base+"/.dev", "api://atlas-gateway", personas)
	if err != nil {
		return err
	}

	// Snowflake: the fake, behind the real adapter.
	sz := snowfake.Small
	if *size == "large" {
		sz = snowfake.Large
	}
	wh := snowfake.New(mem, snowfake.Harbour(sz)...)
	wh.ChunkRows, wh.LinkTTL, wh.Latency = *chunkRows, *linkTTL, *latency
	sln, err := net.Listen("tcp", *storageAddr)
	if err != nil {
		return err
	}
	wh.SetStorageURL("http://" + sln.Addr().String())
	go http.Serve(sln, wh.Storage()) //nolint:errcheck // stops with the process
	source := snowflake.New(wh, &http.Client{Timeout: 5 * time.Minute}, mem)

	// Services and the snapshot.
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return err
	}
	svc, err := harbour.New(source, pub)
	if err != nil {
		return err
	}
	gov := devGovernance()
	if *govPath != "" {
		if gov, err = catalog.LoadGovernance(*govPath); err != nil {
			return err
		}
	}
	snap, err := catalog.Build([]ports.Service{svc}, pins, gov)
	if err != nil {
		return err
	}

	var auditOut io.Writer
	if *auditPath != "" {
		f, err := os.OpenFile(*auditPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		defer f.Close()
		auditOut = f
	}
	pipe := &pipeline.Pipeline{
		Verifier: issuer.Verifier(), Snapshot: func() *catalog.Snapshot { return snap },
		Limits: fakes.NewLimits(), Audit: fakes.NewAudit(auditOut), Key: priv, Environment: *env, Now: time.Now,
	}
	edge := &edgehttp.Edge{Pipeline: pipe, Mem: mem, Log: log, Bootstrap: protocol.Bootstrap{
		Protocol: protocol.Version, Environment: *env,
		Servers: map[string]protocol.Server{"gateway": {URL: base}},
		Builtins: map[string]protocol.Binding{
			protocol.AddressWhoAmI:   builtinBinding(protocol.AddressWhoAmI),
			protocol.AddressCatalog:  builtinBinding(protocol.AddressCatalog),
			protocol.AddressSearch:   builtinBinding(protocol.AddressSearch),
			protocol.AddressDescribe: builtinBinding(protocol.AddressDescribe),
		},
		SignIn: protocol.SignIn{Issuer: issuer.Issuer, Audience: issuer.Audience, DevToken: "/.dev/token"},
	}}
	mux := http.NewServeMux()
	mux.Handle("/", edge.Handler())
	mux.HandleFunc("POST /.dev/token", issuer.Handler)

	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 5 * time.Minute}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()
	ready, _ := json.Marshal(map[string]string{"gateway": base, "storage": "http://" + sln.Addr().String()})
	fmt.Println(string(ready))
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func builtinBinding(address string) protocol.Binding {
	return protocol.Binding{Protocol: "https", Server: "gateway", Method: "POST", Address: "/v1/atlas/" + address}
}
