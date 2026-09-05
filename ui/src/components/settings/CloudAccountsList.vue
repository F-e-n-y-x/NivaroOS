<template>
	<div class="cloud-accounts-list">
		<div v-for="a in accounts" :key="a.mount_point" class="setting-row">
			<i class="row-icon mdi" :class="`mdi-${a.icon || 'cloud-outline'}`"></i>
			<div class="row-label">
				<div class="setting-title">{{ a.name || a.fs }}</div>
				<div class="setting-desc">{{ a.mount_point }}</div>
			</div>
			<div class="row-control">
				<b-button rounded size="is-small" type="is-danger" outlined :loading="removingKey === a.mount_point" @click="confirmRemove(a)">
					{{ $t('Remove') }}
				</b-button>
			</div>
		</div>

		<div v-if="!accounts.length" class="account-empty">
			{{ $t('Nothing connected yet - add one below and it shows up as a location in Files.') }}
		</div>
	</div>
</template>

<script>
export default {
	name: 'cloud-accounts-list',
	data() {
		return {
			accounts: [],
			removingKey: null
		}
	},
	created() {
		this.refresh()
	},
	methods: {
		refresh() {
			this.$api.cloud.list().then(res => {
				if (res.data.success === 200) this.accounts = res.data.data || []
			})
		},
		confirmRemove(account) {
			this.$buefy.dialog.confirm({
				container: '#window-settings',
				title: this.$t('Remove account'),
				message: this.$t('Disconnect {name}? This unmounts it from Files - it will no longer be accessible from here.', { name: account.name || account.fs }),
				type: 'is-danger',
				confirmText: this.$t('Remove'),
				cancelText: this.$t('Cancel'),
				onConfirm: () => {
					this.removingKey = account.mount_point
					this.$api.cloud
						.umount({ mount_point: account.mount_point })
						.then(() => this.refresh())
						.finally(() => {
							this.removingKey = null
						})
				}
			})
		}
	}
}
</script>
