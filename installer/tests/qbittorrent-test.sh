#!/usr/bin/env bash
# install-qbittorrent.sh: reading the latest static release, and the swap
# that backs up the profile first. No network, no root, temp folders only.
#   bash installer/tests/qbittorrent-test.sh
set -u
here="$(cd "$(dirname "$0")" && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
export QBT_DIR="$tmp/opt" QBT_PROFILE="$tmp/profile" QBT_BACKUPS="$tmp/bak" QBT_UNIT=nivaroos-test-absent.service QBT_SYSTEM_BIN="$tmp/usr/qbittorrent-nox" MANIFEST_FILE="$tmp/manifest"
# shellcheck source-path=SCRIPTDIR source=../../services/download-sidecar/build/scripts/install-qbittorrent.sh
. "$here/../../services/download-sidecar/build/scripts/install-qbittorrent.sh"
fail=0
check() { if [ "$2" = "$3" ]; then echo "ok   $1"; else echo "FAIL $1: got '$2', want '$3'"; fail=1; fi; }

FIXTURE='{
  "tag_name": "release-5.2.5_v2.0.15",
  "assets": [
    {
      "name": "aarch64-qbittorrent-nox",
      "digest": "sha256:1111111111111111111111111111111111111111111111111111111111111111"
    },
    {
      "name": "x86_64-qbittorrent-nox",
      "uploader": {
        "login": "github-actions[bot]"
      },
      "digest": "sha256:2222222222222222222222222222222222222222222222222222222222222222"
    },
    {
      "name": "dependency-version.json",
      "digest": "sha256:3333333333333333333333333333333333333333333333333333333333333333"
    }
  ]
}'
check "latest: x86_64" "$(printf '%s' "$FIXTURE" | latest_from_json x86_64)" "release-5.2.5_v2.0.15 2222222222222222222222222222222222222222222222222222222222222222"
check "latest: aarch64" "$(printf '%s' "$FIXTURE" | latest_from_json aarch64)" "release-5.2.5_v2.0.15 1111111111111111111111111111111111111111111111111111111111111111"
check "latest: no digest" "$(printf '%s' "$FIXTURE" | grep -v digest | latest_from_json x86_64)" ""
check "latest: garbage" "$(echo '<html>rate limited</html>' | latest_from_json x86_64)" ""

# Fake binaries that answer --version like qbittorrent-nox does.
fake() { mkdir -p "$(dirname "$1")"; printf '#!/bin/sh\necho "qBittorrent v%s"\n' "$2" > "$1"; chmod 755 "$1"; }
fake "$tmp/usr/qbittorrent-nox" 5.1.0
mkdir -p "$QBT_PROFILE/qBittorrent/config" "$QBT_PROFILE/qBittorrent/data/BT_backup" "$QBT_PROFILE/qBittorrent/cache"
echo 'WebUI\Port=28646' > "$QBT_PROFILE/qBittorrent/config/qBittorrent.conf"
echo x > "$QBT_PROFILE/qBittorrent/data/BT_backup/abc.fastresume"

MODE=distro point "$tmp/usr/qbittorrent-nox" >/dev/null
check "first link to the same version: no backup" "$(qbt_version "$LINK") $(find "$QBT_BACKUPS" -name '*.tar.gz' 2>/dev/null | wc -l)" "5.1.0 0"
fake "$QBT_DIR/qbittorrent-nox-5.2.4" 5.2.4
fake "$QBT_DIR/qbittorrent-nox-5.2.3" 5.2.3
point "$QBT_DIR/qbittorrent-nox-5.2.4" >/dev/null
check "switch: older static build removed" "$([ -e "$QBT_DIR/qbittorrent-nox-5.2.3" ] || echo gone)" gone
check "switch: new version" "$(qbt_version "$LINK")" 5.2.4
b="$(find "$QBT_BACKUPS" -name 'profile-5.1.0-*.tar.gz')"
check "switch: profile backed up" "$(tar -tzf "$b" | grep -c -e 'config/qBittorrent.conf' -e 'abc.fastresume')" 2
check "switch: cache left out" "$(tar -tzf "$b" | grep -c cache)" 0
point "$QBT_DIR/qbittorrent-nox-5.2.4" >/dev/null
check "same target: no new backup" "$(find "$QBT_BACKUPS" -name '*.tar.gz' | wc -l)" 1
check "manifest lists the folder" "$(cat "$MANIFEST_FILE")" "$QBT_DIR"

[ "$fail" = 0 ] && echo "all passed"
exit "$fail"
