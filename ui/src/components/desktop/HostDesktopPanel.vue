<!-- src/components/desktop/HostDesktopPanel.vue -->
<!--
	Host PC / Server Desktop Console.
	Streams the host machine's physical X11 desktop display (:0) directly
	into the NivaroOS desktop window or standalone browser tab via noVNC (RFB).
	Follows the exact UI architecture, design tokens, and components of VmConsolePanel:
	- Console toolbar with identity, status pill, and grouped action bars
	- Draggable floating on-screen keyboard (identical layout, shortcuts, and behavior)
	- Keys shortcut menu (Ctrl+Alt+Del, Win Key, Alt+Tab, Ctrl+Shift+Esc, Alt+F4)
	- Quick clipboard paste modal (with keystroke typing and clipboard paste)
	- Quick display size & resolution changer with presets, auto-match window, and custom WxH
	- View scaling toggle (Fit to Window vs 1:1 Actual Size)
	- Speedometer quality presets (High Quality, Balanced, Low Bandwidth)
	- Dedicated Fullscreen and Open in New Tab actions
	- Console statusbar displaying live connection dot, resolution, quality, scale, and display info
-->
<template>
	<div class="host-desktop-panel">
		<!-- Main Console Toolbar -->
		<div class="console-toolbar">
			<!-- Identity / Status -->
			<div class="vm-identity">
				<b-icon icon="monitor" custom-size="mdi-18px"></b-icon>
				<span class="vm-name">{{ $t('Host Desktop') }}</span>
				<span class="status-pill" :class="'is-' + status">{{ statusText }}</span>
			</div>

			<div class="toolbar-actions">
				<!-- Segment 1: Input Controls -->
				<div class="toolbar-group">
					<button
						type="button"
						class="toolbar-btn icon-only-btn"
						:class="{ active: keyboardOpen }"
						:title="$t('On-Screen Keyboard')"
						@click="keyboardOpen = !keyboardOpen"
					>
						<b-icon icon="keyboard-outline" custom-size="mdi-16px"></b-icon>
					</button>

					<div ref="keysMenuWrapper" class="menu-wrapper">
						<button
							type="button"
							class="toolbar-btn"
							:title="$t('Send Key Shortcuts')"
							@click="keysMenuOpen = !keysMenuOpen"
						>
							<b-icon icon="keyboard-settings-outline" custom-size="mdi-16px"></b-icon>
							<span>{{ $t('Keys') }}</span>
							<b-icon icon="chevron-down" custom-size="mdi-14px"></b-icon>
						</button>
						<div v-if="keysMenuOpen" class="power-menu keys-menu">
							<button type="button" class="power-menu-item" @click="sendCtrlAltDel(); keysMenuOpen = false">
								<b-icon icon="apple-keyboard-control" custom-size="mdi-16px"></b-icon>
								<span>Ctrl+Alt+Del</span>
							</button>
							<button type="button" class="power-menu-item" @click="sendWinKey(); keysMenuOpen = false">
								<b-icon icon="microsoft-windows" custom-size="mdi-16px"></b-icon>
								<span>{{ $t('Win Key') }}</span>
							</button>
							<button type="button" class="power-menu-item" @click="sendAltTab(); keysMenuOpen = false">
								<b-icon icon="tab" custom-size="mdi-16px"></b-icon>
								<span>Alt + Tab</span>
							</button>
							<button type="button" class="power-menu-item" @click="sendCtrlShiftEsc(); keysMenuOpen = false">
								<b-icon icon="chart-line" custom-size="mdi-16px"></b-icon>
								<span>Ctrl+Shift+Esc</span>
							</button>
							<button type="button" class="power-menu-item" @click="sendAltF4(); keysMenuOpen = false">
								<b-icon icon="close-box-outline" custom-size="mdi-16px"></b-icon>
								<span>Alt + F4</span>
							</button>
						</div>
					</div>

					<button
						type="button"
						class="toolbar-btn icon-only-btn"
						:title="$t('Paste clipboard text into Host')"
						@click="pasteClipboard"
					>
						<b-icon icon="content-paste" custom-size="mdi-16px"></b-icon>
					</button>
				</div>

				<div class="toolbar-divider"></div>

				<!-- Segment 2: View & Stream Controls -->
				<div class="toolbar-group">
					<!-- Display Resolution & Size Menu -->
					<div ref="displayMenuWrapper" class="menu-wrapper">
						<button
							type="button"
							class="toolbar-btn"
							:class="{ active: displayMenuOpen }"
							:title="$t('Display Resolution & Size')"
							@click="displayMenuOpen = !displayMenuOpen"
						>
							<b-icon icon="monitor-screenshot" custom-size="mdi-16px"></b-icon>
							<span>{{ currentResolution || '1920x1080' }}</span>
							<b-icon icon="chevron-down" custom-size="mdi-14px"></b-icon>
						</button>

						<div v-if="displayMenuOpen" class="device-menu display-dropdown-menu">
							<div class="device-menu-header-row">
								<div class="header-title-group">
									<b-icon icon="monitor-screenshot" size="is-small"></b-icon>
									<p class="device-menu-title">{{ $t('Display Resolution') }}</p>
								</div>
								<span class="active-resolution-badge">{{ currentResolution || '1920x1080' }}</span>
							</div>
							<p class="device-menu-hint">{{ $t('Dynamically change host physical display resolution.') }}</p>

							<!-- Auto Match Window Option -->
							<div
								class="device-menu-row auto-match-row"
								:class="{ disabled: resizingHost }"
								@click="matchWindowResolution"
							>
								<div class="device-row-icon active">
									<b-icon icon="arrow-expand-all" size="is-small"></b-icon>
								</div>
								<div class="network-row-details">
									<span class="network-row-label">{{ $t('Match Current Window') }}</span>
									<span class="network-row-meta">{{ currentWindowEstimate }}</span>
								</div>
								<b-icon v-if="resizingHost" icon="loading" custom-class="mdi-spin" size="is-small"></b-icon>
							</div>

							<p class="device-menu-title device-menu-title-divided">{{ $t('Preset Resolutions') }}</p>
							<div class="device-menu-scrollable">
								<div
									v-for="r in availableResolutions"
									:key="r.width + 'x' + r.height"
									class="device-menu-row"
									:class="{ active: currentResolution === `${r.width}x${r.height}` }"
									@click="changeResolution(r.width, r.height)"
								>
									<div class="device-row-icon" :class="{ active: currentResolution === `${r.width}x${r.height}` }">
										<b-icon icon="monitor" size="is-small"></b-icon>
									</div>
									<span class="device-menu-desc">{{ r.label }}</span>
									<b-icon
										v-if="currentResolution === `${r.width}x${r.height}`"
										icon="check"
										size="is-small"
										custom-class="has-text-success"
									></b-icon>
								</div>
							</div>

							<p class="device-menu-title device-menu-title-divided">{{ $t('Custom Resolution') }}</p>
							<div class="custom-res-row" @click.stop>
								<input
									v-model.number="customWidth"
									type="number"
									class="custom-res-field"
									placeholder="1920"
									min="640"
									max="7680"
								/>
								<span class="custom-res-multiply">×</span>
								<input
									v-model.number="customHeight"
									type="number"
									class="custom-res-field"
									placeholder="1080"
									min="480"
									max="4320"
								/>
								<button
									type="button"
									class="custom-res-btn"
									:disabled="resizingHost || !customWidth || !customHeight"
									@click="applyCustomResolution"
								>
									<b-icon v-if="resizingHost" icon="loading" custom-class="mdi-spin" size="is-small"></b-icon>
									<span v-else>{{ $t('Set') }}</span>
								</button>
							</div>
						</div>
					</div>

					<!-- Fit vs 1:1 Scale Toggle -->
					<button
						type="button"
						class="toolbar-btn"
						:title="scaleToFit ? $t('Show actual size (1:1)') : $t('Scale to fit window')"
						@click="toggleScale"
					>
						<b-icon :icon="scaleToFit ? 'fit-to-page-outline' : 'aspect-ratio'" custom-size="mdi-16px"></b-icon>
						<span>{{ scaleToFit ? $t('Fit') : $t('1:1') }}</span>
					</button>

					<!-- Bandwidth & Stream Quality Menu -->
					<div ref="qualityMenuWrapper" class="menu-wrapper">
						<button
							type="button"
							class="toolbar-btn"
							:title="$t('Display & Bandwidth Quality')"
							@click="qualityMenuOpen = !qualityMenuOpen"
						>
							<b-icon icon="speedometer" custom-size="mdi-16px"></b-icon>
							<span>{{ qualityModeLabel }}</span>
							<b-icon icon="chevron-down" custom-size="mdi-14px"></b-icon>
						</button>
						<div v-if="qualityMenuOpen" class="power-menu quality-menu">
							<button
								v-for="q in qualityOptions"
								:key="q.mode"
								type="button"
								class="power-menu-item quality-menu-item"
								:class="{ active: qualityMode === q.mode }"
								@click="setQualityMode(q.mode)"
							>
								<b-icon :icon="q.icon" custom-size="mdi-18px"></b-icon>
								<span class="quality-menu-text">
									<span class="quality-menu-title">{{ $t(q.label) }}</span>
									<span class="quality-menu-desc">{{ $t(q.desc) }}</span>
								</span>
								<b-icon v-if="qualityMode === q.mode" icon="check" custom-size="mdi-16px" class="quality-menu-check"></b-icon>
							</button>
						</div>
					</div>

					<!-- Fullscreen Action -->
					<button
						type="button"
						class="toolbar-btn icon-only-btn"
						:title="$t('Fullscreen')"
						@click="toggleFullscreen"
					>
						<b-icon icon="fullscreen" custom-size="mdi-16px"></b-icon>
					</button>

					<!-- Open Standalone Tab Action -->
					<button
						type="button"
						class="toolbar-btn icon-only-btn"
						:title="$t('Open in New Tab')"
						@click="openInNewTab"
					>
						<b-icon icon="open-in-new" custom-size="mdi-16px"></b-icon>
					</button>
				</div>

				<!-- Optional Close Button for Standalone Tab Wrapper -->
				<button
					v-if="showClose"
					type="button"
					class="toolbar-btn icon-only-btn close-btn"
					:title="$t('Close')"
					@click="$emit('close')"
				>
					<b-icon icon="close" custom-size="mdi-16px"></b-icon>
				</button>
			</div>
		</div>

		<!-- Display Canvas Screen Container -->
		<div
			ref="screen"
			class="console-screen"
			:class="{ 'is-scrollable': !scaleToFit }"
		></div>

		<!-- Disconnected / Reconnecting Overlay -->
		<div v-if="status !== 'connected'" class="console-status">
			<b-icon v-if="status === 'connecting'" icon="loading" custom-class="mdi-spin" custom-size="mdi-36px"></b-icon>
			<b-icon v-else icon="lan-disconnect" custom-size="mdi-36px"></b-icon>
			<span>{{ statusText }}</span>
			<button v-if="status === 'disconnected'" class="reconnect-btn" @click="connect">
				{{ $t('Reconnect') }}
			</button>
		</div>

		<!-- Floating Draggable On-Screen Keyboard -->
		<div
			v-show="keyboardOpen"
			ref="keyboard"
			class="on-screen-keyboard"
			:style="keyboardStyle"
		>
			<div class="osk-header" @pointerdown="startKeyboardDrag">
				<b-icon icon="drag-horizontal-variant" size="is-small"></b-icon>
				<span class="osk-title">{{ $t('Keyboard') }}</span>
				<button
					type="button"
					class="osk-close"
					:title="$t('Close')"
					@pointerdown.stop
					@mousedown.stop
					@click.stop="closeKeyboard"
				>
					<b-icon icon="close" size="is-small"></b-icon>
				</button>
			</div>
			<div class="osk-keys">
				<div class="osk-alpha">
					<div v-for="(row, i) in keyboardRows" :key="'a' + i" class="osk-row">
						<button
							v-for="key in row"
							:key="key.code"
							type="button"
							class="osk-key"
							:style="keyStyle(key)"
							:class="{ active: key.sticky && stickyState(key.sticky) }"
							@click="pressKey(key)"
						>
							{{ keyLabel(key) }}
						</button>
					</div>
				</div>
				<div class="osk-side">
					<div class="osk-row osk-fn-spacer"></div>
					<div v-for="(row, i) in navRows" :key="'n' + i" class="osk-row">
						<button
							v-for="key in row"
							:key="key.code"
							type="button"
							class="osk-key"
							:style="keyStyle(key)"
							@click="pressKey(key)"
						>
							{{ keyLabel(key) }}
						</button>
					</div>
					<div class="osk-side-fill"></div>
					<div v-for="(row, i) in arrowRows" :key="'r' + i" class="osk-row">
						<button
							v-for="(key, j) in row"
							:key="j"
							type="button"
							class="osk-key"
							:class="{ 'osk-key-empty': !key }"
							:style="keyStyle(key || { u: 1 })"
							:disabled="!key"
							@click="key && pressKey(key)"
						>
							{{ key ? keyLabel(key) : '' }}
						</button>
					</div>
				</div>
				<div class="osk-shortcuts-col">
					<button
						v-for="s in shortcuts"
						:key="s.key"
						type="button"
						class="osk-key osk-shortcut-btn"
						:style="keyStyle({ u: 1 })"
						:title="$t(s.label)"
						@click="sendShortcut(s.key)"
					>
						<b-icon :icon="s.icon" size="is-small"></b-icon>
					</button>
				</div>
			</div>
		</div>

		<!-- Statusbar at the Bottom -->
		<div class="console-statusbar">
			<span class="statusbar-item" :class="{ 'is-live': status === 'connected' }">
				<span class="activity-dot"></span>{{ statusText }}
			</span>
			<span class="statusbar-item">
				<b-icon icon="monitor" size="is-small"></b-icon>
				{{ currentResolution || '1920x1080' }}
			</span>
			<span class="statusbar-item">
				<b-icon icon="speedometer" size="is-small"></b-icon>
				{{ qualityModeLabel }}
			</span>
			<span class="statusbar-item">
				<b-icon :icon="scaleToFit ? 'fit-to-page-outline' : 'aspect-ratio'" size="is-small"></b-icon>
				{{ scaleToFit ? $t('Fit') : $t('1:1') }}
			</span>
			<span class="statusbar-item">
				<b-icon icon="desktop-classic" size="is-small"></b-icon>
				{{ $t('Display :0') }}
			</span>
		</div>

		<!-- Quick Paste & Type Modal -->
		<vm-overlay-panel
			:active="showPasteDialog"
			:title="$t('Paste Text to Host Desktop')"
			max-width="30rem"
			@close="showPasteDialog = false"
		>
			<div class="paste-modal-content">
				<p class="paste-modal-desc">
					{{ $t('Type or paste text (Ctrl+V) to send to the host desktop.') }}
				</p>
				<textarea
					ref="pasteInput"
					v-model="pasteText"
					class="paste-modal-textarea"
					rows="4"
					:placeholder="$t('Paste your text here...')"
					@keydown.enter.ctrl="sendPasteText(false)"
				></textarea>
				<div class="paste-modal-actions">
					<button type="button" class="paste-action-btn" @click="sendPasteText(true)">
						<b-icon icon="keyboard-outline" size="is-small"></b-icon>
						<span>{{ $t('Type Keystrokes') }}</span>
					</button>
					<button
						type="button"
						class="paste-action-btn is-primary"
						:disabled="!pasteText"
						@click="sendPasteText(false)"
					>
						<b-icon icon="content-paste" size="is-small"></b-icon>
						<span>{{ $t('Paste Clipboard') }}</span>
					</button>
				</div>
			</div>
		</vm-overlay-panel>
	</div>
</template>

<script>
import RFB from '@novnc/novnc'
import axios from 'axios'
import VmOverlayPanel from '@/components/desktop/vm/VmOverlayPanel.vue'

const QUALITY_PRESETS = {
	high: { qualityLevel: 9, compressionLevel: 1 },
	balanced: { qualityLevel: 6, compressionLevel: 2 },
	low: { qualityLevel: 2, compressionLevel: 8 },
}

const QUALITY_OPTIONS = [
	{ mode: 'high', icon: 'high-definition', label: 'High Quality', desc: 'Sharpest picture, most data' },
	{ mode: 'balanced', icon: 'tune-vertical', label: 'Balanced', desc: 'Good picture, moderate data' },
	{ mode: 'low', icon: 'speedometer-slow', label: 'Low Bandwidth', desc: 'Softer picture, least lag' },
]

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
	Menu: 0xff67,
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

const SHORTCUTS = [
	{ key: 'c', label: 'Copy', icon: 'content-copy' },
	{ key: 'x', label: 'Cut', icon: 'content-cut' },
	{ key: 'v', label: 'Paste', icon: 'content-paste' },
	{ key: 'z', label: 'Undo', icon: 'undo' },
	{ key: 'a', label: 'Select All', icon: 'select-all' },
]

const KEYBOARD_ROWS = [
	[
		{ code: 'Escape', special: 'Escape', label: 'Esc' },
		{ code: 'F1', special: 'F1', label: 'F1', gapBefore: 1.7 }, { code: 'F2', special: 'F2', label: 'F2' }, { code: 'F3', special: 'F3', label: 'F3' },
		{ code: 'F4', special: 'F4', label: 'F4' }, { code: 'F5', special: 'F5', label: 'F5', gapBefore: 1.7 }, { code: 'F6', special: 'F6', label: 'F6' },
		{ code: 'F7', special: 'F7', label: 'F7' }, { code: 'F8', special: 'F8', label: 'F8' }, { code: 'F9', special: 'F9', label: 'F9', gapBefore: 1.7 },
		{ code: 'F10', special: 'F10', label: 'F10' }, { code: 'F11', special: 'F11', label: 'F11' }, { code: 'F12', special: 'F12', label: 'F12' },
	],
	[
		{ code: 'Backquote', base: '`', shift: '~' }, { code: 'Digit1', base: '1', shift: '!' }, { code: 'Digit2', base: '2', shift: '@' },
		{ code: 'Digit3', base: '3', shift: '#' }, { code: 'Digit4', base: '4', shift: '$' }, { code: 'Digit5', base: '5', shift: '%' },
		{ code: 'Digit6', base: '6', shift: '^' }, { code: 'Digit7', base: '7', shift: '&' }, { code: 'Digit8', base: '8', shift: '*' },
		{ code: 'Digit9', base: '9', shift: '(' }, { code: 'Digit0', base: '0', shift: ')' }, { code: 'Minus', base: '-', shift: '_' },
		{ code: 'Equal', base: '=', shift: '+' }, { code: 'Backspace', special: 'Backspace', label: '⌫', u: 2 },
	],
	[
		{ code: 'Tab', special: 'Tab', label: 'Tab', u: 1.5 }, { code: 'KeyQ', base: 'q', shift: 'Q' }, { code: 'KeyW', base: 'w', shift: 'W' },
		{ code: 'KeyE', base: 'e', shift: 'E' }, { code: 'KeyR', base: 'r', shift: 'R' }, { code: 'KeyT', base: 't', shift: 'T' },
		{ code: 'KeyY', base: 'y', shift: 'Y' }, { code: 'KeyU', base: 'u', shift: 'U' }, { code: 'KeyI', base: 'i', shift: 'I' },
		{ code: 'KeyO', base: 'o', shift: 'O' }, { code: 'KeyP', base: 'p', shift: 'P' }, { code: 'BracketLeft', base: '[', shift: '{' },
		{ code: 'BracketRight', base: ']', shift: '}' }, { code: 'Backslash', base: '\\', shift: '|', u: 1.5 },
	],
	[
		{ code: 'CapsLock', special: 'CapsLock', label: 'Caps', u: 1.75, sticky: 'capsLockActive' },
		{ code: 'KeyA', base: 'a', shift: 'A' }, { code: 'KeyS', base: 's', shift: 'S' }, { code: 'KeyD', base: 'd', shift: 'D' },
		{ code: 'KeyF', base: 'f', shift: 'F' }, { code: 'KeyG', base: 'g', shift: 'G' }, { code: 'KeyH', base: 'h', shift: 'H' },
		{ code: 'KeyJ', base: 'j', shift: 'J' }, { code: 'KeyK', base: 'k', shift: 'K' }, { code: 'KeyL', base: 'l', shift: 'L' },
		{ code: 'Semicolon', base: ';', shift: ':' }, { code: 'Quote', base: "'", shift: '"' },
		{ code: 'Enter', special: 'Enter', label: 'Enter', u: 2.25 },
	],
	[
		{ code: 'ShiftLeft', special: 'Shift', label: 'Shift', u: 2.25, sticky: 'shiftActive' },
		{ code: 'KeyZ', base: 'z', shift: 'Z' }, { code: 'KeyX', base: 'x', shift: 'X' }, { code: 'KeyC', base: 'c', shift: 'C' },
		{ code: 'KeyV', base: 'v', shift: 'V' }, { code: 'KeyB', base: 'b', shift: 'B' }, { code: 'KeyN', base: 'n', shift: 'N' },
		{ code: 'KeyM', base: 'm', shift: 'M' }, { code: 'Comma', base: ',', shift: '<' }, { code: 'Period', base: '.', shift: '>' },
		{ code: 'Slash', base: '/', shift: '?' }, { code: 'ShiftRight', special: 'Shift', label: 'Shift', u: 2.75, sticky: 'shiftActive' },
	],
	[
		{ code: 'ControlLeft', special: 'Control', label: 'Ctrl', u: 1.5, sticky: 'ctrlActive' },
		{ code: 'MetaLeft', special: 'Super', label: 'Win', u: 1.25 },
		{ code: 'AltLeft', special: 'Alt', label: 'Alt', u: 1.25, sticky: 'altActive' },
		{ code: 'Space', base: ' ', label: 'Space', u: 6.25 },
		{ code: 'AltRight', special: 'Alt', label: 'Alt', u: 1.25 },
		{ code: 'MetaRight', special: 'Super', label: 'Win', u: 1.25 },
		{ code: 'ContextMenu', special: 'Menu', label: 'Menu', u: 1.25 },
		{ code: 'ControlRight', special: 'Control', label: 'Ctrl', u: 1.5 },
	],
]

const NAV_ROWS = [
	[{ code: 'Insert', special: 'Insert', label: 'Ins' }, { code: 'Home', special: 'Home', label: 'Home' }, { code: 'PageUp', special: 'PageUp', label: 'PgUp' }],
	[{ code: 'Delete', special: 'Delete', label: 'Del' }, { code: 'End', special: 'End', label: 'End' }, { code: 'PageDown', special: 'PageDown', label: 'PgDn' }],
]

const ARROW_ROWS = [
	[null, { code: 'ArrowUp', special: 'ArrowUp', label: '▲' }, null],
	[{ code: 'ArrowLeft', special: 'ArrowLeft', label: '◀' }, { code: 'ArrowDown', special: 'ArrowDown', label: '▼' }, { code: 'ArrowRight', special: 'ArrowRight', label: '▶' }],
]

export default {
	name: 'HostDesktopPanel',
	components: {
		VmOverlayPanel,
	},
	props: {
		showClose: {
			type: Boolean,
			default: false,
		},
	},
	data() {
		return {
			rfb: null,
			status: 'connecting',
			keysMenuOpen: false,
			qualityMenuOpen: false,
			displayMenuOpen: false,
			scaleToFit: true,
			qualityMode: 'high',
			qualityOptions: QUALITY_OPTIONS,
			keyboardOpen: false,
			keyboardPos: null,
			shiftActive: false,
			capsLockActive: false,
			ctrlActive: false,
			altActive: false,
			showPasteDialog: false,
			pasteText: '',
			currentResolution: '1920x1080',
			availableResolutions: [],
			resizingHost: false,
			currentWindowEstimate: '1920 × 1080',
			customWidth: null,
			customHeight: null,
			keyboardRows: KEYBOARD_ROWS,
			navRows: NAV_ROWS,
			arrowRows: ARROW_ROWS,
			shortcuts: SHORTCUTS,
		}
	},
	computed: {
		statusText() {
			switch (this.status) {
				case 'connected':
					return this.$t('Connected')
				case 'connecting':
					return this.$t('Connecting...')
				case 'disconnected':
				default:
					return this.$t('Disconnected')
			}
		},
		qualityModeLabel() {
			const m = this.qualityOptions.find((q) => q.mode === this.qualityMode)
			return m ? this.$t(m.label) : this.qualityMode
		},
		keyboardStyle() {
			if (!this.keyboardPos) return {}
			return {
				left: this.keyboardPos.x + 'px',
				top: this.keyboardPos.y + 'px',
				bottom: 'auto',
				transform: 'none',
			}
		},
	},
	watch: {
		status: {
			immediate: true,
			handler(val) {
				this.$emit('status-change', val)
			},
		},
		keyboardOpen(open) {
			if (open) {
				this.$nextTick(() => {
					const target = document.fullscreenElement ? this.$el : document.body
					if (this.$refs.keyboard && this.$refs.keyboard.parentNode !== target) {
						target.appendChild(this.$refs.keyboard)
					}
					if (!this.keyboardPos && this.$refs.keyboard) {
						const kbRect = this.$refs.keyboard.getBoundingClientRect()
						this.keyboardPos = {
							x: Math.max(10, Math.round((window.innerWidth - kbRect.width) / 2)),
							y: Math.max(10, Math.round(window.innerHeight - kbRect.height - 50)),
						}
					}
					this.clampKeyboardPos()
				})
			}
		},
		displayMenuOpen(open) {
			if (open) {
				this.updateWindowEstimate()
			}
		},
	},
	mounted() {
		try {
			const savedQuality = localStorage.getItem('host_desktop_quality_mode')
			if (savedQuality && QUALITY_PRESETS[savedQuality]) {
				this.qualityMode = savedQuality
			}
		} catch (e) {}

		this.connect()
		this.fetchHostDisplay()

		document.addEventListener('mousedown', this.onOutsideClick)
		document.addEventListener('fullscreenchange', this.onFullscreenChange)
		window.addEventListener('resize', this.onWindowResize)

		this.panelResizeObserver = new ResizeObserver(() => {
			this.clampKeyboardPos()
			this.updateWindowEstimate()
		})
		this.panelResizeObserver.observe(this.$el)
	},
	beforeDestroy() {
		if (this.rfb) {
			this.rfb.disconnect()
			this.rfb = null
		}
		document.removeEventListener('mousedown', this.onOutsideClick)
		document.removeEventListener('fullscreenchange', this.onFullscreenChange)
		window.removeEventListener('resize', this.onWindowResize)

		if (this.panelResizeObserver) {
			this.panelResizeObserver.disconnect()
		}
		if (this.$refs.keyboard && this.$refs.keyboard.parentNode) {
			this.$refs.keyboard.parentNode.removeChild(this.$refs.keyboard)
		}
	},
	methods: {
		connect() {
			if (this.rfb) {
				this.rfb.disconnect()
				this.rfb = null
			}
			this.status = 'connecting'

			const proto = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
			const host = window.location.hostname || '127.0.0.1'
			const url = `${proto}//${host}:28641/host/console`

			try {
				this.rfb = new RFB(this.$refs.screen, url)
				this.rfb.scaleViewport = this.scaleToFit
				this.rfb.resizeSession = false

				const preset = QUALITY_PRESETS[this.qualityMode] || QUALITY_PRESETS.high
				this.rfb.qualityLevel = preset.qualityLevel
				this.rfb.compressionLevel = preset.compressionLevel

				this.rfb.addEventListener('connect', () => {
					this.status = 'connected'
					const p = QUALITY_PRESETS[this.qualityMode] || QUALITY_PRESETS.high
					this.rfb.qualityLevel = p.qualityLevel
					this.rfb.compressionLevel = p.compressionLevel
				})

				this.rfb.addEventListener('disconnect', () => {
					this.status = 'disconnected'
				})

				this.rfb.addEventListener('clipboard', (e) => {
					const text = e.detail && e.detail.text
					if (text && navigator.clipboard && navigator.clipboard.writeText) {
						navigator.clipboard.writeText(text).catch(() => {})
					}
				})

				this.rfb.addEventListener('fbsize', (e) => {
					if (e.detail && e.detail.width && e.detail.height) {
						this.currentResolution = `${e.detail.width}x${e.detail.height}`
					}
				})
			} catch (e) {
				this.status = 'disconnected'
			}
		},

		async fetchHostDisplay() {
			try {
				const res = await axios.get('/api/host/display')
				if (res.data) {
					this.currentResolution = res.data.current || this.currentResolution
					this.availableResolutions = res.data.resolutions || []
				}
			} catch (e) {
				// Direct sidecar port fallback
				try {
					const host = window.location.hostname || '127.0.0.1'
					const res = await axios.get(`//${host}:28641/host/display`)
					if (res.data) {
						this.currentResolution = res.data.current || this.currentResolution
						this.availableResolutions = res.data.resolutions || []
					}
				} catch (err) {}
			}
		},

		async changeResolution(width, height) {
			if (!width || !height || this.resizingHost) return
			this.resizingHost = true
			try {
				let res = null
				try {
					res = await axios.post('/api/host/display', { width, height })
				} catch (e) {
					const host = window.location.hostname || '127.0.0.1'
					res = await axios.post(`//${host}:28641/host/display`, { width, height })
				}
				if (res && res.data) {
					this.currentResolution = res.data.current || `${width}x${height}`
				}
				this.displayMenuOpen = false
				this.$buefy.toast.open({
					message: `${this.$t('Host Display Resolution')}: ${width}×${height}`,
					type: 'is-success',
					duration: 2500,
				})
			} catch (e) {
				this.$buefy.toast.open({
					message: this.$t('Failed to change display resolution'),
					type: 'is-danger',
					duration: 3500,
				})
			} finally {
				this.resizingHost = false
			}
		},

		updateWindowEstimate() {
			if (!this.$refs.screen) return
			const w = Math.round(this.$refs.screen.clientWidth)
			const h = Math.round(this.$refs.screen.clientHeight)
			if (w > 0 && h > 0) {
				this.currentWindowEstimate = `${w} × ${h}`
			}
		},

		async matchWindowResolution() {
			if (!this.$refs.screen || this.resizingHost) return
			const w = Math.max(640, Math.round(this.$refs.screen.clientWidth))
			const h = Math.max(480, Math.round(this.$refs.screen.clientHeight))
			await this.changeResolution(w, h)
		},

		async applyCustomResolution() {
			if (!this.customWidth || !this.customHeight) return
			await this.changeResolution(this.customWidth, this.customHeight)
		},

		toggleScale() {
			this.scaleToFit = !this.scaleToFit
			if (this.rfb) {
				this.rfb.scaleViewport = this.scaleToFit
			}
		},

		setQualityMode(mode) {
			this.qualityMode = mode
			this.qualityMenuOpen = false
			try {
				localStorage.setItem('host_desktop_quality_mode', mode)
			} catch (e) {}
			if (this.rfb) {
				const preset = QUALITY_PRESETS[mode] || QUALITY_PRESETS.high
				this.rfb.qualityLevel = preset.qualityLevel
				this.rfb.compressionLevel = preset.compressionLevel
			}
			this.$buefy.toast.open({
				message: `${this.$t('Bandwidth profile')}: ${this.qualityModeLabel}`,
				type: 'is-info',
				duration: 2000,
			})
		},

		toggleFullscreen() {
			if (document.fullscreenElement) {
				document.exitFullscreen()
			} else {
				this.$el.requestFullscreen()
			}
		},

		openInNewTab() {
			window.open('/#/host-desktop', '_blank')
		},

		onOutsideClick(event) {
			if (!event || !event.target) return
			if (typeof event.target.closest === 'function') {
				if (
					event.target.closest('.vm-overlay') ||
					event.target.closest('.modal') ||
					event.target.closest('.dialog') ||
					event.target.closest('.device-menu') ||
					event.target.closest('.power-menu')
				) {
					return
				}
			}
			if (this.keysMenuOpen && this.$refs.keysMenuWrapper && !this.$refs.keysMenuWrapper.contains(event.target)) {
				this.keysMenuOpen = false
			}
			if (this.qualityMenuOpen && this.$refs.qualityMenuWrapper && !this.$refs.qualityMenuWrapper.contains(event.target)) {
				this.qualityMenuOpen = false
			}
			if (this.displayMenuOpen && this.$refs.displayMenuWrapper && !this.$refs.displayMenuWrapper.contains(event.target)) {
				this.displayMenuOpen = false
			}
		},

		onWindowResize() {
			this.clampKeyboardPos()
			this.updateWindowEstimate()
		},

		onFullscreenChange() {
			if (!this.keyboardOpen || !this.$refs.keyboard) return
			const target = document.fullscreenElement ? this.$el : document.body
			if (this.$refs.keyboard.parentNode !== target) {
				target.appendChild(this.$refs.keyboard)
			}
			this.$nextTick(() => {
				this.clampKeyboardPos()
			})
		},

		closeKeyboard() {
			this.keyboardOpen = false
		},

		startKeyboardDrag(event) {
			const header = event.currentTarget
			header.setPointerCapture(event.pointerId)
			const kbEl = this.$refs.keyboard
			if (!kbEl) return
			const kbRect = kbEl.getBoundingClientRect()
			const offsetX = event.clientX - kbRect.left
			const offsetY = event.clientY - kbRect.top
			const onMove = (e) => {
				const maxX = Math.max(0, window.innerWidth - kbRect.width)
				const maxY = Math.max(0, window.innerHeight - kbRect.height)
				const x = Math.max(0, Math.min(e.clientX - offsetX, maxX))
				const y = Math.max(0, Math.min(e.clientY - offsetY, maxY))
				this.keyboardPos = { x, y }
			}
			const onUp = () => {
				header.releasePointerCapture(event.pointerId)
				header.removeEventListener('pointermove', onMove)
				header.removeEventListener('pointerup', onUp)
				header.removeEventListener('pointercancel', onUp)
			}
			header.addEventListener('pointermove', onMove)
			header.addEventListener('pointerup', onUp)
			header.addEventListener('pointercancel', onUp)
		},

		clampKeyboardPos() {
			if (!this.keyboardPos || !this.$refs.keyboard) return
			const kbRect = this.$refs.keyboard.getBoundingClientRect()
			const maxX = Math.max(0, window.innerWidth - kbRect.width)
			const maxY = Math.max(0, window.innerHeight - kbRect.height)
			const x = Math.max(0, Math.min(this.keyboardPos.x, maxX))
			const y = Math.max(0, Math.min(this.keyboardPos.y, maxY))
			if (x !== this.keyboardPos.x || y !== this.keyboardPos.y) {
				this.keyboardPos = { x, y }
			}
		},

		stickyState(prop) {
			return this[prop]
		},

		keyStyle(key) {
			const KEY_REM = 2.3
			const GAP_REM = 0.25
			const u = key.u || 1
			const style = { width: `${u * KEY_REM + (u - 1) * GAP_REM}rem` }
			if (key.gapBefore) style.marginLeft = `${key.gapBefore}rem`
			return style
		},

		keyLabel(key) {
			if (key.special) return key.label
			return this.shiftActive ? key.shift || key.base : key.base
		},

		pressKey(key) {
			if (!this.rfb) return
			if (key.sticky) {
				this[key.sticky] = !this[key.sticky]
				this.rfb.sendKey(SPECIAL_KEYSYMS[key.special], key.code, this[key.sticky])
				return
			}
			if (key.special) {
				const keysym = SPECIAL_KEYSYMS[key.special]
				this.rfb.sendKey(keysym, key.code, true)
				this.rfb.sendKey(keysym, key.code, false)
				return
			}
			const ch = this.shiftActive ? key.shift || key.base : key.base
			const keysym = ch.charCodeAt(0)
			this.rfb.sendKey(keysym, key.code, true)
			this.rfb.sendKey(keysym, key.code, false)
		},

		sendShortcut(letter) {
			if (!this.rfb) return
			const ctrl = SPECIAL_KEYSYMS.Control
			const code = 'Key' + letter.toUpperCase()
			const keysym = letter.charCodeAt(0)
			this.rfb.sendKey(ctrl, 'ControlLeft', true)
			this.rfb.sendKey(keysym, code, true)
			this.rfb.sendKey(keysym, code, false)
			this.rfb.sendKey(ctrl, 'ControlLeft', false)
		},

		sendCtrlAltDel() {
			if (this.rfb) this.rfb.sendCtrlAltDel()
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

		async pasteClipboard() {
			if (!this.rfb) return
			let text = ''
			try {
				if (navigator.clipboard && navigator.clipboard.readText) {
					text = await navigator.clipboard.readText()
				}
			} catch (e) {}

			if (text) {
				this.rfb.clipboardPasteFrom(text)
				this.$buefy.toast.open({
					message: this.$t('Pasted into Host clipboard'),
					type: 'is-success',
					position: 'is-top',
					duration: 2000,
				})
			} else {
				this.pasteText = ''
				this.showPasteDialog = true
				this.$nextTick(() => {
					if (this.$refs.pasteInput) this.$refs.pasteInput.focus()
				})
			}
		},

		sendPasteText(asKeystrokes = false) {
			if (!this.rfb || !this.pasteText) return
			if (asKeystrokes) {
				const str = this.pasteText
				for (let i = 0; i < str.length; i++) {
					const char = str[i]
					if (char === '\n') {
						this.rfb.sendKey(SPECIAL_KEYSYMS.Enter || 0xff0d, 'Enter', true)
						this.rfb.sendKey(SPECIAL_KEYSYMS.Enter || 0xff0d, 'Enter', false)
					} else {
						const code = str.charCodeAt(i)
						this.rfb.sendKey(code, null, true)
						this.rfb.sendKey(code, null, false)
					}
				}
				this.$buefy.toast.open({
					message: this.$t('Typed text into Host'),
					type: 'is-success',
					position: 'is-top',
					duration: 2000,
				})
			} else {
				this.rfb.clipboardPasteFrom(this.pasteText)
				this.$buefy.toast.open({
					message: this.$t('Pasted into Host clipboard'),
					type: 'is-success',
					position: 'is-top',
					duration: 2000,
				})
			}
			this.pasteText = ''
			this.showPasteDialog = false
		},
	},
}
</script>

<style lang="scss" scoped>
.host-desktop-panel {
	position: absolute;
	inset: 0;
	background: #000;
	display: flex;
	flex-direction: column;
	color: #fff;
	overflow: hidden;
}

.console-toolbar {
	position: relative;
	z-index: 20;
	flex-shrink: 0;
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: 0.5rem;
	padding: 0.4rem 0.65rem;
	background: #141416;
	border-bottom: 1px solid rgba(255, 255, 255, 0.08);
	user-select: none;
	overflow: visible;
}

.vm-identity {
	display: flex;
	align-items: center;
	gap: 0.5rem;
}

.vm-name {
	font-weight: 600;
	font-size: 0.85rem;
	letter-spacing: -0.01em;
}

.status-pill {
	font-size: 0.68rem;
	padding: 0.1rem 0.5rem;
	border-radius: 999px;
	background: rgba(255, 255, 255, 0.1);
	color: rgba(255, 255, 255, 0.7);

	&.is-connected {
		background: rgba(72, 199, 116, 0.2);
		color: #48c774;
	}
	&.is-connecting {
		background: rgba(255, 221, 87, 0.15);
		color: #ffdd57;
	}
	&.is-disconnected {
		background: rgba(255, 56, 96, 0.15);
		color: #ff3860;
	}
}

.toolbar-actions {
	display: flex;
	align-items: center;
	gap: 0.35rem;
	flex-wrap: nowrap;
	min-width: 0;
	overflow: visible;
}

.toolbar-group {
	display: inline-flex;
	align-items: center;
	gap: 0.2rem;
	background: rgba(255, 255, 255, 0.05);
	padding: 0.18rem;
	border-radius: 8px;
	border: 1px solid rgba(255, 255, 255, 0.06);
}

.toolbar-divider {
	width: 1px;
	height: 1.25rem;
	background: rgba(255, 255, 255, 0.1);
	margin: 0 0.15rem;
	flex-shrink: 0;
}

.toolbar-btn {
	display: inline-flex;
	align-items: center;
	gap: 0.3rem;
	border: none;
	background: transparent;
	color: rgba(255, 255, 255, 0.85);
	font-family: inherit;
	font-size: 0.75rem;
	font-weight: 500;
	padding: 0.32rem 0.55rem;
	border-radius: 6px;
	cursor: pointer;
	white-space: nowrap;
	transition: all 0.14s ease;

	&:hover:not(:disabled) {
		background: rgba(255, 255, 255, 0.12);
		color: #fff;
	}
	&.active {
		background: rgba(255, 255, 255, 0.22);
		color: #fff;
		font-weight: 600;
	}
	&:disabled {
		opacity: 0.35;
		cursor: default;
	}

	&.icon-only-btn {
		padding: 0.32rem 0.42rem;
	}

	&.close-btn:hover {
		background: rgba(239, 68, 68, 0.25);
		color: #ef4444;
	}
}

@media (max-width: 680px) {
	.toolbar-btn span {
		display: none;
	}
	.toolbar-btn {
		padding: 0.32rem 0.42rem;
	}
	.toolbar-actions {
		gap: 0.2rem;
	}
	.toolbar-group {
		gap: 0.1rem;
		padding: 0.12rem;
	}
}

.menu-wrapper {
	position: relative;
}

.power-menu {
	position: absolute;
	top: calc(100% + 0.45rem);
	left: 0;
	z-index: 1000;
	background: #1e1e24;
	border: 1px solid rgba(255, 255, 255, 0.14);
	border-radius: 10px;
	box-shadow: 0 16px 36px rgba(0, 0, 0, 0.55);
	padding: 0.35rem;
	min-width: 11rem;
	display: flex;
	flex-direction: column;
	gap: 0.15rem;
}

.keys-menu {
	left: 0 !important;
	right: auto !important;
	min-width: 12rem;
}

.power-menu-item {
	display: flex;
	align-items: center;
	gap: 0.5rem;
	border: none;
	background: none;
	color: #fff;
	font-family: inherit;
	font-size: 0.8rem;
	padding: 0.4rem 0.5rem;
	border-radius: 5px;
	cursor: pointer;
	text-align: left;

	&:hover {
		background: rgba(255, 255, 255, 0.08);
	}
	&.active {
		background: rgba(255, 255, 255, 0.14);
		font-weight: 600;
	}
}

.quality-menu {
	min-width: 14rem;
	left: auto !important;
	right: 0 !important;
}

.quality-menu-item {
	align-items: flex-start;
	gap: 0.6rem;
	padding: 0.5rem;
}

.quality-menu-text {
	display: flex;
	flex-direction: column;
	gap: 0.1rem;
	min-width: 0;
}

.quality-menu-title {
	font-size: 0.8rem;
	font-weight: 600;
}

.quality-menu-desc {
	font-size: 0.68rem;
	color: rgba(255, 255, 255, 0.5);
	white-space: normal;
}

.quality-menu-check {
	margin-left: auto;
	flex-shrink: 0;
	color: #48c774;
}

/* Device Menu / Display Menu Dropdown */
.device-menu {
	position: absolute;
	top: calc(100% + 0.35rem);
	right: 0;
	z-index: 1000;
	background: #262626;
	border: 1px solid rgba(255, 255, 255, 0.12);
	border-radius: 12px;
	box-shadow: 0 12px 32px rgba(0, 0, 0, 0.5);
	padding: 0.75rem;
	width: 21rem;
	max-width: calc(100vw - 2rem);
	overflow: visible !important;
	display: flex;
	flex-direction: column;
	gap: 0.35rem;
}

.display-dropdown-menu {
	left: auto !important;
	right: 0 !important;
}

.device-menu-header-row {
	display: flex;
	align-items: center;
	justify-content: space-between;
	margin-bottom: 0.2rem;
}

.header-title-group {
	display: flex;
	align-items: center;
	gap: 0.45rem;

	.device-menu-title {
		margin: 0;
	}
}

.active-resolution-badge {
	font-size: 0.68rem;
	font-family: monospace;
	font-weight: 600;
	color: #60a5fa;
	background: rgba(37, 99, 235, 0.18);
	padding: 0.12rem 0.4rem;
	border-radius: 4px;
}

.device-menu-title {
	font-size: 0.72rem;
	font-weight: 700;
	text-transform: uppercase;
	letter-spacing: 0.04em;
	color: rgba(255, 255, 255, 0.5);
	margin: 0 0 0.25rem;
}

.device-menu-title-divided {
	margin-top: 0.5rem;
	padding-top: 0.5rem;
	border-top: 1px solid rgba(255, 255, 255, 0.08);
}

.device-menu-hint {
	font-size: 0.72rem;
	color: rgba(255, 255, 255, 0.45);
	margin: 0.1rem 0 0.3rem;
}

.device-menu-scrollable {
	display: flex;
	flex-direction: column;
	gap: 0.25rem;
	max-height: 12rem;
	overflow-y: auto;
	scrollbar-width: thin;
	scrollbar-color: rgba(255, 255, 255, 0.2) transparent;

	&::-webkit-scrollbar {
		width: 5px;
	}
	&::-webkit-scrollbar-thumb {
		background: rgba(255, 255, 255, 0.2);
		border-radius: 4px;
	}
}

.device-menu-row {
	display: flex;
	align-items: center;
	gap: 0.65rem;
	padding: 0.45rem 0.55rem;
	border-radius: 8px;
	cursor: pointer;
	color: #fff;
	font-size: 0.78rem;
	transition: background 0.12s ease;

	&:hover:not(.disabled) {
		background: rgba(255, 255, 255, 0.06);
	}
	&.active {
		background: rgba(37, 99, 235, 0.15);
	}
	&.disabled {
		opacity: 0.5;
		cursor: default;
	}
}

.auto-match-row {
	background: rgba(59, 130, 246, 0.08);
	border: 1px solid rgba(59, 130, 246, 0.22);
}

.device-row-icon {
	flex-shrink: 0;
	width: 1.9rem;
	height: 1.9rem;
	border-radius: 50%;
	display: flex;
	align-items: center;
	justify-content: center;
	background: rgba(255, 255, 255, 0.08);
	color: rgba(255, 255, 255, 0.5);

	&.active {
		background: rgba(37, 99, 235, 0.25);
		color: #60a5fa;
	}
}

.network-row-details {
	flex: 1 1 auto;
	min-width: 0;
	display: flex;
	flex-direction: column;
	gap: 0.1rem;
}

.network-row-label {
	font-size: 0.78rem;
	font-weight: 600;
	color: #fff;
}

.network-row-meta {
	font-size: 0.68rem;
	color: rgba(255, 255, 255, 0.5);
}

.device-menu-desc {
	flex: 1 1 auto;
	min-width: 0;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}

.custom-res-row {
	display: flex;
	align-items: center;
	gap: 0.4rem;
	margin-top: 0.2rem;
}

.custom-res-field {
	flex: 1;
	width: 4rem;
	background: rgba(0, 0, 0, 0.35);
	border: 1px solid rgba(255, 255, 255, 0.15);
	border-radius: 6px;
	padding: 0.35rem 0.5rem;
	color: #fff;
	font-size: 0.75rem;
	font-family: monospace;

	&:focus {
		outline: none;
		border-color: rgba(59, 130, 246, 0.6);
	}
}

.custom-res-multiply {
	color: rgba(255, 255, 255, 0.4);
	font-weight: 600;
}

.custom-res-btn {
	border: none;
	background: rgba(255, 255, 255, 0.18);
	color: #fff;
	font-family: inherit;
	font-size: 0.75rem;
	font-weight: 600;
	padding: 0.35rem 0.8rem;
	border-radius: 6px;
	cursor: pointer;
	transition: background 0.14s ease;

	&:hover:not(:disabled) {
		background: rgba(255, 255, 255, 0.28);
	}
	&:disabled {
		opacity: 0.4;
		cursor: default;
	}
}

/* Console Screen */
.console-screen {
	flex: 1 1 auto;
	min-height: 0;
	position: relative;
	overflow: hidden;

	&.is-scrollable {
		overflow: auto;
	}
}

.console-status {
	position: absolute;
	top: 50%;
	left: 50%;
	transform: translate(-50%, -50%);
	display: flex;
	flex-direction: column;
	align-items: center;
	gap: 0.75rem;
	color: rgba(255, 255, 255, 0.7);

	::v-deep .icon {
		width: 2.25rem;
		height: 2.25rem;
	}
}

.reconnect-btn {
	border: none;
	background: #3273dc;
	color: #fff;
	font-family: inherit;
	font-size: 0.8rem;
	font-weight: 600;
	padding: 0.5rem 1rem;
	border-radius: 6px;
	cursor: pointer;

	&:hover {
		background: #2366d1;
	}
}

/* Floating On-Screen Keyboard */
.on-screen-keyboard {
	position: fixed !important;
	left: 50%;
	bottom: 2rem;
	transform: translateX(-50%);
	z-index: 100000 !important;
	display: flex;
	flex-direction: column;
	gap: 0.3rem;
	width: fit-content;
	max-width: calc(100vw - 2rem);
	overflow-x: auto;
	padding: 0.5rem 0.75rem 0.75rem;
	background: rgba(38, 38, 38, 0.95);
	backdrop-filter: blur(16px);
	-webkit-backdrop-filter: blur(16px);
	border: 1px solid rgba(255, 255, 255, 0.16);
	border-radius: 14px;
	box-shadow: 0 20px 50px rgba(0, 0, 0, 0.65), 0 0 0 1px rgba(255, 255, 255, 0.08);
	user-select: none;
}

.osk-header {
	display: flex;
	align-items: center;
	gap: 0.4rem;
	padding-bottom: 0.35rem;
	margin-bottom: 0.15rem;
	border-bottom: 1px solid rgba(255, 255, 255, 0.1);
	color: rgba(255, 255, 255, 0.6);
	cursor: move;
}

.osk-title {
	font-size: 0.72rem;
	font-weight: 600;
	text-transform: uppercase;
	letter-spacing: 0.03em;
	flex: 1 1 auto;
}

.osk-close {
	border: none;
	background: transparent;
	color: rgba(255, 255, 255, 0.6);
	cursor: pointer;
	display: flex;
	padding: 0.15rem;
	border-radius: 4px;

	&:hover {
		background: rgba(255, 255, 255, 0.1);
		color: #fff;
	}
}

.osk-keys {
	display: flex;
	gap: 0.6rem;
}

.osk-alpha {
	display: flex;
	flex-direction: column;
	gap: 0.25rem;
}

.osk-side {
	display: flex;
	flex-direction: column;
	gap: 0.25rem;
}

.osk-fn-spacer {
	height: 2.3rem;
	visibility: hidden;
}

.osk-side-fill {
	flex: 1 1 auto;
	min-height: 0.25rem;
}

.osk-shortcuts-col {
	display: flex;
	flex-direction: column;
	justify-content: flex-end;
	gap: 0.25rem;
}

.osk-shortcut-btn {
	display: flex;
	align-items: center;
	justify-content: center;
}

.osk-row {
	display: flex;
	gap: 0.25rem;
}

.osk-key {
	flex: 0 0 auto;
	border: none;
	border-bottom: 2px solid rgba(0, 0, 0, 0.35);
	background: #454545;
	color: #fff;
	font-family: inherit;
	font-size: 0.75rem;
	padding: 0.55rem 0.25rem;
	border-radius: 5px;
	cursor: pointer;
	min-width: 0;

	&:hover {
		background: #4f4f4f;
	}
	&:active {
		background: #3a3a3a;
		border-bottom-width: 0;
		transform: translateY(2px);
	}
	&.osk-key-empty {
		visibility: hidden;
		pointer-events: none;
	}
	&.active {
		background: #7a7a7a;
		border-bottom-color: rgba(0, 0, 0, 0.25);
	}
}

/* Console Statusbar */
.console-statusbar {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	gap: 1rem;
	padding: 0.35rem 0.85rem;
	background: #1a1a1a;
	border-top: 1px solid rgba(255, 255, 255, 0.08);
	font-size: 0.72rem;
	color: rgba(255, 255, 255, 0.55);
	flex-wrap: wrap;

	::v-deep .icon {
		width: 1rem;
		height: 1rem;
		margin-right: 0.25rem;
	}
}

.statusbar-item {
	display: flex;
	align-items: center;
	white-space: nowrap;
}

.activity-dot {
	width: 6px;
	height: 6px;
	border-radius: 50%;
	background: rgba(255, 255, 255, 0.3);
	margin-right: 0.4rem;
	flex-shrink: 0;
}

.statusbar-item.is-live .activity-dot {
	background: #48c774;
	box-shadow: 0 0 4px #48c774;
}

/* Paste Modal */
.paste-modal-content {
	padding: 1rem;
	display: flex;
	flex-direction: column;
	gap: 0.75rem;
}

.paste-modal-desc {
	font-size: 0.8rem;
	color: rgba(255, 255, 255, 0.7);
	margin: 0;
}

.paste-modal-textarea {
	width: 100%;
	background: rgba(0, 0, 0, 0.45);
	border: 1px solid rgba(255, 255, 255, 0.15);
	border-radius: 8px;
	padding: 0.6rem;
	color: #fff;
	font-family: monospace;
	font-size: 0.82rem;
	resize: vertical;

	&:focus {
		outline: none;
		border-color: rgba(255, 255, 255, 0.4);
	}
}

.paste-modal-actions {
	display: flex;
	align-items: center;
	justify-content: flex-end;
	gap: 0.5rem;
}

.paste-action-btn {
	display: inline-flex;
	align-items: center;
	gap: 0.35rem;
	border: none;
	border-radius: 6px;
	padding: 0.4rem 0.85rem;
	font-family: inherit;
	font-size: 0.78rem;
	font-weight: 600;
	cursor: pointer;
	background: rgba(255, 255, 255, 0.12);
	color: #fff;
	transition: background 0.15s ease;

	&:hover:not(:disabled) {
		background: rgba(255, 255, 255, 0.22);
	}
	&.is-primary {
		background: rgba(255, 255, 255, 0.22);
		&:hover:not(:disabled) {
			background: rgba(255, 255, 255, 0.32);
		}
	}
	&:disabled {
		opacity: 0.4;
		cursor: default;
	}
}
</style>
