<template>
	<div class="settings-app">
		<settings-nav :sections="sections" :active-section="activeSection" :compact="compact" @select="activeSection = $event"></settings-nav>

		<div class="settings-main">
			<settings-search :rows="searchRows" @jump="activeSection = $event"></settings-search>

			<div ref="content" class="settings-content" :class="{ 'is-narrow': narrow }">
				<system-section v-if="activeSection === 'system'"></system-section>
				<packages-section v-else-if="activeSection === 'packages'"></packages-section>
				<containers-section v-else-if="activeSection === 'containers'"></containers-section>
				<scheduled-tasks-section v-else-if="activeSection === 'schedules'"></scheduled-tasks-section>
				<updates-section v-else-if="activeSection === 'updates'"></updates-section>
				<network-section v-else-if="activeSection === 'network'"></network-section>
				<storage-section v-else-if="activeSection === 'storage'"></storage-section>
				<online-accounts-section v-else-if="activeSection === 'cloud'"></online-accounts-section>
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
import OnlineAccountsSection, { ROWS as CLOUD_ROWS } from '@/components/settings/sections/OnlineAccountsSection.vue'
import SystemSection, { ROWS as SYSTEM_ROWS } from '@/components/settings/sections/SystemSection.vue'
import UpdatesSection, { ROWS as UPDATES_ROWS } from '@/components/settings/sections/UpdatesSection.vue'
import PackagesSection, { ROWS as PACKAGES_ROWS } from '@/components/settings/sections/PackagesSection.vue'
import ContainersSection, { ROWS as CONTAINERS_ROWS } from '@/components/settings/sections/ContainersSection.vue'
import ScheduledTasksSection, { ROWS as SCHEDULES_ROWS } from '@/components/settings/sections/ScheduledTasksSection.vue'
import { classifyWidth } from '@/utils/settings/breakpoints'

const SECTIONS = [
	{ id: 'system', label: 'System', icon: 'system-outline', pack: 'casa', color: '#2563eb', bg: 'rgba(37, 99, 235, 0.12)', rows: SYSTEM_ROWS },
	{ id: 'packages', label: 'Package Manager', icon: 'cube-outline', pack: 'mdi', color: '#6366f1', bg: 'rgba(99, 102, 241, 0.12)', rows: PACKAGES_ROWS },
	{ id: 'containers', label: 'Container', icon: 'docker', pack: 'mdi', color: '#0ea5e9', bg: 'rgba(14, 165, 233, 0.12)', rows: CONTAINERS_ROWS },
	{ id: 'schedules', label: 'Scheduled Tasks', icon: 'clock-outline', pack: 'mdi', color: '#8b5cf6', bg: 'rgba(139, 92, 246, 0.12)', rows: SCHEDULES_ROWS },
	{ id: 'appearance', label: 'Appearance', icon: 'wallpaper-outline', pack: 'casa', color: '#a855f7', bg: 'rgba(168, 85, 247, 0.12)', rows: APPEARANCE_ROWS },
	{ id: 'network', label: 'Network & Sharing', icon: 'internet-outline', pack: 'casa', color: '#06b6d4', bg: 'rgba(6, 182, 212, 0.12)', rows: NETWORK_ROWS },
	{ id: 'storage', label: 'Storage', icon: 'storage-other', pack: 'casa', color: '#f59e0b', bg: 'rgba(245, 158, 11, 0.12)', rows: STORAGE_ROWS },
	{ id: 'cloud', label: 'Online Storage', icon: 'cloud-outline', pack: 'mdi', color: '#0891b2', bg: 'rgba(8, 145, 178, 0.12)', rows: CLOUD_ROWS },
	{ id: 'users', label: 'Users & Access', icon: 'user-edit-outline', pack: 'casa', color: '#f43f5e', bg: 'rgba(244, 63, 94, 0.12)', rows: USERS_ROWS },
	{ id: 'updates', label: 'Updates', icon: 'cloud-download-outline', pack: 'mdi', color: '#10b981', bg: 'rgba(16, 185, 129, 0.12)', rows: UPDATES_ROWS }
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
		OnlineAccountsSection,
		SystemSection,
		UpdatesSection,
		PackagesSection,
		ContainersSection,
		ScheduledTasksSection
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
	width: 100%;
	position: relative;
	overflow: hidden;
	background: #ffffff;
	color: #1e293b;
	font-family: $family-sans-serif;
}

.settings-main {
	flex: 1;
	display: flex;
	flex-direction: column;
	min-width: 0;
	background: #ffffff;
}

.settings-content {
	flex: 1;
	overflow-y: auto;
	padding: 1.5rem 2.25rem 3rem;
	background: #ffffff;

	&.is-narrow {
		padding: 1rem 1rem 2rem;
	}
}
</style>
