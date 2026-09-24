# CasaOS-MessageBus

[![Go Reference](https://pkg.go.dev/badge/github.com/F-e-n-y-x/NivaroOS/services/message-bus.svg)](https://pkg.go.dev/github.com/F-e-n-y-x/NivaroOS/services/message-bus) [![Go Report Card](https://goreportcard.com/badge/github.com/F-e-n-y-x/NivaroOS/services/message-bus)](https://goreportcard.com/report/github.com/F-e-n-y-x/NivaroOS/services/message-bus) [![goreleaser](https://github.com/F-e-n-y-x/NivaroOS-MessageBus/actions/workflows/release.yml/badge.svg)](https://github.com/F-e-n-y-x/NivaroOS-MessageBus/actions/workflows/release.yml)

Message bus accepts events and actions from various sources and delivers them to subscribers.

See [openapi.yaml](./api/message_bus/openapi.yaml) for API specification.

## Notification feed

Events are delivered live and then forgotten, so a device that wasn't
connected (a phone, a browser opened later) used to miss them. The bus also
keeps a persisted **notification feed** of the events worth telling someone
about:

- Which events count: `service/notification_classify.go` (the rule list is at
  the top). In short: any event named `*:notify` with a `title` property (the
  way a service raises a notification on purpose, e.g.
  `nivaroos:backup:notify`), app install / uninstall / update results and app
  start / stop / restart / settings failures, and storage format / creation
  results. Live state streams (utilization, progress, file operations,
  hot-plug) are not stored.
- Storage: `<DBPath>/message-bus-notifications.db` (`[app] DBPath`, default
  `/var/lib/nivaroos/db`, mode 0600). Not the runtime path: that is a tmpfs.
  The schema is migrated automatically on start (`PRAGMA user_version`).
- Retention: 30 days, at most 1000 entries.
- Entries are shared by every user; read and dismissed state is per user
  (the access token's user id).
- API (normal JWT): `GET /v2/message_bus/notifications?after=&before=&limit=&unread=`,
  `POST /v2/message_bus/notifications/read` and `/dismiss` with
  `{"ids":[..]}` or `{"all":true,"up_to":<id>}`.
- Live: `message-bus:notification:created` and
  `message-bus:notification:state` (source `message-bus`) on socket.io and
  `/event/message-bus`.

To raise a notification from a new service, register and publish an event
whose name ends in `:notify` with at least `title` (English), and ideally
`message`, `level` (`info|success|warning|error`), `key` + `args` (i18n) and
`action` (JSON, e.g. `{"target":"settings","props":{"section":"storage"}}`).




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