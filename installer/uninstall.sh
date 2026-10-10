#!/usr/bin/env bash
# ==============================================================================
#  NivaroOS uninstaller
#  https://github.com/F-e-n-y-x/NivaroOS
#
#    sudo nivaroos-uninstall                 asks before deleting any data
#    sudo nivaroos-uninstall --yes           unattended; keeps all data
#    sudo nivaroos-uninstall --yes --delete-data
#
#  Removes NivaroOS's services, units, drop-ins, binaries, CLIs, web
#  dashboard, config and state. Your data (/DATA: apps' data, VMs, files) is
#  only deleted when you say so, and never on another drive: drives mounted
#  under /DATA, or a /DATA that is itself a separate drive or storage pool,
#  are left alone. Docker, Go and other system packages stay installed.
#  Safe to re-run.
# ==============================================================================

if [ -z "${BASH_VERSION:-}" ]; then
	exec bash "$0" "$@"
fi

set -Eeuo pipefail

ALL_UNITS="nivaroos-watchdog.timer nivaroos-watchdog.service nivaroos-gateway.service nivaroos-message-bus.service nivaroos.service nivaroos-user-service.service nivaroos-app-management.service nivaroos-local-storage.service nivaroos-gpu-sidecar.service nivaroos-fans.service nivaroos-vm-sidecar.service nivaroos-download-sidecar.service nivaroos-torrent.service nivaroos-ds-browser.socket nivaroos-ds-browser.service nivaroos-ds-browser-install.service nivaroos-backup.service nivaroos-host-desktop.service rclone.service"
MANIFEST_FILE="/var/lib/nivaroos/manifest"
DESKTOP_PROVISION_MARKER="/var/lib/nivaroos/provisioned-desktop"
GDM_WAYLAND_MARKER="/var/lib/nivaroos/host-desktop-gdm-wayland"
APPS_DIR="/var/lib/nivaroos/apps"
DATA_DIR="${NIVAROOS_DATA_DIR:-/DATA}"
LOG_FILE="/var/log/nivaroos-uninstall.log"

YES=""
DATA_MODE="" # "" ask, keep, delete
DRY_RUN=""
REMOVE_PROVISIONED_DESKTOP=""
DEL_APPS=no
DEL_VMS=no
DEL_FILES=no
STEP_NUM=0
TOTAL_STEPS=0
FAILED_STEPS=()
SKIPPED_PATHS=()
LEFTOVER_ITEMS=""
PROVISIONED_DESKTOP_INFO=""
GDM_WAYLAND_INFO=""

# >>> nivaroos-ui - one block, copied verbatim into install.sh, uninstall.sh
# and nivaroos-safety-lib.sh (installer/tests/ui-block-test.sh keeps the
# copies identical; change it here, then copy). Ink and greys like the
# app's Rack style; colour only for status. No colour when stdout is not a
# terminal, NO_COLOR is set, or TERM=dumb; ASCII without a UTF-8 locale.
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ] && [ "${TERM:-dumb}" != dumb ]; then
	UI_B=$'\033[1m' UI_D=$'\033[2m' UI_R=$'\033[0m'
	UI_OK=$'\033[38;5;36m' UI_WARN=$'\033[38;5;178m' UI_ERR=$'\033[38;5;167m' UI_MINT=$'\033[38;5;122m'
else
	UI_B='' UI_D='' UI_R='' UI_OK='' UI_WARN='' UI_ERR='' UI_MINT=''
fi
case "${LC_ALL:-${LC_CTYPE:-${LANG:-}}}" in
	*[Uu][Tt][Ff]-8* | *[Uu][Tt][Ff]8*) UI_I_OK='✓' UI_I_ERR='✗' UI_I_DOT='●' UI_N1='█▀▄  █' UI_N2='█  ▀▄█' ;;
	*) UI_I_OK='ok' UI_I_ERR='x' UI_I_DOT='o' UI_N1='|\   |' UI_N2='|  \ |' ;;
esac
# ui_banner TITLE [SUBTITLE] - the "Ni" mark (an N whose last stroke is the
# stem of an i, mint dot on top) with the title beside it.
ui_banner() {
	printf '\n       %s%s%s\n' "$UI_MINT" "$UI_I_DOT" "$UI_R"
	printf '  %s   %sNivaroOS%s %s\n' "$UI_N1" "$UI_B" "$UI_R" "$1"
	printf '  %s   %s%s%s\n' "$UI_N2" "$UI_D" "${2:-}" "$UI_R"
}
ui_head() { printf '\n  %s%s%s\n' "$UI_B" "$1" "$UI_R"; }
ui_ok() { printf '  %s%s%s %s\n' "$UI_OK" "$UI_I_OK" "$UI_R" "$1"; }
ui_info() { printf '  %s-%s %s\n' "$UI_D" "$UI_R" "$1"; }
ui_warn() { printf '  %s!%s %s\n' "$UI_WARN" "$UI_R" "$1" >&2; }
ui_err() { printf '  %s%s%s %s\n' "$UI_ERR" "$UI_I_ERR" "$UI_R" "$1" >&2; }
ui_die() { ui_err "$1"; exit "${2:-1}"; }
# ui_kv KEY VALUE - one aligned "key  value" row.
ui_kv() { printf '    %s%-12s%s %s\n' "$UI_D" "$1" "$UI_R" "$2"; }
# <<< nivaroos-ui

INTERACTIVE_TTY="false"
if [ -t 1 ] && { : </dev/tty; } 2>/dev/null; then
	INTERACTIVE_TTY="true"
fi

trap 'ui_err "The uninstaller stopped unexpectedly at line $LINENO (log: $LOG_FILE)."; exit 1' ERR
trap 'printf "\n"; ui_warn "Cancelled - run it again to finish (it is safe to repeat)."; exit 130' INT TERM

usage() {
	ui_banner "uninstaller" "Removes NivaroOS; asks before deleting data"
	cat <<'EOF'

  Usage
    sudo nivaroos-uninstall [options]

  Options
    -y, --yes                       Unattended: no questions. Data is kept
                                    unless --delete-data is given too
    --keep-data                     Keep all data in /DATA (apps' data, VMs, files)
                                    and the installed apps' containers
    --delete-data                   Delete apps (containers and /DATA/AppData),
                                    VMs (/DATA/VMs) and everything else in /DATA
    --remove-provisioned-desktop    Also remove a desktop NivaroOS installed for
                                    Host Desktop (never one you already had)
    --dry-run                       Show what would be removed; change nothing
    -h, --help                      Show this help

  Never touched: drives mounted under /DATA, a /DATA on its own drive or
  pool, Docker, Go and other system packages.

  Exit   0 done, 1 something could not be removed, 2 bad option, 130 cancelled
EOF
}

parse_args() {
	while [ $# -gt 0 ]; do
		case "$1" in
			--yes | -y | --unattended) YES=yes ;;
			--keep-data) DATA_MODE=keep ;;
			--delete-data | --purge-data) DATA_MODE=delete ;;
			--remove-provisioned-desktop) REMOVE_PROVISIONED_DESKTOP=yes ;;
			--dry-run | -n) DRY_RUN=yes ;;
			--width | -w) shift ;; # accepted for compatibility
			--width=*) ;;
			--help | -h)
				usage
				exit 0
				;;
			*)
				ui_err "Unknown option '$1' - see --help."
				exit 2
				;;
		esac
		[ $# -eq 0 ] || shift
	done
}

check_root() {
	[ "$(id -u)" -eq 0 ] && return 0
	[ -n "$DRY_RUN" ] && return 0
	if [ -f "$0" ] && command -v sudo >/dev/null 2>&1; then
		exec sudo -E bash "$0" "$@"
	fi
	ui_die "The uninstaller needs root: sudo nivaroos-uninstall"
}

# ask QUESTION - yes/no on the terminal, default no.
ask() {
	local reply=""
	printf '  ? %s [y/N]: ' "$1"
	read -r reply </dev/tty || reply=""
	case "$reply" in [yY] | [yY][eE][sS]) return 0 ;; esac
	return 1
}

# on_root_fs PATH - PATH is on the same filesystem as / (not a drive or
# pool mounted there or above it).
on_root_fs() {
	[ "$(stat -c %d "$1" 2>/dev/null)" = "$(stat -c %d / 2>/dev/null)" ]
}

# safe_rm PATH - delete PATH unless it, or the filesystem it is on, is not
# the system disk. --one-file-system also skips drives mounted inside it.
safe_rm() {
	local p="$1"
	[ -e "$p" ] || [ -L "$p" ] || return 0
	if ! on_root_fs "$p" || mountpoint -q "$p" 2>/dev/null; then
		echo "kept $p - it is on another drive"
		echo "$p" >>"$SKIP_FILE"
		return 0
	fi
	rm -rf --one-file-system "$p"
}

# choose_data decides, before anything is removed, what happens to data.
choose_data() {
	case "$DATA_MODE" in
		keep) return 0 ;;
		delete)
			DEL_APPS=yes DEL_VMS=yes DEL_FILES=yes
			return 0
			;;
	esac
	# Unattended, or no terminal to ask on: data is kept.
	[ -n "$YES" ] && return 0
	[ "$INTERACTIVE_TTY" = "true" ] || return 0
	ui_head "Your data"
	printf '    %sEach answer defaults to no: data is kept unless you say yes.%s\n\n' "$UI_D" "$UI_R"
	ask "Delete installed apps (their containers) and their data in ${DATA_DIR}/AppData?" && DEL_APPS=yes
	ask "Delete virtual machines (definitions and disks in ${DATA_DIR}/VMs)?" && DEL_VMS=yes
	ask "Delete everything else in ${DATA_DIR} (Documents, Downloads, Media, Gallery ...)?" && DEL_FILES=yes
	return 0
}

confirm() {
	ui_head "This will"
	ui_info "stop and remove every NivaroOS service, timer and drop-in"
	ui_info "remove NivaroOS binaries, CLIs, the web dashboard, /etc/nivaroos and /var/lib/nivaroos"
	if [ "$DEL_APPS" = yes ]; then ui_warn "DELETE installed apps' containers and ${DATA_DIR}/AppData" 2>&1; else ui_ok "keep installed apps and ${DATA_DIR}/AppData"; fi
	if [ "$DEL_VMS" = yes ]; then ui_warn "DELETE virtual machines and ${DATA_DIR}/VMs" 2>&1; else ui_ok "keep virtual machines and ${DATA_DIR}/VMs"; fi
	if [ "$DEL_FILES" = yes ]; then ui_warn "DELETE your other files in ${DATA_DIR}" 2>&1; else ui_ok "keep your files in ${DATA_DIR}"; fi
	ui_ok "leave drives mounted under ${DATA_DIR} and other drives alone"
	printf '\n'
	[ -n "$DRY_RUN" ] && return 0
	[ -n "$YES" ] && return 0
	if [ "$INTERACTIVE_TTY" != "true" ]; then
		ui_die "No terminal to confirm on - run with --yes to uninstall unattended (data is kept unless --delete-data)." 2
	fi
	ask "Uninstall NivaroOS?" || {
		ui_info "Nothing was changed."
		exit 0
	}
	printf '\n'
}

# run_step TITLE SCRIPT - best effort: a failed step is reported (with its
# last output lines) and the uninstall goes on; the exit code says so.
run_step() {
	local title="$1" out rc tag
	shift
	STEP_NUM=$((STEP_NUM + 1))
	tag="$(printf '%*d/%d' "${#TOTAL_STEPS}" "$STEP_NUM" "$TOTAL_STEPS")"
	[ -t 1 ] && printf '  %s-%s %s  %s' "$UI_D" "$UI_R" "$tag" "$title"
	out="$(mktemp)"
	printf '>>> %s\n' "$title" >>"$LOG_FILE" 2>/dev/null || true
	set +e
	trap '' ERR
	(
		trap - INT TERM
		eval "$*"
	) >"$out" 2>&1 </dev/null
	rc=$?
	trap 'ui_err "The uninstaller stopped unexpectedly at line $LINENO (log: $LOG_FILE)."; exit 1' ERR
	set -e
	cat "$out" >>"$LOG_FILE" 2>/dev/null || true
	[ -t 1 ] && printf '\r\033[K'
	if [ "$rc" -eq 0 ]; then
		ui_ok "$tag  $title"
	else
		ui_err "$tag  $title (exit code $rc)" 2>&1
		tail -n 8 "$out" | sed "s/^/      ${UI_D}|${UI_R} /"
		FAILED_STEPS+=("$title")
	fi
	rm -f "$out"
}

# Backup & Sync took over the old backup/sync Scheduled Tasks; hand them
# back while core still runs, so uninstalling never leaves a user's old
# backups switched off. Failing to reach core is reported, not fatal.
release_backup_tasks() {
	systemctl stop nivaroos-backup.service >/dev/null 2>&1 || true
	if ! /usr/bin/nivaroos-backup release-scheduled-tasks; then
		echo 'Could not hand every Scheduled Task back (is core running?). They stay disabled in schedules.json.' >&2
	fi
}

# remove_apps stops and deletes the containers of every app NivaroOS
# installed (one compose project per folder in APPS_DIR).
remove_apps() {
	local d name ids
	command -v docker >/dev/null 2>&1 || return 0
	for d in "$APPS_DIR"/*/; do
		[ -d "$d" ] || continue
		name="$(basename "$d")"
		ids="$(docker ps -aq --filter "label=com.docker.compose.project=${name}" 2>/dev/null || true)"
		if [ -n "$ids" ]; then
			echo "removing app ${name}"
			# shellcheck disable=SC2086
			docker rm -f -v $ids >/dev/null
		fi
	done
}

# remove_vms removes the libvirt VMs whose disks live in DATA_DIR/VMs.
remove_vms() {
	local vm
	command -v virsh >/dev/null 2>&1 || return 0
	while IFS= read -r vm; do
		[ -n "$vm" ] || continue
		virsh -c qemu:///system dumpxml "$vm" 2>/dev/null | grep -q "${DATA_DIR}/VMs/" || continue
		echo "removing VM ${vm}"
		virsh -c qemu:///system destroy "$vm" >/dev/null 2>&1 || true
		virsh -c qemu:///system undefine --nvram "$vm" >/dev/null 2>&1 || virsh -c qemu:///system undefine "$vm" >/dev/null
	done < <(virsh -c qemu:///system list --all --name 2>/dev/null)
}

stop_services() {
	local u
	for u in $ALL_UNITS $(systemctl list-unit-files --no-legend 'nivaroos*' 'usb-mount@*' 2>/dev/null | awk '{print $1}'); do
		systemctl disable --now "$u" >/dev/null 2>&1 || true
	done
	systemctl stop 'usb-mount@*.service' >/dev/null 2>&1 || true
	pkill -f 'websockify.*28642.*5900' >/dev/null 2>&1 || true
}

# is_ours PATH - a manifest entry is only ever deleted inside NivaroOS's own
# system locations (never /DATA or anything a stray line could point at).
is_ours() {
	case "$1" in
		*..*) return 1 ;;
		/usr/bin/nivaroos* | /usr/bin/casaos* | /usr/local/bin/nivaroos* | /usr/local/lib/nivaroos | /usr/local/lib/nivaroos/*) return 0 ;;
		/usr/lib/systemd/system/* | /etc/systemd/system/nivaroos*) return 0 ;;
		/usr/share/nivaroos/* | /opt/nivaroos | /opt/nivaroos/* | /etc/nivaroos/* | /var/lib/nivaroos/* | /var/log/nivaroos/*) return 0 ;;
		/etc/sysctl.d/*nivaroos* | /etc/udev/rules.d/11-usb-mount.rules | /etc/avahi/services/nivaroos.service | /etc/apparmor.d/nivaroos-* | /etc/modules-load.d/nivaroos-*) return 0 ;;
		/etc/systemd/system/docker.service.d/override.conf) return 0 ;;
	esac
	return 1
}

remove_units() {
	local f
	if [ -f "$MANIFEST_FILE" ]; then
		while IFS= read -r f; do
			case "$f" in *.service | *.socket | *.timer) is_ours "$f" && rm -f "$f" ;; esac
		done <"$MANIFEST_FILE"
	fi
	rm -f /usr/lib/systemd/system/nivaroos*.service /usr/lib/systemd/system/nivaroos*.socket \
		/usr/lib/systemd/system/nivaroos*.timer /usr/lib/systemd/system/nivaroos*.buildroot \
		/usr/lib/systemd/system/rclone.service /usr/lib/systemd/system/rclone.service.prev \
		/usr/lib/systemd/system/usb-mount@.service \
		/etc/udev/rules.d/11-usb-mount.rules /etc/sysctl.d/99-nivaroos.conf \
		/etc/modules-load.d/nivaroos-fans.conf /etc/avahi/services/nivaroos.service \
		/etc/systemd/system/docker.service.d/override.conf
	rm -rf /etc/systemd/system/nivaroos*
	systemctl daemon-reload
	udevadm control --reload-rules >/dev/null 2>&1 || true
	systemctl reload avahi-daemon >/dev/null 2>&1 || true
}

# The Download Station browser and torrent engine: their packages only if
# NivaroOS installed them (a browser the box already had stays).
remove_ds_extras() {
	local rec=/usr/share/nivaroos/ds-browser/installed.txt line
	pkg_remove() {
		if command -v apt-get >/dev/null 2>&1; then DEBIAN_FRONTEND=noninteractive apt-get remove -y "$1" >/dev/null 2>&1 || true
		elif command -v dnf >/dev/null 2>&1; then dnf remove -y "$1" >/dev/null 2>&1 || true
		elif command -v pacman >/dev/null 2>&1; then pacman -R --noconfirm "$1" >/dev/null 2>&1 || true
		elif command -v zypper >/dev/null 2>&1; then zypper -n rm "$1" >/dev/null 2>&1 || true
		elif command -v apk >/dev/null 2>&1; then apk del "$1" >/dev/null 2>&1 || true
		fi
	}
	if [ -f "$rec" ]; then
		while IFS= read -r line; do
			case "$line" in
				package:*) pkg_remove "${line#package:}" ;;
				file:*) is_ours "${line#file:}" && rm -f "${line#file:}" ;;
				user:*) userdel "${line#user:}" >/dev/null 2>&1 || true ;;
			esac
		done <"$rec"
	fi
	if grep -qx 'package:qbittorrent-nox' /usr/share/nivaroos/torrent/installed.txt 2>/dev/null; then
		pkg_remove qbittorrent-nox
	fi
	if [ -f /etc/apparmor.d/nivaroos-ds-browser ]; then
		apparmor_parser -R /etc/apparmor.d/nivaroos-ds-browser >/dev/null 2>&1 || true
	fi
	rm -f /etc/apparmor.d/nivaroos-ds-browser /etc/sysctl.d/60-nivaroos-ds-browser.conf
	return 0
}

# Host Desktop: GDM's WaylandEnable=false back to what it was, the display
# drop-ins it wrote, and the Wayland portal's remembered screen-share grant.
revert_host_desktop() {
	rm -f /usr/local/bin/nivaroos-host-desktop.sh /usr/local/bin/nivaroos-host-desktop-de-install.sh \
		/etc/lightdm/lightdm.conf.d/60-nivaroos-host-desktop.conf /etc/sddm.conf.d/60-nivaroos-host-desktop.conf \
		/etc/X11/xorg.conf.d/10-nivaroos-headless.conf /run/nivaroos/host-desktop-wayland.status \
		/root/.local/state/nivaroos/host-desktop-restore-token /home/*/.local/state/nivaroos/host-desktop-restore-token
	[ -n "$GDM_WAYLAND_INFO" ] || return 0
	local conf orig
	conf="$(printf '%s\n' "$GDM_WAYLAND_INFO" | sed -n 1p)"
	orig="$(printf '%s\n' "$GDM_WAYLAND_INFO" | sed -n 2p)"
	[ -f "$conf" ] || return 0
	if [ "$orig" = "absent" ] || [ -z "$orig" ]; then
		sed -i '/^WaylandEnable=false$/d' "$conf"
	else
		sed -i "s|^WaylandEnable=false\$|${orig}|" "$conf"
	fi
	echo "restored GDM's Wayland setting in ${conf} (from the next login screen)"
}

remove_files() {
	local f
	if [ -f "$MANIFEST_FILE" ]; then
		while IFS= read -r f; do
			is_ours "$f" && rm -rf "$f"
		done <"$MANIFEST_FILE"
	fi
	rm -f /usr/bin/nivaroos /usr/bin/nivaroos.* /usr/bin/nivaroos-* /usr/bin/casaos-cli \
		/usr/local/bin/nivaroos /usr/local/bin/nivaroos-* /usr/local/bin/nivaroos-gpu-driver-install.sh
	rm -rf /usr/local/lib/nivaroos /usr/share/nivaroos /etc/nivaroos /var/log/nivaroos /run/nivaroos /var/run/nivaroos /opt/nivaroos /var/lib/casaos
	if [ "$DEL_APPS" = yes ] || [ ! -d "$APPS_DIR" ]; then
		rm -rf /var/lib/nivaroos
	else
		# Kept apps keep their compose files, so a reinstall picks them up.
		find /var/lib/nivaroos -mindepth 1 -maxdepth 1 ! -name apps -exec rm -rf {} +
	fi
}

provisioned_desktop_de() {
	case "$PROVISIONED_DESKTOP_INFO" in
		alongside:* | replaced:*) ;;
		*) return 1 ;;
	esac
	case "${PROVISIONED_DESKTOP_INFO#*:}" in
		xfce | cinnamon | mate) echo "${PROVISIONED_DESKTOP_INFO#*:}" ;;
		*) return 1 ;;
	esac
}

remove_provisioned_desktop() {
	local de="$1"
	if command -v apt-get >/dev/null 2>&1; then
		case "$de" in
			xfce) apt-get purge -y xfce4 xfce4-terminal ;;
			cinnamon) apt-get purge -y cinnamon-core cinnamon ;;
			mate) apt-get purge -y mate-desktop-environment-core mate-desktop-environment ;;
		esac
		apt-get autoremove -y
	elif command -v pacman >/dev/null 2>&1; then
		case "$de" in
			xfce) pacman -Rns --noconfirm xfce4 xfce4-goodies || pacman -Rns --noconfirm xfce4 ;;
			cinnamon) pacman -Rns --noconfirm cinnamon ;;
			mate) pacman -Rns --noconfirm mate mate-extra || pacman -Rns --noconfirm mate ;;
		esac
	elif command -v dnf >/dev/null 2>&1; then
		dnf remove -y "@${de}-desktop-environment" || dnf group remove -y "${de}-desktop"
	elif command -v zypper >/dev/null 2>&1; then
		case "$de" in
			xfce) zypper --non-interactive remove --clean-deps xfce4-session ;;
			cinnamon) zypper --non-interactive remove --clean-deps cinnamon ;;
			mate) zypper --non-interactive remove --clean-deps mate-session-manager ;;
		esac
	fi
}

delete_data() {
	local p
	if [ "$DEL_APPS" = yes ]; then safe_rm "${DATA_DIR}/AppData"; fi
	if [ "$DEL_VMS" = yes ]; then safe_rm "${DATA_DIR}/VMs"; fi
	if [ "$DEL_FILES" = yes ] && [ -d "$DATA_DIR" ]; then
		for p in "$DATA_DIR"/* "$DATA_DIR"/.[!.]*; do
			case "$p" in "${DATA_DIR}/AppData" | "${DATA_DIR}/VMs") continue ;; esac
			safe_rm "$p"
		done
		rmdir "$DATA_DIR" 2>/dev/null || true
	fi
	return 0
}

verify_teardown() {
	local u b
	: >"$LEFT_FILE"
	for u in $ALL_UNITS; do
		systemctl is-active --quiet "$u" 2>/dev/null && echo "still running: $u" >>"$LEFT_FILE"
		[ -n "$(systemctl list-unit-files --no-legend "$u" 2>/dev/null)" ] && echo "unit still installed: $u" >>"$LEFT_FILE"
	done
	for b in /usr/bin/nivaroos* /usr/local/bin/nivaroos* /etc/nivaroos /opt/nivaroos; do
		[ -e "$b" ] && echo "still present: $b" >>"$LEFT_FILE"
	done
	return 0
}

print_summary() {
	local p
	printf '\n'
	if [ -n "$LEFTOVER_ITEMS" ] || [ "${#FAILED_STEPS[@]}" -gt 0 ]; then
		ui_warn "${UI_B}NivaroOS is uninstalled, with leftovers${UI_R}"
		while IFS= read -r p; do [ -n "$p" ] && ui_kv "left" "$p"; done <<<"$LEFTOVER_ITEMS"
		for p in "${FAILED_STEPS[@]}"; do ui_kv "failed" "$p"; done
		ui_kv "log" "$LOG_FILE"
		ui_kv "retry" "running the uninstaller again is safe"
	else
		ui_ok "${UI_B}NivaroOS is uninstalled${UI_R}"
	fi
	ui_head "Data"
	if [ "$DEL_APPS" = yes ]; then ui_kv "apps" "deleted"; else ui_kv "apps" "kept (containers, ${DATA_DIR}/AppData, ${APPS_DIR})"; fi
	if [ "$DEL_VMS" = yes ]; then ui_kv "VMs" "deleted"; else ui_kv "VMs" "kept (${DATA_DIR}/VMs)"; fi
	if [ "$DEL_FILES" = yes ]; then ui_kv "files" "deleted"; else ui_kv "files" "kept (${DATA_DIR})"; fi
	for p in "${SKIPPED_PATHS[@]}"; do ui_kv "on a drive" "$p (not deleted)"; done
	printf '    %sDocker, Go and other system packages stay installed.%s\n\n' "$UI_D" "$UI_R"
	[ -z "$LEFTOVER_ITEMS" ] && [ "${#FAILED_STEPS[@]}" -eq 0 ]
}

main() {
	parse_args "$@"
	check_root "$@"
	ui_banner "uninstaller" "Removes NivaroOS; asks before deleting data"
	if [ ! -e /usr/bin/nivaroos ] && [ ! -e /etc/nivaroos ] && [ ! -e /var/lib/nivaroos ]; then
		ui_info "NivaroOS does not look installed here - removing any leftovers."
	fi
	choose_data
	confirm
	if [ -n "$DRY_RUN" ]; then
		ui_info "Dry run - nothing was changed."
		exit 0
	fi

	: >>"$LOG_FILE" 2>/dev/null || LOG_FILE=/dev/null
	SKIP_FILE="$(mktemp)"
	LEFT_FILE="$(mktemp)"
	[ -f "$DESKTOP_PROVISION_MARKER" ] && PROVISIONED_DESKTOP_INFO="$(cat "$DESKTOP_PROVISION_MARKER")"
	[ -f "$GDM_WAYLAND_MARKER" ] && GDM_WAYLAND_INFO="$(cat "$GDM_WAYLAND_MARKER")"
	local de=""
	de="$(provisioned_desktop_de || true)"

	local steps=()
	[ -x /usr/bin/nivaroos-backup ] && steps+=("Handing Scheduled Tasks back from Backup & Sync:release_backup_tasks")
	[ "$DEL_APPS" = yes ] && steps+=("Removing installed apps:remove_apps")
	[ "$DEL_VMS" = yes ] && steps+=("Removing virtual machines:remove_vms")
	steps+=(
		"Stopping services:stop_services"
		"Removing units and drop-ins:remove_units"
		"Reverting Host Desktop changes:revert_host_desktop"
		"Removing Download Station's browser and torrent engine:remove_ds_extras"
		"Removing binaries, dashboard, config and state:remove_files"
	)
	[ "$DEL_APPS$DEL_VMS$DEL_FILES" != nonono ] && steps+=("Deleting the data you chose:delete_data")
	[ -n "$de" ] && [ "$REMOVE_PROVISIONED_DESKTOP" = yes ] && steps+=("Removing the ${de} desktop NivaroOS installed:remove_provisioned_desktop $de")
	steps+=("Checking nothing is left:verify_teardown")
	TOTAL_STEPS=${#steps[@]}

	ui_head "Uninstalling"
	local s
	for s in "${steps[@]}"; do
		run_step "${s%%:*}" "${s#*:}"
	done

	mapfile -t SKIPPED_PATHS <"$SKIP_FILE"
	LEFTOVER_ITEMS="$(cat "$LEFT_FILE")"
	rm -f "$SKIP_FILE" "$LEFT_FILE"
	if [ -n "$de" ] && [ "$REMOVE_PROVISIONED_DESKTOP" != yes ]; then
		ui_info "The ${de} desktop NivaroOS installed for Host Desktop stays. Remove it with --remove-provisioned-desktop."
	fi
	print_summary || exit 1
}

main "$@"
