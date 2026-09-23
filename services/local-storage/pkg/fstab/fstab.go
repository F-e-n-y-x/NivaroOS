package fstab

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

const (
	PassDoNotCheck      = 0
	PassCheckDuringBoot = 1
	PassCheckAfterBoot  = 2

	DefaultPath = "/etc/fstab"

	// ManagedComment is appended to every entry this package writes via Add, and is
	// used to recognize (on read) which fstab lines are safe for a caller to edit or
	// delete versus pre-existing system entries (root, swap, manually-added shares,
	// etc.) that must never be touched by an automated tool.
	ManagedComment = "Added by the NivaroOS"
)

func isManagedComment(line string) bool {
	return strings.Contains(line, "Added by the NivaroOS") ||
		strings.Contains(line, "Added by NivaroOS") ||
		strings.Contains(line, "Added by the CasaOS") ||
		strings.Contains(line, "Added by CasaOS")
}

var (
	_fstab *FStab

	ErrInvalidFSTabEntry                     = errors.New("invalid fstab entry")
	ErrDifferentFSTabEntryWithSameMountPoint = errors.New("a different fstab entry with the same mount point already exists")
	ErrEntryNotFound                         = errors.New("no matching fstab entry found")
)

type (
	Entry struct {
		// The device name, label, UUID, or other means of specifying the partition or data source this entry refers to.
		Source string

		// Where the contents of the device may be accessed after mounting
		MountPoint string

		// The type of file system to be mounted.
		FSType string

		// Options describing various other aspects of the file system, such as whether it is automatically mounted at boot, which users may mount or access it, whether it may be written to or only read from, its size, and so forth; the special option defaults refers to a pre-determined set of options depending on the file system type.
		Options string

		// A number indicating whether and how often the file system should be backed up by the dump program; a zero indicates the file system will never be automatically backed up.
		Dump int

		// A number indicating the order in which the fsck program will check the devices for errors at boot time
		Pass int

		// Managed is true if this entry was written by this package (carries ManagedComment).
		// Only managed entries are safe for a caller to edit, disable, or delete automatically -
		// entries the admin added or that shipped with the base system (Managed == false) must
		// be left alone.
		Managed bool

		// Enabled is false for a managed entry that has been temporarily commented out (disabled
		// at boot) rather than removed. Always true for entries returned by GetEntries, which only
		// ever sees active (non-commented) lines; GetAllEntries also returns disabled ones.
		Enabled bool
	}

	FStab struct {
		path string
	}
)

func (e *Entry) String() string {
	return e.Source + "\t" + e.MountPoint + "\t" + e.FSType + "\t" + e.Options + "\t" + strconv.Itoa(e.Dump) + "\t" + strconv.Itoa(e.Pass)
}

// mu serialises every change to fstab: read-modify-write sequences used to
// run concurrently through one shared temp file, so two quick changes could
// lose or garble lines (the root filesystem's included).
var mu sync.Mutex

// rewrite replaces fstab with edit(current lines) atomically: a fresh temp
// file in the same directory, fsynced, then renamed over it, with the
// previous version kept as .nivaroos.bak. Callers hold mu.
func (f *FStab) rewrite(edit func(lines []string) ([]string, error)) error {
	var lines []string
	if err := foreachLine(f.path, func(line string) error {
		lines = append(lines, line)
		return nil
	}); err != nil {
		return err
	}
	out, err := edit(lines)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(f.path), ".fstab-nivaroos-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	var b strings.Builder
	for _, l := range out {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	if _, err := tmp.WriteString(b.String()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := copy(f.path, f.path+".nivaroos.bak"); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), f.path)
}

func (f *FStab) Add(e Entry, replace bool) error {
	mu.Lock()
	defer mu.Unlock()
	entry, err := f.GetEntryByMountPoint(e.MountPoint)
	if err != nil {
		return err
	}
	if entry != nil && !replace &&
		(entry.Source != e.Source ||
			entry.FSType != e.FSType ||
			entry.Options != e.Options ||
			entry.Dump != e.Dump ||
			entry.Pass != e.Pass) {
		return ErrDifferentFSTabEntryWithSameMountPoint
	}
	return f.rewrite(func(lines []string) ([]string, error) {
		out := make([]string, 0, len(lines)+1)
		for _, line := range lines {
			if en, _ := parseEntry(line); en != nil && en.MountPoint == e.MountPoint {
				continue // replaced below
			}
			out = append(out, line)
		}
		return append(out, e.String()+"\t# Added by the NivaroOS"), nil
	})
}

// RemoveByMountPoint removes the active entry for mountpoint, or comments
// it out (disables it) when comment is true.
func (f *FStab) RemoveByMountPoint(mountpoint string, comment bool) error {
	mu.Lock()
	defer mu.Unlock()
	return f.rewrite(func(lines []string) ([]string, error) {
		out := make([]string, 0, len(lines))
		for _, line := range lines {
			if en, _ := parseEntry(line); en != nil && en.MountPoint == mountpoint {
				if comment {
					out = append(out, "#"+line)
				}
				continue
			}
			out = append(out, line)
		}
		return out, nil
	})
}

// RemoveAnyByMountPoint removes the entry for mountpoint whether it is
// active or disabled (a managed, commented-out line). Editing a disabled
// drive used to leave the commented line and append a second entry.
func (f *FStab) RemoveAnyByMountPoint(mountpoint string) error {
	mu.Lock()
	defer mu.Unlock()
	return f.rewrite(func(lines []string) ([]string, error) {
		out := make([]string, 0, len(lines))
		for _, line := range lines {
			if en, _ := parseEntry(line); en != nil && en.MountPoint == mountpoint {
				continue
			}
			if t := strings.TrimSpace(line); strings.HasPrefix(t, "#") {
				if en := parseManagedCommentLine(t); en != nil && en.MountPoint == mountpoint {
					continue
				}
			}
			out = append(out, line)
		}
		return out, nil
	})
}

// Enable un-comments a previously disabled managed entry so it becomes active again at
// next boot. It is the inverse of RemoveByMountPoint(mountpoint, true), and - like every
// other mutating method on FStab - it refuses to touch anything that isn't a managed
// entry, since only managed (commented-with-ManagedComment) lines are ever recognized.
func (f *FStab) Enable(mountpoint string) error {
	mu.Lock()
	defer mu.Unlock()
	return f.rewrite(func(lines []string) ([]string, error) {
		found := false
		out := make([]string, 0, len(lines))
		for _, line := range lines {
			t := strings.TrimSpace(line)
			if !found && strings.HasPrefix(t, "#") {
				if en := parseManagedCommentLine(t); en != nil && en.MountPoint == mountpoint {
					found = true
					out = append(out, strings.TrimSpace(strings.TrimPrefix(t, "#")))
					continue
				}
			}
			out = append(out, line)
		}
		if !found {
			return nil, ErrEntryNotFound
		}
		return out, nil
	})
}

// Adopt appends ManagedComment to an existing system fstab line to bring it under NivaroOS management.
func (f *FStab) Adopt(mountpoint string) error {
	mu.Lock()
	defer mu.Unlock()
	return f.rewrite(func(lines []string) ([]string, error) {
		found := false
		out := make([]string, 0, len(lines))
		for _, line := range lines {
			if en, _ := parseEntry(line); en != nil && en.MountPoint == mountpoint && !en.Managed {
				found = true
				out = append(out, strings.TrimRight(line, "\r\n\t ")+"\t# "+ManagedComment)
				continue
			}
			out = append(out, line)
		}
		if !found {
			return nil, ErrEntryNotFound
		}
		return out, nil
	})
}

func (f *FStab) GetEntries() ([]*Entry, error) {
	entries := []*Entry{}

	if err := foreachLine(f.path, func(line string) error {
		entry, err := parseEntry(line)
		if err != nil {
			return err
		}
		if entry != nil {
			entries = append(entries, entry)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return entries, nil
}

// GetAllEntries returns every active entry (like GetEntries) plus any managed entry
// that has been disabled (commented out via SetEnabled/RemoveByMountPoint(_, true)).
// Non-managed comments and blank lines are still ignored, exactly as in GetEntries -
// this only ever reaches into a "#"-prefixed line when it recognizes ManagedComment.
func (f *FStab) GetAllEntries() ([]*Entry, error) {
	entries := []*Entry{}

	if err := foreachLine(f.path, func(line string) error {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "#") {
			if entry := parseManagedCommentLine(trimmed); entry != nil {
				entries = append(entries, entry)
			}
			return nil
		}

		entry, err := parseEntry(line)
		if err != nil {
			return err
		}
		if entry != nil {
			entries = append(entries, entry)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return entries, nil
}

func (f *FStab) GetEntryByMountPoint(mountpoint string) (*Entry, error) {
	entries, err := f.GetEntries()
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.MountPoint == mountpoint {
			return entry, nil
		}
	}

	return nil, nil
}

func (f *FStab) GetEntryBySource(source string) (*Entry, error) {
	entries, err := f.GetEntries()
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.Source == source {
			return entry, nil
		}
	}

	return nil, nil
}

// GetEntryByUUID finds an entry whose Source refers to the given filesystem UUID, in any
// of the common forms an fstab line uses: "UUID=<uuid>", "/dev/disk/by-uuid/<uuid>", or a
// bare "<uuid>".
func (f *FStab) GetEntryByUUID(uuid string) (*Entry, error) {
	entries, err := f.GetEntries()
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		switch entry.Source {
		case uuid, "UUID=" + uuid, "/dev/disk/by-uuid/" + uuid:
			return entry, nil
		}
	}

	return nil, nil
}

// Enable un-comments a previously disabled managed entry so it becomes active again at
// next boot. It is the inverse of RemoveByMountPoint(mountpoint, true), and - like every
// other mutating method on FStab - it refuses to touch anything that isn't a managed
// entry, since only managed (commented-with-ManagedComment) lines are ever recognized.

// Adopt appends ManagedComment to an existing system fstab line to bring it under NivaroOS management.

func Get() *FStab {
	if _fstab == nil {
		_fstab = &FStab{
			path: DefaultPath,
		}
	}

	return _fstab
}

func parseEntry(line string) (*Entry, error) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return nil, nil
	}

	fields := strings.Fields(line)
	if len(fields) < 4 {
		return nil, nil
	}

	entry := Entry{
		Dump:    0,
		Pass:    PassDoNotCheck,
		Managed: isManagedComment(line),
		Enabled: true,
	}

	entry.Source = fields[0]
	entry.MountPoint = fields[1]
	entry.FSType = fields[2]
	entry.Options = fields[3]

	if len(fields) > 4 {
		dump, err := strconv.Atoi(fields[4])
		if err != nil {
			return nil, ErrInvalidFSTabEntry
		}
		entry.Dump = dump
	}

	if len(fields) > 5 {
		pass, err := strconv.Atoi(fields[5])
		if err != nil {
			return nil, ErrInvalidFSTabEntry
		}
		entry.Pass = pass
	}

	return &entry, nil
}

// parseManagedCommentLine tries to recover a managed Entry from a "#"-prefixed line -
// i.e. one previously disabled via RemoveByMountPoint(_, true). trimmedLine must already
// be whitespace-trimmed and start with "#". Returns nil for any comment that isn't one of
// this package's own disabled entries (an ordinary admin comment, a commented-out system
// entry the admin disabled by hand, etc.) - those are left alone.
func parseManagedCommentLine(trimmedLine string) *Entry {
	rest := strings.TrimSpace(strings.TrimPrefix(trimmedLine, "#"))
	if !isManagedComment(rest) {
		return nil
	}

	entry, err := parseEntry(rest)
	if err != nil || entry == nil {
		return nil
	}

	entry.Enabled = false
	return entry
}

func foreachLine(path string, handle func(line string) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Split(bufio.ScanLines)

	for scanner.Scan() {
		line := scanner.Text()
		if err := handle(line); err != nil {
			return err
		}
	}

	return nil
}

func copy(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}
