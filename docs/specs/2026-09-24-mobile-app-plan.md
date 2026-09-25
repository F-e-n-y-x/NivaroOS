# NivaroOS mobile app plan (Android)

Status: plan, nothing built yet (2026-09-24). Research merged into §4-§6
the same day.
Owner ask: mobile work was paused while the server changed a lot. Plan the
next mobile build: (A) everything that must be fixed or added first so the
app works with today's server, then (B) new features, with (C) Android phone
backup as the major new feature.

Scope: the Flutter app in `mobile/` (Android; iOS stays possible but is not
planned here). App version at the time of the audit: `1.2.1+6`
(`mobile/pubspec.yaml`).

---

## 0. How it was checked

- Every app call site (`mobile/lib/services/*.dart`, `mobile/lib/screens/*.dart`,
  `mobile/android/app/src/main/`) was traced to the server handler that
  answers it today: gateway (`services/gateway/route/gateway_route.go`), core
  (`services/core/route/v1.go`, `route/v2.go`, `route/v1/*.go`), user-service,
  local-storage, app-management (`api/app_management/openapi.yaml`),
  vm-sidecar, gpu-sidecar, download-sidecar, message-bus and the new
  `services/backup`.
- Server behaviour was read from code, not guessed. Nothing was run on a
  phone and no live requests were sent. Items marked **(verify)** are
  inferred from code and need a device or live check before they are fixed.
- File:line references are to the tree as of 2026-09-24. They will drift;
  the function names next to them are the stable reference.
- Android and Google Play facts in §5 are stated as of Android 15 / Play
  policy in force in 2026. Play policy is out of scope while the app is
  sideload-only (§7.1); the Play notes are kept for the day that changes.
- §4, §5 and §6 also merge three research reports written on 2026-09-24:
  a feature inventory of the web UI (`ui/src/apps/*`, `ui/src/shell/*`,
  `ui/src/shared/*`) against `mobile/lib/**`, a survey of NAS and
  self-hosted mobile apps and their user complaints, and a review of
  Android platform rules (target SDK 35 to 37) and Android backup apps.
  Their sources are listed in §9.

---

## 1. Goals and non-goals

### Goals
1. The app works against today's server on the LAN **and** through a reverse
   proxy or tunnel that only publishes the dashboard's one https port
   (Cloudflare Tunnel, nginx, Tailscale Funnel). No feature may need a second
   port.
2. Nothing the app does can damage server state (VM definitions, files) or
   silently do less than it says (a "move" that copies, a "restart" that 404s).
3. One authenticated HTTP layer: every REST call, WebSocket and upload gets a
   fresh token and handles 401 the same way.
4. Feature parity with the web UI where it makes sense on a phone (Backup &
   Sync, Download Station, storage, VM clipboard, notifications).
5. A real Android phone backup: photos and videos in original quality,
   incremental, resumable, battery-aware, restorable, landing in the
   Backup & Sync module with versions and notifications. Honest about what a
   non-rooted phone and Google Play allow.
6. Tests: contract tests against server fixtures, unit tests for the backup
   engine, scripted end-to-end runs on an emulator.

### Non-goals
- iOS in this plan (the code stays portable; no iOS work is scheduled).
- Root, Magisk or Shizuku features. The app never asks for root.
- Backing up other apps' private data (not possible without root, §5.2).
- Replacing Google's own device backup (Wi-Fi passwords, system settings).
- Push through Firebase Cloud Messaging in the first version (§4 WP1-4 explains
  why).
- A web-view wrapper. The app stays native, as `mobile/README.md` says.

---

## 2. What the app has today

Native screens: discovery (mDNS `_nivaroos._tcp`), login, server profiles,
dashboard (utilization, disks, VM thumbnails), files (browse, upload,
download, copy/move, companion phones as sources), file viewer (video, PDF,
markdown), apps (compose list, start/stop, logs, custom install), app store,
system updates and logs, terminal (`/v1/sys/wsterm`), VM list/form/console
(native RFB client), Host Desktop (native RFB), Tailscale modal, speed test,
companion devices (phone registers with the server, runs an embedded file
server on port 8765 and a reverse WebSocket tunnel), settings.

Background: `flutter_background_service` runs a headless isolate in an
always-on `dataSync` foreground service that registers the phone every 30 s
and keeps the file server and tunnel up.

Not present at all: Backup & Sync, Download Station, GPU stats, storage
management (format/pools), notifications of any kind, biometric lock,
self-signed https support, tests (`mobile/test/` is empty).

---

## 3. Phase 0: must fix before any release

### 3.1 Audit findings (mobile)

Severity: **S1** breaks a feature or damages data, **S2** security or
privacy, **S3** reliability, **S4** stale docs or build.

#### VM Manager and Host Desktop

**M-01 (S1) VM REST calls bypass the gateway.**
`lib/services/vm_client.dart:175` (`VmClient(this.host, {this.port = 28641})`),
`:180` (`Uri.parse('http://$host:$port$path')`), `:202-209` (screenshot and
console URLs), callers `screens/vm_list_screen.dart:31`,
`screens/dashboard_screen.dart:50`, `screens/vm_console_screen.dart:39`,
`screens/host_desktop_screen.dart:46`.
Wrong: always plain `http://<host>:28641`. Behind a tunnel or reverse proxy
that port is not published, and an https server name has no http listener on
28641, so the whole VM list, VM form, snapshots, host display and the
dashboard VM thumbnails fail. The gateway now serves the sidecar same-origin
at `/v1/vm-sidecar/*` (REST and VNC WebSockets, prefix stripped,
`gateway_route.go:119-167`, dispatched at `:287-292`); the web UI moved to it
(`ui/src/api/vmSidecar.js`).
Do: build every VM URL from `ApiClient.baseUrl` + `/v1/vm-sidecar`, scheme
following the base (`https` → `wss`). Delete the `host`/`port` constructor
arguments. Keep no direct-port fallback: the gateway route works on the LAN
too.

**M-02 (S1) VNC consoles bypass the gateway and are always `ws://`.**
`lib/services/rfb_client.dart:63-70` (`const scheme = 'ws'`,
`'$scheme://$host:$port$path?token=…'`), callers
`screens/vm_console_screen.dart:40`, `screens/host_desktop_screen.dart:47`.
Wrong: same as M-01, plus the comment at `rfb_client.dart:56-62` ("vm-sidecar
has no TLS listener") is obsolete: TLS is the gateway's or the proxy's job now.
Do: `wss|ws://<base>/v1/vm-sidecar/vms/{name}/console` and
`…/v1/vm-sidecar/host/console`. Send the token in the `Authorization` header
of the handshake (`IOWebSocketChannel.connect(uri, headers: …)`; dart:io can
set headers, and `services/vm-sidecar/auth.go:47-50` reads the header first).
Drop `?token=` for native clients so tokens stop landing in proxy access logs.
The sidecar's origin check (`console.go:25-26`) accepts clients with no
`Origin`, which is what the app sends.

**M-03 (S3) VM calls never refresh the token.**
`vm_client.dart:439-450` (`_checkOk`) and every `http.*` call in that file use
`ApiClient.instance.accessToken` directly. After the 3 h access-token lifetime
(`services/common/utils/jwt/jwt.go:54-56`) every VM call returns 401 until
some other screen happens to trigger a refresh. `Image.network(screenshotUrl)`
(`dashboard_screen.dart:781-782`, `vm_list_screen.dart:508-509`) has the same
problem. Host routes always need a real token, even from loopback
(`vm-sidecar/auth.go:38-45`), so there is no fallback.
Do: VmClient goes through the shared authenticated layer (WP0-1). Screenshot
images are fetched with headers (`Image.network(url, headers: …)`) after a
freshness check.

**M-04 (S1, data damage) Saving a VM from the phone rewrites it wrongly.**
`vm_client.dart:268-315` (`updateVm`), caller `screens/vm_form_screen.dart:123-138`.
The sidecar's contract (`services/vm-sidecar/domain.go:1364-1380`,
`UpdateVMRequest`) is a **full** spec: "All fields are required (the frontend
always sends the full current+edited form)". Disks are matched by position,
and "a new Path (or an empty one) provisions and attaches an additional disk".
The app sends:
- `disks: [{path: '', gib: …}]` → a new empty disk is attached on every save
  that includes a disk size (verify on a test VM; the contract says so);
- one NIC with no `mac` → the VM's NIC list is replaced by one new NIC with a
  new MAC (a new DHCP lease; Windows may treat it as new hardware), and any
  second NIC is dropped;
- no `usb_devices` / `pci_devices` → passthrough devices are removed
  (`omitempty` lists, not in the "keeps current" exceptions).
Do: `updateVm` takes the loaded `Vm` and sends it back complete (every disk
with its `path` and `target`, every NIC with its `mac`, USB, PCI, shared
folders, boot order), changing only edited fields. Disk size can only grow an
existing disk. "Add disk" becomes its own explicit action
(`POST /vms/{name}/disks`). Until this lands, the Edit VM screen is disabled
in the build.

**M-05 (S3) VM model is missing fields.** `vm_client.dart:106-123`
(`Vm.fromJson`). Missing: `usb_devices`, `pci_devices`, `shared_folders`,
`boot_order`, `clipboard_channel`, `warning`, disk `target`. States other
than `running` (for example `paused`, with `/pause` and `/resume` routes in
`handlers.go:68-69`) are shown as stopped. Needed for M-04 and the clipboard
work in Phase 1.

#### Terminal

**M-06 (S3) Terminal uses the legacy resize frame.**
`screens/terminal_screen.dart:95-99` and `:202-206` send
`jsonEncode({'type':'resize',…})` as a plain TEXT frame. Framing v2
(`services/common/utils/wsterm/wsterm.go:6-31`): BINARY = input, TEXT starting
with 0x00 = JSON control. The legacy form still works
(`parseLegacyResize`, `wsterm.go:85-103`) but only for that exact object; it
is kept for old clients and should not be relied on.
Do: send resize as TEXT `"\u0000" + jsonEncode({...})`. Input already goes as
BINARY (`_send`, `:225-228`, `utf8.encode` → `List<int>`); keep that.

**M-07 (S3) Output decoding breaks multi-byte characters.**
`terminal_screen.dart:183` decodes each BINARY frame on its own
(`utf8.decode(data, allowMalformed: true)`). The server cuts output into
8 KiB frames (`wsterm.CopyOutput`, `wsterm.go:142-150`) with no regard for
UTF-8 boundaries, so box-drawing characters, emoji and non-Latin text split
across two frames show as U+FFFD.
Do: one `Utf8Decoder().startChunkedConversion(sink)` per session. Show the
close reason (`1011` = server failure) instead of a generic "disconnected".
Check token freshness before connecting and send it only in the header
(`:156-169` sends both header and `?token=`); the core JWT lookup reads the
header first (`services/core/route/v1.go:65-72`) and the terminal never skips
auth (`LocalAutomationSkipper("/v1/sys/wsterm")`, `v1.go:55`).

#### Apps, power, Tailscale, files

**M-08 (S1) App start/stop/restart calls a route that does not exist.**
`screens/apps_screen.dart:573-574` and `:591-592` call
`PUT /v2/app_management/compose/{id}/state` with `{"state": …}`. The route is
`PUT /compose/{id}/status` and its body is a bare JSON string
`"start" | "stop" | "restart"` (`services/app-management/api/app_management/openapi.yaml:528-548`,
`RequestComposeAppStatus` at `:877-887`; the web UI's generated client
`ui/src/apps/app-store/AppCard.vue:928`).
Do: `ApiClient.put('/v2/app_management/compose/$id/status', body: 'stop')`
(the client JSON-encodes it to `"stop"`). Poll `GET …/status` until the state
changes instead of the fixed 600 ms sleep.

**M-09 (S1) Reboot and power-off call routes that do not exist.**
`screens/settings_screen.dart:218` (`POST /v1/sys/power/$action`) and the
fallback `:226` (`PUT /sys/power`). Neither exists in core
(`services/core/route/v1.go:77-139`). The real route is
`PUT /v1/sys/state/:state` with `restart` or `off` (`route/v1/system.go:896-904`),
which `dashboard_screen.dart:162` already uses correctly.
Do: one helper used by both screens.

**M-10 (S1, S2) Tailscale sign-in uses a route that does not exist and leaks
the key.** `services/tailscale_service.dart:183-190`:
`POST /v1/tailscale/auth` does not exist (routes: `status`, `state/:state`,
`prefs`, `installed`, `install`, `v1.go:269-277`), so every attempt falls
into the fallback, which stores the Tailscale **auth key** as user custom
config (`/users/current/custom/tailscale_auth`) in plain text, where nothing
reads it.
Do: remove the auth-key path and the fallback. Use the server's real flow:
`PUT /v1/tailscale/state/up` returns `{"state":"needs_login","login_url":…}`
when a login is needed (`route/v1/tailscale.go:165-186`); open that URL in
the browser and poll status. Ask the owner to delete any saved
`tailscale_auth` custom key (one-time cleanup in the app on upgrade:
`DELETE /v1/users/current/custom/tailscale_auth`).

**M-11 (S1) "Move" from server to phone is a copy.**
`screens/files_screen.dart:905` deletes the source with
`deleteWithBody('/file/delete', …)`; that route does not exist (core has
`DELETE /v1/file` and `DELETE /v1/batch`, `v1.go:151-216`), and the error is
swallowed (`catch (_) {}`). The user is told the file moved; it is still on
the server.
Do: use `DELETE /v1/batch` like the rest of the screen, only after the
downloaded file's size matches the server's, and report a failed delete.

**M-12 (S3) Uploads skip the shared client and the resumable protocol.**
`files_screen.dart:767` builds `'${baseUrl}/v1/file/upload'` by string
concatenation (no scheme normalization, unlike `ApiClient._uri`), sends one
multipart request per file with the token read once (no refresh on 401) and
`totalChunks=1`. Core now rejects multi-chunk uploads there and points to
`/v2/casaos/file/upload` (`route/v1/file.go:1171-1176`), the resumable,
size-verified protocol the web UI uses (`ui/src/apps/files/UploadTray.vue:129`,
server `route/v2/file.go:22-95`).
Do: implement the v2 protocol (GET chunk check for resume, POST chunks) with
8 MiB chunks. 8 MiB keeps every request under common proxy limits
(Cloudflare's 100 MB request body cap on its standard plans, nginx
`client_max_body_size`).

**M-13 (S3) Opening an app uses a raw port.** `screens/apps_screen.dart:345`
opens `http://<host>:<app port>`. Through a tunnel that port is not
reachable. This cannot be fixed in the app alone. Do: prefer the app's
configured URL/scheme (already read above that line); when only a port is
known and the server is not on the LAN, say "this app is only reachable on
your network" instead of opening a dead URL.

**M-14 (S4) Stale path filter.** `files_screen.dart:352` hides
`/data/vm-shares`; VM shares now live in `/DATA/VMs/share`
(`installer/install.sh`, VM Manager step). Update the filter.

#### Session and token handling

**M-15 (S3) A failed refresh for any reason logs the user out.**
`services/api_client.dart:274-276`: `if (res.statusCode != 200) { _handleAuthFailure(); …}`.
A 502 from the gateway while user-service restarts, a Cloudflare 5xx, or a
captive portal page all clear the stored session. In the background isolate
this also clears the shared secure storage, so the UI is logged out too.
The server returns 401 for a genuinely bad refresh token
(`services/user/route/v1/user.go:870-884`).
Do: only 401 ends the session. Anything else keeps the tokens and reports
"server unreachable".

**M-16 (S3) WebSockets and side clients never refresh.** Terminal
(`terminal_screen.dart:151`), RFB (`rfb_client.dart:64`), companion tunnel
(`companion_file_server.dart:510-529`), uploads (`files_screen.dart:765-766`)
and VM calls read `accessToken` once. Do: `ApiClient.freshToken()` decodes
the JWT `exp` and refreshes when less than 5 minutes remain; every
non-`_send` caller uses it.

**M-17 (S3) Switching server profile leaves the background isolate on the old
server.** `screens/server_profiles_screen.dart:54-58` and `:85-86` change
`ApiClient` in the UI isolate only. The headless isolate
(`services/background_sync_isolate.dart:35-51`) keeps its own `ApiClient`
and keeps registering with, and serving files to, the previous server with
the previous tokens. Saved profiles also keep the tokens from login time; a
refresh never updates them, so switching back after 7 days fails.
Do: on profile switch, stop the service, switch, start it again. Keep the
companion device id and secret per profile. Write refreshed tokens back into
the active profile.

#### Background work, companion file server, Android manifest

**M-18 (S3, Play) The always-on foreground service will not survive Android 15.**
`services/background_service.dart:41-67` (`isForegroundMode: true`,
`foregroundServiceTypes: [AndroidForegroundType.dataSync]`),
`services/device_sync_service.dart:333-343` (30 s heartbeat). The build
targets `flutter.targetSdkVersion` (`android/app/build.gradle`), which is 35
with current Flutter. For apps targeting Android 15, `dataSync` foreground
services are limited to 6 hours per 24 hours; the system then calls
`Service.onTimeout` and the app must stop the service within seconds or it
is crashed. A heartbeat is also not a valid `dataSync` use under Play's
foreground-service policy, and a 30 s wake-up all day costs battery.
Do: heartbeat moves to WorkManager (15 min periodic, network constraint).
The foreground service runs only while the user has "Share this phone's
storage with the server" switched on, handles `onTimeout`, and says so in its
notification. Phone backup (Phase 2) uses WorkManager, not this service.

**M-19 (S2) The service is exported.** `android/app/src/main/AndroidManifest.xml:35-38`
overrides the plugin service with `android:exported="true"`. Any app on the
phone can start or bind it. Do: `exported="false"`.

**M-20 (S2) The companion file server can read and write the app's private
files.** `services/companion_file_server.dart:239` (`_handleDownload`),
`:308` (`_handleUpload`), `:328`, `:348`, `:379` take any absolute `path`
with no confinement. With the secret, the server (or anyone who learns the
secret) can read `/data/user/0/<app>/…` including shared preferences and the
Flutter secure-storage file. Other issues in the same file: it binds all
interfaces in plain http (`:35`, reachable on public Wi-Fi; the secret is the
only guard), compares the secret with `!=` (`:112`, not constant time), and
logs the tunnel URL including `?token=` (`:523`).
Do: allow only paths under the shared-storage roots
(`/storage/emulated/<user>` and mounted SD cards) after resolving symlinks;
reject everything else. Constant-time compare. Remove the token from the log
line and from the tunnel URL (header only). Default the whole feature to
**off**; switching it on explains that the server can then browse the phone.

**M-21 (S2) Fabricated device numbers.** `device_sync_service.dart:122-123`
and `:201-205` report 128 GB total / 48 GB used when real values are
missing; `:231` reports 100 % battery when unknown. The server and the web
UI show these as facts. Do: send nothing (null) when unknown; show "unknown".

**M-22 (S4, Play) Manifest and distribution.** `AndroidManifest.xml:11`
`REQUEST_INSTALL_PACKAGES` (used by self-update,
`screens/system_updates_screen.dart:229`, `:475`), `:14`
`MANAGE_EXTERNAL_STORAGE`, `:21` `usesCleartextTraffic="true"`. The first
two are restricted on Google Play; self-update outside Play is not allowed
for a Play-distributed app. Today the APK is sideloaded
(`mobile/build.sh` copies it to `/DATA/Downloads`). Do: WP0-6 splits the
build into two flavors (`full`, sideload/F-Droid; `play`) and decides per
permission. Cleartext stays allowed for LAN IP addresses only, through a
`network_security_config.xml` (see M-24), not app-wide.

#### Docs, tests, missing modules

**M-23 (S4) README is out of date.** `mobile/README.md:8-10` (says the VM
console embeds the web console; it is a native RFB client now),
`:32-36` (says the VM sidecar has no auth; it requires the JWT,
`vm-sidecar/auth.go`), `:66` and `:70` (debug-signed only; release signing
exists, see `android/app/build.gradle`), `:87-94` (limitations list).
Do: rewrite with the architecture after Phase 0. Do not describe the
keystore location in the README.

**M-24 (S3) Self-signed https does not work.** README `:90-93`. A server on
https with its own certificate fails every call with a TLS error the app
shows as "Could not reach the server" (`api_client.dart:215-217` swallows the
exception). Phase 0 minimum: a precise error ("the server's certificate is
not trusted"). Real support (trust on first use with a pinned fingerprint)
is Phase 1 WP1-8.

**M-25 (S3) No tests.** `mobile/test/` is empty. Every finding above would
have been caught by a contract test against server fixtures. WP0-7.

**M-26 (info) Not broken, not present.** The app does not call GPU
(`/v1/gpu/*`), Download Station (`/v1/download-station/*`, `/health`),
Backup & Sync (`/v1/backup/*`, `/health`), storage jobs
(`/v1/storage/jobs/:id`), the message bus, or any notification feed, so
nothing there is broken. These are Phase 1 features. Whenever the app starts
using an optional module it must first ask its `…/health` route through the
gateway and hide the feature when it does not answer, as the web UI does
(`ui/src/utils/downloadStationInstalled.js`, `backupInstalled.js`).

**M-27 (info) Local-automation rule.** The app is never same-host, so the
loopback rule in `services/common/middleware/localauth.go` never exempts it,
and it must not try: it never sends `X-Nivaroos-Local-Automation` (the
gateway strips it anyway, `MarkLocalAutomation`, `localauth.go:77-83`). No
change needed. One deployment note belongs in the docs: a reverse proxy
**on the NivaroOS box itself** must add `X-Forwarded-For`
(nginx: `proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;`).
Without it, a request from the phone (no `Origin`, no `Sec-Fetch-Site`)
arriving from a loopback proxy looks like local automation
(`IsDirectLocalAutomation`, `localauth.go:47-49`) and skips the JWT on the
backends that allow that. Check `cloudflared` on this box sends it (verify).

### 3.2 Server problems found during the audit

These are server bugs, not mobile ones. S-01 should be fixed now, whatever
happens to the app. The others block Phase 1 or 2. S-02 to S-07 are being
fixed on the server (2026-09-24).

**S-01 (S2, fix now) Path traversal in companion upload.**
`services/core/route/v1/companion.go:1388-1395` (`PostCompanionDeviceUpload`):
`?path=` is `filepath.Clean`ed and only a leading `/` is removed, so
`path=../../../etc/cron.d` joins outside the device folder; core runs as
root. Any logged-in user can write a file anywhere. Do: resolve under the
device folder with a within-root check (reuse whatever the Files rebuild uses
for this), reject `..`, add a handler test with `../` and symlinked
directories. The download check at `:1353-1360` uses a bare prefix test
(`/DATA/Companion2/…` passes); fix it the same way.

**S-02 (S1) Companion dedup deletes other phones.** Being fixed on the
server (2026-09-24).
`companion.go:170-221` (`deduplicateCompanionDevicesLocked`) and
`:880-898` (register) treat two devices with the same IP as one and delete
the older. The IP is the phone's own **private LAN address** (`input.IP`,
`:861-864`), so two phones on different home networks that both have
`192.168.1.23`, or one phone that gets another's old DHCP lease, delete each
other along with their folder mapping. Model+name matching has the same
effect for two identical phones with default names. Do: identify devices by
id only. Migration: none needed (the file keeps working).

**S-03 (S3) Companion devices are global.** Being fixed on the server
(2026-09-24). Every logged-in user sees,
renames, browses and deletes every user's phones (`GetCompanionDevices`,
`:797-837`, no owner). Today every account is effectively an admin
(`services/backup/jobs/auth.go:75-81` notes tokens carry no role), so this is
low risk now; add `owner_user_id` before roles arrive.

**S-04 (S3) Companion files off-LAN: listing works, download does not.**
Being fixed on the server (2026-09-24).
The reverse tunnel only implements `list` (`FetchCompanionFilesFromDevice`,
`:355-430`); downloads and uploads need a direct LAN connection
(`ProxyCompanionStream`, `:433-440`). Through a tunnel the web UI shows the
phone's files and then fails on open. Phase 1 WP1-7 either streams over the
tunnel or returns a clear "phone is not on the same network" error.

**S-05 (S2) Message-bus WebSockets need no token.** Being fixed on the
server (2026-09-24).
`services/message-bus/route/routers.go:50-52` skips JWT for every
`GET` + `Upgrade: websocket`, which covers `/v2/message_bus/event/{source}`
and socket.io's websocket transport. Anyone who can reach the dashboard can
subscribe to every event (file operations, backup progress with file names).
Do: require `?token=` (or the header) on WebSocket upgrades, after checking
that the web UI's socket.io client sends one (verify `ui/src/main.js:64-66`).
The app needs this fixed before it subscribes to events (WP1-4).

**S-06 (S3) No notification feed on the server.** Being fixed on the
server (2026-09-24).
`services/core/service/notify.go:52-66` (`SendNotify`) only publishes to the
message bus; the backup service does the same (`services/backup/jobs/notify.go`).
The web UI keeps its own NotificationCenter. A phone that was not connected
at that moment never learns a backup failed. Phase 1 WP1-4 adds a persisted
feed.

**S-07 (S3) No long-lived device credential.** Being fixed on the server
(2026-09-24). Access tokens live 3 h and
refresh tokens 7 days (`jwt.go:54-60`); a password change ends all sessions
(`user.go:880-884`). A phone that is off for a week, or whose owner changed
the password, silently stops backing up. Phase 2 WP2-1 adds a revocable,
scoped device token.

### 3.3 Reverse proxy and tunnel check

Target: the server is reachable only as `https://nas.example.com` (one port,
real certificate) through Cloudflare Tunnel, nginx or Tailscale Funnel.

| Area | Today | After Phase 0 |
|---|---|---|
| REST through `ApiClient` | works (scheme from base URL, same origin) | works |
| Login, refresh | works; a proxy 5xx during refresh logs the user out (M-15) | works |
| Terminal WebSocket | works (`wss` from base); token also in URL | works, header only |
| VM list, form, snapshots, thumbnails | broken (M-01) | works via `/v1/vm-sidecar` |
| VM console, Host Desktop | broken (M-02) | works via `/v1/vm-sidecar`, `wss` |
| File upload | works for files under the proxy's body limit only (M-12) | works, 8 MiB chunks |
| File download | works (streamed GET) | works |
| Companion registration and tunnel | works (`wss` from base) | works |
| Companion file access by the server | list only; download needs LAN (S-04) | Phase 1 |
| Open an installed app | only if the app has its own public URL (M-13) | honest message |
| mDNS discovery | LAN only, by design | same; manual URL entry for remote |
| Self-signed https | fails (M-24) | clear error; Phase 1 pinning |
| Speed test | works (`/speedtest/*` on the gateway) | works |

VNC over a tunnel is slow with the current encodings: the RFB client asks
only for Raw and CopyRect (`rfb_client.dart:196`), so a full 1280×720 frame
is 3.6 MB. Phase 1 WP1-6 adds a compressed encoding.

### 3.4 Phase 0 work packages

Effort is in engineer-days (ed) for one person who knows Flutter and can run
the Go tests. It includes tests.

**WP0-1 One authenticated HTTP layer** (M-03, M-12 part, M-15, M-16). 3 ed.
- Files: `lib/services/api_client.dart` (new `freshToken()`, `authHeaders()`,
  `send(http.BaseRequest)` with the same 401-refresh-retry for streamed and
  multipart requests; refresh ends the session only on 401),
  `lib/services/vm_client.dart`, `rfb_client.dart`, `companion_file_server.dart`,
  `screens/terminal_screen.dart`, `screens/files_screen.dart`.
- Server changes: none.
- Acceptance: unit tests with a fake server: expired token → one refresh →
  retry succeeds; refresh 502 → session kept, "unreachable" shown; refresh
  401 → logged out; two parallel 401s → one refresh call. WebSocket connect
  with 4 min left on the token refreshes first.

**WP0-2 VM Manager and Host Desktop through the gateway** (M-01, M-02, M-05). 2 ed.
- Files: `vm_client.dart`, `rfb_client.dart`, the four screens that construct
  them, `models/` (full `Vm`).
- Acceptance: against a server with only the gateway port open (firewall
  28641): list, start, stop, snapshot, screenshot, VM console and Host Desktop
  work over `http://LAN` and over `https://` behind nginx with a real
  certificate. Host console without a token → 401.

**WP0-3 Safe VM edit** (M-04). 2 ed.
- Files: `vm_client.dart` (`updateVm(Vm current, VmEdits edits)`),
  `screens/vm_form_screen.dart`, new `addDisk()`.
- Server changes: none; add a vm-sidecar handler test that a full spec
  round-trip (GET → PUT unchanged) leaves the domain XML identical except for
  libvirt-generated fields.
- Acceptance: on a VM with 2 disks, 2 NICs and a USB device: edit memory,
  save, GET: disks, MACs, USB device unchanged, no new disk file in
  `/DATA/VMs/<name>/`. Grow disk 20 → 30 GiB works; shrink is refused in the
  form.

**WP0-4 Broken calls** (M-06, M-07, M-08, M-09, M-10, M-11, M-13, M-14). 2.5 ed.
- Files: `terminal_screen.dart`, `apps_screen.dart`, `settings_screen.dart`,
  `dashboard_screen.dart`, `tailscale_service.dart`, `widgets/tailscale_modal.dart`,
  `files_screen.dart`.
- Acceptance: contract tests (WP0-7) for each route and body; terminal
  prints `printf '\xe2\x94\x80%.0s' {1..5000}` (a long run of 3-byte
  characters) with no U+FFFD; resize from rotation reaches the pty
  (`stty size` changes); app stop/start/restart changes the compose status;
  reboot sends `PUT /v1/sys/state/restart`; Tailscale up opens the login URL;
  server→phone move deletes the source only after a size match.

**WP0-5 Background and companion hardening** (M-17, M-18, M-19, M-20, M-21). 3.5 ed.
- Files: `background_service.dart`, `background_sync_isolate.dart`,
  `device_sync_service.dart`, `companion_file_server.dart`,
  `screens/server_profiles_screen.dart`, `screens/companion_devices_screen.dart`,
  `AndroidManifest.xml`, new WorkManager heartbeat (native Kotlin worker or
  the `workmanager` plugin; decide in WP2-2 and use the same).
- Server changes: S-01, S-02 in `services/core/route/v1/companion.go` with
  handler tests.
- Acceptance: fresh install → storage sharing is off; heartbeat every
  ~15 min visible in the device list; with sharing on, `GET /download?path=/data/user/0/…`
  → 403 and `path=/storage/emulated/0/../../data/…` → 403; Android 15
  emulator runs sharing for 7 h without a crash (the service stops cleanly at
  the timeout and the UI says so); profile switch → the next heartbeat goes to
  the new server only.

**WP0-6 Distribution decision and flavors** (M-22, M-24 part). 2 ed.
- Files: `android/app/build.gradle` (`productFlavors { full {…}; play {…} }`
  with a `src/play/AndroidManifest.xml` that uses `tools:node="remove"` for
  `REQUEST_INSTALL_PACKAGES` and `MANAGE_EXTERNAL_STORAGE`),
  `res/xml/network_security_config.xml` (cleartext allowed for private
  addresses only, see note), Dart `const bool kPlayBuild` from
  `--dart-define`, `mobile/build.sh` (builds the `full` flavor; signing
  config untouched).
- Note on cleartext: `network_security_config` matches domains, not IP
  ranges. Practical rule: allow cleartext app-wide in `full` (LAN servers are
  commonly plain http by IP), and in `play` allow it with the in-app warning
  "this connection is not encrypted" on every non-https profile.
- Acceptance: both flavors build; `play` APK's merged manifest has neither
  restricted permission and no self-update entry point; `full` keeps today's
  behaviour. Signing uses the existing keystore (see the release-signing
  note; never regenerate it).

**WP0-7 Contract tests** (M-25). 2.5 ed.
- Files: `mobile/test/contract/*.dart`, fixtures under
  `mobile/test/fixtures/` generated from the server: a small Go test in
  each service that writes the JSON it returns for the routes the app uses
  (VM list/get, compose status, sys/state, utilization, companion register,
  backup `docs/specs/backup-api.json` examples), checked into git and
  compared in CI both ways.
- Acceptance: `flutter test` runs in the existing CI; changing a server
  route or body breaks a test on one side.

**WP0-8 README and in-app docs** (M-23, M-27). 0.5 ed.

Phase 0 total: about **18 ed**. Release gate: all WP0 acceptance checks pass
on a physical Android 14 or 15 phone, on the LAN and through a tunnel.

---

## 4. Phase 1: roadmap to a stable app with the best UX

This section replaces the first Phase 1 list. It keeps the work package
numbers WP1-1 to WP1-12 that §3 refers to, and adds new ones from the
2026-09-24 research (§0). Sources are in §9.

Priorities:
- **P0**: joins the Phase 0 release gate (§3.4). The first release after
  Phase 0 does not ship without it.
- **P1**: the next release.
- **P2**: the release after that.
- **P3**: backlog (§6).

Effort is in engineer-days (ed), tests included, as in §3.4.

### 4.1 Findings added after the audit

§3.1 is kept as audited. These items came up afterwards and use the same
severity scale. They are fixed in the work packages named.

**M-28 (S1) "Flush Cached Memory & Buffers" starts a system update.**
`widgets/monitor_modals.dart:914-923` calls `POST /sys/update`, which is
core's `SystemUpdate` (`services/core/route/v1/system.go:70`, route at
`route/v1.go:82`). The app then says "Memory sync and caches flushed
successfully." whatever happened. There is no drop-caches REST route; drop
caches exists only as a scheduled-task action
(`services/core/service/schedule.go:449`). Do: remove the button. A real
admin-only route is not planned. WP1-13.

**M-29 (S1) Server-to-server copy and move overwrite silently.**
`screens/files_screen.dart` (around `:850`) sends `/batch/task` with
`'style': 'overwrite'`. The web UI asks first (`TransferConflictWindow.vue`).
Do: a conflict sheet (skip / overwrite / keep both, "apply to all"). WP1-15.

**M-30 (S3) Basic server file operations are missing.** No rename
(`PUT /v1/file/name` exists), no new folder (`POST /v1/folder`), no new
file, no compress or extract (`/v1/file/archive`, `/unarchive`). "Search"
filters only the open folder; the web UI has no server-side file search
either. WP1-15, WP1-26.

**M-31 (S3) The App Store falls back to a hard-coded app list.**
`screens/app_store_screen.dart:24-96` embeds Plex, Nextcloud and others with
GitHub icon URLs. It shows no store sources, no CPU architecture filter and
no install progress. Do: live catalog only, with an error state when it
cannot load. WP1-9.

**M-32 (S4) The speed test measures the phone, not the server.**
`services/speedtest_service.dart` tests the phone's internet against public
CDNs. The web Network widget runs the server's test (`/v1/sys/speedtest*`).
Do: keep both and label them: "Test this phone's connection" and "Test
server speed". WP1-13 (labels), WP1-24 (server test).

**M-33 (S3) Addendum to M-18: the companion service needs a redesign.**
The research confirms M-18 and adds three facts:
- On Android 15+, `Service.onTimeout()` must call `stopSelf()` within
  seconds, or the app crashes.
- A `dataSync` foreground service cannot start from `BOOT_COMPLETED`.
- It is not known whether `flutter_background_service` ^5.0.10 implements
  `onTimeout` (verify).
The owner keeps "browse my phone from the server" (§7.4), so the feature
moves to on-demand sharing sessions. WP1-7.

**M-34 (S1 for §5) Sideloaded apps cannot get SMS and call-log
permissions without an extra step (Android 15+).** Android 15 treats SMS
permissions and the default-SMS and dialer roles as "restricted settings"
for apps that were not installed from a store. The permission prompt stays
greyed out until the user opens App info → ⋮ → **Allow restricted
settings**. Without handling this, SMS backup looks broken on every Android
15+ phone. WP1-14 (detector and guide), WP2-9 (flows).

**M-35 (S3) Google developer verification.** From 30 September 2026,
certified devices in Brazil, Indonesia, Singapore and Thailand install only
apps registered to a verified developer; this goes global in 2027.
Unverified apps still install through ADB, through the "advanced flow"
(developer options, a restart, a 24-hour wait, then biometric
confirmation), or under a free "limited distribution" account on up to 20
devices. See WPR-2 and the owner question in §4.7.

**M-36 (S3) Target SDK 35 and 36 rules are not handled.**
- Edge-to-edge is enforced at target 35, and the opt-out is ignored at 36.
  Only about 6 screens use `SafeArea`.
- Predictive back is on by default at target 36, and
  `android:enableOnBackInvokedCallback="true"` is missing from the manifest.
- At target 36, orientation and resizability settings are ignored on
  screens ≥600 dp wide, and there is no `NavigationRail` layout.
- `ACCESS_LOCAL_NETWORK` becomes mandatory at target 37 for all traffic to
  private addresses and mDNS. That is the app's core traffic.
WP1-14.

**M-37 (S3) Dart's TLS ignores Android's trust settings.** Dart's
`HttpClient` bundles BoringSSL and ignores `network_security_config` and
user-installed CAs. Certificate pinning for self-signed servers must be done
in Dart (WP1-8). WebView and native code do honour
`network_security_config`, so cleartext (global today) is scoped there to
LAN and `.local` hosts.

**M-38 (S3) `flutter_secure_storage` upgrade trap.** The app uses ^9.2.2.
v10 drops EncryptedSharedPreferences and migrates stored data
automatically; v11 removed that migration path. A user who jumps from a 9.x
build to an 11.x build loses the stored tokens. Do: ship a 10.x build first
and move to 11 in a later release. WP1-14.

**M-39 (S3, verify) 16 KB memory pages.** Plugins with native `.so` files
(`flutter_pdfview`, `ffi` libraries, video) must be 16 KB-aligned, or a
sideloaded APK crashes on 16 KB-page devices. WPR-3 checks it.

### 4.2 Design principles

These rules apply to every screen, old and new. WP1-16 builds the shared
parts; every other work package is reviewed against this list.

**Navigation**
- Phone width (compact, <600 dp): a bottom `NavigationBar` with at most five
  destinations: Dashboard, Files, Apps, Backup, More. The user can hide tabs
  they don't use (for example VMs). Optional modules (Backup & Sync,
  Download Station, GPU) are hidden when their `…/health` route does not
  answer (M-26).
- Medium width (600–839 dp): `NavigationRail`. Expanded width (≥840 dp):
  rail or drawer, plus list-detail layouts for Files, Apps and Backup. The
  layout reacts live to fold, unfold and window resize, because orientation
  locks are ignored at target 36.
- Back: `PopScope` with `onPopInvokedWithResult` only, and
  `PredictiveBackPageTransitionsBuilder` in `pageTransitionsTheme`. Back is
  never blocked silently. The terminal and VM console ask before they close.
- Every notification and widget opens a specific screen through a deep link.
- Test with both gesture and 3-button navigation.

**Material 3**
- Keep `useMaterial3`. Add `dynamic_color` so Android 12+ uses the
  wallpaper palette, with the NivaroOS seed colour as the fallback.
  Status colours (ok, warning, error) are harmonised with the palette.
- A theme setting: system, light or dark. Widgets and notifications match.
- Do not use the community M3 Expressive package for core UI.
- Every screen draws edge to edge. Content respects system-bar insets
  (`SafeArea` or `MediaQuery.paddingOf(context)`), and lists scroll under
  the navigation bar.

**Adaptive layouts**
- Use the M3 window size classes above; no fixed widths.
- Landscape works on every screen (Plex removed it in 2025 and was pushed to
  bring it back).
- Dialogs become bottom sheets on phones and dialogs on large screens.

**Offline and caching**
- The last dashboard, apps, files and backup responses are cached per
  server profile with a timestamp. With no connection the screen shows the
  cached data and "Offline — last updated 5 min ago", not a blank page.
- Folder listings are paged and cached. Cached content shows instantly and
  refreshes quietly. Thumbnails load lazily into a size-limited cache.
- Retries use exponential backoff with jitter and start again by themselves
  when connectivity returns.
- Pull-to-refresh on every list.

**Error states**
Every screen and dashboard card has exactly these states, built once as
shared widgets:

| State | Shows |
|---|---|
| loading | a skeleton, never a bare spinner; after 15 s it becomes an error |
| empty | what goes here and the action that fills it |
| offline | cached data with its age, or "can't reach the server" |
| auth expired | "sign in again", keeping the current screen |
| server error | a short message, **Retry**, and a copyable detail (route, status, body excerpt) |
| permission denied | why the permission is needed, and a button to the right settings page |
| module absent | "Install X on the server", nothing else |
| TLS problem | "the server's certificate is not trusted" (M-24), with the pinning flow |

An endless spinner is a bug; users of Unraid apps complain about exactly
that.

**Safety in the UI**
- Deletes go to the server Trash where it applies, with an UNDO snackbar.
- Destructive disk actions need the disk's name typed.
- Power actions, terminal and VM delete need the app lock (WP1-8).
- The app never runs its own VPN. Android allows one VPN at a time and
  Tailscale fails when another app is the always-on VPN. The app points to
  the Tailscale app instead.

**Notifications**
- Channels: Progress, Backups, Downloads, System, Security.
- One steady progress notification per long job, updated in place, never
  flashing. Nextcloud users counted about 80 "preparing upload"
  notifications a day. Everything else is grouped.
- Every pause or skip says why: "waiting for Wi-Fi", "battery saver",
  "server full (507)".

**Accessibility**
- Test with TalkBack and at 200 % font scale. Never clamp `textScaler`
  globally.
- Every icon-only button has a `tooltip` or `Semantics` label. List tiles
  use `MergeSemantics`. Progress uses `liveRegion`.
- Touch targets ≥48 dp, text contrast ≥4.5:1.

**Performance budgets** (measured in profile mode on the device matrix,
WPR-5, and checked before each release):

| Measure | Budget |
|---|---|
| Cold start to first dashboard frame with cached data, mid-range phone (Pixel 6a class) | ≤1.5 s |
| First page of a 2,500-entry folder on the LAN | ≤1 s (Nextcloud users report 18–19 s) |
| Scrolling a long list | ≥99 % of frames under 16 ms, no frame over 50 ms |
| Progress ticks | rebuild the one row (`ValueListenable` per row), not the list |
| Thumbnail cache | ≤100 MB on disk, ≤50 MB in memory |
| Idle background wake-ups | <10 per 24 h attributed to the app (same as §5.9) |
| Release APK size | not more than 10 % above the Phase 0 build without a stated reason |

**Features are not removed without a replacement.** Changes ship in
stages (WPR-6). A phone feature that exists today is removed only once its
replacement has shipped.

### 4.3 Stability and release engineering

**WPR-1 Crash and error reporting without Google (P0).** 2 ed, of which
0.5 server.
- Catch `FlutterError.onError`, `PlatformDispatcher.instance.onError` and
  errors in background isolates and workers. Native crashes are caught with
  ACRA or a small Kotlin `UncaughtExceptionHandler` (choose in the WP).
- Reports go to a local ring buffer (last 50 reports, at most 1 MB): app
  version, Android version, device model, stack trace, the last 200 log
  lines. Tokens, passwords, file names and URLs with query strings are
  removed before anything is written.
- "Send diagnostics to my server" (opt-in, with a preview of what is sent)
  posts to a new core route `POST /v1/sys/mobile-diagnostics` (user JWT).
  The report is stored rotated under the NivaroOS log directory and shown in
  the web UI's logs. Optional: the user can enter a Sentry-compatible DSN,
  for example a self-hosted GlitchTip. There is no third party by default.
- Acceptance: a forced Dart exception, a native crash and a crash in a
  WorkManager worker each appear in the ring buffer after a restart. After
  "Send", they appear on the server with no token or password in them.

**WPR-2 Self-update from GitHub releases (P0).** 3 ed, of which 0.5 server.
Sideloading is the only distribution (§7.1), so this is how every fix
reaches users.
- The check runs at most once a day. The server proxies and caches it
  (`GET /v1/sys/mobile-app/latest`, cached 6 h), so phones behind one NAT
  don't share GitHub's unauthenticated limit of 60 requests per hour per IP.
  Without the server route the app asks
  `api.github.com/repos/<org>/<repo>/releases/latest` directly.
- The release page works with Obtainium:
  - The tag carries the version (`mobile-v1.3.0`).
  - Asset names are stable (`nivaroos-android.apk`, `SHA256SUMS`).
  - The notes include the changelog and the SHA-256 of the signing
    certificate, so Obtainium and AppVerifier users can pin it.
- Install steps:
  1. Show the changelog.
  2. Download the APK.
  3. Check its SHA-256 against `SHA256SUMS`.
  4. Check that its signing certificate equals the installed app's
     (`PackageManager` signing info on the downloaded archive). A mismatch
     stops the update with a clear message.
  5. Install through a `PackageInstaller` session (`REQUEST_INSTALL_PACKAGES`
     is declared).
  This replaces the flow in `screens/system_updates_screen.dart:229`, `:475`.
- **Signature continuity.** Every release is signed with the existing
  release key. Android refuses an update signed with a different key, so a
  new key would force every user to uninstall and lose app data. The key is
  never regenerated or rotated. It is kept where the release-signing note
  says, with an offline backup. Nothing in this plan changes signing.
- README: install and update steps, including ADB and the Android
  "advanced flow" for unverified apps (M-35).
- Acceptance:
  - An update from build N to N+1 installs and keeps the login.
  - An APK with a wrong checksum is refused.
  - An APK signed with a test key is refused before Android is asked.
  - 100 phones checking in a day cause at most 4 GitHub requests from the
    server.

**WPR-3 CI checks (P0).** 2 ed. The existing workflow
(`.github/workflows/android-build.yml`, debug APK only) grows into:
- `flutter analyze` with no warnings, and `dart format --set-exit-if-changed`.
- `flutter test` (unit and contract tests, WP0-7) and the Kotlin unit tests
  (§5.9).
- A release-mode build. CI signs with a throwaway CI key, so the real key
  never leaves the owner's machine; releases are signed locally with
  `mobile/build.sh`.
- Merged-manifest assertions: the service is `exported="false"` (M-19), no
  permission that isn't on an allow-list, `enableOnBackInvokedCallback`
  present.
- `zipalign -c -P 16 -v 4` on the APK (16 KB pages, M-39).
- The APK size budget (§4.2).
- A dependency check: no Firebase, no Play Services artifacts. This keeps
  F-Droid possible later.
- Acceptance: each check fails the build when it is broken on purpose once.

**WPR-4 Integration tests against a test server (P1).** 5 ed.
- A script (`mobile/test/e2e/test-server.sh`) builds gateway, core,
  user-service, local-storage, app-management, message-bus, backup and
  download-sidecar from the same commit. It runs them in a temporary root
  with seeded data, a test user and short token lifetimes (verify that
  lifetimes can be set by config; add a test-only setting if not). A
  second profile puts nginx with `client_max_body_size 10m` and a
  self-signed certificate in front.
- `integration_test` runs on an Android emulator (API 34 and 36) in CI.
  Flows:
  - login, and token expiry with refresh;
  - files: list, rename, conflict, upload resumed after cutting the
    network, trash and undo;
  - app start and stop;
  - a backup run with its progress;
  - a notification from the feed;
  - certificate pinning, including a changed certificate;
  - module-absent states.
- VM, GPU and Host Desktop need real hardware. They run from a manual
  checklist on the owner's box before each release, using the same test
  IDs.
- Acceptance: the suite runs in under 20 minutes in CI and fails on any
  contract drift.

**WPR-5 Device matrix and release gate (P0).** 1 ed to set up, about 0.5 ed
per release to run.

| Device | Why |
|---|---|
| Pixel (physical), Android 16 | reference; predictive back, target 36 rules |
| Samsung (physical), One UI on Android 15 | vendor background limits, Samsung RCS-as-SMS behaviour |
| Xiaomi / HyperOS (physical or a borrowed one) | the vendor that kills background work hardest |
| Emulator API 29 (Android 10) or the app's `minSdk` if higher (verify `flutter.minSdkVersion`) | oldest supported; the MediaStore fallback paths |
| Emulator API 34 | UIDT jobs, partial photo access |
| Emulator API 35 with the 16 KB page image | 16 KB alignment, `dataSync` timeout |
| Emulator API 36 with `RESTRICT_LOCAL_NETWORK` enabled | local network permission; Tailscale 100.64/10 behaviour |
| Tablet or foldable emulator | rail and list-detail layouts, fold and unfold |
| Low-RAM emulator (2–3 GB) | memory budget, process death during uploads |

Each release runs, on the physical phones: gesture and 3-button
navigation, dark mode, 200 % font, TalkBack on the main flows, LAN and
tunnel. It also runs the performance budgets and WPR-4. The Phase 0 gate
(§3.4) is part of this.

**WPR-6 Staged releases and settings migrations (P1).** 1 ed.
- The app has two update channels: stable and beta (GitHub pre-releases,
  opt-in in Settings). A release goes to beta for at least a week.
- App settings and backup settings carry a schema version. Each upgrade
  runs versioned, idempotent migrations, tested from every earlier
  released version. Nextcloud users saw their settings reset after updates;
  migrations are what prevents that. A migration never drops a setting it
  does not understand. When a migration changes a backup setting, the app
  shows what changed.

### 4.4 Work packages by priority

**P0: first release after Phase 0**

**WP1-13 Post-audit fixes (P0).** 0.5 ed. M-28 (remove the flush
button), M-32 (speed-test labels), and removal of the hard-coded store list
(M-31; the full store work is WP1-9). Acceptance: no code path calls
`/sys/update` except the System Updates screen; the store shows an error
state when the catalog cannot load.

**WP1-14 Android platform readiness (P0).** 4 ed.
- Edge-to-edge insets on every screen, checked by a widget test that
  renders each screen with large system insets.
- `enableOnBackInvokedCallback`, predictive back transitions.
- `flutter_secure_storage` 10.x shipped in this release (M-38), 11.x not
  before the next one. Tokens kept out of Android Auto Backup
  (`allowBackup="false"` or backup rules).
- M-33: implement `onTimeout` in the foreground service, or replace the
  plugin with a small Kotlin service if it cannot. No foreground start from
  boot.
- A shared "restricted settings" helper (M-34). It detects the greyed-out
  state and opens App info (`ACTION_APPLICATION_DETAILS_SETTINGS`) with
  illustrated steps. Used by WP2-9 and any later restricted permission.
- Local network permission prepared for target 37: declared, requested with
  a rationale on the discovery and login screens, and denial handled (TCP
  timeouts, UDP `EPERM`). Tested now with
  `adb shell am compat enable RESTRICT_LOCAL_NETWORK <package>`. The target
  stays at the Flutter default until this passes.
- Acceptance: all screens pass the insets test. The app runs 7 h with
  sharing on, on API 35, without a crash. An upgrade from the Phase 0 build
  keeps the login. Discovery works with the local network restriction on,
  after the permission is granted.

**WP1-15 Safe file operations (P0).** 3 ed. M-29, M-30.
- Long-press starts multi-select with an action bar: copy, cut, delete,
  rename, share. A "Paste here" button appears while something is on the
  clipboard.
- A conflict sheet for copy and move with skip / overwrite / keep both and
  "apply to all", using the same server behaviour as the web UI's
  `TransferConflictWindow.vue`.
- Rename (`PUT /v1/file/name`), new folder (`POST /v1/folder`), new file.
- Acceptance: copying onto an existing name never overwrites without an
  answer. Each operation has a contract test (WP0-7).

**WP1-16 UI foundation (P0).** 4 ed.
- The shared state widgets and skeletons from §4.2.
- The per-profile response cache with its age.
- The connection badge (LAN / tunnel / Tailscale / offline) in the app bar.
- The theme with `dynamic_color` and the system/light/dark setting.
- Adaptive navigation (bar, rail, list-detail).
- Existing screens move onto these in this package.
- Acceptance: with the server switched off, every main screen shows cached
  data or the offline state within 15 s, and none spins forever.

**P1: next release**

**WP1-1 Backup & Sync management (P1).** 5 ed.
- Shown only when `GET /v1/backup/health` answers (optional module, spec
  `2026-09-24-backup-sync-app.md` §0).
- Screens:
  - Overview: a status header, then "Needs attention" first, then upcoming
    and recent runs.
  - Job list: a card per job with **Run now** (`POST /jobs/:id/run`),
    cancel, and enable/disable (`/toggle`).
  - Job detail: runs, and the next run from `/cron/preview`.
  - Run: steps and log (`/runs/:id/log?after=`).
  - The "waiting for you" decision (`POST /runs/:id/decide`, with the
    preview list from `/runs/:id/preview`). It is also reachable from a
    notification action.
  - Versions: a timeline, then browse, then restore to the server
    (`POST /jobs/:id/restore`) or save to the phone (`POST /downloads` →
    `GET /downloads/:token`, through the Android file picker).
- Job creation stays in the web UI; the app links there. Folders get a
  "Back up this folder" entry once WP1-26 lands.
- Contract: `docs/specs/backup-api.json`. The service accepts a raw token or
  `Bearer` (`services/backup/jobs/auth.go:55-58`). Writes need an admin
  (`:61-64`); show its 403 text.
- Progress: message-bus events `nivaroos:backup:run-*` (WP1-4), falling back
  to `GET /runs/:id` every 3 s while the screen is open, as the web UI does.
- Acceptance: with the module absent nothing about Backup shows. With it
  present, a run started on the phone shows live progress and ends with the
  same status the web UI shows. A decision answered from the notification
  reaches the server.

**WP1-2 Download Station (P1).** 3.5 ed.
- Health `GET /v1/download-station/health`.
- The list has progress bars, filter chips and swipe actions: pause,
  resume, retry, remove, and pause all.
- Add: URL, magnet, or a torrent file from the share sheet. Options:
  checksum, connections, and a save-to folder picked from `/storage/roots`.
- The detail page has "refresh link" and "show in Files". Settings: speed
  limit, concurrency, default folder.
- Change-feed polling with `GET /events?after=`
  (`services/download-sidecar/handlers.go:231-235`).
- Android share target: "Share → NivaroOS → Download on server". This is
  the phone-only win the market research points to.
- The built-in browser proxy (`/b/…`), the Lite Browser and the ad blocker
  are refused by the gateway on purpose and are not offered.
- Acceptance: a link shared from Chrome starts a download on the server.
  Its completion raises a notification (WP1-4).

**WP1-4 Notifications (P1).** 5 ed, of which 2 server.
- Server (S-05, S-06, being fixed now):
  - Token required on message-bus WebSockets.
  - A persisted feed (S-06, done in the message bus, the hub every event
    already passes through): `GET /v2/message_bus/notifications?after=<id>&before=<id>&limit=&unread=`
    returns `{data: [{id, time, source_id, event_name, category, level,
    title, message, key, args, action, icon, read}], unread_count,
    latest_id, has_more}`, plus `POST /v2/message_bus/notifications/read`
    and `/dismiss` (`{ids}` or `{all, up_to}`), read state per user. It
    stores every `*:notify` event with a `title` (backup today), app
    install/uninstall/update results and app failures, and storage job
    results (rules: `services/message-bus/service/notification_classify.go`).
    Kept 30 days, at most 1000. Live: `message-bus:notification:created`
    / `:state` on `/v2/message_bus/event/message-bus`. Not yet fed: the
    Download Station completion (the sidecar has no bus events yet; it
    needs to publish a `*:notify` event) and core's `SendNotify`, which
    only carries live state (utilization, file operations), not
    notifications.
  - If the server fix already delivers these, only the app part remains
    (3 ed).
- In the app: an inbox with categories, unread state, mark read and clear,
  like the web NotificationCenter.
- Alert types: disk SMART warning or failure, low storage, backup failed,
  backup stale, update available, new login, device paired.
- While the app is open: one WebSocket to
  `/v2/message_bus/event/{source_id}?names=…` (plain JSON, no socket.io
  client) with the token.
- In the background: a periodic WorkManager poll of the feed (15 min
  minimum, network constraint), local notifications on the channels from
  §4.2 with action buttons (Retry, Open, Decide) and deep links.
- Why not FCM: it needs a Firebase project and a Google service account on
  every self-hosted server, and it sends metadata through Google. It also
  rules out F-Droid. Instant delivery without Google (UnifiedPush / ntfy,
  or a Home Assistant-style persistent socket) is in Phase 3 (§6).

**WP1-5 VM clipboard (P1).** 2 ed.
- The web console has a clipboard panel, and the sidecar adds a
  `qemu-vdagent` channel with `copypaste`
  (`services/vm-sidecar/clipboard.go:25-31`). A VM reports
  `clipboard_channel` (`domain.go:127`) and needs guest tools.
- RFB:
  - Send ClientCutText (message 6) for paste.
  - Stop discarding ServerCutText (message 3, today read and dropped at
    `rfb_client.dart:255-258`) and put it on the phone clipboard.
  - Negotiate the Extended Clipboard pseudo-encoding for UTF-8 text when
    the server offers it (QEMU implements it together with the vdagent
    clipboard; verify on the target QEMU version). Without it, RFB cut
    text is Latin-1, so non-Latin text uses "Type it" (today's
    `_openTextInputDialog`, `widgets/rfb_view.dart:491-521`).
- UI: a clipboard sheet with "Paste into VM", "Type it" and the last 10
  items (kept in memory only). When `clipboard_channel` is false it shows
  "turns on the next time this VM starts", the same text as the web. The
  sheet is a shared component that Host Desktop reuses (WP1-6).

**WP1-7 Companion rework: browse my phone (P1).** 3 ed. Owner decision
§7.4, M-18, M-33, S-04.
- "Share this phone with the server" becomes a session with an end time:
  while the app is open, or for a chosen time (default 1 h). The session
  runs as a `dataSync` foreground service that handles `onTimeout` and stays
  far inside the 6 h per 24 h budget. Its notification shows the time left
  and a Stop button.
- The heartbeat is WorkManager (WP0-5), not this service.
- When the web UI wants to browse a phone that isn't sharing, it says
  "Open NivaroOS on the phone and tap Share". A server-triggered start is
  possible once instant notifications exist (§6).
- A `specialUse` foreground service type is an option if the owner wants
  longer sessions; sideloading avoids Play review, but Android still
  enforces the type rules. Decide in the WP.
- Transfers: use whatever the server fix for S-04 provides (streaming over
  the reverse WebSocket, or a clear "phone is not on the same network"
  error). The app side is chunked binary frames with request ids and
  backpressure.
- Acceptance:
  - Sharing for 1 h ends on time, with no crash.
  - Off-LAN browsing from the web UI either works or shows the clear error.
  - All M-20 path checks still pass.

**WP1-8 Security (P1).** 4 ed.
- App lock: biometric or device credential (`local_auth`;
  `BIOMETRIC_STRONG` or device credential). It locks after N minutes in the
  background and is required before terminal, power actions and VM delete.
  A lost phone must not mean root on the server.
- Self-signed https (M-24, M-37): trust on first use.
  - On first connect, show the certificate's SHA-256 fingerprint and ask.
  - Store the pin with the server profile.
  - Every connection, HTTP and WebSocket, goes through one `HttpClient`
    whose `badCertificateCallback` accepts only the pinned certificate, and
    only for that host.
  - A changed certificate stops with a blocking warning.
  - Certificates from a real CA need no pin.
- Cleartext: `usesCleartextTraffic` removed; a `network_security_config`
  allows cleartext for LAN and `.local` hosts for the native and WebView
  parts. Dart traffic on a plain-http profile shows "not encrypted" in the
  connection badge.
- Tokens: already in the Keystore via `flutter_secure_storage`. Add
  `FLAG_SECURE` on the terminal and login screens. Never log tokens (M-20).

**WP1-9 Apps and App Store (P1).** 6.5 ed. Was P2; the research moves it
up because "installing in background" with no feedback is the most visible
gap after Files.
- Installed apps: a card per app with a status dot. Start, stop and
  restart go through `/status` (M-08, WP0-4) with status polling. The
  app's tips (passwords, next steps) show on the detail page, and web links
  open in a Custom Tab. 0.5 ed.
- Container logs: follow toggle, timestamps, line count, search, share the
  log text. 1 ed.
- Install progress from the app-management events: a progress notification
  and a status chip on the card. 1.5 ed.
- Updates: an "Updates" tab with a count badge
  (`/v2/app_management/apps/upgradable`), Update all, and a per-app
  auto-update toggle (the web's Settings > Containers). 2 ed.
- Uninstall: a destructive sheet with a "delete app data" checkbox, the
  same question the web asks. 0.5 ed.
- App Store: the live catalog only (M-31), category chips, search, a
  "Not for this CPU" label, and a detail page with screenshots and Install.
  1 ed.
- Acceptance: installing an app from the phone shows progress to the end.
  "Update all" updates every listed app and the badge clears.

**WP1-17 Transfers and the share target (P1).** 3 ed.
- A Transfers screen: running, failed and done, with Retry, Cancel and
  "Show in folder".
- One persistent progress notification with Cancel, for uploads and
  downloads started from the phone. Long transfers outlive the screen.
- Share sheet: "Upload to NivaroOS" asks for a folder (last used first) and
  uploads through the v2 resumable protocol (M-12).
- Acceptance: 20 photos shared from the gallery upload with the screen
  closed. Cutting the network and restoring it resumes them.

**WP1-18 Trash and undo (P1).** 1.5 ed. A "Trash" location (restore,
empty). Delete shows a snackbar with UNDO. Touch deletes happen by
accident.

**WP1-19 Share links (P1).** 2 ed.
- "Share link" on a file or folder uses the server's Quick Share (an
  expiring link) with expiry chips (1 day, 7 days, 30 days). Then the
  Android share sheet opens with the URL, or a QR code.
- A "Shared" list shows active links; swipe to revoke.
- A link made on the LAN warns when the server has no public URL.
- Server: uses the existing Quick Share routes (verify expiry support).
- Acceptance: the link works from another network through the tunnel. A
  revoked link returns 404.

**WP1-20 Server profiles and connection (P1).** 3 ed.
- Each profile has an internal URL (used on the home Wi-Fi, matched by
  SSID, optionally BSSID) and an external URL (used everywhere else), as
  in Home Assistant.
- The app tries the internal URL first with a short timeout, falls back to
  the external one, and keeps the current screen when it switches.
- SSID matching needs "location all the time". The app explains why before
  asking; without it the app still works through the fallback order.
- A status dot per server in the profile list, and fast switching between
  profiles (M-17 rules apply).
- Tailscale: detect whether the server answers on its tailnet address and
  say so in the connection badge.
- Acceptance:
  - Leaving home Wi-Fi mid-session switches to the external URL within one
    request timeout, without a logout.
  - Coming home switches back.

**WP1-23 Viewers (P1 image, P2 video).** 2 ed. Image: pinch-zoom and a
swipe gallery through the folder (P1, 1 ed). Video: play MKV and HEVC
through the server's remux stream `/v1/file/stream` when the phone cannot
decode the file (P2, 1 ed).

**P2: the release after**

**WP1-3 Storage (P2).** 3 ed.
- A drive detail page: a health badge, temperature, SMART summary, "Run
  short test", and the standby timer shown read-only.
- USB eject (`DELETE /v1/disks/usb`) on USB rows. Mounts shown read-only.
- Format and pool creation are long jobs: start with `PUT` or
  `POST /v1/storage`, then poll `GET /v1/storage/jobs/:id`
  (`services/local-storage/route/v1.go:53-60`). They show progress and
  need the disk's name typed to confirm.

**WP1-6 VM console and Host Desktop quality (P2).** 5 ed.
- Tight or ZRLE decoding (zlib streams through `dart:io`
  `RawZLibFilter`) and `ExtendedDesktopSize` (auto-resize).
- A low-bandwidth mode on mobile data: lower quality, fewer frames.
- The Host Desktop toolbar: resolution, bandwidth profile, clipboard
  (WP1-5 sheet), caps lock.
- The rest of the VM form:
  - pause and resume;
  - USB/PCI passthrough and shared folders, shown read-only but kept on
    save (M-04, M-05);
  - extra disks;
  - ISO upload from the phone through the gateway route (verify the
    chunking the sidecar supports);
  - the VM Manager setup state with "Install libvirt".

**WP1-10 Settings (P2).** 3 ed.
- Account: change password.
- Scheduled tasks: list, enable, run now and the last log (creation stays
  on the web).
- Read-only: NivaroOS users, network interfaces, SMB shares.
- System updates: the changelog and a reboot prompt.
- Privacy text for the companion and backup features.
- The Play policy re-check and data-safety form from the first plan are
  shelved with the Play flavor (§7.1).

**WP1-11 Widgets, tiles and shortcuts (P2).** 3 ed.
- Home-screen widgets:
  - Status: CPU, RAM, storage and last backup.
  - Action button: "Back up now", "Start app X", "Wake VM", with an
    optional app-lock check.
- Widgets match the theme (Material You, light, dark or transparent).
  They refresh every 30 minutes through the WorkManager poll and show
  "updated X ago" honestly.
- Quick Settings tiles "Back up now" (after Phase 2) and "Server status".
  Launcher shortcuts for Files, Terminal and Downloads.

**WP1-12 Tailscale (P2).** 1 ed. A Tailscale page: status, peers with
status dots, exit node and subnet prefs (`/v1/tailscale/prefs`), and the
login URL flow (the route fix is in WP0-4, M-10). Suggest the Tailscale app
when the server is reachable only on a tailnet.

**WP1-21 Pairing by code (P2).** 3 ed, of which 1.5 server. The idea is
Jellyfin's Quick Connect: the app shows a 6-character code and a QR, and a
signed-in admin approves it in the web UI. The app then receives its
credential without the password ever being typed on the phone. This ties
into the scoped device credential (S-07, D5). Every approval creates a
"device paired" notification.

**WP1-24 Dashboard cards (P2).** 2.5 ed.
- A GPU card when the GPU sidecar is present (VRAM, temperature, power,
  processes). 1 ed.
- The server speed test (`/v1/sys/speedtest*`). 0.5 ed.
- Cards can be reordered and hidden. 1 ed.
- A "Last backup: X ago" card once Phase 2 ships.

**WP1-25 Activity (P2).** 2 ed. One screen of running jobs across
modules (backups, downloads, storage jobs, app installs), alerts first,
with pause, run and cancel where the module allows it. TrueNAS and QNAP
users look at this first.

**WP1-26 Files extras (P2).** 3.5 ed.
- A "Locations" bottom sheet: storage, USB, cloud, phones. 1 ed.
- "Save to…" through the Android file picker (SAF). 0.5 ed.
- An "Info" sheet: size, dates, permissions, folder size. 0.5 ed.
- Compress and extract as batch tasks with progress (M-30). 1 ed.
- "Back up this folder", which opens the backup quick-create. 0.5 ed.

### 4.5 Web UI parity

The full inventory is summarised here. Rows that are already in the app
and need no work are left out.

| Area | Ported (work package) | Stays in the web UI (the app links there) |
|---|---|---|
| Files | multi-select, conflicts, rename, new folder/file (WP1-15); transfers, share target (WP1-17); trash (WP1-18); share links (WP1-19); locations, info, compress, save-to (WP1-26); viewers (WP1-23) | SMB share setup, cloud account setup (OAuth and iCloud flows), Doc/Excel viewing ("Open with…") |
| Apps | status, logs, install progress, updates, auto-update, uninstall, catalog (WP1-9) | the full app-settings editor, custom install forms, app sources, icon editor, folders, taskbar pins |
| VMs | power incl. pause, clipboard, console quality, host toolbar, ISO upload (WP1-5, WP1-6) | network creation, passthrough editing |
| Download Station | everything except the Lite Browser (WP1-2) | Lite Browser and ad blocker (not offered at all) |
| Backup & Sync | overview, run, decide, versions, restore (WP1-1); phone backup (Phase 2) | the job wizard |
| Storage | health, SMART test, eject, format jobs (WP1-3) | fstab, persistent mounts, widget drive visibility |
| Settings | password, scheduled tasks, read-only lists, update changelog (WP1-10); Tailscale (WP1-12) | APT package manager, system/Samba user editing, hostname and port, appearance and wallpaper |
| Shell | notifications (WP1-4); GPU card, server speed test (WP1-24); activity (WP1-25) | dock, window manager, date/time pill, settings search |
| App-only | app lock (WP1-8), widgets and tiles (WP1-11), profiles with two URLs (WP1-20), pairing (WP1-21) | — |

Web features rated P3 (low value on a phone) are in the §6 backlog.

### 4.6 Release plan

| Release | Contents | Effort |
|---|---|---|
| R1 "works and is safe" | Phase 0 (§3.4) + P0: WP1-13 to WP1-16, WPR-1, WPR-2, WPR-3, WPR-5 | ~18 + 11.5 + 8 = ~37.5 ed |
| R2 "daily driver" | P1: WP1-1, -2, -4, -5, -7, -8, -9, -17, -18, -19, -20, -23 (image), WPR-4, WPR-6 | ~39.5 + 6 = ~45.5 ed |
| R3 "phone backup" | Phase 2 first release (§5.10), in parallel with P2 | ~40 ed |
| R4 "complete" | P2: WP1-3, -6, -10, -11, -12, -21, -24, -25, -26, WP1-23 (video) | ~27 ed |

Phase 2 depends on WP1-1 (Backup screens), WP1-4 (notifications) and
WP1-14 (restricted settings helper, secure storage). It can start on the
server (WP2-1) during R2.

### 4.7 Questions for the owner

1. **Google developer verification (M-35).** Register the package name and
   the existing release key (no rotation) under a free limited-distribution
   account (up to 20 devices) or a full developer account before global
   enforcement in 2027? Without it, installs on certified phones need ADB or
   the advanced flow.
2. **Longer "browse my phone" sessions (WP1-7).** Is a time-limited
   session (default 1 h) acceptable, or should a `specialUse` foreground
   service allow always-on sharing?

---

## 5. Phase 2: Android phone backup

### 5.1 What can be backed up on a non-rooted phone

"Play" is whether the needed permission is allowed for an app distributed
on Google Play; "full" is the sideload/F-Droid flavor, which Play policy does
not bind.

| Category | How | Permission | Play | Restore | Verdict |
|---|---|---|---|---|---|
| Photos and videos | MediaStore (`Images`, `Video`), original bytes | `READ_MEDIA_IMAGES`, `READ_MEDIA_VIDEO` (13+), `READ_EXTERNAL_STORAGE` (≤12), `ACCESS_MEDIA_LOCATION` | allowed for apps whose core purpose needs broad media access (photo backup is one), with the Play Console declaration; partial access (14+ "selected photos") must be handled | insert through MediaStore | **v1** |
| Audio, voice recordings | MediaStore `Audio` | `READ_MEDIA_AUDIO` | allowed | MediaStore | v1 (opt-in) |
| Other folders (Documents, Download, app exports) | Storage Access Framework tree (`ACTION_OPEN_DOCUMENT_TREE`, persisted grant) | none | allowed | into a chosen SAF tree | **v1** |
| Whole shared storage | direct file API | `MANAGE_EXTERNAL_STORAGE` | restricted; "backup and restore" is a listed acceptable use, but approval is discretionary | same | full flavor v1; play only if approved |
| Contacts | `ContactsContract` → vCard (`Contacts.CONTENT_MULTI_VCARD_URI`) | `READ_CONTACTS` | allowed (runtime permission, prominent disclosure) | system importer (no permission) or direct insert (`WRITE_CONTACTS`) | **v1** |
| Calendar | `CalendarContract` → iCalendar | `READ_CALENDAR` | allowed | insert into a local calendar (`WRITE_CALENDAR`) | v1 (local calendars by default) |
| SMS and MMS | `Telephony` provider (`content://sms`, `content://mms` + parts) | `READ_SMS` | restricted to the default SMS app and approved exceptions ("backup and restore" is an exception category; declaration and review required, approval not guaranteed) | only the **default SMS app** may write; needs the role for the restore | full flavor v1; play after approval |
| Call log | `CallLog.Calls` | `READ_CALL_LOG` / `WRITE_CALL_LOG` | same restricted group as SMS | write needs `WRITE_CALL_LOG` | full flavor v1; play after approval |
| Installed apps list | `PackageManager` with a `<queries>` for `MAIN`/`LAUNCHER` | none | allowed (no `QUERY_ALL_PACKAGES` needed for launchable apps) | Play Store links | **v1** |
| App APKs | `ApplicationInfo.sourceDir` + `splitSourceDirs` (world-readable) | none to read | reading is fine; installing needs `REQUEST_INSTALL_PACKAGES` (restricted) | `PackageInstaller` session with all splits | full flavor; play: list + links only |
| Other apps' data | not reachable (`/data/data/<pkg>` is private; `Android/data`, `Android/obb` blocked since 11) | — | — | — | **no** (§5.2) |
| WhatsApp, Signal local backups | their own backup files in shared storage (`Android/media/com.whatsapp/…`, Signal's chosen backup folder) | SAF tree grant, or all-files | allowed via SAF | copy back before the app's first start | v1 as a guided SAF folder |
| Wi-Fi networks, device settings | saved Wi-Fi passwords are not readable by apps since Android 10; system settings need privileged access to restore | — | — | — | **no**; point to Google's backup |
| This app's own settings | server profiles (without tokens), backup config | none | allowed | import | v1 |

Sideload notes (§7.1): only the `full` flavor is built, so the "full"
column applies and the "Play" column is kept for reference only. On
Android 15+, a sideloaded app gets `READ_SMS`, `READ_CALL_LOG` and the SMS
role only after the user allows restricted settings for it (M-34, §5.2).

### 5.2 Limits, stated plainly (these go into the app's help screen)

- **Other apps' data cannot be backed up.** Android isolates each app's
  private storage. Android's own backup (Google One) and `adb backup` are
  the only system paths; `adb backup` is deprecated and returns nothing for
  apps targeting Android 12+ unless they opt in, and Google's backup cannot
  be read by other apps. Shizuku runs with `adb shell` rights, which also
  cannot read other apps' private data. Only root can, and root is a
  non-goal. What the app does instead: back up the files apps export
  themselves (WhatsApp/Signal backup files, exports in Documents), the app
  list, and APKs (full flavor).
- **RCS chats are not in the SMS provider.** Google Messages keeps RCS
  messages in its own storage, and outgoing RCS is not in `content://sms`.
  Only allow-listed apps can read RCS. Only SMS and MMS are backed up.
  Samsung Messages can store RCS as SMS/MMS, so on Samsung phones a restore
  may show duplicates of chats that the RCS store still has; the restore
  screen warns about this.
- **SMS and call log need "Allow restricted settings" (Android 15+).**
  Android treats these permissions and the SMS role as restricted for apps
  not installed from a store. Until the user opens App info → ⋮ → **Allow
  restricted settings**, the permission prompt is greyed out. The app
  detects this and walks the user through it with pictures (WP1-14 helper).
  It happens once per install.
- **Restoring SMS needs NivaroOS to be the default SMS app for a moment.**
  Android lets only the default SMS app write messages. The restore flow asks
  for the role, writes, then asks the user to switch back. To be eligible for
  the role the app must include the standard SMS-app components (receivers
  for `SMS_DELIVER` and `WAP_PUSH_DELIVER`, a respond-via-message service, a
  compose activity). They are in the full flavor only and do nothing except
  store incoming messages while the role is held.
- **Contacts and calendars synced to Google are already in Google.** The app
  still exports them (a copy you own), but on restore it offers "local
  contacts only" by default to avoid duplicates.
- **WhatsApp restore follows WhatsApp's rules**: same phone number, the file
  copied back before first launch, and for end-to-end encrypted backups the
  user's password or 64-digit key. The app copies files; it cannot decrypt
  or merge chats.
- **Background time is limited.** Android may defer work on battery saver,
  Doze and app standby buckets; some vendors kill background work harder
  (the app links to per-vendor guidance at dontkillmyapp.com). Big first
  backups should run with the app open or as a user-started transfer
  (§5.5). Silent failure is the top complaint about Immich, Synology Photos
  and Nextcloud backups, so the app always shows when the last successful
  run was and warns when it is stale (§5.5 "Health checks"). A backup also
  runs whenever the app is opened, which works even on phones that kill
  background work.
- **Photos with location.** Without `ACCESS_MEDIA_LOCATION` Android removes
  GPS tags from the bytes the app reads. The app requests it and reads with
  `MediaStore.setRequireOriginal()`; if the user refuses, the backup is marked
  "location removed" per file.
- **HEVC video transcoding.** Android 12+ can hand apps that don't declare
  HEVC support a transcoded AVC copy. The app declares support for HEVC and
  HDR (media capabilities resource in the manifest) so it always gets the
  original. Test: SHA-256 on phone equals SHA-256 on server.

### 5.3 Design decisions

**D1. Where backups land: the Backup & Sync module, as a new "device" job
type. Not the companion device folder.**
Why:
- The module already has what a phone backup needs: runs with status and
  logs, versions, retention, restore and download, stale-backup
  notifications ("no successful backup of Pixel 8 in 7 days"), the admin
  check, and the UI. The companion folder (`/DATA/Companion/<name>`,
  `companion.go:112-119`) is a flat folder with none of these, is shared by
  all users (S-03), is keyed by a name that the dedup bug can reassign
  (S-02), and its upload handler had the traversal bug (S-01).
- A server-pull design (the server reads the phone through the companion
  file server) cannot work: the phone is usually not reachable (NAT, Doze,
  tunnels, S-04), and contacts, SMS, call log and calendar must be exported
  on the phone anyway. The phone pushes.
- 3-2-1 comes free: the device's backup folder is an ordinary location, so a
  normal Backup job can copy it to USB or cloud.
Cost: phone backup needs the optional module installed (default yes in the
installer). When `GET /v1/backup/health` does not answer, the app says
"Install Backup & Sync on the server to back up this phone" and offers
nothing else; there is no second code path through the companion folder.
The companion feature stays for "browse my phone from the server" only.

**D2. Storage format: browsable files for media and folders, snapshot files
for everything else, one manifest.**
- Media and SAF folders are stored as normal files under their phone path
  (`DCIM/Camera/PXL_20260901_101500123.jpg`), original bytes, original
  modification time. Users can browse them in Files and point Immich or
  PhotoPrism at the folder read-only.
- Contacts, SMS/MMS, call log, calendar and the app list are **snapshots**:
  one file per run (`contacts/2026-09-24T03-00Z.vcf`,
  `messages/sms-2026-09-24T03-00Z.xml`, `calllog/calls-2026-09-24T03-00Z.xml`,
  `calendar/<calendar>/….ics`, `apps/….json`, APKs under
  `apps/apk/<package>/<versionCode>/`). A snapshot identical to the
  previous one (same SHA-256) is not stored again.
- Formats are chosen so the files are useful without NivaroOS:
  - SMS and MMS use the XML schema of SMS Backup & Restore (SyncTech), the
    most used Android SMS backup app. `<smses count>` holds `<sms address
    date type body read …>` and `<mms>` elements, with parts in
    `<parts><part seq ct data=base64 text>` and addresses in
    `<addrs><addr type=137/151>`.
  - Calls use the same app's `<calls><call number duration date type
    presentation>`.
  - Users can move to or from SMS Backup & Restore, and existing parsers
    and viewers read the files. The files are plain XML, not compressed
    (SMS Backup & Restore reads `.xml`) and not encrypted by default (§7.3).
  - Contacts are vCard (`.vcf`), calendars iCalendar (`.ics`), one file per
    calendar.
- Messages and calls are incremental:
  - Each run writes a file with only the new items.
  - The server keeps a merged index of item keys per device. The key for a
    message is SHA-256 of `address | date | type | body`; for a call it is
    `number | date | duration`.
  - On request the server builds one complete SMS Backup & Restore file
    from all runs (§5.4), so one file restores everything.
  - Contacts carry a manifest with a hash per contact, so a restore can
    skip exact duplicates.
- A changed media file (edited on the phone) keeps the old bytes as a version
  under the module's versions area (same mechanism as `mirror` with
  versions); retention per job.
- Deleted on the phone: **never deleted on the server by default.** The
  manifest records `deleted_on_device_at`; the restore browser can show or
  hide those files; an optional job setting removes them from the backup N
  days later (off by default). Items in the phone's trash (`IS_TRASHED`,
  Android 11+) are treated as deleted for this rule, and are backed up if
  they were never backed up before.
- Dedup: SHA-256 per file. Before uploading, the phone sends a batch of
  `{path, size, mtime, sha256}`; the server answers per item `have`,
  `have_elsewhere` (same content at another path in this device's backup →
  server links or copies locally, no upload) or `need`. A photo saved in two
  apps is uploaded once. Hashing costs CPU: hashes are computed once and
  cached in the phone's index keyed by `(media id, generation, size)`.
- What to upload is decided by content hash against the server's list,
  never by a "last sync time". Synology Photos syncs by date and silently
  skips older photos added later.
- The server keeps an "excluded hashes" list per device. A file the owner
  deletes from the backup on the server goes on it, so the phone does not
  upload it again (Immich users report re-uploads of deleted duplicates).
- A run is complete only when its session is finished and the snapshot
  index is written, and that is the last step (as in Seedvault). An
  interrupted run never looks complete, and the next run resumes it.

**D3. Upload protocol: tus 1.0 subset in the backup service.**
Creation, core (PATCH with `Upload-Offset`), HEAD for resume, termination,
plus a final `sha256` check before the file is moved into place
(write to a temp name, fsync, rename). Chunk size 8 MiB (proxy limits, §3.1
M-12). A standard protocol with existing client libraries and clear resume
semantics; the core v2 upload endpoint writes to arbitrary paths outside the
module and knows nothing about manifests, so it is not reused.

**D4. The phone engine is native Kotlin, with Flutter UI.**
WorkManager workers run without an Activity and without a Flutter engine.
The current code shows what a headless Dart isolate costs (plugins missing
from the background engine, `device_sync_service.dart:208-218`; crash
workarounds in `background_sync_isolate.dart:23-33`). A Kotlin module
(`android/app/src/main/kotlin/…/backup/`) owns scanning, the local index
(Room/SQLite), hashing, uploads (OkHttp), exports and restore, and talks to
Flutter through a typed Pigeon channel for status and settings. The UI stays
Dart.

**D5. Credentials: a scoped, revocable device token.**
Enrolling the phone (logged in, admin) calls `POST /v1/backup/devices` and
receives a random 256-bit device token, stored in the Android Keystore
(`EncryptedSharedPreferences` or a Keystore-wrapped key). The backup service
stores only its hash and accepts it **only** on `/v1/backup/devices/{id}/*`.
It does not expire on its own, survives password changes (the owner
revokes it from the web UI or the phone), and is rotated by the server every
30 days on a normal request (new token in a response header, old one valid
for 24 h). A lost phone is revoked in one click. This fixes S-07 without
touching user-service.

**D6. Encryption.**
- In transit: https is required when the server is not on the LAN; plain
  http on the LAN only after an explicit "not encrypted" confirmation per
  profile; pinned self-signed certificates (WP1-8) count as https.
- At rest, default: the files are stored as-is on the chosen server volume,
  readable by admins, because browsability is the point for photos. This is
  stated in the enrolment screen.
- At rest, optional per category, **off by default for every category**,
  including SMS/MMS and call log (owner decision §7.3): client-side
  encryption on the phone with a key derived
  from a recovery passphrase (Argon2id; the app generates a 12-word
  passphrase, shows it once, requires "I saved it"). Snapshot files are
  encrypted with AES-256-GCM in 1 MiB chunks; the server never sees the key.
  Losing the passphrase loses those snapshots; the app says so. Encrypted
  categories are not browsable on the server, and encrypted SMS files are
  no longer readable by SMS Backup & Restore.

**D7. One job per phone, categories inside it, schedules per category.**
The device job has categories, each with its own schedule and conditions:

| Category | Default schedule | Default conditions |
|---|---|---|
| Photos and videos | on new media (within ~15 min) + daily catch-up | Wi-Fi (unmetered), battery not low |
| Folders | daily | Wi-Fi, charging |
| Contacts, calendar | daily | any network, battery not low |
| SMS/MMS, call log | daily | any network, battery not low |
| Apps list | weekly | Wi-Fi |
| APKs | weekly | Wi-Fi, charging |

Global switches: "Use mobile data" (off), "Only while charging" (off), "Large
videos only on Wi-Fi" (on), "Pause backups" (until tomorrow / until turned
on).

Triggers a category can use (as in PhotoSync), each combinable with
"unmetered only":
- new media;
- on Wi-Fi, optionally only on a named SSID (this needs location
  permission, explained before asking);
- charger connected;
- a schedule.

Media include and exclude lists work per album (bucket), with a separate
policy for videos.

### 5.4 Server design (in `services/backup`)

**Data model** (tables in the existing `backup.db`):

```go
// device registered for phone backup
type Device struct {
    ID          string    // "dev_" + random
    OwnerUserID int
    Name        string    // "Pixel 8"
    Platform    string    // "android"
    OSVersion   string
    AppVersion  string
    JobID       string    // the device job (type "device")
    TokenHash   []byte    // sha256 of the current device token
    PrevHash    []byte    // previous token during rotation grace
    RotatedAt   time.Time
    RevokedAt   *time.Time
    LastSeenAt  time.Time
    CreatedAt   time.Time
}

// one row per file ever backed up for a device (media and folders)
type DeviceFile struct {
    DeviceID     string
    Category     string // "media" | "folder:<tree id>"
    Path         string // relative, as on the phone ("DCIM/Camera/…")
    Size         int64
    MTime        time.Time
    SHA256       string
    MediaID      int64  // MediaStore _ID, 0 for SAF
    TakenAt      *time.Time
    LocationKept bool
    FirstSeenAt  time.Time
    DeletedOnDeviceAt *time.Time
    StoredPath   string // under the job destination
}

// one row per snapshot (contacts, sms, calllog, calendar, apps, apk)
type DeviceSnapshot struct {
    DeviceID  string
    Category  string
    TakenAt   time.Time
    File      string
    Items     int
    SHA256    string
    Encrypted bool
}

// resumable upload
type Upload struct {
    ID, DeviceID, Path, SHA256 string
    Size, Offset int64
    CreatedAt, ExpiresAt time.Time // 7 days
}
```

The device job is a `Job` with `type: "device"`, one source endpoint of new
kind `device` (`RefID` = device id; not browsable as a source for other job
types except through its destination folder) and a normal destination
(`volume`, `usb` or `merge`; cloud and SMB are refused as a device-job
destination in v1 because uploads must land on a local POSIX filesystem;
copy onward with a second job). Default destination:
`/DATA/Backup/<device name>/` on the data volume (owner decision, §7).

**Runs.** A phone session is recorded as a run of the device job
(trigger `device`, kind per category), so Activity, logs, notifications and
the stale check work unchanged. A session starts with `POST …/sessions`,
reports progress with each batch, and ends with `…/sessions/:sid/finish`
(status, counts, errors). A session with no activity for 30 min is closed as
`interrupted`. The stale notification threshold is per device (default 7
days without a successful media run).

**Endpoints** (all under `/v1/backup`, through the existing gateway route):

| Method and path | Auth | Purpose |
|---|---|---|
| `POST /devices` | user JWT, admin | enrol: `{name, platform, os_version, app_version}` → `{device, token, job}` |
| `GET /devices`, `GET /devices/:id` | user JWT | list and detail (web UI and app) |
| `PUT /devices/:id` | user JWT, admin | rename, categories, schedules, retention, destination |
| `POST /devices/:id/revoke` | user JWT, admin | revoke the token |
| `DELETE /devices/:id?purge_data=` | user JWT, admin | remove, optionally with its data (audited) |
| `GET /devices/:id/config` | device token | categories, schedules, conditions, server limits |
| `POST /devices/:id/sessions` | device token | start a run: `{category}` → `{session_id, run_id}` |
| `POST /devices/:id/sessions/:sid/check` | device token | batch of ≤ 500 `{path,size,mtime,sha256}` → per item `have`/`have_elsewhere`/`need` |
| `POST /devices/:id/uploads` | device token | tus creation: `Upload-Length`, metadata `path`, `sha256`, `mtime`, `category` |
| `HEAD /devices/:id/uploads/:uid` | device token | current offset |
| `PATCH /devices/:id/uploads/:uid` | device token | append a chunk (`Upload-Offset`, ≤ 8 MiB) |
| `DELETE /devices/:id/uploads/:uid` | device token | abandon |
| `POST /devices/:id/sessions/:sid/deleted` | device token | batch of paths no longer on the phone |
| `POST /devices/:id/sessions/:sid/finish` | device token | end the run with counts and errors |
| `GET /devices/:id/files?path=&include_deleted=` | device token or user JWT | browse for restore |
| `GET /devices/:id/files/content?path=` | device token or user JWT | download one file (Range) |
| `GET /devices/:id/snapshots?category=` | device token or user JWT | list snapshots |
| `GET /devices/:id/snapshots/:sid/content` | device token or user JWT | download a snapshot |
| `GET /devices/:id/snapshots/full?category=sms\|calllog` | device token or user JWT | one complete SMS Backup & Restore XML built from all runs (streamed) |
| `POST /devices/:id/sessions/:sid/check-items` | device token | messages and calls: batch of item keys → which are new |
| `GET /devices/:id/excluded`, `POST /devices/:id/excluded` | device token (read), user JWT admin (write) | the hashes the phone must not upload again |
| `POST /devices/:id/snapshots/import?category=sms\|calllog` | user JWT, admin | import an SMS Backup & Restore XML file made elsewhere |

Rules: every path is relative, cleaned and joined under the device folder
with a within-root check (tests with `..`, absolute paths, symlinks, NUL,
very long names, names that differ only in case on case-insensitive
destinations); a finished upload whose SHA-256 differs is deleted and
reported; free-space precheck before accepting an upload (destination
free space minus a 5 % reserve); per-device rate limit on `check`; uploads
older than 7 days are purged; the device token never works on any other
route. Events: `nivaroos:backup:run-*` as today, plus
`nivaroos:backup:device-changed`.

**Stale alerts.** The stale rule is per device and per category (default:
7 days without a successful media run, 3 days for messages). It sends a
notification through the feed (WP1-4), so the phone and the web UI both
see it even when the phone itself is the part that stopped working.
Imports of SMS Backup & Restore XML files made elsewhere are accepted as
snapshots of their category (`POST /devices/:id/snapshots/import`, user
JWT admin), so a user moving from that app keeps their history.

**Web UI.** A "Phones" section in the Backup app: devices, last run per
category, storage used, revoke, and a restore browser for device files
(same components as §12.7 of the Backup spec).

### 5.5 Phone design

**Local index** (Room): `media(volume, media_id, generation_modified,
relative_path, display_name, mime, size, date_modified, date_taken, sha256,
state, server_state, uploaded_at, deleted_seen_at)`, `saf_file(tree_id,
document_id, path, size, last_modified, sha256, state)`,
`snapshot(category, taken_at, sha256, uploaded)`, `session(id, category,
started_at, finished_at, status, bytes, files, errors)`, `settings`.

**Media scan (incremental).**
1. For each volume from `MediaStore.getExternalVolumeNames()` (internal and
   SD card), read `MediaStore.getGeneration(volume)`.
2. Query rows with `GENERATION_MODIFIED > last_generation` (Android 11+);
   on Android 10 and older, fall back to `DATE_MODIFIED`/`SIZE` comparison
   for the whole collection.
3. A full scan once a week (and after the volume's `MediaStore.getVersion()`
   changes, which means the index was rebuilt) finds deletions: indexed rows
   no longer present → `deleted` batch to the server.
4. Include `IS_PENDING = 0` rows only; include trashed rows with
   `QUERY_ARG_MATCH_TRASHED` so first-time trashed items are still backed up.
5. Types: everything MediaStore lists as image or video, including HEIC/HEIF,
   AVIF, DNG and other raw formats the camera app registers, motion photos
   (JPEG with embedded MP4, kept as one file), and burst sets. Raw files
   some camera apps write outside MediaStore are covered by a SAF folder.
6. Read with `setRequireOriginal(uri)` (needs `ACCESS_MEDIA_LOCATION`) and
   the declared HEVC/HDR capabilities, so the bytes are the original.

**Triggers.**
- New media: a WorkManager job with content URI triggers on
  `MediaStore.Images/Video.Media.EXTERNAL_CONTENT_URI`
  (`Constraints.Builder().addContentUriTrigger(…, true)`, re-enqueued after
  each run) plus the category's constraints.
- Schedules: `PeriodicWorkRequest` per category (minimum 15 min; daily and
  weekly as configured), with `setRequiredNetworkType(UNMETERED)` when Wi-Fi
  only, `setRequiresCharging`, `setRequiresBatteryNotLow`,
  `setRequiresStorageNotLow`.
- Long runs: the worker calls `setForeground()` with a progress notification
  (foreground service type `dataSync`), works in slices of at most 30 min or
  2 GB, records progress in the index, and re-enqueues itself. This stays far
  under the Android 15 limit of 6 h/24 h for `dataSync`, and every slice is
  resumable.
- "Back up now" and the first full backup: on Android 14+ a user-initiated
  data transfer job (`JobInfo.Builder.setUserInitiated(true)`, permission
  `RUN_USER_INITIATED_JOBS`), which the system lets run longer; on older
  versions an expedited WorkManager request with a foreground notification.
  A user-initiated job needs a network constraint and calls
  `setNotification()` in `onStartJob`. It can be scheduled only while the
  app is visible and is exempt from standby-bucket quotas. This is Google's
  guidance for user-started transfers (Maps saw 10 % better download
  reliability with it). The first run is offered as a "focused backup":
  the app suggests Wi-Fi and a charger so a large first run isn't cut off.
- After boot or app update WorkManager restores its schedules; no
  `RECEIVE_BOOT_COMPLETED` receiver of our own is needed for backup, and no
  backup starts from boot.
- Every trigger is registered again on every app start. Nextcloud had
  auto-upload rules that were armed only once, at creation, and then
  silently stopped.
- At target 36, jobs that run alongside a foreground service or start in
  the foreground count against the job quota. The design does not rely on
  a foreground service keeping a job alive. Every upload is resumable and
  idempotent.

**Health checks.** Immich v3 added these after users lost photos without
noticing:
- Setup checks battery optimisation and the notification permission.
  The battery exemption (`REQUEST_IGNORE_BATTERY_OPTIMIZATIONS`) is asked
  politely: only after the user turns on automatic backup, with a
  one-screen explanation, and with a link to dontkillmyapp.com for the
  phone's maker.
- A status tile shows "Background: restricted / unrestricted".
- A "Last backup: X ago" card sits at the top of the Backup tab and on the
  dashboard.
- A local warning fires when no backup has run for 24 h while there is
  pending media. The server's stale alert (§5.4) covers a phone that is
  off.
- The queue says why each item waits: "waiting for Wi-Fi", "battery
  saver", "failed: 413 (proxy limit)". On Android 16 the reason a job was
  skipped comes from `getPendingJobReasonsHistory`.

**Upload loop.** Per session: build a batch of up to 500 changed items →
`check` → for `need` items, upload in size order (small first, so a slow
network still finishes many files), 2 in parallel, 8 MiB chunks; on any
network error keep the tus upload id and resume from `HEAD` next time; on
401 with the device token stop and show "This phone was removed from
backup" (revoked); on 507/`no_space` stop the session and notify.
Constraint changes mid-run (Wi-Fi lost, charger unplugged) stop the worker
cleanly; WorkManager restarts it when the constraints hold again.

**Exports.**
- Contacts: collect lookup keys, read
  `Uri.withAppendedPath(Contacts.CONTENT_MULTI_VCARD_URI, joinedKeys)` in
  pages (the provider limits the key list length), concatenate into one
  vCard file; include photos (the provider includes `PHOTO`); record the
  account per contact in a sidecar JSON so restore can target "local" or the
  original account.
- Calendar: one iCalendar file per calendar with `VEVENT`s from
  `CalendarContract.Events` + `Reminders` + `Attendees`; recurring events as
  RRULE, not expanded.
- SMS/MMS: SMS Backup & Restore XML (D2). It holds the `sms` fields
  `address`, `date`, `date_sent`, `type`, `read`, `body`, `sub_id`, plus MMS
  with addresses and parts (`content://mms/part`); part bytes are base64 in
  the `data` attribute, as that format requires. Only items whose key the
  server does not have are written (`check-items`).
- Call log: SMS Backup & Restore `<calls>` XML from `CallLog.Calls`, also
  incremental.
- Contacts: a per-contact hash manifest (`contacts/….manifest.json`) next
  to the vCard. `QueryParameterVcardNoPhoto` is not used, because photos
  are kept.
- Apps: package, label, version name/code, installer package, first install
  time, Play link; APKs (full flavor) copied from `sourceDir` and
  `splitSourceDirs`; skip system apps unless updated; skip apps above a size
  limit (default 500 MB).

### 5.6 Restore flows

| Category | Flow |
|---|---|
| Photos and videos | Browse the device backup (or another device's, for a new phone) by folder or month; select, or "restore everything not on this phone" (hash compare against the local index). Files are written through `MediaStore` insert with `RELATIVE_PATH` = original folder, `IS_PENDING=1` while writing, `DATE_TAKEN` and `DATE_MODIFIED` from the manifest; EXIF stays in the bytes. Resumable, same slicing as backup. Name clashes get " (restored)". |
| Folders | Pick a target SAF tree (default: the original tree if still granted); files are written with their relative paths; existing files with the same hash are skipped. |
| Contacts | Default: hand the vCard file to the system Contacts app (`ACTION_VIEW`, `text/x-vcard`), which asks the account and handles duplicates, no permission needed. Advanced: direct insert with `WRITE_CONTACTS` into a chosen account, skipping contacts whose name and first phone number already exist. |
| Calendar | Insert into a local "NivaroOS restored" calendar through the sync-adapter URI (`CALLER_IS_SYNCADAPTER`, `ACCOUNT_TYPE_LOCAL`), or into a chosen writable calendar; skip events whose UID already exists. |
| SMS/MMS | A three-step screen: (1) make NivaroOS the default SMS app (after the restricted-settings check, M-34; `RoleManager.createRequestRoleIntent(ROLE_SMS)`), (2) restore, (3) switch back to your SMS app. Messages are written in date order in batches of 500 per transaction, with progress and resume; duplicates are skipped by the item key (address + date + type + body hash). The source can be this phone's backup, another phone's, or any SMS Backup & Restore file on the server. The app always ends at the default-apps screen, even after a failure, and a notification reminds the user while NivaroOS still holds the role. If the role is refused, nothing is written. On Samsung, the RCS duplicate warning (§5.2) is shown first. |
| Call log | Insert with `WRITE_CALL_LOG` (restricted settings as above), in batches, dedup on number + date + duration. |
| Apps | Checklist of apps not installed on this phone with "Open in Play Store" (`market://details?id=…`). Full flavor: "Install from backup" for APKs, one `PackageInstaller` session per app with base and all splits; the user confirms each install; skipped when the phone's ABI or SDK does not match. |
| WhatsApp, Signal files | Guided: install the app, don't open it; the restore copies the folder back into the granted SAF tree (for WhatsApp `Android/media/com.whatsapp/WhatsApp/`); then open the app and choose its own restore. |

Every restore follows the same pattern, taken from the best backup apps:
1. **Preview**: counts and date range per category before anything is
   written.
2. **Mode**: "merge (skip duplicates)" (default) or "only newer than
   <date>".
3. **Resumable**: an interrupted restore continues where it stopped.
4. **Summary**: restored / skipped / failed, with a copyable log.

Restore uses the device token of the phone doing the restore; restoring
another device's backup needs a one-time user login (admin) on the phone,
not the other device's token.

### 5.7 UI flow

1. **Enrol** (Backup tab → "Back up this phone"): server check (module
   installed, admin) → name → destination (default shown with free space) →
   categories with plain descriptions, each with its permission prompt at the
   moment it is switched on (never all at once) → conditions → encryption
   passphrase only if the user turned encryption on → a first-run checklist
   (server reachable, permissions, battery optimisation, notifications, a
   test upload of one file) → "Start first backup". The first backup runs
   as a user-initiated "focused backup" with a progress screen and an
   estimate.
2. **Status** (Backup tab home): per category last success, pending items
   ("312 photos waiting for Wi-Fi"), current activity, errors with a Fix
   action (grant permission, free space on server, sign in again), "Back up
   now", "Pause". The status sits at the top of the Backup tab, not under
   settings, and the dashboard repeats "Last backup: X ago".
2a. **Free up space**: remove from the phone items the server has
   confirmed by hash, filterable by date range, size and folder, with a
   count and size before anything is deleted (through MediaStore delete
   requests, which Android confirms with the user).
3. **Settings per category**: schedule, conditions, include/exclude folders
   (for media: which buckets, e.g. exclude `Screenshots`), deleted-on-phone
   rule, encryption (fixed after first run of that category unless reset).
4. **Restore**: device picker (this phone, other phones of this account),
   category, then the flows in §5.6.
5. **Help**: the §5.2 limits in plain words, vendor background-restriction
   guidance, what the server can see.

Disclosure screens appear before each sensitive permission (contacts,
calendar, SMS, call log, media location) and say what is sent where. Play
does not require them for a sideloaded app, but they stay: they are how
users learn what the server will see.

### 5.8 Flavors and Play policy

Shelved by owner decision §7.1: only `full` is built and every row's
`full` column applies. The table is kept for the day a Play build is
wanted.

| Item | `full` (sideload / F-Droid) | `play` |
|---|---|---|
| Media, folders (SAF), contacts, calendar, apps list | yes | yes, with Photo/Video permissions declaration and data-safety form |
| `MANAGE_EXTERNAL_STORAGE` (whole storage) | yes | only after an approved all-files-access declaration |
| SMS/MMS, call log backup and restore; SMS-app components | yes | only after an approved SMS/Call Log exception declaration; until then removed by the flavor manifest |
| APK backup | yes | yes (reading is not restricted) |
| APK install on restore, self-update | yes (`REQUEST_INSTALL_PACKAGES`) | no; Play links only |
| Notifications | WorkManager poll; later UnifiedPush | same |

F-Droid: no proprietary dependencies (no Firebase, no Play Services), and a
reproducible build; check every Flutter plugin's native dependencies before
submitting.

### 5.9 Tests

- **Kotlin unit** (Robolectric + fake ContentResolver): generation-based
  diff (new, edited, deleted, trashed, pending), MediaStore version change
  forces full scan, hash cache invalidation, batch building, tus resume from
  a given offset, 401/507 handling, slice limits, vCard paging with 1 000
  contacts, SMS/MMS export shape, restore dedup rules.
- **Go** (`services/backup`, real temp dirs): enrol/revoke/rotate, device
  token accepted only on device routes, every path attack (`..`, absolute,
  symlink out of root, NUL, 4 KiB name), tus create/patch/head/abandon,
  wrong SHA-256 deleted, offset mismatch 409, free-space refusal, check
  answers (`have`, `have_elsewhere` links without upload), deleted marking,
  session timeout → `interrupted`, run events emitted, retention of versions
  and snapshots, stale notification after N days.
- **Contract**: `docs/specs/backup-api.json` extended with the device routes;
  fixtures shared by the Go tests and the Dart/Kotlin clients.
- **End-to-end on an emulator** (scripted, `mobile/test/e2e/phone-backup.sh`):
  seed 500 photos (JPEG, HEIC, DNG, a 2 GB HEVC video, a motion photo) with
  GPS tags via `adb push` + media scan; enrol; first backup completes;
  SHA-256 of every file equal on both sides and GPS tags present; turn on
  airplane mode mid-upload, then off → resume without re-sending finished
  chunks; kill the app process mid-run → resumes; reboot → schedules still
  run; delete 10 photos on the phone → marked deleted, still on server;
  restore to a wiped emulator → all present in the gallery with original
  dates; run through nginx with `client_max_body_size 10m` and through a
  Cloudflare-like 100 MB limit → works; revoke on the web → phone shows
  "removed" within one run.
- **Battery check**: 24 h on a physical phone with daily schedules and no new
  media: fewer than 10 wake-ups attributed to the app in battery stats.
- **Format compatibility**: SMS and call XML written by the app parse with
  an independent SMS Backup & Restore parser, and a fixture file exported by
  SMS Backup & Restore imports without loss. Round trip: export → restore to
  a wiped emulator → export again gives the same item keys. The same round
  trip for vCard (1,000 contacts) and iCalendar (recurring events).
- **Restricted settings**: on an Android 15 emulator with the APK installed
  by `adb install` as a file-manager install would do, the SMS permission is
  greyed out, the app shows the guide, and after "Allow restricted
  settings" the backup runs.
- **Health checks**: with battery optimisation on and the app force-stopped,
  the stale warning appears on the server after the threshold and the
  dashboard card shows the age.

### 5.10 Phase 2 work packages

**WP2-1 Server: device API in `services/backup`.** 9 ed.
Model, enrol/revoke/rotate, device-token auth, sessions as runs, check,
tus uploads, deleted marking, browse/download, snapshots, retention,
free-space precheck, events, tests. Contract JSON. Added by the research:
the excluded-hash list, item keys for messages and calls, the full SMS
Backup & Restore file build, XML import, per-category stale alerts (+1 ed).
The scoped device credential is the server fix for S-07 now under way;
reuse it rather than building a second one.

**WP2-2 Phone engine skeleton.** 6 ed.
Kotlin module, Room index, Pigeon channel, WorkManager setup (content
triggers, periodic, expedited/user-initiated, foreground slices), device
token storage, upload loop with tus client (OkHttp), error model.

**WP2-3 Photos and videos.** 6 ed.
Scanner (generations, volumes, trash, pending), hashing cache, originals
(`setRequireOriginal`, media capabilities), include/exclude buckets,
deleted handling, progress notification.

**WP2-4 Enrol, status and settings UI.** 6 ed. Screens of §5.7 1-3,
permissions and disclosures, help screen, the first-run checklist, the
health checks and the queue reasons of §5.5 (+1 ed).

**WP2-5 Folders (SAF), WhatsApp/Signal guides.** 3 ed.

**WP2-6 Contacts and calendar (backup + restore).** 4.5 ed. Includes the
per-contact hash manifest and duplicate skipping.

**WP2-7 Restore for media and folders.** 5 ed. Browser, "restore
everything missing", MediaStore insert, resumable.

**WP2-8 Client-side encryption for snapshot categories.** 3 ed. Argon2id,
AES-GCM chunked format with a versioned header, passphrase flow, tests
with known vectors. Opt-in and off by default (§7.3), so it is last in the
order.

**WP2-9 SMS/MMS and call log.** 7 ed. SMS Backup & Restore XML export
(incremental), SMS-app components, the restricted-settings onboarding,
role request, the three-step restore in batches with resume, dedup, and
import of existing SMS Backup & Restore files (+1 ed).

**WP2-10 Apps list and APKs.** 3 ed. List, links, APK copy, full-flavor
install sessions with splits.

**WP2-11 Web UI "Phones" section.** 4 ed. Device list, detail, revoke,
restore browser (in `ui/src/apps/backup/`).

**WP2-12 End-to-end and battery tests.** 4 ed.

**WP2-13 Free up space.** 2 ed. §5.7 2a.

Order: WP2-1 and WP2-2 in parallel (against the contract JSON), then WP2-3
and WP2-4, then WP2-7 (a backup that cannot be restored is not released),
then the rest, with WP2-8 last. First release of phone backup = WP2-1 to
WP2-4, WP2-7, WP2-11, WP2-12 (media only, about 40 ed). Full Phase 2: about
**62.5 ed**. Phase 2 needs WP1-1, WP1-4 and WP1-14 from Phase 1 (§4.6).

---

## 6. Phase 3: later and backlog

**Later features**
- Instant notifications without Google. 4 ed. Two options, both opt-in,
  with the WorkManager poll (WP1-4) as the default:
  - UnifiedPush: the server posts to the user's distributor, for example
    ntfy. No foreground service is needed on the phone.
  - A Home Assistant-style persistent WebSocket to the message bus, with a
    low-priority notification the user can silence. It needs a foreground
    service type that is not `dataSync`, because of the 6 h limit.
  Either option also lets the server ask a phone to start sharing (WP1-7).
- "Keep offline" for whole folders, with a storage limit. 3 ed. Synology
  Drive users ask for folders, not only single files.
- Global search over apps, VMs, downloads and the current folder. 1.5 ed.
  The web UI has none either.
- iOS build (Photos framework backup has different rules: background
  processing tasks, no SMS/call log access at all). Separate plan.
- Two-way folder sync phone ↔ server (bisync semantics, conflict copies).
- Immich/PhotoPrism hint: offer to add the device's media folder as an
  external library.
- A Play build and its SMS/call log and all-files-access declarations,
  only if the owner changes decision §7.1.
- F-Droid submission: the CI dependency check (WPR-3) keeps it possible;
  it also needs a reproducible build.
- Wear OS / Android Auto status tile.
- Multi-user roles once tokens carry them (companion owner field S-03
  first).
- Shizuku-based extras (for example reading `Android/data` on versions where
  shell may) only if a clear, safe use appears; not planned.

**P3 backlog from the web UI inventory** (low value on a phone; done when
time allows):

| Item | Mobile UX | Effort |
|---|---|---|
| SMB shares of a folder | read-only list in Settings > Shares | 0.5 |
| Open terminal at a path | overflow menu "Open terminal here" | 0.3 |
| App settings | view read-only; edit only env and ports; the rest links to the web | 3 |
| App sources | Settings list | 0.5 |
| Export app as compose | share the compose text | 0.3 |
| VM networks | read-only list | 0.5 |
| Snapshot notes | notes field | 0.2 |
| NivaroOS, system and SMB users | lists; SMB password reset | 1 |
| Terminal sessions | a chip row to switch between sessions | 1 |
| MergerFS pools | read-only | 0.3 |
| Audio | background playback with a media session | 1.5 |
| Code viewer | syntax highlighting | 1 |
| Feedback | send feedback from the About page | 0.3 |

Backlog total: about **10.5 ed**, plus about 8.5 ed for the later features
that have an estimate.

**Deliberately not ported**
- The Lite Browser and ad blocker. The gateway blocks the `/b/` proxy on
  purpose, and the phone has a real browser.
- Appearance and wallpaper, dock, window manager, icon editor, date/time
  pill: desktop metaphors.
- The APT package manager, fstab, Samba and system user editing, the backup
  job wizard, full app-settings forms. These stay on the web and the app
  links there.
- Doc/Excel viewers: native apps do this better ("Open with…").
- A drop-caches button (M-28).

---

## 7. Owner decisions (2026-09-24)

These answer the open questions and override anything above that says
otherwise.

1. **Distribution: sideload only.** No Google Play for now. The app ships as
   an APK from GitHub releases and local builds. Only the `full` flavor is
   built; the `play` flavor and its restrictions (§5.8) are shelved, so SMS,
   call log and all-files access are available in the one build.
2. **Destination: `/DATA/Backup/<device name>/`.** Each phone's backups go
   into its own folder under `/DATA/Backup/`. The device name is sanitised
   and made unique the same way companion folders are (a second phone with
   the same name gets `<name> (2)`); renaming the phone moves the folder.
3. **SMS and call log are not encrypted by default.** They are stored as
   plain snapshot files like the other categories. Encryption stays an
   opt-in setting, off by default.
4. **Keep "browse my phone from the server".** The companion feature stays,
   with its security fixes (S-01 done, S-02 to S-04) and the scoped device
   credential.
5. **NivaroOS updates and system package updates are separate pages**
   (2026-09-25), as in the web UI's settings: "NivaroOS update" (version,
   what's new, update NivaroOS; the phone app's own update lives here too)
   and "System packages" (Debian package updates, security first, upgrade).
   Two rows in More; Home's attention rows link to the right one.
6. **Files gets tabs (2026-09-26)**, like the web UI's Files: several
   folders open at once, copy or cut in one tab and paste in another; tabs
   survive leaving and returning to Files. To build after stage 3 (the
   Files screen is owned by the Trash work until then).

---

## 8. Effort summary

| Phase | Scope | Effort |
|---|---|---|
| 0 | fixes against today's server, safety, auth layer, flavors, contract tests | ~18 ed (incl. ~2 ed server: S-01, S-02) |
| 1, P0 | post-audit fixes, platform readiness, safe file operations, UI foundation (WP1-13 to WP1-16) | ~11.5 ed |
| 1, P1 | Backup & Sync, Download Station, notifications, VM clipboard, companion rework, security, apps and store, transfers, trash, share links, server profiles, image viewer | ~39.5 ed (incl. ~2 ed server: S-05, S-06) |
| 1, P2 | storage, VM console and Host Desktop, settings, widgets, Tailscale, pairing, dashboard cards, activity, files extras, video remux | ~27 ed (incl. ~1.5 ed server: pairing) |
| Release engineering | crash reports, self-update, CI, integration tests, device matrix, staged releases (WPR-1 to WPR-6) | ~14 ed (incl. ~1 ed server) plus ~0.5 ed per release |
| 2 | Android phone backup, first release (media) / complete | ~40 ed / ~62.5 ed (incl. ~13 ed server and web UI) |
| 3 | later features / P3 backlog | ~8.5 ed estimated items / ~10.5 ed |

Phase 1 with release engineering is about **92 ed**, up from 35 ed. The
difference comes from the web UI inventory (about 25 ed of Files, Apps,
Settings and Shell work the first plan did not cover), the Android platform
work (about 8 ed), release engineering (14 ed), and new app-only features
(server profiles, pairing, activity, dashboard cards; about 10 ed).

By release (§4.6): R1 ~37.5 ed, R2 ~45.5 ed, R3 ~40 ed (phone backup, first
release), R4 ~27 ed.

Server work for S-02 to S-07 is under way (2026-09-24). Where that work
already delivers what WP1-4 (feed and WebSocket token), WP1-7 (S-04) or
WP2-1 (device credential) need, subtract it: about 2 ed from WP1-4 and
about 1 ed from WP2-1.

---

## 9. References

Links were collected by the research on 2026-09-24. Forum posts and issue threads are user
reports, not vendor statements.

**Android platform**
- Foreground service timeouts (`dataSync` 6 h / 24 h, `onTimeout`):
  https://developer.android.com/develop/background-work/services/fgs/timeout
- Foreground service types: https://developer.android.com/develop/background-work/services/fgs/service-types
- Migrating off `dataSync` (Android 15): https://developer.android.com/about/versions/15/changes/datasync-migration
- User-initiated data transfer jobs: https://developer.android.com/develop/background-work/background-tasks/uidt
- Google Maps and UIDT reliability: https://android-developers.googleblog.com/2024/09/google-maps-improved-download-reliability-user-initiated-data-transfer-api.html
- Android 16 behaviour changes (target 36): https://developer.android.com/about/versions/16/behavior-changes-16
- Android 16 changes for all apps (job quotas): https://developer.android.com/about/versions/16/behavior-changes-all
- Flutter, edge-to-edge by default: https://docs.flutter.dev/release/breaking-changes/default-systemuimode-edge-to-edge
- Flutter, predictive back: https://docs.flutter.dev/platform-integration/android/predictive-back
- Orientation and resizability changes in Android 16: https://android-developers.googleblog.com/2025/01/orientation-and-resizability-changes-in-android-16.html
- Preparing for resizability in Android 17: https://android-developers.googleblog.com/2026/02/prepare-your-app-for-resizability-and.html
- Material 3 window size classes: https://m3.material.io/foundations/adaptive-design/foldables
- Flutter, large screens: https://docs.flutter.dev/ui/adaptive-responsive/large-screens
- Local network permission: https://developer.android.com/privacy-and-security/local-network-permission
- Flutter issue on the local network permission: https://github.com/flutter/flutter/issues/184859
- 16 KB page sizes: https://developer.android.com/guide/practices/page-sizes
- Doze and battery optimisation: https://developer.android.com/training/monitoring-device-state/doze-standby
- Network security configuration: https://developer.android.com/privacy-and-security/security-config
- `flutter_workmanager`: https://github.com/fluttercommunity/flutter_workmanager

**Sideloading and distribution**
- Android developer verification: https://support.google.com/android-developer-console/answer/16561738?hl=en
- Android Developers blog, developer verification (2026-03): https://android-developers.googleblog.com/2026/03/android-developer-verification.html
- Advanced-flow sideloading rollout: https://www.androidauthority.com/google-android-advanced-flow-sideloading-rollout-begins-3700073/
- Android 15 restricted settings for sideloaded apps: https://www.androidauthority.com/android-15-restricted-settings-sideloading-3481098/,
  https://9to5google.com/2024/09/12/android-15-sideloaded-apps-restrictions/,
  https://textbee.dev/blog/android-15-send-sms-permission-guide
- Obtainium: https://github.com/ImranR98/Obtainium
- GitHub REST API rate limits: https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api

**Flutter UX, stability and security**
- Flutter Material: https://docs.flutter.dev/ui/design/material
- `material_3_expressive` (community package, not adopted): https://pub.dev/packages/material_3_expressive
- Flutter accessibility: https://flutter.dev/docs/development/accessibility-and-localization/accessibility
- Flutter error reporting: https://docs.flutter.dev/cookbook/maintenance/error-reporting
- ACRA: https://github.com/ACRA/acra
- Dart `HttpClient` ignores Android trust settings: https://github.com/dart-lang/sdk/issues/50435,
  https://github.com/flutter/flutter/issues/140737
- `flutter_secure_storage` changelog and migration issue: https://pub.dev/packages/flutter_secure_storage/changelog,
  https://github.com/juliansteenbakker/flutter_secure_storage/issues/1235

**NAS and self-hosted apps**
- Synology Photos (Android): https://kb.synology.com/en-global/DSM/help/SynologyPhotos/Android?version=7
- Synology Photos background stalls: https://community.synology.com/enu/forum/1/post/150222,
  https://community.synology.com/enu/forum/1/post/195717
- Synology Photos skips older photos (date-based sync): https://community.synology.com/enu/forum/1/post/189436
- Synology Photos reviews: https://justuseapp.com/en/app/1484764501/synology-photos/reviews
- Synology Drive offline folders request: https://community.synology.com/enu/forum/1/post/140647
- Synology Drive mobile sharing: https://kb.synology.com/en-us/DSM/tutorial/Synology_Drive_file_sharing_mobile_client
- Synology DS cam: https://www.synology.com/en-global/surveillance/feature/mobile
- Synology DS finder: https://play.google.com/store/apps/details?id=com.synology.DSfinder&hl=en
- QNAP Qmanager: https://play.google.com/store/apps/details?id=com.qnap.qmanager&hl=en_US&gl=US
- QNAP Qfile Pro: https://play.google.com/store/apps/details?id=com.qnap.qfile&hl=en_US
- QuMagie review: https://www.letemsvetemapplem.eu/en/2023/12/03/vyzkouseli-jsme-qumagie-mobile-mejte-sve-fotky-na-qnap-nas-dokonale-pod-palcem/
- TrueNAS Pulse: https://apps.apple.com/us/app/truenas-pulse/id6759870893
- NASdroid: https://github.com/boswelja/NASdroid
- Unraid U-Manager thread: https://forums.unraid.net/topic/195455-new-android-app-u-manager-monitor-control-your-unraid-server/
- Unraid Array: https://github.com/Advice-Dog/array-public
- Unraid ControlR: https://apertoire.com/controlr
- ZimaOS remote access: https://www.zimaspace.com/docs/zimaos/remote-access
- ZimaClient: https://play.google.com/store/apps/details?id=net.icewhale.zima&hl=en_US
- CasaZimaCli: https://play.google.com/store/apps/details?id=cloud.iothub.casazima_cli&hl=en
- Umbrel for iPhone: https://umbrel.com/support/getting-started/umbrel-for-iphone
- Nextcloud Android: https://play.google.com/store/apps/details?id=com.nextcloud.client
- Nextcloud issues: auto upload not triggering https://github.com/nextcloud/android/issues/8285,
  https://github.com/nextcloud/android/issues/9320; settings reset
  https://github.com/nextcloud/android/issues/14931; unexplained pauses
  https://github.com/nextcloud/android/issues/17212; notification spam
  https://github.com/nextcloud/android/issues/12596; false maintenance mode
  https://github.com/nextcloud/android/issues/10805; rules armed once
  https://github.com/nextcloud/android/issues/17439
- Nextcloud slow listings: https://help.nextcloud.com/t/nextcloud-android-app-gets-slower-over-time/177691
- Immich mobile backup: https://docs.immich.app/features/mobile-backup/
- Immich v3.0.0: https://github.com/immich-app/immich/releases/tag/v3.0.0
- Immich PRs: setup checks https://github.com/immich-app/immich/pull/26610;
  retry on reconnect https://github.com/immich-app/immich/pull/30909;
  checksum before upload https://github.com/immich-app/immich/pull/16133
- Immich issues: background backup on Samsung and Xiaomi
  https://github.com/immich-app/immich/issues/26115,
  https://github.com/immich-app/immich/discussions/9205; deleted duplicates
  re-upload https://github.com/immich-app/immich/issues/23897
- Failing self-hosted photo backups: https://www.androidpolice.com/self-hosted-photo-backups-were-failing-until-changed-two-settings/
- Home Assistant Companion, networking: https://companion.home-assistant.io/docs/troubleshooting/networking/,
  https://community.home-assistant.io/t/ha-app-for-internal-and-external-use/522731
- Home Assistant local push: https://www.home-assistant.io/blog/2022/02/11/android-february/
- Home Assistant widgets: https://companion.home-assistant.io/docs/integrations/android-widgets/
- Home Assistant features: https://companion.home-assistant.io/docs/core/
- Home Assistant and the foreground service limit: https://github.com/home-assistant/android/issues/5338
- Findroid and Jellyfin clients: https://www.xda-developers.com/i-finally-found-a-jellyfin-app-that-doesnt-make-me-miss-plex/,
  https://www.howtogeek.com/forget-the-official-jellyfin-app-these-android-clients-are-what-you-actually-need/
- Jellyfin Quick Connect: https://jellyfin.org/docs/general/server/quick-connect/
- Plex app backlash: https://www.howtogeek.com/plex-is-fixing-its-unpopular-new-mobile-apps/,
  https://www.xda-developers.com/after-months-backlash-plex-rolling-back-controversial-big-screen-app-redesign/
- Tailscale Android redesign: https://tailscale.com/blog/android
- Tailscale and other VPNs: https://tailscale.com/docs/reference/faq/other-vpns,
  https://github.com/tailscale/tailscale/issues/12372
- Portainer mobile apps: https://apps.apple.com/us/app/portainer-mobile-docker-admin/id6761166809,
  https://play.google.com/store/apps/details?id=com.umegs.portainer_mobile&hl=en
- ntfy on phones: https://docs.ntfy.sh/subscribe/phone/
- UnifiedPush with ntfy: https://unifiedpush.org/users/distributors/ntfy/

**Android backup apps and formats**
- SMS Backup & Restore FAQ: https://www.synctech.com.au/sms-backup-restore/sms-faqs/
- SMS Backup & Restore XML reference (independent parser): https://shruggietech.github.io/sms-backup-restore-parser/xml-reference/
- SMS Backup & Restore on RCS: https://www.synctech.com.au/faqs/rcs-messages/
- RCS access for third-party apps: https://www.xda-developers.com/google-messages-rcs-api-third-party-apps/
- Seedvault storage design: https://github.com/seedvault-app/seedvault/blob/android16/storage/doc/design.md
- PhotoSync auto-transfer triggers: https://www.photosync-app.com/support/android/answers/android-autotransfer-howto
- FolderSync scheduling: https://foldersync.io/docs/help/v2/scheduling/,
  https://foldersync.io/docs/faq/scheduling/
- Syncthing-Fork: https://github.com/Catfriend1/syncthing-android,
  https://github.com/Catfriend1/syncthing-android/blob/main/wiki/Info-on-battery-optimization-and-settings-affecting-battery-usage.md
- Neo Backup FAQ: https://github.com/NeoApplications/neo-backup/blob/main/FAQ.md
- Swift Backup: https://www.swiftapps.org/
- vCard round-trip notes: https://github.com/t1nk333r/dav-provider-android/issues/6
