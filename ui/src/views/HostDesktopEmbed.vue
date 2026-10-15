<!-- src/views/HostDesktopEmbed.vue
	#/host-desktop/embed?mode=smooth - the Android app's WebView: the Smooth
	(WebRTC) viewer full-bleed, nothing else. The app injects its JWT as
	window.__nivaroosToken before load (never in the URL) and listens on
	window.NivaroosBridge for {"state":"connecting|live|fallback|error","reason"}.
	On "fallback" (no WebRTC here, or no first frame within 4 s) the app
	switches to its own VNC client.
-->
<script setup>
import { onMounted, ref } from 'vue'
import HostDesktopWebRTC from '@/shell/desktop/HostDesktopWebRTC.vue'
import { loadPrefs } from '@/shell/desktop/streamMode'

const prefs = loadPrefs()
const state = ref('connecting')
const reason = ref('')
let posted = ''

function onState(e) {
	// The bridge only knows connecting|live|fallback|error.
	const s = e.state === 'reconnecting' ? 'connecting' : e.state
	state.value = s
	reason.value = e.reason || ''
	const msg = JSON.stringify({ state: s, reason: reason.value })
	if (msg === posted) return
	posted = msg
	try {
		if (window.NivaroosBridge) window.NivaroosBridge.postMessage(msg)
	} catch {}
}

function onClipboard(text) {
	if (navigator.clipboard && navigator.clipboard.writeText) navigator.clipboard.writeText(text).catch(() => {})
}

onMounted(() => {
	document.title = 'Host Desktop - NivaroOS'
})
</script>

<template>
	<div class="hd-embed">
		<host-desktop-web-r-t-c :fallback="true" :fps="prefs.fps" :max-kbps="prefs.maxKbps" @state="onState" @clipboard="onClipboard"></host-desktop-web-r-t-c>
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
