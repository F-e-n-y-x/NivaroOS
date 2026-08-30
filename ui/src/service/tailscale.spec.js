import { describe, test, expect, vi, beforeEach } from 'vitest'
import { api } from './service.js'
import tailscale from './tailscale'

vi.mock('./service.js', () => ({
	api: { get: vi.fn(), put: vi.fn() }
}))

describe('tailscale service', () => {
	beforeEach(() => {
		api.get.mockReset()
		api.put.mockReset()
	})

	test('getStatus GETs /tailscale/status', () => {
		tailscale.getStatus()
		expect(api.get).toHaveBeenCalledWith('/tailscale/status')
	})

	test('setState PUTs /tailscale/state/:state', () => {
		tailscale.setState('up')
		expect(api.put).toHaveBeenCalledWith('/tailscale/state/up')
	})

	test('getPrefs GETs /tailscale/prefs', () => {
		tailscale.getPrefs()
		expect(api.get).toHaveBeenCalledWith('/tailscale/prefs')
	})

	test('setPrefs PUTs /tailscale/prefs with the given data', () => {
		tailscale.setPrefs({ shields_up: true })
		expect(api.put).toHaveBeenCalledWith('/tailscale/prefs', { shields_up: true })
	})
})
