<template>
	<div class="vm-app" ref="root" :class="{ 'nav-collapsed': navCollapsed }">
		<aside class="vm-nav">
			<button v-for="s in sections" :key="s.id" class="nav-item hover-effect _is-radius"
				:class="{ active: activeSection === s.id }" :title="navCollapsed ? $t(s.label) : ''" @click="activeSection = s.id">
				<b-icon :icon="s.icon" :pack="s.pack || 'casa'" size="is-20"></b-icon>
				<span>{{ $t(s.label) }}</span>
			</button>
		</aside>

		<div class="vm-content">
			<section v-if="activeSection === 'vms'" class="vm-section">
				<vm-setup-screen v-if="!setupReady" @ready="setupReady = true"></vm-setup-screen>
				<vm-list v-else @open-snapshots="handleOpenSnapshots"></vm-list>
			</section>

			<section v-if="activeSection === 'snapshots'" class="vm-section">
				<vm-snapshots :initial-vm="targetSnapshotVm"></vm-snapshots>
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
import VmSnapshots from './vm/VmSnapshots.vue'
import VmNetworks from './vm/VmNetworks.vue'
import VmStorage from './vm/VmStorage.vue'
import { vmSidecar } from '@/api/vmSidecar'

// Below this width, the 13.5rem-wide labeled nav would leave barely any
// room at all for content (VM cards, snapshot lists, etc.) - collapsing it
// to icon-only mirrors Settings' own nav-collapse threshold/behavior
// (see utils/settings/breakpoints.js), just via a plain ResizeObserver
// here rather than a shared breakpoints module, since this is the only
// place in this app that needs one.
const NAV_COLLAPSE_WIDTH = 700

export default {
	name: 'vm-manager-app',
	components: { VmSetupScreen, VmList, VmSnapshots, VmNetworks, VmStorage },
	data() {
		return {
			activeSection: 'vms',
			targetSnapshotVm: '',
			sections: [
				{ id: 'vms', label: 'VMs', icon: 'display-applications-outline' },
				{ id: 'snapshots', label: 'Snapshots', icon: 'camera-outline', pack: 'mdi' },
				{ id: 'networks', label: 'Networks', icon: 'network-outline' },
				{ id: 'storage', label: 'Storage', icon: 'storage-outline' }
			],
			setupReady: false,
			navCollapsed: false
		}
	},
	async created() {
		const status = await vmSidecar.getSetupStatus().catch(() => ({ ready: false }))
		this.setupReady = !!status.ready
	},
	mounted() {
		this.resizeObserver = new ResizeObserver(entries => {
			const width = entries[0].contentRect.width
			this.navCollapsed = width < NAV_COLLAPSE_WIDTH
		})
		this.resizeObserver.observe(this.$refs.root)
	},
	beforeDestroy() {
		if (this.resizeObserver) this.resizeObserver.disconnect()
	},
	methods: {
		handleOpenSnapshots(vmName) {
			this.targetSnapshotVm = vmName || ''
			this.activeSection = 'snapshots'
		}
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
	background: var(--theme-bg-window, #f8fafc);
	color: var(--theme-text-primary, #1e293b);
	font-family: $family-sans-serif;
}

.vm-nav {
	flex-shrink: 0;
	width: 13.5rem;
	padding: 1.25rem 0.75rem;
	background: var(--theme-card-bg, #ffffff);
	border-right: 1px solid rgba(0, 0, 0, 0.06);
	display: flex;
	flex-direction: column;
	gap: 0.2rem;
	user-select: none;
}

.nav-collapsed .vm-nav {
	width: 3.75rem;
	padding: 1.25rem 0.5rem;

	.nav-item span {
		display: none;
	}

	.nav-item {
		justify-content: center;
		padding: 0.6rem;
	}
}

.nav-item {
	display: flex;
	align-items: center;
	gap: 0.75rem;
	border: none;
	background: transparent;
	color: var(--theme-text-secondary, #475569);
	padding: 0.6rem 0.85rem;
	font-size: 0.85rem;
	font-weight: 400;
	border-radius: 9px;
	text-align: left;
	cursor: pointer;
	width: 100%;
	transition: background 0.12s ease, color 0.12s ease;

	.icon {
		color: var(--theme-text-muted, #94a3b8);
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
		background: var(--theme-bg-window, #f8fafc);
		color: var(--theme-text-primary, #1e293b);

		.icon {
			color: var(--theme-text-primary, #1e293b);
		}
	}

	&.active {
		background: var(--theme-card-subtle, #f1f5f9);
		color: var(--theme-text-primary, #1e293b);
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
	background: var(--theme-bg-window, #f8fafc);
}

.vm-section {
	height: 100%;
}
</style>
