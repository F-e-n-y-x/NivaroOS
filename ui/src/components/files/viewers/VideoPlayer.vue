<!-- src/components/files/viewers/VideoPlayer.vue -->
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
	<files-viewer-chrome @download="downloadFile(item)">
		<div class="video-player-body">
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
				this.instance = new Artplayer({
					url: this.getFileUrl(this.item),
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
			}
		})
	},
	beforeDestroy() {
		if (this.instance && this.instance.destroy) {
			this.instance.destroy(false)
		}
	},
	methods: {
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
.video-player-body {
	position: relative;
	width: 100%;
	height: 100%;
	display: flex;
	align-items: center;
	justify-content: center;
}
.player {
	width: 100%;
	height: 100%;
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
