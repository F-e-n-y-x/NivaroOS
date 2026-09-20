#!/usr/bin/env bash
# ==============================================================================
#  NivaroOS Installer & Updater Script
#  Modern Self-Hosted Personal Cloud & Container Platform
#  GitHub: https://github.com/F-e-n-y-x/NivaroOS
# ==============================================================================
#
# Supported Environments:
#   Debian 11+, Ubuntu 20.04+, Linux Mint, Pop!_OS, Raspberry Pi OS,
#   CentOS/RHEL/Rocky/AlmaLinux 8+, Fedora 38+, Arch Linux, openSUSE
#   (Alpine Linux: packages install fine, but service management is
#   systemd-only for now - see check_distro's warning)
#
# Quick Install / Update:
#   curl -fsSL https://raw.githubusercontent.com/F-e-n-y-x/NivaroOS/master/installer/install.sh | sudo bash
# ==============================================================================

# Ensure bash execution
if [ -z "${BASH_VERSION:-}" ]; then
	exec bash "$0" "$@"
fi

# -E (errtrace) is the fix for a silent-death class of bug: without it, a
# `trap ... ERR` is NOT inherited into shell functions, command
# substitutions, or subshells - it only fires for a failing command at the
# script's own top level. Nearly everything here (every step, every helper)
# runs inside a function, so any unexpected failure inside one would just
# exit immediately via `set -e` with the ERR trap never firing at all - no
# on_fatal_error message, nothing - which looks exactly like the installer
# quietly dying mid-run for no visible reason.
set -Eeuo pipefail
shopt -s checkwinsize 2>/dev/null || true

# ------------------------------------------------------------------------------
# Configuration & Constants
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
GO_VERSION="1.23.4"
MIN_RECOMMENDED_MEMORY_MB="1024"
MIN_REQUIRED_MEMORY_MB="384"
MIN_RECOMMENDED_DISK_GB="5"
MIN_REQUIRED_DISK_GB="1"

CUSTOM_PORT=""
DETECTED_PORT="80"
IS_UPGRADE="false"
WITH_VM=""
WITH_HOST_DESKTOP=""
YES=""
DEBUG=""
CLI_WIDTH=""
CLI_HEIGHT=""
BASE_STEPS=11
STEP_NUM=0
TOTAL_STEPS=$BASE_STEPS
CURRENT_STEP_TITLE=""
CURRENT_STEP_PID=""
IN_ALT_SCREEN="false"
START_TIME=0
DATE_TAG="$(date +'%Y%m%d-%H%M%S')"

LOG_DIR="/var/log/nivaroos"
INSTALL_LOG="${LOG_DIR}/install-${DATE_TAG}.log"
LATEST_LOG="${LOG_DIR}/install.log"
MANIFEST_FILE="/var/lib/nivaroos/manifest"

# ------------------------------------------------------------------------------
# Desktop Environment / Host Desktop Streaming State
#
# Host Desktop streaming works by pointing x11vnc at display :0. x11vnc can
# only ever capture an X11 session - it cannot see anything running under
# Wayland. A display manager being installed (gdm/lightdm/sddm) says nothing
# about whether the session it actually launches is X11 or Wayland, so all of
# this state exists to answer the real question ("will streaming work") and
# to fix it when the answer is no, instead of just checking "is a DE present"
# and hoping for the best.
# ------------------------------------------------------------------------------
KVM_AVAILABLE="no"
DE_SUPPORT_STATE=""        # supported | wayland_only | no_de
DETECTED_DE_NAME=""        # gnome | plasma | xfce | cinnamon | mate | lxqt | budgie | deepin | lxde | ""
DETECTED_XSESSION_NAME=""  # the .desktop id (no extension) of a usable X11 session, if any
DETECTED_DM="none"         # lightdm | gdm3 | gdm | sddm | xdm | none
DESKTOP_ACTION="none"      # none | ensure_x11_default | install_x11_companion | install_new_de_alongside | replace_de
DESKTOP_ENV_CHOICE=""      # xfce | cinnamon | mate
REPLACE_DESKTOP=""
DESKTOP_PROVISION_MARKER="/var/lib/nivaroos/provisioned-desktop"

# Checkbox-menu widget state (see checkbox_menu()) - deliberately global so a
# caller can populate them, invoke the widget, and read the results back.
CBM_LABELS=()
CBM_DESCS=()
CBM_STATE=()

export PATH="/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:$PATH"
export DEBIAN_FRONTEND=noninteractive
export NEEDRESTART_MODE=a

# ------------------------------------------------------------------------------
# Terminal & Color Formatting
# ------------------------------------------------------------------------------
IS_TTY="false"
if [ -t 1 ] && [ "${TERM:-}" != "dumb" ] && [ -z "${NO_COLOR:-}" ]; then
	IS_TTY="true"
fi

# Whether we can actually run an interactive prompt/menu. This is
# deliberately NOT "[ -t 0 ]" (is stdin a terminal) - the documented
# `curl -fsSL ... | sudo bash` install method pipes the script itself into
# bash, so stdin is always the curl pipe and always fails that check, even
# though the user is sitting at a real terminal watching stdout and could
# answer prompts just fine. Every prompt in this script already reads from
# /dev/tty directly for exactly this reason - so what actually matters is
# whether /dev/tty is there to read from and stdout is a real terminal to
# print the menu to, not what stdin happens to be connected to.
INTERACTIVE_TTY="false"
if [ "$IS_TTY" = "true" ] && [ -e /dev/tty ] && [ -r /dev/tty ] && [ -w /dev/tty ]; then
	INTERACTIVE_TTY="true"
fi

if [ "$IS_TTY" = "true" ]; then
	COLOR_RESET='\033[0m'
	COLOR_BOLD='\033[1m'
	COLOR_DIM='\033[2m'
	COLOR_CYAN='\033[38;5;51m'
	COLOR_BLUE='\033[38;5;39m'
	COLOR_GREEN='\033[38;5;48m'
	COLOR_YELLOW='\033[38;5;220m'
	COLOR_RED='\033[38;5;196m'
	COLOR_PURPLE='\033[38;5;141m'
	COLOR_MUTED='\033[38;5;244m'
	COLOR_WHITE='\033[38;5;255m'
else
	COLOR_RESET=''
	COLOR_BOLD=''
	COLOR_DIM=''
	COLOR_CYAN=''
	COLOR_BLUE=''
	COLOR_GREEN=''
	COLOR_YELLOW=''
	COLOR_RED=''
	COLOR_PURPLE=''
	COLOR_MUTED=''
	COLOR_WHITE=''
fi

SPINNER_FRAMES=("⠋" "⠙" "⠹" "⠸" "⠼" "⠴" "⠦" "⠧" "⠇" "⠏")

info()    { printf '%b\n' "${COLOR_CYAN}ℹ${COLOR_RESET}  ${COLOR_WHITE}$1${COLOR_RESET}"; }
success() { printf '%b\n' "${COLOR_GREEN}✔${COLOR_RESET}  ${COLOR_GREEN}$1${COLOR_RESET}"; }
warn()    { printf '%b\n' "${COLOR_YELLOW}⚠${COLOR_RESET}  ${COLOR_YELLOW}$1${COLOR_RESET}" >&2; }
error()   { printf '%b\n' "${COLOR_RED}✖${COLOR_RESET}  ${COLOR_RED}$1${COLOR_RESET}" >&2; }

# Initialize logging system
init_logging() {
	mkdir -p "$LOG_DIR" 2>/dev/null || true
	touch "$INSTALL_LOG" 2>/dev/null || true
	ln -sf "$INSTALL_LOG" "$LATEST_LOG" 2>/dev/null || true
}

log_raw() {
	if [ -w "$INSTALL_LOG" ]; then
		printf '[%s] %s\n' "$(date +'%Y-%m-%d %H:%M:%S')" "$*" >> "$INSTALL_LOG"
	fi
}


# Kills a process and every descendant of it. A step's command is often a
# pipeline or a multi-command chain running as children of the subshell
# run_step backgrounds - killing just that subshell's own PID does not stop
# them (bash does not forward a signal from a killed subshell to its own
# children), which left orphaned processes running after a cancelled step.
kill_tree() {
	local pid="$1"
	local sig="${2:-TERM}"
	local child children
	# A leaf process has no children, so `pgrep -P` exits 1 (its normal
	# "found nothing" status) - guard the substitution itself, not just
	# uses of its output, or the script's own ERR trap misfires here.
	children="$(pgrep -P "$pid" 2>/dev/null || true)"
	for child in $children; do
		kill_tree "$child" "$sig"
	done
	kill -"$sig" "$pid" 2>/dev/null || true
}

cleanup_on_exit() {
	if [ "$IN_ALT_SCREEN" = "true" ]; then
		printf "\033[?1049l"
		IN_ALT_SCREEN="false"
	fi
	if [ "$IS_TTY" = "true" ]; then
		printf "\033[?25h" # Restore cursor
	fi
}
trap cleanup_on_exit EXIT

# Previously, INT/TERM were routed through cleanup_on_exit too - which only
# restores the cursor and does not exit. Bash does not terminate a script on
# a trapped signal unless the handler says so, so pressing Ctrl+C (or the
# installer receiving SIGTERM) did *nothing visible*: the spinner kept
# running against a step whose background job might itself be gone, forever,
# with no message and no way to cancel short of killing the whole terminal/
# session from outside - which is indistinguishable from "the installer
# crashed silently". This handler actually stops the run, kills whatever
# step was still in flight, and says so.
handle_interrupt() {
	local sig="$1"
	if [ -n "$CURRENT_STEP_PID" ] && kill -0 "$CURRENT_STEP_PID" 2>/dev/null; then
		kill_tree "$CURRENT_STEP_PID" TERM
		sleep 0.3
		kill_tree "$CURRENT_STEP_PID" KILL
	fi
	if [ "$IN_ALT_SCREEN" = "true" ]; then
		printf "\033[?1049l"
		IN_ALT_SCREEN="false"
	fi
	if [ "$IS_TTY" = "true" ]; then
		printf "\033[?25h\n"
	else
		printf "\n"
	fi
	if [ -n "$CURRENT_STEP_TITLE" ]; then
		warn "Installation cancelled (${sig}) during step ${STEP_NUM}/${TOTAL_STEPS}: ${CURRENT_STEP_TITLE}"
	else
		warn "Installation cancelled (${sig})."
	fi
	info "Nothing further will run. Log so far: ${INSTALL_LOG}"
	info "Re-run this script to resume/retry - completed steps are safe to repeat."
	exit 130
}
trap 'handle_interrupt INT' INT
trap 'handle_interrupt TERM' TERM

strip_ansi() {
	printf '%b' "$1" | sed -E 's/\x1b\[[0-9;]*[a-zA-Z]//g; s/\033\[[0-9;]*[a-zA-Z]//g' | tr '\r\t' '  '
}

# ------------------------------------------------------------------------------
# Real-Time Terminal Dimension Detection & CPR Hardware Probing
# ------------------------------------------------------------------------------
TERM_COLS=80
TERM_ROWS=24

get_term_size() {
	if [ -n "${CLI_WIDTH:-}" ] && [ "$CLI_WIDTH" -ge 40 ] 2>/dev/null; then
		TERM_COLS="$CLI_WIDTH"
		TERM_ROWS="${CLI_HEIGHT:-24}"
		return
	fi
	if [ -n "${WIDTH:-}" ] && [ "$WIDTH" -ge 40 ] 2>/dev/null; then
		TERM_COLS="$WIDTH"
		TERM_ROWS="${HEIGHT:-24}"
		return
	fi

	local rows=0 cols=0

	# 1. Probe terminal emulator directly via ANSI CPR (Cursor Position Report)
	if [ "$IS_TTY" = "true" ] && [ -e /dev/tty ] && [ -r /dev/tty ] && [ -w /dev/tty ]; then
		if command -v stty >/dev/null 2>&1; then
			local old_stty
			old_stty="$(stty -g </dev/tty 2>/dev/null || true)"
			if [ -n "$old_stty" ]; then
				stty raw -echo min 0 time 0 </dev/tty 2>/dev/null || true
				printf "\0337\033[9999;9999H\033[6n\0338" >/dev/tty 2>/dev/null || true
				local resp=""
				read -r -t 0.08 -d 'R' resp </dev/tty 2>/dev/null || true
				stty "$old_stty" </dev/tty 2>/dev/null || true
				if [[ "$resp" =~ \[([0-9]+)\;([0-9]+) ]]; then
					rows="${BASH_REMATCH[1]}"
					cols="${BASH_REMATCH[2]}"
				fi
			fi
		fi
	fi

	# 2. Fallback to stty size on /dev/tty
	if [ -z "$cols" ] || [ "$cols" -le 0 ] 2>/dev/null; then
		if [ -e /dev/tty ]; then
			local stty_out
			stty_out="$(stty size </dev/tty 2>/dev/null || true)"
			if [ -n "$stty_out" ]; then
				rows="$(echo "$stty_out" | awk '{print $1}')"
				cols="$(echo "$stty_out" | awk '{print $2}')"
			fi
		fi
	fi

	# 3. Fallback to stty size on stdin
	if [ -z "$cols" ] || [ "$cols" -le 0 ] 2>/dev/null; then
		if [ -t 0 ] || [ -t 1 ]; then
			local stty_out
			stty_out="$(stty size 2>/dev/null || true)"
			if [ -n "$stty_out" ]; then
				rows="$(echo "$stty_out" | awk '{print $1}')"
				cols="$(echo "$stty_out" | awk '{print $2}')"
			fi
		fi
	fi

	# 4. Fallback to tput
	if [ -z "$cols" ] || [ "$cols" -le 0 ] 2>/dev/null; then
		if command -v tput >/dev/null 2>&1; then
			cols="$(tput cols 2>/dev/null || true)"
			rows="$(tput lines 2>/dev/null || true)"
		fi
	fi

	# 5. Fallback to environment variables
	if [ -z "$cols" ] || [ "$cols" -le 0 ] 2>/dev/null; then
		cols="${COLUMNS:-80}"
		rows="${LINES:-24}"
	fi

	if [ "$cols" -lt 40 ] 2>/dev/null; then cols=80; fi
	if [ "$rows" -lt 10 ] 2>/dev/null; then rows=24; fi

	# Sync kernel tty driver if CPR found a larger width
	if [ "$cols" -gt 0 ] && [ "$rows" -gt 0 ] && [ -e /dev/tty ]; then
		stty rows "$rows" cols "$cols" </dev/tty 2>/dev/null || true
	fi

	TERM_COLS="$cols"
	TERM_ROWS="$rows"
}

get_terminal_width() {
	get_term_size
	echo "$TERM_COLS"
}

get_terminal_height() {
	get_term_size
	echo "$TERM_ROWS"
}

# ------------------------------------------------------------------------------
# Banner & Diagnostics Display
# ------------------------------------------------------------------------------
print_banner() {
	if [ "$IS_TTY" = "true" ] && [ -z "${NO_CLEAR:-}" ]; then
		clear 2>/dev/null || true
	fi
	printf "\n"
	printf '%b' "${COLOR_BOLD}${COLOR_CYAN}"
	cat <<'EOF'
    _   _ _                         ___  ____
   | \ | (_)_   ____ _ _ __ ___    / _ \/ ___|
   |  \| | \ \ / / _` | '__/ _ \  | | | \___ \
   | |\  | |\ V / (_| | | | (_) | | |_| |___) |
   |_| \_|_| \_/ \__,_|_|  \___/   \___/|____/
EOF
	printf '%b\n' "${COLOR_RESET}"
	printf '%b\n\n' "   ${COLOR_PURPLE}✦${COLOR_RESET} ${COLOR_BOLD}Modern Self-Hosted Personal Cloud & Container Platform${COLOR_RESET} ${COLOR_PURPLE}✦${COLOR_RESET}"
}

print_diagnostics_card() {
	get_term_size
	local box_width=$((TERM_COLS - 2))
	if [ "$box_width" -lt 40 ]; then box_width=40; fi
	local inner_width=$((box_width - 6))

	local os_name="Linux"
	if [ -f "$OS_RELEASE_FILE" ]; then
		# shellcheck disable=SC1090
		. "$OS_RELEASE_FILE"
		os_name="${PRETTY_NAME:-$ID}"
	fi

	local arch kernel
	arch="$(uname -m)"
	kernel="$(uname -r)"

	local mem_mb="0" mem_gb_str="Unknown"
	if [ -f /proc/meminfo ]; then
		mem_mb="$(awk '/MemTotal:/ { print int($2/1024) }' /proc/meminfo 2>/dev/null || echo "0")"
	fi
	if [ -z "$mem_mb" ] || [ "$mem_mb" -eq 0 ]; then
		mem_mb="$(LC_ALL=C free -m 2>/dev/null | awk '/^Mem:/ { print $2 }' || echo "0")"
	fi
	if [ -n "$mem_mb" ] && [ "$mem_mb" -gt 0 ]; then
		mem_gb_str="$(awk "BEGIN {printf \"%.1f GB\", $mem_mb/1024}")"
	fi

	local disk_gb_str="Unknown" disk_mb
	disk_mb="$(LC_ALL=C df -m / 2>/dev/null | tail -n 1 | awk '{print $4}' || echo "0")"
	if [ -n "$disk_mb" ] && [ "$disk_mb" -gt 0 ]; then
		disk_gb_str="$(awk "BEGIN {printf \"%.1f GB\", $disk_mb/1024}")"
	fi

	local kvm_status="Supported (KVM Acceleration Available)"
	if [ ! -e /dev/kvm ]; then
		kvm_status="Not Detected (Emulation Only)"
	fi

	local docker_status="Not Installed (Auto-installs during setup)"
	if command -v docker >/dev/null 2>&1; then
		local d_ver
		d_ver="$(docker version --format '{{.Server.Version}}' 2>/dev/null || docker -v 2>/dev/null | awk '{print $3}' | tr -d ',' || echo 'installed')"
		docker_status="Installed (v${d_ver})"
	fi

	local title_tag=" System Diagnostics "
	local top_dashes_len=$((box_width - ${#title_tag} - 4))
	if [ "$top_dashes_len" -lt 2 ]; then top_dashes_len=2; fi
	local top_dashes=""
	for ((d=0; d<top_dashes_len; d++)); do top_dashes+="─"; done

	local bot_dashes=""
	for ((d=0; d<box_width-2; d++)); do bot_dashes+="─"; done

	printf '%b\n' "${COLOR_MUTED}╭──${COLOR_BOLD}${COLOR_WHITE}${title_tag}${COLOR_RESET}${COLOR_MUTED}${top_dashes}╮${COLOR_RESET}"

	render_diag_line() {
		local label="$1" val="$2"
		local prefix="• ${label}: "
		# The old version computed a truncated preview just to size the
		# padding, then printed the ORIGINAL untruncated label/value anyway -
		# so a long value (a long docker version string, a long distro
		# name) sailed straight past the box's right border instead of
		# actually being cut to fit, breaking the border exactly like the
		# "Docker Engine" line did.
		local avail=$((inner_width - ${#prefix}))
		if [ "$avail" -lt 1 ]; then avail=1; fi
		if [ "${#val}" -gt "$avail" ]; then
			if [ "$avail" -gt 1 ]; then
				val="${val:0:$((avail - 1))}…"
			else
				val="${val:0:$avail}"
			fi
		fi
		local clean_text="${prefix}${val}"
		local pad_len=$((inner_width - ${#clean_text}))
		local pad=""
		if [ "$pad_len" -gt 0 ]; then
			pad="$(printf '%*s' "$pad_len" '')"
		fi
		printf '%b\n' "${COLOR_MUTED}│${COLOR_RESET}  ${COLOR_CYAN}•${COLOR_RESET} ${COLOR_MUTED}${label}:${COLOR_RESET} ${COLOR_WHITE}${val}${COLOR_RESET}${pad}  ${COLOR_MUTED}│${COLOR_RESET}"
	}

	render_diag_line "Operating System" "${os_name} (${arch})"
	render_diag_line "Linux Kernel    " "${kernel}"
	render_diag_line "System Memory   " "${mem_gb_str}"
	render_diag_line "Free Disk on /  " "${disk_gb_str}"
	render_diag_line "Virtualization  " "${kvm_status}"
	render_diag_line "Docker Engine   " "${docker_status}"

	printf '%b\n\n' "${COLOR_MUTED}╰${bot_dashes}╯${COLOR_RESET}"
}

# ------------------------------------------------------------------------------
# Pre-flight Checks & Elevation
# ------------------------------------------------------------------------------
check_root() {
	if [ "$(id -u)" -ne 0 ]; then
		if [ ! -f "$0" ]; then
			error "NivaroOS installer requires root privileges. Please run with sudo:"
			printf '%b\n' "   ${COLOR_CYAN}curl -fsSL https://raw.githubusercontent.com/F-e-n-y-x/NivaroOS/master/installer/install.sh | sudo bash${COLOR_RESET}\n"
			exit 1
		fi
		if command -v sudo >/dev/null 2>&1; then
			info "Root privileges required. Elevating with sudo..."
			exec sudo -E bash "$0" "$@"
		else
			error "NivaroOS installer must be run as root."
			printf '%b\n' "   ${COLOR_MUTED}Please login as root or install sudo.${COLOR_RESET}"
			exit 1
		fi
	fi
}

check_distro() {
	if [ ! -f "$OS_RELEASE_FILE" ]; then
		warn "$OS_RELEASE_FILE not found, distribution family cannot be verified."
		return 0
	fi
	# shellcheck disable=SC1090
	. "$OS_RELEASE_FILE"
	local family="${ID:-} ${ID_LIKE:-}"
	case " $family " in
		*" debian "*|*" ubuntu "*|*" raspbian "*|*" armbian "*|*" pop "*|*" mint "*|*" zorin "*|*" kali "*) ;;
		*" rhel "*|*" centos "*|*" fedora "*|*" rocky "*|*" almalinux "*|*" amzn "*) ;;
		*" arch "*|*" manjaro "*|*" endeavouros "*) ;;
		*" opensuse "*|*" sles "*) ;;
		*" alpine "*)
			warn "Alpine Linux uses OpenRC, not systemd - this installer only knows how to create/enable/health-check systemd services, so NivaroOS's services will need to be started and supervised manually after this script finishes. Installation will continue, but expect to see 'inactive' services until you set that up yourself."
			;;
		*)
			warn "Distribution '${ID:-unknown}' has not been fully validated, but installation will continue."
			;;
	esac
}

check_resources() {
	local mem_mb="0" disk_gb
	if [ -f /proc/meminfo ]; then
		mem_mb="$(awk '/MemTotal:/ { print int($2/1024) }' /proc/meminfo 2>/dev/null || echo "0")"
	fi
	if [ -z "$mem_mb" ] || [ "$mem_mb" -eq 0 ]; then
		mem_mb="$(LC_ALL=C free -m 2>/dev/null | awk '/^Mem:/ { print $2 }' || echo "0")"
	fi

	local disk_kb
	disk_kb="$(LC_ALL=C df -P / 2>/dev/null | tail -n 1 | awk '{print $4}')"
	if [ -n "$disk_kb" ] && [ "$disk_kb" -eq "$disk_kb" ] 2>/dev/null; then
		disk_gb=$((disk_kb / 1024 / 1024))
	else
		disk_gb=0
	fi

	if [ -n "$mem_mb" ] && [ "$mem_mb" -gt 0 ]; then
		if [ "$mem_mb" -lt "$MIN_REQUIRED_MEMORY_MB" ]; then
			error "Only ${mem_mb}MB of memory detected - NivaroOS requires at least ${MIN_REQUIRED_MEMORY_MB}MB to install."
			exit 1
		elif [ "$mem_mb" -lt "$MIN_RECOMMENDED_MEMORY_MB" ]; then
			warn "Only ${mem_mb}MB of memory detected - ${MIN_RECOMMENDED_MEMORY_MB}MB+ is recommended for optimal performance."
		fi
	fi
	if [ -n "$disk_gb" ] && [ "$disk_gb" -gt 0 ]; then
		if [ "$disk_gb" -lt "$MIN_REQUIRED_DISK_GB" ]; then
			error "Only ${disk_gb}GB of free disk space on / - NivaroOS requires at least ${MIN_REQUIRED_DISK_GB}GB."
			exit 1
		elif [ "$disk_gb" -lt "$MIN_RECOMMENDED_DISK_GB" ]; then
			warn "Only ${disk_gb}GB of free disk space on / - ${MIN_RECOMMENDED_DISK_GB}GB+ is recommended."
		fi
	fi
}

# ------------------------------------------------------------------------------
# Port Conflict & Existing Installation Detection
# ------------------------------------------------------------------------------
is_port_in_use() {
	local port="$1"
	if command -v ss >/dev/null 2>&1; then
		ss -tuln 2>/dev/null | grep -qE ":${port}\s" && return 0
	elif command -v netstat >/dev/null 2>&1; then
		netstat -tuln 2>/dev/null | grep -qE ":${port}\s" && return 0
	elif command -v lsof >/dev/null 2>&1; then
		lsof -iTCP:"${port}" -sTCP:LISTEN >/dev/null 2>&1 && return 0
	fi
	return 1
}

find_process_on_port() {
	local port="$1"
	local proc="unknown"
	if command -v ss >/dev/null 2>&1; then
		proc="$(ss -tulnp 2>/dev/null | grep -E ":${port}\s" | awk '{print $NF}' | head -1 || echo "")"
	elif command -v lsof >/dev/null 2>&1; then
		proc="$(lsof -iTCP:"${port}" -sTCP:LISTEN -F c 2>/dev/null | sed 's/^c//' | head -1 || echo "")"
	fi
	echo "$proc"
}

resolve_port_conflict() {
	# Check if existing NivaroOS configuration exists. This is checked
	# independent of whatever port is actually requested/free right now -
	# otherwise re-running with a different --port than a prior install
	# used (which is now sitting free, since the old install owns its own
	# port) would misclassify a genuine upgrade as a fresh install below.
	if [ -f /etc/nivaroos/gateway.ini ]; then
		IS_UPGRADE="true"
		local saved_port
		saved_port="$(awk -F '=' '/^[[:space:]]*port[[:space:]]*=/ {gsub(/[[:space:]]/, "", $2); print $2}' /etc/nivaroos/gateway.ini 2>/dev/null || echo "")"
		if [ -n "$saved_port" ]; then
			DETECTED_PORT="$saved_port"
		fi
	fi

	local target_port="${CUSTOM_PORT:-$DETECTED_PORT}"

	# Check if port is in use
	if is_port_in_use "$target_port"; then
		local conflict_proc
		conflict_proc="$(find_process_on_port "$target_port")"

		# If port is in use by NivaroOS itself, this is an upgrade/reinstall
		if [[ "$conflict_proc" =~ nivaroos || "$conflict_proc" =~ casaos ]]; then
			IS_UPGRADE="true"
			DETECTED_PORT="$target_port"
			info "Existing NivaroOS installation detected on Port ${target_port}. Performing in-place upgrade..."
			return 0
		fi

		# If in use by another third-party process
		local alt_port=8080
		while [ "$alt_port" -le 65535 ]; do
			if ! is_port_in_use "$alt_port"; then
				break
			fi
			alt_port=$((alt_port + 1))
		done

		if [ -n "$CUSTOM_PORT" ]; then
			warn "Requested port ${CUSTOM_PORT} is already bound by ${conflict_proc}."
			if [ -n "$YES" ] || [ "$INTERACTIVE_TTY" != "true" ]; then
				DETECTED_PORT="$CUSTOM_PORT"
				return 0
			fi
		else
			warn "Port 80 is currently in use by ${conflict_proc}."
		fi

		if [ -n "$YES" ] || [ "$INTERACTIVE_TTY" != "true" ]; then
			info "Non-interactive mode: Automatically assigning free port ${alt_port}."
			DETECTED_PORT="$alt_port"
			return 0
		fi

		printf '%b\n' "  ${COLOR_PURPLE}◆${COLOR_RESET} Port 80 is occupied. You can use available port ${COLOR_CYAN}${alt_port}${COLOR_RESET} or specify a custom port."
		local user_port=""
		printf '%b' "  ${COLOR_CYAN}?${COLOR_RESET} ${COLOR_BOLD}HTTP Dashboard Port [${alt_port}]:${COLOR_RESET} "
		read -r user_port </dev/tty || user_port=""
		if [ -z "$user_port" ]; then
			DETECTED_PORT="$alt_port"
		else
			DETECTED_PORT="$user_port"
		fi
		printf '%b\n\n' "  ${COLOR_GREEN}✔${COLOR_RESET} Selected Dashboard Port: ${COLOR_BOLD}${DETECTED_PORT}${COLOR_RESET}"
	else
		DETECTED_PORT="$target_port"
	fi
}

# ------------------------------------------------------------------------------
# CLI Arguments & Options
# ------------------------------------------------------------------------------
parse_args() {
	while [ $# -gt 0 ]; do
		case "$1" in
			--with-vm) WITH_VM=yes ;;
			--without-vm) WITH_VM=no ;;
			--with-host-desktop) WITH_HOST_DESKTOP=yes ;;
			--without-host-desktop) WITH_HOST_DESKTOP=no ;;
			--desktop-environment=*) DESKTOP_ENV_CHOICE="${1#*=}" ;;
			--desktop-environment)
				shift
				DESKTOP_ENV_CHOICE="${1:-}"
				;;
			--replace-desktop) REPLACE_DESKTOP=yes ;;
			--port=*) CUSTOM_PORT="${1#*=}" ;;
			--port)
				shift
				CUSTOM_PORT="${1:-}"
				;;
			--width=*) CLI_WIDTH="${1#*=}" ;;
			--width|-w)
				shift
				CLI_WIDTH="${1:-}"
				;;
			--branch=*) BRANCH="${1#*=}" ;;
			--branch|-b)
				shift
				BRANCH="${1:-master}"
				;;
			--repo=*) REPO_URL="${1#*=}" ;;
			--yes|-y|--unattended) YES=yes ;;
			--debug) DEBUG=yes ;;
			--help|-h)
				print_banner
				printf '%b\n' "${COLOR_BOLD}Usage:${COLOR_RESET} install.sh [options]\n"
				printf '%b\n' "${COLOR_BOLD}Options:${COLOR_RESET}"
				printf '%b\n' "  ${COLOR_CYAN}-y, --yes${COLOR_RESET}                    Automatic non-interactive installation (accept all defaults, skips the selection menu)"
				printf '%b\n' "  ${COLOR_CYAN}--with-vm${COLOR_RESET}                    Install VM Manager with QEMU/KVM, libvirt & web console"
				printf '%b\n' "  ${COLOR_CYAN}--without-vm${COLOR_RESET}                 Skip VM Manager installation (can be enabled later via CLI)"
				printf '%b\n' "  ${COLOR_CYAN}--with-host-desktop${COLOR_RESET}          Stream this machine's own desktop over VNC (requires VM Manager)"
				printf '%b\n' "  ${COLOR_CYAN}--without-host-desktop${COLOR_RESET}       Skip Host Desktop streaming installation"
				printf '%b\n' "  ${COLOR_CYAN}--desktop-environment <de>${COLOR_RESET}   Desktop to install/pair for Host Desktop: xfce, cinnamon, or mate (default: xfce)"
				printf '%b\n' "  ${COLOR_CYAN}--replace-desktop${COLOR_RESET}            Allow replacing an existing Wayland-only desktop instead of installing alongside it (destructive)"
				printf '%b\n' "  ${COLOR_CYAN}--port <port>${COLOR_RESET}                Custom HTTP dashboard port (default: 80 or next free port)"
				printf '%b\n' "  ${COLOR_CYAN}--width <cols>${COLOR_RESET}               Force specific terminal box width (default: auto-detect)"
				printf '%b\n' "  ${COLOR_CYAN}--branch <branch>${COLOR_RESET}            Git branch or tag to install (default: master)"
				printf '%b\n' "  ${COLOR_CYAN}--debug${COLOR_RESET}                      Show detailed verbose logs during installation"
				printf '%b\n' "  ${COLOR_CYAN}-h, --help${COLOR_RESET}                   Display this help message and exit"
				printf '\n'
				exit 0
				;;
			*)
				error "Unknown argument '$1'. Run with --help to see available options."
				exit 1
				;;
		esac
		# The two-token flags above (--port, --width/-w, --branch/-b,
		# --desktop-environment) already shift once themselves to consume
		# their value - if that value was the last argument on the command
		# line, $# is already 0 here, and an unconditional shift would fail
		# ("shift count out of range"), which set -e turns into the whole
		# installer aborting over a missing flag value instead of just
		# falling back to its default.
		[ $# -eq 0 ] || shift
	done
}

# ------------------------------------------------------------------------------
# Interactive Checkbox Multi-Select Widget
#
# Populate the global CBM_LABELS/CBM_DESCS/CBM_STATE arrays (same length,
# CBM_STATE holding "0"/"1"), then call checkbox_menu "<title>". On return,
# CBM_STATE holds the user's final choices. Space toggles, up/down (or j/k)
# moves, Enter confirms, Ctrl+C cancels the whole installation cleanly.
# Only ever call this when already known to be an interactive TTY - callers
# are responsible for the "$YES"/"$INTERACTIVE_TTY" non-interactive fallback.
# ------------------------------------------------------------------------------
checkbox_menu() {
	local title="$1"
	local n=${#CBM_LABELS[@]}
	local cur=0
	local key="" rest=""
	local old_stty=""
	old_stty="$(stty -g </dev/tty 2>/dev/null || true)"
	stty raw -echo </dev/tty 2>/dev/null || true
	printf "\033[?25l"

	local first_draw=true
	local drawn_lines=0
	local i out

	while true; do
		out=""
		out+="\r\n  ${COLOR_BOLD}${COLOR_WHITE}${title}${COLOR_RESET}\r\n"
		out+="  ${COLOR_MUTED}up/down or j/k move   space toggle   enter confirm${COLOR_RESET}\r\n\r\n"
		for ((i=0; i<n; i++)); do
			local box="[ ]"
			local label_color="${COLOR_WHITE}"
			if [ "${CBM_STATE[$i]}" = "1" ]; then
				box="[x]"
				label_color="${COLOR_GREEN}"
			fi
			local pointer="   "
			if [ "$i" -eq "$cur" ]; then
				pointer="${COLOR_CYAN} > ${COLOR_RESET}"
			fi
			out+="  ${pointer}${COLOR_BOLD}${box}${COLOR_RESET} ${label_color}${CBM_LABELS[$i]}${COLOR_RESET}\r\n"
			if [ -n "${CBM_DESCS[$i]:-}" ]; then
				out+="      ${COLOR_MUTED}${CBM_DESCS[$i]}${COLOR_RESET}\r\n"
			fi
		done

		if [ "$first_draw" = "false" ]; then
			printf "\033[%dA" "$drawn_lines"
		fi
		first_draw=false
		printf '%b' "$out"
		drawn_lines="$(printf '%b' "$out" | wc -l)"

		key=""
		IFS= read -rsn1 key </dev/tty || true
		if [ "$key" = "$(printf '\033')" ]; then
			rest=""
			IFS= read -rsn2 -t 0.01 rest </dev/tty || true
			key="${key}${rest}"
		fi

		case "$key" in
			$'\033[A'|k|K) cur=$(( (cur - 1 + n) % n )) ;;
			$'\033[B'|j|J) cur=$(( (cur + 1) % n )) ;;
			' ')
				if [ "${CBM_STATE[$cur]}" = "1" ]; then
					CBM_STATE[$cur]=0
				else
					CBM_STATE[$cur]=1
				fi
				;;
			"$(printf '\003')")
				stty "$old_stty" </dev/tty 2>/dev/null || true
				printf "\033[?25h\n"
				warn "Installation cancelled."
				exit 130
				;;
			"")
				break
				;;
			*) : ;;
		esac
	done

	printf "\033[?25h"
	if [ -n "$old_stty" ]; then
		stty "$old_stty" </dev/tty 2>/dev/null || true
	fi
	printf "\r\n"
}

# ------------------------------------------------------------------------------
# Desktop Environment / X11 Support Detection
# ------------------------------------------------------------------------------

# Populates DETECTED_DM, DETECTED_DE_NAME, DETECTED_XSESSION_NAME and
# DE_SUPPORT_STATE. Safe to call repeatedly (e.g. re-run after provisioning
# to see whether it actually fixed things).
detect_display_server_support() {
	DETECTED_DM="none"
	local dm
	for dm in lightdm gdm3 gdm sddm xdm; do
		if systemctl list-unit-files 2>/dev/null | grep -q "^${dm}\.service"; then
			DETECTED_DM="$dm"
			break
		fi
	done

	DETECTED_DE_NAME=""
	DETECTED_XSESSION_NAME=""
	DE_SUPPORT_STATE="no_de"

	if [ "$DETECTED_DM" = "none" ] && [ ! -d /usr/share/xsessions ] && [ ! -d /usr/share/wayland-sessions ]; then
		return
	fi

	# Prefer whatever is actually running right now (the strongest signal
	# for "this is the desktop in real use"), falling back to whatever
	# session files happen to be installed if nobody is logged in yet.
	local de_map=(
		"gnome-shell:gnome" "plasmashell:plasma" "xfce4-session:xfce"
		"cinnamon:cinnamon" "cinnamon-session:cinnamon" "mate-session:mate"
		"lxqt-session:lxqt" "budgie-wm:budgie" "budgie-panel:budgie"
		"deepin-session:deepin" "lxsession:lxde"
	)
	local m proc_name de_id
	for m in "${de_map[@]}"; do
		proc_name="${m%%:*}"
		de_id="${m##*:}"
		if pgrep -x "$proc_name" >/dev/null 2>&1; then
			DETECTED_DE_NAME="$de_id"
			break
		fi
	done

	if [ -z "$DETECTED_DE_NAME" ] && [ -d /usr/share/xsessions ]; then
		local f base
		for f in /usr/share/xsessions/*.desktop; do
			[ -e "$f" ] || continue
			base="$(basename "$f" .desktop)"
			case "$base" in
				gnome*) DETECTED_DE_NAME="gnome" ;;
				plasma*|kde*) DETECTED_DE_NAME="plasma" ;;
				xfce*) DETECTED_DE_NAME="xfce" ;;
				cinnamon*) DETECTED_DE_NAME="cinnamon" ;;
				mate*) DETECTED_DE_NAME="mate" ;;
				lxqt*) DETECTED_DE_NAME="lxqt" ;;
				budgie*) DETECTED_DE_NAME="budgie" ;;
				deepin*) DETECTED_DE_NAME="deepin" ;;
				[Ll][Xx][Dd][Ee]*) DETECTED_DE_NAME="lxde" ;;
			esac
			[ -n "$DETECTED_DE_NAME" ] && break
		done
	fi

	if [ -z "$DETECTED_DE_NAME" ] && [ -d /usr/share/wayland-sessions ]; then
		local f base
		for f in /usr/share/wayland-sessions/*.desktop; do
			[ -e "$f" ] || continue
			base="$(basename "$f" .desktop)"
			case "$base" in
				gnome*) DETECTED_DE_NAME="gnome" ;;
				plasma*|kde*) DETECTED_DE_NAME="plasma" ;;
				*) DETECTED_DE_NAME="$base" ;;
			esac
			[ -n "$DETECTED_DE_NAME" ] && break
		done
	fi

	if [ -z "$DETECTED_DE_NAME" ] && [ "$DETECTED_DM" = "none" ]; then
		DE_SUPPORT_STATE="no_de"
		return
	fi

	# x11vnc (what Host Desktop streaming actually runs) can only capture an
	# X11 session, never Wayland - so finding *a* xsessions entry that
	# genuinely matches the detected desktop is the real test, not just
	# "a display manager service exists".
	if [ -d /usr/share/xsessions ]; then
		local f base
		for f in /usr/share/xsessions/*.desktop; do
			[ -e "$f" ] || continue
			base="$(basename "$f" .desktop)"
			case "$base" in
				*wayland*) continue ;;
			esac
			case "${DETECTED_DE_NAME}:${base}" in
				gnome:gnome*|plasma:plasma*|plasma:kde*|xfce:xfce*|cinnamon:cinnamon*|mate:mate*|lxqt:lxqt*|budgie:budgie*|deepin:deepin*|lxde:[Ll][Xx][Dd][Ee]*)
					DETECTED_XSESSION_NAME="$base"
					break
					;;
			esac
		done
		# Detected DE didn't map to a known id but at least one non-Wayland
		# xsession exists anyway - trust that over guessing wrong.
		if [ -z "$DETECTED_XSESSION_NAME" ]; then
			for f in /usr/share/xsessions/*.desktop; do
				[ -e "$f" ] || continue
				base="$(basename "$f" .desktop)"
				case "$base" in *wayland*) continue ;; esac
				DETECTED_XSESSION_NAME="$base"
				break
			done
		fi
	fi

	if [ -n "$DETECTED_XSESSION_NAME" ]; then
		DE_SUPPORT_STATE="supported"
	elif [ -n "$DETECTED_DE_NAME" ]; then
		DE_SUPPORT_STATE="wayland_only"
	else
		DE_SUPPORT_STATE="no_de"
	fi
}

# ------------------------------------------------------------------------------
# Desktop Environment / X11 Provisioning Helpers
# ------------------------------------------------------------------------------
ensure_apt_universe_enabled() {
	if ! command -v apt-get >/dev/null 2>&1; then return 0; fi
	if ! command -v add-apt-repository >/dev/null 2>&1; then
		apt-get install -y software-properties-common >/dev/null 2>&1 || true
	fi
	if command -v add-apt-repository >/dev/null 2>&1; then
		add-apt-repository -y universe >/dev/null 2>&1 || true
	fi
	pkg_update
}

pkg_install_xorg_stack() {
	if command -v apt-get >/dev/null 2>&1; then
		pkg_install xserver-xorg xinit
	elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
		pkg_install xorg-x11-server-Xorg xorg-x11-xinit
	elif command -v pacman >/dev/null 2>&1; then
		pkg_install xorg-server xorg-xinit
	elif command -v zypper >/dev/null 2>&1; then
		pkg_install xorg-x11-server
	elif command -v apk >/dev/null 2>&1; then
		pkg_install xorg-server xinit
	fi
}

pkg_install_display_manager() {
	pkg_install lightdm lightdm-gtk-greeter
}

# Adds a real X11 session to a DE that currently only offers Wayland,
# without touching anything else about the existing install.
pkg_install_x11_companion() {
	local de="$1"
	case "$de" in
		gnome)
			if command -v apt-get >/dev/null 2>&1; then
				pkg_install xserver-xorg gnome-session
			elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
				pkg_install xorg-x11-server-Xorg gnome-session-xsession
			elif command -v pacman >/dev/null 2>&1; then
				pkg_install xorg-server gnome-session
			elif command -v zypper >/dev/null 2>&1; then
				pkg_install xorg-x11-server gnome-session
			fi
			;;
		plasma)
			if command -v apt-get >/dev/null 2>&1; then
				pkg_install xserver-xorg plasma-workspace-x11
			elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
				pkg_install xorg-x11-server-Xorg plasma-workspace-x11
			elif command -v pacman >/dev/null 2>&1; then
				pkg_install xorg-server plasma-workspace
			elif command -v zypper >/dev/null 2>&1; then
				pkg_install xorg-x11-server
			fi
			;;
		*)
			return 1
			;;
	esac
}

pkg_install_de() {
	local de="$1"
	case "$de" in
		xfce)
			if command -v apt-get >/dev/null 2>&1; then
				pkg_install xfce4 xfce4-terminal
			elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
				dnf group install -y "Xfce Desktop" >/dev/null 2>&1 || dnf install -y @xfce-desktop-environment >/dev/null 2>&1 || pkg_install xfce4-session xfce4-panel xfdesktop xfce4-settings xfce4-terminal
			elif command -v pacman >/dev/null 2>&1; then
				pkg_install xfce4 xfce4-goodies
			elif command -v zypper >/dev/null 2>&1; then
				zypper install -y -t pattern xfce >/dev/null 2>&1 || pkg_install xfce4-session
			elif command -v apk >/dev/null 2>&1; then
				pkg_install xfce4 xfce4-terminal
			fi
			;;
		cinnamon)
			if command -v apt-get >/dev/null 2>&1; then
				pkg_install cinnamon-core || pkg_install cinnamon
			elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
				dnf group install -y "Cinnamon Desktop" >/dev/null 2>&1 || dnf install -y @cinnamon-desktop-environment >/dev/null 2>&1 || pkg_install cinnamon-desktop
			elif command -v pacman >/dev/null 2>&1; then
				pkg_install cinnamon
			elif command -v zypper >/dev/null 2>&1; then
				zypper install -y -t pattern cinnamon >/dev/null 2>&1 || pkg_install cinnamon
			elif command -v apk >/dev/null 2>&1; then
				pkg_install cinnamon
			fi
			;;
		mate)
			if command -v apt-get >/dev/null 2>&1; then
				pkg_install mate-desktop-environment-core || pkg_install mate-desktop-environment
			elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
				dnf group install -y "MATE Desktop" >/dev/null 2>&1 || dnf install -y @mate-desktop-environment >/dev/null 2>&1 || pkg_install mate-desktop
			elif command -v pacman >/dev/null 2>&1; then
				pkg_install mate mate-extra
			elif command -v zypper >/dev/null 2>&1; then
				zypper install -y -t pattern mate >/dev/null 2>&1 || pkg_install mate
			elif command -v apk >/dev/null 2>&1; then
				pkg_install mate-desktop
			fi
			;;
	esac
}

# Best-effort removal of an existing desktop, only ever invoked after the
# operator has explicitly, interactively confirmed replacement (see
# resolve_desktop_environment_support). Never called from a non-interactive
# / --yes run.
remove_desktop_environment() {
	local de="$1"
	case "$de" in
		gnome)
			if command -v apt-get >/dev/null 2>&1; then
				apt-get purge -y gnome-shell gnome-session ubuntu-desktop gdm3 gdm >/dev/null 2>&1 || true
			elif command -v dnf >/dev/null 2>&1; then
				dnf group remove -y "GNOME Desktop" >/dev/null 2>&1 || true
			elif command -v pacman >/dev/null 2>&1; then
				pacman -Rns --noconfirm gnome gdm >/dev/null 2>&1 || true
			fi
			;;
		plasma)
			if command -v apt-get >/dev/null 2>&1; then
				apt-get purge -y plasma-desktop sddm >/dev/null 2>&1 || true
			elif command -v dnf >/dev/null 2>&1; then
				dnf group remove -y "KDE Plasma Workspaces" >/dev/null 2>&1 || true
			elif command -v pacman >/dev/null 2>&1; then
				pacman -Rns --noconfirm plasma sddm >/dev/null 2>&1 || true
			fi
			;;
		xfce)
			if command -v apt-get >/dev/null 2>&1; then
				apt-get purge -y xfce4 >/dev/null 2>&1 || true
			elif command -v pacman >/dev/null 2>&1; then
				pacman -Rns --noconfirm xfce4 >/dev/null 2>&1 || true
			fi
			;;
		cinnamon)
			if command -v apt-get >/dev/null 2>&1; then
				apt-get purge -y cinnamon-core cinnamon >/dev/null 2>&1 || true
			elif command -v pacman >/dev/null 2>&1; then
				pacman -Rns --noconfirm cinnamon >/dev/null 2>&1 || true
			fi
			;;
		mate)
			if command -v apt-get >/dev/null 2>&1; then
				apt-get purge -y mate-desktop-environment-core mate-desktop-environment >/dev/null 2>&1 || true
			elif command -v pacman >/dev/null 2>&1; then
				pacman -Rns --noconfirm mate >/dev/null 2>&1 || true
			fi
			;;
		*)
			echo "Don't know how to safely remove '${de}' automatically - leaving it in place and installing alongside it instead." >&2
			;;
	esac
	if command -v apt-get >/dev/null 2>&1; then
		apt-get autoremove -y >/dev/null 2>&1 || true
	fi
}

# Points the display manager at a real X11 session by default, so streaming
# works right after boot without the user having to manually pick "Xorg" at
# the login screen every time.
configure_default_x11_session() {
	detect_display_server_support
	if [ "$DE_SUPPORT_STATE" != "supported" ]; then
		return
	fi
	case "$DETECTED_DM" in
		lightdm)
			mkdir -p /etc/lightdm/lightdm.conf.d
			cat > /etc/lightdm/lightdm.conf.d/60-nivaroos-host-desktop.conf <<LIGHTDMEOF
[Seat:*]
user-session=${DETECTED_XSESSION_NAME}
LIGHTDMEOF
			echo '/etc/lightdm/lightdm.conf.d/60-nivaroos-host-desktop.conf' >> "$MANIFEST_FILE"
			;;
		gdm3|gdm)
			local gdm_conf="/etc/gdm3/custom.conf"
			[ -f "$gdm_conf" ] || gdm_conf="/etc/gdm/custom.conf"
			if [ -f "$gdm_conf" ]; then
				if grep -q "^WaylandEnable" "$gdm_conf" 2>/dev/null; then
					sed -i 's/^WaylandEnable=.*/WaylandEnable=false/' "$gdm_conf"
				elif grep -q "^\[daemon\]" "$gdm_conf" 2>/dev/null; then
					sed -i '/^\[daemon\]/a WaylandEnable=false' "$gdm_conf"
				else
					printf '\n[daemon]\nWaylandEnable=false\n' >> "$gdm_conf"
				fi
			fi
			;;
		sddm)
			mkdir -p /etc/sddm.conf.d
			cat > /etc/sddm.conf.d/60-nivaroos-host-desktop.conf <<SDDMEOF
[Autologin]
Session=${DETECTED_XSESSION_NAME}
SDDMEOF
			echo '/etc/sddm.conf.d/60-nivaroos-host-desktop.conf' >> "$MANIFEST_FILE"
			;;
	esac
}

# Runs inside a run_step subshell - reads the DESKTOP_ACTION/DETECTED_*
# globals decided earlier by resolve_desktop_environment_support().
run_desktop_provisioning() {
	case "$DESKTOP_ACTION" in
		ensure_x11_default)
			: # Already has a working X11 session - just lock it in as default below.
			;;
		install_x11_companion)
			echo "Adding an X11 session to your existing ${DETECTED_DE_NAME} desktop..."
			pkg_install_x11_companion "$DETECTED_DE_NAME" || true
			;;
		install_new_de_alongside)
			echo "Installing ${DESKTOP_ENV_CHOICE} alongside your current desktop, for streaming..."
			ensure_apt_universe_enabled
			pkg_install_xorg_stack || true
			if [ "$DETECTED_DM" = "none" ]; then
				pkg_install_display_manager || true
			fi
			pkg_install_de "$DESKTOP_ENV_CHOICE" || true
			mkdir -p /var/lib/nivaroos
			echo "alongside:${DESKTOP_ENV_CHOICE}" > "$DESKTOP_PROVISION_MARKER"
			;;
		replace_de)
			echo "Removing existing desktop environment (${DETECTED_DE_NAME:-unknown}) and installing ${DESKTOP_ENV_CHOICE}..."
			remove_desktop_environment "$DETECTED_DE_NAME"
			ensure_apt_universe_enabled
			pkg_install_xorg_stack || true
			if [ "$DETECTED_DM" = "none" ]; then
				pkg_install_display_manager || true
			fi
			pkg_install_de "$DESKTOP_ENV_CHOICE" || true
			mkdir -p /var/lib/nivaroos
			echo "replaced:${DESKTOP_ENV_CHOICE}" > "$DESKTOP_PROVISION_MARKER"
			;;
	esac

	systemctl daemon-reload >/dev/null 2>&1 || true
	configure_default_x11_session

	detect_display_server_support
	if [ "$DE_SUPPORT_STATE" = "supported" ]; then
		echo "Desktop Environment ready for streaming: ${DETECTED_DE_NAME:-unknown} via ${DETECTED_XSESSION_NAME}."
	else
		echo "Could not fully verify a working X11 session after provisioning - Host Desktop may still show a blank stream. Check 'journalctl -u nivaroos-host-desktop' after reboot." >&2
	fi
}

provision_desktop_environment() {
	run_step "Configuring Desktop Environment for Streaming" "run_desktop_provisioning"
}

# Interactive decision-making only (fast, no package installs here) - run
# once during the selection phase, before the progress pipeline starts.
resolve_desktop_environment_support() {
	DESKTOP_ACTION="none"
	detect_display_server_support

	case "$DE_SUPPORT_STATE" in
		supported)
			printf '%b\n' "  ${COLOR_GREEN}✔ Detected ${DETECTED_DE_NAME:-a desktop environment} with a working X11 session (${DETECTED_XSESSION_NAME}) - Host Desktop streaming will work.${COLOR_RESET}"
			DESKTOP_ACTION="ensure_x11_default"
			return
			;;
		wayland_only)
			printf '%b\n' "  ${COLOR_YELLOW}⚠ Detected ${DETECTED_DE_NAME:-your desktop} running under Wayland with no X11 session installed.${COLOR_RESET}"
			printf '%b\n' "    ${COLOR_MUTED}Host Desktop streaming uses x11vnc, which cannot capture a Wayland session - without a fix it would silently show a blank/frozen stream.${COLOR_RESET}"
			;;
		no_de)
			printf '%b\n' "  ${COLOR_YELLOW}⚠ No desktop environment detected on this machine - there is nothing yet for Host Desktop to stream.${COLOR_RESET}"
			;;
	esac

	if [ -n "$YES" ] || [ "$INTERACTIVE_TTY" != "true" ]; then
		# --replace-desktop is the operator explicitly pre-authorizing a
		# destructive swap for this run - only honor it here (skipping the
		# interactive typed-name confirmation entirely) when there is
		# actually something worth replacing; a healthy X11 desktop is
		# never touched just because the flag was passed.
		if [ "$REPLACE_DESKTOP" = "yes" ] && [ "$DE_SUPPORT_STATE" = "wayland_only" ]; then
			DESKTOP_ACTION="replace_de"
			DESKTOP_ENV_CHOICE="${DESKTOP_ENV_CHOICE:-xfce}"
			printf '%b\n\n' "  ${COLOR_YELLOW}--replace-desktop given: removing ${DETECTED_DE_NAME:-the current desktop} and installing ${DESKTOP_ENV_CHOICE} instead.${COLOR_RESET}"
		elif [ "$DE_SUPPORT_STATE" = "wayland_only" ] && { [ "$DETECTED_DE_NAME" = "gnome" ] || [ "$DETECTED_DE_NAME" = "plasma" ]; }; then
			DESKTOP_ACTION="install_x11_companion"
			printf '%b\n\n' "  ${COLOR_MUTED}Non-interactive mode: applying the safe, non-destructive fix automatically.${COLOR_RESET}"
		else
			DESKTOP_ACTION="install_new_de_alongside"
			DESKTOP_ENV_CHOICE="${DESKTOP_ENV_CHOICE:-xfce}"
			printf '%b\n\n' "  ${COLOR_MUTED}Non-interactive mode: applying the safe, non-destructive fix automatically.${COLOR_RESET}"
		fi
		return
	fi

	printf '\n'
	local options=() actions=()
	if [ "$DE_SUPPORT_STATE" = "wayland_only" ] && { [ "$DETECTED_DE_NAME" = "gnome" ] || [ "$DETECTED_DE_NAME" = "plasma" ]; }; then
		options+=("Add an X11 session to your existing ${DETECTED_DE_NAME} desktop (recommended - keeps everything else unchanged)")
		actions+=("install_x11_companion")
	fi
	options+=("Install XFCE alongside your current setup, just for streaming (lightweight, most compatible)")
	actions+=("install_new_de_alongside:xfce")
	options+=("Install Cinnamon alongside your current setup, just for streaming (modern look, heavier)")
	actions+=("install_new_de_alongside:cinnamon")
	options+=("Install MATE alongside your current setup, just for streaming (lightweight, classic look)")
	actions+=("install_new_de_alongside:mate")
	if [ "$DE_SUPPORT_STATE" = "wayland_only" ]; then
		options+=("Replace your current desktop entirely with a chosen one (destructive - removes ${DETECTED_DE_NAME:-your current desktop})")
		actions+=("replace_de")
	fi
	options+=("Skip Host Desktop for now")
	actions+=("skip")

	local i
	for i in "${!options[@]}"; do
		printf '%b\n' "    ${COLOR_CYAN}$((i+1)))${COLOR_RESET} ${options[$i]}"
	done
	local choice=""
	printf '%b' "\n  ${COLOR_CYAN}?${COLOR_RESET} ${COLOR_BOLD}Choose how to proceed [1]:${COLOR_RESET} "
	read -r choice </dev/tty || choice=""
	[ -z "$choice" ] && choice=1
	local idx=$((choice - 1))
	if [ "$idx" -lt 0 ] || [ "$idx" -ge "${#actions[@]}" ]; then idx=0; fi
	local picked="${actions[$idx]}"

	case "$picked" in
		install_x11_companion)
			DESKTOP_ACTION="install_x11_companion"
			;;
		install_new_de_alongside:*)
			DESKTOP_ACTION="install_new_de_alongside"
			DESKTOP_ENV_CHOICE="${picked#*:}"
			;;
		replace_de)
			printf '\n'
			printf '%b\n' "    ${COLOR_CYAN}1)${COLOR_RESET} XFCE   ${COLOR_CYAN}2)${COLOR_RESET} Cinnamon   ${COLOR_CYAN}3)${COLOR_RESET} MATE"
			local de_choice=""
			printf '%b' "  ${COLOR_CYAN}?${COLOR_RESET} ${COLOR_BOLD}Replace with which desktop? [1]:${COLOR_RESET} "
			read -r de_choice </dev/tty || de_choice=""
			case "$de_choice" in
				2) DESKTOP_ENV_CHOICE="cinnamon" ;;
				3) DESKTOP_ENV_CHOICE="mate" ;;
				*) DESKTOP_ENV_CHOICE="xfce" ;;
			esac
			# --replace-desktop on the command line already IS the
			# operator's explicit authorization for this destructive
			# action, given before this menu even ran - picking "Replace"
			# from the menu is the second, in-the-moment confirmation, so
			# the typed-name safety net below is for the case where the
			# flag was never given and this is the only confirmation.
			if [ "$REPLACE_DESKTOP" = "yes" ]; then
				DESKTOP_ACTION="replace_de"
				printf '%b\n\n' "  ${COLOR_YELLOW}--replace-desktop given: skipping the extra typed confirmation.${COLOR_RESET}"
			else
				printf '%b\n' "\n  ${COLOR_RED}This will remove your current desktop environment (${DETECTED_DE_NAME:-unknown}) and install ${DESKTOP_ENV_CHOICE}.${COLOR_RESET}"
				printf '%b' "  Type the desktop's name (${COLOR_BOLD}${DETECTED_DE_NAME:-unknown}${COLOR_RESET}) to confirm, or press Enter to cancel: "
				local confirm_reply=""
				read -r confirm_reply </dev/tty || confirm_reply=""
				if [ -n "$DETECTED_DE_NAME" ] && [ "$confirm_reply" = "$DETECTED_DE_NAME" ]; then
					DESKTOP_ACTION="replace_de"
				else
					info "Replacement cancelled - installing ${DESKTOP_ENV_CHOICE} alongside your current desktop instead."
					DESKTOP_ACTION="install_new_de_alongside"
				fi
			fi
			;;
		skip)
			DESKTOP_ACTION="none"
			WITH_HOST_DESKTOP="no"
			info "Host Desktop skipped - your other component selections are unaffected."
			;;
	esac
	printf '\n'
}

# ------------------------------------------------------------------------------
# Component Selection
# ------------------------------------------------------------------------------
compute_default_selections() {
	if [ -e /dev/kvm ]; then KVM_AVAILABLE="yes"; else KVM_AVAILABLE="no"; fi

	if [ -z "$WITH_VM" ]; then
		if [ "$KVM_AVAILABLE" = "yes" ]; then WITH_VM=yes; else WITH_VM=no; fi
	fi
	[ -z "$WITH_HOST_DESKTOP" ] && WITH_HOST_DESKTOP=no
}

# Samba and mDNS are core parts of NivaroOS (Network Shares and mobile-app
# discovery are always-on dashboard features, not add-ons) - they always
# install, with no flag to skip them, the same as Docker or the web
# dashboard itself. Only VM Manager and Host Desktop are optional enough to
# warrant a selection screen: VM Manager pulls in QEMU/KVM/libvirt, and Host
# Desktop can provision or replace a desktop environment - both are real,
# consequential choices. Samba/mDNS are neither.
select_components() {
	compute_default_selections

	if [ -n "$YES" ] || [ "$INTERACTIVE_TTY" != "true" ]; then
		: # Respect flags/detected defaults as-is, no menu.
	else
		printf '%b\n' "${COLOR_BOLD}${COLOR_WHITE}Select Optional Components:${COLOR_RESET}"
		printf '%b\n' "  ${COLOR_MUTED}Core Platform (Dashboard, Gateway, App Store, File Manager, Samba File${COLOR_RESET}"
		printf '%b\n\n' "  ${COLOR_MUTED}Sharing, mDNS Discovery) always installs.${COLOR_RESET}"

		local vm_desc hd_desc
		if [ "$KVM_AVAILABLE" = "yes" ]; then
			vm_desc="KVM hardware acceleration detected on this CPU."
		else
			vm_desc="No KVM acceleration detected - VMs would use slower software emulation."
		fi
		hd_desc="Requires VM Manager (shares its vm-sidecar). Streams this machine's own physical desktop."

		CBM_LABELS=(
			"VM Manager - QEMU/KVM, Libvirt, Web Console, VirtIO-FS"
			"Host Desktop Streaming - stream this machine's own desktop over VNC"
		)
		CBM_DESCS=(
			"$vm_desc"
			"$hd_desc"
		)
		CBM_STATE=(
			"$([ "$WITH_VM" = "yes" ] && echo 1 || echo 0)"
			"$([ "$WITH_HOST_DESKTOP" = "yes" ] && echo 1 || echo 0)"
		)

		checkbox_menu "Select Optional Components"

		WITH_VM="$([ "${CBM_STATE[0]}" = "1" ] && echo yes || echo no)"
		WITH_HOST_DESKTOP="$([ "${CBM_STATE[1]}" = "1" ] && echo yes || echo no)"
	fi

	if [ "$WITH_HOST_DESKTOP" = "yes" ] && [ "$WITH_VM" != "yes" ]; then
		printf '%b\n\n' "  ${COLOR_YELLOW}ℹ Host Desktop requires VM Manager (shares its vm-sidecar) - enabling VM Manager too.${COLOR_RESET}"
		WITH_VM=yes
	fi

	if [ "$WITH_HOST_DESKTOP" = "yes" ]; then
		resolve_desktop_environment_support
	fi

	TOTAL_STEPS=$BASE_STEPS
	[ "$WITH_VM" = "yes" ] && TOTAL_STEPS=$((TOTAL_STEPS + 1))
	if [ "$WITH_HOST_DESKTOP" = "yes" ]; then
		TOTAL_STEPS=$((TOTAL_STEPS + 2)) # provisioning step + the streaming service step
	fi

	printf '%b\n' "${COLOR_BOLD}${COLOR_WHITE}Selected Components:${COLOR_RESET}"
	local mark_vm="${COLOR_MUTED}○ VM Manager (off)${COLOR_RESET}"
	local mark_hd="${COLOR_MUTED}○ Host Desktop (off)${COLOR_RESET}"
	[ "$WITH_VM" = "yes" ] && mark_vm="${COLOR_GREEN}✔ VM Manager${COLOR_RESET}"
	[ "$WITH_HOST_DESKTOP" = "yes" ] && mark_hd="${COLOR_GREEN}✔ Host Desktop${COLOR_RESET}"
	printf '%b\n' "  ${mark_vm}"
	printf '%b\n\n' "  ${mark_hd}"
}

# ------------------------------------------------------------------------------
# Error Handling & Reporting
# ------------------------------------------------------------------------------
print_error_card() {
	local title="$1"
	local exit_code="$2"
	local log_file="$3"

	get_term_size
	local box_width=$((TERM_COLS - 2))
	if [ "$box_width" -lt 40 ]; then box_width=40; fi
	local inner_width=$((box_width - 6))

	local title_tag=" Installation Failed "
	local top_dashes_len=$((box_width - ${#title_tag} - 4))
	if [ "$top_dashes_len" -lt 2 ]; then top_dashes_len=2; fi
	local top_dashes=""
	for ((d=0; d<top_dashes_len; d++)); do top_dashes+="─"; done

	local bot_dashes=""
	for ((d=0; d<box_width-2; d++)); do bot_dashes+="─"; done

	printf "\n"
	printf '%b\n' "${COLOR_RED}╭──${COLOR_BOLD}${title_tag}${COLOR_RESET}${COLOR_RED}${top_dashes}╮${COLOR_RESET}"

	render_err_line() {
		local text="$1"
		local plain
		plain="$(strip_ansi "$text")"
		if [ "${#plain}" -gt "$inner_width" ]; then
			plain="${plain:0:$inner_width}"
		fi
		local pad_len=$((inner_width - ${#plain}))
		local pad=""
		if [ "$pad_len" -gt 0 ]; then
			pad="$(printf '%*s' "$pad_len" '')"
		fi
		printf '%b\n' "${COLOR_RED}│${COLOR_RESET}  ${plain}${pad}  ${COLOR_RED}│${COLOR_RESET}"
	}

	render_err_line "✖ Step Failed: ${title}"
	render_err_line "✖ Exit Code  : ${exit_code}"
	render_err_line ""
	render_err_line "Recent Log Output:"
	printf '%b\n' "${COLOR_RED}├${bot_dashes}┤${COLOR_RESET}"

	if [ -f "$log_file" ] && [ -s "$log_file" ]; then
		while IFS= read -r line; do
			render_err_line "$line"
		done < <(tail -n 14 "$log_file")
	else
		render_err_line "(No detailed log output captured)"
	fi

	printf '%b\n' "${COLOR_RED}├${bot_dashes}┤${COLOR_RESET}"
	render_err_line "Troubleshooting Tips:"
	render_err_line "• Full installation log: ${INSTALL_LOG}"
	render_err_line "• Verify internet connectivity and package mirrors."
	render_err_line "• Report issues: https://github.com/F-e-n-y-x/NivaroOS/issues"
	printf '%b\n\n' "${COLOR_RED}╰${bot_dashes}╯${COLOR_RESET}"
}

on_fatal_error() {
	local exit_code=$?
	local line_no="$1"
	# If this fires while a step's live spinner loop is mid-frame (e.g. an
	# unexpected error in the renderer itself, not a step command failure -
	# those are handled separately and already close this first), we're
	# still on the alternate screen buffer. Printing the error there and
	# then exiting - which restores the real screen via cleanup_on_exit's
	# EXIT trap - would silently discard the very message just printed,
	# leaving the terminal looking like nothing happened at all instead of
	# showing why it stopped.
	if [ "$IN_ALT_SCREEN" = "true" ]; then
		printf "\033[?1049l\033[?25h"
		IN_ALT_SCREEN="false"
	fi
	if [ "$exit_code" -ne 0 ]; then
		log_raw "Fatal error at line ${line_no} (exit code ${exit_code})"
		error "Installation terminated unexpectedly at line ${line_no} (exit code ${exit_code})."
	fi
	exit "$exit_code"
}
trap 'on_fatal_error "$LINENO"' ERR

# ------------------------------------------------------------------------------
# Full-Width Responsive Split-Pane Live Stream Step Runner
# ------------------------------------------------------------------------------
run_step() {
	local title="$1"
	shift
	STEP_NUM=$((STEP_NUM + 1))
	CURRENT_STEP_TITLE="$title"
	local step_tag="[${STEP_NUM}/${TOTAL_STEPS}]"
	local start_ts
	start_ts=$(date +%s)

	log_raw ">>> START STEP ${STEP_NUM}/${TOTAL_STEPS}: ${title}"

	local log_file
	log_file=$(mktemp /tmp/nivaroos-install-step-XXXXXX.log)

	if [ "$DEBUG" = "yes" ]; then
		printf '%b\n' "  ${COLOR_CYAN}➜${COLOR_RESET} ${COLOR_BOLD}${COLOR_BLUE}${step_tag}${COLOR_RESET} ${COLOR_WHITE}${title}${COLOR_RESET} (verbose)..."
		if ! (
			export PATH="/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin:$PATH"
			export DEBIAN_FRONTEND=noninteractive
			export NEEDRESTART_MODE=a
			export GOWORK=off
			eval "$*"
		) 2>&1 | tee -a "$INSTALL_LOG"; then
			local exit_code=$?
			log_raw "<<< FAILED STEP ${STEP_NUM}: ${title} (exit code ${exit_code})"
			error "Step ${STEP_NUM} failed: ${title}"
			exit "$exit_code"
		fi
		local end_ts
		end_ts=$(date +%s)
		local elapsed=$((end_ts - start_ts))
		log_raw "<<< COMPLETED STEP ${STEP_NUM}: ${title} [${elapsed}s]"
		printf '%b\n' "  ${COLOR_GREEN}✔${COLOR_RESET} ${COLOR_BOLD}${COLOR_BLUE}${step_tag}${COLOR_RESET} ${COLOR_WHITE}${title}${COLOR_RESET} ${COLOR_MUTED}[${elapsed}s]${COLOR_RESET}"
		rm -f "$log_file"
		return 0
	fi

	if [ "$IS_TTY" = "true" ]; then
		(
			export PATH="/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin:$PATH"
			export DEBIAN_FRONTEND=noninteractive
			export NEEDRESTART_MODE=a
			export GOWORK=off
			eval "$*"
		) > "$log_file" 2>&1 </dev/null &
		local cmd_pid=$!
		CURRENT_STEP_PID="$cmd_pid"

		local frame_idx=0
		local num_frames=${#SPINNER_FRAMES[@]}

		# Draw the live spinner+log box on the terminal's ALTERNATE screen
		# buffer, not the normal scrolling one. The previous approach drew
		# in the normal buffer and returned to the top of its own box each
		# frame with a relative "cursor up N lines" - which only stays
		# correct as long as nothing has caused the terminal to scroll
		# since the last frame. A long-running step (like compiling all the
		# Go services) renders hundreds of frames, and the moment any of
		# them pushed the box against the bottom of the terminal and the
		# terminal scrolled, "up N lines" started landing one or more rows
		# above where the box's top actually was - so every subsequent
		# frame got printed as a brand new set of lines instead of
		# overwriting, which is exactly the "[5/14] ... repeated many
		# times" behavior. The alternate screen buffer never scrolls (it's
		# always exactly the terminal's current size) and "\033[H" (home)
		# is an absolute position, not a relative one - so this can't
		# desync regardless of how long the step runs or how the terminal
		# gets resized mid-step. Leaving the alternate buffer (\033[?1049l)
		# restores the real screen exactly as it looked before entering,
		# so none of these frames ever touch real scrollback - only the
		# final one-line result (printed after leaving, below) does.
		printf "\033[?1049h\033[?25l"
		IN_ALT_SCREEN="true"

		while kill -0 "$cmd_pid" 2>/dev/null; do
			local current_ts
			current_ts=$(date +%s)
			local elapsed=$((current_ts - start_ts))
			local frame="${SPINNER_FRAMES[$frame_idx]}"

			# Dynamically re-query terminal size EVERY frame in real-time
			get_term_size
			local box_width=$((TERM_COLS - 2))
			if [ "$box_width" -lt 38 ]; then box_width=38; fi
			local inner_width=$((box_width - 6))

			# The box is drawn from the absolute top of the (alternate)
			# screen every frame, so its total height must never exceed
			# the terminal's CURRENT row count or it gets clipped/overlaps
			# at the bottom - unlike the old scrolling-buffer approach,
			# there's no scrollback to fall back on here. Reserve 3 rows
			# for the spinner/header line plus the box's own top and
			# bottom borders, and fit the log lines into whatever's left,
			# recomputed every frame so a live terminal resize (SIGWINCH)
			# is honored immediately rather than only at the next step.
			local reserved_lines=3
			local available_lines=$((TERM_ROWS - reserved_lines))
			# Use however much vertical space is actually there instead of
			# a fixed 10/14-line box that leaves most of a large terminal
			# blank - capped only so an absurdly tall terminal doesn't turn
			# this into an unreasonably long scrolling wall of log lines.
			local max_log_lines=30
			local num_log_lines="$available_lines"
			if [ "$num_log_lines" -gt "$max_log_lines" ]; then
				num_log_lines="$max_log_lines"
			fi

			printf "\033[H"

			if [ "$available_lines" -lt 3 ]; then
				# Even the minimum useful box (3 log lines + header + top/
				# bottom borders = 6 rows) doesn't fit a terminal this
				# short. Rather than draw something guaranteed to overflow
				# it, fall back to a single status line - still absolutely
				# positioned and cleared with \033[J, so it stays fully
				# adaptive with no leftover content regardless of size.
				local status_line="  ${frame} ${step_tag} ${title} (${elapsed}s)"
				local clean_status
				clean_status="$(strip_ansi "$status_line")"
				if [ "${#clean_status}" -gt "$TERM_COLS" ]; then
					clean_status="${clean_status:0:$TERM_COLS}"
				fi
				printf "\033[2K%b%s%b\r\n" "${COLOR_CYAN}" "$clean_status" "${COLOR_RESET}"
				printf "\033[J"
				frame_idx=$(( (frame_idx + 1) % num_frames ))
				sleep 0.08
				continue
			fi

			if [ "$num_log_lines" -lt 3 ]; then
				num_log_lines=3
			fi

			# Top Half: Progress Header with Animated Spinner & Live Timer
			printf "\033[2K  %b %b %b %b(%ds)%b\r\n" \
				"${COLOR_CYAN}${frame}${COLOR_RESET}" \
				"${COLOR_BOLD}${COLOR_BLUE}${step_tag}${COLOR_RESET}" \
				"${COLOR_WHITE}${title}${COLOR_RESET}" \
				"${COLOR_MUTED}" "${elapsed}" "${COLOR_RESET}"

			# Bottom Half: Fully Enclosed, Laser-Aligned Live Activity Box
			local title_tag=" Live Activity "
			local top_dashes_len=$((box_width - ${#title_tag} - 4))
			if [ "$top_dashes_len" -lt 2 ]; then top_dashes_len=2; fi
			local top_dashes=""
			for ((d=0; d<top_dashes_len; d++)); do top_dashes+="─"; done

			printf "\033[2K%b╭──%b%s%b%s╮%b\r\n" \
				"${COLOR_MUTED}" "${COLOR_CYAN}" "${title_tag}" "${COLOR_MUTED}" "${top_dashes}" "${COLOR_RESET}"

			local lines=()
			if [ -f "$log_file" ] && [ -s "$log_file" ]; then
				mapfile -t lines < <(tail -n "$num_log_lines" "$log_file" 2>/dev/null || true)
			fi

			local pad_count=$((num_log_lines - ${#lines[@]}))
			for ((p=0; p<pad_count; p++)); do
				local empty_pad=""
				if [ "$inner_width" -gt 3 ]; then
					empty_pad="$(printf '%*s' "$((inner_width - 3))" '')"
				fi
				printf "\033[2K%b│%b  ...%s  %b│%b\r\n" "${COLOR_MUTED}" "${COLOR_MUTED}" "$empty_pad" "${COLOR_MUTED}" "${COLOR_RESET}"
			done

			for l in "${lines[@]}"; do
				local clean_l
				clean_l="$(strip_ansi "$l")"
				if [ "${#clean_l}" -gt "$inner_width" ]; then
					clean_l="${clean_l:0:$inner_width}"
				fi
				local pad_len=$((inner_width - ${#clean_l}))
				local pad=""
				if [ "$pad_len" -gt 0 ]; then
					pad="$(printf '%*s' "$pad_len" '')"
				fi
				printf "\033[2K%b│%b  %s%s  %b│%b\r\n" \
					"${COLOR_MUTED}" "${COLOR_WHITE}" "$clean_l" "$pad" "${COLOR_MUTED}" "${COLOR_RESET}"
			done

			local bot_dashes=""
			for ((d=0; d<box_width-2; d++)); do bot_dashes+="─"; done
			printf "\033[2K%b╰%s╯%b\r\n" "${COLOR_MUTED}" "${bot_dashes}" "${COLOR_RESET}"

			# Erase anything left over below this frame from a taller
			# previous one (e.g. the terminal just got shrunk).
			printf "\033[J"

			frame_idx=$(( (frame_idx + 1) % num_frames ))
			sleep 0.08
		done

		# `wait` for a specific PID returns that job's own exit status - if
		# it's nonzero, `wait` itself counts as a failing command under
		# set -e, which aborts the WHOLE SCRIPT right here, before any of
		# the code below (which exists specifically to handle a failed
		# step gracefully - print_error_card, the log tail, etc.) ever
		# runs. Every failed step has always died silently at this exact
		# line instead of showing why.
		#
		# `set +e` alone is not enough to fix this: the ERR trap fires
		# based on errtrace (-E), independent of whether errexit is
		# currently on, so on_fatal_error would still run (and still
		# unconditionally exit) even with errexit off. Both the trap AND
		# errexit have to be suspended around this one call, then both
		# restored, for `wait`'s result to actually be inspectable instead
		# of immediately fatal.
		trap '' ERR
		set +e
		wait "$cmd_pid"
		local exit_code=$?
		set -e
		trap 'on_fatal_error "$LINENO"' ERR
		CURRENT_STEP_PID=""
		local end_ts
		end_ts=$(date +%s)
		local total_elapsed=$((end_ts - start_ts))

		cat "$log_file" >> "$INSTALL_LOG" 2>/dev/null || true

		# Leave the alternate screen - this restores the real screen
		# exactly as it was before entering, with none of the spinner
		# frames ever having touched it.
		printf "\033[?1049l\033[?25h"
		IN_ALT_SCREEN="false"

		if [ "$exit_code" -eq 0 ]; then
			log_raw "<<< COMPLETED STEP ${STEP_NUM}: ${title} [${total_elapsed}s]"
			printf "\r\033[2K  %b %b %b %b[%ds]%b\n" \
				"${COLOR_GREEN}✔${COLOR_RESET}" \
				"${COLOR_BOLD}${COLOR_BLUE}${step_tag}${COLOR_RESET}" \
				"${COLOR_WHITE}${title}${COLOR_RESET}" \
				"${COLOR_MUTED}" "${total_elapsed}" "${COLOR_RESET}"
			rm -f "$log_file"
		else
			log_raw "<<< FAILED STEP ${STEP_NUM}: ${title} [${total_elapsed}s, exit code ${exit_code}]"
			printf "\r\033[2K  %b %b %b %b[%ds - FAILED]%b\n" \
				"${COLOR_RED}✖${COLOR_RESET}" \
				"${COLOR_BOLD}${COLOR_RED}${step_tag}${COLOR_RESET}" \
				"${COLOR_WHITE}${title}${COLOR_RESET}" \
				"${COLOR_RED}" "${total_elapsed}" "${COLOR_RESET}"
			print_error_card "$title" "$exit_code" "$log_file"
			exit "$exit_code"
		fi
	else
		printf "  ➜ %s %s...\n" "$step_tag" "$title"
		if (
			export PATH="/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin:$PATH"
			export DEBIAN_FRONTEND=noninteractive
			export NEEDRESTART_MODE=a
			export GOWORK=off
			eval "$*"
		) > "$log_file" 2>&1 </dev/null; then
			local end_ts
			end_ts=$(date +%s)
			local total_elapsed=$((end_ts - start_ts))
			cat "$log_file" >> "$INSTALL_LOG" 2>/dev/null || true
			log_raw "<<< COMPLETED STEP ${STEP_NUM}: ${title} [${total_elapsed}s]"
			printf "  ✔ %s %s [%ds]\n" "$step_tag" "$title" "$total_elapsed"
			rm -f "$log_file"
		else
			local exit_code=$?
			cat "$log_file" >> "$INSTALL_LOG" 2>/dev/null || true
			log_raw "<<< FAILED STEP ${STEP_NUM}: ${title} (exit code ${exit_code})"
			printf "  ✖ %s %s [FAILED with exit code %d]\n" "$step_tag" "$title" "$exit_code"
			print_error_card "$title" "$exit_code" "$log_file"
			exit "$exit_code"
		fi
	fi
}

# ------------------------------------------------------------------------------
# Package Manager & Dependency Helpers
# ------------------------------------------------------------------------------
pkg_update() {
	if command -v apt-get >/dev/null 2>&1; then
		apt-get update -qq
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
		apt-get install -y --no-install-recommends "${pkgs[@]}"
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

install_core_dependencies() {
	run_step "Installing Core System Dependencies" "
		pkg_update
		if command -v apt-get >/dev/null 2>&1; then
			pkg_install curl wget git tar ca-certificates udev util-linux pciutils smartmontools parted build-essential rsync
		elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
			pkg_install curl wget git tar ca-certificates systemd-udev util-linux pciutils smartmontools parted make gcc rsync
		elif command -v pacman >/dev/null 2>&1; then
			pkg_install curl wget git tar ca-certificates systemd util-linux pciutils smartmontools parted base-devel rsync
		elif command -v zypper >/dev/null 2>&1; then
			pkg_install curl wget git tar ca-certificates udev util-linux pciutils smartmontools parted make gcc rsync
		elif command -v apk >/dev/null 2>&1; then
			pkg_install curl wget git tar ca-certificates udev util-linux pciutils smartmontools parted build-base rsync
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
			curl -fsSL https://get.docker.com | sh
		fi

		mkdir -p /etc/systemd/system/docker.service.d
		cat > /etc/systemd/system/docker.service.d/override.conf <<'DOCKEREOF'
[Service]
Environment=\"DOCKER_MIN_API_VERSION=1.24\"
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
			git fetch --all --tags --prune
			# reset --hard + clean (not checkout + pull) so a dirty tree -
			# left behind by a previous crashed run, or a manual edit made
			# while debugging - can never hard-abort this step. This is an
			# unattended installer/updater, not a workspace the running
			# user is expected to have made their own changes in.
			git reset --hard \"origin/$BRANCH\"
			git clean -fdx
		else
			if [ -d \"$SRC_DIR\" ] && [ \"\$(ls -A \"$SRC_DIR\" 2>/dev/null)\" ]; then
				rm -rf \"${SRC_DIR:?}\"/* \"${SRC_DIR:?}\"/.[!.]* 2>/dev/null || true
			fi
			git clone --branch \"$BRANCH\" --depth 1 \"$REPO_URL\" \"$SRC_DIR\"
		fi

		# Ensure Go toolchain is installed
		if ! command -v go >/dev/null 2>&1; then
			go_arch=\"amd64\"
			case \"\$(uname -m)\" in
				x86_64) go_arch=\"amd64\" ;;
				aarch64|arm64) go_arch=\"arm64\" ;;
				armv7l|armhf) go_arch=\"armv6l\" ;;
			esac
			wget -q \"https://go.dev/dl/go${GO_VERSION}.linux-\${go_arch}.tar.gz\" -O /tmp/go.tar.gz
			rm -rf /usr/local/go
			tar -C /usr/local -xzf /tmp/go.tar.gz
			rm -f /tmp/go.tar.gz
			export PATH=\"/usr/local/go/bin:\$PATH\"
		fi
	"
}

# ------------------------------------------------------------------------------
# Microservices Compilation & Installation
# ------------------------------------------------------------------------------
install_core_services() {
	run_step "Compiling & Installing NivaroOS Microservices" "
		cd \"$SRC_DIR\"
		export PATH=\"/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin:\$PATH\"

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

		mkdir -p /var/lib/nivaroos /var/run/nivaroos /etc/nivaroos /DATA/AppData /DATA/Documents /DATA/Downloads /DATA/Media /DATA/Gallery /DATA/VMs/share
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
		cd \"${SRC_DIR}/services/user\"
		go build -o /usr/bin/nivaroos-user-service .
		echo '/usr/bin/nivaroos-user-service' >> \"$MANIFEST_FILE\"

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
			\"${SRC_DIR}/services/core/build/sysroot/usr/lib/systemd/system/rclone.service:/usr/lib/systemd/system/rclone.service\"
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

		# Write GPU Sidecar service unit
		cat > /usr/lib/systemd/system/nivaroos-gpu-sidecar.service <<'GPUEOF'
[Unit]
Description=NivaroOS GPU Sidecar
After=network.target

[Service]
ExecStart=/usr/bin/nivaroos-gpu-sidecar
Restart=always
# Conservative hardening - this service doesn't need write access to /home
# or to modify /usr, /boot or /etc, but still needs root-equivalent access
# to query GPU/driver state, so this stops short of ProtectSystem=strict
# or a narrow ReadWritePaths allowlist that risks blocking a real write
# path this installer can't fully enumerate.
NoNewPrivileges=true
ProtectHome=true
ProtectSystem=full

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

		# 11. Install USB mount helper script
		mkdir -p /usr/share/nivaroos/shell
		if [ -f \"${SRC_DIR}/services/core/build/sysroot/usr/share/nivaroos/shell/usb-mount.sh\" ]; then
			cp -f \"${SRC_DIR}/services/core/build/sysroot/usr/share/nivaroos/shell/usb-mount.sh\" /usr/share/nivaroos/shell/usb-mount.sh
			chmod 755 /usr/share/nivaroos/shell/usb-mount.sh
			echo '/usr/share/nivaroos/shell/usb-mount.sh' >> \"$MANIFEST_FILE\"
		fi

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
		if command -v apt-get >/dev/null 2>&1; then
			pkg_install qemu-system-x86 qemu-utils libvirt-daemon-system libvirt-clients virtinst bridge-utils ovmf cloud-image-utils pkg-config libvirt-dev
		elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
			pkg_install qemu-kvm qemu-img libvirt libvirt-client virt-install bridge-utils edk2-ovmf pkgconf-pkg-config libvirt-devel
		elif command -v pacman >/dev/null 2>&1; then
			pkg_install qemu-base libvirt virt-install bridge-utils edk2-ovmf pkgconf
		elif command -v zypper >/dev/null 2>&1; then
			pkg_install qemu-kvm qemu-tools libvirt libvirt-client virt-install bridge-utils qemu-ovmf-x86_64 pkg-config libvirt-devel
		elif command -v apk >/dev/null 2>&1; then
			pkg_install qemu-system-x86_64 qemu-img libvirt libvirt-daemon virt-install bridge dnsmasq ovmf pkgconf libvirt-dev
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
ProtectHome=true

[Install]
WantedBy=multi-user.target
VMEOF
		echo '/usr/lib/systemd/system/nivaroos-vm-sidecar.service' >> \"$MANIFEST_FILE\"

		mkdir -p /DATA/VMs/Images /DATA/VMs/ISOs /DATA/VMs/Disks
		systemctl daemon-reload >/dev/null 2>&1 || true
		systemctl enable --now libvirtd >/dev/null 2>&1 || true
		systemctl enable --now nivaroos-vm-sidecar >/dev/null 2>&1 || true
	"
}

# ------------------------------------------------------------------------------
# Host Desktop Streaming Installation (Optional Add-on, requires VM Manager -
# it streams over the same vm-sidecar the VM Manager installs). By the time
# this runs, provision_desktop_environment() has already made sure an X11
# session actually exists and is the default - this step only installs the
# VNC bridge itself.
# ------------------------------------------------------------------------------
install_host_desktop() {
	run_step "Installing Host Desktop Streaming (x11vnc & websockify)" "
		# Best-effort: x11vnc/websockify aren't in every distro's official
		# repos (notably Arch/Alpine). This is an optional add-on, so a
		# missing package here should not abort the whole installation -
		# we just warn and the feature stays unavailable until installed
		# manually.
		pkg_install x11vnc websockify || true

		if ! command -v x11vnc >/dev/null 2>&1; then
			echo 'x11vnc could not be installed automatically on this distro - Host Desktop streaming will be unavailable until it is installed manually.' >&2
		fi

		cat > /usr/local/bin/nivaroos-host-desktop.sh <<'HOSTDESKEOF'
#!/bin/bash
set -e

# Find X authority file
find_auth() {
    for f in /var/run/lightdm/root/:0 /run/lightdm/root/:0 /root/.Xauthority /home/*/.Xauthority; do
        if [ -f \"\$f\" ]; then
            echo \"\$f\"
            return 0
        fi
    done
    echo \"\"
}

# Wait for X server on :0 if not yet ready
for i in {1..30}; do
    if [ -S /tmp/.X11-unix/X0 ] || xdpyinfo -display :0 >/dev/null 2>&1; then
        break
    fi
    sleep 1
done

AUTH=\$(find_auth)

# Set initial default framebuffer resolution to 1920x1080 if currently lower (e.g. 640x480 headless default)
if [ -n \"\$AUTH\" ]; then
    DISPLAY=:0 XAUTHORITY=\"\$AUTH\" xrandr --fb 1920x1080 2>/dev/null || true
else
    xrandr -display :0 --fb 1920x1080 2>/dev/null || true
fi

# Start websockify proxy on port 28642 if not already running
if ! ss -tulpn | grep -q \":28642 \"; then
    /usr/bin/websockify -D 28642 127.0.0.1:5900 2>/dev/null || true
fi

# -noxdamage: some desktop compositors (GL-based effects) make the X11
# damage extension unreliable, silently missing change events - which is
# what causes stale/corrupted patches on the stream. Polling instead of
# trusting damage events costs a little CPU but eliminates that class of
# artifact entirely.
# -fixscreen X=5: -noxdamage alone doesn't fully fix it - the compositor
# can still leave x11vnc's own tile-comparison believing certain regions
# are unchanged when the actually-displayed (composited) content moved on
# without it. X=5 forces a genuine full re-read of the X11 framebuffer
# from the X server every 5s, bypassing that comparison entirely, so any
# such patch self-heals within 5 seconds regardless of what caused it.
# -localhost: this server is reachable only via the vm-sidecar's WebSocket
# proxy (which always connects over 127.0.0.1) - there is no legitimate
# reason to expose a raw, unauthenticated VNC port to the network.
if [ -n \"\$AUTH\" ]; then
    exec /usr/bin/x11vnc -display :0 -auth \"\$AUTH\" -xrandr resize -forever -shared -repeat -noxdamage -fixscreen X=5 -localhost -rfbport 5900 -nopw
else
    exec /usr/bin/x11vnc -display :0 -auth guess -xrandr resize -forever -shared -repeat -noxdamage -fixscreen X=5 -localhost -rfbport 5900 -nopw
fi
HOSTDESKEOF
		chmod +x /usr/local/bin/nivaroos-host-desktop.sh
		echo '/usr/local/bin/nivaroos-host-desktop.sh' >> \"$MANIFEST_FILE\"

		cat > /usr/lib/systemd/system/nivaroos-host-desktop.service <<'HOSTDESKSVCEOF'
[Unit]
Description=NivaroOS Host Desktop Remote VNC Server
After=network.target lightdm.service display-manager.service
Wants=lightdm.service

[Service]
Type=simple
ExecStart=/usr/local/bin/nivaroos-host-desktop.sh
Restart=always
RestartSec=3
KillMode=mixed

[Install]
WantedBy=multi-user.target
HOSTDESKSVCEOF
		echo '/usr/lib/systemd/system/nivaroos-host-desktop.service' >> \"$MANIFEST_FILE\"

		sort -u -o \"$MANIFEST_FILE\" \"$MANIFEST_FILE\" 2>/dev/null || true

		systemctl daemon-reload >/dev/null 2>&1 || true
		if command -v x11vnc >/dev/null 2>&1; then
			systemctl enable --now nivaroos-host-desktop >/dev/null 2>&1 || true
		fi
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
ensure_node_toolchain() {
	if command -v pnpm >/dev/null 2>&1; then
		return 0
	fi

	echo "No prebuilt web dashboard found - installing Node.js and pnpm to build it from source..."

	local node_major=0
	if command -v node >/dev/null 2>&1; then
		node_major="$(node -v 2>/dev/null | sed -E 's/^v([0-9]+).*/\1/')"
	fi
	if [ -z "$node_major" ] || ! [ "$node_major" -ge 18 ] 2>/dev/null; then
		# Distro-default Node packages are frequently too old (or entirely
		# absent) for a modern Vite/Vue build - NodeSource's setup script is
		# the standard way to get a current LTS on apt/dnf/yum systems.
		if command -v apt-get >/dev/null 2>&1; then
			curl -fsSL https://deb.nodesource.com/setup_20.x | bash - >/dev/null 2>&1 || true
			pkg_install nodejs
		elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
			curl -fsSL https://rpm.nodesource.com/setup_20.x | bash - >/dev/null 2>&1 || true
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
# Summary Card Display
# ------------------------------------------------------------------------------
print_summary() {
	local end_ts
	end_ts=$(date +%s)
	local total_duration=$((end_ts - START_TIME))

	get_term_size
	local box_width=$((TERM_COLS - 2))
	if [ "$box_width" -lt 40 ]; then box_width=40; fi
	local inner_width=$((box_width - 6))

	local bot_dashes=""
	for ((d=0; d<box_width-2; d++)); do bot_dashes+="─"; done

	local action_title="🎉  NivaroOS Installed Successfully!"
	if [ "$IS_UPGRADE" = "true" ]; then
		action_title="🎉  NivaroOS Updated Successfully!"
	fi
	local top_title=" ${action_title} (completed in ${total_duration}s) "
	local top_dashes_len=$((box_width - ${#top_title} - 4))
	if [ "$top_dashes_len" -lt 2 ]; then top_dashes_len=2; fi
	local top_dashes=""
	for ((d=0; d<top_dashes_len; d++)); do top_dashes+="─"; done

	printf "\n"
	printf '%b\n' "${COLOR_GREEN}╭──${COLOR_BOLD}${COLOR_GREEN}${top_title}${COLOR_RESET}${COLOR_GREEN}${top_dashes}╮${COLOR_RESET}"

	render_sum_line() {
		local text="$1"
		local plain
		plain="$(strip_ansi "$text")"
		if [ "${#plain}" -gt "$inner_width" ]; then
			plain="${plain:0:$inner_width}"
		fi
		local pad_len=$((inner_width - ${#plain}))
		local pad=""
		if [ "$pad_len" -gt 0 ]; then
			pad="$(printf '%*s' "$pad_len" '')"
		fi
		printf '%b\n' "${COLOR_GREEN}│${COLOR_RESET}  ${text}${pad}  ${COLOR_GREEN}│${COLOR_RESET}"
	}

	render_sum_line ""
	render_sum_line "${COLOR_BOLD}${COLOR_WHITE}Access your Web Dashboard at:${COLOR_RESET}"

	local port_suffix=""
	if [ -n "$DETECTED_PORT" ] && [ "$DETECTED_PORT" != "80" ]; then
		port_suffix=":${DETECTED_PORT}"
	fi

	local ips
	ips="$(list_reachable_ips)"
	if [ -n "$ips" ]; then
		while read -r ip iface; do
			[ -z "$ip" ] && continue
			local url="http://${ip}${port_suffix}"
			render_sum_line "${COLOR_CYAN}➜${COLOR_RESET}  ${COLOR_BOLD}${url}${COLOR_RESET} ${COLOR_MUTED}(${iface})${COLOR_RESET}"
		done <<< "$ips"
	fi

	local vpn_ips
	vpn_ips="$(list_vpn_ips)"
	if [ -n "$vpn_ips" ]; then
		while read -r vip viface; do
			[ -z "$vip" ] && continue
			local vurl="http://${vip}${port_suffix}"
			render_sum_line "${COLOR_PURPLE}➜${COLOR_RESET}  ${COLOR_BOLD}${vurl}${COLOR_RESET} ${COLOR_MUTED}(${viface} VPN)${COLOR_RESET}"
		done <<< "$vpn_ips"
	fi

	render_sum_line "${COLOR_CYAN}➜${COLOR_RESET}  ${COLOR_BOLD}http://localhost${port_suffix}${COLOR_RESET} ${COLOR_MUTED}(local)${COLOR_RESET}"
	render_sum_line ""
	render_sum_line "${COLOR_BOLD}${COLOR_WHITE}System Services Status:${COLOR_RESET}"

	local s_core="${COLOR_GREEN}✔ Core Engine${COLOR_RESET}"
	local s_gw="${COLOR_GREEN}✔ Gateway${COLOR_RESET}"
	local s_mb="${COLOR_GREEN}✔ Message Bus${COLOR_RESET}"
	local s_app="${COLOR_GREEN}✔ App Management${COLOR_RESET}"
	local s_ls="${COLOR_GREEN}✔ Local Storage${COLOR_RESET}"
	local s_usr="${COLOR_GREEN}✔ User Service${COLOR_RESET}"
	local s_gpu="${COLOR_GREEN}✔ GPU Sidecar${COLOR_RESET}"

	if ! systemctl is-active --quiet nivaroos.service 2>/dev/null; then s_core="${COLOR_RED}✖ Core Engine${COLOR_RESET}"; fi
	if ! systemctl is-active --quiet nivaroos-gateway.service 2>/dev/null; then s_gw="${COLOR_RED}✖ Gateway${COLOR_RESET}"; fi
	if ! systemctl is-active --quiet nivaroos-message-bus.service 2>/dev/null; then s_mb="${COLOR_RED}✖ Message Bus${COLOR_RESET}"; fi
	if ! systemctl is-active --quiet nivaroos-app-management.service 2>/dev/null; then s_app="${COLOR_RED}✖ App Management${COLOR_RESET}"; fi
	if ! systemctl is-active --quiet nivaroos-local-storage.service 2>/dev/null; then s_ls="${COLOR_RED}✖ Local Storage${COLOR_RESET}"; fi
	if ! systemctl is-active --quiet nivaroos-user-service.service 2>/dev/null; then s_usr="${COLOR_RED}✖ User Service${COLOR_RESET}"; fi
	if ! systemctl is-active --quiet nivaroos-gpu-sidecar.service 2>/dev/null; then s_gpu="${COLOR_RED}✖ GPU Sidecar${COLOR_RESET}"; fi

	render_sum_line "${s_core}    ${s_gw}    ${s_mb}"
	render_sum_line "${s_app}    ${s_ls}    ${s_usr}"

	if [ "$WITH_VM" = "yes" ]; then
		local s_vm="${COLOR_GREEN}✔ VM Virtualization${COLOR_RESET}"
		if ! systemctl is-active --quiet nivaroos-vm-sidecar.service 2>/dev/null; then s_vm="${COLOR_RED}✖ VM Virtualization${COLOR_RESET}"; fi
		render_sum_line "${s_gpu}    ${s_vm}"
	else
		render_sum_line "${s_gpu}    ${COLOR_MUTED}○ VM Virtualization (Off)${COLOR_RESET}"
	fi

	if [ "$WITH_HOST_DESKTOP" = "yes" ]; then
		local s_hd="${COLOR_GREEN}✔ Host Desktop${COLOR_RESET}"
		if ! systemctl is-active --quiet nivaroos-host-desktop.service 2>/dev/null; then s_hd="${COLOR_RED}✖ Host Desktop${COLOR_RESET}"; fi
		local hd_de_note=""
		if [ -n "$DETECTED_DE_NAME" ]; then
			hd_de_note=" ${COLOR_MUTED}(${DETECTED_DE_NAME} via ${DETECTED_XSESSION_NAME:-X11})${COLOR_RESET}"
		fi
		render_sum_line "${s_hd}${hd_de_note}"
	else
		render_sum_line "${COLOR_MUTED}○ Host Desktop (Off)${COLOR_RESET}"
	fi

	local s_smb="${COLOR_GREEN}✔ Samba File Sharing${COLOR_RESET}"
	if ! (systemctl is-active --quiet smbd 2>/dev/null || systemctl is-active --quiet smb 2>/dev/null || systemctl is-active --quiet samba 2>/dev/null); then
		s_smb="${COLOR_RED}✖ Samba File Sharing${COLOR_RESET}"
	fi
	local s_mdns="${COLOR_GREEN}✔ mDNS Discovery${COLOR_RESET}"
	if ! systemctl is-active --quiet avahi-daemon 2>/dev/null; then
		s_mdns="${COLOR_RED}✖ mDNS Discovery${COLOR_RESET}"
	fi
	render_sum_line "${s_smb}    ${s_mdns}"

	render_sum_line ""
	render_sum_line "${COLOR_BOLD}${COLOR_WHITE}Quick Start Commands:${COLOR_RESET}"
	render_sum_line "• Management CLI:     ${COLOR_CYAN}nivaroos --help${COLOR_RESET}"
	render_sum_line "• System Status:      ${COLOR_CYAN}nivaroos healthcheck${COLOR_RESET}"
	render_sum_line "• View Live Logs:     ${COLOR_CYAN}journalctl -u nivaroos -f${COLOR_RESET}"
	render_sum_line "• Service Controls:   ${COLOR_CYAN}systemctl restart nivaroos-gateway${COLOR_RESET}"
	render_sum_line "• Uninstall NivaroOS: ${COLOR_CYAN}nivaroos-uninstall${COLOR_RESET}"
	render_sum_line ""
	printf '%b\n\n' "${COLOR_GREEN}╰${bot_dashes}╯${COLOR_RESET}"
}

# ------------------------------------------------------------------------------
# Main Execution Pipeline
# ------------------------------------------------------------------------------
main() {
	START_TIME=$(date +%s)
	parse_args "$@"
	check_root "$@"
	init_logging
	check_distro
	check_resources
	print_banner
	print_diagnostics_card
	resolve_port_conflict
	select_components

	if [ "$IS_UPGRADE" = "true" ]; then
		info "Starting NivaroOS automated upgrade pipeline..."
	else
		info "Starting NivaroOS automated installation pipeline..."
	fi
	printf "\n"

	install_core_dependencies
	tune_system_limits
	check_docker
	clone_or_update_repo
	install_core_services
	install_samba

	if [ "$WITH_VM" = "yes" ]; then
		install_vm_manager
	fi

	if [ "$WITH_HOST_DESKTOP" = "yes" ]; then
		provision_desktop_environment
		install_host_desktop
	fi

	install_ui
	start_core_services
	verify_health
	install_uninstall_wrapper
	install_mdns_advertisement

	print_summary
}

main "$@"
