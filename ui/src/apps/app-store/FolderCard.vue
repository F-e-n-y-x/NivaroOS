<template>
	<div :class="{ 'drag-target': isDragTarget }" :data-folder-id="folder.id"
		class="common-card is-flex is-align-items-center is-justify-content-center app-card folder-card"
		@contextmenu.prevent.stop="handleCardContextMenu">

		<!-- Desktop Context Menu (portal mounted to body on open) -->
		<div
			v-show="menuVisible"
			ref="menu"
			class="desktop-context-menu"
			:style="{ top: menuY + 'px', left: menuX + 'px' }"
			@contextmenu.prevent.stop
		>
			<button class="ctx-item" @click="closeMenuThen('open', folder)">
				<i class="mdi mdi-folder-open-outline ctx-icon"></i>
				<span class="ctx-label">{{ $t('Open') }}</span>
			</button>
			<button class="ctx-item" @click="closeMenuThen('rename', folder)">
				<i class="mdi mdi-pencil-outline ctx-icon"></i>
				<span class="ctx-label">{{ $t('Rename') }}</span>
			</button>
			<button class="ctx-item" @click="closeMenuThen('editIcon', folder)">
				<i class="mdi mdi-image-edit-outline ctx-icon"></i>
				<span class="ctx-label">{{ $t('Edit icon') }}</span>
			</button>
			<div class="ctx-divider"></div>
			<button class="ctx-item is-danger" @click="closeMenuThen('delete', folder)">
				<i class="mdi mdi-trash-can-outline ctx-icon"></i>
				<span class="ctx-label">{{ $t('Delete folder') }}</span>
			</button>
		</div>

		<div class="blur-background"></div>
		<div class="cards-content" role="button" tabindex="0" aria-haspopup="menu"
			:aria-label="$t('Folder {name}, {count} apps', { name: folder.name, count: (folder.apps || []).length })"
			@click="handleFolderClick" @dblclick="handleFolderDblClick"
			@keydown.enter.self.prevent="$emit('open', folder)" @keydown.space.self.prevent="$emit('open', folder)"
			@keydown.self="handleFolderKeydown">
			<div class="has-text-centered is-flex is-justify-content-center is-flex-direction-column icon-cell">
				<div class="is-flex is-justify-content-center">
					<div v-if="folder.icon" class="folder-custom-icon is-52x52" :style="{ borderRadius: (folder.iconRadius || 0) + '%' }">
						<img :src="folder.icon" :alt="folder.name || ''">
					</div>
					<div v-else class="folder-icon-grid is-52x52">
						<div v-for="i in 4" :key="i" class="folder-icon-cell">
							<b-image v-if="previewApps[i - 1]" :src="previewApps[i - 1].icon" alt=""
								:src-fallback="$assetUrl(require('@/assets/img/app-icons/default.svg'))" webp-fallback=".jpg"></b-image>
						</div>
					</div>
				</div>
				<p class="app-label one-line">
					<a class="one-line" style="cursor:default">{{ folder.name }}</a>
				</p>
			</div>
		</div>
	</div>
</template>

<script>
const MENU_WIDTH = 224

export default {
	name: 'folder-card',
	props: {
		folder: {
			type: Object,
			required: true
		},
		isDragTarget: {
			type: Boolean,
			default: false
		}
	},
	data() {
		return {
			menuVisible: false,
			menuX: 0,
			menuY: 0
		}
	},
	computed: {
		previewApps() {
			return (this.folder.apps || []).slice(0, 4)
		}
	},
	mounted() {
		this.handleCloseOtherMenus = sender => {
			if (sender !== this) {
				this.closeMenu()
			}
		}
		this.$EventBus.$on('CLOSE_ALL_CONTEXT_MENUS', this.handleCloseOtherMenus)
	},
	beforeDestroy() {
		this.closeMenu()
		this.$EventBus.$off('CLOSE_ALL_CONTEXT_MENUS', this.handleCloseOtherMenus)
		if (this.$refs.menu && this.$refs.menu.parentNode) {
			this.$refs.menu.parentNode.removeChild(this.$refs.menu)
		}
	},
	methods: {
		handleFolderClick() {
			if (this.$store.state.isMobile) {
				this.$emit('open', this.folder)
			}
		},
		handleFolderDblClick() {
			this.$emit('open', this.folder)
		},
		handleFolderKeydown(event) {
			// Shift+F10 / the ContextMenu key open the menu from the keyboard.
			if ((event.key === 'F10' && event.shiftKey) || event.key === 'ContextMenu') {
				event.preventDefault()
				event.stopPropagation()
				const rect = event.currentTarget.getBoundingClientRect()
				this.handleCardContextMenu({
					clientX: rect.left + rect.width / 2,
					clientY: rect.top + rect.height / 2,
					fromKeyboard: true,
					preventDefault() {},
					stopPropagation() {}
				})
			}
		},
		handleCardContextMenu(event) {
			if (event) {
				event.preventDefault()
				event.stopPropagation()
			}
			this.menuOpenedByKeyboard = !!(event && event.fromKeyboard)

			this.$EventBus.$emit('CLOSE_ALL_CONTEXT_MENUS', this)

			const targetContainer = document.fullscreenElement || document.body
			if (this.$refs.menu && this.$refs.menu.parentNode !== targetContainer) {
				targetContainer.appendChild(this.$refs.menu)
			}

			const clientX = event ? event.clientX : window.innerWidth / 2
			const clientY = event ? event.clientY : window.innerHeight / 2

			let x = Math.max(12, Math.min(window.innerWidth - MENU_WIDTH - 16, clientX))
			let y = Math.max(12, clientY)

			this.menuX = x
			this.menuY = y
			this.menuVisible = true

			this.addEventListeners()

			this.$nextTick(() => {
				if (!this.$refs.menu) return
				const rect = this.$refs.menu.getBoundingClientRect()
				const maxBottom = window.innerHeight - 80 // Above taskbar dock
				if (rect.bottom > maxBottom) {
					const adjustedY = Math.max(12, clientY - rect.height)
					this.menuY = Math.min(adjustedY, maxBottom - rect.height)
				}
				if (rect.right > window.innerWidth - 12) {
					this.menuX = Math.max(12, window.innerWidth - rect.width - 12)
				}
				if (this.menuOpenedByKeyboard) {
					const first = this.$refs.menu.querySelector('.ctx-item:not([disabled])')
					if (first) first.focus()
				}
			})
		},
		closeMenu() {
			if (!this.menuVisible) return
			this.menuVisible = false
			this.removeEventListeners()
			if (this.menuOpenedByKeyboard) {
				this.menuOpenedByKeyboard = false
				const card = this.$el && this.$el.querySelector && this.$el.querySelector('.cards-content')
				if (card) card.focus()
			}
		},
		closeMenuThen(eventName, ...args) {
			this.closeMenu()
			this.$emit(eventName, ...args)
		},
		onOutsideClick(event) {
			if (this.menuVisible && this.$refs.menu && !this.$refs.menu.contains(event.target)) {
				this.closeMenu()
			}
		},
		onKeyDown(event) {
			if (event.key === 'Escape' && this.menuVisible) {
				this.closeMenu()
				return
			}
			if (this.menuVisible && this.$refs.menu && (event.key === 'ArrowDown' || event.key === 'ArrowUp')) {
				const items = Array.from(this.$refs.menu.querySelectorAll('.ctx-item:not([disabled])'))
				if (!items.length) return
				event.preventDefault()
				const idx = items.indexOf(document.activeElement)
				const next = event.key === 'ArrowDown'
					? items[(idx + 1) % items.length]
					: items[(idx - 1 + items.length) % items.length]
				next.focus()
			}
		},
		addEventListeners() {
			document.addEventListener('mousedown', this.onOutsideClick)
			document.addEventListener('keydown', this.onKeyDown)
			window.addEventListener('blur', this.closeMenu)
			window.addEventListener('resize', this.closeMenu)
			window.addEventListener('scroll', this.closeMenu, true)
		},
		removeEventListeners() {
			document.removeEventListener('mousedown', this.onOutsideClick)
			document.removeEventListener('keydown', this.onKeyDown)
			window.removeEventListener('blur', this.closeMenu)
			window.removeEventListener('resize', this.closeMenu)
			window.removeEventListener('scroll', this.closeMenu, true)
		}
	}
}
</script>

<style lang="scss" scoped>
.folder-card.drag-target {
	transform: scale(1.12);
	transition: transform 0.15s ease;

	.blur-background {
		opacity: 1;
		background-color: rgba(255, 255, 255, 0.25);
	}

	.folder-icon-grid {
		animation: folder-drag-pulse 0.6s ease-in-out infinite;
	}
}

@keyframes folder-drag-pulse {
	0%, 100% {
		box-shadow: 0 0 0 0 rgba(255, 255, 255, 0.4);
	}
	50% {
		box-shadow: 0 0 0 6px rgba(255, 255, 255, 0.15);
	}
}

.cards-content:focus {
	outline: none;
}

.cards-content:focus-visible {
	outline: 2px solid var(--color-primary-fg);
	outline-offset: 2px;
	border-radius: var(--radius-card);
}

.folder-custom-icon {
	overflow: hidden;
	border-radius: var(--radius-card);

	img {
		width: 100%;
		height: 100%;
		object-fit: cover;
	}
}

.folder-icon-grid {
	display: grid;
	grid-template-columns: repeat(2, 1fr);
	grid-template-rows: repeat(2, 1fr);
	gap: 2px;
	border-radius: var(--radius-card);
	overflow: hidden;
	background: rgba(255, 255, 255, 0.1);
	padding: 2px;
}

.folder-icon-cell {
	background: rgba(255, 255, 255, 0.08);
	border-radius: var(--radius-xs);
	overflow: hidden;

	img {
		width: 100%;
		height: 100%;
		object-fit: cover;
	}
}

.icon-cell {
	width: 100%;
	height: 100%;
	padding: var(--space-2) var(--space-1) var(--space-1);
	box-sizing: border-box;
	justify-content: center;
	gap: 0;
}

.app-label {
	margin-top: var(--space-1);
	font-size: var(--font-xs);
	font-weight: 500;
	color: #fff;
	text-shadow: 0 1px 3px rgba(0, 0, 0, 0.85);
	line-height: 1.2;
	max-width: 84px;
	overflow: hidden;
	text-overflow: ellipsis;
	white-space: nowrap;

	// The label text sits inside a bare <a> (see template) - forced with
	// !important since a plain `color: inherit` here can lose a
	// same-specificity tie against _card.scss's `.common-card a { color:
	// white }` depending on runtime style-injection order.
	a {
		color: #fff !important;
		text-decoration: none;
	}
}
</style>
