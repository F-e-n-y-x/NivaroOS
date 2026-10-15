// node conn.mjs <url> [reps=5] [watchSecs=0]
// Connect time: navigate -> first frame whose pixel at (700,150) (testsrc2 red bar) is non-black
// on the largest canvas. With watchSecs>0 it then keeps the page open and logs every gap
// (no new frame for >1 s) so a server kill/restart shows whether the client recovers by itself.
import { spawn } from 'node:child_process';
import { rmSync } from 'node:fs';
const [url, repsArg = '5', watchArg = '0'] = process.argv.slice(2);
const dbg = 9800 + Math.floor(Math.random() * 150), prof = `/root/nivaroos-wip/hostdesk-alt-proto/run/prof-${dbg}`;
const chrome = spawn('google-chrome', ['--headless=new', '--no-sandbox', `--remote-debugging-port=${dbg}`, `--user-data-dir=${prof}`,
  '--window-size=3398,1220', '--no-first-run', '--autoplay-policy=no-user-gesture-required', 'about:blank'], { stdio: 'ignore' });
const sleep = ms => new Promise(r => setTimeout(r, ms));
let list; for (let i = 0; i < 50; i++) { try { list = await (await fetch(`http://127.0.0.1:${dbg}/json/list`)).json(); break; } catch { await sleep(200); } }
const ws = new WebSocket(list.find(t => t.type === 'page').webSocketDebuggerUrl);
let id = 0; const pend = new Map();
const send = (method, params = {}) => new Promise(res => { const i = ++id; pend.set(i, res); ws.send(JSON.stringify({ id: i, method, params })); });
ws.onmessage = ev => { const m = JSON.parse(ev.data); if (m.id && pend.has(m.id)) { pend.get(m.id)(m.result); pend.delete(m.id); } };
await new Promise(r => ws.onopen = r);
await send('Page.enable');
const ev = async e => (await send('Runtime.evaluate', { expression: e, returnByValue: true })).result?.value;
const probe = `(() => { const c = [...document.querySelectorAll('canvas')].sort((a, b) => b.width * b.height - a.width * a.height)[0];
  if (!c || c.width < 800) return 0; const t = document.createElement('canvas'); t.width = t.height = 1;
  t.getContext('2d').drawImage(c, 700, 150, 1, 1, 0, 0, 1, 1); const d = t.getContext('2d').getImageData(0, 0, 1, 1).data; return d[0] + d[1] + d[2]; })()`;
const times = [];
for (let r = 0; r < +repsArg; r++) {
  await send('Page.navigate', { url: 'about:blank' }); await sleep(500);
  const t0 = Date.now(); await send('Page.navigate', { url });
  let t = -1; while (Date.now() - t0 < 15000) { if ((await ev(probe)) > 0) { t = Date.now() - t0; break; } await sleep(10); }
  times.push(t);
}
const out = { url, connect_ms: times };
if (+watchArg > 0) {
  // frame hash of the testsrc2 counter patch; a gap = same hash for >1 s
  const hash = `(() => { const c = [...document.querySelectorAll('canvas')].sort((a, b) => b.width * b.height - a.width * a.height)[0];
    const t = document.createElement('canvas'); t.width = 160; t.height = 40; const x = t.getContext('2d'); x.drawImage(c, 600, 100, 160, 40, 0, 0, 160, 40);
    const d = x.getImageData(0, 0, 160, 40).data; let h = 0; for (let i = 0; i < d.length; i += 8) h = (h * 31 + d[i]) | 0; return h; })()`;
  let last = null, lastT = Date.now(); const gaps = []; const end = Date.now() + watchArg * 1000; let inGap = null;
  while (Date.now() < end) {
    const h = await ev(hash);
    if (h !== last) { if (inGap) { gaps.push({ at_s: +((inGap - (end - watchArg * 1000)) / 1000).toFixed(1), frozen_ms: Date.now() - inGap }); inGap = null; } last = h; lastT = Date.now(); }
    else if (!inGap && Date.now() - lastT > 1000) inGap = lastT;
    await sleep(50);
  }
  out.gaps = gaps; out.stuck_at_end = !!inGap;
}
console.log(JSON.stringify(out));
chrome.kill('SIGTERM'); await sleep(800); rmSync(prof, { recursive: true, force: true }); process.exit(0);
