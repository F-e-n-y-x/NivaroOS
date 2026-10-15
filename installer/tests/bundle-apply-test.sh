#!/bin/bash
# shellcheck disable=SC2016 # check() evals its single-quoted test
# installer/bundle/apply.sh against a temp root (NV_ROOT), a fake systemctl
# and a fake curl: install, only-what-changed updates, health-check and
# probation rollbacks, migrations, tampering, manual rollback. Nothing real
# is touched. Run: bash installer/tests/bundle-apply-test.sh
set -u

HERE="$(cd "$(dirname "$0")/.." && pwd)"
PASS=0 FAIL=0
T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT
ok() { PASS=$((PASS + 1)); }
bad() { FAIL=$((FAIL + 1)); echo "FAIL: $*" >&2; }
check() { if eval "$1"; then ok; else bad "$2"; fi; }

# Fake systemctl: state in $SD/<unit>/; a unit's binary is the root's
# /usr/bin file named in $SD/<unit>/bin. BAD in it: fails to start.
# FLAKY: runs, but its restart count grows after the first few looks.
cat >"$T/systemctl" <<'EOF'
#!/bin/bash
SD="$FAKE_SD"
echo "$*" >>"$SD/calls"
u="${*: -1}"
mkdir -p "$SD/$u"
bin() { echo "$NV_ROOT/usr/bin/$(cat "$SD/$1/bin" 2>/dev/null)"; }
start() {
	if grep -q BAD "$(bin "$1")" 2>/dev/null; then
		echo failed >"$SD/$1/ActiveState"; echo failed >"$SD/$1/SubState"; return 1
	fi
	echo active >"$SD/$1/ActiveState"; echo running >"$SD/$1/SubState"
}
case "$1" in
	show)
		p="$3"
		if [ "$p" = NRestarts ] && grep -q FLAKY "$(bin "$u")" 2>/dev/null; then
			n=$(($(cat "$SD/$u/looks" 2>/dev/null || echo 0) + 1)); echo "$n" >"$SD/$u/looks"
			echo $((n > 3 ? n - 3 : 0)); exit 0
		fi
		cat "$SD/$u/$p" 2>/dev/null || { [ "$p" = NRestarts ] && echo 0; }
		;;
	is-active) [ "$(cat "$SD/$u/ActiveState" 2>/dev/null)" = active ] ;;
	stop) echo inactive >"$SD/$u/ActiveState"; echo dead >"$SD/$u/SubState" ;;
	restart | start) start "$u" ;;
	enable) touch "$SD/$u/enabled" ;;
	*) exit 0 ;;
esac
EOF
cat >"$T/curl" <<'EOF'
#!/bin/bash
printf '%s' "${FAKE_CURL_CODE:-200}"
EOF
chmod +x "$T/systemctl" "$T/curl"

export NV_ROOT="$T/root" FAKE_SD="$T/sd" NIVAROOS_SYSTEMCTL="$T/systemctl" NIVAROOS_CURL="$T/curl"
export NIVAROOS_NO_SYSLOG=1 NIVAROOS_DEPLOY_SETTLE=0 NIVAROOS_PROBATION=0 NIVAROOS_PROBE_EVERY=1
SNAP="$NV_ROOT/var/lib/nivaroos/update/snap"

# mkbundle DIR VERSION CORE [BACKUP] - a bundle whose core binary says CORE.
mkbundle() {
	local d="$1" v="$2" core="$3" backup="${4:-backup 1}" rr
	rm -rf "$d"
	rr="$d/root"
	mkdir -p "$rr/usr/bin" "$rr/usr/lib/systemd/system" "$rr/etc/nivaroos" "$rr/var/lib/nivaroos/www" \
		"$rr/usr/share/nivaroos" "$rr/usr/local/lib/nivaroos" "$d/lib" "$d/migrations"
	printf '#!/bin/sh\necho %s\n' "$core" >"$rr/usr/bin/nivaroos"
	printf '#!/bin/sh\necho %s\n' "$backup" >"$rr/usr/bin/nivaroos-backup"
	printf '#!/bin/sh\necho gateway\n' >"$rr/usr/bin/nivaroos-gateway"
	chmod 755 "$rr"/usr/bin/*
	echo "[Service]" >"$rr/usr/lib/systemd/system/nivaroos.service"
	echo "[Service]" >"$rr/usr/lib/systemd/system/nivaroos-backup.service"
	echo "[Service]" >"$rr/usr/lib/systemd/system/nivaroos-gateway.service"
	echo "x = 1" >"$rr/etc/nivaroos/casaos.conf.sample"
	echo "<html>$v</html>" >"$rr/var/lib/nivaroos/www/index.html"
	echo "[Service]" >"$rr/usr/local/lib/nivaroos/10-nivaroos-resilience.conf"
	echo "[Service]" >"$rr/usr/lib/systemd/system/nivaroos-update-resume.service"
	[ "$v" = source ] || printf '{"version":"%s","channel":"stable","commit":"c","built":"b"}\n' "$v" >"$rr/usr/share/nivaroos/release.json"
	cp "$HERE/bundle/apply.sh" "$d/apply.sh"
	cp "$HERE/nivaroos-safety-lib.sh" "$d/lib/safety-lib.sh"
	{
		echo "version $v"
		echo "component core always nivaroos.service"
		echo "component gateway always nivaroos-gateway.service"
		echo "component backup /usr/bin/nivaroos-backup nivaroos-backup.service"
		echo "component safety always -"
		echo "component ui always -"
		echo "component release always -"
		(cd "$d" && find root apply.sh lib migrations -type f | sort) | while read -r f; do
			case "$f" in
				root/usr/bin/nivaroos | root/usr/lib/systemd/system/nivaroos.service | root/etc/nivaroos/*) c=core ;;
				*gateway*) c=gateway ;;
				*backup*) c=backup ;;
				root/var/lib/nivaroos/www/*) c=ui ;;
				*release.json) c=release ;;
				root/*) c=safety ;;
				*) c=bundle ;;
			esac
			echo "file $c $(stat -c %a "$d/$f") $(sha256sum "$d/$f" | cut -d' ' -f1) $f"
		done
	} >"$d/MANIFEST"
}
apply() { out="$(bash "$1/apply.sh" "${@:2}" 2>&1)"; rc=$?; }
for u in nivaroos.service nivaroos-gateway.service nivaroos-backup.service; do
	mkdir -p "$FAKE_SD/$u"
done
echo nivaroos >"$FAKE_SD/nivaroos.service/bin"
echo nivaroos-gateway >"$FAKE_SD/nivaroos-gateway.service/bin"
echo nivaroos-backup >"$FAKE_SD/nivaroos-backup.service/bin"
core() { cat "$NV_ROOT/usr/bin/nivaroos"; }

# 1. First install: everything in place, services up, confs from samples.
mkbundle "$T/b1" 1.0.0 "core 1"
apply "$T/b1" install --with backup
check '[ "$rc" = 0 ]' "install exits 0 ($rc): $out"
check 'grep -q "core 1" "$NV_ROOT/usr/bin/nivaroos"' "install puts the core binary in place"
check '[ -f "$NV_ROOT/etc/nivaroos/casaos.conf" ]' "install creates the live conf from its sample"
check '[ "$(readlink "$NV_ROOT/usr/local/bin/nivaroos")" = /usr/bin/nivaroos-cli ]' "install links nivaroos to the CLI"
check '[ -f "$NV_ROOT/etc/systemd/system/nivaroos.service.d/10-nivaroos-resilience.conf" ]' "install adds the resilience drop-in"
check '[ -f "$FAKE_SD/nivaroos-backup.service/enabled" ]' "install enables a chosen add-on"
check 'grep -q "1.0.0" "$NV_ROOT/usr/share/nivaroos/release.json"' "install writes release.json"
check 'grep -qx /usr/bin/nivaroos "$NV_ROOT/var/lib/nivaroos/manifest"' "install records files in the uninstall manifest"
check '[ "$(cat "$SNAP/state")" = committed ]' "install commits"
check '[ -f "$FAKE_SD/nivaroos-update-resume.service/enabled" ] && ! grep -qE "^(start|restart) nivaroos-update-resume" "$FAKE_SD/calls"' "the resume unit is enabled for boot, never started"

# 2. Re-applying the same bundle changes nothing and stops nothing.
: >"$FAKE_SD/calls"
apply "$T/b1" update
check '[ "$rc" = 0 ] && grep -q "nothing to do" <<<"$out"' "same bundle again: nothing to do ($rc): $out"
check '! grep -q "^stop" "$FAKE_SD/calls"' "same bundle again: nothing stopped"

# 3. Update where only core changed: backup keeps running, untouched.
echo "user edit" >>"$NV_ROOT/etc/nivaroos/casaos.conf"
echo "db v1" >"$NV_ROOT/var/lib/nivaroos/db/casaOS.db"
mkbundle "$T/b2" 1.1.0 "core 2"
: >"$FAKE_SD/calls"
apply "$T/b2" update
check '[ "$rc" = 0 ]' "core-only update exits 0 ($rc): $out"
check 'grep -q "core 2" "$NV_ROOT/usr/bin/nivaroos"' "core-only update installs the new core"
check 'grep -q "core 1" "$NV_ROOT/usr/bin/nivaroos.prev"' "core-only update keeps the old core as .prev"
check '! grep -q "nivaroos-backup" "$FAKE_SD/calls"' "core-only update never touches Backup & Sync"
check 'grep -q "^stop nivaroos.service" "$FAKE_SD/calls" && grep -q "^restart nivaroos.service" "$FAKE_SD/calls"' "core-only update restarts core"
check 'grep -q "user edit" "$NV_ROOT/etc/nivaroos/casaos.conf"' "an update never overwrites a live conf"
check 'grep -q "1.1.0" "$NV_ROOT/var/lib/nivaroos/www/index.html"' "update swaps the dashboard"
check 'grep -q "1.0.0" "$NV_ROOT/var/lib/nivaroos/www.prev/index.html"' "update keeps the old dashboard as www.prev"
check '[ "$(cat "$SNAP/from-version") $(cat "$SNAP/to-version")" = "1.0.0 1.1.0" ]' "the snapshot names both versions"

# 4. A new core that doesn't start: whole set back, database restored.
mkbundle "$T/b3" 1.2.0 "core 3 BAD"
printf '#!/bin/sh\necho "db v3 migrated" >"$NV_ROOT/var/lib/nivaroos/db/casaOS.db"\n' >"$T/b3/migrations/0001-touch-db.sh"
mkbundle_manifest_add() { echo "file bundle 755 $(sha256sum "$1/$2" | cut -d' ' -f1) $2" >>"$1/MANIFEST"; }
mkbundle_manifest_add "$T/b3" migrations/0001-touch-db.sh
apply "$T/b3" update
check '[ "$rc" = 1 ]' "failed health check exits 1 ($rc): $out"
check 'grep -q "core 2" "$NV_ROOT/usr/bin/nivaroos"' "failed health check restores the previous core"
check 'grep -q "1.1.0" "$NV_ROOT/usr/share/nivaroos/release.json"' "failed health check restores release.json"
check 'grep -q "1.1.0" "$NV_ROOT/var/lib/nivaroos/www/index.html"' "failed health check restores the dashboard"
check 'grep -qx "db v1" "$NV_ROOT/var/lib/nivaroos/db/casaOS.db"' "failed health check restores the database"
check '[ "$(cat "$FAKE_SD/nivaroos.service/ActiveState")" = active ]' "core runs again after the rollback"
check '[ "$(cat "$SNAP/state")" = rolled-back ]' "the snapshot is marked rolled back"
check 'grep -qx 0001-touch-db.sh "$NV_ROOT/var/lib/nivaroos/update/migrations.done"' "a migration is recorded once run"

# 5. A failing migration: nothing of the new release stays.
mkbundle "$T/b4" 1.3.0 "core 4"
printf '#!/bin/sh\nexit 1\n' >"$T/b4/migrations/0002-fails.sh"
mkbundle_manifest_add "$T/b4" migrations/0002-fails.sh
apply "$T/b4" update
check '[ "$rc" = 1 ] && grep -q "migration 0002-fails.sh failed" <<<"$out"' "failing migration exits 1 ($rc): $out"
check 'grep -q "core 2" "$NV_ROOT/usr/bin/nivaroos"' "failing migration leaves the old core"
check '! grep -qx 0002-fails.sh "$NV_ROOT/var/lib/nivaroos/update/migrations.done"' "a failed migration is not recorded"

# 6. A file that doesn't match MANIFEST: refused, nothing changed.
mkbundle "$T/b5" 1.4.0 "core 5"
echo tampered >>"$T/b5/root/usr/bin/nivaroos"
: >"$FAKE_SD/calls"
apply "$T/b5" update
check '[ "$rc" = 3 ] && grep -q "does not match" <<<"$out"' "tampered bundle refused with 3 ($rc): $out"
check 'grep -q "core 2" "$NV_ROOT/usr/bin/nivaroos" && ! grep -q "^stop" "$FAKE_SD/calls"' "tampered bundle changes nothing"

# 7. Probation: a service that starts crash-looping a little later.
mkbundle "$T/b6" 1.5.0 "core 6 FLAKY"
rm -f "$FAKE_SD/nivaroos.service/looks"
NIVAROOS_PROBATION=3 apply "$T/b6" update
check '[ "$rc" = 1 ] && grep -q "failed during probation" <<<"$out"' "probation failure rolls back with 1 ($rc): $out"
check 'grep -q "core 2" "$NV_ROOT/usr/bin/nivaroos"' "probation failure restores the previous core"

# 8. Manual rollback after a good update, then nothing left to roll back.
mkbundle "$T/b7" 1.6.0 "core 7"
apply "$T/b7" update
check '[ "$rc" = 0 ] && grep -q "core 7" "$NV_ROOT/usr/bin/nivaroos"' "good update after rollbacks ($rc): $out"
apply "$SNAP" rollback
check '[ "$rc" = 0 ] && grep -q "core 2" "$NV_ROOT/usr/bin/nivaroos"' "manual rollback restores the previous release ($rc): $out"
check 'grep -q "1.1.0" "$NV_ROOT/usr/share/nivaroos/release.json"' "manual rollback restores release.json"
apply "$SNAP" rollback
check '[ "$rc" = 3 ]' "a second rollback is refused ($rc): $out"

# 9. A from-source bundle drops release.json (the box is no longer on a release).
mkbundle "$T/b8" source "core 8"
apply "$T/b8" install
check '[ "$rc" = 0 ] && [ ! -e "$NV_ROOT/usr/share/nivaroos/release.json" ]' "from-source install removes release.json ($rc): $out"

# 10. An add-on that isn't installed is not installed by an update.
rm -f "$NV_ROOT/usr/bin/nivaroos-backup"
mkbundle "$T/b9" 1.7.0 "core 9" "backup 9"
apply "$T/b9" update
check '[ "$rc" = 0 ] && [ ! -e "$NV_ROOT/usr/bin/nivaroos-backup" ]' "update skips an add-on this box doesn't have ($rc): $out"

echo "bundle-apply: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]
