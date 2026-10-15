package jobs

import (
	"context"
	"sync"
	"time"
)

// swr is a stale-while-revalidate cache for answers that need the
// destination (is it online, which versions it holds). A cloud
// destination can take seconds to answer - longer while a run uploads to
// it - and GET /jobs is polled by every open screen, so a fresh answer is
// returned as is, a stale one at once while one background call refreshes
// it, and only a missing one is waited for.
type swr[T any] struct {
	mu sync.Mutex
	m  map[string]*swrEntry[T]
}

type swrEntry[T any] struct {
	v       T
	has     bool // v holds an answer
	at      time.Time
	loading bool
	done    chan struct{} // closed when the first load ends
}

// get returns key's value, and false when there is none yet. Loads run
// under bg (the service's context, tracked by wg), not the request's:
// only the first ask waits, at most until ctx ends, and a load that takes
// longer still fills the cache for the next ask. load's ok=false answers
// are not kept (the engine wasn't there, the service is stopping).
func (c *swr[T]) get(ctx, bg context.Context, wg *sync.WaitGroup, key string, ttl time.Duration, now func() time.Time, load func(ctx context.Context) (T, bool)) (T, bool) {
	c.mu.Lock()
	if c.m == nil {
		c.m = map[string]*swrEntry[T]{}
	}
	e := c.m[key]
	if e == nil {
		e = &swrEntry[T]{done: make(chan struct{})}
		c.m[key] = e
	}
	if !e.loading && (!e.has || now().Sub(e.at) > ttl) {
		e.loading = true
		wg.Add(1)
		go func() {
			defer wg.Done()
			nv, ok := load(bg)
			c.mu.Lock()
			defer c.mu.Unlock()
			e.loading = false
			if ok {
				e.v, e.has, e.at = nv, true, now()
			} else if !e.has {
				delete(c.m, key) // the next ask tries again
			}
			select {
			case <-e.done:
			default:
				close(e.done)
			}
		}()
	}
	if e.has {
		v := e.v
		c.mu.Unlock()
		return v, true
	}
	done := e.done
	c.mu.Unlock()
	select {
	case <-done:
	case <-ctx.Done():
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return e.v, e.has
}
