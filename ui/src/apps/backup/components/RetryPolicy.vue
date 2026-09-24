<!-- "If it fails: retry [3] times (1 min, 10 min, 1 h)" (spec §12.4 When).
     Only errors that can pass on their own (network, a busy app, a cloud
     asking to slow down) are retried; the delays are the job's backoff. -->
<template>
	<div class="retry-policy wz-row">
		<label :for="selectId" class="wz-label">{{ $t('backup.wizard.when.retry') }}</label>
		<select :id="selectId" class="wz-select" :value="max" :aria-describedby="hintId" @change="$emit('patch', { retryMax: Number($event.target.value) })">
			<option v-for="n in choices" :key="n" :value="n">{{ n === 0 ? $t('backup.wizard.when.retry_never') : $t('backup.wizard.when.retry_times', { n }) }}</option>
		</select>
		<p :id="hintId" class="wz-hint rp-hint">{{ max ? $t('backup.wizard.when.retry_hint', { delays: delaysText }) : $t('backup.wizard.when.retry_off_hint') }}</p>
	</div>
</template>

<script>
import { fieldId } from '../wizard/fields'

export default {
	name: 'RetryPolicy',
	props: {
		max: { type: Number, default: 3 },
		backoffSec: { type: Array, default: () => [60, 600, 3600] },
		idp: { type: String, required: true },
		fmt: { type: Object, required: true }
	},
	computed: {
		selectId() {
			return fieldId(this.idp, 'retry.max')
		},
		hintId() {
			return this.selectId + '-hint'
		},
		choices() {
			const base = [0, 1, 2, 3, 5]
			return base.includes(this.max) ? base : [...base, this.max].sort((a, b) => a - b)
		},
		// The delay before each retry; the last one repeats.
		delaysText() {
			const list = []
			const b = this.backoffSec.length ? this.backoffSec : [60]
			for (let i = 0; i < this.max; i++) list.push(this.fmt.duration(b[Math.min(i, b.length - 1)]))
			return this.fmt.list(list)
		}
	}
}
</script>

<style lang="scss" scoped>
@import './wizard-form.scss';

.rp-hint {
	width: 100%;
}
</style>
