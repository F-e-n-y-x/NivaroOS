#!/usr/bin/env bash
# This script uses bash-only syntax (arrays, etc.) and will fail with a
# confusing syntax error if run under a POSIX `sh` (e.g. dash, the default
# /bin/sh on Debian/Ubuntu) instead of bash - `sh install.sh` or a shebang-
# ignoring invocation both hit this. Detect that and transparently re-exec
# under bash instead of failing. This block itself must stay POSIX sh
# compatible so dash can parse it far enough to hit the re-exec.
if [ -z "${BASH_VERSION:-}" ]; then
	exec bash "$0" "$@"
fi

set -euo pipefail

REPO_URL="https://github.com/F-e-n-y-x/NivaroOS.git"
SRC_DIR="/opt/nivaroos/src"
OS_RELEASE_FILE="${OS_RELEASE_FILE:-/etc/os-release}"
GO_VERSION="1.23.4"
MIN_RECOMMENDED_MEMORY_MB="1024"
MIN_REQUIRED_MEMORY_MB="256"
MIN_RECOMMENDED_DISK_GB="5"
MIN_REQUIRED_DISK_GB="1"
WITH_VM=""
YES=""
STEP_NUM=0

export PATH="/usr/local/go/bin:/usr/local/bin:$PATH"

# Piping this script via `curl | bash` means the script's own body is still
# being streamed from stdin while it runs. If any subprocess we invoke tries
# to read from stdin too - most commonly apt-get's debconf frontend, or
# needrestart's whiptail prompt on Ubuntu - it races with bash's own read of
# the remaining script and both bash and the installer can silently die
# mid-script with no error message. Force every apt-get call non-interactive
# so nothing ever tries.
export DEBIAN_FRONTEND=noninteractive
export NEEDRESTART_MODE=a

###############################################################################
# Output helpers - deliberately dependency-free (no gum/whiptail/etc). A
# previous version of this script wrapped step execution and the add-on
# picker in `gum spin`/`gum choose` for a nicer look, but that put an
# external TUI tool's exit-code/terminal-state handling on the critical path
# for "did the install actually succeed" - which is exactly the kind of
# thing an installer can't afford to get subtly wrong. Plain, boring output
# is worth more here than a spinner.
###############################################################################

COLOR_RESET='\033[0m'
COLOR_BOLD='\033[1m'
COLOR_GREEN='\033[32m'
COLOR_YELLOW='\033[33m'
COLOR_RED='\033[31m'

info()  { printf '%b\n' "${COLOR_GREEN}[INFO]${COLOR_RESET} $1"; }
warn()  { printf '%b\n' "${COLOR_YELLOW}[WARN]${COLOR_RESET} $1" >&2; }
error() { printf '%b\n' "${COLOR_RED}[ERROR]${COLOR_RESET} $1" >&2; }

# Any command that fails without going through run_step's own error handling
# (e.g. a bare command at the top level of a function) still gets a clear,
# actionable message instead of the script just silently vanishing.
on_error() {
	local exit_code=$? line="$1"
	error "Installation failed at installer line ${line} (exit code ${exit_code})."
	error "Please open an issue at https://github.com/F-e-n-y-x/NivaroOS/issues and include this output."
	exit "$exit_code"
}
trap 'on_error "$LINENO"' ERR

check_root() {
	if [ "$(id -u)" -ne 0 ]; then
		error "NivaroOS installer must be run as root (use sudo or su)."
		exit 1
	fi
}

check_distro() {
	if [ ! -f "$OS_RELEASE_FILE" ]; then
		error "$OS_RELEASE_FILE not found, cannot determine distro."
		exit 1
	fi
	# shellcheck disable=SC1090
	. "$OS_RELEASE_FILE"
	local family="${ID:-} ${ID_LIKE:-}"
	case " $family " in
		*" debian "*|*" ubuntu "*) ;;
		*)
			error "Unsupported distro '${ID:-unknown}' - NivaroOS's installer supports Debian, Ubuntu, and their derivatives (Mint, Pop!_OS, Raspberry Pi OS, etc.) only."
			exit 1
			;;
	esac
	info "Distro check passed: ${PRETTY_NAME:-$ID}"
}

# Non-fatal below the recommended thresholds (a piped/unattended install
# shouldn't block on this), fatal only when resources are low enough that
# the build steps below are essentially guaranteed to fail anyway.
check_resources() {
	local mem_mb disk_gb
	mem_mb="$(LC_ALL=C free -m 2>/dev/null | awk '/^Mem:/ { print $2 }')"
	disk_gb="$(($(LC_ALL=C df -P / 2>/dev/null | tail -n 1 | awk '{print $4}') / 1024 / 1024))"

	if [ -n "$mem_mb" ]; then
		if [ "$mem_mb" -lt "$MIN_REQUIRED_MEMORY_MB" ]; then
			error "Only ${mem_mb}MB of memory detected - NivaroOS needs at least ${MIN_REQUIRED_MEMORY_MB}MB to install."
			exit 1
		elif [ "$mem_mb" -lt "$MIN_RECOMMENDED_MEMORY_MB" ]; then
			warn "Only ${mem_mb}MB of memory detected - ${MIN_RECOMMENDED_MEMORY_MB}MB+ is recommended. Continuing anyway."
		fi
	fi
	if [ -n "$disk_gb" ]; then
		if [ "$disk_gb" -lt "$MIN_REQUIRED_DISK_GB" ]; then
			error "Only ${disk_gb}GB of free disk space on / - NivaroOS needs at least ${MIN_REQUIRED_DISK_GB}GB to install."
			exit 1
		elif [ "$disk_gb" -lt "$MIN_RECOMMENDED_DISK_GB" ]; then
			warn "Only ${disk_gb}GB of free disk space on / - ${MIN_RECOMMENDED_DISK_GB}GB+ is recommended. Continuing anyway."
		fi
	fi
}

parse_args() {
	while [ $# -gt 0 ]; do
		case "$1" in
			--with-vm) WITH_VM=yes ;;
			--without-vm) WITH_VM=no ;;
			--yes|-y) YES=yes ;;
			--help|-h)
				echo "NivaroOS Installer"
				echo "Usage: install.sh [options]"
				echo ""
				echo "Options:"
				echo "  --yes, -y       Skip interactive prompts and install defaults"
				echo "  --with-vm       Install with VM Manager support (QEMU/KVM + libvirt)"
				echo "  --without-vm    Install without VM Manager support"
				echo "  --help, -h      Show this help message"
				exit 0
				;;
			*)
				error "Unknown argument '$1'."
				exit 1
				;;
		esac
		shift
	done
}

# Runs "$*" with its stdin isolated from this script's own (in a `curl|bash`
# invocation, still-live) stdin, logging output and only surfacing it on
# failure so a successful run stays quiet.
run_step() {
	local title="$1"
	shift
	STEP_NUM=$((STEP_NUM + 1))
	info "Step ${STEP_NUM}: ${title}"
	local log
	log="$(mktemp)"
	if ! bash -c "export PATH=\"/usr/local/go/bin:/usr/local/bin:\$PATH\"; $*" >"$log" 2>&1 </dev/null; then
		error "Step ${STEP_NUM} failed: ${title}"
		cat "$log" >&2
		rm -f "$log"
		exit 1
	fi
	rm -f "$log"
}

select_addons() {
	if [ -n "$WITH_VM" ]; then
		return
	fi
	if [ -n "$YES" ] || [ ! -t 0 ]; then
		WITH_VM=no
		return
	fi
	local reply=""
	read -r -p "Install the VM Manager add-on (QEMU/KVM + libvirt)? [y/N] " reply </dev/tty || reply=""
	case "$reply" in
		[yY]|[yY][eE][sS]) WITH_VM=yes ;;
		*) WITH_VM=no ;;
	esac
}

print_banner() {
	printf '%b' "${COLOR_BOLD}${COLOR_GREEN}"
	cat <<'EOF'
  ╔════════════════════════════════════════════════╗
  ║                    NIVAROOS                     ║
  ║     Self-hosted personal cloud & container      ║
  ║                    platform                     ║
  ╚════════════════════════════════════════════════╝
EOF
	printf '%b\n' "${COLOR_RESET}"
}

# Verifies $1's sha256 against $2; on mismatch, deletes $1 and returns non-zero.
verify_sha256() {
	local file="$1" expected="$2" actual
	actual="$(sha256sum "$file" 2>/dev/null | awk '{print $1}')"
	if [ -z "$expected" ] || [ "$actual" != "$expected" ]; then
		rm -f "$file"
		return 1
	fi
}

install_build_deps() {
	run_step "Updating package lists..." apt-get update
	run_step "Installing build and runtime dependencies..." apt-get install -y \
		git nodejs npm curl ca-certificates build-essential \
		smartmontools hdparm parted ntfs-3g samba

	if ! command -v go >/dev/null 2>&1 || ! go_version_ok; then
		install_go_toolchain
	fi

	if ! command -v pnpm >/dev/null 2>&1; then
		if command -v corepack >/dev/null 2>&1; then
			corepack enable || true
			corepack prepare pnpm@9.0.6 --activate || true
		fi
		if ! command -v pnpm >/dev/null 2>&1; then
			npm install -g pnpm@9.0.6 >/dev/null 2>&1 || curl -fsSL https://get.pnpm.io/install.sh | env PNPM_VERSION=9.0.6 sh - || true
		fi
	fi
}

go_version_ok() {
	local ver
	ver="$(go version | awk '{print $3}' | sed 's/^go//')"
	printf '%s\n%s\n' "$GO_VERSION" "$ver" | sort -V -C
}

install_go_toolchain() {
	local arch go_arch
	arch="$(dpkg --print-architecture)"
	case "$arch" in
		amd64) go_arch=amd64 ;;
		arm64) go_arch=arm64 ;;
		*)
			error "Unsupported architecture '$arch' for Go toolchain install."
			exit 1
			;;
	esac
	local tarball="go${GO_VERSION}.linux-${go_arch}.tar.gz"
	# Unlike optional tooling, the Go toolchain is essential (it builds every
	# service), so a verifiable checksum mismatch aborts the install rather
	# than proceeding on a possibly-tampered compiler. go.dev's JSON index is
	# the source of truth for the official sha256; if that lookup itself
	# fails (network hiccup, endpoint shape change) we warn and continue
	# rather than bricking installs over the verification step itself failing.
	local expected_sha
	expected_sha="$(curl -fsSL 'https://go.dev/dl/?mode=json&include=all' 2>/dev/null | tr -d ' \n' | grep -o "\"filename\":\"${tarball}\"[^}]*\"sha256\":\"[a-f0-9]*\"" | grep -o '"sha256":"[a-f0-9]*"' | head -1 | grep -o '[a-f0-9]\{64\}')"
	run_step "Installing the Go toolchain (v${GO_VERSION})..." "curl -fsSL https://go.dev/dl/${tarball} -o /tmp/${tarball}"
	if [ -n "$expected_sha" ]; then
		if ! verify_sha256 "/tmp/${tarball}" "$expected_sha"; then
			error "Go toolchain download failed checksum verification (expected ${expected_sha}) - aborting."
			exit 1
		fi
	else
		warn "Could not fetch the official Go toolchain checksum to verify against, proceeding without verification."
	fi
	rm -rf /usr/local/go
	tar -C /usr/local -xzf "/tmp/${tarball}"
	rm -f "/tmp/${tarball}"
	export PATH="/usr/local/go/bin:$PATH"
	echo 'export PATH="/usr/local/go/bin:$PATH"' > /etc/profile.d/nivaroos-go.sh
	ln -sf /usr/local/go/bin/go /usr/bin/go
	ln -sf /usr/local/go/bin/gofmt /usr/bin/gofmt
}

# app-management drives Docker directly (compose apps, the app store) - it
# is not optional. Installs it via Docker's own official convenience script
# if missing, and makes sure the daemon is actually up either way.
check_docker() {
	if ! command -v docker >/dev/null 2>&1; then
		run_step "Installing Docker..." "curl -fsSL https://get.docker.com | sh"
	fi
	if ! systemctl is-enabled --quiet docker 2>/dev/null; then
		systemctl enable docker >/dev/null 2>&1 || true
	fi
	if ! systemctl is-active --quiet docker 2>/dev/null; then
		run_step "Starting Docker..." systemctl start docker
	fi
	if ! docker version >/dev/null 2>&1; then
		error "Docker was installed but isn't responding - check 'systemctl status docker'."
		exit 1
	fi
	info "Docker is installed and running."
}

clone_repo() {
	if [ -d "$SRC_DIR/.git" ]; then
		run_step "Updating existing checkout..." "git -C '$SRC_DIR' fetch origin master && git -C '$SRC_DIR' reset --hard origin/master"
		return
	fi
	rm -rf "$SRC_DIR"
	mkdir -p "$(dirname "$SRC_DIR")"
	run_step "Cloning NivaroOS..." git clone "$REPO_URL" "$SRC_DIR"
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
		run_step "Building $bin_name..." go build -o "/usr/bin/$bin_name" .
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

install_cli() {
	(
		cd "$SRC_DIR/cli"
		run_step "Building nivaroos-cli..." go build -o /usr/bin/nivaroos-cli .
	)
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
}

install_core_services() {
	init_directories_and_configs
	for name in $CORE_SERVICES; do
		install_service "$name"
	done
	init_directories_and_configs
	write_gpu_sidecar_unit
	systemctl daemon-reload
	systemctl enable --now nivaroos-gpu-sidecar.service >/dev/null 2>&1 || true
	install_cli
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
	run_step "Installing libvirt, QEMU KVM, and dependencies..." apt-get install -y libvirt-dev gcc libvirt-daemon-system qemu-kvm qemu-utils
	systemctl enable --now libvirtd.service >/dev/null 2>&1 || true
	(
		cd "$SRC_DIR/services/vm-sidecar"
		run_step "Building nivaroos-vm-sidecar..." go build -o /usr/bin/nivaroos-vm-sidecar .
	)
	write_vm_sidecar_unit
	systemctl daemon-reload
	systemctl enable --now nivaroos-vm-sidecar.service >/dev/null 2>&1 || true
}

install_ui() {
	(
		cd "$SRC_DIR/ui"
		run_step "Installing UI dependencies..." pnpm install --frozen-lockfile
		run_step "Building the web UI..." pnpm run build
	)
	mkdir -p /var/lib/nivaroos/www /var/lib/casaos/www
	cp -a "$SRC_DIR/ui/build/sysroot/var/lib/nivaroos/www/." /var/lib/nivaroos/www/
	cp -a "$SRC_DIR/ui/build/sysroot/var/lib/nivaroos/www/." /var/lib/casaos/www/ 2>/dev/null || true
}

CORE_SERVICE_UNITS="nivaroos-gateway.service nivaroos-message-bus.service nivaroos.service nivaroos-user-service.service nivaroos-app-management.service nivaroos-local-storage.service"

start_core_services() {
	systemctl daemon-reload
	for unit in $CORE_SERVICE_UNITS nivaroos-gpu-sidecar.service; do
		systemctl enable --now "$unit" >/dev/null 2>&1 || true
		systemctl restart "$unit" >/dev/null 2>&1 || true
	done
}

# A thin, memorable wrapper around the actual uninstall.sh in the checked-out
# source tree, so users don't need to remember where that is or re-fetch it.
install_uninstall_wrapper() {
	cat > /usr/bin/nivaroos-uninstall <<EOF
#!/usr/bin/env bash
exec bash "$SRC_DIR/installer/uninstall.sh" "\$@"
EOF
	chmod +x /usr/bin/nivaroos-uninstall
}

# Every real (non-virtual) NIC's IPv4 address, for a useful "where do I
# actually reach this thing" summary on multi-NIC/multi-bridge hosts, where
# `hostname -I`'s first entry is frequently a Docker bridge, not the LAN.
list_reachable_ips() {
	ip -4 -o addr show scope global 2>/dev/null | awk '{print $2, $4}' | while read -r iface cidr; do
		case "$iface" in
			docker*|veth*|br-*|virbr*) continue ;;
		esac
		echo "${cidr%/*} ${iface}"
	done
}

print_summary() {
	echo ""
	info "NivaroOS installed successfully!"
	echo ""
	for unit in $CORE_SERVICE_UNITS nivaroos-gpu-sidecar.service; do
		if systemctl is-active --quiet "$unit"; then
			printf '%b\n' "  ${COLOR_GREEN}✓${COLOR_RESET} $unit"
		else
			printf '%b\n' "  ${COLOR_RED}✗${COLOR_RESET} $unit (not running)"
		fi
	done
	if [ "$WITH_VM" = "yes" ]; then
		if systemctl is-active --quiet nivaroos-vm-sidecar.service; then
			printf '%b\n' "  ${COLOR_GREEN}✓${COLOR_RESET} nivaroos-vm-sidecar.service"
		else
			printf '%b\n' "  ${COLOR_RED}✗${COLOR_RESET} nivaroos-vm-sidecar.service (not running)"
		fi
	else
		echo "  ○ VM Manager not installed - run 'nivaroos-cli vm enable' to add it later"
	fi
	echo ""
	local port
	port="$(grep -m1 '^port=' /etc/nivaroos/gateway.ini 2>/dev/null | cut -d= -f2)"
	local ips
	ips="$(list_reachable_ips)"
	if [ -n "$ips" ]; then
		info "Open your browser and visit:"
		while read -r ip iface; do
			[ -z "$ip" ] && continue
			if [ -z "$port" ] || [ "$port" = "80" ]; then
				echo "  - http://${ip} (${iface})"
			else
				echo "  - http://${ip}:${port} (${iface})"
			fi
		done <<< "$ips"
	else
		info "Open http://$(hostname -I | awk '{print $1}')/ to finish setup."
	fi
	echo ""
	echo "Uninstall: nivaroos-uninstall"
}

main() {
	parse_args "$@"
	check_root
	check_distro
	check_resources
	print_banner
	select_addons
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
