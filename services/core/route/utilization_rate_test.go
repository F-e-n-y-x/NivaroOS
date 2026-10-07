package route

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
)

// Walks the publisher's 500 ms ticks for a span and counts what it sends.
func countTicks(start time.Time, span time.Duration, fastUntil int64) (usual, live int) {
	var last time.Time
	for now := start; now.Before(start.Add(span)); now = now.Add(utilizationFastEvery) {
		switch utilizationDue(now, last, fastUntil) {
		case UtilizationEvent:
			last = now
			usual++
		case UtilizationLiveEvent:
			live++
		}
	}
	return
}

func TestUtilizationPaceWithoutLease(t *testing.T) {
	usual, live := countTicks(time.Unix(1000, 0), time.Minute, 0)
	if usual != 12 || live != 0 {
		t.Fatalf("no lease: want 12 usual readings a minute and no live ones, got %d usual, %d live", usual, live)
	}
}

func TestUtilizationPaceWithLease(t *testing.T) {
	start := time.Unix(1000, 0)
	until := start.Add(utilizationFastLease).UnixNano()
	usual, live := countTicks(start, utilizationFastLease, until)
	// 15 s at 2 Hz: 30 readings, 3 of them the usual 5 s ones.
	if usual != 3 || live != 27 {
		t.Fatalf("lease: want 3 usual + 27 live, got %d + %d", usual, live)
	}
	// The lease lapsing puts it back to 5 s.
	usual, live = countTicks(start.Add(utilizationFastLease), time.Minute, until)
	if usual != 12 || live != 0 {
		t.Fatalf("after the lease: want 12 usual and no live, got %d usual, %d live", usual, live)
	}
}

func TestUtilizationJitterKeepsFiveSeconds(t *testing.T) {
	last := time.Unix(1000, 0)
	// A tick that fires a little early still counts as the 5 s one.
	if got := utilizationDue(last.Add(5*time.Second-10*time.Millisecond), last, 0); got != UtilizationEvent {
		t.Fatalf("early tick: got %q", got)
	}
	if got := utilizationDue(last.Add(4*time.Second), last, 0); got != "" {
		t.Fatalf("4 s in without a lease: got %q", got)
	}
}

func TestPostUtilizationLiveStartsLease(t *testing.T) {
	utilizationFastUntil.Store(0)
	t.Cleanup(func() { utilizationFastUntil.Store(0) })
	rec := httptest.NewRecorder()
	before := time.Now()
	if err := postUtilizationLive(echo.New().NewContext(httptest.NewRequest(http.MethodPost, "/v1/sys/utilization/live", nil), rec)); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var body struct {
		Data struct {
			LeaseMs int64 `json:"lease_ms"`
			EveryMs int64 `json:"every_ms"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Data.LeaseMs != 15000 || body.Data.EveryMs != 500 {
		t.Fatalf("body %s", rec.Body.String())
	}
	if until := time.Unix(0, utilizationFastUntil.Load()); until.Before(before.Add(utilizationFastLease)) {
		t.Fatalf("lease ends %v, want at least %v", until, before.Add(utilizationFastLease))
	}
}
