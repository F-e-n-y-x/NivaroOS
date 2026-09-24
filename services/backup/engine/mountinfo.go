package engine

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// mountEntry is one line of /proc/self/mountinfo (proc(5)):
//
//	36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 - ext3 /dev/root rw,errors=continue
//	(1)(2)(3)   (4)   (5)      (6)      (7)   (8) (9)   (10)         (11)
type mountEntry struct {
	ID         int    // (1) unique per mount; a remount of the same device gets a new one
	Parent     int    // (2)
	MajMin     string // (3) st_dev of files on this mount, "8:33" (0:NN for btrfs, fuse, tmpfs)
	Root       string // (4) the directory of the filesystem that is mounted here ("/" or a subvolume / bind source)
	MountPoint string // (5)
	Options    string // (6) per-mount options
	FSType     string // (9)
	Source     string // (10) "/dev/sdc1", "tmpfs", "a:b" (mergerfs branches), "remote:" (rclone)
	SuperOpts  string // (11) per-superblock options
}

// parseMountInfo reads a mountinfo table. Malformed lines are an error:
// the table decides where writes go, so a half-understood table must not
// be used.
func parseMountInfo(r io.Reader) ([]mountEntry, error) {
	var out []mountEntry
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024) // overlay lines can be very long
	line := 0
	for sc.Scan() {
		line++
		text := sc.Text()
		if strings.TrimSpace(text) == "" {
			continue
		}
		m, err := parseMountInfoLine(text)
		if err != nil {
			return nil, fmt.Errorf("mountinfo line %d: %w", line, err)
		}
		out = append(out, m)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading mountinfo: %w", err)
	}
	return out, nil
}

func parseMountInfoLine(text string) (mountEntry, error) {
	fields := strings.Split(text, " ")
	// Fields 7.. are optional tags up to the "-" separator.
	sep := -1
	for i := 6; i < len(fields); i++ {
		if fields[i] == "-" {
			sep = i
			break
		}
	}
	if len(fields) < 10 || sep < 0 || len(fields) < sep+4 {
		return mountEntry{}, fmt.Errorf("unexpected format %q", text)
	}
	id, err := strconv.Atoi(fields[0])
	if err != nil {
		return mountEntry{}, fmt.Errorf("mount id %q: %w", fields[0], err)
	}
	parent, err := strconv.Atoi(fields[1])
	if err != nil {
		return mountEntry{}, fmt.Errorf("parent id %q: %w", fields[1], err)
	}
	return mountEntry{
		ID:         id,
		Parent:     parent,
		MajMin:     fields[2],
		Root:       unescapeMountField(fields[3]),
		MountPoint: unescapeMountField(fields[4]),
		Options:    fields[5],
		FSType:     unescapeMountField(fields[sep+1]),
		Source:     unescapeMountField(fields[sep+2]),
		SuperOpts:  fields[sep+3],
	}, nil
}

// unescapeMountField undoes the kernel's octal escaping of space, tab,
// newline and backslash (\040, \011, \012, \134) in mountinfo paths.
func unescapeMountField(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) && isOctal(s[i+1]) && isOctal(s[i+2]) && isOctal(s[i+3]) {
			v := (s[i+1]-'0')<<6 | (s[i+2]-'0')<<3 | (s[i+3] - '0')
			b.WriteByte(v)
			i += 3
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isOctal(c byte) bool { return c >= '0' && c <= '7' }

// hasMountOption reports whether a comma-separated option list contains
// opt exactly, or "opt=..." when opt ends with "=".
func hasMountOption(list, opt string) bool {
	for _, o := range strings.Split(list, ",") {
		if o == opt || (strings.HasSuffix(opt, "=") && strings.HasPrefix(o, opt)) {
			return true
		}
	}
	return false
}

// mountOptionValue returns the value of "key=value" in a comma-separated
// option list.
func mountOptionValue(list, key string) (string, bool) {
	for _, o := range strings.Split(list, ",") {
		if k, v, ok := strings.Cut(o, "="); ok && k == key {
			return v, true
		}
	}
	return "", false
}

// mountTable is one parsed snapshot of the mount table.
type mountTable struct {
	entries []mountEntry
	byID    map[int]int // mount id -> index
}

func newMountTable(entries []mountEntry) *mountTable {
	t := &mountTable{entries: entries, byID: make(map[int]int, len(entries))}
	for i, m := range entries {
		t.byID[m.ID] = i
	}
	return t
}

func (t *mountTable) byMountID(id int) (mountEntry, bool) {
	if t == nil {
		return mountEntry{}, false
	}
	i, ok := t.byID[id]
	if !ok {
		return mountEntry{}, false
	}
	return t.entries[i], true
}

// shadowed reports whether mount i of the table can't be reached any
// more because something was mounted over it or over one of its
// ancestors afterwards. Sandboxed services see this all the time:
// systemd's ProtectSystem/ReadWritePaths bind /DATA onto itself, which
// hides the original /DATA/<disk> mounts, and propagation then adds
// visible copies of them below the bind. A shadowed mount must never be
// picked as where a path lives: its mount point now leads elsewhere.
// Table order is mount order; a later mount at the same point, or at an
// ancestor that isn't in i's own parent chain, is on top of it.
func (t *mountTable) shadowed(i int) bool {
	m := t.entries[i]
	for _, n := range t.entries[i+1:] {
		if n.ID == m.ID || !pathWithin(m.MountPoint, n.MountPoint) || t.isAncestor(n.ID, m) {
			continue
		}
		return true
	}
	return false
}

// isAncestor reports whether mount id is m's parent, grandparent, ...
func (t *mountTable) isAncestor(id int, m mountEntry) bool {
	for hops := 0; hops < len(t.entries); hops++ {
		if m.Parent == id {
			return true
		}
		p, ok := t.byMountID(m.Parent)
		if !ok || p.ID == m.ID {
			return false
		}
		m = p
	}
	return false
}

// containing returns the mount a (clean, absolute, symlink-free) path
// lives on: the longest mount point that is the path or an ancestor of
// it. When several mounts share that mount point the last one in the
// table is the visible one (it was mounted on top).
func (t *mountTable) containing(p string) (mountEntry, bool) {
	best, bestLen, found := mountEntry{}, -1, false
	for _, m := range t.entries {
		if !pathWithin(p, m.MountPoint) {
			continue
		}
		if l := len(m.MountPoint); l >= bestLen {
			best, bestLen, found = m, l, true
		}
	}
	return best, found
}

// readMountTable reads and parses the mountinfo file at path.
func readMountTable(path string) (*mountTable, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	entries, err := parseMountInfo(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	return newMountTable(entries), nil
}

// fsPathIn converts a path below mount m to the path inside m's
// filesystem ("/" + subvolume or bind root + the rest).
func fsPathIn(m mountEntry, p string) string {
	rel, _ := filepath.Rel(m.MountPoint, p)
	if rel == "." {
		rel = ""
	}
	return filepath.Join("/", m.Root, rel)
}
