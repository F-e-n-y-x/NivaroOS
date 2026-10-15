//go:build e2e

// End-to-end Files tests: the real nivaroos core binary on a throwaway
// loopback port, with its own config, database, runtime dir and data
// volumes. Run with services/core/e2e/run.sh (see README.md there).
//
// Isolation: the suite re-runs itself inside a private mount namespace
// (unshare -m, needs root) and masks /etc/nivaroos, /var/lib/nivaroos and
// /tmp with empty tmpfs mounts there, so the test core can't read or
// write the live server's state (companion devices, trash index, secret
// key, upload staging, /DATA/Companion). It also runs in its own network
// namespace (unshare -n): only loopback, plus test-only LAN and Tailscale
// addresses on it for the fake companion phones (companion_test.go). The
// live server never sees any of these mounts or addresses.
// Without root the suite skips.
package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

var (
	work    string // everything the suite creates lives under here
	diskVol string // own mount point on disk (big files)
	ramVol  string // tmpfs: a second volume (cross-device moves)
	fullVol string // tiny tmpfs: full-disk errors
	coreBin string
	core    *coreProc
	bus     = &fakeBus{}
	skipAll string
)

func TestMain(m *testing.M) {
	if os.Getenv("NV_E2E_NS") == "" {
		if os.Getuid() != 0 {
			fmt.Println("files e2e: needs root (private mount namespace) - skipping")
			os.Exit(0)
		}
		cmd := exec.Command("unshare", append([]string{"--mount", "--net", "--propagation", "private", "--", os.Args[0]}, os.Args[1:]...)...)
		cmd.Env = append(os.Environ(), "NV_E2E_NS=1")
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				os.Exit(ee.ExitCode())
			}
			fmt.Println("unshare:", err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	code := 1
	if err := setup(); err != nil {
		fmt.Println("files e2e setup:", err)
	} else {
		code = m.Run()
	}
	teardown()
	os.Exit(code)
}

func mountTmpfs(dir, size string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return syscall.Mount("tmpfs", dir, "tmpfs", 0, "size="+size)
}

func setup() error {
	base := os.Getenv("NV_E2E_DIR")
	if base == "" {
		wd, _ := os.Getwd()
		base = filepath.Join(wd, ".work") // on disk, gitignored
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return err
	}
	var err error
	if work, err = os.MkdirTemp(base, "run-"); err != nil {
		return err
	}
	diskVol = filepath.Join(work, "disk")
	ramVol = filepath.Join(work, "ram")
	fullVol = filepath.Join(work, "full")
	_ = os.MkdirAll(diskVol, 0o755)
	if err := syscall.Mount(diskVol, diskVol, "", syscall.MS_BIND, ""); err != nil {
		return fmt.Errorf("bind %s: %w", diskVol, err)
	}
	if err := mountTmpfs(ramVol, "1g"); err != nil {
		return err
	}
	if err := mountTmpfs(fullVol, "4m"); err != nil {
		return err
	}

	coreBin = os.Getenv("NV_E2E_CORE_BIN")
	if coreBin == "" {
		coreBin = filepath.Join(work, "nivaroos-core")
		build := exec.Command("go", "build", "-o", coreBin, "..")
		build.Stdout, build.Stderr = os.Stdout, os.Stderr
		if err := build.Run(); err != nil {
			return fmt.Errorf("building core: %w", err)
		}
	}

	// Live-server state, masked inside this namespace only. /DATA only if
	// it exists (creating it would touch the host); without it companion
	// folders go under /var/lib/nivaroos.
	masks := []string{"/etc/nivaroos", "/var/lib/nivaroos", "/tmp"}
	if _, err := os.Stat("/DATA"); err == nil {
		masks = append(masks, "/DATA")
	}
	for _, d := range masks {
		if err := mountTmpfs(d, "64m"); err != nil {
			return fmt.Errorf("masking %s: %w", d, err)
		}
	}
	// The private network namespace: loopback up, plus the fake phones'
	// LAN and Tailscale addresses (the core refuses to dial loopback).
	for _, args := range [][]string{{"link", "set", "lo", "up"}, {"addr", "add", phoneLAN + "/32", "dev", "lo"}, {"addr", "add", phoneTailnet + "/32", "dev", "lo"}} {
		if out, err := exec.Command("ip", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("ip %v: %v %s", args, err, out)
		}
	}

	srv := httptest.NewServer(bus)
	for _, d := range []string{"run", "db", "log"} {
		_ = os.MkdirAll(filepath.Join(work, d), 0o755)
	}
	for _, f := range []string{"management.url", "message-bus.url"} {
		if err := os.WriteFile(filepath.Join(work, "run", f), []byte(srv.URL), 0o644); err != nil {
			return err
		}
	}
	conf := fmt.Sprintf("[app]\nLogPath = %[1]s/log/\nLogSaveName = core\nLogFileExt = log\nDBPath = %[1]s/db\nShellPath = %[1]s/shell\nUserDataPath = %[1]s/db/conf\n\n[server]\nRunMode = release\n\n[system]\n\n[common]\nRuntimePath = %[1]s/run\n", work)
	if err := os.WriteFile(filepath.Join(work, "core.conf"), []byte(conf), 0o644); err != nil {
		return err
	}
	core = &coreProc{}
	return core.start()
}

func teardown() {
	if core != nil {
		core.stop(syscall.SIGTERM)
	}
	if work != "" && os.Getenv("NV_E2E_KEEP") == "" {
		for _, d := range []string{diskVol, ramVol, fullVol} {
			_ = syscall.Unmount(d, syscall.MNT_DETACH)
		}
		_ = os.RemoveAll(work)
	}
}

// ---- the core process ----

type coreProc struct {
	cmd  *exec.Cmd
	base string
}

func (c *coreProc) start() error {
	urlFile := filepath.Join(work, "run", "nivaroos.url")
	_ = os.Remove(urlFile)
	logf, err := os.OpenFile(filepath.Join(work, "core.out"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	c.cmd = exec.Command(coreBin, "-c", filepath.Join(work, "core.conf"))
	c.cmd.Dir = work
	c.cmd.Stdout, c.cmd.Stderr = logf, logf
	if err := c.cmd.Start(); err != nil {
		return err
	}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if raw, err := os.ReadFile(urlFile); err == nil && len(raw) > 0 {
			c.base = strings.TrimSpace(string(raw))
			if resp, err := http.Get(c.base + "/ping"); err == nil {
				resp.Body.Close()
				if resp.StatusCode == 200 {
					return nil
				}
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("core didn't come up, see %s/core.out", work)
}

func (c *coreProc) stop(sig syscall.Signal) {
	if c.cmd == nil || c.cmd.Process == nil {
		return
	}
	_ = c.cmd.Process.Signal(sig)
	done := make(chan struct{})
	go func() { _ = c.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		_ = c.cmd.Process.Kill()
		<-done
	}
	c.cmd = nil
}

// restart simulates a crash (SIGKILL) or a clean service restart.
func restartCore(t *testing.T, sig syscall.Signal) {
	t.Helper()
	core.stop(sig)
	if err := core.start(); err != nil {
		t.Fatal(err)
	}
}

// ---- fake gateway + message bus: accepts everything, records events ----

type fakeBus struct {
	mu     sync.Mutex
	events [][]jobEvent // each nivaroos:file:operate publish, in order
}

func (b *fakeBus) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/v1/gateway/routes") {
		w.WriteHeader(http.StatusCreated)
		return
	}
	if p, _ := url.PathUnescape(r.URL.Path); strings.HasSuffix(p, "/nivaroos:file:operate") {
		var props map[string]string
		var model struct {
			Data []jobEvent `json:"data"`
		}
		if json.Unmarshal(body, &props) == nil && json.Unmarshal([]byte(props["file_operate"]), &model) == nil {
			b.mu.Lock()
			b.events = append(b.events, model.Data)
			b.mu.Unlock()
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{}`))
}

// eventsFor returns every published snapshot of one job, in order.
func (b *fakeBus) eventsFor(id string) []jobEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []jobEvent
	for _, list := range b.events {
		for _, e := range list {
			if e.ID == id {
				out = append(out, e)
			}
		}
	}
	return out
}

// jobEvent is what the UI reads (legacy + engine fields).
type jobEvent struct {
	ID            string `json:"id"`
	State         string `json:"state"`
	Status        string `json:"status"`
	Finished      bool   `json:"finished"`
	Cancelled     bool   `json:"cancelled"`
	ProcessedSize int64  `json:"processed_size"`
	TotalSize     int64  `json:"total_size"`
	FilesTotal    int    `json:"files_total"`
	FilesDone     int    `json:"files_done"`
	FilesFailed   int    `json:"files_failed"`
	FilesSkipped  int    `json:"files_skipped"`
	BytesTotal    int64  `json:"bytes_total"`
	BytesDone     int64  `json:"bytes_done"`
	Error         string `json:"error"`
	Failures      []struct {
		Path  string `json:"path"`
		Error string `json:"error"`
	} `json:"failures"`
}

// ---- API helpers ----

type result struct {
	Success int             `json:"success"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
	status  int
}

func call(t *testing.T, method, path string, body interface{}) result {
	t.Helper()
	var rd io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rd = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, core.base+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var r result
	_ = json.Unmarshal(raw, &r)
	r.status = resp.StatusCode
	return r
}

// submit queues a copy/move through the same endpoint the Files app uses.
func submit(t *testing.T, kind string, sources []string, dest, style string) string {
	t.Helper()
	items := []map[string]string{}
	for _, s := range sources {
		items = append(items, map[string]string{"from": s})
	}
	r := call(t, "POST", "/v1/batch/task", map[string]interface{}{"type": kind, "item": items, "to": dest, "style": style})
	var j jobEvent
	if r.Success != 200 || json.Unmarshal(r.Data, &j) != nil || j.ID == "" {
		t.Fatalf("submit %s %v -> %s: %d %s", kind, sources, dest, r.Success, r.Message)
	}
	return j.ID
}

func listJobs(t *testing.T) []jobEvent {
	t.Helper()
	r := call(t, "GET", "/v1/batch/tasks", nil)
	var jobs []jobEvent
	if err := json.Unmarshal(r.Data, &jobs); err != nil {
		t.Fatalf("tasks: %v (%s)", err, r.Data)
	}
	return jobs
}

func getJob(t *testing.T, id string) (jobEvent, bool) {
	for _, j := range listJobs(t) {
		if j.ID == id {
			return j, true
		}
	}
	return jobEvent{}, false
}

var terminal = map[string]bool{"done": true, "done_with_errors": true, "failed": true, "cancelled": true, "interrupted": true}

// waitJob waits for a terminal state - a job that never gets there is the
// "stuck in progress" bug - then checks that what the UI was told agrees.
func waitJob(t *testing.T, id string, timeout time.Duration) jobEvent {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		j, ok := getJob(t, id)
		if !ok {
			t.Fatalf("job %s vanished from /v1/batch/tasks", id)
		}
		if terminal[j.State] {
			checkReported(t, j)
			return j
		}
		if time.Now().After(deadline) {
			t.Fatalf("job %s stuck in %q after %s: %+v", id, j.State, timeout, j)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// checkReported: the published events (what the UI renders) end in the
// same terminal state as the API, progress never goes backwards or past
// the total, and the counters add up.
func checkReported(t *testing.T, j jobEvent) {
	t.Helper()
	if !j.Finished {
		t.Errorf("job %s: state %s but finished=false", j.ID, j.State)
	}
	if j.State != "cancelled" && j.State != "failed" && j.State != "interrupted" {
		if got := j.FilesDone + j.FilesFailed + j.FilesSkipped; got < j.FilesTotal {
			t.Errorf("job %s %s: done %d + failed %d + skipped %d < total %d", j.ID, j.State, j.FilesDone, j.FilesFailed, j.FilesSkipped, j.FilesTotal)
		}
		if j.BytesDone != j.BytesTotal {
			t.Errorf("job %s %s: bytes %d of %d", j.ID, j.State, j.BytesDone, j.BytesTotal)
		}
	}
	if (j.State == "done" && j.FilesFailed > 0) || (j.State == "done_with_errors" && j.FilesFailed == 0) {
		t.Errorf("job %s: state %s with %d failures", j.ID, j.State, j.FilesFailed)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		evs := bus.eventsFor(j.ID)
		if len(evs) > 0 && evs[len(evs)-1].State == j.State {
			last := evs[len(evs)-1]
			if !last.Finished || last.Status == "PROCESSING" || last.Status == "CALCULATING" {
				t.Errorf("job %s: last event not terminal: %+v", j.ID, last)
			}
			var prev int64
			for _, e := range evs {
				if e.BytesDone < prev {
					t.Errorf("job %s: progress went backwards %d -> %d", j.ID, prev, e.BytesDone)
				}
				if e.BytesTotal > 0 && e.BytesDone > e.BytesTotal {
					t.Errorf("job %s: progress past total %d/%d", j.ID, e.BytesDone, e.BytesTotal)
				}
				prev = e.BytesDone
			}
			return
		}
		if time.Now().After(deadline) {
			state := "<none>"
			if len(evs) > 0 {
				state = evs[len(evs)-1].State
			}
			t.Errorf("job %s: API says %s but the last event the UI got says %s", j.ID, j.State, state)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}
