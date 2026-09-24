<!-- "What kind of job?" (spec §12.4 What): Mirror / Copy new files /
     Archive in plain words, as a radio group with arrow keys and a roving
     tabindex. Types the destination can't take (app data to a cloud) are
     disabled with the reason. -->
<template>
	<div class="job-type-picker">
		<div class="jt-options" role="radiogroup" :aria-labelledby="labelledby" @keydown="onKeydown">
			<div v-for="(t, i) in types" :key="t" :ref="'opt-' + i" class="jt-option" role="radio" :aria-checked="t === value ? 'true' : 'false'"
				:aria-disabled="isDisabled(t) ? 'true' : 'false'" :aria-describedby="idp + '-type-' + t + '-desc'" :tabindex="i === focusIndex ? 0 : -1"
				:class="{ 'is-checked': t === value, 'is-disabled': isDisabled(t) }" @click="select(i)">
				<span class="jt-radio" aria-hidden="true"><span v-if="t === value" class="jt-dot"></span></span>
				<b-icon :icon="icons[t]" custom-size="mdi-20px" class="jt-icon" aria-hidden="true"></b-icon>
				<span class="jt-text">
					<span class="jt-title">{{ $t('backup.type.' + t + '.label') }}</span>
					<span :id="idp + '-type-' + t + '-desc'" class="jt-desc">
						{{ $t('backup.type.' + t + '.promise') }}
						<span v-if="isDisabled(t)" class="jt-disabled-reason">{{ $t('backup.wizard.what.type_needs_archive') }}</span>
					</span>
				</span>
			</div>
		</div>
		<details class="wz-details jt-explainer">
			<summary>{{ $t('backup.wizard.what.explainer_title') }}</summary>
			<div class="wz-details-body">
				<dl class="jt-explain-list">
					<template v-for="t in types">
						<dt :key="t + '-dt'">{{ $t('backup.type.' + t + '.label') }}</dt>
						<dd :key="t + '-dd'">{{ $t('backup.type.' + t + '.deleted') }}</dd>
					</template>
				</dl>
				<p class="wz-hint">{{ $t('backup.wizard.what.explainer_sync') }}</p>
			</div>
		</details>
	</div>
</template>

<script>
import { radioKeyTarget } from '../wizard/radioKeys'

export default {
	name: 'JobTypePicker',
	props: {
		value: { type: String, default: 'copy' },
		// Types that may be picked (the rest are shown disabled).
		allowed: { type: Array, default: () => ['mirror', 'copy', 'archive'] },
		idp: { type: String, required: true },
		labelledby: { type: String, default: null }
	},
	data() {
		return {
			types: ['mirror', 'copy', 'archive'],
			icons: { mirror: 'mirror', copy: 'content-copy', archive: 'archive-outline' }
		}
	},
	computed: {
		focusIndex() {
			const i = this.types.indexOf(this.value)
			return i < 0 ? 0 : i
		}
	},
	methods: {
		isDisabled(t) {
			return !this.allowed.includes(t)
		},
		select(i) {
			const t = this.types[i]
			if (!t || this.isDisabled(t)) return
			if (t !== this.value) this.$emit('input', t)
			this.$nextTick(() => {
				const r = this.$refs['opt-' + i]
				const el = Array.isArray(r) ? r[0] : r
				if (el) el.focus()
			})
		},
		onKeydown(e) {
			if (e.key === ' ') {
				e.preventDefault()
				this.select(this.focusIndex)
				return
			}
			const next = radioKeyTarget(e.key, this.focusIndex, this.types.length, i => this.isDisabled(this.types[i]))
			if (next < 0) return
			e.preventDefault()
			this.select(next)
		}
	}
}
</script>

<style lang="scss" scoped>
@import './wizard-form.scss';

.job-type-picker {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
}

.jt-options {
	display: flex;
	flex-direction: column;
	gap: var(--space-2);
}

.jt-option {
	display: flex;
	align-items: flex-start;
	gap: var(--space-3);
	padding: var(--space-3);
	border: 1px solid var(--theme-card-border);
	border-radius: var(--radius-card);
	background: var(--theme-card-bg);
	cursor: pointer;
	user-select: none;

	&:hover:not(.is-disabled) {
		background: var(--theme-card-hover);
	}
	&.is-checked {
		border-color: var(--color-primary-fg);
		background: var(--theme-card-selected);
		box-shadow: inset 0 0 0 1px var(--color-primary-fg);
	}
	&.is-disabled {
		cursor: not-allowed;
		.jt-title {
			color: var(--theme-text-secondary);
		}
	}
	&:focus-visible {
		@include picker-focus-ring;
	}
}

.jt-radio {
	flex-shrink: 0;
	display: inline-flex;
	align-items: center;
	justify-content: center;
	width: 18px;
	height: 18px;
	margin-top: 0.1rem;
	border-radius: 50%;
	border: 2px solid var(--theme-input-border);

	.is-checked & {
		border-color: var(--color-primary-fg);
	}
}

.jt-dot {
	width: 8px;
	height: 8px;
	border-radius: 50%;
	background: var(--color-primary-fg);
}

.jt-icon {
	flex-shrink: 0;
	color: var(--theme-text-secondary);
}

.jt-text {
	display: flex;
	flex-direction: column;
	gap: 0.15rem;
	min-width: 0;
}

.jt-title {
	font-size: var(--font-sm);
	font-weight: 600;
	color: var(--theme-text-primary);
}

.jt-desc {
	font-size: var(--font-xs);
	color: var(--theme-text-secondary);
	line-height: 1.45;
}

.jt-disabled-reason {
	display: block;
	margin-top: 0.15rem;
	font-weight: 600;
	color: var(--color-warning-fg);
}

.jt-explain-list {
	display: grid;
	grid-template-columns: max-content 1fr;
	gap: var(--space-1) var(--space-3);
	margin: 0;
	font-size: var(--font-xs);

	dt {
		font-weight: 600;
		color: var(--theme-text-primary);
	}
	dd {
		margin: 0;
		color: var(--theme-text-secondary);
	}

	@media (max-width: 480px) {
		grid-template-columns: 1fr;
		dd {
			margin-bottom: var(--space-2);
		}
	}
}
</style>
