#!/usr/bin/env bash
# apply.sh - install a NivaroOS server bundle (built by installer/build-bundle.sh).
# The one apply path for a first install, a from-source update (install.sh)
# and a release update (nivaroos update): self-updater design, sections 3 and 7.
#
#   apply.sh install [--with vm,download-station,backup]   install.sh: the
#            components chosen there, plus every add-on already installed
#   apply.sh update     nivaroos update: the installed components only; a
#            failed health check or probation rolls the whole set back
#   apply.sh rollback [--restore-data] [--reason TEXT]
#   apply.sh probation  watch the last update again (after a reboot)
#
# rollback and probation are run from the copy apply puts in the snapshot
# ($UPDATE_DIR/snap/apply.sh), so they always match the release that made it.
#
# Only what changed is touched: a file byte-identical to the installed one
# is skipped, and a component none of whose files changed keeps its service
# running (Backup & Sync, VM consoles, Host Desktop never notice).
#
# Steps (each printed as "::step NAME" for nivaroos update):
#   verify    every bundle file against MANIFEST (nothing changed if wrong)
#   stopping  watchdog paused; snapshot (old files, .prev, www.prev); changed
#             services stopped, gateway last; their databases copied
#   migrating migrations/NNNN-*.sh not yet run on this box (expand-only)
#   installing files renamed into place, units reloaded, services started
#   health    every restarted service steady, the gateway answering /ping
#   probation (update) NIVAROOS_PROBATION seconds (600) of the same checks
#
# Exit: 0 done (or nothing to do), 1 failed (update: rolled back), 2 failed
# and the rollback is unhealthy too, 3 refused before anything changed.
#
# Tests (installer/tests/bundle-apply-test.sh) run it against a temp root:
# NV_ROOT prefixes every path; NIVAROOS_SYSTEMCTL / NIVAROOS_CURL are fakes.
set -Eeuo pipefail

B="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
r="${NV_ROOT:-}"
export NIVAROOS_BIN_DIR="$r/usr/bin"
export NIVAROOS_WWW_DIR="$r/var/lib/nivaroos/www"
export NIVAROOS_WATCHDOG_PAUSE="${NIVAROOS_WATCHDOG_PAUSE:-$r/run/nivaroos/watchdog.pause}"
export NIVAROOS_SAFETY_LOG="${NIVAROOS_SAFETY_LOG:-$r/var/log/nivaroos/update.log}"
NV_LOG_TAG=nivaroos-update
# shellcheck source=../nivaroos-safety-lib.sh
. "$B/lib/safety-lib.sh"

UPDATE_DIR="${NIVAROOS_UPDATE_DIR:-$r/var/lib/nivaroos/update}"
SNAP="$UPDATE_DIR/snap"
UNIT_DIR="$r/usr/lib/systemd/system"
DROPIN_DIR="$r/etc/systemd/system"
INSTALL_MANIFEST="$r/var/lib/nivaroos/manifest"
CURL="${NIVAROOS_CURL:-curl}"
SETTLE="${NIVAROOS_DEPLOY_SETTLE:-10}"
PROBATION="${NIVAROOS_PROBATION:-600}"
PROBE_EVERY="${NIVAROOS_PROBE_EVERY:-15}"

# Start order (gateway first, as install.sh always did); stop is the reverse.
ORDER="nivaroos-gateway.service nivaroos-message-bus.service nivaroos-user-service.service
nivaroos-local-storage.service nivaroos-app-management.service nivaroos-gpu-sidecar.service
nivaroos-fans.service nivaroos.service nivaroos-vm-sidecar.service nivaroos-download-sidecar.service
nivaroos-backup.service nivaroos-host-firewall.service nivaroos-watchdog.timer nivaroos-update-check.timer"

# Databases copied (while their service is stopped) before a component
# changes, and put back by a rollback right after a failed update.
data_paths() {
	case "$1" in
		core) echo /var/lib/nivaroos/db/casaOS.db ;;
		user) echo /var/lib/nivaroos/db/user.db ;;
		local-storage) echo /var/lib/nivaroos/db/local-storage.db ;;
		message-bus) echo /var/lib/nivaroos/db/message-bus-notifications.db ;;
		backup) echo /var/lib/nivaroos/backup/backup.db ;;
	esac
}

say() { printf '%s\n' "$*"; nv_log "$*"; }
step() { printf '::step %s\n' "$1"; }
die3() { say "refused: $*"; exit 3; }

# A daemon is stopped before its files change; a timer or the firewall's
# oneshot is only restarted afterwards (stopping the firewall unit would
# drop its rules for a moment).
restart_only() { case "$1" in *.timer | nivaroos-host-firewall.service) return 0 ;; esac; return 1; }

ordered() { # ordered UNIT... - in start order
	local u out=() rest=() want=" $* "
	for u in $ORDER; do [[ "$want" == *" $u "* ]] && out+=("$u"); done
	for u in "$@"; do [[ " $ORDER " == *[[:space:]]"$u"[[:space:]]* ]] || rest+=("$u"); done
	printf '%s\n' "${out[@]}" "${rest[@]}" | sed '/^$/d'
}

gateway_port() {
	local p
	p="$(sed -n 's/^[[:space:]]*port[[:space:]]*=[[:space:]]*\([0-9][0-9]*\).*/\1/p' "$r/etc/nivaroos/gateway.ini" 2>/dev/null | head -n 1)"
	echo "${p:-80}"
}
gateway_ok() {
	local code
	code="$("$CURL" -s -o /dev/null -m 5 -w '%{http_code}' "http://127.0.0.1:$(gateway_port)/ping" 2>/dev/null || true)"
	[ "$code" = 200 ]
}

# healthy UNIT... - each daemon running and not restarted for SETTLE
# seconds, timers active, and the gateway answering within 30 s.
healthy() {
	local u ok
	declare -A nr0=()
	for u in "$@"; do nr0[$u]="$(nv_prop "$u" NRestarts)"; done
	sleep "$SETTLE"
	ok=0
	for u in "$@"; do
		case "$u" in
			*.timer) [ "$(nv_prop "$u" ActiveState)" = active ] || { say "health: $u is not active"; ok=1; } ;;
			nivaroos-host-firewall.service) [ "$(nv_prop "$u" ActiveState)" != failed ] || { say "health: $u failed"; ok=1; } ;;
			*)
				if ! nv_unit_healthy "$u" || [ "${nr0[$u]:-0}" != "$(nv_prop "$u" NRestarts)" ]; then
					say "health: $u is not running steadily"
					ok=1
				fi
				;;
		esac
	done
	for _ in 1 2 3 4 5 6; do
		gateway_ok && return $ok
		sleep 5
	done
	say "health: the gateway does not answer /ping on port $(gateway_port)"
	return 1
}

daemon_reload() { "$NV_SYSTEMCTL" daemon-reload >/dev/null 2>&1 || true; }

# ---------------------------------------------------------------------------
# rollback - put back everything the last apply changed.
# ---------------------------------------------------------------------------
# Returns 0 (rolled back, healthy) or 2 (the old version is unhealthy too).
do_rollback() {
	local restore_data="" reason="" dst u f
	while [ $# -gt 0 ]; do
		case "$1" in
			--restore-data) restore_data=yes ;;
			--reason) reason="${2:-}"; shift ;;
		esac
		shift
	done
	step rolling_back
	say "rolling back $(cat "$SNAP/to-version") to $(cat "$SNAP/from-version")${reason:+: $reason}"
	nv_pause 1800
	local units=()
	mapfile -t units <"$SNAP/units"
	for u in $(ordered "${units[@]}" | tac); do
		restart_only "$u" || "$NV_SYSTEMCTL" stop "$u" >/dev/null 2>&1 || true
	done
	while IFS= read -r dst; do
		[ -n "$dst" ] || continue
		case "$dst" in
			/usr/bin/nivaroos*) nv_restore_prev "$r$dst" || say "rollback: no previous $dst" ;;
			*) if [ -f "$SNAP/files$dst" ]; then cp -p "$SNAP/files$dst" "$r$dst.rb.$$" && mv -f "$r$dst.rb.$$" "$r$dst"; fi ;;
		esac
	done <"$SNAP/changed"
	while IFS= read -r dst; do
		if [ -n "$dst" ]; then rm -f "$r$dst"; fi
	done <"$SNAP/added"
	if [ -f "$SNAP/www" ]; then nv_swap_www || say "rollback: no previous web dashboard"; fi
	if [ -n "$restore_data" ] && [ -d "$SNAP/data" ]; then
		while IFS= read -r -d '' f; do
			dst="${f#"$SNAP/data"}"
			rm -f "$r$dst-wal" "$r$dst-shm"
			cp -p "$f" "$r$dst"
		done < <(find "$SNAP/data" -type f -print0)
		say "rollback: databases restored from before the update"
	fi
	daemon_reload
	for u in $(ordered "${units[@]}"); do nv_restart "$u" || true; done
	echo rolled-back >"$SNAP/state"
	if [ "${#units[@]}" -eq 0 ] || healthy "${units[@]}"; then
		say "rolled back to $(cat "$SNAP/from-version")"
		nv_unpause
		return 0
	fi
	say "STUCK: the previous version is not healthy either - see nivaroos-recover status"
	nv_unpause
	return 2
}

# rolled_back_exit - after a failed update: 1 when the rollback is healthy, 2 when not.
rolled_back_exit() { if do_rollback "$@"; then exit 1; fi; exit 2; }

# probation - NIVAROOS_PROBATION seconds of watching the restarted units;
# any failure rolls the whole set back (not one binary - that is the
# watchdog's job outside an update).
do_probation() {
	[ -f "$SNAP/units" ] || exit 0
	local units=() u until fails=0
	mapfile -t units <"$SNAP/units"
	declare -A base=()
	for u in "${units[@]}"; do base[$u]="$(nv_prop "$u" NRestarts)"; done
	step probation
	nv_pause $((PROBATION + 600))
	until=$(($(nv_now) + PROBATION))
	while [ "$(nv_now)" -lt "$until" ]; do
		sleep "$PROBE_EVERY"
		for u in "${units[@]}"; do
			restart_only "$u" && continue
			if [ "$(nv_prop "$u" ActiveState)" = failed ] || [ "$(nv_prop "$u" NRestarts)" -ge $((${base[$u]:-0} + 2)) ] 2>/dev/null; then
				nv_unpause
				rolled_back_exit --restore-data --reason "$u failed during probation"
			fi
		done
		if gateway_ok; then fails=0; else fails=$((fails + 1)); fi
		if [ "$fails" -ge 3 ]; then
			nv_unpause
			rolled_back_exit --restore-data --reason "the gateway stopped answering during probation"
		fi
	done
	echo committed >"$SNAP/state"
	nv_unpause
	say "update to $(cat "$SNAP/to-version") committed"
}

# ---------------------------------------------------------------------------
# install / update
# ---------------------------------------------------------------------------
do_apply() {
	local mode="$1" with=","
	shift
	while [ $# -gt 0 ]; do
		case "$1" in
			--with) with=",${2:-},"; shift ;;
		esac
		shift
	done
	[ -f "$B/MANIFEST" ] || die3 "no MANIFEST in $B"
	mkdir -p "$UPDATE_DIR"
	chmod 700 "$UPDATE_DIR"
	exec 8>"$UPDATE_DIR/.apply.lock"
	if command -v flock >/dev/null 2>&1 && ! flock -n 8; then die3 "another apply is running"; fi

	# --- verify -------------------------------------------------------------
	step verify
	local VERSION="" kind a b c d
	declare -A WHEN=() CUNITS=() FCOMP=() FMODE=() FSHA=()
	local COMPS=() FILES=()
	while read -r kind a b c d; do
		case "$kind" in
			version) VERSION="$a" ;;
			component) COMPS+=("$a"); WHEN[$a]="$b"; CUNITS[$a]="$c" ;;
			file) FILES+=("$d"); FCOMP[$d]="$a"; FMODE[$d]="$b"; FSHA[$d]="$c" ;;
		esac
	done <"$B/MANIFEST"
	local rel
	for rel in "${FILES[@]}"; do
		case "$rel" in /* | *..*) die3 "bad path in MANIFEST: $rel" ;; esac
		[ "$(sha256sum "$B/$rel" 2>/dev/null | cut -d' ' -f1)" = "${FSHA[$rel]}" ] || die3 "$rel does not match MANIFEST"
	done

	# Which components this box gets.
	declare -A SEL=()
	local comp when
	for comp in "${COMPS[@]}"; do
		when="${WHEN[$comp]}"
		if [ "$when" = always ] || [ -e "$r$when" ] || { [ "$mode" = install ] && [[ "$with" == *",$comp,"* ]]; }; then
			SEL[$comp]=1
		fi
	done

	# What changed.
	declare -A CHANGED=()
	local changed=() added=() dst
	for rel in "${FILES[@]}"; do
		comp="${FCOMP[$rel]}"
		[ -n "${SEL[$comp]:-}" ] || continue
		dst="${rel#root}"
		if [ ! -f "$r$dst" ]; then
			added+=("$rel")
			CHANGED[$comp]=1
		elif ! cmp -s "$B/$rel" "$r$dst"; then
			changed+=("$rel")
			CHANGED[$comp]=1
		fi
	done
	# A dashboard with files the new one doesn't have changed too (it is
	# swapped as a whole).
	local www_new=""
	if [ -n "${SEL[ui]:-}" ]; then
		local n_bundle n_inst
		n_bundle="$(printf '%s\n' "${FILES[@]}" | grep -c '^root/var/lib/nivaroos/www/' || true)"
		n_inst="$(find "$r/var/lib/nivaroos/www" -type f 2>/dev/null | wc -l || true)"
		[ "$n_bundle" = "$n_inst" ] || CHANGED[ui]=1
		[ -n "${CHANGED[ui]:-}" ] && www_new=yes
	fi
	# A from-source bundle has no release.json: an installed one would
	# claim the old release.
	local drop_release=""
	if [ -z "${FSHA[root/usr/share/nivaroos/release.json]:-}" ] && [ -f "$r/usr/share/nivaroos/release.json" ]; then
		drop_release=yes
	fi

	if [ "${#CHANGED[@]}" -eq 0 ] && [ -z "$drop_release" ] && [ "$mode" = update ]; then
		say "$VERSION is already installed - nothing to do"
		exit 0
	fi
	say "applying $VERSION ($mode): changed ${!CHANGED[*]}"

	# --- stopping -----------------------------------------------------------
	step stopping
	nv_pause 3600
	local units=() u
	for comp in "${!CHANGED[@]}"; do
		[ "${CUNITS[$comp]}" = - ] && continue
		IFS=',' read -r -a us <<<"${CUNITS[$comp]}"
		units+=("${us[@]}")
	done
	declare -A WAS_ACTIVE=()
	for u in "${units[@]}"; do
		"$NV_SYSTEMCTL" is-active --quiet "$u" 2>/dev/null && WAS_ACTIVE[$u]=1
	done

	rm -rf "$SNAP.new"
	mkdir -p "$SNAP.new/files" "$SNAP.new/data" "$SNAP.new/lib"
	cp -p "$B/apply.sh" "$SNAP.new/apply.sh"
	cp -p "$B/lib/safety-lib.sh" "$SNAP.new/lib/safety-lib.sh"
	echo "$VERSION" >"$SNAP.new/to-version"
	sed -n 's/.*"version":"\([^"]*\)".*/\1/p' "$r/usr/share/nivaroos/release.json" 2>/dev/null >"$SNAP.new/from-version" || true
	[ -s "$SNAP.new/from-version" ] || echo source >"$SNAP.new/from-version"
	: >"$SNAP.new/changed"
	: >"$SNAP.new/added"
	for rel in "${changed[@]}"; do
		dst="${rel#root}"
		case "$dst" in
			/var/lib/nivaroos/www/*) continue ;;
			/usr/bin/nivaroos*) nv_snapshot "$r$dst" ;;
			*) install -D -p -m "$(stat -c %a "$r$dst")" "$r$dst" "$SNAP.new/files$dst" ;;
		esac
		echo "$dst" >>"$SNAP.new/changed"
	done
	for rel in "${added[@]}"; do
		case "$rel" in root/var/lib/nivaroos/www/*) continue ;; esac
		echo "${rel#root}" >>"$SNAP.new/added"
	done
	if [ -n "$drop_release" ]; then
		install -D -p "$r/usr/share/nivaroos/release.json" "$SNAP.new/files/usr/share/nivaroos/release.json"
		echo /usr/share/nivaroos/release.json >>"$SNAP.new/changed"
	fi
	if [ -n "$www_new" ]; then
		nv_snapshot_www
		touch "$SNAP.new/www"
	fi
	# Restarted afterwards: the changed components' units that were running
	# (update), or all of them (install); a unit file new in this release
	# is enabled and started either way.
	local restart=()
	for u in "${units[@]}"; do
		if [ "$mode" = install ] || [ -n "${WAS_ACTIVE[$u]:-}" ] || [ ! -f "$UNIT_DIR/$u" ]; then
			restart+=("$u")
		fi
	done
	printf '%s\n' "${restart[@]}" | sed '/^$/d' >"$SNAP.new/units"
	echo applying >"$SNAP.new/state"
	rm -rf "$SNAP.old"
	if [ -d "$SNAP" ]; then mv "$SNAP" "$SNAP.old"; fi
	mv "$SNAP.new" "$SNAP"
	rm -rf "$SNAP.old"

	for u in $(ordered "${units[@]}" | tac); do
		restart_only "$u" || "$NV_SYSTEMCTL" stop "$u" >/dev/null 2>&1 || true
	done
	local p
	for comp in "${!CHANGED[@]}"; do
		for p in $(data_paths "$comp"); do
			for f in "$r$p" "$r$p-wal" "$r$p-shm"; do
				if [ -f "$f" ]; then install -D -p -m 600 "$f" "$SNAP/data${f#"$r"}"; fi
			done
		done
	done

	fail() {
		say "failed: $*"
		[ "$mode" != update ] || rolled_back_exit --restore-data --reason "$*"
		nv_unpause
		exit 1
	}

	# --- migrating ----------------------------------------------------------
	step migrating
	local m name done_file="$UPDATE_DIR/migrations.done"
	touch "$done_file"
	for m in "$B"/migrations/[0-9][0-9][0-9][0-9]-*.sh; do
		[ -f "$m" ] || continue
		name="${m##*/}"
		grep -qxF "$name" "$done_file" && continue
		say "migration $name"
		NV_ROOT="$r" bash "$m" || fail "migration $name failed"
		echo "$name" >>"$done_file"
	done

	# --- installing ---------------------------------------------------------
	step installing
	local reload=""
	for rel in "${changed[@]}" "${added[@]}"; do
		[ -n "$rel" ] || continue
		dst="${rel#root}"
		case "$dst" in /var/lib/nivaroos/www/*) continue ;; esac
		# Checked again right before it goes in (no check-then-use gap).
		[ "$(sha256sum "$B/$rel" | cut -d' ' -f1)" = "${FSHA[$rel]}" ] || fail "$rel changed after verification"
		mkdir -p "$(dirname "$r$dst")"
		{ install -m "${FMODE[$rel]}" "$B/$rel" "$r$dst.new.$$" && mv -f "$r$dst.new.$$" "$r$dst"; } || fail "could not install $dst"
		case "$dst" in /usr/lib/systemd/system/*) reload=yes ;; esac
		grep -qxF "$dst" "$INSTALL_MANIFEST" 2>/dev/null || echo "$dst" >>"$INSTALL_MANIFEST"
	done
	if [ -n "$drop_release" ]; then rm -f "$r/usr/share/nivaroos/release.json"; fi
	if [ -n "$www_new" ]; then
		rm -rf "$NV_WWW_DIR.new" "$NV_WWW_DIR.old"
		cp -a "$B/root/var/lib/nivaroos/www" "$NV_WWW_DIR.new" || fail "could not copy the web dashboard"
		if [ -d "$NV_WWW_DIR" ]; then mv "$NV_WWW_DIR" "$NV_WWW_DIR.old"; fi
		mv "$NV_WWW_DIR.new" "$NV_WWW_DIR" || fail "could not install the web dashboard"
		rm -rf "$NV_WWW_DIR.old"
	fi
	# Live configs come from their sample only when there is none yet.
	for f in "$r"/etc/nivaroos/*.sample; do
		if [ -f "$f" ] && [ ! -e "${f%.sample}" ]; then cp -p "$f" "${f%.sample}"; fi
	done
	mkdir -p "$r/usr/local/bin"
	ln -sfn /usr/bin/nivaroos-cli "$r/usr/local/bin/nivaroos"
	ln -sfn /usr/bin/nivaroos-cli "$r/usr/bin/casaos-cli"
	ln -sfn /usr/bin/nivaroos-uninstall "$r/usr/local/bin/nivaroos-uninstall"
	# Restart limits for every NivaroOS service (explicit per-unit drop-ins:
	# prefix drop-ins need a newer systemd than some supported distros ship).
	local res="$r/usr/local/lib/nivaroos/10-nivaroos-resilience.conf" uf
	if [ -f "$res" ]; then
		for uf in "$UNIT_DIR"/nivaroos*.service; do
			[ -f "$uf" ] || continue
			u="${uf##*/}"
			[ "$u" = nivaroos-watchdog.service ] && continue
			if ! cmp -s "$res" "$DROPIN_DIR/$u.d/10-nivaroos-resilience.conf"; then
				install -D -m 644 "$res" "$DROPIN_DIR/$u.d/10-nivaroos-resilience.conf"
				reload=yes
			fi
		done
	fi
	# Folders the units need (idempotent; the services keep them up to date).
	mkdir -p "$r/var/lib/nivaroos/db" "$r/var/lib/nivaroos/watchdog" "$r/var/log/nivaroos" "$r/etc/nivaroos"
	if [ -n "${SEL[fans]:-}" ]; then install -d -m 700 "$r/var/lib/nivaroos/fans"; mkdir -p "$r/etc/modules-load.d"; fi
	if [ -n "${SEL[download-station]:-}" ]; then install -d -m 700 "$r/var/lib/nivaroos/download-station" "$r/var/lib/nivaroos/torrent"; fi
	if [ -n "${SEL[backup]:-}" ]; then install -d -m 700 "$r/var/lib/nivaroos/backup"; fi
	if [ -n "${CHANGED[fans]:-}" ] && [ -z "$r" ]; then
		/usr/bin/nivaroos-fans detect-modules || say "fan driver detection failed - motherboard fans stay under BIOS control"
	fi
	if [ -n "$reload" ]; then daemon_reload; fi

	if [ "$mode" = install ]; then
		# Every chosen component's services enabled and running.
		for comp in "${!SEL[@]}"; do
			[ "${CUNITS[$comp]}" = - ] && continue
			IFS=',' read -r -a us <<<"${CUNITS[$comp]}"
			for u in "${us[@]}"; do "$NV_SYSTEMCTL" enable "$u" >/dev/null 2>&1 || true; done
			[ -n "${CHANGED[$comp]:-}" ] || for u in "${us[@]}"; do "$NV_SYSTEMCTL" start "$u" >/dev/null 2>&1 || true; done
		done
	fi
	for u in "${restart[@]}"; do
		[ -n "${WAS_ACTIVE[$u]:-}" ] || "$NV_SYSTEMCTL" enable "$u" >/dev/null 2>&1 || true
	done
	for u in $(ordered "${restart[@]}"); do
		nv_restart "$u" || say "start of $u failed"
	done
	echo applied >"$SNAP/state"

	# --- health -------------------------------------------------------------
	step health
	if [ "${#restart[@]}" -gt 0 ] && ! healthy "${restart[@]}"; then
		fail "health check"
	fi
	say "$VERSION installed (${!CHANGED[*]})"
	if [ "$mode" = update ]; then
		nv_unpause
		do_probation
	else
		echo committed >"$SNAP/state"
		nv_unpause
	fi
	exit 0
}

case "${1:-}" in
	install | update) do_apply "$@" ;;
	rollback)
		shift
		[ -f "$SNAP/changed" ] || die3 "no update to roll back (no snapshot in $SNAP)"
		[ "$(cat "$SNAP/state" 2>/dev/null)" != rolled-back ] || die3 "the last update was already rolled back"
		if do_rollback "$@"; then exit 0; fi
		exit 2
		;;
	probation) do_probation ;;
	*) sed -n '2,30p' "$0"; exit 3 ;;
esac
