// The README shots: each boots the desktop, then opens windows on it.
const W = {
  files: { id: 'files', title: 'Files', component: 'FilesApp' },
  store: { id: 'appstore', title: 'App Store', component: 'AppStoreApp' },
  settings: { id: 'settings', title: 'Settings', component: 'SettingsApp' },
  backup: { id: 'backup', title: 'Backup & Sync', component: 'BackupApp' },
  downloads: { id: 'download-station', title: 'Download Station', component: 'DownloadStationApp' },
  vms: { id: 'vms', title: 'VMs', component: 'VmManagerApp' },
  terminal: { id: 'terminal', title: 'Terminal', component: 'TerminalPanel' },
};
const open = (page, w, rect) => page.eval(`open_(${JSON.stringify(w)}, ${JSON.stringify(rect)})`);
// Finds the first component whose options pass `test` (a JS expression on o).
const withVm = (page, test, body) => page.eval(`(() => { let hit; const walk = vm => { const o = vm.$options; if (!hit && (${test})) hit = vm; vm.$children.forEach(walk); }; walk($root); if (!hit) throw new Error('no vm: ${test.replace(/'/g, '')}'); return (${body})(hit); })()`);

const G = '\x1b[1;32m', B = '\x1b[1;34m', D = '\x1b[2m', C = '\x1b[36m', Y = '\x1b[33m', R = '\x1b[0m';
const prompt = `${G}demo@nivaro${R}:${B}~${R}$ `;
const TERMINAL = [
  `${prompt}docker ps --format 'table {{.Names}}\\t{{.Status}}\\t{{.Ports}}'`,
  `NAMES           STATUS                  PORTS`,
  `jellyfin        Up 4 days (healthy)     0.0.0.0:8096->8096/tcp`,
  `immich-server   Up 4 days (healthy)     0.0.0.0:2283->2283/tcp`,
  `nextcloud       Up 4 days               0.0.0.0:10081->80/tcp`,
  `adguardhome     Up 4 days               0.0.0.0:53->53/udp, 0.0.0.0:3000->3000/tcp`,
  `vaultwarden     Up 2 days (healthy)     0.0.0.0:8000->80/tcp`,
  `qbittorrent     Up 9 hours              0.0.0.0:8181->8181/tcp`,
  ``,
  `${prompt}df -h /DATA/media /DATA/backup`,
  `Filesystem      Size  Used Avail Use% Mounted on`,
  `/dev/sdb1       7.3T  4.1T  3.2T  57% /DATA/media`,
  `/dev/sdc1       3.6T  1.2T  2.4T  34% /DATA/backup`,
  ``,
  `${prompt}systemctl status nivaroos-core --no-pager | head -4`,
  `${G}●${R} nivaroos-core.service - NivaroOS core`,
  `     Loaded: loaded (/usr/lib/systemd/system/nivaroos-core.service; ${G}enabled${R})`,
  `     Active: ${G}active (running)${R} since Sat 2026-10-03 09:12:44 UTC; 4 days ago`,
  `   Main PID: 1183 (nivaroos-core)`,
  ``,
  `${prompt}ip -br addr show enp3s0`,
  `enp3s0           ${G}UP${R}             ${C}192.168.1.20/24${R} fe80::a00:27ff:fe4e:66a1/64`,
  ``,
  `${prompt}`,
].join('\r\n');

const hero = async (page, wait) => {
  await open(page, W.store, { x: 230, y: 36, width: 900, height: 600 });
  await wait(800);
  await open(page, W.files, { x: 520, y: 330, width: 730, height: 540 });
};
// Single-app shots are cropped to their window plus a strip of desktop.
const CROP = [140, 20, 1320, 900];
// One app, centred, big enough to read once the shot is shown at half size.
const single = (w, rect = {}) => async (page, wait) => { await open(page, w, { x: 160, y: 40, width: 1280, height: 860, ...rect }); await wait(1500); };
export default {
  'desktop-light': { theme: 'light', run: hero },
  'desktop-dark': { theme: 'dark', run: hero },
  settings: { theme: 'light', run: single(W.settings), crop: CROP },
  backup: { theme: 'light', run: single(W.backup), crop: CROP },
  downloads: { theme: 'light', run: single(W.downloads), crop: CROP },
  'download-browser': { theme: 'light', crop: CROP, run: async (page, wait) => {
    await single(W.downloads)(page, wait);
    await withVm(page, "o.name === 'download-station-app'", "vm => { vm.browserStarted = true; vm.activeSection = 'browser' }");
    await wait(1500);
    await withVm(page, "o.name === 'ds-browser'", "vm => vm.$refs.impl.openUrl('https://downloads.example.com/linux/', false)");
    await wait(2500);
  } },
  vms: { theme: 'dark', run: single(W.vms), crop: CROP },
  terminal: { theme: 'dark', crop: [180, 60, 1080, 740], run: async (page, wait) => {
    await single(W.terminal, { x: 200, y: 80, width: 1040, height: 700 })(page, wait);
    await wait(1500);
    await withVm(page, "o.methods && o.methods.createTerm", `vm => { vm.attach = () => {}; vm.createError = null; vm.connState = 'idle'; vm.pillText = ''; vm.term.reset(); vm.term.write(${JSON.stringify(TERMINAL)}); }`);
  } },
};
