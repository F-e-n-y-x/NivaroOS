<template>
	<div class="shares-panel">
		<div v-for="s in shares" :key="s.id" class="setting-row">
			<b-icon class="row-icon" icon="share" pack="casa" size="is-20"></b-icon>
			<div class="row-label">
				<div class="setting-title">{{ s.path }}</div>
				<div class="setting-desc">{{ $t('SMB network folder share') }}</div>
			</div>
			<div class="row-control">
				<b-button rounded size="is-small" type="is-danger" outlined @click="confirmDelete(s)">
					{{ $t('Delete') }}
				</b-button>
			</div>
		</div>

		<div v-if="!shares.length" class="account-empty">
			{{ $t('No network shares configured yet.') }}
		</div>

		<div class="account-inline-form mt-3">
			<b-input v-model="newPath" :placeholder="$t('Folder path, e.g. /DATA/Shared')" size="is-small" class="add-input mr-2"></b-input>
			<b-button rounded size="is-small" type="is-dark" :loading="creating" :disabled="!newPath.trim()" @click="createShare">
				<i class="mdi mdi-plus mr-1"></i>{{ $t('Share folder') }}
			</b-button>
		</div>
		<p v-if="error" class="error-note">{{ error }}</p>
	</div>
</template>

<script>
export default {
	name: 'network-shares-panel',
	data() {
		return {
			shares: [],
			newPath: '',
			creating: false,
			error: ''
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
		createShare() {
			this.error = ''
			if (!this.newPath) return
			this.creating = true
			this.$api.samba.createShare([{ path: this.newPath, anonymous: true }]).then(res => {
				if (res.data.success === 200) {
					this.newPath = ''
					this.refresh()
				} else {
					this.error = res.data.message
				}
			}).catch(e => {
				this.error = e.response && e.response.data ? e.response.data.message : this.$t('Failed to create share')
			}).finally(() => {
				this.creating = false
			})
		},
		confirmDelete(share) {
			this.$buefy.dialog.confirm({
				container: '#window-settings',
				title: this.$t('Delete share'),
				message: this.$t('Stop sharing {path}?', { path: share.path }),
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

.add-input {
	width: 18rem;
}

.error-note {
	padding: 0 1.25rem 0.75rem;
	color: #ef4444;
	font-size: 0.75rem;
}
</style>
