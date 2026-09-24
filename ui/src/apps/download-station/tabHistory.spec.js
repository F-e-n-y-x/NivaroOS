import { describe, test, expect } from 'vitest'
import { createHistory, recordVisit, canGo, go, MAX_ENTRIES } from './tabHistory'

describe('lite browser tab history', () => {
	test("back on a tab's first page goes nowhere (never out of NivaroOS)", () => {
		const h = createHistory()
		recordVisit(h, 'https://a.example/')
		expect(canGo(h, 'back')).toBe(false)
		expect(go(h, 'back')).toBe('')
	})

	test('back and forward walk the pages the tab visited', () => {
		const h = createHistory()
		;['https://a/', 'https://b/', 'https://c/'].forEach((u) => recordVisit(h, u))
		expect(go(h, 'back')).toBe('https://b/')
		recordVisit(h, 'https://b/')
		expect(go(h, 'back')).toBe('https://a/')
		recordVisit(h, 'https://a/')
		expect(canGo(h, 'back')).toBe(false)
		expect(go(h, 'forward')).toBe('https://b/')
		recordVisit(h, 'https://b/')
		expect(h.entries).toEqual(['https://a/', 'https://b/', 'https://c/'])
	})

	test('a new page after going back drops the forward pages', () => {
		const h = createHistory()
		;['https://a/', 'https://b/', 'https://c/'].forEach((u) => recordVisit(h, u))
		recordVisit(h, go(h, 'back'))
		recordVisit(h, 'https://d/')
		expect(h.entries).toEqual(['https://a/', 'https://b/', 'https://d/'])
		expect(canGo(h, 'forward')).toBe(false)
	})

	test('a redirect after Back replaces the entry instead of adding one', () => {
		const h = createHistory()
		;['https://a/', 'https://b/'].forEach((u) => recordVisit(h, u))
		go(h, 'back')
		recordVisit(h, 'https://a/home')
		expect(h.entries).toEqual(['https://a/home', 'https://b/'])
		expect(h.index).toBe(0)
	})

	test('reloads and repeated reports of the same page add nothing; the list is capped', () => {
		const h = createHistory()
		recordVisit(h, 'https://a/')
		recordVisit(h, 'https://a/')
		expect(h.entries).toHaveLength(1)
		for (let i = 0; i < MAX_ENTRIES + 10; i++) recordVisit(h, `https://p${i}/`)
		expect(h.entries).toHaveLength(MAX_ENTRIES)
		expect(h.index).toBe(MAX_ENTRIES - 1)
	})

	test("the browser's own Back inside the page moves the cursor instead of adding a page", () => {
		const h = createHistory()
		;['https://a/', 'https://b/', 'https://c/'].forEach((u) => recordVisit(h, u))
		recordVisit(h, 'https://b/')
		expect(h.entries).toEqual(['https://a/', 'https://b/', 'https://c/'])
		expect(h.index).toBe(1)
		recordVisit(h, 'https://c/')
		expect(h.index).toBe(2)
	})
})
