import { describe, expect, it } from 'vitest'
import { formatCountdown, formatPairCode, pairAddresses, pairPayload, secondsLeft } from './pairPhone'

describe('pairAddresses', () => {
	it('lists the browser origin, LAN, then Tailscale name and IP on the gateway port', () => {
		expect(pairAddresses({
			origin: 'https://nas.example.com',
			port: 8080,
			lanIps: ['192.168.1.20'],
			tailscaleIps: ['100.64.0.5', 'fd7a:115c:a1e0::5'],
			tailscaleName: 'nas.tail1234.ts.net.',
		})).toEqual([
			'https://nas.example.com',
			'http://192.168.1.20:8080',
			'http://nas.tail1234.ts.net:8080',
			'http://100.64.0.5:8080',
			'http://[fd7a:115c:a1e0::5]:8080',
		])
	})

	it('drops duplicates, loopback and port 80', () => {
		expect(pairAddresses({ origin: 'http://192.168.1.20', port: 80, lanIps: ['192.168.1.20', '127.0.0.1'] }))
			.toEqual(['http://192.168.1.20'])
		expect(pairAddresses({ origin: 'http://localhost:8080', port: 8080, lanIps: [] })).toEqual([])
		expect(pairAddresses()).toEqual([])
	})
})

describe('pairPayload', () => {
	it('carries the code and every address, nothing else', () => {
		const p = pairPayload('ABCDEFGH', ['http://192.168.1.20:8080', 'https://nas.example.com'])
		expect(p).toBe('nivaroos://pair?code=ABCDEFGH&url=http%3A%2F%2F192.168.1.20%3A8080&url=https%3A%2F%2Fnas.example.com')
		const u = new URL(p)
		expect([...u.searchParams.keys()]).toEqual(['code', 'url', 'url'])
	})
})

describe('code display', () => {
	it('formats and counts down', () => {
		expect(formatPairCode('ABCDEFGH')).toBe('ABCD-EFGH')
		expect(secondsLeft(1000, 728_500)).toBe(272)
		expect(secondsLeft(1000, 2_000_000)).toBe(0)
		expect(formatCountdown(272)).toBe('4:32')
		expect(formatCountdown(5)).toBe('0:05')
	})
})
