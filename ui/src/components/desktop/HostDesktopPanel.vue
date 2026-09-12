<!-- src/components/desktop/HostDesktopPanel.vue -->
<!--
	Host PC / Server Desktop Console.
	Streams the host machine's physical / X11 desktop display (:0) directly
	into the NivaroOS desktop window or standalone browser tab via noVNC (RFB).
	Includes keyboard/mouse controls, special key shortcuts, clipboard sync,
	scaling modes, and one-click "Open in New Tab".
-->
<template>
	<div class="host-desktop-panel">
		<!-- Main Console Toolbar -->
		<div class="desktop-console-toolbar">
			<!-- Identity / Status -->
			<div class="toolbar-identity">
				<b-icon icon="monitor-dashboard" custom-size="mdi-18px" class="monitor-icon"></b-icon>
				<span class="toolbar-title">{{ $t('Host Desktop') }}</span>
				<span class="status-pill" :class="'is-' + status">
					<span class="status-dot"></span>
					{{ statusText }}
				</span>
			</div>

			<div class="toolbar-actions">
				<!-- Segment 1: Input & Virtual Keyboard -->
				<div class="toolbar-group">
					<button
						type="button"
						class="toolbar-btn icon-only-btn"
						:class="{ active: keyboardOpen }"
						:title="$t('On-Screen Virtual Keyboard')"
						@click="keyboardOpen = !keyboardOpen"
					>
						<b-icon icon="keyboard-outline" custom-size="mdi-16px"></b-icon>
					</button>

					<!-- Keys Menu Dropdown -->
					<div ref="keysMenuWrapper" class="menu-wrapper">
						<button
							type="button"
							class="toolbar-btn"
							:title="$t('Send Key Combinations')"
							@click="keysMenuOpen = !keysMenuOpen"
						>
							<b-icon icon="keyboard-settings-outline" custom-size="mdi-16px"></b-icon>
							<span>{{ $t('Keys') }}</span>
							<b-icon icon="chevron-down" custom-size="mdi-14px"></b-icon>
						</button>
						<div v-if="keysMenuOpen" class="dropdown-popover keys-menu">
							<button type="button" class="popover-item" @click="sendCtrlAltDel(); keysMenuOpen = false">
								<b-icon icon="apple-keyboard-control" custom-size="mdi-16px"></b-icon>
								<span>Ctrl + Alt + Del</span>
							</button>
							<button type="button" class="popover-item" @click="sendWinKey(); keysMenuOpen = false">
								<b-icon icon="microsoft-windows" custom-size="mdi-16px"></b-icon>
								<span>{{ $t('Windows Key') }}</span>
							</button>
							<button type="button" class="popover-item" @click="sendAltTab(); keysMenuOpen = false">
								<b-icon icon="tab" custom-size="mdi-16px"></b-icon>
								<span>Alt + Tab</span>
							</button>
							<button type="button" class="popover-item" @click="sendCtrlShiftEsc(); keysMenuOpen = false">
								<b-icon icon="chart-line" custom-size="mdi-16px"></b-icon>
								<span>Ctrl + Shift + Esc</span>
							</button>
							<button type="button" class="popover-item" @click="sendAltF4(); keysMenuOpen = false">
								<b-icon icon="close-box-outline" custom-size="mdi-16px"></b-icon>
								<span>Alt + F4</span>
							</button>
						</div>
					</div>

					<!-- Paste Clipboard -->
					<button
						type="button"
						class="toolbar-btn icon-only-btn"
						:title="$t('Paste text into Host Desktop')"
						@click="pasteClipboard"
					>
						<b-icon icon="content-paste" custom-size="mdi-16px"></b-icon>
					</button>
				</div>

				<div class="toolbar-divider"></div>

				<!-- Segment 2: Viewport & Quality Controls -->
				<div class="toolbar-group">
					<!-- Scale Toggle -->
					<button
						type="button"
						class="toolbar-btn"
						:title="scaleToFit ? $t('Show actual 1:1 resolution') : $t('Fit to window')"
						@click="toggleScale"
					>
						<b-icon :icon="scaleToFit ? 'fit-to-page-outline' : 'aspect-ratio'" custom-size="mdi-16px"></b-icon>
						<span>{{ scaleToFit ? $t('Fit') : $t('1:1') }}</span>
					</button>

					<!-- Quality Preset Dropdown -->
					<div ref="qualityMenuWrapper" class="menu-wrapper">
						<button
							type="button"
							class="toolbar-btn"
							:title="$t('Streaming Quality Mode')"
							@click="qualityMenuOpen = !qualityMenuOpen"
						>
							<b-icon icon="speedometer" custom-size="mdi-16px"></b-icon>
							<span>{{ qualityLabel }}</span>
							<b-icon icon="chevron-down" custom-size="mdi-14px"></b-icon>
						</button>
						<div v-if="qualityMenuOpen" class="dropdown-popover quality-menu">
							<button
								v-for="q in qualityOptions"
								:key="q.mode"
								type="button"
								class="popover-item quality-item"
								:class="{ active: qualityMode === q.mode }"
								@click="setQualityMode(q.mode)"
							>
								<b-icon :icon="q.icon" custom-size="mdi-16px"></b-icon>
								<div class="quality-text">
									<div class="quality-title">{{ q.label }}</div>
									<div class="quality-desc">{{ q.desc }}</div>
								</div>
								<b-icon v-if="qualityMode === q.mode" icon="check" custom-size="mdi-14px" class="quality-check"></b-icon>
							</button>
						</div>
					</div>

					<!-- Reconnect Button -->
					<button
						v-if="status === 'disconnected'"
						type="button"
						class="toolbar-btn reconnect-btn"
						:title="$t('Reconnect to Host Desktop')"
						@click="connect"
					>
						<b-icon icon="refresh" custom-size="mdi-16px"></b-icon>
						<span>{{ $t('Reconnect') }}</span>
					</button>

					<!-- Open in New Tab Button -->
					<button
						type="button"
						class="toolbar-btn new-tab-btn"
						:title="$t('Open Host Desktop in New Tab / Window')"
						@click="openInNewTab"
					>
						<b-icon icon="open-in-new" custom-size="mdi-16px"></b-icon>
						<span>{{ $t('New Tab') }}</span>
					</button>

					<!-- Fullscreen -->
					<button
						type="button"
						class="toolbar-btn icon-only-btn"
						:title="$t('Fullscreen')"
						@click="toggleFullscreen"
					>
						<b-icon :icon="isFullscreen ? 'fullscreen-exit' : 'fullscreen'" custom-size="mdi-16px"></b-icon>
					</button>
				</div>
			</div>
		</div>

		<!-- Main Screen Canvas Area -->
		<div
			ref="screenWrapper"
			class="screen-wrapper"
			tabindex="0"
			@click="focusCanvas"
		>
			<div ref="screen" class="vnc-canvas"></div>

			<!-- Connecting / Disconnected Overlay -->
			<div v-if="status !== 'connected'" class="screen-state-overlay">
				<div class="state-card">
					<b-icon
						:icon="status === 'connecting' ? 'loading' : 'alert-circle-outline'"
						custom-size="mdi-48px"
						:class="{ 'spin-icon': status === 'connecting', 'error-icon': status === 'disconnected' }"
					></b-icon>
					<h3 class="state-title">
						{{ status === 'connecting' ? $t('Connecting to Host Desktop...') : $t('Disconnected from Host Desktop') }}
					</h3>
					<p class="state-sub">
						{{ status === 'connecting' ? $t('Establishing direct display stream to physical session :0') : $t('Display session ended or host service restarted.') }}
					</p>
					<button
						v-if="status === 'disconnected'"
						type="button"
						class="action-reconnect-btn"
						@click="connect"
					>
						<b-icon icon="refresh" custom-size="mdi-16px"></b-icon>
						<span>{{ $t('Reconnect Now') }}</span>
					</button>
				</div>
			</div>
		</div>

		<!-- Virtual On-Screen Keyboard Drawer -->
		<div v-if="keyboardOpen" class="virtual-keyboard-drawer">
			<div class="drawer-header">
				<div class="drawer-title">
					<b-icon icon="keyboard-outline" custom-size="mdi-16px"></b-icon>
					<span>{{ $t('Virtual Keyboard') }}</span>
				</div>
				<button type="button" class="drawer-close" @click="keyboardOpen = false">
					<b-icon icon="close" custom-size="mdi-14px"></b-icon>
				</button>
			</div>
			<div class="keyboard-body">
				<!-- Row 1: Function Keys -->
				<div class="kb-row">
					<button type="button" class="kb-key kb-key-fn" @click="sendSpecialKey('Escape', 'Escape')">Esc</button>
					<button v-for="n in 12" :key="'f'+n" type="button" class="kb-key kb-key-fn" @click="sendSpecialKey('F'+n, 'F'+n)">F{{ n }}</button>
					<button type="button" class="kb-key kb-key-fn" @click="sendSpecialKey('Delete', 'Delete')">Del</button>
				</div>
				<!-- Row 2: Numbers -->
				<div class="kb-row">
					<button v-for="k in numberRow" :key="k" type="button" class="kb-key" @click="sendCharKey(k)">{{ k }}</button>
					<button type="button" class="kb-key kb-key-action" @click="sendSpecialKey('Backspace', 'Backspace')">⌫</button>
				</div>
				<!-- Row 3: QWERTY -->
				<div class="kb-row">
					<button type="button" class="kb-key kb-key-action" @click="sendSpecialKey('Tab', 'Tab')">Tab</button>
					<button v-for="k in ['q','w','e','r','t','y','u','i','o','p']" :key="k" type="button" class="kb-key" @click="sendCharKey(k)">{{ isShiftActive ? k.toUpperCase() : k }}</button>
				</div>
				<!-- Row 4: ASDF -->
				<div class="kb-row">
					<button type="button" class="kb-key kb-key-action" :class="{ active: isCapsActive }" @click="isCapsActive = !isCapsActive">Caps</button>
					<button v-for="k in ['a','s','d','f','g','h','j','k','l']" :key="k" type="button" class="kb-key" @click="sendCharKey(k)">{{ (isShiftActive || isCapsActive) ? k.toUpperCase() : k }}</button>
					<button type="button" class="kb-key kb-key-action kb-key-enter" @click="sendSpecialKey('Enter', 'Enter')">Enter</button>
				</div>
				<!-- Row 5: ZXCV -->
				<div class="kb-row">
					<button type="button" class="kb-key kb-key-action" :class="{ active: isShiftActive }" @click="isShiftActive = !isShiftActive">Shift</button>
					<button v-for="k in ['z','x','c','v','b','n','m']" :key="k" type="button" class="kb-key" @click="sendCharKey(k)">{{ (isShiftActive || isCapsActive) ? k.toUpperCase() : k }}</button>
					<button type="button" class="kb-key kb-key-arrow" @click="sendSpecialKey('ArrowUp', 'ArrowUp')">▲</button>
				</div>
				<!-- Row 6: Modifiers & Space -->
				<div class="kb-row">
					<button type="button" class="kb-key kb-key-action" :class="{ active: isCtrlActive }" @click="toggleModifier('Control', 'isCtrlActive')">Ctrl</button>
					<button type="button" class="kb-key kb-key-action" :class="{ active: isAltActive }" @click="toggleModifier('Alt', 'isAltActive')">Alt</button>
					<button type="button" class="kb-key kb-key-action" @click="sendWinKey">Win</button>
					<button type="button" class="kb-key kb-key-space" @click="sendCharKey(' ')">Space</button>
					<button type="button" class="kb-key kb-key-arrow" @click="sendSpecialKey('ArrowLeft', 'ArrowLeft')">◀</button>
					<button type="button" class="kb-key kb-key-arrow" @click="sendSpecialKey('ArrowDown', 'ArrowDown')">▼</button>
					<button type="button" class="kb-key kb-key-arrow" @click="sendSpecialKey('ArrowRight', 'ArrowRight')">▶</button>
				</div>
			</div>
		</div>
	</div>
</template>

<script>
import RFB from '@novnc/novnc'

const SPECIAL_KEYSYMS = {
	Backspace: 0xff08,
	Tab: 0xff09,
	Enter: 0xff0d,
	Escape: 0xff1b,
	CapsLock: 0xffe5,
	Shift: 0xffe1,
	Control: 0xffe3,
	Alt: 0xffe9,
	Super: 0xffeb,
	ArrowLeft: 0xff51,
	ArrowUp: 0xff52,
	ArrowRight: 0xff53,
	ArrowDown: 0xff54,
	Delete: 0xffff,
	Home: 0xff50,
	End: 0xff57,
	Insert: 0xff63,
	PageUp: 0xff55,
	PageDown: 0xff56,
	F1: 0xffbe,
	F2: 0xffbf,
	F3: 0xffc0,
	F4: 0xffc1,
	F5: 0xffc2,
	F6: 0xffc3,
	F7: 0xffc4,
	F8: 0xffc5,
	F9: 0xffc6,
	F10: 0xffc7,
	F11: 0xffc8,
	F12: 0xffc9,
}

const QUALITY_PRESETS = {
	auto: { qualityLevel: 7, compressionLevel: 2, label: 'Auto Balanced' },
	smooth: { qualityLevel: 5, compressionLevel: 1, label: 'Smooth / Low Latency' },
	crisp: { qualityLevel: 9, compressionLevel: 4, label: 'High Fidelity (Crisp)' },
	lowbw: { qualityLevel: 2, compressionLevel: 9, label: 'Low Bandwidth' },
}

export default {
	name: 'host-desktop-panel',
	props: {
		showClose: { type: Boolean, default: false },
	},
	data() {
		return {
			rfb: null,
			status: 'disconnected', // connecting, connected, disconnected
			scaleToFit: true,
			isFullscreen: false,
			keyboardOpen: false,
			keysMenuOpen: false,
			qualityMenuOpen: false,
			qualityMode: 'auto',
			isShiftActive: false,
			isCapsActive: false,
			isCtrlActive: false,
			isAltActive: false,
			qualityOptions: [
				{ mode: 'auto', label: 'Auto Balanced', desc: 'Optimal framerate and clarity', icon: 'speedometer' },
				{ mode: 'smooth', label: 'Smooth / Low Latency', desc: 'Fastest response time', icon: 'lightning-bolt' },
				{ mode: 'crisp', label: 'High Fidelity (Crisp)', desc: 'Sharpest text & detail', icon: 'image-filter-hdr' },
				{ mode: 'lowbw', label: 'Low Bandwidth', desc: 'Conserves network data', icon: 'wifi-strength-1' },
			],
			numberRow: ['1','2','3','4','5','6','7','8','9','0','-','='],
		}
	},
	computed: {
		statusText() {
			if (this.status === 'connected') return this.$t('Connected')
			if (this.status === 'connecting') return this.$t('Connecting...')
			return this.$t('Disconnected')
		},
		qualityLabel() {
			return QUALITY_PRESETS[this.qualityMode]?.label || this.$t('Auto')
		},
	},
	mounted() {
		this.connect()
		document.addEventListener('mousedown', this.onOutsideClick)
		document.addEventListener('fullscreenchange', this.onFullscreenChange)
		this.resizeObserver = new ResizeObserver(() => {
			if (this.rfb && this.scaleToFit) {
				this.rfb.scaleViewport = true
			}
		})
		if (this.$refs.screenWrapper) {
			this.resizeObserver.observe(this.$refs.screenWrapper)
		}
	},
	beforeDestroy() {
		this.disconnect()
		document.removeEventListener('mousedown', this.onOutsideClick)
		document.removeEventListener('fullscreenchange', this.onFullscreenChange)
		if (this.resizeObserver) {
			this.resizeObserver.disconnect()
		}
	},
	methods: {
		getHostConsoleUrl() {
			const hostname = window.location.hostname || '127.0.0.1'
			const wsProto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
			// Primary port 28641 through NivaroOS vm-sidecar /host/console route
			return `${wsProto}//${hostname}:28641/host/console`
		},
		getFallbackConsoleUrl() {
			const hostname = window.location.hostname || '127.0.0.1'
			const wsProto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
			// Direct websockify port
			return `${wsProto}//${hostname}:28642`
		},
		connect() {
			this.disconnect()
			this.status = 'connecting'

			const primaryUrl = this.getHostConsoleUrl()
			const fallbackUrl = this.getFallbackConsoleUrl()

			try {
				this.initRfb(primaryUrl, () => {
					// On error with primary, attempt fallback
					console.warn('Primary host console connection failed, trying websockify fallback...')
					this.initRfb(fallbackUrl, () => {
						this.status = 'disconnected'
					})
				})
			} catch (e) {
				this.initRfb(fallbackUrl, () => {
					this.status = 'disconnected'
				})
			}
		},
		initRfb(url, onError) {
			if (!this.$refs.screen) return
			this.rfb = new RFB(this.$refs.screen, url)
			this.rfb.scaleViewport = this.scaleToFit
			this.rfb.clipViewport = false
			this.applyQualitySettings()

			this.rfb.addEventListener('connect', () => {
				this.status = 'connected'
				this.applyQualitySettings()
				this.focusCanvas()
			})

			this.rfb.addEventListener('disconnect', (e) => {
				if (this.status === 'connecting' && onError) {
					onError(e)
				} else {
					this.status = 'disconnected'
				}
			})

			this.rfb.addEventListener('clipboard', (e) => {
				const text = e.detail && e.detail.text
				if (text && navigator.clipboard && navigator.clipboard.writeText) {
					navigator.clipboard.writeText(text).catch(() => {})
				}
			})
		},
		disconnect() {
			if (this.rfb) {
				try {
					this.rfb.disconnect()
				} catch (e) {}
				this.rfb = null
			}
		},
		focusCanvas() {
			this.$nextTick(() => {
				if (this.$refs.screen) {
					const canvas = this.$refs.screen.querySelector('canvas')
					if (canvas) canvas.focus()
				}
			})
		},
		toggleScale() {
			this.scaleToFit = !this.scaleToFit
			if (this.rfb) {
				this.rfb.scaleViewport = this.scaleToFit
			}
		},
		applyQualitySettings() {
			if (!this.rfb) return
			const preset = QUALITY_PRESETS[this.qualityMode] || QUALITY_PRESETS.auto
			this.rfb.qualityLevel = preset.qualityLevel
			this.rfb.compressionLevel = preset.compressionLevel
		},
		setQualityMode(mode) {
			this.qualityMode = mode
			this.applyQualitySettings()
			this.qualityMenuOpen = false
		},
		toggleFullscreen() {
			if (!document.fullscreenElement) {
				const target = this.$el || document.documentElement
				target.requestFullscreen().catch(() => {})
			} else {
				document.exitFullscreen().catch(() => {})
			}
		},
		onFullscreenChange() {
			this.isFullscreen = !!document.fullscreenElement
		},
		openInNewTab() {
			// Opens the Host Desktop in a full-page standalone browser tab
			const routeUrl = this.$router.resolve({ name: 'HostDesktopStandalone' })
			window.open(routeUrl.href, '_blank')
		},
		sendCtrlAltDel() {
			if (this.rfb) {
				this.rfb.sendCtrlAltDel()
			}
		},
		sendWinKey() {
			if (!this.rfb) return
			const winKey = SPECIAL_KEYSYMS.Super
			this.rfb.sendKey(winKey, 'MetaLeft', true)
			setTimeout(() => {
				if (this.rfb) this.rfb.sendKey(winKey, 'MetaLeft', false)
			}, 60)
		},
		sendAltTab() {
			if (!this.rfb) return
			const alt = SPECIAL_KEYSYMS.Alt
			const tab = SPECIAL_KEYSYMS.Tab
			this.rfb.sendKey(alt, 'AltLeft', true)
			this.rfb.sendKey(tab, 'Tab', true)
			setTimeout(() => {
				if (this.rfb) {
					this.rfb.sendKey(tab, 'Tab', false)
					this.rfb.sendKey(alt, 'AltLeft', false)
				}
			}, 60)
		},
		sendCtrlShiftEsc() {
			if (!this.rfb) return
			const ctrl = SPECIAL_KEYSYMS.Control
			const shift = SPECIAL_KEYSYMS.Shift
			const esc = SPECIAL_KEYSYMS.Escape
			this.rfb.sendKey(ctrl, 'ControlLeft', true)
			this.rfb.sendKey(shift, 'ShiftLeft', true)
			this.rfb.sendKey(esc, 'Escape', true)
			setTimeout(() => {
				if (this.rfb) {
					this.rfb.sendKey(esc, 'Escape', false)
					this.rfb.sendKey(shift, 'ShiftLeft', false)
					this.rfb.sendKey(ctrl, 'ControlLeft', false)
				}
			}, 60)
		},
		sendAltF4() {
			if (!this.rfb) return
			const alt = SPECIAL_KEYSYMS.Alt
			const f4 = SPECIAL_KEYSYMS.F4
			this.rfb.sendKey(alt, 'AltLeft', true)
			this.rfb.sendKey(f4, 'F4', true)
			setTimeout(() => {
				if (this.rfb) {
					this.rfb.sendKey(f4, 'F4', false)
					this.rfb.sendKey(alt, 'AltLeft', false)
				}
			}, 60)
		},
		sendCharKey(char) {
			if (!this.rfb) return
			const targetChar = (this.isShiftActive || this.isCapsActive) ? char.toUpperCase() : char
			const keysym = targetChar.charCodeAt(0)
			this.rfb.sendKey(keysym, 'Key' + targetChar.toUpperCase(), true)
			setTimeout(() => {
				if (this.rfb) this.rfb.sendKey(keysym, 'Key' + targetChar.toUpperCase(), false)
			}, 30)
			if (this.isShiftActive) this.isShiftActive = false
		},
		sendSpecialKey(symName, code) {
			if (!this.rfb) return
			const keysym = SPECIAL_KEYSYMS[symName]
			if (keysym) {
				this.rfb.sendKey(keysym, code, true)
				setTimeout(() => {
					if (this.rfb) this.rfb.sendKey(keysym, code, false)
				}, 40)
			}
		},
		toggleModifier(symName, stateProp) {
			this[stateProp] = !this[stateProp]
			if (this.rfb) {
				const keysym = SPECIAL_KEYSYMS[symName]
				this.rfb.sendKey(keysym, symName, this[stateProp])
			}
		},
		async pasteClipboard() {
			if (!this.rfb) return
			let text = ''
			try {
				if (navigator.clipboard && navigator.clipboard.readText) {
					text = await navigator.clipboard.readText()
				}
			} catch (e) {}

			if (!text) {
				this.$buefy.dialog.prompt({
					title: this.$t('Paste to Host Desktop'),
					message: this.$t('Enter or paste text to send to host machine:'),
					inputAttrs: { placeholder: this.$t('Type or paste text here...') },
					trapFocus: true,
					onConfirm: val => {
						if (val) this.typeStringIntoRemote(val)
					}
				})
			} else {
				this.typeStringIntoRemote(text)
			}
		},
		typeStringIntoRemote(str) {
			if (!this.rfb || !str) return
			for (let i = 0; i < str.length; i++) {
				const ch = str[i]
				setTimeout(() => {
					if (!this.rfb) return
					if (ch === '\n') {
						this.rfb.sendKey(SPECIAL_KEYSYMS.Enter, 'Enter', true)
						this.rfb.sendKey(SPECIAL_KEYSYMS.Enter, 'Enter', false)
					} else {
						const keysym = ch.charCodeAt(0)
						this.rfb.sendKey(keysym, 'Key' + ch.toUpperCase(), true)
						this.rfb.sendKey(keysym, 'Key' + ch.toUpperCase(), false)
					}
				}, i * 15)
			}
			this.$buefy.toast.open({
				message: this.$t('Pasted text into host session'),
				type: 'is-success',
				position: 'is-top',
				duration: 2000
			})
		},
		onOutsideClick(e) {
			if (this.$refs.keysMenuWrapper && !this.$refs.keysMenuWrapper.contains(e.target)) {
				this.keysMenuOpen = false
			}
			if (this.$refs.qualityMenuWrapper && !this.$refs.qualityMenuWrapper.contains(e.target)) {
				this.qualityMenuOpen = false
			}
		}
	}
}
</script>

<style lang="scss" scoped>
.host-desktop-panel {
	display: flex;
	flex-direction: column;
	width: 100%;
	height: 100%;
	background: #09090b;
	color: #f4f4f5;
	overflow: hidden;
	position: relative;
	user-select: none;
}

/* Toolbar */
.desktop-console-toolbar {
	display: flex;
	align-items: center;
	justify-content: space-between;
	background: #141416;
	border-bottom: 1px solid rgba(255, 255, 255, 0.1);
	padding: 0.4rem 0.75rem;
	height: 42px;
	flex-shrink: 0;
	gap: 0.5rem;
}

.toolbar-identity {
	display: flex;
	align-items: center;
	gap: 0.5rem;
	font-weight: 600;
	font-size: 0.85rem;

	.monitor-icon {
		color: #38bdf8;
	}

	.toolbar-title {
		color: #f4f4f5;
		white-space: nowrap;
	}
}

.status-pill {
	display: inline-flex;
	align-items: center;
	gap: 0.35rem;
	font-size: 0.6875rem;
	font-weight: 500;
	padding: 0.15rem 0.5rem;
	border-radius: 9999px;
	text-transform: capitalize;

	.status-dot {
		width: 6px;
		height: 6px;
		border-radius: 50%;
	}

	&.is-connected {
		background: rgba(34, 197, 94, 0.15);
		color: #4ade80;
		.status-dot { background: #22c55e; }
	}

	&.is-connecting {
		background: rgba(234, 179, 8, 0.15);
		color: #facc15;
		.status-dot { background: #eab308; }
	}

	&.is-disconnected {
		background: rgba(239, 68, 68, 0.15);
		color: #f87171;
		.status-dot { background: #ef4444; }
	}
}

.toolbar-actions {
	display: flex;
	align-items: center;
	gap: 0.35rem;
}

.toolbar-group {
	display: flex;
	align-items: center;
	gap: 0.25rem;
}

.toolbar-divider {
	width: 1px;
	height: 18px;
	background: rgba(255, 255, 255, 0.12);
	margin: 0 0.25rem;
}

.toolbar-btn {
	display: inline-flex;
	align-items: center;
	gap: 0.35rem;
	background: #1e1e22;
	border: 1px solid rgba(255, 255, 255, 0.1);
	color: #d4d4d8;
	border-radius: 6px;
	padding: 0.25rem 0.55rem;
	font-size: 0.775rem;
	font-weight: 500;
	cursor: pointer;
	transition: all 0.15s ease;
	line-height: 1.2;

	&:hover {
		background: #27272a;
		color: #ffffff;
		border-color: rgba(255, 255, 255, 0.22);
	}

	&.active {
		background: #2563eb;
		color: #ffffff;
		border-color: #3b82f6;
	}

	&.icon-only-btn {
		padding: 0.25rem 0.4rem;
	}

	&.new-tab-btn {
		color: #60a5fa;
		border-color: rgba(96, 165, 250, 0.3);

		&:hover {
			background: #2563eb;
			color: #ffffff;
			border-color: #2563eb;
		}
	}

	&.reconnect-btn {
		color: #fbbf24;
		border-color: rgba(251, 191, 36, 0.3);
	}
}

/* Dropdown Menus */
.menu-wrapper {
	position: relative;
}

.dropdown-popover {
	position: absolute;
	top: calc(100% + 4px);
	right: 0;
	z-index: 1000;
	background: #18181b;
	border: 1px solid rgba(255, 255, 255, 0.14);
	border-radius: 8px;
	box-shadow: 0 16px 36px rgba(0, 0, 0, 0.8);
	padding: 0.3rem;
	min-width: 170px;
}

.popover-item {
	display: flex;
	align-items: center;
	gap: 0.5rem;
	width: 100%;
	padding: 0.4rem 0.6rem;
	border: none;
	background: transparent;
	color: #f4f4f5;
	font-size: 0.8rem;
	border-radius: 5px;
	cursor: pointer;
	text-align: left;
	transition: background 0.12s ease;

	&:hover {
		background: #2563eb;
		color: #ffffff;
	}

	&.quality-item {
		justify-content: space-between;

		.quality-text {
			flex: 1;
			margin-left: 0.35rem;
		}

		.quality-title {
			font-weight: 500;
			font-size: 0.8rem;
		}

		.quality-desc {
			font-size: 0.7rem;
			color: #a1a1aa;
		}

		&:hover .quality-desc {
			color: #e0f2fe;
		}

		&.active {
			background: rgba(37, 99, 235, 0.25);
			color: #60a5fa;
		}
	}
}

/* Screen Wrapper */
.screen-wrapper {
	flex: 1;
	position: relative;
	background: #000000;
	overflow: hidden;
	display: flex;
	align-items: center;
	justify-content: center;
	outline: none;

	.vnc-canvas {
		width: 100%;
		height: 100%;
		display: flex;
		align-items: center;
		justify-content: center;

		::v-deep canvas {
			outline: none;
			max-width: 100%;
			max-height: 100%;
		}
	}
}

.screen-state-overlay {
	position: absolute;
	inset: 0;
	background: rgba(0, 0, 0, 0.82);
	backdrop-filter: blur(8px);
	display: flex;
	align-items: center;
	justify-content: center;
	z-index: 50;
}

.state-card {
	background: #18181b;
	border: 1px solid rgba(255, 255, 255, 0.12);
	border-radius: 14px;
	padding: 2rem 2.5rem;
	text-align: center;
	max-width: 440px;
	box-shadow: 0 20px 48px rgba(0, 0, 0, 0.8);

	.spin-icon {
		color: #38bdf8;
		animation: spin 1.2s linear infinite;
	}

	.error-icon {
		color: #f87171;
	}

	.state-title {
		font-size: 1.15rem;
		font-weight: 600;
		margin: 1rem 0 0.5rem;
		color: #f4f4f5;
	}

	.state-sub {
		font-size: 0.825rem;
		color: #a1a1aa;
		line-height: 1.4;
		margin-bottom: 1.25rem;
	}

	.action-reconnect-btn {
		display: inline-flex;
		align-items: center;
		gap: 0.4rem;
		background: #2563eb;
		color: #ffffff;
		border: none;
		padding: 0.5rem 1.2rem;
		border-radius: 8px;
		font-weight: 500;
		cursor: pointer;
		transition: background 0.15s ease;

		&:hover {
			background: #1d4ed8;
		}
	}
}

@keyframes spin {
	100% { transform: rotate(360deg); }
}

/* Virtual Keyboard Drawer */
.virtual-keyboard-drawer {
	background: #141416;
	border-top: 1px solid rgba(255, 255, 255, 0.12);
	padding: 0.6rem 0.85rem;
	flex-shrink: 0;
	box-shadow: 0 -8px 24px rgba(0, 0, 0, 0.5);

	.drawer-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		margin-bottom: 0.4rem;
		color: #a1a1aa;
		font-size: 0.75rem;

		.drawer-title {
			display: flex;
			align-items: center;
			gap: 0.35rem;
		}

		.drawer-close {
			border: none;
			background: transparent;
			color: #a1a1aa;
			cursor: pointer;
			padding: 2px;
			border-radius: 4px;

			&:hover {
				color: #ffffff;
				background: rgba(255, 255, 255, 0.1);
			}
		}
	}

	.keyboard-body {
		display: flex;
		flex-direction: column;
		gap: 0.3rem;
	}

	.kb-row {
		display: flex;
		gap: 0.25rem;
		justify-content: center;
	}

	.kb-key {
		min-width: 34px;
		height: 32px;
		background: #222226;
		border: 1px solid rgba(255, 255, 255, 0.1);
		color: #f4f4f5;
		border-radius: 5px;
		font-size: 0.75rem;
		font-weight: 500;
		cursor: pointer;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		transition: all 0.1s ease;
		padding: 0 0.35rem;

		&:hover {
			background: #323238;
			border-color: rgba(255, 255, 255, 0.25);
		}

		&:active {
			background: #2563eb;
			color: #ffffff;
			transform: translateY(1px);
		}

		&.active {
			background: #2563eb;
			color: #ffffff;
			border-color: #3b82f6;
		}

		&.kb-key-fn {
			font-size: 0.6875rem;
			color: #93c5fd;
			background: #1b1f2b;
		}

		&.kb-key-action {
			background: #1a1a1e;
			color: #cbd5e1;
		}

		&.kb-key-space {
			flex: 1;
			max-width: 240px;
		}

		&.kb-key-enter {
			min-width: 54px;
		}

		&.kb-key-arrow {
			min-width: 30px;
			color: #60a5fa;
		}
	}
}
</style>
