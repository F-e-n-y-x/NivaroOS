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
session token or this box has no data for them: `v1/cloud`, `v1/disks` (drive health, the same drives as `v1/sys/disks-usage`), `v1/disks/usb`,
`v1/storage`, `v1/users/current/custom/{shortcut,link,legacy_app_overrides}`,
`v1/vm-sidecar/host/display`, `v1/vm-sidecar/host/desktop/installed`, `v2/app_management/compose/jellyfin/logs` and the
terminal session lists `v1/sys/terminal-sessions` and `v1/container/terminal-sessions`.
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

Edit app (2026-09-26): `v2/app_management/compose/jellyfin.yaml` is the
compose file as `GET /v2/app_management/compose/{id}` with
`Accept: application/yaml` returns it, written by hand: this box has no
compose apps, so the shape comes from the server's own code path
(compose-go's loader + `GenerateYAMLFromComposeApp`, run locally on a
sample file) - long-form ports and volumes, bytes for the memory limit,
4-space indent, keys sorted. The tests pass it as an override (the fake
server only serves .json/.txt/.png by URL).


Free up memory (2026-09-26): `v1/sys/memory/clear.json` is written by hand
in the shape of `POST /v1/sys/memory/clear` (services/core
route/v1/memclear.go) - it isn't a GET, so the shots pass it as an
override; it is never called against the real box. `swapTotal`/`swapUsed`
in `v1/sys/utilization.json` were added by hand (2 GB of a 16 GB swap in
use) so the sheet offers "Also empty swap".

Backup & Sync (2026-09-30): `backup/*.json` are not served by URL; the
Backup shots and widget tests pass them as overrides (`backupServer` in
`../backup_test.dart`). They are built from the examples in
`docs/specs/backup-api.json` - the frozen REST contract, whose examples
`services/backup/jobs/contract_test.go` decodes strictly into the Go
types - with ids, names and times moved to this shot's day (+05:30) and a
longer run history, version list and plan written in the same shapes. This
box has no backup jobs, so there was nothing real to capture beyond
`GET /v1/backup/health` and an empty `GET /v1/backup/jobs`.

Download Station and share links (2026-10-07): `ds/downloads.json` keeps
the exact field set of a real `GET /downloads` from this box's
download-sidecar (`DownloadView`, services/download-sidecar/engine.go),
with the entries rewritten to public sample files in each state
(downloading, queued, paused, failed, completed) and the segments dropped;
`ds/settings.json` is the real `GET /settings`, `ds/roots.json` a trimmed
`GET /storage/roots`. They are not served by URL (Home would then show a
failed download in every shot); the shots pass them as overrides.
`v1/quickshare.json` is in the shape of the core's `GET /v1/quickshare`
(`quickShareItem`, services/core/route/v1/quickshare.go), taken from a
real create on this box with names, paths and hosts replaced; its times
are relative to the shot time.
