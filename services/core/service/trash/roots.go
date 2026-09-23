package trash

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Filesystems a Trash can't live on: cloud drives (a "rename" there is an
// upload), network shares and pseudo filesystems.
var noTrashFS = map[string]bool{
	"fuse.rclone": true, "cifs": true, "smb3": true, "nfs": true, "nfs4": true, "fuse.sshfs": true,
	"proc": true, "sysfs": true, "tmpfs": true, "devtmpfs": true, "overlay": true,
}

type mountEntry struct {
	point  string
	fstype string
}

func mountFor(path string) (mountEntry, bool) {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return mountEntry{}, false
	}
	defer f.Close()
	var best mountEntry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		sep := -1
		for i, x := range fields {
			if x == "-" {
				sep = i
				break
			}
		}
		if sep < 0 || sep+1 >= len(fields) || len(fields) < 5 {
			continue
		}
		point := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\134`, `\`).Replace(fields[4])
		if (path == point || strings.HasPrefix(path, strings.TrimSuffix(point, "/")+"/")) && len(point) >= len(best.point) {
			best = mountEntry{point: point, fstype: fields[sep+1]}
		}
	}
	return best, best.point != ""
}

// SupportsTrash reports whether deleting at path can go to a Trash (so the
// UI can say "Move to Trash" or warn "Delete permanently").
func SupportsTrash(path string) bool {
	m, ok := mountFor(filepath.Clean(path))
	return ok && !noTrashFS[m.fstype]
}

func defaultRootFor(path string) (string, error) {
	m, ok := mountFor(path)
	if !ok {
		return "", ErrNoTrashHere
	}
	if noTrashFS[m.fstype] {
		return "", ErrNoTrashHere
	}
	if m.point != "/" {
		return m.point, nil
	}
	// The system disk: keep its trash with the user's data when the item
	// is under /DATA on that disk, out of the way otherwise.
	if strings.HasPrefix(path, "/DATA/") {
		if dm, ok := mountFor("/DATA"); ok && dm.point == "/" {
			return "/DATA", nil
		}
	}
	if _, err := os.Stat("/var/lib/nivaroos"); err == nil {
		return "/var/lib/nivaroos", nil
	}
	return "", errors.New("no place for a Trash on this drive")
}
