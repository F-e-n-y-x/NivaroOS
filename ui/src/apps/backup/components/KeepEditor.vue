<!-- "Keep" (spec §12.4): what each job type keeps, and the safety stops.
     Mirror: recycle days, delete/change guards, preview of the first run.
     Archive: how many archives. Copy: nothing is ever deleted. Every type:
     the empty-source guard and "check files after copying" (disabled, with
     the reason, when the destination has no checksums). -->
<template>
	<div class="keep-editor">
		<section class="wz-section">
			<h3 class="wz-heading">{{ $t('backup.wizard.keep.what_heading') }}</h3>
			<template v-if="type === 'mirror'">
				<div class="wz-row">
					<label :for="daysId" class="wz-label">{{ $t('backup.wizard.keep.recycle_days') }}</label>
					<input :id="daysId" class="wz-number" type="number" min="0" max="3650" step="1" inputmode="numeric" :value="versionsDays"
						:aria-invalid="errors['retention.versions_days'] ? 'true' : 'false'" :aria-describedby="daysDescribedBy"
						@input="patchInt('versionsDays', $event.target.value)" />
					<span class="wz-hint">{{ $t('backup.wizard.keep.days') }}</span>
				</div>
				<p :id="daysId + '-hint'" class="wz-hint">{{ Number(versionsDays) === 0 ? $t('backup.wizard.keep.recycle_forever_hint') : $t('backup.wizard.keep.recycle_hint') }}</p>
				<field-error :idp="idp" field="retention.versions_days" :errors="errors"></field-error>
			</template>
			<template v-else-if="type === 'archive'">
				<div class="wz-row">
					<label :for="keepId" class="wz-label">{{ $t('backup.wizard.keep.keep_last') }}</label>
					<input :id="keepId" class="wz-number" type="number" min="1" max="1000" step="1" inputmode="numeric" :value="keepLast"
						:aria-invalid="errors['retention.keep_last'] ? 'true' : 'false'" :aria-describedby="keepDescribedBy"
						@input="patchInt('keepLast', $event.target.value)" />
					<span class="wz-hint">{{ $t('backup.wizard.keep.archives') }}</span>
				</div>
				<p :id="keepId + '-hint'" class="wz-hint">{{ $t('backup.wizard.keep.keep_last_hint') }}</p>
				<field-error :idp="idp" field="retention.keep_last" :errors="errors"></field-error>
			</template>
			<p v-else class="wz-note tone-ok">
				<b-icon icon="shield-check-outline" custom-size="mdi-16px" aria-hidden="true"></b-icon>
				<span>{{ $t('backup.keep.copy_never_deletes') }}</span>
			</p>
		</section>

		<section class="wz-section">
			<h3 class="wz-heading">{{ $t('backup.wizard.keep.safety_heading') }}</h3>
			<div v-if="type !== 'archive'" class="ke-guard">
				<span class="wz-label">{{ $t('backup.wizard.keep.guard_intro') }}</span>
				<div class="wz-row">
					<template v-if="type === 'mirror'">
						<label :for="deleteId">{{ $t('backup.wizard.keep.delete_more') }}</label>
						<input :id="deleteId" class="wz-number" type="number" min="1" max="100" step="1" inputmode="numeric" :value="deletePct"
							:aria-invalid="errors['guards.delete_pct'] ? 'true' : 'false'" :aria-describedby="deleteDescribedBy" @input="patchInt('deletePct', $event.target.value)" />
						<span aria-hidden="true">%</span>
					</template>
					<label :for="changeId">{{ type === 'mirror' ? $t('backup.wizard.keep.or_change_more') : $t('backup.wizard.keep.change_more') }}</label>
					<input :id="changeId" class="wz-number" type="number" min="1" max="100" step="1" inputmode="numeric" :value="changePct"
						:aria-invalid="errors['guards.change_pct'] ? 'true' : 'false'" :aria-describedby="changeDescribedBy" @input="patchInt('changePct', $event.target.value)" />
					<span aria-hidden="true">%</span>
					<span>{{ $t('backup.wizard.keep.stop_and_ask') }}</span>
				</div>
				<p :id="changeId + '-hint'" class="wz-hint">{{ $t('backup.wizard.keep.guard_hint') }}</p>
				<field-error :idp="idp" field="guards.delete_pct" :errors="errors"></field-error>
				<field-error :idp="idp" field="guards.change_pct" :errors="errors"></field-error>
			</div>

			<div class="wz-row">
				<label :for="emptyId">{{ $t('backup.wizard.keep.empty_source') }}</label>
				<input :id="emptyId" class="wz-number" type="number" min="1" max="100" step="1" inputmode="numeric" :value="emptySourcePct"
					:aria-invalid="errors['guards.empty_source_pct'] ? 'true' : 'false'" :aria-describedby="emptyDescribedBy" @input="patchInt('emptySourcePct', $event.target.value)" />
				<span aria-hidden="true">%</span>
				<span>{{ $t('backup.wizard.keep.empty_source_after') }}</span>
			</div>
			<field-error :idp="idp" field="guards.empty_source_pct" :errors="errors"></field-error>

			<label v-if="type !== 'archive'" class="wz-check">
				<input type="checkbox" :checked="previewFirst" @change="$emit('patch', { previewFirst: $event.target.checked, previewTouched: true })" />
				<span>
					{{ $t('backup.wizard.keep.preview_first') }}
					<span class="wz-hint ke-block">{{ $t('backup.wizard.keep.preview_first_hint') }}</span>
				</span>
			</label>

			<label class="wz-check" :class="{ 'is-disabled': verifyBlocked }">
				<input type="checkbox" :checked="verify && !verifyBlocked" :disabled="verifyBlocked" :aria-describedby="idp + '-verify-hint'"
					@change="$emit('patch', { verify: $event.target.checked })" />
				<span>
					{{ $t('backup.wizard.keep.verify') }}
					<span :id="idp + '-verify-hint'" class="wz-hint ke-block">{{ verifyBlocked ? $t('backup.wizard.keep.verify_blocked') : $t('backup.wizard.keep.verify_hint') }}</span>
				</span>
			</label>
		</section>
	</div>
</template>

<script>
import FieldError from './FieldError.vue'
import { fieldId, describedBy } from '../wizard/fields'

export default {
	name: 'KeepEditor',
	components: { FieldError },
	props: {
		type: { type: String, required: true },
		versionsDays: { type: [Number, String], default: 30 },
		keepLast: { type: [Number, String], default: 8 },
		deletePct: { type: [Number, String], default: 10 },
		changePct: { type: [Number, String], default: 30 },
		emptySourcePct: { type: [Number, String], default: 50 },
		previewFirst: { type: Boolean, default: false },
		verify: { type: Boolean, default: false },
		// The destination can't be checked (no hashes, e.g. TeraBox).
		verifyBlocked: { type: Boolean, default: false },
		idp: { type: String, required: true },
		errors: { type: Object, default: () => ({}) }
	},
	computed: {
		daysId() {
			return fieldId(this.idp, 'retention.versions_days')
		},
		keepId() {
			return fieldId(this.idp, 'retention.keep_last')
		},
		deleteId() {
			return fieldId(this.idp, 'guards.delete_pct')
		},
		changeId() {
			return fieldId(this.idp, 'guards.change_pct')
		},
		emptyId() {
			return fieldId(this.idp, 'guards.empty_source_pct')
		},
		daysDescribedBy() {
			return describedBy(this.idp, 'retention.versions_days', this.errors, this.daysId + '-hint')
		},
		keepDescribedBy() {
			return describedBy(this.idp, 'retention.keep_last', this.errors, this.keepId + '-hint')
		},
		deleteDescribedBy() {
			return describedBy(this.idp, 'guards.delete_pct', this.errors, this.changeId + '-hint')
		},
		changeDescribedBy() {
			return describedBy(this.idp, 'guards.change_pct', this.errors, this.changeId + '-hint')
		},
		emptyDescribedBy() {
			return describedBy(this.idp, 'guards.empty_source_pct', this.errors)
		}
	},
	methods: {
		patchInt(field, v) {
			this.$emit('patch', { [field]: v === '' ? '' : Math.round(Number(v)) })
		}
	}
}
</script>

<style lang="scss" scoped>
@import './wizard-form.scss';

.keep-editor {
	min-width: 0;
}

.ke-guard {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	font-size: var(--font-sm);
}

.wz-row {
	font-size: var(--font-sm);
	color: var(--theme-text-primary);
}

.ke-block {
	display: block;
}
</style>
