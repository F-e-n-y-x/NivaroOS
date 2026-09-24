package service

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const hdparmConfPath = "/etc/hdparm.conf"

// Standby persistence: /etc/hdparm.conf is only read by Debian/Ubuntu's
// hdparm package (its 85-hdparm.rules). To work on every distro, the timer
// is persisted as a udev rule keyed by the disk's ID_SERIAL (stable across
// /dev/sdX renames and ports), which runs `hdparm -S <code>` whenever the
// disk appears - at boot (coldplug) and on hotplug. hdparm.conf is still
// read as a fallback for timers set by older versions, and this device's
// block there is removed on the next change so Debian's rule (which runs
// after ours, 85 > 69) can't re-apply a stale value.
const standbyRulesPath = "/etc/udev/rules.d/69-nivaroos-hdparm.rules"

var (
	hdparmBlockHeaderRe = regexp.MustCompile(`^\s*(\S+)\s*\{\s*$`)
	hdparmSpindownRe    = regexp.MustCompile(`^\s*spindown_time\s*=\s*(\d+)\s*$`)
)

// resolveStableDiskID returns a /dev/disk/by-id/* path for the given raw
// block device path (e.g. /dev/sda) so a standby setting written to
// /etc/hdparm.conf survives the device being assigned a different /dev/sdX
// letter across reboots (which /usr/lib/udev/hdparm re-applies on every
// "add" event, including at boot). Falls back to the raw path if no by-id
// symlink is found for it.
func resolveStableDiskID(path string) string {
	const byIDDir = "/dev/disk/by-id"
	entries, err := os.ReadDir(byIDDir)
	if err != nil {
		return path
	}
	for _, e := range entries {
		// Skip partition symlinks (…-partN) - we want the whole-device link,
		// matching the whole-disk path this is always called with.
		if strings.Contains(e.Name(), "-part") {
			continue
		}
		full := filepath.Join(byIDDir, e.Name())
		resolved, err := filepath.EvalSymlinks(full)
		if err != nil {
			continue
		}
		if resolved == path {
			return full
		}
	}
	return path
}

// minutesToSpindownCode maps a friendly minute value onto hdparm -S's
// encoded scale: 0 = disabled, 1-240 = value*5 seconds (up to 20 min),
// 241-251 = (value-240)*30 minutes (up to 5.5 hours).
func minutesToSpindownCode(minutes int) int {
	switch {
	case minutes <= 0:
		return 0
	case minutes <= 20:
		code := minutes * 12 // minutes -> 5-second units
		if code < 1 {
			code = 1
		}
		return code
	default:
		steps := int((float64(minutes) + 15) / 30) // round to nearest 30 min
		if steps < 1 {
			steps = 1
		}
		if steps > 11 {
			steps = 11 // 240 + 11*30 = 330 min (~5.5h), hdparm -S's usable max
		}
		return 240 + steps
	}
}

func spindownCodeToMinutes(code int) int {
	switch {
	case code <= 0:
		return 0
	case code <= 240:
		return (code * 5) / 60
	case code <= 251:
		return (code - 240) * 30
	case code == 252:
		return 21
	default:
		return 0 // 253/255 are vendor-specific - not something we ever write
	}
}

func readHdparmSpindownCode(id string) (int, bool) {
	return readHdparmSpindownCodeFrom(hdparmConfPath, id)
}

func readHdparmSpindownCodeFrom(confPath, id string) (int, bool) {
	data, err := os.ReadFile(confPath)
	if err != nil {
		return 0, false
	}
	lines := strings.Split(string(data), "\n")
	inBlock := false
	for _, line := range lines {
		if !inBlock {
			if m := hdparmBlockHeaderRe.FindStringSubmatch(line); m != nil && m[1] == id {
				inBlock = true
			}
			continue
		}
		if strings.TrimSpace(line) == "}" {
			inBlock = false
			continue
		}
		if m := hdparmSpindownRe.FindStringSubmatch(line); m != nil {
			code, _ := strconv.Atoi(m[1])
			return code, true
		}
	}
	return 0, false
}

// writeHdparmSpindownCodeTo replaces (or removes, for code == 0) this
// device's block in an hdparm.conf. Only ever touches the single block
// matching id. (Legacy format - new timers go to the udev rule.)
func writeHdparmSpindownCodeTo(confPath, id string, code int) error {
	data, err := os.ReadFile(confPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	var lines []string
	if len(data) > 0 {
		lines = strings.Split(string(data), "\n")
	}

	out := make([]string, 0, len(lines))
	i := 0
	for i < len(lines) {
		line := lines[i]
		if m := hdparmBlockHeaderRe.FindStringSubmatch(line); m != nil && m[1] == id {
			i++
			for i < len(lines) && strings.TrimSpace(lines[i]) != "}" {
				i++
			}
			i++ // skip the closing "}" line itself
			continue
		}
		out = append(out, line)
		i++
	}

	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}

	if code > 0 {
		out = append(out, "", id+" {", fmt.Sprintf("\tspindown_time = %d", code), "}")
	}
	out = append(out, "")

	return os.WriteFile(confPath, []byte(strings.Join(out, "\n")), 0o644)
}

// udevSerialRe: characters udev itself allows in ID_SERIAL (it replaces
// everything else), minus anything with meaning in a rule match string.
var udevSerialRe = regexp.MustCompile(`^[A-Za-z0-9#+\-.:=@_]{1,200}$`)

// ParseUdevProperties parses `udevadm info --query=property` output.
func ParseUdevProperties(out string) map[string]string {
	props := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if i := strings.IndexByte(line, '='); i > 0 {
			props[line[:i]] = strings.TrimSpace(line[i+1:])
		}
	}
	return props
}

// StandbyRuleKey picks the udev property to key the rule on: ID_SERIAL,
// else ID_SERIAL_SHORT, else ID_WWN. The value must be rule-safe.
func StandbyRuleKey(props map[string]string) (string, string, error) {
	for _, k := range []string{"ID_SERIAL", "ID_SERIAL_SHORT", "ID_WWN"} {
		if v := props[k]; v != "" {
			if !udevSerialRe.MatchString(v) {
				return "", "", fmt.Errorf("the disk's %s %q can't be used in a udev rule", k, v)
			}
			return k, v, nil
		}
	}
	return "", "", fmt.Errorf("the disk has no serial number udev knows - its standby timer can't be persisted")
}

var standbyRuleRe = regexp.MustCompile(`ENV\{(ID_SERIAL|ID_SERIAL_SHORT|ID_WWN)\}=="([^"]*)".*\s-S\s+(\d+)\s`)

func standbyRuleLine(key, value, hdparm string, code int) string {
	return fmt.Sprintf(`ACTION=="add", SUBSYSTEM=="block", ENV{DEVTYPE}=="disk", ENV{%s}=="%s", RUN+="%s -S %d $devnode"`, key, value, hdparm, code)
}

// UpsertStandbyRule returns rules-file content with this disk's line
// replaced (code > 0) or removed (code == 0). Other lines are kept as-is.
func UpsertStandbyRule(content, key, value, hdparm string, code int) (string, error) {
	if !udevSerialRe.MatchString(value) {
		return "", fmt.Errorf("invalid udev match value %q", value)
	}
	if !filepath.IsAbs(hdparm) || strings.ContainsAny(hdparm, "\"\n $`") {
		return "", fmt.Errorf("invalid hdparm path %q", hdparm)
	}
	if code < 0 || code > 255 {
		return "", fmt.Errorf("invalid spindown code %d", code)
	}
	var out []string
	for _, line := range strings.Split(content, "\n") {
		if m := standbyRuleRe.FindStringSubmatch(line + " "); m != nil && m[1] == key && m[2] == value {
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		out = append(out, line)
	}
	if len(out) == 0 || !strings.HasPrefix(out[0], "#") {
		out = append([]string{"# Managed by NivaroOS local-storage (Settings > Disks > standby). One line per disk."}, out...)
	}
	if code > 0 {
		out = append(out, standbyRuleLine(key, value, hdparm, code))
	}
	return strings.Join(out, "\n") + "\n", nil
}

// ReadStandbyRuleCode finds this disk's code in the rules content.
func ReadStandbyRuleCode(content, key, value string) (int, bool) {
	for _, line := range strings.Split(content, "\n") {
		if m := standbyRuleRe.FindStringSubmatch(line + " "); m != nil && m[1] == key && m[2] == value {
			code, err := strconv.Atoi(m[3])
			return code, err == nil
		}
	}
	return 0, false
}

// removeHdparmConfSpindown drops spindown_time from this device's legacy
// hdparm.conf block (the block itself only if nothing else is left in it).
// Never creates the file; other settings (apm, write cache…) are kept.
func removeHdparmConfSpindown(confPath string, ids ...string) error {
	data, err := os.ReadFile(confPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	lines := strings.Split(string(data), "\n")
	out := make([]string, 0, len(lines))
	changed := false
	for i := 0; i < len(lines); i++ {
		m := hdparmBlockHeaderRe.FindStringSubmatch(lines[i])
		if m == nil || !want[m[1]] {
			out = append(out, lines[i])
			continue
		}
		var body []string
		j := i + 1
		for ; j < len(lines) && strings.TrimSpace(lines[j]) != "}"; j++ {
			if hdparmSpindownRe.MatchString(lines[j]) {
				changed = true
				continue
			}
			body = append(body, lines[j])
		}
		kept := false
		for _, b := range body {
			if t := strings.TrimSpace(b); t != "" && !strings.HasPrefix(t, "#") {
				kept = true
			}
		}
		if kept {
			out = append(out, lines[i])
			out = append(out, body...)
			if j < len(lines) {
				out = append(out, lines[j])
			}
		} else {
			changed = true
		}
		i = j
	}
	if !changed {
		return nil
	}
	return os.WriteFile(confPath, []byte(strings.Join(out, "\n")), 0o644)
}
