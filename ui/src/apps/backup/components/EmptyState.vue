<!-- First-run state of the Overview (spec §12.2): what a backup is, and
     three ways to start - each opens the wizard (another window). -->
<template>
	<div class="bk-first-run">
		<b-icon icon="backup-restore" pack="mdi" custom-size="mdi-48px" class="bk-first-icon" aria-hidden="true"></b-icon>
		<h2 class="bk-first-title">{{ $t('backup.empty.title') }}</h2>
		<p class="bk-first-text">{{ $t('backup.empty.text') }}</p>
		<div class="bk-first-actions">
			<button type="button" class="bk-start" data-autofocus @click="$emit('start', { preset: presets.photos })">
				<b-icon icon="usb-flash-drive-outline" pack="mdi" custom-size="mdi-24px" aria-hidden="true"></b-icon>
				<span class="bk-start-title">{{ $t('backup.empty.photos') }}</span>
				<span class="bk-start-hint">{{ $t('backup.empty.photos_hint') }}</span>
			</button>
			<button type="button" class="bk-start" @click="$emit('start', { preset: presets.apps })">
				<b-icon icon="shield-check-outline" pack="mdi" custom-size="mdi-24px" aria-hidden="true"></b-icon>
				<span class="bk-start-title">{{ $t('backup.empty.apps') }}</span>
				<span class="bk-start-hint">{{ $t('backup.empty.apps_hint') }}</span>
			</button>
			<button type="button" class="bk-start" @click="$emit('start', {})">
				<b-icon icon="plus-circle-outline" pack="mdi" custom-size="mdi-24px" aria-hidden="true"></b-icon>
				<span class="bk-start-title">{{ $t('backup.empty.scratch') }}</span>
				<span class="bk-start-hint">{{ $t('backup.empty.scratch_hint') }}</span>
			</button>
		</div>
		<backup-vs-sync-explainer></backup-vs-sync-explainer>
	</div>
</template>

<script>
import BackupVsSyncExplainer from './BackupVsSyncExplainer.vue'

// Preset ids of the wizard's presets.js (spec §12.4) these buttons start from.
export const EMPTY_STATE_PRESETS = Object.freeze({ photos: 'photos_usb', apps: 'apps' })

export default {
	name: 'EmptyState',
	components: { BackupVsSyncExplainer },
	data() {
		return { presets: EMPTY_STATE_PRESETS }
	}
}
</script>

<style lang="scss" scoped>
.bk-first-run {
	display: flex;
	flex-direction: column;
	align-items: center;
	gap: var(--space-3);
	padding: var(--space-8) var(--space-4);
	text-align: center;
}
.bk-first-icon {
	color: var(--color-primary-fg);
}
.bk-first-title {
	margin: 0;
	font-size: var(--font-2xl);
	font-weight: 600;
	color: var(--theme-text-primary);
}
.bk-first-text {
	margin: 0;
	max-width: 34rem;
	color: var(--theme-text-secondary);
}
.bk-first-actions {
	display: grid;
	grid-template-columns: repeat(auto-fit, minmax(11rem, 1fr));
	gap: var(--space-3);
	width: 100%;
	max-width: 40rem;
	margin: var(--space-2) 0;
}
.bk-start {
	display: flex;
	flex-direction: column;
	align-items: flex-start;
	gap: var(--space-1);
	min-height: 44px;
	padding: var(--space-4);
	border: 1px solid var(--theme-card-border);
	border-radius: var(--radius-card);
	background: var(--theme-card-bg);
	color: var(--theme-text-primary);
	font-family: inherit;
	text-align: left;
	cursor: pointer;
	transition: border-color 0.15s ease, background 0.15s ease;
	.icon {
		color: var(--color-primary-fg);
	}
	&:hover {
		border-color: var(--color-primary-fg);
		background: var(--theme-card-hover);
	}
}
.bk-start-title {
	font-weight: 600;
}
.bk-start-hint {
	font-size: var(--font-xs);
	color: var(--theme-text-secondary);
}
@media (prefers-reduced-motion: reduce) {
	.bk-start {
		transition: none;
	}
}
</style>
