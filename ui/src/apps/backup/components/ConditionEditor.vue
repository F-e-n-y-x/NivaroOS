<!-- "Only run when…" (spec §12.4 When, §7.4): the destination must be
     available (always, and locked, for a mirror), what to do when it isn't
     (skip, wait, fail), and an optional time window in server time that
     may wrap midnight. -->
<template>
	<div class="condition-editor">
		<label class="wz-check" :class="{ 'is-disabled': isMirror }">
			<input type="checkbox" :checked="isMirror || destAvailable" :disabled="isMirror" :aria-describedby="isMirror ? idp + '-mirror-lock' : null"
				@change="$emit('patch', { destAvailable: $event.target.checked })" />
			<span>
				{{ $t('backup.wizard.when.dest_available') }}
				<span v-if="isMirror" :id="idp + '-mirror-lock'" class="ce-lock">
					<b-icon icon="lock-outline" custom-size="mdi-14px" aria-hidden="true"></b-icon>{{ $t('backup.wizard.when.mirror_locked') }}
				</span>
			</span>
		</label>

		<div class="wz-row ce-indent">
			<label :for="unmetId" class="wz-label">{{ $t('backup.wizard.when.if_not') }}</label>
			<select :id="unmetId" class="wz-select" :value="effectiveUnmet" @change="$emit('patch', { whenUnmet: $event.target.value })">
				<option value="skip">{{ $t('backup.wizard.when.unmet.skip') }}</option>
				<option value="wait">{{ $t('backup.wizard.when.unmet.wait') }}</option>
				<option value="fail">{{ $t('backup.wizard.when.unmet.fail') }}</option>
			</select>
			<template v-if="effectiveUnmet === 'wait'">
				<label :for="waitId" class="wz-label">{{ $t('backup.wizard.when.wait_up_to') }}</label>
				<select :id="waitId" class="wz-select" :value="waitHours" :aria-describedby="waitDescribedBy" @change="$emit('patch', { waitMaxMin: Number($event.target.value) * 60 })">
					<option v-for="h in waitChoices" :key="h" :value="h">{{ $t('backup.wizard.when.n_hours', { n: h }) }}</option>
				</select>
			</template>
		</div>
		<p class="wz-hint ce-indent">{{ $t('backup.wizard.when.unmet_hint.' + effectiveUnmet) }}</p>
		<field-error class="ce-indent" :idp="idp" field="wait_max_min" :errors="errors"></field-error>

		<label class="wz-check">
			<input type="checkbox" :checked="windowOn" @change="$emit('patch', { windowOn: $event.target.checked })" />
			<span>{{ $t('backup.wizard.when.window') }}</span>
		</label>
		<div v-if="windowOn" class="ce-indent">
			<div class="wz-row">
				<label :for="startId" class="wz-label">{{ $t('backup.wizard.when.window_from') }}</label>
				<input :id="startId" class="wz-input ce-time" type="time" step="60" required :value="windowStart"
					:aria-invalid="errors['window.start'] ? 'true' : 'false'" :aria-describedby="startDescribedBy" @change="$emit('patch', { windowStart: $event.target.value })" />
				<label :for="endId" class="wz-label">{{ $t('backup.wizard.when.window_to') }}</label>
				<input :id="endId" class="wz-input ce-time" type="time" step="60" required :value="windowEnd"
					:aria-invalid="errors['window.end'] ? 'true' : 'false'" :aria-describedby="endDescribedBy" @change="$emit('patch', { windowEnd: $event.target.value })" />
			</div>
			<p :id="startId + '-hint'" class="wz-hint">{{ wrapsMidnight ? $t('backup.wizard.when.window_wraps') : $t('backup.wizard.when.window_hint') }}</p>
			<field-error :idp="idp" field="window.start" :errors="errors"></field-error>
			<field-error :idp="idp" field="window.end" :errors="errors"></field-error>
		</div>
	</div>
</template>

<script>
import FieldError from './FieldError.vue'
import { fieldId, describedBy } from '../wizard/fields'

export default {
	name: 'ConditionEditor',
	components: { FieldError },
	props: {
		type: { type: String, required: true },
		destAvailable: { type: Boolean, default: true },
		effectiveUnmet: { type: String, default: 'skip' },
		waitMaxMin: { type: [Number, String], default: 360 },
		windowOn: { type: Boolean, default: false },
		windowStart: { type: String, default: '22:00' },
		windowEnd: { type: String, default: '07:00' },
		idp: { type: String, required: true },
		errors: { type: Object, default: () => ({}) }
	},
	computed: {
		isMirror() {
			return this.type === 'mirror'
		},
		unmetId() {
			return fieldId(this.idp, 'when_unmet')
		},
		waitId() {
			return fieldId(this.idp, 'wait_max_min')
		},
		startId() {
			return fieldId(this.idp, 'window.start')
		},
		endId() {
			return fieldId(this.idp, 'window.end')
		},
		waitDescribedBy() {
			return describedBy(this.idp, 'wait_max_min', this.errors)
		},
		startDescribedBy() {
			return describedBy(this.idp, 'window.start', this.errors, this.startId + '-hint')
		},
		endDescribedBy() {
			return describedBy(this.idp, 'window.end', this.errors, this.startId + '-hint')
		},
		waitHours() {
			return Math.max(1, Math.round(Number(this.waitMaxMin) / 60))
		},
		waitChoices() {
			const base = [1, 2, 3, 6, 12, 24, 48]
			return base.includes(this.waitHours) ? base : [...base, this.waitHours].sort((a, b) => a - b)
		},
		wrapsMidnight() {
			return this.windowStart && this.windowEnd && this.windowEnd < this.windowStart
		}
	}
}
</script>

<style lang="scss" scoped>
@import './wizard-form.scss';

.condition-editor {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	min-width: 0;
}

.ce-indent {
	margin-left: var(--space-6);

	@media (max-width: 480px) {
		margin-left: 0;
	}
}

.ce-lock {
	display: inline-flex;
	align-items: center;
	gap: 0.15rem;
	margin-left: var(--space-2);
	font-size: var(--font-xs);
	color: var(--theme-text-muted);
}

.ce-time {
	width: 8.5rem;
}
</style>
