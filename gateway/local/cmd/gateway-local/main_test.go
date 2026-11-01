package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jhgonline/fi-app-atlas/sdk/go/kit/conc"
)

// R016: a start-up error ends the development gateway with that error, with
// -catalog set too. Before, the catalogue watcher's deferred wait ran before
// the deferred cancel that would stop it, so the process hung forever.
func TestR016AStartUpErrorEndsTheProcess(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no", "such", "dir", "addr")
	done := conc.Go(func() error {
		return run([]string{"-listen", "127.0.0.1:0", "-catalog", t.TempDir(), "-addr-file", missing, "-quiet"})
	})
	ended := make(chan error, 1)
	conc.Go(func() error { ended <- done(); return nil })
	select {
	case err := <-ended:
		if err == nil || !strings.Contains(err.Error(), "addr") {
			t.Fatalf("got %v", err)
		}
	case <-time.After(time.Minute): // a guard against the hang, never a wait for it
		t.Fatal("the development gateway hung on a start-up error")
	}
}
