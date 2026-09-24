package engine

import (
	"context"
	"sync"
)

// eventBuffer is each subscriber's channel size (API.Events): a slow
// reader loses events instead of blocking the engine.
const eventBuffer = 64

// eventHub fans engine events out to Events subscribers.
type eventHub struct {
	mu     sync.Mutex
	subs   map[chan Event]struct{}
	closed bool
}

func newEventHub() *eventHub { return &eventHub{subs: map[chan Event]struct{}{}} }

// subscribe registers a channel, primed with initial (sent before any
// later event), and removes and closes it when ctx ends.
func (h *eventHub) subscribe(ctx context.Context, initial []Event) (<-chan Event, error) {
	ch := make(chan Event, eventBuffer)
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil, Errorf(CodeEngineUnavailable, "engine is shutting down")
	}
	for _, ev := range initial {
		select {
		case ch <- ev:
		default:
		}
	}
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	go func() {
		<-ctx.Done()
		h.mu.Lock()
		defer h.mu.Unlock()
		if _, ok := h.subs[ch]; ok {
			delete(h.subs, ch)
			close(ch)
		}
	}()
	return ch, nil
}

func (h *eventHub) emit(ev Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs {
		select {
		case ch <- ev:
		default: // full: this reader reconciles with Volumes / JobStatus
		}
	}
}

func (h *eventHub) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for ch := range h.subs {
		delete(h.subs, ch)
		close(ch)
	}
}

// Events streams volume and job events until ctx ends. Every volume
// mounted when the subscription starts is sent first, with Initial set.
func (e *Engine) Events(ctx context.Context) (<-chan Event, error) {
	now := e.now()
	var initial []Event
	for _, v := range e.current().volumes() {
		v := v
		initial = append(initial, Event{Type: EventVolumeMounted, Time: now, Volume: &v, Initial: true})
	}
	return e.subs.subscribe(ctx, initial)
}
