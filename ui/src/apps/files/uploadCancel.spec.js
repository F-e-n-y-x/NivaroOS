// @vitest-environment jsdom
import { describe, expect, test, vi } from 'vitest'
import Uploader from 'simple-uploader.js'
import { uploadIdentifier } from './uploadIdentifier'
import { cancelUpload } from './uploadCancel'

describe('cancelUpload', () => {
	test('sends the fields that key the upload on the server, plus cancel', async () => {
		const up = new Uploader({ generateUniqueIdentifier: uploadIdentifier })
		up.addFiles([new File(['abcdef'], 'clip.mp4')])
		const file = up.files[0]
		file.targetPath = '/DATA/Videos'
		const fetch = vi.fn(() => Promise.resolve({ ok: true }))
		vi.stubGlobal('fetch', fetch)
		await cancelUpload('/v2/casaos/file/upload', { Authorization: 't' }, file)
		const [url, init] = fetch.mock.calls[0]
		expect(url).toBe('/v2/casaos/file/upload')
		expect(init.method).toBe('POST')
		expect(init.headers.Authorization).toBe('t')
		expect(Object.fromEntries(init.body)).toEqual({
			cancel: '1',
			path: '/DATA/Videos',
			relativePath: 'clip.mp4',
			filename: 'clip.mp4',
			identifier: file.uniqueIdentifier,
			totalSize: '6',
		})
		fetch.mockImplementation(() => Promise.reject(new Error('offline')))
		await cancelUpload('/x', {}, file) // never throws
		vi.unstubAllGlobals()
	})
})
