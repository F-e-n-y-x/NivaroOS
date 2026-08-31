# NivaroOS Fork — Feature Backlog

Queued after Milestone 1 (fork building + swapped in, matching current
install — see docs/superpowers/specs/2026-08-19-casaos-fork-milestone1-design.md).
Each item gets its own brainstorm/design pass before implementation.

## 15. VM Manager — DONE (2026-08-20)
New Dock-pinned windowed app for creating/running/managing QEMU/KVM VMs,
backed by a new `casaos-vm-sidecar` (libvirt-go, port 28641, see
`docs/superpowers/specs/2026-08-20-vm-manager-design.md`) - same
one-small-Go-service shape as the GPU sidecar. Covers: install detection
+ explicit user-triggered setup (`qemu-system-x86`/`qemu-utils`/
`libvirt-daemon-system`/`libvirt-clients`/`ovmf`, since "qemu-kvm" isn't
a real package on Debian 13), VM create/start/shutdown/reboot/force-off/
delete, a noVNC console tab (sidecar proxies browser WebSocket straight
to the VM's VNC port, no separate websockify process), default NAT
networking plus opt-in bridged networks (host NIC picker excludes
whichever interface currently carries the default route, so it can never
disconnect the box), and ISO upload/browse. VM disks default to
`/DATA/VMs/<name>.qcow2`, overridable per VM.

GPU passthrough, snapshots/cloning/live migration, cloud-init
provisioning, and SPICE are explicitly out of scope - queued for later
milestones, same treatment as the GPU widget was its own milestone.

Sidecar handlers are unit-tested against libvirt's `test:///default`
fake driver (26 tests, no real KVM needed); the domain-create/lifecycle
path was also verified against this box's real `qemu:///system` (booted
an actual `qemu-system-x86_64 -accel kvm` process, confirmed VNC port
resolution, cleaned up). `casaos-vm-sidecar.service` is enabled and
running.

## 1. GPU widget
Native dashboard tile: utilization %, VRAM used/total, temperature, power
draw/limit. Click-to-expand shows per-process GPU usage for **all** host
processes (not just CasaOS-managed apps), unlike the existing CPU/RAM
widgets. Data source: sidecar service polling `nvidia-smi` (this PC has an
NVIDIA GTX 1080 Ti). Fall back to full Go-backend integration only if the
sidecar approach doesn't look/feel native enough.

## 2. Base-bar removed at the source
Currently hidden via a `display: none !important` CSS override in
`custom.css` (quick patch on the live install). Once editing the real
source anyway, remove the branding/contact bar (`base-bar` /
`brand-bar` / `contact-bar` in `App.vue`) properly instead of patching
around it.

## 3. Proper compact/85% view — DONE (2026-08-20)
Root font-size scaling (`html.is-compact-view { font-size: 85% }`)
instead of the earlier `zoom` hack that broke fixed elements and click
coordinates — almost everything here is rem-based, so this shrinks
spacing/type proportionally without changing the CSS pixel scale
factor. Toggle lives in the top-bar settings dropdown for now
(localStorage-persisted); will migrate into item 6's Settings page.

## 4. Windowing system for Files & Settings (+ future apps) — DONE (2026-08-20)
Real window manager: `store.windows[]` + z-index counter, drag/resize
(right/bottom/corner handles)/minimize/close, multiple windows open at
once, minimized ones collect in a bottom taskbar dock. Files and the new
Settings/Terminal apps all open through it (`DesktopWindow.vue`'s
component registry). Confirmed with the user: multi-window + minimize,
not single-window/close-only.

Still open: pinning arbitrary folders to the sidebar (favorites-style)
for Files specifically - unrelated to the windowing mechanism itself,
not done yet.

## 5. App grid / desktop launcher rework — DONE (2026-08-20)
- ✅ Legacy apps unified into the main draggable list, no separate section.
- ✅ Icon editing (crop/pan/zoom + live CSS roundness) for any app —
  container/legacy apps via "Edit", v1/v2 apps via "Edit icon".
- ✅ Web-link shortcut tiles — turned out to already exist via the
  pre-existing "Add external link/APP" option (per-user storage only,
  never touches app-management).
- ✅ Folders — drag-to-add/drag-out, right-click Rename/Edit icon/Delete,
  custom icon (falls back to a 2x2 member-preview grid), persisted via
  per-user custom storage.

## 6. Dedicated Settings page (ZimaOS-style) — DONE (2026-08-20)
`SettingsApp.vue` now owns everything that used to live in TopBar's
dropdown - TopBar.vue and SearchBar.vue are deleted entirely. Sections:
Account (Profile via `AccountPanel` + new CasaOS/System/SMB user-manager
tabs), Appearance (wallpaper, transparency slider, blur slider),
General (language, RSS, recommended/existing-app switches, USB
automount), System (WebUI port, restart/shutdown). Compact view was
removed outright (was broken) rather than migrated. Settings window
uses a light/white theme (`.window-light` in `DesktopWindow.vue`) while
Files/Terminal keep the dark glass chrome.

New backend to support this: `route/v1/sysusers.go` (Linux user
add/delete/password/sudo+docker group membership via useradd/userdel/
chpasswd/usermod - `ayush` is hard-protected from deletion since
Terminal depends on it), `route/v1/smbusers.go` (pdbedit/smbpasswd
wrapper), and CasaOS-UserService's new `POST /v1/users/register-key`
(upstream only ever issued one key, before the first user existed -
this mints fresh ones on demand so more CasaOS users can be added
after initial setup).

Window transparency/blur is now globally adjustable, not hardcoded:
`--ui-backdrop-alpha`/`--ui-backdrop-blur` custom properties (set in
`App.vue`, persisted to localStorage) feed into `$backDropColor`/
`$backDropBlur` in `_variables.scss`, so every consumer (widget
`.blur-background`, `DesktopWindow`, `Dock`) picks it up automatically.
Default is now much more opaque with a light blur, replacing the old
hardcoded 0.4-alpha/5px-blur glass look.

Not done: per-field widget toggles (e.g. GPU widget's name/driver
line) inside Settings - widget reordering itself is drag-on-desktop now
(see item 8), not a Settings-page concern anymore.

## 8. Terminal remade as a real local shell — DONE (2026-08-20)
Replaced the SSH-login-screen flow entirely. New `/v1/sys/wsterm`
backend endpoint spawns a real pty (`creack/pty`) running bash with
privileges dropped to the `ayush` desktop user (CasaOS runs as root, so
no password needed - same trust model as a real desktop's built-in
terminal). `TerminalCard.vue` connects straight to it on mount, no
username/password form. Old `wsssh`/`ssh-login` endpoints and
`checkSshLogin` are left in place but unused.

## 9. Bottom dock — DONE (2026-08-20)
`WindowTaskbar.vue` (only appeared when something was minimized)
replaced by `Dock.vue`: always-visible pinned launchers for Files/
Terminal/Settings (open/minimized shown via a dot under the icon,
click toggles focus/minimize like a real dock) plus chips for any other
open windows.

## 7. Responsive wide-screen layout — DONE (2026-08-19)
Bulma's `.container` was capping at a flat 1344px past the fullhd
breakpoint; scoped `.container.home-container` override now scales with
viewport width (90% at 1344px+, 92% at 1920px+, capped at 2400px past
2560px). App grid switched from a fixed column *count* per breakpoint to
fixed-size `auto-fill` columns, which was the actual fix for tiles/gaps
looking oversized on wide screens - a fixed count has no choice but to
stretch tiles to fill the row.

## 10. Desktop-style layout: apps left column, freely-movable widgets right — DONE (2026-08-20)
`Home.vue`'s two columns swapped (apps left / widgets right), widget
column widened 20rem -> 39rem to fit two widget columns side by side.
`SideBar.vue` rebuilt from a vuedraggable reorder-list into a real 2D
canvas: widgets sit at saved `{col,row}` grid coordinates, drag freely
via a movement-threshold-gated mousedown handler (so clicks on a
widget's own buttons still work), and snap to the nearest cell on drop;
dropping on an occupied cell swaps the two widgets. Position persists
per-user via `widgets_config` custom storage.

## 11. Files (FilePanel) windowed-mode redesign — DONE (2026-08-22)
Full ground-up rewrite (`src/components/files/`, see
`docs/superpowers/specs/2026-08-21-files-app-design.md` and
`docs/superpowers/plans/2026-08-21-files-app-rewrite.md`), not just the
original infinite-loading bug fix - a real Explorer/Finder-style
windowed layout (sidebar + toolbar + breadcrumb + resizable grid/list
pane), built for the smaller fixed-size window context from scratch
instead of adapting the old full-page modal. Covers the complete
legacy feature set (browse/select/copy/cut/paste/rename/delete/new
folder+file/upload via drag-drop/download, all 7 file viewers as their
own standalone desktop windows, the Shared and LAN Drop sections) plus
new capabilities the legacy app never had: tabs and multiple
simultaneous Files windows, full drag-and-drop between them and the
sidebar/desktop (with a Windows-style Copy here/Move here menu and a
transfer-progress indicator), and a real-time listing refresh reacting
to the backend's actual async copy/move completion signal instead of
its immediate (and premature) HTTP ack.

The legacy `src/components/filebrowser/` tree (~60 files) is deleted
outright - `FilesApp` is now what the "Files" dock icon and every other
`OPEN_WINDOW('files', ...)` call site opens, with no fallback. Two
small pieces of shared code the new app still genuinely needed
(`ErrorHolder.vue`, the LAN-drop `Network.js` wire protocol) moved into
`src/components/files/` first, unchanged.

## 12. Desktop windows persist across sessions — DONE (2026-08-20)
Files/Terminal/Settings windows (position, size, minimized state)
survive a page reload/new session - synced to localStorage on every
window mutation, restored by `WindowManager` on load if nothing's open
yet. Edit-app windows are intentionally excluded (their props are a
live snapshot of an app-list item, meaningless after a reload).

## 13. Universal per-app Edit (rename/icon/roundness), as a real window — DONE (2026-08-20)
Every app - including system apps (Files/Settings/Terminal/App Store),
previously with no context menu at all - now has an Edit option
covering rename, icon, and roundness. Rename writes to
`item.title.custom`, which the existing `ice_i18n` helper already
checks first, so every existing title display picks it up with no new
rendering path. The Edit dialog itself was converted from a
`$buefy.modal.open` call (dimmed backdrop, fixed position) into a real
desktop window - movable/resizable/minimizable - since a floating
modal read as "opening another window" on top of the desktop.

## 14. Settings app remade from scratch — DONE (2026-08-20)
Full rebuild, not a restyle: new IA (Account / Users & Access /
Appearance / General / System), genuine CasaOS visual language (real
icon names from iconfonts-casaos, the hover-effect/_is-radius utility
classes the old TopBar dropdown used), plain white window matching
every other `.modal-card`. Nothing inside Settings opens a separate
floating window anymore - AccountPanel/WallpaperModal gained an
`embedded` prop to drop their own header chrome, port editing and the
system/SMB user password changes became inline expand-in-row controls
instead of modals.
