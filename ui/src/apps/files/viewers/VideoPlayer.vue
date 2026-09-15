<!-- src/apps/files/viewers/VideoPlayer.vue -->
<!--
	Ported from src/components/filebrowser/viewers/VideoPlayer.vue. The
	plan's own Reference note called this a "near-trivial <video>
	wrapper" - it isn't: it's an Artplayer instance for video and a
	separate vue-aplayer instance (with music-metadata-browser-derived
	cover art/title/artist) for audio, since 'video-player' in
	filePanelMap covers both video AND audio extensions. Ported in full,
	same libraries, new chrome only.
-->
<template>
	<files-viewer-chrome :no-overflow="true" @download="downloadFile(item)" @viewer-resize="onViewerResize">
		<div v-if="hasError" class="viewer-error-state">
			<b-icon icon="file-alert-outline" custom-size="mdi-48px"></b-icon>
			<p>{{ errorMessage }}</p>
			<b-button type="is-primary" icon-left="download-outline" @click="downloadFile(item)">{{ $t('Download') }}</b-button>
		</div>
		<div v-else class="video-player-body">
			<div v-if="poster" class="audio-blur-background" :style="{ backgroundImage: `url(${poster})` }"></div>
			<div v-if="isVideo" ref="artRef" class="player"></div>
			<aplayer
				v-if="isAudio"
				:key="item.path"
				:autoplay="true"
				preload="auto"
				class="player-audio"
				theme="#41b883"
				:music="{ title: audioTitle, artist: audioArtist, src: getFileUrl(item), pic: poster }"
			></aplayer>
		</div>
	</files-viewer-chrome>
</template>

<script>
import { mixin } from '@/mixins/mixin'
import ViewerChrome from './ViewerChrome.vue'
import Aplayer from 'vue-aplayer'
import Artplayer from 'artplayer'
import * as mm from 'music-metadata-browser'

Aplayer.disableVersionBadge = true

// Extensions with no native browser support at all, regardless of the
// codec inside them - Chrome/Firefox only reliably play mp4/m4v/webm
// natively. Every one of these needs the backend's on-the-fly remux
// endpoint (GetStreamRemuxVideo) instead of raw file-serving. Sourced from
// mixins/mixin.js's own typeMap['video-x-generic'] list, minus the three
// natively-fine ones - .mov specifically was the other format reported
// broken alongside .mkv, and QuickTime's container isn't natively
// supported in Chrome/Firefox any more than Matroska is.
const NEEDS_REMUX_EXTENSIONS = ['mkv', '3gp', 'avi', 'm2ts', 'flv', 'vob', 'ts', 'mts', 'mov', 'wmv', 'rm', 'rmvb', 'asf', 'mpg', 'mpeg', 'f4v']

export default {
	name: 'files-video-player',
	mixins: [mixin],
	components: { FilesViewerChrome: ViewerChrome, Aplayer },
	props: {
		item: { type: Object, required: true },
	},
	data() {
		return {
			type: '',
			instance: null,
			poster: '',
			audioTitle: this.item.name,
			audioArtist: '...',
			hasError: false,
			errorMessage: '',
		}
	},
	computed: {
		isVideo() {
			return this.type === 'video-x-generic'
		},
		isAudio() {
			return this.type === 'audio-x-generic'
		},
	},
	mounted() {
		const ext = this.getFileExt(this.item)
		Object.keys(this.typeMap).forEach((_type) => {
			if (this.typeMap[_type].indexOf(ext.toLowerCase()) > -1) this.type = _type
		})
		this.$nextTick(() => {
			if (this.isAudio) {
				this.loadAudioMetadata()
			} else {
				const needsRemux = NEEDS_REMUX_EXTENSIONS.includes(this.getFileExt(this.item).toLowerCase())
				this.instance = new Artplayer({
					url: needsRemux ? this.getStreamUrl(this.item) : this.getFileUrl(this.item),
					// Explicit, rather than left to Artplayer's own
					// type-sniffing (which falls back to reading a
					// "file extension" off the URL - both getFileUrl() and
					// getStreamUrl() end in `&token=<JWT>`, and a JWT's
					// header.payload.signature shape means the URL's last
					// "." isn't a file extension at all here, it's an
					// arbitrary fragment of the signature). Harmless for the
					// native case (nothing in this app registers a
					// customType handler Artplayer would need `type` to
					// select), but removes any ambiguity for the remux case,
					// where this must read as mp4 - that's the one thing the
					// backend endpoint always actually returns.
					type: needsRemux ? 'mp4' : this.getFileExt(this.item).toLowerCase(),
					container: this.$refs.artRef,
					setting: true,
					flip: true,
					playbackRate: true,
					aspectRatio: true,
					subtitleOffset: true,
					fullscreenWeb: true,
					fullscreen: true,
					autoplay: true,
					pip: true,
					theme: '#007AE5',
					playsInline: true,
					screenshot: true,
					airplay: true,
					lang: this.$i18n.locale.replace('_', '-'),
				})
				// Artplayer's own docs list these as the two error events -
				// 'video:error' is the native <video> element's own decode/
				// network failure (e.g. exactly the "container/codec this
				// browser can't play" case), 'error' is Artplayer's own
				// higher-level failure (e.g. exhausted its reconnect
				// attempts). Previously unhandled: a failure here left the
				// player either stuck on its loading spinner forever, or
				// silently blank, with nothing on screen explaining why and
				// no way to tell without opening devtools.
				this.instance.on('video:error', () => this.onPlaybackError())
				this.instance.on('error', () => this.onPlaybackError())
			}
		})
	},
	beforeDestroy() {
		if (this.instance && this.instance.destroy) {
			this.instance.destroy(false)
		}
	},
	methods: {
		onPlaybackError() {
			this.hasError = true
			this.errorMessage = this.$t(
				'This video couldn’t be played - the file may be corrupted, or use a video/audio codec this browser can’t decode.'
			)
		},
		// Artplayer only re-runs its own internal layout math on the
		// browser's native `window` resize event (or a fullscreen/orientation
		// change) - it has no ResizeObserver of its own, so resizing just
		// this window (a CSS-only change, `window` itself never fires
		// resize) left the video at its original size/position. 'resize' is
		// a documented public event (types/events.d.ts) instance.emit() can
		// trigger the same internal re-layout externally.
		onViewerResize() {
			this.instance && this.instance.emit && this.instance.emit('resize')
		},
		async loadAudioMetadata() {
			const fileUrl = this.getFileUrl(this.item)
			const metadata = await mm.fetchFromUrl(fileUrl)
			if (metadata.common.picture && metadata.common.picture.length) {
				const blob = new Blob([metadata.common.picture[0].data], { type: metadata.common.picture[0].format })
				this.poster = URL.createObjectURL(blob)
			}
			this.audioTitle = metadata.common.title || this.item.name
			this.audioArtist = metadata.common.artist || '...'
		},
	},
}
</script>

<style lang="scss" scoped>
.viewer-error-state {
	display: flex;
	flex-direction: column;
	align-items: center;
	gap: var(--space-3);
	padding: var(--space-6);
	max-width: 24rem;
	text-align: center;
	color: #fff;
}
.video-player-body {
	position: relative;
	width: 100%;
	height: 100%;
	display: flex;
	align-items: center;
	justify-content: center;
	overflow: hidden;
}
.player {
	width: 100%;
	height: 100%;
	overflow: hidden;

	::v-deep .art-video-player {
		width: 100% !important;
		height: 100% !important;
		max-width: 100% !important;
		max-height: 100% !important;
		overflow: hidden !important;
	}

	// Artplayer's own built-in stylesheet hardcodes this (the big
	// play/pause/loading icon shown over the video) to
	// `position:absolute;bottom:65px;right:30px` - genuinely by design, not
	// a conflict with anything here, it's just not centered by default.
	// Re-centering it properly (not just nudging the bottom/right offsets,
	// which would still be off-center for any player size other than
	// whatever Artplayer tuned those two numbers for).
	::v-deep .art-state {
		top: 50% !important;
		left: 50% !important;
		bottom: auto !important;
		right: auto !important;
		transform: translate(-50%, -50%) !important;
	}

	::v-deep .art-video-player video {
		width: 100% !important;
		height: 100% !important;
		object-fit: contain;
	}
}
.player-audio {
	position: relative;
	z-index: 1;
	width: 100%;
	max-width: 80rem;
	max-height: 4.125rem;
}
.audio-blur-background {
	position: absolute;
	inset: 0;
	z-index: 0;
	background-size: cover;
	background-position: center;
	background-color: rgba(53, 54, 58, 0.4);
	backdrop-filter: blur(10px) saturate(180%);
}
</style>
