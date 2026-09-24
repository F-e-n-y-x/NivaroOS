package enginetest

import (
	"context"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

func TestFakeJobLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	f := New()

	events, err := f.Events(ctx)
	if err != nil {
		t.Fatal(err)
	}
	id, err := f.StartJob(ctx, engine.JobRequest{Op: engine.OpSync, RunID: "run_1"})
	if err != nil {
		t.Fatal(err)
	}
	logs, err := f.JobLog(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Log(id, engine.LogLine{Lvl: engine.LogInfo, Code: "copied", MsgKey: "backup.log.copied"}); err != nil {
		t.Fatal(err)
	}
	if err := f.Progress(id, engine.Stats{Bytes: 10, TotalBytes: 20, Step: "transfer"}); err != nil {
		t.Fatal(err)
	}
	st, err := f.JobStatus(ctx, id)
	if err != nil || st.State != engine.JobRunning || st.Stats.Bytes != 10 {
		t.Fatalf("status = %+v, %v", st, err)
	}
	if err := f.Finish(id, engine.Result{Counts: engine.Counts{Added: 3}}); err != nil {
		t.Fatal(err)
	}
	if err := f.Finish(id, engine.Result{}); err == nil {
		t.Error("finishing twice should fail")
	}

	var got []engine.LogLine
	for l := range logs {
		got = append(got, l)
	}
	if len(got) != 1 || got[0].Code != "copied" || got[0].T.IsZero() {
		t.Fatalf("log lines = %+v", got)
	}
	select {
	case ev := <-events:
		if ev.Type != engine.EventJobDone || ev.JobID != id || ev.State != engine.JobDone || ev.RunID != "run_1" {
			t.Fatalf("event = %+v", ev)
		}
	case <-ctx.Done():
		t.Fatal("no job.done event")
	}
	st, _ = f.JobStatus(ctx, id)
	if st.Result == nil || st.Result.Counts.Added != 3 || st.EndedAt == nil {
		t.Fatalf("final status = %+v", st)
	}
	if n := len(f.CallsTo("StartJob")); n != 1 {
		t.Errorf("StartJob calls = %d", n)
	}
}

func TestFakeStopJob(t *testing.T) {
	ctx := context.Background()
	f := New()
	id, _ := f.StartJob(ctx, engine.JobRequest{Op: engine.OpCopy})
	if err := f.StopJob(ctx, id); err != nil {
		t.Fatal(err)
	}
	st, _ := f.JobStatus(ctx, id)
	if st.State != engine.JobError || st.Result.ErrorCode != engine.CodeCancelledByUser {
		t.Fatalf("status after stop = %+v", st)
	}
	// A finished job's stop is a no-op.
	if err := f.StopJob(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.JobStatus(ctx, 999); engine.CodeOf(err) != engine.CodeNotFound {
		t.Fatalf("unknown job err = %v", err)
	}
}

func TestFakeEventsCloseOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := New()
	ch, _ := f.Events(ctx)
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("unexpected event")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("channel not closed after cancel")
	}
	f.Emit(engine.Event{Type: engine.EventVolumeMounted}) // no subscribers left: must not panic
}

func TestFakeHealthMatchesContract(t *testing.T) {
	h, err := New().Health(context.Background())
	if err != nil || h.API != engine.APIVersion {
		t.Fatalf("health = %+v, %v", h, err)
	}
}
