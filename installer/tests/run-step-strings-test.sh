#!/usr/bin/env bash
# Each run_step "<title>" "<script>" body in install.sh is one double-
# quoted string, expanded by the outer shell before the step runs - even
# the text of quoted heredocs and comments inside it. An unescaped
# backtick there runs a command at that moment, and a bare " ends the
# string early (a backticked nvidia-smi in a comment once pasted its
# output table into nivaroos-gpu-sidecar.service). Fails on either.
set -u
f="${1:-$(dirname "$0")/../install.sh}"
out="$(awk '
	!inrun && /run_step "[^"]*" "$/ { inrun = 1; next }
	inrun && /^[[:space:]]*"[[:space:]]*$/ { inrun = 0; next }
	inrun {
		line = $0
		gsub(/\\\\/, "", line)      # \\ - an escaped backslash
		gsub(/\\[`"$]/, "", line)   # \` \" \$ - escaped, fine
		if (index(line, "`") || index(line, "\"")) print NR ": " $0
	}
' "$f")"
if [ -n "$out" ]; then
	echo "unescaped backtick or double quote inside a run_step string:" >&2
	echo "$out" >&2
	exit 1
fi
echo "ok: run_step strings have no unescaped backticks or double quotes"
