import { expect, test, describe } from 'vitest'
import { deleteOutcome, trashPlace } from './trashWhere'

describe('deleteOutcome', () => {
	test.each([
		[{ supported: true, kind: 'disk' }, false, { mode: 'trash', kind: 'disk' }],
		[{ supported: true, kind: 'phone' }, false, { mode: 'trash', kind: 'phone' }],
		[{ supported: true, kind: 'share' }, true, { mode: 'permanent', kind: 'share', reason: 'chosen' }],
		[{ supported: false, kind: 'cloud', provider: 'Google Drive' }, false, { mode: 'provider', kind: 'cloud', provider: 'Google Drive' }],
		[{ supported: false, kind: 'cloud', provider: 'TeraBox' }, true, { mode: 'provider', kind: 'cloud', provider: 'TeraBox' }],
		[{ supported: false, kind: 'cloud', reason: 'no_server_move' }, false, { mode: 'permanent', kind: 'cloud', reason: 'no_server_move' }],
		[{ supported: false, kind: 'share', reason: 'readonly' }, false, { mode: 'permanent', kind: 'share', reason: 'readonly' }],
		[{ supported: false }, false, { mode: 'permanent', kind: 'disk', reason: 'no_trash' }],
		[null, false, { mode: 'permanent', kind: 'disk', reason: 'no_trash' }],
	])('%j skip=%s', (support, skip, want) => {
		expect(deleteOutcome(support, skip)).toEqual(want)
	})
})

describe('trashPlace', () => {
	test('disk items need no place', () => {
		expect(trashPlace({ kind: 'disk' })).toBe(null)
		expect(trashPlace({})).toBe(null)
	})
	test('phone, cloud and share items name where they are', () => {
		expect(trashPlace({ kind: 'phone', location: 'Pixel 8' })).toEqual({ icon: 'cellphone', label: 'Pixel 8' })
		expect(trashPlace({ kind: 'cloud', location: 'gdrive' })).toEqual({ icon: 'cloud-outline', label: 'gdrive' })
		expect(trashPlace({ kind: 'share', location: '//nas/media' })).toEqual({ icon: 'folder-network-outline', label: '//nas/media' })
	})
})
