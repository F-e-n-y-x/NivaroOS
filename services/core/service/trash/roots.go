package trash

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// Where a delete can go depends on the filesystem:
//   - local disks: a trash folder on the same disk (a rename);
//   - network shares (SMB/CIFS, NFS, SSHFS): the same, at the share's top
//     folder - a rename inside one share is done by the file server, so it
//     is just as instant and never copies anything;
//   - cloud drives (rclone mounts, owned by nivaroos-local-storage): a
//     rename is a server-side move only when the provider supports moving
//     files and folders; local-storage publishes that per mount (see
//     CloudTrashFile). Some providers keep their own trash (Google Drive,
//     OneDrive, TeraBox): a delete already lands there, so NivaroOS doesn't
//     trash it twice and says where it went;
//   - pseudo filesystems: no Trash.
var (
	shareFS  = map[string]bool{"cifs": true, "smb3": true, "nfs": true, "nfs4": true, "fuse.sshfs": true}
	pseudoFS = map[string]bool{"proc": true, "sysfs": true, "tmpfs": true, "devtmpfs": true, "overlay": true}
)

const (
	KindDisk  = "disk"
	KindShare = "share"
	KindCloud = "cloud"
	KindPhone = "phone"
)

// Why a location has no Trash (Support.Reason).
const (
	ReasonReadOnly     = "readonly"       // can't create the trash folder there
	ReasonNoServerMove = "no_server_move" // cloud drive that can't move files server-side
	ReasonNoTrash      = "no_trash"
)

// Support says what deleting at a path does.
type Support struct {
	// Supported: it goes to the NivaroOS Trash.
	Supported bool   `json:"supported"`
	Kind      string `json:"kind"`
	// Provider: the cloud provider whose own trash a delete lands in
	// instead ("Google Drive"); empty otherwise.
	Provider string `json:"provider,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// Paths for tests.
var (
	mountInfoPath = "/proc/self/mountinfo"
	// CloudTrashFile is written by nivaroos-local-storage: for each cloud
	// mount point, {"mode": "trash"|"provider"|"none", "provider": name}.
	CloudTrashFile = "/var/run/nivaroos/cloud-trash.json"
)

type mountEntry struct {
	point  string
	fstype string
	source string
}

func mountFor(path string) (mountEntry, bool) {
	f, err := os.Open(mountInfoPath)
	if err != nil {
		return mountEntry{}, false
	}
	defer f.Close()
	unescape := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\134`, `\`).Replace
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
		if sep < 0 || sep+2 >= len(fields) || len(fields) < 5 {
			continue
		}
		point := unescape(fields[4])
		if (path == point || strings.HasPrefix(path, strings.TrimSuffix(point, "/")+"/")) && len(point) >= len(best.point) {
			best = mountEntry{point: point, fstype: fields[sep+1], source: unescape(fields[sep+2])}
		}
	}
	return best, best.point != ""
}

func kindOf(fstype string) string {
	switch {
	case shareFS[fstype]:
		return KindShare
	case fstype == "fuse.rclone":
		return KindCloud
	}
	return KindDisk
}

type cloudTrash struct {
	Mode     string `json:"mode"`
	Provider string `json:"provider,omitempty"`
}

func cloudTrashFor(point string) cloudTrash {
	var all map[string]cloudTrash
	if raw, err := os.ReadFile(CloudTrashFile); err == nil && json.Unmarshal(raw, &all) == nil {
		if c, ok := all[point]; ok {
			return c
		}
	}
	// Unknown (an older local-storage, or not mounted by it): a rename
	// could be a full download and upload - no Trash.
	return cloudTrash{Mode: "none"}
}

// writable: the trash folder (or, before it exists, the root it goes in)
// can be written - false on a read-only share or one we may only read.
func writable(root string) bool {
	dir := trashDir(root)
	if _, err := os.Stat(dir); err != nil {
		dir = root
	}
	return canWrite(dir) == nil
}

var canWrite = func(dir string) error { return syscall.Access(dir, 2 /* W_OK */) }

// place finds the root of path's Trash and whether it can have one.
func place(path string) (string, Support) {
	m, ok := mountFor(path)
	if !ok || pseudoFS[m.fstype] {
		return "", Support{Kind: KindDisk, Reason: ReasonNoTrash}
	}
	s := Support{Kind: kindOf(m.fstype)}
	if s.Kind == KindCloud {
		switch c := cloudTrashFor(m.point); c.Mode {
		case "provider":
			s.Provider = c.Provider
			return "", s
		case "trash":
		default:
			s.Reason = ReasonNoServerMove
			return "", s
		}
	}
	root := m.point
	if m.point == "/" {
		// The system disk: keep its trash with the user's data when the
		// item is under /DATA on that disk, out of the way otherwise.
		root = ""
		if strings.HasPrefix(path, "/DATA/") {
			if dm, ok := mountFor("/DATA"); ok && dm.point == "/" {
				root = "/DATA"
			}
		}
		if _, err := os.Stat("/var/lib/nivaroos"); root == "" && err == nil {
			root = "/var/lib/nivaroos"
		}
		if root == "" {
			s.Reason = ReasonNoTrash
			return "", s
		}
	}
	if !writable(root) {
		s.Reason = ReasonReadOnly
		return "", s
	}
	s.Supported = true
	return root, s
}

// SupportsTrash says whether deleting at path goes to the Trash (so the
// UI can say "Move to Trash", or where else it goes, or warn "Delete
// permanently").
func SupportsTrash(path string) Support {
	path = filepath.Clean(path)
	if IsTrashPath(path) {
		m, _ := mountFor(path)
		return Support{Kind: kindOf(m.fstype), Reason: ReasonNoTrash}
	}
	_, s := place(path)
	return s
}

func defaultRootFor(path string) (string, error) {
	root, s := place(path)
	if !s.Supported {
		return "", ErrNoTrashHere
	}
	return root, nil
}

// where describes a trash root for the Trash list: its kind and, for a
// share or cloud drive, which one.
func where(root string) (kind, location string) {
	m, ok := mountFor(root)
	if !ok {
		return KindDisk, ""
	}
	switch k := kindOf(m.fstype); k {
	case KindShare:
		return k, m.source
	case KindCloud:
		return k, filepath.Base(m.point)
	default:
		return k, ""
	}
}
