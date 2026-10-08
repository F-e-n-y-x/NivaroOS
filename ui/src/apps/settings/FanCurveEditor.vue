<template>
	<div class="fan-curve" :class="{ 'is-readonly': readonly }">
		<svg ref="svg" :viewBox="`0 0 ${width} ${height}`" class="curve-svg" role="img"
			:aria-label="$t('Fan curve: speed by temperature')"
			@pointermove="onMove" @pointerup="onUp" @pointercancel="onUp" @dblclick="onDblClick">
			<!-- grid -->
			<g class="grid">
				<line v-for="t in tTicks" :key="'t' + t" :x1="scale.x(t)" :x2="scale.x(t)" :y1="scale.y(100)" :y2="scale.y(0)"></line>
				<line v-for="p in pTicks" :key="'p' + p" :x1="scale.x(tMin)" :x2="scale.x(tMax)" :y1="scale.y(p)" :y2="scale.y(p)"></line>
			</g>
			<g class="axis">
				<text v-for="t in tTicks" :key="'tl' + t" :x="scale.x(t)" :y="height - 4" text-anchor="middle">{{ t }}°</text>
				<text v-for="p in pTicks" :key="'pl' + p" :x="scale.x(tMin) - 6" :y="scale.y(p) + 3" text-anchor="end">{{ p }}%</text>
			</g>
			<!-- floor and emergency zones -->
			<rect class="floor-zone" :x="scale.x(tMin)" :width="scale.x(tMax) - scale.x(tMin)" :y="scale.y(minPct)" :height="scale.y(0) - scale.y(minPct)"></rect>
			<rect v-if="critical" class="crit-zone" :x="scale.x(Math.min(critical, tMax))" :width="Math.max(0, scale.x(tMax) - scale.x(Math.min(critical, tMax)))" :y="scale.y(100)" :height="scale.y(0) - scale.y(100)"></rect>
			<path class="curve-fill" :d="fillPath"></path>
			<path class="curve-line" :d="linePath"></path>
			<!-- live position -->
			<g v-if="currentTemp !== null && currentTemp !== undefined" class="now">
				<line :x1="scale.x(clampT(currentTemp))" :x2="scale.x(clampT(currentTemp))" :y1="scale.y(100)" :y2="scale.y(0)"></line>
				<circle :cx="scale.x(clampT(currentTemp))" :cy="scale.y(evalAt(currentTemp))" r="4"></circle>
			</g>
			<g v-for="(pt, i) in local" :key="i" class="handle" :class="{ active: dragging === i, selected: selected === i }"
				tabindex="0" role="slider" :aria-valuetext="`${pt.t} °C, ${pt.p}%`" :aria-label="$t('Curve point {n}', { n: i + 1 })"
				@pointerdown.prevent="onDown($event, i)" @keydown="onKey($event, i)" @focus="selected = i">
				<circle :cx="scale.x(pt.t)" :cy="scale.y(pt.p)" r="12" class="hit"></circle>
				<circle :cx="scale.x(pt.t)" :cy="scale.y(pt.p)" r="6" class="dot"></circle>
			</g>
		</svg>
		<div class="curve-foot">
			<span v-if="selected !== null && local[selected]" class="point-readout">
				{{ $t('Point {n}', { n: selected + 1 }) }}: <b>{{ local[selected].t }} °C → {{ local[selected].p }}%</b>
			</span>
			<span v-else class="hint">{{ readonly ? '' : $t('Drag the points. Double-click to add one. Arrow keys fine-tune the selected point.') }}</span>
			<span v-if="!readonly" class="curve-actions">
				<b-button size="is-small" rounded :disabled="local.length >= limits.max_points" @click="add">{{ $t('Add point') }}</b-button>
				<b-button size="is-small" rounded :disabled="selected === null || local.length <= limits.min_points" @click="remove">{{ $t('Remove point') }}</b-button>
			</span>
		</div>
	</div>
</template>

<script>
import { DEFAULT_LIMITS, addPoint, curvePath, evalCurve, makeScale, movePoint, removePoint } from '@/utils/fans/curve'

export default {
	emits: ['change', 'update:modelValue'],
	name: 'fan-curve-editor',
	props: {
		modelValue: { type: Array, required: true },
		minPct: { type: Number, default: 20 },
		limits: { type: Object, default: () => DEFAULT_LIMITS },
		currentTemp: { type: Number, default: null },
		critical: { type: Number, default: 0 },
		readonly: { type: Boolean, default: false }
	},
	data() {
		return {
			width: 480,
			height: 240,
			tMin: 20,
			tMax: 100,
			local: this.modelValue.map((p) => ({ ...p })),
			dragging: null,
			selected: null
		}
	},
	computed: {
		scale() {
			return makeScale({ width: this.width, height: this.height, tMin: this.tMin, tMax: this.tMax })
		},
		tTicks() {
			return [20, 30, 40, 50, 60, 70, 80, 90, 100]
		},
		pTicks() {
			return [0, 25, 50, 75, 100]
		},
		linePath() {
			return curvePath(this.local, this.scale, this.tMin, this.tMax)
		},
		fillPath() {
			if (!this.local.length) return ''
			return `${this.linePath} L${this.scale.x(this.tMax)},${this.scale.y(0)} L${this.scale.x(this.tMin)},${this.scale.y(0)} Z`
		}
	},
	watch: {
		modelValue(v) {
			if (this.dragging === null) this.local = v.map((p) => ({ ...p }))
		}
	},
	methods: {
		clampT(t) {
			return Math.min(this.tMax, Math.max(this.tMin, t))
		},
		evalAt(t) {
			return Math.max(this.minPct, evalCurve(this.local, t))
		},
		svgPoint(ev) {
			const svg = this.$refs.svg
			const r = svg.getBoundingClientRect()
			return { x: ((ev.clientX - r.left) / r.width) * this.width, y: ((ev.clientY - r.top) / r.height) * this.height }
		},
		onDown(ev, i) {
			if (this.readonly) return
			this.dragging = i
			this.selected = i
			try {
				this.$refs.svg.setPointerCapture(ev.pointerId)
			} catch (e) {}
		},
		onMove(ev) {
			if (this.dragging === null) return
			const { x, y } = this.svgPoint(ev)
			this.local = movePoint(this.local, this.dragging, this.scale.t(x), this.scale.p(y), this.minPct, this.limits)
			this.$emit('update:modelValue', this.local)
		},
		onUp() {
			if (this.dragging === null) return
			this.dragging = null
			this.commit()
		},
		onDblClick(ev) {
			if (this.readonly) return
			const { x } = this.svgPoint(ev)
			const next = addPoint(this.local, this.scale.t(x), this.limits)
			if (next !== this.local) {
				this.local = next
				this.commit()
			}
		},
		onKey(ev, i) {
			if (this.readonly) return
			const step = ev.shiftKey ? 5 : 1
			const pt = this.local[i]
			let t = pt.t
			let p = pt.p
			if (ev.key === 'ArrowUp') p += step
			else if (ev.key === 'ArrowDown') p -= step
			else if (ev.key === 'ArrowRight') t += step
			else if (ev.key === 'ArrowLeft') t -= step
			else if (ev.key === 'Delete' || ev.key === 'Backspace') {
				this.selected = i
				this.remove()
				ev.preventDefault()
				return
			} else return
			ev.preventDefault()
			this.local = movePoint(this.local, i, t, p, this.minPct, this.limits)
			this.commit()
		},
		add() {
			const next = addPoint(this.local, null, this.limits)
			if (next !== this.local) {
				this.local = next
				this.commit()
			}
		},
		remove() {
			if (this.selected === null) return
			const next = removePoint(this.local, this.selected, this.limits)
			if (next !== this.local) {
				this.local = next
				this.selected = null
				this.commit()
			}
		},
		commit() {
			this.$emit('change', this.local.map((p) => ({ ...p })))
		}
	}
}
</script>

<style lang="scss" scoped>
.fan-curve {
	width: 100%;
}

.curve-svg {
	width: 100%;
	height: auto;
	max-height: 260px;
	display: block;
	touch-action: none;
	user-select: none;
}

.grid line {
	stroke: var(--theme-card-border, rgba(0, 0, 0, 0.08));
	stroke-width: 1;
}

.axis text {
	font-size: 10px;
	fill: var(--theme-text-muted, #94a3b8);
	font-variant-numeric: tabular-nums;
}

.floor-zone {
	fill: var(--theme-text-muted, #94a3b8);
	opacity: 0.1;
}

.crit-zone {
	fill: var(--color-danger, #ef4444);
	opacity: 0.08;
}

.curve-fill {
	fill: var(--color-primary, #2563eb);
	opacity: 0.08;
}

.curve-line {
	fill: none;
	stroke: var(--color-primary, #2563eb);
	stroke-width: 2.25;
	stroke-linejoin: round;
}

.now {
	line {
		stroke: var(--color-warning, #f59e0b);
		stroke-width: 1.25;
		stroke-dasharray: 3 3;
	}

	circle {
		fill: var(--color-warning, #f59e0b);
	}
}

.handle {
	cursor: grab;
	outline: none;

	.hit {
		fill: transparent;
	}

	.dot {
		fill: var(--theme-card-bg, #ffffff);
		stroke: var(--color-primary, #2563eb);
		stroke-width: 2.5;
		transition: r 0.1s ease;
	}

	&:hover .dot,
	&.selected .dot,
	&:focus-visible .dot {
		fill: var(--color-primary, #2563eb);
	}

	&.active {
		cursor: grabbing;
	}
}

.is-readonly .handle {
	cursor: default;
}

.curve-foot {
	display: flex;
	flex-wrap: wrap;
	align-items: center;
	justify-content: space-between;
	gap: var(--space-2, 0.5rem);
	margin-top: var(--space-2, 0.5rem);
	font-size: 0.775rem;
	color: var(--theme-text-muted, #64748b);

	b {
		color: var(--theme-text-primary, #1e293b);
		font-weight: 600;
		font-variant-numeric: tabular-nums;
	}
}

.curve-actions {
	display: inline-flex;
	gap: var(--space-2, 0.5rem);
}
</style>
