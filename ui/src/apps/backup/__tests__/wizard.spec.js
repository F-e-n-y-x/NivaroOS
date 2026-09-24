import { describe, test, expect } from 'vitest'
import fs from 'fs'
import path from 'path'
import {
	newDraft, draftFromJob, draftToJob, validateDraft, errorsForStep, firstStepWithErrors, mapServerFieldErrors,
	blockingCheckErrors, defaultName, defaultDestSubPath, staleHours, syncHookChoices
} from '../wizard/draft'
import { PRESETS, visiblePresets, presetById, safeTypes, autoTypeForDest } from '../presets'
import { jobSummary } from '../summaries'

// Wizard model and client-side validation (spec §12.4, §16.3
// validate.spec.js / presets.spec.js): the rules match the server's
// validate.go, and a stored job survives draftFromJob -> draftToJob
// unchanged, so opening and saving a job never rewrites it.
const doc = JSON.parse(fs.readFileSync(path.resolve(__dirname, '../../../../../docs/specs/backup-api.json'), 'utf8'))
const endpoint = id => doc.endpoints.find(e => e.id === id)
const LOCATIONS = endpoint('locations').response
const STORED_JOBS = endpoint('jobs_list').response
const loc = ref => LOCATIONS.find(l => l.ref_id === ref)

const READ_ONLY = ['created_at', 'updated_at', 'health', 'last_run', 'active_run', 'next_run', 'dest_online', 'stats']
function asJob(stored) {
	const out = { ...stored }
	for (const k of READ_ONLY) delete out[k]
	return out
}

const tower = { kind: 'volume', ref_id: loc('6c1e2f0a-9b1d-4c1e-8f3a-2b7d9e0c4a11').ref_id, sub_path: 'photos', label: 'tower' }
const usb = { kind: 'usb', ref_id: '3A4F-1C22', match: { serial: '4C530001230914117281', size_bytes: 128035676160 }, sub_path: 'NivaroOS Backups/Photos', label: 'Sandisk 128G' }
const gdrive = { kind: 'cloud', ref_id: 'gdrive', sub_path: 'NivaroOS Backups/AppData', label: 'Google Drive' }
const immich = { kind: 'volume', ref_id: '0b7c5e8e-1111-4a2b-9c3d-5e6f7a8b9c0d', sub_path: 'DATA/AppData/immich', label: 'immich', preset: 'appdata:immich' }

function validDraft(over = {}) {
	return Object.assign(newDraft({ sourceEndpoint: tower, destEndpoint: usb }), over)
}

describe('stored jobs round-trip through the wizard unchanged', () => {
	test.each(STORED_JOBS.map(j => [j.name, j]))('%s', (_, stored) => {
		const job = asJob(stored)
		expect(draftToJob(draftFromJob(job))).toEqual(job)
	})

	test('the PUT fixture request is what saving an unchanged job sends', () => {
		const put = endpoint('jobs_update').request
		const sent = draftToJob(draftFromJob(put))
		expect(sent).toEqual(asJob(put))
		expect(sent.revision).toBe(put.revision)
	})
})

describe('new drafts have safe defaults', () => {
	test('scratch: copy (never deletes), daily 03:00, nightly retries, failures notified', () => {
		const job = draftToJob(validDraft())
		expect(job.type).toBe('copy')
		expect(job.triggers).toEqual([{ kind: 'schedule', cron: '0 3 * * *', catch_up: true }])
		expect(job.conditions).toEqual({ dest_available: true, when_unmet: 'wait', wait_max_min: 360 })
		expect(job.filters.exclude_presets).toEqual(['caches', 'trash', 'temp'])
		expect(job.options).toMatchObject({ preview_first: false, low_priority: true, max_duration_sec: 86400 })
		expect(job.retry).toEqual({ max: 3, backoff_sec: [60, 600, 3600] })
		expect(job.notify).toEqual({ on_success: false, on_failure: true, stale_after_hours: 48 })
		expect(job.retention).toEqual({})
		expect(job.id).toBeUndefined()
	})

	test('settings defaults are used', () => {
		const d = newDraft({ settings: { catch_up_default: false, default_delete_pct: 5, default_change_pct: 20, default_versions_days: 90 } })
		expect(d).toMatchObject({ catchUp: false, deletePct: 5, changePct: 20, versionsDays: 90 })
	})

	test('a mirror previews its first run and keeps 30 days of versions', () => {
		const job = draftToJob(newDraft({ preset: presetById('photos_usb'), locations: LOCATIONS, destEndpoint: usb }))
		expect(job.type).toBe('mirror')
		expect(job.options.preview_first).toBe(true)
		expect(job.retention).toEqual({ versions_days: 30 })
		expect(job.triggers).toEqual([{ kind: 'volume_mounted', min_gap_hours: 24, catch_up: true }])
		expect(job.conditions.when_unmet).toBe('skip')
		expect(job.notify.stale_after_hours).toBe(336)
	})

	test('manual-only jobs have no triggers and no staleness warning', () => {
		const job = draftToJob(validDraft({ scheduleOn: false }))
		expect(job.triggers).toEqual([])
		expect(job.notify.stale_after_hours).toBe(0)
	})

	test('stale hours are twice the interval, at least 48', () => {
		expect(staleHours(validDraft({ cron: '0 4 * * 0' }))).toBe(336)
		expect(staleHours(validDraft({ cron: '0 */6 * * *' }))).toBe(48)
	})

	test('app data sources stop and restart their app by default', () => {
		const d = validDraft({ sources: [immich] })
		syncHookChoices(d)
		expect(draftToJob(d).hooks).toEqual([
			{ phase: 'pre', action: 'stop_apps', apps: ['immich'], app_mode: 'together', timeout_sec: 300, fail_policy: 'abort' },
			{ phase: 'post', action: 'start_apps', apps: ['immich'], app_mode: 'together', timeout_sec: 300, fail_policy: 'continue' }
		])
		d.stopApps.immich = false
		expect(draftToJob(d).hooks).toEqual([])
	})

	test('VM sources shut the VM down and start it again', () => {
		const d = validDraft({ sources: [{ ...immich, sub_path: 'DATA/VMs/win11', preset: 'vm:win11', label: 'win11' }] })
		syncHookChoices(d)
		expect(draftToJob(d).hooks.map(h => [h.phase, h.action, h.vm, h.timeout_sec])).toEqual([['pre', 'shutdown_vm', 'win11', 600], ['post', 'start_vm', 'win11', 600]])
	})

	test('name and folder defaults', () => {
		const d = validDraft()
		expect(defaultName(d)).toBe('photos → Sandisk 128G')
		expect(draftToJob(d).name).toBe('photos → Sandisk 128G')
		expect(defaultDestSubPath('Photos / 2024')).toBe('NivaroOS Backups/Photos - 2024')
		expect(defaultDestSubPath('..')).toBe('NivaroOS Backups/Backup')
	})
})

describe('validation matches the server rules', () => {
	test('a complete draft is valid', () => {
		expect(validateDraft(validDraft())).toEqual({})
	})

	test('source count per type', () => {
		expect(validateDraft(validDraft({ sources: [] })).sources).toBe('required')
		expect(validateDraft(validDraft({ sources: [tower, immich] })).sources).toBe('too_many')
		expect(validateDraft(validDraft({ type: 'archive', sources: [tower, immich] })).sources).toBeUndefined()
		expect(validateDraft(validDraft({ type: 'archive', sources: Array(17).fill(tower) })).sources).toBe('too_many')
	})

	test('sub-paths may not climb or be absolute', () => {
		expect(validateDraft(validDraft({ dest: { ...usb, sub_path: '../etc' } }))['dest.sub_path']).toBe('path_not_allowed')
		expect(validateDraft(validDraft({ dest: { ...usb, sub_path: '/abs' } }))['dest.sub_path']).toBe('path_not_allowed')
		expect(validateDraft(validDraft({ sources: [{ ...tower, sub_path: 'a/../b' }] })).sources).toBe('path_not_allowed')
	})

	test('destination inside the source, or the source inside the destination', () => {
		expect(validateDraft(validDraft({ dest: { ...tower, sub_path: 'photos/backup' } })).dest).toBe('dest_inside_source')
		expect(validateDraft(validDraft({ dest: { ...tower, sub_path: '' } })).dest).toBe('dest_inside_source')
		expect(validateDraft(validDraft({ dest: { ...tower, sub_path: 'photos-backup' } })).dest).toBeUndefined()
	})

	test('app data to a cloud needs Archive', () => {
		const d = validDraft({ sources: [immich], dest: gdrive, type: 'mirror' })
		expect(validateDraft(d)['dest.type']).toBe('appdata_cloud_needs_archive')
		expect(validateDraft({ ...d, type: 'archive' })['dest.type']).toBeUndefined()
		expect(validateDraft({ ...d, dest: usb })['dest.type']).toBeUndefined()
	})

	test('schedule, plug-in and window', () => {
		expect(validateDraft(validDraft({ cron: '0 25 * * *' })).cron).toBe('invalid_cron')
		expect(validateDraft(validDraft({ cron: '0 25 * * *', scheduleOn: false })).cron).toBeUndefined()
		expect(validateDraft(validDraft({ plugOn: true, plugGapHours: 9999 }))['plug.min_gap_hours']).toBe('out_of_range')
		expect(validateDraft(validDraft({ plugOn: true, dest: gdrive, sources: [{ kind: 'cloud', ref_id: 'x', sub_path: '', label: 'x' }] }))['plug.volume']).toBe('not_resolvable')
		expect(validateDraft(validDraft({ windowOn: true, windowStart: '25:00' }))['window.start']).toBe('invalid_time')
		expect(validateDraft(validDraft({ windowOn: true, windowStart: '22:00', windowEnd: '22:00' }))['window.end']).toBe('invalid')
	})

	test('guards and retention ranges', () => {
		expect(validateDraft(validDraft({ deletePct: 0 }))['guards.delete_pct']).toBe('out_of_range')
		expect(validateDraft(validDraft({ changePct: 101 }))['guards.change_pct']).toBe('out_of_range')
		expect(validateDraft(validDraft({ type: 'mirror', versionsDays: -1 }))['retention.versions_days']).toBe('out_of_range')
		expect(validateDraft(validDraft({ type: 'archive', keepLast: 0 }))['retention.keep_last']).toBe('out_of_range')
		expect(validateDraft(validDraft({ type: 'copy', keepLast: 0 }))['retention.keep_last']).toBeUndefined()
	})

	test('a mirror always requires the destination to be there', () => {
		expect(draftToJob(validDraft({ type: 'mirror', destAvailable: false })).conditions.dest_available).toBe(true)
		expect(draftToJob(validDraft({ type: 'copy', destAvailable: false })).conditions.dest_available).toBe(false)
	})

	test('errors are grouped by step, Review sends you to the first', () => {
		const errors = validateDraft(validDraft({ sources: [], cron: 'x', deletePct: 0 }))
		expect(Object.keys(errorsForStep(errors, 'what'))).toEqual(['sources'])
		expect(Object.keys(errorsForStep(errors, 'when'))).toEqual(['cron'])
		expect(Object.keys(errorsForStep(errors, 'keep'))).toEqual(['guards.delete_pct'])
		expect(firstStepWithErrors(errors)).toBe('what')
		expect(firstStepWithErrors({})).toBeNull()
	})
})

describe('server errors land on the wizard fields', () => {
	test('field_errors paths', () => {
		const job = draftToJob(validDraft({ plugOn: true, plugFirst: true }))
		expect(mapServerFieldErrors({ 'dest.sub_path': 'path_not_allowed', 'triggers[1].cron': 'invalid_cron', 'triggers[0].min_gap_hours': 'out_of_range', 'filters.exclude[2]': 'invalid_filter', 'guards.delete_pct': 'out_of_range' }, job)).toEqual({
			'dest.sub_path': 'path_not_allowed', cron: 'invalid_cron', 'plug.min_gap_hours': 'out_of_range', 'filters.exclude': 'invalid_filter', 'guards.delete_pct': 'out_of_range'
		})
	})

	test('blocking /validate checks', () => {
		const res = { checks: [{ id: 'free_space', status: 'fail', code: 'no_space' }, { id: 'not_inside', status: 'fail', code: 'dest_inside_source' }, { id: 'type_for_dest', status: 'fail' }] }
		expect(blockingCheckErrors(res)).toEqual({ dest: 'dest_inside_source', 'dest.type': 'appdata_cloud_needs_archive' })
		expect(blockingCheckErrors(endpoint('validate').response)).toEqual({})
	})
})

describe('presets', () => {
	test('hidden when they cannot apply', () => {
		const ids = visiblePresets(LOCATIONS).map(p => p.id)
		expect(ids).toContain('apps') // appdata:immich exists
		expect(ids).not.toContain('vms') // no vm: presets
		expect(ids).not.toContain('downloads') // no data:Downloads
		expect(ids).toContain('scratch')
		expect(visiblePresets([]).map(p => p.id)).toEqual(['photos_usb', 'documents_cloud', 'scratch'])
	})

	test('pre-fill the source from the folder presets', () => {
		const d = newDraft({ preset: presetById('documents_cloud'), locations: LOCATIONS })
		expect(d.sources).toEqual([{ kind: 'volume', ref_id: '0b7c5e8e-1111-4a2b-9c3d-5e6f7a8b9c0d', sub_path: 'DATA/Documents', label: 'Documents', preset: 'data:Documents' }])
		expect(d.type).toBe('copy')
		expect(d.cron).toBe('0 2 * * *')
	})

	test('every preset builds a job with valid triggers', () => {
		for (const p of PRESETS) {
			const d = newDraft({ preset: p, locations: LOCATIONS, sourceEndpoint: tower, destEndpoint: usb })
			const e = validateDraft(d)
			expect(e.cron).toBeUndefined()
			expect(e['plug.volume']).toBeUndefined()
		}
	})

	test('safe types per destination', () => {
		expect(safeTypes([immich], 'cloud')).toEqual(['archive'])
		expect(safeTypes([immich], 'usb')).toEqual(['mirror', 'copy', 'archive'])
		expect(safeTypes([tower], 'cloud')).toEqual(['mirror', 'copy', 'archive'])
		expect(autoTypeForDest(presetById('apps'), 'cloud')).toBe('archive')
		expect(autoTypeForDest(presetById('apps'), 'volume')).toBe('mirror')
		expect(autoTypeForDest(presetById('scratch'), 'cloud')).toBeNull()
	})
})

describe('summaries', () => {
	test('same keys for every job shape', () => {
		const keys = jobSummary(asJob(STORED_JOBS[1])).map(m => m.key)
		expect(keys).toEqual([
			'backup.summary.archive', 'backup.summary.when_schedule', 'backup.summary.catch_up', 'backup.summary.window',
			'backup.summary.unmet_wait', 'backup.keep.last_archives', 'backup.summary.stop_apps', 'backup.summary.skips',
			'backup.summary.verify', 'backup.summary.retry'
		])
		const mirror = jobSummary(asJob(STORED_JOBS[0]))
		expect(mirror[0]).toEqual({ key: 'backup.summary.mirror', args: { source: 'tower › photos', dest: 'Sandisk 128G › NivaroOS Backups/Photos' } })
		expect(mirror.find(m => m.key === 'backup.summary.when_plug_gap').args).toEqual({ drive: 'Sandisk 128G', hours: 24 })
		expect(jobSummary(draftToJob(validDraft({ scheduleOn: false }))).map(m => m.key)).toContain('backup.summary.when_manual')
	})

	test('every summary key and its placeholders exist in en_US.json', () => {
		const en = JSON.parse(fs.readFileSync(path.resolve(__dirname, '../../../assets/lang/en_US.json'), 'utf8'))
		const jobs = [...STORED_JOBS.map(asJob), draftToJob(validDraft({ scheduleOn: false })), draftToJob(validDraft({ type: 'copy', plugOn: true, plugGapHours: 0, appMode: 'one_at_a_time', sources: [immich] }))]
		for (const job of jobs) {
			for (const m of jobSummary(job)) {
				expect(en[m.key], m.key).toBeTruthy()
				for (const ph of en[m.key].match(/\{(\w+)\}/g) || []) {
					const name = ph.slice(1, -1)
					expect(Object.keys(m.args).concat(m.args.schedule_cron !== undefined ? ['schedule'] : []), `${m.key} ${ph}`).toContain(name)
				}
			}
		}
	})
})
