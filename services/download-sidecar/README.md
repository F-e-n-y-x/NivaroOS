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
- **Google Drive links** - `drive.google.com/file/d/<id>/view`, `uc?id=`
  and `open?id=` share links are rewritten to Drive's direct download
  endpoint, and its "can't scan for viruses - Download anyway" page is
  followed automatically (`sharelinks.go`), so they download as the real
  file, multi-connection. Private files and quota errors are reported
  as such.
- **Lite browser** - a rewriting proxy under `/b/<session>/<scheme>/<host>/...`
  so any site can be shown in an iframe. HTML/CSS URLs are rewritten
  server-side, `inject.js` keeps runtime URLs in the proxy, a per-session
  cookie jar keeps sites signed in, and clicking a real file link is
  captured and handed to the downloader with the page's cookies/referer.
  Visit history is kept server-side (`history.go`, History tab).
- **Ad blocker** - uBlock Origin's default filter lists and syntax
  (network filters incl. `$third-party`/`$domain=`/`$important`/
  `$badfilter`, strict document blocking, element hiding with `#@#` and
  `$generichide`). Scriptlets (`##+js`) and procedural cosmetic filters
  are skipped. Global on/off, per-list toggles, trusted sites, My filters.

## Security notes

- API routes require the NivaroOS JWT (loopback exempt, like the other
  sidecars). Proxy routes are authorised by the unguessable session ID.
- Outbound connections to loopback / link-local are refused at dial time
  (`netguard.go`) - otherwise a web page in the browser could reach
  loopback-trusting NivaroOS APIs through the proxy.
- All proxied sites share one origin (the proxy's), like any rewriting
  proxy - don't use the lite browser for sensitive logins.

## Testing

`go test ./...` - engine tests run against local `httptest` range servers
(multi-connection integrity, pause/resume, no-range fallback, stalls,
429s, expired links), plus filter-engine and proxy-rewrite tests.
