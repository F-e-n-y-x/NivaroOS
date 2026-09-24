import { describe, test, expect, vi } from 'vitest'
import fs from 'fs'
import path from 'path'

vi.mock('../../../service/service.js', () => ({ instance: { request: vi.fn() } }))

import {
	overallState,
	attentionItems,
	attentionFor,
	runningItems,
	upcomingItems,
	filterJobs,
	sortJobs,
	distinctCrons,
	retentionMessage,
	liveFromEvent,
	runProgress,
	groupRunsByDay,
	normalizeActivityFilters,
	activityQuery,
	runInRange,
	localPath,
	joinPath,
	parentPath,
	isFinalStatus,
	decisionPayload,
	HEALTH_VIEW,
	STATUS_VIEW
} from '../state'
import { createFormatter } from '../format'
import { renderMessage, errorCodeFromMessage, explainError, errorText } from '../messages'
import { ERROR_CODES } from '../errorCodes'

const doc = JSON.parse(fs.readFileSync(path.resolve(__dirname, '../../../../../docs/specs/backup-api.json'), 'utf8'))
const en = JSON.parse(fs.readFileSync(path.resolve(__dirname, '../../../assets/lang/en_US.json'), 'utf8'))
const fixture = (method, p) => JSON.parse(JSON.stringify(doc.endpoints.find(e => e.method === method && e.path === p).response))

// A vue-i18n-like t() over en_US.json, named {placeholders} only.
const t = (key, args = {}) => {
	const s = en[key]
	if (s === undefined) return key
	return s.replace(/\{(\w+)\}/g, (m, n) => (n in args ? String(args[n]) : m))
}

const jobs = () => fixture('GET', '/jobs')
const [photos, immich] = jobs()

describe('overview state', () => {
	test('no jobs is the empty state', () => {
		expect(overallState([]).state).toBe('empty')
		expect(overallState(null).state).toBe('empty')
	})

	test('the fixtures: a failed cloud job is a problem, last success is the mirror', () => {
		const s = overallState(jobs())
		expect(s).toEqual({ state: 'problem', jobs: 2, attention: 1, lastSuccess: photos.last_run.ended_at })
	})

	test('only healthy jobs is all good; warnings are attention', () => {
		expect(overallState([photos]).state).toBe('ok')
		expect(overallState([{ ...photos, health: 'offline' }]).state).toBe('attention')
		expect(overallState([{ ...photos, health: 'disabled', enabled: false }]).state).toBe('ok')
	})

	test('a failed run is explained by the code in its summary, with its fixes', () => {
		const [item] = attentionItems(jobs())
		expect(item.job.id).toBe(immich.id)
		expect(item.code).toBe('cloud_auth')
		expect(item.actions).toEqual(['fix_sign_in', 'view_log'])
		expect(item.runId).toBe(immich.last_run.id)
	})

	test('a run waiting for a decision comes first and offers Review', () => {
		const waiting = { ...photos, health: 'problem', active_run: { id: 'run_w', kind: 'backup', status: 'waiting_user', ended_at: null, summary: null } }
		const items = attentionItems([immich, waiting])
		expect(items.map(i => i.reason)).toEqual(['waiting', 'error'])
		const w = attentionFor(waiting)
		expect(w).toMatchObject({ reason: 'waiting', actions: ['review'], runId: 'run_w', severity: 'problem' })
	})

	test('attention reasons for offline, dest changed, unresolved import, partial and stale', () => {
		expect(attentionFor({ ...photos, health: 'offline' })).toMatchObject({ code: 'dest_offline', actions: ['how_to_run', 'change_dest'] })
		expect(attentionFor({ ...photos, needs_attention: 'dest_changed' })).toMatchObject({ code: 'dest_marker_mismatch', actions: ['reconnect_dest', 'change_dest'] })
		expect(attentionFor({ ...photos, needs_attention: 'migrated_unresolved', health: 'disabled' })).toMatchObject({ reason: 'migrated_unresolved' })
		expect(attentionFor({ ...photos, health: 'warning', last_run: { ...photos.last_run, status: 'partial' } })).toMatchObject({ reason: 'partial', actions: ['view_log', 'retry'] })
		expect(attentionFor({ ...photos, health: 'warning' })).toMatchObject({ reason: 'stale' })
		expect(attentionFor(photos)).toBe(null)
		// An unknown code in the summary falls back to internal.
		expect(attentionFor({ ...immich, last_run: { ...immich.last_run, summary: null } }).code).toBe('internal')
	})

	test('every attention reason and action is labelled in en_US.json', () => {
		for (const r of ['waiting', 'migrated_unresolved', 'partial', 'stale']) {
			expect(en[`backup.attention.${r}.title`]).toBeTruthy()
			expect(en[`backup.attention.${r}.cause`]).toBeTruthy()
		}
		for (const s of ['empty', 'problem', 'attention', 'ok']) expect(en[`backup.overview.state.${s}`]).toBeTruthy()
		for (const h of Object.keys(HEALTH_VIEW)) expect(en[`backup.health.${h}`]).toBeTruthy()
		for (const s of Object.keys(STATUS_VIEW)) expect(en[`backup.status.${s}`]).toBeTruthy()
	})

	test('running and coming up', () => {
		const running = { ...photos, active_run: { id: 'run_r', kind: 'backup', status: 'running', ended_at: null, summary: null } }
		const queued = { ...immich, active_run: { id: 'run_q', kind: 'backup', status: 'queued', ended_at: null, summary: null } }
		expect(runningItems([queued, running, { ...photos, id: 'idle' }]).map(j => j.active_run.id)).toEqual(['run_r', 'run_q'])
		const up = upcomingItems(jobs())
		// Immich runs tomorrow 03:00, the mirror on Sunday; the mirror also runs on plug-in.
		expect(up.map(u => [u.job.id, u.kind])).toEqual([
			[immich.id, 'schedule'],
			[photos.id, 'schedule'],
			[photos.id, 'plug']
		])
		// A job that is running now isn't "coming up"; a paused one never is.
		expect(upcomingItems([running, { ...immich, enabled: false }]).map(u => u.kind)).toEqual(['plug'])
	})
})

describe('jobs list', () => {
	test('filters', () => {
		const list = jobs()
		expect(filterJobs(list, { filter: 'problems' }).map(j => j.id)).toEqual([immich.id])
		expect(filterJobs(list, { filter: 'imported' }).map(j => j.id)).toEqual([immich.id])
		expect(filterJobs(list, { filter: 'disabled' })).toEqual([])
		expect(filterJobs(list, { filter: 'running' })).toEqual([])
		expect(filterJobs(list, { query: 'sandisk' }).map(j => j.id)).toEqual([photos.id])
		expect(filterJobs(list, { query: 'google' }).map(j => j.id)).toEqual([immich.id])
		expect(filterJobs(list, { query: '  ' })).toHaveLength(2)
	})

	test('sorts', () => {
		const list = jobs()
		expect(sortJobs(list, 'next_run').map(j => j.id)).toEqual([immich.id, photos.id])
		expect(sortJobs(list, 'name').map(j => j.id)).toEqual([immich.id, photos.id])
		expect(sortJobs(list, 'last_run').map(j => j.id)).toEqual([photos.id, immich.id])
		expect(sortJobs(list, 'health').map(j => j.id)).toEqual([immich.id, photos.id])
		// No next run sorts last, and the input isn't mutated.
		const manual = { ...photos, id: 'm', name: 'A', next_run: null }
		const input = [manual, ...list]
		expect(sortJobs(input, 'next_run').map(j => j.id)).toEqual([immich.id, photos.id, 'm'])
		expect(input[0]).toBe(manual)
	})

	test('distinct crons and retention', () => {
		expect(distinctCrons(jobs())).toEqual(['0 3 * * 0', '0 3 * * *'])
		expect(retentionMessage(photos)).toEqual({ key: 'backup.keep.recycle_days', args: { days: 30 } })
		expect(retentionMessage(immich)).toEqual({ key: 'backup.keep.last_archives', args: { keep: 8 } })
		expect(retentionMessage({ ...photos, retention: {} })).toEqual({ key: 'backup.keep.recycle_forever' })
		expect(retentionMessage({ ...photos, type: 'copy' })).toBe(null)
	})
})

describe('live progress', () => {
	test('socket strings become numbers; unknown totals are indeterminate', () => {
		const live = liveFromEvent({ run_id: 'r', bytes: '8100000000', total_bytes: '11900000000', files: '3210', total_files: '4700', speed_bps: '12300000', eta_sec: '360', errors: '0', current_file: 'a.jpg' })
		expect(live).toEqual({ bytes: 8100000000, total_bytes: 11900000000, files: 3210, total_files: 4700, speed_bps: 12300000, eta_sec: 360, errors: 0, current_file: 'a.jpg' })
		expect(runProgress(live)).toEqual({ ratio: 8100000000 / 11900000000, percent: 68 })
		expect(runProgress({ ...live, total_bytes: 0 }).percent).toBe(68)
		expect(runProgress({ ...live, total_bytes: 0, total_files: 0 })).toEqual({ ratio: null, percent: null })
		expect(runProgress(null).ratio).toBe(null)
		expect(liveFromEvent({ eta_sec: '-1', bytes: 'x' })).toMatchObject({ eta_sec: null, bytes: null })
	})

	test('the fixture run detail reads as 68 %', () => {
		expect(runProgress(fixture('GET', '/runs/:id').live).percent).toBe(68)
	})
})

describe('activity', () => {
	const fmt = createFormatter({ locale: 'en_us', timeZone: 'Europe/Berlin', t })

	test('runs group by server-time day', () => {
		const runs = [
			{ id: 'c', ended_at: '2026-09-24T01:30:00+02:00' },
			{ id: 'b', ended_at: '2026-09-23T23:30:00Z' }, // 01:30 on the 24th in Berlin
			{ id: 'a', ended_at: '2026-09-23T10:00:00+02:00' }
		]
		expect(groupRunsByDay(runs, fmt).map(g => [g.day, g.runs.map(r => r.id)])).toEqual([
			['2026-09-24', ['c', 'b']],
			['2026-09-23', ['a']]
		])
	})

	test('filters survive bad storage and map to the /runs query', () => {
		expect(normalizeActivityFilters('junk')).toEqual({ jobId: '', result: 'all', kind: 'all', range: '7d' })
		expect(normalizeActivityFilters({ result: 'nope', kind: 'restore', range: '30d', jobId: 5 })).toEqual({ jobId: '', result: 'all', kind: 'restore', range: '30d' })
		expect(activityQuery({ jobId: 'bk_1', result: 'problems', kind: 'backup' })).toEqual({ jobId: 'bk_1', kind: 'backup', status: ['failed', 'partial', 'interrupted', 'waiting_user'] })
		expect(activityQuery({})).toEqual({})
		const now = Date.parse('2026-09-24T12:00:00Z')
		expect(runInRange({ ended_at: '2026-09-24T01:00:00Z' }, '1d', now)).toBe(true)
		expect(runInRange({ ended_at: '2026-09-20T01:00:00Z' }, '1d', now)).toBe(false)
		expect(runInRange({ ended_at: '2026-09-20T01:00:00Z' }, 'all', now)).toBe(true)
	})

	test('final statuses', () => {
		expect(['success', 'partial', 'failed', 'cancelled', 'skipped', 'interrupted'].every(isFinalStatus)).toBe(true)
		expect(['queued', 'running', 'waiting_user'].some(isFinalStatus)).toBe(false)
	})
})

describe('paths', () => {
	const locations = fixture('GET', '/locations')
	test('local endpoints map to their mount point', () => {
		expect(localPath(photos.sources[0], locations)).toBe('/DATA/tower/photos')
		expect(localPath(photos.dest, locations)).toBe('/media/sdc1/NivaroOS Backups/Photos')
		expect(localPath(immich.dest, locations)).toBe('')
		expect(localPath({ kind: 'usb', ref_id: 'gone' }, locations)).toBe('')
	})
	test('join and parent', () => {
		expect(joinPath('', 'a/', '/b')).toBe('a/b')
		expect(joinPath()).toBe('')
		expect(parentPath('a/b/c')).toBe('a/b')
		expect(parentPath('a')).toBe('')
	})
})

describe('messages', () => {
	const fmt = createFormatter({ locale: 'en_us', timeZone: 'Europe/Berlin', t })

	test('summary args: nested keys, sizes and numbers', () => {
		expect(renderMessage(t, photos.last_run.summary, fmt)).toBe('4,120 added, 12 changed, 318 moved to recycle · 11.2 GB')
		expect(renderMessage(t, immich.last_run.summary, fmt)).toBe(`Failed: ${en['backup.err.cloud_auth.title']}`)
		expect(renderMessage(t, null, fmt)).toBe('')
	})

	test('steps with list args', () => {
		expect(renderMessage(t, { key: 'backup.run.step.stop_apps', args: { apps: ['Immich', 'Blinko'] } }, fmt)).toBe('Stop Immich and Blinko')
	})

	test('error codes from summaries and their explanations', () => {
		expect(errorCodeFromMessage(immich.last_run.summary)).toBe('cloud_auth')
		expect(errorCodeFromMessage(photos.last_run.summary)).toBe('')
		const e = explainError(t, 'cloud_auth')
		expect(e.title).toBe(en['backup.err.cloud_auth.title'])
		expect(e.actions).toEqual(ERROR_CODES.cloud_auth.actions)
		const u = explainError(t, 'service_unavailable')
		expect(u.title).toBe(en['backup.app.unavailable.title'])
		expect(errorText(t, { code: 'no_such' })).toBe(en['backup.err.internal.title'])
	})
})

describe('format', () => {
	const fmt = createFormatter({ locale: 'en_us', timeZone: 'Europe/Berlin', hour12: false, t })
	test('sizes, speeds and durations', () => {
		expect(fmt.bytes(0)).toBe('0 B')
		expect(fmt.bytes(999)).toBe('999 B')
		expect(fmt.bytes(11900000000)).toBe('11.9 GB')
		expect(fmt.bytes(128035676160)).toBe('128 GB')
		expect(fmt.speed(12300000)).toBe('12.3 MB/s')
		expect(fmt.duration(40)).toBe('40 sec')
		expect(fmt.duration(360)).toBe('6 min')
		expect(fmt.duration(3900)).toBe('1 hr 5 min')
	})
	test('server-time day labels', () => {
		const now = new Date('2026-09-24T10:00:00+02:00')
		expect(fmt.when('2026-09-24T03:12:40+02:00', now)).toBe('Today 03:12')
		expect(fmt.when('2026-09-25T03:00:00+02:00', now)).toBe('Tomorrow 03:00')
		expect(fmt.when('2026-09-23T03:00:00+02:00', now)).toBe('Yesterday 03:00')
		expect(fmt.when('2026-09-28T03:00:00+02:00', now)).toBe('Mon, Sep 28, 03:00')
		expect(fmt.relative('2026-09-24T03:00:00+02:00', now)).toBe('7 hours ago')
		expect(createFormatter({ timeZone: 'Not/AZone' }).timeZone).toBe(undefined)
		// The user's 12-hour clock setting wins over the locale.
		expect(createFormatter({ locale: 'en_gb', timeZone: 'Europe/Berlin', hour12: true, t }).when('2026-09-24T15:12:40+02:00', now)).toMatch(/^Today 0?3:12\s?pm$/i)
	})
})

describe('paused run decision', () => {
	test('a run that could delete has nothing preselected: no payload until a choice', () => {
		expect(decisionPayload(true, '')).toBeNull()
		expect(decisionPayload(true, 'bogus')).toBeNull()
		expect(decisionPayload(true, 'copy_once')).toEqual({ proceed: true, mode: 'copy_once' })
		expect(decisionPayload(true, 'as_shown')).toEqual({ proceed: true, mode: 'as_shown' })
	})
	test('without a copy-once alternative, Continue runs it as shown', () => {
		expect(decisionPayload(false, '')).toEqual({ proceed: true, mode: 'as_shown' })
	})
})
