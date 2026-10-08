package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/model"
	"github.com/patrickmn/go-cache"
)

// Fixtures: real smartctl 7.4 --json -a -l devstat output from this
// project's own drives (serials/WWNs scrubbed), some edited into failure
// cases; nvme/sleeping/unknown-bridge follow smartctl 7.4's format.
func fixture(t *testing.T, name string) model.SmartctlA {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "smart", name))
	if err != nil {
		t.Fatal(err)
	}
	return smartFromJSON(t, string(b))
}

var smartNow = time.Date(2026, 10, 9, 12, 0, 0, 0, time.Local)

func metric(r SmartReport, key string) (SmartMetric, bool) {
	for _, m := range r.Metrics {
		if m.Key == key {
			return m, true
		}
	}
	return SmartMetric{}, false
}

func TestSmartReportFixtures(t *testing.T) {
	cases := []struct {
		file, status, verdict, kind, summary string
		want                                 map[string]int64
	}{
		// tower after the power cut: 12 uncorrectable reads, all from
		// ~5300 power-on hours (it is at 17549) - a note, not a warning
		{"hdd_healthy_tower.json", "ok", VerdictGood, "hdd", "Healthy", map[string]int64{"pending": 0, "reallocated": 0, "reported_uncorrect": 12, "crc_errors": 0, "power_on_hours": 17549, "unsafe_shutdowns": 959}},
		{"hdd_pending.json", "ok", VerdictWatch, "hdd", "12 sectors waiting to be remapped - back up this drive", map[string]int64{"pending": 12, "reallocated": 8, "offline_uncorrectable": 2}},
		{"ssd_healthy.json", "ok", VerdictGood, "ssd", "Healthy", map[string]int64{"wear": 10, "unsafe_shutdowns": 1711, "temperature_max": 61}},
		{"ssd_worn.json", "ok", VerdictWatch, "ssd", "The SSD has used 96% of its rated writes - plan a replacement", map[string]int64{"wear": 96}},
		{"nvme.json", "ok", VerdictGood, "nvme", "Healthy", map[string]int64{"wear": 3, "available_spare": 100, "media_errors": 0, "unsafe_shutdowns": 87, "power_on_hours": 9112}},
		{"usb_sat_hdd.json", "ok", VerdictGood, "hdd", "Healthy", map[string]int64{"crc_errors": 3735}},
		{"usb_unknown_bridge.json", "unsupported", VerdictUnknown, "", "This drive (or its USB enclosure) doesn't report SMART health", nil},
	}
	for _, c := range cases {
		r := BuildSmartReport("/dev/x", fixture(t, c.file), nil, smartNow)
		if r.Status != c.status || r.Verdict != c.verdict || r.Kind != c.kind || r.Summary != c.summary {
			t.Errorf("%s: status=%s verdict=%s kind=%s summary=%q reasons=%v", c.file, r.Status, r.Verdict, r.Kind, r.Summary, r.Reasons)
		}
		for k, want := range c.want {
			if m, ok := metric(r, k); !ok || m.Value != want {
				t.Errorf("%s: %s = %+v, want %d", c.file, k, m, want)
			}
		}
	}

	tower := BuildSmartReport("/dev/sdb", fixture(t, "hdd_healthy_tower.json"), nil, smartNow)
	if len(tower.Notes) != 1 || !strings.Contains(tower.Notes[0], "12 uncorrectable read errors logged long ago (last at 5289") {
		t.Errorf("tower notes = %v", tower.Notes)
	}
	if !tower.SelfTest.Supported || tower.SelfTest.Running || tower.SelfTest.LongMinutes != 480 || len(tower.SelfTest.Last) == 0 || !tower.SelfTest.Last[0].Passed {
		t.Errorf("tower self-test = %+v", tower.SelfTest)
	}
	if len(tower.Raw) == 0 || tower.Raw[0].Name != "Raw Read Error Rate" {
		t.Errorf("raw table = %v", tower.Raw)
	}
	if m, _ := metric(BuildSmartReport("/dev/x", fixture(t, "hdd_pending.json"), nil, smartNow), "pending"); m.Level != VerdictWatch {
		t.Errorf("pending metric level = %s", m.Level)
	}
	// fresh errors in the error log make the old read-error count a warning
	if r := BuildSmartReport("/dev/x", fixture(t, "hdd_pending.json"), nil, smartNow); len(r.Reasons) != 4 {
		t.Errorf("pending reasons = %v", r.Reasons)
	}
}

func TestSmartNVMeSelfTestProgressAndWarnings(t *testing.T) {
	m := fixture(t, "nvme.json")
	r := BuildSmartReport("/dev/nvme0", m, nil, smartNow)
	if !r.SelfTest.Running || r.SelfTest.RemainingPercent != 70 || len(r.SelfTest.Last) != 2 {
		t.Errorf("nvme self-test = %+v", r.SelfTest)
	}
	m.NvmeHealth.CriticalWarning = 4
	m.NvmeHealth.MediaErrors = 3
	r = BuildSmartReport("/dev/nvme0", m, nil, smartNow)
	if r.Verdict != VerdictFailing || !strings.Contains(r.Summary, "reliability is degraded") || len(r.Reasons) != 2 {
		t.Errorf("nvme warning: %s %v", r.Verdict, r.Reasons)
	}
}

func TestSmartATASelfTestRunningAndFailed(t *testing.T) {
	m := fixture(t, "ssd_healthy.json")
	m.AtaSmartData.SelfTest.Status.Value = 249
	m.AtaSmartData.SelfTest.Status.RemainingPercent = 90
	if st := BuildSmartReport("/dev/x", m, nil, smartNow).SelfTest; !st.Running || st.RemainingPercent != 90 {
		t.Errorf("running = %+v", st)
	}
	no := false
	m.AtaSmartSelfTestLog.Standard.Table[0].Status.Value = 7
	m.AtaSmartSelfTestLog.Standard.Table[0].Status.String = "Completed: read failure"
	m.AtaSmartSelfTestLog.Standard.Table[0].Status.Passed = &no
	if r := BuildSmartReport("/dev/x", m, nil, smartNow); r.Verdict != VerdictFailing || !strings.Contains(r.Summary, "read failure") {
		t.Errorf("failed self-test: %s %q", r.Verdict, r.Summary)
	}
}

func TestSmartSleepingAndMissing(t *testing.T) {
	asleep := fixture(t, "sleeping.json")
	if !IsSmartStandby(asleep) {
		t.Fatal("standby not detected")
	}
	r := BuildSmartReport("/dev/sdb", SmartForSleepingDrive(asleep, model.SmartctlA{}, false), nil, smartNow)
	if r.Status != "asleep" || r.Verdict != VerdictUnknown {
		t.Errorf("asleep = %+v", r)
	}
	// asleep with an earlier awake reading: that reading, marked stale
	r = BuildSmartReport("/dev/sdb", SmartForSleepingDrive(asleep, fixture(t, "hdd_healthy_tower.json"), true), nil, smartNow)
	if r.Status != "ok" || !r.Stale || r.Verdict != VerdictGood {
		t.Errorf("asleep with last reading = %s stale=%v %s", r.Status, r.Stale, r.Verdict)
	}

	defer func(f func() bool) { smartctlMissing = f }(smartctlMissing)
	smartctlMissing = func() bool { return true }
	if r := BuildSmartReport("/dev/sdb", model.SmartctlA{}, nil, smartNow); r.Status != "tools_missing" || !strings.Contains(r.Summary, "smartmontools") {
		t.Errorf("missing = %+v", r)
	}
	smartctlMissing = func() bool { return false }
	if r := BuildSmartReport("/dev/sdb", model.SmartctlA{}, nil, smartNow); r.Status != "unreadable" {
		t.Errorf("unreadable = %+v", r)
	}
}

func TestSmartHistoryGrowthAndNotifyOnce(t *testing.T) {
	healthy := fixture(t, "hdd_healthy_tower.json")
	day := func(d int) time.Time { return smartNow.AddDate(0, 0, d) }

	rec := &smartDriveRecord{}
	r := BuildSmartReport("/dev/sdb", healthy, rec.Snapshots, day(-3))
	rec.Snapshots = r.History
	if smartNotice(rec, r) {
		t.Fatal("a good drive's first reading notified")
	}

	// three days later: 12 pending sectors
	bad := healthy
	bad.AtaSmartAttributes.Table = append([]model.SmartAttribute{}, healthy.AtaSmartAttributes.Table...)
	for i := range bad.AtaSmartAttributes.Table {
		if bad.AtaSmartAttributes.Table[i].ID == 197 {
			bad.AtaSmartAttributes.Table[i].Raw.Value = 12
		}
	}
	r = BuildSmartReport("/dev/sdb", bad, rec.Snapshots, smartNow)
	if r.Verdict != VerdictFailing || len(r.Changes) != 1 || r.Changes[0] != "Sectors waiting to be remapped went from 0 to 12 this week" {
		t.Fatalf("growth: %s %v", r.Verdict, r.Changes)
	}
	if !strings.Contains(r.Summary, "and the number is growing") || len(r.History) != 2 {
		t.Errorf("summary %q history %d", r.Summary, len(r.History))
	}
	rec.Snapshots = r.History
	if !smartNotice(rec, r) {
		t.Fatal("going failing didn't notify")
	}
	if smartNotice(rec, r) {
		t.Fatal("the same state notified twice")
	}

	// a week on the count is stable: watch, no new notification
	r = BuildSmartReport("/dev/sdb", bad, rec.Snapshots, day(9))
	if r.Verdict != VerdictWatch || len(r.Changes) != 0 || smartNotice(rec, r) {
		t.Fatalf("stable after a week: %s %v", r.Verdict, r.Changes)
	}
	// it grows again: notified again
	for i := range bad.AtaSmartAttributes.Table {
		if bad.AtaSmartAttributes.Table[i].ID == 197 {
			bad.AtaSmartAttributes.Table[i].Raw.Value = 20
		}
	}
	r = BuildSmartReport("/dev/sdb", bad, rec.Snapshots, day(10))
	if !smartNotice(rec, r) {
		t.Fatal("a growing counter didn't notify")
	}

	// a drive first seen already failing notifies once
	fresh := &smartDriveRecord{}
	worn := BuildSmartReport("/dev/sda", fixture(t, "ssd_worn.json"), nil, smartNow)
	if !smartNotice(fresh, worn) || smartNotice(fresh, worn) {
		t.Fatal("first sight of a watch drive: want exactly one notification")
	}
}

func TestSmartReportForPersistsHistory(t *testing.T) {
	defer func(p string) { SmartHistoryPath = p; smartHist.loaded = false }(SmartHistoryPath)
	SmartHistoryPath = filepath.Join(t.TempDir(), "smart-history.json")
	smartHist.loaded = false

	m := fixture(t, "hdd_healthy_tower.json")
	SmartReportFor("/dev/sdb", m)
	smartHist.loaded = false // as after a restart
	r := SmartReportFor("/dev/sdb", m)
	if len(r.History) != 1 || r.History[0].Values["reported_uncorrect"] != 12 {
		t.Fatalf("history = %+v", r.History)
	}
	b, _ := os.ReadFile(SmartHistoryPath)
	if !strings.Contains(string(b), `"pending":0`) {
		t.Fatalf("file = %s", b)
	}
}

func TestSmartSelfTestStart(t *testing.T) {
	defer func(f func(string, string) (string, error)) { smartSelfTestCmd = f }(smartSelfTestCmd)
	var got []string
	smartSelfTestCmd = func(path, kind string) (string, error) {
		got = append(got, path, kind)
		return "Testing has begun.", nil
	}
	defer func(c *cache.Cache) { Cache = c }(Cache)
	Cache = cache.New(time.Minute, time.Minute)
	d := &diskService{}
	if err := d.SmartTest("/dev/sdb", "long"); err != nil || strings.Join(got, " ") != "/dev/sdb long" {
		t.Fatalf("err=%v got=%v", err, got)
	}
	if err := d.SmartTest("/dev/sdb", "conveyance"); err == nil || len(got) != 2 {
		t.Fatal("an unknown test type was started")
	}
}
