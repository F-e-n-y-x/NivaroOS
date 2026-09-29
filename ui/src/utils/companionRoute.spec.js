import { describe, expect, it } from 'vitest'
import { companionRouteInfo } from './companionRoute'

describe('companionRouteInfo', () => {
	it('says how files travel', () => {
		expect(companionRouteInfo({ is_online: true, connection: 'lan', route: 'lan' }).label).toBe('Direct · home network')
		expect(companionRouteInfo({ is_online: true, connection: 'remote', route: 'tailscale' }).label).toBe('Direct · Tailscale')
		const t = companionRouteInfo({ is_online: true, connection: 'remote', route: 'tunnel' })
		expect(t.label).toBe('Through the server tunnel · slower')
		expect(t.hint).toMatch(/Tailscale/)
		expect(companionRouteInfo({ is_online: true, connection: 'remote', route: 'tunnel_list' }).hint).toMatch(/update/)
	})
	it('never mentions the "same network"', () => {
		for (const route of ['lan', 'tailscale', 'tunnel', 'tunnel_list', '']) {
			const i = companionRouteInfo({ is_online: true, connection: 'remote', route })
			expect(`${i.label} ${i.hint}`).not.toMatch(/same network/)
		}
	})
	it('nothing for an offline phone', () => {
		expect(companionRouteInfo({ is_online: false })).toBeNull()
		expect(companionRouteInfo({ is_online: true, connection: 'offline' })).toBeNull()
		expect(companionRouteInfo(null)).toBeNull()
	})
})
