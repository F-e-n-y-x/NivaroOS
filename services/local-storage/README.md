# CasaOS-LocalStorage

[![Go Reference](https://pkg.go.dev/badge/github.com/F-e-n-y-x/NivaroOS/services/local-storage.svg)](https://pkg.go.dev/github.com/F-e-n-y-x/NivaroOS/services/local-storage) [![Go Report Card](https://goreportcard.com/badge/github.com/F-e-n-y-x/NivaroOS/services/local-storage)](https://goreportcard.com/report/github.com/F-e-n-y-x/NivaroOS/services/local-storage) [![goreleaser](https://github.com/F-e-n-y-x/NivaroOS-LocalStorage/actions/workflows/release.yml/badge.svg)](https://github.com/F-e-n-y-x/NivaroOS-LocalStorage/actions/workflows/release.yml)

Disks, mounts, USB drives, storage pools, SMB/NFS shares and cloud drives
(in-process rclone) for NivaroOS.

- **Drive health (SMART)** (`service/smart_health.go`): one `smartctl --json
  -a -l devstat` reading becomes Good / Watch / Failing with the reason.
  `GET /v1/disks/health` (`fresh=1` re-reads, never wakes a sleeping
  drive), `POST /v1/disks/smart-test` (admin). Daily history in
  `/var/lib/nivaroos/smart-history.json` (90 days); an hourly check
  notifies once per change. Unknown USB bridges are retried with `-d sat`.
- **Drives that don't mount**: at start, before Docker, managed fstab
  drives whose boot mount failed are mounted again (NTFS through ntfs-3g
  when ntfs3 refuses). One still unmounted gets a `problem` on
  `GET /v1/storage/fstab` and one notification; `POST
  /v1/storage/fstab/repair` (admin, explicit only) runs ntfsfix / e2fsck
  on the unmounted device, then mounts it.
- **Cloud drives** (`/v1/cloud`): sign in with rclone's shared OAuth app or
  the owner's own client ID/secret (Drive, OneDrive, Dropbox; guide:
  `docs/guides/cloud-own-app.md`). A token must be the whole
  `{"access_token"...}` JSON; a failed Reconnect restores the previous
  credentials. Only the backends in `backend/supported` are linked; a
  remote of another type is logged and not mounted. Each mount's trash mode
  and upload queue go to `/var/run/nivaroos/cloud-trash.json` and
  `cloud-vfs.json`.
- Disk/USB status goes to core every 5 s only when it changed (otherwise
  once a minute); `lsblk` for USB runs only while a USB drive is attached.




## publish api to npm

### edit version in package.json

### run
```bash
yarn

yarn start
```

### publish

Manual publish
```bash
yarn publish
```

Auto publish
```bash 
git push origin dev**
```