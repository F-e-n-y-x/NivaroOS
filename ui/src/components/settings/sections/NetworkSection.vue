<template>
	<section class="settings-section">
		<h2 class="section-title">{{ $t('Network & Sharing') }}</h2>

		<h3 class="setting-card-title">{{ $t('This Device') }}</h3>
		<div class="setting-card">
			<div v-if="!interfaces.length" class="setting-row">
				<div class="row-label">{{ $t('No network interfaces detected.') }}</div>
			</div>
			<div v-for="iface in interfaces" :key="iface.interface" class="setting-row">
				<b-icon class="row-icon" icon="internet-outline" pack="casa" size="is-20"></b-icon>
				<div class="row-label">{{ iface.interface }}</div>
				<div class="row-control">{{ iface.ip }}</div>
			</div>
		</div>

		<h3 class="setting-card-title">{{ $t('Network Shares') }}</h3>
		<div class="setting-card">
			<network-shares-panel></network-shares-panel>
		</div>

		<h3 class="setting-card-title">{{ $t('Remote Access') }}</h3>
		<remote-access-panel></remote-access-panel>
	</section>
</template>

<script>
import NetworkSharesPanel from '@/components/settings/NetworkSharesPanel.vue'
import RemoteAccessPanel from '@/components/settings/RemoteAccessPanel.vue'

export const ROWS = [
	{ label: 'This Device' },
	{ label: 'Network Shares' },
	{ label: 'Remote Access' }
]

export default {
	name: 'network-section',
	components: { NetworkSharesPanel, RemoteAccessPanel },
	data() {
		return {
			interfaces: []
		}
	},
	created() {
		this.$api.sys.getNetworkInterfaces().then(res => {
			if (res.data.success === 200) this.interfaces = res.data.data || []
		})
	}
}
</script>
