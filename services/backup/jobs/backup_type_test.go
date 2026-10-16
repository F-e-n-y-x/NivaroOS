package jobs

import (
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// Versioned (restic) backup jobs: validation, the ops a run asks of the
// engine, and the monthly check.

func backupJob(t *testing.T, name string) Job {
	t.Setenv("NIVAROOS_RESTIC", "/bin/true") // any executable: validation only looks
	j := sampleJob(name)
	j.Type = TypeBackup
	j.Sources = append(j.Sources, Endpoint{Kind: EPUSB, RefID: "usb-uuid", SubPath: "Photos", Label: "USB"})
	j.Dest = Endpoint{Kind: EPCloud, RefID: "gdrive", SubPath: "NivaroOS/" + name, Label: "Drive"}
	return j
}

func TestBackupTypeValidation(t *testing.T) {
	norm, fe := NormalizeJob(backupJob(t, "V"), validEnv())
	if len(fe) > 0 {
		t.Fatalf("valid backup job: %v", fe)
	}
	if r := norm.Retention; r.KeepDaily != DefaultKeepDaily || r.KeepWeekly != DefaultKeepWeekly || r.KeepMonthly != DefaultKeepMonthly {
		t.Errorf("default keep rules: %+v", r)
	}

	j := backupJob(t, "V")
	j.Sources[1] = Endpoint{Kind: EPSMB, RefID: "nas", SubPath: "share/x"}
	mustInvalid(t, j, "sources[1].kind", FieldInvalid)

	j = backupJob(t, "V")
	j.Filters.Include = []string{"*.jpg"}
	mustInvalid(t, j, "filters.include", FieldInvalid)

	j = backupJob(t, "V")
	j.Filters.Exclude = []string{"*.{tmp,bak}"}
	mustInvalid(t, j, "filters.exclude[0]", FieldInvalid)

	j = backupJob(t, "V")
	j.Retention.KeepDaily = -1
	mustInvalid(t, j, "retention.keep_daily", FieldOutOfRange)

	j = backupJob(t, "V")
	j.Dest.Encryption = &engine.Encryption{Mode: engine.EncryptFolder}
	mustInvalid(t, j, "dest.encryption.mode", FieldInvalid)

	j = sampleJob("C")
	j.Dest.Encryption = &engine.Encryption{Mode: engine.EncryptRestic}
	mustInvalid(t, j, "dest.encryption.mode", FieldInvalid)

	j = backupJob(t, "V")
	t.Setenv("NIVAROOS_RESTIC", "/nonexistent/restic")
	mustInvalid(t, j, "type", ErrResticMissing)
}

func TestBackupRunSnapshotsThenForgets(t *testing.T) {
	h := newHarness(t, true)
	j := h.createJob(backupJob(t, "Snap"))

	run := h.run(j, KindBackup)
	req := h.finish(1, okResult(3))
	if req.Op != engine.OpBackup || len(req.Sources) != 2 {
		t.Fatalf("backup request: op %s, %d sources", req.Op, len(req.Sources))
	}
	req = h.finish(2, engine.Result{Counts: engine.Counts{Deleted: 1}})
	if req.Op != engine.OpForget || req.Retention.KeepDaily != DefaultKeepDaily || req.Retention.KeepMonthly != DefaultKeepMonthly {
		t.Fatalf("prune request: op %s, retention %+v", req.Op, req.Retention)
	}
	if r := h.waitStatus(run, StatusSuccess, StatusFailed, StatusPartial); RunStatus(r.Status) != StatusSuccess {
		t.Fatalf("run ended %s (%s)", r.Status, r.ErrorCode)
	}

	check := h.run(j, KindVerify)
	req = h.finish(3, engine.Result{Counts: engine.Counts{Skipped: 1}})
	if req.Op != engine.OpCheck || req.PlanOp != engine.OpBackup || len(req.Sources) != 0 {
		t.Fatalf("check request: op %s plan %s, %d sources", req.Op, req.PlanOp, len(req.Sources))
	}
	h.waitStatus(check, StatusSuccess)
}

func TestMaintenanceChecksVersionedBackupsMonthly(t *testing.T) {
	h := newHarness(t, true)
	j := h.createJob(backupJob(t, "Monthly"))
	copyJob := h.createJob(sampleJob("Copy"))
	for _, id := range []string{j.ID, copyJob.ID} {
		now := time.Now()
		if err := h.svc.store.UpdateJobState(id, func(st *JobState) { st.LastSuccessAt = &now }); err != nil {
			t.Fatal(err)
		}
	}
	h.svc.verifyBackups()
	if n := len(h.eng.CallsTo("StartJob")); n != 0 {
		t.Fatalf("a new job was checked at once (%d engine jobs)", n)
	}
	later := time.Now().Add(verifyEvery + time.Hour)
	h.setNow(func() time.Time { return later })
	h.svc.verifyBackups()
	_, req := h.startedJob(1)
	if req.Op != engine.OpCheck || req.JobID != j.ID {
		t.Fatalf("maintenance started %s of %s", req.Op, req.JobID)
	}
	h.svc.verifyBackups() // one is queued now: no second
	runs, err := h.svc.store.ListRuns(RunFilter{Kinds: []RunKind{KindVerify}})
	if err != nil || len(runs) != 1 {
		t.Fatalf("verify runs: %d, %v", len(runs), err)
	}
}
