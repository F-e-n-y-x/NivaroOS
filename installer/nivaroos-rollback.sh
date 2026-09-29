#!/bin/bash
# nivaroos-rollback - put the previous NivaroOS build back.
#
#   nivaroos-rollback                 show what can be rolled back
#   nivaroos-rollback all             every binary that has a different .prev
#   nivaroos-rollback gateway         one component (binary or unit name:
#                                     nivaroos-gateway, gateway,
#                                     nivaroos-gateway.service ...)
#   nivaroos-rollback www             the web dashboard
#   nivaroos-rollback --dry-run ...   say what would happen
#
# A rollback swaps <binary> and <binary>.prev and restarts the units that
# run it, so running the same rollback again undoes it. Installed to
# /usr/local/bin/nivaroos-rollback by installer/install.sh; works over SSH
# (e.g. via Tailscale) when the dashboard itself is down.
set -u

NV_LOG_TAG=nivaroos-rollback
LIB="${NIVAROOS_SAFETY_LIB:-/usr/local/lib/nivaroos/safety-lib.sh}"
[ -f "$LIB" ] || LIB="$(dirname "$(readlink -f "$0")")/nivaroos-safety-lib.sh"
# shellcheck source=nivaroos-safety-lib.sh
. "$LIB"

DRY=""
ARGS=()
for a in "$@"; do
	case "$a" in
		-n | --dry-run) DRY=1 ;;
		-h | --help)
			sed -n '2,15p' "$0" | sed 's/^# \{0,1\}//'
			exit 0
			;;
		*) ARGS+=("$a") ;;
	esac
done

if [ -z "${NIVAROOS_ALLOW_NONROOT:-}" ] && [ "$(id -u)" -ne 0 ]; then
	echo "nivaroos-rollback: run as root (sudo nivaroos-rollback ...)" >&2
	exit 1
fi

fmt_time() { date -d "@$1" '+%Y-%m-%d %H:%M' 2>/dev/null || echo "$1"; }

list() {
	local bin any=""
	echo "Binaries with a previous build (<binary>.prev):"
	while IFS= read -r bin; do
		[ -f "$bin.prev" ] || continue
		any=1
		if nv_has_prev "$bin"; then
			printf '  %-32s current %s   previous %s\n' "$(basename "$bin")" "$(fmt_time "$(nv_changed_at "$bin")")" "$(fmt_time "$(stat -c %Y "$bin.prev")")"
		else
			printf '  %-32s (previous is identical - nothing to roll back)\n' "$(basename "$bin")"
		fi
	done < <(nv_binaries)
	[ -n "$any" ] || echo "  (none)"
	if [ -f "$NV_WWW_DIR.prev/index.html" ]; then
		echo "Web dashboard: previous build at $NV_WWW_DIR.prev ($(fmt_time "$(stat -c %Y "$NV_WWW_DIR.prev")")) - 'nivaroos-rollback www'"
	fi
	echo
	echo "Usage: nivaroos-rollback all | www | <component> ...   (--dry-run to preview)"
}

# resolve NAME -> binary path. Accepts the binary name (nivaroos-gateway),
# the short name (gateway, core), or a unit (nivaroos-user-service[.service]).
resolve() {
	local n="$1" c bin
	case "$n" in core | main) n=nivaroos ;; esac
	for c in "$NV_BIN_DIR/$n" "$NV_BIN_DIR/nivaroos-$n"; do
		case "$c" in */*.service | */*.prev | */*.bad.*) continue ;; esac
		[ -f "$c" ] && { echo "$c"; return 0; }
	done
	for c in "${n%.service}.service" "nivaroos-${n%.service}.service"; do
		bin="$(nv_unit_binary "$c")"
		[ -n "$bin" ] && { echo "$bin"; return 0; }
	done
	return 1
}

rollback_bin() {
	local bin="$1" u units rc=0
	if ! nv_has_prev "$bin"; then
		echo "$(basename "$bin"): no different previous build to roll back to" >&2
		return 1
	fi
	units="$(nv_units_for_binary "$bin")"
	if [ -n "$DRY" ]; then
		echo "would swap $bin <-> $bin.prev and restart: ${units:-(no unit)}"
		return 0
	fi
	nv_swap_prev "$bin" || { echo "$(basename "$bin"): swap failed" >&2; return 1; }
	nv_log "manual rollback: $bin <- $bin.prev (the replaced build is now $bin.prev)"
	echo "$(basename "$bin"): previous build restored (run again to undo)"
	for u in $units; do
		if nv_restart "$u"; then
			echo "  restarted $u"
		else
			echo "  restart of $u FAILED - see: journalctl -u $u -n 50" >&2
			rc=1
		fi
	done
	return $rc
}

rollback_www() {
	if [ ! -f "$NV_WWW_DIR.prev/index.html" ]; then
		echo "web dashboard: no previous build at $NV_WWW_DIR.prev" >&2
		return 1
	fi
	if [ -n "$DRY" ]; then
		echo "would swap $NV_WWW_DIR <-> $NV_WWW_DIR.prev"
		return 0
	fi
	nv_swap_www || { echo "web dashboard: swap failed" >&2; return 1; }
	nv_log "manual rollback: web dashboard <- $NV_WWW_DIR.prev"
	echo "web dashboard: previous build restored (reload the page; run again to undo)"
}

if [ ${#ARGS[@]} -eq 0 ] || [ "${ARGS[0]}" = list ]; then
	list
	exit 0
fi

# Keep the watchdog from reacting to our restarts.
nv_pause 300
trap nv_unpause EXIT

rc=0
for a in "${ARGS[@]}"; do
	case "$a" in
		all)
			done_any=""
			while IFS= read -r bin; do
				nv_has_prev "$bin" || continue
				done_any=1
				rollback_bin "$bin" || rc=1
			done < <(nv_binaries)
			[ -n "$done_any" ] || echo "nothing to roll back"
			;;
		www | web | ui) rollback_www || rc=1 ;;
		*)
			if bin="$(resolve "$a")"; then
				rollback_bin "$bin" || rc=1
			else
				echo "unknown component '$a' - run nivaroos-rollback with no arguments for the list" >&2
				rc=1
			fi
			;;
	esac
done
exit $rc
