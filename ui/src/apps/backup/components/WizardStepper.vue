<!-- The wizard's step list (spec §12.4): Start (new jobs only) → What →
     Where → When → Keep → Review. Completed steps are buttons you can jump
     back to; later steps are not reachable until you get there. A step
     with errors shows an icon and says so in text. -->
<template>
	<nav class="wizard-stepper" :aria-label="$t('backup.wizard.steps_label')">
		<ol>
			<li v-for="(s, i) in steps" :key="s" :class="{ 'is-current': s === current, 'is-done': isReachable(s) && s !== current, 'has-error': errorCount(s) > 0 }">
				<button v-if="isReachable(s)" type="button" class="ws-step" :aria-current="s === current ? 'step' : null" @click="$emit('go', s)">
					<span class="ws-num" aria-hidden="true">
						<b-icon v-if="errorCount(s) > 0" icon="alert-circle" custom-size="mdi-16px"></b-icon>
						<b-icon v-else-if="s !== current && isDone(s)" icon="check" custom-size="mdi-16px"></b-icon>
						<template v-else>{{ i + 1 }}</template>
					</span>
					<span class="ws-label">{{ $t('backup.wizard.step.' + s) }}</span>
					<span v-if="errorCount(s) > 0" class="sr-only">{{ $t('backup.wizard.step_has_errors', { n: errorCount(s) }) }}</span>
					<span v-else-if="s !== current && isDone(s)" class="sr-only">{{ $t('backup.wizard.step_done') }}</span>
				</button>
				<span v-else class="ws-step is-locked" aria-disabled="true">
					<span class="ws-num" aria-hidden="true">{{ i + 1 }}</span>
					<span class="ws-label">{{ $t('backup.wizard.step.' + s) }}</span>
				</span>
			</li>
		</ol>
	</nav>
</template>

<script>
export default {
	name: 'WizardStepper',
	props: {
		steps: { type: Array, required: true },
		current: { type: String, required: true },
		// The furthest step reached; everything up to it is clickable.
		reached: { type: String, required: true },
		// { step: errorCount } for steps that were checked
		errorCounts: { type: Object, default: () => ({}) }
	},
	methods: {
		isReachable(s) {
			return this.steps.indexOf(s) <= this.steps.indexOf(this.reached)
		},
		isDone(s) {
			return this.steps.indexOf(s) < this.steps.indexOf(this.reached) || (s !== this.current && this.isReachable(s))
		},
		errorCount(s) {
			return this.errorCounts[s] || 0
		}
	}
}
</script>

<style lang="scss" scoped>
@import './wizard-form.scss';

.wizard-stepper ol {
	display: flex;
	gap: var(--space-1);
	margin: 0;
	padding: 0;
	list-style: none;
	overflow-x: auto;
	scrollbar-width: none;
}

.wizard-stepper li {
	flex: 1 1 0;
	min-width: 0;
}

.ws-step {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	width: 100%;
	min-height: 2.5rem;
	padding: 0 var(--space-2);
	border: none;
	border-bottom: 3px solid var(--theme-card-border);
	background: transparent;
	color: var(--theme-text-secondary);
	font-family: inherit;
	font-size: var(--font-sm);
	text-align: left;
	cursor: pointer;

	&:hover:not(.is-locked) {
		background: var(--theme-card-hover);
	}
	&:focus-visible {
		@include picker-focus-ring;
		outline-offset: -2px;
	}
	&.is-locked {
		cursor: default;
		color: var(--theme-text-muted);
	}
	@media (pointer: coarse) {
		min-height: 44px;
	}
}

.ws-num {
	flex-shrink: 0;
	display: inline-flex;
	align-items: center;
	justify-content: center;
	width: 1.5rem;
	height: 1.5rem;
	border-radius: 50%;
	border: 1px solid var(--theme-input-border);
	font-size: var(--font-xs);
	font-weight: 700;
}

.ws-label {
	min-width: 0;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}

.is-done .ws-step {
	color: var(--theme-text-primary);
	border-bottom-color: var(--color-primary-fg);

	.ws-num {
		border-color: var(--color-primary-fg);
		color: var(--color-primary-fg);
	}
}

.is-current .ws-step {
	color: var(--theme-text-primary);
	font-weight: 700;
	border-bottom-color: var(--color-primary);

	.ws-num {
		border-color: var(--color-primary);
		background: var(--color-primary);
		color: var(--color-primary-text);
	}
}

.has-error .ws-step .ws-num {
	border-color: var(--color-danger-fg);
	background: transparent;
	color: var(--color-danger-fg);
}

// Phones: numbers only, the current step keeps its name.
@media (max-width: 560px) {
	.wizard-stepper li:not(.is-current) {
		flex: 0 0 auto;
		.ws-label {
			position: absolute;
			width: 1px;
			height: 1px;
			overflow: hidden;
			clip: rect(0 0 0 0);
		}
	}
}
</style>
