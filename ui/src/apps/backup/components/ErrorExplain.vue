<!-- Explains one problem (spec §12.2): the problem, then its cause, then
     one to three direct fixes (errorCodes.js actions). "How to run it" is
     answered in place - it reveals the fix text - everything else is
     emitted as action(name) for the app to carry out. -->
<template>
	<div class="bk-explain">
		<p class="bk-explain-title">
			<b-icon :icon="icon" pack="mdi" custom-size="mdi-18px" :class="'bk-tone-' + tone" aria-hidden="true"></b-icon>
			<span>{{ texts.title }}</span>
		</p>
		<p v-if="texts.cause" class="bk-explain-cause">{{ texts.cause }}</p>
		<p v-if="texts.fix && (showFix || !actions.includes('how_to_run'))" :id="fixId" class="bk-explain-fix">{{ texts.fix }}</p>
		<div v-if="actions.length" class="bk-explain-actions">
			<button
				v-for="(a, i) in actions"
				:key="a"
				type="button"
				class="bk-btn is-small"
				:class="{ 'is-primary': i === 0 }"
				:aria-expanded="a === 'how_to_run' ? (showFix ? 'true' : 'false') : null"
				:aria-controls="a === 'how_to_run' ? fixId : null"
				:disabled="busy"
				@click="onAction(a)"
			>
				{{ $t('backup.action.' + a) }}
			</button>
		</div>
	</div>
</template>

<script>
import { explainError } from '../messages'

let seq = 0

export default {
	name: 'ErrorExplain',
	props: {
		// An error_code (errorCodes.js) ...
		code: { type: String, default: '' },
		// ... or an attention reason (backup.attention.<reason>.*).
		reason: { type: String, default: '' },
		actions: { type: Array, default: () => [] },
		tone: { type: String, default: 'danger' },
		busy: { type: Boolean, default: false }
	},
	data() {
		return { showFix: false, fixId: `bk-fix-${++seq}` }
	},
	computed: {
		texts() {
			if (this.code) return explainError(this.$t.bind(this), this.code)
			const base = `backup.attention.${this.reason}`
			return { title: this.$t(`${base}.title`), cause: this.$t(`${base}.cause`), fix: '' }
		},
		icon() {
			return this.tone === 'danger' ? 'alert-octagon-outline' : 'alert-outline'
		}
	},
	methods: {
		onAction(a) {
			if (a === 'how_to_run') {
				this.showFix = !this.showFix
				return
			}
			this.$emit('action', a)
		}
	}
}
</script>

<style lang="scss" scoped>
.bk-explain {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	min-width: 0;
	p {
		margin: 0;
	}
}
.bk-explain-title {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	font-weight: 600;
	color: var(--theme-text-primary);
}
.bk-explain-cause,
.bk-explain-fix {
	font-size: var(--font-sm);
	color: var(--theme-text-secondary);
	overflow-wrap: anywhere;
}
.bk-explain-fix {
	color: var(--theme-text-primary);
}
.bk-explain-actions {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-2);
	margin-top: var(--space-1);
}
</style>
