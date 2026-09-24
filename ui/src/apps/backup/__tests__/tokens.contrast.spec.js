import { describe, test, expect } from 'vitest'
import fs from 'fs'
import path from 'path'

// Spec §13: every --status-<tone>-{bg,fg} pair reaches 4.5:1 in both
// themes, computed from the SCSS values - on the pill's own background and
// on the card (the fg is also used alone, e.g. for status icons and text).
// Also: no hex colours in backup component styles, and no modal dialogs.
const scssDir = path.resolve(__dirname, '../../../assets/scss/common')
const root = fs.readFileSync(path.join(scssDir, '_root.scss'), 'utf8')
const dark = fs.readFileSync(path.join(scssDir, '_dark.scss'), 'utf8')

function token(src, name) {
	const m = new RegExp(`--${name}:\\s*(#[0-9a-fA-F]{6})\\b`).exec(src)
	return m ? m[1] : null
}

function luminance(hex) {
	const [r, g, b] = [1, 3, 5].map(i => parseInt(hex.slice(i, i + 2), 16) / 255).map(c => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4))
	return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

function contrast(a, b) {
	const [x, y] = [luminance(a), luminance(b)].sort((p, q) => q - p)
	return (x + 0.05) / (y + 0.05)
}

const TONES = ['ok', 'warn', 'danger', 'info', 'muted']

describe('status tokens', () => {
	for (const [theme, src] of [
		['light', root],
		['dark', dark]
	]) {
		test(`${theme}: every pill pair and fg-on-card reaches 4.5:1`, () => {
			const card = token(src, 'theme-card-bg')
			expect(card).toBeTruthy()
			const low = []
			for (const tone of TONES) {
				const bg = token(src, `status-${tone}-bg`)
				const fg = token(src, `status-${tone}-fg`)
				expect(bg, `--status-${tone}-bg (${theme})`).toBeTruthy()
				expect(fg, `--status-${tone}-fg (${theme})`).toBeTruthy()
				if (contrast(fg, bg) < 4.5) low.push(`${tone} on its bg: ${contrast(fg, bg).toFixed(2)}`)
				if (contrast(fg, card) < 4.5) low.push(`${tone} on the card: ${contrast(fg, card).toFixed(2)}`)
			}
			expect(low).toEqual([])
		})
	}
})

function files(dir, ext) {
	if (!fs.existsSync(dir)) return []
	return fs.readdirSync(dir, { withFileTypes: true }).flatMap(e => {
		const p = path.join(dir, e.name)
		if (e.isDirectory()) return files(p, ext)
		return ext.some(x => e.name.endsWith(x)) ? [p] : []
	})
}

describe('backup components (spec §16.3 CI greps)', () => {
	const appDir = path.resolve(__dirname, '..')
	const vue = files(appDir, ['.vue'])
	const scss = files(appDir, ['.scss'])

	test('no hex colours in styles - theme tokens only', () => {
		const offenders = []
		for (const f of vue.concat(scss)) {
			const src = fs.readFileSync(f, 'utf8')
			const styles = f.endsWith('.vue') ? (src.match(/<style[\s\S]*?<\/style>/g) || []).join('\n') : src
			styles.split('\n').forEach((line, i) => {
				if (/#[0-9a-fA-F]{3,8}\b/.test(line.replace(/\/\/.*$/, ''))) offenders.push(`${path.relative(appDir, f)}:${i + 1}`)
			})
		}
		expect(offenders).toEqual([])
	})

	test('no Buefy dialogs or modals - every dialog is a desktop window', () => {
		const offenders = vue.concat(files(appDir, ['.js'])).filter(f => !f.includes('__tests__')).filter(f => /\$buefy\.dialog|<b-modal|\$buefy\.modal/.test(fs.readFileSync(f, 'utf8')))
		expect(offenders.map(f => path.relative(appDir, f))).toEqual([])
	})
})
