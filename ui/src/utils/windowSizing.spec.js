import { describe, test, expect, beforeEach, vi } from 'vitest'
import { openRect, rememberSize, rememberedSize } from './windowSizing'

const big = { width: 1920, height: 1080 }
const laptop = { width: 1366, height: 768 }

describe('window sizing', () => {
	beforeEach(() => {
		const store = {}
		global.localStorage = { getItem: vi.fn(k => store[k] ?? null), setItem: vi.fn((k, v) => { store[k] = v }) }
	})

	test('a main app opens large and centred on a big screen', () => {
		const r = openRect('FilesApp', { width: 960, height: 620 }, big, 0, null)
		expect(r.width).toBe(1421)
		expect(r.height).toBe(864)
		expect(r.x).toBe(Math.round((1920 - 1421) / 2))
	})

	test('never smaller than the size the app asks for, never bigger than the screen', () => {
		const r = openRect('AppStoreApp', { width: 1040, height: 720 }, laptop, 0, null)
		expect(r.width).toBe(1040)
		expect(r.height).toBe(768 - 88)
		expect(r.x + r.width).toBeLessThanOrEqual(1366)
	})

	test('a tool window keeps its own size', () => {
		const r = openRect('FeedbackPanel', { width: 520, height: 560 }, big, 0, null)
		expect(r).toMatchObject({ width: 520, height: 560, x: 80, y: 60 })
	})

	test('a size the user chose is used again, clamped to the screen', () => {
		rememberSize('VmManagerApp', 1700, 1000)
		expect(rememberedSize('VmManagerApp')).toEqual({ width: 1700, height: 1000 })
		const r = openRect('VmManagerApp', { width: 880, height: 560 }, laptop, 0, rememberedSize('VmManagerApp'))
		expect(r.width).toBe(1366 - 32)
		expect(r.height).toBe(768 - 88)
		expect(openRect('VmManagerApp', {}, big, 0, rememberedSize('VmManagerApp'))).toMatchObject({ width: 1700, height: 992 })
	})

	test('explicit positions are kept and windows stagger', () => {
		expect(openRect('FilesApp', { x: 10, y: 20 }, big, 0, null)).toMatchObject({ x: 10, y: 20 })
		const a = openRect('FilesApp', {}, big, 0, null), b = openRect('FilesApp', {}, big, 1, null)
		expect(b.x - a.x).toBe(24)
	})

	test('broken storage never breaks opening a window', () => {
		global.localStorage = { getItem: () => { throw new Error('denied') }, setItem: () => { throw new Error('denied') } }
		expect(rememberedSize('FilesApp')).toBeNull()
		expect(() => rememberSize('FilesApp', 1, 1)).not.toThrow()
	})
})
