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
import { HIDDEN_GRACE_MS, REASONS, STREAM_PRESETS, adaptInit, adaptStep, chooseLane, cursorAt, displayScale, lossPct, rfbMask, toRemote, watchdog, wheelSteps } from './streamMode'

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
	// After a long time hidden (stream stopped): resume by itself when shown
	// again (the app's WebView) instead of waiting for a click.
	autoResume: { type: Boolean, default: false },
})
// state: { state: 'connecting'|'live'|'reconnecting'|'paused'|'fallback'|'error', reason }
// caps: /host/stream's modes.webrtc; adapt: { preset, dir: 'down'|'up' }
// cursor: the server's {t:'cursor', w, h, x, y, png} as is (the embed hands it to the app)
const emit = defineEmits(['ready', 'state', 'stats', 'size', 'clipboard', 'caps', 'adapt', 'cursor'])

const SESSION_EXPIRED = 'Your session expired - sign in again'
const KS = { ctrl: 0xffe3, alt: 0xffe9, del: 0xffff }

const root = ref(null)
const video = ref(null)
// The host's cursor shape as a CSS cursor ('' = the browser's arrow).
const cursorCss = ref('')

let pc = null
let dc = null
// Bumped by every new offer and by stop(): late results of an older one are dropped.
let gen = 0
let stopped = false
let encoder = ''
let offerAt = 0
let connectedAt = 0
let firstFrameAt = 0
let lastHbAt = 0
let lastFrameAt = 0
let framesDecoded = 0
let attempt = 0
let retryTimer = null
let hiddenTimer = null
let paused = false
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
let cursor = null // the server's last {"t":"cursor"}
let cursorImg = null // it decoded, for drawing it scaled
let resizeObs = null

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
		applyCursor()
	} else if (m.t === 'cursor') onCursor(m)
	else if (m.t === 'clip' && typeof m.text === 'string') emit('clipboard', m.text)
	else if (m.t === 'enc') {
		// The server (re)started the encoder (preset, resize, PLI, hang):
		// the picture pauses for a moment - that's not a freeze.
		running = m
		lastFrameAt = Date.now()
	} else if (m.t === 'err') {
		console.warn('Host Desktop stream error:', m.code)
		// Every encoder on the server failed: no picture is coming.
		if (m.code === 'encoder_failed') props.fallback ? giveUp(REASONS.encoder) : retry(REASONS.encoder)
	}
}

async function offer() {
	const my = ++gen
	closePeer()
	offerAt = lastHbAt = Date.now()
	connectedAt = firstFrameAt = 0
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
	if (stopped || retryTimer || paused) return
	const now = Date.now()
	if (!connectedAt && pc && pc.connectionState === 'connected') connectedAt = now
	const v = watchdog({
		now,
		offerAt,
		connectedAt,
		firstFrameAt,
		lastHbAt,
		// Hidden, the browser throttles frame callbacks and decoding: only
		// the heartbeat says whether the stream is alive.
		lastFrameAt: document.hidden ? now : lastFrameAt,
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

// Switching tabs keeps the peer and the server's encoder: back, the same
// session goes on at once. Only a tab hidden for HIDDEN_GRACE_MS stops
// (and says "paused" when shown again; a click resumes).
function onVisibility() {
	if (stopped) return
	clearTimeout(hiddenTimer)
	hiddenTimer = null
	if (document.hidden) {
		if (!paused) hiddenTimer = setTimeout(pauseHidden, HIDDEN_GRACE_MS)
		return
	}
	lastFrameAt = Date.now() // frame callbacks were throttled: give them a moment
	if (paused) props.autoResume ? resume() : setState('paused', REASONS.paused)
}

function pauseHidden() {
	hiddenTimer = null
	if (stopped) return
	gen++
	clearTimeout(retryTimer)
	retryTimer = null
	closePeer()
	paused = true
	setState('paused', REASONS.paused)
}

function resume() {
	if (!paused || stopped) return
	paused = false
	setState('connecting')
	offer()
}

// ── Cursor ──
// The video has no cursor in it: the host's shape (x11vnc's, via the
// sidecar) becomes the CSS cursor, so resize arrows, I-beams and hands show.
// Wayland sends none: the browser's arrow stays.
function onCursor(m) {
	cursor = m
	cursorImg = null
	emit('cursor', m)
	applyCursor()
	if (!m.w || !m.h || !m.png) return
	const img = new Image()
	img.onload = () => {
		if (cursor !== m) return
		cursorImg = img
		applyCursor()
	}
	img.src = 'data:image/png;base64,' + m.png
}

// At the display scale (fit shrinks the desktop, so the shape too, with
// its hotspot): a scaled copy once the image has loaded.
function applyCursor() {
	const c = cursor
	if (!c) return
	if (!c.w || !c.h || !c.png) return (cursorCss.value = 'none')
	const { w, h } = remoteSize()
	const r = video.value && w && h ? video.value.getBoundingClientRect() : null
	const at = cursorAt(c, r && r.width ? displayScale(r, w, h) : 1)
	let url = 'data:image/png;base64,' + c.png
	if (at.w !== c.w || at.h !== c.h) {
		if (!cursorImg) return // the shape before stays until it's ready
		const cv = document.createElement('canvas')
		cv.width = at.w
		cv.height = at.h
		const ctx = cv.getContext('2d')
		if (!ctx) return
		ctx.imageSmoothingQuality = 'high'
		ctx.drawImage(cursorImg, 0, 0, at.w, at.h)
		url = cv.toDataURL('image/png')
	}
	cursorCss.value = `url("${url}") ${at.x} ${at.y}, default`
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
	if (paused) {
		if (e.type === 'pointerdown') resume()
		return
	}
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
	resume,
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
	applyCursor()
}

onMounted(() => {
	keyboard = new Keyboard(root.value)
	keyboard.onkeyevent = (ks, code, down) => send({ t: 'k', ks, d: down })
	keyboard.grab()
	document.addEventListener('visibilitychange', onVisibility)
	tickTimer = setInterval(tick, 1000)
	statsTimer = setInterval(pollStats, 1000)
	emit('ready', rfbLike)
	if (window.ResizeObserver) {
		resizeObs = new ResizeObserver(applyCursor)
		resizeObs.observe(video.value)
	}
	start()
})

onBeforeUnmount(() => {
	stop()
	clearTimeout(hiddenTimer)
	clearInterval(tickTimer)
	clearInterval(statsTimer)
	document.removeEventListener('visibilitychange', onVisibility)
	if (resizeObs) resizeObs.disconnect()
	if (keyboard) keyboard.ungrab()
})
</script>

<template>
	<div ref="root" class="hd-webrtc" :class="{ 'is-fit': scaleToFit }" :style="cursorCss ? { cursor: cursorCss } : null" tabindex="0" @contextmenu.prevent>
		<!-- draggable / dragstart: Firefox would start a native drag of the video
		on press-and-move (pointercancel: the host's drag or resize ends). -->
		<video
			ref="video"
			class="hd-webrtc-video"
			autoplay
			muted
			playsinline
			disablepictureinpicture
			draggable="false"
			@dragstart.prevent
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
	/* The cursor isn't in the video (it would lag a frame): the local one,
	in the host's shape (cursorCss on the root, inherited), is it. */
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
