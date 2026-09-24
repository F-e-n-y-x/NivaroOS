<!-- src/shell/desktop/HostDesktopPanel.vue -->
<!--
	Host PC / Server Desktop Console.
	Streams the host machine's own X11 desktop into the NivaroOS desktop
	window or a standalone browser tab via noVNC (RFB), over vm-sidecar's
	authenticated /host/console WebSocket (which proxies to x11vnc's
	root-only unix socket - there is no raw VNC port).
	Follows the UI architecture, design tokens, and components of VmConsolePanel:
	- Console toolbar with identity, status pill, and grouped action bars
	- Draggable floating on-screen keyboard
	- Keys shortcut menu (Ctrl+Alt+Del, Win Key, Alt+Tab, Ctrl+Shift+Esc, Alt+F4)
	- Quick clipboard paste modal (with keystroke typing and clipboard paste)
	- Display resolution changer (real xrandr modes, match window, custom WxH)
	- View scaling toggle (Fit to Window vs 1:1 Actual Size)
	- Quality presets and stream settings (x11vnc refresh behaviour)
	- Fullscreen and Open in New Tab actions
	- Statusbar with connection, resolution, quality, scale, and display info
-->
<template>
	<div ref="root" class="host-desktop-root">
		<!-- Session expired: the token could not be refreshed. -->
		<div v-if="authExpired" class="host-desktop-panel host-desktop-not-installed">
			<div class="not-installed-card" role="alert">
				<b-icon icon="account-lock-outline" custom-size="mdi-36px"></b-icon>
				<h3>{{ $t('Your session expired - sign in again') }}</h3>
				<b-button type="is-primary" @click="checkAgain">{{ $t('Check again') }}</b-button>
			</div>
		</div>

		<div v-else-if="installed === false" class="host-desktop-panel host-desktop-not-installed">
			<div class="not-installed-card">
				<b-icon icon="monitor-off" custom-size="mdi-36px"></b-icon>
				<h3>{{ $t('Host Desktop Streaming is not installed') }}</h3>
				<p>{{ $t('Install it to stream this machine\'s own physical desktop over VNC.') }}</p>
				<b-button type="is-primary" :loading="installing" @click="installHostDesktop">
					{{ $t('Install') }}
				</b-button>
				<p v-if="installing" class="not-installed-hint">{{ $t('Installing packages - this can take a few minutes.') }}</p>
				<p v-if="installError" class="not-installed-error" role="alert">{{ installError }}</p>
				<details v-if="installLog" class="install-log">
					<summary>{{ $t('Show install log') }}</summary>
					<pre>{{ installLog }}</pre>
				</details>
			</div>
		</div>

		<div v-else-if="installChecked && needsDesktopChoice" class="host-desktop-panel host-desktop-not-installed">
			<div class="not-installed-card de-card">
				<b-icon icon="monitor-dashboard" custom-size="mdi-36px"></b-icon>
				<h3>{{ $t('No compatible desktop to stream') }}</h3>
				<p v-if="deState === 'no_de'">
					{{ $t('This machine has no desktop environment installed yet.') }}
				</p>
				<p v-else-if="deState === 'wayland_session'">
					{{ $t('{de} is currently running a Wayland session. Log out on the server and choose the "Xorg" or "X11" session at the login screen, then check again.', { de: deDisplayName || $t('Your desktop') }) }}
				</p>
				<p v-else-if="deState === 'unsupported'">
					{{ deReason || $t('Host Desktop streaming is not supported on this system.') }}
				</p>
				<p v-else>
					{{ $t('{de} is running under Wayland, which Host Desktop cannot capture - only an X11 session can be streamed.', { de: deDisplayName || $t('Your desktop') }) }}
				</p>
				<p v-if="deReason && deState !== 'unsupported'" class="not-installed-hint">{{ deReason }}</p>

				<div v-if="canProvision" class="de-choice-list">
					<button
						v-if="deState === 'wayland_only' && deX11Companion"
						type="button"
						class="de-choice-btn"
						@click="chooseDesktop('x11-companion')"
					>
						{{ $t('Add an X11 session to your existing {de} (recommended)', { de: deDisplayName }) }}
					</button>
					<button type="button" class="de-choice-btn" @click="chooseDesktop('alongside', 'xfce')">
						{{ $t('Install XFCE alongside (lightweight, most compatible)') }}
					</button>
					<button type="button" class="de-choice-btn" @click="chooseDesktop('alongside', 'cinnamon')">
						{{ $t('Install Cinnamon alongside (modern look)') }}
					</button>
					<button type="button" class="de-choice-btn" @click="chooseDesktop('alongside', 'mate')">
						{{ $t('Install MATE alongside (lightweight, classic look)') }}
					</button>
					<button
						v-if="deState === 'wayland_only' && deName"
						type="button"
						class="de-choice-btn de-choice-destructive"
						@click="openReplaceDesktopPrompt"
					>
						{{ $t('Replace {de} entirely (destructive)', { de: deDisplayName }) }}
					</button>
				</div>

				<div v-if="showReplacePrompt && canProvision" class="de-replace-confirm">
					<p>{{ $t('This removes {de} and installs the desktop you pick next. Type its name to confirm:', { de: deName }) }}</p>
					<div class="de-replace-row">
						<label class="sr-only" for="hd-replace-choice">{{ $t('Desktop to install') }}</label>
						<select id="hd-replace-choice" v-model="replaceChoice" class="de-replace-select">
							<option value="xfce">XFCE</option>
							<option value="cinnamon">Cinnamon</option>
							<option value="mate">MATE</option>
						</select>
						<label class="sr-only" for="hd-replace-confirm">{{ $t('Type {de} to confirm', { de: deName }) }}</label>
						<input
							id="hd-replace-confirm"
							v-model="replaceConfirmText"
							type="text"
							:placeholder="deName"
							class="de-replace-input"
							autocomplete="off"
						/>
						<b-button type="is-danger" :disabled="replaceConfirmText !== deName" @click="confirmReplaceDesktop">
							{{ $t('Replace') }}
						</b-button>
					</div>
				</div>

				<!-- Standalone tab has no window manager to open a terminal in:
				     show the exact command to run instead. -->
				<div v-if="provisionCommand" class="de-command-box">
					<p v-if="standalone">{{ $t('Run this command in a terminal on the server (or open the Terminal app from the NivaroOS dashboard):') }}</p>
					<p v-else>{{ $t('A terminal opened with this command. When it finishes, check again:') }}</p>
					<div class="de-command-row">
						<code class="de-command">{{ provisionCommand }}</code>
						<button type="button" class="de-choice-btn de-copy-btn" :aria-label="$t('Copy command')" @click="copyProvisionCommand">
							<b-icon icon="content-copy" size="is-small"></b-icon>
						</button>
					</div>
					<a v-if="standalone" class="de-dashboard-link" href="/#/" target="_blank" rel="noopener">{{ $t('Open NivaroOS dashboard') }}</a>
				</div>

				<div class="de-footer-actions">
					<b-button :loading="checkingAgain" @click="checkAgain">{{ $t('Check again') }}</b-button>
					<b-button v-if="deState !== 'unsupported'" type="is-text" class="de-try-anyway" @click="tryAnyway">
						{{ $t('Try anyway') }}
					</b-button>
				</div>
				<p v-if="installError" class="not-installed-error" role="alert">{{ installError }}</p>
			</div>
		</div>

		<div v-else-if="installChecked" class="host-desktop-panel">
			<!-- Main Console Toolbar -->
			<div class="console-toolbar">
				<!-- Identity / Status -->
				<div class="vm-identity">
					<b-icon icon="monitor" custom-size="mdi-18px"></b-icon>
					<span class="vm-name">{{ $t('Host Desktop') }}</span>
					<span class="status-pill" :class="'is-' + status" aria-live="polite">{{ statusText }}</span>
					<span v-if="status === 'connected' && pingMs !== null" class="ping-pill" :class="pingClass" :title="$t('Round-trip time to this host')">
						<b-icon icon="wifi" custom-size="mdi-14px"></b-icon>
						{{ pingMs }} ms
					</span>
				</div>

				<div class="toolbar-actions">
					<!-- Segment 1: Input Controls -->
					<div class="toolbar-group">
						<button
							type="button"
							class="toolbar-btn icon-only-btn"
							:class="{ active: keyboardOpen }"
							:title="$t('On-Screen Keyboard')"
							:aria-label="$t('On-Screen Keyboard')"
							:aria-pressed="keyboardOpen ? 'true' : 'false'"
							@click="toggleKeyboard"
						>
							<b-icon icon="keyboard-outline" custom-size="mdi-16px"></b-icon>
						</button>

						<div ref="keysMenuWrapper" class="menu-wrapper">
							<button
								type="button"
								class="toolbar-btn"
								:title="$t('Send Key Shortcuts')"
								:aria-label="$t('Send Key Shortcuts')"
								aria-haspopup="menu"
								:aria-expanded="keysMenuOpen ? 'true' : 'false'"
								@click="keysMenuOpen = !keysMenuOpen"
							>
								<b-icon icon="keyboard-settings-outline" custom-size="mdi-16px"></b-icon>
								<span>{{ $t('Keys') }}</span>
								<b-icon icon="chevron-down" custom-size="mdi-14px"></b-icon>
							</button>
							<div v-if="keysMenuOpen" class="power-menu keys-menu" role="menu">
								<button type="button" role="menuitem" class="power-menu-item" @click="sendCtrlAltDel(); keysMenuOpen = false">
									<b-icon icon="apple-keyboard-control" custom-size="mdi-16px"></b-icon>
									<span>Ctrl+Alt+Del</span>
								</button>
								<button type="button" role="menuitem" class="power-menu-item" @click="sendWinKey(); keysMenuOpen = false">
									<b-icon icon="microsoft-windows" custom-size="mdi-16px"></b-icon>
									<span>{{ $t('Win Key') }}</span>
								</button>
								<button type="button" role="menuitem" class="power-menu-item" @click="sendAltTab(); keysMenuOpen = false">
									<b-icon icon="tab" custom-size="mdi-16px"></b-icon>
									<span>Alt + Tab</span>
								</button>
								<button type="button" role="menuitem" class="power-menu-item" @click="sendCtrlShiftEsc(); keysMenuOpen = false">
									<b-icon icon="chart-line" custom-size="mdi-16px"></b-icon>
									<span>Ctrl+Shift+Esc</span>
								</button>
								<button type="button" role="menuitem" class="power-menu-item" @click="sendAltF4(); keysMenuOpen = false">
									<b-icon icon="close-box-outline" custom-size="mdi-16px"></b-icon>
									<span>Alt + F4</span>
								</button>
							</div>
						</div>

						<div class="menu-wrapper">
							<button
								type="button"
								class="toolbar-btn icon-only-btn"
								data-rclip-toggle
								:class="{ active: clipboardOpen }"
								:title="$t('Clipboard: paste and history')"
								:aria-label="$t('Clipboard: paste and history')"
								:aria-expanded="clipboardOpen ? 'true' : 'false'"
								aria-haspopup="dialog"
								@click="clipboardOpen = !clipboardOpen"
							>
								<b-icon icon="clipboard-text-multiple-outline" custom-size="mdi-16px"></b-icon>
							</button>
							<remote-clipboard-panel
								v-if="clipboardOpen"
								:target="clipboardTarget"
								:target-label="$t('Host')"
								:connected="status === 'connected'"
								:sync-hint="clipboardHint"
								@send="clipboardSend"
								@type="clipboardType"
								@close="closeClipboard"
							></remote-clipboard-panel>
						</div>
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
								:aria-label="$t('Display Resolution & Size')"
								aria-haspopup="menu"
								:aria-expanded="displayMenuOpen ? 'true' : 'false'"
								@click="displayMenuOpen = !displayMenuOpen"
							>
								<b-icon icon="monitor-screenshot" custom-size="mdi-16px"></b-icon>
								<span>{{ resolutionLabel }}</span>
								<b-icon icon="chevron-down" custom-size="mdi-14px"></b-icon>
							</button>

							<div v-if="displayMenuOpen" class="device-menu display-dropdown-menu" role="menu">
								<div class="device-menu-header-row">
									<div class="header-title-group">
										<b-icon icon="monitor-screenshot" size="is-small"></b-icon>
										<p class="device-menu-title">{{ $t('Display Resolution') }}</p>
									</div>
									<span class="active-resolution-badge">{{ resolutionLabel }}</span>
								</div>
								<p class="device-menu-hint">{{ $t('Dynamically change host physical display resolution.') }}</p>
								<p v-if="displayError" class="device-menu-hint device-menu-error" role="alert">{{ displayError }}</p>

								<!-- Auto Match Window Option -->
								<button
									type="button"
									role="menuitem"
									class="device-menu-row auto-match-row"
									:class="{ disabled: resizingHost }"
									:disabled="resizingHost"
									@click="matchWindowResolution"
								>
									<span class="device-row-icon active">
										<b-icon icon="arrow-expand-all" size="is-small"></b-icon>
									</span>
									<span class="network-row-details">
										<span class="network-row-label">{{ $t('Match Current Window') }}</span>
										<span class="network-row-meta">{{ currentWindowEstimate }}</span>
									</span>
									<b-icon v-if="resizingHost" icon="loading" custom-class="mdi-spin" size="is-small"></b-icon>
								</button>

								<p class="device-menu-title device-menu-title-divided">
									{{ displayHeadless ? $t('Preset Resolutions') : $t('Available Modes') }}
								</p>
								<div class="device-menu-scrollable">
									<button
										v-for="r in availableResolutions"
										:key="r.width + 'x' + r.height"
										type="button"
										role="menuitemradio"
										:aria-checked="currentResolution === `${r.width}x${r.height}` ? 'true' : 'false'"
										class="device-menu-row"
										:class="{ active: currentResolution === `${r.width}x${r.height}` }"
										:disabled="resizingHost"
										@click="changeResolution(r.width, r.height)"
									>
										<span class="device-row-icon" :class="{ active: currentResolution === `${r.width}x${r.height}` }">
											<b-icon icon="monitor" size="is-small"></b-icon>
										</span>
										<span class="device-menu-desc">{{ r.label || `${r.width} x ${r.height}` }}</span>
										<b-icon
											v-if="currentResolution === `${r.width}x${r.height}`"
											icon="check"
											size="is-small"
											custom-class="has-text-success"
										></b-icon>
									</button>
									<p v-if="!availableResolutions.length" class="device-menu-hint">{{ $t('No display modes reported.') }}</p>
								</div>

								<p class="device-menu-title device-menu-title-divided">{{ $t('Custom Resolution') }}</p>
								<div class="custom-res-row" @click.stop>
									<label class="sr-only" for="hd-custom-width">{{ $t('Width') }}</label>
									<input
										id="hd-custom-width"
										v-model.number="customWidth"
										type="number"
										class="custom-res-field"
										placeholder="1920"
										min="640"
										max="7680"
									/>
									<span class="custom-res-multiply" aria-hidden="true">×</span>
									<label class="sr-only" for="hd-custom-height">{{ $t('Height') }}</label>
									<input
										id="hd-custom-height"
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
							:aria-label="scaleToFit ? $t('Show actual size (1:1)') : $t('Scale to fit window')"
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
								:aria-label="$t('Display & Bandwidth Quality')"
								aria-haspopup="menu"
								:aria-expanded="qualityMenuOpen ? 'true' : 'false'"
								@click="qualityMenuOpen = !qualityMenuOpen"
							>
								<b-icon icon="speedometer" custom-size="mdi-16px"></b-icon>
								<span>{{ qualityModeLabel }}</span>
								<b-icon icon="chevron-down" custom-size="mdi-14px"></b-icon>
							</button>
							<div v-if="qualityMenuOpen" class="power-menu quality-menu" role="menu">
								<button
									v-for="q in qualityOptions"
									:key="q.mode"
									type="button"
									role="menuitemradio"
									:aria-checked="qualityMode === q.mode ? 'true' : 'false'"
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

								<div class="stream-settings" @click.stop>
									<p class="device-menu-title device-menu-title-divided">{{ $t('Stream Settings') }}</p>
									<label class="stream-setting-row" for="hd-noxdamage">
										<input id="hd-noxdamage" v-model="streamSettings.noxdamage" type="checkbox" :disabled="savingSettings" />
										<span>{{ $t('Disable X damage (for compositors)') }}</span>
									</label>
									<label class="stream-setting-row" for="hd-fixscreen">
										<span>{{ $t('Periodic full refresh (fixes stale patches)') }}</span>
										<select id="hd-fixscreen" v-model.number="streamSettings.fixscreen" class="stream-setting-select" :disabled="savingSettings">
											<option :value="0">{{ $t('Off') }}</option>
											<option :value="5">5 s</option>
											<option :value="15">15 s</option>
											<option :value="60">60 s</option>
										</select>
									</label>
									<button type="button" class="custom-res-btn stream-settings-save" :disabled="savingSettings" @click="saveStreamSettings">
										<b-icon v-if="savingSettings" icon="loading" custom-class="mdi-spin" size="is-small"></b-icon>
										<span v-else>{{ $t('Apply & restart stream') }}</span>
									</button>
								</div>
							</div>
						</div>

						<!-- Fullscreen Action -->
						<button
							type="button"
							class="toolbar-btn icon-only-btn"
							:title="$t('Fullscreen')"
							:aria-label="$t('Fullscreen')"
							@click="toggleFullscreen"
						>
							<b-icon icon="fullscreen" custom-size="mdi-16px"></b-icon>
						</button>

						<!-- Open Standalone Tab Action -->
						<button
							v-if="!standalone"
							type="button"
							class="toolbar-btn icon-only-btn"
							:title="$t('Open in New Tab')"
							:aria-label="$t('Open in New Tab')"
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
						:aria-label="$t('Close')"
						@click="closePanel"
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

			<!-- Changing the host's actual display mode mid-stream produces a few
			     garbled/corrupted frames while x11vnc catches up to the new
			     framebuffer size - this covers that transition instead of
			     showing it. -->
			<div v-if="resizingHost" class="console-resizing-overlay">
				<b-icon icon="loading" custom-class="mdi-spin" custom-size="mdi-36px"></b-icon>
				<span>{{ $t('Adjusting display resolution...') }}</span>
			</div>

			<!-- Disconnected / Reconnecting Overlay -->
			<div v-if="status !== 'connected'" class="console-status" aria-live="polite">
				<b-icon v-if="status === 'connecting'" icon="loading" custom-class="mdi-spin" custom-size="mdi-36px"></b-icon>
				<b-icon v-else icon="lan-disconnect" custom-size="mdi-36px"></b-icon>
				<span>{{ statusText }}</span>
				<span v-if="connectError" class="console-status-reason">{{ connectError }}</span>
				<span v-else-if="status === 'disconnected' && serviceStatus && serviceStatus.reason" class="console-status-reason">
					{{ serviceStatus.reason }}
				</span>
				<div v-if="status === 'disconnected'" class="console-status-actions">
					<button type="button" class="reconnect-btn" @click="manualReconnect">
						{{ $t('Reconnect') }}
					</button>
					<button
						v-if="serviceStatus && serviceStatus.installed && !serviceStatus.service_active"
						type="button"
						class="reconnect-btn is-secondary"
						:disabled="restartingService"
						@click="restartService"
					>
						<b-icon v-if="restartingService" icon="loading" custom-class="mdi-spin" size="is-small"></b-icon>
						<span>{{ $t('Restart service') }}</span>
					</button>
					<button type="button" class="reconnect-btn is-secondary" @click="checkAgain">
						{{ $t('Check again') }}
					</button>
				</div>
			</div>

			<!-- Non-blocking "host copied text" chip (replaces the old dialog that
			     popped up and stole focus every time the host clipboard changed). -->
			<div v-if="clipChip.visible" class="host-clip-chip" role="status">
				<b-icon icon="clipboard-check-outline" size="is-small"></b-icon>
				<span>{{ clipChip.copied ? $t('Copied from Host clipboard') : $t('Host copied text') }}</span>
				<button v-if="!clipChip.copied" type="button" class="host-clip-chip-btn" @click="openClipboardFromChip">
					{{ $t('View') }}
				</button>
				<button type="button" class="host-clip-chip-btn" :aria-label="$t('Dismiss')" @click="hideClipChip">
					<b-icon icon="close" size="is-small"></b-icon>
				</button>
			</div>

			<!-- Floating Draggable On-Screen Keyboard -->
			<div
				v-show="keyboardVisible"
				ref="keyboard"
				class="on-screen-keyboard"
				:class="{ 'is-compact': compactKeyboard }"
				:style="keyboardStyle"
				role="dialog"
				:aria-label="$t('On-Screen Keyboard')"
			>
				<div class="osk-header" @pointerdown="startKeyboardDrag">
					<b-icon icon="drag-horizontal-variant" size="is-small"></b-icon>
					<span class="osk-title">{{ $t('Keyboard') }}</span>
					<button
						type="button"
						class="osk-close"
						:title="$t('Close')"
						:aria-label="$t('Close')"
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
								:aria-pressed="key.sticky ? (stickyState(key.sticky) ? 'true' : 'false') : null"
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
								:aria-hidden="key ? null : 'true'"
								:aria-label="key ? key.code : null"
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
							:aria-label="$t(s.label)"
							@click="sendShortcut(s.key)"
						>
							<b-icon :icon="s.icon" size="is-small"></b-icon>
						</button>
					</div>
				</div>
			</div>

			<!-- Statusbar at the Bottom -->
			<div class="console-statusbar">
				<button
					v-if="capsLockActive"
					type="button"
					class="statusbar-item statusbar-capslock"
					:title="$t('Caps Lock is on - click to turn off')"
					@click="disableCapsLock"
				>
					<b-icon icon="apple-keyboard-caps" size="is-small"></b-icon>
					{{ $t('Caps Lock') }}
				</button>
				<span class="statusbar-item" :class="{ 'is-live': status === 'connected' }">
					<span class="activity-dot"></span>{{ statusText }}
				</span>
				<span class="statusbar-item">
					<b-icon icon="monitor" size="is-small"></b-icon>
					{{ resolutionLabel }}
				</span>
				<span class="statusbar-item">
					<b-icon icon="speedometer" size="is-small"></b-icon>
					{{ qualityModeLabel }}
				</span>
				<span class="statusbar-item">
					<b-icon :icon="scaleToFit ? 'fit-to-page-outline' : 'aspect-ratio'" size="is-small"></b-icon>
					{{ scaleToFit ? $t('Fit') : $t('1:1') }}
				</span>
				<span v-if="hostDisplayName" class="statusbar-item">
					<b-icon icon="desktop-classic" size="is-small"></b-icon>
					{{ $t('Display {display}', { display: hostDisplayName }) }}
				</span>
			</div>

		</div>
		<div v-else class="host-desktop-panel host-desktop-checking" aria-live="polite">
			<span>{{ $t('Checking Host Desktop status...') }}</span>
		</div>
	</div>
</template>

<script>
import RFB from '@novnc/novnc'
import { instance as http } from '@/service/service'
import { apiBase as sidecarApiBase, wsBase as sidecarWsBase } from '@/api/vmSidecar'
import RemoteClipboardPanel from '@/shared/clipboard/RemoteClipboardPanel.vue'
import { record as recordClipboard, typeText } from '@/service/remoteClipboard'

const QUALITY_PRESETS = {
	high: { qualityLevel: 9, compressionLevel: 1 },
	balanced: { qualityLevel: 6, compressionLevel: 2 },
	low: { qualityLevel: 2, compressionLevel: 8 },
}

const DEFAULT_QUALITY = 'balanced'

const QUALITY_OPTIONS = [
	{ mode: 'high', icon: 'high-definition', label: 'High Quality', desc: 'Sharpest picture, most data' },
	{ mode: 'balanced', icon: 'tune-vertical', label: 'Balanced', desc: 'Good picture, moderate data' },
	{ mode: 'low', icon: 'speedometer-slow', label: 'Low Bandwidth', desc: 'Softer picture, least lag' },
]

const DE_DISPLAY_NAMES = {
	gnome: 'GNOME',
	plasma: 'KDE Plasma',
	xfce: 'Xfce',
	cinnamon: 'Cinnamon',
	mate: 'MATE',
	lxqt: 'LXQt',
	budgie: 'Budgie',
	deepin: 'Deepin',
	lxde: 'LXDE',
}

// The on-screen keyboard's host window id (see Dock.vue / ContextMenu.vue).
const HOST_DESKTOP_WINDOW_ID = 'host-desktop'

const INSTALL_TIMEOUT_MS = 15 * 60 * 1000

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

// Latched modifiers on the on-screen keyboard: data prop -> keysym/code.
const STICKY_MODIFIERS = {
	shiftActive: { keysym: 0xffe1, code: 'ShiftLeft' },
	ctrlActive: { keysym: 0xffe3, code: 'ControlLeft' },
	altActive: { keysym: 0xffe9, code: 'AltLeft' },
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

// Through the gateway's same-origin route (see api/vmSidecar.js), so Host
// Desktop works over https and behind a tunnel or reverse proxy too.
function sidecarUrl(path) {
	return `${sidecarApiBase()}${path}`
}

// The app instance's request interceptor adds a "Language" header, which
// vm-sidecar's CORS preflight doesn't allow (Content-Type, Authorization
// only) - strip it per request (axios gives each request its own copy of
// the header objects) while keeping the default JSON body transform.
function stripLanguageHeader(data, headers) {
	if (headers) {
		delete headers.Language
		if (headers.common) delete headers.common.Language
	}
	return data
}

function sidecarConfig(config) {
	const defaults = http.defaults.transformRequest
	return {
		...(config || {}),
		transformRequest: [stripLanguageHeader].concat(Array.isArray(defaults) ? defaults : defaults ? [defaults] : []),
	}
}

function errorMessage(e, fallback) {
	const d = e && e.response && e.response.data
	return (d && (d.error || d.message)) || fallback
}

function isUnauthorized(e) {
	return !!(e && e.response && e.response.status === 401)
}

export default {
	name: 'HostDesktopPanel',
	components: {
		RemoteClipboardPanel,
	},
	props: {
		showClose: {
			type: Boolean,
			default: false,
		},
		// True in the standalone browser tab (views/HostDesktopStandalone.vue),
		// which has no window manager - OPEN_WINDOW does nothing there.
		standalone: {
			type: Boolean,
			default: false,
		},
	},
	data() {
		return {
			rfb: null,
			status: 'connecting',
			reconnectAttempt: 0,
			reconnectTimer: null,
			capsLockPollTimer: null,
			pingMs: null,
			intentionalDisconnect: false,
			resizeSettleResolve: null,
			keysMenuOpen: false,
			qualityMenuOpen: false,
			displayMenuOpen: false,
			scaleToFit: true,
			qualityMode: DEFAULT_QUALITY,
			qualityOptions: QUALITY_OPTIONS,
			keyboardOpen: false,
			keyboardPos: null,
			compactKeyboard: false,
			shiftActive: false,
			capsLockActive: false,
			ctrlActive: false,
			altActive: false,
			clipboardOpen: false,
			clipChip: { visible: false, copied: false },
			clipChipTimer: null,
			clipboardInsecureWarned: false,
			currentResolution: '',
			availableResolutions: [],
			displayName: '',
			displayHeadless: false,
			displayError: '',
			resizingHost: false,
			currentWindowEstimate: '',
			customWidth: null,
			customHeight: null,
			keyboardRows: KEYBOARD_ROWS,
			navRows: NAV_ROWS,
			arrowRows: ARROW_ROWS,
			shortcuts: SHORTCUTS,
			installed: null,
			installChecked: false,
			installing: false,
			installError: '',
			installLog: '',
			authExpired: false,
			deChecked: false,
			deState: '',
			deName: '',
			deReason: '',
			deX11Companion: false,
			forceConnect: false,
			checkingAgain: false,
			provisionCommand: '',
			showReplacePrompt: false,
			replaceChoice: 'xfce',
			replaceConfirmText: '',
			serviceStatus: null,
			connectError: '',
			restartingService: false,
			streamSettings: { fixscreen: 0, noxdamage: true },
			savingSettings: false,
			connectedThisAttempt: false,
		}
	},
	computed: {
		needsDesktopChoice() {
			return !this.forceConnect && this.deChecked && this.deState !== '' && this.deState !== 'supported'
		},
		canProvision() {
			return this.deState === 'no_de' || this.deState === 'wayland_only'
		},
		deDisplayName() {
			if (!this.deName) return ''
			return DE_DISPLAY_NAMES[this.deName] || this.deName
		},
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
		resolutionLabel() {
			return this.currentResolution || '—'
		},
		clipboardTarget() {
			return { kind: 'host', name: 'Host' }
		},
		clipboardHint() {
			if (!window.isSecureContext) {
				return this.$t("Over http:// this browser won't share its clipboard automatically: use the box below, and Copy in the history.")
			}
			return ''
		},
		hostDisplayName() {
			return this.displayName || (this.serviceStatus && this.serviceStatus.display) || ''
		},
		qualityModeLabel() {
			const m = this.qualityOptions.find((q) => q.mode === this.qualityMode)
			return m ? this.$t(m.label) : this.qualityMode
		},
		pingClass() {
			if (this.pingMs === null) return ''
			if (this.pingMs < 80) return 'is-good'
			if (this.pingMs < 200) return 'is-ok'
			return 'is-bad'
		},
		// The desktop window this panel lives in (none in the standalone tab).
		minimized() {
			if (this.standalone || !this.$store || !this.$store.state.windows) return false
			const win = this.$store.state.windows.find((w) => w.id === HOST_DESKTOP_WINDOW_ID)
			return !!(win && win.minimized)
		},
		keyboardVisible() {
			return this.keyboardOpen && !this.minimized && this.status === 'connected'
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
		keyboardVisible(visible) {
			if (visible) {
				this.$nextTick(() => {
					this.placeKeyboard()
				})
			}
		},
		minimized(min) {
			if (min) {
				this.stopCapsLockPoll()
				this.closeMenus()
			} else if (this.status === 'connected') {
				this.startCapsLockPoll()
			}
		},
		displayMenuOpen(open) {
			if (open) {
				this.updateWindowEstimate()
				this.fetchHostDisplay()
			}
		},
		qualityMenuOpen(open) {
			if (open) this.fetchStreamSettings()
		},
	},
	mounted() {
		try {
			const savedQuality = localStorage.getItem('host_desktop_quality_mode')
			if (savedQuality && QUALITY_PRESETS[savedQuality]) {
				this.qualityMode = savedQuality
			}
		} catch (e) {}

		this.updateCompactKeyboard()
		this.checkInstalled()

		document.addEventListener('mousedown', this.onOutsideClick)
		document.addEventListener('keydown', this.onDocumentKeydown)
		document.addEventListener('fullscreenchange', this.onFullscreenChange)
		document.addEventListener('visibilitychange', this.onVisibilityChange)
		window.addEventListener('resize', this.onWindowResize)

		// Observe the stable root wrapper - the inner panel is swapped by
		// v-if (checking / not installed / console) and would orphan the
		// observer if it were observed directly.
		if (typeof ResizeObserver !== 'undefined' && this.$refs.root) {
			this.panelResizeObserver = new ResizeObserver(() => {
				this.clampKeyboardPos()
				this.updateWindowEstimate()
			})
			this.panelResizeObserver.observe(this.$refs.root)
		}
	},
	beforeDestroy() {
		this.releaseStickyModifiers()
		this.intentionalDisconnect = true
		this.clearReconnectTimer()
		if (this.rfb) {
			this.rfb.disconnect()
			this.rfb = null
		}
		document.removeEventListener('mousedown', this.onOutsideClick)
		document.removeEventListener('keydown', this.onDocumentKeydown)
		document.removeEventListener('fullscreenchange', this.onFullscreenChange)
		document.removeEventListener('visibilitychange', this.onVisibilityChange)
		window.removeEventListener('resize', this.onWindowResize)
		this.stopCapsLockPoll()
		this.hideClipChip()

		if (this.panelResizeObserver) {
			this.panelResizeObserver.disconnect()
			this.panelResizeObserver = null
		}
		if (this.$refs.keyboard && this.$refs.keyboard.parentNode) {
			this.$refs.keyboard.parentNode.removeChild(this.$refs.keyboard)
		}
	},
	methods: {
		// ── Sidecar requests (app axios instance: token + 401 refresh/retry) ──
		sidecarGet(path, config) {
			return http.get(sidecarUrl(path), sidecarConfig(config))
		},
		sidecarPost(path, data, config) {
			return http.post(sidecarUrl(path), data || {}, sidecarConfig(config))
		},
		sidecarPut(path, data, config) {
			return http.put(sidecarUrl(path), data || {}, sidecarConfig(config))
		},

		// Refreshes serviceStatus. Returns false only on an auth failure the
		// app's refresh flow couldn't recover from.
		async fetchServiceStatus() {
			try {
				const res = await this.sidecarGet('/host/desktop/status')
				this.serviceStatus = res.data || null
				if (this.serviceStatus && this.serviceStatus.display && !this.displayName) {
					this.displayName = this.serviceStatus.display
				}
				return true
			} catch (e) {
				if (isUnauthorized(e)) {
					this.authExpired = true
					return false
				}
				// Older sidecar without /status, or transient error: keep going.
				return true
			}
		},

		async connect() {
			this.clearReconnectTimer()
			if (this.rfb) {
				this.releaseStickyModifiers()
				this.intentionalDisconnect = true
				this.rfb.disconnect()
				this.rfb = null
			}
			this.intentionalDisconnect = false
			this.status = 'connecting'
			this.connectError = ''
			this.connectedThisAttempt = false

			// A cheap authenticated call first: if the access token is stale,
			// the app's interceptor refreshes it here, so the WebSocket below
			// (which can only carry the token as a query param, and can't be
			// retried by the interceptor) gets a fresh one.
			const ok = await this.fetchServiceStatus()
			if (!ok) {
				this.status = 'disconnected'
				this.intentionalDisconnect = true
				return
			}
			if (this.intentionalDisconnect) return

			// The console element only exists once the console branch of the
			// template has rendered (e.g. right after an install).
			await this.$nextTick()
			if (!this.$refs.screen) {
				await this.$nextTick()
			}
			if (!this.$refs.screen) {
				this.status = 'disconnected'
				return
			}

			let token = ''
			try {
				token = localStorage.getItem('access_token') || ''
			} catch (e) {}
			const url = `${sidecarWsBase()}/host/console?token=${encodeURIComponent(token)}`

			try {
				const rfb = new RFB(this.$refs.screen, url)
				this.rfb = rfb
				rfb.scaleViewport = this.scaleToFit
				rfb.resizeSession = false
				this.applyQualityPreset()

				rfb.addEventListener('connect', () => {
					if (this.rfb !== rfb) return
					this.status = 'connected'
					this.connectedThisAttempt = true
					this.connectError = ''
					this.reconnectAttempt = 0
					this.applyQualityPreset()
					if (!this.minimized) this.startCapsLockPoll()
					this.fetchHostDisplay()
				})

				rfb.addEventListener('disconnect', () => {
					if (this.rfb !== rfb) return
					// The host never saw our key-ups; reset the latches locally.
					this.shiftActive = false
					this.ctrlActive = false
					this.altActive = false
					this.status = 'disconnected'
					this.stopCapsLockPoll()
					this.pingMs = null
					this.rfb = null
					// Ask the sidecar why (a failed WebSocket upgrade's 502 body
					// isn't readable from the browser).
					this.fetchServiceStatus()
					if (!this.intentionalDisconnect) {
						this.scheduleReconnect()
					}
				})

				rfb.addEventListener('securityfailure', (e) => {
					const reason = e && e.detail && e.detail.reason
					this.connectError = reason
						? this.$t('The host desktop VNC server rejected the connection: {reason}', { reason })
						: this.$t('The host desktop VNC server rejected the connection.')
					this.intentionalDisconnect = true
				})

				rfb.addEventListener('credentialsrequired', () => {
					this.connectError = this.$t('The host desktop VNC server asked for a password. NivaroOS runs it without one behind its own sign-in - restart the Host Desktop service to restore its settings.')
					this.intentionalDisconnect = true
					try {
						rfb.disconnect()
					} catch (err) {}
				})

				rfb.addEventListener('clipboard', (e) => {
					const text = e.detail && e.detail.text
					if (!text) return
					this.receiveHostClipboard(text)
				})

				rfb.addEventListener('fbsize', (e) => {
					if (e.detail && e.detail.width && e.detail.height) {
						this.currentResolution = `${e.detail.width}x${e.detail.height}`
					}
					if (this.resizeSettleResolve) {
						this.resizeSettleResolve()
						this.resizeSettleResolve = null
					}
				})
			} catch (e) {
				this.status = 'disconnected'
				if (!this.intentionalDisconnect) {
					this.scheduleReconnect()
				}
			}
		},

		manualReconnect() {
			this.reconnectAttempt = 0
			this.connect()
		},

		applyQualityPreset() {
			if (!this.rfb) return
			const p = QUALITY_PRESETS[this.qualityMode] || QUALITY_PRESETS[DEFAULT_QUALITY]
			this.rfb.qualityLevel = p.qualityLevel
			this.rfb.compressionLevel = p.compressionLevel
		},

		clearReconnectTimer() {
			if (this.reconnectTimer) {
				clearTimeout(this.reconnectTimer)
				this.reconnectTimer = null
			}
		},

		// Exponential backoff (1s, 2s, 4s, 8s, capped at 15s) so a dropped
		// connection recovers on its own once the network/host comes back,
		// without hammering the server with rapid retries.
		scheduleReconnect() {
			this.clearReconnectTimer()
			if (this.authExpired) return
			const delay = Math.min(15000, 1000 * Math.pow(2, this.reconnectAttempt))
			this.reconnectAttempt += 1
			this.reconnectTimer = setTimeout(() => {
				this.reconnectTimer = null
				if (!this.intentionalDisconnect) {
					this.connect()
				}
			}, delay)
		},

		async checkInstalled() {
			try {
				const res = await this.sidecarGet('/host/desktop/installed')
				this.installed = !!(res.data && res.data.installed)
			} catch (e) {
				if (isUnauthorized(e)) {
					this.authExpired = true
					this.installChecked = true
					return
				}
				// Can't tell either way - assume installed rather than
				// blocking an otherwise-working panel behind a transient
				// network error unrelated to whether it's installed.
				this.installed = true
			}
			this.installChecked = true
			if (this.installed) {
				await this.checkDeStatus()
				if (!this.needsDesktopChoice) {
					this.connect()
					this.fetchHostDisplay()
				}
			}
		},

		// Re-runs the whole detection (installed -> desktop -> connect), e.g.
		// after provisioning a desktop or restarting the service.
		async checkAgain() {
			this.checkingAgain = true
			this.installChecked = false
			this.authExpired = false
			this.installError = ''
			this.forceConnect = false
			this.deChecked = false
			this.provisionCommand = ''
			this.intentionalDisconnect = true
			this.clearReconnectTimer()
			if (this.rfb) {
				this.releaseStickyModifiers()
				this.rfb.disconnect()
				this.rfb = null
			}
			this.reconnectAttempt = 0
			try {
				await this.checkInstalled()
			} finally {
				this.checkingAgain = false
			}
		},

		// Streaming (x11vnc) being installed and there being an X11 desktop
		// for it to actually capture are separate questions. If this check
		// itself is unavailable (older backend, transient error), treat it
		// as supported rather than blocking the panel.
		async checkDeStatus() {
			try {
				const res = await this.sidecarGet('/host/desktop/de-status')
				const d = res.data || {}
				this.deState = d.state || 'supported'
				this.deName = d.de_name || ''
				this.deReason = d.reason || ''
				this.deX11Companion = !!d.x11_companion_available
				if (d.display && !this.displayName) this.displayName = d.display
			} catch (e) {
				if (isUnauthorized(e)) this.authExpired = true
				this.deState = 'supported'
			}
			this.deChecked = true
		},

		async tryAnyway() {
			this.forceConnect = true
			await this.$nextTick()
			this.connect()
			this.fetchHostDisplay()
		},

		chooseDesktop(action, de, confirm) {
			this.installError = ''
			// No sudo prefix: the script re-execs itself with sudo when it
			// isn't already root (and sudo may not even exist when it is).
			const parts = [`bash /usr/local/bin/nivaroos-host-desktop-de-install.sh --action=${action}`]
			if (de) parts.push(`--de=${de}`)
			if (confirm) parts.push(`--confirm=${confirm}`)
			const command = parts.join(' ')
			this.provisionCommand = command
			if (this.standalone) return
			try {
				this.$store.commit('OPEN_WINDOW', {
					id: 'terminal-host-desktop-de-' + Date.now(),
					title: this.$t('Set Up Desktop'),
					component: 'TerminalPanel',
					props: { initCommand: command },
					width: 820,
					height: 480,
				})
			} catch (e) {
				this.installError = this.$t('Could not open a terminal - run the command above on the server instead.')
			}
		},

		async copyProvisionCommand() {
			const text = this.provisionCommand
			if (!text) return
			try {
				if (navigator.clipboard && navigator.clipboard.writeText) {
					await navigator.clipboard.writeText(text)
					this.$buefy.toast.open({ message: this.$t('Command copied'), type: 'is-success', duration: 2000 })
					return
				}
			} catch (e) {}
			this.$buefy.toast.open({
				message: this.$t('Select the command and copy it manually.'),
				type: 'is-warning',
				duration: 3000,
			})
		},

		openReplaceDesktopPrompt() {
			this.showReplacePrompt = true
			this.replaceConfirmText = ''
		},

		confirmReplaceDesktop() {
			if (this.replaceConfirmText !== this.deName) return
			this.chooseDesktop('replace', this.replaceChoice, this.deName)
			this.showReplacePrompt = false
		},

		async installHostDesktop() {
			this.installing = true
			this.installError = ''
			this.installLog = ''
			try {
				const res = await this.sidecarPost('/host/desktop/install', {}, { timeout: INSTALL_TIMEOUT_MS })
				const d = (res && res.data) || {}
				this.installLog = d.log || ''
				this.installed = !!d.installed
				if (this.installed) {
					const warnings = Array.isArray(d.warnings) ? d.warnings.filter(Boolean) : []
					if (warnings.length) {
						this.$buefy.toast.open({ message: warnings.join(' '), type: 'is-warning', duration: 6000 })
					}
					await this.checkDeStatus()
					if (!this.needsDesktopChoice) {
						// connect() waits for the console element to render.
						this.connect()
						this.fetchHostDisplay()
					}
				} else {
					this.installError = this.$t('Install finished, but the service did not come up - check the server logs.')
				}
			} catch (e) {
				if (isUnauthorized(e)) {
					this.authExpired = true
				}
				const d = e && e.response && e.response.data
				if (d && d.log) this.installLog = d.log
				this.installError = errorMessage(e, this.$t('Failed to install Host Desktop streaming'))
			} finally {
				this.installing = false
			}
		},

		async restartService() {
			this.restartingService = true
			try {
				const res = await this.sidecarPost('/host/desktop/restart')
				if (res && res.data) this.serviceStatus = res.data
				this.reconnectAttempt = 0
				this.connect()
			} catch (e) {
				if (isUnauthorized(e)) this.authExpired = true
				this.$buefy.toast.open({
					message: errorMessage(e, this.$t('Could not restart the Host Desktop service')),
					type: 'is-danger',
					duration: 4000,
				})
			} finally {
				this.restartingService = false
			}
		},

		async fetchStreamSettings() {
			try {
				const res = await this.sidecarGet('/host/desktop/settings')
				if (res && res.data) {
					this.streamSettings = {
						fixscreen: Number(res.data.fixscreen) || 0,
						noxdamage: res.data.noxdamage !== false,
					}
				}
			} catch (e) {}
		},

		async saveStreamSettings() {
			this.savingSettings = true
			try {
				const res = await this.sidecarPut('/host/desktop/settings', {
					fixscreen: Number(this.streamSettings.fixscreen) || 0,
					noxdamage: !!this.streamSettings.noxdamage,
				})
				if (res && res.data) {
					this.streamSettings = {
						fixscreen: Number(res.data.fixscreen) || 0,
						noxdamage: res.data.noxdamage !== false,
					}
				}
				this.qualityMenuOpen = false
				this.$buefy.toast.open({ message: this.$t('Stream settings saved - reconnecting'), type: 'is-success', duration: 2500 })
				// The service restarts to apply them; the resulting disconnect
				// reconnects on its own (backoff starts at 1s).
				this.reconnectAttempt = 0
			} catch (e) {
				if (isUnauthorized(e)) this.authExpired = true
				this.$buefy.toast.open({
					message: errorMessage(e, this.$t('Could not save stream settings')),
					type: 'is-danger',
					duration: 4000,
				})
			} finally {
				this.savingSettings = false
			}
		},

		async fetchHostDisplay() {
			try {
				const res = await this.sidecarGet('/host/display')
				const d = res.data || {}
				this.displayError = ''
				if (d.current) this.currentResolution = d.current
				this.availableResolutions = Array.isArray(d.resolutions) ? d.resolutions : []
				this.displayHeadless = !!d.headless
				if (d.display) this.displayName = d.display
			} catch (e) {
				if (isUnauthorized(e)) return
				this.displayError = errorMessage(e, this.$t('Could not read the host display.'))
			}
		},

		async changeResolution(width, height) {
			if (!width || !height || this.resizingHost) return
			this.resizingHost = true
			try {
				const res = await this.sidecarPost('/host/display', { width, height })
				const requested = `${width}x${height}`
				let applied = requested
				if (res && res.data) {
					applied = res.data.current || requested
					this.currentResolution = applied
					if (Array.isArray(res.data.resolutions)) this.availableResolutions = res.data.resolutions
				}
				this.displayMenuOpen = false
				const shown = applied.replace('x', '×')
				this.$buefy.toast.open({
					message:
						applied === requested
							? `${this.$t('Host Display Resolution')}: ${shown}`
							: this.$t("This display can't show {requested}, so it's set to {applied}", { requested: requested.replace('x', '×'), applied: shown }),
					type: applied === requested ? 'is-success' : 'is-info',
					duration: applied === requested ? 2500 : 5000,
				})
				// Changing the host's X server mode produces a few garbled
				// frames while x11vnc (via -xrandr resize) catches up. Wait for
				// noVNC's `fbsize` event - or a timeout - plus a short settle.
				await this.waitForResize(4000)
				await new Promise((resolve) => setTimeout(resolve, 700))
			} catch (e) {
				this.$buefy.toast.open({
					message: errorMessage(e, this.$t('Failed to change display resolution')),
					type: 'is-danger',
					duration: 3500,
				})
			} finally {
				this.resizingHost = false
				this.resizeSettleResolve = null
			}
		},

		waitForResize(timeoutMs) {
			return new Promise((resolve) => {
				this.resizeSettleResolve = resolve
				setTimeout(resolve, timeoutMs)
			})
		},

		// Physical pixel size the stream area wants: CSS size x device pixel
		// ratio (capped at 2 - beyond that the host would render at sizes no
		// one can read), snapped to even numbers and the supported range.
		windowTargetSize() {
			if (!this.$refs.screen) return null
			const dpr = Math.min(2, Math.max(1, window.devicePixelRatio || 1))
			const even = (n) => n - (n % 2)
			const w = Math.min(7680, Math.max(640, even(Math.round(this.$refs.screen.clientWidth * dpr))))
			const h = Math.min(4320, Math.max(480, even(Math.round(this.$refs.screen.clientHeight * dpr))))
			return { w, h }
		},

		updateWindowEstimate() {
			const t = this.windowTargetSize()
			if (!t) return
			this.currentWindowEstimate = `${t.w} × ${t.h}`
		},

		async matchWindowResolution() {
			if (this.resizingHost) return
			// The exact size: the sidecar sets it (NVIDIA viewport, or a new
			// CVT mode) and only falls back to the monitor's nearest real
			// mode when the driver can't; the reply says what it got.
			const t = this.windowTargetSize()
			if (!t) return
			await this.changeResolution(t.w, t.h)
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
			this.applyQualityPreset()
			this.$buefy.toast.open({
				message: `${this.$t('Bandwidth profile')}: ${this.qualityModeLabel}`,
				type: 'is-info',
				duration: 2000,
			})
		},

		toggleFullscreen() {
			if (document.fullscreenElement) {
				document.exitFullscreen()
			} else if (this.$el && this.$el.requestFullscreen) {
				this.$el.requestFullscreen()
			}
		},

		openInNewTab() {
			window.open('/#/host-desktop', '_blank')
		},

		closePanel() {
			this.releaseStickyModifiers()
			this.$emit('close')
		},

		closeMenus() {
			this.keysMenuOpen = false
			this.qualityMenuOpen = false
			this.displayMenuOpen = false
		},

		onDocumentKeydown(event) {
			if (event.key !== 'Escape') return
			if (this.keysMenuOpen || this.qualityMenuOpen || this.displayMenuOpen) {
				this.closeMenus()
				event.stopPropagation()
			}
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

		onVisibilityChange() {
			if (document.hidden) {
				this.stopCapsLockPoll()
			} else if (this.status === 'connected' && !this.minimized) {
				this.startCapsLockPoll()
			}
		},

		onWindowResize() {
			this.updateCompactKeyboard()
			this.clampKeyboardPos()
			this.updateWindowEstimate()
		},

		updateCompactKeyboard() {
			this.compactKeyboard = typeof window !== 'undefined' && window.innerWidth < 900
		},

		// The keyboard is re-parented to <body> (or the fullscreen element)
		// so it can float over the whole page, like VmConsolePanel's.
		placeKeyboard() {
			const kb = this.$refs.keyboard
			if (!kb) return
			const target = document.fullscreenElement ? this.$el : document.body
			if (kb.parentNode !== target) {
				target.appendChild(kb)
			}
			if (!this.keyboardPos) {
				const kbRect = kb.getBoundingClientRect()
				this.keyboardPos = {
					x: Math.max(0, Math.round((window.innerWidth - kbRect.width) / 2)),
					y: Math.max(0, Math.round(window.innerHeight - kbRect.height - 50)),
				}
			}
			this.clampKeyboardPos()
		},

		onFullscreenChange() {
			if (!this.keyboardVisible || !this.$refs.keyboard) return
			this.$nextTick(() => this.placeKeyboard())
		},

		toggleKeyboard() {
			if (this.keyboardOpen) {
				this.closeKeyboard()
			} else {
				this.keyboardOpen = true
			}
		},

		closeKeyboard() {
			// Hidden latched modifiers would keep affecting every key typed on
			// the real keyboard - release them with the keyboard.
			this.releaseStickyModifiers()
			this.keyboardOpen = false
		},

		releaseStickyModifiers() {
			Object.keys(STICKY_MODIFIERS).forEach((prop) => {
				if (!this[prop]) return
				const m = STICKY_MODIFIERS[prop]
				if (this.rfb) {
					try {
						this.rfb.sendKey(m.keysym, m.code, false)
					} catch (e) {}
				}
				this[prop] = false
			})
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

		// Key widths are expressed in units of --osk-key (shrunk on phones
		// by the .is-compact class) so the whole keyboard scales together.
		keyStyle(key) {
			const u = key.u || 1
			const style = { width: `calc(var(--osk-key) * ${u} + var(--osk-gap) * ${u - 1})` }
			if (key.gapBefore) style.marginLeft = `calc(var(--osk-key) * ${(key.gapBefore / 2.3).toFixed(3)})`
			return style
		},

		keyLabel(key) {
			if (key.special) return key.label
			return this.shiftActive ? key.shift || key.base : key.base
		},

		pressKey(key) {
			if (!this.rfb) return
			if (key.sticky) {
				// CapsLock toggles on a full press+release, unlike Shift/Ctrl/Alt
				// which stay latched until pressed again.
				if (key.special === 'CapsLock') {
					const keysym = SPECIAL_KEYSYMS.CapsLock
					this.rfb.sendKey(keysym, key.code, true)
					this.rfb.sendKey(keysym, key.code, false)
					// Optimistic - re-checked against the host's real state.
					this.capsLockActive = !this.capsLockActive
					setTimeout(() => this.pollCapsLock(), 400)
					return
				}
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

		// The indicator reflects the HOST's CapsLock state (vm-sidecar's
		// /host/desktop/capslock, backed by `xset q`), polled only while
		// connected, visible and not minimised.
		startCapsLockPoll() {
			this.stopCapsLockPoll()
			if (this.status !== 'connected' || this.minimized || document.hidden) return
			this.pollCapsLock()
			this.capsLockPollTimer = setInterval(() => this.pollCapsLock(), 1500)
		},

		stopCapsLockPoll() {
			if (this.capsLockPollTimer) {
				clearInterval(this.capsLockPollTimer)
				this.capsLockPollTimer = null
			}
		},

		async pollCapsLock() {
			if (this.status !== 'connected') return
			// Doubles as the toolbar ping reading (RTT to vm-sidecar).
			const t0 = performance.now()
			try {
				const res = await this.sidecarGet('/host/desktop/capslock')
				if (this.status !== 'connected') return
				this.pingMs = Math.round(performance.now() - t0)
				if (res && res.data) {
					this.capsLockActive = !!res.data.caps_lock
				}
			} catch (e) {
				// Transient hiccup - keep the last known values.
			}
		},

		disableCapsLock() {
			if (!this.rfb) return
			const keysym = SPECIAL_KEYSYMS.CapsLock
			this.rfb.sendKey(keysym, 'CapsLock', true)
			this.rfb.sendKey(keysym, 'CapsLock', false)
			this.capsLockActive = false
			setTimeout(() => this.pollCapsLock(), 400)
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

		// Pushed whenever x11vnc sees the host clipboard change. writeText()
		// needs a secure context; when it can't, a small chip offers the
		// manual copy dialog instead of popping one up and stealing focus.
		receiveHostClipboard(text) {
			if (!text) return
			recordClipboard({ text, direction: 'from', target: this.clipboardTarget })
			if (navigator.clipboard && navigator.clipboard.writeText) {
				navigator.clipboard
					.writeText(text)
					.then(() => this.showClipChip(true))
					.catch(() => this.showClipChip(false))
				return
			}
			this.showClipChip(false)
			if (!this.clipboardInsecureWarned && !window.isSecureContext) {
				this.clipboardInsecureWarned = true
				this.$buefy.toast.open({
					message: this.$t('Automatic clipboard sync needs HTTPS - open Host Desktop over https:// to enable it.'),
					type: 'is-warning',
					position: 'is-top',
					duration: 6000,
				})
			}
		},

		showClipChip(copied) {
			this.clipChip = { visible: true, copied }
			if (this.clipChipTimer) clearTimeout(this.clipChipTimer)
			this.clipChipTimer = setTimeout(() => this.hideClipChip(), copied ? 2500 : 8000)
		},

		hideClipChip() {
			if (this.clipChipTimer) {
				clearTimeout(this.clipChipTimer)
				this.clipChipTimer = null
			}
			this.clipChip = { visible: false, copied: false }
		},

		// The chip's "View": the clipboard popover lists the text with a
		// Copy button that also works without the Clipboard API.
		openClipboardFromChip() {
			this.hideClipChip()
			this.clipboardOpen = true
		},

		// Clipboard popover (shared/clipboard/RemoteClipboardPanel.vue).
		clipboardSend(text) {
			if (this.rfb) this.rfb.clipboardPasteFrom(text)
		},

		clipboardType(text) {
			// x11vnc runs with -add_keysyms, so Unicode keysyms type
			// characters the host keymap lacks.
			typeText(this.rfb, text)
		},

		closeClipboard() {
			this.clipboardOpen = false
			if (this.rfb) this.rfb.focus()
		},
	},
}
</script>

<style lang="scss" scoped>
.host-desktop-root {
	position: absolute;
	inset: 0;
	overflow: hidden;
	background: #000;
}

.sr-only {
	position: absolute;
	width: 1px;
	height: 1px;
	padding: 0;
	margin: -1px;
	overflow: hidden;
	clip: rect(0, 0, 0, 0);
	white-space: nowrap;
	border: 0;
}

// Keyboard focus ring for everything interactive in the panel (inputs used
// to set `outline: none` with only a subtle border change).
.host-desktop-root ::v-deep button:focus-visible,
.host-desktop-root ::v-deep select:focus-visible,
.host-desktop-root ::v-deep input:focus-visible,
.host-desktop-root ::v-deep textarea:focus-visible,
.host-desktop-root ::v-deep summary:focus-visible,
.host-desktop-root ::v-deep a:focus-visible {
	outline: 2px solid #60a5fa;
	outline-offset: 2px;
}

.host-desktop-panel {
	position: absolute;
	inset: 0;
	background: #000;
	display: flex;
	flex-direction: column;
	color: #fff;
	overflow: hidden;
}

.host-desktop-not-installed,
.host-desktop-checking {
	align-items: center;
	justify-content: center;
	text-align: center;
}

.not-installed-card {
	display: flex;
	flex-direction: column;
	align-items: center;
	gap: var(--space-2, 0.5rem);
	max-width: 360px;
	padding: var(--space-4, 1.5rem);

	h3 {
		margin: 0;
		font-size: 1.1rem;
	}

	p {
		margin: 0;
		color: rgba(255, 255, 255, 0.7);
		font-size: 0.9rem;
	}
}

.host-desktop-not-installed {
	overflow-y: auto;
	padding: var(--space-3, 1rem) 0;
}

.not-installed-card.de-card {
	max-width: 30rem;
	width: 100%;
	margin: auto;
}

.not-installed-hint {
	font-size: 0.8rem !important;
}

.not-installed-error {
	color: #f14668 !important;
}

.install-log {
	width: 100%;
	text-align: left;
	font-size: 0.8rem;
	color: rgba(255, 255, 255, 0.7);

	pre {
		max-height: 12rem;
		overflow: auto;
		white-space: pre-wrap;
		word-break: break-word;
		background: rgba(255, 255, 255, 0.06);
		color: rgba(255, 255, 255, 0.85);
		padding: 0.5rem;
		border-radius: 6px;
		font-size: 0.75rem;
	}
}

.de-command-box {
	width: 100%;
	text-align: left;
	margin-top: var(--space-2, 0.5rem);

	p {
		margin: 0 0 0.35rem;
		font-size: 0.8rem;
	}
}

.de-command-row {
	display: flex;
	gap: 0.4rem;
	align-items: stretch;
}

.de-command {
	flex: 1 1 auto;
	min-width: 0;
	display: block;
	padding: 0.5rem 0.6rem;
	border-radius: 6px;
	background: rgba(255, 255, 255, 0.08);
	color: #e5e7eb;
	font-size: 0.75rem;
	white-space: pre-wrap;
	word-break: break-all;
	user-select: all;
}

.de-copy-btn {
	width: auto !important;
	flex: 0 0 auto;
	display: inline-flex;
	align-items: center;
}

.de-dashboard-link {
	display: inline-block;
	margin-top: 0.4rem;
	font-size: 0.8rem;
	color: #60a5fa;
}

.de-footer-actions {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2, 0.5rem);
	justify-content: center;
	margin-top: var(--space-2, 0.5rem);
}

.de-try-anyway {
	color: rgba(255, 255, 255, 0.75) !important;
}

.de-choice-list {
	display: flex;
	flex-direction: column;
	gap: var(--space-2, 0.5rem);
	width: 100%;
}

.de-choice-btn {
	width: 100%;
	padding: 0.6rem 0.9rem;
	border-radius: 6px;
	border: 1px solid rgba(255, 255, 255, 0.15);
	background: rgba(255, 255, 255, 0.06);
	color: #fff;
	font-size: 0.85rem;
	text-align: left;
	cursor: pointer;
	transition: background 0.15s ease;
}

.de-choice-btn:hover {
	background: rgba(255, 255, 255, 0.12);
}

.de-choice-destructive {
	border-color: rgba(241, 70, 104, 0.4);
	color: #f14668;
}

.de-replace-confirm {
	width: 100%;
	margin-top: var(--space-2, 0.5rem);
	padding-top: var(--space-2, 0.5rem);
	border-top: 1px solid rgba(255, 255, 255, 0.1);

	p {
		margin: 0 0 var(--space-2, 0.5rem);
		font-size: 0.85rem;
		color: rgba(255, 255, 255, 0.7);
	}
}

.de-replace-row {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2, 0.5rem);
	align-items: center;
}

.de-replace-select,
.de-replace-input {
	flex: 1;
	padding: 0.4rem 0.6rem;
	border-radius: 6px;
	border: 1px solid rgba(255, 255, 255, 0.15);
	background: rgba(255, 255, 255, 0.06);
	color: #fff;
	font-size: 0.85rem;
}

.console-toolbar {
	position: relative;
	z-index: 20;
	flex-shrink: 0;
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: var(--space-2);
	padding: var(--space-2) var(--space-3);
	background: #141416;
	border-bottom: 1px solid rgba(255, 255, 255, 0.08);
	user-select: none;
	overflow: visible;
}

.vm-identity {
	display: flex;
	align-items: center;
	gap: var(--space-2);
}

.vm-name {
	font-weight: 600;
	font-size: var(--font-base);
	letter-spacing: -0.01em;
}

.status-pill {
	font-size: var(--font-2xs);
	padding: var(--space-1) var(--space-2);
	border-radius: var(--radius-pill);
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

.ping-pill {
	display: inline-flex;
	align-items: center;
	gap: var(--space-1);
	font-size: var(--font-2xs);
	padding: var(--space-1) var(--space-2);
	border-radius: var(--radius-pill);
	background: rgba(255, 255, 255, 0.1);
	color: rgba(255, 255, 255, 0.7);

	&.is-good {
		background: rgba(72, 199, 116, 0.2);
		color: #48c774;
	}
	&.is-ok {
		background: rgba(255, 221, 87, 0.15);
		color: #ffdd57;
	}
	&.is-bad {
		background: rgba(255, 56, 96, 0.15);
		color: #ff3860;
	}

	::v-deep .icon {
		margin: 0;
	}
}

.toolbar-actions {
	display: flex;
	align-items: center;
	gap: var(--space-1);
	flex-wrap: wrap;
	justify-content: flex-end;
	min-width: 0;
	overflow: visible;
}

.toolbar-group {
	display: inline-flex;
	align-items: center;
	gap: var(--space-1);
	background: rgba(255, 255, 255, 0.05);
	padding: var(--space-1);
	border-radius: var(--radius-control);
	border: 1px solid rgba(255, 255, 255, 0.06);
}

.toolbar-divider {
	width: 1px;
	height: 1.25rem;
	background: rgba(255, 255, 255, 0.1);
	margin: 0 var(--space-1);
	flex-shrink: 0;
}

.toolbar-btn {
	display: inline-flex;
	align-items: center;
	gap: var(--space-1);
	border: none;
	background: transparent;
	color: rgba(255, 255, 255, 0.85);
	font-family: inherit;
	font-size: var(--font-xs);
	font-weight: 500;
	padding: var(--space-1) var(--space-2);
	border-radius: var(--radius-sm);
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
		padding: var(--space-1) var(--space-2);
	}

	&.close-btn:hover {
		background: rgba(239, 68, 68, 0.25);
		color: #ef4444;
	}
}

@media (max-width: 680px) {
	.console-toolbar {
		flex-wrap: wrap;
		padding: var(--space-1) var(--space-2);
	}
	.toolbar-actions {
		justify-content: flex-start;
	}
	.toolbar-btn span {
		display: none;
	}
	.toolbar-btn {
		padding: var(--space-1) var(--space-2);
	}
	.toolbar-actions {
		gap: var(--space-1);
	}
	.toolbar-group {
		gap: var(--space-1);
		padding: var(--space-1);
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
	border-radius: var(--radius-card);
	box-shadow: var(--shadow-lg);
	padding: var(--space-1);
	min-width: 11rem;
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
}

.keys-menu {
	left: 0 !important;
	right: auto !important;
	min-width: 12rem;
}

.power-menu-item {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	border: none;
	background: none;
	color: #fff;
	font-family: inherit;
	font-size: var(--font-sm);
	padding: var(--space-2) var(--space-2);
	border-radius: var(--radius-sm);
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
	gap: var(--space-2);
	padding: var(--space-2);
}

.quality-menu-text {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	min-width: 0;
}

.quality-menu-title {
	font-size: var(--font-sm);
	font-weight: 600;
}

.quality-menu-desc {
	font-size: var(--font-2xs);
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
	border-radius: var(--radius-card);
	box-shadow: var(--shadow-md);
	padding: var(--space-3);
	width: 21rem;
	max-width: calc(100vw - 2rem);
	overflow: visible !important;
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
}

.display-dropdown-menu {
	left: auto !important;
	right: 0 !important;
}

.device-menu-header-row {
	display: flex;
	align-items: center;
	justify-content: space-between;
	margin-bottom: var(--space-1);
}

.header-title-group {
	display: flex;
	align-items: center;
	gap: var(--space-2);

	.device-menu-title {
		margin: 0;
	}
}

.active-resolution-badge {
	font-size: var(--font-2xs);
	font-family: monospace;
	font-weight: 600;
	color: #60a5fa;
	background: rgba(37, 99, 235, 0.18);
	padding: var(--space-1) var(--space-2);
	border-radius: var(--radius-xs);
}

.device-menu-title {
	font-size: var(--font-xs);
	font-weight: 700;
	text-transform: uppercase;
	letter-spacing: 0.04em;
	color: rgba(255, 255, 255, 0.5);
	margin: 0 0 var(--space-1);
}

.device-menu-title-divided {
	margin-top: var(--space-2);
	padding-top: var(--space-2);
	border-top: 1px solid rgba(255, 255, 255, 0.08);
}

.device-menu-hint {
	font-size: var(--font-xs);
	color: rgba(255, 255, 255, 0.45);
	margin: var(--space-1) 0 var(--space-1);
}

.device-menu-scrollable {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	max-height: 12rem;
	overflow-y: auto;
	scrollbar-width: thin;
	scrollbar-color: rgba(255, 255, 255, 0.2) transparent;

	&::-webkit-scrollbar {
		width: 5px;
	}
	&::-webkit-scrollbar-thumb {
		background: rgba(255, 255, 255, 0.2);
		border-radius: var(--radius-xs);
	}
}

.device-menu-row {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	width: 100%;
	border: none;
	background: none;
	font-family: inherit;
	text-align: left;
	padding: var(--space-2) var(--space-2);
	border-radius: var(--radius-control);
	cursor: pointer;
	color: #fff;
	font-size: var(--font-xs);
	transition: background 0.12s ease;

	&:hover:not(.disabled):not(:disabled) {
		background: rgba(255, 255, 255, 0.06);
	}
	&.active {
		background: rgba(37, 99, 235, 0.15);
	}
	&.disabled,
	&:disabled {
		opacity: 0.5;
		cursor: default;
	}
}

.device-menu-error {
	color: #f87171;
}

.stream-settings {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	padding: 0 var(--space-2) var(--space-2);
}

.stream-setting-row {
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: var(--space-2);
	font-size: var(--font-xs);
	color: rgba(255, 255, 255, 0.85);
	cursor: pointer;
}

.stream-setting-select {
	background: rgba(0, 0, 0, 0.35);
	border: 1px solid rgba(255, 255, 255, 0.15);
	border-radius: var(--radius-sm);
	color: #fff;
	font-size: var(--font-xs);
	padding: 0.15rem 0.3rem;
}

.stream-settings-save {
	align-self: flex-end;
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
	gap: var(--space-1);
}

.network-row-label {
	font-size: var(--font-xs);
	font-weight: 600;
	color: #fff;
}

.network-row-meta {
	font-size: var(--font-2xs);
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
	gap: var(--space-2);
	margin-top: var(--space-1);
}

.custom-res-field {
	flex: 1;
	width: 4rem;
	background: rgba(0, 0, 0, 0.35);
	border: 1px solid rgba(255, 255, 255, 0.15);
	border-radius: var(--radius-sm);
	padding: var(--space-1) var(--space-2);
	color: #fff;
	font-size: var(--font-xs);
	font-family: monospace;

	&:focus {
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
	font-size: var(--font-xs);
	font-weight: 600;
	padding: var(--space-1) var(--space-3);
	border-radius: var(--radius-sm);
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
	gap: var(--space-3);
	color: rgba(255, 255, 255, 0.7);

	::v-deep .icon {
		width: 2.25rem;
		height: 2.25rem;
	}
}

.console-resizing-overlay {
	position: absolute;
	inset: 0;
	background: #000;
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	gap: var(--space-3);
	color: rgba(255, 255, 255, 0.85);
	z-index: 5;

	::v-deep .icon {
		width: 2.25rem;
		height: 2.25rem;
	}
}

.console-status-reason {
	max-width: min(32rem, 90vw);
	text-align: center;
	font-size: var(--font-sm);
	color: rgba(255, 255, 255, 0.8);
}

.console-status-actions {
	display: flex;
	flex-wrap: wrap;
	justify-content: center;
	gap: var(--space-2);
}

.host-clip-chip {
	position: absolute;
	right: var(--space-3);
	bottom: 2.5rem;
	z-index: 15;
	display: inline-flex;
	align-items: center;
	gap: var(--space-2);
	max-width: calc(100% - 2rem);
	padding: var(--space-1) var(--space-2) var(--space-1) var(--space-3);
	border-radius: var(--radius-pill);
	background: rgba(30, 30, 36, 0.92);
	border: 1px solid rgba(255, 255, 255, 0.14);
	color: rgba(255, 255, 255, 0.9);
	font-size: var(--font-xs);
	box-shadow: var(--shadow-md);
	pointer-events: auto;
}

.host-clip-chip-btn {
	display: inline-flex;
	align-items: center;
	border: none;
	background: rgba(255, 255, 255, 0.12);
	color: #fff;
	font-family: inherit;
	font-size: var(--font-2xs);
	font-weight: 600;
	padding: 0.15rem 0.5rem;
	border-radius: var(--radius-pill);
	cursor: pointer;

	&:hover {
		background: rgba(255, 255, 255, 0.22);
	}
}

.reconnect-btn {
	display: inline-flex;
	align-items: center;
	gap: var(--space-1);
	border: none;
	background: #3273dc;
	color: #fff;
	font-family: inherit;
	font-size: var(--font-sm);
	font-weight: 600;
	padding: var(--space-2) var(--space-4);
	border-radius: var(--radius-sm);
	cursor: pointer;

	&:hover:not(:disabled) {
		background: #2366d1;
	}
	&.is-secondary {
		background: rgba(255, 255, 255, 0.14);
		&:hover:not(:disabled) {
			background: rgba(255, 255, 255, 0.24);
		}
	}
	&:disabled {
		opacity: 0.5;
		cursor: default;
	}
}

/* Floating On-Screen Keyboard */
.on-screen-keyboard {
	--osk-key: 2.3rem;
	--osk-gap: 0.25rem;
	position: fixed !important;
	left: 50%;
	bottom: 2rem;
	transform: translateX(-50%);
	z-index: 100000 !important;
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	width: fit-content;
	max-width: calc(100vw - 2rem);
	overflow-x: auto;
	padding: var(--space-2) var(--space-3) var(--space-3);
	background: rgba(38, 38, 38, 0.95);
	backdrop-filter: blur(16px);
	-webkit-backdrop-filter: blur(16px);
	border: 1px solid rgba(255, 255, 255, 0.16);
	border-radius: var(--radius-modal);
	box-shadow: 0 20px 50px rgba(0, 0, 0, 0.65), 0 0 0 1px rgba(255, 255, 255, 0.08);
	user-select: none;
	color: #fff;

	// Phones / narrow windows: shrink every key together (widths are in
	// --osk-key units) and drop the side columns so the keyboard fits.
	&.is-compact {
		--osk-key: min(2.3rem, calc((100vw - 3rem) / 15.5));
		--osk-gap: 0.15rem;
		max-width: calc(100vw - 0.5rem);
		padding: var(--space-1) var(--space-2) var(--space-2);

		.osk-side,
		.osk-shortcuts-col {
			display: none;
		}
		.osk-key {
			padding: var(--space-1) 0;
			font-size: var(--font-2xs);
		}
	}
}

.osk-header {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	padding-bottom: var(--space-1);
	margin-bottom: var(--space-1);
	border-bottom: 1px solid rgba(255, 255, 255, 0.1);
	color: rgba(255, 255, 255, 0.6);
	cursor: move;
}

.osk-title {
	font-size: var(--font-xs);
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
	padding: var(--space-1);
	border-radius: var(--radius-xs);

	&:hover {
		background: rgba(255, 255, 255, 0.1);
		color: #fff;
	}
}

.osk-keys {
	display: flex;
	gap: var(--space-2);
}

.osk-alpha {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
}

.osk-side {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
}

.osk-fn-spacer {
	height: var(--osk-key);
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
	gap: var(--space-1);
}

.osk-shortcut-btn {
	display: flex;
	align-items: center;
	justify-content: center;
}

.osk-row {
	display: flex;
	gap: var(--osk-gap);
}

.osk-key {
	flex: 0 0 auto;
	border: none;
	border-bottom: 2px solid rgba(0, 0, 0, 0.35);
	background: #454545;
	color: #fff;
	font-family: inherit;
	font-size: var(--font-xs);
	padding: var(--space-2) var(--space-1);
	border-radius: var(--radius-sm);
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
	gap: var(--space-4);
	padding: var(--space-1) var(--space-3);
	background: #1a1a1a;
	border-top: 1px solid rgba(255, 255, 255, 0.08);
	font-size: var(--font-xs);
	color: rgba(255, 255, 255, 0.55);
	flex-wrap: wrap;

	::v-deep .icon {
		width: 1rem;
		height: 1rem;
		margin-right: var(--space-1);
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
	margin-right: var(--space-2);
	flex-shrink: 0;
}

.statusbar-item.is-live .activity-dot {
	background: #48c774;
	box-shadow: 0 0 4px #48c774;
}

.statusbar-capslock {
	border: none;
	background: rgba(255, 221, 87, 0.18);
	color: #ffdd57;
	font-weight: 600;
	padding: var(--space-1) var(--space-2);
	border-radius: var(--radius-pill);
	cursor: pointer;
	font-family: inherit;
	font-size: inherit;
	transition: background 0.14s ease;

	::v-deep .icon {
		margin-right: var(--space-1);
	}

	&:hover {
		background: rgba(255, 221, 87, 0.3);
	}
}
</style>
