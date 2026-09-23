#!/bin/sh
# NivaroOS Guest Tools for Linux.
#
# Linux already has the VirtIO drivers (virtio-net/blk/scsi/gpu and the
# virtiofs file system are in every mainstream kernel), so this only:
#   1. installs the QEMU guest agent (clean shutdown, IP reporting, and it
#      lets NivaroOS run future setups for you automatically),
#   2. mounts the NivaroOS shared folder (/DATA/VMs/share on the server,
#      tag "share") at /mnt/nivaroos-share, now and on every boot,
#   3. links it into the desktop user's home as ~/NivaroOS-Share.
# Safe to run again. Run as root (it re-runs itself with sudo if needed).
set -u

TAG=share
MNT=/mnt/nivaroos-share

say() { printf '%s\n' "$*"; }
warn() { printf '[!] %s\n' "$*"; FAILED=1; }
FAILED=0

if [ "$(id -u)" -ne 0 ]; then
	if command -v sudo >/dev/null 2>&1; then exec sudo sh "$0" "$@"; fi
	say "Please run this as root."
	exit 1
fi

say "=================================================================="
say "  NivaroOS Guest Tools - shared folder and guest agent"
say "=================================================================="

# --- 1. QEMU guest agent (best effort: needs the distro's repositories)
say "[1/3] Installing the QEMU guest agent..."
if command -v qemu-ga >/dev/null 2>&1 || [ -x /usr/bin/qemu-ga ] || [ -x /usr/sbin/qemu-ga ]; then
	say "      already installed"
elif command -v apt-get >/dev/null 2>&1; then
	DEBIAN_FRONTEND=noninteractive apt-get install -y qemu-guest-agent >/dev/null 2>&1 ||
		{ apt-get update >/dev/null 2>&1 && DEBIAN_FRONTEND=noninteractive apt-get install -y qemu-guest-agent >/dev/null 2>&1; } ||
		warn "could not install qemu-guest-agent (no internet in the VM?) - the shared folder still works"
elif command -v dnf >/dev/null 2>&1; then
	dnf install -y qemu-guest-agent >/dev/null 2>&1 || warn "could not install qemu-guest-agent"
elif command -v zypper >/dev/null 2>&1; then
	zypper --non-interactive install qemu-guest-agent >/dev/null 2>&1 || warn "could not install qemu-guest-agent"
elif command -v pacman >/dev/null 2>&1; then
	pacman -S --noconfirm qemu-guest-agent >/dev/null 2>&1 || warn "could not install qemu-guest-agent"
elif command -v apk >/dev/null 2>&1; then
	apk add qemu-guest-agent >/dev/null 2>&1 || warn "could not install qemu-guest-agent"
else
	say "      no known package manager - skipped"
fi
if command -v systemctl >/dev/null 2>&1; then
	systemctl enable --now qemu-guest-agent >/dev/null 2>&1 || true
fi

# --- 2. shared folder
say "[2/3] Mounting the NivaroOS shared folder at $MNT..."
grep -qw virtiofs /proc/filesystems 2>/dev/null || modprobe virtiofs 2>/dev/null || true
if ! grep -qw virtiofs /proc/filesystems 2>/dev/null; then
	warn "this kernel has no virtiofs support (needs Linux 5.4 or newer)"
else
	mkdir -p "$MNT"
	if ! grep -qE "^[[:space:]]*$TAG[[:space:]]+$MNT[[:space:]]" /etc/fstab 2>/dev/null; then
		# nofail: the VM still boots if the share is ever missing.
		if printf '%s\n' "$TAG $MNT virtiofs defaults,nofail 0 0" >>/etc/fstab 2>/dev/null; then
			say "      added to /etc/fstab (mounted on every boot)"
		else
			say "      /etc/fstab is read-only here - mounting for this session only"
		fi
	fi
	if mountpoint -q "$MNT" 2>/dev/null || grep -q " $MNT virtiofs " /proc/mounts; then
		say "      already mounted"
	elif mount -t virtiofs "$TAG" "$MNT" 2>/tmp/nivaroos-mount.err; then
		say "      mounted"
	else
		warn "mount failed: $(cat /tmp/nivaroos-mount.err 2>/dev/null)"
		say "    Usually the VM has no shared-folder device yet: shut it down,"
		say "    start it again from NivaroOS, then run this script once more."
	fi
fi

# --- 3. shortcut in the desktop user's home
say "[3/3] Adding a ~/NivaroOS-Share shortcut..."
U="${SUDO_USER:-}"
[ -z "$U" ] && U=$(awk -F: '$3>=1000 && $3<60000 {print $1; exit}' /etc/passwd)
if [ -n "$U" ]; then
	H=$(awk -F: -v u="$U" '$1==u {print $6}' /etc/passwd)
	if [ -n "$H" ] && [ -d "$H" ] && [ ! -e "$H/NivaroOS-Share" ]; then
		ln -s "$MNT" "$H/NivaroOS-Share" && chown -h "$U" "$H/NivaroOS-Share" 2>/dev/null
	fi
	say "      $H/NivaroOS-Share"
fi

say ""
if [ "$FAILED" -ne 0 ]; then
	say "Finished with problems - see the [!] lines above."
	exit 1
fi
say "Done. The NivaroOS shared folder is at $MNT (and ~/NivaroOS-Share)."
exit 0
