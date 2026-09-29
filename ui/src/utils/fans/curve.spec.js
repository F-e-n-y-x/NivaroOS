import { describe, test, expect } from 'vitest'
import { evalCurve, validateCurve, movePoint, addPoint, removePoint, fitCurve, makeScale, curvePath, fanStateLabel, formatRpm, tempTone } from './curve'

const c = [{ t: 40, p: 30 }, { t: 60, p: 50 }, { t: 80, p: 100 }]

describe('fan curves', () => {
	test('evalCurve interpolates and is flat outside the points (same as the service)', () => {
		expect(evalCurve(c, 20)).toBe(30)
		expect(evalCurve(c, 50)).toBe(40)
		expect(evalCurve(c, 70)).toBe(75)
		expect(evalCurve(c, 95)).toBe(100)
		expect(evalCurve([], 50)).toBe(100)
	})

	test('validateCurve refuses what the service refuses', () => {
		expect(validateCurve(c, 20)).toBe('')
		expect(validateCurve([{ t: 40, p: 30 }], 20)).toMatch(/2 to 8/)
		expect(validateCurve([{ t: 40, p: 60 }, { t: 60, p: 50 }], 20)).toMatch(/slow the fan down/)
		expect(validateCurve([{ t: 40, p: 10 }, { t: 60, p: 50 }], 20)).toMatch(/between 20%/)
		expect(validateCurve([{ t: 40, p: 30 }, { t: 40, p: 50 }], 20)).toMatch(/rise/)
		expect(validateCurve([{ t: 10, p: 30 }, { t: 40, p: 50 }], 20)).toMatch(/Temperatures/)
	})

	test('movePoint keeps the curve valid whatever the drag', () => {
		// Dragged below the floor and past its neighbour.
		let m = movePoint(c, 1, 90, 5, 20)
		expect(m[1]).toEqual({ t: 79, p: 30 })
		expect(validateCurve(m, 20)).toBe('')
		// Dragged above the next point's speed.
		m = movePoint(c, 1, 55.4, 120, 20)
		expect(m[1]).toEqual({ t: 55, p: 100 })
		expect(validateCurve(m, 20)).toBe('')
		// First point can't go under the minimum temperature.
		m = movePoint(c, 0, 0, 25, 25)
		expect(m[0]).toEqual({ t: 20, p: 25 })
		// Original untouched.
		expect(c[1]).toEqual({ t: 60, p: 50 })
	})

	test('addPoint and removePoint respect the point limits', () => {
		const a = addPoint(c, 50)
		expect(a).toHaveLength(4)
		expect(a[1]).toEqual({ t: 50, p: 40 })
		expect(addPoint(c)).toHaveLength(4) // widest gap
		expect(addPoint(c, 60)).toBe(c) // taken
		let full = c
		for (let i = 0; i < 10; i++) full = addPoint(full)
		expect(full.length).toBeLessThanOrEqual(8)
		expect(validateCurve(full, 20)).toBe('')
		expect(removePoint(c, 1)).toHaveLength(2)
		expect(removePoint([{ t: 40, p: 30 }, { t: 60, p: 50 }], 0)).toHaveLength(2)
	})

	test('fitCurve lifts points to a raised minimum', () => {
		expect(fitCurve(c, 45).map((x) => x.p)).toEqual([45, 50, 100])
	})

	test('scale round-trips and the path spans the whole range', () => {
		const s = makeScale({ width: 400, height: 240 })
		expect(Math.round(s.t(s.x(63)))).toBe(63)
		expect(Math.round(s.p(s.y(42)))).toBe(42)
		const d = curvePath(c, s)
		expect(d.startsWith('M')).toBe(true)
		expect(d.split('L')).toHaveLength(5)
	})

	test('labels', () => {
		expect(fanStateLabel({ state: 'emergency' })).toMatch(/full speed/)
		expect(fanStateLabel({ state: 'manual', mode: 'curve' })).toMatch(/curve/)
		expect(fanStateLabel({ state: 'auto' })).toMatch(/Auto/)
		expect(formatRpm(2380)).toBe('2,380 RPM')
		expect(formatRpm(null)).toBe('')
		expect(tempTone(86, 85)).toBe('danger')
		expect(tempTone(78, 85)).toBe('warn')
		expect(tempTone(50, 85)).toBe('ok')
	})
})
