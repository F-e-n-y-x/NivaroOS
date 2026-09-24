<!-- "Backup vs sync?" - a plain-language, inline explainer (a real
     <details>/<summary>, keyboard-operable without script). -->
<template>
	<details class="bk-explainer">
		<summary>
			<b-icon icon="help-circle-outline" pack="mdi" custom-size="mdi-18px" aria-hidden="true"></b-icon>
			<span>{{ $t('backup.explain.summary') }}</span>
		</summary>
		<dl>
			<div v-for="type in types" :key="type">
				<dt>{{ $t(`backup.type.${type}.label`) }}</dt>
				<dd>{{ $t(`backup.type.${type}.promise`) }} {{ $t(`backup.type.${type}.deleted`) }}</dd>
			</div>
		</dl>
		<p class="bk-secondary">{{ $t('backup.explain.sync_note') }}</p>
	</details>
</template>

<script>
export default {
	name: 'BackupVsSyncExplainer',
	data() {
		return { types: ['copy', 'mirror', 'archive'] }
	}
}
</script>

<style lang="scss" scoped>
.bk-explainer {
	width: 100%;
	max-width: 36rem;
	text-align: left;
	border: 1px solid var(--theme-card-border);
	border-radius: var(--radius-card);
	background: var(--theme-card-bg);
	summary {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		min-height: 44px;
		padding: var(--space-2) var(--space-3);
		cursor: pointer;
		color: var(--color-primary-fg);
		font-weight: 500;
		border-radius: var(--radius-card);
	}
	dl,
	p {
		margin: 0;
		padding: 0 var(--space-4) var(--space-3);
		font-size: var(--font-sm);
	}
	dl > div {
		padding: var(--space-1) 0;
	}
	dt {
		font-weight: 600;
		color: var(--theme-text-primary);
	}
	dd {
		margin: 0;
		color: var(--theme-text-secondary);
	}
}
</style>
