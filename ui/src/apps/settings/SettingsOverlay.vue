<!--
	A Settings dialog shown as a real desktop window.

	It used to be a full-screen overlay: a blurred backdrop over the whole
	desktop that blocked every other window until it was closed. Now opening
	it creates an ordinary window (PortalWindow) and moves this dialog's
	content into it - it can be moved, stacked, minimised and used side by
	side with anything else. The content stays this component's own (slots,
	bindings, events), so callers didn't change: `active`, `title`, `width`,
	`@close`, default + `footer` slots.
-->
<template>
	<div class="settings-overlay-home" hidden>
		<div v-if="active" ref="content" class="settings-overlay-content" @keydown.esc.stop="$emit('close')">
			<div class="settings-overlay-body" :class="bodyClass">
				<slot></slot>
			</div>
			<footer v-if="$slots.footer" class="settings-overlay-foot">
				<slot name="footer"></slot>
			</footer>
		</div>
	</div>
</template>

<script>
export default {
	name: 'settings-overlay',
	props: {
		active: { type: Boolean, default: false },
		title: { type: String, default: '' },
		width: { type: [String, Number], default: '32rem' },
		bodyClass: { type: String, default: '' }
	},
	data() {
		return {
			windowId: 'settings-dialog-' + Math.random().toString(36).slice(2, 9),
			open: false
		}
	},
	computed: {
		widthPx() {
			if (typeof this.width === 'number') return this.width
			const m = String(this.width).match(/^([\d.]+)(rem|px)?$/)
			if (!m) return 520
			return m[2] === 'px' ? +m[1] : Math.round(+m[1] * 16)
		},
		// Used to notice the window being closed from its own title bar.
		windowStillOpen() {
			return this.$store.state.windows.some(w => w.id === this.windowId)
		}
	},
	watch: {
		active: {
			immediate: true,
			handler(val) {
				if (val) this.openWindow()
				else this.closeWindow()
			}
		},
		windowStillOpen(open) {
			if (!open && this.open) {
				this.open = false
				this.$emit('close')
			}
		},
		title(t) {
			const win = this.$store.state.windows.find(w => w.id === this.windowId)
			if (win) win.title = t
		}
	},
	beforeDestroy() {
		this.closeWindow()
	},
	methods: {
		openWindow() {
			const width = Math.min(this.widthPx, window.innerWidth - 32)
			const height = Math.min(480, window.innerHeight - 96)
			this.$store.commit('OPEN_WINDOW', {
				id: this.windowId,
				title: this.title,
				component: 'PortalWindow',
				props: { portalId: this.windowId },
				width,
				height,
				x: Math.max(16, Math.round((window.innerWidth - width) / 2)),
				y: Math.max(40, Math.round((window.innerHeight - height) / 3))
			})
			this.open = true
			this.moveIn(0)
		},
		// Wait for the window (and our v-if content) to exist, then move the
		// content into it and fit the window to it.
		moveIn(tries) {
			this.$nextTick(() => {
				const target = document.getElementById('portal-' + this.windowId)
				const content = this.$refs.content
				if (!target || !content) {
					if (tries < 20 && this.open) setTimeout(() => this.moveIn(tries + 1), 25)
					return
				}
				target.appendChild(content)
				const first = content.querySelector('input, select, textarea, button')
				if (first) first.focus()
				// Grow to the content (up to most of the screen).
				const want = Math.min(content.scrollHeight + 52, window.innerHeight - 96)
				const win = this.$store.state.windows.find(w => w.id === this.windowId)
				if (win && want > 120) this.$store.commit('UPDATE_WINDOW_RECT', { id: this.windowId, x: win.x, y: win.y, width: win.width, height: want })
			})
		},
		closeWindow() {
			if (!this.open) return
			this.open = false
			if (this.windowStillOpen) this.$store.commit('CLOSE_WINDOW', this.windowId)
		}
	}
}
</script>

<style lang="scss" scoped>
.settings-overlay-content {
	display: flex;
	flex-direction: column;
	min-height: 100%;
	color: var(--theme-text-primary, #334155);
}

.settings-overlay-body {
	flex: 1 1 auto;
	padding: var(--space-5);
	color: var(--theme-text-secondary, #334155);
	font-size: var(--font-base);
}

.settings-overlay-foot {
	position: sticky;
	bottom: 0;
	display: flex;
	align-items: center;
	justify-content: flex-end;
	gap: var(--space-2);
	padding: var(--space-3) var(--space-5);
	border-top: 1px solid var(--theme-card-border, #f1f5f9);
	background: var(--theme-input-bg, #f8fafc);
}
</style>
