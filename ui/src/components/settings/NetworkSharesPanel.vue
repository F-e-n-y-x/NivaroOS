<template>
	<div class="shares-panel">
		<p class="hint">{{ $t('Share a folder on this box over the local network via SMB.') }}</p>

		<div v-for="s in shares" :key="s.id" class="user-row">
			<div class="user-main">
				<b-icon icon="share" pack="casa" size="is-20"></b-icon>
				<div class="user-name">{{ s.path }}</div>
			</div>
			<b-button rounded size="is-small" type="is-danger" outlined @click="confirmDelete(s)">
				{{ $t('Delete') }}
			</b-button>
		</div>

		<form class="add-user-form" @submit.prevent="createShare">
			<b-input v-model="newPath" :placeholder="$t('Folder path, e.g. /DATA/Shared')" size="is-small" class="add-input"></b-input>
			<b-button rounded size="is-small" type="is-dark" native-type="submit" :loading="creating">
				{{ $t('Share folder') }}
			</b-button>
		</form>
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
	padding: 1.25rem;
	gap: 0.5rem;
}

.hint {
	font-size: 0.75rem;
	opacity: 0.6;
	margin-bottom: 0.25rem;
}

.user-row {
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: 0.75rem;
	padding: 0.6rem 0;
	border-bottom: 1px solid rgba(0, 0, 0, 0.08);
}

.user-main {
	display: flex;
	align-items: center;
	gap: 0.6rem;
}

.user-name {
	font-weight: 600;
	font-size: 0.85rem;
}

.add-user-form {
	display: flex;
	align-items: center;
	gap: 0.5rem;
	margin-top: 0.75rem;
	flex-wrap: wrap;
}

.add-input {
	width: 16rem;
}
</style>
