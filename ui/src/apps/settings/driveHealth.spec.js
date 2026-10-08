import { describe, test, expect } from 'vitest'
import { metricText, chartKeys, defaultChartKey, chartPoints, selfTestText } from './driveHealth'

const history = [
	{ date: '2026-10-01', verdict: 'good', values: { pending: 0, reallocated: 0, temperature: 38, power_on_hours: 17500 } },
	{ date: '2026-10-05', verdict: 'watch', values: { pending: 12, reallocated: 0, temperature: 40, power_on_hours: 17549 } }
]

describe('drive health helpers', () => {
	test('metricText says hours in years, units otherwise', () => {
		expect(metricText({ value: 17549, unit: 'h' })).toBe('17,549 h · 2.0 years')
		expect(metricText({ value: 100, unit: 'h' })).toBe('100 h · 4 days')
		expect(metricText({ value: 38, unit: '°C' })).toBe('38 °C')
		expect(metricText({ value: 10, unit: '%' })).toBe('10%')
		expect(metricText({ value: 3735 })).toBe('3,735')
	})

	test('charts the counter that moved', () => {
		expect(chartKeys(history)).toEqual(['pending', 'temperature'])
		expect(defaultChartKey({ history })).toBe('pending')
		expect(defaultChartKey({ history: [history[0]] })).toBe('temperature')
		expect(defaultChartKey({ history: [] })).toBe('')
	})

	test('chartPoints spans the box; a flat line sits in the middle', () => {
		expect(chartPoints(history, 'pending', 240, 48)).toBe('0,48 240,0')
		expect(chartPoints(history, 'reallocated', 240, 48)).toBe('0,24 240,24') // flat
		expect(chartPoints([history[0]], 'pending', 240, 48)).toBe('120,24')
		expect(chartPoints(history, 'media_errors')).toBe('')
	})

	test('selfTestText', () => {
		expect(selfTestText({ supported: false })).toMatch(/does not support/)
		expect(selfTestText({ supported: true, running: true, remaining_percent: 70, last: [] })).toBe('Running · 70% left')
		expect(selfTestText({ supported: true, last: [] })).toBe('Never run')
		expect(selfTestText({ supported: true, last: [{ type: 'Short offline', result: 'Completed without error', hours: 12614 }] })).toBe('Short offline: Completed without error (at 12,614 h)')
	})
})
