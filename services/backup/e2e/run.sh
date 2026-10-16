#!/bin/sh
# Backup & Sync end-to-end suite: builds nivaroos-backup and drives
# throwaway instances (temp state, temp rclone.conf with local remotes,
# a free loopback port) over the REST API. It never touches the live
# service, /DATA or a cloud account. Test data goes under
# ~/.cache/nivaroos-backup-e2e-* (NIVAROOS_BACKUP_E2E_DIR to move it;
# NIVAROOS_BACKUP_E2E_KEEP=1 keeps it after a failure).
#
#   services/backup/e2e/run.sh                 # everything (~6 min)
#   services/backup/e2e/run.sh -short          # skip the 3-min catch-up wait
#   services/backup/e2e/run.sh -v -run Crash   # one test, verbose
#
# Versioned (restic) backups use NIVAROOS_RESTIC, else the installed
# /usr/lib/nivaroos/bin/restic; without either those tests skip.
set -e
cd "$(dirname "$0")/.."
export PATH="$PATH:/usr/local/go/bin" GOWORK=off GOTOOLCHAIN=local
exec go test -tags e2e -count=1 -timeout 30m "$@" ./e2e
