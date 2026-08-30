import { expect, test, describe } from 'vitest'
import { classifyWidth } from './breakpoints'

describe('classifyWidth', () => {
	test('wide window: nothing collapsed', () => {
		expect(classifyWidth(900)).toEqual({
			sidebarCollapsed: false,
			toolbarCollapsed: false,
			singleColumnGrid: false,
		})
	})

	test('narrow window: sidebar + toolbar collapse, grid stays multi-column', () => {
		expect(classifyWidth(500)).toEqual({
			sidebarCollapsed: true,
			toolbarCollapsed: true,
			singleColumnGrid: false,
		})
	})

	test('at the window floor: grid drops to a single column too', () => {
		expect(classifyWidth(400)).toEqual({
			sidebarCollapsed: true,
			toolbarCollapsed: true,
			singleColumnGrid: true,
		})
	})

	test('boundary values are exclusive on the collapse threshold', () => {
		expect(classifyWidth(560).sidebarCollapsed).toBe(false)
		expect(classifyWidth(559).sidebarCollapsed).toBe(true)
		expect(classifyWidth(420).singleColumnGrid).toBe(false)
		expect(classifyWidth(419).singleColumnGrid).toBe(true)
	})
})
