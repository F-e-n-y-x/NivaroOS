import { describe, test, expect } from 'vitest'
import { httpsUrl, canEnableRedirect } from './lanHttps'

describe('lanHttps', () => {
	test('httpsUrl drops the default port and keeps the host the browser used', () => {
		expect(httpsUrl({ hostname: '192.168.1.20' }, '443')).toBe('https://192.168.1.20/')
		expect(httpsUrl({ hostname: 'nas.local' }, 8443)).toBe('https://nas.local:8443/')
		expect(httpsUrl({ hostname: '[fd00::1]' })).toBe('https://[fd00::1]/')
	})

	test('redirect only once this browser is on the box over https', () => {
		const on = { enabled: true, running: true }
		expect(canEnableRedirect({ protocol: 'https:', hostname: '192.168.1.20' }, on)).toBe(true)
		expect(canEnableRedirect({ protocol: 'http:', hostname: '192.168.1.20' }, on)).toBe(false)
		expect(canEnableRedirect({ protocol: 'https:', hostname: 'box.tail1.ts.net' }, on)).toBe(false)
		expect(canEnableRedirect({ protocol: 'https:', hostname: '192.168.1.20' }, { enabled: true, running: false })).toBe(false)
		expect(canEnableRedirect({ protocol: 'https:', hostname: '192.168.1.20' }, null)).toBe(false)
	})
})
