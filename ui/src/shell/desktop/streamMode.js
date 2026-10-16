// Host Desktop stream lane: WebRTC first (Auto), the noVNC "Compatible"
// one as the fallback and an explicit choice. Which stream to open, the
// quality presets and their network adaptation, when to give up on WebRTC,
// and when a live WebRTC session has silently died. Pure -
// HostDesktopWebRTC.vue feeds it clocks, stats and peer state. Rules:
// /root/nivaroos-wip/webrtc-contract.md.

// 'smooth' = WebRTC only (no VNC fallback); the value is kept for saved prefs.
export const STREAM_MODES = ['auto', 'smooth', 'compatible']
// Lowest first: network adaptation steps along this.
export const STREAM_PRESETS = ['low', 'balanced', 'high']
// Custom: the raw fps / bitrate below instead of a preset.
export const PRESET_CHOICES = [...STREAM_PRESETS, 'custom']
export const STREAM_FPS = [30, 60]
export const STREAM_KBPS = [4000, 8000, 12000, 20000, 40000]
export const STREAM_DEFAULTS = { mode: 'auto', preset: 'balanced', adapt: true, fps: 30, maxKbps: 12000 }

// $t keys. The server owns the numbers (GET /host/stream "presets").
export const PRESET_LABELS = { low: 'Low bandwidth', balanced: 'Balanced', high: 'High quality', custom: 'Custom' }
export const PRESET_HINTS = {
	low: 'For slow or mobile connections',
	balanced: 'Sharp and smooth on most networks',
	high: 'Full resolution, for a fast home network',
	custom: 'Your own frame rate and bitrate',
}

// "GPU · NVENC" / "CPU · x264" for an encoder element name.
export function encoderLabel(enc) {
	return { nvh264enc: 'GPU · NVENC', vah264enc: 'GPU · VA-API', x264enc: 'CPU · x264' }[enc] || ''
}

// One preset's numbers from /host/stream, e.g. "1280 wide · 24 fps · 2.5 Mbit/s".
export function presetSummary(spec) {
	if (!spec || !spec.fps) return ''
	const w = spec.max_width ? `${spec.max_width} wide` : 'Full size'
	return `${w} · ${spec.fps} fps · ${Math.round(spec.max_kbps / 100) / 10} Mbit/s`
}

// The first frame: ICE + DTLS within CONNECT_MS of the offer, then a
// picture within FIRST_FRAME_MS of "connected" (the server only starts the
// encoder then; NVENC + the browser's decoder take a second or two).
export const CONNECT_MS = 8000
export const FIRST_FRAME_MS = 4000
export const HEARTBEAT_STALE_MS = 6000
// Longer than the server's own hung-encoder restart (3 s without RTP), so
// that heals first instead of racing a re-offer.
export const FRAME_STALE_MS = 4000
// A hidden tab keeps its stream this long (switching tabs must not
// reconnect), then stops it to free the server's encoder.
export const HIDDEN_GRACE_MS = 10 * 60 * 1000

// Reasons are en_US.json keys (the server's own reason already is one).
export const REASONS = {
	unsupported: "This server can't stream WebRTC video yet",
	unavailable: 'WebRTC video is not available on this server',
	noVideo: 'No video arrived - a tunnel or firewall may block UDP port 28650',
	ice: 'The direct connection to the server failed',
	heartbeat: 'The server stopped answering',
	frozen: 'The picture froze',
	peer: 'The connection dropped',
	encoder: 'WebRTC video failed to start on this server',
	route: 'WebRTC video needs your home network or Tailscale - this connection goes through the internet',
	paused: 'Paused while hidden - click to resume',
}

const STORAGE_KEY = 'host_desktop_stream'
const PREFS_VERSION = 2

// v1 prefs (WebRTC was "Smooth (beta)", default Compatible) were only ever
// saved by someone changing the stream settings, so their mode is a choice
// and kept; raw fps/bitrate other than the old defaults become Custom.
export function normalizePrefs(p) {
	const o = p && typeof p === 'object' ? p : {}
	const fps = STREAM_FPS.includes(Number(o.fps)) ? Number(o.fps) : STREAM_DEFAULTS.fps
	const maxKbps = STREAM_KBPS.includes(Number(o.maxKbps)) ? Number(o.maxKbps) : STREAM_DEFAULTS.maxKbps
	let preset = PRESET_CHOICES.includes(o.preset) ? o.preset : STREAM_DEFAULTS.preset
	if (o.v !== PREFS_VERSION && o.preset === undefined && (fps !== 30 || maxKbps !== 12000)) preset = 'custom'
	return {
		v: PREFS_VERSION,
		mode: STREAM_MODES.includes(o.mode) ? o.mode : STREAM_DEFAULTS.mode,
		preset,
		adapt: typeof o.adapt === 'boolean' ? o.adapt : STREAM_DEFAULTS.adapt,
		fps,
		maxKbps,
	}
}

// Next to the panel's other per-browser setting (host_desktop_quality_mode).
export function loadPrefs(storage = globalThis.localStorage) {
	try {
		return normalizePrefs(JSON.parse(storage.getItem(STORAGE_KEY) || '{"v":2}'))
	} catch {
		return normalizePrefs({ v: PREFS_VERSION })
	}
}

export function savePrefs(prefs, storage = globalThis.localStorage) {
	const p = normalizePrefs({ ...prefs, v: PREFS_VERSION })
	try {
		storage.setItem(STORAGE_KEY, JSON.stringify(p))
	} catch {}
	return p
}

// WebRTC's video is UDP to the server's LAN and Tailscale addresses only
// (contract: ICE), so Auto doesn't try it from a page reached any other way
// (a Cloudflare tunnel, a public domain) - same rule as the app's
// smoothBlockedByRoute.
export function directRoute(hostname) {
	const h = String(hostname || '').toLowerCase().replace(/^\[|\]$/g, '')
	if (!h || h === 'localhost' || (!h.includes('.') && !h.includes(':'))) return true
	if (h.endsWith('.local') || h.endsWith('.ts.net')) return true
	if (h.includes(':')) return h === '::1' || /^(?:fe80|fc|fd)/.test(h)
	const p = h.split('.').map(Number)
	if (p.length !== 4 || p.some((x) => !Number.isInteger(x) || x < 0 || x > 255)) return false
	const [a, b] = p
	return a === 10 || a === 127 || (a === 172 && b >= 16 && b <= 31) || (a === 192 && b === 168) || (a === 169 && b === 254) || (a === 100 && b >= 64 && b <= 127)
}

// caps: the GET /host/stream body, or null when the sidecar has no such
// route (older build) or it failed. hostname: the page's (Auto only).
export function chooseLane(mode, caps, hostname = '') {
	if (mode === 'compatible') return { lane: 'vnc', reason: '' }
	if (mode === 'auto' && !directRoute(hostname)) return { lane: 'vnc', reason: REASONS.route }
	const w = caps && caps.modes && caps.modes.webrtc
	if (!w) return { lane: 'vnc', reason: REASONS.unsupported }
	if (!w.available) return { lane: 'vnc', reason: w.reason || REASONS.unavailable }
	return { lane: 'webrtc', reason: '', encoder: w.encoder || '' }
}

// Network adaptation within the chosen preset (never above it): 3 bad
// seconds (RTT or packet loss) step down a level, 30 good ones step back
// up, and a change is left to settle for 10 s first.
export const ADAPT = { badRttMs: 250, badLossPct: 5, badSamples: 3, goodRttMs: 120, goodLossPct: 1, goodMs: 30000, holdMs: 10000 }

export const adaptInit = (preset, now = 0) => ({ preset, bad: 0, goodSince: 0, changedAt: now })

// a: adaptInit state; s: { now, rttMs, lossPct } (null = unknown); base:
// the user's preset. → { ...a, change: null | { preset, dir: 'down'|'up' } }
export function adaptStep(a, s, base) {
	const lv = STREAM_PRESETS.indexOf(a.preset)
	const top = STREAM_PRESETS.indexOf(base)
	const rtt = s.rttMs ?? 0
	const loss = s.lossPct ?? 0
	const bad = rtt > ADAPT.badRttMs || loss > ADAPT.badLossPct
	const good = rtt < ADAPT.goodRttMs && loss < ADAPT.goodLossPct
	const next = { ...a, bad: bad ? a.bad + 1 : 0, goodSince: good ? a.goodSince || s.now : 0, change: null }
	if (lv < 0 || top < 0 || s.now - a.changedAt < ADAPT.holdMs) return next
	let to = -1
	if (next.bad >= ADAPT.badSamples && lv > 0) to = lv - 1
	else if (lv < top && next.goodSince && s.now - next.goodSince >= ADAPT.goodMs) to = lv + 1
	if (to < 0) return next
	const preset = STREAM_PRESETS[to]
	return { ...adaptInit(preset, s.now), change: { preset, dir: to < lv ? 'down' : 'up' } }
}

// Packet loss in % between two inbound-rtp samples ({lost, received}).
export function lossPct(prev, cur) {
	if (!prev || !cur) return null
	const lost = cur.lost - prev.lost
	const got = cur.received - prev.received
	return lost + got > 0 ? Math.max(0, (100 * lost) / (lost + got)) : null
}

// One watchdog tick. s: { now, offerAt, connectedAt (0 = not yet),
// firstFrameAt (0 = none yet), lastHbAt, lastFrameAt (the server's "enc" -
// an encoder restart - counts as one), pcState, fallback (Auto: give up on
// WebRTC instead of retrying it) }.
// → { action: 'wait' | 'reoffer' | 'fallback', reason }
export function watchdog(s) {
	const dead = s.pcState === 'failed' || s.pcState === 'closed'
	if (!s.firstFrameAt) {
		const late = s.connectedAt ? s.now - s.connectedAt > FIRST_FRAME_MS : s.now - s.offerAt > CONNECT_MS
		if (!dead && !late) return { action: 'wait', reason: '' }
		return { action: s.fallback ? 'fallback' : 'reoffer', reason: dead ? REASONS.ice : REASONS.noVideo }
	}
	if (dead) return { action: 'reoffer', reason: REASONS.peer }
	if (s.now - s.lastHbAt > HEARTBEAT_STALE_MS) return { action: 'reoffer', reason: REASONS.heartbeat }
	// Heartbeats still arrive, and capture is fixed-rate (ximagesrc
	// use-damage=false), so 4 s without a decoded frame is a hung encoder
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
