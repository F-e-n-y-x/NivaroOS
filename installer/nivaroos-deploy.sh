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
	ui_banner "deploy" "Replace one binary safely; the previous build is kept"
	cat <<'EOF'

  Usage
    sudo nivaroos-deploy <new-binary> <unit>       e.g. /tmp/gw.new nivaroos-gateway
    sudo nivaroos-deploy --www <dashboard-dir>     e.g. ui/build/sysroot/var/lib/nivaroos/www

  The running binary is kept as <binary>.prev and the unit restarted. If it
  is not up and steady after NIVAROOS_DEPLOY_SETTLE seconds (default 10),
  the previous build goes straight back. Undo: nivaroos-rollback.

  Exit   0 deployed, 1 failed (previous build restored), 2 bad usage
EOF
}

case "${1:-}" in
	-h | --help)
		usage
		exit 0
		;;
	'')
		usage >&2
		exit 2
		;;
esac

if [ -z "${NIVAROOS_ALLOW_NONROOT:-}" ] && [ "$(id -u)" -ne 0 ]; then
	ui_die "nivaroos-deploy needs root: sudo nivaroos-deploy ..."
fi

nv_pause 900
trap nv_unpause EXIT

if [ "$1" = "--www" ]; then
	src="${2:-}"
	[ -f "$src/index.html" ] || ui_die "$src/index.html not found"
	nv_snapshot_www || ui_die "Could not keep the current dashboard as www.prev"
	rm -rf "$NV_WWW_DIR.new"
	cp -a "$src" "$NV_WWW_DIR.new" || exit 1
	rm -rf "$NV_WWW_DIR.old"
	if [ -d "$NV_WWW_DIR" ]; then mv "$NV_WWW_DIR" "$NV_WWW_DIR.old" || exit 1; fi
	mv "$NV_WWW_DIR.new" "$NV_WWW_DIR" || { mv "$NV_WWW_DIR.old" "$NV_WWW_DIR"; exit 1; }
	rm -rf "$NV_WWW_DIR.old"
	nv_log "deployed web dashboard from $src (previous kept as $NV_WWW_DIR.prev)"
	ui_ok "Web dashboard deployed  ${UI_D}(undo: nivaroos-rollback www)${UI_R}"
	exit 0
fi

new="$1"
unit="${2:-}"
[ -n "$unit" ] || {
	usage >&2
	exit 2
}
case "$unit" in *.service) ;; *) unit="$unit.service" ;; esac
[ -f "$new" ] || ui_die "$new not found"
[ -x "$new" ] || [ -n "${NIVAROOS_ALLOW_NONEXEC:-}" ] || ui_die "$new is not executable"

bin="$(nv_unit_binary "$unit")"
if [ -z "$bin" ]; then
	ui_die "$unit does not run a $NV_BIN_DIR/nivaroos* binary"
fi

if [ -f "$bin" ] && cmp -s "$new" "$bin"; then
	ui_ok "$(basename "$bin") is already this build"
	exit 0
fi

nv_snapshot "$bin" || ui_die "Could not keep $bin as $bin.prev"
nv_install_binary "$new" "$bin" || ui_die "Could not install $new as $bin"
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
	ui_ok "$unit restarted with the new $(basename "$bin")  ${UI_D}(undo: nivaroos-rollback $(basename "$bin"))${UI_R}"
else
	rc=1
	ui_err "$unit did not come up with the new build - restoring the previous one"
	if nv_restore_prev "$bin"; then
		nv_log "deploy of $bin failed ($unit not steady); previous build restored"
		for u in $(nv_units_for_binary "$bin"); do nv_restart "$u" || true; done
		nv_restart "$unit" || true
		if nv_unit_healthy "$unit"; then
			ui_ok "Previous build restored; $unit is running" >&2
		else
			ui_err "Previous build restored, but $unit is still not running: journalctl -u $unit -n 50"
		fi
	else
		nv_log "deploy of $bin failed and there was no previous build to restore"
		ui_err "No previous build to restore: journalctl -u $unit -n 50"
	fi
fi
exit $rc
