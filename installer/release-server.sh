#!/usr/bin/env bash
# release-server.sh - build, sign and publish a NivaroOS server release.
# Run on the release server (the only place the signing key exists), from
# a clean checkout of the commit to release. docs/guides/server-updates.md.
#
#   installer/release-server.sh 1.5.0            stable: GitHub release server-v1.5.0
#   installer/release-server.sh 1.6.0-beta.1     beta: a GitHub pre-release
#   installer/release-server.sh 1.5.0 --dry-run  everything but the tag and the upload
#
# Steps: build the bundle (installer/build-bundle.sh, this machine's arch),
# SHA256SUMS, sign it with minisign (legacy -l signature, trusted comment
# "nivaroos-server <version> <channel>"), check the result with the very
# verifier the boxes run (cli/update TestReleaseBundle, compiled-in key),
# apply it to a throwaway root, then tag server-vX.Y.Z and publish the
# tarball + SHA256SUMS + SHA256SUMS.minisig with gh (never as GitHub's
# "Latest" release: that stays the Android app's).
#
# Environment: NIVAROOS_RELEASE_KEY (default /root/.nivaroos-release/
# nivaroos-server.key), NIVAROOS_RELEASE_OUT (default
# /root/.nivaroos-release/out), NIVAROOS_MIN_FROM (MANIFEST min_from).
set -Eeuo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
KEY="${NIVAROOS_RELEASE_KEY:-/root/.nivaroos-release/nivaroos-server.key}"
VERSION="${1:-}"
DRY=""
[ "${2:-}" = --dry-run ] && DRY=yes
die() { echo "release-server: $*" >&2; exit 1; }

[[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$ ]] || { sed -n '2,24p' "$0"; exit 2; }
case "$VERSION" in *-*) CHANNEL=beta ;; *) CHANNEL=stable ;; esac
TAG="server-v$VERSION"
OUT="${NIVAROOS_RELEASE_OUT:-/root/.nivaroos-release/out}/$TAG"
export PATH="/usr/local/go/bin:$PATH" GOTOOLCHAIN=local GOWORK=off

for c in minisign gh git sha256sum go pnpm; do command -v "$c" >/dev/null || die "$c is not installed"; done
[ -f "$KEY" ] || die "no signing key at $KEY"
[ -z "$(git -C "$REPO" status --porcelain)" ] || die "$REPO has uncommitted changes - release a clean checkout"
if [ -z "$DRY" ]; then
	git -C "$REPO" fetch -q origin
	[ "$(git -C "$REPO" rev-parse HEAD)" = "$(git -C "$REPO" rev-parse origin/master)" ] ||
		echo "release-server: note - HEAD is not origin/master ($(git -C "$REPO" rev-parse --short HEAD))" >&2
	! git -C "$REPO" ls-remote --exit-code --tags origin "refs/tags/$TAG" >/dev/null || die "$TAG already exists"
fi

# The key must be the one the boxes trust (cli/update/nivaroos-server.pub).
mkdir -p "$OUT"
chmod 700 "$(dirname "$OUT")"
rm -f "$OUT/key.pub"
minisign -R -s "$KEY" -p "$OUT/key.pub" >/dev/null
[ "$(tail -n1 "$OUT/key.pub")" = "$(tail -n1 "$REPO/cli/update/nivaroos-server.pub")" ] ||
	die "$KEY is not the key compiled into nivaroos-cli (cli/update/nivaroos-server.pub)"
rm -f "$OUT/key.pub"

case "$(uname -m)" in x86_64) ARCH=amd64 ;; aarch64 | arm64) ARCH=arm64 ;; *) die "unsupported arch $(uname -m)" ;; esac
TARBALL="$OUT/nivaroos-server-$VERSION-linux-$ARCH.tar.gz"
echo "== building $TAG ($CHANNEL, $ARCH) from $(git -C "$REPO" rev-parse --short HEAD)"
bash "$REPO/installer/build-bundle.sh" --src "$REPO" --version "$VERSION" --channel "$CHANNEL" --out "$OUT/bundle" --tar "$TARBALL"

grep -q "^## \[server $VERSION\]" "$REPO/CHANGELOG.md" ||
	echo "release-server: note - CHANGELOG.md has no '## [server $VERSION]' section; the release notes say only the version" >&2

echo "== signing"
(cd "$OUT" && sha256sum "$(basename "$TARBALL")" >SHA256SUMS)
rm -f "$OUT/SHA256SUMS.minisig"
minisign -S -l -s "$KEY" -m "$OUT/SHA256SUMS" -x "$OUT/SHA256SUMS.minisig" \
	-c "NivaroOS server $VERSION" -t "nivaroos-server $VERSION $CHANNEL" >/dev/null

echo "== verifying with the boxes' own verifier"
(cd "$REPO/cli" && NIVAROOS_RELEASE_DIR="$OUT" NIVAROOS_RELEASE_VERSION="$VERSION" NIVAROOS_RELEASE_CHANNEL="$CHANNEL" \
	go test -count=1 -run '^TestReleaseBundle$' ./update/)

echo "== applying to a throwaway root"
T="$(mktemp -d "$OUT/smoke.XXXXXX")"
trap 'rm -rf "$T"' EXIT
# shellcheck disable=SC2016 # the fake's own $1/$3
printf '#!/bin/sh\ncase "$1" in show) case "$3" in ActiveState) echo active ;; SubState) echo running ;; NRestarts) echo 0 ;; esac ;; is-active) exit 1 ;; esac\nexit 0\n' >"$T/systemctl"
printf '#!/bin/sh\nprintf 200\n' >"$T/curl"
chmod +x "$T/systemctl" "$T/curl"
NV_ROOT="$T/root" NIVAROOS_SYSTEMCTL="$T/systemctl" NIVAROOS_CURL="$T/curl" NIVAROOS_NO_SYSLOG=1 NIVAROOS_DEPLOY_SETTLE=0 \
	bash "$OUT/bundle/apply.sh" install --with vm,download-station,backup >"$T/apply.log" 2>&1 ||
	{ cat "$T/apply.log" >&2; die "the bundle does not apply cleanly"; }
grep -q "\"version\":\"$VERSION\"" "$T/root/usr/share/nivaroos/release.json" || die "release.json is not $VERSION"

if [ -n "$DRY" ]; then
	echo "== dry run: $OUT (not tagged, not published)"
	exit 0
fi

echo "== publishing $TAG"
git -C "$REPO" tag -a "$TAG" -m "NivaroOS server $VERSION"
git -C "$REPO" push -q origin "refs/tags/$TAG"
pre=()
[ "$CHANNEL" = beta ] && pre=(--prerelease)
gh release create "$TAG" --repo F-e-n-y-x/NivaroOS --verify-tag --latest=false "${pre[@]}" \
	--title "NivaroOS server $VERSION" --notes-file "$OUT/bundle/CHANGES.md" \
	"$TARBALL" "$OUT/SHA256SUMS" "$OUT/SHA256SUMS.minisig"
echo "== published: https://github.com/F-e-n-y-x/NivaroOS/releases/tag/$TAG"
