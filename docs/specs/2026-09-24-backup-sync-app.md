# Backup & Sync app (v1)

Status: spec, not yet built (2026-09-24)
Owner ask: "make an app so users can add scheduled backups, sync and other types
of backup periodically to any type of storage in Nivaro, and condition based
also ... better UI and UX ... not only work on this PC but all ... quality work."

Inputs: the scheduler audit, the storage/events audit, industry research, design
A (engine-first), design B (UX-first) and an adversarial critique. This file
merges them into one buildable spec. Every critique point is closed in
§19, which says how.

Side answer, for the record: the **Syncthing** and **Smart Home** blocks
(`ui/src/apps/syncthing/SyncBlock.vue`, `ui/src/apps/smart-home/SmartBlock.vue`)
have nothing to do with scheduled tasks. They are leftover CasaOS promo cards.
Their only host is `ui/src/shell/CoreService.vue`, which nothing imports. They
are deleted in WP-UI-SHELL (§17).

---

## 1. Goals and non-goals

### Goals
1. One desktop app, **Backup & Sync**, where a non-expert can protect any folder
   and send it to **any NivaroOS storage**: internal drive, USB drive, merged pool,
   SMB share, or any cloud account (Google Drive, OneDrive, Dropbox, S3, B2,
   WebDAV, SFTP, SMB, iCloud, TeraBox).
2. Jobs run **on a schedule** and **on conditions**: drive plugged in or mounted,
   a missed run caught up after boot, "only if the destination is available", and
   "only inside a time window". Retries back off.
3. **No silent data loss**, ever. The engine checks the mount by identity, refuses
   a destination that sits inside the source, stops on delete, change or empty
   source guards, keeps a versions (recycle) folder, and cancels when a drive is
   unplugged mid-run.
4. It **works the same on every install** that `installer/install.sh` supports.
   v1 adds no new binaries or packages. Existing tasks migrate automatically and
   nothing is set up by hand on one box.
5. The UI follows the house rules. Every dialog is a real desktop window. It
   meets WCAG 2.2 AA in dark and light themes, works fully from the keyboard,
   runs on phone and tablet layouts, and uses `$t` with every key present in
   `en_US.json`.
6. The rebuild also fixes the existing scheduler's worst bugs: `/usr/bin/rclone`
   is missing on fresh installs, `schedules.json` writes are not atomic, the
   archive command can be injected, and custom cron gets overwritten.

### Non-goals (v1)
- Versioned, deduplicated, encrypted backup with restic. That is v1.1 (§18).
  The data model and UI already reserve room for it.
- Two-way sync (bisync), the on-file-change trigger, database dumps, VM live
  snapshots, a peer NivaroOS target, and the crypt mirror. See "Later" (§18).
- Per-job bandwidth limits. rclone's `accounting.TokenBucket` is global, so a
  per-job limit would also throttle every cloud mount. Cut.
- Pause and resume inside a run. The rclone library has no pause. Cancel, then
  run again: the re-run skips files that already match.
- "On AC/UPS" and "CPU idle" conditions. Nice and ionice priority covers them.
- Alpine and OpenRC. The installer is systemd-only (`install.sh:485` only warns
  there).
- NFS client mounts. No `mount.nfs` is installed. Use SFTP or SMB cloud accounts.
- Continuous device-to-device sync. That stays the optional Syncthing app in the
  App Store.

---

## 2. v1 scope at a glance

| Area | v1 |
|---|---|
| Job types | **Copy new files**, **Mirror** (with a recycle folder), **Archive** (`.tar.zst`, pure Go) |
| Sources | any local folder on a resolved endpoint; app data (`/DATA/AppData/<app>`); VM folders (`/DATA/VMs/<vm>`); exported Samba shares (as presets) |
| Destinations | internal/fstab volume, USB (by identity), mergerfs pool, SMB share (through the in-process rclone `smb` backend), cloud remote |
| Triggers | schedule (cron via builder), volume mounted (USB/any, by identity), boot catch-up, manual |
| Conditions | destination available (forced for Mirror), time window; on unmet: skip / wait / fail |
| Safety | mountinfo resolver + identity marker, dest-inside-source, allowed roots, free space, fstype quirks, empty-source sentinel 50 %, delete guard 10 %, change guard 30 %, cancel-on-unmount |
| Hooks | stop/start apps (all at once or one at a time), shut down/start VM, post-hook replay after crash |
| Restore | Mirror: browse `.nivaro-versions/<ts>` and the current destination; Archive: list archives and extract selected paths; Copy: browse destination; restore to original or other folder, keep-both default |
| History | runs table, JSONL log per run, retention, live progress over message bus, cancel |
| Notify | failure, waiting_user, stale backup, offline past retry window → NotificationCenter with `act.window` deep link |
| Migration | `schedules.json` backup/sync tasks → jobs (idempotent); old executor refuses backup/sync; atomic writes |
| Installer | remove `rclone.service`; create `/var/lib/nivaroos/backup`; memory cap for local-storage; fix Go toolchain pin; update deferral |
| UI | `BackupApp` (Overview, Jobs, Restore, Activity, Settings), 5-step wizard, storage + folder pickers, run window, preview/guard window, restore window; shared `ScheduleBuilder` also used by Scheduled Tasks |

---

## 3. Architecture

```
 UI (Vue 2) ──HTTP /v1/backup/*──▶ gateway ──▶ nivaroos (core)
    ▲                                         service/backup/  (jobs, triggers, queue,
    │ socket.io (message bus)                 conditions, hooks, runs, notify, migration)
    │                                             │  unix socket, HTTP/1.1 + NDJSON
    └──── nivaroos:backup:* events ◀── core       ▼
                                           nivaroos-local-storage
                                             service/engine/ (resolve, precheck, copy/sync/
                                             archive/check/list/purge, mountwatch, stats)
                                             rclone v1.75.1 as a library (already linked)
```

### 3.1 Ownership

| Concern | Owner | Package |
|---|---|---|
| Job definitions, validation, triggers, conditions, queue and locks, hooks, run records, logs, notifications, REST API, migration | **core** | `services/core/service/backup/`, routes `services/core/route/v1/backup.go` |
| Endpoint listing and resolution, mountinfo, identity marker, prechecks, data movement, restore copy, archive, version listing, purge, mount events, live stats | **local-storage** | `services/local-storage/service/engine/`, server `services/local-storage/route/engine.go` |
| Cron clock | **one** robfig `cron.Cron` in core, shared by Schedules and Backup | `services/core/service/scheduler_clock.go` (new; `schedule.go` switches to it) |

Why the engine lives in local-storage:
- local-storage already links rclone v1.75.1 with `backend/all` plus the custom
  TeraBox backend, so no CLI binary is needed on any install.
- It is the only writer of `/root/.config/rclone/rclone.conf`, so OAuth refresh
  tokens can't race. OneDrive and Dropbox rotate them.
- Rejected: a separate sidecar or a per-run worker process. Both create a second
  writer of `rclone.conf`.

### 3.2 Engine transport
- Unix socket: `/run/nivaroos-engine/engine.sock`. local-storage creates the
  directory with mode 0700 and the socket with 0600, both root-owned, and serves
  a plain `net/http` server started from local-storage `main.go`.
- The socket is **never** on the gateway. This keeps the `/v1` gin group free of
  any loopback JWT exemption.
- Core client: `services/core/service/backup/engineclient.go`, an
  `http.Transport{DialContext: unix}` client with 5 s connect and per-call
  timeouts.
- Contract version: `GET /health` returns
  `{"api":1,"rclone":"v1.75.1","pid":1234,"started_at":"…"}`. Core refuses to
  start runs when `api` differs from `engineAPIVersion` and reports
  `engine_mismatch`. Both are built from one source tree, so the two versions
  only differ in the middle of an update.
- Event stream: `GET /events` sends NDJSON over a long-lived response carrying
  `volume.mounted`, `volume.unmounted` and `job.done`. Core reconnects with
  exponential backoff from 1 s to 60 s, and runs a 60 s reconcile
  (`GET /volumes`, diffed against the last snapshot) so no event is lost while
  it is disconnected.
  - This replaces design A's proposed Go message-bus WebSocket subscriber. The
    stream is simpler, needs no new client in `common`, and stays inside the root
    trust boundary.
  - local-storage **also** publishes `local-storage:volume:mounted|unmounted` on
    the message bus so the UI and other services can see them. The event types
    are registered in `local-storage/common/message.go`.

### 3.3 Engines and versions

| Engine | Version | Source | How it gets onto every install | Runtime check |
|---|---|---|---|---|
| rclone library (copy, sync, check, list, purge, rcat, smb, local, all cloud backends) | **v1.75.1**, pinned by `services/local-storage/go.mod` | in-process | built by `install.sh` with local-storage, as today | `GET /health` → `rclone` field |
| archive writer/reader | Go `archive/tar` + `github.com/klauspost/compress/zstd` (already an indirect dependency of rclone; promote to direct in local-storage go.mod) | in-process | same | same |
| restic (**v1.1**) | pinned `0.18.1` (bump when implementing, record in `installer/versions.env`) | built from source: `GOBIN=/usr/lib/nivaroos/bin go install github.com/restic/restic/cmd/restic@v0.18.1`, verified by the Go checksum DB | new `install_restic()` step, non-fatal | see §3.4 |

**Toolchain fact (verified).** `services/local-storage/go.mod` says `go 1.26.0`
while `install.sh:46` pins `GO_VERSION="1.23.4"`. Today the build works only
because the default `GOTOOLCHAIN=auto` downloads a newer toolchain at build time.
- WP-INST sets `GO_VERSION` to the highest `go` directive among `services/*`, or
  exports `GOTOOLCHAIN=go1.26.0+auto` explicitly.
- It fails the install loudly if the local-storage build fails. An offline box
  must not end up without an engine.

### 3.4 Capability check and on-demand install (vm-sidecar pattern)
This mirrors `services/vm-sidecar/setup.go`: `GET /setup/status` and an explicit
`POST /setup/install`. Nothing is installed without a user action. Unlike
vm-sidecar, the check never uses `dpkg-query` (Debian only). It probes the
binary itself:
- `GET /v1/backup/capabilities` →
  ```json
  {"engine":{"available":true,"api":1,"rclone":"v1.75.1"},
   "restic":{"available":false,"version":"","path":"/usr/lib/nivaroos/bin/restic","min":"0.14.0","installable":true},
   "timezone":"Europe/Berlin","utc_offset":"+02:00","clock_synced":true,
   "ram_bytes":8254390272,"max_concurrent":2}
  ```
- `POST /v1/backup/engines/restic/install` (v1.1) runs the same `go install` as
  the installer, in the background, and returns `{run_id}`. It is logged like a
  run. If Go is missing it answers `409 {"error_code":"toolchain_missing"}`, and
  the UI says "Update NivaroOS to add this engine."
- In v1 only the `engine` block matters. If `available=false` (local-storage is
  down or still starting), jobs wait (`engine_unavailable`, a transient error)
  and are never failed.

### 3.5 Resource limits (engine must not take cloud mounts down)
- local-storage calls `debug.SetMemoryLimit`: 25 % of RAM, clamped between
  512 MiB and 2 GiB. Its unit gets `MemoryHigh=` at 40 % of RAM through a drop-in
  `nivaroos-local-storage.service.d/10-memory.conf` written by the installer.
  This is a cgroup setting, so it needs no mount namespace and doesn't hit the
  pitfall recorded in the unit comment.
- Per run: `Transfers=4`, `Checkers=8`, `BufferSize=16M`, `UseListR=false` (no
  `--fast-list`), `LowLevelRetries=10`, `Retries=1`. Core owns higher-level
  retries.
- Global engine semaphore: at most **1 cloud-side run** and at most
  **2 runs in total**.
- Every engine goroutine that we start runs under `recover()` and logs, but
  `recover()` does not catch panics in rclone's own goroutines. The real
  protection is the memory limit, the caps above and a 1 M-file stress test
  (§16.4). If local-storage dies, core marks the run `interrupted` and requeues
  it (§8).

### 3.6 Where data never goes
- The engine never reads or writes through a FUSE cloud mount. The resolver maps
  any `/mnt/<type>_<name>/…` path to the cloud remote whose `mount_point` equals
  it. It never passes a `/mnt/*` fuse path to rclone. This avoids pulling a whole
  cloud through `CacheMode=full` into the root disk (`storage.go:80`).
- The engine never uses a shell. Archive, hooks and options are all typed, and
  there are no free-form extra args.

---

## 4. Data model

Store: **`/var/lib/nivaroos/backup/backup.db`**, glebarez pure-Go sqlite (already
a core dependency), opened with `?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)`.
- It is a separate database from `casaOS.db`, whose `SetMaxOpenConns(1)`, lack
  of WAL, and `AutoMigrate` errors that are only printed stay as they are.
- `AutoMigrate` errors are **fatal** for the backup service. The app then
  reports `store_unavailable`, and the rest of core keeps running.
- Only state transitions are written to the database. Live progress stays in
  memory.
- JSON columns are `string` fields with typed accessors, so no
  `gorm.io/datatypes` dependency is added.

```go
// services/core/service/backup/model.go
package backup

type JobType string // "copy" | "mirror" | "archive"   (v1.1: "backup"; later: "twoway")

type EndpointKind string
const (
    EPVolume  EndpointKind = "volume"  // internal/fstab/root-fs volume: RefID = fs UUID
    EPUSB     EndpointKind = "usb"     // removable: RefID = fs UUID; Match = serial+size
    EPMerge   EndpointKind = "merge"   // mergerfs pool: RefID = pool mount point, Members = UUIDs
    EPSMB     EndpointKind = "smb"     // RefID = core ConnectionsDBModel ID (engine uses rclone smb backend)
    EPCloud   EndpointKind = "cloud"   // RefID = rclone remote name (section in rclone.conf)
)

type Endpoint struct {
    Kind    EndpointKind `json:"kind"`
    RefID   string       `json:"ref_id"`
    Match   *DevMatch    `json:"match,omitempty"`   // usb only
    SubPath string       `json:"sub_path"`          // relative, cleaned, no "..", no NUL, no leading "/"
    Label   string       `json:"label"`             // display only, never used to resolve
    Preset  string       `json:"preset,omitempty"`  // "appdata:<app>" | "vm:<name>" | "share:<id>" (drives hook defaults/UI)
}
type DevMatch struct {
    Serial   string `json:"serial,omitempty"`
    SizeByte int64  `json:"size_bytes,omitempty"` // partition size; FAT/exFAT UUIDs are 8 hex and clonable
}

type Trigger struct {
    Kind       string `json:"kind"`                 // "schedule" | "volume_mounted" | "manual"
    Cron       string `json:"cron,omitempty"`       // robfig 5-field or descriptor, validated server-side
    VolumeRef  *Endpoint `json:"volume,omitempty"`  // volume_mounted: default = job destination
    MinGapHours int   `json:"min_gap_hours,omitempty"` // volume_mounted: at most once per N h (default 0 = once per attach)
    CatchUp     bool  `json:"catch_up"`               // default AppSettings.CatchUpDefault (true)
}

type Conditions struct {
    DestAvailable bool   `json:"dest_available"`       // forced true for mirror
    Window        *TimeWindow `json:"window,omitempty"`// server local time, may wrap midnight
    WhenUnmet     string `json:"when_unmet"`           // "skip" | "wait" | "fail" (default "wait" for schedule, "skip" for others)
    WaitMaxMin    int    `json:"wait_max_min,omitempty"` // default 360
}
type TimeWindow struct{ Start, End string } // "22:00", "07:00"

type Filters struct {
    ExcludePresets []string `json:"exclude_presets"` // "caches","trash","temp","thumbs","node_modules"
    Exclude        []string `json:"exclude"`          // rclone filter globs, validated by filter.NewFilter at save
    Include        []string `json:"include,omitempty"`
    MaxSizeBytes   int64    `json:"max_size_bytes,omitempty"`
}

type Options struct {
    Verify         bool `json:"verify"`            // post-run one-way check; disabled on dests without hashes (TeraBox)
    PreviewFirst   bool `json:"preview_first"`     // dry-run before the FIRST real run (default true for mirror new jobs)
    LowPriority    bool `json:"low_priority"`      // default true; best-effort, see note below the structs
    MaxDurationSec int  `json:"max_duration_sec"`  // default 86400
    PreserveMeta   *bool `json:"preserve_meta,omitempty"` // default: true for local-ish dests (rclone Metadata + links)
    CopyEmptyDirs  bool `json:"copy_empty_dirs"`
}

type Guards struct {
    EmptySourcePct int `json:"empty_source_pct"` // default 50: source file count dropped by more than this vs last success -> waiting_user
    DeletePct      int `json:"delete_pct"`       // default 10 (mirror)
    ChangePct      int `json:"change_pct"`       // default 30: modified+deleted vs dest count (mirror, copy)
    AllowEmptySrc  bool `json:"allow_empty_source"`
}

type Retention struct {
    VersionsDays int `json:"versions_days,omitempty"` // mirror: default 30 (0 = keep forever)
    KeepLast     int `json:"keep_last,omitempty"`     // archive: default 8
    // v1.1 restic: Mode "smart"|"last_n"|"custom", Hourly..Yearly
}

type Hook struct {
    Phase      string   `json:"phase"`       // "pre" | "post"   (post runs if the matching pre ran)
    Action     string   `json:"action"`      // "stop_apps" | "start_apps" | "shutdown_vm" | "start_vm"
    Apps       []string `json:"apps,omitempty"`
    AppMode    string   `json:"app_mode,omitempty"` // "together" | "one_at_a_time"
    VM         string   `json:"vm,omitempty"`
    TimeoutSec int      `json:"timeout_sec"` // default 300 (vm: 600)
    FailPolicy string   `json:"fail_policy"` // "abort" (default) | "continue"
}

type Retry struct {
    Max        int   `json:"max"`         // default 3
    BackoffSec []int `json:"backoff_sec"` // default [60, 600, 3600]
}

type NotifyPrefs struct {
    OnSuccess       bool `json:"on_success"`         // default false
    OnFailure       bool `json:"on_failure"`         // default true (UI shows locked on)
    StaleAfterHours int  `json:"stale_after_hours"`  // default max(48, 2 × schedule interval)
}

// Persisted (gorm) ------------------------------------------------------
type JobRow struct {
    ID           string `gorm:"primaryKey"`          // "bk_" + 12 hex
    Name         string `gorm:"not null"`
    Type         string `gorm:"not null"`
    Enabled      bool
    Sources      string // JSON []Endpoint (copy/mirror: exactly 1; archive: 1..16)
    Dest         string // JSON Endpoint
    Triggers     string // JSON []Trigger
    Conditions   string
    Filters      string
    Options      string
    Guards       string
    Retention    string
    Hooks        string // JSON []Hook
    RetryPolicy  string
    Notify       string
    DestFolderID string // uuid written in the dest marker (§6.2)
    NeedsAttention string // "" | "migrated_unresolved" | "dest_changed" | ...
    MigratedFrom *string `gorm:"uniqueIndex"`     // schedule task id; UNIQUE -> migration idempotent
    Revision     int     `gorm:"not null;default:1"` // optimistic concurrency (PUT must send it; 409 otherwise)
    CreatedAt, UpdatedAt time.Time
}

type RunRow struct {
    ID          string `gorm:"primaryKey"` // "run_" + ULID
    JobID       string `gorm:"index"`
    Kind        string // "backup" | "preview" | "restore" | "verify" | "prune"
    Trigger     string // "schedule" | "volume_mounted" | "catch_up" | "manual" | "retry"
    Status      string `gorm:"index"` // see §5
    Phase       string // "precheck" | "pre_hooks" | "transfer" | "verify" | "prune" | "post_hooks"
    Attempt     int
    ErrorCode   string
    Summary     string // human sentence, i18n key + args JSON ("backup.run.summary.ok|{...}")
    QueuedAt    time.Time
    StartedAt, EndedAt *time.Time
    FilesAdded, FilesChanged, FilesDeleted, FilesSkipped, FilesErrored int64
    BytesTransferred, BytesTotal int64
    EngineJobID int64
    HooksDone   string // JSON: [{hook_idx, pre_done, prior_state:{app:"running"}}] -> replay source
    GuardInfo   string // JSON for waiting_user: {guard:"delete", pct:42, sample:[...]} 
    LogPath     string // /var/lib/nivaroos/backup/logs/<job>/<run>.jsonl(.gz)
    Coalesced   int
    RestoreSpec string // JSON for kind=restore
}

type MetaRow struct { Key string `gorm:"primaryKey"`; Value string }
// keys: "schema_version", "migration.schedules_v1" = JSON report, "settings" = JSON AppSettings

type AppSettings struct {
    MaxConcurrent     int  `json:"max_concurrent"`      // 1..4, default 2 (engine still caps cloud at 1)
    CatchUpDefault    bool `json:"catch_up_default"`    // default true
    LogRetentionDays  int  `json:"log_retention_days"`  // default 180
    DefaultDeletePct  int  `json:"default_delete_pct"`  // 10
    DefaultChangePct  int  `json:"default_change_pct"`  // 30
    DefaultVersionsDays int `json:"default_versions_days"` // 30
}
```

`Options.LowPriority` works like this. The engine runs the job's transfer in a
goroutine locked to an OS thread (`runtime.LockOSThread`) and calls
`unix.Setpriority(PRIO_PROCESS, tid, 10)` plus `ioprio_set(IOPRIO_CLASS_IDLE)`
on that thread. Worker goroutines inherit nothing, so we also cap `Transfers=2`
when LowPriority is set. This is a best-effort limit and is documented as such.

### 4.1 Filesystem layout (created by the installer and again by core at start, idempotently)
```
/var/lib/nivaroos/backup/            0700 root
  backup.db  backup.db-wal  backup.db-shm
  logs/<job_id>/<run_id>.jsonl[.gz]
  staging/                            (archive restore temp; allowed root)
  secrets/                            0700 (v1.1 restic keys, <job_id>.key 0600)
/run/nivaroos-engine/engine.sock      0600
```

### 4.2 Migration from Scheduled Tasks (`backup/migrate.go`, at core start, idempotent)
1. Wait for the engine to be reachable, retrying for up to 10 min. Migration
   needs the resolver, and local-storage starts **after** core
   (`After=nivaroos.service`). Until then, do nothing. Never fail.
2. Read `schedules.json`. Select `type in {backup, sync}`, plus legacy
   `action_type` values that map to them.
3. For each selected task, skip it if a `JobRow` with `MigratedFrom=task.ID`
   exists (UNIQUE index). Otherwise:
   - **Resolve the paths.**
     - `remote:path` becomes `EPCloud`.
     - `/mnt/<type>_<name>/…` becomes `EPCloud` through the mount_point lookup.
     - `/mnt/<host>/<share>/…` becomes `EPSMB` through the connections DB.
     - Any other local path becomes the mount it sits on (engine `/resolve-path`
       → UUID + sub_path).
   - **Map the action.**

     | old action | new job |
     |---|---|
     | copy / rclone_copy | copy |
     | sync / rclone_sync | mirror (versions 30 days, guards on) |
     | rsync / rsync_backup | mirror (versions 30 days, guards on) |
     | move / rclone_move | copy, with the note "was Move, now Copy so originals are never deleted" |
     | archive / tar_archive | archive (keep last 8) |
   - **Extra args.** Map known flags onto typed fields (`--exclude X` →
     `Filters.Exclude`). Drop unknown ones and list them in the report.
   - **Cron** is kept byte-for-byte as a `schedule` trigger.
   - **Destination inside source** (e.g. `/DATA` → `/DATA/Backup`): add an
     automatic exclude of the destination sub-path and note it in the report.
   - **Unresolvable** (drive absent, remote gone): import with
     `Enabled=false, NeedsAttention="migrated_unresolved"`.
   - Migrated jobs **keep running** on their old cron. There is no forced dry
     run. The guards (§6.4) are the protection: a run that would delete or change
     too much goes to `waiting_user`.
4. Commit all inserts in one transaction.
5. Then rewrite `schedules.json` **without** the migrated entries, using the new
   atomic writer (temp file → `fsync` → `rename` → `fsync` of the directory).
   Before that, copy the original once to `schedules.json.pre-backup-migration`.
   If core crashes between steps 4 and 5, the UNIQUE index makes the next start
   skip the duplicates and only redo step 5.
6. Store the report in `MetaRow["migration.schedules_v1"]` and send one
   notification: "2 backup tasks moved to Backup & Sync, with safety checks
   added." The notification has an `act.window` link to Settings → Import.
7. **Same release, in `schedule.go`:**
   - `executeTask` refuses types `backup` and `sync` with the output "This task
     moved to Backup & Sync" and status failed. It never runs `/usr/bin/rclone`
     again.
   - `load()` renames a file it can't parse to `schedules.json.corrupt-<ts>`
     and refuses to save until an operator acts, instead of starting empty.
   - All saves go through the atomic writer.
   - `GetTargets` stops calling `rclone config dump`. The Schedules editor no
     longer offers backup/sync.

---

## 5. Run lifecycle (state machine)

```
            ┌──────────── cancel ─────────────────────────────────┐
            ▼                                                     │
 [queued] ──▶ [running:precheck] ──▶ [running:pre_hooks] ──▶ [running:transfer]
    │  ▲            │ unmet/offline          │ hook failed(abort)     │
    │  │            ▼                        ▼                        ├─▶ guard tripped ─▶ [waiting_user]
    │  │  skip ─▶ [skipped]           [running:post_hooks]            │                     │ proceed → running:transfer
    │  │  wait ─▶ (re-check 5m, until WaitMaxMin) ─▶ [skipped|failed] │                     │ decline/timeout 72h → cancelled
    │  │  fail ─▶ [failed]                                            ▼
    │  └── retry (transient, attempt<Max, backoff) ◀── [running:verify] ─▶ [running:prune] ─▶ [running:post_hooks]
    │                                                                                          │
    └─ coalesce (job already queued/running: Coalesced++)                                      ▼
                                                            [success] | [partial] | [failed] | [cancelled]
 at core start: running:* → [interrupted] → post-hook replay → requeue (trigger="retry") if Retry.Max>0
```

- **Final states:** `success`, `partial` (finished with file errors, listed),
  `failed`, `cancelled`, `skipped`, `interrupted`. `waiting_user` and `queued`
  are resting states, and `running` has a `phase`. There is no `paused`.
- Every transition is one database update plus one message-bus event (§10).
- `post_hooks` always runs when any pre hook ran, even after failure or cancel.
- `waiting_user` releases the worker slot. Resuming requeues the run at the head
  of the queue with the guard override for this run only. After 72 h with no
  answer the run becomes `cancelled`, with a notification.
- **Error classes** decide retries. The enum lives in `backup/errors.go`, and a
  generated `ui/src/apps/backup/errorCodes.js` has the same values, checked by a
  test.

| class | codes | retry? |
|---|---|---|
| transient | `engine_unavailable`, `network_unreachable`, `cloud_rate_limited`, `dest_offline` (only when `when_unmet=wait`), `io_error` | yes, backoff |
| deferred | `cloud_quota_daily` (Google Drive 750 GB/day, `userRateLimitExceeded`) | no retry; requeue once at the next local midnight +10 min, status `skipped` with that summary |
| guard | `delete_guard`, `change_guard`, `empty_source`, `dest_marker_mismatch` | no, → waiting_user or failed |
| config | `dest_inside_source`, `path_not_allowed`, `fat32_file_too_large`, `case_collision`, `endpoint_unknown`, `cloud_auth`, `no_space` | no, fail + notify |
| hook | `app_stop_failed`, `vm_shutdown_timeout`, `app_start_failed` | no (post-hook failures → partial + notify) |
| lifecycle | `interrupted`, `cancelled_by_user`, `cancelled_unmounted`, `max_duration` | interrupted → requeue once; others no |

---

## 6. Endpoints, resolution and prechecks (engine)

### 6.1 Listing: `GET engine:/endpoints` (proxied as `/v1/backup/locations`)
Built from:
- `LSBLK(false)`, `ListFstabMounts`, `GetUSBDriveStatusList`, `GetMerges`
- the rclone config sections, with `About()` fetched in the background (5 s
  timeout, cached for 60 s)
- core's SMB connections and exported shares, merged in by core
- the app list (app-management) and the VM list (vm-sidecar `GET /vms` when
  installed; nothing is parsed from `virsh` text)

There are no hard-coded `/DATA/*` folders. They appear as **folder presets**
inside the root-filesystem volume. Remembered USB drives (those that appear in
any job) are listed with `online:false, last_seen`.

```json
[{"kind":"volume","ref_id":"6c1e…","label":"tower","fstype":"ntfs3","mount_point":"/DATA/tower",
  "online":true,"free":1980000000000,"total":3960000000000,"removable":false,"system_disk":false,
  "physical_disk":"sdb","exported_share":false,"quirks":["case_insensitive","ntfs_chars"],"warnings":[]},
 {"kind":"usb","ref_id":"3A4F-1C22","match":{"serial":"4C53…","size_bytes":128035676160},"label":"Sandisk 128G",
  "fstype":"exfat","online":false,"last_seen":"2026-09-21T18:02:11+02:00","quirks":["mtime_2s","case_insensitive","ntfs_chars"]},
 {"kind":"cloud","ref_id":"terabox_terabox_1789494942","label":"TeraBox","provider":"terabox","online":true,
  "free":null,"quirks":["no_modtime","no_hash"],"warnings":["limited_change_detection"]},
 {"kind":"smb","ref_id":"3","label":"\\\\192.168.1.20\\backup","online":true,"free":66500000000}]
```

### 6.2 Resolution, done inside the engine right before any write
There is no check-then-act across processes. Core sends endpoints, and the
engine resolves them at the moment of use.
- **volume / usb:**
  - Parse `/proc/self/mountinfo`.
  - Map each mount's source device to its fs UUID (`/dev/disk/by-uuid` symlinks,
    falling back to LSBLK).
  - Pick the mount whose UUID equals `RefID`. For usb, the serial and size must
    also match when they are stored. Two candidates means the result is
    `ambiguous_device` (a cloned drive), a config error.
  - The resolved root is that mount's mount point, and the run records its
    **mount ID**.
  - For root-filesystem volumes the root is `/`. Allowed roots still apply, so
    only `/DATA/...` sub-paths can be used.
  - This handles bind mounts, btrfs subvolumes, mergerfs and fuse correctly,
    where design A's `st_dev != parent` test did not.
- **merge:** the pool mount must exist in mountinfo with fstype
  `fuse.mergerfs`. Free space is checked per branch, and the UI warns that free
  space is per branch.
- **smb:** core sends host, share, user and password (from its connections DB)
  over the socket for this run only. The engine builds
  `:smb,host=…,user=…,pass=<obscured>:share/sub` in memory, so the credentials
  never touch `rclone.conf`. The in-process `smb` backend is cancellable, so
  there are no hung kernel CIFS threads. The kernel CIFS mount stays for Files
  only. WP-API also adds `soft,echo_interval=15,actimeo=1` to
  `connections.go`'s mount options so Files can't hang for minutes either.
- **cloud:** the remote must be in `config.FileSections()`. The engine uses
  `fs.NewFs(remote+":"+sub)` directly. Auth errors map to `cloud_auth`.
- **Sub-path rules:** `filepath.Clean`, then reject `..`, a leading `/` and NUL.
  After joining, run `EvalSymlinks` and require the result to stay under the
  endpoint root **and** under an allowed root (`/DATA`, `/mnt`, `/media`,
  `/var/lib/nivaroos/backup/staging`). Denied everywhere: `/proc`, `/sys`,
  `/dev`, `/boot`, `/etc`, `/run`, and `/var/lib/nivaroos` except staging.
  Core enforces the same rules again as defence in depth.

**Destination identity marker.** On the first successful run the engine writes
`<dest>/.nivaroos-backup.json`:
```json
{"v":1,"job_id":"bk_…","dest_folder_id":"<uuid4>","fs_uuid":"6c1e…","created":"…","host":"<machine-id hash>"}
```
- Every later run requires the marker, with a matching `dest_folder_id`, both
  at the start and again right before the delete phase. If it is missing,
  resolution fails with `dest_marker_mismatch` ("the drive is not the one this
  job wrote to, or it isn't mounted").
- The "Reconnect to this folder" action in the UI re-adopts a folder after the
  user confirms.
- A destination with no marker is accepted only when the job has no successful
  run yet.

**Cancel on unmount.** The engine's mountwatch (§7.3) cancels every run
context whose recorded mount ID disappears, with `cancelled_unmounted`, before
rclone can fall back to writing into the empty directory on the root disk. The
check runs again on every directory create through a wrapping `fs.Fs` (a cheap
cached `mountID` lookup, refreshed on each mountinfo change).

### 6.3 Prechecks (`engine/precheck.go`, all mandatory, reported as a list)
1. Source and destination resolve and are online.
2. After `EvalSymlinks`, neither side lies inside the other. The one exception
   is a destination under the source that is covered by an automatic exclude;
   the engine adds that exclude itself.
3. Allowed and denied roots, as in §6.2.
4. **Free space:** `statfs` or `About()`. A first copy, mirror or archive needs
   at least `source_size × 1.05`. Later runs need at least 1 GiB plus the
   estimated delta. Remotes without `About` skip this check with a warning.
5. **Filesystem quirks table** (`engine/fsquirks.go`, keyed by destination
   fstype or backend):

   | dest | ModifyWindow | encoding | checks |
   |---|---|---|---|
   | vfat | 2 s | `local` Win/Mac encoding | fail with `fat32_file_too_large` if any file > 4 GiB-1 |
   | exfat | 2 s | Win encoding | case-collision scan |
   | ntfs / ntfs3 / fuseblk | 100 ns (1 µs) | Win encoding | case-collision scan |
   | ext4 / xfs / btrfs | 1 ns | default | – |
   | onedrive | backend default | backend | case-collision scan |
   | drive (Google) | backend | backend | skip Google Docs (`--drive-skip-gdocs`), duplicate-name warning, 750 GB/day → `cloud_quota_daily` |
   | terabox | `ModTimeNotSupported` | backend | `SizeOnly=true`, verify disabled, UI label "limited: changes detected by size only" |

   A case collision (`a.txt` and `A.txt` both in the source) fails with
   `case_collision` and lists the first 20. It never loops.
6. **Metadata:** when the destination is local, USB, NTFS, ext4 or a merge pool,
   set `Metadata=true` (rclone `-M`) and `Links=true` (`.rclonelink`
   translation). Cloud destinations get neither, which is why AppData and VM
   presets toward cloud **only allow Archive** (§12.3, step 1).
7. **Source sentinel:** the source must be non-empty unless
   `allow_empty_source`. If the source's file count fell by more than
   `EmptySourcePct` compared with the last success, the run goes to
   `waiting_user` with `empty_source`.

### 6.4 Guards during the transfer (mirror and copy)
- Mirror runs with `DeleteMode=DeleteModeAfter`, so `MaxDelete` trips before
  anything is deleted.
- Mirror runs a **planning pass** first: `sync.Sync` with `DryRun` into a
  counting logger, bounded to 10 min, and skipped when the previous run had
  fewer than 1000 files.
  - Delete guard: `delete_count / dest_count > DeletePct`.
  - Change guard: `(modified + deleted) / dest_count > ChangePct`. This catches
    ransomware that rewrites files instead of deleting them.
  - If either trips, go to `waiting_user` with a sample of 500 paths, before any
    write.
  - As a second barrier, `MaxDelete` is set to `ceil(dest_count × DeletePct)`
    on the real pass.
- Copy runs only the change guard, against files it would overwrite, because
  Copy also overwrites changed files.
- **Mirror exclusions, always added on the destination side:**
  `/.nivaro-versions/**`, `/.nivaroos-backup.json`, and the destination path
  when it sits under the source. Without them rclone deletes its own versions
  folder, or `operations.go:1943` refuses an overlapping backup dir.
- **Versions folder:** `BackupDir = <dest>/.nivaro-versions/<run-start UTC, 20260924T030000Z>`.

---

## 7. Triggers and conditions

### 7.1 Shared clock
- `services/core/service/scheduler_clock.go` owns the single `cron.Cron`, with
  the parser `cron.Minute|Hour|Dom|Month|Dow|Descriptor` and the server's local
  time zone.
- `schedule.go` and `backup/triggers.go` both register entries on it, each
  keeping its own ID map.
- **Clock gate:** the clock does not start until the time is synced
  (`timedatectl show -p NTPSynchronized --value` = `yes`, polled every 10 s) or
  10 min pass. In the second case it logs `clock_unsynced` and shows a banner in
  the app. The same gate applies to catch-up. This protects Pi-class boards with
  no RTC from firing cron at 1970 times.
- DST behaviour is robfig's: a spring-forward gap time is skipped and a
  fall-back repeated time fires once. It is tested explicitly (§16.1), and the
  UI preview shows the real next times from the server.

### 7.2 Trigger kinds (v1)
| kind | fires when | notes |
|---|---|---|
| `schedule` | cron entry | `cron/preview` gives the human text + next 5 times server-side |
| `volume_mounted` | engine event `volume.mounted` whose uuid (+serial/size) matches `VolumeRef` (default: job dest, else job source) | 30 s settle delay; at most once per attach (keyed by mount ID); optional `min_gap_hours`; mounts already present when the watcher starts (boot, service restart) do **not** fire - they go to catch-up |
| catch-up (implicit, per trigger `catch_up`, default from `AppSettings.CatchUpDefault`) | 3 min after the clock gate opens | for each schedule trigger: if `sched.Next(lastSuccessOrCreated) < now`, enqueue once (coalesced); for volume triggers: if the volume is present and no success since it was last seen, enqueue once |
| `manual` | Run now | highest priority |


### 7.3 Mountwatch (`engine/mountwatch.go`)
- The engine opens `/proc/self/mountinfo` and waits on `POLLPRI`, which the
  kernel signals on mount-table changes. It diffs the table, with a 60 s timer as
  a backstop.
- It emits `volume.mounted` and `volume.unmounted` with `mount_id, uuid, serial,
  size, fstype, mount_point, label, tran`. It covers `usb-mount.sh`, fstab,
  merge and manual mounts with **no change to `usb-mount.sh`**, and removes the
  race where `disk:added` fires before the mount exists.

### 7.4 Conditions
- They are evaluated at `precheck`, and again when a waiting run re-checks every
  5 min.
- `dest_available`: the destination resolves, with a matching marker if it has
  one. It is forced on and locked for Mirror.
- `window`: the current server time lies within the window (it may wrap
  midnight). Outside it, `wait` means wait until the window opens, up to
  `WaitMaxMin`.
- `when_unmet`:
  - `skip`: record `skipped` in grey with a reason, which is not a failure.
  - `wait`: re-check every 5 min, then `skipped` once `WaitMaxMin` passes.
  - `fail`: `failed` plus a notification.
- Two failures of `dest_offline` beyond the retry window send the notification
  "offline past retry window".

---

## 8. Queue, locking, hooks, resume

### 8.1 Queue (`backup/queue.go`)
- Worker pool of size `AppSettings.MaxConcurrent`, default 2. The engine also
  caps cloud work at 1.
- Priority: manual, then retry, then triggered, then catch-up. FIFO within a
  priority.
- **Per job:** one run is active or queued at a time. Later triggers increment
  `Coalesced` on the queued or running row and are never dropped silently.
- **Per destination write lock:** the key is `kind|ref_id|sub_path`, and a
  prefix overlap counts as a conflict. Restore runs take the write lock on
  their target **and** on the source endpoint of every job whose source contains
  that target, so a restore into `/DATA/Documents` waits for a running
  Documents backup.
- **Per app and per VM lock:** jobs whose hooks touch the same app or VM run one
  after the other.
- **With Schedules:** `schedule.go` gains `IsBusy(kind, target)`, and backup
  exposes the same. A Schedules VM or container task on a target that a running
  backup hook holds waits up to 30 min, then fails with `busy`. The reverse also
  holds: a backup hook waits for a running Schedules task.

### 8.2 Hooks (`backup/hooks.go`)
- **stop_apps / start_apps:** app-management `/v2/app_management/compose/<app>`
  status change, using the HTTP helper pattern of
  `updateContainerViaAppManagement`.
  - The app's prior state is recorded in `RunRow.HooksDone` **before** acting.
    An app that was already stopped is not started afterwards.
  - `together` stops every app, runs one transfer, then starts every app.
  - `one_at_a_time` splits an archive or copy with several sources into
    sequential sub-transfers, each wrapped in its own stop and start. This
    applies only when each source maps to a single app.
- **shutdown_vm / start_vm:** through vm-sidecar when it is installed
  (`POST /vms/<name>/shutdown`), otherwise `virsh shutdown <name>`.
  - Poll the state until `shut off`, with `TimeoutSec` defaulting to 600.
  - On timeout, abort with `vm_shutdown_timeout`. The notification explains:
    "The VM didn't shut down (no ACPI or guest agent?). Nothing was copied, the
    VM is still running." The engine never forces the VM off.
  - A VM that was already off is not started afterwards.
- **Database detection** is a heuristic: the image matches
  `postgres|mariadb|mysql|mongo|redis|immich|nextcloud|…`, or a volume under
  AppData contains `PG_VERSION`, `ibdata1` or `*.sqlite`. Stopping such apps is
  the default, and the user can override it per app in the wizard.
- **Expected downtime** is `last run duration` (or an estimate at 50 MB/s),
  shown in Review and in the run notification.
- **Post-hook replay:** at core start, any run left `running` whose `HooksDone`
  has a pre hook without its post is replayed (the recorded apps are started
  and the VM is started) **before** it is marked `interrupted`. An app is never
  left stopped after a crash or an update.

### 8.3 Resume and cancel
- **Resume:** rclone skips files whose size and modtime (or size only on
  TeraBox) already match, so a retry after an interruption is effectively a
  resume. A partially written archive (`*.tar.zst.partial`) is deleted and the
  archive is rebuilt. It is renamed to its final name only on success.
- **Cancel:** `POST /runs/:id/cancel` → `engine:/jobs/:eid/stop`, which calls
  `jobs` cancel on the context. Post hooks still run. Files already copied stay.
- **max_duration:** the context deadline. The run ends as `failed` with
  `max_duration`.

### 8.4 Updates and restarts
- `install.sh`'s update path calls
  `curl -s http://127.0.0.1:<gateway>/v1/backup/runs?status=running` (core's
  loopback exemption applies) before restarting services.
  - If runs are active, it prints them and asks "Wait for them / Cancel them /
    Abort update". It waits by default in non-interactive mode, for up to 2 h,
    then cancels.
  - `--force` skips the check.
- After the restart, runs interrupted anyway are replayed and requeued (§5).

---

## 9. Retention

| type | rule | implementation |
|---|---|---|
| Copy | none; nothing is deleted at dest | – |
| Mirror | `.nivaro-versions/<ts>` folders older than `VersionsDays` are purged | a `prune` phase after a successful mirror run: list the first level of `.nivaro-versions`, parse the **folder name** timestamp (never mtime), `operations.Purge` those older than the cutoff. Pruning is **suspended** while the job has any run in `waiting_user` or its last run tripped a guard; the UI shows "Clean-up paused until you review" |
| Archive | keep last `KeepLast` files matching `nivaro_<job>_<ts>.tar.zst` | after success; names parsed, not mtime; the new archive is never counted until renamed from `.partial` |
| Logs | last 100 runs per job, or 180 days, and 500 MB in total | daily maintenance tick in core; oldest first; logs gzipped at run end |

Retention for versioned backups (restic GFS "Smart", last N, custom, timeline
preview) is **v1.1**. The `Retention` struct gains `Mode, Hourly…Yearly` then.

---

## 10. Progress, events, notifications, logs

### 10.1 Message-bus events (registered in `services/core/common/message.go`)
- `nivaroos:backup:run-begin`, `-progress`, `-end`, `-waiting`, and
  `nivaroos:backup:job-changed`.
- Properties are strings, as the bus requires:
  - `run_id, job_id, status, phase, error_code`
  - `bytes, total_bytes, files, total_files, speed_bps, eta_sec, errors, current_file`
- `progress` is sent at most once per second per run, and only while the value
  changes.

### 10.2 Stats source
- Core polls `engine:/jobs/:eid`, which returns the `accounting` stats for group
  `bk_<run_id>`, once a second while a run is active. It keeps the result in
  memory and never writes it to the database.

### 10.3 UI transport
- Socket.io, through the `$socket` already set up in `main.js`, with path
  `/v2/message_bus/socket.io/`.
- Fallback: `GET /v1/backup/runs/:id` every 3 s, and only while the run window
  is visible and the socket is disconnected.
- This replaces the old full-list refetch every 2 s × 900.

### 10.4 Notifications
Notifications go through `MyService.Notify().SendNotify` and are persisted to
NotificationCenter. Actions carry
`act.window = {id:'backup', component:'BackupApp', props:{section:'activity', runId}}`.

| event | default | text (i18n key) |
|---|---|---|
| run failed | on (per-job can't disable in v1) | `backup.notify.failed` "Documents → Google Drive failed: sign-in expired." [View log] [Fix] |
| waiting_user | on | `backup.notify.waiting` "Mirror paused: would delete 42 % of files. Review?" [Review] |
| stale | on, once/day max | `backup.notify.stale` "No successful backup of Photos in 9 days." |
| dest offline past retry window | on | `backup.notify.offline` |
| success | off (per job) | `backup.notify.success` |
| USB run start / done | on for volume_mounted triggers | toast "Photos → Sandisk is running, don't unplug." then "Done, safe to eject" [Eject → `DELETE /v1/disks/usb`] |
| migration report | once | `backup.notify.migrated` |

### 10.5 Logs
- JSONL, one object per line:
  `{"t":"…","lvl":"info|warn|error","code":"file_copied|…","msg_key":"backup.log.copied","args":{…},"raw":"<engine line>"}`.
- The engine writes rclone log lines for the stats group through a per-run
  `fs.LogOutput` hook routed back over the job's NDJSON log stream, and core
  appends them to the file.
- `GET /runs/:id/log?after=<byte offset>&limit=500` returns
  `{lines, next_offset, done}`. A gzipped finished log is read through gzip, and
  the offsets are into the decompressed stream.

---

## 11. REST API (core, `/v1/backup`, JWT group; gateway route added next to `/v1/schedules` in `core/main.go`)

- Envelope: the standard `model.Result{success, message, data}`.
- Validation errors: HTTP 400 with
  `data:{"error_code":"validation","field_errors":{"dest.sub_path":"backup.err.path_not_allowed"}}`.
- Every mutating call checks for the admin role through a
  `requireRole("admin")` middleware that reads the role claim; today everyone is
  admin. Each call writes an audit log line (`job_id`, user, action). Create,
  update and restore are limited to 30 requests per minute per user.

| Method | Path | Request | Response `data` |
|---|---|---|---|
| GET | `/capabilities` | – | §3.4 |
| GET | `/locations` | `?role=source\|dest` | §6.1 array |
| GET | `/locations/browse` | `?kind&ref_id&path&dirs_only=1` | `{"path":"photos","entries":[{"name":"2024","dir":true,"size":0,"mtime":"…"}],"truncated":false}` (max 2000) |
| POST | `/locations/resolve-path` | `{"path":"/DATA/tower/photos"}` | `{"endpoint":{…},"ok":true}` (used by Files entry point + migration) |
| GET | `/jobs` | – | `[{…job, "health":"ok\|warning\|problem\|offline\|disabled","last_run":{id,status,ended_at,summary},"next_run":"2026-09-25T03:00:00+02:00","dest_online":true}]` - **no logs** |
| POST | `/jobs` | Job (below) | job (`revision:1`) |
| GET | `/jobs/:id` | – | job + `stats:{size_bytes, versions_count, last_success}` |
| PUT | `/jobs/:id` | Job incl. `revision` | job / **409** `{"error_code":"revision_conflict","current":{…}}` |
| DELETE | `/jobs/:id` | `?purge_data=false` | `{}` (purge = delete dest folder contents incl. versions; audited) |
| POST | `/jobs/:id/toggle` | `{"enabled":false}` | job |
| POST | `/jobs/:id/run` | `{"preview":false}` | `{"run_id":"run_…"}` (202) |
| POST | `/jobs/:id/reconnect-dest` | – | job (re-adopt marker) |
| POST | `/validate` | Job | `{"ok":false,"checks":[{"id":"dest_inside_source","status":"fail","msg_key":"…","args":{}}],"estimate":{"source_bytes":…,"dest_free":…}}` |
| POST | `/cron/preview` | `{"cron":"0 3 * * *"}` | `{"valid":true,"human_key":"backup.cron.daily_at","args":{"time":"03:00"},"next":["…5 ISO"],"timezone":"Europe/Berlin","error":""}` |
| GET | `/runs` | `?job_id&status&kind&limit=50&before=<run_id>` | `{"runs":[…RunRow minus paths],"next_before":"run_…"}` |
| GET | `/runs/:id` | – | run + `live:{bytes,total_bytes,files,total_files,speed_bps,eta_sec,current_file}` |
| GET | `/runs/:id/log` | `?after&limit` | §10.5 |
| POST | `/runs/:id/cancel` | – | run |
| POST | `/runs/:id/decide` | `{"proceed":true}` | run (waiting_user only) |
| GET | `/runs/:id/preview` | `?op=add\|update\|delete&q&offset` | `{"counts":{"add":4120,"update":12,"delete":318,"bytes_add":…},"items":[{"op":"delete","path":"…","size":…}]}` |
| GET | `/jobs/:id/versions` | – | `[{"id":"current","time":null,"label_key":"backup.ver.current"},{"id":"v_20260923T030000Z","time":"…","files":14,"bytes":…,"kind":"recycle"}]` / archives: `kind:"archive"` |
| GET | `/jobs/:id/versions/:vid/browse` | `?path` | entries |
| POST | `/jobs/:id/restore` | `{"version_id":"v_…","paths":["docs/tax.pdf"],"target":{"mode":"original"}\|{"mode":"other","endpoint":{…}},"conflict":"keep_both\|overwrite\|skip","dry_run":false}` | `{"run_id"}` |
| POST | `/downloads` | `{"job_id","version_id","paths":[…]}` | `{"token":"dl_…","expires_in":60}` |
| GET | `/downloads/:token` | – (no JWT; single-use token, 60 s, bound to requester IP) | file or zip stream |
| GET/PUT | `/settings` | AppSettings | AppSettings |
| GET | `/migration` | – | report; POST `/migration/rerun` re-runs (idempotent) |

**Job JSON** (request and response):
```json
{"id":"bk_1a2b3c4d5e6f","name":"Photos → Sandisk","type":"mirror","enabled":true,"revision":3,
 "sources":[{"kind":"volume","ref_id":"6c1e…","sub_path":"photos","label":"tower"}],
 "dest":{"kind":"usb","ref_id":"3A4F-1C22","match":{"serial":"4C53…","size_bytes":128035676160},"sub_path":"NivaroOS Backups/Photos","label":"Sandisk 128G"},
 "triggers":[{"kind":"volume_mounted","min_gap_hours":24,"catch_up":true},{"kind":"schedule","cron":"0 3 * * 0","catch_up":true}],
 "conditions":{"dest_available":true,"when_unmet":"skip"},
 "filters":{"exclude_presets":["caches","trash"],"exclude":[]},
 "options":{"verify":false,"preview_first":true,"low_priority":true,"max_duration_sec":86400},
 "guards":{"empty_source_pct":50,"delete_pct":10,"change_pct":30,"allow_empty_source":false},
 "retention":{"versions_days":30},
 "hooks":[],"retry":{"max":3,"backoff_sec":[60,600,3600]},
 "notify":{"on_success":false,"on_failure":true,"stale_after_hours":336},
 "needs_attention":"","migrated_from":null,"created_at":"…","updated_at":"…"}
```

**Server-side validation** (`backup/validate.go`):
- source count: 1 for copy and mirror, 1–16 for archive
- endpoint kinds and sub-paths as in §6.2
- cron is valid
- `volume_mounted` has a resolvable ref
- guards are within 1–100
- `hooks` only reference installed apps and VMs
- an AppData or VM preset toward a cloud destination is **rejected** unless the
  type is `archive` (`backup.err.appdata_cloud_needs_archive`)
- Mirror requires `dest_available=true`

### 11.1 Internal engine API (unix socket, local-storage `route/engine.go`)
| Method | Path | Body | Response |
|---|---|---|---|
| GET | `/health` | – | `{api, rclone, pid, started_at}` |
| GET | `/endpoints` | – | §6.1 (without core-owned smb/app/vm data) |
| GET | `/volumes` | – | `[{mount_id, uuid, serial, size, fstype, mount_point, label, tran}]` |
| POST | `/resolve` | `{endpoint, smb_creds?}` | `{root, mount_id, online, quirks, free, marker:{…}\|null}` |
| POST | `/resolve-path` | `{path}` | `{endpoint}` |
| GET | `/browse` | `?endpoint(json)&path&dirs_only` | entries |
| POST | `/jobs` | `{"op":"copy\|sync\|archive\|extract\|check\|list_versions\|purge_versions\|purge_archives\|plan","run_id","src":Endpoint,"dst":Endpoint,"smb_creds":{…},"filters":{…},"options":{…},"guards":{…},"dest_folder_id":"…","first_run":bool,"restore":{…}}` | `{"engine_job_id":17}` |
| GET | `/jobs/:eid` | – | `{state:"running\|done\|error", stats:{…}, result:{counts, error_code, error_detail, guard:{…}}}` |
| POST | `/jobs/:eid/stop` | – | `{}` |
| GET | `/jobs/:eid/log` | NDJSON stream | lines |
| GET | `/events` | NDJSON stream | `{type:"volume.mounted",…}` |

The engine keeps finished job results for 1 h, so core can collect them after
a reconnect.

---

## 12. UI

### 12.1 App and navigation
- `BackupApp`: window id `backup`, size 1040×680, persistable. Add it to
  `PERSISTABLE_COMPONENTS` in `store/mutations.js`.
- Add it to Dock `BUILTIN_DEFS` as "Backup & Sync" with a new icon at
  `assets/img/app-icons/backup.svg`.
- Sidebar, in the same style as SettingsNav:
  **Overview · Jobs · Restore · Activity · Settings**.
  - On phones (<480 px) this becomes a bottom tab bar inside the window.
  - On tablets it becomes a collapsible rail.
- Deep links are props on the `backup` window:
  - `{section, jobId, runId, wizard:true, preset, sourcePath, destRef}`
  - A second `OPEN_WINDOW` for the same id merges the props and stamps
    `requestedAt`, and a watcher in the app reacts to it.

### 12.2 Overview
```
┌ Backup & Sync ────────────────────────────────────────────── _ □ × ┐
│ ▣ Overview  │ ● All good · 4 jobs · last success 03:12   [+ New job]│
│ ≡ Jobs      │ ┌ Needs attention (1) ─────────────────────────────┐  │
│ ⟲ Restore   │ │ ▲ Photos → Sandisk USB                           │  │
│ ◷ Activity  │ │   No backup in 9 days: drive not connected.      │  │
│ ⚙ Settings  │ │   [How to run it] [Change destination]           │  │
│             │ └──────────────────────────────────────────────────┘  │
│             │ ┌ Running now ─────────────────────────────────────┐  │
│             │ │ AppData → tank   ███████░░░ 68 % · 12 MB/s  [Open]│  │
│             │ └──────────────────────────────────────────────────┘  │
│             │ ┌ Coming up ───────────────────────────────────────┐  │
│             │ │ Tonight 03:00  Documents → Google Drive          │  │
│             │ │ On plug-in     Photos → Sandisk USB              │  │
│             │ │ Times are server time (Europe/Berlin, UTC+2)     │  │
│             │ └──────────────────────────────────────────────────┘  │
└─────────────┴──────────────────────────────────────────────────────┘
```
- The header state is one of **All good / Needs attention / Problem / No jobs
  yet**, shown with an icon, text and a colour token.
- Each attention row gives the problem, then its cause, then one to three direct
  fixes, taken from the `errorCodes.js` actions.
- The empty state explains what a backup is, with the buttons "Back up photos to
  a USB drive", "Protect my apps" and "Start from scratch", plus an inline
  "Backup vs sync?" explainer.
- Coverage (3-2-1) and the 14-day strip are **later**.

### 12.3 Jobs
```
│ Jobs                         [Search…] [All ▾] [Sort: Next run ▾]   │
│ ┌──────────────────────────────────────────────────────────────┐    │
│ │ [mirror] AppData → tank                ▶ Running 68 %         │    │
│ │ Mirror · recycle 30 days · Every day at 3:00 AM               │    │
│ │ Deleted files are kept in the recycle folder for 30 days.     │    │
│ │                          [Open progress] [Cancel]  [⋯]        │    │
│ ├──────────────────────────────────────────────────────────────┤    │
│ │ [copy] Documents → Google Drive                  [● On ]      │    │
│ │ Copy new files · Every day at 2:00 AM                         │    │
│ │ ✕ Failed 7 h ago: sign-in expired · next: tonight 02:00       │    │
│ │                          [Run now] [Fix sign-in]   [⋯]        │    │
│ └──────────────────────────────────────────────────────────────┘    │
```
- Rows are list items. Enter opens the detail pane, with tabs Summary, History,
  Versions and Settings. The pane sits on the right when the window is wide and
  becomes a full view with a Back button when it is narrow.
- The ⋯ menu is `role="menu"` with arrow keys and holds: Edit, Duplicate, Preview
  run, Pause schedule (which toggles `enabled`), Delete.
- Delete uses `confirmWindow` with the checkbox "Also delete the backup data on
  the destination" (`checkedIsDanger`).
- Filters: All, Problems, Running, Disabled, Imported.
- Migrated jobs carry an "Imported from Scheduled Tasks" badge until they are
  edited once.

### 12.4 Wizard (`BackupJobWizardWindow`, id `backup-wizard-<jobId|new-<ts>>`, 720×620)
Steps: **Start (new only) → What → Where → When → Keep → Review**.
- The stepper can jump back to any completed step.
- Focus moves to the step heading on each step change.
- Enter never submits a partial wizard.
- The draft autosaves to localStorage under the window id (wrapped in
  try/catch). Reopening offers "Restore your unsaved job?".
- Closing with changes asks through `confirmWindow`.
- Editing an existing job opens straight to Review with edit links.

**Start: presets** (`presets.js`). A preset is hidden when it can't apply (no VMs,
no containers).

| Preset | Type | What | Where | When |
|---|---|---|---|---|
| Back up photos to USB when plugged in | Mirror | Immich library if detected, else `/DATA/Gallery` | ask for USB (identity) | volume_mounted, ≤1/day |
| Protect my documents in the cloud | Copy | `/DATA/Documents` | ask for cloud | daily 02:00 |
| Protect my apps' data | Archive (cloud) / Mirror (local) | `/DATA/AppData/<selected>` | ask | daily 03:00, stop apps with DB |
| Back up virtual machines | Archive (cloud) / Mirror (local) | `/DATA/VMs/<selected>` | ask | weekly Sun 04:00, shut down VM |
| Copy downloads to a drive | Copy | `/DATA/Downloads` | ask | every 6 h |
| Start from scratch | – | – | – | – |

**What** (job type and source):
```
│ ○ Start ● What ○ Where ○ When ○ Keep ○ Review                      │
│  What kind of job?   (radiogroup; arrow keys)                      │
│  (•) Mirror       Destination becomes an exact copy.               │
│                   Deleted files go to a recycle folder (30 days).  │
│  ( ) Copy new files  Adds new/changed files. Never deletes there.  │
│  ( ) Archive      One .tar.zst file per run. Keeps owners & links. │
│  ▸ What happens if I delete a file?                                │
│ ───────────────────────────────────────────────────────────────── │
│  What do you want to protect?                                      │
│  [folder] photos   tower (NTFS) · 210 GB                [Change]   │
│  Quick add: [My apps' data ▾] [Virtual machines ▾] [Shared folders]│
│  Skip: [✓] Cache & temp  [✓] Trash  [ ] Files over [4] GB  [+ Pattern]│
│  ▸ Keep apps consistent (2 apps)                                   │
│     [✓] Stop Immich during backup (≈ 4 min)   [✓] Stop Blinko      │
│     Stop them: (•) all together  ( ) one at a time                 │
└────────────────────────────────────────────────────────────────────┘
```
- Choosing a source opens `StoragePickerWindow` in source mode, then
  `FolderPickerWindow`.
- Hints:
  - An AppData app with a database: "Immich keeps a database here. Stop it so
    the copy isn't corrupted." The default is on, and it can be overridden.
  - A running VM: "This VM will be shut down and started again."
- A custom exclude pattern is validated live through `/validate` (debounced) and
  shows example matches.

**Where**: a location card plus **Choose…**, which opens `StoragePickerWindow`:
```
┌ Choose where to save ─────────────────────────────────────── _ □ × ┐
│ [Search locations…]                                   [Refresh]    │
│ INTERNAL DRIVES                                                    │
│  (•) tower   NTFS · 1.8 TB free of 3.6 TB  ████████░░  ● Ready     │
│  ( ) tank    ext4 · 820 GB free of 1 TB    ███░░░░░░░  ● Ready     │
│  ( ) System disk (/DATA) · 40 GB free      ▲ Not recommended ⓘ     │
│ USB DRIVES                                                         │
│  ( ) Sandisk 128G  exFAT · 101 GB free     ● Plugged in            │
│  ( ) Backup-WD 2T  last seen 3 days ago    ○ Not connected         │
│ NETWORK SHARES                                                     │
│  ( ) \\192.168.1.20\backup  62 GB free     ● Online                │
│ CLOUD ACCOUNTS                                                     │
│  ( ) Google Drive           1.2 TB free    ● Connected             │
│  ( ) TeraBox                —              ▲ Limited (size only) ⓘ │
│  [+ Connect a network share] [+ Add cloud account]                 │
│                                 [Cancel]  [Choose folder →]        │
└────────────────────────────────────────────────────────────────────┘
```
- Status is always an icon plus text, never colour alone.
- The free/total bar is `role="meter"` with a text alternative.
- Choosing the system disk or the same physical disk as the source shows an
  amber note and asks for an extra confirmation.
- A destination inside an exported Samba share, or on an NTFS or exFAT USB drive
  mounted `umask=000`, shows: "Anyone on your network who can open this share
  can change or delete the backup." Offer "Use a folder outside shared folders".
  Where the filesystem supports it, the engine creates the job folder as root
  0700.
- The folder step proposes `<location>/NivaroOS Backups/<job name>` and creates
  it on the first run.
- The space estimate comes from `/validate`: "Needs about 212 GB. 1.8 TB free,
  OK."
- **Blocking checks:** destination inside source, path not allowed, and an
  AppData or VM source to cloud with a type other than Archive. For the last
  one the message is "Cloud storage can't keep file owners and links. Use
  Archive for app data", with a one-click "Switch to Archive".
- Quirk hints come from `quirks[]`, e.g. FAT32's 4 GB limit, or TeraBox
  detecting changes by size only.

**When**:
```
│  Run this job                                                      │
│  [✓] On a schedule   [ Every day ▾ ] at [03]:[00]                  │
│      Every day at 3:00 AM · server time (Europe/Berlin, UTC+2)     │
│      (9:00 PM your time)  Next: Thu 25 Sep 03:00 · Fri 26 · Sat 27 │
│  [✓] When "Sandisk 128G" is plugged in   at most once every [24] h │
│  [✓] If a run was missed (server was off), run it after start-up   │
│  Only run when…                                                    │
│  [✓] The destination is available  (required for Mirror) 🔒         │
│      If not: [ Skip this run ▾ ]                                   │
│  [ ] Between [22:00] and [07:00]                                   │
│  If it fails: retry [3 ▾] times ([1 min, 10 min, 1 h])             │
```
- `ScheduleBuilder` (shared) offers: every hour, every N hours, every day, some
  weekdays, every week, every month, and Custom.
- Custom shows a cron field. It opens automatically whenever the existing cron
  doesn't match a builder pattern, and never rewrites it.
- The preview always comes from `/cron/preview`, in server time with the zone
  label, and shows browser time as well when the zones differ.
- An invalid cron shows the parser error inline and disables Next.
- With no trigger ticked, the job is "Manual only" and the step says so.

**Keep**:
- **Mirror:** "Keep deleted and replaced files for [30] days", the guards ("If a
  run would delete more than [10] % or change more than [30] %, stop and ask
  me"), and "Preview the first run" (on).
- **Archive:** "Keep the last [8] archives".
- **Copy:** only the line "Nothing is ever deleted at the destination".
- A "Check files after copying" toggle (verify). It is disabled with the reason
  when the destination has no hashes.

**Review**:
- A plain-sentence summary built by `JobSummary.vue` from the same i18n keys the
  job card uses.
- An editable name, defaulting to "<source> → <destination>".
- Advanced, a `<details>` with a real summary button, holds: low priority,
  maximum duration, include patterns and empty folders.
- "Run the first backup now" (on).
- **Create** posts the job. If `preview_first` is on, it then opens
  `BackupPreviewWindow`, otherwise it runs the job and opens `BackupRunWindow`.

### 12.5 Run window (`BackupRunWindow`, id `backup-run-<runId>`, 640×520)
```
┌ Running · AppData → tank ─────────────────────────────────── _ □ × ┐
│  ███████████████████████░░░░░░░░░  68 %                            │
│  Copying · 3,210 of 4,700 files · 8.1 of 11.9 GB                   │
│  12.3 MB/s · about 6 min left · started 03:00                      │
│  Now: immich/library/2024/IMG_2231.HEIC                            │
│  ┌ Steps ───────────────────────────────────────────────────────┐  │
│  │ ✓ Checked the destination (tank, marker OK)                  │  │
│  │ ✓ Stopped Immich (will start again after)                    │  │
│  │ ▶ Copying files                                              │  │
│  │ ○ Cleaning versions older than 30 days                       │  │
│  │ ○ Starting Immich                                            │  │
│  └──────────────────────────────────────────────────────────────┘  │
│  ▸ Details log (live)  [Show technical output] [Copy] [Download]   │
│                                              [ Cancel run ]        │
└────────────────────────────────────────────────────────────────────┘
```
- Progress is `role="progressbar"`. An `aria-live="polite"` region announces
  only step changes and the result.
- The log is `role="log"` with `aria-live="off"`. It auto-scrolls unless the user
  has scrolled up, in which case a "Jump to latest" chip appears.
- Cancel confirms: "Files already copied stay. Immich will be started again."
- When the run finishes, the window shows a summary: added, changed, moved to
  recycle, skipped (with reasons), errors (a list), duration and data.
  `partial` is shown amber.
- The window opens read-only for past runs.
- It is **not** in `DARK_WINDOW_COMPONENTS`. The log box uses the theme's code
  background.

### 12.6 Preview and guard window (`BackupPreviewWindow`, 760×560, NO_SCROLL)
```
│  Preview: what this mirror run will do                             │
│  + 4,120 to add (11.2 GB)   ~ 12 to update   − 318 to delete       │
│  ▲ 318 files at the destination aren't in your source. They'll be  │
│    moved to the recycle folder and kept 30 days.                   │
│  [Adds] [Updates] [Deletes (318)]   [Search…]                      │
│  − NivaroOS Backups/Photos/old-phone/DCIM/…   (virtual list)       │
│  ( ) Run it as shown  ( ) Run as "Copy new files" once (no deletes)│
│                                  [ Cancel run ]  [ Continue ]      │
```
The same window handles `waiting_user` runs from guards, empty source and first
preview, and is opened from the notification. Continue calls
`/runs/:id/decide`.

### 12.7 Restore (section and `BackupRestoreWindow`)
```
│ Restore                                                            │
│  1. Which job?   [ AppData → tank ▾ ]                              │
│  2. From when?                                                     │
│   ● Current copy (as of today 03:12)                               │
│   ○ Deleted or replaced on Tue 23 Sep 03:00   14 files · 80 MB     │
│   ○ Deleted or replaced on Mon 22 Sep 03:00    2 files             │
│  3. [ Browse this version → ]                                      │
```
- Versions come from `/versions`:
  - Mirror: current, then the recycle folders, newest first.
  - Archive: the archive list.
  - Copy: "This job keeps one copy. Browse it."
- **Browse** opens `BackupBrowseWindow` (900×600, NO_SCROLL):
  - It looks like Files: a breadcrumb, a list, a checkbox per row, and
    `Download` (single-use token) and `Restore…`.
  - The badge reads "Read-only · Deleted or replaced on …".
  - Archive browsing lists the tar index. The engine streams it once, then
    caches it under staging for 1 h.
- **Restore…** opens `BackupRestoreWindow` (560×520):
  - Target: the original location (default), or another folder through the
    pickers.
  - If the file exists: **Keep both** (default, named "name (restored 2026-09-24)"),
    Replace (with an amber note), or Skip.
  - The restore runs as a `kind=restore` run with the same run window, so its
    locks, log and notification are the same as a backup's. On finish it offers
    "Open folder in Files".
- Restore targets obey the allowed roots and `EvalSymlinks`, checked in the
  engine.

### 12.8 Activity
A list of runs grouped by day, with filters for job, result, kind and date range.
The filter state is kept in localStorage, wrapped in try/catch. Each run opens
`BackupRunWindow` read-only. Skipped runs appear in grey with their reason.

### 12.9 Settings section
- Defaults: concurrency, catch-up, guard percentages, recycle days.
- Log retention.
- Remembered USB drives: rename (display only) or forget.
- Import: the migration report and "Re-run import".
- About the engine: versions from capabilities, the clock-synced state, and a
  "Run self-test" button that does a copy, mirror and restore round trip in
  staging and reports each step.

### 12.10 Scheduled Tasks integration
- **`ScheduledTasksSection.vue`:**
  - Remove the backup, sync and archive templates. Add a card: "Back up and sync
    your files → Open Backup & Sync".
  - Add a collapsed, read-only list "Backup & Sync jobs" showing status and next
    run. Clicking a job opens `backup` with `{section:'jobs', jobId}`.
- **`ScheduledTaskWindow.vue`:**
  - The type select drops backup and sync. "File backup…" opens the wizard.
  - It uses the shared `ScheduleBuilder`, which fixes the overwrite-cron and
    no-preview bugs.
  - Changing the type no longer resets the cron.
- **`ScheduledTaskLogWindow.vue`:** refreshes while the task is running, and
  translates its action tag.
- **`en_US.json`:** add all 149 missing `schedule.*` keys and wrap the unwrapped
  strings listed in the audit.

### 12.11 Files (component list)
```
ui/src/apps/backup/
  BackupApp.vue            BackupNav.vue            backup-common.scss
  presets.js               jobLabels.js             errorCodes.js (generated, see §5)
  summaries.js             (pure: job → i18n key+args for card/review/notify parity)
  sections/  OverviewSection.vue  JobsSection.vue  RestoreSection.vue
             ActivitySection.vue  BackupSettingsSection.vue
  components/ StatusHeader.vue  AttentionList.vue  RunningJobCard.vue  UpcomingList.vue
             JobCard.vue  JobDetailsPane.vue  JobSummary.vue  JobTypePicker.vue
             PresetGrid.vue  SourcePicker.vue  ExcludeEditor.vue  AppConsistencyOptions.vue
             LocationCard.vue  LocationRow.vue  SpaceEstimate.vue  TriggerEditor.vue
             ConditionEditor.vue  RetryPolicy.vue  KeepEditor.vue  SafetyOptions.vue
             AdvancedOptions.vue  RunProgress.vue  RunSteps.vue  RunLog.vue  RunSummary.vue
             VersionList.vue  VirtualFileList.vue  ActivityList.vue  ErrorExplain.vue
             EmptyState.vue  BackupVsSyncExplainer.vue  WizardStepper.vue
  windows/   BackupJobWizardWindow.vue  BackupRunWindow.vue  BackupPreviewWindow.vue
             BackupBrowseWindow.vue  BackupRestoreWindow.vue
  __tests__/ presets.spec.js  summaries.spec.js  errorCodes.spec.js  validate.spec.js
             i18nKeys.spec.js  JobTypePicker.spec.js  WizardStepper.spec.js
ui/src/shared/scheduling/ ScheduleBuilder.vue  CronSummary.vue  cronPatterns.js (+ spec)
ui/src/shared/storage/    StoragePickerWindow.vue  FolderPickerWindow.vue
                          (DsFolderPickerWindow becomes a thin wrapper)
ui/src/service/backup.js  (API client; every call returns data or throws {error_code})
ui/src/assets/img/app-icons/backup.svg
```
Edits:
- `windowRegistry.js`: register `BackupApp`, the 5 windows and the 2 shared
  pickers. Add `BackupPreviewWindow`, `BackupBrowseWindow`,
  `StoragePickerWindow` and `FolderPickerWindow` to `NO_SCROLL_COMPONENTS`.
- `Dock.vue` `BUILTIN_DEFS`.
- `store/mutations.js` `PERSISTABLE_COMPONENTS`.
- Files context menu: "Back up this folder…" calls `resolve-path`, then opens
  the wizard with the source filled in.
- Storage drive rows: "Back up to this drive".
- Delete `shell/CoreService.vue`, `apps/syncthing/`, `apps/smart-home/`, and the
  `recommendSwitch` state, mutation and getter.

### 12.12 i18n
- Namespace `backup.*`, grouped as: `backup.nav`, `backup.overview`, `backup.jobs`,
  `backup.type.<t>.{label,promise,deleted}`, `backup.wizard.<step>.*`,
  `backup.loc.*`, `backup.trigger.*`, `backup.cond.*`, `backup.keep.*`,
  `backup.run.*`, `backup.restore.*`, `backup.err.<error_code>.{title,cause,fix}`,
  `backup.notify.*`, `backup.cron.*`, `backup.log.*`.
- The backend sends **keys and args**, never English: run `summary`, `human_key`,
  `msg_key`, check results.
- Notification text is rendered in core from `en_US` (core has no i18n), and the
  notification also stores `{key,args}` so the UI can re-render it in the
  user's locale.
- Dates use `Intl.DateTimeFormat` with the server `timeZone` from capabilities.
  Sizes and counts use `Intl.NumberFormat`.
- Test `i18nKeys.spec.js` scans `ui/src/apps/backup`, `ui/src/shared/scheduling`,
  `ui/src/shared/storage` and the three Schedules files for `$t('…')` literals,
  and fails on any key missing from `en_US.json`. It also checks that every
  `error_code` has `title`, `cause` and `fix`.

---

## 13. Accessibility and theme rules (checked in review and tests)
- **Interactive elements:** every one is a `<button>`, `<a>`, input, or an
  element with a role and key handlers. There are no clickable divs.
  - Type and preset cards are `role="radio"` in a `radiogroup`, with arrow keys,
    Space to select and a roving tabindex.
  - Chips use `aria-pressed`. Switches use `role="switch"` + `aria-checked` and a
    visible label.
  - Icon-only buttons have a translated `aria-label` plus a `title`.
- **Focus:**
  - Focus is always visible through `:focus-visible` with the
    `--color-primary-fg` ring.
  - A window focuses its first meaningful control when it opens, and focus
    returns to the opener when it closes.
  - Esc closes windows that aren't destructive. Confirm windows trap focus.
- **Wizard errors:** a summary at the top of the step links to each field, and
  each field refers to its error with `aria-describedby`.
- **Colour:**
  - Status always uses an icon plus text plus colour.
  - Pills use `--status-{ok,warn,danger,info,muted}-{bg,fg}` tokens, added to
    `_root.scss` and `_dark.scss`. Each pair reaches ≥ 4.5:1 in both themes and
    is checked by `tokens.contrast.spec.js`, which computes the contrast from the
    SCSS values. This replaces the old `#f43f5e` on a 10 % tint.
- **Theme tokens only:** `--theme-card-bg`, `--theme-text-*`, `--space-*`,
  `--radius-*`, `--font-*`. There are no hex values in the backup components,
  enforced by a grep in CI (§16.2).
- **Touch targets:** at least 44×44 px on phone and tablet. The wizard footer is
  sticky and respects `env(safe-area-inset-bottom)`. Nothing scrolls the page
  horizontally, and wide lists scroll inside their own container.
- **Motion:** `prefers-reduced-motion` turns off the progress shimmer and
  transitions.
- **Keyboard shortcuts** inside the app: `N` (new job), `/` (search), `R` (run
  the selected job), and `?` to list them. They never fire while focus is in a
  field.
- **Strings:** layouts allow text 30 % longer than English.

---

## 14. Security summary
- **Endpoints:** resolved server-side from IDs. Client `Resolved` paths are never
  trusted. Allowed and denied roots plus `EvalSymlinks` apply in both core and
  the engine.
- **Surface:** no shell anywhere and no free-form flags. The engine socket is
  root-only with mode 0600.
- **Credentials:**
  - SMB credentials are passed per run in memory and never logged. The logger
    redacts every `pass`.
  - Cloud credentials stay in `rclone.conf`. API responses never include secrets.
- **Downloads:** single-use 60 s tokens instead of a JWT in the URL.
- **Role and audit:** an admin role check on all mutations. Audit lines go to the
  journal for delete-with-purge, restore, decide-proceed on a guard, and settings
  changes.
- **Destination folders:** created 0700 root where the filesystem allows it, with
  a warning where it doesn't (NTFS/exFAT with `umask=000`, exported shares).

---

## 15. Installer changes (`installer/install.sh`, all supported distros)
1. **Go toolchain:** raise `GO_VERSION`, or export
   `GOTOOLCHAIN=go1.26.0+auto`, so that local-storage (`go 1.26.0`) builds on
   purpose rather than by accident. A failed local-storage build aborts the
   install with a clear message.
2. **rclone.service:** stop installing `rclone.service`. On update, run
   `systemctl disable --now rclone.service`, remove the unit, and add a manifest
   cleanup entry. Nothing uses `/var/run/rclone/rclone.sock`, because the
   `httper/drive.go` clients are commented out. Leave `/usr/bin/rclone` alone if
   a user installed it.
3. **Directories:** create `/var/lib/nivaroos/backup/{logs,staging,secrets}`
   (0700) and add them to the manifest as data, so they are never removed on
   uninstall without `--purge`.
4. **Memory drop-in:** write
   `/usr/lib/systemd/system/nivaroos-local-storage.service.d/10-memory.conf`
   with `MemoryHigh=40%` (systemd accepts percentages), then
   `systemctl daemon-reload`.
5. **Update deferral:** the running-backup check before service restarts (§8.4),
   plus a `--force` flag.
6. **v1.1:** `install_restic()`, as in §3.3. It is non-fatal, and capabilities
   report the result.
7. **No new packages in v1.** `hdparm` for the old `disk_standby_check` is
   handled in the Schedules work: the task checks for the binary and reports
   "not available" instead of `|| true`.
8. **Migration:** needs no installer step. It runs in core on first start.

---

## 16. Test plan

### 16.1 Go: core `service/backup/` (engine faked behind an `EngineClient` interface)
- `validate_test.go`:
  - sub-path rejection: `..`, NUL, absolute paths, symlink escape via a fake
    resolver
  - destination inside source
  - AppData to cloud rejected unless Archive
  - Mirror forces `dest_available`
  - invalid cron
- `migrate_test.go`, with a fixture `schedules.json` containing every action and
  alias, a self-including `/DATA` → `/DATA/Backup` task, an absent USB drive, a
  `/mnt/<type>_<name>` path, unknown extra args and VM/container tasks:
  - the result is correct
  - a second run is a no-op
  - a crash injected between the database commit and the JSON rewrite leaves no
    duplicates
  - VM and container tasks survive
  - the `.pre-backup-migration` copy is written once
- `queue_test.go`: coalescing, priority, concurrency cap, destination prefix
  lock, restore-versus-backup lock, app lock, `IsBusy` interplay with a fake
  schedule service.
- `triggers_test.go`:
  - fake clock with robfig schedules, catch-up coalescing
  - clock gate (unsynced → armed after 10 min)
  - DST: Europe/Berlin spring-forward 02:30 is skipped, fall-back 02:30 runs
    once
  - `volume_mounted`: matching by UUID, serial and size; ambiguous clone; no
    fire for mounts present at start; `min_gap_hours`
- `lifecycle_test.go`: every transition in §5, including interrupted at start
  with post-hook replay (a fake app-management records calls), retry backoff per
  error class, the deferred `cloud_quota_daily`, cancel running post hooks,
  `waiting_user` decide and timeout, and `max_duration`.
- `hooks_test.go`: prior state respected, one-at-a-time split, VM shutdown
  timeout never forces off.
- `api_test.go`: echo `httptest` for every route, envelope shape, 409 revision
  conflict, lists carrying no log fields, single-use download token.
- `errors_test.go`: every code has a class, and the generated `errorCodes.js`
  matches the Go enum.
- `schedule_atomic_test.go` (existing package):
  - atomic write survives a kill between the write and the rename
  - a corrupt file is preserved and saving is refused
  - the backup/sync executor refuses those types

### 16.2 Go: local-storage `service/engine/` (real local backend in `t.TempDir()`)
- `resolve_test.go`: fake mountinfo covering a UUID→mount on the root filesystem,
  a bind mount, a btrfs subvolume and mergerfs; unmounted detection; the marker
  (missing, mismatched, new destination); `/mnt/<type>_<name>` → cloud
  endpoint.
- `sync_test.go`:
  - Copy never deletes.
  - Mirror moves deletions into `.nivaro-versions/<ts>` and never deletes
    `.nivaro-versions` or the marker.
  - A destination under the source is auto-excluded.
  - The delete guard and the change guard trip before any write.
  - `MaxDelete` is the second barrier.
  - Dry run changes nothing.
  - Stop cancels.
  - Metadata and symlinks survive on a local destination.
- `unmount_test.go`: a fake mountwatch removes the mount ID mid-run, the run is
  cancelled, and nothing is written after that (a write-counting wrapper `fs.Fs`
  verifies it).
- `fsquirks_test.go`: FAT32 4 GB fail, case collision, `ModifyWindow` per fstype,
  TeraBox size-only and verify disabled.
- `archive_test.go`: names with quotes, `$()`, spaces and newlines round-trip;
  owner, mode, symlinks and sparse files are preserved (checked on a tmpfs file
  with holes); the `.partial` rename; keep-last-N by name.
- `retention_test.go`: pruning by folder name (fake mtimes are ignored), and no
  pruning while suspended.
- `mountwatch_test.go`: a mountinfo diff produces the right events; `POLLPRI`
  runs against a fake fd.
- `stress_test.go` (build tag `stress`, run in CI nightly): a 1 M-file tree,
  Mirror with memory under `SetMemoryLimit`, no goroutine leak after stop.
- `api_test.go` over a real unix socket in a temp dir: socket mode 0600, the
  `/health` contract version.

### 16.3 UI: vitest
- Pure functions: `presets.spec.js` (visibility rules, safe types per
  destination), `summaries.spec.js` (job → key and args, identical for card and
  review), `cronPatterns.spec.js` (patterns round-trip, and an unknown cron goes
  to Custom unchanged), `validate.spec.js` (client pre-validation matches the
  server rules).
- Components with `@vue/test-utils`: `JobTypePicker` (arrow keys, `aria-checked`),
  `WizardStepper` (focus moves to the heading, completed steps are clickable),
  `ScheduleBuilder` (no cron rewrite on type change), `RunLog` (jump-to-latest
  behaviour), `LocationRow` (status text is present, not colour alone).
- `i18nKeys.spec.js` and `tokens.contrast.spec.js`, as in §12.12 and §13.
- CI grep: no `#[0-9a-f]{3,6}` in `ui/src/apps/backup/**/*.vue` style blocks, and
  no `$buefy.dialog` or `b-modal` in backup files.

### 16.4 End-to-end checks (scripted, on a fresh VM from `install.sh`, and on this box)
1. Fresh install on Debian 13, Ubuntu 24.04 and Fedora (amd64), plus a Raspberry
   Pi OS arm64 image:
   - `GET /v1/backup/capabilities` shows the engine available
   - `rclone.service` is absent
   - the backup directories exist
2. Local Copy and Mirror `/DATA/Documents` → an ext4 volume. Delete 5 files and
   run again: they appear in `.nivaro-versions`, and restore brings them back
   with keep-both.
3. Delete guard: remove 50 % of the source and let the scheduled run fire. The
   run is `waiting_user`, the notification links to the preview, and declining
   leaves the destination untouched.
4. USB: plug a stick (loop device + udev in the VM) with a job bound to it. The
   run fires once, and a replug within `min_gap_hours` does not fire.
   **Unplug mid-run:** the run ends `cancelled_unmounted` and nothing lands on
   the root disk (`du` on the empty mount directory stays 0).
5. Missing drive: unmount the destination. With `when_unmet=skip` the run is
   `skipped`, and `/DATA/<mount>` stays empty.
6. Cloud: Copy to Google Drive, and to TeraBox with size-only, verify disabled
   and the UI label shown.
7. SMB destination through the rclone `smb` backend, with the share cut mid-run.
   Cancel returns within 10 s.
8. App hook: a job with Immich or Blinko stops it and restarts it. Kill core
   mid-run and restart: the app is started again (replay) and the run is
   `interrupted` then requeued.
9. Migration: seed `schedules.json` with the three old templates and re-run the
   installer:
   - the jobs appear
   - Schedules shows the card
   - the old executor refuses backup/sync
   - re-running changes nothing
10. Update while a run is active: the installer asks or waits.
11. Clock gate: a VM with NTP off and the clock set to 1970 does not fire cron
    until 10 min pass, and the banner shows.
12. UI on desktop, tablet and phone, dark and light: axe-core WCAG AA with zero
    violations on every backup window, the compositing contrast scan, a
    keyboard-only walkthrough creating a job, and no horizontal overflow.

---

## 17. Implementation plan: independent work packages

The interfaces are frozen first (WP-0). After that, the packages can run in
parallel against fakes.

| WP | Owner area | Delivers | Depends on (interface only) |
|---|---|---|---|
| **WP-0 Contracts** | core + local-storage + ui | `backup/model.go` structs, `backup/errors.go` enum + generator for `errorCodes.js`, `engine` API types (`services/local-storage/service/engine/api.go` mirrored in `services/core/service/backup/engineapi.go`, with a shared JSON fixture test), OpenAPI-ish table §11 as `docs/specs/backup-api.json` fixtures, i18n key skeleton in `en_US.json` | – |
| **WP-ENGINE** | local-storage `service/engine/` + `route/engine.go` + `main.go` socket server | resolve (mountinfo, marker), endpoints, browse, prechecks, fsquirks, copy/sync/plan/check/archive/extract/list_versions/purge, stats, log stream, stop, mountwatch + `/events` + bus events, memory limit, SMB via rclone smb, resolve-path | WP-0 engine types |
| **WP-CORE-JOBS** | core `service/backup/` store, validate, API routes, gateway route, role check, downloads tokens, settings, capabilities | CRUD, runs, logs, preview, versions, restore endpoints calling `EngineClient` | WP-0; uses `FakeEngine` until WP-ENGINE lands |
| **WP-TRIGGERS** | core `scheduler_clock.go`, `backup/triggers.go`, `queue.go`, `lifecycle.go`, `hooks.go`, `notify.go` | shared cron + clock gate, volume events via `/events`, catch-up, conditions, queue/locks, state machine, retries, hooks + replay, message-bus progress, notifications, stale check, log retention tick | WP-0; `EngineClient` fake; `schedule.go` `IsBusy` |
| **WP-MIGRATE** | core `backup/migrate.go` + `schedule.go` fixes | migration, atomic writer, corrupt-file handling, executor refusal, GetTargets cleanup | WP-0; `EngineClient.ResolvePath` fake |
| **WP-UI-SHELL** | `ui/src/apps/backup/` app, nav, Overview, Jobs, Activity, Settings, `service/backup.js`, registry/dock/persist edits, tokens, dead-code removal (CoreService, syncthing, smart-home, recommendSwitch) | BackupApp with socket + polling fallback | WP-0 fixtures (mock `service/backup.js` with fixture JSON) |
| **WP-UI-WIZARD** | wizard window + components, `shared/scheduling/*`, `shared/storage/*` pickers, Preview window, Schedules section/editor changes + 149 i18n keys | create/edit flow, ScheduleBuilder shared | WP-0 fixtures; `OPEN_WINDOW` contract |
| **WP-UI-RESTORE** | Restore section, Browse + Restore windows, Run window | restore flow, live run view | WP-0 fixtures |
| **WP-INST** | `installer/install.sh` | toolchain pin, rclone.service removal, dirs, memory drop-in, update deferral | `/v1/backup/runs?status=running` shape (WP-0) |
| **WP-QA** | tests §16.3/16.4, e2e scripts under `installer/tests/backup-e2e.sh`, axe runs | sign-off matrix | all |

Frozen interfaces between packages:
- **Engine ↔ core:** §11.1 over the unix socket, versioned by `api`, with
  fixtures in `services/*/testdata/engine/*.json` checked by tests on both sides.
- **Core ↔ UI:** §11 plus the §10.1 event names and properties. The UI never
  parses English text, only `error_code` and `*_key`.
- **Shared scheduler clock:** `type Clock interface { Add(spec string, fn func()) (cron.EntryID, error); Remove(cron.EntryID); Next(spec string, from time.Time) (time.Time, error); Armed() <-chan struct{} }`.
- **Backup ↔ Schedules locks:** `IsBusy(kind, target string) bool` and
  `WaitFree(ctx, kind, target)`.
- **Windows:** `OPEN_WINDOW({id, component, title, props, width, height})`, with
  the ids listed in §12.

Order of merge:
1. WP-0
2. WP-ENGINE, WP-CORE-JOBS, WP-TRIGGERS, WP-MIGRATE and the three UI packages, in
   parallel
3. WP-INST
4. WP-QA
5. Release together. The migration and the executor refusal ship in the same
   build.

---

## 18. Later (not v1)

**v1.1**
- restic "Back up with history":
  - built from source at a pinned version, with on-demand install through
    capabilities
  - cloud repositories through `cmd/serve/restic`, imported into local-storage
    (it registers in `init`), on a random 127.0.0.1 port with per-run basic auth
  - GFS "Smart" retention with a timeline preview, monthly `check --read-data-subset=5%`
  - stale-lock rule: older than 30 min, or the PID is dead on this host
  - a warning when RAM is under 2 GB, and a size cap on `RESTIC_CACHE_DIR`
- **Recovery key:**
  - a passphrase with at least 128 bits of entropy, stored at
    `/var/lib/nivaroos/backup/secrets/<job>.key` (0600)
  - never written to any destination, audited when revealed
  - a mandatory "I saved it" confirmation before the first run
  - a re-auth prompt only if user-service adds `POST /v1/users/verify-password`
- **Honest restore instructions:** cloud repositories need
  `restic -r rclone:<remote>:<path>` and an rclone config, not one command.
- Snapshot browser, search, zip download, "connect existing repository", test
  restore.
- `after_job` chains with a cycle check, and the condition "last success older
  than X h".
- A global backup bandwidth limit that openly also limits cloud mounts.
- Email, ntfy, webhook and Healthchecks notifications.

**Later**
- Two-way bisync (Advanced, "not a backup").
- `on_change` inotify with the sysctl drop-in and a scan fallback.
- Database-dump hooks, and `vm_snapshot` through vm-sidecar.
- Encrypted crypt mirror, and a NivaroOS peer target (append-only restic REST).
- 3-2-1 coverage view, 14-day strip, weekly digest, desktop widget, companion
  app status, compare with current.

**Separate fix (Schedules):** the container "update" task should wait for
app-management's update to finish before it reports success.

---

## 19. How each critique point is resolved

| # | Critique point | Resolution in this spec |
|---|---|---|
| 1 | Mirror deletes its own versions folder / overlap refused | Always exclude `/.nivaro-versions/**`, the marker, and dest-under-source; `DeleteModeAfter`; test in §16.2 (§6.4) |
| 2 | Mount checks wrong; drive removed mid-run | mountinfo+UUID resolver in the engine at write time, identity marker checked at start and before delete, cancel-on-unmount by mount ID (§6.2, §7.3), e2e #4/#5 |
| 3 | Per-job bandwidth impossible | Cut from v1; global limit in v1.1 with honest wording (§1, §18) |
| 4 | Metadata/symlinks/sparse | `Metadata`+`Links` for local dests; AppData/VM → cloud only as Archive (tar keeps owner/mode/links/sparse); presets pick type by dest (§6.3, §12.4) |
| 5 | fs quirks, TeraBox, Google Drive | fsquirks table: ModifyWindow, encoding, FAT32 4 GB, case collisions, TeraBox size-only + verify off + label, GDrive skip-gdocs + `cloud_quota_daily` deferred (§6.3, §5) |
| 6 | Migration crash-safety, no silent stop | UNIQUE `migrated_from`, DB commit then atomic JSON rewrite + one-time copy, migrated jobs keep running with guards, move→copy, old executor refuses backup/sync in same release (§4.2) |
| 7 | Mirror not ransomware-safe; exposed dests | change guard 30 %, pruning suspended while guarded, prune by folder name, 0700 folders, share/umask warnings, default folder outside shares (§6.4, §9, §12.4) |
| 8 | Engine crash kills mounts | SetMemoryLimit + MemoryHigh drop-in, caps, no fast-list, one cloud run, stress test; in-process kept to avoid token races (§3.5) |
| 9 | Service order + updates | engine_unavailable is transient/wait, migration waits for engine; installer defers on running runs; replay + requeue (§3.4, §4.2, §8.4) |
| 10 | Portability facts | Alpine out of scope; restic built from source (v1.1); serve/restic import noted; separate WAL backup.db with fatal migration errors; clock gate for RTC-less boards; plus the Go 1.26 toolchain gap (§3.3, §4, §7.1, §15) |
| 11 | CIFS hangs, plaintext creds | Engine uses rclone `smb` backend with per-run in-memory creds (cancellable); kernel mount gets `soft,echo_interval` for Files. Encrypting the connections DB password is a follow-up issue, noted (§6.2) |
| 12 | FUSE path usage | Resolver maps `/mnt/<type>_<name>` to the cloud remote; never passes fuse paths; mergerfs per-branch free warning (§3.6, §6.2) |
| 13 | Restore/browse arbitrary root I/O | Allowed/denied roots + EvalSymlinks in engine and core for browse/restore; single-use download tokens; re-auth dropped unless endpoint exists (§6.2, §11, §18) |
| 14 | Recovery key design | One design fixed for v1.1: passphrase ≥128 bits, 0600 path, never on dest, audited, honest restore text (§18) |
| 15 | USB identity | UUID + serial + size, ambiguous → error, pre-existing mounts go to catch-up not attach, labels display-only (§4, §6.2, §7.2) |
| 16 | Queue: no pause, restore lock, one cron, DST | No pause (cancel+re-run), restore locks source of containing jobs, one shared clock, DST tests (§5, §7.1, §8.1, §16.1) |
| 17 | restic on small ARM | v1.1: RAM warning, cache cap, stale-lock rule (§18) |
| 18 | Hooks downtime, VM no ACPI, DB heuristic | Downtime shown in Review+notification, `vm_shutdown_timeout` with cause and never force-off, per-app override (§8.2) |
| 19 | Two schedulers one UI | Shared ScheduleBuilder + server cron preview used by Schedules; 149 keys added (§12.10) |
| 20 | Dead rclone.service | Removed with manifest cleanup (§15) |
| 21 | Error code enum | One Go enum with classes, generated JS, test for parity and i18n completeness (§5, §12.12) |
| B-table | A vs B contradictions | Adopted every "Pick" from the critique table: restic v1.1; delete-entries migration + copy + UNIQUE; move→copy; runs with guards; meta row marker; `volume:mounted`; `/locations` with typed ids; three guards; critique's status set; no pause; stale 2×/min 2 d; logs path/limits; passphrase key; after_job v1.1; generalised folder picker; no per-job bandwidth; revision 409 |

Quality checklist applied to every file this work touches:
- no hard-coded box paths (`/DATA/tower`)
- no shell
- every string goes through `$t`, with the key present
- every dialog is a window
- theme tokens only
- keyboard access
- AA contrast in both themes
- tests for each bug fixed
- works from a fresh `install.sh` on another machine
