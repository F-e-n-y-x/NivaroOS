#!/usr/bin/env bash
# ==============================================================================
#  Sandboxed units (security audit 2026-10, M3): each hardened service's own
#  Go tests must pass inside that unit's sandbox, and the unit must score
#  "OK" in systemd-analyze security. Run as root from a checkout:
#
#    installer/tests/unit-sandbox-test.sh
#
#  It never touches the running services: the tests run in transient units
#  (systemd-run) with the sandbox lines of the unit files in this checkout,
#  on temp dirs. Needs go (GOWORK=off) and systemd.
#  Exit status: 0 all passed, 1 a check failed, 2 missing prerequisite.
# ==============================================================================
set -u
cd "$(dirname "$0")/../../services" || exit 2
[ "$(id -u)" = 0 ] || { echo "run as root"; exit 2; }
export PATH=/usr/local/go/bin:$PATH GOWORK=off GOTOOLCHAIN=local

# The test binaries can't live in /tmp or /var/tmp: PrivateTmp hides them
# from the unit. Next to the sources instead.
bins=$(mktemp -d "$PWD/.sandbox-test.XXXXXX") || exit 2
trap 'rm -rf "$bins"' EXIT

fail=0
check() { # service unit package
	local svc=$1 unit=$2 pkg=$3 file args=()
	file=$svc/build/sysroot/usr/lib/systemd/system/$unit.service
	while IFS= read -r l; do
		case "$l" in '' | '#'* | '['* | Exec* | PIDFile* | Restart* | Type*) continue ;; esac
		args+=(-p "$l")
	done < <(sed -n '/^\[Service\]/,/^\[Install\]/p' "$file")
	local t="$bins/$unit-${pkg//\//-}.test"
	(cd "$svc" && go test -c -o "$t" "./$pkg") || { echo "FAIL $unit: build $pkg"; fail=1; return; }
	if systemd-run --quiet --wait --pipe --collect -p WorkingDirectory="$PWD/$svc/$pkg" "${args[@]}" "$t" >"$t.log" 2>&1; then
		echo "PASS $unit: $pkg tests inside the sandbox"
	else
		echo "FAIL $unit: $pkg tests inside the sandbox"; tail -20 "$t.log"; fail=1
	fi
	local score
	score=$(systemd-analyze security --offline=true "$file" 2>/dev/null | tail -1 | grep -o '[0-9]*\.[0-9]' | head -1)
	if awk -v s="${score:-10}" 'BEGIN { exit !(s <= 3.0) }'; then
		echo "PASS $unit: exposure $score"
	else
		echo "FAIL $unit: exposure ${score:-?} (want <= 3.0)"; fail=1
	fi
}

check gateway nivaroos-gateway route
check gateway nivaroos-gateway service
check user nivaroos-user-service route
check user nivaroos-user-service route/v1
check message-bus nivaroos-message-bus repository
check message-bus nivaroos-message-bus route
exit $fail
