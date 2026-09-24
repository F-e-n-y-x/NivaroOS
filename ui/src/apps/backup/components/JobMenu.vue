<!-- The ⋯ menu of a job (spec §12.3): role=menu with arrow keys, Home/End,
     Esc (focus back to the button) and Tab to leave. -->
<template>
	<div class="bk-menu-wrap" @keydown="onKeydown">
		<button
			ref="trigger"
			type="button"
			class="bk-icon-btn"
			aria-haspopup="menu"
			:aria-expanded="open ? 'true' : 'false'"
			:aria-controls="open ? menuId : null"
			:aria-label="$t('backup.jobs.more_named', { name: job.name })"
			:title="$t('backup.jobs.more')"
			@click="toggle"
		>
			<b-icon icon="dots-horizontal" pack="mdi" custom-size="mdi-20px"></b-icon>
		</button>
		<ul v-if="open" :id="menuId" ref="menu" class="bk-menu" role="menu" :aria-label="$t('backup.jobs.more_named', { name: job.name })">
			<li v-for="item in items" :key="item.id" role="none">
				<button type="button" role="menuitem" tabindex="-1" class="bk-menu-item" :class="{ 'is-danger': item.danger }" @click="choose(item.id)">
					<b-icon :icon="item.icon" pack="mdi" custom-size="mdi-18px" aria-hidden="true"></b-icon>
					<span>{{ $t(item.label) }}</span>
				</button>
			</li>
		</ul>
	</div>
</template>

<script>
let seq = 0

export default {
	name: 'JobMenu',
	props: {
		job: { type: Object, required: true }
	},
	data() {
		return { open: false, menuId: `bk-job-menu-${++seq}` }
	},
	computed: {
		items() {
			const running = !!(this.job.active_run && ['queued', 'running', 'waiting_user'].includes(this.job.active_run.status))
			const list = [
				{ id: 'edit', icon: 'pencil-outline', label: 'backup.jobs.menu.edit' },
				{ id: 'duplicate', icon: 'content-duplicate', label: 'backup.jobs.menu.duplicate' }
			]
			if (!running) list.push({ id: 'preview', icon: 'eye-outline', label: 'backup.jobs.menu.preview' })
			list.push(this.job.enabled ? { id: 'pause', icon: 'pause-circle-outline', label: 'backup.jobs.menu.pause' } : { id: 'resume', icon: 'play-circle-outline', label: 'backup.jobs.menu.resume' })
			list.push({ id: 'delete', icon: 'trash-can-outline', label: 'backup.jobs.menu.delete', danger: true })
			return list
		}
	},
	beforeDestroy() {
		document.removeEventListener('pointerdown', this.onOutside, true)
	},
	methods: {
		toggle() {
			if (this.open) this.close(true)
			else this.show(0)
		},
		show(index) {
			this.open = true
			document.addEventListener('pointerdown', this.onOutside, true)
			this.$nextTick(() => this.focusItem(index))
		},
		close(returnFocus) {
			this.open = false
			document.removeEventListener('pointerdown', this.onOutside, true)
			if (returnFocus && this.$refs.trigger) this.$refs.trigger.focus()
		},
		onOutside(e) {
			if (!this.$el.contains(e.target)) this.close(false)
		},
		menuItems() {
			return this.$refs.menu ? Array.from(this.$refs.menu.querySelectorAll('[role="menuitem"]')) : []
		},
		focusItem(i) {
			const items = this.menuItems()
			if (items.length) items[(i + items.length) % items.length].focus()
		},
		onKeydown(e) {
			if (!this.open) {
				if (e.target === this.$refs.trigger && (e.key === 'ArrowDown' || e.key === 'ArrowUp')) {
					e.preventDefault()
					this.show(e.key === 'ArrowUp' ? -1 : 0)
				}
				return
			}
			const items = this.menuItems()
			const cur = items.indexOf(document.activeElement)
			switch (e.key) {
				case 'ArrowDown':
					e.preventDefault()
					this.focusItem(cur + 1)
					break
				case 'ArrowUp':
					e.preventDefault()
					this.focusItem(cur < 0 ? -1 : cur - 1)
					break
				case 'Home':
					e.preventDefault()
					this.focusItem(0)
					break
				case 'End':
					e.preventDefault()
					this.focusItem(-1)
					break
				case 'Escape':
					// Only the menu closes, not the window.
					e.preventDefault()
					e.stopPropagation()
					this.close(true)
					break
				case 'Tab':
					this.close(false)
					break
			}
		},
		choose(id) {
			this.close(true)
			this.$emit('select', id)
		}
	}
}
</script>

<style lang="scss" scoped>
.bk-menu-wrap {
	position: relative;
}
.bk-menu {
	position: absolute;
	right: 0;
	top: calc(100% + var(--space-1));
	z-index: 20;
	min-width: 12rem;
	margin: 0;
	padding: var(--space-1);
	list-style: none;
	background: var(--theme-dropdown-bg);
	border: 1px solid var(--theme-dropdown-border);
	border-radius: var(--radius-control);
	box-shadow: var(--shadow-md);
}
.bk-menu-item {
	display: flex;
	align-items: center;
	gap: var(--space-2);
	width: 100%;
	min-height: 2.25rem;
	padding: var(--space-1) var(--space-2);
	border: none;
	border-radius: var(--radius-sm);
	background: transparent;
	color: var(--theme-text-primary);
	font-family: inherit;
	font-size: var(--font-sm);
	text-align: left;
	cursor: pointer;
	&:hover,
	&:focus {
		background: var(--theme-card-hover);
		outline: none;
	}
	&:focus-visible {
		outline: 2px solid var(--color-primary-fg);
		outline-offset: -2px;
	}
	&.is-danger {
		color: var(--color-danger-fg);
	}
}
</style>
