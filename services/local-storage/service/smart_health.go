package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/model"
	"go.uber.org/zap"
)

// Drive health from SMART, in plain words.
//
// BuildSmartReport turns one smartctl --json reading into a verdict (good /
// watch / failing) with the reasons, the numbers that actually predict a
// failure (Backblaze's list: reallocated, pending and offline-uncorrectable
// sectors, reported uncorrectable errors, plus cable errors, SSD wear and
// NVMe spare/media errors), self-test state and a raw table. A small daily
// history (smart-history.json) lets it say "pending sectors went 0 → 12
// this week" and lets the watcher notify once per change.

const (
	VerdictGood    = "good"
	VerdictWatch   = "watch"
	VerdictFailing = "failing"
	VerdictUnknown = "unknown"

	smartHistoryDays = 90
	smartGrowthDays  = 7
	// an error-log entry this many power-on hours old still counts as recent
	smartRecentErrHours = 30 * 24
)

var (
	SmartHistoryPath = "/var/lib/nivaroos/smart-history.json"
	smartctlMissing  = func() bool { _, err := exec.LookPath("smartctl"); return err != nil }
)

type SmartMetric struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Value   int64  `json:"value"`
	Unit    string `json:"unit,omitempty"`
	Explain string `json:"explain"`
	Level   string `json:"level"` // good | note (old, not increasing) | watch | failing
}

type SmartSelfTestEntry struct {
	Type   string `json:"type"`
	Result string `json:"result"`
	Passed bool   `json:"passed"`
	Hours  int    `json:"hours"`
}

type SmartSelfTest struct {
	Supported        bool                 `json:"supported"`
	Running          bool                 `json:"running"`
	RemainingPercent int                  `json:"remaining_percent"`
	ShortMinutes     int                  `json:"short_minutes,omitempty"`
	LongMinutes      int                  `json:"long_minutes,omitempty"`
	Last             []SmartSelfTestEntry `json:"last"`
}

type SmartSnapshot struct {
	Date    string           `json:"date"` // 2006-01-02, local time
	Verdict string           `json:"verdict"`
	Values  map[string]int64 `json:"values"`
}

type SmartRawRow struct {
	ID     int    `json:"id,omitempty"`
	Name   string `json:"name"`
	Value  string `json:"value,omitempty"`
	Worst  string `json:"worst,omitempty"`
	Thresh string `json:"thresh,omitempty"`
	Raw    string `json:"raw"`
	Failed string `json:"failed,omitempty"`
}

type SmartReport struct {
	Path string `json:"path"`
	// ok | asleep (never read awake) | tools_missing | unsupported | unreadable
	Status  string   `json:"status"`
	Verdict string   `json:"verdict"`
	Summary string   `json:"summary"`
	Reasons []string `json:"reasons"`
	Notes   []string `json:"notes"`
	Kind    string   `json:"kind,omitempty"` // hdd | ssd | nvme
	Model   string   `json:"model,omitempty"`
	// Stale: the drive is asleep; this is its last awake reading.
	Stale    bool            `json:"stale"`
	Metrics  []SmartMetric   `json:"metrics"`
	SelfTest SmartSelfTest   `json:"self_test"`
	Changes  []string        `json:"changes"`
	History  []SmartSnapshot `json:"history"`
	Raw      []SmartRawRow   `json:"raw"`

	values map[string]int64
}

// counters that mean damage when they go up
var smartBadCounters = []string{"pending", "reallocated", "offline_uncorrectable", "reported_uncorrect", "crc_errors", "media_errors"}

var smartLabels = map[string][2]string{
	"reallocated":           {"Reallocated sectors", "Bad spots the drive has already swapped for spares. A few can be harmless; a growing number means the surface is wearing out."},
	"pending":               {"Sectors waiting to be remapped", "Spots the drive couldn't read and will replace on the next write. Data there may already be lost - back up this drive."},
	"offline_uncorrectable": {"Unreadable sectors (offline scan)", "Spots the drive's own background scan couldn't read at all."},
	"reported_uncorrect":    {"Read errors reported to the system", "Reads the drive couldn't correct, so a file read failed."},
	"crc_errors":            {"Cable / connection errors", "Data garbled between drive and computer. Almost always the cable, connector or USB enclosure, not the disk itself."},
	"media_errors":          {"Media errors", "Data the SSD couldn't read back intact."},
	"power_on_hours":        {"Powered on", "Total hours the drive has run."},
	"power_cycles":          {"Power cycles", "How many times it was switched on."},
	"unsafe_shutdowns":      {"Unsafe shutdowns", "Times power was cut without a proper shutdown (power cut, pulled cable). Not a failure itself, but each one risks file-system damage."},
	"temperature":           {"Temperature", "Now."},
	"temperature_max":       {"Highest temperature", "The hottest it has ever been."},
	"wear":                  {"SSD wear", "Share of its rated write endurance used. 100% means worn out."},
	"available_spare":       {"Spare blocks left", "Reserve the SSD uses to replace worn-out blocks."},
}

var smartUnits = map[string]string{"power_on_hours": "h", "temperature": "°C", "temperature_max": "°C", "wear": "%", "available_spare": "%"}

func smartRank(v string) int {
	switch v {
	case VerdictWatch:
		return 1
	case VerdictFailing:
		return 2
	case VerdictGood:
		return 0
	}
	return -1
}

// SmartUnavailable: the report for a drive with no reading at all.
func SmartUnavailable(path string) SmartReport {
	r := SmartReport{Path: path, Verdict: VerdictUnknown, Status: "unreadable", Summary: "Couldn't read this drive's health"}
	if smartctlMissing() {
		r.Status, r.Summary = "tools_missing", "SMART tools (smartmontools) aren't installed - re-run the NivaroOS installer to add them"
	}
	return r
}

// BuildSmartReport: history oldest first, today's snapshot not yet in it
// (or replaced by this reading).
func BuildSmartReport(path string, m model.SmartctlA, history []SmartSnapshot, now time.Time) SmartReport {
	if reflect.DeepEqual(m, model.SmartctlA{}) {
		return SmartUnavailable(path)
	}
	r := SmartReport{Path: path, Verdict: VerdictUnknown, Model: strings.TrimSpace(m.ModelName), Reasons: []string{}, Notes: []string{}, Changes: []string{}, Metrics: []SmartMetric{}, Raw: []SmartRawRow{}, History: []SmartSnapshot{}}
	if m.Sleeping && !m.StaleHealth {
		r.Status, r.Summary = "asleep", "Asleep - health is read when it wakes up"
		r.History = history
		return r
	}
	nvme := m.NvmeHealth
	if len(m.AtaSmartAttributes.Table) == 0 && nvme == nil {
		r.Status, r.Summary = "unsupported", "This drive (or its USB enclosure) doesn't report SMART health"
		return r
	}
	r.Status, r.Stale = "ok", m.Sleeping

	attrs := map[int]model.SmartAttribute{}
	for _, a := range m.AtaSmartAttributes.Table {
		attrs[a.ID] = a
	}
	attr := func(ids ...int) (int64, bool) {
		for _, id := range ids {
			if a, ok := attrs[id]; ok {
				return a.Raw.Value, true
			}
		}
		return 0, false
	}
	stat := func(name string) (int64, bool) {
		for _, p := range m.AtaDeviceStatistics.Pages {
			for _, t := range p.Table {
				if t.Name == name && t.Value != nil {
					return *t.Value, true
				}
			}
		}
		return 0, false
	}
	v := map[string]int64{}
	set := func(k string, x int64, ok bool) {
		if ok {
			v[k] = x
		}
	}
	setp := func(k string) func(int64, bool) { return func(x int64, ok bool) { set(k, x, ok) } }
	either := func(k string, a int64, aok bool, b int64, bok bool) {
		if aok {
			v[k] = a
		} else {
			set(k, b, bok)
		}
	}

	switch {
	case nvme != nil || strings.EqualFold(m.Device.Protocol, "NVMe"):
		r.Kind = "nvme"
	case m.RotationRate > 0:
		r.Kind = "hdd"
	default:
		if _, spin := attrs[3]; spin { // Spin_Up_Time: a USB bridge hid the rotation rate
			r.Kind = "hdd"
		} else {
			r.Kind = "ssd"
		}
	}

	if nvme != nil {
		v["media_errors"] = nvme.MediaErrors
		v["power_on_hours"] = nvme.PowerOnHours
		v["power_cycles"] = nvme.PowerCycles
		v["unsafe_shutdowns"] = nvme.UnsafeShutdowns
		v["wear"] = int64(nvme.PercentageUsed)
		v["available_spare"] = int64(nvme.AvailableSpare)
		set("temperature", int64(nvme.Temperature), nvme.Temperature > 0)
	} else {
		a, aok := attr(5)
		b, bok := stat("Number of Reallocated Logical Sectors")
		either("reallocated", a, aok, b, bok)
		setp("pending")(attr(197))
		setp("offline_uncorrectable")(attr(198))
		a, aok = attr(187)
		b, bok = stat("Number of Reported Uncorrectable Errors")
		either("reported_uncorrect", a, aok, b, bok)
		a, aok = attr(199)
		b, bok = stat("Number of Interface CRC Errors")
		either("crc_errors", a, aok, b, bok)
		v["power_on_hours"] = int64(m.PowerOnTime.Hours)
		v["power_cycles"] = int64(m.PowerCycleCount)
		// 174 Unexpected_Power_Loss (SSD), 192 Power-Off_Retract_Count (HDD)
		setp("unsafe_shutdowns")(attr(174, 192))
		setp("wear")(stat("Percentage Used Endurance Indicator"))
		set("temperature", int64(m.Temperature.Current), m.Temperature.Current > 0)
	}
	set("temperature_max", int64(m.Temperature.LifetimeMax), m.Temperature.LifetimeMax > 0)
	r.values = v

	// growth this week: against the oldest snapshot of the last 7 days
	today := now.Format("2006-01-02")
	since := now.AddDate(0, 0, -smartGrowthDays).Format("2006-01-02")
	grew := map[string]int64{}
	for _, k := range smartBadCounters {
		cur, ok := v[k]
		if !ok {
			continue
		}
		for _, s := range history {
			if s.Date < since || s.Date == today {
				continue
			}
			if was, ok := s.Values[k]; ok {
				if cur > was {
					grew[k] = was
					r.Changes = append(r.Changes, fmt.Sprintf("%s went from %d to %d this week", smartLabels[k][0], was, cur))
				}
				break
			}
		}
	}

	levels := map[string]string{}
	var failing, watch []string
	flag := func(level, metric, reason string) {
		if metric != "" && smartRank(level) > smartRank(levels[metric]) {
			levels[metric] = level
		}
		if level == VerdictFailing {
			failing = append(failing, reason)
		} else {
			watch = append(watch, reason)
		}
	}
	growing := func(k string) string {
		if _, ok := grew[k]; ok {
			return ", and the number is growing"
		}
		return ""
	}
	// newest error-log entry within the last month of power-on time
	lastErr := 0
	for _, e := range m.AtaSmartErrorLog.Summary.Table {
		if e.LifetimeHours > lastErr {
			lastErr = e.LifetimeHours
		}
	}
	recentErr := lastErr > 0 && v["power_on_hours"]-int64(lastErr) <= smartRecentErrHours

	exit := m.Smartctl.ExitStatus
	if exit&8 != 0 {
		flag(VerdictFailing, "", "The drive's own health check says it is failing - back up this drive now and replace it")
	}
	if exit&16 != 0 {
		var names []string
		for _, a := range m.AtaSmartAttributes.Table {
			if a.WhenFailed == "now" {
				names = append(names, strings.ReplaceAll(a.Name, "_", " "))
			}
		}
		flag(VerdictFailing, "", "Past the maker's failure threshold ("+strings.Join(names, ", ")+") - back up this drive and replace it")
	}
	if n := v["pending"]; n > 0 {
		lv := VerdictWatch
		if growing("pending") != "" {
			lv = VerdictFailing
		}
		flag(lv, "pending", fmt.Sprintf("%d %s waiting to be remapped%s - back up this drive", n, plural(int(n), "sector", "sectors"), growing("pending")))
	}
	if n := v["offline_uncorrectable"]; n > 0 {
		lv := VerdictWatch
		if growing("offline_uncorrectable") != "" {
			lv = VerdictFailing
		}
		flag(lv, "offline_uncorrectable", fmt.Sprintf("%d unreadable %s found by the drive's own scan%s - back up this drive", n, plural(int(n), "sector", "sectors"), growing("offline_uncorrectable")))
	}
	if n := v["reallocated"]; n > 0 {
		lv := VerdictWatch
		if n >= 100 || growing("reallocated") != "" {
			lv = VerdictFailing
		}
		flag(lv, "reallocated", fmt.Sprintf("%d bad %s already replaced by spares%s - keep a current backup", n, plural(int(n), "sector", "sectors"), growing("reallocated")))
	}
	if n := v["reported_uncorrect"]; n > 0 {
		if growing("reported_uncorrect") != "" || recentErr {
			flag(VerdictWatch, "reported_uncorrect", fmt.Sprintf("%d read %s the drive couldn't correct, some recently - keep a current backup", n, plural(int(n), "error", "errors")))
		} else {
			levels["reported_uncorrect"] = "note" // old, not increasing
			r.Notes = append(r.Notes, fmt.Sprintf("%d uncorrectable read %s logged long ago%s; not increasing", n, plural(int(n), "error", "errors"), oldErr(lastErr)))
		}
	}
	if n := v["crc_errors"]; n > 0 {
		if growing("crc_errors") != "" || recentErr {
			flag(VerdictWatch, "crc_errors", fmt.Sprintf("%d cable/connection %s, some recently - reseat or replace the cable (or USB enclosure)", n, plural(int(n), "error", "errors")))
		} else {
			levels["crc_errors"] = "note" // old, not increasing
			r.Notes = append(r.Notes, fmt.Sprintf("%d cable/connection %s logged long ago%s; not increasing", n, plural(int(n), "error", "errors"), oldErr(lastErr)))
		}
	}
	if nvme != nil {
		cw := nvme.CriticalWarning
		for i, why := range []string{"spare blocks are below the safe level", "it is over its temperature limit", "its reliability is degraded", "it switched itself to read-only", "its power-loss backup failed"} {
			if cw&(1<<i) != 0 {
				flag(VerdictFailing, "", "The SSD warns that "+why+" - back up this drive and replace it")
			}
		}
		if cw&1 == 0 && nvme.AvailableSpareThreshold > 0 && nvme.AvailableSpare < nvme.AvailableSpareThreshold {
			flag(VerdictFailing, "available_spare", "Spare blocks are below the safe level - back up this drive and replace it")
		}
		if n := nvme.MediaErrors; n > 0 {
			lv := VerdictWatch
			if growing("media_errors") != "" {
				lv = VerdictFailing
			}
			flag(lv, "media_errors", fmt.Sprintf("%d media %s%s - keep a current backup", n, plural(int(n), "error", "errors"), growing("media_errors")))
		}
	}
	if w, ok := v["wear"]; ok {
		if w >= 100 {
			flag(VerdictFailing, "wear", fmt.Sprintf("The SSD has used %d%% of its rated writes - it is worn out, replace it", w))
		} else if w >= 90 {
			flag(VerdictWatch, "wear", fmt.Sprintf("The SSD has used %d%% of its rated writes - plan a replacement", w))
		}
	}
	if t, ok := v["temperature"]; ok {
		hot := int64(m.Temperature.OpLimitMax)
		if hot <= 0 {
			hot = map[string]int64{"hdd": 60, "ssd": 70, "nvme": 75}[r.Kind]
		}
		if t >= hot-5 {
			flag(VerdictWatch, "temperature", fmt.Sprintf("Running hot (%d °C, limit %d °C) - check the airflow", t, hot))
		}
	}

	r.SelfTest = smartSelfTest(m)
	if len(r.SelfTest.Last) > 0 && !r.SelfTest.Last[0].Passed {
		t := r.SelfTest.Last[0]
		flag(VerdictFailing, "", fmt.Sprintf("The last self-test (%s, at %d h) failed: %s - back up this drive", t.Type, t.Hours, t.Result))
	}

	switch {
	case len(failing) > 0:
		r.Verdict, r.Reasons = VerdictFailing, append(failing, watch...)
	case len(watch) > 0:
		r.Verdict, r.Reasons = VerdictWatch, watch
	default:
		r.Verdict = VerdictGood
	}
	if len(r.Reasons) > 0 {
		r.Summary = r.Reasons[0]
	} else {
		r.Summary = "Healthy"
	}

	order := []string{"pending", "reallocated", "offline_uncorrectable", "reported_uncorrect", "crc_errors", "media_errors", "wear", "available_spare", "temperature", "temperature_max", "power_on_hours", "power_cycles", "unsafe_shutdowns"}
	for _, k := range order {
		x, ok := v[k]
		if !ok {
			continue
		}
		lv := levels[k]
		if lv == "" {
			lv = VerdictGood
		}
		r.Metrics = append(r.Metrics, SmartMetric{Key: k, Label: smartLabels[k][0], Value: x, Unit: smartUnits[k], Explain: smartLabels[k][1], Level: lv})
	}

	for _, a := range m.AtaSmartAttributes.Table {
		raw := a.Raw.String
		if raw == "" {
			raw = fmt.Sprint(a.Raw.Value)
		}
		r.Raw = append(r.Raw, SmartRawRow{ID: a.ID, Name: strings.ReplaceAll(a.Name, "_", " "), Value: fmt.Sprint(a.Value), Worst: fmt.Sprint(a.Worst), Thresh: fmt.Sprint(a.Thresh), Raw: raw, Failed: strings.TrimSpace(a.WhenFailed)})
	}
	if nvme != nil {
		for _, kv := range []struct {
			n string
			x int64
		}{{"Critical warning", int64(nvme.CriticalWarning)}, {"Temperature", int64(nvme.Temperature)}, {"Available spare", int64(nvme.AvailableSpare)}, {"Available spare threshold", int64(nvme.AvailableSpareThreshold)}, {"Percentage used", int64(nvme.PercentageUsed)}, {"Data units written", nvme.DataUnitsWritten}, {"Power cycles", nvme.PowerCycles}, {"Power-on hours", nvme.PowerOnHours}, {"Unsafe shutdowns", nvme.UnsafeShutdowns}, {"Media errors", nvme.MediaErrors}, {"Error log entries", nvme.NumErrLogEntries}} {
			r.Raw = append(r.Raw, SmartRawRow{Name: kv.n, Raw: fmt.Sprint(kv.x)})
		}
	}

	r.History = withSnapshot(history, SmartSnapshot{Date: today, Verdict: r.Verdict, Values: v})
	return r
}

func oldErr(h int) string {
	if h <= 0 {
		return ""
	}
	return fmt.Sprintf(" (last at %d power-on hours)", h)
}

func smartSelfTest(m model.SmartctlA) SmartSelfTest {
	st := SmartSelfTest{Last: []SmartSelfTestEntry{}}
	if n := m.NvmeSelfTestLog; n != nil {
		st.Supported = true
		st.Running = n.CurrentSelfTestOperation.Value != 0
		if st.Running {
			st.RemainingPercent = 100 - n.CurrentSelfTestCompletionPercent
		}
		for _, e := range n.Table {
			if e.SelfTestResult.Value == 15 { // unused entry
				continue
			}
			// 1-4: aborted (by a command, reset, namespace removal, format)
			st.Last = append(st.Last, SmartSelfTestEntry{Type: e.SelfTestCode.String, Result: e.SelfTestResult.String, Passed: e.SelfTestResult.Value < 5 || e.SelfTestResult.Value > 7, Hours: e.PowerOnHours})
		}
	} else {
		d := m.AtaSmartData
		st.Supported = d.Capabilities.SelfTestsSupported
		st.Running = d.SelfTest.Status.Value>>4 == 15
		if st.Running {
			st.RemainingPercent = d.SelfTest.Status.RemainingPercent
		}
		st.ShortMinutes, st.LongMinutes = d.SelfTest.PollingMinutes.Short, d.SelfTest.PollingMinutes.Extended
		for _, e := range m.AtaSmartSelfTestLog.Standard.Table {
			if e.Status.Value>>4 == 15 { // the running one
				continue
			}
			// 1, 2: aborted/interrupted by the host - not the drive's fault
			bad := e.Status.Passed != nil && !*e.Status.Passed && e.Status.Value >= 3 && e.Status.Value <= 8
			st.Last = append(st.Last, SmartSelfTestEntry{Type: e.Type.String, Result: e.Status.String, Passed: !bad, Hours: e.LifetimeHours})
		}
	}
	if len(st.Last) > 5 {
		st.Last = st.Last[:5]
	}
	return st
}

func withSnapshot(h []SmartSnapshot, s SmartSnapshot) []SmartSnapshot {
	out := append([]SmartSnapshot{}, h...)
	if n := len(out); n > 0 && out[n-1].Date == s.Date {
		out[n-1] = s
	} else {
		out = append(out, s)
	}
	if len(out) > smartHistoryDays {
		out = out[len(out)-smartHistoryDays:]
	}
	return out
}

// --- history + notify-once state, one JSON file ---

type smartDriveRecord struct {
	Snapshots       []SmartSnapshot  `json:"snapshots"`
	NotifiedVerdict string           `json:"notified_verdict,omitempty"`
	NotifiedValues  map[string]int64 `json:"notified_values,omitempty"`
}

var smartHist = struct {
	sync.Mutex
	loaded bool
	data   map[string]*smartDriveRecord
}{}

func smartHistKey(m model.SmartctlA, path string) string {
	if s := strings.TrimSpace(m.SerialNumber); s != "" {
		return strings.TrimSpace(m.ModelName) + "|" + s
	}
	return "path|" + path
}

func smartRecord(key string) *smartDriveRecord {
	if !smartHist.loaded {
		smartHist.loaded = true
		smartHist.data = map[string]*smartDriveRecord{}
		if b, err := os.ReadFile(SmartHistoryPath); err == nil {
			_ = json.Unmarshal(b, &smartHist.data)
		}
	}
	rec := smartHist.data[key]
	if rec == nil {
		rec = &smartDriveRecord{}
		smartHist.data[key] = rec
	}
	return rec
}

func smartHistSave() {
	b, _ := json.Marshal(smartHist.data)
	_ = os.MkdirAll(filepath.Dir(SmartHistoryPath), 0o755)
	tmp := SmartHistoryPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err == nil {
		err = os.Rename(tmp, SmartHistoryPath)
		if err != nil {
			logger.Error("smart history not saved", zap.Error(err))
		}
	}
}

// SmartReportFor builds the report for path from reading m and keeps the
// daily snapshot (only from an awake reading).
func SmartReportFor(path string, m model.SmartctlA) SmartReport {
	if reflect.DeepEqual(m, model.SmartctlA{}) {
		return SmartUnavailable(path)
	}
	smartHist.Lock()
	defer smartHist.Unlock()
	key := smartHistKey(m, path)
	rec := smartRecord(key)
	r := BuildSmartReport(path, m, rec.Snapshots, time.Now())
	if r.Status == "ok" && !r.Stale {
		last := SmartSnapshot{}
		if n := len(rec.Snapshots); n > 0 {
			last = rec.Snapshots[n-1]
		}
		now := r.History[len(r.History)-1]
		if !reflect.DeepEqual(last, now) {
			rec.Snapshots = r.History
			smartHistSave()
		}
	}
	return r
}

// smartNotice: whether this reading deserves a notification - the verdict
// got worse, or a damage counter grew while not good. Updates what was
// last notified; the first reading of a drive is the baseline (notified
// only when it is already watch/failing).
func smartNotice(rec *smartDriveRecord, r SmartReport) bool {
	if r.Status != "ok" || r.Stale || r.Verdict == VerdictUnknown {
		return false
	}
	first := rec.NotifiedVerdict == ""
	worse := smartRank(r.Verdict) > smartRank(rec.NotifiedVerdict)
	grew := false
	if rec.NotifiedValues == nil {
		rec.NotifiedValues = map[string]int64{}
	}
	for _, k := range smartBadCounters {
		cur, ok := r.values[k]
		if !ok {
			continue
		}
		if !first && cur > rec.NotifiedValues[k] {
			grew = true
		}
		if first || cur > rec.NotifiedValues[k] {
			rec.NotifiedValues[k] = cur
		}
	}
	rec.NotifiedVerdict = r.Verdict
	if first {
		return smartRank(r.Verdict) >= 1
	}
	return worse || (grew && smartRank(r.Verdict) >= 1)
}

// CheckSmartHealth reads every drive (cached, never waking a sleeping one)
// and notifies once per change.
func (d *diskService) CheckSmartHealth() {
	if smartctlMissing() {
		return
	}
	for _, blk := range d.LSBLK(false) {
		if blk.Type != "" && blk.Type != "disk" {
			continue
		}
		m := d.SmartCTL(blk.Path)
		r := SmartReportFor(blk.Path, m)
		if r.Status != "ok" {
			continue
		}
		smartHist.Lock()
		rec := smartRecord(smartHistKey(m, blk.Path))
		send := smartNotice(rec, r)
		smartHistSave()
		smartHist.Unlock()
		if !send {
			continue
		}
		name := r.Model
		for _, c := range blk.Children {
			if c.MountPoint != "" {
				name = filepath.Base(c.MountPoint)
				break
			}
		}
		if blk.MountPoint == "/" || WalkDisk(blk, 5, func(b model.LSBLKModel) bool { return b.MountPoint == "/" }) != nil {
			name = "The system drive"
		}
		title, level := name+" needs watching", "warning"
		if r.Verdict == VerdictFailing {
			title, level = name+" may be failing", "error"
		}
		msg := r.Summary + "."
		if len(r.Changes) > 0 {
			msg += " " + strings.Join(r.Changes, "; ") + "."
		}
		msg += " Open Settings > Storage > the drive's Health for details."
		args, _ := json.Marshal(map[string]string{"path": blk.Path, "reason": "smart_" + r.Verdict})
		notifyDrive(map[string]string{
			"title": title, "message": msg, "level": level, "category": "storage",
			"args": string(args), "action": `{"target":"settings","props":{"section":"storage"}}`,
		})
	}
}

// StartSmartWatcher checks drive health hourly (first a few minutes after
// start, once the drives have settled).
func (d *diskService) StartSmartWatcher(ctx context.Context) {
	go func() {
		wait := 3 * time.Minute
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			d.CheckSmartHealth()
			wait = time.Hour
		}
	}()
}
