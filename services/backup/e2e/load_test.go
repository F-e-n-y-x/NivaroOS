//go:build e2e

package e2e

import (
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
	"github.com/F-e-n-y-x/NivaroOS/services/backup/jobs"
)

// hangingServer accepts connections and never answers: a cloud account
// that has stopped responding.
func hangingServer(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var conns []net.Conn
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			conns = append(conns, c)
		}
	}()
	t.Cleanup(func() {
		l.Close()
		for _, c := range conns {
			c.Close()
		}
	})
	return l.Addr().String()
}

// timed runs one GET and fails if it took longer than max.
func (in *instance) timed(path string, max time.Duration, out interface{}) time.Duration {
	in.t.Helper()
	start := time.Now()
	in.must("GET", path, nil, out)
	d := time.Since(start)
	if d > max {
		in.t.Errorf("GET %s took %s (limit %s)", path, d.Round(time.Millisecond), max)
	}
	return d
}

// The Backup page and the run history stay fast with thousands of runs,
// a large backup and a cloud destination that hangs.
func TestPageLoadTimes(t *testing.T) {
	in := newInstance(t)
	hung := hangingServer(t)
	in.writeConf(fmt.Sprintf("[e2ecloud]\ntype = local\n\n[hung]\ntype = webdav\nurl = http://%s/\nvendor = other\n", hung))

	writeFiles(t, in.path("src"), 5000, 512)
	big := in.createJob(job("big", jobs.TypeMirror, in.endpoint(in.path("src")), in.endpoint(in.path("dst"))))
	realRun := in.waitRun(in.runJob(big.ID), 2*time.Minute)
	if realRun.Status != jobs.StatusSuccess {
		t.Fatalf("%s %s", realRun.Status, realRun.ErrorCode)
	}
	start := time.Now()
	hj := job("hung", jobs.TypeCopy, in.endpoint(in.path("src")), engine.Endpoint{Kind: engine.EPCloud, RefID: "hung", SubPath: "b", Label: "hung"})
	var hungJob jobs.JobDetail
	st := in.call("POST", "/jobs", hj, &hungJob)
	t.Logf("create job on a hanging cloud: HTTP %d in %s", st, time.Since(start).Round(time.Millisecond))
	if time.Since(start) > 20*time.Second {
		t.Errorf("creating a job on a hanging cloud took %s", time.Since(start))
	}

	t.Logf("setup took %s", time.Since(start))
	start = time.Now()
	// Thousands of past runs.
	in.stop()
	store, err := jobs.OpenStore(in.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	const n = 5000
	base := time.Now().Add(-n * time.Hour)
	for i := 0; i < n; i++ {
		at := base.Add(time.Duration(i) * time.Hour)
		end := at.Add(10 * time.Minute)
		status := jobs.StatusSuccess
		if i%7 == 0 {
			status = jobs.StatusPartial
		}
		r := jobs.RunRow{ID: jobs.NewRunID(at), JobID: big.ID, Kind: string(jobs.KindBackup), Trigger: string(jobs.RunBySchedule),
			Status: string(status), Phase: string(jobs.PhaseTransfer), Attempt: 1, QueuedAt: at, StartedAt: &at, EndedAt: &end,
			FilesAdded: 12, BytesTransferred: 1 << 20, Summary: "backup.run.summary.ok|{\"added\":12}"}
		if err := store.CreateRun(r); err != nil {
			t.Fatal(err)
		}
	}
	store.Close()
	t.Logf("%d runs stored in %s", n, time.Since(start))
	in.start()

	const fast = time.Second
	var list []jobs.JobListItem
	d := in.timed("/jobs", 5*time.Second, &list) // first ask resolves destinations (3 s cap)
	t.Logf("GET /jobs first: %s", d)
	if len(list) < 1 {
		t.Fatalf("jobs: %+v", list)
	}
	in.timed("/jobs", fast, &list)
	var jd jobs.JobDetail
	in.timed("/jobs/"+big.ID, 5*time.Second, &jd)
	in.timed("/jobs/"+big.ID, fast, &jd)
	if hungJob.ID != "" {
		in.timed("/jobs/"+hungJob.ID, 5*time.Second, &jd)
		in.timed("/jobs/"+hungJob.ID, fast, &jd)
		start := time.Now()
		st := in.call("GET", "/jobs/"+hungJob.ID+"/versions", nil, nil)
		if d := time.Since(start); st != http.StatusConflict || d > 31*time.Second {
			t.Errorf("versions of a hanging cloud: HTTP %d after %s", st, d)
		}
		// The next ask is answered from the cache, not after another wait.
		start = time.Now()
		st = in.call("GET", "/jobs/"+hungJob.ID+"/versions", nil, nil)
		if d := time.Since(start); st != http.StatusConflict || d > fast {
			t.Errorf("versions of a hanging cloud, asked again: HTTP %d after %s", st, d)
		}
	}

	// History: every page, newest first, quickly.
	var page jobs.RunList
	pages, total := 0, 0
	before := ""
	for {
		in.timed("/runs?limit=50&before="+before, fast, &page)
		pages++
		total += len(page.Runs)
		if page.NextBefore == "" {
			break
		}
		before = page.NextBefore
	}
	if total < n {
		t.Errorf("history has %d runs, want %d+", total, n)
	}
	in.timed("/runs?job_id="+big.ID+"&limit=200", fast, &page)
	in.timed("/runs?status=partial&limit=200", fast, &page)
	var rd jobs.RunDetail
	in.timed("/runs/"+realRun.ID, fast, &rd)
	var lp jobs.LogPage
	in.timed("/runs/"+realRun.ID+"/log?limit=2000", fast, &lp)

	var vers []engine.Version
	in.timed("/jobs/"+big.ID+"/versions", fast, &vers)
	var br engine.BrowseResult
	in.timed("/jobs/"+big.ID+"/versions/current/browse?path=d001", fast, &br)
	t.Logf("%d history pages", pages)
}
