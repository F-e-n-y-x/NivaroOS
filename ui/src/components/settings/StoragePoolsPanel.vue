<template>
	<div class="storage-pools-panel">
		<div class="row-label-heading">{{ $t('Combined storage (mergerfs)') }}</div>
		<p v-if="!mergeEnabled" class="hint">
			{{ $t('Combined storage is not enabled on this box. Set EnableMergerFS=true in /etc/recasa/local-storage.conf and restart recasa-local-storage to turn it on.') }}
		</p>
		<div v-else>
			<p v-if="!merges.length" class="hint">{{ $t('No combined storage pools configured.') }}</p>
			<div v-for="m in merges" :key="m.mount_point" class="user-row">
				<div class="user-name">{{ m.mount_point }}</div>
				<span class="badge">{{ m.fstype }}</span>
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
.storage-pools-panel {
	padding: 1.25rem;
}

.hint {
	font-size: 0.75rem;
	opacity: 0.6;
	margin-bottom: 0.25rem;
}

.mt-4 {
	margin-top: 1.5rem;
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
	flex-direction: column;
	gap: 0.15rem;
}

.user-name {
	font-weight: 600;
	font-size: 0.85rem;
}

.badge {
	font-size: 0.7rem;
	opacity: 0.6;
}
</style>
