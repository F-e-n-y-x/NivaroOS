# Files App Ground-Up Rewrite — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the legacy full-page `filebrowser/FilePanel.vue` (and its ~40 sub-components) with a ground-up rebuilt `FilesApp.vue` that is fully at home inside `DesktopWindow.vue`'s resizable/movable window — window-local responsive breakpoints, in-window dialogs, a clamped context menu, and a real maximize button on the shared window chrome — while preserving every existing feature and talking to the exact same Go backend API.

**Architecture:** A thin `FilesApp.vue` shell owns a `ResizeObserver` on its own element and a small explicit "controller" object (provided via Vue's `provide`/`inject`) that exposes navigation, selection, clipboard, and view-mode state to its children — replacing the legacy `provide() { return { filePanel: this } }` God-component pattern, which injected an entire 1157-line component instance. Two pieces of state that are already global and genuinely cross-cutting (`currentPath`, `operateObject`/clipboard, `isViewGrid`) are reused from the existing flat Vuex store exactly as-is; everything Files-specific and ephemeral (listing, loading/error, selection, upload queue, sidebar-collapsed, active section) lives in `FilesApp`'s own local reactive state. All new components talk to the unchanged Go backend through the existing `$api.folder` / `$api.batch` / `$api.samba` / `$api.local_storage` service modules and the existing `simple-uploader.js` / Socket.IO wiring.

**Tech Stack:** Vue 2 (Options API), Buefy, Vuex (flat, non-namespaced store), `simple-uploader.js`, `socket.io-client` / `vue-socket.io-extended`, CodeMirror, `vitest` for pure-logic unit tests.

**Spec:** `docs/superpowers/specs/2026-08-21-files-app-design.md`

## Global Constraints

- Zero Go backend changes. Every new component calls the exact endpoints the legacy code already calls (see each task's **Interfaces**).
- No new global Vuex state except reusing the three keys that already exist and are already cross-cutting: `state.currentPath` (mutation `SET_CURRENT_PATH`), `state.operateObject` (mutation `SET_OPERATE_OBJECT`), `state.isViewGrid` (mutation `SET_IS_VIEW_GRID`). All other Files-specific state is local to `FilesApp.vue`, exposed to descendants via `provide`/`inject` of a small explicit controller object — never the raw component instance.
- No `window.innerWidth` / `100vh` / `document.getElementById` for layout decisions anywhere in new code. Every size-dependent decision reads from a `ResizeObserver`-driven width/height, scoped to the relevant element (either `FilesApp`'s own root, or a specific sub-container like the breadcrumb bar).
- New files live under `src/components/files/` (components) and `src/utils/files/` (pure logic). Existing shared infrastructure that other parts of the app also depend on - `src/mixins/mixin.js` (file-type/icon mapping, download, copy/cut, delete - also used by wallpaper features), `simple-uploader.js`, `socket.io-client`, `Network.js`'s WebRTC/WebSocket protocol - is reused, not rewritten.
- Every Vue component task ends with a documented manual verification in the running app (this repo has no `@vue/test-utils`; see the spec's Testing section). Every pure-logic task ends with a `vitest` test written first.
- Legacy `src/components/filebrowser/` stays untouched and fully working until the final cutover task. `src/components/fileList/FilePanel.vue` (unrelated icon-picker used by `IconInput.vue`) is never touched.
- Run `pnpm test` (vitest) after every logic task, and `pnpm lint` after every task, from `/root/casaos-fork/CasaOS-UI`.

---

### Task 1: Path utilities

**Files:**
- Create: `src/utils/files/path.js`
- Test: `src/utils/files/path.spec.js`

**Interfaces:**
- Produces: `baseName(path: string): string`, `parentPath(path: string): string | null`, `joinPath(dir: string, name: string): string` — used by Tasks 2, 9, 13.

- [ ] **Step 1: Write the failing tests**

```js
// src/utils/files/path.spec.js
import { expect, test, describe } from 'vitest'
import { baseName, parentPath, joinPath } from './path'

describe('baseName', () => {
	test.each([
		['/DATA', 'DATA'],
		['/DATA/tower', 'tower'],
		['/DATA/tower/photos', 'photos'],
	])('baseName(%s) -> %s', (input, expected) => {
		expect(baseName(input)).toBe(expected)
	})
})

describe('parentPath', () => {
	test.each([
		['/DATA', null],
		['/DATA/tower', '/DATA'],
		['/DATA/tower/photos', '/DATA/tower'],
	])('parentPath(%s) -> %s', (input, expected) => {
		expect(parentPath(input)).toBe(expected)
	})
})

describe('joinPath', () => {
	test('no trailing slash', () => {
		expect(joinPath('/DATA', 'tower')).toBe('/DATA/tower')
	})
	test('trailing slash on dir', () => {
		expect(joinPath('/DATA/', 'tower')).toBe('/DATA/tower')
	})
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run (from `/root/casaos-fork/CasaOS-UI`): `pnpm vitest run src/utils/files/path.spec.js`
Expected: FAIL with "Cannot find module './path'" or similar.

- [ ] **Step 3: Write the implementation**

```js
// src/utils/files/path.js
export function baseName(path) {
	const segments = path.split('/').filter(Boolean)
	return segments[segments.length - 1] || ''
}

export function parentPath(path) {
	const segments = path.split('/').filter(Boolean)
	if (segments.length <= 1) return null
	return '/' + segments.slice(0, -1).join('/')
}

export function joinPath(dir, name) {
	return dir.endsWith('/') ? `${dir}${name}` : `${dir}/${name}`
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `pnpm vitest run src/utils/files/path.spec.js`
Expected: PASS (7 tests).

- [ ] **Step 5: Commit**

```bash
cd /root/casaos-fork/CasaOS-UI
git add src/utils/files/path.js src/utils/files/path.spec.js
git commit -m "Add Files path utilities (baseName/parentPath/joinPath)"
```

---

### Task 2: Breadcrumb utility

**Files:**
- Create: `src/utils/files/breadcrumb.js`
- Test: `src/utils/files/breadcrumb.spec.js`

**Reference:** legacy algorithm in `src/components/filebrowser/components/FileBreadcrumb.vue:133-146` (`buildPathArray`) - a leading `"Root"` crumb with `path: "/"`, then one crumb per path segment. This task extracts only the pure array-building part; the DOM-measurement-based overflow/collapse behavior in the same file (`onResize`, lines 106-131) is ported live into Task 7 (`Toolbar.vue`), because it inherently needs real layout measurement, which isn't something to unit test per this project's convention (see Global Constraints).

**Interfaces:**
- Produces: `buildBreadcrumb(path: string): Array<{name: string, path: string}>` — used by Task 7.

- [ ] **Step 1: Write the failing tests**

```js
// src/utils/files/breadcrumb.spec.js
import { expect, test, describe } from 'vitest'
import { buildBreadcrumb } from './breadcrumb'

describe('buildBreadcrumb', () => {
	test('root path', () => {
		expect(buildBreadcrumb('/DATA')).toEqual([
			{ name: 'Root', path: '/' },
			{ name: 'DATA', path: '/DATA' },
		])
	})

	test('nested path', () => {
		expect(buildBreadcrumb('/DATA/tower/photos')).toEqual([
			{ name: 'Root', path: '/' },
			{ name: 'DATA', path: '/DATA' },
			{ name: 'tower', path: '/DATA/tower' },
			{ name: 'photos', path: '/DATA/tower/photos' },
		])
	})

	test('bare root', () => {
		expect(buildBreadcrumb('/')).toEqual([{ name: 'Root', path: '/' }])
	})
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `pnpm vitest run src/utils/files/breadcrumb.spec.js`
Expected: FAIL - module not found.

- [ ] **Step 3: Write the implementation**

```js
// src/utils/files/breadcrumb.js
export function buildBreadcrumb(path) {
	const normalized = path === '/' ? '' : path
	const segments = normalized.split('/').filter(Boolean)
	const crumbs = [{ name: 'Root', path: '/' }]
	let acc = ''
	for (const segment of segments) {
		acc = `${acc}/${segment}`
		crumbs.push({ name: segment, path: acc })
	}
	return crumbs
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `pnpm vitest run src/utils/files/breadcrumb.spec.js`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
cd /root/casaos-fork/CasaOS-UI
git add src/utils/files/breadcrumb.js src/utils/files/breadcrumb.spec.js
git commit -m "Add Files breadcrumb-building utility"
```

---

### Task 3: Breakpoint utility

**Files:**
- Create: `src/utils/files/breakpoints.js`
- Test: `src/utils/files/breakpoints.spec.js`

**Interfaces:**
- Produces: `classifyWidth(width: number): { sidebarCollapsed: boolean, toolbarCollapsed: boolean, singleColumnGrid: boolean }` — used by Tasks 5, 7, 8, 10.

- [ ] **Step 1: Write the failing tests**

```js
// src/utils/files/breakpoints.spec.js
import { expect, test, describe } from 'vitest'
import { classifyWidth } from './breakpoints'

describe('classifyWidth', () => {
	test('wide window: nothing collapsed', () => {
		expect(classifyWidth(900)).toEqual({
			sidebarCollapsed: false,
			toolbarCollapsed: false,
			singleColumnGrid: false,
		})
	})

	test('narrow window: sidebar + toolbar collapse, grid stays multi-column', () => {
		expect(classifyWidth(500)).toEqual({
			sidebarCollapsed: true,
			toolbarCollapsed: true,
			singleColumnGrid: false,
		})
	})

	test('at the window floor: grid drops to a single column too', () => {
		expect(classifyWidth(400)).toEqual({
			sidebarCollapsed: true,
			toolbarCollapsed: true,
			singleColumnGrid: true,
		})
	})

	test('boundary values are exclusive on the collapse threshold', () => {
		expect(classifyWidth(560).sidebarCollapsed).toBe(false)
		expect(classifyWidth(559).sidebarCollapsed).toBe(true)
		expect(classifyWidth(420).singleColumnGrid).toBe(false)
		expect(classifyWidth(419).singleColumnGrid).toBe(true)
	})
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `pnpm vitest run src/utils/files/breakpoints.spec.js`
Expected: FAIL - module not found.

- [ ] **Step 3: Write the implementation**

```js
// src/utils/files/breakpoints.js
const SIDEBAR_TOOLBAR_THRESHOLD = 560
const SINGLE_COLUMN_THRESHOLD = 420

export function classifyWidth(width) {
	const collapsed = width < SIDEBAR_TOOLBAR_THRESHOLD
	return {
		sidebarCollapsed: collapsed,
		toolbarCollapsed: collapsed,
		singleColumnGrid: width < SINGLE_COLUMN_THRESHOLD,
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `pnpm vitest run src/utils/files/breakpoints.spec.js`
Expected: PASS (4 tests, 7 assertions).

- [ ] **Step 5: Commit**

```bash
cd /root/casaos-fork/CasaOS-UI
git add src/utils/files/breakpoints.js src/utils/files/breakpoints.spec.js
git commit -m "Add Files window-width breakpoint classifier"
```

---

### Task 4: Selection utilities

**Files:**
- Create: `src/utils/files/selection.js`
- Test: `src/utils/files/selection.spec.js`

**Reference:** legacy behavior in `src/components/filebrowser/FilePanel.vue:631-674` (`handleSelect`, `handelListChange`) - selection is tracked as a "part of the list item objects" flag today (`item.isSelected`); this rewrite tracks it as a plain array of paths instead (simpler to reason about, and matches how `filesController.selection` will be read by `ContentView`, `Toolbar`, and `ContextMenu` independently without all three needing the same list-item object references).

**Interfaces:**
- Produces: `toggleSelect(selection: string[], path: string): string[]`, `selectRange(list: Array<{path: string}>, fromPath: string, toPath: string): string[]`, `summarize(list: Array<{path: string}>, selection: string[]): { count: number, total: number, state: 'none' | 'part' | 'all' }` — used by Task 10.

- [ ] **Step 1: Write the failing tests**

```js
// src/utils/files/selection.spec.js
import { expect, test, describe } from 'vitest'
import { toggleSelect, selectRange, summarize } from './selection'

describe('toggleSelect', () => {
	test('adds an unselected path', () => {
		expect(toggleSelect(['/a'], '/b')).toEqual(['/a', '/b'])
	})
	test('removes an already-selected path', () => {
		expect(toggleSelect(['/a', '/b'], '/a')).toEqual(['/b'])
	})
})

describe('selectRange', () => {
	const list = [{ path: '/a' }, { path: '/b' }, { path: '/c' }, { path: '/d' }]
	test('selects an inclusive forward range', () => {
		expect(selectRange(list, '/a', '/c')).toEqual(['/a', '/b', '/c'])
	})
	test('selects an inclusive reversed range', () => {
		expect(selectRange(list, '/c', '/a')).toEqual(['/a', '/b', '/c'])
	})
	test('single-item range when from equals to', () => {
		expect(selectRange(list, '/b', '/b')).toEqual(['/b'])
	})
})

describe('summarize', () => {
	const list = [{ path: '/a' }, { path: '/b' }]
	test('none selected', () => {
		expect(summarize(list, [])).toEqual({ count: 0, total: 2, state: 'none' })
	})
	test('some selected', () => {
		expect(summarize(list, ['/a'])).toEqual({ count: 1, total: 2, state: 'part' })
	})
	test('all selected', () => {
		expect(summarize(list, ['/a', '/b'])).toEqual({ count: 2, total: 2, state: 'all' })
	})
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `pnpm vitest run src/utils/files/selection.spec.js`
Expected: FAIL - module not found.

- [ ] **Step 3: Write the implementation**

```js
// src/utils/files/selection.js
export function toggleSelect(selection, path) {
	return selection.includes(path)
		? selection.filter((p) => p !== path)
		: [...selection, path]
}

export function selectRange(list, fromPath, toPath) {
	const paths = list.map((item) => item.path)
	let start = paths.indexOf(fromPath)
	let end = paths.indexOf(toPath)
	if (start === -1 || end === -1) return []
	if (start > end) [start, end] = [end, start]
	return paths.slice(start, end + 1)
}

export function summarize(list, selection) {
	const total = list.length
	const count = list.filter((item) => selection.includes(item.path)).length
	const state = count === 0 ? 'none' : count === total ? 'all' : 'part'
	return { count, total, state }
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `pnpm vitest run src/utils/files/selection.spec.js`
Expected: PASS (7 tests).

- [ ] **Step 5: Commit**

```bash
cd /root/casaos-fork/CasaOS-UI
git add src/utils/files/selection.js src/utils/files/selection.spec.js
git commit -m "Add Files selection utilities (path-array based)"
```

---

### Task 5: `FilesApp.vue` shell with controller + window-local sizing

**Files:**
- Create: `src/components/files/FilesApp.vue`
- Modify: `src/components/desktop/DesktopWindow.vue` (register `FilesApp` in `COMPONENT_REGISTRY`)
- Modify: `src/components/desktop/Dock.vue` (temporary second dock entry for development-only testing, removed in Task 20)

**Interfaces:**
- Consumes: `classifyWidth` from Task 3.
- Produces: the `filesController` object, injected by every component in Tasks 7-19:
  ```
  {
    currentPath: string,          // reactive, mirrors $store.state.currentPath
    breakpoints: { sidebarCollapsed, toolbarCollapsed, singleColumnGrid },
    sidebarCollapsed: boolean,    // user-toggleable, independent of breakpoint auto-collapse
    activeSection: 'browser' | 'shared' | 'drop',
    navigate(path: string): void,
    setActiveSection(section): void,
    toggleSidebar(): void,
  }
  ```

- [ ] **Step 1: Create the shell component**

```vue
<!-- src/components/files/FilesApp.vue -->
<template>
	<div ref="root" class="files-app">
		<slot></slot>
	</div>
</template>

<script>
import { classifyWidth } from '@/utils/files/breakpoints'

export default {
	name: 'files-app',
	provide() {
		return { filesController: this.controller }
	},
	data() {
		return {
			controller: {
				currentPath: this.$store.state.currentPath || '/DATA',
				breakpoints: classifyWidth(960),
				sidebarCollapsed: false,
				activeSection: 'browser',
				navigate: this.navigate,
				setActiveSection: this.setActiveSection,
				toggleSidebar: this.toggleSidebar,
			},
			resizeObserver: null,
		}
	},
	mounted() {
		this.resizeObserver = new ResizeObserver((entries) => {
			const width = entries[0].contentRect.width
			this.controller.breakpoints = classifyWidth(width)
		})
		this.resizeObserver.observe(this.$refs.root)
		if (!this.$store.state.currentPath) {
			this.navigate('/DATA')
		}
	},
	beforeDestroy() {
		this.resizeObserver && this.resizeObserver.disconnect()
	},
	methods: {
		navigate(path) {
			this.controller.currentPath = path
			this.$store.commit('SET_CURRENT_PATH', path)
		},
		setActiveSection(section) {
			this.controller.activeSection = section
		},
		toggleSidebar() {
			this.controller.sidebarCollapsed = !this.controller.sidebarCollapsed
		},
	},
}
</script>

<style lang="scss" scoped>
.files-app {
	display: flex;
	flex-direction: column;
	height: 100%;
	width: 100%;
	overflow: hidden;
	position: relative;
}
</style>
```

- [ ] **Step 2: Register in `DesktopWindow.vue`**

In `src/components/desktop/DesktopWindow.vue`, add the import and registry entry alongside the existing ones (do not remove `FilePanel` yet):

```js
import FilesApp from '@/components/files/FilesApp.vue'
```

```js
const COMPONENT_REGISTRY = {
	FilePanel,
	FilesApp,
	TerminalPanel,
	SettingsApp,
	LegacyAppEditPanel,
	VmManagerApp
}
```

- [ ] **Step 3: Add a temporary dev-only dock entry**

In `src/components/desktop/Dock.vue`, add a second pinned entry so the new app can be opened side-by-side with the working legacy one during development, without touching the real `files` id. Add to the `PINNED` array:

```js
const PINNED = [
	{ id: 'files', label: 'Files', icon: filesIcon },
	{ id: 'files-new', label: 'Files (New)', icon: filesIcon },
	{ id: 'terminal', label: 'Terminal', icon: terminalIcon },
	{ id: 'vms', label: 'VMs', icon: vmManagerIcon },
	{ id: 'settings', label: 'Settings', icon: settingsIcon }
]
```

And add a branch in `open(id)`:

```js
} else if (id === 'files-new') {
	this.$store.commit('OPEN_WINDOW', {
		id: 'files-new', title: 'Files (New)', component: 'FilesApp', width: 960, height: 620
	})
}
```

- [ ] **Step 4: Manual verification**

Run the dev server (`pnpm dev` from `CasaOS-UI`), open the desktop, click the new "Files (New)" dock icon. Confirm: a window opens showing an empty `.files-app` container, it drags by its titlebar, it resizes from all 8 handles down to the existing 360x280 floor, and shrinking it below 560px width doesn't error in the console (nothing visibly reacts yet - that's expected, `classifyWidth` output isn't consumed by anything until Task 7 onward).

- [ ] **Step 5: Commit**

```bash
cd /root/casaos-fork/CasaOS-UI
git add src/components/files/FilesApp.vue src/components/desktop/DesktopWindow.vue src/components/desktop/Dock.vue
git commit -m "Add FilesApp shell with window-local resize controller"
```

---

### Task 6: Maximize/restore for `DesktopWindow.vue`

**Files:**
- Modify: `src/components/desktop/DesktopWindow.vue`
- Modify: `src/store/mutations.js`
- Modify: `src/store/state.js` (none - `maximized`/pre-maximize rect live per-window inside the existing `windows[]` array entries, no new top-level state key)

**Interfaces:**
- Produces: mutation `TOGGLE_MAXIMIZE_WINDOW(state, id)`, and each `windows[]` entry gains optional fields `maximized: boolean`, `preMaximizeRect: {x,y,width,height} | null`. Existing consumers of `windows[]` entries (`Dock.vue`, `WindowManager.vue`) are unaffected since these are additive optional fields.

- [ ] **Step 1: Add the mutation**

In `src/store/mutations.js`, add near the other window mutations (alongside `UPDATE_WINDOW_RECT`, using the same `persistWindows(state)` call at the end that every other window mutation uses):

```js
TOGGLE_MAXIMIZE_WINDOW(state, id) {
	const win = state.windows.find(w => w.id === id)
	if (!win) return
	if (win.maximized) {
		const rect = win.preMaximizeRect
		win.x = rect.x
		win.y = rect.y
		win.width = rect.width
		win.height = rect.height
		win.maximized = false
		win.preMaximizeRect = null
	} else {
		win.preMaximizeRect = { x: win.x, y: win.y, width: win.width, height: win.height }
		win.x = 24
		win.y = 24
		win.width = Math.max(360, window.innerWidth - 48)
		win.height = Math.max(280, window.innerHeight - 96)
		win.maximized = true
	}
	persistWindows(state)
},
```

This is the one place in the whole rewrite that legitimately reads `window.innerWidth`/`innerHeight` - maximizing genuinely means "fill the desktop," which is a viewport-relative concept by definition, unlike the per-app layout decisions everywhere else in this plan.

- [ ] **Step 2: Add the button and wire it up in `DesktopWindow.vue`**

In the titlebar, before the existing minimize button:

```html
<button class="window-btn window-btn-maximize" :title="$t('Maximize')" @click.stop="toggleMaximize"></button>
```

Add `@dblclick="toggleMaximize"` to the existing `.window-titlebar` div (alongside its existing `@mousedown="startDrag"`).

Add the method:

```js
toggleMaximize() {
	this.$store.commit('TOGGLE_MAXIMIZE_WINDOW', this.win.id)
},
```

Add the button's style next to `.window-btn-minimize`/`.window-btn-close`:

```scss
.window-btn-maximize {
	background: #3dd06a;
}
```

- [ ] **Step 3: Manual verification**

Reopen "Files (New)" (or Terminal/Settings/VMs - this is shared chrome). Click the new green button: window snaps to ~90% of the desktop at (24,24). Click again: it returns to its exact previous position and size. Double-click the titlebar: same toggle. Reload the page while maximized (BACKLOG item #12's persistence): confirm it comes back maximized.

- [ ] **Step 4: Commit**

```bash
cd /root/casaos-fork/CasaOS-UI
git add src/store/mutations.js src/components/desktop/DesktopWindow.vue
git commit -m "Add maximize/restore to the shared window chrome"
```

---

### Task 7: `Toolbar.vue` — breadcrumb (with real overflow measurement) + actions

**Files:**
- Create: `src/components/files/Toolbar.vue`
- Modify: `src/components/files/FilesApp.vue` (render `Toolbar` inside the shell, above the content area)

**Reference:** breadcrumb overflow-measurement algorithm ported from `src/components/filebrowser/components/FileBreadcrumb.vue:106-131` (`onResize`), replacing its `document.getElementById` lookups with template `ref`s and its `AFTER_FILES_ENTER` event-bus trigger with the component's own `ResizeObserver` on the breadcrumb container - the measurement technique (a hidden "shadow" breadcrumb used to measure natural width, then hide middle crumbs into a dropdown until it fits) is correct as-is and is kept; only its trigger and DOM-access mechanism change.

**Interfaces:**
- Consumes: `filesController` (injected: `currentPath`, `navigate`, `breakpoints`) from Task 5; `buildBreadcrumb` from Task 2.
- Produces: emits `new-folder`, `new-file`, `upload`, `toggle-view` - consumed by Task 13 (dialogs) and Task 15 (uploader). No selection-aware buttons yet (added in Task 11 once `ContentView` owns selection).

- [ ] **Step 1: Write the component**

```vue
<!-- src/components/files/Toolbar.vue -->
<template>
	<header class="files-toolbar">
		<div ref="breadContainer" class="breadcrumb-bar">
			<template v-if="filesController.breakpoints.toolbarCollapsed">
				<b-dropdown v-if="hiddenCrumbs.length" aria-role="list">
					<template #trigger>
						<b-icon icon="dots-horizontal" custom-size="mdi-18px"></b-icon>
					</template>
					<b-dropdown-item v-for="c in hiddenCrumbs" :key="c.path" @click="go(c)">
						{{ $t(c.name) }}
					</b-dropdown-item>
				</b-dropdown>
				<span v-for="c in visibleCrumbs" :key="c.path" class="crumb" @click="go(c)">{{ $t(c.name) }}</span>
			</template>
			<template v-else>
				<span ref="liveCrumbs" class="live-crumbs">
					<b-dropdown v-if="hiddenCrumbs.length" aria-role="list">
						<template #trigger>
							<b-icon icon="dots-horizontal" custom-size="mdi-18px"></b-icon>
						</template>
						<b-dropdown-item v-for="c in hiddenCrumbs" :key="c.path" @click="go(c)">
							{{ $t(c.name) }}
						</b-dropdown-item>
					</b-dropdown>
					<span v-for="c in visibleCrumbs" :key="c.path" class="crumb" @click="go(c)">{{ $t(c.name) }}</span>
				</span>
				<span ref="shadowCrumbs" class="shadow-crumbs">
					<span v-for="c in crumbs" :key="'shadow-' + c.path" class="crumb">{{ $t(c.name) }}</span>
				</span>
			</template>
		</div>

		<div class="actions" :class="{ overflow: filesController.breakpoints.toolbarCollapsed }">
			<template v-if="filesController.breakpoints.toolbarCollapsed">
				<b-dropdown aria-role="list" position="is-bottom-left">
					<template #trigger>
						<b-icon icon="dots-vertical" custom-size="mdi-18px"></b-icon>
					</template>
					<b-dropdown-item aria-role="menuitem" @click="$emit('new-folder')">{{ $t('New Folder') }}</b-dropdown-item>
					<b-dropdown-item aria-role="menuitem" @click="$emit('new-file')">{{ $t('New File') }}</b-dropdown-item>
					<b-dropdown-item aria-role="menuitem" @click="$emit('upload')">{{ $t('Upload') }}</b-dropdown-item>
					<b-dropdown-item aria-role="menuitem" @click="$emit('toggle-view')">{{ $t('Change View') }}</b-dropdown-item>
				</b-dropdown>
			</template>
			<template v-else>
				<b-button size="is-small" icon-left="folder-plus-outline" @click="$emit('new-folder')">{{ $t('New Folder') }}</b-button>
				<b-button size="is-small" icon-left="file-plus-outline" @click="$emit('new-file')">{{ $t('New File') }}</b-button>
				<b-button size="is-small" icon-left="upload-outline" @click="$emit('upload')">{{ $t('Upload') }}</b-button>
				<b-tooltip :label="$t('Change View')" position="is-left" type="is-dark">
					<b-icon icon="view-grid-outline" class="is-clickable" @click.native="$emit('toggle-view')"></b-icon>
				</b-tooltip>
			</template>
		</div>
	</header>
</template>

<script>
import { buildBreadcrumb } from '@/utils/files/breadcrumb'

export default {
	name: 'files-toolbar',
	inject: ['filesController'],
	data() {
		return { hiddenCount: 0, resizeObserver: null }
	},
	computed: {
		crumbs() {
			return buildBreadcrumb(this.filesController.currentPath)
		},
		visibleCrumbs() {
			return this.hiddenCount === 0 ? this.crumbs : this.crumbs.slice(this.hiddenCount)
		},
		hiddenCrumbs() {
			return this.hiddenCount === 0 ? [] : this.crumbs.slice(0, this.hiddenCount)
		},
	},
	watch: {
		'filesController.currentPath'() {
			this.hiddenCount = 0
			this.$nextTick(this.measure)
		},
	},
	mounted() {
		this.resizeObserver = new ResizeObserver(() => this.measure())
		this.resizeObserver.observe(this.$refs.breadContainer)
		this.$nextTick(this.measure)
	},
	beforeDestroy() {
		this.resizeObserver && this.resizeObserver.disconnect()
	},
	methods: {
		go(crumb) {
			this.filesController.navigate(crumb.path)
		},
		measure() {
			if (this.filesController.breakpoints.toolbarCollapsed) return
			const container = this.$refs.breadContainer
			const shadow = this.$refs.shadowCrumbs
			if (!container || !shadow) return
			let hidden = 0
			while (shadow.scrollWidth > container.clientWidth && hidden < this.crumbs.length - 1) {
				hidden += 1
				this.hiddenCount = hidden
				this.$forceUpdate()
			}
			if (hidden === 0) this.hiddenCount = 0
		},
	},
}
</script>

<style lang="scss" scoped>
.files-toolbar {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: 0.5rem 0.75rem;
	border-bottom: 1px solid rgb(228 233 237);
	min-width: 0;
}
.breadcrumb-bar {
	flex: 1 1 auto;
	min-width: 0;
	display: flex;
	align-items: center;
	overflow: hidden;
	position: relative;
}
.shadow-crumbs {
	position: absolute;
	visibility: hidden;
	white-space: nowrap;
	pointer-events: none;
}
.crumb {
	cursor: pointer;
	padding: 0 0.35rem;
	white-space: nowrap;
	&:hover { text-decoration: underline; }
}
.actions {
	flex-shrink: 0;
	display: flex;
	gap: 0.5rem;
	align-items: center;
}
</style>
```

- [ ] **Step 2: Wire into `FilesApp.vue`**

```html
<div ref="root" class="files-app">
	<files-toolbar @new-folder="..." @new-file="..." @upload="..." @toggle-view="..."></files-toolbar>
	<slot></slot>
</div>
```

(the four `@`-handlers are left as empty no-ops wired to `console.log` for now - Tasks 13 and 15 replace them with real dialog/upload triggers; import and register `Toolbar` as `files-toolbar` in `FilesApp.vue`'s `components`.)

- [ ] **Step 3: Manual verification**

Open "Files (New)". Confirm the breadcrumb shows "Root / DATA" (since `currentPath` defaults to `/DATA` from Task 5). Resize the window narrower than 560px: action buttons collapse into a "⋮" dropdown. Manually set `filesController.currentPath` to a long nested path via Vue devtools and shrink the window width until the shadow measurement kicks in: confirm the middle crumbs collapse into a "…" dropdown and clicking a dropdown entry navigates (`filesController.currentPath` updates, visible in devtools - `ContentView` doesn't exist yet so nothing else reacts).

- [ ] **Step 4: Commit**

```bash
cd /root/casaos-fork/CasaOS-UI
git add src/components/files/Toolbar.vue src/components/files/FilesApp.vue
git commit -m "Add Files toolbar with window-local breadcrumb overflow"
```

---

### Task 8: `Sidebar.vue` shell (expanded / icon-rail)

**Files:**
- Create: `src/components/files/Sidebar.vue`
- Modify: `src/components/files/FilesApp.vue` (render `Sidebar` beside the main content column)

**Interfaces:**
- Consumes: `filesController` (injected: `sidebarCollapsed`, `toggleSidebar`, `breakpoints`, `activeSection`, `setActiveSection`) from Task 5.
- Produces: a named default slot (expanded mode) and a `#rail` slot (collapsed mode) so Task 9's `FolderTree`/`MountList` can render differently in each mode without `Sidebar` needing to know their internals.

- [ ] **Step 1: Write the component**

```vue
<!-- src/components/files/Sidebar.vue -->
<template>
	<aside class="files-sidebar" :class="{ collapsed: isCollapsed }">
		<div class="sidebar-header">
			<h3 v-if="!isCollapsed" class="title is-6 mb-0">{{ $t('Files') }}</h3>
			<b-icon
				:icon="isCollapsed ? 'chevron-right' : 'chevron-left'"
				custom-size="mdi-18px"
				class="is-clickable"
				@click.native="filesController.toggleSidebar()"
			></b-icon>
		</div>
		<div class="sidebar-body scrollbars-light">
			<slot v-if="!isCollapsed"></slot>
			<slot v-else name="rail"></slot>
		</div>
		<div class="sidebar-nav">
			<button
				class="nav-entry"
				:class="{ active: filesController.activeSection === 'shared' }"
				:title="$t('FilesShare')"
				@click="filesController.setActiveSection('shared')"
			>
				<b-icon icon="share-variant-outline" custom-size="mdi-18px"></b-icon>
				<span v-if="!isCollapsed">{{ $t('FilesShare') }}</span>
			</button>
			<button
				class="nav-entry"
				:class="{ active: filesController.activeSection === 'drop' }"
				:title="$t('FilesDrop')"
				@click="filesController.setActiveSection('drop')"
			>
				<b-icon icon="access-point" custom-size="mdi-18px"></b-icon>
				<span v-if="!isCollapsed">{{ $t('FilesDrop') }}</span>
			</button>
		</div>
	</aside>
</template>

<script>
export default {
	name: 'files-sidebar',
	inject: ['filesController'],
	computed: {
		isCollapsed() {
			return this.filesController.sidebarCollapsed || this.filesController.breakpoints.sidebarCollapsed
		},
	},
}
</script>

<style lang="scss" scoped>
.files-sidebar {
	flex-shrink: 0;
	width: 15rem;
	display: flex;
	flex-direction: column;
	border-right: 1px solid rgb(228 233 237);
	transition: width 0.15s ease;
	&.collapsed { width: 3.25rem; }
}
.sidebar-header {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: 0.6rem 0.75rem;
}
.sidebar-body {
	flex: 1 1 auto;
	overflow-y: auto;
	min-height: 0;
}
.sidebar-nav {
	flex-shrink: 0;
	border-top: 1px solid rgb(228 233 237);
	padding: 0.4rem;
}
.nav-entry {
	display: flex;
	align-items: center;
	gap: 0.5rem;
	width: 100%;
	padding: 0.4rem 0.5rem;
	border: none;
	background: none;
	cursor: pointer;
	border-radius: 6px;
	&:hover, &.active { background: rgba(0, 0, 0, 0.05); }
}
</style>
```

- [ ] **Step 2: Wire into `FilesApp.vue`**

Wrap the existing `<slot>` in a flex row alongside the new sidebar:

```html
<div ref="root" class="files-app">
	<files-toolbar ...></files-toolbar>
	<div class="files-body">
		<files-sidebar></files-sidebar>
		<slot></slot>
	</div>
</div>
```

```scss
.files-body {
	flex: 1 1 auto;
	display: flex;
	min-height: 0;
}
```

- [ ] **Step 3: Manual verification**

Open "Files (New)". Confirm the sidebar shows a "Files" header, an empty body, and two nav buttons (Share, Drop) with icons. Click the chevron: sidebar collapses to an icon rail (nav button labels disappear, header title disappears). Shrink the window below 560px width: sidebar auto-collapses even without clicking the chevron; widen it back above 560px: it auto-expands again (unless the user had manually collapsed it - re-widening after a manual collapse should NOT auto-force it back open, since `isCollapsed` is an OR of both flags; note this as expected behavior, not a bug, when verifying).

- [ ] **Step 4: Commit**

```bash
cd /root/casaos-fork/CasaOS-UI
git add src/components/files/Sidebar.vue src/components/files/FilesApp.vue
git commit -m "Add Files sidebar shell with icon-rail collapse"
```

---

### Task 9: `FolderTree.vue` + `MountList.vue`

**Files:**
- Create: `src/components/files/FolderTree.vue`
- Create: `src/components/files/MountList.vue`
- Modify: `src/components/files/Sidebar.vue` usage site in `FilesApp.vue` (pass these as the default slot content)

**Reference:** port the API-calling logic verbatim from `src/components/filebrowser/sidebar/TreeList.vue` (folder tree: expand/collapse, active-path highlighting, `$api.folder.getList` per node) and `src/components/filebrowser/sidebar/MountList.vue` (mounted-disk list, `$api.local_storage` calls, merge-storage indicator). Both are being rewritten as components (per the approved "full rewrite" scope), but the *backend calls and data shapes* are unchanged - use the existing files as the reference for exactly which endpoints to call and how to interpret the response, adapting only: (a) navigation now calls `filesController.navigate(path)` instead of emitting through the `GOTO` event bus event, and (b) active-path comparison reads `filesController.currentPath` instead of a `path`/`isActive` prop pair.

**Interfaces:**
- Consumes: `filesController` (injected: `currentPath`, `navigate`) from Task 5; `$api.folder.getList`, `$api.local_storage.get` (service module paths from `src/service/folder.js`, `src/service/local_storage.js`).
- Produces: nothing consumed by later tasks directly - these are leaf UI components.

- [ ] **Step 1: Write `FolderTree.vue`**

Build this by reading `src/components/filebrowser/sidebar/TreeList.vue` in full and porting its tree-expansion state machine and `$api.folder.getList(path)` calls node-by-node exactly as it does today (lazy-load children on expand, cache loaded children, show a folder icon per legacy's `getFileIcon`-style logic from `src/mixins/mixin.js:80-125` - reuse that mixin's `getFileIcon` method rather than reimplementing icon-mapping). Replace:
- the `path`/`autoLoad`/`isActive` props with direct reads from `inject: ['filesController']`
- any `this.$EventBus.$emit(events.GOTO, {...})` call with `this.filesController.navigate(path)`
- the root path with `filesController.currentPath` for highlighting the active node

- [ ] **Step 2: Write `MountList.vue`**

Port from `src/components/filebrowser/sidebar/MountList.vue` following the same rule: same `$api.local_storage.get`/merge-status calls, same disk-icon/label logic, navigation calls `filesController.navigate(path)` instead of the event bus.

- [ ] **Step 3: Wire into `Sidebar.vue`'s usage site**

In `FilesApp.vue`:

```html
<files-sidebar>
	<folder-tree></folder-tree>
	<mount-list></mount-list>
	<template #rail>
		<folder-tree rail></folder-tree>
	</template>
</files-sidebar>
```

(`FolderTree` accepts a boolean `rail` prop that renders icon-only nodes with no labels/children, for the collapsed icon-rail mode from Task 8.)

- [ ] **Step 4: Manual verification**

Open "Files (New)". Confirm the sidebar tree loads the real `/DATA` folder structure from the backend (same folders visible in the legacy Files app), expanding/collapsing nodes works, and the mount list shows the box's real mounted disks (including `tower`/`tank`/`blue` from the SMB fix earlier, if those paths are also local mounts under `/DATA`). Click a folder: `filesController.currentPath` updates (check via devtools, and confirm the Task 7 breadcrumb updates to match).

- [ ] **Step 5: Commit**

```bash
cd /root/casaos-fork/CasaOS-UI
git add src/components/files/FolderTree.vue src/components/files/MountList.vue src/components/files/FilesApp.vue
git commit -m "Add Files sidebar folder tree and mount list"
```

---

### Task 10: `ContentView.vue` core (listing, empty/error, grid+list render)

**Files:**
- Create: `src/components/files/ContentView.vue`
- Create: `src/components/files/GridItem.vue`
- Create: `src/components/files/ListRow.vue`
- Modify: `src/components/files/FilesApp.vue` (render `ContentView` as the main content area, only when `activeSection === 'browser'`)

**Reference:** fetch/response-shaping logic ported verbatim from `src/components/filebrowser/FilePanel.vue:562-606` (`getFileList`) - same `$api.folder.getList(path)` call, same field mapping (`date`, `is_dir`, `name`, `path`, `size`, `write`, `extensions`), same hidden-file filter (`!item.name.startsWith('.')`), same `orderBy(filterList, ['is_dir'], ['desc'])` sort (directories first). Empty/error visuals reuse the existing `EmptyHolder.vue`/`ErrorHolder.vue` from `src/components/filebrowser/components/` (import directly - these two are simple, presentational, and already window-agnostic, so they're the one pair of legacy sub-components reused as-is rather than rewritten, consistent with the spec calling out third-party/already-agnostic dependencies as exceptions).

**Interfaces:**
- Consumes: `filesController` (injected: `currentPath`, `navigate`, `breakpoints`, `activeSection`) from Task 5; `$api.folder.getList` from `src/service/folder.js`; `state.isViewGrid` / `SET_IS_VIEW_GRID` from the existing global store.
- Produces: local `listing: Array<{name,path,is_dir,size,date,write,extensions}>` and `loading`/`error` state, plus a `dblclick-item` internal handler that calls `filesController.navigate` for directories - this is the last core piece before Task 11 adds selection on top of it.

- [ ] **Step 1: Write `ContentView.vue`**

```vue
<!-- src/components/files/ContentView.vue -->
<template>
	<section class="content-view" v-show="filesController.activeSection === 'browser'">
		<b-loading v-model="loading" :is-full-page="false"></b-loading>
		<error-holder v-if="error" :error="error"></error-holder>
		<empty-holder v-else-if="!loading && listing.length === 0" @newFile="$emit('new-file')" @newFolder="$emit('new-folder')"></empty-holder>
		<div v-else class="items" :class="[viewMode, { 'single-column': filesController.breakpoints.singleColumnGrid }]">
			<template v-if="viewMode === 'grid'">
				<grid-item v-for="item in listing" :key="item.path" :item="item" @open="openItem"></grid-item>
			</template>
			<template v-else>
				<list-row v-for="item in listing" :key="item.path" :item="item" @open="openItem"></list-row>
			</template>
		</div>
	</section>
</template>

<script>
import orderBy from 'lodash/orderBy'
import EmptyHolder from '@/components/filebrowser/components/EmptyHolder.vue'
import ErrorHolder from '@/components/filebrowser/components/ErrorHolder.vue'
import GridItem from './GridItem.vue'
import ListRow from './ListRow.vue'

export default {
	name: 'files-content-view',
	inject: ['filesController'],
	components: { EmptyHolder, ErrorHolder, GridItem, ListRow },
	data() {
		return { listing: [], loading: true, error: '' }
	},
	computed: {
		viewMode() {
			return this.$store.state.isViewGrid ? 'grid' : 'list'
		},
	},
	watch: {
		'filesController.currentPath': {
			immediate: true,
			handler(path) {
				this.fetchListing(path)
			},
		},
	},
	methods: {
		fetchListing(path) {
			this.loading = true
			this.$api.folder
				.getList(path)
				.then((res) => {
					this.loading = false
					if (res.data.success === 200) {
						const mapped = res.data.data.content.map((item) => ({
							date: item.date,
							is_dir: item.is_dir,
							name: item.name,
							path: item.path,
							size: item.size,
							write: item.write,
							extensions: item.extensions,
						}))
						const visible = mapped.filter((item) => !item.name.startsWith('.'))
						this.listing = orderBy(visible, ['is_dir'], ['desc'])
						this.error = ''
					}
				})
				.catch((err) => {
					this.loading = false
					this.listing = []
					this.error = err.response ? err.response.data.data : String(err)
				})
		},
		reload() {
			this.fetchListing(this.filesController.currentPath)
		},
		openItem(item) {
			if (item.is_dir) {
				this.filesController.navigate(item.path)
			} else {
				this.$emit('open-file', item)
			}
		},
	},
}
</script>

<style lang="scss" scoped>
.content-view {
	flex: 1 1 auto;
	min-width: 0;
	min-height: 0;
	overflow: auto;
	position: relative;
	padding: 0.75rem;
}
.items.grid {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(6.5rem, 1fr));
	gap: 0.75rem;
	&.single-column { grid-template-columns: 1fr; }
}
.items.list {
	display: flex;
	flex-direction: column;
}
</style>
```

- [ ] **Step 2: Write `GridItem.vue`**

Port the per-item template (icon via `getFileIcon` from `src/mixins/mixin.js`, thumbnail via `hasThumb`/`getThumbUrl` for image types) from `src/components/filebrowser/components/GirdView.vue:20-70`, trimmed to a single item (no list-level drag-select yet - that's Task 11) with props `item: Object`, emitting `open` on double-click/tap.

- [ ] **Step 3: Write `ListRow.vue`**

Same porting approach from `src/components/filebrowser/components/ListView.vue`, one row per item, columns for name/size/date, emitting `open` on double-click.

- [ ] **Step 4: Wire into `FilesApp.vue`**

```html
<files-sidebar>...</files-sidebar>
<files-content-view ref="contentView" @open-file="onOpenFile"></files-content-view>
```

(`onOpenFile` is a no-op stub until Task 18's viewers exist.)

- [ ] **Step 5: Manual verification**

Open "Files (New)". Confirm the real `/DATA` folder contents load (same files/folders visible in the legacy Files app), directories are sorted first, double-clicking a folder navigates into it and updates the breadcrumb, and the grid switches to a single column below 420px width. Temporarily point `currentPath` at a path you know 404s (devtools) to confirm the error state renders; navigate to a genuinely empty directory to confirm the empty state renders.

- [ ] **Step 6: Commit**

```bash
cd /root/casaos-fork/CasaOS-UI
git add src/components/files/ContentView.vue src/components/files/GridItem.vue src/components/files/ListRow.vue src/components/files/FilesApp.vue
git commit -m "Add Files content view: listing fetch, grid/list render, empty/error states"
```

---

### Task 11: Selection + drag-select + selection-aware toolbar

**Files:**
- Modify: `src/components/files/ContentView.vue`, `src/components/files/GridItem.vue`, `src/components/files/ListRow.vue`
- Modify: `src/components/files/Toolbar.vue`

**Reference:** drag-select rectangle behavior ported from `src/components/filebrowser/components/GirdView.vue` (`onDragSelectionStart` and its mousemove/mouseup handlers - search for that method name in the file) and shift/ctrl-click semantics ported from `src/components/filebrowser/FilePanel.vue:631-674`.

**Interfaces:**
- Consumes: `toggleSelect`, `selectRange`, `summarize` from Task 4.
- Produces: `ContentView` exposes `selection: string[]` and `clearSelection()` via a `$refs.contentView` ref from `FilesApp.vue` (already established in Task 10's template) - Task 12 (context menu) and Task 15 (batch actions) read/call these through that ref. `Toolbar` gains a `selection-summary` prop.

- [ ] **Step 1: Add selection state and interaction handlers to `ContentView.vue`**

```js
data() {
	return { listing: [], loading: true, error: '', selection: [], lastClickedPath: null }
},
computed: {
	summary() {
		return summarize(this.listing, this.selection)
	},
},
methods: {
	// ...existing methods...
	onItemClick(item, event) {
		if (event.shiftKey && this.lastClickedPath) {
			this.selection = selectRange(this.listing, this.lastClickedPath, item.path)
		} else if (event.ctrlKey || event.metaKey) {
			this.selection = toggleSelect(this.selection, item.path)
		} else {
			this.selection = [item.path]
		}
		this.lastClickedPath = item.path
	},
	selectAll() {
		this.selection = this.listing.map((item) => item.path)
	},
	clearSelection() {
		this.selection = []
	},
},
```

Import `toggleSelect`, `selectRange`, `summarize` from `@/utils/files/selection`. Pass `:selected="selection.includes(item.path)"` to each `grid-item`/`list-row` and listen for a new `@select="onItemClick(item, $event)"` emit from each (emitted from their existing click handler, passing the native event through).

- [ ] **Step 2: Add drag-select rectangle**

Port `onDragSelectionStart` from `src/components/filebrowser/components/GirdView.vue` into `ContentView.vue`'s root `.items` element (`@mousedown.left.prevent="onDragSelectionStart"`), keeping its rectangle-intersection math as-is; replace whatever it currently mutates (`item.isSelected` flags) with building a new `this.selection` array from the intersecting items' paths instead.

- [ ] **Step 3: Wire selection summary into `Toolbar.vue`**

Add a `selection-summary` prop (`{ count, total, state }`) to `Toolbar.vue`. When `count > 0`, replace the breadcrumb bar with a "N selected" label plus Copy/Move/Download/Delete buttons (same icons/labels as legacy `src/components/filebrowser/components/OperationToolbar.vue`) that emit `copy-selection`, `move-selection`, `download-selection`, `delete-selection`, `clear-selection`. In `FilesApp.vue`, pass `:selection-summary="$refs.contentView && $refs.contentView.summary"` (guard for the ref not existing on first render) and wire those five new emits to call the corresponding methods on `$refs.contentView` (`clearSelection`) - the four action emits are stubbed as no-ops until Task 15 implements the actual `$api.batch` calls.

- [ ] **Step 4: Manual verification**

Click an item: selected (visually highlighted - add a `.selected` class keyed off the `selected` prop in `GridItem`/`ListRow`). Shift-click another: range selected. Ctrl-click a third: added to selection without clearing the others. Toolbar switches to the "N selected" bar with the four action buttons. Drag a rectangle over multiple items from empty space: they become selected. Click "Clear" (or empty space without dragging): selection clears and the toolbar reverts to breadcrumb mode.

- [ ] **Step 5: Commit**

```bash
cd /root/casaos-fork/CasaOS-UI
git add src/components/files/ContentView.vue src/components/files/GridItem.vue src/components/files/ListRow.vue src/components/files/Toolbar.vue src/components/files/FilesApp.vue
git commit -m "Add Files multi-select (click/shift/ctrl/drag-rect) and selection toolbar"
```

---

### Task 12: `ContextMenu.vue` (window-clamped) + item actions

**Files:**
- Create: `src/components/files/ContextMenu.vue`
- Modify: `src/components/files/ContentView.vue`, `src/components/files/GridItem.vue`, `src/components/files/ListRow.vue`

**Reference:** menu item list and action wiring ported from `src/components/filebrowser/components/ContextMenu.vue` (rename/copy/cut/download/delete/detail/new-share entries) and the shared `operate`/`deleteItem`/`downloadFile`/`getPanelType` methods from `src/mixins/mixin.js:146-331` (reused as a mixin, not rewritten - see Global Constraints). The one deliberate behavior change is positioning: the legacy version sets `top/left` directly from `event.clientX`/`clientY` (`src/components/filebrowser/components/ContextMenu.vue:192-197`), which is wrong once the container isn't anchored at the page origin. This task computes position relative to `FilesApp`'s own bounding rect instead, and clamps it so the menu never renders outside the window.

**Interfaces:**
- Consumes: `filesController` (injected: for the "New Share" action's current path) from Task 5; `mixin` from `src/mixins/mixin.js` (`operate`, `deleteItem`, `downloadFile`, `getPanelType`).
- Produces: emits `rename-request(item)`, `detail-request(item)` - consumed by Task 14's dialogs.

- [ ] **Step 1: Write `ContextMenu.vue`**

```vue
<!-- src/components/files/ContextMenu.vue -->
<template>
	<div v-if="visible" ref="menu" class="files-context-menu" :style="{ top: y + 'px', left: x + 'px' }">
		<button class="menu-item" @click="act('open')">{{ $t('Open') }}</button>
		<button class="menu-item" @click="act('rename')">{{ $t('Rename') }}</button>
		<button class="menu-item" @click="act('copy')">{{ $t('Copy') }}</button>
		<button class="menu-item" @click="act('cut')">{{ $t('Cut') }}</button>
		<button class="menu-item" @click="act('download')">{{ $t('Download') }}</button>
		<button class="menu-item" @click="act('detail')">{{ $t('Detail') }}</button>
		<button class="menu-item is-danger" @click="act('delete')">{{ $t('Delete') }}</button>
	</div>
</template>

<script>
import { mixin } from '@/mixins/mixin'

const MENU_WIDTH = 160
const MENU_HEIGHT = 224

export default {
	name: 'files-context-menu',
	mixins: [mixin],
	inject: ['filesController'],
	data() {
		return { visible: false, x: 0, y: 0, item: null }
	},
	methods: {
		open(event, item, boundsEl) {
			this.item = item
			const bounds = boundsEl.getBoundingClientRect()
			const rawX = event.clientX - bounds.left
			const rawY = event.clientY - bounds.top
			this.x = Math.min(rawX, bounds.width - MENU_WIDTH)
			this.y = Math.min(rawY, bounds.height - MENU_HEIGHT)
			this.visible = true
		},
		close() {
			this.visible = false
		},
		act(action) {
			switch (action) {
				case 'rename':
					this.$emit('rename-request', this.item)
					break
				case 'detail':
					this.$emit('detail-request', this.item)
					break
				case 'copy':
					this.operate('copy', this.item)
					break
				case 'cut':
					this.operate('cut', this.item)
					break
				case 'download':
					this.downloadFile(this.item)
					break
				case 'delete':
					this.deleteItem(this.item)
					this.$emit('reload')
					break
				case 'open':
					this.$emit('open-request', this.item)
					break
			}
			this.close()
		},
	},
}
</script>

<style lang="scss" scoped>
.files-context-menu {
	position: absolute;
	z-index: 50;
	background: #fff;
	border-radius: 6px;
	box-shadow: 0 4px 16px rgba(0, 0, 0, 0.2);
	padding: 0.25rem;
	min-width: 160px;
}
.menu-item {
	display: block;
	width: 100%;
	text-align: left;
	padding: 0.4rem 0.6rem;
	border: none;
	background: none;
	cursor: pointer;
	border-radius: 4px;
	&:hover { background: rgba(0, 0, 0, 0.06); }
	&.is-danger { color: #f2534a; }
}
</style>
```

Note `deleteItem` from the mixin already calls `reload()` internally if the host component defines it as a method (see `src/mixins/mixin.js:309-311`: `if (typeof this.reload === 'function') this.reload()`) - since `ContextMenu.vue` itself has no `reload` method, the explicit `this.$emit('reload')` after `deleteItem` above is what actually triggers `ContentView.reload()` in Step 2 below; this is intentional, not a duplicate of the mixin's internal check.

- [ ] **Step 2: Wire into `ContentView.vue`**

Add `<files-context-menu ref="ctxMenu" @reload="reload" @rename-request="$emit('rename-request', $event)" @detail-request="$emit('detail-request', $event)" @open-request="openItem"></files-context-menu>` inside `ContentView.vue`'s template (positioned as a direct child of `.content-view`, which must have `position: relative` - already set in Task 10). Add `@contextmenu.prevent="$refs.ctxMenu.open($event, item, $el)"` to `GridItem.vue`/`ListRow.vue` (passing `this.$el.closest('.content-view')` as `boundsEl` - `ContentView`'s own root, not `FilesApp`'s, since that's the scrollable clipping container the menu must stay inside).

- [ ] **Step 3: Manual verification**

Right-click a file near the bottom-right corner of a small (360x280) Files window: confirm the menu appears fully inside the window, not clipped or offset into the desktop behind it (this is the concrete regression test for the legacy `clientX`/`clientY` bug). Try Rename, Copy+paste (paste isn't wired until Task 15, so just confirm `SET_OPERATE_OBJECT` fires - check via devtools), Download (confirm a download starts), and Delete (confirm a confirmation flow - reuse whatever `deleteItem`'s existing `$buefy.dialog.confirm` call does, unchanged from the mixin).

- [ ] **Step 4: Commit**

```bash
cd /root/casaos-fork/CasaOS-UI
git add src/components/files/ContextMenu.vue src/components/files/ContentView.vue src/components/files/GridItem.vue src/components/files/ListRow.vue
git commit -m "Add window-clamped Files context menu"
```

---

### Task 13: `DialogOverlay.vue` + New Folder / New File dialogs

**Files:**
- Create: `src/components/files/DialogOverlay.vue`
- Create: `src/components/files/dialogs/NewFolderDialog.vue`
- Create: `src/components/files/dialogs/NewFileDialog.vue`
- Modify: `src/components/files/FilesApp.vue` (host the dialogs, replace the Task 7 stub handlers)

**Reference:** validation rules and API calls ported from `src/components/filebrowser/modals/NewFolderModal.vue` (`$api.folder.create`) and `src/components/filebrowser/modals/NewFileModal.vue` (`$api.file.create`) - same duplicate-name and empty-name validation messages, same success/error toasts.

**Interfaces:**
- Consumes: `joinPath` from Task 1; `filesController.currentPath` from Task 5.
- Produces: `DialogOverlay` is a generic shell reused again in Task 14 - it accepts a `title` prop and renders its default slot inside a card, confined to `FilesApp`'s bounds; emits `close`.

- [ ] **Step 1: Write `DialogOverlay.vue`**

```vue
<!-- src/components/files/DialogOverlay.vue -->
<template>
	<div class="dialog-overlay" @click.self="$emit('close')">
		<div class="dialog-card">
			<header class="dialog-header">
				<span class="dialog-title">{{ title }}</span>
				<b-icon icon="close-outline" pack="casa" class="is-clickable" @click.native="$emit('close')"></b-icon>
			</header>
			<div class="dialog-body">
				<slot></slot>
			</div>
		</div>
	</div>
</template>

<script>
export default {
	name: 'files-dialog-overlay',
	props: { title: { type: String, required: true } },
}
</script>

<style lang="scss" scoped>
.dialog-overlay {
	position: absolute;
	inset: 0;
	background: rgba(0, 0, 0, 0.25);
	display: flex;
	align-items: center;
	justify-content: center;
	z-index: 40;
}
.dialog-card {
	background: #fff;
	border-radius: 8px;
	width: min(22rem, calc(100% - 2rem));
	max-height: calc(100% - 2rem);
	overflow: auto;
	box-shadow: 0 8px 24px rgba(0, 0, 0, 0.3);
}
.dialog-header {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: 0.75rem 1rem;
	border-bottom: 1px solid rgb(228 233 237);
}
.dialog-body { padding: 1rem; }
</style>
```

`FilesApp.vue`'s root element (`.files-app`) must be `position: relative` (already set in Task 5) for `inset: 0` here to confine correctly to the window rather than the viewport.

- [ ] **Step 2: Write `NewFolderDialog.vue`**

Port the validation and submit logic from `src/components/filebrowser/modals/NewFolderModal.vue` in full (read that file - it's 154 lines - and carry over its exact validation messages and the `$api.folder.create(joinPath(currentPath, folderName))` call), wrapped in `<files-dialog-overlay :title="$t('New Folder')">` instead of a Buefy modal-card. Props: `current-path: String`. Emits `created` (parent reloads the listing) and `close`.

- [ ] **Step 3: Write `NewFileDialog.vue`**

Same approach from `src/components/filebrowser/modals/NewFileModal.vue` (`$api.file.create`).

- [ ] **Step 4: Wire into `FilesApp.vue`**

```html
<new-folder-dialog v-if="activeDialog === 'new-folder'" :current-path="controller.currentPath" @created="onDialogCreated" @close="activeDialog = null"></new-folder-dialog>
<new-file-dialog v-if="activeDialog === 'new-file'" :current-path="controller.currentPath" @created="onDialogCreated" @close="activeDialog = null"></new-file-dialog>
```

Add `activeDialog: null` to `FilesApp.vue`'s `data()`. Replace the Task 7 stub `@new-folder`/`@new-file` handlers on `<files-toolbar>` with `activeDialog = 'new-folder'` / `activeDialog = 'new-file'`. `onDialogCreated` calls `this.$refs.contentView.reload()` and sets `activeDialog = null`.

- [ ] **Step 5: Manual verification**

Click "New Folder": dialog appears centered inside the Files window (not the full browser viewport - confirm by making the window small and dragging it to a corner; the dialog must stay inside it). Create a folder: it appears in the listing after the dialog closes. Try creating one with a duplicate name: confirm the same validation error message as the legacy modal. Repeat for "New File".

- [ ] **Step 6: Commit**

```bash
cd /root/casaos-fork/CasaOS-UI
git add src/components/files/DialogOverlay.vue src/components/files/dialogs/NewFolderDialog.vue src/components/files/dialogs/NewFileDialog.vue src/components/files/FilesApp.vue
git commit -m "Add in-window New Folder/New File dialogs"
```

---

### Task 14: Rename + Detail dialogs

**Files:**
- Create: `src/components/files/dialogs/RenameDialog.vue`
- Create: `src/components/files/dialogs/DetailDialog.vue`
- Modify: `src/components/files/FilesApp.vue`, `src/components/files/ContextMenu.vue` (already emits `rename-request`/`detail-request` from Task 12)

**Reference:** `src/components/filebrowser/modals/RenameModal.vue` (`$api.folder.rename`/`$api.file.rename` depending on `is_dir`) and `src/components/filebrowser/modals/DetailModal.vue` (read-only file metadata display - name, size via `renderSize` from `src/mixins/mixin.js`, date, path).

- [ ] **Step 1: Write `RenameDialog.vue`**

Port validation/submit from `RenameModal.vue`, wrapped in `<files-dialog-overlay :title="$t('Rename')">`. Props: `item: Object`. Emits `renamed`, `close`.

- [ ] **Step 2: Write `DetailDialog.vue`**

Port the read-only display from `DetailModal.vue`, wrapped in `<files-dialog-overlay :title="$t('Detail')">`. Props: `item: Object`. Emits `close`.

- [ ] **Step 3: Wire into `FilesApp.vue`**

```html
<rename-dialog v-if="activeDialog === 'rename'" :item="dialogItem" @renamed="onDialogCreated" @close="activeDialog = null"></rename-dialog>
<detail-dialog v-if="activeDialog === 'detail'" :item="dialogItem" @close="activeDialog = null"></detail-dialog>
```

Add `dialogItem: null` to `FilesApp.vue`'s `data()`. Handle `ContentView`'s bubbled `rename-request`/`detail-request` events (from Task 12) by setting `dialogItem = item; activeDialog = 'rename' | 'detail'`.

- [ ] **Step 4: Manual verification**

Right-click a file → Rename: dialog opens in-window, pre-filled with the current name; submitting renames it and the listing refreshes. Right-click → Detail: shows correct name/size/date/path for that exact file.

- [ ] **Step 5: Commit**

```bash
cd /root/casaos-fork/CasaOS-UI
git add src/components/files/dialogs/RenameDialog.vue src/components/files/dialogs/DetailDialog.vue src/components/files/FilesApp.vue
git commit -m "Add in-window Rename/Detail dialogs"
```

---

### Task 15: `UploadTray.vue` + drag-drop upload + batch clipboard actions

**Files:**
- Create: `src/components/files/UploadTray.vue`
- Modify: `src/components/files/ContentView.vue` (drag-drop target, paste handling)
- Modify: `src/components/files/Toolbar.vue`'s wiring in `FilesApp.vue` (implement the four selection-action stubs from Task 11)

**Reference:** `simple-uploader.js` wiring ported from `src/components/filebrowser/FilePanel.vue:728-791` (`getTargetUrl`, `setUploaderOpts`) - same target URL (`/v2/casaos/file/upload`), same `Authorization` header from `$store.state.access_token`, same `fileAdded`/`dragover`/`uploadStart`/`complete`/`fileError` event handlers. Clipboard paste ported from `src/components/filebrowser/FilePanel.vue:694-712` (`paste`) - unchanged use of `state.operateObject`/`SET_OPERATE_OBJECT` and `$api.batch.task`.

**Interfaces:**
- Consumes: `state.operateObject`/`SET_OPERATE_OBJECT` (existing global store) from Task 5's constraint; `$api.batch.task`/`$api.batch.delete` from `src/service/batch.js`.
- Produces: nothing consumed later - this closes out the core browser feature set (upload, copy/cut/paste, batch delete/download).

- [ ] **Step 1: Write `UploadTray.vue`**

Port the uploader instantiation and its event handlers from `FilePanel.vue:728-791` into a component that wraps `simple-uploader.js` the same way `src/components/filebrowser/uploader/components/uploader.vue` does today (same library, new wrapper - see Global Constraints), rendering a docked progress list (per-file name + percentage + error badge) `position: absolute; bottom: 0; left: 0; right: 0` within `ContentView`'s bounds rather than the legacy's viewport-relative placement. Props: `current-path: String`. Emits `uploaded` (parent reloads).

- [ ] **Step 2: Wire drag-drop and paste into `ContentView.vue`**

Add `@dragover.prevent`, `@drop.prevent="onDrop"` to `.content-view`'s root element, forwarding dropped files to the `UploadTray`'s uploader instance (`assignDrop`-equivalent, or feed `event.dataTransfer.files` directly to `uploaderInstance.addFiles`). Add a `keyup`/`paste` listener scoped to `ContentView`'s root (not `document`, unlike the legacy global listener) that calls a new `paste()` method: reads `$store.state.operateObject`, calls `$api.batch.task({...operateObject, to: filesController.currentPath, style: 'overwrite'})`, then `SET_OPERATE_OBJECT(null)` and `reload()` on success.

- [ ] **Step 3: Mix `src/mixins/mixin.js` into `FilesApp.vue`**

`FilesApp.vue` is about to call `this.operate`/`this.downloadFile`/`this.deleteItem` directly (this step), and Task 18/19 add `this.getPanelType`/`this.downloadFile` calls on it too - none of those exist on `FilesApp.vue` yet (Task 5's skeleton doesn't include the mixin; only Task 12's standalone `ContextMenu.vue` mixed it in for its own use). Add it once here, at the top-level component all later tasks build on:

```js
import { mixin } from '@/mixins/mixin'

export default {
	name: 'files-app',
	mixins: [mixin],
	// ...existing provide()/data()/mounted()/methods from Task 5 unchanged...
}
```

- [ ] **Step 4: Implement the four batch-selection actions**

In `FilesApp.vue`, replace the Task 11 stub handlers for `copy-selection`/`move-selection`/`download-selection`/`delete-selection`:
- `copy-selection`/`move-selection`: call `this.operate('copy'|'cut', selectedItems)` (from `src/mixins/mixin.js`, mixed into `FilesApp.vue`) with the actual item objects (map `$refs.contentView.selection` paths back to `$refs.contentView.listing` entries).
- `download-selection`: call `this.downloadFile(selectedItems)` (same mixin, already handles both single-item and array cases per `src/mixins/mixin.js:167-193`).
- `delete-selection`: call `this.deleteItem(selectedItems)` (same mixin), then `$refs.contentView.reload()` and `$refs.contentView.clearSelection()`.

- [ ] **Step 5: Manual verification**

Drag a real file from the desktop file manager onto the Files window content area: upload progress appears in the docked tray, file appears in the listing on completion. Select 2+ files, click "Copy" in the selection toolbar, navigate to a different folder, press Ctrl+V (or Cmd+V): files are copied there and the listing refreshes. Select files and click "Delete": confirmation dialog appears (from the mixin, unchanged), files are removed on confirm.

- [ ] **Step 6: Commit**

```bash
cd /root/casaos-fork/CasaOS-UI
git add src/components/files/UploadTray.vue src/components/files/ContentView.vue src/components/files/FilesApp.vue
git commit -m "Add Files upload tray, drag-drop upload, and batch copy/move/download/delete"
```

---

### Task 16: `SharedView.vue` (Samba shares section)

**Files:**
- Create: `src/components/files/SharedView.vue`
- Create: `src/components/files/dialogs/ShareSelectDialog.vue`
- Modify: `src/components/files/FilesApp.vue` (render `SharedView` when `activeSection === 'shared'`)

**Reference:** `src/components/filebrowser/shared/ShareListPage.vue` (list + unshare) and `src/components/filebrowser/shared/SelectShareModal.vue` (folder picker + `$api.samba.createShare`). These now correctly list the shares fixed live on the box earlier in this conversation (`DATA`/`tower`/`tank`/`blue`, all now rows in `o_shares` and returned by `GET /v1/samba/shares`).

**Interfaces:**
- Consumes: `$api.samba.getShares`/`createShare`/`deleteShare` from `src/service/samba.js`; `filesController.activeSection` from Task 5.

- [ ] **Step 1: Write `SharedView.vue`**

Port the list-rendering and unshare-confirmation logic from `ShareListPage.vue`, fetching via `$api.samba.getShares()` on `activeSection` becoming `'shared'` (watch it, don't fetch eagerly on mount, matching the legacy lazy-section-switch behavior). Each row shows the share name/path and an "Unshare" action calling `$api.samba.deleteShare(id)` then refetching.

- [ ] **Step 2: Write `ShareSelectDialog.vue`**

Port from `SelectShareModal.vue`, wrapped in `<files-dialog-overlay :title="$t('Share a folder')">`: a folder picker (reuse `FolderTree.vue` from Task 9 in a picker mode - add a `picker` prop to `FolderTree.vue` that emits `pick(path)` instead of navigating) plus a "Share" button calling `$api.samba.createShare({ path })`.

- [ ] **Step 3: Wire into `FilesApp.vue`**

```html
<files-shared-view v-show="controller.activeSection === 'shared'" @add-share="activeDialog = 'share-select'"></files-shared-view>
<share-select-dialog v-if="activeDialog === 'share-select'" @created="activeDialog = null" @close="activeDialog = null"></share-select-dialog>
```

- [ ] **Step 4: Manual verification**

Click the sidebar's "Share" nav entry: the browser view swaps to the shares list, showing `DATA`/`tower`/`tank`/`blue` (confirming the earlier SMB DB fix is correctly surfaced here). Click "Unshare" on one: confirmation, then it's removed from both this list and (separately, out of scope to re-verify here) `smb.conf`. Add a new share via the picker: it appears in the list.

- [ ] **Step 5: Commit**

```bash
cd /root/casaos-fork/CasaOS-UI
git add src/components/files/SharedView.vue src/components/files/dialogs/ShareSelectDialog.vue src/components/files/FolderTree.vue src/components/files/FilesApp.vue
git commit -m "Add Files Shared section (Samba share list/create/unshare)"
```

---

### Task 17: `DropView.vue` (LAN Drop section)

**Files:**
- Create: `src/components/files/DropView.vue`
- Modify: `src/components/files/FilesApp.vue` (render `DropView` when `activeSection === 'drop'`)

**Reference:** UI wrapper rewritten fresh; the WebRTC/WebSocket peer-discovery protocol in `src/components/filebrowser/drop/Network.js` is imported and used exactly as today (per Global Constraints - this is a wire protocol, not layout code). Port the peer-list rendering and file-send interaction from `src/components/filebrowser/drop/DropPage.vue`, dropping its full-page header/close-button chrome (the window itself already has a titlebar/close button) and its `isDesktop`-conditional circular layout in favor of a simple responsive peer grid that respects `filesController.breakpoints.singleColumnGrid` the same way Task 10's file grid does.

**Interfaces:**
- Consumes: `ServerConnection` (and related classes) from `src/components/filebrowser/drop/Network.js`, unchanged.

- [ ] **Step 1: Write `DropView.vue`**

Instantiate the same `ServerConnection` from `Network.js`, render discovered peers as a simple grid of clickable peer tiles (name + device-type icon), clicking one opens a file picker and sends via the same protocol calls `DropPage.vue` uses today (find and reuse its send-file method verbatim).

- [ ] **Step 2: Wire into `FilesApp.vue`**

```html
<files-drop-view v-show="controller.activeSection === 'drop'"></files-drop-view>
```

- [ ] **Step 3: Manual verification**

Click the sidebar's "Drop" nav entry: view swaps to the drop section. If a second device on the LAN has the legacy or new Drop page open, confirm it appears as a discovered peer and a test file send completes (this needs two devices/browser tabs - verify with two browser tabs on the same machine at minimum, since `Network.js`'s discovery is same-network-based, not same-tab-based).

- [ ] **Step 4: Commit**

```bash
cd /root/casaos-fork/CasaOS-UI
git add src/components/files/DropView.vue src/components/files/FilesApp.vue
git commit -m "Add Files Drop section (LAN transfer UI, protocol unchanged)"
```

---

### Task 18: `ViewerChrome.vue` + Image/Video/Markdown viewers

**Files:**
- Create: `src/components/files/viewers/ViewerChrome.vue`
- Create: `src/components/files/viewers/ImageViewer.vue`
- Create: `src/components/files/viewers/VideoPlayer.vue`
- Create: `src/components/files/viewers/MarkdownEditor.vue`
- Modify: `src/components/files/FilesApp.vue` (host the active viewer, wire `ContentView`'s `open-file` emit from Task 10 to `getPanelType` from the mixin)

**Reference:** `getPanelType`/`filePanelMap` from `src/mixins/mixin.js:29-37,127-136` decides which viewer opens for a given file extension - port this mapping decision unchanged (including its existing quirk: `.md` files are NOT in `filePanelMap`'s unions today, so they fall through to the Detail dialog rather than opening `MarkdownEditor` - preserve this exactly as the legacy app already behaves, don't silently "fix" it as part of a layout rewrite). Viewer internals ported from `src/components/filebrowser/viewers/ImageViewer.vue`, `VideoPlayer.vue`, `MarkdownEditor.vue` respectively - same libraries, same load logic, new chrome/positioning only.

**Interfaces:**
- Consumes: `getPanelType` from `src/mixins/mixin.js` (mixed into `FilesApp.vue`).
- Produces: `ViewerChrome.vue` is a shared wrapper (title bar with back/download/close, fills `position:absolute; inset:0` within `FilesApp`) reused again in Task 19.

- [ ] **Step 1: Write `ViewerChrome.vue`**

```vue
<!-- src/components/files/viewers/ViewerChrome.vue -->
<template>
	<div class="viewer-chrome">
		<header class="viewer-header">
			<b-icon icon="arrow-left" custom-size="mdi-18px" class="is-clickable" @click.native="$emit('close')"></b-icon>
			<span class="viewer-title one-line">{{ item.name }}</span>
			<b-icon icon="download-outline" custom-size="mdi-18px" class="is-clickable" @click.native="$emit('download')"></b-icon>
		</header>
		<div class="viewer-body">
			<slot></slot>
		</div>
	</div>
</template>

<script>
export default {
	name: 'files-viewer-chrome',
	props: { item: { type: Object, required: true } },
}
</script>

<style lang="scss" scoped>
.viewer-chrome {
	position: absolute;
	inset: 0;
	background: #1e1e1e;
	display: flex;
	flex-direction: column;
	z-index: 30;
}
.viewer-header {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	gap: 0.75rem;
	padding: 0.5rem 0.75rem;
	color: #fff;
	background: #262626;
}
.viewer-title { flex: 1 1 auto; min-width: 0; }
.viewer-body {
	flex: 1 1 auto;
	min-height: 0;
	overflow: auto;
	display: flex;
	align-items: center;
	justify-content: center;
}
</style>
```

- [ ] **Step 2: Write `ImageViewer.vue` and `VideoPlayer.vue`**

Port from `src/components/filebrowser/viewers/ImageViewer.vue` and `VideoPlayer.vue` (both are near-trivial `<img>`/`<video>` wrappers pointed at the file-content URL, same as `src/mixins/mixin.js`'s `getFileUrl`), wrapped in `<files-viewer-chrome :item="item" @close="$emit('close')" @download="...">`.

- [ ] **Step 3: Write `MarkdownEditor.vue`**

Port from `src/components/filebrowser/viewers/MarkdownEditor.vue` (same markdown editor library usage), same chrome wrapping. Per the Reference note above, this viewer is built but - matching existing behavior exactly - is not currently reachable from a file click (only from wherever else `filePanelMap` might route to it, if anywhere; verify by grep for other invokers before assuming it's fully dead code).

- [ ] **Step 4: Wire into `FilesApp.vue`**

```js
methods: {
	onOpenFile(item) {
		const type = this.getPanelType(item) // from the mixin
		if (type) {
			this.activeViewerItem = item
			this.activeViewerType = type
		} else {
			this.dialogItem = item
			this.activeDialog = 'detail'
		}
	},
}
```

```html
<image-viewer v-if="activeViewerType === 'image-viewer'" :item="activeViewerItem" @close="activeViewerType = null" @download="downloadFile(activeViewerItem)"></image-viewer>
<video-player v-if="activeViewerType === 'video-player'" :item="activeViewerItem" @close="activeViewerType = null" @download="downloadFile(activeViewerItem)"></video-player>
```

(`MarkdownEditor` has no `filePanelMap` entry per the preserved quirk above, so it isn't wired into this dispatch table - it exists as a component for Task 19's remaining viewers to follow the same pattern, and in case a future task re-enables it.)

- [ ] **Step 5: Manual verification**

Click an image file: opens full-window (or maximized, per Task 6) image viewer with working back/download buttons, confined to the Files window's bounds even when the window isn't maximized. Click a video file: same, with playback controls working. Confirm `.md` files still fall through to the Detail dialog, matching current production behavior.

- [ ] **Step 6: Commit**

```bash
cd /root/casaos-fork/CasaOS-UI
git add src/components/files/viewers/ViewerChrome.vue src/components/files/viewers/ImageViewer.vue src/components/files/viewers/VideoPlayer.vue src/components/files/viewers/MarkdownEditor.vue src/components/files/FilesApp.vue
git commit -m "Add Files viewer chrome + image/video/markdown viewers"
```

---

### Task 19: Remaining viewers (CodeEditor, DocViewer, ExcelViewer, PdfViewer)

**Files:**
- Create: `src/components/files/viewers/CodeEditor.vue`
- Create: `src/components/files/viewers/DocViewer.vue`
- Create: `src/components/files/viewers/ExcelViewer.vue`
- Create: `src/components/files/viewers/PdfViewer.vue`
- Modify: `src/components/files/FilesApp.vue` (add these four to the dispatch table from Task 18)

**Reference:** CodeMirror mode imports and edit/save logic ported from `src/components/filebrowser/viewers/CodeEditor.vue` in full (it's the most complex viewer - read the whole file, not just a snippet, before porting; same CodeMirror mode list, same `$api.file.getContent`/`$api.file.update` calls). `DocViewer.vue`/`ExcelViewer.vue`/`PdfViewer.vue` ported the same way from their respective legacy files - same rendering libraries, wrapped in `ViewerChrome` from Task 18 instead of their legacy full-viewport positioning.

- [ ] **Step 1: Write `CodeEditor.vue`**

Port from `src/components/filebrowser/viewers/CodeEditor.vue` in full: same CodeMirror mode imports, same `$api.file.getContent(path)` load and `$api.file.update(path, content)` save, wrapped in `<files-viewer-chrome>`. Add a Save button to the chrome for this viewer specifically (the base `ViewerChrome` only has back/download - either add an optional named slot to `ViewerChrome.vue` for extra header actions, or a `showSave`/`@save` prop; pick the slot approach for consistency with Vue's composition idioms already used in `ViewerChrome`).

- [ ] **Step 2: Write `DocViewer.vue`, `ExcelViewer.vue`, `PdfViewer.vue`**

Port each from its legacy counterpart in `src/components/filebrowser/viewers/`, same library usage, wrapped in `ViewerChrome`.

- [ ] **Step 3: Wire into `FilesApp.vue`**'s dispatch table

```html
<code-editor v-if="activeViewerType === 'code-editor'" :item="activeViewerItem" @close="activeViewerType = null" @download="downloadFile(activeViewerItem)"></code-editor>
<doc-viewer v-if="activeViewerType === 'doc-viewer'" :item="activeViewerItem" @close="activeViewerType = null" @download="downloadFile(activeViewerItem)"></doc-viewer>
<excel-viewer v-if="activeViewerType === 'excel-viewer'" :item="activeViewerItem" @close="activeViewerType = null" @download="downloadFile(activeViewerItem)"></excel-viewer>
<pdf-viewer v-if="activeViewerType === 'pdf-viewer'" :item="activeViewerItem" @close="activeViewerType = null" @download="downloadFile(activeViewerItem)"></pdf-viewer>
```

- [ ] **Step 4: Manual verification**

Open one file of each remaining type (a code file, a `.doc`/`.docx`, a `.xls`/`.xlsx`, a `.pdf`). Confirm each renders correctly inside the Files window at both a small window size and maximized (Task 6), and that `CodeEditor`'s Save button actually persists an edit (verify by editing, saving, closing, reopening the same file).

- [ ] **Step 5: Commit**

```bash
cd /root/casaos-fork/CasaOS-UI
git add src/components/files/viewers/CodeEditor.vue src/components/files/viewers/DocViewer.vue src/components/files/viewers/ExcelViewer.vue src/components/files/viewers/PdfViewer.vue src/components/files/viewers/ViewerChrome.vue src/components/files/FilesApp.vue
git commit -m "Add remaining Files viewers: code editor, doc, excel, PDF"
```

---

### Task 20: Cutover and legacy cleanup

**Files:**
- Modify: `src/components/desktop/DesktopWindow.vue` (registry: `files` id now resolves to `FilesApp`)
- Modify: `src/components/desktop/Dock.vue` (remove the temporary `files-new` entry from Task 5)
- Delete: `src/components/filebrowser/` (entire directory)
- Modify: `BACKLOG.md` (mark item #11 fully DONE)

**Interfaces:** none - this is the final integration task, no new interfaces.

- [ ] **Step 1: Repoint the real `files` window id**

In `src/components/desktop/DesktopWindow.vue`'s `COMPONENT_REGISTRY`, remove the `FilePanel` entry (and its now-dead special-case `mounted()` hook at lines 65-79 that calls `this.$refs.content.init()` - `FilesApp` doesn't need this workaround since Task 10's `ContentView` fetches on `currentPath`'s `immediate: true` watcher, not a two-call init dance). In `src/components/desktop/Dock.vue`'s `open('files')` branch, change `component: 'FilePanel'` to `component: 'FilesApp'`; remove the `AFTER_FILES_ENTER` event-bus emit (dead - nothing in the new components listens for it) and the entire `files-new` entry added in Task 5 (both from `PINNED` and from `open(id)`).

- [ ] **Step 2: Delete the legacy tree**

```bash
cd /root/casaos-fork/CasaOS-UI
git rm -r src/components/filebrowser
```

Grep the whole `src/` tree for any remaining reference to `filebrowser/` to confirm nothing else imports from it (the only known other consumer, `IconInput.vue`, imports from `fileList/FilePanel.vue`, a different directory - confirm this with `grep -rn "components/filebrowser" src/` and expect zero remaining hits before deleting).

- [ ] **Step 3: Update `BACKLOG.md`**

Mark item #11 as `— DONE (2026-08-21)` with a one-paragraph summary matching the style of the other completed items (see #4, #5, #6, #8, #9 for the existing format), referencing this plan's spec.

- [ ] **Step 4: Full manual regression pass**

With only the real "Files" dock icon now present (no more "Files (New)"), re-verify the complete feature set end to end in one pass: open, navigate via sidebar tree/mounts/breadcrumb, grid/list toggle, select (click/shift/ctrl/drag), copy/cut/paste, rename, delete, new folder/file, upload via drag-drop, download, all 7 viewers, Shared section (including the earlier SMB fix's 4 shares), Drop section, resize from all 8 handles down to 360x280, drag to move, minimize/restore, maximize/restore, and reload-persistence of window position/size/maximized state. Fix anything broken before considering this task done - do not commit a broken cutover.

- [ ] **Step 5: Commit**

```bash
cd /root/casaos-fork/CasaOS-UI
git add -A
git commit -m "Cut over Files window to the new FilesApp; delete legacy filebrowser tree"
cd /root/casaos-fork
git add BACKLOG.md
git commit -m "Mark BACKLOG item 11 (Files windowed-mode redesign) DONE"
```
