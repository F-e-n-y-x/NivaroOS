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
