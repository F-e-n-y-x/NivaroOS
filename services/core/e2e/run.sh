#!/bin/sh
# Files end-to-end suite: real core binary, throwaway port, temp data.
# Usage: services/core/e2e/run.sh [go test flags, e.g. -run TestUpload]
# Env: NV_E2E_LARGE_MB (default 3072), NV_E2E_DIR (work dir, on disk),
#      NV_E2E_KEEP=1 (keep the work dir), NV_E2E_CORE_BIN (prebuilt core).
set -e
cd "$(dirname "$0")"
export GOWORK=off GOTOOLCHAIN=${GOTOOLCHAIN:-local}
[ "$(id -u)" = 0 ] || exec sudo -E "$0" "$@"
exec go test -tags e2e -count=1 -timeout 60m -v "$@" .
