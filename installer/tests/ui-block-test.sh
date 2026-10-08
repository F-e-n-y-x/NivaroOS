#!/usr/bin/env bash
# The nivaroos-ui block (colours, glyphs, banner, message helpers) is copied
# into install.sh, uninstall.sh and nivaroos-safety-lib.sh, because the
# installer must work as one downloaded file. Fails when the copies drift,
# or when colour leaks into non-terminal output or NO_COLOR.
set -u
d="$(cd "$(dirname "$0")/.." && pwd)"
block() { sed -n '/^# >>> nivaroos-ui/,/^# <<< nivaroos-ui/p' "$1"; }
ref="$(block "$d/nivaroos-safety-lib.sh")"
[ -n "$ref" ] || { echo "no nivaroos-ui block in nivaroos-safety-lib.sh" >&2; exit 1; }
rc=0
for f in install.sh uninstall.sh; do
	if [ "$(block "$d/$f")" != "$ref" ]; then
		echo "$f: nivaroos-ui block differs from nivaroos-safety-lib.sh's:" >&2
		diff <(printf '%s\n' "$ref") <(block "$d/$f") >&2
		rc=1
	fi
done
out="$(eval "$ref"; ui_banner t s; ui_ok a; ui_err b 2>&1)"
case "$out" in *$'\033'*) echo "colour written to a non-terminal" >&2; rc=1 ;; esac
if command -v script >/dev/null 2>&1; then
	out="$(NO_COLOR=1 TERM=xterm script -qec "bash -c '$(printf '%s\n' "$ref" | sed "s/'/'\\\\''/g"); ui_ok a'" /dev/null)"
	case "$out" in *$'\033'*) echo "colour written despite NO_COLOR" >&2; rc=1 ;; esac
fi
[ $rc -eq 0 ] && echo "ok: nivaroos-ui block identical in all copies, no colour off-terminal"
exit $rc
