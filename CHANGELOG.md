# Changelog

Notable user-facing changes to NivaroOS. (`services/core/CHANGELOG.md` is the
inherited CasaOS history and is not kept up to date.)

## Unreleased

### Added

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

### Security

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

- New NivaroOS logo, "Ni": an N whose last stroke doubles as an i, with a
  mint status dot, on a blue (#2563EB) tile. It replaces the old CasaOS
  three-circle cloud mark everywhere: web UI favicon, PWA/home-screen icons,
  sign-in and first-run screens, Settings > About, the app launch screen,
  the Download Station browser's new tab, the Android app's launcher icon
  (adaptive and Android 13 themed icon) and in-app mark, and the README.
  Source files, colours and usage rules: [`docs/brand/`](docs/brand/README.md).
