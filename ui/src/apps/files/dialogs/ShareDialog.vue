<!-- src/apps/files/dialogs/ShareDialog.vue -->
<template>
	<files-dialog-overlay :title="$t('Share') + ' – ' + itemName" @close="$emit('close')">
		<div class="share-dialog">
			<div v-if="isFolder" class="share-tabs">
				<button class="share-tab" :class="{ active: activeTab === 'quick' }" @click="activeTab = 'quick'">
					{{ $t('Quick Share') }}
				</button>
				<button class="share-tab" :class="{ active: activeTab === 'smb' }" @click="activeTab = 'smb'">
					{{ $t('SMB Share') }}
				</button>
			</div>

			<!-- Quick Share: a short, expiring, unauthenticated download URL for one file. -->
			<div v-if="activeTab === 'quick'" class="share-panel">
				<template v-if="isFolder">
					<p class="share-hint">
						{{ $t('Quick Share works for individual files. To share this whole folder, use SMB Share instead.') }}
					</p>
				</template>
				<template v-else-if="!createdShare">
					<b-field :label="$t('Link expires')">
						<b-select v-model="expiry" expanded>
							<option value="1h">{{ $t('1 hour') }}</option>
							<option value="1d">{{ $t('1 day') }}</option>
							<option value="never">{{ $t('Never') }}</option>
						</b-select>
					</b-field>
					<div class="dialog-actions">
						<b-button :label="$t('Create Link')" :loading="isCreating" rounded type="is-primary" @click="createQuickShare"></b-button>
					</div>
				</template>
				<template v-else>
					<b-field :label="$t('Share Link')">
						<b-input :value="createdShare.url" readonly expanded></b-input>
						<p class="control">
							<b-button :icon-left="copied ? 'check' : 'content-copy'" @click="copyLink"></b-button>
						</p>
					</b-field>
					<p class="share-hint">{{ expiryText }}</p>
					<div class="dialog-actions">
						<b-button :label="$t('Done')" rounded type="is-primary" @click="$emit('close')"></b-button>
					</div>
				</template>
			</div>

			<!-- SMB Share: this folder becomes a persistent local-network share. -->
			<div v-if="isFolder && activeTab === 'smb'" class="share-panel">
				<template v-if="!smbCreated">
					<p class="share-hint">
						{{ $t('Anyone on your local network will be able to access this folder, with no login required.') }}
					</p>
					<div class="dialog-actions">
						<b-button :label="$t('Create SMB Share')" :loading="smbCreating" rounded type="is-primary" @click="createSmbShare"></b-button>
					</div>
				</template>
				<template v-else>
					<p class="share-hint is-success">{{ $t('SMB share created. Manage it from the Shared Folders section.') }}</p>
					<div class="dialog-actions">
						<b-button :label="$t('Done')" rounded type="is-primary" @click="$emit('close')"></b-button>
					</div>
				</template>
			</div>
		</div>
	</files-dialog-overlay>
</template>

<script>
import copy from 'clipboard-copy'
import DialogOverlay from '../DialogOverlay.vue'
import { baseName } from '@/utils/files/path'

export default {
	name: 'share-dialog',
	components: { FilesDialogOverlay: DialogOverlay },
	props: {
		item: { type: Object, required: true },
	},
	data() {
		return {
			activeTab: 'quick',
			expiry: '1d',
			isCreating: false,
			createdShare: null,
			copied: false,
			smbCreating: false,
			smbCreated: false,
		}
	},
	computed: {
		isFolder() {
			return !!(this.item && this.item.is_dir)
		},
		itemName() {
			return (this.item && (this.item.name || baseName(this.item.path))) || ''
		},
		expiryText() {
			if (!this.createdShare || !this.createdShare.expires_at) return this.$t('This link never expires.')
			const date = new Date(this.createdShare.expires_at * 1000)
			return this.$t('Expires') + ': ' + date.toLocaleString()
		},
	},
	methods: {
		createQuickShare() {
			this.isCreating = true
			this.$api.quickshare
				.create(this.item.path, this.expiry)
				.then((res) => {
					if (res.data.success === 200) {
						this.createdShare = res.data.data
					} else {
						this.$buefy.toast.open({ message: res.data.message, type: 'is-danger' })
					}
					this.isCreating = false
				})
				.catch((error) => {
					this.isCreating = false
					this.$buefy.toast.open({
						message: (error.response && error.response.data && error.response.data.message) || this.$t('Failed to create share link'),
						type: 'is-danger',
					})
				})
		},
		copyLink() {
			if (!this.createdShare) return
			copy(this.createdShare.url)
			this.copied = true
			setTimeout(() => (this.copied = false), 2000)
		},
		createSmbShare() {
			this.smbCreating = true
			this.$api.samba
				.createShare([{ path: this.item.path, anonymous: true }])
				.then(() => {
					this.smbCreating = false
					this.smbCreated = true
				})
				.catch((error) => {
					this.smbCreating = false
					this.$buefy.toast.open({
						message: (error.response && error.response.data && error.response.data.message) || this.$t('Failed to create SMB share'),
						type: 'is-danger',
					})
				})
		},
	},
}
</script>

<style lang="scss" scoped>
.share-dialog {
	min-width: 20rem;
}
.share-tabs {
	display: flex;
	gap: var(--space-2);
	margin-bottom: var(--space-4);
	border-bottom: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.1));
}
.share-tab {
	background: none;
	border: none;
	padding: var(--space-2) var(--space-1);
	margin-bottom: -1px;
	border-bottom: 2px solid transparent;
	color: var(--theme-text-secondary, #64748b);
	cursor: pointer;
	font-weight: 500;

	&.active {
		color: var(--color-primary, #2563eb);
		border-bottom-color: var(--color-primary, #2563eb);
	}
}
.share-hint {
	color: var(--theme-text-secondary, #64748b);
	font-size: var(--font-sm);
	margin-bottom: var(--space-3);

	&.is-success {
		color: var(--color-success, #16a34a);
	}
}
.dialog-actions {
	display: flex;
	justify-content: flex-end;
	margin-top: var(--space-4);
}
</style>
