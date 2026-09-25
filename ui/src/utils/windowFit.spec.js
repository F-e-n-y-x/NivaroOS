import { describe, test, expect } from 'vitest'
import { windowFitSizes } from './windowFit'

describe('windowFitSizes', () => {
	test('keeps the window shape at 75, 66 and 50 percent, in even numbers', () => {
		// 50% would be 866x456 - under the 480 minimum, so it's left out.
		expect(windowFitSizes({ w: 1734, h: 912 })).toEqual([
			{ w: 1300, h: 684, percent: 75 },
			{ w: 1144, h: 602, percent: 66 },
		])
	})

	test('drops sizes below 640x480 and needs a window', () => {
		expect(windowFitSizes({ w: 1000, h: 700 }).map((o) => o.percent)).toEqual([75])
		expect(windowFitSizes(null)).toEqual([])
		expect(windowFitSizes({ w: 3840, h: 2160 })[2]).toEqual({ w: 1920, h: 1080, percent: 50 })
	})
})
