<!-- src/apps/files/SharedView.vue -->
<template>
	<div class="shared-view">
		<header class="shared-header">
			<h3 class="title is-6 mb-0">{{ $t('Shares') }}</h3>
			<b-button icon-left="plus" rounded size="is-small" type="is-primary" @click="$emit('add-share')">
				{{ $t('Share a folder') }}
			</b-button>
		</header>

		<div v-if="!isLoading && quickList.length === 0 && smbList.length === 0" class="shared-empty">
			<b-image :src="require('@/assets/img/share/share-empty.svg')" class="is-160x160"></b-image>
			<p>{{ $t('Right-click any file to Quick Share it, or share a whole folder over your local network.') }}</p>
			<b-button rounded type="is-primary" @click="$emit('add-share')">{{ $t('Start') }}</b-button>
		</div>

		<div v-else class="shared-list scrollbars-light">
			<template v-if="quickList.length">
				<div class="shared-section-label">{{ $t('Quick Shares') }}</div>
				<div v-for="item in quickList" :key="'q-' + item.id" class="shared-row">
					<span class="shared-icon">
						<b-icon icon="link-variant" pack="mdi" class="casa-color-blue" custom-size="casa-24px"></b-icon>
					</span>
					<div class="shared-info">
						<div class="shared-name one-line">{{ item.name }}</div>
						<div class="shared-path one-line">{{ quickShareExpiryText(item) }}</div>
					</div>
					<b-button outlined rounded size="is-small" @click="copyQuickShareLink(item)">
						{{ $t('Copy Link') }}
					</b-button>
					<b-button outlined rounded size="is-small" type="is-danger" @click="confirmRevokeQuickShare(item)">
						{{ $t('Revoke') }}
					</b-button>
				</div>
			</template>

			<template v-if="smbList.length">
				<div class="shared-section-label">{{ $t('SMB Shares') }}</div>
				<div v-for="item in smbList" :key="'s-' + item.id" class="shared-row">
					<span class="shared-icon">
						<b-icon icon="data-outline" pack="casa" class="casa-color-blue" custom-size="casa-24px"></b-icon>
					</span>
					<div class="shared-info">
						<div class="shared-name one-line">{{ item.name }}</div>
						<div class="shared-path one-line">{{ item.path }}</div>
					</div>
					<b-button outlined rounded size="is-small" type="is-danger" @click="confirmUnshare(item)">
						{{ $t('UnShare') }}
					</b-button>
				</div>
			</template>
		</div>

		<b-loading v-model="isLoading" :is-full-page="false"></b-loading>
		<confirm-window v-bind="confirmWindowProps" @confirm="_onConfirmWindowConfirm" @cancel="_onConfirmWindowCancel"></confirm-window>
	</div>
</template>

<script>
import copy from 'clipboard-copy'
import { confirmWindowMixin } from '@/mixins/confirmWindow'

export default {
	name: 'files-shared-view',
	mixins: [confirmWindowMixin],
	inject: ['filesController'],
	data() {
		return {
			smbList: [],
			quickList: [],
			// Starts false (not true, unlike legacy ShareListPage.vue's `isLoading: true` +
			// mounted() fetch) because this component is always mounted (kept in the DOM via
			// `v-show` in FilesApp.vue) rather than created fresh each time the section is
			// switched to - so per the brief, fetching is driven by watching
			// filesController.activeSection instead of an eager mount-time fetch.
			isLoading: false,
		}
	},
	watch: {
		'filesController.activeSection'(section) {
			if (section === 'shared') {
				this.getSharedList()
			}
		},
	},
	methods: {
		// Kept as one method (fetching both share types) rather than splitting
		// it, since FilesApp.vue's onShareCreated() calls this by name on
		// `$refs.sharedView` after an SMB share is created via ShareSelectDialog.
		async getSharedList() {
			this.isLoading = true
			await Promise.all([this.fetchSmbShares(), this.fetchQuickShares()])
			this.isLoading = false
		},
		async fetchSmbShares() {
			try {
				const res = await this.$api.samba.getShares()
				this.smbList = res.data.data.map((item) => {
					return {
						id: item.id,
						name: item.path.split('/').pop(),
						path: item.path,
					}
				})
			} catch (error) {
				this.smbList = []
			}
		},
		async fetchQuickShares() {
			try {
				const res = await this.$api.quickshare.list()
				this.quickList = res.data.success === 200 ? res.data.data : []
			} catch (error) {
				this.quickList = []
			}
		},
		quickShareExpiryText(item) {
			if (!item.expires_at) return this.$t('Never expires')
			return this.$t('Expires') + ': ' + new Date(item.expires_at * 1000).toLocaleString()
		},
		copyQuickShareLink(item) {
			copy(item.url)
			this.$buefy.toast.open({ message: this.$t('Link copied'), type: 'is-success' })
		},
		confirmRevokeQuickShare(item) {
			this.confirmWindow({
				title: this.$t('Revoke Share Link'),
				message: this.$t('Are you sure you want to revoke this share link? Anyone with the link will no longer be able to access the file.'),
				confirmText: this.$t('Revoke'),
				cancelText: this.$t('Cancel'),
				iconPack: 'casa',
				icon: 'danger',
				type: 'is-danger',
				hasIcon: true,
				onConfirm: () => {
					this.$api.quickshare
						.remove(item.id)
						.then(() => {
							this.fetchQuickShares()
							this.$buefy.toast.open({ message: this.$t('Share link revoked.'), type: 'is-success' })
						})
						.catch(() => {
							this.$buefy.toast.open({ message: this.$t('Failed to revoke share link.'), type: 'is-danger' })
						})
				},
			})
		},

		// Mirrors legacy FilePanel.vue's handleUnShare (the $EventBus.UN_SHARE handler that
		// ActionButton.vue/ContextMenu.vue in the old filebrowser both delegate to).
		confirmUnshare(item) {
			this.confirmWindow({
				title: this.$t('Unsharing Folder'),
				message: this.$t('Are you sure you want to unshare this Folder?'),
				confirmText: this.$t('UnShare'),
				cancelText: this.$t('Cancel'),
				iconPack: 'casa',
				icon: 'danger',
				type: 'is-danger',
				hasIcon: true,
				onConfirm: () => {
					this.$api.samba
						.deleteShare(item.id)
						.then(() => {
							this.getSharedList()
							this.$buefy.toast.open({
								message: this.$t('Folder unshared.'),
								type: 'is-success',
							})
						})
						.catch(() => {
							this.$buefy.toast.open({
								message: this.$t('Unshared failed.'),
								type: 'is-danger',
							})
						})
				},
			})
		},
	},
}
</script>

<style lang="scss" scoped>
.shared-view {
	flex: 1 1 auto;
	display: flex;
	flex-direction: column;
	min-width: 0;
	min-height: 0;
	padding: var(--space-4) var(--space-6);
	position: relative;
}
.shared-header {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	justify-content: space-between;
	margin-bottom: var(--space-4);
}
.shared-empty {
	flex: 1 1 auto;
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	gap: var(--space-3);
	text-align: center;
	color: var(--theme-text-muted, rgba(0, 0, 0, 0.6));
}
.shared-list {
	flex: 1 1 auto;
	overflow-y: auto;
	min-height: 0;
}
.shared-section-label {
	font-size: var(--font-xs);
	font-weight: 600;
	text-transform: uppercase;
	letter-spacing: 0.02em;
	color: var(--theme-text-muted, rgba(0, 0, 0, 0.5));
	padding: var(--space-2) var(--space-3);

	&:not(:first-child) {
		margin-top: var(--space-2);
	}
}
.shared-row {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	padding: var(--space-2) var(--space-3);
	border-radius: var(--radius-sm);
	&:hover {
		background: var(--theme-card-hover, rgba(0, 0, 0, 0.04));
	}
}
.shared-icon {
	flex-shrink: 0;
	display: flex;
	align-items: center;
}
.shared-info {
	flex: 1 1 auto;
	min-width: 0;
}
.shared-name {
	font-weight: 600;
}
.shared-path {
	font-size: var(--font-xs);
	color: var(--theme-text-muted, rgba(0, 0, 0, 0.6));
}
.one-line {
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
}
</style>
