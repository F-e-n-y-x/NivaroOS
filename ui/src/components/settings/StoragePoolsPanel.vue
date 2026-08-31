<template>
	<div class="storage-pools-panel">
		<h3 class="setting-card-title">{{ $t('Combined Storage Pools (MergerFS)') }}</h3>
		<div class="setting-card">
			<div v-for="m in merges" :key="m.mount_point" class="setting-row">
				<b-icon class="row-icon" icon="storage-other" pack="casa" size="is-20"></b-icon>
				<div class="row-label">
					<div class="setting-title">{{ m.mount_point }}</div>
					<div class="setting-desc">{{ m.fstype }}</div>
				</div>
				<div class="row-control">
					<span class="setting-chip">{{ m.fstype || $t('Active') }}</span>
				</div>
			</div>
			<div v-if="!mergeEnabled" class="account-empty">
				{{ $t('Combined storage is not enabled on this host. Set EnableMergerFS=true in /etc/nivaroos/local-storage.conf to turn it on.') }}
			</div>
			<div v-else-if="!merges.length" class="account-empty">
				{{ $t('No combined storage pools configured.') }}
			</div>
		</div>
		<p v-if="error" class="error-note">{{ error }}</p>
	</div>
</template>

<script>
export default {
	name: 'storage-pools-panel',
	data() {
		return {
			merges: [],
			mergeEnabled: false,
			error: ''
		}
	},
	created() {
		this.loadMerges()
	},
	methods: {
		loadMerges() {
			this.$api.local_storage.getMergerfsInfo().then(res => {
				this.mergeEnabled = true
				this.merges = (res.data && res.data.data) || []
			}).catch(() => {
				this.mergeEnabled = false
			})
		}
	}
}
</script>

<style lang="scss" scoped>
.error-note {
	padding: 0 1.25rem 0.75rem;
	color: #ef4444;
	font-size: 0.75rem;
}
</style>
