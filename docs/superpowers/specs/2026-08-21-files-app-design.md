# Files App — Ground-Up Windowed Rewrite — Design

## Status: DESIGN APPROVED (2026-08-21)

## Context

BACKLOG.md item #4 (windowing system) wraps the pre-fork, full-page
`filebrowser/FilePanel.vue` (1157 lines) inside `DesktopWindow.vue`.
Item #11 flags this as "PARTIALLY DONE": a loading bug was fixed, but
the real internal layout redesign for a small, resizable, movable
window was explicitly deferred. This spec is that redesign.

The legacy component was built for a full page, not a window:

- `b-sidebar` (Buefy) is designed for a full-page overlay/mobile
  drawer, not a pane inside an arbitrary-sized window.
- `$buefy.modal.open()` (New Folder/File/Rename/Detail/Share dialogs)
  renders centered on the browser *viewport*, dimming the whole
  screen regardless of where the Files window is or how big it is.
- `VueBreakpointMixin` and the `.full-screen` CSS class key off
  `window.innerWidth` / `100vh` - meaningless once Files lives in an
  arbitrarily-sized, arbitrarily-positioned window rather than the
  page.
- The context menu positions against viewport coordinates, which is
  wrong once the window isn't anchored at the page origin.

Confirmed with the user: this is a full ground-up rewrite of every
layer (shell, sidebar, toolbar, grid/list view, all 7 viewers,
uploader UI, dialogs, context menu, sharing/drop sections) - not a
reuse-and-reskin of the existing ~40 sub-components. The one explicit
exception is third-party libraries the legacy code depends on
(`simple-uploader.js` for chunked upload/resume, `socket.io-client` /
`vue-socket.io-extended` for live updates) - those are vendored
dependencies, not CasaOS code, and reimplementing a chunked-upload
protocol from zero would be pure risk for no benefit. The Go backend
is unchanged; every new component talks to the exact same `/v1/folder`,
`/v1/batch`, `/v1/samba`, `/v1/local-storage`, and
`/v2/casaos/file/upload` endpoints the legacy code already uses.

Two related decisions from the same conversation, folded into this
build:

1. **In-window dialogs.** New Folder/File/Rename/Detail/Share become
   overlay panels confined to the Files window's own bounds, not
   viewport-centered modals.
2. **Window maximize.** `DesktopWindow.vue` currently has no maximize -
   only drag/resize/minimize/close. Viewers (PDF/video/image especially)
   are painful in a small window, so this build adds a real
   maximize/restore button to the shared window chrome (benefits
   Terminal/Settings/VM Manager too), not a Files-only workaround.

## Approaches considered

1. **Modular shell + extend the existing flat Vuex store (chosen).**
   A thin `FilesApp.vue` shell composes independent feature components
   (Sidebar, Toolbar, ContentView, Viewers, Dialogs, ContextMenu).
   Shared state lives as new keys/mutations/actions added to the
   *existing* flat store (`src/store/{state,mutations,actions,getters}.js`),
   matching how `windows[]` already lives there - this codebase never
   uses namespaced Vuex modules, so introducing one here would be an
   unfamiliar pattern for no benefit.

2. **Self-contained component, local state + provide/inject (no
   store).** This is how `VmManagerApp.vue`/`SettingsApp.vue` are
   built. Right call for those - they're single-purpose management
   panels. Wrong call for Files: it has a clipboard that must survive
   navigation, live Socket.IO events that need to reach several
   independent panes at once, multi-item selection, and a background
   upload queue. Routing all of that through provide/inject from one
   root component reintroduces the God-component coupling that made
   the legacy 1157-line file hard to change.

3. **Generic, decoupled "file-manager engine" package.** Rejected -
   over-engineering with no second consumer.

## Architecture

### Component tree

All new files live under `src/components/files/`:

```
FilesApp.vue                 shell: flex column, height:100%,
                              ResizeObserver on its own root element
├── Toolbar.vue               breadcrumb + actions; action items
│                              collapse into a "⋯" overflow menu below
│                              a width threshold; breadcrumb itself
│                              collapses long paths behind a "…" crumb
├── Sidebar.vue                expanded (tree + mounts + nav entries)
│   │                          OR collapsed icon-rail below a width
│   │                          threshold (user can also pin either way)
│   ├── FolderTree.vue         rewrite of sidebar/TreeList.vue
│   └── MountList.vue          rewrite of sidebar/MountList.vue
├── ContentView.vue            single component, `mode: 'grid'|'list'`
│   │                          prop (replaces separate GirdView/ListView),
│   │                          owns the drag-drop-to-upload target
│   ├── SharedView.vue         Samba shares section (rewrite of
│   │                          shared/ShareListPage.vue)
│   └── DropView.vue           LAN Snapdrop-style transfer section
│                              (rewrite of drop/DropPage.vue; the
│                              underlying WebRTC/WebSocket protocol in
│                              Network.js is preserved as-is)
├── UploadTray.vue             docked upload progress, positioned
│                              within FilesApp's own bounds
├── ContextMenu.vue            position clamped to FilesApp's own
│                              bounding rect, not the viewport
├── DialogOverlay.vue          shared chrome for New Folder/New
│                              File/Rename/Detail/Share dialogs -
│                              renders inside FilesApp's own bounds
└── viewers/                   CodeEditor, ImageViewer, VideoPlayer,
                                MarkdownEditor, DocViewer, ExcelViewer,
                                PdfViewer - each fills FilesApp's own
                                bounds (position:absolute; inset:0
                                relative to FilesApp, not 100vw/100vh)
```

### Window-local sizing, not viewport-based

Every size-dependent decision - sidebar collapse, toolbar overflow,
dialog centering, context-menu clamping, viewer fill - is driven by a
`ResizeObserver` on `FilesApp`'s own root element, exposed to
descendants via `provide`/`inject` as a small reactive `{ width,
height }`. Nothing checks `window.innerWidth` or `100vh`.

Two breakpoints on `FilesApp`'s own width:
- **< 560px:** sidebar auto-collapses to icon-rail; toolbar action
  buttons collapse into the overflow menu; breadcrumb shows only the
  last 2 segments + a "…" crumb for the rest.
- **< 420px (the window's practical floor, given `DesktopWindow`'s
  existing 360px `MIN_WIDTH`):** grid view switches to a single
  column; list view keeps working as-is (it already reflows one row
  per item).

### State (added to the existing flat store)

New state keys (mirroring the existing flat, non-namespaced
convention): `filesCurrentPath`, `filesBreadcrumb`, `filesListing`,
`filesLoading`, `filesError`, `filesSelection` (array of paths),
`filesClipboard` (renamed/kept-compatible with the existing
`operateObject` shape so `$api.batch.task` keeps working unchanged),
`filesViewMode`, `filesSidebarCollapsed`, `filesActiveSection`
(`browser` / `shared` / `drop`), `filesUploadQueue`.

Actions call the same backend endpoints already in use:
`$api.folder.getList/getSize/getFileCount`, `$api.batch.task` (copy/
move/delete), `$api.samba.*` (now correctly populated - see the SMB
fix applied directly to this box, outside this spec's scope), and
`$api.local_storage.getMergerfsInfo`. The uploader UI wraps the same
`simple-uploader.js` instance pointed at
`/v2/casaos/file/upload`. Live updates come through the same
`casaos:file:operate`, `storage_status`, and `local-storage:disk:added`
Socket.IO events, now dispatching store mutations instead of being
handled by one component's local `sockets: {}` block.

### Dialogs

`DialogOverlay.vue` renders `position:absolute; inset:0` inside
`FilesApp`'s own element, with a centered card and a backdrop that
only dims the Files window's content - not the whole screen. New
Folder, New File, Rename, Detail (file info), and Share-select all use
this same shell with different body content, replacing five separate
`$buefy.modal.open()` call sites in the legacy code.

### Window chrome: maximize

`DesktopWindow.vue` gains a maximize/restore button in the titlebar
(plus double-click-titlebar as a shortcut), storing the pre-maximize
`{x, y, width, height}` in the same `windows[]` store entry that
already persists position/size/minimized state across sessions
(BACKLOG item #12), so maximize state and the restore rect survive a
reload the same way. Maximized size matches the existing "stagger"
inset convention roughly 90% of the desktop area, leaving the Dock
visible.

## Data flow

1. `FilesApp` mounts inside `DesktopWindow` → dispatches an init
   action → fetches the root folder listing, mount list, and Samba
   share status in parallel.
2. Navigation (breadcrumb click, folder double-click, sidebar tree
   click) dispatches a `filesNavigate(path)` action → updates
   `filesCurrentPath`/`filesBreadcrumb` → re-fetches the listing.
3. Selection changes (click, shift-click range, ctrl-click, "select
   all") mutate `filesSelection` directly - `ContentView` and
   `Toolbar` both read from it, so the batch-action toolbar and the
   "N selected" label never fall out of sync (the legacy code
   duplicated this bookkeeping in a component-local `selectState`).
4. Copy/cut populates `filesClipboard`; paste dispatches
   `$api.batch.task` with the clipboard's `to` set to
   `filesCurrentPath`, then clears it on success - same contract as
   today.
5. Socket.IO events dispatch store mutations directly, so any open
   view (grid, shared list, sidebar mount list) re-renders from the
   same source of truth instead of each needing its own listener.

## Error handling

- Folder-list failures: inline error state in `ContentView` (kept
  concept from the existing `ErrorHolder`) + a toast, matching current
  behavior.
- Upload failures: per-file error badge in `UploadTray`, using
  `simple-uploader.js`'s existing `fileError` event - not a new retry
  protocol.
- Viewer failures (unsupported/corrupt file): inline "Can't preview
  this file" state with a "Download instead" action. This replaces the
  legacy silent fallback to a generic `DetailModal` for anything
  `getPanelType()` didn't recognize.
- Samba/share API failures: existing toast pattern, unchanged.

## Testing

This repo has no component-mount testing today (no
`@vue/test-utils`; the two existing spec files -
`file_utils.spec.js`, `vmSidecar.spec.js` - are pure-function
`vitest` tests). This build follows the same convention rather than
introducing a new testing layer:

- **Unit-tested with `vitest`:** breadcrumb-building from a path,
  path-join/parent-path utilities, the width-breakpoint calculator,
  selection-set reducers (select/select-range/select-all/clear),
  clipboard-shape helpers.
- **Manually verified in the running app** (called out explicitly,
  same as the VM Manager spec's verification section): drag-to-resize
  and drag-to-move at every threshold width, sidebar collapse/expand,
  toolbar overflow collapse, drag-and-drop upload, all 7 viewers
  rendering inside a small window and inside a maximized window,
  context-menu clamping near window edges, in-window dialogs staying
  confined when the window is dragged mid-interaction, maximize/
  restore (including after a page reload), and the Shared tab showing
  live Samba shares end-to-end.

## Out of scope for this build (backlog for later)

- Virtualized rendering for very large folders - revisit if real
  folder sizes make this a problem.
- Any change to the LAN Drop feature's WebRTC/WebSocket wire protocol
  (`Network.js`) - only its UI wrapper is rewritten.
- Any Go backend changes - this is a frontend-only rewrite against the
  existing API surface.
- Multi-instance Files windows (opening two independent Files windows
  at once) - `OPEN_WINDOW` is currently a singleton-by-`id` pattern
  used by every app in the Dock; changing that is a windowing-system
  decision, not a Files-specific one.
