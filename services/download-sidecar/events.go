package main

import (
	"sync"
	"time"
)

// Event is a download finishing or failing. The UI polls /events?after=N
// and raises a desktop notification for each new one - the same thing it
// would get from a push channel, without one more socket per window.
type Event struct {
	Seq      int64     `json:"seq"`
	Kind     string    `json:"kind"`
	ID       string    `json:"id"`
	Filename string    `json:"filename"`
	Path     string    `json:"path,omitempty"`
	Message  string    `json:"message,omitempty"`
	Time     time.Time `json:"time"`
}

type eventLog struct {
	// Epoch identifies this process's event sequence. Seq restarts at 1
	// whenever the service restarts, so a client that still remembers a
	// seq from before would otherwise see "nothing new" until the counter
	// caught up again - silently losing every notification in between. A
	// changed epoch tells it to resync from 0.
	epoch  string
	mu     sync.Mutex
	seq    int64
	events []Event
	max    int
}

func newEventLog(max int) *eventLog { return &eventLog{max: max, epoch: newID()} }

func (l *eventLog) Epoch() string { return l.epoch }

func (l *eventLog) Push(e Event) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seq++
	e.Seq = l.seq
	e.Time = time.Now()
	l.events = append(l.events, e)
	if len(l.events) > l.max {
		l.events = l.events[len(l.events)-l.max:]
	}
}

// Since returns events newer than seq, plus the latest seq (so a client
// that connects fresh can start from "now" without replaying history).
func (l *eventLog) Since(seq int64) ([]Event, int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := []Event{}
	for _, e := range l.events {
		if e.Seq > seq {
			out = append(out, e)
		}
	}
	return out, l.seq
}
