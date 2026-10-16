# Replacing `@ntohq/buefy-next` in ui/

Status: plan (Phase 1). Owner request 2026-10-16. Agent: ui-buefy.

`@ntohq/buefy-next` 0.2.0 is an archived Vue 3 fork of Buefy. It gets no
fixes (security or Vue updates) any more. This doc lists what ui/ uses, picks
what replaces it, and says how to move without breaking any screen.

## 1. Inventory (origin/master 6ce1d307, 226 .vue files, 180 of them use `b-*`)

### Components

| Component | Uses | Files | Where (when few) |
|---|---|---|---|
| `b-icon` | 788 | 144 | everywhere. Props: `icon`, `pack` (`casa`/`mdi`), `size`, `custom-size`, `custom-class`, `type` |
| `b-button` | 248 | 74 | everywhere. Props: `type`, `size`, `rounded`, `outlined`, `expanded`, `loading`, `disabled`, `label`, `icon-left`, `native-type`, `tag`, `href` |
| `b-input` | 89 | 35 | settings, files, app store, backup, vm, login |
| `b-field` | 53 | 22 | settings, app store, backup |
| `b-switch` | 41 | 15 | settings, backup, widgets |
| `b-select` | 15 | 8 | settings/SmbUsersPanel, ServerUpdatePanel, CloudCachePanel, ScheduledTaskWindow, sections/FansSection, sections/SystemSection, CloudAccountsPanel, files/dialogs/ShareDialog |
| `b-loading` | 13 | 13 | window loaders |
| `b-image` | 11 | 10 | app store, desktop |
| `b-checkbox` | 11 | 8 | files/TrashView, dialogs/DeleteDialog, ds/DsTorrentDetailWindow, ds/DsAddTorrentWindow, vm/VmList, shared/ConfirmDialogWindow, shared/FeedbackPanel, shell/FreeMemoryWindow |
| `b-dropdown` + `b-dropdown-item` | 7 + 23 | 5 | files/Toolbar, app-store/AppStoreApp, ds/DsLiteBrowser, vm/VmConsolePanel, shared/widgets/WidgetFieldsMenu |
| `b-tooltip` | 5 | 3 | app-store/AppCard, files/Toolbar, files/fileList/FilePanel |
| `b-tabs` + `b-tab-item` | 2 + 4 | 2 | app-store/AppTerminalPanel, shared/forms/ImportPanel |
| `b-upload` | 2 | 2 | vm/VmStorage, shared/forms/ImportPanel |
| `b-autocomplete` | 2 | 2 | app-store/AddToFolderPanel, app-store/ExternalLinkPanel |
| `b-progress` | 2 | 1 | settings/ServerUpdatePanel |
| `b-numberinput` | 1 | 1 | vm/VmDiskList |
| `b-notification` | 1 | 1 | views/Login |
| `b-modal` | 0 | 0 | gone already (tests in apps/backup guard against it) |

No Buefy directives are used.

### Programmatic API (`this.$buefy.*`, 80 non-test files)

| Call | Uses | Options used |
|---|---|---|
| `toast.open` | 266 | `message`, `type`, `duration`, `position`, `queue` |
| `dialog.confirm` | 9 | `title`, `message`, `confirmText`, `cancelText`, `type`, `canCancel`, `onConfirm`, `onCancel` |
| `snackbar.open` | 6 | `message`, `type`, `position`, `duration`, `queue`, `indefinite`, `actionText`, `onAction` |
| `dialog.alert` | 1 | |
| `config.setOptions` | 1 | App.vue: the `casa` and `mdi` icon packs (prefix + size classes) |
| `modal`, `notification`, `loading` | 0 | |

`message` is HTML. `utils/buefySanitize.js` (L6) runs every message through
DOMPurify before Buefy sees it. That must stay.

Other couplings:
- `plugins/a11yLabels.js` patches `BSwitch`/`BCheckbox`/`BSelect` with a mixin
  that names unlabelled controls.
- 12 mount specs `app.use(Buefy)`.
- Styles: `assets/scss/app.scss` imports `@ntohq/buefy-next/src/scss/buefy`
  (37 component sheets). Our own SCSS styles Buefy's DOM by class name
  (`.switch`, `.b-checkbox`, `.toast`, `.snackbar`, `.notices`, `.dropdown`,
  `.b-tooltip`, `.control`, `.select`, `.upload`, `.loading-overlay`,
  `.b-tabs`, ...).
- Bulma 0.9.4 is a separate package and stays. It is plain CSS and not part
  of this work.

## 2. Options

| | Oruga (`@oruga-ui/oruga-next` + `@oruga-ui/theme-bulma`) | PrimeVue unstyled | Our own small components |
|---|---|---|---|
| Maintained | yes (Buefy's official successor) | yes | by us; about 20 small SFCs |
| Look | `@oruga-ui/theme-bulma` 0.10 depends on **Bulma 1.0.4**. We are on 0.9.4, so we would have to upgrade Bulma at the same time (Sass modules, CSS variables, new dark mode): every screen changes | no Bulma theme. We would restyle every component with pass-through classes | **same look**: same DOM and class names as Buefy, so Bulma, Buefy's SCSS (vendored) and our SCSS overrides all keep applying |
| API | different props (`variant` not `type`, `o-field`, `iconPack` config, a programmatic API through `useOruga()`). Every call site changes | completely different API | same prop names. Call sites don't change |
| Bundle | Oruga core + theme CSS on top of Bulma | large, plus our styling layer | today Buefy registers its whole library (~82 KB gz JS min build). Our kit covers only what we use |
| a11y | good | good | we decide. Toasts get `role=status`/`alert`, the dialog traps focus and Esc closes it, labels as a11yLabels does today |
| Risk | big-bang restyle | big-bang restyle | small. One component at a time, everything else unchanged |

**Pick: our own small components** (`ui/src/ui/`). Most of the usage is icon,
button and form controls that render a few Bulma elements. The rest is about
twelve component types, each in 1 to 8 files. Oruga would force the Bulma 1.x
upgrade, and we would rewrite every call site anyway. Our own kit keeps the
pixels identical and leaves no dependency to go stale again.

## 3. Approach

### Compat layer: same names, same props, same DOM
- `src/ui/*.vue`: one SFC per component. Same prop names and events as the
  Buefy component we use (only the props we actually use). It renders the
  same DOM and classes Buefy rendered.
- `src/ui/index.js`: a plugin. It installs Buefy (while any part is left),
  then registers our components under the **same global names** (`BIcon`,
  `BButton`, ...). That overrides Buefy's one component at a time. No
  template has to change, so the parallel agents (ui-deps-small,
  ui-codemirror6, the HostDesktop fix) get no conflicts. Each swap is one
  small commit that we can revert.
- This differs from the brief's "app by app": a global swap per component
  moves every app at once. That is safer here because the screenshot run
  covers every app window in both themes, and it means no edits to
  hundreds of files.
- `plugins/a11yLabels.js` keeps working: it patches whatever component is
  registered under the name.
- Mount specs use `app.use(UI)` from `@/ui` instead of `app.use(Buefy)`.
  `shell/desktop/HostDesktopPanel.connect.spec.js` is left alone while
  another agent works on HostDesktop*.

### Programmatic API: one service module
- `src/ui/notices.js` exports `toast`, `snackbar` and `dialog`
  (`open`/`confirm`/`alert`). The only sanitising point is in there:
  DOMPurify with `purifyConfig.default` (L6, moved from
  `utils/buefySanitize.js`, same test).
- `app.config.globalProperties.$buefy` becomes `{ toast, snackbar, dialog,
  config }` from this module, so the 280 call sites stay as they are. A later
  sed can rename it to `$ui` when Buefy is gone.
- Same DOM as Buefy (`body > .notices.is-top|.is-bottom > .toast.is-*`,
  `.snackbar`, `.dialog.modal.is-active`), so `_toast.scss`/`_snackbar.scss`
  still apply. Queue semantics stay: `queue` (default on) replaces the
  showing notice, `queue: false` stacks.

### Styles
- When the last component has moved, the Buefy sheets we still need
  (`_switch`, `_checkbox`, `_notices`, `_dialog`, `_loading`, `_tooltip`,
  `_dropdown`, `_tabs`, `_upload`, `_form`, `_select`, `_image`,
  `_numberinput`, `_autocomplete`, `_icon`, `_progress`, `_notification`)
  get copied into `assets/scss/ui/` with Buefy's MIT notice. The import of
  `@ntohq/buefy-next/src/scss/buefy` and the package go.

### Visual parity check
- Build `before` (master) and `after` (each commit) into separate www dirs.
  Serve each with `vue3-shots/tools/mock.cjs` on a spare port and shoot with
  `vue3-shots/tools/shoot.cjs`: login, desktop and 12 app windows, light and
  dark. Then `compare.py` gives a diff score per shot.
- `ui-buefy-shots/tools/notices.cjs` also shoots a toast of each type, the
  snackbar, a confirm dialog and a loading overlay.
- Stop the mock by its PID. Never `pkill -f`.
- Gate per commit: no shot changes beyond noise (clock/animation), no new
  console errors, full `vitest run` green.

### Order
1. Phase 2a: `src/ui` plugin and notices service (toast/snackbar/dialog),
   specs moved to `@/ui`.
2. Phase 2b, one commit each: icon, button, field+input, switch, checkbox,
   select, tooltip.
3. Phase 3: loading, image, dropdown(+item), tabs(+item), upload, progress,
   autocomplete, numberinput, notification. Then vendor the SCSS and remove
   the package.
