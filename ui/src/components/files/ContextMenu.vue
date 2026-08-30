<!-- src/components/files/ContextMenu.vue -->
<template>
	<div v-if="visible" ref="menu" class="files-context-menu" :style="{ top: y + 'px', left: x + 'px' }">
		<button class="menu-item" @click="act('open')">
			<b-icon icon="open-in-app" custom-size="mdi-16px"></b-icon>
			<span>{{ $t('Open') }}</span>
		</button>
		<button v-if="item && item.is_dir" class="menu-item" @click="act('open-new-tab')">
			<b-icon icon="tab-plus" custom-size="mdi-16px"></b-icon>
			<span>{{ $t('Open in New Tab') }}</span>
		</button>
		<div class="menu-sep"></div>
		<button class="menu-item" @click="act('rename')">
			<b-icon icon="pencil-outline" custom-size="mdi-16px"></b-icon>
			<span>{{ $t('Rename') }}</span>
		</button>
		<button class="menu-item" @click="act('copy')">
			<b-icon icon="content-copy" custom-size="mdi-16px"></b-icon>
			<span>{{ $t('Copy') }}</span>
		</button>
		<button class="menu-item" @click="act('cut')">
			<b-icon icon="content-cut" custom-size="mdi-16px"></b-icon>
			<span>{{ $t('Cut') }}</span>
		</button>
		<button class="menu-item" @click="act('download')">
			<b-icon icon="download-outline" custom-size="mdi-16px"></b-icon>
			<span>{{ $t('Download') }}</span>
		</button>
		<button class="menu-item" @click="act('compress')">
			<b-icon icon="folder-zip-outline" custom-size="mdi-16px"></b-icon>
			<span>{{ $t('Compress to Zip') }}</span>
		</button>
		<button v-if="isArchive" class="menu-item" @click="act('extract')">
			<b-icon icon="archive-arrow-down-outline" custom-size="mdi-16px"></b-icon>
			<span>{{ $t('Extract') }}</span>
		</button>
		<template v-if="item && item.is_dir">
			<div class="menu-sep"></div>
			<button class="menu-item" @click="act('favorite')">
				<b-icon :icon="isFavorite ? 'star' : 'star-outline'" custom-size="mdi-16px"></b-icon>
				<span>{{ isFavorite ? $t('Remove from Favorite') : $t('Add to Favorite') }}</span>
			</button>
			<button class="menu-item" @click="act('share')">
				<b-icon icon="share-variant-outline" custom-size="mdi-16px"></b-icon>
				<span>{{ $t('Share') }}</span>
			</button>
		</template>
		<div class="menu-sep"></div>
		<button class="menu-item is-danger" @click="act('delete')">
			<b-icon icon="trash-can-outline" custom-size="mdi-16px"></b-icon>
			<span>{{ $t('Delete') }}</span>
		</button>
		<button class="menu-item" @click="act('detail')">
			<b-icon icon="information-outline" custom-size="mdi-16px"></b-icon>
			<span>{{ $t('Detail') }}</span>
		</button>
	</div>
</template>

<script>
import { mixin } from '@/mixins/mixin'
import events from '@/events/events'
import { isArchive as isArchiveFile } from '@/utils/files/archive'

const MENU_WIDTH = 190
// Three extra folder-only items ("Open in New Tab"/"Add a shortcut"/"Share"), plus 3
// section dividers, can appear above Detail/Delete for a directory's full menu.
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
	},
	mounted() {
		// Buefy's b-dropdown (used by the legacy filebrowser context menu) closes
		// itself on an outside click by default. This custom menu is a plain
		// positioned <div>, not a b-dropdown, so that behavior has to be
		// reimplemented here explicitly - without it the menu would stay open
		// indefinitely (floating over the UI) until a menu item is clicked.
		document.addEventListener('mousedown', this.onOutsideClick)
	},
	beforeDestroy() {
		document.removeEventListener('mousedown', this.onOutsideClick)
	},
	methods: {
		open(event, item, boundsEl) {
			this.item = item
			const bounds = boundsEl.getBoundingClientRect()
			// `boundsEl` (.content-view) is both the scroll container and the
			// position:relative anchor for this menu, mounted as its direct
			// child (unlike Task 11's .drag-select-box, which is positioned
			// inside .items - the same scrolling frame its coordinates are
			// measured against). `getBoundingClientRect()` gives a
			// visible-viewport-relative rect, but the menu's CSS top/left are
			// interpreted against the scrolled CONTENT's origin - so both the
			// raw click position and the clamp bounds need boundsEl's own
			// scroll offset folded in, or the menu renders shifted up/left by
			// scrollTop/scrollLeft (or entirely off-screen) whenever the
			// listing is scrolled at the time of the right-click.
			const scrollLeft = boundsEl.scrollLeft
			const scrollTop = boundsEl.scrollTop
			const rawX = event.clientX - bounds.left + scrollLeft
			const rawY = event.clientY - bounds.top + scrollTop
			this.x = Math.max(scrollLeft, Math.min(rawX, scrollLeft + bounds.width - MENU_WIDTH))
			this.y = Math.max(scrollTop, Math.min(rawY, scrollTop + bounds.height - MENU_HEIGHT))
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
					// 'move' (not 'cut') is the literal type string the backend paste
					// flow ($api.batch.task) expects in operateObject.type - matches
					// legacy ContextMenu.vue:68's `operate('move', items)` for its
					// "Cut" menu item.
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
					// Delegated up to FilesApp, which shows this.item in an
					// in-window ConfirmDialog (position:absolute; inset:0 within
					// FilesApp, same as every other dialog) rather than this
					// component calling Buefy's global $buefy.dialog.confirm()
					// directly - that API renders a viewport-wide overlay over the
					// whole desktop, not confined to the Files window.
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

		// Ported from the legacy shortcut mechanism (src/components/filebrowser/modals/
		// NewFolderModal.vue, "Add a shortcut" checkbox), which only ever ran at
		// folder-creation time. This makes the same action available for any existing
		// folder via the right-click menu. FolderTree.vue's shortcutList is a one-time
		// copy of $store.state.shortcutData (refreshed only in its own mounted()/
		// getNewList(), not a reactive computed), so a plain store dispatch alone won't
		// update the sidebar - RELOAD_FILE_LIST is the same event FolderTree already
		// listens for to refresh itself.
		addToFavorite() {
			let shortcut = this.$store.state.shortcutData
			if (!shortcut) shortcut = []
			shortcut.push({ name: this.item.name, path: this.item.path, type: 'folder' })
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

		// Ported from the real legacy right-click menu (src/components/filebrowser/
		// components/ContextMenu.vue:229-243's shareFoler()) - same createShare payload
		// shape already verified correct in the Shared-section work (array of
		// {path, anonymous}). Skips the legacy getShareLink() follow-up modal (out of
		// scope here) in favor of a plain success/error toast.
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
	z-index: 50;
	background: #fff;
	border-radius: 8px;
	box-shadow: 0 8px 24px rgba(0, 0, 0, 0.18);
	padding: 0.3rem;
	min-width: 190px;
}
.menu-item {
	display: flex;
	align-items: center;
	gap: 0.6rem;
	width: 100%;
	text-align: left;
	padding: 0.4rem 0.6rem;
	border: none;
	background: none;
	cursor: pointer;
	border-radius: 5px;
	font-family: inherit;
	font-size: 0.85rem;
	color: #2c3e50;
	.icon { color: rgba(0, 0, 0, 0.5); flex-shrink: 0; }
	&:hover { background: rgba(0, 0, 0, 0.06); }
	&.is-danger {
		color: #f2534a;
		.icon { color: #f2534a; }
	}
}
.menu-sep {
	height: 1px;
	margin: 0.3rem 0.4rem;
	background: rgb(228 233 237);
}
</style>
