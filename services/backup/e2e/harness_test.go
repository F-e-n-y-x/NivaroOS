//go:build e2e

// Package e2e drives a throwaway nivaroos-backup (the real binary, the
// real rclone engine) over its REST API: temp data dir, temp rclone.conf
// with local remotes, a free loopback port. It never touches the live
// service, /DATA or a real cloud account. Run: services/backup/e2e/run.sh
package e2e

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/backup/engine"
	"github.com/F-e-n-y-x/NivaroOS/services/backup/jobs"
	"github.com/F-e-n-y-x/NivaroOS/services/common/localtoken"
)

var (
	binPath string
	baseDir string // on disk, under an allowed backup root (engine.DefaultAllowedRoots)
)

func TestMain(m *testing.M) {
	cache, err := os.UserCacheDir()
	if err != nil {
		panic(err)
	}
	if d := os.Getenv("NIVAROOS_BACKUP_E2E_DIR"); d != "" {
		cache = d
	}
	if err := os.MkdirAll(cache, 0o755); err != nil {
		panic(err)
	}
	baseDir, err = os.MkdirTemp(cache, "nivaroos-backup-e2e-")
	if err != nil {
		panic(err)
	}
	binPath = filepath.Join(baseDir, "nivaroos-backup")
	build := exec.Command("go", "build", "-o", binPath, "..")
	build.Stdout, build.Stderr = os.Stdout, os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "build:", err)
		os.Exit(1)
	}
	code := m.Run()
	if code == 0 || os.Getenv("NIVAROOS_BACKUP_E2E_KEEP") == "" {
		os.RemoveAll(baseDir)
	} else {
		fmt.Fprintln(os.Stderr, "kept", baseDir)
	}
	os.Exit(code)
}

// instance is one throwaway service: its own data dir, runtime dir and
// rclone.conf, on its own port.
type instance struct {
	t       *testing.T
	dir     string // everything of this test lives here
	dataDir string
	conf    string
	addr    string
	cmd     *exec.Cmd
	logf    *os.File
	env     []string
	args    []string // extra flags (later ones win)
}

// newInstance starts a service; env adds environment variables (e.g.
// RCLONE_BWLIMIT=1M to make a run slow enough to interrupt).
func newInstance(t *testing.T, env ...string) *instance {
	t.Helper()
	return newInstanceArgs(t, nil, env...)
}

// newInstanceArgs is newInstance with extra service flags.
func newInstanceArgs(t *testing.T, args []string, env ...string) *instance {
	t.Helper()
	dir, err := os.MkdirTemp(baseDir, strings.ReplaceAll(t.Name(), "/", "_")+"-")
	if err != nil {
		t.Fatal(err)
	}
	in := &instance{t: t, dir: dir, dataDir: filepath.Join(dir, "state"), conf: filepath.Join(dir, "rclone.conf"), env: env, args: args}
	for _, d := range []string{in.dataDir, filepath.Join(dir, "run")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A cloud remote that is really a local folder: the cloud code paths
	// (remote resolve, no mount watch) without a network.
	// The process runs in dir, so e2ecloud:x is dir/x.
	in.writeConf("[e2ecloud]\ntype = local\n")
	in.start()
	t.Cleanup(func() {
		in.stop()
		if !t.Failed() {
			os.RemoveAll(dir)
		}
	})
	return in
}

func (in *instance) writeConf(s string) {
	if err := os.WriteFile(in.conf, []byte(s), 0o600); err != nil {
		in.t.Fatal(err)
	}
}

func (in *instance) path(p ...string) string { return filepath.Join(append([]string{in.dir}, p...)...) }

func freePort(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

func (in *instance) start() {
	in.t.Helper()
	if in.addr == "" {
		in.addr = freePort(in.t)
	}
	lf, err := os.OpenFile(filepath.Join(in.dir, "service.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		in.t.Fatal(err)
	}
	in.logf = lf
	args := []string{"-addr", in.addr, "-data-dir", in.dataDir,
		"-runtime-path", filepath.Join(in.dir, "run"), "-core-db", filepath.Join(in.dir, "no-core.db"),
		"-rclone-config", in.conf, "-config-job=false"}
	in.cmd = exec.Command(binPath, append(args, in.args...)...)
	in.cmd.Stdout, in.cmd.Stderr = lf, lf
	in.cmd.Dir = in.dir
	in.cmd.Env = append(os.Environ(), "RCLONE_CONFIG="+in.conf)
	in.cmd.Env = append(in.cmd.Env, in.env...)
	if err := in.cmd.Start(); err != nil {
		in.t.Fatal(err)
	}
	started := time.Now()
	deadline := started.Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if resp, err := http.Get("http://" + in.addr + jobs.APIBase + "/health"); err == nil {
			resp.Body.Close()
			if d := time.Since(started); d > 5*time.Second {
				in.t.Logf("service took %s to come up", d.Round(time.Millisecond))
			}
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Dump the goroutines to the log to see where it hangs.
	_ = in.cmd.Process.Signal(syscall.SIGQUIT)
	_ = in.cmd.Wait()
	in.cmd = nil
	in.t.Fatalf("service did not come up; log:\n%s", tail(in.log(), 20000))
}

// kill is a crash: SIGKILL, no shutdown.
func (in *instance) kill() {
	if in.cmd == nil {
		return
	}
	_ = in.cmd.Process.Kill()
	_ = in.cmd.Wait()
	in.cmd = nil
	in.logf.Close()
}

// stop is systemctl stop: SIGTERM, then wait.
func (in *instance) stop() {
	if in.cmd == nil {
		return
	}
	_ = in.cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan struct{})
	go func() { _ = in.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		in.t.Errorf("service ignored SIGTERM for 30 s")
		_ = in.cmd.Process.Kill()
		<-done
	}
	in.cmd = nil
	in.logf.Close()
}

func (in *instance) log() string {
	b, _ := os.ReadFile(filepath.Join(in.dir, "service.log"))
	return string(b)
}

// call does one API request and decodes the envelope's data into out.
// It returns the HTTP status.
func (in *instance) call(method, path string, body, out interface{}) int {
	in.t.Helper()
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			in.t.Fatal(err)
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, "http://"+in.addr+jobs.APIBase+path, rd)
	if err != nil {
		in.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := in.do(req)
	if err != nil {
		in.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	raw, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(raw, &env); err != nil {
		in.t.Fatalf("%s %s: %d %s", method, path, resp.StatusCode, raw)
	}
	if out != nil && resp.StatusCode < 300 {
		if err := json.Unmarshal(env.Data, out); err != nil {
			in.t.Fatalf("%s %s: decode %s: %v", method, path, env.Data, err)
		}
	}
	if resp.StatusCode >= 300 {
		in.t.Logf("%s %s: %d %s", method, path, resp.StatusCode, env.Data)
	}
	return resp.StatusCode
}

// do sends req as same-host automation: with the local token where this
// host has one (a box with the strict marker no longer trusts bare
// loopback), which the throwaway instance checks the same way.
func (in *instance) do(req *http.Request) (*http.Response, error) {
	if tok := localtoken.Read(); tok != "" {
		req.Header.Set(localtoken.Header, tok)
	}
	return http.DefaultClient.Do(req)
}

// must is call that fails the test on a non-2xx answer.
func (in *instance) must(method, path string, body, out interface{}) {
	in.t.Helper()
	if st := in.call(method, path, body, out); st >= 300 {
		in.t.Fatalf("%s %s: HTTP %d", method, path, st)
	}
}

// endpoint resolves an absolute folder the way Files' "Back up this
// folder" does.
func (in *instance) endpoint(abs string) engine.Endpoint {
	in.t.Helper()
	var r engine.ResolvePathResult
	in.must("POST", "/locations/resolve-path", engine.ResolvePathRequest{Path: abs}, &r)
	if !r.OK || r.Endpoint == nil {
		in.t.Fatalf("resolve-path %s: %s", abs, r.Reason)
	}
	return *r.Endpoint
}

func cloud(sub string) engine.Endpoint {
	return engine.Endpoint{Kind: engine.EPCloud, RefID: "e2ecloud", SubPath: sub, Label: "e2e cloud"}
}

// job is a manual job of type typ from src to dest with test-friendly
// defaults (no preview, no guards in the way).
func job(name string, typ jobs.JobType, src, dest engine.Endpoint) jobs.Job {
	return jobs.Job{
		Name: name, Type: typ, Enabled: true,
		Sources: []engine.Endpoint{src}, Dest: dest, Triggers: []jobs.Trigger{},
		Conditions: jobs.Conditions{DestAvailable: true, WhenUnmet: jobs.UnmetFail},
		Guards:     engine.Guards{EmptySourcePct: 100, DeletePct: 100, ChangePct: 100, AllowEmptySrc: true},
		Retry:      jobs.Retry{Max: 1, BackoffSec: []int{10}},
		Notify:     jobs.NotifyPrefs{OnFailure: true},
		Hooks:      []jobs.Hook{},
		Filters:    engine.Filters{ExcludePresets: []string{}, Exclude: []string{}},
	}
}

func (in *instance) createJob(j jobs.Job) jobs.JobDetail {
	in.t.Helper()
	var out jobs.JobDetail
	in.must("POST", "/jobs", j, &out)
	return out
}

func (in *instance) runJob(id string) string {
	in.t.Helper()
	var r jobs.RunStarted
	in.must("POST", "/jobs/"+id+"/run", jobs.RunRequest{}, &r)
	return r.RunID
}

func (in *instance) getRun(id string) jobs.RunDetail {
	in.t.Helper()
	var r jobs.RunDetail
	in.must("GET", "/runs/"+id, nil, &r)
	return r
}

// waitRun waits for run id to reach a final state.
func (in *instance) waitRun(id string, max time.Duration) jobs.RunDetail {
	in.t.Helper()
	deadline := time.Now().Add(max)
	for {
		r := in.getRun(id)
		if r.Status.Final() {
			return r
		}
		if time.Now().After(deadline) {
			in.t.Fatalf("run %s still %s/%s after %s; log:\n%s", id, r.Status, r.Phase, max, tail(in.log(), 3000))
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// waitFor polls cond every 50 ms until it holds.
func (in *instance) waitFor(what string, max time.Duration, cond func() bool) {
	in.t.Helper()
	deadline := time.Now().Add(max)
	for !cond() {
		if time.Now().After(deadline) {
			in.t.Fatalf("timed out after %s waiting for %s", max, what)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func tail(s string, n int) string {
	if len(s) > n {
		return s[len(s)-n:]
	}
	return s
}

// ---------------------------------------------------------------------
// Test data

// writeFiles makes n files of size bytes each (random) under dir, spread
// over subfolders, and returns their relative paths.
func writeFiles(t *testing.T, dir string, n int, size int) []string {
	t.Helper()
	var out []string
	buf := make([]byte, size)
	for i := 0; i < n; i++ {
		rel := filepath.Join(fmt.Sprintf("d%03d", i%100), fmt.Sprintf("f%06d.bin", i))
		if _, err := rand.Read(buf); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(dir, rel), buf)
		out = append(out, rel)
	}
	return out
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeBig writes a size-byte random file in 1 MiB blocks.
func writeBig(t *testing.T, path string, size int64) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := io.CopyN(f, rand.Reader, size); err != nil {
		t.Fatal(err)
	}
}

// tree maps every regular file under dir (relative, slash separated) to
// its sha256, skipping Backup's own marker files.
func tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !d.Type().IsRegular() || strings.HasPrefix(d.Name(), ".nivaroos") {
			return nil
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		out[filepath.ToSlash(rel)] = hex.EncodeToString(h.Sum(nil))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// sameTree fails unless got holds exactly want's files with want's hashes.
func sameTree(t *testing.T, want, got map[string]string) {
	t.Helper()
	bad := 0
	for p, h := range want {
		if got[p] != h {
			if bad++; bad <= 10 {
				t.Errorf("%s: want %.12s, got %.12q", p, h, got[p])
			}
		}
	}
	for p := range got {
		if _, ok := want[p]; !ok {
			if bad++; bad <= 10 {
				t.Errorf("unexpected file %s", p)
			}
		}
	}
	if bad > 0 {
		t.Fatalf("%d differences (want %d files, got %d)", bad, len(want), len(got))
	}
}
