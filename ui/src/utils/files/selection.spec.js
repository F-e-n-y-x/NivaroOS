import { expect, test, describe } from 'vitest'
import { toggleSelect, selectRange, summarize } from './selection'

describe('toggleSelect', () => {
	test('adds an unselected path', () => {
		expect(toggleSelect(['/a'], '/b')).toEqual(['/a', '/b'])
	})
	test('removes an already-selected path', () => {
		expect(toggleSelect(['/a', '/b'], '/a')).toEqual(['/b'])
	})
})

describe('selectRange', () => {
	const list = [{ path: '/a' }, { path: '/b' }, { path: '/c' }, { path: '/d' }]
	test('selects an inclusive forward range', () => {
		expect(selectRange(list, '/a', '/c')).toEqual(['/a', '/b', '/c'])
	})
	test('selects an inclusive reversed range', () => {
		expect(selectRange(list, '/c', '/a')).toEqual(['/a', '/b', '/c'])
	})
	test('single-item range when from equals to', () => {
		expect(selectRange(list, '/b', '/b')).toEqual(['/b'])
	})
})

describe('summarize', () => {
	const list = [{ path: '/a' }, { path: '/b' }]
	test('none selected', () => {
		expect(summarize(list, [])).toEqual({ count: 0, total: 2, state: 'none' })
	})
	test('some selected', () => {
		expect(summarize(list, ['/a'])).toEqual({ count: 1, total: 2, state: 'part' })
	})
	test('all selected', () => {
		expect(summarize(list, ['/a', '/b'])).toEqual({ count: 2, total: 2, state: 'all' })
	})
})
