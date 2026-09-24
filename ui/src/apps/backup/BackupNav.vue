<!-- Backup & Sync sections (spec §12.1), styled like SettingsNav: a
     sidebar when the window is wide, a collapsible icon rail on tablet
     widths, and a bottom tab bar on phones (< 480 px). One <nav> with
     aria-current in every layout, so assistive tech sees the same thing. -->
<template>
	<nav class="bk-nav" :class="['is-' + layout, { 'is-expanded': layout === 'rail' && expanded }]" :aria-label="$t('backup.nav.label')">
		<button
			v-if="layout === 'rail'"
			type="button"
			class="bk-nav-toggle"
			:aria-expanded="expanded ? 'true' : 'false'"
			:aria-label="expanded ? $t('backup.nav.collapse') : $t('backup.nav.expand')"
			:title="expanded ? $t('backup.nav.collapse') : $t('backup.nav.expand')"
			@click="expanded = !expanded"
		>
			<b-icon :icon="expanded ? 'chevron-double-left' : 'chevron-double-right'" pack="mdi" custom-size="mdi-20px"></b-icon>
		</button>
		<button
			v-for="s in items"
			:key="s.id"
			type="button"
			class="bk-nav-item"
			:class="{ active: active === s.id }"
			:aria-current="active === s.id ? 'page' : null"
			:aria-label="showLabels ? null : labelWithBadge(s)"
			:title="showLabels ? null : labelWithBadge(s)"
			@click="$emit('select', s.id)"
		>
			<span class="bk-nav-icon">
				<b-icon :icon="s.icon" pack="mdi" custom-size="mdi-20px"></b-icon>
				<span v-if="badges[s.id]" class="bk-nav-badge" :class="'is-' + badgeKind(s.id)" aria-hidden="true">{{ badges[s.id] }}</span>
			</span>
			<span v-if="showLabels" class="bk-nav-label">{{ $t('backup.nav.' + s.id) }}</span>
			<span v-if="showLabels && badges[s.id]" class="bk-sr-only">{{ badgeText(s.id) }}</span>
		</button>
	</nav>
</template>

<script>
import { BACKUP_SECTIONS } from './windows'

const ICONS = { overview: 'view-dashboard-outline', jobs: 'format-list-checks', restore: 'backup-restore', activity: 'history', settings: 'cog-outline' }

// What a section's badge counts: jobs that need the user (red), or runs
// in progress (Activity: informational, not a problem).
const BADGE_KIND = { activity: 'running' }
const BADGE_KEY = { attention: 'backup.nav.badge', running: 'backup.nav.badge_running' }

export default {
	name: 'BackupNav',
	props: {
		active: { type: String, required: true },
		// side | rail | tabs
		layout: { type: String, default: 'side' },
		// { sectionId: count } shown as a badge (e.g. jobs needing attention)
		badges: { type: Object, default: () => ({}) }
	},
	data() {
		return { expanded: false }
	},
	computed: {
		items() {
			return BACKUP_SECTIONS.map(id => ({ id, icon: ICONS[id] }))
		},
		showLabels() {
			return this.layout !== 'rail' || this.expanded
		}
	},
	methods: {
		badgeKind(id) {
			return BADGE_KIND[id] || 'attention'
		},
		badgeText(id) {
			return this.$t(BADGE_KEY[this.badgeKind(id)], { count: this.badges[id] })
		},
		labelWithBadge(s) {
			const label = this.$t('backup.nav.' + s.id)
			return this.badges[s.id] ? `${label} (${this.badgeText(s.id)})` : label
		}
	}
}
</script>

<style lang="scss" scoped>
.bk-nav {
	flex-shrink: 0;
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	padding: var(--space-3);
	background: var(--theme-card-subtle);
	border-right: 1px solid var(--theme-card-border);
	overflow-y: auto;
	user-select: none;

	&.is-side {
		width: 12.5rem;
	}
	&.is-rail {
		width: 4rem;
		align-items: center;
		padding: var(--space-3) var(--space-2);
		&.is-expanded {
			width: 12.5rem;
			align-items: stretch;
		}
	}
	&.is-tabs {
		order: 2;
		flex-direction: row;
		justify-content: space-around;
		width: 100%;
		padding: var(--space-1) var(--space-1) calc(var(--space-1) + env(safe-area-inset-bottom));
		border-right: none;
		border-top: 1px solid var(--theme-card-border);
		overflow: visible;
	}
}

.bk-nav-toggle,
.bk-nav-item {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	border: none;
	background: transparent;
	color: var(--theme-text-secondary);
	padding: var(--space-2) var(--space-3);
	font-family: inherit;
	font-size: var(--font-base);
	border-radius: var(--radius-control);
	text-align: left;
	cursor: pointer;
	width: 100%;
	min-height: 2.25rem;
	transition: background 0.12s ease, color 0.12s ease;

	&:hover {
		background: var(--theme-card-hover);
		color: var(--theme-text-primary);
	}
	&:focus {
		outline: none;
	}
	&:focus-visible {
		outline: 2px solid var(--color-primary-fg);
		outline-offset: 1px;
	}
}

.bk-nav-toggle {
	justify-content: center;
	color: var(--theme-text-muted);
}

.bk-nav-item.active {
	background: var(--theme-card-selected);
	color: var(--color-primary-fg);
	font-weight: 500;
}

.is-rail:not(.is-expanded) .bk-nav-item {
	justify-content: center;
	padding: var(--space-2);
	min-height: 44px;
}

.is-tabs .bk-nav-item {
	flex-direction: column;
	gap: 0.125rem;
	justify-content: center;
	padding: var(--space-1);
	min-height: 48px;
	font-size: var(--font-2xs);
	text-align: center;
	.bk-nav-label {
		overflow: hidden;
		text-overflow: ellipsis;
		max-width: 100%;
	}
}

.bk-nav-icon {
	position: relative;
	display: inline-flex;
}

.bk-nav-badge {
	position: absolute;
	top: -0.375rem;
	right: -0.5rem;
	min-width: 1rem;
	height: 1rem;
	padding: 0 0.25rem;
	border-radius: var(--radius-pill);
	background: var(--status-danger-fg);
	color: var(--theme-card-bg);
	font-size: var(--font-2xs);
	font-weight: 600;
	line-height: 1rem;
	text-align: center;

	&.is-running {
		background: var(--status-info-fg);
	}
}

.bk-nav-label {
	white-space: nowrap;
}

@media (prefers-reduced-motion: reduce) {
	.bk-nav-item {
		transition: none;
	}
}
</style>
