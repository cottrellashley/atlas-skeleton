package pipeline

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
	"github.com/jhgonline/fi-app-atlas/sdk/go/protocol"
)

// limiter holds the protective limits (§6.2.4): a rate, as a token bucket
// per caller and capability, and the calls running at once. These protect
// the gateway and its sources; they are not a budget (R07), and a replica
// counts only its own calls.
type limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	calls   int
}

type bucket struct {
	tokens  float64
	at      time.Time
	running int
}

// maxIdle is how long an idle bucket is kept; older ones are forgotten, so
// the limiter's memory is bounded by its recent callers.
const maxIdle = 10 * time.Minute

// take admits one call, or says how many seconds to wait. release ends it.
func (l *limiter) take(key string, lim ports.Limit, now time.Time) (release func(), retryAfter int, ok bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.buckets == nil {
		l.buckets = map[string]*bucket{}
	}
	if l.calls++; l.calls%1024 == 0 {
		for k, b := range l.buckets {
			if b.running == 0 && now.Sub(b.at) > maxIdle {
				delete(l.buckets, k)
			}
		}
	}
	b := l.buckets[key]
	if b == nil {
		b = &bucket{tokens: float64(lim.PerMinute), at: now}
		l.buckets[key] = b
	}
	if lim.PerMinute > 0 {
		rate := float64(lim.PerMinute) / 60
		b.tokens = min(float64(lim.PerMinute), b.tokens+now.Sub(b.at).Seconds()*rate)
		b.at = now
		if b.tokens < 1 {
			return nil, int((1-b.tokens)/rate) + 1, false
		}
	}
	if lim.Concurrency > 0 && b.running >= lim.Concurrency {
		return nil, 1, false
	}
	if lim.PerMinute > 0 {
		b.tokens--
	}
	b.running++
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			b.running--
			l.mu.Unlock()
		})
	}, 0, true
}

// remaining is what is left of a caller's limits, for Atlas-Limit-Remaining.
func (l *limiter) remaining(key string, lim ports.Limit) map[string]int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := map[string]int64{}
	b := l.buckets[key]
	if b == nil {
		return out
	}
	if lim.PerMinute > 0 {
		out["rate"] = int64(b.tokens)
	}
	if lim.Concurrency > 0 {
		out["concurrency"] = int64(lim.Concurrency - b.running)
	}
	return out
}

// shedRetryAfter is the Retry-After of a call shed for want of room: the
// replica's, or an upstream's (06-7 §6.7.2, §6.7.5).
const shedRetryAfter = 2

// gate bounds the calls a replica runs at once, from their arrival to the
// end of their answer: a stream holds its place until it ends (06-7
// §6.7.2's in-flight limit).
type gate struct {
	n   atomic.Int64
	max int64
}

func newGate(max int) *gate { return &gate{max: int64(max)} }

// take is a place, or false when the replica has none.
func (g *gate) take() (release func(), ok bool) {
	if g.n.Add(1) > g.max {
		g.n.Add(-1)
		return nil, false
	}
	var once sync.Once
	return func() { once.Do(func() { g.n.Add(-1) }) }, true
}

// errFull is a bulkhead whose queue is full: the call is shed.
var errFull = errors.New("pipeline: the upstream's bulkhead is full")

// bulkhead holds the calls that reach one upstream (06-7 §6.7.5): at most
// its concurrency at once, and at most twice that many waiting for a place,
// so a slow upstream costs its own capabilities and never the replica's
// goroutines (constitution rule 13).
type bulkhead struct {
	name    string
	slots   chan struct{}
	waiting atomic.Int64
	queue   int64
}

func newBulkhead(name string, concurrency int) *bulkhead {
	return &bulkhead{name: name, slots: make(chan struct{}, concurrency), queue: 2 * int64(concurrency)}
}

// enter is a place, waiting for one while ctx lasts; errFull when the
// queue is full, at once.
func (b *bulkhead) enter(ctx context.Context) (release func(), err error) {
	select {
	case b.slots <- struct{}{}:
		return b.leave(), nil
	default:
	}
	if b.waiting.Add(1) > b.queue {
		b.waiting.Add(-1)
		return nil, errFull
	}
	defer b.waiting.Add(-1)
	select {
	case b.slots <- struct{}{}:
		return b.leave(), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (b *bulkhead) leave() func() {
	var once sync.Once
	return func() { once.Do(func() { <-b.slots }) }
}

// full is the problem of a call the bulkhead sheds, naming its upstream
// (06-7 §6.7.5: a full bulkhead answers at once).
func (b *bulkhead) full() *protocol.Problem {
	return protocol.NewProblem(protocol.CodeUnavailable, fmt.Sprintf("the %s is answering as many calls as it allows (%d), with as many waiting as it allows (%d)", b.name, cap(b.slots), b.queue)).
		WithRetryAfter(shedRetryAfter).WithNext("Wait retry_after seconds, then repeat the call.")
}

// Through names the upstream a provider's work reaches (a warehouse, a
// vendor's API), so the pipeline holds its calls in that upstream's
// bulkhead (06-7 §6.7.5) whichever capability, and whichever provider,
// makes them. A provider no upstream names (the built-ins, which read only
// the snapshot) has no bulkhead.
func Through(upstream string, p ports.Provider) ports.Provider {
	return through{Provider: p, upstream: upstream}
}

type through struct {
	ports.Provider
	upstream string
}

// upstreamOf is the upstream a provider names, or "".
func upstreamOf(p ports.Provider) string {
	if t, ok := p.(through); ok {
		return t.upstream
	}
	return ""
}

// bulkheads are the pipeline's, one per upstream, made as each is first
// reached. Upstream names come from the deployment's code, never from a
// call, so the map is bounded.
type bulkheads struct {
	mu          sync.Mutex
	by          map[string]*bulkhead
	concurrency map[string]int
	fallback    int
}

func (bs *bulkheads) of(name string) *bulkhead {
	bs.mu.Lock()
	defer bs.mu.Unlock()
	if b := bs.by[name]; b != nil {
		return b
	}
	n := bs.concurrency[name]
	if n <= 0 {
		n = bs.fallback
	}
	b := newBulkhead(name, n)
	if bs.by == nil {
		bs.by = map[string]*bulkhead{}
	}
	bs.by[name] = b
	return b
}
