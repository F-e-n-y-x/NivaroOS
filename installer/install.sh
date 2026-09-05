#!/usr/bin/env bash
# ==============================================================================
#  NivaroOS Installer Script
#  Modern Self-Hosted Personal Cloud & Container Platform
#  GitHub: https://github.com/F-e-n-y-x/NivaroOS
# ==============================================================================
#
# Supported Environments:
#   Debian 11+, Ubuntu 20.04+, Linux Mint, Pop!_OS, Raspberry Pi OS,
#   CentOS/RHEL/Rocky/AlmaLinux 8+, Fedora 38+, Arch Linux, openSUSE, Alpine
#
# Quick Install:
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
WITH_VM=""
YES=""
DEBUG=""
STEP_NUM=0
TOTAL_STEPS=9
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

# ------------------------------------------------------------------------------
# Real-Time Terminal Dimension Detection
# ------------------------------------------------------------------------------
TERM_COLS=80
TERM_ROWS=24

get_term_size() {
	local rows=24 cols=80
	if [ -e /dev/tty ]; then
		local stty_out
		stty_out="$(stty size </dev/tty 2>/dev/null || true)"
		if [ -n "$stty_out" ]; then
			rows="$(echo "$stty_out" | awk '{print $1}')"
			cols="$(echo "$stty_out" | awk '{print $2}')"
		fi
	fi
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
	if [ -z "$cols" ] || [ "$cols" -le 0 ] 2>/dev/null; then
		if command -v tput >/dev/null 2>&1; then
			cols="$(tput cols 2>/dev/null || true)"
			rows="$(tput lines 2>/dev/null || true)"
		fi
	fi
	if [ -z "$cols" ] || [ "$cols" -le 0 ] 2>/dev/null; then
		cols="${COLUMNS:-80}"
		rows="${LINES:-24}"
	fi
	if [ "$cols" -lt 40 ] 2>/dev/null; then cols=80; fi
	if [ "$rows" -lt 10 ] 2>/dev/null; then rows=24; fi
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
	local inner_width=$((box_width - 4))

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

	local title_tag="System Diagnostics"
	local top_dashes_len=$((box_width - ${#title_tag} - 6))
	if [ "$top_dashes_len" -lt 2 ]; then top_dashes_len=2; fi
	local top_dashes=""
	for ((d=0; d<top_dashes_len; d++)); do top_dashes+="─"; done

	local bot_dashes=""
	for ((d=0; d<box_width-2; d++)); do bot_dashes+="─"; done

	printf '%b' "${COLOR_MUTED}╭── ${COLOR_BOLD}${COLOR_WHITE}${title_tag}${COLOR_RESET}${COLOR_MUTED} ${top_dashes}╮${COLOR_RESET}\n"
	
	render_diag_line() {
		local label="$1" val="$2"
		local clean_text="• ${label}: ${val}"
		local pad_len=$((inner_width - ${#clean_text}))
		if [ "$pad_len" -lt 0 ]; then pad_len=0; fi
		local pad=""
		for ((p=0; p<pad_len; p++)); do pad+=" "; done
		printf '%b' "│  ${COLOR_CYAN}•${COLOR_RESET} ${COLOR_MUTED}${label}:${COLOR_RESET} ${COLOR_WHITE}${val}${COLOR_RESET}${pad}  ${COLOR_MUTED}│${COLOR_RESET}\n"
	}

	render_diag_line "Operating System" "${os_name} (${arch})"
	render_diag_line "Linux Kernel    " "${kernel}"
	render_diag_line "System Memory   " "${mem_gb_str}"
	render_diag_line "Free Disk on /  " "${disk_gb_str}"
	render_diag_line "Virtualization  " "${kvm_status}"
	render_diag_line "Docker Engine   " "${docker_status}"

	printf '%b' "${COLOR_MUTED}╰${bot_dashes}╯${COLOR_RESET}\n\n"
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
		*" alpine "*) ;;
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

	disk_gb="$(($(LC_ALL=C df -P / 2>/dev/null | tail -n 1 | awk '{print $4}') / 1024 / 1024))"

	if [ -n "$mem_mb" ] && [ "$mem_mb" -gt 0 ]; then
		if [ "$mem_mb" -lt "$MIN_REQUIRED_MEMORY_MB" ]; then
			error "Only ${mem_mb}MB of memory detected - NivaroOS requires at least ${MIN_REQUIRED_MEMORY_MB}MB to install."
			exit 1
		elif [ "$mem_mb" -lt "$MIN_RECOMMENDED_MEMORY_MB" ]; then
			warn "Only ${mem_mb}MB of memory detected - ${MIN_RECOMMENDED_MEMORY_MB}MB+ is recommended for optimal performance."
		fi
	fi
	if [ -n "$disk_gb" ] && [ "$disk_gb" -ge 0 ]; then
		if [ "$disk_gb" -lt "$MIN_REQUIRED_DISK_GB" ]; then
			error "Only ${disk_gb}GB of free disk space on / - NivaroOS requires at least ${MIN_REQUIRED_DISK_GB}GB."
			exit 1
		elif [ "$disk_gb" -lt "$MIN_RECOMMENDED_DISK_GB" ]; then
			warn "Only ${disk_gb}GB of free disk space on / - ${MIN_RECOMMENDED_DISK_GB}GB+ is recommended."
		fi
	fi
}

# ------------------------------------------------------------------------------
# Port Conflict Detection & Resolution
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
	local target_port="${CUSTOM_PORT:-80}"

	if ! is_port_in_use "$target_port"; then
		DETECTED_PORT="$target_port"
		return 0
	fi

	local conflict_proc
	conflict_proc="$(find_process_on_port "$target_port")"
	[ -z "$conflict_proc" ] && conflict_proc="another web service / container"

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
		shift
	done
}

select_addons() {
	if [ -n "$WITH_VM" ]; then
		if [ "$WITH_VM" = "yes" ]; then
			TOTAL_STEPS=10
		fi
		return
	fi

	# Default to auto-detecting KVM in non-interactive mode
	if [ -n "$YES" ] || [ ! -t 0 ]; then
		if [ -e /dev/kvm ]; then
			WITH_VM=yes
			TOTAL_STEPS=10
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
		TOTAL_STEPS=10
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
	local inner_width=$((box_width - 4))

	local bot_dashes=""
	for ((d=0; d<box_width-2; d++)); do bot_dashes+="─"; done

	local top_dashes_len=$((box_width - 24))
	if [ "$top_dashes_len" -lt 2 ]; then top_dashes_len=2; fi
	local top_dashes=""
	for ((d=0; d<top_dashes_len; d++)); do top_dashes+="─"; done

	printf "\n"
	printf '%b' "${COLOR_RED}╭── ${COLOR_BOLD}Installation Failed${COLOR_RESET}${COLOR_RED} ${top_dashes}╮${COLOR_RESET}\n"
	printf '%b' "│  ${COLOR_RED}✖ Step Failed:${COLOR_RESET}  ${COLOR_WHITE}${title}${COLOR_RESET}\n"
	printf '%b' "│  ${COLOR_RED}✖ Exit Code:${COLOR_RESET}    ${COLOR_WHITE}${exit_code}${COLOR_RESET}\n"
	printf '%b' "│\n"
	printf '%b' "│  ${COLOR_BOLD}Recent Log Output:${COLOR_RESET}\n"
	printf '%b' "${COLOR_RED}├${bot_dashes}┤${COLOR_RESET}\n"

	if [ -f "$log_file" ] && [ -s "$log_file" ]; then
		while IFS= read -r line; do
			local clean_l
			clean_l=$(printf '%s' "$line" | tr '\r\t' '  ' | cut -c 1-"$inner_width")
			printf "│  %b\n" "${COLOR_MUTED}${clean_l}${COLOR_RESET}"
		done < <(tail -n 14 "$log_file")
	else
		printf "│  %b\n" "${COLOR_MUTED}(No detailed log output captured)${COLOR_RESET}"
	fi

	printf '%b' "${COLOR_RED}├${bot_dashes}┤${COLOR_RESET}\n"
	printf '%b' "│  ${COLOR_BOLD}Troubleshooting Tips:${COLOR_RESET}\n"
	printf '%b' "│  • Full installation log saved to: ${COLOR_CYAN}${INSTALL_LOG}${COLOR_RESET}\n"
	printf '%b' "│  • Verify internet connectivity and package mirrors.\n"
	printf '%b' "│  • Report issues at: ${COLOR_CYAN}https://github.com/F-e-n-y-x/NivaroOS/issues${COLOR_RESET}\n"
	printf '%b' "${COLOR_RED}╰${bot_dashes}╯${COLOR_RESET}\n\n"
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
# Full-Width Responsive Split-Pane Live Stream Step Runner (10 Live Activity Lines)
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
			local inner_width=$((box_width - 4))

			# 10 Live Activity Lines by default, adaptable if terminal is small/tall
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

			# Bottom Half: Full-Width Edge-to-Edge Live Activity Box
			local title_tag="Live Activity"
			local top_dashes_len=$((box_width - ${#title_tag} - 6))
			if [ "$top_dashes_len" -lt 2 ]; then top_dashes_len=2; fi
			local top_dashes=""
			for ((d=0; d<top_dashes_len; d++)); do top_dashes+="─"; done

			printf "\r\033[2K%b╭── %b%s%b %s╮%b\n" \
				"${COLOR_MUTED}" "${COLOR_CYAN}" "${title_tag}" "${COLOR_MUTED}" "${top_dashes}" "${COLOR_RESET}"

			local lines=()
			if [ -f "$log_file" ] && [ -s "$log_file" ]; then
				mapfile -t lines < <(tail -n "$num_log_lines" "$log_file" 2>/dev/null || true)
			fi

			local pad_count=$((num_log_lines - ${#lines[@]}))
			for ((p=0; p<pad_count; p++)); do
				printf "\r\033[2K%b│%b  %-*s  %b│%b\n" \
					"${COLOR_MUTED}" "${COLOR_MUTED}" "$inner_width" "..." "${COLOR_MUTED}" "${COLOR_RESET}"
			done

			for l in "${lines[@]}"; do
				local clean_l
				clean_l=$(printf '%s' "$l" | tr '\r\t' '  ' | cut -c 1-"$inner_width")
				printf "\r\033[2K%b│%b  %-*s  %b│%b\n" \
					"${COLOR_MUTED}" "${COLOR_WHITE}" "$inner_width" "$clean_l" "${COLOR_MUTED}" "${COLOR_RESET}"
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

		local d_running=false
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
			git checkout \"$BRANCH\"
			git pull origin \"$BRANCH\" || true
		else
			# If directory has contents without .git, clean it up
			if [ -d \"$SRC_DIR\" ] && [ \"\$(ls -A \"$SRC_DIR\" 2>/dev/null)\" ]; then
				rm -rf \"${SRC_DIR:?}\"/* \"${SRC_DIR:?}\"/.[!.]* 2>/dev/null || true
			fi
			git clone --branch \"$BRANCH\" --depth 1 \"$REPO_URL\" \"$SRC_DIR\"
		fi

		# Ensure Go toolchain is installed
		if ! command -v go >/dev/null 2>&1; then
			local go_arch=\"amd64\"
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

		mkdir -p /var/lib/nivaroos /var/run/nivaroos /etc/nivaroos /DATA/AppData /DATA/Documents /DATA/Downloads /DATA/Media /DATA/Gallery
		touch \"$MANIFEST_FILE\"

		# Compile Core Engine
		go build -o /usr/bin/nivaroos ./cmd/nivaroos
		echo '/usr/bin/nivaroos' >> \"$MANIFEST_FILE\"

		# Compile Gateway
		cd \"${SRC_DIR}/cmd/gateway\"
		go build -o /usr/bin/nivaroos-gateway .
		echo '/usr/bin/nivaroos-gateway' >> \"$MANIFEST_FILE\"

		# Compile Message Bus
		cd \"${SRC_DIR}/cmd/message-bus\"
		go build -o /usr/bin/nivaroos-message-bus .
		echo '/usr/bin/nivaroos-message-bus' >> \"$MANIFEST_FILE\"

		# Compile App Management
		cd \"${SRC_DIR}/cmd/app-management\"
		go build -o /usr/bin/nivaroos-app-management .
		echo '/usr/bin/nivaroos-app-management' >> \"$MANIFEST_FILE\"

		# Compile Local Storage
		cd \"${SRC_DIR}/cmd/local-storage\"
		go build -o /usr/bin/nivaroos-local-storage .
		echo '/usr/bin/nivaroos-local-storage' >> \"$MANIFEST_FILE\"

		# Compile User Service
		cd \"${SRC_DIR}/cmd/user-service\"
		go build -o /usr/bin/nivaroos-user-service .
		echo '/usr/bin/nivaroos-user-service' >> \"$MANIFEST_FILE\"

		# Compile GPU Sidecar
		cd \"${SRC_DIR}/cmd/gpu-sidecar\"
		go build -o /usr/bin/nivaroos-gpu-sidecar .
		echo '/usr/bin/nivaroos-gpu-sidecar' >> \"$MANIFEST_FILE\"

		# Compile Unified Management CLI
		cd \"${SRC_DIR}/cmd/cli\"
		go build -o /usr/bin/nivaroos-cli .
		echo '/usr/bin/nivaroos-cli' >> \"$MANIFEST_FILE\"
		ln -sf /usr/bin/nivaroos-cli /usr/local/bin/nivaroos 2>/dev/null || true
		ln -sf /usr/bin/nivaroos-cli /usr/bin/casaos-cli 2>/dev/null || true

		# Install systemd service units
		cd \"$SRC_DIR\"
		local units=(
			\"build/sysroot/usr/lib/systemd/system/nivaroos.service:/usr/lib/systemd/system/nivaroos.service\"
			\"build/sysroot/usr/lib/systemd/system/nivaroos-gateway.service:/usr/lib/systemd/system/nivaroos-gateway.service\"
			\"build/sysroot/usr/lib/systemd/system/nivaroos-message-bus.service:/usr/lib/systemd/system/nivaroos-message-bus.service\"
			\"build/sysroot/usr/lib/systemd/system/nivaroos-app-management.service:/usr/lib/systemd/system/nivaroos-app-management.service\"
			\"build/sysroot/usr/lib/systemd/system/nivaroos-local-storage.service:/usr/lib/systemd/system/nivaroos-local-storage.service\"
			\"build/sysroot/usr/lib/systemd/system/nivaroos-user-service.service:/usr/lib/systemd/system/nivaroos-user-service.service\"
			\"build/sysroot/usr/lib/systemd/system/nivaroos-gpu-sidecar.service:/usr/lib/systemd/system/nivaroos-gpu-sidecar.service\"
			\"build/sysroot/usr/lib/systemd/system/rclone.service:/usr/lib/systemd/system/rclone.service\"
			\"build/sysroot/usr/lib/systemd/system/usb-mount@.service:/usr/lib/systemd/system/usb-mount@.service\"
		)

		mkdir -p /usr/lib/systemd/system
		for u in \"\${units[@]}\"; do
			local src=\"\${u%%:*}\"
			local dst=\"\${u##*:}\"
			if [ -f \"\$src\" ]; then
				cp -f \"\$src\" \"\$dst\"
				echo \"\$dst\" >> \"$MANIFEST_FILE\"
			fi
		done

		# Apply Custom Port configuration if specified
		if [ -n \"$DETECTED_PORT\" ] && [ \"$DETECTED_PORT\" != \"80\" ]; then
			mkdir -p /etc/nivaroos
			cat > /etc/nivaroos/gateway.ini <<GWCONF
[gateway]
port = ${DETECTED_PORT}
GWCONF
			echo '/etc/nivaroos/gateway.ini' >> \"$MANIFEST_FILE\"
		fi

		# Sort manifest uniquely
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
		fi

		cd \"${SRC_DIR}/cmd/vm-sidecar\"
		go build -o /usr/bin/nivaroos-vm-sidecar .
		echo '/usr/bin/nivaroos-vm-sidecar' >> \"$MANIFEST_FILE\"

		if [ -f \"${SRC_DIR}/build/sysroot/usr/lib/systemd/system/nivaroos-vm-sidecar.service\" ]; then
			cp -f \"${SRC_DIR}/build/sysroot/usr/lib/systemd/system/nivaroos-vm-sidecar.service\" /usr/lib/systemd/system/nivaroos-vm-sidecar.service
			echo '/usr/lib/systemd/system/nivaroos-vm-sidecar.service' >> \"$MANIFEST_FILE\"
		fi

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
		if [ -d \"${SRC_DIR}/ui/dist\" ]; then
			cp -rf \"${SRC_DIR}/ui/dist\"/* /var/lib/nivaroos/www/ 2>/dev/null || true
		elif [ -d \"${SRC_DIR}/build/sysroot/var/lib/nivaroos/www\" ]; then
			cp -rf \"${SRC_DIR}/build/sysroot/var/lib/nivaroos/www\"/* /var/lib/nivaroos/www/ 2>/dev/null || true
		elif [ -d \"${SRC_DIR}/build/sysroot/var/lib/casaos/www\" ]; then
			cp -rf \"${SRC_DIR}/build/sysroot/var/lib/casaos/www\"/* /var/lib/nivaroos/www/ 2>/dev/null || true
		fi
	"
}

# ------------------------------------------------------------------------------
# Service Activation
# ------------------------------------------------------------------------------
start_core_services() {
	run_step "Reloading System Daemons & Starting Services" "
		systemctl daemon-reload
		local services=(
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
		local target_port=\"${DETECTED_PORT:-80}\"
		local healthy=false

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
			# Verify whether processes are active via systemctl
			if systemctl is-active --quiet nivaroos-gateway; then
				healthy=true
			fi
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
	local inner_width=$((box_width - 4))

	local bot_dashes=""
	for ((d=0; d<box_width-2; d++)); do bot_dashes+="─"; done

	local top_title="🎉  NivaroOS Installed Successfully! (completed in ${total_duration}s)"
	local top_dashes_len=$((box_width - ${#top_title} - 4))
	if [ "$top_dashes_len" -lt 2 ]; then top_dashes_len=2; fi
	local top_dashes=""
	for ((d=0; d<top_dashes_len; d++)); do top_dashes+="─"; done

	printf "\n"
	printf '%b\n' "${COLOR_GREEN}╭── ${COLOR_BOLD}${COLOR_GREEN}${top_title}${COLOR_RESET}${COLOR_GREEN} ${top_dashes}╮${COLOR_RESET}"
	printf '%b\n' "│"
	printf '%b\n' "│   ${COLOR_BOLD}${COLOR_WHITE}Access your Web Dashboard at:${COLOR_RESET}"

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
			printf "│   ${COLOR_CYAN}➜${COLOR_RESET}  ${COLOR_BOLD}%-32s${COLOR_RESET} ${COLOR_MUTED}(%s)${COLOR_RESET}\n" "$url" "$iface"
		done <<< "$ips"
	fi

	local vpn_ips
	vpn_ips="$(list_vpn_ips)"
	if [ -n "$vpn_ips" ]; then
		while read -r vip viface; do
			[ -z "$vip" ] && continue
			local vurl="http://${vip}${port_suffix}"
			printf "│   ${COLOR_PURPLE}➜${COLOR_RESET}  ${COLOR_BOLD}%-32s${COLOR_RESET} ${COLOR_MUTED}(%s VPN)${COLOR_RESET}\n" "$vurl" "$viface"
		done <<< "$vpn_ips"
	fi

	printf "│   ${COLOR_CYAN}➜${COLOR_RESET}  ${COLOR_BOLD}%-32s${COLOR_RESET} ${COLOR_MUTED}(local)${COLOR_RESET}\n" "http://localhost${port_suffix}"

	printf '%b\n' "│"
	printf '%b\n' "│   ${COLOR_BOLD}${COLOR_WHITE}System Services Status:${COLOR_RESET}"

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

	printf "│   %-32b %-32b\n" "$s_core" "$s_gw"
	printf "│   %-32b %-32b\n" "$s_mb" "$s_app"
	printf "│   %-32b %-32b\n" "$s_ls" "$s_usr"

	if [ "$WITH_VM" = "yes" ]; then
		local s_vm="${COLOR_GREEN}✔ VM Virtualization${COLOR_RESET}"
		if ! systemctl is-active --quiet nivaroos-vm-sidecar.service 2>/dev/null; then s_vm="${COLOR_RED}✖ VM Virtualization${COLOR_RESET}"; fi
		printf "│   %-32b %-32b\n" "$s_gpu" "$s_vm"
	else
		printf "│   %-32b %-32b\n" "$s_gpu" "${COLOR_MUTED}○ VM Virtualization (Off)${COLOR_RESET}"
	fi

	printf '%b\n' "│"
	printf '%b\n' "│   ${COLOR_BOLD}${COLOR_WHITE}Quick Start Commands:${COLOR_RESET}"
	printf '%b\n' "│   • Management CLI:     ${COLOR_CYAN}nivaroos --help${COLOR_RESET}"
	printf '%b\n' "│   • System Status:      ${COLOR_CYAN}nivaroos status${COLOR_RESET}"
	printf '%b\n' "│   • View Live Logs:     ${COLOR_CYAN}journalctl -u nivaroos -f${COLOR_RESET}"
	printf '%b\n' "│   • Service Controls:   ${COLOR_CYAN}systemctl restart nivaroos-gateway${COLOR_RESET}"
	printf '%b\n' "│   • Uninstall NivaroOS: ${COLOR_CYAN}nivaroos-uninstall${COLOR_RESET}"
	printf '%b\n' "│"
	printf '%b\n\n' "${COLOR_GREEN}╰${bot_dashes}╯${COLOR_RESET}"
}

# ------------------------------------------------------------------------------
# Main Execution Pipeline
# ------------------------------------------------------------------------------
main() {
	START_TIME=$(date +%s)
	init_logging
	parse_args "$@"
	check_root
	check_distro
	check_resources
	print_banner
	print_diagnostics_card
	resolve_port_conflict
	select_addons

	info "Starting NivaroOS automated installation pipeline..."
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

	print_summary
}

main "$@"
