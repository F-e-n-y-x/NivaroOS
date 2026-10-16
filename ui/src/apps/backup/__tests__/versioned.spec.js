import { describe, test, expect } from 'vitest'
import { newDraft, draftFromJob, draftToJob, validateDraft } from '../wizard/draft'
import { safeTypes } from '../presets'
import { jobSummary } from '../summaries'

// Versioned (restic) jobs in the wizard: which sources allow them, the
// keep rules and encryption they send, and that a stored one round-trips.
const tower = { kind: 'volume', ref_id: 'tower-uuid', sub_path: 'photos', label: 'tower' }
const docs = { kind: 'volume', ref_id: 'tower-uuid', sub_path: 'docs', label: 'tower' }
const nas = { kind: 'smb', ref_id: '3', sub_path: 'share/x', label: 'NAS' }
const gdrive = { kind: 'cloud', ref_id: 'gdrive', sub_path: 'NivaroOS Backups/Photos', label: 'Google Drive' }
const immich = { kind: 'volume', ref_id: 'tower-uuid', sub_path: 'DATA/AppData/immich', label: 'immich', preset: 'appdata:immich' }

function draft(over = {}) {
	return Object.assign(newDraft({ sourceEndpoint: tower, destEndpoint: gdrive }), { type: 'backup' }, over)
}

describe('versioned backups', () => {
	test('only local folders can be versioned; app data may go to a cloud that way', () => {
		expect(safeTypes([tower], 'cloud')).toContain('backup')
		expect(safeTypes([nas], 'volume')).not.toContain('backup')
		expect(safeTypes([immich], 'cloud')).toEqual(['archive', 'backup'])
	})

	test('a new versioned job keeps 7 daily, 4 weekly, 12 monthly and is encrypted by restic', () => {
		const job = draftToJob(draft({ sources: [tower, docs], encryptMode: 'restic' }))
		expect(job.type).toBe('backup')
		expect(job.sources).toHaveLength(2)
		expect(job.retention).toEqual({ keep_daily: 7, keep_weekly: 4, keep_monthly: 12 })
		expect(job.dest.encryption).toEqual({ mode: 'restic', recovery_key: false })
		expect(validateDraft(draft({ sources: [tower, docs], encryptMode: 'restic' }))).toEqual({})
	})

	test('every keep rule off keeps everything rather than the defaults', () => {
		expect(draftToJob(draft({ keepDaily: 0, keepWeekly: 0, keepMonthly: 0 })).retention).toEqual({ keep_last: 1000 })
	})

	test('rclone crypt, include rules and {a,b} patterns are refused', () => {
		expect(validateDraft(draft({ encryptMode: 'folder' }))['dest.encryption']).toBe('invalid')
		expect(validateDraft(draft({ type: 'copy', encryptMode: 'restic' }))['dest.encryption']).toBe('invalid')
		expect(validateDraft(draft({ include: ['*.jpg'] }))['filters.include']).toBe('invalid')
		expect(validateDraft(draft({ exclude: ['*.{tmp,bak}'] }))['filters.exclude']).toBe('invalid_filter')
		expect(validateDraft(draft({ keepDaily: -1 }))['retention.keep_daily']).toBe('out_of_range')
	})

	test('a stored versioned job round-trips unchanged', () => {
		const job = draftToJob(draft({ keepDaily: 3, keepWeekly: 0, keepMonthly: 6, snapKeepLast: 2, encryptMode: 'restic' }))
		expect(job.retention).toEqual({ keep_daily: 3, keep_monthly: 6, keep_last: 2 })
		expect(draftToJob(draftFromJob(job))).toEqual(job)
	})

	test('its summary says what it keeps', () => {
		const keys = jobSummary(draftToJob(draft({ encryptMode: 'restic' }))).map(l => l.key)
		expect(keys).toEqual(expect.arrayContaining(['backup.summary.backup', 'backup.summary.encrypted_restic', 'backup.keep.snapshots']))
	})
})
