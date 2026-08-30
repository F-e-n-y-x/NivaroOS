import { expect, test, describe } from 'vitest'
import { classifyWidth } from './breakpoints'

describe('classifyWidth', () => {
	test('wide window: nothing collapsed', () => {
		expect(classifyWidth(900)).toEqual({ navCollapsed: false, rowsStacked: false })
	})

	test('narrow window: nav collapses, rows do not stack yet', () => {
		expect(classifyWidth(600)).toEqual({ navCollapsed: true, rowsStacked: false })
	})

	test('very narrow window: both collapse', () => {
		expect(classifyWidth(400)).toEqual({ navCollapsed: true, rowsStacked: true })
	})

	test('boundary values are exclusive on each threshold', () => {
		expect(classifyWidth(736).navCollapsed).toBe(false)
		expect(classifyWidth(735).navCollapsed).toBe(true)
		expect(classifyWidth(544).rowsStacked).toBe(false)
		expect(classifyWidth(543).rowsStacked).toBe(true)
	})
})
