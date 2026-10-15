//go:build e2e

package e2e

import (
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
	"github.com/F-e-n-y-x/NivaroOS/services/backup/jobs"
)

func TestJobCRUD(t *testing.T) {
	in := newInstance(t)
	if err := os.MkdirAll(in.path("src"), 0o755); err != nil {
		t.Fatal(err)
	}
	j := in.createJob(job("crud", jobs.TypeCopy, in.endpoint(in.path("src")), in.endpoint(in.path("dst"))))
	if j.Revision != 1 {
		t.Fatalf("revision %d", j.Revision)
	}
	edit := j.Job
	edit.Name = "crud renamed"
	edit.Filters.Exclude = []string{"/thumbs/**"}
	var got jobs.JobDetail
	in.must("PUT", "/jobs/"+j.ID, edit, &got)
	if got.Name != "crud renamed" || got.Revision != 2 || len(got.Filters.Exclude) != 1 {
		t.Fatalf("after PUT: %+v", got.Job)
	}
	// A stale revision is a conflict, not a silent overwrite.
	if st := in.call("PUT", "/jobs/"+j.ID, edit, nil); st != http.StatusConflict {
		t.Fatalf("stale PUT: HTTP %d", st)
	}
	var list []jobs.JobListItem
	in.must("GET", "/jobs", nil, &list)
	if len(list) != 1 || list[0].Name != "crud renamed" {
		t.Fatalf("list: %+v", list)
	}
	// Survives a restart.
	in.stop()
	in.start()
	in.must("GET", "/jobs/"+j.ID, nil, &got)
	if got.Name != "crud renamed" {
		t.Fatalf("after restart: %+v", got.Job)
	}
	in.must("DELETE", "/jobs/"+j.ID, nil, nil)
	if st := in.call("GET", "/jobs/"+j.ID, nil, nil); st != http.StatusNotFound {
		t.Fatalf("deleted job: HTTP %d", st)
	}
}

// slowJob makes a source of n 16 KiB files and a job over it; with
// slow (RCLONE_BWLIMIT=1M) 500 files take about 8 s.
const slow = "RCLONE_BWLIMIT=1M"

func slowJob(t *testing.T, in *instance, n int) (jobs.JobDetail, map[string]string) {
	writeFiles(t, in.path("src"), n, 16<<10)
	j := job("slow", jobs.TypeCopy, in.endpoint(in.path("src")), in.endpoint(in.path("dst")))
	j.Options.LowPriority = true
	return in.createJob(j), tree(t, in.path("src"))
}

// waitTransferring waits until run id is copying files.
func (in *instance) waitTransferring(id string) {
	in.t.Helper()
	in.waitFor("run "+id+" to transfer", time.Minute, func() bool {
		r := in.getRun(id)
		if r.Status.Final() {
			in.t.Fatalf("run %s ended %s before the test could interrupt it; make it slower", id, r.Status)
		}
		return r.Status == jobs.StatusRunning && r.Live != nil && r.Live.Files > 0
	})
}

func TestCancelMidRun(t *testing.T) {
	in := newInstance(t, slow)
	j, want := slowJob(t, in, 500)
	id := in.runJob(j.ID)
	in.waitTransferring(id)
	in.must("POST", "/runs/"+id+"/cancel", struct{}{}, nil)
	r := in.waitRun(id, 30*time.Second)
	if r.Status != jobs.StatusCancelled {
		t.Fatalf("cancelled run: %s %s", r.Status, r.ErrorCode)
	}
	// The next run completes the copy.
	r = in.waitRun(in.runJob(j.ID), 2*time.Minute)
	if r.Status != jobs.StatusSuccess {
		t.Fatalf("run after cancel: %s %s", r.Status, r.ErrorCode)
	}
	sameTree(t, want, tree(t, in.path("dst")))
}

// A crash (SIGKILL) mid-transfer must not leave the run "running"
// forever: the next start ends it in a final state, and the next run
// finishes the copy.
func TestCrashMidRun(t *testing.T) {
	in := newInstance(t, slow)
	j, want := slowJob(t, in, 500)
	id := in.runJob(j.ID)
	in.waitTransferring(id)
	in.kill()
	in.start()
	r := in.waitRun(id, 30*time.Second)
	if r.Status == jobs.StatusSuccess {
		t.Fatalf("killed run reports success")
	}
	if r.ErrorCode == "" || r.EndedAt == nil {
		t.Fatalf("killed run ended %s with no error code / end time: %+v", r.Status, r.Run)
	}
	var list jobs.RunList
	in.must("GET", "/runs?job_id="+j.ID+"&status=queued,running", nil, &list)
	for _, rr := range list.Runs {
		in.waitRun(rr.ID, 2*time.Minute)
	}
	r = in.waitRun(in.runJob(j.ID), 2*time.Minute)
	if r.Status != jobs.StatusSuccess {
		t.Fatalf("run after crash: %s %s", r.Status, r.ErrorCode)
	}
	sameTree(t, want, tree(t, in.path("dst")))
}

// systemctl stop mid-transfer (every update restarts the service): the
// service exits in time, the run ends interrupted, not "cancelled by
// you", and its retry finishes the copy after the next start.
func TestStopMidRun(t *testing.T) {
	in := newInstance(t, slow)
	j, want := slowJob(t, in, 500)
	id := in.runJob(j.ID)
	in.waitTransferring(id)
	in.stop()
	in.start()
	r := in.waitRun(id, 30*time.Second)
	if r.Status != jobs.StatusInterrupted || r.ErrorCode != jobs.ErrInterrupted {
		t.Fatalf("stopped run: %s %q", r.Status, r.ErrorCode)
	}
	var list jobs.RunList
	in.must("GET", "/runs?job_id="+j.ID, nil, &list)
	if len(list.Runs) != 2 || list.Runs[0].Trigger != jobs.RunByRetry {
		t.Fatalf("no retry queued: %+v", list.Runs)
	}
	if r := in.waitRun(list.Runs[0].ID, 2*time.Minute); r.Status != jobs.StatusSuccess {
		t.Fatalf("retry: %s %s", r.Status, r.ErrorCode)
	}
	sameTree(t, want, tree(t, in.path("dst")))
}

// A transient failure (source folder gone) is retried after the backoff
// and the retry succeeds once the source is back.
func TestRetryBackoff(t *testing.T) {
	in := newInstance(t)
	writeFiles(t, in.path("src"), 10, 1<<10)
	want := tree(t, in.path("src"))
	jj := job("retry", jobs.TypeCopy, in.endpoint(in.path("src")), in.endpoint(in.path("dst")))
	jj.Retry = jobs.Retry{Max: 3, BackoffSec: []int{10}}
	j := in.createJob(jj)
	if err := os.Rename(in.path("src"), in.path("src.away")); err != nil {
		t.Fatal(err)
	}
	first := in.waitRun(in.runJob(j.ID), time.Minute)
	if first.Status != jobs.StatusFailed || jobs.ClassOf(first.ErrorCode) != jobs.ClassTransient {
		t.Fatalf("first attempt: %s %s (class %s)", first.Status, first.ErrorCode, jobs.ClassOf(first.ErrorCode))
	}
	var list jobs.RunList
	in.waitFor("a queued retry", 10*time.Second, func() bool {
		in.must("GET", "/runs?job_id="+j.ID, nil, &list)
		return len(list.Runs) == 2
	})
	retry := list.Runs[0]
	if retry.Trigger != jobs.RunByRetry || retry.Attempt != 2 {
		t.Fatalf("retry run: trigger %s attempt %d", retry.Trigger, retry.Attempt)
	}
	if err := os.Rename(in.path("src.away"), in.path("src")); err != nil {
		t.Fatal(err)
	}
	r := in.waitRun(retry.ID, time.Minute)
	if r.Status != jobs.StatusSuccess {
		t.Fatalf("retry: %s %s", r.Status, r.ErrorCode)
	}
	if gap := r.StartedAt.Sub(*first.EndedAt); gap < 9*time.Second {
		t.Fatalf("retry started %s after the failure, backoff is 10 s", gap)
	}
	sameTree(t, want, tree(t, in.path("dst")))
}

func TestExcludes(t *testing.T) {
	in := newInstance(t)
	writeFiles(t, in.path("src", "photos"), 20, 1<<10)
	writeFiles(t, in.path("src", "thumbs"), 20, 1<<10)
	writeFiles(t, in.path("src", "photos", "thumbs"), 5, 1<<10) // not at the root: kept
	want := tree(t, in.path("src"))
	for p := range want {
		if len(p) > 7 && p[:7] == "thumbs/" {
			delete(want, p)
		}
	}
	jj := job("excl", jobs.TypeMirror, in.endpoint(in.path("src")), in.endpoint(in.path("dst")))
	jj.Filters.Exclude = []string{"/thumbs/**"}
	j := in.createJob(jj)
	r := in.waitRun(in.runJob(j.ID), time.Minute)
	if r.Status != jobs.StatusSuccess {
		t.Fatalf("%s %s", r.Status, r.ErrorCode)
	}
	sameTree(t, want, tree(t, in.path("dst")))
	// A mirror never deletes what it excludes... at the destination it
	// was never copied; and a second run is a no-op.
	r = in.waitRun(in.runJob(j.ID), time.Minute)
	if r.Status != jobs.StatusSuccess || r.Counts.Added+r.Counts.Changed+r.Counts.Deleted != 0 {
		t.Fatalf("second run: %s %+v", r.Status, r.Counts)
	}
}

// Many small files and one large one: everything arrives intact and the
// run's counts match. A large copy runs at full speed.
func TestManySmallAndLarge(t *testing.T) {
	in := newInstance(t)
	writeFiles(t, in.path("src", "small"), 3000, 2<<10)
	writeBig(t, in.path("src", "big.bin"), 768<<20)
	want := tree(t, in.path("src"))
	j := in.createJob(job("big", jobs.TypeCopy, in.endpoint(in.path("src")), cloud("cloud/dst")))
	r := in.waitRun(in.runJob(j.ID), 5*time.Minute)
	if r.Status != jobs.StatusSuccess {
		t.Fatalf("%s %s", r.Status, r.ErrorCode)
	}
	sameTree(t, want, tree(t, in.path("cloud", "dst")))
	// The destination's identity marker (.nivaroos-backup.json, < 1 KiB)
	// is written in the same transfer.
	if total := int64(3000*2<<10 + 768<<20); r.Counts.Added != int64(len(want)) || !aboutTotal(r.Counts.BytesTransferred, total) {
		t.Errorf("counts: added %d (want %d), bytes %d (want %d)", r.Counts.Added, len(want), r.Counts.BytesTransferred, total)
	}
	// A second run copies nothing.
	r = in.waitRun(in.runJob(j.ID), 5*time.Minute)
	if r.Status != jobs.StatusSuccess || r.Counts.Added+r.Counts.Changed != 0 || r.Counts.BytesTransferred != 0 {
		t.Fatalf("second run: %s %+v", r.Status, r.Counts)
	}
}

// The live numbers the run window shows match what really moves: bytes
// and files only grow and stay within the totals, and the speed is the
// real rate - here capped at 4 MiB/s - for small files as for large.
func TestProgressAndSpeed(t *testing.T) {
	const rate = 4 << 20
	in := newInstance(t, "RCLONE_BWLIMIT=4M")
	writeFiles(t, in.path("src", "small"), 2000, 8<<10) // ~16 MiB of small files
	writeBig(t, in.path("src", "zz-big.bin"), 16<<20)
	want := tree(t, in.path("src"))
	total := int64(2000*8<<10 + 16<<20)
	j := in.createJob(job("progress", jobs.TypeCopy, in.endpoint(in.path("src")), cloud("cloud/dst")))
	id := in.runJob(j.ID)

	type sample struct {
		at time.Time
		l  jobs.LiveStats
	}
	var samples []sample
	for {
		r := in.getRun(id)
		if r.Status.Final() {
			break
		}
		if r.Live != nil {
			l := *r.Live
			l.ETASec = nil
			if len(samples) == 0 || samples[len(samples)-1].l != l {
				samples = append(samples, sample{time.Now(), l})
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	r := in.waitRun(id, time.Second)
	if r.Status != jobs.StatusSuccess {
		t.Fatalf("%s %s", r.Status, r.ErrorCode)
	}
	sameTree(t, want, tree(t, in.path("cloud", "dst")))
	if !aboutTotal(r.Counts.BytesTransferred, total) {
		t.Errorf("bytes %d, want %d", r.Counts.BytesTransferred, total)
	}
	if len(samples) < 4 {
		t.Fatalf("only %d distinct live samples in an ~8 s run", len(samples))
	}
	var prev jobs.LiveStats
	start := samples[0].at
	for i, s := range samples {
		l := s.l
		t.Logf("%5.1fs %+v", s.at.Sub(start).Seconds(), l)
		if l.Bytes < prev.Bytes || l.Files < prev.Files {
			t.Errorf("sample %d: progress went backwards", i)
		}
		if !aboutTotal(l.Bytes, total) && l.Bytes > total || !aboutTotal(l.TotalBytes, total) && l.TotalBytes > total || l.Files > int64(len(want))+1 || l.TotalFiles > int64(len(want))+1 {
			t.Errorf("sample %d: more than the source has (%d bytes, %d files)", i, total, len(want))
		}
		if l.TotalBytes > 0 && l.Bytes > l.TotalBytes {
			t.Errorf("sample %d: bytes > total", i)
		}
		// Past the first two seconds (rclone's average warms up) the
		// speed must be the real rate, within a factor of two.
		if s.at.Sub(start) > 2*time.Second && l.Bytes < total && (l.SpeedBps < rate/2 || l.SpeedBps > rate*2) {
			t.Errorf("sample %d: speed %d B/s, real rate is %d B/s", i, l.SpeedBps, rate)
		}
		prev = l
	}
}

// aboutTotal: got is want plus at most the 1 KiB marker file.
func aboutTotal(got, want int64) bool { return got >= want && got <= want+1<<10 }

func TestMirrorRestore(t *testing.T) {
	in := newInstance(t)
	writeFiles(t, in.path("src"), 30, 8<<10)
	v1 := tree(t, in.path("src"))
	j := in.createJob(job("mirror", jobs.TypeMirror, in.endpoint(in.path("src")), cloud("cloud/mirror")))
	if r := in.waitRun(in.runJob(j.ID), time.Minute); r.Status != jobs.StatusSuccess {
		t.Fatalf("first run: %s %s", r.Status, r.ErrorCode)
	}
	// Lose and change some files; the mirror keeps the old ones in its
	// recycle folder (a version).
	os.Remove(in.path("src", "d000", "f000000.bin"))
	writeFile(t, in.path("src", "d001", "f000001.bin"), []byte("changed"))
	if r := in.waitRun(in.runJob(j.ID), time.Minute); r.Status != jobs.StatusSuccess {
		t.Fatalf("second run: %s %s", r.Status, r.ErrorCode)
	}
	var vers []engine.Version
	in.must("GET", "/jobs/"+j.ID+"/versions", nil, &vers)
	if len(vers) < 2 {
		t.Fatalf("versions: %+v", vers)
	}

	// Restore the current version to a new folder: identical to the source.
	var started jobs.RunStarted
	other := in.endpoint(in.path("restored"))
	in.must("POST", "/jobs/"+j.ID+"/restore", jobs.RestoreRequest{VersionID: "current", Target: jobs.RestoreTarget{Mode: "other", Endpoint: &other}}, &started)
	if r := in.waitRun(started.RunID, time.Minute); r.Status != jobs.StatusSuccess {
		t.Fatalf("restore to other: %s %s %+v", r.Status, r.ErrorCode, r.Summary)
	}
	sameTree(t, tree(t, in.path("src")), tree(t, in.path("restored")))

	// Restore the old files from the recycle version to the original
	// place, overwriting: back to v1.
	in.must("POST", "/jobs/"+j.ID+"/restore", jobs.RestoreRequest{VersionID: vers[1].ID, Target: jobs.RestoreTarget{Mode: "original"}, Conflict: engine.ConflictOverwrite}, &started)
	if r := in.waitRun(started.RunID, time.Minute); r.Status != jobs.StatusSuccess {
		t.Fatalf("restore to original: %s %s %+v", r.Status, r.ErrorCode, r.Summary)
	}
	sameTree(t, v1, tree(t, in.path("src")))
}

func TestPurgeOnDelete(t *testing.T) {
	in := newInstance(t)
	writeFiles(t, in.path("src"), 10, 1<<10)
	j := in.createJob(job("purge", jobs.TypeMirror, in.endpoint(in.path("src")), in.endpoint(in.path("dst", "job"))))
	if r := in.waitRun(in.runJob(j.ID), time.Minute); r.Status != jobs.StatusSuccess {
		t.Fatalf("%s %s", r.Status, r.ErrorCode)
	}
	writeFile(t, in.path("dst", "other.txt"), []byte("not the job's"))
	var del jobs.DeleteJobResult
	in.must("DELETE", "/jobs/"+j.ID+"?purge_data=true", nil, &del)
	if del.PurgeRunID == "" {
		t.Fatal("no purge run")
	}
	if r := in.waitRun(del.PurgeRunID, time.Minute); r.Status != jobs.StatusSuccess {
		t.Fatalf("purge: %s %s", r.Status, r.ErrorCode)
	}
	if _, err := os.Stat(in.path("dst", "job")); !os.IsNotExist(err) {
		t.Fatalf("job folder still there: %v", err)
	}
	if _, err := os.Stat(in.path("dst", "other.txt")); err != nil {
		t.Fatalf("purge removed a file outside the job folder: %v", err)
	}
	// Source untouched.
	if n := len(tree(t, in.path("src"))); n != 10 {
		t.Fatalf("source has %d files", n)
	}
}
