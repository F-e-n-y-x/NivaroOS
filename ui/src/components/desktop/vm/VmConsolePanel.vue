<!-- src/components/desktop/vm/VmConsolePanel.vue -->
<!--
	The one console UI, shared by every place a VM's display can be
	opened: a proper desktop window (VmList's "Console" action, via
	DesktopWindow's COMPONENT_REGISTRY), and a standalone browser tab
	(VmConsoleStandalone.vue, "Open in New Tab"). Building this once
	instead of three slightly-different copies is deliberate: every
	previous copy used Buefy's <b-dropdown> for its power menu, which
	across several rounds of testing in this app's "desktop window"
	context turned out unreliable (three-dot menu, standalone-tab power
	menu) for reasons never fully pinned down - replaced here with a
	plain boolean-toggled menu (the same pattern ContextMenu.vue already
	uses reliably elsewhere in Files), closed on outside click.
-->
<template>
	<div class="vm-console-panel">
		<div class="console-toolbar">
			<!-- Standalone window header / VM identity -->
			<div v-if="showClose" class="vm-identity">
				<b-icon icon="monitor" custom-size="mdi-18px"></b-icon>
				<span class="vm-name">{{ vmName }}</span>
				<span class="status-pill" :class="'is-' + status">{{ statusText }}</span>
			</div>

			<div class="toolbar-actions">
				<!-- Segment 1: Input Controls -->
				<div class="toolbar-group">
					<button type="button" class="toolbar-btn icon-only-btn" :class="{ active: keyboardOpen }" :title="$t('On-Screen Keyboard')" @click="keyboardOpen = !keyboardOpen">
						<b-icon icon="keyboard-outline" custom-size="mdi-16px"></b-icon>
					</button>

					<div ref="keysMenuWrapper" class="menu-wrapper">
						<button type="button" class="toolbar-btn" :title="$t('Send Key Shortcuts')" @click="keysMenuOpen = !keysMenuOpen">
							<b-icon icon="keyboard-settings-outline" custom-size="mdi-16px"></b-icon>
							<span>{{ $t('Keys') }}</span>
							<b-icon icon="chevron-down" custom-size="mdi-14px"></b-icon>
						</button>
						<div v-if="keysMenuOpen" class="power-menu keys-menu">
							<button type="button" class="power-menu-item" @click="sendCtrlAltDel(); keysMenuOpen = false">
								<b-icon icon="apple-keyboard-control" custom-size="mdi-16px"></b-icon><span>Ctrl+Alt+Del</span>
							</button>
							<button type="button" class="power-menu-item" @click="sendWinKey(); keysMenuOpen = false">
								<b-icon icon="microsoft-windows" custom-size="mdi-16px"></b-icon><span>{{ $t('Win Key') }}</span>
							</button>
							<button type="button" class="power-menu-item" @click="sendAltTab(); keysMenuOpen = false">
								<b-icon icon="tab" custom-size="mdi-16px"></b-icon><span>Alt + Tab</span>
							</button>
							<button type="button" class="power-menu-item" @click="sendCtrlShiftEsc(); keysMenuOpen = false">
								<b-icon icon="chart-line" custom-size="mdi-16px"></b-icon><span>Ctrl+Shift+Esc</span>
							</button>
							<button type="button" class="power-menu-item" @click="sendAltF4(); keysMenuOpen = false">
								<b-icon icon="close-box-outline" custom-size="mdi-16px"></b-icon><span>Alt + F4</span>
							</button>
						</div>
					</div>

					<button type="button" class="toolbar-btn icon-only-btn" :title="$t('Paste clipboard text into VM')" @click="pasteClipboard">
						<b-icon icon="content-paste" custom-size="mdi-16px"></b-icon>
					</button>
				</div>

				<div class="toolbar-divider"></div>

				<!-- Segment 2: View & Stream Controls -->
				<div class="toolbar-group">
					<button type="button" class="toolbar-btn" :title="scaleToFit ? $t('Show actual size') : $t('Scale to fit window')" @click="toggleScale">
						<b-icon :icon="scaleToFit ? 'fit-to-page-outline' : 'aspect-ratio'" custom-size="mdi-16px"></b-icon>
						<span>{{ scaleToFit ? $t('Fit') : $t('1:1') }}</span>
					</button>

					<div ref="qualityMenuWrapper" class="menu-wrapper">
						<button type="button" class="toolbar-btn" :title="$t('Display & Bandwidth Quality')" @click="qualityMenuOpen = !qualityMenuOpen">
							<b-icon icon="speedometer" custom-size="mdi-16px"></b-icon>
							<span>{{ qualityModeLabel }}</span>
							<b-icon icon="chevron-down" custom-size="mdi-14px"></b-icon>
						</button>
						<div v-if="qualityMenuOpen" class="power-menu quality-menu">
							<button type="button" v-for="q in qualityOptions" :key="q.mode" class="power-menu-item quality-menu-item" :class="{ active: qualityMode === q.mode }" @click="setQualityMode(q.mode)">
								<b-icon :icon="q.icon" custom-size="mdi-18px"></b-icon>
								<span class="quality-menu-text">
									<span class="quality-menu-title">{{ $t(q.label) }}</span>
									<span class="quality-menu-desc">{{ $t(q.desc) }}</span>
								</span>
								<b-icon v-if="qualityMode === q.mode" icon="check" custom-size="mdi-16px" class="quality-menu-check"></b-icon>
							</button>
						</div>
					</div>

					<button type="button" class="toolbar-btn icon-only-btn" :title="$t('Fullscreen')" @click="toggleFullscreen">
						<b-icon icon="fullscreen" custom-size="mdi-16px"></b-icon>
					</button>
				</div>

				<div class="toolbar-divider"></div>

				<!-- Segment 3: Storage, Share, Hardware & Network -->
				<div class="toolbar-group">
					<!-- Disks & ISO -->
					<div ref="diskMenuWrapper" class="menu-wrapper">
						<button type="button" class="toolbar-btn" :class="{ active: diskMenuOpen }" :title="$t('Storage & CD-ROM')" @click="diskMenuOpen = !diskMenuOpen">
							<b-icon icon="harddisk" custom-size="mdi-16px"></b-icon>
							<span>{{ $t('Disks') }}</span>
							<b-icon icon="chevron-down" custom-size="mdi-14px"></b-icon>
						</button>
						<div v-if="diskMenuOpen" class="device-menu">
							<p class="device-menu-title">{{ $t('Boot ISO') }}</p>
							<div class="device-menu-row disk-row">
								<div class="device-row-icon" :class="{ active: !!isoFileName }">
									<b-icon icon="disc" size="is-small"></b-icon>
								</div>
								<span class="device-menu-desc">{{ isoFileName || $t('No ISO loaded') }}</span>
								<button type="button" v-if="isoFileName" class="device-menu-detach" :disabled="diskBusy" :title="$t('Eject')" @click="ejectBootISO">
									<b-icon icon="eject-outline" size="is-small"></b-icon>
								</button>
							</div>
							<div class="device-menu-add">
								<vm-dropdown
									v-model="selectedISO"
									:options="isoDropdownOptions"
									:placeholder="availableISOs.length ? $t('Select an ISO...') : $t('No ISOs available')"
									:disabled="diskBusy || !availableISOs.length"
									dark
									icon="disc"
									size="small"
									style="flex: 1 1 auto; min-width: 0;"
								></vm-dropdown>
								<button type="button" class="device-menu-attach-btn" :disabled="diskBusy || !selectedISO" @click="insertBootISO">
									<b-icon icon="tray-arrow-down" size="is-small"></b-icon>
									<span>{{ $t('Insert') }}</span>
								</button>
							</div>
							<p class="device-menu-title device-menu-title-divided">{{ $t('Attached Disks') }}</p>
							<p v-if="!(vm && vm.disks && vm.disks.length)" class="device-menu-hint">{{ $t('No virtual disks yet') }}</p>
							<div class="device-menu-scrollable">
								<div v-for="disk in (vm && vm.disks) || []" :key="disk.target" class="device-menu-row disk-row">
									<div class="device-row-icon active">
										<b-icon :icon="disk.ssd ? 'harddisk' : 'database'" size="is-small"></b-icon>
									</div>
									<span class="device-menu-desc">{{ disk.target }} &middot; {{ disk.gib }} GiB &middot; {{ disk.bus.toUpperCase() }}</span>
									<button type="button" class="device-menu-detach" :disabled="diskBusy" :title="$t('Detach')" @click="detachDiskConfirm(disk)">
										<b-icon icon="eject-outline" size="is-small"></b-icon>
									</button>
								</div>
							</div>
						</div>
					</div>

					<!-- Live Shared Folders (VirtIO-FS) -->
					<div ref="shareMenuWrapper" class="menu-wrapper">
						<button type="button" class="toolbar-btn" :class="{ active: (vm && vm.shared_folders && vm.shared_folders.length) }" :title="$t('Host Shared Folders (Live Mount)')" @click="toggleShareMenu">
							<b-icon icon="folder-sync-outline" custom-size="mdi-16px"></b-icon>
							<span>{{ $t('Share') }}</span>
							<span v-if="vm && vm.shared_folders && vm.shared_folders.length" class="share-badge-dot"></span>
							<b-icon icon="chevron-down" custom-size="mdi-14px"></b-icon>
						</button>
						<div v-if="shareMenuOpen" class="device-menu share-menu">
							<div class="device-menu-header-row">
								<div class="header-title-group">
									<b-icon icon="folder-sync-outline" size="is-small"></b-icon>
									<p class="device-menu-title">{{ $t('Live Shared Folders') }}</p>
								</div>
								<span v-if="vm && vm.shared_folders && vm.shared_folders.length" class="share-connected-badge">
									<span class="share-status-dot"></span> {{ vm.shared_folders.length }} {{ $t('Active') }}
								</span>
							</div>
							<p class="device-menu-hint">{{ $t('Direct host directory pass-through via VirtIO-FS with full disk space and zero-network instant access.') }}</p>

							<!-- Active Shares List -->
							<div v-if="vm && vm.shared_folders && vm.shared_folders.length" class="share-list-section">
								<div v-for="sf in vm.shared_folders" :key="sf.target_tag" class="share-active-box">
									<div class="share-active-icon">
										<b-icon icon="folder-check-outline" size="is-small"></b-icon>
									</div>
									<div class="share-active-details">
										<span class="share-active-path" :title="sf.source_dir">{{ sf.source_dir }}</span>
										<span class="share-active-meta">
											<span class="share-status-dot"></span>
											<span class="tag-pill">{{ sf.target_tag }}</span>
											<template v-if="sf.read_only">&middot; {{ $t('Read-Only') }}</template>
											<template v-else>&middot; {{ $t('Read / Write') }}</template>
										</span>
									</div>
									<button class="share-remove-btn" :disabled="shareBusy" :title="$t('Unmount shared folder')" @click="detachShare(sf.target_tag)">
										<b-icon icon="close" size="is-small"></b-icon>
									</button>
								</div>
							</div>

							<!-- Add Share Section -->
							<div class="share-add-box">
								<label class="share-label">{{ $t('Add Host Folder') }}</label>
								<div class="share-folder-picker-row">
									<span class="share-folder-path" :class="{ 'is-empty': !selectedShareFolder }">
										{{ selectedShareFolder || $t('Select host directory...') }}
									</span>
									<button type="button" class="share-browse-btn" @click="showShareFolderPicker = true">
										<b-icon icon="folder-open" size="is-small"></b-icon>
										<span>{{ $t('Browse') }}</span>
									</button>
								</div>
								<div v-if="selectedShareFolder" class="share-tag-row">
									<input v-model="shareTag" class="share-tag-input" :placeholder="$t('Mount tag (e.g. nivaroshare)')" />
									<button class="share-action-btn is-primary" :disabled="shareBusy" @click="attachShare">
										<b-icon v-if="shareBusy" icon="loading" custom-class="mdi-spin" size="is-small"></b-icon>
										<b-icon v-else icon="plus" size="is-small"></b-icon>
										<span>{{ $t('Mount Live') }}</span>
									</button>
								</div>
							</div>

							<!-- Guest Instructions -->
							<div class="share-instructions-card">
								<div class="instructions-header">
									<span class="instructions-title">{{ $t('How to access in VM:') }}</span>
									<div class="os-tab-buttons">
										<button type="button" class="os-tab-btn" :class="{ active: instructionsTab === 'linux' }" @click="instructionsTab = 'linux'">Linux</button>
										<button type="button" class="os-tab-btn" :class="{ active: instructionsTab === 'windows' }" @click="instructionsTab = 'windows'">Windows</button>
									</div>
								</div>
								<div v-if="instructionsTab === 'linux'" class="instructions-body">
									<div class="code-snippet-box">
										<code>sudo mount -t virtiofs {{ (vm && vm.shared_folders && vm.shared_folders[0] && vm.shared_folders[0].target_tag) || 'nivaroshare' }} /mnt</code>
										<button type="button" class="code-copy-btn" :title="$t('Copy Command')" @click="copyCommand(`sudo mount -t virtiofs ${(vm && vm.shared_folders && vm.shared_folders[0] && vm.shared_folders[0].target_tag) || 'nivaroshare'} /mnt`)">
											<b-icon icon="content-copy" size="is-small"></b-icon>
										</button>
									</div>
								</div>
								<div v-else class="instructions-body">
									<p class="win-instruct-text">{{ $t('Insert Guest Tools CD and run NivaroOS-Guest-Tools-Setup.bat inside Windows to auto-install all drivers & mount shared folders.') }}</p>
									<button type="button" class="insert-virtio-btn" :disabled="virtioWinBusy" @click="insertVirtioWinCD">
										<b-icon v-if="virtioWinBusy" icon="loading" custom-class="mdi-spin" size="is-small"></b-icon>
										<b-icon v-else icon="disc" size="is-small"></b-icon>
										<span>{{ $t('Insert NivaroOS Guest Tools') }}</span>
									</button>
								</div>
							</div>
						</div>
					</div>

					<!-- Host USB Devices -->
					<div ref="usbMenuWrapper" class="menu-wrapper">
						<button type="button" class="toolbar-btn" :class="{ active: usbMenuOpen }" :title="$t('USB Device Passthrough')" @click="usbMenuOpen = !usbMenuOpen">
							<b-icon icon="usb" custom-size="mdi-16px"></b-icon>
							<span>{{ $t('USB') }}</span>
							<b-icon icon="chevron-down" custom-size="mdi-14px"></b-icon>
						</button>
						<div v-if="usbMenuOpen" class="device-menu">
							<p class="device-menu-title">{{ $t('USB Devices') }}</p>
							<p v-if="loadingHostCaps" class="device-menu-hint">{{ $t('Loading host devices...') }}</p>
							<p v-else-if="!hostUsbDevices.length" class="device-menu-hint">{{ $t('No USB devices found on the host.') }}</p>
							<div class="device-menu-scrollable">
								<label v-for="dev in hostUsbDevices" :key="dev.vendor_id + ':' + dev.product_id" class="device-menu-row" :class="{ active: isUsbAttached(dev) }">
									<div class="device-row-icon" :class="{ active: isUsbAttached(dev) }">
										<b-icon icon="usb" size="is-small"></b-icon>
									</div>
									<span class="device-menu-desc">{{ dev.description || (dev.vendor_id + ':' + dev.product_id) }}</span>
									<input type="checkbox" class="device-menu-checkbox" :checked="isUsbAttached(dev)" :disabled="usbBusy" @change="toggleUsbDevice(dev, $event.target.checked)" />
								</label>
							</div>
						</div>
					</div>

					<!-- Network -->
					<div ref="netMenuWrapper" class="menu-wrapper">
						<button type="button" class="toolbar-btn" :class="{ active: netMenuOpen }" :title="$t('Network Adapters')" @click="netMenuOpen = !netMenuOpen">
							<b-icon :icon="networkIcon" :custom-size="networkIconSize"></b-icon>
							<span>{{ $t('Network') }}</span>
							<b-icon icon="chevron-down" custom-size="mdi-14px"></b-icon>
						</button>
						<div v-if="netMenuOpen" class="device-menu network-dropdown-menu">
							<div class="device-menu-header-row">
								<div class="header-title-group">
									<button v-if="editingNet" type="button" class="net-back-btn" :title="$t('Back')" @click="editingNet = false">
										<b-icon icon="arrow-left" size="is-small"></b-icon>
									</button>
									<p class="device-menu-title">{{ editingNet ? $t('Edit Adapter') : $t('Network Adapters') }}</p>
								</div>
								<button v-if="!editingNet && vm && vm.networks && vm.networks.length" type="button" class="net-edit-toggle-btn" @click="startEditingNet(vm.networks[0])">
									<b-icon icon="pencil-outline" size="is-small"></b-icon>
									<span>{{ $t('Edit') }}</span>
								</button>
							</div>
							<p v-if="!(vm && vm.networks && vm.networks.length)" class="device-menu-hint">{{ $t('No network adapters attached.') }}</p>
							<template v-if="!editingNet">
								<div v-for="(n, idx) in (vm && vm.networks) || []" :key="idx" class="device-menu-row net-menu-row">
									<div class="device-row-icon" :class="{ active: n.link_state !== 'down' }">
										<b-icon :icon="n.mode === 'bridge' ? 'lan-connect' : 'lan'" size="is-small" :custom-size="n.mode === 'bridge' ? 'mdi-14px' : 'mdi-16px'"></b-icon>
									</div>
									<div class="network-row-details">
										<span class="network-row-label">{{ n.mode === 'bridge' ? n.bridge_name : $t('NAT Network') }}</span>
										<span class="network-row-meta">{{ (n.model || 'virtio').toUpperCase() }} <template v-if="n.mac">&middot; {{ n.mac }}</template></span>
									</div>
									<button
										type="button"
										class="network-link-toggle"
										:class="{ 'is-connected': n.link_state !== 'down' }"
										:title="n.link_state === 'down' ? $t('Connect virtual ethernet cable') : $t('Disconnect virtual ethernet cable')"
										:disabled="netBusy"
										@click="toggleNetworkLink(n)"
									>
										<b-icon :icon="n.link_state === 'down' ? 'lan-disconnect' : 'lan-check'" size="is-small"></b-icon>
										<span>{{ n.link_state === 'down' ? $t('Connect') : $t('Disconnect') }}</span>
									</button>
								</div>
							</template>
							<template v-else>
								<div class="net-inline-editor">
									<div class="net-editor-section">
										<label class="net-editor-label">{{ $t('Network Mode') }}</label>
										<vm-dropdown
											v-model="netForm.mode"
											:options="networkModeOptions"
											dark
											size="small"
										></vm-dropdown>
									</div>
									<div v-if="netForm.mode === 'bridge'" class="net-editor-section">
										<label class="net-editor-label">{{ $t('Bridge Interface') }}</label>
										<vm-dropdown
											v-model="netForm.bridge_name"
											:options="bridgeDropdownOptions"
											:placeholder="$t('Select bridge interface...')"
											dark
											icon="lan-connect"
											size="small"
										></vm-dropdown>
									</div>
									<div class="net-editor-section">
										<label class="net-editor-label">{{ $t('Adapter Emulation Model') }}</label>
										<vm-dropdown
											v-model="netForm.model"
											:options="adapterModelOptions"
											dark
											size="small"
										></vm-dropdown>
									</div>
									<div class="net-editor-actions">
										<button type="button" class="net-cancel-btn" @click="editingNet = false">{{ $t('Cancel') }}</button>
										<button type="button" class="net-save-btn" :disabled="netBusy" @click="saveNetworkAdapter">
											<b-icon v-if="netBusy" icon="loading" custom-class="mdi-spin" size="is-small"></b-icon>
											<b-icon v-else icon="check" size="is-small"></b-icon>
											<span>{{ $t('Apply Changes') }}</span>
										</button>
									</div>
								</div>
							</template>
						</div>
					</div>
				</div>

				<div class="toolbar-divider"></div>

				<!-- Segment 4: Power Controls -->
				<div class="toolbar-group">
					<div ref="powerMenuWrapper" class="menu-wrapper">
						<button type="button" class="toolbar-btn power-btn" :class="{ 'is-running': vmState === 'running' }" @click="powerMenuOpen = !powerMenuOpen">
							<b-icon icon="power" custom-size="mdi-16px"></b-icon>
							<span>{{ $t('Power') }}</span>
							<b-icon icon="chevron-down" custom-size="mdi-14px"></b-icon>
						</button>
						<div v-if="powerMenuOpen" class="power-menu power-menu-right">
							<button type="button" v-if="vmState !== 'running'" class="power-menu-item is-start" @click="runAction('startVM')">
								<b-icon icon="play" custom-size="mdi-16px"></b-icon><span>{{ $t('Start') }}</span>
							</button>
							<template v-else>
								<button type="button" class="power-menu-item" @click="runAction('shutdownVM')">
									<b-icon icon="power" custom-size="mdi-16px"></b-icon><span>{{ $t('Shutdown') }}</span>
								</button>
								<button type="button" class="power-menu-item" @click="runAction('resetVM')">
									<b-icon icon="restart" custom-size="mdi-16px"></b-icon><span>{{ $t('Reset') }}</span>
								</button>
								<button type="button" class="power-menu-item is-danger" @click="runAction('forceOffVM')">
									<b-icon icon="power-plug-off-outline" custom-size="mdi-16px"></b-icon><span>{{ $t('Force off') }}</span>
								</button>
							</template>
						</div>
					</div>
				</div>

				<button type="button" v-if="showClose" class="toolbar-btn icon-only-btn close-btn" :title="$t('Close')" @click="$emit('close')">
					<b-icon icon="close" custom-size="mdi-16px"></b-icon>
				</button>
			</div>
		</div>
		<div ref="screen" class="console-screen"></div>
		<div v-if="status !== 'connected'" class="console-status">
			<b-icon v-if="status === 'connecting'" icon="loading" custom-class="mdi-spin" custom-size="mdi-36px"></b-icon>
			<b-icon v-else icon="lan-disconnect" custom-size="mdi-36px"></b-icon>
			<span>{{ statusText }}</span>
			<button v-if="status === 'disconnected'" class="reconnect-btn" @click="connect">{{ $t('Reconnect') }}</button>
		</div>

		<div v-if="keyboardOpen" ref="keyboard" class="on-screen-keyboard" :style="keyboardStyle">
			<div class="osk-header" @pointerdown="startKeyboardDrag">
				<b-icon icon="drag-horizontal-variant" size="is-small"></b-icon>
				<span class="osk-title">{{ $t('Keyboard') }}</span>
				<button type="button" class="osk-close" :title="$t('Close')" @pointerdown.stop @mousedown.stop @click.stop="closeKeyboard">
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
						>{{ keyLabel(key) }}</button>
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
						>{{ keyLabel(key) }}</button>
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
						>{{ key ? keyLabel(key) : '' }}</button>
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

		<div class="console-statusbar">
			<span class="statusbar-item" :class="{ 'is-live': status === 'connected' }">
				<span class="activity-dot"></span>{{ statusText }}
			</span>
			<span class="statusbar-item"><b-icon :icon="networkIcon" size="is-small"></b-icon>{{ networkSummary }}</span>
			<span class="statusbar-item"><b-icon icon="harddisk" size="is-small"></b-icon>{{ diskSummary }}</span>
			<span v-if="vm && vm.iso_path" class="statusbar-item"><b-icon icon="disc" size="is-small"></b-icon>{{ isoFileName }}</span>
			<span v-if="vm && vm.usb_devices && vm.usb_devices.length" class="statusbar-item"><b-icon icon="usb" size="is-small"></b-icon>{{ vm.usb_devices.length }}</span>
			<span v-if="sharedFolder && sharedFolder.attached" class="statusbar-item is-shared-live">
				<b-icon icon="folder-sync" size="is-small"></b-icon>{{ $t('USB Share') }} ({{ sharedFolder.size_mb >= 1024 ? (sharedFolder.size_mb / 1024) + ' GB' : sharedFolder.size_mb + ' MB' }})
			</span>
		</div>

		<vm-file-picker-dialog
			:active="showShareFolderPicker"
			:title="$t('Select Host Folder to Share')"
			start-path="/DATA"
			directory-mode
			@selected="onShareFolderSelected"
			@close="showShareFolderPicker = false"
		></vm-file-picker-dialog>

		<!-- Quick Paste & Type Modal -->
		<vm-overlay-panel :active="showPasteDialog" :title="$t('Paste Text to VM')" max-width="30rem" @close="showPasteDialog = false">
			<div class="paste-modal-content">
				<p class="paste-modal-desc">
					{{ $t('Type or paste text (Ctrl+V) to send to the virtual machine.') }}
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
					<button type="button" class="paste-action-btn is-primary" :disabled="!pasteText" @click="sendPasteText(false)">
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
import { vmSidecar } from '@/api/vmSidecar'
import VmDropdown from './VmDropdown.vue'
import VmFilePickerDialog from './VmFilePickerDialog.vue'
import VmOverlayPanel from './VmOverlayPanel.vue'

const STATE_POLL_MS = 3000

// noVNC's own qualityLevel (0-9, JPEG quality - higher looks better but
// sends more data) and compressionLevel (0-9, zlib - higher shrinks the
// stream further at the cost of CPU) trade actual network bandwidth for
// picture quality, unlike the Fit/1:1 toggle which only changes local
// canvas scaling and sends no less data either way.
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

// Standard X11 keysym values (keysymdef.h) for keys that aren't plain
// printable characters - stable, decades-old constants, not something
// noVNC's public API (just the RFB class) exposes a table for itself.
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

// One-click Ctrl+<letter> chords for the guest, since there's no host
// keyboard to physically hold Ctrl down while tapping another key.
// Its own vertical column at the far right, one key per row, each the
// same 1u size as a normal letter key.
const SHORTCUTS = [
	{ key: 'c', label: 'Copy', icon: 'content-copy' },
	{ key: 'x', label: 'Cut', icon: 'content-cut' },
	{ key: 'v', label: 'Paste', icon: 'content-paste' },
	{ key: 'z', label: 'Undo', icon: 'undo' },
	{ key: 'a', label: 'Select All', icon: 'select-all' },
]

// Printable keys give both their base and shift-held character - X11
// keysyms for the whole ASCII printable range are numerically identical
// to the character's own code point, so sending one is just
// `char.charCodeAt(0)` once the right character (base vs shift) is
// picked - no separate keysym table needed for these.
// Each key's `u` is its width in keyboard "units" (1u = a normal letter
// key) - real keycap proportions, not an even flex-stretch split. This is
// what makes rows of different lengths (15 keys in the number row, 6 in
// the nav cluster) still line up into a recognizable keyboard silhouette
// instead of each row stretching to fill the same total width.
const KEYBOARD_ROWS = [
	[
		{ code: 'Escape', special: 'Escape', label: 'Esc' },
		// gapBefore groups F1-4/F5-8/F9-12 the way a real function row
		// does, and not incidentally - it's also what stretches this row
		// out to the same total width as the number row below it (13
		// keys vs. that row's 15), so F12 actually lines up flush with
		// the side column instead of stopping ~5rem short of it.
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
		{ code: 'CapsLock', special: 'CapsLock', label: 'Caps', u: 1.75 }, { code: 'KeyA', base: 'a', shift: 'A' }, { code: 'KeyS', base: 's', shift: 'S' },
		{ code: 'KeyD', base: 'd', shift: 'D' }, { code: 'KeyF', base: 'f', shift: 'F' }, { code: 'KeyG', base: 'g', shift: 'G' },
		{ code: 'KeyH', base: 'h', shift: 'H' }, { code: 'KeyJ', base: 'j', shift: 'J' }, { code: 'KeyK', base: 'k', shift: 'K' },
		{ code: 'KeyL', base: 'l', shift: 'L' }, { code: 'Semicolon', base: ';', shift: ':' }, { code: 'Quote', base: "'", shift: '"' },
		{ code: 'Enter', special: 'Enter', label: 'Enter', u: 2.25 },
	],
	[
		{ code: 'ShiftLeft', special: 'Shift', label: 'Shift', sticky: 'shiftActive', u: 2.25 }, { code: 'KeyZ', base: 'z', shift: 'Z' }, { code: 'KeyX', base: 'x', shift: 'X' },
		{ code: 'KeyC', base: 'c', shift: 'C' }, { code: 'KeyV', base: 'v', shift: 'V' }, { code: 'KeyB', base: 'b', shift: 'B' },
		{ code: 'KeyN', base: 'n', shift: 'N' }, { code: 'KeyM', base: 'm', shift: 'M' }, { code: 'Comma', base: ',', shift: '<' },
		{ code: 'Period', base: '.', shift: '>' }, { code: 'Slash', base: '/', shift: '?' }, { code: 'ShiftRight', special: 'Shift', label: 'Shift', sticky: 'shiftActive', u: 2.75 },
	],
	[
		{ code: 'ControlLeft', special: 'Control', label: 'Ctrl', sticky: 'ctrlActive', u: 1.25 }, { code: 'MetaLeft', special: 'Super', label: '⊞', sticky: 'superActive', u: 1.25 },
		{ code: 'AltLeft', special: 'Alt', label: 'Alt', sticky: 'altActive', u: 1.25 },
		{ code: 'Space', base: ' ', label: 'Space', u: 6.25 },
		{ code: 'AltRight', special: 'Alt', label: 'Alt', sticky: 'altActive', u: 1.25 }, { code: 'MetaRight', special: 'Super', label: '⊞', sticky: 'superActive', u: 1.25 },
		{ code: 'ContextMenu', special: 'Menu', label: '☰', u: 1.25 }, { code: 'ControlRight', special: 'Control', label: 'Ctrl', sticky: 'ctrlActive', u: 1.25 },
	],
]

// The nav/arrow clusters sit in their own column to the right of the main
// block (osk-side), same as a real keyboard - not inline at the end of
// whichever row happened to have room.
const NAV_ROWS = [
	[
		{ code: 'Insert', special: 'Insert', label: 'Ins' }, { code: 'Home', special: 'Home', label: 'Home' }, { code: 'PageUp', special: 'PageUp', label: 'PgUp' },
	],
	[
		{ code: 'Delete', special: 'Delete', label: 'Del' }, { code: 'End', special: 'End', label: 'End' }, { code: 'PageDown', special: 'PageDown', label: 'PgDn' },
	],
]

// `null` cells render as invisible placeholders, so the arrow cluster
// keeps its familiar inverted-T shape instead of a solid 2x3 block.
const ARROW_ROWS = [
	[null, { code: 'ArrowUp', special: 'ArrowUp', label: '▲' }, null],
	[
		{ code: 'ArrowLeft', special: 'ArrowLeft', label: '◀' },
		{ code: 'ArrowDown', special: 'ArrowDown', label: '▼' },
		{ code: 'ArrowRight', special: 'ArrowRight', label: '▶' },
	],
]

export default {
	name: 'vm-console-panel',
	components: {
		VmDropdown,
		VmFilePickerDialog,
		VmOverlayPanel,
	},
	props: {
		vmName: { type: String, required: true },
		// Default false: as a desktop window, the shared titlebar already
		// has its own close button - a second one here would be redundant.
		// The standalone browser-tab wrapper explicitly opts in, since
		// there's nothing else to close a tab with.
		showClose: { type: Boolean, default: false },
	},
	data() {
		return {
			rfb: null,
			status: 'connecting',
			vmState: null,
			vm: null,
			showPasteDialog: false,
			pasteText: '',
			scaleToFit: true,
			// The VNC stream's own encoding quality/compression trade-off -
			// distinct from both the VM's display resolution (a libvirt
			// hardware hint) and the Fit/1:1 toggle above (a local view
			// setting). This is what actually trades picture quality for
			// less lag on a slow connection, since it changes how much data
			// noVNC asks the sidecar's VNC server to send per frame.
			qualityMode: (function () {
				try {
					return localStorage.getItem('vm_console_quality_mode') || 'balanced'
				} catch (e) {
					return 'balanced'
				}
			})(),
			qualityOptions: QUALITY_OPTIONS,
			qualityMenuOpen: false,
			keysMenuOpen: false,
			powerMenuOpen: false,
			netMenuOpen: false,
			netBusy: false,
			editingNet: false,
			editingNetMAC: '',
			netForm: { mode: 'nat', bridge_name: '', model: 'virtio', mac: '', link_state: 'up' },
			availableBridges: [],
			statePollTimer: null,
			usbMenuOpen: false,
			diskMenuOpen: false,
			shareMenuOpen: false,
			sharedFolder: null,
			selectedShareFolder: '',
			shareSizeMB: 1024,
			shareBusy: false,
			shareAction: '',
			showShareFolderPicker: false,
			loadingHostCaps: false,
			hostUsbDevices: [],
			usbBusy: false,
			diskBusy: false,
			availableISOs: [],
			selectedISO: '',
			keyboardOpen: false,
			keyboardPos: null,
			keyboardRows: KEYBOARD_ROWS,
			navRows: NAV_ROWS,
			arrowRows: ARROW_ROWS,
			shortcuts: SHORTCUTS,
			shiftActive: false,
			ctrlActive: false,
			altActive: false,
			superActive: false,
		}
	},
	computed: {
		statusText() {
			return (
				{
					connecting: this.$t('Connecting...'),
					connected: this.$t('Connected'),
					disconnected: this.$t('Disconnected'),
				}[this.status] || this.status
			)
		},
		networkIcon() {
			return this.vm && this.vm.network_mode && this.vm.network_mode.startsWith('bridge:') ? 'lan-connect' : 'lan'
		},
		// lan-connect's own artwork is visually denser than lan's at the same
		// font-size, so it reads as noticeably bigger/bolder next to it -
		// render it a size down to compensate.
		networkIconSize() {
			return this.vm && this.vm.network_mode && this.vm.network_mode.startsWith('bridge:') ? 'mdi-14px' : 'mdi-16px'
		},
		networkSummary() {
			if (!this.vm || !this.vm.network_mode) return this.$t('None')
			return this.vm.network_mode.startsWith('bridge:') ? this.vm.network_mode.replace('bridge:', '') : this.$t('NAT')
		},
		diskSummary() {
			const disks = (this.vm && this.vm.disks) || []
			if (!disks.length) return this.$t('None')
			const totalGiB = disks.reduce((sum, d) => sum + (d.gib || 0), 0)
			return `${disks.length} · ${totalGiB} GiB`
		},
		isoFileName() {
			const path = this.vm && this.vm.iso_path
			return path ? path.slice(path.lastIndexOf('/') + 1) : ''
		},
		keyboardStyle() {
			if (!this.keyboardPos) return {}
			return { left: this.keyboardPos.x + 'px', top: this.keyboardPos.y + 'px', bottom: 'auto', transform: 'none' }
		},
		qualityModeLabel() {
			return (
				{
					high: this.$t('High Quality'),
					balanced: this.$t('Balanced'),
					low: this.$t('Low Bandwidth'),
				}[this.qualityMode] || this.qualityMode
			)
		},
		isoDropdownOptions() {
			return (this.availableISOs || []).map((iso) => ({
				value: iso.name,
				label: iso.name,
				icon: 'disc',
				meta: iso.size_bytes ? `${Math.round((iso.size_bytes / (1024 * 1024 * 1024)) * 10) / 10} GB` : '',
			}))
		},
		bridgeDropdownOptions() {
			return (this.availableBridges || []).map((b) => ({
				value: b.name,
				label: b.name,
				icon: 'lan-connect',
			}))
		},
		networkModeOptions() {
			return [
				{ value: 'nat', label: this.$t('NAT (Shared Network)'), icon: 'lan', meta: 'Default' },
				{ value: 'bridge', label: this.$t('Bridge (Direct LAN Access)'), icon: 'lan-connect', meta: 'Bridged' },
			]
		},
		adapterModelOptions() {
			return [
				{ value: 'virtio', label: 'VirtIO', meta: 'Fastest (Linux/VirtIO-Win)', icon: 'speedometer' },
				{ value: 'e1000e', label: 'Intel e1000e', meta: 'Native Windows 10/11 & Linux', icon: 'microsoft-windows' },
				{ value: 'e1000', label: 'Intel e1000', meta: 'Universal Legacy', icon: 'lan' },
				{ value: 'rtl8139', label: 'Realtek RTL8139', meta: 'Legacy 10/100', icon: 'lan' },
			]
		},
	},
	watch: {
		netMenuOpen(open) {
			if (open) {
				this.editingNet = false
				this.loadAvailableNetworks()
			}
		},
	},
	mounted() {
		this.connect()
		this.pollState()
		this.statePollTimer = setInterval(this.pollState, STATE_POLL_MS)
		document.addEventListener('mousedown', this.onOutsideClick)
		window.addEventListener('resize', this.clampKeyboardPos)
		this.panelResizeObserver = new ResizeObserver(() => this.clampKeyboardPos())
		this.panelResizeObserver.observe(this.$el)
	},
	beforeDestroy() {
		if (this.rfb) this.rfb.disconnect()
		clearInterval(this.statePollTimer)
		document.removeEventListener('mousedown', this.onOutsideClick)
		window.removeEventListener('resize', this.clampKeyboardPos)
		if (this.panelResizeObserver) this.panelResizeObserver.disconnect()
		if (this.$refs.keyboard && this.$refs.keyboard.parentNode === document.body) {
			document.body.removeChild(this.$refs.keyboard)
		}
	},
	methods: {
		connect() {
			if (this.rfb) {
				this.rfb.disconnect()
				this.rfb = null
			}
			this.status = 'connecting'
			this.rfb = new RFB(this.$refs.screen, vmSidecar.consoleUrl(this.vmName))
			this.rfb.scaleViewport = this.scaleToFit
			const preset = QUALITY_PRESETS[this.qualityMode] || QUALITY_PRESETS.balanced
			this.rfb.qualityLevel = preset.qualityLevel
			this.rfb.compressionLevel = preset.compressionLevel
			this.rfb.addEventListener('connect', () => {
				this.status = 'connected'
				const p = QUALITY_PRESETS[this.qualityMode] || QUALITY_PRESETS.balanced
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
		},
		async pollState() {
			try {
				this.vm = await vmSidecar.getVM(this.vmName)
				this.vmState = this.vm ? this.vm.state : null
			} catch (e) {
				// VM may have just been deleted, or the sidecar is briefly
				// unreachable - keep showing the last known state rather than
				// flipping the power menu around on a transient error.
			}
		},
		toggleShareMenu() {
			this.shareMenuOpen = !this.shareMenuOpen
		},
		onShareFolderSelected(path) {
			this.selectedShareFolder = path
			const base = path.split('/').filter(Boolean).pop() || 'shared'
			this.shareTag = base
		},
		async attachShare() {
			if (!this.selectedShareFolder || this.shareBusy) return
			this.shareBusy = true
			try {
				const tag = this.shareTag || this.selectedShareFolder.split('/').filter(Boolean).pop() || 'shared'
				await vmSidecar.attachSharedFolder(this.vmName, {
					source_dir: this.selectedShareFolder,
					target_tag: tag,
					read_only: this.shareReadOnly,
				})
				this.selectedShareFolder = ''
				this.shareTag = ''
				this.$buefy.toast.open({
					message: this.$t('Live shared folder mounted!'),
					type: 'is-success',
					position: 'is-top',
					duration: 3500,
				})
				await this.pollState()
			} catch (e) {
				this.$buefy.toast.open({
					message: e.message || this.$t('Failed to mount shared folder'),
					type: 'is-danger',
					position: 'is-top',
					duration: 4000,
				})
			} finally {
				this.shareBusy = false
			}
		},
		async detachShare(tag) {
			if (this.shareBusy) return
			this.shareBusy = true
			try {
				await vmSidecar.detachSharedFolder(this.vmName, tag)
				this.$buefy.toast.open({
					message: this.$t('Shared folder unmounted!'),
					type: 'is-success',
					position: 'is-top',
					duration: 3500,
				})
				await this.pollState()
			} catch (e) {
				this.$buefy.toast.open({
					message: e.message || this.$t('Failed to unmount shared folder'),
					type: 'is-danger',
					position: 'is-top',
					duration: 4000,
				})
			} finally {
				this.shareBusy = false
			}
		},
		async insertVirtioWinCD() {
			this.virtioWinBusy = true
			try {
				await vmSidecar.insertVirtioWin(this.vmName)
				this.$buefy.toast.open({
					message: this.$t('VirtIO Drivers ISO inserted into CD drive! Open "This PC" in Windows to install the VirtIO-FS service.'),
					type: 'is-success',
					position: 'is-top',
					duration: 5000,
				})
				await this.pollState()
			} catch (e) {
				this.$buefy.toast.open({
					message: e.message || this.$t('Failed to insert VirtIO Drivers CD'),
					type: 'is-danger',
					position: 'is-top',
					duration: 4000,
				})
			} finally {
				this.virtioWinBusy = false
			}
		},
		copyCommand(cmd) {
			if (navigator.clipboard) {
				navigator.clipboard.writeText(cmd)
				this.$buefy.toast.open({
					message: this.$t('Command copied to clipboard!'),
					type: 'is-success',
					position: 'is-top',
					duration: 2500,
				})
			}
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
			} catch (e) {
				// Browser permission or non-secure HTTP context blocked direct read
			}
			if (text) {
				this.rfb.clipboardPasteFrom(text)
				this.$buefy.toast.open({
					message: this.$t('Pasted into VM clipboard'),
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
					message: this.$t('Typed text into VM'),
					type: 'is-success',
					position: 'is-top',
					duration: 2000,
				})
			} else {
				this.rfb.clipboardPasteFrom(this.pasteText)
				this.$buefy.toast.open({
					message: this.$t('Pasted into VM clipboard'),
					type: 'is-success',
					position: 'is-top',
					duration: 2000,
				})
			}
			this.pasteText = ''
			this.showPasteDialog = false
		},
		toggleScale() {
			this.scaleToFit = !this.scaleToFit
			if (this.rfb) this.rfb.scaleViewport = this.scaleToFit
		},
		setQualityMode(mode) {
			this.qualityMode = mode
			this.qualityMenuOpen = false
			try {
				localStorage.setItem('vm_console_quality_mode', mode)
			} catch (e) {}
			if (this.rfb) {
				const preset = QUALITY_PRESETS[mode] || QUALITY_PRESETS.balanced
				this.rfb.qualityLevel = preset.qualityLevel
				this.rfb.compressionLevel = preset.compressionLevel
			}
			this.$buefy.toast.open({
				message: `${this.$t('Bandwidth profile')}: ${this.qualityModeLabel}`,
				type: 'is-info',
				duration: 1800,
			})
		},
		toggleFullscreen() {
			if (document.fullscreenElement) {
				document.exitFullscreen()
			} else {
				this.$el.requestFullscreen()
			}
		},
		onOutsideClick(event) {
			if (!event || !event.target) return
			if (typeof event.target.closest === 'function') {
				if (event.target.closest('.vm-overlay') || event.target.closest('.modal') || event.target.closest('.dialog')) {
					return
				}
			}
			if (this.keysMenuOpen && this.$refs.keysMenuWrapper && !this.$refs.keysMenuWrapper.contains(event.target)) {
				this.keysMenuOpen = false
			}
			if (this.netMenuOpen && this.$refs.netMenuWrapper && !this.$refs.netMenuWrapper.contains(event.target)) {
				this.netMenuOpen = false
			}
			if (this.powerMenuOpen && this.$refs.powerMenuWrapper && !this.$refs.powerMenuWrapper.contains(event.target)) {
				this.powerMenuOpen = false
			}
			if (this.usbMenuOpen && this.$refs.usbMenuWrapper && !this.$refs.usbMenuWrapper.contains(event.target)) {
				this.usbMenuOpen = false
			}
			if (this.diskMenuOpen && this.$refs.diskMenuWrapper && !this.$refs.diskMenuWrapper.contains(event.target)) {
				this.diskMenuOpen = false
			}
			if (this.shareMenuOpen && this.$refs.shareMenuWrapper && !this.$refs.shareMenuWrapper.contains(event.target)) {
				this.shareMenuOpen = false
			}
			if (this.qualityMenuOpen && this.$refs.qualityMenuWrapper && !this.$refs.qualityMenuWrapper.contains(event.target)) {
				this.qualityMenuOpen = false
			}
		},
		closeKeyboard() {
			this.keyboardOpen = false
			if (this.$refs.keyboard && this.$refs.keyboard.parentNode === document.body) {
				document.body.removeChild(this.$refs.keyboard)
			}
		},
		async toggleNetworkLink(net) {
			if (!net.mac) return
			this.netBusy = true
			const targetState = net.link_state === 'down' ? 'up' : 'down'
			try {
				await vmSidecar.setNetworkLink(this.vmName, net.mac, targetState)
				net.link_state = targetState
				this.$buefy.toast.open({
					message: targetState === 'up' ? this.$t('Network connected') : this.$t('Network disconnected'),
					type: targetState === 'up' ? 'is-success' : 'is-warning',
					duration: 2000,
				})
				await this.pollState()
			} catch (e) {
				this.$buefy.toast.open({ message: e.message || this.$t('Failed to change network state'), type: 'is-danger' })
			} finally {
				this.netBusy = false
			}
		},
		startEditingNet(net) {
			const currentNet = net || (this.vm && this.vm.networks && this.vm.networks[0]) || {}
			this.editingNet = true
			this.editingNetMAC = currentNet.mac || ''
			const rawModel = (currentNet.model || 'virtio').toLowerCase()
			const rawMode = (currentNet.mode || 'nat').toLowerCase()
			this.netForm = {
				mode: rawMode === 'bridge' ? 'bridge' : 'nat',
				bridge_name: currentNet.bridge_name || (this.availableBridges[0] ? this.availableBridges[0].name : 'br0'),
				model: ['virtio', 'e1000e', 'e1000', 'rtl8139'].includes(rawModel) ? rawModel : 'virtio',
				mac: currentNet.mac || '',
				link_state: currentNet.link_state || 'up',
			}
			this.loadAvailableNetworks()
		},
		async saveNetworkAdapter() {
			this.netBusy = true
			try {
				await vmSidecar.updateNetworkAdapter(this.vmName, this.editingNetMAC, this.netForm)
				this.$buefy.toast.open({
					message: this.$t('Network adapter updated successfully'),
					type: 'is-success',
					duration: 2500,
				})
				this.editingNet = false
				await this.pollState()
			} catch (e) {
				this.$buefy.toast.open({ message: e.message || this.$t('Failed to update network adapter'), type: 'is-danger' })
			} finally {
				this.netBusy = false
			}
		},
		async loadAvailableNetworks() {
			try {
				const nets = await vmSidecar.listNetworks()
				this.availableBridges = (nets || []).filter((n) => n.mode === 'bridge')
			} catch (e) {}
		},
		async runAction(method) {
			this.powerMenuOpen = false
			try {
				await vmSidecar[method](this.vmName)
				await this.pollState()
				// A fresh start (or a reboot cycling the VNC server) has no
				// listening console yet the instant the API call returns -
				// give it a moment, then reconnect if we're not already.
				if (method === 'startVM' && this.status !== 'connected') {
					setTimeout(() => this.connect(), 2000)
				}
			} catch (e) {
				this.$buefy.toast.open({ message: e.message, type: 'is-danger' })
			}
		},
		isUsbAttached(dev) {
			return ((this.vm && this.vm.usb_devices) || []).some((d) => d.vendor_id.replace('0x', '') === dev.vendor_id.replace('0x', '') && d.product_id.replace('0x', '') === dev.product_id.replace('0x', ''))
		},
		async loadHostUsbDevices() {
			this.loadingHostCaps = true
			try {
				const caps = await vmSidecar.getHostCapabilities()
				this.hostUsbDevices = caps.usb_devices || []
			} catch (e) {
				this.hostUsbDevices = []
			} finally {
				this.loadingHostCaps = false
			}
		},
		async toggleUsbDevice(dev, attach) {
			this.usbBusy = true
			try {
				if (attach) {
					await vmSidecar.attachUSBDevice(this.vmName, { vendor_id: dev.vendor_id, product_id: dev.product_id })
				} else {
					await vmSidecar.detachUSBDevice(this.vmName, dev.vendor_id, dev.product_id)
				}
				await this.pollState()
			} catch (e) {
				this.$buefy.toast.open({ message: e.message, type: 'is-danger' })
			} finally {
				this.usbBusy = false
			}
		},
		async detachDiskConfirm(disk) {
			this.$buefy.dialog.confirm({
				title: this.$t('Detach Disk'),
				message: this.$t('Detach') + ` ${disk.target}? ` + this.$t('The backing file is kept, not deleted - only unplug a disk the guest has safely unmounted, the same risk as unplugging a live USB drive.'),
				confirmText: this.$t('Detach'),
				type: 'is-danger',
				onConfirm: async () => {
					this.diskBusy = true
					try {
						await vmSidecar.detachDisk(this.vmName, disk.target)
						await this.pollState()
					} catch (e) {
						this.$buefy.toast.open({ message: e.message, type: 'is-danger' })
					} finally {
						this.diskBusy = false
					}
				},
			})
		},
		async ejectBootISO() {
			this.diskBusy = true
			try {
				await vmSidecar.ejectCDROM(this.vmName)
				await this.pollState()
			} catch (e) {
				this.$buefy.toast.open({ message: e.message, type: 'is-danger' })
			} finally {
				this.diskBusy = false
			}
		},
		async loadAvailableISOs() {
			try {
				this.availableISOs = await vmSidecar.listISOs()
			} catch (e) {
				this.availableISOs = []
			}
		},
		async insertBootISO() {
			if (!this.selectedISO) return
			this.diskBusy = true
			try {
				// The sidecar always serves ISOs from this one fixed
				// directory (defaultISODir) - same convention the create-VM
				// wizard's file picker uses as its start-path.
				await vmSidecar.insertCDROM(this.vmName, `/DATA/VMs/isos/${this.selectedISO}`)
				this.selectedISO = ''
				await this.pollState()
			} catch (e) {
				this.$buefy.toast.open({ message: e.message, type: 'is-danger' })
			} finally {
				this.diskBusy = false
			}
		},
		stickyState(prop) {
			return this[prop]
		},
		// Fixed-unit key width (see KEYBOARD_ROWS's comment) - not a flex
		// stretch, so a key is the same width in every row it appears in.
		keyStyle(key) {
			const KEY_REM = 2.3
			const GAP_REM = 0.25
			const u = key.u || 1
			const style = { width: `${u * KEY_REM + (u - 1) * GAP_REM}rem` }
			if (key.gapBefore) style.marginLeft = `${key.gapBefore}rem`
			return style
		},
		startKeyboardDrag(event) {
			const header = event.currentTarget
			header.setPointerCapture(event.pointerId)
			const kbEl = this.$refs.keyboard
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
		// Re-pins a dragged keyboard back inside the viewport bounds -
		// called on every window resize / fullscreen toggle so it stays visible.
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
		// A one-click Ctrl+<letter> chord - there's no host key to physically
		// hold down while tapping another one on a click-only keyboard.
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
	},
	watch: {
		// The desktop window's own titlebar shows the connection pill now
		// (see DesktopWindow.vue) - it has no other way to know this
		// state, since it mounts this panel generically like any window
		// content.
		status: {
			immediate: true,
			handler(val) {
				this.$emit('status-change', val)
			},
		},
		keyboardOpen(open) {
			if (open) {
				this.$nextTick(() => {
					if (this.$refs.keyboard && this.$refs.keyboard.parentNode !== document.body) {
						document.body.appendChild(this.$refs.keyboard)
					}
					if (!this.keyboardPos && this.$refs.keyboard) {
						const kbRect = this.$refs.keyboard.getBoundingClientRect()
						this.keyboardPos = {
							x: Math.max(10, Math.round((window.innerWidth - kbRect.width) / 2)),
							y: Math.max(10, Math.round(window.innerHeight - kbRect.height - 40)),
						}
					}
					this.clampKeyboardPos()
				})
			} else {
				if (this.$refs.keyboard && this.$refs.keyboard.parentNode === document.body) {
					document.body.removeChild(this.$refs.keyboard)
				}
			}
		},
		usbMenuOpen(open) {
			if (open) this.loadHostUsbDevices()
		},
		diskMenuOpen(open) {
			if (open) this.loadAvailableISOs()
		},
	},
}
</script>

<style lang="scss" scoped>
.vm-console-panel {
	position: absolute;
	inset: 0;
	background: #000;
	display: flex;
	flex-direction: column;
	color: #fff;
}
.console-toolbar {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: 0.5rem;
	padding: 0.4rem 0.65rem;
	background: #141416;
	border-bottom: 1px solid rgba(255, 255, 255, 0.08);
	user-select: none;
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

	&.power-btn {
		color: rgba(255, 255, 255, 0.85);
		&.is-running {
			color: #34d399;
		}
	}

	&.close-btn:hover {
		background: rgba(239, 68, 68, 0.25);
		color: #ef4444;
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
.power-menu-right {
	left: auto !important;
	right: 0 !important;
}
.network-dropdown-menu {
	left: auto !important;
	right: 0 !important;
	width: 22.5rem;
	max-width: calc(100vw - 2rem);
	max-height: none !important;
	overflow: visible !important;
}
.device-menu-header-row {
	display: flex;
	align-items: center;
	justify-content: space-between;
	margin-bottom: 0.4rem;
}
.header-title-group {
	display: flex;
	align-items: center;
	gap: 0.45rem;

	.device-menu-title {
		margin: 0;
	}
}
.net-back-btn {
	border: none;
	background: rgba(255, 255, 255, 0.08);
	color: #fff;
	border-radius: 4px;
	padding: 0.15rem 0.35rem;
	cursor: pointer;
	display: flex;
	align-items: center;
	justify-content: center;

	&:hover {
		background: rgba(255, 255, 255, 0.16);
	}
}
.net-edit-toggle-btn {
	display: inline-flex;
	align-items: center;
	gap: 0.25rem;
	border: 1px solid rgba(255, 255, 255, 0.15);
	background: rgba(255, 255, 255, 0.08);
	color: rgba(255, 255, 255, 0.85);
	font-family: inherit;
	font-size: 0.72rem;
	font-weight: 500;
	padding: 0.2rem 0.55rem;
	border-radius: 6px;
	cursor: pointer;
	transition: all 0.15s ease;

	&:hover {
		background: rgba(255, 255, 255, 0.16);
		border-color: rgba(255, 255, 255, 0.25);
		color: #fff;
	}
}
.net-inline-editor {
	display: flex;
	flex-direction: column;
	gap: 0.75rem;
	padding: 0.75rem;
	background: rgba(0, 0, 0, 0.32);
	border: 1px solid rgba(255, 255, 255, 0.09);
	border-radius: 10px;
	margin-top: 0.25rem;
	overflow: visible !important;
}
.net-editor-section {
	display: flex;
	flex-direction: column;
	gap: 0.35rem;
}
.net-editor-label {
	font-size: 0.68rem;
	font-weight: 700;
	color: rgba(255, 255, 255, 0.55);
	text-transform: uppercase;
	letter-spacing: 0.03em;
}
.net-editor-actions {
	display: flex;
	align-items: center;
	justify-content: flex-end;
	gap: 0.5rem;
	margin-top: 0.25rem;
	padding-top: 0.65rem;
	border-top: 1px solid rgba(255, 255, 255, 0.08);
}
.net-cancel-btn {
	border: none;
	background: rgba(255, 255, 255, 0.07);
	color: rgba(255, 255, 255, 0.7);
	font-family: inherit;
	font-size: 0.75rem;
	font-weight: 500;
	padding: 0.38rem 0.75rem;
	border-radius: 6px;
	cursor: pointer;
	transition: all 0.12s ease;

	&:hover {
		color: #fff;
		background: rgba(255, 255, 255, 0.14);
	}
}
.net-save-btn {
	display: inline-flex;
	align-items: center;
	gap: 0.35rem;
	border: none;
	background: rgba(255, 255, 255, 0.18);
	color: #fff;
	font-family: inherit;
	font-size: 0.75rem;
	font-weight: 600;
	padding: 0.38rem 0.95rem;
	border-radius: 6px;
	cursor: pointer;
	transition: all 0.15s ease;

	&:hover:not(:disabled) {
		background: rgba(255, 255, 255, 0.28);
	}
	&:active:not(:disabled) {
		transform: scale(0.97);
	}
	&:disabled {
		opacity: 0.4;
		cursor: default;
	}
}
.net-menu-row {
	display: flex;
	align-items: center;
	gap: 0.6rem;
	padding: 0.45rem 0.55rem;
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
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}
.network-link-toggle {
	display: flex;
	align-items: center;
	gap: 0.3rem;
	border: 1px solid rgba(255, 255, 255, 0.15);
	background: rgba(255, 255, 255, 0.08);
	color: rgba(255, 255, 255, 0.8);
	font-family: inherit;
	font-size: 0.72rem;
	font-weight: 500;
	padding: 0.3rem 0.55rem;
	border-radius: 6px;
	cursor: pointer;
	flex-shrink: 0;
	transition: background 0.15s ease, border-color 0.15s ease, color 0.15s ease;

	&:hover:not(:disabled) {
		background: rgba(255, 255, 255, 0.18);
		color: #fff;
	}
	&.is-connected {
		background: rgba(16, 185, 129, 0.15);
		border-color: rgba(16, 185, 129, 0.35);
		color: #34d399;

		&:hover:not(:disabled) {
			background: rgba(239, 68, 68, 0.2);
			border-color: rgba(239, 68, 68, 0.4);
			color: #f87171;
		}
	}
	&:disabled {
		opacity: 0.4;
		cursor: default;
	}
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
	&.is-danger {
		color: #ff6b6b;
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
.console-screen {
	flex: 1 1 auto;
	min-height: 0;
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

	// Buefy's <b-icon> wraps every glyph in a Bulma .icon span fixed at
	// 1.5rem (24px) by default - custom-size only scales the glyph's own
	// font-size, so the mdi-36px icons here overflowed their own wrapper
	// unless it's resized to match.
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
.toolbar-btn.active {
	background: rgba(255, 255, 255, 0.28);
}
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
	width: 22rem;
	overflow: visible !important;
	display: flex;
	flex-direction: column;
	gap: 0.35rem;
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
	&::-webkit-scrollbar-track {
		background: transparent;
	}
}
.device-menu-title {
	font-size: 0.72rem;
	font-weight: 700;
	text-transform: uppercase;
	letter-spacing: 0.04em;
	color: rgba(255, 255, 255, 0.5);
	margin: 0 0 0.25rem;
}
.device-menu-hint {
	font-size: 0.75rem;
	color: rgba(255, 255, 255, 0.45);
	margin: 0.2rem 0;
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

	&:hover {
		background: rgba(255, 255, 255, 0.06);
	}
	&.active {
		background: rgba(37, 99, 235, 0.15);
	}
	&.disk-row {
		cursor: default;
		background: rgba(255, 255, 255, 0.03);
		margin-bottom: 0.25rem;
		&:hover {
			background: rgba(255, 255, 255, 0.05);
		}
	}
}
.device-row-icon {
	flex-shrink: 0;
	width: 2rem;
	height: 2rem;
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
.device-menu-checkbox {
	flex-shrink: 0;
	width: 1.05rem;
	height: 1.05rem;
	cursor: pointer;
}
.device-menu-title-divided {
	margin-top: 0.6rem;
	padding-top: 0.6rem;
	border-top: 1px solid rgba(255, 255, 255, 0.08);
}
.device-menu-desc {
	flex: 1 1 auto;
	min-width: 0;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}
.device-menu-detach {
	flex-shrink: 0;
	border: none;
	background: transparent;
	color: rgba(255, 255, 255, 0.5);
	cursor: pointer;
	display: flex;
	padding: 0.3rem;
	border-radius: 6px;
	transition: background 0.12s ease, color 0.12s ease;

	&:hover:not(:disabled) {
		background: rgba(239, 68, 68, 0.2);
		color: #f87171;
	}
	&:disabled {
		opacity: 0.4;
		cursor: default;
	}
}
.device-menu-add {
	display: flex;
	align-items: center;
	gap: 0.45rem;
	margin-top: 0.4rem;
	padding-top: 0.6rem;
	border-top: 1px solid rgba(255, 255, 255, 0.08);
}
.custom-select-wrapper {
	position: relative;
	flex: 1 1 auto;
	min-width: 0;
	display: flex;
	align-items: center;
}
.device-menu-select {
	width: 100%;
	height: 2.15rem;
	background: rgba(255, 255, 255, 0.07);
	border: 1px solid rgba(255, 255, 255, 0.16);
	border-radius: 8px;
	color: #f8fafc;
	font-family: inherit;
	font-size: 0.78rem;
	font-weight: 500;
	padding: 0 1.9rem 0 0.65rem;
	appearance: none;
	-webkit-appearance: none;
	-moz-appearance: none;
	cursor: pointer;
	outline: none;
	transition: border-color 0.15s ease, background 0.15s ease;

	&:hover:not(:disabled) {
		background: rgba(255, 255, 255, 0.1);
		border-color: rgba(255, 255, 255, 0.28);
	}
	&:focus {
		border-color: #3b82f6;
		box-shadow: 0 0 0 2px rgba(59, 130, 246, 0.25);
	}
	&:disabled {
		opacity: 0.4;
		cursor: default;
	}
	option {
		background: #1e1e1e;
		color: #f8fafc;
		padding: 6px 10px;
	}
}
.select-chevron {
	position: absolute;
	right: 0.5rem;
	pointer-events: none;
	color: rgba(255, 255, 255, 0.5);
}
.device-menu-attach-btn {
	display: inline-flex;
	align-items: center;
	gap: 0.35rem;
	border: none;
	background: rgba(255, 255, 255, 0.16);
	color: #fff;
	font-family: inherit;
	font-size: 0.78rem;
	font-weight: 600;
	padding: 0 0.85rem;
	height: 2.15rem;
	border-radius: 8px;
	cursor: pointer;
	flex-shrink: 0;
	transition: background 0.15s ease, transform 0.1s ease;

	&:hover:not(:disabled) {
		background: rgba(255, 255, 255, 0.26);
	}
	&:active:not(:disabled) {
		transform: scale(0.97);
	}
	&:disabled {
		opacity: 0.4;
		cursor: default;
	}
}
// A floating overlay, not a docked panel - it sits on top of the console
// screen (and can overlap the statusbar) so it behaves like an on-screen
// keyboard laid over whatever's underneath, the same way a real external
// keyboard doesn't resize the monitor's picture to make room for itself.
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
// Keeps the nav cluster (Ins/Home/PgUp/Del/End/PgDn) starting at the
// number row's height, not the function row's - same as a real keyboard,
// where that block sits below the F-keys, not level with them.
.osk-fn-spacer {
	height: 2.3rem;
	visibility: hidden;
}
// Pushes the arrow cluster down to the bottom, level with the shift and
// modifier rows, instead of it sitting right under the nav cluster.
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
.statusbar-item.is-shared-live {
	color: #34d399;
	font-weight: 500;
}

/* File Share Menu Styles */
.share-menu {
	width: 22rem;
	right: 0 !important;
	left: auto !important;
}

.share-form-group {
	display: flex;
	flex-direction: column;
	gap: 0.35rem;
	margin-top: 0.4rem;
}

.share-label {
	font-size: 0.72rem;
	font-weight: 600;
	color: rgba(255, 255, 255, 0.7);
	text-transform: uppercase;
	letter-spacing: 0.03em;
}

.share-folder-picker-row {
	display: flex;
	align-items: center;
	gap: 0.4rem;
	background: rgba(255, 255, 255, 0.05);
	border: 1px solid rgba(255, 255, 255, 0.12);
	border-radius: 8px;
	padding: 0.35rem 0.5rem;
}

.share-folder-path {
	flex: 1 1 auto;
	font-size: 0.78rem;
	color: #fff;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
	font-family: monospace;

	&.is-empty {
		color: rgba(255, 255, 255, 0.35);
		font-family: inherit;
		font-style: italic;
	}
}

.share-browse-btn {
	border: none;
	background: rgba(255, 255, 255, 0.12);
	color: #fff;
	border-radius: 6px;
	padding: 0.25rem 0.55rem;
	font-family: inherit;
	font-size: 0.72rem;
	font-weight: 500;
	cursor: pointer;
	display: flex;
	align-items: center;
	gap: 0.3rem;
	transition: background 0.15s ease;

	&:hover {
		background: rgba(255, 255, 255, 0.2);
	}
}

.share-add-box {
	display: flex;
	flex-direction: column;
	gap: 0.35rem;
	margin-top: 0.35rem;
	background: rgba(255, 255, 255, 0.04);
	padding: 0.55rem;
	border-radius: 8px;
	border: 1px solid rgba(255, 255, 255, 0.08);
}

.share-tag-row {
	display: flex;
	gap: 0.35rem;
	margin-top: 0.35rem;
}

.share-tag-input {
	flex: 1;
	background: rgba(0, 0, 0, 0.4);
	border: 1px solid rgba(255, 255, 255, 0.15);
	border-radius: 6px;
	padding: 0.35rem 0.5rem;
	color: #fff;
	font-family: monospace;
	font-size: 0.75rem;

	&:focus {
		outline: none;
		border-color: rgba(255, 255, 255, 0.4);
	}
}

.share-action-btn {
	display: inline-flex;
	align-items: center;
	justify-content: center;
	gap: 0.35rem;
	border: none;
	border-radius: 6px;
	padding: 0.35rem 0.75rem;
	font-family: inherit;
	font-size: 0.75rem;
	font-weight: 600;
	cursor: pointer;
	background: rgba(255, 255, 255, 0.14);
	color: #fff;
	white-space: nowrap;
	transition: background 0.15s ease;

	&:hover:not(:disabled) {
		background: rgba(255, 255, 255, 0.24);
	}

	&.is-primary {
		background: rgba(255, 255, 255, 0.2);
		color: #fff;
		&:hover:not(:disabled) {
			background: rgba(255, 255, 255, 0.3);
		}
	}

	&:disabled {
		opacity: 0.4;
		cursor: default;
	}
}

.share-active-box {
	display: flex;
	align-items: center;
	gap: 0.55rem;
	background: rgba(255, 255, 255, 0.05);
	border: 1px solid rgba(255, 255, 255, 0.1);
	border-radius: 8px;
	padding: 0.45rem 0.6rem;
	margin-top: 0.25rem;
}

.share-active-icon {
	width: 1.8rem;
	height: 1.8rem;
	border-radius: 6px;
	background: rgba(255, 255, 255, 0.1);
	color: rgba(255, 255, 255, 0.9);
	display: flex;
	align-items: center;
	justify-content: center;
	flex-shrink: 0;
}

.share-active-details {
	display: flex;
	flex-direction: column;
	gap: 0.15rem;
	min-width: 0;
	flex: 1 1 auto;
}

.share-active-path {
	font-size: 0.75rem;
	font-weight: 600;
	color: #fff;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
	font-family: monospace;
}

.share-active-meta {
	font-size: 0.68rem;
	color: rgba(255, 255, 255, 0.6);
	display: flex;
	align-items: center;
	gap: 0.35rem;

	.tag-pill {
		background: rgba(255, 255, 255, 0.1);
		color: #fff;
		padding: 0.05rem 0.35rem;
		border-radius: 4px;
		font-family: monospace;
		font-weight: 600;
	}
}

.share-remove-btn {
	border: none;
	background: transparent;
	color: rgba(255, 255, 255, 0.5);
	cursor: pointer;
	padding: 0.25rem;
	border-radius: 4px;
	display: flex;
	align-items: center;
	justify-content: center;
	transition: all 0.12s ease;

	&:hover {
		color: #ef4444;
		background: rgba(239, 68, 68, 0.15);
	}
}

.share-instructions-card {
	margin-top: 0.5rem;
	padding: 0.55rem;
	background: rgba(0, 0, 0, 0.35);
	border: 1px solid rgba(255, 255, 255, 0.08);
	border-radius: 8px;
	display: flex;
	flex-direction: column;
	gap: 0.35rem;
}

.instructions-header {
	display: flex;
	align-items: center;
	justify-content: space-between;
}

.instructions-title {
	font-size: 0.68rem;
	font-weight: 700;
	color: rgba(255, 255, 255, 0.6);
	text-transform: uppercase;
	letter-spacing: 0.03em;
}

.os-tab-buttons {
	display: inline-flex;
	background: rgba(255, 255, 255, 0.08);
	border-radius: 5px;
	padding: 0.1rem;
}

.os-tab-btn {
	border: none;
	background: transparent;
	color: rgba(255, 255, 255, 0.7);
	font-family: inherit;
	font-size: 0.68rem;
	font-weight: 600;
	padding: 0.15rem 0.45rem;
	border-radius: 4px;
	cursor: pointer;

	&.active {
		background: rgba(255, 255, 255, 0.25);
		color: #fff;
	}
}

.code-snippet-box {
	display: flex;
	align-items: center;
	justify-content: space-between;
	background: rgba(0, 0, 0, 0.4);
	border: 1px solid rgba(255, 255, 255, 0.09);
	border-radius: 6px;
	padding: 0.35rem 0.5rem;
	gap: 0.4rem;

	code {
		font-size: 0.72rem;
		font-family: monospace;
		color: rgba(255, 255, 255, 0.9);
		overflow-x: auto;
		white-space: nowrap;
	}
}

.code-copy-btn {
	border: none;
	background: rgba(255, 255, 255, 0.1);
	color: rgba(255, 255, 255, 0.8);
	border-radius: 4px;
	padding: 0.2rem;
	cursor: pointer;
	display: flex;
	align-items: center;
	justify-content: center;
	transition: all 0.12s ease;

	&:hover {
		color: #fff;
		background: rgba(255, 255, 255, 0.2);
	}
}

.win-instruct-text {
	font-size: 0.7rem;
	color: rgba(255, 255, 255, 0.7);
	line-height: 1.3;
	margin-bottom: 0.35rem;
}

.insert-virtio-btn {
	display: inline-flex;
	align-items: center;
	justify-content: center;
	gap: 0.4rem;
	width: 100%;
	border: 1px solid rgba(255, 255, 255, 0.15);
	background: rgba(255, 255, 255, 0.08);
	color: #fff;
	font-family: inherit;
	font-size: 0.75rem;
	font-weight: 600;
	padding: 0.35rem;
	border-radius: 6px;
	cursor: pointer;
	transition: all 0.15s ease;

	&:hover:not(:disabled) {
		background: rgba(255, 255, 255, 0.16);
		color: #fff;
	}
	&:disabled {
		opacity: 0.4;
		cursor: default;
	}
}

.share-badge-dot {
	width: 7px;
	height: 7px;
	border-radius: 50%;
	background: #34d399;
	display: inline-block;
	margin-left: 0.2rem;
	box-shadow: 0 0 6px #34d399;
}

.share-connected-badge {
	display: inline-flex;
	align-items: center;
	gap: 0.35rem;
	font-size: 0.68rem;
	font-weight: 600;
	color: #34d399;
	background: rgba(16, 185, 129, 0.15);
	padding: 0.15rem 0.45rem;
	border-radius: 9999px;
}

.power-menu-item.is-start {
	color: #4ade80;
	&:hover {
		background: rgba(74, 222, 128, 0.15);
	}
}

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
