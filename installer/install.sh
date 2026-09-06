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

set -euo pipefail
shopt -s checkwinsize 2>/dev/null || true

# ------------------------------------------------------------------------------
# Configuration & Constants
# ------------------------------------------------------------------------------
REPO_URL="https://github.com/F-e-n-y-x/NivaroOS.git"
BRANCH="master"
SRC_DIR="/opt/nivaroos/src"
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
YES=""
DEBUG=""
CLI_WIDTH=""
CLI_HEIGHT=""
STEP_NUM=0
TOTAL_STEPS=10
START_TIME=0
DATE_TAG="$(date +'%Y%m%d-%H%M%S')"

LOG_DIR="/var/log/nivaroos"
INSTALL_LOG="${LOG_DIR}/install-${DATE_TAG}.log"
LATEST_LOG="${LOG_DIR}/install.log"
MANIFEST_FILE="/var/lib/nivaroos/manifest"

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

cleanup_on_exit() {
	if [ "$IS_TTY" = "true" ]; then
		printf "\033[?25h" # Restore cursor
	fi
}
trap cleanup_on_exit EXIT INT TERM

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
		local clean_text="• ${label}: ${val}"
		if [ "${#clean_text}" -gt "$inner_width" ]; then
			clean_text="${clean_text:0:$inner_width}"
		fi
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
			if [ -n "$YES" ] || [ ! -t 0 ]; then
				DETECTED_PORT="$CUSTOM_PORT"
				return 0
			fi
		else
			warn "Port 80 is currently in use by ${conflict_proc}."
		fi

		if [ -n "$YES" ] || [ ! -t 0 ]; then
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
				printf '%b\n' "  ${COLOR_CYAN}-y, --yes${COLOR_RESET}             Automatic non-interactive installation (accept all defaults)"
				printf '%b\n' "  ${COLOR_CYAN}--with-vm${COLOR_RESET}             Install VM Manager with QEMU/KVM, libvirt & web console"
				printf '%b\n' "  ${COLOR_CYAN}--without-vm${COLOR_RESET}          Skip VM Manager installation (can be enabled later via CLI)"
				printf '%b\n' "  ${COLOR_CYAN}--port <port>${COLOR_RESET}         Custom HTTP dashboard port (default: 80 or next free port)"
				printf '%b\n' "  ${COLOR_CYAN}--width <cols>${COLOR_RESET}        Force specific terminal box width (default: auto-detect)"
				printf '%b\n' "  ${COLOR_CYAN}--branch <branch>${COLOR_RESET}     Git branch or tag to install (default: master)"
				printf '%b\n' "  ${COLOR_CYAN}--debug${COLOR_RESET}               Show detailed verbose logs during installation"
				printf '%b\n' "  ${COLOR_CYAN}-h, --help${COLOR_RESET}            Display this help message and exit"
				printf '\n'
				exit 0
				;;
			*)
				error "Unknown argument '$1'. Run with --help to see available options."
				exit 1
				;;
		esac
		# The two-token flags above (--port, --width/-w, --branch/-b) already
		# shift once themselves to consume their value - if that value was
		# the last argument on the command line, $# is already 0 here, and
		# an unconditional shift would fail ("shift count out of range"),
		# which set -e turns into the whole installer aborting over a
		# missing flag value instead of just falling back to its default.
		[ $# -eq 0 ] || shift
	done
}

select_addons() {
	if [ -n "$WITH_VM" ]; then
		if [ "$WITH_VM" = "yes" ]; then
			TOTAL_STEPS=11
		fi
		return
	fi

	# Default to auto-detecting KVM in non-interactive mode
	if [ -n "$YES" ] || [ ! -t 0 ]; then
		if [ -e /dev/kvm ]; then
			WITH_VM=yes
			TOTAL_STEPS=11
		else
			WITH_VM=no
		fi
		return
	fi

	printf '%b\n' "${COLOR_BOLD}${COLOR_WHITE}Optional Add-on Components:${COLOR_RESET}"
	printf '%b\n' "  ${COLOR_PURPLE}◆${COLOR_RESET} ${COLOR_BOLD}VM Manager${COLOR_RESET} (Hardware Virtualization, QEMU/KVM, Libvirt, Web Console & VirtIO-FS)"
	if [ -e /dev/kvm ]; then
		printf '%b\n' "    ${COLOR_GREEN}✔ KVM Hardware Acceleration detected on this CPU.${COLOR_RESET}"
	else
		printf '%b\n' "    ${COLOR_YELLOW}ℹ KVM hardware acceleration not detected; software emulation will be used.${COLOR_RESET}"
	fi

	local prompt_default="Y/n"
	if [ ! -e /dev/kvm ]; then
		prompt_default="y/N"
	fi

	local reply=""
	printf '%b' "\n  ${COLOR_CYAN}?${COLOR_RESET} ${COLOR_BOLD}Enable VM Manager add-on?${COLOR_RESET} [${prompt_default}]: "
	read -r reply </dev/tty || reply=""

	if [ -z "$reply" ]; then
		if [ -e /dev/kvm ]; then
			WITH_VM=yes
		else
			WITH_VM=no
		fi
	else
		case "$reply" in
			[yY]|[yY][eE][sS]) WITH_VM=yes ;;
			*) WITH_VM=no ;;
		esac
	fi

	if [ "$WITH_VM" = "yes" ]; then
		TOTAL_STEPS=11
		printf '%b\n\n' "  ${COLOR_GREEN}✔${COLOR_RESET} VM Manager enabled."
	else
		printf '%b\n\n' "  ${COLOR_MUTED}○${COLOR_RESET} VM Manager skipped (can be enabled anytime via CLI: 'nivaroos vm enable')."
	fi
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
	local step_tag="[${STEP_NUM}/${TOTAL_STEPS}]"
	local start_ts
	start_ts=$(date +%s)

	log_raw ">>> START STEP ${STEP_NUM}/${TOTAL_STEPS}: ${title}"

	local log_file
	log_file=$(mktemp /tmp/nivaroos-install-step-XXXXXX.log)

	if [ "$DEBUG" = "yes" ]; then
		printf '%b\n' "  ${COLOR_CYAN}➜${COLOR_RESET} ${COLOR_BOLD}${COLOR_BLUE}${step_tag}${COLOR_RESET} ${COLOR_WHITE}${title}${COLOR_RESET} (verbose)..."
		if ! bash -c "export PATH=\"/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin:\$PATH\"; export DEBIAN_FRONTEND=noninteractive; export NEEDRESTART_MODE=a; $*" 2>&1 | tee -a "$INSTALL_LOG"; then
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
			eval "$*"
		) > "$log_file" 2>&1 </dev/null &
		local cmd_pid=$!

		local frame_idx=0
		local num_frames=${#SPINNER_FRAMES[@]}
		local first_render=true
		local last_rendered_lines=0

		printf "\033[?25l"

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

			# 10 Live Activity Lines by default, adaptable for small/tall terminals
			local num_log_lines=10
			if [ "$TERM_ROWS" -le 16 ]; then
				num_log_lines=$((TERM_ROWS - 6))
				if [ "$num_log_lines" -lt 4 ]; then num_log_lines=4; fi
			elif [ "$TERM_ROWS" -ge 42 ]; then
				num_log_lines=14
			fi
			local total_rendered_lines=$((num_log_lines + 3))

			if [ "$first_render" = "false" ]; then
				printf "\033[%dA" "$last_rendered_lines"
			else
				first_render=false
			fi
			last_rendered_lines="$total_rendered_lines"

			# Top Half: Progress Header with Animated Spinner & Live Timer
			printf "\r\033[2K  %b %b %b %b(%ds)%b\n" \
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

			printf "\r\033[2K%b╭──%b%s%b%s╮%b\n" \
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
				printf "\r\033[2K%b│%b  ...%s  %b│%b\n" "${COLOR_MUTED}" "${COLOR_MUTED}" "$empty_pad" "${COLOR_MUTED}" "${COLOR_RESET}"
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
				printf "\r\033[2K%b│%b  %s%s  %b│%b\n" \
					"${COLOR_MUTED}" "${COLOR_WHITE}" "$clean_l" "$pad" "${COLOR_MUTED}" "${COLOR_RESET}"
			done

			local bot_dashes=""
			for ((d=0; d<box_width-2; d++)); do bot_dashes+="─"; done
			printf "\r\033[2K%b╰%s╯%b\n" "${COLOR_MUTED}" "${bot_dashes}" "${COLOR_RESET}"

			frame_idx=$(( (frame_idx + 1) % num_frames ))
			sleep 0.08
		done

		wait "$cmd_pid"
		local exit_code=$?
		local end_ts
		end_ts=$(date +%s)
		local total_elapsed=$((end_ts - start_ts))

		cat "$log_file" >> "$INSTALL_LOG" 2>/dev/null || true

		# Cleanly erase the live activity pane on completion
		if [ "$first_render" = "false" ]; then
			printf "\033[%dA" "$last_rendered_lines"
			for ((c=0; c<last_rendered_lines; c++)); do
				printf "\r\033[2K\n"
			done
			printf "\033[%dA" "$last_rendered_lines"
		fi

		printf "\033[?25h"

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
			pkg_install curl wget git tar ca-certificates udev util-linux pciutils smartmontools parted build-essential
		elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
			pkg_install curl wget git tar ca-certificates systemd-udev util-linux pciutils smartmontools parted make gcc
		elif command -v pacman >/dev/null 2>&1; then
			pkg_install curl wget git tar ca-certificates systemd util-linux pciutils smartmontools parted base-devel
		elif command -v zypper >/dev/null 2>&1; then
			pkg_install curl wget git tar ca-certificates udev util-linux pciutils smartmontools parted make gcc
		elif command -v apk >/dev/null 2>&1; then
			pkg_install curl wget git tar ca-certificates udev util-linux pciutils smartmontools parted build-base
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
			sleep 1
		done

		if [ \"\$d_running\" = \"false\" ]; then
			echo \"Docker daemon failed to start or respond within 10 seconds.\" >&2
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
		if [ -d \"${SRC_DIR}/.git\" ]; then
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
			wget -q \"https://go.dev/dl/go${GO_VERSION}.linux-${go_arch}.tar.gz\" -O /tmp/go.tar.gz
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

		mkdir -p /var/lib/nivaroos /var/run/nivaroos /etc/nivaroos /DATA/AppData /DATA/Documents /DATA/Downloads /DATA/Media /DATA/Gallery
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
		if command -v apt-get >/dev/null 2>&1; then
			pkg_install qemu-system-x86 qemu-utils libvirt-daemon-system libvirt-clients virtinst bridge-utils ovmf cloud-image-utils
		elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
			pkg_install qemu-kvm qemu-img libvirt libvirt-client virt-install bridge-utils edk2-ovmf
		elif command -v pacman >/dev/null 2>&1; then
			pkg_install qemu-base libvirt virt-install bridge-utils edk2-ovmf
		elif command -v zypper >/dev/null 2>&1; then
			pkg_install qemu-kvm qemu-tools libvirt libvirt-client virt-install bridge-utils qemu-ovmf-x86_64
		elif command -v apk >/dev/null 2>&1; then
			pkg_install qemu-system-x86_64 qemu-img libvirt libvirt-daemon virt-install bridge dnsmasq ovmf
		else
			echo 'No known package manager found (apt/dnf/yum/pacman/zypper/apk) - cannot install QEMU/libvirt automatically. Skipping VM Manager; install those packages yourself and re-run with --with-vm.' >&2
			exit 1
		fi

		cd \"${SRC_DIR}/services/vm-sidecar\"
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
# Web Dashboard UI Assets
# ------------------------------------------------------------------------------
install_ui() {
	run_step "Deploying Web Dashboard & Frontend Assets" "
		mkdir -p /var/lib/nivaroos/www

		ui_source_dir=\"\"
		if [ -d \"${SRC_DIR}/ui/dist\" ]; then
			ui_source_dir=\"${SRC_DIR}/ui/dist\"
		elif [ -d \"${SRC_DIR}/build/sysroot/var/lib/nivaroos/www\" ]; then
			ui_source_dir=\"${SRC_DIR}/build/sysroot/var/lib/nivaroos/www\"
		elif [ -d \"${SRC_DIR}/build/sysroot/var/lib/casaos/www\" ]; then
			ui_source_dir=\"${SRC_DIR}/build/sysroot/var/lib/casaos/www\"
		fi

		# No prebuilt output shipped in this checkout - try to build it
		# ourselves, but only using tools already present on this system
		# (this installer doesn't set up a Node.js toolchain on its own,
		# across 8+ distro families, purely to build the UI once).
		if [ -z \"\$ui_source_dir\" ] && command -v pnpm >/dev/null 2>&1; then
			echo 'No prebuilt dashboard found - building the frontend from source with pnpm (this can take a few minutes)...'
			( cd \"${SRC_DIR}/ui\" && pnpm install --frozen-lockfile 2>/dev/null || pnpm install ) && \
			( cd \"${SRC_DIR}/ui\" && pnpm vue-cli-service build --dest \"${SRC_DIR}/build/sysroot/var/lib/nivaroos/www\" --mode production )
			if [ -d \"${SRC_DIR}/build/sysroot/var/lib/nivaroos/www\" ]; then
				ui_source_dir=\"${SRC_DIR}/build/sysroot/var/lib/nivaroos/www\"
			fi
		fi

		if [ -z \"\$ui_source_dir\" ]; then
			echo 'Could not find or build the web dashboard (no ui/dist, no build/sysroot output, and pnpm is not installed to build one). The rest of NivaroOS will run, but the dashboard will be empty until you build ui/ manually and re-run this installer.' >&2
			exit 1
		fi

		cp -rf \"\$ui_source_dir\"/* /var/lib/nivaroos/www/
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
			if curl -fsSL -m 2 \"http://127.0.0.1:${target_port}/ping\" >/dev/null 2>&1 || \
			   curl -fsSL -m 2 \"http://127.0.0.1:${target_port}/v1/sys/version\" >/dev/null 2>&1 || \
			   curl -fsSL -m 2 \"http://127.0.0.1:${target_port}/\" >/dev/null 2>&1; then
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
			echo \"Gateway did not respond on port ${target_port} after 20s\" >&2
			exit 1
		fi
	"
}

# ------------------------------------------------------------------------------
# mDNS Advertisement (lets the NivaroOS mobile app auto-discover this
# server on the local network instead of the user typing an IP address)
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

	render_sum_line ""
	render_sum_line "${COLOR_BOLD}${COLOR_WHITE}Quick Start Commands:${COLOR_RESET}"
	render_sum_line "• Management CLI:     ${COLOR_CYAN}nivaroos --help${COLOR_RESET}"
	render_sum_line "• System Status:      ${COLOR_CYAN}nivaroos status${COLOR_RESET}"
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
	init_logging
	parse_args "$@"
	check_root "$@"
	check_distro
	check_resources
	print_banner
	print_diagnostics_card
	resolve_port_conflict
	select_addons

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

	if [ "$WITH_VM" = "yes" ]; then
		install_vm_manager
	fi

	install_ui
	start_core_services
	verify_health
	install_uninstall_wrapper
	install_mdns_advertisement

	print_summary
}

main "$@"
