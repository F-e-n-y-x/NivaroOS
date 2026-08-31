<template>
	<aside class="settings-nav" :class="{ 'is-compact': compact }">
		<div v-if="!compact" class="nav-header">
			<span class="nav-title">{{ $t('Settings') }}</span>
		</div>
		<nav class="nav-list">
			<button
				v-for="s in sections"
				:key="s.id"
				class="nav-item"
				:class="{ active: activeSection === s.id }"
				:title="compact ? $t(s.label) : null"
				@click="$emit('select', s.id)"
			>
				<div class="icon-badge" :style="{ '--icon-color': s.color || '#2563eb', '--icon-bg': s.bg || 'rgba(37, 99, 235, 0.12)' }">
					<b-icon :icon="s.icon" :pack="s.pack || 'casa'" size="is-18"></b-icon>
				</div>
				<span v-if="!compact" class="nav-label">{{ $t(s.label) }}</span>
			</button>
		</nav>
	</aside>
</template>

<script>
export default {
	name: 'settings-nav',
	props: {
		sections: { type: Array, required: true },
		activeSection: { type: String, required: true },
		compact: { type: Boolean, default: false }
	}
}
</script>

<style lang="scss" scoped>
.settings-nav {
	flex-shrink: 0;
	width: 14rem;
	padding: 1.25rem 0.85rem;
	background: rgba(255, 255, 255, 0.65);
	backdrop-filter: blur(24px) saturate(180%);
	-webkit-backdrop-filter: blur(24px) saturate(180%);
	border-right: 1px solid rgba(0, 0, 0, 0.07);
	display: flex;
	flex-direction: column;
	gap: 0.75rem;
	overflow-y: auto;
	user-select: none;

	&.is-compact {
		width: 4.5rem;
		align-items: center;
		padding: 1.25rem 0.4rem;
	}
}

.nav-header {
	padding: 0.25rem 0.65rem 0.5rem;
}

.nav-title {
	font-size: 1.15rem;
	font-weight: 700;
	letter-spacing: -0.02em;
	color: #0f172a;
}

.nav-list {
	display: flex;
	flex-direction: column;
	gap: 0.25rem;
	width: 100%;
}

.nav-item {
	position: relative;
	display: flex;
	align-items: center;
	gap: 0.75rem;
	border: none;
	background: transparent;
	color: #475569;
	padding: 0.55rem 0.75rem;
	font-size: 0.875rem;
	font-weight: 500;
	border-radius: 10px;
	text-align: left;
	cursor: pointer;
	width: 100%;
	transition: all 0.15s cubic-bezier(0.16, 1, 0.3, 1);

	.icon-badge {
		flex-shrink: 0;
		width: 28px;
		height: 28px;
		display: flex;
		align-items: center;
		justify-content: center;
		border-radius: 7px;
		background: var(--icon-bg);
		color: var(--icon-color);
		transition: all 0.15s ease;
	}

	&:hover {
		background: rgba(0, 0, 0, 0.04);
		color: #0f172a;
	}

	&.active {
		background: #ffffff;
		color: #0f172a;
		font-weight: 600;
		box-shadow: 0 2px 8px rgba(0, 0, 0, 0.06), 0 1px 2px rgba(0, 0, 0, 0.04);

		.icon-badge {
			transform: scale(1.05);
		}
	}
}

.is-compact .nav-item {
	justify-content: center;
	padding: 0.6rem 0.4rem;
}

.nav-label {
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
	flex: 1;
}
</style>
