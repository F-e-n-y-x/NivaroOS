<!-- src/apps/download-station/rb/DsBrowserMenu.vue -->
<!-- A small menu at a point: the browser's right-click menu, a tab's menu
     and the Back/Forward history lists. Sections are separated by a rule;
     arrow keys, Home/End, Enter and Esc work. Rendered on <body> so no
     window's transform or overflow clips it. -->
<template>
	<div class="rb-menu-layer" @mousedown.self="close" @contextmenu.prevent.self="close" @wheel.self="close">
		<div ref="menu" class="rb-menu" role="menu" :aria-label="label" tabindex="-1" :style="style" @keydown="onKey">
			<div v-if="title" class="rb-menu-title one-line">{{ title }}</div>
			<template v-for="(section, si) in sections">
				<div v-if="si > 0 && section.length" :key="'sep' + si" class="rb-menu-sep" role="separator"></div>
				<button v-for="item in section" :key="si + '-' + item.id" type="button" role="menuitem" class="rb-menu-item"
					:class="{ 'is-primary': item.primary, 'is-active': flat.indexOf(item) === index, 'is-checked': item.checked }"
					:disabled="item.disabled" :aria-disabled="item.disabled ? 'true' : 'false'"
					@mouseenter="index = flat.indexOf(item)" @click="choose(item)">
					<span class="rb-menu-icon">
						<img v-if="item.image" :src="item.image" alt="" />
						<b-icon v-else-if="item.checked" icon="check" custom-size="mdi-16px"></b-icon>
						<b-icon v-else-if="item.icon" :icon="item.icon" custom-size="mdi-16px"></b-icon>
					</span>
					<span class="rb-menu-label one-line">{{ $t(item.label) }}</span>
					<span v-if="item.hint" class="rb-menu-hint">{{ item.hint }}</span>
				</button>
			</template>
		</div>
	</div>
</template>

<script>
import { stepMenu } from './rbClient'

export default {
	emits: ['close', 'select'],
	name: 'ds-browser-menu',
	props: {
		sections: { type: Array, required: true },
		x: { type: Number, required: true },
		y: { type: Number, required: true },
		title: { type: String, default: '' },
		label: { type: String, default: 'Menu' }
	},
	data() {
		return { index: -1, style: { left: this.x + 'px', top: this.y + 'px', visibility: 'hidden' } }
	},
	computed: {
		flat() {
			return this.sections.flat()
		}
	},
	mounted() {
		document.body.appendChild(this.$el)
		this.$nextTick(() => {
			// Keep the whole menu on screen: flip left/up near an edge.
			const m = this.$refs.menu
			const w = m.offsetWidth
			const h = m.offsetHeight
			const vw = window.innerWidth
			const vh = window.innerHeight
			let left = this.x
			let top = this.y
			if (left + w > vw - 4) left = Math.max(4, left - w)
			if (top + h > vh - 4) top = Math.max(4, vh - h - 4)
			this.style = { left: left + 'px', top: top + 'px' }
			m.focus({ preventScroll: true })
		})
		window.addEventListener('blur', this.close)
		window.addEventListener('resize', this.close)
	},
	beforeUnmount() {
		window.removeEventListener('blur', this.close)
		window.removeEventListener('resize', this.close)
		if (this.$el.parentNode) this.$el.parentNode.removeChild(this.$el)
	},
	methods: {
		close() {
			this.$emit('close')
		},
		choose(item) {
			if (item.disabled) return
			this.$emit('select', item)
			this.$emit('close')
		},
		onKey(e) {
			const items = this.flat
			switch (e.key) {
				case 'ArrowDown':
					this.index = stepMenu(items, this.index < 0 ? -1 : this.index, 1)
					break
				case 'ArrowUp':
					this.index = stepMenu(items, this.index < 0 ? 0 : this.index, -1)
					break
				case 'Home':
					this.index = stepMenu(items, -1, 1)
					break
				case 'End':
					this.index = stepMenu(items, 0, -1)
					break
				case 'Enter':
				case ' ':
					if (this.index >= 0) this.choose(items[this.index])
					break
				case 'Escape':
				case 'Tab':
					this.close()
					break
				default:
					return
			}
			e.preventDefault()
			e.stopPropagation()
		}
	}
}
</script>

<style lang="scss" scoped>
.rb-menu-layer {
	position: fixed;
	inset: 0;
	z-index: 9000;
}

.rb-menu {
	position: fixed;
	min-width: 15rem;
	max-width: 22rem;
	max-height: calc(100vh - 8px);
	overflow-y: auto;
	padding: var(--space-1) 0;
	border-radius: var(--radius-md, 10px);
	border: 1px solid var(--theme-card-border, rgba(0, 0, 0, 0.08));
	background: var(--theme-card-bg, #fff);
	color: var(--theme-text-primary, #1e293b);
	box-shadow: 0 12px 32px rgba(15, 23, 42, 0.18), 0 2px 6px rgba(15, 23, 42, 0.08);
	outline: none;
	font-size: var(--font-sm);
	animation: rb-menu-in 0.09s ease-out;
}

@keyframes rb-menu-in {
	from {
		opacity: 0;
		transform: translateY(-2px);
	}
}

@media (prefers-reduced-motion: reduce) {
	.rb-menu {
		animation: none;
	}
}

.rb-menu-title {
	padding: var(--space-1) var(--space-3) var(--space-2);
	font-size: var(--font-xs);
	color: var(--theme-text-muted, #64748b);
}

.rb-menu-item {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	width: 100%;
	min-height: 2rem;
	padding: 0 var(--space-3) 0 var(--space-2);
	border: none;
	background: transparent;
	color: inherit;
	font: inherit;
	text-align: left;
	cursor: pointer;

	&.is-active:not(:disabled) {
		background: var(--theme-card-hover, rgba(0, 0, 0, 0.05));
	}

	&.is-primary .rb-menu-label {
		color: var(--color-primary-fg, #1d4ed8);
		font-weight: 500;
	}

	&.is-primary .rb-menu-icon {
		color: var(--color-primary-fg, #1d4ed8);
	}

	&:disabled {
		cursor: default;
		opacity: 0.4;
	}
}

.rb-menu-icon {
	flex-shrink: 0;
	display: inline-flex;
	align-items: center;
	justify-content: center;
	width: 1.25rem;
	color: var(--theme-text-secondary, #64748b);

	img {
		width: 16px;
		height: 16px;
	}
}

.rb-menu-label {
	flex: 1 1 auto;
	min-width: 0;
}

.rb-menu-hint {
	flex-shrink: 0;
	margin-left: var(--space-4);
	font-size: var(--font-xs);
	color: var(--theme-text-muted, #94a3b8);
}

.rb-menu-sep {
	height: 1px;
	margin: var(--space-1) 0;
	background: var(--theme-card-border, rgba(0, 0, 0, 0.08));
}

.one-line {
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;
}
</style>
