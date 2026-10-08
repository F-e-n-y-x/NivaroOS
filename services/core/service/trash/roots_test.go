package trash

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// fakeMounts points mountFor at a made-up mountinfo: each entry is
// mount point -> "fstype source".
func fakeMounts(t *testing.T, mounts map[string][2]string) {
	t.Helper()
	var s string
	i := 30
	for point, m := range mounts {
		s += fmt.Sprintf("%d 1 0:%d / %s rw,relatime shared:1 - %s %s rw\n", i, i, point, m[0], m[1])
		i++
	}
	p := filepath.Join(t.TempDir(), "mountinfo")
	os.WriteFile(p, []byte(s), 0o644)
	old := mountInfoPath
	mountInfoPath = p
	t.Cleanup(func() { mountInfoPath = old })
}

func TestSupportPerLocation(t *testing.T) {
	disk, share, roShare, cloudMove, gdrive, s3, unknown, tmp := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	fakeMounts(t, map[string][2]string{
		disk: {"ext4", "/dev/sdb1"}, share: {"cifs", "//nas/media"}, roShare: {"nfs4", "nas:/backup"},
		cloudMove: {"fuse.rclone", "webdav:"}, gdrive: {"fuse.rclone", "gdrive:"}, s3: {"fuse.rclone", "s3:"},
		unknown: {"fuse.rclone", "new:"}, tmp: {"tmpfs", "tmpfs"},
	})
	cf := filepath.Join(t.TempDir(), "cloud-trash.json")
	os.WriteFile(cf, []byte(fmt.Sprintf(`{%q:{"mode":"trash"},%q:{"mode":"provider","provider":"Google Drive"},%q:{"mode":"none"}}`, cloudMove, gdrive, s3)), 0o644)
	oldCF, oldW := CloudTrashFile, canWrite
	CloudTrashFile = cf
	canWrite = func(dir string) error {
		if dir == roShare {
			return syscall.EROFS
		}
		return nil
	}
	t.Cleanup(func() { CloudTrashFile, canWrite = oldCF, oldW })

	for _, c := range []struct {
		dir  string
		want Support
	}{
		{disk, Support{Supported: true, Kind: KindDisk}},
		{share, Support{Supported: true, Kind: KindShare}},
		{roShare, Support{Kind: KindShare, Reason: ReasonReadOnly}},
		{cloudMove, Support{Supported: true, Kind: KindCloud}},
		{gdrive, Support{Kind: KindCloud, Provider: "Google Drive"}},
		{s3, Support{Kind: KindCloud, Reason: ReasonNoServerMove}},
		{unknown, Support{Kind: KindCloud, Reason: ReasonNoServerMove}},
		{tmp, Support{Kind: KindDisk, Reason: ReasonNoTrash}},
		{filepath.Join(share, trashDirName, "files"), Support{Kind: KindShare, Reason: ReasonNoTrash}},
	} {
		if got := SupportsTrash(filepath.Join(c.dir, "x")); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.dir, got, c.want)
		}
	}

	// A share and a cloud drive keep their Trash at their top folder, and
	// the list says where each item lives.
	b := New(Options{IndexPath: filepath.Join(t.TempDir(), "roots.json")})
	write(t, filepath.Join(share, "film.mkv"), "F")
	write(t, filepath.Join(cloudMove, "doc.txt"), "D")
	if _, err := b.Trash([]string{filepath.Join(share, "film.mkv"), filepath.Join(cloudMove, "doc.txt")}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(share, trashDirName, "info")); err != nil {
		t.Fatal("share trash not at the share's top folder")
	}
	kinds := map[string]string{}
	for _, it := range b.List() {
		kinds[it.Name] = it.Kind + " " + it.Location
	}
	if kinds["film.mkv"] != "share //nas/media" || kinds["doc.txt"] != "cloud "+filepath.Base(cloudMove) {
		t.Fatalf("kinds: %v", kinds)
	}
	if _, err := b.Trash([]string{filepath.Join(roShare, "x")}); err == nil {
		t.Fatal("trashed on a read-only share")
	}
}

func TestHungRootDoesNotHangTheTrash(t *testing.T) {
	b, root := newTestBin(t)
	write(t, filepath.Join(root, "a"), "A")
	if _, err := b.Trash([]string{filepath.Join(root, "a")}); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	b.beforeRead = func(string) { <-release }
	old := rootTimeout
	rootTimeout = 50 * time.Millisecond
	t.Cleanup(func() { rootTimeout = old })

	if n := len(b.List()); n != 0 {
		t.Fatalf("hung root listed %d items", n)
	}
	// Still hung: skipped at once, not waited for again.
	start := time.Now()
	b.List()
	if time.Since(start) > 40*time.Millisecond {
		t.Fatal("waited on a root already known to hang")
	}
	close(release)
	time.Sleep(20 * time.Millisecond)
	if n := len(b.List()); n != 1 {
		t.Fatalf("after recovery: %d items", n)
	}
}
