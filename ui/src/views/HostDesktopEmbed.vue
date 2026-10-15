<!-- src/views/HostDesktopEmbed.vue
	#/host-desktop/embed?mode=smooth - the Android app's WebView: the WebRTC
	viewer full-bleed, nothing else. The app injects its JWT as
	window.__nivaroosToken before load (never in the URL) and listens on
	window.NivaroosBridge for {"state":"connecting|live|fallback|error","reason"}.
	On "fallback" (no WebRTC here, or no first frame within 4 s) the app
	switches to its own VNC client.
	The app draws its own console over the page and drives everything
	through window.nivaroosEmbed (key, pointer, clip, setStream, view); the
	page renders the video and posts {t:"size"}, {t:"stats"}, {t:"adapt"}
	and {t:"clip"} back. Contract: /root/nivaroos-wip/webrtc-contract.md (Embedding).
-->
<script setup>
import { onBeforeUnmount, onMounted, ref } from 'vue'
import HostDesktopWebRTC from '@/shell/desktop/HostDesktopWebRTC.vue'
import { PRESET_CHOICES, STREAM_FPS, STREAM_KBPS, loadPrefs, normalizePrefs } from '@/shell/desktop/streamMode'

// The app sets its own preset / fps / bitrate before load
// (window.__nivaroosStream: {preset, adapt, fps, maxKbps}; an older app
// sends only fps / maxKbps, which normalizePrefs turns into Custom).
const prefs = window.__nivaroosStream ? normalizePrefs(window.__nivaroosStream) : loadPrefs()
const preset = ref(prefs.preset)
const adapt = ref(prefs.adapt)
const fps = ref(prefs.fps)
const maxKbps = ref(prefs.maxKbps)
const state = ref('connecting')
const reason = ref('')
let posted = ''
let postedStats = ''

function post(obj) {
	try {
		if (!window.NivaroosBridge) return false
		window.NivaroosBridge.postMessage(JSON.stringify(obj))
		return true
	} catch {
		return false
	}
}

function onState(e) {
	// The bridge only knows connecting|live|fallback|error.
	const s = e.state === 'reconnecting' || e.state === 'paused' ? 'connecting' : e.state
	state.value = s
	reason.value = e.reason || ''
	const msg = JSON.stringify({ state: s, reason: reason.value })
	if (msg === posted) return
	posted = msg
	post({ state: s, reason: reason.value })
}

const onSize = (s) => post({ t: 'size', w: s.w, h: s.h })

function onStats(s) {
	const msg = JSON.stringify(s)
	if (msg === postedStats) return
	postedStats = msg
	post({ t: 'stats', ...s })
}

// The host copied text. Android's WebView has no navigator.clipboard on
// plain http, so the app copies it to the phone.
function onClipboard(text) {
	if (post({ t: 'clip', text })) return
	if (navigator.clipboard && navigator.clipboard.writeText) navigator.clipboard.writeText(text).catch(() => {})
}

// The app's input and settings. key: X keysym, down; pointer: remote
// pixels and an RFB button mask (8/16/32/64 = wheel); clip: text to the
// host's clipboard; setStream({preset, adapt, fps, maxKbps}): offers again
// with them (adapt alone doesn't);
// view({x, y, w, h} | null): where the video goes, in CSS pixels (the
// app's fit / zoom / pan), null = fit.
function onReady(rfb) {
	window.nivaroosEmbed = {
		key: (ks, down) => rfb.sendKey(ks, null, down),
		pointer: (x, y, m) => rfb.pointer(x, y, m),
		clip: (text) => rfb.clipboardPasteFrom(text),
		// A value it doesn't know leaves that setting as it is.
		setStream(o) {
			const v = o || {}
			if (PRESET_CHOICES.includes(v.preset)) preset.value = v.preset
			else if (v.preset === undefined && (STREAM_FPS.includes(Number(v.fps)) || STREAM_KBPS.includes(Number(v.maxKbps)))) preset.value = 'custom' // an older app
			if (typeof v.adapt === 'boolean') adapt.value = v.adapt
			if (STREAM_FPS.includes(Number(v.fps))) fps.value = Number(v.fps)
			if (STREAM_KBPS.includes(Number(v.maxKbps))) maxKbps.value = Number(v.maxKbps)
		},
		view: (r) => rfb.place(r),
	}
}

onMounted(() => {
	document.title = 'Host Desktop - NivaroOS'
})

onBeforeUnmount(() => {
	delete window.nivaroosEmbed
})
</script>

<template>
	<div class="hd-embed">
		<host-desktop-web-r-t-c
			:fallback="true"
			:auto-resume="true"
			:preset="preset"
			:adapt="adapt"
			:fps="fps"
			:max-kbps="maxKbps"
			@ready="onReady"
			@state="onState"
			@clipboard="onClipboard"
			@size="onSize"
			@stats="onStats"
			@adapt="(a) => post({ t: 'adapt', ...a })"
		></host-desktop-web-r-t-c>
		<div v-if="state !== 'live'" class="hd-embed-status" role="status" aria-live="polite">
			<span v-if="state === 'connecting'">{{ $t('Connecting...') }}</span>
			<span v-else-if="state === 'fallback'">{{ $t('Compatible mode') }}</span>
			<span v-else>{{ $t('Disconnected') }}</span>
			<span v-if="reason" class="hd-embed-reason">{{ $t(reason) }}</span>
		</div>
	</div>
</template>

<style scoped>
.hd-embed {
	position: fixed;
	inset: 0;
	background: #000;
	overflow: hidden;
}

.hd-embed-status {
	position: absolute;
	inset: 0;
	display: flex;
	flex-direction: column;
	align-items: center;
	justify-content: center;
	gap: 8px;
	padding: 16px;
	color: rgba(255, 255, 255, 0.85);
	font-size: 16px;
	text-align: center;
	pointer-events: none;
}

.hd-embed-reason {
	font-size: 14px;
	color: rgba(255, 255, 255, 0.6);
}
</style>
