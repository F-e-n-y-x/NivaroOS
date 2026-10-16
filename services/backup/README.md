# nivaroos-backup

The optional Backup & Sync module (spec: `docs/specs/2026-09-24-backup-sync-app.md`,
§0 first). One binary, `/usr/bin/nivaroos-backup`, listening on
`127.0.0.1:28643` only; the gateway routes `/v1/backup/*` to it.

| Package | What |
|---|---|
| `jobs/` | job store, validation, triggers, queue and locks, hooks, runs, logs, notifications, migration, REST API |
| `engine/` | resolve, prechecks, guards, copy / mirror / archive / restore through rclone (library, v1.75.1, same `rclone.conf` as local-storage), versioned backups through restic (`restic.go`), mountwatch |
| `engine/enginetest/` | `FakeEngine`, an in-memory `engine.API` for jobs tests |
| `cmd/gen-uicontract/` | writes `ui/src/apps/backup/errorCodes.js` and `events.js` from the Go enums |

## Behaviour worth knowing

- **Paths** (`engine/paths.go`): sources may be any folder except
  `/proc`, `/sys`, `/dev`, `/run` and Backup's own staging/spool folders;
  destinations and restore targets only under `/DATA`, `/mnt`, `/media`,
  `/home`, `/root`, `/srv`, `/opt`. Resolving anything that isn't a job
  source (phone backup folders, destination checks) keeps the write roots.
- **Backends**: only the rclone backends NivaroOS offers are linked
  (`local-storage/backend/supported`); a remote of another type in
  `rclone.conf` is skipped with a log line naming it.
- **Google Drive**: 64 MiB upload chunks and `pacer_min_sleep` 10 ms
  (`engine/rcloneconf.go`); rclone still backs off on 403s.
- **Encryption**: `dest.encryption` folder (rclone crypt) or archive (7z
  AES-256, split volumes); spec "Encrypted backups" in §18.
- **Versioned backups** (type `backup`): restic v0.19.1, shipped by the
  installer at `/usr/lib/nivaroos/bin/restic`. The repository is the
  destination folder; restic reaches it only through the engine's REST
  server on a private unix socket, over the same rclone filesystem as every
  other job (any remote, mount guard, Drive: 16 uploads, 64 MiB packs).
  Encryption mode `restic` = the repo password (recovery key as for
  folders); none = `--insecure-no-password`. Retention keep_last/daily/
  weekly/monthly runs `forget` after each success (`prune` only when
  snapshots went); maintenance queues a `check --read-data-subset=5%`
  monthly. Stale locks of a crashed run are removed (`unlock`) first.
- **Overview speed**: `GET /jobs` and `GET /jobs/:id` answer destination
  online state and versions from a stale-while-revalidate cache
  (`jobs/swr.go`; 30 s / 60 s, keyed by revision and last run). Only the
  first ask waits.
- **Resources** (unit): `MemoryHigh=3G`, `MemoryMax=6G`, `CPUWeight=50`,
  `IOWeight=50`; the Go heap is capped by `engine.SetMemoryLimit`.

## Frozen contracts (WP-0)

Change these only additively; the tests below fail on drift.

| Contract | Go | Checked by |
|---|---|---|
| Engine interface (§11.1) | `engine/api.go` (`engine.API`, `jobs.EngineClient`) | `engine/api_test.go` against `testdata/engine/*.json` |
| Data model (§4) | `jobs/model.go` | `jobs/contract_test.go` |
| Error codes + classes (§5) | `engine/errors.go`, `jobs/errors.go` | `TestEveryEngineCodeHasAClass`, `TestUIContractInSync` |
| Message-bus events (§10.1) | `jobs/events.go` | `TestAPIEventsMatchGo`, `TestUIContractInSync` |
| Clock, Schedules locks (§17) | `jobs/clock.go`, `jobs/locks.go` | compile |
| REST API (§11) | `jobs/apitypes.go` | `TestAPIFixtures` against `docs/specs/backup-api.json` |
| i18n keys the backend sends | `jobs/i18nkeys.go` | `TestI18nKeysInEnUS` (keys and `{placeholders}` in `en_US.json`) |

Fixtures must decode strictly into their type and re-encode to exactly the
same JSON (`internal/fixturetest`), so they can't drift or omit a field.

After changing `jobs/errors.go` or `jobs/events.go`:

```sh
cd services/backup && GOWORK=off go generate ./jobs
```

## Build and test

```sh
export PATH=$PATH:/usr/local/go/bin
GOWORK=off go vet ./... && GOWORK=off go test ./...
```

End-to-end (`e2e/`, build tag `e2e`): the real binary and rclone engine
on throwaway instances - create/edit/delete, run, cancel, crash
(SIGKILL) and stop mid-run, retry backoff, excludes, many small and
large files, restore to the original place and elsewhere (sha256
checked), purge on delete, live progress and speed, schedule catch-up,
versioned backups into a folder, a remote and WebDAV (dedup, restore,
forget, check, crash mid-upload),
and page load times with thousands of runs and a hanging cloud. It
never touches the live service, `/DATA` or a cloud account.

```sh
services/backup/e2e/run.sh            # ~6 min; -short skips the catch-up wait
```
