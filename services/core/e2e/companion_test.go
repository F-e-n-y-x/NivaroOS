//go:build e2e

package e2e

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Fake companion phones: HTTP file servers like the app's
// CompanionFileServer, on test-only addresses in the suite's network
// namespace. Their files are generated, not stored, so a 5 GB transfer
// costs the test neither disk nor memory.
const (
	phoneLAN     = "10.213.0.1"
	phoneTailnet = "100.64.213.1"
	phoneRoot    = "/storage/emulated/0"
)

// pattern: byte i of every file is i % 251; patBuf holds enough of it to
// serve or check any chunk by slicing at offset % 251.
var patBuf = func() []byte {
	b := make([]byte, 251*4096+256<<10)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}()

func patAt(off int64, n int) []byte {
	s := int(off % 251)
	return patBuf[s : s+n]
}

type fakePhone struct {
	id, secret string
	files      map[string]int64 // phone path -> size (generated content)
	dropAt     atomic.Int64     // >0: a download aborts after this many bytes
	mu         sync.Mutex
	got        map[string]int64 // completed, verified uploads: path -> size
	uploads    atomic.Int32
	storage    string // its folder on the server (/DATA/Companion/...)
}

func (f *fakePhone) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if r.URL.Path == "/hello" {
		mac := hmac.New(sha256.New, []byte(f.secret))
		mac.Write([]byte("nivaroos-companion-hello\n" + f.id + "\n" + q.Get("nonce")))
		_ = json.NewEncoder(w).Encode(map[string]string{"id": f.id, "proof": hex.EncodeToString(mac.Sum(nil))})
		return
	}
	if r.Header.Get("X-Companion-Secret") != f.secret {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	p := q.Get("path")
	switch r.URL.Path {
	case "/files":
		_, _ = w.Write([]byte(`{"success":false,"message":"Not a folder"}`))
	case "/download":
		f.mu.Lock()
		size, ok := f.files[p]
		if !ok {
			size, ok = f.got[p]
		}
		f.mu.Unlock()
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Range") == "bytes=0-0" {
			w.Header().Set("Content-Range", "bytes 0-0/"+strconv.FormatInt(size, 10))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte{0})
			return
		}
		w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
		drop := f.dropAt.Load()
		for off := int64(0); off < size; {
			if drop > 0 && off >= drop {
				panic(http.ErrAbortHandler) // the phone dropped off mid-file
			}
			n := int64(256 << 10)
			if size-off < n {
				n = size - off
			}
			if _, err := w.Write(patAt(off, int(n))); err != nil {
				return
			}
			off += n
		}
	case "/upload":
		f.uploads.Add(1)
		buf := make([]byte, 256<<10)
		var off int64
		for {
			n, err := r.Body.Read(buf)
			if n > 0 && !bytes.Equal(buf[:n], patAt(off, n)) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"success":false,"message":"corrupt"}`))
				return
			}
			off += int64(n)
			if err != nil {
				break
			}
		}
		if r.ContentLength >= 0 && off != r.ContentLength {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"success":false,"message":"The upload was incomplete"}`))
			return
		}
		f.mu.Lock()
		f.got[p] = off
		f.mu.Unlock()
		_, _ = w.Write([]byte(`{"success":true}`))
	case "/mkdir", "/delete":
		_, _ = w.Write([]byte(`{"success":true}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// newPhone starts a fake phone on the LAN (and, with tailnet, also the
// Tailscale) test address and registers it with the core, the way the
// app's heartbeat does.
func newPhone(t *testing.T, id string, tailnet bool) *fakePhone {
	t.Helper()
	f := &fakePhone{id: id, files: map[string]int64{}, got: map[string]int64{}}
	ln, err := net.Listen("tcp", phoneLAN+":0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	srv := &http.Server{Handler: f}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	addrs := []string{phoneLAN}
	if tailnet {
		tln, err := net.Listen("tcp", net.JoinHostPort(phoneTailnet, strconv.Itoa(port)))
		if err != nil {
			t.Fatal(err)
		}
		go func() { _ = srv.Serve(tln) }()
		addrs = append(addrs, phoneTailnet)
	}
	r := call(t, "POST", "/v1/companion/register", map[string]interface{}{
		"id": id, "name": id, "platform": "android", "ip": phoneLAN, "addresses": addrs, "port": port, "root_path": phoneRoot, "is_online": true,
	})
	var data struct {
		Secret string `json:"secret"`
		Device struct {
			StoragePath string `json:"storage_path"`
		} `json:"device"`
	}
	if r.Success != 200 || json.Unmarshal(r.Data, &data) != nil || data.Secret == "" || data.Device.StoragePath == "" {
		t.Fatalf("register %s: %d %s %s", id, r.Success, r.Message, r.Data)
	}
	f.secret, f.storage = data.Secret, data.Device.StoragePath
	t.Cleanup(func() { call(t, "DELETE", "/v1/companion/devices/"+id, nil) })
	return f
}

func coreRSS(t *testing.T) int64 {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(core.cmd.Process.Pid) + "/status")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "VmRSS:") {
			kb, _ := strconv.ParseInt(strings.Fields(line)[1], 10, 64)
			return kb << 10
		}
	}
	return 0
}

func p2pSize() int64 {
	if mb, err := strconv.ParseInt(os.Getenv("NV_E2E_P2P_MB"), 10, 64); err == nil && mb > 0 {
		return mb << 20
	}
	return 5 << 30
}

// A 5 GB phone-to-phone copy streams through the core: its memory stays
// flat, nothing lands in /tmp or the upload staging dir, and progress goes
// 0 -> 100% in one pass.
func TestCompanionToCompanionLargeStream(t *testing.T) {
	a, b := newPhone(t, "e2e-big-a", false), newPhone(t, "e2e-big-b", false)
	size := p2pSize()
	a.files[phoneRoot+"/big.bin"] = size

	base := coreRSS(t)
	id := submit(t, "copy", []string{a.storage + "/big.bin"}, b.storage+"/Download", "overwrite")
	var peak int64
	var mid bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			j, _ := getJob(t, id)
			if rss := coreRSS(t); rss > peak {
				peak = rss
			}
			if j.BytesTotal > 0 && j.BytesDone > j.BytesTotal/10 && j.BytesDone < j.BytesTotal*9/10 {
				mid = true
			}
			if terminal[j.State] {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
	j := waitJob(t, id, 30*time.Minute)
	<-done
	if j.State != "done" || j.BytesDone != size {
		t.Fatalf("job %s: %d of %d bytes, %+v", j.State, j.BytesDone, size, j.Failures)
	}
	if b.got[phoneRoot+"/Download/big.bin"] != size {
		t.Fatalf("B has %v", b.got)
	}
	if !mid {
		t.Error("progress never showed a value between 10% and 90%")
	}
	if grew := peak - base; grew > 96<<20 {
		t.Errorf("core RSS grew %d MB during a %d MB stream", grew>>20, size>>20)
	}
	t.Logf("%d MB copied, core RSS %d -> peak %d MB", size>>20, base>>20, peak>>20)
	for _, d := range []string{"/tmp", "/var/lib/nivaroos/uploads"} {
		if left, _ := os.ReadDir(d); len(left) > 0 {
			t.Errorf("staged in %s: %v", d, left)
		}
	}
}

// Phone A drops off mid-file: the item fails, B keeps nothing, and Retry
// sends the file again.
func TestCompanionToCompanionDropMidPipe(t *testing.T) {
	a, b := newPhone(t, "e2e-drop-a", false), newPhone(t, "e2e-drop-b", false)
	const size = 256 << 20
	a.files[phoneRoot+"/v.mp4"] = size
	a.dropAt.Store(size / 3)

	id := submit(t, "copy", []string{a.storage + "/v.mp4"}, b.storage, "overwrite")
	j := waitJob(t, id, 5*time.Minute)
	if j.State != "failed" && j.State != "done_with_errors" {
		t.Fatalf("job after the drop: %s", j.State)
	}
	if len(b.got) != 0 {
		t.Fatalf("B kept a partial file: %v", b.got)
	}

	a.dropAt.Store(0)
	r := call(t, "POST", "/v1/batch/"+id+"/retry", nil)
	var nj jobEvent
	_ = json.Unmarshal(r.Data, &nj)
	j = waitJob(t, nj.ID, 5*time.Minute)
	if j.State != "done" || b.got[phoneRoot+"/v.mp4"] != size {
		t.Fatalf("retry: %s, B has %v", j.State, b.got)
	}
}

// Phase 1: phone B gets a peer ticket for a file on phone A; the ticket is
// A's HMAC (what A's file server checks) and only A's Tailscale address is
// offered. Without Tailscale on A the server says to use the relay.
func TestCompanionPeerTicket(t *testing.T) {
	a := newPhone(t, "e2e-peer-a", true)
	r := call(t, "POST", "/v1/companion/e2e-peer-a/peer-ticket", map[string]string{"path": a.storage + "/DCIM/x.mp4", "op": "read"})
	var d struct {
		Candidates []string  `json:"candidates"`
		Ticket     string    `json:"ticket"`
		Path       string    `json:"path"`
		ExpiresAt  time.Time `json:"expires_at"`
	}
	if r.status != 200 || json.Unmarshal(r.Data, &d) != nil {
		t.Fatalf("peer-ticket: %d %s", r.status, r.Message)
	}
	if len(d.Candidates) != 1 || !strings.HasPrefix(d.Candidates[0], phoneTailnet+":") || d.Path != phoneRoot+"/DCIM/x.mp4" {
		t.Fatalf("got %+v", d)
	}
	dot := strings.LastIndex(d.Ticket, ".")
	payload, err := base64.RawURLEncoding.DecodeString(d.Ticket[:dot])
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, []byte(a.secret))
	mac.Write([]byte("nivaroos-peer\n" + a.id + "\n" + string(payload)))
	if hex.EncodeToString(mac.Sum(nil)) != d.Ticket[dot+1:] || !strings.HasSuffix(string(payload), "\nread\n\n"+d.Path) {
		t.Fatalf("ticket doesn't verify with A's secret: %q", payload)
	}
	if left := time.Until(d.ExpiresAt); left < 119*time.Minute || left > 2*time.Hour {
		t.Fatalf("expires in %s", left)
	}

	lanOnly := newPhone(t, "e2e-peer-lan", false)
	if r := call(t, "POST", "/v1/companion/e2e-peer-lan/peer-ticket", map[string]string{"path": lanOnly.storage + "/x", "op": "read"}); r.status != http.StatusConflict {
		t.Fatalf("no Tailscale on the phone: %d, want 409", r.status)
	}
}
