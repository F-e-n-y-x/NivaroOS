# RESUME: cloud mount disk-cache settings (paused, nothing committed or deployed)

Worktree: /root/nivaroos-wip/wt-cache (detached at origin/master b8ddd16). Don't commit this file.
Build: PATH=/usr/local/go/bin:$PATH GOWORK=off.

## Live findings on this box (important)
- nivaroos-local-storage mounts **every** cloud account in-process (it holds both FUSE fds). Its unit has no HOME,
  so rclone's cache dir is **/tmp/rclone, which is tmpfs (RAM)**: 5.6 GB used, and a **5.9 GB pending upload**
  (vfsMeta/terabox_ayush_soni_terabox_1790698020/WIN11...ISO has "Dirty": true). That is RAM that can't be freed
  until the upload finishes, so it may have played a part in yesterday's OOM. Don't delete it.
- /root/.cache/rclone (116K, old test/terabox leftovers, all clean) and /var/cache/nivaroos/rclone (the rcd's
  --cache-dir; has an old google_drive dir) also exist. That makes **3 cache dirs**, not 2.
- rclone rcd (v1.61.1, rclone.service) has **0 mounts**. Core's CheckAndMountAll (route/init.go) tries it, but
  local-storage wins. Core's rc mount path does exist (services/core/service/storage.go, recover.go).
- Listing /mnt/google_drive_drive_1788637372 hung for more than 5s (a separate, existing problem).

## Done (uncommitted, not compiled or tested yet)
- services/local-storage/pkg/cloudcache/settings.go: Settings {Mode off|writes|full, MaxSize, MaxAge, Dir}. It
  covers defaults, Load/Store from local-storage.conf [cloud_cache] (keys Mode, MaxSize bytes, MaxAge seconds, Dir),
  WriteEnvFile(/etc/nivaroos/rclone-cache.env, RCLONE_CACHE_DIR=), Validate(env) with an injectable Env, MountFor
  and HumanBytes. Validate refuses: a bad mode, size <= 0 or >= free space (plus current cache use), an age outside
  1 min..365 d, a relative path, system folders, anything inside or containing a cloud or network mount, and
  tmpfs/ramfs. It checks the folder is writable. It warns about the system disk, a cache near the free-space
  limit, and mode off.
- services/local-storage/pkg/cloudcache/cache.go: Scan(dir, remote) (sparse disk usage, pending count and bytes
  from vfsMeta Dirty), Clear (removes only non-dirty items and orphan data; keeps dirty or unreadable metadata),
  Migrate(old, new) (moves dirty items with rename or an EXDEV-safe copy+fsync+rename, drops clean items, never
  deletes pending ones, removes the old dir only when empty) and Remotes(dir).

## Left
1. Tests: pkg/cloudcache/*_test.go (validation, Scan, Clear keeps dirty, Migrate same-fs/clash/no-space) using t.TempDir.
2. services/local-storage/service/cloud_cache.go:
   - At startup, before CheckAndMountAll (main.go ~line 107): load settings, `rconfig.SetCacheDir(s.Dir)` (1.75
     has config.SetCacheDir), and Migrate each of cloudcache.LegacyDirs into s.Dir. Nothing is mounted yet at that
     point. The ISO copy goes tmpfs->disk (~6 GB); consider running migrate+mount in a goroutine so Type=notify
     startup and nivaroos-deploy's 10s settle aren't blocked.
   - MountStorage: build vfsOpt from the settings (CacheMode, CacheMaxSize, CacheMaxAge).
   - Fix the race in storage.go's Wait goroutine: it deletes MountLists[mountPoint] unconditionally, which would
     drop a remount's new entry. Only delete if it is still the same mnt.
   - Apply(new): validate, then refuse if any account has pending uploads (live VFS.Stats diskCache
     uploadsInProgress+uploadsQueued, rcd vfs/stats, plus a disk Scan). Also refuse if files are open under a
     mount point (scan /proc/*/fd). Note pkg/mount's unmount shuts the VFS down BEFORE fuse.Unmount, so a busy
     unmount leaves a dead mount. Then save to config.Cfg and SaveTo, unmount all, SetCacheDir, Clear the old dir
     if the location changed, and remount. For rcd: write the env file, unmount/remount rcd mounts via the
     /var/run/rclone/rclone.sock rc (mount/listmounts, mount/unmount, mount/mount), and
     `systemctl restart rclone` when the dir changed.
   - ClearCache(name|all): refuse while that account has pending uploads, then unmount, Clear, remount.
   - Usage: per config section with mount_point, Scan(s.Dir, name) plus live stats.
3. Routes (local-storage owns /v1/cloud): GET /v1/cloud/cache, PUT /v1/cloud/cache (?dry_run=1 for validation
   only), POST /v1/cloud/cache/clear {name?}. Register them in route/v1.go next to the other v1CloudGroup routes.
4. Core: services/core/pkg/utils/httper/drive.go Mount() should read [cloud_cache] from
   /etc/nivaroos/local-storage.conf (go-ini is already in core's go.mod) for its vfsOpt JSON (CacheMaxAge in ns).
5. rclone.service (services/core/build/sysroot/...): `Environment=RCLONE_CACHE_DIR=/var/cache/nivaroos/rclone`,
   `EnvironmentFile=-/etc/nivaroos/rclone-cache.env`, `mkdir -p ${RCLONE_CACHE_DIR}`, `--cache-dir ${RCLONE_CACHE_DIR}`.
   install.sh already copies this unit (line ~1631). Install the unit on this box with daemon-reload.
6. UI: ui/src/service/cloud.js (cacheGet/cacheSave/cacheClear), a new Cache card in
   sections/OnlineAccountsSection.vue (plus a ROWS search entry), and a panel component with mode, size, age,
   location (reuse DsFolderPickerWindow via OPEN_WINDOW, as in download-station/DsSettingsPanel.vue browse()),
   per-account usage and pending, Clear buttons, and the copy "Keeps recently used parts of files on this server
   so videos seek instantly and uploads finish in the background". Put the logic in a .js module and add vitest.
7. go test, then pnpm test and build (check free -g first; don't run them together). Commit, push, and deploy
   local-storage (and core for drive.go) with nivaroos-deploy, then the web build.
- cache agent resume note: also handle uploads that can never finish (e.g. TeraBox 'more than allowed free plan limit (4GB)'): stop retrying, list as 'Upload can't finish' with Save to folder / Discard; cache dir already moved to /var/cache/nivaroos/rclone by 2232fd6+882a493

## Session 2 (2026-09-30, paused by owner before any code change; nothing committed/deployed)
Re-read everything; no code edits yet. Already on master: 2232fd6+882a493 (pkg/vfscachedir, Dir=/var/cache/nivaroos/rclone,
Setup() at main.go ~107 moves legacy dirs; cloudcache.LegacyDirs/Migrate overlap with it - reuse vfscachedir.Setup at start,
use cloudcache.Migrate only for user location changes). Design worked out against rclone v1.75.1 source:
- Reach the private vfs.cache (*vfscache.Cache) of mnt.VFS via reflect+unsafe (field "cache"); guard nil + add a test with a
  real vfs.New on a local temp remote so an rclone bump that renames it fails the test. Gives Queue(), QueueSetExpiry(),
  InUse(), DirtyItem(), Remove().
- Clear cache WITHOUT unmount: walk vfsMeta/<remote>; for clean (Dirty=false) items that are !InUse and DirtyItem()==nil ->
  cache.Remove(name); unmounted accounts -> cloudcache.Clear. Never touch dirty/unreadable meta.
- Upload failures: writeback.upload retries forever (delay doubles to 5 min) and logs
  fs.Errorf(name, "vfs cache: failed to upload try #%d, will retry in %v: %v") WITH wb.mu HELD. Wrap fs/log.Handler via
  fs.SetLogger(wrapper) to record lastErr[name] (attr "object") and signal a goroutine (never call writeback from the
  handler: deadlock). Goroutine: per mount Queue(); item !Uploading with permanent reason (TeraBox "free plan limit",
  400/403(not rateLimit)/413/422, quota/too large phrases; NOT 401/408/429/5xx) or tries>=10 -> park with
  QueueSetExpiry(id, now+50y) and persist {remote,name,size,reason,tries,since} in a JSON file
  (e.g. /var/lib/nivaroos/local-storage/cloud-stuck.json). After each MountStorage (reload re-queues dirty items ~5s later)
  re-park persisted ones immediately.
- Retry = drop from stuck list, QueueSetExpiry(id, now), count tries from that point. Discard = vfs.Stat(name): if File and
  DirEntry()==nil (never uploaded) -> File.Remove(); else cache.Remove(name) only (File.Remove would delete the remote
  original). Save to folder = copy vfs/<remote>/<name> to chosen local folder (not in a cloud mount), sha256-verify vs source
  and size==meta Size, then Discard.
- Pending guard for settings/location change: refuse when non-parked uploads are queued/in progress; parked (stuck) ones are
  safe (on disk, Migrate moves dirty items, re-parked on remount). Fix the Wait-goroutine race in storage.go (delete
  MountLists[mp] only if still the same mnt).
- Core drive.go: rcd has 0 mounts on this box; still make its vfsOpt read [cloud_cache] (point 4 above) - keep minimal,
  another agent works in services/core (pull before commit).
Everything in "Left" 1-7 above is still to do.
