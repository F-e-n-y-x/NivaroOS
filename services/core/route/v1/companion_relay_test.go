package v1

import (
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/core/service"
	"github.com/F-e-n-y-x/NivaroOS/services/core/service/transfer"
)

// relayPhone is a phone file server that generates its files instead of
// holding them, so a large transfer costs the test no memory either.
type relayPhone struct {
	id, secret string
	files      map[string]int64               // path -> size
	dirs       map[string][]CompanionFileItem // path -> listing
	dlFailAt   int64                          // abort a download after this many bytes
	dlStallAt  int64                          // stall a download here until the request ends
	upFailAt   int64                          // reject an upload after this many bytes
	uploads    atomic.Int32                   // upload requests received
	inflight   atomic.Int32
	mu         sync.Mutex
	stored     map[string][32]byte // completed uploads: path -> sha256
}

// patternReader yields the same bytes for a given offset every time.
type patternReader struct{ off, n int64 }

func (p *patternReader) Read(b []byte) (int, error) {
	if p.off >= p.n {
		return 0, io.EOF
	}
	if int64(len(b)) > p.n-p.off {
		b = b[:p.n-p.off]
	}
	for i := range b {
		b[i] = byte((p.off + int64(i)) % 251)
	}
	p.off += int64(len(b))
	return len(b), nil
}

func patternSum(n int64) [32]byte {
	h := sha256.New()
	io.Copy(h, &patternReader{n: n})
	var s [32]byte
	copy(s[:], h.Sum(nil))
	return s
}

func (f *relayPhone) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/hello" {
		json.NewEncoder(w).Encode(map[string]string{"id": f.id, "proof": companionHelloProof(f.secret, f.id, r.URL.Query().Get("nonce"))})
		return
	}
	if r.Header.Get("X-Companion-Secret") != f.secret {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	f.inflight.Add(1)
	defer f.inflight.Add(-1)
	p := r.URL.Query().Get("path")
	switch r.URL.Path {
	case "/files":
		items, ok := f.dirs[p]
		if !ok {
			w.Write([]byte(`{"success":false,"message":"Not a folder"}`))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "files": items})
	case "/download":
		f.mu.Lock()
		size, ok := f.files[p]
		f.mu.Unlock()
		if !ok {
			w.WriteHeader(404)
			return
		}
		if r.Header.Get("Range") == "bytes=0-0" {
			w.Header().Set("Content-Range", "bytes 0-0/"+strconv.FormatInt(size, 10))
			w.WriteHeader(http.StatusPartialContent)
			w.Write([]byte{0})
			return
		}
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		src := &patternReader{n: size}
		buf := make([]byte, 256<<10)
		for src.off < size {
			if f.dlFailAt > 0 && src.off >= f.dlFailAt {
				panic(http.ErrAbortHandler) // the phone dropped off mid-file
			}
			if f.dlStallAt > 0 && src.off >= f.dlStallAt {
				<-r.Context().Done()
				return
			}
			n, _ := src.Read(buf)
			if _, err := w.Write(buf[:n]); err != nil {
				return
			}
		}
	case "/upload":
		f.uploads.Add(1)
		h := sha256.New()
		var body io.Reader = r.Body
		if f.upFailAt > 0 {
			body = io.LimitReader(r.Body, f.upFailAt)
		}
		n, err := io.Copy(h, body)
		if err != nil || f.upFailAt > 0 || (r.ContentLength >= 0 && n != r.ContentLength) {
			w.WriteHeader(400)
			w.Write([]byte(`{"success":false,"message":"The upload was incomplete"}`))
			return
		}
		var s [32]byte
		copy(s[:], h.Sum(nil))
		f.mu.Lock()
		f.stored[p] = s
		f.files[p] = n
		f.mu.Unlock()
		w.Write([]byte(`{"success":true}`))
	case "/mkdir", "/delete":
		w.Write([]byte(`{"success":true}`))
	default:
		w.WriteHeader(404)
	}
}

const relayRoot = "/storage/emulated/0"

// twoPhones registers phones A and B (fake, on loopback) and returns their
// server-side folders. TMPDIR and the upload staging dir point at empty
// folders that must stay empty.
func twoPhones(t *testing.T, a, b *relayPhone) (aDir, bDir string) {
	t.Helper()
	_, base := useTempCompanionState(t)
	useLoopbackAddressKinds(t)
	tmp, staging := t.TempDir(), t.TempDir()
	t.Setenv("TMPDIR", tmp)
	oldStaging := service.UploadStagingDir
	service.UploadStagingDir = staging
	t.Cleanup(func() {
		service.UploadStagingDir = oldStaging
		for _, d := range []string{tmp, staging} {
			if left, _ := os.ReadDir(d); len(left) > 0 {
				t.Errorf("files left in %s: %v", d, left)
			}
		}
	})
	companionMu.Lock()
	companionLoaded = true
	for _, p := range []*relayPhone{a, b} {
		port := freePort(t)
		listenOn(t, p, port, "127.0.0.1")
		companionDevices[p.id] = &CompanionDevice{ID: p.id, Name: p.id, OwnerUserID: "1", Secret: p.secret, Port: port,
			IP: "127.0.0.1", StoragePath: filepath.Join(base, p.id), RootPath: relayRoot, LastSeen: time.Now()}
	}
	companionMu.Unlock()
	return filepath.Join(base, a.id), filepath.Join(base, b.id)
}

func newRelayPhone(id string) *relayPhone {
	return &relayPhone{id: id, secret: id + "-secret", files: map[string]int64{}, dirs: map[string][]CompanionFileItem{}, stored: map[string][32]byte{}}
}

// runJob copies src to dest through the real engine and records every
// progress value it saw.
func runJob(t *testing.T, src, dest string, during func(m *transfer.Manager, id string)) (transfer.Job, []int64) {
	return runSpec(t, transfer.Spec{Kind: transfer.KindCopy, Sources: []string{src}, Dest: dest}, during)
}

func runSpec(t *testing.T, spec transfer.Spec, during func(m *transfer.Manager, id string)) (transfer.Job, []int64) {
	t.Helper()
	service.InitTransfers(t.TempDir())
	m := service.Transfers
	t.Cleanup(m.Close)
	j, err := m.Submit(spec)
	if err != nil {
		t.Fatal(err)
	}
	return waitJobDone(t, m, j.ID, during)
}

func waitJobDone(t *testing.T, m *transfer.Manager, id string, during func(m *transfer.Manager, id string)) (transfer.Job, []int64) {
	t.Helper()
	j := transfer.Job{ID: id}
	var seen []int64
	deadline := time.Now().Add(60 * time.Second)
	for {
		cur, _ := m.Get(j.ID)
		seen = append(seen, cur.BytesDone)
		if left, _ := os.ReadDir(os.Getenv("TMPDIR")); len(left) > 0 {
			t.Fatalf("staged in TMPDIR: %v", left)
		}
		if during != nil && cur.State == transfer.StateRunning && cur.BytesDone > 0 {
			during(m, j.ID)
			during = nil
		}
		if cur.State.Terminal() {
			return cur, seen
		}
		if time.Now().After(deadline) {
			t.Fatalf("job stuck: %+v", cur)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func waitIdle(t *testing.T, phones ...*relayPhone) {
	t.Helper()
	waitFor(t, func() bool {
		for _, p := range phones {
			if p.inflight.Load() != 0 {
				return false
			}
		}
		return true
	})
}

func TestCompanionToCompanionStreamsWithoutStaging(t *testing.T) {
	a, b := newRelayPhone("A"), newRelayPhone("B")
	const size = 96<<20 + 123
	a.files[relayRoot+"/big.bin"] = size
	aDir, bDir := twoPhones(t, a, b)

	j, seen := runJob(t, aDir+"/big.bin", bDir+"/Download", nil)
	if j.State != transfer.StateDone {
		t.Fatalf("job: %+v", j)
	}
	waitIdle(t, a, b)
	if b.stored[relayRoot+"/Download/big.bin"] != patternSum(size) {
		t.Fatal("B didn't get the same bytes")
	}
	if j.BytesTotal != size || j.BytesDone != size {
		t.Fatalf("bytes %d/%d, want %d", j.BytesDone, j.BytesTotal, size)
	}
	for i := 1; i < len(seen); i++ {
		if seen[i] < seen[i-1] {
			t.Fatalf("progress went back: %d -> %d", seen[i-1], seen[i])
		}
	}
}

func TestCompanionToCompanionFolder(t *testing.T) {
	a, b := newRelayPhone("A"), newRelayPhone("B")
	f := relayRoot + "/F"
	a.dirs[f] = []CompanionFileItem{{Name: "x.jpg", Path: f + "/x.jpg", Size: 1000}, {Name: "sub", Path: f + "/sub", IsDir: true}}
	a.dirs[f+"/sub"] = []CompanionFileItem{{Name: "y.mp4", Path: f + "/sub/y.mp4", Size: 5000}}
	a.files[f+"/x.jpg"], a.files[f+"/sub/y.mp4"] = 1000, 5000
	aDir, bDir := twoPhones(t, a, b)

	j, _ := runJob(t, aDir+"/F", bDir, nil)
	waitIdle(t, a, b)
	if j.State != transfer.StateDone || j.BytesDone != 6000 || j.BytesTotal != 6000 {
		t.Fatalf("job: %+v", j)
	}
	if b.stored[relayRoot+"/F/x.jpg"] != patternSum(1000) || b.stored[relayRoot+"/F/sub/y.mp4"] != patternSum(5000) {
		t.Fatalf("B has %v", b.stored)
	}
}

func TestCompanionToCompanionCancel(t *testing.T) {
	a, b := newRelayPhone("A"), newRelayPhone("B")
	a.files[relayRoot+"/big.bin"] = 64 << 20
	a.dlStallAt = 8 << 20
	aDir, bDir := twoPhones(t, a, b)

	j, _ := runJob(t, aDir+"/big.bin", bDir, func(m *transfer.Manager, id string) { m.Cancel(id) })
	if j.State != transfer.StateCancelled {
		t.Fatalf("job: %+v", j)
	}
	waitIdle(t, a, b)
	if len(b.stored) != 0 {
		t.Fatalf("B kept a partial file: %v", b.stored)
	}
}

func TestCompanionToCompanionSourceDrops(t *testing.T) {
	a, b := newRelayPhone("A"), newRelayPhone("B")
	a.files[relayRoot+"/big.bin"] = 32 << 20
	a.dlFailAt = 4 << 20
	aDir, bDir := twoPhones(t, a, b)

	j, _ := runJob(t, aDir+"/big.bin", bDir, nil)
	if j.State != transfer.StateFailed && j.State != transfer.StateDoneWithErrors {
		t.Fatalf("job: %+v", j)
	}
	waitIdle(t, a, b)
	if len(b.stored) != 0 {
		t.Fatalf("B kept a partial file: %v", b.stored)
	}
}

func TestCompanionToCompanionTargetRejects(t *testing.T) {
	a, b := newRelayPhone("A"), newRelayPhone("B")
	a.files[relayRoot+"/big.bin"] = 32 << 20
	b.upFailAt = 4 << 20
	aDir, bDir := twoPhones(t, a, b)

	j, _ := runJob(t, aDir+"/big.bin", bDir, nil)
	if j.State != transfer.StateFailed && j.State != transfer.StateDoneWithErrors {
		t.Fatalf("job: %+v", j)
	}
	waitIdle(t, a, b)
	if len(b.stored) != 0 {
		t.Fatalf("B kept a rejected file: %v", b.stored)
	}
}

// A phone that drops mid-file fails only that item; the engine's Retry
// then sends just what didn't arrive and the job finishes complete.
func TestCompanionToCompanionDropThenRetry(t *testing.T) {
	a, b := newRelayPhone("A"), newRelayPhone("B")
	f := relayRoot + "/F"
	a.dirs[f] = []CompanionFileItem{{Name: "x.jpg", Path: f + "/x.jpg", Size: 1000}, {Name: "big.bin", Path: f + "/big.bin", Size: 32 << 20}}
	a.files[f+"/x.jpg"], a.files[f+"/big.bin"] = 1000, 32<<20
	a.dlFailAt = 4 << 20
	aDir, bDir := twoPhones(t, a, b)

	j, _ := runJob(t, aDir+"/F", bDir, nil)
	waitIdle(t, a, b)
	if j.State != transfer.StateDoneWithErrors && j.State != transfer.StateFailed {
		t.Fatalf("job: %+v", j)
	}
	if _, ok := b.stored[f+"/big.bin"]; ok {
		t.Fatal("B kept the partial file")
	}
	before := b.uploads.Load()
	a.dlFailAt = 0
	r, err := service.Transfers.Retry(j.ID)
	if err != nil {
		t.Fatal(err)
	}
	r, _ = waitJobDone(t, service.Transfers, r.ID, nil)
	waitIdle(t, a, b)
	if r.State != transfer.StateDone || r.BytesDone != r.BytesTotal {
		t.Fatalf("retry: %+v", r)
	}
	if got := b.uploads.Load() - before; got != 1 {
		t.Fatalf("retry sent %d files, want only the one that failed", got)
	}
	if b.stored[f+"/big.bin"] != patternSum(32<<20) {
		t.Fatal("B didn't get big.bin after the retry")
	}
}

// "Skip" keeps the file already on B, even one of another size.
func TestCompanionToCompanionSkipKeepsTarget(t *testing.T) {
	a, b := newRelayPhone("A"), newRelayPhone("B")
	a.files[relayRoot+"/x.jpg"] = 1000
	b.files[relayRoot+"/x.jpg"] = 7
	aDir, bDir := twoPhones(t, a, b)

	j, _ := runSpec(t, transfer.Spec{Kind: transfer.KindCopy, Sources: []string{aDir + "/x.jpg"}, Dest: bDir, Conflict: transfer.ConflictSkip}, nil)
	waitIdle(t, a, b)
	if j.State != transfer.StateDone || b.uploads.Load() != 0 {
		t.Fatalf("job %+v, uploads %d", j, b.uploads.Load())
	}
}
