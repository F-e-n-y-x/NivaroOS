// Host Desktop "Smooth (beta)" lane: which stream to open (WebRTC or the
// noVNC "Compatible" one), when to give up on WebRTC, and when a live
// WebRTC session has silently died. Pure - HostDesktopWebRTC.vue feeds it
// clocks and peer state. Rules: /root/nivaroos-wip/webrtc-contract.md
// ("Liveness / self-heal").

export const STREAM_MODES = ['auto', 'smooth', 'compatible']
export const STREAM_FPS = [30, 60]
export const STREAM_KBPS = [4000, 8000, 12000, 20000, 40000]
// Compatible until the owner flips it.
export const STREAM_DEFAULTS = { mode: 'compatible', fps: 30, maxKbps: 12000 }

export const FIRST_FRAME_MS = 4000
export const HEARTBEAT_STALE_MS = 6000
export const FRAME_STALE_MS = 3000

// Reasons are en_US.json keys (the server's own reason already is one).
export const REASONS = {
	unsupported: "This server can't stream Smooth video yet",
	unavailable: 'Smooth video is not available on this server',
	noVideo: 'No video arrived - a tunnel or firewall may block UDP port 28650',
	ice: 'The direct connection to the server failed',
	heartbeat: 'The server stopped answering',
	frozen: 'The picture froze',
	peer: 'The connection dropped',
}

const STORAGE_KEY = 'host_desktop_stream'

export function normalizePrefs(p) {
	const o = p && typeof p === 'object' ? p : {}
	return {
		mode: STREAM_MODES.includes(o.mode) ? o.mode : STREAM_DEFAULTS.mode,
		fps: STREAM_FPS.includes(Number(o.fps)) ? Number(o.fps) : STREAM_DEFAULTS.fps,
		maxKbps: STREAM_KBPS.includes(Number(o.maxKbps)) ? Number(o.maxKbps) : STREAM_DEFAULTS.maxKbps,
	}
}

// Next to the panel's other per-browser setting (host_desktop_quality_mode).
export function loadPrefs(storage = globalThis.localStorage) {
	try {
		return normalizePrefs(JSON.parse(storage.getItem(STORAGE_KEY) || '{}'))
	} catch {
		return normalizePrefs()
	}
}

export function savePrefs(prefs, storage = globalThis.localStorage) {
	const p = normalizePrefs(prefs)
	try {
		storage.setItem(STORAGE_KEY, JSON.stringify(p))
	} catch {}
	return p
}

// caps: the GET /host/stream body, or null when the sidecar has no such
// route (older build) or it failed.
export function chooseLane(mode, caps) {
	if (mode === 'compatible') return { lane: 'vnc', reason: '' }
	const w = caps && caps.modes && caps.modes.webrtc
	if (!w) return { lane: 'vnc', reason: REASONS.unsupported }
	if (!w.available) return { lane: 'vnc', reason: w.reason || REASONS.unavailable }
	return { lane: 'webrtc', reason: '', encoder: w.encoder || '' }
}

// One watchdog tick. s: { now, offerAt, firstFrameAt (0 = none yet),
// lastHbAt, lastFrameAt, pcState, fallback (Auto: give up on WebRTC
// instead of retrying it) }.
// → { action: 'wait' | 'reoffer' | 'fallback', reason }
export function watchdog(s) {
	const dead = s.pcState === 'failed' || s.pcState === 'closed'
	if (!s.firstFrameAt) {
		if (!dead && s.now - s.offerAt <= FIRST_FRAME_MS) return { action: 'wait', reason: '' }
		return { action: s.fallback ? 'fallback' : 'reoffer', reason: dead ? REASONS.ice : REASONS.noVideo }
	}
	if (dead) return { action: 'reoffer', reason: REASONS.peer }
	if (s.now - s.lastHbAt > HEARTBEAT_STALE_MS) return { action: 'reoffer', reason: REASONS.heartbeat }
	// Heartbeats still arrive, and capture is fixed-rate (ximagesrc
	// use-damage=false), so 3 s without a decoded frame is a hung encoder
	// or decoder, not a still screen.
	if (s.now - s.lastFrameAt > FRAME_STALE_MS) return { action: 'reoffer', reason: REASONS.frozen }
	return { action: 'wait', reason: '' }
}

// Client point → remote framebuffer pixel, for a <video> drawn with
// object-fit: contain inside rect (letterboxed).
export function toRemote(clientX, clientY, rect, w, h) {
	const s = Math.min(rect.width / w, rect.height / h) || 1
	const ox = rect.left + (rect.width - w * s) / 2
	const oy = rect.top + (rect.height - h * s) / 2
	const clamp = (v, max) => Math.max(0, Math.min(max - 1, Math.round(v)))
	return { x: clamp((clientX - ox) / s, w), y: clamp((clientY - oy) / s, h) }
}

// PointerEvent.buttons (1 left, 2 right, 4 middle) → RFB mask (1 left,
// 2 middle, 4 right).
export const rfbMask = (buttons) => (buttons & 1) | (buttons & 4 ? 2 : 0) | (buttons & 2 ? 4 : 0)

const WHEEL_STEP_PX = 50
// Accumulates wheel deltas (trackpads send many tiny ones) into whole
// notches. → { acc, steps } (negative steps = up/left).
export function wheelSteps(acc, delta, deltaMode) {
	const px = deltaMode === 1 ? delta * 40 : deltaMode === 2 ? delta * 800 : delta
	const total = acc + px
	const steps = Math.trunc(total / WHEEL_STEP_PX)
	return { acc: total - steps * WHEEL_STEP_PX, steps }
}
