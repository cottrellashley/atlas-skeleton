// Command atlas-engine is the runner protocol over the Go client (§9.8.3):
// the conformance suite's runner, and the engine the Python SDK starts.
// Commands arrive as JSON lines on standard input and answers leave on
// standard output; Arrow tables go to the pipe ATLAS_ENGINE_RESULTS_FD
// names. When its input ends it cancels what is running and exits, so it
// never outlives its face (ATLAS-PY-007).
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"

	atlas "github.com/jhgonline/fi-app-atlas/sdk/go"
	"github.com/jhgonline/fi-app-atlas/sdk/go/engine"
)

func main() {
	// Ctrl+C reaches the whole process group: the face, not the engine,
	// decides what it ends, and cancels its call (§5.1 rule 3).
	signal.Ignore(os.Interrupt)
	fd, _ := strconv.Atoi(os.Getenv("ATLAS_ENGINE_RESULTS_FD"))
	e := engine.New(atlas.Options{Name: os.Getenv("ATLAS_ENGINE_NAME")}, os.Stdout, engine.ResultsPipe(fd))
	if err := e.Run(context.Background(), os.Stdin); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
