<!-- src/components/files/ContextMenu.vue -->
<template>
	<div
		v-if="visible"
		ref="menu"
		class="files-context-menu"
		:style="{ top: y + 'px', left: x + 'px' }"
		@contextmenu.prevent.stop
	>
		<!-- 1. BLANK SPACE CONTEXT MENU (when right clicking empty area) -->
		<template v-if="!item">
			<button class="menu-item" @click="act('new-folder')">
				<i class="mdi mdi-folder-plus-outline menu-icon"></i>
				<span class="menu-label">{{ $t('New Folder') }}</span>
			</button>
			<button class="menu-item" @click="act('new-file')">
				<i class="mdi mdi-file-plus-outline menu-icon"></i>
				<span class="menu-label">{{ $t('New File') }}</span>
			</button>
			<button class="menu-item" @click="act('upload')">
				<i class="mdi mdi-upload-outline menu-icon"></i>
				<span class="menu-label">{{ $t('Upload') }}</span>
			</button>
			<div class="menu-sep"></div>
			<button v-if="hasClipboard" class="menu-item" @click="act('paste')">
				<i class="mdi mdi-content-paste menu-icon"></i>
				<span class="menu-label">{{ $t('Paste') }}</span>
			</button>
			<button class="menu-item" @click="act('select-all')">
				<i class="mdi mdi-select-all menu-icon"></i>
				<span class="menu-label">{{ $t('Select All') }}</span>
			</button>
			<button class="menu-item" @click="act('reload')">
				<i class="mdi mdi-refresh menu-icon"></i>
				<span class="menu-label">{{ $t('Refresh') }}</span>
			</button>
			<div class="menu-sep"></div>
			<button class="menu-item" @click="act('open-window')">
				<i class="mdi mdi-open-in-new menu-icon"></i>
				<span class="menu-label">{{ $t('Open in New Window') }}</span>
			</button>
		</template>

		<!-- 2. ITEM CONTEXT MENU (when right clicking a file or folder) -->
		<template v-else>
			<button class="menu-item" @click="act('open')">
				<i class="mdi mdi-open-in-app menu-icon"></i>
				<span class="menu-label">{{ $t('Open') }}</span>
			</button>
			<button v-if="item.is_dir" class="menu-item" @click="act('open-new-tab')">
				<i class="mdi mdi-tab-plus menu-icon"></i>
				<span class="menu-label">{{ $t('Open in New Tab') }}</span>
			</button>
			<div class="menu-sep"></div>
			<button class="menu-item" @click="act('rename')">
				<i class="mdi mdi-pencil-outline menu-icon"></i>
				<span class="menu-label">{{ $t('Rename') }}</span>
			</button>
			<button class="menu-item" @click="act('copy')">
				<i class="mdi mdi-content-copy menu-icon"></i>
				<span class="menu-label">{{ $t('Copy') }}</span>
			</button>
			<button class="menu-item" @click="act('cut')">
				<i class="mdi mdi-content-cut menu-icon"></i>
				<span class="menu-label">{{ $t('Cut') }}</span>
			</button>
			<button class="menu-item" @click="act('download')">
				<i class="mdi mdi-download-outline menu-icon"></i>
				<span class="menu-label">{{ $t('Download') }}</span>
			</button>
			<button class="menu-item" @click="act('compress')">
				<i class="mdi mdi-folder-zip-outline menu-icon"></i>
				<span class="menu-label">{{ $t('Compress to Zip') }}</span>
			</button>
			<button v-if="isArchive" class="menu-item" @click="act('extract')">
				<i class="mdi mdi-archive-arrow-down-outline menu-icon"></i>
				<span class="menu-label">{{ $t('Extract') }}</span>
			</button>
			<template v-if="item.is_dir">
				<div class="menu-sep"></div>
				<button class="menu-item" @click="act('favorite')">
					<i :class="isFavorite ? 'mdi mdi-star text-amber-500' : 'mdi mdi-star-outline'" class="menu-icon"></i>
					<span class="menu-label">{{ isFavorite ? $t('Remove from Favorite') : $t('Add to Favorite') }}</span>
				</button>
				<button class="menu-item" @click="act('share')">
					<i class="mdi mdi-share-variant-outline menu-icon"></i>
					<span class="menu-label">{{ $t('Share') }}</span>
				</button>
			</template>
			<div class="menu-sep"></div>
			<button class="menu-item is-danger" @click="act('delete')">
				<i class="mdi mdi-trash-can-outline menu-icon"></i>
				<span class="menu-label">{{ $t('Delete') }}</span>
			</button>
			<button class="menu-item" @click="act('detail')">
				<i class="mdi mdi-information-outline menu-icon"></i>
				<span class="menu-label">{{ $t('Detail') }}</span>
			</button>
		</template>
	</div>
</template>

<script>
import { mixin } from '@/mixins/mixin'
import events from '@/events/events'
import { isArchive as isArchiveFile } from '@/utils/files/archive'

const MENU_WIDTH = 215
const MENU_HEIGHT = 400

export default {
	name: 'files-context-menu',
	mixins: [mixin],
	inject: ['filesController'],
	data() {
		return { visible: false, x: 0, y: 0, item: null }
	},
	computed: {
		isFavorite() {
			if (!this.item) return false
			const shortcuts = this.$store.state.shortcutData || []
			return shortcuts.some((s) => s.path === this.item.path)
		},
		isArchive() {
			return isArchiveFile(this.item)
		},
		hasClipboard() {
			return !!(this.$store.state.operateObject && this.$store.state.operateObject.item && this.$store.state.operateObject.item.length)
		}
	},
	mounted() {
		document.addEventListener('mousedown', this.onOutsideClick)
	},
	beforeDestroy() {
		document.removeEventListener('mousedown', this.onOutsideClick)
	},
	methods: {
		open(event, item, boundsEl) {
			this.item = item
			const bounds = boundsEl.getBoundingClientRect()
			const scrollLeft = boundsEl.scrollLeft
			const scrollTop = boundsEl.scrollTop
			const rawX = event.clientX - bounds.left + scrollLeft
			const rawY = event.clientY - bounds.top + scrollTop
			this.x = Math.max(scrollLeft + 4, Math.min(rawX, scrollLeft + bounds.width - MENU_WIDTH - 8))
			this.y = Math.max(scrollTop + 4, Math.min(rawY, scrollTop + bounds.height - (item ? MENU_HEIGHT : 260) - 8))
			this.visible = true
		},
		close() {
			this.visible = false
		},
		onOutsideClick(event) {
			if (this.visible && this.$refs.menu && !this.$refs.menu.contains(event.target)) {
				this.close()
			}
		},
		act(action) {
			switch (action) {
				case 'new-folder':
					if (this.filesController && this.filesController.openNewFolder) {
						this.filesController.openNewFolder()
					} else {
						this.$emit('new-folder')
					}
					break
				case 'new-file':
					if (this.filesController && this.filesController.openNewFile) {
						this.filesController.openNewFile()
					} else {
						this.$emit('new-file')
					}
					break
				case 'upload':
					if (this.filesController && this.filesController.openUpload) {
						this.filesController.openUpload()
					} else {
						this.$emit('upload')
					}
					break
				case 'paste':
					this.$emit('paste')
					break
				case 'select-all':
					this.$emit('select-all')
					break
				case 'reload':
					this.$emit('reload')
					break
				case 'open-window':
					if (this.filesController && this.filesController.openNewWindow) {
						this.filesController.openNewWindow(this.filesController.currentPath)
					}
					break
				case 'rename':
					this.$emit('rename-request', this.item)
					break
				case 'detail':
					this.$emit('detail-request', this.item)
					break
				case 'copy':
					this.operate('copy', this.item)
					break
				case 'cut':
					this.operate('move', this.item)
					break
				case 'download':
					this.downloadFile(this.item)
					break
				case 'compress':
					this.$emit('compress-request', this.item)
					break
				case 'extract':
					this.$emit('extract-request', this.item)
					break
				case 'favorite':
					if (this.isFavorite) {
						this.removeFromFavorite()
					} else {
						this.addToFavorite()
					}
					break
				case 'share':
					this.shareFolder()
					break
				case 'delete':
					this.$emit('delete-request', this.item)
					break
				case 'open':
					this.$emit('open-request', this.item)
					break
				case 'open-new-tab':
					this.$emit('open-new-tab-request', this.item)
					break
			}
			this.close()
		},

		addToFavorite() {
			let shortcut = this.$store.state.shortcutData
			if (!shortcut) shortcut = []
			shortcut.push({
				name: this.item.name,
				path: this.item.path,
			})
			this.$store.dispatch('SET_SHORTCUT_DATA', shortcut).then(() => {
				this.$EventBus.$emit(events.RELOAD_FILE_LIST)
				this.$buefy.toast.open({ message: this.$t('Added to favorites'), type: 'is-success' })
			})
		},

		removeFromFavorite() {
			const shortcut = (this.$store.state.shortcutData || []).filter((s) => s.path !== this.item.path)
			this.$store.dispatch('SET_SHORTCUT_DATA', shortcut).then(() => {
				this.$EventBus.$emit(events.RELOAD_FILE_LIST)
				this.$buefy.toast.open({ message: this.$t('Removed from favorites'), type: 'is-success' })
			})
		},

		async shareFolder() {
			try {
				await this.$api.samba.createShare([{ path: this.item.path, anonymous: true }])
				this.$buefy.toast.open({ message: this.$t('Share created successfully'), type: 'is-success' })
			} catch (error) {
				this.$buefy.toast.open({ message: error.response.data.message, type: 'is-danger' })
			}
		},
	},
}
</script>

<style lang="scss" scoped>
.files-context-menu {
	position: absolute;
	z-index: 100;
	width: 215px;
	background: rgba(255, 255, 255, 0.95);
	backdrop-filter: blur(24px) saturate(180%);
	-webkit-backdrop-filter: blur(24px) saturate(180%);
	border: 1px solid rgba(0, 0, 0, 0.09);
	border-radius: 12px;
	box-shadow: 0 16px 36px rgba(0, 0, 0, 0.16), 0 2px 8px rgba(0, 0, 0, 0.08);
	padding: 0.35rem;
	animation: filesCtxFade 0.12s cubic-bezier(0.16, 1, 0.3, 1);
	user-select: none;
}

@keyframes filesCtxFade {
	from {
		opacity: 0;
		transform: scale(0.96) translateY(-4px);
	}
	to {
		opacity: 1;
		transform: scale(1) translateY(0);
	}
}

.menu-item {
	display: flex;
	align-items: center;
	gap: 0.65rem;
	width: 100%;
	text-align: left;
	padding: 0.44rem 0.7rem;
	border: none;
	background: none;
	cursor: pointer;
	border-radius: 7px;
	font-family: inherit;
	font-size: 0.8125rem;
	font-weight: 500;
	color: #1e293b;
	transition: all 0.12s ease;

	.menu-icon {
		font-size: 1.15rem;
		width: 1.25rem;
		text-align: center;
		color: #475569;
		flex-shrink: 0;
		line-height: 1;
		transition: color 0.12s ease;
	}

	.menu-label {
		flex: 1;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	&:hover {
		background: #2563eb;
		color: #ffffff;

		.menu-icon {
			color: #ffffff;
		}
	}

	&.is-danger {
		color: #dc2626;

		.menu-icon {
			color: #dc2626;
		}

		&:hover {
			background: #dc2626;
			color: #ffffff;

			.menu-icon {
				color: #ffffff;
			}
		}
	}

	&:active {
		transform: scale(0.98);
	}
}

.menu-sep {
	height: 1px;
	margin: 0.35rem 0.4rem;
	background: rgba(0, 0, 0, 0.08);
}
</style>
