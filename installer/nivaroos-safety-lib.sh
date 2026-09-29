#!/bin/bash
# Shared helpers for NivaroOS's safety net: nivaroos-watchdog,
# nivaroos-rollback and nivaroos-deploy. Installed to
# /usr/local/lib/nivaroos/safety-lib.sh and sourced by those scripts.
#
# Model: every NivaroOS binary lives at /usr/bin/nivaroos*. Before a binary
# is replaced (installer, nivaroos-deploy) the running one is kept as
# <binary>.prev. Rolling back = putting .prev back in place. A binary that
# was rolled away is kept as <binary>.bad.<timestamp> for inspection.
#
# Everything the scripts touch is overridable through environment
# variables, so installer/tests/safety-net-test.sh can run them against a
# temp directory and a fake systemctl.

NV_SYSTEMCTL="${NIVAROOS_SYSTEMCTL:-systemctl}"
NV_BIN_DIR="${NIVAROOS_BIN_DIR:-/usr/bin}"
NV_WWW_DIR="${NIVAROOS_WWW_DIR:-/var/lib/nivaroos/www}"
NV_STATE_DIR="${NIVAROOS_WATCHDOG_STATE:-/var/lib/nivaroos/watchdog}"
NV_LOG_FILE="${NIVAROOS_SAFETY_LOG:-/var/log/nivaroos/watchdog.log}"
NV_PAUSE_FILE="${NIVAROOS_WATCHDOG_PAUSE:-/run/nivaroos/watchdog.pause}"
NV_LOG_TAG="${NV_LOG_TAG:-nivaroos-safety}"

nv_now() {
	if [ -n "${NIVAROOS_NOW:-}" ]; then echo "$NIVAROOS_NOW"; else date +%s; fi
}

nv_log() {
	local msg="$*"
	mkdir -p "$(dirname "$NV_LOG_FILE")" 2>/dev/null || true
	printf '%s %s: %s\n' "$(date '+%Y-%m-%dT%H:%M:%S%z')" "$NV_LOG_TAG" "$msg" >>"$NV_LOG_FILE" 2>/dev/null || true
	if [ -z "${NIVAROOS_NO_SYSLOG:-}" ] && command -v logger >/dev/null 2>&1; then
		logger -t "$NV_LOG_TAG" -- "$msg" 2>/dev/null || true
	fi
	[ -n "${NV_VERBOSE:-}" ] && printf '%s\n' "$msg" >&2
	return 0
}

# nv_prop UNIT PROP - one systemd property value ("" when unknown).
nv_prop() {
	"$NV_SYSTEMCTL" show -p "$2" --value "$1" 2>/dev/null | head -n 1
}

# nv_unit_binary UNIT - the /usr/bin/nivaroos* binary UNIT runs, or "".
# ExecStart looks like "{ path=/usr/bin/nivaroos-gateway ; argv[]=... }".
nv_unit_binary() {
	local exec path
	exec="$("$NV_SYSTEMCTL" show -p ExecStart --value "$1" 2>/dev/null)"
	path="$(printf '%s\n' "$exec" | sed -n 's/.*path=\([^ ;]*\).*/\1/p' | head -n 1)"
	case "$path" in
		"$NV_BIN_DIR"/nivaroos*) ;;
		*) return 0 ;;
	esac
	case "$path" in
		*.prev | *.bad.* | *.new | *.bak) return 0 ;;
	esac
	printf '%s\n' "$path"
}

# nv_units - enabled NivaroOS service units (not the safety net's own).
nv_units() {
	"$NV_SYSTEMCTL" list-unit-files --type=service --state=enabled --no-legend --no-pager 'nivaroos*' 2>/dev/null |
		awk '{print $1}' | grep -E '^nivaroos[a-z0-9-]*\.service$' | grep -v '^nivaroos-watchdog\.service$' || true
}

# nv_units_for_binary BIN - enabled units whose ExecStart is BIN.
nv_units_for_binary() {
	local u
	for u in $(nv_units); do
		[ "$(nv_unit_binary "$u")" = "$1" ] && printf '%s\n' "$u"
	done
	return 0
}

# nv_binaries - installed NivaroOS binaries (not .prev/.bad/... copies).
nv_binaries() {
	local f
	for f in "$NV_BIN_DIR"/nivaroos*; do
		[ -f "$f" ] || continue
		case "$f" in
			*.prev | *.bad.* | *.new | *.bak | *.tmp | *.tmp.*) continue ;;
		esac
		printf '%s\n' "$f"
	done
}

# nv_changed_at FILE - seconds since epoch of FILE's last content/rename
# change (the later of mtime and ctime: `mv new bin` keeps new's mtime but
# sets ctime).
nv_changed_at() {
	local m c
	m=$(stat -c %Y "$1" 2>/dev/null || echo 0)
	c=$(stat -c %Z "$1" 2>/dev/null || echo 0)
	[ "$m" -gt "$c" ] && echo "$m" || echo "$c"
}

# nv_has_prev BIN - true when BIN.prev exists and differs from BIN.
nv_has_prev() {
	[ -f "$1.prev" ] && [ -f "$1" ] && ! cmp -s "$1" "$1.prev"
}

# nv_snapshot BIN - keep the current BIN as BIN.prev (before replacing it).
# Identical copies are left alone so re-running an install doesn't throw
# away the last different build.
nv_snapshot() {
	local bin="$1"
	[ -f "$bin" ] || return 0
	if [ -f "$bin.prev" ] && cmp -s "$bin" "$bin.prev"; then
		return 0
	fi
	cp -p "$bin" "$bin.prev.tmp.$$" && mv -f "$bin.prev.tmp.$$" "$bin.prev"
}

# nv_install_binary NEW BIN - atomically put NEW in place as BIN (0755).
nv_install_binary() {
	install -m 755 "$1" "$2.new.$$" && mv -f "$2.new.$$" "$2"
}

# nv_restore_prev BIN - BIN.prev goes back in place; the replaced BIN is
# kept as BIN.bad.<ts> (only the newest 2 are kept).
nv_restore_prev() {
	local bin="$1" ts
	nv_has_prev "$bin" || return 1
	ts=$(date +%Y%m%d-%H%M%S)
	cp -p "$bin" "$bin.bad.$ts" 2>/dev/null || true
	cp -p "$bin.prev" "$bin.restore.tmp.$$" && mv -f "$bin.restore.tmp.$$" "$bin" || return 1
	# shellcheck disable=SC2012
	ls -1t "$bin".bad.* 2>/dev/null | tail -n +3 | while IFS= read -r old; do rm -f "$old"; done
	return 0
}

# nv_swap_prev BIN - exchange BIN and BIN.prev (a manual rollback that can
# itself be undone by running it again).
nv_swap_prev() {
	local bin="$1"
	nv_has_prev "$bin" || return 1
	cp -p "$bin" "$bin.swap.tmp.$$" || return 1
	cp -p "$bin.prev" "$bin.restore.tmp.$$" && mv -f "$bin.restore.tmp.$$" "$bin" || { rm -f "$bin.swap.tmp.$$"; return 1; }
	mv -f "$bin.swap.tmp.$$" "$bin.prev"
}

# nv_snapshot_www - keep the installed web dashboard as www.prev.
nv_snapshot_www() {
	[ -f "$NV_WWW_DIR/index.html" ] || return 0
	rm -rf "$NV_WWW_DIR.prev.tmp"
	cp -a "$NV_WWW_DIR" "$NV_WWW_DIR.prev.tmp" || return 1
	rm -rf "$NV_WWW_DIR.prev"
	mv "$NV_WWW_DIR.prev.tmp" "$NV_WWW_DIR.prev"
}

# nv_swap_www - exchange www and www.prev.
nv_swap_www() {
	[ -f "$NV_WWW_DIR.prev/index.html" ] || return 1
	rm -rf "$NV_WWW_DIR.swap"
	mv "$NV_WWW_DIR" "$NV_WWW_DIR.swap" || return 1
	mv "$NV_WWW_DIR.prev" "$NV_WWW_DIR" || { mv "$NV_WWW_DIR.swap" "$NV_WWW_DIR"; return 1; }
	mv "$NV_WWW_DIR.swap" "$NV_WWW_DIR.prev"
}

# nv_pause SECONDS-OF-VALIDITY - tell the watchdog to stay out of the way
# (an install or deploy is replacing binaries and restarting units). The
# pause expires on its own so a crashed installer can't disable it forever.
nv_pause() {
	mkdir -p "$(dirname "$NV_PAUSE_FILE")" 2>/dev/null || true
	echo "$(($(nv_now) + ${1:-3600}))" >"$NV_PAUSE_FILE" 2>/dev/null || true
}

nv_unpause() {
	rm -f "$NV_PAUSE_FILE" 2>/dev/null || true
}

nv_paused() {
	[ -f "$NV_PAUSE_FILE" ] || return 1
	local until
	until=$(cat "$NV_PAUSE_FILE" 2>/dev/null)
	case "$until" in '' | *[!0-9]*) until=0 ;; esac
	[ "$(nv_now)" -lt "$until" ]
}

# nv_unit_healthy UNIT - running and not restarting.
nv_unit_healthy() {
	[ "$(nv_prop "$1" ActiveState)" = "active" ] && [ "$(nv_prop "$1" SubState)" = "running" ]
}

# nv_restart UNIT - clear a start-limit "failed" state and restart.
nv_restart() {
	"$NV_SYSTEMCTL" reset-failed "$1" >/dev/null 2>&1 || true
	"$NV_SYSTEMCTL" restart "$1" >/dev/null 2>&1
}
