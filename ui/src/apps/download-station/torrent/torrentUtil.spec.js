import { describe, test, expect } from 'vitest'
import { filterTorrents, countByFilter, parseSources, magnetName, isTorrentFile, formatRatio, toKiB, fromKiB, supported, badgeClass, percent, isPaused } from './torrentUtil'

const list = [
	{ name: 'Debian netinst', state: 'downloading', progress: 0.4, category: 'Linux' },
	{ name: 'Ubuntu', state: 'seeding', progress: 1 },
	{ name: 'Arch', state: 'paused', progress: 0.1 },
	{ name: 'Fedora', state: 'completed', progress: 1 },
	{ name: 'Magnet', state: 'metadata', progress: 0 }
]

describe('torrent list', () => {
	test('filters by state', () => {
		expect(filterTorrents(list, 'downloading', '').map(t => t.name)).toEqual(['Debian netinst', 'Magnet'])
		expect(filterTorrents(list, 'seeding', '').map(t => t.name)).toEqual(['Ubuntu'])
		expect(filterTorrents(list, 'paused', '').map(t => t.name)).toEqual(['Arch'])
		expect(filterTorrents(list, 'completed', '').map(t => t.name)).toEqual(['Ubuntu', 'Fedora'])
		expect(countByFilter(list)).toEqual({ all: 5, downloading: 2, seeding: 1, paused: 1, completed: 2 })
	})
	test('search matches name and category', () => {
		expect(filterTorrents(list, 'all', 'linux').map(t => t.name)).toEqual(['Debian netinst'])
		expect(filterTorrents(list, 'all', 'UBU').map(t => t.name)).toEqual(['Ubuntu'])
	})
	test('state helpers', () => {
		expect(badgeClass('seeding')).toBe('is-completed')
		expect(badgeClass('error')).toBe('is-failed')
		expect(badgeClass('queued')).toBe('is-paused')
		expect(badgeClass('stalled')).toBe('is-downloading')
		expect(percent({ progress: 0.12345 })).toBe(12.3)
		expect(percent({ progress: 1.2 })).toBe(100)
		expect(isPaused({ state: 'completed' })).toBe(true)
		expect(isPaused({ state: 'seeding' })).toBe(false)
		expect(formatRatio(0.5)).toBe('0.50')
		expect(formatRatio(123.4)).toBe('123')
		expect(formatRatio(-1)).toBe('—')
	})
})

describe('adding', () => {
	test('picks magnets and http links out of pasted text', () => {
		const text = 'magnet:?xt=urn:btih:abc&dn=x\n  https://example.org/a.torrent junk ftp://no\nmagnet:?xt=urn:btih:abc&dn=x'
		expect(parseSources(text)).toEqual(['magnet:?xt=urn:btih:abc&dn=x', 'https://example.org/a.torrent'])
		expect(parseSources('')).toEqual([])
	})
	test('magnet display name', () => {
		expect(magnetName('magnet:?xt=urn:btih:ABC&dn=Debian+13%20netinst')).toBe('Debian 13 netinst')
		expect(magnetName('magnet:?xt=urn:btih:ABC')).toBe('ABC')
	})
	test('.torrent files by name or type', () => {
		expect(isTorrentFile({ name: 'a.TORRENT' })).toBe(true)
		expect(isTorrentFile({ name: 'x', type: 'application/x-bittorrent' })).toBe(true)
		expect(isTorrentFile({ name: 'a.zip', type: 'application/zip' })).toBe(false)
	})
})

describe('settings', () => {
	test('KiB/s conversions', () => {
		expect(toKiB(1048576)).toBe(1024)
		expect(fromKiB('512')).toBe(524288)
		expect(fromKiB(-3)).toBe(0)
		expect(fromKiB('')).toBe(0)
	})
	test('engine capabilities', () => {
		expect(supported([], 'lsd')).toBe(true)
		expect(supported(['lsd', 'preallocate'], 'lsd')).toBe(false)
		expect(supported(['all'], 'speed')).toBe(false)
	})
})
