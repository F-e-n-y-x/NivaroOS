package jobs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
)

// The omitted_add line of a capped plan list counts, but is no item.
func TestPreviewPageCountsOmittedAdds(t *testing.T) {
	p := filepath.Join(t.TempDir(), "plan.jsonl")
	body := `{"op":"add","path":"a","size":3}
{"op":"delete","path":"b","size":1}
{"op":"omitted_add","path":"","size":30,"count":10}
`
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	page, err := readPreviewPage(p, "", "", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	if page.Counts.Add != 11 || page.Counts.BytesAdd != 33 || page.Counts.Delete != 1 || page.Total != 2 || len(page.Items) != 2 {
		t.Errorf("page %+v", page)
	}
}

// Past maxRunLogBytes engine lines are dropped after one note; the job
// side's own lines still go in.
func TestRunLogCapsEngineLines(t *testing.T) {
	lg, err := OpenRunLog(filepath.Join(t.TempDir(), "run.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer lg.Close()
	lg.size = maxRunLogBytes - 10 // as if it were nearly full
	lg.Write(engine.LogLine{Lvl: engine.LogInfo, Code: "copied", MsgKey: "backup.log.copied", Args: map[string]interface{}{"path": "x"}})
	lg.Write(engine.LogLine{Lvl: engine.LogInfo, Code: "copied", MsgKey: "backup.log.copied", Args: map[string]interface{}{"path": "y"}})
	lg.Info("backup.log.finished", map[string]interface{}{"status_key": "backup.status.success"})
	lg.Close()
	raw, _ := os.ReadFile(lg.Path())
	got := string(raw)
	if strings.Contains(got, `"path":"x"`) || strings.Count(got, "later engine lines are left out") != 1 || !strings.Contains(got, "backup.log.finished") {
		t.Errorf("log:\n%s", got)
	}
}

// A run doesn't start when the state folder's disk is nearly full.
func TestRunFailsWhenTheSystemDiskIsFull(t *testing.T) {
	h := newHarness(t, true, func(c *Config, h *harness) { c.MinStateFree = 1 << 62 })
	job := h.createJob(sampleJob("Docs"))
	r := h.waitStatus(h.run(job, KindBackup), StatusFailed)
	if ErrorCode(r.ErrorCode) != ErrSystemDiskFull || len(h.eng.CallsTo("StartJob")) != 0 {
		t.Errorf("run %+v", r)
	}
}
