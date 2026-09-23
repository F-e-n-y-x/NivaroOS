// Package aptjob runs apt operations (install, remove, upgrade, update) as
// background jobs.
//
// They used to run inside the HTTP request with a context timeout: the web
// UI gave up after 60 s ("Install failed" while apt was still working), the
// backend SIGKILLed apt-get at 5-10 minutes (leaving dpkg half-configured),
// a restart of core killed dpkg with it (same cgroup), and nothing stopped
// two operations from colliding on the dpkg lock.
//
// Now: one job at a time; each runs in its own systemd unit (outside core's
// cgroup, never killed on a timeout) and writes its output and exit code to
// files, so the job and its log survive a core restart and a page reload.
package aptjob

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type State string

const (
	StateRunning     State = "running"
	StateDone        State = "done"
	StateFailed      State = "failed"
	StateInterrupted State = "interrupted"
)

var ErrBusy = errors.New("another package operation is already running")

// Job is one apt operation.
type Job struct {
	ID         string     `json:"id"`
	Kind       string     `json:"kind"` // install, remove, purge, upgrade, upgrade-all, update
	Packages   []string   `json:"packages"`
	State      State      `json:"state"`
	ExitCode   int        `json:"exit_code"`
	StartedAt  time.Time  `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	Logs       []string   `json:"logs,omitempty"`
}

type Options struct {
	Dir string // job metadata, logs and exit codes
	// Launch starts argv detached, appending its output to logPath and
	// writing its exit code to exitPath when it ends. Default: systemd-run.
	Launch func(id string, argv []string, logPath, exitPath string) error
	// Alive reports whether the job's process is still there. Default:
	// systemctl is-active on its unit.
	Alive func(id string) bool
}

type Runner struct {
	mu   sync.Mutex
	opts Options
	cur  *Job // the latest job (running or finished)
}

func New(opts Options) *Runner {
	if opts.Launch == nil {
		opts.Launch = systemdLaunch
	}
	if opts.Alive == nil {
		opts.Alive = systemdAlive
	}
	_ = os.MkdirAll(opts.Dir, 0o755)
	r := &Runner{opts: opts}
	if raw, err := os.ReadFile(r.metaPath()); err == nil {
		var j Job
		if json.Unmarshal(raw, &j) == nil && j.ID != "" {
			r.cur = &j
		}
	}
	return r
}

func (r *Runner) metaPath() string          { return filepath.Join(r.opts.Dir, "current.json") }
func (r *Runner) logPath(id string) string  { return filepath.Join(r.opts.Dir, id+".log") }
func (r *Runner) exitPath(id string) string { return filepath.Join(r.opts.Dir, id+".exit") }

// Status returns the latest job with the tail of its log. A zero Job (empty
// ID) means no package operation has run yet.
func (r *Runner) Status() Job {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refreshLocked()
	if r.cur == nil {
		return Job{}
	}
	j := *r.cur
	j.Logs = readTail(r.logPath(j.ID), 3000)
	return j
}

// refreshLocked moves a running job to its final state once it ended.
func (r *Runner) refreshLocked() {
	j := r.cur
	if j == nil || j.State != StateRunning {
		return
	}
	code, ok := readExit(r.exitPath(j.ID))
	if !ok {
		if r.opts.Alive(j.ID) {
			return
		}
		// Gone without an exit code - but it may have finished between the
		// two checks.
		if code, ok = readExit(r.exitPath(j.ID)); !ok {
			now := time.Now()
			j.State, j.ExitCode, j.FinishedAt = StateInterrupted, -1, &now
			r.saveLocked()
			return
		}
	}
	now := time.Now()
	j.ExitCode, j.FinishedAt = code, &now
	j.State = StateDone
	if code != 0 {
		j.State = StateFailed
	}
	r.saveLocked()
}

func (r *Runner) saveLocked() {
	raw, _ := json.Marshal(r.cur)
	tmp := r.metaPath() + ".tmp"
	if os.WriteFile(tmp, raw, 0o644) == nil {
		_ = os.Rename(tmp, r.metaPath())
	}
}

func (r *Runner) start(kind string, pkgs []string, argv []string) (Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refreshLocked()
	if r.cur != nil && r.cur.State == StateRunning {
		return *r.cur, ErrBusy
	}
	j := &Job{ID: newID(), Kind: kind, Packages: pkgs, State: StateRunning, StartedAt: time.Now()}
	header := fmt.Sprintf("[%s] $ %s\n", j.StartedAt.Format("15:04:05"), strings.Join(argv, " "))
	if err := os.WriteFile(r.logPath(j.ID), []byte(header), 0o644); err != nil {
		return Job{}, err
	}
	r.cur = j
	r.saveLocked()
	if err := r.opts.Launch(j.ID, argv, r.logPath(j.ID), r.exitPath(j.ID)); err != nil {
		now := time.Now()
		j.State, j.ExitCode, j.FinishedAt = StateFailed, -1, &now
		appendLog(r.logPath(j.ID), "could not start: "+err.Error())
		r.saveLocked()
		return *j, err
	}
	r.pruneOldLogsLocked(10)
	return *j, nil
}

// ---- operations ----

var validPackage = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]*(:[a-z0-9]+)?$`)

func validate(pkgs []string) error {
	if len(pkgs) == 0 {
		return errors.New("no packages specified")
	}
	for _, p := range pkgs {
		if !validPackage.MatchString(p) {
			return fmt.Errorf("invalid package name: %q", p)
		}
	}
	return nil
}

// aptGet builds a non-interactive apt-get argv. Config-file prompts keep the
// local version (an unattended upgrade must never hang on a question).
func aptGet(args ...string) []string {
	return append([]string{"env", "DEBIAN_FRONTEND=noninteractive", "LC_ALL=C", "NEEDRESTART_MODE=a",
		"apt-get", "-o", "Dpkg::Options::=--force-confdef", "-o", "Dpkg::Options::=--force-confold"}, args...)
}

func (r *Runner) Install(pkgs []string, reinstall bool) (Job, error) {
	if err := validate(pkgs); err != nil {
		return Job{}, err
	}
	args := []string{"install", "-y"}
	if reinstall {
		args = append(args, "--reinstall")
	}
	return r.start("install", pkgs, aptGet(append(append(args, "--"), pkgs...)...))
}

func (r *Runner) Remove(pkgs []string, purge bool) (Job, error) {
	if err := validate(pkgs); err != nil {
		return Job{}, err
	}
	verb := "remove"
	if purge {
		verb = "purge"
	}
	return r.start(verb, pkgs, aptGet(append([]string{verb, "-y", "--"}, pkgs...)...))
}

func (r *Runner) Upgrade(pkgs []string) (Job, error) {
	if err := validate(pkgs); err != nil {
		return Job{}, err
	}
	return r.start("upgrade", pkgs, aptGet(append([]string{"install", "-y", "--only-upgrade", "--"}, pkgs...)...))
}

// UpgradeAll is the one "upgrade everything" (dist-upgrade, so held-back
// packages whose dependencies changed are upgraded too).
func (r *Runner) UpgradeAll() (Job, error) {
	return r.start("upgrade-all", nil, aptGet("dist-upgrade", "-y"))
}

func (r *Runner) Update() (Job, error) {
	return r.start("update", nil, aptGet("update"))
}

// ---- launching ----

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// wrapper is the shell line a job runs: argv (quoted - never interpreted),
// output appended to the log, exit code written atomically at the end.
func wrapper(argv []string, logPath, exitPath string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		q[i] = shellQuote(a)
	}
	return fmt.Sprintf("exec >>%s 2>&1 </dev/null; %s; c=$?; echo $c > %s.tmp && mv %s.tmp %s",
		shellQuote(logPath), strings.Join(q, " "), shellQuote(exitPath), shellQuote(exitPath), shellQuote(exitPath))
}

func unitName(id string) string { return "nivaroos-apt-" + id }

func systemdLaunch(id string, argv []string, logPath, exitPath string) error {
	out, err := exec.Command("systemd-run", "--unit="+unitName(id), "--collect", "--quiet",
		"--description=NivaroOS package operation", "--", "sh", "-c", wrapper(argv, logPath, exitPath)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func systemdAlive(id string) bool {
	return exec.Command("systemctl", "is-active", "--quiet", unitName(id)).Run() == nil
}

// ---- files ----

func newID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(b)
}

func readExit(path string) (int, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	code, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil {
		return 0, false
	}
	return code, true
}

func appendLog(path, line string) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintln(f, line)
}

// readTail returns up to n last lines of the log (reading at most 512 KB).
func readTail(path string, n int) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	const max = 512 << 10
	size, _ := f.Seek(0, io.SeekEnd)
	start := size - max
	if start < 0 {
		start = 0
	}
	buf := make([]byte, size-start)
	if _, err := f.ReadAt(buf, start); err != nil && err != io.EOF {
		return nil
	}
	lines := strings.Split(strings.TrimRight(strings.ReplaceAll(string(buf), "\r", ""), "\n"), "\n")
	if start > 0 && len(lines) > 1 {
		lines = lines[1:] // first line is partial
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

func (r *Runner) pruneOldLogsLocked(keep int) {
	entries, err := os.ReadDir(r.opts.Dir)
	if err != nil {
		return
	}
	var ids []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".log") {
			ids = append(ids, strings.TrimSuffix(e.Name(), ".log"))
		}
	}
	// IDs start with a timestamp, so lexical order is age order.
	for i := 0; i < len(ids)-keep; i++ {
		if r.cur != nil && ids[i] == r.cur.ID {
			continue
		}
		os.Remove(r.logPath(ids[i]))
		os.Remove(r.exitPath(ids[i]))
	}
}
