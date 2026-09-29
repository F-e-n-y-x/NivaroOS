<template>
	<div ref="win" class="terminal-window" :class="{ 'is-narrow': narrow }">
		<!-- This bar IS the window's titlebar (draggable, own minimize/close,
		     no maximize) - same treatment as Files' TabBar.vue. It stays
		     visible with the Logs tab too: as the only titlebar, hiding it
		     would remove drag/minimize/close. -->
		<div class="terminal-tabs" @pointerdown="$emit('drag-start', $event)">
			<div v-for="tab in tabs" :key="tab.key" class="terminal-tab"
				:class="{ active: tab.key === activeKey, 'is-exited': tabExited(tab) }"
				@auxclick.middle.prevent="closeTab(tab.key)">
				<form v-if="renamingKey === tab.key" class="terminal-tab-rename" @submit.prevent="commitTabRename(tab)" @pointerdown.stop>
					<input ref="tabRenameInput" v-model="renameValue" type="text" maxlength="80" :aria-label="$t('Session name')"
						@keydown.esc.prevent.stop="cancelTabRename" @blur="commitTabRename(tab)">
				</form>
				<button v-else type="button" class="terminal-tab-label" :aria-pressed="tab.key === activeKey ? 'true' : 'false'"
					:title="tabTooltip(tab)" @click="activate(tab.key)" @dblclick="startTabRename(tab)">
					<i v-if="tab.type === 'logs'" class="mdi mdi-text-box-outline terminal-tab-icon" aria-hidden="true"></i>
					<i v-else-if="tabKind(tab) === 'container'" class="mdi mdi-docker terminal-tab-icon" aria-hidden="true"></i>
					<span v-if="tab.type === 'shell'" class="terminal-tab-dot" :class="tabDotClass(tab)" aria-hidden="true"></span>
					<span class="one-line">{{ tabTitle(tab) }}</span>
				</button>
				<button type="button" class="terminal-tab-close"
					:title="closeTabTitle(tab)" :aria-label="closeTabTitle(tab)"
					@click.stop="closeTab(tab.key)">
					<b-icon icon="close" size="is-small"></b-icon>
				</button>
			</div>
			<button type="button" class="terminal-tab-add" :title="$t('New terminal')" :aria-label="$t('New terminal')" @click="newTab()">
				<b-icon icon="plus" size="is-small"></b-icon>
			</button>
			<div class="terminal-tabs-spacer"></div>
			<button type="button" class="terminal-tab-add has-badge" :class="{ active: sidebarOpen }"
				:title="$t('Sessions')" :aria-label="$t('Sessions')" :aria-pressed="sidebarOpen ? 'true' : 'false'"
				@click="toggleSidebar">
				<i class="mdi mdi-format-list-bulleted-square" aria-hidden="true"></i>
				<span v-if="backgroundCount" class="terminal-badge">{{ backgroundCount }}</span>
			</button>
			<button type="button" class="terminal-tab-add" :title="$t('New Window')" :aria-label="$t('New Window')" @click="openNewWindow">
				<b-icon icon="open-in-new" size="is-small"></b-icon>
			</button>
			<button type="button" class="logs-button" :class="{ active: hasLogsTab }" @click="openLogsTab">
				<b-icon icon="history-records-outline" pack="casa" custom-size="casa-14px" />
				<span>{{ $t('Logs') }}</span>
			</button>
			<div class="window-controls">
				<button type="button" class="window-btn window-btn-minimize" :title="$t('Minimize')" :aria-label="$t('Minimize')" @click.stop="$emit('minimize')"></button>
				<button type="button" class="window-btn window-btn-close" :title="$t('Close')" :aria-label="$t('Close')" @click.stop="requestClose"></button>
			</div>
		</div>

		<div class="terminal-main">
			<div class="terminal-body">
				<div v-for="tab in shellTabs" :key="tab.key" v-show="tab.key === activeKey" class="terminal-body-layer">
					<terminal-card :ref="'terminal-' + tab.key" :session="tab.initialSession"
						:create-spec="tab.initialSession ? null : tab.createSpec" :init-command="tab.initCommand" closable
						@session="onTabSession(tab, $event)" @exit="onTabExit(tab, $event)" @gone="onTabGone(tab)"
						@state="onTabState(tab, $event)" @close="closeTab(tab.key)" @rename="startTabRename(tab)"
						@end="endTabSession(tab)" @show-sessions="openSidebar"></terminal-card>
				</div>
				<!-- Outside the v-for above: a static ref inside a v-for is
				     always an array in Vue 2. -->
				<div v-if="hasLogsTab" v-show="activeKey === 'logs'" class="terminal-body-layer">
					<logs-card ref="logs" :data="logData" :error="logError"></logs-card>
				</div>

				<div v-if="!tabs.length" class="terminal-empty">
					<i class="mdi mdi-console terminal-empty-icon" aria-hidden="true"></i>
					<p class="terminal-empty-title">{{ $t('No terminal open') }}</p>
					<p v-if="backgroundCount" class="terminal-empty-text">
						{{ $t('{n} session(s) still running in the background.', { n: backgroundCount }) }}
					</p>
					<div class="terminal-empty-actions">
						<button type="button" class="terminal-empty-btn" @click="newTab()">
							<i class="mdi mdi-plus mr-1" aria-hidden="true"></i>{{ $t('New terminal') }}
						</button>
						<button v-if="backgroundCount && !sidebarOpen" type="button" class="terminal-empty-btn is-quiet" @click="openSidebar">
							{{ $t('Show sessions') }}
						</button>
					</div>
				</div>
			</div>

			<aside v-if="sidebarOpen" class="terminal-sidebar" :aria-label="$t('Terminal sessions')">
				<session-list show-close :sessions="allSessions" :loading="listLoading" :error="listError"
					:open-keys="openKeys" :current-key="currentSessionKey" :new-label="$t('New terminal')"
					:footnote="sidebarFootnote"
					@refresh="refreshSessions" @close="sidebarOpen = false" @open="openSession" @new="newTab()"
					@rename="renameSession" @kill="killSession"></session-list>
			</aside>
		</div>

		<b-loading v-model="isLoading" :is-full-page="false"></b-loading>
	</div>
</template>

<script>
import TerminalCard from './TerminalCard.vue'
import LogsCard from './LogsCard.vue'
import SessionList from './SessionList.vue'
import terminalSessions from '@/service/terminalSessions.js'
import { apiError } from '@/utils/apiError'
import {
	sessionKey, familyOf, isIdleShell, loadWindowLayout, saveWindowLayout, forgetWindowLayout,
	saveLastClosed, takeLastClosed, restorableTabs,
} from './termSessions.js'

const LIST_POLL_MS = 5000

function escapeHtml(s) {
	return String(s || '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]))
}

let tabSeq = 0
function nextTabKey() {
	tabSeq += 1
	return 't' + Date.now().toString(36) + tabSeq
}

export default {
	name: 'terminal-panel',
	components: {
		TerminalCard,
		LogsCard,
		SessionList,
	},
	props: {
		// A command to type and run in a new shell as soon as it is live -
		// e.g. Settings' "Open Terminal" for an rclone-authorize cloud account.
		initCommand: { type: String, default: '' },
		// Stamped by OPEN_WINDOW when this window is asked for again.
		requestedAt: { type: Number, default: 0 },
		// A window opened with "New Window" starts with a new shell instead
		// of picking up the sessions of the last closed window.
		fresh: { type: Boolean, default: false },
	},
	data() {
		return {
			isLoading: false,
			logData: '',
			logError: '',
			logsInFlight: false,
			timer: null,
			// { key, type: 'shell'|'logs', session, createSpec, initCommand,
			//   state, exit, gone }. All open tabs stay mounted (v-show), so
			// switching never disconnects a shell.
			tabs: [],
			activeKey: null,
			sidebarOpen: false,
			allSessions: [],
			listLoading: false,
			listError: '',
			renamingKey: '',
			renameValue: '',
			narrow: false,
		}
	},
	computed: {
		windowId() {
			const w = this.$parent && this.$parent.win
			return (w && w.id) || 'terminal'
		},
		hasLogsTab() {
			return this.tabs.some(t => t.type === 'logs')
		},
		shellTabs() {
			return this.tabs.filter(t => t.type === 'shell')
		},
		openKeys() {
			return this.shellTabs.filter(t => t.session).map(t => sessionKey(t.session))
		},
		currentSessionKey() {
			const tab = this.tabs.find(t => t.key === this.activeKey)
			return tab && tab.session ? sessionKey(tab.session) : ''
		},
		// Running sessions not shown in this window.
		backgroundCount() {
			const open = new Set(this.openKeys)
			return this.allSessions.filter(s => s.state === 'running' && !open.has(sessionKey(s))).length
		},
		sidebarFootnote() {
			return this.$t('Sessions keep running when you close a tab or this window. They end if the server restarts.')
		},
		layout() {
			const tabs = this.shellTabs.filter(t => t.session && t.session.id && !this.tabExited(t) && !t.gone)
				.map(t => ({ id: t.session.id, kind: familyOf(t.session) }))
			const active = this.tabs.find(t => t.key === this.activeKey)
			return { tabs, active: active && active.session ? active.session.id : null }
		},
	},
	watch: {
		layout: {
			deep: true,
			handler(l) {
				if (this.initialised) saveWindowLayout(this.windowId, l)
			},
		},
		requestedAt() {
			if (this.initCommand) this.newTab({ initCommand: this.initCommand })
		},
		sidebarOpen(open) {
			if (open) this.refreshSessions()
			this.$nextTick(() => {
				const ref = this.activeShellRef()
				if (ref) ref.active(true)
			})
		},
	},
	mounted() {
		// Logs are only fetched while the Logs tab is on screen.
		this.timer = setInterval(() => {
			if (this.logsVisible()) this.getLogs()
			if (this.sidebarOpen && this.onScreen()) this.refreshSessions(true)
		}, LIST_POLL_MS)
		document.addEventListener('visibilitychange', this.onVisibility)
		this.resizeObserver = new ResizeObserver(() => {
			const w = this.$refs.win
			if (w) this.narrow = w.clientWidth < 620
		})
		this.resizeObserver.observe(this.$refs.win)
		this.init()
	},
	beforeDestroy() {
		clearInterval(this.timer)
		document.removeEventListener('visibilitychange', this.onVisibility)
		if (this.resizeObserver) this.resizeObserver.disconnect()
		// The window is being closed (a page reload doesn't destroy it):
		// its sessions keep running, and the next Terminal picks them up.
		const layout = this.layout
		if (layout.tabs.length) saveLastClosed(layout)
		forgetWindowLayout(this.windowId)
	},
	methods: {
		async init() {
			if (this.initCommand) {
				this.newTab({ initCommand: this.initCommand })
				this.initialised = true
				this.refreshSessions(true)
				return
			}
			const saved = loadWindowLayout(this.windowId) || (this.fresh ? null : takeLastClosed())
			if (saved) {
				this.isLoading = true
				try {
					const { sessions, errors } = await terminalSessions.listAll()
					this.allSessions = sessions
					const failedKinds = Object.keys(errors || {})
					const { tabs, active } = restorableTabs(saved, sessions)
					tabs.forEach(s => this.addTab({ session: s }, false))
					// A family whose list failed: try its saved tabs anyway, the
					// card finds out whether they still exist.
					saved.tabs.filter(t => failedKinds.includes(t.kind)).forEach(t => this.addTab({ session: { id: t.id, kind: t.kind, title: '' } }, false))
					if (this.tabs.length) {
						const tab = this.tabs.find(t => t.session && t.session.id === active) || this.tabs[0]
						this.activate(tab.key)
					} else {
						this.$buefy.toast.open({
							message: this.$t('Your previous terminal session has ended.'),
							type: 'is-dark',
							position: 'is-bottom',
							duration: 3500,
						})
					}
				} catch (e) {
					saved.tabs.forEach(t => this.addTab({ session: { id: t.id, kind: t.kind, title: '' } }, false))
					if (this.tabs.length) this.activate(this.tabs[0].key)
				} finally {
					this.isLoading = false
				}
			}
			if (!this.tabs.length) this.newTab()
			this.initialised = true
			saveWindowLayout(this.windowId, this.layout)
		},

		onScreen() {
			return !document.hidden && !!this.$el && this.$el.getClientRects().length > 0
		},
		logsVisible() {
			return this.activeKey === 'logs' && this.onScreen()
		},
		onVisibility() {
			if (this.logsVisible()) this.getLogs()
			if (this.sidebarOpen && this.onScreen()) this.refreshSessions(true)
		},
		getLogs() {
			if (this.logsInFlight) return
			this.logsInFlight = true
			this.$api.sys.getLogs(500).then(res => {
				const data = (res.data && res.data.data) || ''
				this.logData = typeof data === 'string' ? data.replace(/\n+$/, '') : String(data)
				this.logError = ''
			}).catch(err => {
				this.logError = this.$t('Could not load logs: {reason}', {
					reason: (err && err.response && err.response.data && err.response.data.message) || (err && err.message) || this.$t('Unknown error')
				})
			}).finally(() => {
				this.logsInFlight = false
			})
		},

		// --- sessions list ---------------------------------------------------
		async refreshSessions(silent) {
			if (this.listInFlight) return
			this.listInFlight = true
			if (!silent) this.listLoading = true
			try {
				const { sessions, errors } = await terminalSessions.listAll()
				this.allSessions = sessions
				const failed = Object.keys(errors || {})
				this.listError = failed.length === 2
					? apiError(errors.host, this.$t("Couldn't load the session list"))
					: ''
			} catch (err) {
				this.listError = apiError(err, this.$t("Couldn't load the session list"))
			} finally {
				this.listInFlight = false
				this.listLoading = false
			}
		},
		toggleSidebar() {
			this.sidebarOpen = !this.sidebarOpen
		},
		openSidebar() {
			this.sidebarOpen = true
		},
		openSession(s) {
			const key = sessionKey(s)
			const tab = this.shellTabs.find(t => t.session && sessionKey(t.session) === key)
			if (tab) {
				this.activate(tab.key)
			} else {
				this.addTab({ session: s })
			}
			if (this.narrow) this.sidebarOpen = false
		},
		async renameSession(s, title) {
			try {
				const updated = await terminalSessions.rename(s, title)
				if (updated) this.applySessionUpdate(updated)
			} catch (err) {
				this.toastError(apiError(err, this.$t("Couldn't rename the session")))
			}
			this.refreshSessions(true)
		},
		async killSession(s) {
			const key = sessionKey(s)
			try {
				await terminalSessions.kill(s)
			} catch (err) {
				if (!err || !err.response || err.response.status !== 404) {
					this.toastError(apiError(err, this.$t("Couldn't end the session")))
					return
				}
			}
			// Its tab (if any) goes too: ending it here was the point.
			this.shellTabs.filter(t => t.session && sessionKey(t.session) === key).forEach(t => this.removeTab(t.key))
			this.allSessions = this.allSessions.filter(x => sessionKey(x) !== key)
			this.refreshSessions(true)
		},
		applySessionUpdate(s) {
			const key = sessionKey(s)
			this.shellTabs.forEach(t => {
				if (t.session && sessionKey(t.session) === key) t.session = Object.assign({}, t.session, s)
			})
			this.allSessions = this.allSessions.map(x => (sessionKey(x) === key ? s : x))
		},

		// --- tabs --------------------------------------------------------------
		addTab(opts, activate = true) {
			const tab = {
				key: nextTabKey(),
				type: 'shell',
				session: opts.session || null,
				// What the card mounts with; `session` then follows the server.
				initialSession: opts.session || null,
				createSpec: opts.session ? null : (opts.createSpec || { kind: 'host' }),
				initCommand: opts.initCommand || '',
				state: 'idle',
				exit: null,
				gone: false,
				wasLive: false,
			}
			this.tabs.push(tab)
			if (activate) this.activate(tab.key)
			return tab
		},
		newTab(opts = {}) {
			return this.addTab({ createSpec: { kind: 'host' }, initCommand: opts.initCommand })
		},
		tabKind(tab) {
			if (tab.session) return familyOf(tab.session)
			return (tab.createSpec && tab.createSpec.kind) || 'host'
		},
		tabTitle(tab) {
			if (tab.type === 'logs') return this.$t('Logs')
			if (tab.session && tab.session.title) return tab.session.title
			return tab.session ? this.$t('Terminal') : this.$t('New terminal')
		},
		tabTooltip(tab) {
			if (tab.type !== 'shell' || !tab.session) return this.tabTitle(tab)
			const bits = [this.tabTitle(tab)]
			if (tab.session.cwd) bits.push(tab.session.cwd)
			bits.push(this.$t('Double-click to rename'))
			return bits.join('\n')
		},
		tabExited(tab) {
			return tab.type === 'shell' && (!!tab.exit || (tab.session && tab.session.state === 'exited'))
		},
		tabDotClass(tab) {
			if (this.tabExited(tab) || tab.gone) return 'is-exited'
			if (tab.state === 'reconnecting' || (tab.state === 'connecting' && tab.wasLive)) return 'is-warn'
			if (tab.state === 'live') return 'is-live'
			return 'is-pending'
		},
		closeTabTitle(tab) {
			if (tab.type === 'logs' || this.tabExited(tab) || tab.gone || !tab.session) return this.$t('Close {name}', { name: this.tabTitle(tab) })
			return this.$t('Close tab (the session keeps running)')
		},
		onTabSession(tab, s) {
			tab.session = s
			this.applySessionUpdate(s)
			if (!this.allSessions.some(x => sessionKey(x) === sessionKey(s))) this.allSessions = [s, ...this.allSessions]
		},
		onTabExit(tab, detail) {
			tab.exit = detail
		},
		onTabGone(tab) {
			tab.gone = true
		},
		onTabState(tab, state) {
			tab.state = state
			if (state === 'live') {
				tab.wasLive = true
				tab.exit = null
				tab.gone = false
			}
		},
		activeShellRef() {
			const tab = this.tabs.find(t => t.key === this.activeKey && t.type === 'shell')
			return tab ? this.getTabRef(tab) : null
		},
		// Refs used inside a v-for are arrays in Vue 2 even when unique.
		getTabRef(tab) {
			const ref = tab.type === 'logs' ? this.$refs.logs : this.$refs['terminal-' + tab.key]
			return Array.isArray(ref) ? ref[0] : ref
		},
		activate(key) {
			if (key === this.activeKey) {
				this.$nextTick(() => {
					const ref = this.activeShellRef()
					if (ref) ref.active(true)
				})
				return
			}
			const previous = this.tabs.find(t => t.key === this.activeKey)
			const previousRef = previous && this.getTabRef(previous)
			if (previousRef) previousRef.active(false)
			this.activeKey = key
			// The newly active tab was sized while hidden (v-show = zero
			// width), so it needs a fresh fit.
			this.$nextTick(() => {
				const tab = this.tabs.find(t => t.key === key)
				const ref = tab && this.getTabRef(tab)
				if (ref) ref.active(true)
			})
		},
		removeTab(key) {
			const idx = this.tabs.findIndex(t => t.key === key)
			if (idx < 0) return
			this.tabs.splice(idx, 1)
			if (this.activeKey === key) {
				const fallback = this.tabs[idx] || this.tabs[idx - 1]
				this.activeKey = null
				if (fallback) this.activate(fallback.key)
			}
		},
		// Closing a tab only detaches: the shell keeps running and is one
		// click away in Sessions. An ended session is dismissed for good.
		closeTab(key) {
			const tab = this.tabs.find(t => t.key === key)
			if (!tab) return
			if (tab.type === 'shell' && tab.session && tab.session.id) {
				if (this.tabExited(tab)) {
					terminalSessions.kill(tab.session).catch(() => {})
				} else if (!tab.gone) {
					const s = tab.session
					this.removeTab(key)
					this.$buefy.snackbar.open({
						message: this.$t('<b>{name}</b> keeps running in the background.', { name: escapeHtml(s.title || this.$t('Terminal')) }),
						type: 'is-dark',
						position: 'is-bottom-right',
						actionText: this.$t('End it'),
						queue: false,
						duration: 5000,
						onAction: () => this.killSession(s),
					})
					this.refreshSessions(true)
					return
				}
			}
			this.removeTab(key)
		},
		async endTabSession(tab) {
			const s = tab.session
			if (!s || !s.id) {
				this.removeTab(tab.key)
				return
			}
			if (this.tabExited(tab) || tab.gone) {
				this.closeTab(tab.key)
				return
			}
			let fresh = s
			try {
				fresh = (await terminalSessions.get(familyOf(s), s.id)) || s
			} catch (e) { /* use what we have */ }
			const proceed = () => this.killSession(fresh)
			if (isIdleShell(fresh)) {
				proceed()
				return
			}
			this.$buefy.dialog.confirm({
				title: this.$t('End session?'),
				message: this.$t('<b>{cmd}</b> is still running in {name}. Ending the session stops it.', {
					cmd: escapeHtml(String(fresh.command).split('/').pop()),
					name: escapeHtml(fresh.title),
				}),
				confirmText: this.$t('End session'),
				cancelText: this.$t('Cancel'),
				type: 'is-danger',
				onConfirm: proceed,
			})
		},
		startTabRename(tab) {
			if (tab.type !== 'shell' || !tab.session || !tab.session.id || this.tabExited(tab)) return
			this.renamingKey = tab.key
			this.renameValue = tab.session.title || ''
			this.$nextTick(() => {
				const r = this.$refs.tabRenameInput
				const el = Array.isArray(r) ? r[0] : r
				if (el) {
					el.focus()
					el.select()
				}
			})
		},
		cancelTabRename() {
			const tab = this.tabs.find(t => t.key === this.renamingKey)
			this.renamingKey = ''
			if (tab) this.activate(tab.key)
		},
		commitTabRename(tab) {
			if (this.renamingKey !== tab.key) return
			this.renamingKey = ''
			const title = this.renameValue.trim()
			if (title && tab.session && title !== tab.session.title) this.renameSession(tab.session, title)
			this.activate(tab.key)
		},

		openNewWindow() {
			this.$store.commit('OPEN_WINDOW', {
				id: 'terminal-' + Date.now(),
				title: this.$t('Terminal'),
				component: 'TerminalPanel',
				width: 720,
				height: 480,
				props: { fresh: true },
			})
		},
		// The logs button opens (or re-focuses) the single Logs tab.
		openLogsTab() {
			if (!this.hasLogsTab) {
				this.tabs.push({ key: 'logs', type: 'logs' })
			}
			this.activate('logs')
			this.getLogs()
		},

		// Closing the window detaches every tab; say so, and offer to end them.
		requestClose() {
			const running = this.shellTabs.filter(t => t.session && t.session.id && !this.tabExited(t) && !t.gone).map(t => t.session)
			this.$emit('close')
			if (!running.length) return
			const msg = running.length === 1
				? this.$t('<b>{name}</b> keeps running. Open Terminal again to continue.', { name: escapeHtml(running[0].title || this.$t('Terminal')) })
				: this.$t('{n} terminal sessions keep running. Open Terminal again to continue.', { n: running.length })
			this.$buefy.snackbar.open({
				message: msg,
				type: 'is-dark',
				position: 'is-bottom-right',
				actionText: running.length === 1 ? this.$t('End session') : this.$t('End all'),
				queue: false,
				duration: 6000,
				onAction: () => {
					running.forEach(s => terminalSessions.kill(s).catch(() => {}))
				},
			})
		},

		toastError(message) {
			this.$buefy.toast.open({ message, type: 'is-danger', position: 'is-top', duration: 3500 })
		},
	},
}
</script>

<style lang="scss" scoped>
.terminal-window {
	height: 100%;
	display: flex;
	flex-direction: column;
	background: #1e1e1e;
}

.terminal-main {
	position: relative;
	flex: 1 1 auto;
	min-height: 0;
	display: flex;
}

.terminal-body {
	flex: 1 1 auto;
	min-width: 0;
	min-height: 0;
	position: relative;
}

.terminal-body-layer {
	position: absolute;
	top: 0;
	left: 0;
	right: 0;
	bottom: 0;
	overflow: hidden;

	::v-deep #logs {
		height: 100%;
		min-height: 0;
	}

	::v-deep .terminal-instance {
		height: 100%;
		min-height: 0;
	}
}

.terminal-sidebar {
	flex: 0 0 17rem;
	width: 17rem;
	min-height: 0;
	overflow-y: auto;
	padding: var(--space-3);
	background: #202023;
	border-left: 1px solid rgba(255, 255, 255, 0.08);
	display: flex;
	flex-direction: column;

	> .session-list {
		flex: 1 1 auto;
	}

	// A narrow window can't spare the width: the list floats over the
	// terminal instead of squeezing it.
	.is-narrow & {
		position: absolute;
		top: 0;
		right: 0;
		bottom: 0;
		width: min(17rem, 90%);
		z-index: 12;
		box-shadow: -12px 0 32px rgba(0, 0, 0, 0.45);
	}
}

.terminal-empty {
	position: absolute;
	inset: 0;
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	gap: var(--space-2);
	padding: var(--space-4);
	color: #a1a1aa;
	text-align: center;
	font-size: var(--font-sm);
}

.terminal-empty-icon {
	font-size: 2.5rem;
	color: #52525b;
}

.terminal-empty-title {
	color: #f4f4f5;
	font-weight: 600;
}

.terminal-empty-actions {
	display: flex;
	gap: var(--space-2);
	margin-top: var(--space-2);
}

.terminal-empty-btn {
	display: inline-flex;
	align-items: center;
	height: 2rem;
	padding: 0 var(--space-3);
	border: none;
	border-radius: var(--radius-sm);
	background: var(--console-accent);
	color: #0f1115;
	font-size: var(--font-xs);
	font-weight: 600;
	cursor: pointer;

	&:hover {
		background: #5fe8b0;
	}

	&.is-quiet {
		background: rgba(255, 255, 255, 0.08);
		color: #e4e4e7;

		&:hover {
			background: rgba(255, 255, 255, 0.16);
		}
	}

	&:focus-visible {
		outline: 2px solid var(--console-accent);
		outline-offset: 2px;
	}
}

.terminal-tabs {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	gap: var(--space-1);
	padding: var(--space-1) var(--space-2);
	background: #1e1e1e;
	overflow-x: auto;
	cursor: grab;
}

.terminal-tabs-spacer {
	flex: 1 1 auto;
	min-width: 0.5rem;
}

.window-controls {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	gap: var(--space-2);
	margin-left: var(--space-3);
	// The tab strip scrolls horizontally with many tabs; the window's only
	// Close/Minimize must stay reachable.
	position: sticky;
	right: 0;
	padding-left: var(--space-2);
	background: #1e1e1e;
}

.window-btn {
	width: 0.85rem;
	height: 0.85rem;
	border-radius: 50%;
	border: none;
	cursor: pointer;
	padding: 0;
}

.window-btn-minimize {
	background: #f6bd3b;
}

.window-btn-close {
	background: #f2534a;
}

.terminal-tab {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	max-width: 12rem;
	border: none;
	background: rgba(255, 255, 255, 0.05);
	color: rgba(255, 255, 255, 0.55);
	font-size: var(--font-xs);
	padding: var(--space-1) var(--space-2);
	// Inactive tabs are fully rounded; the active one is flush with the
	// content below it.
	border-radius: var(--radius-sm);
	cursor: pointer;
	flex-shrink: 0;

	&.active {
		border-radius: var(--radius-sm) var(--radius-sm) 0 0;
		background: #1e1e1e;
		color: #fff;
	}

	&.is-exited .one-line {
		text-decoration: line-through;
		text-decoration-color: rgba(255, 255, 255, 0.3);
	}
}

.terminal-tab-label {
	display: flex;
	align-items: center;
	gap: 0.35rem;
	min-width: 0;
	border: none;
	background: transparent;
	color: inherit;
	font: inherit;
	padding: 0;
	cursor: pointer;
}

.terminal-tab-icon {
	font-size: 0.85rem;
	opacity: 0.8;
}

.terminal-tab-dot {
	flex-shrink: 0;
	width: 0.4rem;
	height: 0.4rem;
	border-radius: 50%;
	background: #52525b;

	&.is-live {
		background: #22c55e;
	}

	&.is-warn {
		background: #eab308;
	}

	&.is-pending {
		background: #71717a;
	}

	&.is-exited {
		background: transparent;
		border: 1px solid #71717a;
	}
}

.terminal-tab-rename input {
	width: 9rem;
	height: 1.35rem;
	padding: 0 var(--space-1);
	border: 1px solid var(--console-accent);
	border-radius: var(--radius-xs);
	outline: none;
	background: var(--console-bg);
	color: #fff;
	font: inherit;
}

.terminal-tab-label,
.terminal-tab-close,
.terminal-tab-add,
.logs-button,
.window-btn {
	&:focus-visible {
		outline: 2px solid var(--console-accent);
		outline-offset: 1px;
	}
}

.terminal-tab-close {
	display: flex;
	align-items: center;
	border: none;
	background: transparent;
	color: inherit;
	border-radius: var(--radius-xs);
	padding: 1px;
	cursor: pointer;

	&:hover {
		background: rgba(255, 255, 255, 0.15);
	}
}

.terminal-tab-add {
	position: relative;
	flex-shrink: 0;
	display: flex;
	align-items: center;
	justify-content: center;
	width: 1.6rem;
	height: 1.6rem;
	border: none;
	background: transparent;
	color: rgba(255, 255, 255, 0.5);
	border-radius: var(--radius-sm);
	cursor: pointer;

	&:hover {
		background: rgba(255, 255, 255, 0.08);
		color: #fff;
	}

	&.active {
		background: rgba(255, 255, 255, 0.14);
		color: #fff;
	}
}

.terminal-badge {
	position: absolute;
	top: -2px;
	right: -3px;
	min-width: 0.95rem;
	height: 0.95rem;
	padding: 0 3px;
	border-radius: 999px;
	background: var(--console-accent);
	color: #0f1115;
	font-size: 0.6rem;
	font-weight: 700;
	line-height: 0.95rem;
	text-align: center;
}

.logs-button {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	justify-content: center;
	gap: var(--space-1);
	height: 1.6rem;
	padding: 0 var(--space-2);
	border: none;
	background: transparent;
	color: rgba(255, 255, 255, 0.5);
	font-size: var(--font-xs);
	border-radius: var(--radius-sm);
	cursor: pointer;

	// Buefy's b-icon ships its own margin, which stacked with the gap.
	::v-deep .icon {
		margin: 0 !important;
	}

	&:hover {
		background: rgba(255, 255, 255, 0.08);
		color: #fff;
	}

	&.active {
		background: rgba(255, 255, 255, 0.14);
		color: #fff;
	}
}
</style>
