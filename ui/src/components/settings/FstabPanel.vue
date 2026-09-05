<template>
	<div class="fstab-panel">
		<div class="panel-header">
			<div class="header-text">
				<h3 class="setting-card-title">{{ $t('Persistent Mounts (fstab)') }}</h3>
				<p class="hint">{{ $t('Add an already-formatted drive as a real /etc/fstab entry, so it mounts automatically at boot - the same result as editing fstab by hand, without the editor.') }}</p>
			</div>
			<div class="header-actions">
				<button class="icon-button" type="button" :title="$t('Refresh')" :disabled="loadingCandidates" @click="refresh">
					<i class="mdi mdi-refresh" :class="{ 'mdi-spin': loadingCandidates }"></i>
				</button>
				<button class="add-button" type="button" :title="$t('Add drive to fstab')" @click="toggleAddForm">
					<i class="mdi" :class="showAddForm ? 'mdi-close' : 'mdi-plus'"></i>
				</button>
			</div>
		</div>

		<div v-if="showAddForm" class="fstab-form is-block">
			<p class="field-help">{{ $t('Choose a drive') }}</p>
			<div class="drive-grid">
				<button
					v-for="c in candidates"
					:key="c.uuid"
					type="button"
					class="drive-tile"
					:class="{ active: addDraft.uuid === c.uuid }"
					@click="selectCandidate(c)"
				>
					<i class="mdi" :class="driveIcon(c.fstype)"></i>
					<div class="drive-tile-info">
						<span class="drive-tile-name one-line">{{ c.label || c.path }}</span>
						<span class="drive-tile-meta">{{ formatSize(c.size) }} &middot; {{ c.fstype }}</span>
					</div>
				</button>
			</div>
			<p v-if="!loadingCandidates && !candidates.length" class="hint">{{ $t('No addable drives found - format one first under Available Disks above.') }}</p>

			<template v-if="addDraft.uuid">
				<p class="field-help mt-3">{{ $t("What's this drive for? (optional - fills in sensible defaults, everything stays editable below)") }}</p>
				<div class="preset-grid">
					<button v-for="p in visiblePresets" :key="p.id" type="button" class="preset-tile" @click="applyPreset(addDraft, p)">
						<i class="mdi" :class="`mdi-${p.icon}`"></i>
						<span>{{ $t(p.label) }}</span>
					</button>
				</div>

				<b-field :label="$t('Mount point')">
					<b-input v-model="addDraft.mount_point" placeholder="/DATA/..." size="is-small" expanded></b-input>
				</b-field>
				<b-field :label="$t('Filesystem')">
					<b-input v-model="addDraft.fstype" size="is-small" expanded></b-input>
				</b-field>

				<div class="fstab-switch-row">
					<span>{{ $t('Mount automatically at boot') }}</span>
					<b-switch v-model="addDraft.mount_at_boot" size="is-small" type="is-dark"></b-switch>
				</div>
				<div class="fstab-switch-row">
					<span>{{ $t('Read-only') }}</span>
					<b-switch v-model="addDraft.read_only" size="is-small" type="is-dark"></b-switch>
				</div>
				<div class="fstab-switch-row">
					<span>{{ $t('Check filesystem at boot') }}</span>
					<b-switch v-model="addDraft.check_at_boot" size="is-small" type="is-dark"></b-switch>
				</div>

				<b-field :label="$t('Advanced options')">
					<b-input v-model="addDraft.options" :placeholder="$t('e.g. noatime,uid=1000 (optional)')" size="is-small" expanded></b-input>
				</b-field>

				<div class="form-actions">
					<b-button rounded size="is-small" type="is-dark" :loading="submitting" @click="submitAdd">{{ $t('Add') }}</b-button>
					<b-button rounded size="is-small" @click="showAddForm = false">{{ $t('Cancel') }}</b-button>
				</div>
			</template>
			<p v-if="formError" class="error-note">{{ formError }}</p>
		</div>

		<div class="setting-card">
			<b-loading v-model="loadingMounts" :is-full-page="false"></b-loading>
			<div v-for="m in mounts" :key="m.mount_point">
				<div class="setting-row">
					<i class="row-icon mdi" :class="driveIcon(m.fstype)"></i>
					<div class="row-label">
						<div class="setting-title">{{ m.drive_label || m.uuid }} &rarr; {{ m.mount_point }}</div>
						<div class="setting-desc">
							{{ m.fstype }} &middot; {{ formatSize(m.size) }}
							<span class="status-dot" :class="{ 'is-good': m.mounted }"></span>{{ m.mounted ? $t('Mounted') : $t('Not mounted') }}
							<span v-if="!m.enabled" class="setting-chip">{{ $t('Disabled at boot') }}</span>
							<span v-if="m.read_only" class="setting-chip">{{ $t('Read-only') }}</span>
						</div>
					</div>
					<div class="row-control">
						<b-switch :value="m.enabled" size="is-small" class="is-flex-direction-row-reverse mr-2" type="is-dark"
							:title="$t('Mount at boot')" @input="toggleEnabled(m, $event)"></b-switch>
						<button class="icon-button mr-2" type="button" :title="$t('Edit')" @click="toggleEdit(m)">
							<i class="mdi mdi-pencil-outline"></i>
						</button>
						<b-button rounded size="is-small" type="is-danger" outlined :loading="removing === m.mount_point" @click="confirmRemove(m)">
							{{ $t('Remove') }}
						</b-button>
					</div>
				</div>

				<div v-if="editingMountPoint === m.mount_point" class="fstab-form is-block full-width">
					<div class="preset-grid">
						<button v-for="p in visiblePresetsFor(m.fstype)" :key="p.id" type="button" class="preset-tile" @click="applyPreset(editDraft, p)">
							<i class="mdi" :class="`mdi-${p.icon}`"></i>
							<span>{{ $t(p.label) }}</span>
						</button>
					</div>
					<b-field :label="$t('Mount point')">
						<b-input v-model="editDraft.new_mount_point" size="is-small" expanded></b-input>
					</b-field>
					<b-field :label="$t('Filesystem')">
						<b-input v-model="editDraft.fstype" size="is-small" expanded></b-input>
					</b-field>
					<div class="fstab-switch-row">
						<span>{{ $t('Mount automatically at boot') }}</span>
						<b-switch v-model="editDraft.mount_at_boot" size="is-small" type="is-dark"></b-switch>
					</div>
					<div class="fstab-switch-row">
						<span>{{ $t('Read-only') }}</span>
						<b-switch v-model="editDraft.read_only" size="is-small" type="is-dark"></b-switch>
					</div>
					<div class="fstab-switch-row">
						<span>{{ $t('Check filesystem at boot') }}</span>
						<b-switch v-model="editDraft.check_at_boot" size="is-small" type="is-dark"></b-switch>
					</div>
					<b-field :label="$t('Advanced options')">
						<b-input v-model="editDraft.options" size="is-small" expanded></b-input>
					</b-field>
					<div class="form-actions">
						<b-button rounded size="is-small" type="is-dark" :loading="submitting" @click="submitEdit(m)">{{ $t('Save') }}</b-button>
						<b-button rounded size="is-small" @click="editingMountPoint = null">{{ $t('Cancel') }}</b-button>
					</div>
					<p v-if="formError" class="error-note">{{ formError }}</p>
				</div>
			</div>
			<div v-if="!loadingMounts && !mounts.length" class="account-empty">{{ $t('No drives added yet.') }}</div>
		</div>

		<button type="button" class="system-entries-toggle" @click="showSystem = !showSystem">
			<i class="mdi" :class="showSystem ? 'mdi-chevron-up' : 'mdi-chevron-down'"></i>
			{{ $t('System entries ({count})', { count: systemEntries.length }) }}
		</button>
		<div v-if="showSystem" class="setting-card">
			<p class="hint">{{ $t("These come from the base system or were added by hand - not managed here. Edit /etc/fstab directly if you need to change them.") }}</p>
			<div v-for="e in systemEntries" :key="e.mount_point" class="setting-row">
				<i class="row-icon mdi mdi-lock-outline"></i>
				<div class="row-label">
					<div class="setting-title">{{ e.mount_point }}</div>
					<div class="setting-desc">{{ e.source }} &middot; {{ e.fstype }} &middot; {{ e.options }}</div>
				</div>
			</div>
			<div v-if="!systemEntries.length" class="account-empty">{{ $t('No other fstab entries.') }}</div>
		</div>

		<p v-if="error" class="error-note">{{ error }}</p>
	</div>
</template>

<script>
import { formatSize } from '@/utils/formatSize'

const emptyDraft = () => ({
	uuid: '',
	mount_point: '',
	fstype: '',
	options: 'noatime',
	read_only: false,
	mount_at_boot: true,
	check_at_boot: false
})

const PRESETS = [
	{ id: 'general', label: 'General Storage', icon: 'harddisk', read_only: false, mount_at_boot: true, check_at_boot: false, options: 'noatime' },
	{ id: 'media', label: 'Media Library', icon: 'movie-open-outline', read_only: false, mount_at_boot: true, check_at_boot: false, options: 'noatime' },
	{ id: 'backup', label: 'Backup Target', icon: 'backup-restore', read_only: false, mount_at_boot: true, check_at_boot: true, options: 'noatime' },
	{ id: 'archive', label: 'Read-Only Archive', icon: 'lock-outline', read_only: true, mount_at_boot: true, check_at_boot: false, options: '' }
]

const WINDOWS_FSTYPES = ['ntfs', 'ntfs3', 'exfat', 'vfat']
const WINDOWS_PRESET = {
	id: 'windows', label: 'Windows Drive Permissions', icon: 'microsoft-windows',
	read_only: false, mount_at_boot: true, check_at_boot: false, options: 'noatime,uid=1000,gid=1000,dmask=022,fmask=133'
}

export default {
	name: 'fstab-panel',
	data() {
		return {
			mounts: [],
			systemEntries: [],
			candidates: [],
			showAddForm: false,
			showSystem: false,
			addDraft: emptyDraft(),
			editingMountPoint: null,
			editDraft: {},
			submitting: false,
			removing: null,
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
		visiblePresetsFor(fstype) {
			if (WINDOWS_FSTYPES.includes((fstype || '').toLowerCase())) return [...PRESETS, WINDOWS_PRESET]
			return PRESETS
		},
		driveIcon(fstype) {
			const ft = (fstype || '').toLowerCase()
			if (WINDOWS_FSTYPES.includes(ft)) return 'mdi-microsoft-windows'
			if (['ext2', 'ext3', 'ext4', 'xfs', 'btrfs', 'f2fs'].includes(ft)) return 'mdi-linux'
			return 'mdi-harddisk'
		},
		applyPreset(draft, preset) {
			draft.read_only = preset.read_only
			draft.mount_at_boot = preset.mount_at_boot
			draft.check_at_boot = preset.check_at_boot
			draft.options = preset.options
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
				this.error = this.$t('Failed to load fstab entries')
			}).finally(() => {
				this.loadingMounts = false
			})
			this.refreshCandidates()
		},
		refreshCandidates() {
			this.loadingCandidates = true
			this.$api.fstab.candidates().then(res => {
				if (res.data.success === 200) this.candidates = res.data.data || []
			}).finally(() => {
				this.loadingCandidates = false
			})
		},
		toggleAddForm() {
			this.showAddForm = !this.showAddForm
			this.formError = ''
			if (this.showAddForm) {
				this.addDraft = emptyDraft()
				this.refreshCandidates()
			}
		},
		selectCandidate(c) {
			this.addDraft.uuid = c.uuid
			this.addDraft.fstype = c.fstype
			if (!this.addDraft.mount_point) {
				const name = (c.label || c.uuid.slice(0, 8)).replace(/[^a-zA-Z0-9_-]/g, '_')
				this.addDraft.mount_point = `/DATA/${name}`
			}
			this.applyPreset(this.addDraft, this.visiblePresetsFor(c.fstype)[0])
		},
		submitAdd() {
			this.formError = ''
			this.submitting = true
			this.$api.fstab.create(this.addDraft).then(res => {
				if (res.data.success !== 200) {
					this.formError = res.data.message
					return
				}
				this.showAddForm = false
				this.refresh()
				this.$buefy.toast.open({ message: this.$t('Drive added and mounted'), type: 'is-success' })
			}).catch(e => {
				this.formError = (e.response && e.response.data && e.response.data.message) || this.$t('Failed to add mount')
			}).finally(() => {
				this.submitting = false
			})
		},
		toggleEdit(m) {
			if (this.editingMountPoint === m.mount_point) {
				this.editingMountPoint = null
				return
			}
			this.formError = ''
			this.editingMountPoint = m.mount_point
			this.editDraft = {
				mount_point: m.mount_point,
				new_mount_point: m.mount_point,
				fstype: m.fstype,
				options: '',
				read_only: m.read_only,
				mount_at_boot: m.mount_at_boot,
				check_at_boot: m.check_at_boot
			}
		},
		submitEdit(m) {
			this.formError = ''
			this.submitting = true
			this.$api.fstab.update({ mount_point: m.mount_point, ...this.editDraft }).then(res => {
				if (res.data.success !== 200) {
					this.formError = res.data.message
					return
				}
				this.editingMountPoint = null
				this.refresh()
				this.$buefy.toast.open({ message: this.$t('Changes saved'), type: 'is-success' })
			}).catch(e => {
				this.formError = (e.response && e.response.data && e.response.data.message) || this.$t('Failed to save changes')
			}).finally(() => {
				this.submitting = false
			})
		},
		toggleEnabled(m, enabled) {
			this.$api.fstab.setEnabled(m.mount_point, enabled).then(() => {
				this.refresh()
				this.$buefy.toast.open({
					message: enabled ? this.$t('Will mount at next boot') : this.$t('Disabled at boot - currently mounted drive left untouched'),
					type: 'is-success'
				})
			}).catch(() => {
				this.error = this.$t('Failed to update mount')
			})
		},
		confirmRemove(m) {
			this.$buefy.dialog.confirm({
				container: '#window-settings',
				title: this.$t('Remove mount'),
				message: this.$t('Unmount {mount} and remove it from fstab? The drive itself and its data are left untouched.', { mount: m.mount_point }),
				type: 'is-danger',
				confirmText: this.$t('Remove'),
				cancelText: this.$t('Cancel'),
				onConfirm: () => {
					this.removing = m.mount_point
					this.$api.fstab.remove(m.mount_point).then(() => {
						this.refresh()
						this.$buefy.toast.open({ message: this.$t('Mount removed'), type: 'is-success' })
					}).catch(e => {
						this.error = (e.response && e.response.data && e.response.data.message) || this.$t('Failed to remove mount')
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
.panel-header {
	display: flex;
	align-items: flex-start;
	justify-content: space-between;
	gap: 1rem;
}

.header-text {
	flex: 1;
}

.header-actions {
	display: flex;
	align-items: center;
	gap: 0.5rem;
	flex-shrink: 0;
}

.hint {
	font-size: 0.75rem;
	opacity: 0.6;
	margin-top: 0.15rem;
}

.field-help {
	font-size: 0.75rem;
	color: rgba(0, 0, 0, 0.55);
	margin-bottom: 0.35rem;
}

.add-button {
	flex-shrink: 0;
	width: 1.9rem;
	height: 1.9rem;
	border-radius: 50%;
	border: none;
	background: hsla(208, 100%, 50%, 1);
	color: #fff;
	display: flex;
	align-items: center;
	justify-content: center;
	cursor: pointer;

	&:hover {
		background: hsla(208, 100%, 44%, 1);
	}
}

.icon-button {
	flex-shrink: 0;
	border: none;
	background: rgba(0, 0, 0, 0.05);
	width: 1.7rem;
	height: 1.7rem;
	border-radius: 50%;
	display: flex;
	align-items: center;
	justify-content: center;
	cursor: pointer;
	color: rgba(44, 62, 80, 0.6);

	&:hover:not(:disabled) {
		background: rgba(0, 0, 0, 0.09);
		color: #1e293b;
	}

	&:disabled {
		opacity: 0.5;
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

.fstab-form.is-block {
	padding: 0.75rem 1.25rem 1rem;
	background: rgba(0, 0, 0, 0.02);
	border-radius: 8px;
	margin: 0.5rem 0 1rem;

	&.full-width {
		margin: 0 0 0.75rem 3.25rem;
		width: calc(100% - 3.25rem);
	}
}

.drive-grid,
.preset-grid {
	display: flex;
	flex-wrap: wrap;
	gap: 0.5rem;
	margin-bottom: 0.75rem;
}

.drive-tile {
	display: flex;
	align-items: center;
	gap: 0.6rem;
	border: 1px solid rgba(0, 0, 0, 0.08);
	background: #fff;
	border-radius: 8px;
	padding: 0.5rem 0.75rem;
	font-size: 0.8rem;
	cursor: pointer;
	min-width: 11rem;
	text-align: left;
	transition: background 0.12s ease, border-color 0.12s ease;

	i {
		font-size: 1.4rem;
		flex-shrink: 0;
		color: rgba(44, 62, 80, 0.6);
	}

	&:hover {
		background: rgba(0, 0, 0, 0.03);
	}

	&.active {
		border-color: #3273dc;
		background: rgba(50, 115, 220, 0.08);

		i {
			color: #3273dc;
		}
	}
}

.drive-tile-info {
	display: flex;
	flex-direction: column;
	min-width: 0;
}

.drive-tile-name {
	font-weight: 500;
	max-width: 10rem;
}

.drive-tile-meta {
	font-size: 0.7rem;
	opacity: 0.6;
}

.preset-tile {
	display: flex;
	align-items: center;
	gap: 0.4rem;
	border: 1px solid rgba(0, 0, 0, 0.08);
	background: #fff;
	border-radius: 8px;
	padding: 0.45rem 0.75rem;
	font-size: 0.8rem;
	cursor: pointer;
	transition: background 0.12s ease, border-color 0.12s ease;

	i {
		font-size: 1.05rem;
	}

	&:hover {
		border-color: #3273dc;
		background: rgba(50, 115, 220, 0.08);
		color: #3273dc;
	}
}

.fstab-switch-row {
	display: flex;
	align-items: center;
	justify-content: space-between;
	font-size: 0.8rem;
	padding: 0.35rem 0;
}

.form-actions {
	display: flex;
	justify-content: flex-end;
	gap: 0.5rem;
	margin-top: 0.5rem;
}

.status-dot {
	display: inline-block;
	width: 0.4rem;
	height: 0.4rem;
	border-radius: 50%;
	background: rgba(0, 0, 0, 0.3);
	margin-right: 0.3rem;

	&.is-good {
		background: #23d160;
	}
}

.system-entries-toggle {
	display: flex;
	align-items: center;
	gap: 0.35rem;
	border: none;
	background: none;
	color: rgba(44, 62, 80, 0.6);
	font-size: 0.75rem;
	cursor: pointer;
	padding: 0.5rem 0;

	&:hover {
		color: #1e293b;
	}
}

.error-note {
	padding: 0 1.25rem 0.75rem;
	color: #ef4444;
	font-size: 0.75rem;
}
</style>
