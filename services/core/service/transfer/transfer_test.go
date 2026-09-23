package transfer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	m := NewManager(Options{StatePath: filepath.Join(t.TempDir(), "jobs.json")})
	t.Cleanup(m.Close)
	return m
}

// waitDone polls the public snapshot until the job leaves the active states.
func waitDone(t *testing.T, m *Manager, id string) Job {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		j, ok := m.Get(id)
		if !ok {
			t.Fatalf("job %s disappeared", id)
		}
		if j.State.Terminal() {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	j, _ := m.Get(id)
	t.Fatalf("job %s still %s", id, j.State)
	return Job{}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestCopySingleFile(t *testing.T) {
	m := newTestManager(t)
	src := filepath.Join(t.TempDir(), "report.pdf")
	writeFile(t, src, "PDF-CONTENT")
	dest := t.TempDir()

	j, err := m.Submit(Spec{Kind: KindCopy, Sources: []string{src}, Dest: dest})
	if err != nil {
		t.Fatal(err)
	}
	j = waitDone(t, m, j.ID)

	if j.State != StateDone {
		t.Fatalf("state = %s, failures = %+v", j.State, j.Failures)
	}
	if got := readFile(t, filepath.Join(dest, "report.pdf")); got != "PDF-CONTENT" {
		t.Fatalf("copied content = %q", got)
	}
	if j.FilesDone != 1 || j.BytesDone != int64(len("PDF-CONTENT")) {
		t.Fatalf("counters: files=%d bytes=%d", j.FilesDone, j.BytesDone)
	}
	if !bytes.Equal([]byte(readFile(t, src)), []byte("PDF-CONTENT")) {
		t.Fatal("source changed")
	}
}

// shmDir is on a different filesystem (tmpfs /dev/shm) from t.TempDir()
// (/tmp) on this box - a real cross-drive move, not a simulated one.
func shmDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/dev/shm", "nvxfer-test-")
	if err != nil {
		t.Skip("no /dev/shm")
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

// makeAlbum creates album/{a,b,c}.jpg + album/sub/d.jpg under root.
func makeAlbum(t *testing.T, root string) string {
	src := filepath.Join(root, "album")
	for _, n := range []string{"a.jpg", "b.jpg", "c.jpg", "sub/d.jpg"} {
		writeFile(t, filepath.Join(src, n), "data-"+n)
	}
	return src
}

// blockAt makes a destination path unwritable as a file by putting a
// non-empty directory there (works even as root, unlike chmod).
func blockAt(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(path, "blocker"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func assertNoTempFiles(t *testing.T, root string) {
	t.Helper()
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err == nil && strings.Contains(filepath.Base(p), ".nvtmp-") {
			t.Errorf("leftover temp file %s", p)
		}
		return nil
	})
}

func TestCopyFolderReportsTheFileThatFailed(t *testing.T) {
	m := newTestManager(t)
	src := makeAlbum(t, t.TempDir())
	dest := t.TempDir()
	blockAt(t, filepath.Join(dest, "album", "b.jpg"))

	j, _ := m.Submit(Spec{Kind: KindCopy, Sources: []string{src}, Dest: dest})
	j = waitDone(t, m, j.ID)

	if j.State != StateDoneWithErrors {
		t.Fatalf("state = %s, want done_with_errors", j.State)
	}
	if j.FilesFailed != 1 || len(j.Failures) != 1 || j.Failures[0].Path != filepath.Join(src, "b.jpg") {
		t.Fatalf("failures = %+v", j.Failures)
	}
	if j.FilesTotal != 4 || j.FilesDone != 3 {
		t.Fatalf("files total=%d done=%d", j.FilesTotal, j.FilesDone)
	}
	for _, n := range []string{"a.jpg", "c.jpg", "sub/d.jpg"} {
		if got := readFile(t, filepath.Join(dest, "album", n)); got != "data-"+n {
			t.Errorf("%s = %q", n, got)
		}
	}
	assertNoTempFiles(t, dest)
}

func TestCrossDriveMoveNeverDeletesAFileThatDidNotArrive(t *testing.T) {
	m := newTestManager(t)
	src := makeAlbum(t, t.TempDir())
	dest := shmDir(t)
	blockAt(t, filepath.Join(dest, "album", "b.jpg"))

	j, _ := m.Submit(Spec{Kind: KindMove, Sources: []string{src}, Dest: dest})
	j = waitDone(t, m, j.ID)

	if j.State != StateDoneWithErrors {
		t.Fatalf("state = %s", j.State)
	}
	// The file that failed is still at the source...
	if got := readFile(t, filepath.Join(src, "b.jpg")); got != "data-b.jpg" {
		t.Fatalf("source b.jpg lost: %q", got)
	}
	// ...everything that arrived was removed from the source...
	for _, n := range []string{"a.jpg", "c.jpg", "sub/d.jpg"} {
		if _, err := os.Stat(filepath.Join(src, n)); !os.IsNotExist(err) {
			t.Errorf("source %s should be gone after a verified move", n)
		}
		if got := readFile(t, filepath.Join(dest, "album", n)); got != "data-"+n {
			t.Errorf("dest %s = %q", n, got)
		}
	}
	// ...and emptied sub-folders are cleaned up.
	if _, err := os.Stat(filepath.Join(src, "sub")); !os.IsNotExist(err) {
		t.Error("empty source sub-folder left behind")
	}
	assertNoTempFiles(t, dest)
}

func TestSameDriveMoveIsARename(t *testing.T) {
	m := newTestManager(t)
	root := t.TempDir()
	src := makeAlbum(t, root)
	dest := filepath.Join(root, "moved")
	os.MkdirAll(dest, 0o755)
	inode := func(p string) uint64 {
		var st syscall.Stat_t
		syscall.Stat(p, &st)
		return st.Ino
	}
	before := inode(filepath.Join(src, "a.jpg"))

	j, _ := m.Submit(Spec{Kind: KindMove, Sources: []string{src}, Dest: dest})
	j = waitDone(t, m, j.ID)
	if j.State != StateDone {
		t.Fatalf("state = %s %+v", j.State, j.Failures)
	}
	if inode(filepath.Join(dest, "album", "a.jpg")) != before {
		t.Error("same-drive move copied data instead of renaming")
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("source folder still exists")
	}
	if j.FilesDone != 4 {
		t.Errorf("files done = %d", j.FilesDone)
	}
}

func TestPasteIntoSameFolderKeepsBoth(t *testing.T) {
	m := newTestManager(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "notes.txt")
	writeFile(t, src, "v1")
	j, _ := m.Submit(Spec{Kind: KindCopy, Sources: []string{src}, Dest: dir})
	j = waitDone(t, m, j.ID)
	if j.State != StateDone || readFile(t, filepath.Join(dir, "notes (2).txt")) != "v1" {
		t.Fatalf("state=%s", j.State)
	}
}

func TestConflictPolicies(t *testing.T) {
	m := newTestManager(t)
	srcDir, dest := t.TempDir(), t.TempDir()
	src := filepath.Join(srcDir, "a.txt")
	writeFile(t, src, "new")
	writeFile(t, filepath.Join(dest, "a.txt"), "old")

	j, _ := m.Submit(Spec{Kind: KindCopy, Sources: []string{src}, Dest: dest, Conflict: ConflictSkip})
	if j = waitDone(t, m, j.ID); readFile(t, filepath.Join(dest, "a.txt")) != "old" {
		t.Fatal("skip overwrote the existing file")
	}
	j, _ = m.Submit(Spec{Kind: KindCopy, Sources: []string{src}, Dest: dest, Conflict: ConflictRename})
	if j = waitDone(t, m, j.ID); readFile(t, filepath.Join(dest, "a (2).txt")) != "new" {
		t.Fatal("rename didn't keep both")
	}
	j, _ = m.Submit(Spec{Kind: KindCopy, Sources: []string{src}, Dest: dest, Conflict: ConflictOverwrite})
	if j = waitDone(t, m, j.ID); readFile(t, filepath.Join(dest, "a.txt")) != "new" {
		t.Fatal("overwrite didn't replace")
	}
}

func TestCopyFolderIntoItselfIsRefused(t *testing.T) {
	m := newTestManager(t)
	src := makeAlbum(t, t.TempDir())
	j, _ := m.Submit(Spec{Kind: KindCopy, Sources: []string{src}, Dest: filepath.Join(src, "sub")})
	j = waitDone(t, m, j.ID)
	if j.FilesFailed != 1 || !strings.Contains(j.Failures[0].Error, "into itself") {
		t.Fatalf("got %s %+v", j.State, j.Failures)
	}
}

func TestManyPastesAtOnceAllComplete(t *testing.T) {
	// The old queue could drop a job submitted while another finished.
	m := newTestManager(t)
	dest := t.TempDir()
	var ids []string
	for i := 0; i < 12; i++ {
		src := filepath.Join(t.TempDir(), fmt.Sprintf("f%02d.bin", i))
		writeFile(t, src, strings.Repeat("x", 64<<10))
		j, err := m.Submit(Spec{Kind: KindCopy, Sources: []string{src}, Dest: dest})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, j.ID)
	}
	for i, id := range ids {
		if j := waitDone(t, m, id); j.State != StateDone {
			t.Fatalf("job %d: %s", i, j.State)
		}
		if _, err := os.Stat(filepath.Join(dest, fmt.Sprintf("f%02d.bin", i))); err != nil {
			t.Fatalf("file %d missing", i)
		}
	}
}

func TestCancelLeavesNoPartialFile(t *testing.T) {
	m := newTestManager(t)
	src := filepath.Join(t.TempDir(), "big.bin")
	f, _ := os.Create(src)
	f.Truncate(1 << 30) // 1 GiB sparse: takes a while to copy
	f.Close()
	dest := t.TempDir()
	j, _ := m.Submit(Spec{Kind: KindCopy, Sources: []string{src}, Dest: dest})
	for {
		cur, _ := m.Get(j.ID)
		if cur.BytesDone > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	m.Cancel(j.ID)
	j = waitDone(t, m, j.ID)
	if j.State != StateCancelled {
		t.Fatalf("state = %s", j.State)
	}
	if _, err := os.Stat(filepath.Join(dest, "big.bin")); !os.IsNotExist(err) {
		t.Error("a partial file was left under the real name")
	}
	assertNoTempFiles(t, dest)
}

func TestDeleteFolderAndReportFailures(t *testing.T) {
	m := newTestManager(t)
	root := t.TempDir()
	src := makeAlbum(t, root)
	j, _ := m.Submit(Spec{Kind: KindDelete, Sources: []string{src, filepath.Join(root, "missing")}})
	j = waitDone(t, m, j.ID)
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatal("folder not deleted")
	}
	if j.State != StateDoneWithErrors || j.FilesFailed != 1 || j.FilesDone != 4 {
		t.Fatalf("state=%s done=%d failed=%d %+v", j.State, j.FilesDone, j.FilesFailed, j.Failures)
	}
}

func TestUnfinishedJobsAreInterruptedAfterRestart(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "jobs.json")
	m := NewManager(Options{StatePath: statePath})
	src := filepath.Join(t.TempDir(), "big.bin")
	f, _ := os.Create(src)
	f.Truncate(1 << 30)
	f.Close()
	j, _ := m.Submit(Spec{Kind: KindCopy, Sources: []string{src}, Dest: t.TempDir()})
	for {
		cur, _ := m.Get(j.ID)
		if cur.BytesDone > 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	m.Close() // service stops mid-copy

	m2 := NewManager(Options{StatePath: statePath})
	defer m2.Close()
	got, ok := m2.Get(j.ID)
	if !ok || got.State != StateInterrupted {
		t.Fatalf("after restart: ok=%v state=%s", ok, got.State)
	}
}

func TestRetryFinishesWhatFailed(t *testing.T) {
	m := newTestManager(t)
	src := makeAlbum(t, t.TempDir())
	dest := t.TempDir()
	block := filepath.Join(dest, "album", "b.jpg")
	blockAt(t, block)
	j, _ := m.Submit(Spec{Kind: KindCopy, Sources: []string{src}, Dest: dest})
	j = waitDone(t, m, j.ID)
	if j.State != StateDoneWithErrors {
		t.Fatalf("setup: %s", j.State)
	}
	os.RemoveAll(block) // user fixes the problem

	r, err := m.Retry(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	r = waitDone(t, m, r.ID)
	if r.State != StateDone {
		t.Fatalf("retry state = %s %+v", r.State, r.Failures)
	}
	if readFile(t, filepath.Join(dest, "album", "b.jpg")) != "data-b.jpg" {
		t.Fatal("retry didn't copy the failed file")
	}
	if r.FilesSkipped != 3 {
		t.Errorf("retry should skip the 3 files already there, skipped %d", r.FilesSkipped)
	}
	if _, err := os.Stat(filepath.Join(dest, "album (2)")); !os.IsNotExist(err) {
		t.Error("retry created a duplicate folder instead of finishing the first")
	}
	if _, ok := m.Get(j.ID); ok {
		t.Error("the retried job should be replaced by its retry")
	}
}

func TestChangesArePublishedWithFinalState(t *testing.T) {
	var mu sync.Mutex
	var last []Job
	m := NewManager(Options{OnChange: func(js []Job) { mu.Lock(); last = js; mu.Unlock() }})
	defer m.Close()
	src := filepath.Join(t.TempDir(), "x.txt")
	writeFile(t, src, "x")
	j, _ := m.Submit(Spec{Kind: KindCopy, Sources: []string{src}, Dest: t.TempDir()})
	waitDone(t, m, j.ID)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		ok := len(last) == 1 && last[0].State == StateDone
		mu.Unlock()
		if ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("final state was never published")
}

func TestAfterWriteFailureIsReported(t *testing.T) {
	var sawSyncing bool
	var m *Manager
	m = NewManager(Options{AfterWrite: func(ctx context.Context, dest string, progress func(string)) error {
		progress("Uploading 1 file(s) to gdrive")
		for _, j := range m.List() {
			if j.State == StateSyncing {
				sawSyncing = true
			}
		}
		return errors.New("1 file(s) failed to upload to gdrive")
	}})
	defer m.Close()
	src := filepath.Join(t.TempDir(), "x.txt")
	writeFile(t, src, "x")
	j, _ := m.Submit(Spec{Kind: KindCopy, Sources: []string{src}, Dest: t.TempDir()})
	j = waitDone(t, m, j.ID)
	if j.State != StateDoneWithErrors || !strings.Contains(j.Failures[0].Error, "failed to upload") {
		t.Fatalf("cloud upload failure not reported: %s %+v", j.State, j.Failures)
	}
	if !sawSyncing {
		t.Error("job never showed the syncing state")
	}
}
