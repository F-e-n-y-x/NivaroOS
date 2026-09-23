# Settings app quality rebuild

Status: done (2026-09-24) - all 6 phases, see section 5
Owner ask: check the Settings app fully, from every angle — function, UI/UX,
contrast — quality work, not quick fixes.

## 0. How it was checked

- Five read-only code+live audits (storage/network, system/updates/packages,
  containers/schedules, users/remote/companion/cloud, shell/appearance/design
  system): every action traced UI → client → gateway → handler → effect.
  Live checks were GET-only.
- Runtime audit of the production bundle (same-origin harness, all writes
  blocked at the browser): 11 sections × dark/light × desktop/tablet/phone —
  axe-core WCAG 2.1 AA, overflow, console errors, failed/slow API calls,
  idle polling, memory growth, screenshots.
- A compositing contrast scan (axe skips text on translucent layers, which is
  exactly where this UI's dark-on-dark hides) over every section and tab.
- The critical claims below were re-read in the code before being listed.

Limit: local-storage and user-service endpoints need a real login token
(loopback isn't exempt there), so Storage/Cloud/Users data was reviewed from
code, not live.

## 1. Tonight's mitigation (done 2026-09-24 00:04)

The two jobs below would have deleted / restarted the owner's deliberately
stopped containers within hours; both were paused with the owner's OK:
- "Docker System Cleanup" (02:00, `docker system prune -f`) — disabled.
- Global container auto-update (03:00) — disabled.

## 2. Findings (proven unless marked suspect)

### A. Can destroy data or hand out root (fix first)

| # | Problem | Where |
|---|---|---|
| A1 | Nightly auto-update recreates **every** container (no `HasUpdate` check), starts stopped ones, incl. databases; 60 recreations since 09-20 | app-management/service/container_update_manager.go:549, container.go:574 |
| A2 | Update reports success when the image pull failed | container.go:582-593 |
| A3 | Prune task (`docker system prune -f`) deletes all stopped containers; template doesn't say so | core/service/schedule.go:291 |
| A4 | Opening the updater window starts `apt-get dist-upgrade -y` (mounted() auto-start) | ui SystemUpdateWindow.vue:147-151 |
| A5 | apt install/remove/upgrade run inside the HTTP request: UI fails at 60 s, backend SIGKILLs dpkg at 5/10 min | core/route/v1/apt_packages.go:45-52 |
| A6 | chpasswd line injection (`user:pass\n`), `root` and "protected" users accepted | core/route/v1/sysusers.go:32,163-169 |
| A7 | Profile update saves the client-sent user `id`/`role`/`avatar` → edit other users, avatar path = arbitrary file read as root | user/route/v1/user.go:283-317, service/user.go:77 |
| A8 | USB eject builds a bash string from the request path (injection; labels with spaces fail) | local-storage/service/disk.go:154 |
| A9 | Disks → Remove offered for the system disk; `umount --force` + `RemoveAll(mountpoint)` | DisksPanel.vue:104, local-storage service/disk.go:280-303 |
| A10 | Samba share name/path unvalidated → smb.conf injection; `chmod 0777` on any path | core samba.go:86-121, service/shares.go:97 |
| A11 | apt source line regex accepts newlines (multi-line injection into sources) | apt_packages.go:381 |
| A12 | fstab: no lock, shared temp file without O_TRUNC; edit unmounts before validating; edit of a disabled entry duplicates it | local-storage pkg/fstab/fstab.go:122-300, fstab_manager.go:519-556 |
| A13 | Remove companion device silently `RemoveAll`s its backups; rename ignores errors; same-sanitised names share a folder | core route/v1/companion.go ~1057-1143 |
| A14 | Malformed avatar upload → `log.Fatal` kills user-service | user.go:212 |
| A15 | Backup/sync tasks killed after 10 min (half-done `move`) | schedule.go:210 |

### B. Features that don't work

| # | Problem | Where |
|---|---|---|
| B1 | Package lists keyed by name; 281 multi-arch duplicates → Vue renderer crash, Settings window stops switching sections | PackagesSection.vue:133,182 |
| B2 | Schedule enable toggle calls `PUT /:id/enable`; route is `POST /:id/toggle` | ui service/schedules.js:27, core route/v1.go:297 |
| B3 | Editing a backup/sync task drops source/dest/direction/mode/args, says "updated" | schedule.go:532-560 |
| B4 | Per-container auto-update switch never persists (id vs name keys, orphans) | container_update_manager.go:168-362 |
| B5 | Container logs line count ignored (`lines` vs `tail`) | ui service/container.js:151, docker.go:327 |
| B6 | "Update" schedule does pull+restart (old image) | schedule.go:249-261 |
| B7 | WebUI port change always 400 (number vs string); polling never stops | SystemSection.vue:371, common/model/gateway.go:9 |
| B8 | Delete apt source always fails (double-wrapped body; full path rejected) | ui sys.js:211, apt_packages.go:362 |
| B9 | "Update NivaroOS": banner always wrong (string compare `v1.2.1` vs `1.0.0`), update is a no-op reporting success | UpdatesSection.vue:224, core system.go:68 |
| B10 | deb822 `.sources` repos invisible; search silently truncates to 100 before sorting | apt_packages.go:95,350 |
| B11 | System logs: whole 10.7 MB file sent on every System visit | core service/system.go:530 |
| B12 | Remote access: tailscaled not running; `tailscale up` blocks past 60 s; error detail hidden | core route/v1/tailscale.go:109-124 |
| B13 | Remote SMB: port ignored, mount error ignored ("Connected"), dial without timeout | core samba.go:216-242, pkg/samba |
| B14 | Cloud account remove/reconnect panics on never-mounted accounts | local-storage service/storage.go:116 |
| B15 | fstab: NTFS defaults to ntfs-3g; fstype regex rejects `-`; `nofail` not enforced; manual device path can't work; one bad line breaks the panel | FstabPanel.vue, fstab_manager.go |
| B16 | Unmounted data drive listed as "Available — Format" | local-storage route/v1/disk.go:102 |
| B17 | Widget toggles lost when the sidebar isn't mounted; theme not synced across devices | WidgetVisibilityPanel.vue:72, AppearanceSection.vue:173 |
| B18 | Deep links ignored when Settings is already open on that prop; reload always lands on System | SettingsApp.vue:84, mutations.js:334 |
| B19 | Search: ~18 of 44 entries match nothing; English-only; jumps to section only; blank icons | ROWS exports, utils/settingsSearch.js |
| B20 | Admin delete: no self/last-admin guard; resolves id via shadowed route (`current`) | user.go:593, users.js:87 |

### C. Feedback, state and honesty

- Errors show axios' generic text instead of the server message
  (`err.message` vs `err.response.data.message`) across schedules, containers,
  packages, users; many actions have no catch at all (share/SMB/system users,
  disks eject, cloud rename/remove, appearance).
- No refresh after actions / no polling while jobs run: Run Now shows a red
  dot for "running"; container Restart sets state optimistically; lists never
  refresh (runtime census: 0 requests in 30 s idle on every section).
- Stuck states: task "running" forever after a restart; upgrade polling dies
  on one failed request; port-change polling never stops.
- Overlapping runs: no guard for the same scheduled task or across apt ops.
- Destructive actions without confirmation: container Update/Update all/
  Restart, fstab Unmount, apt Uninstall (no dependency preview).
- Raw `{placeholders}` in confirmations (format disk, delete user/device/…)
  — the disk-format dialog doesn't say which disk.
- Random task order (map iteration); stored XSS via task name in v-html
  confirm; unbounded task output stored and returned in every list.
- Memory: cycling sections 4× grew DOM nodes 2,614 → 9,574 and heap
  108 → 152 MB (suspect leak; source not yet isolated).
- Console errors on every load: `cleanUrlPath e.includes is not a function`,
  `assignBrowse … reading 'length'` (wallpaper uploader).

### D. Design system, contrast, accessibility

- Dark mode is a second, hard-coded colour system (`_dark.scss`, ~2,900
  lines of `!important` hex). It blanks every border in Settings
  (`.settings-app * { border-color: transparent }`) and misses shared
  classes: user names **1.21:1**, WebUI port **1.23:1** (measured).
- Light mode: muted token `#94a3b8` on white **2.56:1** (versions, hints,
  schedule times, stats), Bulma success `#48c78e` **2.14:1**, `#10b981`
  **2.54:1**, companion pills 2.3:1. Borders 1.2–1.3:1 in both themes.
- Two primaries (#2075f3 Buefy vs #2563eb token; white on #2075f3 = 4.29).
  Primary actions split 37 `is-dark` / 38 `is-primary`; `is-dark` switches ON
  are fainter than OFF in dark mode (1.47:1).
- axe: 204 unlabelled form controls, 12 unnamed selects, 12 keyboard-
  unreachable scroll regions; overlay without Esc/focus trap/dialog role;
  search without keyboard support; theme cards without focus ring.
- Responsive: phone — Containers scrolls sideways, Packages tabs overflow;
  tablet — Updates version strings overflow.

## 3. Principles for the fixes

1. **Long operations are jobs.** apt, upgrades, container updates, backups,
   tailscale login: start → job id → status/log polling → survives reload and
   the 60 s HTTP limit; never kill dpkg.
2. **Never report success you didn't verify** (pull, mount, update, save).
3. **Validate on the server**; the UI validates for convenience only.
   No shell strings built from input — argv only.
4. **Destructive = confirm + say exactly what happens** (which disk, which
   containers, what data goes).
5. **One design system.** Tokens are the only colour source; dark mode =
   token values, not overrides. Every text pair ≥ 4.5:1, UI edges ≥ 3:1.
6. **Every section: loading, empty, error and stale states**, and it
   refreshes after its own actions.
7. **Tests at the seams** (as in the Files rebuild): Go handler/service tests
   on real temp dirs/files; UI checks via the runtime audit (axe + contrast
   = 0 failures) re-run each phase.

## 4. Phases

1. **Safety** — A1–A15 (backend + the updater auto-start). Re-enable the two
   paused nightly jobs once A1–A3 are fixed.
2. **Broken features** — B1–B20.
3. **Feedback & state** — section C.
4. **Design system, contrast, a11y, responsive** — section D.
5. **Auth hardening** — token revocation on password change/delete, issuer
   check, bcrypt with transparent upgrade from MD5, per-IP login limiting,
   `Header.Set` for user_id.
6. **Verification** — full runtime audit re-run (all sections, both themes,
   three widths) with 0 contrast/axe criticals, 0 console errors; live
   end-to-end of each fixed action.

## 5. Progress

| Phase | Commits | Verified |
|---|---|---|
| 1 Safety (A1-A15) | 2019c0f, df6bb28, 69c51c5, 5aaff79, ac12a39 | Real-Docker recreate tests; apt jobs incl. restart recovery + real systemd-run + UI e2e; handler tests on SQLite for profile/avatar; live: root/service/owner password change and newline injection rejected; fstab 40 concurrent toggles intact; tail of a 26 MB log in ms |
| 2 Broken features (B1-B20) | 6973af2, b75009a, fe4ffe9 | Auto-update switch survives a real recreate; live: deb822 listed, python3 ranked first, tailscale NoDaemon state, honest 409 on "update NivaroOS"; browser: search jumps to rows/tabs, deep links repeat |
| 3 Feedback & state | faf0720 | apiError + authRefresh unit tests; System leak 490 nodes/visit -> 0 (heap snapshot: token-refresh queue never drained); removal preview live (containerd -> docker-ce) |
| 4 Design system, contrast, a11y, layout | 47629f3 | Full runtime re-audit, see below |
| 5 Auth hardening | 11cd4ca | jwt issuer test; MD5->bcrypt upgrade, revocation, deleted user, per-client limits (handlers on SQLite); all services rebuilt, users table migrated |
| 6 Verification | this | Below |

Final runtime audit (production bundle, 11 sections x dark/light x
desktop/tablet/phone, every tab):

| Check | Start | End |
|---|---|---|
| axe WCAG 2.1 AA violations | 228 | 0 |
| Text below 4.5:1 (compositing scan) | ~400 | 0 |
| Horizontal overflow | 30+ | 0 |
| Raw {placeholders} | 22 across the UI | 0 |
| DOM nodes after cycling all sections 4x | 2,614 -> 9,574 | 1,502 -> 1,502 |
| System section open (UI freeze) | 2.7 s | 0 |
| Console errors from Settings | 3 kinds | 0 (only the harness's own missing login) |

Tests added: Go (core, user, local-storage, app-management with real
Docker, common jwt) and Vitest (search, apiError, authRefresh, VM client)
- UI suite 73/73.

Left for later (not blocking): the users' existing MD5 hashes upgrade on
their next login; access tokens elsewhere than the user service stay
valid up to their 3 h lifetime after a password change.

Also fixed while there: the 2.7 s UI freeze on opening Settings (10 MB log
rendered) and the Terminal re-fetching it every 5 s; Package Manager's
duplicate-key freeze (B1) and delete-source (B8); 22 raw {placeholder}
strings across the UI.
