#!/usr/bin/env bash
# ==============================================================================
#  Backup & Sync end-to-end checks (spec docs/specs/2026-09-24-backup-sync-app.md
#  §16.4), run on an installed NivaroOS box as root.
#
#  Non-destructive by default: everything it creates lives in
#  /DATA/.nivaro-backup-e2e (removed at the end) and in jobs named
#  "e2e: ..." (deleted at the end). It never touches other jobs, other
#  folders, drives, apps, VMs or Scheduled Tasks.
#
#    installer/tests/backup-e2e.sh                 # the default checks
#    installer/tests/backup-e2e.sh --migration     # also re-run the Scheduled
#                                                  # Tasks import (idempotent,
#                                                  # but it does talk to core)
#    installer/tests/backup-e2e.sh --url http://127.0.0.1:28691/v1/backup --no-gateway
#                                                  # a test instance on another port
#    installer/tests/backup-e2e.sh --keep          # leave the temp folder and jobs
#
#  Checks that need hardware, accounts or a throwaway VM (fresh installs per
#  distro, USB plug/unplug, cloud and SMB targets, app hooks with a core
#  kill, the 1970 clock, axe-core on the UI) are listed as SKIP with what
#  to do by hand.
#
#  Exit status: 0 when every check ran passed (SKIPs don't count), 1 when
#  one failed, 2 on bad usage or a missing prerequisite.
# ==============================================================================

if [ -z "${BASH_VERSION:-}" ]; then
	exec bash "$0" "$@"
fi
set -Eeuo pipefail

API="http://127.0.0.1:28643/v1/backup"
USE_GATEWAY=yes
KEEP=no
WITH_MIGRATION=no
WORK="/DATA/.nivaro-backup-e2e"
RUN_TIMEOUT=300

usage() {
	sed -n '2,27p' "$0" | sed 's/^# \{0,1\}//'
}

while [ $# -gt 0 ]; do
	case "$1" in
		--url) shift; API="${1:?--url needs a value}" ;;
		--url=*) API="${1#*=}" ;;
		--no-gateway) USE_GATEWAY=no ;;
		--keep) KEEP=yes ;;
		--migration) WITH_MIGRATION=yes ;;
		-h|--help) usage; exit 0 ;;
		*) echo "unknown argument: $1 (see --help)" >&2; exit 2 ;;
	esac
	shift
done
API="${API%/}"

if [ "$(id -u)" -ne 0 ]; then
	echo "run as root: the checks read and write under /DATA as the service does" >&2
	exit 2
fi
for bin in curl python3; do
	command -v "$bin" >/dev/null 2>&1 || { echo "$bin is required" >&2; exit 2; }
done

PASS=0
FAIL=0
SKIP=0
JOBS_CREATED=()

c_ok=$'\033[32m'; c_bad=$'\033[31m'; c_skip=$'\033[33m'; c_off=$'\033[0m'
[ -t 1 ] || { c_ok=''; c_bad=''; c_skip=''; c_off=''; }

pass() { PASS=$((PASS + 1)); printf '  %sPASS%s %s\n' "$c_ok" "$c_off" "$1"; }
fail() { FAIL=$((FAIL + 1)); printf '  %sFAIL%s %s\n' "$c_bad" "$c_off" "$1"; }
skip() { SKIP=$((SKIP + 1)); printf '  %sSKIP%s %s\n' "$c_skip" "$c_off" "$1"; }
section() { printf '\n%s\n' "$1"; }

# check "description" command... : PASS when the command succeeds.
check() {
	local what="$1"
	shift
	if "$@"; then pass "$what"; else fail "$what"; fi
}

# ---------------------------------------------------------------------------
# HTTP + JSON helpers. Requests go without a token: loopback, no browser
# headers - the service's local automation rule (and the gateway vouches
# for the same through its header).

LAST_STATUS=""
LAST_BODY=""

# api METHOD PATH [JSON] - sets LAST_STATUS/LAST_BODY; never fails the shell.
api() {
	local method="$1" path="$2" data="${3:-}" out
	local args=(-sS -m 60 -X "$method" -w $'\n%{http_code}')
	if [ -n "$data" ]; then
		args+=(-H 'Content-Type: application/json' --data-binary "$data")
	fi
	out="$(curl "${args[@]}" "${API}${path}" 2>/dev/null)" || out=$'\n000'
	LAST_STATUS="${out##*$'\n'}"
	LAST_BODY="${out%$'\n'*}"
}

# jq_py EXPR - evaluates a Python expression over the JSON on stdin (as
# "d"; "data" is the envelope's data). Prints "" for None / errors.
jq_py() {
	python3 -c '
import json, sys
try:
    d = json.load(sys.stdin)
except Exception:
    sys.exit(0)
data = d.get("data") if isinstance(d, dict) and "data" in d else d
try:
    v = eval(sys.argv[1])
except Exception:
    sys.exit(0)
if v is None:
    pass
elif isinstance(v, bool):
    print("true" if v else "false")
elif isinstance(v, (dict, list)):
    print(json.dumps(v))
else:
    print(v)
' "$1"
}

field() { printf '%s' "$LAST_BODY" | jq_py "$1"; }

# endpoint_for PATH - the endpoint JSON Backup resolves a folder to.
endpoint_for() {
	api POST /locations/resolve-path "{\"path\":\"$1\"}"
	if [ "$LAST_STATUS" != 200 ] || [ "$(field 'data["ok"]')" != true ]; then
		echo "resolve-path $1 failed ($LAST_STATUS): $LAST_BODY" >&2
		return 1
	fi
	field 'data["endpoint"]'
}

# job_json NAME TYPE SRC_EP DEST_EP [DELETE_PCT] [CONDITIONS_JSON]
job_json() {
	local name="$1" type="$2" src="$3" dst="$4" del="${5:-40}" cond="${6:-}"
	[ -n "$cond" ] || cond='{"dest_available": true, "when_unmet": "wait"}'
	cat <<EOF
{
  "name": "$name", "type": "$type", "enabled": true,
  "sources": [$src],
  "dest": $dst,
  "triggers": [{"kind": "schedule", "cron": "0 3 1 1 *", "catch_up": false}],
  "conditions": $cond,
  "filters": {"exclude_presets": [], "exclude": []},
  "options": {"verify": false, "preview_first": false, "low_priority": false, "max_duration_sec": 3600, "copy_empty_dirs": false},
  "guards": {"empty_source_pct": 90, "delete_pct": $del, "change_pct": 90, "allow_empty_source": false},
  "retention": {"versions_days": 30},
  "hooks": [],
  "retry": {"max": 0, "backoff_sec": []},
  "notify": {"on_success": false, "on_failure": false, "stale_after_hours": 0},
  "needs_attention": "",
  "migrated_from": null
}
EOF
}

# create_job JSON - sets JOB_ID ("" on failure) and remembers the job for
# the cleanup. Not called in $(...): the list must survive.
JOB_ID=""
create_job() {
	JOB_ID=""
	api POST /jobs "$1"
	if [ "$LAST_STATUS" != 200 ] && [ "$LAST_STATUS" != 201 ]; then
		echo "    create job failed ($LAST_STATUS): $LAST_BODY" >&2
		return 0
	fi
	JOB_ID="$(field 'data["id"]')"
	if [ -z "$JOB_ID" ]; then
		echo "    create job: no id in $LAST_BODY" >&2
		return 0
	fi
	JOBS_CREATED+=("$JOB_ID")
}

# run_job ID - starts a run now; sets RID ("" when it couldn't start).
RID=""
run_job() {
	api POST "/jobs/$1/run" '{"preview":false}'
	RID="$(field 'data["run_id"]')"
	[ -n "$RID" ] || echo "    run job $1 failed ($LAST_STATUS): $LAST_BODY" >&2
}

# wait_run RUN_ID - waits until the run is final or waiting_user and
# prints its status (or "timeout", or "not_started" without a run id).
wait_run() {
	local id="$1" st i
	if [ -z "$id" ]; then
		printf 'not_started'
		return 0
	fi
	for ((i = 0; i < RUN_TIMEOUT; i++)); do
		api GET "/runs/$id"
		st="$(field 'data["status"]')"
		case "$st" in
			success|partial|failed|cancelled|skipped|interrupted|waiting_user)
				printf '%s' "$st"
				return 0
				;;
		esac
		sleep 1
	done
	printf 'timeout'
}

# run_job_wait ID - run_job + wait_run; sets RID and ST.
ST=""
run_job_wait() {
	run_job "$1"
	ST="$(wait_run "$RID")"
}

# expect_status DESCRIPTION WANT... - checks ST against the wanted
# statuses; on a mismatch shows why the run ended as it did.
expect_status() {
	local what="$1" w
	shift
	for w in "$@"; do
		if [ "$ST" = "$w" ]; then
			pass "$what ($ST)"
			return 0
		fi
	done
	fail "$what (got $ST, want $*)"
	if [ -n "$RID" ]; then
		api GET "/runs/$RID"
		echo "    run $RID: error=$(field 'data["error_code"]') summary=$(field 'data["summary"]') guard=$(field 'data["guard"]')" >&2
	fi
}

files_in() { find "$1" -type f 2>/dev/null | wc -l | tr -d ' '; }

cleanup() {
	local rc=$?
	if [ "$KEEP" = yes ]; then
		echo
		echo "kept: $WORK and jobs ${JOBS_CREATED[*]:-none}"
		exit "$rc"
	fi
	local id
	for id in "${JOBS_CREATED[@]}"; do
		api DELETE "/jobs/$id"
	done
	rm -rf "$WORK"
	exit "$rc"
}
trap cleanup EXIT

# ---------------------------------------------------------------------------
section "1. Installed module"

api GET /health
if [ "$LAST_STATUS" != 200 ] || [ "$(field 'd.get("installed")')" != true ]; then
	fail "GET /health answers installed:true ($LAST_STATUS $LAST_BODY)"
	echo "Backup & Sync isn't answering at $API - nothing else can be checked." >&2
	exit 1
fi
pass "GET /health answers installed:true (version $(field 'd.get("version")'))"

api GET /capabilities
check "capabilities: engine available" test "$(field 'data["engine"]["available"]')" = true

if [ "$API" = "http://127.0.0.1:28643/v1/backup" ]; then
	check "nivaroos-backup.service is active" systemctl is-active --quiet nivaroos-backup.service
	check "the unit is enabled (starts at boot)" systemctl is-enabled --quiet nivaroos-backup.service
	check "state folder /var/lib/nivaroos/backup is 0700" test "$(stat -c %a /var/lib/nivaroos/backup 2>/dev/null)" = 700
	for d in logs staging; do
		check "state folder /var/lib/nivaroos/backup/$d exists" test -d "/var/lib/nivaroos/backup/$d"
	done
	check "/usr/bin/nivaroos-backup is installed" test -x /usr/bin/nivaroos-backup
else
	skip "unit and state folder checks (a test instance at $API)"
fi
if systemctl list-unit-files rclone.service 2>/dev/null | grep -q '^rclone.service'; then
	skip "rclone.service is still installed (spec §15.2 removes it; not part of this module)"
else
	pass "rclone.service is absent"
fi

if [ "$USE_GATEWAY" = yes ]; then
	gw_port="$(awk -F '=' '/^[[:space:]]*port[[:space:]]*=/ {gsub(/[[:space:]]/, "", $2); print $2}' /etc/nivaroos/gateway.ini 2>/dev/null || true)"
	gw_port="${gw_port:-80}"
	gw="http://127.0.0.1:${gw_port}/v1/backup"
	check "gateway: GET /v1/backup/health without a token" \
		sh -c "curl -fsS -m 10 '$gw/health' | grep -q '\"installed\":true'"
	# A browser tab (Origin header) never counts as local automation.
	code="$(curl -sS -m 10 -o /dev/null -w '%{http_code}' -H 'Origin: http://evil.example' "$gw/jobs" 2>/dev/null || true)"
	check "gateway: a browser request without a token is refused (got $code)" test "$code" = 401
	# The installer's update check (§8.4) - loopback automation through the gateway.
	code="$(curl -sS -m 10 -o /dev/null -w '%{http_code}' "$gw/runs?status=running" 2>/dev/null || true)"
	check "gateway: GET /runs?status=running as local automation (got $code)" test "$code" = 200
else
	skip "gateway route checks (--no-gateway)"
fi

# ---------------------------------------------------------------------------
section "2. Copy and Mirror, versions, restore with keep-both"

rm -rf "$WORK"
mkdir -p "$WORK/src/docs/sub" "$WORK/copy" "$WORK/mirror"
for i in $(seq 1 20); do
	printf 'file %s\n' "$i" > "$WORK/src/docs/f$i.txt"
done
printf 'nested\n' > "$WORK/src/docs/sub/nested.txt"

src_ep="$(endpoint_for "$WORK/src")" || { fail "resolve $WORK/src"; exit 1; }
copy_ep="$(endpoint_for "$WORK/copy")" || { fail "resolve $WORK/copy"; exit 1; }
mirror_ep="$(endpoint_for "$WORK/mirror")" || { fail "resolve $WORK/mirror"; exit 1; }
pass "resolve-path maps the test folders to endpoints"

create_job "$(job_json 'e2e: copy' copy "$src_ep" "$copy_ep")"
copy_id="$JOB_ID"
[ -n "$copy_id" ] || { fail "create the copy job"; exit 1; }
create_job "$(job_json 'e2e: mirror' mirror "$src_ep" "$mirror_ep")"
mirror_id="$JOB_ID"
[ -n "$mirror_id" ] || { fail "create the mirror job"; exit 1; }
pass "created jobs $copy_id (copy) and $mirror_id (mirror)"

run_job_wait "$copy_id"
expect_status "copy: first run succeeds" success
check "copy: 21 files at the destination" test "$(files_in "$WORK/copy/docs")" -eq 21
run_job_wait "$mirror_id"
expect_status "mirror: first run succeeds" success
check "mirror: 21 files at the destination" test "$(files_in "$WORK/mirror/docs")" -eq 21

rm -f "$WORK/src/docs/f1.txt" "$WORK/src/docs/f2.txt" "$WORK/src/docs/f3.txt" "$WORK/src/docs/f4.txt" "$WORK/src/docs/f5.txt"
run_job_wait "$mirror_id"
expect_status "mirror: run after deleting 5 files succeeds" success
check "mirror: the 5 files are gone from the mirror" test ! -e "$WORK/mirror/docs/f1.txt"
check "mirror: the 5 files are in .nivaro-versions" \
	test "$(find "$WORK/mirror/.nivaro-versions" -name 'f[1-5].txt' -type f 2>/dev/null | wc -l | tr -d ' ')" -eq 5
run_job_wait "$copy_id"
expect_status "copy: second run succeeds" success
check "copy: never deletes (f1.txt still there)" test -e "$WORK/copy/docs/f1.txt"

api GET "/jobs/$mirror_id/versions"
vid="$(field '[v["id"] for v in data if v.get("kind") == "recycle"][0]')"
check "versions list a recycle version ($vid)" test -n "$vid"

if [ -n "$vid" ]; then
	api GET "/jobs/$mirror_id/versions/$vid/browse?path="
	check "browse the version (HTTP $LAST_STATUS)" test "$LAST_STATUS" = 200
	# One restored file comes back where a newer one now sits: keep both.
	printf 'newer\n' > "$WORK/src/docs/f1.txt"
	body='{"version_id":"'"$vid"'","paths":["docs/f1.txt","docs/f2.txt","docs/f3.txt","docs/f4.txt","docs/f5.txt"],"target":{"mode":"original"},"conflict":"keep_both","dry_run":false}'
	api POST "/jobs/$mirror_id/restore" "$body"
	RID="$(field 'data["run_id"]')"
	if [ -n "$RID" ]; then
		ST="$(wait_run "$RID")"
		expect_status "restore run succeeds" success
		check "restore: f2..f5 are back in the source" test -e "$WORK/src/docs/f2.txt" -a -e "$WORK/src/docs/f5.txt"
		check "restore: the newer f1.txt is untouched" grep -qx newer "$WORK/src/docs/f1.txt"
		check "restore: keep-both added the old f1 next to it" \
			test "$(find "$WORK/src/docs" -maxdepth 1 -name 'f1*' -type f | wc -l | tr -d ' ')" -ge 2
	else
		fail "start a restore ($LAST_STATUS $LAST_BODY)"
	fi
fi

# ---------------------------------------------------------------------------
section "3. Delete guard"

# Put the source back to f1..f20 and sync the mirror to it (the keep-both
# copy from the restore goes; one file of 21 is under the guard).
find "$WORK/src/docs" -maxdepth 1 -type f -regextype posix-extended ! -regex '.*/f[0-9]+\.txt' -delete
printf 'file 1\n' > "$WORK/src/docs/f1.txt"
run_job_wait "$mirror_id"
if [ "$ST" = waiting_user ]; then
	api POST "/runs/$RID/decide" '{"proceed":true,"mode":"as_shown"}'
	ST="$(wait_run "$RID")"
fi
expect_status "mirror: back in sync with the source" success
before="$(files_in "$WORK/mirror/docs")"
for i in $(seq 2 2 20); do
	rm -f "$WORK/src/docs/f$i.txt"
done
run_job_wait "$mirror_id"
expect_status "removing half the source stops the run for a decision" waiting_user
if [ "$ST" = waiting_user ]; then
	api GET "/runs/$RID/preview"
	check "the preview answers (HTTP $LAST_STATUS)" test "$LAST_STATUS" = 200
	api POST "/runs/$RID/decide" '{"proceed":false}'
	ST="$(wait_run "$RID")"
	expect_status "declining ends the run" cancelled
	check "declining leaves the destination untouched" test "$(files_in "$WORK/mirror/docs")" -eq "$before"
fi

# ---------------------------------------------------------------------------
section "5. Missing destination with when_unmet=skip"

# A drive that isn't plugged in: a filesystem UUID nothing has.
ghost='{"kind":"volume","ref_id":"00000000-e2e0-4000-8000-000000000000","sub_path":"e2e","label":"e2e missing drive"}'
create_job "$(job_json 'e2e: missing drive' copy "$src_ep" "$ghost" 40 '{"dest_available":true,"when_unmet":"skip"}')"
skip_id="$JOB_ID"
if [ -n "$skip_id" ]; then
	run_job_wait "$skip_id"
	expect_status "a run whose destination is missing is skipped" skipped
else
	fail "create a job with a missing destination ($LAST_STATUS $LAST_BODY)"
fi

# ---------------------------------------------------------------------------
section "9. Scheduled Tasks import"

api GET /migration
state="$(field 'data["state"]')"
check "the import report answers (state: ${state:-?})" test "$LAST_STATUS" = 200
if [ "$WITH_MIGRATION" = yes ]; then
	api GET /jobs
	n_before="$(field 'len(data)')"
	api POST /migration/rerun '{}'
	check "re-running the import answers (HTTP $LAST_STATUS)" test "$LAST_STATUS" = 200
	api GET /jobs
	check "re-running the import creates no job ($n_before -> $(field 'len(data)'))" test "$(field 'len(data)')" = "$n_before"
else
	skip "re-run the import and check nothing changes (--migration)"
fi

# ---------------------------------------------------------------------------
section "10. Update while a backup runs"

api GET "/runs?status=running"
check "the installer's running-backup query answers (HTTP $LAST_STATUS)" test "$LAST_STATUS" = 200

# ---------------------------------------------------------------------------
section "Needs hardware, accounts or a throwaway VM (by hand)"
skip "1. fresh install on Debian 13, Ubuntu 24.04, Fedora, Raspberry Pi OS arm64: run this script after install.sh"
skip "4. USB: loop device + udev in a VM, volume_mounted trigger, min_gap_hours, unplug mid-run -> cancelled_unmounted, du of the empty mount dir stays 0"
skip "6. cloud: copy to Google Drive and to TeraBox (size-only, verify off, label shown)"
skip "7. SMB destination, cut the share mid-run; Cancel returns within 10 s"
skip "8. app hook (Immich/Blinko), kill the service mid-run: the app is started again, the run is interrupted then requeued"
skip "11. clock gate: NTP off, clock at 1970 - no cron fires for 10 min, the banner shows"
skip "12. UI: axe-core WCAG AA on every Backup window, dark and light, desktop/tablet/phone, keyboard-only job creation"

# ---------------------------------------------------------------------------
printf '\n%d passed, %d failed, %d skipped\n' "$PASS" "$FAIL" "$SKIP"
[ "$FAIL" -eq 0 ]
