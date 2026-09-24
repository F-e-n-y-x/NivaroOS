<template>
	<div class="storage-pools-panel">
		<h3 class="setting-card-title">{{ $t('Combined Storage Pools (MergerFS)') }}</h3>
		<div class="setting-card">
			<div v-for="m in merges" :key="m.mount_point" class="setting-row">
				<b-icon class="row-icon" icon="storage-other" pack="casa" size="is-20"></b-icon>
				<div class="row-label">
					<div class="setting-title">{{ m.mount_point }}</div>
					<div class="setting-desc">
						{{ m.fstype }}
						<template v-if="memberCount(m)"> &middot; {{ $t('{count} member drive(s)', { count: memberCount(m) }) }}</template>
					</div>
				</div>
				<div class="row-control">
					<span class="setting-chip">{{ m.fstype || $t('Active') }}</span>
				</div>
			</div>
			<div v-if="state === 'loading'" class="account-empty">{{ $t('Loading...') }}</div>
			<div v-else-if="state === 'disabled'" class="account-empty">
				{{ $t("Combined storage pools aren't turned on for this server. This is an advanced, server-level setting - ask your administrator to enable it if you need it.") }}
			</div>
			<div v-else-if="state === 'error'" class="account-empty pools-error" role="alert">
				<span>{{ error }}</span>
				<b-button rounded size="is-small" class="ml-2" @click="loadMerges">{{ $t('Retry') }}</b-button>
			</div>
			<div v-else-if="!merges.length" class="account-empty">
				{{ $t('No combined storage pools configured.') }}
			</div>
		</div>
	</div>
</template>

<script>
import events from '@/events/events'

export default {
	name: 'storage-pools-panel',
	data() {
		return {
			merges: [],
			// 'loading' | 'ready' | 'disabled' (server has mergerfs off) | 'error'
			state: 'loading',
			error: ''
		}
	},
	created() {
		this.loadMerges()
		this.$EventBus.$on(events.STORAGE_CHANGED, this.loadMerges)
	},
	beforeDestroy() {
		this.$EventBus.$off(events.STORAGE_CHANGED, this.loadMerges)
	},
	methods: {
		memberCount(m) {
			return Array.isArray(m.source_volume_uuids) ? m.source_volume_uuids.length : 0
		},
		loadMerges() {
			if (!this.merges.length) this.state = 'loading'
			this.error = ''
			this.$api.local_storage.getMergerfsInfo().then(res => {
				this.merges = (res.data && res.data.data) || []
				this.state = 'ready'
			}).catch(e => {
				const status = e && e.response && e.response.status
				// The server answers 503 only when mergerfs is switched off in
				// its config; anything else (500, network down, timeout) is a
				// real failure and must not be passed off as "not enabled".
				if (status === 503) {
					this.state = 'disabled'
					this.merges = []
					return
				}
				const msg = e && e.response && e.response.data && e.response.data.message
				this.state = 'error'
				this.error = status
					? this.$t('Could not load storage pools ({status}): {message}', { status, message: msg || this.$t('server error') })
					: this.$t('Could not reach the server to load storage pools.')
			})
		}
	}
}
</script>

<style lang="scss" scoped>
.pools-error {
	display: flex;
	align-items: center;
	justify-content: space-between;
	gap: var(--space-2);
	color: var(--color-danger-fg);
}
</style>
