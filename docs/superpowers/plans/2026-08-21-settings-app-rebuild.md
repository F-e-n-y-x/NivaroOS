# Settings App Rebuild Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rebuild CasaOS-UI's Settings app as a modular, window-size-responsive shell covering 6 categories (adding Storage & Disks, Network Shares, Remote Access, and About/Updates), and fix the window-chassis bug (unthrottled drag/resize persistence) that makes windows in this fork feel janky.

**Architecture:** `SettingsApp.vue` becomes a thin shell (`SettingsNav` + `SettingsSearch` + a `ResizeObserver`-driven content pane) that switches between section components under `src/components/settings/sections/`: Appearance, Users & Access (which also covers the logged-in user's own account — see the mid-execution IA ruling below), General, Network & Sharing, Storage, System. Each section either wraps an existing panel (Appearance, Users, General) or is new UI over already-running, already-wrapped backend endpoints (Network, Storage, System/About) — though per a later user decision, the relocated panels get a real visual redesign, not just a structural move (see the design-pass note below). `DesktopWindow.vue` gets a `requestAnimationFrame`-batched drag/resize (persisting to `localStorage` once on mouseup instead of every pixel); window maximize/restore already exists from unrelated prior work and was removed per explicit user decision (see Global Constraints).

**Tech Stack:** Vue 2 (Options API) + Buefy, Vuex, vitest for pure-logic unit tests (no Vue component test harness exists in this repo — see Global Constraints).

**Spec:** `docs/superpowers/specs/2026-08-21-settings-app-rebuild-design.md`

## Global Constraints

- No new Go backend code, with one named exception: Task 8 (Remote Access) adds a small new route to the CasaOS backend for Tailscale, since — unlike every other new panel in this plan — there is no existing route to consume. Every other new panel consumes routes/services that already exist and already run (verified against source in `CasaOS/route/v1/*.go` and `CasaOS-LocalStorage/route/v1/*.go` + `route/v2/*.go`).
- Match existing code style exactly: **tab indentation** in all `.vue`/`.js` files (verified against existing files — this repo does not use spaces).
- This repo has no Vue component test harness (only `vitest` unit tests over pure JS — see `src/mixins/file_utils.spec.js`, `src/api/vmSidecar.spec.js`). New pure-logic modules (window rect math, breakpoint classification, search filtering, the new `tailscale.js` service) get real `vitest` specs. New Vue components (panels, sections, the shell) are verified manually through the running app — do not invent a component-testing setup that doesn't match repo convention.
- Remote Access uses Tailscale, not ZeroTier — Tailscale is confirmed installed, logged in, and actively connected on this box (`tailscale status` shows a live tailnet with peers), so `RemoteAccessPanel.vue` shows real status/peers directly, with no install-detection empty state needed (unlike the originally-considered ZeroTier design, since `zerotier-one` was confirmed not installed). This is the one task in the plan touching the Go backend (see the Global Constraints exception above) and requiring a CasaOS binary rebuild + `systemctl restart casaos` — confirmed with the user before this task runs, given it briefly interrupts the live WebUI.
- mergerfs (`EnableMergerFS`) is confirmed `false` in `/etc/casaos/local-storage.conf` on this box — `StoragePoolsPanel.vue` must degrade to an informational state when the merges endpoint 503s, not assume it's enabled.
- Do not touch `SideBar.vue`, `Home.vue`, or `src/widgets/Network.vue` — this was unrelated in-progress work at spec time; it has since landed on `main` (commit `9550662`) as part of an unrelated "Files app rewrite" wave and remains out of scope here.
- **Window maximize/restore already exists** — added in commit `ec5b52e` ("Add maximize/restore to the shared window chrome"), after this plan's design phase. It lives entirely in Vuex (`TOGGLE_MAXIMIZE_WINDOW` mutation in `src/store/mutations.js`, storing `maximized`/`preMaximizeRect` directly on the window object) plus a button already in `DesktopWindow.vue`'s template. **Do not reimplement this.** This plan's window-chassis work (Task 1) is scoped to the one bug that survived that wave: `UPDATE_WINDOW_RECT` still commits — and persists to `localStorage` — on every raw `mousemove` during drag/resize.
- The Files app rewrite also established a `src/utils/<app>/breakpoints.js` convention for window-size-driven responsive layout (see `src/utils/files/breakpoints.js`: a pure `classifyWidth(width)` returning a flags object, `ResizeObserver` in the shell's `mounted()` measuring `entries[0].contentRect.width`). Settings' own breakpoint utility (Task 2) follows this exact shape for consistency rather than inventing a divergent one.
- Files' own "Shared" panel (`src/components/files/SharedView.vue` + `dialogs/ShareSelectDialog.vue`, commit `3301d4e`) already manages SMB shares via `$api.samba`, and its commit message documents the verified real payload contract: `createShare` takes an array of `{path, anonymous}` objects, not a bare `{path}`. Settings' `NetworkSharesPanel.vue` (Task 9) is a separate, simpler surface (no folder-tree picker) but must send the same verified payload shape, since it's the same backend resource — a share created from either surface shows up in both.
- Search (`SettingsSearch.vue`) implements jump-to-section only in this plan, not per-row scroll+highlight — the design doc's "highlight" phrase is descoped here since it requires per-row DOM anchors that don't exist yet; call this out explicitly rather than silently dropping it (see Task 3).
- **IA change, decided mid-execution (after Tasks 1-8 were dispatched):** Account is no longer a separate top-level section — the user found it redundant next to Users & Access and asked for it folded in. `AccountSection.vue` (Task 5's output) becomes dead code and is deleted; the logged-in user's own profile (`AccountPanel.vue`, unchanged) becomes a 4th tab/option inside `UsersSection.vue` (Task 6's output, amended by the new Task 11 below) alongside CasaOS/System/SMB Users. The nav is 6 categories, not 7: Users & Access, Appearance, Network & Sharing, Storage, General, System.
- **Relocated panels get a real redesign, not just a structural move — decided mid-execution (after Tasks 5-7 were already complete).** The original design's premise — "existing panel visuals aren't broken, only structure/bugs are" — turned out wrong: the user found concrete problems in exactly the panels Tasks 5-7 relocated verbatim (a wallpaper-embedded-mode layout glitch, inverted transparency-slider labels — both already fixed directly, not through a task — and the Users & Access tab switcher's visual design called out as "very bad"). Tasks 5-7 are NOT being redone individually; instead, once every section is wired into the shell (end of Task 12, the shell-rewrite task), a dedicated visual-design pass (not yet a numbered task — needs the fully-assembled app to review against, via actual screenshots/browser automation) revisits every section's visual design, including what Tasks 5-7 relocated as-is.

---

### Task 1: Fix window drag/resize persistence (throttle to mouseup)

**Files:**
- Modify: `src/store/mutations.js`
- Create: `src/store/mutations.spec.js`
- Modify: `src/components/desktop/DesktopWindow.vue`

**Interfaces:**
- Produces: Vuex mutation `PERSIST_WINDOWS(state)` — writes the current `state.windows` to `localStorage` (same shape `persistWindows` already produces). Called from `DesktopWindow.vue`'s drag/resize `mouseup` handlers in this same task.
- Consumes: existing `persistWindows(state)` helper and `UPDATE_WINDOW_RECT` mutation already in `src/store/mutations.js`.

- [ ] **Step 1: Write the failing tests for the persistence split**

Create `src/store/mutations.spec.js`:

```js
import { describe, test, expect, beforeEach, vi } from 'vitest'
import mutations from './mutations'

function makeState(win) {
	return { windows: [win], nextWindowZIndex: 1 }
}

describe('window mutations persistence', () => {
	beforeEach(() => {
		global.localStorage = { setItem: vi.fn(), getItem: vi.fn() }
	})

	test('UPDATE_WINDOW_RECT updates the rect without touching localStorage', () => {
		const win = { id: 'settings', title: 'Settings', component: 'SettingsApp', x: 0, y: 0, width: 900, height: 600, zIndex: 1, minimized: false }
		const state = makeState(win)
		mutations.UPDATE_WINDOW_RECT(state, { id: 'settings', x: 10, y: 20, width: 950, height: 650 })
		expect(state.windows[0]).toMatchObject({ x: 10, y: 20, width: 950, height: 650 })
		expect(global.localStorage.setItem).not.toHaveBeenCalled()
	})

	test('PERSIST_WINDOWS writes the persistable window fields to localStorage', () => {
		const win = { id: 'settings', title: 'Settings', component: 'SettingsApp', x: 10, y: 20, width: 950, height: 650, zIndex: 1, minimized: false }
		const state = makeState(win)
		mutations.PERSIST_WINDOWS(state)
		expect(global.localStorage.setItem).toHaveBeenCalledTimes(1)
		const [key, json] = global.localStorage.setItem.mock.calls[0]
		expect(key).toBe('casaos_open_windows')
		expect(JSON.parse(json)).toEqual([{ id: 'settings', title: 'Settings', component: 'SettingsApp', x: 10, y: 20, width: 950, height: 650, minimized: false }])
	})

	test('CLOSE_WINDOW still persists immediately (unaffected by the drag/resize throttle fix)', () => {
		const win = { id: 'settings', title: 'Settings', component: 'SettingsApp', x: 0, y: 0, width: 900, height: 600, zIndex: 1, minimized: false }
		const state = makeState(win)
		mutations.CLOSE_WINDOW(state, 'settings')
		expect(global.localStorage.setItem).toHaveBeenCalledTimes(1)
		expect(state.windows).toHaveLength(0)
	})
})
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /root/casaos-fork/CasaOS-UI && pnpm test mutations.spec.js`
Expected: FAIL — `PERSIST_WINDOWS` doesn't exist yet, and `UPDATE_WINDOW_RECT` currently DOES call `localStorage.setItem` (so the first test fails too).

- [ ] **Step 3: Split persistence out of `UPDATE_WINDOW_RECT` in `src/store/mutations.js`**

Find:

```js
	UPDATE_WINDOW_RECT(state, { id, x, y, width, height }) {
		const win = state.windows.find(w => w.id === id)
		if (!win) return
		if (x !== undefined) win.x = x
		if (y !== undefined) win.y = y
		if (width !== undefined) win.width = width
		if (height !== undefined) win.height = height
		persistWindows(state)
	},
```

Replace with:

```js
	UPDATE_WINDOW_RECT(state, { id, x, y, width, height }) {
		const win = state.windows.find(w => w.id === id)
		if (!win) return
		if (x !== undefined) win.x = x
		if (y !== undefined) win.y = y
		if (width !== undefined) win.width = width
		if (height !== undefined) win.height = height
	},

	// Drag/resize call UPDATE_WINDOW_RECT on every mousemove for a smooth
	// visual, but persisting to localStorage on every pixel of movement
	// is real jank - callers commit this once on mouseup instead.
	PERSIST_WINDOWS(state) {
		persistWindows(state)
	},
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `pnpm test mutations.spec.js`
Expected: PASS (all 3 tests)

- [ ] **Step 5: Throttle `DesktopWindow.vue`'s drag handler to one `requestAnimationFrame` per frame, persist once on mouseup**

In `src/components/desktop/DesktopWindow.vue`, replace the `startDrag` method:

```js
		startDrag(e) {
			this.focus()
			const startX = e.clientX
			const startY = e.clientY
			const originX = this.win.x
			const originY = this.win.y
			let pending = null
			let frame = null

			const flush = () => {
				frame = null
				if (pending) this.$store.commit('UPDATE_WINDOW_RECT', pending)
			}

			const onMove = moveEvent => {
				const dx = moveEvent.clientX - startX
				const dy = moveEvent.clientY - startY
				const maxX = Math.max(0, window.innerWidth - this.win.width)
				const maxY = Math.max(0, window.innerHeight - this.win.height)
				pending = {
					id: this.win.id,
					x: Math.min(maxX, Math.max(0, originX + dx)),
					y: Math.min(maxY, Math.max(0, originY + dy))
				}
				if (!frame) frame = requestAnimationFrame(flush)
			}
			const onUp = () => {
				window.removeEventListener('mousemove', onMove)
				window.removeEventListener('mouseup', onUp)
				if (frame) cancelAnimationFrame(frame)
				if (pending) this.$store.commit('UPDATE_WINDOW_RECT', pending)
				this.$store.commit('PERSIST_WINDOWS')
			}
			window.addEventListener('mousemove', onMove)
			window.addEventListener('mouseup', onUp)
		},
```

- [ ] **Step 6: Apply the same throttle pattern to `startResize`**

Replace the `startResize` method:

```js
		startResize(direction, e) {
			this.focus()
			const startX = e.clientX
			const startY = e.clientY
			const originWidth = this.win.width
			const originHeight = this.win.height
			const originX = this.win.x
			const originY = this.win.y

			const affectsRight = ['right', 'corner-br', 'corner-tr'].includes(direction)
			const affectsLeft = ['left', 'corner-tl', 'corner-bl'].includes(direction)
			const affectsBottom = ['bottom', 'corner-br', 'corner-bl'].includes(direction)
			const affectsTop = ['top', 'corner-tl', 'corner-tr'].includes(direction)

			let pending = null
			let frame = null
			const flush = () => {
				frame = null
				if (pending) this.$store.commit('UPDATE_WINDOW_RECT', pending)
			}

			const onMove = moveEvent => {
				const dx = moveEvent.clientX - startX
				const dy = moveEvent.clientY - startY
				const rect = { id: this.win.id }

				if (affectsRight) {
					const maxWidth = Math.max(MIN_WIDTH, window.innerWidth - originX)
					rect.width = Math.min(maxWidth, Math.max(MIN_WIDTH, originWidth + dx))
				} else if (affectsLeft) {
					const maxWidth = Math.max(MIN_WIDTH, originX + originWidth)
					const newWidth = Math.min(maxWidth, Math.max(MIN_WIDTH, originWidth - dx))
					rect.width = newWidth
					rect.x = Math.max(0, originX + originWidth - newWidth)
				}

				if (affectsBottom) {
					const maxHeight = Math.max(MIN_HEIGHT, window.innerHeight - originY)
					rect.height = Math.min(maxHeight, Math.max(MIN_HEIGHT, originHeight + dy))
				} else if (affectsTop) {
					const maxHeight = Math.max(MIN_HEIGHT, originY + originHeight)
					const newHeight = Math.min(maxHeight, Math.max(MIN_HEIGHT, originHeight - dy))
					rect.height = newHeight
					rect.y = Math.max(0, originY + originHeight - newHeight)
				}

				pending = rect
				if (!frame) frame = requestAnimationFrame(flush)
			}
			const onUp = () => {
				window.removeEventListener('mousemove', onMove)
				window.removeEventListener('mouseup', onUp)
				if (frame) cancelAnimationFrame(frame)
				if (pending) this.$store.commit('UPDATE_WINDOW_RECT', pending)
				this.$store.commit('PERSIST_WINDOWS')
			}
			window.addEventListener('mousemove', onMove)
			window.addEventListener('mouseup', onUp)
		}
```

- [ ] **Step 7: Manual check**

Run `pnpm dev` (see Task 13 for the full manual-verification pass; here just a smoke check), open Settings, drag the window by its titlebar and resize from a corner. Confirm it still moves/resizes smoothly (no visible regression) — the fix is about `localStorage` write frequency, not visible behavior.

- [ ] **Step 8: Commit**

```bash
cd /root/casaos-fork/CasaOS-UI
git add src/store/mutations.js src/store/mutations.spec.js src/components/desktop/DesktopWindow.vue
git commit -m "Throttle window drag/resize persistence to mouseup instead of every mousemove"
```

---

### Task 2: Remove the maximize button from the shared window chrome

**Files:**
- Modify: `src/store/mutations.js`
- Modify: `src/components/desktop/DesktopWindow.vue`

**Interfaces:**
- Removes: Vuex mutation `TOGGLE_MAXIMIZE_WINDOW`, the `maximized`/`preMaximizeRect` fields it used, and `DesktopWindow.vue`'s `toggleMaximize()` method and maximize button. Nothing else in this plan depends on any of these.

The maximize button (added in commit `ec5b52e`, before this plan's design phase — see Global Constraints) is being removed by explicit user decision: Files and Terminal already deliberately have no maximize (their own tab-bar titlebars are minimize+close only, "by design"), and the user wants that same minimize+close-only convention applied everywhere rather than having Settings/VM Manager/Legacy-App-Edit be the odd ones out with a third button.

- [ ] **Step 1: Remove the mutation from `src/store/mutations.js`**

Find:

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

Replace with nothing (delete the block, including the trailing blank line, so `UPDATE_WINDOW_RECT`/`RESTORE_WINDOWS` end up adjacent as they were before `ec5b52e`).

- [ ] **Step 2: Stop persisting the now-unused fields**

Find:

```js
		.map(({ id, title, component, x, y, width, height, minimized, maximized, preMaximizeRect }) => ({ id, title, component, x, y, width, height, minimized, maximized, preMaximizeRect }))
```

Replace with:

```js
		.map(({ id, title, component, x, y, width, height, minimized }) => ({ id, title, component, x, y, width, height, minimized }))
```

- [ ] **Step 3: Remove the button, the dblclick shortcut, and the method from `DesktopWindow.vue`**

Find:

```html
		<div v-if="!ownTitlebarComponents.includes(win.component)" class="window-titlebar" @mousedown="startDrag" @dblclick="toggleMaximize">
			<span class="window-title one-line">{{ win.title }}</span>
			<div class="window-controls">
				<button class="window-btn window-btn-maximize" :title="$t('Maximize')" @click.stop="toggleMaximize"></button>
				<button class="window-btn window-btn-minimize" :title="$t('Minimize')" @click.stop="minimize"></button>
				<button class="window-btn window-btn-close" :title="$t('Close')" @click.stop="close"></button>
			</div>
		</div>
```

Replace with:

```html
		<div v-if="!ownTitlebarComponents.includes(win.component)" class="window-titlebar" @mousedown="startDrag">
			<span class="window-title one-line">{{ win.title }}</span>
			<div class="window-controls">
				<button class="window-btn window-btn-minimize" :title="$t('Minimize')" @click.stop="minimize"></button>
				<button class="window-btn window-btn-close" :title="$t('Close')" @click.stop="close"></button>
			</div>
		</div>
```

- [ ] **Step 4: Remove the `toggleMaximize` method**

Find:

```js
		toggleMaximize() {
			this.$store.commit('TOGGLE_MAXIMIZE_WINDOW', this.win.id)
		},

```

Replace with nothing (delete the block — `minimize()` and `startDrag(e)` end up adjacent).

- [ ] **Step 5: Remove the button's CSS**

Find:

```scss
.window-btn-maximize {
	background: #3dd06a;
}

```

Replace with nothing (delete the block — `.window-btn-minimize` and `.window-btn-close` end up adjacent, as they were before `ec5b52e`).

- [ ] **Step 6: Manual check**

Run `pnpm dev`, open Settings, VM Manager, and the Edit dialog on any app — confirm each titlebar now shows only minimize and close, with no visual gap where the third button was. Confirm Files and Terminal are unaffected (they already had their own titlebar, untouched by this task). Reload with a window that was previously left maximized — it should just load at whatever `x/y/width/height` was last persisted (no crash from a missing `preMaximizeRect`).

- [ ] **Step 7: Commit**

```bash
git add src/store/mutations.js src/components/desktop/DesktopWindow.vue
git commit -m "Remove maximize button from the shared window chrome (match Files/Terminal's minimize+close-only convention)"
```

---

### Task 3: Settings responsive breakpoint utility + `SettingsNav.vue`

**Files:**
- Create: `src/utils/settings/breakpoints.js`
- Create: `src/utils/settings/breakpoints.spec.js`
- Create: `src/components/settings/SettingsNav.vue`

This follows the convention already established by the Files app rewrite (`src/utils/files/breakpoints.js`: a pure `classifyWidth(width)` returning a flags object, co-located `.spec.js`) rather than inventing a differently-shaped utility.

**Interfaces:**
- Produces: `classifyWidth(width): { navCollapsed: boolean, rowsStacked: boolean }` — consumed by Task 12 (`SettingsApp.vue` shell).
- Produces: `SettingsNav` component — props `sections: Array<{id, label, icon}>`, `activeSection: string`, `compact: boolean`; emits `select(sectionId)`. Consumed by Task 12.

- [ ] **Step 1: Write the failing test**

Create `src/utils/settings/breakpoints.spec.js`:

```js
import { expect, test, describe } from 'vitest'
import { classifyWidth } from './breakpoints'

describe('classifyWidth', () => {
	test('wide window: nothing collapsed', () => {
		expect(classifyWidth(900)).toEqual({ navCollapsed: false, rowsStacked: false })
	})

	test('narrow window: nav collapses, rows do not stack yet', () => {
		expect(classifyWidth(600)).toEqual({ navCollapsed: true, rowsStacked: false })
	})

	test('very narrow window: both collapse', () => {
		expect(classifyWidth(400)).toEqual({ navCollapsed: true, rowsStacked: true })
	})

	test('boundary values are exclusive on each threshold', () => {
		expect(classifyWidth(736).navCollapsed).toBe(false)
		expect(classifyWidth(735).navCollapsed).toBe(true)
		expect(classifyWidth(544).rowsStacked).toBe(false)
		expect(classifyWidth(543).rowsStacked).toBe(true)
	})
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `pnpm test breakpoints.spec.js`
Expected: FAIL — `src/utils/settings/breakpoints.js` doesn't exist. (This filename matches the existing `src/utils/files/breakpoints.spec.js`; vitest disambiguates by path, so both can coexist.)

- [ ] **Step 3: Implement `src/utils/settings/breakpoints.js`**

```js
const NAV_COLLAPSE_THRESHOLD = 736
const ROW_STACK_THRESHOLD = 544

export function classifyWidth(width) {
	return {
		navCollapsed: width < NAV_COLLAPSE_THRESHOLD,
		rowsStacked: width < ROW_STACK_THRESHOLD,
	}
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `pnpm test breakpoints.spec.js`
Expected: PASS (both `src/utils/files/breakpoints.spec.js` and this new one)

- [ ] **Step 5: Create `src/components/settings/SettingsNav.vue`**

```vue
<template>
	<aside class="settings-nav" :class="{ 'is-compact': compact }">
		<button v-for="s in sections" :key="s.id" class="nav-item hover-effect _is-radius"
			:class="{ active: activeSection === s.id }" :title="compact ? $t(s.label) : null"
			@click="$emit('select', s.id)">
			<b-icon :icon="s.icon" pack="casa" size="is-20"></b-icon>
			<span v-if="!compact" class="nav-label">{{ $t(s.label) }}</span>
		</button>
	</aside>
</template>

<script>
export default {
	name: 'settings-nav',
	props: {
		sections: { type: Array, required: true },
		activeSection: { type: String, required: true },
		compact: { type: Boolean, default: false }
	}
}
</script>

<style lang="scss" scoped>
.settings-nav {
	flex-shrink: 0;
	width: 13rem;
	padding: 1rem 0.6rem;
	background: rgba(0, 0, 0, 0.015);
	border-right: 1px solid rgb(228 233 237);
	display: flex;
	flex-direction: column;
	gap: 0.15rem;
	overflow-y: auto;

	&.is-compact {
		width: 3.75rem;
		align-items: center;
	}

	&.is-compact .nav-item {
		justify-content: center;
		padding: 0.55rem;
	}
}

.nav-item {
	display: flex;
	align-items: center;
	gap: 0.6rem;
	border: none;
	background: transparent;
	color: inherit;
	padding: 0.55rem 0.75rem;
	font-size: 0.85rem;
	text-align: left;
	cursor: pointer;
	width: 100%;

	.icon {
		color: hsla(208, 16%, 42%, 1);
	}

	&.active {
		background: hsla(208, 100%, 96%, 1);
		color: hsla(208, 100%, 45%, 1);
		font-weight: 600;

		.icon {
			color: hsla(208, 100%, 45%, 1);
		}
	}
}

.nav-label {
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
}
</style>
```

- [ ] **Step 6: Commit**

```bash
git add src/utils/settings/breakpoints.js src/utils/settings/breakpoints.spec.js src/components/settings/SettingsNav.vue
git commit -m "Add settings width-breakpoint utility and SettingsNav rail component"
```

---

### Task 4: Settings search utility + `SettingsSearch.vue`

**Files:**
- Create: `src/utils/settingsSearch.js`
- Create: `src/utils/settingsSearch.spec.js`
- Create: `src/components/settings/SettingsSearch.vue`

**Interfaces:**
- Produces: `filterRows(rows, query): Array` where each `row` is `{sectionId, sectionLabel, label}` — matches case-insensitively on `label`. Consumed by `SettingsSearch.vue` and, indirectly, Task 12.
- Produces: `SettingsSearch` component — prop `rows: Array<{sectionId, sectionLabel, label}>`; emits `jump(sectionId)`. Consumed by Task 12.
- Note (see Global Constraints): this is jump-to-section only. Highlighting the specific matched row is not implemented — there is no per-row DOM anchor to scroll to yet, and building one is out of scope for this plan.

- [ ] **Step 1: Write the failing test**

Create `src/utils/settingsSearch.spec.js`:

```js
import { describe, test, expect } from 'vitest'
import { filterRows } from './settingsSearch'

const ROWS = [
	{ sectionId: 'appearance', sectionLabel: 'Appearance', label: 'Window transparency' },
	{ sectionId: 'general', sectionLabel: 'General', label: 'Language' },
	{ sectionId: 'system', sectionLabel: 'System', label: 'WebUI Port' }
]

describe('filterRows', () => {
	test('empty query returns no results', () => {
		expect(filterRows(ROWS, '')).toEqual([])
	})
	test('matches case-insensitively on the row label', () => {
		expect(filterRows(ROWS, 'window')).toEqual([ROWS[0]])
	})
	test('matches a substring anywhere in the label', () => {
		expect(filterRows(ROWS, 'port')).toEqual([ROWS[2]])
	})
	test('whitespace-only query returns no results', () => {
		expect(filterRows(ROWS, '   ')).toEqual([])
	})
})
```

- [ ] **Step 2: Run to verify it fails**

Run: `pnpm test settingsSearch.spec.js`
Expected: FAIL — module doesn't exist.

- [ ] **Step 3: Implement `src/utils/settingsSearch.js`**

```js
export function filterRows(rows, query) {
	const q = query.trim().toLowerCase()
	if (!q) return []
	return rows.filter(r => r.label.toLowerCase().includes(q))
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `pnpm test settingsSearch.spec.js`
Expected: PASS

- [ ] **Step 5: Create `src/components/settings/SettingsSearch.vue`**

```vue
<template>
	<div class="settings-search">
		<b-input v-model="query" :placeholder="$t('Search settings')" size="is-small" icon-pack="casa" icon="search-outline" rounded></b-input>
		<div v-if="results.length" class="search-results">
			<button v-for="r in results" :key="r.sectionId + r.label" class="search-result hover-effect _is-radius" @click="jump(r)">
				<span class="result-label">{{ $t(r.label) }}</span>
				<span class="result-section">{{ $t(r.sectionLabel) }}</span>
			</button>
		</div>
	</div>
</template>

<script>
import { filterRows } from '@/utils/settingsSearch'

export default {
	name: 'settings-search',
	props: {
		rows: { type: Array, required: true }
	},
	data() {
		return { query: '' }
	},
	computed: {
		results() {
			return filterRows(this.rows, this.query).slice(0, 8)
		}
	},
	methods: {
		jump(result) {
			this.$emit('jump', result.sectionId)
			this.query = ''
		}
	}
}
</script>

<style lang="scss" scoped>
.settings-search {
	position: relative;
	padding: 0.75rem 1rem 0;
}

.search-results {
	position: absolute;
	left: 1rem;
	right: 1rem;
	top: 100%;
	margin-top: 0.25rem;
	background: #fff;
	border: 1px solid rgb(228 233 237);
	border-radius: 8px;
	box-shadow: 0 8px 24px rgba(0, 0, 0, 0.12);
	z-index: 5;
	overflow: hidden;
}

.search-result {
	display: flex;
	justify-content: space-between;
	width: 100%;
	border: none;
	background: transparent;
	padding: 0.5rem 0.75rem;
	font-size: 0.8rem;
	cursor: pointer;
	text-align: left;
}

.result-section {
	opacity: 0.5;
	font-size: 0.7rem;
}
</style>
```

- [ ] **Step 6: Commit**

```bash
git add src/utils/settingsSearch.js src/utils/settingsSearch.spec.js src/components/settings/SettingsSearch.vue
git commit -m "Add settings search utility and SettingsSearch component"
```

---

### Task 5: Shared settings row styles + `AccountSection.vue` + `AppearanceSection.vue`

**Files:**
- Create: `src/assets/scss/common/_settings.scss`
- Modify: `src/assets/scss/app.scss`
- Create: `src/components/settings/sections/AccountSection.vue`
- Create: `src/components/settings/sections/AppearanceSection.vue`

**Interfaces:**
- Produces: global (unscoped) CSS classes `.section-title`, `.settings-tabs`, `.setting-row-group`, `.row-label-heading`, `.setting-row`, `.slider-control`, `.slider-hint`, `.set-select`, `.port-input`, `.error-note`, plus the `.settings-content.is-narrow .setting-row` stacking rule — consumed by every section/panel component in this plan and by Task 12's shell.
- Produces: `AccountSection` (no exported `ROWS` — wraps `AccountPanel` wholesale) and `AppearanceSection` (exports `ROWS`) — both consumed by Task 12.

This is a direct relocation of markup/logic already in the current `SettingsApp.vue` (lines already read from the live file) — no behavior change.

- [ ] **Step 1: Extract the shared row styles into a global partial**

Create `src/assets/scss/common/_settings.scss`:

```scss
.section-title {
	font-family: $family-sans-serif;
	font-size: 1.1rem;
	font-weight: 600;
	margin-bottom: 1.25rem;
	padding-bottom: 0.75rem;
	border-bottom: 1px solid rgb(228 233 237);
}

.settings-tabs {
	.tabs {
		margin-bottom: 1rem;
	}
}

.setting-row-group {
	margin-bottom: 1.5rem;
}

.row-label-heading {
	font-size: 0.8rem;
	font-weight: 600;
	color: hsla(208, 16%, 42%, 1);
	margin-bottom: 0.5rem;
}

.setting-row {
	display: flex;
	align-items: center;
	padding: 0.75rem 0.6rem;
	min-height: 2.5rem;

	.row-icon {
		flex-shrink: 0;
		margin-right: 0.6rem;
		color: hsla(208, 16%, 42%, 1);
	}

	.row-label {
		flex: 1;
		font-size: 0.875rem;
	}

	.row-control {
		display: flex;
		align-items: center;
		flex-shrink: 0;
	}
}

// Narrow Settings window: stack the label above the control instead of
// forcing them onto one cramped row (see utils/settings/breakpoints.js).
.settings-content.is-narrow .setting-row {
	flex-wrap: wrap;
	row-gap: 0.5rem;

	.row-label {
		flex-basis: 100%;
	}

	.row-control {
		flex-basis: 100%;
	}
}

.slider-control {
	gap: 0.6rem;

	input[type='range'] {
		width: 8rem;
		accent-color: hsla(208, 100%, 45%, 1);
	}
}

.slider-hint {
	font-size: 0.7rem;
	opacity: 0.5;
	white-space: nowrap;
}

.set-select {
	.select select {
		font-size: 0.8rem;
	}
}

.port-input {
	width: 6rem;
}

.error-note {
	color: #d64545;
	font-size: 0.75rem;
	padding: 0 0.6rem;
	margin-top: -0.5rem;
	margin-bottom: 0.5rem;
}
```

- [ ] **Step 2: Import the partial globally**

In `src/assets/scss/app.scss`, find:

```scss
@import "common/widgets";
@import "common/sections";
```

Replace with:

```scss
@import "common/widgets";
@import "common/sections";
@import "common/settings";
```

- [ ] **Step 3: Create `src/components/settings/sections/AccountSection.vue`**

```vue
<template>
	<section class="settings-section">
		<h2 class="section-title">{{ $t('Account') }}</h2>
		<account-panel embedded></account-panel>
	</section>
</template>

<script>
import AccountPanel from '@/components/account/AccountPanel.vue'

export const ROWS = []

export default {
	name: 'account-section',
	components: { AccountPanel }
}
</script>
```

- [ ] **Step 4: Create `src/components/settings/sections/AppearanceSection.vue`**

```vue
<template>
	<section class="settings-section">
		<h2 class="section-title">{{ $t('Appearance') }}</h2>

		<div class="setting-row-group">
			<div class="row-label-heading">{{ $t('Wallpaper') }}</div>
			<wallpaper-modal embedded></wallpaper-modal>
		</div>

		<div class="setting-row hover-effect _is-radius">
			<b-icon class="row-icon" icon="control-outline" pack="casa" size="is-20"></b-icon>
			<div class="row-label">{{ $t('Window transparency') }}</div>
			<div class="row-control slider-control">
				<span class="slider-hint">{{ $t('Opaque') }}</span>
				<input v-model.number="backdropAlphaPct" type="range" min="40" max="100" step="1" @input="applyBackdropAlpha" />
				<span class="slider-hint">{{ $t('Transparent') }}</span>
			</div>
		</div>

		<div class="setting-row hover-effect _is-radius">
			<b-icon class="row-icon" icon="control-outline" pack="casa" size="is-20"></b-icon>
			<div class="row-label">{{ $t('Window blur') }}</div>
			<div class="row-control slider-control">
				<span class="slider-hint">{{ $t('None') }}</span>
				<input v-model.number="backdropBlurPx" type="range" min="0" max="24" step="1" @input="applyBackdropBlur" />
				<span class="slider-hint">{{ $t('Strong') }}</span>
			</div>
		</div>

		<div class="setting-row-group">
			<div class="row-label-heading">{{ $t('Widgets') }}</div>
			<widget-visibility-panel></widget-visibility-panel>
		</div>
	</section>
</template>

<script>
import WallpaperModal from '@/components/wallpaper/WallpaperModal.vue'
import WidgetVisibilityPanel from '@/components/settings/WidgetVisibilityPanel.vue'

export const ROWS = [
	{ label: 'Wallpaper' },
	{ label: 'Window transparency' },
	{ label: 'Window blur' },
	{ label: 'Widgets' }
]

export default {
	name: 'appearance-section',
	components: { WallpaperModal, WidgetVisibilityPanel },
	data() {
		return {
			backdropAlphaPct: 100,
			backdropBlurPx: 0
		}
	},
	created() {
		this.restoreBackdropSettings()
	},
	methods: {
		restoreBackdropSettings() {
			const alpha = localStorage.getItem('uiBackdropAlpha')
			const blur = localStorage.getItem('uiBackdropBlur')
			this.backdropAlphaPct = alpha !== null ? Math.round(parseFloat(alpha) * 100) : 100
			this.backdropBlurPx = blur !== null ? parseFloat(blur) : 0
		},
		applyBackdropAlpha() {
			const alpha = this.backdropAlphaPct / 100
			document.documentElement.style.setProperty('--ui-backdrop-alpha', alpha)
			localStorage.setItem('uiBackdropAlpha', alpha)
		},
		applyBackdropBlur() {
			document.documentElement.style.setProperty('--ui-backdrop-blur', `${this.backdropBlurPx}px`)
			localStorage.setItem('uiBackdropBlur', this.backdropBlurPx)
		}
	}
}
</script>
```

- [ ] **Step 5: Commit**

```bash
git add src/assets/scss/common/_settings.scss src/assets/scss/app.scss src/components/settings/sections/AccountSection.vue src/components/settings/sections/AppearanceSection.vue
git commit -m "Extract shared settings-row styles; add Account and Appearance sections"
```

---

### Task 6: `UsersSection.vue` + `GeneralSection.vue`

**Files:**
- Create: `src/components/settings/sections/UsersSection.vue`
- Create: `src/components/settings/sections/GeneralSection.vue`

**Interfaces:**
- Consumes: global row styles from Task 5.
- Produces: `UsersSection` — prop `narrow: boolean` (from Task 12's breakpoint classification), exports `ROWS`. `GeneralSection` — exports `ROWS`. Both consumed by Task 12.

- [ ] **Step 1: Create `src/components/settings/sections/UsersSection.vue`**

```vue
<template>
	<section class="settings-section">
		<h2 class="section-title">{{ $t('Users & Access') }}</h2>

		<b-select v-if="narrow" v-model="usersTab" class="set-select users-tab-select" size="is-small">
			<option value="casaos">{{ $t('CasaOS Users') }}</option>
			<option value="system">{{ $t('System Users') }}</option>
			<option value="smb">{{ $t('SMB Users') }}</option>
		</b-select>
		<b-tabs v-else v-model="usersTab" type="is-toggle" size="is-small" :animated="false" class="settings-tabs">
			<b-tab-item :label="$t('CasaOS Users')" value="casaos"></b-tab-item>
			<b-tab-item :label="$t('System Users')" value="system"></b-tab-item>
			<b-tab-item :label="$t('SMB Users')" value="smb"></b-tab-item>
		</b-tabs>

		<component :is="activePanel"></component>
	</section>
</template>

<script>
import CasaosUsersPanel from '@/components/settings/CasaosUsersPanel.vue'
import SystemUsersPanel from '@/components/settings/SystemUsersPanel.vue'
import SmbUsersPanel from '@/components/settings/SmbUsersPanel.vue'

export const ROWS = [
	{ label: 'CasaOS Users' },
	{ label: 'System Users' },
	{ label: 'SMB Users' }
]

const PANELS = {
	casaos: 'CasaosUsersPanel',
	system: 'SystemUsersPanel',
	smb: 'SmbUsersPanel'
}

export default {
	name: 'users-section',
	components: { CasaosUsersPanel, SystemUsersPanel, SmbUsersPanel },
	props: {
		narrow: { type: Boolean, default: false }
	},
	data() {
		return { usersTab: 'casaos' }
	},
	computed: {
		activePanel() {
			return PANELS[this.usersTab]
		}
	}
}
</script>

<style lang="scss" scoped>
.users-tab-select {
	margin-bottom: 1rem;
	display: block;
}
</style>
```

- [ ] **Step 2: Create `src/components/settings/sections/GeneralSection.vue`**

```vue
<template>
	<section class="settings-section">
		<h2 class="section-title">{{ $t('General') }}</h2>

		<div class="setting-row hover-effect _is-radius">
			<b-icon class="row-icon" icon="language-outline" pack="casa" size="is-20"></b-icon>
			<div class="row-label">{{ $t('Language') }}</div>
			<div class="row-control">
				<b-select v-model="barData.lang" class="set-select" size="is-small" @input="saveBarData">
					<option v-for="lang in languages" :key="lang.lang" :value="lang.lang">{{ lang.name }}</option>
				</b-select>
			</div>
		</div>

		<div class="setting-row hover-effect _is-radius">
			<b-icon class="row-icon" icon="news-outline" pack="casa" size="is-20"></b-icon>
			<div class="row-label">{{ $t('Show news feed from CasaOS Blog') }}</div>
			<div class="row-control">
				<b-switch v-model="rssSwitch" class="is-flex-direction-row-reverse mr-0" type="is-dark" @input="onRssToggle"></b-switch>
			</div>
		</div>

		<div class="setting-row hover-effect _is-radius">
			<b-icon class="row-icon" icon="display-applications-outline" pack="casa" size="is-20"></b-icon>
			<div class="row-label">{{ $t('Show Recommended Apps') }}</div>
			<div class="row-control">
				<b-switch v-model="barData.recommend_switch" class="is-flex-direction-row-reverse mr-0" type="is-dark" @input="saveBarData"></b-switch>
			</div>
		</div>

		<div v-if="hasNotImportedApps" class="setting-row hover-effect _is-radius">
			<b-icon class="row-icon" icon="docker-outline" pack="casa" size="is-20"></b-icon>
			<div class="row-label">{{ $t('Show other Docker container app(s)') }}</div>
			<div class="row-control">
				<b-switch v-model="barData.existing_apps_switch" class="is-flex-direction-row-reverse mr-0" type="is-dark" @input="saveBarData"></b-switch>
			</div>
		</div>
	</section>
</template>

<script>
import { mixin } from '@/mixins/mixin'
import messages from '@/assets/lang'

const systemConfigName = 'system'

export const ROWS = [
	{ label: 'Language' },
	{ label: 'Show news feed from CasaOS Blog' },
	{ label: 'Show Recommended Apps' },
	{ label: 'Show other Docker container app(s)' }
]

export default {
	name: 'general-section',
	mixins: [mixin],
	data() {
		return {
			barData: {
				lang: this.getLangFromBrowser ? this.getLangFromBrowser() : 'en_us',
				recommend_switch: true,
				existing_apps_switch: true,
				rss_switch: false
			},
			rssSwitch: false,
			languages: Object.entries(messages).map(([key, value]) => ({ lang: key, name: value.lang_name }))
		}
	},
	computed: {
		hasNotImportedApps() {
			return this.$store.state.notImportList.length > 0
		}
	},
	created() {
		this.getBarData()
	},
	methods: {
		getBarData() {
			this.$api.users.getCustomStorage(systemConfigName).then(res => {
				if (res.data.success === 200 && res.data.data !== '') {
					this.barData = res.data.data
					this.rssSwitch = !!this.barData.rss_switch
				}
			})
		},
		saveBarData() {
			this.$api.users.setCustomStorage(systemConfigName, this.barData).then(res => {
				if (res.data.success === 200) {
					this.barData = res.data.data
					if (this.barData.lang) {
						const lang = this.barData.lang.includes('_') ? this.barData.lang : 'en_us'
						this.setLang(lang)
					}
					this.$store.commit('SET_RECOMMEND_SWITCH', this.barData.recommend_switch)
				}
			})
		},
		onRssToggle(val) {
			if (!val) {
				this.barData.rss_switch = false
				this.saveBarData()
				this.$store.commit('SET_RSS_SWITCH', false)
				return
			}
			this.$buefy.dialog.confirm({
				title: this.$t('Show news feed from CasaOS Blog'),
				message: this.$t('CasaOS dashboard will get the the latest news feed of https://blog.casaos.io via Internet, which might leave your visit records to the site. Do you accept?'),
				type: 'is-dark',
				confirmText: this.$t('Accept'),
				cancelText: this.$t('Cancel'),
				onConfirm: () => {
					this.barData.rss_switch = true
					this.saveBarData()
					this.$store.commit('SET_RSS_SWITCH', true)
				},
				onCancel: () => {
					this.rssSwitch = false
				}
			})
		}
	}
}
</script>
```

- [ ] **Step 3: Commit**

```bash
git add src/components/settings/sections/UsersSection.vue src/components/settings/sections/GeneralSection.vue
git commit -m "Add Users & Access and General settings sections"
```

---

### Task 7: `SystemSection.vue` (fixed power confirm) + `AboutPanel.vue`

**Files:**
- Create: `src/components/settings/sections/SystemSection.vue`
- Create: `src/components/settings/AboutPanel.vue`

**Interfaces:**
- Consumes: global row styles from Task 5; `sys.getVersion()`, `sys.hardwareInfo()`, `sys.getLogs()`, `sys.updateCasaOS()` (all already exist in `src/service/sys.js`).
- Produces: `SystemSection` (exports `ROWS`) consumed by Task 12.

This replaces the existing `power()` method's button-relabeling hack ("click Restart, label changes to 'Are you sure?', click again") with a real `$buefy.dialog.confirm`, matching the confirm pattern already used by `onRssToggle` elsewhere in this codebase.

- [ ] **Step 1: Confirm the exact response shapes this task relies on**

Already verified against source (no action needed, just don't second-guess these while implementing):
- `GET /v1/sys/version` (→ `sys.getVersion()`): `{success, data: {need_update: bool, version: {version, change_log, ...}, current_version: string}}` (`CasaOS/route/v1/system.go:GetSystemCheckVersion`, `model/version.go:Version`).
- `GET /v1/sys/hardware` (→ `sys.hardwareInfo()`): `{success, data: {drive_model: string, arch: string}}` (`GetSystemHardwareInfo`).
- `GET /v1/sys/logs` (→ `sys.getLogs()`): `{success, data: string[]}` (`GetCasaOSErrorLogs` → `GetCasaOSLogs(line)`, defaults to last 100 lines).
- `POST /v1/sys/update` (→ `sys.updateCasaOS()`): `{success, message}`, no data — poll `getVersion()` afterward until `need_update` flips false.

- [ ] **Step 2: Create `src/components/settings/AboutPanel.vue`**

```vue
<template>
	<div class="about-panel">
		<div class="setting-row hover-effect _is-radius">
			<b-icon class="row-icon" icon="internet-outline" pack="casa" size="is-20"></b-icon>
			<div class="row-label">{{ $t('Installed version') }}</div>
			<div class="row-control">{{ currentVersion }}</div>
		</div>

		<div class="setting-row hover-effect _is-radius">
			<b-icon class="row-icon" icon="downloads-outline" pack="casa" size="is-20"></b-icon>
			<div class="row-label">
				{{ needUpdate ? $t('Update available: {version}', { version: latestVersion }) : $t('CasaOS is up to date') }}
			</div>
			<div class="row-control">
				<b-button v-if="needUpdate" rounded size="is-small" type="is-dark" :loading="updating" @click="applyUpdate">
					{{ $t('Update') }}
				</b-button>
			</div>
		</div>

		<div class="setting-row hover-effect _is-radius">
			<b-icon class="row-icon" icon="control-outline" pack="casa" size="is-20"></b-icon>
			<div class="row-label">{{ $t('CPU architecture') }}</div>
			<div class="row-control">{{ hardware.arch }}</div>
		</div>

		<div class="setting-row hover-effect _is-radius">
			<b-icon class="row-icon" icon="storage-other" pack="casa" size="is-20"></b-icon>
			<div class="row-label">{{ $t('Device') }}</div>
			<div class="row-control">{{ hardware.drive_model || $t('Unknown') }}</div>
		</div>

		<div class="setting-row-group">
			<div class="row-label-heading">
				{{ $t('Error logs') }}
				<b-button class="ml-2" rounded size="is-small" @click="loadLogs">{{ $t('Refresh') }}</b-button>
			</div>
			<pre class="log-view">{{ logText }}</pre>
		</div>
	</div>
</template>

<script>
export default {
	name: 'about-panel',
	data() {
		return {
			currentVersion: '',
			needUpdate: false,
			latestVersion: '',
			updating: false,
			hardware: { arch: '', drive_model: '' },
			logText: ''
		}
	},
	created() {
		this.loadVersion()
		this.loadHardware()
		this.loadLogs()
	},
	methods: {
		loadVersion() {
			this.$api.sys.getVersion().then(res => {
				if (res.data.success === 200) {
					const data = res.data.data
					this.currentVersion = data.current_version
					this.needUpdate = !!data.need_update
					this.latestVersion = data.version && data.version.version ? data.version.version : ''
				}
			})
		},
		loadHardware() {
			this.$api.sys.hardwareInfo().then(res => {
				if (res.data.success === 200) this.hardware = res.data.data
			})
		},
		loadLogs() {
			this.$api.sys.getLogs().then(res => {
				if (res.data.success === 200) {
					const lines = res.data.data || []
					this.logText = Array.isArray(lines) ? lines.join('\n') : String(lines)
				}
			})
		},
		applyUpdate() {
			this.updating = true
			this.$api.sys.updateCasaOS().then(() => {
				const timer = setInterval(() => {
					this.$api.sys.getVersion().then(res => {
						if (res.data.success === 200 && !res.data.data.need_update) {
							clearInterval(timer)
							this.updating = false
							this.loadVersion()
						}
					})
				}, 5000)
			}).catch(() => {
				this.updating = false
			})
		}
	}
}
</script>

<style lang="scss" scoped>
.log-view {
	max-height: 12rem;
	overflow: auto;
	background: rgba(0, 0, 0, 0.03);
	border-radius: 6px;
	padding: 0.6rem;
	font-size: 0.7rem;
	white-space: pre-wrap;
	word-break: break-word;
}
</style>
```

- [ ] **Step 3: Create `src/components/settings/sections/SystemSection.vue`**

```vue
<template>
	<section class="settings-section">
		<h2 class="section-title">{{ $t('System') }}</h2>

		<div class="setting-row hover-effect _is-radius">
			<b-icon class="row-icon" icon="port-outline" pack="casa" size="is-20"></b-icon>
			<div class="row-label">{{ $t('WebUI Port') }}</div>
			<div class="row-control">
				<template v-if="!editingPort">
					<span class="mr-2">{{ port }}</span>
					<b-button rounded size="is-small" @click="startEditPort">{{ $t('Change') }}</b-button>
				</template>
				<template v-else>
					<b-input v-model="portInput" type="number" size="is-small" class="port-input"
						@keyup.enter.native="savePort"></b-input>
					<b-button class="ml-2" rounded size="is-small" @click="editingPort = false">{{ $t('Cancel') }}</b-button>
					<b-button class="ml-2" rounded size="is-small" type="is-dark" :loading="savingPort" @click="savePort">
						{{ $t('Save') }}
					</b-button>
				</template>
			</div>
		</div>
		<p v-if="portError" class="error-note">{{ portError }}</p>

		<div class="setting-row hover-effect _is-radius">
			<b-icon class="row-icon has-text-red" icon="restart-outline" pack="casa" size="is-20"></b-icon>
			<div class="row-label has-text-red">{{ $t('Restart or Shutdown') }}</div>
			<div class="row-control">
				<b-button class="mr-2" rounded size="is-small" @click="power('Restart')">{{ $t('Restart') }}</b-button>
				<b-button rounded size="is-small" type="is-danger" @click="power('Shutdown')">{{ $t('Shutdown') }}</b-button>
			</div>
		</div>

		<div class="setting-row-group">
			<div class="row-label-heading">{{ $t('About') }}</div>
			<about-panel></about-panel>
		</div>

		<b-modal v-model="showPower" :can-cancel="false" scroll="clip" width="20rem">
			<b-message @close="resetPower">
				<template #header>
					{{ $t(showPowerTitle) }}
				</template>
				<div>{{ $t(showPowerMessage) }}</div>
			</b-message>
		</b-modal>
	</section>
</template>

<script>
import AboutPanel from '@/components/settings/AboutPanel.vue'

export const ROWS = [
	{ label: 'WebUI Port' },
	{ label: 'Restart or Shutdown' },
	{ label: 'About' }
]

export default {
	name: 'system-section',
	components: { AboutPanel },
	data() {
		return {
			port: '',
			editingPort: false,
			portInput: '',
			savingPort: false,
			portError: '',
			showPower: false,
			showPowerTitle: '',
			showPowerMessage: ''
		}
	},
	created() {
		this.getPort()
	},
	methods: {
		getPort() {
			this.$api.sys.getServerPort().then(res => {
				if (res.data.success === 200) this.port = res.data.data
			})
		},
		startEditPort() {
			this.portInput = this.port
			this.portError = ''
			this.editingPort = true
		},
		savePort() {
			const port = Number(this.portInput)
			if (!port || port < 80 || port > 65535) {
				this.portError = this.$t('Port range is 80-65535')
				return
			}
			this.portError = ''
			this.savingPort = true
			this.$messageBus('dashboardsetting_webuiport', String(port))
			this.$api.sys.editServerPort({ port }).then(res => {
				if (res.data.success === 200) {
					this.pollNewPort(port)
				} else {
					this.savingPort = false
					this.portError = res.data.message
				}
			}).catch(err => {
				this.savingPort = false
				this.portError = err.response && err.response.data ? err.response.data.message : this.$t('Failed to change port')
			})
		},
		pollNewPort(port) {
			const timer = setInterval(() => {
				const checkUrl = `${this.$protocol}//${this.$baseIp}:${port}`
				this.$api.sys.checkUiPort(`${checkUrl}/v1/gateway/port`).then(res => {
					if (res.data.success === 200) {
						clearInterval(timer)
						window.open(`${this.$protocol}//${this.$baseIp}:${res.data.data}`, '_self')
					}
				})
			}, 1000)
		},

		power(key) {
			const isRestart = key === 'Restart'
			this.$buefy.dialog.confirm({
				title: this.$t(key),
				message: isRestart ? this.$t('Restart the system now?') : this.$t('Shut down the system now?'),
				type: 'is-danger',
				confirmText: this.$t(key),
				cancelText: this.$t('Cancel'),
				onConfirm: () => this.doPower(isRestart)
			})
		},
		doPower(isRestart) {
			this.showPower = true
			this.showPowerTitle = isRestart ? 'Restarting now' : 'Now shutting down'
			this.showPowerMessage = isRestart
				? 'Please wait for about 90 seconds.'
				: 'Please wait for about 30 seconds before cutting off the power.'
			let timer
			this.$api.sys.power(isRestart ? 'restart' : 'off').then(res => {
				if (res.data.success === 200) {
					this.showPowerMessage = res.data.data
					timer = setInterval(() => {
						this.$api.users.getUserStatus().then(statusRes => {
							if (statusRes.data.data.initialized) {
								clearInterval(timer)
								location.reload()
							}
						})
					}, 30000)
				}
			})
		},
		resetPower() {
			this.showPower = false
		}
	}
}
</script>
```

- [ ] **Step 4: Commit**

```bash
git add src/components/settings/AboutPanel.vue src/components/settings/sections/SystemSection.vue
git commit -m "Add System section with fixed power confirm dialog and new About panel"
```

---

### Task 8: Tailscale backend route + service + `RemoteAccessPanel.vue`

**This task spans two separate git repositories**, not one: the backend route lives in `/root/casaos-fork/CasaOS` (its own git repo, remote `CasaOS.git`), and the frontend lives in `/root/casaos-fork/CasaOS-UI` (a different repo, the one every other task in this plan touches). Commit separately in each repo. There is no existing Tailscale integration anywhere in this fork to build on — confirmed by grepping both repos for "tailscale" (no hits) before this task was written.

**Files:**
- Create: `/root/casaos-fork/CasaOS/route/v1/tailscale.go`
- Modify: `/root/casaos-fork/CasaOS/route/v1.go`
- Create: `src/service/tailscale.js` (in CasaOS-UI)
- Create: `src/service/tailscale.spec.js` (in CasaOS-UI)
- Modify: `src/service/api.js` (in CasaOS-UI)
- Create: `src/components/settings/RemoteAccessPanel.vue` (in CasaOS-UI)

**Interfaces:**
- Produces (backend): `GET /v1/tailscale/status` → `{success, message, data}` where `data` is the raw, unmodified output of `tailscale status --json` (Tailscale's own stable JSON schema — not worth re-modeling into Go structs just to re-serialize it). `PUT /v1/tailscale/state/:state` (`:state` is `up` or `down`) → runs `tailscale up`/`tailscale down`, returns `{success, message, data: "<state>"}`.
- Produces (frontend): `$api.tailscale.{getStatus, setState}` — registered globally on `Vue.prototype.$api` via `src/service/api.js` (same mechanism as `$api.samba`, `$api.storage`, etc.). Consumed by `RemoteAccessPanel.vue` here and, structurally, by Task 9's `NetworkSection.vue`.

Confirmed on this box: `tailscale`/`tailscaled` are installed, `tailscaled` is active, and `tailscale status --json` returns real data — a top-level `BackendState` (`"Running"` when connected), a top-level `TailscaleIPs` array (this device's own tailnet IPs), and a `Peer` object **keyed by node key** (not an array) whose values have `HostName`, `DNSName`, `OS`, `Online`, `TailscaleIPs`. `RemoteAccessPanel.vue` must iterate `Object.values(data.Peer || {})`, not treat `Peer` as an array.

- [ ] **Step 1: Implement the backend route**

Create `/root/casaos-fork/CasaOS/route/v1/tailscale.go`. This reuses the `ok`/`badParams`/`serviceError` response helpers already defined in this same package by `route/v1/sysusers.go` — do not redefine them.

```go
package v1

import (
	"encoding/json"
	"os/exec"

	"github.com/labstack/echo/v4"
)

// GetTailscaleStatus shells `tailscale status --json` and passes its output
// straight through as the Data field - Tailscale's own JSON schema is a
// stable, documented external contract, not something worth re-modeling
// into Go structs just to re-serialize it back to JSON for the frontend.
func GetTailscaleStatus(ctx echo.Context) error {
	out, err := exec.Command("tailscale", "status", "--json").Output()
	if err != nil {
		return serviceError(ctx, err)
	}
	return ok(ctx, json.RawMessage(out))
}

// PutTailscaleState runs `tailscale up` or `tailscale down` depending on
// the :state path param - mirrors PutSystemState's :state-param shape
// (route/v1/system.go) used for restart/shutdown.
func PutTailscaleState(ctx echo.Context) error {
	state := ctx.Param("state")
	if state != "up" && state != "down" {
		return badParams(ctx, "state must be 'up' or 'down'")
	}
	if err := exec.Command("tailscale", state).Run(); err != nil {
		return serviceError(ctx, err)
	}
	return ok(ctx, state)
}
```

- [ ] **Step 2: Register the routes**

In `/root/casaos-fork/CasaOS/route/v1.go`, find:

```go
		v1ZerotierGroup := v1Group.Group("/zt")
		v1ZerotierGroup.Use()
		{
			v1ZerotierGroup.Any("/*url", v1.ZerotierProxy)
		}
	}
```

Replace with:

```go
		v1ZerotierGroup := v1Group.Group("/zt")
		v1ZerotierGroup.Use()
		{
			v1ZerotierGroup.Any("/*url", v1.ZerotierProxy)
		}

		v1TailscaleGroup := v1Group.Group("/tailscale")
		v1TailscaleGroup.Use()
		{
			v1TailscaleGroup.GET("/status", v1.GetTailscaleStatus)
			v1TailscaleGroup.PUT("/state/:state", v1.PutTailscaleState)
		}
	}
```

(The existing ZeroTier route stays — it's unused dead weight but removing it is out of scope for this task; don't touch it beyond leaving it exactly as found.)

- [ ] **Step 3: Build and verify the backend compiles**

From `/root/casaos-fork/CasaOS`:

```bash
go build -o /tmp/casaos-new .
```

Expected: builds with no errors. This does not touch the running system yet — it only proves the new code compiles. If the build fails, fix the Go code (not the build command) and retry.

- [ ] **Step 4: Deploy the new binary and verify the route is live**

This replaces the live, running `casaos` binary — the one system-level step in this whole plan. Do it carefully, with a rollback path:

```bash
sudo cp /usr/bin/casaos /usr/bin/casaos.bak-task8
sudo systemctl stop casaos
sudo cp /tmp/casaos-new /usr/bin/casaos
sudo systemctl start casaos
sleep 2
systemctl is-active casaos
```

Expected: `systemctl is-active casaos` prints `active`. If it does not, or the service fails to start, immediately roll back:

```bash
sudo systemctl stop casaos
sudo cp /usr/bin/casaos.bak-task8 /usr/bin/casaos
sudo systemctl start casaos
```

and report BLOCKED with what you saw in `journalctl -u casaos -n 50 --no-pager`.

Once the service is confirmed active, verify the new route with an authenticated request. Reuse whatever this box's existing dev/admin session token is via the running dev server's browser session, or simplest: check the route responds (even a 401 without a token proves the route exists and didn't 404):

```bash
curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:80/v1/tailscale/status
```

Expected: `401` (JWT-protected, as designed) rather than `404` (which would mean the route didn't register). A `401` here is success for this check — it proves the route exists.

If the deploy succeeds, remove the backup once you're confident: `sudo rm -f /usr/bin/casaos.bak-task8` (leave it if anything about the deploy felt uncertain — flag that in your report instead of deleting it).

- [ ] **Step 5: Commit the backend change**

```bash
cd /root/casaos-fork/CasaOS
git add route/v1/tailscale.go route/v1.go
git commit -m "Add Tailscale status/state routes (shells the tailscale CLI)"
```

- [ ] **Step 6: Write the failing frontend test**

Create `src/service/tailscale.spec.js` (in CasaOS-UI):

```js
import { describe, test, expect, vi, beforeEach } from 'vitest'
import { api } from './service.js'
import tailscale from './tailscale'

vi.mock('./service.js', () => ({
	api: { get: vi.fn(), put: vi.fn() }
}))

describe('tailscale service', () => {
	beforeEach(() => {
		api.get.mockReset()
		api.put.mockReset()
	})

	test('getStatus GETs /tailscale/status', () => {
		tailscale.getStatus()
		expect(api.get).toHaveBeenCalledWith('/tailscale/status')
	})

	test('setState PUTs /tailscale/state/:state', () => {
		tailscale.setState('up')
		expect(api.put).toHaveBeenCalledWith('/tailscale/state/up')
	})
})
```

- [ ] **Step 7: Run to verify it fails**

Run: `pnpm test tailscale.spec.js`
Expected: FAIL — `src/service/tailscale.js` doesn't exist.

- [ ] **Step 8: Implement `src/service/tailscale.js`**

```js
import { api } from './service.js'

const PREFIX = '/tailscale'

const tailscale = {
	getStatus() {
		return api.get(`${PREFIX}/status`)
	},
	setState(state) {
		return api.put(`${PREFIX}/state/${state}`)
	}
}
export default tailscale
```

- [ ] **Step 9: Run to verify it passes**

Run: `pnpm test tailscale.spec.js`
Expected: PASS

- [ ] **Step 10: Register it on `$api`**

In `src/service/api.js`, find:

```js
import samba from './samba.js';
```

Replace with:

```js
import samba from './samba.js';
import tailscale from './tailscale.js';
```

Find:

```js
	disks,
	storage,
	samba,
	driver,
	cloud,
```

Replace with:

```js
	disks,
	storage,
	samba,
	tailscale,
	driver,
	cloud,
```

- [ ] **Step 11: Create `src/components/settings/RemoteAccessPanel.vue`**

```vue
<template>
	<div class="remote-access-panel">
		<div v-if="loading" class="hint">{{ $t('Checking Tailscale status...') }}</div>

		<template v-else>
			<div class="setting-row hover-effect _is-radius">
				<b-icon class="row-icon" icon="internet-outline" pack="casa" size="is-20"></b-icon>
				<div class="row-label">{{ $t('Tailscale') }}</div>
				<div class="row-control">
					<b-switch :value="isRunning" class="is-flex-direction-row-reverse mr-0" type="is-dark" :loading="toggling" @input="toggle"></b-switch>
				</div>
			</div>

			<div v-if="isRunning" class="setting-row hover-effect _is-radius">
				<b-icon class="row-icon" icon="internet-outline" pack="casa" size="is-20"></b-icon>
				<div class="row-label">{{ $t('This device') }}</div>
				<div class="row-control">{{ selfIp }}</div>
			</div>

			<template v-if="isRunning">
				<div class="row-label-heading mt-4">{{ $t('Tailnet devices') }}</div>
				<p v-if="!peers.length" class="hint">{{ $t('No other devices in this tailnet.') }}</p>
				<div v-for="p in peers" :key="p.hostName" class="user-row">
					<div class="user-main">
						<div class="user-name">{{ p.hostName }}</div>
						<span class="badge">{{ p.os }} &middot; {{ p.ip }}</span>
					</div>
					<span class="badge" :class="{ 'has-text-success': p.online }">{{ p.online ? $t('Online') : $t('Offline') }}</span>
				</div>
			</template>
			<p v-if="error" class="error-note">{{ error }}</p>
		</template>
	</div>
</template>

<script>
export default {
	name: 'remote-access-panel',
	data() {
		return {
			loading: true,
			toggling: false,
			backendState: '',
			selfIp: '',
			peers: [],
			error: ''
		}
	},
	computed: {
		isRunning() {
			return this.backendState === 'Running'
		}
	},
	created() {
		this.refresh()
	},
	methods: {
		refresh() {
			this.loading = true
			this.$api.tailscale.getStatus().then(res => {
				if (res.data.success === 200) {
					const data = res.data.data
					this.backendState = data.BackendState
					this.selfIp = (data.TailscaleIPs && data.TailscaleIPs[0]) || ''
					const peerMap = data.Peer || {}
					this.peers = Object.values(peerMap).map(p => ({
						hostName: p.HostName,
						os: p.OS,
						ip: (p.TailscaleIPs && p.TailscaleIPs[0]) || '',
						online: !!p.Online
					}))
				}
			}).catch(() => {
				this.error = this.$t('Failed to reach Tailscale')
			}).finally(() => {
				this.loading = false
			})
		},
		toggle(value) {
			this.toggling = true
			this.error = ''
			this.$api.tailscale.setState(value ? 'up' : 'down').then(() => {
				this.refresh()
			}).catch(e => {
				this.error = e.response && e.response.data ? e.response.data.message : this.$t('Failed to change Tailscale state')
			}).finally(() => {
				this.toggling = false
			})
		}
	}
}
</script>

<style lang="scss" scoped>
.hint {
	font-size: 0.75rem;
	opacity: 0.6;
}

.mt-4 {
	margin-top: 1.5rem;
}

.user-row {
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: 0.75rem;
	padding: 0.6rem 0;
	border-bottom: 1px solid rgba(0, 0, 0, 0.08);
}

.user-main {
	display: flex;
	flex-direction: column;
	gap: 0.15rem;
}

.user-name {
	font-weight: 600;
	font-size: 0.85rem;
}

.badge {
	font-size: 0.7rem;
	opacity: 0.6;
}
</style>
```

- [ ] **Step 12: Commit the frontend change**

```bash
cd /root/casaos-fork/CasaOS-UI
git add src/service/tailscale.js src/service/tailscale.spec.js src/service/api.js src/components/settings/RemoteAccessPanel.vue
git commit -m "Add Tailscale service wrapper and Remote Access panel"
```

---

### Task 9: `NetworkSharesPanel.vue` + `NetworkSection.vue`

**Files:**
- Create: `src/components/settings/NetworkSharesPanel.vue`
- Create: `src/components/settings/sections/NetworkSection.vue`

**Interfaces:**
- Consumes: `$api.samba.{getShares, createShare, deleteShare}` (already registered — `src/service/samba.js`), `RemoteAccessPanel` (Task 8).
- Produces: `NetworkSection` (exports `ROWS`) consumed by Task 12.

Backend contract confirmed against `CasaOS/model/share.go` (`type Shares struct { ID uint json:"id"; Anonymous bool json:"anonymous"; Path string json:"path" }`) and `CasaOS/route/v1/samba.go`: `GET /samba/shares` → `{success, data: [{id, anonymous, path}]}`; `POST /samba/shares` takes a **JSON array** `[{path}]` (server forces `anonymous: true` and derives the share name from the path); `DELETE /samba/shares/:id`. This is the "serve a local folder over SMB" feature — deliberately not the separate "connections" API (`GetSambaConnectionsList`/`PostSambaConnectionsCreate`), which mounts a *remote* SMB share as a client and is out of scope (not part of what the user asked for).

- [ ] **Step 1: Create `src/components/settings/NetworkSharesPanel.vue`**

```vue
<template>
	<div class="shares-panel">
		<p class="hint">{{ $t('Share a folder on this box over the local network via SMB.') }}</p>

		<div v-for="s in shares" :key="s.id" class="user-row">
			<div class="user-main">
				<b-icon icon="share" pack="casa" size="is-20"></b-icon>
				<div class="user-name">{{ s.path }}</div>
			</div>
			<b-button rounded size="is-small" type="is-danger" outlined @click="confirmDelete(s)">
				{{ $t('Delete') }}
			</b-button>
		</div>

		<form class="add-user-form" @submit.prevent="createShare">
			<b-input v-model="newPath" :placeholder="$t('Folder path, e.g. /DATA/Shared')" size="is-small" class="add-input"></b-input>
			<b-button rounded size="is-small" type="is-dark" native-type="submit" :loading="creating">
				{{ $t('Share folder') }}
			</b-button>
		</form>
		<p v-if="error" class="error-note">{{ error }}</p>
	</div>
</template>

<script>
export default {
	name: 'network-shares-panel',
	data() {
		return {
			shares: [],
			newPath: '',
			creating: false,
			error: ''
		}
	},
	created() {
		this.refresh()
	},
	methods: {
		refresh() {
			this.$api.samba.getShares().then(res => {
				if (res.data.success === 200) this.shares = res.data.data || []
			})
		},
		createShare() {
			this.error = ''
			if (!this.newPath) return
			this.creating = true
			this.$api.samba.createShare([{ path: this.newPath, anonymous: true }]).then(res => {
				if (res.data.success === 200) {
					this.newPath = ''
					this.refresh()
				} else {
					this.error = res.data.message
				}
			}).catch(e => {
				this.error = e.response && e.response.data ? e.response.data.message : this.$t('Failed to create share')
			}).finally(() => {
				this.creating = false
			})
		},
		confirmDelete(share) {
			this.$buefy.dialog.confirm({
				title: this.$t('Delete share'),
				message: this.$t('Stop sharing {path}?', { path: share.path }),
				type: 'is-danger',
				confirmText: this.$t('Delete'),
				cancelText: this.$t('Cancel'),
				onConfirm: () => {
					this.$api.samba.deleteShare(share.id).then(() => this.refresh())
				}
			})
		}
	}
}
</script>

<style lang="scss" scoped>
.shares-panel {
	display: flex;
	flex-direction: column;
	gap: 0.5rem;
}

.hint {
	font-size: 0.75rem;
	opacity: 0.6;
	margin-bottom: 0.25rem;
}

.user-row {
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: 0.75rem;
	padding: 0.6rem 0;
	border-bottom: 1px solid rgba(0, 0, 0, 0.08);
}

.user-main {
	display: flex;
	align-items: center;
	gap: 0.6rem;
}

.user-name {
	font-weight: 600;
	font-size: 0.85rem;
}

.add-user-form {
	display: flex;
	align-items: center;
	gap: 0.5rem;
	margin-top: 0.75rem;
	flex-wrap: wrap;
}

.add-input {
	width: 16rem;
}
</style>
```

- [ ] **Step 2: Create `src/components/settings/sections/NetworkSection.vue`**

```vue
<template>
	<section class="settings-section">
		<h2 class="section-title">{{ $t('Network & Sharing') }}</h2>

		<div class="setting-row-group">
			<div class="row-label-heading">{{ $t('Network Shares') }}</div>
			<network-shares-panel></network-shares-panel>
		</div>

		<div class="setting-row-group">
			<div class="row-label-heading">{{ $t('Remote Access') }}</div>
			<remote-access-panel></remote-access-panel>
		</div>
	</section>
</template>

<script>
import NetworkSharesPanel from '@/components/settings/NetworkSharesPanel.vue'
import RemoteAccessPanel from '@/components/settings/RemoteAccessPanel.vue'

export const ROWS = [
	{ label: 'Network Shares' },
	{ label: 'Remote Access' }
]

export default {
	name: 'network-section',
	components: { NetworkSharesPanel, RemoteAccessPanel }
}
</script>
```

- [ ] **Step 3: Commit**

```bash
git add src/components/settings/NetworkSharesPanel.vue src/components/settings/sections/NetworkSection.vue
git commit -m "Add Network Shares panel and Network & Sharing section"
```

---

### Task 10: `DisksPanel.vue` + `StoragePoolsPanel.vue` + `StorageSection.vue`

**Files:**
- Create: `src/components/settings/DisksPanel.vue`
- Create: `src/components/settings/StoragePoolsPanel.vue`
- Create: `src/components/settings/sections/StorageSection.vue`

**Interfaces:**
- Consumes: `$api.disks.{getDiskList, getUsbs, umount, umountUsb}`, `$api.storage.create`, `$api.sys.{getUsbStatus, toggleUsbAutoMount}` (all already registered), `$api.local_storage.{get, getMergerfsInfo, delete}` (already registered).
- Produces: `StorageSection` (exports `ROWS`) consumed by Task 12.

Backend contracts confirmed against `CasaOS-LocalStorage/model/disk.go`: `Drive{name,size,model,health,temperature,disk_type,need_format,serial,path,children_number}` (all lowercase JSON tags), `USBDriveStatus{name,size,model,avail,children}`, `USBChildren{name,size,avail,mount_point}`. `GET /disk` (`disks.getDiskList()`) returns `{success, data: {disks: Drive[], avail: Drive[]}}` — `avail` is the subset of `disks` with no mount point (safe to identify a disk's "available" status by matching `path` between the two arrays). `POST /storage` (`storage.create({path, name, format})`) mounts (and, if `format:true`, formats first) a disk. Combined-storage (`mergerfs`) is confirmed disabled on this box (`EnableMergerFS=false` in `/etc/casaos/local-storage.conf`) — `StoragePoolsPanel` must show that state, not a broken CRUD form.

- [ ] **Step 1: Create `src/components/settings/DisksPanel.vue`**

```vue
<template>
	<div class="disks-panel">
		<div class="setting-row hover-effect _is-radius">
			<b-icon class="row-icon" icon="usb-outline" pack="casa" size="is-20"></b-icon>
			<div class="row-label">{{ $t('Automount USB Drive') }}</div>
			<div class="row-control">
				<b-switch v-model="autoUsbMount" class="is-flex-direction-row-reverse mr-0" type="is-dark" @input="toggleAutoMount"></b-switch>
			</div>
		</div>

		<div class="row-label-heading mt-4">{{ $t('Available disks') }}</div>
		<p v-if="!avail.length" class="hint">{{ $t('No unformatted disks detected.') }}</p>
		<div v-for="d in avail" :key="d.path" class="user-row">
			<div class="user-main">
				<div class="user-name">{{ d.path }}</div>
				<span class="badge">{{ formatSize(d.size) }} &middot; {{ d.disk_type }}</span>
			</div>
			<b-button rounded size="is-small" type="is-dark" :loading="busyPath === d.path" @click="confirmAdd(d)">
				{{ $t('Use for storage') }}
			</b-button>
		</div>

		<div class="row-label-heading mt-4">{{ $t('Mounted disks') }}</div>
		<div v-for="d in mountedDisks" :key="d.path" class="user-row">
			<div class="user-main">
				<div class="user-name">{{ d.path }}</div>
				<span class="badge">{{ formatSize(d.size) }} &middot; {{ d.disk_type }} &middot; {{ d.health === 'true' ? $t('Healthy') : $t('Check disk') }}</span>
			</div>
			<b-button rounded size="is-small" type="is-danger" outlined :loading="busyPath === d.path" @click="confirmRemove(d)">
				{{ $t('Remove') }}
			</b-button>
		</div>

		<div class="row-label-heading mt-4">{{ $t('USB drives') }}</div>
		<p v-if="!usb.length" class="hint">{{ $t('No USB drives connected.') }}</p>
		<div v-for="u in usb" :key="u.name" class="user-row">
			<div class="user-main">
				<div class="user-name">{{ u.name }}</div>
				<span class="badge">{{ formatSize(u.size) }}</span>
			</div>
			<b-button v-for="c in u.children" :key="c.mount_point" rounded size="is-small" type="is-danger" outlined @click="confirmEject(c)">
				{{ $t('Eject {mount}', { mount: c.mount_point }) }}
			</b-button>
		</div>
		<p v-if="error" class="error-note">{{ error }}</p>
	</div>
</template>

<script>
export default {
	name: 'disks-panel',
	data() {
		return {
			disks: [],
			avail: [],
			usb: [],
			autoUsbMount: false,
			busyPath: '',
			error: ''
		}
	},
	computed: {
		mountedDisks() {
			return this.disks.filter(d => !this.avail.some(a => a.path === d.path))
		}
	},
	created() {
		this.refresh()
		this.refreshUsb()
		this.getAutoMountStatus()
	},
	methods: {
		formatSize(bytes) {
			if (!bytes) return '0 B'
			const units = ['B', 'KB', 'MB', 'GB', 'TB']
			let i = 0
			let size = bytes
			while (size >= 1024 && i < units.length - 1) {
				size /= 1024
				i++
			}
			return `${size.toFixed(1)} ${units[i]}`
		},
		refresh() {
			this.$api.disks.getDiskList().then(res => {
				if (res.data.success === 200) {
					this.disks = res.data.data.disks || []
					this.avail = res.data.data.avail || []
				}
			})
		},
		refreshUsb() {
			this.$api.disks.getUsbs().then(res => {
				if (res.data.success === 200) this.usb = res.data.data || []
			})
		},
		getAutoMountStatus() {
			this.$api.sys.getUsbStatus().then(res => {
				if (res.data.success === 200) this.autoUsbMount = res.data.data === 'True'
			})
		},
		toggleAutoMount() {
			this.$api.sys.toggleUsbAutoMount({ state: this.autoUsbMount ? 'on' : 'off' })
		},
		confirmAdd(disk) {
			this.$buefy.dialog.confirm({
				title: this.$t('Use disk for storage'),
				message: this.$t('Format and mount {path} for use as storage? Any existing data on it will be erased.', { path: disk.path }),
				type: 'is-danger',
				confirmText: this.$t('Format & use'),
				cancelText: this.$t('Cancel'),
				onConfirm: () => {
					this.busyPath = disk.path
					this.error = ''
					this.$api.storage.create({ path: disk.path, name: '', format: true }).then(res => {
						if (res.data.success !== 200) this.error = res.data.message
						this.refresh()
					}).catch(e => {
						this.error = e.response && e.response.data ? e.response.data.message : this.$t('Failed to add storage')
					}).finally(() => {
						this.busyPath = ''
					})
				}
			})
		},
		confirmRemove(disk) {
			this.$buefy.dialog.confirm({
				title: this.$t('Remove disk'),
				message: this.$t('Unmount and stop using {path} for storage?', { path: disk.path }),
				type: 'is-danger',
				confirmText: this.$t('Remove'),
				cancelText: this.$t('Cancel'),
				onConfirm: () => {
					this.busyPath = disk.path
					this.$api.disks.umount({ path: disk.path }).then(() => this.refresh()).finally(() => {
						this.busyPath = ''
					})
				}
			})
		},
		confirmEject(child) {
			this.$buefy.dialog.confirm({
				title: this.$t('Eject USB drive'),
				message: this.$t('Safely eject {mount}?', { mount: child.mount_point }),
				type: 'is-danger',
				confirmText: this.$t('Eject'),
				cancelText: this.$t('Cancel'),
				onConfirm: () => {
					this.$api.disks.umountUsb({ mount_point: child.mount_point }).then(() => this.refreshUsb())
				}
			})
		}
	}
}
</script>

<style lang="scss" scoped>
.hint {
	font-size: 0.75rem;
	opacity: 0.6;
	margin-bottom: 0.25rem;
}

.mt-4 {
	margin-top: 1.5rem;
}

.user-row {
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: 0.75rem;
	padding: 0.6rem 0;
	border-bottom: 1px solid rgba(0, 0, 0, 0.08);
}

.user-main {
	display: flex;
	flex-direction: column;
	gap: 0.15rem;
}

.user-name {
	font-weight: 600;
	font-size: 0.85rem;
}

.badge {
	font-size: 0.7rem;
	opacity: 0.6;
}
</style>
```

- [ ] **Step 2: Create `src/components/settings/StoragePoolsPanel.vue`**

```vue
<template>
	<div class="storage-pools-panel">
		<div class="row-label-heading">{{ $t('Combined storage (mergerfs)') }}</div>
		<p v-if="!mergeEnabled" class="hint">
			{{ $t('Combined storage is not enabled on this box. Set EnableMergerFS=true in /etc/casaos/local-storage.conf and restart casaos-local-storage to turn it on.') }}
		</p>
		<div v-else>
			<p v-if="!merges.length" class="hint">{{ $t('No combined storage pools configured.') }}</p>
			<div v-for="m in merges" :key="m.mount_point" class="user-row">
				<div class="user-name">{{ m.mount_point }}</div>
				<span class="badge">{{ m.fstype }}</span>
			</div>
		</div>

		<div class="row-label-heading mt-4">{{ $t('Extra mount points') }}</div>
		<p v-if="!mounts.length" class="hint">{{ $t('No extra mount points configured.') }}</p>
		<div v-for="m in mounts" :key="m.mount_point" class="user-row">
			<div class="user-main">
				<div class="user-name">{{ m.mount_point }}</div>
				<span class="badge">{{ m.source }} &middot; {{ m.fstype }}</span>
			</div>
			<b-button rounded size="is-small" type="is-danger" outlined @click="confirmUnmount(m)">
				{{ $t('Unmount') }}
			</b-button>
		</div>
		<p v-if="error" class="error-note">{{ error }}</p>
	</div>
</template>

<script>
export default {
	name: 'storage-pools-panel',
	data() {
		return {
			mounts: [],
			merges: [],
			mergeEnabled: false,
			error: ''
		}
	},
	created() {
		this.loadMounts()
		this.loadMerges()
	},
	methods: {
		loadMounts() {
			this.$api.local_storage.get().then(res => {
				this.mounts = (res.data && res.data.data) || []
			}).catch(() => {
				this.mounts = []
			})
		},
		loadMerges() {
			this.$api.local_storage.getMergerfsInfo().then(res => {
				this.mergeEnabled = true
				this.merges = (res.data && res.data.data) || []
			}).catch(() => {
				this.mergeEnabled = false
			})
		},
		confirmUnmount(mount) {
			this.$buefy.dialog.confirm({
				title: this.$t('Unmount'),
				message: this.$t('Unmount {path}?', { path: mount.mount_point }),
				type: 'is-danger',
				confirmText: this.$t('Unmount'),
				cancelText: this.$t('Cancel'),
				onConfirm: () => {
					this.$api.local_storage.delete({ mount_point: mount.mount_point }).then(() => this.loadMounts()).catch(e => {
						this.error = e.response && e.response.data && e.response.data.message ? e.response.data.message : this.$t('Failed to unmount')
					})
				}
			})
		}
	}
}
</script>

<style lang="scss" scoped>
.hint {
	font-size: 0.75rem;
	opacity: 0.6;
	margin-bottom: 0.25rem;
}

.mt-4 {
	margin-top: 1.5rem;
}

.user-row {
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: 0.75rem;
	padding: 0.6rem 0;
	border-bottom: 1px solid rgba(0, 0, 0, 0.08);
}

.user-main {
	display: flex;
	flex-direction: column;
	gap: 0.15rem;
}

.user-name {
	font-weight: 600;
	font-size: 0.85rem;
}

.badge {
	font-size: 0.7rem;
	opacity: 0.6;
}
</style>
```

- [ ] **Step 3: Create `src/components/settings/sections/StorageSection.vue`**

```vue
<template>
	<section class="settings-section">
		<h2 class="section-title">{{ $t('Storage') }}</h2>

		<div class="setting-row-group">
			<div class="row-label-heading">{{ $t('Disks & USB') }}</div>
			<disks-panel></disks-panel>
		</div>

		<div class="setting-row-group">
			<div class="row-label-heading">{{ $t('Storage Pools') }}</div>
			<storage-pools-panel></storage-pools-panel>
		</div>
	</section>
</template>

<script>
import DisksPanel from '@/components/settings/DisksPanel.vue'
import StoragePoolsPanel from '@/components/settings/StoragePoolsPanel.vue'

export const ROWS = [
	{ label: 'Automount USB Drive' },
	{ label: 'Disks & USB' },
	{ label: 'Storage Pools' }
]

export default {
	name: 'storage-section',
	components: { DisksPanel, StoragePoolsPanel }
}
</script>
```

- [ ] **Step 4: Commit**

```bash
git add src/components/settings/DisksPanel.vue src/components/settings/StoragePoolsPanel.vue src/components/settings/sections/StorageSection.vue
git commit -m "Add Disks/USB panel, Storage Pools panel, and Storage section"
```

---

### Task 11: Fold Account into Users & Access

**Files:**
- Modify: `src/components/settings/sections/UsersSection.vue`
- Delete: `src/components/settings/sections/AccountSection.vue`

**Interfaces:**
- Produces: `UsersSection`'s `PANELS` map gains an `account` entry (`AccountPanel`, rendered with `embedded: true`); `ROWS` gains a `'My Account'` entry; default `usersTab` becomes `'account'`. Consumed by Task 12 (shell) — Task 12 must NOT import `AccountSection` at all, and the nav has 6 entries, not 7 (see Global Constraints IA-change note).

This is a mid-execution addition: the user found a separate top-level "Account" section next to "Users & Access" redundant and asked for it folded in as a 4th tab/option alongside CasaOS/System/SMB Users, sharing the exact same wide-tabs/narrow-dropdown switcher `UsersSection.vue` already has from Task 6. `AccountSection.vue` (Task 5's output) was never wired into anything (Task 12 hasn't run yet), so deleting it now is a clean removal of unused code, not a revert of shipped behavior.

- [ ] **Step 1: Add the Account tab to `UsersSection.vue`**

Find:

```vue
<template>
	<section class="settings-section">
		<h2 class="section-title">{{ $t('Users & Access') }}</h2>

		<b-select v-if="narrow" v-model="usersTab" class="set-select users-tab-select" size="is-small">
			<option value="casaos">{{ $t('CasaOS Users') }}</option>
			<option value="system">{{ $t('System Users') }}</option>
			<option value="smb">{{ $t('SMB Users') }}</option>
		</b-select>
		<b-tabs v-else v-model="usersTab" type="is-toggle" size="is-small" :animated="false" class="settings-tabs">
			<b-tab-item :label="$t('CasaOS Users')" value="casaos"></b-tab-item>
			<b-tab-item :label="$t('System Users')" value="system"></b-tab-item>
			<b-tab-item :label="$t('SMB Users')" value="smb"></b-tab-item>
		</b-tabs>

		<component :is="activePanel"></component>
	</section>
</template>

<script>
import CasaosUsersPanel from '@/components/settings/CasaosUsersPanel.vue'
import SystemUsersPanel from '@/components/settings/SystemUsersPanel.vue'
import SmbUsersPanel from '@/components/settings/SmbUsersPanel.vue'

export const ROWS = [
	{ label: 'CasaOS Users' },
	{ label: 'System Users' },
	{ label: 'SMB Users' }
]

const PANELS = {
	casaos: 'CasaosUsersPanel',
	system: 'SystemUsersPanel',
	smb: 'SmbUsersPanel'
}

export default {
	name: 'users-section',
	components: { CasaosUsersPanel, SystemUsersPanel, SmbUsersPanel },
	props: {
		narrow: { type: Boolean, default: false }
	},
	data() {
		return { usersTab: 'casaos' }
	},
	computed: {
		activePanel() {
			return PANELS[this.usersTab]
		}
	}
}
</script>
```

Replace with:

```vue
<template>
	<section class="settings-section">
		<h2 class="section-title">{{ $t('Users & Access') }}</h2>

		<b-select v-if="narrow" v-model="usersTab" class="set-select users-tab-select" size="is-small">
			<option value="account">{{ $t('My Account') }}</option>
			<option value="casaos">{{ $t('CasaOS Users') }}</option>
			<option value="system">{{ $t('System Users') }}</option>
			<option value="smb">{{ $t('SMB Users') }}</option>
		</b-select>
		<b-tabs v-else v-model="usersTab" type="is-toggle" size="is-small" :animated="false" class="settings-tabs">
			<b-tab-item :label="$t('My Account')" value="account"></b-tab-item>
			<b-tab-item :label="$t('CasaOS Users')" value="casaos"></b-tab-item>
			<b-tab-item :label="$t('System Users')" value="system"></b-tab-item>
			<b-tab-item :label="$t('SMB Users')" value="smb"></b-tab-item>
		</b-tabs>

		<component :is="activePanel" v-bind="activePanelProps"></component>
	</section>
</template>

<script>
import AccountPanel from '@/components/account/AccountPanel.vue'
import CasaosUsersPanel from '@/components/settings/CasaosUsersPanel.vue'
import SystemUsersPanel from '@/components/settings/SystemUsersPanel.vue'
import SmbUsersPanel from '@/components/settings/SmbUsersPanel.vue'

export const ROWS = [
	{ label: 'My Account' },
	{ label: 'CasaOS Users' },
	{ label: 'System Users' },
	{ label: 'SMB Users' }
]

const PANELS = {
	account: 'AccountPanel',
	casaos: 'CasaosUsersPanel',
	system: 'SystemUsersPanel',
	smb: 'SmbUsersPanel'
}

export default {
	name: 'users-section',
	components: { AccountPanel, CasaosUsersPanel, SystemUsersPanel, SmbUsersPanel },
	props: {
		narrow: { type: Boolean, default: false }
	},
	data() {
		return { usersTab: 'account' }
	},
	computed: {
		activePanel() {
			return PANELS[this.usersTab]
		},
		activePanelProps() {
			return this.usersTab === 'account' ? { embedded: true } : {}
		}
	}
}
</script>
```

(The `<style>` block is unchanged — leave it exactly as-is.)

- [ ] **Step 2: Delete the now-unused `AccountSection.vue`**

```bash
git rm src/components/settings/sections/AccountSection.vue
```

- [ ] **Step 3: Verify**

Run `pnpm test -- --run` — full suite should still pass (no test covers either file directly). Grep to confirm nothing references the deleted file: `grep -rl "AccountSection" src` should return nothing.

- [ ] **Step 4: Commit**

```bash
git add src/components/settings/sections/UsersSection.vue
git commit -m "Fold Account into Users & Access as a 4th tab; remove now-unused AccountSection"
```

---

### Task 12: Rewrite `SettingsApp.vue` as the shell (final integration)

**Files:**
- Modify: `src/components/desktop/SettingsApp.vue`

**Interfaces:**
- Consumes: `SettingsNav`/`SettingsSearch` (Task 3/4), `classifyWidth` (Task 3), all 6 section components and their exported `ROWS` (Tasks 5–10, minus `AccountSection` which Task 11 deleted), plus Task 11's amended `UsersSection`. This is the last task before manual verification — everything from Tasks 1–11 gets wired together here.

This fully replaces the current monolithic `SettingsApp.vue` (the file already registered in `DesktopWindow.vue`'s `COMPONENT_REGISTRY` — no change needed there, since the import path `./SettingsApp.vue` and export name stay the same). Per the Global Constraints IA-change note, there is no `account` entry anywhere below — Account was folded into Users & Access in Task 11.

- [ ] **Step 1: Replace the entire contents of `src/components/desktop/SettingsApp.vue`**

```vue
<template>
	<div class="settings-app">
		<settings-nav :sections="sections" :active-section="activeSection" :compact="compact" @select="activeSection = $event"></settings-nav>

		<div class="settings-main">
			<settings-search :rows="searchRows" @jump="activeSection = $event"></settings-search>

			<div ref="content" class="settings-content" :class="{ 'is-narrow': narrow }">
				<users-section v-if="activeSection === 'users'" :narrow="narrow"></users-section>
				<appearance-section v-else-if="activeSection === 'appearance'"></appearance-section>
				<network-section v-else-if="activeSection === 'network'"></network-section>
				<storage-section v-else-if="activeSection === 'storage'"></storage-section>
				<general-section v-else-if="activeSection === 'general'"></general-section>
				<system-section v-else-if="activeSection === 'system'"></system-section>
			</div>
		</div>
	</div>
</template>

<script>
import SettingsNav from '@/components/settings/SettingsNav.vue'
import SettingsSearch from '@/components/settings/SettingsSearch.vue'
import AppearanceSection, { ROWS as APPEARANCE_ROWS } from '@/components/settings/sections/AppearanceSection.vue'
import UsersSection, { ROWS as USERS_ROWS } from '@/components/settings/sections/UsersSection.vue'
import NetworkSection, { ROWS as NETWORK_ROWS } from '@/components/settings/sections/NetworkSection.vue'
import StorageSection, { ROWS as STORAGE_ROWS } from '@/components/settings/sections/StorageSection.vue'
import GeneralSection, { ROWS as GENERAL_ROWS } from '@/components/settings/sections/GeneralSection.vue'
import SystemSection, { ROWS as SYSTEM_ROWS } from '@/components/settings/sections/SystemSection.vue'
import { classifyWidth } from '@/utils/settings/breakpoints'

const SECTIONS = [
	{ id: 'users', label: 'Users & Access', icon: 'user-edit-outline', rows: USERS_ROWS },
	{ id: 'appearance', label: 'Appearance', icon: 'wallpaper-outline', rows: APPEARANCE_ROWS },
	{ id: 'network', label: 'Network & Sharing', icon: 'internet-outline', rows: NETWORK_ROWS },
	{ id: 'storage', label: 'Storage', icon: 'storage-other', rows: STORAGE_ROWS },
	{ id: 'general', label: 'General', icon: 'settings-outline', rows: GENERAL_ROWS },
	{ id: 'system', label: 'System', icon: 'system-outline', rows: SYSTEM_ROWS }
]

export default {
	name: 'settings-app',
	components: {
		SettingsNav,
		SettingsSearch,
		AppearanceSection,
		UsersSection,
		NetworkSection,
		StorageSection,
		GeneralSection,
		SystemSection
	},
	data() {
		return {
			activeSection: 'users',
			sections: SECTIONS,
			width: 900,
			resizeObserver: null
		}
	},
	computed: {
		breakpoints() {
			return classifyWidth(this.width)
		},
		compact() {
			return this.breakpoints.navCollapsed
		},
		narrow() {
			return this.breakpoints.rowsStacked
		},
		searchRows() {
			return SECTIONS.flatMap(s => s.rows.map(r => ({ sectionId: s.id, sectionLabel: s.label, label: r.label })))
		}
	},
	mounted() {
		this.resizeObserver = new ResizeObserver(entries => {
			this.width = entries[0].contentRect.width
		})
		this.resizeObserver.observe(this.$el)
	},
	beforeDestroy() {
		if (this.resizeObserver) this.resizeObserver.disconnect()
	}
}
</script>

<style lang="scss" scoped>
.settings-app {
	display: flex;
	height: 100%;
	background: #fff;
	color: #2c3e50;
	font-family: $family-sans-serif;
}

.settings-main {
	flex: 1;
	display: flex;
	flex-direction: column;
	min-width: 0;
}

.settings-content {
	flex: 1;
	overflow: auto;
	padding: 1.75rem 2rem;

	&.is-narrow {
		padding: 1rem 1rem;
	}
}
</style>
```

- [ ] **Step 2: Run the full unit test suite**

Run: `cd /root/casaos-fork/CasaOS-UI && pnpm test`
Expected: PASS — all specs from Tasks 1–4 and 8, plus the pre-existing `file_utils.spec.js`/`vmSidecar.spec.js`.

- [ ] **Step 3: Lint**

Run: `pnpm lint`
Expected: no new errors introduced by any file in this plan (tab indentation was matched throughout; fix anything the linter flags before moving on).

- [ ] **Step 4: Commit**

```bash
git add src/components/desktop/SettingsApp.vue
git commit -m "Rewrite SettingsApp.vue as a responsive shell over the new section components"
```

---

### Task 13: Manual verification pass

No new files — this is the manual QA pass the spec calls for, since this repo has no Vue component test harness (Global Constraints).

- [ ] **Step 1: Start the dev server**

```bash
cd /root/casaos-fork/CasaOS-UI
pnpm dev
```

This proxies API calls to the live backend per the `VUE_APP_DEV_IP`/`VUE_APP_DEV_PORT` env vars already configured in this repo (`vue.config.js`'s `devServer.proxy`). Open `http://localhost:8080`.

- [ ] **Step 2: Window chassis**

- Open Settings from the Dock. Drag it by the titlebar around the desktop — confirm it moves smoothly with no stutter.
- Resize from each of the 4 edges and 4 corners — confirm no stutter and the opposite edge stays anchored.
- Confirm the titlebar shows only minimize and close (no maximize button) — same as Files and Terminal's own titlebars. Also check VM Manager and an app's Edit dialog for the same.
- Reload the page after moving/resizing Settings — confirm the persisted rect on reload matches what was last set (Task 1's fix didn't break normal persistence, it only stopped mid-drag writes).

- [ ] **Step 3: Responsive layout**

- With Settings open and wide (~900px+), confirm the nav rail shows icons + labels.
- Shrink the window below ~736px width — nav rail should collapse to icon-only (hover shows a tooltip via the `title` attribute).
- Shrink further below ~544px — setting rows (e.g. in Appearance or General) should stack label above control instead of overflowing.
- Open Users & Access at a narrow width — the tab switcher should become a dropdown instead of toggle buttons.

- [ ] **Step 4: Each section's happy path**

- **Appearance**: move the transparency/blur sliders, confirm the window backdrop updates live (and confirm the slider labels now match the actual direction — left="Transparent", right="Opaque"); toggle a widget's visibility and confirm it disappears/reappears on the desktop.
- **Users & Access**: confirm all four tabs (My Account, CasaOS/System/SMB users) render — My Account shows the profile panel and edits save; the other three still list and can add/delete a user, same as before the rebuild.
- **Network & Sharing**: create a test SMB share pointing at an existing folder (e.g. `/DATA`), confirm it appears in the list, then delete it. Confirm Remote Access shows real Tailscale status (running, this device's IP) and lists actual tailnet peers with correct online/offline state.
- **Storage**: confirm the disk list and USB list render with real data from this box; toggle USB automount; if a spare/unused disk is available, exercise "Use for storage" end to end — otherwise just confirm the confirm-dialog gate appears and cancel out. Confirm Storage Pools shows the "not enabled" message (expected, `EnableMergerFS=false` on this box) and lists any existing `/v2/local_storage/mount` entries without error.
- **General**: change language, toggle RSS (confirm the consent dialog appears), toggle recommended/existing-app switches.
- **System**: confirm WebUI port editing still works; click Restart/Shutdown and confirm the new real confirm dialog appears (and cancel out — do not actually restart/shut down the box during verification unless intentional); confirm About shows the installed version, architecture, device model, and a non-empty error log excerpt.

- [ ] **Step 5: Search**

- Type a partial row label (e.g. "port", "blur", "usb") into the search box at the top of Settings and confirm matching rows from the right sections appear, and clicking one jumps to that section.

- [ ] **Step 6: Report results**

If anything in Steps 2–5 doesn't match, note exactly which check failed before considering this plan complete — do not mark this task done on an assumption.
