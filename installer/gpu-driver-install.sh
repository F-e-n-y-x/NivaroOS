#!/usr/bin/env bash
# ==============================================================================
#  NivaroOS GPU Driver Installer
#
#  Detects which GPU vendor(s) are present via lspci and installs the
#  matching driver/userspace stack for the running distro. Standalone (does
#  not source or depend on installer/install.sh) so it can be run directly
#  by an admin, or triggered from the dashboard's GPU widget when it detects
#  a GPU with no working driver.
#
#  Usage:
#    sudo bash gpu-driver-install.sh            # detect and install for all found GPUs
#    sudo bash gpu-driver-install.sh --status   # only report detection/driver status as JSON, install nothing
#    sudo bash gpu-driver-install.sh --vendor=nvidia   # force a specific vendor instead of auto-detecting
# ==============================================================================

if [ -z "${BASH_VERSION:-}" ]; then
	exec bash "$0" "$@"
fi

set -Eeuo pipefail

IS_TTY="false"
if [ -t 1 ] && [ "${TERM:-}" != "dumb" ] && [ -z "${NO_COLOR:-}" ]; then
	IS_TTY="true"
fi
if [ "$IS_TTY" = "true" ]; then
	C_RESET='\033[0m'; C_CYAN='\033[38;5;51m'
	C_GREEN='\033[38;5;48m'; C_YELLOW='\033[38;5;220m'; C_RED='\033[38;5;196m'
else
	C_RESET=''; C_CYAN=''; C_GREEN=''; C_YELLOW=''; C_RED=''
fi
info()    { printf '%b\n' "${C_CYAN}i${C_RESET}  $1"; }
success() { printf '%b\n' "${C_GREEN}OK${C_RESET}  $1"; }
warn()    { printf '%b\n' "${C_YELLOW}!${C_RESET}  $1" >&2; }
error()   { printf '%b\n' "${C_RED}x${C_RESET}  $1" >&2; }

STATUS_ONLY="false"
FORCE_VENDOR=""
for arg in "$@"; do
	case "$arg" in
		--status) STATUS_ONLY="true" ;;
		--vendor=*) FORCE_VENDOR="${arg#*=}" ;;
		--help|-h)
			printf '%s\n' "Usage: gpu-driver-install.sh [--status] [--vendor=nvidia|amd|intel]"
			exit 0
			;;
	esac
done

if [ "$STATUS_ONLY" != "true" ] && [ "$(id -u)" -ne 0 ]; then
	if command -v sudo >/dev/null 2>&1; then
		exec sudo -E bash "$0" "$@"
	else
		error "This needs root to install packages - re-run as root or with sudo."
		exit 1
	fi
fi

# ------------------------------------------------------------------------------
# Package Manager Helpers (same shape as installer/install.sh's)
# ------------------------------------------------------------------------------
pkg_update() {
	if command -v apt-get >/dev/null 2>&1; then apt-get update -qq
	elif command -v dnf >/dev/null 2>&1; then dnf check-update || true
	elif command -v yum >/dev/null 2>&1; then yum check-update || true
	elif command -v pacman >/dev/null 2>&1; then pacman -Sy --noconfirm
	elif command -v zypper >/dev/null 2>&1; then zypper refresh
	fi
}
pkg_install() {
	local pkgs=("$@")
	if command -v apt-get >/dev/null 2>&1; then
		DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "${pkgs[@]}"
	elif command -v dnf >/dev/null 2>&1; then dnf install -y "${pkgs[@]}"
	elif command -v yum >/dev/null 2>&1; then yum install -y "${pkgs[@]}"
	elif command -v pacman >/dev/null 2>&1; then pacman -S --noconfirm --needed "${pkgs[@]}"
	elif command -v zypper >/dev/null 2>&1; then zypper install -y --no-recommends "${pkgs[@]}"
	else
		error "No known package manager found (apt/dnf/yum/pacman/zypper)."
		return 1
	fi
}

# ------------------------------------------------------------------------------
# GPU Detection
# ------------------------------------------------------------------------------
# lspci's own [vendor-id] tag is more reliable than matching vendor name
# strings (which vary in wording/capitalization across chipsets) - 10de is
# always NVIDIA, 1002 always AMD/ATI, 8086 always Intel, regardless of the
# specific card.
detect_gpu_vendors() {
	if ! command -v lspci >/dev/null 2>&1; then
		error "lspci not found - install pciutils first (this ships with NivaroOS's own installer)."
		return 1
	fi

	local pci_output
	pci_output="$(lspci -nn 2>/dev/null | grep -Ei 'VGA compatible controller|3D controller|Display controller' || true)"

	DETECTED_VENDORS=()
	if echo "$pci_output" | grep -qi '\[10de:'; then DETECTED_VENDORS+=("nvidia"); fi
	if echo "$pci_output" | grep -qi '\[1002:'; then DETECTED_VENDORS+=("amd"); fi
	if echo "$pci_output" | grep -qi '\[8086:'; then DETECTED_VENDORS+=("intel"); fi

	DETECTED_GPU_NAMES="$(echo "$pci_output" | sed -E 's/^[0-9a-f:.]+ [^:]+: //' || true)"
}

driver_working_for() {
	local vendor="$1"
	case "$vendor" in
		nvidia) command -v nvidia-smi >/dev/null 2>&1 && nvidia-smi >/dev/null 2>&1 ;;
		amd)
			# The open-source amdgpu driver is in-kernel on any distro new
			# enough to ship a supported card - "working" here means the
			# kernel module is actually bound to the card, not just present
			# on disk (modinfo would succeed either way).
			lsmod 2>/dev/null | grep -q '^amdgpu' ;;
		intel)
			# i915 likewise ships in-kernel - same reasoning as amdgpu above.
			lsmod 2>/dev/null | grep -q '^i915' ;;
		*) return 1 ;;
	esac
}

print_status_json() {
	detect_gpu_vendors
	local first=true
	printf '{"gpus":['
	local v
	for v in "${DETECTED_VENDORS[@]:-}"; do
		[ -z "$v" ] && continue
		if [ "$first" = "true" ]; then first=false; else printf ','; fi
		local working="false"
		driver_working_for "$v" && working="true"
		printf '{"vendor":"%s","driver_working":%s}' "$v" "$working"
	done
	printf ']}\n'
}

# ------------------------------------------------------------------------------
# Per-Vendor Install
# ------------------------------------------------------------------------------
install_nvidia() {
	info "Installing NVIDIA driver..."
	if command -v apt-get >/dev/null 2>&1; then
		# ubuntu-drivers (when present - Ubuntu/Pop!_OS/Mint) picks the exact
		# recommended driver version for the detected card, which is more
		# reliable than guessing a single package name that may not match
		# every card generation - fall back to Debian's plain nvidia-driver
		# meta-package (pulls in the current version for the distro release)
		# when it isn't available.
		if command -v ubuntu-drivers >/dev/null 2>&1; then
			ubuntu-drivers autoinstall
		else
			pkg_install nvidia-driver firmware-misc-nonfree || pkg_install nvidia-driver
		fi
	elif command -v dnf >/dev/null 2>&1; then
		if dnf repolist 2>/dev/null | grep -qi rpmfusion; then
			pkg_install akmod-nvidia xorg-x11-drv-nvidia-cuda
		else
			warn "RPM Fusion isn't enabled - Fedora/RHEL ship no NVIDIA driver in their own repos."
			warn "Enable RPM Fusion (see https://rpmfusion.org/Configuration) and re-run this script."
			return 1
		fi
	elif command -v pacman >/dev/null 2>&1; then
		pkg_install nvidia nvidia-utils nvidia-settings
	elif command -v zypper >/dev/null 2>&1; then
		if zypper lr 2>/dev/null | grep -qi nvidia; then
			pkg_install x11-video-nvidiaG06 nvidia-video-G06
		else
			warn "openSUSE's NVIDIA repo isn't added - see https://en.opensuse.org/SDB:NVIDIA_drivers and re-run this script."
			return 1
		fi
	else
		error "No known package manager found - cannot install the NVIDIA driver automatically."
		return 1
	fi
	success "NVIDIA driver installed. A reboot is required before it takes effect."
}

install_amd() {
	info "Installing AMD GPU userspace stack (amdgpu is already in-kernel on any distro new enough to support this card)..."
	if command -v apt-get >/dev/null 2>&1; then
		pkg_install firmware-amd-graphics mesa-vulkan-drivers libgl1-mesa-dri xserver-xorg-video-amdgpu
	elif command -v dnf >/dev/null 2>&1; then
		pkg_install mesa-dri-drivers mesa-vulkan-drivers xorg-x11-drv-amdgpu
	elif command -v pacman >/dev/null 2>&1; then
		pkg_install mesa vulkan-radeon xf86-video-amdgpu
	elif command -v zypper >/dev/null 2>&1; then
		pkg_install Mesa-dri Mesa-libGL1 kernel-firmware-amdgpu
	else
		error "No known package manager found - cannot install the AMD driver stack automatically."
		return 1
	fi
	success "AMD GPU driver/firmware installed. A reboot is recommended."
}

install_intel() {
	info "Installing Intel GPU userspace stack (i915 is already in-kernel)..."
	if command -v apt-get >/dev/null 2>&1; then
		pkg_install intel-media-va-driver mesa-vulkan-drivers libgl1-mesa-dri
	elif command -v dnf >/dev/null 2>&1; then
		pkg_install intel-media-driver mesa-dri-drivers mesa-vulkan-drivers
	elif command -v pacman >/dev/null 2>&1; then
		pkg_install mesa vulkan-intel intel-media-driver
	elif command -v zypper >/dev/null 2>&1; then
		pkg_install Mesa-dri intel-media-driver
	else
		error "No known package manager found - cannot install the Intel driver stack automatically."
		return 1
	fi
	success "Intel GPU driver/media stack installed."
}

install_for_vendor() {
	case "$1" in
		nvidia) install_nvidia ;;
		amd) install_amd ;;
		intel) install_intel ;;
		*) error "Unknown vendor '$1' - expected nvidia, amd, or intel."; return 1 ;;
	esac
}

# ------------------------------------------------------------------------------
# Main
# ------------------------------------------------------------------------------
main() {
	if [ "$STATUS_ONLY" = "true" ]; then
		print_status_json
		exit 0
	fi

	if [ -n "$FORCE_VENDOR" ]; then
		pkg_update
		install_for_vendor "$FORCE_VENDOR"
		exit $?
	fi

	detect_gpu_vendors

	if [ "${#DETECTED_VENDORS[@]}" -eq 0 ]; then
		warn "No NVIDIA, AMD, or Intel GPU detected via lspci."
		if [ -n "${DETECTED_GPU_NAMES:-}" ]; then
			warn "Display controller(s) found, but not from a recognized vendor:"
			printf '%s\n' "$DETECTED_GPU_NAMES" >&2
		fi
		exit 1
	fi

	info "Detected GPU vendor(s): ${DETECTED_VENDORS[*]}"
	pkg_update

	local overall_ok=true
	local v
	for v in "${DETECTED_VENDORS[@]}"; do
		if driver_working_for "$v"; then
			success "${v} driver is already installed and working - skipping."
			continue
		fi
		if ! install_for_vendor "$v"; then
			overall_ok=false
		fi
	done

	if [ "$overall_ok" = "true" ]; then
		success "Done. If a driver was newly installed above, reboot for it to take effect."
	else
		error "One or more drivers failed to install - see the messages above."
		exit 1
	fi
}

main "$@"
