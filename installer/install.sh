#!/usr/bin/env bash
# ==============================================================================
#  NivaroOS installer and updater
#  https://github.com/F-e-n-y-x/NivaroOS
#
#  Install or update with one command:
#    curl -fsSL https://raw.githubusercontent.com/F-e-n-y-x/NivaroOS/master/installer/install.sh | sudo bash
#  With flags:   ... | sudo bash -s -- -y --without-vm
#  Options:      ... | sudo bash -s -- --help
#
#  Supported: Debian 12/13 and Ubuntu 22.04/24.04/26.04 on amd64 or arm64.
#  Other Debian/Ubuntu derivatives, Fedora/RHEL, Arch and openSUSE are
#  best effort. systemd is required.
#
#  Safe to re-run: every step is idempotent, so a failed or interrupted run
#  is resumed by running the same command again, and an update is the same
#  command too. Everything is defined first and only `main` at the very end
#  runs, so a truncated download never runs half a script.
# ==============================================================================

if [ -z "${BASH_VERSION:-}" ]; then
	exec bash "$0" "$@"
fi

# -E (errtrace): without it the ERR trap is not inherited by functions, and
# nearly everything here runs in one, so a failure would exit silently.
set -Eeuo pipefail

# ------------------------------------------------------------------------------
# Configuration
# ------------------------------------------------------------------------------
REPO_URL="https://github.com/F-e-n-y-x/NivaroOS.git"
BRANCH="master"
SRC_DIR="/opt/nivaroos/src"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" 2>/dev/null && pwd || echo "")"
LOCAL_REPO=""
if [ -n "$SCRIPT_DIR" ] && [ -f "${SCRIPT_DIR}/../services/core/main.go" ]; then
	LOCAL_REPO="$(cd "${SCRIPT_DIR}/.." && pwd)"
fi
OS_RELEASE_FILE="${OS_RELEASE_FILE:-/etc/os-release}"
# Go toolchain installed to /usr/local/go when the one on PATH is missing or
# older than the highest `go` directive in services/*/go.mod + cli/go.mod
# (see clone_or_update_repo; GO_MIN_VERSION is the floor if that can't be
# read). Bump GO_VERSION whenever a go.mod needs a newer release.
GO_VERSION="1.27.1"
GO_MIN_VERSION="1.26.0"
MIN_RECOMMENDED_MEMORY_MB="1024"
MIN_REQUIRED_MEMORY_MB="384"
# Building from source needs room for the Go toolchain and module cache,
# node_modules and Docker.
MIN_RECOMMENDED_DISK_GB="10"
MIN_REQUIRED_DISK_GB="3"

CUSTOM_PORT=""
DETECTED_PORT="80"
IS_UPGRADE="false"
WITH_VM=""
WITH_HOST_DESKTOP=""
WITH_DOWNLOAD_STATION=""
WITH_DS_BROWSER=yes
WITH_DS_TORRENT=yes
DS_TORRENT_BUILD=static
WITH_BACKUP=""
FORCE=""
YES=""
DEBUG=""
DRY_RUN=""
CLI_WIDTH=""
STEP_NUM=0
TOTAL_STEPS=0
CURRENT_STEP_TITLE=""
CURRENT_STEP_PID=""
START_TIME=0
DATE_TAG="$(date +'%Y%m%d-%H%M%S')"
INSTALL_CMD="curl -fsSL https://raw.githubusercontent.com/F-e-n-y-x/NivaroOS/master/installer/install.sh | sudo bash"

LOG_DIR="/var/log/nivaroos"
INSTALL_LOG="${LOG_DIR}/install-${DATE_TAG}.log"
LATEST_LOG="${LOG_DIR}/install.log"
MANIFEST_FILE="/var/lib/nivaroos/manifest"
LOCK_FILE="/run/lock/nivaroos-install.lock"

# VM Manager's hardware acceleration, decided once up front. (Whether there
# is an X11 desktop for Host Desktop to stream is checked later, from the
# dashboard - see select_components.)
KVM_AVAILABLE="no"

# Checkbox-menu widget state (see checkbox_menu()).
CBM_LABELS=()
CBM_DESCS=()
CBM_STATE=()

export PATH="/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:$PATH"
# Builds must use the toolchain this script installed - never silently
# download a different one mid-build.
export GOTOOLCHAIN=local
export GOWORK=off
export DEBIAN_FRONTEND=noninteractive
export NEEDRESTART_MODE=a

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

# Prompts read /dev/tty, not stdin: with `curl ... | sudo bash` stdin is the
# script itself, yet the user is at a real terminal and can answer.
INTERACTIVE_TTY="false"
if [ -t 1 ] && { : </dev/tty; } 2>/dev/null; then
	INTERACTIVE_TTY="true"
fi
# Unattended (-y, or nobody at a terminal): take every default, never ask.
unattended() { [ -n "$YES" ] || [ "$INTERACTIVE_TTY" != "true" ]; }

# Old helper names, still used inside the steps.
info() { ui_info "$1"; }
success() { ui_ok "$1"; }
warn() { ui_warn "$1"; }
error() { ui_err "$1"; }

init_logging() {
	mkdir -p "$LOG_DIR" 2>/dev/null || true
	touch "$INSTALL_LOG" 2>/dev/null || true
	ln -sf "$INSTALL_LOG" "$LATEST_LOG" 2>/dev/null || true
}

log_raw() {
	if [ -w "$INSTALL_LOG" ]; then
		printf '[%s] %s\n' "$(date +'%Y-%m-%d %H:%M:%S')" "$*" >>"$INSTALL_LOG"
	fi
}

# One installer at a time: two runs building into /usr/bin at once would
# leave a mix of both.
take_lock() {
	mkdir -p "$(dirname "$LOCK_FILE")" 2>/dev/null || true
	exec 9>"$LOCK_FILE"
	if command -v flock >/dev/null 2>&1 && ! flock -n 9; then
		ui_die "Another NivaroOS install or update is already running (lock: ${LOCK_FILE})."
	fi
}

# Kills a process and every descendant: a step is a subshell whose children
# (pipelines, builds) a signal to the subshell alone would leave running.
kill_tree() {
	local pid="$1" sig="${2:-TERM}" child children
	children="$(pgrep -P "$pid" 2>/dev/null || true)"
	for child in $children; do
		kill_tree "$child" "$sig"
	done
	kill -"$sig" "$pid" 2>/dev/null || true
}

cleanup_on_exit() {
	if [ -t 1 ]; then printf '\033[?25h'; fi
}
trap cleanup_on_exit EXIT

# Ctrl+C / SIGTERM: stop the step in flight and say where it stopped. (A
# trap that doesn't exit would leave the spinner running forever.)
handle_interrupt() {
	if [ -n "$CURRENT_STEP_PID" ] && kill -0 "$CURRENT_STEP_PID" 2>/dev/null; then
		kill_tree "$CURRENT_STEP_PID" TERM
		sleep 0.3
		kill_tree "$CURRENT_STEP_PID" KILL
	fi
	if [ -t 1 ]; then printf '\r\033[K\033[?25h'; fi
	printf '\n'
	if [ -n "$CURRENT_STEP_TITLE" ]; then
		ui_warn "Cancelled ($1) during step ${STEP_NUM}/${TOTAL_STEPS}: ${CURRENT_STEP_TITLE}"
	else
		ui_warn "Cancelled ($1)."
	fi
	ui_kv "log" "$INSTALL_LOG"
	ui_kv "resume" "run the same command again - finished steps are safe to repeat"
	exit 130
}
trap 'handle_interrupt INT' INT
trap 'handle_interrupt TERM' TERM

on_fatal_error() {
	local exit_code=$? line_no="$1"
	if [ "$exit_code" -ne 0 ]; then
		log_raw "Fatal error at line ${line_no} (exit code ${exit_code})"
		ui_err "The installer stopped unexpectedly at line ${line_no} (exit code ${exit_code})."
		ui_kv "log" "$INSTALL_LOG"
	fi
	exit "$exit_code"
}
trap 'on_fatal_error "$LINENO"' ERR

strip_ansi() {
	printf '%s' "$1" | sed -E 's/\x1b\[[0-9;?]*[a-zA-Z]//g' | tr '\r\t' '  '
}

term_cols() {
	local c=""
	if [ -n "$CLI_WIDTH" ] && [ "$CLI_WIDTH" -ge 40 ] 2>/dev/null; then
		echo "$CLI_WIDTH"
		return
	fi
	c="$({ stty size </dev/tty; } 2>/dev/null | awk '{print $2}')" || true
	[ -n "$c" ] && [ "$c" -ge 20 ] 2>/dev/null || c="${COLUMNS:-80}"
	echo "$c"
}

# ------------------------------------------------------------------------------
# Preflight
# ------------------------------------------------------------------------------
check_root() {
	[ "$(id -u)" -eq 0 ] && return 0
	[ -n "$DRY_RUN" ] && return 0
	if [ ! -f "$0" ]; then
		ui_err "The installer needs root. Run it with sudo:"
		printf '\n    %s\n\n' "$INSTALL_CMD"
		exit 1
	fi
	if command -v sudo >/dev/null 2>&1; then
		ui_info "Root privileges required - asking sudo."
		exec sudo -E bash "$0" "$@"
	fi
	ui_die "The installer must run as root (log in as root or install sudo)."
}

mem_mb() {
	awk '/MemTotal:/ { print int($2/1024) }' /proc/meminfo 2>/dev/null || echo 0
}

disk_free_gb() {
	LC_ALL=C df -Pk / 2>/dev/null | awk 'NR==2 { print int($4/1024/1024) }'
}

# is_port_in_use PORT - `ss` when present, else a plain TCP connect (bash's
# /dev/tcp), which works on a minimal system without iproute2.
is_port_in_use() {
	local port="$1"
	if command -v ss >/dev/null 2>&1; then
		ss -tln 2>/dev/null | grep -qE "[:.]${port}[[:space:]]" && return 0
		return 1
	fi
	(exec 3<>"/dev/tcp/127.0.0.1/${port}") 2>/dev/null
}

find_process_on_port() {
	local port="$1" proc=""
	if command -v ss >/dev/null 2>&1; then
		proc="$(ss -tlnp 2>/dev/null | grep -E "[:.]${port}[[:space:]]" | grep -o '(("[^"]*"' | head -1 | tr -d '("')" || true
	fi
	echo "${proc:-another program}"
}

# net_ok HOST - HTTPS to HOST works (curl, or a bare TCP connect without it).
net_ok() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsS -m 15 -o /dev/null "https://$1" 2>/dev/null
	else
		timeout 15 bash -c "exec 3<>/dev/tcp/$1/443" 2>/dev/null
	fi
}

casaos_installed() {
	local d
	for d in /etc/systemd/system /usr/lib/systemd/system /lib/systemd/system; do
		[ -f "$d/casaos.service" ] && return 0
	done
	return 1
}

# preflight checks everything up front and lists the result, so a box that
# can't take NivaroOS is told why before anything is changed. Problems that
# make the install fail are collected and reported together.
preflight() {
	local fatal=() os_name="Linux" id="" ver="" like="" arch mem disk

	ui_head "Preflight"

	if [ -f "$OS_RELEASE_FILE" ]; then
		# shellcheck disable=SC1090
		. "$OS_RELEASE_FILE"
		os_name="${PRETTY_NAME:-${ID:-Linux}}"
		id="${ID:-}"
		ver="${VERSION_ID:-}"
		like="${ID_LIKE:-}"
	fi
	arch="$(uname -m)"
	case "$id:$ver" in
		debian:12* | debian:13* | ubuntu:22.04 | ubuntu:24.04 | ubuntu:26.04)
			ui_ok "System       ${os_name}, ${arch}" ;;
		debian:* | ubuntu:*)
			ui_warn "System       ${os_name} is not a tested release (Debian 12/13, Ubuntu 22.04/24.04/26.04) - continuing" ;;
		*)
			case " $id $like " in
				*" debian "* | *" ubuntu "*) ui_warn "System       ${os_name} (Debian-based, not tested) - continuing" ;;
				*" rhel "* | *" fedora "* | *" centos "* | *" arch "* | *" opensuse "* | *" suse "*)
					ui_warn "System       ${os_name} is best effort - continuing" ;;
				*) ui_warn "System       ${os_name} has not been validated - continuing" ;;
			esac
			;;
	esac

	case "$arch" in
		x86_64 | aarch64 | arm64) ;;
		armv7l | armhf) ui_warn "Architecture ${arch} is best effort (amd64 and arm64 are supported)" ;;
		*) fatal+=("Architecture ${arch} is not supported (amd64 or arm64)") ;;
	esac

	if [ -d /run/systemd/system ]; then
		ui_ok "Init         systemd"
	else
		fatal+=("systemd is not running - NivaroOS runs as systemd services")
	fi

	mem="$(mem_mb)"
	if [ "${mem:-0}" -gt 0 ]; then
		if [ "$mem" -lt "$MIN_REQUIRED_MEMORY_MB" ]; then
			fatal+=("Only ${mem} MB of memory - at least ${MIN_REQUIRED_MEMORY_MB} MB is needed")
		elif [ "$mem" -lt "$MIN_RECOMMENDED_MEMORY_MB" ]; then
			ui_warn "Memory       ${mem} MB (${MIN_RECOMMENDED_MEMORY_MB} MB or more recommended)"
		else
			ui_ok "Memory       $(awk "BEGIN { printf \"%.1f GB\", ${mem}/1024 }")"
		fi
	fi

	disk="$(disk_free_gb)"
	if [ -n "$disk" ]; then
		if [ "$disk" -lt "$MIN_REQUIRED_DISK_GB" ]; then
			fatal+=("Only ${disk} GB free on / - at least ${MIN_REQUIRED_DISK_GB} GB is needed to build NivaroOS")
		elif [ "$disk" -lt "$MIN_RECOMMENDED_DISK_GB" ]; then
			ui_warn "Disk         ${disk} GB free on / (${MIN_RECOMMENDED_DISK_GB} GB or more recommended)"
		else
			ui_ok "Disk         ${disk} GB free on /"
		fi
	fi

	if net_ok github.com; then
		ui_ok "Network      github.com reachable"
	else
		fatal+=("github.com is not reachable - check this machine's internet connection and DNS")
	fi

	if casaos_installed; then
		fatal+=("CasaOS is installed. NivaroOS replaces it and uses the same port and paths - remove it first (casaos-uninstall), then run this again")
	fi

	resolve_port_conflict
	if [ "$IS_UPGRADE" = "true" ]; then
		ui_ok "Existing     NivaroOS found - updating in place, data and settings are kept"
	fi

	if [ -e /dev/kvm ]; then
		ui_ok "KVM          available"
	else
		ui_info "KVM          not available - virtual machines would use slow emulation"
	fi
	if command -v docker >/dev/null 2>&1; then
		ui_ok "Docker       $(docker version --format '{{.Server.Version}}' 2>/dev/null || echo installed)"
	else
		ui_info "Docker       not installed yet - the installer adds it"
	fi

	if [ "${#fatal[@]}" -gt 0 ]; then
		printf '\n'
		local f
		for f in "${fatal[@]}"; do ui_err "$f"; done
		printf '\n'
		ui_die "Nothing was changed."
	fi
}

# Picks the dashboard port. An existing install keeps its port; a port held
# by another program moves to the next free one (or asks).
resolve_port_conflict() {
	# Checked regardless of whether the requested port is free: re-running
	# with another --port than a prior install used is still an upgrade.
	if [ -f /etc/nivaroos/gateway.ini ] || [ -x /usr/bin/nivaroos-gateway ]; then
		IS_UPGRADE="true"
		local saved_port
		saved_port="$(awk -F '=' '/^[[:space:]]*port[[:space:]]*=/ {gsub(/[[:space:]]/, "", $2); print $2}' /etc/nivaroos/gateway.ini 2>/dev/null || echo "")"
		[ -n "$saved_port" ] && DETECTED_PORT="$saved_port"
	fi

	local target_port="${CUSTOM_PORT:-$DETECTED_PORT}"
	if ! is_port_in_use "$target_port"; then
		DETECTED_PORT="$target_port"
		ui_ok "Port         ${DETECTED_PORT} free for the dashboard"
		return 0
	fi

	local conflict_proc
	conflict_proc="$(find_process_on_port "$target_port")"
	if [[ "$conflict_proc" =~ nivaroos ]] || { [ "$IS_UPGRADE" = "true" ] && [ "$target_port" = "$DETECTED_PORT" ]; }; then
		DETECTED_PORT="$target_port"
		ui_ok "Port         ${DETECTED_PORT} (NivaroOS's own)"
		return 0
	fi

	local alt_port=8080
	while [ "$alt_port" -le 65535 ] && is_port_in_use "$alt_port"; do
		alt_port=$((alt_port + 1))
	done

	if [ -n "$CUSTOM_PORT" ] && unattended; then
		# An explicit --port is respected even if something holds it now.
		ui_warn "Port         ${CUSTOM_PORT} is in use by ${conflict_proc} - using it anyway (--port)"
		DETECTED_PORT="$CUSTOM_PORT"
		return 0
	fi
	if unattended; then
		ui_warn "Port         ${target_port} is in use by ${conflict_proc} - using ${alt_port}"
		DETECTED_PORT="$alt_port"
		return 0
	fi

	ui_warn "Port         ${target_port} is in use by ${conflict_proc}"
	local user_port=""
	printf '  ? Dashboard port [%s]: ' "$alt_port"
	read -r user_port </dev/tty || user_port=""
	case "$user_port" in
		'') DETECTED_PORT="$alt_port" ;;
		*[!0-9]*) ui_die "'${user_port}' is not a port number." 2 ;;
		*) DETECTED_PORT="$user_port" ;;
	esac
	ui_ok "Port         ${DETECTED_PORT}"
}

# ------------------------------------------------------------------------------
# Arguments
# ------------------------------------------------------------------------------
usage() {
	ui_banner "installer" "Install or update - safe to re-run"
	cat <<EOF

  Usage
    ${INSTALL_CMD}
    ... | sudo bash -s -- [options]
    sudo bash installer/install.sh [options]          (from a checkout)

  Options
    -y, --yes                     Unattended: take the defaults, ask nothing
    --dry-run                     Run the checks and show the plan; change nothing
    --with-vm, --without-vm       VM Manager: QEMU/KVM, libvirt, web console
                                  (default: on when KVM is available)
    --with-host-desktop           Stream this machine's own desktop (needs VM Manager)
    --without-host-desktop        (default)
    --with-download-station       Downloads, torrents, browser, ad blocker (default)
    --without-download-station
    --without-ds-browser          Keep Download Station's browser in Lite mode (no Chromium)
    --without-ds-torrent          No qbittorrent-nox (torrents use the built-in engine)
    --ds-torrent-distro           qbittorrent-nox from the distro's packages, not the
                                  official static build (default: static)
    --with-backup                 Backup & Sync (default)
    --without-backup
    --port <port>                 Dashboard port (default: 80, or the next free one)
    --branch <ref>                Branch or tag to install (default: master)
    --repo <url>                  Git repository to install from
    --force                       Update even while a backup is running (it is retried after)
    --debug                       Show every step's full output
    --width <cols>                Fixed output width (default: the terminal's)
    -h, --help                    Show this help

  Logs   ${LOG_DIR}/install.log
  Exit   0 done, 1 failed (the step and its log are shown), 2 bad option, 130 cancelled
EOF
}

parse_args() {
	while [ $# -gt 0 ]; do
		case "$1" in
			--with-vm) WITH_VM=yes ;;
			--without-vm) WITH_VM=no ;;
			--with-host-desktop) WITH_HOST_DESKTOP=yes ;;
			--without-host-desktop) WITH_HOST_DESKTOP=no ;;
			--with-download-station) WITH_DOWNLOAD_STATION=yes ;;
			--without-download-station) WITH_DOWNLOAD_STATION=no ;;
			--without-ds-browser) WITH_DS_BROWSER=no ;;
			--without-ds-torrent) WITH_DS_TORRENT=no ;;
			--ds-torrent-distro) DS_TORRENT_BUILD=distro ;;
			--with-backup) WITH_BACKUP=yes ;;
			--without-backup) WITH_BACKUP=no ;;
			--force) FORCE=yes ;;
			--dry-run | -n) DRY_RUN=yes ;;
			--port=*) CUSTOM_PORT="${1#*=}" ;;
			--port)
				shift
				CUSTOM_PORT="${1:-}"
				;;
			--width=*) CLI_WIDTH="${1#*=}" ;;
			--width | -w)
				shift
				CLI_WIDTH="${1:-}"
				;;
			--branch=*) BRANCH="${1#*=}" ;;
			--branch | -b)
				shift
				BRANCH="${1:-master}"
				;;
			--repo=*) REPO_URL="${1#*=}" ;;
			--repo)
				shift
				REPO_URL="${1:-$REPO_URL}"
				;;
			--yes | -y | --unattended) YES=yes ;;
			--debug) DEBUG=yes ;;
			--help | -h)
				usage
				exit 0
				;;
			*)
				ui_err "Unknown option '$1' - see --help."
				exit 2
				;;
		esac
		# A two-token flag given last has already shifted its value away.
		[ $# -eq 0 ] || shift
	done
	case "$CUSTOM_PORT" in
		'') ;;
		*[!0-9]*) ui_die "--port needs a number, not '${CUSTOM_PORT}'." 2 ;;
	esac
}

# ------------------------------------------------------------------------------
# Interactive checkbox list. Populate CBM_LABELS/CBM_DESCS/CBM_STATE (same
# length, CBM_STATE "0"/"1"), call checkbox_menu "<title>", read CBM_STATE.
# Space toggles, up/down or j/k move, Enter confirms, Ctrl+C cancels. Only
# call it when interactive (see unattended).
# ------------------------------------------------------------------------------
checkbox_menu() {
	local title="$1" n=${#CBM_LABELS[@]} cur=0 key="" rest="" old_stty=""
	old_stty="$(stty -g </dev/tty 2>/dev/null || true)"
	stty raw -echo </dev/tty 2>/dev/null || true
	printf '\033[?25l'

	local first_draw=true drawn_lines=0 i out
	while true; do
		out="\r\n  ${UI_B}${title}${UI_R}\r\n  ${UI_D}up/down move   space toggle   enter confirm${UI_R}\r\n\r\n"
		for ((i = 0; i < n; i++)); do
			local box="[ ]" pointer="  "
			[ "${CBM_STATE[$i]}" = "1" ] && box="[${UI_OK}x${UI_R}]"
			[ "$i" -eq "$cur" ] && pointer="${UI_B}>${UI_R} "
			out+="  ${pointer}${box} ${CBM_LABELS[$i]}\r\n"
			[ -n "${CBM_DESCS[$i]:-}" ] && out+="        ${UI_D}${CBM_DESCS[$i]}${UI_R}\r\n"
		done
		[ "$first_draw" = "false" ] && printf '\033[%dA' "$drawn_lines"
		first_draw=false
		printf '%b' "$out"
		drawn_lines="$(printf '%b' "$out" | wc -l)"

		key=""
		IFS= read -rsn1 key </dev/tty || true
		if [ "$key" = $'\033' ]; then
			rest=""
			IFS= read -rsn2 -t 0.01 rest </dev/tty || true
			key="${key}${rest}"
		fi
		case "$key" in
			$'\033[A' | k | K) cur=$(((cur - 1 + n) % n)) ;;
			$'\033[B' | j | J) cur=$(((cur + 1) % n)) ;;
			' ') if [ "${CBM_STATE[$cur]}" = "1" ]; then CBM_STATE[cur]=0; else CBM_STATE[cur]=1; fi ;;
			$'\003')
				stty "$old_stty" </dev/tty 2>/dev/null || true
				printf '\033[?25h\n'
				ui_warn "Cancelled - nothing was changed."
				exit 130
				;;
			'') break ;;
		esac
	done
	printf '\033[?25h'
	[ -n "$old_stty" ] && stty "$old_stty" </dev/tty 2>/dev/null
	printf '\r\n'
}

# ------------------------------------------------------------------------------
# Components
# ------------------------------------------------------------------------------
compute_default_selections() {
	if [ -e /dev/kvm ]; then KVM_AVAILABLE="yes"; else KVM_AVAILABLE="no"; fi
	if [ -z "$WITH_VM" ]; then
		if [ "$KVM_AVAILABLE" = "yes" ]; then WITH_VM=yes; else WITH_VM=no; fi
	fi
	# `if`, not `[ -z ] && x=...`: as a function's last statement a false
	# test would be its exit status and abort the installer under set -e.
	if [ -z "$WITH_HOST_DESKTOP" ]; then
		WITH_HOST_DESKTOP=no
	fi
	# On by default: single pure-Go services with no system packages.
	if [ -z "$WITH_DOWNLOAD_STATION" ]; then
		WITH_DOWNLOAD_STATION=yes
	fi
	if [ -z "$WITH_BACKUP" ]; then
		WITH_BACKUP=yes
	fi
}

# Samba and mDNS are core (Network Shares and mobile-app discovery are
# always-on features) and always install, like Docker and the dashboard.
# Only VM Manager (QEMU/libvirt), Host Desktop (x11vnc), Download Station
# and Backup & Sync are choices.
#
# Host Desktop here only installs the streaming service. Whether there is
# an X11 desktop to stream is checked when Host Desktop is first opened in
# the dashboard (services/vm-sidecar/hostdesktop/host-desktop-de-install.sh
# --status), which offers to set one up then - not here, where it used to
# block the install on a prompt for a box that may have no one at its
# console yet.
select_components() {
	compute_default_selections

	if ! unattended; then
		local vm_desc
		if [ "$KVM_AVAILABLE" = "yes" ]; then
			vm_desc="KVM hardware acceleration detected on this CPU."
		else
			vm_desc="No KVM acceleration - VMs would use slower software emulation."
		fi
		CBM_LABELS=(
			"VM Manager          QEMU/KVM, libvirt, web console, VirtIO-FS"
			"Host Desktop        stream this machine's own desktop"
			"Download Station    multi-connection downloads, torrents, browser, ad blocker"
			"Backup & Sync       scheduled backups and sync to drives, shares and clouds"
		)
		CBM_DESCS=(
			"$vm_desc"
			"Needs VM Manager (shares its vm-sidecar)."
			"qbittorrent-nox runs only while a torrent is active; uBlock Origin filter lists."
			"Plug-in backups, mirrors and archives. No extra packages."
		)
		CBM_STATE=(
			"$([ "$WITH_VM" = "yes" ] && echo 1 || echo 0)"
			"$([ "$WITH_HOST_DESKTOP" = "yes" ] && echo 1 || echo 0)"
			"$([ "$WITH_DOWNLOAD_STATION" = "yes" ] && echo 1 || echo 0)"
			"$([ "$WITH_BACKUP" = "yes" ] && echo 1 || echo 0)"
		)
		printf '\n  %sDashboard, gateway, App Store, files, Samba shares and mDNS always install.%s' "$UI_D" "$UI_R"
		checkbox_menu "Optional components"
		WITH_VM="$([ "${CBM_STATE[0]}" = "1" ] && echo yes || echo no)"
		WITH_HOST_DESKTOP="$([ "${CBM_STATE[1]}" = "1" ] && echo yes || echo no)"
		WITH_DOWNLOAD_STATION="$([ "${CBM_STATE[2]}" = "1" ] && echo yes || echo no)"
		WITH_BACKUP="$([ "${CBM_STATE[3]}" = "1" ] && echo yes || echo no)"
	fi

	if [ "$WITH_HOST_DESKTOP" = "yes" ] && [ "$WITH_VM" != "yes" ]; then
		ui_info "Host Desktop needs VM Manager - turning VM Manager on too."
		WITH_VM=yes
	fi

	ui_head "Components"
	local name val
	for name in "VM Manager:$WITH_VM" "Host Desktop:$WITH_HOST_DESKTOP" "Download Station:$WITH_DOWNLOAD_STATION" "Backup & Sync:$WITH_BACKUP"; do
		val="${name##*:}"
		if [ "$val" = "yes" ]; then ui_ok "${name%:*}"; else ui_info "${UI_D}${name%:*} (off)${UI_R}"; fi
	done
}

# ------------------------------------------------------------------------------
# Step runner. Each step runs in a subshell with its output going to its own
# log (appended to the install log afterwards). On a terminal one line shows
# the step, a timer and the step's latest output line; --debug streams the
# whole output; without a terminal (CI, a pipe to a file) a start and a
# result line are printed. A failed step shows its last log lines and stops.
# ------------------------------------------------------------------------------
print_failure() {
	local title="$1" exit_code="$2" log_file="$3"
	printf '\n'
	ui_err "Step ${STEP_NUM}/${TOTAL_STEPS} failed: ${title} (exit code ${exit_code})"
	printf '\n    %sLast lines of its output%s\n' "$UI_D" "$UI_R"
	if [ -s "$log_file" ]; then
		tail -n 20 "$log_file" | while IFS= read -r line; do
			printf '    %s|%s %s\n' "$UI_D" "$UI_R" "$(strip_ansi "$line")"
		done
	else
		printf '    %s|%s (no output)\n' "$UI_D" "$UI_R"
	fi
	printf '\n'
	ui_kv "full log" "$INSTALL_LOG"
	ui_kv "retry" "run the same command again - it picks up from here"
	ui_kv "report" "https://github.com/F-e-n-y-x/NivaroOS/issues"
	printf '\n'
}

run_step() {
	local title="$1"
	shift
	STEP_NUM=$((STEP_NUM + 1))
	CURRENT_STEP_TITLE="$title"
	local tag start_ts log_file exit_code elapsed
	tag="$(printf '%*d/%d' "${#TOTAL_STEPS}" "$STEP_NUM" "$TOTAL_STEPS")"
	start_ts=$(date +%s)
	log_raw ">>> START STEP ${STEP_NUM}/${TOTAL_STEPS}: ${title}"
	log_file="$(mktemp /tmp/nivaroos-install-step-XXXXXX.log)"

	# The installer's own traps would report a failing command inside the
	# step as if the installer itself had died; the step reports through
	# its exit status instead.
	(
		trap - ERR INT TERM EXIT
		eval "$*"
	) >"$log_file" 2>&1 </dev/null &
	CURRENT_STEP_PID=$!

	if [ "$DEBUG" = "yes" ]; then
		printf '  %s%s%s  %s\n' "$UI_D" "$tag" "$UI_R" "$title"
		tail -n +1 -f --pid="$CURRENT_STEP_PID" "$log_file" 2>/dev/null | sed 's/^/      /' || true
	elif [ -t 1 ]; then
		local frames i=0 cols last line room
		if [ "$UI_I_OK" = "ok" ]; then frames='-\|/'; else frames='⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏'; fi
		printf '\033[?25l'
		while kill -0 "$CURRENT_STEP_PID" 2>/dev/null; do
			elapsed=$(($(date +%s) - start_ts))
			cols="$(term_cols)"
			line="  ${frames:i%${#frames}:1} ${tag}  ${title}  ${elapsed}s"
			room=$((cols - ${#line} - 3))
			last=""
			if [ "$room" -gt 8 ]; then
				last="$(tail -n 1 "$log_file" 2>/dev/null || true)"
				last="$(strip_ansi "$last")"
				[ "${#last}" -gt "$room" ] && last="${last:0:$((room - 1))}…"
			fi
			if [ "${#line}" -ge "$cols" ]; then line="${line:0:$((cols - 1))}"; fi
			printf '\r\033[K%s  %s%s%s' "$line" "$UI_D" "$last" "$UI_R"
			i=$((i + 1))
			sleep 0.1
		done
		printf '\r\033[K\033[?25h'
	else
		printf '  - %s  %s ...\n' "$tag" "$title"
	fi

	# `wait` returns the step's status; under set -e plus the ERR trap
	# (errtrace fires regardless of errexit) a failure would abort right
	# here, before it can be reported. Suspend both for this one call.
	trap '' ERR
	set +e
	wait "$CURRENT_STEP_PID"
	exit_code=$?
	set -e
	trap 'on_fatal_error "$LINENO"' ERR
	CURRENT_STEP_PID=""
	elapsed=$(($(date +%s) - start_ts))
	cat "$log_file" >>"$INSTALL_LOG" 2>/dev/null || true

	if [ "$exit_code" -eq 0 ]; then
		log_raw "<<< COMPLETED STEP ${STEP_NUM}: ${title} [${elapsed}s]"
		printf '  %s%s%s %s  %s  %s%ss%s\n' "$UI_OK" "$UI_I_OK" "$UI_R" "$tag" "$title" "$UI_D" "$elapsed" "$UI_R"
		rm -f "$log_file"
		CURRENT_STEP_TITLE=""
		return 0
	fi
	log_raw "<<< FAILED STEP ${STEP_NUM}: ${title} [${elapsed}s, exit code ${exit_code}]"
	printf '  %s%s%s %s  %s  %s%ss%s\n' "$UI_ERR" "$UI_I_ERR" "$UI_R" "$tag" "$title" "$UI_D" "$elapsed" "$UI_R"
	print_failure "$title" "$exit_code" "$log_file"
	rm -f "$log_file"
	exit 1
}

# ------------------------------------------------------------------------------
# Package Manager & Dependency Helpers
# ------------------------------------------------------------------------------
pkg_update() {
	if command -v apt-get >/dev/null 2>&1; then
		apt-get -o DPkg::Lock::Timeout=600 update -qq
	elif command -v dnf >/dev/null 2>&1; then
		dnf check-update || true
	elif command -v yum >/dev/null 2>&1; then
		yum check-update || true
	elif command -v pacman >/dev/null 2>&1; then
		pacman -Sy --noconfirm
	elif command -v zypper >/dev/null 2>&1; then
		zypper refresh
	elif command -v apk >/dev/null 2>&1; then
		apk update
	fi
}

pkg_install() {
	local pkgs=("$@")
	if command -v apt-get >/dev/null 2>&1; then
		apt-get -o DPkg::Lock::Timeout=600 install -y --no-install-recommends "${pkgs[@]}"
	elif command -v dnf >/dev/null 2>&1; then
		dnf install -y "${pkgs[@]}"
	elif command -v yum >/dev/null 2>&1; then
		yum install -y "${pkgs[@]}"
	elif command -v pacman >/dev/null 2>&1; then
		pacman -S --noconfirm --needed "${pkgs[@]}"
	elif command -v zypper >/dev/null 2>&1; then
		zypper install -y --no-recommends "${pkgs[@]}"
	elif command -v apk >/dev/null 2>&1; then
		apk add --no-cache "${pkgs[@]}"
	fi
}

# pkg_install_each <pkg|alt>... - install packages one at a time, never
# failing: for tools that aren't packaged on every distro (or not under the
# same name). "a|b" tries a, then b. Reports what couldn't be installed.
pkg_install_each() {
	local spec alt ok alts missing=()
	for spec in "$@"; do
		ok=no
		IFS='|' read -r -a alts <<< "$spec"
		for alt in "${alts[@]}"; do
			if pkg_install "$alt"; then ok=yes; break; fi
		done
		[ "$ok" = yes ] || missing+=("${spec}")
	done
	if [ "${#missing[@]}" -gt 0 ]; then
		echo "Note: not available from this distro's repositories, skipped: ${missing[*]}" >&2
	fi
	return 0
}

install_core_dependencies() {
	# Second list per distro = storage/system tools the dashboard shells out
	# to, installed one at a time (pkg_install_each) because not every
	# distro packages all of them:
	#   dmidecode   RAM DIMM info            hdparm      disk standby/APM
	#   sudo        helper scripts           udevil      USB auto-mount (helper.sh/usb-mount.sh)
	#   ntfs-3g, exfatprogs, dosfstools, e2fsprogs  format/mount NTFS, exFAT, FAT, ext4
	#   fdisk       sfdisk (own package on Debian/Ubuntu; part of util-linux elsewhere)
	#   mergerfs    optional storage pooling
	#   lm-sensors  `sensors`/sensors-detect for fan control troubleshooting
	#               (nivaroos-fans finds and loads the fan driver itself)
	#   7zip        7z for Backup & Sync's encrypted archives (p7zip on older
	#               distros); without it "Encrypted archive" is unavailable
	# Known gaps: udevil is not in Fedora/RHEL, Arch (AUR only) or openSUSE
	# repos; mergerfs is not in Fedora/RHEL repos (upstream RPMs/COPR);
	# dmidecode doesn't exist on 32-bit ARM. Those features degrade
	# gracefully when the tool is missing.
	run_step "Installing Core System Dependencies" "
		pkg_update
		if command -v apt-get >/dev/null 2>&1; then
			pkg_install curl wget git tar ca-certificates udev util-linux pciutils smartmontools parted build-essential rsync iproute2 procps
			pkg_install_each dmidecode sudo hdparm udevil ntfs-3g 'exfatprogs|exfat-utils' dosfstools fdisk e2fsprogs mergerfs lm-sensors '7zip|p7zip-full'
		elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
			pkg_install curl wget git tar ca-certificates systemd-udev util-linux pciutils smartmontools parted make gcc rsync
			pkg_install_each dmidecode sudo hdparm udevil ntfs-3g 'exfatprogs|exfat-utils' dosfstools e2fsprogs mergerfs lm_sensors '7zip|p7zip-plugins'
		elif command -v pacman >/dev/null 2>&1; then
			pkg_install curl wget git tar ca-certificates systemd util-linux pciutils smartmontools parted base-devel rsync
			pkg_install_each dmidecode sudo hdparm udevil ntfs-3g exfatprogs dosfstools e2fsprogs mergerfs lm_sensors '7zip|p7zip'
		elif command -v zypper >/dev/null 2>&1; then
			pkg_install curl wget git tar ca-certificates udev util-linux pciutils smartmontools parted make gcc rsync
			pkg_install_each dmidecode sudo hdparm udevil ntfs-3g exfatprogs dosfstools e2fsprogs mergerfs sensors '7zip|p7zip-full'
		elif command -v apk >/dev/null 2>&1; then
			pkg_install curl wget git tar ca-certificates udev util-linux pciutils smartmontools parted build-base rsync
			pkg_install_each dmidecode sudo hdparm udevil ntfs-3g ntfs-3g-progs exfatprogs dosfstools sfdisk e2fsprogs mergerfs lm-sensors '7zip|p7zip'
		fi
	"
}

# ------------------------------------------------------------------------------
# System & Kernel Limits Tuning
# ------------------------------------------------------------------------------
tune_system_limits() {
	run_step "Tuning Kernel System Limits & Storage Automount" "
		mkdir -p /etc/sysctl.d /etc/security/limits.d /etc/udev/rules.d /etc/systemd/system/docker.service.d /var/lib/nivaroos
		touch \"$MANIFEST_FILE\"

		cat > /etc/sysctl.d/99-nivaroos.conf <<'SYSCTLEOF'
fs.inotify.max_user_watches = 524288
fs.inotify.max_user_instances = 8192
fs.file-max = 2097152
net.core.somaxconn = 65535
net.ipv4.ip_forward = 1
net.ipv4.conf.all.forwarding = 1
SYSCTLEOF
		echo '/etc/sysctl.d/99-nivaroos.conf' >> \"$MANIFEST_FILE\"
		sysctl --system >/dev/null 2>&1 || sysctl -p /etc/sysctl.d/99-nivaroos.conf >/dev/null 2>&1 || true

		cat > /etc/udev/rules.d/11-usb-mount.rules <<'UDEVEOF'
ACTION==\"add\", SUBSYSTEMS==\"usb\", SUBSYSTEM==\"block\", ENV{DEVTYPE}==\"partition\", RUN{program}+=\"/usr/bin/systemctl --no-block start usb-mount@%k.service\"
ACTION==\"remove\", SUBSYSTEMS==\"usb\", SUBSYSTEM==\"block\", ENV{DEVTYPE}==\"partition\", RUN{program}+=\"/usr/bin/systemctl --no-block stop usb-mount@%k.service\"
UDEVEOF
		echo '/etc/udev/rules.d/11-usb-mount.rules' >> \"$MANIFEST_FILE\"
		udevadm control --reload-rules >/dev/null 2>&1 || true
	"
}

# ------------------------------------------------------------------------------
# Docker Engine Detection & Installation
# ------------------------------------------------------------------------------
check_docker() {
	run_step "Verifying & Configuring Container Runtime (Docker)" "
		if ! command -v docker >/dev/null 2>&1; then
			# Downloaded in full before it runs. A release get.docker.com
			# doesn't know yet (a brand-new Ubuntu) falls back to the
			# distro's own Docker package.
			if ! { curl -fsSL https://get.docker.com -o /tmp/nivaroos-get-docker.sh && sh /tmp/nivaroos-get-docker.sh; }; then
				echo 'get.docker.com could not install Docker here - trying the distro package.' >&2
				if command -v apt-get >/dev/null 2>&1; then
					pkg_install docker.io
					pkg_install_each docker-compose-v2
				else
					exit 1
				fi
			fi
			rm -f /tmp/nivaroos-get-docker.sh
		fi

		mkdir -p /etc/systemd/system/docker.service.d
		cat > /etc/systemd/system/docker.service.d/override.conf <<'DOCKEREOF'
[Service]
DOCKEREOF
		echo '/etc/systemd/system/docker.service.d/override.conf' >> \"$MANIFEST_FILE\"

		systemctl daemon-reload >/dev/null 2>&1 || true
		systemctl enable --now docker >/dev/null 2>&1 || true

		d_running=false
		for i in {1..10}; do
			if docker info >/dev/null 2>&1; then
				d_running=true
				break
			fi
			echo \"Waiting for Docker daemon to respond (attempt \${i}/10)...\"
			sleep 1
		done

		if [ \"\$d_running\" = \"false\" ]; then
			echo \"Docker daemon failed to start or respond within 10 seconds. Recent status:\" >&2
			systemctl status docker --no-pager -l 2>&1 | tail -n 15 >&2 || true
			journalctl -u docker --no-pager -n 20 2>&1 >&2 || true
			exit 1
		fi
	"
}

# ------------------------------------------------------------------------------
# Repository Source & Go Toolchain
# ------------------------------------------------------------------------------
clone_or_update_repo() {
	run_step "Fetching NivaroOS Source (${BRANCH})" "
		mkdir -p \"$SRC_DIR\"
		if [ -n \"$LOCAL_REPO\" ] && [ \"$LOCAL_REPO\" != \"$SRC_DIR\" ]; then
			echo \"Syncing local repository from \${LOCAL_REPO} to \${SRC_DIR}...\"
			mkdir -p \"$SRC_DIR\"
			if command -v rsync >/dev/null 2>&1; then
				rsync -a --delete --exclude='mobile' --exclude='ui/node_modules' --exclude='.git' \"\${LOCAL_REPO}/\" \"\${SRC_DIR}/\"
			else
				cp -a \"\${LOCAL_REPO}/.\" \"\${SRC_DIR}/\"
				rm -rf \"\${SRC_DIR}/mobile\" \"\${SRC_DIR}/ui/node_modules\" 2>/dev/null || true
			fi
		elif [ -d \"${SRC_DIR}/.git\" ]; then
			cd \"$SRC_DIR\"
			# The server never builds the phone app (mobile/): a sparse,
			# blobless checkout keeps its sources and screenshot goldens off
			# every box. Older installs are switched over once, here; fetches
			# then skip mobile/'s files. Best effort: a git without
			# sparse-checkout just keeps the full tree.
			if ! git sparse-checkout list >/dev/null 2>&1; then
				git config remote.origin.promisor true || true
				git config remote.origin.partialclonefilter blob:none || true
				git sparse-checkout set --no-cone '/*' '!/mobile/' 2>/dev/null || true
			fi
			# Fetch exactly the requested branch or tag (works on the shallow
			# single-branch clone too, and when --branch or --repo changed).
			git remote set-url origin \"$REPO_URL\"
			git fetch --prune --depth 1 origin \"$BRANCH\"
			# reset --hard + clean (not checkout + pull) so a dirty tree -
			# left behind by a previous crashed run, or a manual edit made
			# while debugging - can never hard-abort this step. This is an
			# unattended installer/updater, not a workspace the running
			# user is expected to have made their own changes in.
			git reset --hard FETCH_HEAD
			git clean -fdx
		else
			if [ -d \"$SRC_DIR\" ] && [ \"\$(ls -A \"$SRC_DIR\" 2>/dev/null)\" ]; then
				rm -rf \"${SRC_DIR:?}\"/* \"${SRC_DIR:?}\"/.[!.]* 2>/dev/null || true
			fi
			# Everything but mobile/ (see above); a git too old for partial
			# or sparse clones gets the plain shallow clone.
			if git clone --branch \"$BRANCH\" --depth 1 --filter=blob:none --sparse \"$REPO_URL\" \"$SRC_DIR\" &&
				git -C \"$SRC_DIR\" sparse-checkout set --no-cone '/*' '!/mobile/'; then
				:
			else
				rm -rf \"${SRC_DIR:?}\"
				git clone --branch \"$BRANCH\" --depth 1 \"$REPO_URL\" \"$SRC_DIR\"
			fi
		fi

		# Ensure a new-enough Go toolchain: the highest go directive across
		# every module this installer builds. A distro Go (e.g. Debian 12's
		# 1.19) on PATH is not enough - it can't build a go 1.26 module, and
		# with GOTOOLCHAIN=local it won't fetch one either.
		go_need=\"${GO_MIN_VERSION}\"
		for mod in \"${SRC_DIR}\"/services/*/go.mod \"${SRC_DIR}\"/cli/go.mod; do
			[ -f \"\$mod\" ] || continue
			v=\"\$(awk '/^go [0-9]/ {print \$2; exit}' \"\$mod\")\"
			[ -n \"\$v\" ] || continue
			go_need=\"\$(printf '%s\\n%s\\n' \"\$go_need\" \"\$v\" | sort -V | tail -n1)\"
		done
		go_have=\"\"
		if [ -x /usr/local/go/bin/go ]; then
			go_have=\"\$(GOTOOLCHAIN=local /usr/local/go/bin/go env GOVERSION 2>/dev/null | sed 's/^go//')\"
		elif command -v go >/dev/null 2>&1; then
			go_have=\"\$(GOTOOLCHAIN=local go env GOVERSION 2>/dev/null | sed 's/^go//')\"
		fi
		go_ok=no
		if [ -n \"\$go_have\" ] && [ \"\$(printf '%s\\n%s\\n' \"\$go_need\" \"\$go_have\" | sort -V | head -n1)\" = \"\$go_need\" ]; then
			go_ok=yes
		fi
		if [ \"\$go_ok\" != yes ]; then
			echo \"Installing Go ${GO_VERSION} (found: \${go_have:-none}, need >= \$go_need)\"
			go_arch=\"amd64\"
			case \"\$(uname -m)\" in
				x86_64) go_arch=\"amd64\" ;;
				aarch64|arm64) go_arch=\"arm64\" ;;
				armv7l|armhf) go_arch=\"armv6l\" ;;
			esac
			wget -q \"https://go.dev/dl/go${GO_VERSION}.linux-\${go_arch}.tar.gz\" -O /tmp/go.tar.gz
			# Checked against Google's published SHA-256 before it is used.
			go_sum=\"\$(curl -fsSL \"https://dl.google.com/go/go${GO_VERSION}.linux-\${go_arch}.tar.gz.sha256\")\"
			echo \"\${go_sum%% *}  /tmp/go.tar.gz\" | sha256sum -c -
			rm -rf /usr/local/go
			tar -C /usr/local -xzf /tmp/go.tar.gz
			rm -f /tmp/go.tar.gz
			go_have=\"\$(GOTOOLCHAIN=local /usr/local/go/bin/go env GOVERSION 2>/dev/null | sed 's/^go//')\"
			if [ \"\$(printf '%s\\n%s\\n' \"\$go_need\" \"\${go_have:-0}\" | sort -V | head -n1)\" != \"\$go_need\" ]; then
				echo \"Go ${GO_VERSION} is older than the go \$go_need a go.mod requires - bump GO_VERSION in install.sh.\" >&2
				exit 1
			fi
		fi
		export PATH=\"/usr/local/go/bin:\$PATH\"
		go version
	"
}

# ------------------------------------------------------------------------------
# Microservices Compilation & Installation
# ------------------------------------------------------------------------------
install_core_services() {
	run_step "Compiling & Installing NivaroOS Microservices" "
		cd \"$SRC_DIR\"
		export PATH=\"/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin:\$PATH\"

		# Safety net (see install_safety_net): keep the build that is running
		# now as <binary>.prev before anything is replaced, so a bad update can
		# be undone with nivaroos-rollback - or is undone by the watchdog when
		# a service keeps failing right after it. The watchdog stays out of
		# the way while binaries are being replaced (the pause expires on its
		# own if the installer dies).
		mkdir -p /run/nivaroos
		echo \$(( \$(date +%s) + 7200 )) > /run/nivaroos/watchdog.pause
		for b in /usr/bin/nivaroos /usr/bin/nivaroos-*; do
			[ -f \"\$b\" ] || continue
			case \"\$b\" in *.prev|*.bad.*|*.new|*.new.*|*.bak|*.tmp|*.tmp.*|*/nivaroos-uninstall) continue ;; esac
			if [ ! -f \"\$b.prev\" ] || ! cmp -s \"\$b\" \"\$b.prev\"; then
				cp -p \"\$b\" \"\$b.prev.tmp\" && mv -f \"\$b.prev.tmp\" \"\$b.prev\" || true
			fi
		done

		# Stop existing background services before replacing binaries if upgrading
		active_units=(
			nivaroos.service
			nivaroos-gateway.service
			nivaroos-message-bus.service
			nivaroos-app-management.service
			nivaroos-local-storage.service
			nivaroos-user-service.service
			nivaroos-gpu-sidecar.service
		)
		for u in \"\${active_units[@]}\"; do
			systemctl stop \"\$u\" >/dev/null 2>&1 || true
		done

		# /DATA/Gallery/Wallpaper specifically - not just /DATA/Gallery - since
		# ui/src/shell/wallpaper/WallpaperModal.vue uploads directly into that
		# subdirectory and the upload handler doesn't create it on the fly;
		# without it, uploading a custom wallpaper fails on every fresh
		# install.
		mkdir -p /var/lib/nivaroos /var/run/nivaroos /etc/nivaroos /DATA/AppData /DATA/Documents /DATA/Downloads /DATA/Media /DATA/Gallery/Wallpaper /DATA/VMs/share
		chmod 777 /DATA/VMs/share 2>/dev/null || true
		touch \"$MANIFEST_FILE\"

		# 1. Compile Core Engine
		cd \"${SRC_DIR}/services/core\"
		go build -o /usr/bin/nivaroos .
		echo '/usr/bin/nivaroos' >> \"$MANIFEST_FILE\"

		# 2. Compile Gateway
		cd \"${SRC_DIR}/services/gateway\"
		go build -o /usr/bin/nivaroos-gateway .
		echo '/usr/bin/nivaroos-gateway' >> \"$MANIFEST_FILE\"

		# 3. Compile Message Bus
		cd \"${SRC_DIR}/services/message-bus\"
		go build -o /usr/bin/nivaroos-message-bus .
		echo '/usr/bin/nivaroos-message-bus' >> \"$MANIFEST_FILE\"

		# 4. Compile App Management
		cd \"${SRC_DIR}/services/app-management\"
		go build -o /usr/bin/nivaroos-app-management .
		echo '/usr/bin/nivaroos-app-management' >> \"$MANIFEST_FILE\"

		# 5. Compile Local Storage
		cd \"${SRC_DIR}/services/local-storage\"
		go build -o /usr/bin/nivaroos-local-storage .
		echo '/usr/bin/nivaroos-local-storage' >> \"$MANIFEST_FILE\"

		# 6. Compile User Service
		# Binary is named 'nivaroos-user' (no '-service' suffix) to match
		# what nivaroos-user-service.service's ExecStart actually invokes,
		# and what uninstall.sh already expects to remove - the '-service'
		# suffix belongs to the systemd UNIT name, not the binary.
		cd \"${SRC_DIR}/services/user\"
		go build -o /usr/bin/nivaroos-user .
		echo '/usr/bin/nivaroos-user' >> \"$MANIFEST_FILE\"

		# 7. Compile GPU Sidecar
		cd \"${SRC_DIR}/services/gpu-sidecar\"
		go build -o /usr/bin/nivaroos-gpu-sidecar .
		echo '/usr/bin/nivaroos-gpu-sidecar' >> \"$MANIFEST_FILE\"

		# 8. Compile Unified Management CLI
		cd \"${SRC_DIR}/cli\"
		go build -o /usr/bin/nivaroos-cli .
		echo '/usr/bin/nivaroos-cli' >> \"$MANIFEST_FILE\"
		ln -sf /usr/bin/nivaroos-cli /usr/local/bin/nivaroos 2>/dev/null || true
		ln -sf /usr/bin/nivaroos-cli /usr/bin/casaos-cli 2>/dev/null || true

		# 9. Install systemd service units from repository
		mkdir -p /usr/lib/systemd/system
		service_mappings=(
			\"${SRC_DIR}/services/core/build/sysroot/usr/lib/systemd/system/nivaroos.service:/usr/lib/systemd/system/nivaroos.service\"
			\"${SRC_DIR}/services/core/build/sysroot/usr/share/nivaroos/shell/usb-mount@.service:/usr/lib/systemd/system/usb-mount@.service\"
			\"${SRC_DIR}/services/gateway/build/sysroot/usr/lib/systemd/system/nivaroos-gateway.service:/usr/lib/systemd/system/nivaroos-gateway.service\"
			\"${SRC_DIR}/services/message-bus/build/sysroot/usr/lib/systemd/system/nivaroos-message-bus.service:/usr/lib/systemd/system/nivaroos-message-bus.service\"
			\"${SRC_DIR}/services/app-management/build/sysroot/usr/lib/systemd/system/nivaroos-app-management.service:/usr/lib/systemd/system/nivaroos-app-management.service\"
			\"${SRC_DIR}/services/local-storage/build/sysroot/usr/lib/systemd/system/nivaroos-local-storage.service:/usr/lib/systemd/system/nivaroos-local-storage.service\"
			\"${SRC_DIR}/services/user/build/sysroot/usr/lib/systemd/system/nivaroos-user-service.service:/usr/lib/systemd/system/nivaroos-user-service.service\"
		)

		for m in \"\${service_mappings[@]}\"; do
			src=\"\${m%%:*}\"
			dst=\"\${m##*:}\"
			if [ -f \"\$src\" ]; then
				cp -f \"\$src\" \"\$dst\"
				echo \"\$dst\" >> \"$MANIFEST_FILE\"
			fi
		done

		# Write GPU Sidecar service unit. This whole step is one double-quoted
		# run_step string: no backticks, \$(...) or bare double quotes in the
		# unit text below - they run or end the string before the quoted
		# heredoc ever sees them (a backticked nvidia-smi once pasted its
		# output table into the unit).
		cat > /usr/lib/systemd/system/nivaroos-gpu-sidecar.service <<'GPUEOF'
[Unit]
Description=NivaroOS GPU Sidecar
After=network.target

[Service]
ExecStart=/usr/bin/nivaroos-gpu-sidecar
Restart=always
# ProtectSystem=full (read-only /usr, /boot, /etc) was fine when this
# service only ever ran nvidia-smi - it now also runs
# gpu-driver-install.sh on request, which does a real apt-get install of a
# driver package and so unavoidably writes new files under /usr, like any
# package install does. Read-only /usr made every such install fail
# silently (permission/read-only-filesystem errors from inside apt),
# which is exactly what made the GPU widget's Install Driver button not
# work.
# NoNewPrivileges/ProtectHome stay - neither blocks a package install and
# both are still free hardening for the far more common nvidia-smi-only
# code path.
NoNewPrivileges=true
# read-only, not true: Host Desktop's xrandr/xset calls need to read the
# desktop session's X cookie (/run/user/<uid>/gdm/Xauthority, ~/.Xauthority),
# which ProtectHome=true hides entirely.
ProtectHome=read-only

[Install]
WantedBy=multi-user.target
GPUEOF
		echo '/usr/lib/systemd/system/nivaroos-gpu-sidecar.service' >> \"$MANIFEST_FILE\"

		# 10. Install default service configuration templates if not already present
		config_mappings=(
			\"${SRC_DIR}/services/core/build/sysroot/etc/nivaroos/casaos.conf.sample:/etc/nivaroos/casaos.conf\"
			\"${SRC_DIR}/services/gateway/build/sysroot/etc/nivaroos/gateway.ini.sample:/etc/nivaroos/gateway.ini\"
			\"${SRC_DIR}/services/message-bus/build/sysroot/etc/nivaroos/message-bus.conf.sample:/etc/nivaroos/message-bus.conf\"
			\"${SRC_DIR}/services/app-management/build/sysroot/etc/nivaroos/app-management.conf.sample:/etc/nivaroos/app-management.conf\"
			\"${SRC_DIR}/services/local-storage/build/sysroot/etc/nivaroos/local-storage.conf.sample:/etc/nivaroos/local-storage.conf\"
			\"${SRC_DIR}/services/user/build/sysroot/etc/nivaroos/user-service.conf.sample:/etc/nivaroos/user-service.conf\"
		)
		for cm in \"\${config_mappings[@]}\"; do
			csrc=\"\${cm%%:*}\"
			cdst=\"\${cm##*:}\"
			if [ -f \"\$csrc\" ]; then
				cp -f \"\$csrc\" \"\${cdst}.sample\" 2>/dev/null || true
				if [ ! -f \"\$cdst\" ]; then
					cp -f \"\$csrc\" \"\$cdst\"
					echo \"\$cdst\" >> \"$MANIFEST_FILE\"
				fi
			fi
		done

		# Leftovers from CasaOS's UI analytics (removed): nothing runs
		# start.d any more, so upgraded installs just lose the dead files.
		rm -f /etc/nivaroos/start.d/register-ui-events.sh /var/lib/nivaroos/ui-message-bus.json
		rmdir /etc/nivaroos/start.d 2>/dev/null || true

		# 11. Install the services' shell helpers. Only usb-mount.sh used to
		# be installed, but core sources helper.sh (device tree, network
		# cards, time zone, Samba reload) and local-storage sources
		# local-storage-helper.sh (disk and USB mounting) - on a fresh
		# install those calls all failed.
		mkdir -p /usr/share/nivaroos/shell
		for sh in \"${SRC_DIR}\"/services/core/build/sysroot/usr/share/nivaroos/shell/*.sh \"${SRC_DIR}\"/services/local-storage/build/sysroot/usr/share/nivaroos/shell/*.sh; do
			[ -f \"\$sh\" ] || continue
			cp -f \"\$sh\" /usr/share/nivaroos/shell/
			chmod 755 \"/usr/share/nivaroos/shell/\$(basename \"\$sh\")\"
			echo \"/usr/share/nivaroos/shell/\$(basename \"\$sh\")\" >> \"$MANIFEST_FILE\"
		done

		# Save custom port configuration if specified
		if [ -n \"$DETECTED_PORT\" ] && [ \"$DETECTED_PORT\" != \"80\" ]; then
			mkdir -p /etc/nivaroos
			cat > /etc/nivaroos/gateway.ini <<GWCONF
[gateway]
port = ${DETECTED_PORT}
GWCONF
			echo '/etc/nivaroos/gateway.ini' >> \"$MANIFEST_FILE\"
		fi

		sort -u -o \"$MANIFEST_FILE\" \"$MANIFEST_FILE\" 2>/dev/null || true
	"
}

# ------------------------------------------------------------------------------
# Fan control (services/fans, nivaroos-fans.service - every install). Built
# with cgo so NVIDIA fans work through NVML (the library is loaded at run
# time; a box without the driver just lists no NVIDIA fans); without a C
# compiler it falls back to a pure-Go build (motherboard/AMD fans only).
# `detect-modules` finds the motherboard fan controller driver (the kernel
# drivers probe the super-I/O chip themselves) and persists it in
# /etc/modules-load.d/nivaroos-fans.conf. Stopping the service hands every
# fan back to the BIOS, so an upgrade never leaves a fan at a fixed speed.
# ------------------------------------------------------------------------------
install_fan_control() {
	run_step "Installing Fan Control" "
		export PATH=\"/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin:\$PATH\"
		systemctl stop nivaroos-fans.service >/dev/null 2>&1 || true
		cd \"${SRC_DIR}/services/fans\"
		export GOWORK=off
		if ! CGO_ENABLED=1 go build -o /usr/bin/nivaroos-fans.new . ; then
			echo 'Fan control: cgo build failed - building without NVIDIA support.' >&2
			if ! CGO_ENABLED=0 go build -o /usr/bin/nivaroos-fans.new . ; then
				rm -f /usr/bin/nivaroos-fans.new
				echo 'Fan control failed to build (see the log above). The previous version, if any, is kept.' >&2
				systemctl start nivaroos-fans.service >/dev/null 2>&1 || true
				exit 0
			fi
		fi
		mv -f /usr/bin/nivaroos-fans.new /usr/bin/nivaroos-fans
		echo '/usr/bin/nivaroos-fans' >> \"$MANIFEST_FILE\"

		mkdir -p /etc/modules-load.d /var/lib/nivaroos/fans /run/nivaroos
		chmod 700 /var/lib/nivaroos/fans
		/usr/bin/nivaroos-fans detect-modules || echo 'Fan driver detection failed - motherboard fans stay under BIOS control.' >&2

		cp -f \"${SRC_DIR}/services/fans/build/sysroot/usr/lib/systemd/system/nivaroos-fans.service\" /usr/lib/systemd/system/nivaroos-fans.service
		echo '/usr/lib/systemd/system/nivaroos-fans.service' >> \"$MANIFEST_FILE\"
		systemctl daemon-reload >/dev/null 2>&1 || true
		systemctl enable --now nivaroos-fans >/dev/null 2>&1 || true
	"
}

# ------------------------------------------------------------------------------
# VM Manager Installation (Optional Add-on)
# ------------------------------------------------------------------------------
install_vm_manager() {
	run_step "Installing VM Manager (QEMU/KVM & Libvirt Sidecar)" "
		# pkg-config + the libvirt dev headers are build-time-only
		# requirements of the vm-sidecar's cgo libvirt.org/go/libvirt
		# bindings - without them 'go build' fails outright with
		# 'exec: \"pkg-config\": executable file not found in \$PATH' (or,
		# with pkg-config present but no dev headers, a '.pc file not
		# found' error instead). Runtime-only libvirt packages
		# (libvirt-daemon-system/libvirt-clients etc.) never pull these in.
		# virtiofsd: every VM's shared folder (/DATA/VMs/share) needs it -
		# without it a VM can't start. xorriso (libisoburn on Arch) builds
		# the NivaroOS Guest Tools disc.
		if command -v apt-get >/dev/null 2>&1; then
			pkg_install qemu-system-x86 qemu-utils libvirt-daemon-system libvirt-clients virtinst bridge-utils ovmf cloud-image-utils xorriso pkg-config libvirt-dev
			# Its own package from Debian 12 / Ubuntu 23.10; part of qemu before.
			pkg_install_each virtiofsd
		elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
			pkg_install qemu-kvm qemu-img libvirt libvirt-client virt-install bridge-utils edk2-ovmf virtiofsd xorriso pkgconf-pkg-config libvirt-devel
		elif command -v pacman >/dev/null 2>&1; then
			pkg_install qemu-base libvirt virt-install bridge-utils edk2-ovmf virtiofsd libisoburn pkgconf
		elif command -v zypper >/dev/null 2>&1; then
			pkg_install qemu-kvm qemu-tools libvirt libvirt-client virt-install bridge-utils qemu-ovmf-x86_64 virtiofsd xorriso pkg-config libvirt-devel
		elif command -v apk >/dev/null 2>&1; then
			pkg_install qemu-system-x86_64 qemu-img libvirt libvirt-daemon virt-install bridge dnsmasq ovmf virtiofsd xorriso pkgconf libvirt-dev
		else
			echo 'No known package manager found (apt/dnf/yum/pacman/zypper/apk) - cannot install QEMU/libvirt automatically. Skipping VM Manager; install those packages yourself and re-run with --with-vm.' >&2
			exit 1
		fi

		cd \"${SRC_DIR}/services/vm-sidecar\"
		export GOWORK=off
		go build -o /usr/bin/nivaroos-vm-sidecar .
		echo '/usr/bin/nivaroos-vm-sidecar' >> \"$MANIFEST_FILE\"

		cat > /usr/lib/systemd/system/nivaroos-vm-sidecar.service <<'VMEOF'
[Unit]
Description=NivaroOS VM Sidecar
After=network.target nivaroos-message-bus.service libvirtd.service

[Service]
ExecStart=/usr/bin/nivaroos-vm-sidecar
Restart=always
# Conservative hardening only - this service genuinely needs root (it
# bind-mounts host directories for shared folders, and talks to libvirtd
# for USB/PCI passthrough), so this stops short of ProtectSystem=strict or
# a narrow ReadWritePaths allowlist that risks blocking a real write path
# (/DATA, libvirt's own state dirs, etc.) this installer can't fully
# enumerate ahead of time.
NoNewPrivileges=true
# read-only, not true: Host Desktop's xrandr/xset calls need to read the
# desktop session's X cookie (/run/user/<uid>/gdm/Xauthority, ~/.Xauthority),
# which ProtectHome=true hides entirely.
ProtectHome=read-only

[Install]
WantedBy=multi-user.target
VMEOF
		echo '/usr/lib/systemd/system/nivaroos-vm-sidecar.service' >> \"$MANIFEST_FILE\"

		# One layout, matching vm-sidecar: ISOs in /DATA/VMs/ISOs, the one
		# shared folder VMs can reach in /DATA/VMs/share, and each VM's own
		# disks + UEFI vars in /DATA/VMs/<vm name>/ (created per VM). The
		# old lowercase 'isos' and the unused Images/Disks dirs are gone.
		mkdir -p /DATA/VMs/ISOs /DATA/VMs/share

		# virtio-win.iso: the Windows VirtIO drivers + guest agent. The
		# sidecar builds the NivaroOS Guest Tools disc from it (adding
		# WinFsp and the setup scripts) and would download it itself on
		# first use; fetching it here makes that first use instant.
		# Best-effort: several hundred MB, so a slow or offline install
		# just skips it.
		if [ ! -f /DATA/VMs/ISOs/virtio-win.iso ]; then
			echo 'Downloading virtio-win.iso (Windows guest drivers) - this can take a while...'
			# At most 5 minutes per run; a partial download is kept and
			# resumed (-C -) by the next run instead of starting over.
			curl -fL -C - --connect-timeout 15 --max-time 300 \
				-o /DATA/VMs/ISOs/.virtio-win.iso.part \
				https://fedorapeople.org/groups/virt/virtio-win/direct-downloads/stable-virtio/virtio-win.iso \
				&& mv /DATA/VMs/ISOs/.virtio-win.iso.part /DATA/VMs/ISOs/virtio-win.iso \
				|| echo 'virtio-win.iso is not fully downloaded yet - the next update resumes it, or NivaroOS fetches it the first time you set up Guest Tools in a VM.' >&2
		fi

		systemctl daemon-reload >/dev/null 2>&1 || true
		systemctl enable --now libvirtd >/dev/null 2>&1 || true
		systemctl enable --now nivaroos-vm-sidecar >/dev/null 2>&1 || true

		# CLI-only on purpose - see nivaroos-bridge.sh's own header comment
		# for why bridge networking (which can cut off the very session
		# managing it) is never exposed as a WebUI button.
		if [ -f \"${SRC_DIR}/installer/nivaroos-bridge.sh\" ]; then
			cp -f \"${SRC_DIR}/installer/nivaroos-bridge.sh\" /usr/local/bin/nivaroos-bridge
			chmod 755 /usr/local/bin/nivaroos-bridge
			echo '/usr/local/bin/nivaroos-bridge' >> \"$MANIFEST_FILE\"
		fi
	"
}

# ------------------------------------------------------------------------------
# Download Station Installation (Optional Add-on, on by default). One pure-Go
# service: the multi-connection download engine, the ad blocker, and the
# browser - a sandboxed Chromium run on demand by nivaroos-ds-browser
# (install-ds-browser.sh), with the rewriting proxy kept as its Lite mode.
# Filter lists are fetched by the service itself on first start.
# ------------------------------------------------------------------------------
install_download_station() {
	run_step "Installing Download Station (Downloader, Browser & Ad Blocker)" "
		export PATH=\"/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin:\$PATH\"
		systemctl stop nivaroos-download-sidecar.service >/dev/null 2>&1 || true

		cd \"${SRC_DIR}/services/download-sidecar\"
		export GOWORK=off
		go build -o /usr/bin/nivaroos-download-sidecar .
		echo '/usr/bin/nivaroos-download-sidecar' >> \"$MANIFEST_FILE\"

		mkdir -p /DATA/Downloads /var/lib/nivaroos/download-station
		chmod 700 /var/lib/nivaroos/download-station

		# The full browser (a sandboxed Chromium in its own unit and user,
		# plus uBlock Origin Lite). Same script on install and update, so an
		# existing box gains it when it updates; it never fails the install -
		# without it Download Station keeps its Lite browser. Runs before the
		# sidecar starts: the sidecar's unit needs the staging folder to exist.
		if [ \"${WITH_DS_BROWSER}\" != \"no\" ]; then
			bash \"${SRC_DIR}/services/download-sidecar/build/scripts/install-ds-browser.sh\" --src \"${SRC_DIR}\" --manifest \"$MANIFEST_FILE\" || echo 'The Download Station browser could not be set up - its Lite browser is used instead.'
		fi

		# Torrents: qbittorrent-nox (libtorrent - the fastest engine in our
		# benchmark) as nivaroos-torrent.service, a sandboxed unit the
		# sidecar starts only while a torrent is active. The official static
		# build by default (distros lag behind its security fixes), the
		# distro package with --ds-torrent-distro or when the static build
		# can't be had. Never fails the install: without either Download
		# Station uses its built-in engine. Its profile folder must exist
		# before the sidecar starts (the sidecar's unit lists it as writable).
		mkdir -p /var/lib/nivaroos/torrent
		chmod 700 /var/lib/nivaroos/torrent
		if [ \"${WITH_DS_TORRENT}\" != \"no\" ]; then
			# The unit first: the script restarts a running engine on the new binary.
			cp -f \"${SRC_DIR}/services/download-sidecar/build/sysroot/usr/lib/systemd/system/nivaroos-torrent.service\" /usr/lib/systemd/system/nivaroos-torrent.service
			echo '/usr/lib/systemd/system/nivaroos-torrent.service' >> \"$MANIFEST_FILE\"
			systemctl daemon-reload >/dev/null 2>&1 || true
			qbt_script=\"${SRC_DIR}/services/download-sidecar/build/scripts/install-qbittorrent.sh\"
			if [ \"${DS_TORRENT_BUILD}\" != \"static\" ] || ! bash \"\$qbt_script\" --manifest \"$MANIFEST_FILE\"; then
				if ! command -v qbittorrent-nox >/dev/null 2>&1; then
					if pkg_install qbittorrent-nox && command -v qbittorrent-nox >/dev/null 2>&1; then
						mkdir -p /usr/share/nivaroos/torrent
						echo 'package:qbittorrent-nox' > /usr/share/nivaroos/torrent/installed.txt
					fi
				fi
				bash \"\$qbt_script\" --distro --manifest \"$MANIFEST_FILE\" || echo 'qbittorrent-nox is not available for this system - torrents use the built-in engine.'
			fi
		fi

		# The unit lives in the project (hardened: read-only system, writable
		# storage roots only) - one copy, updated with every install.
		cp -f \"${SRC_DIR}/services/download-sidecar/build/sysroot/usr/lib/systemd/system/nivaroos-download-sidecar.service\" /usr/lib/systemd/system/nivaroos-download-sidecar.service
		echo '/usr/lib/systemd/system/nivaroos-download-sidecar.service' >> \"$MANIFEST_FILE\"

		systemctl daemon-reload >/dev/null 2>&1 || true
		systemctl enable --now nivaroos-download-sidecar >/dev/null 2>&1 || true
	"
}

# ------------------------------------------------------------------------------
# Backup & Sync Installation (Optional Add-on, on by default). One pure-Go
# service: the job side (store, schedules, conditions, hooks, REST API at
# /v1/backup through the gateway) and the engine (rclone linked as a
# library, reading the same rclone.conf local-storage maintains, so every
# cloud account is a backup target). It links local-storage's TeraBox
# backend, so it builds from the same source tree. Its first start imports
# the old backup/sync Scheduled Tasks by itself (through core's API, and
# retried until core answers) - no installer step for that.
# ------------------------------------------------------------------------------
install_backup() {
	run_step "Installing Backup & Sync (Scheduled Backups, Sync & Archives)" "
		export PATH=\"/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin:\$PATH\"
		# Stopping it now is safe: the update check (check_running_backups)
		# already waited for or cancelled running jobs, and a run cut off
		# here is replayed and retried at the next start.
		systemctl stop nivaroos-backup.service >/dev/null 2>&1 || true

		cd \"${SRC_DIR}/services/backup\"
		export GOWORK=off
		if ! go build -o /usr/bin/nivaroos-backup.new .; then
			rm -f /usr/bin/nivaroos-backup.new
			echo 'Backup & Sync failed to build (see the log above). The previous version, if any, is kept.' >&2
			exit 1
		fi
		mv -f /usr/bin/nivaroos-backup.new /usr/bin/nivaroos-backup
		echo '/usr/bin/nivaroos-backup' >> \"$MANIFEST_FILE\"

		# The service creates what it needs itself (StateDirectory= and at
		# start); creating the folders here too keeps the modes right on a
		# box where an older run left them 0755.
		mkdir -p /var/lib/nivaroos/backup/logs /var/lib/nivaroos/backup/staging /var/lib/nivaroos/backup/engine
		chmod 700 /var/lib/nivaroos/backup /var/lib/nivaroos/backup/logs /var/lib/nivaroos/backup/staging /var/lib/nivaroos/backup/engine
		# rclone's config folder (local-storage's cloud accounts) is one of
		# the unit's writable paths; one that appears only after the service
		# started would stay read-only to it until a restart.
		[ -d /root/.config/rclone ] || install -d -m 700 /root/.config/rclone

		# The unit lives in the project (hardened: read-only system, writable
		# storage roots, own state and rclone's config folder only) - one
		# copy, updated with every install.
		cp -f \"${SRC_DIR}/services/backup/build/sysroot/usr/lib/systemd/system/nivaroos-backup.service\" /usr/lib/systemd/system/nivaroos-backup.service
		echo '/usr/lib/systemd/system/nivaroos-backup.service' >> \"$MANIFEST_FILE\"

		systemctl daemon-reload >/dev/null 2>&1 || true
		systemctl enable --now nivaroos-backup >/dev/null 2>&1 || true
	"
}

# ------------------------------------------------------------------------------
# Update deferral (spec docs/specs/2026-09-24-backup-sync-app.md §8.4). An
# upgrade restarts every service; a backup cut off mid-transfer is retried
# afterwards, but a long cloud upload would start over, so running backups
# are waited for (or cancelled, or the update aborted) first. Runs before
# any service is stopped. Asks the service directly on its loopback port as
# local automation (no token: loopback peer, no browser headers); an old
# install without Backup, or a service that doesn't answer, has nothing to
# wait for. --force skips the check.
# ------------------------------------------------------------------------------
BACKUP_API="http://127.0.0.1:28643/v1/backup"
BACKUP_WAIT_MAX_SECS=7200

# running_backup_ids prints the ids of running backup runs, one per line
# (nothing when none run or the service isn't there).
running_backup_ids() {
	local body
	body="$(curl -fsS -m 5 "${BACKUP_API}/runs?status=running" 2>/dev/null)" || return 0
	printf '%s' "$body" | grep -o '"id":"run_[^"]*"' | sed 's/^"id":"//; s/"$//' || true
}

# running_backup_names prints "job name" per running run, for the prompt.
running_backup_names() {
	local body
	body="$(curl -fsS -m 5 "${BACKUP_API}/runs?status=running" 2>/dev/null)" || return 0
	printf '%s' "$body" | grep -o '"job_name":"[^"]*"' | sed 's/^"job_name":"//; s/"$//' || true
}

cancel_running_backups() {
	local id
	for id in $(running_backup_ids); do
		curl -fsS -m 10 -X POST -H 'Content-Type: application/json' -d '{}' "${BACKUP_API}/runs/${id}/cancel" >/dev/null 2>&1 || true
	done
	# Cancelling stops the transfer and runs the post hooks (restarting
	# apps and VMs the job stopped); give that a moment to finish.
	local i
	for i in $(seq 1 60); do
		[ -z "$(running_backup_ids)" ] && return 0
		sleep 2
	done
	warn "Some backups are still stopping; the upgrade stops the rest, and those are retried after it."
}

# wait_for_running_backups waits up to BACKUP_WAIT_MAX_SECS, then cancels.
wait_for_running_backups() {
	local waited=0
	while [ -n "$(running_backup_ids)" ]; do
		if [ "$waited" -ge "$BACKUP_WAIT_MAX_SECS" ]; then
			warn "Backups still running after $((BACKUP_WAIT_MAX_SECS / 3600)) h - cancelling them so the upgrade can go on (each job runs again at its next scheduled time)."
			cancel_running_backups
			return 0
		fi
		if [ $((waited % 60)) -eq 0 ]; then
			info "Waiting for running backups to finish ($((waited / 60)) min so far; --force skips this)..."
		fi
		sleep 15
		waited=$((waited + 15))
	done
	success "No backups running any more."
}

check_running_backups() {
	[ "$IS_UPGRADE" = "true" ] || return 0
	[ "$FORCE" = "yes" ] && return 0
	command -v curl >/dev/null 2>&1 || return 0
	[ -n "$(running_backup_ids)" ] || return 0

	local names
	names="$(running_backup_names)"
	ui_warn "Backups are running right now:"
	while IFS= read -r n; do
		[ -n "$n" ] && printf '      %s\n' "${n}"
	done <<< "$names"

	if unattended; then
		wait_for_running_backups
		return 0
	fi

	local choice=""
	printf '    %s1%s  Wait for them to finish (default)\n' "$UI_B" "$UI_R"
	printf '    %s2%s  Cancel them now (each job runs again at its next scheduled time)\n' "$UI_B" "$UI_R"
	printf '    %s3%s  Abort the update\n' "$UI_B" "$UI_R"
	printf '  ? Choice [1]: '
	read -r choice </dev/tty || choice=""
	case "$choice" in
		2) cancel_running_backups ;;
		3)
			info "Update aborted; nothing was changed."
			exit 0
			;;
		*) wait_for_running_backups ;;
	esac
}

# pause_backup_service stops Backup & Sync for the rest of an upgrade, right
# after check_running_backups and before any core service is stopped. Left
# running, it could start a scheduled run during the clone and build that
# follow, whose apps would then be stopped while install_core_services
# stops app-management underneath it. Stopping it here also runs the post
# hooks of anything still running (with --force the check was skipped)
# while app-management still answers; a run cut off is replayed, and a
# restart that fails is retried, when the new build starts. install_backup
# starts it again; with --without-backup, remove_backup_module removes it.
pause_backup_service() {
	[ "$IS_UPGRADE" = "true" ] || return 0
	systemctl is-active --quiet nivaroos-backup.service 2>/dev/null || return 0
	info "Pausing Backup & Sync until the upgrade is done..."
	systemctl stop nivaroos-backup.service >/dev/null 2>&1 || true
}

# remove_backup_module is an upgrade with --without-backup on a box that has
# Backup & Sync: it is removed instead of being left running, unbuilt,
# against the new core. First the Scheduled Tasks it took over are handed
# back (while core still runs - this is before install_core_services), so
# the user's old backups run again; then the binary and unit go. Its data
# (/var/lib/nivaroos/backup: jobs, history) stays, so installing it again
# later picks up where it left off. Failing to reach core is reported, not
# fatal.
remove_backup_module() {
	[ "$IS_UPGRADE" = "true" ] || return 0
	[ "$WITH_BACKUP" = "no" ] || return 0
	[ -x /usr/bin/nivaroos-backup ] || return 0
	info "Removing Backup & Sync (--without-backup); its jobs and history are kept in /var/lib/nivaroos/backup."
	systemctl stop nivaroos-backup.service >/dev/null 2>&1 || true
	if ! /usr/bin/nivaroos-backup release-scheduled-tasks; then
		warn "Could not hand every Scheduled Task back from Backup & Sync (is core running?). They stay disabled in Scheduled Tasks; enable them there."
	fi
	systemctl disable nivaroos-backup.service >/dev/null 2>&1 || true
	rm -f /usr/bin/nivaroos-backup /usr/lib/systemd/system/nivaroos-backup.service
	systemctl daemon-reload >/dev/null 2>&1 || true
	success "Backup & Sync removed."
}

# ------------------------------------------------------------------------------
# Host Desktop Streaming Installation (Optional Add-on, requires VM Manager -
# it streams over the vm-sidecar the VM Manager installs). The sidecar binary
# owns this entirely: the x11vnc wrapper, its unit and the desktop provisioner
# are embedded in it (services/vm-sidecar/hostdesktop/, the single source of
# truth), and `nivaroos-vm-sidecar install-host-desktop` is exactly what the
# dashboard's Install button and `nivaroos host-desktop enable` run -
# per-distro packages (x11vnc, xdotool, xrandr/xset/xrefresh, xdpyinfo) and
# all. Whether there's an X11 session to stream is checked later, from the
# dashboard.
# ------------------------------------------------------------------------------
install_host_desktop() {
	run_step "Installing Host Desktop Streaming (x11vnc)" "
		if [ ! -x /usr/bin/nivaroos-vm-sidecar ]; then
			echo 'nivaroos-vm-sidecar is missing (VM Manager step failed?) - skipping Host Desktop. Enable it later with: nivaroos host-desktop enable' >&2
			exit 0
		fi
		# Optional add-on: a distro without x11vnc (e.g. RHEL without EPEL)
		# must not abort the whole installation.
		if ! /usr/bin/nivaroos-vm-sidecar install-host-desktop; then
			echo 'Host Desktop streaming could not be installed (see above) - the rest of NivaroOS is unaffected. Retry later with: nivaroos host-desktop enable' >&2
		fi
		for f in /usr/local/bin/nivaroos-host-desktop.sh /usr/local/bin/nivaroos-host-desktop-de-install.sh /usr/lib/systemd/system/nivaroos-host-desktop.service; do
			[ -e \"\$f\" ] && echo \"\$f\" >> \"$MANIFEST_FILE\"
		done
		sort -u -o \"$MANIFEST_FILE\" \"$MANIFEST_FILE\" 2>/dev/null || true
	"
}

# ------------------------------------------------------------------------------
# Samba Network File Sharing (Core - always installed, not a selectable
# add-on, since Network Shares is a core dashboard feature)
#
# services/core/service/shares.go writes /etc/samba/smb.nivaroos.conf and
# restarts the "smbd" unit whenever shares change from the dashboard - but it
# only ever writes /etc/samba/smb.conf if that file already exists, and does
# nothing (silently, no error) if it doesn't. Without the samba package
# actually installed, /etc/samba never exists, smbd is never a valid unit,
# and Network Shares in the dashboard quietly does nothing forever. This
# step is what makes that feature real.
# ------------------------------------------------------------------------------
provision_samba() {
	if command -v apt-get >/dev/null 2>&1; then
		pkg_install samba
	elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
		pkg_install samba samba-common-tools
	elif command -v pacman >/dev/null 2>&1; then
		pkg_install samba
	elif command -v zypper >/dev/null 2>&1; then
		pkg_install samba
	elif command -v apk >/dev/null 2>&1; then
		pkg_install samba samba-common-tools
	else
		echo "No known package manager found - cannot install Samba automatically. Network Shares will stay unavailable until 'samba' is installed manually." >&2
		return 0
	fi

	mkdir -p /etc/samba
	systemctl enable --now smbd >/dev/null 2>&1 || systemctl enable --now smb >/dev/null 2>&1 || systemctl enable --now samba >/dev/null 2>&1 || true
	systemctl enable --now nmbd >/dev/null 2>&1 || systemctl enable --now nmb >/dev/null 2>&1 || true
}

install_samba() {
	run_step "Installing Samba Network File Sharing" "provision_samba"
}

# ------------------------------------------------------------------------------
# Web Dashboard UI Assets
#
# The git repo never commits prebuilt frontend output (ui/dist,
# build/sysroot/.../www are all build artifacts, not tracked) - so a
# genuinely fresh `git clone` has nothing to serve unless something builds
# it. Node.js + pnpm were never installed by this script at all, so on any
# machine that doesn't already happen to have pnpm from unrelated prior
# work, this step always failed outright with "no prebuilt www files
# found" - it never had a real path to succeed on a first-time install.
# ------------------------------------------------------------------------------
# The UI builds with Vite 8: Node 20.19+ or 22.12+.
node_ok() {
	local v major minor
	v="$(node -v 2>/dev/null | sed -E 's/^v//')"
	major="${v%%.*}"
	minor="${v#*.}"
	minor="${minor%%.*}"
	[ "$major" -ge 23 ] 2>/dev/null && return 0
	[ "$major" -eq 22 ] 2>/dev/null && [ "$minor" -ge 12 ] 2>/dev/null && return 0
	[ "$major" -eq 20 ] 2>/dev/null && [ "$minor" -ge 19 ] 2>/dev/null
}

ensure_node_toolchain() {
	if command -v pnpm >/dev/null 2>&1 && node_ok; then
		return 0
	fi

	echo "No prebuilt web dashboard found - installing Node.js and pnpm to build it from source..."

	if ! node_ok; then
		# Distro-default Node packages are frequently too old (or entirely
		# absent) for a modern Vite/Vue build - NodeSource's setup script is
		# the standard way to get a current LTS on apt/dnf/yum systems.
		if command -v apt-get >/dev/null 2>&1; then
			{ curl -fsSL https://deb.nodesource.com/setup_22.x -o /tmp/nivaroos-nodesource.sh && bash /tmp/nivaroos-nodesource.sh >/dev/null 2>&1; } || true
			rm -f /tmp/nivaroos-nodesource.sh
			pkg_install nodejs
		elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
			{ curl -fsSL https://rpm.nodesource.com/setup_22.x -o /tmp/nivaroos-nodesource.sh && bash /tmp/nivaroos-nodesource.sh >/dev/null 2>&1; } || true
			rm -f /tmp/nivaroos-nodesource.sh
			pkg_install nodejs
		elif command -v pacman >/dev/null 2>&1; then
			pkg_install nodejs npm
		elif command -v zypper >/dev/null 2>&1; then
			pkg_install nodejs20 || pkg_install nodejs
		elif command -v apk >/dev/null 2>&1; then
			pkg_install nodejs npm
		fi
	fi

	# Read the exact pnpm version this UI expects from its own
	# package.json (via corepack, Node's built-in package-manager
	# activator) rather than hardcoding a version here that would drift
	# out of sync with the repo over time.
	local pnpm_ver
	pnpm_ver="$(grep -oE '"packageManager":[[:space:]]*"pnpm@[^"]+"' "${SRC_DIR}/ui/package.json" 2>/dev/null | sed -E 's/.*pnpm@([^"]+)"/\1/')"
	pnpm_ver="${pnpm_ver:-9}"

	if command -v corepack >/dev/null 2>&1; then
		corepack enable >/dev/null 2>&1 || true
		corepack prepare "pnpm@${pnpm_ver}" --activate >/dev/null 2>&1 || true
	fi
	if ! command -v pnpm >/dev/null 2>&1 && command -v npm >/dev/null 2>&1; then
		npm install -g "pnpm@${pnpm_ver}" >/dev/null 2>&1 || npm install -g pnpm >/dev/null 2>&1 || true
	fi
}

install_ui() {
	run_step "Deploying Web Dashboard & Frontend Assets" "
		mkdir -p /var/lib/nivaroos/www

		ui_source_dir=\"\"
		if [ -d \"${SRC_DIR}/build/sysroot/var/lib/nivaroos/www\" ] && [ -f \"${SRC_DIR}/build/sysroot/var/lib/nivaroos/www/index.html\" ]; then
			ui_source_dir=\"${SRC_DIR}/build/sysroot/var/lib/nivaroos/www\"
		elif [ -d \"${SRC_DIR}/ui/build/sysroot/var/lib/nivaroos/www\" ] && [ -f \"${SRC_DIR}/ui/build/sysroot/var/lib/nivaroos/www/index.html\" ]; then
			ui_source_dir=\"${SRC_DIR}/ui/build/sysroot/var/lib/nivaroos/www\"
		elif [ -d \"${SRC_DIR}/ui/dist\" ] && [ -f \"${SRC_DIR}/ui/dist/index.html\" ]; then
			ui_source_dir=\"${SRC_DIR}/ui/dist\"
		elif [ -d \"${SRC_DIR}/build/sysroot/var/lib/casaos/www\" ] && [ -f \"${SRC_DIR}/build/sysroot/var/lib/casaos/www/index.html\" ]; then
			ui_source_dir=\"${SRC_DIR}/build/sysroot/var/lib/casaos/www\"
		fi

		# If prebuilt output is not found, install Node.js/pnpm (if not
		# already present) and build from source.
		if [ -z \"\$ui_source_dir\" ]; then
			ensure_node_toolchain
		fi
		if [ -z \"\$ui_source_dir\" ] && command -v pnpm >/dev/null 2>&1; then
			echo 'Building frontend from source with pnpm...'
			( cd \"${SRC_DIR}/ui\" && pnpm install --prefer-offline 2>/dev/null || pnpm install ) && \
			( cd \"${SRC_DIR}/ui\" && pnpm run build )
			if [ -d \"${SRC_DIR}/ui/build/sysroot/var/lib/nivaroos/www\" ]; then
				ui_source_dir=\"${SRC_DIR}/ui/build/sysroot/var/lib/nivaroos/www\"
			elif [ -d \"${SRC_DIR}/build/sysroot/var/lib/nivaroos/www\" ]; then
				ui_source_dir=\"${SRC_DIR}/build/sysroot/var/lib/nivaroos/www\"
			fi
		fi

		if [ -n \"\$ui_source_dir\" ]; then
			# Keep the dashboard being replaced (nivaroos-rollback www).
			if [ -f /var/lib/nivaroos/www/index.html ]; then
				rm -rf /var/lib/nivaroos/www.prev.tmp
				cp -a /var/lib/nivaroos/www /var/lib/nivaroos/www.prev.tmp && rm -rf /var/lib/nivaroos/www.prev && mv /var/lib/nivaroos/www.prev.tmp /var/lib/nivaroos/www.prev || true
			fi
			cp -rf \"\$ui_source_dir\"/* /var/lib/nivaroos/www/
		elif [ -f /var/lib/nivaroos/www/index.html ]; then
			echo 'Preserving existing installed web dashboard in /var/lib/nivaroos/www.'
		else
			echo 'Could not find or build the web dashboard (no prebuilt www files found). The rest of NivaroOS will run, but the dashboard will be empty until you build ui/ manually.' >&2
			exit 1
		fi
	"
}

# ------------------------------------------------------------------------------
# Service Activation
# ------------------------------------------------------------------------------
start_core_services() {
	run_step "Reloading System Daemons & Starting Services" "
		systemctl daemon-reload
		services=(
			nivaroos-gateway
			nivaroos-message-bus
			nivaroos-user-service
			nivaroos-local-storage
			nivaroos-app-management
			nivaroos-gpu-sidecar
			nivaroos-fans
			nivaroos
		)

		for svc in \"\${services[@]}\"; do
			systemctl enable --now \"\$svc\" >/dev/null 2>&1 || true
		done
	"
}

# ------------------------------------------------------------------------------
# Health Verification
# ------------------------------------------------------------------------------
verify_health() {
	run_step "Verifying Microservices Health & Endpoints" "
		target_port=\"${DETECTED_PORT:-80}\"
		healthy=false

		for i in {1..20}; do
			if curl -fsSL -m 2 \"http://127.0.0.1:\${target_port}/ping\" >/dev/null 2>&1 || \
			   curl -fsSL -m 2 \"http://127.0.0.1:\${target_port}/v1/sys/version\" >/dev/null 2>&1 || \
			   curl -fsSL -m 2 \"http://127.0.0.1:\${target_port}/\" >/dev/null 2>&1; then
				healthy=true
				break
			fi
			sleep 1
		done

		if [ \"\$healthy\" = \"false\" ]; then
			if systemctl is-active --quiet nivaroos-gateway; then
				healthy=true
			fi
		fi

		if [ \"\$healthy\" = \"false\" ]; then
			echo \"Gateway did not respond on port \${target_port} after 20s\" >&2
			exit 1
		fi
	"
}

# ------------------------------------------------------------------------------
# mDNS Advertisement (Core - always installed, not a selectable add-on,
# since mobile-app discovery is a core feature). Lets the NivaroOS mobile
# app auto-discover this server on the local network instead of the user
# typing an IP address.
# ------------------------------------------------------------------------------
install_mdns_advertisement() {
	run_step "Advertising This Server on the Local Network (mDNS)" "
		if command -v apt-get >/dev/null 2>&1; then
			pkg_install avahi-daemon avahi-utils
		elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
			pkg_install avahi avahi-tools
		elif command -v pacman >/dev/null 2>&1; then
			pkg_install avahi
		elif command -v zypper >/dev/null 2>&1; then
			pkg_install avahi
		elif command -v apk >/dev/null 2>&1; then
			pkg_install avahi avahi-tools
		fi

		if command -v avahi-daemon >/dev/null 2>&1; then
			mkdir -p /etc/avahi/services
			mdns_port=\"${DETECTED_PORT:-80}\"
			cat > /etc/avahi/services/nivaroos.service <<MDNSEOF
<?xml version=\"1.0\" standalone='no'?>
<!DOCTYPE service-group SYSTEM \"avahi-service.dtd\">
<service-group>
	<name replace-wildcards=\"yes\">NivaroOS on %h</name>
	<service>
		<type>_nivaroos._tcp</type>
		<port>\${mdns_port}</port>
	</service>
</service-group>
MDNSEOF
			echo '/etc/avahi/services/nivaroos.service' >> \"$MANIFEST_FILE\"
			systemctl enable --now avahi-daemon >/dev/null 2>&1 || true
			systemctl reload avahi-daemon >/dev/null 2>&1 || systemctl restart avahi-daemon >/dev/null 2>&1 || true
		else
			echo 'avahi-daemon not available on this system - the mobile app will still work, just via manually entering this server'\''s address instead of auto-discovery.'
		fi
	"
}

# ------------------------------------------------------------------------------
# Safety Net: restart limits, watchdog, rollback & recovery tools
# ------------------------------------------------------------------------------
# For a box that is looked after remotely (Tailscale/SSH) or not at all:
#  - every nivaroos*.service gets a drop-in with sane restart limits;
#  - nivaroos-watchdog.timer (every 2 min) restarts failed units, rolls a
#    binary that keeps failing right after an update back to <binary>.prev,
#    keeps tailscaled/sshd up and probes the gateway over HTTP;
#  - nivaroos-rollback / nivaroos-deploy / nivaroos-recover for a shell.
install_safety_net() {
	run_step "Installing Safety Net (watchdog, rollback & recovery tools)" "
		mkdir -p /usr/local/lib/nivaroos /usr/local/bin /var/lib/nivaroos/watchdog /var/log/nivaroos
		install -m 644 \"${SRC_DIR}/installer/nivaroos-safety-lib.sh\" /usr/local/lib/nivaroos/safety-lib.sh
		install -m 755 \"${SRC_DIR}/installer/nivaroos-watchdog.sh\" /usr/local/lib/nivaroos/nivaroos-watchdog
		install -m 755 \"${SRC_DIR}/installer/nivaroos-rollback.sh\" /usr/local/bin/nivaroos-rollback
		install -m 755 \"${SRC_DIR}/installer/nivaroos-deploy.sh\" /usr/local/bin/nivaroos-deploy
		install -m 755 \"${SRC_DIR}/installer/nivaroos-recover.sh\" /usr/local/bin/nivaroos-recover
		install -m 644 \"${SRC_DIR}/installer/systemd/nivaroos-watchdog.service\" /usr/lib/systemd/system/nivaroos-watchdog.service
		install -m 644 \"${SRC_DIR}/installer/systemd/nivaroos-watchdog.timer\" /usr/lib/systemd/system/nivaroos-watchdog.timer
		for f in /usr/local/lib/nivaroos /usr/local/bin/nivaroos-rollback /usr/local/bin/nivaroos-deploy /usr/local/bin/nivaroos-recover \\
			/usr/lib/systemd/system/nivaroos-watchdog.service /usr/lib/systemd/system/nivaroos-watchdog.timer /var/lib/nivaroos/watchdog /var/log/nivaroos/watchdog.log; do
			grep -qxF \"\$f\" \"$MANIFEST_FILE\" 2>/dev/null || echo \"\$f\" >> \"$MANIFEST_FILE\"
		done

		# Restart limits for every NivaroOS service unit, whichever step
		# installed it (explicit per-unit drop-ins: prefix drop-ins need a
		# newer systemd than some supported distros ship).
		for unit_file in /usr/lib/systemd/system/nivaroos*.service /etc/systemd/system/nivaroos*.service; do
			[ -f \"\$unit_file\" ] || continue
			unit=\$(basename \"\$unit_file\")
			case \"\$unit\" in nivaroos-watchdog.service) continue ;; esac
			mkdir -p \"/etc/systemd/system/\$unit.d\"
			install -m 644 \"${SRC_DIR}/installer/systemd/10-nivaroos-resilience.conf\" \"/etc/systemd/system/\$unit.d/10-nivaroos-resilience.conf\"
		done
		systemctl daemon-reload
		systemctl enable --now nivaroos-watchdog.timer >/dev/null 2>&1 || true

		# Tailscale is how this box is reached from outside the LAN: make
		# sure it comes back after a reboot.
		if systemctl list-unit-files tailscaled.service >/dev/null 2>&1 && [ -x \"\$(command -v tailscaled 2>/dev/null)\" ]; then
			systemctl enable tailscaled >/dev/null 2>&1 || true
		fi

		# Installation is over: the watchdog may act again.
		rm -f /run/nivaroos/watchdog.pause
	"
}

# ------------------------------------------------------------------------------
# Uninstall Wrapper Installation
# ------------------------------------------------------------------------------
install_uninstall_wrapper() {
	run_step "Installing Dedicated Uninstaller Script" "
		mkdir -p /usr/local/bin /usr/bin
		if [ -f \"${SRC_DIR}/installer/uninstall.sh\" ]; then
			cp -f \"${SRC_DIR}/installer/uninstall.sh\" /usr/bin/nivaroos-uninstall
			chmod 755 /usr/bin/nivaroos-uninstall
			ln -sf /usr/bin/nivaroos-uninstall /usr/local/bin/nivaroos-uninstall 2>/dev/null || true
			echo '/usr/bin/nivaroos-uninstall' >> \"$MANIFEST_FILE\"
		fi

		# nivaroos-gpu-sidecar shells out to this exact path when the
		# dashboard's GPU widget suggests installing a driver - see
		# services/gpu-sidecar/main.go.
		if [ -f \"${SRC_DIR}/installer/gpu-driver-install.sh\" ]; then
			cp -f \"${SRC_DIR}/installer/gpu-driver-install.sh\" /usr/local/bin/nivaroos-gpu-driver-install.sh
			chmod 755 /usr/local/bin/nivaroos-gpu-driver-install.sh
			echo '/usr/local/bin/nivaroos-gpu-driver-install.sh' >> \"$MANIFEST_FILE\"
		fi
	"
}

# ------------------------------------------------------------------------------
# Network IP Discovery Helpers
# ------------------------------------------------------------------------------
list_reachable_ips() {
	local out=""
	if command -v ip >/dev/null 2>&1; then
		out="$(ip -4 -o addr show scope global 2>/dev/null | awk '{split($4, a, "/"); print a[1], $2}')"
	elif command -v ifconfig >/dev/null 2>&1; then
		out="$(ifconfig 2>/dev/null | awk '/inet / && !/127.0.0.1/ {print $2}')"
	fi
	echo "$out" | grep -vE "docker0|br-|veth|tailscale|wg|tun|virbr" || true
}

list_vpn_ips() {
	local out=""
	if command -v ip >/dev/null 2>&1; then
		out="$(ip -4 -o addr show scope global 2>/dev/null | awk '{split($4, a, "/"); print a[1], $2}')"
	fi
	echo "$out" | grep -E "tailscale|wg|tun" || true
}

# ------------------------------------------------------------------------------
# Summary
# ------------------------------------------------------------------------------
# svc_line LABEL UNIT... - ok when any of the units is active.
svc_line() {
	local label="$1" u
	shift
	for u in "$@"; do
		if systemctl is-active --quiet "$u" 2>/dev/null; then
			ui_ok "$label"
			return 0
		fi
	done
	ui_err "$label  ${UI_D}(not running: journalctl -u $1 -n 50)${UI_R}" 2>&1
}

print_summary() {
	local total=$(($(date +%s) - START_TIME)) port_suffix="" ip iface
	[ -n "$DETECTED_PORT" ] && [ "$DETECTED_PORT" != "80" ] && port_suffix=":${DETECTED_PORT}"

	printf '\n'
	if [ "$IS_UPGRADE" = "true" ]; then
		ui_ok "${UI_B}NivaroOS is updated${UI_R}  ${UI_D}$((total / 60))m $((total % 60))s${UI_R}"
	else
		ui_ok "${UI_B}NivaroOS is installed${UI_R}  ${UI_D}$((total / 60))m $((total % 60))s${UI_R}"
	fi

	ui_head "Open the dashboard"
	while read -r ip iface; do
		[ -n "$ip" ] && ui_kv "$iface" "http://${ip}${port_suffix}"
	done <<<"$(list_reachable_ips)"
	while read -r ip iface; do
		[ -n "$ip" ] && ui_kv "$iface" "http://${ip}${port_suffix}  ${UI_D}(VPN)${UI_R}"
	done <<<"$(list_vpn_ips)"
	ui_kv "local" "http://localhost${port_suffix}"
	[ "$IS_UPGRADE" = "true" ] || printf '    %sCreate your account on first visit.%s\n' "$UI_D" "$UI_R"

	ui_head "Services"
	svc_line "Core" nivaroos.service
	svc_line "Gateway" nivaroos-gateway.service
	svc_line "Message bus" nivaroos-message-bus.service
	svc_line "Apps" nivaroos-app-management.service
	svc_line "Storage" nivaroos-local-storage.service
	svc_line "Users" nivaroos-user-service.service
	svc_line "GPU" nivaroos-gpu-sidecar.service
	svc_line "Fan control" nivaroos-fans.service
	svc_line "Samba shares" smbd smb samba
	svc_line "mDNS discovery" avahi-daemon
	[ "$WITH_VM" = "yes" ] && svc_line "VM Manager" nivaroos-vm-sidecar.service
	if [ "$WITH_HOST_DESKTOP" = "yes" ]; then
		svc_line "Host Desktop" nivaroos-host-desktop.service
		printf '      %sA blank screen? Open it in the dashboard - it offers to set up a desktop.%s\n' "$UI_D" "$UI_R"
	fi
	[ "$WITH_DOWNLOAD_STATION" = "yes" ] && svc_line "Download Station" nivaroos-download-sidecar.service
	[ "$WITH_BACKUP" = "yes" ] && svc_line "Backup & Sync" nivaroos-backup.service

	ui_head "Next"
	ui_kv "manage" "nivaroos-cli --help"
	ui_kv "status" "sudo nivaroos-recover status"
	ui_kv "undo" "sudo nivaroos-rollback      (the previous build is kept)"
	ui_kv "update" "run the install command again"
	ui_kv "remove" "sudo nivaroos-uninstall"
	ui_kv "log" "$INSTALL_LOG"
	printf '\n'
}

# ------------------------------------------------------------------------------
# Main
# ------------------------------------------------------------------------------
# plan_steps prints the step functions this run will call, in order. The
# step count shown as [n/total] is this list's length.
plan_steps() {
	echo install_core_dependencies tune_system_limits check_docker clone_or_update_repo install_core_services install_fan_control install_samba
	[ "$WITH_VM" = "yes" ] && echo install_vm_manager
	[ "$WITH_HOST_DESKTOP" = "yes" ] && echo install_host_desktop
	[ "$WITH_DOWNLOAD_STATION" = "yes" ] && echo install_download_station
	[ "$WITH_BACKUP" = "yes" ] && echo install_backup
	echo install_ui start_core_services verify_health install_uninstall_wrapper install_safety_net install_mdns_advertisement
}

main() {
	START_TIME=$(date +%s)
	parse_args "$@"
	check_root "$@"
	ui_banner "installer" "Branch ${BRANCH} - log ${LATEST_LOG}"
	if [ -z "$DRY_RUN" ]; then
		take_lock
		init_logging
		log_raw "NivaroOS installer: branch ${BRANCH}, repo ${REPO_URL}, args: $*"
	fi
	preflight
	select_components

	local steps=() s
	read -r -a steps <<<"$(plan_steps | tr '\n' ' ')"
	TOTAL_STEPS=${#steps[@]}

	if [ -n "$DRY_RUN" ]; then
		ui_head "Plan (dry run - nothing is changed)"
		local n=0
		for s in "${steps[@]}"; do
			n=$((n + 1))
			ui_kv "$n/${TOTAL_STEPS}" "$s"
		done
		printf '\n'
		exit 0
	fi

	if [ "$IS_UPGRADE" = "true" ]; then
		ui_head "Updating"
	else
		ui_head "Installing"
	fi

	check_running_backups
	remove_backup_module
	pause_backup_service

	for s in "${steps[@]}"; do
		"$s"
	done

	print_summary
}

main "$@"
