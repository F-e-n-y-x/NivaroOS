#!/bin/bash
# nivaroos-deploy - replace one NivaroOS binary safely.
#
#   nivaroos-deploy <new-binary> <unit>          e.g. nivaroos-deploy /tmp/gw.new nivaroos-gateway
#   nivaroos-deploy --www <built-dashboard-dir>  e.g. nivaroos-deploy --www ui/build/sysroot/var/lib/nivaroos/www
#
# The running binary is kept as <binary>.prev, the new one is put in place
# atomically and the unit restarted. If the unit isn't up and steady after
# NIVAROOS_DEPLOY_SETTLE seconds (default 10), the previous binary goes
# straight back and the command fails. The web dashboard is swapped as a
# whole directory, with the old one kept as www.prev.
#
# Use this (not a bare `mv` + restart) for every manual deploy, so
# nivaroos-rollback and the watchdog always have the previous build.
set -u

NV_LOG_TAG=nivaroos-deploy
LIB="${NIVAROOS_SAFETY_LIB:-/usr/local/lib/nivaroos/safety-lib.sh}"
[ -f "$LIB" ] || LIB="$(dirname "$(readlink -f "$0")")/nivaroos-safety-lib.sh"
# shellcheck source=nivaroos-safety-lib.sh
. "$LIB"

SETTLE="${NIVAROOS_DEPLOY_SETTLE:-10}"

usage() {
	sed -n '2,14p' "$0" | sed 's/^# \{0,1\}//'
	exit "${1:-2}"
}

if [ -z "${NIVAROOS_ALLOW_NONROOT:-}" ] && [ "$(id -u)" -ne 0 ]; then
	echo "nivaroos-deploy: run as root" >&2
	exit 1
fi

[ $# -ge 1 ] || usage
case "$1" in -h | --help) usage 0 ;; esac

nv_pause 900
trap nv_unpause EXIT

if [ "$1" = "--www" ]; then
	src="${2:-}"
	[ -f "$src/index.html" ] || { echo "nivaroos-deploy: $src/index.html not found" >&2; exit 1; }
	nv_snapshot_www || { echo "nivaroos-deploy: could not keep the current dashboard as www.prev" >&2; exit 1; }
	rm -rf "$NV_WWW_DIR.new"
	cp -a "$src" "$NV_WWW_DIR.new" || exit 1
	rm -rf "$NV_WWW_DIR.old"
	if [ -d "$NV_WWW_DIR" ]; then mv "$NV_WWW_DIR" "$NV_WWW_DIR.old" || exit 1; fi
	mv "$NV_WWW_DIR.new" "$NV_WWW_DIR" || { mv "$NV_WWW_DIR.old" "$NV_WWW_DIR"; exit 1; }
	rm -rf "$NV_WWW_DIR.old"
	nv_log "deployed web dashboard from $src (previous kept as $NV_WWW_DIR.prev)"
	echo "web dashboard deployed (undo: nivaroos-rollback www)"
	exit 0
fi

new="$1"
unit="${2:-}"
[ -n "$unit" ] || usage
case "$unit" in *.service) ;; *) unit="$unit.service" ;; esac
[ -f "$new" ] || { echo "nivaroos-deploy: $new not found" >&2; exit 1; }
[ -x "$new" ] || [ -n "${NIVAROOS_ALLOW_NONEXEC:-}" ] || { echo "nivaroos-deploy: $new is not executable" >&2; exit 1; }

bin="$(nv_unit_binary "$unit")"
if [ -z "$bin" ]; then
	echo "nivaroos-deploy: $unit does not run a $NV_BIN_DIR/nivaroos* binary" >&2
	exit 1
fi

if [ -f "$bin" ] && cmp -s "$new" "$bin"; then
	echo "$(basename "$bin") is already this build"
	exit 0
fi

nv_snapshot "$bin" || { echo "nivaroos-deploy: could not keep $bin as $bin.prev" >&2; exit 1; }
nv_install_binary "$new" "$bin" || { echo "nivaroos-deploy: install failed" >&2; exit 1; }
nv_log "deployed $bin for $unit (previous kept as $bin.prev)"

# steady UNIT: running after SETTLE seconds and not restarted meanwhile.
steady() {
	local u="$1" nr0 nr1
	nr0="$(nv_prop "$u" NRestarts)"
	sleep "$SETTLE"
	nr1="$(nv_prop "$u" NRestarts)"
	nv_unit_healthy "$u" && [ "${nr0:-0}" = "${nr1:-0}" ]
}

rc=0
for u in $(nv_units_for_binary "$bin"); do
	[ "$u" = "$unit" ] && continue
	nv_restart "$u" || true
done
if nv_restart "$unit" && steady "$unit"; then
	echo "$unit restarted with the new $(basename "$bin") (undo: nivaroos-rollback $(basename "$bin"))"
else
	rc=1
	echo "$unit did not come up with the new build - restoring the previous one" >&2
	if nv_restore_prev "$bin"; then
		nv_log "deploy of $bin failed ($unit not steady); previous build restored"
		for u in $(nv_units_for_binary "$bin"); do nv_restart "$u" || true; done
		nv_restart "$unit" || true
		if nv_unit_healthy "$unit"; then
			echo "previous build restored; $unit is running" >&2
		else
			echo "previous build restored but $unit is still not running: journalctl -u $unit -n 50" >&2
		fi
	else
		nv_log "deploy of $bin failed and there was no previous build to restore"
		echo "no previous build to restore: journalctl -u $unit -n 50" >&2
	fi
fi
exit $rc
