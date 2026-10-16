#!/usr/bin/env bash
# shellcheck disable=SC2016,SC2034 # check() evals its single-quoted tests (which use these)
# install.sh --restore against a temp root and a fake nivaroos-backup: the
# option's checks, the step order (restore last), the key, the dry run
# before the restore and no restore when the dry run refuses. The restore
# itself is tested in services/backup (jobs, e2e).
# Run: bash installer/tests/restore-test.sh
set -u
d="$(cd "$(dirname "$0")/.." && pwd)"
PASS=0 FAIL=0
check() { if eval "$1"; then PASS=$((PASS + 1)); else FAIL=$((FAIL + 1)); echo "FAIL: $2" >&2; fi; }

T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT
touch "$T/settings.7z.001"

# parse_args runs in a subshell: it may exit.
args() { (
	# shellcheck disable=SC1090
	. <(sed '$d' "$d/install.sh")
	set +eE
	trap - ERR INT TERM EXIT
	parse_args "$@" 2>/dev/null || exit $?
	echo "$RESTORE_FILE|$WITH_BACKUP"
); }

check '[ "$(cd "$T" && args --restore settings.7z.001)" = "$T/settings.7z.001|yes" ]' "--restore makes the path absolute and turns Backup on"
check '[ "$(args --restore="$T/settings.7z.001")" = "$T/settings.7z.001|yes" ]' "--restore=FILE"
args --restore "$T/missing.7z.001" >/dev/null
check '[ $? = 2 ]' "a missing file is a bad option"
args --without-backup --restore "$T/settings.7z.001" >/dev/null
check '[ $? = 2 ]' "--restore with --without-backup is a bad option"

# shellcheck disable=SC1090
. <(sed '$d' "$d/install.sh")
set +eE
trap - ERR INT TERM EXIT
run_step() { eval "$2"; }

RESTORE_FILE="$T/settings.7z.001" WITH_BACKUP=yes
check '[ "$(plan_steps | tail -n 1)" = restore_settings ]' "the restore is the last step"
RESTORE_FILE="" check '! plan_steps | grep -q restore_settings' "no restore step without --restore"
RESTORE_FILE="$T/settings.7z.001"

YES=yes
unset NIVAROOS_RESTORE_KEY
(ask_restore_key) 2>/dev/null
check '[ $? = 2 ]' "unattended without NIVAROOS_RESTORE_KEY stops before installing"

# The fake binary logs its arguments and the key it got; REFUSE makes the
# dry run fail.
export NV_ROOT="$T/root" CALLS="$T/calls"
mkdir -p "$NV_ROOT/usr/bin"
cat >"$NV_ROOT/usr/bin/nivaroos-backup" <<'EOF'
#!/bin/bash
echo "$* key=$NIVAROOS_RESTORE_KEY" >>"$CALLS"
case " $* " in *" -yes "*) exit 0 ;; esac
[ -z "${REFUSE:-}" ] || { echo "refused: newer"; exit 3; }
echo "  replace  /etc/nivaroos/gateway.ini"
EOF
chmod +x "$NV_ROOT/usr/bin/nivaroos-backup"

NIVAROOS_RESTORE_KEY="ABCDEF-GHIJKL"
ask_restore_key
out="$(restore_settings 2>&1)"
check '[ "$(cat "$CALLS")" = "config-restore -root $NV_ROOT -file $T/settings.7z.001 key=ABCDEF-GHIJKL
config-restore -root $NV_ROOT -file $T/settings.7z.001 -yes key=ABCDEF-GHIJKL" ]' "dry run, then the restore, with the key from the environment"
check '[[ "$out" == *"replace  /etc/nivaroos/gateway.ini"* ]]' "the plan is shown"
check '[[ "$out" != *ABCDEF* ]]' "the key is never printed"

rm -f "$CALLS"
(REFUSE=1 restore_settings) >/dev/null 2>&1
check '[ $? != 0 ] && [ "$(wc -l <"$CALLS")" = 1 ] && ! grep -q -- -yes "$CALLS"' "a refused dry run restores nothing"

echo "restore-test: $PASS passed, $FAIL failed"
[ "$FAIL" = 0 ]
