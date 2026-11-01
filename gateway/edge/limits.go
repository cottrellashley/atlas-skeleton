package edge

import (
	"math"
	"net/netip"
	"sync"
	"time"
)

// buckets are token buckets by client address, kept by one replica: the
// pre-auth limit is the one place a per-replica bucket is used (§6.7.13).
// A bucket is charged only by a call refused as unauthenticated, so a
// caller who signs in is never counted, however many calls it has open,
// and leaves no state behind. Their number is bounded: a bucket that has
// refilled to its burst holds nothing an absent one would not, so those
// are forgotten when the bound is reached; if none is, every further
// address shares one bucket, and memory never grows with the addresses a
// flood comes from.
type buckets struct {
	rate, burst float64
	max         int

	mu     sync.Mutex
	m      map[string]*bucket
	shared bucket
	swept  time.Time // when the full table was last swept
}

// sweepEvery is how often a full table may be swept, so a flood from new
// addresses cannot make every charge walk the whole table.
const sweepEvery = time.Second

type bucket struct {
	tokens float64
	at     time.Time
}

func newBuckets(rate float64, burst, max int) *buckets {
	return &buckets{rate: rate, burst: float64(burst), max: max, m: map[string]*bucket{}, shared: bucket{tokens: float64(burst)}}
}

// fill brings a bucket up to now.
func (b *buckets) fill(k *bucket, now time.Time) {
	if !k.at.IsZero() && now.After(k.at) {
		k.tokens = math.Min(b.burst, k.tokens+now.Sub(k.at).Seconds()*b.rate)
	}
	if now.After(k.at) {
		k.at = now
	}
}

// allow reports whether key may make a call now, or how many whole seconds
// until it may: it has a token left of its allowance.
func (b *buckets) allow(key string, now time.Time) (retryAfter int, ok bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	k := b.m[key]
	if k == nil {
		if len(b.m) < b.max {
			return 0, true
		}
		k = &b.shared
	}
	b.fill(k, now)
	if k.tokens >= 1 {
		return 0, true
	}
	return max(1, int(math.Ceil((1-k.tokens)/b.rate))), false
}

// charge spends one of key's tokens: a call of its was refused as
// unauthenticated.
func (b *buckets) charge(key string, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	k := b.m[key]
	if k == nil {
		if len(b.m) >= b.max && now.Sub(b.swept) >= sweepEvery {
			b.sweep(now)
		}
		if len(b.m) < b.max {
			k = &bucket{tokens: b.burst, at: now}
			b.m[key] = k
		} else {
			k = &b.shared
		}
	}
	b.fill(k, now)
	k.tokens = math.Max(0, k.tokens-1)
}

// sweep forgets the buckets that have refilled to their burst.
func (b *buckets) sweep(now time.Time) {
	b.swept = now
	for key, k := range b.m {
		b.fill(k, now)
		if k.tokens >= b.burst {
			delete(b.m, key)
		}
	}
}

// size is how many addresses are tracked.
func (b *buckets) size() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.m)
}

// clientKey is the pre-auth limit's key for a client address: an IPv4
// address, or an IPv6 address's /64, which one subscriber holds whole.
// An address that does not parse shares one key.
func clientKey(ip string) string {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return ""
	}
	a = a.Unmap()
	if a.Is4() {
		return a.String()
	}
	p, err := a.Prefix(64)
	if err != nil {
		return ""
	}
	return p.String()
}

func minTime(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}
