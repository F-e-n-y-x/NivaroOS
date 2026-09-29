// The README's UPI support card: a crisp vector QR carrying the owner's
// exact Google Pay/UPI payload, in the NivaroOS card style (graphite, the
// Ni mark, outlined Geist). Scanners need dark modules on a light field,
// so the QR sits on paper white.
//   node gen-support.js <fonts dir> <out.svg>
const opentype = require('opentype.js'), QR = require('qrcode'), fs = require('fs'), path = require('path')
const [fontsDir = '../../mobile/assets/fonts', out = 'nivaroos-support-upi.svg'] = process.argv.slice(2)
const PAYLOAD = 'upi://pay?pa=ayushsoni2911@okaxis&pn=Ayush%20Soni&aid=uGICAgIDQlsPbRQ'
const UPI_ID = 'ayushsoni2911@okaxis'
const sans = opentype.loadSync(path.join(fontsDir, 'Geist-400.ttf'))
const sansB = opentype.loadSync(path.join(fontsDir, 'Geist-600.ttf'))
const mono = opentype.loadSync(path.join(fontsDir, 'GeistMono-500.ttf'))
const C = { bg: '#0F1115', edge: '#262A33', text: '#E6E9EF', dim: '#8A909C', blue: '#2563EB', mint: '#7DF9C5', paper: '#F4F2EE', ink: '#0F1115' }
const W = 760, H = 300
let body = ''
const width = (font, s, size) => [...s].reduce((a, ch) => a + font.charToGlyph(ch).advanceWidth * size / font.unitsPerEm, 0)
function text(x, y, s, size, fill, font = sans) {
  let d = '', cx = x
  for (const ch of s) { const g = font.charToGlyph(ch); d += g.getPath(cx, y, size).toPathData(1); cx += g.advanceWidth * size / font.unitsPerEm }
  body += `<path d="${d}" fill="${fill}"/>`
  return cx
}
body += `<rect x="1" y="1" width="${W - 2}" height="${H - 2}" rx="18" fill="${C.bg}" stroke="${C.edge}" stroke-width="2"/>`
// QR on paper
const qr = QR.create(PAYLOAD, { errorCorrectionLevel: 'M' })
const n = qr.modules.size, quiet = 3, box = 236, bx = 32, by = (H - box) / 2
const cell = box / (n + quiet * 2)
body += `<rect x="${bx}" y="${by}" width="${box}" height="${box}" rx="14" fill="${C.paper}"/>`
let d = ''
for (let r = 0; r < n; r++) for (let c = 0; c < n; c++) {
  if (qr.modules.get(r, c)) { const x = bx + (c + quiet) * cell, y = by + (r + quiet) * cell; d += `M${x.toFixed(2)} ${y.toFixed(2)}h${cell.toFixed(2)}v${cell.toFixed(2)}h-${cell.toFixed(2)}z` }
}
body += `<path d="${d}" fill="${C.ink}" shape-rendering="crispEdges"/>`
// Right side
const tx = bx + box + 40
// small Ni mark
const m = 30 / 256
body += `<g transform="translate(${tx} 58) scale(${m})"><rect width="256" height="256" rx="60" fill="${C.blue}"/><path d="M80 194V110L176 194V110" fill="none" stroke="#fff" stroke-width="32" stroke-linecap="round" stroke-linejoin="round"/><circle cx="176" cy="62" r="17" fill="${C.mint}"/></g>`
text(tx + 42, 80, 'Support NivaroOS', 24, C.text, sansB)
text(tx, 122, 'NivaroOS is free and open source, built in spare time.', 15, C.dim)
text(tx, 144, 'If it saves you money or time, a small UPI tip helps.', 15, C.dim)
text(tx, 186, 'UPI ID', 13, C.dim, sansB)
body += `<rect x="${tx - 2}" y="196" width="${width(mono, UPI_ID, 19) + 26}" height="36" rx="9" fill="#171A20" stroke="${C.edge}"/>`
text(tx + 11, 221, UPI_ID, 19, C.mint, mono)
text(tx, 262, 'Scan with any UPI app: Google Pay, PhonePe, Paytm, BHIM', 13, C.dim)
fs.writeFileSync(out, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${W} ${H}" width="${W}" height="${H}" role="img" aria-label="Support NivaroOS by UPI: ${UPI_ID}"><title>Support NivaroOS - UPI ${UPI_ID}</title>${body}</svg>\n`)
console.log(out, fs.statSync(out).size, 'modules', n)
