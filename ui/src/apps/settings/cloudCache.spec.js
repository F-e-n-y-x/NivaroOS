import { describe, test, expect } from 'vitest'
import { GB, formatBytes, ageToParts, settingsToForm, formToSettings, sameSettings, validateForm, accountLine, clearable, totals, stuckList, clearResultText } from './cloudCache'

const defaults = { mode: 'full', max_size: 20 * GB, max_age: 3600, dir: '/var/cache/nivaroos/rclone' }

describe('cloud cache settings form', () => {
	test('formatBytes matches the service', () => {
		expect(formatBytes(512)).toBe('512 B')
		expect(formatBytes(1536)).toBe('1.5 KB')
		expect(formatBytes(20 * GB)).toBe('20 GB')
		expect(formatBytes(5900 * 1024 * 1024)).toBe('5.8 GB')
		expect(formatBytes(undefined)).toBe('0 B')
	})

	test('ageToParts picks the biggest whole unit', () => {
		expect(ageToParts(3600)).toEqual({ n: 1, unit: 3600 })
		expect(ageToParts(5400)).toEqual({ n: 90, unit: 60 })
		expect(ageToParts(2 * 86400)).toEqual({ n: 2, unit: 86400 })
		expect(ageToParts(30)).toEqual({ n: 1, unit: 60 })
	})

	test('settings round-trip through the form', () => {
		const f = settingsToForm(defaults)
		expect(f).toEqual({ mode: 'full', sizeGB: 20, ageN: 1, ageUnit: 3600, dir: '/var/cache/nivaroos/rclone' })
		expect(formToSettings(f)).toEqual(defaults)
		expect(sameSettings(formToSettings(f), defaults)).toBe(true)
		expect(sameSettings(formToSettings({ ...f, sizeGB: 10 }), defaults)).toBe(false)
		expect(formToSettings({ ...f, dir: ' /DATA/c ' }).dir).toBe('/DATA/c')
	})

	test('validateForm refuses what the service refuses', () => {
		const f = settingsToForm(defaults)
		expect(validateForm(f)).toBe('')
		expect(validateForm({ ...f, mode: 'most' })).toMatch(/mode/)
		expect(validateForm({ ...f, sizeGB: 0 })).toMatch(/above 0/)
		expect(validateForm({ ...f, sizeGB: 'abc' })).toMatch(/above 0/)
		expect(validateForm({ ...f, ageN: 30, ageUnit: 1 })).toMatch(/at least 1 minute/)
		expect(validateForm({ ...f, ageN: 400, ageUnit: 86400 })).toMatch(/365 days/)
		expect(validateForm({ ...f, dir: 'relative' })).toMatch(/folder/)
		expect(validateForm({ ...f, dir: '/' })).toMatch(/whole disk/)
		// size vs free space, only where the cache is now
		const ctx = { free: 10 * GB, currentDir: f.dir, used: 2 * GB }
		expect(validateForm(f, ctx)).toMatch(/less than the free space there \(12 GB\)/)
		expect(validateForm({ ...f, sizeGB: 11 }, ctx)).toBe('')
		expect(validateForm({ ...f, dir: '/DATA/Disk1/c' }, ctx)).toBe('')
	})
})

describe('cloud cache accounts', () => {
	const tb = {
		name: 'terabox_x', label: 'TeraBox', mounted: true, used_bytes: 6 * GB, pending_bytes: 5 * GB, pending_files: 2,
		uploading: 0, waiting: 1, wait_bytes: 1024, stuck: [{ remote: 'terabox_x', name: 'iso/win.iso', size: 5 * GB, reason: 'too big' }]
	}
	const gd = { name: 'gd', label: '', mounted: false, used_bytes: 0, pending_bytes: 0, uploading: 0, waiting: 0, wait_bytes: 0, stuck: [] }

	test('accountLine says what is cached, waiting and stuck', () => {
		expect(accountLine(tb)).toBe("6 GB cached · 1 file waiting to upload (1 KB) · 1 can't finish")
		expect(accountLine(gd)).toBe('0 B cached · nothing waiting to upload · not connected')
		expect(accountLine({ ...gd, mounted: true, uploading: 2 })).toBe('0 B cached · uploading 2 files now')
	})

	test('clear only counts data that is already uploaded', () => {
		expect(clearable(tb)).toBe(GB)
		expect(clearable({ used_bytes: 1, pending_bytes: 5 })).toBe(0)
	})

	test('totals and the stuck list', () => {
		expect(totals([tb, gd])).toEqual({ used: 6 * GB, waiting: 1, waitBytes: 1024, stuck: 1 })
		const l = stuckList([tb, gd])
		expect(l).toHaveLength(1)
		expect(l[0]).toMatchObject({ label: 'TeraBox', fileName: 'win.iso', key: 'terabox_x:iso/win.iso' })
		expect(stuckList([{ ...tb, label: '' }])[0].label).toBe('terabox_x')
	})

	test('clearResultText', () => {
		expect(clearResultText({ removed_files: 3, freed_bytes: 2 * GB, kept_pending: 1 })).toBe('Freed 2 GB. Kept 1 file still waiting to upload or open.')
		expect(clearResultText({ removed_files: 0, freed_bytes: 0, kept_pending: 0 })).toBe('Nothing to clear.')
	})
})
