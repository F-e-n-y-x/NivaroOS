#!/usr/bin/env bash
# ==============================================================================
#  NivaroOS Installer Script
#  Modern Self-Hosted Personal Cloud & Container Platform
# ==============================================================================
# This script uses bash-only syntax (arrays, etc.) and will fail with a
# confusing syntax error if run under a POSIX `sh` (e.g. dash on Debian/Ubuntu)
# instead of bash. Detect that and transparently re-exec under bash.
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
MIN_REQUIRED_MEMORY_MB="256"
MIN_RECOMMENDED_DISK_GB="5"
MIN_REQUIRED_DISK_GB="1"

CUSTOM_PORT=""
WITH_VM=""
YES=""
DEBUG=""
STEP_NUM=0
TOTAL_STEPS=8
START_TIME=0

export PATH="/usr/local/go/bin:/usr/local/bin:$PATH"
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

info()  { printf '%b\n' "${COLOR_CYAN}ℹ${COLOR_RESET}  ${COLOR_WHITE}$1${COLOR_RESET}"; }
success() { printf '%b\n' "${COLOR_GREEN}✔${COLOR_RESET}  ${COLOR_GREEN}$1${COLOR_RESET}"; }
warn()  { printf '%b\n' "${COLOR_YELLOW}⚠${COLOR_RESET}  ${COLOR_YELLOW}$1${COLOR_RESET}" >&2; }
error() { printf '%b\n' "${COLOR_RED}✖${COLOR_RESET}  ${COLOR_RED}$1${COLOR_RESET}" >&2; }

cleanup_on_exit() {
	if [ "$IS_TTY" = "true" ]; then
		printf "\033[?25h" # Restore cursor visibility
	fi
}
trap cleanup_on_exit EXIT INT TERM

# ------------------------------------------------------------------------------
# Banner & Diagnostics Display
# ------------------------------------------------------------------------------
print_banner() {
	clear 2>/dev/null || true
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

	local mem_mb mem_gb_str
	mem_mb="$(LC_ALL=C free -m 2>/dev/null | awk '/^Mem:/ { print $2 }' || echo "0")"
	if [ -n "$mem_mb" ] && [ "$mem_mb" -gt 0 ]; then
		mem_gb_str="$(awk "BEGIN {printf \"%.1f GB\", $mem_mb/1024}")"
	else
		mem_gb_str="Unknown"
	fi

	local disk_gb_str="Unknown"
	local disk_mb
	disk_mb="$(LC_ALL=C df -m / 2>/dev/null | tail -n 1 | awk '{print $4}' || echo "0")"
	if [ -n "$disk_mb" ] && [ "$disk_mb" -gt 0 ]; then
		disk_gb_str="$(awk "BEGIN {printf \"%.1f GB\", $disk_mb/1024}")"
	fi

	local kvm_status="Supported (KVM Acceleration Available)"
	if [ ! -e /dev/kvm ]; then
		kvm_status="Not Detected (Emulation Only)"
	fi

	local docker_status="Not Installed (Will be auto-installed)"
	if command -v docker >/dev/null 2>&1; then
		local d_ver
		d_ver="$(docker version --format '{{.Server.Version}}' 2>/dev/null || docker -v 2>/dev/null | awk '{print $3}' | tr -d ',' || echo 'installed')"
		docker_status="Installed (v${d_ver})"
	fi

	printf '%b' "${COLOR_MUTED}╭── ${COLOR_BOLD}${COLOR_WHITE}System Diagnostics${COLOR_RESET}${COLOR_MUTED} ──────────────────────────────────────────────────────────╮${COLOR_RESET}\n"
	printf '%b' "│  ${COLOR_CYAN}•${COLOR_RESET} ${COLOR_MUTED}Operating System:${COLOR_RESET}   ${COLOR_WHITE}${os_name} (${arch})${COLOR_RESET}\n"
	printf '%b' "│  ${COLOR_CYAN}•${COLOR_RESET} ${COLOR_MUTED}Linux Kernel:${COLOR_RESET}       ${COLOR_WHITE}${kernel}${COLOR_RESET}\n"
	printf '%b' "│  ${COLOR_CYAN}•${COLOR_RESET} ${COLOR_MUTED}System Memory:${COLOR_RESET}      ${COLOR_WHITE}${mem_gb_str}${COLOR_RESET}\n"
	printf '%b' "│  ${COLOR_CYAN}•${COLOR_RESET} ${COLOR_MUTED}Free Disk on /:${COLOR_RESET}     ${COLOR_WHITE}${disk_gb_str}${COLOR_RESET}\n"
	printf '%b' "│  ${COLOR_CYAN}•${COLOR_RESET} ${COLOR_MUTED}Virtualization:${COLOR_RESET}     ${COLOR_WHITE}${kvm_status}${COLOR_RESET}\n"
	printf '%b' "│  ${COLOR_CYAN}•${COLOR_RESET} ${COLOR_MUTED}Docker Engine:${COLOR_RESET}      ${COLOR_WHITE}${docker_status}${COLOR_RESET}\n"
	printf '%b' "${COLOR_MUTED}╰───────────────────────────────────────────────────────────────────────────────╯${COLOR_RESET}\n\n"
}

# ------------------------------------------------------------------------------
# Pre-flight Checks
# ------------------------------------------------------------------------------
check_root() {
	if [ "$(id -u)" -ne 0 ]; then
		error "NivaroOS installer must be run as root."
		printf '%b\n' "   ${COLOR_MUTED}Please rerun with:${COLOR_RESET} ${COLOR_CYAN}sudo bash $0${COLOR_RESET}"
		exit 1
	fi
}

check_distro() {
	if [ ! -f "$OS_RELEASE_FILE" ]; then
		error "$OS_RELEASE_FILE not found, cannot determine Linux distribution."
		exit 1
	fi
	# shellcheck disable=SC1090
	. "$OS_RELEASE_FILE"
	local family="${ID:-} ${ID_LIKE:-}"
	case " $family " in
		*" debian "*|*" ubuntu "*) ;;
		*)
			error "Unsupported distribution '${ID:-unknown}'."
			printf '%b\n' "   ${COLOR_MUTED}NivaroOS supports Debian, Ubuntu, Linux Mint, Pop!_OS, Raspberry Pi OS, and their derivatives.${COLOR_RESET}"
			exit 1
			;;
	esac
}

check_resources() {
	local mem_mb disk_gb
	mem_mb="$(LC_ALL=C free -m 2>/dev/null | awk '/^Mem:/ { print $2 }' || echo "0")"
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
			--yes|-y) YES=yes ;;
			--debug) DEBUG=yes ;;
			--help|-h)
				print_banner
				printf '%b\n' "${COLOR_BOLD}Usage:${COLOR_RESET} install.sh [options]\n"
				printf '%b\n' "${COLOR_BOLD}Options:${COLOR_RESET}"
				printf '%b\n' "  ${COLOR_CYAN}-y, --yes${COLOR_RESET}             Automatic non-interactive installation (accept all defaults)"
				printf '%b\n' "  ${COLOR_CYAN}--with-vm${COLOR_RESET}             Install VM Manager with QEMU/KVM, libvirt & web console"
				printf '%b\n' "  ${COLOR_CYAN}--without-vm${COLOR_RESET}          Skip VM Manager installation (can be enabled later via CLI)"
				printf '%b\n' "  ${COLOR_CYAN}--port <port>${COLOR_RESET}         Custom HTTP dashboard port (default: 80)"
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
			TOTAL_STEPS=9
		fi
		return
	fi

	# Default to auto-detecting KVM in non-interactive mode
	if [ -n "$YES" ] || [ ! -t 0 ]; then
		if [ -e /dev/kvm ]; then
			WITH_VM=yes
			TOTAL_STEPS=9
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
		TOTAL_STEPS=9
		printf '%b\n\n' "  ${COLOR_GREEN}✔${COLOR_RESET} VM Manager selected."
	else
		printf '%b\n\n' "  ${COLOR_MUTED}○${COLOR_RESET} VM Manager skipped (can be enabled anytime with 'nivaroos-cli vm enable')."
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
	printf '%b' "│  • Verify internet connectivity and package repository mirrors.               │\n"
	printf '%b' "│  • Ensure system meets minimum RAM (1GB+) and free disk space (5GB+).         │\n"
	printf '%b' "│  • Full log saved to: ${COLOR_CYAN}${log_file}${COLOR_RESET}\n"
	printf '%b' "│  • Report issues at:  ${COLOR_CYAN}https://github.com/F-e-n-y-x/NivaroOS/issues${COLOR_RESET}           │\n"
	printf '%b' "${COLOR_RED}╰───────────────────────────────────────────────────────────────────────────────╯${COLOR_RESET}\n\n"
}

on_fatal_error() {
	local exit_code=$?
	local line_no="$1"
	if [ "$exit_code" -ne 0 ]; then
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

	local log_file
	log_file=$(mktemp /tmp/nivaroos-install-step-XXXXXX.log)

	if [ "$DEBUG" = "yes" ]; then
		printf '%b\n' "  ${COLOR_CYAN}➜${COLOR_RESET} ${COLOR_BOLD}${COLOR_BLUE}${step_tag}${COLOR_RESET} ${COLOR_WHITE}${title}${COLOR_RESET} (running verbose)..."
		if ! bash -c "export PATH=\"/usr/local/go/bin:/usr/local/bin:\$PATH\"; export DEBIAN_FRONTEND=noninteractive; export NEEDRESTART_MODE=a; $*"; then
			local exit_code=$?
			error "Step ${STEP_NUM} failed: ${title}"
			exit "$exit_code"
		fi
		local end_ts
		end_ts=$(date +%s)
		local elapsed=$((end_ts - start_ts))
		printf '%b\n' "  ${COLOR_GREEN}✔${COLOR_RESET} ${COLOR_BOLD}${COLOR_BLUE}${step_tag}${COLOR_RESET} ${COLOR_WHITE}${title}${COLOR_RESET} ${COLOR_MUTED}[${elapsed}s]${COLOR_RESET}"
		rm -f "$log_file"
		return 0
	fi

	if [ "$IS_TTY" = "true" ]; then
		(
			export PATH="/usr/local/go/bin:/usr/local/bin:$PATH"
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
			
			printf "\r\033[2K  %b %b %s%b %b(%ds)%b" \
				"${COLOR_CYAN}${frame}${COLOR_RESET}" \
				"${COLOR_BOLD}${COLOR_BLUE}${step_tag}${COLOR_RESET}" \
				"${COLOR_WHITE}${title}${COLOR_RESET}" \
				"${COLOR_RESET}" \
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

		if [ "$exit_code" -eq 0 ]; then
			printf "\r\033[2K  %b %b %s%b %b[%ds]%b\n" \
				"${COLOR_GREEN}✔${COLOR_RESET}" \
				"${COLOR_BOLD}${COLOR_BLUE}${step_tag}${COLOR_RESET}" \
				"${COLOR_WHITE}${title}${COLOR_RESET}" \
				"${COLOR_RESET}" \
				"${COLOR_MUTED}" "${total_elapsed}" "${COLOR_RESET}"
			rm -f "$log_file"
		else
			printf "\r\033[2K  %b %b %s%b %b[%ds - FAILED]%b\n" \
				"${COLOR_RED}✖${COLOR_RESET}" \
				"${COLOR_BOLD}${COLOR_RED}${step_tag}${COLOR_RESET}" \
				"${COLOR_WHITE}${title}${COLOR_RESET}" \
				"${COLOR_RESET}" \
				"${COLOR_RED}" "${total_elapsed}" "${COLOR_RESET}"
			print_error_card "$title" "$exit_code" "$log_file"
			exit "$exit_code"
		fi
	else
		printf "  ➜ %s %s...\n" "$step_tag" "$title"
		if (
			export PATH="/usr/local/go/bin:/usr/local/bin:$PATH"
			export DEBIAN_FRONTEND=noninteractive
			export NEEDRESTART_MODE=a
			eval "$*"
		) > "$log_file" 2>&1 </dev/null; then
			local end_ts
			end_ts=$(date +%s)
			local total_elapsed=$((end_ts - start_ts))
			printf "  ✔ %s %s [%ds]\n" "$step_tag" "$title" "$total_elapsed"
			rm -f "$log_file"
		else
			local exit_code=$?
			printf "  ✖ %s %s [FAILED with exit code %d]\n" "$step_tag" "$title" "$exit_code"
			print_error_card "$title" "$exit_code" "$log_file"
			exit "$exit_code"
		fi
	fi
}

# ------------------------------------------------------------------------------
# Installation Routines
# ------------------------------------------------------------------------------
verify_sha256() {
	local file="$1" expected="$2" actual
	actual="$(sha256sum "$file" 2>/dev/null | awk '{print $1}')"
	if [ -z "$expected" ] || [ "$actual" != "$expected" ]; then
		rm -f "$file"
		return 1
	fi
}

go_version_ok() {
	local ver
	ver="$(go version 2>/dev/null | awk '{print $3}' | sed 's/^go//' || echo "0")"
	printf '%s\n%s\n' "$GO_VERSION" "$ver" | sort -V -C
}

install_go_toolchain() {
	local arch go_arch
	arch="$(dpkg --print-architecture 2>/dev/null || uname -m)"
	case "$arch" in
		amd64|x86_64) go_arch=amd64 ;;
		arm64|aarch64) go_arch=arm64 ;;
		*)
			error "Unsupported architecture '$arch' for Go toolchain installation."
			exit 1
			;;
	esac

	local tarball="go${GO_VERSION}.linux-${go_arch}.tar.gz"
	local expected_sha
	expected_sha="$(curl -fsSL 'https://go.dev/dl/?mode=json&include=all' 2>/dev/null | tr -d ' \n' | grep -o "\"filename\":\"${tarball}\"[^}]*\"sha256\":\"[a-f0-9]*\"" | grep -o '"sha256":"[a-f0-9]*"' | head -1 | grep -o '[a-f0-9]\{64\}' || true)"

	curl -fsSL "https://go.dev/dl/${tarball}" -o "/tmp/${tarball}"
	if [ -n "$expected_sha" ]; then
		if ! verify_sha256 "/tmp/${tarball}" "$expected_sha"; then
			error "Go toolchain download failed checksum verification (expected ${expected_sha})."
			exit 1
		fi
	fi

	rm -rf /usr/local/go
	tar -C /usr/local -xzf "/tmp/${tarball}"
	rm -f "/tmp/${tarball}"
	export PATH="/usr/local/go/bin:$PATH"
	echo 'export PATH="/usr/local/go/bin:$PATH"' > /etc/profile.d/nivaroos-go.sh
	ln -sf /usr/local/go/bin/go /usr/bin/go
	ln -sf /usr/local/go/bin/gofmt /usr/bin/gofmt
}

install_build_deps() {
	run_step "Updating package lists & repositories" apt-get update
	run_step "Installing core build & system tools" apt-get install -y \
		git nodejs npm curl ca-certificates build-essential \
		smartmontools hdparm parted ntfs-3g samba

	if ! command -v go >/dev/null 2>&1 || ! go_version_ok; then
		run_step "Configuring Go toolchain (v${GO_VERSION})" install_go_toolchain
	fi

	if ! command -v pnpm >/dev/null 2>&1; then
		run_step "Installing pnpm package manager" "
			if command -v corepack >/dev/null 2>&1; then
				corepack enable || true
				corepack prepare pnpm@9.0.6 --activate || true
			fi
			if ! command -v pnpm >/dev/null 2>&1; then
				npm install -g pnpm@9.0.6 >/dev/null 2>&1 || curl -fsSL https://get.pnpm.io/install.sh | env PNPM_VERSION=9.0.6 sh - || true
			fi
		"
	fi
}

check_docker() {
	if ! command -v docker >/dev/null 2>&1; then
		run_step "Installing Docker Engine & Container Daemon" "curl -fsSL https://get.docker.com | sh"
	fi
	if ! systemctl is-enabled --quiet docker 2>/dev/null; then
		systemctl enable docker >/dev/null 2>&1 || true
	fi
	if ! systemctl is-active --quiet docker 2>/dev/null; then
		run_step "Starting Docker daemon service" systemctl start docker
	fi
	if ! docker version >/dev/null 2>&1; then
		error "Docker was installed but is not responding. Check 'systemctl status docker'."
		exit 1
	fi
}

clone_repo() {
	if [ -d "$SRC_DIR/.git" ]; then
		run_step "Updating repository checkout (${BRANCH})" "git -C '$SRC_DIR' fetch origin '$BRANCH' && git -C '$SRC_DIR' reset --hard 'origin/$BRANCH'"
		return
	fi
	rm -rf "$SRC_DIR"
	mkdir -p "$(dirname "$SRC_DIR")"
	run_step "Fetching NivaroOS source code (${BRANCH})" git clone --branch "$BRANCH" "$REPO_URL" "$SRC_DIR"
}

CORE_SERVICES="core app-management gateway user local-storage message-bus gpu-sidecar"

install_service() {
	local name="$1"
	local bin_name="nivaroos-$name"
	if [ "$name" = "core" ]; then
		bin_name="nivaroos"
	fi
	(
		cd "$SRC_DIR/services/$name"
		go build -o "/usr/bin/$bin_name" .
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

GPU_SIDECAR_UNIT=/usr/lib/systemd/system/nivaroos-gpu-sidecar.service

write_gpu_sidecar_unit() {
	cat > "$GPU_SIDECAR_UNIT" <<'UNIT_EOF'
[Unit]
After=network.target
Description=NivaroOS GPU Sidecar

[Service]
ExecStart=/usr/bin/nivaroos-gpu-sidecar
Restart=always

[Install]
WantedBy=multi-user.target
UNIT_EOF
}

init_directories_and_configs() {
	mkdir -p /etc/nivaroos /var/lib/nivaroos /var/lib/casaos /var/log/nivaroos /var/run/nivaroos /DATA
	for sample in /etc/nivaroos/*.sample /etc/nivaroos/*.conf.sample; do
		if [ -f "$sample" ]; then
			local conf="${sample%.sample}"
			if [ ! -f "$conf" ]; then
				cp "$sample" "$conf"
			fi
		fi
	done

	if [ -n "$CUSTOM_PORT" ] && [ -f /etc/nivaroos/gateway.ini ]; then
		sed -i "s/^port=.*/port=${CUSTOM_PORT}/g" /etc/nivaroos/gateway.ini
	fi
}

install_core_services() {
	run_step "Compiling core engine, services & management CLI" "
		init_directories_and_configs
		for name in $CORE_SERVICES; do
			install_service \"\$name\"
		done
		(
			cd \"$SRC_DIR/cli\"
			go build -o /usr/bin/nivaroos-cli .
		)
		if [ -d \"$SRC_DIR/build/sysroot\" ]; then
			cp -a \"$SRC_DIR/build/sysroot/.\" /
		fi
		init_directories_and_configs
		write_gpu_sidecar_unit
		systemctl daemon-reload
		systemctl enable --now nivaroos-gpu-sidecar.service >/dev/null 2>&1 || true
	"
}

VM_SIDECAR_UNIT=/usr/lib/systemd/system/nivaroos-vm-sidecar.service

write_vm_sidecar_unit() {
	cat > "$VM_SIDECAR_UNIT" <<'UNIT_EOF'
[Unit]
After=network.target nivaroos-message-bus.service libvirtd.service
Description=NivaroOS VM Sidecar

[Service]
ExecStart=/usr/bin/nivaroos-vm-sidecar
Restart=always

[Install]
WantedBy=multi-user.target
UNIT_EOF
}

install_vm_manager() {
	run_step "Configuring virtualization drivers & VM sidecar" "
		apt-get install -y libvirt-dev gcc libvirt-daemon-system qemu-kvm qemu-utils
		systemctl enable --now libvirtd.service >/dev/null 2>&1 || true
		(
			cd \"$SRC_DIR/services/vm-sidecar\"
			go build -o /usr/bin/nivaroos-vm-sidecar .
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

CORE_SERVICE_UNITS="nivaroos-gateway.service nivaroos-message-bus.service nivaroos.service nivaroos-user-service.service nivaroos-app-management.service nivaroos-local-storage.service"

start_core_services() {
	run_step "Starting system services & activating gateway" "
		systemctl daemon-reload
		for unit in $CORE_SERVICE_UNITS nivaroos-gpu-sidecar.service; do
			systemctl enable --now \"\$unit\" >/dev/null 2>&1 || true
			systemctl restart \"\$unit\" >/dev/null 2>&1 || true
		done
	"
}

install_uninstall_wrapper() {
	cat > /usr/bin/nivaroos-uninstall <<EOF
#!/usr/bin/env bash
exec bash "$SRC_DIR/installer/uninstall.sh" "\$@"
EOF
	chmod +x /usr/bin/nivaroos-uninstall
}

list_reachable_ips() {
	ip -4 -o addr show scope global 2>/dev/null | awk '{print $2, $4}' | while read -r iface cidr; do
		case "$iface" in
			docker*|veth*|br-*|virbr*) continue ;;
		esac
		echo "${cidr%/*} ${iface}"
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

	local port
	port="$(grep -m1 '^port=' /etc/nivaroos/gateway.ini 2>/dev/null | cut -d= -f2 || echo "")"
	if [ -z "$port" ] || [ "$port" = "80" ]; then
		port=""
	else
		port=":${port}"
	fi

	local ips
	ips="$(list_reachable_ips)"
	if [ -n "$ips" ]; then
		while read -r ip iface; do
			[ -z "$ip" ] && continue
			local url="http://${ip}${port}"
			printf "│   ${COLOR_CYAN}➜${COLOR_RESET}  ${COLOR_BOLD}%-30s${COLOR_RESET} ${COLOR_MUTED}(%s)${COLOR_RESET}\n" "$url" "$iface"
		done <<< "$ips"
	fi
	printf "│   ${COLOR_CYAN}➜${COLOR_RESET}  ${COLOR_BOLD}%-30s${COLOR_RESET} ${COLOR_MUTED}(local)${COLOR_RESET}\n" "http://localhost${port}"

	printf '%b' "│                                                                               │\n"
	printf '%b' "│   ${COLOR_BOLD}${COLOR_WHITE}System Services Status:${COLOR_RESET}                                                     │\n"

	local s_core="${COLOR_GREEN}✔ Core Engine${COLOR_RESET}"
	local s_gw="${COLOR_GREEN}✔ Gateway${COLOR_RESET}"
	local s_mb="${COLOR_GREEN}✔ Message Bus${COLOR_RESET}"
	local s_app="${COLOR_GREEN}✔ App Management${COLOR_RESET}"
	local s_ls="${COLOR_GREEN}✔ Local Storage${COLOR_RESET}"
	local s_usr="${COLOR_GREEN}✔ User Service${COLOR_RESET}"
	local s_gpu="${COLOR_GREEN}✔ GPU Sidecar${COLOR_RESET}"

	if ! systemctl is-active --quiet nivaroos.service; then s_core="${COLOR_RED}✖ Core Engine${COLOR_RESET}"; fi
	if ! systemctl is-active --quiet nivaroos-gateway.service; then s_gw="${COLOR_RED}✖ Gateway${COLOR_RESET}"; fi
	if ! systemctl is-active --quiet nivaroos-message-bus.service; then s_mb="${COLOR_RED}✖ Message Bus${COLOR_RESET}"; fi
	if ! systemctl is-active --quiet nivaroos-app-management.service; then s_app="${COLOR_RED}✖ App Management${COLOR_RESET}"; fi
	if ! systemctl is-active --quiet nivaroos-local-storage.service; then s_ls="${COLOR_RED}✖ Local Storage${COLOR_RESET}"; fi
	if ! systemctl is-active --quiet nivaroos-user-service.service; then s_usr="${COLOR_RED}✖ User Service${COLOR_RESET}"; fi
	if ! systemctl is-active --quiet nivaroos-gpu-sidecar.service; then s_gpu="${COLOR_RED}✖ GPU Sidecar${COLOR_RESET}"; fi

	printf "│   %-32b %-32b    │\n" "$s_core" "$s_gw"
	printf "│   %-32b %-32b    │\n" "$s_mb" "$s_app"
	printf "│   %-32b %-32b    │\n" "$s_ls" "$s_usr"

	if [ "$WITH_VM" = "yes" ]; then
		local s_vm="${COLOR_GREEN}✔ VM Manager (Sidecar)${COLOR_RESET}"
		if ! systemctl is-active --quiet nivaroos-vm-sidecar.service; then s_vm="${COLOR_RED}✖ VM Manager (Sidecar)${COLOR_RESET}"; fi
		printf "│   %-32b %-32b    │\n" "$s_gpu" "$s_vm"
	else
		printf "│   %-32b %-32b    │\n" "$s_gpu" "${COLOR_MUTED}○ VM Manager (Disabled)${COLOR_RESET}"
	fi

	printf '%b' "│                                                                               │\n"
	printf '%b' "│   ${COLOR_BOLD}${COLOR_WHITE}Quick Start Commands:${COLOR_RESET}                                                       │\n"
	printf '%b' "│   • Management CLI:     ${COLOR_CYAN}nivaroos-cli --help${COLOR_RESET}                                   │\n"
	printf '%b' "│   • View Live Logs:     ${COLOR_CYAN}journalctl -u nivaroos -f${COLOR_RESET}                             │\n"
	printf '%b' "│   • Service Controls:   ${COLOR_CYAN}systemctl {status|restart} nivaroos-gateway${COLOR_RESET}           │\n"
	printf '%b' "│   • Uninstall NivaroOS: ${COLOR_CYAN}nivaroos-uninstall${COLOR_RESET}                                    │\n"
	printf '%b' "│                                                                               │\n"
	printf '%b' "${COLOR_GREEN}╰───────────────────────────────────────────────────────────────────────────────╯${COLOR_RESET}\n\n"
}

# ------------------------------------------------------------------------------
# Main Entry Point
# ------------------------------------------------------------------------------
main() {
	START_TIME=$(date +%s)
	parse_args "$@"
	check_root
	check_distro
	check_resources
	print_banner
	print_diagnostics_card
	select_addons

	info "Starting installation pipeline..."
	printf "\n"

	install_build_deps
	check_docker
	clone_repo
	install_core_services

	if [ "$WITH_VM" = "yes" ]; then
		install_vm_manager
	fi

	install_ui
	start_core_services
	install_uninstall_wrapper

	print_summary
}

main "$@"
