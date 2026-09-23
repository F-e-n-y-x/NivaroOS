package v1

import (
	"os"
	"path/filepath"
	"testing"
)

func writeBackup(t *testing.T, dir, name string) {
	t.Helper()
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, name), []byte("photo"), 0o644)
}

// Renaming a phone moved its backup folder with the error ignored: if the
// move failed the device pointed at a new, empty folder and the backups
// "vanished".
func TestRenamingADeviceMovesItsBackups(t *testing.T) {
	base := t.TempDir()
	dev := &CompanionDevice{ID: "a", Name: "Pixel", StoragePath: filepath.Join(base, "Pixel")}
	writeBackup(t, dev.StoragePath, "IMG_1.jpg")
	devs := map[string]*CompanionDevice{"a": dev}

	if err := relocateDeviceFolder(base, devs, dev, "Work Phone"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dev.StoragePath, "IMG_1.jpg")); err != nil {
		t.Fatalf("backup not at the new folder %s: %v", dev.StoragePath, err)
	}
}

// Two devices whose names sanitise the same used to share one folder -
// deleting one wiped the other's backups.
func TestADeviceNeverTakesAnotherDevicesFolder(t *testing.T) {
	base := t.TempDir()
	a := &CompanionDevice{ID: "a", Name: "Phone", StoragePath: filepath.Join(base, "Phone")}
	b := &CompanionDevice{ID: "b", Name: "Tablet", StoragePath: filepath.Join(base, "Tablet")}
	writeBackup(t, a.StoragePath, "a.jpg")
	writeBackup(t, b.StoragePath, "b.jpg")
	devs := map[string]*CompanionDevice{"a": a, "b": b}

	if err := relocateDeviceFolder(base, devs, b, "Phone"); err != nil {
		t.Fatal(err)
	}
	if b.StoragePath == a.StoragePath {
		t.Fatal("two devices now share one backup folder")
	}
	if _, err := os.Stat(filepath.Join(b.StoragePath, "b.jpg")); err != nil {
		t.Fatalf("b's backups not moved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(a.StoragePath, "a.jpg")); err != nil {
		t.Fatalf("a's backups disturbed: %v", err)
	}
}

func TestAFailedFolderMoveKeepsTheOldPath(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "Pixel")
	writeBackup(t, old, "IMG_1.jpg")
	// The backup base is unusable (a file, not a folder): nothing can be
	// created in it, even as root.
	base := filepath.Join(dir, "not-a-folder")
	os.WriteFile(base, []byte("x"), 0o644)
	dev := &CompanionDevice{ID: "a", Name: "Pixel", StoragePath: old}

	if err := relocateDeviceFolder(base, map[string]*CompanionDevice{"a": dev}, dev, "Other"); err == nil {
		t.Fatal("expected an error")
	}
	if dev.StoragePath != old {
		t.Fatalf("path changed although the move failed: %s", dev.StoragePath)
	}
	if _, err := os.Stat(filepath.Join(old, "IMG_1.jpg")); err != nil {
		t.Fatalf("backups lost: %v", err)
	}
}
