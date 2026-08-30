<!-- src/components/files/SharedView.vue -->
<template>
	<div class="shared-view">
		<header class="shared-header">
			<h3 class="title is-6 mb-0">{{ $t('Shared Folders') }}</h3>
			<b-button icon-left="plus" rounded size="is-small" type="is-primary" @click="$emit('add-share')">
				{{ $t('Share a folder') }}
			</b-button>
		</header>

		<div v-if="!isLoading && list.length === 0" class="shared-empty">
			<b-image :src="require('@/assets/img/share/share-empty.svg')" class="is-160x160"></b-image>
			<p>{{ $t('Follow the guide to start sharing your files on the local network.') }}</p>
			<b-button rounded type="is-primary" @click="$emit('add-share')">{{ $t('Start') }}</b-button>
		</div>

		<div v-else class="shared-list scrollbars-light">
			<div v-for="item in list" :key="item.id" class="shared-row">
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
		</div>

		<b-loading v-model="isLoading" :is-full-page="false"></b-loading>
	</div>
</template>

<script>
export default {
	name: 'files-shared-view',
	inject: ['filesController'],
	data() {
		return {
			list: [],
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
		async getSharedList() {
			this.isLoading = true
			try {
				const res = await this.$api.samba.getShares()
				this.list = res.data.data.map((item) => {
					return {
						id: item.id,
						name: item.path.split('/').pop(),
						path: item.path,
					}
				})
			} catch (error) {
				this.list = []
			}
			this.isLoading = false
		},

		// Mirrors legacy FilePanel.vue's handleUnShare (the $EventBus.UN_SHARE handler that
		// ActionButton.vue/ContextMenu.vue in the old filebrowser both delegate to).
		confirmUnshare(item) {
			this.$buefy.dialog.confirm({
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
	padding: 1rem 1.5rem;
	position: relative;
}
.shared-header {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	justify-content: space-between;
	margin-bottom: 1rem;
}
.shared-empty {
	flex: 1 1 auto;
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	gap: 0.75rem;
	text-align: center;
	color: rgba(0, 0, 0, 0.6);
}
.shared-list {
	flex: 1 1 auto;
	overflow-y: auto;
	min-height: 0;
}
.shared-row {
	display: flex;
	align-items: center;
	gap: 0.75rem;
	padding: 0.5rem 0.75rem;
	border-radius: 6px;
	&:hover {
		background: rgba(0, 0, 0, 0.04);
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
	font-size: 0.8rem;
	color: rgba(0, 0, 0, 0.6);
}
.one-line {
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
}
</style>
