#!/usr/bin/env bash
# ==============================================================================
#  NivaroOS Host Desktop - Desktop Environment Provisioner
#
#  SINGLE SOURCE OF TRUTH: embedded into nivaroos-vm-sidecar (go:embed, see
#  hostdesktop_install.go) and written to
#  /usr/local/bin/nivaroos-host-desktop-de-install.sh by the same install
#  logic that installs the x11vnc service. Edit it here only.
#
#  Host Desktop streaming (x11vnc) can only capture an X11 (Xorg) session -
#  never Wayland. This script answers "will streaming actually show
#  anything on this machine" and fixes it when the answer is no.
#
#  Usage:
#    bash host-desktop-de-install.sh --status
#        Detection only, one line of JSON, installs nothing, needs no root.
#
#    bash host-desktop-de-install.sh --action=x11-companion
#        Adds an X11 session to the existing GNOME/Plasma desktop (only
#        valid when --status reports state=wayland_only and
#        x11_companion_available=true).
#
#    bash host-desktop-de-install.sh --action=alongside --de=xfce|cinnamon|mate
#        Installs the given desktop (plus Xorg and a display manager if
#        missing) without touching whatever is already there.
#
#    bash host-desktop-de-install.sh --action=replace --de=xfce|cinnamon|mate --confirm=<current-de-name>
#        Installs the given desktop, verifies it, and only THEN removes the
#        currently detected one. --confirm must exactly match the de_name
#        --status reports, or this refuses to run.
#
#  Actions re-run themselves with sudo when started as a normal user.
#  Requires systemd (Alpine/OpenRC: reported as unsupported).
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
			sed -n '2,34p' "$0" | sed 's/^# \{0,1\}//'
			exit 0
			;;
	esac
done

info()  { printf '%s\n' "$1" >&2; }
error() { printf 'ERROR: %s\n' "$1" >&2; }

WRAPPER=/usr/local/bin/nivaroos-host-desktop.sh
LIB_DIR=/var/lib/nivaroos
PROVISION_MARKER="$LIB_DIR/provisioned-desktop"
GDM_MARKER="$LIB_DIR/host-desktop-gdm-wayland"
LIGHTDM_CONF=/etc/lightdm/lightdm.conf.d/60-nivaroos-host-desktop.conf
SDDM_CONF=/etc/sddm.conf.d/60-nivaroos-host-desktop.conf

# ------------------------------------------------------------------------------
# Platform
# ------------------------------------------------------------------------------
OS_ID=""
OS_LIKE=""
if [ -r /etc/os-release ]; then
	OS_ID="$(. /etc/os-release && echo "${ID:-}")"
	OS_LIKE="$(. /etc/os-release && echo "${ID_LIKE:-}")"
fi

PM=""
if command -v apt-get >/dev/null 2>&1; then PM=apt
elif command -v dnf >/dev/null 2>&1; then PM=dnf
elif command -v yum >/dev/null 2>&1; then PM=yum
elif command -v pacman >/dev/null 2>&1; then PM=pacman
elif command -v zypper >/dev/null 2>&1; then PM=zypper
elif command -v apk >/dev/null 2>&1; then PM=apk
fi

UNSUPPORTED_REASON=""
if [ ! -d /run/systemd/system ] || ! command -v systemctl >/dev/null 2>&1; then
	UNSUPPORTED_REASON="This system does not run systemd (e.g. Alpine/OpenRC). Host Desktop provisioning only supports systemd distributions (Debian, Ubuntu, Fedora, Arch, openSUSE)."
elif [ "$PM" = "apk" ] || [ -z "$PM" ]; then
	UNSUPPORTED_REASON="No supported package manager found (apt, dnf, pacman or zypper)."
fi

is_ubuntu() {
	case " $OS_ID $OS_LIKE " in *" ubuntu "*) return 0 ;; esac
	return 1
}

# ------------------------------------------------------------------------------
# Package helpers
# ------------------------------------------------------------------------------
pkg_update() {
	case "$PM" in
		apt) apt-get update -qq ;;
		dnf) dnf -q makecache || true ;;
		yum) yum -q makecache || true ;;
		pacman) pacman -Sy --noconfirm ;;
		zypper) zypper --non-interactive refresh ;;
	esac
}

pkg_install() {
	case "$PM" in
		apt) DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends "$@" ;;
		dnf) dnf install -y "$@" ;;
		yum) yum install -y "$@" ;;
		pacman) pacman -S --noconfirm --needed "$@" ;;
		zypper) zypper --non-interactive install --no-recommends "$@" ;;
		*) return 1 ;;
	esac
}

pkg_installed() {
	case "$PM" in
		apt) dpkg-query -W -f='${Status}' "$1" 2>/dev/null | grep -q 'install ok installed' ;;
		dnf|yum|zypper) rpm -q "$1" >/dev/null 2>&1 ;;
		pacman) pacman -Qq "$1" >/dev/null 2>&1 || pacman -Qqg "$1" >/dev/null 2>&1 ;;
		*) return 1 ;;
	esac
}

# pkg_available <pkg> - installed, or installable from the configured repos.
# Uses local metadata only, so --status stays fast and offline-safe.
pkg_available() {
	pkg_installed "$1" && return 0
	case "$PM" in
		apt)
			local cand
			cand="$(apt-cache policy "$1" 2>/dev/null | awk '/Candidate:/ {print $2; exit}')"
			[ -n "$cand" ] && [ "$cand" != "(none)" ]
			;;
		dnf) timeout 20 dnf -q -C list --available "$1" >/dev/null 2>&1 ;;
		yum) timeout 20 yum -q -C list available "$1" >/dev/null 2>&1 ;;
		pacman) pacman -Si "$1" >/dev/null 2>&1 || pacman -Sg "$1" >/dev/null 2>&1 ;;
		zypper) timeout 20 zypper --non-interactive --no-refresh search -x "$1" >/dev/null 2>&1 ;;
		*) return 1 ;;
	esac
}

# first_available <pkg>... - print the first installable package name.
first_available() {
	local p
	for p in "$@"; do
		if pkg_available "$p"; then
			printf '%s\n' "$p"
			return 0
		fi
	done
	return 1
}

pkg_remove() {
	local installed=() p
	for p in "$@"; do
		pkg_installed "$p" && installed+=("$p")
	done
	[ "${#installed[@]}" -gt 0 ] || return 0
	info "Removing: ${installed[*]}"
	case "$PM" in
		apt) DEBIAN_FRONTEND=noninteractive apt-get purge -y "${installed[@]}" ;;
		dnf) dnf remove -y "${installed[@]}" ;;
		yum) yum remove -y "${installed[@]}" ;;
		pacman) pacman -Rns --noconfirm "${installed[@]}" ;;
		zypper) zypper --non-interactive remove --clean-deps "${installed[@]}" ;;
	esac
}

xorg_server_pkg() {
	case "$PM" in
		apt) echo xserver-xorg ;;
		dnf|yum) echo xorg-x11-server-Xorg ;;
		pacman) echo xorg-server ;;
		zypper) echo xorg-x11-server ;;
	esac
}

# ------------------------------------------------------------------------------
# Detection
# ------------------------------------------------------------------------------
DETECTED_DM="none"
DM_ENABLED="false"
DETECTED_DE_NAME=""
DETECTED_XSESSION_NAME=""
DE_SUPPORT_STATE="no_de"
SESSION_TYPE=""
SESSION_DISPLAY=""
SESSION_XWAYLAND="0"
X11_COMPANION_AVAILABLE="false"
REASON=""

dm_unit_exists() {
	[ -n "$(systemctl list-unit-files "$1.service" --no-legend 2>/dev/null)" ]
}

detect_dm() {
	DETECTED_DM="none"
	DM_ENABLED="false"
	local id
	if [ "$(systemctl show -p LoadState --value display-manager.service 2>/dev/null)" = "loaded" ]; then
		id="$(systemctl show -p Id --value display-manager.service 2>/dev/null || true)"
		id="${id%.service}"
		if [ -n "$id" ] && [ "$id" != "display-manager" ]; then
			DETECTED_DM="$id"
			DM_ENABLED="true"
			return
		fi
	fi
	local dm
	for dm in gdm3 gdm sddm lightdm lxdm xdm slim; do
		if dm_unit_exists "$dm"; then
			DETECTED_DM="$dm"
			return
		fi
	done
	return 0
}

# Current session: the wrapper's resolver (same logic x11vnc uses) when it's
# installed, logind alone otherwise.
detect_session() {
	SESSION_TYPE=""
	SESSION_DISPLAY=""
	SESSION_XWAYLAND="0"
	local line
	# Only a wrapper that knows --resolve: an older one ignores its
	# arguments and would start x11vnc instead.
	if [ -x "$WRAPPER" ] && grep -q -- '--resolve' "$WRAPPER" 2>/dev/null; then
		while IFS= read -r line; do
			case "$line" in
				DISPLAY=*) SESSION_DISPLAY="${line#DISPLAY=}" ;;
				SESSION_TYPE=*) SESSION_TYPE="${line#SESSION_TYPE=}" ;;
				XWAYLAND=*) SESSION_XWAYLAND="${line#XWAYLAND=}" ;;
			esac
		done < <(timeout 20 "$WRAPPER" --resolve 2>/dev/null || true)
		return
	fi
	command -v loginctl >/dev/null 2>&1 || return 0
	local sid
	sid="$(loginctl show-seat seat0 -p ActiveSession --value 2>/dev/null || true)"
	[ -n "$sid" ] || return 0
	SESSION_TYPE="$(loginctl show-session "$sid" -p Type --value 2>/dev/null || true)"
	if [ "$SESSION_TYPE" = "x11" ]; then
		SESSION_DISPLAY="$(loginctl show-session "$sid" -p Display --value 2>/dev/null || true)"
	fi
	return 0
}

detect_de() {
	DETECTED_DE_NAME=""
	local de_map=(
		"gnome-shell:gnome" "plasmashell:plasma" "xfce4-session:xfce"
		"cinnamon:cinnamon" "cinnamon-session:cinnamon" "mate-session:mate"
		"lxqt-session:lxqt" "budgie-wm:budgie" "budgie-panel:budgie"
		"deepin-session:deepin" "lxsession:lxde"
	)
	local m
	for m in "${de_map[@]}"; do
		if pgrep -x "${m%%:*}" >/dev/null 2>&1; then
			DETECTED_DE_NAME="${m##*:}"
			return
		fi
	done
	local dir f base
	for dir in /usr/share/xsessions /usr/share/wayland-sessions; do
		[ -d "$dir" ] || continue
		for f in "$dir"/*.desktop; do
			[ -e "$f" ] || continue
			base="$(basename "$f" .desktop)"
			case "$base" in
				gnome*|ubuntu*) DETECTED_DE_NAME="gnome" ;;
				plasma*|kde*) DETECTED_DE_NAME="plasma" ;;
				xfce*) DETECTED_DE_NAME="xfce" ;;
				cinnamon*) DETECTED_DE_NAME="cinnamon" ;;
				mate*) DETECTED_DE_NAME="mate" ;;
				lxqt*) DETECTED_DE_NAME="lxqt" ;;
				budgie*) DETECTED_DE_NAME="budgie" ;;
				deepin*) DETECTED_DE_NAME="deepin" ;;
				[Ll][Xx][Dd][Ee]*) DETECTED_DE_NAME="lxde" ;;
			esac
			[ -n "$DETECTED_DE_NAME" ] && return
		done
	done
	return 0
}

# detect_xsession: an X11 session file matching the detected desktop,
# otherwise any X11 session at all.
detect_xsession() {
	DETECTED_XSESSION_NAME=""
	[ -d /usr/share/xsessions ] || return 0
	local f base
	if [ -n "$DETECTED_DE_NAME" ] && [ -e "/usr/share/xsessions/${DETECTED_DE_NAME}.desktop" ]; then
		DETECTED_XSESSION_NAME="$DETECTED_DE_NAME"
		return 0
	fi
	for f in /usr/share/xsessions/*.desktop; do
		[ -e "$f" ] || continue
		base="$(basename "$f" .desktop)"
		case "$base" in *wayland*) continue ;; esac
		case "${DETECTED_DE_NAME}:${base}" in
			gnome:gnome*|gnome:ubuntu*|plasma:plasma*|plasma:kde*|xfce:xfce*|cinnamon:cinnamon*|mate:mate*|lxqt:lxqt*|budgie:budgie*|deepin:deepin*|lxde:[Ll][Xx][Dd][Ee]*)
				DETECTED_XSESSION_NAME="$base"
				return
				;;
		esac
	done
	for f in /usr/share/xsessions/*.desktop; do
		[ -e "$f" ] || continue
		base="$(basename "$f" .desktop)"
		case "$base" in *wayland*) continue ;; esac
		DETECTED_XSESSION_NAME="$base"
		return
	done
	return 0
}

gnome_major_version() {
	local v
	v="$(gnome-shell --version 2>/dev/null | grep -oE '[0-9]+' | head -n1 || true)"
	printf '%s\n' "${v:-0}"
}

# companion_packages <de> - candidates (first installable wins) for the X11
# session of an installed GNOME/Plasma. GNOME 49+ and Plasma 6.8+ dropped
# their X11 sessions upstream, which is why this checks availability rather
# than assuming.
companion_candidates() {
	case "$1:$PM" in
		gnome:apt) echo "gnome-session" ;;
		gnome:dnf|gnome:yum) echo "gnome-session-xsession gnome-classic-session-xsession" ;;
		gnome:pacman) echo "gnome-session" ;;
		gnome:zypper) echo "gnome-session-xsession gnome-session" ;;
		plasma:apt) echo "plasma-session-x11 plasma-workspace-x11" ;;
		plasma:dnf|plasma:yum) echo "plasma-workspace-x11" ;;
		plasma:pacman) echo "plasma-x11-session" ;;
		plasma:zypper) echo "plasma6-session-x11 plasma5-session" ;;
	esac
}

companion_available() {
	local de="$1" cands
	case "$de" in
		gnome) [ "$(gnome_major_version)" -ge 49 ] && return 1 ;;
		plasma) ;;
		*) return 1 ;;
	esac
	cands="$(companion_candidates "$de")"
	[ -n "$cands" ] || return 1
	# shellcheck disable=SC2086
	first_available $cands >/dev/null
}

detect_display_server_support() {
	DE_SUPPORT_STATE="no_de"
	REASON=""
	X11_COMPANION_AVAILABLE="false"
	if [ -n "$UNSUPPORTED_REASON" ]; then
		DE_SUPPORT_STATE="unsupported"
		REASON="$UNSUPPORTED_REASON"
		return
	fi
	detect_dm
	detect_session
	detect_de
	detect_xsession

	# A real Xorg is running right now (any DM, or plain startx with no DM
	# at all) - that is all x11vnc needs.
	if [ -n "$SESSION_DISPLAY" ] && [ "$SESSION_XWAYLAND" != "1" ] && [ "$SESSION_TYPE" != "wayland" ]; then
		DE_SUPPORT_STATE="supported"
		return
	fi

	if [ "$SESSION_TYPE" = "wayland" ]; then
		if [ -n "$DETECTED_XSESSION_NAME" ]; then
			DE_SUPPORT_STATE="wayland_session"
			REASON="You are logged in to a Wayland session. Log out and choose the '${DETECTED_XSESSION_NAME}' (Xorg/X11) session at the login screen."
		else
			DE_SUPPORT_STATE="wayland_only"
			REASON="The desktop only has a Wayland session installed."
		fi
	elif [ -n "$DETECTED_XSESSION_NAME" ]; then
		DE_SUPPORT_STATE="supported"
	elif [ -n "$DETECTED_DE_NAME" ]; then
		DE_SUPPORT_STATE="wayland_only"
		REASON="The desktop only has a Wayland session installed."
	else
		DE_SUPPORT_STATE="no_de"
		REASON="No desktop environment is installed."
	fi

	if [ "$DE_SUPPORT_STATE" = "wayland_only" ] && companion_available "$DETECTED_DE_NAME"; then
		X11_COMPANION_AVAILABLE="true"
	fi
}

json_escape() {
	local s="$1"
	s="${s//\\/\\\\}"
	s="${s//\"/\\\"}"
	s="${s//$'\n'/ }"
	s="${s//$'\t'/ }"
	printf '%s' "$s"
}

print_status_json() {
	detect_display_server_support
	local supported=true
	[ -n "$UNSUPPORTED_REASON" ] && supported=false
	printf '{"state":"%s","de_name":"%s","xsession":"%s","dm":"%s","dm_enabled":%s,"session_type":"%s","display":"%s","x11_companion_available":%s,"distro_supported":%s,"reason":"%s"}\n' \
		"$DE_SUPPORT_STATE" "$(json_escape "$DETECTED_DE_NAME")" "$(json_escape "$DETECTED_XSESSION_NAME")" \
		"$(json_escape "$DETECTED_DM")" "$DM_ENABLED" "$(json_escape "$SESSION_TYPE")" \
		"$(json_escape "$SESSION_DISPLAY")" "$X11_COMPANION_AVAILABLE" "$supported" "$(json_escape "$REASON")"
}

if [ "$STATUS_ONLY" = "true" ]; then
	print_status_json
	exit 0
fi

# ------------------------------------------------------------------------------
# Everything below provisions - root, systemd and a known package manager.
# ------------------------------------------------------------------------------
if [ -n "$UNSUPPORTED_REASON" ]; then
	error "$UNSUPPORTED_REASON"
	exit 2
fi

if [ "$(id -u)" -ne 0 ]; then
	if command -v sudo >/dev/null 2>&1; then
		exec sudo -E bash "$0" "$@"
	fi
	error "This needs root to install packages - re-run as root or with sudo."
	exit 1
fi

# shellcheck disable=SC2154
trap 'rc=$?; error "Step failed (exit ${rc}) at line ${LINENO}: ${BASH_COMMAND}. Nothing was removed; your current desktop is untouched unless a message above says otherwise."; exit "$rc"' ERR

ensure_apt_universe_enabled() {
	[ "$PM" = "apt" ] || return 0
	is_ubuntu || return 0
	if ! command -v add-apt-repository >/dev/null 2>&1; then
		DEBIAN_FRONTEND=noninteractive apt-get install -y software-properties-common >/dev/null 2>&1 || true
	fi
	if command -v add-apt-repository >/dev/null 2>&1; then
		add-apt-repository -y universe >/dev/null 2>&1 || true
	fi
}

pkg_install_xorg_stack() {
	case "$PM" in
		apt) pkg_install xserver-xorg xinit ;;
		dnf|yum) pkg_install xorg-x11-server-Xorg xorg-x11-xinit ;;
		pacman) pkg_install xorg-server xorg-xinit ;;
		zypper) pkg_install xorg-x11-server xinit ;;
	esac
}

pkg_install_lightdm() {
	case "$PM" in
		apt) pkg_install lightdm lightdm-gtk-greeter ;;
		dnf|yum) pkg_install lightdm lightdm-gtk ;;
		pacman) pkg_install lightdm lightdm-gtk-greeter ;;
		zypper) pkg_install lightdm lightdm-gtk-greeter ;;
	esac
}

# enable_display_manager <dm> - make <dm> THE display manager: take over
# the display-manager.service alias (disabling whichever DM held it) and
# boot to graphical.target. Fedora/Arch/openSUSE never enable a freshly
# installed DM on their own; Debian's debconf may not either when run
# non-interactively.
enable_display_manager() {
	local dm="$1" cur
	cur="$(systemctl show -p Id --value display-manager.service 2>/dev/null || true)"
	cur="${cur%.service}"
	if [ -n "$cur" ] && [ "$cur" != "display-manager" ] && [ "$cur" != "$dm" ] && dm_unit_exists "$cur"; then
		info "Disabling ${cur} in favour of ${dm}..."
		systemctl disable "${cur}.service" >/dev/null 2>&1 || true
	fi
	if [ -f /etc/X11/default-display-manager ]; then
		command -v "$dm" >/dev/null 2>&1 && command -v "$dm" > /etc/X11/default-display-manager
	fi
	# openSUSE picks its DM through /etc/sysconfig/displaymanager.
	if [ -f /etc/sysconfig/displaymanager ]; then
		sed -i "s/^DISPLAYMANAGER=.*/DISPLAYMANAGER=\"${dm}\"/" /etc/sysconfig/displaymanager || true
		command -v update-alternatives >/dev/null 2>&1 && update-alternatives --set default-displaymanager "/usr/lib/X11/displaymanagers/${dm}" >/dev/null 2>&1 || true
	fi
	systemctl enable --force "${dm}.service"
	systemctl set-default graphical.target >/dev/null 2>&1 || true
	return 0
}

pkg_install_x11_companion() {
	local de="$1" cands pkg
	cands="$(companion_candidates "$de")"
	# shellcheck disable=SC2086
	pkg="$(first_available $cands)" || {
		error "No X11 session package is available for ${de} on this distribution (GNOME 49+ and recent Plasma releases no longer ship one). Install Xfce/Cinnamon/MATE alongside instead."
		return 1
	}
	pkg_install "$(xorg_server_pkg)" "$pkg"
}

pkg_install_de() {
	local de="$1"
	case "$de:$PM" in
		xfce:apt) pkg_install xfce4 xfce4-terminal ;;
		cinnamon:apt) pkg_install cinnamon-core || pkg_install cinnamon ;;
		mate:apt) pkg_install mate-desktop-environment-core || pkg_install mate-desktop-environment ;;
		xfce:dnf|xfce:yum) "$PM" install -y @xfce-desktop-environment || "$PM" group install -y xfce-desktop || pkg_install xfce4-session xfce4-panel xfdesktop xfce4-settings xfwm4 xfce4-terminal ;;
		cinnamon:dnf|cinnamon:yum) "$PM" install -y @cinnamon-desktop-environment || "$PM" group install -y cinnamon-desktop || pkg_install cinnamon ;;
		mate:dnf|mate:yum) "$PM" install -y @mate-desktop-environment || "$PM" group install -y mate-desktop || pkg_install mate-session-manager mate-panel marco caja ;;
		xfce:pacman) pkg_install xfce4 xfce4-goodies ;;
		cinnamon:pacman) pkg_install cinnamon ;;
		mate:pacman) pkg_install mate mate-extra ;;
		xfce:zypper) zypper --non-interactive install -t pattern xfce || pkg_install xfce4-session xfce4-panel xfdesktop xfwm4 ;;
		cinnamon:zypper) zypper --non-interactive install -t pattern cinnamon || pkg_install cinnamon ;;
		mate:zypper) zypper --non-interactive install -t pattern mate || pkg_install mate-session-manager ;;
		*)
			error "Unknown desktop '${de}' - expected xfce, cinnamon, or mate."
			return 1
			;;
	esac
}

# de_core_packages <de> - what "remove this desktop" means per distro: the
# core session packages only. Display managers are never removed (they may
# be driving the login screen right now); the new DM simply takes over.
de_core_packages() {
	case "$1:$PM" in
		gnome:apt) echo "ubuntu-desktop ubuntu-desktop-minimal gnome-shell gnome-session" ;;
		gnome:dnf|gnome:yum|gnome:zypper) echo "gnome-shell" ;;
		gnome:pacman) echo "gnome-shell" ;;
		plasma:apt) echo "kde-plasma-desktop plasma-desktop plasma-workspace" ;;
		plasma:dnf|plasma:yum) echo "plasma-workspace" ;;
		plasma:pacman) echo "plasma-workspace" ;;
		plasma:zypper) echo "plasma6-workspace plasma5-workspace" ;;
		xfce:apt) echo "xfce4 xfce4-session" ;;
		xfce:pacman) echo "xfce4-session" ;;
		xfce:*) echo "xfce4-session" ;;
		cinnamon:apt) echo "cinnamon-core cinnamon" ;;
		cinnamon:*) echo "cinnamon" ;;
		mate:apt) echo "mate-desktop-environment mate-desktop-environment-core mate-session-manager" ;;
		mate:*) echo "mate-session-manager" ;;
	esac
}

remove_desktop_environment() {
	local de="$1" pkgs
	pkgs="$(de_core_packages "$de")"
	if [ -z "$pkgs" ]; then
		error "Don't know how to safely remove '${de}' automatically - leaving it installed alongside the new desktop."
		return 0
	fi
	# shellcheck disable=SC2086
	pkg_remove $pkgs || error "Removing ${de} did not complete cleanly - the new desktop is installed and set as default either way."
	if [ "$PM" = "apt" ]; then
		DEBIAN_FRONTEND=noninteractive apt-get autoremove -y >/dev/null 2>&1 || true
	fi
	return 0
}

xsession_exists_for() {
	local f
	for f in /usr/share/xsessions/"$1"*.desktop; do
		[ -e "$f" ] && return 0
	done
	return 1
}

set_ini_key() {
	# set_ini_key <file> <section> <key> <value>
	local file="$1" section="$2" key="$3" value="$4"
	mkdir -p "$(dirname "$file")"
	[ -f "$file" ] || : > "$file"
	if awk -v s="[$section]" -v k="$key" '
		$0 == s { in_s = 1; next }
		/^\[/ { in_s = 0 }
		in_s && $0 ~ "^" k "=" { found = 1 }
		END { exit found ? 0 : 1 }' "$file"; then
		awk -v s="[$section]" -v k="$key" -v v="$value" '
			$0 == s { in_s = 1; print; next }
			/^\[/ { in_s = 0 }
			in_s && $0 ~ "^" k "=" { print k "=" v; next }
			{ print }' "$file" > "$file.nivaroos.tmp" && cat "$file.nivaroos.tmp" > "$file" && rm -f "$file.nivaroos.tmp"
	elif grep -qxF "[$section]" "$file"; then
		awk -v s="[$section]" -v k="$key" -v v="$value" '
			{ print }
			$0 == s && !done { print k "=" v; done = 1 }' "$file" > "$file.nivaroos.tmp" && cat "$file.nivaroos.tmp" > "$file" && rm -f "$file.nivaroos.tmp"
	else
		printf '\n[%s]\n%s=%s\n' "$section" "$key" "$value" >> "$file"
	fi
	return 0
}

conf_has_key() {
	# conf_has_key <key-regex> <files...>
	local re="$1"
	shift
	grep -hsE "^[[:space:]]*${re}[[:space:]]*=[[:space:]]*[^[:space:]]" "$@" >/dev/null 2>&1
}

configure_gdm_x11() {
	local session="$1" gdm_conf="" f orig
	for f in /etc/gdm3/custom.conf /etc/gdm3/daemon.conf /etc/gdm/custom.conf; do
		[ -f "$f" ] && { gdm_conf="$f"; break; }
	done
	[ -n "$gdm_conf" ] || { gdm_conf=/etc/gdm/custom.conf; [ -d /etc/gdm3 ] && gdm_conf=/etc/gdm3/custom.conf; }
	mkdir -p "$(dirname "$gdm_conf")" "$LIB_DIR"
	[ -f "$gdm_conf" ] || : > "$gdm_conf"
	if [ ! -f "$GDM_MARKER" ]; then
		orig="$(awk '/^\[daemon\]/{d=1;next} /^\[/{d=0} d && /^WaylandEnable=/{print; exit}' "$gdm_conf")"
		printf '%s\n%s\n' "$gdm_conf" "${orig:-absent}" > "$GDM_MARKER"
	fi
	set_ini_key "$gdm_conf" daemon WaylandEnable false
	# GDM remembers each user's last session in AccountsService - point
	# them at the X11 session so the next login lands on it.
	local u uid
	for f in /var/lib/AccountsService/users/*; do
		[ -f "$f" ] || continue
		u="$(basename "$f")"
		uid="$(id -u "$u" 2>/dev/null || echo 0)"
		[ "$uid" -ge 1000 ] || continue
		set_ini_key "$f" User XSession "$session"
		set_ini_key "$f" User Session "$session"
	done
	return 0
}

configure_default_x11_session() {
	detect_dm
	detect_de
	detect_xsession
	local session="$DETECTED_XSESSION_NAME"
	if [ -n "${1:-}" ]; then
		local f
		for f in /usr/share/xsessions/"$1"*.desktop; do
			[ -e "$f" ] || continue
			case "$f" in *wayland*) continue ;; esac
			session="$(basename "$f" .desktop)"
			break
		done
	fi
	[ -n "$session" ] || return 0
	case "$DETECTED_DM" in
		lightdm)
			mkdir -p "$(dirname "$LIGHTDM_CONF")"
			{
				echo "# Written by NivaroOS Host Desktop (removed by nivaroos-uninstall)"
				echo "[Seat:*]"
				echo "user-session=${session}"
				if conf_has_key 'autologin-user' /etc/lightdm/lightdm.conf /etc/lightdm/lightdm.conf.d/*.conf; then
					echo "autologin-session=${session}"
				fi
			} > "$LIGHTDM_CONF"
			;;
		gdm3|gdm)
			configure_gdm_x11 "$session"
			;;
		sddm)
			mkdir -p "$(dirname "$SDDM_CONF")"
			{
				echo "# Written by NivaroOS Host Desktop (removed by nivaroos-uninstall)"
				echo "[General]"
				echo "DisplayServer=x11"
				if conf_has_key 'User' /etc/sddm.conf /etc/sddm.conf.d/*.conf /usr/lib/sddm/sddm.conf.d/*.conf; then
					echo "[Autologin]"
					echo "Session=${session}.desktop"
				fi
			} > "$SDDM_CONF"
			# Pre-select the X11 session on the greeter.
			if [ -d /var/lib/sddm ]; then
				set_ini_key /var/lib/sddm/state.conf Last Session "/usr/share/xsessions/${session}.desktop" || true
				chown sddm: /var/lib/sddm/state.conf 2>/dev/null || true
			fi
			;;
	esac
	return 0
}

record_provisioned() {
	mkdir -p "$LIB_DIR"
	echo "$1" > "$PROVISION_MARKER"
}

do_install() {
	detect_display_server_support
	local prior_de="$DETECTED_DE_NAME" prior_dm="$DETECTED_DM" prior_dm_enabled="$DM_ENABLED"

	case "$ACTION" in
		x11-companion)
			if [ "$DE_SUPPORT_STATE" != "wayland_only" ]; then
				error "x11-companion only applies when --status reports state=wayland_only (currently: ${DE_SUPPORT_STATE})."
				exit 1
			fi
			info "Adding an X11 session to your existing ${DETECTED_DE_NAME} desktop..."
			ensure_apt_universe_enabled
			pkg_update
			pkg_install_x11_companion "$DETECTED_DE_NAME"
			configure_default_x11_session "$DETECTED_DE_NAME"
			;;
		alongside|replace)
			case "$DE_CHOICE" in
				xfce|cinnamon|mate) ;;
				*) error "${ACTION} requires --de=xfce|cinnamon|mate"; exit 1 ;;
			esac
			if [ "$ACTION" = "replace" ]; then
				if [ -z "$prior_de" ] || [ "$CONFIRM_NAME" != "$prior_de" ]; then
					error "replace requires --confirm=<current-de-name> matching exactly what --status reports as de_name (currently: '${prior_de}'). Refusing to guess on a destructive action."
					exit 1
				fi
				if [ "$prior_de" = "$DE_CHOICE" ]; then
					error "${DE_CHOICE} is already the current desktop."
					exit 1
				fi
			fi
			info "Installing ${DE_CHOICE} for Host Desktop streaming..."
			ensure_apt_universe_enabled
			pkg_update
			pkg_install_xorg_stack
			pkg_install_de "$DE_CHOICE"
			if ! xsession_exists_for "$DE_CHOICE"; then
				error "${DE_CHOICE} was installed but no X11 session file for it appeared in /usr/share/xsessions - stopping here, nothing was removed."
				exit 1
			fi
			# Display manager: install LightDM when there is none, or when
			# replacing a desktop whose own DM (GDM/SDDM) belongs to it; make
			# sure whichever DM ends up in charge is actually enabled
			# (Fedora/Arch/openSUSE never enable a freshly installed one).
			if [ "$prior_dm" = "none" ] || { [ "$ACTION" = "replace" ] && [ "$prior_dm" != "lightdm" ]; }; then
				pkg_install_lightdm
				enable_display_manager lightdm
			elif [ "$prior_dm_enabled" != "true" ]; then
				enable_display_manager "$prior_dm"
			fi
			if [ "$ACTION" = "replace" ]; then
				info "New desktop verified - now removing ${prior_de}..."
				remove_desktop_environment "$prior_de"
				record_provisioned "replaced:${DE_CHOICE}"
			else
				record_provisioned "alongside:${DE_CHOICE}"
			fi
			systemctl daemon-reload >/dev/null 2>&1 || true
			configure_default_x11_session "$DE_CHOICE"
			;;
		*)
			error "Unknown or missing --action (expected x11-companion, alongside, or replace). Use --status to check detection first, or --help for usage."
			exit 1
			;;
	esac

	detect_dm
	detect_de
	detect_xsession
	if [ -n "$DETECTED_XSESSION_NAME" ]; then
		info "Done. X11 session ready for streaming: ${DETECTED_XSESSION_NAME} (display manager: ${DETECTED_DM})."
		info "Log out (or reboot) and sign in to the X11 session for Host Desktop to show it."
	else
		error "Provisioning ran, but no X11 session file could be found afterwards. A reboot may be needed - check again with --status."
		exit 1
	fi
}

do_install
