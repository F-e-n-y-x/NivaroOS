import { expect, test, describe } from 'vitest'
import { baseName, parentPath, joinPath, shellQuote } from './path'

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

describe('shellQuote', () => {
	test('plain path', () => {
		expect(shellQuote('/DATA/tower')).toBe("'/DATA/tower'")
	})
	test('path with a space', () => {
		expect(shellQuote('/DATA/My Photos')).toBe("'/DATA/My Photos'")
	})
	test('path with an embedded single quote', () => {
		expect(shellQuote("/DATA/O'Brien")).toBe("'/DATA/O'\\''Brien'")
	})
})
