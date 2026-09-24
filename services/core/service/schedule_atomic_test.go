package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func readTasks(t *testing.T, path string) []ScheduleTask {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var list []ScheduleTask
	if err := json.Unmarshal(raw, &list); err != nil {
		t.Fatalf("%s is not valid JSON: %v", path, err)
	}
	return list
}

// A crash between writing and renaming used to be a crash between
// truncating and writing: schedules.json ended up empty.
func TestSaveIsAtomic(t *testing.T) {
	s := newTestScheduler(t)
	if _, err := s.CreateTask(ScheduleTask{Name: "one", Type: "command", Command: "true", Cron: "0 1 * * *"}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(s.dataFile)

	crash := errors.New("killed")
	beforeAtomicRename = func(string) error { return crash }
	defer func() { beforeAtomicRename = func(string) error { return nil } }()
	if _, err := s.CreateTask(ScheduleTask{Name: "two", Type: "command", Command: "true", Cron: "0 2 * * *"}); !errors.Is(err, crash) {
		t.Fatalf("save error = %v, want the simulated crash", err)
	}
	after, _ := os.ReadFile(s.dataFile)
	if string(after) != string(before) {
		t.Fatalf("the file changed although the write never completed:\n%s", after)
	}
	entries, _ := os.ReadDir(filepath.Dir(s.dataFile))
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}

	beforeAtomicRename = func(string) error { return nil }
	if _, err := s.CreateTask(ScheduleTask{Name: "three", Type: "command", Command: "true", Cron: "0 3 * * *"}); err != nil {
		t.Fatal(err)
	}
	if n := len(readTasks(t, s.dataFile)); n != 3 {
		// "two" is in memory: the failed save only lost the write.
		t.Fatalf("%d tasks on disk, want 3", n)
	}
	if fi, _ := os.Stat(s.dataFile); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v, want 0600 (commands may hold secrets)", fi.Mode().Perm())
	}
}

// A file that didn't parse was ignored and the next save replaced every
// task with nothing.
func TestCorruptFileIsKeptAndSavingRefused(t *testing.T) {
	s := newTestScheduler(t)
	if err := os.WriteFile(s.dataFile, []byte(`[{"id":"t1","name":"half`), 0o644); err != nil {
		t.Fatal(err)
	}
	s.load()
	matches, _ := filepath.Glob(s.dataFile + ".corrupt-*")
	if len(matches) != 1 {
		t.Fatalf("corrupt copy: %v", matches)
	}
	if raw, _ := os.ReadFile(matches[0]); string(raw) != `[{"id":"t1","name":"half` {
		t.Fatalf("the kept copy differs: %q", raw)
	}
	if _, err := s.CreateTask(ScheduleTask{Name: "x", Type: "command", Command: "true", Cron: "@daily"}); err == nil {
		t.Fatal("saving was allowed after a failed load")
	}
	if _, err := os.Stat(s.dataFile); !os.IsNotExist(err) {
		t.Fatalf("a new schedules.json was written over the damaged state: %v", err)
	}
}

func migratedTask(s *scheduleService) *ScheduleTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := &ScheduleTask{ID: "b1", Name: "Photos", Type: "backup", Cron: "* * * * *", Enabled: true,
		SourcePath: "/nonexistent/src", DestPath: "/nonexistent/dst", Action: "copy", MigratedTo: "backup"}
	s.tasks[t.ID] = t
	s.scheduleLocked(t)
	return t
}

// Backup & Sync imports a task and marks it; core must never run it too.
func TestMigratedTaskNeverRuns(t *testing.T) {
	s := newTestScheduler(t)
	task := migratedTask(s)
	if task.entryID != 0 || task.NextRun != "" {
		t.Fatalf("a moved task got a timer: entry %d next %q", task.entryID, task.NextRun)
	}
	s.executeTask("b1")
	if task.LastRun != "" || task.LastStatus != "" {
		t.Fatalf("the executor ran a moved task: %+v", task)
	}
	if _, err := s.RunTaskNow("b1"); !errors.Is(err, ErrTaskMigrated) {
		t.Fatalf("Run now = %v, want ErrTaskMigrated", err)
	}
	// Switching it on in Scheduled Tasks doesn't bring the timer back.
	if _, err := s.ToggleTask("b1", true); err != nil {
		t.Fatal(err)
	}
	if task.entryID != 0 {
		t.Fatal("toggling a moved task on scheduled it")
	}
}

func TestMigratedMarkerSurvivesEditsAndIsReleasedExplicitly(t *testing.T) {
	s := newTestScheduler(t)
	migratedTask(s)
	edit := ScheduleTask{Name: "Photos (renamed)", Type: "backup", Cron: "0 4 * * *", Enabled: true,
		SourcePath: "/nonexistent/src", DestPath: "/nonexistent/dst", Action: "copy"}
	got, err := s.UpdateTask("b1", edit)
	if err != nil {
		t.Fatal(err)
	}
	if got.MigratedTo != "backup" || s.tasks["b1"].entryID != 0 {
		t.Fatalf("an editor save dropped the marker: %+v", got)
	}

	// The module hands it back.
	edit.SetMigratedTo("")
	got, err = s.UpdateTask("b1", edit)
	if err != nil {
		t.Fatal(err)
	}
	if got.MigratedTo != "" || s.tasks["b1"].entryID == 0 {
		t.Fatalf("released task isn't scheduled again: %+v", got)
	}

	// Stored and sent even when empty: Backup & Sync reads the key's
	// presence as "this core keeps the marker".
	raw, _ := os.ReadFile(s.dataFile)
	if !strings.Contains(string(raw), `"migrated_to": ""`) {
		t.Fatalf("migrated_to missing from the saved file:\n%s", raw)
	}
	out, _ := json.Marshal(s.GetTasks())
	if !strings.Contains(string(out), `"migrated_to":""`) {
		t.Fatalf("migrated_to missing from the API: %s", out)
	}

	// A create never carries a marker.
	c, err := s.CreateTask(ScheduleTask{Name: "n", Type: "command", Command: "true", Cron: "@daily", MigratedTo: "backup"})
	if err != nil || c.MigratedTo != "" {
		t.Fatalf("create kept a marker: %+v %v", c, err)
	}
}

// The marker and every other field survive a restart.
func TestMigratedMarkerSurvivesReload(t *testing.T) {
	s := newTestScheduler(t)
	migratedTask(s)
	s.mu.Lock()
	if err := s.saveLocked(); err != nil {
		t.Fatal(err)
	}
	s.mu.Unlock()
	s2 := newTestScheduler(t)
	s2.dataFile = s.dataFile
	s2.load()
	if tk := s2.tasks["b1"]; tk == nil || tk.MigratedTo != "backup" || tk.entryID != 0 {
		t.Fatalf("after reload: %+v", tk)
	}
}

// The archive command was a bash -c string with the paths pasted in: a
// folder name could run commands.
func TestArchivePathsCantInjectCommands(t *testing.T) {
	root := t.TempDir()
	pwned := filepath.Join(root, "pwned")
	src := filepath.Join(root, `docs"; touch `+pwned+`; echo "`)
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(root, "out $(touch "+pwned+")")
	now := time.Date(2026, 9, 24, 3, 0, 0, 0, time.UTC)
	out, err := runArchive(context.Background(), src, dest, now)
	if err != nil {
		t.Fatalf("archive failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(pwned); err == nil {
		t.Fatal("a path ran as a shell command")
	}
	if _, err := os.Stat(filepath.Join(dest, "backup_20260924_030000.tar.gz")); err != nil {
		t.Fatalf("archive not written: %v", err)
	}

	// A folder called like an option stays a folder.
	dash := filepath.Join(root, "-v")
	if err := os.MkdirAll(dash, 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := runArchive(context.Background(), dash, filepath.Join(root, "out2"), now); err != nil {
		t.Fatalf("archive of %q failed: %v\n%s", dash, err, out)
	}
	if _, err := runArchive(context.Background(), "relative/src", dest, now); err == nil {
		t.Fatal("relative paths accepted")
	}
}

func TestTransferPathsCantBeOptions(t *testing.T) {
	s := newTestScheduler(t)
	out, err := s.runTaskAction(&ScheduleTask{Type: "backup", Action: "copy", SourcePath: "--config=/tmp/x", DestPath: "/DATA/b"})
	if err == nil {
		t.Fatalf("a source starting with '-' was passed on: %s", out)
	}
}

// Custom cron text is stored as given, through edits and restarts (the
// editor used to replace it with the nearest preset).
func TestCustomCronIsKeptVerbatim(t *testing.T) {
	s := newTestScheduler(t)
	c, err := s.CreateTask(ScheduleTask{Name: "c", Type: "command", Command: "true", Cron: "15 4 */2 * *"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateTask(c.ID, ScheduleTask{Name: "c2", Type: "command", Command: "true", Cron: "15 4 */2 * *"}); err != nil {
		t.Fatal(err)
	}
	if got := readTasks(t, s.dataFile)[0].Cron; got != "15 4 */2 * *" {
		t.Fatalf("cron on disk = %q", got)
	}
}
