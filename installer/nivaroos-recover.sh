#!/bin/bash
# nivaroos-recover - get back into NivaroOS from a shell (SSH over Tailscale
# or the LAN) when the dashboard or the phone app won't let you in.
#
#   nivaroos-recover status                 services, remote access, recent watchdog actions
#   nivaroos-recover unlock                 clear login rate-limit lockouts (restarts the user service)
#   nivaroos-recover reset-password <user>  set a new random password for <user> and print it
#   nivaroos-recover clear-revocations      forget every ended session (sessions of removed
#                                           phones / changed passwords work again until they expire)
#   nivaroos-recover restart                restart every NivaroOS service
#
# A bad update: `nivaroos-rollback` (list) / `nivaroos-rollback all`.
set -u

NV_LOG_TAG=nivaroos-recover
LIB="${NIVAROOS_SAFETY_LIB:-/usr/local/lib/nivaroos/safety-lib.sh}"
[ -f "$LIB" ] || LIB="$(dirname "$(readlink -f "$0")")/nivaroos-safety-lib.sh"
# shellcheck source=nivaroos-safety-lib.sh
. "$LIB"

USER_BIN="${NIVAROOS_USER_BIN:-$NV_BIN_DIR/nivaroos-user}"
USER_CONF="${NIVAROOS_USER_CONF:-/etc/nivaroos/user-service.conf}"
USER_UNIT=nivaroos-user-service.service
REVOKED="${NIVAROOS_REVOKED_SESSIONS:-/var/lib/nivaroos/revoked_sessions.json}"

usage() {
	ui_banner "recover" "Get back in from a shell when the dashboard won't let you"
	cat <<'EOF'

  Usage
    sudo nivaroos-recover status                  services, remote access, recent watchdog actions
    sudo nivaroos-recover unlock                  clear login lockouts (restarts the user service)
    sudo nivaroos-recover reset-password <user>   set a new random password and print it
    sudo nivaroos-recover clear-revocations       forget ended sessions (they work again until they expire)
    sudo nivaroos-recover restart                 restart every NivaroOS service

  A bad update: nivaroos-rollback (list) / nivaroos-rollback all.

  Exit   0 done, 1 failed, 2 bad usage
EOF
}

cmd="${1:-status}"
shift || true
case "$cmd" in
	-h | --help | help)
		usage
		exit 0
		;;
esac

if [ -z "${NIVAROOS_ALLOW_NONROOT:-}" ] && [ "$(id -u)" -ne 0 ]; then
	ui_die "nivaroos-recover needs root: sudo nivaroos-recover ..."
fi

case "$cmd" in
	status)
		ui_head "Services"
		for u in $(nv_units); do
			if nv_unit_healthy "$u"; then
				ui_ok "$(printf '%-38s %s%s/%s%s' "$u" "$UI_D" "$(nv_prop "$u" ActiveState)" "$(nv_prop "$u" SubState)" "$UI_R")"
			else
				ui_err "$(printf '%-38s %s/%s' "$u" "$(nv_prop "$u" ActiveState)" "$(nv_prop "$u" SubState)")" 2>&1
			fi
		done
		ui_head "Remote access"
		if command -v tailscale >/dev/null 2>&1; then
			ui_kv "tailscaled" "$("$NV_SYSTEMCTL" is-active tailscaled 2>/dev/null)"
			ui_kv "address" "$(tailscale ip -4 2>/dev/null | head -n 1)"
			tailscale status --json 2>/dev/null | python3 -c 'import json,sys
d=json.load(sys.stdin); s=d.get("Self",{})
print("    %-12s %s" % ("MagicDNS", (s.get("DNSName") or "").rstrip(".")))
print("    %-12s %s" % ("key expiry", s.get("KeyExpiry") or "disabled (never expires)"))' 2>/dev/null || true
		else
			ui_kv "tailscale" "not installed"
		fi
		for u in ssh.service sshd.service ssh.socket; do
			st="$("$NV_SYSTEMCTL" is-active "$u" 2>/dev/null || true)"
			[ -n "$st" ] && [ "$st" != "inactive" ] && ui_kv "$u" "$st"
		done
		ui_head "Recent watchdog and rollback actions"
		if [ -s "$NV_LOG_FILE" ]; then tail -n 10 "$NV_LOG_FILE" | sed 's/^/    /'; else printf '    %s(none)%s\n' "$UI_D" "$UI_R"; fi
		printf '\n'

		;;
	unlock)
		nv_restart "$USER_UNIT" || ui_die "Could not restart $USER_UNIT: journalctl -u $USER_UNIT -n 50"
		ui_ok "Login lockouts cleared  ${UI_D}($USER_UNIT restarted)${UI_R}"
		nv_log "login lockouts cleared from the shell"
		;;
	reset-password)
		user="${1:-}"
		[ -n "$user" ] || ui_die "Usage: nivaroos-recover reset-password <username>" 2
		[ -x "$USER_BIN" ] || ui_die "$USER_BIN not found"
		# Current builds of the user service reset the password and exit.
		# Older ones then went on to start a second user service; give them
		# a private runtime directory (so it can't replace the running
		# service's address file) and a time limit, and restart the real
		# service afterwards (it re-registers its routes with the gateway).
		rt="$(mktemp -d)"
		trap 'rm -rf "$rt"' EXIT
		cp -f "${NIVAROOS_RUNTIME_DIR:-/var/run/nivaroos}/management.url" "$rt/" 2>/dev/null || true
		sed "s#^[[:space:]]*RuntimePath[[:space:]]*=.*#RuntimePath=$rt#" "$USER_CONF" >"$rt/user-service.conf"
		out="$(timeout 30 "$USER_BIN" -c "$rt/user-service.conf" -ru -user "$user" 2>/dev/null)"
		rc=0
		if ! printf '%s\n' "$out" | grep -q '^Password:'; then
			printf '%s\n' "$out" | grep -v '^git commit\|^build date' >&2
			ui_err "Password reset failed"
			nv_restart "$USER_UNIT" >/dev/null 2>&1 || true
			exit 1
		fi
		printf '%s\n' "$out" | grep '^UserName:\|^Password:'
		ui_ok "Sign in with this password, then change it in Settings."
		nv_restart "$USER_UNIT" >/dev/null 2>&1 || true
		nv_log "password of '$user' reset from the shell"
		exit $rc
		;;
	clear-revocations)
		if [ -f "$REVOKED" ]; then
			bak="$REVOKED.cleared-$(date +%Y%m%d-%H%M%S)"
			mv -f "$REVOKED" "$bak" || ui_die "Could not move $REVOKED"
			ui_ok "Ended-session list moved to $bak"
			nv_log "revoked-session list cleared from the shell (kept as $bak)"
		else
			ui_info "No ended sessions recorded"
		fi
		;;
	restart)
		rc=0
		for u in $(nv_units); do
			if nv_restart "$u"; then ui_ok "restarted $u"; else ui_err "restart of $u failed - journalctl -u $u -n 50"; rc=1; fi
		done
		exit $rc
		;;
	*)
		ui_err "Unknown command '$cmd' - see --help."
		exit 2
		;;
esac
