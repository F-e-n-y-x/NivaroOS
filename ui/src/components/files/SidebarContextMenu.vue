<!-- src/components/files/SidebarContextMenu.vue -->
<template>
	<div
		v-show="visible"
		ref="menu"
		class="sidebar-context-menu"
		:style="{ top: y + 'px', left: x + 'px' }"
		@contextmenu.prevent.stop
	>
		<button class="ctx-item" @click="act('open')">
			<i class="mdi mdi-folder-open-outline ctx-icon"></i>
			<span class="ctx-label">{{ $t('Open') }}</span>
		</button>
		<button class="ctx-item" @click="act('open-new-tab')">
			<i class="mdi mdi-tab-plus ctx-icon"></i>
			<span class="ctx-label">{{ $t('Open in New Tab') }}</span>
		</button>
		<button class="ctx-item" @click="act('open-new-window')">
			<i class="mdi mdi-open-in-new ctx-icon"></i>
			<span class="ctx-label">{{ $t('Open in New Window') }}</span>
		</button>

		<div class="ctx-divider"></div>

		<button class="ctx-item" @click="act('toggle-favorite')">
			<i :class="isFavorite ? 'mdi mdi-star text-amber-500' : 'mdi mdi-star-outline'" class="ctx-icon"></i>
			<span class="ctx-label">{{ isFavorite ? $t('Remove from Favorite') : $t('Add to Favorite') }}</span>
		</button>
		<button class="ctx-item" @click="act('copy-path')">
			<i class="mdi mdi-content-copy ctx-icon"></i>
			<span class="ctx-label">{{ $t('Copy Path') }}</span>
		</button>

		<!-- Eject / Disconnect option for removable mounts -->
		<template v-if="isRemovable">
			<div class="ctx-divider"></div>
			<button class="ctx-item is-warning" @click="act('eject')">
				<i :class="ejectIcon" class="ctx-icon"></i>
				<span class="ctx-label">{{ ejectLabel }}</span>
			</button>
		</template>

		<template v-if="item && item.path">
			<div class="ctx-divider"></div>
			<button class="ctx-item" @click="act('detail')">
				<i class="mdi mdi-information-outline ctx-icon"></i>
				<span class="ctx-label">{{ $t('Detail') }}</span>
			</button>
		</template>
	</div>
</template>

<script>
import events from '@/events/events'

const MENU_WIDTH = 210
const MENU_HEIGHT = 280

export default {
	name: 'sidebar-context-menu',
	inject: ['filesController'],
	data() {
		return {
			visible: false,
			x: 0,
			y: 0,
			item: null,
			mountType: null,
		}
	},
	computed: {
		isFavorite() {
			if (!this.item || !this.item.path) return false
			const shortcuts = this.$store.state.shortcutData || []
			return shortcuts.some((s) => s.path === this.item.path)
		},
		isRemovable() {
			return ['usb', 'network', 'cloud'].includes(this.mountType)
		},
		ejectIcon() {
			if (this.mountType === 'network') return 'mdi mdi-lan-disconnect'
			if (this.mountType === 'cloud') return 'mdi mdi-cloud-off-outline'
			return 'mdi mdi-eject-outline'
		},
		ejectLabel() {
			if (this.mountType === 'network') return this.$t('Disconnect')
			return this.$t('Eject')
		},
	},
	mounted() {
		document.addEventListener('mousedown', this.onOutsideClick)
		window.addEventListener('resize', this.close)
	},
	beforeDestroy() {
		document.removeEventListener('mousedown', this.onOutsideClick)
		window.removeEventListener('resize', this.close)
	},
	methods: {
		open(event, item, mountType = null) {
			if (event && event.stopPropagation) {
				event.stopPropagation()
			}
			this.item = item
			this.mountType = mountType || (item && item.mountType) || null

			const clientX = event && typeof event.clientX === 'number' ? event.clientX : 100
			const clientY = event && typeof event.clientY === 'number' ? event.clientY : 100

			const maxLeft = Math.max(12, window.innerWidth - MENU_WIDTH - 16)
			const maxTop = Math.max(12, window.innerHeight - MENU_HEIGHT - 60)

			this.x = Math.max(12, Math.min(maxLeft, clientX))
			this.y = Math.max(12, Math.min(maxTop, clientY))
			this.visible = true
		},
		close() {
			this.visible = false
		},
		onOutsideClick(event) {
			if (event && event.button === 2) return
			if (this.visible && this.$refs.menu && !this.$refs.menu.contains(event.target)) {
				this.close()
			}
		},
		act(action) {
			if (!this.item) {
				this.close()
				return
			}
			switch (action) {
				case 'open':
					if (this.filesController && this.filesController.navigate) {
						this.filesController.navigate(this.item.path)
					}
					break
				case 'open-new-tab':
					if (this.filesController && this.filesController.newTab) {
						this.filesController.newTab(this.item.path)
					}
					break
				case 'open-new-window':
					if (this.filesController && this.filesController.openNewWindow) {
						this.filesController.openNewWindow(this.item.path)
					}
					break
				case 'toggle-favorite':
					if (this.isFavorite) {
						this.removeFromFavorite()
					} else {
						this.addToFavorite()
					}
					break
				case 'copy-path':
					this.copyPath()
					break
				case 'eject':
					this.$emit('eject', this.item, this.mountType)
					break
				case 'detail':
					if (this.filesController && this.filesController.openDetail) {
						this.filesController.openDetail({
							name: this.item.name,
							path: this.item.path,
							is_dir: true,
							...this.item,
						})
					}
					break
			}
			this.close()
		},
		addToFavorite() {
			if (!this.item) return
			const currentShortcuts = this.$store.state.shortcutData || []
			if (currentShortcuts.some((s) => s.path === this.item.path)) return
			const shortcut = [
				...currentShortcuts,
				{
					name: this.item.name || this.item.path.split('/').pop() || this.item.path,
					path: this.item.path,
					icon: 'folder-outline',
					pack: 'casa',
				},
			]
			this.$store.dispatch('SET_SHORTCUT_DATA', shortcut).then(() => {
				this.$EventBus.$emit(events.RELOAD_FILE_LIST)
				this.$buefy.toast.open({ message: this.$t('Added to favorites'), type: 'is-success' })
			})
		},
		removeFromFavorite() {
			if (!this.item) return
			const currentShortcuts = this.$store.state.shortcutData || []
			const shortcut = currentShortcuts.filter((s) => s.path !== this.item.path)
			this.$store.dispatch('SET_SHORTCUT_DATA', shortcut).then(() => {
				this.$EventBus.$emit(events.RELOAD_FILE_LIST)
				this.$buefy.toast.open({ message: this.$t('Removed from favorites'), type: 'is-success' })
			})
		},
		copyPath() {
			if (!this.item || !this.item.path) return
			if (navigator.clipboard && navigator.clipboard.writeText) {
				navigator.clipboard
					.writeText(this.item.path)
					.then(() => {
						this.$buefy.toast.open({
							message: this.$t('Path copied to clipboard'),
							type: 'is-success',
							position: 'is-top',
							duration: 2000,
						})
					})
					.catch(() => {
						this.fallbackCopyText(this.item.path)
					})
			} else {
				this.fallbackCopyText(this.item.path)
			}
		},
		fallbackCopyText(text) {
			try {
				const textArea = document.createElement('textarea')
				textArea.value = text
				textArea.style.position = 'fixed'
				textArea.style.opacity = '0'
				document.body.appendChild(textArea)
				textArea.focus()
				textArea.select()
				document.execCommand('copy')
				document.body.removeChild(textArea)
				this.$buefy.toast.open({
					message: this.$t('Path copied to clipboard'),
					type: 'is-success',
					position: 'is-top',
					duration: 2000,
				})
			} catch (e) {
				this.$buefy.toast.open({
					message: this.$t('Failed to copy path'),
					type: 'is-danger',
					position: 'is-top',
				})
			}
		},
	},
}
</script>

<style lang="scss" scoped>
.sidebar-context-menu {
	position: fixed;
	z-index: 999999;
	width: 210px;
	background: rgba(255, 255, 255, 0.88);
	backdrop-filter: blur(24px) saturate(180%);
	-webkit-backdrop-filter: blur(24px) saturate(180%);
	border: 1px solid rgba(255, 255, 255, 0.65);
	border-radius: 12px;
	box-shadow: 0 16px 36px rgba(0, 0, 0, 0.16), 0 2px 8px rgba(0, 0, 0, 0.08);
	padding: 0.35rem;
	user-select: none;
	animation: ctxFadeIn 0.12s cubic-bezier(0.16, 1, 0.3, 1);
}

@keyframes ctxFadeIn {
	from {
		opacity: 0;
		transform: scale(0.96) translateY(-4px);
	}
	to {
		opacity: 1;
		transform: scale(1) translateY(0);
	}
}

.ctx-item {
	display: flex;
	align-items: center;
	gap: 0.65rem;
	width: 100%;
	padding: 0.45rem 0.65rem;
	border-radius: 7px;
	font-family: inherit;
	font-size: 0.8125rem;
	font-weight: 500;
	color: #1e293b;
	transition: all 0.12s ease;
	cursor: pointer;
	border: none;
	background: transparent;
	text-align: left;

	.ctx-icon {
		font-size: 1.1rem;
		color: #475569;
		flex-shrink: 0;
		line-height: 1;
		transition: color 0.12s ease;
	}

	.ctx-label {
		flex: 1;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	&:hover {
		background: #2563eb;
		color: #ffffff;

		.ctx-icon {
			color: #ffffff;
		}
	}

	&.is-warning {
		color: #d97706;

		.ctx-icon {
			color: #d97706;
		}

		&:hover {
			background: #d97706;
			color: #ffffff;

			.ctx-icon {
				color: #ffffff;
			}
		}
	}

	&.is-danger {
		color: #dc2626;

		.ctx-icon {
			color: #dc2626;
		}

		&:hover {
			background: #dc2626;
			color: #ffffff;

			.ctx-icon {
				color: #ffffff;
			}
		}
	}

	&:active {
		transform: scale(0.98);
	}
}

.ctx-divider {
	height: 1px;
	margin: 0.35rem 0.4rem;
	background: rgba(0, 0, 0, 0.08);
}
</style>
