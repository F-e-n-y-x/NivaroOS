import { describe, test, expect } from 'vitest'
import { buildIdFromHtml, isChunkLoadError } from './updateWatcher'

describe('update watcher', () => {
	test('reads the build id from the served index.html', () => {
		const html = '<head><script defer="defer" src="/app.2cf48ed3.js"></script><script src="/js/custom.js"></script></head>'
		expect(buildIdFromHtml(html)).toBe('app.2cf48ed3.js')
		expect(buildIdFromHtml('<html></html>')).toBe('')
	})

	test('recognises chunk load failures only', () => {
		expect(isChunkLoadError({ name: 'ChunkLoadError', message: 'Loading chunk 655 failed.' })).toBe(true)
		expect(isChunkLoadError(new Error('Loading CSS chunk 12 failed.'))).toBe(true)
		expect(isChunkLoadError(new Error('Network Error'))).toBe(false)
		expect(isChunkLoadError(null)).toBe(false)
	})
})
