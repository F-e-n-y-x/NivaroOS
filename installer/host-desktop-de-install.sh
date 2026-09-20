#!/usr/bin/env bash
# ==============================================================================
#  NivaroOS Host Desktop - Desktop Environment Provisioner
#
#  Host Desktop streaming (x11vnc) can only ever capture an X11 session -
#  never Wayland. This script answers "will streaming actually show
#  anything on this machine" and fixes it when the answer is no. It is
#  deliberately independent of whether the x11vnc/websockify streaming
#  service itself is installed - that's a separate concern (installed by
#  installer/install.sh or `nivaroos host-desktop enable`) from whether
#  there's a compatible desktop for it to stream. Standalone, like
#  gpu-driver-install.sh, so it can be run directly by an admin or
#  triggered from the dashboard's Host Desktop panel when it detects no
#  working X11 session.
#
#  Usage:
#    sudo bash host-desktop-de-install.sh --status
#        Reports detection only, JSON, installs nothing.
#
#    sudo bash host-desktop-de-install.sh --action=x11-companion
#        Adds an X11 session to the existing GNOME/Plasma desktop (only
#        valid when --status reports state=wayland_only and a de_name of
#        gnome or plasma).
#
#    sudo bash host-desktop-de-install.sh --action=alongside --de=xfce|cinnamon|mate
#        Installs the given desktop (plus Xorg/a display manager if
#        missing) without touching whatever is already there.
#
#    sudo bash host-desktop-de-install.sh --action=replace --de=xfce|cinnamon|mate --confirm=<current-de-name>
#        Removes the currently detected desktop and installs the given
#        one instead. Destructive - --confirm must exactly match the
#        de_name --status reports, or this refuses to run.
# ==============================================================================

if [ -z "${BASH_VERSION:-}" ]; then
	exec bash "$0" "$@"
fi

set -Eeuo pipefail

ACTION=""
DE_CHOICE=""
CONFIRM_NAME=""
STATUS_ONLY="false"
for arg in "$@"; do
	case "$arg" in
		--status) STATUS_ONLY="true" ;;
		--action=*) ACTION="${arg#*=}" ;;
		--de=*) DE_CHOICE="${arg#*=}" ;;
		--confirm=*) CONFIRM_NAME="${arg#*=}" ;;
		--help|-h)
			sed -n '2,29p' "$0" | sed 's/^# \{0,1\}//'
			exit 0
			;;
	esac
done

info()  { printf '%s\n' "$1" >&2; }
error() { printf '%s\n' "$1" >&2; }

if [ "$STATUS_ONLY" != "true" ] && [ "$(id -u)" -ne 0 ]; then
	if command -v sudo >/dev/null 2>&1; then
		exec sudo -E bash "$0" "$@"
	else
		error "This needs root to install packages - re-run as root or with sudo."
		exit 1
	fi
fi

# ------------------------------------------------------------------------------
# Package Manager Helpers (same shape as installer/install.sh's)
# ------------------------------------------------------------------------------
pkg_update() {
	if command -v apt-get >/dev/null 2>&1; then apt-get update -qq
	elif command -v dnf >/dev/null 2>&1; then dnf check-update || true
	elif command -v yum >/dev/null 2>&1; then yum check-update || true
	elif command -v pacman >/dev/null 2>&1; then pacman -Sy --noconfirm
	elif command -v zypper >/dev/null 2>&1; then zypper refresh
	fi
}
pkg_install() {
	local pkgs=("$@")
	if command -v apt-get >/dev/null 2>&1; then
		DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "${pkgs[@]}"
	elif command -v dnf >/dev/null 2>&1; then dnf install -y "${pkgs[@]}"
	elif command -v yum >/dev/null 2>&1; then yum install -y "${pkgs[@]}"
	elif command -v pacman >/dev/null 2>&1; then pacman -S --noconfirm --needed "${pkgs[@]}"
	elif command -v zypper >/dev/null 2>&1; then zypper install -y --no-recommends "${pkgs[@]}"
	fi
}

# ------------------------------------------------------------------------------
# Desktop Environment / X11 Support Detection
#
# Mirrors installer/install.sh's detect_display_server_support() exactly -
# see that function's comments for the full reasoning behind each check.
# Kept as a separate, duplicated implementation (not sourced from
# install.sh) so this script stays genuinely standalone and runnable on
# its own, matching gpu-driver-install.sh's design.
# ------------------------------------------------------------------------------
DETECTED_DM="none"
DETECTED_DE_NAME=""
DETECTED_XSESSION_NAME=""
DE_SUPPORT_STATE="no_de"

detect_display_server_support() {
	DETECTED_DM="none"
	local dm
	for dm in lightdm gdm3 gdm sddm xdm; do
		if systemctl list-unit-files 2>/dev/null | grep -q "^${dm}\.service"; then
			DETECTED_DM="$dm"
			break
		fi
	done

	DETECTED_DE_NAME=""
	DETECTED_XSESSION_NAME=""
	DE_SUPPORT_STATE="no_de"

	if [ "$DETECTED_DM" = "none" ] && [ ! -d /usr/share/xsessions ] && [ ! -d /usr/share/wayland-sessions ]; then
		return
	fi

	local de_map=(
		"gnome-shell:gnome" "plasmashell:plasma" "xfce4-session:xfce"
		"cinnamon:cinnamon" "cinnamon-session:cinnamon" "mate-session:mate"
		"lxqt-session:lxqt" "budgie-wm:budgie" "budgie-panel:budgie"
		"deepin-session:deepin" "lxsession:lxde"
	)
	local m proc_name de_id
	for m in "${de_map[@]}"; do
		proc_name="${m%%:*}"
		de_id="${m##*:}"
		if pgrep -x "$proc_name" >/dev/null 2>&1; then
			DETECTED_DE_NAME="$de_id"
			break
		fi
	done

	if [ -z "$DETECTED_DE_NAME" ] && [ -d /usr/share/xsessions ]; then
		local f base
		for f in /usr/share/xsessions/*.desktop; do
			[ -e "$f" ] || continue
			base="$(basename "$f" .desktop)"
			case "$base" in
				gnome*) DETECTED_DE_NAME="gnome" ;;
				plasma*|kde*) DETECTED_DE_NAME="plasma" ;;
				xfce*) DETECTED_DE_NAME="xfce" ;;
				cinnamon*) DETECTED_DE_NAME="cinnamon" ;;
				mate*) DETECTED_DE_NAME="mate" ;;
				lxqt*) DETECTED_DE_NAME="lxqt" ;;
				budgie*) DETECTED_DE_NAME="budgie" ;;
				deepin*) DETECTED_DE_NAME="deepin" ;;
				[Ll][Xx][Dd][Ee]*) DETECTED_DE_NAME="lxde" ;;
			esac
			[ -n "$DETECTED_DE_NAME" ] && break
		done
	fi

	if [ -z "$DETECTED_DE_NAME" ] && [ -d /usr/share/wayland-sessions ]; then
		local f base
		for f in /usr/share/wayland-sessions/*.desktop; do
			[ -e "$f" ] || continue
			base="$(basename "$f" .desktop)"
			case "$base" in
				gnome*) DETECTED_DE_NAME="gnome" ;;
				plasma*|kde*) DETECTED_DE_NAME="plasma" ;;
				*) DETECTED_DE_NAME="$base" ;;
			esac
			[ -n "$DETECTED_DE_NAME" ] && break
		done
	fi

	if [ -z "$DETECTED_DE_NAME" ] && [ "$DETECTED_DM" = "none" ]; then
		DE_SUPPORT_STATE="no_de"
		return
	fi

	if [ -d /usr/share/xsessions ]; then
		local f base
		for f in /usr/share/xsessions/*.desktop; do
			[ -e "$f" ] || continue
			base="$(basename "$f" .desktop)"
			case "$base" in
				*wayland*) continue ;;
			esac
			case "${DETECTED_DE_NAME}:${base}" in
				gnome:gnome*|plasma:plasma*|plasma:kde*|xfce:xfce*|cinnamon:cinnamon*|mate:mate*|lxqt:lxqt*|budgie:budgie*|deepin:deepin*|lxde:[Ll][Xx][Dd][Ee]*)
					DETECTED_XSESSION_NAME="$base"
					break
					;;
			esac
		done
		if [ -z "$DETECTED_XSESSION_NAME" ]; then
			for f in /usr/share/xsessions/*.desktop; do
				[ -e "$f" ] || continue
				base="$(basename "$f" .desktop)"
				case "$base" in *wayland*) continue ;; esac
				DETECTED_XSESSION_NAME="$base"
				break
			done
		fi
	fi

	if [ -n "$DETECTED_XSESSION_NAME" ]; then
		DE_SUPPORT_STATE="supported"
	elif [ -n "$DETECTED_DE_NAME" ]; then
		DE_SUPPORT_STATE="wayland_only"
	else
		DE_SUPPORT_STATE="no_de"
	fi
}

print_status_json() {
	detect_display_server_support
	printf '{"state":"%s","de_name":"%s","xsession":"%s","dm":"%s"}\n' \
		"$DE_SUPPORT_STATE" "$DETECTED_DE_NAME" "$DETECTED_XSESSION_NAME" "$DETECTED_DM"
}

# ------------------------------------------------------------------------------
# Provisioning
# ------------------------------------------------------------------------------
ensure_apt_universe_enabled() {
	if ! command -v apt-get >/dev/null 2>&1; then return 0; fi
	if ! command -v add-apt-repository >/dev/null 2>&1; then
		apt-get install -y software-properties-common >/dev/null 2>&1 || true
	fi
	if command -v add-apt-repository >/dev/null 2>&1; then
		add-apt-repository -y universe >/dev/null 2>&1 || true
	fi
	pkg_update
}

pkg_install_xorg_stack() {
	if command -v apt-get >/dev/null 2>&1; then
		pkg_install xserver-xorg xinit
	elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
		pkg_install xorg-x11-server-Xorg xorg-x11-xinit
	elif command -v pacman >/dev/null 2>&1; then
		pkg_install xorg-server xorg-xinit
	elif command -v zypper >/dev/null 2>&1; then
		pkg_install xorg-x11-server
	fi
}

pkg_install_display_manager() {
	pkg_install lightdm lightdm-gtk-greeter
}

pkg_install_x11_companion() {
	local de="$1"
	case "$de" in
		gnome)
			if command -v apt-get >/dev/null 2>&1; then
				pkg_install xserver-xorg gnome-session
			elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
				pkg_install xorg-x11-server-Xorg gnome-session-xsession
			elif command -v pacman >/dev/null 2>&1; then
				pkg_install xorg-server gnome-session
			elif command -v zypper >/dev/null 2>&1; then
				pkg_install xorg-x11-server gnome-session
			fi
			;;
		plasma)
			if command -v apt-get >/dev/null 2>&1; then
				pkg_install xserver-xorg plasma-workspace-x11
			elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
				pkg_install xorg-x11-server-Xorg plasma-workspace-x11
			elif command -v pacman >/dev/null 2>&1; then
				pkg_install xorg-server plasma-workspace
			elif command -v zypper >/dev/null 2>&1; then
				pkg_install xorg-x11-server
			fi
			;;
		*)
			error "No known X11 companion package for '${de}'."
			return 1
			;;
	esac
}

pkg_install_de() {
	local de="$1"
	case "$de" in
		xfce)
			if command -v apt-get >/dev/null 2>&1; then
				pkg_install xfce4 xfce4-terminal
			elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
				dnf group install -y "Xfce Desktop" >/dev/null 2>&1 || dnf install -y @xfce-desktop-environment >/dev/null 2>&1 || pkg_install xfce4-session xfce4-panel xfdesktop xfce4-settings xfce4-terminal
			elif command -v pacman >/dev/null 2>&1; then
				pkg_install xfce4 xfce4-goodies
			elif command -v zypper >/dev/null 2>&1; then
				zypper install -y -t pattern xfce >/dev/null 2>&1 || pkg_install xfce4-session
			fi
			;;
		cinnamon)
			if command -v apt-get >/dev/null 2>&1; then
				pkg_install cinnamon-core || pkg_install cinnamon
			elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
				dnf group install -y "Cinnamon Desktop" >/dev/null 2>&1 || dnf install -y @cinnamon-desktop-environment >/dev/null 2>&1 || pkg_install cinnamon-desktop
			elif command -v pacman >/dev/null 2>&1; then
				pkg_install cinnamon
			elif command -v zypper >/dev/null 2>&1; then
				zypper install -y -t pattern cinnamon >/dev/null 2>&1 || pkg_install cinnamon
			fi
			;;
		mate)
			if command -v apt-get >/dev/null 2>&1; then
				pkg_install mate-desktop-environment-core || pkg_install mate-desktop-environment
			elif command -v dnf >/dev/null 2>&1 || command -v yum >/dev/null 2>&1; then
				dnf group install -y "MATE Desktop" >/dev/null 2>&1 || dnf install -y @mate-desktop-environment >/dev/null 2>&1 || pkg_install mate-desktop
			elif command -v pacman >/dev/null 2>&1; then
				pkg_install mate mate-extra
			elif command -v zypper >/dev/null 2>&1; then
				zypper install -y -t pattern mate >/dev/null 2>&1 || pkg_install mate
			fi
			;;
		*)
			error "Unknown desktop '${de}' - expected xfce, cinnamon, or mate."
			return 1
			;;
	esac
}

remove_desktop_environment() {
	local de="$1"
	case "$de" in
		gnome)
			if command -v apt-get >/dev/null 2>&1; then
				apt-get purge -y gnome-shell gnome-session ubuntu-desktop gdm3 gdm >/dev/null 2>&1 || true
			elif command -v dnf >/dev/null 2>&1; then
				dnf group remove -y "GNOME Desktop" >/dev/null 2>&1 || true
			elif command -v pacman >/dev/null 2>&1; then
				pacman -Rns --noconfirm gnome gdm >/dev/null 2>&1 || true
			fi
			;;
		plasma)
			if command -v apt-get >/dev/null 2>&1; then
				apt-get purge -y plasma-desktop sddm >/dev/null 2>&1 || true
			elif command -v dnf >/dev/null 2>&1; then
				dnf group remove -y "KDE Plasma Workspaces" >/dev/null 2>&1 || true
			elif command -v pacman >/dev/null 2>&1; then
				pacman -Rns --noconfirm plasma sddm >/dev/null 2>&1 || true
			fi
			;;
		xfce)
			if command -v apt-get >/dev/null 2>&1; then
				apt-get purge -y xfce4 >/dev/null 2>&1 || true
			elif command -v pacman >/dev/null 2>&1; then
				pacman -Rns --noconfirm xfce4 >/dev/null 2>&1 || true
			fi
			;;
		cinnamon)
			if command -v apt-get >/dev/null 2>&1; then
				apt-get purge -y cinnamon-core cinnamon >/dev/null 2>&1 || true
			elif command -v pacman >/dev/null 2>&1; then
				pacman -Rns --noconfirm cinnamon >/dev/null 2>&1 || true
			fi
			;;
		mate)
			if command -v apt-get >/dev/null 2>&1; then
				apt-get purge -y mate-desktop-environment-core mate-desktop-environment >/dev/null 2>&1 || true
			elif command -v pacman >/dev/null 2>&1; then
				pacman -Rns --noconfirm mate >/dev/null 2>&1 || true
			fi
			;;
		*)
			error "Don't know how to safely remove '${de}' automatically - leaving it in place and installing alongside it instead."
			;;
	esac
	if command -v apt-get >/dev/null 2>&1; then
		apt-get autoremove -y >/dev/null 2>&1 || true
	fi
}

configure_default_x11_session() {
	detect_display_server_support
	if [ "$DE_SUPPORT_STATE" != "supported" ]; then
		return
	fi
	case "$DETECTED_DM" in
		lightdm)
			mkdir -p /etc/lightdm/lightdm.conf.d
			cat > /etc/lightdm/lightdm.conf.d/60-nivaroos-host-desktop.conf <<LIGHTDMEOF
[Seat:*]
user-session=${DETECTED_XSESSION_NAME}
LIGHTDMEOF
			;;
		gdm3|gdm)
			local gdm_conf="/etc/gdm3/custom.conf"
			[ -f "$gdm_conf" ] || gdm_conf="/etc/gdm/custom.conf"
			if [ -f "$gdm_conf" ]; then
				if grep -q "^WaylandEnable" "$gdm_conf" 2>/dev/null; then
					sed -i 's/^WaylandEnable=.*/WaylandEnable=false/' "$gdm_conf"
				elif grep -q "^\[daemon\]" "$gdm_conf" 2>/dev/null; then
					sed -i '/^\[daemon\]/a WaylandEnable=false' "$gdm_conf"
				else
					printf '\n[daemon]\nWaylandEnable=false\n' >> "$gdm_conf"
				fi
			fi
			;;
		sddm)
			mkdir -p /etc/sddm.conf.d
			cat > /etc/sddm.conf.d/60-nivaroos-host-desktop.conf <<SDDMEOF
[Autologin]
Session=${DETECTED_XSESSION_NAME}
SDDMEOF
			;;
	esac
}

do_install() {
	detect_display_server_support
	local prior_de="$DETECTED_DE_NAME"

	case "$ACTION" in
		x11-companion)
			if [ "$DE_SUPPORT_STATE" != "wayland_only" ]; then
				error "x11-companion only applies when --status reports state=wayland_only (currently: ${DE_SUPPORT_STATE})."
				exit 1
			fi
			info "Adding an X11 session to your existing ${DETECTED_DE_NAME} desktop..."
			pkg_install_x11_companion "$DETECTED_DE_NAME"
			;;
		alongside)
			if [ -z "$DE_CHOICE" ]; then
				error "alongside requires --de=xfce|cinnamon|mate"
				exit 1
			fi
			info "Installing ${DE_CHOICE} alongside your current desktop, for streaming..."
			ensure_apt_universe_enabled
			pkg_install_xorg_stack || true
			if [ "$DETECTED_DM" = "none" ]; then
				pkg_install_display_manager || true
			fi
			pkg_install_de "$DE_CHOICE"
			mkdir -p /var/lib/nivaroos
			echo "alongside:${DE_CHOICE}" > /var/lib/nivaroos/provisioned-desktop
			;;
		replace)
			if [ -z "$DE_CHOICE" ]; then
				error "replace requires --de=xfce|cinnamon|mate"
				exit 1
			fi
			if [ -z "$prior_de" ] || [ "$CONFIRM_NAME" != "$prior_de" ]; then
				error "replace requires --confirm=<current-de-name> matching exactly what --status reports as de_name (currently: '${prior_de}'). Refusing to guess on a destructive action."
				exit 1
			fi
			info "Removing existing desktop environment (${prior_de}) and installing ${DE_CHOICE}..."
			remove_desktop_environment "$prior_de"
			ensure_apt_universe_enabled
			pkg_install_xorg_stack || true
			pkg_install_display_manager || true
			pkg_install_de "$DE_CHOICE"
			mkdir -p /var/lib/nivaroos
			echo "replaced:${DE_CHOICE}" > /var/lib/nivaroos/provisioned-desktop
			;;
		*)
			error "Unknown or missing --action (expected x11-companion, alongside, or replace). Use --status to check detection first, or --help for usage."
			exit 1
			;;
	esac

	systemctl daemon-reload >/dev/null 2>&1 || true
	configure_default_x11_session

	detect_display_server_support
	if [ "$DE_SUPPORT_STATE" = "supported" ]; then
		info "Done. Desktop Environment ready for streaming: ${DETECTED_DE_NAME:-unknown} via ${DETECTED_XSESSION_NAME}."
		info "If a desktop was newly installed, log out/reboot once to actually start it."
	else
		error "Provisioning ran, but no working X11 session could be confirmed afterward. A reboot may be needed before it takes effect - check again with --status."
	fi
}

if [ "$STATUS_ONLY" = "true" ]; then
	print_status_json
	exit 0
fi

do_install
