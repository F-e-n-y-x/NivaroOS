package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// testSys is a fake system in a temp dir: a mountinfo file, sysfs and udev
// records for fake block devices whose "mount points" are ordinary temp
// folders. The engine reads it exactly as it reads the real one.
type testSys struct {
	t        *testing.T
	root     string
	allowed  string // the only allowed root
	sysBlock string
	udev     string
	dev      string
	mi       string
	rconf    string

	mu     sync.Mutex
	mounts []*fakeMount
	nextID int
	nextMn int
	extra  []string // raw mountinfo lines
	waiter *testWaiter
}

type fakeMount struct {
	ID         int
	Dev        string // kernel name
	MajMin     string
	MountPoint string
	FSType     string
	Root       string
	UUID       string
	Serial     string
	Size       int64
	Label      string
	USB        bool
	Source     string
	Options    string
	// ShownMajMin is what mountinfo says instead of MajMin (btrfs and
	// fuse report an anonymous 0:NN device there).
	ShownMajMin string
}

// testWaiter lets a test say "the mount table changed".
type testWaiter struct {
	ch chan struct{}
}

func (w *testWaiter) wait(timeout time.Duration, quit <-chan struct{}) (bool, error) {
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-w.ch:
		return true, nil
	case <-quit:
		return false, nil
	case <-t.C:
		return false, nil
	}
}

func (w *testWaiter) close() error { return nil }

func newTestSys(t *testing.T) *testSys {
	t.Helper()
	root := t.TempDir()
	s := &testSys{
		t: t, root: root, allowed: filepath.Join(root, "allowed"),
		sysBlock: filepath.Join(root, "sys", "class", "block"), udev: filepath.Join(root, "udev"),
		dev: filepath.Join(root, "dev"), mi: filepath.Join(root, "mountinfo"), rconf: filepath.Join(root, "rclone.conf"),
		nextID: 100, nextMn: 16, waiter: &testWaiter{ch: make(chan struct{}, 8)},
	}
	for _, d := range []string{s.allowed, s.sysBlock, s.udev, filepath.Join(s.dev, "disk", "by-uuid"), filepath.Join(root, "spool")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(s.rconf, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "machine-id"), []byte("0123456789abcdef\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.write()
	return s
}

// addVolume creates a fake block device and mounts it at
// allowed/<name>. opts can adjust the mount before it is written.
func (s *testSys) addVolume(name, uuid, fstype string, opts ...func(*fakeMount)) *fakeMount {
	s.t.Helper()
	s.mu.Lock()
	s.nextID++
	s.nextMn++
	m := &fakeMount{
		ID: s.nextID, Dev: fmt.Sprintf("sd%c1", 'a'+rune(s.nextMn-16)), MajMin: fmt.Sprintf("8:%d", s.nextMn),
		MountPoint: filepath.Join(s.allowed, name), FSType: fstype, Root: "/", UUID: uuid,
		Serial: "SER" + uuid, Size: 64 << 30, Label: name,
	}
	s.mu.Unlock()
	for _, o := range opts {
		o(m)
	}
	if err := os.MkdirAll(m.MountPoint, 0o755); err != nil {
		s.t.Fatal(err)
	}
	s.addDevice(m)
	s.mu.Lock()
	s.mounts = append(s.mounts, m)
	s.mu.Unlock()
	s.write()
	return m
}

// addDevice writes sysfs and udev records for m's device.
func (s *testSys) addDevice(m *fakeMount) {
	disk := strings.TrimRight(m.Dev, "0123456789")
	diskDir := filepath.Join(s.sysBlock, disk)
	partDir := filepath.Join(diskDir, m.Dev)
	must(s.t, os.MkdirAll(partDir, 0o755))
	must(s.t, os.WriteFile(filepath.Join(diskDir, "dev"), []byte("8:0\n"), 0o644))
	removable := "0\n"
	if m.USB {
		removable = "1\n"
	}
	must(s.t, os.WriteFile(filepath.Join(diskDir, "removable"), []byte(removable), 0o644))
	must(s.t, os.WriteFile(filepath.Join(partDir, "dev"), []byte(m.MajMin+"\n"), 0o644))
	must(s.t, os.WriteFile(filepath.Join(partDir, "size"), []byte(fmt.Sprintf("%d\n", m.Size/512)), 0o644))
	must(s.t, os.WriteFile(filepath.Join(partDir, "partition"), []byte("1\n"), 0o644))
	link := filepath.Join(s.sysBlock, m.Dev)
	_ = os.Remove(link)
	must(s.t, os.Symlink(filepath.Join(disk, m.Dev), link))
	bus := "ata"
	if m.USB {
		bus = "usb"
	}
	udev := fmt.Sprintf("E:ID_FS_UUID=%s\nE:ID_FS_TYPE=%s\nE:ID_FS_LABEL=%s\nE:ID_SERIAL_SHORT=%s\nE:ID_BUS=%s\n", m.UUID, m.FSType, m.Label, m.Serial, bus)
	must(s.t, os.WriteFile(filepath.Join(s.udev, "b"+m.MajMin), []byte(udev), 0o644))
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func escapeMI(p string) string {
	r := strings.NewReplacer(" ", `\040`, "\t", `\011`, "\n", `\012`, `\`, `\134`)
	return r.Replace(p)
}

func (s *testSys) write() {
	s.mu.Lock()
	defer s.mu.Unlock()
	var b strings.Builder
	b.WriteString("1 0 0:1 / " + escapeMI(s.root) + " rw - tmpfs tmpfs rw\n")
	for _, m := range s.mounts {
		src := m.Source
		if src == "" {
			src = "/dev/" + m.Dev
		}
		opts := m.Options
		if opts == "" {
			opts = "rw"
		}
		majmin := m.MajMin
		if m.ShownMajMin != "" {
			majmin = m.ShownMajMin
		}
		fmt.Fprintf(&b, "%d 1 %s %s %s rw,relatime shared:1 - %s %s %s\n", m.ID, majmin, escapeMI(m.Root), escapeMI(m.MountPoint), m.FSType, escapeMI(src), opts)
	}
	for _, l := range s.extra {
		b.WriteString(l + "\n")
	}
	tmp := s.mi + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		s.t.Fatal(err)
	}
	if err := os.Rename(tmp, s.mi); err != nil {
		s.t.Fatal(err)
	}
}

// bind mounts root (a folder inside m's filesystem, "/" + path) of m's
// device a second time at mountPoint, like a bind mount or a subvolume
// mount: same device, new mount ID.
func (s *testSys) bind(m *fakeMount, root, mountPoint string) *fakeMount {
	s.t.Helper()
	must(s.t, os.MkdirAll(mountPoint, 0o755))
	s.mu.Lock()
	s.nextID++
	b := *m
	b.ID, b.Root, b.MountPoint = s.nextID, root, mountPoint
	s.mounts = append(s.mounts, &b)
	s.mu.Unlock()
	s.write()
	return &b
}

// addMerge mounts a mergerfs pool of branches at allowed/<name>.
func (s *testSys) addMerge(name string, branches ...string) string {
	s.t.Helper()
	mp := filepath.Join(s.allowed, name)
	must(s.t, os.MkdirAll(mp, 0o755))
	s.mu.Lock()
	s.nextID++
	line := fmt.Sprintf("%d 1 0:77 / %s rw,relatime shared:9 - fuse.mergerfs %s rw,user_id=0,group_id=0", s.nextID, escapeMI(mp), escapeMI(strings.Join(branches, ":")))
	s.extra = append(s.extra, line)
	s.mu.Unlock()
	s.write()
	return mp
}

// unmount removes a mount from the table (the folder stays, like an
// empty mount point).
func (s *testSys) unmount(m *fakeMount) {
	s.mu.Lock()
	for i, x := range s.mounts {
		if x == m {
			s.mounts = append(s.mounts[:i], s.mounts[i+1:]...)
			break
		}
	}
	s.mu.Unlock()
	s.write()
}

// remount mounts m again with a new mount ID.
func (s *testSys) remount(m *fakeMount) {
	s.mu.Lock()
	s.nextID++
	m.ID = s.nextID
	s.mounts = append(s.mounts, m)
	s.mu.Unlock()
	s.write()
}

func (s *testSys) notify() { s.waiter.ch <- struct{}{} }

func (s *testSys) writeRcloneConf(content string) {
	must(s.t, os.WriteFile(s.rconf, []byte(content), 0o600))
}

func (s *testSys) config() Config {
	return Config{
		MountInfoPath: s.mi, SysBlockDir: s.sysBlock, UdevDataDir: s.udev, DevDir: s.dev,
		MachineIDPath: filepath.Join(s.root, "machine-id"), DataRoot: filepath.Join(s.allowed, "DATA"),
		AllowedRoots: []string{s.allowed}, StagingDir: filepath.Join(s.root, "staging"),
		SpoolDir: filepath.Join(s.root, "spool"), RcloneConfigPath: s.rconf,
		NoMemoryLimit: true, WatchBackstop: time.Hour, Logf: s.t.Logf,
	}
}

var waiterMu sync.Mutex

// engine starts an engine on the fake system; it is closed at test end.
func (s *testSys) engine() *Engine {
	s.t.Helper()
	waiterMu.Lock()
	prev := newChangeWaiter
	w := s.waiter
	newChangeWaiter = func(string) (changeWaiter, error) { return w, nil }
	e, err := New(s.config())
	newChangeWaiter = prev
	waiterMu.Unlock()
	if err != nil {
		s.t.Fatal(err)
	}
	s.t.Cleanup(func() { _ = e.Close() })
	return e
}

// ep is a volume endpoint for a fake mount.
func ep(m *fakeMount, sub string) Endpoint {
	k := EPVolume
	var match *DevMatch
	if m.USB {
		k = EPUSB
		match = &DevMatch{Serial: m.Serial, SizeBytes: m.Size}
	}
	return Endpoint{Kind: k, RefID: m.UUID, Match: match, SubPath: sub, Label: m.Label}
}

// writeTree creates files: path -> content ("/" suffix = folder).
func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for p, c := range files {
		full := filepath.Join(root, filepath.FromSlash(p))
		if strings.HasSuffix(p, "/") {
			must(t, os.MkdirAll(full, 0o755))
			continue
		}
		must(t, os.MkdirAll(filepath.Dir(full), 0o755))
		must(t, os.WriteFile(full, []byte(c), 0o644))
	}
}

// readTree returns every regular file under root as path -> content.
func readTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(root, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if fi.Mode().IsRegular() {
			raw, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			out[filepath.ToSlash(rel)] = string(raw)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return out
}

// runJob starts a job and waits for it to end.
func runJob(t *testing.T, e *Engine, req JobRequest) JobStatus {
	t.Helper()
	if req.RunID == "" {
		req.RunID = fmt.Sprintf("run_test_%d", time.Now().UnixNano())
	}
	id, err := e.StartJob(context.Background(), req)
	if err != nil {
		t.Fatalf("StartJob: %v", err)
	}
	return waitJob(t, e, id)
}

// waitJobTimeout bounds waitJob (the stress test raises it).
var waitJobTimeout = 60 * time.Second

func waitJob(t *testing.T, e *Engine, id JobID) JobStatus {
	t.Helper()
	deadline := time.Now().Add(waitJobTimeout)
	for time.Now().Before(deadline) {
		st, err := e.JobStatus(context.Background(), id)
		if err != nil {
			t.Fatalf("JobStatus: %v", err)
		}
		if st.State == JobDone || st.State == JobError {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %d did not finish", id)
	return JobStatus{}
}

// jobLog collects a finished job's log.
func jobLog(t *testing.T, e *Engine, id JobID) []LogLine {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	ch, err := e.JobLog(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	var out []LogLine
	for l := range ch {
		out = append(out, l)
	}
	return out
}

const testFolderID = "5f0c7a52-3d1e-4b8f-9a6c-2e4d8b1f7c30"

// baseReq is a job request with sensible defaults.
func baseReq(op Op, src, dst Endpoint) JobRequest {
	return JobRequest{
		Op: op, JobID: "bk_test0000001", Sources: []Endpoint{src}, Dest: dst,
		Guards:       Guards{EmptySourcePct: 50, DeletePct: 10, ChangePct: 30},
		DestFolderID: testFolderID, FirstRun: true,
	}
}

func expectState(t *testing.T, st JobStatus, state JobState, code ErrorCode) {
	t.Helper()
	if st.State != state || st.Result == nil || st.Result.ErrorCode != code {
		t.Fatalf("got state %s code %q (%s), want %s %q", st.State, resultCode(st), resultDetail(st), state, code)
	}
}

func resultCode(st JobStatus) ErrorCode {
	if st.Result == nil {
		return "<no result>"
	}
	return st.Result.ErrorCode
}

func resultDetail(st JobStatus) string {
	if st.Result == nil {
		return ""
	}
	return st.Result.ErrorDetail
}
