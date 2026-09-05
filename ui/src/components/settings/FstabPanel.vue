<template>
	<div class="fstab-panel">
		<!-- Panel Header -->
		<div class="fstab-header">
			<div class="header-left">
				<div class="header-title-row">
					<h3 class="setting-card-title mb-0">{{ $t('Mounted Drives') }}</h3>
					<div class="status-counters">
						<span class="count-pill is-primary" :title="$t('Managed persistent mounts')">
							<i class="mdi mdi-check-circle-outline mr-1"></i>{{ mounts.length }} {{ $t('Managed') }}
						</span>
						<span v-if="candidates.length" class="count-pill is-info" :title="$t('Available unmanaged drives detected')">
							<i class="mdi mdi-harddisk-plus mr-1"></i>{{ candidates.length }} {{ $t('Available to Mount') }}
						</span>
					</div>
				</div>
				<p class="hint">
					{{ $t('Mount internal or external drives preserving all existing files. Configured with safe boot options and automatically mounted at startup.') }}
				</p>
			</div>
			<div class="header-actions">
				<button class="icon-button" type="button" :title="$t('Refresh')" :disabled="loadingMounts || loadingCandidates" @click="refresh">
					<i class="mdi mdi-refresh" :class="{ 'mdi-spin': loadingMounts || loadingCandidates }"></i>
				</button>
				<b-button rounded size="is-small" type="is-primary" icon-left="plus" class="add-mount-btn" @click="openAddWizard()">
					{{ $t('Mount Drive') }}
				</b-button>
			</div>
		</div>

		<!-- Managed Drives Section -->
		<div class="mounts-container">
			<b-loading v-model="loadingMounts" :is-full-page="false"></b-loading>

			<!-- Managed Drives Card Grid -->
			<div v-if="mounts.length" class="mount-cards-grid">
				<div v-for="m in mounts" :key="m.mount_point" class="mount-card" :class="{ 'is-unmounted': !m.mounted, 'is-disabled': !m.enabled, 'is-missing': isDeviceMissing(m) }">
					<!-- Card Header -->
					<div class="mount-card-header">
						<div class="drive-icon-wrap" :class="getDriveIconClass(m.fstype)">
							<i class="mdi" :class="isDeviceMissing(m) ? 'mdi-harddisk-remove' : driveIcon(m.fstype)"></i>
						</div>
						<div class="drive-titles">
							<div class="drive-name-row">
								<span class="drive-main-name one-line" :title="m.drive_label || m.mount_point">
									{{ isDeviceMissing(m) ? $t('Drive not detected') : (m.drive_label || m.drive_path || m.mount_point) }}
								</span>
								<span class="fs-badge">{{ (m.fstype || 'auto').toUpperCase() }}</span>
							</div>
							<div class="drive-source-meta one-line" :title="m.source">
								<i class="mdi mdi-disc mr-1"></i>{{ m.drive_path || m.source }}
							</div>
						</div>
						<div class="mount-status-badges">
							<span v-if="isDeviceMissing(m)" class="status-pill is-missing" :title="$t('This drive is not currently connected. If you replaced or reformatted it, remove this entry and mount the new drive.')">
								<span class="status-dot is-bad"></span>
								{{ $t('Not Detected') }}
							</span>
							<span v-else class="status-pill" :class="m.mounted ? 'is-mounted' : 'is-unmounted'">
								<span class="status-dot" :class="{ 'is-good': m.mounted }"></span>
								{{ m.mounted ? $t('Mounted') : $t('Unmounted') }}
							</span>
						</div>
					</div>

					<!-- Card Body: Mount Point & Specs -->
					<div class="mount-card-body">
						<div class="mount-target-box" @click="copyToClipboard(m.mount_point)">
							<i class="mdi mdi-folder-outline target-icon"></i>
							<span class="target-path one-line" :title="m.mount_point">{{ m.mount_point }}</span>
							<button class="copy-btn" type="button" :title="$t('Copy mount path')">
								<i class="mdi mdi-content-copy"></i>
							</button>
						</div>

						<div class="mount-specs-row">
							<span v-if="m.size" class="spec-item">
								<i class="mdi mdi-database-outline mr-1"></i>{{ formatSize(m.size) }}
							</span>
							<span v-if="m.read_only" class="spec-item is-warning">
								<i class="mdi mdi-lock-outline mr-1"></i>{{ $t('Read-Only') }}
							</span>
							<span v-if="!m.enabled" class="spec-item is-muted">
								<i class="mdi mdi-pause-circle-outline mr-1"></i>{{ $t('Disabled at Boot') }}
							</span>
							<span v-else class="spec-item is-success">
								<i class="mdi mdi-power-cycle mr-1"></i>{{ $t('Auto-mount at Boot') }}
							</span>
						</div>

						<div v-if="m.options" class="options-preview one-line" :title="m.options">
							<code>{{ m.options }}</code>
						</div>
					</div>

					<!-- Card Footer: Actions -->
					<div class="mount-card-footer">
						<div class="boot-toggle-wrap" :title="$t('Automatically mount this drive when server starts')">
							<span class="boot-toggle-label">{{ $t('Boot auto-mount') }}</span>
							<b-switch :value="m.enabled" size="is-small" type="is-primary" @input="toggleEnabled(m, $event)"></b-switch>
						</div>
						<div class="action-buttons-wrap">
							<!-- Mount / Unmount Toggle -->
							<button
								v-if="m.mounted"
								class="action-btn is-warning-light"
								type="button"
								:disabled="actionBusy === m.mount_point"
								:title="$t('Unmount drive')"
								@click="toggleMountState(m, false)"
							>
								<i class="mdi" :class="actionBusy === m.mount_point ? 'mdi-loading mdi-spin' : 'mdi-eject-outline'"></i>
								<span>{{ $t('Unmount') }}</span>
							</button>
							<button
								v-else
								class="action-btn is-success-light"
								type="button"
								:disabled="actionBusy === m.mount_point"
								:title="$t('Mount drive now')"
								@click="toggleMountState(m, true)"
							>
								<i class="mdi" :class="actionBusy === m.mount_point ? 'mdi-loading mdi-spin' : 'mdi-power-plug-outline'"></i>
								<span>{{ $t('Mount') }}</span>
							</button>

							<!-- Edit Settings -->
							<button class="action-btn" type="button" :title="$t('Edit mount settings')" @click="openEditModal(m)">
								<i class="mdi mdi-pencil-outline"></i>
								<span>{{ $t('Edit') }}</span>
							</button>

							<!-- Delete / Remove -->
							<button class="action-btn is-danger-light" type="button" :title="$t('Remove from fstab (files are safe)')" @click="confirmRemove(m)">
								<i class="mdi" :class="removing === m.mount_point ? 'mdi-loading mdi-spin' : 'mdi-trash-can-outline'"></i>
							</button>
						</div>
					</div>
				</div>
			</div>

			<!-- Empty State when no managed mounts -->
			<div v-else-if="!loadingMounts" class="fstab-empty-card">
				<div class="empty-icon-circle">
					<i class="mdi mdi-harddisk-plus"></i>
				</div>
				<h4 class="empty-title">{{ $t('No persistent drives mounted yet') }}</h4>
				<p class="empty-desc">
					{{ $t('Add any internal or external drive (NTFS, EXT4, exFAT, BTRFS) without formatting. Keep all your existing files and have them mounted automatically on boot.') }}
				</p>
				<b-button rounded size="is-small" type="is-primary" icon-left="plus" class="mt-2" @click="openAddWizard()">
					{{ $t('Mount an Existing Drive') }}
				</b-button>
			</div>
		</div>

		<!-- Other Startup / System Entries Section -->
		<div class="system-section">
			<button type="button" class="system-entries-toggle" @click="showSystem = !showSystem">
				<i class="mdi" :class="showSystem ? 'mdi-chevron-down' : 'mdi-chevron-right'"></i>
				<span>{{ $t('System & Pre-existing Startup Drives ({count})', { count: systemEntries.length }) }}</span>
			</button>

			<div v-if="showSystem" class="system-card mt-2">
				<p class="system-desc">
					{{ $t('Pre-existing system mounts (root filesystem, boot EFI, swap, or manual entries). You can adopt storage drives to manage them via the WebUI.') }}
				</p>
				<div class="system-table-wrap">
					<div v-for="e in systemEntries" :key="e.mount_point" class="system-row">
						<div class="system-row-icon">
							<i class="mdi" :class="isSystemProtected(e.mount_point) ? 'mdi-shield-lock-outline' : driveIcon(e.fstype)"></i>
						</div>
						<div class="system-row-info">
							<div class="system-mount-title">
								<span class="system-path">{{ e.mount_point }}</span>
								<span class="fs-badge ml-2">{{ (e.fstype || 'auto').toUpperCase() }}</span>
								<span v-if="isSystemProtected(e.mount_point)" class="protected-pill ml-2">{{ $t('System Protected') }}</span>
							</div>
							<div class="system-meta one-line">
								<span>{{ e.source }}</span>
								<span v-if="e.size">&middot; {{ formatSize(e.size) }}</span>
								<span v-if="e.options">&middot; {{ e.options }}</span>
							</div>
						</div>
						<div class="system-row-actions">
							<b-button
								v-if="!isSystemProtected(e.mount_point)"
								rounded
								size="is-small"
								type="is-dark"
								outlined
								icon-left="shield-plus-outline"
								:loading="adopting === e.mount_point"
								@click="adoptEntry(e)"
							>
								{{ $t('Manage with WebUI') }}
							</b-button>
						</div>
					</div>
					<div v-if="!systemEntries.length" class="account-empty">{{ $t('No other entries found.') }}</div>
				</div>
			</div>
		</div>

		<!-- In-Window Overlay Modal: Mount Drive Wizard -->
		<div v-if="showAddModal" class="in-window-modal-backdrop" @click.self="showAddModal = false">
			<div class="in-window-modal-card">
				<div class="modal-card-header">
					<div class="modal-header-text">
						<h4 class="modal-card-title">{{ $t('Mount Existing Drive') }}</h4>
						<p class="modal-subtitle">{{ $t('Mount a drive preserving all files and data. No formatting required.') }}</p>
					</div>
					<button class="modal-close-btn" type="button" @click="showAddModal = false">
						<i class="mdi mdi-close"></i>
					</button>
				</div>

				<div class="modal-card-body">
					<!-- Step 1: Pick Drive / Partition -->
					<div class="wizard-section">
						<label class="wizard-label">
							<span class="step-num">1</span>
							<span>{{ $t('Select a Drive or Partition') }}</span>
						</label>

						<div v-if="candidates.length" class="candidate-grid">
							<div
								v-for="c in candidates"
								:key="c.uuid"
								class="candidate-card"
								:class="{ active: addDraft.uuid === c.uuid }"
								@click="selectCandidate(c)"
							>
								<div class="candidate-icon" :class="getDriveIconClass(c.fstype)">
									<i class="mdi" :class="driveIcon(c.fstype)"></i>
								</div>
								<div class="candidate-details">
									<div class="candidate-name one-line">{{ c.label || c.path }}</div>
									<div class="candidate-meta">
										<span>{{ formatSize(c.size) }}</span> &middot;
										<span class="fs-text">{{ c.fstype }}</span>
										<span v-if="c.parent_model" class="model-text"> &middot; {{ c.parent_model }}</span>
									</div>
								</div>
								<div v-if="addDraft.uuid === c.uuid" class="check-mark">
									<i class="mdi mdi-check"></i>
								</div>
							</div>
						</div>
						<div v-else-if="!loadingCandidates" class="empty-candidate-box">
							<p class="hint">{{ $t('No unmounted formatted partitions detected automatically. You can enter device path or UUID manually below.') }}</p>
						</div>

						<a class="manual-uuid-toggle mt-2" @click="showManualDevice = !showManualDevice">
							<i class="mdi" :class="showManualDevice ? 'mdi-chevron-up' : 'mdi-chevron-down'"></i>
							{{ $t('Or enter custom Device Path / UUID') }}
						</a>
						<div v-if="showManualDevice" class="manual-device-box mt-2">
							<b-field :label="$t('Drive UUID or Device Path')">
								<b-input v-model="addDraft.uuid" placeholder="e.g. B000FE9A00FE66AE or /dev/sdb1" size="is-small"></b-input>
							</b-field>
						</div>
					</div>

					<!-- Step 2: Choose Purpose Preset -->
					<div v-if="addDraft.uuid" class="wizard-section mt-4">
						<label class="wizard-label">
							<span class="step-num">2</span>
							<span>{{ $t('Choose Drive Purpose & Preset') }}</span>
						</label>
						<p class="wizard-subhint">{{ $t('Presets automatically optimize filesystem permissions, performance, and boot flags.') }}</p>

						<div class="preset-cards-grid">
							<div
								v-for="p in visiblePresets"
								:key="p.id"
								class="preset-card"
								:class="{ active: presetMatches(addDraft, p) }"
								@click="applyPreset(addDraft, p)"
							>
								<div class="preset-icon-wrap">
									<i class="mdi" :class="`mdi-${p.icon}`"></i>
								</div>
								<div class="preset-info">
									<span class="preset-title">{{ $t(p.label) }}</span>
									<span class="preset-desc">{{ $t(p.desc) }}</span>
								</div>
								<div v-if="presetMatches(addDraft, p)" class="preset-selected-dot">
									<i class="mdi mdi-check"></i>
								</div>
							</div>
						</div>
					</div>

					<!-- Step 3: Mount Target Location -->
					<div v-if="addDraft.uuid" class="wizard-section mt-4">
						<label class="wizard-label">
							<span class="step-num">3</span>
							<span>{{ $t('Mount Target Directory') }}</span>
						</label>
						<b-field :message="$t('Where this drive will be accessible in Files (e.g. /DATA/Movies, /DATA/Storage)')">
							<b-input v-model="addDraft.mount_point" placeholder="/DATA/..." size="is-small" expanded></b-input>
						</b-field>

						<!-- Quick Target Shortcuts -->
						<div class="quick-path-pills">
							<span class="quick-title">{{ $t('Quick suggestions:') }}</span>
							<button v-for="tag in getQuickPaths(addDraft)" :key="tag" type="button" class="path-pill" @click="addDraft.mount_point = tag">
								{{ tag }}
							</button>
						</div>
					</div>

					<!-- Step 4: Boot & Safety Options -->
					<div v-if="addDraft.uuid" class="wizard-section mt-4">
						<label class="wizard-label">
							<span class="step-num">4</span>
							<span>{{ $t('Startup & Safety Settings') }}</span>
						</label>

						<div class="switch-card">
							<div class="switch-card-row">
								<div class="switch-card-text">
									<span class="title-text">{{ $t('Mount automatically on server startup') }}</span>
									<span class="sub-text">{{ $t('Ensures drive is ready every time system reboots') }}</span>
								</div>
								<b-switch v-model="addDraft.mount_at_boot" size="is-small" type="is-primary"></b-switch>
							</div>

							<div class="switch-card-row">
								<div class="switch-card-text">
									<span class="title-text">{{ $t('Read-only mode') }}</span>
									<span class="sub-text">{{ $t('Protects all data from being modified or deleted') }}</span>
								</div>
								<b-switch v-model="addDraft.read_only" size="is-small" type="is-primary"></b-switch>
							</div>
						</div>

						<!-- Advanced Options Expander -->
						<a class="manual-uuid-toggle mt-3" @click="showAddAdvanced = !showAddAdvanced">
							<i class="mdi" :class="showAddAdvanced ? 'mdi-chevron-up' : 'mdi-chevron-down'"></i>
							{{ $t('Advanced Mount Flags') }}
						</a>
						<div v-if="showAddAdvanced" class="advanced-box mt-2">
							<b-field :label="$t('Filesystem Type Override')">
								<b-input v-model="addDraft.fstype" placeholder="e.g. ntfs-3g, ext4, btrfs, exfat" size="is-small"></b-input>
							</b-field>
							<div class="switch-card-row p-0 mb-3">
								<div class="switch-card-text">
									<span class="title-text">{{ $t('Check filesystem at boot (fsck)') }}</span>
									<span class="sub-text">{{ $t('Scans filesystem for integrity during system startup') }}</span>
								</div>
								<b-switch v-model="addDraft.check_at_boot" size="is-small" type="is-primary"></b-switch>
							</div>
							<b-field :label="$t('Extra mount options')">
								<b-input v-model="addDraft.options" :placeholder="$t('e.g. noatime,uid=1000,gid=1000')" size="is-small"></b-input>
							</b-field>
						</div>
					</div>

					<div v-if="formError" class="modal-error-alert mt-3">
						<i class="mdi mdi-alert-circle-outline mr-2"></i>
						<span>{{ formError }}</span>
					</div>
				</div>

				<div class="modal-card-footer">
					<b-button rounded size="is-small" @click="showAddModal = false">{{ $t('Cancel') }}</b-button>
					<b-button
						rounded
						size="is-small"
						type="is-primary"
						icon-left="check"
						:loading="submitting"
						:disabled="!addDraft.uuid || !addDraft.mount_point"
						@click="submitAdd"
					>
						{{ $t('Mount & Save to fstab') }}
					</b-button>
				</div>
			</div>
		</div>

		<!-- In-Window Overlay Modal: Edit Mount Settings -->
		<div v-if="showEditModal" class="in-window-modal-backdrop" @click.self="showEditModal = false">
			<div class="in-window-modal-card">
				<div class="modal-card-header">
					<div class="modal-header-text">
						<h4 class="modal-card-title">{{ $t('Edit Mount Settings') }}</h4>
						<p class="modal-subtitle">{{ editDraft.drive_label || editDraft.mount_point }} &middot; {{ editDraft.source }}</p>
					</div>
					<button class="modal-close-btn" type="button" @click="showEditModal = false">
						<i class="mdi mdi-close"></i>
					</button>
				</div>

				<div class="modal-card-body">
					<!-- Presets Selection -->
					<div class="wizard-section">
						<label class="wizard-label">
							<span>{{ $t('Purpose Preset') }}</span>
						</label>
						<div class="preset-cards-grid">
							<div
								v-for="p in visiblePresetsFor(editDraft.fstype)"
								:key="p.id"
								class="preset-card"
								:class="{ active: presetMatches(editDraft, p) }"
								@click="applyPreset(editDraft, p)"
							>
								<div class="preset-icon-wrap">
									<i class="mdi" :class="`mdi-${p.icon}`"></i>
								</div>
								<div class="preset-info">
									<span class="preset-title">{{ $t(p.label) }}</span>
									<span class="preset-desc">{{ $t(p.desc) }}</span>
								</div>
								<div v-if="presetMatches(editDraft, p)" class="preset-selected-dot">
									<i class="mdi mdi-check"></i>
								</div>
							</div>
						</div>
					</div>

					<!-- Mount Target -->
					<div class="wizard-section mt-4">
						<b-field :label="$t('Mount Point Directory')" :message="$t('Target path in Files')">
							<b-input v-model="editDraft.new_mount_point" size="is-small" expanded></b-input>
						</b-field>
					</div>

					<!-- Switches -->
					<div class="switch-card mt-3">
						<div class="switch-card-row">
							<div class="switch-card-text">
								<span class="title-text">{{ $t('Mount automatically on server startup') }}</span>
								<span class="sub-text">{{ $t('Drive will be available every boot') }}</span>
							</div>
							<b-switch v-model="editDraft.mount_at_boot" size="is-small" type="is-primary"></b-switch>
						</div>

						<div class="switch-card-row">
							<div class="switch-card-text">
								<span class="title-text">{{ $t('Read-only mode') }}</span>
								<span class="sub-text">{{ $t('Protects all data from being modified') }}</span>
							</div>
							<b-switch v-model="editDraft.read_only" size="is-small" type="is-primary"></b-switch>
						</div>
					</div>

					<!-- Advanced -->
					<a class="manual-uuid-toggle mt-3" @click="showEditAdvanced = !showEditAdvanced">
						<i class="mdi" :class="showEditAdvanced ? 'mdi-chevron-up' : 'mdi-chevron-down'"></i>
						{{ $t('Advanced Mount Flags') }}
					</a>
					<div v-if="showEditAdvanced" class="advanced-box mt-2">
						<b-field :label="$t('Filesystem Type Override')">
							<b-input v-model="editDraft.fstype" size="is-small"></b-input>
						</b-field>
						<div class="switch-card-row p-0 mb-3">
							<div class="switch-card-text">
								<span class="title-text">{{ $t('Check filesystem at boot (fsck)') }}</span>
							</div>
							<b-switch v-model="editDraft.check_at_boot" size="is-small" type="is-primary"></b-switch>
						</div>
						<b-field :label="$t('Extra mount options')">
							<b-input v-model="editDraft.options" size="is-small"></b-input>
						</b-field>
					</div>

					<div v-if="formError" class="modal-error-alert mt-3">
						<i class="mdi mdi-alert-circle-outline mr-2"></i>
						<span>{{ formError }}</span>
					</div>
				</div>

				<div class="modal-card-footer">
					<b-button rounded size="is-small" @click="showEditModal = false">{{ $t('Cancel') }}</b-button>
					<b-button rounded size="is-small" type="is-primary" icon-left="check" :loading="submitting" @click="submitEdit">
						{{ $t('Save Changes') }}
					</b-button>
				</div>
			</div>
		</div>

		<p v-if="error" class="error-note">{{ error }}</p>
	</div>
</template>

<script>
import { formatSize } from '@/utils/formatSize'
import events from '@/events/events'

const emptyDraft = () => ({
	uuid: '',
	mount_point: '',
	fstype: '',
	options: 'defaults,noatime,nofail',
	read_only: false,
	mount_at_boot: true,
	check_at_boot: false
})

const PRESETS = [
	{
		id: 'general',
		label: 'General Storage',
		desc: 'Standard daily file storage with fast access',
		icon: 'harddisk',
		read_only: false,
		mount_at_boot: true,
		check_at_boot: false,
		options: 'defaults,noatime,nofail'
	},
	{
		id: 'media',
		label: 'Media Library',
		desc: 'Optimized for Plex, Jellyfin, and video streaming',
		icon: 'movie-open-outline',
		read_only: false,
		mount_at_boot: true,
		check_at_boot: false,
		options: 'defaults,noatime,nofail'
	},
	{
		id: 'fast_ssd',
		label: 'Fast SSD / NVMe',
		desc: 'Discard trim enabled for solid state disks',
		icon: 'flash-outline',
		read_only: false,
		mount_at_boot: true,
		check_at_boot: false,
		options: 'defaults,noatime,nodiratime,discard,nofail'
	},
	{
		id: 'archive',
		label: 'Read-Only Archive',
		desc: 'Safe write-protection for sensitive archives & backups',
		icon: 'lock-outline',
		read_only: true,
		mount_at_boot: true,
		check_at_boot: false,
		options: 'defaults,ro,nofail'
	}
]

const WINDOWS_FSTYPES = ['ntfs', 'ntfs3', 'ntfs-3g', 'exfat', 'vfat', 'fat32']
const WINDOWS_PRESET = {
	id: 'windows',
	label: 'Windows NTFS / exFAT',
	desc: 'Auto-applies Linux user permissions (uid/gid 1000) for full read/write access',
	icon: 'microsoft-windows',
	read_only: false,
	mount_at_boot: true,
	check_at_boot: false,
	options: 'defaults,noatime,nofail,uid=1000,gid=1000,dmask=022,fmask=133'
}

export default {
	name: 'fstab-panel',
	data() {
		return {
			mounts: [],
			systemEntries: [],
			candidates: [],
			showAddModal: false,
			showEditModal: false,
			showAddAdvanced: false,
			showEditAdvanced: false,
			showManualDevice: false,
			showSystem: false,
			addDraft: emptyDraft(),
			editDraft: {},
			submitting: false,
			removing: null,
			adopting: null,
			actionBusy: null,
			loadingMounts: false,
			loadingCandidates: false,
			formError: '',
			error: ''
		}
	},
	computed: {
		visiblePresets() {
			return this.visiblePresetsFor(this.addDraft.fstype)
		}
	},
	created() {
		this.refresh()
	},
	methods: {
		formatSize,
		extraOptions(options) {
			return (options || '')
				.split(',')
				.map(t => t.trim())
				.filter(t => t && !['ro', 'rw', 'auto', 'noauto'].includes(t))
				.join(',')
		},
		visiblePresetsFor(fstype) {
			const ft = (fstype || '').toLowerCase()
			if (WINDOWS_FSTYPES.some(w => ft.includes(w))) {
				return [WINDOWS_PRESET, ...PRESETS]
			}
			return [...PRESETS, WINDOWS_PRESET]
		},
		driveIcon(fstype) {
			const ft = (fstype || '').toLowerCase()
			if (WINDOWS_FSTYPES.some(w => ft.includes(w))) return 'mdi-microsoft-windows'
			if (['ext2', 'ext3', 'ext4', 'xfs', 'btrfs', 'f2fs'].includes(ft)) return 'mdi-linux'
			return 'mdi-harddisk'
		},
		getDriveIconClass(fstype) {
			const ft = (fstype || '').toLowerCase()
			if (WINDOWS_FSTYPES.some(w => ft.includes(w))) return 'is-windows'
			if (['ext2', 'ext3', 'ext4', 'xfs', 'btrfs', 'f2fs'].includes(ft)) return 'is-linux'
			return 'is-default'
		},
		isSystemProtected(mountPoint) {
			const protectedPaths = ['/', '/boot', '/boot/efi', '/swap', 'none', '[SWAP]']
			return protectedPaths.includes(mountPoint) || mountPoint.startsWith('/boot/')
		},
		applyPreset(draft, preset) {
			draft.read_only = preset.read_only
			draft.mount_at_boot = preset.mount_at_boot
			draft.check_at_boot = preset.check_at_boot
			draft.options = preset.options
		},
		presetMatches(draft, preset) {
			return (
				draft.read_only === preset.read_only &&
				draft.mount_at_boot === preset.mount_at_boot &&
				draft.check_at_boot === preset.check_at_boot &&
				draft.options === preset.options
			)
		},
		getQuickPaths(draft) {
			const label = (draft.uuid || 'drive').replace(/[^a-zA-Z0-9_-]/g, '_').slice(0, 12)
			return [`/DATA/${label}`, '/DATA/storage', '/DATA/media', '/DATA/backup', '/DATA/tank']
		},
		copyToClipboard(text) {
			if (navigator.clipboard && navigator.clipboard.writeText) {
				navigator.clipboard.writeText(text).then(() => {
					this.$buefy.toast.open({ message: this.$t('Path copied to clipboard'), type: 'is-info', duration: 1500 })
				})
			}
		},
		// A managed entry whose UUID no longer matches any currently-attached drive
		// (reformatted, replaced, or unplugged) looks identical to a plain "unmounted"
		// entry unless called out separately - and "Mount" on it will always fail.
		isDeviceMissing(m) {
			return !m.mounted && !m.drive_path
		},
		refresh() {
			this.error = ''
			this.loadingMounts = true
			this.$api.fstab.list().then(res => {
				if (res.data.success === 200) {
					this.mounts = (res.data.data && res.data.data.managed) || []
					this.systemEntries = (res.data.data && res.data.data.system) || []
				}
			}).catch(() => {
				this.error = this.$t('Failed to load persistent mounts')
			}).finally(() => {
				this.loadingMounts = false
			})
			this.refreshCandidates()
			// Files' MountList lives in a separate component tree with no other
			// way to learn a drive was added/edited/mounted/unmounted/removed here.
			this.$EventBus.$emit(events.RELOAD_MOUNT_LIST)
		},
		refreshCandidates() {
			this.loadingCandidates = true
			this.$api.fstab.candidates().then(res => {
				if (res.data.success === 200) {
					this.candidates = res.data.data || []
				}
			}).finally(() => {
				this.loadingCandidates = false
			})
		},
		openAddWizard() {
			this.addDraft = emptyDraft()
			this.formError = ''
			this.showAddAdvanced = false
			this.showManualDevice = false
			this.showAddModal = true
			this.refreshCandidates()
		},
		selectCandidate(c) {
			this.addDraft.uuid = c.uuid || c.path
			this.addDraft.fstype = c.fstype
			const safeName = (c.label || c.path.split('/').pop() || c.uuid.slice(0, 8)).replace(/[^a-zA-Z0-9_-]/g, '_')
			this.addDraft.mount_point = c.mount_point || `/DATA/${safeName}`
			const presets = this.visiblePresetsFor(c.fstype)
			this.applyPreset(this.addDraft, presets[0])
		},
		submitAdd() {
			this.formError = ''
			this.submitting = true
			this.$api.fstab.create(this.addDraft).then(res => {
				if (res.data.success !== 200) {
					this.formError = res.data.message
					return
				}
				this.showAddModal = false
				this.refresh()
				this.$buefy.toast.open({ message: this.$t('Drive mounted and saved to fstab successfully'), type: 'is-success' })
			}).catch(e => {
				this.formError = (e.response && e.response.data && e.response.data.message) || this.$t('Failed to mount drive')
			}).finally(() => {
				this.submitting = false
			})
		},
		openEditModal(m) {
			this.formError = ''
			this.showEditAdvanced = false
			this.editDraft = {
				mount_point: m.mount_point,
				new_mount_point: m.mount_point,
				drive_label: m.drive_label,
				source: m.source,
				fstype: m.fstype,
				options: this.extraOptions(m.options),
				read_only: m.read_only,
				mount_at_boot: m.mount_at_boot,
				check_at_boot: m.check_at_boot
			}
			this.showEditModal = true
		},
		submitEdit() {
			this.formError = ''
			this.submitting = true
			this.$api.fstab.update(this.editDraft).then(res => {
				if (res.data.success !== 200) {
					this.formError = res.data.message
					return
				}
				this.showEditModal = false
				this.refresh()
				this.$buefy.toast.open({ message: this.$t('Mount settings updated'), type: 'is-success' })
			}).catch(e => {
				this.formError = (e.response && e.response.data && e.response.data.message) || this.$t('Failed to update mount')
			}).finally(() => {
				this.submitting = false
			})
		},
		toggleEnabled(m, enabled) {
			const previous = m.enabled
			m.enabled = enabled
			this.$api.fstab.setEnabled(m.mount_point, enabled).then(() => {
				this.refresh()
				this.$buefy.toast.open({
					message: enabled ? this.$t('Will mount at system startup') : this.$t('Disabled at startup (drive left untouched)'),
					type: 'is-success'
				})
			}).catch(() => {
				m.enabled = previous
				this.error = this.$t('Failed to update startup configuration')
			})
		},
		toggleMountState(m, shouldMount) {
			this.actionBusy = m.mount_point
			const promise = shouldMount ? this.$api.fstab.mount(m.mount_point) : this.$api.fstab.umount(m.mount_point)
			promise.then(() => {
				this.refresh()
				this.$buefy.toast.open({
					message: shouldMount ? this.$t('Drive mounted successfully') : this.$t('Drive unmounted'),
					type: 'is-success'
				})
			}).catch(e => {
				const msg = (e.response && e.response.data && e.response.data.message) || (shouldMount ? this.$t('Failed to mount drive') : this.$t('Failed to unmount drive'))
				this.$buefy.toast.open({ message: msg, type: 'is-danger' })
			}).finally(() => {
				this.actionBusy = null
			})
		},
		adoptEntry(e) {
			this.adopting = e.mount_point
			this.$api.fstab.adopt(e.mount_point).then(() => {
				this.refresh()
				this.$buefy.toast.open({ message: this.$t('Drive is now managed by NivaroOS WebUI'), type: 'is-success' })
			}).catch(err => {
				const msg = (err.response && err.response.data && err.response.data.message) || this.$t('Failed to adopt drive')
				this.$buefy.toast.open({ message: msg, type: 'is-danger' })
			}).finally(() => {
				this.adopting = null
			})
		},
		confirmRemove(m) {
			this.$buefy.dialog.confirm({
				container: '#window-settings',
				title: this.$t('Remove from /etc/fstab?'),
				message: this.$t(
					'<b>{name}</b> ({mount}) will be removed from startup mounts.<br><br><b>Note:</b> Your drive and all its contents stay 100% safe and are NOT erased. You can mount it again anytime.',
					{ name: m.drive_label || m.drive_path || m.mount_point, mount: m.mount_point }
				),
				type: 'is-danger',
				confirmText: this.$t('Remove Mount'),
				cancelText: this.$t('Cancel'),
				onConfirm: () => {
					this.removing = m.mount_point
					this.$api.fstab.remove(m.mount_point).then(() => {
						this.refresh()
						this.$buefy.toast.open({ message: this.$t('Drive removed from fstab'), type: 'is-success' })
					}).catch(e => {
						this.error = (e.response && e.response.data && e.response.data.message) || this.$t('Failed to remove drive')
					}).finally(() => {
						this.removing = null
					})
				}
			})
		}
	}
}
</script>

<style lang="scss" scoped>
.fstab-panel {
	margin-top: 1.5rem;
	position: relative;
}

.fstab-header {
	display: flex;
	align-items: flex-start;
	justify-content: space-between;
	gap: 1rem;
	margin-bottom: 0.85rem;
}

.header-left {
	flex: 1;
	min-width: 0;
}

.header-title-row {
	display: flex;
	align-items: center;
	gap: 0.75rem;
	flex-wrap: wrap;
}

.status-counters {
	display: flex;
	align-items: center;
	gap: 0.4rem;
}

.count-pill {
	display: inline-flex;
	align-items: center;
	padding: 0.15rem 0.55rem;
	font-size: 0.7rem;
	font-weight: 600;
	border-radius: 20px;

	&.is-primary {
		background: rgba(50, 115, 220, 0.12);
		color: #3273dc;
	}

	&.is-info {
		background: rgba(35, 209, 96, 0.12);
		color: #23d160;
	}
}

.header-actions {
	display: flex;
	align-items: center;
	gap: 0.5rem;
	flex-shrink: 0;
}

.add-mount-btn {
	font-weight: 600;
	box-shadow: 0 2px 8px rgba(50, 115, 220, 0.25);
}

.hint {
	font-size: 0.78rem;
	opacity: 0.65;
	margin-top: 0.25rem;
	line-height: 1.35;
}

.icon-button {
	flex-shrink: 0;
	border: none;
	background: rgba(0, 0, 0, 0.05);
	width: 1.85rem;
	height: 1.85rem;
	border-radius: 50%;
	display: flex;
	align-items: center;
	justify-content: center;
	cursor: pointer;
	color: rgba(44, 62, 80, 0.65);
	transition: all 0.15s ease;

	&:hover:not(:disabled) {
		background: rgba(0, 0, 0, 0.1);
		color: #1e293b;
	}

	&:disabled {
		opacity: 0.4;
		cursor: default;
	}
}

.mdi-spin {
	animation: fstab-spin 1s linear infinite;
}

@keyframes fstab-spin {
	from { transform: rotate(0deg); }
	to { transform: rotate(360deg); }
}

/* Mounts Cards Grid */
.mounts-container {
	position: relative;
	min-height: 4rem;
}

.mount-cards-grid {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
	gap: 0.85rem;
}

.mount-card {
	background: #fff;
	border: 1px solid rgba(0, 0, 0, 0.08);
	border-radius: 12px;
	padding: 0.85rem;
	display: flex;
	flex-direction: column;
	gap: 0.65rem;
	box-shadow: 0 1px 4px rgba(0, 0, 0, 0.03);
	transition: border-color 0.15s ease, box-shadow 0.15s ease;

	&:hover {
		border-color: rgba(50, 115, 220, 0.3);
		box-shadow: 0 4px 12px rgba(0, 0, 0, 0.06);
	}

	&.is-unmounted {
		border-style: dashed;
		background: rgba(255, 255, 255, 0.7);
	}

	&.is-missing {
		border-style: dashed;
		border-color: rgba(239, 68, 68, 0.35);
		background: rgba(239, 68, 68, 0.03);
	}
}

.mount-card-header {
	display: flex;
	align-items: center;
	gap: 0.65rem;
}

.drive-icon-wrap {
	width: 2.3rem;
	height: 2.3rem;
	border-radius: 10px;
	display: flex;
	align-items: center;
	justify-content: center;
	font-size: 1.35rem;
	flex-shrink: 0;

	&.is-windows {
		background: rgba(0, 120, 215, 0.1);
		color: #0078d7;
	}

	&.is-linux {
		background: rgba(234, 179, 8, 0.12);
		color: #d97706;
	}

	&.is-default {
		background: rgba(100, 116, 139, 0.1);
		color: #475569;
	}
}

.drive-titles {
	flex: 1;
	min-width: 0;
}

.drive-name-row {
	display: flex;
	align-items: center;
	gap: 0.4rem;
}

.drive-main-name {
	font-size: 0.88rem;
	font-weight: 600;
	color: #1e293b;
	max-width: 11rem;
}

.fs-badge {
	font-size: 0.65rem;
	font-weight: 700;
	padding: 0.1rem 0.4rem;
	border-radius: 4px;
	background: rgba(0, 0, 0, 0.06);
	color: rgba(30, 41, 59, 0.8);
	letter-spacing: 0.02em;
}

.drive-source-meta {
	font-size: 0.7rem;
	color: rgba(100, 116, 139, 0.8);
	display: flex;
	align-items: center;
	margin-top: 0.1rem;
}

.mount-status-badges {
	flex-shrink: 0;
}

.status-pill {
	display: inline-flex;
	align-items: center;
	padding: 0.15rem 0.5rem;
	border-radius: 12px;
	font-size: 0.7rem;
	font-weight: 500;

	&.is-mounted {
		background: rgba(35, 209, 96, 0.1);
		color: #23d160;
	}

	&.is-unmounted {
		background: rgba(100, 116, 139, 0.1);
		color: #64748b;
	}

	&.is-missing {
		background: rgba(239, 68, 68, 0.1);
		color: #ef4444;
	}
}

.status-dot {
	width: 0.4rem;
	height: 0.4rem;
	border-radius: 50%;
	background: #94a3b8;
	margin-right: 0.35rem;

	&.is-good {
		background: #23d160;
	}

	&.is-bad {
		background: #ef4444;
	}
}

/* Mount Card Body */
.mount-card-body {
	display: flex;
	flex-direction: column;
	gap: 0.45rem;
}

.mount-target-box {
	display: flex;
	align-items: center;
	gap: 0.4rem;
	background: rgba(241, 245, 249, 0.8);
	border: 1px solid rgba(226, 232, 240, 0.8);
	border-radius: 8px;
	padding: 0.35rem 0.55rem;
	cursor: pointer;
	transition: background 0.15s ease;

	&:hover {
		background: rgba(226, 232, 240, 0.9);

		.copy-btn {
			opacity: 1;
		}
	}

	.target-icon {
		font-size: 1rem;
		color: #3b82f6;
		flex-shrink: 0;
	}

	.target-path {
		font-family: monospace;
		font-size: 0.78rem;
		font-weight: 600;
		color: #1e293b;
		flex: 1;
	}

	.copy-btn {
		border: none;
		background: none;
		opacity: 0.5;
		cursor: pointer;
		font-size: 0.85rem;
		color: #64748b;
		padding: 0;
		display: flex;
		align-items: center;
	}
}

.mount-specs-row {
	display: flex;
	align-items: center;
	gap: 0.6rem;
	flex-wrap: wrap;
	font-size: 0.72rem;
	color: #64748b;
}

.spec-item {
	display: inline-flex;
	align-items: center;

	&.is-warning {
		color: #f59e0b;
		font-weight: 500;
	}

	&.is-success {
		color: #10b981;
	}

	&.is-muted {
		color: #94a3b8;
	}
}

.options-preview {
	font-size: 0.68rem;
	code {
		background: rgba(0, 0, 0, 0.04);
		padding: 0.1rem 0.35rem;
		border-radius: 4px;
		color: #64748b;
	}
}

/* Mount Card Footer */
.mount-card-footer {
	display: flex;
	align-items: center;
	justify-content: space-between;
	border-top: 1px solid rgba(0, 0, 0, 0.05);
	padding-top: 0.55rem;
	margin-top: 0.1rem;
	gap: 0.5rem;
}

.boot-toggle-wrap {
	display: flex;
	align-items: center;
	gap: 0.4rem;
}

.boot-toggle-label {
	font-size: 0.72rem;
	color: #64748b;
}

.action-buttons-wrap {
	display: flex;
	align-items: center;
	gap: 0.35rem;
}

.action-btn {
	border: 1px solid rgba(0, 0, 0, 0.08);
	background: #fff;
	border-radius: 6px;
	padding: 0.25rem 0.5rem;
	font-size: 0.72rem;
	font-weight: 500;
	color: #334155;
	display: inline-flex;
	align-items: center;
	gap: 0.25rem;
	cursor: pointer;
	transition: all 0.12s ease;

	i {
		font-size: 0.88rem;
	}

	&:hover:not(:disabled) {
		background: rgba(0, 0, 0, 0.04);
		color: #0f172a;
	}

	&:disabled {
		opacity: 0.5;
		cursor: default;
	}

	&.is-success-light {
		background: rgba(35, 209, 96, 0.08);
		border-color: rgba(35, 209, 96, 0.25);
		color: #16a34a;

		&:hover:not(:disabled) {
			background: rgba(35, 209, 96, 0.16);
		}
	}

	&.is-warning-light {
		background: rgba(245, 158, 11, 0.08);
		border-color: rgba(245, 158, 11, 0.25);
		color: #d97706;

		&:hover:not(:disabled) {
			background: rgba(245, 158, 11, 0.16);
		}
	}

	&.is-danger-light {
		background: rgba(239, 68, 68, 0.06);
		border-color: rgba(239, 68, 68, 0.2);
		color: #dc2626;

		&:hover:not(:disabled) {
			background: rgba(239, 68, 68, 0.14);
		}
	}
}

/* Empty State Card */
.fstab-empty-card {
	background: #fff;
	border: 1px dashed rgba(0, 0, 0, 0.12);
	border-radius: 12px;
	padding: 2.2rem 1.5rem;
	text-align: center;
	display: flex;
	flex-direction: column;
	align-items: center;
}

.empty-icon-circle {
	width: 3.5rem;
	height: 3.5rem;
	border-radius: 50%;
	background: rgba(50, 115, 220, 0.08);
	color: #3273dc;
	display: flex;
	align-items: center;
	justify-content: center;
	font-size: 1.8rem;
	margin-bottom: 0.75rem;
}

.empty-title {
	font-size: 0.95rem;
	font-weight: 600;
	color: #1e293b;
	margin-bottom: 0.3rem;
}

.empty-desc {
	font-size: 0.78rem;
	color: #64748b;
	max-width: 26rem;
	margin-bottom: 0.85rem;
	line-height: 1.4;
}

/* System Section */
.system-section {
	margin-top: 1.25rem;
}

.system-entries-toggle {
	display: inline-flex;
	align-items: center;
	gap: 0.35rem;
	border: none;
	background: none;
	color: rgba(71, 85, 105, 0.85);
	font-size: 0.8rem;
	font-weight: 600;
	cursor: pointer;
	padding: 0.35rem 0;

	&:hover {
		color: #0f172a;
	}
}

.system-card {
	background: #fff;
	border: 1px solid rgba(0, 0, 0, 0.06);
	border-radius: 10px;
	padding: 0.85rem;
}

.system-desc {
	font-size: 0.75rem;
	color: #64748b;
	margin-bottom: 0.65rem;
}

.system-table-wrap {
	display: flex;
	flex-direction: column;
	gap: 0.4rem;
}

.system-row {
	display: flex;
	align-items: center;
	gap: 0.65rem;
	padding: 0.45rem 0.65rem;
	background: rgba(248, 250, 252, 0.8);
	border-radius: 8px;
	font-size: 0.78rem;
}

.system-row-icon {
	font-size: 1.1rem;
	color: #64748b;
	flex-shrink: 0;
}

.system-row-info {
	flex: 1;
	min-width: 0;
}

.system-mount-title {
	display: flex;
	align-items: center;
	font-weight: 600;
	color: #1e293b;
}

.protected-pill {
	font-size: 0.65rem;
	font-weight: 600;
	background: rgba(100, 116, 139, 0.12);
	color: #475569;
	padding: 0.1rem 0.4rem;
	border-radius: 4px;
}

.system-meta {
	font-size: 0.7rem;
	color: #64748b;
	margin-top: 0.1rem;
}

.system-row-actions {
	flex-shrink: 0;
}

/* In-Window Modal Overlay */
.in-window-modal-backdrop {
	position: fixed;
	top: 0;
	left: 0;
	right: 0;
	bottom: 0;
	background: rgba(15, 23, 42, 0.45);
	backdrop-filter: blur(4px);
	display: flex;
	align-items: center;
	justify-content: center;
	z-index: 1050;
	padding: 1rem;
}

.in-window-modal-card {
	background: #fff;
	border-radius: 16px;
	width: 100%;
	max-width: 580px;
	max-height: 85vh;
	display: flex;
	flex-direction: column;
	box-shadow: 0 20px 40px rgba(0, 0, 0, 0.18);
	overflow: hidden;
}

.modal-card-header {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: 1rem 1.25rem 0.75rem;
	border-bottom: 1px solid rgba(0, 0, 0, 0.06);
}

.modal-card-title {
	font-size: 1.05rem;
	font-weight: 700;
	color: #0f172a;
	margin: 0;
}

.modal-subtitle {
	font-size: 0.75rem;
	color: #64748b;
	margin-top: 0.15rem;
}

.modal-close-btn {
	border: none;
	background: rgba(0, 0, 0, 0.05);
	width: 1.8rem;
	height: 1.8rem;
	border-radius: 50%;
	display: flex;
	align-items: center;
	justify-content: center;
	cursor: pointer;
	color: #64748b;

	&:hover {
		background: rgba(0, 0, 0, 0.1);
		color: #0f172a;
	}
}

.modal-card-body {
	padding: 1rem 1.25rem;
	overflow-y: auto;
	flex: 1;
}

.wizard-section {
	margin-bottom: 0.5rem;
}

.wizard-label {
	display: flex;
	align-items: center;
	gap: 0.45rem;
	font-size: 0.82rem;
	font-weight: 700;
	color: #1e293b;
	margin-bottom: 0.45rem;
}

.step-num {
	width: 1.3rem;
	height: 1.3rem;
	border-radius: 50%;
	background: #3273dc;
	color: #fff;
	font-size: 0.7rem;
	font-weight: 700;
	display: inline-flex;
	align-items: center;
	justify-content: center;
}

.wizard-subhint {
	font-size: 0.73rem;
	color: #64748b;
	margin-bottom: 0.65rem;
}

/* Candidates Grid */
.candidate-grid {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(230px, 1fr));
	gap: 0.5rem;
}

.candidate-card {
	display: flex;
	align-items: center;
	gap: 0.6rem;
	border: 1px solid rgba(0, 0, 0, 0.08);
	background: #fff;
	border-radius: 10px;
	padding: 0.55rem 0.75rem;
	cursor: pointer;
	position: relative;
	transition: all 0.12s ease;

	&:hover {
		border-color: #3273dc;
		background: rgba(50, 115, 220, 0.03);
	}

	&.active {
		border-color: #3273dc;
		background: rgba(50, 115, 220, 0.08);
		box-shadow: 0 0 0 1px #3273dc;
	}
}

.candidate-icon {
	width: 2rem;
	height: 2rem;
	border-radius: 8px;
	display: flex;
	align-items: center;
	justify-content: center;
	font-size: 1.2rem;
	flex-shrink: 0;

	&.is-windows {
		background: rgba(0, 120, 215, 0.1);
		color: #0078d7;
	}

	&.is-linux {
		background: rgba(234, 179, 8, 0.12);
		color: #d97706;
	}

	&.is-default {
		background: rgba(100, 116, 139, 0.1);
		color: #475569;
	}
}

.candidate-details {
	flex: 1;
	min-width: 0;
}

.candidate-name {
	font-size: 0.8rem;
	font-weight: 600;
	color: #1e293b;
}

.candidate-meta {
	font-size: 0.68rem;
	color: #64748b;
}

.fs-text {
	text-transform: uppercase;
	font-weight: 600;
}

.check-mark {
	position: absolute;
	top: 0.4rem;
	right: 0.4rem;
	width: 1rem;
	height: 1rem;
	border-radius: 50%;
	background: #3273dc;
	color: #fff;
	font-size: 0.65rem;
	display: flex;
	align-items: center;
	justify-content: center;
}

.empty-candidate-box {
	background: rgba(241, 245, 249, 0.6);
	border-radius: 8px;
	padding: 0.75rem;
	text-align: center;
}

.manual-uuid-toggle {
	display: inline-flex;
	align-items: center;
	gap: 0.25rem;
	font-size: 0.74rem;
	color: #3273dc;
	cursor: pointer;
	font-weight: 500;
}

.manual-device-box {
	background: rgba(248, 250, 252, 0.8);
	border: 1px solid rgba(0, 0, 0, 0.06);
	border-radius: 8px;
	padding: 0.65rem;
}

/* Preset Cards Grid */
.preset-cards-grid {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
	gap: 0.5rem;
}

.preset-card {
	border: 1px solid rgba(0, 0, 0, 0.08);
	background: #fff;
	border-radius: 10px;
	padding: 0.6rem 0.75rem;
	display: flex;
	align-items: flex-start;
	gap: 0.55rem;
	cursor: pointer;
	position: relative;
	transition: all 0.12s ease;

	&:hover {
		border-color: #3273dc;
		background: rgba(50, 115, 220, 0.02);
	}

	&.active {
		border-color: #3273dc;
		background: rgba(50, 115, 220, 0.08);
		box-shadow: 0 0 0 1px #3273dc;
	}
}

.preset-icon-wrap {
	font-size: 1.15rem;
	color: #3273dc;
	flex-shrink: 0;
	margin-top: 0.05rem;
}

.preset-info {
	flex: 1;
	display: flex;
	flex-direction: column;
	min-width: 0;
}

.preset-title {
	font-size: 0.8rem;
	font-weight: 600;
	color: #1e293b;
}

.preset-desc {
	font-size: 0.68rem;
	color: #64748b;
	line-height: 1.25;
	margin-top: 0.15rem;
}

.preset-selected-dot {
	width: 1rem;
	height: 1rem;
	border-radius: 50%;
	background: #3273dc;
	color: #fff;
	font-size: 0.65rem;
	display: flex;
	align-items: center;
	justify-content: center;
	flex-shrink: 0;
}

/* Quick Path Pills */
.quick-path-pills {
	display: flex;
	align-items: center;
	gap: 0.35rem;
	flex-wrap: wrap;
	margin-top: 0.4rem;
}

.quick-title {
	font-size: 0.7rem;
	color: #64748b;
}

.path-pill {
	border: 1px solid rgba(0, 0, 0, 0.08);
	background: rgba(0, 0, 0, 0.03);
	border-radius: 12px;
	padding: 0.15rem 0.5rem;
	font-size: 0.68rem;
	font-family: monospace;
	cursor: pointer;
	color: #334155;

	&:hover {
		border-color: #3273dc;
		color: #3273dc;
		background: rgba(50, 115, 220, 0.06);
	}
}

/* Switch Card */
.switch-card {
	background: rgba(248, 250, 252, 0.8);
	border: 1px solid rgba(0, 0, 0, 0.06);
	border-radius: 10px;
	padding: 0.5rem 0.85rem;
	display: flex;
	flex-direction: column;
	gap: 0.5rem;
}

.switch-card-row {
	display: flex;
	align-items: center;
	justify-content: space-between;
	padding: 0.25rem 0;
}

.switch-card-text {
	display: flex;
	flex-direction: column;
}

.title-text {
	font-size: 0.78rem;
	font-weight: 600;
	color: #1e293b;
}

.sub-text {
	font-size: 0.68rem;
	color: #64748b;
}

.advanced-box {
	background: rgba(248, 250, 252, 0.8);
	border: 1px solid rgba(0, 0, 0, 0.06);
	border-radius: 8px;
	padding: 0.75rem;
}

.modal-error-alert {
	display: flex;
	align-items: center;
	background: rgba(239, 68, 68, 0.1);
	border: 1px solid rgba(239, 68, 68, 0.25);
	color: #dc2626;
	padding: 0.5rem 0.75rem;
	border-radius: 8px;
	font-size: 0.75rem;
}

.modal-card-footer {
	display: flex;
	align-items: center;
	justify-content: flex-end;
	gap: 0.5rem;
	padding: 0.75rem 1.25rem;
	border-top: 1px solid rgba(0, 0, 0, 0.06);
	background: #f8fafc;
}

.error-note {
	padding: 0.5rem 0;
	color: #ef4444;
	font-size: 0.75rem;
}
</style>
