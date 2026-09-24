import { describe, test, expect } from 'vitest'
import fs from 'fs'
import path from 'path'
import { assetUrl } from './assetUrl'

// The production build turns require()'d images into { default: url }; one
// reaching an <img src> unwrapped throws inside Vue's patch and freezes the
// component (the CPU widget's process list once stuck open that way).
function vueFiles(dir) {
	return fs.readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
		const p = path.join(dir, e.name)
		if (e.isDirectory()) return vueFiles(p)
		return e.name.endsWith('.vue') && !e.name.startsWith('__') ? [p] : []
	})
}

describe('asset URLs', () => {
	test('unwraps a module namespace and passes a URL through', () => {
		expect(assetUrl({ default: '/img/x.svg' })).toBe('/img/x.svg')
		expect(assetUrl('/img/x.svg')).toBe('/img/x.svg')
	})

	test('every asset require() in a component is wrapped', () => {
		const bare = /(?<![\w$]\()\brequire\((['"`])@\/assets\//
		const offenders = vueFiles(path.resolve(__dirname, '..'))
			.flatMap((f) => fs.readFileSync(f, 'utf8').split('\n').map((line, i) => [f, i + 1, line]))
			.filter(([, , line]) => bare.test(line) && !/assetUrl\(require\(/.test(line))
			.map(([f, n]) => `${path.relative(path.resolve(__dirname, '..'), f)}:${n}`)
		expect(offenders).toEqual([])
	})
})
