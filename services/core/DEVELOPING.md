# NivaroOS core: development

Core lives in the NivaroOS monorepo (github.com/F-e-n-y-x/NivaroOS); the web
UI is in `ui/` and has its own pnpm setup.

## Build and test
Needs the Go version in `go.mod`.

```bash
cd services/core
GOWORK=off go build -o /tmp/nivaroos .
GOWORK=off go test ./...
```

## Files end-to-end tests
`installer/tests/files-e2e.sh` (as root) builds this service and runs the
Files suite in `e2e/` against a throwaway instance - see
docs/specs/2026-09-23-files-reliability.md §5.
