package snowflake

import (
	"context"
	"fmt"
	"sync"

	"github.com/jhgonline/fi-app-atlas/gateway/ports"
)

// bulkhead bounds the work an engine is given (§6.7: each upstream its own
// in-flight limit and a bounded queue, twice its concurrency): places for
// the statements running, a queue of at most a fixed number of calls
// waiting for one, and an answer at once, ports.ErrSourceUnavailable, for
// a call past both (R050). A waiting call leaves when its context ends.
type bulkhead struct {
	places chan struct{}
	mu     sync.Mutex
	queued int
	queue  int
}

func newBulkhead(inflight, queue int) *bulkhead {
	return &bulkhead{places: make(chan struct{}, inflight), queue: queue}
}

// acquire takes a place, waiting in the queue when every place is taken.
func (b *bulkhead) acquire(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case b.places <- struct{}{}:
		return nil
	default:
	}
	b.mu.Lock()
	if b.queued >= b.queue {
		b.mu.Unlock()
		return fmt.Errorf("%w: %d statements are running and %d waiting, the most the source takes", ports.ErrSourceUnavailable, cap(b.places), b.queue)
	}
	b.queued++
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		b.queued--
		b.mu.Unlock()
	}()
	select {
	case b.places <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// release gives a place back.
func (b *bulkhead) release() { <-b.places }
