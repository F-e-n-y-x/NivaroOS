import { describe, test, expect } from 'vitest'
import fs from 'fs'
import path from 'path'
import { assetUrl, currentBuiltinUrl } from './assetUrl'

const src = path.resolve(__dirname, '..')
function files(dir) {
	return fs.readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
		const p = path.join(dir, e.name)
		if (e.isDirectory()) return files(p)
		return /\.(vue|js)$/.test(e.name) && !e.name.startsWith('__') && !e.name.endsWith('.spec.js') ? [p] : []
	})
}

describe('asset URLs', () => {
	test('resolves an image and gives undefined for anything else', () => {
		expect(assetUrl('img/logo/logo.svg')).toMatch(/logo.*\.svg/)
		expect(assetUrl('img/nope.svg')).toBeUndefined()
	})

	test('a built-in wallpaper saved by another build maps to this build', () => {
		expect(currentBuiltinUrl('/img/wallpaper01.a4b92b0e.jpg')).toBe(assetUrl('background/wallpaper01.jpg'))
		expect(currentBuiltinUrl('/DATA/Gallery/me.jpg')).toBe('/DATA/Gallery/me.jpg')
		expect(currentBuiltinUrl('')).toBe('')
	})

	test('every literal assetUrl() path exists', () => {
		const missing = files(src).flatMap((f) =>
			[...fs.readFileSync(f, 'utf8').matchAll(/assetUrl\((['"`])([^'"`$]+)\1\)/g)]
				.map((m) => m[2])
				.filter((p) => !fs.existsSync(path.join(src, 'assets', p)))
				.map((p) => `${path.relative(src, f)}: ${p}`))
		expect(missing).toEqual([])
	})
})
