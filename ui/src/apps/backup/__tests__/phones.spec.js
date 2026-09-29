import { describe, test, expect } from 'vitest'
import fs from 'fs'
import path from 'path'
import {
	PHONE_CATEGORIES,
	deviceTone,
	deviceStateKey,
	crumbs,
	parentPath,
	sortEntries,
	locationText,
	moveProgress,
	categoryCount,
	phoneSchedule,
	settingsErrors,
	needsMode,
	snapshotLabel
} from '../phones'

// Phones section logic (phones.js) against the contract fixtures.
const doc = JSON.parse(fs.readFileSync(path.resolve(__dirname, '../../../../../docs/specs/backup-api.json'), 'utf8'))
const fixture = id => doc.endpoints.find(e => e.id === id)
const t = (k, args) => (args ? `${k}|${JSON.stringify(args)}` : k)
const fmt = { number: n => String(n), dateTime: v => `dt(${v})`, bytes: n => `${n} B` }

describe('phones.js', () => {
	const detail = fixture('device_get').response

	test('categories match the server, in its order', () => {
		expect(fixture('device_config').response.categories.map(c => c.category)).toEqual(PHONE_CATEGORIES)
		expect(detail.categories.map(c => c.category)).toEqual(PHONE_CATEGORIES)
	})

	test('status: revoked and a broken location first, then failures, offline, age', () => {
		const now = new Date('2026-09-30T12:00:00Z').getTime()
		const d = detail.device
		expect(deviceTone(d, { destination: detail.destination, now })).toBe('ok')
		expect(deviceStateKey(d, { destination: detail.destination, now })).toBe('backup.phones.state.ok')
		expect(deviceTone({ ...d, revoked_at: '2026-09-30T00:00:00Z' }, { now })).toBe('danger')
		expect(deviceStateKey({ ...d, revoked_at: 'x' })).toBe('backup.phones.state.revoked')
		expect(deviceTone(d, { destination: { online: false, error_code: 'dest_offline' }, now })).toBe('warn')
		expect(deviceStateKey(d, { destination: { online: false, error_code: 'dest_offline' } })).toBe('backup.phones.state.dest_offline')
		expect(deviceTone(d, { destination: { online: true, error_code: 'dest_marker_mismatch' }, now })).toBe('danger')
		expect(deviceTone({ ...d, last_status: 'failed' }, { now })).toBe('danger')
		expect(deviceTone({ ...d, last_status: 'partial' }, { now })).toBe('warn')
		expect(deviceTone({ ...d, last_backup_at: null }, { now })).toBe('muted')
		expect(deviceTone(d, { now: now + 8 * 86400000 })).toBe('warn')
		expect(deviceStateKey(d, { now: now + 8 * 86400000 })).toBe('backup.phones.state.stale')
		expect(deviceTone(d, { now: now + 8 * 86400000, staleDays: 0 })).toBe('ok')
		expect(deviceStateKey({ ...d, moving: true })).toBe('backup.phones.state.moving')
	})

	test('breadcrumbs and parents', () => {
		expect(crumbs('DCIM/Camera/2026')).toEqual([
			{ name: 'DCIM', path: 'DCIM' },
			{ name: 'Camera', path: 'DCIM/Camera' },
			{ name: '2026', path: 'DCIM/Camera/2026' }
		])
		expect(crumbs('')).toEqual([])
		expect(parentPath('DCIM/Camera')).toBe('DCIM')
		expect(parentPath('DCIM')).toBe('')
	})

	test('folders first, then natural name order', () => {
		const got = sortEntries([{ name: 'b10.jpg' }, { name: 'Z', dir: true }, { name: 'b2.jpg' }, { name: 'a', dir: true }]).map(e => e.name)
		expect(got).toEqual(['a', 'Z', 'b2.jpg', 'b10.jpg'])
		expect(sortEntries(fixture('device_browse').response.entries)[0].dir).toBe(true)
	})

	test('location text: default or the picked place', () => {
		expect(locationText(t, detail.destination)).toBe('backup.phones.location_default|{"path":"/DATA/Backup/Pixel 8"}')
		const moving = fixture('device_destination').response
		expect(locationText(t, { location: moving.move.target })).toBe('Backup Stick › Phones')
		expect(moveProgress(moving.move)).toBeCloseTo(3100000000 / 48318382080)
		expect(moveProgress(null)).toBe(null)
		expect(moveProgress({ state: 'moving', total_bytes: 0, total_files: 0 })).toBe(null)
	})

	test('what a category keeps', () => {
		const media = detail.categories.find(c => c.category === 'media')
		expect(categoryCount(t, fmt, media)).toBe('backup.phones.count_files|{"count":"18275"} · backup.phones.count_deleted|{"count":"12"}')
		expect(categoryCount(t, fmt, detail.categories.find(c => c.category === 'contacts'))).toBe('backup.phones.count_items|{"count":"412"}')
		expect(categoryCount(t, fmt, { category: 'sms', exports: 3, items: 40 })).toBe('backup.phones.count_items_all|{"count":"40"}')
		expect(categoryCount(t, fmt, { category: 'sms', exports: 0 })).toBe('backup.phones.count_none')
	})

	test('the phone schedule is shown as reported', () => {
		expect(phoneSchedule(t, detail.phone, 'contacts')).toBe('daily 03:00 · backup.phones.condition.wifi · backup.phones.condition.charging')
		expect(phoneSchedule(t, detail.phone, 'sms')).toBe('')
		expect(phoneSchedule(t, { categories: { sms: { enabled: false } } }, 'sms')).toBe('backup.phones.schedule_off')
		expect(phoneSchedule(t, { categories: { sms: { enabled: true, conditions: ['on_moon'] } } }, 'sms')).toBe('backup.phones.schedule_on · on_moon')
		expect(phoneSchedule(t, null, 'media')).toBe('')
	})

	test('retention form limits', () => {
		expect(settingsErrors(detail.settings)).toEqual({})
		expect(settingsErrors({ ...detail.settings, keep_last: 0, stale_days: 1.5 })).toEqual({ keep_last: 'out_of_range', stale_days: 'out_of_range' })
		expect(Object.keys(settingsErrors({}))).toHaveLength(4)
	})

	test('a phone with backups asks move or fresh', () => {
		expect(needsMode(detail)).toBe(true)
		expect(needsMode({ categories: detail.categories.map(c => ({ ...c, files: 0, deleted_files: 0, exports: 0 })), snapshots: 0 })).toBe(false)
		expect(needsMode(null)).toBe(false)
	})

	test('snapshot labels', () => {
		expect(snapshotLabel(t, fmt, 'latest')).toBe('backup.phones.snapshot_latest')
		const s = fixture('device_snapshots').response[0]
		expect(snapshotLabel(t, fmt, s)).toBe(`dt(${s.taken_at})`)
		expect(snapshotLabel(t, fmt, { ...s, status: 'partial' })).toBe(`backup.phones.snapshot_partial|{"when":"dt(${s.taken_at})"}`)
	})
})
