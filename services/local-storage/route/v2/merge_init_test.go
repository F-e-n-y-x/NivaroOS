package v2

import (
	"net/http"
	"testing"
)

func TestPlanInitMerge(t *testing.T) {
	ok := initMergeState{MergerFSInstalled: true, MountPoint: "/DATA", DataExists: true, DataIsRealDir: true, DataEmpty: false}

	// mergerfs missing is checked first - before anything is moved
	st := ok
	st.MergerFSInstalled = false
	if _, code, _ := planInitMerge(st); code != http.StatusBadRequest {
		t.Fatalf("no mergerfs: %d", code)
	}
	for _, mp := range []string{"", "/", "/etc", "/DATA/", "/mnt/x", "/var/lib/nivaroos/files"} {
		st := ok
		st.MountPoint = mp
		if _, code, _ := planInitMerge(st); code != http.StatusBadRequest {
			t.Errorf("mount point %q: %d", mp, code)
		}
	}
	st = ok
	st.DataIsMountpoint = true
	if _, code, _ := planInitMerge(st); code != http.StatusConflict {
		t.Fatalf("/DATA mounted: %d", code)
	}
	st = ok
	st.DataIsRealDir = false
	if _, code, _ := planInitMerge(st); code != http.StatusConflict {
		t.Fatalf("/DATA symlink: %d", code)
	}
	st = ok
	st.BaseExists, st.BaseEmpty = true, false
	if _, code, _ := planInitMerge(st); code != http.StatusConflict {
		t.Fatalf("both non-empty: %d", code)
	}

	plan, code, _ := planInitMerge(ok)
	if code != 0 || !plan.MoveDataToBase || plan.RemoveEmptyBase {
		t.Fatalf("normal: %+v %d", plan, code)
	}
	st = ok
	st.BaseExists, st.BaseEmpty = true, true
	plan, code, _ = planInitMerge(st)
	if code != 0 || !plan.MoveDataToBase || !plan.RemoveEmptyBase {
		t.Fatalf("empty base: %+v %d", plan, code)
	}
	st = ok
	st.DataEmpty = true
	st.BaseExists, st.BaseEmpty = true, false
	if plan, code, _ := planInitMerge(st); code != 0 || plan.MoveDataToBase {
		t.Fatalf("empty /DATA: %+v %d", plan, code)
	}
	st = initMergeState{MergerFSInstalled: true, MountPoint: "/DATA"}
	if plan, code, _ := planInitMerge(st); code != 0 || plan.MoveDataToBase {
		t.Fatalf("no /DATA: %+v %d", plan, code)
	}
}
