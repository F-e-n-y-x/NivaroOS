<!-- src/apps/files/ContextMenu.vue -->
<template>
	<div
		v-show="visible"
		ref="menu"
		class="files-context-menu"
		:style="{ top: y + 'px', left: x + 'px' }"
		@contextmenu.prevent.stop
	>
		<!-- 1. MULTIPLE SELECTION CONTEXT MENU -->
		<template v-if="isMultiSelect">
			<div class="ctx-header">
				<div class="ctx-badge">
					<i class="mdi mdi-checkbox-multiple-marked-outline mr-1"></i>
					<span>{{ selectedItems.length }} {{ $t('items selected') }}</span>
				</div>
			</div>
			<div class="ctx-divider"></div>
			<button class="ctx-item" @click="act('copy-selection')">
				<i class="mdi mdi-content-copy ctx-icon"></i>
				<span class="ctx-label">{{ $t('Copy') }} ({{ selectedItems.length }})</span>
			</button>
			<button class="ctx-item" @click="act('cut-selection')">
				<i class="mdi mdi-content-cut ctx-icon"></i>
				<span class="ctx-label">{{ $t('Cut') }} ({{ selectedItems.length }})</span>
			</button>
			<button class="ctx-item" @click="act('download-selection')">
				<i class="mdi mdi-download-outline ctx-icon"></i>
				<span class="ctx-label">{{ $t('Download') }}</span>
			</button>
			<button class="ctx-item" @click="act('compress-selection')">
				<i class="mdi mdi-folder-zip-outline ctx-icon"></i>
				<span class="ctx-label">{{ $t('Compress to Zip') }}</span>
			</button>
			<div class="ctx-divider"></div>
			<button class="ctx-item is-danger" @click="act('delete-selection')">
				<i class="mdi mdi-trash-can-outline ctx-icon"></i>
				<span class="ctx-label">{{ $t('Delete') }} ({{ selectedItems.length }})</span>
			</button>
		</template>

		<!-- 2. SINGLE ITEM CONTEXT MENU (when right clicking a single file or folder) -->
		<template v-else-if="item">
			<button class="ctx-item" @click="act('open')">
				<i class="mdi mdi-open-in-app ctx-icon"></i>
				<span class="ctx-label">{{ $t('Open') }}</span>
			</button>
			<button v-if="item.is_dir" class="ctx-item" @click="act('open-new-tab')">
				<i class="mdi mdi-tab-plus ctx-icon"></i>
				<span class="ctx-label">{{ $t('Open in New Tab') }}</span>
			</button>
			<div class="ctx-divider"></div>
			<button class="ctx-item" @click="act('rename')">
				<i class="mdi mdi-pencil-outline ctx-icon"></i>
				<span class="ctx-label">{{ $t('Rename') }}</span>
			</button>
			<button class="ctx-item" @click="act('copy')">
				<i class="mdi mdi-content-copy ctx-icon"></i>
				<span class="ctx-label">{{ $t('Copy') }}</span>
			</button>
			<button class="ctx-item" @click="act('cut')">
				<i class="mdi mdi-content-cut ctx-icon"></i>
				<span class="ctx-label">{{ $t('Cut') }}</span>
			</button>
			<button class="ctx-item" @click="act('download')">
				<i class="mdi mdi-download-outline ctx-icon"></i>
				<span class="ctx-label">{{ $t('Download') }}</span>
			</button>
			<button class="ctx-item" @click="act('compress')">
				<i class="mdi mdi-folder-zip-outline ctx-icon"></i>
				<span class="ctx-label">{{ $t('Compress to Zip') }}</span>
			</button>
			<button v-if="isArchive" class="ctx-item" @click="act('extract')">
				<i class="mdi mdi-archive-arrow-down-outline ctx-icon"></i>
				<span class="ctx-label">{{ $t('Extract') }}</span>
			</button>
			<template v-if="item.is_dir">
				<button v-if="hasClipboard" class="ctx-item" @click="act('paste-into')">
					<i class="mdi mdi-content-paste ctx-icon"></i>
					<span class="ctx-label">{{ $t('Paste into folder') }}</span>
				</button>
				<div class="ctx-divider"></div>
				<button class="ctx-item" @click="act('favorite')">
					<i :class="isFavorite ? 'mdi mdi-star text-amber-500' : 'mdi mdi-star-outline'" class="ctx-icon"></i>
					<span class="ctx-label">{{ isFavorite ? $t('Remove from Favorite') : $t('Add to Favorite') }}</span>
				</button>
				<!-- Only with the Backup & Sync module installed (spec §0). -->
				<button v-if="backupInstalled" class="ctx-item" @click="act('backup-folder')">
					<i class="mdi mdi-backup-restore ctx-icon"></i>
					<span class="ctx-label">{{ $t('backup.entry.back_up_folder') }}</span>
				</button>
			</template>
			<div class="ctx-divider"></div>
			<button class="ctx-item" @click="act('share')">
				<i class="mdi mdi-share-variant-outline ctx-icon"></i>
				<span class="ctx-label">{{ $t('Share') }}</span>
			</button>
			<div class="ctx-divider"></div>
			<button class="ctx-item is-danger" @click="act('delete')">
				<i class="mdi mdi-trash-can-outline ctx-icon"></i>
				<span class="ctx-label">{{ $t('Delete') }}</span>
			</button>
			<button class="ctx-item" @click="act('detail')">
				<i class="mdi mdi-information-outline ctx-icon"></i>
				<span class="ctx-label">{{ $t('Detail') }}</span>
			</button>
		</template>

		<!-- 3. BLANK SPACE CONTEXT MENU (when right clicking empty area) -->
		<template v-else>
			<button class="ctx-item" @click="act('new-folder')">
				<i class="mdi mdi-folder-plus-outline ctx-icon"></i>
				<span class="ctx-label">{{ $t('New Folder') }}</span>
			</button>
			<button class="ctx-item" @click="act('new-file')">
				<i class="mdi mdi-file-plus-outline ctx-icon"></i>
				<span class="ctx-label">{{ $t('New File') }}</span>
			</button>
			<button class="ctx-item" @click="act('upload')">
				<i class="mdi mdi-upload-outline ctx-icon"></i>
				<span class="ctx-label">{{ $t('Upload') }}</span>
			</button>
			<div class="ctx-divider"></div>
			<button v-if="hasClipboard" class="ctx-item" @click="act('paste')">
				<i class="mdi mdi-content-paste ctx-icon"></i>
				<span class="ctx-label">{{ $t('Paste') }}</span>
			</button>
			<button class="ctx-item" @click="act('select-all')">
				<i class="mdi mdi-select-all ctx-icon"></i>
				<span class="ctx-label">{{ $t('Select All') }}</span>
			</button>
			<button class="ctx-item" @click="act('reload')">
				<i class="mdi mdi-refresh ctx-icon"></i>
				<span class="ctx-label">{{ $t('Refresh') }}</span>
			</button>
			<div class="ctx-divider"></div>
			<button class="ctx-item" @click="act('toggle-hidden')">
				<i :class="showHidden ? 'mdi mdi-eye-off-outline ctx-icon' : 'mdi mdi-eye-outline ctx-icon'"></i>
				<span class="ctx-label">{{ showHidden ? $t('Hide Hidden Files') : $t('Show Hidden Files') }}</span>
				<i v-if="showHidden" class="mdi mdi-check ctx-check"></i>
			</button>
			<div class="ctx-divider"></div>
			<button class="ctx-item" @click="act('open-window')">
				<i class="mdi mdi-open-in-new ctx-icon"></i>
				<span class="ctx-label">{{ $t('Open in New Window') }}</span>
			</button>
			<button class="ctx-item" @click="act('open-terminal')">
				<i class="mdi mdi-console ctx-icon"></i>
				<span class="ctx-label">{{ $t('Open in Terminal') }}</span>
			</button>
		</template>
	</div>
</template>

<script>
import { mixin } from '@/mixins/mixin'
import events from '@/events/events'
import { isArchive as isArchiveFile } from '@/utils/files/archive'
import { checkBackupInstalled } from '@/utils/backupInstalled'
import { backupWindow } from '@/apps/backup/windows'

const MENU_WIDTH = 224
const MENU_HEIGHT = 380

export default {
	name: 'files-context-menu',
	mixins: [mixin],
	inject: ['filesController'],
	data() {
		return { visible: false, x: 0, y: 0, item: null, selectedItems: [], backupInstalled: false }
	},
	computed: {
		isMultiSelect() {
			return this.selectedItems && this.selectedItems.length > 1
		},
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
		},
		showHidden() {
			return !!this.$store.state.showHidden
		}
	},
	mounted() {
		checkBackupInstalled().then(ok => { this.backupInstalled = ok })
		document.addEventListener('mousedown', this.onOutsideClick)
		window.addEventListener('resize', this.close)
	},
	beforeDestroy() {
		document.removeEventListener('mousedown', this.onOutsideClick)
		window.removeEventListener('resize', this.close)
	},
	methods: {
		open(event, item, boundsEl, selectedItems = []) {
			if (event && event.stopPropagation) {
				event.stopPropagation()
			}
			this.item = item
			this.selectedItems = Array.isArray(selectedItems) ? selectedItems : []
			const menuHeight = this.isMultiSelect ? 240 : (item ? MENU_HEIGHT : 260)
			const clientX = event && typeof event.clientX === 'number' ? event.clientX : 100
			const clientY = event && typeof event.clientY === 'number' ? event.clientY : 100

			const maxLeft = Math.max(12, window.innerWidth - MENU_WIDTH - 16)
			const maxTop = Math.max(12, window.innerHeight - menuHeight - 80) // Stay above taskbar

			this.x = Math.max(12, Math.min(maxLeft, clientX))
			this.y = Math.max(12, Math.min(maxTop, clientY))
			this.visible = true
		},
		close() {
			this.visible = false
		},
		onOutsideClick(event) {
			if (event && event.button === 2) return // Don't close on right-click mousedown
			if (this.visible && this.$refs.menu && !this.$refs.menu.contains(event.target)) {
				this.close()
			}
		},
		act(action) {
			switch (action) {
				case 'toggle-hidden':
					this.$store.commit('SET_SHOW_HIDDEN', !this.showHidden)
					this.$EventBus.$emit(events.RELOAD_FILE_LIST)
					break
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
				case 'paste-into':
					this.$emit('paste-into', this.item && this.item.path)
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
				case 'open-terminal':
					if (this.filesController && this.filesController.openTerminal) {
						this.filesController.openTerminal()
					}
					break
				case 'copy-selection':
					this.$emit('copy-selection')
					break
				case 'cut-selection':
					this.$emit('move-selection')
					break
				case 'download-selection':
					this.$emit('download-selection')
					break
				case 'compress-selection':
					this.$emit('compress-selection')
					break
				case 'delete-selection':
					this.$emit('delete-selection')
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
					this.$emit('share-request', this.item)
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
				case 'backup-folder':
					// Backup & Sync resolves the path to a drive and folder
					// and opens its new-job wizard with it as the source.
					if (this.item && this.item.path) {
						this.$store.commit('OPEN_WINDOW', backupWindow(this.$t.bind(this), 'app', { wizard: true, sourcePath: this.item.path }))
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

	},
}
</script>

<style lang="scss" scoped>
.files-context-menu {
	position: fixed;
	z-index: 999999;
	width: 224px;
	background: var(--theme-menu-bg, rgba(255, 255, 255, 0.88));
	backdrop-filter: blur(24px) saturate(180%);
	-webkit-backdrop-filter: blur(24px) saturate(180%);
	border: 1px solid var(--theme-card-border, rgba(255, 255, 255, 0.65));
	border-radius: var(--radius-card);
	box-shadow: var(--shadow-lg);
	padding: var(--space-1);
	user-select: none;
	animation: ctxFadeIn 0.12s cubic-bezier(0.16, 1, 0.3, 1);
}

// @keyframes ctxFadeIn is defined once, globally, in
// assets/scss/common/_dropdown.scss - see that file for why.

.ctx-header {
	padding: var(--space-1) var(--space-2) var(--space-1);
}

.ctx-badge {
	display: inline-flex;
	align-items: center;
	padding: var(--space-1) var(--space-2);
	background: var(--color-primary-soft, rgba(37, 99, 235, 0.1));
	color: var(--color-primary, #2563eb);
	border-radius: var(--radius-sm);
	font-size: var(--font-xs);
	font-weight: 600;
	letter-spacing: 0.01em;
	width: 100%;
}

.ctx-item {
	display: flex;
	align-items: center;
	gap: var(--space-3);
	width: 100%;
	padding: var(--space-2) var(--space-3);
	border-radius: var(--radius-control);
	font-family: inherit;
	font-size: var(--font-sm);
	font-weight: 500;
	color: var(--theme-text-primary, #1e293b);
	transition: all 0.12s ease;
	cursor: pointer;
	border: none;
	background: transparent;
	text-align: left;

	.ctx-icon {
		font-size: var(--font-lg);
		color: var(--theme-text-secondary, #475569);
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

	.ctx-check {
		font-size: var(--font-md);
		color: var(--color-primary, #2563eb);
		margin-left: auto;
		flex-shrink: 0;
	}

	&:hover {
		background: var(--theme-menu-item-hover-bg, rgba(0, 0, 0, 0.9));
		color: var(--theme-menu-item-hover-text, #ffffff);

		.ctx-icon {
			color: var(--theme-menu-item-hover-text, #ffffff);
		}

		.ctx-check {
			color: var(--theme-menu-item-hover-text, #ffffff);
		}
	}

	&.is-danger {
		color: var(--color-danger, #ef4444);

		.ctx-icon {
			color: var(--color-danger, #ef4444);
		}

		&:hover {
			background: var(--color-danger-hover, #dc2626);
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
	margin: var(--space-1) var(--space-2);
	background: var(--theme-card-border, rgba(0, 0, 0, 0.08));
}
</style>
