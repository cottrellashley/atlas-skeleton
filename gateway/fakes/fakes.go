// Package fakes holds in-memory implementations of every port, for the
// development gateway and for tests. Each stands in for its real system at
// the same seam the real adapter uses (AGENTS.md), and each runs its port's
// suite in porttest, so a fake that drifts from the contract fails the same
// test the adapter would.
package fakes
