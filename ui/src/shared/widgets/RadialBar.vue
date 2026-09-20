<template>
	<div class="radial-gauge-container" :style="cssVariables">
		<div class="gauge-circle-box">
			<svg class="gauge-svg" viewBox="0 0 100 100" xmlns="http://www.w3.org/2000/svg">
				<defs>
					<linearGradient :id="gradientId" x1="0%" y1="0%" x2="100%" y2="100%">
						<stop :stop-color="activeStop1" offset="0%"/>
						<stop :stop-color="activeStop2" offset="100%"/>
					</linearGradient>
				</defs>
				<!-- Background Track (Full 360 concentric circle) -->
				<circle
					class="gauge-track"
					cx="50"
					cy="50"
					r="37"
					shape-rendering="geometricPrecision"
				></circle>
				<!-- Progress Stroke (Starts at 12 o'clock, wraps clockwise) -->
				<circle
					class="gauge-progress"
					cx="50"
					cy="50"
					r="37"
					:stroke-dasharray="circumference"
					:stroke-dashoffset="progressOffset"
					:stroke="`url(#${gradientId})`"
					shape-rendering="geometricPrecision"
				></circle>
			</svg>
			<!-- Perfectly Centered Text Overlay -->
			<div class="gauge-center-content">
				<div class="gauge-val-row">
					<span class="gauge-number">{{ clampedPercent }}</span>
					<span class="gauge-percent">%</span>
				</div>
				<div v-if="label" class="gauge-label">{{ label }}</div>
			</div>
		</div>
		<!-- Optional Legacy Bottom Pill -->
		<div
			v-if="extendContent"
			class="gauge-extend-badge"
			:class="{ 'is-clickable': extendContentClickable }"
			@click="extendClick"
		>
			{{ extendContent }}
		</div>
	</div>
</template>

<script>
export default {
	name: "RadialBar",
	props: {
		dotDiameter: {
			type: String,
			default: "76px",
		},
		circleBorderWidth: {
			type: String,
			default: "5px",
		},
		circleBackgroundColor: {
			type: String,
			default: "",
		},
		stopColorStart: {
			type: String,
			default: "",
		},
		stopColorEnd: {
			type: String,
			default: "",
		},
		percent: {
			type: Number,
			default: 0,
		},
		label: {
			type: String,
			default: "",
		},
		extendContent: {
			type: String,
			default: "",
		},
		extendContentClickable: {
			type: Boolean,
			default: false,
		},
	},

	computed: {
		gradientId() {
			return `radial-bar-grad-${this._uid}`
		},
		clampedPercent() {
			const p = Math.round(this.percent)
			if (isNaN(p) || p < 0) return 0
			if (p > 100) return 100
			return p
		},
		// Circumference for r=37 is 2 * PI * 37 = 232.478
		circumference() {
			return 232.478
		},
		progressOffset() {
			if (this.clampedPercent <= 0) return this.circumference
			if (this.clampedPercent >= 100) return 0
			return this.circumference * (1 - this.clampedPercent / 100)
		},
		activeStop1() {
			if (this.stopColorStart && this.stopColorStart !== '#33FFAA') {
				return this.stopColorStart
			}
			if (this.clampedPercent >= 90) return '#ef4444' // red
			if (this.clampedPercent >= 75) return '#f59e0b' // amber
			if (this.label === 'RAM') return '#8b5cf6' // purple
			if (this.label === 'GPU' || this.label === 'CORE') return '#10b981' // emerald
			if (this.label === 'VRAM') return '#06b6d4' // cyan
			return '#2563eb' // royal blue default
		},
		activeStop2() {
			if (this.stopColorEnd && this.stopColorEnd !== '#FFD580') {
				return this.stopColorEnd
			}
			if (this.clampedPercent >= 90) return '#f97316' // orange
			if (this.clampedPercent >= 75) return '#eab308' // yellow
			if (this.label === 'RAM') return '#6366f1' // indigo
			if (this.label === 'GPU' || this.label === 'CORE') return '#34d399' // mint
			if (this.label === 'VRAM') return '#38bdf8' // sky blue
			return '#38bdf8' // sky blue default
		},
		cssVariables() {
			return {
				"--gauge-size": this.dotDiameter,
				"--gauge-stroke-width": this.circleBorderWidth,
				"--gauge-track-color": this.circleBackgroundColor || "var(--theme-desktop-glass-track, rgba(0, 0, 0, 0.08))",
			};
		},
	},
	methods: {
		extendClick() {
			if (this.extendContentClickable) {
				this.$emit("extendContentClick");
			}
		},
	},
};
</script>

<style lang="scss" scoped>
.radial-gauge-container {
	display: inline-flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	position: relative;
}

.gauge-circle-box {
	position: relative;
	width: var(--gauge-size);
	height: var(--gauge-size);
	display: flex;
	align-items: center;
	justify-content: center;
	flex-shrink: 0;
}

.gauge-svg {
	width: 100%;
	height: 100%;
	fill: none;
	overflow: visible;
}

.gauge-track {
	fill: none;
	stroke: var(--gauge-track-color);
	stroke-width: var(--gauge-stroke-width);
}

.gauge-progress {
	fill: none;
	stroke-width: var(--gauge-stroke-width);
	stroke-linecap: round;
	transform: rotate(-90deg);
	transform-origin: 50% 50%;
	transition: stroke-dashoffset 0.6s cubic-bezier(0.16, 1, 0.3, 1), stroke 0.3s ease;
}

.gauge-center-content {
	position: absolute;
	inset: 0;
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	text-align: center;
	pointer-events: none;
	user-select: none;
}

.gauge-val-row {
	display: inline-flex;
	align-items: baseline;
	justify-content: center;
	line-height: 1;
}

.gauge-number {
	font-size: 1.15rem;
	font-weight: 600;
	font-variant-numeric: tabular-nums;
	letter-spacing: -0.01em;
	color: var(--theme-desktop-glass-text, #0f172a);
	line-height: 1;
}

.gauge-percent {
	font-size: 0.65rem;
	font-weight: 500;
	color: var(--theme-desktop-glass-text-sub, #475569);
	margin-left: 1px;
	line-height: 1;
}

.gauge-label {
	font-size: 0.54rem;
	font-weight: 600;
	text-transform: uppercase;
	letter-spacing: 0.06em;
	color: var(--theme-desktop-glass-text-sub, #64748b);
	margin-top: 2px;
	line-height: 1;
}

.gauge-extend-badge {
	display: inline-flex;
	align-items: center;
	justify-content: center;
	text-align: center;
	font-size: 0.66rem;
	font-weight: 600;
	font-variant-numeric: tabular-nums;
	color: var(--theme-desktop-glass-text, #0f172a);
	background: var(--theme-card-subtle, rgba(0, 0, 0, 0.03));
	border: 1px solid var(--theme-desktop-glass-border, rgba(0, 0, 0, 0.06));
	border-radius: var(--radius-sm, 6px);
	padding: 2px 8px;
	margin-top: 6px;
	max-width: 100%;
	white-space: nowrap;
	overflow: hidden;
	text-overflow: ellipsis;
	transition: all 0.15s ease;

	&.is-clickable {
		cursor: pointer;

		&:hover {
			background: var(--theme-card-hover, rgba(0, 0, 0, 0.08));
			color: var(--color-primary, #2563eb);
			border-color: var(--color-primary, #2563eb);
		}
	}
}
</style>
