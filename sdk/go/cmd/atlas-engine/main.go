// Command atlas-engine is the Go client as a small program, which the Python
// SDK ships and gives every protocol job (§5.1). It reads commands on
// standard input and answers on standard output; table results go to the
// file descriptor ATLAS_ENGINE_RESULTS_FD names. Nothing else writes to
// standard output.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/jhgonline/fi-app-atlas/sdk/go/engine"
)

func main() {
	var results io.Writer
	if v := os.Getenv("ATLAS_ENGINE_RESULTS_FD"); v != "" {
		fd, err := strconv.Atoi(v)
		if err != nil || fd < 0 {
			fmt.Fprintln(os.Stderr, "atlas-engine: ATLAS_ENGINE_RESULTS_FD is not a file descriptor")
			os.Exit(2)
		}
		f := os.NewFile(uintptr(fd), "results")
		defer f.Close()
		results = f
	}
	// Ctrl+C reaches every process at the terminal. What it means for a call
	// is the face's to decide, and the face sends cancel (§5.1).
	signal.Ignore(os.Interrupt)
	go endWhenOrphaned()
	if err := engine.New(os.Stdout, results).Run(context.Background(), os.Stdin); err != nil {
		fmt.Fprintln(os.Stderr, "atlas-engine:", err)
		os.Exit(1)
	}
}

// endWhenOrphaned ends the engine, work and all, once the process that
// started it is gone, which the engine sees as a change of parent: no engine
// outlives its Python process (ATLAS-PY-007).
func endWhenOrphaned() {
	parent := os.Getppid()
	for range time.Tick(200 * time.Millisecond) {
		if os.Getppid() != parent {
			os.Exit(1)
		}
	}
}
