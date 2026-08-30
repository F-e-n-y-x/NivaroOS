
<template>
	<div ref="terminalRoot" class="terminal-instance is-flex is-align-items-center is-justify-content-center">
		<div v-if="connectError" class="card card-shadow mb-6">
			<div class="card-content">
				<div class="content">
					<b-notification :closable="false" aria-close-label="Close notification" role="alert" type="is-danger">
						{{ connectError }}
					</b-notification>
					<div class="buttons mt-5">
						<b-button expanded rounded type="is-primary" @click="connect">{{ $t('Connect') }}</b-button>
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

export default {
	name: "terminal-card",
	props: {
		id: String,
		label: String,
		initWsUrl: String
	},
	data() {
		return {
			term: "",
			fitAddon: null,
			rows: 40,
			cols: 100,
			state: true,
			isVaild: false,
			wsUrl: "",
			connectError: "",
		}
	},
	computed: {
		buttonSzie() {
			return this.$store.state.device == "mobile" ? 'is-small' : ''
		}
	},
	mounted() {
		// Each instance needs its own FitAddon - it used to be a single
		// module-level singleton shared by every TerminalCard, which broke
		// as soon as more than one tab existed (fit() would only ever
		// affect whichever terminal loaded the addon most recently).
		this.fitAddon = new FitAddon();
		this.rows = this.$refs.terminalRoot.offsetHeight / 16 - 6;
		this.cols = this.$refs.terminalRoot.offsetWidth / 14;
		this.connect();

		// The desktop window this lives in can be resized independently of
		// the browser viewport (drag handles, not a real browser resize),
		// so a plain window 'resize' listener isn't enough to keep the
		// terminal sized correctly.
		this.resizeObserver = new ResizeObserver(() => this.onWindowResize())
		this.resizeObserver.observe(this.$refs.terminalRoot)
	},
	beforeDestroy() {
		if (this.socket) this.socket.close()
		if (this.term != "") this.term.dispose()
		window.removeEventListener('resize', this.onWindowResize)
		if (this.resizeObserver) this.resizeObserver.disconnect()
	},

	methods: {
		connect() {
			// No username/password prompt - the backend drops straight into
			// the local desktop user's own shell (no SSH hop), same as a
			// real desktop's built-in terminal.
			this.connectError = ""
			this.$messageBus('terminallogs_connect')
			const query = {
				token: this.$store.state.access_token,
				cols: Math.max(parseInt(this.cols) || 100, 20),
				rows: Math.max(parseInt(this.rows) || 30, 10),
			}
			this.wsUrl = this.initWsUrl || `${this.$wsProtocol}//${this.$baseURL}/v1/sys/wsterm?${qs.stringify(query)}`
			this.isVaild = true
			this.initSocket();
		},
		initTerm() {
			const term = new Terminal({
				// rendererType: 'canvas',
				fontSize: 13,
				cursorStyle: 'underline', //光标样式
				cursorBlink: true, //光标闪烁
				theme: { background: '#1E1E1E' },
				rows: parseInt(this.rows), //行数
				cols: parseInt(this.cols), // 不指定行数，自动回车后光标从下一行开始
				fontFamily: "Consolas, Monaco, monospace",
			});
			const attachAddon = new AttachAddon(this.socket);

			term.loadAddon(attachAddon);
			term.loadAddon(this.fitAddon);
			term.open(this.$refs.xtermEl);
			this.fitAddon.fit();
			term.focus();
			this.term = term
			window.addEventListener('resize', this.onWindowResize)

			this.socket.send(JSON.stringify({
				type: "resize",
				cols: this.term.cols,
				rows: this.term.rows
			}))

		},
		initSocket() {
			this.socket = new WebSocket(this.wsUrl);
			this.socketOnClose();
			this.socketOnOpen();
			this.socketOnError();

			this.socket.onmessage = (event) => {
				if (event.data == "\r\n[?2004l\rlogout\r\n") {
					this.socket.close()
					if (this.term != "") this.term.dispose()
					window.removeEventListener('resize', this.onWindowResize)
					this.isVaild = false
					this.connectError = this.$t('Terminal session ended')
				}
			}
		},
		socketOnOpen() {
			this.socket.onopen = () => {
				this.initTerm()
			}
		},
		socketOnClose() {
			this.socket.onclose = () => {
				console.log('close socket')
			}
		},
		socketOnError() {
			this.socket.onerror = () => {
				console.log('socket failure')
			}
		},
		onWindowResize() {
			if (!this.isVaild) {
				return false
			}
			this.$nextTick(() => {
				try {
					this.fitAddon.fit();
					this.socket.send(JSON.stringify({
						type: "resize",
						cols: this.term.cols,
						rows: this.term.rows
					}))
				} catch (e) {
					console.log("e", e.message);
				}
			})

		},
		getTop(e) {
			let offset = e.offsetTop;
			if (e.offsetParent != null) offset += this.getTop(e.offsetParent);
			return offset;
		},
		active(state) {
			this.state = state;
			if (state) {
				this.onWindowResize();
			}
		}
	}
}
</script>

<style lang="scss" scoped>
.terminal-instance {
	width: 100%;
	height: 100%;
	font-size: 0.75rem;
	padding: 0.5rem 0.75rem;
	box-sizing: border-box;

	.card {
		.card-content {
			padding: 2.5rem;
			width: 25rem;
		}

		&.card-shadow {
			box-shadow: 0px 40px 80px rgba(115, 120, 128, 0.25) !important;
			border-radius: 8px;
		}
	}
}

.xterm {
	width: 100%;
	height: 100%;
}
</style>
