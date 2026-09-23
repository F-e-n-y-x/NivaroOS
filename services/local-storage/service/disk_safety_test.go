package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/model"
)

func init() { logger.LogInitConsoleOnly() }

// After unmounting, the mount folder was removed with RemoveAll - if the
// unmount hadn't really taken, that deleted the data underneath.
func TestRemovingAMountFolderNeverDeletesItsContents(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "tower")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "precious.txt"), []byte("x"), 0o644)
	removeEmptyMountDir(dir)
	if _, err := os.Stat(filepath.Join(dir, "precious.txt")); err != nil {
		t.Fatalf("contents deleted: %v", err)
	}
	empty := filepath.Join(t.TempDir(), "empty")
	os.MkdirAll(empty, 0o755)
	removeEmptyMountDir(empty)
	if _, err := os.Stat(empty); !os.IsNotExist(err) {
		t.Fatal("empty mount folder left behind")
	}
}

// Disks > Remove was offered (and executed) for the system disk.
func TestTheSystemDiskCantBeRemoved(t *testing.T) {
	system := model.LSBLKModel{Path: "/dev/sda", Children: []model.LSBLKModel{{MountPoint: "/boot/efi"}, {MountPoint: "/"}}}
	swap := model.LSBLKModel{Path: "/dev/sdc", Children: []model.LSBLKModel{{MountPoint: "[SWAP]"}}}
	data := model.LSBLKModel{Path: "/dev/sdb", Children: []model.LSBLKModel{{MountPoint: "/DATA/tower"}}}
	if !diskHoldsSystem(system) || !diskHoldsSystem(swap) {
		t.Fatal("system/swap disk not recognised")
	}
	if diskHoldsSystem(data) {
		t.Fatal("data disk treated as the system disk")
	}
}

// USB eject built a bash command from the request path.
func TestUSBEjectOnlyAcceptsARealMountPoint(t *testing.T) {
	if !isCurrentMountPoint("/") {
		t.Fatal("/ not recognised as a mount point")
	}
	for _, p := range []string{t.TempDir(), "/DATA/x; rm -rf /", "relative", ""} {
		if isCurrentMountPoint(p) {
			t.Errorf("%q accepted as a mount point", p)
		}
	}
}
