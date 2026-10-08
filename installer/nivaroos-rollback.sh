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
			ui_banner "rollback" "Put the previous build back"
			cat <<'EOF'

  Usage
    sudo nivaroos-rollback                  list what can be rolled back
    sudo nivaroos-rollback all              every binary with a different previous build
    sudo nivaroos-rollback <component>      one: gateway, core, nivaroos-user-service ...
    sudo nivaroos-rollback www              the web dashboard
    sudo nivaroos-rollback --dry-run ...    say what would happen

  A rollback swaps <binary> and <binary>.prev and restarts the units that
  run it, so running the same rollback again undoes it.

  Exit   0 done, 1 something could not be rolled back, 2 bad usage
EOF
			exit 0
			;;
		-*)
			ui_err "Unknown option '$a' - see --help."
			exit 2
			;;
		*) ARGS+=("$a") ;;
	esac
done

if [ -z "${NIVAROOS_ALLOW_NONROOT:-}" ] && [ "$(id -u)" -ne 0 ]; then
	ui_die "nivaroos-rollback needs root: sudo nivaroos-rollback ..."
fi

fmt_time() { date -d "@$1" '+%Y-%m-%d %H:%M' 2>/dev/null || echo "$1"; }

list() {
	local bin any=""
	ui_head "Previous builds"
	while IFS= read -r bin; do
		[ -f "$bin.prev" ] || continue
		any=1
		if nv_has_prev "$bin"; then
			printf '    %-30s %scurrent%s %s   %sprevious%s %s\n' "$(basename "$bin")" "$UI_D" "$UI_R" "$(fmt_time "$(nv_changed_at "$bin")")" "$UI_D" "$UI_R" "$(fmt_time "$(stat -c %Y "$bin.prev")")"
		else
			printf '    %-30s %s(previous is identical - nothing to roll back)%s\n' "$(basename "$bin")" "$UI_D" "$UI_R"
		fi
	done < <(nv_binaries)
	[ -n "$any" ] || printf '    %s(none)%s\n' "$UI_D" "$UI_R"
	if [ -f "$NV_WWW_DIR.prev/index.html" ]; then
		printf '    %-30s %sprevious%s %s\n' "www (dashboard)" "$UI_D" "$UI_R" "$(fmt_time "$(stat -c %Y "$NV_WWW_DIR.prev")")"
	fi
	printf '\n    %snivaroos-rollback all | www | <component>   (--dry-run to preview)%s\n\n' "$UI_D" "$UI_R"
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
		ui_err "$(basename "$bin"): no different previous build to roll back to"
		return 1
	fi
	units="$(nv_units_for_binary "$bin")"
	if [ -n "$DRY" ]; then
		ui_info "would swap $bin <-> $bin.prev and restart: ${units:-(no unit)}"
		return 0
	fi
	nv_swap_prev "$bin" || { ui_err "$(basename "$bin"): swap failed"; return 1; }
	nv_log "manual rollback: $bin <- $bin.prev (the replaced build is now $bin.prev)"
	ui_ok "$(basename "$bin"): previous build restored  ${UI_D}(run again to undo)${UI_R}"
	for u in $units; do
		if nv_restart "$u"; then
			ui_kv "restarted" "$u"
		else
			ui_err "restart of $u failed - journalctl -u $u -n 50"
			rc=1
		fi
	done
	return $rc
}

rollback_www() {
	if [ ! -f "$NV_WWW_DIR.prev/index.html" ]; then
		ui_err "Web dashboard: no previous build at $NV_WWW_DIR.prev"
		return 1
	fi
	if [ -n "$DRY" ]; then
		ui_info "would swap $NV_WWW_DIR <-> $NV_WWW_DIR.prev"
		return 0
	fi
	nv_swap_www || { ui_err "Web dashboard: swap failed"; return 1; }
	nv_log "manual rollback: web dashboard <- $NV_WWW_DIR.prev"
	ui_ok "Web dashboard: previous build restored  ${UI_D}(reload the page; run again to undo)${UI_R}"
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
			[ -n "$done_any" ] || ui_info "Nothing to roll back"
			;;
		www | web | ui) rollback_www || rc=1 ;;
		*)
			if bin="$(resolve "$a")"; then
				rollback_bin "$bin" || rc=1
			else
				ui_err "Unknown component '$a' - run nivaroos-rollback with no arguments for the list"
				rc=1
			fi
			;;
	esac
done
exit $rc
