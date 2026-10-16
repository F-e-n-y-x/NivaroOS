#!/usr/bin/env bash
# build-bundle.sh - build every NivaroOS server component into one bundle
# directory: the one build path for a from-source install (install.sh runs
# it on the box) and for a server release (release-server.sh, tarred and
# signed). The bundle's apply.sh then installs it - the one apply path for
# installs and updates (self-updater design, section 3.1).
#
#   installer/build-bundle.sh --out DIR [--version X.Y.Z] [--channel stable|beta]
#                             [--src REPO] [--without vm,download-station,backup]
#                             [--tar FILE.tar.gz]
#
# Layout of DIR:
#   MANIFEST      one line per fact (see below), read by apply.sh and nivaroos-cli
#   apply.sh      install | update | rollback | probation (installer/bundle/apply.sh)
#   lib/          safety-lib.sh (the bundle's own copy)
#   migrations/   NNNN-name.sh, expand-only, each run once per box
#   CHANGES.md    this release's changelog section (release builds)
#   root/         a sysroot: root/usr/bin/nivaroos is installed as /usr/bin/nivaroos
#
# MANIFEST lines (space separated, paths never contain spaces):
#   version|channel|commit|arch|min_from|glibc_min <value>
#   component <name> <always | installed-if-path> <unit,unit | ->
#   file <component> <mode> <sha256> <bundle-relative path>
#
# Builds are reproducible (-trimpath -buildvcs=false, the version lives in
# release.json, not in a binary), so a component that didn't change between
# two releases is byte-identical and apply.sh leaves its service running.
# Without --version (a from-source install) there is no release.json and
# core is stamped with its commit instead, as before.
set -Eeuo pipefail

SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="" VERSION="" CHANNEL="" WITHOUT="" TAR=""
MIN_FROM="${NIVAROOS_MIN_FROM:-0.0.0}"

die() { echo "build-bundle: $*" >&2; exit 1; }

while [ $# -gt 0 ]; do
	case "$1" in
		--out) OUT="${2:-}"; shift ;;
		--version) VERSION="${2#v}"; shift ;;
		--channel) CHANNEL="${2:-}"; shift ;;
		--src) SRC="${2:-}"; shift ;;
		--without) WITHOUT=",${2:-},"; shift ;;
		--tar) TAR="${2:-}"; shift ;;
		-h | --help) sed -n '2,30p' "$0"; exit 0 ;;
		*) die "unknown option $1" ;;
	esac
	shift
done
[ -n "$OUT" ] || die "--out DIR is required"
[ -f "$SRC/services/core/main.go" ] || die "$SRC is not a NivaroOS checkout"
if [ -n "$VERSION" ]; then
	[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]] || die "--version $VERSION is not X.Y.Z[-pre]"
	[ -n "$CHANNEL" ] || { case "$VERSION" in *-*) CHANNEL=beta ;; *) CHANNEL=stable ;; esac; }
fi
case "${CHANNEL:-stable}" in stable | beta) ;; *) die "--channel must be stable or beta" ;; esac
without() { [[ "$WITHOUT" == *",$1,"* ]]; }

case "$(uname -m)" in
	x86_64) ARCH=amd64 ;;
	aarch64 | arm64) ARCH=arm64 ;;
	*) ARCH="$(uname -m)" ;;
esac

export PATH="/usr/local/go/bin:$PATH" GOTOOLCHAIN=local GOWORK=off
export GOFLAGS='-trimpath -buildvcs=false "-ldflags=-s -w"'

rm -rf "$OUT"
mkdir -p "$OUT/root" "$OUT/lib" "$OUT/migrations"
OUT="$(cd "$OUT" && pwd)"
R="$OUT/root"
FILES="$OUT/.files"
: >"$FILES"
COMPONENTS=()

# put COMPONENT MODE SRC DST - stage SRC as DST (an absolute install path).
put() {
	[ -f "$3" ] || die "missing $3"
	case "$4" in *' '*) die "path with a space: $4" ;; esac
	install -D -m "$2" "$3" "$R$4"
	echo "$1 $2 root$4" >>"$FILES"
}
# put_tree COMPONENT SRC-DIR DST-DIR - every file under SRC-DIR.
put_tree() {
	local f rel mode
	[ -f "$2/index.html" ] || die "$2 has no index.html"
	while IFS= read -r -d '' f; do
		rel="${f#"$2"/}"
		mode=0644
		[ -x "$f" ] && mode=0755
		put "$1" "$mode" "$f" "$3/$rel"
	done < <(find "$2" -type f -print0 | sort -z)
}
# component NAME WHEN UNITS - WHEN: always, or the path whose presence
# means the box has it (optional add-ons are only updated where installed).
component() { COMPONENTS+=("component $1 $2 ${3:--}"); }

# gobuild COMPONENT MODULE-DIR BINARY [go build args...]
gobuild() {
	local comp="$1" dir="$2" bin="$3"
	shift 3
	echo "build: $bin"
	(cd "$SRC/$dir" && go build "$@" -o "$OUT/.bin/$bin" .)
	put "$comp" 0755 "$OUT/.bin/$bin" "/usr/bin/$bin"
}

unitsrc() { echo "$SRC/services/$1/build/sysroot/usr/lib/systemd/system/$2"; }
sysroot() { echo "$SRC/services/$1/build/sysroot"; }

# --- core services (always) -------------------------------------------------
vpkg=github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils/version
if [ -n "$VERSION" ]; then
	gobuild core services/core nivaroos
else
	# A from-source install has no release.json: core knows its build from
	# these (Settings > Updates compares it with GitHub master).
	commit="$(git -C "$SRC" rev-parse HEAD 2>/dev/null || true)"
	cdate="$(git -C "$SRC" log -1 --format=%cI 2>/dev/null || true)"
	gobuild core services/core nivaroos -ldflags "-s -w -X $vpkg.Commit=$commit -X $vpkg.Date=$cdate"
fi
put core 0644 "$(unitsrc core nivaroos.service)" /usr/lib/systemd/system/nivaroos.service
put core 0644 "$(sysroot core)/usr/share/nivaroos/shell/usb-mount@.service" /usr/lib/systemd/system/usb-mount@.service
put core 0644 "$(sysroot core)/etc/nivaroos/casaos.conf.sample" /etc/nivaroos/casaos.conf.sample
for sh in "$(sysroot core)"/usr/share/nivaroos/shell/*.sh; do
	put core 0755 "$sh" "/usr/share/nivaroos/shell/${sh##*/}"
done
component core always nivaroos.service

gobuild gateway services/gateway nivaroos-gateway
put gateway 0644 "$(unitsrc gateway nivaroos-gateway.service)" /usr/lib/systemd/system/nivaroos-gateway.service
put gateway 0644 "$(sysroot gateway)/etc/nivaroos/gateway.ini.sample" /etc/nivaroos/gateway.ini.sample
component gateway always nivaroos-gateway.service

gobuild message-bus services/message-bus nivaroos-message-bus
put message-bus 0644 "$(unitsrc message-bus nivaroos-message-bus.service)" /usr/lib/systemd/system/nivaroos-message-bus.service
put message-bus 0644 "$(sysroot message-bus)/etc/nivaroos/message-bus.conf.sample" /etc/nivaroos/message-bus.conf.sample
component message-bus always nivaroos-message-bus.service

gobuild app-management services/app-management nivaroos-app-management
put app-management 0644 "$(unitsrc app-management nivaroos-app-management.service)" /usr/lib/systemd/system/nivaroos-app-management.service
put app-management 0644 "$(sysroot app-management)/etc/nivaroos/app-management.conf.sample" /etc/nivaroos/app-management.conf.sample
component app-management always nivaroos-app-management.service

gobuild local-storage services/local-storage nivaroos-local-storage
put local-storage 0644 "$(unitsrc local-storage nivaroos-local-storage.service)" /usr/lib/systemd/system/nivaroos-local-storage.service
put local-storage 0644 "$(sysroot local-storage)/etc/nivaroos/local-storage.conf.sample" /etc/nivaroos/local-storage.conf.sample
put local-storage 0755 "$(sysroot local-storage)/usr/share/nivaroos/shell/local-storage-helper.sh" /usr/share/nivaroos/shell/local-storage-helper.sh
component local-storage always nivaroos-local-storage.service

# The binary is nivaroos-user; the unit keeps its -service name.
gobuild user services/user nivaroos-user
put user 0644 "$(unitsrc user nivaroos-user-service.service)" /usr/lib/systemd/system/nivaroos-user-service.service
put user 0644 "$(sysroot user)/etc/nivaroos/user-service.conf.sample" /etc/nivaroos/user-service.conf.sample
component user always nivaroos-user-service.service

gobuild gpu-sidecar services/gpu-sidecar nivaroos-gpu-sidecar
put gpu-sidecar 0644 "$(unitsrc gpu-sidecar nivaroos-gpu-sidecar.service)" /usr/lib/systemd/system/nivaroos-gpu-sidecar.service
component gpu-sidecar always nivaroos-gpu-sidecar.service

# Fan control: cgo for NVIDIA fans through NVML (loaded at run time); a
# pure-Go build (motherboard/AMD fans only) without a C compiler.
echo "build: nivaroos-fans"
if ! (cd "$SRC/services/fans" && CGO_ENABLED=1 go build -o "$OUT/.bin/nivaroos-fans" .); then
	echo "build-bundle: fans cgo build failed - building without NVIDIA support" >&2
	(cd "$SRC/services/fans" && CGO_ENABLED=0 go build -o "$OUT/.bin/nivaroos-fans" .)
fi
put fans 0755 "$OUT/.bin/nivaroos-fans" /usr/bin/nivaroos-fans
put fans 0644 "$(unitsrc fans nivaroos-fans.service)" /usr/lib/systemd/system/nivaroos-fans.service
component fans always nivaroos-fans.service

# The CLI (also `nivaroos update`); apply.sh links nivaroos and casaos-cli to it.
gobuild cli cli nivaroos-cli
component cli always -

# --- safety net, update units, scripts (always) -----------------------------
I="$SRC/installer"
put safety 0644 "$I/nivaroos-safety-lib.sh" /usr/local/lib/nivaroos/safety-lib.sh
put safety 0755 "$I/nivaroos-watchdog.sh" /usr/local/lib/nivaroos/nivaroos-watchdog
put safety 0755 "$I/nivaroos-rollback.sh" /usr/local/bin/nivaroos-rollback
put safety 0755 "$I/nivaroos-deploy.sh" /usr/local/bin/nivaroos-deploy
put safety 0755 "$I/nivaroos-recover.sh" /usr/local/bin/nivaroos-recover
put safety 0755 "$I/uninstall.sh" /usr/bin/nivaroos-uninstall
put safety 0755 "$I/gpu-driver-install.sh" /usr/local/bin/nivaroos-gpu-driver-install.sh
put safety 0755 "$I/nivaroos-host-firewall.sh" /usr/local/lib/nivaroos/nivaroos-host-firewall
put safety 0644 "$I/systemd/10-nivaroos-resilience.conf" /usr/local/lib/nivaroos/10-nivaroos-resilience.conf
for u in nivaroos-watchdog.service nivaroos-watchdog.timer nivaroos-host-firewall.service \
	nivaroos-update.service nivaroos-update-check.timer nivaroos-update-resume.service; do
	put safety 0644 "$I/systemd/$u" "/usr/lib/systemd/system/$u"
done
# Restarted when changed: the timers reschedule, the firewall re-applies.
# nivaroos-update.service is never started here (it may be what runs us).
component safety always nivaroos-watchdog.timer,nivaroos-update-check.timer,nivaroos-host-firewall.service

# --- optional add-ons (updated only where installed) ------------------------
if ! without vm; then
	# libvirt_dlopen: libvirt is loaded at run time, so the binary has no
	# link-time dependency on one libvirt ABI.
	gobuild vm services/vm-sidecar nivaroos-vm-sidecar -tags libvirt_dlopen
	put vm 0644 "$(unitsrc vm-sidecar nivaroos-vm-sidecar.service)" /usr/lib/systemd/system/nivaroos-vm-sidecar.service
	put vm 0755 "$I/nivaroos-bridge.sh" /usr/local/bin/nivaroos-bridge
	component vm /usr/bin/nivaroos-vm-sidecar nivaroos-vm-sidecar.service
fi
if ! without download-station; then
	gobuild download-station services/download-sidecar nivaroos-download-sidecar
	put download-station 0644 "$(unitsrc download-sidecar nivaroos-download-sidecar.service)" /usr/lib/systemd/system/nivaroos-download-sidecar.service
	put download-station 0644 "$(unitsrc download-sidecar nivaroos-torrent.service)" /usr/lib/systemd/system/nivaroos-torrent.service
	component download-station /usr/bin/nivaroos-download-sidecar nivaroos-download-sidecar.service
fi
if ! without backup; then
	gobuild backup services/backup nivaroos-backup
	put backup 0644 "$(unitsrc backup nivaroos-backup.service)" /usr/lib/systemd/system/nivaroos-backup.service
	# restic for versioned backups (job type "backup"), pinned and built
	# from source, verified by the Go checksum database. The engine looks
	# for it at /usr/lib/nivaroos/bin/restic (engine.ResticPath).
	RESTIC_VERSION=v0.19.1
	echo "build: restic $RESTIC_VERSION"
	(cd "$OUT" && CGO_ENABLED=0 GOBIN="$OUT/.bin" go install "github.com/restic/restic/cmd/restic@$RESTIC_VERSION")
	put backup 0755 "$OUT/.bin/restic" /usr/lib/nivaroos/bin/restic
	component backup /usr/bin/nivaroos-backup nivaroos-backup.service
fi

# --- web dashboard ------------------------------------------------------------
WWW="$SRC/ui/build/sysroot/var/lib/nivaroos/www"
if [ -z "${NIVAROOS_PREBUILT_WWW:-}" ]; then
	echo "build: web dashboard"
	(cd "$SRC/ui" && { pnpm install --frozen-lockfile --prefer-offline || pnpm install; } && pnpm run build)
else
	WWW="$NIVAROOS_PREBUILT_WWW"
fi
put_tree ui "$WWW" /var/lib/nivaroos/www
component ui always -

# --- release identity -----------------------------------------------------------
COMMIT="$(git -C "$SRC" rev-parse HEAD 2>/dev/null || echo unknown)"
if [ -n "$VERSION" ]; then
	printf '{"version":"%s","channel":"%s","commit":"%s","built":"%s"}\n' \
		"$VERSION" "$CHANNEL" "$COMMIT" "$(git -C "$SRC" log -1 --format=%cI 2>/dev/null || date -u +%FT%TZ)" >"$OUT/.release.json"
	put release 0644 "$OUT/.release.json" /usr/share/nivaroos/release.json
	# This release's section of CHANGELOG.md: "## [server X.Y.Z] - date" up
	# to the next "## " (server and app versions are numbered apart).
	awk -v h="## [server $VERSION]" '/^## / { p = (index($0, h) == 1) } p' "$SRC/CHANGELOG.md" >"$OUT/CHANGES.md" || true
	[ -s "$OUT/CHANGES.md" ] || echo "NivaroOS server $VERSION" >"$OUT/CHANGES.md"
	echo "bundle 0644 CHANGES.md" >>"$FILES"
fi
component release always -

# --- the apply machinery --------------------------------------------------------
install -m 0755 "$I/bundle/apply.sh" "$OUT/apply.sh"
install -m 0644 "$I/nivaroos-safety-lib.sh" "$OUT/lib/safety-lib.sh"
echo "bundle 0755 apply.sh" >>"$FILES"
echo "bundle 0644 lib/safety-lib.sh" >>"$FILES"
for m in "$I"/migrations/[0-9][0-9][0-9][0-9]-*.sh; do
	[ -f "$m" ] || continue
	install -m 0755 "$m" "$OUT/migrations/${m##*/}"
	echo "bundle 0755 migrations/${m##*/}" >>"$FILES"
done

# glibc_min: the newest GLIBC_x.y symbol version any binary needs (cgo ones).
GLIBC_MIN="$(for b in "$OUT"/.bin/*; do objdump -T "$b" 2>/dev/null; done |
	grep -o 'GLIBC_[0-9][0-9.]*' | sed 's/GLIBC_//' | sort -V | tail -n1 || true)"

{
	echo "version ${VERSION:-source}"
	echo "channel ${CHANNEL:-source}"
	echo "commit $COMMIT"
	echo "arch $ARCH"
	echo "min_from $MIN_FROM"
	echo "glibc_min ${GLIBC_MIN:-0}"
	printf '%s\n' "${COMPONENTS[@]}"
	while read -r comp mode rel; do
		echo "file $comp $mode $(sha256sum "$OUT/$rel" | cut -d' ' -f1) $rel"
	done <"$FILES"
} >"$OUT/MANIFEST"
rm -rf "$OUT/.bin" "$OUT/.files" "$OUT/.release.json"

if [ -n "$TAR" ]; then
	top="nivaroos-server-${VERSION:-source}-linux-$ARCH"
	# Fixed owner, order and times: the same tree gives the same tarball.
	tar --sort=name --owner=0 --group=0 --numeric-owner --mtime='2000-01-01 00:00Z' \
		--transform "s,^\.,$top," -C "$OUT" -cf - . | gzip -n >"$TAR"
	echo "bundle: $TAR"
fi
echo "bundle: $OUT ($(grep -c '^file ' "$OUT/MANIFEST") files, version ${VERSION:-source}, glibc >= ${GLIBC_MIN:-?})"
