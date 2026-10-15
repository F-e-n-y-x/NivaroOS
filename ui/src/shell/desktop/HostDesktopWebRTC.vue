<!-- src/shell/desktop/HostDesktopWebRTC.vue
	Host Desktop over WebRTC (the primary lane): the host's screen as H.264
	(vm-sidecar POST /host/webrtc/offer), input over the `input` data
	channel. A quality preset (the server maps it per GPU/CPU encoder) or
	custom fps/bitrate; with `adapt` it steps the preset down when RTT or
	packet loss climbs and back up when the network is stable again (on the
	data channel: the encoder restarts, the peer stays) and says so (`adapt`). Contract: /root/nivaroos-wip/webrtc-contract.md. Used by
	HostDesktopPanel.vue (behind its toolbar) and views/HostDesktopEmbed.vue
	(the app's WebView). It reports what happens through `state`; the
	parent decides what a fallback means (the panel opens noVNC).
	`ready` hands the parent an object shaped like the bits of noVNC's RFB
	the panel's toolbar uses (sendKey, sendCtrlAltDel, clipboardPasteFrom,
	focus, disconnect), so its shortcuts, on-screen keyboard and clipboard
	work unchanged.
-->
<script setup>
import { markRaw, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import Keyboard from '@novnc-core/input/keyboard.js'
import { apiBase } from '@/api/vmSidecar'
import { reconnectDelay } from '@/utils/consoleSocket'
import { REASONS, STREAM_PRESETS, adaptInit, adaptStep, chooseLane, lossPct, rfbMask, toRemote, watchdog, wheelSteps } from './streamMode'

const props = defineProps({
	scaleToFit: { type: Boolean, default: true },
	// Auto: give up on WebRTC (state "fallback") when the first frame
	// doesn't come; Smooth keeps retrying it.
	fallback: { type: Boolean, default: true },
	// low|balanced|high, or custom (fps / maxKbps instead).
	preset: { type: String, default: 'balanced' },
	adapt: { type: Boolean, default: true },
	fps: { type: Number, default: 30 },
	maxKbps: { type: Number, default: 12000 },
})
// state: { state: 'connecting'|'live'|'reconnecting'|'fallback'|'error', reason }
// caps: /host/stream's modes.webrtc; adapt: { preset, dir: 'down'|'up' }
const emit = defineEmits(['ready', 'state', 'stats', 'size', 'clipboard', 'caps', 'adapt'])

const SESSION_EXPIRED = 'Your session expired - sign in again'
const KS = { ctrl: 0xffe3, alt: 0xffe9, del: 0xffff }

const root = ref(null)
const video = ref(null)

let pc = null
let dc = null
// Bumped by every new offer and by stop(): late results of an older one are dropped.
let gen = 0
let stopped = false
let encoder = ''
let offerAt = 0
let firstFrameAt = 0
let lastHbAt = 0
let lastFrameAt = 0
let framesDecoded = 0
let attempt = 0
let retryTimer = null
let tickTimer = null
let statsTimer = null
let remote = { w: 0, h: 0 }
let mask = 0
const wheelAcc = { x: 0, y: 0 }
let keyboard = null
// The preset streaming now: props.preset, or a step below it while the
// network is bad. Offers use it too, so a re-offer keeps the adaptation.
let adaptState = adaptInit(props.preset)
let lastRtp = null
let running = null // the server's last {"t":"enc"}

// The app's WebView injects its JWT here before load (never in the URL).
function token() {
	if (window.__nivaroosToken) return window.__nivaroosToken
	try {
		return localStorage.getItem('access_token') || ''
	} catch {
		return ''
	}
}

function api(path, body) {
	const ctl = new AbortController()
	const t = setTimeout(() => ctl.abort(), 10000)
	const headers = { Authorization: token() }
	if (body) headers['Content-Type'] = 'application/json'
	return fetch(apiBase() + path, { method: body ? 'POST' : 'GET', headers, body: body && JSON.stringify(body), signal: ctl.signal }).finally(() => clearTimeout(t))
}

const setState = (state, reason = '') => emit('state', { state, reason })

function send(m) {
	if (dc && dc.readyState === 'open') dc.send(JSON.stringify(m))
}

function closePeer() {
	if (dc) {
		send({ t: 'release' })
		dc.close()
	}
	if (pc) pc.close()
	pc = dc = null
	mask = 0
	if (video.value) video.value.srcObject = null
}

function stop() {
	stopped = true
	gen++
	clearTimeout(retryTimer)
	retryTimer = null
	closePeer()
}

function giveUp(reason) {
	stop()
	setState('fallback', reason)
}

function authFailed() {
	stop()
	setState('error', SESSION_EXPIRED)
}

// Not live yet in Smooth mode (or the server said no): try again with
// backoff instead of hammering the encoder.
function retry(reason) {
	gen++
	closePeer()
	setState('reconnecting', reason)
	clearTimeout(retryTimer)
	retryTimer = setTimeout(() => {
		retryTimer = null
		offer()
	}, reconnectDelay(attempt++))
}

function markFrame() {
	lastFrameAt = Date.now()
	if (firstFrameAt) return
	firstFrameAt = lastFrameAt
	attempt = 0
	setState('live')
}

function watchFrames(my) {
	const v = video.value
	if (!v || !v.requestVideoFrameCallback) return // getStats' framesDecoded covers it
	const cb = () => {
		if (my !== gen) return
		markFrame()
		v.requestVideoFrameCallback(cb)
	}
	v.requestVideoFrameCallback(cb)
}

function iceGathered(p) {
	return new Promise((resolve) => {
		if (p.iceGatheringState === 'complete') return resolve()
		const t = setTimeout(resolve, 2000)
		p.addEventListener('icegatheringstatechange', () => {
			if (p.iceGatheringState !== 'complete') return
			clearTimeout(t)
			resolve()
		})
	})
}

function onMessage(e) {
	let m
	try {
		m = JSON.parse(e.data)
	} catch {
		return
	}
	if (!m) return
	if (m.t === 'hb') lastHbAt = Date.now()
	else if (m.t === 'size' && m.w > 0 && m.h > 0) {
		remote = { w: m.w, h: m.h }
		emit('size', remote)
	} else if (m.t === 'clip' && typeof m.text === 'string') emit('clipboard', m.text)
	else if (m.t === 'enc') running = m
	else if (m.t === 'err') {
		console.warn('Host Desktop stream error:', m.code)
		// Every encoder on the server failed: no picture is coming.
		if (m.code === 'encoder_failed') props.fallback ? giveUp(REASONS.encoder) : retry(REASONS.encoder)
	}
}

async function offer() {
	const my = ++gen
	closePeer()
	offerAt = lastHbAt = Date.now()
	firstFrameAt = 0
	framesDecoded = 0
	lastRtp = running = null
	// Host candidates only (contract): no STUN/TURN servers.
	const p = (pc = new RTCPeerConnection())
	p.addTransceiver('video', { direction: 'recvonly' })
	dc = p.createDataChannel('input', { ordered: true })
	dc.onmessage = onMessage
	p.ontrack = (e) => {
		if ('playoutDelayHint' in e.receiver) e.receiver.playoutDelayHint = 0
		if ('jitterBufferTarget' in e.receiver) e.receiver.jitterBufferTarget = 0
		if (video.value) video.value.srcObject = (e.streams && e.streams[0]) || new MediaStream([e.track])
		watchFrames(my)
	}
	try {
		await p.setLocalDescription(await p.createOffer())
		await iceGathered(p)
		if (my !== gen) return
		const stream = STREAM_PRESETS.includes(adaptState.preset) ? { preset: adaptState.preset } : { fps: props.fps, max_kbps: props.maxKbps }
		const res = await api('/host/webrtc/offer', { sdp: p.localDescription.sdp, type: 'offer', ...stream })
		if (my !== gen) return
		if (res.status === 401) return authFailed()
		const body = await res.json().catch(() => ({}))
		if (my !== gen) return
		if (!res.ok) {
			const reason = body.reason || REASONS.unavailable
			return props.fallback ? giveUp(reason) : retry(reason)
		}
		await p.setRemoteDescription(body)
	} catch (e) {
		// Network error or a bad answer: the watchdog's first-frame rule decides.
		console.warn('Host Desktop offer failed:', e)
	}
}

function tick() {
	if (stopped || retryTimer || document.hidden) return
	const v = watchdog({
		now: Date.now(),
		offerAt,
		firstFrameAt,
		lastHbAt,
		lastFrameAt,
		pcState: pc ? pc.connectionState : 'closed',
		fallback: props.fallback,
	})
	if (v.action === 'fallback') giveUp(v.reason)
	else if (v.action === 'reoffer' && !firstFrameAt) retry(v.reason)
	else if (v.action === 'reoffer') {
		setState('reconnecting', v.reason)
		offer()
	}
}

async function pollStats() {
	const p = pc
	if (!p || !p.getStats) return
	let report
	try {
		report = await p.getStats()
	} catch {
		return
	}
	if (p !== pc) return
	let fps = null
	let rttMs = null
	let pairId = ''
	let rtp = null
	report.forEach((r) => {
		if (r.type === 'inbound-rtp' && r.kind === 'video') {
			if (r.framesPerSecond != null) fps = Math.round(r.framesPerSecond)
			rtp = { lost: r.packetsLost || 0, received: r.packetsReceived || 0 }
			if (r.framesDecoded > framesDecoded) {
				framesDecoded = r.framesDecoded
				markFrame()
			}
		} else if (r.type === 'transport' && r.selectedCandidatePairId) pairId = r.selectedCandidatePairId
	})
	report.forEach((r) => {
		const selected = pairId ? r.id === pairId : r.nominated && r.state === 'succeeded'
		if (r.type === 'candidate-pair' && selected && r.currentRoundTripTime != null) rttMs = Math.round(r.currentRoundTripTime * 1000)
	})
	const loss = lossPct(lastRtp, rtp)
	lastRtp = rtp
	if (props.adapt && firstFrameAt && dc && dc.readyState === 'open') {
		adaptState = adaptStep(adaptState, { now: Date.now(), rttMs, lossPct: loss }, props.preset)
		if (adaptState.change) {
			send({ t: 'preset', preset: adaptState.change.preset })
			emit('adapt', adaptState.change)
		}
	}
	const enc = running || {}
	emit('stats', { encoder: enc.encoder || encoder, fps, rttMs, preset: enc.preset || (STREAM_PRESETS.includes(adaptState.preset) ? adaptState.preset : 'custom') })
}

async function start() {
	setState('connecting')
	let caps = null
	try {
		const res = await api('/host/stream')
		if (res.status === 401) return authFailed()
		if (res.ok) caps = await res.json()
	} catch {}
	if (stopped) return
	if (caps && caps.modes && caps.modes.webrtc) emit('caps', caps.modes.webrtc)
	const pick = chooseLane(props.fallback ? 'auto' : 'smooth', caps, location.hostname)
	if (pick.lane !== 'webrtc') return giveUp(pick.reason)
	encoder = pick.encoder
	offer()
}

// A hidden tab decodes nothing (and the frame watchdog would misfire):
// drop the peer - the server stops the encoder - and re-offer on return.
function onVisibility() {
	if (stopped) return
	if (document.hidden) {
		clearTimeout(retryTimer)
		retryTimer = null
		gen++
		closePeer()
	} else if (!pc) {
		setState('connecting')
		offer()
	}
}

// ── Input ──
function remoteSize() {
	const v = video.value
	return remote.w ? remote : { w: (v && v.videoWidth) || 0, h: (v && v.videoHeight) || 0 }
}

function sendPointer(e, m) {
	const { w, h } = remoteSize()
	if (!w || !h) return
	const { x, y } = toRemote(e.clientX, e.clientY, video.value.getBoundingClientRect(), w, h)
	send({ t: 'p', x, y, m })
}

function onPointer(e) {
	if (e.type === 'pointerdown') {
		root.value.focus({ preventScroll: true })
		try {
			e.target.setPointerCapture(e.pointerId)
		} catch {}
	}
	mask = rfbMask(e.buttons)
	sendPointer(e, mask)
}

// Wheel bits: 8 up, 16 down, 32 left, 64 right - each notch a press + release.
function onWheel(e) {
	for (const [axis, delta, neg, pos] of [['y', e.deltaY, 8, 16], ['x', e.deltaX, 32, 64]]) {
		const r = wheelSteps(wheelAcc[axis], delta, e.deltaMode)
		wheelAcc[axis] = r.acc
		const n = Math.min(Math.abs(r.steps), 10)
		for (let i = 0; i < n; i++) {
			sendPointer(e, mask | (r.steps < 0 ? neg : pos))
			sendPointer(e, mask)
		}
	}
}

// noVNC semantics: down undefined = press and release.
function sendKey(ks, code, down) {
	if (down === undefined) {
		sendKey(ks, code, true)
		sendKey(ks, code, false)
		return
	}
	send({ t: 'k', ks, d: !!down })
}

const rfbLike = markRaw({
	scaleViewport: true,
	sendKey,
	sendCtrlAltDel() {
		for (const k of [KS.ctrl, KS.alt, KS.del]) sendKey(k, null, true)
		for (const k of [KS.del, KS.alt, KS.ctrl]) sendKey(k, null, false)
	},
	clipboardPasteFrom: (text) => send({ t: 'clip', text: String(text) }),
	focus: () => root.value && root.value.focus({ preventScroll: true }),
	disconnect: stop,
	// The app's WebView drives input itself (its native console gestures)
	// and only wants the picture: a pointer in remote pixels with an RFB
	// mask (wheel bits included), and where to draw the video.
	pointer(x, y, m) {
		const n = (v) => Math.max(0, Math.round(Number(v)) || 0)
		send({ t: 'p', x: n(x), y: n(y), m: n(m) & 0x7f })
	},
	// r: { x, y, w, h } in CSS pixels of this page, or null for "fit".
	place(r) {
		const v = video.value
		if (!v) return
		const ok = r && ['x', 'y', 'w', 'h'].every((k) => Number.isFinite(Number(r[k])))
		if (!ok) return v.removeAttribute('style')
		Object.assign(v.style, { position: 'absolute', left: `${r.x}px`, top: `${r.y}px`, width: `${r.w}px`, height: `${r.h}px`, objectFit: 'fill' })
	},
})

// A new preset (or fps / bitrate): they go with the offer, so offer again.
watch(
	() => [props.preset, props.fps, props.maxKbps],
	() => {
		adaptState = adaptInit(props.preset, Date.now())
		if (stopped || !pc) return
		setState('reconnecting')
		offer()
	},
)
// Adaptation off: back to the chosen preset.
watch(
	() => props.adapt,
	(on) => {
		if (on || adaptState.preset === props.preset) return
		adaptState = adaptInit(props.preset, Date.now())
		send({ t: 'preset', preset: props.preset })
	},
)

// Before the server's `size` message, the video's own size stands in.
function onVideoResize() {
	const v = video.value
	if (!remote.w && v && v.videoWidth) emit('size', { w: v.videoWidth, h: v.videoHeight })
}

onMounted(() => {
	keyboard = new Keyboard(root.value)
	keyboard.onkeyevent = (ks, code, down) => send({ t: 'k', ks, d: down })
	keyboard.grab()
	document.addEventListener('visibilitychange', onVisibility)
	tickTimer = setInterval(tick, 1000)
	statsTimer = setInterval(pollStats, 1000)
	emit('ready', rfbLike)
	start()
})

onBeforeUnmount(() => {
	stop()
	clearInterval(tickTimer)
	clearInterval(statsTimer)
	document.removeEventListener('visibilitychange', onVisibility)
	if (keyboard) keyboard.ungrab()
})
</script>

<template>
	<div ref="root" class="hd-webrtc" :class="{ 'is-fit': scaleToFit }" tabindex="0" @contextmenu.prevent>
		<video
			ref="video"
			class="hd-webrtc-video"
			autoplay
			muted
			playsinline
			disablepictureinpicture
			@pointerdown.prevent="onPointer"
			@pointermove="onPointer"
			@pointerup="onPointer"
			@pointercancel="onPointer"
			@wheel.prevent="onWheel"
			@resize="onVideoResize"
		></video>
	</div>
</template>

<style scoped>
.hd-webrtc {
	position: absolute;
	inset: 0;
	overflow: auto;
	background: #000;
	outline: none;
}

.hd-webrtc-video {
	display: block;
	/* The cursor isn't in the video (it would lag a frame): the local one is it. */
	cursor: default;
	touch-action: none;
	user-select: none;
}

.hd-webrtc.is-fit {
	overflow: hidden;
}

.hd-webrtc.is-fit .hd-webrtc-video {
	width: 100%;
	height: 100%;
	object-fit: contain;
}
</style>
