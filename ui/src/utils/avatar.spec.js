import { describe, test, expect, beforeEach } from 'vitest'
import { avatarUrl, initials, checkAvatarFile, clampOffset, cropRect, currentUser, rememberedAvatar, forgetAvatar } from './avatar'

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
		expect(avatarUrl('b&o b', 'abc', 't')).toBe('/v1/users/avatar?username=b%26o+b&token=t&v=abc')
		expect(avatarUrl('bob', undefined, 't')).toBe('/v1/users/avatar?username=bob&token=t')
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
