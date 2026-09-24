package engine

import (
	"sort"
	"strings"
	"time"

	"github.com/rclone/rclone/fs"
)

// fsQuirks is how the engine adapts to one kind of destination (spec
// §6.3 table), keyed by the local filesystem type or the rclone backend.
type fsQuirks struct {
	Quirks []Quirk
	// ModifyWindow is the modtime tolerance; 0 keeps rclone's default
	// (the finer of the two sides' precision).
	ModifyWindow time.Duration
	// WinEncoding makes the local backend store Windows-reserved
	// characters as look-alikes instead of failing.
	WinEncoding bool
	// CaseInsensitive tells rclone that a.txt and A.txt are one file, and
	// makes the prechecks scan the source for such collisions.
	CaseInsensitive bool
	// SizeOnly: modtimes can't be trusted, compare sizes only (TeraBox).
	SizeOnly bool
	// NoHash disables verify.
	NoHash bool
	// Local: owners, modes and symlinks can be kept (Metadata + links).
	Local bool
	// DriveOptions: skip Google Docs, stop at the daily upload limit.
	Drive bool
}

// windowsEncoding is rclone's local-backend encoding on Windows: the
// characters FAT, exFAT and NTFS can't store become full-width
// look-alikes, so every name survives the round trip.
const windowsEncoding = "Slash,LtGt,DoubleQuote,Colon,Question,Asterisk,Pipe,BackSlash,Ctl,RightSpace,RightPeriod,InvalidUtf8,Dot"

// localQuirks returns the quirks of a local filesystem type.
func localQuirks(fstype string) fsQuirks {
	q := fsQuirks{Local: true}
	switch strings.TrimPrefix(fstype, "fuse.") {
	case "vfat", "msdos", "fat", "fat32":
		q.Quirks = []Quirk{QuirkMtime2s, QuirkCaseInsensitive, QuirkNTFSChars, QuirkMaxFile4G}
		q.ModifyWindow = 2 * time.Second
		q.WinEncoding, q.CaseInsensitive = true, true
	case "exfat":
		q.Quirks = []Quirk{QuirkMtime2s, QuirkCaseInsensitive, QuirkNTFSChars}
		q.ModifyWindow = 2 * time.Second
		q.WinEncoding, q.CaseInsensitive = true, true
	case "ntfs", "ntfs3", "fuseblk", "ntfs-3g":
		q.Quirks = []Quirk{QuirkCaseInsensitive, QuirkNTFSChars}
		q.ModifyWindow = time.Microsecond // NTFS keeps 100 ns ticks; allow for rounding
		q.WinEncoding, q.CaseInsensitive = true, true
	case "mergerfs":
		q.Quirks = []Quirk{QuirkPerBranchFree}
	default: // ext4, xfs, btrfs, f2fs, zfs, ...: nanosecond times, POSIX names
		q.ModifyWindow = time.Nanosecond
	}
	return q
}

// remoteQuirks returns the quirks of an rclone backend type ("drive",
// "terabox", "smb", ...). f, when not nil, refines no_hash from what the
// live filesystem offers.
func remoteQuirks(provider string, f fs.Info) fsQuirks {
	q := fsQuirks{Quirks: []Quirk{QuirkNoMetadata}}
	switch provider {
	case "terabox":
		q.Quirks = append(q.Quirks, QuirkNoModTime, QuirkNoHash)
		q.SizeOnly, q.NoHash = true, true
	case "drive":
		q.Quirks = append(q.Quirks, QuirkDailyQuota)
		q.Drive = true
	case "onedrive":
		q.Quirks = append(q.Quirks, QuirkCaseInsensitive, QuirkNTFSChars)
		q.CaseInsensitive = true
	case "dropbox", "box", "pcloud", "koofr":
		q.Quirks = append(q.Quirks, QuirkCaseInsensitive)
		q.CaseInsensitive = true
	case "smb":
		// Windows and Samba shares compare names without case.
		q.Quirks = append(q.Quirks, QuirkCaseInsensitive, QuirkNTFSChars, QuirkNoHash)
		q.CaseInsensitive, q.NoHash = true, true
	case "webdav", "ftp", "iclouddrive":
		q.Quirks = append(q.Quirks, QuirkNoHash)
		q.NoHash = true
	}
	if f != nil {
		if f.Hashes().Count() == 0 && !q.NoHash {
			q.Quirks = append(q.Quirks, QuirkNoHash)
			q.NoHash = true
		}
		if f.Features().CaseInsensitive && !q.CaseInsensitive {
			q.Quirks = append(q.Quirks, QuirkCaseInsensitive)
			q.CaseInsensitive = true
		}
		if f.Precision() == fs.ModTimeNotSupported && !q.SizeOnly {
			q.Quirks = append(q.Quirks, QuirkNoModTime)
			q.SizeOnly = true
		}
	}
	return q
}

// warningsFor maps quirks to the location warnings they imply.
func warningsFor(q []Quirk) []Warning {
	var out []Warning
	for _, x := range q {
		if x == QuirkNoModTime {
			out = append(out, WarnLimitedChangeDetection)
		}
	}
	return out
}

// sortedQuirks returns q without duplicates, in the stable UI order of
// the Quirk constants.
func sortedQuirks(q []Quirk) []Quirk {
	order := map[Quirk]int{
		QuirkMtime2s: 0, QuirkCaseInsensitive: 1, QuirkNTFSChars: 2, QuirkMaxFile4G: 3,
		QuirkNoModTime: 4, QuirkNoHash: 5, QuirkNoMetadata: 6, QuirkPerBranchFree: 7, QuirkDailyQuota: 8,
	}
	seen := map[Quirk]bool{}
	out := []Quirk{}
	for _, x := range q {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return order[out[i]] < order[out[j]] })
	return out
}

// worldWritable reports a FAT/exFAT/NTFS mount whose options let every
// user write (umask=000, fmask/dmask=0000): destination folders there
// can't be made private (spec §14).
func worldWritable(m mountEntry) bool {
	all := m.Options + "," + m.SuperOpts
	for _, k := range []string{"umask", "fmask", "dmask"} {
		if v, ok := mountOptionValue(all, k); ok && strings.Trim(v, "0") == "" {
			return true
		}
	}
	return false
}

// fat32MaxFile is the largest file FAT32 can hold.
const fat32MaxFile = 4<<30 - 1
