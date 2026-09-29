# Phone backup: device API contract (WP2-1)

Status: implemented in `services/backup` (jobs/phone_*.go). This is the
contract the phone app (WP2-2 onwards) codes against. Machine-readable
examples of every route are in `docs/specs/backup-api.json` (ids
`device_*`); `jobs/contract_test.go` checks them against the Go types.
Mobile plan: `docs/specs/2026-09-24-mobile-app-plan.md` §5.3-§5.4, §5.10,
§7.

All paths are under `/v1/backup` (through the gateway; the service also
answers without the prefix). Every JSON answer is the usual envelope
`{success, message, data}`; errors carry `data.error_code` (see
`ui/src/apps/backup/errorCodes.js`), `detail` and, for `validation`,
`field_errors`.

## 1. Auth

- Enrol once as the owner: `POST /devices {name, platform}` (admin JWT)
  answers `{device, token}`. The token (`nvd_` + 43 base64url characters,
  256 bits) is shown only then. Store it in the Android Keystore.
- Every device call: `Authorization: Bearer <token>`. Never `?token=`.
- The token opens only `/devices/<own id>/*`. Another device's id, or any
  other route, is `401 unauthorized`.
- Rotation: any answer may carry `X-NivaroOS-Device-Token` (the new token)
  and `X-NivaroOS-Device-Token-Old-Valid-Until` (RFC 3339). Store the new
  one at once; the old one works for 24 h more, and every request with it
  in that window is told the same new token again, so parallel uploads are
  safe.
- `401` on a device route means revoked, removed or replaced: stop, show
  "Link this phone again" (the owner uses `POST /devices/:id/token` or
  enrols anew). Never retry with the same token in a loop.
- Routes marked "device or owner" also take the owner's admin JWT: a phone
  restoring *another* phone's backup signs in as the owner once and reads
  that device's routes with the JWT. Upload and session routes take the
  device token only.

## 2. Limits (from `GET /devices/:id/config`, `limits`)

| field | value | meaning |
|---|---|---|
| `chunk_size` | 8 MiB | largest PATCH body |
| `max_upload_size` | 64 GiB | largest file |
| `check_batch` | 500 | items per check |
| `deleted_batch` | 1000 | paths per deleted |
| `item_keys_batch` | 1000 | keys per check-items |
| `max_open_uploads` | 64 | unfinished uploads per phone (429 above) |
| `max_open_sessions` | 8 | open sessions per phone |
| `max_finish_errors` | 200 | errors kept per finish |
| `session_idle_sec` | 1800 | a session idle this long ends as interrupted |
| `upload_expiry_sec` | 604800 | an unfinished upload untouched this long is dropped |
| `check_per_minute` | 240 | check calls per minute (429 above) |

Read them from config; don't hard-code them.

## 3. Categories

| category | kind | stored as | notes |
|---|---|---|---|
| `media` | files | `media/<path>` | photos, videos, audio (MediaStore); path = RELATIVE_PATH + DISPLAY_NAME, e.g. `DCIM/Camera/PXL_1.jpg` |
| `files` | files | `files/<path>` | folders picked with SAF; path = `<tree label>/<relative path>` |
| `apks` | files | `apps/apk/<path>` | path = `<package>/<versionCode>/base.apk` (and `split_*.apk`) |
| `contacts` | export | `contacts/contacts-<ts>.vcf` | one vCard 3.0/4.0 file with every contact |
| `calendar` | export, named | `calendar/<name>/calendar-<ts>.ics` | one iCalendar file per calendar; `name` = the calendar's display name |
| `sms` | export, incremental | `messages/sms-<ts>.xml` | SMS Backup & Restore XML, new items only |
| `calllog` | export, incremental | `calllog/calls-<ts>.xml` | SMS Backup & Restore call XML, new items only |
| `apps` | export | `apps/apps-<ts>.json` | JSON array (or `{"apps": [...]}`) of installed apps |
| `settings` | export | `settings/settings-<ts>.json` | readable device settings, JSON |

`<ts>` is UTC `20060102T150405Z`. Paths are relative, `/`-separated,
valid UTF-8, at most 4096 bytes, each segment at most 255 bytes, no empty,
`.` or `..` segment, no NUL, backslash or control character, no leading
`/`. Anything else is `invalid` (check) or `400` (upload).

Files kinds keep a per-file manifest (path, size, mtime, SHA-256) and the
older contents of a path as versions. Export kinds keep one file per run;
a full export equal (same SHA-256) to the newest one is not stored again
(`unchanged`).

## 4. A backup, step by step

1. `GET /devices/:id/config` - check `destination.error_code`: empty means
   backups can run. `dest_offline` = the drive holding this phone's
   backups is not connected: show "Backup drive not connected" and retry
   later (never an error loop). `dest_marker_mismatch`,
   `path_not_allowed`: the owner must fix the location in the web UI.
2. `PUT /devices/:id/phone-settings` (when the phone's own settings
   changed): `{app_version, os_version, model, paused, categories:
   {media: {enabled, schedule, conditions, encrypted}}}`. Display only.
3. `POST /devices/:id/sessions {categories, reason, expect_bytes}` -
   `201` a new session, or `200` with `resumed: true` when a session with
   the same categories is still open (after the app was killed). Keep
   `id`. Refusals: `409 dest_offline`, `409 dest_marker_mismatch`,
   `507 no_space` (expect_bytes + 5 % of the drive must be free),
   `409 invalid_state` (moving, too many sessions).
4. Files categories, per batch of up to 500:
   `POST /devices/:id/sessions/:sid/check {category, items: [{path, size,
   mtime, sha256}]}`. `mtime` is unix **milliseconds**; `sha256` lowercase
   hex, or `""` when not hashed yet (cheap first pass). Answers, per item
   in the same order:
   - `have` - nothing to do;
   - `need_hash` - hash it and check it again (unknown path, or size or
     mtime changed since the backup);
   - `need` - upload it; `upload_id` + `offset` when an unfinished upload
     of exactly this file exists (resume it: HEAD, then PATCH);
   - `have_elsewhere` - the same content was already backed up under
     another path (a moved photo); the server placed it, nothing to send;
   - `excluded` - the owner deleted this content from the backup; never
     upload it (keep a local "excluded" mark until `GET .../excluded`
     no longer lists it);
   - `invalid` - the path breaks the rules above; report it at finish.
5. Upload every `need` (below). Parallel uploads are fine (the server
   locks per upload); 2-4 at a time is plenty.
6. Files that disappeared from the phone since the last backup:
   `POST /devices/:id/sessions/:sid/deleted {category, paths}`. They stay
   in the backup, marked `deleted_on_device_at`, and are offered by
   restore. A path that appears again clears its mark at the next check.
7. Export categories: build the export, upload it with `name` (calendar)
   or without. For sms/calllog first ask which items are new (section 7).
8. `POST /devices/:id/sessions/:sid/finish {status, errors}` - status
   `success`, `partial` (some files failed; list them in `errors: [{category,
   path, message}]`), `failed` or `cancelled`. success/partial make a
   snapshot (`snapshot` in the answer); `pending_uploads` counts uploads of
   this session left unfinished (resumable for 7 days from a new session).

A session that sees no request for 30 minutes ends as `interrupted`; any
later call on it answers `409 session_closed`. Start a new session; the
unfinished uploads resume in it (the create below finds them).

The owner can cancel a session from the web UI (it is a run of kind
`device` in Activity): calls then answer `409 session_closed`.

## 5. Uploads (tus 1.0 subset)

Core protocol plus creation and termination; no other extensions.

**Create** `POST /devices/:id/uploads`, no body, headers:

- `Tus-Resumable: 1.0.0`
- `Upload-Length: <bytes>`
- `Upload-Metadata: key base64(value),key base64(value),...` with
  - `session` (required) - the open session id,
  - `category` (required, one of the session's),
  - `path` (files kinds) or `name` (calendar only; none for other exports),
  - `sha256` (required) - of the whole file, lowercase hex,
  - `mtime` - unix ms (files kinds: applied to the stored file),
  - `taken_at` - unix ms (media: DATE_TAKEN), `media_id` - MediaStore _ID,
  - `encrypted` - `1` when the content is client-side encrypted (WP2-8).

Answers (envelope `UploadCreated {upload_id, location, offset, length,
expires_at, status}`):

- `201`, status `created`, headers `Location` and `Upload-Offset: 0`;
- `200`, status `resumed` - an unfinished upload of the same file (same
  category, path/name, sha256 and length) exists: continue at `offset`;
- `200`, status `have` - already stored with this content; nothing to send;
- `200`, status `excluded` - see check; nothing to send;
- `201`, status `stored` - a 0-byte file, stored at once (no upload id);
- `507 no_space` (this file plus every other unfinished upload plus the 5 %
  reserve), `413 too_large` (> 64 GiB), `429` (64 unfinished uploads),
  `409 session_closed`, `409 dest_offline`, `400 validation`.

**Offset** `HEAD /devices/:id/uploads/:uid` - `Upload-Offset`,
`Upload-Length`, no body; `404` when unknown, finished, expired, or void
because the owner changed this phone's backup location (start over with a
new create).

**Data** `PATCH /devices/:id/uploads/:uid`, headers `Tus-Resumable`,
`Content-Type: application/offset+octet-stream`, `Upload-Offset: <current
offset>`, body at most `chunk_size` (8 MiB) and never past
`Upload-Length`:

- `204` + `Upload-Offset` - chunk stored (fsynced);
- on the last chunk the server checks the SHA-256 and places the file;
  the `204` then carries `X-NivaroOS-Upload-Result`:
  `stored` | `unchanged` (a full export equal to the newest one) |
  `no_new_items` (an sms/calllog export whose items are all known);
- `409 offset_mismatch` + the right `Upload-Offset` header - continue
  from there;
- `422 checksum_mismatch` - the bytes don't match `sha256`; the upload is
  gone; re-read the file and create a new upload;
- `413 too_large` - body over 8 MiB or past the length (nothing stored);
- `400 io_error` + `Upload-Offset` - the body was cut short; what arrived
  is kept, continue from the offset;
- `409 invalid_state` - another PATCH of this upload is still running;
- `409 session_closed` - the upload's session ended: start a session and
  create again (answers `resumed`);
- `409 dest_offline` - the drive went away; retry later.

**Abandon** `DELETE /devices/:id/uploads/:uid` - `204`.

Resume after a lost answer or a crash: HEAD (or the create/check answer's
offset) says where to continue; the server always keeps exactly what it
confirmed. Uploads untouched for 7 days are dropped.

## 6. Where the files go, and a missing drive

Each phone has one folder: `<location>/<folder>`, by default
`/DATA/Backup/<phone name>` (sanitised, unique, `device.folder`). The owner
may pick another folder or drive per phone in the web UI (the app may
offer the same through the owner's JWT, `POST /devices/:id/destination`).
The phone never sends paths on the server; it only sees
`destination.path` for display.

When the drive holding the folder is not connected the server refuses
(`409 dest_offline`) - it never falls back to another disk. The app
shows it as a waiting state, not a failure, and retries on its schedule.

When the owner changes the location, unfinished uploads become void
(HEAD 404): create them again. With "move", backups keep their history;
with "start fresh", the next check answers `need` for everything.

## 7. Messages and call log (incremental)

Format: SMS Backup & Restore XML
(`<smses count=".."><sms .../><mms ...>...</mms></smses>`,
`<calls count=".."><call .../></calls>`), attributes as that app writes
them; `date` in unix ms.

Item key = lowercase hex SHA-256 of the UTF-8 string:

| element | string |
|---|---|
| `sms` | `"sms|" + address + "|" + date + "|" + type + "|" + body` |
| `mms` | `"mms|" + address + "|" + date + "|" + msg_box + "|" + m_id` |
| `call` | `"call|" + number + "|" + date + "|" + duration` |

Values exactly as they will appear in the XML attributes (after XML
unescaping; `body` is the message text). Go reference: `jobs.ItemKey`.

Flow per session: compute the keys of the phone's messages,
`POST /devices/:id/sessions/:sid/check-items {category: "sms", keys}` in
batches of 1000, export only the `new` ones into one XML file, upload it.
The server parses the file, records its keys, and stores it only when at
least one item is new (`no_new_items` otherwise). Encrypted exports
(`encrypted=1`) can't be parsed: the server stores them as they are and
records no keys, so for an encrypted category the phone keeps its own list
of keys it has sent.

Restore: `GET /devices/:id/exports/full?category=sms&snapshot=<id|latest>`
streams one merged XML with every item once, oldest export first. The
owner can import an SMS Backup & Restore file made elsewhere
(`POST /devices/:id/exports/import?category=sms`, admin).

## 8. Restore and browse (device or owner)

- `GET /devices/:id/snapshots` - finished sessions, newest first.
- `GET /devices/:id/snapshots/:snap/browse?category=media&path=DCIM&include_deleted=1`
  - `:snap` = snapshot id or `latest` (now, including an open session's
  uploads). Folders first, then files as they were then (`version: true`
  = an older content than the current one).
- `GET /devices/:id/restore-manifest?snapshot=&category=&after=&limit=`
  - every file of the snapshot (also deleted-on-phone ones, with
  `deleted_on_device_at`), ordered by category then path, each with
  `content` (a GET path under `/v1/backup`); the first page of an
  all-category listing also has `exports`. Continue with
  `?after=<next_after>` until `next_after` is `""`. The phone compares
  size/sha256 with what it has and fetches only what is missing.
- `GET /devices/:id/files/content?category=&path=&snapshot=` - the bytes;
  `Range` works (206) for resumable downloads; `ETag` /
  `X-NivaroOS-SHA256` and `X-NivaroOS-MTime` (unix ms) headers.
- `GET /devices/:id/exports?category=&snapshot=` (`all=1`: every export)
  and `GET /devices/:id/exports/:eid/content`.
- `GET /devices/:id/excluded` - hashes never to upload.
- `POST /devices/:id/verify {deep}` - check the stored files; damaged ones
  are answered `need` at the next check.

## 9. Events and the web UI

`nivaroos:backup:device-changed {device_id, change}` on the message bus
(change: enrolled, session_started, session_ended, settings,
phone_settings, destination, moved, move_failed, revoked, relinked,
removed, verified, files_removed, imported). Sessions also emit the run
events (`run-begin`, `run-end`) with `kind: device` and `job_id` = the
device id. A phone that hasn't finished a backup for `stale_days` (owner
setting, default 7) gets a `backup.notify.device_stale` notification once
a day.

## 10. Server design notes (for maintainers)

- A session is a `RunRow` with `JobID` = device id, `Kind` device,
  `Trigger` device; restarts don't end it (`recoverRuns` skips it), the
  30-minute idle sweep does; `POST /runs/:id/cancel` ends it as cancelled.
- The location is an engine endpoint pinned at enrolment (the endpoint
  `/DATA/Backup` resolves to) or picked by the owner. `Engine.Resolve`
  returns `LocalPath` (symlink-free, inside the allowed roots) only for an
  online local endpoint; the job side writes only below it. The folder's
  st_dev is cached for 30 s and re-checked before every write; the folder
  holds `.nivaroos-device.json` (`device_id`), another id is
  `dest_marker_mismatch`. Reserved folders: `.nivaro-versions/<ts>/
  <category>/<path>` (replaced contents), `.nivaro-uploads/<id>.part`.
- Index tables (gorm AutoMigrate, additive): phone_session_rows,
  phone_file_rows, phone_version_rows, phone_export_rows,
  phone_snapshot_rows, phone_upload_rows, phone_item_rows,
  phone_excluded_rows; new DeviceRow columns for the location, settings,
  move state and last verify.
- A snapshot is a point in time. What a path held at T: the current row
  when `content_since <= T`, else the version with `since <= T < until`.
  Stamps are UTC and strictly increasing within the process.
- Retention: keep the newest `keep_last` snapshots and the newest of each
  of the last `keep_days` days; a version goes when no kept snapshot falls
  in `[since, until)`; a full export goes when it is not the newest and no
  kept snapshot falls in its time in effect; incremental exports are never
  pruned; `deleted_purge_days` removes files deleted on the phone.
  Nothing is pruned while a session is open.
- Changing the location: refused while a session is open, when the new
  folder is inside the current one or holds it, overlaps another phone or
  a job destination, or exists non-empty. Move = rename on the same
  filesystem, else a background copy with a `.nivaro-moving` sentinel;
  the switch happens after the copy, the old folder is removed last; a
  restart mid-copy removes the half copy and marks the move failed.
  Fresh = switch and drop the index (the excluded list stays).
- Auth routing: under `/devices/{id}/`, a bearer shaped like a device
  token goes to the device mux; otherwise a route the owner mux knows goes
  through the JWT path; anything else answers 401.
