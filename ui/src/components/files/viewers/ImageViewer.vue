<!-- src/components/files/viewers/ImageViewer.vue -->
<!--
	Ported from src/components/filebrowser/viewers/ImageViewer.vue - same
	v-viewer library and zoom/rotate/prev-next behavior. The legacy
	toolbar auto-hid after 5s of no mouse movement (it floated over the
	image); that's dropped here since ViewerChrome's header is a
	persistent top bar, not a floating overlay, so there's nothing for it
	to get in the way of. Also fixes two real bugs found while porting:
	(1) legacy bound `window.onkeyup` directly (last-writer-wins, no
	cleanup) rather than addEventListener/removeEventListener - harmless
	as a one-off full-page modal, but two Image Viewers open in two
	windows at once (this app now supports multiple windows) would fight
	over arrow-key navigation. (2) SVGs rendered broken/blank: viewerjs
	computes its zoom/pan canvas from the image's natural width/height,
	and an SVG with no explicit width/height attribute (the common case
	for hand-authored icons using only viewBox) reports 0x0 - a known
	viewerjs limitation, not fixable from this side. SVGs are vector, so
	there's nothing to lose by not zooming/panning them anyway: they now
	render as a plain, correctly-sized <img> instead of going through
	viewerjs at all.
-->
<template>
	<files-viewer-chrome :no-overflow="true" @download="downloadFile(currentItem)">
		<template #actions>
			<b-icon
				v-if="itemList.length > 1"
				:class="{ disabled: disablePrev }"
				icon="arrow-left-thin"
				custom-size="mdi-18px"
				class="is-clickable"
				@click.native="prev"
			></b-icon>
			<template v-if="!isSvg">
				<b-icon icon="magnify-plus-outline" custom-size="mdi-18px" class="is-clickable" @click.native="viewer && viewer.zoom(0.1)"></b-icon>
				<b-icon icon="format-rotate-90" custom-size="mdi-18px" class="is-clickable" @click.native="viewer && viewer.rotate(90)"></b-icon>
				<b-icon icon="restore" custom-size="mdi-18px" class="is-clickable" @click.native="viewer && viewer.reset()"></b-icon>
				<b-icon icon="magnify-minus-outline" custom-size="mdi-18px" class="is-clickable" @click.native="viewer && viewer.zoom(-0.1)"></b-icon>
			</template>
			<b-icon
				v-if="itemList.length > 1"
				:class="{ disabled: disableNext }"
				icon="arrow-right-thin"
				custom-size="mdi-18px"
				class="is-clickable"
				@click.native="next"
			></b-icon>
		</template>
		<div class="image-viewer-body" :class="{ 'svg-backdrop': isSvg }">
			<img v-if="isSvg" :src="currentItemArray[0]" class="svg-image" alt="image" />
			<viewer v-else ref="viewerRoot" :images="currentItemArray" :options="viewerOptions" class="viewer" @inited="inited">
				<template #default="scope">
					<!-- v-viewer's inline mode builds its own separate canvas/list
					     DOM nodes from these <img> elements - it never hides the
					     source elements themselves (that's the modal/backdrop
					     mode's job, drawing over them); in inline mode without a
					     backdrop, the source stays visible underneath unless the
					     embedding page hides it, which is exactly what this class
					     does. -->
					<img v-for="src in scope.images" :key="src" :src="src" alt="image" class="viewer-source" />
				</template>
			</viewer>
		</div>
	</files-viewer-chrome>
</template>

<script>
import { mixin } from '@/mixins/mixin'
import ViewerChrome from './ViewerChrome.vue'
import 'viewerjs/dist/viewer.css'
import { component as Viewer } from 'v-viewer'

const IMAGE_EXTENSIONS = ['png', 'jpg', 'jpeg', 'bmp', 'gif', 'webp', 'svg', 'tiff']

export default {
	name: 'files-image-viewer',
	mixins: [mixin],
	components: { FilesViewerChrome: ViewerChrome, Viewer },
	props: {
		item: { type: Object, required: true },
		// Sibling items from the same folder listing, for prev/next -
		// FilesApp passes the active tab's current ContentView.listing.
		list: { type: Array, default: () => [] },
	},
	data() {
		return {
			itemList: [],
			currentItem: this.item,
			currentItemIndex: 0,
			currentItemArray: [],
			viewer: null,
			viewerOptions: {
				button: false,
				toolbar: false,
				title: false,
				navbar: false,
				backdrop: false,
				transition: false,
				inline: true,
				initialViewIndex: 0,
			},
		}
	},
	computed: {
		disableNext() {
			return this.currentItemIndex >= this.itemList.length - 1
		},
		disablePrev() {
			return this.currentItemIndex <= 0
		},
		isSvg() {
			return this.getFileExt(this.currentItem).toLowerCase() === 'svg'
		},
	},
	created() {
		this.filterImages()
		this.getCurrentImageIndex()
		this.setSourceImageURLs()
	},
	mounted() {
		window.addEventListener('keyup', this.onKeyUp)
	},
	beforeDestroy() {
		window.removeEventListener('keyup', this.onKeyUp)
	},
	methods: {
		onKeyUp(e) {
			if (e.code === 'ArrowRight') this.next()
			else if (e.code === 'ArrowLeft') this.prev()
		},
		inited(viewer) {
			this.viewer = viewer
			this.viewer.show()
		},
		next() {
			if (this.currentItemIndex < this.itemList.length - 1) {
				this.currentItemIndex++
				this.setSourceImageURLs()
			}
		},
		prev() {
			if (this.currentItemIndex > 0) {
				this.currentItemIndex--
				this.setSourceImageURLs()
			}
		},
		filterImages() {
			this.itemList = this.list.filter((item) => !item.is_dir && IMAGE_EXTENSIONS.indexOf(this.getFileExt(item).toLowerCase()) > -1)
			if (!this.itemList.length) this.itemList = [this.item]
		},
		getCurrentImageIndex() {
			const index = this.itemList.findIndex((item) => item.path === this.currentItem.path)
			this.currentItemIndex = index > -1 ? index : 0
		},
		setSourceImageURLs() {
			this.currentItem = this.itemList[this.currentItemIndex]
			this.currentItemArray = [this.getFileUrl(this.currentItem)]
		},
	},
}
</script>

<style lang="scss" scoped>
.image-viewer-body {
	width: 100%;
	height: 100%;
	display: flex;
	align-items: center;
	justify-content: center;
	// Same checkered pattern viewerjs's own canvas uses (see the raster
	// path below) - gives transparent SVGs visible contrast against the
	// dark viewer background, instead of just disappearing into it.
	&.svg-backdrop {
		background-image: url("data:image/svg+xml;utf8,%3C?xml version='1.0' encoding='UTF-8'?%3E%3Csvg xmlns='http://www.w3.org/2000/svg' width='50' height='50' viewBox='0 0 16 16'%3E%3Cpath fill='%23ccc' d='M8 6.5A1.5 1.5 0 1 0 8 9.5A1.5 1.5 0 1 0 8 6.5z' fill-opacity='0.1' /%3E%3C/svg%3E");
		background-color: #fff;
	}
}
.svg-image {
	max-width: 90%;
	max-height: 90%;
	object-fit: contain;
}
.viewer-source {
	display: none;
}
.viewer {
	width: 100%;
	height: 100%;
}
.disabled {
	opacity: 0.35;
	pointer-events: none;
}
</style>
