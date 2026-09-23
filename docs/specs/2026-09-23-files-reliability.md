# Files reliability rebuild

Status: done (2026-09-23) - phases 1-5 shipped, see section 4
Owner complaint: copies report "complete" while files are missing; pastes
sometimes never start; top bar actions unreliable across browsers/devices;
companion and cloud transfers untrustworthy.

## 1. Proven root causes (audit, file:line)

Transfer engine (`services/core`)
- `pkg/utils/file/file.go:357` `CopyDirCtx` prints per-file errors and returns
  nil. Reproduced by `copy_repro_test.go`. The job is then marked finished.
- `service/file.go:323` cross-drive **move** runs `os.RemoveAll(source)` after
  that "successful" copy, deleting files that never copied (data loss). Same
  shape for companion moves (`DeleteCompanionPath`).
- `service/notify.go:139,205,215` two completion detectors (one guesses from
  `ProcessedSize >= TotalSize`) both call `OpStrArrPopFront()` blindly; a
  double pop drops the next queued job forever ("paste never started").
- `service/file.go` worker and 400 ms poller each store their own copy of
  the job whose `Item` slice shares one backing array (data race), a new
  unawaited notify goroutine is spawned every poll.
- `CopyFile`/`CopySingleFile` ignore `Close()` errors, never fsync, write in
  place (a failure leaves a truncated file under the real name).
- `ComputeOperateSizes` zeroes an item's size on any walk error.
- One global FIFO; only the front job runs; no per-item result anywhere.

Frontend (`ui/src/apps/files`)
- Event payload has no per-item failure field: "finished" always renders as
  success.
- Paste gives no feedback for ~3 s and has no in-flight guard; users click
  again, creating duplicate overwrite jobs.
- Two progress widgets (`OperationTray`, `FileOperationStatus`) with
  different math/lifetimes; the global one shows negative % while sizing.
- No `.catch` on paste/drag-drop/delete/rename failures; delete and drag-drop
  refresh with blind 0/400/1200 ms timers.
- Clipboard is per-tab Vuex memory: can't cross tabs, browsers or devices.
- Downloads use a hidden iframe with no success/failure signal; multi-file
  zip URL carries every path in the query string.

Uploads / downloads
- Chunks are written under their final name and assembled when
  `ReadDir` count == total, with no lock: a still-being-written chunk can be
  spliced in (truncated upload). Staging dir keyed by filename only (two
  same-named uploads collide). `testChunks:false` disables resume.
- Folder drag-drop bypasses the uploader's directory traversal: folders are
  silently lost. No folder picker.
- Zip downloads keep streaming after per-file errors (silent partial zip).

Companion / cloud / shares
- Companion listing/download silently fall back to stale NAS-side copies
  (HTTP 200) when the phone is offline.
- Cloud (rclone VFS cache "full"): the copy "finishes" when data reaches the
  local cache; the real upload happens later and its failures are never seen.
- CIFS mounts are hard mounts (hang forever when the server drops).

## 2. Design

### 2.1 Transfer engine (new `service/transfer`)
One job manager owns all copy / move / delete jobs. Nothing else mutates
job state; readers get snapshots.

Job model
- `id, type(copy|move|delete), sources[], dest, conflict(overwrite|skip|rename)`
- `state`: queued → scanning → running → (syncing) → done | done_with_errors
  | failed | cancelled | interrupted
- counters: `files_total/done/failed/skipped`, `bytes_total/done`, `speed`,
  `current`, timestamps, `failures[] {path, error}` (capped list + count).

Execution
1. **Scan**: walk every source into a plan (dirs, files, symlinks, sizes).
   Unreadable entries become recorded failures, never zero-size.
2. **Copy each file**: write to `name.nvtmp-<job>` in the destination dir,
   `fsync`, check `Close()`, verify written size == source size, then atomic
   rename to the final name; preserve mode + mtime; recreate symlinks.
   Conflicts resolved per policy (rename = "name (2).ext").
3. **Move**: same filesystem → one `os.Rename` of the top item (atomic).
   Otherwise copy as above and delete **only files that verified**, then
   remove source dirs only if empty. A failed file stays in the source.
4. **Delete**: jobs too, with per-entry errors and progress.
5. **Cloud sync phase**: if the destination is an rclone mount, after copying
   poll rclone `vfs/stats` until its upload queue drains; upload errors fail
   the job instead of vanishing.
6. **Concurrency**: jobs run in parallel up to a small limit (2) instead of a
   single global FIFO; within a job files are sequential (disk-friendly).
7. **Storage backends** behind one small interface (stat/list/open/create/
   mkdir/remove/rename): local + companion. Companion copies stream through
   it (no staging) and report real errors.

Events + API
- One publisher goroutine: publishes on every state change and at most every
  500 ms for progress. Payload keeps the legacy fields the UI/mobile read
  (`id,to,type,finished,cancelled,processed_size,total_size,speed,status`)
  plus the new ones (`state, files_*, failures, affected_dirs`).
- `GET /v1/batch/tasks` (snapshot incl. recent history) so a client that
  missed events (reconnect, new tab, another device) resyncs.
- `POST /v1/batch/task` returns the job id + initial snapshot (was: nothing).
- `POST /v1/batch/:id/retry` re-runs only the failed/unfinished items.
- Legacy endpoints (`/v1/batch/task`, `/v1/file/copy|move`, cancel) map onto
  the engine unchanged in shape - the mobile app keeps working.
- Recent jobs persisted to disk; after a restart unfinished jobs show as
  `interrupted` with Retry, never as silently done.

### 2.2 Frontend
- **One transfer store** (Vuex module) = single source of truth: seeded from
  `GET /v1/batch/tasks`, updated by socket events, re-polled on reconnect and
  while jobs are active and the socket is quiet.
- **One Transfers panel** in the desktop shell replacing both trays: live
  progress, "N failed" with the list, Retry, Cancel, Show in folder,
  auto-collapse of clean successes; failures stay until dismissed.
- **Paste**: immediate "Queued" card from the POST response, button busy
  while in flight, identical paste within a few seconds is deduped. Every API
  failure surfaces a toast. Drag-drop uses the same code path.
- Listings refresh from `affected_dirs` of finished jobs (targeted, no blind
  timers).
- **Clipboard** shared across tabs (BroadcastChannel/localStorage) and across
  devices (per-user server storage), with a visible "N items on clipboard"
  pill.

### 2.3 Uploads / downloads
- Chunks: write to temp then rename; staging keyed by the uploader's unique
  identifier; per-upload lock for assembly; verify assembled size; path
  sanitising of `relativePath`; resume via `testChunks`.
- Folder upload: folder picker (`webkitdirectory`) + drag-drop traversal
  preserving structure.
- Downloads: POST a download ticket (no giant URLs), real `<a download>`
  links, and a readable error manifest inside zips when files fail.

### 2.4 Companion / cloud / shares
- Offline companion → explicit "device offline" error (UI banner), backups
  only when the user asks for them.
- CIFS mounts get `soft` + timeouts so a vanished share errors instead of
  hanging.

## 3. Delivery phases (each tested, committed, deployed)
1. Transfer engine + API + legacy compatibility + tests (unit + real disk).
2. Frontend transfer store/panel, paste/drag-drop/delete flows, clipboard.
3. Uploads (atomic, resumable, folders) and downloads.
4. Companion streaming + offline errors, cloud sync phase, CIFS options.
5. Toolbar/UX polish; end-to-end runs (large files, 10k small files,
   cross-drive move with failures, two browsers at once).

## 4. Outcome

| Phase | Commit | Verified |
|---|---|---|
| 1 Engine | cdc0cf9 | 3,001 files / 594 MB copy checksum-identical; cross-drive move with a blocked file kept it at the source; retry completed it |
| 2 Frontend | 1f3a34f | blocked file reported with reason; triple paste = one job; fresh tab resyncs from history; cut/move/delete refresh listings |
| 3 Uploads/downloads | 6c29747 | 50 MB upload as 7 parallel reversed chunks + duplicate byte-identical; resume checks; folders kept; zip names + in-zip error report |
| 4 Cloud/companion | 12c1fa5 | 30 MB copy to Google Drive held in "syncing" for the real 29 s upload; object size verified on Google |
| 5 UX | (this commit) | Replace / Keep both / Skip dialog before overwriting; copy/cut confirmations |

Also found and fixed along the way: every v2 GET without Content-Type
panicked (router validator); the old upload service could panic the core
service; cross-drive rename copied into newname/oldname then deleted the
original; zip downloads were always named "batch".

Known limits / follow-ups
- A companion single-file download still falls back to the server backup
  copy when the phone is unreachable (listings are now explicit about it).
- Deleting still removes permanently; a Trash would add a safety net.
- The mobile app's own copy/paste screen doesn't show engine job progress
  yet (it can now: POST /v1/batch/task returns the job id).
