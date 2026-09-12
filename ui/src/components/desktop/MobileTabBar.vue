<!--
	The mobile "app shell" equivalent of Dock.vue - a fixed bottom tab bar
	instead of a floating launcher. Only 3 of this app's ~40 possible
	windows get a permanent tab (Files/VMs/Settings, the most-reached-for
	destinations); everything else (Terminal, App Store, installed
	containers, any window pushed on top of one of these) is reached via
	the "Apps" tab's icon grid instead, the same way a phone's home screen
	is where you go for anything not already pinned to your own dock.
-->
<template>
	<nav class="mobile-tab-bar">
		<button
			v-for="tab in tabs"
			:key="tab.id"
			type="button"
			class="mobile-tab"
			:class="{ active: activeTabId === tab.id }"
			@click="selectTab(tab)"
		>
			<b-icon :icon="tab.icon" :pack="tab.pack || 'mdi'" custom-size="mdi-24px"></b-icon>
			<span>{{ $t(tab.label) }}</span>
		</button>
	</nav>
</template>

<script>
const TABS = [
	{ id: 'files', label: 'Files', icon: 'folder-outline', component: 'FilesApp' },
	{ id: 'apps', label: 'Apps', icon: 'apps' },
	{ id: 'vms', label: 'VMs', icon: 'display-applications-outline', pack: 'casa', component: 'VmManagerApp' },
	{ id: 'settings', label: 'Settings', icon: 'cog-outline', component: 'SettingsApp' }
]

export default {
	name: 'mobile-tab-bar',
	data() {
		return { tabs: TABS }
	},
	computed: {
		windows() {
			return this.$store.state.windows
		},
		activeWindow() {
			let top = null
			for (const w of this.windows) {
				if (w.minimized) continue
				if (!top || w.zIndex > top.zIndex) top = w
			}
			return top
		},
		activeTabId() {
			if (!this.activeWindow) return 'apps'
			const match = this.tabs.find(t => t.id === this.activeWindow.id)
			return match ? match.id : ''
		}
	},
	methods: {
		selectTab(tab) {
			if (tab.id === 'apps') {
				// Matches a phone's Home button: every open screen just stops
				// being the visible one (state, live sessions, etc. all
				// survive) rather than actually closing anything.
				this.windows.forEach(w => {
					if (!w.minimized) this.$store.commit('TOGGLE_MINIMIZE_WINDOW', w.id)
				})
				return
			}
			const existing = this.windows.find(w => w.id === tab.id)
			if (existing) {
				this.$store.commit('FOCUS_WINDOW', tab.id)
			} else {
				this.$store.commit('OPEN_WINDOW', { id: tab.id, title: this.$t(tab.label), component: tab.component })
			}
		}
	}
}
</script>

<style lang="scss" scoped>
.mobile-tab-bar {
	position: fixed;
	left: 0;
	right: 0;
	bottom: 0;
	z-index: 99990;
	display: flex;
	align-items: stretch;
	background: var(--theme-menu-bg, rgba(255, 255, 255, 0.96));
	backdrop-filter: blur(12px);
	-webkit-backdrop-filter: blur(12px);
	border-top: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.08));
	// The home indicator / gesture-nav area on real phones sits below this -
	// this keeps tab labels from ever being crowded against the very edge.
	padding-bottom: env(safe-area-inset-bottom, 0);
}

.mobile-tab {
	flex: 1 1 0;
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	gap: 0.15rem;
	padding: 0.4rem 0.25rem 0.35rem;
	border: none;
	background: transparent;
	color: var(--theme-text-muted, rgba(44, 62, 80, 0.5));
	font-size: 0.68rem;
	cursor: pointer;

	&.active {
		color: var(--color-primary, #2563eb);
	}

	&:active {
		opacity: 0.7;
	}
}
</style>
