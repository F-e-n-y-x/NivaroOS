import { expect, test, describe } from 'vitest'
import { baseName, parentPath, joinPath } from './path'

describe('baseName', () => {
	test.each([
		['/DATA', 'DATA'],
		['/DATA/tower', 'tower'],
		['/DATA/tower/photos', 'photos'],
	])('baseName(%s) -> %s', (input, expected) => {
		expect(baseName(input)).toBe(expected)
	})
})

describe('parentPath', () => {
	test.each([
		['/DATA', null],
		['/DATA/tower', '/DATA'],
		['/DATA/tower/photos', '/DATA/tower'],
	])('parentPath(%s) -> %s', (input, expected) => {
		expect(parentPath(input)).toBe(expected)
	})
})

describe('joinPath', () => {
	test('no trailing slash', () => {
		expect(joinPath('/DATA', 'tower')).toBe('/DATA/tower')
	})
	test('trailing slash on dir', () => {
		expect(joinPath('/DATA/', 'tower')).toBe('/DATA/tower')
	})
})
