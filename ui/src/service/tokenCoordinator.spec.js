import { describe, test, expect, vi } from 'vitest'
import { createRefreshCoordinator, tokenExpiry, usable, SessionEndedError, RefreshUnavailableError, REFRESH_LEASE_KEY } from './tokenCoordinator'

const NOW = 1_790_000_000_000

// A JWT-shaped token (unsigned) that expires in `inSec` seconds.
function jwt(name, inSec = 3 * 3600) {
	const enc = (o) => Buffer.from(JSON.stringify(o)).toString('base64url')
	return `${enc({ alg: 'ES256' })}.${enc({ n: name, exp: Math.floor(NOW / 1000) + inSec })}.sig`
}

// One browser: every tab shares this storage.
function memoryStorage(init = {}) {
	const m = new Map(Object.entries(init))
	return {
		getItem: (k) => (m.has(k) ? m.get(k) : null),
		setItem: (k, v) => m.set(k, String(v)),
		removeItem: (k) => m.delete(k),
		map: m,
	}
}

// Web Locks, shared by the tabs of one browser.
function fakeLocks() {
	let chain = Promise.resolve()
	return {
		request: (name, opts, fn) => {
			const run = chain.then(() => fn())
			chain = run.catch(() => {})
			return run
		},
	}
}

// The user service: accepts only refresh tokens it knows, issues new ones.
function fakeServer({ valid, revoked = {} }) {
	let n = 0
	const calls = []
	const post = vi.fn(async (rt) => {
		calls.push(rt)
		await Promise.resolve()
		if (revoked[rt]) return { status: 401, data: { success: 401, data: { reason: revoked[rt] } } }
		if (!valid.has(rt)) return { status: 401, data: { success: 401, data: { refused: 'expired' } } }
		n++
		const d = { access_token: jwt(`a${n}`), refresh_token: `r${n}`, expires_at: 1 }
		valid.add(d.refresh_token)
		return { status: 200, data: { success: 200, data: d } }
	})
	return { post, calls }
}

const tab = (storage, post, extra = {}) =>
	createRefreshCoordinator({ storage, post, now: () => NOW, sleep: () => Promise.resolve(), ...extra })

describe('token helpers', () => {
	test('reads the expiry of a JWT', () => {
		expect(tokenExpiry(jwt('x', 60))).toBe(NOW + 60 * 1000)
		expect(tokenExpiry('garbage')).toBe(0)
		expect(usable(jwt('x', 60), NOW)).toBe(true)
		expect(usable(jwt('x', 5), NOW)).toBe(false) // about to expire
		expect(usable(jwt('x', -60), NOW)).toBe(false)
		expect(usable('', NOW)).toBe(false)
	})
})

describe('refresh across tabs', () => {
	test('a successful refresh stores the new tokens and tells the other tabs', async () => {
		const storage = memoryStorage({ access_token: jwt('a0', -1), refresh_token: 'r0' })
		const srv = fakeServer({ valid: new Set(['r0']) })
		const channel = { postMessage: vi.fn() }
		const onTokens = vi.fn()
		const t = tab(storage, srv.post, { channel, onTokens })
		const access = await t.refresh(storage.getItem('access_token'))
		expect(storage.getItem('access_token')).toBe(access)
		expect(storage.getItem('refresh_token')).toBe('r1')
		expect(onTokens).toHaveBeenCalledWith(access, 'r1')
		expect(channel.postMessage).toHaveBeenCalledWith({ type: 'tokens' })
	})

	// Tab B's request was refused with the old access token after tab A
	// had already refreshed: B uses A's tokens, nothing is sent.
	test('a tab with a stale access token uses the newer tokens another tab stored', async () => {
		const stale = jwt('a0', -1)
		const storage = memoryStorage({ access_token: stale, refresh_token: 'r0' })
		const srv = fakeServer({ valid: new Set(['r0']) })
		const locks = fakeLocks()
		const a = tab(storage, srv.post, { locks })
		const b = tab(storage, srv.post, { locks })
		const fromA = await a.refresh(stale)
		const fromB = await b.refresh(stale)
		expect(fromB).toBe(fromA)
		expect(srv.post).toHaveBeenCalledTimes(1)
	})

	test('two tabs refreshing at the same moment make one refresh (Web Locks)', async () => {
		const stale = jwt('a0', -1)
		const storage = memoryStorage({ access_token: stale, refresh_token: 'r0' })
		const srv = fakeServer({ valid: new Set(['r0']) })
		const locks = fakeLocks()
		const a = tab(storage, srv.post, { locks })
		const b = tab(storage, srv.post, { locks })
		const [x, y] = await Promise.all([a.refresh(stale), b.refresh(stale)])
		expect(x).toBe(y)
		expect(srv.post).toHaveBeenCalledTimes(1)
	})

	test('two tabs refreshing at the same moment make one refresh (localStorage lease, no Web Locks)', async () => {
		const stale = jwt('a0', -1)
		const storage = memoryStorage({ access_token: stale, refresh_token: 'r0' })
		const srv = fakeServer({ valid: new Set(['r0']) })
		// Real (macro-task) sleeps so the two tabs interleave like real ones.
		const sleep = (ms) => new Promise((r) => setTimeout(r, Math.min(ms, 5)))
		const now = () => NOW
		const a = createRefreshCoordinator({ storage, post: srv.post, sleep, now, tabId: 'A' })
		const b = createRefreshCoordinator({ storage, post: srv.post, sleep, now, tabId: 'B' })
		const [x, y] = await Promise.all([a.refresh(stale), b.refresh(stale)])
		expect(x).toBe(y)
		expect(srv.post).toHaveBeenCalledTimes(1)
		expect(storage.getItem(REFRESH_LEASE_KEY)).toBe(null)
	})

	test('concurrent refreshes in one tab share one request', async () => {
		const storage = memoryStorage({ access_token: jwt('a0', -1), refresh_token: 'r0' })
		const srv = fakeServer({ valid: new Set(['r0']) })
		const t = tab(storage, srv.post)
		const all = await Promise.all([t.refresh('x'), t.refresh('x'), t.refresh('x')])
		expect(new Set(all).size).toBe(1)
		expect(srv.post).toHaveBeenCalledTimes(1)
	})

	test('always sends the newest stored refresh token, not the one the tab started with', async () => {
		const storage = memoryStorage({ access_token: jwt('a0', -1), refresh_token: 'r-old' })
		const srv = fakeServer({ valid: new Set(['r-new']) })
		const t = tab(storage, srv.post)
		// Another tab signs in again before this one refreshes.
		storage.setItem('refresh_token', 'r-new')
		storage.setItem('access_token', jwt('fresh-but-refused', -1))
		await t.refresh(storage.getItem('access_token'))
		expect(srv.calls).toEqual(['r-new'])
	})

	// The 2026-09-30 sign-out: a refresh refused (401) while another tab
	// had just stored newer tokens must not sign the browser out.
	test('a refused refresh uses newer tokens another tab stored meanwhile instead of signing out', async () => {
		const storage = memoryStorage({ access_token: jwt('a0', -1), refresh_token: 'r0' })
		const post = vi.fn(async () => {
			// Tab A's refresh lands while ours is on the wire; ours is refused.
			storage.setItem('access_token', jwt('fromA'))
			storage.setItem('refresh_token', 'rA')
			return { status: 401, data: { success: 401, data: { refused: 'expired' } } }
		})
		const t = tab(storage, post)
		expect(await t.refresh('whatever')).toBe(storage.getItem('access_token'))
	})

	test('a refused refresh waits briefly for a tab that is storing its tokens', async () => {
		const storage = memoryStorage({ access_token: jwt('a0', -1), refresh_token: 'r0' })
		let sleeps = 0
		const sleep = async () => {
			if (++sleeps === 3) {
				storage.setItem('access_token', jwt('late'))
				storage.setItem('refresh_token', 'r-late')
			}
		}
		const post = async () => ({ status: 401, data: { success: 401 } })
		const t = tab(storage, post, { sleep })
		expect(await t.refresh('x')).toBe(storage.getItem('access_token'))
	})

	test('a refused refresh with no newer tokens anywhere ends the session, with the server reason', async () => {
		const storage = memoryStorage({ access_token: jwt('a0', -1), refresh_token: 'r0' })
		const srv = fakeServer({ valid: new Set(), revoked: { r0: 'password_changed' } })
		const t = tab(storage, srv.post)
		const err = await t.refresh('x').catch((e) => e)
		expect(err).toBeInstanceOf(SessionEndedError)
		expect(err.sessionEnded).toBe(true)
		expect(err.reason).toBe('password_changed')
	})

	test('no refresh token stored (signed out in another tab) ends the session without a request', async () => {
		const storage = memoryStorage({})
		const post = vi.fn()
		const err = await tab(storage, post).refresh('x').catch((e) => e)
		expect(err).toBeInstanceOf(SessionEndedError)
		expect(post).not.toHaveBeenCalled()
	})

	test('a network error or 5xx is not a sign-out', async () => {
		const storage = memoryStorage({ access_token: jwt('a0', -1), refresh_token: 'r0' })
		const down = tab(storage, async () => Promise.reject(new Error('Network Error')))
		const e1 = await down.refresh('x').catch((e) => e)
		expect(e1).toBeInstanceOf(RefreshUnavailableError)
		expect(e1.sessionEnded).toBeUndefined()
		const e2 = await tab(storage, async () => ({ status: 502, data: '' })).refresh('x').catch((e) => e)
		expect(e2).toBeInstanceOf(RefreshUnavailableError)
		expect(storage.getItem('refresh_token')).toBe('r0') // nothing cleared
	})

	test('refresh() without a refused token always asks the server', async () => {
		const storage = memoryStorage({ access_token: jwt('a0'), refresh_token: 'r0' })
		const srv = fakeServer({ valid: new Set(['r0']) })
		await tab(storage, srv.post).refresh(undefined)
		expect(srv.post).toHaveBeenCalledTimes(1)
	})
})
