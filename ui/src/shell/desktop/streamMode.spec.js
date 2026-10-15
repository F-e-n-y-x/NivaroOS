import { describe, expect, test } from 'vitest'
import fs from 'node:fs'
import path from 'node:path'
import { REASONS, STREAM_DEFAULTS, chooseLane, loadPrefs, normalizePrefs, rfbMask, savePrefs, toRemote, watchdog, wheelSteps } from './streamMode'

const memStorage = () => {
	const m = new Map()
	return { getItem: (k) => (m.has(k) ? m.get(k) : null), setItem: (k, v) => m.set(k, String(v)) }
}

describe('stream prefs', () => {
	test('default is Compatible, 30 fps, 12000 kbps', () => {
		expect(normalizePrefs()).toEqual({ mode: 'compatible', fps: 30, maxKbps: 12000 })
		expect(loadPrefs(memStorage())).toEqual(STREAM_DEFAULTS)
	})
	test('junk falls back per field; a round trip keeps valid values', () => {
		expect(normalizePrefs({ mode: 'turbo', fps: 144, maxKbps: '20000' })).toEqual({ mode: 'compatible', fps: 30, maxKbps: 20000 })
		const s = memStorage()
		savePrefs({ mode: 'smooth', fps: 60, maxKbps: 40000 }, s)
		expect(loadPrefs(s)).toEqual({ mode: 'smooth', fps: 60, maxKbps: 40000 })
		s.setItem('host_desktop_stream', '{not json')
		expect(loadPrefs(s)).toEqual(STREAM_DEFAULTS)
	})
})

describe('chooseLane', () => {
	const caps = (webrtc) => ({ modes: { vnc: { available: true }, webrtc } })
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
	const base = { now: 0, offerAt: 0, firstFrameAt: 0, lastHbAt: 0, lastFrameAt: 0, pcState: 'connecting', fallback: true }
	test('no first frame within 4 s: Auto falls back, Smooth re-offers', () => {
		expect(watchdog({ ...base, now: 4000 }).action).toBe('wait')
		expect(watchdog({ ...base, now: 4001 })).toEqual({ action: 'fallback', reason: REASONS.noVideo })
		expect(watchdog({ ...base, now: 4001, fallback: false })).toEqual({ action: 'reoffer', reason: REASONS.noVideo })
	})
	test('ICE failure before the first frame falls back at once', () => {
		expect(watchdog({ ...base, now: 500, pcState: 'failed' })).toEqual({ action: 'fallback', reason: REASONS.ice })
	})
	const live = { ...base, firstFrameAt: 1000, lastHbAt: 10000, lastFrameAt: 10000, pcState: 'connected', now: 10000 }
	test('live: healthy waits, even in Auto', () => {
		expect(watchdog(live).action).toBe('wait')
	})
	test('live: dead peer, 6 s without hb, or 3 s without a frame re-offers (never falls back)', () => {
		expect(watchdog({ ...live, pcState: 'closed' })).toEqual({ action: 'reoffer', reason: REASONS.peer })
		expect(watchdog({ ...live, now: 16001, lastFrameAt: 16001 })).toEqual({ action: 'reoffer', reason: REASONS.heartbeat })
		expect(watchdog({ ...live, now: 13001, lastHbAt: 13000 })).toEqual({ action: 'reoffer', reason: REASONS.frozen })
		expect(watchdog({ ...live, now: 13000, lastHbAt: 13000 }).action).toBe('wait')
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
