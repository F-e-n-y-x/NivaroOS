import { expect, test, describe } from 'vitest'
import { buildBreadcrumb } from './breadcrumb'

describe('buildBreadcrumb', () => {
	test('root path', () => {
		expect(buildBreadcrumb('/DATA')).toEqual([
			{ name: 'Root', path: '/' },
			{ name: 'DATA', path: '/DATA' },
		])
	})

	test('nested path', () => {
		expect(buildBreadcrumb('/DATA/tower/photos')).toEqual([
			{ name: 'Root', path: '/' },
			{ name: 'DATA', path: '/DATA' },
			{ name: 'tower', path: '/DATA/tower' },
			{ name: 'photos', path: '/DATA/tower/photos' },
		])
	})

	test('bare root', () => {
		expect(buildBreadcrumb('/')).toEqual([{ name: 'Root', path: '/' }])
	})
})
