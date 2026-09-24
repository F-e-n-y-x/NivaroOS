import { describe, test, expect, vi } from 'vitest'
import fs from 'fs'
import path from 'path'

// The real transport isn't used here (it would pull in the router and
// store); every test goes through the fixture mock or a stub.
vi.mock('./service.js', () => ({ instance: { request: vi.fn() } }))

import { createBackupClient, toBackupError, BackupError, CLIENT_CODES, downloadUrl, BACKUP_BASE } from './backup'
import { createMockTransport, mockError } from '../apps/backup/mockTransport'

const doc = JSON.parse(fs.readFileSync(path.resolve(__dirname, '../../../docs/specs/backup-api.json'), 'utf8'))
const fixture = (method, p) => doc.endpoints.find(e => e.method === method && e.path === p)

describe('backup client against the WP-0 fixtures', () => {
	test('every fixture route answers with its example data', async () => {
		const t = createMockTransport(doc)
		const c = createBackupClient(t)
		expect(await c.health()).toEqual(fixture('GET', '/health').response)
		expect(await c.capabilities()).toEqual(fixture('GET', '/capabilities').response)
		expect(await c.listJobs()).toEqual(fixture('GET', '/jobs').response)
		expect(await c.getJob('bk_1a2b3c4d5e6f')).toEqual(fixture('GET', '/jobs/:id').response)
		expect(await c.getRun('run_1')).toEqual(fixture('GET', '/runs/:id').response)
		expect(await c.listVersions('bk_1')).toEqual(fixture('GET', '/jobs/:id/versions').response)
		expect(await c.browseVersion('bk_1', 'current', 'old-phone')).toEqual(fixture('GET', '/jobs/:id/versions/:vid/browse').response)
		expect(await c.getSettings()).toEqual(fixture('GET', '/settings').response)
		expect(await c.getMigration()).toEqual(fixture('GET', '/migration').response)
		expect(await c.listDrives()).toEqual(fixture('GET', '/drives').response)
		expect(await c.listDevices()).toEqual(fixture('GET', '/devices').response)
		expect((await c.runJob('bk_1')).run_id).toMatch(/^run_/)
		expect((await c.restore('bk_1', fixture('POST', '/jobs/:id/restore').request)).run_id).toMatch(/^run_/)
	})

	test('every contract route has a client method', async () => {
		const hit = new Set()
		const handlers = {}
		for (const e of doc.endpoints) {
			const key = `${e.method} ${e.path}`
			handlers[key] = ({ clone, fixture: f }) => {
				hit.add(key)
				return clone(f.response)
			}
		}
		const c = createBackupClient(createMockTransport(doc, { handlers }))
		const job = fixture('GET', '/jobs/:id').response
		await Promise.all([
			c.health(), c.capabilities(),
			c.locations('dest'), c.browseLocation({ kind: 'usb', ref_id: 'x' }), c.resolvePath('/DATA/Photos'),
			c.listJobs(), c.createJob(job), c.getJob('bk_1'), c.updateJob(job), c.deleteJob('bk_1'),
			c.toggleJob('bk_1', true), c.runJob('bk_1'), c.reconnectDest('bk_1'), c.validate(job), c.cronPreview('0 3 * * *'),
			c.listRuns(), c.getRun('run_1'), c.getRunLog('run_1'), c.cancelRun('run_1'), c.decideRun('run_1', { proceed: false }), c.getRunPreview('run_1', { op: 'delete' }),
			c.listVersions('bk_1'), c.browseVersion('bk_1', 'current'), c.restore('bk_1', fixture('POST', '/jobs/:id/restore').request),
			c.createDownload({ jobId: 'bk_1', versionId: 'current', paths: [] }),
			c.getSettings(), c.putSettings(fixture('GET', '/settings').response), c.getMigration(), c.rerunMigration(),
			c.listDrives(), c.renameDrive('3A4F-1C22', 'Stick'), c.forgetDrive('3A4F-1C22'), c.busy('app', 'immich'),
			c.listDevices(), c.enrollDevice({ name: 'Pixel 8', platform: 'android' }), c.revokeDevice('dev_1')
		])
		// The token download is a plain same-origin link (downloadUrl), not
		// an API call; device routes take the phone's own token, never the
		// web UI's session.
		const expected = doc.endpoints
			.filter(e => e.auth !== 'device')
			.map(e => `${e.method} ${e.path}`)
			.filter(k => k !== 'GET /downloads/:token')
		expect(expected.filter(k => !hit.has(k))).toEqual([])
	})

	test('builds the documented paths, bodies and query strings', async () => {
		const t = createMockTransport(doc)
		const c = createBackupClient(t)
		await c.listRuns({ jobId: 'bk_1', status: ['running', 'queued'], limit: 20 })
		await c.getRunLog('run_1', { after: 4096 })
		await c.deleteJob('bk_1', { purgeData: true })
		await c.deleteJob('bk_1')
		await c.toggleJob('bk_1', 0)
		await c.decideRun('run_9', { proceed: true, mode: 'copy_once' })
		await c.createDownload({ jobId: 'bk_1', versionId: 'current', paths: ['a.txt'] })
		await c.browseLocation({ kind: 'usb', ref_id: '3A4F-1C22', path: '', dirsOnly: true })
		expect(t.calls.map(x => [x.method, x.path, x.params, x.data])).toEqual([
			['GET', '/runs', { job_id: 'bk_1', status: 'running,queued', limit: 20 }, undefined],
			['GET', '/runs/run_1/log', { after: 4096 }, undefined],
			['DELETE', '/jobs/bk_1', { purge_data: 'true' }, undefined],
			['DELETE', '/jobs/bk_1', {}, undefined],
			['POST', '/jobs/bk_1/toggle', {}, { enabled: false }],
			['POST', '/runs/run_9/decide', {}, { proceed: true, mode: 'copy_once' }],
			['POST', '/downloads', {}, { job_id: 'bk_1', version_id: 'current', paths: ['a.txt'] }],
			['GET', '/locations/browse', { kind: 'usb', ref_id: '3A4F-1C22', dirs_only: 1 }, undefined]
		])
	})

	test('ids are escaped into the path', async () => {
		const t = createMockTransport(doc)
		await createBackupClient(t).getJob('a/b c')
		expect(t.calls[0].path).toBe('/jobs/a%2Fb%20c')
	})

	test('an ErrorBody becomes a BackupError with its code, fields and current job', async () => {
		const stored = fixture('GET', '/jobs/:id').response
		const t = createMockTransport(doc, {
			handlers: {
				'PUT /jobs/:id': () => {
					throw mockError('revision_conflict', 409, { current: stored })
				},
				'POST /jobs': () => {
					throw mockError('validation', 400, { field_errors: { 'dest.sub_path': 'path_not_allowed' } })
				}
			}
		})
		const c = createBackupClient(t)
		const conflict = await c.updateJob({ id: 'bk_1', revision: 2 }).catch(e => e)
		expect(conflict).toBeInstanceOf(BackupError)
		expect(conflict.code).toBe('revision_conflict')
		expect(conflict.status).toBe(409)
		expect(conflict.current).toEqual(stored)
		const invalid = await c.createJob({}).catch(e => e)
		expect(invalid.code).toBe('validation')
		expect(invalid.fieldErrors).toEqual({ 'dest.sub_path': 'path_not_allowed' })
	})

	test('a missing route (module not installed) is service_unavailable', async () => {
		const c = createBackupClient(createMockTransport({ endpoints: [] }))
		const err = await c.listJobs().catch(e => e)
		expect(err.code).toBe(CLIENT_CODES.UNAVAILABLE)
		expect(err.status).toBe(404)
	})
})

describe('toBackupError', () => {
	test('network failure, abort and bare statuses', () => {
		expect(toBackupError(new Error('Network Error')).code).toBe('service_unavailable')
		expect(toBackupError({ __CANCEL__: true }).code).toBe('aborted')
		expect(toBackupError({ name: 'AbortError' }).code).toBe('aborted')
		expect(toBackupError({ response: { status: 502, data: '<html>' } }).code).toBe('service_unavailable')
		expect(toBackupError({ response: { status: 401, data: '' } }).code).toBe('unauthorized')
		expect(toBackupError({ response: { status: 403, data: {} } }).code).toBe('forbidden')
		expect(toBackupError({ response: { status: 500, data: 'boom' } }).code).toBe('internal')
	})

	test('an enveloped error keeps its detail for "Show technical output"', () => {
		const e = toBackupError({ response: { status: 503, data: { success: 503, message: 'store_unavailable', data: { error_code: 'store_unavailable', detail: 'disk I/O error' } } } })
		expect(e.code).toBe('store_unavailable')
		expect(e.detail).toBe('disk I/O error')
	})

	test('a 200 that is not an envelope is refused', async () => {
		const c = createBackupClient(async () => ({ status: 200, data: '<!doctype html>' }))
		expect((await c.listJobs().catch(e => e)).code).toBe('service_unavailable')
		// ...except /health, which isn't enveloped.
		const h = createBackupClient(async () => ({ status: 200, data: { installed: true } }))
		expect(await h.health()).toEqual({ installed: true })
	})
})

test('downloadUrl is same-origin under the gateway route', () => {
	expect(downloadUrl('dl_ab/c')).toBe(`${BACKUP_BASE}/downloads/dl_ab%2Fc`)
})
