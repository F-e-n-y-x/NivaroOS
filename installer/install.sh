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
