package main

// What the API reads and changes: the status snapshot, per-fan settings,
// presets, the critical temperatures and the identify step.

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"
)

type FanStatus struct {
	ID           string  `json:"id"`
	Label        string  `json:"label"`
	DefaultLabel string  `json:"default_label"`
	Kind         string  `json:"kind"`  // hwmon | nvidia
	Class        string  `json:"class"` // motherboard | gpu | laptop | board
	Chip         string  `json:"chip"`
	Channel      string  `json:"channel"`
	RPM          *int    `json:"rpm"`
	Pct          *int    `json:"pct"`
	Mode         string  `json:"mode"`
	State        string  `json:"state"` // auto | manual | emergency | readonly | identifying
	FixedPct     int     `json:"fixed_pct"`
	Curve        Curve   `json:"curve"`
	Source       string  `json:"source"`
	MinPct       int     `json:"min_pct"`
	FloorPct     int     `json:"floor_pct"` // lowest min_pct this fan accepts
	HystC        float64 `json:"hysteresis_c"`
	Controllable bool    `json:"controllable"`
	ReadOnly     string  `json:"readonly_reason,omitempty"`
	HasTach      bool    `json:"has_tach"`
	Tach         string  `json:"tach,omitempty"`
	CanIdentify  bool    `json:"can_identify"`
	Alert        string  `json:"alert,omitempty"`
	AlertAt      int64   `json:"alert_at,omitempty"`
}

type TempStatus struct {
	ID     string   `json:"id"`
	Label  string   `json:"label"`
	C      *float64 `json:"c"`
	Crit   int      `json:"critical_c"`
	Hot    bool     `json:"hot"`
	Detail string   `json:"detail,omitempty"`
}

type Source struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type Status struct {
	Controllable bool         `json:"controllable"`
	Emergency    bool         `json:"emergency"`
	CritCPU      int          `json:"critical_cpu_c"`
	CritGPU      int          `json:"critical_gpu_c"`
	Limits       Limits       `json:"limits"`
	Preset       string       `json:"preset"`
	Presets      []string     `json:"presets"`
	Fans         []FanStatus  `json:"fans"`
	Temps        []TempStatus `json:"temps"`
	Sources      []Source     `json:"sources"`
	Notes        []Note       `json:"notes"`
}

type Limits struct {
	MinPct     int `json:"min_pct"`
	MinCritC   int `json:"min_critical_c"`
	MaxCritCPU int `json:"max_critical_cpu_c"`
	MaxCritGPU int `json:"max_critical_gpu_c"`
	MaxPoints  int `json:"max_points"`
	MinPoints  int `json:"min_points"`
	MinTempC   int `json:"min_temp_c"`
	MaxTempC   int `json:"max_temp_c"`
	MaxHystC   int `json:"max_hysteresis_c"`
}

func roundPtr(f float64) *float64 { v := math.Round(f*10) / 10; return &v }

func (c *Controller) configFor(cs *chanState) FanConfig {
	fc := FanConfig{Mode: modeAuto, MinPct: DefaultMinPct, HystC: DefaultHystC}
	if p := c.settings.Fans[cs.ch.ID]; p != nil {
		fc = *p
	}
	if fc.Source == "" {
		fc.Source = cs.ch.DefaultSrc
	}
	if fc.MinPct < cs.ch.HWMinPct {
		fc.MinPct = cs.ch.HWMinPct
	}
	if len(fc.Curve) == 0 {
		fc.Curve = fitCurve(presetCurve("balanced", cs.ch.Class == "gpu" || strings.HasPrefix(fc.Source, "gpu:")), fc.MinPct)
	}
	if fc.FixedPct == 0 {
		fc.FixedPct = 50
		if fc.FixedPct < fc.MinPct {
			fc.FixedPct = fc.MinPct
		}
	}
	return fc
}

func (c *Controller) readOnlyReason(cs *chanState) string {
	if !c.isRoot {
		return "The fan service is not running as root, so it can only watch the fans."
	}
	if cs.ch.ReadOnly != "" {
		return cs.ch.ReadOnly
	}
	if cs.writeRO != "" {
		return cs.writeRO
	}
	return c.protectedLocked(cs)
}

// freshLocked makes sure the temperatures are current before a change is
// judged against them (NVML may have been idle).
func (c *Controller) freshLocked() {
	c.lastStatus = c.now()
	c.readTempsLocked()
}

// Status is the snapshot the dashboard and the app poll.
func (c *Controller) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.now().Sub(c.lastStatus) >= statusWindow {
		// First look in a while: GPU readings are stale (NVML was idle).
		c.lastStatus = c.now()
		c.readTempsLocked()
		for _, cs := range c.chans {
			if cs.ch.Kind == kindNVIDIA {
				cs.duty, cs.rpm = cs.ch.io.read()
			}
		}
	}
	c.lastStatus = c.now()
	return c.statusLocked()
}

func (c *Controller) statusLocked() Status {
	s := Status{
		Emergency: c.emergencyLocked(),
		CritCPU:   c.settings.CritCPU,
		CritGPU:   c.settings.CritGPU,
		Preset:    c.settings.Preset,
		Presets:   append([]string{modeAuto}, presetNames...),
		Limits: Limits{MinPct: HardMinPct, MinCritC: MinCritC, MaxCritCPU: MaxCritCPU, MaxCritGPU: MaxCritGPU,
			MaxPoints: MaxCurvePoints, MinPoints: MinCurvePoints, MinTempC: MinCurveTempC, MaxTempC: MaxCurveTempC, MaxHystC: MaxHystC},
		Fans:    []FanStatus{},
		Temps:   []TempStatus{},
		Sources: []Source{},
		Notes:   append([]Note{}, c.detect.notes()...),
	}
	if s.Preset == "" {
		s.Preset = modeAuto
	}
	cpu := TempStatus{ID: "cpu", Label: "CPU", Crit: c.settings.CritCPU, Hot: c.hotCPU, Detail: c.temps.CPUName}
	if c.temps.CPUOK {
		cpu.C = roundPtr(c.temps.CPU)
	}
	s.Temps = append(s.Temps, cpu)
	s.Sources = append(s.Sources, Source{ID: "cpu", Label: "CPU"})
	gids := make([]string, 0, len(c.temps.GPUName))
	for id := range c.temps.GPUName {
		gids = append(gids, id)
	}
	sortStrings(gids)
	for _, id := range gids {
		t := TempStatus{ID: id, Label: c.temps.GPUName[id], Crit: c.settings.CritGPU, Hot: c.hotGPU[id]}
		if v, ok := c.temps.GPUs[id]; ok {
			t.C = roundPtr(v)
		}
		s.Temps = append(s.Temps, t)
		s.Sources = append(s.Sources, Source{ID: id, Label: c.temps.GPUName[id]})
	}
	if len(gids) > 0 {
		s.Sources = append(s.Sources, Source{ID: "max", Label: "Hottest of CPU and GPU"})
	}
	if !c.temps.CPUOK {
		s.Notes = append(s.Notes, Note{Level: "warn", Code: "no_cpu_temp", Message: "No CPU temperature sensor was found, so motherboard fans stay on Auto (NivaroOS won't drive a fan it can't protect)."})
	}
	if c.gpuErr != "" {
		s.Notes = append(s.Notes, Note{Level: "info", Code: "nvml", Message: "NVIDIA fans: " + c.gpuErr})
	}
	if !c.isRoot {
		s.Notes = append(s.Notes, Note{Level: "warn", Code: "not_root", Message: "The fan service is not running as root, so fans are read-only."})
	}
	for _, id := range c.order {
		cs := c.chans[id]
		fc := c.configFor(cs)
		label := fc.Label
		if label == "" {
			label = cs.ch.DefaultLabel
		}
		f := FanStatus{
			ID: id, Label: label, DefaultLabel: cs.ch.DefaultLabel, Kind: cs.ch.Kind, Class: cs.ch.Class,
			Chip: cs.ch.Chip, Channel: cs.ch.Channel, Mode: fc.Mode, FixedPct: fc.FixedPct, Curve: fc.Curve,
			Source: fc.Source, MinPct: fc.MinPct, FloorPct: cs.ch.HWMinPct, HystC: fc.HystC,
			Alert: fc.Alert, AlertAt: fc.AlertAt, HasTach: fc.TachSeen || cs.rpm > 0,
		}
		if f.FloorPct < HardMinPct {
			f.FloorPct = HardMinPct
		}
		if cs.rpm >= 0 {
			r := cs.rpm
			f.RPM = &r
		}
		if cs.duty >= 0 {
			p := int(math.Round(cs.duty))
			f.Pct = &p
		}
		if hio, ok := cs.ch.io.(*hwmonIO); ok && hio.tach > 0 {
			f.Tach = fmt.Sprintf("fan%d", hio.tach)
		}
		f.ReadOnly = c.readOnlyReason(cs)
		f.Controllable = f.ReadOnly == ""
		f.CanIdentify = f.Controllable && cs.ch.Kind == kindHwmon && len(cs.ch.TachChoices) > 0
		switch {
		case cs.identifying:
			f.State = "identifying"
		case f.ReadOnly != "" && !cs.controlled:
			f.State = "readonly"
		case cs.controlled && cs.emergency:
			f.State = "emergency"
		case cs.controlled:
			f.State = "manual"
		default:
			f.State = modeAuto
		}
		if f.Controllable {
			s.Controllable = true
		}
		s.Fans = append(s.Fans, f)
	}
	if len(s.Fans) == 0 {
		s.Notes = append(s.Notes, Note{Level: "info", Code: "no_fans", Message: "No controllable fan was found on this machine."})
	}
	return s
}

func sortStrings(a []string) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j] < a[j-1]; j-- {
			a[j], a[j-1] = a[j-1], a[j]
		}
	}
}

// FanUpdate is POST /v1/fans/fan. Absent fields are left alone.
type FanUpdate struct {
	ID       string   `json:"id"`
	Label    *string  `json:"label"`
	Mode     *string  `json:"mode"`
	FixedPct *int     `json:"fixed_pct"`
	Curve    Curve    `json:"curve"`
	Source   *string  `json:"source"`
	MinPct   *int     `json:"min_pct"`
	HystC    *float64 `json:"hysteresis_c"`
	// Tach: "fan3" to set which speed input belongs to this pwm, "" for
	// the default.
	Tach *string `json:"tach"`
}

type userError struct{ msg string }

func (e userError) Error() string { return e.msg }

func badf(f string, a ...interface{}) error { return userError{fmt.Sprintf(f, a...)} }

var errUnknownFan = userError{"no such fan"}

func (c *Controller) UpdateFan(u FanUpdate) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	cs, ok := c.chans[u.ID]
	if !ok {
		return errUnknownFan
	}
	c.freshLocked()
	fc := c.configFor(cs)
	if u.Label != nil {
		l := strings.TrimSpace(*u.Label)
		if len([]rune(l)) > 40 {
			return badf("a fan name can be at most 40 characters")
		}
		if l == cs.ch.DefaultLabel {
			l = ""
		}
		fc.Label = l
	}
	if u.MinPct != nil {
		floor := cs.ch.HWMinPct
		if floor < HardMinPct {
			floor = HardMinPct
		}
		if *u.MinPct < floor || *u.MinPct > 100 {
			return badf("the minimum speed of this fan must be between %d%% and 100%%", floor)
		}
		fc.MinPct = *u.MinPct
		if fc.FixedPct < fc.MinPct {
			fc.FixedPct = fc.MinPct
		}
		fc.Curve = fitCurve(fc.Curve, fc.MinPct)
	}
	if u.HystC != nil {
		if *u.HystC < 0 || *u.HystC > MaxHystC || math.IsNaN(*u.HystC) {
			return badf("hysteresis must be between 0 and %d °C", MaxHystC)
		}
		fc.HystC = *u.HystC
	}
	if u.Source != nil {
		if !c.validSourceLocked(*u.Source) {
			return badf("unknown temperature source")
		}
		fc.Source = *u.Source
	}
	if u.FixedPct != nil {
		if *u.FixedPct < fc.MinPct || *u.FixedPct > 100 {
			return badf("a fixed speed must be between %d%% (this fan's minimum) and 100%%", fc.MinPct)
		}
		fc.FixedPct = *u.FixedPct
	}
	if u.Curve != nil {
		if err := validateCurve(u.Curve, fc.MinPct); err != nil {
			return userError{err.Error()}
		}
		fc.Curve = append(Curve{}, u.Curve...)
	}
	if u.Tach != nil {
		if cs.ch.Kind != kindHwmon {
			return badf("this fan has no speed input to choose")
		}
		if *u.Tach == "" {
			fc.Tach, fc.TachSet = 0, false
		} else {
			var n int
			if _, err := fmt.Sscanf(*u.Tach, "fan%d", &n); err != nil || !containsInt(cs.ch.TachChoices, n) {
				return badf("unknown speed input %q", *u.Tach)
			}
			fc.Tach, fc.TachSet = n, true
		}
		fc.TachSeen = false
		if hio, ok := cs.ch.io.(*hwmonIO); ok {
			hio.tach = cs.ch.TachIndex
			if fc.TachSet {
				hio.tach = fc.Tach
			}
		}
	}
	if u.Mode != nil {
		switch *u.Mode {
		case modeAuto, modeFixed, modeCurve:
		default:
			return badf("mode must be auto, fixed or curve")
		}
		if *u.Mode != modeAuto {
			if r := c.readOnlyReason(cs); r != "" {
				return userError{r}
			}
		}
		if *u.Mode == modeCurve {
			if err := validateCurve(fc.Curve, fc.MinPct); err != nil {
				return userError{err.Error()}
			}
		}
		fc.Mode = *u.Mode
		fc.Alert, fc.AlertAt = "", 0
		c.settings.Preset = "custom"
	}
	if fc.Source == cs.ch.DefaultSrc {
		fc.Source = ""
	}
	nfc := fc
	c.settings.Fans[u.ID] = &nfc
	c.normalizePresetLocked()
	c.saveLocked()
	return nil
}

func containsInt(a []int, v int) bool {
	for _, x := range a {
		if x == v {
			return true
		}
	}
	return false
}

func (c *Controller) validSourceLocked(src string) bool {
	if src == "cpu" || src == "max" || src == "" {
		return true
	}
	_, ok := c.temps.GPUName[src]
	return ok
}

// normalizePresetLocked: "auto" when every fan is on Auto.
func (c *Controller) normalizePresetLocked() {
	for _, fc := range c.settings.Fans {
		if fc.Mode != modeAuto {
			return
		}
	}
	c.settings.Preset = modeAuto
}

// ApplyPreset sets every controllable fan to Auto or to a preset curve.
func (c *Controller) ApplyPreset(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if name != modeAuto && !containsStr(presetNames, name) {
		return badf("unknown preset")
	}
	c.freshLocked()
	for _, id := range c.order {
		cs := c.chans[id]
		fc := c.configFor(cs)
		if name == modeAuto {
			fc.Mode = modeAuto
		} else {
			if c.readOnlyReason(cs) != "" {
				continue
			}
			gpu := cs.ch.Class == "gpu" || strings.HasPrefix(fc.Source, "gpu:")
			fc.Curve = fitCurve(presetCurve(name, gpu), fc.MinPct)
			fc.Mode = modeCurve
		}
		fc.Alert, fc.AlertAt = "", 0
		if fc.Source == cs.ch.DefaultSrc {
			fc.Source = ""
		}
		nfc := fc
		c.settings.Fans[id] = &nfc
	}
	c.settings.Preset = name
	c.saveLocked()
	return nil
}

func containsStr(a []string, v string) bool {
	for _, x := range a {
		if x == v {
			return true
		}
	}
	return false
}

func (c *Controller) SetCritical(cpu, gpu *int) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cpu != nil {
		if *cpu < MinCritC || *cpu > MaxCritCPU {
			return badf("the CPU emergency temperature must be between %d and %d °C", MinCritC, MaxCritCPU)
		}
		c.settings.CritCPU = *cpu
	}
	if gpu != nil {
		if *gpu < MinCritC || *gpu > MaxCritGPU {
			return badf("the GPU emergency temperature must be between %d and %d °C", MinCritC, MaxCritGPU)
		}
		c.settings.CritGPU = *gpu
	}
	c.saveLocked()
	return nil
}

// ---- identify ----

type TachChange struct {
	Tach   string `json:"tach"`
	Before int    `json:"before_rpm"`
	After  int    `json:"after_rpm"`
}

type IdentifyResult struct {
	ID      string       `json:"id"`
	Tach    string       `json:"tach"` // "" when no speed input reacted
	Pct     int          `json:"test_pct"`
	Changes []TachChange `json:"changes"`
	Message string       `json:"message"`
}

const identifyHold = 7 * time.Second

// Identify briefly changes one fan's speed - up to 100%, or down to 50% if
// it already runs at 80% or more (never below its floor) - and reports
// which speed input reacted, so the user knows which header this is. It
// runs only when asked, is aborted by an emergency, and ends with the fan
// back in its previous mode.
func (c *Controller) Identify(id string) (IdentifyResult, error) {
	c.mu.Lock()
	cs, ok := c.chans[id]
	if !ok {
		c.mu.Unlock()
		return IdentifyResult{}, errUnknownFan
	}
	if r := c.readOnlyReason(cs); r != "" {
		c.mu.Unlock()
		return IdentifyResult{}, userError{r}
	}
	if cs.ch.Kind != kindHwmon || len(cs.ch.TachChoices) == 0 {
		c.mu.Unlock()
		return IdentifyResult{}, badf("this fan has no speed inputs to watch")
	}
	if cs.identifying {
		c.mu.Unlock()
		return IdentifyResult{}, badf("this fan is being identified already")
	}
	if c.emergencyLocked() {
		c.mu.Unlock()
		return IdentifyResult{}, badf("not while the machine is at its emergency temperature")
	}
	fc := c.configFor(cs)
	cs.identifying = true
	dir := cs.ch.hwmonDir
	tachs := cs.ch.TachChoices
	duty, _ := cs.ch.io.read()
	test := 100
	if duty >= 80 {
		test = 50
	}
	test = clampPct(test, c.minFor(cs, &fc))
	c.mu.Unlock()

	sample := func() map[int]int {
		m := map[int]int{}
		for _, t := range tachs {
			if v, err := readInt(filepath.Join(dir, fmt.Sprintf("fan%d_input", t))); err == nil && v >= 0 && v < 30000 {
				m[t] = int(v)
			}
		}
		return m
	}
	before := sample()

	c.mu.Lock()
	err := c.applyLocked(cs, test)
	c.mu.Unlock()
	var after map[int]int
	aborted := false
	if err == nil {
		for waited := time.Duration(0); waited < identifyHold; waited += time.Second {
			c.sleep(time.Second)
			c.mu.Lock()
			hot := c.emergencyLocked()
			c.mu.Unlock()
			if hot {
				aborted = true
				break
			}
		}
		after = sample()
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	cs.identifying = false
	cs.applied = -1 // the loop re-applies the fan's own mode at once
	cs.graceUntil = c.now().Add(8 * time.Second)
	if fc.Mode == modeAuto {
		c.releaseLocked(cs, "")
	} else if c.emergencyLocked() {
		_ = c.applyLocked(cs, 100)
	}
	if err != nil {
		return IdentifyResult{}, userError{"couldn't change this fan's speed: " + err.Error()}
	}
	if aborted {
		return IdentifyResult{}, badf("stopped: the machine reached its emergency temperature")
	}
	res := IdentifyResult{ID: id, Pct: test, Changes: []TachChange{}}
	best, bestDelta := 0, 0
	for _, t := range tachs {
		b, a := before[t], after[t]
		res.Changes = append(res.Changes, TachChange{Tach: fmt.Sprintf("fan%d", t), Before: b, After: a})
		d := a - b
		if d < 0 {
			d = -d
		}
		need := 150
		if b/7 > need {
			need = b / 7
		}
		if d >= need && d > bestDelta {
			best, bestDelta = t, d
		}
	}
	if best == 0 {
		res.Message = "No fan speed changed. Either nothing is connected to this header, or the fan has no speed signal (3-pin fans on some headers)."
		return res, nil
	}
	res.Tach = fmt.Sprintf("fan%d", best)
	res.Message = fmt.Sprintf("This channel drives the fan reported as %s.", res.Tach)
	// Remember the mapping (stall detection watches that input).
	nfc := c.configFor(cs)
	nfc.Tach, nfc.TachSet, nfc.TachSeen = best, true, true
	if nfc.Source == cs.ch.DefaultSrc {
		nfc.Source = ""
	}
	c.settings.Fans[id] = &nfc
	if hio, ok := cs.ch.io.(*hwmonIO); ok {
		hio.tach = best
	}
	c.saveLocked()
	return res, nil
}

// AllAuto hands every fan back now and saves Auto for all.
func (c *Controller) AllAuto() error { return c.ApplyPreset(modeAuto) }

// Redetect re-runs driver detection (admin action) and rescans.
func (c *Controller) Redetect(d detector) {
	res := d.run(true)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.detect = res
	c.rescanLocked()
}
