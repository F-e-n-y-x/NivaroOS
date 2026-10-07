// Fake API answers for shoot.mjs. All data is made up: example hostnames,
// 192.168.1.x addresses, demo user. Keep it that way.
const ok = data => ({ success: 200, message: 'ok', data });
const GiB = 1024 ** 3, TiB = 1024 ** 4;
const ICON = n => `https://cdn.jsdelivr.net/gh/IceWhaleTech/CasaOS-AppStore@main/Apps/${n}/icon.png`;

const apps = [
  ['jellyfin', 'Jellyfin', 'Jellyfin', 8096], ['immich', 'Immich', 'Immich', 2283], ['nextcloud', 'Nextcloud', 'Nextcloud', 10081],
  ['homeassistant', 'Home Assistant', 'HomeAssistant', 8123], ['adguard', 'AdGuard Home', 'AdGuardHome', 3000],
  ['vaultwarden', 'Vaultwarden', 'Vaultwarden', 8000], ['qbittorrent', 'qBittorrent', 'qBittorrent', 8181],
  ['navidrome', 'Navidrome', 'Navidrome', 4533], ['photoprism', 'PhotoPrism', 'PhotoPrism', 2342],
].map(([id, title, icon, port], i) => ({ app_type: 'v2app', name: id, store_app_id: id, title: { en_us: title }, icon: ICON(icon),
  status: 'running', port: String(port), hostname: '192.168.1.20', scheme: 'http', index: '/' }));

const cpu = i => ({ percent: 18 + 9 * Math.sin(i / 3) + (i % 5), percpu: Array.from({ length: 12 }, (_, k) => 10 + ((k * 7 + i * 3) % 30)),
  num: 12, temperature: 47, mhz: 4200, model: 'amd', model_name: 'AMD Ryzen 5 5600X 6-Core Processor', power: null });
const mem = { total: 32 * GiB, available: 18.6 * GiB, used: 13.4 * GiB, usedPercent: 41.9, swapUsed: 0 };
const net = i => [{ name: 'enp3s0', state: 'up', time: 1760000000 + i * 3,
  bytesRecv: 8.2e12 + i * 3 * 2.4e6 * (1 + 0.7 * Math.sin(i / 2.2)), bytesSent: 1.3e12 + i * 3 * 3.2e5 * (1 + 0.6 * Math.cos(i / 3)) }];

export const SOCKET = {
  utilization: i => ({ Properties: { sys_cpu: JSON.stringify(cpu(i)), sys_mem: JSON.stringify(mem), sys_net: JSON.stringify(net(i)) } }),
};

const SHOT = n => `https://cdn.jsdelivr.net/gh/IceWhaleTech/CasaOS-AppStore@main/Apps/${n}/screenshot-1.png`;
const catalog = [
  ['Jellyfin', 'Media', 'The free software media system', 'Jellyfin'], ['Immich', 'Gallery', 'Self-hosted photo and video backup', 'Immich'],
  ['Nextcloud', 'Cloud', 'Files, calendars and contacts on your own server', 'Nextcloud'], ['Syncthing', 'Backup', 'Continuous peer-to-peer file sync', 'Syncthing'],
  ['Gitea', 'Developer', 'A painless self-hosted Git service', 'Gitea'], ['Grafana', 'Utilities', 'Dashboards for every metric you collect', 'Grafana'],
  ['Portainer', 'Developer', 'Container management made easy', 'Portainer'], ['Navidrome', 'Media', 'Your personal music streaming server', 'Navidrome'],
  ['Memos', 'Utilities', 'A lightweight, self-hosted memo hub', 'Memos'], ['AdGuardHome', 'Network', 'Network-wide ad and tracker blocking', 'AdGuard Home'],
  ['Vaultwarden', 'Utilities', 'Bitwarden-compatible password server', 'Vaultwarden'], ['qBittorrent', 'Downloader', 'A light, open-source BitTorrent client', 'qBittorrent'],
  ['PhotoPrism', 'Gallery', 'AI-powered photo app for the decentralized web', 'PhotoPrism'], ['Audiobookshelf', 'Media', 'Self-hosted audiobook and podcast server', 'Audiobookshelf'],
  ['Emby', 'Media', 'Personal media server for your collection', 'Emby'], ['2FAuth', 'Utilities', 'Manage your two-factor authentication codes', '2FAuth'],
];
const storeList = Object.fromEntries(catalog.map(([id, category, tagline, title]) => [id.toLowerCase(), { category, icon: ICON(id),
  tagline: { en_us: tagline }, title: { en_us: title }, author: 'Community', screenshot_link: [SHOT(id)], architectures: ['amd64', 'arm64'] }]));
const cats = [...new Set(catalog.map(c => c[1]))].map((name, id) => ({ id, name, count: catalog.filter(c => c[1] === name).length }));

const now = Date.parse('2026-10-05T18:30:00Z');
const entry = (dir, name, isDir, size, ago) => ({ name, path: `${dir}/${name}`.replace('//', '/'), is_dir: isDir, size, write: true,
  date: new Date(now - ago * 3600e3).toISOString(), extensions: {} });
const folders = {
  '/DATA': [['AppData', 1, 0, 2], ['Backups', 1, 0, 30], ['Documents', 1, 0, 5], ['Downloads', 1, 0, 1], ['Media', 1, 0, 48], ['Photos', 1, 0, 9],
    ['Projects', 1, 0, 70], ['budget-2026.xlsx', 0, 48e3, 20], ['debian-13.1.0-amd64-netinst.iso', 0, 783e6, 26], ['family-trip.mp4', 0, 1.9e9, 96],
    ['notes.md', 0, 6.2e3, 3], ['router-config.tar.gz', 0, 2.4e6, 200], ['setup-guide.pdf', 0, 3.1e6, 120]],
};
const listing = dir => (folders[dir] || folders['/DATA']).map(([n, d, sz, ago]) => entry(dir, n, !!d, sz, ago));

const disk = (mount_point, label, kind, size, used) => ({ mount_point, label, kind, size_bytes: size, used_bytes: used, avail_bytes: size - used, percent: Math.round(used / size * 100) });
const custom = {
  system: { lang: 'en_us', recommend_switch: true, existing_apps_switch: true },
  dock_pinned_apps: ['Files', 'App Store', 'Terminal', 'VMs', 'Backup & Sync', 'Download Station', 'Settings'],
};

// Backup & Sync: the frozen API contract's own example responses, plus two more jobs.
import fs from 'node:fs';
const spec = JSON.parse(fs.readFileSync(new URL('../../specs/backup-api.json', import.meta.url)));
const specRoutes = spec.endpoints.map(e => ({ e, re: new RegExp('^/v1/backup' + e.path.replace(/:[a-z_]+/g, '[^/]+') + '$') }));
const jobsExample = spec.endpoints.find(e => e.method === 'GET' && e.path === '/jobs').response;
const job0 = jobsExample[0];
const jobs = [...jobsExample,
  { ...job0, id: 'bk_docs', name: 'Documents → Cloud', type: 'backup', health: 'ok', dest: { ...job0.dest, kind: 'cloud', label: 'cloud-archive' },
    last_run: { ...job0.last_run, ended_at: '2026-10-07T02:10:00+02:00' } },
  { ...job0, id: 'bk_apps', name: 'App data → tank', type: 'backup', health: 'ok', dest: { ...job0.dest, kind: 'volume', label: 'tank' },
    last_run: { ...job0.last_run, ended_at: '2026-10-07T04:00:00+02:00' } }];

// Download Station.
const dl = (id, filename, size, downloaded, state, extra = {}) => ({ id, url: `https://downloads.example.com/${filename}`, filename, dir: '/DATA/Downloads',
  size, downloaded, state, connections: state === 'downloading' ? 8 : 0, resumable: true, created_at: '2026-10-07T18:00:00Z', ...extra });
const downloads = [
  dl('d1', 'debian-13.1.0-amd64-netinst.iso', 783e6, 412e6, 'downloading', { speed: 11.4e6, eta: 33 }),
  dl('d2', 'ubuntu-24.04.3-desktop-amd64.iso', 6.2e9, 1.9e9, 'downloading', { speed: 24.8e6, eta: 174 }),
  dl('d3', 'blender-4.5.3-linux-x64.tar.xz', 389e6, 0, 'queued'),
  dl('d4', 'big-buck-bunny-1080p.mkv', 928e6, 290e6, 'paused'),
  dl('d5', 'holiday-video.mp4', 1.4e9, 0, 'failed', { error: 'the server answered 403 Forbidden - the link may have expired' }),
  dl('d6', 'archlinux-2026.10.01-x86_64.iso', 1.2e9, 1.2e9, 'completed', { completed_at: '2026-10-07T17:40:00Z' }),
  dl('d7', 'jellyfin-backup-2026-10.tar.gz', 2.1e9, 2.1e9, 'completed', { completed_at: '2026-10-06T22:05:00Z' }),
];

// VMs.
const vm = (name, state, vcpus, memory_mib, bridge) => ({ name, state, vcpus, memory_mib, networks: [{ mode: bridge ? 'bridge' : 'nat', bridge_name: bridge || '' }] });
const vms = [vm('ubuntu-server', 'running', 4, 8192, 'br0'), vm('home-assistant', 'running', 2, 4096, 'br0'), vm('debian-dev', 'running', 2, 4096), vm('windows-test', 'shutoff', 4, 8192)];

const routes = {
  'GET /v1/users/status': () => ok({ initialized: true, key: '' }),
  'GET /v1/users/current': () => ok({ id: 1, username: 'demo', nickname: 'Demo', role: 'admin', avatar: '' }),
  'GET /v1/sys/utilization': () => ok({ cpu: cpu(0), mem, net: net(0) }),
  'GET /v1/sys/disks-usage': () => ok([
    disk('/', 'System', 'nvme', 476 * GiB, 132 * GiB), disk('/DATA/media', 'media', 'hdd', 7.3 * TiB, 4.1 * TiB),
    disk('/DATA/backup', 'backup', 'hdd', 3.6 * TiB, 1.2 * TiB)]),
  'GET /v1/sys/network-interfaces': () => ok([{ name: 'enp3s0', ip: ['192.168.1.20'], ipv4: ['192.168.1.20'] }]),
  'GET /v1/gpu/gpu-stats': () => ({ name: 'NVIDIA GeForce RTX 3060', driver_version: '550.120', utilization_percent: 12, memory_used_mib: 1840, memory_total_mib: 12288, temperature_c: 44, power_draw_w: 38, processes: [] }),
  'GET /v1/gpu/driver-status': () => ({ gpus: [{ vendor: 'nvidia', driver_working: true }] }),
  'GET /v2/app_management/web/appgrid': () => ok(apps),
  'GET /v1/backup/health': () => ({ installed: true }),
  'GET /v1/download-station/health': () => ({ installed: true }),
  'GET /v1/folder': q => ok({ content: listing(q.get('path')), total: listing(q.get('path')).length, index: 1 }),
  'GET /v2/app_management/apps': q => ok({ list: q.get('recommend') ? Object.fromEntries(Object.entries(storeList).slice(0, 5)) : storeList, installed: ['jellyfin', 'immich', 'nextcloud', 'adguardhome', 'vaultwarden', 'qbittorrent', 'navidrome', 'photoprism'] }),
  'GET /v2/app_management/categories': () => ok(cats),
  'GET /v2/app_management/appstore': () => ok([{ id: 0, url: 'https://github.com/IceWhaleTech/CasaOS-AppStore', name: 'Official' }]),
  'GET /v1/sys/hardware': () => ok({ arch: 'amd64' }),
  'GET /v1/download-station/downloads': () => downloads,
  'GET /v1/download-station/settings': () => ({ download_dir: '/DATA/Downloads', max_concurrent: 3, connections: 8, adblock_enabled: true }),
  'GET /v1/download-station/events': () => ({ events: [], next: 0 }),
  'GET /v1/download-station/rb/status': () => ({ available: false, reason: 'not_installed', install: '' }),
  'POST /v1/download-station/browser/sessions': () => ({ id: 's1', prefix: '/browse/s1/', blocked_by_host: {}, blocked_total: 0 }),
  'GET /v1/download-station/browser/sessions/s1': () => ({ id: 's1', prefix: '/browse/s1/', blocked_by_host: { 'ads.example.net': 7 }, blocked_total: 7 }),
  'GET /v1/download-station/browser/history': () => [],
  'GET /v1/vm-sidecar/setup/status': () => ({ ready: true }),
  'GET /v1/vm-sidecar/vms': () => vms,
  'GET /v1/backup/jobs': () => ok(jobs),
  'GET /v2/message_bus/notifications': () => ok([]),
};

export function answer(method, pathname, query, body) {
  const key = `${method} ${pathname}`;
  if (routes[key]) return routes[key](query, body);
  const b = specRoutes.find(r => r.e.method === method && r.re.test(pathname));
  if (b) return b.e.envelope === false ? b.e.response : ok(b.e.response);
  const c = pathname.match(/^\/v1\/users\/current\/custom\/(\w+)$/);
  if (c) return ok(method === 'GET' ? (custom[c[1]] ?? '') : JSON.parse(body || 'null'));
}

// VM console previews: plain text consoles drawn with ImageMagick.
import { execFileSync } from 'node:child_process';
const consoles = {
  'ubuntu-server': ['#300a24', ['Ubuntu 24.04.3 LTS ubuntu-server tty1', '', 'ubuntu-server login: demo', 'Password:', 'Welcome to Ubuntu 24.04.3 LTS (GNU/Linux 6.8.0-85-generic x86_64)', '', '  System load:  0.08    Processes:      141', '  Usage of /:   23.4%   Users logged in: 1', '  Memory usage: 18%     IPv4 address:   192.168.1.41', '', 'demo@ubuntu-server:~$ _']],
  'home-assistant': ['#0b1c2c', ['Welcome to Home Assistant', 'homeassistant login: root', '', '  _    _                         _          _     _              _   ', ' | |  | |   Home Assistant OS 16.2', '', '  Home Assistant URL:  http://homeassistant.local:8123', '  Observer URL:        http://homeassistant.local:4357', '', 'ha > _']],
  'debian-dev': ['#111111', ['Debian GNU/Linux 13 debian-dev tty1', '', 'debian-dev login: demo', 'Last login: Tue Oct  6 21:14:02 on tty1', 'demo@debian-dev:~$ uname -a', 'Linux debian-dev 6.12.48+deb13-amd64 #1 SMP x86_64 GNU/Linux', 'demo@debian-dev:~$ _']],
};
const previews = {};
export function vmPreview(name) {
  const c = consoles[name]; if (!c) return null;
  return previews[name] ??= execFileSync('magick', ['-size', '1024x640', `xc:${c[0]}`, '-font', 'DejaVu-Sans-Mono', '-pointsize', '30', '-fill', '#e5e5e5',
    '-annotate', '+28+52', c[1].join('\n'), 'png:-']);
}

// What the Download Station's Lite browser shows: a made-up mirror page.
export const MIRROR_PAGE = `<!doctype html><meta charset="utf-8"><title>Linux images - downloads.example.com</title>
<style>body{font:15px/1.5 system-ui,sans-serif;margin:0;color:#1f2937;background:#f8fafc}header{background:#1e3a8a;color:#fff;padding:22px 40px}
header h1{margin:0;font-size:24px}header p{margin:4px 0 0;opacity:.8}main{padding:24px 40px;max-width:900px}
table{width:100%;border-collapse:collapse;background:#fff;border-radius:10px;overflow:hidden;box-shadow:0 1px 3px #0001}
td,th{padding:12px 16px;border-bottom:1px solid #e5e7eb;text-align:left}th{background:#f1f5f9;font-size:13px;color:#64748b}
a.btn{background:#2563eb;color:#fff;padding:6px 14px;border-radius:6px;text-decoration:none;font-size:13px}</style>
<header><h1>downloads.example.com</h1><p>Community mirror &middot; Linux installer images</p></header>
<main><h2>Latest releases</h2><table><tr><th>Image</th><th>Version</th><th>Size</th><th></th></tr>
<tr><td>Debian netinst (amd64)</td><td>13.1.0</td><td>783 MB</td><td><a class="btn" href="#">Download</a></td></tr>
<tr><td>Ubuntu Desktop (amd64)</td><td>24.04.3 LTS</td><td>6.2 GB</td><td><a class="btn" href="#">Download</a></td></tr>
<tr><td>Ubuntu Server (arm64)</td><td>24.04.3 LTS</td><td>2.9 GB</td><td><a class="btn" href="#">Download</a></td></tr>
<tr><td>Fedora Workstation Live</td><td>43</td><td>2.4 GB</td><td><a class="btn" href="#">Download</a></td></tr>
<tr><td>Arch Linux</td><td>2026.10.01</td><td>1.2 GB</td><td><a class="btn" href="#">Download</a></td></tr>
<tr><td>Alpine Linux standard</td><td>3.22.2</td><td>246 MB</td><td><a class="btn" href="#">Download</a></td></tr></table>
<p style="color:#64748b">Checksums and signatures are next to every image.</p></main>`;
