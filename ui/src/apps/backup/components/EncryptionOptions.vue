<!-- "Encrypt backups" on the Where step (spec §18): off, an encrypted
     folder (rclone crypt, recommended) or an encrypted archive (7z). A new
     job asks for the password twice, with a strength meter, an optional
     recovery key and the warning that both lost = backup lost. The
     password lives in `secret`, never in the (autosaved) draft. An edited
     job only shows how it is encrypted: the password can't change. -->
<template>
	<section class="wz-section encryption-options">
		<h3 class="wz-heading">{{ $t('backup.encrypt.heading') }}</h3>
		<p class="wz-sub">{{ $t('backup.encrypt.intro') }}</p>

		<template v-if="isEdit">
			<p class="wz-note" :class="mode ? 'tone-ok' : 'tone-info'">
				<b-icon :icon="mode ? 'lock-outline' : 'lock-open-variant-outline'" custom-size="mdi-16px" aria-hidden="true"></b-icon>
				<span>
					<strong>{{ $t('backup.encrypt.mode_' + (mode || 'off')) }}.</strong>
					{{ mode ? $t('backup.encrypt.cant_change') : $t('backup.encrypt.cant_add') }}
					<button v-if="mode" type="button" class="wz-note-action" @click="$emit('new-copy')">{{ $t('backup.encrypt.new_copy') }}</button>
				</span>
			</p>
		</template>

		<template v-else>
			<div :id="modeId" class="ee-modes" role="radiogroup" :aria-label="$t('backup.encrypt.heading')" :aria-describedby="modeDescribedBy" tabindex="-1">
				<label v-for="m in modes" :key="m.id" class="wz-check ee-mode" :class="{ 'is-disabled': m.disabled, 'is-on': mode === m.id }">
					<input type="radio" :name="idp + '-encrypt'" :value="m.id" :checked="mode === m.id" :disabled="m.disabled" @change="choose(m.id)" />
					<span>
						<strong>{{ $t('backup.encrypt.mode_' + (m.id || 'off')) }}</strong>
						<span v-if="m.id === 'folder'" class="wz-pill tone-ok ee-rec">{{ $t('backup.encrypt.recommended') }}</span>
						<span class="wz-hint ee-block">{{ $t('backup.encrypt.mode_' + (m.id || 'off') + '_hint', { size: volumeText }) }}</span>
						<span v-if="m.id === 'archive' && !sevenZip" class="wz-hint ee-block">{{ $t('backup.encrypt.no_7z') }}</span>
					</span>
				</label>
			</div>
			<field-error :idp="idp" field="dest.encryption" :errors="errors"></field-error>

			<p v-if="mode === 'archive'" class="wz-hint">{{ $t('backup.encrypt.why_not_zip') }}</p>

			<div v-if="mode === 'archive'" class="wz-row">
				<label :for="volumeId" class="wz-label">{{ $t('backup.encrypt.volume') }}</label>
				<input :id="volumeId" class="wz-number" type="number" min="0.1" max="4.29" step="0.1" inputmode="decimal" :value="volumeGb"
					:aria-invalid="errors['dest.encryption.volume_bytes'] ? 'true' : 'false'" :aria-describedby="volumeDescribedBy"
					@input="$emit('patch', { volumeBytes: Math.round(Number($event.target.value) * 1e9) })" />
				<span class="wz-hint">GB</span>
			</div>
			<p v-if="mode === 'archive'" :id="volumeId + '-hint'" class="wz-hint">{{ $t('backup.encrypt.volume_hint') }}</p>
			<field-error :idp="idp" field="dest.encryption.volume_bytes" :errors="errors"></field-error>

			<template v-if="mode">
				<template v-if="!secret.useRecoveryKey">
					<div class="ee-field">
						<label :for="pwId" class="wz-label">{{ $t('backup.encrypt.password') }}</label>
						<div class="wz-row">
							<input :id="pwId" class="wz-input ee-pw" :type="show ? 'text' : 'password'" autocomplete="new-password" spellcheck="false" :value="secret.password"
								:aria-invalid="errors['encryption.password'] ? 'true' : 'false'" :aria-describedby="pwDescribedBy" @input="set({ password: $event.target.value })" />
							<button type="button" class="wz-secondary" :aria-pressed="show ? 'true' : 'false'" @click="show = !show">
								{{ show ? $t('backup.encrypt.hide') : $t('backup.encrypt.show') }}
							</button>
						</div>
						<div class="ee-meter">
							<meter :id="pwId + '-meter'" min="0" max="4" low="2" high="3" optimum="4" :value="strength" :aria-label="$t('backup.encrypt.strength')"></meter>
							<span :id="pwId + '-strength'" class="wz-hint" :class="'tone-' + strengthTone">{{ secret.password ? $t('backup.encrypt.strength_' + strength) : $t('backup.encrypt.strength_hint') }}</span>
						</div>
						<field-error :idp="idp" field="encryption.password" :errors="errors"></field-error>
					</div>
					<div class="ee-field">
						<label :for="confirmId" class="wz-label">{{ $t('backup.encrypt.confirm') }}</label>
						<input :id="confirmId" class="wz-input ee-pw" :type="show ? 'text' : 'password'" autocomplete="new-password" spellcheck="false" :value="secret.confirm"
							:aria-invalid="errors['encryption.confirm'] ? 'true' : 'false'" :aria-describedby="confirmDescribedBy" @input="set({ confirm: $event.target.value })" />
						<field-error :idp="idp" field="encryption.confirm" :errors="errors"></field-error>
					</div>
					<label class="wz-check">
						<input type="checkbox" :checked="secret.makeRecoveryKey" @change="set({ makeRecoveryKey: $event.target.checked })" />
						<span>
							{{ $t('backup.encrypt.make_recovery') }}
							<span class="wz-hint ee-block">{{ $t('backup.encrypt.make_recovery_hint') }}</span>
						</span>
					</label>
				</template>
				<div v-else class="ee-field">
					<label :for="recoveryId" class="wz-label">{{ $t('backup.encrypt.recovery_key') }}</label>
					<input :id="recoveryId" class="wz-input ee-pw" type="text" autocomplete="off" spellcheck="false" :value="secret.recoveryKey"
						:aria-invalid="errors['encryption.recovery_key'] ? 'true' : 'false'" :aria-describedby="recoveryDescribedBy" @input="set({ recoveryKey: $event.target.value })" />
					<p :id="recoveryId + '-hint'" class="wz-hint">{{ $t('backup.encrypt.recovery_key_hint') }}</p>
					<field-error :idp="idp" field="encryption.recovery_key" :errors="errors"></field-error>
				</div>
				<button type="button" class="wz-link ee-switch" @click="set({ useRecoveryKey: !secret.useRecoveryKey })">
					{{ secret.useRecoveryKey ? $t('backup.encrypt.use_password') : $t('backup.encrypt.use_recovery') }}
				</button>

				<p class="wz-note tone-danger" role="note">
					<b-icon icon="alert-octagon-outline" custom-size="mdi-16px" aria-hidden="true"></b-icon>
					<span><strong>{{ $t('backup.encrypt.warning_title') }}</strong> {{ $t('backup.encrypt.warning') }}</span>
				</p>
			</template>
		</template>
	</section>
</template>

<script>
import FieldError from './FieldError.vue'
import { fieldId, describedBy } from '../wizard/fields'
import { passwordStrength } from '../wizard/draft'

export default {
	emits: ['new-copy', 'patch', 'secret'],
	name: 'EncryptionOptions',
	components: { FieldError },
	props: {
		mode: { type: String, default: '' },
		type: { type: String, required: true },
		volumeBytes: { type: [Number, String], default: 3900000000 },
		secret: { type: Object, required: true },
		isEdit: { type: Boolean, default: false },
		// capabilities.sevenzip.available (null = unknown: allowed, the server decides)
		sevenZip: { type: Boolean, default: true },
		idp: { type: String, required: true },
		errors: { type: Object, default: () => ({}) }
	},
	data() {
		return { show: false }
	},
	computed: {
		modes() {
			return [{ id: '' }, { id: 'folder' }, { id: 'archive', disabled: !this.sevenZip && this.mode !== 'archive' }]
		},
		volumeGb() {
			return Math.round((Number(this.volumeBytes) / 1e9) * 100) / 100
		},
		volumeText() {
			return `${this.volumeGb} GB`
		},
		strength() {
			return passwordStrength(this.secret.password)
		},
		strengthTone() {
			if (!this.secret.password) return 'muted'
			return ['danger', 'danger', 'warn', 'ok', 'ok'][this.strength]
		},
		modeId() {
			return fieldId(this.idp, 'dest.encryption')
		},
		volumeId() {
			return fieldId(this.idp, 'dest.encryption.volume_bytes')
		},
		pwId() {
			return fieldId(this.idp, 'encryption.password')
		},
		confirmId() {
			return fieldId(this.idp, 'encryption.confirm')
		},
		recoveryId() {
			return fieldId(this.idp, 'encryption.recovery_key')
		},
		modeDescribedBy() {
			return describedBy(this.idp, 'dest.encryption', this.errors)
		},
		volumeDescribedBy() {
			return describedBy(this.idp, 'dest.encryption.volume_bytes', this.errors, this.volumeId + '-hint')
		},
		pwDescribedBy() {
			return describedBy(this.idp, 'encryption.password', this.errors, this.pwId + '-strength')
		},
		confirmDescribedBy() {
			return describedBy(this.idp, 'encryption.confirm', this.errors)
		},
		recoveryDescribedBy() {
			return describedBy(this.idp, 'encryption.recovery_key', this.errors, this.recoveryId + '-hint')
		}
	},
	methods: {
		choose(m) {
			const patch = { encryptMode: m }
			// An encrypted archive is an archive job: one full 7z per run.
			if (m === 'archive' && this.type !== 'archive') patch.type = 'archive'
			this.$emit('patch', patch)
		},
		set(p) {
			this.$emit('secret', { ...this.secret, ...p })
		}
	}
}
</script>

<style lang="scss" scoped>
@import './wizard-form.scss';

.ee-modes {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
	outline: none;
}

.ee-mode {
	padding: var(--space-2) var(--space-3);
	border: 1px solid var(--theme-card-border);
	border-radius: var(--radius-sm);

	&.is-on {
		border-color: var(--color-primary);
	}
}

.ee-block {
	display: block;
}

.ee-rec {
	margin-left: var(--space-2);
}

.ee-field {
	display: flex;
	flex-direction: column;
	gap: var(--space-1);
	min-width: 0;
}

.ee-pw {
	flex: 1 1 16rem;
}

.ee-meter {
	display: flex;
	align-items: center;
	gap: var(--space-2);

	meter {
		width: 10rem;
		height: 0.5rem;
	}
}

.ee-switch {
	align-self: flex-start;
}

.ee-meter {
	.tone-danger {
		color: var(--color-danger-fg);
	}
	.tone-warn {
		color: var(--color-warning-fg);
	}
	.tone-ok {
		color: var(--color-success-fg);
	}
}
</style>
