#!/usr/bin/env bash
# shellcheck disable=SC2016 # check() evals its single-quoted test
# install.sh's remove_leftovers_now against a temp root: removes CasaOS-era
# leftovers and stale binary copies, keeps .prev, casaos-cli, the rest of
# /var/lib/casaos, and a CasaOS that is still enabled.
# Run: bash installer/tests/leftovers-test.sh
set -u
d="$(cd "$(dirname "$0")/.." && pwd)"
PASS=0 FAIL=0
check() { if eval "$1"; then PASS=$((PASS + 1)); else FAIL=$((FAIL + 1)); echo "FAIL: $2" >&2; fi; }

# shellcheck disable=SC1090
. <(sed '$d' "$d/install.sh")
set +eE
trap - ERR INT TERM EXIT

NV_ROOT="$(mktemp -d)"
trap 'rm -rf "$NV_ROOT"' EXIT
CASAOS_STATE=disabled
systemctl() { [ "$CASAOS_STATE" = enabled ]; }

setup() {
	rm -rf "${NV_ROOT:?}"/*
	mkdir -p "$NV_ROOT"/usr/bin "$NV_ROOT"/usr/lib/systemd/system "$NV_ROOT"/var/lib/nivaroos/www "$NV_ROOT"/var/lib/casaos/{www/js,db,apps}
	touch "$NV_ROOT"/var/lib/nivaroos/www/index.html "$NV_ROOT"/var/lib/casaos/www/js/app.1.js "$NV_ROOT"/var/lib/casaos/db/x.db
	for f in nivaroos nivaroos.prev nivaroos.bak nivaroos-app-management.bak nivaroos-user nivaroos-user-service nivaroos-backup nivaroos-backup.prev \
		casaos casaos-gateway casaos-uninstall casaos.bak-task8 casaos-local-storage.bak-1 nivaroos-local-storage.bad.new nivaroos-local-storage.bad.old; do
		echo x >"$NV_ROOT/usr/bin/$f"
	done
	chmod +x "$NV_ROOT"/usr/bin/nivaroos
	ln -s nivaroos-cli "$NV_ROOT"/usr/bin/casaos-cli
	touch -d '-8 days' "$NV_ROOT"/usr/bin/nivaroos-local-storage.bad.old
	touch "$NV_ROOT"/usr/lib/systemd/system/{casaos.service,casaos-gateway.service,casaos-gateway.service.buildroot,nivaroos.service}
}
e() { [ -e "$NV_ROOT/$1" ] || [ -L "$NV_ROOT/$1" ]; }

setup
remove_leftovers_now
for f in var/lib/casaos/www usr/bin/casaos usr/bin/casaos-gateway usr/bin/casaos-uninstall usr/bin/casaos.bak-task8 usr/bin/casaos-local-storage.bak-1 \
	usr/bin/nivaroos.bak usr/bin/nivaroos-app-management.bak usr/bin/nivaroos-user-service usr/bin/nivaroos-local-storage.bad.old \
	usr/lib/systemd/system/casaos.service usr/lib/systemd/system/casaos-gateway.service usr/lib/systemd/system/casaos-gateway.service.buildroot; do
	check "! e $f" "$f should be removed"
done
for f in var/lib/casaos/db/x.db var/lib/casaos/apps usr/bin/nivaroos usr/bin/nivaroos.prev usr/bin/nivaroos-user usr/bin/nivaroos-backup usr/bin/nivaroos-backup.prev \
	usr/bin/casaos-cli usr/bin/nivaroos-local-storage.bad.new usr/lib/systemd/system/nivaroos.service; do
	check "e $f" "$f must be kept"
done

setup
CASAOS_STATE=enabled
remove_leftovers_now
check 'e usr/bin/casaos && e usr/bin/casaos-gateway && e usr/lib/systemd/system/casaos.service' "an enabled CasaOS is left alone"
check '! casaos_leftover' "an enabled CasaOS is not a leftover"

setup
CASAOS_STATE=disabled
rm -f "$NV_ROOT"/var/lib/nivaroos/www/index.html "$NV_ROOT"/usr/bin/nivaroos
remove_leftovers_now
check 'e var/lib/casaos/www && e usr/bin/casaos' "no NivaroOS dashboard/binary yet: CasaOS files stay"

echo "passed: $PASS  failed: $FAIL"
[ "$FAIL" -eq 0 ]
