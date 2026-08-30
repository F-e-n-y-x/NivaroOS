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

// writeHdparmSpindownCode replaces (or removes, for code == 0) this device's
// block in /etc/hdparm.conf. Only ever touches the single block matching id -
// any other content in the file (comments, other devices' blocks) is left
// exactly as it was.
func writeHdparmSpindownCode(id string, code int) error {
	return writeHdparmSpindownCodeTo(hdparmConfPath, id, code)
}

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
