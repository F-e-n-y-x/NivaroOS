<template>
	<div class="settings-app">
		<settings-nav :sections="sections" :active-section="activeSection" :compact="compact" @select="activeSection = $event"></settings-nav>

		<div class="settings-main">
			<settings-search :rows="searchRows" @jump="activeSection = $event"></settings-search>

			<div ref="content" class="settings-content" :class="{ 'is-narrow': narrow }">
				<system-section v-if="activeSection === 'system'"></system-section>
				<updates-section v-else-if="activeSection === 'updates'"></updates-section>
				<network-section v-else-if="activeSection === 'network'"></network-section>
				<storage-section v-else-if="activeSection === 'storage'"></storage-section>
				<appearance-section v-else-if="activeSection === 'appearance'"></appearance-section>
				<users-section v-else-if="activeSection === 'users'" :narrow="narrow"></users-section>
			</div>
		</div>
	</div>
</template>

<script>
import SettingsNav from '@/components/settings/SettingsNav.vue'
import SettingsSearch from '@/components/settings/SettingsSearch.vue'
import AppearanceSection, { ROWS as APPEARANCE_ROWS } from '@/components/settings/sections/AppearanceSection.vue'
import UsersSection, { ROWS as USERS_ROWS } from '@/components/settings/sections/UsersSection.vue'
import NetworkSection, { ROWS as NETWORK_ROWS } from '@/components/settings/sections/NetworkSection.vue'
import StorageSection, { ROWS as STORAGE_ROWS } from '@/components/settings/sections/StorageSection.vue'
import SystemSection, { ROWS as SYSTEM_ROWS } from '@/components/settings/sections/SystemSection.vue'
import UpdatesSection, { ROWS as UPDATES_ROWS } from '@/components/settings/sections/UpdatesSection.vue'
import { classifyWidth } from '@/utils/settings/breakpoints'

const SECTIONS = [
	{ id: 'system', label: 'System', icon: 'system-outline', pack: 'casa', color: '#2563eb', bg: 'rgba(37, 99, 235, 0.12)', rows: SYSTEM_ROWS },
	{ id: 'appearance', label: 'Appearance', icon: 'wallpaper-outline', pack: 'casa', color: '#8b5cf6', bg: 'rgba(139, 92, 246, 0.12)', rows: APPEARANCE_ROWS },
	{ id: 'network', label: 'Network & Sharing', icon: 'internet-outline', pack: 'casa', color: '#06b6d4', bg: 'rgba(6, 182, 212, 0.12)', rows: NETWORK_ROWS },
	{ id: 'storage', label: 'Storage', icon: 'storage-other', pack: 'casa', color: '#f59e0b', bg: 'rgba(245, 158, 11, 0.12)', rows: STORAGE_ROWS },
	{ id: 'users', label: 'Users & Access', icon: 'user-edit-outline', pack: 'casa', color: '#f43f5e', bg: 'rgba(244, 63, 94, 0.12)', rows: USERS_ROWS },
	{ id: 'updates', label: 'Updates', icon: 'update', pack: 'mdi', color: '#10b981', bg: 'rgba(16, 185, 129, 0.12)', rows: UPDATES_ROWS }
]

export default {
	name: 'settings-app',
	components: {
		SettingsNav,
		SettingsSearch,
		AppearanceSection,
		UsersSection,
		NetworkSection,
		StorageSection,
		SystemSection,
		UpdatesSection
	},
	props: {
		section: {
			type: String,
			default: ''
		}
	},
	data() {
		return {
			activeSection: this.section || 'system',
			sections: SECTIONS,
			width: 900,
			resizeObserver: null
		}
	},
	watch: {
		section(val) {
			if (val) this.activeSection = val
		}
	},
	computed: {
		breakpoints() {
			return classifyWidth(this.width)
		},
		compact() {
			return this.breakpoints.navCollapsed
		},
		narrow() {
			return this.breakpoints.rowsStacked
		},
		searchRows() {
			return SECTIONS.flatMap(s => s.rows.map(r => ({ sectionId: s.id, sectionLabel: s.label, sectionIcon: s.icon, label: r.label })))
		}
	},
	mounted() {
		this.resizeObserver = new ResizeObserver(entries => {
			this.width = entries[0].contentRect.width
		})
		this.resizeObserver.observe(this.$el)
	},
	beforeDestroy() {
		if (this.resizeObserver) this.resizeObserver.disconnect()
	}
}
</script>

<style lang="scss" scoped>
.settings-app {
	display: flex;
	height: 100%;
	background: #F7F7F7;
	color: #2c3e50;
	font-family: $family-sans-serif;
}

.settings-main {
	flex: 1;
	display: flex;
	flex-direction: column;
	min-width: 0;
	background: #F7F7F7;
}

.settings-content {
	flex: 1;
	overflow-y: auto;
	padding: 1.5rem 2.25rem 3rem;

	&.is-narrow {
		padding: 1rem 1rem 2rem;
	}
}
</style>
