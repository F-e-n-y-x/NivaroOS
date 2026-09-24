<!-- "Keep apps consistent" (spec §12.4 What, §8.2 hooks): app data copied
     while its app runs can be corrupt (a database mid-write), so each app
     whose data is backed up is stopped for the backup and started again
     after - on by default, can be turned off. VMs are shut down the same
     way. An app or VM that was already stopped stays stopped. -->
<template>
	<details class="wz-details app-consistency" :open="open">
		<summary>{{ $t('backup.wizard.what.consistency_title', { n: appNames.length + vmNames.length }) }}</summary>
		<div class="wz-details-body">
			<p class="wz-hint">{{ $t('backup.wizard.what.consistency_hint') }}</p>
			<label v-for="a in appNames" :key="'app-' + a" class="wz-check">
				<input type="checkbox" :checked="stopApps[a]" @change="setApp(a, $event.target.checked)" />
				<span>
					{{ $t('backup.wizard.what.stop_app', { app: a }) }}
					<span v-if="!stopApps[a]" class="ac-risk">{{ $t('backup.wizard.what.stop_app_off', { app: a }) }}</span>
				</span>
			</label>
			<fieldset v-if="checkedApps.length > 1" class="ac-mode">
				<legend class="wz-label">{{ $t('backup.wizard.what.app_mode') }}</legend>
				<label class="wz-check">
					<input type="radio" :name="idp + '-app-mode'" value="together" :checked="appMode !== 'one_at_a_time'" @change="$emit('patch', { appMode: 'together' })" />
					<span>{{ $t('backup.wizard.what.app_mode_together') }}</span>
				</label>
				<label class="wz-check">
					<input type="radio" :name="idp + '-app-mode'" value="one_at_a_time" :checked="appMode === 'one_at_a_time'" @change="$emit('patch', { appMode: 'one_at_a_time' })" />
					<span>{{ $t('backup.wizard.what.app_mode_each') }}</span>
				</label>
			</fieldset>
			<label v-for="v in vmNames" :key="'vm-' + v" class="wz-check">
				<input type="checkbox" :checked="shutdownVms[v]" @change="setVm(v, $event.target.checked)" />
				<span>
					{{ $t('backup.wizard.what.shutdown_vm', { vm: v }) }}
					<span class="ac-note">{{ shutdownVms[v] ? $t('backup.wizard.what.shutdown_vm_note') : $t('backup.wizard.what.shutdown_vm_off') }}</span>
				</span>
			</label>
			<field-error :idp="idp" field="hooks" :errors="errors"></field-error>
		</div>
	</details>
</template>

<script>
import FieldError from './FieldError.vue'

export default {
	name: 'AppConsistencyOptions',
	components: { FieldError },
	props: {
		stopApps: { type: Object, required: true },
		appMode: { type: String, default: 'together' },
		shutdownVms: { type: Object, required: true },
		idp: { type: String, required: true },
		errors: { type: Object, default: () => ({}) },
		open: { type: Boolean, default: true }
	},
	computed: {
		appNames() {
			return Object.keys(this.stopApps)
		},
		vmNames() {
			return Object.keys(this.shutdownVms)
		},
		checkedApps() {
			return this.appNames.filter(a => this.stopApps[a])
		}
	},
	methods: {
		setApp(a, on) {
			this.$emit('patch', { stopApps: { ...this.stopApps, [a]: on } })
		},
		setVm(v, on) {
			this.$emit('patch', { shutdownVms: { ...this.shutdownVms, [v]: on } })
		}
	}
}
</script>

<style lang="scss" scoped>
@import './wizard-form.scss';

.ac-mode {
	margin: 0 0 0 var(--space-6);
	padding: 0;
	border: none;
}

.ac-risk,
.ac-note {
	display: block;
	font-size: var(--font-xs);
}

.ac-risk {
	color: var(--color-warning-fg);
	font-weight: 600;
}

.ac-note {
	color: var(--theme-text-muted);
}
</style>
