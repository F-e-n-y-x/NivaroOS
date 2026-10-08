#!/usr/bin/env bash
# shellcheck disable=SC2016 # check() evals its single-quoted test
# Uninstaller data safety, run against temp folders (nothing real is
# touched): manifest entries outside NivaroOS's own paths are never
# deleted, data is deleted only as chosen, and never on another
# filesystem. Run: bash installer/tests/uninstall-test.sh
set -u
d="$(cd "$(dirname "$0")/.." && pwd)"
PASS=0 FAIL=0
check() { if eval "$1"; then PASS=$((PASS + 1)); else FAIL=$((FAIL + 1)); echo "FAIL: $2" >&2; fi; }

# The uninstaller's functions, without running main.
# shellcheck disable=SC1090
. <(sed '$d' "$d/uninstall.sh")
set +eE
trap - ERR INT TERM

for p in /usr/bin/nivaroos-gateway /usr/lib/systemd/system/nivaroos.service /var/lib/nivaroos/www /opt/nivaroos/chromium /etc/sysctl.d/60-nivaroos-ds-browser.conf; do
	check "is_ours $p" "is_ours should accept $p"
done
for p in /DATA /DATA/AppData / /etc/passwd /usr/bin/bash /var/lib/nivaroos/../../etc /root ''; do
	check "! is_ours '$p'" "is_ours must reject '$p'"
done

# delete_data on the system disk (/var/tmp) deletes what was chosen.
mk() {
	DATA_DIR="$(mktemp -d "$1/nv-data.XXXXXX")"
	mkdir -p "$DATA_DIR/AppData/x" "$DATA_DIR/VMs/vm1" "$DATA_DIR/Documents" "$DATA_DIR/.hidden"
	SKIP_FILE="$(mktemp)"
}
mk /var/tmp
if on_root_fs "$DATA_DIR"; then
	DEL_APPS=yes DEL_VMS=no DEL_FILES=no delete_data >/dev/null
	check '[ ! -e "$DATA_DIR/AppData" ] && [ -d "$DATA_DIR/VMs" ] && [ -d "$DATA_DIR/Documents" ]' "apps only: AppData deleted, VMs and files kept"
	DEL_APPS=no DEL_VMS=no DEL_FILES=yes delete_data >/dev/null
	check '[ -d "$DATA_DIR/VMs" ] && [ ! -e "$DATA_DIR/Documents" ] && [ ! -e "$DATA_DIR/.hidden" ]' "files: everything but kept VMs deleted"
	DEL_VMS=yes DEL_FILES=yes delete_data >/dev/null
	check '[ ! -e "$DATA_DIR" ]' "all deleted: the data folder itself goes"
else
	echo "skip: /var/tmp is not on the root filesystem here"
fi
rm -rf "$DATA_DIR" "$SKIP_FILE"

# ...and never on another filesystem (/dev/shm is tmpfs).
if [ -d /dev/shm ] && [ -w /dev/shm ] && ! on_root_fs /dev/shm; then
	mk /dev/shm
	DEL_APPS=yes DEL_VMS=yes DEL_FILES=yes delete_data >/dev/null
	check '[ -d "$DATA_DIR/AppData/x" ] && [ -d "$DATA_DIR/VMs/vm1" ] && [ -d "$DATA_DIR/Documents" ]' "another drive: nothing deleted"
	check 'grep -q AppData "$SKIP_FILE"' "another drive: reported as kept"
	rm -rf "$DATA_DIR" "$SKIP_FILE"
fi

echo "passed: $PASS  failed: $FAIL"
[ "$FAIL" -eq 0 ]
