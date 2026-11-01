//go:build !unix

package engine

// pollable leaves the results pipe as it is where a pipe cannot be polled:
// a stream's writes are then cut only by the engine's end.
func pollable(int) {}
