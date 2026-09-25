# Screenshot fixtures

The fake server in `../harness.dart` answers every request from this tree,
by URL:

| Request | File |
|---|---|
| `GET /v1/sys/utilization` | `v1/sys/utilization.json` |
| `GET /v2/app_management/web/appgrid` | `v2/app_management/web/appgrid.json` |
| `GET /v1/vm-sidecar/vms/mint` (VM sidecar, through the gateway) | `v1/vm-sidecar/vms/mint.json` |
| a plain-text answer (`/v1/sys/version/current`) | `v1/sys/version/current.txt` |
| a picture (`/v1/vm-sidecar/vms/Ghost-Windows-11/screenshot`) | `v1/vm-sidecar/vms/Ghost-Windows-11/screenshot.png` |

The query string is ignored (`/v1/folder?path=…` always reads
`v1/folder.json`); pass `overrides` to `shoot()` for anything else. A
request with no file gets a 404 and is printed as
`[screenshots] <name>: no fixture for …`, so a screen that starts calling a
new endpoint shows up in the test output.

## Where the data came from

Captured 2026-09-25 with read-only `curl` GETs against a real NivaroOS box
(`http://127.0.0.1/v1/…`, `http://127.0.0.1/v2/…` and the VM sidecar on
`:28641`), then scrubbed:

- People and devices renamed (Alex Morgan, "Alex's phone", `alex@example.com`),
  the tailnet renamed to `example-tailnet.ts.net`, Tailscale node keys, node
  IDs and companion device IDs replaced with hashes, profile pictures
  removed, public IPv4 addresses moved to 203.0.113.0/24 (LAN and Tailscale
  addresses kept).
- Personal side projects in the app list and containers renamed to common
  self-hosted apps (Jellyfin, Nextcloud, Home Assistant, …) and personal
  files in `v1/folder.json` replaced.
- The app store catalogue cut to 17 well-known apps, English text only
  (the real one is 4 MB). `v1/sys/logs.json` keeps the last 80 lines.

Written by hand, in the shape the server returns, because they need a
session token or this box has no data for them: `v1/cloud`, `v1/disks/usb`,
`v1/storage`, `v1/users/current/custom/{shortcut,link,legacy_app_overrides}`,
`v1/vm-sidecar/host/display`, `v1/vm-sidecar/host/desktop/installed` and `v2/app_management/compose/jellyfin/logs`.
`v1/vm-sidecar/vms/Ghost-Windows-11/screenshot.png` is a drawn stand-in for
a VM's desktop (no real screen captured).

Never put real tokens, keys, e-mail addresses or public IPs here: the
goldens and fixtures are committed.

Added for the Apps area (2026-09-25): `v2/app_management/{categories,appstore,info}`,
`v2/app_management/apps/upgradable` and `v2/app_management/apps/jellyfin`
(trimmed to English) are read-only GETs from the same box; the app store
sources are the public casaos.app and bigbeartechworld URLs.
`v1/container/jellyfin/logs` is written by hand in the shape of
`GET /v1/container/{id}/logs?timestamps=true` (Docker RFC 3339 timestamps
with nanoseconds, one line each).

The Trash (`v1/trash.json`, `files/trash_empty.json`): the empty one is a
real `GET /v1/trash` from this box (its Trash was empty, and the server
sends `"items": null` then); the full one is written by hand in the same
shape (services/core service/trash `Item`), with `deleted_at` in local
time without an offset so the relative times match the frozen shot time.
