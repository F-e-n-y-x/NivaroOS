package service

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/model"
)

func withStatusSeams(t *testing.T) (urlFile string, pushes *int) {
	t.Helper()
	urlFile = filepath.Join(t.TempDir(), "nivaroos.url")
	if err := os.WriteFile(urlFile, []byte("http://127.0.0.1:1"), 0o644); err != nil {
		t.Fatal(err)
	}
	n := 0
	oldFile, oldPush := coreURLFile, pushStatus
	coreURLFile = func() string { return urlFile }
	pushStatus = func(map[string]interface{}) error { n++; return nil }
	statusLast, coreStarted = map[string]sentStatus{}, time.Time{}
	t.Cleanup(func() { coreURLFile, pushStatus = oldFile, oldPush; statusLast = map[string]sentStatus{} })
	return urlFile, &n
}

// An unchanged status is skipped, but sent again at once when core
// restarted (it rewrites its address file), not a minute later.
func TestSendStatusResendsAfterCoreRestart(t *testing.T) {
	urlFile, pushes := withStatusSeams(t)
	sendStatus("sys_usb", []int{1})
	sendStatus("sys_usb", []int{1})
	if *pushes != 1 {
		t.Fatalf("unchanged status pushed %d times, want 1", *pushes)
	}
	later := time.Now().Add(time.Second)
	if err := os.Chtimes(urlFile, later, later); err != nil {
		t.Fatal(err)
	}
	sendStatus("sys_usb", []int{1})
	if *pushes != 2 {
		t.Fatalf("after core restart pushes=%d, want 2", *pushes)
	}
}

type fakeDisk struct {
	DiskService
	fresh, cached int
}

func (f *fakeDisk) LSBLK(useCache bool) []model.LSBLKModel {
	if useCache {
		f.cached++
	} else {
		f.fresh++
	}
	return nil
}
func (f *fakeDisk) GetUSBDriveStatusList() []model.USBDriveStatus { f.LSBLK(true); return nil }

type fakeServices struct {
	Services
	disk *fakeDisk
}

func (f fakeServices) Disk() DiskService { return f.disk }

// Each 5 s tick runs exactly one fresh lsblk, so disk usage and hot-plugs
// are at most one tick old (the cache alone lives 100 s).
func TestSendStorageStatsOneFreshLSBLKPerTick(t *testing.T) {
	withStatusSeams(t)
	disk := &fakeDisk{}
	old := MyService
	MyService = fakeServices{disk: disk}
	t.Cleanup(func() { MyService = old })
	SendStorageStats()
	if disk.fresh != 1 {
		t.Fatalf("fresh lsblk per tick = %d, want 1", disk.fresh)
	}
}
