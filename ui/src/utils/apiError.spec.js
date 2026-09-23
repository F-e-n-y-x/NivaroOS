import { describe, test, expect } from 'vitest'
import { apiError } from './apiError'

const res = (data) => ({ response: { data } })

describe('apiError', () => {
	test("core's reason is in data", () => {
		expect(apiError(res({ success: 4000, message: 'Parameters Error', data: 'password too short' }))).toBe('password too short')
	})
	test('otherwise the message, unless it is a generic one', () => {
		expect(apiError(res({ message: 'that name is reserved' }))).toBe('that name is reserved')
		expect(apiError(res({ message: 'service error' }), 'Save failed')).toBe('Save failed')
	})
	test("axios' generic text is the last resort", () => {
		expect(apiError({ message: 'Request failed with status code 400' }, 'Save failed')).toBe('Save failed')
		expect(apiError({ message: 'Network Error' })).toMatch(/reach the server/)
	})
})
