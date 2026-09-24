//go:build stress

// Stress test (spec §16.2), run nightly rather than in the default suite:
//
//	GOWORK=off go test -tags stress -run TestStress -timeout 2h ./engine/
//
// NIVARO_STRESS_FILES overrides the file count (default 1 000 000).
package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"testing"
	"time"
)

func TestStressMirrorManyFiles(t *testing.T) {
	n := 1_000_000
	if v := os.Getenv("NIVARO_STRESS_FILES"); v != "" {
		var err error
		if n, err = strconv.Atoi(v); err != nil {
			t.Fatalf("NIVARO_STRESS_FILES: %v", err)
		}
	}
	waitJobTimeout = 2 * time.Hour
	defer func() { waitJobTimeout = 60 * time.Second }()
	_, e, src, dst := twoVolumes(t)
	root := filepath.Join(src.MountPoint, "many")
	for i := 0; i < n; i++ {
		dir := filepath.Join(root, fmt.Sprintf("%03d", i%1000))
		if i < 1000 {
			must(t, os.MkdirAll(dir, 0o755))
		}
		must(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%07d", i)), []byte{byte(i)}, 0o644))
	}
	limit := SetMemoryLimit()
	defer debug.SetMemoryLimit(-1)
	baseline := runtime.NumGoroutine()

	var peak uint64
	stop := make(chan struct{})
	go func() {
		var ms runtime.MemStats
		for {
			select {
			case <-stop:
				return
			case <-time.After(time.Second):
				runtime.ReadMemStats(&ms)
				if ms.HeapInuse > peak {
					peak = ms.HeapInuse
				}
			}
		}
	}()
	req := baseReq(OpSync, ep(src, "many"), ep(dst, "many"))
	st := runJob(t, e, req)
	expectState(t, st, JobDone, "")
	if st.Result.Counts.Added != int64(n) {
		t.Fatalf("added %d of %d", st.Result.Counts.Added, n)
	}
	// A second run finds nothing to do; then stop one mid-way.
	req.FirstRun = false
	req.Baseline = &Baseline{SourceFiles: int64(n), DestFiles: int64(n)}
	expectState(t, runJob(t, e, withRun(req, "run_stress2")), JobDone, "")
	must(t, os.RemoveAll(filepath.Join(dst.MountPoint, "many", "500")))
	id, err := e.StartJob(context.Background(), withRun(req, "run_stress3"))
	must(t, err)
	time.Sleep(2 * time.Second)
	must(t, e.StopJob(context.Background(), id))
	waitJob(t, e, id)
	close(stop)
	t.Logf("peak heap in use %d MiB (limit %d MiB)", peak>>20, limit>>20)
	if peak > uint64(limit)*3/2 {
		t.Errorf("heap peaked at %d MiB, over 1.5x the %d MiB limit", peak>>20, limit>>20)
	}
	time.Sleep(2 * time.Second)
	if g := runtime.NumGoroutine(); g > baseline+20 {
		t.Errorf("%d goroutines after the runs, %d before: a leak", g, baseline)
	}
}
