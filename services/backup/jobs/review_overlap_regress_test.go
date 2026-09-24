package jobs

import (
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// A mirror deletes whatever in its destination isn't in its source, and
// only its own root marker and recycle folder are protected. A mirror
// whose destination holds another job's destination would recycle that
// backup (marker included) on its next run; saving such a job must be
// refused.
func TestMirrorDestCannotContainAnotherJobsDest(t *testing.T) {
	h := newHarness(t, true)
	a := sampleJob("Photos")
	a.Dest.SubPath = "Backups/Photos"
	wantStatus(t, doRequest(h.svc, "POST", "/v1/backup/jobs", a, reqOpts{}), 201)

	b := sampleJob("Docs")
	b.Type = TypeMirror
	b.Retention.VersionsDays = 30
	b.Dest.SubPath = "Backups"
	rec := doRequest(h.svc, "POST", "/v1/backup/jobs", b, reqOpts{})
	if rec.Code == 201 {
		t.Fatalf("a mirror into %q was saved although job %q backs up into %q below it", b.Dest.SubPath, a.Name, a.Dest.SubPath)
	}
}

// Editing a job keeps its own destination; another job's nested
// destination is refused on update too, and exact duplicates as well.
func TestDestOverlapOnUpdateAndDuplicates(t *testing.T) {
	h := newHarness(t, true)
	a := h.createJob(sampleJob("Photos"))
	b := h.createJob(sampleJob("Docs"))

	// Saving a unchanged is fine (its own destination doesn't count).
	a.Name = "Photos 2"
	wantStatus(t, doRequest(h.svc, "PUT", "/v1/backup/jobs/"+a.ID, a, reqOpts{}), 200)

	// b into a's folder: refused with the field code.
	b.Dest = a.Dest
	rec := doRequest(h.svc, "PUT", "/v1/backup/jobs/"+b.ID, b, reqOpts{})
	wantStatus(t, rec, 400)
	if fe := errorBody(t, rec).FieldErrors; fe["dest.sub_path"] != string(FieldDestOverlapsJob) {
		t.Errorf("field errors %v", fe)
	}
	// Another drive, same sub path: no overlap.
	b.Dest.RefID = "other-uuid"
	wantStatus(t, doRequest(h.svc, "PUT", "/v1/backup/jobs/"+b.ID, b, reqOpts{}), 200)
}

// POST /validate warns about folders shared with a mirror.
func TestValidateWarnsAboutMirrorOverlap(t *testing.T) {
	h := newHarness(t, true)
	h.eng.Lock()
	h.eng.PrecheckV = engine.PrecheckResult{OK: true, Checks: []engine.Check{{ID: engine.CheckDestResolves, Status: engine.CheckPass}}}
	h.eng.Unlock()
	m := sampleJob("Mirror")
	m.Type = TypeMirror
	m.Retention.VersionsDays = 30
	m.Sources = []Endpoint{{Kind: EPVolume, RefID: "root-uuid", SubPath: "DATA/Media"}}
	h.createJob(m)
	// This job writes into the mirror's source.
	j := sampleJob("Into media")
	j.Dest = Endpoint{Kind: EPVolume, RefID: "root-uuid", SubPath: "DATA/Media/Copies"}
	j.Sources = []Endpoint{{Kind: EPVolume, RefID: "tank-uuid", SubPath: "Photos"}}
	rec := doRequest(h.svc, "POST", "/v1/backup/validate", j, reqOpts{})
	var res ValidateResult
	envelope(t, rec, &res)
	found := false
	for _, c := range res.Checks {
		if c.ID == CheckJobOverlap && c.Status == engine.CheckWarn && c.MsgKey == "backup.check.overlap_mirror_source" && c.Args["job"] == "Mirror" {
			found = true
		}
	}
	if !found || !res.OK {
		t.Errorf("no overlap warning (or not ok): %+v", res)
	}
}

// "Reconnect" doesn't take over another job's folder silently: a marker
// naming a job that still exists here is refused, one naming a job this
// box doesn't know needs adopt_other_job.
func TestReconnectRefusesAnotherJobsMarker(t *testing.T) {
	h := newHarness(t, true)
	other := h.createJob(sampleJob("Other"))
	marker := &engine.Marker{V: 1, JobID: other.ID, DestFolderID: "other-folder"}
	h.eng.Lock()
	h.eng.ResolveFn = func(r engine.ResolveRequest) (engine.Resolved, error) {
		return engine.Resolved{Root: "/x", Online: true, Marker: marker}, nil
	}
	h.eng.Unlock()
	job := h.createJob(sampleJob("Docs"))
	path := "/v1/backup/jobs/" + job.ID + "/reconnect-dest"

	rec := doRequest(h.svc, "POST", path, ReconnectRequest{AdoptOtherJob: true}, reqOpts{})
	wantStatus(t, rec, 409)
	if b := errorBody(t, rec); b.ErrorCode != ErrDestMarkerMismatch {
		t.Errorf("existing job's folder: %+v", b)
	}

	h.eng.Lock()
	marker = &engine.Marker{V: 1, JobID: "bk_from_before", DestFolderID: "old-folder"}
	h.eng.Unlock()
	wantStatus(t, doRequest(h.svc, "POST", path, nil, reqOpts{}), 409)
	if got, _ := h.svc.store.GetJob(job.ID); got.DestFolderID == "old-folder" {
		t.Fatal("adopted without confirmation")
	}
	wantStatus(t, doRequest(h.svc, "POST", path, ReconnectRequest{AdoptOtherJob: true}, reqOpts{}), 200)
	if got, _ := h.svc.store.GetJob(job.ID); got.DestFolderID != "old-folder" {
		t.Errorf("not adopted after confirmation: %q", got.DestFolderID)
	}
}
