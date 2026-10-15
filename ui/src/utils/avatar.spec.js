import { describe, test, expect, beforeEach, vi } from 'vitest'
import { avatarUrl, initials, checkAvatarFile, clampOffset, cropRect, currentUser, rememberedAvatar, forgetAvatar, createAvatarLookup } from './avatar'

function fakeStorage() {
	const data = {}
	return {
		getItem: k => (k in data ? data[k] : null),
		setItem: (k, v) => { data[k] = String(v) },
		removeItem: k => { delete data[k] }
	}
}

describe('avatar', () => {
	beforeEach(() => {
		globalThis.localStorage = fakeStorage()
	})

	test('URL only when there may be a picture; version makes it cacheable', () => {
		expect(avatarUrl('bob', '', 't')).toBe('')
		expect(avatarUrl('bob', 'abc', '')).toBe('')
		expect(avatarUrl('b&o b', 'abc', 't')).toBe('/v1/users/avatar?username=b%26o+b&mt=t&v=abc')
		expect(avatarUrl('bob', undefined, 't')).toBe('/v1/users/avatar?username=bob&mt=t')
	})

	test('initials', () => {
		expect(initials('ayush')).toBe('A')
		expect(initials('ayush.soni')).toBe('AS')
		expect(initials('john_doe-x')).toBe('JD')
		expect(initials('')).toBe('?')
	})

	test('file check', () => {
		expect(checkAvatarFile({ type: 'image/png', size: 1000 })).toBe('')
		expect(checkAvatarFile({ type: 'image/gif', size: 1000 })).not.toBe('')
		expect(checkAvatarFile({ type: 'image/svg+xml', size: 10 })).not.toBe('')
		expect(checkAvatarFile({ type: 'image/jpeg', size: 30 << 20 })).not.toBe('')
		expect(checkAvatarFile(null)).not.toBe('')
	})

	test('crop: zoom 1 shows the centred square; panning stops at the edges', () => {
		// 400x200 image in a 100 px view: scale 0.5 -> 200x100 on screen
		expect(cropRect(0, 0, 400, 200, 100, 1)).toEqual({ sx: 100, sy: 0, side: 200 })
		expect(clampOffset(500, 500, 400, 200, 100, 1)).toEqual({ x: 50, y: 0 })
		const o = clampOffset(-500, 0, 400, 200, 100, 1)
		expect(cropRect(o.x, o.y, 400, 200, 100, 1)).toEqual({ sx: 200, sy: 0, side: 200 })
		// zoom 2 halves the source square
		expect(cropRect(0, 0, 400, 200, 100, 2).side).toBe(100)
	})

	test('current user falls back to the copy Login stored', () => {
		localStorage.setItem('user', JSON.stringify({ username: 'alice', avatar_version: 'v1' }))
		expect(currentUser({ state: { user: { username: '' } } }).avatar_version).toBe('v1')
		expect(currentUser({ state: { user: { username: 'bob' } } }).username).toBe('bob')
	})

	test('login picture only for the account it was kept for', () => {
		localStorage.setItem('avatar_thumb', JSON.stringify({ username: 'alice', data: 'data:image/png;base64,x' }))
		expect(rememberedAvatar('alice')).toBe('data:image/png;base64,x')
		expect(rememberedAvatar('bob')).toBe('')
		forgetAvatar()
		expect(rememberedAvatar('alice')).toBe('')
	})
})

describe('login avatar lookup', () => {
	test('debounced: one request per pause in typing, the last name wins', async () => {
		vi.useFakeTimers()
		const asked = []
		const shown = []
		const lookup = createAvatarLookup(s => shown.push(s), {
			load: (url) => {
				asked.push(url)
				return url.endsWith('=owner') ? Promise.resolve() : Promise.reject(new Error('404'))
			}
		})
		for (const n of ['o', 'ow', 'own', 'owne', 'owner']) {
			lookup(n)
			vi.advanceTimersByTime(100)
		}
		await vi.advanceTimersByTimeAsync(300)
		expect(asked).toEqual(['/v1/users/login-avatar?username=owner'])
		expect(shown).toEqual(['/v1/users/login-avatar?username=owner'])

		lookup('ghost')
		await vi.advanceTimersByTimeAsync(300)
		expect(shown.at(-1)).toBe('') // unknown name: back to the placeholder, no error
		lookup('  ')
		expect(shown.at(-1)).toBe('') // empty: at once, no request
		expect(asked).toHaveLength(2)
		vi.useRealTimers()
	})

	test('a slow answer for an earlier name is dropped', async () => {
		vi.useFakeTimers()
		const shown = []
		let finishSlow
		const lookup = createAvatarLookup(s => shown.push(s), {
			load: url => url.endsWith('=slow') ? new Promise(r => { finishSlow = r }) : Promise.reject(new Error('404'))
		})
		lookup('slow')
		await vi.advanceTimersByTimeAsync(300)
		lookup('other')
		await vi.advanceTimersByTimeAsync(300)
		finishSlow()
		await vi.advanceTimersByTimeAsync(0)
		expect(shown).toEqual([''])
		vi.useRealTimers()
	})
})
