import { describe, test, expect, beforeEach, vi } from 'vitest'
import { setIssuer, ticket, ticketNow, dirOf, fileMediaUrl, urlWithTicket, REUSE_MS } from './mediaTicket'

describe('media tickets', () => {
	let calls
	beforeEach(() => {
		calls = []
		setIssuer(async (kind, target) => {
			calls.push(`${kind}:${target}`)
			return `T${calls.length}`
		})
		vi.useRealTimers()
	})

	test('folder of a path', () => {
		expect(dirOf('/DATA/Photos/a.jpg')).toBe('/DATA/Photos')
		expect(dirOf('/a.jpg')).toBe('/')
		expect(dirOf('/DATA/x/')).toBe('/DATA')
	})

	test('links carry the folder ticket, never the token', async () => {
		expect(fileMediaUrl('/v1/image', { path: '/DATA/P/a b.jpg', type: 'thumbnail' })).toBe('')
		await ticket('dir', '/DATA/P')
		const u = fileMediaUrl('/v1/image', { path: '/DATA/P/a b.jpg', type: 'thumbnail' })
		expect(u).toBe('/v1/image?path=%2FDATA%2FP%2Fa+b.jpg&type=thumbnail&mt=T1')
		expect(u).not.toMatch(/token=/)
		// one request for the whole folder
		fileMediaUrl('/v1/image', { path: '/DATA/P/c.jpg' })
		expect(calls).toEqual(['dir:/DATA/P'])
	})

	test('concurrent asks share one request; a stale ticket is replaced', async () => {
		vi.useFakeTimers()
		const [a, b] = await Promise.all([ticket('url', '/v1/users/avatar'), ticket('url', '/v1/users/avatar')])
		expect(a).toBe('T1')
		expect(b).toBe('T1')
		expect(urlWithTicket('/v1/users/avatar?username=bob', '/v1/users/avatar')).toBe('/v1/users/avatar?username=bob&mt=T1')
		vi.advanceTimersByTime(REUSE_MS + 1)
		expect(ticketNow('url', '/v1/users/avatar')).toBe('T1') // still shown while the new one comes
		await ticket('url', '/v1/users/avatar')
		expect(ticketNow('url', '/v1/users/avatar')).toBe('T2')
	})
})
