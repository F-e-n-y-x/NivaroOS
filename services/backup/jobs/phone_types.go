package jobs

import "time"

// Phone backup API types (docs/specs/backup-api.json, device routes;
// docs/specs/2026-09-30-phone-backup-device-api.md). Additive to the
// frozen WP-0 contract.

// DeviceDestStatus is where a phone's backups go and whether that place
// is usable right now.
type DeviceDestStatus struct {
	// Location is the folder the phone's own folder lives in, as picked
	// with the folder picker; null means the default (/DATA/Backup).
	Location *Endpoint `json:"location"`
	Default  bool      `json:"default"`
	// Path is the phone's folder: <location>/<device folder>. When the
	// drive is missing it is the last path seen.
	Path   string `json:"path"`
	Online bool   `json:"online"`
	// FreeBytes is the free space for backups (null when unknown or
	// offline); ReserveBytes is kept free on top (5 % of the drive).
	FreeBytes    *int64 `json:"free_bytes"`
	ReserveBytes int64  `json:"reserve_bytes"`
	// ErrorCode says why backups can't run there now (dest_offline,
	// path_not_allowed, dest_marker_mismatch...), "" when they can.
	ErrorCode ErrorCode `json:"error_code,omitempty"`
	Detail    string    `json:"detail,omitempty"`
	Quirks    []Quirk   `json:"quirks"`
}

// DeviceCategoryStatus is one category of a phone's backup.
type DeviceCategoryStatus struct {
	Category PhoneCategory `json:"category"`
	Kind     string        `json:"kind"` // files | export
	// LastBackupAt is the newest snapshot that covered the category;
	// LastStatus the outcome of the newest session that did ("" never).
	LastBackupAt *time.Time `json:"last_backup_at"`
	LastStatus   RunStatus  `json:"last_status"`
	LastRunID    string     `json:"last_run_id"`
	Files        int64      `json:"files"`         // files kinds: files present on the phone
	DeletedFiles int64      `json:"deleted_files"` // files kinds: kept, deleted on the phone
	Exports      int64      `json:"exports"`       // export kinds: export files kept
	Items        int64      `json:"items"`         // export kinds: items in the newest export (sms/calllog: all runs)
	SizeBytes    int64      `json:"size_bytes"`    // everything kept, versions included
}

// DeviceMove is a location change in progress or failed.
type DeviceMove struct {
	State      string    `json:"state"` // moving | failed
	Target     *Endpoint `json:"target"`
	TargetPath string    `json:"target_path"`
	Files      int64     `json:"files"`
	TotalFiles int64     `json:"total_files"`
	Bytes      int64     `json:"bytes"`
	TotalBytes int64     `json:"total_bytes"`
	Error      string    `json:"error"`
	StartedAt  time.Time `json:"started_at"`
}

// DeviceDetail is GET /devices/:id.
type DeviceDetail struct {
	Device       Device                 `json:"device"`
	Destination  DeviceDestStatus       `json:"destination"`
	Settings     DeviceSettings         `json:"settings"`
	Phone        *PhoneSettings         `json:"phone"`
	Categories   []DeviceCategoryStatus `json:"categories"`
	OpenSessions []PhoneSession         `json:"open_sessions"`
	Move         *DeviceMove            `json:"move"`
	Snapshots    int64                  `json:"snapshots"`
	Excluded     int64                  `json:"excluded"`
	// LastVerify is the newest check of the stored files (null: never).
	LastVerify *VerifyResult `json:"last_verify"`
}

// DeviceUpdateRequest is PUT /devices/:id; absent fields are unchanged.
type DeviceUpdateRequest struct {
	Name     *string         `json:"name,omitempty"`
	Settings *DeviceSettings `json:"settings,omitempty"`
}

// Move modes of POST /devices/:id/destination.
const (
	DestModeMove  = "move"  // move the existing backups to the new place
	DestModeFresh = "fresh" // leave them where they are; start an empty backup
)

// DeviceDestRequest is POST /devices/:id/destination. Location null
// goes back to the default. Mode is required when the phone already has
// backups.
type DeviceDestRequest struct {
	Location *Endpoint `json:"location"`
	Mode     string    `json:"mode"`
}

// PhoneLimits are the server limits a phone must keep to.
type PhoneLimits struct {
	ChunkSize       int64 `json:"chunk_size"`
	MaxUploadSize   int64 `json:"max_upload_size"`
	CheckBatch      int   `json:"check_batch"`
	DeletedBatch    int   `json:"deleted_batch"`
	ItemKeysBatch   int   `json:"item_keys_batch"`
	MaxOpenUploads  int   `json:"max_open_uploads"`
	MaxOpenSessions int   `json:"max_open_sessions"`
	MaxFinishErrors int   `json:"max_finish_errors"`
	SessionIdleSec  int   `json:"session_idle_sec"`
	UploadExpirySec int   `json:"upload_expiry_sec"`
	CheckPerMinute  int   `json:"check_per_minute"`
}

// PhoneCategoryInfo describes a category to the phone.
type PhoneCategoryInfo struct {
	Category    PhoneCategory `json:"category"`
	Kind        string        `json:"kind"`
	Incremental bool          `json:"incremental"`
	Named       bool          `json:"named"`
	Extension   string        `json:"extension,omitempty"`
}

// DeviceConfig is GET /devices/:id/config.
type DeviceConfig struct {
	Device      Device              `json:"device"`
	Destination DeviceDestStatus    `json:"destination"`
	Settings    DeviceSettings      `json:"settings"`
	Limits      PhoneLimits         `json:"limits"`
	Categories  []PhoneCategoryInfo `json:"categories"`
	Excluded    int64               `json:"excluded"`
	ServerTime  time.Time           `json:"server_time"`
}

// PhoneSessionCounts are what a session did.
type PhoneSessionCounts struct {
	Uploaded  int64 `json:"uploaded"`  // files and exports received
	Linked    int64 `json:"linked"`    // have_elsewhere: placed from a copy already here
	Unchanged int64 `json:"unchanged"` // check answered have
	Deleted   int64 `json:"deleted"`   // marked deleted on the phone
	Exports   int64 `json:"exports"`   // export files stored
	Bytes     int64 `json:"bytes"`     // bytes received
	Errors    int64 `json:"errors"`
}

// SessionStartRequest is POST /devices/:id/sessions.
type SessionStartRequest struct {
	Categories []PhoneCategory `json:"categories"`
	// Reason is the phone's trigger (schedule, new_media, manual,
	// app_open, charging, wifi); display only.
	Reason string `json:"reason"`
	// ExpectBytes is the phone's estimate of what it will send, for the
	// free-space check (0: unknown).
	ExpectBytes int64 `json:"expect_bytes"`
}

// PhoneSession is a backup session.
type PhoneSession struct {
	ID           string             `json:"id"`
	RunID        string             `json:"run_id"`
	Categories   []PhoneCategory    `json:"categories"`
	Reason       string             `json:"reason"`
	Status       string             `json:"status"` // open | finished | interrupted | cancelled
	StartedAt    time.Time          `json:"started_at"`
	LastActivity time.Time          `json:"last_activity"`
	EndedAt      *time.Time         `json:"ended_at"`
	Counts       PhoneSessionCounts `json:"counts"`
	SnapshotID   string             `json:"snapshot_id"`
	// Resumed: POST /sessions found this open session for the same
	// categories and handed it back instead of starting another.
	Resumed bool `json:"resumed"`
}

// Session states.
const (
	SessionOpen        = "open"
	SessionFinished    = "finished"
	SessionInterrupted = "interrupted"
	SessionCancelled   = "cancelled"
)

// CheckItem is one file the phone has.
type CheckItem struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	MTime  int64  `json:"mtime"`  // unix ms
	SHA256 string `json:"sha256"` // lowercase hex; "" = not hashed yet
}

// CheckRequest is POST /devices/:id/sessions/:sid/check.
type CheckRequest struct {
	Category PhoneCategory `json:"category"`
	Items    []CheckItem   `json:"items"`
}

// Check answers.
const (
	CheckHave          = "have"           // stored with this content: nothing to do
	CheckHaveElsewhere = "have_elsewhere" // same content was here under another path: placed without upload
	CheckNeed          = "need"           // upload it
	CheckNeedHash      = "need_hash"      // not known by size+mtime: send again with sha256
	CheckExcluded      = "excluded"       // the owner deleted this content from the backup: never upload
	CheckInvalid       = "invalid"        // path refused (see the contract)
)

// CheckAnswer is the answer for one item, in request order.
type CheckAnswer struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	// UploadID / Offset: an unfinished upload of this very file exists;
	// resume it (HEAD / PATCH) rather than creating another.
	UploadID string `json:"upload_id,omitempty"`
	Offset   int64  `json:"offset,omitempty"`
}

// CheckResult is the answer to a check.
type CheckResult struct {
	Items []CheckAnswer `json:"items"`
}

// DeletedRequest is POST /devices/:id/sessions/:sid/deleted.
type DeletedRequest struct {
	Category PhoneCategory `json:"category"`
	Paths    []string      `json:"paths"`
}

// DeletedResult counts what was marked.
type DeletedResult struct {
	Marked  int `json:"marked"`
	Unknown int `json:"unknown"` // not in the backup (never uploaded) or already marked
}

// ItemKeysRequest is POST /devices/:id/sessions/:sid/check-items.
type ItemKeysRequest struct {
	Category PhoneCategory `json:"category"` // sms | calllog
	Keys     []string      `json:"keys"`
}

// ItemKeysResult lists the keys the server doesn't have yet.
type ItemKeysResult struct {
	New []string `json:"new"`
}

// PhoneErrorReport is one failure the phone reports at finish.
type PhoneErrorReport struct {
	Category PhoneCategory `json:"category"`
	Path     string        `json:"path"`
	Message  string        `json:"message"`
}

// FinishRequest is POST /devices/:id/sessions/:sid/finish.
type FinishRequest struct {
	// Status is success, partial, failed or cancelled.
	Status RunStatus          `json:"status"`
	Errors []PhoneErrorReport `json:"errors"`
}

// FinishResult is its answer.
type FinishResult struct {
	Session        PhoneSession   `json:"session"`
	Snapshot       *PhoneSnapshot `json:"snapshot"`
	PendingUploads int            `json:"pending_uploads"`
}

// UploadCreated answers POST /devices/:id/uploads (also in the
// Location and Upload-Offset headers).
type UploadCreated struct {
	UploadID  string    `json:"upload_id"`
	Location  string    `json:"location"`
	Offset    int64     `json:"offset"`
	Length    int64     `json:"length"`
	ExpiresAt time.Time `json:"expires_at"`
	// Status: created, resumed (an unfinished upload of the same file),
	// or have (already stored: nothing to send, upload_id empty).
	Status string `json:"status"`
}

// PhoneSnapshot is a finished session: the backup as it was then.
type PhoneSnapshot struct {
	ID         string             `json:"id"`
	SessionID  string             `json:"session_id"`
	RunID      string             `json:"run_id"`
	TakenAt    time.Time          `json:"taken_at"`
	Categories []PhoneCategory    `json:"categories"`
	Status     RunStatus          `json:"status"`
	Counts     PhoneSessionCounts `json:"counts"`
}

// PhoneEntry is one file or folder in a snapshot of a file category.
type PhoneEntry struct {
	Name              string     `json:"name"`
	Path              string     `json:"path"`
	Dir               bool       `json:"dir"`
	Size              int64      `json:"size"`
	MTime             int64      `json:"mtime"`
	SHA256            string     `json:"sha256"`
	TakenAt           int64      `json:"taken_at"`
	MediaID           int64      `json:"media_id"`
	DeletedOnDeviceAt *time.Time `json:"deleted_on_device_at"`
	// Version is true when the snapshot holds an older content than the
	// current one.
	Version bool `json:"version"`
}

// PhoneBrowseResult is GET /devices/:id/snapshots/:sid/browse.
type PhoneBrowseResult struct {
	Snapshot string        `json:"snapshot"`
	TakenAt  time.Time     `json:"taken_at"`
	Category PhoneCategory `json:"category"`
	Path     string        `json:"path"`
	Entries  []PhoneEntry  `json:"entries"`
	// Truncated: more than 2000 entries; narrow with a sub-folder.
	Truncated bool `json:"truncated"`
}

// PhoneExport is one export file.
type PhoneExport struct {
	ID        string        `json:"id"`
	Category  PhoneCategory `json:"category"`
	Name      string        `json:"name"`
	TakenAt   time.Time     `json:"taken_at"`
	Size      int64         `json:"size"`
	SHA256    string        `json:"sha256"`
	Items     int64         `json:"items"`
	Encrypted bool          `json:"encrypted"`
	Imported  bool          `json:"imported"`
}

// RestoreItem is one file to pull back to the phone.
type RestoreItem struct {
	Category          PhoneCategory `json:"category"`
	Path              string        `json:"path"`
	Size              int64         `json:"size"`
	MTime             int64         `json:"mtime"`
	SHA256            string        `json:"sha256"`
	TakenAt           int64         `json:"taken_at"`
	MediaID           int64         `json:"media_id"`
	DeletedOnDeviceAt *time.Time    `json:"deleted_on_device_at"`
	// Content is the GET path (under /v1/backup) that returns the bytes.
	Content string `json:"content"`
}

// RestoreManifest is GET /devices/:id/restore-manifest, one page.
type RestoreManifest struct {
	Snapshot string        `json:"snapshot"`
	TakenAt  time.Time     `json:"taken_at"`
	Items    []RestoreItem `json:"items"`
	// Exports are the export files in effect at the snapshot (first page
	// only), with their content paths in ExportContent.
	Exports []PhoneExport `json:"exports"`
	// NextAfter continues the listing (?after=), "" at the end.
	NextAfter string `json:"next_after"`
}

// PhoneExcluded is one excluded content hash.
type PhoneExcluded struct {
	SHA256  string    `json:"sha256"`
	Path    string    `json:"path"`
	AddedAt time.Time `json:"added_at"`
}

// ExcludedUpdate is POST /devices/:id/excluded.
type ExcludedUpdate struct {
	Add    []PhoneExcluded `json:"add"`
	Remove []string        `json:"remove"`
}

// DeviceDownloadRequest is POST /devices/:id/downloads: one file of a
// snapshot (category + path), one export (export_id), or the complete
// sms/calllog file (full + category).
type DeviceDownloadRequest struct {
	Category PhoneCategory `json:"category"`
	Path     string        `json:"path"`
	Snapshot string        `json:"snapshot"`
	ExportID string        `json:"export_id"`
	Full     bool          `json:"full"`
}

// VerifyRequest is POST /devices/:id/verify. Deep re-reads every file
// and compares its SHA-256; otherwise only presence and size.
type VerifyRequest struct {
	Deep bool `json:"deep"`
}
