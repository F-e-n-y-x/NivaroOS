// Fixture-backed transport for createBackupClient() (service/backup.js):
// answers every route of docs/specs/backup-api.json with that route's
// example response, in the real envelope, so the client, the state logic
// and the app can be exercised without the service. Tests load the
// fixture file with fs and pass it in; nothing here is bundled with it.
//
//   const transport = createMockTransport(doc, {
//     handlers: { 'GET /jobs': () => [] },          // override one route
//   })
//   const client = createBackupClient(transport)
//
// A handler gets { params, query, body, fixture, clone } and returns the
// `data` payload, or throws mockError(code, status) to answer with an
// ErrorBody. Every request is recorded in transport.calls.
import { BACKUP_BASE } from '../../service/backup'

export function mockError(code, status = 400, extra = {}) {
	const err = new Error(`${status} ${code}`)
	err.mockStatus = status
	err.mockBody = { error_code: code, ...extra }
	return err
}

function clone(v) {
	return v === undefined ? undefined : JSON.parse(JSON.stringify(v))
}

function compile(path) {
	const names = []
	const re = path.replace(/[.*+?^${}()|[\]\\]/g, '\\$&').replace(/:([a-z_]+)/g, (_, n) => {
		names.push(n)
		return '([^/]+)'
	})
	return { re: new RegExp(`^${re}$`), names }
}

function httpError(status, data) {
	const err = new Error(`Request failed with status code ${status}`)
	err.response = { status, data }
	return err
}

export function createMockTransport(doc, { handlers = {} } = {}) {
	const routes = (doc.endpoints || []).map(e => ({ ...compile(e.path), fixture: e, key: `${e.method} ${e.path}` }))
	const calls = []

	async function transport(req) {
		const method = String(req.method || 'get').toUpperCase()
		const url = String(req.url || '')
		const path = url.startsWith(BACKUP_BASE) ? url.slice(BACKUP_BASE.length) || '/' : url
		calls.push({ method, path, params: req.params || {}, data: req.data })
		for (const r of routes) {
			if (r.fixture.method !== method) continue
			const m = r.re.exec(path)
			if (!m) continue
			const params = {}
			r.names.forEach((n, i) => (params[n] = decodeURIComponent(m[i + 1])))
			const enveloped = r.fixture.envelope !== false
			let data
			const status = r.fixture.status || 200
			const handler = handlers[r.key]
			try {
				data = handler ? await handler({ params, query: req.params || {}, body: req.data, fixture: r.fixture, clone }) : clone(r.fixture.response)
			} catch (e) {
				if (!e || !e.mockStatus) throw e
				throw httpError(e.mockStatus, { success: e.mockStatus, message: e.mockBody.error_code, data: e.mockBody })
			}
			if (!enveloped) return { status, data }
			return { status, data: { success: status, message: 'ok', data } }
		}
		// What the gateway answers for a route that doesn't exist.
		throw httpError(404, '404 page not found')
	}
	transport.calls = calls
	return transport
}
