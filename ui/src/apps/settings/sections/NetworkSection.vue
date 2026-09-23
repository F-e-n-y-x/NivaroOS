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

		<h3 class="setting-card-title">{{ $t('Connect to a Network Share') }}</h3>
		<div class="setting-card">
			<network-connections-panel></network-connections-panel>
		</div>

		<h3 class="setting-card-title">{{ $t('Share a Folder from This Device') }}</h3>
		<div class="setting-card">
			<network-shares-panel></network-shares-panel>
		</div>

		<h3 class="setting-card-title">{{ $t('Remote Access') }}</h3>
		<remote-access-panel></remote-access-panel>
	</section>
</template>

<script>
import NetworkSharesPanel from '@/apps/settings/NetworkSharesPanel.vue'
import NetworkConnectionsPanel from '@/apps/settings/NetworkConnectionsPanel.vue'
import RemoteAccessPanel from '@/apps/settings/RemoteAccessPanel.vue'

// Search index: labels are the titles this section renders (the search
// jumps to them); keywords are other words people type for them.
export const ROWS = [
	{ label: 'This Device', keywords: 'ip address network interface' },
	{ label: 'Connect to a Network Share', keywords: 'smb cifs nas windows share mount' },
	{ label: 'Share a Folder from This Device', keywords: 'samba smb share folder' },
	{ label: 'Remote Access', keywords: 'tailscale vpn remote' }
]

export default {
	name: 'network-section',
	components: { NetworkSharesPanel, NetworkConnectionsPanel, RemoteAccessPanel },
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
