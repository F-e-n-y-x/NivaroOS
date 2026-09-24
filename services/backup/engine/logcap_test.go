package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Per-file lines past the cap are left out: the head, one lines_omitted
// line with the count, then the last lines in order. Other lines are
// always kept.
func TestLogSpoolCapsPerFileLines(t *testing.T) {
	sp, err := newLogSpool(t.TempDir(), 1, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	defer sp.remove()
	total := logKeepHead + logKeepTail + 1234
	for i := 0; i < total; i++ {
		sp.append(LogLine{Lvl: LogInfo, Code: "copied", MsgKey: "backup.log.copied", Args: map[string]interface{}{"path": fmt.Sprint(i)}})
		if i == logKeepHead+10 {
			sp.append(LogLine{Lvl: LogInfo, Code: "phase", MsgKey: "backup.log.phase"})
		}
	}
	sp.finish()
	lines := sp.lines()
	if want := logKeepHead + 1 + 1 + logKeepTail; len(lines) != want {
		t.Fatalf("%d lines, want %d", len(lines), want)
	}
	if lines[logKeepHead].Code != "phase" {
		t.Errorf("a non-file line was dropped or moved: %+v", lines[logKeepHead])
	}
	om := lines[logKeepHead+1]
	if om.Code != "lines_omitted" || om.Args["count"] != float64(1234) {
		t.Errorf("omitted line %+v", om)
	}
	if first, last := lines[logKeepHead+2].Args["path"], lines[len(lines)-1].Args["path"]; first != fmt.Sprint(total-logKeepTail) || last != fmt.Sprint(total-1) {
		t.Errorf("tail runs %v .. %v", first, last)
	}
}

// A real run's plan list holds at most planListAdds adds; the rest are
// counted on one omitted_add line. Deletions are always listed.
func TestRealRunPlanListCapsAdds(t *testing.T) {
	_, e, src, dst := twoVolumes(t)
	root := filepath.Join(src.MountPoint, "data")
	files := map[string]string{}
	for i := 0; i < 12; i++ {
		files[filepathJoin("f", i)] = "abc"
	}
	writeTree(t, root, files)
	saved := planListAdds
	planListAdds = 5
	t.Cleanup(func() { planListAdds = saved })
	req := baseReq(OpSync, ep(src, "data"), ep(dst, "m"))
	req.PreviewFile = filepath.Join(t.TempDir(), "plan.jsonl")
	expectState(t, runJob(t, e, req), JobDone, "")
	raw, err := os.ReadFile(req.PreviewFile)
	if err != nil {
		t.Fatal(err)
	}
	var adds int
	var omitted PreviewItem
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var it PreviewItem
		if err := json.Unmarshal([]byte(line), &it); err != nil {
			t.Fatal(err)
		}
		switch it.Op {
		case "add":
			adds++
		case PreviewOmittedAdd:
			omitted = it
		}
	}
	if adds != 5 || omitted.Count != 7 || omitted.Size != 21 {
		t.Errorf("listed %d adds, omitted %+v; want 5 and 7 (21 bytes)", adds, omitted)
	}
}
