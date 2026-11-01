// Package conc may start goroutines: it is the one place that does.
package conc

func Go(fn func()) { go fn() }
