<template>
	<div ref="terminalRoot" class="terminal-instance" :class="{ 'has-keybar': showKeyBar }" @contextmenu="onContextMenu">
		<div ref="xtermEl" class="xterm" @mouseup="onMouseUp"></div>

		<!-- Short-lived status: "Copied", "Restoring session...". Top centre,
		     out of the way of the prompt (which is usually at the bottom). -->
		<transition name="term-fade">
			<div v-if="pillText" class="term-pill" role="status" aria-live="polite">
				<i v-if="pillIcon" :class="['mdi', pillIcon]" aria-hidden="true"></i>
				<span>{{ pillText }}</span>
			</div>
		</transition>

		<!-- Find in scrollback (Ctrl+Shift+F). -->
		<div v-if="findOpen" class="term-find" role="search" @keydown.stop>
			<i class="mdi mdi-magnify term-find-icon" aria-hidden="true"></i>
			<input ref="findInput" v-model="findQuery" type="text" class="term-find-input" spellcheck="false" autocomplete="off"
				:placeholder="$t('Find')" :aria-label="$t('Find in terminal')"
				@input="find(1, true)" @keydown.enter.exact.prevent="find(1)" @keydown.shift.enter.prevent="find(-1)"
				@keydown.esc.prevent="closeFind">
			<span class="term-find-count" :class="{ 'is-miss': findQuery && findMiss }">{{ findCountText }}</span>
			<button type="button" class="term-icon-btn" :class="{ 'is-on': findCase }" :aria-pressed="findCase ? 'true' : 'false'"
				:title="$t('Match case')" :aria-label="$t('Match case')" @click="findCase = !findCase; find(1, true)">
				<span class="term-find-case">Aa</span>
			</button>
			<button type="button" class="term-icon-btn" :title="$t('Previous match (Shift+Enter)')" :aria-label="$t('Previous match')" @click="find(-1)">
				<i class="mdi mdi-chevron-up" aria-hidden="true"></i>
			</button>
			<button type="button" class="term-icon-btn" :title="$t('Next match (Enter)')" :aria-label="$t('Next match')" @click="find(1)">
				<i class="mdi mdi-chevron-down" aria-hidden="true"></i>
			</button>
			<button type="button" class="term-icon-btn" :title="$t('Close (Esc)')" :aria-label="$t('Close find')" @click="closeFind">
				<i class="mdi mdi-close" aria-hidden="true"></i>
			</button>
		</div>

		<!-- Connection / session state. Deliberately a small bar, not a
		     replacement for the terminal: the scrollback stays readable. -->
		<div v-if="banner" class="terminal-ended" :class="'is-' + banner.tone" role="status">
			<i :class="['mdi', banner.icon, 'terminal-ended-icon']" aria-hidden="true"></i>
			<span class="terminal-ended-text" :title="banner.text">{{ banner.text }}</span>
			<button v-for="a in banner.actions" :key="a.id" type="button" class="terminal-ended-btn" :class="{ 'is-quiet': a.quiet }"
				:disabled="a.disabled" @click="onBannerAction(a.id)">
				<i v-if="a.icon" :class="['mdi', a.icon, 'mr-1']" aria-hidden="true"></i>{{ a.label }}
			</button>
		</div>

		<!-- Multi-line paste into a shell that would run every line at once. -->
		<div v-if="pasteAsk" class="term-scrim" @keydown.stop @mousedown.self="cancelPaste">
			<div class="term-dialog" role="alertdialog" aria-modal="true" :aria-label="$t('Paste {n} lines?', { n: pasteAsk.count })" @keydown.esc.prevent="cancelPaste">
				<p class="term-dialog-title">{{ $t('Paste {n} lines?', { n: pasteAsk.count }) }}</p>
				<p class="term-dialog-text">{{ $t('This shell runs each line as soon as it is pasted.') }}</p>
				<pre class="term-dialog-preview">{{ pasteAsk.preview.lines.join('\n') }}<template v-if="pasteAsk.preview.more">
{{ $t('... {n} more lines', { n: pasteAsk.preview.more }) }}</template></pre>
				<label class="term-dialog-check">
					<input v-model="pasteAskRemember" type="checkbox">
					<span>{{ $t("Don't ask again") }}</span>
				</label>
				<div class="term-dialog-actions">
					<button type="button" class="terminal-ended-btn is-quiet" @click="cancelPaste">{{ $t('Cancel') }}</button>
					<button ref="pasteConfirmBtn" type="button" class="terminal-ended-btn" @click="confirmPaste">{{ $t('Paste') }}</button>
				</div>
			</div>
		</div>

		<!-- Paste on a phone over plain http: the page can't read the
		     clipboard, and a phone has no Ctrl+Shift+V - so the user pastes
		     into a text box (long-press > Paste) and sends it from there. -->
		<div v-if="pasteSheet" class="term-scrim" @keydown.stop @mousedown.self="closePasteSheet">
			<div class="term-dialog" role="dialog" aria-modal="true" :aria-label="$t('Paste into terminal')" @keydown.esc.prevent="closePasteSheet">
				<p class="term-dialog-title">{{ $t('Paste into terminal') }}</p>
				<p class="term-dialog-text">{{ $t('Long-press the box and choose Paste, then send it.') }}</p>
				<textarea ref="pasteSheetInput" v-model="pasteSheet.text" class="term-paste-input" rows="4" spellcheck="false" autocomplete="off"
					autocapitalize="off" :aria-label="$t('Text to paste')"></textarea>
				<div class="term-dialog-actions">
					<button type="button" class="terminal-ended-btn is-quiet" @click="closePasteSheet">{{ $t('Cancel') }}</button>
					<button type="button" class="terminal-ended-btn" :disabled="!pasteSheet.text" @click="submitPasteSheet">{{ $t('Paste into terminal') }}</button>
				</div>
			</div>
		</div>

		<!-- Right-click menu. Shift+right-click still opens the browser's own. -->
		<div v-if="menu" ref="menu" class="term-menu" role="menu" :aria-label="$t('Terminal menu')" :style="{ left: menu.x + 'px', top: menu.y + 'px' }"
			@keydown.stop="onMenuKey" @mousedown.stop @contextmenu.prevent.stop>
			<button type="button" role="menuitem" class="term-menu-item" :disabled="!menu.hasSelection" @click="menuRun('copy')">
				<i class="mdi mdi-content-copy" aria-hidden="true"></i><span>{{ $t('Copy') }}</span><kbd>{{ keyLabel('copy') }}</kbd>
			</button>
			<button type="button" role="menuitem" class="term-menu-item" :disabled="!canInput" @click="menuRun('paste')">
				<i class="mdi mdi-content-paste" aria-hidden="true"></i><span>{{ $t('Paste') }}</span><kbd>{{ keyLabel('paste') }}</kbd>
			</button>
			<button type="button" role="menuitem" class="term-menu-item" @click="menuRun('selectAll')">
				<i class="mdi mdi-select-all" aria-hidden="true"></i><span>{{ $t('Select all') }}</span><kbd>{{ keyLabel('selectAll') }}</kbd>
			</button>
			<div class="term-menu-sep" role="separator"></div>
			<button type="button" role="menuitem" class="term-menu-item" @click="menuRun('find')">
				<i class="mdi mdi-magnify" aria-hidden="true"></i><span>{{ $t('Find...') }}</span><kbd>{{ keyLabel('find') }}</kbd>
			</button>
			<button type="button" role="menuitem" class="term-menu-item" @click="menuRun('clear')">
				<i class="mdi mdi-notification-clear-all" aria-hidden="true"></i><span>{{ $t('Clear scrollback') }}</span><kbd>{{ keyLabel('clear') }}</kbd>
			</button>
			<div class="term-menu-sep" role="separator"></div>
			<div class="term-menu-row">
				<i class="mdi mdi-format-size" aria-hidden="true"></i><span>{{ $t('Text size') }}</span>
				<button type="button" role="menuitem" class="term-icon-btn" :disabled="fontSize <= fontMin" :aria-label="$t('Smaller text')" @click="zoom(-1)">
					<i class="mdi mdi-minus" aria-hidden="true"></i>
				</button>
				<span class="term-menu-value">{{ fontSize }}</span>
				<button type="button" role="menuitem" class="term-icon-btn" :disabled="fontSize >= fontMax" :aria-label="$t('Larger text')" @click="zoom(1)">
					<i class="mdi mdi-plus" aria-hidden="true"></i>
				</button>
			</div>
			<button type="button" role="menuitemcheckbox" class="term-menu-item" :aria-checked="copyOnSelect ? 'true' : 'false'" @click="setCopyOnSelect(!copyOnSelect, true)">
				<i :class="['mdi', copyOnSelect ? 'mdi-checkbox-marked' : 'mdi-checkbox-blank-outline']" aria-hidden="true"></i><span>{{ $t('Copy on select') }}</span>
			</button>
			<template v-if="sessionInfo && !legacyMode">
				<div class="term-menu-sep" role="separator"></div>
				<button type="button" role="menuitem" class="term-menu-item" @click="menuRun('rename')">
					<i class="mdi mdi-pencil-outline" aria-hidden="true"></i><span>{{ $t('Rename session...') }}</span>
				</button>
				<button type="button" role="menuitem" class="term-menu-item is-danger" @click="menuRun('end')">
					<i class="mdi mdi-close-circle-outline" aria-hidden="true"></i><span>{{ sessionExited ? $t('Dismiss session') : $t('End session') }}</span>
				</button>
			</template>
			<p class="term-menu-hint">{{ $t('Shift+right-click for the browser menu') }}</p>
		</div>

		<!-- Keys a phone keyboard doesn't have. Ctrl is sticky: it applies
		     to the next key typed. -->
		<div v-if="showKeyBar" class="terminal-keybar" role="toolbar" :aria-label="$t('Terminal keys')">
			<button type="button" @click="sendKey('\u001b')">Esc</button>
			<button type="button" @click="sendKey('\t')">Tab</button>
			<button type="button" :class="{ 'is-on': ctrlArmed }" :aria-pressed="ctrlArmed ? 'true' : 'false'" @click="toggleCtrl">Ctrl</button>
			<button type="button" :aria-label="$t('Arrow left')" @click="sendKey('\u001b[D')">&larr;</button>
			<button type="button" :aria-label="$t('Arrow up')" @click="sendKey('\u001b[A')">&uarr;</button>
			<button type="button" :aria-label="$t('Arrow down')" @click="sendKey('\u001b[B')">&darr;</button>
			<button type="button" :aria-label="$t('Arrow right')" @click="sendKey('\u001b[C')">&rarr;</button>
			<button type="button" :aria-label="$t('Paste')" @click="menuRun('paste')"><i class="mdi mdi-content-paste" aria-hidden="true"></i></button>
		</div>
	</div>
</template>

<script>
import 'xterm/css/xterm.css'
import { Terminal } from 'xterm'
import { FitAddon } from 'xterm-addon-fit'
import { SearchAddon } from 'xterm-addon-search'
import { WebLinksAddon } from 'xterm-addon-web-links'
import terminalSessions from '@/service/terminalSessions.js'
import { apiError } from '@/utils/apiError'
import { guardTerminalKeydown, shortcutLabel } from './termKeys.js'
import { copyText, readClipboard, pasteNeedsConfirm, pasteLineCount, pastePreview } from './termClipboard.js'
import { TermSocket } from './termProtocol.js'
import { attachUrl, familyOf, exitSummary, statusOf } from './termSessions.js'

const FONT_KEY = 'terminalFontSize'
const COPY_ON_SELECT_KEY = 'terminalCopyOnSelect'
const PASTE_WARN_KEY = 'terminalPasteWarnOff'
const PREFS_EVENT = 'nvos-terminal-prefs'
const FONT_MIN = 9
const FONT_MAX = 28
const FONT_DEFAULT = 13
// Proxies (and the Cloudflare tunnel) cut idle WebSockets; the server pings
// every 25 s as well, this covers the other direction.
const KEEPALIVE_MS = 25000
const textEncoder = new TextEncoder()

function readNumber(key, fallback) {
	try {
		const v = parseInt(localStorage.getItem(key), 10)
		return isFinite(v) ? v : fallback
	} catch (e) {
		return fallback
	}
}

function writeItem(key, value) {
	try { localStorage.setItem(key, value) } catch (e) { /* private mode */ }
}

function basename(p) {
	const parts = String(p || '').split('/')
	return parts[parts.length - 1] || ''
}

export default {
	name: "terminal-card",
	props: {
		// Attach to this existing session (a Session object from the API).
		session: { type: Object, default: null },
		// ...or create one on mount: { kind: 'host'|'container', title?,
		// container?, shell? }.
		createSpec: { type: Object, default: null },
		// Old plain-connect endpoint (one socket = one shell, no reattach).
		initWsUrl: String,
		// A command line to type and run once the new shell is live (e.g.
		// Settings' "Open Terminal" for `rclone authorize`). Only for a
		// session this card creates, and only once.
		initCommand: String,
		// Offer "Close tab" when the session ends.
		closable: { type: Boolean, default: false },
		id: String,
		label: String,
	},
	data() {
		return {
			state: true,
			fontSize: Math.min(FONT_MAX, Math.max(FONT_MIN, readNumber(FONT_KEY, FONT_DEFAULT))),
			fontMin: FONT_MIN,
			fontMax: FONT_MAX,
			copyOnSelect: readNumber(COPY_ON_SELECT_KEY, 0) === 1,
			pasteWarnOff: readNumber(PASTE_WARN_KEY, 0) === 1,
			ctrlArmed: false,
			pasteSheet: null,
			coarsePointer: typeof window !== 'undefined' && window.matchMedia ? window.matchMedia('(pointer: coarse)').matches : false,
			sessionInfo: this.session || null,
			creating: false,
			createError: null,
			connState: 'idle',
			connDetail: {},
			everLive: false,
			exitInfo: null,
			initCommandSent: false,
			pillText: '',
			pillIcon: '',
			findOpen: false,
			findQuery: '',
			findCase: false,
			findMiss: false,
			findResults: null,
			menu: null,
			pasteAsk: null,
			pasteAskRemember: false,
		}
	},
	computed: {
		showKeyBar() {
			return this.coarsePointer || this.$store.state.device === 'mobile'
		},
		legacyMode() {
			return !!this.initWsUrl && !this.session && !this.createSpec
		},
		sessionExited() {
			return !!this.exitInfo || (this.sessionInfo && this.sessionInfo.state === 'exited')
		},
		canInput() {
			return this.connState === 'live' && !this.exitInfo
		},
		findCountText() {
			if (!this.findQuery) return ''
			if (this.findMiss) return this.$t('No results')
			const r = this.findResults
			if (!r) return ''
			if (r.resultCount === undefined) return this.$t('1000+')
			if (r.resultCount <= 0) return this.$t('No results')
			return `${r.resultIndex + 1}/${r.resultCount}`
		},
		banner() {
			const t = (s, p) => this.$t(s, p)
			if (this.createError) {
				const actions = [{ id: 'retry-create', label: t('Try again'), icon: 'mdi-refresh', disabled: this.creating }]
				if (this.createError.status === 429) actions.unshift({ id: 'show-sessions', label: t('Show sessions'), icon: 'mdi-view-list-outline' })
				if (this.closable) actions.push({ id: 'close', label: t('Close tab'), quiet: true })
				return { tone: 'error', icon: 'mdi-alert-circle-outline', text: this.createError.message, actions }
			}
			if (this.legacyMode) {
				if (this.connState !== 'ended') return null
				return {
					tone: 'info', icon: 'mdi-console',
					text: this.connDetail.opened ? (this.connDetail.reason || t('Terminal session ended')) : t('Failed to establish terminal connection'),
					actions: [{ id: 'reconnect', label: t('Reconnect'), icon: 'mdi-refresh' }]
				}
			}
			const restart = { id: 'restart', label: t('New session'), icon: 'mdi-plus', disabled: this.creating }
			const close = this.closable ? [{ id: 'close', label: t('Close tab'), quiet: true }] : []
			if (this.exitInfo) {
				return { tone: 'info', icon: 'mdi-flag-checkered', text: t(exitSummary(this.exitInfo), { code: this.exitInfo.code }), actions: [restart, ...close] }
			}
			if (this.connState === 'gone') {
				return { tone: 'info', icon: 'mdi-link-variant-off', text: t('This session is no longer running - the server may have restarted.'), actions: [restart, ...close] }
			}
			if (this.everLive && (this.connState === 'reconnecting' || this.connState === 'connecting')) {
				return {
					tone: 'warn', icon: 'mdi-wifi-strength-alert-outline',
					text: t('Connection lost - the shell keeps running. Reconnecting...'),
					actions: this.connState === 'reconnecting' ? [{ id: 'retry-now', label: t('Retry now'), icon: 'mdi-refresh' }] : []
				}
			}
			if (this.connState === 'reconnecting' || (this.connState === 'connecting' && this.connDetail.attempt > 0)) {
				return { tone: 'warn', icon: 'mdi-lan-pending', text: t("Can't reach the terminal. Retrying..."), actions: [] }
			}
			return null
		},
	},
	watch: {
		connState(state) {
			if (state === 'restoring' && this.connDetail.replayBytes > 0) this.showPill(this.$t('Restoring session...'), 'mdi-history', 0)
			else if (state === 'connecting' && !this.everLive && !this.connDetail.attempt) this.showPill(this.$t('Connecting...'), 'mdi-lan-connect', 0)
			else if (state === 'live' && (this.pillText === this.$t('Restoring session...') || this.pillText === this.$t('Connecting...'))) this.hidePill()
		},
	},
	mounted() {
		this.lastSize = { cols: 0, rows: 0 }
		this.createTerm()

		this.onGlobalKeyBound = (e) => this.onGlobalKey(e)
		this.onPasteBound = (e) => this.onPasteEvent(e)
		this.onDocPointerBound = (e) => this.onDocPointer(e)
		this.onPrefsBound = () => this.onPrefsChanged()
		this.onWakeBound = () => this.onWake()
		window.addEventListener('keydown', this.onGlobalKeyBound, true)
		this.$refs.terminalRoot.addEventListener('paste', this.onPasteBound, true)
		document.addEventListener('pointerdown', this.onDocPointerBound, true)
		window.addEventListener(PREFS_EVENT, this.onPrefsBound)
		window.addEventListener('online', this.onWakeBound)
		document.addEventListener('visibilitychange', this.onWakeBound)

		this.resizeObserver = new ResizeObserver(() => this.onWindowResize())
		this.resizeObserver.observe(this.$refs.terminalRoot)
		window.addEventListener('resize', this.onWindowResize)

		if (this.session) this.attach()
		else if (this.legacyMode) this.connectLegacy()
		else this.createAndAttach(this.createSpec || { kind: 'host' })
	},
	beforeDestroy() {
		this.destroying = true
		this.stopKeepalive()
		if (this.sock) this.sock.detach()
		this.sock = null
		window.removeEventListener('keydown', this.onGlobalKeyBound, true)
		if (this.$refs.terminalRoot) this.$refs.terminalRoot.removeEventListener('paste', this.onPasteBound, true)
		document.removeEventListener('pointerdown', this.onDocPointerBound, true)
		window.removeEventListener(PREFS_EVENT, this.onPrefsBound)
		window.removeEventListener('online', this.onWakeBound)
		document.removeEventListener('visibilitychange', this.onWakeBound)
		window.removeEventListener('resize', this.onWindowResize)
		if (this.resizeObserver) this.resizeObserver.disconnect()
		cancelAnimationFrame(this.fitFrame)
		clearTimeout(this.pillTimer)
		clearTimeout(this.pasteFallbackTimer)
		if (this.term) {
			this.term.dispose()
			this.term = null
		}
	},

	methods: {
		// The terminal is created once and kept for the component's life.
		createTerm() {
			const term = new Terminal({
				fontSize: this.fontSize,
				lineHeight: 1.2,
				fontFamily: '"Fira Code", "JetBrains Mono", "Cascadia Code", Menlo, Monaco, Consolas, "Courier New", monospace',
				cursorStyle: 'block',
				cursorBlink: true,
				allowProposedApi: true,
				scrollback: 10000,
				// Right-click opens our menu; it must not replace the
				// selection the user is about to copy.
				rightClickSelectsWord: false,
				macOptionIsMeta: true,
				tabStopWidth: 4,
				windowsMode: false,
				theme: {
					background: '#0f1115',
					foreground: '#f4f4f5',
					cursor: '#7df9c5',
					cursorAccent: '#0f1115',
					selection: 'rgba(214, 219, 228, 0.28)',
					black: '#0f1115',
					red: '#ef4444',
					green: '#22c55e',
					yellow: '#eab308',
					blue: '#3b82f6',
					magenta: '#a855f7',
					cyan: '#06b6d4',
					white: '#f4f4f5',
					brightBlack: '#71717a',
					brightRed: '#f87171',
					brightGreen: '#4ade80',
					brightYellow: '#fde047',
					brightBlue: '#60a5fa',
					brightMagenta: '#c084fc',
					brightCyan: '#22d3ee',
					brightWhite: '#ffffff'
				}
			})
			this.fitAddon = new FitAddon()
			term.loadAddon(this.fitAddon)
			this.searchAddon = new SearchAddon()
			term.loadAddon(this.searchAddon)
			this.searchAddon.onDidChangeResults((r) => { this.findResults = r || { resultIndex: -1, resultCount: undefined } })
			// Links are only "live" with a modifier held (VS Code's convention)
			// so a plain click still places the cursor / starts a selection.
			term.loadAddon(new WebLinksAddon((event, uri) => {
				if (event.ctrlKey || event.metaKey) {
					window.open(uri, '_blank', 'noopener,noreferrer')
				}
			}))
			term.onData(data => this.sendInput(data))
			term.onBinary(data => this.sendBinary(data))
			term.open(this.$refs.xtermEl)
			this.term = term
			this.fit()
		},

		fit() {
			if (!this.term || !this.fitAddon) return
			try {
				this.fitAddon.fit()
			} catch (e) {
				// Hidden (display:none) container: nothing to measure yet.
			}
		},

		size() {
			return {
				cols: Math.max(this.term ? this.term.cols : 0, 20),
				rows: Math.max(this.term ? this.term.rows : 0, 5),
			}
		},

		token() {
			try {
				return localStorage.getItem('access_token') || this.$store.state.access_token
			} catch (e) {
				return this.$store.state.access_token
			}
		},

		// --- sessions ------------------------------------------------------
		async createAndAttach(spec) {
			if (this.creating) return
			this.lastSpec = spec
			this.creating = true
			this.createError = null
			this.fit()
			const { cols, rows } = this.size()
			const body = { cols, rows }
			if (spec.title) body.title = spec.title
			if (spec.container) body.container = spec.container
			if (spec.shell) body.shell = spec.shell
			try {
				const s = await terminalSessions.create(spec.kind, body)
				if (this.destroying) return
				if (!s || !s.id) throw new Error(this.$t('The server did not return a session'))
				this.sessionInfo = s
				this.exitInfo = null
				this.createdHere = true
				this.$emit('session', s)
				this.attach()
			} catch (err) {
				if (this.destroying) return
				const status = statusOf(err)
				this.createError = {
					status,
					message: status === 429
						? this.$t('You have reached the limit of open terminal sessions. Close one to start another.')
						: apiError(err, this.$t('Could not start a terminal'))
				}
				this.$emit('create-error', this.createError)
			} finally {
				this.creating = false
			}
		},

		attach() {
			if (this.sock) this.sock.detach()
			const info = this.sessionInfo
			const kind = familyOf(info)
			const id = info.id
			const sock = new TermSocket({
				persistent: true,
				url: () => {
					const { cols, rows } = this.size()
					return attachUrl(this.sessionInfo, { wsBase: `${this.$wsProtocol}//${this.$baseURL}`, token: this.token(), cols, rows })
				},
				verify: () => terminalSessions.probe(kind, id),
				onState: (state, detail) => this.onConnState(state, detail),
				onHello: (s) => {
					// The replay is the whole visible history: start from a
					// clean screen so a reattach doesn't print it twice.
					if (this.term) this.term.reset()
					if (s) this.updateSession(s)
				},
				onOutput: (bytes) => this.term && this.term.write(bytes),
				onText: (text) => this.term && this.term.write(text),
				onLive: () => this.onLive(),
				onSession: (s) => this.updateSession(s),
				onExit: (d) => this.onExit(d),
			})
			this.sock = sock
			sock.open()
		},

		connectLegacy() {
			if (this.sock) this.sock.detach()
			this.fit()
			const sock = new TermSocket({
				persistent: false,
				url: () => {
					const { cols, rows } = this.size()
					try {
						const u = new URL(this.initWsUrl)
						u.searchParams.set('cols', cols)
						u.searchParams.set('rows', rows)
						if (u.searchParams.has('token')) u.searchParams.set('token', this.token())
						return u.toString()
					} catch (e) {
						return this.initWsUrl
					}
				},
				onState: (state, detail) => {
					this.onConnState(state, detail)
					if (state === 'live') this.onLive()
					if (state === 'ended' && this.term) this.term.write('\r\n\x1b[2m[' + this.$t('session ended') + ']\x1b[0m\r\n')
				},
				onOutput: (bytes) => this.term && this.term.write(bytes),
				onText: (text) => this.term && this.term.write(text),
			})
			this.sock = sock
			sock.open()
		},

		onConnState(state, detail) {
			this.connState = state
			this.connDetail = detail || {}
			if (state === 'live') this.everLive = true
			if (state === 'live') this.startKeepalive()
			else this.stopKeepalive()
			this.$emit('state', state)
			if (state === 'gone') this.$emit('gone', this.sessionInfo)
		},

		onLive() {
			if (!this.term) return
			this.term.write('', () => this.term && this.term.scrollToBottom())
			this.sendResize(true)
			if (this.state) this.term.focus()
			this.runInitCommand()
		},

		onExit(detail) {
			this.exitInfo = detail
			if (this.sessionInfo) this.sessionInfo = Object.assign({}, this.sessionInfo, { state: 'exited', exit_code: detail.code, exit_reason: detail.reason })
			if (this.term) this.term.write('\r\n\x1b[2m[' + this.$t(exitSummary(detail), { code: detail.code }) + ']\x1b[0m\r\n')
			this.$emit('exit', detail)
		},

		updateSession(s) {
			this.sessionInfo = s
			if (s.state === 'exited' && !this.exitInfo) {
				this.exitInfo = { code: typeof s.exit_code === 'number' ? s.exit_code : -1, reason: s.exit_reason || 'exited' }
			}
			this.$emit('session', s)
		},

		// A fresh session like this one (same kind / container / shell).
		restart() {
			const s = this.sessionInfo
			const spec = s
				? { kind: familyOf(s), container: s.kind === 'container' ? (s.container_id || s.container) : undefined, shell: s.kind === 'container' ? basename(s.shell) : undefined }
				: (this.lastSpec || this.createSpec || { kind: 'host' })
			if (this.sock) this.sock.detach()
			this.sock = null
			this.exitInfo = null
			this.connState = 'idle'
			this.everLive = false
			if (this.term) this.term.write('\r\n')
			this.createAndAttach(spec)
		},

		onBannerAction(id) {
			if (id === 'retry-create') this.createAndAttach(this.lastSpec || this.createSpec || { kind: 'host' })
			else if (id === 'show-sessions') this.$emit('show-sessions')
			else if (id === 'close') this.$emit('close')
			else if (id === 'restart') this.restart()
			else if (id === 'retry-now' && this.sock) this.sock.retryNow()
			else if (id === 'reconnect') {
				if (this.term) this.term.write('\r\n')
				this.connectLegacy()
			}
		},

		onWake() {
			if (document.visibilityState === 'hidden') return
			if (this.sock && this.connState === 'reconnecting') this.sock.retryNow()
		},

		// Leave the session running and stop showing it.
		detach() {
			this.stopKeepalive()
			if (this.sock) this.sock.detach()
			this.sock = null
		},

		// --- wire ----------------------------------------------------------
		sendInput(data) {
			if (!data) return
			if (this.ctrlArmed) data = this.applyCtrl(data)
			this.sendRaw(data)
		},

		sendBinary(data) {
			if (!this.sock) return
			const buf = new Uint8Array(data.length)
			for (let i = 0; i < data.length; i++) buf[i] = data.charCodeAt(i) & 0xff
			this.sock.send(buf)
		},

		sendRaw(data) {
			if (this.sock) this.sock.send(textEncoder.encode(data))
		},

		sendResize(force) {
			if (!this.term || !this.sock) return
			const { cols, rows } = this.term
			if (!cols || !rows) return
			if (!force && cols === this.lastSize.cols && rows === this.lastSize.rows) return
			if (this.sock.sendControl({ type: 'resize', cols, rows })) this.lastSize = { cols, rows }
		},

		startKeepalive() {
			this.stopKeepalive()
			this.keepaliveTimer = setInterval(() => this.sock && this.sock.sendControl({ type: 'ping' }), KEEPALIVE_MS)
		},

		stopKeepalive() {
			if (this.keepaliveTimer) {
				clearInterval(this.keepaliveTimer)
				this.keepaliveTimer = null
			}
		},

		// --- keyboard ------------------------------------------------------
		// Capture phase on window: runs before xterm and before the browser
		// acts on the chord (see termKeys.js).
		onGlobalKey(e) {
			guardTerminalKeydown(e, {
				inside: (t) => !!t && !!this.$refs.xtermEl && this.$refs.xtermEl.contains(t),
				run: (action) => (action === 'paste' ? this.keyboardPaste() : this.runAction(action)),
			})
		},

		keyLabel(action) {
			return shortcutLabel(action)
		},

		runAction(action) {
			switch (action) {
				case 'copy': return this.copySelection(true)
				case 'paste': return this.menuPaste()
				case 'find': return this.openFind()
				case 'selectAll': return this.term && this.term.selectAll()
				case 'clear': return this.term && this.term.clear()
				case 'zoomIn': return this.zoom(1)
				case 'zoomOut': return this.zoom(-1)
				case 'zoomReset': return this.zoom(0)
				case 'rename': return this.$emit('rename', this.sessionInfo)
				case 'end': return this.$emit('end', this.sessionInfo)
			}
		},

		// --- clipboard -----------------------------------------------------
		copySelection(explicit) {
			const text = this.term && this.term.getSelection()
			if (!text) {
				if (explicit) this.showPill(this.$t('Select text to copy'), 'mdi-cursor-text')
				return Promise.resolve(false)
			}
			return copyText(text).then((ok) => {
				if (ok) this.showPill(this.$t('Copied'), 'mdi-check')
				else if (explicit) this.showPill(this.$t("Couldn't copy - try Shift+right-click, Copy"), 'mdi-alert-outline', 3500)
				return ok
			})
		},

		onMouseUp(e) {
			if (!this.copyOnSelect || e.button !== 0) return
			// After xterm has finished the selection for this mouseup.
			setTimeout(() => {
				if (this.term && this.term.hasSelection()) this.copySelection(false)
			}, 0)
		},

		// Ctrl+Shift+V: the browser's own paste fires a `paste` event into
		// xterm's textarea (onPasteEvent). If none comes (a browser without
		// that shortcut), read the clipboard directly when allowed.
		keyboardPaste() {
			this.awaitingPasteEvent = true
			clearTimeout(this.pasteFallbackTimer)
			this.pasteFallbackTimer = setTimeout(() => {
				if (!this.awaitingPasteEvent) return
				this.awaitingPasteEvent = false
				this.menuPaste()
			}, 250)
		},

		onPasteEvent(e) {
			const el = this.$refs.xtermEl
			if (!el || !el.contains(e.target)) return
			this.awaitingPasteEvent = false
			clearTimeout(this.pasteFallbackTimer)
			const text = e.clipboardData ? e.clipboardData.getData('text/plain') : ''
			e.preventDefault()
			e.stopPropagation()
			this.pasteText(text)
		},

		menuPaste() {
			readClipboard().then((text) => {
				if (text === null) {
					if (this.showKeyBar) return this.openPasteSheet()
					this.showPill(this.$t('The browser only lets this page paste from the keyboard: press {key}', { key: shortcutLabel('paste') }), 'mdi-keyboard-outline', 4000)
					return
				}
				this.pasteText(text)
			})
		},

		pasteText(text) {
			if (!text || !this.term) return
			if (!this.canInput) {
				this.showPill(this.$t('Not connected - nothing was pasted'), 'mdi-alert-outline')
				return
			}
			const bracketed = !!(this.term.modes && this.term.modes.bracketedPasteMode)
			if (pasteNeedsConfirm(text, { bracketed, disabled: this.pasteWarnOff })) {
				this.pasteAsk = { text, count: pasteLineCount(text), preview: pastePreview(text) }
				this.pasteAskRemember = false
				this.$nextTick(() => this.$refs.pasteConfirmBtn && this.$refs.pasteConfirmBtn.focus())
				return
			}
			this.term.paste(text)
			this.term.focus()
		},

		openPasteSheet() {
			this.pasteSheet = { text: '' }
			this.$nextTick(() => this.$refs.pasteSheetInput && this.$refs.pasteSheetInput.focus())
		},

		closePasteSheet() {
			this.pasteSheet = null
			if (this.term) this.term.focus()
		},

		submitPasteSheet() {
			const text = this.pasteSheet ? this.pasteSheet.text : ''
			this.pasteSheet = null
			this.pasteText(text)
		},

		confirmPaste() {
			const ask = this.pasteAsk
			this.pasteAsk = null
			if (this.pasteAskRemember) {
				this.pasteWarnOff = true
				writeItem(PASTE_WARN_KEY, '1')
			}
			if (ask && this.term) {
				this.term.paste(ask.text)
				this.term.focus()
			}
		},

		cancelPaste() {
			this.pasteAsk = null
			if (this.term) this.term.focus()
		},

		// --- find ----------------------------------------------------------
		openFind() {
			const sel = this.term && this.term.getSelection()
			if (sel && !sel.includes('\n') && sel.length < 200) this.findQuery = sel
			this.findOpen = true
			this.$nextTick(() => {
				const input = this.$refs.findInput
				if (input) {
					input.focus()
					input.select()
				}
				if (this.findQuery) this.find(1, true)
			})
		},

		closeFind() {
			this.findOpen = false
			this.findResults = null
			if (this.searchAddon) this.searchAddon.clearDecorations()
			if (this.term) this.term.focus()
		},

		// dir: 1 next, -1 previous. incremental: typing refines the match
		// under the cursor instead of jumping past it.
		find(dir, incremental) {
			if (!this.searchAddon) return
			if (!this.findQuery) {
				this.searchAddon.clearDecorations()
				this.findMiss = false
				this.findResults = null
				return
			}
			const opts = {
				caseSensitive: this.findCase,
				incremental: !!incremental && dir > 0,
				decorations: {
					matchBackground: '#3f3f46',
					matchBorder: '#71717a',
					matchOverviewRuler: '#a1a1aa',
					activeMatchBackground: '#1f5c4a',
					activeMatchBorder: '#7df9c5',
					activeMatchColorOverviewRuler: '#7df9c5'
				}
			}
			const found = dir < 0 ? this.searchAddon.findPrevious(this.findQuery, opts) : this.searchAddon.findNext(this.findQuery, opts)
			this.findMiss = !found
		},

		// --- context menu --------------------------------------------------
		onContextMenu(e) {
			const el = this.$refs.xtermEl
			if (!el || !el.contains(e.target) || e.shiftKey) return
			e.preventDefault()
			const root = this.$refs.terminalRoot.getBoundingClientRect()
			this.menu = { x: e.clientX - root.left, y: e.clientY - root.top, hasSelection: !!(this.term && this.term.hasSelection()) }
			this.$nextTick(() => {
				const m = this.$refs.menu
				if (!m) return
				const r = m.getBoundingClientRect()
				const maxX = root.width - r.width - 4
				const maxY = root.height - r.height - 4
				this.menu = Object.assign({}, this.menu, { x: Math.max(4, Math.min(this.menu.x, maxX)), y: Math.max(4, Math.min(this.menu.y, maxY)) })
				const first = m.querySelector('.term-menu-item:not([disabled])')
				if (first) first.focus()
			})
		},

		closeMenu(refocus) {
			if (!this.menu) return
			this.menu = null
			if (refocus && this.term) this.term.focus()
		},

		onDocPointer(e) {
			if (this.menu && this.$refs.menu && !this.$refs.menu.contains(e.target)) this.closeMenu(false)
		},

		onMenuKey(e) {
			if (e.key === 'Escape') {
				e.preventDefault()
				this.closeMenu(true)
				return
			}
			if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp') return
			e.preventDefault()
			const items = Array.from(this.$refs.menu.querySelectorAll('button:not([disabled])'))
			const i = items.indexOf(document.activeElement)
			const next = e.key === 'ArrowDown' ? (i + 1) % items.length : (i - 1 + items.length) % items.length
			if (items[next]) items[next].focus()
		},

		menuRun(action) {
			this.closeMenu(action !== 'find' && action !== 'rename')
			this.runAction(action)
		},

		// --- text size / prefs ---------------------------------------------
		// delta: +1 / -1, or 0 to reset.
		zoom(delta) {
			const next = delta === 0 ? FONT_DEFAULT : Math.min(FONT_MAX, Math.max(FONT_MIN, this.fontSize + delta))
			this.setFontSize(next)
		},

		setFontSize(size) {
			if (size === this.fontSize && this.term && this.term.options.fontSize === size) return
			this.fontSize = size
			writeItem(FONT_KEY, String(size))
			this.applyFontSize()
			window.dispatchEvent(new Event(PREFS_EVENT))
		},

		applyFontSize() {
			if (!this.term) return
			if (typeof this.term.setOption === 'function') this.term.setOption('fontSize', this.fontSize)
			else this.term.options.fontSize = this.fontSize
			this.onWindowResize()
		},

		setCopyOnSelect(on, announce) {
			this.copyOnSelect = !!on
			writeItem(COPY_ON_SELECT_KEY, on ? '1' : '0')
			if (announce) window.dispatchEvent(new Event(PREFS_EVENT))
		},

		// Another terminal changed a shared preference.
		onPrefsChanged() {
			const size = Math.min(FONT_MAX, Math.max(FONT_MIN, readNumber(FONT_KEY, FONT_DEFAULT)))
			if (size !== this.fontSize) {
				this.fontSize = size
				this.applyFontSize()
			}
			this.copyOnSelect = readNumber(COPY_ON_SELECT_KEY, 0) === 1
			this.pasteWarnOff = readNumber(PASTE_WARN_KEY, 0) === 1
			this.$emit('prefs')
		},

		showPill(text, icon, ms = 1400) {
			clearTimeout(this.pillTimer)
			this.pillText = text
			this.pillIcon = icon || ''
			if (ms > 0) this.pillTimer = setTimeout(() => this.hidePill(), ms)
		},

		hidePill() {
			clearTimeout(this.pillTimer)
			this.pillText = ''
			this.pillIcon = ''
		},

		// --- phone key bar ---------------------------------------------------
		sendKey(seq) {
			this.sendInput(seq)
			if (this.term) this.term.focus()
		},

		toggleCtrl() {
			this.ctrlArmed = !this.ctrlArmed
			if (this.term) this.term.focus()
		},

		applyCtrl(data) {
			this.ctrlArmed = false
			// Ctrl + an arrow / Home / End from the key bar: the xterm modifier
			// form (Ctrl+Left = ESC[1;5D, word left in most shells).
			const m = /^\u001b[[O]([A-DHF])$/.exec(data)
			if (m) return '\u001b[1;5' + m[1]
			if (data.length !== 1) return data
			const c = data.toUpperCase().charCodeAt(0)
			// Ctrl+@..Ctrl+_ (A-Z, [, \, ], ^, _) map to 0x00-0x1f.
			if (c >= 64 && c <= 95) return String.fromCharCode(c - 64)
			if (data === ' ') return '\u0000'
			if (data === '?') return '\u007f'
			return data
		},

		// Only for a session this card started, once; a short delay lets the
		// shell print its prompt first.
		runInitCommand() {
			if (!this.initCommand || this.initCommandSent) return
			if (!this.legacyMode && !this.createdHere) return
			this.initCommandSent = true
			setTimeout(() => this.sendRaw(this.initCommand + '\r'), 400)
		},

		onWindowResize() {
			if (!this.term || !this.fitAddon) return
			cancelAnimationFrame(this.fitFrame)
			this.fitFrame = requestAnimationFrame(() => {
				this.fit()
				this.sendResize(false)
			})
		},

		focus() {
			if (this.term) this.term.focus()
		},

		active(state) {
			this.state = state
			if (state) {
				this.onWindowResize()
				if (this.term) {
					this.$nextTick(() => this.term && this.term.focus())
				}
			} else {
				this.closeMenu(false)
			}
		}
	}
}
</script>

<style lang="scss" scoped>
.terminal-instance {
	position: relative;
	display: flex;
	flex-direction: column;
	width: 100%;
	height: 100%;
	background: var(--console-bg);
	padding: var(--space-1) var(--space-2);
	box-sizing: border-box;
	overflow: hidden;
}

.xterm {
	flex: 1 1 auto;
	min-height: 0;
	width: 100%;
	height: 100%;

	::v-deep .xterm-viewport {
		overflow-y: auto !important;
	}
}

.term-fade-enter-active,
.term-fade-leave-active {
	transition: opacity 0.15s ease, transform 0.15s ease;
}

.term-fade-enter,
.term-fade-leave-to {
	opacity: 0;
	transform: translate(-50%, -4px);
}

.term-pill {
	position: absolute;
	top: var(--space-2);
	left: 50%;
	transform: translateX(-50%);
	display: inline-flex;
	align-items: center;
	gap: var(--space-1);
	max-width: calc(100% - 2 * var(--space-4));
	padding: 0.2rem var(--space-3);
	border-radius: 999px;
	background: rgba(39, 39, 42, 0.95);
	border: 1px solid rgba(255, 255, 255, 0.14);
	color: #f4f4f5;
	font-size: var(--font-xs);
	box-shadow: var(--shadow-xl);
	z-index: 6;
	pointer-events: none;

	span {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
}

.term-find {
	position: absolute;
	top: var(--space-2);
	right: var(--space-3);
	display: flex;
	align-items: center;
	gap: 2px;
	max-width: calc(100% - 2 * var(--space-3));
	padding: 3px 4px 3px var(--space-2);
	border-radius: var(--radius-control);
	background: var(--console-raised);
	border: 1px solid rgba(255, 255, 255, 0.16);
	box-shadow: var(--shadow-xl);
	z-index: 7;
}

.term-find-icon {
	color: #a1a1aa;
}

.term-find-input {
	width: 12rem;
	min-width: 5rem;
	flex: 1 1 auto;
	height: 1.6rem;
	padding: 0 var(--space-1);
	border: none;
	outline: none;
	background: transparent;
	color: #f4f4f5;
	font-size: var(--font-xs);
	font-family: inherit;
}

.term-find-count {
	flex-shrink: 0;
	min-width: 3.2rem;
	text-align: right;
	padding-right: var(--space-1);
	color: #a1a1aa;
	font-size: var(--font-xs);
	font-variant-numeric: tabular-nums;

	&.is-miss {
		color: #fca5a5;
	}
}

.term-find-case {
	font-size: 0.7rem;
	font-weight: 700;
}

.term-icon-btn {
	flex-shrink: 0;
	display: inline-flex;
	align-items: center;
	justify-content: center;
	width: 1.6rem;
	height: 1.6rem;
	padding: 0;
	border: none;
	border-radius: var(--radius-sm);
	background: transparent;
	color: #d4d4d8;
	cursor: pointer;

	&:hover:not(:disabled) {
		background: rgba(255, 255, 255, 0.1);
		color: #fff;
	}

	&.is-on {
		background: rgba(125, 249, 197, 0.22);
		color: #fff;
	}

	&:disabled {
		opacity: 0.4;
		cursor: default;
	}

	&:focus-visible {
		outline: 2px solid var(--console-accent);
		outline-offset: 1px;
	}
}

.terminal-ended {
	position: absolute;
	left: 50%;
	bottom: var(--space-4);
	transform: translateX(-50%);
	display: flex;
	align-items: center;
	gap: var(--space-2);
	max-width: calc(100% - 2 * var(--space-4));
	padding: var(--space-2) var(--space-2) var(--space-2) var(--space-3);
	border-radius: var(--radius-control);
	background: var(--console-raised);
	border: 1px solid rgba(255, 255, 255, 0.14);
	box-shadow: var(--shadow-xl);
	color: #f4f4f5;
	font-size: var(--font-xs);
	z-index: 5;

	&.is-warn {
		border-color: rgba(234, 179, 8, 0.45);

		.terminal-ended-icon {
			color: #fde047;
		}
	}

	&.is-error {
		border-color: rgba(239, 68, 68, 0.5);

		.terminal-ended-icon {
			color: #fca5a5;
		}
	}

	.terminal-ended-icon {
		flex-shrink: 0;
		font-size: 1rem;
		color: #a1a1aa;
	}

	.terminal-ended-text {
		min-width: 0;
		margin-right: var(--space-1);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
}

.terminal-ended-btn {
	flex-shrink: 0;
	display: inline-flex;
	align-items: center;
	height: 1.75rem;
	padding: 0 var(--space-3);
	border: none;
	border-radius: var(--radius-sm);
	background: var(--console-accent);
	color: #0f1115;
	font-size: var(--font-xs);
	font-weight: 600;
	cursor: pointer;
	white-space: nowrap;

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

	&:disabled {
		opacity: 0.6;
		cursor: default;
	}

	&:focus-visible {
		outline: 2px solid var(--console-accent);
		outline-offset: 2px;
	}
}

.term-scrim {
	position: absolute;
	inset: 0;
	display: flex;
	align-items: center;
	justify-content: center;
	padding: var(--space-4);
	background: rgba(0, 0, 0, 0.45);
	z-index: 9;
}

.term-dialog {
	width: 100%;
	max-width: 30rem;
	padding: var(--space-4);
	border-radius: var(--radius-card, 12px);
	background: var(--console-raised);
	border: 1px solid rgba(255, 255, 255, 0.14);
	box-shadow: var(--shadow-xl);
	color: #f4f4f5;
	font-size: var(--font-sm);
}

.term-dialog-title {
	font-weight: 600;
	margin-bottom: var(--space-1);
}

.term-dialog-text {
	color: #a1a1aa;
	font-size: var(--font-xs);
	margin-bottom: var(--space-3);
}

.term-paste-input {
	display: block;
	width: 100%;
	margin: 0 0 var(--space-3);
	padding: var(--space-2) var(--space-3);
	border: 1px solid rgba(255, 255, 255, 0.16);
	border-radius: var(--radius-sm);
	background: var(--console-bg);
	color: #e4e4e7;
	font-family: "Fira Code", "JetBrains Mono", Menlo, Consolas, monospace;
	font-size: 16px; /* 16px: iOS doesn't zoom the page on focus */
	resize: vertical;
}

.term-dialog-preview {
	max-height: 9rem;
	overflow: auto;
	margin: 0 0 var(--space-3);
	padding: var(--space-2) var(--space-3);
	border-radius: var(--radius-sm);
	background: var(--console-bg);
	color: #e4e4e7;
	font-family: "Fira Code", "JetBrains Mono", Menlo, Consolas, monospace;
	font-size: var(--font-xs);
	line-height: 1.45;
	white-space: pre;
}

.term-dialog-check {
	display: inline-flex;
	align-items: center;
	gap: var(--space-2);
	color: #d4d4d8;
	font-size: var(--font-xs);
	cursor: pointer;
}

.term-dialog-actions {
	display: flex;
	justify-content: flex-end;
	gap: var(--space-2);
	margin-top: var(--space-3);
}

.term-menu {
	position: absolute;
	min-width: 14rem;
	padding: var(--space-1);
	border-radius: var(--radius-control);
	background: var(--console-raised);
	border: 1px solid rgba(255, 255, 255, 0.14);
	box-shadow: var(--shadow-xl);
	color: #f4f4f5;
	font-size: var(--font-xs);
	z-index: 10;
}

.term-menu-item,
.term-menu-row {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	width: 100%;
	min-height: 1.9rem;
	padding: 0 var(--space-2);
	border: none;
	border-radius: var(--radius-sm);
	background: transparent;
	color: inherit;
	font: inherit;
	text-align: left;

	> .mdi {
		width: 1rem;
		color: #a1a1aa;
		font-size: 0.95rem;
	}

	> span {
		flex: 1 1 auto;
	}

	kbd {
		background: none;
		border: none;
		box-shadow: none;
		padding: 0;
		color: #71717a;
		font-family: inherit;
		font-size: 0.7rem;
	}
}

.term-menu-item {
	cursor: pointer;

	&:hover:not(:disabled),
	&:focus-visible {
		background: rgba(125, 249, 197, 0.16);
		outline: none;
	}

	&:disabled {
		opacity: 0.4;
		cursor: default;
	}

	&.is-danger {
		color: #fca5a5;

		> .mdi {
			color: #fca5a5;
		}
	}
}

.term-menu-value {
	flex: 0 0 auto !important;
	min-width: 1.4rem;
	text-align: center;
	font-variant-numeric: tabular-nums;
}

.term-menu-sep {
	height: 1px;
	margin: var(--space-1) 0;
	background: rgba(255, 255, 255, 0.1);
}

.term-menu-hint {
	margin: var(--space-1) var(--space-2) 2px;
	color: #71717a;
	font-size: 0.68rem;
}

.terminal-keybar {
	flex-shrink: 0;
	display: flex;
	gap: var(--space-1);
	padding-top: var(--space-1);
	overflow-x: auto;

	button {
		flex: 1 0 auto;
		min-width: 2.5rem;
		min-height: 2.25rem;
		border: 1px solid rgba(255, 255, 255, 0.14);
		border-radius: var(--radius-sm);
		background: var(--console-raised);
		color: #f4f4f5;
		font-size: var(--font-xs);
		cursor: pointer;

		&.is-on {
			background: var(--console-accent);
			border-color: var(--console-accent);
			color: #0f1115;
		}

		&:focus-visible {
			outline: 2px solid var(--console-accent);
			outline-offset: 1px;
		}
	}
}
</style>
