package jobs

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// Server-side validation (spec §11, §16.1 validate_test.go).

func validEnv() ValidateEnv {
	return ValidateEnv{
		Settings: DefaultAppSettings(),
		Apps:     func() (map[string]bool, error) { return map[string]bool{"immich": true, "blinko": false}, nil },
		VMs:      func() (map[string]bool, error) { return map[string]bool{"win11": true}, nil },
	}
}

// mustInvalid normalises j and requires field to carry code.
func mustInvalid(t *testing.T, j Job, field string, code interface{}) {
	t.Helper()
	_, fe := NormalizeJob(j, validEnv())
	if got, want := fe[field], fmt.Sprint(code); got != want {
		t.Fatalf("field_errors[%q] = %q, want %q (all: %v)", field, got, want, fe)
	}
}

func TestNormalizeValidJobFillsDefaults(t *testing.T) {
	j := sampleJob("Docs")
	j.Type = TypeMirror
	j.Triggers = []Trigger{{Kind: TriggerSchedule, Cron: "0 3 * * *"}}
	j.Conditions = Conditions{DestAvailable: true}
	j.Retry = Retry{Max: 3}
	j.ID, j.Revision, j.NeedsAttention, j.DestFolderID = "bk_forged", 99, AttentionDestChanged, "forged"
	norm, fe := NormalizeJob(j, validEnv())
	if len(fe) > 0 {
		t.Fatalf("unexpected field errors: %v", fe)
	}
	if norm.ID != "" || norm.Revision != 0 || norm.NeedsAttention != "" || norm.DestFolderID != "" {
		t.Errorf("server-owned fields not cleared: %+v", norm)
	}
	if norm.Conditions.WhenUnmet != UnmetWait {
		t.Errorf("when_unmet default with a schedule = %q, want wait", norm.Conditions.WhenUnmet)
	}
	if norm.Conditions.WaitMaxMin != DefaultWaitMaxMin || norm.Options.MaxDurationSec != DefaultMaxDurationSec {
		t.Errorf("defaults not filled: %+v %+v", norm.Conditions, norm.Options)
	}
	if norm.Guards.DeletePct != DefaultDeletePct || norm.Guards.ChangePct != DefaultChangePct || norm.Guards.EmptySourcePct != DefaultEmptySourcePct {
		t.Errorf("guard defaults: %+v", norm.Guards)
	}
	if len(norm.Retry.BackoffSec) != 3 || !norm.Notify.OnFailure {
		t.Errorf("retry/notify defaults: %+v %+v", norm.Retry, norm.Notify)
	}
	if norm.Notify.StaleAfterHours != DefaultStaleMinHours {
		t.Errorf("stale default for a daily job = %d, want %d", norm.Notify.StaleAfterHours, DefaultStaleMinHours)
	}
	// A manual-only job waits nowhere: skip.
	j2 := sampleJob("Manual")
	n2, _ := NormalizeJob(j2, validEnv())
	if n2.Conditions.WhenUnmet != UnmetSkip {
		t.Errorf("when_unmet default without a schedule = %q, want skip", n2.Conditions.WhenUnmet)
	}
}

func TestNormalizeSettingsDefaultsApply(t *testing.T) {
	env := validEnv()
	env.Settings.DefaultDeletePct, env.Settings.DefaultChangePct = 5, 40
	norm, fe := NormalizeJob(sampleJob("x"), env)
	if len(fe) > 0 {
		t.Fatal(fe)
	}
	if norm.Guards.DeletePct != 5 || norm.Guards.ChangePct != 40 {
		t.Errorf("guards %+v, want the app defaults 5/40", norm.Guards)
	}
}

func TestSubPathRejection(t *testing.T) {
	for _, bad := range []string{"../etc", "DATA/../../etc", "/etc/passwd", "a\x00b", `a\b`, "..", "x/.."} {
		j := sampleJob("x")
		j.Sources[0].SubPath = bad
		mustInvalid(t, j, "sources[0].sub_path", ErrPathNotAllowed)
	}
	for in, want := range map[string]string{"": "", ".": "", "a//b/": "a/b", "./a/./b": "a/b", "Photos 2024": "Photos 2024"} {
		got, ok := CleanSubPath(in)
		if !ok || got != want {
			t.Errorf("CleanSubPath(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	long := strings.Repeat("a/", maxSubPathLen)
	if _, ok := CleanSubPath(long); ok {
		t.Error("an over-long sub-path was accepted")
	}
}

func TestEndpointFields(t *testing.T) {
	j := sampleJob("x")
	j.Dest.Kind = "ftp"
	mustInvalid(t, j, "dest.kind", FieldInvalid)

	j = sampleJob("x")
	j.Dest.RefID = "  "
	mustInvalid(t, j, "dest.ref_id", FieldRequired)

	j = sampleJob("x")
	j.Dest = Endpoint{Kind: EPSMB, RefID: "3", SubPath: ""}
	mustInvalid(t, j, "dest.sub_path", FieldRequired)

	j = sampleJob("x")
	j.Sources[0].Preset = "appdata:"
	mustInvalid(t, j, "sources[0].preset", FieldInvalid)

	// A match block only means something on a USB drive.
	j = sampleJob("x")
	j.Dest.Match = &DevMatch{Serial: "S1"}
	norm, fe := NormalizeJob(j, validEnv())
	if len(fe) > 0 || norm.Dest.Match != nil {
		t.Errorf("match on a volume kept: %+v %v", norm.Dest, fe)
	}
}

func TestSourceCount(t *testing.T) {
	j := sampleJob("x")
	j.Sources = nil
	mustInvalid(t, j, "sources", FieldRequired)

	j = sampleJob("x")
	j.Sources = append(j.Sources, Endpoint{Kind: EPVolume, RefID: "root-uuid", SubPath: "DATA/Photos"})
	mustInvalid(t, j, "sources", FieldTooMany)

	j = sampleJob("x")
	j.Type = TypeArchive
	j.Sources = nil
	for i := 0; i < MaxArchiveSources+1; i++ {
		j.Sources = append(j.Sources, Endpoint{Kind: EPVolume, RefID: "root-uuid", SubPath: "DATA/f" + string(rune('a'+i))})
	}
	mustInvalid(t, j, "sources", FieldTooMany)
	j.Sources = j.Sources[:MaxArchiveSources]
	if _, fe := NormalizeJob(j, validEnv()); len(fe) > 0 {
		t.Errorf("16 archive sources rejected: %v", fe)
	}

	j = sampleJob("x")
	j.Type = TypeArchive
	j.Sources = []Endpoint{j.Sources[0], j.Sources[0]}
	mustInvalid(t, j, "sources[1]", FieldInvalid)
}

func TestDestInsideSource(t *testing.T) {
	j := sampleJob("x")
	j.Sources[0] = Endpoint{Kind: EPVolume, RefID: "root-uuid", SubPath: "DATA"}
	j.Dest = Endpoint{Kind: EPVolume, RefID: "root-uuid", SubPath: "DATA/Backup"}
	mustInvalid(t, j, "dest.sub_path", ErrDestInsideSource)

	// With the automatic exclude of the destination it is allowed (the
	// migration adds that rule).
	rule, ok := DestExcludeRule(j.Sources[0], j.Dest)
	if !ok || rule != "/Backup/**" {
		t.Fatalf("DestExcludeRule = %q, %v", rule, ok)
	}
	j.Filters.Exclude = []string{rule}
	if _, fe := NormalizeJob(j, validEnv()); len(fe) > 0 {
		t.Errorf("excluded destination still rejected: %v", fe)
	}

	// A source inside (or equal to) the destination is never fine.
	j = sampleJob("x")
	j.Sources[0] = Endpoint{Kind: EPVolume, RefID: "tank-uuid", SubPath: "Backups/Docs"}
	j.Dest = Endpoint{Kind: EPVolume, RefID: "tank-uuid", SubPath: "Backups"}
	mustInvalid(t, j, "sources[0].sub_path", ErrDestInsideSource)

	// "DATA2" is not inside "DATA": segments, not string prefixes.
	j = sampleJob("x")
	j.Sources[0] = Endpoint{Kind: EPVolume, RefID: "root-uuid", SubPath: "DATA"}
	j.Dest = Endpoint{Kind: EPVolume, RefID: "root-uuid", SubPath: "DATA2/Backup"}
	if _, fe := NormalizeJob(j, validEnv()); len(fe) > 0 {
		t.Errorf("sibling folder rejected: %v", fe)
	}
}

func TestAppDataToCloudNeedsArchive(t *testing.T) {
	for _, preset := range []string{"appdata:immich", "vm:win11"} {
		j := sampleJob("x")
		j.Sources[0].Preset = preset
		j.Dest = Endpoint{Kind: EPCloud, RefID: "gdrive", SubPath: "Backups"}
		mustInvalid(t, j, "type", ErrAppdataCloudNeedsArchive)
		j.Type = TypeArchive
		if _, fe := NormalizeJob(j, validEnv()); len(fe) > 0 {
			t.Errorf("%s archive to cloud rejected: %v", preset, fe)
		}
	}
	// Plain data to a cloud is fine for any type.
	j := sampleJob("x")
	j.Sources[0].Preset = "data:Documents"
	j.Dest = Endpoint{Kind: EPCloud, RefID: "gdrive", SubPath: "Backups"}
	if _, fe := NormalizeJob(j, validEnv()); len(fe) > 0 {
		t.Errorf("data to cloud rejected: %v", fe)
	}
}

func TestMirrorNeedsDestAvailable(t *testing.T) {
	j := sampleJob("x")
	j.Type = TypeMirror
	j.Conditions.DestAvailable = false
	mustInvalid(t, j, "conditions.dest_available", FieldMirrorNeedsDest)
	j.Conditions.DestAvailable = true
	if _, fe := NormalizeJob(j, validEnv()); len(fe) > 0 {
		t.Error(fe)
	}
}

func TestTriggerValidation(t *testing.T) {
	for _, bad := range []string{"* * *", "61 * * * *", "0 3 * * * *", "@sometimes", "TZ=UTC x"} {
		j := sampleJob("x")
		j.Triggers = []Trigger{{Kind: TriggerSchedule, Cron: bad}}
		mustInvalid(t, j, "triggers[0].cron", FieldInvalidCron)
	}
	j := sampleJob("x")
	j.Triggers = []Trigger{{Kind: TriggerSchedule}}
	mustInvalid(t, j, "triggers[0].cron", FieldRequired)

	for _, good := range []string{"0 3 * * *", "*/15 * * * *", "@daily", "@every 6h", "30 2 * * 1-5"} {
		j := sampleJob("x")
		j.Triggers = []Trigger{{Kind: TriggerSchedule, Cron: good}}
		norm, fe := NormalizeJob(j, validEnv())
		if len(fe) > 0 {
			t.Errorf("cron %q rejected: %v", good, fe)
		} else if norm.Triggers[0].Cron != good {
			t.Errorf("cron not kept byte for byte: %q -> %q", good, norm.Triggers[0].Cron)
		}
	}

	j = sampleJob("x")
	j.Triggers = []Trigger{{Kind: "sunrise"}}
	mustInvalid(t, j, "triggers[0].kind", FieldInvalid)

	// volume_mounted defaults to the destination volume ...
	j = sampleJob("x")
	j.Triggers = []Trigger{{Kind: TriggerVolumeMounted, MinGapHours: 24}}
	if _, fe := NormalizeJob(j, validEnv()); len(fe) > 0 {
		t.Errorf("volume trigger on a volume destination rejected: %v", fe)
	}
	// ... and needs one: cloud to cloud has none.
	j.Sources[0] = Endpoint{Kind: EPCloud, RefID: "a", SubPath: "x"}
	j.Dest = Endpoint{Kind: EPCloud, RefID: "b", SubPath: "y"}
	mustInvalid(t, j, "triggers[0].volume", FieldNotResolvable)
	j.Triggers[0].VolumeRef = &Endpoint{Kind: EPCloud, RefID: "b"}
	mustInvalid(t, j, "triggers[0].volume", FieldNotResolvable)

	j = sampleJob("x")
	j.Triggers = []Trigger{{Kind: TriggerVolumeMounted, MinGapHours: -1}}
	mustInvalid(t, j, "triggers[0].min_gap_hours", FieldOutOfRange)

	j = sampleJob("x")
	for i := 0; i <= maxTriggers; i++ {
		j.Triggers = append(j.Triggers, Trigger{Kind: TriggerManual})
	}
	mustInvalid(t, j, "triggers", FieldTooMany)
}

func TestConditionValidation(t *testing.T) {
	j := sampleJob("x")
	j.Conditions.Window = &TimeWindow{Start: "25:00", End: "06:00"}
	mustInvalid(t, j, "conditions.window.start", FieldInvalidTime)
	j.Conditions.Window = &TimeWindow{Start: "01:00", End: "01:00"}
	mustInvalid(t, j, "conditions.window.end", FieldInvalid)
	j.Conditions.Window = &TimeWindow{Start: "22:00", End: "06:00"}
	if _, fe := NormalizeJob(j, validEnv()); len(fe) > 0 {
		t.Errorf("a window over midnight rejected: %v", fe)
	}
	j.Conditions.WhenUnmet = "maybe"
	mustInvalid(t, j, "conditions.when_unmet", FieldInvalid)
	j.Conditions.WhenUnmet = UnmetWait
	j.Conditions.WaitMaxMin = 1
	mustInvalid(t, j, "conditions.wait_max_min", FieldOutOfRange)
}

func TestGuardRetryOptionRanges(t *testing.T) {
	j := sampleJob("x")
	j.Guards.DeletePct = 101
	mustInvalid(t, j, "guards.delete_pct", FieldOutOfRange)
	j = sampleJob("x")
	j.Guards.ChangePct = -3
	mustInvalid(t, j, "guards.change_pct", FieldOutOfRange)
	j = sampleJob("x")
	j.Retry.Max = maxRetryMax + 1
	mustInvalid(t, j, "retry.max", FieldOutOfRange)
	j = sampleJob("x")
	j.Retry.BackoffSec = []int{60, 1}
	mustInvalid(t, j, "retry.backoff_sec[1]", FieldOutOfRange)
	j = sampleJob("x")
	j.Options.MaxDurationSec = 5
	mustInvalid(t, j, "options.max_duration_sec", FieldOutOfRange)
	j = sampleJob("x")
	j.Type = TypeArchive
	j.Retention.KeepLast = -1
	mustInvalid(t, j, "retention.keep_last", FieldOutOfRange)

	// Retention belongs to one type each.
	j = sampleJob("x")
	j.Type = TypeArchive
	j.Retention = Retention{VersionsDays: 30}
	norm, fe := NormalizeJob(j, validEnv())
	if len(fe) > 0 || norm.Retention.VersionsDays != 0 || norm.Retention.KeepLast != DefaultKeepLast {
		t.Errorf("archive retention: %+v %v", norm.Retention, fe)
	}
	j = sampleJob("x")
	j.Retention = Retention{VersionsDays: 30, KeepLast: 4}
	norm, _ = NormalizeJob(j, validEnv())
	if norm.Retention != (Retention{}) {
		t.Errorf("copy retention kept: %+v", norm.Retention)
	}
}

func TestFilterValidation(t *testing.T) {
	j := sampleJob("x")
	j.Filters.ExcludePresets = []string{"caches", "caches", "bogus"}
	mustInvalid(t, j, "filters.exclude_presets[2]", FieldInvalid)

	j = sampleJob("x")
	j.Filters.Exclude = []string{"*.tmp", "  ", "{unclosed"}
	mustInvalid(t, j, "filters.exclude[2]", ErrInvalidFilter)

	j = sampleJob("x")
	j.Filters.ExcludePresets = []string{"caches", "caches"}
	j.Filters.Exclude = []string{" *.tmp ", ""}
	norm, fe := NormalizeJob(j, validEnv())
	if len(fe) > 0 {
		t.Fatal(fe)
	}
	if len(norm.Filters.ExcludePresets) != 1 || len(norm.Filters.Exclude) != 1 || norm.Filters.Exclude[0] != "*.tmp" {
		t.Errorf("filters not cleaned: %+v", norm.Filters)
	}
	if norm.Filters.Include != nil {
		t.Errorf("empty include kept: %#v", norm.Filters.Include)
	}
}

func TestHookValidation(t *testing.T) {
	j := sampleJob("x")
	j.Hooks = []Hook{{Phase: HookPre, Action: HookStopApps, Apps: []string{"ghost"}}}
	mustInvalid(t, j, "hooks[0].apps[0]", FieldUnknownApp)

	j.Hooks = []Hook{{Phase: HookPre, Action: HookStopApps, Apps: []string{"../x"}}}
	mustInvalid(t, j, "hooks[0].apps[0]", FieldInvalid)

	j.Hooks = []Hook{{Phase: HookPre, Action: HookShutdownVM, VM: "nope"}}
	mustInvalid(t, j, "hooks[0].vm", FieldUnknownVM)

	j.Hooks = []Hook{{Phase: "during", Action: HookStopApps, Apps: []string{"immich"}}}
	mustInvalid(t, j, "hooks[0].phase", FieldInvalid)

	j.Hooks = []Hook{{Phase: HookPre, Action: "reboot"}}
	mustInvalid(t, j, "hooks[0].action", FieldInvalid)

	j.Hooks = []Hook{{Phase: HookPre, Action: HookStopApps, Apps: []string{"immich"}, TimeoutSec: 1}}
	mustInvalid(t, j, "hooks[0].timeout_sec", FieldOutOfRange)

	// one_at_a_time only splits an archive of app folders.
	j.Hooks = []Hook{{Phase: HookPre, Action: HookStopApps, Apps: []string{"immich"}, AppMode: AppModeOneAtATime}}
	mustInvalid(t, j, "hooks[0].app_mode", FieldInvalid)

	j = sampleJob("x")
	j.Type = TypeArchive
	j.Sources = []Endpoint{
		{Kind: EPVolume, RefID: "root-uuid", SubPath: "DATA/AppData/immich", Preset: "appdata:immich"},
		{Kind: EPVolume, RefID: "root-uuid", SubPath: "DATA/AppData/blinko", Preset: "appdata:blinko"},
	}
	j.Hooks = []Hook{{Phase: HookPre, Action: HookStopApps, Apps: []string{"immich", "blinko", "immich"}, AppMode: AppModeOneAtATime}}
	norm, fe := NormalizeJob(j, validEnv())
	if len(fe) > 0 {
		t.Fatalf("one_at_a_time archive rejected: %v", fe)
	}
	h := norm.Hooks[0]
	if len(h.Apps) != 2 || h.TimeoutSec != DefaultHookTimeoutSec || h.FailPolicy != FailAbort {
		t.Errorf("hook not normalised: %+v", h)
	}

	// VM hooks get the longer default timeout; an unreachable VM Manager
	// doesn't block saving (the hook fails at run time instead).
	j = sampleJob("x")
	j.Hooks = []Hook{{Phase: HookPre, Action: HookShutdownVM, VM: "win11", Apps: []string{"immich"}}}
	env := validEnv()
	env.VMs = func() (map[string]bool, error) { return nil, errors.New("vm-sidecar down") }
	norm, fe = NormalizeJob(j, env)
	if len(fe) > 0 || norm.Hooks[0].TimeoutSec != DefaultVMHookTimeoutSec || norm.Hooks[0].Apps != nil {
		t.Errorf("vm hook: %+v %v", norm.Hooks[0], fe)
	}
}

func TestNameValidation(t *testing.T) {
	j := sampleJob("x")
	j.Name = "   "
	mustInvalid(t, j, "name", FieldRequired)
	j.Name = "a\nb"
	mustInvalid(t, j, "name", FieldInvalid)
	j.Name = strings.Repeat("é", maxNameLen+1)
	mustInvalid(t, j, "name", FieldInvalid)
	j.Type = "twoway"
	mustInvalid(t, j, "type", FieldInvalid)
}

func TestDefaultStaleHours(t *testing.T) {
	for cron, want := range map[string]int{
		"0 3 * * *":    DefaultStaleMinHours, // daily: 2 x 24 = 48
		"0 3 * * 0":    14 * 24,              // weekly: 2 x 168
		"*/10 * * * *": DefaultStaleMinHours, // never below 48
		"0 3 1 * *":    2 * 31 * 24,          // monthly: the longest month counts
		"0 3 * * 1-5":  2 * 72,               // weekdays: the weekend gap counts
		"@every 6h":    DefaultStaleMinHours,
		// Yearly: 2 years would be out of range, and the job could be
		// saved but never run.
		"0 3 1 1 *": maxStaleHours,
	} {
		j := sampleJob("x")
		j.Triggers = []Trigger{{Kind: TriggerSchedule, Cron: cron}}
		if got := defaultStaleHours(j); got != want {
			t.Errorf("defaultStaleHours(%q) = %d, want %d", cron, got, want)
		}
	}
	if got := defaultStaleHours(sampleJob("manual")); got != 14*24 {
		t.Errorf("manual job stale default = %d", got)
	}
}

func TestEndpointsOverlap(t *testing.T) {
	a := Endpoint{Kind: EPVolume, RefID: "u", SubPath: "DATA"}
	cases := []struct {
		b    Endpoint
		want bool
	}{
		{Endpoint{Kind: EPVolume, RefID: "u", SubPath: "DATA/x"}, true},
		{Endpoint{Kind: EPVolume, RefID: "u", SubPath: ""}, true},
		{Endpoint{Kind: EPVolume, RefID: "u", SubPath: "DATA"}, true},
		{Endpoint{Kind: EPVolume, RefID: "u", SubPath: "DATAX"}, false},
		{Endpoint{Kind: EPVolume, RefID: "v", SubPath: "DATA"}, false},
		{Endpoint{Kind: EPUSB, RefID: "u", SubPath: "DATA"}, false},
	}
	for _, c := range cases {
		if got := endpointsOverlap(a, c.b); got != c.want {
			t.Errorf("overlap(%v, %v) = %v", a, c.b, got)
		}
	}
}
