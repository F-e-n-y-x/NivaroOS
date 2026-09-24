<!-- Start step: what kind of backup to set up (spec §12.4 presets), as a
     radio group of cards. Arrow keys move and select, Enter continues. -->
<template>
	<div class="preset-grid" role="radiogroup" :aria-labelledby="labelledby" @keydown="onKeydown">
		<div v-for="(p, i) in presets" :key="p.id" :ref="'card-' + i" class="preset-card" role="radio"
			:aria-checked="p.id === value ? 'true' : 'false'" :aria-describedby="idp + '-preset-' + p.id + '-desc'"
			:tabindex="i === focusIndex ? 0 : -1" :class="{ 'is-checked': p.id === value }"
			@click="select(i)" @dblclick="$emit('choose', p.id)">
			<b-icon :icon="p.icon" custom-size="mdi-24px" class="preset-icon" aria-hidden="true"></b-icon>
			<span class="preset-text">
				<span class="preset-title">{{ $t('backup.preset.' + p.id + '.title') }}</span>
				<span :id="idp + '-preset-' + p.id + '-desc'" class="preset-desc">{{ $t('backup.preset.' + p.id + '.desc') }}</span>
			</span>
			<b-icon v-if="p.id === value" icon="check-circle" custom-size="mdi-20px" class="preset-check" aria-hidden="true"></b-icon>
		</div>
	</div>
</template>

<script>
import { radioKeyTarget } from '../wizard/radioKeys'

export default {
	name: 'PresetGrid',
	props: {
		presets: { type: Array, required: true },
		value: { type: String, default: '' },
		idp: { type: String, required: true },
		labelledby: { type: String, default: null }
	},
	computed: {
		focusIndex() {
			const i = this.presets.findIndex(p => p.id === this.value)
			return i < 0 ? 0 : i
		}
	},
	methods: {
		select(i) {
			const p = this.presets[i]
			if (!p) return
			this.$emit('input', p.id)
			this.focusCard(i)
		},
		focusCard(i) {
			this.$nextTick(() => {
				const r = this.$refs['card-' + i]
				const el = Array.isArray(r) ? r[0] : r
				if (el) el.focus()
			})
		},
		focus() {
			this.focusCard(this.focusIndex)
		},
		onKeydown(e) {
			if (e.key === ' ') {
				e.preventDefault()
				this.select(this.focusIndex)
				return
			}
			if (e.key === 'Enter') {
				e.preventDefault()
				if (this.value) this.$emit('choose', this.value)
				return
			}
			const next = radioKeyTarget(e.key, this.focusIndex, this.presets.length)
			if (next < 0) return
			e.preventDefault()
			this.select(next)
		}
	}
}
</script>

<style lang="scss" scoped>
@import './wizard-form.scss';

.preset-grid {
	display: grid;
	grid-template-columns: repeat(auto-fill, minmax(15rem, 1fr));
	gap: var(--space-2);
}

.preset-card {
	display: flex;
	align-items: flex-start;
	gap: var(--space-3);
	min-height: 4.5rem;
	padding: var(--space-3);
	border: 1px solid var(--theme-card-border);
	border-radius: var(--radius-card);
	background: var(--theme-card-bg);
	cursor: pointer;
	user-select: none;

	&:hover {
		background: var(--theme-card-hover);
	}
	&.is-checked {
		border-color: var(--color-primary-fg);
		background: var(--theme-card-selected);
		box-shadow: inset 0 0 0 1px var(--color-primary-fg);
	}
	&:focus-visible {
		@include picker-focus-ring;
	}
}

.preset-icon {
	flex-shrink: 0;
	color: var(--color-primary-fg);
}

.preset-text {
	flex: 1 1 auto;
	min-width: 0;
	display: flex;
	flex-direction: column;
	gap: 0.15rem;
}

.preset-title {
	font-size: var(--font-sm);
	font-weight: 600;
	color: var(--theme-text-primary);
}

.preset-desc {
	font-size: var(--font-xs);
	color: var(--theme-text-secondary);
	line-height: 1.4;
}

.preset-check {
	flex-shrink: 0;
	color: var(--color-primary-fg);
}
</style>
