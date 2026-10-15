(() => {
  // Samples the streamed desktop canvas every animation frame:
  //  - fps: distinct changes of the ffplay frame-counter patch (screen 600..760,100..140)
  //  - lat: when the latency square (screen 0..400,0..400) turns white / black
  const pick = () => [...document.querySelectorAll('canvas')].sort((a, b) => b.width * b.height - a.width * a.height)[0];
  const t = document.createElement('canvas'); t.width = 160; t.height = 41;
  const tc = t.getContext('2d', { willReadFrequently: true });
  const M = window.__m = { changes: 0, frames: 0, last: '', sq: -1, waiting: null };
  const off = () => { const c = pick(); const r = c.getBoundingClientRect(); return { c, sx: c.width / r.width }; };
  function tick() {
    const { c } = off();
    try {
      tc.drawImage(c, 600, 100, 160, 40, 0, 0, 160, 40);
      tc.drawImage(c, 200, 200, 1, 1, 0, 40, 1, 1);
      const d = tc.getImageData(0, 0, 160, 41).data;
      let h = 0; for (let i = 0; i < 160 * 40 * 4; i += 8) h = (h * 31 + d[i]) | 0;
      const k = String(h); M.frames++;
      if (k !== M.last) { M.changes++; M.last = k; }
      const p = d[160 * 40 * 4];
      M.sq = p;
      if (M.waiting && (M.waiting.want ? p > 200 : p < 50)) { M.waiting.res(performance.now() - M.waiting.t0); M.waiting = null; }
    } catch (e) { M.err = String(e); }
    requestAnimationFrame(tick);
  }
  requestAnimationFrame(tick);
  window.__wait = (want) => new Promise(res => { M.waiting = { want, t0: performance.now(), res }; setTimeout(() => { if (M.waiting) { M.waiting = null; res(-1); } }, 3000); });
  const c = pick(); return { canvases: document.querySelectorAll('canvas').length, w: c.width, h: c.height, rect: JSON.stringify(c.getBoundingClientRect()) };
})()
