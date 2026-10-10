# Changelog

Notable user-facing changes to NivaroOS. (`services/core/CHANGELOG.md` is the
inherited CasaOS history and is not kept up to date.) Format:
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/). Versions are the
GitHub release tags, which carry the Android app's version; the server is
updated from `master` by re-running the installer.

## [Unreleased]

Server, web and app changes on `master` since v1.4.11. The server gets them
by re-running the installer; the app changes ship in the next APK.

### Added

- Host Desktop on Wayland. GNOME (mutter's remote-desktop API, no prompt),
  wlroots compositors (sway, Hyprland, labwc, river, wayfire; through
  wayvnc) and other desktops such as KDE Plasma 6 (xdg-desktop-portal; the
  person at the server clicks "Allow" once) now stream like X11 desktops,
  over the same socket, so the web panel and the app work unchanged. GNOME
  won't share a locked screen: the panel and app offer "Unlock the
  server's screen" (re-locked a minute after the last viewer leaves).
  Wayland login screens can't be shared. X11 desktops keep x11vnc. The
  Wayland packages install where a Wayland desktop exists, and on older
  installs once the active session is Wayland.
- Cloud sign-in with your own app (Google Drive, OneDrive, Dropbox): Add
  and Reconnect can use your own OAuth client ID and secret instead of
  rclone's shared one, which the providers rate-limit. They are stored on
  this server only; the account list shows "Your own app". Step-by-step
  guide: [`docs/guides/cloud-own-app.md`](docs/guides/cloud-own-app.md).
- App: Notifications inbox (Home's bell with an unread badge, and More >
  Notifications) for the server's notification feed, grouped by day, with
  read/unread, mark all read and pull to refresh; a tap opens the
  download, torrent, backup run, drive health page or app. New entries are
  posted on the phone in Downloads / Backups / System channels without
  Google's push service: live while the app runs, otherwise from the
  15-minute background check. The permission is asked from the inbox.
- Network widget (web and app): each physical NIC's link speed ("enp7s0 ·
  1 Gbit/s", or Wi-Fi) and today's received/sent totals, kept across
  restarts and reboots (`/var/lib/nivaroos/net-today.json`).
- App: the Backup & Sync job form has every option the web wizard has
  (skip patterns, stop apps/VMs, run conditions, safety limits, advanced),
  new encrypted jobs, and Preview, Check backup, Duplicate, Delete, Test
  password and Reconnect on the job page.
- Finished and failed downloads and torrents go to the notification feed,
  so the web Notification Center and the phone get them with no Download
  Station window open. App install/update entries open the app.
- [Privacy Policy](PRIVACY.md), [Terms of Service](TERMS.md) and the root
  `LICENSE` file.

### Changed

- Web UI loads windows, viewers and languages when they are first opened:
  the sign-in screen downloads 1.07 MB of JS instead of 10.4 MB (316 KB vs
  2.9 MB gzipped). Hashed JS/CSS/fonts/images are cached by the browser
  for good, and the gateway serves pre-built Brotli/gzip files.
- Backup & Sync opens at once: the overview (`GET /jobs`) no longer probes
  every destination on each call (3 s to about 2 ms); the app shows jobs
  as they arrive, keeps the last answer per server, and after 20 s without
  an answer says "Can't reach the server" with Retry.
- Backups to Google Drive pace requests faster (10 ms minimum instead of
  rclone's 100 ms; it still backs off when Google says so).
- Lighter on the server:
  - Fast, successful requests between NivaroOS services are no longer
    logged (they were 92% of journal lines); errors, slow requests and
    everything from browsers and the app still are.
  - The systemd journal is capped at 1 GB
    (`/etc/systemd/journald.conf.d/50-nivaroos.conf`).
  - Service binaries are built stripped (`-trimpath -s -w`, about 30%
    smaller). local-storage and backup link only the rclone backends
    NivaroOS offers (drive, dropbox, onedrive, iclouddrive, s3, b2,
    webdav, sftp, smb, terabox, plus local and crypt): about 40% smaller
    and 20 MB less memory each.
  - Backup has a fixed memory cap (`MemoryHigh=3G`, `MemoryMax=6G`, was
    40%/60% of RAM). Backup, the torrent engine and Download Station run
    at half CPU/IO weight; Download Station gets `MemoryHigh=1G`.
  - Container CPU/RAM widgets: one stats round per request, shared by
    every open widget, instead of a polling loop that kept running 100 s.
  - The watchdog reads all units in one `systemctl show` (0.55 s to 0.13 s
    CPU per run); local-storage pushes disk/USB status only when it
    changes; Download Station builds its ad blocker on first use (37 MB
    less memory on boxes that never use its browser).
- The installer removes leftovers of older versions and CasaOS
  (`/var/lib/casaos/www`, CasaOS binaries and units once CasaOS is
  disabled, `/usr/bin/*.bak*`, week-old watchdog `.bad.*` builds) and
  installs the dashboard as a whole folder. Disabled CasaOS leftovers no
  longer make the installer refuse to update.

### Removed

- The message bus's socket.io endpoint (`/v2/message_bus/socket.io`) and
  `nivaroos-cli message-bus subscribe socketio`; clients use the
  WebSockets at `/v2/message_bus/event/{source}`.
- A cloud remote in `rclone.conf` of a type NivaroOS doesn't offer is no
  longer mounted or backed up (both services log it by name and type).

### Fixed

- Blank dashboard after the Vite move (jshint's Node `events` import).
- Host Desktop / VM console stayed on "Connecting..." after the stream
  was up.
- Cloud sign-in: a token cut off while copying is refused instead of
  saved; a failed Reconnect puts the old sign-in back; Connect no longer
  stays disabled after a single paste.
- Download Station: a finished browser download could miss its
  "completed" event; a finished torrent could be announced twice.
- App: moving an encrypted backup job to another destination was refused.
- Profile pictures uploaded straight from a phone photo are turned upright
  (EXIF orientation).
- Backup: two engine races (purge marker check, rclone stats) and a stale
  cached remote after re-pointing an encrypted job's remote.

### Security

- The message bus validates registered type names (1-128 of
  `[A-Za-z0-9._:-]`) and caps publish/trigger bodies at 1 MiB (JSON object
  of strings); bad ones get a 400.
- Go 1.27.2 and golang.org/x/net v0.60.0 (net/http HTTP/2 and CONNECT
  fixes, GO-2026-6603/6610-6613/6617). govulncheck: 0 in every module.

## [1.4.11] - 2026-10-08

Server, web and app (app 1.4.11+21).

### Added

- Drive health (SMART), in plain words, for every drive including the
  system drive (SATA HDD/SSD, NVMe, USB enclosures - an unknown USB bridge
  is retried as SAT). Each drive gets a verdict - Good, Watch or Failing -
  with the reason ("12 sectors waiting to be remapped - back up this
  drive") and the numbers that actually predict a failure: reallocated,
  pending and unreadable sectors, uncorrectable read errors, cable errors,
  SSD wear and spare blocks, NVMe media errors, temperature, power-on
  hours, power cycles and unsafe shutdowns. Old counts that aren't
  growing (read errors years ago) are a note, not a warning. A daily
  snapshot keeps 90 days of history ("pending sectors went from 0 to 12
  this week", with a chart), and the server checks hourly and sends one
  notification when a drive turns Watch/Failing or a damage counter
  grows - never repeated for the same state. Sleeping drives are still
  never woken. Settings > Storage > a drive's info shows the Health panel
  (verdict, key numbers with what they mean, history chart, short/long
  self-test with progress and past results, raw SMART table); the app has
  a Drives group on Server health that opens the same page. Starting a
  self-test is now admin only. Without smartmontools the drive says so
  ("SMART tools aren't installed") instead of looking healthy.
- Drives that don't mount after a power cut. A NivaroOS drive set to
  mount at boot that isn't mounted (left dirty or damaged by a power cut,
  or not connected) is shown in Settings > Storage and in the app's
  Server health with the cause in plain words, and sent as a
  notification. NTFS drives the kernel driver refuses as dirty are
  mounted through ntfs-3g automatically; anything else gets a Repair
  drive button (admin, never automatic: ntfsfix for NTFS, e2fsck for
  ext2-4), which mounts the drive afterwards. Apps whose folders are on
  a drive that isn't mounted no longer start on empty folders on the
  system disk: they wait ("Immich waits for drive tower") and start again
  by themselves once the drive is mounted.
- Trash for network shares, cloud drives and phones (web and app).
  Deleting on an SMB/NFS/SSHFS share or a cloud drive that can move files
  on its servers (WebDAV, Dropbox, local-style remotes...) moves it to a
  Trash folder on that share or drive - instant, nothing re-uploaded.
  Google Drive, OneDrive and TeraBox keep their own trash, so a delete
  there says "Move to Google Drive's trash" instead of trashing twice.
  Deleting a file on a paired phone moves it to a hidden Trash folder on
  the phone (no confirmation on the phone, and it leaves the gallery).
  The Trash lists all of them with where each item lives; restore,
  delete forever, Empty Trash and the 30-day clean-up work for every
  kind, and a phone that's offline shows its items as unavailable until
  it's back. Read-only shares and drives without server-side moves still
  warn "Delete permanently" and say why.
### Security

- Download Station's torrent engine is now the official static
  qBittorrent-nox build (5.2.4 today, the latest release looked up on
  every install/update, SHA-256 checked) instead of the distro package
  (Debian 13 ships 5.1.0, before 5.2.1's SSRF-via-redirect fix). The
  profile (settings and the torrent list) is backed up to
  /var/lib/nivaroos/torrent.bak before the version changes, and torrents
  carry on where they were. `--ds-torrent-distro` keeps the distro
  package. On qBittorrent 5.2+ Download Station talks to it with an API
  key instead of a login session (older versions keep the login), and a
  restart of Download Station while torrents run no longer locks it out
  of qBittorrent (it logged in with an empty password and got banned).
- Apps: app management talks to Docker through the current official
  modules (moby/moby client v0.6 and API v1.56, Docker Compose v5.6 SDK
  and compose-go v2) instead of the frozen docker/docker v24 and Compose
  v2.23 ones. govulncheck: 28 reachable vulnerabilities to 0 (the CLI: 9
  to 0). Installing, updating and removing apps, custom compose apps,
  app status (failed vs stopped), auto-updates, logs, stats and holding
  apps whose drive is missing work as before. New installs no longer
  relax Docker's minimum API version (`DOCKER_MIN_API_VERSION=1.24`).
  Compose files NivaroOS writes now spell device mappings in compose's
  long form and add the default `mode: ingress` to ports (same meaning).
- Server: the Go services' HTTP framework moved from Echo v4 (security
  fixes end 2026-12-31) to Echo v5.4. Routes, status codes, error bodies,
  sign-in/token checks, CORS and the web UI files behave as before.
  local-storage's request log now leaves out query strings like the
  other services' (tokens can ride in them).
- Web UI dependency advisories cut from 134 to 6 (`pnpm audit --prod`;
  the critical and 42 of 44 highs gone). The app tips editor no longer
  uses `@kangc/v-md-editor`, which bundled mermaid 8 and DOMPurify 2.3:
  tips are edited in a plain text box and previewed as Markdown,
  sanitized by DOMPurify 3 like the update notes. axios is on 1.x;
  lodash, qs, minimatch, brace-expansion, fast-uri and source-map-js are
  on patched versions, and unused nanoid, markdown-it and yamljs are
  removed. What's left needs Vue 3 (vue 2, Tiptap 2, vue-dompurify-html
  2's braces), a socket.io-client 4 move (parseuri) or replacing
  music-metadata-browser (music-metadata, file-type).
- The old CasaOS rclone daemon (`rclone.service`) is gone. It ran a
  separate, outdated rclone (1.61, Jan 2023) with its remote-control API
  open to anything on the box (no auth, any web origin), and nothing in
  NivaroOS needed it any more: cloud drives are mounted by local-storage's
  built-in rclone 1.75. On upgrade, local-storage stops and disables it
  once it serves no mounts and keeps the unit as `rclone.service.prev`.
  Also removed: core's unused legacy cloud OAuth callback (`/v1/recover`).
  The `rclone` command itself stays installed (Terminal sign-in,
  Scheduled Tasks).
- Download Station's browser keeps itself up to date. Chrome now ships
  every two weeks, and this browser opens any website on the server, so
  instead of a fixed Chrome for Testing build it installs the current
  Stable (from Google's Chrome for Testing list, checked against the
  storage's checksum and its own version) and the latest uBlock Origin
  Lite (checked against GitHub's SHA-256). A weekly timer
  (`nivaroos-ds-browser-update.timer`) repeats this; it does nothing when
  nothing changed and never closes an open browser - the next start uses
  the new version. The previous version is kept: `install-ds-browser.sh
  --rollback` goes back to it (and the timer then waits for a newer
  release). Offline, the pinned versions are the fallback, never a
  downgrade. On x86_64 the browser now always uses this Chrome for Testing
  rather than a Chrome or Chromium package the box already has.
- Files > Extract can no longer write outside the folder it extracts into.
  A crafted zip or tar could use `../` names, absolute paths, or a symlink
  followed by a file "inside" it to write anywhere on the server
  (CVE-2025-3445 in the old, unmaintained archiver library). Extraction now
  uses `mholt/archives` and writes only through a folder-confined handle:
  such an archive is refused (and the half-made folder removed), and
  symlinks, hardlinks and device entries inside archives are skipped. 7z
  archives can now be extracted too. Downloading folders as zip/tar and
  Compress use the same new library.
- Go dependency security pass across all services: golang.org/x/crypto,
  net, text, image and sys to current; golang-jwt v4.5.2 (login-header
  DoS, CVE-2025-30204); gorilla/websocket v1.5.3; kin-openapi v0.149
  (request-validation DoS fixes); Echo v4.16 (its old JWT middleware, the
  last user of the never-fixed jwt v3, is replaced by echo-jwt); xz, runc
  and spdystream patch releases. The installer now installs Go 1.27.1.
  govulncheck: every service is clean except App Management's Docker /
  Compose / containerd advisories, which need the Docker SDK move.

### Changed

- Web UI moved to Vue 3.5 and Vite 8 (from Vue 2.7, end of life since
  2023, and Vue CLI/webpack). Vuex 4, Vue Router 4, vue-i18n 11,
  Buefy-next (still on Bulma 0.9), Tiptap 3, xterm.js 6 (`@xterm/xterm`),
  vuedraggable 4, v-viewer 3, vue-dompurify-html 5. The sign-in and
  first-run forms use small built-in checks instead of vee-validate; the
  first-run animation uses lottie-web directly; unused dialogs and form
  components were removed. Everything looks and works as before; `pnpm
  run build` still writes to `ui/build/sysroot/var/lib/nivaroos/www`.
  `pnpm audit --prod` is clean (was 6 advisories). Building the UI from
  source needs Node 20.19+ or 22.12+ (the installer now checks).
- Web UI: live updates (widgets, notifications, app installs, file
  operations, backup progress) arrive over the message bus's plain
  WebSockets instead of socket.io 2 (unmaintained; the client had an open
  ReDoS advisory). The audio player in Files is the browser's own player
  with the track's cover, title and artist (read by `music-metadata`, which
  replaces the deprecated `music-metadata-browser`). 20 unused UI
  dependencies removed.
- Removed dead CasaOS code: core's `/v1/driver` list and its Google
  Drive / Dropbox / OneDrive OAuth drivers (cloud accounts are
  local-storage's), the gateway's stale `/v1/recover` and `/v1/driver`
  routes (dropped from routes.json at start), and helper.sh's SysV
  `/etc/init.d/smbd` reload (systemd 260 has no SysV scripts; nothing
  called it). The uninstaller also removes `rclone.service.prev`.
- The GPU widget's stats come from NVML inside the GPU sidecar instead of
  starting `nvidia-smi` twice per refresh (about 29 ms of CPU per refresh
  down to about 4 ms on a GTX 1080 Ti). Same JSON; `nvidia-smi` is still
  used when NVML can't be loaded.
- CI: GitHub Actions moved to their Node 24 versions (checkout v7,
  setup-java v6, upload-artifact v7, docker/* latest).

## [1.4.10] - 2026-10-08

App only.

### Added

- App: three more styles in Settings > Appearance - Soft (rounded white
  cards on grey), Terminal (monospace, square, ruled panels) and Bold
  (heavy type, big numbers). Six styles in all, each in light, dark and
  true black with every accent.

### Changed

- App: fresh installs start in Rack on true black (an existing choice is
  kept).

## [1.4.9] - 2026-10-07

Everything since v1.2.1.

### Added

- Encrypted backups for storage you don't trust: an encrypted folder
  (rclone crypt, names hidden) or a 7z AES-256 archive split under 4 GB,
  with a recovery key, Test password and Check backup.
- Phone backup (photos, folders, contacts, calendar, SMS, call log, apps)
  from the app to a folder on the server, and a Phones section in Backup &
  Sync.
- App: Backup & Sync screens, Server health page, Fans page, profile
  pictures, and Home widgets refreshing every second or in Real time.
- App: Host Desktop and VM console reworked after Microsoft's Windows App:
  full screen, auto-hiding toolbar, pinch zoom, Touch or Mouse pointer
  mode, Fit / Fill / 1:1 / Match this phone, quality, extra keys,
  auto-reconnect.
- Download Station's browser runs a real Chromium on the server, with
  TeraBox sign-in through it.
- Persistent terminal sessions with Ctrl+Shift+C / V, find and a context
  menu. Online storage cache on disk with settings and Clear cache.
- App: "Upload to NivaroOS" in Android's Share sheet. Share photos,
  videos, documents or any files from Gallery, Files, WhatsApp, Chrome and
  the like, pick the server and folder (Downloads, Gallery, Documents, a
  per-phone folder, the last one used, or any folder), choose keep both /
  replace / skip for names already there, and the upload runs in the
  background with one progress notification and Cancel. It resumes after
  the network drops, ends with "Uploaded 3 files · Open" (opens the
  folder in Files), and shows why any file failed, with Retry.
- Torrents in Download Station (web and app): add magnet links, .torrent
  files or links to them (with folder, category and "start paused"), or
  drop .torrent files in a watch folder. Each torrent shows progress,
  speeds, peers/seeds, ratio and time left, its files (pick which to
  download and their priority) and trackers, with pause / resume / recheck
  / remove (with or without files) and sequential download. Settings
  modelled on qBittorrent - speed limits with an alternative schedule,
  queueing, seeding limits, port and UPnP, DHT/PeX/LSD, encryption,
  connection limits, incomplete and category folders - plus an
  auto-updated public trackers list that is never added to private
  torrents. Runs on qbittorrent-nox (installed with Download Station,
  started only while a torrent is active), your own qBittorrent, or a
  built-in engine. The app opens magnet links and .torrent files, and
  "Download on server" accepts shared magnet links.
- Fan control (Settings > Fans & Cooling, and a Fans page in the app): live
  fan speeds and CPU/GPU temperatures, per-fan Auto / Fixed / Curve modes
  with a drag-to-edit curve, Quiet / Balanced / Performance / Auto
  profiles, renaming, and an on-demand Identify step. Works on any PC with
  what it finds: motherboard super-I/O chips (the driver is found and
  loaded by the installer), AMD GPUs, laptop fan drivers and the Raspberry
  Pi fan through hwmon, NVIDIA GPUs through NVML. Safety first: never below
  20% (or the GPU's own minimum), 100% on every driven fan at the emergency
  temperature (CPU 85 °C / GPU 83 °C by default), a stalled fan or a lost
  sensor goes back to Auto, and every fan goes back to BIOS/driver control
  whenever the service stops, crashes or the machine shuts down. Fans that
  can't be driven (ACPI resource conflict, no driver, read-only driver) are
  shown read-only with the reason.
- Download Station in the Android app (More > Download Station): every
  download with live progress, speed and time left, filters and search,
  pause / resume / retry / remove (with or without the file), open a
  finished file or show it in Files, add links with a save folder and name,
  and the download settings. Share a link from Chrome or any app to
  NivaroOS ("Download on server") to queue it on the server. Home lists
  failed downloads under Needs attention.
- Share links from the app (Files > a file or folder > Share link…):
  one-time links, 1 hour / 1 day / 7 days / never, an optional password,
  then copy, share or show a QR code; a link made over the home network or
  Tailscale says it only works there. Files > Shared links lists them with
  Revoke. On the server, Quick Share links can now be one-time, password
  protected (asked on a small page before the download), last 7 days, and
  share folders as a ZIP.

### Changed

- New NivaroOS logo, "Ni": an N whose last stroke doubles as an i, with a
  mint status dot, on a blue (#2563EB) tile. It replaces the old CasaOS
  three-circle cloud mark everywhere: web UI favicon, PWA/home-screen icons,
  sign-in and first-run screens, Settings > About, the app launch screen,
  the Download Station browser's new tab, the Android app's launcher icon
  (adaptive and Android 13 themed icon) and in-app mark, and the README.
  Source files, colours and usage rules: [`docs/brand/`](docs/brand/README.md).

[Unreleased]: https://github.com/F-e-n-y-x/NivaroOS/compare/v1.4.11...HEAD
[1.4.11]: https://github.com/F-e-n-y-x/NivaroOS/compare/v1.4.10...v1.4.11
[1.4.10]: https://github.com/F-e-n-y-x/NivaroOS/compare/v1.4.9...v1.4.10
[1.4.9]: https://github.com/F-e-n-y-x/NivaroOS/compare/v1.2.1...v1.4.9
