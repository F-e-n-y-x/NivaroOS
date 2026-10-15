//go:build e2e

package e2e

import (
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/jobs"
)

func TestCopyLocalAndCloud(t *testing.T) {
	in := newInstance(t)
	writeFiles(t, in.path("src"), 50, 10<<10)
	want := tree(t, in.path("src"))
	j := in.createJob(job("local", jobs.TypeCopy, in.endpoint(in.path("src")), in.endpoint(in.path("dst"))))
	r := in.waitRun(in.runJob(j.ID), time.Minute)
	if r.Status != jobs.StatusSuccess {
		t.Fatalf("local: %s %s %+v", r.Status, r.ErrorCode, r.Summary)
	}
	sameTree(t, want, tree(t, in.path("dst")))

	c := in.createJob(job("cloud", jobs.TypeCopy, in.endpoint(in.path("src")), cloud("cloud/dst")))
	r = in.waitRun(in.runJob(c.ID), time.Minute)
	if r.Status != jobs.StatusSuccess {
		t.Fatalf("cloud: %s %s %+v", r.Status, r.ErrorCode, r.Summary)
	}
	sameTree(t, want, tree(t, in.path("cloud/dst")))
}
