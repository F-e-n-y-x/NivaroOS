#!/bin/bash
# ==============================================================================
#  NivaroOS Host Desktop - x11vnc wrapper
#
#  SINGLE SOURCE OF TRUTH. This file is embedded into nivaroos-vm-sidecar
#  (services/vm-sidecar/hostdesktop_install.go, go:embed) and written to
#  /usr/local/bin/nivaroos-host-desktop.sh by the sidecar's install logic -
#  which installer/install.sh (`nivaroos-vm-sidecar install-host-desktop`),
#  `nivaroos host-desktop enable` and the dashboard's Install button all
#  call. Never copy this text anywhere else; edit it here.
#
#  What it does:
#    1. Finds the X11 display actually in use (active seat0 login session,
#       or the running Xorg's own ":N" / -auth arguments - works for GDM,
#       SDDM, LightDM, XDM and plain startx, not just LightDM's :0).
#    2. On a headless box whose display manager could not start Xorg
#       (no monitor attached), writes ONE Xorg Device section for the
#       driver actually bound to the primary GPU and restarts the display
#       manager - at most once per boot, and never while anybody has an
#       active graphical session.
#    3. Runs x11vnc on a root-only unix socket (no TCP port at all). The
#       only way in is vm-sidecar's authenticated /host/console WebSocket.
#
#  Usage:
#    nivaroos-host-desktop.sh            run (what the systemd unit does)
#    nivaroos-host-desktop.sh --resolve  print the detected display as
#                                        KEY=value lines and exit
# ==============================================================================
set -u
umask 022

RUN_DIR=/run/nivaroos
SOCK="$RUN_DIR/hostvnc.sock"
STATE_ENV="$RUN_DIR/host-desktop.env"
STATE_XAUTH="$RUN_DIR/host-desktop.xauth"
CONF=/etc/nivaroos/host-desktop.conf
LIB_DIR=/var/lib/nivaroos
DM_RESTART_MARKER="$LIB_DIR/host-desktop-dm-restart"
HEADLESS_MARKER="$LIB_DIR/host-desktop-headless-conf"
XORG_HEADLESS_CONF=/etc/X11/xorg.conf.d/10-nivaroos-headless.conf

# ------------------------------------------------------------------------------
# Settings (written by vm-sidecar's PUT /host/desktop/settings). Parsed, not
# sourced, so a malformed file can never run code as root.
#   FIXSCREEN=<seconds>  periodic full re-read of the framebuffer (0 = off)
#   NOXDAMAGE=0|1        ignore the X DAMAGE extension (poll instead)
# ------------------------------------------------------------------------------
FIXSCREEN=0
NOXDAMAGE=1
if [ -r "$CONF" ]; then
	while IFS='=' read -r key val; do
		case "$key" in
			FIXSCREEN) [[ "$val" =~ ^[0-9]{1,4}$ ]] && FIXSCREEN="$val" ;;
			NOXDAMAGE) [[ "$val" =~ ^[01]$ ]] && NOXDAMAGE="$val" ;;
		esac
	done < "$CONF"
fi

# ------------------------------------------------------------------------------
# Display resolution
# ------------------------------------------------------------------------------
R_DISPLAY=""
R_AUTH=""
R_TYPE=""
R_DESKTOP=""
R_USER=""
R_UID=""
R_XWAYLAND=0

# active_seat_session prints the seat0 active session id (empty if none).
active_seat_session() {
	command -v loginctl >/dev/null 2>&1 || return 0
	loginctl show-seat seat0 -p ActiveSession --value 2>/dev/null || true
}

session_prop() {
	loginctl show-session "$1" -p "$2" --value 2>/dev/null || true
}

# list_xorg_servers prints "<display> <auth-file-or-empty>" for every running
# real X server (Xorg/X - never Xwayland, which only exists inside a Wayland
# session and cannot be captured by x11vnc).
list_xorg_servers() {
	local p exe base args disp auth i
	for p in /proc/[0-9]*; do
		[ -r "$p/cmdline" ] || continue
		mapfile -d '' -t args < "$p/cmdline" 2>/dev/null || continue
		[ "${#args[@]}" -gt 0 ] || continue
		exe="${args[0]}"
		base="${exe##*/}"
		case "$base" in
			Xorg|X|Xorg.bin|Xorg.wrap) ;;
			*) continue ;;
		esac
		disp=""
		auth=""
		for ((i = 1; i < ${#args[@]}; i++)); do
			case "${args[$i]}" in
				:[0-9]*) [ -z "$disp" ] && disp="${args[$i]}" ;;
				-auth) auth="${args[$((i + 1))]:-}" ;;
			esac
		done
		[ -n "$disp" ] || disp=":0"
		printf '%s %s\n' "$disp" "$auth"
	done | sort -u
}

display_socket_exists() {
	local n="${1#:}"
	n="${n%%.*}"
	[ -S "/tmp/.X11-unix/X${n}" ]
}

# guess_auth_for <display> <uid> <user> - well-known cookie locations for
# display managers / startx that didn't show up as an -auth argument.
guess_auth_for() {
	local d="$1" uid="$2" user="$3" f home
	for f in "/run/lightdm/root/$d" "/var/run/lightdm/root/$d" \
		${uid:+"/run/user/$uid/gdm/Xauthority"} \
		/run/sddm/* /var/run/sddm/*; do
		[ -f "$f" ] && { printf '%s\n' "$f"; return 0; }
	done
	if [ -n "$user" ]; then
		home="$(getent passwd "$user" 2>/dev/null | cut -d: -f6)"
		[ -n "$home" ] && [ -f "$home/.Xauthority" ] && { printf '%s\n' "$home/.Xauthority"; return 0; }
	fi
	[ -f /root/.Xauthority ] && { printf '%s\n' /root/.Xauthority; return 0; }
	for f in /home/*/.Xauthority; do
		[ -f "$f" ] && { printf '%s\n' "$f"; return 0; }
	done
	return 0
}

is_xwayland_display() {
	command -v xdpyinfo >/dev/null 2>&1 || return 1
	local out
	if [ -n "$2" ]; then
		out="$(DISPLAY="$1" XAUTHORITY="$2" timeout 5 xdpyinfo 2>/dev/null)" || return 1
	else
		out="$(DISPLAY="$1" timeout 5 xdpyinfo 2>/dev/null)" || return 1
	fi
	printf '%s' "$out" | grep -q 'XWAYLAND'
}

resolve_display() {
	R_DISPLAY=""
	R_AUTH=""
	R_TYPE=""
	R_DESKTOP=""
	R_USER=""
	R_UID=""
	R_XWAYLAND=0

	local sid sdisp=""
	sid="$(active_seat_session)"
	if [ -n "$sid" ]; then
		R_TYPE="$(session_prop "$sid" Type)"
		R_DESKTOP="$(session_prop "$sid" Desktop)"
		R_USER="$(session_prop "$sid" Name)"
		R_UID="$(session_prop "$sid" User)"
		sdisp="$(session_prop "$sid" Display)"
	fi

	local servers line d a first_d="" first_a=""
	servers="$(list_xorg_servers)"
	while IFS= read -r line; do
		[ -n "$line" ] || continue
		d="${line%% *}"
		a="${line#* }"
		[ "$a" = "$line" ] && a=""
		if [ -z "$first_d" ]; then first_d="$d"; first_a="$a"; fi
		if [ -n "$sdisp" ] && [ "$d" = "$sdisp" ]; then
			R_DISPLAY="$d"
			R_AUTH="$a"
		fi
	done <<< "$servers"

	# No Xorg matched the active session (e.g. the active session is
	# Wayland, or startx from a tty session): use the first real Xorg that
	# is running - a GDM/SDDM greeter, a startx session, a second seat...
	if [ -z "$R_DISPLAY" ] && [ -n "$first_d" ] && [ "$R_TYPE" != "wayland" ]; then
		R_DISPLAY="$first_d"
		R_AUTH="$first_a"
	fi

	# An X11 session the process scan couldn't see (hidepid=/proc) but
	# logind knows about.
	if [ -z "$R_DISPLAY" ] && [ "$R_TYPE" = "x11" ] && [ -n "$sdisp" ]; then
		R_DISPLAY="$sdisp"
	fi

	[ -n "$R_DISPLAY" ] || return 0

	if [ -z "$R_AUTH" ] || [ ! -f "$R_AUTH" ]; then
		R_AUTH="$(guess_auth_for "$R_DISPLAY" "$R_UID" "$R_USER")"
	fi

	if is_xwayland_display "$R_DISPLAY" "$R_AUTH"; then
		R_XWAYLAND=1
	fi
}

x_reachable() {
	display_socket_exists "$R_DISPLAY" || return 1
	command -v xdpyinfo >/dev/null 2>&1 || return 0
	if [ -n "$R_AUTH" ]; then
		DISPLAY="$R_DISPLAY" XAUTHORITY="$R_AUTH" timeout 5 xdpyinfo >/dev/null 2>&1
	else
		DISPLAY="$R_DISPLAY" timeout 5 xdpyinfo >/dev/null 2>&1
	fi
}

# write_state <state> <reason> - what vm-sidecar reads for GET
# /host/desktop/status and for its own xrandr/xset calls (it runs with
# ProtectHome, so it cannot read /run/user/* or /home/* cookies itself -
# hence the root-only copy of the cookie below).
write_state() {
	mkdir -p "$RUN_DIR"
	local tmp="$STATE_ENV.tmp.$$" xauth=""
	if [ "$1" = "running" ] && [ -n "$R_AUTH" ] && [ -f "$R_AUTH" ]; then
		if (umask 077 && cp -f "$R_AUTH" "$STATE_XAUTH.tmp.$$") 2>/dev/null; then
			mv -f "$STATE_XAUTH.tmp.$$" "$STATE_XAUTH"
			xauth="$STATE_XAUTH"
		fi
	fi
	(
		umask 077
		{
			printf 'STATE=%s\n' "$1"
			printf 'REASON=%s\n' "${2//$'\n'/ }"
			printf 'DISPLAY=%s\n' "$R_DISPLAY"
			printf 'XAUTHORITY=%s\n' "$xauth"
			printf 'SESSION_TYPE=%s\n' "$R_TYPE"
			printf 'DESKTOP=%s\n' "$R_DESKTOP"
			printf 'XWAYLAND=%s\n' "$R_XWAYLAND"
		} > "$tmp"
	) && mv -f "$tmp" "$STATE_ENV"
}

# ------------------------------------------------------------------------------
# Headless (no monitor) support
# ------------------------------------------------------------------------------

# primary_card prints the /sys/class/drm/cardN of the boot VGA device.
primary_card() {
	local c first=""
	for c in /sys/class/drm/card[0-9]*; do
		case "${c##*/}" in *-*) continue ;; esac
		[ -e "$c/device" ] || continue
		[ -n "$first" ] || first="$c"
		if [ "$(cat "$c/device/boot_vga" 2>/dev/null)" = "1" ]; then
			printf '%s\n' "$c"
			return 0
		fi
	done
	[ -n "$first" ] && printf '%s\n' "$first"
	return 0
}

# monitor_state <card> -> "connected", "none" (KMS connectors exist, none
# connected) or "unknown" (no KMS connector list - e.g. NVIDIA without
# nvidia-drm.modeset=1 - so we genuinely cannot tell and must not guess).
monitor_state() {
	local card="$1" f found=0
	for f in "$card"-*/status; do
		[ -f "$f" ] || continue
		found=1
		if [ "$(cat "$f" 2>/dev/null)" = "connected" ]; then
			echo connected
			return 0
		fi
	done
	if [ "$found" = 1 ]; then echo none; else echo unknown; fi
}

xorg_driver_exists() {
	local d
	for d in /usr/lib/xorg/modules/drivers /usr/lib64/xorg/modules/drivers /usr/lib/x86_64-linux-gnu/xorg/modules/drivers /usr/lib/aarch64-linux-gnu/xorg/modules/drivers; do
		[ -f "$d/${1}_drv.so" ] && return 0
	done
	return 1
}

# xorg_driver_for <kernel-driver> - the Xorg DDX to name in the Device
# section. modesetting works on every KMS driver; the vendor DDX is only
# used where it's what Xorg would pick anyway (NVIDIA's proprietary one
# has no modesetting fallback at all).
xorg_driver_for() {
	case "$1" in
		nvidia) echo nvidia ;;
		amdgpu) if xorg_driver_exists amdgpu; then echo amdgpu; else echo modesetting; fi ;;
		radeon) if xorg_driver_exists radeon; then echo radeon; else echo modesetting; fi ;;
		nouveau) if xorg_driver_exists nouveau; then echo nouveau; else echo modesetting; fi ;;
		*) echo modesetting ;;
	esac
}

# pci_busid <card> -> "PCI:bus:dev:func" (decimal, domain via @ when != 0).
pci_busid() {
	local addr dom bus dev fn
	addr="$(basename "$(readlink -f "$1/device")")"
	[[ "$addr" =~ ^([0-9a-fA-F]{4}):([0-9a-fA-F]{2}):([0-9a-fA-F]{2})\.([0-7])$ ]] || return 1
	dom=$((16#${BASH_REMATCH[1]}))
	bus=$((16#${BASH_REMATCH[2]}))
	dev=$((16#${BASH_REMATCH[3]}))
	fn=$((BASH_REMATCH[4]))
	if [ "$dom" -eq 0 ]; then
		printf 'PCI:%d:%d:%d\n' "$bus" "$dev" "$fn"
	else
		printf 'PCI:%d@%d:%d:%d\n' "$bus" "$dom" "$dev" "$fn"
	fi
}

# write_headless_xorg_conf: returns 0 when a config is in place (written now
# or earlier), 1 when this machine doesn't qualify.
write_headless_xorg_conf() {
	[ -f "$XORG_HEADLESS_CONF" ] && return 0
	local card kdrv xdrv busid
	card="$(primary_card)"
	[ -n "$card" ] || return 1
	[ "$(monitor_state "$card")" = "none" ] || return 1
	kdrv="$(basename "$(readlink -f "$card/device/driver")" 2>/dev/null)"
	[ -n "$kdrv" ] || return 1
	xdrv="$(xorg_driver_for "$kdrv")"
	busid="$(pci_busid "$card" 2>/dev/null || true)"
	mkdir -p "${XORG_HEADLESS_CONF%/*}" "$LIB_DIR"
	{
		echo "# Written by NivaroOS Host Desktop: no monitor was connected, so"
		echo "# let Xorg start anyway. Removed by nivaroos-uninstall."
		echo 'Section "Device"'
		echo '    Identifier "NivaroOSHeadless"'
		echo "    Driver \"${xdrv}\""
		[ -n "$busid" ] && echo "    BusID \"${busid}\""
		echo '    Option "AllowEmptyInitialConfiguration" "true"'
		echo 'EndSection'
	} > "$XORG_HEADLESS_CONF"
	echo "$XORG_HEADLESS_CONF" > "$HEADLESS_MARKER"
	echo "host-desktop: no monitor on ${card##*/} (${kdrv}) - wrote $XORG_HEADLESS_CONF for driver ${xdrv}" >&2
	return 0
}

# any_graphical_session: true if logind has any active/online x11/wayland
# session anywhere - never restart the display manager under someone.
any_graphical_session() {
	command -v loginctl >/dev/null 2>&1 || return 1
	local s t st
	for s in $(loginctl list-sessions --no-legend 2>/dev/null | awk '{print $1}'); do
		t="$(session_prop "$s" Type)"
		st="$(session_prop "$s" State)"
		case "$t" in x11|wayland|mir) ;; *) continue ;; esac
		case "$st" in active|online) return 0 ;; esac
	done
	return 1
}

display_manager_unit() {
	local id
	id="$(systemctl show -p Id --value display-manager.service 2>/dev/null || true)"
	[ "$(systemctl show -p LoadState --value display-manager.service 2>/dev/null)" = "loaded" ] || return 1
	printf '%s\n' "${id:-display-manager.service}"
}

current_boot_id() {
	cat /proc/sys/kernel/random/boot_id 2>/dev/null || echo unknown
}

# maybe_fix_headless: at most one display-manager restart per boot, only
# when no X server is running at all and nobody is logged in graphically.
maybe_fix_headless() {
	[ "$(cat "$DM_RESTART_MARKER" 2>/dev/null)" = "$(current_boot_id)" ] && return 0
	[ -n "$(list_xorg_servers)" ] && return 0
	any_graphical_session && return 0
	local dm
	dm="$(display_manager_unit)" || return 0
	write_headless_xorg_conf || return 0
	mkdir -p "$LIB_DIR"
	current_boot_id > "$DM_RESTART_MARKER"
	echo "host-desktop: restarting ${dm} once so Xorg picks up the headless config" >&2
	systemctl restart "$dm" 2>/dev/null || true
}

# ------------------------------------------------------------------------------
# Entry points
# ------------------------------------------------------------------------------
if [ "${1:-}" = "--resolve" ]; then
	resolve_display
	printf 'DISPLAY=%s\nXAUTHORITY=%s\nSESSION_TYPE=%s\nDESKTOP=%s\nXWAYLAND=%s\n' \
		"$R_DISPLAY" "$R_AUTH" "$R_TYPE" "$R_DESKTOP" "$R_XWAYLAND"
	exit 0
fi

if ! command -v x11vnc >/dev/null 2>&1; then
	echo "host-desktop: x11vnc is not installed" >&2
	R_DISPLAY="" write_state failed "x11vnc is not installed"
	exit 1
fi

# Wait - indefinitely, quietly - for a usable X11 display. Exiting instead
# would make systemd restart this every few seconds forever on a headless
# or Wayland-only box.
waited=0
while :; do
	resolve_display
	if [ -n "$R_DISPLAY" ] && [ "$R_XWAYLAND" != 1 ] && x_reachable; then
		break
	fi
	if [ "$R_TYPE" = "wayland" ]; then
		write_state waiting "The active login session is Wayland; Host Desktop can only stream an X11 (Xorg) session. Log out and pick the Xorg/X11 session at the login screen."
	elif [ -n "$R_DISPLAY" ]; then
		write_state waiting "X display $R_DISPLAY exists but could not be opened (no usable X authority cookie found)."
	else
		write_state waiting "No X11 display is running on this machine yet."
	fi
	if [ "$waited" -ge 20 ]; then
		maybe_fix_headless
	fi
	sleep 5
	waited=$((waited + 5))
done

# A headless Xorg started with AllowEmptyInitialConfiguration comes up at a
# tiny default framebuffer - bump that one case only. A real monitor's mode
# (or a size the user picked in the panel) is never overridden.
if command -v xrandr >/dev/null 2>&1; then
	xr="$(DISPLAY="$R_DISPLAY" XAUTHORITY="$R_AUTH" timeout 5 xrandr 2>/dev/null || true)"
	if [ -n "$xr" ] && ! printf '%s\n' "$xr" | grep -qE '^[^ ]+ connected'; then
		if [[ "$xr" =~ current\ ([0-9]+)\ x\ ([0-9]+) ]] && [ "${BASH_REMATCH[1]}" -lt 1280 ]; then
			DISPLAY="$R_DISPLAY" XAUTHORITY="$R_AUTH" timeout 5 xrandr --fb 1920x1080 >/dev/null 2>&1 || true
		fi
	fi
fi

write_state running ""

args=(
	-display "$R_DISPLAY"
	# Unix socket only (-rfbport 0 = no TCP listener at all), created 0600
	# root by the umask below: only root - i.e. vm-sidecar's authenticated
	# /host/console proxy - can connect. No password is needed on top.
	-rfbport 0 -unixsock "$SOCK" -noipv6 -nopw
	-forever -shared
	-xrandr resize
	# -repeat (not -norepeat): -norepeat broke holding a key (Backspace,
	# arrows) entirely.
	-repeat
	# -capslock: skip the fake Shift for uppercase keysyms when the host's
	# CapsLock is already on (which would otherwise invert the case).
	-capslock
	# Map keysyms the host keymap lacks onto spare keycodes, so Unicode
	# text (accents, CJK, emoji...) typed in the browser arrives intact.
	-add_keysyms
	# Only CLIPBOARD (explicit copy) goes to the viewer - PRIMARY changes
	# on every text selection and would spam the browser with clipboard
	# updates.
	-noprimary
)
if [ -n "$R_AUTH" ] && [ -f "$R_AUTH" ]; then
	args+=(-auth "$R_AUTH")
else
	args+=(-auth guess)
fi
# -noxdamage: GL compositors make XDAMAGE miss changes (stale patches);
# polling costs a little CPU. Configurable because it isn't needed without
# a compositor.
[ "$NOXDAMAGE" = "1" ] && args+=(-noxdamage)
# -fixscreen X=N: re-read the whole X framebuffer every N seconds - off by default
# (it costs a full-screen update each time); enable from the panel when a
# compositor still leaves stale rectangles.
[ "$FIXSCREEN" -gt 0 ] 2>/dev/null && args+=(-fixscreen "X=${FIXSCREEN}")

mkdir -p "$RUN_DIR"
rm -f "$SOCK"

CHILD=""
cleanup() {
	[ -n "$CHILD" ] && kill "$CHILD" 2>/dev/null
	rm -f "$SOCK"
	R_DISPLAY="" write_state stopped ""
}
trap 'cleanup; exit 0' TERM INT
trap cleanup EXIT

started_display="$R_DISPLAY"
(umask 077 && exec x11vnc "${args[@]}") &
CHILD=$!

# Watch for the desktop moving to another display (logout -> greeter on a
# new server, user switch). x11vnc would keep streaming a dead or wrong
# display; exiting lets systemd restart us, which re-resolves.
while kill -0 "$CHILD" 2>/dev/null; do
	sleep 10 &
	wait $! 2>/dev/null || true
	kill -0 "$CHILD" 2>/dev/null || break
	resolve_display
	if [ -n "$R_DISPLAY" ] && [ "$R_DISPLAY" != "$started_display" ] && [ "$R_XWAYLAND" != 1 ]; then
		echo "host-desktop: desktop moved from $started_display to $R_DISPLAY - restarting" >&2
		kill "$CHILD" 2>/dev/null
		break
	fi
done
wait "$CHILD" 2>/dev/null
exit 1
