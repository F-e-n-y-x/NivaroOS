package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/pkg/cloudcache"
	_ "github.com/rclone/rclone/backend/memory"
	"github.com/rclone/rclone/cmd/mountlib"
	"github.com/rclone/rclone/fs"
	rconfig "github.com/rclone/rclone/fs/config"
	rlog "github.com/rclone/rclone/fs/log"
	"github.com/rclone/rclone/vfs"
	"github.com/rclone/rclone/vfs/vfscommon"
)

const teraboxErr = "vfs cache: failed to upload try #1, will retry in 10s: vfs cache: failed to transfer file from cache to remote: Size of upload file more than allowed free plan limit (4GB)"

// testMount gives a VFS on an in-memory remote "tr" with a disk cache in a
// temp dir; uploads wait an hour unless moved forward, so they stay queued.
func testMount(t *testing.T) (cloudMount, string, fs.Fs) {
	t.Helper()
	cacheDir := t.TempDir()
	if err := rconfig.SetCacheDir(cacheDir); err != nil {
		t.Fatal(err)
	}
	rconfig.LoadedData().SetValue("tr", "type", "memory")
	rconfig.LoadedData().SetValue("tr", "mount_point", "/mnt/tr-test")
	ctx := context.Background()
	f, err := fs.NewFs(ctx, "tr:")
	if err != nil {
		t.Fatal(err)
	}
	opt := vfscommon.Opt
	opt.CacheMode = vfscommon.CacheModeFull
	opt.WriteBack = fs.Duration(time.Hour)
	v := vfs.New(ctx, f, &opt)
	t.Cleanup(func() { v.Shutdown(); v.CleanUp() })

	oldSet, oldStore, oldLists, oldOpen := CloudCacheSettings(), stuckStore, MountLists, openUnder
	s := cloudcache.Defaults()
	s.Dir = cacheDir
	setCloudCacheSettings(s)
	stuckStore = cloudcache.OpenStuckStore(filepath.Join(t.TempDir(), "stuck.json"))
	m := cloudMount{Name: "tr", MountPoint: "/mnt/tr-test", VFS: v}
	MountLists = map[string]*mountlib.MountPoint{m.MountPoint: {MountPoint: m.MountPoint, Fs: f, VFS: v}}
	openUnder = func(string) []string { return nil }
	t.Cleanup(func() {
		setCloudCacheSettings(oldSet)
		stuckStore, MountLists, openUnder = oldStore, oldLists, oldOpen
	})
	return m, cacheDir, f
}

func queued(t *testing.T, m cloudMount, name string) (q struct {
	found, parked bool
}) {
	t.Helper()
	for _, it := range queueOf(vfsCacheOf(m.VFS)) {
		if it.Name == name {
			q.found, q.parked = true, isParked(it)
		}
	}
	return q
}

func TestVFSCacheAccessor(t *testing.T) {
	m, dir, _ := testMount(t)
	c := vfsCacheOf(m.VFS)
	if c == nil {
		t.Fatal("can't reach rclone's VFS cache - did an rclone update rename vfs.VFS.cache?")
	}
	m.VFS.Mkdir("a", 0o755)
	if err := m.VFS.WriteFile("a/new.iso", []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	if q := queued(t, m, "a/new.iso"); !q.found || q.parked {
		t.Fatalf("expected a queued upload: %+v", q)
	}
	if p := cloudcache.PendingItems(dir, "tr"); len(p) != 1 || p[0].Name != "a/new.iso" {
		t.Fatalf("pending on disk: %+v (cache layout changed?)", p)
	}
	if vfsCacheOf(nil) != nil {
		t.Fatal("nil VFS")
	}
}

func TestPermanentFailureParksAndPendingGuard(t *testing.T) {
	m, _, _ := testMount(t)
	m.VFS.WriteFile("big.iso", []byte("0123456789"), 0o644)
	if err := checkIdle([]cloudMount{m}); err == nil || !strings.Contains(err.Error(), "still uploading 1 file") {
		t.Fatalf("pending guard should refuse: %v", err)
	}
	// a transient failure keeps retrying
	uploadFails.record("big.iso", "vfs cache: failed to upload try #1, will retry in 10s: unexpected EOF")
	checkUploads([]cloudMount{m}, time.Now())
	if q := queued(t, m, "big.iso"); q.parked {
		t.Fatal("a transient error must not park the upload")
	}
	// TeraBox's size limit stops it at once
	uploadFails.record("big.iso", teraboxErr)
	checkUploads([]cloudMount{m}, time.Now())
	if q := queued(t, m, "big.iso"); !q.found || !q.parked {
		t.Fatalf("expected parked: %+v", q)
	}
	it, ok := stuckStore.Get("tr", "big.iso")
	if !ok || !strings.Contains(it.Reason, "4 GB") || it.Size != 10 {
		t.Fatalf("stuck entry: %+v %v", it, ok)
	}
	// parked uploads don't block settings changes
	if err := checkIdle([]cloudMount{m}); err != nil {
		t.Fatalf("parked upload blocked the change: %v", err)
	}
	openUnder = func(string) []string { return []string{"vlc (42)"} }
	var busy *ErrCacheBusy
	if err := checkIdle([]cloudMount{m}); !errors.As(err, &busy) || !strings.Contains(err.Error(), "vlc") {
		t.Fatalf("open files should refuse: %v", err)
	}
	st := GetCloudCacheStatus()
	if len(st.Accounts) == 0 || st.Accounts[0].Name != "tr" || len(st.Accounts[0].Stuck) != 1 || st.Accounts[0].Waiting != 0 || st.Accounts[0].PendingFiles != 1 {
		t.Fatalf("status: %+v", st.Accounts)
	}
}

func TestRetryUploadsAgain(t *testing.T) {
	m, _, f := testMount(t)
	m.VFS.WriteFile("r.bin", []byte("retry me"), 0o644)
	uploadFails.record("r.bin", teraboxErr)
	checkUploads([]cloudMount{m}, time.Now())
	if err := RetryStuckUpload("tr", "r.bin"); err != nil {
		t.Fatal(err)
	}
	if _, ok := stuckStore.Get("tr", "r.bin"); ok {
		t.Fatal("still listed as stuck")
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := f.NewObject(context.Background(), "r.bin"); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("retried upload never reached the remote")
}

func TestSaveToFolderVerifiesThenDrops(t *testing.T) {
	m, dir, f := testMount(t)
	m.VFS.Mkdir("iso", 0o755)
	m.VFS.WriteFile("iso/win.iso", []byte("big file body"), 0o644)
	uploadFails.record("iso/win.iso", teraboxErr)
	checkUploads([]cloudMount{m}, time.Now())
	dest := t.TempDir()
	if _, err := SaveStuckUpload("tr", "iso/win.iso", "relative"); err == nil {
		t.Fatal("relative folder accepted")
	}
	if _, err := SaveStuckUpload("tr", "iso/win.iso", "/mnt/tr-test/x"); err == nil {
		t.Fatal("folder on the online drive accepted")
	}
	if _, err := SaveStuckUpload("tr", "iso/win.iso", dir); err == nil {
		t.Fatal("folder inside the cache accepted")
	}
	if _, ok := stuckStore.Get("tr", "iso/win.iso"); !ok {
		t.Fatal("a refused save must keep the upload")
	}
	res, err := SaveStuckUpload("tr", "iso/win.iso", dest)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dest, "win.iso"))
	if string(raw) != "big file body" || res.Bytes != 13 {
		t.Fatalf("saved copy: %q %+v", raw, res)
	}
	if q := queued(t, m, "iso/win.iso"); q.found {
		t.Fatal("still queued after save")
	}
	if len(cloudcache.PendingItems(dir, "tr")) != 0 {
		t.Fatal("still pending on disk")
	}
	if _, err := m.VFS.Stat("iso/win.iso"); err == nil {
		t.Fatal("a never-uploaded file should leave the drive")
	}
	if _, err := f.NewObject(context.Background(), "iso/win.iso"); err == nil {
		t.Fatal("must not have uploaded")
	}
}

func TestDiscardKeepsCloudVersion(t *testing.T) {
	m, dir, f := testMount(t)
	ctx := context.Background()
	// v1 is in the cloud; v2 is written locally and can't upload
	m.VFS.WriteFile("doc.txt", []byte("v1"), 0o644)
	for _, it := range queueOf(vfsCacheOf(m.VFS)) {
		vfsCacheOf(m.VFS).QueueSetExpiry(it.ID, time.Now(), 0)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if o, err := f.NewObject(ctx, "doc.txt"); err == nil && o.Size() == 2 && len(queueOf(vfsCacheOf(m.VFS))) == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("v1 never uploaded")
		}
		time.Sleep(100 * time.Millisecond)
	}
	m.VFS.WriteFile("doc.txt", []byte("version 2"), 0o644)
	uploadFails.record("doc.txt", teraboxErr)
	checkUploads([]cloudMount{m}, time.Now())
	if err := DiscardStuckUpload("tr", "doc.txt"); err != nil {
		t.Fatal(err)
	}
	o, err := f.NewObject(ctx, "doc.txt")
	if err != nil || o.Size() != 2 {
		t.Fatalf("the cloud version was touched: %v", err)
	}
	if len(cloudcache.PendingItems(dir, "tr")) != 0 || len(queueOf(vfsCacheOf(m.VFS))) != 0 {
		t.Fatal("local change not discarded")
	}
	if err := DiscardStuckUpload("tr", "doc.txt"); err == nil {
		t.Fatal("discarding something not stuck should fail")
	}
}

func TestClearMountedNeverTouchesPending(t *testing.T) {
	m, dir, f := testMount(t)
	ctx := context.Background()
	// a clean cached file: upload it, then read it back through the VFS
	m.VFS.WriteFile("clean.txt", []byte("cached"), 0o644)
	for _, it := range queueOf(vfsCacheOf(m.VFS)) {
		vfsCacheOf(m.VFS).QueueSetExpiry(it.ID, time.Now(), 0)
	}
	deadline := time.Now().Add(10 * time.Second)
	for len(queueOf(vfsCacheOf(m.VFS))) > 0 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if _, err := f.NewObject(ctx, "clean.txt"); err != nil {
		t.Fatal("clean.txt never uploaded")
	}
	m.VFS.WriteFile("pending.bin", []byte("not uploaded"), 0o644)
	res, err := ClearCloudCache("tr")
	if err != nil {
		t.Fatal(err)
	}
	if res.RemovedFiles != 1 || res.KeptPending != 1 {
		t.Fatalf("clear: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(cloudcache.DataRoot(dir, "tr"), "clean.txt")); err == nil {
		t.Fatal("clean cache kept")
	}
	if p := cloudcache.PendingItems(dir, "tr"); len(p) != 1 || p[0].Name != "pending.bin" {
		t.Fatalf("pending touched: %+v", p)
	}
	if q := queued(t, m, "pending.bin"); !q.found {
		t.Fatal("pending upload dropped from the queue")
	}
	// the clean file still reads (from the cloud)
	if b, err := m.VFS.ReadFile("clean.txt"); err != nil || string(b) != "cached" {
		t.Fatalf("read after clear: %q %v", b, err)
	}
	if _, err := ClearCloudCache("nope"); err == nil {
		t.Fatal("unknown account accepted")
	}
}

func TestLogHookRecordsUploadFailures(t *testing.T) {
	fs.SetLogger(&uploadLogHook{Handler: rlog.Handler})
	fs.Errorf("dir/x.bin", "vfs cache: failed to upload try #%d, will retry in %v: %v", 2, 20*time.Second, errors.New("http error 413: too big"))
	fs.Errorf("dir/y.bin", "something else failed")
	if msg, ok := uploadFails.get("dir/x.bin"); !ok || !strings.Contains(msg, "413") {
		t.Fatalf("failure not recorded: %q %v", msg, ok)
	}
	if _, ok := uploadFails.get("dir/y.bin"); ok {
		t.Fatal("unrelated error recorded")
	}
}
