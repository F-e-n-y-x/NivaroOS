<template>
	<div class="fstab-panel">
		<div class="panel-header">
			<div class="header-text">
				<h3 class="setting-card-title">{{ $t('Persistent Mounts (fstab)') }}</h3>
				<p class="hint">{{ $t('Add an already-formatted drive as a real /etc/fstab entry, so it mounts automatically at boot - the same result as editing fstab by hand, without the editor.') }}</p>
			</div>
			<button class="add-button" type="button" :title="$t('Add drive to fstab')" @click="toggleAddForm">
				<b-icon :icon="showAddForm ? 'close-outline' : 'add-outline'" pack="casa" size="is-20"></b-icon>
			</button>
		</div>

		<form v-if="showAddForm" class="fstab-form" @submit.prevent="submitAdd">
			<div class="fstab-form-row">
				<label class="fstab-form-label">{{ $t('Drive') }}</label>
				<b-select v-model="addDraft.uuid" :placeholder="$t('Choose a drive')" size="is-small" expanded @input="onCandidateSelected">
					<option v-for="c in candidates" :key="c.uuid" :value="c.uuid">{{ candidateLabel(c) }}</option>
				</b-select>
			</div>
			<p v-if="!candidates.length" class="hint">{{ $t('No addable drives found - format one first under Available Disks above.') }}</p>

			<template v-if="addDraft.uuid">
				<div class="fstab-form-row">
					<label class="fstab-form-label">{{ $t('Mount point') }}</label>
					<b-input v-model="addDraft.mount_point" placeholder="/DATA/..." size="is-small" expanded></b-input>
				</div>
				<div class="fstab-form-row">
					<label class="fstab-form-label">{{ $t('Filesystem') }}</label>
					<b-input v-model="addDraft.fstype" size="is-small" expanded></b-input>
				</div>

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

				<div class="fstab-form-row">
					<label class="fstab-form-label">{{ $t('Advanced options') }}</label>
					<b-input v-model="addDraft.options" :placeholder="$t('e.g. noatime,uid=1000 (optional)')" size="is-small" expanded></b-input>
				</div>

				<div class="fstab-form-actions">
					<b-button rounded size="is-small" type="is-dark" native-type="submit" :loading="submitting">{{ $t('Add') }}</b-button>
				</div>
			</template>
			<p v-if="formError" class="error-note">{{ formError }}</p>
		</form>

		<div class="setting-card">
			<div v-for="m in mounts" :key="m.mount_point">
				<div class="setting-row">
					<b-icon class="row-icon" icon="storage-other" pack="casa" size="is-20"></b-icon>
					<div class="row-label">
						<div class="setting-title">{{ m.drive_label || m.uuid }} &rarr; {{ m.mount_point }}</div>
						<div class="setting-desc">
							{{ m.fstype }} &middot; {{ formatSize(m.size) }}
							<span class="setting-chip" :class="{ 'is-good': m.mounted }">{{ m.mounted ? $t('Mounted') : $t('Not mounted') }}</span>
							<span v-if="!m.enabled" class="setting-chip">{{ $t('Disabled at boot') }}</span>
							<span v-if="m.read_only" class="setting-chip">{{ $t('Read-only') }}</span>
						</div>
					</div>
					<div class="row-control">
						<b-switch :value="m.enabled" size="is-small" class="is-flex-direction-row-reverse mr-2" type="is-dark"
							:title="$t('Mount at boot')" @input="toggleEnabled(m, $event)"></b-switch>
						<button class="icon-button mr-2" type="button" :title="$t('Edit')" @click="toggleEdit(m)">
							<b-icon icon="edit-outline" pack="casa" size="is-16"></b-icon>
						</button>
						<b-button rounded size="is-small" type="is-danger" outlined @click="confirmRemove(m)">{{ $t('Remove') }}</b-button>
					</div>
				</div>

				<form v-if="editingMountPoint === m.mount_point" class="fstab-form full-width" @submit.prevent="submitEdit(m)">
					<div class="fstab-form-row">
						<label class="fstab-form-label">{{ $t('Mount point') }}</label>
						<b-input v-model="editDraft.new_mount_point" size="is-small" expanded></b-input>
					</div>
					<div class="fstab-form-row">
						<label class="fstab-form-label">{{ $t('Filesystem') }}</label>
						<b-input v-model="editDraft.fstype" size="is-small" expanded></b-input>
					</div>
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
					<div class="fstab-form-row">
						<label class="fstab-form-label">{{ $t('Advanced options') }}</label>
						<b-input v-model="editDraft.options" size="is-small" expanded></b-input>
					</div>
					<div class="fstab-form-actions">
						<b-button rounded size="is-small" @click="editingMountPoint = null">{{ $t('Cancel') }}</b-button>
						<b-button rounded size="is-small" type="is-dark" native-type="submit" :loading="submitting">{{ $t('Save') }}</b-button>
					</div>
					<p v-if="formError" class="error-note">{{ formError }}</p>
				</form>
			</div>
			<div v-if="!mounts.length" class="account-empty">{{ $t('No drives added yet.') }}</div>
		</div>

		<button type="button" class="system-entries-toggle" @click="showSystem = !showSystem">
			<b-icon :icon="showSystem ? 'chevron-up' : 'chevron-down'" pack="mdi" size="is-16"></b-icon>
			{{ $t('System entries ({count})', { count: systemEntries.length }) }}
		</button>
		<div v-if="showSystem" class="setting-card">
			<p class="hint">{{ $t("These come from the base system or were added by hand - not managed here. Edit /etc/fstab directly if you need to change them.") }}</p>
			<div v-for="e in systemEntries" :key="e.mount_point" class="setting-row">
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
	options: '',
	read_only: false,
	mount_at_boot: true,
	check_at_boot: false
})

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
			formError: '',
			error: ''
		}
	},
	created() {
		this.refresh()
	},
	methods: {
		formatSize,
		refresh() {
			this.error = ''
			this.$api.fstab.list().then(res => {
				if (res.data.success === 200) {
					this.mounts = (res.data.data && res.data.data.managed) || []
					this.systemEntries = (res.data.data && res.data.data.system) || []
				}
			}).catch(() => {
				this.error = this.$t('Failed to load fstab entries')
			})
			this.refreshCandidates()
		},
		refreshCandidates() {
			this.$api.fstab.candidates().then(res => {
				if (res.data.success === 200) this.candidates = res.data.data || []
			})
		},
		candidateLabel(c) {
			let label = `${c.label || c.path} · ${this.formatSize(c.size)} · ${c.fstype}`
			if (c.mounted) label += ` · ${this.$t('currently mounted at {mp}', { mp: c.mount_point })}`
			return label
		},
		toggleAddForm() {
			this.showAddForm = !this.showAddForm
			this.formError = ''
			if (this.showAddForm) {
				this.addDraft = emptyDraft()
				this.refreshCandidates()
			}
		},
		onCandidateSelected(uuid) {
			const candidate = this.candidates.find(c => c.uuid === uuid)
			if (!candidate) return
			this.addDraft.fstype = candidate.fstype
			if (!this.addDraft.mount_point) {
				const name = (candidate.label || uuid.slice(0, 8)).replace(/[^a-zA-Z0-9_-]/g, '_')
				this.addDraft.mount_point = `/DATA/${name}`
			}
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
			}).catch(e => {
				this.formError = (e.response && e.response.data && e.response.data.message) || this.$t('Failed to save changes')
			}).finally(() => {
				this.submitting = false
			})
		},
		toggleEnabled(m, enabled) {
			this.$api.fstab.setEnabled(m.mount_point, enabled).then(() => this.refresh()).catch(() => {
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
					this.$api.fstab.remove(m.mount_point).then(() => this.refresh()).catch(e => {
						this.error = (e.response && e.response.data && e.response.data.message) || this.$t('Failed to remove mount')
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

.hint {
	font-size: 0.75rem;
	opacity: 0.6;
	margin-top: 0.15rem;
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

	&:hover {
		background: rgba(0, 0, 0, 0.09);
		color: #1e293b;
	}
}

.fstab-form {
	padding: 0.75rem 1.25rem;
	background: rgba(0, 0, 0, 0.02);
	border-radius: 8px;
	margin: 0.5rem 0 1rem;
	display: flex;
	flex-direction: column;
	gap: 0.6rem;

	&.full-width {
		margin: 0 0 0.75rem 3.25rem;
		width: calc(100% - 3.25rem);
	}
}

.fstab-form-row {
	display: flex;
	align-items: center;
	gap: 0.75rem;
}

.fstab-form-label {
	width: 6.5rem;
	flex-shrink: 0;
	font-size: 0.8rem;
	opacity: 0.75;
}

.fstab-switch-row {
	display: flex;
	align-items: center;
	justify-content: space-between;
	font-size: 0.8rem;
	padding-left: 6.5rem;
}

.fstab-form-actions {
	display: flex;
	justify-content: flex-end;
	gap: 0.5rem;
	margin-top: 0.25rem;
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
