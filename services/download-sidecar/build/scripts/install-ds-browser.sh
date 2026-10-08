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
# --update is what nivaroos-ds-browser-update.timer runs weekly: it brings
# Chrome for Testing and uBO Lite to their latest releases and stops
# nothing - a running browser keeps its version, its next start (the
# socket starts it on demand) uses the new one. --rollback points the
# browser back at the previous Chrome for Testing kept beside it.
#
# Usage: install-ds-browser.sh [--src <NivaroOS checkout>] [--manifest <file>] [--update|--rollback]
set -u
export PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:${PATH:-}"
umask 022

SRC_DIR=""
MODE=install
MANIFEST_FILE="${MANIFEST_FILE:-/var/lib/nivaroos/manifest}"
while [ $# -gt 0 ]; do
	case "$1" in
		--src) SRC_DIR="$2"; shift ;;
		--manifest) MANIFEST_FILE="$2"; shift ;;
		--update) MODE=update ;;
		--rollback) MODE=rollback ;;
	esac
	shift
done
if [ -z "$SRC_DIR" ]; then
	SRC_DIR="$(cd "$(dirname "$0")/../../../.." && pwd)"
fi
SYSROOT="${SRC_DIR}/services/download-sidecar/build/sysroot"

# The latest releases are looked up on every run (resolve_versions); these
# pins are the fallback when that fails, never a downgrade.
UBOL_VERSION="2026.926.2202"
UBOL_SHA256="9a0d94e832fde9430f64817ff1ba3f34040f19caa113a24e6d84aad1d05eb1aa"
CFT_VERSION="154.0.8037.57"
CFT_SHA256="ceee2972074d441ea7c4ba8bcc0eaab77e7e87680f6653d73d3065851fe10302"
CFT_JSON_URL="https://googlechromelabs.github.io/chrome-for-testing/last-known-good-versions-with-downloads.json"
UBOL_API_URL="https://api.github.com/repos/uBlockOrigin/uBOL-home/releases/latest"

STATE_DIR=/var/lib/nivaroos/ds-browser
SHARE_DIR=/usr/share/nivaroos/ds-browser
# One folder per Chrome for Testing version; CFT_DIR is a symlink to the
# current one, swapped atomically, with the previous one kept for rollback.
CFT_ROOT=/opt/nivaroos/chromium.d
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

chrome_version() { "$1" --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+' | head -1; }
chrome_major() { chrome_version "$1" | cut -d. -f1; }

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

# newer A B: A is a later version than B.
newer() { [ "$1" != "$2" ] && [ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | tail -1)" = "$1" ]; }

valid_version() { printf '%s' "$1" | grep -qxE '[0-9]+(\.[0-9]+){1,3}'; }

# Stable's version from Chrome for Testing's last-known-good JSON (stdin).
# No jq/python needed: "version" comes before any nested object in a channel.
cft_stable_from_json() {
	tr -d ' \t\r\n' | grep -oE '"Stable":\{[^}]*"version":"[0-9.]+"' | grep -oE '[0-9]+(\.[0-9]+){3}' | head -1
}

# "<version> <sha256>" of the Chromium build from GitHub's latest-release
# JSON for uBO Lite (stdin, as the API prints it: one key per line).
ubol_latest_from_json() {
	awk -F'"' '
		$2 == "tag_name" { tag = $4 }
		$2 == "name" && $4 ~ /^uBOLite_.*\.chromium\.zip$/ { want = 1; next }
		want && $2 == "digest" { sub(/^sha256:/, "", $4); dig = $4; want = 0 }
		END { if (tag != "" && dig ~ /^[0-9a-f]{64}$/ && tag ~ /^[0-9.]+$/) print tag, dig }'
}

# Picks the latest Chrome for Testing Stable and uBO Lite. Anything missing
# or odd keeps the pins; a JSON-resolved Chrome is verified against the
# storage's own MD5 (CFT_SHA256 empty), a pinned one against CFT_SHA256.
resolve_versions() {
	local v line
	v="$(curl -fsSL --connect-timeout 15 --max-time 60 "$CFT_JSON_URL" 2>/dev/null | cft_stable_from_json)"
	if valid_version "$v" && newer "$v" "$CFT_VERSION"; then
		CFT_VERSION="$v"; CFT_SHA256=""
	elif [ "$v" != "$CFT_VERSION" ]; then
		say "could not read the latest Chrome for Testing - using the pinned ${CFT_VERSION}"
	fi
	line="$(curl -fsSL --connect-timeout 15 --max-time 60 -H 'Accept: application/vnd.github+json' "$UBOL_API_URL" 2>/dev/null | ubol_latest_from_json)"
	if [ -n "$line" ] && newer "${line% *}" "$UBOL_VERSION"; then
		UBOL_VERSION="${line% *}"; UBOL_SHA256="${line#* }"
	elif [ "${line% *}" != "$UBOL_VERSION" ]; then
		say "could not read the latest uBlock Origin Lite - using the pinned ${UBOL_VERSION}"
	fi
}

# The Chrome for Testing version CFT_DIR points at ("" if none).
cft_current() {
	[ -x "$CFT_DIR/chrome" ] || return 0
	if [ -L "$CFT_DIR" ]; then basename "$(readlink "$CFT_DIR")"; else chrome_version "$CFT_DIR/chrome"; fi
}

# Points CFT_DIR at $CFT_ROOT/$1 in one rename.
cft_point() {
	ln -sfn "$CFT_ROOT/$1" "${CFT_DIR}.lnk" && mv -Tf "${CFT_DIR}.lnk" "$CFT_DIR"
}

cft_libs() {
	# The shared libraries it links against.
	if command -v dnf >/dev/null 2>&1; then
		dnf install -y nss atk at-spi2-atk cups-libs libdrm libxkbcommon mesa-libgbm alsa-lib pango libXcomposite libXdamage libXrandr >/dev/null 2>&1 || true
	elif command -v zypper >/dev/null 2>&1; then
		zypper -n in mozilla-nss libatk-1_0-0 libatk-bridge-2_0-0 cups-libs libdrm2 libxkbcommon0 libgbm1 libasound2 libpango-1_0-0 >/dev/null 2>&1 || true
	elif command -v apt-get >/dev/null 2>&1; then
		apt_install libnss3 libatk1.0-0 libatk-bridge2.0-0 libcups2 libdrm2 libxkbcommon0 libgbm1 libasound2 libpango-1.0-0 libxcomposite1 libxdamage1 libxrandr2 >/dev/null 2>&1 || true
	fi
}

# Installs CFT_VERSION beside the current one and switches to it. A running
# browser is not touched: Chromium finds its files through /proc/self/exe,
# i.e. its own version's folder, which is kept.
install_chrome_for_testing() {
	[ "$(uname -m)" = "x86_64" ] || return 1
	local cur tmp url want got
	cur="$(cft_current)"
	if [ -n "$cur" ] && ! newer "$CFT_VERSION" "$cur"; then
		say "Chrome for Testing ${cur} is current"
		return 0
	fi
	if [ "$(cat "$CFT_ROOT/.rolled-back" 2>/dev/null)" = "$CFT_VERSION" ]; then
		say "Chrome for Testing ${CFT_VERSION} was rolled back - waiting for a newer one"
		return 0
	fi
	url="https://storage.googleapis.com/chrome-for-testing-public/${CFT_VERSION}/linux64/chrome-linux64.zip"
	mkdir -p "$CFT_ROOT"
	tmp="$(mktemp -d "$CFT_ROOT/.new.XXXXXX")"
	say "downloading Chrome for Testing ${CFT_VERSION}..."
	if ! curl -fsSL --connect-timeout 15 --max-time 900 -D "$tmp/headers" -o "$tmp/cft.zip" "$url"; then
		rm -rf "$tmp"; return 1
	fi
	if [ -n "$CFT_SHA256" ]; then
		want="$CFT_SHA256"; got="$(sha256sum "$tmp/cft.zip" | cut -d' ' -f1)"
	else
		# Google's storage publishes each object's MD5.
		want="$(tr -d '\r' < "$tmp/headers" | sed -n 's/^x-goog-hash: *md5=//Ip' | tail -1 | base64 -d 2>/dev/null | od -An -tx1 | tr -d ' \n')"
		got="$(md5sum "$tmp/cft.zip" | cut -d' ' -f1)"
	fi
	if [ -z "$want" ] || [ "$want" != "$got" ]; then
		say "Chrome for Testing ${CFT_VERSION} failed its checksum - not using it"
		rm -rf "$tmp"; return 1
	fi
	command -v unzip >/dev/null 2>&1 || { apt_install unzip >/dev/null 2>&1 || dnf install -y unzip >/dev/null 2>&1 || pacman -S --noconfirm unzip >/dev/null 2>&1 || zypper -n in unzip >/dev/null 2>&1; }
	unzip -q "$tmp/cft.zip" -d "$tmp" || { rm -rf "$tmp"; return 1; }
	cft_libs
	# It must start (all its libraries load) and be the version asked for.
	if [ "$(chrome_version "$tmp/chrome-linux64/chrome")" != "$CFT_VERSION" ]; then
		say "Chrome for Testing ${CFT_VERSION} does not run on this box - not using it"
		rm -rf "$tmp"; return 1
	fi
	chmod -R u=rwX,go=rX "$tmp/chrome-linux64"
	rm -rf "${CFT_ROOT:?}/${CFT_VERSION}"
	mv "$tmp/chrome-linux64" "$CFT_ROOT/$CFT_VERSION"
	rm -rf "$tmp"
	# Before versioned folders it was a plain folder: keep it as the previous one.
	if [ -d "$CFT_DIR" ] && [ ! -L "$CFT_DIR" ]; then
		if [ -n "$cur" ] && [ ! -e "$CFT_ROOT/$cur" ]; then mv "$CFT_DIR" "$CFT_ROOT/$cur"; else rm -rf "$CFT_DIR"; fi
	fi
	cft_point "$CFT_VERSION" || return 1
	say "Chrome for Testing ${cur:-(none)} -> ${CFT_VERSION}"
	# Keep the new and the previous version, and any one a browser still runs.
	local d in_use
	in_use="$(for p in /proc/[0-9]*/exe; do readlink "$p" 2>/dev/null; done)"
	for d in "$CFT_ROOT"/*/; do
		d="${d%/}"
		case "$(basename "$d")" in "$CFT_VERSION" | "$cur") continue ;; esac
		printf '%s\n' "$in_use" | grep -q "^$d/" && continue
		rm -rf "$d"
	done
	manifest "$CFT_ROOT"
	manifest "$CFT_DIR"
}

# --rollback: back to the other kept version.
rollback_chrome_for_testing() {
	local cur d prev=""
	cur="$(cft_current)"
	for d in "$CFT_ROOT"/*/; do
		d="$(basename "$d")"
		[ "$d" != "$cur" ] && [ -x "$CFT_ROOT/$d/chrome" ] && prev="$d"
	done
	if [ -z "$prev" ]; then
		say "no previous Chrome for Testing to roll back to"
		return 1
	fi
	cft_point "$prev" || return 1
	# The weekly update skips this version, and moves on with the next one.
	echo "$cur" > "$CFT_ROOT/.rolled-back"
	say "Chrome for Testing ${cur} -> ${prev} (rolled back; the next browser start uses it)"
}

ensure_chrome() {
	local c
	# On x86_64 the browser is NivaroOS's own Chrome for Testing, which
	# nivaroos-ds-browser-update.timer keeps current; a distro or Google
	# package (updated by the package manager) is the fallback.
	install_chrome_for_testing || true
	if c="$(find_chrome)"; then
		say "using $c ($("$c" --version 2>/dev/null))"
		return 0
	fi
	say "no usable Chrome/Chromium found - installing one"
	case "$(os_family)" in
		debian) apt-get update >/dev/null 2>&1; apt_install chromium && record "package:chromium" ;;
		ubuntu) install_google_chrome_repo || { [ "$(dpkg --print-architecture 2>/dev/null)" = "amd64" ] || say "Ubuntu on $(dpkg --print-architecture 2>/dev/null) has no Chromium outside the snap (which the service can't run) and Google builds Chrome for amd64 only"; } ;;
		fedora) dnf install -y chromium && record "package:chromium" ;;
		arch) pacman -S --noconfirm --needed chromium && record "package:chromium" ;;
		suse) zypper -n in chromium && record "package:chromium" ;;
	esac
	[ "$(uname -m)" = "x86_64" ] || say "Chrome for Testing is x86_64 only (this box is $(uname -m))"
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
	[ -d "$STATE_DIR/staging" ] || STAGING_NEW=1
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
			# Every kept version, so an update needs no new profile.
			"$CFT_ROOT"/*) real="$CFT_ROOT/*/chrome" ;;
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

# The browser host copies it into its own state when its manifest changes
# (rb_host.go ubolCopy), so a running browser is unaffected by the swap.
ensure_ubol() {
	local have
	have="$(sed -n 's/^ *"version": *"\([0-9.]*\)".*/\1/p' "$SHARE_DIR/ubol/manifest.json" 2>/dev/null | head -1)"
	if [ -n "$have" ] && ! newer "$UBOL_VERSION" "$have"; then
		return 0
	fi
	local tmp
	tmp="$(mktemp -d)"
	if curl -fsSL --connect-timeout 15 --max-time 300 -o "$tmp/ubol.zip" "https://github.com/uBlockOrigin/uBOL-home/releases/download/${UBOL_VERSION}/uBOLite_${UBOL_VERSION}.chromium.zip" \
		&& [ "$(sha256sum "$tmp/ubol.zip" | cut -d' ' -f1)" = "$UBOL_SHA256" ]; then
		command -v unzip >/dev/null 2>&1 || apt_install unzip >/dev/null 2>&1 || dnf install -y unzip >/dev/null 2>&1 || pacman -S --noconfirm unzip >/dev/null 2>&1 || true
		mkdir -p "$tmp/ubol" && unzip -q "$tmp/ubol.zip" -d "$tmp/ubol" && grep -q "\"version\": *\"${UBOL_VERSION}\"" "$tmp/ubol/manifest.json" && {
			mkdir -p "$SHARE_DIR"
			rm -rf "$SHARE_DIR/ubol.new" "$SHARE_DIR/ubol.prev"
			mv "$tmp/ubol" "$SHARE_DIR/ubol.new"
			chmod -R u=rwX,go=rX "$SHARE_DIR/ubol.new"
			[ -d "$SHARE_DIR/ubol" ] && mv "$SHARE_DIR/ubol" "$SHARE_DIR/ubol.prev"
			mv "$SHARE_DIR/ubol.new" "$SHARE_DIR/ubol"
			manifest "$SHARE_DIR"
			say "uBlock Origin Lite ${have:-(none)} -> ${UBOL_VERSION}"
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
		for u in nivaroos-ds-browser.socket nivaroos-ds-browser.service nivaroos-ds-browser-install.service nivaroos-ds-browser-update.service nivaroos-ds-browser-update.timer; do
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
	for u in nivaroos-ds-browser.socket nivaroos-ds-browser.service nivaroos-ds-browser-update.service nivaroos-ds-browser-update.timer; do
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
	systemctl enable --now nivaroos-ds-browser.socket nivaroos-ds-browser-update.timer >/dev/null 2>&1 || true
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
	# One run at a time (the timer, the installer, the Install button).
	exec 9>/run/nivaroos-ds-browser-install.lock
	flock 9
	case "$MODE" in
		rollback)
			rollback_chrome_for_testing
			exit $? ;;
		update)
			if ! id nivaroos-browser >/dev/null 2>&1; then
				say "the full browser is not installed - nothing to update"
				exit 0
			fi
			resolve_versions
			install_chrome_for_testing || say "Chrome for Testing was not updated - the current version stays"
			ensure_userns
			ensure_ubol
			say "up to date"
			exit 0 ;;
	esac
	resolve_versions
	install_helper
	ensure_chrome || exit 0
	install_fonts
	ensure_user || exit 0
	ensure_userns
	ensure_ubol
	install_units
	# Download Station's unit only makes the staging folder writable if it
	# existed when the service started: restart it once when it is new (it
	# is already running when "Install full browser" was pressed).
	if [ "${STAGING_NEW:-0}" = 1 ]; then
		systemctl try-restart nivaroos-download-sidecar.service >/dev/null 2>&1 || true
	fi
	say "ready"
}

# Sourced by installer/tests/ds-browser-update-test.sh for its functions.
[ "${BASH_SOURCE[0]}" = "$0" ] && main
