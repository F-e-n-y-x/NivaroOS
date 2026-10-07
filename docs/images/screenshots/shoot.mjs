// Renders the real web UI (the production build) against fake API data in
// headless Chrome and saves the README screenshots. Nothing talks to a real
// server: every /v1, /v2 request is answered from fixtures.mjs.
//   (cd ui && pnpm install && pnpm build)
//   node docs/images/screenshots/shoot.mjs [only-this-shot]
import http from 'node:http'; import fs from 'node:fs'; import path from 'node:path';
import { execFileSync } from 'node:child_process';
import { launch } from './cdp.mjs';
import { answer, SOCKET, vmPreview, MIRROR_PAGE } from './fixtures.mjs';

const WWW = 'ui/build/sysroot/var/lib/nivaroos/www', OUT = 'docs/images/screenshots/web', PORT = 18765;
const TYPES = { '.js': 'text/javascript', '.css': 'text/css', '.html': 'text/html', '.svg': 'image/svg+xml', '.png': 'image/png', '.woff2': 'font/woff2', '.woff': 'font/woff', '.ttf': 'font/ttf' };
const server = http.createServer(async (req, res) => {
  const u = new URL(req.url, 'http://x');
  const shot = u.pathname.match(/^\/v1\/vm-sidecar\/vms\/([^/]+)\/screenshot$/);
  if (shot) { const png = vmPreview(decodeURIComponent(shot[1])); res.statusCode = png ? 200 : 404; res.setHeader('content-type', 'image/png'); return res.end(png); }
  if (/^\/v[12]\//.test(u.pathname)) {
    let body = ''; for await (const c of req) body += c;
    const r = answer(req.method, u.pathname, u.searchParams, body);
    if (process.env.DEBUG) console.log(req.method, req.url, r === undefined ? '(default)' : '');
    res.setHeader('content-type', 'application/json');
    return res.end(JSON.stringify(r === undefined ? { success: 200, message: 'ok', data: null } : r));
  }
  let f = path.join(WWW, u.pathname === '/' ? 'index.html' : u.pathname);
  if (!fs.existsSync(f) || fs.statSync(f).isDirectory()) f = path.join(WWW, 'index.html');
  res.setHeader('content-type', TYPES[path.extname(f)] || 'application/octet-stream');
  fs.createReadStream(f).pipe(res);
}).listen(PORT);

const wait = ms => new Promise(r => setTimeout(r, ms));
const page = await launch(9477);
const ORIGIN = `http://127.0.0.1:${PORT}`;
// Every request leaving the page is checked: our own server and public CDNs
// (app icons) pass, the Download Station sidecar's origin gets the fake page,
// anything else on this machine is refused - real services must never answer.
page.on(async m => {
  if (m.method !== 'Fetch.requestPaused') return;
  const { requestId, request: { url } } = m.params;
  if (url.startsWith(ORIGIN) || url.startsWith('https://') || url.startsWith('data:')) return page.send('Fetch.continueRequest', { requestId });
  if (url.startsWith('http://127.0.0.1:28642/')) return page.send('Fetch.fulfillRequest', { requestId, responseCode: 200,
    responseHeaders: [{ name: 'content-type', value: 'text/html; charset=utf-8' }], body: Buffer.from(MIRROR_PAGE).toString('base64') });
  page.send('Fetch.failRequest', { requestId, errorReason: 'BlockedByClient' });
});
await page.send('Fetch.enable', { patterns: [{ urlPattern: '*' }] });
page.on(m => { if (process.env.DEBUG && m.method === 'Runtime.consoleAPICalled' && m.params.type === 'error') console.log('console.error', JSON.stringify(m.params.args.map(a => a.value || a.description)).slice(0, 400)); });

// Runs in the page: the Vue root, plus a way to replay socket.io events into
// every component that listens for them (the widgets' live data).
const PRELUDE = `
  window.$root = document.querySelector('#app').__vue__;
  window.emit = (ev, data) => { const walk = vm => { const s = vm.$options.sockets; if (s && s[ev]) s[ev].call(vm, data); vm.$children.forEach(walk); }; walk($root); };
  window.open_ = (w, rect) => { $root.$store.commit('OPEN_WINDOW', w); if (rect) Object.assign($root.$store.state.windows.find(x => x.id === w.id), rect); };
`;

async function boot(theme) {
  await page.send('Emulation.setDeviceMetricsOverride', { width: 1600, height: 1000, deviceScaleFactor: 2, mobile: false });
  await page.send('Emulation.setEmulatedMedia', { features: [{ name: 'prefers-color-scheme', value: theme }] });
  await page.send('Page.navigate', { url: `http://127.0.0.1:${PORT}/#/login` });
  await wait(800);
  await page.eval(`localStorage.clear(); Object.entries(${JSON.stringify({
    access_token: 'demo', refresh_token: 'demo', version: 'v1.0.0', lang: 'en_us', uiThemeMode: theme,
    user: JSON.stringify({ id: 1, username: 'demo', nickname: 'Demo', role: 'admin' }),
  })}).forEach(([k, v]) => localStorage.setItem(k, v))`);
  await page.send('Page.navigate', { url: `http://127.0.0.1:${PORT}/#/` });
  await page.send('Page.reload');
  await wait(3500);
  await page.eval(PRELUDE);
  for (let i = 0; i < 24; i++) { await page.eval(`emit('nivaroos:system:utilization', ${JSON.stringify(SOCKET.utilization(i))})`); await wait(60); }
}

async function save(name, crop) { // crop: [x, y, w, h] in CSS px
  await wait(1200);
  const raw = path.join(OUT, name + '.raw.png');
  fs.writeFileSync(raw, await page.shot());
  // 2x capture -> 1x (cropped to the window, if asked), lossy WebP (text stays crisp at q90).
  const [x, y, w, h] = crop || [0, 0, 1600, 1000];
  execFileSync('magick', [raw, '-crop', `${w * 2}x${h * 2}+${x * 2}+${y * 2}`, '+repage', '-filter', 'Lanczos', '-resize', `${w}x${h}`, '-quality', '90', '-define', 'webp:method=6', path.join(OUT, name + '.webp')]);
  fs.unlinkSync(raw);
  console.log('saved', name);
}

const SHOTS = (await import('./scenes.mjs')).default;
const only = process.argv[2];
for (const [name, scene] of Object.entries(SHOTS)) {
  if (only && !name.startsWith(only)) continue;
  await boot(scene.theme || 'light');
  await scene.run(page, wait);
  await save(name, scene.crop);
}
page.close(); server.close(); process.exit(0);
