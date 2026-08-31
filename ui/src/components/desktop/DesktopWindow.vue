<template>
	<div
	:id="'window-' + win.id"
	:style="windowStyle"
	class="desktop-window"
	:class="{ 'window-dark': isDarkWindow, 'window-opaque': win.component === 'FilesApp', 'window-minimized': win.minimized }"
	@mousedown="focus"
	@drop.stop
>
		<div v-if="!ownTitlebarComponents.includes(win.component)" class="window-titlebar" @mousedown="startDrag">
			<b-icon v-if="isConsoleWindow" icon="monitor" custom-size="mdi-16px" class="window-title-icon"></b-icon>
			<span class="window-title" :class="{ 'one-line': !isConsoleWindow }">{{ win.title }}</span>
			<span v-if="isConsoleWindow && consoleStatus" class="window-title-status" :class="'is-' + consoleStatus">{{ consoleStatusText }}</span>
			<div class="window-titlebar-spacer"></div>
			<div class="window-controls">
				<button class="window-btn window-btn-minimize" :title="$t('Minimize')" @click.stop="minimize"></button>
				<button class="window-btn window-btn-close" :title="$t('Close')" @click.stop="close"></button>
			</div>
		</div>
		<div class="window-content">
			<!-- FilesApp has no window-titlebar above (see v-if) - its own tab
			     bar takes over as the draggable top bar and carries the
			     close/minimize controls itself (no maximize, by design). -->
			<component :is="resolvedComponent" ref="content" v-bind="win.props" @close="close" @minimize="minimize" @drag-start="startDrag" @status-change="onConsoleStatusChange"></component>
		</div>

		<div class="resize-handle resize-right" @mousedown.stop="startResize('right', $event)"></div>
		<div class="resize-handle resize-left" @mousedown.stop="startResize('left', $event)"></div>
		<div class="resize-handle resize-bottom" @mousedown.stop="startResize('bottom', $event)"></div>
		<div class="resize-handle resize-top" @mousedown.stop="startResize('top', $event)"></div>
		<div class="resize-handle resize-corner-br" @mousedown.stop="startResize('corner-br', $event)"></div>
		<div class="resize-handle resize-corner-tl" @mousedown.stop="startResize('corner-tl', $event)"></div>
		<div class="resize-handle resize-corner-tr" @mousedown.stop="startResize('corner-tr', $event)"></div>
		<div class="resize-handle resize-corner-bl" @mousedown.stop="startResize('corner-bl', $event)"></div>
	</div>
</template>

<script>
import FilesApp from '@/components/files/FilesApp.vue'
import TerminalPanel from '@/components/logsAndTerminal/TerminalPanel.vue'
import SettingsApp from './SettingsApp.vue'
import AppStoreApp from './AppStoreApp.vue'
import LegacyAppEditPanel from '@/components/Apps/LegacyAppEditPanel.vue'
import VmManagerApp from './VmManagerApp.vue'
import ImageViewer from '@/components/files/viewers/ImageViewer.vue'
import VideoPlayer from '@/components/files/viewers/VideoPlayer.vue'
import CodeEditor from '@/components/files/viewers/CodeEditor.vue'
import DocViewer from '@/components/files/viewers/DocViewer.vue'
import ExcelViewer from '@/components/files/viewers/ExcelViewer.vue'
import PdfViewer from '@/components/files/viewers/PdfViewer.vue'
import VmConsolePanel from './vm/VmConsolePanel.vue'
import CreateVmModal from './vm/CreateVmModal.vue'
import EditVmModal from './vm/EditVmModal.vue'
import FolderWindow from './FolderWindow.vue'

const COMPONENT_REGISTRY = {
	FilesApp,
	TerminalPanel,
	VmConsolePanel,
	CreateVmModal,
	EditVmModal,
	SettingsApp,
	AppStoreApp,
	LegacyAppEditPanel,
	VmManagerApp,
	ImageViewer,
	VideoPlayer,
	CodeEditor,
	DocViewer,
	ExcelViewer,
	PdfViewer,
	FolderWindow
}

const MIN_WIDTH = 360
const MIN_HEIGHT = 280

// These components' own top row IS the window's titlebar (draggable, with
// their own minimize/close controls, no maximize by design) - the shared
// .window-titlebar below would just be a redundant second bar on top of it.
const OWN_TITLEBAR_COMPONENTS = ['FilesApp', 'TerminalPanel']

export default {
	name: 'desktop-window',
	props: {
		win: {
			type: Object,
			required: true
		}
	},
	data() {
		return {
			// Only VmConsolePanel emits this (see its status watcher) - the
			// titlebar shows an icon + connection pill for it instead of
			// leaving that to a second, redundant identity bar inside the
			// console's own content area.
			consoleStatus: null
		}
	},
	computed: {
		resolvedComponent() {
			return COMPONENT_REGISTRY[this.win.component]
		},
		ownTitlebarComponents() {
			return OWN_TITLEBAR_COMPONENTS
		},
		// Every viewer's own ViewerChrome toolbar is dark (#262626) - a
		// white window titlebar sitting directly above that read as a
		// visibly mismatched seam, so these windows get the same dark
		// titlebar treatment TerminalPanel already uses.
		isDarkWindow() {
			return ['TerminalPanel', 'ImageViewer', 'VideoPlayer', 'CodeEditor', 'DocViewer', 'ExcelViewer', 'PdfViewer', 'VmConsolePanel'].includes(this.win.component)
		},
		isConsoleWindow() {
			return this.win.component === 'VmConsolePanel'
		},
		consoleStatusText() {
			return (
				{
					connecting: this.$t('Connecting...'),
					connected: this.$t('Connected'),
					disconnected: this.$t('Disconnected'),
				}[this.consoleStatus] || this.consoleStatus
			)
		},
		windowStyle() {
			return {
				left: this.win.x + 'px',
				top: this.win.y + 'px',
				width: this.win.width + 'px',
				height: this.win.height + 'px',
				zIndex: this.win.zIndex
			}
		}
	},
	methods: {
		focus() {
			this.$store.commit('FOCUS_WINDOW', this.win.id)
		},

		onConsoleStatusChange(status) {
			this.consoleStatus = status
		},

		close() {
			// Some window contents (CodeEditor/MarkdownEditor, with unsaved-edit
			// state) need to intervene before the window actually closes - e.g.
			// showing their own in-window "save before closing?" prompt, then
			// emitting close themselves once that's resolved (already wired
			// via @close="close" below). Everything else has no such method,
			// so it falls straight through to closing immediately, unchanged.
			const content = this.$refs.content
			if (content && typeof content.requestClose === 'function') {
				content.requestClose()
				return
			}
			this.$store.commit('CLOSE_WINDOW', this.win.id)
		},

		minimize() {
			this.$store.commit('TOGGLE_MINIMIZE_WINDOW', this.win.id)
		},

		startDrag(e) {
			this.focus()
			const startX = e.clientX
			const startY = e.clientY
			const originX = this.win.x
			const originY = this.win.y
			let pending = null
			let frame = null
			// Fast mouse movement during a drag can select nearby page text
			// (tab labels, breadcrumbs, etc.) as a native browser artifact,
			// since not every bit of window chrome sets user-select: none
			// itself - suppressing it document-wide for the drag's duration
			// is the standard, bulletproof fix window-manager UIs use.
			document.body.style.userSelect = 'none'

			const flush = () => {
				frame = null
				if (pending) this.$store.commit('UPDATE_WINDOW_RECT', pending)
			}

			const onMove = moveEvent => {
				const dx = moveEvent.clientX - startX
				const dy = moveEvent.clientY - startY
				const maxX = Math.max(0, window.innerWidth - this.win.width)
				const maxY = Math.max(0, window.innerHeight - this.win.height)
				pending = {
					id: this.win.id,
					x: Math.min(maxX, Math.max(0, originX + dx)),
					y: Math.min(maxY, Math.max(0, originY + dy))
				}
				if (!frame) frame = requestAnimationFrame(flush)
			}
			const onUp = () => {
				window.removeEventListener('mousemove', onMove)
				window.removeEventListener('mouseup', onUp)
				if (frame) cancelAnimationFrame(frame)
				if (pending) this.$store.commit('UPDATE_WINDOW_RECT', pending)
				this.$store.commit('PERSIST_WINDOWS')
				document.body.style.userSelect = ''
			}
			window.addEventListener('mousemove', onMove)
			window.addEventListener('mouseup', onUp)
		},

		// direction is one of: right, left, bottom, top, corner-br,
		// corner-tl, corner-tr, corner-bl. Resizing from the left/top
		// (or a corner touching either) has to move x/y to keep the
		// OPPOSITE edge fixed while the dragged edge follows the cursor -
		// otherwise the window would just grow from a fixed top-left
		// origin regardless of which edge you actually dragged.
		startResize(direction, e) {
			this.focus()
			const startX = e.clientX
			const startY = e.clientY
			const originWidth = this.win.width
			const originHeight = this.win.height
			const originX = this.win.x
			const originY = this.win.y

			const affectsRight = ['right', 'corner-br', 'corner-tr'].includes(direction)
			const affectsLeft = ['left', 'corner-tl', 'corner-bl'].includes(direction)
			const affectsBottom = ['bottom', 'corner-br', 'corner-bl'].includes(direction)
			const affectsTop = ['top', 'corner-tl', 'corner-tr'].includes(direction)

			let pending = null
			let frame = null
			document.body.style.userSelect = 'none'
			const flush = () => {
				frame = null
				if (pending) this.$store.commit('UPDATE_WINDOW_RECT', pending)
			}

			const onMove = moveEvent => {
				const dx = moveEvent.clientX - startX
				const dy = moveEvent.clientY - startY
				const rect = { id: this.win.id }

				if (affectsRight) {
					const maxWidth = Math.max(MIN_WIDTH, window.innerWidth - originX)
					rect.width = Math.min(maxWidth, Math.max(MIN_WIDTH, originWidth + dx))
				} else if (affectsLeft) {
					const maxWidth = Math.max(MIN_WIDTH, originX + originWidth)
					const newWidth = Math.min(maxWidth, Math.max(MIN_WIDTH, originWidth - dx))
					rect.width = newWidth
					rect.x = Math.max(0, originX + originWidth - newWidth)
				}

				if (affectsBottom) {
					const maxHeight = Math.max(MIN_HEIGHT, window.innerHeight - originY)
					rect.height = Math.min(maxHeight, Math.max(MIN_HEIGHT, originHeight + dy))
				} else if (affectsTop) {
					const maxHeight = Math.max(MIN_HEIGHT, originY + originHeight)
					const newHeight = Math.min(maxHeight, Math.max(MIN_HEIGHT, originHeight - dy))
					rect.height = newHeight
					rect.y = Math.max(0, originY + originHeight - newHeight)
				}

				pending = rect
				if (!frame) frame = requestAnimationFrame(flush)
			}
			const onUp = () => {
				window.removeEventListener('mousemove', onMove)
				window.removeEventListener('mouseup', onUp)
				if (frame) cancelAnimationFrame(frame)
				if (pending) this.$store.commit('UPDATE_WINDOW_RECT', pending)
				this.$store.commit('PERSIST_WINDOWS')
				document.body.style.userSelect = ''
			}
			window.addEventListener('mousemove', onMove)
			window.addEventListener('mouseup', onUp)
		}
	}
}
</script>

<style lang="scss" scoped>
.desktop-window {
	position: fixed;
	display: flex;
	flex-direction: column;
	background: rgba(255, 255, 255, var(--ui-backdrop-alpha, 1));
	backdrop-filter: $backDropBlur;
	border: 1px solid rgba(0, 0, 0, 0.08);
	border-radius: $backDropBorderRadius;
	box-shadow: 0 12px 40px rgba(0, 0, 0, 0.25);
	overflow: hidden;

	// Minimized windows stay mounted (see WindowManager) so their state
	// (a live terminal session, in-progress work) survives - just hide
	// them visually instead of removing them from the DOM.
	&.window-minimized {
		display: none;
	}

	// Files gets a plain opaque white background, not the shared
	// translucent/blurred glass look every other window uses - the file
	// listing (icons, thumbnails, text) reads better against a flat
	// background than through the global blur/alpha settings.
	&.window-opaque {
		background: #fff;
		backdrop-filter: none;
	}

	// Terminal's content is black by convention (real terminal apps
	// keep dark content even in an otherwise light desktop) - match the
	// window chrome around it instead of leaving a mismatched light bar
	// on top of a black body.
	&.window-dark {
		background: rgba(30, 30, 30, var(--ui-backdrop-alpha, 1));
		border-color: rgba(255, 255, 255, 0.08);

		.window-titlebar {
			background: #262626;
			border-bottom: 1px solid rgba(255, 255, 255, 0.08);
		}

		.window-title {
			color: rgba(255, 255, 255, 0.85);
		}
	}
}

.window-titlebar {
	flex-shrink: 0;
	display: flex;
	align-items: center;
	height: 2.5rem;
	padding: 0 0.75rem;
	cursor: grab;
	background: #fff;
	border-bottom: 1px solid rgb(228 233 237);
	user-select: none;
}

.window-title {
	flex: 0 1 auto;
	min-width: 0;
	color: #2c3e50;
	font-size: 0.85rem;
	font-weight: 500;
}
// Absorbs the leftover space so the title (+ icon/status pill, on a
// console window) sit snugly together on the left instead of the title
// itself stretching to push everything else all the way to the far right.
.window-titlebar-spacer {
	flex: 1 1 auto;
}

// Only ever shown on a console window (always dark - see isDarkWindow),
// so these assume a dark titlebar rather than needing a light variant too.
.window-title-icon {
	flex-shrink: 0;
	margin-right: 0.5rem;
	color: rgba(255, 255, 255, 0.6);
}
.window-title-status {
	flex-shrink: 0;
	margin-left: 0.6rem;
	font-size: 0.68rem;
	padding: 0.1rem 0.5rem;
	border-radius: 999px;
	background: rgba(255, 255, 255, 0.1);
	color: rgba(255, 255, 255, 0.7);

	&.is-connected {
		background: rgba(72, 199, 116, 0.2);
		color: #48c774;
	}
	&.is-connecting {
		background: rgba(255, 221, 87, 0.15);
		color: #ffdd57;
	}
	&.is-disconnected {
		background: rgba(255, 56, 96, 0.15);
		color: #ff3860;
	}
}

.window-controls {
	display: flex;
	gap: 0.5rem;
	flex-shrink: 0;
}

.window-btn {
	width: 0.85rem;
	height: 0.85rem;
	border-radius: 50%;
	border: none;
	cursor: pointer;
	padding: 0;
}

.window-btn-minimize {
	background: #f6bd3b;
}

.window-btn-close {
	background: #f2534a;
}

.window-content {
	flex: 1 1 auto;
	min-height: 0;
	overflow: auto;
	position: relative;
}

.resize-handle {
	position: absolute;
}

.resize-right {
	top: 0.5rem;
	right: 0;
	bottom: 0.5rem;
	width: 6px;
	cursor: ew-resize;
}

.resize-left {
	top: 0.5rem;
	left: 0;
	bottom: 0.5rem;
	width: 6px;
	cursor: ew-resize;
}

.resize-bottom {
	left: 0.5rem;
	right: 0.5rem;
	bottom: 0;
	height: 6px;
	cursor: ns-resize;
}

.resize-top {
	left: 0.5rem;
	right: 0.5rem;
	top: 0;
	height: 6px;
	cursor: ns-resize;
}

.resize-corner-br {
	right: 0;
	bottom: 0;
	width: 14px;
	height: 14px;
	cursor: nwse-resize;
}

.resize-corner-tl {
	left: 0;
	top: 0;
	width: 14px;
	height: 14px;
	cursor: nwse-resize;
}

.resize-corner-tr {
	right: 0;
	top: 0;
	width: 14px;
	height: 14px;
	cursor: nesw-resize;
}

.resize-corner-bl {
	left: 0;
	bottom: 0;
	width: 14px;
	height: 14px;
	cursor: nesw-resize;
}
</style>
