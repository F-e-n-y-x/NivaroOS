package service

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Fake Docker API: two running containers, each stats call returns an
// increasing total_usage so Previous/Data can be told apart.
func TestGetContainerStats(t *testing.T) {
	var statsCalls, lists atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Api-Version", "1.52")
		switch {
		case strings.HasSuffix(r.URL.Path, "/_ping"):
			fmt.Fprint(w, "OK")
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			lists.Add(1)
			fmt.Fprint(w, `[{"Id":"a1","Names":["/immich"],"State":"running","Labels":{"icon":"i.png"}},{"Id":"b2","Names":["/nuvio"],"State":"running"}]`)
		case strings.HasSuffix(r.URL.Path, "/stats"):
			if r.URL.Query().Get("stream") != "false" {
				t.Errorf("stats must not stream: %s", r.URL.RawQuery)
			}
			n := statsCalls.Add(1)
			fmt.Fprintf(w, `{"cpu_stats":{"cpu_usage":{"total_usage":%d},"system_cpu_usage":%d},"memory_stats":{"usage":100,"stats":{"inactive_file":10}}}`, n, n*10)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	t.Setenv("DOCKER_HOST", "tcp://"+strings.TrimPrefix(srv.URL, "http://"))
	t.Setenv("DOCKER_API_VERSION", "1.52")

	ds := &dockerService{}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { ds.GetContainerStats() })
	}
	wg.Wait()
	if lists.Load() != 1 || statsCalls.Load() != 2 {
		t.Fatalf("concurrent requests should share one round: lists=%d stats=%d", lists.Load(), statsCalls.Load())
	}
	first := ds.GetContainerStats()
	if len(first) != 2 || first[0].Previous != nil {
		t.Fatalf("first round: %+v", first)
	}
	if first[0].Title != "immich" || first[0].Icon != "i.png" {
		t.Fatalf("title/icon: %+v", first[0])
	}

	time.Sleep(statsMaxAge + 50*time.Millisecond)
	second := ds.GetContainerStats()
	if statsCalls.Load() != 4 || lists.Load() != 1 || len(second) != 2 {
		t.Fatalf("second round (list still cached): stats=%d lists=%d %+v", statsCalls.Load(), lists.Load(), second)
	}
	for i := range second {
		if fmt.Sprint(second[i].Previous) != fmt.Sprint(first[i].Data) {
			t.Errorf("%s: previous %v, want first sample %v", second[i].Title, second[i].Previous, first[i].Data)
		}
	}
}
