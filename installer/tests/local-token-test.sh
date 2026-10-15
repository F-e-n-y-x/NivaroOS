#!/usr/bin/env bash
# shellcheck disable=SC2016 # check() evals its single-quoted test
# install.sh's local-automation token (security audit 2026-10, H1): made
# once, 0600, never replaced, and sent to the services on stdin, not argv.
# Run: bash installer/tests/local-token-test.sh
set -u
d="$(cd "$(dirname "$0")/.." && pwd)"
PASS=0 FAIL=0
check() { if eval "$1"; then PASS=$((PASS + 1)); else FAIL=$((FAIL + 1)); echo "FAIL: $2" >&2; fi; }

# shellcheck disable=SC1090
. <(sed '$d' "$d/install.sh")
set +eE
trap - ERR INT TERM EXIT

T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT
LOCAL_TOKEN="$T/run/local-automation.token"

ensure_local_token
tok="$(cat "$LOCAL_TOKEN")"
check '[[ "$tok" =~ ^[0-9a-f]{64}$ ]]' "token is 64 hex chars ($tok)"
check '[ "$(stat -c %a "$LOCAL_TOKEN")" = 600 ]' "token file is 0600"
ensure_local_token
check '[ "$(cat "$LOCAL_TOKEN")" = "$tok" ]' "an existing token is kept"
check '[ -z "$(ls "$T/run" | grep -v "^local-automation.token$")" ]' "no temp file left behind"

# A fake curl that records its argv and the headers it got on stdin.
curl() { printf '%s\n' "$*" >"$T/argv"; cat >"$T/stdin"; }
local_api_curl -fsS "http://127.0.0.1:28643/v1/backup/runs"
check 'grep -qx "X-Nivaroos-Local-Token: $tok" "$T/stdin"' "token header sent on stdin"
check 'grep -q -- "-H @-" "$T/argv" && ! grep -q "$tok" "$T/argv"' "token never on the command line"

echo "local-token-test: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]
