<template>
	<div ref="terminalRoot" class="terminal-instance is-flex is-align-items-center is-justify-content-center">
		<div v-if="connectError" class="card card-shadow mb-6">
			<div class="card-content">
				<div class="content">
					<b-notification :closable="false" aria-close-label="Close notification" role="alert" type="is-danger">
						{{ connectError }}
					</b-notification>
					<div class="buttons mt-5">
						<b-button expanded rounded type="is-primary" @click="connect">{{ $t('Reconnect') }}</b-button>
					</div>
				</div>
			</div>
		</div>

		<div v-else ref="xtermEl" class="xterm"></div>
	</div>
</template>

<script>
import qs from 'qs'
import 'xterm/css/xterm.css'
import { Terminal } from 'xterm'
import { FitAddon } from 'xterm-addon-fit'
import { AttachAddon } from 'xterm-addon-attach'
import { WebLinksAddon } from 'xterm-addon-web-links'

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
			term: null,
			fitAddon: null,
			rows: 32,
			cols: 100,
			state: true,
			isVaild: false,
			wsUrl: "",
			connectError: "",
			initCommandSent: false,
		}
	},
	computed: {
		buttonSzie() {
			return this.$store.state.device == "mobile" ? 'is-small' : ''
		}
	},
	mounted() {
		this.fitAddon = new FitAddon()
		this.connect()

		this.resizeObserver = new ResizeObserver(() => this.onWindowResize())
		if (this.$refs.terminalRoot) {
			this.resizeObserver.observe(this.$refs.terminalRoot)
		}
	},
	beforeDestroy() {
		if (this.socket) {
			this.socket.close()
		}
		if (this.term) {
			this.term.dispose()
			this.term = null
		}
		window.removeEventListener('resize', this.onWindowResize)
		if (this.resizeObserver) {
			this.resizeObserver.disconnect()
		}
	},

	methods: {
		connect() {
			this.connectError = ""
			this.$messageBus('terminallogs_connect')
			const query = {
				token: this.$store.state.access_token,
				cols: Math.max(parseInt(this.cols) || 120, 20),
				rows: Math.max(parseInt(this.rows) || 32, 10),
			}
			this.wsUrl = this.initWsUrl || `${this.$wsProtocol}//${this.$baseURL}/v1/sys/wsterm?${qs.stringify(query)}`
			this.isVaild = true
			this.initSocket()
		},
		initTerm() {
			if (this.term) {
				this.term.dispose()
				this.term = null
			}

			const term = new Terminal({
				fontSize: 13,
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
					selectionBackground: 'rgba(56, 189, 248, 0.35)',
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

			const attachAddon = new AttachAddon(this.socket)
			term.loadAddon(attachAddon)
			term.loadAddon(this.fitAddon)
			// Links are only "live" with a modifier held (matches VS Code's
			// integrated terminal convention) so a plain click still just
			// places the cursor / starts a text selection like normal.
			term.loadAddon(new WebLinksAddon((event, uri) => {
				if (event.ctrlKey || event.metaKey) {
					window.open(uri, '_blank', 'noopener,noreferrer')
				}
			}))

			term.open(this.$refs.xtermEl)
			this.$nextTick(() => {
				try {
					this.fitAddon.fit()
					term.focus()
					if (this.socket && this.socket.readyState === WebSocket.OPEN) {
						this.socket.send(JSON.stringify({
							type: "resize",
							cols: term.cols,
							rows: term.rows
						}))
					}
				} catch (e) {
					console.error("fit error:", e)
				}
				this.runInitCommand()
			})

			this.term = term
			window.addEventListener('resize', this.onWindowResize)
		},
		initSocket() {
			this.socket = new WebSocket(this.wsUrl)
			this.socket.binaryType = 'arraybuffer'
			this.socketOnClose()
			this.socketOnOpen()
			this.socketOnError()
		},
		socketOnOpen() {
			this.socket.onopen = () => {
				this.initTerm()
			}
		},
		socketOnClose() {
			this.socket.onclose = () => {
				if (this.term) {
					this.term.dispose()
					this.term = null
				}
				window.removeEventListener('resize', this.onWindowResize)
				this.isVaild = false
				this.connectError = this.$t('Terminal session ended')
			}
		},
		socketOnError() {
			this.socket.onerror = () => {
				this.isVaild = false
				this.connectError = this.$t('Failed to establish terminal connection')
			}
		},
		// Fired once per component instance (not on reconnects) - a short
		// delay gives the shell a moment to print its prompt first so the
		// typed command doesn't land mid-banner.
		runInitCommand() {
			if (!this.initCommand || this.initCommandSent) return
			this.initCommandSent = true
			setTimeout(() => {
				if (this.socket && this.socket.readyState === WebSocket.OPEN) {
					this.socket.send(this.initCommand + '\r')
				}
			}, 400)
		},
		onWindowResize() {
			if (!this.isVaild || !this.term || !this.fitAddon) {
				return false
			}
			this.$nextTick(() => {
				try {
					this.fitAddon.fit()
					if (this.socket && this.socket.readyState === WebSocket.OPEN) {
						this.socket.send(JSON.stringify({
							type: "resize",
							cols: this.term.cols,
							rows: this.term.rows
						}))
					}
				} catch (e) {
					console.log("resize error:", e.message)
				}
			})
		},
		active(state) {
			this.state = state
			if (state) {
				this.onWindowResize()
				if (this.term) {
					this.$nextTick(() => this.term.focus())
				}
			}
		}
	}
}
</script>

<style lang="scss" scoped>
.terminal-instance {
	width: 100%;
	height: 100%;
	background: #18181b;
	padding: 4px 6px;
	box-sizing: border-box;
	overflow: hidden;

	.card {
		.card-content {
			padding: 2.5rem;
			width: 25rem;
		}

		&.card-shadow {
			box-shadow: 0px 40px 80px rgba(0, 0, 0, 0.4) !important;
			border-radius: 8px;
			background: #27272a;
			color: #f4f4f5;
		}
	}
}

.xterm {
	width: 100%;
	height: 100%;

	::v-deep .xterm-viewport {
		overflow-y: auto !important;
	}
}
</style>
