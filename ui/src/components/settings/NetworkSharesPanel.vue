<template>
	<div class="shares-panel">
		<div v-for="s in shares" :key="s.id" class="setting-row">
			<b-icon class="row-icon" icon="share" pack="casa" size="is-20"></b-icon>
			<div class="row-label">
				<div class="setting-title">{{ s.name || s.path }}</div>
				<div class="setting-desc">
					{{ s.path }}
					<span class="share-meta-sep">&middot;</span>
					<span v-if="s.anonymous">{{ $t('Anyone on the network') }}</span>
					<span v-else>{{ $t('Requires SMB sign-in') }}</span>
					<template v-if="s.read_only">
						<span class="share-meta-sep">&middot;</span>{{ $t('Read-only') }}
					</template>
				</div>
			</div>
			<div class="row-control">
				<button class="icon-button" type="button" :title="$t('Edit share')" @click="openEdit(s)">
					<i class="mdi mdi-pencil-outline"></i>
				</button>
				<b-button rounded size="is-small" type="is-danger" outlined @click="confirmDelete(s)">
					{{ $t('Delete') }}
				</b-button>
			</div>
		</div>

		<div v-if="!shares.length" class="account-empty">
			{{ $t('No network shares configured yet.') }}
		</div>

		<div class="add-row">
			<b-button rounded size="is-small" type="is-dark" @click="openAdd">
				<i class="mdi mdi-plus mr-1"></i>{{ $t('Share a folder') }}
			</b-button>
		</div>

		<settings-overlay
			:active="showModal"
			:title="editing ? $t('Edit Network Share') : $t('Share a Folder')"
			width="28rem"
			@close="closeModal"
		>
			<b-field :label="$t('Folder path')" :message="editing ? '' : $t('e.g. /DATA/Shared')">
				<b-input v-model="form.path" size="is-small" :disabled="!!editing" placeholder="/DATA/..."></b-input>
			</b-field>
			<b-field :label="$t('Share name')" :message="$t('Shown to other devices on the network - defaults to the folder name if left blank')">
				<b-input v-model="form.name" size="is-small" :placeholder="defaultName"></b-input>
			</b-field>

			<div class="access-choice">
				<label class="access-option" :class="{ active: form.anonymous }">
					<input type="radio" :checked="form.anonymous" @change="form.anonymous = true" />
					<div class="access-option-text">
						<span class="access-option-title">{{ $t('Anyone on the network') }}</span>
						<span class="access-option-desc">{{ $t("No sign-in required. Windows 10/11 blocks this kind of connection by default though - if it doesn't show up there, use \"Requires SMB sign-in\" instead.") }}</span>
					</div>
				</label>
				<label class="access-option" :class="{ active: !form.anonymous }">
					<input type="radio" :checked="!form.anonymous" @change="form.anonymous = false" />
					<div class="access-option-text">
						<span class="access-option-title">{{ $t('Requires SMB sign-in') }}</span>
						<span class="access-option-desc">{{ $t('Works everywhere, including Windows. Only accounts added under Settings > Users > SMB Users can connect.') }}</span>
					</div>
				</label>
			</div>

			<label class="readonly-toggle">
				<b-switch v-model="form.read_only" size="is-small" type="is-primary"></b-switch>
				<span>{{ $t('Read-only (others can view but not change files)') }}</span>
			</label>

			<p v-if="error" class="error-note">{{ error }}</p>

			<template #footer>
				<b-button rounded size="is-small" @click="closeModal">{{ $t('Cancel') }}</b-button>
				<b-button
					rounded
					size="is-small"
					type="is-primary"
					:loading="saving"
					:disabled="!editing && !form.path.trim()"
					@click="submit"
				>
					{{ editing ? $t('Save Changes') : $t('Share folder') }}
				</b-button>
			</template>
		</settings-overlay>
	</div>
</template>

<script>
import SettingsOverlay from '@/components/settings/SettingsOverlay.vue'

const emptyForm = () => ({ path: '', name: '', anonymous: true, read_only: false })

export default {
	name: 'network-shares-panel',
	components: { SettingsOverlay },
	data() {
		return {
			shares: [],
			showModal: false,
			editing: null,
			form: emptyForm(),
			saving: false,
			error: ''
		}
	},
	computed: {
		defaultName() {
			const parts = (this.form.path || '').split('/').filter(Boolean)
			return parts[parts.length - 1] || ''
		}
	},
	created() {
		this.refresh()
	},
	methods: {
		refresh() {
			this.$api.samba.getShares().then(res => {
				if (res.data.success === 200) this.shares = res.data.data || []
			})
		},
		openAdd() {
			this.editing = null
			this.form = emptyForm()
			this.error = ''
			this.showModal = true
		},
		openEdit(share) {
			this.editing = share
			this.form = { path: share.path, name: share.name || '', anonymous: !!share.anonymous, read_only: !!share.read_only }
			this.error = ''
			this.showModal = true
		},
		closeModal() {
			this.showModal = false
		},
		submit() {
			this.error = ''
			this.saving = true
			if (this.editing) {
				this.$api.samba.updateShare(this.editing.id, {
					name: this.form.name.trim(),
					read_only: this.form.read_only,
					anonymous: this.form.anonymous
				}).then(res => {
					if (res.data.success === 200) {
						this.showModal = false
						this.refresh()
					} else {
						this.error = res.data.message
					}
				}).catch(e => {
					this.error = (e.response && e.response.data && e.response.data.message) || this.$t('Failed to save share')
				}).finally(() => {
					this.saving = false
				})
				return
			}
			this.$api.samba.createShare([{
				path: this.form.path.trim(),
				name: this.form.name.trim(),
				anonymous: this.form.anonymous,
				read_only: this.form.read_only
			}]).then(res => {
				if (res.data.success === 200) {
					this.showModal = false
					this.refresh()
				} else {
					this.error = res.data.message
				}
			}).catch(e => {
				this.error = (e.response && e.response.data && e.response.data.message) || this.$t('Failed to create share')
			}).finally(() => {
				this.saving = false
			})
		},
		confirmDelete(share) {
			this.$buefy.dialog.confirm({
				container: '#window-settings',
				title: this.$t('Delete share'),
				message: this.$t('Stop sharing {path}? The folder and its files are not deleted - only removed from the network.', { path: share.path }),
				type: 'is-danger',
				confirmText: this.$t('Delete'),
				cancelText: this.$t('Cancel'),
				onConfirm: () => {
					this.$api.samba.deleteShare(share.id).then(() => this.refresh())
				}
			})
		}
	}
}
</script>

<style lang="scss" scoped>
.shares-panel {
	display: flex;
	flex-direction: column;
}

.share-meta-sep {
	margin: 0 0.3rem;
}

.add-row {
	padding: 0.9rem 1.25rem;
}

.error-note {
	padding: 0 0.6rem;
	color: var(--color-danger);
	font-size: 0.775rem;
	margin-top: -0.25rem;
	margin-bottom: 0.5rem;
}

.access-choice {
	display: flex;
	flex-direction: column;
	gap: 0.5rem;
	margin: 0.75rem 0 1rem;
}

.access-option {
	display: flex;
	align-items: flex-start;
	gap: 0.6rem;
	padding: 0.6rem 0.75rem;
	border: 1px solid var(--color-border-strong);
	border-radius: var(--radius-control);
	cursor: pointer;
	transition: border-color 0.15s ease, background 0.15s ease;

	input {
		margin-top: 0.2rem;
		accent-color: var(--color-primary);
	}

	&.active {
		border-color: var(--color-primary);
		background: var(--color-primary-soft);
	}
}

.access-option-text {
	display: flex;
	flex-direction: column;
	gap: 0.1rem;
}

.access-option-title {
	font-size: 0.825rem;
	font-weight: 500;
	color: #1e293b;
}

.access-option-desc {
	font-size: 0.7rem;
	color: var(--color-text-muted);
}

.readonly-toggle {
	display: flex;
	align-items: center;
	gap: 0.6rem;
	font-size: 0.8125rem;
	color: #1e293b;
	cursor: pointer;
	margin-bottom: 0.25rem;
}
</style>
