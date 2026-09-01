#!/usr/bin/env bash
set -euo pipefail

# Mirrors install.sh's CORE_SERVICE_UNITS + the units it writes itself
# (GPU_SIDECAR_UNIT / VM_SIDECAR_UNIT) - keep these two lists in sync if
# either script's service list changes.
SRC_DIR="/opt/nivaroos/src"
ALL_UNITS="nivaroos-gateway.service nivaroos-message-bus.service nivaroos.service nivaroos-user-service.service nivaroos-app-management.service nivaroos-local-storage.service nivaroos-gpu-sidecar.service nivaroos-vm-sidecar.service rclone.service"

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
		/usr/lib/systemd/system/nivaroos-gateway.service \
		/usr/lib/systemd/system/nivaroos-gateway.service.buildroot \
		/usr/lib/systemd/system/nivaroos-message-bus.service \
		/usr/lib/systemd/system/nivaroos.service \
		/usr/lib/systemd/system/nivaroos-user-service.service \
		/usr/lib/systemd/system/nivaroos-app-management.service \
		/usr/lib/systemd/system/nivaroos-app-management.service.buildroot \
		/usr/lib/systemd/system/nivaroos-local-storage.service \
		/usr/lib/systemd/system/nivaroos-gpu-sidecar.service \
		/usr/lib/systemd/system/nivaroos-vm-sidecar.service \
		/usr/lib/systemd/system/rclone.service
	systemctl daemon-reload
}

remove_binaries() {
	rm -f \
		/usr/bin/nivaroos /usr/bin/nivaroos-gateway /usr/bin/nivaroos-user \
		/usr/bin/nivaroos-app-management /usr/bin/nivaroos-local-storage \
		/usr/bin/nivaroos-message-bus /usr/bin/nivaroos-vm-sidecar \
		/usr/bin/nivaroos-gpu-sidecar /usr/bin/nivaroos-cli \
		/usr/bin/nivaroos-uninstall /usr/local/bin/mergerfs.ctl
}

remove_dirs() {
	rm -rf /etc/nivaroos /usr/share/nivaroos /var/lib/nivaroos "$(dirname "$SRC_DIR")"
	rm -f /etc/profile.d/nivaroos-go.sh
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
	echo "The Go toolchain, Node.js, pnpm, and Docker were left in place - they aren't managed by this installer."
}

main "$@"
