#!/usr/bin/env bash
# shellcheck disable=SC2016 # check() evals its single-quoted test
# install.sh's box-sized limits: the Backup & Sync memory drop-in
# (min(3G, 40 % RAM), min(6G, 60 % RAM)) and when the journald cap is
# installed (not over the admin's own SystemMaxUse, not on small disks).
# Run: bash installer/tests/limits-test.sh
set -u
d="$(cd "$(dirname "$0")/.." && pwd)"
PASS=0 FAIL=0
check() { if eval "$1"; then PASS=$((PASS + 1)); else FAIL=$((FAIL + 1)); echo "FAIL: $2" >&2; fi; }

# shellcheck disable=SC1090
. <(sed '$d' "$d/install.sh")
set +eE
trap - ERR INT TERM EXIT

MB=0
mem_mb() { echo "$MB"; }
MB=2048 out="$(backup_memory_dropin)"
check 'grep -qx "MemoryHigh=819M" <<<"$out" && grep -qx "MemoryMax=1228M" <<<"$out"' "2 GB box: 40 %/60 % of RAM ($out)"
MB=32768 out="$(backup_memory_dropin)"
check 'grep -qx "MemoryHigh=3072M" <<<"$out" && grep -qx "MemoryMax=6144M" <<<"$out"' "32 GB box: the fixed 3G/6G"
MB=0 out="$(backup_memory_dropin)"
check '[ -z "$out" ]' "unknown RAM: no drop-in"

NV_ROOT="$(mktemp -d)"
trap 'rm -rf "$NV_ROOT"' EXIT
mkdir -p "$NV_ROOT"/etc/systemd/journald.conf.d "$NV_ROOT"/var/log/journal
FS_KB=$((50 * 1024 * 1024))
df() { printf 'Filesystem 1024-blocks Used Available Capacity Mounted\n/dev/x %s 0 0 0%% /\n' "$FS_KB"; }
printf '[Journal]\n#SystemMaxUse=\n' >"$NV_ROOT"/etc/systemd/journald.conf
printf '[Journal]\nSystemMaxUse=1G\n' >"$NV_ROOT"/etc/systemd/journald.conf.d/50-nivaroos.conf
check 'journald_cap_wanted' "50 GB disk, no admin setting (ours and commented defaults ignored): cap installed"
FS_KB=$((8 * 1024 * 1024))
check '! journald_cap_wanted' "8 GB disk: journald's 10 % default is already lower"
FS_KB=$((50 * 1024 * 1024))
printf '[Journal]\nSystemMaxUse=200M\n' >"$NV_ROOT"/etc/systemd/journald.conf
check '! journald_cap_wanted' "admin's SystemMaxUse in journald.conf wins"
printf '[Journal]\n' >"$NV_ROOT"/etc/systemd/journald.conf
printf '[Journal]\n SystemMaxUse = 300M\n' >"$NV_ROOT"/etc/systemd/journald.conf.d/90-admin.conf
check '! journald_cap_wanted' "admin's SystemMaxUse in another drop-in wins"

echo "passed: $PASS  failed: $FAIL"
[ "$FAIL" -eq 0 ]
