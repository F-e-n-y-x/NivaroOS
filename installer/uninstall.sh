#!/usr/bin/env bash
set -euo pipefail

# Mirrors install.sh's LEGACY_SERVICE_UNITS + the units it writes itself
# (GPU_SIDECAR_UNIT / VM_SIDECAR_UNIT) - keep these two lists in sync if
# either script's service list changes.
SRC_DIR="/opt/recasa/src"
ALL_UNITS="casaos-gateway.service casaos-message-bus.service casaos.service casaos-user-service.service casaos-app-management.service casaos-local-storage.service recasa-gpu-sidecar.service recasa-vm-sidecar.service rclone.service"

PURGE_DATA=""
YES=""

parse_args() {
	while [ $# -gt 0 ]; do
		case "$1" in
			--purge-data) PURGE_DATA=yes ;;
			--yes|-y) YES=yes ;;
			*)
				echo "error: unknown argument '$1'" >&2
				exit 1
				;;
		esac
		shift
	done
}

stop_services() {
	for unit in $ALL_UNITS; do
		systemctl disable --now "$unit" >/dev/null 2>&1 || true
	done
}

remove_unit_files() {
	rm -f \
		/usr/lib/systemd/system/casaos-gateway.service \
		/usr/lib/systemd/system/casaos-gateway.service.buildroot \
		/usr/lib/systemd/system/casaos-message-bus.service \
		/usr/lib/systemd/system/casaos.service \
		/usr/lib/systemd/system/casaos-user-service.service \
		/usr/lib/systemd/system/casaos-app-management.service \
		/usr/lib/systemd/system/casaos-app-management.service.buildroot \
		/usr/lib/systemd/system/casaos-local-storage.service \
		/usr/lib/systemd/system/recasa-gpu-sidecar.service \
		/usr/lib/systemd/system/recasa-vm-sidecar.service \
		/usr/lib/systemd/system/rclone.service
	systemctl daemon-reload
}

remove_binaries() {
	rm -f \
		/usr/bin/recasa /usr/bin/recasa-gateway /usr/bin/recasa-user \
		/usr/bin/recasa-app-management /usr/bin/recasa-local-storage \
		/usr/bin/recasa-message-bus /usr/bin/recasa-vm-sidecar \
		/usr/bin/recasa-gpu-sidecar /usr/bin/recasa-cli \
		/usr/local/bin/mergerfs.ctl
}

remove_dirs() {
	rm -rf /etc/recasa /usr/share/recasa /var/lib/recasa "$(dirname "$SRC_DIR")"
	rm -f /etc/profile.d/recasa-go.sh
}

purge_data() {
	if [ -z "$PURGE_DATA" ]; then
		return
	fi
	if [ -t 0 ]; then
		echo "This will permanently delete /DATA - all app data, VM disks, and files." >&2
		read -r -p "Type 'DELETE' to confirm: " confirm
		if [ "$confirm" != "DELETE" ]; then
			echo "Aborted - /DATA left untouched." >&2
			return
		fi
	elif [ -z "$YES" ]; then
		echo "error: --purge-data needs --yes as well when not running interactively" >&2
		exit 1
	fi
	rm -rf /DATA
}

main() {
	parse_args "$@"
	stop_services
	remove_unit_files
	remove_binaries
	remove_dirs
	purge_data
	echo "NivaroOS has been uninstalled."
	if [ -z "$PURGE_DATA" ]; then
		echo "App data, VM disks, and files under /DATA were left untouched. Re-run with --purge-data to remove those too."
	fi
	echo "The Go toolchain, Node.js/pnpm, and gum were left in place - they aren't managed by this installer."
}

main "$@"
