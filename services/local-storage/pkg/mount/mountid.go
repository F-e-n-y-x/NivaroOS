package mount

import (
	"os"
	"strings"
)

// Every FUSE mount gets its own anonymous device number (0:NN in
// /proc/self/mountinfo). rclone's unmount is by path: fusermount -u
// /mnt/x unmounts whatever is on top of /mnt/x right now. When an old
// mount of a path finishes closing after a new one has been mounted there
// (a remount, or the rclone daemon's copy of the same drive), a by-path
// unmount takes the new mount down. On 2026-09-30 the daemon's stale
// Google Drive mount did exactly that ~40 s after local-storage mounted
// the drive. Recording the device a mount got, and unmounting only while
// that device is still the one at the path, stops that.

// MountinfoPath is read for mount identities (tests point it elsewhere).
var MountinfoPath = "/proc/self/mountinfo"

func unescapeMountinfo(s string) string {
	return strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`).Replace(s)
}

// TopMountDevice returns the device ("major:minor") of the mount on top at
// path, and whether anything is mounted there at all.
func TopMountDevice(path string) (string, bool) {
	raw, err := os.ReadFile(MountinfoPath)
	if err != nil {
		return "", false
	}
	dev, found := "", false
	// Later lines are mounted later, so the last match is the one on top.
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.Fields(line)
		if len(f) > 4 && unescapeMountinfo(f[4]) == path {
			dev, found = f[2], true
		}
	}
	return dev, found
}

// StillOurs reports whether the mount that got device dev is still the
// one on top at path. With no recorded device it can't tell and says yes
// (the old by-path behaviour).
func StillOurs(path, dev string) bool {
	if dev == "" {
		return true
	}
	cur, ok := TopMountDevice(path)
	return ok && cur == dev
}
