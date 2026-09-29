import { describe, it, expect } from 'vitest'
import { isStuck, stuckReason, diagReport } from './signinDiag'

const t = (s, p = {}) => s.replace(/\{(\w+)\}/g, (_, k) => p[k])

describe('sign-in diagnostics', () => {
	it('only reports stuck when the server says so', () => {
		expect(isStuck(null)).toBe(false)
		expect(isStuck({ stuck: false })).toBe(false)
		expect(isStuck({ stuck: true })).toBe(true)
	})

	it('explains the most specific cause first', () => {
		expect(stuckReason({ blocked: ['ndus@1024terabox.com (set): ThirdPartyPhaseout'], rejected: 3 }, 'TeraBox', t)).toMatch(/refused some of the cookies TeraBox sets while signing in, so/)
		expect(stuckReason({ blocked: ['x@terabox.com (set): SecureOnly'] }, 'TeraBox', t)).toBe('The browser refused some of the cookies TeraBox sets while signing in.')
		expect(stuckReason({ lost: true, rejected: 2 }, 'TeraBox', t)).toMatch(/signed this window out again/)
		expect(stuckReason({ rejected: 2 }, 'TeraBox', t)).toMatch(/did not accept/)
		expect(stuckReason({ loads: 6 }, 'TeraBox', t)).toMatch(/keeps reloading/)
	})

	it('builds a report with names and reasons only', () => {
		const r = diagReport({ loads: 5, seconds: 70, rejected: 1, blocked: ['ndus@1024terabox.com (set): ThirdPartyPhaseout'], seen: ['1024terabox.com: browserid, lang'] }, 'TeraBox')
		expect(r).toContain('page loads: 5, seconds: 70, rejected by TeraBox: 1, signed out again: no')
		expect(r).toContain('  ndus@1024terabox.com (set): ThirdPartyPhaseout')
		expect(r).toContain('  1024terabox.com: browserid, lang')
		expect(diagReport(null, 'TeraBox')).toContain('(none)')
	})
})
