import { describe, expect, test } from 'vitest'
import { formatRate, stateClass, defaultInterface, ipOf, sparkPath } from './netWidget'

describe('netWidget', () => {
	test('formatRate picks a 1024-based unit', () => {
		expect(formatRate(0)).toEqual({ value: '0', unit: 'B/s' })
		expect(formatRate('x')).toEqual({ value: '0', unit: 'B/s' })
		expect(formatRate(0.5)).toEqual({ value: '512', unit: 'B/s' })
		expect(formatRate(1)).toEqual({ value: '1', unit: 'KB/s' })
		expect(formatRate(408)).toEqual({ value: '408', unit: 'KB/s' })
		expect(formatRate(1536)).toEqual({ value: '1.5', unit: 'MB/s' })
		expect(formatRate(1024 ** 3)).toEqual({ value: '1', unit: 'TB/s' })
		expect(formatRate(1024 ** 4)).toEqual({ value: '1024', unit: 'TB/s' })
	})

	test('stateClass maps operstate', () => {
		expect(stateClass(' UP\n')).toBe('up')
		expect(stateClass('lowerlayerdown')).toBe('down')
		expect(stateClass('unknown')).toBe('unknown')
		expect(stateClass(undefined)).toBe('unknown')
	})

	test('defaultInterface prefers a link that is up', () => {
		const a = { name: 'wlan0', state: 'down' }, b = { name: 'eth0', state: 'up' }
		expect(defaultInterface([a, b])).toBe(b)
		expect(defaultInterface([a])).toBe(a)
		expect(defaultInterface([])).toBe(null)
	})

	test('ipOf looks the interface up by name', () => {
		const list = [{ interface: 'eth0', ip: '192.168.1.2' }]
		expect(ipOf(list, 'eth0')).toBe('192.168.1.2')
		expect(ipOf(list, 'wlan0')).toBe('')
		expect(ipOf(null, 'eth0')).toBe('')
	})

	test('sparkPath spans the box and is flat without data', () => {
		expect(sparkPath([], 1, 200, 40)).toEqual({ line: 'M0 39 L200 39', area: '' })
		const p = sparkPath([0, 10], 10, 200, 40)
		expect(p.line).toBe('M0.0 39.0 L200.0 3.0')
		expect(p.area).toBe('M0.0 39.0 L200.0 3.0 L200 40 L0 40 Z')
	})
})
