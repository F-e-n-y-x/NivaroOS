#!/usr/bin/env node
// NivaroOS brand generator - the single source of every logo file in the repo.
//
//   node docs/brand/gen.js          writes the SVGs in docs/brand/
//   node docs/brand/gen.js --png    also renders every PNG/ICO the web UI,
//                                    the Android app and GitHub ship
//
// Needs opentype.js (`npm i --no-save --prefix docs/brand opentype.js`, or
// anywhere on NODE_PATH) for the outlined wordmark; --png also needs google-chrome
// (headless, for exact SVG rendering) and ImageMagick (`convert`, for the
// .ico and to drop PNG alpha where a platform wants an opaque image).
//
// The mark ("Ni"): an N whose last stroke is also the stem of an i, with the
// i's dot as a status light. Everything is drawn on a 256 grid.
const fs = require('fs'), path = require('path'), os = require('os'), cp = require('child_process')
let opentype
try { opentype = require('opentype.js') } catch {
	console.error('opentype.js not found: run `npm i --no-save --prefix docs/brand opentype.js` first'); process.exit(1)
}
const ROOT = path.resolve(__dirname, '../..')
const OUT = __dirname
const FONTS = path.join(ROOT, 'mobile/assets/fonts/')
const bold = opentype.loadSync(FONTS + 'Geist-600.ttf'), reg = opentype.loadSync(FONTS + 'Geist-400.ttf')

const C = { blue: '#2563EB', mint: '#7DF9C5', teal: '#0EA371', ink: '#0F1115', white: '#FFFFFF' }

// Mark geometry. Stroke 32 with round caps and joins; the dot is r17.
const N_PATH = 'M80 194V110L176 194V110'
const glyph = (n, dot) => `<path d="${N_PATH}" fill="none" stroke="${n}" stroke-width="32" stroke-linecap="round" stroke-linejoin="round"/><circle cx="176" cy="62" r="17" fill="${dot}"/>`
const svg = (w, h, body, title = 'NivaroOS') => `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${w} ${h}" width="${w}" height="${h}" role="img" aria-label="${title}"><title>${title}</title>${body}</svg>\n`
const tile = (bg, n, dot, edge) => `<rect width="256" height="256" rx="60" fill="${bg}"/>` + (edge ? `<rect x="1" y="1" width="254" height="254" rx="59" fill="none" stroke="${edge}" stroke-width="2"/>` : '') + glyph(n, dot)
// The glyph scaled about the grid centre: s = how many canvas units the 256
// grid spans, on a canvas of `size` units.
const scaledGlyph = (size, s, n, dot) => { const k = s / 256, o = (size - s) / 2; return `<g transform="translate(${o.toFixed(3)} ${o.toFixed(3)}) scale(${k.toFixed(5)})">${glyph(n, dot)}</g>` }

// Wordmark: "Nivaro" semibold + "OS" regular in Geist, outlined so it renders
// the same everywhere (no font needed at runtime).
function word(size) {
	const track = -0.012 * size
	let x = 0, d = ''
	const run = (font, text) => { for (const ch of text) { const g = font.charToGlyph(ch); d += g.getPath(x, 0, size).toPathData(2); x += g.advanceWidth * size / font.unitsPerEm + track } }
	run(bold, 'Nivaro'); x += 0.02 * size; run(reg, 'OS')
	const cap = bold.tables.os2.sCapHeight * size / bold.unitsPerEm
	return { d, w: x - track, cap }
}
function lockupBody(text) {
	const w = word(120), gap = 56
	const ty = 128 + w.cap / 2
	return { w: Math.ceil(256 + gap + w.w + 8), h: 256, body: tile(C.blue, C.white, C.mint) + `<path transform="translate(${256 + gap} ${ty.toFixed(1)})" d="${w.d}" fill="${text}"/>` }
}
const lockup = text => { const l = lockupBody(text); return svg(l.w, l.h, l.body) }

const files = {
	'nivaroos-icon.svg': svg(256, 256, tile(C.blue, C.white, C.mint)),
	'nivaroos-icon-dark.svg': svg(256, 256, tile(C.ink, C.white, C.mint, 'rgba(255,255,255,0.14)')),
	'nivaroos-glyph.svg': svg(256, 256, glyph(C.blue, C.teal)),
	'nivaroos-glyph-white.svg': svg(256, 256, glyph(C.white, C.mint)),
	'nivaroos-glyph-mono.svg': svg(256, 256, glyph(C.ink, C.ink)),
	'nivaroos-logo.svg': lockup(C.ink),
	'nivaroos-logo-white.svg': lockup(C.white),
}
{ const w = word(120); files['nivaroos-wordmark.svg'] = svg(Math.ceil(w.w + 4), Math.ceil(w.cap + 8), `<path transform="translate(0 ${(w.cap + 4).toFixed(1)})" d="${w.d}" fill="${C.ink}"/>`) }
for (const [k, v] of Object.entries(files)) fs.writeFileSync(path.join(OUT, k), v)
console.log('svg:', Object.keys(files).join(' '))

// Web UI copies (SVGs are used as-is).
const UI = path.join(ROOT, 'ui')
const copy = (from, to) => { fs.mkdirSync(path.dirname(to), { recursive: true }); fs.copyFileSync(path.join(OUT, from), to) }
copy('nivaroos-logo.svg', path.join(UI, 'src/assets/img/logo/logo.svg'))
copy('nivaroos-logo-white.svg', path.join(UI, 'src/assets/img/logo/logo-white.svg'))
copy('nivaroos-icon.svg', path.join(UI, 'src/assets/img/logo/icon.svg'))
copy('nivaroos-glyph.svg', path.join(UI, 'src/assets/img/logo/glyph.svg'))
copy('nivaroos-glyph-white.svg', path.join(UI, 'src/assets/img/logo/glyph-white.svg'))
copy('nivaroos-icon.svg', path.join(UI, 'public/favicon.svg'))
// Safari pinned tab: one flat colour, Safari tints it.
fs.writeFileSync(path.join(UI, 'public/img/icon/safari-pinned-tab.svg'), `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 256 256">${glyph('#000000', '#000000')}</svg>\n`)

if (!process.argv.includes('--png')) process.exit(0)

// ---- PNG rendering -------------------------------------------------------
const CHROME = process.env.CHROME || '/usr/bin/google-chrome'
const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'nivaroos-brand-'))
function render(body, vw, vh, w, h, out, { opaque } = {}) {
	// vw x vh: SVG viewBox; w x h: output pixels.
	const html = path.join(tmp, 'r.html')
	fs.writeFileSync(html, `<!doctype html><html><head><style>html,body{margin:0;padding:0;background:transparent;overflow:hidden}svg{display:block}</style></head><body><svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${vw} ${vh}" width="${w}" height="${h}">${body}</svg></body></html>`)
	const shot = path.join(tmp, 'shot.png')
	cp.execFileSync(CHROME, ['--headless=new', '--no-sandbox', '--disable-gpu', '--hide-scrollbars', '--force-device-scale-factor=1', '--default-background-color=00000000', `--window-size=${w},${h}`, `--screenshot=${shot}`, 'file://' + html], { stdio: 'ignore' })
	fs.mkdirSync(path.dirname(out), { recursive: true })
	const args = [shot, '-crop', `${w}x${h}+0+0`, '+repage']
	if (opaque) args.push('-background', opaque, '-alpha', 'remove', '-alpha', 'off')
	args.push('-strip', (opaque ? 'PNG24:' : 'PNG32:') + out)
	cp.execFileSync('convert', args)
	console.log('png:', path.relative(ROOT, out), `${w}x${h}`)
}
const TILE = tile(C.blue, C.white, C.mint)
const icon = (px, out) => render(TILE, 256, 256, px, px, out)

// Brand kit PNGs.
for (const px of [1024, 512, 192]) icon(px, path.join(OUT, `nivaroos-icon-${px}.png`))
{ const l = lockupBody(C.ink); render(l.body, l.w, l.h, Math.round(l.w * 512 / 256), 512, path.join(OUT, 'nivaroos-logo.png')) }
{ const l = lockupBody(C.white); render(l.body, l.w, l.h, Math.round(l.w * 512 / 256), 512, path.join(OUT, 'nivaroos-logo-white.png')) }
// GitHub social preview, 1280x640: white lockup centred on ink.
{
	const l = lockupBody(C.white), W = 1280, H = 640, lh = 192, lw = l.w * lh / l.h
	const body = `<rect width="${W}" height="${H}" fill="${C.ink}"/><g transform="translate(${((W - lw) / 2).toFixed(2)} ${((H - lh) / 2).toFixed(2)}) scale(${(lh / l.h).toFixed(5)})">${l.body}</g>`
	render(body, W, H, W, H, path.join(OUT, 'social-preview.png'), { opaque: C.ink })
}

// Web UI.
{ const l = lockupBody(C.ink); render(l.body, l.w, l.h, Math.round(l.w * 128 / 256), 128, path.join(UI, 'src/assets/img/logo/logo.png')) }
const IC = path.join(UI, 'public/img/icon')
icon(192, path.join(IC, 'android-chrome-192x192.png'))
icon(512, path.join(IC, 'android-chrome-512x512.png'))
icon(16, path.join(IC, 'favicon-16x16.png'))
icon(32, path.join(IC, 'favicon-32x32.png'))
// Maskable PWA icon: full-bleed blue, glyph inside the 80% safe circle.
render(`<rect width="512" height="512" fill="${C.blue}"/>` + scaledGlyph(512, 512 * 0.74, C.white, C.mint), 512, 512, 512, 512, path.join(IC, 'maskable-512x512.png'), { opaque: C.blue })
// iOS masks the corners itself and wants an opaque, edge-to-edge square.
render(`<rect width="256" height="256" fill="${C.blue}"/>` + glyph(C.white, C.mint), 256, 256, 180, 180, path.join(IC, 'apple-touch-icon.png'), { opaque: C.blue })
// Windows tile: white glyph on transparent, the tile colour comes from browserconfig.xml.
render(scaledGlyph(256, 150, C.white, C.white), 256, 256, 150, 150, path.join(IC, 'mstile-150x150.png'))
{
	const ico = [16, 32, 48].map(px => { const p = path.join(tmp, `fav-${px}.png`); icon(px, p); return p })
	cp.execFileSync('convert', [...ico, path.join(UI, 'public/favicon.ico')])
	console.log('ico: ui/public/favicon.ico 16/32/48')
}

// Android app.
const M = path.join(ROOT, 'mobile')
const RES = path.join(M, 'android/app/src/main/res')
icon(1024, path.join(M, 'assets/icon/icon.png'))
// Adaptive icon: 108dp canvas, the 256 grid spans 80dp, so the whole mark
// (radius ~98 grid units from the centre) stays inside the 66dp safe circle.
const DPI = { mdpi: 1, hdpi: 1.5, xhdpi: 2, xxhdpi: 3, xxxhdpi: 4 }
for (const [d, k] of Object.entries(DPI)) {
	icon(Math.round(48 * k), path.join(RES, `mipmap-${d}/ic_launcher.png`))
	const px = Math.round(108 * k)
	render(scaledGlyph(108, 80, C.white, C.mint), 108, 108, px, px, path.join(RES, `drawable-${d}/ic_launcher_foreground.png`))
	render(scaledGlyph(108, 80, C.white, C.white), 108, 108, px, px, path.join(RES, `drawable-${d}/ic_launcher_monochrome.png`))
}
fs.rmSync(tmp, { recursive: true, force: true })
