<!-- Review's "Advanced" (spec §12.4): a <details> with a real summary
     button holding low priority, the maximum run time, include patterns,
     empty folders, an empty source, and success notifications. -->
<template>
	<details class="wz-details advanced-options">
		<summary>{{ $t('backup.wizard.review.advanced') }}</summary>
		<div class="wz-details-body">
			<label class="wz-check">
				<input type="checkbox" :checked="lowPriority" @change="$emit('patch', { lowPriority: $event.target.checked })" />
				<span>{{ $t('backup.wizard.review.low_priority') }}<span class="wz-hint ao-block">{{ $t('backup.wizard.review.low_priority_hint') }}</span></span>
			</label>

			<div class="wz-row">
				<label :for="durationId" class="wz-label">{{ $t('backup.wizard.review.max_duration') }}</label>
				<input :id="durationId" class="wz-number" type="number" min="1" max="720" step="1" inputmode="numeric" :value="maxDurationHours"
					:aria-invalid="errors['options.max_duration_sec'] ? 'true' : 'false'" :aria-describedby="durationDescribedBy"
					@input="$emit('patch', { maxDurationHours: $event.target.value === '' ? '' : Math.round(Number($event.target.value)) })" />
				<span class="wz-hint">{{ $t('backup.wizard.when.hours') }}</span>
			</div>
			<field-error :idp="idp" field="options.max_duration_sec" :errors="errors"></field-error>

			<div class="ao-include">
				<label :for="includeId" class="wz-label">{{ $t('backup.wizard.review.include') }}</label>
				<textarea :id="includeId" class="wz-input ao-textarea" rows="3" spellcheck="false" :value="include.join('\n')"
					:aria-invalid="errors['filters.include'] ? 'true' : 'false'" :aria-describedby="includeDescribedBy"
					@change="$emit('patch', { include: splitLines($event.target.value) })"></textarea>
				<p :id="includeId + '-hint'" class="wz-hint">{{ $t('backup.wizard.review.include_hint') }}</p>
				<field-error :idp="idp" field="filters.include" :errors="errors"></field-error>
			</div>

			<label class="wz-check">
				<input type="checkbox" :checked="copyEmptyDirs" @change="$emit('patch', { copyEmptyDirs: $event.target.checked })" />
				<span>{{ $t('backup.wizard.review.empty_dirs') }}</span>
			</label>
			<label class="wz-check">
				<input type="checkbox" :checked="allowEmptySource" @change="$emit('patch', { allowEmptySource: $event.target.checked })" />
				<span>{{ $t('backup.wizard.review.allow_empty') }}<span class="wz-hint ao-block">{{ $t('backup.wizard.review.allow_empty_hint') }}</span></span>
			</label>
			<label class="wz-check">
				<input type="checkbox" :checked="notifyOnSuccess" @change="$emit('patch', { notifyOnSuccess: $event.target.checked })" />
				<span>{{ $t('backup.wizard.review.notify_success') }}</span>
			</label>
			<label class="wz-check is-disabled">
				<input type="checkbox" checked disabled :aria-describedby="idp + '-notify-fail'" />
				<span>{{ $t('backup.wizard.review.notify_failure') }}<span :id="idp + '-notify-fail'" class="wz-hint ao-block">{{ $t('backup.wizard.review.notify_failure_hint') }}</span></span>
			</label>
		</div>
	</details>
</template>

<script>
import FieldError from './FieldError.vue'
import { fieldId, describedBy } from '../wizard/fields'

export default {
	name: 'AdvancedOptions',
	components: { FieldError },
	props: {
		lowPriority: { type: Boolean, default: true },
		maxDurationHours: { type: [Number, String], default: 24 },
		include: { type: Array, default: () => [] },
		copyEmptyDirs: { type: Boolean, default: false },
		allowEmptySource: { type: Boolean, default: false },
		notifyOnSuccess: { type: Boolean, default: false },
		idp: { type: String, required: true },
		errors: { type: Object, default: () => ({}) }
	},
	computed: {
		durationId() {
			return fieldId(this.idp, 'options.max_duration_sec')
		},
		includeId() {
			return fieldId(this.idp, 'filters.include')
		},
		durationDescribedBy() {
			return describedBy(this.idp, 'options.max_duration_sec', this.errors)
		},
		includeDescribedBy() {
			return describedBy(this.idp, 'filters.include', this.errors, this.includeId + '-hint')
		}
	},
	methods: {
		splitLines(v) {
			return String(v || '').split('\n').map(s => s.trim()).filter(Boolean)
		}
	}
}
</script>

<style lang="scss" scoped>
@import './wizard-form.scss';

.ao-block {
	display: block;
}

.ao-include {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
}

.ao-textarea {
	min-height: 4.5rem;
	padding: var(--space-2) var(--space-3);
	resize: vertical;
	font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}
</style>
