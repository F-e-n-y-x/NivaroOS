package jobs

import (
	"strings"
	"time"
)

// Phone backup (mobile plan §5.3-§5.4, §7.2; device API contract in
// docs/specs/2026-09-30-phone-backup-device-api.md). An enrolled phone
// pushes its backups here: every backup is a session, recorded as a run
// (kind "device") of the device's job - the device itself, so the run's
// job_id is the device id - and it ends with a snapshot.
//
// Two kinds of categories:
//
//   - file categories (media, files, apks) keep browsable files under
//     their phone path, with a per-file manifest (path, size, mtime,
//     SHA-256) so unchanged files are never sent again, and older
//     contents kept as versions;
//   - export categories (contacts, calendar, sms, calllog, apps,
//     settings) keep one export file per run (vCard, iCalendar, SMS
//     Backup & Restore XML, JSON). sms and calllog are incremental: each
//     export holds only new items and the server keeps their item keys.
//
// Everything lives under one folder per phone, <destination>/<device
// folder>/ (default /DATA/Backup/<name>/):
//
//	media/<phone path>              photos, videos, audio (MediaStore)
//	files/<phone path>              folders picked with the Storage Access Framework
//	apps/apk/<phone path>           APKs (<package>/<versionCode>/base.apk, splits)
//	contacts/contacts-<ts>.vcf      one vCard file per run
//	calendar/<name>/calendar-<ts>.ics
//	messages/sms-<ts>.xml           SMS Backup & Restore XML, new items only
//	calllog/calls-<ts>.xml
//	apps/apps-<ts>.json             installed apps list
//	settings/settings-<ts>.json     readable device settings
//	.nivaro-versions/<ts>/<category>/<path>   replaced file contents
//	.nivaro-uploads/<upload id>.part          uploads in progress
//	.nivaroos-device.json           identity marker

// PhoneCategory is what a part of a phone backup holds.
type PhoneCategory string

const (
	CatMedia    PhoneCategory = "media"
	CatFiles    PhoneCategory = "files"
	CatAPKs     PhoneCategory = "apks"
	CatContacts PhoneCategory = "contacts"
	CatCalendar PhoneCategory = "calendar"
	CatSMS      PhoneCategory = "sms"
	CatCallLog  PhoneCategory = "calllog"
	CatApps     PhoneCategory = "apps"
	CatSettings PhoneCategory = "settings"
)

// PhoneCategories is every category, in display order.
var PhoneCategories = []PhoneCategory{CatMedia, CatFiles, CatContacts, CatCalendar, CatSMS, CatCallLog, CatApps, CatAPKs, CatSettings}

// Category kinds.
const (
	CatKindFiles  = "files"  // per-file manifest
	CatKindExport = "export" // one export file per run
)

type catInfo struct {
	kind        string
	dir         string // folder under the device root
	prefix, ext string // export file name: <prefix>-<ts><ext>
	incremental bool   // exports hold only new items (item keys)
	named       bool   // exports come in named streams (one per calendar)
	contentType string
}

var catTable = map[PhoneCategory]catInfo{
	CatMedia:    {kind: CatKindFiles, dir: "media"},
	CatFiles:    {kind: CatKindFiles, dir: "files"},
	CatAPKs:     {kind: CatKindFiles, dir: "apps/apk"},
	CatContacts: {kind: CatKindExport, dir: "contacts", prefix: "contacts", ext: ".vcf", contentType: "text/vcard; charset=utf-8"},
	CatCalendar: {kind: CatKindExport, dir: "calendar", prefix: "calendar", ext: ".ics", named: true, contentType: "text/calendar; charset=utf-8"},
	CatSMS:      {kind: CatKindExport, dir: "messages", prefix: "sms", ext: ".xml", incremental: true, contentType: "application/xml"},
	CatCallLog:  {kind: CatKindExport, dir: "calllog", prefix: "calls", ext: ".xml", incremental: true, contentType: "application/xml"},
	CatApps:     {kind: CatKindExport, dir: "apps", prefix: "apps", ext: ".json", contentType: "application/json"},
	CatSettings: {kind: CatKindExport, dir: "settings", prefix: "settings", ext: ".json", contentType: "application/json"},
}

func (c PhoneCategory) valid() bool   { _, ok := catTable[c]; return ok }
func (c PhoneCategory) info() catInfo { return catTable[c] }
func (c PhoneCategory) isFiles() bool { return catTable[c].kind == CatKindFiles }

// Reserved names under a device root.
const (
	phoneVersionsDir = ".nivaro-versions"
	phoneUploadsDir  = ".nivaro-uploads"
	phoneMarkerFile  = ".nivaroos-device.json"
	phoneTimeLayout  = "20060102T150405Z"
)

// Limits (sent to the phone in GET /devices/:id/config).
const (
	PhoneChunkSize        = 8 << 20 // max PATCH body (mobile plan M-12: proxy limits)
	PhoneMaxUploadSize    = 64 << 30
	PhoneCheckBatch       = 500
	PhoneDeletedBatch     = 1000
	PhoneItemKeysBatch    = 1000
	PhoneMaxOpenUploads   = 64
	PhoneMaxOpenSessions  = 8
	PhoneMaxFinishErrors  = 200
	PhoneMaxImportSize    = 1 << 30
	PhoneSessionIdle      = 30 * time.Minute
	PhoneUploadExpiry     = 7 * 24 * time.Hour
	PhoneCheckPerMinute   = 240
	PhoneFreeReservePct   = 5
	phoneMaxPathBytes     = 4096
	phoneMaxSegmentBytes  = 255
	phoneManifestPageMax  = 5000
	phoneManifestPageSize = 1000
	phoneBrowsePageMax    = 2000
)

// Retention and stale defaults for a device.
const (
	DefaultPhoneKeepLast  = 10
	DefaultPhoneKeepDays  = 30
	DefaultPhoneStaleDays = 7
)

// DeviceSettings are the server-side settings of a phone (PUT
// /devices/:id), stored on DeviceRow.Settings.
type DeviceSettings struct {
	// KeepLast snapshots are always kept; KeepDays keeps the newest
	// snapshot of each of the last N days (0: off). File versions and
	// full exports only a pruned snapshot referred to are removed.
	KeepLast int `json:"keep_last"`
	KeepDays int `json:"keep_days"`
	// DeletedPurgeDays removes files deleted on the phone this many days
	// after the phone reported it (0: never, the default).
	DeletedPurgeDays int `json:"deleted_purge_days"`
	// StaleDays: warn when no backup succeeded for this long (0: never).
	StaleDays int `json:"stale_days"`
}

func defaultDeviceSettings() DeviceSettings {
	return DeviceSettings{KeepLast: DefaultPhoneKeepLast, KeepDays: DefaultPhoneKeepDays, StaleDays: DefaultPhoneStaleDays}
}

// PhoneCategorySettings are what the phone reports about one category
// (PUT /devices/:id/phone-settings): shown in the web UI as set by the
// phone, never enforced by the server.
type PhoneCategorySettings struct {
	Enabled bool `json:"enabled"`
	// Schedule is the phone's own description ("daily 03:00", "on new
	// photos", a cron spec) - display only.
	Schedule   string   `json:"schedule"`
	Conditions []string `json:"conditions"` // e.g. "wifi", "charging", "battery_not_low"
	Encrypted  bool     `json:"encrypted"`
}

// PhoneSettings is the body of PUT /devices/:id/phone-settings.
type PhoneSettings struct {
	AppVersion string                                  `json:"app_version"`
	OSVersion  string                                  `json:"os_version"`
	Model      string                                  `json:"model"`
	Categories map[PhoneCategory]PhoneCategorySettings `json:"categories"`
	Paused     bool                                    `json:"paused"`
	ReportedAt *time.Time                              `json:"reported_at"` // set by the server
}

// ---------------------------------------------------------------------
// Tables (gorm AutoMigrate: additive, created on upgrade)

// PhoneSessionRow is one backup session (table phone_session_rows).
type PhoneSessionRow struct {
	ID           string `gorm:"primaryKey"` // "ses_" + 16 hex
	DeviceID     string `gorm:"index"`
	RunID        string
	Categories   string // comma separated
	Reason       string // the phone's trigger: schedule, new_media, manual, app_open...
	Status       string // open | finished | interrupted | cancelled
	StartedAt    time.Time
	LastActivity time.Time
	EndedAt      *time.Time
	Uploaded     int64
	Linked       int64
	Unchanged    int64
	DeletedMark  int64
	Exports      int64
	Bytes        int64
	Errors       int64
	ExpectBytes  int64
	SnapshotID   string
}

// PhoneFileRow is the current manifest entry of one backed-up file
// (table phone_file_rows).
type PhoneFileRow struct {
	DeviceID string `gorm:"primaryKey"`
	Category string `gorm:"primaryKey"`
	Path     string `gorm:"primaryKey"` // as on the phone, relative
	Dir      string `gorm:"index"`      // path.Dir, "" for the top
	Stored   string // relative to the device root
	Size     int64
	MTime    int64  // unix ms, from the phone
	SHA256   string `gorm:"index"`
	MediaID  int64
	TakenAt  int64 // unix ms, 0 unknown
	// FirstSeen is when the path was first backed up; ContentSince when
	// the current content arrived (older contents are versions).
	FirstSeen         time.Time
	ContentSince      time.Time
	DeletedOnDeviceAt *time.Time
	// Damaged is set by verify when the stored file is missing or wrong;
	// check then answers need so the phone sends it again.
	Damaged bool
}

// PhoneVersionRow is an older content of a path (table phone_version_rows),
// valid from Since until Until (exclusive).
type PhoneVersionRow struct {
	ID       int64  `gorm:"primaryKey;autoIncrement"`
	DeviceID string `gorm:"index:idx_phone_ver,priority:1"`
	Category string `gorm:"index:idx_phone_ver,priority:2"`
	Path     string `gorm:"index:idx_phone_ver,priority:3"`
	Dir      string
	Stored   string
	Size     int64
	MTime    int64
	SHA256   string `gorm:"index"`
	MediaID  int64
	TakenAt  int64
	Since    time.Time
	Until    time.Time
	// DeletedOnDeviceAt carried over from the replaced entry.
	DeletedOnDeviceAt *time.Time
}

// PhoneExportRow is one export file of an export category (table
// phone_export_rows).
type PhoneExportRow struct {
	ID        string `gorm:"primaryKey"` // "exp_" + 16 hex
	DeviceID  string `gorm:"index"`
	Category  string
	Name      string // stream name (calendar), "" otherwise
	SessionID string
	TakenAt   time.Time
	Stored    string
	Size      int64
	SHA256    string
	Items     int64
	Encrypted bool
	Imported  bool // POST /devices/:id/exports/import
}

// PhoneSnapshotRow is one finished session (table phone_snapshot_rows):
// what the device's backup looked like at TakenAt.
type PhoneSnapshotRow struct {
	ID         string `gorm:"primaryKey"` // "snap_" + 16 hex
	DeviceID   string `gorm:"index"`
	SessionID  string
	RunID      string
	TakenAt    time.Time
	Categories string
	Status     string // success | partial
	Uploaded   int64
	Linked     int64
	Unchanged  int64
	Deleted    int64
	Exports    int64
	Bytes      int64
	Errors     int64
}

// PhoneUploadRow is a resumable upload (table phone_upload_rows).
type PhoneUploadRow struct {
	ID        string `gorm:"primaryKey"` // "up_" + 24 hex
	DeviceID  string `gorm:"index"`
	SessionID string
	Category  string
	Path      string // file categories: phone path
	Name      string // export categories: stream name
	SHA256    string
	Length    int64
	Offset    int64
	MTime     int64
	TakenAt   int64
	MediaID   int64
	Encrypted bool
	// HashState is the running SHA-256 (encoding.BinaryMarshaler) over
	// the first HashedLen bytes, so finishing needs no re-read.
	HashState []byte
	HashedLen int64
	// DestGen is the device's destination generation at creation; a
	// changed destination voids the upload.
	DestGen   int
	CreatedAt time.Time
	UpdatedAt time.Time
	ExpiresAt time.Time
}

// PhoneItemRow is one message or call item key already backed up
// (table phone_item_rows).
type PhoneItemRow struct {
	DeviceID string `gorm:"primaryKey"`
	Category string `gorm:"primaryKey"`
	Key      string `gorm:"primaryKey"`
}

// PhoneExcludedRow is a content hash the phone must not upload again
// (the owner deleted it from the backup; table phone_excluded_rows).
type PhoneExcludedRow struct {
	DeviceID string `gorm:"primaryKey"`
	SHA256   string `gorm:"primaryKey"`
	Path     string
	AddedAt  time.Time
}

func phoneTables() []interface{} {
	return []interface{}{&PhoneSessionRow{}, &PhoneFileRow{}, &PhoneVersionRow{}, &PhoneExportRow{}, &PhoneSnapshotRow{}, &PhoneUploadRow{}, &PhoneItemRow{}, &PhoneExcludedRow{}}
}

func splitCats(s string) []PhoneCategory {
	var out []PhoneCategory
	for _, c := range strings.Split(s, ",") {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, PhoneCategory(c))
		}
	}
	return out
}

func joinCats(cs []PhoneCategory) string {
	parts := make([]string, len(cs))
	for i, c := range cs {
		parts[i] = string(c)
	}
	return strings.Join(parts, ",")
}

func hasCat(cs []PhoneCategory, c PhoneCategory) bool {
	for _, x := range cs {
		if x == c {
			return true
		}
	}
	return false
}
