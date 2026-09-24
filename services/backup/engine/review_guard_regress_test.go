package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// When the planning pass times out (a big tree, a slow share or cloud)
// only MaxDelete is left. A copy job has no recycle folder, so the
// change guard is its only protection against a source that was
// encrypted or overwritten: without a plan it must not silently
// overwrite the backup copies.
func TestPlanTimeoutKeepsTheChangeGuardForCopy(t *testing.T) {
	e, _, dst, req, root := guardSetup(t)
	saved := planTimeout
	planTimeout = time.Nanosecond
	t.Cleanup(func() { planTimeout = saved })
	req.Op = OpCopy
	req.Baseline = &Baseline{SourceFiles: 20, DestFiles: 20}
	for i := 0; i < 16; i++ { // 80 % "encrypted"
		must(t, os.WriteFile(filepath.Join(root, filepathJoin("f", i)), []byte("encrypted!"), 0o644))
	}
	runJob(t, e, req)
	n := 0
	for _, c := range destSnapshot(t, dst) {
		if c == "encrypted!" {
			n++
		}
	}
	if n > 0 {
		t.Fatalf("%d of 20 backup copies were overwritten with the encrypted source: a timed-out plan disables the change guard", n)
	}
}

// Accepting a tripped delete guard accepts what the user reviewed. If
// the source loses more files before the decision is carried out, the
// run must stop again instead of deleting the extra files too.
func TestAcceptedDeleteGuardIsBoundToWhatWasReviewed(t *testing.T) {
	e, _, _, req, root := guardSetup(t)
	for i := 0; i < 3; i++ { // 15 % > 10 %: trips, the user reviews 3
		must(t, os.Remove(filepath.Join(root, filepathJoin("f", i))))
	}
	st := runJob(t, e, req)
	expectState(t, st, JobError, CodeDeleteGuard)
	if st.Result.Guard == nil || st.Result.Guard.Count != 3 {
		t.Fatalf("guard %+v", st.Result.Guard)
	}
	// Before "Continue" runs, 6 more go (45 % in all, still below the
	// 50 % empty-source sentinel).
	for i := 3; i < 9; i++ {
		must(t, os.Remove(filepath.Join(root, filepathJoin("f", i))))
	}
	// What "Continue" on a reviewed list accepts (jobs acceptedGuards),
	// bound to the reviewed counts (jobs reviewedCounts).
	req.GuardOverride = []string{guardDelete, guardChange}
	req.GuardReviewed = &Reviewed{Deleted: st.Result.Guard.Count}
	st = runJob(t, e, req)
	if st.Result.Counts.Deleted > 3 {
		t.Fatalf("the accepted guard deleted %d files, the user reviewed 3", st.Result.Counts.Deleted)
	}
}
