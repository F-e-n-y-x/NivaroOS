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
	at      time.Time
	loading bool
}

// get returns key's value. load's ok=false answers are not kept (the
// engine wasn't there, the request was cancelled). bg is the context and
// WaitGroup background refreshes run under (the service's).
func (c *swr[T]) get(ctx, bg context.Context, wg *sync.WaitGroup, key string, ttl time.Duration, now func() time.Time, load func(ctx context.Context) (T, bool)) T {
	c.mu.Lock()
	if c.m == nil {
		c.m = map[string]*swrEntry[T]{}
	}
	e := c.m[key]
	if e == nil {
		c.mu.Unlock()
		v, ok := load(ctx)
		if ok {
			c.mu.Lock()
			c.m[key] = &swrEntry[T]{v: v, at: now()}
			c.mu.Unlock()
		}
		return v
	}
	v := e.v
	if !e.loading && now().Sub(e.at) > ttl {
		e.loading = true
		wg.Add(1)
		go func() {
			defer wg.Done()
			nv, ok := load(bg)
			c.mu.Lock()
			defer c.mu.Unlock()
			e.loading = false
			if ok {
				e.v, e.at = nv, now()
			}
		}()
	}
	c.mu.Unlock()
	return v
}
