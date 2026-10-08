#!/bin/bash
# shellcheck disable=SC2016,SC2034 # check() evals its single-quoted test, which reads out/rc
# Tests for the safety net: nivaroos-watchdog, nivaroos-rollback and
# nivaroos-deploy, run against a temp directory and a fake systemctl (no
# real unit is touched). Run: bash installer/tests/safety-net-test.sh
set -u

HERE="$(cd "$(dirname "$0")/.." && pwd)"
PASS=0
FAIL=0

T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT

ok() { PASS=$((PASS + 1)); }
bad() { FAIL=$((FAIL + 1)); echo "FAIL: $*" >&2; }
check() { if eval "$1"; then ok; else bad "$2"; fi; }

# ---------------------------------------------------------------------------
# Fake systemctl: unit state lives in $SD/<unit>/<Property>. A unit whose
# binary contains "BAD" fails on (re)start; any other binary starts fine.
cat >"$T/systemctl" <<'EOF'
#!/bin/bash
SD="$FAKE_SD"
echo "$*" >>"$SD/calls"
prop() { cat "$SD/$1/$2" 2>/dev/null; }
start() {
	local u="$1" bin
	bin="$(sed -n 's/.*path=\([^ ;]*\).*/\1/p' "$SD/$u/ExecStart" 2>/dev/null)"
	if [ -n "$bin" ] && grep -q BAD "$bin" 2>/dev/null; then
		echo failed >"$SD/$u/ActiveState"; echo failed >"$SD/$u/SubState"
		return 1
	fi
	echo active >"$SD/$u/ActiveState"; echo running >"$SD/$u/SubState"
}
case "$1" in
	show)
		# show -p PROP --value UNIT
		p="$3"; u="${@: -1}"
		prop "$u" "$p"
		;;
	list-unit-files)
		for d in "$SD"/*/; do
			u="$(basename "$d")"
			[ -f "$d/enabled" ] && case "$u" in nivaroos*.service) echo "$u enabled enabled" ;; esac
		done
		;;
	is-enabled)
		[ -f "$SD/$2/enabled" ] && { echo enabled; exit 0; }
		echo disabled; exit 1
		;;
	is-active) prop "$2" ActiveState ;;
	reset-failed) [ "$(prop "$2" ActiveState)" = failed ] && echo inactive >"$SD/$2/ActiveState"; exit 0 ;;
	restart | start) start "$2" ;;
	*) exit 0 ;;
esac
EOF
chmod +x "$T/systemctl"

# fake curl: prints $FAKE_CURL_CODE
cat >"$T/curl" <<'EOF'
#!/bin/bash
echo "$*" >>"$FAKE_SD/curl-calls"
printf '%s' "${FAKE_CURL_CODE:-200}"
EOF
chmod +x "$T/curl"

reset_env() {
	rm -rf "${T:?}/sd" "${T:?}/bin" "$T/state" "$T/www" "$T/www.prev" "$T/log" "$T/pause"
	mkdir -p "$T/sd" "$T/bin" "$T/state"
	export FAKE_SD="$T/sd"
	export NIVAROOS_SYSTEMCTL="$T/systemctl"
	export NIVAROOS_BIN_DIR="$T/bin"
	export NIVAROOS_WWW_DIR="$T/www"
	export NIVAROOS_WATCHDOG_STATE="$T/state"
	export NIVAROOS_SAFETY_LOG="$T/log"
	export NIVAROOS_WATCHDOG_PAUSE="$T/pause"
	export NIVAROOS_SAFETY_LIB="$HERE/nivaroos-safety-lib.sh"
	export NIVAROOS_GATEWAY_INI="$T/gateway.ini"
	export NIVAROOS_CURL="$T/curl"
	export NIVAROOS_REMOTE_UNITS="tailscaled.service"
	export NIVAROOS_NO_SYSLOG=1
	export NIVAROOS_ALLOW_NONROOT=1
	export NIVAROOS_DEPLOY_SETTLE=0
	export FAKE_CURL_CODE=200
	unset NIVAROOS_NOW
	printf '[gateway]\nport=8089\n' >"$T/gateway.ini"
}

# unit NAME BINARY-NAME STATE [SUB] [enabled]
unit() {
	local u="$1" b="$2" st="$3" sub="${4:-running}"
	mkdir -p "$FAKE_SD/$u"
	echo "$st" >"$FAKE_SD/$u/ActiveState"
	echo "$sub" >"$FAKE_SD/$u/SubState"
	echo 0 >"$FAKE_SD/$u/NRestarts"
	echo simple >"$FAKE_SD/$u/Type"
	if [ -n "$b" ]; then
		echo "{ path=$T/bin/$b ; argv[]=$T/bin/$b -c /etc/x.conf ; ignore_errors=no }" >"$FAKE_SD/$u/ExecStart"
	fi
	touch "$FAKE_SD/$u/enabled"
}

wd() { bash "$HERE/nivaroos-watchdog.sh"; }
calls() { cat "$FAKE_SD/calls" 2>/dev/null; }

# ---------------------------------------------------------------------------
echo "watchdog: healthy units are left alone"
reset_env
echo good >"$T/bin/nivaroos-gateway"
unit nivaroos-gateway.service nivaroos-gateway active
unit nivaroos.service nivaroos active
wd
check '! calls | grep -q "restart"' "healthy: no restart expected, got: $(calls)"
check '[ ! -s "$T/log" ]' "healthy: nothing should be logged"

echo "watchdog: a failed unit with no fresh binary is restarted"
reset_env
echo good >"$T/bin/nivaroos-message-bus"
unit nivaroos-message-bus.service nivaroos-message-bus failed failed
wd
check 'calls | grep -q "^reset-failed nivaroos-message-bus.service"' "failed: reset-failed expected"
check 'calls | grep -q "^restart nivaroos-message-bus.service"' "failed: restart expected"
check '[ "$(cat "$FAKE_SD/nivaroos-message-bus.service/ActiveState")" = active ]' "failed: unit should be active again"
check 'grep -q "restarted nivaroos-message-bus.service" "$T/log"' "failed: restart logged"
wd
check 'grep -q "healthy again" "$T/log"' "recovered unit logged as healthy again"

echo "watchdog: a unit that keeps failing right after an update is rolled back"
reset_env
echo "good build" >"$T/bin/nivaroos-gateway.prev"
echo "BAD build" >"$T/bin/nivaroos-gateway"
unit nivaroos-gateway.service nivaroos-gateway failed failed
wd # 1st failing check: plain restart (fails again - BAD)
check '[ "$(cat "$T/bin/nivaroos-gateway")" = "BAD build" ]' "no rollback on the first failing check"
check '[ "$(cat "$FAKE_SD/nivaroos-gateway.service/ActiveState")" = failed ]' "BAD build stays failed"
wd # 2nd failing check: rollback
check '[ "$(cat "$T/bin/nivaroos-gateway")" = "good build" ]' "binary should be the .prev build after rollback"
check 'ls "$T/bin"/nivaroos-gateway.bad.* >/dev/null 2>&1' "bad build kept as .bad.*"
check 'grep -q "BAD build" "$T/bin"/nivaroos-gateway.bad.*' ".bad.* holds the bad build"
check 'grep -q "ROLLBACK: nivaroos-gateway.service" "$T/log"' "rollback logged"
check '[ "$(cat "$FAKE_SD/nivaroos-gateway.service/ActiveState")" = active ]' "unit active after rollback"
wd
wd
check '[ "$(grep -c ROLLBACK "$T/log")" = 1 ]' "rollback happens once, no flip-flop"

echo "watchdog: no rollback when the binary changed long ago"
reset_env
echo "good build" >"$T/bin/nivaroos-user.prev"
echo "BAD build" >"$T/bin/nivaroos-user"
unit nivaroos-user-service.service nivaroos-user failed failed
export NIVAROOS_NOW=$(($(date +%s) + 2 * 86400))
wd; wd; wd
check '[ "$(cat "$T/bin/nivaroos-user")" = "BAD build" ]' "old change: binary must not be rolled back"
check '! grep -q ROLLBACK "$T/log"' "old change: no rollback logged"
check '[ "$(calls | grep -c "^restart nivaroos-user-service.service")" -ge 3 ]' "old change: still restarted each run"

echo "watchdog: rollback needs a different .prev"
reset_env
echo "BAD same" >"$T/bin/nivaroos-backup"
cp -p "$T/bin/nivaroos-backup" "$T/bin/nivaroos-backup.prev"
unit nivaroos-backup.service nivaroos-backup failed failed
wd; wd
check '! grep -q ROLLBACK "$T/log"' "identical .prev: no rollback"

echo "watchdog: a crash loop that looks 'running' is detected via NRestarts"
reset_env
echo "good" >"$T/bin/nivaroos-local-storage.prev"
echo "BAD flapping" >"$T/bin/nivaroos-local-storage"
unit nivaroos-local-storage.service nivaroos-local-storage active running
wd # baseline NRestarts=0
echo 5 >"$FAKE_SD/nivaroos-local-storage.service/NRestarts"
wd # +5: failing #1
echo 9 >"$FAKE_SD/nivaroos-local-storage.service/NRestarts"
wd # +4: failing #2 -> rollback
check '[ "$(cat "$T/bin/nivaroos-local-storage")" = good ]' "flapping unit rolled back"
check 'grep -q "crash-looping" "$T/log"' "crash loop logged"

echo "watchdog: a unit in auto-restart is counted, not restarted by us"
reset_env
echo good >"$T/bin/nivaroos-app-management"
unit nivaroos-app-management.service nivaroos-app-management activating auto-restart
wd
check '! calls | grep -q "^restart nivaroos-app-management"' "auto-restart: systemd handles it"
check 'grep -q "nivaroos-app-management.service is restarting" "$T/log"' "auto-restart logged"

echo "watchdog: a deliberately stopped unit is left alone"
reset_env
echo good >"$T/bin/nivaroos-vm-sidecar"
unit nivaroos-vm-sidecar.service nivaroos-vm-sidecar inactive dead
wd
check '! calls | grep -q "^restart nivaroos-vm-sidecar"' "stopped unit must not be restarted"

echo "watchdog: oneshot units are ignored"
reset_env
unit nivaroos-sidecar-firewall.service "" failed failed
echo oneshot >"$FAKE_SD/nivaroos-sidecar-firewall.service/Type"
wd
check '! calls | grep -q "^restart nivaroos-sidecar-firewall"' "oneshot ignored"

echo "watchdog: paused while an install/deploy runs"
reset_env
echo good >"$T/bin/nivaroos-message-bus"
unit nivaroos-message-bus.service nivaroos-message-bus failed failed
echo $(($(date +%s) + 600)) >"$T/pause"
wd
check '! calls | grep -q "^restart"' "paused: nothing restarted"
echo $(($(date +%s) - 1)) >"$T/pause"
wd
check 'calls | grep -q "^restart nivaroos-message-bus.service"' "expired pause: watchdog acts again"

echo "watchdog: tailscaled is brought back"
reset_env
unit tailscaled.service "" inactive dead
wd
check 'calls | grep -q "^restart tailscaled.service"' "tailscaled restarted"
check 'grep -q "started tailscaled.service" "$T/log"' "tailscaled restart logged"
reset_env
unit tailscaled.service "" inactive dead
rm "$FAKE_SD/tailscaled.service/enabled"
wd
check '! calls | grep -q "^restart tailscaled"' "disabled tailscaled left alone"

echo "watchdog: gateway not answering HTTP is restarted on the 2nd check"
reset_env
echo good >"$T/bin/nivaroos-gateway"
unit nivaroos-gateway.service nivaroos-gateway active running
export FAKE_CURL_CODE=000
wd
check '! calls | grep -q "^restart nivaroos-gateway"' "http: no restart on first miss"
check 'grep -q "127.0.0.1:8089" "$FAKE_SD/curl-calls"' "http: probe uses the gateway.ini port"
wd
check 'calls | grep -q "^restart nivaroos-gateway.service"' "http: restart on second miss"
export FAKE_CURL_CODE=401
: >"$FAKE_SD/calls"
wd; wd
check '! calls | grep -q "^restart nivaroos-gateway"' "http: any HTTP answer (401) is healthy"

# ---------------------------------------------------------------------------
echo "rollback: list / swap / undo"
reset_env
echo new >"$T/bin/nivaroos-gateway"
echo old >"$T/bin/nivaroos-gateway.prev"
echo same >"$T/bin/nivaroos"
cp -p "$T/bin/nivaroos" "$T/bin/nivaroos.prev"
unit nivaroos-gateway.service nivaroos-gateway active
out="$(bash "$HERE/nivaroos-rollback.sh")"
check 'printf "%s" "$out" | grep -q "nivaroos-gateway .*current"' "list shows gateway"
check 'printf "%s" "$out" | grep -q "identical"' "list flags identical .prev"
bash "$HERE/nivaroos-rollback.sh" --dry-run gateway >/dev/null
check '[ "$(cat "$T/bin/nivaroos-gateway")" = new ]' "dry run changes nothing"
bash "$HERE/nivaroos-rollback.sh" gateway >/dev/null
check '[ "$(cat "$T/bin/nivaroos-gateway")" = old ] && [ "$(cat "$T/bin/nivaroos-gateway.prev")" = new ]' "rollback swaps"
check 'calls | grep -q "^restart nivaroos-gateway.service"' "rollback restarts the unit"
bash "$HERE/nivaroos-rollback.sh" nivaroos-gateway.service >/dev/null
check '[ "$(cat "$T/bin/nivaroos-gateway")" = new ]' "second rollback (by unit name) undoes the first"
bash "$HERE/nivaroos-rollback.sh" core >/dev/null 2>&1
rc=$?
check '[ $rc -ne 0 ] && [ "$(cat "$T/bin/nivaroos")" = same ]' "identical core: refused, nothing changed"
check '[ ! -f "$T/pause" ]' "rollback removes its watchdog pause"
bash "$HERE/nivaroos-rollback.sh" all >/dev/null
check '[ "$(cat "$T/bin/nivaroos-gateway")" = old ] && [ "$(cat "$T/bin/nivaroos")" = same ]' "all: only differing binaries swapped"
# unit-name resolution for a binary named differently from its unit
echo u-new >"$T/bin/nivaroos-user"; echo u-old >"$T/bin/nivaroos-user.prev"
unit nivaroos-user-service.service nivaroos-user active
bash "$HERE/nivaroos-rollback.sh" user-service >/dev/null
check '[ "$(cat "$T/bin/nivaroos-user")" = u-old ]' "user-service resolves to nivaroos-user"
bash "$HERE/nivaroos-rollback.sh" nosuch >/dev/null 2>&1
rc=$?
check '[ $rc -ne 0 ]' "unknown component fails"

echo "rollback: web dashboard"
mkdir -p "$T/www" "$T/www.prev"
echo new >"$T/www/index.html"; echo old >"$T/www.prev/index.html"
bash "$HERE/nivaroos-rollback.sh" www >/dev/null
check '[ "$(cat "$T/www/index.html")" = old ] && [ "$(cat "$T/www.prev/index.html")" = new ]' "www swapped"

# ---------------------------------------------------------------------------
echo "deploy: good build is installed, previous kept"
reset_env
echo "v1 good" >"$T/bin/nivaroos-gateway"
unit nivaroos-gateway.service nivaroos-gateway active
printf 'v2 good\n' >"$T/new"; chmod +x "$T/new"
bash "$HERE/nivaroos-deploy.sh" "$T/new" nivaroos-gateway >/dev/null
check '[ "$(cat "$T/bin/nivaroos-gateway")" = "v2 good" ]' "deploy installs new build"
check '[ "$(cat "$T/bin/nivaroos-gateway.prev")" = "v1 good" ]' "deploy keeps previous build"
check '[ -x "$T/bin/nivaroos-gateway" ]' "deployed binary executable"
check '[ ! -f "$T/pause" ]' "deploy removes its watchdog pause"

echo "deploy: bad build is rolled back immediately"
printf 'v3 BAD\n' >"$T/new"; chmod +x "$T/new"
bash "$HERE/nivaroos-deploy.sh" "$T/new" nivaroos-gateway.service >/dev/null 2>&1
rc=$?
check '[ $rc -ne 0 ]' "bad deploy exits non-zero"
check '[ "$(cat "$T/bin/nivaroos-gateway")" = "v2 good" ]' "bad deploy restores previous build"
check '[ "$(cat "$FAKE_SD/nivaroos-gateway.service/ActiveState")" = active ]' "unit running again after failed deploy"
check 'grep -q "deploy of .*nivaroos-gateway failed" "$T/log"' "failed deploy logged"

echo "deploy: web dashboard"
mkdir -p "$T/www" "$T/newui"
echo live >"$T/www/index.html"; echo fresh >"$T/newui/index.html"
bash "$HERE/nivaroos-deploy.sh" --www "$T/newui" >/dev/null
check '[ "$(cat "$T/www/index.html")" = fresh ] && [ "$(cat "$T/www.prev/index.html")" = live ]' "www deployed with previous kept"

# ---------------------------------------------------------------------------
echo "recover: reset-password runs the user binary with a private runtime dir"
reset_env
mkdir -p "$T/run"; echo "http://127.0.0.1:1" >"$T/run/management.url"
printf '[common]\nRuntimePath=/var/run/nivaroos\n\n[app]\nDBPath = /var/lib/nivaroos/db\n' >"$T/user.conf"
cat >"$T/fake-user" <<'EOF2'
#!/bin/bash
# args: -c CONF -ru -user NAME
conf="$2"; name="$5"
grep '^RuntimePath=' "$conf" >"$FAKE_SD/user-runtime"
ls "$(sed -n 's/^RuntimePath=//p' "$conf")" >"$FAKE_SD/user-runtime-files"
if [ "$name" = owner ]; then echo "User reset successful"; echo "UserName:owner"; echo "Password:Abc123Abc123Abc1"; exit 0; fi
echo "user not exist"; echo "User:owner"; exit 1
EOF2
chmod +x "$T/fake-user"
unit nivaroos-user-service.service nivaroos-user active
out="$(NIVAROOS_USER_BIN="$T/fake-user" NIVAROOS_USER_CONF="$T/user.conf" NIVAROOS_RUNTIME_DIR="$T/run" bash "$HERE/nivaroos-recover.sh" reset-password owner 2>&1)"
check 'printf "%s" "$out" | grep -q "^Password:Abc123Abc123Abc1"' "recover prints the new password"
check '! grep -q "/var/run/nivaroos" "$FAKE_SD/user-runtime"' "recover must not hand the real runtime dir to the reset"
check 'grep -q management.url "$FAKE_SD/user-runtime-files"' "private runtime dir carries management.url"
check 'calls | grep -q "^restart nivaroos-user-service.service"' "user service restarted after reset"
check 'grep -q "password of .owner. reset" "$T/log"' "reset logged (without the password)"
check '! grep -q Abc123 "$T/log"' "password never logged"
out="$(NIVAROOS_USER_BIN="$T/fake-user" NIVAROOS_USER_CONF="$T/user.conf" NIVAROOS_RUNTIME_DIR="$T/run" bash "$HERE/nivaroos-recover.sh" reset-password ghost 2>&1)"
rc=$?
check '[ $rc -ne 0 ] && printf "%s" "$out" | grep -q "User:owner"' "unknown user fails and lists users"

echo
echo "passed: $PASS  failed: $FAIL"
[ "$FAIL" -eq 0 ]
