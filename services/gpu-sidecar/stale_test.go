package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func getStats(t *testing.T) (int, gpuStats) {
	t.Helper()
	rec := httptest.NewRecorder()
	handleGPUStats(rec, httptest.NewRequest(http.MethodGet, "/gpu-stats", nil))
	var s gpuStats
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}
	return rec.Code, s
}

func resetCache(t *testing.T) {
	old := queryGPUFn
	t.Cleanup(func() {
		queryGPUFn = old
		cached, cachedAt, cacheGood = gpuStats{}, time.Time{}, false
	})
	cached, cachedAt, cacheGood = gpuStats{}, time.Time{}, false
}

// An idle GPU whose nvidia-smi call fails for a moment keeps reporting its
// last reading, marked stale, instead of an error that makes clients hide it.
func TestFailedQueryAfterAGoodOneServesTheLastReading(t *testing.T) {
	resetCache(t)
	queryGPUFn = func() (gpuStats, error) { return gpuStats{Name: "GTX 1080 Ti", UtilizationPercent: 0}, nil }
	if code, s := getStats(t); code != 200 || s.Stale {
		t.Fatalf("first reading: %d %+v", code, s)
	}
	queryGPUFn = func() (gpuStats, error) { return gpuStats{}, errors.New("nvidia-smi: signal: killed") }
	cachedAt = time.Now().Add(-10 * time.Second) // past the 1 s reuse window
	code, s := getStats(t)
	if code != 200 || !s.Stale || s.Name != "GTX 1080 Ti" || s.StaleSeconds < 9 {
		t.Fatalf("after a failed query: %d %+v", code, s)
	}
}

// With no good reading, or one too old, a failure is still an error.
func TestFailedQueryWithNothingRecentIsAnError(t *testing.T) {
	resetCache(t)
	queryGPUFn = func() (gpuStats, error) { return gpuStats{}, errors.New("nvidia-smi: not found") }
	if code, s := getStats(t); code != http.StatusServiceUnavailable || s.Error == "" {
		t.Fatalf("no reading yet: %d %+v", code, s)
	}
	cached, cachedAt = gpuStats{Name: "old"}, time.Now().Add(-staleGrace-time.Second)
	if code, _ := getStats(t); code != http.StatusServiceUnavailable {
		t.Fatalf("reading too old: %d", code)
	}
}
