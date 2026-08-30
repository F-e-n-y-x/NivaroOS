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
	background: #fff;
	color: #2c3e50;
	font-family: $family-sans-serif;
}

.vm-nav {
	flex-shrink: 0;
	width: 13rem;
	padding: 1rem 0.6rem;
	background: rgba(0, 0, 0, 0.015);
	border-right: 1px solid rgb(228 233 237);
	display: flex;
	flex-direction: column;
	gap: 0.15rem;
}

.nav-item {
	display: flex;
	align-items: center;
	gap: 0.6rem;
	border: none;
	background: transparent;
	color: inherit;
	padding: 0.55rem 0.75rem;
	font-size: 0.85rem;
	text-align: left;
	cursor: pointer;
	width: 100%;

	.icon {
		color: hsla(208, 16%, 42%, 1);
	}

	&.active {
		background: hsla(208, 100%, 96%, 1);
		color: hsla(208, 100%, 45%, 1);
		font-weight: 600;

		.icon {
			color: hsla(208, 100%, 45%, 1);
		}
	}
}

.vm-content {
	flex: 1 1 auto;
	overflow: auto;
	min-width: 0;
}

.vm-section {
	height: 100%;
}
</style>
