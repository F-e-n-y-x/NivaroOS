# RESUME: phone backup, server + web half (WP2-1 / WP2-11)

Stopped early because the machine was rebooted. Nothing is committed, built,
tested or deployed. Worktree base: origin/master 51130b1. Delete this file
before committing.

## Done (uncommitted, not compiled yet)
- `services/backup/engine/api.go` + `resolve.go`: new additive field
  `Resolved.LocalPath` (`json:"local_path,omitempty"`). It is the
  symlink-free absolute path of an online local endpoint, set in
  `Engine.Resolve`. It is the only path the jobs side writes phone uploads to.
- `services/backup/jobs/phone_model.go`:
  - categories: media, files, apks (file kind, per-file manifest); contacts,
    calendar(named), sms, calllog (incremental), apps, settings (export kind)
  - folder layout, reserved names, limits: 8 MiB chunk, 64 GiB max upload,
    check batch 500, sessions idle 30 min, uploads expire after 7 days,
    5 % free-space reserve
  - `DeviceSettings` (keep_last 10, keep_days 30, deleted_purge_days 0,
    stale_days 7) and `PhoneSettings` (as reported by the phone)
  - gorm tables `PhoneSessionRow`, `PhoneFileRow`, `PhoneVersionRow`,
    `PhoneExportRow`, `PhoneSnapshotRow`, `PhoneUploadRow`, `PhoneItemRow`,
    `PhoneExcludedRow` (`phoneTables()`)
- `services/backup/jobs/phone_types.go`: every API request/response type,
  with the design written into its comments.

## Design decisions (keep them)
- A session is a RunRow with JobID = device id, Kind "device" (new
  `KindDevice`, add it to RunKinds and the i18n `backup.kind.device`) and
  Trigger "device" (new `RunByDevice`). `toRun` must fill job_name with the
  device name. `recoverRuns` must skip kind device, so a session survives a
  restart and the 30-min idle sweep closes it as interrupted.
  `handleRunCancel` on a device run closes the session as cancelled.
- Destination = Endpoint of the folder that holds the phone's folder. The
  device root is `<LocalPath>/<DeviceRow.Folder>`. Default = ResolvePath of
  `deviceRoot()` (/DATA/Backup), pinned lazily and stored. Offline, or no
  LocalPath, gives 409 `dest_offline`, with no MkdirAll and no fallback. Cache
  the root and st_dev for 30 s, and re-check st_dev before every write. Write
  the marker `.nivaroos-device.json` {device_id}; a mismatch gives
  `dest_marker_mismatch`.
- New DeviceRow columns: Dest (JSON Endpoint), DestDefault, DestGen, DestPath
  (last known), RevokedAt, Settings, PhoneSettings, StaleNotifiedAt,
  MoveState/MoveError/MoveTarget/MoveTargetPath.
- Changing the location: POST /devices/:id/destination {location|null,
  mode move|fresh}.
  - Refused while a session is open, when the target is inside or contains
    the current root or another device's root, or when the target is not
    empty.
  - move: os.Rename on the same filesystem. Otherwise copy in the background
    (MoveState moving, a `.nivaro-moving` sentinel in the target), then
    switch Dest, DestGen++, and RemoveAll the old root. After a restart,
    clean up a half-done move and mark it failed.
  - fresh: switch, then drop the index rows (keep the excluded list). Old
    files stay on disk.
  - Uploads are voided in both modes.
- Rename (PUT /devices/:id name) renames the folder under the same location.
  It is refused while a session is open, or when offline and backups exist.
- tus 1.0 subset:
  - POST /devices/:id/uploads with Upload-Length and Upload-Metadata
    (session, category, path|name, sha256, mtime ms, taken_at, media_id,
    encrypted), answering 201 + Location + envelope UploadCreated. A repeat
    create for the same file resumes. The free-space check (length +
    reserve + other open uploads) gives 507 no_space.
  - HEAD returns Upload-Offset/Length.
  - PATCH (application/offset+octet-stream, Upload-Offset = part file size,
    otherwise 409 offset_mismatch; body ≤ 8 MiB, otherwise 413 too_large)
    appends and fsyncs. The running SHA-256 state is stored in HashState. The
    last chunk verifies the hash (422 checksum_mismatch deletes the upload),
    then places the file: the old content goes to
    .nivaro-versions/<ts>/, a rename, Chtimes, and an upsert of PhoneFileRow.
    It answers 204 + `X-NivaroOS-Upload-Result`.
  - DELETE abandons the upload.
  - Upload lock is per upload (TryLock → 409). Placing a file takes the
    per-device lock.
- check: have / have_elsewhere (hard link, or copy, from any row or version
  with the same sha) / need / need_hash (no sha and size+mtime differ) /
  excluded / invalid. A damaged row counts as need. A reappearing path
  clears its deleted mark. Rate limit 240/min per device.
- Exports: the sha equal to the newest export of (category, name) is not
  stored again. For sms/calllog the server parses the SMS Backup & Restore
  XML (encoding/xml streaming) and records item keys.
  - sms key: sha256 hex of `address|date|type|body`
  - mms key: `mms|address|date|msg_box|m_id`
  - call key: `number|date|duration`
  - An export with 0 new items is not stored.
  - GET exports/full streams one merged, de-duplicated XML.
  - POST exports/import (admin, raw XML, ≤ 1 GiB) imports a file made
    elsewhere.
- Snapshot = finish of a success or partial session (PhoneSnapshotRow).
  Browsing at T is a temporal query:
  - file rows with ContentSince ≤ T, or version rows with
    Since ≤ T < Until; the deleted mark ≤ T is hidden unless
    include_deleted;
  - full exports: the newest per name with TakenAt ≤ T; incremental: all
    with TakenAt ≤ T.
- Retention: keep the newest keep_last snapshots, plus the newest snapshot
  of each of the last keep_days days.
  - A version is pruned when no kept snapshot falls in [Since, Until).
  - A full export is pruned when it is not in effect at any kept snapshot
    and is not the newest.
  - Incremental exports are never pruned.
  - deleted_purge_days removes files deleted on the phone.
  - Upload purge, idle-session sweep and device stale notify
    (`backup.notify.device_stale`) run in maintenance or a 5-min loop.
- Auth routing in `Handler()` for /devices/{id}/* paths:
  - a bearer that `looksLikeDeviceToken` goes to serveDevice(devMux);
  - otherwise, when ownerMux.Handler(r) matches a pattern, the request goes
    through `authed` (the JWT path), and handlers requireAdmin;
  - anything else goes to serveDevice, which answers 401. This keeps
    TestDeviceTokenScope green: ping with a JWT must stay 401.
- Upload routes and session routes are device only. Browse, download,
  config reads, excluded GET and verify also accept an admin JWT. A phone
  restoring another phone's backup logs in once as admin.
- Revoke: POST /devices/:id/revoke clears the token hashes and keeps the data
  browsable. POST /devices/:id/token re-links a reinstalled phone with a new
  token. DELETE /devices/:id?purge_data= removes the device and its index,
  and with purge_data also its folder.
- Event `nivaroos:backup:device-changed` {device_id, change}. Add it to
  EventTypes, backup-api.json events, and `go generate ./jobs` (events.js).
- New API-class error codes: checksum_mismatch, offset_mismatch, too_large,
  session_closed. Each needs en_US backup.err.<code>.title/cause/fix.
- Device downloads for the web: POST /devices/:id/downloads →
  DownloadToken. Extend downloadGrant with a device variant served by
  GET /downloads/<token>.

## Endpoints to implement (all under /v1/backup)
- device token:
  - GET /devices/:id/config
  - PUT /devices/:id/phone-settings
  - POST /devices/:id/sessions
  - POST /sessions/:sid/check
  - POST /sessions/:sid/check-items
  - POST /sessions/:sid/deleted
  - POST /sessions/:sid/finish
  - POST, HEAD, PATCH, DELETE /devices/:id/uploads[/:uid]
- device or admin JWT:
  - GET /devices/:id/snapshots
  - GET /devices/:id/snapshots/:snap/browse?category=&path=&include_deleted=
    (snap may be `latest`)
  - GET /devices/:id/files/content?category=&path=&snapshot= (Range, via
    http.ServeContent)
  - GET /devices/:id/exports?category=&snapshot=
  - GET /devices/:id/exports/:eid/content
  - GET /devices/:id/exports/full?category=sms|calllog&snapshot=
  - GET /devices/:id/restore-manifest?snapshot=&category=&after=&limit=
  - GET /devices/:id/excluded
  - POST /devices/:id/verify
- admin JWT:
  - GET /devices/:id (DeviceDetail)
  - PUT /devices/:id
  - POST /devices/:id/destination
  - POST /devices/:id/revoke
  - POST /devices/:id/token
  - DELETE /devices/:id?purge_data=
  - POST /devices/:id/excluded
  - DELETE /devices/:id/files?category=&path=
  - POST /devices/:id/downloads
  - POST /devices/:id/exports/import?category=
- Extend the Device type additively: revoked_at, dest, dest_path, folder,
  last_backup_at, last_status, size_bytes, moving. Update the fixtures in
  backup-api.json to match (the strict re-encode checks them).

## Next steps
1. Planned files, still to write:
   - jobs/phone_dest.go
   - jobs/phone_upload.go
   - jobs/phone_session.go
   - jobs/phone_exports.go
   - jobs/phone_browse.go
   - jobs/phone_admin.go
2. Changes to existing files:
   - add phoneTables() to Store.migrate AutoMigrate
   - add the DeviceRow columns
   - add a `phone *phoneState` field (locks, root cache, move progress,
     check limiter) to Service, initialised in New
   - add routes: the owner ones in routes(), the device ones in
     deviceRoutes()
   - fix the ErrUnauthorized handling for revoked devices
3. Tests in jobs/phone_test.go:
   - fake engine ResolvePathFn/ResolveFn mapping RefIDs to temp dirs, with an
     "unplug" switch
   - session lifecycle; tus resume (partial PATCH, HEAD, offset 409); sha
     mismatch
   - incremental check (have / need / need_hash / have_elsewhere / excluded)
   - versions and retention; destination move (same fs and copy) and fresh
   - missing-drive refusal (no writes on the "system" dir)
   - auth scoping (other device's token 401, JWT on upload routes 401,
     viewer 403)
   - path attacks: `..`, absolute, NUL, 4 KiB, symlink
   - sms export keys and the full build
4. docs:
   - docs/specs/backup-api.json: device endpoints, "device_or_jwt" auth (and
     allow it in contract_test.go), the event
   - new docs/specs/2026-09-30-phone-backup-device-api.md: the precise
     contract for the app agent (headers, chunk size, resume semantics,
     manifest, per-category formats and item keys)
5. Web:
   - new nav section "phones" (windows.js BACKUP_SECTIONS, BackupNav
     ICONS) and sections/PhonesSection.vue: list, detail with per-category
     table, destination "Change location…"
     (storagePicker → folderPicker → move/fresh dialog), schedule as set by
     the phone, snapshot browser + download via /devices/:id/downloads,
     revoke/remove
   - client methods in ui/src/service/backup.js; the backup.spec.js
     "every route has a client method" covers non-device auth
   - en_US backup.phones.* keys
   - vitest for the pure logic (a phones.js helper)
6. Build and test: `cd services/backup && GOWORK=off go test ./...`; then
   `go generate ./jobs`; `cd ui && pnpm install --frozen-lockfile && pnpm
   vitest run src/apps/backup src/service`.
7. Commit on master (pull --rebase), push, then deploy with nivaroos-deploy.
   The backup unit is nivaroos-backup; for the web, run pnpm build, then
   `nivaroos-deploy --www <wt>/ui/build/sysroot/var/lib/nivaroos/www`.
   Check `free -g` first. The owner travels from 2026-10-01, so deploy only
   tested changes.
