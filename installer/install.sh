#!/usr/bin/env bash
set -euo pipefail

REPO_URL="https://github.com/F-e-n-y-x/NivaroOS.git"
SRC_DIR="/opt/nivaroos/src"
OS_RELEASE_FILE="${OS_RELEASE_FILE:-/etc/os-release}"
WITH_VM=""
YES=""
STEP_NUM=0

export PATH="/usr/local/go/bin:/usr/local/bin:$PATH"

check_root() {
	if [ "$(id -u)" -ne 0 ]; then
		echo "error: NivaroOS installer must be run as root (use sudo or su)" >&2
		exit 1
	fi
}

check_distro() {
	if [ ! -f "$OS_RELEASE_FILE" ]; then
		echo "error: $OS_RELEASE_FILE not found, cannot determine distro" >&2
		exit 1
	fi
	# shellcheck disable=SC1090
	. "$OS_RELEASE_FILE"
	case "$ID" in
		debian|ubuntu) ;;
		*)
			echo "error: unsupported distro '$ID' - NivaroOS's installer supports Debian and Ubuntu only" >&2
			exit 1
			;;
	esac
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
				echo "error: unknown argument '$1'" >&2
				exit 1
				;;
		esac
		shift
	done
}

run_step() {
	local title="$1"
	shift
	STEP_NUM=$((STEP_NUM + 1))
	local log
	log="$(mktemp)"
	local tty_in=/dev/null
	if exec 3</dev/tty 2>/dev/null; then
		exec 3<&-
		tty_in=/dev/tty
	fi
	if command -v gum >/dev/null 2>&1; then
		if gum spin --title "Step ${STEP_NUM}: ${title}" -- bash -c "export PATH=\"/usr/local/go/bin:/usr/local/bin:\$PATH\"; $* >'${log}' 2>&1" <"$tty_in"; then
			rm -f "$log"
		else
			echo "" >&2
			gum style --foreground 196 --bold "✗ Step ${STEP_NUM} failed: ${title}" >&2
			echo "" >&2
			cat "$log" >&2
			rm -f "$log"
			exit 1
		fi
	else
		echo "Step ${STEP_NUM}: ${title}"
		if bash -c "export PATH=\"/usr/local/go/bin:/usr/local/bin:\$PATH\"; $* >'${log}' 2>&1"; then
			rm -f "$log"
		else
			echo "✗ Step ${STEP_NUM} failed: ${title}" >&2
			cat "$log" >&2
			rm -f "$log"
			exit 1
		fi
	fi
}

ADDON_IDS=(vm)
ADDON_LABELS=("VM Manager - create and manage virtual machines")

select_addons() {
	if [ -n "$WITH_VM" ]; then
		return
	fi
	if [ -n "$YES" ] || [ ! -t 0 ] || ! command -v gum >/dev/null 2>&1; then
		WITH_VM=no
		return
	fi
	local chosen
	chosen="$(gum choose --no-limit --header "Select add-ons to install (space to toggle, enter to confirm, none for a minimal install):" "${ADDON_LABELS[@]}" || echo "")"
	local i id upper
	for i in "${!ADDON_IDS[@]}"; do
		id="${ADDON_IDS[$i]}"
		upper="$(printf '%s' "$id" | tr '[:lower:]' '[:upper:]')"
		if printf '%s\n' "$chosen" | grep -qF "${ADDON_LABELS[$i]}"; then
			declare -g "WITH_${upper}=yes"
		else
			declare -g "WITH_${upper}=no"
		fi
	done
}

GUM_VERSION="2.0.0"

# Verifies $1's sha256 against $2; on mismatch, deletes $1 and returns non-zero.
verify_sha256() {
	local file="$1" expected="$2" actual
	actual="$(sha256sum "$file" 2>/dev/null | awk '{print $1}')"
	if [ -z "$expected" ] || [ "$actual" != "$expected" ]; then
		rm -f "$file"
		return 1
	fi
}

install_gum() {
	if command -v gum >/dev/null 2>&1; then
		return
	fi
	apt-get update -qq >/dev/null 2>&1 || true
	apt-get install -y -qq curl tar ca-certificates >/dev/null 2>&1 || true
	local arch gum_arch
	arch="$(dpkg --print-architecture)"
	case "$arch" in
		amd64) gum_arch=x86_64 ;;
		arm64) gum_arch=arm64 ;;
		*)
			return
			;;
	esac
	local tarball="gum_${GUM_VERSION}_Linux_${gum_arch}.tar.gz"
	local expected_sha
	expected_sha="$(curl -fsSL "https://github.com/charmbracelet/gum/releases/download/v${GUM_VERSION}/checksums.txt" 2>/dev/null | awk -v f="$tarball" '$2==f{print $1}')"
	if curl -fsSL "https://github.com/charmbracelet/gum/releases/download/v${GUM_VERSION}/${tarball}" -o "/tmp/${tarball}"; then
		# gum is a cosmetic dependency (the installer falls back to plain
		# stdout when it's missing) - a checksum mismatch skips installing
		# it rather than aborting the whole install.
		if ! verify_sha256 "/tmp/${tarball}" "$expected_sha"; then
			echo "warning: gum download checksum could not be verified, skipping optional gum install" >&2
			rm -f "/tmp/${tarball}"
			return
		fi
		tar -C /tmp -xzf "/tmp/${tarball}" --strip-components=1 "gum_${GUM_VERSION}_Linux_${gum_arch}/gum" 2>/dev/null || true
		if [ -f /tmp/gum ]; then
			install -m 0755 /tmp/gum /usr/local/bin/gum
		fi
		rm -f "/tmp/${tarball}" /tmp/gum
	fi
}

print_banner() {
	if command -v gum >/dev/null 2>&1; then
		gum style \
			--border double --align center --width 50 --margin "1 2" --padding "1 4" \
			--border-foreground 212 --foreground 212 --bold \
			"NIVAROOS" "Self-hosted personal cloud & container platform"
	else
		echo "=================================================="
		echo "  NIVAROOS - Self-hosted personal cloud platform  "
		echo "=================================================="
	fi
}

install_build_deps() {
	run_step "Updating package lists..." apt-get update
	run_step "Installing git, nodejs, npm, curl, build essentials..." apt-get install -y git nodejs npm curl ca-certificates build-essential

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
	printf '%s\n%s\n' "1.23.4" "$ver" | sort -V -C
}

install_go_toolchain() {
	local arch go_arch
	arch="$(dpkg --print-architecture)"
	case "$arch" in
		amd64) go_arch=amd64 ;;
		arm64) go_arch=arm64 ;;
		*)
			echo "error: unsupported architecture '$arch' for Go toolchain install" >&2
			exit 1
			;;
	esac
	local tarball="go1.23.4.linux-${go_arch}.tar.gz"
	# Unlike gum, the Go toolchain is essential (it builds every service), so
	# a verifiable checksum mismatch aborts the install rather than proceeding
	# on a possibly-tampered compiler. go.dev's JSON index is the source of
	# truth for the official sha256; if that lookup itself fails (network
	# hiccup, endpoint shape change) we warn and continue rather than
	# bricking installs over the verification step itself failing.
	local expected_sha
	expected_sha="$(curl -fsSL 'https://go.dev/dl/?mode=json&include=all' 2>/dev/null | tr -d ' \n' | grep -o "\"filename\":\"${tarball}\"[^}]*\"sha256\":\"[a-f0-9]*\"" | grep -o '"sha256":"[a-f0-9]*"' | head -1 | grep -o '[a-f0-9]\{64\}')"
	run_step "Installing the Go toolchain (v1.23.4)..." "curl -fsSL https://go.dev/dl/${tarball} -o /tmp/${tarball}"
	if [ -n "$expected_sha" ]; then
		if ! verify_sha256 "/tmp/${tarball}" "$expected_sha"; then
			echo "error: Go toolchain download failed checksum verification (expected ${expected_sha}) - aborting" >&2
			exit 1
		fi
	else
		echo "warning: could not fetch the official Go toolchain checksum to verify against, proceeding without verification" >&2
	fi
	rm -rf /usr/local/go
	tar -C /usr/local -xzf "/tmp/${tarball}"
	rm -f "/tmp/${tarball}"
	export PATH="/usr/local/go/bin:$PATH"
	echo 'export PATH="/usr/local/go/bin:$PATH"' > /etc/profile.d/nivaroos-go.sh
	ln -sf /usr/local/go/bin/go /usr/bin/go
	ln -sf /usr/local/go/bin/gofmt /usr/bin/gofmt
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

print_summary() {
	local lines=()
	lines+=("NivaroOS installed successfully!")
	lines+=("")
	for unit in $CORE_SERVICE_UNITS nivaroos-gpu-sidecar.service; do
		if systemctl is-active --quiet "$unit"; then
			lines+=("✓ $unit")
		else
			lines+=("✗ $unit (not running)")
		fi
	done
	if [ "$WITH_VM" = "yes" ]; then
		if systemctl is-active --quiet nivaroos-vm-sidecar.service; then
			lines+=("✓ nivaroos-vm-sidecar.service")
		else
			lines+=("✗ nivaroos-vm-sidecar.service (not running)")
		fi
	else
		lines+=("○ VM Manager not installed - run 'nivaroos-cli vm enable' to add it later")
	fi
	lines+=("")
	lines+=("Open http://$(hostname -I | awk '{print $1}')/ to finish setup.")
	if command -v gum >/dev/null 2>&1; then
		gum style --border double --padding "1 2" --border-foreground 212 "${lines[@]}"
	else
		printf '%s\n' "${lines[@]}"
	fi
}

main() {
	check_root
	check_distro
	parse_args "$@"
	install_gum
	print_banner
	select_addons
	install_build_deps
	clone_repo
	install_core_services
	if [ "$WITH_VM" = "yes" ]; then
		install_vm_manager
	fi
	install_ui
	start_core_services
	print_summary
}

main "$@"

