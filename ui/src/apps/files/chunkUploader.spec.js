// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest'
import { createUploader } from './chunkUploader'

// Fake XHR: every request waits in `reqs` until the test answers it.
let reqs
class FakeXHR {
	constructor() { this.upload = {}; this.headers = {}; reqs.push(this) }
	open(method, url) { this.method = method; this.url = url }
	setRequestHeader(k, v) { this.headers[k] = v }
	send(body) { this.body = body }
	abort() { this.aborted = true }
	reply(status, text = '') { this.status = status; this.responseText = text; this.onload() }
	drop() { this.status = 0; this.responseText = ''; this.onerror() }
	get fields() { return this.method === 'GET' ? Object.fromEntries(new URL(this.url, 'http://x').searchParams) : Object.fromEntries([...this.body].filter(([k]) => k !== 'file')) }
}
const pending = () => reqs.filter((r) => r.status === undefined && !r.aborted)
const flush = () => new Promise((r) => setTimeout(r, 0))

beforeEach(() => {
	reqs = []
	vi.stubGlobal('XMLHttpRequest', FakeXHR)
	vi.stubGlobal('fetch', vi.fn(() => Promise.resolve({ ok: true })))
})
afterEach(() => { vi.unstubAllGlobals(); vi.useRealTimers() })

const make = (o = {}) => {
	const cb = { success: [], error: [], complete: 0 }
	const up = createUploader({
		target: '/up', chunkSize: 4, retries: 2, retryInterval: 10,
		headers: () => ({ Authorization: 'tok', Skip: null }),
		fields: (e) => ({ path: e.targetPath }),
		onSuccess: (e) => cb.success.push(e.name), onError: (e, m) => cb.error.push(m), onComplete: () => cb.complete++,
		...o,
	})
	return { up, cb }
}

describe('chunkUploader', () => {
	test('asks for each chunk, then sends it with the fields the server keys the upload by', async () => {
		const { up, cb } = make()
		const [e] = up.add([new File(['abcdefghij'], 'a b.txt')], { targetPath: '/DATA/x' })
		expect(pending().map((r) => r.method)).toEqual(['GET', 'GET', 'GET']) // 3 chunks, 3 at a time
		const t = pending()[2]
		expect(t.fields).toEqual({ chunkNumber: '3', chunkSize: '4', currentChunkSize: '2', totalSize: '10', identifier: e.uniqueIdentifier, filename: 'a b.txt', relativePath: 'a b.txt', totalChunks: '3', path: '/DATA/x' })
		expect(t.headers).toEqual({ Authorization: 'tok' })
		pending().forEach((r) => r.reply(204))
		const posts = pending()
		expect(posts.map((r) => r.method)).toEqual(['POST', 'POST', 'POST'])
		expect(posts[2].fields).toEqual(t.fields)
		expect(posts[2].body.get('file').size).toBe(2)
		expect(posts[2].body.get('file').name).toBe('a b.txt')
		expect([...posts[2].body.keys()].at(-1)).toBe('file') // bytes last
		posts.forEach((r) => r.reply(200))
		expect(cb.success).toEqual(['a b.txt'])
		expect(e.progress).toBe(1)
		expect(cb.complete).toBe(1)
	})

	test('resume: a chunk the server already has (200 on the check) is not sent again', () => {
		const { up, cb } = make()
		up.add([new File(['abcdefgh'], 'r.bin')], { targetPath: '/d' })
		const [c1, c2] = pending()
		c1.reply(200)
		c2.reply(204)
		const posts = pending()
		expect(posts.map((r) => r.method + r.fields.chunkNumber)).toEqual(['POST2'])
		posts[0].reply(200)
		expect(cb.success).toEqual(['r.bin'])
	})

	test('a transient failure retries the chunk; running out of retries fails the file with the server message', async () => {
		vi.useFakeTimers()
		const { up, cb } = make({ testChunks: false })
		up.add([new File(['abc'], 'f')], { targetPath: '/d' })
		pending()[0].reply(500, '{"message":"busy"}')
		expect(pending().length).toBe(0)
		vi.advanceTimersByTime(10)
		pending()[0].drop() // network drop (status 0)
		vi.advanceTimersByTime(10)
		pending()[0].reply(500, '{"message":"not enough free space"}')
		expect(cb.error).toEqual(['{"message":"not enough free space"}'])
		expect(cb.success).toEqual([])
		expect(reqs.length).toBe(3)
	})

	test('a permanent error fails right away and aborts the file\'s other chunks; other files keep going', () => {
		const { up, cb } = make({ testChunks: false, simultaneous: 3 })
		up.add([new File(['abcdefgh'], 'bad'), new File(['x'], 'ok')], { targetPath: '/d' })
		const [b1, b2, ok] = pending()
		b1.reply(400, 'bad path')
		expect(b2.aborted).toBe(true)
		expect(cb.error).toEqual(['bad path'])
		expect(cb.complete).toBe(0)
		ok.reply(200)
		expect(cb.success).toEqual(['ok'])
		expect(cb.complete).toBe(1)
	})

	test('cancel stops the file, frees its slots and tells the server (cancel=1)', async () => {
		const { up } = make({ testChunks: false, simultaneous: 1 })
		const [a] = up.add([new File(['abcdefgh'], 'a'), new File(['x'], 'b')], { targetPath: '/d' })
		const first = pending()[0]
		up.cancel(a)
		expect(first.aborted).toBe(true)
		expect(pending().map((r) => r.body.get('filename'))).toEqual(['b'])
		const [url, init] = fetch.mock.calls[0]
		expect(url).toBe('/up')
		expect(init.headers.Authorization).toBe('tok')
		expect(Object.fromEntries(init.body)).toMatchObject({ cancel: '1', path: '/d', filename: 'a', identifier: a.uniqueIdentifier, totalSize: '8' })
		first.reply(200) // late answer for the aborted request is ignored
		expect(pending().length).toBe(1)
	})

	test('an empty file is one empty chunk; folder entries keep their relative path', async () => {
		const { up, cb } = make({ testChunks: false })
		const f = new File([], 'empty.txt')
		Object.defineProperty(f, 'relativePath', { value: 'dir/empty.txt' })
		up.add([f, Object.assign(new File([], '.'))], { targetPath: '/d' })
		const [r] = pending()
		expect(pending().length).toBe(1)
		expect(r.fields).toMatchObject({ currentChunkSize: '0', totalChunks: '1', relativePath: 'dir/empty.txt' })
		r.reply(201)
		expect(cb.success).toEqual(['empty.txt'])
	})

	test('the same file added twice is uploaded twice (own identifier each time)', () => {
		const { up } = make({ testChunks: false })
		const f = new File(['abc'], 'photo.jpg')
		const [a] = up.add([f])
		const [b] = up.add([f])
		up.add([new File(['xyz'], '日本.txt'), new File(['xyz'], '中国.txt')])
		expect(up.files.length).toBe(4)
		expect(a.uniqueIdentifier).not.toBe(b.uniqueIdentifier)
	})

	test('progress follows the bytes sent and never goes backwards', () => {
		vi.useFakeTimers()
		const { up } = make({ testChunks: false, simultaneous: 1, onProgress: (e) => seen.push(e.progress) })
		const seen = []
		const [e] = up.add([new File(['abcdefgh'], 'p')], { targetPath: '/d' })
		vi.advanceTimersByTime(600)
		pending()[0].upload.onprogress({ lengthComputable: true, loaded: 50, total: 100 })
		expect(seen.at(-1)).toBe(0.25)
		pending()[0].reply(200)
		expect(seen.at(-1)).toBe(0.5)
		expect(e.speed).toBeGreaterThan(0)
	})
})
