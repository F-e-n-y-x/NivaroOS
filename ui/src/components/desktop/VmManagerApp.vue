<template>
	<div class="vm-app">
		<aside class="vm-nav">
			<button v-for="s in sections" :key="s.id" class="nav-item hover-effect _is-radius"
				:class="{ active: activeSection === s.id }" @click="activeSection = s.id">
				<b-icon :icon="s.icon" pack="casa" size="is-20"></b-icon>
				<span>{{ $t(s.label) }}</span>
			</button>
		</aside>

		<div class="vm-content">
			<section v-if="activeSection === 'vms'" class="vm-section">
				<vm-setup-screen v-if="!setupReady" @ready="setupReady = true"></vm-setup-screen>
				<vm-list v-else></vm-list>
			</section>

			<section v-if="activeSection === 'networks'" class="vm-section">
				<vm-networks></vm-networks>
			</section>

			<section v-if="activeSection === 'storage'" class="vm-section">
				<vm-storage></vm-storage>
			</section>
		</div>
	</div>
</template>

<script>
import VmSetupScreen from './vm/VmSetupScreen.vue'
import VmList from './vm/VmList.vue'
import VmNetworks from './vm/VmNetworks.vue'
import VmStorage from './vm/VmStorage.vue'
import { vmSidecar } from '@/api/vmSidecar'

export default {
	name: 'vm-manager-app',
	components: { VmSetupScreen, VmList, VmNetworks, VmStorage },
	data() {
		return {
			activeSection: 'vms',
			sections: [
				{ id: 'vms', label: 'VMs', icon: 'display-applications-outline' },
				{ id: 'networks', label: 'Networks', icon: 'network-outline' },
				{ id: 'storage', label: 'Storage', icon: 'storage-outline' }
			],
			setupReady: false
		}
	},
	async created() {
		const status = await vmSidecar.getSetupStatus().catch(() => ({ ready: false }))
		this.setupReady = !!status.ready
	}
}
</script>

<style lang="scss" scoped>
// Opaque white, matching Settings - Files/Terminal keep the dark glass
// chrome, but this app (like Settings) is a plain management panel, not
// a "desktop surface" app, so it uses the same solid look rather than
// DesktopWindow's default translucent/blurred glass background.
.vm-app {
	position: relative;
	display: flex;
	height: 100%;
	background: #f8fafc;
	color: #1e293b;
	font-family: $family-sans-serif;
}

.vm-nav {
	flex-shrink: 0;
	width: 13.5rem;
	padding: 1.25rem 0.75rem;
	background: #ffffff;
	border-right: 1px solid rgba(0, 0, 0, 0.06);
	display: flex;
	flex-direction: column;
	gap: 0.2rem;
	user-select: none;
}

.nav-item {
	display: flex;
	align-items: center;
	gap: 0.75rem;
	border: none;
	background: transparent;
	color: #475569;
	padding: 0.6rem 0.85rem;
	font-size: 0.85rem;
	font-weight: 400;
	border-radius: 9px;
	text-align: left;
	cursor: pointer;
	width: 100%;
	transition: background 0.12s ease, color 0.12s ease;

	.icon {
		color: #94a3b8;
		transition: color 0.12s ease;
		width: 20px;
		height: 20px;
		font-size: 20px;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		flex-shrink: 0;

		i {
			font-size: 20px;
			line-height: 1;
		}
	}

	&:hover {
		background: #f8fafc;
		color: #1e293b;

		.icon {
			color: #1e293b;
		}
	}

	&.active {
		background: #f1f5f9;
		color: #1e293b;
		font-weight: 500;

		.icon {
			color: #2563eb;
		}
	}
}

.vm-content {
	flex: 1 1 auto;
	overflow: auto;
	min-width: 0;
	background: #f8fafc;
}

.vm-section {
	height: 100%;
}
</style>
