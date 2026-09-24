// Package engine moves the data for Backup & Sync: it resolves endpoints
// (mountinfo + fs UUID, identity marker), runs the prechecks and guards,
// and does copy / mirror / archive / restore through rclone linked as a
// library. The job side (package jobs) drives it only through the API
// interface below - spec §11.1, which was a unix-socket HTTP API when the
// engine lived in local-storage and is now an in-process Go interface
// inside the nivaroos-backup module (spec §0).
//
// This file is the FROZEN contract between the jobs and engine packages.
// The JSON tags are kept so the types can be logged, used as fixtures
// (testdata/engine/*.json, checked by api_test.go) and passed through the
// REST API unchanged. Change it only additively, and bump APIVersion if a
// change is not additive.
package engine

import (
	"context"
	"fmt"
	"io"
	"time"
)

// APIVersion is reported by Health. It only changes when this contract
// changes incompatibly.
const APIVersion = 1

// API is everything the job side may ask of the engine. Every method is
// safe for concurrent use. Errors are *Error values carrying a code from
// errors.go (use CodeOf); any other error is treated as io_error.
type API interface {
	// Health reports the contract version and the linked rclone.
	Health(ctx context.Context) (Health, error)

	// Locations lists what is present right now: volumes, USB drives,
	// merge pools and rclone remotes (spec §6.1). It does not know SMB
	// connections, remembered-but-absent USB drives, apps or VMs - the
	// job side merges those in.
	Locations(ctx context.Context) ([]Location, error)

	// Volumes is the current mount table as the engine sees it (block
	// device mounts with a filesystem UUID, plus merge pools). The job
	// side diffs it against its last snapshot as a reconcile backstop for
	// Events.
	Volumes(ctx context.Context) ([]Volume, error)

	// Resolve maps an endpoint to where it is right now. An offline
	// endpoint is not an error: it returns Online=false. Errors are for
	// endpoints that can never resolve (endpoint_unknown,
	// ambiguous_device, path_not_allowed, cloud_auth).
	Resolve(ctx context.Context, req ResolveRequest) (Resolved, error)

	// ResolvePath turns an absolute local path (Files' "Back up this
	// folder", migration of schedules.json) into an endpoint.
	ResolvePath(ctx context.Context, req ResolvePathRequest) (ResolvePathResult, error)

	// Browse lists one directory of an endpoint, or of a version of a
	// job's destination (VersionID), never above the endpoint root.
	Browse(ctx context.Context, req BrowseRequest) (BrowseResult, error)

	// Precheck runs every §6.3 check that needs no write (resolution,
	// inside-each-other, allowed roots, free space, fs quirks, source
	// sentinel) and estimates the size. It backs POST /validate.
	Precheck(ctx context.Context, req PrecheckRequest) (PrecheckResult, error)

	// ListVersions lists the restorable versions of a job's destination
	// (spec §11 GET /jobs/:id/versions), newest first, "current" first.
	ListVersions(ctx context.Context, req VersionsRequest) ([]Version, error)

	// OpenDownload prepares a single file (raw) or several paths (zip) of
	// a version for streaming to the browser. The caller must close Body.
	OpenDownload(ctx context.Context, req DownloadRequest) (*Download, error)

	// StartJob queues a data job and returns at once. The engine runs at
	// most 2 jobs at a time and at most 1 that touches a cloud remote;
	// the rest wait in state "queued". The job keeps running after ctx
	// ends - ctx only bounds the call itself; use StopJob to cancel.
	StartJob(ctx context.Context, req JobRequest) (JobID, error)

	// JobStatus returns live stats while running and the result when
	// done. Finished jobs are kept for 1 h (then not_found).
	JobStatus(ctx context.Context, id JobID) (JobStatus, error)

	// StopJob cancels a queued or running job (result error_code
	// cancelled_by_user). Stopping a finished job is a no-op.
	StopJob(ctx context.Context, id JobID) error

	// JobLog streams the job's log lines from the start. The channel is
	// closed after the last line of a finished job, or when ctx ends.
	JobLog(ctx context.Context, id JobID) (<-chan LogLine, error)

	// Events streams volume and job events until ctx ends, then closes
	// the channel. A slow reader loses events rather than blocking the
	// engine (buffer of 64); the job side reconciles with Volumes and
	// JobStatus every 60 s.
	Events(ctx context.Context) (<-chan Event, error)
}

// JobID identifies one engine job. It is only unique within one process
// lifetime, which is all RunRow.EngineJobID needs.
type JobID int64

// ---------------------------------------------------------------------
// Endpoints (spec §4, shared with the job model; jobs aliases these)

// EndpointKind says how RefID is interpreted.
type EndpointKind string

const (
	// EPVolume is an internal, fstab or root-filesystem volume. RefID is
	// the filesystem UUID.
	EPVolume EndpointKind = "volume"
	// EPUSB is a removable drive. RefID is the filesystem UUID; Match
	// pins serial and partition size (FAT/exFAT UUIDs are 8 hex digits
	// and get cloned).
	EPUSB EndpointKind = "usb"
	// EPMerge is a mergerfs pool. RefID is the pool mount point.
	EPMerge EndpointKind = "merge"
	// EPSMB is a network share. RefID is core's ConnectionsDBModel ID;
	// the engine reaches it with rclone's smb backend and per-run
	// credentials (SMBCreds), never through the kernel CIFS mount.
	EPSMB EndpointKind = "smb"
	// EPCloud is an rclone remote. RefID is the section name in
	// rclone.conf.
	EPCloud EndpointKind = "cloud"
)

// Valid reports whether k is one of the known kinds.
func (k EndpointKind) Valid() bool {
	switch k {
	case EPVolume, EPUSB, EPMerge, EPSMB, EPCloud:
		return true
	}
	return false
}

// Endpoint is a location by identity, never by path. Label is display
// only and never used to resolve.
type Endpoint struct {
	Kind  EndpointKind `json:"kind"`
	RefID string       `json:"ref_id"`
	Match *DevMatch    `json:"match,omitempty"` // usb only
	// SubPath is relative, slash separated, cleaned: no "..", no NUL, no
	// leading "/". "" is the endpoint root.
	SubPath string `json:"sub_path"`
	Label   string `json:"label"`
	// Preset is "data:<Folder>", "appdata:<app>", "vm:<name>" or
	// "share:<id>" when the source was picked from a preset (the same ids
	// as FolderPreset.ID). It drives hook defaults, the
	// AppData/VM-to-cloud rule and the UI; it never changes resolution.
	Preset string `json:"preset,omitempty"`
}

// DevMatch pins a removable drive beyond its filesystem UUID.
type DevMatch struct {
	Serial    string `json:"serial,omitempty"`
	SizeBytes int64  `json:"size_bytes,omitempty"` // partition size
}

// SMBCreds are sent per call for EPSMB endpoints, keyed by RefID in the
// requests that take them. They are held in memory for that call or job
// only and never written to rclone.conf or logged (String redacts).
type SMBCreds struct {
	Host     string `json:"host"`
	Share    string `json:"share"`
	User     string `json:"user"`
	Password string `json:"password"`
	Domain   string `json:"domain,omitempty"`
}

// String never includes the password, so creds are safe in %v logs.
func (c SMBCreds) String() string {
	return fmt.Sprintf(`\\%s\%s (user %q, password redacted)`, c.Host, c.Share, c.User)
}

// GoString keeps %#v from printing the password too.
func (c SMBCreds) GoString() string { return c.String() }

// ---------------------------------------------------------------------
// Health, locations, volumes

// Health is the engine's contract/version report.
type Health struct {
	API       int       `json:"api"`    // == APIVersion
	Rclone    string    `json:"rclone"` // e.g. "v1.75.1"
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
}

// Quirk is a property of a destination the UI explains and the engine
// adapts to (spec §6.3 table).
type Quirk string

const (
	QuirkCaseInsensitive Quirk = "case_insensitive" // a.txt and A.txt collide
	QuirkNTFSChars       Quirk = "ntfs_chars"       // Windows-reserved characters are encoded
	QuirkMtime2s         Quirk = "mtime_2s"         // FAT/exFAT: 2 s modtime resolution
	QuirkMaxFile4G       Quirk = "max_file_4g"      // FAT32: no file of 4 GiB or more
	QuirkNoModTime       Quirk = "no_modtime"       // changes detected by size only (TeraBox)
	QuirkNoHash          Quirk = "no_hash"          // verify is not available
	QuirkNoMetadata      Quirk = "no_metadata"      // owners, modes and links are not kept (clouds)
	QuirkPerBranchFree   Quirk = "per_branch_free"  // mergerfs: free space is per branch
	QuirkDailyQuota      Quirk = "daily_quota"      // Google Drive 750 GB/day upload limit
)

// Warning is a non-blocking concern about a location, shown in pickers.
type Warning string

const (
	WarnLimitedChangeDetection Warning = "limited_change_detection" // no_modtime
	WarnSystemDisk             Warning = "system_disk"              // same disk as the OS
	WarnExportedShare          Warning = "exported_share"           // inside a Samba share others can write
	WarnWorldWritable          Warning = "world_writable"           // NTFS/exFAT mounted umask=000
)

// FolderPreset is a well-known folder inside a location (the /DATA
// folders, AppData, VMs), offered as a shortcut - never a hard-coded
// location of its own.
type FolderPreset struct {
	// ID is "data:<Folder>", "appdata:<app>" or "vm:<name>".
	ID      string `json:"id"`
	SubPath string `json:"sub_path"`
	Label   string `json:"label"`
}

// Location is one entry of Locations (spec §6.1).
type Location struct {
	Kind         EndpointKind   `json:"kind"`
	RefID        string         `json:"ref_id"`
	Match        *DevMatch      `json:"match,omitempty"`
	Label        string         `json:"label"`
	FSType       string         `json:"fstype,omitempty"`   // volume/usb/merge
	Provider     string         `json:"provider,omitempty"` // cloud: rclone backend type ("drive", "terabox")
	MountPoint   string         `json:"mount_point,omitempty"`
	Online       bool           `json:"online"`
	Free         *int64         `json:"free"`            // bytes; null = unknown (no About, still loading)
	Total        *int64         `json:"total,omitempty"` // bytes
	Removable    bool           `json:"removable"`
	SystemDisk   bool           `json:"system_disk"`
	PhysicalDisk string         `json:"physical_disk,omitempty"` // "sdb"; compare to warn about same-disk backups
	LastSeen     *time.Time     `json:"last_seen,omitempty"`     // set by the job side for remembered, absent drives
	Quirks       []Quirk        `json:"quirks"`
	Warnings     []Warning      `json:"warnings"`
	Presets      []FolderPreset `json:"presets,omitempty"`
}

// Volume is one mounted filesystem, as in Volumes and volume events.
type Volume struct {
	MountID    int    `json:"mount_id"` // mountinfo field 1; changes on every mount
	UUID       string `json:"uuid"`
	Serial     string `json:"serial,omitempty"`
	Size       int64  `json:"size"` // partition size, bytes
	FSType     string `json:"fstype"`
	MountPoint string `json:"mount_point"`
	Label      string `json:"label,omitempty"`
	Tran       string `json:"tran,omitempty"` // lsblk TRAN: "usb", "sata", "nvme"
}

// ---------------------------------------------------------------------
// Resolve

// ResolveRequest asks where an endpoint is right now.
type ResolveRequest struct {
	Endpoint Endpoint  `json:"endpoint"`
	SMBCreds *SMBCreds `json:"smb_creds,omitempty"` // required for EPSMB
}

// Resolved is where an endpoint is right now.
type Resolved struct {
	// Root is for display and logs only: a mount path, "remote:path" or
	// "smb://host/share/sub" (never credentials). Never feed it back.
	Root    string  `json:"root"`
	MountID int     `json:"mount_id,omitempty"` // volume/usb/merge
	Online  bool    `json:"online"`
	Quirks  []Quirk `json:"quirks"`
	Free    *int64  `json:"free"`
	// Marker is the destination identity marker found at Root/SubPath
	// (spec §6.2), or null when there is none.
	Marker *Marker `json:"marker"`
}

// MarkerFile is the destination identity marker's name, relative to the
// destination folder. Mirror never deletes it.
const MarkerFile = ".nivaroos-backup.json"

// VersionsDir is the recycle folder under a mirror destination.
// Deleted and replaced files go to VersionsDir/<run start, UTC,
// 20060102T150405Z>. Mirror never deletes it.
const VersionsDir = ".nivaro-versions"

// VersionTimeLayout names recycle folders and archive timestamps.
const VersionTimeLayout = "20060102T150405Z"

// Marker is the content of MarkerFile.
type Marker struct {
	V            int       `json:"v"` // 1
	JobID        string    `json:"job_id"`
	DestFolderID string    `json:"dest_folder_id"` // uuid4, JobRow.DestFolderID
	FSUUID       string    `json:"fs_uuid,omitempty"`
	Created      time.Time `json:"created"`
	Host         string    `json:"host"` // sha256(machine-id), hex, first 16
}

// ResolvePathRequest is an absolute local path to turn into an endpoint.
type ResolvePathRequest struct {
	Path string `json:"path"`
}

// ResolvePathResult answers ResolvePath. OK=false with Reason (an error
// code, e.g. path_not_allowed, endpoint_unknown) when the path can't be
// expressed as an endpoint; Endpoint is then null.
type ResolvePathResult struct {
	Endpoint *Endpoint `json:"endpoint"`
	OK       bool      `json:"ok"`
	Reason   ErrorCode `json:"reason,omitempty"`
}

// ---------------------------------------------------------------------
// Browse, versions, downloads

// MaxBrowseEntries caps one Browse page.
const MaxBrowseEntries = 2000

// BrowseRequest lists Path (relative to Endpoint.SubPath) of an endpoint.
// With VersionID set, Endpoint is a job destination and the listing is of
// that version: "current", a recycle folder "v_<ts>" or an archive
// "a_<file name>" (its tar index).
type BrowseRequest struct {
	Endpoint  Endpoint  `json:"endpoint"`
	SMBCreds  *SMBCreds `json:"smb_creds,omitempty"`
	VersionID string    `json:"version_id,omitempty"`
	Path      string    `json:"path"`
	DirsOnly  bool      `json:"dirs_only"`
}

// Entry is one row of a listing.
type Entry struct {
	Name  string     `json:"name"`
	Dir   bool       `json:"dir"`
	Size  int64      `json:"size"`
	MTime *time.Time `json:"mtime"` // null when the backend has none
}

// BrowseResult is one directory, sorted dirs first then by name.
type BrowseResult struct {
	Path      string  `json:"path"`
	Entries   []Entry `json:"entries"`
	Truncated bool    `json:"truncated"` // more than MaxBrowseEntries
}

// VersionsRequest names a job destination and how to read its versions.
type VersionsRequest struct {
	JobID    string    `json:"job_id"`
	JobType  string    `json:"job_type"` // "copy" | "mirror" | "archive"
	Dest     Endpoint  `json:"dest"`
	SMBCreds *SMBCreds `json:"smb_creds,omitempty"`
}

// VersionKind is what a Version is.
type VersionKind string

const (
	VersionCurrent VersionKind = "current" // the destination as it is
	VersionRecycle VersionKind = "recycle" // a .nivaro-versions/<ts> folder
	VersionArchive VersionKind = "archive" // one nivaro_<job>_<ts>.tar.zst
)

// Version is one restorable point. IDs: "current", "v_<ts>" (recycle),
// "a_<archive file name>" (archive).
type Version struct {
	ID       string      `json:"id"`
	Kind     VersionKind `json:"kind"`
	Time     *time.Time  `json:"time"` // null for current
	LabelKey string      `json:"label_key,omitempty"`
	Files    int64       `json:"files,omitempty"`
	Bytes    int64       `json:"bytes,omitempty"`
}

// DownloadRequest names files of a version to stream. One file that is
// not a directory streams raw; anything else streams as a zip.
type DownloadRequest struct {
	Dest      Endpoint  `json:"dest"`
	SMBCreds  *SMBCreds `json:"smb_creds,omitempty"`
	VersionID string    `json:"version_id"`
	Paths     []string  `json:"paths"`
}

// Download is an open stream. Size is -1 when unknown (zip).
type Download struct {
	Name        string        `json:"name"`
	ContentType string        `json:"content_type"`
	Size        int64         `json:"size"`
	Body        io.ReadCloser `json:"-"`
}

// ---------------------------------------------------------------------
// Transfer options (spec §4; jobs aliases these)

// Filters select what a job transfers.
type Filters struct {
	// ExcludePresets are named sets: see ExcludePreset* constants.
	ExcludePresets []string `json:"exclude_presets"`
	// Exclude / Include are rclone filter globs, validated with
	// filter.NewFilter at save.
	Exclude      []string `json:"exclude"`
	Include      []string `json:"include,omitempty"`
	MaxSizeBytes int64    `json:"max_size_bytes,omitempty"`
}

// Exclude presets.
const (
	ExcludePresetCaches      = "caches"
	ExcludePresetTrash       = "trash"
	ExcludePresetTemp        = "temp"
	ExcludePresetThumbs      = "thumbs"
	ExcludePresetNodeModules = "node_modules"
)

// ExcludePresets lists every preset, in UI order.
var ExcludePresets = []string{ExcludePresetCaches, ExcludePresetTrash, ExcludePresetTemp, ExcludePresetThumbs, ExcludePresetNodeModules}

// Options tune one job.
type Options struct {
	// Verify runs a one-way check after the transfer; ignored (with a log
	// line) on destinations with QuirkNoHash.
	Verify bool `json:"verify"`
	// PreviewFirst makes the job side run a preview (OpPlan) before the
	// first real run. The engine does not read it.
	PreviewFirst bool `json:"preview_first"`
	// LowPriority: nice 10 + idle I/O class on the transfer thread and
	// Transfers=2. Best effort (spec §4).
	LowPriority    bool  `json:"low_priority"`
	MaxDurationSec int   `json:"max_duration_sec"`
	PreserveMeta   *bool `json:"preserve_meta,omitempty"` // nil = true for local-ish destinations
	CopyEmptyDirs  bool  `json:"copy_empty_dirs"`
}

// Guards stop a run before it does damage (spec §6.4). Percentages are
// 1..100.
type Guards struct {
	EmptySourcePct int  `json:"empty_source_pct"`
	DeletePct      int  `json:"delete_pct"`
	ChangePct      int  `json:"change_pct"`
	AllowEmptySrc  bool `json:"allow_empty_source"`
}

// Retention says what prune ops remove.
type Retention struct {
	VersionsDays int `json:"versions_days,omitempty"` // mirror; 0 = keep forever
	KeepLast     int `json:"keep_last,omitempty"`     // archive
}

// ---------------------------------------------------------------------
// Prechecks

// CheckStatus is the outcome of one check.
type CheckStatus string

const (
	CheckPass CheckStatus = "pass"
	CheckWarn CheckStatus = "warn"
	CheckFail CheckStatus = "fail"
	CheckSkip CheckStatus = "skip" // not applicable or not possible (e.g. no About)
)

// Check IDs (spec §6.3). The job side adds its own for validation
// (see jobs.Check*); the UI names each with "backup.check.<id>".
const (
	CheckSourceResolves = "source_resolves"
	CheckDestResolves   = "dest_resolves"
	CheckNotInside      = "not_inside"
	CheckAllowedRoots   = "allowed_roots"
	CheckFreeSpace      = "free_space"
	CheckFSQuirks       = "fs_quirks"
	CheckMetadata       = "metadata"
	CheckSourceSentinel = "source_sentinel"
	CheckDestMarker     = "dest_marker"
)

// Check is one precheck result. Code is set for warn and fail (the UI
// explains it with backup.err.<code>.*); Args carry numbers for the text.
type Check struct {
	ID     string                 `json:"id"`
	Status CheckStatus            `json:"status"`
	Code   ErrorCode              `json:"code,omitempty"`
	MsgKey string                 `json:"msg_key"`
	Args   map[string]interface{} `json:"args,omitempty"`
}

// PrecheckRequest is a would-be job: nothing is written.
type PrecheckRequest struct {
	JobType      string              `json:"job_type"` // "copy" | "mirror" | "archive"
	Sources      []Endpoint          `json:"sources"`
	Dest         Endpoint            `json:"dest"`
	SMBCreds     map[string]SMBCreds `json:"smb_creds,omitempty"` // by RefID
	Filters      Filters             `json:"filters"`
	Guards       Guards              `json:"guards"`
	DestFolderID string              `json:"dest_folder_id,omitempty"`
	FirstRun     bool                `json:"first_run"`
	Baseline     *Baseline           `json:"baseline,omitempty"`
	// SizeTimeoutSec bounds the source size walk (default 20). A walk cut
	// short leaves Estimate.Partial=true.
	SizeTimeoutSec int `json:"size_timeout_sec,omitempty"`
}

// Estimate sizes a would-be run.
type Estimate struct {
	SourceBytes int64  `json:"source_bytes"`
	SourceFiles int64  `json:"source_files"`
	DestFree    *int64 `json:"dest_free"`
	NeedBytes   int64  `json:"need_bytes"` // what the free-space check requires
	Partial     bool   `json:"partial"`
}

// PrecheckResult is every check plus the estimate. OK is false when any
// check failed.
type PrecheckResult struct {
	OK       bool     `json:"ok"`
	Checks   []Check  `json:"checks"`
	Estimate Estimate `json:"estimate"`
}

// Baseline is what the last successful run saw; the empty-source sentinel
// and the planning-pass threshold (skipped below 1000 files) use it.
type Baseline struct {
	SourceFiles int64 `json:"source_files"`
	DestFiles   int64 `json:"dest_files"`
}

// ---------------------------------------------------------------------
// Jobs

// Op is what a data job does.
type Op string

const (
	OpCopy          Op = "copy"           // copy new/changed files, never delete (job type copy)
	OpSync          Op = "sync"           // mirror with the recycle folder (job type mirror)
	OpArchive       Op = "archive"        // one .tar.zst of all sources (job type archive)
	OpPlan          Op = "plan"           // dry run of copy/sync/archive: counts + preview list, no writes
	OpCheck         Op = "check"          // one-way verify of source against destination
	OpPurgeVersions Op = "purge_versions" // drop recycle folders older than Retention.VersionsDays
	OpPurgeArchives Op = "purge_archives" // keep Retention.KeepLast archives
	OpRestore       Op = "restore"        // copy paths of a version (any kind) to a target
	// OpPurgeDest deletes the job's folder at the destination (DELETE
	// /jobs/:id?purge_data=true), after checking its marker against
	// DestFolderID. At the root of a drive only the engine's own files
	// (marker, recycle folder, the job's archives) go. Added after v1 of
	// this contract; additive, so APIVersion stays 1.
	OpPurgeDest Op = "purge_dest"
)

// JobRequest starts one engine job. Which fields matter depends on Op:
//
//	copy/sync/plan/check: Sources (exactly 1), Dest, Filters, Options, Guards
//	archive:              Sources (1..16), Dest, Filters, Options
//	plan:                 also PlanOp (the op being previewed) and PreviewFile
//	purge_versions/archives: Dest, Retention
//	purge_dest:           Dest, DestFolderID
//	restore:              Dest (the job's destination) and Restore
type JobRequest struct {
	Op    Op     `json:"op"`
	RunID string `json:"run_id"` // stats group "bk_<run_id>", log correlation
	JobID string `json:"job_id"` // archive names, marker
	// PlanOp is the op a plan previews: copy, sync or archive.
	PlanOp   Op                  `json:"plan_op,omitempty"`
	Sources  []Endpoint          `json:"sources,omitempty"`
	Dest     Endpoint            `json:"dest"`
	SMBCreds map[string]SMBCreds `json:"smb_creds,omitempty"` // by RefID
	Filters  Filters             `json:"filters"`
	Options  Options             `json:"options"`
	Guards   Guards              `json:"guards"`
	// GuardOverride lists guards the user already accepted for this run
	// ("delete", "change", "empty_source"); they don't trip again.
	GuardOverride []string `json:"guard_override,omitempty"`
	// GuardReviewed is what the user was shown when they accepted the
	// delete or change guard. It bounds the override: a new plan that
	// deletes or changes more trips the guard again, and the delete limit
	// is the reviewed count. Nil (a decision from an older version) leaves
	// the override unbounded.
	GuardReviewed *Reviewed `json:"guard_reviewed,omitempty"`
	Retention     Retention `json:"retention"`
	// DestFolderID must match the destination marker; with FirstRun and
	// no marker present, the engine writes one with it.
	DestFolderID string    `json:"dest_folder_id"`
	FirstRun     bool      `json:"first_run"`
	Baseline     *Baseline `json:"baseline,omitempty"`
	// PreviewFile is an absolute path (under the job side's data dir)
	// where plan writes one PreviewItem JSON object per line. Optional on
	// copy/sync: their planning pass writes it, so a run stopped by a
	// guard can be reviewed item by item.
	PreviewFile string       `json:"preview_file,omitempty"`
	Restore     *RestoreSpec `json:"restore,omitempty"`
}

// Conflict policy for restores.
const (
	ConflictKeepBoth  = "keep_both" // "name (restored 2026-09-24).ext"
	ConflictOverwrite = "overwrite"
	ConflictSkip      = "skip"
)

// RestoreSpec says what to restore where.
type RestoreSpec struct {
	VersionID string   `json:"version_id"` // "current" | "v_<ts>" | "a_<file>"
	Paths     []string `json:"paths"`      // relative to the destination root; [] = everything
	// Target is where the paths go. For "restore to original" the job
	// side passes the job's (single) source endpoint.
	Target   Endpoint `json:"target"`
	Conflict string   `json:"conflict"` // Conflict* constants
	DryRun   bool     `json:"dry_run"`
}

// JobState is where an engine job is.
type JobState string

const (
	JobQueued  JobState = "queued"  // waiting for an engine slot
	JobRunning JobState = "running" // Stats are live
	JobDone    JobState = "done"    // Result set, Result.ErrorCode "" or file errors only
	JobError   JobState = "error"   // Result set with ErrorCode
)

// Stats are live accounting for a running job.
type Stats struct {
	Bytes       int64  `json:"bytes"`
	TotalBytes  int64  `json:"total_bytes"`
	Files       int64  `json:"files"`
	TotalFiles  int64  `json:"total_files"`
	SpeedBps    int64  `json:"speed_bps"`
	ETASec      *int64 `json:"eta_sec"` // null = unknown
	Errors      int64  `json:"errors"`
	CurrentFile string `json:"current_file"`
	// Step is the engine-internal step: "resolve", "plan", "transfer",
	// "verify", "prune", "restore". The job side maps it to RunRow.Phase.
	Step string `json:"step"`
}

// Counts summarise a finished job.
type Counts struct {
	Added            int64 `json:"added"`
	Changed          int64 `json:"changed"`
	Deleted          int64 `json:"deleted"` // mirror: moved to the recycle folder
	Skipped          int64 `json:"skipped"`
	Errored          int64 `json:"errored"`
	BytesTransferred int64 `json:"bytes_transferred"`
	BytesTotal       int64 `json:"bytes_total"`
	SourceFiles      int64 `json:"source_files"` // becomes the next Baseline
	DestFiles        int64 `json:"dest_files"`
	BytesAdd         int64 `json:"bytes_add,omitempty"` // plan only
}

// Reviewed counts what a reviewed plan showed (JobRequest.GuardReviewed).
type Reviewed struct {
	Deleted int64 `json:"deleted"`
	Updated int64 `json:"updated"`
}

// GuardInfo describes a tripped guard (RunRow.GuardInfo).
type GuardInfo struct {
	Guard  string   `json:"guard"` // "delete" | "change" | "empty_source"
	Pct    float64  `json:"pct"`   // what the run would do, 0..100
	Limit  int      `json:"limit"` // the configured percentage
	Count  int64    `json:"count"`
	Total  int64    `json:"total"`
	Sample []string `json:"sample"` // up to 500 paths
}

// FileError is one file that failed in a partial run.
type FileError struct {
	Path   string    `json:"path"`
	Code   ErrorCode `json:"code"`
	Detail string    `json:"detail"`
}

// Result is how a job ended.
type Result struct {
	Counts Counts `json:"counts"`
	// ErrorCode is "" on success; on a partial success it stays "" and
	// FileErrors lists up to 100 failures.
	ErrorCode   ErrorCode   `json:"error_code"`
	ErrorDetail string      `json:"error_detail"`
	Guard       *GuardInfo  `json:"guard"`
	FileErrors  []FileError `json:"file_errors"`
	// Checks are the prechecks as run (always present for copy/sync/
	// archive/plan, even on success).
	Checks  []Check `json:"checks"`
	MountID int     `json:"mount_id,omitempty"` // the destination mount this run used
	// MarkerWritten reports that this run created the destination marker.
	MarkerWritten bool   `json:"marker_written"`
	ArchiveName   string `json:"archive_name,omitempty"` // archive: final file name
}

// JobStatus is a snapshot of one engine job.
type JobStatus struct {
	ID        JobID      `json:"engine_job_id"`
	RunID     string     `json:"run_id"`
	Op        Op         `json:"op"`
	State     JobState   `json:"state"`
	Stats     Stats      `json:"stats"`
	Result    *Result    `json:"result"` // null until done/error
	StartedAt *time.Time `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
}

// PreviewItem is one line of a plan's PreviewFile.
type PreviewItem struct {
	Op   string `json:"op"` // "add" | "update" | "delete" (PreviewOmittedAdd in the file only)
	Path string `json:"path"`
	Size int64  `json:"size"`
	// Count is only set on a PreviewOmittedAdd line: that many adds
	// (Size bytes in all) are not listed one by one.
	Count int64 `json:"count,omitempty"`
}

// PreviewOmittedAdd is the last line of a real run's plan list whose
// adds were capped; it is counted, never served as an item.
const PreviewOmittedAdd = "omitted_add"

// Log levels.
const (
	LogInfo  = "info"
	LogWarn  = "warn"
	LogError = "error"
)

// LogLine is one line of a run log (spec §10.5). The job side appends it
// as JSONL. MsgKey/Args are for the UI; Raw is the rclone line, if any.
type LogLine struct {
	T      time.Time              `json:"t"`
	Lvl    string                 `json:"lvl"`
	Code   string                 `json:"code"` // "file_copied", "raw", ... (see jobs.LogKeys)
	MsgKey string                 `json:"msg_key,omitempty"`
	Args   map[string]interface{} `json:"args,omitempty"`
	Raw    string                 `json:"raw,omitempty"`
}

// ---------------------------------------------------------------------
// Events

// Event types.
const (
	EventVolumeMounted   = "volume.mounted"
	EventVolumeUnmounted = "volume.unmounted"
	EventJobDone         = "job.done"
)

// Event is one item of the Events stream. Volume is set for volume.*;
// JobID/RunID/State for job.done. Initial reports whether a
// volume.mounted describes a mount that already existed when the watcher
// started (boot, restart) - those must not fire volume_mounted triggers
// (spec §7.2); they go to catch-up.
type Event struct {
	Type    string    `json:"type"`
	Time    time.Time `json:"time"`
	Volume  *Volume   `json:"volume,omitempty"`
	Initial bool      `json:"initial,omitempty"`
	JobID   JobID     `json:"engine_job_id,omitempty"`
	RunID   string    `json:"run_id,omitempty"`
	State   JobState  `json:"state,omitempty"`
}
