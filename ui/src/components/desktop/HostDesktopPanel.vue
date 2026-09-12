<!-- src/components/desktop/HostDesktopPanel.vue -->
<!--
	Host PC / Server Desktop Console.
	Streams the host machine's physical / X11 desktop display (:0) directly
	into the NivaroOS desktop window or standalone browser tab via noVNC (RFB).
	Includes quick display size & resolution changer, auto-match window size,
	zoom controls, scaling modes, virtual keyboard, and one-click "New Tab".
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
				<!-- Segment 1: Quick Display Size & Resolution Changer -->
				<div class="toolbar-group">
					<div ref="displaySizeMenuWrapper" class="menu-wrapper">
						<button
							type="button"
							class="toolbar-btn display-size-btn"
							:class="{ active: displaySizeMenuOpen }"
							:title="$t('Change Server Display Resolution & Size')"
							@click="displaySizeMenuOpen = !displaySizeMenuOpen"
						>
							<b-icon icon="monitor-screenshot" custom-size="mdi-16px"></b-icon>
							<span class="display-res-badge">{{ currentResolution || '1920x1080' }}</span>
							<b-icon icon="chevron-down" custom-size="mdi-14px"></b-icon>
						</button>

						<div v-if="displaySizeMenuOpen" class="dropdown-popover display-size-menu">
							<div class="popover-section-title">
								<b-icon icon="aspect-ratio" custom-size="mdi-14px"></b-icon>
								<span>{{ $t('Display Resolution') }}</span>
							</div>

							<!-- Auto Match Window Option -->
							<button
								type="button"
								class="popover-item auto-match-item"
								:disabled="resizingHost"
								@click="matchWindowResolution"
							>
								<b-icon icon="arrow-expand-all" custom-size="mdi-16px" class="match-icon"></b-icon>
								<div class="popover-item-text">
									<div class="item-title">{{ $t('Match Current Window') }}</div>
									<div class="item-desc">{{ currentWindowEstimate }}</div>
								</div>
								<b-icon v-if="resizingHost" icon="loading" custom-class="mdi-spin" custom-size="mdi-14px"></b-icon>
							</button>

							<div class="popover-divider"></div>

							<!-- Standard Preset Resolutions -->
							<div class="resolution-list">
								<button
									v-for="r in availableResolutions"
									:key="r.width + 'x' + r.height"
									type="button"
									class="popover-item res-item"
									:class="{ active: currentResolution === `${r.width}x${r.height}` }"
									:disabled="resizingHost"
									@click="changeResolution(r.width, r.height)"
								>
									<b-icon icon="monitor" custom-size="mdi-15px"></b-icon>
									<span class="res-label">{{ r.label }}</span>
									<b-icon
										v-if="currentResolution === `${r.width}x${r.height}`"
										icon="check"
										custom-size="mdi-14px"
										class="check-icon"
									></b-icon>
								</button>
							</div>

							<div class="popover-divider"></div>

							<!-- Custom Resolution Input -->
							<div class="custom-res-form" @click.stop>
								<span class="custom-res-label">{{ $t('Custom') }}:</span>
								<input
									v-model.number="customWidth"
									type="number"
									class="custom-res-input"
									placeholder="1920"
									min="640"
									max="7680"
								/>
								<span class="custom-res-sep">×</span>
								<input
									v-model.number="customHeight"
									type="number"
									class="custom-res-input"
									placeholder="1080"
									min="480"
									max="4320"
								/>
								<button
									type="button"
									class="custom-res-apply-btn"
									:disabled="resizingHost || !customWidth || !customHeight"
									@click="applyCustomResolution"
								>
									{{ $t('Set') }}
								</button>
							</div>
						</div>
					</div>

					<!-- View Scaling & Zoom Controls -->
					<div class="zoom-controls">
						<button
							type="button"
							class="toolbar-btn icon-only-btn"
							:class="{ active: scaleMode === 'fit' }"
							:title="$t('Fit display proportionally to window')"
							@click="setScaleMode('fit')"
						>
							<b-icon icon="fit-to-page-outline" custom-size="mdi-16px"></b-icon>
						</button>

						<button
							type="button"
							class="toolbar-btn icon-only-btn"
							:class="{ active: scaleMode === 'actual' }"
							:title="$t('Actual 1:1 pixel size (100%)')"
							@click="setScaleMode('actual')"
						>
							<b-icon icon="aspect-ratio" custom-size="mdi-16px"></b-icon>
						</button>

						<button
							type="button"
							class="toolbar-btn icon-only-btn zoom-btn"
							:disabled="zoomPercent <= 25"
							:title="$t('Zoom Out')"
							@click="adjustZoom(-25)"
						>
							<b-icon icon="minus" custom-size="mdi-14px"></b-icon>
						</button>

						<span class="zoom-badge" :title="$t('Current Display Scale')">{{ zoomPercent }}%</span>

						<button
							type="button"
							class="toolbar-btn icon-only-btn zoom-btn"
							:disabled="zoomPercent >= 300"
							:title="$t('Zoom In')"
							@click="adjustZoom(25)"
						>
							<b-icon icon="plus" custom-size="mdi-14px"></b-icon>
						</button>
					</div>
				</div>

				<div class="toolbar-divider"></div>

				<!-- Segment 2: Input Controls & Keyboard -->
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

				<!-- Segment 3: Quality & Window Controls -->
				<div class="toolbar-group">
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
								<div class="popover-item-text">
									<div class="item-title">{{ q.label }}</div>
									<div class="item-desc">{{ q.desc }}</div>
								</div>
								<b-icon v-if="qualityMode === q.mode" icon="check" custom-size="mdi-14px" class="check-icon"></b-icon>
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

		<!-- Main Screen Viewport Container -->
		<div
			ref="screenWrapper"
			class="screen-wrapper"
			:class="{ 'is-fit': scaleMode === 'fit', 'is-scrollable': scaleMode !== 'fit' }"
			tabindex="0"
			@click="focusCanvas"
		>
			<div
				ref="screen"
				class="vnc-canvas-container"
				:style="canvasTransformStyle"
			></div>

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
import axios from 'axios'

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
			scaleMode: 'fit', // 'fit', 'actual', 'custom'
			zoomPercent: 100,
			isFullscreen: false,
			keyboardOpen: false,
			keysMenuOpen: false,
			qualityMenuOpen: false,
			displaySizeMenuOpen: false,
			qualityMode: 'auto',
			isShiftActive: false,
			isCapsActive: false,
			isCtrlActive: false,
			isAltActive: false,
			resizingHost: false,
			currentResolution: '1920x1080',
			currentWidth: 1920,
			currentHeight: 1080,
			customWidth: 1920,
			customHeight: 1080,
			containerWidth: 1280,
			containerHeight: 720,
			availableResolutions: [
				{ width: 1920, height: 1080, label: '1920 x 1080 (1080p FHD)' },
				{ width: 1600, height: 900, label: '1600 x 900 (HD+)' },
				{ width: 1440, height: 900, label: '1440 x 900 (WXGA+)' },
				{ width: 1366, height: 768, label: '1366 x 768 (Laptop HD)' },
				{ width: 1280, height: 720, label: '1280 x 720 (720p HD)' },
				{ width: 1024, height: 768, label: '1024 x 768 (XGA 4:3)' },
				{ width: 800, height: 600, label: '800 x 600 (SVGA 4:3)' },
			],
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
		currentWindowEstimate() {
			const w = Math.max(640, Math.round(this.containerWidth))
			const h = Math.max(480, Math.round(this.containerHeight))
			return `${w} × ${h}`
		},
		canvasTransformStyle() {
			if (this.scaleMode === 'actual') {
				const scale = this.zoomPercent / 100
				return {
					transform: scale !== 1 ? `scale(${scale})` : 'none',
					transformOrigin: 'top center',
				}
			}
			return {}
		},
	},
	mounted() {
		this.fetchHostDisplayInfo()
		this.connect()
		document.addEventListener('mousedown', this.onOutsideClick)
		document.addEventListener('fullscreenchange', this.onFullscreenChange)

		this.resizeObserver = new ResizeObserver((entries) => {
			for (const entry of entries) {
				if (entry.contentRect) {
					this.containerWidth = entry.contentRect.width
					this.containerHeight = entry.contentRect.height
				}
			}
			if (this.rfb && this.scaleMode === 'fit') {
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
		getApiBase() {
			const proto = window.location.protocol === 'https:' ? 'https:' : 'http:'
			const host = window.location.hostname || '127.0.0.1'
			return `${proto}//${host}:28641`
		},
		async fetchHostDisplayInfo() {
			try {
				const res = await axios.get(`${this.getApiBase()}/host/display`)
				if (res.data) {
					this.currentResolution = res.data.current || '1920x1080'
					this.currentWidth = res.data.width || 1920
					this.currentHeight = res.data.height || 1080
					this.customWidth = this.currentWidth
					this.customHeight = this.currentHeight
					if (res.data.resolutions && res.data.resolutions.length) {
						this.availableResolutions = res.data.resolutions
					}
				}
			} catch (e) {
				console.warn('Failed to fetch host display info:', e)
			}
		},
		async changeResolution(width, height) {
			this.resizingHost = true
			try {
				const res = await axios.post(`${this.getApiBase()}/host/display`, { width, height })
				if (res.data) {
					this.currentResolution = `${width}x${height}`
					this.currentWidth = width
					this.currentHeight = height
					this.customWidth = width
					this.customHeight = height
					this.$buefy.toast.open({
						message: `${this.$t('Display changed to')} ${width}×${height}`,
						type: 'is-success',
						position: 'is-top',
						duration: 2500,
					})
				}
				this.displaySizeMenuOpen = false
				// Re-apply viewport scaling to adapt to new dimensions
				this.$nextTick(() => {
					if (this.rfb && this.scaleMode === 'fit') {
						this.rfb.scaleViewport = true
					}
				})
			} catch (e) {
				this.$buefy.toast.open({
					message: e.response?.data?.error || this.$t('Failed to change display resolution'),
					type: 'is-danger',
					position: 'is-top',
					duration: 3500,
				})
			} finally {
				this.resizingHost = false
			}
		},
		matchWindowResolution() {
			let w = Math.round(this.containerWidth || window.innerWidth)
			let h = Math.round(this.containerHeight || window.innerHeight)
			// Snap to even numbers for video/framebuffer alignment
			if (w % 2 !== 0) w -= 1
			if (h % 2 !== 0) h -= 1
			w = Math.max(640, Math.min(3840, w))
			h = Math.max(480, Math.min(2160, h))
			this.changeResolution(w, h)
		},
		applyCustomResolution() {
			if (this.customWidth && this.customHeight) {
				this.changeResolution(this.customWidth, this.customHeight)
			}
		},
		setScaleMode(mode) {
			this.scaleMode = mode
			if (!this.rfb) return
			if (mode === 'fit') {
				this.rfb.scaleViewport = true
				this.rfb.clipViewport = false
				this.zoomPercent = 100
			} else if (mode === 'actual') {
				this.rfb.scaleViewport = false
				this.rfb.clipViewport = false
			}
		},
		adjustZoom(delta) {
			if (this.scaleMode === 'fit') {
				this.scaleMode = 'actual'
				if (this.rfb) this.rfb.scaleViewport = false
			}
			const next = Math.max(25, Math.min(300, this.zoomPercent + delta))
			this.zoomPercent = next
		},
		getHostConsoleUrl() {
			const hostname = window.location.hostname || '127.0.0.1'
			const wsProto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
			return `${wsProto}//${hostname}:28641/host/console`
		},
		getFallbackConsoleUrl() {
			const hostname = window.location.hostname || '127.0.0.1'
			const wsProto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
			return `${wsProto}//${hostname}:28642`
		},
		connect() {
			this.disconnect()
			this.status = 'connecting'

			const primaryUrl = this.getHostConsoleUrl()
			const fallbackUrl = this.getFallbackConsoleUrl()

			try {
				this.initRfb(primaryUrl, () => {
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
			this.rfb.scaleViewport = (this.scaleMode === 'fit')
			this.rfb.clipViewport = false
			this.rfb.resizeSession = true
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
			if (this.$refs.displaySizeMenuWrapper && !this.$refs.displaySizeMenuWrapper.contains(e.target)) {
				this.displaySizeMenuOpen = false
			}
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
	padding: 0.35rem 0.75rem;
	height: 44px;
	flex-shrink: 0;
	gap: 0.5rem;
	overflow-x: auto;

	&::-webkit-scrollbar {
		display: none;
	}
}

.toolbar-identity {
	display: flex;
	align-items: center;
	gap: 0.5rem;
	font-weight: 600;
	font-size: 0.85rem;
	flex-shrink: 0;

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
	flex-shrink: 0;
}

.toolbar-group {
	display: flex;
	align-items: center;
	gap: 0.3rem;
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
	white-space: nowrap;

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

	&.display-size-btn {
		background: rgba(56, 189, 248, 0.1);
		color: #38bdf8;
		border-color: rgba(56, 189, 248, 0.3);

		&:hover, &.active {
			background: #0284c7;
			color: #ffffff;
			border-color: #0284c7;
		}

		.display-res-badge {
			font-weight: 600;
			letter-spacing: 0.02em;
		}
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

/* Zoom Controls */
.zoom-controls {
	display: inline-flex;
	align-items: center;
	background: #18181b;
	border: 1px solid rgba(255, 255, 255, 0.1);
	border-radius: 6px;
	padding: 1px 2px;
	gap: 2px;

	.zoom-btn {
		border: none;
		background: transparent;
		padding: 3px 5px;
		color: #a1a1aa;

		&:hover:not(:disabled) {
			color: #ffffff;
			background: rgba(255, 255, 255, 0.1);
		}

		&:disabled {
			opacity: 0.35;
			cursor: not-allowed;
		}
	}

	.zoom-badge {
		font-size: 0.725rem;
		font-weight: 600;
		color: #d4d4d8;
		padding: 0 4px;
		min-width: 38px;
		text-align: center;
	}
}

/* Dropdown Menus */
.menu-wrapper {
	position: relative;
}

.dropdown-popover {
	position: absolute;
	top: calc(100% + 4px);
	left: 0;
	z-index: 1000;
	background: #18181b;
	border: 1px solid rgba(255, 255, 255, 0.14);
	border-radius: 10px;
	box-shadow: 0 16px 36px rgba(0, 0, 0, 0.85);
	padding: 0.4rem;
	min-width: 220px;
}

.popover-section-title {
	display: flex;
	align-items: center;
	gap: 0.35rem;
	font-size: 0.7rem;
	font-weight: 700;
	text-transform: uppercase;
	letter-spacing: 0.05em;
	color: #a1a1aa;
	padding: 0.35rem 0.6rem 0.25rem;
}

.popover-divider {
	height: 1px;
	background: rgba(255, 255, 255, 0.08);
	margin: 0.35rem 0;
}

.popover-item {
	display: flex;
	align-items: center;
	gap: 0.5rem;
	width: 100%;
	padding: 0.45rem 0.65rem;
	border: none;
	background: transparent;
	color: #f4f4f5;
	font-size: 0.8rem;
	border-radius: 6px;
	cursor: pointer;
	text-align: left;
	transition: background 0.12s ease;

	&:hover:not(:disabled) {
		background: #2563eb;
		color: #ffffff;

		.item-desc {
			color: #e0f2fe;
		}
	}

	&.active {
		background: rgba(37, 99, 235, 0.25);
		color: #60a5fa;
	}

	&:disabled {
		opacity: 0.5;
		cursor: wait;
	}

	.check-icon {
		margin-left: auto;
		color: #4ade80;
	}

	.popover-item-text {
		flex: 1;

		.item-title {
			font-weight: 600;
			font-size: 0.8rem;
		}

		.item-desc {
			font-size: 0.7rem;
			color: #a1a1aa;
		}
	}

	&.auto-match-item {
		.match-icon {
			color: #38bdf8;
		}
	}
}

.resolution-list {
	max-height: 240px;
	overflow-y: auto;

	&::-webkit-scrollbar {
		width: 4px;
	}
	&::-webkit-scrollbar-thumb {
		background: rgba(255, 255, 255, 0.2);
		border-radius: 2px;
	}
}

.custom-res-form {
	display: flex;
	align-items: center;
	gap: 0.3rem;
	padding: 0.35rem 0.5rem;
	font-size: 0.75rem;

	.custom-res-label {
		color: #a1a1aa;
	}

	.custom-res-input {
		width: 58px;
		background: #27272a;
		border: 1px solid rgba(255, 255, 255, 0.15);
		border-radius: 4px;
		color: #ffffff;
		padding: 2px 4px;
		font-size: 0.75rem;
		text-align: center;
		outline: none;

		&:focus {
			border-color: #3b82f6;
		}
	}

	.custom-res-sep {
		color: #71717a;
	}

	.custom-res-apply-btn {
		background: #2563eb;
		color: #ffffff;
		border: none;
		border-radius: 4px;
		padding: 2px 8px;
		font-size: 0.725rem;
		font-weight: 600;
		cursor: pointer;

		&:hover:not(:disabled) {
			background: #1d4ed8;
		}

		&:disabled {
			opacity: 0.4;
			cursor: not-allowed;
		}
	}
}

/* Screen Viewport Wrapper */
.screen-wrapper {
	flex: 1 1 auto;
	min-height: 0;
	position: relative;
	background: #000000;
	outline: none;

	&.is-fit {
		overflow: hidden;
		display: flex;
		align-items: center;
		justify-content: center;

		.vnc-canvas-container {
			width: 100%;
			height: 100%;
			display: flex;
			align-items: center;
			justify-content: center;

			::v-deep canvas {
				outline: none;
				width: auto !important;
				height: auto !important;
				max-width: 100% !important;
				max-height: 100% !important;
				object-fit: contain;
				display: block;
				margin: auto;
			}
		}
	}

	&.is-scrollable {
		overflow: auto;
		display: flex;
		align-items: center;
		justify-content: center;

		.vnc-canvas-container {
			display: inline-flex;
			align-items: center;
			justify-content: center;
			margin: auto;

			::v-deep canvas {
				outline: none;
				display: block;
			}
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
	padding: 0.5rem 0.75rem;
	flex-shrink: 0;
	box-shadow: 0 -8px 24px rgba(0, 0, 0, 0.5);

	.drawer-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		margin-bottom: 0.35rem;
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
		gap: 0.25rem;
	}

	.kb-row {
		display: flex;
		gap: 0.25rem;
		justify-content: center;
	}

	.kb-key {
		min-width: 32px;
		height: 30px;
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
			min-width: 50px;
		}

		&.kb-key-arrow {
			min-width: 28px;
			color: #60a5fa;
		}
	}
}
</style>
