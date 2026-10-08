import { describe, expect, test } from 'vitest'
import { firstError, minLength, required, sameAs } from './formChecks'

describe('form checks', () => {
	test('first failing rule wins', () => {
		expect(firstError('', [required, minLength(5)])).toBe('This field is required')
		expect(firstError('abc', [required, minLength(5)])).toBe('This field must have more than 5 characters')
		expect(firstError('abcde', [required, minLength(5)])).toBe('')
		expect(firstError('x', [required, sameAs('y')])).toBe('This field confirmation does not match')
		expect(firstError('  ', [required])).toBe('This field is required')
	})
})
