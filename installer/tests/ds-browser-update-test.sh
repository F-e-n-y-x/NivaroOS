#!/usr/bin/env bash
# install-ds-browser.sh: reading the latest Chrome for Testing / uBO Lite
# versions, and falling back to the pins. No network, no root.
#   bash installer/tests/ds-browser-update-test.sh
set -u
# shellcheck disable=SC2317 # curl() stubs are called by the sourced script
here="$(cd "$(dirname "$0")" && pwd)"
# shellcheck source-path=SCRIPTDIR source=../../services/download-sidecar/build/scripts/install-ds-browser.sh
. "$here/../../services/download-sidecar/build/scripts/install-ds-browser.sh"
fail=0
check() { if [ "$2" = "$3" ]; then echo "ok   $1"; else echo "FAIL $1: got '$2', want '$3'"; fail=1; fi; }

# Trimmed from the real last-known-good-versions-with-downloads.json.
CFT_FIXTURE='{"timestamp":"2026-10-07T21:20:20.783Z","channels":{"Stable":{"channel":"Stable","version":"155.0.8059.39","revision":"1560000","downloads":{"chrome":[{"platform":"linux64","url":"https://storage.googleapis.com/chrome-for-testing-public/155.0.8059.39/linux64/chrome-linux64.zip"}]}},"Beta":{"channel":"Beta","version":"156.0.8100.2","revision":"1570000","downloads":{}}}}'
UBOL_FIXTURE='{
  "tag_name": "2026.1006.1931",
  "assets": [
    {
      "name": "uBOLite_2026.1006.1931.edge.zip",
      "digest": "sha256:dd059665fd1d3195f39ade95d30029b7b2582283365b137f83b2fa18cf9250ee"
    },
    {
      "name": "uBOLite_2026.1006.1931.chromium.zip",
      "uploader": {
        "login": "github-actions[bot]"
      },
      "digest": "sha256:1670f92590f5ad5b20f02d0a75e144572567b4ba979b3dc3204c41f651206fe7"
    }
  ]
}'

check "cft: compact JSON" "$(printf '%s' "$CFT_FIXTURE" | cft_stable_from_json)" 155.0.8059.39
check "cft: pretty JSON" "$(printf '%s' "$CFT_FIXTURE" | python3 -m json.tool | cft_stable_from_json)" 155.0.8059.39
check "cft: garbage" "$(echo '<html>rate limited</html>' | cft_stable_from_json)" ""
check "ubol: chromium asset" "$(printf '%s' "$UBOL_FIXTURE" | ubol_latest_from_json)" "2026.1006.1931 1670f92590f5ad5b20f02d0a75e144572567b4ba979b3dc3204c41f651206fe7"
check "ubol: no digest" "$(printf '%s' "$UBOL_FIXTURE" | grep -v digest | ubol_latest_from_json)" ""
check "newer" "$(newer 155.0.8059.39 154.0.8037.57 && echo y)" y
check "not newer (same)" "$(newer 154.0.8037.57 154.0.8037.57 || echo n)" n
check "not newer (older, 10 vs 9)" "$(newer 154.0.8037.9 154.0.8037.10 || echo n)" n

PIN_CFT="$CFT_VERSION" PIN_SHA="$CFT_SHA256" PIN_UBOL="$UBOL_VERSION"
reset() { CFT_VERSION="$PIN_CFT" CFT_SHA256="$PIN_SHA" UBOL_VERSION="$PIN_UBOL"; }

# Network up: both resolve, and the JSON-resolved Chrome has no SHA (MD5 check).
curl() { case "$*" in *chrome-for-testing*) printf '%s' "$CFT_FIXTURE" ;; *api.github.com*) printf '%s' "$UBOL_FIXTURE" ;; esac; }
reset; resolve_versions >/dev/null
check "resolve: cft" "$CFT_VERSION/$CFT_SHA256" "155.0.8059.39/"
check "resolve: ubol" "$UBOL_VERSION" 2026.1006.1931

# Network down: the pins, with their SHA-256.
curl() { return 7; }
reset; out="$(resolve_versions)"; resolve_versions >/dev/null
check "offline: cft pin" "$CFT_VERSION/$CFT_SHA256" "$PIN_CFT/$PIN_SHA"
check "offline: ubol pin" "$UBOL_VERSION" "$PIN_UBOL"
check "offline: says so" "$(printf '%s\n' "$out" | grep -c pinned)" 2

# A JSON older than the pin (or a bogus version) never downgrades.
curl() { case "$*" in *chrome-for-testing*) echo '{"channels":{"Stable":{"channel":"Stable","version":"120.0.1.1"}}}' ;; *) echo '{"tag_name": "x;rm"}' ;; esac; }
reset; resolve_versions >/dev/null
check "older json: cft pin" "$CFT_VERSION" "$PIN_CFT"
check "bad json: ubol pin" "$UBOL_VERSION" "$PIN_UBOL"

exit $fail
