// The README's "fetch" card: a terminal running `nivarofetch`, like
// neofetch - the NivaroOS mark as an LED matrix on the left, facts on the
// right, the palette underneath. Text is outlined (Geist Mono) so it looks
// the same on every machine GitHub shows it on.
//   node gen-fetch.js <fonts dir> <out.svg>
const opentype = require('opentype.js'), fs = require('fs'), path = require('path')
const [fontsDir = '../../mobile/assets/fonts', out = 'nivaroos-fetch.svg'] = process.argv.slice(2)
const mono = opentype.loadSync(path.join(fontsDir, 'GeistMono-400.ttf'))
const monoB = opentype.loadSync(path.join(fontsDir, 'GeistMono-500.ttf'))
const C = {
  bg: '#0F1115', bar: '#171A20', edge: '#262A33', dim: '#7A8190', text: '#D6DBE4',
  key: '#7FA6FF', mint: '#7DF9C5', blue: '#2563EB',
}
const W = 900, H = 392
let body = ''
// Text as paths: one <path> per run, advancing by the font's own widths.
function text(x, y, s, size, fill, font = mono) {
  let d = '', cx = x
  for (const ch of s) {
    const g = font.charToGlyph(ch)
    d += g.getPath(cx, y, size).toPathData(1)
    cx += g.advanceWidth * size / font.unitsPerEm
  }
  body += `<path d="${d}" fill="${fill}"/>`
  return cx
}
// Window
body += `<rect x="1" y="1" width="${W - 2}" height="${H - 2}" rx="14" fill="${C.bg}" stroke="${C.edge}" stroke-width="2"/>`
body += `<path d="M1 15a14 14 0 0 1 14-14h${W - 30}a14 14 0 0 1 14 14v27H1z" fill="${C.bar}"/>`
body += `<line x1="1" y1="42" x2="${W - 1}" y2="42" stroke="${C.edge}" stroke-width="1"/>`
;['#FF5F57', '#FEBC2E', '#28C840'].forEach((c, i) => { body += `<circle cx="${28 + i * 22}" cy="22" r="6.5" fill="${c}"/>` })
{ const t = 'nivaroos — self-hosted home server OS', size = 13
  const w = [...t].reduce((a, ch) => a + mono.charToGlyph(ch).advanceWidth * size / mono.unitsPerEm, 0)
  text((W - w) / 2, 27, t, size, C.dim) }
// Prompt
let px = text(36, 80, 'ayush@atom:~$ ', 15, C.dim)
text(px, 80, 'nivarofetch', 15, C.mint, monoB)
// The mark as an LED matrix: N strokes 2 wide, the i's dot in mint.
const cell = 14, gap = 3, ox = 40, oy = Math.round(118 - 15 + (26 * 9) / 2 - (12 * 14) / 2)
const rows = 9, cols = 11
const on = []
for (let r = 0; r < rows; r++) for (let c = 0; c < cols; c++) {
  const diag = Math.round(r * 7 / (rows - 1)) + 1
  if (c <= 1 || c >= 9 || c === diag || c === diag + 1) on.push([r + 3, c])
}
const shade = r => ['#9DB9FF', '#86A8FF', '#6F96FB', '#5A86F5', '#4677EE', '#3A6CEA', '#2F62E8', '#2658E0', '#1F4FD6'][r - 3]
for (const [r, c] of on) body += `<rect x="${ox + c * cell}" y="${oy + r * cell}" width="${cell - gap}" height="${cell - gap}" rx="2.5" fill="${shade(r)}"/>`
for (const [r, c] of [[0, 9], [0, 10], [1, 9], [1, 10]]) body += `<rect x="${ox + c * cell}" y="${oy + r * cell}" width="${cell - gap}" height="${cell - gap}" rx="2.5" fill="${C.mint}"/>`
// "led off" dots behind, faint, so it reads as a panel
for (let r = 0; r < rows + 3; r++) for (let c = 0; c < cols; c++) {
  if (r === 2) continue
  const lit = on.some(([a, b]) => a === r && b === c) || (r < 2 && c >= 9)
  if (!lit) body += `<rect x="${ox + c * cell + 4}" y="${oy + r * cell + 4}" width="${cell - gap - 8}" height="${cell - gap - 8}" rx="1.2" fill="#1C2029"/>`
}
// Facts
const facts = [
  ['os', 'NivaroOS · Debian / Ubuntu · amd64 · arm64'],
  ['desktop', 'windowed web desktop · widgets · 3 styles'],
  ['apps', 'App Store · compose editor · container shells'],
  ['vms', 'KVM / QEMU · console in the browser · Host Desktop'],
  ['files', 'drives · cloud · phones · Trash · tabs'],
  ['backup', 'Backup & Sync · versions · phone backup'],
  ['remote', 'Tailscale · Cloudflare tunnel · watchdog'],
  ['phone', 'Android companion · widgets · terminal'],
  ['license', 'Apache-2.0'],
]
facts.forEach(([k, v], i) => {
  const y = 118 + i * 26
  text(262, y, k, 15, C.key, monoB)
  text(262 + 9 * 9.07, y, v, 15, C.text)
})
// Palette
;['#0F1115', '#3A3F4B', '#D93036', '#0EA371', '#E3A008', '#2563EB', '#7FA6FF', '#7DF9C5', '#F4F2EE'].forEach((c, i) => {
  body += `<circle cx="${270 + i * 26}" cy="${H - 34}" r="8" fill="${c}"${c === '#0F1115' ? ` stroke="${C.edge}" stroke-width="1.5"` : ''}/>`
})
fs.writeFileSync(out, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${W} ${H}" width="${W}" height="${H}" role="img" aria-label="nivarofetch: NivaroOS at a glance"><title>nivarofetch — NivaroOS at a glance</title>${body}</svg>\n`)
console.log(out, fs.statSync(out).size)
