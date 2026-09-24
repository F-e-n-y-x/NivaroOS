package engine

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func precheck(t *testing.T, e *Engine, req PrecheckRequest) PrecheckResult {
	t.Helper()
	if req.Guards == (Guards{}) {
		req.Guards = Guards{EmptySourcePct: 50, DeletePct: 10, ChangePct: 30}
	}
	res, err := e.Precheck(context.Background(), req)
	must(t, err)
	return res
}

func TestPrecheckEstimatesAndPasses(t *testing.T) {
	_, e, src, dst := twoVolumes(t)
	writeTree(t, filepath.Join(src.MountPoint, "p"), map[string]string{"a": "12345", "b/c": "678", ".cache/x": "skipped by preset"})
	res := precheck(t, e, PrecheckRequest{JobType: jobTypeMirror, Sources: []Endpoint{ep(src, "p")}, Dest: ep(dst, "p"),
		FirstRun: true, Filters: Filters{ExcludePresets: []string{ExcludePresetCaches}}})
	if !res.OK {
		t.Fatalf("precheck failed: %+v", res.Checks)
	}
	order := []string{CheckSourceResolves, CheckDestResolves, CheckAllowedRoots, CheckNotInside, CheckFSQuirks, CheckMetadata, CheckFreeSpace, CheckSourceSentinel, CheckDestMarker}
	if len(res.Checks) != len(order) {
		t.Fatalf("checks = %+v", res.Checks)
	}
	for i, id := range order {
		c := res.Checks[i]
		if c.ID != id || c.MsgKey != "backup.check."+id {
			t.Errorf("check %d = %+v, want %s", i, c, id)
		}
		if c.Status != CheckPass {
			t.Errorf("%s = %s (%s)", c.ID, c.Status, c.Code)
		}
	}
	est := res.Estimate
	if est.SourceFiles != 2 || est.SourceBytes != 8 || est.Partial || est.DestFree == nil || est.NeedBytes != int64(math.Ceil(8*1.05)) {
		t.Fatalf("estimate = %+v", est)
	}
	// Nothing was written.
	if _, err := os.Stat(filepath.Join(dst.MountPoint, "p")); !os.IsNotExist(err) {
		t.Fatal("precheck created the destination")
	}
}

func TestPrecheckReportsEachProblem(t *testing.T) {
	s, e, src, dst := twoVolumes(t)
	writeTree(t, filepath.Join(src.MountPoint, "p"), map[string]string{"a": "1"})

	// Source unplugged: the source check fails, what depends on it is skipped.
	s.unmount(src)
	res := precheck(t, e, PrecheckRequest{JobType: jobTypeCopy, Sources: []Endpoint{ep(src, "p")}, Dest: ep(dst, "p"), FirstRun: true})
	if res.OK || checkByID(res.Checks, CheckSourceResolves).Code != CodeSourceOffline {
		t.Fatalf("offline source: %+v", res.Checks)
	}
	for _, id := range []string{CheckNotInside, CheckFSQuirks, CheckFreeSpace, CheckSourceSentinel} {
		if c := checkByID(res.Checks, id); c.Status != CheckSkip {
			t.Errorf("%s = %s with the source offline", id, c.Status)
		}
	}
	s.remount(src)

	// Empty source.
	must(t, os.MkdirAll(filepath.Join(src.MountPoint, "empty"), 0o755))
	res = precheck(t, e, PrecheckRequest{JobType: jobTypeCopy, Sources: []Endpoint{ep(src, "empty")}, Dest: ep(dst, "e"), FirstRun: true})
	if c := checkByID(res.Checks, CheckSourceSentinel); res.OK || c.Code != CodeEmptySource {
		t.Fatalf("empty source: %+v", c)
	}
	// A folder that already belongs to another job: a warning for a new job.
	writeTree(t, filepath.Join(dst.MountPoint, "taken"), map[string]string{MarkerFile: `{"v":1,"job_id":"bk_other","dest_folder_id":"x"}`})
	res = precheck(t, e, PrecheckRequest{JobType: jobTypeCopy, Sources: []Endpoint{ep(src, "p")}, Dest: ep(dst, "taken"), FirstRun: true})
	if c := checkByID(res.Checks, CheckDestMarker); !res.OK || c.Status != CheckWarn || c.Code != CodeDestMarkerMismatch || c.Args["job_id"] != "bk_other" {
		t.Fatalf("taken folder: ok=%v %+v", res.OK, c)
	}
	// Source inside the destination.
	res = precheck(t, e, PrecheckRequest{JobType: jobTypeMirror, Sources: []Endpoint{ep(src, "p/inner")}, Dest: ep(src, "p"), FirstRun: true})
	if c := checkByID(res.Checks, CheckNotInside); res.OK || c.Code != CodeDestInsideSource {
		t.Fatalf("inside: %+v", c)
	}
	// A bad filter is a request error, not a check.
	if _, err := e.Precheck(context.Background(), PrecheckRequest{JobType: jobTypeCopy, Sources: []Endpoint{ep(src, "p")}, Dest: ep(dst, "p"),
		Filters: Filters{ExcludePresets: []string{"everything"}}}); CodeOf(err) != CodeInvalidFilter {
		t.Fatalf("bad preset: %v", err)
	}
	if err := ValidateFilters(Filters{Exclude: []string{"a\nb"}}); CodeOf(err) != CodeInvalidFilter {
		t.Fatalf("pattern with a newline: %v", err)
	}
	if err := ValidateFilters(Filters{Exclude: []string{"*.tmp", "/cache/**"}, Include: []string{"*.jpg"}, MaxSizeBytes: 1 << 30}); err != nil {
		t.Fatalf("valid filters: %v", err)
	}
	if _, err := e.Precheck(context.Background(), PrecheckRequest{JobType: "twoway", Sources: []Endpoint{ep(src, "p")}, Dest: ep(dst, "p")}); CodeOf(err) != CodeInternal {
		t.Fatalf("unknown type: %v", err)
	}
}

func TestExcludePresetsAndFilters(t *testing.T) {
	_, e, src, dst := twoVolumes(t)
	writeTree(t, filepath.Join(src.MountPoint, "h"), map[string]string{
		"keep.txt": "k", "node_modules/x.js": "n", "a/.cache/y": "c", "Thumbs.db": "t", "big.iso": "0123456789", "~$doc.docx": "l",
	})
	req := baseReq(OpCopy, ep(src, "h"), ep(dst, "h"))
	req.Filters = Filters{ExcludePresets: ExcludePresets, MaxSizeBytes: 5}
	expectState(t, runJob(t, e, req), JobDone, "")
	got := readTree(t, filepath.Join(dst.MountPoint, "h"))
	delete(got, MarkerFile)
	if len(got) != 1 || got["keep.txt"] != "k" {
		t.Fatalf("copied %v", keys(got))
	}
}
