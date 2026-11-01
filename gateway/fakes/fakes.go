// Package fakes holds in-memory implementations of the ports, for local runs
// and the hermetic suite. Each passes its port's suite like any adapter.
package fakes

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"sync"
	"time"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
)

// Limits keeps a token bucket per key in memory: exact within one process,
// which is all a single replica needs.
type Limits struct {
	Now     func() time.Time
	mu      sync.Mutex
	buckets map[string]*bucket
}

type bucket struct {
	tokens float64
	at     time.Time
}

// NewLimits returns empty limits.
func NewLimits() *Limits { return &Limits{Now: time.Now, buckets: map[string]*bucket{}} }

var _ ports.Limits = (*Limits)(nil)

// Reserve takes one token from the key's bucket, which refills at limit per
// period, or refuses with limit_exceeded and the time to one token.
func (l *Limits) Reserve(_ context.Context, key string, limit int, per time.Duration) (ports.Reservation, error) {
	if limit <= 0 {
		return ports.Reservation{Key: key, Remaining: -1}, nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.Now()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: float64(limit), at: now}
		l.buckets[key] = b
	}
	rate := float64(limit) / per.Seconds()
	b.tokens = math.Min(float64(limit), b.tokens+now.Sub(b.at).Seconds()*rate)
	b.at = now
	if b.tokens < 1 {
		wait := int(math.Ceil((1 - b.tokens) / rate))
		return ports.Reservation{}, protocol.NewProblem(protocol.CodeLimitExceeded, "the rate limit for this capability is used up").WithRetryAfter(wait)
	}
	b.tokens--
	return ports.Reservation{Key: key, Remaining: int(b.tokens)}, nil
}

// Settle has nothing to return for a rate limit.
func (l *Limits) Settle(context.Context, ports.Reservation) error { return nil }

// Audit keeps every record in memory and, when given a writer, appends each
// as one JSON line before the call's answer ends.
type Audit struct {
	mu      sync.Mutex
	out     io.Writer
	records []ports.AuditRecord
}

// NewAudit returns an audit sink; out may be nil.
func NewAudit(out io.Writer) *Audit { return &Audit{out: out} }

var _ ports.AuditSink = (*Audit)(nil)

func (a *Audit) Write(_ context.Context, rec ports.AuditRecord) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.records = append(a.records, rec)
	if a.out != nil {
		return json.NewEncoder(a.out).Encode(rec)
	}
	return nil
}

// Records returns a copy of every record written.
func (a *Audit) Records() []ports.AuditRecord {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]ports.AuditRecord(nil), a.records...)
}
