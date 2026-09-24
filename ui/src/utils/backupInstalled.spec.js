import { describe, test, expect, vi, afterEach } from 'vitest'
import { checkBackupInstalled, BACKUP_HEALTH_URL, isMovedToBackup, countsAsActive } from './backupInstalled'

function answer(res) {
	const fetch = vi.fn(() => (res instanceof Error ? Promise.reject(res) : Promise.resolve(res)))
	vi.stubGlobal('fetch', fetch)
	return fetch
}
const json = (status, body) => ({ ok: status >= 200 && status < 300, status, json: () => (body instanceof Error ? Promise.reject(body) : Promise.resolve(body)) })

afterEach(() => vi.unstubAllGlobals())

describe('checkBackupInstalled', () => {
	test('asks the gateway route, without credentials, and trusts installed:true', async () => {
		const fetch = answer(json(200, { installed: true, running: true, service: 'backup', version: '1.0.0' }))
		expect(await checkBackupInstalled(true)).toBe(true)
		expect(fetch).toHaveBeenCalledTimes(1)
		const [url, opts] = fetch.mock.calls[0]
		expect(url).toBe(BACKUP_HEALTH_URL)
		expect(url).toBe('/v1/backup/health')
		expect(opts.credentials).toBe('omit')
		expect(opts.cache).toBe('no-store')
	})

	test('any failure means not installed', async () => {
		answer(json(404, null))
		expect(await checkBackupInstalled(true)).toBe(false)
		answer(json(502, null))
		expect(await checkBackupInstalled(true)).toBe(false)
		answer(new TypeError('Failed to fetch'))
		expect(await checkBackupInstalled(true)).toBe(false)
		// The SPA fallback: 200 with HTML, which isn't JSON.
		answer(json(200, new SyntaxError('Unexpected token <')))
		expect(await checkBackupInstalled(true)).toBe(false)
		// Some other service's health answer.
		answer(json(200, { ok: true }))
		expect(await checkBackupInstalled(true)).toBe(false)
	})

	test('is cached until forced', async () => {
		const first = answer(json(200, { installed: true }))
		expect(await checkBackupInstalled(true)).toBe(true)
		const second = answer(json(404, null))
		expect(await checkBackupInstalled()).toBe(true)
		expect(second).not.toHaveBeenCalled()
		expect(await checkBackupInstalled(true)).toBe(false)
		expect(first).toHaveBeenCalledTimes(1)
	})
})

describe('tasks moved to Backup & Sync', () => {
	test('the marker alone decides, whatever the task still says', () => {
		expect(isMovedToBackup({ migrated_to: 'backup', enabled: true })).toBe(true)
		expect(isMovedToBackup({ migrated_to: '', enabled: true })).toBe(false)
		expect(isMovedToBackup({ enabled: true })).toBe(false)
		expect(isMovedToBackup(null)).toBe(false)
	})
	test('a moved task never counts as an active schedule', () => {
		expect(countsAsActive({ enabled: true })).toBe(true)
		expect(countsAsActive({ enabled: true, migrated_to: 'backup' })).toBe(false)
		expect(countsAsActive({ enabled: false })).toBe(false)
	})
})
