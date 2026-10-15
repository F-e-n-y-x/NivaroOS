//go:build e2e

package e2e

import (
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/jobs"
)

// A schedule missed while the box was off runs once after the next
// start (catch-up, 3 minutes after the clock is trusted); a disabled job
// or one without catch_up doesn't.
func TestScheduleCatchUp(t *testing.T) {
	if testing.Short() {
		t.Skip("waits for the 3-minute catch-up delay")
	}
	in := newInstance(t)
	writeFiles(t, in.path("src"), 20, 1<<10)
	want := tree(t, in.path("src"))
	mk := func(name string, enabled, catchUp bool) jobs.JobDetail {
		j := job(name, jobs.TypeCopy, in.endpoint(in.path("src")), in.endpoint(in.path("dst-"+name)))
		j.Enabled = enabled
		j.Triggers = []jobs.Trigger{{Kind: jobs.TriggerSchedule, Cron: "0 3 * * *", CatchUp: catchUp}}
		return in.createJob(j)
	}
	due := mk("due", true, true)
	off := mk("off", false, true)
	noCatch := mk("nocatch", true, false)

	// Pretend the jobs were made two days ago: two 03:00 runs missed.
	in.stop()
	store, err := jobs.OpenStore(in.dataDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{due.ID, off.ID, noCatch.ID} {
		if _, err := store.MutateJob(id, time.Now(), false, func(j *jobs.Job) error {
			j.CreatedAt = j.CreatedAt.Add(-48 * time.Hour)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	store.Close()
	in.start()

	var list jobs.RunList
	in.waitFor("the catch-up run", 4*time.Minute, func() bool {
		in.must("GET", "/runs?job_id="+due.ID, nil, &list)
		return len(list.Runs) > 0
	})
	if len(list.Runs) != 1 || list.Runs[0].Trigger != jobs.RunByCatchUp {
		t.Fatalf("runs: %+v", list.Runs)
	}
	if r := in.waitRun(list.Runs[0].ID, time.Minute); r.Status != jobs.StatusSuccess {
		t.Fatalf("catch-up run: %s %s", r.Status, r.ErrorCode)
	}
	sameTree(t, want, tree(t, in.path("dst-due")))
	for _, id := range []string{off.ID, noCatch.ID} {
		in.must("GET", "/runs?job_id="+id, nil, &list)
		if len(list.Runs) != 0 {
			t.Errorf("job %s ran: %+v", id, list.Runs)
		}
	}
}
