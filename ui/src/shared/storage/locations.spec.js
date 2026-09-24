import { describe, test, expect } from 'vitest'
import fs from 'fs'
import path from 'path'
import {
	groupLocations, locationStatus, spaceInfo, selectable, riskyDestination, cleanSubPath, joinSubPath, parentSubPath,
	isSameOrInside, validFolderName, endpointFromLocation, findLocation, pickerTarget
} from './locations'

// The shared pickers over GET /v1/backup/locations (spec §6.1, §12.4).
const doc = JSON.parse(fs.readFileSync(path.resolve(__dirname, '../../../../docs/specs/backup-api.json'), 'utf8'))
const LOCATIONS = doc.endpoints.find(e => e.id === 'locations').response
const byRef = ref => LOCATIONS.find(l => l.ref_id === ref)

describe('grouping', () => {
	test('every storage type lands in its group, in picker order', () => {
		const groups = groupLocations(LOCATIONS)
		expect(groups.map(g => g.id)).toEqual(['internal', 'usb', 'cloud'])
		expect(groups[0].items.map(l => l.kind)).toEqual(['volume', 'volume', 'merge'])
		const smb = [...LOCATIONS, { kind: 'smb', ref_id: '3', label: '\\\\192.168.1.20\\backup', online: true, free: 1, quirks: [], warnings: [] }]
		expect(groupLocations(smb).map(g => g.id)).toEqual(['internal', 'usb', 'network', 'cloud'])
	})

	test('search matches name, filesystem, provider and mount point', () => {
		expect(groupLocations(LOCATIONS, 'exfat').flatMap(g => g.items).map(l => l.label)).toEqual(['Sandisk 128G'])
		expect(groupLocations(LOCATIONS, 'terabox').flatMap(g => g.items).map(l => l.label)).toEqual(['TeraBox'])
		expect(groupLocations(LOCATIONS, '/DATA/tower').flatMap(g => g.items).map(l => l.label)).toEqual(['tower'])
		expect(groupLocations(LOCATIONS, 'nothing like it')).toEqual([])
	})
})

describe('status is text + icon, never colour alone', () => {
	test.each([
		['6c1e2f0a-9b1d-4c1e-8f3a-2b7d9e0c4a11', 'dest', 'ok', 'backup.loc.status.ready'],
		['0b7c5e8e-1111-4a2b-9c3d-5e6f7a8b9c0d', 'dest', 'warn', 'backup.loc.status.not_recommended'],
		['0b7c5e8e-1111-4a2b-9c3d-5e6f7a8b9c0d', 'source', 'ok', 'backup.loc.status.ready'],
		['3A4F-1C22', 'dest', 'ok', 'backup.loc.status.plugged_in'],
		['terabox_terabox_1789494942', 'dest', 'warn', 'backup.loc.status.limited'],
		['gdrive', 'dest', 'ok', 'backup.loc.status.connected']
	])('%s as %s', (ref, role, tone, key) => {
		const s = locationStatus(byRef(ref), role)
		expect(s).toMatchObject({ tone, key })
		expect(s.icon).toBeTruthy()
	})

	test('an unplugged, remembered USB drive', () => {
		const loc = { ...byRef('3A4F-1C22'), online: false, last_seen: '2026-09-21T18:02:11+02:00' }
		expect(locationStatus(loc)).toMatchObject({ tone: 'muted', key: 'backup.loc.status.not_connected_seen', args: { at: loc.last_seen } })
		expect(selectable(loc, 'dest')).toBe(true)
		expect(selectable(loc, 'source')).toBe(false)
	})

	test('every status and group key exists in en_US.json', () => {
		const en = JSON.parse(fs.readFileSync(path.resolve(__dirname, '../../assets/lang/en_US.json'), 'utf8'))
		const src = fs.readFileSync(path.resolve(__dirname, 'locations.js'), 'utf8')
		const keys = [...src.matchAll(/'(backup\.loc\.[a-z_.]+)'/g)].map(m => m[1])
		expect(keys.length).toBeGreaterThan(10)
		expect(keys.filter(k => !en[k])).toEqual([])
	})
})

test('free space; null when unknown', () => {
	expect(spaceInfo(byRef('3A4F-1C22'))).toEqual({ free: 101000000000, total: 128035676160, usedRatio: expect.closeTo(0.2111, 3) })
	expect(spaceInfo(byRef('terabox_terabox_1789494942'))).toBeNull()
})

test('risky destinations need an extra confirmation', () => {
	const tower = byRef('6c1e2f0a-9b1d-4c1e-8f3a-2b7d9e0c4a11')
	const system = byRef('0b7c5e8e-1111-4a2b-9c3d-5e6f7a8b9c0d')
	expect(riskyDestination(system, tower)).toBe('system_disk')
	expect(riskyDestination(tower, { ...tower, ref_id: 'other' })).toBe('same_disk')
	expect(riskyDestination(byRef('3A4F-1C22'), tower)).toBeNull()
})

describe('sub-paths follow the backend rules', () => {
	test('cleaning', () => {
		expect(cleanSubPath('photos//2024/')).toEqual({ path: 'photos/2024', error: '' })
		expect(cleanSubPath('./photos/./x')).toEqual({ path: 'photos/x', error: '' })
		expect(cleanSubPath('')).toEqual({ path: '', error: '' })
		expect(cleanSubPath('/abs').error).toBe('invalid')
		expect(cleanSubPath('a/../b').error).toBe('invalid')
		expect(cleanSubPath('a\0b').error).toBe('invalid')
	})

	test('joining, parents and containment', () => {
		expect(joinSubPath('NivaroOS Backups', 'Photos')).toBe('NivaroOS Backups/Photos')
		expect(joinSubPath('', 'x')).toBe('x')
		expect(parentSubPath('a/b/c')).toBe('a/b')
		expect(parentSubPath('a')).toBe('')
		expect(isSameOrInside('photos', 'photos/backup')).toBe(true)
		expect(isSameOrInside('photos', 'photos')).toBe(true)
		expect(isSameOrInside('', 'anything')).toBe(true)
		expect(isSameOrInside('photos', 'photos-old')).toBe(false)
	})

	test('folder names', () => {
		expect(validFolderName('Photos 2024')).toBe(true)
		expect(validFolderName('a/b')).toBe(false)
		expect(validFolderName('..')).toBe(false)
		expect(validFolderName('  ')).toBe(false)
	})
})

test('endpoints are built by identity, USB with its serial and size', () => {
	const usb = byRef('3A4F-1C22')
	expect(endpointFromLocation(usb, 'NivaroOS Backups/Photos')).toEqual({
		kind: 'usb', ref_id: '3A4F-1C22', match: { serial: '4C530001230914117281', size_bytes: 128035676160 }, sub_path: 'NivaroOS Backups/Photos', label: 'Sandisk 128G'
	})
	expect(findLocation(LOCATIONS, { kind: 'cloud', ref_id: 'gdrive' }).label).toBe('Google Drive')
	expect(findLocation(LOCATIONS, { kind: 'usb', ref_id: 'gdrive' })).toBeNull()
})

describe('folder picker target', () => {
	test('a selected row is what "Use" takes, not the folder being browsed', () => {
		expect(pickerTarget('', 'photos')).toBe('photos')
		expect(pickerTarget('DCIM', '2024')).toBe('DCIM/2024')
	})
	test('nothing selected: the folder being browsed', () => {
		expect(pickerTarget('DCIM', '')).toBe('DCIM')
		expect(pickerTarget('', '')).toBe('')
	})
	test('a new folder being added wins over a selection', () => {
		expect(pickerTarget('Backups', 'old', 'new')).toBe('Backups/new')
	})
})
