// REST client for the optional Backup & Sync module (nivaroos-backup,
// reached only through the gateway route /v1/backup - never its port).
// Contract: docs/specs/backup-api.json (frozen, WP-0).
//
// Every call resolves to the envelope's `data` or throws a BackupError
// whose `code` is a backend error_code (errorCodes.js) or one of the
// client-side codes below. The UI never shows `detail` as the message; it
// explains `code` with backup.err.<code>.* (see apps/backup/messages.js).
//
// Calls go through the shared axios instance, so the JWT, the Language
// header and the one-refresh-then-retry 401 handling are the same as for
// every other NivaroOS API. createBackupClient(transport) takes any
// axios-like transport; tests and the fixture mock (apps/backup/
// mockTransport.js) use that instead of the network.
import { instance } from './service.js'

export const BACKUP_BASE = '/v1/backup'

// Client-side codes, for failures that never reached the service.
export const CLIENT_CODES = Object.freeze({
	// No answer, or the gateway's own 404/502/503/504: the module isn't
	// installed or isn't running.
	UNAVAILABLE: 'service_unavailable',
	// The caller aborted the request (AbortController).
	ABORTED: 'aborted'
})

export class BackupError extends Error {
	constructor({ code, status = 0, detail = '', fieldErrors = null, current = null, cause = null }) {
		super(detail || code)
		this.name = 'BackupError'
		this.code = code
		this.status = status
		this.detail = detail
		this.fieldErrors = fieldErrors || {}
		this.current = current
		if (cause) this.cause = cause
	}
}

function isAbort(err) {
	return !!(err && (err.__CANCEL__ || err.code === 'ERR_CANCELED' || err.name === 'AbortError' || err.name === 'CanceledError'))
}

function isEnvelope(body) {
	return !!(body && typeof body === 'object' && typeof body.success === 'number' && 'data' in body)
}

// toBackupError turns whatever a transport threw into a BackupError.
export function toBackupError(err) {
	if (err instanceof BackupError) return err
	if (isAbort(err)) return new BackupError({ code: CLIENT_CODES.ABORTED, cause: err })
	const res = err && err.response
	if (!res) return new BackupError({ code: CLIENT_CODES.UNAVAILABLE, detail: (err && err.message) || '', cause: err })
	const body = res.data
	const data = isEnvelope(body) ? body.data : null
	if (data && typeof data === 'object' && data.error_code) {
		return new BackupError({
			code: data.error_code,
			status: res.status,
			detail: data.detail || '',
			fieldErrors: data.field_errors,
			current: data.current || null,
			cause: err
		})
	}
	// Not our envelope: the gateway (or a proxy) answered for us.
	if ([404, 502, 503, 504].includes(res.status)) return new BackupError({ code: CLIENT_CODES.UNAVAILABLE, status: res.status, cause: err })
	if (res.status === 401) return new BackupError({ code: 'unauthorized', status: 401, cause: err })
	if (res.status === 403) return new BackupError({ code: 'forbidden', status: 403, cause: err })
	if (res.status === 429) return new BackupError({ code: 'rate_limited', status: 429, cause: err })
	return new BackupError({ code: 'internal', status: res.status, detail: (err && err.message) || '', cause: err })
}

// unwrap returns the envelope's data (or the raw body for the one route
// that isn't enveloped).
function unwrap(res, enveloped) {
	const body = res && res.data
	if (!enveloped) return body
	if (!isEnvelope(body)) throw new BackupError({ code: CLIENT_CODES.UNAVAILABLE, status: (res && res.status) || 0, detail: 'response is not a NivaroOS envelope' })
	if (body.success >= 400) {
		const d = body.data || {}
		throw new BackupError({ code: d.error_code || 'internal', status: body.success, detail: d.detail || '', fieldErrors: d.field_errors, current: d.current || null })
	}
	return body.data
}

const enc = encodeURIComponent

// Drops undefined/null/'' so they don't end up as ?key= in the URL.
function cleanParams(params) {
	if (!params) return undefined
	const out = {}
	for (const [k, v] of Object.entries(params)) {
		if (v !== undefined && v !== null && v !== '') out[k] = v
	}
	return Object.keys(out).length ? out : undefined
}

export function axiosTransport(req) {
	return instance.request(req)
}

export function createBackupClient(transport = axiosTransport) {
	async function call(method, path, { params, data, signal, enveloped = true } = {}) {
		const req = { method, url: `${BACKUP_BASE}${path}` }
		const p = cleanParams(params)
		if (p) req.params = p
		if (data !== undefined) req.data = data
		if (signal) req.signal = signal
		let res
		try {
			res = await transport(req)
		} catch (e) {
			throw toBackupError(e)
		}
		return unwrap(res, enveloped)
	}
	const get = (path, params, opts = {}) => call('get', path, { ...opts, params })
	const post = (path, data = {}, opts = {}) => call('post', path, { ...opts, data })
	const put = (path, data, opts = {}) => call('put', path, { ...opts, data })
	const del = (path, params, opts = {}) => call('delete', path, { ...opts, params })

	return {
		// Not enveloped; the install probe itself is utils/backupInstalled.js.
		health: opts => call('get', '/health', { ...opts, enveloped: false }),
		capabilities: opts => get('/capabilities', undefined, opts),

		// Locations and folders
		locations: (role, opts) => get('/locations', { role }, opts),
		browseLocation: ({ kind, ref_id, sub_path, path, dirsOnly } = {}, opts) =>
			get('/locations/browse', { kind, ref_id, sub_path, path, dirs_only: dirsOnly ? 1 : undefined }, opts),
		resolvePath: (path, opts) => post('/locations/resolve-path', { path }, opts),

		// Jobs
		listJobs: opts => get('/jobs', undefined, opts),
		getJob: (id, opts) => get(`/jobs/${enc(id)}`, undefined, opts),
		createJob: (job, opts) => post('/jobs', job, opts),
		// Sends job.revision; a stale one throws code revision_conflict with
		// err.current set to the stored job.
		updateJob: (job, opts) => put(`/jobs/${enc(job.id)}`, job, opts),
		deleteJob: (id, { purgeData = false } = {}, opts) => del(`/jobs/${enc(id)}`, { purge_data: purgeData ? 'true' : undefined }, opts),
		toggleJob: (id, enabled, opts) => post(`/jobs/${enc(id)}/toggle`, { enabled: !!enabled }, opts),
		runJob: (id, { preview = false } = {}, opts) => post(`/jobs/${enc(id)}/run`, { preview: !!preview }, opts),
		// adoptOtherJob confirms taking over a folder whose marker names
		// another job (409 dest_marker_mismatch without it).
		reconnectDest: (id, opts, { adoptOtherJob = false } = {}) => post(`/jobs/${enc(id)}/reconnect-dest`, { adopt_other_job: adoptOtherJob }, opts),
		validate: (job, opts) => post('/validate', job, opts),
		cronPreview: (cron, opts) => post('/cron/preview', { cron }, opts),

		// Runs
		listRuns: ({ jobId, status, kind, limit, before } = {}, opts) =>
			get('/runs', { job_id: jobId, status: Array.isArray(status) ? status.join(',') : status, kind, limit, before }, opts),
		getRun: (id, opts) => get(`/runs/${enc(id)}`, undefined, opts),
		getRunLog: (id, { after = 0, limit } = {}, opts) => get(`/runs/${enc(id)}/log`, { after, limit }, opts),
		cancelRun: (id, opts) => post(`/runs/${enc(id)}/cancel`, {}, opts),
		decideRun: (id, { proceed, mode } = {}, opts) => post(`/runs/${enc(id)}/decide`, mode ? { proceed: !!proceed, mode } : { proceed: !!proceed }, opts),
		getRunPreview: (id, { op, q, offset, limit } = {}, opts) => get(`/runs/${enc(id)}/preview`, { op, q, offset, limit }, opts),

		// Versions, restore and downloads
		listVersions: (jobId, opts) => get(`/jobs/${enc(jobId)}/versions`, undefined, opts),
		browseVersion: (jobId, versionId, path = '', opts) => get(`/jobs/${enc(jobId)}/versions/${enc(versionId)}/browse`, { path }, opts),
		restore: (jobId, req, opts) => post(`/jobs/${enc(jobId)}/restore`, req, opts),
		createDownload: ({ jobId, versionId, paths }, opts) => post('/downloads', { job_id: jobId, version_id: versionId, paths }, opts),

		// Settings, import and drives
		getSettings: opts => get('/settings', undefined, opts),
		putSettings: (settings, opts) => put('/settings', settings, opts),
		getMigration: opts => get('/migration', undefined, opts),
		rerunMigration: opts => post('/migration/rerun', {}, opts),
		listDrives: opts => get('/drives', undefined, opts),
		renameDrive: (uuid, label, opts) => put(`/drives/${enc(uuid)}`, { label }, opts),
		forgetDrive: (uuid, opts) => del(`/drives/${enc(uuid)}`, undefined, opts),
		busy: (kind, target, opts) => get('/busy', { kind, target }, opts)
	}
}

// downloadUrl is where a single-use download token is fetched. It needs
// no JWT (the token is the credential) and is same-origin, so a plain
// link download works.
export function downloadUrl(token) {
	return `${BACKUP_BASE}/downloads/${enc(token)}`
}

const backup = createBackupClient()
export default backup
