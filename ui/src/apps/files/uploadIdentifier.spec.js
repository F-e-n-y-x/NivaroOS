// @vitest-environment jsdom
import { describe, expect, test } from 'vitest'
import Uploader from 'simple-uploader.js'
import { uploadIdentifier } from './uploadIdentifier'

describe('uploadIdentifier', () => {
	test('the same file added twice is uploaded twice', () => {
		const up = new Uploader({ generateUniqueIdentifier: uploadIdentifier })
		const f = new File(['abc'], 'photo.jpg')
		up.addFiles([f])
		up.addFiles([f])
		up.addFiles([new File(['xyz'], '日本.txt'), new File(['xyz'], '中国.txt')])
		expect(up.files.length).toBe(4)
	})
})
