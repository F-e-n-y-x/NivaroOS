import { describe, test, expect } from 'vitest'
import { filterRows } from './settingsSearch'

const ROWS = [
	{ sectionId: 'appearance', sectionLabel: 'Appearance', label: 'Window transparency' },
	{ sectionId: 'general', sectionLabel: 'General', label: 'Language' },
	{ sectionId: 'system', sectionLabel: 'System', label: 'WebUI Port' }
]

describe('filterRows', () => {
	test('empty query returns no results', () => {
		expect(filterRows(ROWS, '')).toEqual([])
	})
	test('matches case-insensitively on the row label', () => {
		expect(filterRows(ROWS, 'window')).toEqual([ROWS[0]])
	})
	test('matches a substring anywhere in the label', () => {
		expect(filterRows(ROWS, 'port')).toEqual([ROWS[2]])
	})
	test('whitespace-only query returns no results', () => {
		expect(filterRows(ROWS, '   ')).toEqual([])
	})
	test('matches keywords (people search "restart", not "Power Management")', () => {
		const rows = [{ sectionId: 'system', sectionLabel: 'System', label: 'Power Management', keywords: 'restart reboot shutdown' }]
		expect(filterRows(rows, 'reboot')).toEqual(rows)
	})
	test('matches the translated label', () => {
		const t = (k) => ({ Language: 'Sprache' }[k] || k)
		expect(filterRows(ROWS, 'sprache', t)).toEqual([ROWS[1]])
	})
	test('every word of the query must match', () => {
		expect(filterRows(ROWS, 'webui port')).toEqual([ROWS[2]])
		expect(filterRows(ROWS, 'webui window')).toEqual([])
	})
})
