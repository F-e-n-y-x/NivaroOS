#!/usr/bin/env bash
# Installs (or updates) the Download Station browser: a Chromium the
# nivaroos-ds-browser service can run, its unprivileged user, its units, and
# the vendored uBlock Origin Lite. Idempotent - installer/install.sh runs it
# on every install and update (so existing boxes get it when they update),
# and nivaroos-ds-browser-install.service runs it when the user presses
# "Install full browser" in Download Station.
#
# It never makes Chromium run without its sandbox. If this box cannot give
# it one, the service reports that and Download Station stays in Lite mode.
#
# Usage: install-ds-browser.sh [--src <NivaroOS checkout>] [--manifest <file>]
set -u
export PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:${PATH:-}"
umask 022

SRC_DIR=""
MANIFEST_FILE="${MANIFEST_FILE:-/var/lib/nivaroos/manifest}"
while [ $# -gt 0 ]; do
	case "$1" in
		--src) SRC_DIR="$2"; shift ;;
		--manifest) MANIFEST_FILE="$2"; shift ;;
	esac
	shift
done
if [ -z "$SRC_DIR" ]; then
	SRC_DIR="$(cd "$(dirname "$0")/../../../.." && pwd)"
fi
SYSROOT="${SRC_DIR}/services/download-sidecar/build/sysroot"

# Pinned downloads (bump together with a test on a real box).
UBOL_VERSION="2026.926.2202"
UBOL_SHA256="9a0d94e832fde9430f64817ff1ba3f34040f19caa113a24e6d84aad1d05eb1aa"
UBOL_URL="https://github.com/uBlockOrigin/uBOL-home/releases/download/${UBOL_VERSION}/uBOLite_${UBOL_VERSION}.chromium.zip"
CFT_VERSION="154.0.8037.57"
CFT_SHA256="ceee2972074d441ea7c4ba8bcc0eaab77e7e87680f6653d73d3065851fe10302"
CFT_URL="https://storage.googleapis.com/chrome-for-testing-public/${CFT_VERSION}/linux64/chrome-linux64.zip"

STATE_DIR=/var/lib/nivaroos/ds-browser
SHARE_DIR=/usr/share/nivaroos/ds-browser
CFT_DIR=/opt/nivaroos/chromium
MIN_MAJOR=120

say() { echo "ds-browser: $*"; }
manifest() { mkdir -p "$(dirname "$MANIFEST_FILE")"; grep -qxF "$1" "$MANIFEST_FILE" 2>/dev/null || echo "$1" >> "$MANIFEST_FILE"; }
# What this script added that is not a file (packages, the user), for
# uninstall.sh - kept out of the main manifest, which lists paths.
record() { mkdir -p "$SHARE_DIR"; grep -qxF "$1" "$SHARE_DIR/installed.txt" 2>/dev/null || echo "$1" >> "$SHARE_DIR/installed.txt"; }

mem_mb() { awk '/MemTotal:/ { print int($2/1024) }' /proc/meminfo 2>/dev/null || echo 0; }

os_family() {
	local id="" like=""
	if [ -f /etc/os-release ]; then
		# shellcheck disable=SC1091
		. /etc/os-release
		id="${ID:-}"; like="${ID_LIKE:-}"
	fi
	case " $id $like " in
		*" ubuntu "*|*" pop "*|*" mint "*|*" zorin "*) echo ubuntu ;;
		*" debian "*|*" raspbian "*|*" armbian "*|*" kali "*) echo debian ;;
		*" fedora "*|*" rhel "*|*" centos "*|*" rocky "*|*" almalinux "*|*" amzn "*) echo fedora ;;
		*" arch "*|*" manjaro "*|*" endeavouros "*) echo arch ;;
		*" opensuse "*|*" sles "*|*" suse "*) echo suse ;;
		*) echo other ;;
	esac
}

chrome_major() {
	"$1" --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+' | head -1 | cut -d. -f1
}

# The same search order as the service (rb_host.go rbChromeCandidates).
find_chrome() {
	local c
	for c in "$CFT_DIR/chrome" /usr/bin/google-chrome-stable /usr/bin/google-chrome /opt/google/chrome/chrome /usr/bin/chromium /usr/lib/chromium/chromium /usr/bin/chromium-browser; do
		[ -x "$c" ] || continue
		# Ubuntu's chromium-browser shim only installs the snap.
		if [ "$(stat -c %s "$c" 2>/dev/null || echo 0)" -lt 65536 ] && grep -q snap "$c" 2>/dev/null; then
			continue
		fi
		local m
		m="$(chrome_major "$c")"
		if [ -n "$m" ] && [ "$m" -ge "$MIN_MAJOR" ]; then
			echo "$c"
			return 0
		fi
	done
	return 1
}

apt_install() { DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "$@"; }

install_fonts() {
	# Headless Chromium draws text with whatever fonts the system has.
	if command -v apt-get >/dev/null 2>&1; then
		apt_install fonts-liberation fonts-dejavu-core fonts-noto-color-emoji >/dev/null 2>&1 || true
	elif command -v dnf >/dev/null 2>&1; then
		dnf install -y liberation-fonts dejavu-sans-fonts google-noto-emoji-color-fonts >/dev/null 2>&1 || true
	elif command -v pacman >/dev/null 2>&1; then
		pacman -S --noconfirm --needed ttf-liberation ttf-dejavu noto-fonts-emoji >/dev/null 2>&1 || true
	elif command -v zypper >/dev/null 2>&1; then
		zypper -n in liberation-fonts dejavu-fonts noto-coloremoji-fonts >/dev/null 2>&1 || true
	fi
}

install_google_chrome_repo() {
	# Ubuntu's own chromium is a snap, which cannot run under the service's
	# user - Google's signed repository is used instead (amd64 only).
	[ "$(dpkg --print-architecture 2>/dev/null)" = "amd64" ] || return 1
	mkdir -p /etc/apt/keyrings
	curl -fsSL --connect-timeout 15 --max-time 60 https://dl.google.com/linux/linux_signing_key.pub | gpg --dearmor --yes -o /etc/apt/keyrings/google-chrome.gpg || return 1
	echo "deb [arch=amd64 signed-by=/etc/apt/keyrings/google-chrome.gpg] https://dl.google.com/linux/chrome/deb/ stable main" > /etc/apt/sources.list.d/google-chrome.list
	apt-get update -o Dir::Etc::sourcelist=sources.list.d/google-chrome.list -o Dir::Etc::sourceparts=- -o APT::Get::List-Cleanup=0 >/dev/null 2>&1 || apt-get update >/dev/null 2>&1 || return 1
	apt_install google-chrome-stable || return 1
	record "package:google-chrome-stable"
	record "file:/etc/apt/sources.list.d/google-chrome.list"
}

install_chrome_for_testing() {
	[ "$(uname -m)" = "x86_64" ] || return 1
	local tmp
	tmp="$(mktemp -d)"
	say "downloading Chrome for Testing ${CFT_VERSION}..."
	if ! curl -fL --connect-timeout 15 --max-time 900 -o "$tmp/cft.zip" "$CFT_URL"; then
		rm -rf "$tmp"; return 1
	fi
	if [ "$(sha256sum "$tmp/cft.zip" | cut -d' ' -f1)" != "$CFT_SHA256" ]; then
		say "Chrome for Testing download failed its checksum - not using it"
		rm -rf "$tmp"; return 1
	fi
	command -v unzip >/dev/null 2>&1 || { apt_install unzip >/dev/null 2>&1 || dnf install -y unzip >/dev/null 2>&1 || pacman -S --noconfirm unzip >/dev/null 2>&1 || zypper -n in unzip >/dev/null 2>&1; }
	unzip -q "$tmp/cft.zip" -d "$tmp" || { rm -rf "$tmp"; return 1; }
	rm -rf "${CFT_DIR}.new"
	mkdir -p "$(dirname "$CFT_DIR")"
	mv "$tmp/chrome-linux64" "${CFT_DIR}.new"
	rm -rf "$CFT_DIR"
	mv "${CFT_DIR}.new" "$CFT_DIR"
	rm -rf "$tmp"
	# The shared libraries it links against.
	if command -v dnf >/dev/null 2>&1; then
		dnf install -y nss atk at-spi2-atk cups-libs libdrm libxkbcommon mesa-libgbm alsa-lib pango libXcomposite libXdamage libXrandr >/dev/null 2>&1 || true
	elif command -v zypper >/dev/null 2>&1; then
		zypper -n in mozilla-nss libatk-1_0-0 libatk-bridge-2_0-0 cups-libs libdrm2 libxkbcommon0 libgbm1 libasound2 libpango-1_0-0 >/dev/null 2>&1 || true
	elif command -v apt-get >/dev/null 2>&1; then
		apt_install libnss3 libatk1.0-0 libatk-bridge2.0-0 libcups2 libdrm2 libxkbcommon0 libgbm1 libasound2 libpango-1.0-0 libxcomposite1 libxdamage1 libxrandr2 >/dev/null 2>&1 || true
	fi
	manifest "$CFT_DIR"
}

ensure_chrome() {
	local c
	if c="$(find_chrome)"; then
		say "using $c ($("$c" --version 2>/dev/null))"
		return 0
	fi
	say "no usable Chrome/Chromium found - installing one"
	case "$(os_family)" in
		debian) apt-get update >/dev/null 2>&1; apt_install chromium && record "package:chromium" ;;
		ubuntu) install_google_chrome_repo ;;
		fedora) dnf install -y chromium && record "package:chromium" ;;
		arch) pacman -S --noconfirm --needed chromium && record "package:chromium" ;;
		suse) zypper -n in chromium && record "package:chromium" ;;
	esac
	if ! find_chrome >/dev/null; then
		install_chrome_for_testing || true
	fi
	if c="$(find_chrome)"; then
		say "installed $c"
		return 0
	fi
	say "could not install Chromium on this system - Download Station will use its Lite browser"
	return 1
}

ensure_user() {
	if ! id nivaroos-browser >/dev/null 2>&1; then
		local nologin=/usr/sbin/nologin
		[ -x "$nologin" ] || nologin=/sbin/nologin
		useradd --system --no-create-home --home-dir "$STATE_DIR" --shell "$nologin" --user-group nivaroos-browser 2>/dev/null \
			|| useradd -r -M -d "$STATE_DIR" -s "$nologin" nivaroos-browser 2>/dev/null \
			|| adduser -S -H -h "$STATE_DIR" -s "$nologin" nivaroos-browser 2>/dev/null
		if ! id nivaroos-browser >/dev/null 2>&1; then
			say "could not create the nivaroos-browser user"
			return 1
		fi
		record "user:nivaroos-browser"
	fi
	mkdir -p "$STATE_DIR/profiles" "$STATE_DIR/staging"
	chown -R nivaroos-browser:nivaroos-browser "$STATE_DIR"
	chmod 700 "$STATE_DIR" "$STATE_DIR/profiles"
	# Root (Download Station) moves finished downloads out of here.
	chmod 2770 "$STATE_DIR/staging"
}

# Chromium's sandbox needs unprivileged user namespaces.
ensure_userns() {
	if [ -f /proc/sys/kernel/unprivileged_userns_clone ] && [ "$(cat /proc/sys/kernel/unprivileged_userns_clone)" = "0" ]; then
		echo "kernel.unprivileged_userns_clone = 1" > /etc/sysctl.d/60-nivaroos-ds-browser.conf
		sysctl -q -p /etc/sysctl.d/60-nivaroos-ds-browser.conf >/dev/null 2>&1 || true
		manifest /etc/sysctl.d/60-nivaroos-ds-browser.conf
	fi
	# Ubuntu 23.10+ only allows them to binaries an AppArmor profile names.
	if [ -f /proc/sys/kernel/apparmor_restrict_unprivileged_userns ] && [ "$(cat /proc/sys/kernel/apparmor_restrict_unprivileged_userns)" = "1" ] && command -v apparmor_parser >/dev/null 2>&1; then
		local c real
		c="$(find_chrome)" || return 0
		real="$(readlink -f "$c")"
		# The real binary behind a wrapper script.
		case "$real" in
			/usr/bin/google-chrome*) real=/opt/google/chrome/chrome ;;
			/usr/bin/chromium) [ -x /usr/lib/chromium/chromium ] && real=/usr/lib/chromium/chromium ;;
		esac
		cat > /etc/apparmor.d/nivaroos-ds-browser <<EOF
# NivaroOS Download Station browser: let Chromium create the user
# namespaces its sandbox needs (Ubuntu restricts them by default).
abi <abi/4.0>,
include <tunables/global>

profile nivaroos-ds-browser ${real} flags=(unconfined) {
  userns,
  include if exists <local/nivaroos-ds-browser>
}
EOF
		apparmor_parser -r /etc/apparmor.d/nivaroos-ds-browser >/dev/null 2>&1 || say "could not load the AppArmor profile"
		manifest /etc/apparmor.d/nivaroos-ds-browser
	fi
}

ensure_ubol() {
	if [ -f "$SHARE_DIR/ubol/manifest.json" ] && grep -q "\"version\": \"${UBOL_VERSION}\"" "$SHARE_DIR/ubol/manifest.json"; then
		return 0
	fi
	local tmp
	tmp="$(mktemp -d)"
	if curl -fL --connect-timeout 15 --max-time 300 -o "$tmp/ubol.zip" "$UBOL_URL" \
		&& [ "$(sha256sum "$tmp/ubol.zip" | cut -d' ' -f1)" = "$UBOL_SHA256" ]; then
		command -v unzip >/dev/null 2>&1 || apt_install unzip >/dev/null 2>&1 || dnf install -y unzip >/dev/null 2>&1 || pacman -S --noconfirm unzip >/dev/null 2>&1 || true
		mkdir -p "$tmp/ubol" && unzip -q "$tmp/ubol.zip" -d "$tmp/ubol" && {
			mkdir -p "$SHARE_DIR"
			rm -rf "$SHARE_DIR/ubol.new"
			mv "$tmp/ubol" "$SHARE_DIR/ubol.new"
			rm -rf "$SHARE_DIR/ubol"
			mv "$SHARE_DIR/ubol.new" "$SHARE_DIR/ubol"
			chmod -R u=rwX,go=rX "$SHARE_DIR"
			manifest "$SHARE_DIR"
			say "uBlock Origin Lite ${UBOL_VERSION} installed"
		}
	else
		say "could not download uBlock Origin Lite - the built-in filter engine still blocks ads"
	fi
	rm -rf "$tmp"
}

# The script and its units, kept where nivaroos-ds-browser-install.service
# (Download Station's "Install full browser" button) can run them again -
# installed even when Chromium could not be, since that is when the button
# is needed.
install_helper() {
	local u
	mkdir -p "$SHARE_DIR/src/services/download-sidecar/build/scripts" "$SHARE_DIR/src/services/download-sidecar/build/sysroot/usr/lib/systemd/system"
	if [ "$(readlink -f "$SRC_DIR")" != "$(readlink -f "$SHARE_DIR/src")" ]; then
		cp -f "$SRC_DIR/services/download-sidecar/build/scripts/install-ds-browser.sh" "$SHARE_DIR/src/services/download-sidecar/build/scripts/"
		for u in nivaroos-ds-browser.socket nivaroos-ds-browser.service nivaroos-ds-browser-install.service; do
			cp -f "$SYSROOT/usr/lib/systemd/system/$u" "$SHARE_DIR/src/services/download-sidecar/build/sysroot/usr/lib/systemd/system/"
		done
	fi
	chmod 755 "$SHARE_DIR/src/services/download-sidecar/build/scripts/install-ds-browser.sh"
	cp -f "$SYSROOT/usr/lib/systemd/system/nivaroos-ds-browser-install.service" /usr/lib/systemd/system/
	manifest /usr/lib/systemd/system/nivaroos-ds-browser-install.service
	manifest "$SHARE_DIR"
	systemctl daemon-reload >/dev/null 2>&1 || true
}

install_units() {
	local u
	for u in nivaroos-ds-browser.socket nivaroos-ds-browser.service; do
		cp -f "$SYSROOT/usr/lib/systemd/system/$u" "/usr/lib/systemd/system/$u"
		manifest "/usr/lib/systemd/system/$u"
	done

	# Memory cap: 15 % of RAM, at least 768 MB, at most 2 GB.
	local mem cap high
	mem="$(mem_mb)"
	cap=$((mem * 15 / 100))
	[ "$cap" -lt 768 ] && cap=768
	[ "$cap" -gt 2048 ] && cap=2048
	high=$((cap * 3 / 4))
	mkdir -p /etc/systemd/system/nivaroos-ds-browser.service.d
	cat > /etc/systemd/system/nivaroos-ds-browser.service.d/10-memory.conf <<EOF
# Written by install-ds-browser.sh for this box's ${mem} MB of RAM.
[Service]
MemoryHigh=${high}M
MemoryMax=${cap}M
EOF
	manifest /etc/systemd/system/nivaroos-ds-browser.service.d/10-memory.conf

	systemctl daemon-reload >/dev/null 2>&1 || true
	# A running browser still has the old binary: stop it, and the socket
	# starts the new one on its next use.
	systemctl stop nivaroos-ds-browser.service >/dev/null 2>&1 || true
	systemctl enable --now nivaroos-ds-browser.socket >/dev/null 2>&1 || true
}

main() {
	if [ "$(id -u)" != "0" ]; then
		echo "run as root" >&2
		exit 1
	fi
	local mem
	mem="$(mem_mb)"
	if [ "$mem" -gt 0 ] && [ "$mem" -lt 1900 ]; then
		say "this box has ${mem} MB of RAM - skipping the full browser (Download Station uses its Lite browser)"
		exit 0
	fi
	command -v curl >/dev/null 2>&1 || apt_install curl >/dev/null 2>&1 || true
	install_helper
	ensure_chrome || exit 0
	install_fonts
	ensure_user || exit 0
	ensure_userns
	ensure_ubol
	install_units
	say "ready"
}

main
