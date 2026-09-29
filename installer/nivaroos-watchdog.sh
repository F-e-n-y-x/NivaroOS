#!/bin/bash
# nivaroos-watchdog - run every 2 minutes by nivaroos-watchdog.timer.
#
# Keeps a NivaroOS box reachable while nobody is in front of it:
#
#   * a NivaroOS unit that failed (including one systemd gave up on after
#     too many restarts) is reset and restarted;
#   * a unit that keeps failing - failed or crash-looping on two runs in a
#     row - right after its binary was replaced (within
#     NIVAROOS_ROLLBACK_WINDOW, 1 h by default) gets the previous binary
#     (<binary>.prev) back, is restarted, and the rollback is logged;
#   * tailscaled and sshd - the way in from outside - are started again if
#     they stopped;
#   * the gateway is probed over HTTP; if it stops answering on two runs in
#     a row it is restarted.
#
# Units that were stopped on purpose (inactive, not failed) are left alone,
# and nothing is touched while an install/deploy holds the pause file.
# Log: /var/log/nivaroos/watchdog.log and the journal (tag nivaroos-watchdog).
set -u

NV_LOG_TAG=nivaroos-watchdog
LIB="${NIVAROOS_SAFETY_LIB:-/usr/local/lib/nivaroos/safety-lib.sh}"
[ -f "$LIB" ] || LIB="$(dirname "$(readlink -f "$0")")/nivaroos-safety-lib.sh"
# shellcheck source=nivaroos-safety-lib.sh
. "$LIB"

ROLLBACK_WINDOW="${NIVAROOS_ROLLBACK_WINDOW:-3600}"
FAILS_BEFORE_ROLLBACK="${NIVAROOS_FAILS_BEFORE_ROLLBACK:-2}"
FLAP_RESTARTS="${NIVAROOS_FLAP_RESTARTS:-3}"
GATEWAY_INI="${NIVAROOS_GATEWAY_INI:-/etc/nivaroos/gateway.ini}"
CURL="${NIVAROOS_CURL:-curl}"
REMOTE_UNITS="${NIVAROOS_REMOTE_UNITS:-tailscaled.service ssh.service sshd.service ssh.socket}"

mkdir -p "$NV_STATE_DIR" 2>/dev/null || true

# One run at a time.
exec 9>"$NV_STATE_DIR/.lock" 2>/dev/null || true
if command -v flock >/dev/null 2>&1 && ! flock -n 9; then
	exit 0
fi

if nv_paused; then
	exit 0
fi

state_get() { cat "$NV_STATE_DIR/$1" 2>/dev/null || true; }
state_set() { printf '%s\n' "$2" >"$NV_STATE_DIR/$1" 2>/dev/null || true; }
state_del() { rm -f "$NV_STATE_DIR/$1" 2>/dev/null || true; }

int_or_zero() { case "$1" in '' | *[!0-9]*) echo 0 ;; *) echo "$1" ;; esac; }

# unit_failing UNIT - failed, restarting right now, or restarted at least
# FLAP_RESTARTS times since the last run (a crash loop that happens to be
# "running" at the moment we look).
unit_failing() {
	local unit="$1" active sub nr last
	active="$(nv_prop "$unit" ActiveState)"
	sub="$(nv_prop "$unit" SubState)"
	nr="$(int_or_zero "$(nv_prop "$unit" NRestarts)")"
	last="$(state_get "$unit.nrestarts")"
	state_set "$unit.nrestarts" "$nr"
	WHY=""
	if [ "$active" = "failed" ]; then
		WHY="failed"
	elif [ "$sub" = "auto-restart" ]; then
		WHY="restarting"
	elif [ -n "$last" ] && [ "$nr" -ge $(($(int_or_zero "$last") + FLAP_RESTARTS)) ]; then
		WHY="crash-looping ($((nr - last)) restarts)"
	fi
	[ -n "$WHY" ]
}

check_unit() {
	local unit="$1" active type bin fails changed
	type="$(nv_prop "$unit" Type)"
	[ "$type" = "oneshot" ] && return 0
	active="$(nv_prop "$unit" ActiveState)"

	if ! unit_failing "$unit"; then
		if [ -n "$(state_get "$unit.fails")" ] && [ "$active" = "active" ]; then
			nv_log "$unit is healthy again"
			state_del "$unit.fails"
		fi
		return 0
	fi

	fails=$(($(int_or_zero "$(state_get "$unit.fails")") + 1))
	state_set "$unit.fails" "$fails"
	bin="$(nv_unit_binary "$unit")"

	if [ -n "$bin" ] && [ "$fails" -ge "$FAILS_BEFORE_ROLLBACK" ] && nv_has_prev "$bin"; then
		changed="$(nv_changed_at "$bin")"
		if [ $(($(nv_now) - changed)) -le "$ROLLBACK_WINDOW" ]; then
			if nv_restore_prev "$bin"; then
				nv_log "ROLLBACK: $unit $WHY on $fails checks in a row after $bin changed; restored $bin.prev (bad build kept as $bin.bad.*)"
				state_del "$unit.fails"
				local u
				for u in $(nv_units_for_binary "$bin"); do
					nv_restart "$u" || nv_log "restart of $u after rollback failed"
				done
				return 0
			fi
			nv_log "ROLLBACK of $bin for $unit failed"
		fi
	fi

	if [ "$active" = "failed" ]; then
		if nv_restart "$unit"; then
			nv_log "restarted $unit ($WHY, check $fails)"
		else
			nv_log "restart of $unit failed ($WHY, check $fails)"
		fi
	else
		# systemd is already restarting it; just keep count.
		nv_log "$unit is $WHY (check $fails)"
	fi
}

# The ways in from outside: without them, nothing else can be fixed.
check_remote_access() {
	local unit enabled active
	for unit in $REMOTE_UNITS; do
		enabled="$("$NV_SYSTEMCTL" is-enabled "$unit" 2>/dev/null || true)"
		[ "$enabled" = "enabled" ] || continue
		active="$(nv_prop "$unit" ActiveState)"
		case "$active" in
			active | activating | reloading) continue ;;
		esac
		if nv_restart "$unit"; then
			nv_log "started $unit (was $active)"
		else
			nv_log "start of $unit failed (was $active)"
		fi
	done
}

gateway_port() {
	local p
	p="$(sed -n 's/^[[:space:]]*port[[:space:]]*=[[:space:]]*\([0-9][0-9]*\).*/\1/p' "$GATEWAY_INI" 2>/dev/null | head -n 1)"
	echo "${p:-80}"
}

# Any HTTP answer at all counts: "000" means no connection/timeout.
check_gateway_http() {
	local unit=nivaroos-gateway.service code fails
	[ "$(nv_prop "$unit" ActiveState)" = "active" ] || { state_del gateway.http; return 0; }
	command -v "$CURL" >/dev/null 2>&1 || return 0
	code="$("$CURL" -s -o /dev/null -m 8 -w '%{http_code}' "http://127.0.0.1:$(gateway_port)/" 2>/dev/null || true)"
	if [ -n "$code" ] && [ "$code" != "000" ]; then
		state_del gateway.http
		return 0
	fi
	fails=$(($(int_or_zero "$(state_get gateway.http)") + 1))
	state_set gateway.http "$fails"
	if [ "$fails" -ge 2 ]; then
		if nv_restart "$unit"; then
			nv_log "restarted $unit: not answering HTTP on port $(gateway_port) ($fails checks)"
		else
			nv_log "restart of $unit failed: not answering HTTP ($fails checks)"
		fi
		state_del gateway.http
	else
		nv_log "$unit not answering HTTP on port $(gateway_port) (check $fails)"
	fi
}

check_remote_access
for u in $(nv_units); do
	check_unit "$u"
done
check_gateway_http
exit 0
