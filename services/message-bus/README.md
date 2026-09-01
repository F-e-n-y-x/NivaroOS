# CasaOS-MessageBus

[![Go Reference](https://pkg.go.dev/badge/github.com/F-e-n-y-x/NivaroOS/services/message-bus.svg)](https://pkg.go.dev/github.com/F-e-n-y-x/NivaroOS/services/message-bus) [![Go Report Card](https://goreportcard.com/badge/github.com/F-e-n-y-x/NivaroOS/services/message-bus)](https://goreportcard.com/report/github.com/F-e-n-y-x/NivaroOS/services/message-bus) [![goreleaser](https://github.com/F-e-n-y-x/NivaroOS-MessageBus/actions/workflows/release.yml/badge.svg)](https://github.com/F-e-n-y-x/NivaroOS-MessageBus/actions/workflows/release.yml)

Message bus accepts events and actions from various sources and delivers them to subscribers.

See [openapi.yaml](./api/message_bus/openapi.yaml) for API specification.




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