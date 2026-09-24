<!-- "Run this job" (spec §12.4 When, §7.2): on a schedule (the shared
     ScheduleBuilder, previewed by the server in server time), when a drive
     is plugged in, and catching up a run missed while the server was off.
     With nothing ticked the job is manual only, and the step says so. -->
<template>
	<div class="trigger-editor">
		<label class="wz-check">
			<input type="checkbox" :checked="scheduleOn" @change="$emit('patch', { scheduleOn: $event.target.checked })" />
			<span>{{ $t('backup.wizard.when.on_schedule') }}</span>
		</label>
		<div v-if="scheduleOn" :id="cronFieldId" class="te-indent" tabindex="-1">
			<schedule-builder :value="cron" :preview-fn="previewFn" :legend="$t('backup.wizard.when.on_schedule')" @input="$emit('patch', { cron: $event })"
				@validity="$emit('validity', $event)"></schedule-builder>
			<field-error :idp="idp" field="cron" :errors="errors"></field-error>
		</div>

		<label class="wz-check" :class="{ 'is-disabled': !plugDrive }">
			<input type="checkbox" :checked="plugOn" :disabled="!plugDrive && !plugOn" :aria-describedby="plugDrive ? null : idp + '-plug-why'"
				@change="$emit('patch', { plugOn: $event.target.checked })" />
			<span>{{ plugDrive ? $t('backup.wizard.when.on_plug', { drive: plugDrive }) : $t('backup.wizard.when.on_plug_generic') }}</span>
		</label>
		<p v-if="!plugDrive" :id="idp + '-plug-why'" class="wz-hint te-indent">{{ $t('backup.wizard.when.plug_needs_drive') }}</p>
		<div v-if="plugOn" class="te-indent wz-row">
			<label :for="gapFieldId" class="wz-label">{{ $t('backup.wizard.when.plug_gap') }}</label>
			<input :id="gapFieldId" class="wz-number" type="number" min="0" max="720" step="1" inputmode="numeric" :value="plugGapHours"
				:aria-invalid="errors['plug.min_gap_hours'] ? 'true' : 'false'" :aria-describedby="gapDescribedBy"
				@input="$emit('patch', { plugGapHours: toInt($event.target.value) })" />
			<span class="wz-hint">{{ $t('backup.wizard.when.hours') }}</span>
			<p :id="gapFieldId + '-hint'" class="wz-hint te-full">{{ $t('backup.wizard.when.plug_gap_hint') }}</p>
			<field-error class="te-full" :idp="idp" field="plug.min_gap_hours" :errors="errors"></field-error>
			<field-error class="te-full" :idp="idp" field="plug.volume" :errors="errors"></field-error>
		</div>

		<label v-if="scheduleOn || plugOn" class="wz-check">
			<input type="checkbox" :checked="catchUp" @change="$emit('patch', { catchUp: $event.target.checked })" />
			<span>{{ $t('backup.wizard.when.catch_up') }}</span>
		</label>

		<p v-if="!scheduleOn && !plugOn" class="wz-note tone-info" role="status">
			<b-icon icon="hand-back-right-outline" custom-size="mdi-16px" aria-hidden="true"></b-icon>
			<span>{{ $t('backup.wizard.when.manual_only') }}</span>
		</p>
	</div>
</template>

<script>
import ScheduleBuilder from '@/shared/scheduling/ScheduleBuilder.vue'
import FieldError from './FieldError.vue'
import { fieldId, describedBy } from '../wizard/fields'

export default {
	name: 'TriggerEditor',
	components: { ScheduleBuilder, FieldError },
	props: {
		scheduleOn: { type: Boolean, default: false },
		cron: { type: String, default: '' },
		plugOn: { type: Boolean, default: false },
		plugGapHours: { type: [Number, String], default: 24 },
		catchUp: { type: Boolean, default: true },
		// Name of the drive a plug-in trigger watches, '' when none can be.
		plugDrive: { type: String, default: '' },
		previewFn: { type: Function, default: null },
		idp: { type: String, required: true },
		errors: { type: Object, default: () => ({}) }
	},
	computed: {
		cronFieldId() {
			return fieldId(this.idp, 'cron')
		},
		gapFieldId() {
			return fieldId(this.idp, 'plug.min_gap_hours')
		},
		gapDescribedBy() {
			return describedBy(this.idp, 'plug.min_gap_hours', this.errors, this.gapFieldId + '-hint')
		}
	},
	methods: {
		toInt(v) {
			return v === '' ? '' : Math.round(Number(v))
		}
	}
}
</script>

<style lang="scss" scoped>
@import './wizard-form.scss';

.trigger-editor {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	min-width: 0;
}

.te-indent {
	margin-left: var(--space-6);
	min-width: 0;

	&:focus {
		outline: none;
	}
	@media (max-width: 480px) {
		margin-left: 0;
	}
}

.te-full {
	width: 100%;
}
</style>
