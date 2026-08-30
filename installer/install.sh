#!/usr/bin/env bash
set -euo pipefail

REPO_URL="https://github.com/F-e-n-y-x/recasa.git"
SRC_DIR="/opt/recasa/src"
OS_RELEASE_FILE="${OS_RELEASE_FILE:-/etc/os-release}"
WITH_VM=""
YES=""

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
			echo "error: unsupported distro '$ID' - Recasa's installer supports Debian and Ubuntu only" >&2
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
			*)
				echo "error: unknown argument '$1'" >&2
				exit 1
				;;
		esac
		shift
	done
}

prompt_vm_manager() {
	if [ -n "$WITH_VM" ]; then
		return
	fi
	if [ -n "$YES" ]; then
		WITH_VM=no
		return
	fi
	read -r -p "Install VM Manager (VM creation/management)? [y/N] " reply
	case "$reply" in
		[Yy]*) WITH_VM=yes ;;
		*) WITH_VM=no ;;
	esac
}

install_build_deps() {
	apt-get update
	apt-get install -y git nodejs curl

	if ! command -v go >/dev/null 2>&1 || ! go_version_ok; then
		install_go_toolchain
	fi

	if ! command -v pnpm >/dev/null 2>&1; then
		corepack enable
		corepack prepare pnpm@9.0.6 --activate
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
	curl -fsSL "https://go.dev/dl/go1.23.4.linux-${go_arch}.tar.gz" -o /tmp/go1.23.4.tar.gz
	rm -rf /usr/local/go
	tar -C /usr/local -xzf /tmp/go1.23.4.tar.gz
	rm -f /tmp/go1.23.4.tar.gz
	export PATH="/usr/local/go/bin:$PATH"
	echo 'export PATH="/usr/local/go/bin:$PATH"' > /etc/profile.d/recasa-go.sh
}

clone_repo() {
	if [ -d "$SRC_DIR/.git" ]; then
		echo "found existing checkout at $SRC_DIR, skipping clone"
		return
	fi
	mkdir -p "$(dirname "$SRC_DIR")"
	git clone "$REPO_URL" "$SRC_DIR"
}

CORE_SERVICES="core app-management gateway user local-storage message-bus gpu-sidecar"

install_service() {
	local name="$1"
	local bin_name="recasa-$name"
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
		bash "$setup_script"
	fi
}

GPU_SIDECAR_UNIT=/usr/lib/systemd/system/recasa-gpu-sidecar.service

write_gpu_sidecar_unit() {
	cat > "$GPU_SIDECAR_UNIT" <<'UNIT_EOF'
[Unit]
After=network.target
Description=Recasa GPU Sidecar

[Service]
ExecStart=/usr/bin/recasa-gpu-sidecar
Restart=always

[Install]
WantedBy=multi-user.target
UNIT_EOF
}

install_cli() {
	(
		cd "$SRC_DIR/cli"
		go build -o /usr/bin/recasa .
	)
}

install_core_services() {
	for name in $CORE_SERVICES; do
		install_service "$name"
	done
	write_gpu_sidecar_unit
	systemctl daemon-reload
	systemctl enable --now recasa-gpu-sidecar.service
	install_cli
}

VM_SIDECAR_UNIT=/usr/lib/systemd/system/recasa-vm-sidecar.service

write_vm_sidecar_unit() {
	cat > "$VM_SIDECAR_UNIT" <<'UNIT_EOF'
[Unit]
After=network.target recasa-message-bus.service
Description=Recasa VM Sidecar

[Service]
ExecStart=/usr/bin/recasa-vm-sidecar
Restart=always

[Install]
WantedBy=multi-user.target
UNIT_EOF
}

install_vm_manager() {
	(
		cd "$SRC_DIR/services/vm-sidecar"
		go build -o /usr/bin/recasa-vm-sidecar .
	)
	write_vm_sidecar_unit
	systemctl daemon-reload
	systemctl enable --now recasa-vm-sidecar.service
}

install_ui() {
	(
		cd "$SRC_DIR/ui"
		pnpm install --frozen-lockfile
		pnpm run build
	)
	mkdir -p /var/lib/recasa/www
	cp -a "$SRC_DIR/ui/build/sysroot/var/lib/recasa/www/." /var/lib/recasa/www/
}

LEGACY_SERVICE_UNITS="casaos-gateway.service casaos-message-bus.service casaos.service casaos-user-service.service casaos-app-management.service casaos-local-storage.service"

start_core_services() {
	systemctl daemon-reload
	for unit in $LEGACY_SERVICE_UNITS recasa-gpu-sidecar.service; do
		systemctl start "$unit"
	done
}

print_summary() {
	echo ""
	echo "Recasa install complete."
	echo ""
	echo "Services:"
	for unit in $LEGACY_SERVICE_UNITS recasa-gpu-sidecar.service; do
		systemctl is-active --quiet "$unit" && echo "  [running] $unit" || echo "  [NOT running] $unit"
	done
	if [ "$WITH_VM" = "yes" ]; then
		systemctl is-active --quiet recasa-vm-sidecar.service && echo "  [running] recasa-vm-sidecar.service" || echo "  [NOT running] recasa-vm-sidecar.service"
	else
		echo "  VM Manager was not installed. Run 'recasa vm enable' to add it later."
	fi
	echo ""
	echo "Open http://$(hostname -I | awk '{print $1}')/ in a browser to finish setup."
}

main() {
	check_distro
	parse_args "$@"
	prompt_vm_manager
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
