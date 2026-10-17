import { describe, expect, test } from 'vitest'
import fs from 'node:fs'
import path from 'node:path'
import { REASONS, STREAM_DEFAULTS, adaptInit, adaptStep, chooseLane, cursorAt, directRoute, displayScale, encoderLabel, loadPrefs, lossPct, normalizePrefs, presetSummary, rfbMask, savePrefs, toRemote, watchdog, wheelSteps } from './streamMode'

const memStorage = () => {
	const m = new Map()
	return { getItem: (k) => (m.has(k) ? m.get(k) : null), setItem: (k, v) => m.set(k, String(v)) }
}

describe('stream prefs', () => {
	test('default is Auto (WebRTC first), Balanced, adapting', () => {
		expect(normalizePrefs({ v: 2 })).toEqual({ v: 2, mode: 'auto', preset: 'balanced', adapt: true, fps: 30, maxKbps: 12000 })
		expect(loadPrefs(memStorage())).toEqual({ v: 2, ...STREAM_DEFAULTS })
	})
	test('junk falls back per field; a round trip keeps valid values', () => {
		expect(normalizePrefs({ v: 2, mode: 'turbo', preset: 'ultra', adapt: 'yes', fps: 144, maxKbps: '20000' })).toEqual({
			v: 2,
			mode: 'auto',
			preset: 'balanced',
			adapt: true,
			fps: 30,
			maxKbps: 20000,
		})
		const s = memStorage()
		savePrefs({ mode: 'smooth', preset: 'custom', adapt: false, fps: 60, maxKbps: 40000 }, s)
		expect(loadPrefs(s)).toEqual({ v: 2, mode: 'smooth', preset: 'custom', adapt: false, fps: 60, maxKbps: 40000 })
		s.setItem('host_desktop_stream', '{not json')
		expect(loadPrefs(s)).toEqual({ v: 2, ...STREAM_DEFAULTS })
	})
	test('v1 prefs (Smooth beta days): the chosen mode stays, raw fps/bitrate become Custom', () => {
		const s = memStorage()
		s.setItem('host_desktop_stream', JSON.stringify({ mode: 'compatible', fps: 30, maxKbps: 12000 }))
		expect(loadPrefs(s)).toMatchObject({ mode: 'compatible', preset: 'balanced', adapt: true })
		s.setItem('host_desktop_stream', JSON.stringify({ mode: 'smooth', fps: 60, maxKbps: 20000 }))
		expect(loadPrefs(s)).toMatchObject({ mode: 'smooth', preset: 'custom', fps: 60, maxKbps: 20000 })
		// An older app's __nivaroosStream has the same shape.
		expect(normalizePrefs({ fps: 30, maxKbps: 12000 })).toMatchObject({ mode: 'auto', preset: 'balanced' })
	})
})

describe('labels', () => {
	test('GPU or CPU encoding', () => {
		expect(encoderLabel('nvh264enc')).toBe('GPU · NVENC')
		expect(encoderLabel('vah264enc')).toBe('GPU · VA-API')
		expect(encoderLabel('x264enc')).toBe('CPU · x264')
		expect(encoderLabel('')).toBe('')
	})
	test('preset summary from /host/stream', () => {
		expect(presetSummary({ fps: 24, max_kbps: 2500, max_width: 1280 })).toBe('1280 wide · 24 fps · 2.5 Mbit/s')
		expect(presetSummary({ fps: 60, max_kbps: 25000, max_width: 0 })).toBe('Full size · 60 fps · 25 Mbit/s')
		expect(presetSummary(undefined)).toBe('')
	})
})

describe('directRoute', () => {
	test('LAN, Tailscale and local names go direct; a public domain (a tunnel) does not', () => {
		for (const h of ['localhost', '192.168.1.20', '10.0.0.5', '172.20.0.1', '100.101.2.3', 'nivaroos.local', 'box.tail1234.ts.net', 'nas', '[fd7a:115c:a1e0::1]', '127.0.0.1'])
			expect(directRoute(h), h).toBe(true)
		for (const h of ['nivaroos.example.com', '8.8.8.8', '100.200.1.1', '172.32.0.1', '[2001:db8::1]']) expect(directRoute(h), h).toBe(false)
	})
})

describe('adaptStep', () => {
	const run = (a, samples, base) => {
		const changes = []
		for (const s of samples) {
			a = adaptStep(a, s, base)
			if (a.change) changes.push({ at: s.now, ...a.change })
		}
		return { a, changes }
	}
	const secs = (from, n, rttMs, lossPct = 0) => Array.from({ length: n }, (_, i) => ({ now: (from + i) * 1000, rttMs, lossPct }))
	test('3 bad seconds (RTT or loss) step down one level; never below Low', () => {
		expect(run(adaptInit('high', -60000), secs(0, 2, 400), 'high').changes).toEqual([])
		expect(run(adaptInit('high', -60000), secs(0, 3, 400), 'high').changes).toEqual([{ at: 2000, preset: 'balanced', dir: 'down' }])
		expect(run(adaptInit('balanced', -60000), secs(0, 3, 5, 8), 'balanced').changes).toEqual([{ at: 2000, preset: 'low', dir: 'down' }])
		expect(run(adaptInit('low', -60000), secs(0, 30, 900, 50), 'low').changes).toEqual([])
	})
	test('a change settles for 10 s; 30 good seconds step back up, never above the chosen preset', () => {
		const { changes } = run(adaptInit('high', -60000), [...secs(0, 3, 400), ...secs(3, 12, 400), ...secs(15, 60, 10)], 'high')
		expect(changes).toEqual([
			{ at: 2000, preset: 'balanced', dir: 'down' },
			{ at: 12000, preset: 'low', dir: 'down' }, // still bad after the 10 s hold
			{ at: 45000, preset: 'balanced', dir: 'up' }, // 30 good seconds
		])
		expect(run(adaptInit('balanced', -60000), secs(0, 60, 10), 'balanced').changes).toEqual([])
	})
	test('one bad second in between resets the count; unknown RTT/loss counts as fine', () => {
		const s = [...secs(0, 2, 400), ...secs(2, 1, 10), ...secs(3, 2, 400)]
		expect(run(adaptInit('high', -60000), s, 'high').changes).toEqual([])
		expect(run(adaptInit('high', -60000), secs(0, 5, null, null), 'high').changes).toEqual([])
	})
	test('lossPct between two inbound-rtp samples', () => {
		expect(lossPct({ lost: 0, received: 100 }, { lost: 10, received: 190 })).toBe(10)
		expect(lossPct(null, { lost: 1, received: 1 })).toBeNull()
		expect(lossPct({ lost: 5, received: 5 }, { lost: 5, received: 5 })).toBeNull()
	})
})

describe('chooseLane', () => {
	const caps = (webrtc) => ({ modes: { vnc: { available: true }, webrtc } })
	test('Auto from a page reached through the internet (a tunnel): VNC at once, with the reason', () => {
		expect(chooseLane('auto', caps({ available: true, encoder: 'x264enc' }), 'nivaroos.example.com')).toEqual({ lane: 'vnc', reason: REASONS.route })
		expect(chooseLane('smooth', caps({ available: true, encoder: 'x264enc' }), 'nivaroos.example.com').lane).toBe('webrtc')
	})
	test('Compatible never tries WebRTC', () => {
		expect(chooseLane('compatible', caps({ available: true, encoder: 'nvh264enc' }))).toEqual({ lane: 'vnc', reason: '' })
	})
	test('Auto/Smooth take WebRTC when the server has it', () => {
		for (const m of ['auto', 'smooth']) expect(chooseLane(m, caps({ available: true, encoder: 'x264enc' }))).toEqual({ lane: 'webrtc', reason: '', encoder: 'x264enc' })
	})
	test('unavailable or an old sidecar → VNC with a reason', () => {
		expect(chooseLane('smooth', caps({ available: false, reason: 'No H.264 encoder' }))).toEqual({ lane: 'vnc', reason: 'No H.264 encoder' })
		expect(chooseLane('auto', caps({ available: false, reason: '' })).reason).toBe(REASONS.unavailable)
		expect(chooseLane('auto', null).reason).toBe(REASONS.unsupported)
	})
})

describe('watchdog', () => {
	const base = { now: 0, offerAt: 0, connectedAt: 0, firstFrameAt: 0, lastHbAt: 0, lastFrameAt: 0, pcState: 'connecting', fallback: true }
	test('connected but no first frame within 4 s of it: Auto falls back, Smooth re-offers', () => {
		const c = { ...base, connectedAt: 3000, pcState: 'connected' }
		expect(watchdog({ ...c, now: 7000 }).action).toBe('wait')
		expect(watchdog({ ...c, now: 7001 })).toEqual({ action: 'fallback', reason: REASONS.noVideo })
		expect(watchdog({ ...c, now: 7001, fallback: false })).toEqual({ action: 'reoffer', reason: REASONS.noVideo })
	})
	// Owner bug (Firefox on Windows, 2026-10-16): the 4 s ran from the offer,
	// so ICE gathering + DTLS + NVENC start + the decoder's start ate it.
	test('a slow connection setup is not "no video": 8 s to connect', () => {
		expect(watchdog({ ...base, now: 8000 }).action).toBe('wait')
		expect(watchdog({ ...base, now: 8001 })).toEqual({ action: 'fallback', reason: REASONS.noVideo })
	})
	test('ICE failure before the first frame falls back at once', () => {
		expect(watchdog({ ...base, now: 500, pcState: 'failed' })).toEqual({ action: 'fallback', reason: REASONS.ice })
	})
	const live = { ...base, firstFrameAt: 1000, lastHbAt: 10000, lastFrameAt: 10000, pcState: 'connected', now: 10000 }
	test('live: healthy waits, even in Auto', () => {
		expect(watchdog(live).action).toBe('wait')
	})
	test('live: dead peer, 6 s without hb, or 4 s without a frame re-offers (never falls back)', () => {
		expect(watchdog({ ...live, pcState: 'closed' })).toEqual({ action: 'reoffer', reason: REASONS.peer })
		expect(watchdog({ ...live, now: 16001, lastFrameAt: 16001 })).toEqual({ action: 'reoffer', reason: REASONS.heartbeat })
		expect(watchdog({ ...live, now: 14001, lastHbAt: 14000 })).toEqual({ action: 'reoffer', reason: REASONS.frozen })
		// The server's own hung-encoder restart (3 s) gets to heal first.
		expect(watchdog({ ...live, now: 13500, lastHbAt: 13000 }).action).toBe('wait')
	})
})

describe('input mapping', () => {
	test('toRemote undoes object-fit: contain letterboxing and clamps', () => {
		// 1920x1080 in a 960x1000 box: scale 0.5, 230 px bars top and bottom.
		const rect = { left: 100, top: 50, width: 960, height: 1000 }
		expect(toRemote(100, 280, rect, 1920, 1080)).toEqual({ x: 0, y: 0 })
		expect(toRemote(580, 550, rect, 1920, 1080)).toEqual({ x: 960, y: 540 })
		expect(toRemote(5000, 0, rect, 1920, 1080)).toEqual({ x: 1919, y: 0 })
	})
	test('toRemote: every edge CSS pixel lands on the edge remote pixel', () => {
		const rect = { left: 0, top: 0, width: 1280, height: 700 }
		const top = (700 - 1220 * (1280 / 3398)) / 2
		expect(toRemote(0, top, rect, 3398, 1220)).toEqual({ x: 0, y: 0 })
		expect(toRemote(1279.99, top + 1220 * (1280 / 3398) - 0.01, rect, 3398, 1220)).toEqual({ x: 3397, y: 1219 })
		// Inside the last remote pixel (not half-way to a neighbour that doesn't exist).
		expect(toRemote(1.9, 1.9, { left: 0, top: 0, width: 1280, height: 700 }, 640, 350)).toEqual({ x: 0, y: 0 })
	})
	test('cursorAt scales the shape and hotspot, at least half size, at most 128 px', () => {
		expect(cursorAt({ w: 32, h: 32, x: 10, y: 20 }, 1)).toEqual({ w: 32, h: 32, x: 10, y: 20, k: 1 })
		expect(cursorAt({ w: 32, h: 32, x: 10, y: 20 }, 0.5)).toEqual({ w: 16, h: 16, x: 5, y: 10, k: 0.5 })
		expect(cursorAt({ w: 32, h: 32, x: 31, y: 0 }, 0.2)).toMatchObject({ w: 16, h: 16, x: 15, y: 0 })
		expect(cursorAt({ w: 96, h: 64, x: 95 }, 2)).toMatchObject({ w: 128, h: 85, x: 127, y: 0 })
		expect(displayScale({ width: 960, height: 1000 }, 1920, 1080)).toBe(0.5)
	})
	test('rfbMask swaps middle and right', () => {
		expect(rfbMask(1)).toBe(1)
		expect(rfbMask(2)).toBe(4)
		expect(rfbMask(4)).toBe(2)
		expect(rfbMask(7)).toBe(7)
	})
	test('wheelSteps turns trackpad pixels and line deltas into notches', () => {
		let r = wheelSteps(0, 30, 0)
		expect(r).toEqual({ acc: 30, steps: 0 })
		r = wheelSteps(r.acc, 30, 0)
		expect(r).toEqual({ acc: 10, steps: 1 })
		expect(wheelSteps(0, -3, 1)).toEqual({ acc: -20, steps: -2 })
	})
})

test('every reason is in en_US.json', () => {
	const en = JSON.parse(fs.readFileSync(path.resolve(__dirname, '../../assets/lang/en_US.json'), 'utf8'))
	for (const r of Object.values(REASONS)) expect(en[r]).toBe(r)
})
