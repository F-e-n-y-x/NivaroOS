package ntfsperm

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

var want = Policy{UID: 1000, GID: 1000, FileMode: 0o777, DirMode: 0o777}

// mountNTFS3 formats a small image and mounts it with the kernel ntfs3
// driver, the way /etc/fstab mounts the data drives.
func mountNTFS3(t *testing.T) string {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("needs root to mount")
	}
	if _, err := exec.LookPath("/usr/sbin/mkntfs"); err != nil {
		t.Skip("mkntfs not installed")
	}
	dir := t.TempDir()
	img, mnt := filepath.Join(dir, "disk.img"), filepath.Join(dir, "mnt")
	if err := os.Mkdir(mnt, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(img)
	if err != nil {
		t.Fatal(err)
	}
	f.Truncate(64 << 20)
	f.Close()
	if out, err := exec.Command("/usr/sbin/mkntfs", "-Q", "-F", "-q", img).CombinedOutput(); err != nil {
		t.Fatalf("mkntfs: %v %s", err, out)
	}
	if out, err := exec.Command("mount", "-t", "ntfs3", "-o", "loop,uid=1000,gid=1000,umask=000", img, mnt).CombinedOutput(); err != nil {
		t.Skipf("ntfs3 not available: %v %s", err, out)
	}
	t.Cleanup(func() { exec.Command("umount", mnt).Run() })
	return mnt
}

func owner(t *testing.T, p string) (uid, gid uint32, mode os.FileMode) {
	t.Helper()
	fi, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	st := fi.Sys().(*syscall.Stat_t)
	return st.Uid, st.Gid, fi.Mode()
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", what)
}

func startWatch(t *testing.T, mnt string) {
	t.Helper()
	w, err := Start(mnt, want)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
}

func looksLikeNtfs3g(t *testing.T, p string, perm os.FileMode) func() bool {
	return func() bool {
		uid, gid, mode := owner(t, p)
		return uid == 1000 && gid == 1000 && (mode&os.ModeSymlink != 0 || mode.Perm() == perm)
	}
}

// Under ntfs-3g every file showed as uid 1000 / 0777. ntfs3 instead keeps
// the creator's owner and mode, so a file root writes (Files app, samba,
// containers) became read-only for the desktop user.
func TestWhatRootCreatesEndsUpOwnedByTheDriveUser(t *testing.T) {
	mnt := mountNTFS3(t)
	startWatch(t, mnt)

	file := filepath.Join(mnt, "report.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(mnt, "Photos", "2026")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(dir, "a.jpg")
	os.WriteFile(nested, []byte("jpg"), 0o600)
	link := filepath.Join(mnt, "latest")
	os.Symlink("Photos/2026/a.jpg", link)

	eventually(t, "file owned by 1000 with 0777", looksLikeNtfs3g(t, file, 0o777))
	eventually(t, "folders owned by 1000 with 0777", looksLikeNtfs3g(t, dir, 0o777))
	eventually(t, "file in new folder fixed", looksLikeNtfs3g(t, nested, 0o777))
	eventually(t, "symlink owned by 1000", looksLikeNtfs3g(t, link, 0))
}

func TestChmodAndRenameAreKeptInLine(t *testing.T) {
	mnt := mountNTFS3(t)
	startWatch(t, mnt)
	f := filepath.Join(mnt, "key")
	os.WriteFile(f, []byte("k"), 0o644)
	eventually(t, "created file fixed", looksLikeNtfs3g(t, f, 0o777))

	os.Chmod(f, 0o600)
	eventually(t, "chmod undone", looksLikeNtfs3g(t, f, 0o777))

	// A file written elsewhere on the drive and renamed in (the transfer
	// engine's temp-then-rename) is fixed too.
	tmp := filepath.Join(mnt, ".x.nvtmp")
	os.WriteFile(tmp, []byte("t"), 0o600)
	os.Chown(tmp, 0, 0)
	dst := filepath.Join(mnt, "final.bin")
	os.Rename(tmp, dst)
	eventually(t, "renamed-in file fixed", looksLikeNtfs3g(t, dst, 0o777))
}

func TestRepairFixesWhatWasCreatedBeforeWatching(t *testing.T) {
	mnt := mountNTFS3(t)
	os.Chown(mnt, 0, 0) // the drive's top folder too
	os.MkdirAll(filepath.Join(mnt, "a", "b"), 0o700)
	os.WriteFile(filepath.Join(mnt, "a", "b", "c.txt"), []byte("c"), 0o600)
	os.WriteFile(filepath.Join(mnt, "untouched.txt"), []byte("u"), 0o644)
	os.Chown(filepath.Join(mnt, "untouched.txt"), 1000, 1000)
	os.Chmod(filepath.Join(mnt, "untouched.txt"), 0o777)

	fixed, err := Repair(context.Background(), mnt, want)
	if err != nil {
		t.Fatal(err)
	}
	if fixed != 4 { // top folder, a, a/b, a/b/c.txt
		t.Fatalf("fixed = %d, want 4", fixed)
	}
	for _, p := range []string{".", "a", "a/b", "a/b/c.txt", "untouched.txt"} {
		if !looksLikeNtfs3g(t, filepath.Join(mnt, p), 0o777)() {
			t.Errorf("%s not fixed", p)
		}
	}
}

func TestPolicyComesFromTheMountOptions(t *testing.T) {
	cases := []struct {
		fstype, opts string
		want         Policy
		ok           bool
	}{
		{"ntfs3", "rw,uid=1000,gid=1000,dmask=0000,fmask=0000,acl,iocharset=utf8,prealloc", Policy{1000, 1000, 0o777, 0o777}, true},
		{"ntfs3", "rw,uid=1000,gid=100,dmask=0022,fmask=0133", Policy{1000, 100, 0o644, 0o755}, true},
		// No uid= means the owner wants ntfs3's own (per-file) ownership.
		{"ntfs3", "rw,acl,iocharset=utf8", Policy{}, false},
		{"fuseblk", "rw,user_id=0,group_id=0,allow_other", Policy{}, false},
		{"ext4", "rw,uid=1000", Policy{}, false},
	}
	for _, c := range cases {
		got, ok := policyFor(c.fstype, c.opts)
		if ok != c.ok || got != c.want {
			t.Errorf("policyFor(%s, %s) = %+v %v, want %+v %v", c.fstype, c.opts, got, ok, c.want, c.ok)
		}
	}
}
