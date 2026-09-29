<!-- src/apps/download-station/rb/DsRemoteBrowser.vue -->
<!-- Download Station's browser: a real Chromium running on the server
     (services/download-sidecar/rb_*.go), shown here as a picture stream.
     Tabs, the address bar, the right-click menu, dialogs and downloads are
     ours; the page itself never runs in NivaroOS's origin. -->
<template>
	<div class="rb-browser" @keydown.capture="onGlobalKey">
		<div class="tab-strip" role="tablist" :aria-label="$t('Tabs')" @dblclick.self="newTab()">
			<div v-for="(t, i) in tabs" :key="t.id" class="tab" role="tab" :aria-selected="t.id === activeId ? 'true' : 'false'"
				:class="{ active: t.id === activeId, dragging: dragId === t.id, 'drop-before': dropIndex === i && dragId !== t.id }"
				:title="t.title || t.url" draggable="true"
				@click="activate(t.id)" @mousedown.middle.prevent="closeTab(t.id)" @contextmenu.prevent="openTabMenu(t, $event)"
				@dragstart="onDragStart(t, $event)" @dragover.prevent="dropIndex = i" @drop.prevent="onDrop(i)" @dragend="onDragEnd">
				<span class="tab-icon">
					<b-icon v-if="t.loading" icon="loading" custom-class="mdi-spin" custom-size="mdi-14px"></b-icon>
					<b-icon v-else-if="t.crashed" icon="emoticon-sad-outline" custom-size="mdi-14px"></b-icon>
					<img v-else-if="t.favicon" :src="t.favicon" alt="" />
					<b-icon v-else icon="web" custom-size="mdi-14px"></b-icon>
				</span>
				<span class="tab-title">{{ tabTitle(t) }}</span>
				<button class="tab-close" type="button" :title="$t('Close tab') + ' (Alt+W)'" :aria-label="$t('Close tab')" @click.stop="closeTab(t.id)">
					<b-icon icon="close" custom-size="mdi-14px"></b-icon>
				</button>
			</div>
			<button class="tab-new" type="button" :title="$t('New tab') + ' (Alt+T)'" :aria-label="$t('New tab')" :disabled="tabs.length >= maxTabs" @click="newTab()">
				<b-icon icon="plus" custom-size="mdi-16px"></b-icon>
			</button>
		</div>

		<div class="address-bar">
			<button class="ds-icon-btn is-flat" type="button" :title="$t('Back') + ' (Alt+←) — ' + $t('right-click for history')" :aria-label="$t('Back')"
				:disabled="!active || !active.canBack" @click="onNavClick('back')" @contextmenu.prevent="openHistoryMenu('back', $event)"
				@mousedown.left="startLongPress('back', $event)" @mouseup="cancelLongPress" @mouseleave="cancelLongPress">
				<b-icon icon="arrow-left" custom-size="mdi-18px"></b-icon>
			</button>
			<button class="ds-icon-btn is-flat" type="button" :title="$t('Forward') + ' (Alt+→)'" :aria-label="$t('Forward')"
				:disabled="!active || !active.canFwd" @click="onNavClick('fwd')" @contextmenu.prevent="openHistoryMenu('fwd', $event)"
				@mousedown.left="startLongPress('fwd', $event)" @mouseup="cancelLongPress" @mouseleave="cancelLongPress">
				<b-icon icon="arrow-right" custom-size="mdi-18px"></b-icon>
			</button>
			<button v-if="active && active.loading" class="ds-icon-btn is-flat" type="button" :title="$t('Stop') + ' (Esc)'" :aria-label="$t('Stop')" @click="send({ t: 'stop', tab: activeId })">
				<b-icon icon="close" custom-size="mdi-18px"></b-icon>
			</button>
			<button v-else class="ds-icon-btn is-flat" type="button" :title="$t('Reload') + ' (F5)'" :aria-label="$t('Reload')" :disabled="!active" @click="reload($event.shiftKey)">
				<b-icon icon="refresh" custom-size="mdi-18px"></b-icon>
			</button>
			<button class="ds-icon-btn is-flat" type="button" :title="$t('Home')" :aria-label="$t('Home')" @click="navigate(homePage)">
				<b-icon icon="home-outline" custom-size="mdi-18px"></b-icon>
			</button>

			<div class="url-field" :class="{ focused: editing, secure: isHttps }">
				<b-icon :icon="isHttps ? 'lock-outline' : isHttp ? 'lock-open-alert-outline' : 'magnify'" custom-size="mdi-16px" class="url-icon"
					:title="isHttps ? $t('Secure connection') : isHttp ? $t('Not secure') : ''"></b-icon>
				<input ref="address" v-model="addressText" class="url-input" :class="{ 'is-masked': !editing && displayParts.host }" spellcheck="false" autocomplete="off"
					:aria-label="$t('Address bar')" :placeholder="$t('Search or enter address')"
					@focus="onAddressFocus" @blur="onAddressBlur" @keydown="onAddressKey" @input="onAddressInput" />
				<div v-if="!editing && displayParts.host" class="url-display" aria-hidden="true" @mousedown.prevent="focusAddress">
					<span class="dim">{{ displayParts.before }}</span><span class="host">{{ displayParts.host }}</span><span class="dim">{{ displayParts.after }}</span>
				</div>
				<button v-if="active && active.url && active.url !== 'about:blank'" class="url-copy" type="button" :class="{ done: copied }"
					:title="copied ? $t('Copied') : $t('Copy address')" :aria-label="$t('Copy address')" @mousedown.prevent @click="copyAddress">
					<b-icon :icon="copied ? 'check' : 'content-copy'" custom-size="mdi-16px"></b-icon>
					<span v-if="copied" class="copied-label">{{ $t('Copied') }}</span>
				</button>
				<ul v-if="editing && suggestions.length" class="url-suggest" role="listbox">
					<li v-for="(s, i) in suggestions" :key="s.id || s.url" role="option" :aria-selected="i === suggestIndex ? 'true' : 'false'"
						:class="{ active: i === suggestIndex }" @mousedown.prevent="pickSuggestion(s, $event)">
						<b-icon :icon="s.search ? 'magnify' : 'history'" custom-size="mdi-14px"></b-icon>
						<span class="s-title one-line">{{ s.title || s.url }}</span>
						<span v-if="!s.search" class="s-url one-line">{{ s.url }}</span>
					</li>
				</ul>
				<div v-if="active && active.loading" class="url-progress" :style="{ transform: `scaleX(${Math.max(0.08, active.progress || 0)})` }"></div>
			</div>

			<button class="ds-icon-btn shield-btn" type="button" :class="{ 'is-active': shieldOn }" :aria-pressed="shieldOn ? 'true' : 'false'"
				:title="shieldTitle" :aria-label="shieldTitle" @click="openShieldMenu($event)">
				<b-icon :icon="shieldOn ? 'shield-check-outline' : 'shield-off-outline'" custom-size="mdi-18px"></b-icon>
				<span v-if="shieldOn && active && active.blocked" class="shield-count">{{ active.blocked > 99 ? '99+' : active.blocked }}</span>
			</button>
			<button class="ds-icon-btn" type="button" :title="$t('Download this page\'s address with Download Station')" :aria-label="$t('Download this page\'s address with Download Station')"
				:disabled="!active || !isWebUrl(active.url)" @click="captureAndAdd(active.url)">
				<b-icon icon="download" custom-size="mdi-18px"></b-icon>
			</button>
			<button class="ds-icon-btn" type="button" :title="$t('More')" :aria-label="$t('More')" @click="openMoreMenu($event)">
				<b-icon icon="dots-vertical" custom-size="mdi-18px"></b-icon>
			</button>
		</div>

		<div class="frame-area">
			<ds-browser-viewport ref="viewport" :class="{ 'is-offstage': !showViewport }" :send="send" :tab="activeId" :inert="!!(menu || dialog || chooser || source)"
				:label="active ? active.title || active.url : ''" @resize="onViewportResize" @contextmenu="onPageContextMenu" @shortcut="onShortcut">
				<div v-if="slow" class="rb-badge" role="status">
					<b-icon icon="speedometer-slow" custom-size="mdi-14px"></b-icon>{{ $t('Slow connection - lower picture quality') }}
				</div>
				<form v-if="findOpen" class="find-bar" role="search" @submit.prevent="find(false)">
					<input ref="find" v-model="findText" class="ds-input" :class="{ miss: findMiss }" :aria-label="$t('Find in page')" :placeholder="$t('Find in page')"
						@keydown.esc.prevent="closeFind" @keydown.enter.shift.prevent="find(true)" @input="findMiss = false" />
					<span v-if="findMiss" class="find-miss">{{ $t('No matches') }}</span>
					<button class="ds-icon-btn is-flat" type="button" :title="$t('Previous')" :aria-label="$t('Previous')" @click="find(true)"><b-icon icon="chevron-up" custom-size="mdi-18px"></b-icon></button>
					<button class="ds-icon-btn is-flat" type="submit" :title="$t('Next')" :aria-label="$t('Next')"><b-icon icon="chevron-down" custom-size="mdi-18px"></b-icon></button>
					<button class="ds-icon-btn is-flat" type="button" :title="$t('Close')" :aria-label="$t('Close')" @click="closeFind"><b-icon icon="close" custom-size="mdi-18px"></b-icon></button>
				</form>
				<div v-if="active && active.crashed" class="rb-panel" role="alert">
					<b-icon icon="emoticon-sad-outline" custom-size="mdi-48px"></b-icon>
					<p class="panel-title">{{ $t('This page stopped working') }}</p>
					<p>{{ $t('The page used too much memory or hit a problem. Reloading usually fixes it.') }}</p>
					<button class="ds-primary-btn" type="button" @click="reload(false)">{{ $t('Reload') }}</button>
				</div>
			</ds-browser-viewport>

			<!-- New tab / blank page -->
			<div v-if="showStart" class="rb-start">
				<div class="start-inner">
					<b-icon icon="web" custom-size="mdi-48px" class="start-logo"></b-icon>
					<form class="start-form" @submit.prevent="submitStart">
						<b-icon icon="magnify" custom-size="mdi-20px" class="start-icon"></b-icon>
						<input ref="startInput" v-model="startText" class="start-input" :aria-label="$t('Search or enter address')" :placeholder="$t('Search DuckDuckGo or type an address')" />
					</form>
					<p class="start-hint">{{ $t('Click a download link on any page and the file goes to Download Station, with the page\'s sign-in, over several connections. Right-click a link for more.') }}</p>
					<div v-if="recent.length" class="start-recent">
						<button v-for="r in recent" :key="r.id" type="button" class="recent-tile" :title="r.url" @click="navigate(r.url)">
							<span class="recent-host one-line">{{ hostOf(r.url) }}</span>
							<span class="recent-title one-line">{{ r.title || r.url }}</span>
						</button>
					</div>
				</div>
			</div>

			<div v-if="state === 'connecting' || (state === 'reconnecting' && !everOpen)" class="rb-panel rb-starting" role="status">
				<div class="skeleton-bar"></div>
				<p class="panel-title">{{ $t('Starting the browser...') }}</p>
			</div>
			<div v-else-if="state === 'reconnecting'" class="rb-reconnect" role="status">
				<b-icon icon="loading" custom-class="mdi-spin" custom-size="mdi-16px"></b-icon>{{ $t('Reconnecting to the browser...') }}
			</div>
			<div v-else-if="state === 'fatal'" class="rb-panel" role="alert">
				<b-icon :icon="fatal && fatal.reason === 'busy' ? 'account-multiple-outline' : 'alert-circle-outline'" custom-size="mdi-48px"></b-icon>
				<p class="panel-title">{{ fatalTitle }}</p>
				<p>{{ fatal && fatal.text }}</p>
				<div class="panel-actions">
					<button class="ds-primary-btn" type="button" @click="restart">{{ $t('Try again') }}</button>
					<button class="ds-secondary-btn" type="button" @click="$emit('fallback', fatal ? fatal.reason : 'error')">{{ $t('Use Lite mode') }}</button>
				</div>
			</div>

			<ds-browser-history v-if="historyOpen" class="rb-history" @open="(url, nt) => { historyOpen = false; nt ? newTab(url) : navigate(url) }"></ds-browser-history>
			<button v-if="historyOpen" class="ds-icon-btn history-close" type="button" :title="$t('Close history')" :aria-label="$t('Close history')" @click="historyOpen = false">
				<b-icon icon="close" custom-size="mdi-18px"></b-icon>
			</button>

			<!-- Page dialogs: alert / confirm / prompt / leave page / sign-in -->
			<div v-if="dialog" class="rb-modal" role="dialog" aria-modal="true" :aria-label="dialogTitle">
				<form class="rb-dialog" @submit.prevent="answerDialog(true)">
					<p class="dialog-origin">{{ dialogTitle }}</p>
					<p class="dialog-message">{{ dialog.message }}</p>
					<input v-if="dialog.kind === 'prompt'" ref="dialogInput" v-model="dialogText" class="ds-input" :aria-label="dialog.message" />
					<template v-if="dialog.kind === 'auth'">
						<input ref="dialogInput" v-model="dialogUser" class="ds-input" autocomplete="username" :aria-label="$t('Username')" :placeholder="$t('Username')" />
						<input v-model="dialogPass" class="ds-input" type="password" autocomplete="current-password" :aria-label="$t('Password')" :placeholder="$t('Password')" />
					</template>
					<div class="dialog-actions">
						<button v-if="dialog.kind !== 'alert'" class="ds-secondary-btn" type="button" @click="answerDialog(false)">
							{{ dialog.kind === 'beforeunload' ? $t('Stay') : $t('Cancel') }}
						</button>
						<button ref="dialogOk" class="ds-primary-btn" type="submit">
							{{ dialog.kind === 'beforeunload' ? $t('Leave') : dialog.kind === 'auth' ? $t('Sign in') : $t('OK') }}
						</button>
					</div>
				</form>
			</div>

			<!-- A page asked for a file -->
			<div v-if="chooser" class="rb-modal" role="dialog" aria-modal="true" :aria-label="$t('Choose a file')">
				<div class="rb-dialog">
					<p class="dialog-origin">{{ active ? hostOf(active.url) : '' }}</p>
					<p class="dialog-message">{{ chooser.multiple ? $t('This page wants you to choose files to upload.') : $t('This page wants you to choose a file to upload.') }}</p>
					<p class="dialog-note">{{ $t('Files are sent from this computer to the browser on your NivaroOS box, then to the page.') }}</p>
					<div v-if="chooser.progress !== null" class="ds-progress"><div class="ds-progress-fill" :style="{ width: Math.round(chooser.progress * 100) + '%' }"></div></div>
					<input ref="fileInput" type="file" class="is-hidden-input" :multiple="chooser.multiple" @change="uploadChosen" />
					<div class="dialog-actions">
						<button class="ds-secondary-btn" type="button" :disabled="chooser.progress !== null" @click="cancelChooser">{{ $t('Cancel') }}</button>
						<button class="ds-primary-btn" type="button" :disabled="chooser.progress !== null" @click="$refs.fileInput.click()">
							<b-icon icon="paperclip" custom-size="mdi-16px"></b-icon>{{ chooser.multiple ? $t('Choose files...') : $t('Choose file...') }}
						</button>
					</div>
				</div>
			</div>

			<!-- View source -->
			<div v-if="source" class="rb-modal" role="dialog" aria-modal="true" :aria-label="$t('Page source')" @keydown.esc="source = null">
				<div class="rb-source">
					<div class="source-head">
						<span class="one-line mono">{{ source.url }}</span>
						<button class="ds-icon-btn is-flat" type="button" :title="$t('Copy')" :aria-label="$t('Copy')" @click="copy(source.text, $t('Source copied'))"><b-icon icon="content-copy" custom-size="mdi-16px"></b-icon></button>
						<button class="ds-icon-btn is-flat" type="button" :title="$t('Close')" :aria-label="$t('Close')" @click="source = null"><b-icon icon="close" custom-size="mdi-18px"></b-icon></button>
					</div>
					<textarea ref="sourceText" class="source-text" readonly :value="source.text" spellcheck="false"></textarea>
				</div>
			</div>
		</div>

		<ds-browser-menu v-if="menu" :sections="menu.sections" :x="menu.x" :y="menu.y" :title="menu.title" :label="menu.label || $t('Menu')"
			@select="menu.onSelect" @close="closeMenu"></ds-browser-menu>
	</div>
</template>

<script>
import { downloadSidecar } from '@/api/downloadSidecar'
import { escapeHtml } from '@/utils/escapeHtml'
import DsBrowserHistory from '../DsBrowserHistory.vue'
import DsBrowserViewport from './DsBrowserViewport.vue'
import DsBrowserMenu from './DsBrowserMenu.vue'
import { RbSession, wsUrl, normalizeAddress, splitForDisplay, hostOf, buildContextMenu, copyText, SEARCH_ENGINES } from './rbClient'

const HIDDEN_DISCONNECT_MS = 30000

export default {
	name: 'ds-remote-browser',
	components: { DsBrowserViewport, DsBrowserMenu, DsBrowserHistory },
	inject: { ds: 'downloadStation' },
	props: {
		visible: { type: Boolean, default: true },
		adblockEnabled: { type: Boolean, default: false },
		allowedSites: { type: Array, default: () => [] }
	},
	data() {
		return {
			state: 'idle',
			everOpen: false,
			fatal: null,
			tabs: [],
			activeId: 0,
			maxTabs: 12,
			addressText: '',
			editing: false,
			copied: false,
			suggestions: [],
			suggestIndex: -1,
			startText: '',
			recent: [],
			homePage: 'https://duckduckgo.com/',
			menu: null,
			dialog: null,
			dialogText: '',
			dialogUser: '',
			dialogPass: '',
			chooser: null,
			source: null,
			findOpen: false,
			findText: '',
			findMiss: false,
			slow: false,
			historyOpen: false,
			dragId: 0,
			dropIndex: -1
		}
	},
	computed: {
		active() {
			return this.tabs.find(t => t.id === this.activeId) || null
		},
		isHttps() {
			return !!(this.active && /^https:/.test(this.active.url))
		},
		isHttp() {
			return !!(this.active && /^http:/.test(this.active.url))
		},
		displayParts() {
			return this.active ? splitForDisplay(this.active.url) : { before: '', host: '', after: '' }
		},
		blank() {
			return !this.active || !this.active.url || this.active.url === 'about:blank'
		},
		showStart() {
			return this.state === 'open' && !!this.active && this.blank && !this.active.loading && !this.historyOpen
		},
		showViewport() {
			return this.state === 'open' || this.state === 'reconnecting'
		},
		activeHost() {
			return this.active ? hostOf(this.active.url) : ''
		},
		siteTrusted() {
			const h = this.activeHost
			return !!h && this.allowedSites.some(s => h === s || h.endsWith('.' + s))
		},
		shieldOn() {
			return this.adblockEnabled && !this.siteTrusted
		},
		shieldTitle() {
			if (!this.adblockEnabled) return this.$t('Ad blocker is off')
			if (this.siteTrusted) return this.$t('Ad blocker is off for this site')
			const n = this.active ? this.active.blocked : 0
			return n ? this.$t('Ad blocker: {n} blocked on this page', { n }) : this.$t('Ad blocker is on')
		},
		fatalTitle() {
			const r = this.fatal && this.fatal.reason
			if (r === 'busy') return this.$t('The browser is busy')
			if (r === 'crashed') return this.$t('The browser stopped working')
			if (r === 'sandbox') return this.$t('The browser can\'t run safely on this system')
			return this.$t('The browser could not start')
		},
		dialogTitle() {
			if (!this.dialog) return ''
			return this.dialog.origin ? this.$t('{site} says', { site: this.dialog.origin }) : this.$t('This page says')
		}
	},
	watch: {
		activeId() {
			this.syncAddress()
			this.findOpen = false
			this.$nextTick(() => this.focusPage())
		},
		'active.url'() {
			this.syncAddress()
			this.maybeRecordHistory()
		},
		'active.title'() {
			this.maybeRecordHistory()
		},
		'active.loading'(loading) {
			if (!loading) {
				this.maybeRecordHistory()
				this.syncAddress()
			}
		},
		visible(v) {
			clearTimeout(this.hideTimer)
			if (v) {
				if (!this.session || this.state === 'closed') this.start()
				this.$nextTick(() => this.focusPage())
			} else {
				// Nothing is shown: drop the stream (the browser keeps the tabs).
				this.hideTimer = setTimeout(() => this.stop(), HIDDEN_DISCONNECT_MS)
			}
		},
		showStart(v) {
			if (v) {
				this.loadRecent()
				this.$nextTick(() => this.$refs.startInput && this.$refs.startInput.focus())
			}
		}
	},
	created() {
		this.seq = 0
		this.pending = {}
		this.pendingNav = null
		this.pendingOpen = null
		this.recorded = {}
		downloadSidecar
			.getSettings()
			.then(s => {
				if (s && s.home_page) this.homePage = s.home_page
			})
			.catch(() => {})
	},
	mounted() {
		this.start()
	},
	beforeDestroy() {
		clearTimeout(this.hideTimer)
		clearTimeout(this.copiedTimer)
		clearTimeout(this.suggestTimer)
		this.stop()
	},
	methods: {
		hostOf,
		isWebUrl(u) {
			return /^https?:/i.test(u || '')
		},
		tabTitle(t) {
			if (!t.url || t.url === 'about:blank') return this.$t('New Tab')
			if (t.title && t.title !== t.url) return t.title
			return hostOf(t.url) || t.url
		},

		// ---- connection ----
		start() {
			this.stop()
			this.fatal = null
			const touch = typeof window !== 'undefined' && window.matchMedia && window.matchMedia('(pointer: coarse)').matches
			this.session = new RbSession({
				url: () => wsUrl(window.location, downloadSidecar.authToken),
				hello: () => {
					const s = this.$refs.viewport ? this.$refs.viewport.size() : { w: 1280, h: 800, dpr: 1 }
					return { ...s, touch, mode: 'browse' }
				},
				onMessage: m => this.onMessage(m),
				onFrame: f => {
					const ack = () => this.send({ t: 'ack' })
					if (this.$refs.viewport) this.$refs.viewport.drawFrame(f, ack)
					else ack()
				},
				onClipImage: png => this.writeClipImage(png),
				onState: (s, detail) => {
					this.state = s
					if (s === 'open') this.everOpen = true
					if (s === 'fatal') this.fatal = detail
				}
			})
			this.session.connect()
		},
		stop() {
			if (this.session) this.session.close()
			this.session = null
		},
		restart() {
			this.everOpen = false
			this.start()
		},
		send(msg) {
			return !!(this.session && this.session.send(msg))
		},

		onMessage(m) {
			switch (m.t) {
				case 'hello':
					this.maxTabs = m.maxTabs || 12
					this.$emit('engine', m.engine)
					break
				case 'tabs': {
					this.tabs = m.tabs || []
					if (m.active) this.activeId = m.active
					else if (!this.tabs.length) this.activeId = 0
					else if (!this.tabs.some(t => t.id === this.activeId)) this.activate(this.tabs[this.tabs.length - 1].id)
					if (this.pendingOpen && this.state === 'open') {
						const p = this.pendingOpen
						this.pendingOpen = null
						this.openUrl(p.url, p.newTab)
					} else if (!this.tabs.length && !this.creatingFirst) {
						this.creatingFirst = true
						this.send({ t: 'tab.new', url: 'about:blank' })
						setTimeout(() => (this.creatingFirst = false), 3000)
					}
					break
				}
				case 'cursor':
					if (this.$refs.viewport) this.$refs.viewport.setCursor(m.css)
					break
				case 'hit':
				case 'clip':
				case 'captured':
				case 'source':
				case 'history': {
					const key = m.t === 'history' ? 'history' : m.seq
					const cb = this.pending[key]
					if (cb) {
						delete this.pending[key]
						cb(m)
					}
					break
				}
				case 'find':
					this.findMiss = !m.found
					break
				case 'quality':
					this.slow = !!m.slow
					break
				case 'dialog':
					this.dialog = m
					this.dialogText = m.default || ''
					this.dialogUser = ''
					this.dialogPass = ''
					this.$nextTick(() => {
						const el = this.$refs.dialogInput || this.$refs.dialogOk
						if (el) el.focus()
					})
					break
				case 'filechooser':
					this.chooser = { ...m, progress: null }
					break
				case 'download':
					this.onDownload(m)
					break
				case 'crashed':
					break
				case 'notice':
					this.toast(m.text, m.level === 'error' ? 'is-danger' : m.level === 'warning' ? 'is-warning' : 'is-info')
					break
				case 'fatal':
					this.fatal = m
					if (m.reason === 'crashed' || m.reason === 'sandbox' || m.reason === 'no_chrome') this.$emit('fallback', m.reason)
					break
			}
		},
		request(msg, key) {
			const seq = key || ++this.seq
			return new Promise(resolve => {
				this.pending[seq] = resolve
				if (!this.send({ ...msg, seq })) {
					delete this.pending[seq]
					resolve(null)
				}
				setTimeout(() => {
					if (this.pending[seq] === resolve) {
						delete this.pending[seq]
						resolve(null)
					}
				}, 10000)
			})
		},

		// ---- public (used by the app shell) ----
		openUrl(url, inNewTab) {
			if (this.state !== 'open') {
				this.pendingOpen = { url, newTab: inNewTab }
				return
			}
			if (!url) {
				this.newTab()
				return
			}
			if (inNewTab && !(this.active && this.blank)) this.newTab(url)
			else this.navigate(url)
		},

		// ---- tabs ----
		activate(id) {
			if (id === this.activeId) return
			this.activeId = id
			this.send({ t: 'tab.activate', tab: id })
		},
		newTab(url, background) {
			if (this.tabs.length >= this.maxTabs) {
				this.toast(this.$t('You can have at most {n} tabs open - close one first', { n: this.maxTabs }), 'is-warning')
				return
			}
			const u = url ? normalizeAddress(url) : 'about:blank'
			this.send({ t: 'tab.new', url: u, typed: !!url, background: !!background, tab: url ? this.activeId : 0 })
		},
		closeTab(id) {
			this.send({ t: 'tab.close', tab: id })
		},
		cycleTab(dir) {
			const i = this.tabs.findIndex(t => t.id === this.activeId)
			if (i < 0 || this.tabs.length < 2) return
			this.activate(this.tabs[(i + dir + this.tabs.length) % this.tabs.length].id)
		},
		onDragStart(t, e) {
			this.dragId = t.id
			e.dataTransfer.effectAllowed = 'move'
			try {
				e.dataTransfer.setData('text/plain', t.url)
			} catch (err) {}
		},
		onDrop(index) {
			if (this.dragId) this.send({ t: 'tab.move', tab: this.dragId, index })
			this.onDragEnd()
		},
		onDragEnd() {
			this.dragId = 0
			this.dropIndex = -1
		},
		openTabMenu(t, e) {
			this.showMenu(e.clientX, e.clientY, [
				[
					{ id: 'reload', label: 'Reload', icon: 'refresh' },
					{ id: 'duplicate', label: 'Duplicate', icon: 'content-duplicate' },
					{ id: 'copy', label: 'Copy address', icon: 'content-copy', disabled: !this.isWebUrl(t.url) }
				],
				[
					{ id: 'close', label: 'Close tab', icon: 'close', hint: 'Alt+W' },
					{ id: 'others', label: 'Close other tabs', icon: 'tab-remove', disabled: this.tabs.length < 2 },
					{ id: 'reopen', label: 'Reopen closed tab', icon: 'tab-unselected', hint: 'Ctrl+Shift+T' }
				]
			], item => {
				if (item.id === 'reload') this.send({ t: 'reload', tab: t.id })
				else if (item.id === 'duplicate') this.send({ t: 'tab.duplicate', tab: t.id })
				else if (item.id === 'copy') this.copy(t.url, this.$t('Address copied'))
				else if (item.id === 'close') this.closeTab(t.id)
				else if (item.id === 'others') this.tabs.filter(x => x.id !== t.id).forEach(x => this.closeTab(x.id))
				else if (item.id === 'reopen') this.send({ t: 'tab.reopen' })
			})
		},

		// ---- navigation ----
		navigate(raw) {
			const url = normalizeAddress(raw)
			if (!url) return
			if (!this.active) {
				this.newTab(url)
				return
			}
			this.historyOpen = false
			this.send({ t: 'nav', tab: this.activeId, url, typed: true })
			this.pendingNav = { tab: this.activeId, url, at: Date.now() }
			this.addressText = url
			this.$nextTick(() => this.focusPage())
		},
		go(dir) {
			if (this.active) this.send({ t: dir, tab: this.activeId })
		},
		// A long press on Back/Forward opened the history list - that press
		// must not also navigate.
		onNavClick(dir) {
			if (this.suppressClick) {
				this.suppressClick = false
				return
			}
			this.go(dir)
		},
		reload(hard) {
			if (this.active) this.send({ t: 'reload', tab: this.activeId, hard: !!hard })
		},
		startLongPress(dir, e) {
			this.cancelLongPress()
			const { clientX, clientY } = e
			this.longPressTimer = setTimeout(() => {
				this.longPressTimer = null
				this.suppressClick = true
				this.openHistoryMenu(dir, { clientX, clientY })
			}, 500)
		},
		cancelLongPress() {
			clearTimeout(this.longPressTimer)
			this.longPressTimer = null
		},
		async openHistoryMenu(dir, e) {
			if (!this.active) return
			const res = await this.request({ t: 'history.get', tab: this.activeId }, 'history')
			if (!res || !res.entries) return
			const idx = res.index
			const list = dir === 'back' ? res.entries.slice(0, idx).reverse() : res.entries.slice(idx + 1)
			if (!list.length) return
			this.showMenu(e.clientX, e.clientY + 12, [list.slice(0, 15).map(en => ({ id: en.id, label: en.title || en.url, icon: 'history' }))], item => {
				this.send({ t: 'history.go', tab: this.activeId, entry: item.id })
			}, this.$t('History'))
		},

		// ---- address bar ----
		syncAddress() {
			if (this.editing) return
			// While a typed address is loading, keep showing it (the tab's
			// own address only changes once the page commits).
			const p = this.pendingNav
			if (p && this.active && p.tab === this.activeId && this.active.url !== p.url && (this.active.loading || Date.now() - p.at < 4000)) {
				this.addressText = p.url
				return
			}
			this.pendingNav = null
			this.addressText = this.active && !this.blank ? this.active.url : ''
		},
		focusAddress() {
			const el = this.$refs.address
			if (el) {
				el.focus()
				el.select()
			}
		},
		onAddressFocus(e) {
			this.editing = true
			this.suggestIndex = -1
			this.$nextTick(() => e.target.select())
		},
		onAddressBlur() {
			this.editing = false
			this.suggestions = []
			this.syncAddress()
		},
		onAddressInput() {
			clearTimeout(this.suggestTimer)
			const q = this.addressText.trim()
			if (!q) {
				this.suggestions = []
				return
			}
			this.suggestTimer = setTimeout(async () => {
				let hist = []
				try {
					hist = (await downloadSidecar.listHistory(q, 6)) || []
				} catch (e) {}
				if (this.addressText.trim() !== q) return
				const items = hist.map(h => ({ id: h.id, url: h.url, title: h.title }))
				if (!/^https?:\/\//i.test(q)) items.unshift({ search: true, url: normalizeAddress(q), title: this.$t('Search DuckDuckGo for "{q}"', { q }) })
				this.suggestions = items.slice(0, 7)
				this.suggestIndex = -1
			}, 120)
		},
		pickSuggestion(s, e) {
			this.suggestions = []
			if (e && (e.altKey || e.button === 1)) this.newTab(s.url)
			else this.navigate(s.url)
			if (this.$refs.address) this.$refs.address.blur()
		},
		onAddressKey(e) {
			if (e.key === 'ArrowDown' && this.suggestions.length) {
				e.preventDefault()
				this.suggestIndex = (this.suggestIndex + 1) % this.suggestions.length
			} else if (e.key === 'ArrowUp' && this.suggestions.length) {
				e.preventDefault()
				this.suggestIndex = this.suggestIndex <= 0 ? this.suggestions.length - 1 : this.suggestIndex - 1
			} else if (e.key === 'Enter') {
				e.preventDefault()
				const s = this.suggestIndex >= 0 ? this.suggestions[this.suggestIndex] : null
				const target = s ? s.url : this.addressText
				this.suggestions = []
				if (e.altKey) this.newTab(target)
				else this.navigate(target)
				this.$refs.address.blur()
			} else if (e.key === 'Escape') {
				e.preventDefault()
				this.$refs.address.blur()
				this.focusPage()
			}
		},
		async copyAddress() {
			if (!this.active) return
			if (await copyText(this.active.url)) {
				this.copied = true
				clearTimeout(this.copiedTimer)
				this.copiedTimer = setTimeout(() => (this.copied = false), 1600)
			} else {
				this.toast(this.$t('Could not copy to the clipboard'), 'is-danger')
			}
		},
		submitStart() {
			const t = this.startText
			this.startText = ''
			this.navigate(t)
		},
		async loadRecent() {
			try {
				const list = (await downloadSidecar.listHistory('', 40)) || []
				const seen = new Set()
				this.recent = list
					.filter(e => {
						const h = hostOf(e.url)
						if (!h || seen.has(h)) return false
						seen.add(h)
						return true
					})
					.slice(0, 8)
			} catch (e) {
				this.recent = []
			}
		},
		maybeRecordHistory() {
			const t = this.active
			if (!t || t.loading || !this.isWebUrl(t.url)) return
			const key = t.url + '\n' + (t.title || '')
			if (this.recorded[t.id] === key) return
			this.recorded[t.id] = key
			downloadSidecar.addHistory(t.url, t.title || '').catch(() => {})
		},

		// ---- page ----
		focusPage() {
			if (this.$refs.viewport && !this.dialog && !this.chooser && !this.editing) this.$refs.viewport.focus()
		},
		onViewportResize(s) {
			this.send({ t: 'resize', ...s })
		},
		onShortcut(name) {
			switch (name) {
				case 'focusUrl':
					this.focusAddress()
					break
				case 'reload':
					this.reload(false)
					break
				case 'hardReload':
					this.reload(true)
					break
				case 'back':
					this.go('back')
					break
				case 'forward':
					this.go('fwd')
					break
				case 'newTab':
					this.newTab()
					break
				case 'closeTab':
					if (this.active) this.closeTab(this.activeId)
					break
				case 'reopenTab':
					this.send({ t: 'tab.reopen' })
					break
				case 'nextTab':
					this.cycleTab(1)
					break
				case 'prevTab':
					this.cycleTab(-1)
					break
				case 'find':
					this.openFind()
					break
				case 'copy':
					this.copySelection()
					break
				case 'cut':
					this.copySelection(true)
					break
				case 'zoomIn':
					this.zoom(1.1)
					break
				case 'zoomOut':
					this.zoom(0.9)
					break
				case 'zoomReset':
					this.zoom(0)
					break
				case 'contextMenu': {
					const r = this.$el.querySelector('.frame-area').getBoundingClientRect()
					this.onPageContextMenu({ x: 40, y: 40, clientX: r.left + 40, clientY: r.top + 40 })
					break
				}
			}
		},
		onGlobalKey(e) {
			// Esc stops a loading page (when the page itself has focus the key
			// still goes to it too, like in any browser).
			if (e.key === 'Escape' && this.active && this.active.loading && !this.editing && !this.dialog) this.send({ t: 'stop', tab: this.activeId })
		},
		zoom(factor) {
			if (this.active) this.send({ t: 'zoom', tab: this.activeId, factor })
		},
		async copySelection(cut) {
			const res = await this.request({ t: 'act', action: cut ? 'cut' : 'copy' })
			if (res && res.text) {
				if (!(await copyText(res.text))) this.toast(this.$t('Could not copy to the clipboard'), 'is-danger')
			}
			this.focusPage()
		},
		openFind() {
			this.findOpen = true
			this.findMiss = false
			this.$nextTick(() => {
				if (this.$refs.find) {
					this.$refs.find.focus()
					this.$refs.find.select()
				}
			})
		},
		closeFind() {
			this.findOpen = false
			this.focusPage()
		},
		find(back) {
			if (this.findText) this.send({ t: 'find', tab: this.activeId, text: this.findText, dir: back ? 'back' : 'fwd' })
		},

		// ---- right-click menu ----
		showMenu(x, y, sections, onSelect, title) {
			this.menu = { x, y, sections, title: title || '', onSelect: item => onSelect(item) }
		},
		closeMenu() {
			this.menu = null
			this.$nextTick(() => this.focusPage())
		},
		async onPageContextMenu(p) {
			const hit = (await this.request({ t: 'hit', x: p.x, y: p.y })) || {}
			const sections = buildContextMenu(hit, this.active || {})
			this.showMenu(p.clientX, p.clientY, sections, item => this.runMenuAction(item.id, hit))
		},
		runMenuAction(id, hit) {
			const page = this.active ? this.active.url : ''
			switch (id) {
				case 'openLinkNewTab':
					return this.send({ t: 'tab.new', url: hit.link, tab: this.activeId })
				case 'openLinkBackground':
					return this.send({ t: 'tab.new', url: hit.link, tab: this.activeId, background: true })
				case 'copyLink':
					return this.copy(hit.link, this.$t('Link copied'))
				case 'copyLinkText':
					return this.copy(hit.linkText, this.$t('Link text copied'))
				case 'downloadLink':
					return this.captureAndAdd(hit.link)
				case 'openImageNewTab':
					return this.send({ t: 'tab.new', url: hit.image, tab: this.activeId })
				case 'copyImage':
					return this.send({ t: 'act', action: 'copyImage', text: JSON.stringify(hit.imageRect || []) })
				case 'copyImageAddress':
					return this.copy(hit.image, this.$t('Image address copied'))
				case 'saveImage':
					return this.captureAndAdd(hit.image)
				case 'copyMediaAddress':
					return this.copy(hit.media, this.$t('Address copied'))
				case 'downloadMedia':
					return this.captureAndAdd(hit.media)
				case 'undo':
				case 'redo':
				case 'selectAll':
					return this.send({ t: 'act', action: id })
				case 'cut':
					return this.copySelection(true)
				case 'copy':
					return hit.selection ? this.copy(hit.selection) : this.copySelection()
				case 'paste':
					return this.pasteFromClipboard()
				case 'openSelection':
					return this.newTab(hit.selection.trim())
				case 'searchGoogle':
					return this.send({ t: 'tab.new', url: SEARCH_ENGINES.google.url(hit.selection.trim()), tab: this.activeId })
				case 'searchDuck':
					return this.send({ t: 'tab.new', url: SEARCH_ENGINES.duckduckgo.url(hit.selection.trim()), tab: this.activeId })
				case 'back':
					return this.go('back')
				case 'forward':
					return this.go('fwd')
				case 'reload':
					return this.reload(false)
				case 'copyPageAddress':
					return this.copy(page, this.$t('Address copied'))
				case 'savePageLink':
					return this.captureAndAdd(page)
				case 'openRealTab':
					return this.openRealTab()
				case 'viewSource':
					return this.viewSource()
			}
		},
		async pasteFromClipboard() {
			let text = ''
			try {
				if (navigator.clipboard && navigator.clipboard.readText) text = await navigator.clipboard.readText()
			} catch (e) {}
			if (text) this.send({ t: 'paste', text })
			else this.toast(this.$t('Press Ctrl+V to paste into the page'), 'is-info')
			this.focusPage()
		},
		async copy(text, done) {
			if (!text) return
			if (await copyText(text)) {
				if (done) this.toast(done, 'is-success')
			} else {
				this.toast(this.$t('Could not copy to the clipboard'), 'is-danger')
			}
		},
		async writeClipImage(png) {
			try {
				if (!window.ClipboardItem || !navigator.clipboard || !navigator.clipboard.write) throw new Error('unsupported')
				await navigator.clipboard.write([new window.ClipboardItem({ 'image/png': new Blob([png], { type: 'image/png' }) })])
				this.toast(this.$t('Image copied'), 'is-success')
			} catch (e) {
				this.toast(this.$t('Your browser only allows copying images when NivaroOS is opened over HTTPS - use "Save image" instead.'), 'is-warning')
			}
		},
		openRealTab() {
			if (this.active && this.isWebUrl(this.active.url)) window.open(this.active.url, '_blank', 'noopener,noreferrer')
		},
		async viewSource() {
			const res = await this.request({ t: 'act', action: 'source' })
			if (res) {
				this.source = { url: res.url, text: res.text }
				this.$nextTick(() => this.$refs.sourceText && this.$refs.sourceText.focus())
			}
		},

		// ---- downloads ----
		// Hands a link to Download Station with this tab's cookies: the
		// browser records them and the Add Download window refers to them by
		// id only.
		async captureAndAdd(url) {
			if (!this.isWebUrl(url)) return
			const res = await this.request({ t: 'act', action: 'capture', url })
			if (res && res.id) this.ds.openAddDownload({ url: res.url, rbCapture: res.id, suggestedName: res.filename || '' })
			else this.ds.openAddDownload({ url })
		},
		async onDownload(m) {
			if (m.captured) {
				this.ds.openAddDownload({ url: m.url, rbCapture: m.captured, suggestedName: m.filename || '' })
				return
			}
			if (m.staged) {
				try {
					const res = await downloadSidecar.rbSaveStaged(m.staged)
					this.toast(this.$t('Saved {name} to {path}', { name: m.filename, path: res.path.replace(/\/[^/]*$/, '') }), 'is-success')
				} catch (e) {
					this.toast(this.$t('Could not save {name}: {error}', { name: m.filename, error: e.message }), 'is-danger')
				}
			}
		},

		// ---- dialogs & uploads ----
		answerDialog(accept) {
			if (!this.dialog) return
			this.send({ t: 'dialog', id: this.dialog.id, accept, text: this.dialogText, user: this.dialogUser, pass: this.dialogPass })
			this.dialog = null
			this.dialogPass = ''
			this.focusPage()
		},
		cancelChooser() {
			if (this.chooser) this.send({ t: 'filecancel', id: this.chooser.id })
			this.chooser = null
			this.focusPage()
		},
		async uploadChosen(e) {
			const files = Array.from(e.target.files || [])
			if (!files.length || !this.chooser) return
			const ch = this.chooser
			ch.progress = 0
			try {
				await downloadSidecar.rbUpload(ch.id, files, p => (ch.progress = p))
				this.chooser = null
			} catch (err) {
				ch.progress = null
				this.toast(this.$t('Upload failed: {error}', { error: err.message }), 'is-danger')
			}
			this.focusPage()
		},

		// ---- menus in the bar ----
		openShieldMenu(e) {
			const host = this.activeHost
			const r = e.currentTarget.getBoundingClientRect()
			const sections = [
				[{ id: 'toggle', label: this.adblockEnabled ? 'Turn the ad blocker off' : 'Turn the ad blocker on', icon: this.adblockEnabled ? 'shield-off-outline' : 'shield-check-outline' }]
			]
			if (host && this.adblockEnabled) {
				sections.push([{ id: 'trust', label: this.siteTrusted ? `Block ads on ${host} again` : `Don't block anything on ${host}`, icon: 'web', checked: false }])
			}
			sections.push([{ id: 'settings', label: 'Ad blocker settings', icon: 'cog-outline' }])
			const blocked = this.active ? this.active.blocked : 0
			const title = this.adblockEnabled && !this.siteTrusted ? this.$t('{n} blocked on this page', { n: blocked }) : ''
			this.showMenu(r.right - 240, r.bottom + 4, sections, item => {
				if (item.id === 'toggle') this.$emit('toggle-adblock')
				else if (item.id === 'trust') this.$emit('trust-site', host, !this.siteTrusted)
				else if (item.id === 'settings') this.$emit('open-adblock')
			}, title)
		},
		openMoreMenu(e) {
			const r = e.currentTarget.getBoundingClientRect()
			const web = this.active && this.isWebUrl(this.active.url)
			const zoom = this.active ? Math.round((this.active.zoom || 1) * 100) : 100
			this.showMenu(r.right - 260, r.bottom + 4, [
				[
					{ id: 'newTab', label: 'New tab', icon: 'tab-plus', hint: 'Alt+T' },
					{ id: 'reopen', label: 'Reopen closed tab', icon: 'tab-unselected', hint: 'Ctrl+Shift+T' },
					{ id: 'history', label: 'History', icon: 'history' }
				],
				[
					{ id: 'find', label: 'Find in page', icon: 'text-search', hint: 'Ctrl+F', disabled: !web },
					{ id: 'zoomOut', label: 'Zoom out', icon: 'magnify-minus-outline', hint: 'Ctrl+−', disabled: !web },
					{ id: 'zoomReset', label: `Zoom ${zoom}% — reset`, icon: 'magnify', hint: 'Ctrl+0', disabled: !web || zoom === 100 },
					{ id: 'zoomIn', label: 'Zoom in', icon: 'magnify-plus-outline', hint: 'Ctrl++', disabled: !web }
				],
				[
					{ id: 'copyAddress', label: 'Copy page address', icon: 'content-copy', disabled: !web },
					{ id: 'openReal', label: 'Open page in a real browser tab', icon: 'open-in-new', disabled: !web },
					{ id: 'source', label: 'View page source', icon: 'code-tags', disabled: !web }
				],
				[
					{ id: 'clear', label: 'Clear browsing data & sign out of sites', icon: 'cookie-remove-outline' },
					{ id: 'lite', label: 'Switch to Lite mode', icon: 'feather' }
				]
			], item => {
				switch (item.id) {
					case 'newTab':
						return this.newTab()
					case 'reopen':
						return this.send({ t: 'tab.reopen' })
					case 'history':
						this.historyOpen = true
						return
					case 'find':
						return this.openFind()
					case 'zoomIn':
						return this.zoom(1.1)
					case 'zoomOut':
						return this.zoom(0.9)
					case 'zoomReset':
						return this.zoom(0)
					case 'copyAddress':
						return this.copyAddress()
					case 'openReal':
						return this.openRealTab()
					case 'source':
						return this.viewSource()
					case 'clear':
						return this.clearProfile()
					case 'lite':
						return this.$emit('fallback', 'user')
				}
			})
		},
		clearProfile() {
			this.$buefy.dialog.confirm({
				title: this.$t('Clear browsing data?'),
				message: escapeHtml(this.$t('This signs you out of every site in the Download Station browser and deletes its cookies, cache and open tabs.')),
				confirmText: this.$t('Clear and sign out'),
				cancelText: this.$t('Cancel'),
				type: 'is-danger',
				onConfirm: async () => {
					try {
						this.stop()
						await downloadSidecar.rbClearProfile()
						this.toast(this.$t('Browsing data cleared'), 'is-success')
					} catch (e) {
						this.toast(this.$t('Could not clear browsing data: {error}', { error: e.message }), 'is-danger')
					}
					this.restart()
				}
			})
		},

		// Toast messages are HTML - always escape.
		toast(text, type) {
			this.$buefy.toast.open({ message: escapeHtml(text || ''), type: type || 'is-info' })
		}
	}
}
</script>

<style lang="scss" scoped>
@import '../ds-common.scss';

.rb-browser {
	display: flex;
	flex-direction: column;
	height: 100%;
	min-height: 0;
	background: var(--theme-card-bg, #fff);
}

// ---- tabs ----
.tab-strip {
	flex-shrink: 0;
	display: flex;
	align-items: flex-end;
	gap: 2px;
	padding: var(--space-2) var(--space-2) 0;
	background: var(--theme-card-subtle, #f1f5f9);
	border-bottom: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.08));
	overflow-x: auto;
	scrollbar-width: none;
	user-select: none;
}

.tab {
	position: relative;
	flex: 0 1 13rem;
	min-width: 5.5rem;
	display: flex;
	align-items: center;
	gap: var(--space-1);
	height: 2rem;
	padding: 0 var(--space-1) 0 var(--space-2);
	border-radius: var(--radius-sm) var(--radius-sm) 0 0;
	font-size: var(--font-xs);
	color: var(--theme-text-secondary, #475569);
	cursor: pointer;

	&:hover {
		background: var(--theme-card-hover, rgba(0, 0, 0, 0.04));
	}

	&.active {
		background: var(--theme-card-bg, #fff);
		color: var(--theme-text-primary, #1e293b);
		box-shadow: 0 -1px 0 var(--theme-card-border, rgba(0, 0, 0, 0.08)), 1px 0 0 var(--theme-card-border, rgba(0, 0, 0, 0.08)), -1px 0 0 var(--theme-card-border, rgba(0, 0, 0, 0.08));
	}

	&.dragging {
		opacity: 0.5;
	}

	&.drop-before::before {
		content: '';
		position: absolute;
		left: -2px;
		top: 4px;
		bottom: 4px;
		width: 2px;
		border-radius: 1px;
		background: var(--color-primary, #2563eb);
	}
}

.tab-icon {
	flex-shrink: 0;
	display: inline-flex;
	align-items: center;
	justify-content: center;
	width: 1rem;
	height: 1rem;
	color: var(--theme-text-muted, #94a3b8);

	img {
		width: 16px;
		height: 16px;
		object-fit: contain;
	}
}

.tab-title {
	flex: 1 1 auto;
	min-width: 0;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}

.tab-close {
	flex-shrink: 0;
	display: inline-flex;
	align-items: center;
	justify-content: center;
	width: 1.25rem;
	height: 1.25rem;
	border: none;
	background: transparent;
	border-radius: var(--radius-xs);
	cursor: pointer;
	padding: 0;
	color: inherit;
	opacity: 0.6;

	&:hover {
		background: var(--theme-card-hover, rgba(0, 0, 0, 0.08));
		opacity: 1;
	}
}

.tab-new {
	flex-shrink: 0;
	display: inline-flex;
	align-items: center;
	justify-content: center;
	width: 1.75rem;
	height: 1.75rem;
	margin: 0 0 0.15rem var(--space-1);
	border: none;
	background: transparent;
	border-radius: var(--radius-sm);
	color: var(--theme-text-secondary, #475569);
	cursor: pointer;

	&:hover:not(:disabled) {
		background: var(--theme-card-hover, rgba(0, 0, 0, 0.06));
	}

	&:disabled {
		opacity: 0.35;
		cursor: default;
	}
}

// ---- address bar ----
.address-bar {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	gap: 2px;
	padding: var(--space-2);
	border-bottom: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.08));
	background: var(--theme-card-bg, #fff);
}

.url-field {
	position: relative;
	flex: 1 1 auto;
	min-width: 6rem;
	height: 2rem;
	margin: 0 var(--space-2);
	border-radius: var(--radius-pill);
	background: var(--theme-card-subtle, #f1f5f9);
	border: 1px solid transparent;

	&.focused {
		background: var(--theme-input-bg, #fff);
		border-color: var(--theme-input-focus, #2563eb);
		box-shadow: 0 0 0 3px var(--theme-focus-ring, rgba(37, 99, 235, 0.4));
	}
}

.url-icon {
	position: absolute;
	left: 0.65rem;
	top: 50%;
	transform: translateY(-50%);
	color: var(--theme-text-muted, #94a3b8);
	pointer-events: none;

	.secure & {
		color: var(--color-success-fg, #047857);
	}
}

.url-input,
.url-display {
	position: absolute;
	inset: 0;
	width: 100%;
	height: 100%;
	padding: 0 2.4rem 0 2rem;
	border: none;
	border-radius: inherit;
	background: transparent;
	color: var(--theme-text-primary, #1e293b);
	font-family: inherit;
	font-size: var(--font-sm);
	line-height: calc(2rem - 2px);
	outline: none;
}

.url-input.is-masked {
	opacity: 0;
}

.url-display {
	overflow: hidden;
	white-space: nowrap;
	text-overflow: ellipsis;
	cursor: text;

	.host {
		color: var(--theme-text-primary, #0f172a);
	}

	.dim {
		color: var(--theme-text-muted, #64748b);
	}
}

.url-copy {
	position: absolute;
	right: 0.25rem;
	top: 50%;
	transform: translateY(-50%);
	display: inline-flex;
	align-items: center;
	gap: 0.2rem;
	height: 1.55rem;
	min-width: 1.55rem;
	padding: 0 0.3rem;
	border: none;
	border-radius: var(--radius-pill);
	background: transparent;
	color: var(--theme-text-muted, #64748b);
	cursor: pointer;
	font-size: var(--font-2xs);

	&:hover {
		background: var(--theme-card-hover, rgba(0, 0, 0, 0.06));
		color: var(--theme-text-primary, #0f172a);
	}

	&.done {
		color: var(--color-success-fg, #047857);
		background: var(--color-success-soft, rgba(16, 185, 129, 0.12));
	}
}

.url-progress {
	position: absolute;
	left: 1rem;
	right: 1rem;
	bottom: -1px;
	height: 2px;
	border-radius: 1px;
	background: var(--color-primary, #2563eb);
	transform-origin: left center;
	transition: transform 0.25s ease;
	pointer-events: none;
}

.url-suggest {
	position: absolute;
	left: 0;
	right: 0;
	top: calc(100% + 6px);
	z-index: 40;
	margin: 0;
	padding: var(--space-1) 0;
	list-style: none;
	border-radius: var(--radius-md, 10px);
	border: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.08));
	background: var(--theme-card-bg, #fff);
	box-shadow: 0 12px 28px rgba(15, 23, 42, 0.16);

	li {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		height: 2rem;
		padding: 0 var(--space-3);
		font-size: var(--font-sm);
		cursor: pointer;
		color: var(--theme-text-primary, #1e293b);

		&.active,
		&:hover {
			background: var(--theme-card-hover, rgba(0, 0, 0, 0.05));
		}

		::v-deep .icon {
			flex-shrink: 0;
			color: var(--theme-text-muted, #94a3b8);
		}
	}

	.s-title {
		flex: 0 1 auto;
		min-width: 0;
	}

	.s-url {
		flex: 1 1 0;
		min-width: 0;
		font-size: var(--font-xs);
		color: var(--theme-text-muted, #64748b);
	}
}

.shield-btn {
	position: relative;
	width: auto;
	min-width: 1.85rem;
	padding: 0 0.35rem;
	gap: 0.15rem;
}

.shield-count {
	font-size: var(--font-2xs);
	font-weight: 600;
	font-variant-numeric: tabular-nums;
}

// ---- page area ----
.frame-area {
	position: relative;
	flex: 1 1 auto;
	min-height: 0;
	background: var(--theme-bg-window, #f8fafc);
}

// Kept laid out (not display:none) while connecting, so its size is right
// in the first hello.
.is-offstage {
	visibility: hidden;
}

.rb-badge {
	position: absolute;
	left: var(--space-2);
	bottom: var(--space-2);
	display: inline-flex;
	align-items: center;
	gap: var(--space-1);
	padding: 0.2rem 0.55rem;
	border-radius: var(--radius-pill);
	background: rgba(15, 23, 42, 0.72);
	color: #fff;
	font-size: var(--font-2xs);
	pointer-events: none;
}

.find-bar {
	position: absolute;
	top: var(--space-2);
	right: var(--space-3);
	z-index: 5;
	display: flex;
	align-items: center;
	gap: 2px;
	padding: var(--space-1);
	border-radius: var(--radius-md, 10px);
	border: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.08));
	background: var(--theme-card-bg, #fff);
	box-shadow: 0 8px 24px rgba(15, 23, 42, 0.16);

	.ds-input {
		width: 14rem;
		height: 1.85rem;

		&.miss {
			border-color: var(--color-danger, #dc2626);
		}
	}
}

.find-miss {
	padding: 0 var(--space-2);
	font-size: var(--font-xs);
	color: var(--color-danger-fg, #b91c1c);
	white-space: nowrap;
}

.rb-panel {
	position: absolute;
	inset: 0;
	z-index: 6;
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	padding: var(--space-6);
	text-align: center;
	background: var(--theme-bg-window, #f8fafc);
	color: var(--theme-text-muted, #94a3b8);

	p {
		max-width: 30rem;
		font-size: var(--font-sm);
		color: var(--theme-text-secondary, #64748b);
		margin: var(--space-2) 0 var(--space-4);
		line-height: 1.5;
	}

	.panel-title {
		font-size: var(--font-md);
		font-weight: 600;
		color: var(--theme-text-primary, #1e293b);
		margin-bottom: 0;
	}
}

.panel-actions {
	display: flex;
	gap: var(--space-2);
}

.rb-starting .skeleton-bar {
	width: min(24rem, 70%);
	height: 0.5rem;
	border-radius: var(--radius-pill);
	background: linear-gradient(90deg, var(--theme-card-hover, #e2e8f0) 0%, var(--theme-card-subtle, #f1f5f9) 50%, var(--theme-card-hover, #e2e8f0) 100%);
	background-size: 200% 100%;
	animation: rb-shimmer 1.2s linear infinite;
}

@keyframes rb-shimmer {
	to {
		background-position: -200% 0;
	}
}

@media (prefers-reduced-motion: reduce) {
	.rb-starting .skeleton-bar {
		animation: none;
	}
}

.rb-reconnect {
	position: absolute;
	top: var(--space-2);
	left: 50%;
	transform: translateX(-50%);
	z-index: 7;
	display: inline-flex;
	align-items: center;
	gap: var(--space-2);
	padding: 0.3rem 0.8rem;
	border-radius: var(--radius-pill);
	background: rgba(15, 23, 42, 0.8);
	color: #fff;
	font-size: var(--font-xs);
}

// ---- start page ----
.rb-start {
	position: absolute;
	inset: 0;
	z-index: 4;
	display: flex;
	justify-content: center;
	overflow-y: auto;
	background: var(--theme-bg-window, #f8fafc);
}

.start-inner {
	width: min(40rem, 100%);
	padding: 12vh var(--space-5) var(--space-6);
	display: flex;
	flex-direction: column;
	align-items: center;
	gap: var(--space-4);
}

.start-logo {
	color: var(--color-primary, #2563eb);
	opacity: 0.85;
}

.start-form {
	position: relative;
	width: 100%;
}

.start-icon {
	position: absolute;
	left: 1rem;
	top: 50%;
	transform: translateY(-50%);
	color: var(--theme-text-muted, #94a3b8);
	pointer-events: none;
}

.start-input {
	width: 100%;
	height: 3rem;
	padding: 0 var(--space-4) 0 3rem;
	border-radius: var(--radius-pill);
	border: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.1));
	background: var(--theme-input-bg, #fff);
	color: var(--theme-text-primary, #1e293b);
	font-family: inherit;
	font-size: var(--font-md);
	outline: none;
	box-shadow: 0 2px 10px rgba(15, 23, 42, 0.06);

	&:focus {
		border-color: var(--theme-input-focus, #2563eb);
		box-shadow: 0 0 0 3px var(--theme-focus-ring, rgba(37, 99, 235, 0.35));
	}
}

.start-hint {
	max-width: 32rem;
	margin: 0;
	text-align: center;
	font-size: var(--font-sm);
	line-height: 1.5;
	color: var(--theme-text-secondary, #64748b);
}

.start-recent {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(9rem, 1fr));
	gap: var(--space-2);
	width: 100%;
	margin-top: var(--space-2);
}

.recent-tile {
	display: flex;
	flex-direction: column;
	gap: 2px;
	padding: var(--space-2) var(--space-3);
	border-radius: var(--radius-sm);
	border: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.08));
	background: var(--theme-card-bg, #fff);
	text-align: left;
	cursor: pointer;
	font-family: inherit;
	min-width: 0;

	&:hover {
		border-color: var(--theme-input-border, #94a3b8);
	}
}

.recent-host {
	font-size: var(--font-sm);
	font-weight: 500;
	color: var(--theme-text-primary, #1e293b);
}

.recent-title {
	font-size: var(--font-xs);
	color: var(--theme-text-muted, #64748b);
}

// ---- overlays ----
.rb-history {
	position: absolute;
	inset: 0;
	z-index: 8;
	background: var(--theme-bg-window, #f8fafc);
}

.history-close {
	position: absolute;
	top: var(--space-3);
	right: var(--space-3);
	z-index: 9;
}

.rb-modal {
	position: absolute;
	inset: 0;
	z-index: 10;
	display: flex;
	align-items: flex-start;
	justify-content: center;
	padding: 8vh var(--space-4) var(--space-4);
	background: rgba(15, 23, 42, 0.35);
}

.rb-dialog {
	display: flex;
	flex-direction: column;
	gap: var(--space-3);
	width: min(28rem, 100%);
	padding: var(--space-5);
	border-radius: var(--radius-md, 12px);
	background: var(--theme-card-bg, #fff);
	color: var(--theme-text-primary, #1e293b);
	box-shadow: 0 20px 48px rgba(15, 23, 42, 0.28);
}

.dialog-origin {
	margin: 0;
	font-size: var(--font-sm);
	font-weight: 600;
}

.dialog-message {
	margin: 0;
	font-size: var(--font-sm);
	line-height: 1.5;
	white-space: pre-wrap;
	word-break: break-word;
	max-height: 40vh;
	overflow-y: auto;
}

.dialog-note {
	margin: 0;
	font-size: var(--font-xs);
	color: var(--theme-text-muted, #64748b);
}

.dialog-actions {
	display: flex;
	justify-content: flex-end;
	gap: var(--space-2);
	margin-top: var(--space-1);
}

.is-hidden-input {
	display: none;
}

.rb-source {
	display: flex;
	flex-direction: column;
	width: min(60rem, 100%);
	height: 80%;
	border-radius: var(--radius-md, 12px);
	background: var(--theme-card-bg, #fff);
	box-shadow: 0 20px 48px rgba(15, 23, 42, 0.28);
	overflow: hidden;
}

.source-head {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	padding: var(--space-2) var(--space-3);
	border-bottom: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.08));
	font-size: var(--font-xs);

	.one-line {
		flex: 1 1 auto;
		min-width: 0;
	}
}

.source-text {
	flex: 1 1 auto;
	width: 100%;
	padding: var(--space-3);
	border: none;
	resize: none;
	outline: none;
	background: var(--theme-card-subtle, #f8fafc);
	color: var(--theme-text-primary, #1e293b);
	font-family: $family-monospace;
	font-size: var(--font-xs);
	line-height: 1.5;
	white-space: pre;
}

.one-line {
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}

.mono {
	font-family: $family-monospace;
}
</style>
