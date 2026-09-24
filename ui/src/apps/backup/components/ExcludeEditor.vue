<!-- "Skip" (spec §12.4 What): named sets of files not worth backing up
     (cache, trash, temp...) as toggle chips, a size limit and custom
     patterns. Patterns are checked by the server's POST /validate as you
     type (the wizard shows its field_errors here). -->
<template>
	<div class="exclude-editor">
		<div class="ex-chips" role="group" :aria-label="$t('backup.wizard.what.skip_label')">
			<button v-for="p in presets" :key="p" type="button" class="ex-chip" :aria-pressed="excludePresets.includes(p) ? 'true' : 'false'" @click="togglePreset(p)">
				<b-icon :icon="excludePresets.includes(p) ? 'check' : 'plus'" custom-size="mdi-14px" aria-hidden="true"></b-icon>
				<span>{{ $t('backup.exclude.' + p) }}</span>
			</button>
		</div>

		<div class="wz-row">
			<label class="wz-check">
				<input type="checkbox" :checked="maxSizeOn" @change="$emit('patch', { maxSizeOn: $event.target.checked })" />
				<span>{{ $t('backup.wizard.what.skip_larger') }}</span>
			</label>
			<label :for="sizeId" class="sr-only">{{ $t('backup.wizard.what.size_gb') }}</label>
			<input :id="sizeId" class="wz-number" type="number" min="0.1" step="0.1" inputmode="decimal" :value="maxSizeGb" :disabled="!maxSizeOn"
				:aria-invalid="errors['filters.max_size_bytes'] ? 'true' : 'false'" :aria-describedby="sizeDescribedBy"
				@input="$emit('patch', { maxSizeGb: $event.target.value === '' ? '' : Number($event.target.value) })" />
			<span class="wz-hint" aria-hidden="true">{{ $t('backup.wizard.what.gb') }}</span>
		</div>
		<field-error :idp="idp" field="filters.max_size_bytes" :errors="errors"></field-error>

		<div class="ex-patterns">
			<ul v-if="exclude.length" class="ex-list" :aria-label="$t('backup.wizard.what.patterns_label')">
				<li v-for="(p, i) in exclude" :key="i + p" class="ex-pattern">
					<code>{{ p }}</code>
					<button type="button" class="ex-remove" :aria-label="$t('backup.wizard.what.remove_pattern', { pattern: p })"
						:title="$t('backup.wizard.what.remove_pattern', { pattern: p })" @click="removePattern(i)">
						<b-icon icon="close" custom-size="mdi-14px" aria-hidden="true"></b-icon>
					</button>
				</li>
			</ul>
			<div class="wz-row">
				<label :for="patternId" class="sr-only">{{ $t('backup.wizard.what.pattern_input') }}</label>
				<input :id="patternId" v-model="newPattern" class="wz-input ex-input" type="text" spellcheck="false" autocomplete="off" autocapitalize="off"
					:placeholder="$t('backup.wizard.what.pattern_placeholder')" :aria-invalid="errors['filters.exclude'] ? 'true' : 'false'"
					:aria-describedby="patternDescribedBy" @keydown.enter.prevent="addPattern" />
				<button type="button" class="wz-secondary" :disabled="!newPattern.trim()" @click="addPattern">{{ $t('backup.wizard.what.add_pattern') }}</button>
			</div>
			<p :id="patternId + '-hint'" class="wz-hint">{{ $t('backup.wizard.what.pattern_hint') }}</p>
			<field-error :idp="idp" field="filters.exclude" :errors="errors"></field-error>
		</div>
	</div>
</template>

<script>
import FieldError from './FieldError.vue'
import { fieldId, describedBy } from '../wizard/fields'

export default {
	name: 'ExcludeEditor',
	components: { FieldError },
	props: {
		excludePresets: { type: Array, required: true },
		exclude: { type: Array, required: true },
		maxSizeOn: { type: Boolean, default: false },
		maxSizeGb: { type: [Number, String], default: 4 },
		idp: { type: String, required: true },
		errors: { type: Object, default: () => ({}) }
	},
	data() {
		return {
			presets: ['caches', 'trash', 'temp', 'thumbs', 'node_modules'],
			newPattern: ''
		}
	},
	computed: {
		sizeId() {
			return fieldId(this.idp, 'filters.max_size_bytes')
		},
		patternId() {
			return fieldId(this.idp, 'filters.exclude')
		},
		sizeDescribedBy() {
			return describedBy(this.idp, 'filters.max_size_bytes', this.errors)
		},
		patternDescribedBy() {
			return describedBy(this.idp, 'filters.exclude', this.errors, this.patternId + '-hint')
		}
	},
	methods: {
		togglePreset(p) {
			const set = this.excludePresets.includes(p) ? this.excludePresets.filter(x => x !== p) : [...this.excludePresets, p]
			// Keep the canonical order.
			this.$emit('patch', { excludePresets: this.presets.filter(x => set.includes(x)).concat(set.filter(x => !this.presets.includes(x))) })
		},
		addPattern() {
			const p = this.newPattern.trim()
			if (!p) return
			if (!this.exclude.includes(p)) this.$emit('patch', { exclude: [...this.exclude, p] })
			this.newPattern = ''
		},
		removePattern(i) {
			this.$emit('patch', { exclude: this.exclude.filter((_, j) => j !== i) })
		}
	}
}
</script>

<style lang="scss" scoped>
@import './wizard-form.scss';

.exclude-editor {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	min-width: 0;
}

.ex-chips {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-1);
}

.ex-chip {
	display: inline-flex;
	align-items: center;
	gap: var(--space-1);
	min-height: 2rem;
	padding: 0 var(--space-3);
	border: 1px solid var(--theme-input-border);
	border-radius: var(--radius-pill);
	background: var(--theme-card-bg);
	color: var(--theme-text-primary);
	font-family: inherit;
	font-size: var(--font-xs);
	cursor: pointer;

	&[aria-pressed='true'] {
		border-color: var(--color-primary-fg);
		background: var(--theme-card-selected);
		color: var(--color-primary-fg);
		font-weight: 600;
	}
	&:focus-visible {
		@include picker-focus-ring;
	}
	@media (pointer: coarse) {
		min-height: 44px;
	}
}

.ex-patterns {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
}

.ex-list {
	display: flex;
	flex-wrap: wrap;
	gap: var(--space-1);
	margin: 0;
	padding: 0;
	list-style: none;
}

.ex-pattern {
	display: inline-flex;
	align-items: center;
	gap: var(--space-1);
	max-width: 100%;
	padding: 0.1rem 0.1rem 0.1rem var(--space-2);
	border: 1px solid var(--theme-card-border);
	border-radius: var(--radius-sm);
	background: var(--theme-card-subtle);

	code {
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		background: transparent;
		color: var(--theme-text-primary);
		font-size: var(--font-xs);
		padding: 0;
	}
}

.ex-remove {
	display: inline-flex;
	align-items: center;
	justify-content: center;
	width: 1.75rem;
	height: 1.75rem;
	border: none;
	border-radius: var(--radius-xs);
	background: transparent;
	color: var(--theme-text-secondary);
	cursor: pointer;

	&:hover {
		background: var(--theme-card-hover);
		color: var(--theme-text-primary);
	}
	&:focus-visible {
		@include picker-focus-ring;
		outline-offset: 0;
	}
	@media (pointer: coarse) {
		width: 44px;
		height: 44px;
	}
}

.ex-input {
	flex: 1 1 12rem;
	font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}
</style>
