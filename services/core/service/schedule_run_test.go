package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/robfig/cron/v3"
)

func newTestScheduler(t *testing.T) *scheduleService {
	t.Helper()
	logger.LogInitConsoleOnly()
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	return &scheduleService{
		tasks:    map[string]*ScheduleTask{},
		cron:     cron.New(cron.WithParser(parser)),
		dataFile: filepath.Join(t.TempDir(), "schedules.json"),
		parser:   parser,
	}
}

func (s *scheduleService) addCommand(id, cmd string) {
	s.mu.Lock()
	s.tasks[id] = &ScheduleTask{ID: id, Name: id, Type: "command", Command: cmd}
	s.mu.Unlock()
}

// "Run now" while the timer run was still going started the task twice
// (two rsync --delete into one destination).
func TestATaskNeverRunsTwiceAtOnce(t *testing.T) {
	s := newTestScheduler(t)
	marker := filepath.Join(t.TempDir(), "runs")
	s.addCommand("t1", "echo run >> "+marker+"; sleep 0.4")
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); s.executeTask("t1") }()
	}
	wg.Wait()
	raw, _ := os.ReadFile(marker)
	if n := strings.Count(string(raw), "run"); n != 1 {
		t.Fatalf("task ran %d times concurrently, want 1", n)
	}
}

// Backups were killed after 10 minutes like any other task.
func TestTransfersHaveNoShortTimeout(t *testing.T) {
	for _, typ := range []string{"backup", "sync"} {
		if d := taskTimeout(&ScheduleTask{Type: typ}); d < 12*time.Hour {
			t.Errorf("%s timeout = %v", typ, d)
		}
	}
	if d := taskTimeout(&ScheduleTask{Type: "vm"}); d > 30*time.Minute {
		t.Errorf("vm timeout = %v", d)
	}
}

// A task that was running when the service stopped stayed "running"
// forever.
func TestARunInterruptedByARestartIsShownAsInterrupted(t *testing.T) {
	s := newTestScheduler(t)
	list := []ScheduleTask{{ID: "t1", Name: "t1", Type: "command", Command: "true", LastStatus: "running"}}
	raw, _ := json.Marshal(list)
	os.WriteFile(s.dataFile, raw, 0o644)
	s.load()
	if got := s.tasks["t1"].LastStatus; got != "interrupted" {
		t.Fatalf("status after restart = %q", got)
	}
}

// The whole output was stored and returned with every list request.
func TestTaskOutputIsCapped(t *testing.T) {
	s := newTestScheduler(t)
	s.addCommand("big", "head -c 500000 /dev/zero | tr '\\0' 'x'; echo; echo THE-END")
	s.executeTask("big")
	out := s.tasks["big"].LastOutput
	if len(out) > maxTaskOutput+200 {
		t.Fatalf("output %d bytes, cap %d", len(out), maxTaskOutput)
	}
	if !strings.Contains(out, "THE-END") {
		t.Fatal("the end of the output (where errors are) was dropped")
	}
}

// Editing a backup task dropped source/destination/direction/mode/args and
// still said "updated".
func TestEditingABackupTaskKeepsEveryField(t *testing.T) {
	s := newTestScheduler(t)
	s.mu.Lock()
	s.tasks["b"] = &ScheduleTask{ID: "b", Name: "old", Type: "backup", Cron: "0 2 * * *", SourcePath: "/DATA/a", DestPath: "/DATA/b", SyncMode: "copy", Direction: "local_to_local"}
	s.mu.Unlock()
	got, err := s.UpdateTask("b", ScheduleTask{Name: "Photos", Type: "backup", Cron: "0 3 * * *", SourcePath: "/DATA/Gallery", DestPath: "gdrive:Backup", SyncMode: "sync", Direction: "local_to_cloud", ExtraArgs: "--bwlimit 5M"})
	if err != nil {
		t.Fatal(err)
	}
	if got.SourcePath != "/DATA/Gallery" || got.DestPath != "gdrive:Backup" || got.SyncMode != "sync" || got.Direction != "local_to_cloud" || got.ExtraArgs != "--bwlimit 5M" {
		t.Fatalf("fields not saved: %+v", got)
	}
}
