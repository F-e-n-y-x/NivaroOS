package trash

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Every test uses a temp dir as its own "filesystem root", so the trash
// lives next to the data exactly as it does on a real drive.
func newTestBin(t *testing.T) (*Bin, string) {
	t.Helper()
	root := t.TempDir()
	b := New(Options{
		IndexPath: filepath.Join(t.TempDir(), "roots.json"),
		RootFor:   func(string) (string, error) { return root, nil },
	})
	return b, root
}

func write(t *testing.T, p, s string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

func TestTrashAndRestoreFileAndFolder(t *testing.T) {
	b, root := newTestBin(t)
	f := filepath.Join(root, "docs", "cv.pdf")
	d := filepath.Join(root, "photos")
	write(t, f, "CV")
	write(t, filepath.Join(d, "a.jpg"), "A")

	items, err := b.Trash([]string{f, d})
	if err != nil || len(items) != 2 {
		t.Fatalf("trash: %v %v", items, err)
	}
	for _, p := range []string{f, d} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Fatalf("%s still there", p)
		}
	}
	list := b.List()
	if len(list) != 2 || list[0].OriginalPath == "" {
		t.Fatalf("list: %+v", list)
	}
	byPath := map[string]Item{}
	for _, it := range list {
		byPath[it.OriginalPath] = it
	}
	if !byPath[d].IsDir || byPath[f].Size != 2 {
		t.Fatalf("metadata: %+v", byPath)
	}

	res := b.Restore([]string{byPath[f].ID, byPath[d].ID})
	if len(res.Failed) != 0 {
		t.Fatalf("restore failed: %+v", res.Failed)
	}
	if read(t, f) != "CV" || read(t, filepath.Join(d, "a.jpg")) != "A" {
		t.Fatal("restored content wrong")
	}
	if len(b.List()) != 0 {
		t.Fatal("trash not empty after restore")
	}
}

func TestTrashIsARenameOnTheSameFilesystem(t *testing.T) {
	b, root := newTestBin(t)
	f := filepath.Join(root, "big.bin")
	write(t, f, "x")
	var before syscall.Stat_t
	syscall.Stat(f, &before)
	items, _ := b.Trash([]string{f})
	var after syscall.Stat_t
	syscall.Stat(b.pathOf(items[0]), &after)
	if before.Ino != after.Ino {
		t.Fatal("trash copied the data instead of renaming")
	}
}

func TestRestoreNeverOverwrites(t *testing.T) {
	b, root := newTestBin(t)
	f := filepath.Join(root, "notes.txt")
	write(t, f, "old")
	items, _ := b.Trash([]string{f})
	write(t, f, "new file with the same name")
	res := b.Restore([]string{items[0].ID})
	if len(res.Failed) != 0 {
		t.Fatal(res.Failed)
	}
	if read(t, f) != "new file with the same name" {
		t.Fatal("restore overwrote a file")
	}
	if read(t, filepath.Join(root, "notes (restored).txt")) != "old" {
		t.Fatal("restored copy not found under a new name")
	}
	if res.Restored[0].Path != filepath.Join(root, "notes (restored).txt") {
		t.Fatalf("restored path %q", res.Restored[0].Path)
	}
}

func TestRestoreRecreatesMissingFolders(t *testing.T) {
	b, root := newTestBin(t)
	f := filepath.Join(root, "a", "b", "c.txt")
	write(t, f, "deep")
	items, _ := b.Trash([]string{f})
	os.RemoveAll(filepath.Join(root, "a"))
	if res := b.Restore([]string{items[0].ID}); len(res.Failed) != 0 {
		t.Fatal(res.Failed)
	}
	if read(t, f) != "deep" {
		t.Fatal("not restored")
	}
}

func TestDeleteForeverAndEmpty(t *testing.T) {
	b, root := newTestBin(t)
	write(t, filepath.Join(root, "1.txt"), "1")
	write(t, filepath.Join(root, "2.txt"), "2")
	items, _ := b.Trash([]string{filepath.Join(root, "1.txt"), filepath.Join(root, "2.txt")})
	if failed := b.Delete([]string{items[0].ID}); len(failed) != 0 {
		t.Fatal(failed)
	}
	if len(b.List()) != 1 {
		t.Fatal("delete forever didn't remove the item")
	}
	b.Empty()
	if len(b.List()) != 0 {
		t.Fatal("empty left items")
	}
	if _, err := os.Stat(b.pathOf(items[1])); !os.IsNotExist(err) {
		t.Fatal("data still on disk after empty")
	}
}

func TestPurgeRemovesOldItems(t *testing.T) {
	b, root := newTestBin(t)
	write(t, filepath.Join(root, "old.txt"), "o")
	write(t, filepath.Join(root, "new.txt"), "n")
	items, _ := b.Trash([]string{filepath.Join(root, "old.txt"), filepath.Join(root, "new.txt")})
	b.backdate(items[0].ID, 31*24*time.Hour)
	if n := b.Purge(30 * 24 * time.Hour); n != 1 {
		t.Fatalf("purged %d", n)
	}
	if list := b.List(); len(list) != 1 || list[0].Name != "new.txt" {
		t.Fatalf("left: %+v", list)
	}
}

func TestCannotTrashTheTrashItself(t *testing.T) {
	b, root := newTestBin(t)
	write(t, filepath.Join(root, "x"), "x")
	b.Trash([]string{filepath.Join(root, "x")})
	if _, err := b.Trash([]string{filepath.Join(root, trashDirName)}); err == nil {
		t.Fatal("trashed the trash folder")
	}
}

func TestListSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	idx := filepath.Join(t.TempDir(), "roots.json")
	opts := Options{IndexPath: idx, RootFor: func(string) (string, error) { return root, nil }}
	write(t, filepath.Join(root, "keep.txt"), "k")
	New(opts).Trash([]string{filepath.Join(root, "keep.txt")})
	if l := New(opts).List(); len(l) != 1 || l[0].Name != "keep.txt" {
		t.Fatalf("after restart: %+v", l)
	}
}
