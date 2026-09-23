package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testManager(t *testing.T) (*Manager, string) {
	t.Helper()
	dir := t.TempDir()
	st := NewSettingsStore(filepath.Join(dir, "state"), filepath.Join(dir, "dl"))
	m := NewManager(filepath.Join(dir, "state"), st, newTransport(true))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		for _, d := range m.List() {
			_ = m.Pause(d.ID)
		}
		cancel()
		m.Wait()
		time.Sleep(50 * time.Millisecond)
	})
	m.Start(ctx)
	return m, filepath.Join(dir, "dl")
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

// rangeServer serves content with full Range support (http.ServeContent),
// counting requests and optionally slowing each read down.
func rangeServer(content []byte, name string, delay time.Duration) (*httptest.Server, *atomic.Int32) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		var rs io.ReadSeeker = bytes.NewReader(content)
		if delay > 0 {
			rs = &slowReader{ReadSeeker: rs, delay: delay}
		}
		http.ServeContent(w, r, name, time.Time{}, rs)
	}))
	return srv, &hits
}

type slowReader struct {
	io.ReadSeeker
	delay time.Duration
}

func (s *slowReader) Read(p []byte) (int, error) {
	time.Sleep(s.delay)
	if len(p) > 64<<10 {
		p = p[:64<<10]
	}
	return s.ReadSeeker.Read(p)
}

func waitState(t *testing.T, m *Manager, id string, want State, timeout time.Duration) DownloadView {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		v, ok := m.Get(id)
		if !ok {
			t.Fatalf("download %s vanished", id)
		}
		if v.State == want {
			return v
		}
		if v.State == StateFailed && want != StateFailed {
			t.Fatalf("download failed: %s", v.Error)
		}
		time.Sleep(20 * time.Millisecond)
	}
	v, _ := m.Get(id)
	t.Fatalf("timed out waiting for %s, state %s (%d/%d)", want, v.State, v.Downloaded, v.Size)
	return v
}

func TestMultiConnectionDownload(t *testing.T) {
	m, dlDir := testManager(t)
	content := randomBytes(12<<20 + 12345)
	srv, hits := rangeServer(content, "big file.bin", 0)
	defer srv.Close()

	v, err := m.Add(AddRequest{URL: srv.URL + "/x", Connections: 8})
	if err != nil {
		t.Fatal(err)
	}
	v = waitState(t, m, v.ID, StateCompleted, 20*time.Second)
	if v.Filename != "big file.bin" {
		t.Errorf("filename from Content-Disposition: got %q", v.Filename)
	}
	if !v.Resumable || v.Size != int64(len(content)) {
		t.Errorf("resumable=%v size=%d", v.Resumable, v.Size)
	}
	got, err := os.ReadFile(filepath.Join(dlDir, "big file.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatal("downloaded content differs")
	}
	if hits.Load() < 8 {
		t.Errorf("expected >= 8 requests (one per connection), got %d", hits.Load())
	}
	if _, err := os.Stat(filepath.Join(dlDir, "big file.bin"+partSuffix)); !os.IsNotExist(err) {
		t.Error(".part file left behind")
	}
}

func TestPauseResumeKeepsProgress(t *testing.T) {
	m, dlDir := testManager(t)
	content := randomBytes(6 << 20)
	srv, _ := rangeServer(content, "p.bin", 2*time.Millisecond)
	defer srv.Close()

	v, _ := m.Add(AddRequest{URL: srv.URL + "/p.bin", Connections: 4})
	waitState(t, m, v.ID, StateDownloading, 5*time.Second)
	// Let some bytes land.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cur, _ := m.Get(v.ID); cur.Downloaded > 256<<10 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := m.Pause(v.ID); err != nil {
		t.Fatal(err)
	}
	paused := waitState(t, m, v.ID, StatePaused, 5*time.Second)
	time.Sleep(100 * time.Millisecond)
	paused, _ = m.Get(v.ID)
	if paused.Downloaded == 0 || paused.Downloaded >= int64(len(content)) {
		t.Fatalf("unexpected paused progress %d", paused.Downloaded)
	}
	if err := m.Resume(v.ID); err != nil {
		t.Fatal(err)
	}
	waitState(t, m, v.ID, StateCompleted, 30*time.Second)
	got, _ := os.ReadFile(filepath.Join(dlDir, "p.bin"))
	if !bytes.Equal(got, content) {
		t.Fatal("content differs after pause/resume")
	}
}

func TestNoRangeSupportFallsBackToSingleConnection(t *testing.T) {
	m, dlDir := testManager(t)
	content := randomBytes(3<<20 + 7)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Ignores Range entirely, like plenty of dynamic endpoints.
		w.Header().Set("Content-Type", "application/zip")
		w.Write(content)
	}))
	defer srv.Close()

	v, _ := m.Add(AddRequest{URL: srv.URL + "/get?id=5", Connections: 8})
	v = waitState(t, m, v.ID, StateCompleted, 10*time.Second)
	if v.Resumable {
		t.Error("should not be resumable")
	}
	if v.Filename != "get.zip" {
		t.Errorf("filename: %q", v.Filename)
	}
	got, _ := os.ReadFile(filepath.Join(dlDir, v.Filename))
	if !bytes.Equal(got, content) {
		t.Fatal("content differs")
	}
}

func TestDynamicSplitUsesIdleConnections(t *testing.T) {
	// 2 initial segments, but 4 connections: once the first two claim
	// their segments, the other two must split the remaining work.
	d := &Download{Resumable: true, Connections: 4, Size: 8 << 20}
	d.Segments = planSegments(8<<20, true, 2)
	m := &Manager{}
	var claimed []*Segment
	for i := 0; i < 4; i++ {
		if s := m.claimNextLocked(d); s != nil {
			claimed = append(claimed, s)
		}
	}
	if len(claimed) != 4 || len(d.Segments) != 4 {
		t.Fatalf("claimed %d segments, total %d", len(claimed), len(d.Segments))
	}
	var covered int64
	for i, s := range d.Segments {
		if i > 0 && s.Start != d.Segments[i-1].End+1 {
			t.Fatalf("gap/overlap at %d: %+v after %+v", i, s, d.Segments[i-1])
		}
		covered += s.End - s.Start + 1
	}
	if covered != 8<<20 {
		t.Fatalf("segments cover %d bytes", covered)
	}
}

func TestExpiredLinkFailsFastAndRefreshWorks(t *testing.T) {
	m, dlDir := testManager(t)
	content := randomBytes(2 << 20)
	good, _ := rangeServer(content, "r.bin", 0)
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer bad.Close()

	v, _ := m.Add(AddRequest{URL: bad.URL + "/r.bin"})
	v = waitState(t, m, v.ID, StateFailed, 10*time.Second)
	if !strings.Contains(v.Error, "403") {
		t.Errorf("error: %q", v.Error)
	}
	newURL := good.URL + "/r.bin"
	if _, err := m.Update(v.ID, UpdateRequest{URL: &newURL}); err != nil {
		t.Fatal(err)
	}
	_ = m.Resume(v.ID)
	v = waitState(t, m, v.ID, StateCompleted, 10*time.Second)
	got, _ := os.ReadFile(filepath.Join(dlDir, v.Filename))
	if !bytes.Equal(got, content) {
		t.Fatal("content differs")
	}
}

func TestStatePersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	st := NewSettingsStore(dir, dir)
	m := NewManager(dir, st, newTransport(true))
	f := false
	v, err := m.Add(AddRequest{URL: "https://example.com/a.iso", Start: &f})
	if err != nil {
		t.Fatal(err)
	}
	m.save()
	m2 := NewManager(dir, st, newTransport(true))
	got, ok := m2.Get(v.ID)
	if !ok || got.State != StatePaused || got.Filename != "a.iso" {
		t.Fatalf("reloaded: %+v ok=%v", got, ok)
	}
}

func TestUniqueFilenames(t *testing.T) {
	m, dlDir := testManager(t)
	_ = os.MkdirAll(dlDir, 0o755)
	_ = os.WriteFile(filepath.Join(dlDir, "a.tar.gz"), []byte("x"), 0o644)
	f := false
	v, _ := m.Add(AddRequest{URL: "https://example.com/a.tar.gz", Start: &f})
	if v.Filename != "a (1).tar.gz" {
		t.Fatalf("got %q", v.Filename)
	}
	v2, _ := m.Add(AddRequest{URL: "https://example.com/a.tar.gz", Start: &f})
	if v2.Filename != "a (2).tar.gz" {
		t.Fatalf("got %q", v2.Filename)
	}
}

func TestSanitizeFilename(t *testing.T) {
	cases := map[string]string{
		"../../etc/passwd": "_.._etc_passwd",
		"  ok.txt ":        "ok.txt",
		"a\x00b\nc":        "abc",
		"...":              "",
	}
	for in, want := range cases {
		if got := sanitizeFilename(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestForbiddenDestinations(t *testing.T) {
	m := &Manager{client: &http.Client{Transport: newTransport(false)}, settings: NewSettingsStore(t.TempDir(), "/tmp")}
	_, err := m.Probe(context.Background(), "http://127.0.0.1:28642/downloads", nil)
	if err == nil || !strings.Contains(err.Error(), errForbiddenDestination.Error()) {
		t.Fatalf("loopback probe should be refused, got %v", err)
	}
	_, err = m.Probe(context.Background(), "http://localhost:1/", nil)
	if err == nil || !strings.Contains(err.Error(), errForbiddenDestination.Error()) {
		t.Fatalf("localhost probe should be refused, got %v", err)
	}
}

func TestParseContentRange(t *testing.T) {
	s, e, total, ok := parseContentRange("bytes 100-199/1000")
	if !ok || s != 100 || e != 199 || total != 1000 {
		t.Fatal(s, e, total, ok)
	}
	if _, _, total, ok := parseContentRange("bytes 0-9/*"); !ok || total != -1 {
		t.Fatal("unknown total")
	}
}

func TestStalledConnectionIsRetried(t *testing.T) {
	old := stallTimeout
	stallTimeout = 300 * time.Millisecond
	defer func() { stallTimeout = old }()
	m, dlDir := testManager(t)
	content := randomBytes(3 << 20)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			// First connection: send a little, then go silent.
			w.Header().Set("Content-Length", "3145728")
			w.Header().Set("Content-Range", "bytes 0-3145727/3145728")
			w.WriteHeader(http.StatusPartialContent)
			w.Write(content[:1000])
			w.(http.Flusher).Flush()
			<-r.Context().Done()
			return
		}
		http.ServeContent(w, r, "s.bin", time.Time{}, bytes.NewReader(content))
	}))
	defer srv.Close()
	v, _ := m.Add(AddRequest{URL: srv.URL + "/s.bin", Connections: 1})
	v = waitState(t, m, v.ID, StateCompleted, 15*time.Second)
	got, _ := os.ReadFile(filepath.Join(dlDir, v.Filename))
	if !bytes.Equal(got, content) {
		t.Fatal("content differs after stall retry")
	}
}

func TestTooManyConnectionsDropsExtraConnections(t *testing.T) {
	m, dlDir := testManager(t)
	content := randomBytes(8 << 20)
	var inflight atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Allows 2 concurrent connections, like a strict mirror.
		if inflight.Add(1) > 2 {
			inflight.Add(-1)
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		defer inflight.Add(-1)
		http.ServeContent(w, r, "t.bin", time.Time{}, &slowReader{ReadSeeker: bytes.NewReader(content), delay: time.Millisecond})
	}))
	defer srv.Close()
	v, _ := m.Add(AddRequest{URL: srv.URL + "/t.bin", Connections: 8})
	v = waitState(t, m, v.ID, StateCompleted, 30*time.Second)
	got, _ := os.ReadFile(filepath.Join(dlDir, v.Filename))
	if !bytes.Equal(got, content) {
		t.Fatal("content differs")
	}
}
