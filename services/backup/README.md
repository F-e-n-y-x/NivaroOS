# nivaroos-backup

The optional Backup & Sync module (spec: `docs/specs/2026-09-24-backup-sync-app.md`,
§0 first). One binary, `/usr/bin/nivaroos-backup`, listening on
`127.0.0.1:28643` only; the gateway routes `/v1/backup/*` to it.

| Package | What |
|---|---|
| `jobs/` | job store, validation, triggers, queue and locks, hooks, runs, logs, notifications, migration, REST API |
| `engine/` | resolve, prechecks, guards, copy / mirror / archive / restore through rclone (library, v1.75.1, same `rclone.conf` as local-storage), mountwatch |
| `engine/enginetest/` | `FakeEngine`, an in-memory `engine.API` for jobs tests |
| `cmd/gen-uicontract/` | writes `ui/src/apps/backup/errorCodes.js` and `events.js` from the Go enums |

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
