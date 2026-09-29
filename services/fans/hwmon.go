package main

// Linux hwmon (/sys/class/hwmon) fan channels: every pwmN a driver exposes
// is a candidate - super-I/O chips (nct6775, it87, f71882fg, w83627ehf...),
// amdgpu's pwm1, laptop drivers (thinkpad, dell_smm, asus...) and the
// Raspberry Pi pwm-fan. Nothing here is driver-specific except the
// read-only explanations: taking control is "pwmN_enable = 1 (manual), then
// pwmN = duty", and handing back is "write back exactly what was there".

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// hwmonDev is one /sys/class/hwmon/hwmonN directory.
type hwmonDev struct {
	Dir     string // resolved hwmon directory
	Name    string // contents of "name"
	DevPath string // resolved device directory ("" when there is none)
	DevBase string // basename of DevPath (e.g. nct6775.656, 0000:0b:00.0)
	Driver  string // basename of device/driver (e.g. nct6775, amdgpu)
}

func readTrim(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func readInt(path string) (int64, error) {
	s, err := readTrim(path)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(s, 10, 64)
}

func writeInt(path string, v int64) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		return err
	}
	_, werr := f.WriteString(strconv.FormatInt(v, 10))
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// scanHwmon lists every hwmon device under sysRoot (normally /sys), sorted
// by directory name so ids and ordering are stable.
func scanHwmon(sysRoot string) []hwmonDev {
	base := filepath.Join(sysRoot, "class", "hwmon")
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var out []hwmonDev
	for _, e := range entries {
		dir := filepath.Join(base, e.Name())
		if r, err := filepath.EvalSymlinks(dir); err == nil {
			dir = r
		}
		name, err := readTrim(filepath.Join(dir, "name"))
		if err != nil {
			continue
		}
		d := hwmonDev{Dir: dir, Name: name}
		if p, err := filepath.EvalSymlinks(filepath.Join(dir, "device")); err == nil {
			d.DevPath = p
			d.DevBase = filepath.Base(p)
			if drv, err := filepath.EvalSymlinks(filepath.Join(p, "driver")); err == nil {
				d.Driver = filepath.Base(drv)
			}
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Dir < out[j].Dir })
	return out
}

// pwmIndexes returns the N of every pwmN file in dir.
func pwmIndexes(dir string) []int {
	var idx []int
	for n := 1; n <= 16; n++ {
		if exists(filepath.Join(dir, fmt.Sprintf("pwm%d", n))) {
			idx = append(idx, n)
		}
	}
	return idx
}

// fanInputs returns the N of every fanN_input file in dir.
func fanInputs(dir string) []int {
	var idx []int
	for n := 1; n <= 16; n++ {
		if exists(filepath.Join(dir, fmt.Sprintf("fan%d_input", n))) {
			idx = append(idx, n)
		}
	}
	return idx
}

func isGPUHwmon(name string) bool { return name == "amdgpu" || name == "radeon" }

// laptopHwmon: drivers whose fan belongs to the laptop/desktop firmware
// (they are labelled differently and some need a module option).
var laptopHwmon = map[string]string{
	"thinkpad":        "ThinkPad",
	"dell_smm":        "Dell",
	"asus":            "ASUS",
	"asus_fan":        "ASUS",
	"hp":              "HP",
	"applesmc":        "Apple SMC",
	"surface_fan":     "Surface",
	"legion_hwmon":    "Legion",
	"ideapad":         "IdeaPad",
	"gpd_fan":         "GPD",
	"steamdeck_hwmon": "Steam Deck",
}

func slug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimRight(b.String(), "-")
}

func hwmonChannelID(d hwmonDev, n int) string {
	id := "hw-" + slug(d.Name)
	if d.DevBase != "" {
		id += "-" + slug(d.DevBase)
	}
	return fmt.Sprintf("%s-pwm%d", id, n)
}

// dutyToRaw / rawToDuty convert between percent and the 0-255 sysfs scale.
func dutyToRaw(pct int) int64 {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return int64((pct*255 + 50) / 100)
}

func rawToDuty(raw int64) float64 {
	if raw < 0 {
		raw = 0
	}
	if raw > 255 {
		raw = 255
	}
	return float64(raw) * 100 / 255
}

// hwmonReadOnlyReason explains why a pwm channel can't be driven, or ""
// when it looks writable. Writes can still fail later; that is handled
// (and explained) where they happen.
func hwmonReadOnlyReason(sysRoot string, d hwmonDev, n int) string {
	pwm := filepath.Join(d.Dir, fmt.Sprintf("pwm%d", n))
	st, err := os.Stat(pwm)
	if err != nil {
		return "The driver stopped exposing this fan channel."
	}
	if st.Mode().Perm()&0o200 == 0 {
		return fmt.Sprintf("The %s driver exposes this fan read-only.", d.Name)
	}
	if d.Name == "thinkpad" {
		v, _ := readTrim(filepath.Join(sysRoot, "module", "thinkpad_acpi", "parameters", "fan_control"))
		if v != "Y" && v != "1" {
			return "ThinkPad fan control is off in the thinkpad_acpi driver. It needs the module option fan_control=1 (options thinkpad_acpi fan_control=1 in /etc/modprobe.d) and a reboot."
		}
	}
	return ""
}

// hwmonIO drives one pwm channel.
//
// pwmN_enable is only ever written with 1 (manual) or with exactly the value
// captured before we took over - never an invented "auto" number, because
// the numbers differ per driver (nct6775: 5 = SmartFan IV, it87/amdgpu: 2 =
// automatic, pwm-fan: 0 = fan off). nct6775 also reads back manual mode at
// duty 255 as 0 ("full speed"), so a BIOS set to full speed shows 0 there.
type hwmonIO struct {
	dir  string
	n    int
	tach int // fanN_input index, 0 = none
}

func (h *hwmonIO) path(f string) string { return filepath.Join(h.dir, fmt.Sprintf(f, h.n)) }

func (h *hwmonIO) read() (duty float64, rpm int) {
	duty, rpm = -1, -1
	if v, err := readInt(h.path("pwm%d")); err == nil {
		duty = rawToDuty(v)
	}
	if h.tach > 0 {
		if v, err := readInt(filepath.Join(h.dir, fmt.Sprintf("fan%d_input", h.tach))); err == nil && v >= 0 && v < 30000 {
			rpm = int(v)
		}
	}
	return
}

func (h *hwmonIO) capture(id string) (original, error) {
	o := original{ID: id, Kind: kindHwmon, HwmonDir: h.dir, PWM: h.n}
	o.HwmonName, _ = readTrim(filepath.Join(h.dir, "name"))
	if p, err := filepath.EvalSymlinks(filepath.Join(h.dir, "device")); err == nil {
		o.DevPath = p
	}
	v, err := readInt(h.path("pwm%d"))
	if err != nil {
		return o, fmt.Errorf("read pwm%d: %w", h.n, err)
	}
	o.Value = v
	if exists(h.path("pwm%d_enable")) {
		e, err := readInt(h.path("pwm%d_enable"))
		if err != nil {
			return o, fmt.Errorf("read pwm%d_enable: %w", h.n, err)
		}
		o.Enable = &e
	}
	return o, nil
}

func (h *hwmonIO) set(pct int) error {
	if exists(h.path("pwm%d_enable")) {
		e, err := readInt(h.path("pwm%d_enable"))
		if err != nil {
			return err
		}
		if e != 1 {
			if err := writeInt(h.path("pwm%d_enable"), 1); err != nil {
				return fmt.Errorf("switch pwm%d to manual: %w", h.n, err)
			}
		}
	}
	return writeInt(h.path("pwm%d"), dutyToRaw(pct))
}

var errChannelGone = errors.New("fan channel no longer present")

// restoreHwmon puts a channel back exactly as it was captured. The hwmon
// directory is found again by name + device when the numbering changed
// (a driver reload renumbers hwmonN).
func restoreHwmon(sysRoot string, o original) error {
	dir := o.HwmonDir
	if n, err := readTrim(filepath.Join(dir, "name")); err != nil || n != o.HwmonName {
		dir = ""
		for _, d := range scanHwmon(sysRoot) {
			if d.Name == o.HwmonName && d.DevPath == o.DevPath {
				dir = d.Dir
				break
			}
		}
		if dir == "" {
			return errChannelGone
		}
	}
	pwm := filepath.Join(dir, fmt.Sprintf("pwm%d", o.PWM))
	enable := pwm + "_enable"
	if o.Enable == nil {
		return writeInt(pwm, o.Value)
	}
	if *o.Enable == 1 {
		// It was in manual mode already (firmware or another tool left it
		// there): put the duty back as well.
		if err := writeInt(enable, 1); err != nil {
			return err
		}
		return writeInt(pwm, o.Value)
	}
	return writeInt(enable, *o.Enable)
}
