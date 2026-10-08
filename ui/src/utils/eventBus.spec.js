import { describe, expect, test, vi } from 'vitest'
import { createEventBus } from './eventBus'

describe('eventBus', () => {
	test('on/off/once/emit', () => {
		const bus = createEventBus()
		const a = vi.fn()
		const b = vi.fn()
		bus.$on('x', a).$once(['x', 'y'], b)
		bus.$emit('x', 1, 2)
		bus.$emit('x', 3)
		expect(a.mock.calls).toEqual([[1, 2], [3]])
		expect(b.mock.calls).toEqual([[1, 2]])
		bus.$off('x', a)
		bus.$emit('x')
		expect(a).toHaveBeenCalledTimes(2)
		const c = vi.fn()
		bus.$once('z', c).$off('z', c)
		bus.$emit('z')
		expect(c).not.toHaveBeenCalled()
		bus.$on('w', c).$off()
		bus.$emit('w')
		expect(c).not.toHaveBeenCalled()
	})
})
