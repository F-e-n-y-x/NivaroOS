<template>
	<div ref="terminalRoot" class="terminal-instance" :class="{ 'has-keybar': showKeyBar }">
		<div ref="xtermEl" class="xterm"></div>

		<!-- Session ended / could not connect. Deliberately a small overlay,
		     not a replacement for the terminal: the scrollback (including any
		     error the backend printed before closing) stays readable. -->
		<div v-if="ended" class="terminal-ended" role="status">
			<span class="terminal-ended-text">{{ endReason || $t('Terminal session ended') }}</span>
			<button type="button" class="terminal-ended-btn" :disabled="connecting" @click="reconnect">
				<i class="mdi mdi-refresh mr-1" aria-hidden="true"></i>{{ $t('Reconnect') }}
			</button>
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
		</div>
	</div>
</template>

<script>
import qs from 'qs'
import 'xterm/css/xterm.css'
import { Terminal } from 'xterm'
import { FitAddon } from 'xterm-addon-fit'
import { WebLinksAddon } from 'xterm-addon-web-links'

const FONT_KEY = 'terminalFontSize'
const COPY_ON_SELECT_KEY = 'terminalCopyOnSelect'
const FONT_MIN = 9
const FONT_MAX = 28
const FONT_DEFAULT = 13
// Some proxies drop a WebSocket after ~60s without traffic.
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

export default {
	name: "terminal-card",
	props: {
		id: String,
		label: String,
		initWsUrl: String,
		// A command line to type and run automatically once the shell
		// connects (e.g. opening a Terminal from Settings to run
		// `rclone authorize`) - only fired once, on the very first connect.
		initCommand: String
	},
	data() {
		return {
			state: true,
			connected: false,
			connecting: false,
			ended: false,
			endReason: "",
			initCommandSent: false,
			fontSize: Math.min(FONT_MAX, Math.max(FONT_MIN, readNumber(FONT_KEY, FONT_DEFAULT))),
			copyOnSelect: readNumber(COPY_ON_SELECT_KEY, 0) === 1,
			ctrlArmed: false,
			coarsePointer: typeof window !== 'undefined' && window.matchMedia ? window.matchMedia('(pointer: coarse)').matches : false,
		}
	},
	computed: {
		showKeyBar() {
			return this.coarsePointer || this.$store.state.device === 'mobile'
		}
	},
	mounted() {
		this.socket = null
		this.lastSize = { cols: 0, rows: 0 }
		this.createTerm()
		this.connect()

		this.resizeObserver = new ResizeObserver(() => this.onWindowResize())
		this.resizeObserver.observe(this.$refs.terminalRoot)
		window.addEventListener('resize', this.onWindowResize)
	},
	beforeDestroy() {
		this.destroying = true
		this.closeSocket()
		if (this.term) {
			this.term.dispose()
			this.term = null
		}
		window.removeEventListener('resize', this.onWindowResize)
		if (this.resizeObserver) {
			this.resizeObserver.disconnect()
		}
		cancelAnimationFrame(this.fitFrame)
	},

	methods: {
		// The terminal is created once and kept for the component's life:
		// a closed session leaves its output on screen and a reconnect
		// continues below it.
		createTerm() {
			const term = new Terminal({
				fontSize: this.fontSize,
				lineHeight: 1.2,
				fontFamily: '"Fira Code", "JetBrains Mono", "Cascadia Code", Menlo, Monaco, Consolas, "Courier New", monospace',
				cursorStyle: 'block',
				cursorBlink: true,
				allowProposedApi: true,
				scrollback: 10000,
				tabStopWidth: 4,
				windowsMode: false,
				theme: {
					background: '#18181b',
					foreground: '#f4f4f5',
					cursor: '#38bdf8',
					cursorAccent: '#18181b',
					selection: 'rgba(56, 189, 248, 0.35)',
					black: '#18181b',
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
			// Links are only "live" with a modifier held (VS Code's convention)
			// so a plain click still places the cursor / starts a selection.
			term.loadAddon(new WebLinksAddon((event, uri) => {
				if (event.ctrlKey || event.metaKey) {
					window.open(uri, '_blank', 'noopener,noreferrer')
				}
			}))
			term.attachCustomKeyEventHandler(this.onKeyEvent)
			term.onData(data => this.sendInput(data))
			term.onBinary(data => this.sendBinary(data))
			term.onSelectionChange(() => {
				if (!this.copyOnSelect) return
				const text = term.getSelection()
				if (text) this.writeClipboard(text)
			})
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

		// The URL carries the fitted size so the pty starts at the right size
		// (no first-screen reflow); initWsUrl (container console) is given
		// with placeholder cols/rows, which are replaced here.
		buildUrl() {
			const cols = Math.max(this.term ? this.term.cols : 0, 20)
			const rows = Math.max(this.term ? this.term.rows : 0, 5)
			if (this.initWsUrl) {
				try {
					const u = new URL(this.initWsUrl)
					u.searchParams.set('cols', cols)
					u.searchParams.set('rows', rows)
					// A reconnect after a token refresh must not reuse the old one.
					if (u.searchParams.has('token') && this.$store.state.access_token) {
						u.searchParams.set('token', this.$store.state.access_token)
					}
					return u.toString()
				} catch (e) {
					return this.initWsUrl
				}
			}
			const query = { token: this.$store.state.access_token, cols, rows }
			return `${this.$wsProtocol}//${this.$baseURL}/v1/sys/wsterm?${qs.stringify(query)}`
		},

		connect() {
			this.closeSocket()
			this.ended = false
			this.endReason = ""
			this.connecting = true
			this.connected = false
			this.fit()

			let socket
			try {
				socket = new WebSocket(this.buildUrl())
			} catch (e) {
				this.onSessionEnd(this.$t('Failed to establish terminal connection'))
				return
			}
			socket.binaryType = 'arraybuffer'
			this.socket = socket
			socket.onopen = () => {
				if (socket !== this.socket) return
				this.connecting = false
				this.connected = true
				this.sendResize(true)
				this.startKeepalive()
				if (this.state && this.term) this.term.focus()
				this.runInitCommand()
			}
			socket.onmessage = (ev) => {
				if (socket !== this.socket || !this.term) return
				this.term.write(typeof ev.data === 'string' ? ev.data : new Uint8Array(ev.data))
			}
			socket.onerror = () => {
				if (socket !== this.socket) return
				this.errored = true
			}
			socket.onclose = (ev) => {
				if (socket !== this.socket || this.destroying) return
				const wasConnected = this.connected
				let reason = ""
				if (!wasConnected) {
					reason = this.$t('Failed to establish terminal connection')
				} else if (ev && ev.reason) {
					reason = ev.reason
				}
				this.onSessionEnd(reason)
			}
		},

		onSessionEnd(reason) {
			this.stopKeepalive()
			this.socket = null
			this.connected = false
			this.connecting = false
			this.ended = true
			this.endReason = reason
			this.errored = false
			if (this.term) {
				this.term.write('\r\n\x1b[2m[' + this.$t('session ended') + ']\x1b[0m\r\n')
			}
		},

		reconnect() {
			if (this.term) this.term.write('\r\n')
			this.connect()
		},

		closeSocket() {
			this.stopKeepalive()
			const s = this.socket
			this.socket = null
			if (s) {
				s.onopen = s.onmessage = s.onerror = s.onclose = null
				try { s.close() } catch (e) { /* already closed */ }
			}
		},

		// --- wire protocol -------------------------------------------------
		// Framing v2 (services/common/utils/wsterm): input goes out as
		// BINARY frames and is never parsed by the server; control messages
		// are TEXT frames starting with NUL + JSON, so nothing a user types
		// or pastes can be mistaken for a resize.
		sendInput(data) {
			if (!data) return
			if (this.ctrlArmed) {
				data = this.applyCtrl(data)
			}
			this.sendRaw(data)
		},

		sendBinary(data) {
			if (!this.socket || this.socket.readyState !== WebSocket.OPEN) return
			const buf = new Uint8Array(data.length)
			for (let i = 0; i < data.length; i++) buf[i] = data.charCodeAt(i) & 0xff
			this.socket.send(buf)
		},

		sendRaw(data) {
			if (this.socket && this.socket.readyState === WebSocket.OPEN) {
				this.socket.send(textEncoder.encode(data))
			}
		},

		sendControl(msg) {
			if (this.socket && this.socket.readyState === WebSocket.OPEN) {
				this.socket.send('\u0000' + JSON.stringify(msg))
			}
		},

		sendResize(force) {
			if (!this.term) return
			const { cols, rows } = this.term
			if (!cols || !rows) return
			if (!force && cols === this.lastSize.cols && rows === this.lastSize.rows) return
			if (!this.socket || this.socket.readyState !== WebSocket.OPEN) return
			this.lastSize = { cols, rows }
			this.sendControl({ type: 'resize', cols, rows })
		},

		// A browser can't send ping frames; a control message the server
		// doesn't act on keeps idle-timeout proxies from cutting the
		// connection without touching the pty.
		startKeepalive() {
			this.stopKeepalive()
			this.keepaliveTimer = setInterval(() => this.sendControl({ type: 'ping' }), KEEPALIVE_MS)
		},

		stopKeepalive() {
			if (this.keepaliveTimer) {
				clearInterval(this.keepaliveTimer)
				this.keepaliveTimer = null
			}
		},

		// --- keyboard / clipboard -----------------------------------------
		onKeyEvent(e) {
			if (e.type !== 'keydown') return true
			const mod = e.ctrlKey || e.metaKey
			if (mod && e.shiftKey && (e.key === 'C' || e.key === 'c')) {
				const text = this.term && this.term.getSelection()
				if (text) this.writeClipboard(text)
				e.preventDefault()
				return false
			}
			if (mod && e.shiftKey && (e.key === 'V' || e.key === 'v')) {
				e.preventDefault()
				this.pasteClipboard()
				return false
			}
			if (mod && !e.shiftKey && !e.altKey && (e.key === '=' || e.key === '+')) {
				e.preventDefault()
				this.zoom(1)
				return false
			}
			if (mod && !e.shiftKey && !e.altKey && e.key === '-') {
				e.preventDefault()
				this.zoom(-1)
				return false
			}
			if (mod && !e.shiftKey && !e.altKey && e.key === '0') {
				e.preventDefault()
				this.zoom(0)
				return false
			}
			return true
		},

		writeClipboard(text) {
			if (navigator.clipboard && navigator.clipboard.writeText) {
				navigator.clipboard.writeText(text).catch(() => this.legacyCopy(text))
			} else {
				this.legacyCopy(text)
			}
		},

		legacyCopy(text) {
			const ta = document.createElement('textarea')
			ta.value = text
			ta.setAttribute('readonly', '')
			ta.style.position = 'fixed'
			ta.style.opacity = '0'
			document.body.appendChild(ta)
			ta.select()
			try { document.execCommand('copy') } catch (e) { /* unsupported */ }
			document.body.removeChild(ta)
			if (this.term) this.term.focus()
		},

		pasteClipboard() {
			if (!navigator.clipboard || !navigator.clipboard.readText) {
				this.$buefy.toast.open({
					message: this.$t('Paste is blocked here - use the browser menu or Shift+Insert'),
					type: 'is-warning',
					position: 'is-top',
					duration: 3000
				})
				return
			}
			navigator.clipboard.readText().then(text => {
				if (text && this.term) this.term.paste(text)
			}).catch(() => {
				this.$buefy.toast.open({
					message: this.$t('Paste is blocked here - use the browser menu or Shift+Insert'),
					type: 'is-warning',
					position: 'is-top',
					duration: 3000
				})
			})
		},

		// delta: +1 / -1, or 0 to reset.
		zoom(delta) {
			const next = delta === 0 ? FONT_DEFAULT : Math.min(FONT_MAX, Math.max(FONT_MIN, this.fontSize + delta))
			this.setFontSize(next)
		},

		setFontSize(size) {
			this.fontSize = size
			try { localStorage.setItem(FONT_KEY, String(size)) } catch (e) { /* private mode */ }
			if (this.term) {
				if (typeof this.term.setOption === 'function') this.term.setOption('fontSize', size)
				else this.term.options.fontSize = size
				this.onWindowResize()
			}
		},

		setCopyOnSelect(on) {
			this.copyOnSelect = !!on
			try { localStorage.setItem(COPY_ON_SELECT_KEY, on ? '1' : '0') } catch (e) { /* private mode */ }
		},

		// --- phone key bar ----------------------------------------------------
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
			if (data.length !== 1) return data
			const c = data.toUpperCase().charCodeAt(0)
			// Ctrl+@..Ctrl+_ (A-Z, [, \, ], ^, _) map to 0x00-0x1f.
			if (c >= 64 && c <= 95) return String.fromCharCode(c - 64)
			if (data === ' ') return '\u0000'
			if (data === '?') return '\u007f'
			return data
		},

		// Fired once per component instance (not on reconnects) - a short
		// delay gives the shell a moment to print its prompt first so the
		// typed command doesn't land mid-banner.
		runInitCommand() {
			if (!this.initCommand || this.initCommandSent) return
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

		active(state) {
			this.state = state
			if (state) {
				this.onWindowResize()
				if (this.term) {
					this.$nextTick(() => this.term && this.term.focus())
				}
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
	background: #18181b;
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

.terminal-ended {
	position: absolute;
	left: 50%;
	bottom: var(--space-4);
	transform: translateX(-50%);
	display: flex;
	align-items: center;
	gap: var(--space-3);
	max-width: calc(100% - 2 * var(--space-4));
	padding: var(--space-2) var(--space-3);
	border-radius: var(--radius-control);
	background: #27272a;
	border: 1px solid rgba(255, 255, 255, 0.14);
	box-shadow: var(--shadow-xl);
	color: #f4f4f5;
	font-size: var(--font-xs);
	z-index: 5;

	.terminal-ended-text {
		min-width: 0;
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
	background: #1d4ed8;
	color: #fff;
	font-size: var(--font-xs);
	font-weight: 600;
	cursor: pointer;

	&:hover {
		background: #1e40af;
	}

	&:disabled {
		opacity: 0.6;
		cursor: default;
	}

	&:focus-visible {
		outline: 2px solid #93c5fd;
		outline-offset: 2px;
	}
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
		background: #27272a;
		color: #f4f4f5;
		font-size: var(--font-xs);
		cursor: pointer;

		&.is-on {
			background: #1d4ed8;
			border-color: #1d4ed8;
		}

		&:focus-visible {
			outline: 2px solid #93c5fd;
			outline-offset: 1px;
		}
	}
}
</style>
