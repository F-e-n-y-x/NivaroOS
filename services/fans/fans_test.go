package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---- fake sysfs ----

type fakeSys struct {
	t    *testing.T
	root string
}

func newFakeSys(t *testing.T) *fakeSys {
	t.Helper()
	return &fakeSys{t: t, root: t.TempDir()}
}

func (f *fakeSys) write(path string, v interface{}) {
	f.t.Helper()
	p := filepath.Join(f.root, path)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(fmt.Sprint(v)+"\n"), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fakeSys) read(path string) string {
	f.t.Helper()
	b, err := os.ReadFile(filepath.Join(f.root, path))
	if err != nil {
		f.t.Fatal(err)
	}
	return strings.TrimSpace(string(b))
}

// hwmon adds class/hwmon/<hw> -> devices/<dev>/hwmon/<hw> with a driver link.
func (f *fakeSys) hwmon(hw, name, dev, driver string) string {
	f.t.Helper()
	devDir := filepath.Join(f.root, "devices", dev)
	real := filepath.Join(devDir, "hwmon", hw)
	if err := os.MkdirAll(real, 0o755); err != nil {
		f.t.Fatal(err)
	}
	if driver != "" {
		drv := filepath.Join(f.root, "bus", "drivers", driver)
		mod := filepath.Join(f.root, "module", driver)
		os.MkdirAll(drv, 0o755)
		os.MkdirAll(mod, 0o755)
		os.Symlink(mod, filepath.Join(drv, "module"))
		os.Symlink(drv, filepath.Join(devDir, "driver"))
	}
	os.Symlink(devDir, filepath.Join(real, "device"))
	os.MkdirAll(filepath.Join(f.root, "class", "hwmon"), 0o755)
	os.Symlink(real, filepath.Join(f.root, "class", "hwmon", hw))
	rel := filepath.Join("devices", dev, "hwmon", hw)
	f.write(filepath.Join(rel, "name"), name)
	return rel
}

// board builds the atom box: k10temp + nct6793 with 3 pwm channels (pwm2
// has a fan on fan2), all firmware-controlled (enable 5).
func (f *fakeSys) board() (k10, nct string) {
	k10 = f.hwmon("hwmon0", "k10temp", "pci0000:00/0000:00:18.3", "k10temp")
	f.write(k10+"/temp1_input", 45000)
	f.write(k10+"/temp1_label", "Tctl")
	nct = f.hwmon("hwmon1", "nct6793", "platform/nct6775.656", "nct6775")
	for n := 1; n <= 3; n++ {
		f.write(fmt.Sprintf("%s/pwm%d", nct, n), 128)
		f.write(fmt.Sprintf("%s/pwm%d_enable", nct, n), 5)
		f.write(fmt.Sprintf("%s/fan%d_input", nct, n), 0)
	}
	f.write(nct+"/fan2_input", 1200)
	// Junk inputs a super-I/O reports - never used.
	f.write(nct+"/temp1_input", 117000)
	f.write(nct+"/temp1_label", "SYSTIN")
	return
}

func (f *fakeSys) cpu(c float64) {
	f.write("devices/pci0000:00/0000:00:18.3/hwmon/hwmon0/temp1_input", int(c*1000))
}

// ---- fake NVML ----

type fakeGPU struct {
	mu        sync.Mutex
	devs      []gpuDevice
	speed     map[int]int
	manual    map[int]bool
	defaults  int
	setErr    error
	openErr   error
	closeCall int
	calls     int
}

func newFakeGPU() *fakeGPU {
	return &fakeGPU{
		devs:   []gpuDevice{{Index: 0, UUID: "GPU-abc", Name: "Fake 1080", NumFans: 1, MinPct: 25, MaxPct: 100, TempC: 50, TempOK: true, CanControl: true}},
		speed:  map[int]int{0: 30},
		manual: map[int]bool{},
	}
}

func (g *fakeGPU) Devices() ([]gpuDevice, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls++
	return append([]gpuDevice{}, g.devs...), nil
}
func (g *fakeGPU) FanSpeed(i, f int) (int, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.speed[f], nil
}
func (g *fakeGPU) SetFanSpeed(i, f, p int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.setErr != nil {
		return g.setErr
	}
	g.speed[f], g.manual[f] = p, true
	return nil
}
func (g *fakeGPU) SetDefaultFanSpeed(i, f int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.manual[f] = false
	g.speed[f] = 30
	g.defaults++
	return nil
}
func (g *fakeGPU) Close() { g.closeCall++ }
func (g *fakeGPU) temp(c float64) {
	g.mu.Lock()
	g.devs[0].TempC = c
	g.mu.Unlock()
}

// ---- harness ----

type rig struct {
	fs    *fakeSys
	nct   string
	st    *store
	gpu   *fakeGPU
	c     *Controller
	clock time.Time
}

func newRig(t *testing.T, withGPU bool) *rig {
	t.Helper()
	fs := newFakeSys(t)
	_, nct := fs.board()
	r := &rig{fs: fs, nct: nct, clock: time.Unix(1_700_000_000, 0)}
	r.st = &store{settingsPath: filepath.Join(fs.root, "data", "settings.json"), originalsPath: filepath.Join(fs.root, "run", "fans-originals.json")}
	var lg *lazyGPU
	if withGPU {
		r.gpu = newFakeGPU()
		lg = newLazyGPU(func() (gpuAPI, error) { return r.gpu, nil })
	} else {
		lg = newLazyGPU(func() (gpuAPI, error) { return nil, errNoNVML })
	}
	lg.now = func() time.Time { return r.clock }
	r.c = newController(fs.root, r.st, lg, true)
	r.c.now = func() time.Time { return r.clock }
	r.c.sleep = func(d time.Duration) { r.clock = r.clock.Add(d) }
	r.c.Start()
	return r
}

func (r *rig) tick(n int) {
	for i := 0; i < n; i++ {
		r.clock = r.clock.Add(tickInterval)
		r.c.Tick()
	}
}

const pwm2 = "hw-nct6793-nct6775-656-pwm2"

func (r *rig) set(t *testing.T, u FanUpdate) {
	t.Helper()
	if err := r.c.UpdateFan(u); err != nil {
		t.Fatalf("UpdateFan: %v", err)
	}
	r.tick(1) // the API applies at once
}

func sp(s string) *string { return &s }
func ip(i int) *int       { return &i }

func (r *rig) fan(t *testing.T, id string) FanStatus {
	t.Helper()
	for _, f := range r.c.Status().Fans {
		if f.ID == id {
			return f
		}
	}
	t.Fatalf("fan %s not found", id)
	return FanStatus{}
}

// ---- curve math ----

func TestCurveEval(t *testing.T) {
	c := Curve{{40, 30}, {60, 50}, {80, 100}}
	for _, tc := range []struct{ t, want float64 }{{20, 30}, {40, 30}, {50, 40}, {60, 50}, {70, 75}, {80, 100}, {95, 100}} {
		if got := c.eval(tc.t); got != tc.want {
			t.Errorf("eval(%v) = %v, want %v", tc.t, got, tc.want)
		}
	}
}

func TestCurveValidation(t *testing.T) {
	bad := []Curve{
		{{40, 30}},                    // too few
		{{40, 30}, {40, 50}},          // temps not rising
		{{40, 60}, {60, 50}},          // slows down when hotter
		{{40, 10}, {60, 50}},          // below floor
		{{10, 30}, {60, 50}},          // temp out of range
		{{40, 30}, {60, 101}},         // over 100
		make(Curve, MaxCurvePoints+1), // too many
	}
	for i, c := range bad {
		if validateCurve(c, 20) == nil {
			t.Errorf("curve %d accepted: %v", i, c)
		}
	}
	if err := validateCurve(Curve{{30, 20}, {70, 100}}, 20); err != nil {
		t.Errorf("good curve refused: %v", err)
	}
}

func TestHysteresis(t *testing.T) {
	var h hysteresis
	steps := []struct{ in, want float64 }{{60, 60}, {62, 62}, {61, 62}, {60, 62}, {59, 59}, {60, 60}}
	for _, s := range steps {
		if got := h.update(s.in, 3); got != s.want {
			t.Fatalf("update(%v) = %v, want %v", s.in, got, s.want)
		}
	}
}

func TestRampAndClamp(t *testing.T) {
	if ramp(-1, 70, 15, 3) != 70 || ramp(30, 70, 15, 3) != 45 || ramp(70, 30, 15, 3) != 67 || ramp(50, 52, 15, 3) != 52 {
		t.Fatal("ramp")
	}
	if clampPct(5, 0) != HardMinPct || clampPct(150, 20) != 100 || clampPct(30, 40) != 40 {
		t.Fatal("clamp")
	}
}

// ---- detection ----

func TestDetectionIDsLabelsAndSources(t *testing.T) {
	r := newRig(t, true)
	s := r.c.Status()
	if len(s.Fans) != 4 {
		t.Fatalf("want 3 board + 1 GPU fan, got %d", len(s.Fans))
	}
	f := s.Fans[1]
	if f.ID != pwm2 || f.Label != "Fan header 2" || f.Source != "cpu" || f.Tach != "fan2" || !f.Controllable {
		t.Fatalf("pwm2 = %+v", f)
	}
	g := s.Fans[3]
	if g.Kind != kindNVIDIA || g.Source != "gpu:nv-gpu-abc" || g.FloorPct != 25 || g.MinPct != 25 {
		t.Fatalf("gpu fan = %+v", g)
	}
	// The junk SYSTIN 117 °C must not be a source or a temperature.
	for _, tm := range s.Temps {
		if tm.C != nil && *tm.C > 100 {
			t.Fatalf("junk temperature used: %+v", tm)
		}
	}
	if s.Temps[0].C == nil || *s.Temps[0].C != 45 {
		t.Fatalf("cpu temp = %+v", s.Temps[0])
	}
}

func TestReadOnlyWithoutCPUTemp(t *testing.T) {
	r := newRig(t, false)
	os.Remove(filepath.Join(r.fs.root, "devices/pci0000:00/0000:00:18.3/hwmon/hwmon0/temp1_input"))
	r.tick(1)
	f := r.fan(t, pwm2)
	if f.Controllable || !strings.Contains(f.ReadOnly, "CPU temperature") {
		t.Fatalf("want read-only without a CPU sensor, got %+v", f)
	}
	if err := r.c.UpdateFan(FanUpdate{ID: pwm2, Mode: sp(modeFixed)}); err == nil {
		t.Fatal("fixed mode accepted without a CPU temperature")
	}
}

func TestReadOnlyNotRootAndThinkpad(t *testing.T) {
	r := newRig(t, false)
	r.c.isRoot = false
	if f := r.fan(t, pwm2); f.Controllable {
		t.Fatal("controllable while not root")
	}
	fs := newFakeSys(t)
	d := hwmonDev{Dir: filepath.Join(fs.root, "tp"), Name: "thinkpad"}
	fs.write("tp/pwm1", 100)
	if !strings.Contains(hwmonReadOnlyReason(fs.root, d, 1), "fan_control=1") {
		t.Fatal("thinkpad without fan_control must be read-only")
	}
	fs.write("module/thinkpad_acpi/parameters/fan_control", "Y")
	if hwmonReadOnlyReason(fs.root, d, 1) != "" {
		t.Fatal("thinkpad with fan_control=Y should be writable")
	}
	os.Chmod(filepath.Join(fs.root, "tp/pwm1"), 0o444)
	d.Name = "it8686"
	if hwmonReadOnlyReason(fs.root, d, 1) == "" {
		t.Fatal("0444 pwm must be read-only")
	}
}

// ---- modes ----

func TestFixedThenAutoRestoresOriginal(t *testing.T) {
	r := newRig(t, false)
	r.set(t, FanUpdate{ID: pwm2, Mode: sp(modeFixed), FixedPct: ip(60)})
	if got := r.fs.read(r.nct + "/pwm2_enable"); got != "1" {
		t.Fatalf("enable = %s, want 1 (manual)", got)
	}
	if got := r.fs.read(r.nct + "/pwm2"); got != "153" {
		t.Fatalf("pwm2 = %s, want 153 (60%%)", got)
	}
	if o := r.st.loadOriginals(); o[pwm2].Enable == nil || *o[pwm2].Enable != 5 {
		t.Fatalf("original not recorded: %+v", o)
	}
	if f := r.fan(t, pwm2); f.State != "manual" {
		t.Fatalf("state %s", f.State)
	}
	r.set(t, FanUpdate{ID: pwm2, Mode: sp(modeAuto)})
	r.tick(1)
	if got := r.fs.read(r.nct + "/pwm2_enable"); got != "5" {
		t.Fatalf("enable after auto = %s, want the original 5", got)
	}
	if len(r.st.loadOriginals()) != 0 {
		t.Fatal("originals kept after restore")
	}
}

func TestFloorEnforced(t *testing.T) {
	r := newRig(t, false)
	if err := r.c.UpdateFan(FanUpdate{ID: pwm2, Mode: sp(modeFixed), FixedPct: ip(10)}); err == nil {
		t.Fatal("10% accepted")
	}
	if err := r.c.UpdateFan(FanUpdate{ID: pwm2, MinPct: ip(5)}); err == nil {
		t.Fatal("min 5% accepted")
	}
	// A tampered settings file is clamped, not obeyed.
	st := defaultSettings()
	st.Fans[pwm2] = &FanConfig{Mode: modeFixed, FixedPct: 1, MinPct: 1}
	r.st.saveSettings(st)
	loaded := r.st.loadSettings()
	if fc := loaded.Fans[pwm2]; fc.MinPct < HardMinPct || fc.FixedPct < HardMinPct {
		t.Fatalf("tampered config not clamped: %+v", fc)
	}
}

func TestCurveWithHysteresisAndSmoothing(t *testing.T) {
	r := newRig(t, false)
	curve := Curve{{40, 20}, {60, 60}, {80, 100}}
	r.fs.cpu(50) // -> 40%
	r.set(t, FanUpdate{ID: pwm2, Curve: curve, Mode: sp(modeCurve), HystC: func() *float64 { v := 3.0; return &v }()})
	if got := r.fs.read(r.nct + "/pwm2"); got != dutyToRawStr(40) {
		t.Fatalf("first write %s, want 40%%", got)
	}
	r.fs.cpu(70) // -> 80%, ramps up 15 per tick
	r.tick(1)
	if got := r.fs.read(r.nct + "/pwm2"); got != dutyToRawStr(55) {
		t.Fatalf("ramp up: %s, want 55%%", got)
	}
	r.tick(2)
	if got := r.fs.read(r.nct + "/pwm2"); got != dutyToRawStr(80) {
		t.Fatalf("after ramp: %s, want 80%%", got)
	}
	r.fs.cpu(68) // within hysteresis: stays at 80
	r.tick(1)
	if got := r.fs.read(r.nct + "/pwm2"); got != dutyToRawStr(80) {
		t.Fatalf("hysteresis: %s, want 80%%", got)
	}
	r.fs.cpu(60) // below: 60% target, ramps down 3 per tick
	r.tick(1)
	if got := r.fs.read(r.nct + "/pwm2"); got != dutyToRawStr(77) {
		t.Fatalf("ramp down: %s, want 77%%", got)
	}
}

func dutyToRawStr(p int) string { return fmt.Sprint(dutyToRaw(p)) }

// ---- safety ----

func TestEmergencyFullSpeedAndClear(t *testing.T) {
	r := newRig(t, false)
	r.set(t, FanUpdate{ID: pwm2, Mode: sp(modeFixed), FixedPct: ip(30)})
	r.fs.cpu(86)
	r.tick(1)
	if got := r.fs.read(r.nct + "/pwm2"); got != "255" {
		t.Fatalf("emergency: pwm2 = %s, want 255", got)
	}
	s := r.c.Status()
	if !s.Emergency || r.fan(t, pwm2).State != "emergency" {
		t.Fatal("emergency not reported")
	}
	// Auto fans are left to the firmware even in an emergency.
	if got := r.fs.read(r.nct + "/pwm1_enable"); got != "5" {
		t.Fatalf("auto fan touched: enable %s", got)
	}
	r.fs.cpu(82) // not 5 °C below yet
	r.tick(1)
	if got := r.fs.read(r.nct + "/pwm2"); got != "255" {
		t.Fatal("emergency cleared too early")
	}
	r.fs.cpu(79)
	r.tick(1)
	if got := r.fs.read(r.nct + "/pwm2"); got != dutyToRawStr(30) {
		t.Fatalf("after emergency: %s, want 30%%", got)
	}
	// Critical temperature bounds.
	if r.c.SetCritical(ip(99), nil) == nil || r.c.SetCritical(nil, ip(95)) == nil || r.c.SetCritical(ip(50), nil) == nil {
		t.Fatal("unsafe critical temperature accepted")
	}
	if err := r.c.SetCritical(ip(80), ip(80)); err != nil {
		t.Fatal(err)
	}
}

func TestStallGoesBackToAuto(t *testing.T) {
	r := newRig(t, false)
	r.tick(1) // sees 1200 RPM on fan2
	r.set(t, FanUpdate{ID: pwm2, Mode: sp(modeFixed), FixedPct: ip(25)})
	r.fs.write(r.nct+"/fan2_input", 0) // stalls
	r.tick(4)                          // within the 8 s grace
	if r.fan(t, pwm2).Mode != modeFixed {
		t.Fatal("stall detected inside the grace period")
	}
	r.tick(3)
	f := r.fan(t, pwm2)
	if f.Mode != modeAuto || !strings.Contains(f.Alert, "stopped") {
		t.Fatalf("stall not handled: %+v", f)
	}
	if got := r.fs.read(r.nct + "/pwm2_enable"); got != "5" {
		t.Fatalf("stalled fan not restored: enable %s", got)
	}
	// A channel that never showed a speed has no stall detection.
	r.set(t, FanUpdate{ID: "hw-nct6793-nct6775-656-pwm1", Mode: sp(modeFixed), FixedPct: ip(40)})
	r.tick(10)
	if r.fan(t, "hw-nct6793-nct6775-656-pwm1").Mode != modeFixed {
		t.Fatal("tach-less channel treated as stalled")
	}
}

func TestSensorLostGoesBackToAuto(t *testing.T) {
	r := newRig(t, false)
	r.set(t, FanUpdate{ID: pwm2, Mode: sp(modeCurve)})
	os.Remove(filepath.Join(r.fs.root, "devices/pci0000:00/0000:00:18.3/hwmon/hwmon0/temp1_input"))
	r.tick(1)
	if f := r.fan(t, pwm2); f.Mode != modeAuto || f.Alert == "" {
		t.Fatalf("sensor loss: %+v", f)
	}
	if got := r.fs.read(r.nct + "/pwm2_enable"); got != "5" {
		t.Fatal("not restored")
	}
}

func TestWriteErrorsGoBackToAuto(t *testing.T) {
	r := newRig(t, false)
	r.set(t, FanUpdate{ID: pwm2, Mode: sp(modeFixed), FixedPct: ip(50)})
	// The pwm file turns into a directory: every write fails.
	p := filepath.Join(r.fs.root, r.nct, "pwm2")
	os.Remove(p)
	os.Mkdir(p, 0o755)
	r.tick(13) // the speed is re-asserted every 20 s; 3 failures in a row
	if got := r.fs.read(r.nct + "/pwm2_enable"); got != "5" {
		t.Fatalf("not handed back: enable %s", got)
	}
	if f := r.fan(t, pwm2); f.Mode != modeAuto || f.Controllable {
		t.Fatalf("write errors: %+v", f)
	}
}

// ---- failsafe ----

func TestShutdownRestoresEverything(t *testing.T) {
	r := newRig(t, true)
	r.set(t, FanUpdate{ID: pwm2, Mode: sp(modeFixed), FixedPct: ip(70)})
	gid := "nv-gpu-abc-fan0"
	r.set(t, FanUpdate{ID: gid, Mode: sp(modeFixed), FixedPct: ip(60)})
	if !r.gpu.manual[0] || r.gpu.speed[0] != 60 {
		t.Fatalf("gpu not driven: %+v", r.gpu.speed)
	}
	r.c.Shutdown()
	if got := r.fs.read(r.nct + "/pwm2_enable"); got != "5" {
		t.Fatalf("pwm2 enable %s after shutdown", got)
	}
	if r.gpu.manual[0] || r.gpu.defaults != 1 {
		t.Fatal("gpu not handed back to the driver")
	}
	if len(r.st.loadOriginals()) != 0 {
		t.Fatal("originals left")
	}
}

func TestCrashRecoveryFromOriginals(t *testing.T) {
	r := newRig(t, true)
	// Firmware had pwm3 in manual at 128 (enable 1): restore must put the
	// duty back too.
	r.fs.write(r.nct+"/pwm3_enable", 1)
	r.set(t, FanUpdate{ID: "hw-nct6793-nct6775-656-pwm3", Mode: sp(modeFixed), FixedPct: ip(90)})
	r.set(t, FanUpdate{ID: pwm2, Mode: sp(modeFixed), FixedPct: ip(40)})
	r.set(t, FanUpdate{ID: "nv-gpu-abc-fan0", Mode: sp(modeFixed), FixedPct: ip(50)})
	// "Crash": no Shutdown. ExecStopPost runs `nivaroos-fans restore`.
	n, err := restoreLeftovers(r.fs.root, r.st, newLazyGPU(func() (gpuAPI, error) { return r.gpu, nil }))
	if err != nil || n != 3 {
		t.Fatalf("restore: n=%d err=%v", n, err)
	}
	if r.fs.read(r.nct+"/pwm2_enable") != "5" || r.fs.read(r.nct+"/pwm3_enable") != "1" || r.fs.read(r.nct+"/pwm3") != "128" {
		t.Fatal("hwmon not restored to originals")
	}
	if r.gpu.manual[0] {
		t.Fatal("gpu still manual")
	}
	// The originals recorded are the firmware's, not our own writes: a
	// second capture must not overwrite the first.
	r2 := newRig(t, false)
	r2.set(t, FanUpdate{ID: pwm2, Mode: sp(modeFixed), FixedPct: ip(40)})
	r2.c.mu.Lock()
	cs := r2.c.chans[pwm2]
	cs.controlled = false // as if state were lost
	r2.c.applyLocked(cs, 50)
	r2.c.mu.Unlock()
	if o := r2.st.loadOriginals()[pwm2]; *o.Enable != 5 {
		t.Fatalf("original overwritten with our own state: %+v", o)
	}
}

func TestStartupRestoresLeftovers(t *testing.T) {
	r := newRig(t, false)
	r.set(t, FanUpdate{ID: pwm2, Mode: sp(modeFixed), FixedPct: ip(40)})
	// New process with the same store: Start hands back first.
	c2 := newController(r.fs.root, r.st, newLazyGPU(func() (gpuAPI, error) { return nil, errNoNVML }), true)
	c2.Start()
	if got := r.fs.read(r.nct + "/pwm2_enable"); got != "5" {
		t.Fatalf("startup restore: enable %s", got)
	}
	// ...and then applies the saved settings again on its first tick.
	c2.Tick()
	if got := r.fs.read(r.nct + "/pwm2_enable"); got != "1" {
		t.Fatal("saved settings not applied at start")
	}
}

func TestHwmonRenumberedRestore(t *testing.T) {
	r := newRig(t, false)
	r.set(t, FanUpdate{ID: pwm2, Mode: sp(modeFixed), FixedPct: ip(40)})
	o := r.st.loadOriginals()[pwm2]
	o.HwmonDir = filepath.Join(r.fs.root, "nowhere")
	if err := restoreHwmon(r.fs.root, o); err != nil {
		t.Fatalf("restore by device path: %v", err)
	}
	if r.fs.read(r.nct+"/pwm2_enable") != "5" {
		t.Fatal("not restored")
	}
}

// ---- NVIDIA ----

func TestNVIDIACurveEmergencyAndErrors(t *testing.T) {
	r := newRig(t, true)
	gid := "nv-gpu-abc-fan0"
	if err := r.c.UpdateFan(FanUpdate{ID: gid, Mode: sp(modeFixed), FixedPct: ip(22)}); err == nil {
		t.Fatal("below the GPU's own minimum accepted")
	}
	r.set(t, FanUpdate{ID: gid, Mode: sp(modeCurve), Curve: Curve{{40, 30}, {70, 70}, {80, 100}}})
	if r.gpu.speed[0] != 43 { // 50 °C on 40->70
		t.Fatalf("gpu curve: %d", r.gpu.speed[0])
	}
	r.gpu.temp(84)
	r.tick(1)
	if r.gpu.speed[0] != 100 || !r.c.Status().Emergency {
		t.Fatalf("gpu emergency: %d", r.gpu.speed[0])
	}
	r.gpu.temp(60)
	r.gpu.setErr = errors.New("no permission")
	r.tick(3)
	if f := r.fan(t, gid); f.Mode != modeAuto || f.Controllable {
		t.Fatalf("gpu write errors: %+v", f)
	}
	if r.gpu.manual[0] {
		t.Fatal("gpu not handed back after errors")
	}
}

func TestNVIDIAIdleShutdown(t *testing.T) {
	r := newRig(t, true)
	r.tick(1)
	r.clock = r.clock.Add(2 * nvmlIdle)
	r.c.gpu.idle(false)
	if r.gpu.closeCall != 1 {
		t.Fatal("NVML kept open while idle")
	}
	// All fans on Auto and nobody looking: NVML isn't touched at all.
	before := r.gpu.calls
	r.tick(20)
	if r.gpu.calls != before {
		t.Fatalf("NVML used while idle: %d calls", r.gpu.calls-before)
	}
	// Looking at the status wakes it up.
	if s := r.c.Status(); s.Temps[1].C == nil || r.gpu.calls == before {
		t.Fatal("status didn't read the GPU")
	}
}

// ---- presets, identify ----

func TestPresets(t *testing.T) {
	r := newRig(t, true)
	if err := r.c.ApplyPreset("quiet"); err != nil {
		t.Fatal(err)
	}
	s := r.c.Status()
	if s.Preset != "quiet" {
		t.Fatal(s.Preset)
	}
	for _, f := range s.Fans {
		if f.Mode != modeCurve || f.Curve[0].P < f.MinPct {
			t.Fatalf("preset not applied to %s: %+v", f.ID, f)
		}
	}
	if s.Fans[3].Curve[len(s.Fans[3].Curve)-1].T != 82 {
		t.Fatal("GPU fan should get the GPU curve")
	}
	r.c.ApplyPreset(modeAuto)
	r.tick(1)
	if r.fs.read(r.nct+"/pwm2_enable") != "5" || r.gpu.manual[0] {
		t.Fatal("auto preset didn't hand back")
	}
	if r.c.ApplyPreset("turbo") == nil {
		t.Fatal("unknown preset accepted")
	}
}

func TestIdentify(t *testing.T) {
	r := newRig(t, false)
	id := "hw-nct6793-nct6775-656-pwm3"
	// The fan on pwm3 is actually wired to fan1's tach: it spins up once
	// pwm3 goes to 100%.
	r.c.sleep = func(d time.Duration) {
		r.clock = r.clock.Add(d)
		if r.fs.read(r.nct+"/pwm3") == "255" {
			r.fs.write(r.nct+"/fan1_input", 1500)
		}
	}
	res, err := r.c.Identify(id)
	if err != nil {
		t.Fatal(err)
	}
	if res.Tach != "fan1" || res.Pct != 100 {
		t.Fatalf("identify = %+v", res)
	}
	if r.fs.read(r.nct+"/pwm3_enable") != "5" {
		t.Fatal("identify left the fan driven")
	}
	if f := r.fan(t, id); f.Tach != "fan1" || !f.HasTach {
		t.Fatalf("mapping not kept: %+v", f)
	}
	// Already at full speed: identify slows it to 50% instead.
	r.fs.write(r.nct+"/pwm1", 255)
	r.c.sleep = func(d time.Duration) { r.clock = r.clock.Add(d) }
	res, _ = r.c.Identify("hw-nct6793-nct6775-656-pwm1")
	if res.Pct != 50 || res.Tach != "" {
		t.Fatalf("identify at full speed = %+v", res)
	}
}

// ---- driver detection ----

type fakeModules struct {
	fs      *fakeSys
	loaded  map[string]bool
	logs    []string
	busy    map[string]bool
	hasChip string // module whose chip exists
	unloads []string
}

func (m *fakeModules) Loaded(n string) bool { return m.loaded[n] }
func (m *fakeModules) Load(n string) error {
	if m.busy[n] {
		m.logs = append(m.logs, "ACPI Warning: SystemIO range 0x295-0x296 conflicts with OpRegion 0x290-0x299 (\\_GPE.HWM)")
		return errors.New("modprobe: ERROR: could not insert '" + n + "': Device or resource busy")
	}
	if n != m.hasChip {
		return errors.New("modprobe: ERROR: could not insert '" + n + "': No such device")
	}
	m.loaded[n] = true
	nct := m.fs.hwmon("hwmon5", "nct6798", "platform/nct6775.2592", "nct6775")
	m.fs.write(nct+"/pwm1", 100)
	return nil
}
func (m *fakeModules) Unload(n string) error { m.unloads = append(m.unloads, n); return nil }
func (m *fakeModules) KernelLog() []string   { return append([]string{}, m.logs...) }
func (m *fakeModules) Cmdline() string       { return "BOOT_IMAGE=/vmlinuz quiet" }

func newDetector(fs *fakeSys, ops moduleOps) detector {
	return detector{sysRoot: fs.root, ops: ops, confPath: filepath.Join(fs.root, "etc/modules-load.d/nivaroos-fans.conf"),
		statePath: filepath.Join(fs.root, "data/detect.json"), markerPath: filepath.Join(fs.root, "run/fans-detect.done"), isRoot: true}
}

func TestDetectLoadsAndPersistsDriver(t *testing.T) {
	fs := newFakeSys(t)
	m := &fakeModules{fs: fs, loaded: map[string]bool{}, hasChip: "nct6775"}
	d := newDetector(fs, m)
	r := d.run(false)
	if len(r.Present) != 1 || len(r.Modules) != 1 || r.Modules[0] != "nct6775" {
		t.Fatalf("detect = %+v", r)
	}
	conf := fs.read("etc/modules-load.d/nivaroos-fans.conf")
	if !strings.Contains(conf, "\nnct6775") {
		t.Fatalf("conf = %q", conf)
	}
	// Once per boot unless forced.
	m.hasChip = ""
	if r2 := d.run(false); len(r2.Modules) != 1 {
		t.Fatal("marker ignored")
	}
}

func TestDetectACPIConflictIsReadOnlyWithGuidance(t *testing.T) {
	fs := newFakeSys(t)
	m := &fakeModules{fs: fs, loaded: map[string]bool{}, busy: map[string]bool{"it87": true}}
	r := newDetector(fs, m).run(true)
	if !r.Conflict || r.ConflictModule != "it87" || len(r.Present) != 0 {
		t.Fatalf("detect = %+v", r)
	}
	notes := r.notes()
	if len(notes) == 0 || notes[0].Code != "acpi_conflict" || !strings.Contains(notes[0].Guidance, "acpi_enforce_resources=lax") || !strings.Contains(notes[0].Guidance, "never changes") {
		t.Fatalf("notes = %+v", notes)
	}
	if exists(filepath.Join(fs.root, "etc/modules-load.d/nivaroos-fans.conf")) {
		t.Fatal("conf written without a chip")
	}
}

func TestDetectAlreadyPresent(t *testing.T) {
	fs := newFakeSys(t)
	fs.board()
	m := &fakeModules{fs: fs, loaded: map[string]bool{"nct6775": true}}
	r := newDetector(fs, m).run(true)
	if len(r.Tried) != 0 || len(r.Modules) != 1 || r.Modules[0] != "nct6775" {
		t.Fatalf("detect = %+v", r)
	}
}

// ---- API ----

func TestAPIAuthAndValidation(t *testing.T) {
	r := newRig(t, false)
	a := newAPI(r.c, newDetector(r.fs, &fakeModules{fs: r.fs, loaded: map[string]bool{}}), r.fs.root)
	a.validate = func(tok string) (bool, bool) {
		switch tok {
		case "admin":
			return true, true
		case "user":
			return true, false
		}
		return false, false
	}
	h := a.handler()
	do := func(method, path, tok, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		req.RemoteAddr = "192.168.1.20:5000" // not local automation
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	if w := do("GET", "/v1/fans/health", "", ""); w.Code != 200 {
		t.Fatal("health needs no token")
	}
	if w := do("GET", "/v1/fans/status", "", ""); w.Code != 401 {
		t.Fatalf("status without token: %d", w.Code)
	}
	if w := do("GET", "/v1/fans/status", "user", ""); w.Code != 200 {
		t.Fatalf("status as user: %d", w.Code)
	}
	body := `{"id":"` + pwm2 + `","mode":"fixed","fixed_pct":60}`
	if w := do("POST", "/v1/fans/fan", "user", body); w.Code != 403 {
		t.Fatalf("write as non-admin: %d", w.Code)
	}
	w := do("POST", "/v1/fans/fan", "admin", body)
	if w.Code != 200 {
		t.Fatalf("write as admin: %d %s", w.Code, w.Body)
	}
	var s Status
	json.Unmarshal(w.Body.Bytes(), &s)
	if s.Fans[1].State != "manual" || r.fs.read(r.nct+"/pwm2") != "153" {
		t.Fatalf("not applied at once: %+v", s.Fans[1])
	}
	if w := do("POST", "/v1/fans/fan", "admin", `{"id":"`+pwm2+`","fixed_pct":5}`); w.Code != 400 {
		t.Fatalf("5%% accepted: %d", w.Code)
	}
	if w := do("POST", "/v1/fans/fan", "admin", `{"id":"nope","mode":"auto"}`); w.Code != 404 {
		t.Fatalf("unknown fan: %d", w.Code)
	}
	if w := do("POST", "/v1/fans/fan", "admin", `{"id":"`+pwm2+`","bogus":1}`); w.Code != 400 {
		t.Fatalf("unknown field: %d", w.Code)
	}
	if w := do("POST", "/v1/fans/settings", "admin", `{"critical_cpu_c":120}`); w.Code != 400 {
		t.Fatalf("unsafe critical: %d", w.Code)
	}
	if w := do("POST", "/v1/fans/fan", "admin", `{"id":"`+pwm2+`","label":"CPU fan"}`); w.Code != 200 || r.fan(t, pwm2).Label != "CPU fan" {
		t.Fatalf("rename: %d", w.Code)
	}
	if w := do("POST", "/v1/fans/auto", "admin", ``); w.Code != 200 || r.fs.read(r.nct+"/pwm2_enable") != "5" {
		t.Fatalf("all auto: %d", w.Code)
	}
	if !tokenIsAdmin("x."+b64(`{"id":1}`)+".y") || tokenIsAdmin("x."+b64(`{"role":"viewer"}`)+".y") {
		t.Fatal("tokenIsAdmin")
	}
}

func b64(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
