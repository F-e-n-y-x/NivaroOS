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
	sed -n '2,13p' "$0" | sed 's/^# \{0,1\}//'
	exit "${1:-2}"
}

if [ -z "${NIVAROOS_ALLOW_NONROOT:-}" ] && [ "$(id -u)" -ne 0 ]; then
	echo "nivaroos-recover: run as root (sudo nivaroos-recover ...)" >&2
	exit 1
fi

cmd="${1:-status}"
shift || true

case "$cmd" in
	status)
		echo "== NivaroOS services"
		for u in $(nv_units); do
			printf '  %-40s %s/%s\n' "$u" "$(nv_prop "$u" ActiveState)" "$(nv_prop "$u" SubState)"
		done
		echo "== Remote access"
		if command -v tailscale >/dev/null 2>&1; then
			printf '  tailscaled: %s   address: %s\n' "$("$NV_SYSTEMCTL" is-active tailscaled 2>/dev/null)" "$(tailscale ip -4 2>/dev/null | head -n 1)"
			tailscale status --json 2>/dev/null | python3 -c 'import json,sys
d=json.load(sys.stdin); s=d.get("Self",{})
print("  MagicDNS name:", (s.get("DNSName") or "").rstrip("."))
print("  node key expiry:", s.get("KeyExpiry") or "disabled (never expires)")' 2>/dev/null || true
		else
			echo "  tailscale is not installed"
		fi
		for u in ssh.service sshd.service ssh.socket; do
			st="$("$NV_SYSTEMCTL" is-active "$u" 2>/dev/null || true)"
			[ -n "$st" ] && [ "$st" != "inactive" ] && printf '  %-12s %s\n' "$u" "$st"
		done
		echo "== Recent watchdog / rollback actions"
		if [ -s "$NV_LOG_FILE" ]; then tail -n 10 "$NV_LOG_FILE" | sed 's/^/  /'; else echo "  (none)"; fi
		;;
	unlock)
		nv_restart "$USER_UNIT" && echo "login lockouts cleared ($USER_UNIT restarted)"
		nv_log "login lockouts cleared from the shell"
		;;
	reset-password)
		user="${1:-}"
		[ -n "$user" ] || { echo "usage: nivaroos-recover reset-password <username>" >&2; exit 2; }
		[ -x "$USER_BIN" ] || { echo "$USER_BIN not found" >&2; exit 1; }
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
			echo "password reset failed" >&2
			nv_restart "$USER_UNIT" >/dev/null 2>&1 || true
			exit 1
		fi
		printf '%s\n' "$out" | grep '^UserName:\|^Password:'
		echo "Sign in with this password, then change it in Settings."
		nv_restart "$USER_UNIT" >/dev/null 2>&1 || true
		nv_log "password of '$user' reset from the shell"
		exit $rc
		;;
	clear-revocations)
		if [ -f "$REVOKED" ]; then
			bak="$REVOKED.cleared-$(date +%Y%m%d-%H%M%S)"
			mv -f "$REVOKED" "$bak" && echo "ended-session list moved to $bak"
			nv_log "revoked-session list cleared from the shell (kept as $bak)"
		else
			echo "no ended sessions recorded"
		fi
		;;
	restart)
		for u in $(nv_units); do
			nv_restart "$u" && echo "restarted $u" || echo "restart of $u FAILED" >&2
		done
		;;
	-h | --help | help) usage 0 ;;
	*) usage ;;
esac
