#!/usr/bin/env bash
# Points /opt/nivaroos/qbittorrent/qbittorrent-nox - the path
# nivaroos-torrent.service and the sidecar use - at a qBittorrent-nox:
#
#   (default) the official static build (userdocs/qbittorrent-nox-static):
#     the latest release is looked up on every run (the pins below are the
#     fallback, never a downgrade) and its SHA-256 checked against GitHub's
#     digest for the asset, or the pin. Distributions lag (Debian 13: 5.1,
#     before 5.2.1's SSRF fix).
#   --distro: the distribution's package (install.sh installs it first).
#
# Before the version changes it backs up the profile (settings and the
# torrent list) to /var/lib/nivaroos/torrent.bak, and a running engine is
# restarted on the new binary. Exits non-zero when nothing usable is in
# place, so install.sh can fall back to the package.
#
# Usage: install-qbittorrent.sh [--distro] [--manifest <file>]
set -u
export PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:${PATH:-}"
umask 022

MODE=static
MANIFEST_FILE="${MANIFEST_FILE:-/var/lib/nivaroos/manifest}"
while [ $# -gt 0 ]; do
	case "$1" in
		--distro) MODE=distro ;;
		--manifest) MANIFEST_FILE="$2"; shift ;;
	esac
	shift
done

QBT_VERSION="5.2.4"
QBT_TAG="release-5.2.4_v2.0.15"
API_URL="https://api.github.com/repos/userdocs/qbittorrent-nox-static/releases/latest"
DIR="${QBT_DIR:-/opt/nivaroos/qbittorrent}"
LINK="$DIR/qbittorrent-nox"
PROFILE="${QBT_PROFILE:-/var/lib/nivaroos/torrent}"
BACKUPS="${QBT_BACKUPS:-/var/lib/nivaroos/torrent.bak}"
SYSTEM_BIN="${QBT_SYSTEM_BIN:-/usr/bin/qbittorrent-nox}"
UNIT="${QBT_UNIT:-nivaroos-torrent.service}"

say() { echo "qbittorrent: $*"; }
manifest() { mkdir -p "$(dirname "$MANIFEST_FILE")"; grep -qxF "$1" "$MANIFEST_FILE" 2>/dev/null || echo "$1" >> "$MANIFEST_FILE"; }
newer() { [ "$1" != "$2" ] && [ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | tail -1)" = "$1" ]; }
qbt_version() { "$1" --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1; }

# The release asset for this CPU, and the pinned digest ("" = no pin).
arch_asset() {
	case "$(uname -m)" in
		x86_64) echo "x86_64 14d29323f1c12c1e8892ac69496f8b51ddebb74b4b590fc13e343aa213fecaf4" ;;
		aarch64 | arm64) echo "aarch64 f12e821d5782c39e3093d788689182e9ac823fa01b3a82d6c00fdb993e4428d8" ;;
		armv7l) echo "armv7 6bbd4d8f7811ca98ca8e22f7e98e8b87423958d2f751d9f87c18005c94ebf710" ;;
		riscv64) echo "riscv64 9e209c501ce603a5f41d6b4cedec1df3d46c72559badf02f2390a4c2cfc7d2c0" ;;
		*) return 1 ;;
	esac
}

# "tag sha256" of the latest release's asset $1 (GitHub API JSON on stdin).
latest_from_json() {
	awk -F'"' -v want="$1-qbittorrent-nox" '
		$2 == "tag_name" { tag = $4 }
		$2 == "name" { hit = ($4 == want); next }
		hit && $2 == "digest" { sub(/^sha256:/, "", $4); dig = $4; hit = 0 }
		END { if (tag ~ /^release-[0-9.]+_v[0-9.]+$/ && dig ~ /^[0-9a-f]{64}$/) print tag, dig }'
}

# Swaps LINK to $1, backing up the profile first when the version changes.
point() {
	local target="$1" old new was_active=""
	old="$(qbt_version "$LINK")"
	# Before this link existed the unit ran the package's binary.
	[ -n "$old" ] || old="$(qbt_version "$SYSTEM_BIN")"
	new="$(qbt_version "$target")"
	if [ "$(readlink "$LINK" 2>/dev/null)" = "$target" ]; then
		return 0
	fi
	if systemctl is-active --quiet "$UNIT" 2>/dev/null; then
		was_active=1
		systemctl stop "$UNIT" >/dev/null 2>&1 || true
	fi
	if [ "$old" != "$new" ] && [ -d "$PROFILE/qBittorrent" ]; then
		mkdir -p "$BACKUPS" && chmod 700 "$BACKUPS"
		if tar -C "$PROFILE" --exclude=qBittorrent/cache -czf "$BACKUPS/profile-${old:-unknown}-$(date +%Y%m%d%H%M%S).tar.gz" qBittorrent; then
			# Keep the three newest.
			find "$BACKUPS" -maxdepth 1 -name 'profile-*.tar.gz' -printf '%T@ %p\n' | sort -rn | tail -n +4 | cut -d' ' -f2- | xargs -r rm -f
		else
			say "could not back up the profile - keeping ${old:-the current one}"
			[ -n "$was_active" ] && systemctl start "$UNIT" >/dev/null 2>&1
			return 1
		fi
	fi
	mkdir -p "$DIR"
	ln -sfn "$target" "$LINK.new" && mv -Tf "$LINK.new" "$LINK" || return 1
	manifest "$DIR"
	say "qBittorrent-nox ${old:-(none)} -> ${new} ($MODE)"
	if [ -n "$was_active" ]; then
		systemctl start "$UNIT" >/dev/null 2>&1 || say "the torrent engine did not restart (journalctl -u $UNIT)"
	fi
	# Static builds no longer pointed at.
	local f
	for f in "$DIR"/qbittorrent-nox-*; do
		[ -e "$f" ] && [ "$f" != "$(readlink "$LINK")" ] && rm -f "$f"
	done
	return 0
}

install_static() {
	local asset pin line tag="$QBT_TAG" ver="$QBT_VERSION" want got tmp
	line="$(arch_asset)" || { say "no static build for $(uname -m)"; return 1; }
	read -r asset pin <<<"$line"
	line="$(curl -fsSL --connect-timeout 15 --max-time 60 -H 'Accept: application/vnd.github+json' "$API_URL" 2>/dev/null | latest_from_json "$asset")"
	want="$pin"
	if [ -n "$line" ]; then
		local v="${line%% *}"; v="${v#release-}"; v="${v%%_*}"
		if newer "$v" "$ver"; then tag="${line%% *}"; ver="$v"; want="${line#* }"; fi
	else
		say "could not read the latest release - using the pinned ${ver}"
	fi
	if [ -x "$DIR/qbittorrent-nox-$ver" ] && [ "$(qbt_version "$DIR/qbittorrent-nox-$ver")" = "$ver" ]; then
		point "$DIR/qbittorrent-nox-$ver"
		return
	fi
	mkdir -p "$DIR"
	tmp="$(mktemp "$DIR/.dl.XXXXXX")"
	if ! curl -fsSL --connect-timeout 15 --max-time 900 -o "$tmp" "https://github.com/userdocs/qbittorrent-nox-static/releases/download/${tag}/${asset}-qbittorrent-nox"; then
		rm -f "$tmp"; say "could not download qBittorrent-nox ${ver}"
		[ -x "$LINK" ]; return
	fi
	got="$(sha256sum "$tmp" | cut -d' ' -f1)"
	if [ -z "$want" ] || [ "$got" != "$want" ]; then
		rm -f "$tmp"; say "qBittorrent-nox ${ver} failed its checksum - not using it"
		[ -x "$LINK" ]; return
	fi
	chmod 755 "$tmp"
	if [ "$(qbt_version "$tmp")" != "$ver" ]; then
		rm -f "$tmp"; say "qBittorrent-nox ${ver} does not run on this box - not using it"
		[ -x "$LINK" ]; return
	fi
	mv -f "$tmp" "$DIR/qbittorrent-nox-$ver"
	point "$DIR/qbittorrent-nox-$ver"
}

main() {
	if [ "$MODE" = distro ]; then
		[ -x "$SYSTEM_BIN" ] || { say "the qbittorrent-nox package is not installed"; return 1; }
		point "$SYSTEM_BIN"
	else
		install_static
	fi
}

# Sourced by installer/tests/qbittorrent-test.sh for its functions.
[ "${BASH_SOURCE[0]}" = "$0" ] && main
