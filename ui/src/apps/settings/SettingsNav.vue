<template>
	<aside class="settings-nav" :class="{ 'is-compact': compact }">
		<nav class="nav-list">
			<button
				v-for="s in sections"
				:key="s.id"
				class="nav-item hover-effect _is-radius"
				:class="{ active: activeSection === s.id }"
				:aria-current="activeSection === s.id ? 'page' : null"
				:aria-label="compact ? $t(s.label) : null"
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
	padding: var(--space-3);
	background: var(--theme-card-subtle, #ffffff);
	border-right: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.06));
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	overflow-y: auto;
	user-select: none;

	&.is-compact {
		width: 4rem;
		align-items: center;
		padding: var(--space-3) var(--space-2);
	}
}

.nav-list {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	width: 100%;
}

.nav-item {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	border: none;
	background: transparent;
	color: var(--theme-text-secondary, #475569);
	padding: var(--space-2) var(--space-3);
	font-size: var(--font-base);
	font-weight: 400;
	border-radius: var(--radius-control);
	text-align: left;
	cursor: pointer;
	width: 100%;
	transition: background 0.12s ease, color 0.12s ease;

	.icon {
		color: var(--theme-text-muted, #94a3b8);
		transition: color 0.12s ease;
		width: 20px;
		height: 20px;
		font-size: var(--font-xl);
		display: inline-flex;
		align-items: center;
		justify-content: center;
		flex-shrink: 0;
		line-height: 1;

		i, .mdi, [class^="casa-"], [class*=" casa-"] {
			font-size: var(--font-xl);
			line-height: 1;
			display: inline-flex;
			align-items: center;
			justify-content: center;
			text-align: center;
			width: 20px;
			height: 20px;

			&::before {
				font-size: var(--font-xl);
				line-height: 1;
				text-align: center;
				vertical-align: middle;
			}
		}
	}

	&:hover {
		background: var(--theme-card-hover, #f8fafc); color: var(--theme-text-primary, #1e293b);

		.icon {
			color: var(--theme-text-primary, #1e293b);
		}
	}

	&.active {
		background: var(--theme-card-bg, #f1f5f9); color: var(--theme-text-primary, #1e293b);
		font-weight: 600;
		// Not colour alone: an accent bar marks the current section.
		box-shadow: inset 3px 0 0 var(--color-primary-fg, #1d4ed8);

		.icon {
			color: var(--color-primary-fg, #1d4ed8);
		}
	}
}

.is-compact .nav-item {
	justify-content: center;
	padding: var(--space-2);
}

.nav-label {
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
	flex: 1;
}
</style>
