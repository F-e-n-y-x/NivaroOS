import { describe, test, expect } from 'vitest'
import { isBusy, stepLabel, resultLabel, formatSize } from './serverUpdate'

const t = (s, a) => (a ? s.replace(/\{(\w+)\}/g, (_, k) => a[k]) : s)

describe('serverUpdate', () => {
	test('busy while the updater runs or is mid-update', () => {
		expect(isBusy({ running: true, state: { state: 'idle' } })).toBe(true)
		expect(isBusy({ running: false, state: { state: 'probation' } })).toBe(true)
		expect(isBusy({ running: false, state: { state: 'available' } })).toBe(false)
		expect(isBusy(null)).toBe(false)
	})

	test('step words follow apply.sh steps', () => {
		expect(stepLabel(t, { state: 'downloading', progress: 64 })).toBe('Downloading (64%)')
		expect(stepLabel(t, { state: 'applying', step: 'stopping' })).toBe('Stopping the services that changed')
		expect(stepLabel(t, { state: 'applying', step: 'something-new' })).toBe('Installing')
		expect(stepLabel(t, { state: 'idle' })).toBe('')
	})

	test('the last result says what was installed or why it was rolled back', () => {
		expect(resultLabel(t, { result: 'installed', from: '1.4.0', to: '1.5.0' })).toBe('Updated to 1.5.0')
		expect(resultLabel(t, { result: 'rolled_back', from: '1.4.0', to: '1.5.0', reason: 'failed: health check' })).toBe(
			'1.5.0 was rolled back to 1.4.0: failed: health check'
		)
		expect(resultLabel(t, { result: 'rolled_back', from: 'source', to: '1.5.0' })).toBe('1.5.0 was rolled back to the build from source')
		expect(resultLabel(t, null)).toBe('')
	})

	test('sizes', () => {
		expect(formatSize(134217728)).toBe('128 MB')
		expect(formatSize(2 * 1073741824)).toBe('2.0 GB')
	})
})
