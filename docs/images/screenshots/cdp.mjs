// Minimal Chrome DevTools Protocol driver: no puppeteer, just node's WebSocket.
import { spawn } from 'node:child_process';
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

export async function launch(port = 9333) {
  const dir = mkdtempSync(join(process.env.SHOTS_TMP || tmpdir(), 'chrome-'));
  const proc = spawn('google-chrome', ['--headless=new', '--no-sandbox', '--disable-gpu', '--hide-scrollbars',
    `--remote-debugging-port=${port}`, `--user-data-dir=${dir}`, '--font-render-hinting=none', 'about:blank'], { stdio: 'ignore' });
  process.on('exit', () => proc.kill());
  let targets;
  for (let i = 0; i < 100 && !targets; i++) {
    await new Promise(r => setTimeout(r, 100));
    targets = await fetch(`http://127.0.0.1:${port}/json/list`).then(r => r.json()).catch(() => null);
  }
  const ws = new WebSocket(targets.find(t => t.type === 'page').webSocketDebuggerUrl);
  await new Promise(r => ws.addEventListener('open', r, { once: true }));
  let id = 0; const pending = new Map(); const listeners = [];
  ws.addEventListener('message', e => {
    const m = JSON.parse(e.data);
    if (m.id && pending.has(m.id)) { const p = pending.get(m.id); pending.delete(m.id); m.error ? p.rej(new Error(m.error.message)) : p.res(m.result); }
    else listeners.forEach(l => l(m));
  });
  const send = (method, params = {}) => new Promise((res, rej) => { const i = ++id; pending.set(i, { res, rej }); ws.send(JSON.stringify({ id: i, method, params })); });
  const page = {
    send, on: l => listeners.push(l),
    async eval(expr) {
      const r = await send('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true });
      if (r.exceptionDetails) throw new Error(r.exceptionDetails.exception?.description || r.exceptionDetails.text);
      return r.result.value;
    },
    async shot() { return Buffer.from((await send('Page.captureScreenshot', { format: 'png' })).data, 'base64'); },
    close() { ws.close(); proc.kill(); },
  };
  await send('Page.enable'); await send('Runtime.enable');
  return page;
}
