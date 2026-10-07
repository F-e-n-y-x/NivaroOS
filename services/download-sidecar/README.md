# nivaroos-download-sidecar

Backs the **Download Station** windowed app, on port `:28642`. Installed
as `nivaroos-download-sidecar.service` (installer option "Download
Station", on by default; `--without-download-station` to skip). Pure Go,
no cgo, no system packages. State lives in
`/var/lib/nivaroos/download-station` (`downloads.json`, `settings.json`,
`filters/`); downloads default to `/DATA/Downloads`.

## What it does

- **Multi-connection downloads (IDM-style)** - probes with `Range:
  bytes=0-`, splits the file into up to 32 ranges, and re-splits the
  biggest remaining range whenever a connection frees up (dynamic
  segmentation). Pause/resume, resume across restarts, retries with
  backoff, a 30 s stall watchdog per connection, HTTP 429/503 handling
  (drops extra connections instead of failing), a global speed limit,
  a queue (max simultaneous downloads), and "refresh link" for expired
  URLs that keeps the bytes already fetched. Servers without range
  support fall back to one connection.
- **Torrents** (`torrent*.go`) - magnet links, `.torrent` uploads, http(s)
  links to a `.torrent` (LAN indexers allowed, the user typed them) and a
  watch folder. One manager drives a `TorrentEngine` interface, so the UI
  is the same whichever engine runs (Settings > Torrents > Engine):
  - **qBittorrent** (default when installed): libtorrent through
    `qbittorrent-nox`, as `nivaroos-torrent.service` - never enabled; this
    service starts it when a torrent needs it and stops it 2 minutes after
    nothing is downloading or seeding. Its WebUI listens on
    `127.0.0.1:28646` only, with a random password written into its
    profile (`/var/lib/nivaroos/torrent`) by this service; every start
    re-applies the settings and the lock-down (no external programs, no
    WebUI UPnP, no localhost bypass). The unit may execute nothing but
    itself and sees no NivaroOS state but its profile.
  - **My qBittorrent**: the user's own instance (URL + login); torrents
    are driven, its preferences are never changed.
  - **Built-in** (anacrolix/torrent, in this process): the fallback when
    `qbittorrent-nox` isn't packaged; no LSD, preallocation or global
    connection cap.
  Settings follow qBittorrent: global and alternative speed limits with a
  schedule, queueing, seeding ratio/time limits (pause or remove), port,
  UPnP/NAT-PMP, DHT/PeX/LSD, encryption, connection limits, save /
  incomplete / per-category folders, preallocation, per-torrent file
  priorities, sequential and first/last piece. From qBittorrent Enhanced
  only the public tracker list: fetched from a URL (default ngosang
  `trackers_best.txt`) daily or on demand and added to public torrents only
  - never to a torrent whose metadata has the private flag (or isn't known
  yet). Every save folder passes the same storage-root checks as downloads.
  Benchmark on the dev box (Debian netinst, first 32 MB, 2 runs each):
  qbittorrent-nox 5.3-7.1 MB/s, ~19 ms CPU/MB; anacrolix 3.8-4.0 MB/s,
  ~30 ms CPU/MB - hence the default.
- **Google Drive links** - `drive.google.com/file/d/<id>/view`, `uc?id=`
  and `open?id=` share links are rewritten to Drive's direct download
  endpoint, and its "can't scan for viruses - Download anyway" page is
  followed automatically (`sharelinks.go`), so they download as the real
  file, multi-connection. Private files and quota errors are reported
  as such.
- **Browser** - a real Chromium on the server, streamed to the UI
  (`rb_*.go`, `cdp.go`; design: `docs/specs/2026-09-29-ds-browser.md`).
  The same binary runs it as `nivaroos-download-sidecar browser-host` in
  its own socket-activated unit, `nivaroos-ds-browser.service`, as the
  unprivileged user `nivaroos-browser` with Chrome's own sandbox on. It
  starts on the first use, stops after 10 minutes nobody is looking, and
  keeps one profile per NivaroOS user (sign-ins persist; open tabs come
  back lazily). CDP runs over `--remote-debugging-pipe` - no debug port.
  The UI reaches it at `/v1/download-station/rb/ws` through the gateway
  (so it works over HTTPS and tunnels): JSON messages plus JPEG frames with
  flow control and adaptive quality, and input sent back. Downloads are
  caught as they start and handed to the engine with the tab's cookies.
  All of Chromium's traffic goes through an egress proxy on the netguard
  dialer (loopback/link-local never; LAN only for hosts the user typed;
  QUIC and non-proxied WebRTC off). `install-ds-browser.sh` (run by
  `installer/install.sh` on every install/update) sets it all up.
- **Lite browser** - the fallback when Chromium can't be installed or run
  (or the box has < 2 GB RAM; also Ubuntu on arm64, whose only Chromium is
  the snap and where Google/Chrome for Testing ship no build - Debian,
  Raspberry Pi OS, Fedora, Arch and openSUSE on arm64 use their chromium): a rewriting proxy under
  `/b/<session>/<scheme>/<host>/...` so any site can be shown in an iframe.
  HTML/CSS URLs are rewritten server-side, `inject.js` keeps runtime URLs
  in the proxy, a per-session cookie jar keeps sites signed in, and
  clicking a real file link is captured and handed to the downloader with
  the page's cookies/referer. Text responses are gzipped. Visit history is
  kept server-side (`history.go`, History tab).
- **Ad blocker** - in the browser, uBlock Origin Lite (vendored, loaded
  as an extension - it does the scriptlet/procedural filtering YouTube-
  style ads need) plus this service's own engine, applied to every request
  through CDP Fetch interception: uBlock Origin's default filter lists and
  syntax (network filters incl. `$third-party`/`$domain=`/`$important`/
  `$badfilter`, strict document blocking, element hiding with `#@#` and
  `$generichide`). Global on/off, per-list toggles, trusted sites and My
  filters apply to both; the shield shows each page's blocked count. In
  Lite mode only the own engine applies (scriptlets are skipped there).

## Security notes

- API routes require the NivaroOS JWT (loopback exempt, like the other
  sidecars). Proxy routes are authorised by the unguessable session ID.
- Outbound connections to loopback / link-local are refused at dial time
  (`netguard.go`) - otherwise a web page in the browser could reach
  loopback-trusting NivaroOS APIs through the proxy.
- All proxied sites share one origin (the proxy's), like any rewriting
  proxy - don't use the lite browser for sensitive logins. The full
  browser has no such limit: sites run only inside Chromium's sandboxed
  renderers, the UI only gets pictures, and the only cookie export is a
  provider's sign-in (`/rb/cookies/export`, TeraBox) for Online Accounts.

## Testing

`go test ./...` - engine tests run against local `httptest` range servers
(multi-connection integrity, pause/resume, no-range fallback, stalls,
429s, expired links), plus filter-engine and proxy-rewrite tests.
