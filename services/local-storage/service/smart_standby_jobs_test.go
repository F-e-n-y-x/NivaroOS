package service

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/model"
)

// --- SMART: a sleeping drive is not unhealthy ---

func smartFromJSON(t *testing.T, s string) model.SmartctlA {
	t.Helper()
	var m model.SmartctlA
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestSleepingDriveKeepsLastHealth(t *testing.T) {
	asleep := smartFromJSON(t, `{"smartctl":{"exit_status":2,"messages":[{"string":"Device is in STANDBY mode, exit(2)","severity":"information"}]},"device":{"name":"/dev/sdb"}}`)
	if !IsSmartStandby(asleep) {
		t.Fatal("STANDBY not detected")
	}
	awake := smartFromJSON(t, `{"model_name":"WDC","smart_status":{"passed":true},"temperature":{"current":31}}`)
	if IsSmartStandby(awake) {
		t.Fatal("awake drive detected as asleep")
	}

	// never read awake: unknown, not unhealthy
	m := SmartForSleepingDrive(asleep, model.SmartctlA{}, false)
	if got := SmartHealth(m); got != "unknown" || !m.Sleeping {
		t.Fatalf("health = %q sleeping=%v", got, m.Sleeping)
	}
	// previously healthy: stays healthy
	m = SmartForSleepingDrive(asleep, awake, true)
	if got := SmartHealth(m); got != "true" || !m.Sleeping || m.Temperature.Current != 31 {
		t.Fatalf("health = %q (%+v)", got, m)
	}
	// previously failing: stays failing
	failing := awake
	failing.SmartStatus.Passed = false
	if got := SmartHealth(SmartForSleepingDrive(asleep, failing, true)); got != "false" {
		t.Fatalf("failing drive reported %q while asleep", got)
	}
	if SmartHealth(model.SmartctlA{}) != "true" || SmartHealth(failing) != "false" {
		t.Fatal("awake health mapping changed")
	}
}

// --- standby persistence (udev rule, any distro) ---

func TestUdevPropertiesAndRuleKey(t *testing.T) {
	props := ParseUdevProperties("DEVNAME=/dev/sda\nID_SERIAL=WDC_WD40EFRX-68N32N0_WD-WCC7K1234567\nID_SERIAL_SHORT=WD-WCC7K1234567\n")
	k, v, err := StandbyRuleKey(props)
	if err != nil || k != "ID_SERIAL" || v != "WDC_WD40EFRX-68N32N0_WD-WCC7K1234567" {
		t.Fatalf("%s %s %v", k, v, err)
	}
	k, _, err = StandbyRuleKey(map[string]string{"ID_SERIAL_SHORT": "ABC123"})
	if err != nil || k != "ID_SERIAL_SHORT" {
		t.Fatalf("fallback key %s %v", k, err)
	}
	for _, bad := range []string{`x", RUN+="/bin/sh`, "a\nb", "a*", "a b", "a?", "a[1]"} {
		if _, _, err := StandbyRuleKey(map[string]string{"ID_SERIAL": bad}); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if _, _, err := StandbyRuleKey(map[string]string{}); err == nil {
		t.Error("disk without serial accepted")
	}
}

func TestStandbyRuleRoundTrip(t *testing.T) {
	content, err := UpsertStandbyRule("", "ID_SERIAL", "DISK_A", "/usr/sbin/hdparm", 241)
	if err != nil {
		t.Fatal(err)
	}
	content, err = UpsertStandbyRule(content+"# admin comment\n", "ID_SERIAL", "DISK_B", "/usr/sbin/hdparm", 120)
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := ReadStandbyRuleCode(content, "ID_SERIAL", "DISK_A"); !ok || c != 241 {
		t.Fatalf("A = %d %v\n%s", c, ok, content)
	}
	if c, ok := ReadStandbyRuleCode(content, "ID_SERIAL", "DISK_B"); !ok || c != 120 {
		t.Fatalf("B = %d %v", c, ok)
	}
	// update A, then disable B
	content, _ = UpsertStandbyRule(content, "ID_SERIAL", "DISK_A", "/usr/sbin/hdparm", 60)
	content, _ = UpsertStandbyRule(content, "ID_SERIAL", "DISK_B", "/usr/sbin/hdparm", 0)
	if c, _ := ReadStandbyRuleCode(content, "ID_SERIAL", "DISK_A"); c != 60 {
		t.Fatalf("A not updated: %d", c)
	}
	if _, ok := ReadStandbyRuleCode(content, "ID_SERIAL", "DISK_B"); ok {
		t.Fatal("B not removed")
	}
	if strings.Count(content, "DISK_A") != 1 || !strings.Contains(content, "# admin comment") {
		t.Fatalf("unexpected content:\n%s", content)
	}
	if !strings.Contains(content, `ACTION=="add", SUBSYSTEM=="block", ENV{DEVTYPE}=="disk", ENV{ID_SERIAL}=="DISK_A", RUN+="/usr/sbin/hdparm -S 60 $devnode"`) {
		t.Fatalf("rule line format:\n%s", content)
	}
	for _, bad := range [][2]string{{"x\"y", "/usr/sbin/hdparm"}, {"OK", "hdparm"}, {"OK", "/usr/sbin/hdparm; reboot"}} {
		if _, err := UpsertStandbyRule("", "ID_SERIAL", bad[0], bad[1], 10); err == nil {
			t.Errorf("%q/%q accepted", bad[0], bad[1])
		}
	}
}

func TestLegacyHdparmConfSpindownRemovalKeepsOtherSettings(t *testing.T) {
	conf := filepath.Join(t.TempDir(), "hdparm.conf")
	os.WriteFile(conf, []byte("# comment\n/dev/disk/by-id/A {\n\tspindown_time = 241\n\tapm = 127\n}\n\n/dev/disk/by-id/B {\n\tspindown_time = 120\n}\n"), 0o644)
	if err := removeHdparmConfSpindown(conf, "/dev/disk/by-id/A", "/dev/disk/by-id/B", "/dev/sda"); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(conf)
	s := string(raw)
	if strings.Contains(s, "spindown_time") || !strings.Contains(s, "apm = 127") || !strings.Contains(s, "/dev/disk/by-id/A {") || strings.Contains(s, "/dev/disk/by-id/B") || !strings.Contains(s, "# comment") {
		t.Fatalf("result:\n%s", s)
	}
	missing := filepath.Join(t.TempDir(), "none.conf")
	if err := removeHdparmConfSpindown(missing, "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("hdparm.conf created on a distro that doesn't have one")
	}
}

// --- async storage jobs ---

type recorder struct {
	mu     sync.Mutex
	events []string
}

func (r *recorder) publish(event string, j StorageJob) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event+":"+j.State+":"+j.Step)
}

func (r *recorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

func waitJob(t *testing.T, s *JobStore, id string) StorageJob {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if j, ok := s.Get(id); ok && j.State != JobStateRunning {
			return j
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return StorageJob{}
}

func TestStorageJobLifecycle(t *testing.T) {
	r := &recorder{}
	s := NewJobStore(r.publish)
	release := make(chan struct{})
	finished := false
	job := s.Start("create", "/dev/sdb", func(progress func(string)) (string, error) {
		progress("partitioning")
		<-release
		progress("mounting")
		return "/mnt/Storage_sdb1", nil
	}, func() { finished = true })

	if job.ID == "" || job.State != JobStateRunning {
		t.Fatalf("start = %+v", job)
	}
	if j, ok := s.Get(job.ID); !ok || j.State != JobStateRunning {
		t.Fatalf("running job = %+v %v", j, ok)
	}
	close(release)
	j := waitJob(t, s, job.ID)
	if j.State != JobStateDone || j.MountPoint != "/mnt/Storage_sdb1" || j.FinishedAt == 0 || !finished {
		t.Fatalf("done job = %+v finished=%v", j, finished)
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(r.snapshot()) < 4 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	want := []string{
		"local-storage:storage-job:progress:running:queued",
		"local-storage:storage-job:progress:running:partitioning",
		"local-storage:storage-job:progress:running:mounting",
		"local-storage:storage-job:end:done:done",
	}
	if strings.Join(r.snapshot(), "\n") != strings.Join(want, "\n") {
		t.Fatalf("events:\n%s", strings.Join(r.snapshot(), "\n"))
	}
}

func TestStorageJobErrorAndPanic(t *testing.T) {
	s := NewJobStore(nil)
	j := waitJob(t, s, s.Start("format", "/dev/sdb1", func(func(string)) (string, error) {
		return "", errors.New("mkfs failed")
	}, nil).ID)
	if j.State != JobStateError || j.Message != "mkfs failed" {
		t.Fatalf("error job = %+v", j)
	}
	released := false
	j = waitJob(t, s, s.Start("format", "/dev/sdb1", func(func(string)) (string, error) {
		panic("boom")
	}, func() { released = true }).ID)
	if j.State != JobStateError || !released {
		t.Fatalf("panicking job = %+v released=%v", j, released)
	}
	if _, ok := s.Get("nope"); ok {
		t.Fatal("unknown job found")
	}
}

func TestStorageJobPruning(t *testing.T) {
	s := NewJobStore(nil)
	now := time.Now()
	s.now = func() time.Time { return now }
	old := &StorageJob{ID: "old", State: JobStateDone, FinishedAt: now.Add(-2 * time.Hour).Unix()}
	running := &StorageJob{ID: "run", State: JobStateRunning, StartedAt: now.Add(-3 * time.Hour).Unix()}
	s.jobs["old"], s.jobs["run"] = old, running
	s.mu.Lock()
	s.pruneLocked()
	s.mu.Unlock()
	if _, ok := s.jobs["old"]; ok {
		t.Fatal("old finished job kept")
	}
	if _, ok := s.jobs["run"]; !ok {
		t.Fatal("running job pruned")
	}
}

func TestJobEventProperties(t *testing.T) {
	p := JobEventProperties(StorageJob{ID: "1", Kind: "create", Path: "/dev/sdb", State: "error", Message: "x"})
	if p["local-storage:job_id"] != "1" || p["local-storage:state"] != "error" || p["local-storage:message"] != "x" || p["local-storage:path"] != "/dev/sdb" {
		t.Fatalf("%v", p)
	}
}
