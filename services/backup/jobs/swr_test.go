package jobs

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// GET /jobs waits for a destination only the first time; after that a
// slow destination (a cloud busy with an upload) answers from the cache
// at once and is re-checked in the background.
func TestJobsListDoesNotWaitForASlowDestination(t *testing.T) {
	now := time.Now()
	h := newHarness(t, true, withNow(func() time.Time { return now }))
	wantStatus(t, doRequest(h.svc, "POST", "/v1/backup/jobs", sampleJob("Docs"), reqOpts{}), 201)

	var calls atomic.Int32
	release := make(chan struct{})
	h.eng.ResolveFn = func(engine.ResolveRequest) (engine.Resolved, error) {
		calls.Add(1)
		<-release
		return engine.Resolved{Online: false, Quirks: []engine.Quirk{}}, nil
	}
	defer close(release)

	list := func() time.Duration {
		start := time.Now()
		wantStatus(t, doRequest(h.svc, "GET", "/v1/backup/jobs", nil, reqOpts{}), 200)
		return time.Since(start)
	}
	if d := list(); d > time.Second {
		t.Fatalf("fresh answer from the create: %v", d)
	}
	h.setNow(func() time.Time { return now.Add(onlineTTL + time.Second) })
	if d := list(); d > time.Second {
		t.Fatalf("stale answer should come at once, took %v", d)
	}
	if d := list(); d > time.Second {
		t.Fatalf("a refresh is under way: answer at once, took %v", d)
	}
	deadline := time.Now().Add(2 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if n := calls.Load(); n != 1 {
		t.Fatalf("background re-checks: %d, want 1", n)
	}
}
