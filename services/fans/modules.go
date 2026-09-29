package main

// Motherboard fan controller drivers. Most boards' fans hang off a
// super-I/O chip whose driver is not loaded automatically (there's nothing
// on a bus to announce it). The in-kernel drivers probe the chip
// themselves, safely, and refuse to load when their chip isn't there -
// which is exactly the probe we need: try the known drivers in turn, keep
// the first one that brings up pwm channels, and persist it in
// /etc/modules-load.d/nivaroos-fans.conf so it loads at every boot. The
// installer runs this (`nivaroos-fans detect-modules`) and the service
// runs it once per boot when no motherboard fan channel is present.
//
// When the firmware (ACPI) claims the chip's I/O ports the driver refuses
// with "resource busy"/"conflicts with OpRegion". That is reported with the
// acpi_enforce_resources=lax option as optional guidance - NivaroOS never
// changes the kernel command line itself.

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Probed in this order; the first that brings up pwm channels wins.
// nct6775 before w83627ehf (both know some Nuvoton/Winbond ids; nct6775
// is the newer driver), it87 for ITE, f71882fg for Fintek, nct6683 for
// Nuvoton EC boards (often read-only), dell_smm_hwmon for Dell desktops
// (it refuses to load on anything else).
var candidateModules = []string{"nct6775", "it87", "f71882fg", "w83627ehf", "w83627hf", "nct6683", "sch5627", "dme1737", "dell_smm_hwmon"}

const modulesConfPath = "/etc/modules-load.d/nivaroos-fans.conf"

type moduleOps interface {
	Loaded(name string) bool
	Load(name string) error
	Unload(name string) error
	KernelLog() []string
	Cmdline() string
}

type detectResult struct {
	At             int64    `json:"at"`
	Present        []string `json:"present"` // chips with pwm channels
	Modules        []string `json:"modules"` // persisted for boot
	Tried          []string `json:"tried"`
	Conflict       bool     `json:"acpi_conflict"`
	ConflictModule string   `json:"conflict_module,omitempty"`
	ConflictLine   string   `json:"conflict_line,omitempty"`
	Lax            bool     `json:"acpi_lax"`
	NotRoot        bool     `json:"not_root,omitempty"`
	Err            string   `json:"error,omitempty"`
}

const acpiGuidance = "Advanced, at your own risk: adding acpi_enforce_resources=lax to the kernel command line (GRUB_CMDLINE_LINUX_DEFAULT in /etc/default/grub, then update-grub and a reboot) lets the driver use the chip anyway. The firmware may be using the chip too, so readings or control can misbehave. NivaroOS never changes the kernel command line for you."

func (r detectResult) notes() []Note {
	var out []Note
	if r.Conflict {
		msg := "The motherboard's firmware (ACPI) reserves the fan controller"
		if r.ConflictModule != "" {
			msg += " (" + r.ConflictModule + " driver)"
		}
		msg += ", so Linux won't drive it and motherboard fans are read-only (the BIOS keeps controlling them)."
		n := Note{Level: "warn", Code: "acpi_conflict", Message: msg, Guidance: acpiGuidance}
		if r.Lax {
			n.Guidance = "acpi_enforce_resources=lax is already set, but the driver still refused the chip."
		}
		out = append(out, n)
	} else if r.At != 0 && len(r.Present) == 0 && !r.NotRoot {
		out = append(out, Note{Level: "info", Code: "no_board_driver", Message: "No supported motherboard fan controller driver found a chip on this machine, so motherboard fans stay under BIOS control. GPU and laptop fans are listed if their drivers expose them."})
	}
	if r.Err != "" {
		out = append(out, Note{Level: "warn", Code: "detect_error", Message: r.Err})
	}
	return out
}

type detector struct {
	sysRoot    string
	ops        moduleOps
	confPath   string
	statePath  string // last result (/var/lib/nivaroos/fans/detect.json)
	markerPath string // once per boot (/run/nivaroos/fans-detect.done)
	isRoot     bool
}

var conflictRe = regexp.MustCompile(`(?i)(conflicts with OpRegion|resource conflict|ACPI.*conflict)`)

func (d detector) boardChips() (names []string, devs []hwmonDev) {
	for _, h := range scanHwmon(d.sysRoot) {
		if isGPUHwmon(h.Name) || len(pwmIndexes(h.Dir)) == 0 {
			continue
		}
		names = append(names, h.Name)
		devs = append(devs, h)
	}
	return
}

// moduleOf: the kernel module behind a hwmon device's driver.
func moduleOf(h hwmonDev) string {
	if h.DevPath == "" {
		return ""
	}
	m, err := filepath.EvalSymlinks(filepath.Join(h.DevPath, "driver", "module"))
	if err != nil {
		return ""
	}
	return filepath.Base(m)
}

func (d detector) load() detectResult {
	var r detectResult
	if b, err := os.ReadFile(d.statePath); err == nil {
		_ = json.Unmarshal(b, &r)
	}
	return r
}

func (d detector) run(force bool) detectResult {
	if !force && exists(d.markerPath) {
		return d.load()
	}
	r := detectResult{At: time.Now().Unix()}
	defer func() {
		if b, err := json.MarshalIndent(r, "", "  "); err == nil {
			_ = writeFileAtomic(d.statePath, b, 0o644)
		}
		_ = writeFileAtomic(d.markerPath, []byte("1\n"), 0o644)
	}()
	if !d.isRoot {
		r.NotRoot = true
		r.Present, _ = d.boardChips()
		return r
	}
	r.Lax = strings.Contains(d.ops.Cmdline(), "acpi_enforce_resources=lax")

	names, devs := d.boardChips()
	if len(names) == 0 {
		for _, m := range candidateModules {
			if d.ops.Loaded(m) {
				continue
			}
			r.Tried = append(r.Tried, m)
			before := len(d.ops.KernelLog())
			err := d.ops.Load(m)
			logs := d.ops.KernelLog()
			var fresh []string
			if len(logs) > before {
				fresh = logs[before:]
			}
			if err == nil {
				if n, dv := d.boardChips(); len(n) > 0 {
					names, devs = n, dv
					break
				}
				_ = d.ops.Unload(m) // loaded, but nothing to drive
			}
			busy := err != nil && strings.Contains(strings.ToLower(err.Error()), "resource busy")
			for _, l := range fresh {
				if conflictRe.MatchString(l) {
					busy = true
					if r.ConflictLine == "" {
						r.ConflictLine = strings.TrimSpace(l)
					}
				}
			}
			if busy && !r.Conflict {
				r.Conflict, r.ConflictModule = true, m
			}
		}
	}
	r.Present = names
	for _, h := range devs {
		if m := moduleOf(h); m != "" && containsStr(candidateModules, m) && !containsStr(r.Modules, m) {
			r.Modules = append(r.Modules, m)
		}
	}
	if len(r.Present) > 0 {
		r.Conflict, r.ConflictModule, r.ConflictLine = false, "", ""
	}
	if len(r.Modules) > 0 {
		if err := writeModulesConf(d.confPath, r.Modules); err != nil {
			r.Err = "Couldn't save the fan driver for the next boot: " + err.Error()
		}
	}
	return r
}

// writeModulesConf writes the conf when its module list changed. An
// existing file is never removed: a chip that didn't answer this once is
// still better loaded at boot.
func writeModulesConf(path string, mods []string) error {
	body := "# NivaroOS fan control: motherboard fan controller driver(s), loaded at boot.\n# Written by `nivaroos-fans detect-modules` (installer/install.sh); safe to delete.\n" + strings.Join(mods, "\n") + "\n"
	if old, err := os.ReadFile(path); err == nil {
		have := map[string]bool{}
		for _, l := range strings.Split(string(old), "\n") {
			l = strings.TrimSpace(l)
			if l != "" && !strings.HasPrefix(l, "#") {
				have[l] = true
			}
		}
		all := true
		for _, m := range mods {
			all = all && have[m]
		}
		if all {
			return nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeFileAtomic(path, []byte(body), 0o644)
}

// realModules runs modprobe/dmesg.
type realModules struct{ sysRoot string }

func (m realModules) Loaded(name string) bool {
	return exists(filepath.Join(m.sysRoot, "module", name))
}

func runCmd(timeout time.Duration, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return string(out), &cmdError{msg}
	}
	return string(out), nil
}

type cmdError struct{ msg string }

func (e *cmdError) Error() string { return e.msg }

func (m realModules) Load(name string) error {
	_, err := runCmd(30*time.Second, "modprobe", name)
	return err
}

func (m realModules) Unload(name string) error {
	_, err := runCmd(15*time.Second, "modprobe", "-r", name)
	return err
}

func (m realModules) KernelLog() []string {
	out, err := runCmd(10*time.Second, "dmesg")
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimRight(out, "\n"), "\n")
}

func (m realModules) Cmdline() string {
	b, _ := os.ReadFile("/proc/cmdline")
	return string(b)
}
