<template>
	<aside class="settings-nav" :class="{ 'is-compact': compact }">
		<div v-if="!compact" class="nav-header">
			<span class="nav-title">{{ $t('Settings') }}</span>
		</div>
		<nav class="nav-list">
			<button
				v-for="s in sections"
				:key="s.id"
				class="nav-item hover-effect _is-radius"
				:class="{ active: activeSection === s.id }"
				:title="compact ? $t(s.label) : null"
				@click="$emit('select', s.id)"
			>
				<b-icon :icon="s.icon" :pack="s.pack || 'casa'" size="is-20"></b-icon>
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
	width: 13.5rem;
	padding: 1.25rem 0.75rem;
	background: rgba(0, 0, 0, 0.015);
	border-right: 1px solid rgba(0, 0, 0, 0.06);
	display: flex;
	flex-direction: column;
	gap: 0.35rem;
	overflow-y: auto;
	user-select: none;

	&.is-compact {
		width: 4rem;
		align-items: center;
		padding: 1.25rem 0.4rem;
	}
}

.nav-header {
	padding: 0.25rem 0.65rem 0.4rem;
}

.nav-title {
	font-size: 0.7rem;
	font-weight: 700;
	letter-spacing: 0.05em;
	text-transform: uppercase;
	color: rgba(0, 0, 0, 0.4);
}

.nav-list {
	display: flex;
	flex-direction: column;
	gap: 0.2rem;
	width: 100%;
}

.nav-item {
	display: flex;
	align-items: center;
	gap: 0.75rem;
	border: none;
	background: transparent;
	color: rgba(44, 62, 80, 0.75);
	padding: 0.6rem 0.85rem;
	font-size: 0.85rem;
	font-weight: 500;
	border-radius: 9px;
	text-align: left;
	cursor: pointer;
	width: 100%;
	transition: background 0.12s ease, color 0.12s ease;

	.icon {
		color: rgba(44, 62, 80, 0.5);
		transition: color 0.12s ease;
	}

	&:hover {
		background: rgba(0, 0, 0, 0.04);
		color: #2c3e50;

		.icon {
			color: #2c3e50;
		}
	}

	&.active {
		background: #ffffff;
		color: #2c3e50;
		font-weight: 600;
		box-shadow: 0 1px 3px rgba(0, 0, 0, 0.08);

		.icon {
			color: #2563eb;
		}
	}
}

.is-compact .nav-item {
	justify-content: center;
	padding: 0.6rem;
}

.nav-label {
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
	flex: 1;
}
</style>
