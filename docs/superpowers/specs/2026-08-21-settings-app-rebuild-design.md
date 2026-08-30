# Settings App Rebuild — Design

## Status: DESIGN APPROVED (2026-08-21)

## Context

The current `SettingsApp.vue` (backlog items 4/6/14, done 2026-08-20)
already opens through the shared window manager
(`WindowManager.vue`/`DesktopWindow.vue`) with drag/resize/minimize,
and already covers Account / Users & Access / Appearance / General /
System. The user considers it a mess: bad/buggy code, missing
functionality, and poor UI/UX — not something to patch further, but to
rebuild properly.

Investigation found two separate problems:

1. **Window chassis bugs.** `DesktopWindow.vue`'s drag/resize handlers
   commit to Vuex — and write `localStorage` via `persistWindows` —
   on every raw `mousemove` event, with no throttling. This is what
   reads as janky/buggy dragging. There is also no maximize/restore
   control, only minimize/close.
2. **Settings content doesn't adapt to the window.** The layout uses
   fixed-rem sizing with no responsiveness tied to the window's actual
   size (only the browser viewport could ever trigger a CSS media
   query, and the window can be any size inside a large viewport), so
   a narrow Settings window just breaks instead of reflowing.

Separately, the backend already exposes significant functionality
Settings never surfaces, confirmed with the user as in-scope to add:

- **Storage & Disks** — `route/v1/storage.go` + `route/v1/disk.go`
  (CasaOS) and `route/v2/mount.go`/`merge.go` (CasaOS-LocalStorage),
  already wrapped by `src/service/storage.js`, `disks.js`,
  `local_storage.js` and registered on `$api` (`$api.storage`,
  `$api.disks`, `$api.local_storage`) — no UI consumes them.
- **Network Shares (SMB)** — `route/v1/samba.go` (shares +
  connections, distinct from the SMB *user accounts* Settings already
  manages), already wrapped by `src/service/samba.js` and registered
  as `$api.samba` — no UI consumes it.
- **Remote Access (Tailscale)** — post-approval revision: originally
  scoped against a legacy ZeroTier proxy route (`v1/zt/*`), but
  `zerotier-one` is not installed on this box, while Tailscale is
  already installed, logged in, and actively connected (real tailnet
  with peers). Tailscale has no existing backend integration in this
  fork at all, so this section requires a small new Go route (shells
  the `tailscale` CLI) — the one exception to this design's "no new
  backend" framing, confirmed with the user given it requires a CasaOS
  binary rebuild + service restart.
- **System info & Updates** — `route/v1/system.go` has version
  check/update, hardware info, and error logs; no frontend service
  file or UI yet.

Confirmed with the user:
- Window mechanics AND content responsiveness are both to be fixed.
- New sections to add: Storage & Disks, Network Shares (SMB), Remote
  Access (Tailscale), System info & Updates.
- Approach B (modular rebuild within one Settings app) over patching
  in place (A) or splitting Storage/Network into separate dock apps
  like VM Manager (C) — Settings stays the single "rest of the OS"
  control panel, matching ZimaOS/Umbrel/TrueNAS Scale precedent.

## Architecture

```
SettingsApp.vue                          (shell: rail + search + ResizeObserver)
├─ SettingsNav.vue                       (icon rail, 7 categories)
├─ SettingsSearch.vue                    (filters rows across all sections, jump-to + highlight)
└─ sections/
   ├─ AccountSection.vue                 (wraps existing AccountPanel — unchanged)
   ├─ AppearanceSection.vue              (wallpaper, transparency/blur, WidgetVisibilityPanel — unchanged internals)
   ├─ UsersSection.vue                   (existing CasaOS/System/SMB user tabs — unchanged internals)
   ├─ NetworkSection.vue
   │  ├─ NetworkSharesPanel.vue          (NEW — samba.js: create/list/delete SMB shares)
   │  └─ RemoteAccessPanel.vue           (NEW — zerotier.js: install-detection empty state → join/status/leave)
   ├─ StorageSection.vue
   │  ├─ DisksPanel.vue                  (NEW — disks.js: disk/USB list, umount, Automount USB toggle)
   │  └─ StoragePoolsPanel.vue           (NEW — storage.js + local_storage.js: add/format/delete, mergerfs pools)
   ├─ GeneralSection.vue                 (language, RSS, app-discovery switches — unchanged)
   └─ SystemSection.vue
      ├─ core rows                       (port, restart/shutdown — unchanged, power() confirm fixed)
      └─ AboutPanel.vue                  (NEW — sys.js additions: version/update check, hardware info, error log viewer)
```

Existing panels (`AccountPanel`, the three user-management panels,
`WallpaperModal`, `WidgetVisibilityPanel`) relocate into their new
section wrapper as-is — no internal rewrite, since they aren't what's
broken. Only the shell, nav, layout, and the four new panels are new
code.

## Information architecture

7 nav categories, replacing the current flat 5: **Account, Appearance,
Users & Access, Network & Sharing, Storage, General, System.**
"Automount USB Drive" moves from General into Storage → Disks, since
it's a disk behavior and now has a disk list to live next to.

## Window chassis fixes (`DesktopWindow.vue`)

- **Throttle drag/resize.** Accumulate the pending rect in a local
  variable inside the `mousemove` handler, apply it to the DOM/Vuex via
  `requestAnimationFrame`, and call `persistWindows` (the
  `localStorage` write) only once, on `mouseup`. The store still
  updates live for smooth on-screen dragging; the expensive
  serialize-and-write no longer happens on every pixel of movement.
- **Maximize/restore.** New titlebar button between minimize and
  close, plus double-click-titlebar as a shortcut, matching the
  existing minimize/close interaction pattern. Restoring returns to
  the pre-maximize `{x, y, width, height}`, held on the window object
  in memory — not persisted, since a persisted maximized state would
  be meaningless against a different screen size on next login.
- Resize-handle geometry (the 8-direction opposite-edge-anchoring math)
  is already correct and is not changed.

## Responsive content layout

`SettingsApp.vue`'s root element gets a `ResizeObserver` watching its
own content box (not `window`/the viewport). Two breakpoints, each
toggling a class on the root:

- `is-compact` (width below ~46rem): `SettingsNav` collapses to
  icon-only (labels hidden, tooltip on hover).
- `is-narrow` (width below ~34rem): individual `.setting-row`s stack
  icon+label above their control instead of side-by-side; `b-tabs`
  (Users & Access) switch to a dropdown-style selector instead of
  toggle buttons.

This makes Settings actually respond to the window being resized,
which today it structurally cannot do.

## Data flow & error handling

- Each new panel owns its own fetch/loading/error state locally,
  matching the existing pattern in `CasaosUsersPanel` etc. No new
  Vuex module — Settings has never needed cross-section shared state
  beyond what's already global (backdrop alpha/blur via CSS custom
  properties, widget visibility via per-user custom storage).
- **RemoteAccessPanel** calls a status check first; since
  `zerotier-one` isn't installed, it renders an install-detection
  empty state (same shape as VM Manager's qemu/libvirt check:
  "not installed — click to set up", no silent auto-install) rather
  than assuming the daemon is present. New `src/service/zerotier.js`
  wraps `/v1/zt/*`, registered on `$api` in `src/service/api.js`
  alongside the existing modules.
- **Destructive storage/samba actions** (format a storage device,
  delete a pool, delete a share) get a real `$buefy.dialog.confirm`
  before calling the API — not the button-relabeling hack `power()`
  currently uses ("click Restart, label changes to 'Are you sure?',
  click again"). `power()` itself is fixed to use the same real
  confirm dialog, both for consistency and because the relabel hack
  has no way to be cancelled by clicking away.
- **SettingsSearch** indexes `{section, row-label}` pairs built
  statically from each section's own row metadata (a small exported
  array per section component) — pure client-side filtering, no new
  backend or persisted index.

## Testing

No existing component-test harness covers these panels, and this runs
against a live single-box backend, so verification is manual through
the running app:
- Resize/move/maximize the Settings window at several widths and
  confirm the nav rail and setting rows reflow at the `is-compact` /
  `is-narrow` breakpoints instead of breaking.
- Exercise each new panel's happy path: create and delete a test SMB
  share; list disks and USB devices, toggle automount; format a scratch
  storage device if one is available on this box, or verify the
  confirm-dialog gate if not; view version/hardware info and the error
  log viewer.
- Confirm `RemoteAccessPanel` renders the install-detection empty
  state correctly, since `zerotier-one` is not installed on this box.
- Confirm drag/resize no longer visibly stutters, and that maximize/
  restore round-trips to the exact prior rect.

## Out of scope / untouched

- No new Go backend code — every new section is a UI layer over
  routes/services that already exist and already run.
- ZeroTier daemon installation itself is out of scope; only the
  install-detection UI path is built, matching the VM Manager
  precedent of explicit, user-triggered setup for optional system
  dependencies.
- The user's other uncommitted changes (`SideBar.vue`, `Home.vue`,
  `Network.vue` widget) are unrelated in-progress work and are not
  touched by this rebuild.
- Snap-to-edge/aero-snap window docking is not part of this work —
  only maximize/restore and the throttling fix.
