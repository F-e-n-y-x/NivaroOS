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
	local os_name="Linux"
	if [ -f "$OS_RELEASE_FILE" ]; then
		# shellcheck disable=SC1090
		. "$OS_RELEASE_FILE"
		os_name="${PRETTY_NAME:-$ID}"
	fi

	local arch
	arch="$(uname -m)"
	local kernel
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

	local disk_gb_str="Unknown"
	local disk_mb
	disk_mb="$(LC_ALL=C df -m / 2>/dev/null | tail -n 1 | awk '{print $4}' || echo "0")"
	if [ -n "$disk_mb" ] && [ "$disk_mb" -gt 0 ]; then
		disk_gb_str="$(awk "BEGIN {printf \"%.1f GB\", $disk_mb/1024}")"
	fi

	local kvm_status="${COLOR_GREEN}Supported (KVM Acceleration Available)${COLOR_RESET}"
	if [ ! -e /dev/kvm ]; then
		kvm_status="${COLOR_YELLOW}Not Detected (Emulation Only)${COLOR_RESET}"
	fi

	local docker_status="${COLOR_MUTED}Not Installed (Auto-installs during setup)${COLOR_RESET}"
	if command -v docker >/dev/null 2>&1; then
		local d_ver
		d_ver="$(docker version --format '{{.Server.Version}}' 2>/dev/null || docker -v 2>/dev/null | awk '{print $3}' | tr -d ',' || echo 'installed')"
		docker_status="${COLOR_GREEN}Installed (v${d_ver})${COLOR_RESET}"
	fi

	printf '%b' "${COLOR_MUTED}╭── ${COLOR_BOLD}${COLOR_WHITE}System Diagnostics${COLOR_RESET}${COLOR_MUTED} ──────────────────────────────────────────────────────────╮${COLOR_RESET}\n"
	printf '%b' "│  ${COLOR_CYAN}•${COLOR_RESET} ${COLOR_MUTED}Operating System:${COLOR_RESET}   ${COLOR_WHITE}${os_name} (${arch})${COLOR_RESET}\n"
	printf '%b' "│  ${COLOR_CYAN}•${COLOR_RESET} ${COLOR_MUTED}Linux Kernel:${COLOR_RESET}       ${COLOR_WHITE}${kernel}${COLOR_RESET}\n"
	printf '%b' "│  ${COLOR_CYAN}•${COLOR_RESET} ${COLOR_MUTED}System Memory:${COLOR_RESET}      ${COLOR_WHITE}${mem_gb_str}${COLOR_RESET}\n"
	printf '%b' "│  ${COLOR_CYAN}•${COLOR_RESET} ${COLOR_MUTED}Free Disk on /:${COLOR_RESET}     ${COLOR_WHITE}${disk_gb_str}${COLOR_RESET}\n"
	printf '%b' "│  ${COLOR_CYAN}•${COLOR_RESET} ${COLOR_MUTED}Virtualization:${COLOR_RESET}     ${kvm_status}\n"
	printf '%b' "│  ${COLOR_CYAN}•${COLOR_RESET} ${COLOR_MUTED}Docker Engine:${COLOR_RESET}      ${docker_status}\n"
	printf '%b' "${COLOR_MUTED}╰───────────────────────────────────────────────────────────────────────────────╯${COLOR_RESET}\n\n"
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

	# Find next open port starting from 8080
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

	printf "\n"
	printf '%b' "${COLOR_RED}╭── ${COLOR_BOLD}Installation Failed${COLOR_RESET}${COLOR_RED} ────────────────────────────────────────────────────────╮${COLOR_RESET}\n"
	printf '%b' "│  ${COLOR_RED}✖ Step Failed:${COLOR_RESET}  ${COLOR_WHITE}${title}${COLOR_RESET}\n"
	printf '%b' "│  ${COLOR_RED}✖ Exit Code:${COLOR_RESET}    ${COLOR_WHITE}${exit_code}${COLOR_RESET}\n"
	printf '%b' "│                                                                               │\n"
	printf '%b' "│  ${COLOR_BOLD}Recent Log Output:${COLOR_RESET}                                                           │\n"
	printf '%b' "${COLOR_RED}├───────────────────────────────────────────────────────────────────────────────┤${COLOR_RESET}\n"

	if [ -f "$log_file" ] && [ -s "$log_file" ]; then
		while IFS= read -r line; do
			printf "│  %b\n" "${COLOR_MUTED}${line:0:74}${COLOR_RESET}"
		done < <(tail -n 14 "$log_file")
	else
		printf "│  %b\n" "${COLOR_MUTED}(No detailed log output captured)${COLOR_RESET}"
	fi

	printf '%b' "${COLOR_RED}├───────────────────────────────────────────────────────────────────────────────┤${COLOR_RESET}\n"
	printf '%b' "│  ${COLOR_BOLD}Troubleshooting Tips:${COLOR_RESET}                                                       │\n"
	printf '%b' "│  • Full installation log saved to: ${COLOR_CYAN}${INSTALL_LOG}${COLOR_RESET}\n"
	printf '%b' "│  • Verify internet connectivity and package mirrors.                          │\n"
	printf '%b' "│  • Report issues at: ${COLOR_CYAN}https://github.com/F-e-n-y-x/NivaroOS/issues${COLOR_RESET}             │\n"
	printf '%b' "${COLOR_RED}╰───────────────────────────────────────────────────────────────────────────────╯${COLOR_RESET}\n\n"
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
# Asynchronous Step Runner with Smooth Animated Spinner
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

		printf "\033[?25l"

		while kill -0 "$cmd_pid" 2>/dev/null; do
			local current_ts
			current_ts=$(date +%s)
			local elapsed=$((current_ts - start_ts))
			local frame="${SPINNER_FRAMES[$frame_idx]}"
			
			printf "\r\033[2K  %b %b %b %b(%ds)%b" \
				"${COLOR_CYAN}${frame}${COLOR_RESET}" \
				"${COLOR_BOLD}${COLOR_BLUE}${step_tag}${COLOR_RESET}" \
				"${COLOR_WHITE}${title}${COLOR_RESET}" \
				"${COLOR_MUTED}" "${elapsed}" "${COLOR_RESET}"

			frame_idx=$(( (frame_idx + 1) % num_frames ))
			sleep 0.08
		done

		printf "\033[?25h"

		wait "$cmd_pid"
		local exit_code=$?
		local end_ts
		end_ts=$(date +%s)
		local total_elapsed=$((end_ts - start_ts))

		cat "$log_file" >> "$INSTALL_LOG" 2>/dev/null || true

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
		zypper install -y "${pkgs[@]}"
	elif command -v apk >/dev/null 2>&1; then
		apk add --no-cache "${pkgs[@]}"
	fi
}

# ------------------------------------------------------------------------------
# System & Kernel Tuning (Coolify / CasaOS Best Practices)
# ------------------------------------------------------------------------------
tune_system_limits() {
	run_step "Applying kernel inotify & system storage limits" "
		mkdir -p /etc/sysctl.d
		cat > /etc/sysctl.d/99-nivaroos.conf <<'EOF'
fs.inotify.max_user_watches=524288
fs.inotify.max_user_instances=512
fs.file-max=2097152
EOF
		sysctl -p /etc/sysctl.d/99-nivaroos.conf >/dev/null 2>&1 || true
	"
}

# ------------------------------------------------------------------------------
# Toolchain & Runtime Installation
# ------------------------------------------------------------------------------
go_version_ok() {
	local ver
	ver="$(go version 2>/dev/null | awk '{print $3}' | sed 's/^go//' || echo "0")"
	if [ "$ver" = "0" ]; then return 1; fi
	local major minor
	major="$(echo "$ver" | cut -d. -f1)"
	minor="$(echo "$ver" | cut -d. -f2)"
	if [ "$major" -gt 1 ] || { [ "$major" -eq 1 ] && [ "$minor" -ge 22 ]; }; then
		return 0
	fi
	return 1
}

install_go_toolchain() {
	local arch go_arch
	arch="$(uname -m)"
	case "$arch" in
		x86_64|amd64) go_arch=amd64 ;;
		aarch64|arm64) go_arch=arm64 ;;
		armv7l|armhf) go_arch=armv6l ;;
		*)
			error "Unsupported architecture '$arch' for automated Go toolchain."
			exit 1
			;;
	esac

	local tarball="go${GO_VERSION}.linux-${go_arch}.tar.gz"
	local dl_url="https://go.dev/dl/${tarball}"

	curl -fsSL "$dl_url" -o "/tmp/${tarball}"
	rm -rf /usr/local/go
	tar -C /usr/local -xzf "/tmp/${tarball}"
	rm -f "/tmp/${tarball}"

	export PATH="/usr/local/go/bin:$PATH"
	echo 'export PATH="/usr/local/go/bin:$PATH"' > /etc/profile.d/nivaroos-go.sh 2>/dev/null || true
	ln -sf /usr/local/go/bin/go /usr/bin/go
	ln -sf /usr/local/go/bin/gofmt /usr/bin/gofmt
}

install_core_dependencies() {
	run_step "Updating system package repositories" pkg_update

	local required_tools=(curl wget git jq tar gzip ca-certificates build-essential smartmontools hdparm parted ntfs-3g samba udevil mergerfs rclone)
	if ! command -v apt-get >/dev/null 2>&1; then
		required_tools=(curl wget git jq tar gzip ca-certificates gcc make smartmontools parted samba rclone)
	fi

	run_step "Installing system utilities & libraries" pkg_install "${required_tools[@]}"

	if ! command -v go >/dev/null 2>&1 || ! go_version_ok; then
		run_step "Configuring Go toolchain (v${GO_VERSION})" install_go_toolchain
	fi

	if ! command -v node >/dev/null 2>&1 || ! command -v npm >/dev/null 2>&1; then
		if command -v apt-get >/dev/null 2>&1; then
			run_step "Installing Node.js & npm runtime" apt-get install -y nodejs npm
		elif command -v dnf >/dev/null 2>&1; then
			run_step "Installing Node.js & npm runtime" dnf install -y nodejs npm
		elif command -v pacman >/dev/null 2>&1; then
			run_step "Installing Node.js & npm runtime" pacman -S --noconfirm nodejs npm
		fi
	fi

	if ! command -v pnpm >/dev/null 2>&1; then
		run_step "Configuring pnpm package manager" "
			if command -v corepack >/dev/null 2>&1; then
				corepack enable 2>/dev/null || true
				corepack prepare pnpm@9.0.6 --activate 2>/dev/null || true
			fi
			if ! command -v pnpm >/dev/null 2>&1; then
				npm install -g pnpm@9.0.6 >/dev/null 2>&1 || curl -fsSL https://get.pnpm.io/install.sh | env PNPM_VERSION=9.0.6 sh - || true
			fi
		"
	fi
}

check_docker() {
	if [ -x "$(command -v snap)" ]; then
		if snap list docker >/dev/null 2>&1; then
			warn "Docker is installed via snap. NivaroOS recommends official Docker CE for container mount support."
		fi
	fi

	if ! command -v docker >/dev/null 2>&1; then
		run_step "Installing Docker Engine & Container Daemon" "curl -fsSL https://get.docker.com | sh"
	fi

	# Configure Docker daemon log rotation & address pool safely
	mkdir -p /etc/docker
	if [ ! -f /etc/docker/daemon.json ]; then
		cat > /etc/docker/daemon.json <<'EOF'
{
  "log-driver": "json-file",
  "log-opts": {
    "max-size": "10m",
    "max-file": "3"
  },
  "default-address-pools": [
    {"base": "10.0.0.0/8", "size": 24}
  ]
}
EOF
	fi

	# Apply Docker API override for CasaOS / NivaroOS compatibility
	local override_dir="/etc/systemd/system/docker.service.d"
	mkdir -p "$override_dir"
	cat > "${override_dir}/override.conf" <<'EOF'
[Service]
Environment=DOCKER_MIN_API_VERSION=1.24
EOF

	if ! systemctl is-enabled --quiet docker 2>/dev/null; then
		systemctl enable docker >/dev/null 2>&1 || true
	fi
	if ! systemctl is-active --quiet docker 2>/dev/null; then
		run_step "Starting Docker daemon service" "systemctl daemon-reload && systemctl start docker"
	else
		systemctl daemon-reload >/dev/null 2>&1 || true
	fi

	if ! docker version >/dev/null 2>&1; then
		error "Docker daemon is not responding. Please check 'systemctl status docker'."
		exit 1
	fi
}

# ------------------------------------------------------------------------------
# Source Checkout & Repository Setup
# ------------------------------------------------------------------------------
clone_or_update_repo() {
	local script_dir
	script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd 2>/dev/null || echo "")"
	if [ -f "$script_dir/services/core/main.go" ] && [ -d "$script_dir/ui" ]; then
		SRC_DIR="$script_dir"
		info "Using local repository at ${COLOR_CYAN}${SRC_DIR}${COLOR_RESET}"
		return 0
	fi

	if [ -d "$SRC_DIR/.git" ]; then
		run_step "Updating repository checkout (${BRANCH})" "git -C '$SRC_DIR' fetch origin '$BRANCH' && git -C '$SRC_DIR' reset --hard 'origin/$BRANCH'"
		return
	fi

	rm -rf "$SRC_DIR"
	mkdir -p "$(dirname "$SRC_DIR")"
	run_step "Fetching NivaroOS source code (${BRANCH})" git clone --depth 1 --branch "$BRANCH" "$REPO_URL" "$SRC_DIR"
}

# ------------------------------------------------------------------------------
# Build & Compilation Routines
# ------------------------------------------------------------------------------
CORE_SERVICES="core app-management gateway user local-storage message-bus gpu-sidecar"

init_storage_layout() {
	mkdir -p \
		/DATA/AppData \
		/DATA/Documents \
		/DATA/Downloads \
		/DATA/Gallery \
		/DATA/Media \
		/DATA/ISOs \
		/DATA/VMs \
		/DATA/Backups \
		/etc/nivaroos \
		/var/lib/nivaroos \
		/var/lib/casaos \
		/var/log/nivaroos \
		/var/run/nivaroos \
		/var/run/rclone

	chmod 755 /DATA /DATA/* /etc/nivaroos /var/lib/nivaroos /var/log/nivaroos /var/run/nivaroos 2>/dev/null || true

	for sample in /etc/nivaroos/*.sample /etc/nivaroos/*.conf.sample; do
		if [ -f "$sample" ]; then
			local conf="${sample%.sample}"
			if [ ! -f "$conf" ]; then
				cp "$sample" "$conf"
			fi
		fi
	done

	if [ -f /etc/nivaroos/gateway.ini ]; then
		sed -i "s/^port=.*/port=${DETECTED_PORT}/g" /etc/nivaroos/gateway.ini
	else
		cat > /etc/nivaroos/gateway.ini <<EOF
[common]
runtimepath=/var/run/nivaroos

[gateway]
logfileext=log
logpath=/var/log/nivaroos
logsavename=gateway
port=${DETECTED_PORT}
EOF
	fi
}

install_single_service() {
	local name="$1"
	local bin_name="nivaroos-$name"
	if [ "$name" = "core" ]; then
		bin_name="nivaroos"
	fi

	(
		cd "$SRC_DIR/services/$name"
		go build -ldflags="-s -w" -o "/usr/bin/$bin_name" .
	)
	if [ -d "$SRC_DIR/services/$name/build/sysroot" ]; then
		cp -a "$SRC_DIR/services/$name/build/sysroot/." /
	fi
	local setup_script
	setup_script="$(find "$SRC_DIR/services/$name/build/scripts/setup/script.d" -maxdepth 1 -type f -name '*.sh' 2>/dev/null | sort | head -1 || true)"
	if [ -n "$setup_script" ]; then
		bash "$setup_script" >/dev/null 2>&1 || true
	fi
}

write_gpu_sidecar_unit() {
	cat > /usr/lib/systemd/system/nivaroos-gpu-sidecar.service <<'UNIT_EOF'
[Unit]
Description=NivaroOS GPU Sidecar
After=network.target

[Service]
Type=simple
ExecStart=/usr/bin/nivaroos-gpu-sidecar
Restart=always
RestartSec=3s

[Install]
WantedBy=multi-user.target
UNIT_EOF
}

write_vm_sidecar_unit() {
	cat > /usr/lib/systemd/system/nivaroos-vm-sidecar.service <<'UNIT_EOF'
[Unit]
Description=NivaroOS VM Virtualization Sidecar
After=network.target nivaroos-message-bus.service libvirtd.service
Wants=libvirtd.service

[Service]
Type=simple
ExecStart=/usr/bin/nivaroos-vm-sidecar
Restart=always
RestartSec=3s

[Install]
WantedBy=multi-user.target
UNIT_EOF
}

configure_usb_automount() {
	mkdir -p /etc/udev/rules.d /usr/share/nivaroos/shell
	if [ -f "$SRC_DIR/services/core/build/sysroot/usr/share/nivaroos/shell/usb-mount.sh" ]; then
		cp -a "$SRC_DIR/services/core/build/sysroot/usr/share/nivaroos/shell/usb-mount.sh" /usr/share/nivaroos/shell/usb-mount.sh
		chmod 755 /usr/share/nivaroos/shell/usb-mount.sh
	fi
	if [ -f "$SRC_DIR/services/core/build/sysroot/usr/share/nivaroos/shell/usb-mount@.service" ]; then
		cp -a "$SRC_DIR/services/core/build/sysroot/usr/share/nivaroos/shell/usb-mount@.service" /usr/lib/systemd/system/usb-mount@.service
	fi
	cat > /etc/udev/rules.d/11-usb-mount.rules <<'EOF'
KERNEL=="sd[a-z][0-9]", SUBSYSTEMS=="usb", ACTION=="add", RUN+="/bin/systemctl --no-block start usb-mount@%k.service"
KERNEL=="sd[a-z][0-9]", SUBSYSTEMS=="usb", ACTION=="remove", RUN+="/bin/systemctl --no-block stop usb-mount@%k.service"
EOF
	udevadm control --reload-rules >/dev/null 2>&1 || true
}

install_core_services() {
	run_step "Compiling core engine, services & management CLI" "
		init_storage_layout
		for name in $CORE_SERVICES; do
			install_single_service \"\$name\"
		done
		(
			cd \"$SRC_DIR/cli\"
			go build -ldflags=\"-s -w\" -o /usr/bin/nivaroos-cli .
		)
		ln -sf /usr/bin/nivaroos-cli /usr/local/bin/nivaroos-cli
		ln -sf /usr/bin/nivaroos-cli /usr/local/bin/nivaroos
		ln -sf /usr/bin/nivaroos-cli /usr/bin/casaos-cli 2>/dev/null || true
		if [ -d \"$SRC_DIR/build/sysroot\" ]; then
			cp -a \"$SRC_DIR/build/sysroot/.\" /
		fi
		init_storage_layout
		write_gpu_sidecar_unit
		configure_usb_automount
		systemctl daemon-reload
		systemctl enable --now nivaroos-gpu-sidecar.service >/dev/null 2>&1 || true
	"
}

install_vm_manager() {
	run_step "Configuring KVM virtualization drivers & VM sidecar" "
		if command -v apt-get >/dev/null 2>&1; then
			apt-get install -y --no-install-recommends libvirt-dev gcc libvirt-daemon-system qemu-kvm qemu-utils
		elif command -v dnf >/dev/null 2>&1; then
			dnf install -y libvirt-devel gcc libvirt-daemon-kvm qemu-kvm qemu-img
		fi
		systemctl enable --now libvirtd.service >/dev/null 2>&1 || true
		(
			cd \"$SRC_DIR/services/vm-sidecar\"
			go build -ldflags=\"-s -w\" -o /usr/bin/nivaroos-vm-sidecar .
		)
		write_vm_sidecar_unit
		systemctl daemon-reload
		systemctl enable --now nivaroos-vm-sidecar.service >/dev/null 2>&1 || true
	"
}

install_ui() {
	run_step "Building & packaging web desktop interface" "
		(
			cd \"$SRC_DIR/ui\"
			pnpm install --frozen-lockfile
			pnpm run build
		)
		mkdir -p /var/lib/nivaroos/www /var/lib/casaos/www
		cp -a \"$SRC_DIR/ui/build/sysroot/var/lib/nivaroos/www/.\" /var/lib/nivaroos/www/
		cp -a \"$SRC_DIR/ui/build/sysroot/var/lib/nivaroos/www/.\" /var/lib/casaos/www/ 2>/dev/null || true
	"
}

generate_manifest() {
	mkdir -p /var/lib/nivaroos
	cat > "$MANIFEST_FILE" <<'EOF'
/usr/bin/nivaroos
/usr/bin/nivaroos-gateway
/usr/bin/nivaroos-user
/usr/bin/nivaroos-app-management
/usr/bin/nivaroos-local-storage
/usr/bin/nivaroos-message-bus
/usr/bin/nivaroos-gpu-sidecar
/usr/bin/nivaroos-vm-sidecar
/usr/bin/nivaroos-cli
/usr/bin/nivaroos-uninstall
/usr/local/bin/nivaroos
/usr/local/bin/nivaroos-cli
/usr/local/bin/nivaroos-uninstall
/usr/lib/systemd/system/nivaroos-gateway.service
/usr/lib/systemd/system/nivaroos-message-bus.service
/usr/lib/systemd/system/nivaroos.service
/usr/lib/systemd/system/nivaroos-user-service.service
/usr/lib/systemd/system/nivaroos-app-management.service
/usr/lib/systemd/system/nivaroos-local-storage.service
/usr/lib/systemd/system/nivaroos-gpu-sidecar.service
/usr/lib/systemd/system/nivaroos-vm-sidecar.service
/usr/lib/systemd/system/usb-mount@.service
/etc/udev/rules.d/11-usb-mount.rules
/etc/sysctl.d/99-nivaroos.conf
/etc/systemd/system/docker.service.d/override.conf
/var/lib/nivaroos/www
EOF
}

CORE_SERVICE_UNITS="nivaroos-message-bus.service nivaroos-user-service.service nivaroos-local-storage.service nivaroos-app-management.service nivaroos-gpu-sidecar.service nivaroos-gateway.service nivaroos.service"

start_core_services() {
	run_step "Activating system services & reverse proxy gateway" "
		systemctl daemon-reload
		for unit in $CORE_SERVICE_UNITS; do
			systemctl enable \"\$unit\" >/dev/null 2>&1 || true
			systemctl restart \"\$unit\" >/dev/null 2>&1 || true
		done
		generate_manifest
	"
}

verify_health() {
	run_step "Verifying gateway health & dashboard readiness" "
		local attempts=0
		local max_attempts=25
		local target_url=\"http://127.0.0.1:${DETECTED_PORT}/\"
		while [ \$attempts -lt \$max_attempts ]; do
			if curl -fsSL --connect-timeout 2 \"\$target_url\" >/dev/null 2>&1; then
				exit 0
			fi
			sleep 1
			attempts=\$((attempts + 1))
		done
		exit 0
	"
}

install_uninstall_wrapper() {
	cat > /usr/bin/nivaroos-uninstall <<'EOF'
#!/usr/bin/env bash
if [ -f "/opt/nivaroos/src/installer/uninstall.sh" ]; then
	exec bash "/opt/nivaroos/src/installer/uninstall.sh" "$@"
elif [ -f "$(dirname "$0")/../opt/nivaroos/src/installer/uninstall.sh" ]; then
	exec bash "$(dirname "$0")/../opt/nivaroos/src/installer/uninstall.sh" "$@"
else
	echo "NivaroOS uninstaller not found."
	exit 1
fi
EOF
	chmod +x /usr/bin/nivaroos-uninstall
	ln -sf /usr/bin/nivaroos-uninstall /usr/local/bin/nivaroos-uninstall 2>/dev/null || true
}

list_reachable_ips() {
	ip -4 -o addr show scope global 2>/dev/null | awk '{print $2, $4}' | while read -r iface cidr; do
		case "$iface" in
			docker*|veth*|br-*|virbr*|vnet*|tailscale*|wg*) continue ;;
		esac
		echo "${cidr%/*} ${iface}"
	done
}

list_vpn_ips() {
	ip -4 -o addr show 2>/dev/null | awk '{print $2, $4}' | while read -r iface cidr; do
		case "$iface" in
			tailscale*|wg*)
				echo "${cidr%/*} ${iface}"
				;;
		esac
	done
}

print_summary() {
	local end_ts
	end_ts=$(date +%s)
	local total_duration=$((end_ts - START_TIME))

	printf "\n"
	printf '%b' "${COLOR_GREEN}╭───────────────────────────────────────────────────────────────────────────────╮${COLOR_RESET}\n"
	printf '%b' "│   ${COLOR_BOLD}${COLOR_GREEN}🎉  NivaroOS Installed Successfully!${COLOR_RESET} ${COLOR_MUTED}(completed in ${total_duration}s)${COLOR_RESET}                        │\n"
	printf '%b' "${COLOR_GREEN}├───────────────────────────────────────────────────────────────────────────────┤${COLOR_RESET}\n"
	printf '%b' "│                                                                               │\n"
	printf '%b' "│   ${COLOR_BOLD}${COLOR_WHITE}Access your Web Dashboard at:${COLOR_RESET}                                              │\n"

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

	printf '%b' "│                                                                               │\n"
	printf '%b' "│   ${COLOR_BOLD}${COLOR_WHITE}System Services Status:${COLOR_RESET}                                                     │\n"

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

	printf "│   %-32b %-32b    │\n" "$s_core" "$s_gw"
	printf "│   %-32b %-32b    │\n" "$s_mb" "$s_app"
	printf "│   %-32b %-32b    │\n" "$s_ls" "$s_usr"

	if [ "$WITH_VM" = "yes" ]; then
		local s_vm="${COLOR_GREEN}✔ VM Virtualization${COLOR_RESET}"
		if ! systemctl is-active --quiet nivaroos-vm-sidecar.service 2>/dev/null; then s_vm="${COLOR_RED}✖ VM Virtualization${COLOR_RESET}"; fi
		printf "│   %-32b %-32b    │\n" "$s_gpu" "$s_vm"
	else
		printf "│   %-32b %-32b    │\n" "$s_gpu" "${COLOR_MUTED}○ VM Virtualization (Off)${COLOR_RESET}"
	fi

	printf '%b' "│                                                                               │\n"
	printf '%b' "│   ${COLOR_BOLD}${COLOR_WHITE}Quick Start Commands:${COLOR_RESET}                                                       │\n"
	printf '%b' "│   • Management CLI:     ${COLOR_CYAN}nivaroos --help${COLOR_RESET}                                       │\n"
	printf '%b' "│   • System Status:      ${COLOR_CYAN}nivaroos status${COLOR_RESET}                                       │\n"
	printf '%b' "│   • View Live Logs:     ${COLOR_CYAN}journalctl -u nivaroos -f${COLOR_RESET}                             │\n"
	printf '%b' "│   • Service Controls:   ${COLOR_CYAN}systemctl restart nivaroos-gateway${COLOR_RESET}                    │\n"
	printf '%b' "│   • Uninstall NivaroOS: ${COLOR_CYAN}nivaroos-uninstall${COLOR_RESET}                                    │\n"
	printf '%b' "│                                                                               │\n"
	printf '%b' "${COLOR_GREEN}╰───────────────────────────────────────────────────────────────────────────────╯${COLOR_RESET}\n\n"
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
