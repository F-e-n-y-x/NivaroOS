// node bench.mjs <label> <url> <serverPids,comma> <ssPort> <secs> [lat=1] [shots=1]
import { spawn, execSync } from 'node:child_process';
import { readFileSync, writeFileSync, mkdirSync, rmSync } from 'node:fs';
const [label, url, pidsArg, ssPort, secsArg, latArg = '1', shotsArg = '1'] = process.argv.slice(2);
const secs = +secsArg; let pids;
const dbg = 9300 + Math.floor(Math.random() * 500);
const prof = `/root/nivaroos-wip/hostdesk-alt-proto/prof-${dbg}`;
const chrome = spawn('google-chrome', ['--headless=new', '--no-sandbox', '--ignore-certificate-errors', `--remote-debugging-port=${dbg}`, `--user-data-dir=${prof}`,
  '--window-size=3398,1220', '--force-device-scale-factor=1', '--no-first-run', '--disable-extensions',
  '--autoplay-policy=no-user-gesture-required', 'about:blank'], { stdio: 'ignore' });
const sleep = ms => new Promise(r => setTimeout(r, ms));
let list; for (let i = 0; i < 50; i++) { try { list = await (await fetch(`http://127.0.0.1:${dbg}/json/list`)).json(); break; } catch { await sleep(200); } }
const ws = new WebSocket(list.find(t => t.type === 'page').webSocketDebuggerUrl);
let id = 0; const pend = new Map();
const send = (method, params = {}) => new Promise(res => { const i = ++id; pend.set(i, res); ws.send(JSON.stringify({ id: i, method, params })); setTimeout(() => { if (pend.has(i)) { pend.delete(i); console.error('cdp timeout', method); res({ result: { value: -1 } }); } }, 6000); });
ws.onmessage = ev => { const m = JSON.parse(ev.data); if (m.id && pend.has(m.id)) { pend.get(m.id)(m.result ?? m); pend.delete(m.id); } else if (m.method === 'Runtime.exceptionThrown') console.error('[exc]', JSON.stringify(m.params.exceptionDetails).slice(0, 300)); };
await new Promise(r => ws.onopen = r);
await send('Runtime.enable'); await send('Page.enable');
await send('Emulation.setDeviceMetricsOverride', { width: 3398, height: 1220, deviceScaleFactor: 1, mobile: false });
await send('Page.navigate', { url });
await sleep(10000);
const ev = async (expression) => (await send('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true })).result?.value;
console.error('stage nav', Date.now());
const info = await ev(readFileSync('/root/nivaroos-wip/hostdesk-alt-proto/measure.js', 'utf8'));
const cpu = p => { try { const f = readFileSync(`/proc/${p}/stat`, 'utf8').split(') ')[1].split(' '); return (+f[11] + +f[12]) / 100; } catch { return 0; } };
const chromeCpu = () => { try { return execSync(`pgrep -f -- '--user-data-dir=${prof}'`).toString().trim().split('\n').reduce((s, p) => s + cpu(p), 0); } catch { return 0; } };
const sent = () => { if (ssPort === 'rtc') return 0; const o = execSync(`ss -tin state established '( sport = :${ssPort} )'`).toString(); return [...o.matchAll(/bytes_sent:(\d+)/g)].reduce((s, m) => s + +m[1], 0); };
const enc = () => { try { return execSync('nvidia-smi --query-gpu=encoder.stats.sessionCount,utilization.encoder --format=csv,noheader').toString().trim(); } catch { return '?'; } };
// steady-state window
pids = pidsArg.split(',').filter(Boolean).flatMap(p => p.startsWith('@') ? execSync(`pgrep -f -- '${p.slice(1)}' || true`).toString().trim().split('\n').filter(Boolean) : [p]);
const c0 = pids.map(cpu), cc0 = chromeCpu(), b0 = sent(), m0 = await ev('({...window.__m})'), t0 = Date.now(), e0 = enc();
console.error('stage window', Date.now());
await sleep(secs * 1000);
console.error('stage window-end', Date.now());
const c1 = pids.map(cpu), cc1 = chromeCpu(), b1 = sent(), m1 = await ev('({...window.__m, waiting:null})'), dt = (Date.now() - t0) / 1000, e1 = enc();
const rb = ssPort === 'rtc' ? ((m1.rtcBytes ?? 0) - (m0.rtcBytes ?? 0)) : 0;
const out = { label, info, secs: dt, server_cpu_pct: pids.map((p, i) => +(100 * (c1[i] - c0[i]) / dt).toFixed(1)), chrome_cpu_pct: +(100 * (cc1 - cc0) / dt).toFixed(1),
  mbit_s: +(((b1 - b0) + rb) * 8 / dt / 1e6).toFixed(2), pids, fps_seen: +((m1.changes - m0.changes) / dt).toFixed(1), raf_fps: +((m1.frames - m0.frames) / dt).toFixed(1), nvenc: [e0, e1], err: m1.err };
if (latArg === '1') {
  const lat = []; await send('Input.dispatchMouseEvent', { type: 'mouseReleased', x: 200, y: 200, button: 'left', clickCount: 1 }); await sleep(500);
  for (let i = 0; i < (+process.env.N || 20); i++) {
    for (const [type, want] of [['mousePressed', true], ['mouseReleased', false]]) {
      const w = send('Runtime.evaluate', { expression: `window.__wait(${want})`, returnByValue: true, awaitPromise: true });
      await sleep(5);
      await send('Input.dispatchMouseEvent', { type, x: 200, y: 200, button: 'left', clickCount: 1 });
      lat.push((await w).result.value); if (process.env.DBG) console.error(Date.now()%100000, type, lat.at(-1), await ev('window.__m.sq')); await sleep(150);
    }
  }
  const ok = lat.filter(x => x >= 0).sort((a, b) => a - b);
  out.latency_ms = { n: ok.length, fail: lat.length - ok.length, p50: +ok[Math.floor(ok.length / 2)]?.toFixed(1), p90: +ok[Math.floor(ok.length * 0.9)]?.toFixed(1), min: +ok[0]?.toFixed(1) };
}
console.error('stage shots', Date.now(), JSON.stringify(out));
if (shotsArg === '1') {
  const dir = `/root/nivaroos-wip/hostdesk-alt-proto/shots-${label}`; rmSync(dir, { recursive: true, force: true }); mkdirSync(dir, { recursive: true });
  for (let i = 0; i < 8; i++) { const d = await ev(`(() => { const c = [...document.querySelectorAll('canvas')].sort((a, b) => b.width * b.height - a.width * a.height)[0]; const t = document.createElement('canvas'); t.width = 1280; t.height = 720; t.getContext('2d').drawImage(c, 600, 100, 1280, 720, 0, 0, 1280, 720); return t.toDataURL('image/png'); })()`); writeFileSync(`${dir}/${i}.png`, Buffer.from(d.split(',')[1], 'base64')); await sleep(70); }
}
console.log(JSON.stringify(out));
chrome.kill('SIGTERM'); await sleep(1000); rmSync(prof, { recursive: true, force: true }); process.exit(0);
