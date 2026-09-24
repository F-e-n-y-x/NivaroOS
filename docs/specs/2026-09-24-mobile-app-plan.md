# NivaroOS mobile app plan (Android)

Status: plan, nothing built yet (2026-09-24).
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
  policy in force in 2026. Play policy text changes; the release checklist
  (§4 WP1-10) re-checks each item against the live policy pages before
  submission.

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
happens to the app. The others block Phase 1 or 2.

**S-01 (S2, fix now) Path traversal in companion upload.**
`services/core/route/v1/companion.go:1388-1395` (`PostCompanionDeviceUpload`):
`?path=` is `filepath.Clean`ed and only a leading `/` is removed, so
`path=../../../etc/cron.d` joins outside the device folder; core runs as
root. Any logged-in user can write a file anywhere. Do: resolve under the
device folder with a within-root check (reuse whatever the Files rebuild uses
for this), reject `..`, add a handler test with `../` and symlinked
directories. The download check at `:1353-1360` uses a bare prefix test
(`/DATA/Companion2/…` passes); fix it the same way.

**S-02 (S1) Companion dedup deletes other phones.**
`companion.go:170-221` (`deduplicateCompanionDevicesLocked`) and
`:880-898` (register) treat two devices with the same IP as one and delete
the older. The IP is the phone's own **private LAN address** (`input.IP`,
`:861-864`), so two phones on different home networks that both have
`192.168.1.23`, or one phone that gets another's old DHCP lease, delete each
other along with their folder mapping. Model+name matching has the same
effect for two identical phones with default names. Do: identify devices by
id only. Migration: none needed (the file keeps working).

**S-03 (S3) Companion devices are global.** Every logged-in user sees,
renames, browses and deletes every user's phones (`GetCompanionDevices`,
`:797-837`, no owner). Today every account is effectively an admin
(`services/backup/jobs/auth.go:75-81` notes tokens carry no role), so this is
low risk now; add `owner_user_id` before roles arrive.

**S-04 (S3) Companion files off-LAN: listing works, download does not.**
The reverse tunnel only implements `list` (`FetchCompanionFilesFromDevice`,
`:355-430`); downloads and uploads need a direct LAN connection
(`ProxyCompanionStream`, `:433-440`). Through a tunnel the web UI shows the
phone's files and then fails on open. Phase 1 WP1-7 either streams over the
tunnel or returns a clear "phone is not on the same network" error.

**S-05 (S2) Message-bus WebSockets need no token.**
`services/message-bus/route/routers.go:50-52` skips JWT for every
`GET` + `Upgrade: websocket`, which covers `/v2/message_bus/event/{source}`
and socket.io's websocket transport. Anyone who can reach the dashboard can
subscribe to every event (file operations, backup progress with file names).
Do: require `?token=` (or the header) on WebSocket upgrades, after checking
that the web UI's socket.io client sends one (verify `ui/src/main.js:64-66`).
The app needs this fixed before it subscribes to events (WP1-4).

**S-06 (S3) No notification feed on the server.**
`services/core/service/notify.go:52-66` (`SendNotify`) only publishes to the
message bus; the backup service does the same (`services/backup/jobs/notify.go`).
The web UI keeps its own NotificationCenter. A phone that was not connected
at that moment never learns a backup failed. Phase 1 WP1-4 adds a persisted
feed.

**S-07 (S3) No long-lived device credential.** Access tokens live 3 h and
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

## 4. Phase 1: parity and new features

Priority order. P1 = next release after Phase 0, P2 = the one after.

**WP1-1 Backup & Sync management (P1).** 5 ed.
- Shown only when `GET /v1/backup/health` answers (optional module, spec
  `2026-09-24-backup-sync-app.md` §0). Screens: overview (last run per job,
  failures first), job list, job detail (runs, next run from
  `/cron/preview`), **Run now** (`POST /jobs/:id/run`), cancel, the
  "waiting for you" decision (`POST /runs/:id/decide`, with the preview list
  from `/runs/:id/preview`), run log (`/runs/:id/log?after=`), versions and
  browse, restore to server (`POST /jobs/:id/restore`) and download
  (`POST /downloads` → `GET /downloads/:token`). Job creation stays on the
  web UI in v1 of the app (the wizard is large); editing on the phone is
  limited to enable/disable (`/toggle`).
- Contract: `docs/specs/backup-api.json`. The service accepts a raw token or
  `Bearer` (`services/backup/jobs/auth.go:55-58`); writes need admin
  (`:61-64`), show its 403 text.
- Progress: message-bus events `nivaroos:backup:run-*` (WP1-4), falling back
  to `GET /runs/:id` every 3 s while the screen is open, as the web UI does.
- Acceptance: with the module absent nothing about Backup shows; with it
  present a run started on the phone shows live progress and ends with the
  same status the web UI shows.

**WP1-2 Download Station (P1).** 3 ed.
- Health `GET /v1/download-station/health`; list, add (URL, magnet, torrent
  file from the share sheet), pause/resume/delete, pause-all, change-feed
  polling with `GET /events?after=` (`services/download-sidecar/handlers.go:231-235`),
  folder picker from `/storage/roots`. The built-in browser proxy (`/b/…`) is
  refused by the gateway on purpose and is not offered.
- Android share target: "Share → NivaroOS → Download on server" for links.

**WP1-3 Storage (P2).** 3 ed.
- Disks, SMART summary, USB eject (`DELETE /v1/disks/usb`), mounts.
  Format and pool creation are long jobs now: start with `PUT`/`POST /v1/storage`,
  then poll `GET /v1/storage/jobs/:id` (`services/local-storage/route/v1.go:53-60`).
  Destructive actions need typed confirmation (the disk's name), as on the
  web.

**WP1-4 Notifications (P1).** 5 ed, of which 2 server.
- Server (S-05, S-06): (a) token required on message-bus WebSockets;
  (b) a persisted feed in core: `GET /v1/notifications?after=<id>&limit=`
  returning `{id, time, source, level, title, message, action}` and
  `POST /v1/notifications/read`, fed by core's `SendNotify` and by a
  message-bus subscription to `nivaroos:backup:notify` and the Download
  Station completion event; kept 30 days.
- App while open: one WebSocket to `/v2/message_bus/event/{source_id}?names=…`
  (plain JSON; no socket.io client needed) with the token.
- App in background: WorkManager periodic poll of the feed (15 min minimum,
  network constraint), local notifications per channel (Backups, Downloads,
  System, Security). Deep links open the matching screen.
- Why not FCM first: it needs a Firebase project, a Google service account on
  every self-hosted server, and sends metadata through Google; the F-Droid
  flavor cannot include it. Later option: UnifiedPush (the server posts to
  the user's distributor, for example ntfy), which works in both flavors
  (Phase 3).

**WP1-5 VM clipboard (P1).** 2 ed.
- The web console has a clipboard panel and the sidecar adds a
  `qemu-vdagent` channel with `copypaste` (`services/vm-sidecar/clipboard.go:25-31`);
  a VM reports `clipboard_channel` (`domain.go:127`) and needs guest tools.
- RFB: send ClientCutText (message 6) for paste; stop discarding
  ServerCutText (message 3, today read and dropped at `rfb_client.dart:255-258`)
  and put it on the phone clipboard. Negotiate the Extended Clipboard
  pseudo-encoding for UTF-8 text when the server offers it (QEMU implements
  it together with the vdagent clipboard; verify on the target QEMU version);
  without it RFB cut text is Latin-1, so non-Latin text uses "Type it"
  (today's `_openTextInputDialog`, `widgets/rfb_view.dart:491-521`).
- UI: clipboard sheet with "Paste into VM", "Type it", last 10 items (kept in
  memory only), and the hint "turns on the next time this VM starts" when
  `clipboard_channel` is false, same text as the web.

**WP1-6 VM console quality (P2).** 3 ed. Tight or ZRLE decoding (zlib
streams through `dart:io` `RawZLibFilter`), `ExtendedDesktopSize`, the rest
of the VM form (USB/PCI passthrough, shared folders, extra disks, pause and
resume, ISO upload through the gateway route with the v2-style chunking the
sidecar supports (verify), VM Manager setup status and install).

**WP1-7 Companion over tunnels (P2).** 3 ed, mostly server. Stream
download/upload over the existing reverse WebSocket (chunked binary frames
with request ids, backpressure) or return a clear error (S-04). S-03 owner
field.

**WP1-8 Security (P1).** 4 ed.
- Biometric or device-credential app lock (`local_auth`; `BIOMETRIC_STRONG`
  or device credential), lock after N minutes in background, required before
  terminal, power actions and VM delete.
- Self-signed https: on first connect show the certificate's SHA-256
  fingerprint and ask; store the pin per profile; every connection (HTTP and
  WebSocket, through one `HttpClient` with `badCertificateCallback` that
  accepts only the pinned key) checks it; a changed certificate stops with a
  clear screen. Real CA certificates need no pin.
- Tokens: already in the Keystore via `flutter_secure_storage`; add
  `FLAG_SECURE` on the terminal and login screens; never log tokens (M-20).

**WP1-9 App store and apps (P2).** 2 ed. Install progress through the
app-management events, updates available (`/v2/app_management/apps/upgradable`),
uninstall with the data question the web asks.

**WP1-10 Settings, release checklist (P2).** 2 ed. Users (read-only),
network, shares, system update status; Play policy re-check per permission;
data-safety form; privacy text for the companion and backup features.

**WP1-11 Widgets and shortcuts (P2).** 2 ed. Home-screen widget (CPU, memory,
storage, last backup status) refreshed by the same WorkManager poll; quick
settings tile "Back up now" (after Phase 2).

**WP1-12 Tailscale (P2).** 1 ed. Status, peers, exit node and subnet prefs
(`/v1/tailscale/prefs`), login URL flow (M-10). Suggest the Tailscale app
when the server is only on a tailnet.

Phase 1 total: about **35 ed** (including about 5 ed server work).

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
  messages in its own storage. Only SMS and MMS are backed up.
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
  (the app links to per-vendor guidance). Big first backups should run with
  the app open or as a user-started transfer (§5.5).
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
  `messages/2026-09-24T03-00Z.jsonl.gz` with MMS parts under
  `messages/parts/<sha256>`, `calllog/….jsonl.gz`, `calendar/<calendar>/….ics`,
  `apps/….json`, APKs under `apps/apk/<package>/<versionCode>/`). A snapshot
  identical to the previous one (same SHA-256) is not stored again.
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
- At rest, optional per category (default **on** for SMS/MMS and call log,
  off for the rest): client-side encryption on the phone with a key derived
  from a recovery passphrase (Argon2id; the app generates a 12-word
  passphrase, shows it once, requires "I saved it"). Snapshot files are
  encrypted with AES-256-GCM in 1 MiB chunks; the server never sees the key.
  Losing the passphrase loses those snapshots; the app says so. Encrypted
  categories are not browsable on the server.

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
`/DATA/Backups/Devices/<device name>/` on the data volume.

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

Rules: every path is relative, cleaned and joined under the device folder
with a within-root check (tests with `..`, absolute paths, symlinks, NUL,
very long names, names that differ only in case on case-insensitive
destinations); a finished upload whose SHA-256 differs is deleted and
reported; free-space precheck before accepting an upload (destination
free space minus a 5 % reserve); per-device rate limit on `check`; uploads
older than 7 days are purged; the device token never works on any other
route. Events: `nivaroos:backup:run-*` as today, plus
`nivaroos:backup:device-changed`.

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
- After boot or app update WorkManager restores its schedules; no
  `RECEIVE_BOOT_COMPLETED` receiver of our own is needed for backup.

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
- SMS/MMS (full flavor): JSON lines per message (`address`, `date`,
  `date_sent`, `type`, `read`, `body`, `thread_id`, `sub_id`), MMS with
  addresses and parts (`content://mms/part`), part bytes content-addressed.
- Call log (full flavor): JSON lines from `CallLog.Calls`.
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
| SMS/MMS (full) | Explain, request `ROLE_SMS` (`RoleManager.createRequestRoleIntent`), write messages in date order (dedup on address + date + body hash), then open the default-apps screen to switch back. If the role is refused, nothing is written. |
| Call log (full) | Insert with `WRITE_CALL_LOG`, dedup on number + date + duration. |
| Apps | Checklist of apps not installed on this phone with "Open in Play Store" (`market://details?id=…`). Full flavor: "Install from backup" for APKs, one `PackageInstaller` session per app with base and all splits; the user confirms each install; skipped when the phone's ABI or SDK does not match. |
| WhatsApp, Signal files | Guided: install the app, don't open it; the restore copies the folder back into the granted SAF tree (for WhatsApp `Android/media/com.whatsapp/WhatsApp/`); then open the app and choose its own restore. |

Restore uses the device token of the phone doing the restore; restoring
another device's backup needs a one-time user login (admin) on the phone,
not the other device's token.

### 5.7 UI flow

1. **Enrol** (Backup tab → "Back up this phone"): server check (module
   installed, admin) → name → destination (default shown with free space) →
   categories with plain descriptions, each with its permission prompt at the
   moment it is switched on (never all at once) → conditions → encryption
   passphrase for the categories that use it → "Start first backup". The
   first backup runs as a user-initiated transfer with a progress screen and
   an estimate.
2. **Status** (Backup tab home): per category last success, pending items
   ("312 photos waiting for Wi-Fi"), current activity, errors with a Fix
   action (grant permission, free space on server, sign in again), "Back up
   now", "Pause".
3. **Settings per category**: schedule, conditions, include/exclude folders
   (for media: which buckets, e.g. exclude `Screenshots`), deleted-on-phone
   rule, encryption (fixed after first run of that category unless reset).
4. **Restore**: device picker (this phone, other phones of this account),
   category, then the flows in §5.6.
5. **Help**: the §5.2 limits in plain words, vendor background-restriction
   guidance, what the server can see.

Play prominent-disclosure screens appear before each sensitive permission
(contacts, calendar, SMS, call log, media location) and say what is sent
where.

### 5.8 Flavors and Play policy

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

### 5.10 Phase 2 work packages

**WP2-1 Server: device API in `services/backup`.** 8 ed.
Model, enrol/revoke/rotate, device-token auth, sessions as runs, check,
tus uploads, deleted marking, browse/download, snapshots, retention,
free-space precheck, events, tests. Contract JSON.

**WP2-2 Phone engine skeleton.** 6 ed.
Kotlin module, Room index, Pigeon channel, WorkManager setup (content
triggers, periodic, expedited/user-initiated, foreground slices), device
token storage, upload loop with tus client (OkHttp), error model.

**WP2-3 Photos and videos.** 6 ed.
Scanner (generations, volumes, trash, pending), hashing cache, originals
(`setRequireOriginal`, media capabilities), include/exclude buckets,
deleted handling, progress notification.

**WP2-4 Enrol, status and settings UI.** 5 ed. Screens of §5.7 1-3,
permissions and disclosures, help screen.

**WP2-5 Folders (SAF), WhatsApp/Signal guides.** 3 ed.

**WP2-6 Contacts and calendar (backup + restore).** 4 ed.

**WP2-7 Restore for media and folders.** 5 ed. Browser, "restore
everything missing", MediaStore insert, resumable.

**WP2-8 Client-side encryption for snapshot categories.** 3 ed. Argon2id,
AES-GCM chunked format with a versioned header, passphrase flow, tests
with known vectors.

**WP2-9 SMS/MMS and call log, full flavor.** 6 ed. Export, SMS-app
components, role request, restore, dedup.

**WP2-10 Apps list and APKs.** 3 ed. List, links, APK copy, full-flavor
install sessions with splits.

**WP2-11 Web UI "Phones" section.** 4 ed. Device list, detail, revoke,
restore browser (in `ui/src/apps/backup/`).

**WP2-12 End-to-end and battery tests.** 4 ed.

Order: WP2-1 and WP2-2 in parallel (against the contract JSON), then WP2-3
and WP2-4, then WP2-7 (a backup that cannot be restored is not released),
then the rest. First release of phone backup = WP2-1 to WP2-4, WP2-7,
WP2-11, WP2-12 (media only, about 38 ed). Full Phase 2: about **57 ed**.

---

## 6. Phase 3: later

- UnifiedPush notifications (server posts to the user's distributor; works
  without Google in both flavors). 3 ed.
- iOS build (Photos framework backup has different rules: background
  processing tasks, no SMS/call log access at all). Separate plan.
- Two-way folder sync phone ↔ server (bisync semantics, conflict copies).
- Immich/PhotoPrism hint: offer to add the device's media folder as an
  external library.
- Play submission of SMS/call log and all-files-access declarations, if the
  owner wants the Play flavor to have them.
- Wear OS / Android Auto status tile.
- Multi-user roles once tokens carry them (companion owner field S-03 first).
- Shizuku-based extras (for example reading `Android/data` on versions where
  shell may) only if a clear, safe use appears; not planned.

---

## 7. Open questions for the owner

1. Distribution: keep sideloading only (`full`), or also publish on Play
   (`play`, with the restrictions in §5.8)? This decides WP0-6 and WP1-10.
2. Default destination for phone backups: `/DATA/Backups/Devices/` on the
   data volume, or ask every time?
3. Should SMS/call log snapshots be encrypted by default (plan: yes), given
   that a lost passphrase makes them unrecoverable?
4. Keep the companion "browse my phone from the server" feature at all
   after Phase 2, or drop it (it is the largest attack surface in the app)?

---

## 8. Effort summary

| Phase | Scope | Effort |
|---|---|---|
| 0 | fixes against today's server, safety, auth layer, flavors, contract tests | ~18 ed (incl. ~2 ed server: S-01, S-02) |
| 1 | Backup & Sync, Download Station, notifications, clipboard, security, storage, more | ~35 ed (incl. ~5 ed server: S-05, S-06, S-04) |
| 2 | Android phone backup, first release (media) / complete | ~38 ed / ~57 ed (incl. ~12 ed server and web UI) |
| 3 | later items | not estimated |
