// Command gateway-local is the development gateway: the real gateway, its
// core, edge and Snowflake adapter, over stand-ins for what a laptop does
// not have. Its warehouse is DuckDB holding the Harbour fixture under
// Snowflake's names, every statement compiled for Snowflake and transpiled;
// its sign-in is a table of development tokens, with a token endpoint that
// refreshes them; its policy is the fixture's. Nothing needs Docker or an
// account.
//
//	gateway-local -listen 127.0.0.1:8740
//	atlas --gateway http://127.0.0.1:8740 login --token dev-alice
//
// -catalog names a directory of further capabilities (profiles/ and views/,
// as a domain's owner writes them): the gateway serves each new pair it
// finds there while it runs, which is how a capability is added without
// releasing any client.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"atlas.local/gateway/adapter/policyfile"
	"atlas.local/gateway/assemble"
	"atlas.local/gateway/local/devgateway"
	"atlas.local/gateway/views"
	"atlas.local/sdk/go/kit/httpserver"
	"atlas.local/sdk/go/kit/lifecycle"
)

func main() {
	if err := run(os.Args[1:]); err != nil && !lifecycle.IsSignal(err) {
		fmt.Fprintln(os.Stderr, "gateway-local:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("gateway-local", flag.ContinueOnError)
	listen := fs.String("listen", "127.0.0.1:8740", "the address to listen on (port 0 picks one)")
	exclude := fs.String("exclude", "", "pinned ids to withhold, comma separated")
	catalog := fs.String("catalog", "", "a directory of further capabilities, served as they appear")
	policyPath := fs.String("policy", "", "a policy file to use instead of the fixture's")
	addrFile := fs.String("addr-file", "", "write the gateway's URL to this file once it listens")
	quiet := fs.Bool("quiet", false, "log only failures")
	if err := fs.Parse(args); err != nil {
		return err
	}
	level := slog.LevelInfo
	if *quiet {
		level = slog.LevelWarn
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	// The listener is kept and served, never closed and opened again, so
	// the address the gateway reports is the one it serves.
	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		return err
	}
	defer ln.Close()
	url := "http://" + ln.Addr().String()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	opt := devgateway.Options{URL: url, Log: log}
	if *exclude != "" {
		opt.Exclude = strings.Split(*exclude, ",")
	}
	if *policyPath != "" {
		if opt.Policy, err = policyfile.New(*policyPath); err != nil {
			return err
		}
	}
	dev, err := devgateway.New(ctx, opt)
	if err != nil {
		return err
	}
	defer dev.Close()
	if *addrFile != "" {
		if err := os.WriteFile(*addrFile, []byte(url+"\n"), 0o600); err != nil {
			return fmt.Errorf("-addr-file: %w", err)
		}
	}
	cfg := httpserver.Defaults()
	cfg.Addr = ln.Addr().String()
	cfg.ShutdownDelay = 0
	srv := httpserver.New(ctx, cfg, dev.Handler, log)
	// One process group (§6.7.15): the signal, the server and the catalogue
	// watcher start and stop together, so a start-up error or a signal ends
	// every one of them (R016: the watcher used to be waited for before it
	// was told to stop, and a start-up error hung the process).
	var group lifecycle.Group
	group.Add(lifecycle.SignalActor(ctx, os.Interrupt, syscall.SIGTERM))
	group.Add(func() error { return httpserver.Serve(ctx, srv, ln, cfg, nil, log) }, func(error) { cancel() })
	if *catalog != "" {
		g := dev.Gateway
		group.Add(lifecycle.ContextActor(ctx, func(ctx context.Context) error { watch(ctx, g, *catalog, log); return nil }))
	}
	fmt.Fprintf(os.Stderr, "Atlas development gateway at %s\n  sign in: atlas --gateway %s login --token dev-alice   (or dev-bob, dev-carol, dev-dave, dev-nightly-risk)\n", url, url)
	return group.Run()
}

// watch serves every new capability that appears in dir, until ctx ends.
func watch(ctx context.Context, g *assemble.Gateway, dir string, log *slog.Logger) {
	var served []string
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		files, err := views.Read(os.DirFS(dir))
		if err != nil {
			log.Warn("catalog", "dir", filepath.Clean(dir), "err", err)
		}
		var fresh []views.Files
		for _, f := range files {
			if !slices.Contains(served, f.Name) {
				fresh = append(fresh, f)
			}
		}
		if len(fresh) > 0 {
			if err := g.Add(fresh...); err != nil {
				log.Warn("catalog: not served", "err", err)
			} else {
				for _, f := range fresh {
					served = append(served, f.Name)
					log.Info("catalog: now serving", "capability", f.Name)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
