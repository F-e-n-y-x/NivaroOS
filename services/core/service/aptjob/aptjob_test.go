package aptjob

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// shellLaunch runs the job's wrapper directly (no systemd) - the real
// runner uses systemd-run so a core restart can't kill dpkg, but the state
// handling is the same.
func shellLaunch(id string, argv []string, logPath, exitPath string) error {
	cmd := exec.Command("sh", "-c", wrapper(argv, logPath, exitPath))
	return cmd.Start()
}

func newTestRunner(t *testing.T, dir string) *Runner {
	t.Helper()
	// The unit counts as alive until its exit file appears (as systemd's
	// is-active would say for a real job).
	return New(Options{Dir: dir, Launch: shellLaunch, Alive: func(string) bool { return true }})
}

func waitFinished(t *testing.T, r *Runner) Job {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		j := r.Status()
		if j.State != StateRunning {
			return j
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("job still running: %+v", r.Status())
	return Job{}
}

func TestAJobReportsItsOutputAndExitCode(t *testing.T) {
	r := newTestRunner(t, t.TempDir())
	if _, err := r.start("install", []string{"demo"}, []string{"sh", "-c", "echo Unpacking demo; echo Setting up demo; exit 0"}); err != nil {
		t.Fatal(err)
	}
	j := waitFinished(t, r)
	if j.State != StateDone || j.ExitCode != 0 {
		t.Fatalf("state=%s exit=%d", j.State, j.ExitCode)
	}
	if !strings.Contains(strings.Join(j.Logs, "\n"), "Setting up demo") {
		t.Fatalf("logs = %q", j.Logs)
	}
}

func TestAFailingJobIsFailedWithItsCode(t *testing.T) {
	r := newTestRunner(t, t.TempDir())
	r.start("install", []string{"nope"}, []string{"sh", "-c", "echo 'E: Unable to locate package nope' >&2; exit 100"})
	j := waitFinished(t, r)
	if j.State != StateFailed || j.ExitCode != 100 {
		t.Fatalf("state=%s exit=%d", j.State, j.ExitCode)
	}
	if !strings.Contains(strings.Join(j.Logs, "\n"), "Unable to locate package") {
		t.Fatalf("stderr missing from logs: %q", j.Logs)
	}
}

// Two apt operations at once fail on the dpkg lock; the second is refused
// up front instead.
func TestOnlyOneJobRunsAtATime(t *testing.T) {
	r := newTestRunner(t, t.TempDir())
	if _, err := r.start("upgrade-all", nil, []string{"sh", "-c", "sleep 0.5"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.start("install", []string{"x"}, []string{"true"}); !errors.Is(err, ErrBusy) {
		t.Fatalf("second job: err = %v, want ErrBusy", err)
	}
	waitFinished(t, r)
	if _, err := r.start("install", []string{"x"}, []string{"true"}); err != nil {
		t.Fatalf("after the first finished: %v", err)
	}
}

// Restarting core used to lose the job (and kill dpkg with it). The job
// runs outside core; a new runner picks its state back up.
func TestAJobSurvivesARestartOfTheService(t *testing.T) {
	dir := t.TempDir()
	r := newTestRunner(t, dir)
	r.start("upgrade-all", nil, []string{"sh", "-c", "echo step1; sleep 0.4; echo step2; exit 0"})

	// "Restart": a fresh runner on the same directory while the job still
	// runs (liveness says the unit is alive until its exit file appears).
	r2 := New(Options{Dir: dir, Launch: shellLaunch, Alive: func(string) bool { return true }})
	if s := r2.Status(); s.State != StateRunning || s.Kind != "upgrade-all" {
		t.Fatalf("after restart: %+v", s)
	}
	j := waitFinished(t, r2)
	if j.State != StateDone || !strings.Contains(strings.Join(j.Logs, "\n"), "step2") {
		t.Fatalf("after restart the job ended as %+v", j)
	}
}

// If the job's process died without writing an exit code (power cut, OOM),
// it's "interrupted" - never shown as running forever or as success.
func TestAJobThatVanishedIsInterrupted(t *testing.T) {
	dir := t.TempDir()
	r := newTestRunner(t, dir)
	j, _ := r.start("install", []string{"x"}, []string{"sh", "-c", "sleep 30"})
	// Pretend the unit is gone and no exit file will ever appear.
	os.Remove(filepath.Join(dir, j.ID+".exit"))
	r2 := New(Options{Dir: dir, Launch: shellLaunch, Alive: func(string) bool { return false }})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && r2.Status().State == StateRunning {
		time.Sleep(20 * time.Millisecond)
	}
	if s := r2.Status(); s.State != StateInterrupted {
		t.Fatalf("state = %s, want interrupted", s.State)
	}
}

func TestPackageNamesCantBeFlagsOrShell(t *testing.T) {
	r := newTestRunner(t, t.TempDir())
	for _, bad := range []string{"-o", "--allow-unauthenticated", "a;rm -rf /", "$(id)", "A_Upper", ""} {
		if _, err := r.Install([]string{bad}, false); err == nil {
			t.Errorf("accepted package name %q", bad)
		}
	}
}

// Opt-in (NVOS_SYSTEMD_TEST=1, root): the real launcher runs the job in its
// own systemd unit and the runner follows it to the end.
func TestRealSystemdLaunch(t *testing.T) {
	if os.Getenv("NVOS_SYSTEMD_TEST") != "1" {
		t.Skip("set NVOS_SYSTEMD_TEST=1 to run against systemd")
	}
	r := New(Options{Dir: t.TempDir()})
	j, err := r.start("test", nil, []string{"sh", "-c", "echo from-systemd; sleep 0.3; exit 3"})
	if err != nil {
		t.Fatal(err)
	}
	if !systemdAlive(j.ID) {
		t.Log("unit already gone (fast) - fine if the exit file is there")
	}
	j = waitFinished(t, r)
	if j.State != StateFailed || j.ExitCode != 3 || !strings.Contains(strings.Join(j.Logs, "\n"), "from-systemd") {
		t.Fatalf("got %+v", j)
	}
}
