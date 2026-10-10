// Pure helpers for Settings > Companion Devices > Pair a phone
// (apps/settings/PairPhoneCard.vue). The QR holds the server's addresses
// and the one-time code - never a token or password:
//
//   nivaroos://pair?code=ABCDEFGH&url=http%3A%2F%2F192.168.1.20&url=...
//
// The app (mobile/lib/utils/pair_link.dart) tries the addresses in order.

const LOOPBACK = /^(localhost|127\.|\[?::1\]?$)/

function hostPort(host, port) {
	const p = Number(port)
	return p && p !== 80 ? `${host}:${p}` : host
}

// Every address a phone may reach this server at: the one this browser
// uses (a proxy or tunnel name too), then the LAN IPs and Tailscale (name,
// then IP) on the gateway port. Duplicates and loopback are dropped.
export function pairAddresses({ origin = '', port = 0, lanIps = [], tailscaleIps = [], tailscaleName = '' } = {}) {
	const out = []
	const add = (url) => {
		if (url && !out.includes(url)) out.push(url)
	}
	try {
		const u = new URL(origin)
		if (!LOOPBACK.test(u.hostname)) add(u.origin)
	} catch {
		// no usable origin
	}
	const name = String(tailscaleName || '').replace(/\.$/, '')
	const hosts = [...lanIps, name, ...tailscaleIps]
	for (const h of hosts) {
		if (h && !LOOPBACK.test(h)) add('http://' + hostPort(h.includes(':') && !h.startsWith('[') ? `[${h}]` : h, port))
	}
	return out
}

export function pairPayload(code, addresses) {
	const q = new URLSearchParams({ code })
	for (const a of addresses) q.append('url', a)
	return 'nivaroos://pair?' + q.toString()
}

// ABCDEFGH -> ABCD-EFGH (how it is read out and typed).
export function formatPairCode(code) {
	const c = String(code || '')
	return c.length === 8 ? `${c.slice(0, 4)}-${c.slice(4)}` : c
}

// Whole seconds until expiresAt (unix seconds), never below 0.
export function secondsLeft(expiresAt, nowMs = Date.now()) {
	return Math.max(0, Math.ceil(expiresAt - nowMs / 1000))
}

// 272 -> "4:32"
export function formatCountdown(s) {
	return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`
}
