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
	<nav class="mobile-tab-bar" :aria-label="$t('Main navigation')">
		<button
			v-for="tab in tabs"
			:key="tab.id"
			type="button"
			class="mobile-tab"
			:class="{ active: activeTabId === tab.id }"
			:aria-current="activeTabId === tab.id ? 'page' : null"
			:aria-label="tab.id === 'notifications' && unreadCount > 0 ? $t('Notifications ({count} unread)', { count: unreadCount }) : null"
			@click="selectTab(tab)"
		>
			<span class="mobile-tab-icon">
				<b-icon :icon="tab.id === 'notifications' && unreadCount > 0 ? 'bell-badge-outline' : tab.icon" :pack="tab.pack || 'mdi'" custom-size="mdi-24px"></b-icon>
				<span v-if="tab.id === 'notifications' && unreadCount > 0" class="mobile-tab-badge" aria-hidden="true">{{ unreadCount > 99 ? '99+' : unreadCount }}</span>
			</span>
			<span>{{ $t(tab.label) }}</span>
		</button>
	</nav>
</template>

<script>
import { COMPONENT_REGISTRY } from '@/utils/desktop/windowRegistry'
import { activityService } from '@/service/activity'

// The phone has no tray (NotificationCenter / clock pill), so its
// notification list - with sign-out and restart/shutdown - is a screen
// of its own. Registered here until windowRegistry.js lists it.

const TABS = [
	{ id: 'files', label: 'Files', icon: 'folder-outline', component: 'FilesApp' },
	{ id: 'apps', label: 'Apps', icon: 'apps' },
	{ id: 'vms', label: 'VMs', icon: 'display-applications-outline', pack: 'casa', component: 'VmManagerApp' },
	{ id: 'settings', label: 'Settings', icon: 'cog-outline', component: 'SettingsApp' },
	{ id: 'notifications', label: 'Alerts', icon: 'bell-outline', component: 'NotificationList', props: { isWindow: true, showAccount: true }, titleKey: 'Notifications' }
]

export default {
	name: 'mobile-tab-bar',
	data() {
		return { tabs: TABS, unreadCount: 0 }
	},
	created() {
		this.unreadCount = activityService.getUnreadCount()
		this.unsubscribe = activityService.subscribe(list => {
			this.unreadCount = list.filter(a => !a.read).length
		})
	},
	beforeDestroy() {
		if (this.unsubscribe) this.unsubscribe()
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
				this.$store.commit('OPEN_WINDOW', { id: tab.id, title: this.$t(tab.titleKey || tab.label), component: tab.component, props: tab.props })
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
	// A fixed height MobileScreenHost's screens end above (keep the two
	// --mobile-tab-bar-height fallbacks in step).
	box-sizing: border-box;
	height: calc(var(--mobile-tab-bar-height, 3.5rem) + env(safe-area-inset-bottom, 0px));
}

.mobile-tab {
	flex: 1 1 0;
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	gap: var(--space-1);
	padding: var(--space-2) var(--space-1) var(--space-1);
	border: none;
	background: transparent;
	color: var(--theme-text-muted, rgba(44, 62, 80, 0.5));
	font-size: var(--font-2xs);
	cursor: pointer;

	&.active {
		color: var(--color-primary-fg, var(--color-primary, #2563eb));
	}

	&:focus-visible {
		outline: 2px solid var(--theme-focus-ring, var(--color-primary));
		outline-offset: -2px;
	}

	&:active {
		opacity: 0.7;
	}
}
.mobile-tab-icon {
	position: relative;
	display: inline-flex;
}

.mobile-tab-badge {
	position: absolute;
	top: -4px;
	right: -10px;
	min-width: 16px;
	height: 16px;
	padding: 0 var(--space-1);
	border-radius: var(--radius-pill);
	background: var(--color-danger);
	color: #ffffff;
	font-size: var(--font-2xs);
	font-weight: 700;
	line-height: 16px;
	text-align: center;
}
</style>
