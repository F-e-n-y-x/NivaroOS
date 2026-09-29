package main

// The control loop and every safety rule:
//
//   - Auto is the default for every fan and means "not ours": the channel
//     is left exactly as the firmware/driver set it, or put back exactly
//     so (pwmN_enable and pwmN as captured / NVML default policy).
//   - Nothing is ever driven below the fan's floor (at least 20%).
//   - Emergency: when the CPU or any GPU reaches its critical temperature
//     every fan we drive goes to 100% until it is EmergencyClearC below.
//   - A fan we drive that reads 0 RPM for StallTicks ticks (while it has
//     shown a speed before) is stalled: it goes back to Auto, with an alert.
//   - No trusted CPU temperature (or, for a GPU fan, that GPU's) means no
//     protection, so such a fan is not taken over at all; a sensor lost
//     while driving hands the fan back.
//   - Repeated write errors hand the fan back.
//   - Stop, crash, restart and shutdown hand every fan back (Shutdown here,
//     `nivaroos-fans restore` as ExecStopPost, restoreLeftovers at start).

import (
	"errors"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

type fanIO interface {
	read() (duty float64, rpm int)
	capture(id string) (original, error)
	set(pct int) error
}

// channel is what detection found; chanState what the loop knows about it.
type channel struct {
	ID           string
	Kind         string // hwmon | nvidia
	Class        string // motherboard | gpu | laptop | board
	Chip         string
	Channel      string // pwm2, fan0
	DefaultLabel string
	DefaultSrc   string
	HWMinPct     int
	ReadOnly     string
	TachIndex    int   // default tach (pwmN -> fanN) for hwmon
	TachChoices  []int // fanN_input on the same chip
	hwmonDir     string
	gpuUUID      string
	gpuFan       int
	io           fanIO
}

type chanState struct {
	ch          channel
	controlled  bool
	orig        original
	applied     int
	lastWrite   time.Time
	hyst        hysteresis
	zeroTicks   int
	graceUntil  time.Time
	writeErrs   int
	duty        float64
	rpm         int
	identifying bool
	writeRO     string // read-only learned from a failed write
	emergency   bool
}

type tempState struct {
	CPU     float64
	CPUOK   bool
	CPUName string
	GPUs    map[string]float64 // source id -> °C
	GPUName map[string]string
}

type Note struct {
	Level    string `json:"level"` // info | warn | error
	Code     string `json:"code"`
	Message  string `json:"message"`
	Guidance string `json:"guidance,omitempty"`
}

type Controller struct {
	mu       sync.Mutex
	sysRoot  string
	gpu      *lazyGPU
	store    *store
	settings Settings
	chans    map[string]*chanState
	order    []string
	temps    tempState
	cpuSens  *cpuSensor
	hotCPU   bool
	hotGPU   map[string]bool
	ticks    int
	detect   detectResult
	gpuErr   string
	// NVML is only touched while it is needed: some fan is off Auto (the
	// GPU temperature feeds the emergency), or someone looked at the
	// status in the last statusWindow. lastGPUs is the last device list.
	lastStatus time.Time
	gpuDone    bool
	lastGPUs   []gpuDevice
	isRoot     bool

	now   func() time.Time
	sleep func(time.Duration)
	// notify is called once per completed tick (the systemd watchdog).
	notify func()
}

func newController(sysRoot string, st *store, gpu *lazyGPU, isRoot bool) *Controller {
	return &Controller{
		sysRoot: sysRoot,
		gpu:     gpu,
		store:   st,
		chans:   map[string]*chanState{},
		hotGPU:  map[string]bool{},
		isRoot:  isRoot,
		now:     time.Now,
		sleep:   time.Sleep,
		notify:  func() {},
	}
}

// Start: hand back anything a previous run left driven, load settings and
// find the fans. The loop itself is run by the caller (tick).
func (c *Controller) Start() {
	if n, err := restoreLeftovers(c.sysRoot, c.store, c.gpu); n > 0 || err != nil {
		log.Printf("fans: handed %d fan(s) left over by a previous run back to Auto (err=%v)", n, err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.settings = c.store.loadSettings()
	c.rescanLocked()
	c.observeLocked()
}

// ---- detection ----

func (c *Controller) rescanLocked() {
	devs := scanHwmon(c.sysRoot)
	c.cpuSens = findCPUSensor(c.sysRoot, devs)
	var found []channel
	gpuNames := map[string]string{}
	amdCount := 0
	for _, d := range devs {
		idx := pwmIndexes(d.Dir)
		if isGPUHwmon(d.Name) {
			amdCount++
			gpuNames[gpuSourceAMD(d)] = fmt.Sprintf("AMD GPU %d", amdCount)
		}
		if len(idx) == 0 {
			continue
		}
		tachs := fanInputs(d.Dir)
		for _, n := range idx {
			ch := channel{
				ID:          hwmonChannelID(d, n),
				Kind:        kindHwmon,
				Chip:        d.Name,
				Channel:     fmt.Sprintf("pwm%d", n),
				HWMinPct:    HardMinPct,
				TachChoices: tachs,
				hwmonDir:    d.Dir,
			}
			for _, t := range tachs {
				if t == n {
					ch.TachIndex = n
				}
			}
			switch {
			case isGPUHwmon(d.Name):
				ch.Class = "gpu"
				ch.DefaultSrc = gpuSourceAMD(d)
				ch.DefaultLabel = gpuNames[ch.DefaultSrc] + " fan"
			case laptopHwmon[d.Name] != "":
				ch.Class = "laptop"
				ch.DefaultSrc = "cpu"
				ch.DefaultLabel = fmt.Sprintf("%s fan %d", laptopHwmon[d.Name], n)
			case d.Name == "pwmfan" || d.Name == "pwm-fan" || strings.HasPrefix(d.Name, "pwmfan"):
				ch.Class = "board"
				ch.DefaultSrc = "cpu"
				ch.DefaultLabel = "Board fan"
			default:
				ch.Class = "motherboard"
				ch.DefaultSrc = "cpu"
				ch.DefaultLabel = fmt.Sprintf("Fan header %d", n)
			}
			ch.ReadOnly = hwmonReadOnlyReason(c.sysRoot, d, n)
			found = append(found, ch)
		}
	}

	// NVIDIA through NVML.
	c.gpuErr = ""
	gpus := c.lastGPUs
	if c.gpu != nil && (!c.gpuDone || c.wantGPULocked()) {
		var err error
		gpus, err = c.gpu.Devices()
		if err != nil && !errors.Is(err, errNoNVML) {
			c.gpuErr = err.Error()
		}
		c.gpuDone, c.lastGPUs = true, gpus
	}
	for i, g := range gpus {
		src := gpuSourceNV(g.UUID)
		name := g.Name
		if name == "" {
			name = fmt.Sprintf("NVIDIA GPU %d", i+1)
		}
		gpuNames[src] = name
		for f := 0; f < g.NumFans; f++ {
			ch := channel{
				ID:         fmt.Sprintf("nv-%s-fan%d", slug(g.UUID), f),
				Kind:       kindNVIDIA,
				Class:      "gpu",
				Chip:       name,
				Channel:    fmt.Sprintf("fan%d", f),
				DefaultSrc: src,
				HWMinPct:   HardMinPct,
				gpuUUID:    g.UUID,
				gpuFan:     f,
			}
			if g.MinPct > ch.HWMinPct {
				ch.HWMinPct = g.MinPct
			}
			ch.DefaultLabel = name + " fan"
			if g.NumFans > 1 {
				ch.DefaultLabel = fmt.Sprintf("%s fan %d", name, f+1)
			}
			if !g.CanControl {
				ch.ReadOnly = g.Reason
			}
			found = append(found, ch)
		}
	}
	c.temps.GPUName = gpuNames

	seen := map[string]bool{}
	var order []string
	for _, ch := range found {
		seen[ch.ID] = true
		order = append(order, ch.ID)
		if ch.Kind == kindHwmon {
			tach := ch.TachIndex
			if fc := c.settings.Fans[ch.ID]; fc != nil && fc.TachSet {
				tach = fc.Tach
			}
			ch.io = &hwmonIO{dir: ch.hwmonDir, n: pwmNum(ch.Channel), tach: tach}
		} else {
			ch.io = &nvidiaIO{gpu: c.gpu, uuid: ch.gpuUUID, fan: ch.gpuFan}
		}
		if cs, ok := c.chans[ch.ID]; ok {
			cs.ch = ch
		} else {
			c.chans[ch.ID] = &chanState{ch: ch, applied: -1, rpm: -1, duty: -1}
		}
	}
	for id, cs := range c.chans {
		if !seen[id] {
			if cs.controlled {
				c.releaseLocked(cs, "")
			}
			delete(c.chans, id)
		}
	}
	c.order = order
}

func pwmNum(s string) int {
	var n int
	fmt.Sscanf(s, "pwm%d", &n)
	return n
}

// ---- temperatures and emergency ----

const statusWindow = 15 * time.Second

func (c *Controller) wantGPULocked() bool {
	if c.now().Sub(c.lastStatus) < statusWindow {
		return true
	}
	for _, fc := range c.settings.Fans {
		if fc.Mode != modeAuto {
			return true
		}
	}
	for _, cs := range c.chans {
		if cs.controlled || cs.identifying {
			return true
		}
	}
	return false
}

func (c *Controller) readTempsLocked() {
	c.temps.CPU, c.temps.CPUOK = c.cpuSens.read()
	if c.cpuSens != nil {
		c.temps.CPUName = c.cpuSens.Source
	}
	g := map[string]float64{}
	for _, d := range scanHwmon(c.sysRoot) {
		if isGPUHwmon(d.Name) {
			if t, ok := amdgpuTemp(d); ok {
				g[gpuSourceAMD(d)] = t
			}
		}
	}
	if c.gpu != nil && c.wantGPULocked() {
		if devs, err := c.gpu.Devices(); err == nil {
			for _, d := range devs {
				if d.TempOK {
					g[gpuSourceNV(d.UUID)] = d.TempC
				}
			}
		}
	}
	c.temps.GPUs = g
}

func (c *Controller) updateEmergencyLocked() {
	crit := float64(c.settings.CritCPU)
	if c.temps.CPUOK {
		if c.temps.CPU >= crit {
			c.hotCPU = true
		} else if c.temps.CPU < crit-EmergencyClearC {
			c.hotCPU = false
		}
	}
	gcrit := float64(c.settings.CritGPU)
	for id, t := range c.temps.GPUs {
		if t >= gcrit {
			c.hotGPU[id] = true
		} else if t < gcrit-EmergencyClearC {
			delete(c.hotGPU, id)
		}
	}
	for id := range c.hotGPU {
		if _, ok := c.temps.GPUs[id]; !ok {
			delete(c.hotGPU, id)
		}
	}
}

func (c *Controller) emergencyLocked() bool { return c.hotCPU || len(c.hotGPU) > 0 }

// sourceTemp returns the temperature a fan steers by.
func (c *Controller) sourceTemp(src string) (float64, bool) {
	switch {
	case src == "" || src == "cpu":
		return c.temps.CPU, c.temps.CPUOK
	case src == "max":
		best, ok := c.temps.CPU, c.temps.CPUOK
		for _, t := range c.temps.GPUs {
			if !ok || t > best {
				best, ok = t, true
			}
		}
		return best, ok
	default:
		t, ok := c.temps.GPUs[src]
		return t, ok
	}
}

// protectedLocked: may this fan be driven at all? It needs the CPU
// temperature (emergency protection) and, for a GPU fan, its GPU's.
func (c *Controller) protectedLocked(cs *chanState) string {
	if !c.temps.CPUOK && cs.ch.Class != "gpu" {
		return "No CPU temperature sensor could be read, so NivaroOS can't protect against overheating and leaves this fan on Auto."
	}
	if cs.ch.Class == "gpu" {
		if _, ok := c.temps.GPUs[cs.ch.DefaultSrc]; !ok {
			return "This GPU's temperature can't be read, so NivaroOS leaves its fan on Auto."
		}
	}
	return ""
}

// ---- the loop ----

// observeLocked reads temperatures and fans without acting (status CLI).
func (c *Controller) observeLocked() {
	c.readTempsLocked()
	c.updateEmergencyLocked()
	for _, id := range c.order {
		cs := c.chans[id]
		cs.duty, cs.rpm = cs.ch.io.read()
	}
}

// Tick runs one control step (every tickInterval).
func (c *Controller) Tick() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ticks++
	if c.ticks%15 == 0 {
		c.rescanLocked()
	}
	c.readTempsLocked()
	c.updateEmergencyLocked()
	emergency := c.emergencyLocked()
	dirty := false
	keepGPU := false
	wantGPU := c.wantGPULocked()
	for _, id := range c.order {
		cs := c.chans[id]
		if cs.ch.Kind != kindNVIDIA || wantGPU {
			cs.duty, cs.rpm = cs.ch.io.read()
		}
		fc := c.settings.Fans[id]
		if cs.rpm > 0 && fc != nil && !fc.TachSeen {
			fc.TachSeen = true
			dirty = true
		} else if cs.rpm > 0 && fc == nil {
			fc = &FanConfig{Mode: modeAuto, MinPct: DefaultMinPct, HystC: DefaultHystC, TachSeen: true}
			c.settings.Fans[id] = fc
			dirty = true
		}
		if cs.identifying {
			if cs.ch.Kind == kindNVIDIA {
				keepGPU = true
			}
			continue
		}
		if fc == nil || fc.Mode == modeAuto || cs.ch.ReadOnly != "" || cs.writeRO != "" {
			if cs.controlled {
				c.releaseLocked(cs, "")
			}
			continue
		}
		if cs.ch.Kind == kindNVIDIA {
			keepGPU = true
		}
		if reason := c.protectedLocked(cs); reason != "" {
			if cs.controlled {
				c.releaseLocked(cs, "")
			}
			c.autoWithAlertLocked(fc, reason)
			dirty = true
			continue
		}
		target, ok := c.targetLocked(cs, fc)
		if !ok {
			c.releaseLocked(cs, "")
			c.autoWithAlertLocked(fc, "The temperature this fan follows can no longer be read, so it went back to Auto.")
			dirty = true
			continue
		}
		cs.emergency = emergency
		if emergency {
			target = 100
		} else if fc.Mode == modeCurve {
			target = ramp(cs.applied, target, RampUpPerTick, RampDownPerTick)
		}
		target = clampPct(target, c.minFor(cs, fc))
		if err := c.applyLocked(cs, target); err != nil {
			cs.writeErrs++
			log.Printf("fans: %s: %v", id, err)
			if cs.writeErrs >= WriteErrorsLimit || !cs.controlled {
				c.releaseLocked(cs, "")
				cs.writeRO = "The driver refused speed changes on this channel (" + err.Error() + ")."
				c.autoWithAlertLocked(fc, "NivaroOS couldn't set this fan's speed ("+err.Error()+"), so it went back to Auto.")
				dirty = true
			}
			continue
		}
		cs.writeErrs = 0
		cs.duty = float64(cs.applied)
		// Stall: driven, has shown a speed before, now 0 RPM.
		if fc.TachSeen && cs.rpm == 0 && c.now().After(cs.graceUntil) {
			cs.zeroTicks++
			if cs.zeroTicks >= StallTicks {
				c.releaseLocked(cs, "")
				c.autoWithAlertLocked(fc, fmt.Sprintf("This fan stopped (0 RPM) at %d%%, so it went back to Auto. Raise its minimum speed before trying again.", cs.applied))
				cs.zeroTicks = 0
				dirty = true
			}
		} else {
			cs.zeroTicks = 0
		}
	}
	if dirty {
		c.saveLocked()
	}
	if c.gpu != nil {
		c.gpu.idle(keepGPU)
	}
	c.notify()
}

func (c *Controller) minFor(cs *chanState, fc *FanConfig) int {
	m := fc.MinPct
	if m < cs.ch.HWMinPct {
		m = cs.ch.HWMinPct
	}
	if m < HardMinPct {
		m = HardMinPct
	}
	return m
}

func (c *Controller) targetLocked(cs *chanState, fc *FanConfig) (int, bool) {
	switch fc.Mode {
	case modeFixed:
		return fc.FixedPct, true
	case modeCurve:
		src := fc.Source
		if src == "" {
			src = cs.ch.DefaultSrc
		}
		t, ok := c.sourceTemp(src)
		if !ok {
			return 0, false
		}
		h := fc.HystC
		eff := cs.hyst.update(t, h)
		return int(math.Round(fitCurve(fc.Curve, c.minFor(cs, fc)).eval(eff))), true
	}
	return 0, false
}

// applyLocked takes the channel over if needed (recording its original
// first - never the other way round) and writes the speed.
func (c *Controller) applyLocked(cs *chanState, pct int) error {
	if !cs.controlled {
		o, err := cs.ch.io.capture(cs.ch.ID)
		if err != nil {
			return err
		}
		o, err = c.store.putOriginal(o)
		if err != nil {
			return fmt.Errorf("could not save the fan's original state, not taking it over: %w", err)
		}
		cs.orig, cs.controlled = o, true
		cs.applied = -1
		cs.graceUntil = c.now().Add(8 * time.Second)
		cs.hyst = hysteresis{}
	}
	if pct == cs.applied && c.now().Sub(cs.lastWrite) < 20*time.Second {
		return nil
	}
	if pct > cs.applied && cs.applied >= 0 {
		// Speeding up: the new speed needs a moment to show on the tach.
		cs.graceUntil = c.now().Add(4 * time.Second)
	}
	if err := cs.ch.io.set(pct); err != nil {
		return err
	}
	cs.applied, cs.lastWrite = pct, c.now()
	return nil
}

// releaseLocked hands a channel back to the firmware/driver.
func (c *Controller) releaseLocked(cs *chanState, _ string) {
	if !cs.controlled {
		return
	}
	var err error
	if cs.orig.Kind == kindNVIDIA {
		err = restoreNVIDIA(c.gpu, cs.orig)
	} else {
		err = restoreHwmon(c.sysRoot, cs.orig)
	}
	if err != nil && !errors.Is(err, errChannelGone) {
		// Keep the original on file: ExecStopPost / the next start retry.
		log.Printf("fans: handing %s back to Auto failed: %v (will retry)", cs.ch.ID, err)
		return
	}
	_ = c.store.dropOriginal(cs.ch.ID)
	cs.controlled, cs.applied, cs.emergency, cs.zeroTicks = false, -1, false, 0
}

func (c *Controller) autoWithAlertLocked(fc *FanConfig, msg string) {
	if fc.Mode != modeAuto || fc.Alert != msg {
		log.Printf("fans: %s", msg)
	}
	fc.Mode = modeAuto
	fc.Alert = msg
	fc.AlertAt = c.now().Unix()
	c.settings.Preset = "custom"
}

func (c *Controller) saveLocked() {
	if err := c.store.saveSettings(c.settings); err != nil {
		log.Printf("fans: saving settings: %v", err)
	}
}

// Shutdown hands every fan back (SIGTERM, service stop, system shutdown).
func (c *Controller) Shutdown() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range c.order {
		c.releaseLocked(c.chans[id], "")
	}
	if n, err := restoreLeftovers(c.sysRoot, c.store, c.gpu); n > 0 || err != nil {
		log.Printf("fans: restored %d more (err=%v)", n, err)
	}
	if c.gpu != nil {
		c.gpu.Close()
	}
}

// restoreLeftovers hands back every channel in the originals file - what
// `nivaroos-fans restore` (ExecStopPost) and startup run. Returns how
// many were restored.
func restoreLeftovers(sysRoot string, st *store, gpu *lazyGPU) (int, error) {
	m := st.loadOriginals()
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	// Motherboard fans first: they need no library and are the ones that
	// cool the CPU.
	sort.SliceStable(ids, func(i, j int) bool { return m[ids[i]].Kind == kindHwmon && m[ids[j]].Kind != kindHwmon })
	n := 0
	var firstErr error
	for _, id := range ids {
		o := m[id]
		var err error
		if o.Kind == kindNVIDIA {
			if gpu == nil {
				err = errNoNVML
			} else {
				err = restoreNVIDIA(gpu, o)
			}
		} else {
			err = restoreHwmon(sysRoot, o)
		}
		if err != nil && !errors.Is(err, errChannelGone) {
			if firstErr == nil {
				firstErr = fmt.Errorf("%s: %w", id, err)
			}
			continue
		}
		_ = st.dropOriginal(id)
		n++
	}
	return n, firstErr
}
