# Server updates

How a NivaroOS server updates itself, how to release an update, and what to
do when one goes wrong. Design: the self-updater design of 2026-10 (owner
decisions of 2026-10-16: minisign key on the release server, automatic
installs at night, separate `server-vX.Y.Z` releases, non-Debian installs
keep updating from source).

## What a box does

- Every night at about 04:00 (`nivaroos-update-check.timer`, plus once 20 min
  after boot) `nivaroos-update.service` asks GitHub for the newest
  `server-vX.Y.Z` release on the box's channel (stable; beta boxes also take
  pre-releases).
- If one is newer than `/usr/share/nivaroos/release.json` it is announced in
  the notification feed. With automatic installs on (the default) and the
  time within 30 min before / 90 min after the window (04:00), it is
  installed - unless a backup run, a Files transfer, a VM console or a Host
  Desktop session is active. Then it waits for the next night
  (`state.json` `skipped` says why).
- Install = download (resumable) -> verify -> the release's own `apply.sh`:
  only files that differ are replaced and only their services restart
  (Backup & Sync, VM consoles and Host Desktop keep running when their parts
  didn't change). The previous build is kept (`<binary>.prev`, `www.prev`,
  `/var/lib/nivaroos/update/snap/` with the old units, scripts, release.json
  and the changed services' databases).
- Health check (each restarted service steady, the gateway answering `/ping`),
  then 10 minutes of probation. Any failure puts the whole previous release
  back, databases included, marks the version bad (not offered again) and
  says so in a notification. A clean install says "NivaroOS X is installed".
- Up to an hour later, the watchdog rolls the whole release back (not one
  binary) if a service it installed keeps failing.
- A reboot in the middle: `nivaroos-update-resume.service` rolls back an
  interrupted install, re-runs an interrupted probation, or just records an
  install that had finished.

Boxes built from source (no release.json) are told about releases but never
install them automatically; `sudo nivaroos update apply` moves one onto
releases. Non-Debian/Ubuntu or non-glibc boxes keep updating with the
installer.

### Verification

`SHA256SUMS` is signed with minisign (legacy Ed25519 signature) by the key in
`/root/.nivaroos-release/` on the release server; the public key is compiled
into `nivaroos-cli` (`cli/update/nivaroos-server.pub`). The signed trusted
comment must read `nivaroos-server <version> <channel>`, so an old release
or a beta can't be served as something else. The tarball is hashed and
extracted from the same open file in a root-only folder (files and folders
only, nothing outside the bundle), every file is checked against the
bundle's `MANIFEST`, and `apply.sh` checks each file again right before it
goes in.

## Settings and commands

Settings > Updates (web) or the CLI:

    sudo nivaroos update                       status
    sudo nivaroos update check                 ask GitHub now
    sudo nivaroos update download | verify     fetch / verify only
    sudo nivaroos update apply                 install the newest release now
    sudo nivaroos update apply --backups cancel_backups
    sudo nivaroos update apply nivaroos-server-1.5.0-linux-amd64.tar.gz \
         --sums SHA256SUMS --sig SHA256SUMS.minisig      offline bundle
    sudo nivaroos update rollback [--restore-data]
    sudo nivaroos update set --auto notify     tell me only (default: install)
    sudo nivaroos update set --channel beta    or stable
    sudo nivaroos update set --window 03:00    automatic install time
    sudo nivaroos update history

State: `/var/lib/nivaroos/update/` (`state.json`, `settings.json`,
`history.jsonl`, `request.json` from core, `download/`, `staged/`, `snap/`).
Log: `journalctl -u nivaroos-update` and `/var/log/nivaroos/update.log`.

## Releasing (on the release server)

1. Add a `## [server X.Y.Z] - YYYY-MM-DD` section to `CHANGELOG.md` (it becomes
   the release notes), commit, push.
2. From a clean checkout of that commit:

       installer/release-server.sh X.Y.Z --dry-run   # build, sign, self-check
       installer/release-server.sh X.Y.Z             # ... and publish

   `X.Y.Z-beta.N` publishes a GitHub pre-release (beta channel). The script
   builds the bundle for this machine's architecture, signs `SHA256SUMS`,
   verifies the result with the boxes' own verifier and compiled-in key,
   applies it to a throwaway root, tags `server-vX.Y.Z` and uploads the
   tarball, `SHA256SUMS` and `SHA256SUMS.minisig` (never as GitHub's
   "Latest", which stays the Android app's).

Bundles carry `glibc_min` (the newest glibc symbol any binary needs; 2.34
for a build on Debian 13); a box with an older glibc is told to use the
installer. Only amd64 is built today (this server); an arm64 box sees no
bundle for its arch and keeps the installer until an arm64 machine runs the
script too.

Each bundle is built from the same sources by the same toolchain with
`-trimpath -buildvcs=false`, so unchanged services are byte-identical across
releases and never restart.

### Migrations

`installer/migrations/NNNN-name.sh` run once per box during an install or
update, after the changed services stop and before the new files go in
(`NV_ROOT` prefixes paths in tests). They must be expand-only: the previous
release must still run after them, because a rollback doesn't undo them
(add a file, unit, package or setting; remove it one release later).

## The key

- Secret key: `/root/.nivaroos-release/nivaroos-server.key` (0600, folder
  0700, no passphrase so the release script runs unattended). Never commit
  or copy it anywhere but an offline backup.
- Public key: `cli/update/nivaroos-server.pub` (key id 4F70A2166AA171CA).
- Lost or leaked key: generate a new pair, put the new public key in
  `cli/update/nivaroos-server.pub`, and ship that nivaroos-cli to the boxes
  with the installer (`curl ... install.sh | sudo bash`) - boxes only trust
  the key their nivaroos-cli was built with.

## When something goes wrong

- `sudo nivaroos update` shows the state, the last error and the last result.
- `sudo nivaroos update rollback` puts the previous release back (add
  `--restore-data` to also restore the databases from before the update;
  changes made since are lost).
- "STUCK" (the rollback is unhealthy too): `sudo nivaroos-recover status`,
  `sudo nivaroos-rollback all`, or re-run the installer.
