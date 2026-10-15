// Media links - thumbnails, viewers, downloads, video, avatars, VM
// previews - carry a short-lived ticket (?mt=) instead of the 3 h access
// token, which ended up in browser history, copied links and screenshots
// (security audit L5). The user service signs one per folder ('dir': the
// files directly in it, through /v1/file, /v1/image, /v1/file/stream) or
// per URL ('url': an avatar, a VM preview); see
// services/common/utils/jwt/ticket.go. WebSockets keep ?token=.
import { reactive } from 'vue'

// The server drops a ticket 5 min after its last use; ask for a new one
// before that.
export const REUSE_MS = 4 * 60 * 1000

const cache = reactive({}) // 'kind:target' -> { t, at }
const pending = {}

let issue = async (kind, target) => {
	const { api } = await import('./service.js')
	const res = await api.post('/users/current/ticket', { kind, target })
	const t = res && res.data && res.data.data && res.data.data.ticket
	if (!t) throw new Error((res && res.data && res.data.message) || 'no ticket')
	return t
}

// For tests.
export function setIssuer(fn) {
	issue = fn
	for (const k of Object.keys(cache)) delete cache[k]
}

const fresh = (e) => e && Date.now() - e.at < REUSE_MS

// The ticket, asking the server when there is no fresh one.
export function ticket(kind, target) {
	const k = `${kind}:${target}`
	if (fresh(cache[k])) return Promise.resolve(cache[k].t)
	if (!pending[k]) {
		pending[k] = issue(kind, target)
			.then((t) => {
				cache[k] = { t, at: Date.now() }
				return t
			})
			.finally(() => delete pending[k])
	}
	return pending[k]
}

// The ticket now ('' until it arrives - reactive, so a template or computed
// that reads it updates then).
export function ticketNow(kind, target) {
	const e = cache[`${kind}:${target}`]
	if (!fresh(e)) ticket(kind, target).catch(() => {})
	return e ? e.t : ''
}

export function dirOf(path) {
	const p = String(path || '').replace(/\/+$/, '')
	const i = p.lastIndexOf('/')
	return i <= 0 ? '/' : p.slice(0, i)
}

// Wait for the ticket of a file's folder (before a one-shot fileMediaUrl).
export const fileTicket = (path) => ticket('dir', dirOf(path))

// base?params&mt=<ticket of params.path's folder>, or '' until it arrives.
export function fileMediaUrl(base, params) {
	const t = ticketNow('dir', dirOf(params.path))
	return t ? `${base}?${new URLSearchParams({ ...params, mt: t })}` : ''
}

// url with the 'url' ticket for urlPath (as the gateway sees it), or ''.
export function urlWithTicket(url, urlPath) {
	const t = ticketNow('url', urlPath)
	return t ? `${url}${url.includes('?') ? '&' : '?'}mt=${encodeURIComponent(t)}` : ''
}
