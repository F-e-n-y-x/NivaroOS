// Token refresh shared by every tab of this browser.
//
// All tabs share one pair of tokens in localStorage. Each tab used to
// refresh on its own and sign out on any failed refresh, and /logout
// clears localStorage for every tab - so one tab holding an older token,
// two tabs refreshing at once, or a refresh that failed for a moment (the
// user service restarting) signed out the whole browser.
//
// Now:
//   - one refresh at a time across tabs (Web Locks, or a short lease in
//     localStorage where they're missing), and one per tab (single-flight);
//   - the tokens are always read from storage right before refreshing: if
//     another tab already refreshed, its tokens are used and nothing is
//     sent;
//   - when the refresh is refused (401), storage is checked again (and
//     briefly watched) for newer tokens another tab stored meanwhile; only
//     if there are none has the session really ended;
//   - a refresh that fails any other way (network, 5xx) never signs out.
//
// Pure of Vue/axios - storage, network, locks and time are passed in (see
// tokenCoordinator.spec.js).

export const TOKEN_KEYS = { access: 'access_token', refresh: 'refresh_token', expires: 'expires_at' }
export const REFRESH_LOCK = 'nivaroos-token-refresh'
export const REFRESH_LEASE_KEY = 'nvos_refresh_lease'
export const AUTH_CHANNEL = 'nivaroos-auth'

// The session is over: sign in again. reason: the server's reason
// (password_changed, signed_out_everywhere, expired, ...) or what was
// missing locally.
export class SessionEndedError extends Error {
	constructor(reason) {
		super(`session ended: ${reason}`)
		this.name = 'SessionEndedError'
		this.sessionEnded = true
		this.reason = reason
	}
}

// Couldn't refresh right now (offline, server restarting): keep the
// session, fail only the request.
export class RefreshUnavailableError extends Error {
	constructor(cause) {
		super(`token refresh unavailable: ${(cause && cause.message) || cause}`)
		this.name = 'RefreshUnavailableError'
		this.cause = cause
	}
}

function b64urlDecode(s) {
	s = s.replace(/-/g, '+').replace(/_/g, '/')
	while (s.length % 4) s += '='
	if (typeof atob === 'function') return atob(s)
	return Buffer.from(s, 'base64').toString('binary')
}

// tokenExpiry: the token's exp in ms, 0 when it can't be read.
export function tokenExpiry(token) {
	try {
		const payload = JSON.parse(b64urlDecode(String(token).split('.')[1]))
		return typeof payload.exp === 'number' ? payload.exp * 1000 : 0
	} catch (e) {
		return 0
	}
}

// usable: present and not about to expire (unknown expiry counts as
// usable; the server decides).
export function usable(token, now) {
	if (!token) return false
	const exp = tokenExpiry(token)
	return exp === 0 || exp - now > 10 * 1000
}

/**
 * @param {object} o
 * @param {{getItem, setItem, removeItem}} o.storage   localStorage
 * @param {(refreshToken: string) => Promise<{status: number, data: any}>} o.post
 *   POST /v1/users/refresh; resolves for any HTTP answer, rejects only when
 *   there is none (network)
 * @param {object} [o.locks]     navigator.locks
 * @param {object} [o.channel]   a BroadcastChannel (AUTH_CHANNEL)
 * @param {(access: string, refresh: string) => void} [o.onTokens]  new tokens stored by this tab
 */
export function createRefreshCoordinator({
	storage,
	post,
	locks = null,
	channel = null,
	onTokens = () => {},
	now = () => Date.now(),
	sleep = (ms) => new Promise((r) => setTimeout(r, ms)),
	tabId = Math.random().toString(36).slice(2),
	leaseMs = 15000,
	graceMs = 2000,
}) {
	const get = (k) => {
		try {
			return storage.getItem(k) || ''
		} catch (e) {
			return ''
		}
	}
	const read = () => ({ access: get(TOKEN_KEYS.access), refresh: get(TOKEN_KEYS.refresh) })

	const store = (d) => {
		try {
			storage.setItem(TOKEN_KEYS.access, d.access_token)
			storage.setItem(TOKEN_KEYS.refresh, d.refresh_token)
			if (d.expires_at != null) storage.setItem(TOKEN_KEYS.expires, d.expires_at)
		} catch (e) {
			// Storage refused (private mode, quota): this tab still has them.
		}
		onTokens(d.access_token, d.refresh_token)
		if (channel) {
			try {
				channel.postMessage({ type: 'tokens' })
			} catch (e) {}
		}
	}

	// The stored access token when it's usable and isn't the one that was
	// just refused: another tab refreshed already.
	const newerThan = (sentAccess) => {
		if (sentAccess === undefined) return ''
		const { access } = read()
		return access && access !== sentAccess && usable(access, now()) ? access : ''
	}

	const readLease = () => {
		try {
			return JSON.parse(get(REFRESH_LEASE_KEY) || 'null')
		} catch (e) {
			return null
		}
	}

	// One refresh at a time across tabs.
	const withLock = async (fn) => {
		if (locks && typeof locks.request === 'function') {
			return locks.request(REFRESH_LOCK, { mode: 'exclusive' }, fn)
		}
		// Lease in localStorage: good enough where Web Locks are missing
		// (plain http on a LAN address isn't a secure context).
		const deadline = now() + leaseMs
		for (;;) {
			const l = readLease()
			if (!l || l.until < now() || l.owner === tabId) {
				try {
					storage.setItem(REFRESH_LEASE_KEY, JSON.stringify({ owner: tabId, until: now() + leaseMs }))
				} catch (e) {
					break
				}
				await sleep(20) // let a racing tab's write land, then check who won
				const mine = readLease()
				if (mine && mine.owner === tabId) break
			}
			if (now() >= deadline) break // a dead tab's lease: go ahead
			await sleep(100)
		}
		try {
			return await fn()
		} finally {
			const l = readLease()
			if (l && l.owner === tabId) {
				try {
					storage.removeItem(REFRESH_LEASE_KEY)
				} catch (e) {}
			}
		}
	}

	// After a refused refresh: did another tab store newer tokens?
	const replacedSince = (sentRefresh) => {
		const t = read()
		return t.refresh && t.refresh !== sentRefresh && usable(t.access, now()) ? t.access : ''
	}

	const doRefresh = async (sentAccess) => {
		const early = newerThan(sentAccess)
		if (early) return early
		return withLock(async () => {
			const waited = newerThan(sentAccess)
			if (waited) return waited
			const sentRefresh = read().refresh
			if (!sentRefresh) throw new SessionEndedError('no_refresh_token')
			let res
			try {
				res = await post(sentRefresh)
			} catch (e) {
				throw new RefreshUnavailableError(e)
			}
			const body = (res && res.data) || {}
			if (res && res.status === 200 && body.success == 200 && body.data && body.data.access_token) {
				store(body.data)
				return body.data.access_token
			}
			if (res && res.status === 401) {
				let newer = replacedSince(sentRefresh)
				// A tab without the lock may be storing its refresh right now.
				for (let waitedMs = 0; !newer && waitedMs < graceMs; waitedMs += 200) {
					await sleep(200)
					newer = replacedSince(sentRefresh)
				}
				if (newer) return newer
				const d = body.data && typeof body.data === 'object' ? body.data : {}
				throw new SessionEndedError(d.reason || d.refused || 'refused')
			}
			throw new RefreshUnavailableError(new Error(`HTTP ${res ? res.status : '?'}`))
		})
	}

	let inFlight = null
	// refresh(sentAccess): a fresh access token. sentAccess is the token a
	// request was just refused with (undefined: refresh regardless).
	const refresh = (sentAccess) => {
		if (!inFlight) {
			inFlight = Promise.resolve()
				.then(() => doRefresh(sentAccess))
				.finally(() => {
					inFlight = null
				})
		}
		return inFlight
	}
	return { refresh, read }
}
