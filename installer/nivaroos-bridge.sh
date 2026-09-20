#!/usr/bin/env bash
# ==============================================================================
#  nivaroos-bridge - CLI-only VM bridge network setup
#
#  Creates a Linux bridge over a physical Ethernet NIC for VM Manager's
#  bridged networking (so guests get their own LAN IP via DHCP, not NAT).
#  The bridge is given the SAME MAC address as the physical NIC it replaces
#  on the network - most routers/DHCP servers key leases off MAC, so without
#  this the host would silently get a brand new lease/IP the moment the
#  bridge takes over, which is the exact "IP issue" this script exists to
#  avoid.
#
#  Deliberately CLI-only, not exposed in the WebUI: this touches the same
#  interface a remote admin's SSH/dashboard session is running over, so a
#  bad config can cut that session off entirely - something only worth doing
#  from a terminal, with a console/IPMI fallback available, not a button in
#  a web page that could vanish along with the connection that loaded it.
#
#  Safety model (mirrors what NetworkManager's own `nmcli device checkpoint`
#  does for NM-managed devices, generalized here to work under NetworkManager,
#  netplan, systemd-networkd, or classic /etc/network/interfaces alike, since
#  NM's checkpoint only ever covers NM-managed devices):
#    1. Back up whatever config currently owns the chosen NIC.
#    2. Apply the bridge.
#    3. Start a WATCHDOG, detached from this terminal (setsid+nohup, so it
#       keeps running even if this SSH session is the very thing that just
#       died), that automatically restores the backup if nobody confirms
#       within a timeout (default 180s).
#    4. Only a typed "CONFIRM" from a session that's still alive cancels
#       the watchdog. Silence, a dropped connection, or Ctrl-C all mean
#       "this broke something" and get reverted automatically.
#
#  Usage:
#    sudo nivaroos-bridge                  # interactive wizard
#    sudo nivaroos-bridge --list           # list candidate physical NICs, change nothing
#    sudo nivaroos-bridge --status         # show existing bridges + pending rollbacks
#    sudo nivaroos-bridge --confirm        # cancel the most recent pending auto-rollback
#    sudo nivaroos-bridge --rollback       # immediately restore the most recent backup
#    sudo nivaroos-bridge --bridge-name=br1 --timeout=300
# ==============================================================================

if [ -z "${BASH_VERSION:-}" ]; then
	exec bash "$0" "$@"
fi

set -Eeuo pipefail

IS_TTY="false"
if [ -t 1 ] && [ "${TERM:-}" != "dumb" ] && [ -z "${NO_COLOR:-}" ]; then
	IS_TTY="true"
fi
if [ "$IS_TTY" = "true" ]; then
	C_RESET='\033[0m'; C_CYAN='\033[38;5;51m'; C_BOLD='\033[1m'
	C_GREEN='\033[38;5;48m'; C_YELLOW='\033[38;5;220m'; C_RED='\033[38;5;196m'
else
	C_RESET=''; C_CYAN=''; C_BOLD=''; C_GREEN=''; C_YELLOW=''; C_RED=''
fi
info()    { printf '%b\n' "${C_CYAN}i${C_RESET}  $1"; }
success() { printf '%b\n' "${C_GREEN}OK${C_RESET}  $1"; }
warn()    { printf '%b\n' "${C_YELLOW}!${C_RESET}  $1" >&2; }
error()   { printf '%b\n' "${C_RED}x${C_RESET}  $1" >&2; }
log()     { printf '%s  %s\n' "$(date -Iseconds)" "$1" >> "$LOG_FILE"; }

BACKUP_ROOT="/var/backups/nivaroos-bridge"
LOG_FILE="/var/log/nivaroos-bridge.log"
BRIDGE_NAME="br0"
ROLLBACK_TIMEOUT=180
ACTION="wizard"
TARGET_DIR=""

usage() {
	cat <<'EOF'
Usage: nivaroos-bridge [--list|--status|--confirm[=DIR]|--rollback[=DIR]]
                        [--bridge-name=NAME] [--timeout=SECONDS]

  (no flags)        Interactive wizard: pick a NIC, create a bridge that
                     clones its MAC address, with automatic rollback if not
                     confirmed within the timeout.
  --list            List candidate physical Ethernet interfaces. Read-only.
  --status          Show existing bridges and any pending auto-rollback. Read-only.
  --confirm[=DIR]   Cancel the pending auto-rollback (most recent, or DIR).
  --rollback[=DIR]  Immediately restore the backed-up config (most recent, or DIR).
  --bridge-name=X   Bridge interface name to create (default: br0).
  --timeout=N       Seconds to wait for confirmation before auto-reverting (default: 180).
EOF
}

for arg in "$@"; do
	case "$arg" in
		--list) ACTION="list" ;;
		--status) ACTION="status" ;;
		--confirm) ACTION="confirm" ;;
		--confirm=*) ACTION="confirm"; TARGET_DIR="${arg#*=}" ;;
		--rollback) ACTION="rollback" ;;
		--rollback=*) ACTION="rollback"; TARGET_DIR="${arg#*=}" ;;
		--bridge-name=*) BRIDGE_NAME="${arg#*=}" ;;
		--timeout=*) ROLLBACK_TIMEOUT="${arg#*=}" ;;
		--watchdog-run) ACTION="watchdog" ;;
		--watchdog-run=*) ACTION="watchdog"; TARGET_DIR="${arg#*=}" ;;
		--help|-h) usage; exit 0 ;;
		*) error "Unknown argument: $arg"; usage; exit 1 ;;
	esac
done

NEEDS_ROOT="true"
case "$ACTION" in list|status) NEEDS_ROOT="false" ;; esac
if [ "$NEEDS_ROOT" = "true" ] && [ "$(id -u)" -ne 0 ]; then
	if command -v sudo >/dev/null 2>&1; then
		exec sudo -E bash "$0" "$@"
	else
		error "This needs root - re-run as root or with sudo."
		exit 1
	fi
fi
mkdir -p "$BACKUP_ROOT" "$(dirname "$LOG_FILE")"

# ------------------------------------------------------------------------------
# Backend detection
# ------------------------------------------------------------------------------
detect_backend() {
	if systemctl is-active --quiet NetworkManager 2>/dev/null; then
		BACKEND="networkmanager"
	elif command -v netplan >/dev/null 2>&1 && [ -d /etc/netplan ] && ls /etc/netplan/*.yaml >/dev/null 2>&1; then
		BACKEND="netplan"
	elif systemctl is-active --quiet systemd-networkd 2>/dev/null; then
		BACKEND="networkd"
	elif [ -f /etc/network/interfaces ]; then
		BACKEND="ifupdown"
	else
		BACKEND="unknown"
	fi
}

# ------------------------------------------------------------------------------
# Interface discovery - physical Ethernet only. A real NIC has a "device"
# symlink (points at its PCI/USB device); virtual interfaces (bridges,
# veth pairs, docker0, tun/tap, VPN interfaces) never do. Wi-Fi is excluded
# too - 802.11 can't be bridged at layer 2 the way Ethernet can without
# special (and unreliable) 4-address-mode support.
# ------------------------------------------------------------------------------
list_candidate_interfaces() {
	local iface name
	for iface in /sys/class/net/*; do
		name="$(basename "$iface")"
		[ "$name" = "lo" ] && continue
		[ -e "$iface/device" ] || continue
		[ -d "$iface/bridge" ] && continue
		[ -d "$iface/wireless" ] && continue
		[ -e "$iface/master" ] && continue
		echo "$name"
	done
}

describe_interface() {
	local nic="$1" mac carrier ip4
	mac="$(cat "/sys/class/net/$nic/address" 2>/dev/null || echo "unknown")"
	if [ "$(cat "/sys/class/net/$nic/carrier" 2>/dev/null || echo 0)" = "1" ]; then
		carrier="link up"
	else
		carrier="no link"
	fi
	ip4="$(ip -4 -o addr show dev "$nic" 2>/dev/null | awk '{print $4}' | head -n1)"
	[ -z "$ip4" ] && ip4="no IPv4"
	printf '%-10s  mac %-17s  %-8s  %s\n' "$nic" "$mac" "$carrier" "$ip4"
}

default_gateway_for() {
	ip route show dev "$1" 2>/dev/null | awk '/^default/ {print $3; exit}'
	ip route show default 2>/dev/null | awk -v n="$1" '$0 ~ n {print $3; exit}'
}

# ------------------------------------------------------------------------------
# Backup + rollback script generation
# ------------------------------------------------------------------------------
backup_current_config() {
	local backend="$1" nic="$2" backup_dir="$3"
	mkdir -p "$backup_dir"
	echo "$backend" > "$backup_dir/backend"
	echo "$nic" > "$backup_dir/nic"
	echo "$BRIDGE_NAME" > "$backup_dir/bridge_name"

	case "$backend" in
		networkmanager)
			mkdir -p "$backup_dir/system-connections"
			cp -a /etc/NetworkManager/system-connections/. "$backup_dir/system-connections/" 2>/dev/null || true
			;;
		netplan)
			mkdir -p "$backup_dir/netplan"
			cp -a /etc/netplan/. "$backup_dir/netplan/" 2>/dev/null || true
			;;
		networkd)
			mkdir -p "$backup_dir/network"
			cp -a /etc/systemd/network/. "$backup_dir/network/" 2>/dev/null || true
			;;
		ifupdown)
			cp -a /etc/network/interfaces "$backup_dir/interfaces" 2>/dev/null || true
			;;
	esac

	local rb="$backup_dir/rollback.sh"
	{
		echo '#!/bin/bash'
		echo 'set -e'
		case "$backend" in
			networkmanager)
				cat <<EOF
nmcli con down "${BRIDGE_NAME}" >/dev/null 2>&1 || true
nmcli con delete "${BRIDGE_NAME}" >/dev/null 2>&1 || true
nmcli con delete "${BRIDGE_NAME}-port-${nic}" >/dev/null 2>&1 || true
rm -f /etc/NetworkManager/system-connections/${BRIDGE_NAME}*.nmconnection
cp -a "${backup_dir}/system-connections/." /etc/NetworkManager/system-connections/ 2>/dev/null || true
systemctl restart NetworkManager
EOF
				;;
			netplan)
				cat <<EOF
rm -f /etc/netplan/90-nivaroos-bridge.yaml
cp -a "${backup_dir}/netplan/." /etc/netplan/ 2>/dev/null || true
netplan apply
EOF
				;;
			networkd)
				cat <<EOF
rm -f /etc/systemd/network/90-nivaroos-${BRIDGE_NAME}*
cp -a "${backup_dir}/network/." /etc/systemd/network/ 2>/dev/null || true
systemctl restart systemd-networkd
EOF
				;;
			ifupdown)
				cat <<EOF
ip link set "${BRIDGE_NAME}" down 2>/dev/null || true
brctl delbr "${BRIDGE_NAME}" 2>/dev/null || ip link delete "${BRIDGE_NAME}" 2>/dev/null || true
cp -a "${backup_dir}/interfaces" /etc/network/interfaces
(systemctl restart networking 2>/dev/null) || (ifdown -a 2>/dev/null; ifup -a 2>/dev/null) || true
EOF
				;;
		esac
	} > "$rb"
	chmod 700 "$rb"
}

# ------------------------------------------------------------------------------
# Per-backend bridge creation - all three set the bridge's MAC to the
# physical NIC's original MAC, and leave DHCP to hand it the same lease.
# ------------------------------------------------------------------------------
apply_networkmanager() {
	local nic="$1" brname="$2" mac="$3"
	nmcli con add type bridge con-name "$brname" ifname "$brname" bridge.stp no
	nmcli con modify "$brname" bridge.mac-address "$mac" ipv4.method auto ipv6.method auto connection.autoconnect yes
	nmcli con add type ethernet con-name "${brname}-port-${nic}" ifname "$nic" master "$brname"
	nmcli con up "$brname"
	nmcli con up "${brname}-port-${nic}"
}

apply_netplan() {
	local nic="$1" brname="$2" mac="$3"
	cat > /etc/netplan/90-nivaroos-bridge.yaml <<EOF
network:
  version: 2
  ethernets:
    ${nic}:
      dhcp4: no
      dhcp6: no
  bridges:
    ${brname}:
      interfaces: [${nic}]
      macaddress: ${mac}
      dhcp4: yes
      dhcp6: yes
      parameters:
        stp: false
        forward-delay: 0
EOF
	chmod 600 /etc/netplan/90-nivaroos-bridge.yaml
	netplan apply
}

apply_networkd() {
	local nic="$1" brname="$2" mac="$3"
	cat > "/etc/systemd/network/90-nivaroos-${brname}.netdev" <<EOF
[NetDev]
Name=${brname}
Kind=bridge
MACAddress=${mac}
EOF
	cat > "/etc/systemd/network/90-nivaroos-${brname}-port.network" <<EOF
[Match]
Name=${nic}

[Network]
Bridge=${brname}
EOF
	cat > "/etc/systemd/network/90-nivaroos-${brname}.network" <<EOF
[Match]
Name=${brname}

[Network]
DHCP=yes
EOF
	systemctl restart systemd-networkd
}

apply_ifupdown() {
	local nic="$1" brname="$2" mac="$3"
	cat >> /etc/network/interfaces <<EOF

# --- nivaroos-bridge: ${brname} over ${nic} (added $(date -Iseconds)) ---
auto ${brname}
iface ${brname} inet dhcp
    bridge_ports ${nic}
    hwaddress ether ${mac}
EOF
	ifdown "$nic" 2>/dev/null || true
	(systemctl restart networking 2>/dev/null) || ifup "$brname"
}

# ------------------------------------------------------------------------------
# Watchdog - runs detached from the invoking terminal (see start_watchdog),
# so a bridge that breaks the very SSH session it was started from still
# gets reverted right on schedule.
# ------------------------------------------------------------------------------
watchdog_run() {
	local backup_dir="$1"
	sleep "$ROLLBACK_TIMEOUT"
	if [ -f "$backup_dir/confirmed" ]; then
		log "watchdog: $backup_dir already confirmed, nothing to do."
		return 0
	fi
	log "watchdog: no confirmation for $backup_dir within ${ROLLBACK_TIMEOUT}s - rolling back."
	bash "$backup_dir/rollback.sh" >> "$LOG_FILE" 2>&1 || log "watchdog: rollback.sh for $backup_dir exited non-zero."
	touch "$backup_dir/rolled-back"
}

start_watchdog() {
	local backup_dir="$1"
	setsid nohup bash "$0" --watchdog-run="$backup_dir" --timeout="$ROLLBACK_TIMEOUT" </dev/null >>"$LOG_FILE" 2>&1 &
	disown
	log "watchdog started for $backup_dir (pid $!, timeout ${ROLLBACK_TIMEOUT}s)."
}

latest_pending_backup() {
	local d
	for d in $(ls -1dt "$BACKUP_ROOT"/*/ 2>/dev/null); do
		d="${d%/}"
		if [ ! -f "$d/confirmed" ] && [ ! -f "$d/rolled-back" ]; then
			echo "$d"
			return 0
		fi
	done
	return 1
}

latest_backup() {
	ls -1dt "$BACKUP_ROOT"/*/ 2>/dev/null | head -n1 | sed 's:/$::'
}

# ------------------------------------------------------------------------------
# Actions
# ------------------------------------------------------------------------------
action_list() {
	detect_backend
	info "Network backend detected: ${BACKEND}"
	local ifaces
	ifaces="$(list_candidate_interfaces)"
	if [ -z "$ifaces" ]; then
		warn "No physical Ethernet interfaces found (Wi-Fi and virtual interfaces are excluded)."
		return 0
	fi
	printf '\n'
	local nic
	while IFS= read -r nic; do
		describe_interface "$nic"
	done <<< "$ifaces"
}

action_status() {
	printf '%bExisting bridges:%b\n' "$C_BOLD" "$C_RESET"
	local br found=false
	for br in /sys/class/net/*/bridge; do
		[ -d "$br" ] || continue
		found=true
		local name mac
		name="$(basename "$(dirname "$br")")"
		mac="$(cat "$(dirname "$br")/address" 2>/dev/null)"
		printf '  %-10s mac %s  ports: %s\n' "$name" "$mac" "$(ls "$(dirname "$br")/brif" 2>/dev/null | tr '\n' ' ')"
	done
	[ "$found" = "false" ] && echo "  (none)"

	printf '\n%bPending auto-rollbacks:%b\n' "$C_BOLD" "$C_RESET"
	local pending
	pending="$(latest_pending_backup 2>/dev/null || true)"
	if [ -n "$pending" ]; then
		local nic brn
		nic="$(cat "$pending/nic" 2>/dev/null)"
		brn="$(cat "$pending/bridge_name" 2>/dev/null)"
		echo "  $pending  (bridge=$brn over $nic - not yet confirmed)"
		echo "  Run 'nivaroos-bridge --confirm' to keep it, or 'nivaroos-bridge --rollback' to revert it now."
	else
		echo "  (none)"
	fi
}

action_confirm() {
	local dir="$TARGET_DIR"
	if [ -z "$dir" ]; then
		dir="$(latest_pending_backup)" || { error "No pending bridge setup to confirm."; exit 1; }
	fi
	[ -d "$dir" ] || { error "No such backup directory: $dir"; exit 1; }
	if [ -f "$dir/rolled-back" ]; then
		error "$dir was already rolled back - nothing to confirm."
		exit 1
	fi
	touch "$dir/confirmed"
	success "Confirmed. $dir will not be auto-reverted."
}

action_rollback() {
	local dir="$TARGET_DIR"
	if [ -z "$dir" ]; then
		dir="$(latest_backup)" || { error "No nivaroos-bridge backup found to roll back to."; exit 1; }
	fi
	[ -d "$dir" ] && [ -f "$dir/rollback.sh" ] || { error "No rollback script at $dir"; exit 1; }
	warn "Rolling back to the network config backed up at $dir ..."
	bash "$dir/rollback.sh"
	touch "$dir/rolled-back"
	success "Rolled back. Previous network config restored."
}

action_watchdog() {
	[ -n "$TARGET_DIR" ] || { error "watchdog invoked with no backup dir"; exit 1; }
	watchdog_run "$TARGET_DIR"
}

action_wizard() {
	detect_backend
	if [ "$BACKEND" = "unknown" ]; then
		error "No supported network backend detected (checked: NetworkManager, netplan, systemd-networkd, /etc/network/interfaces)."
		exit 1
	fi
	info "Network backend detected: ${BACKEND}"

	mapfile -t IFACES < <(list_candidate_interfaces)
	if [ "${#IFACES[@]}" -eq 0 ]; then
		error "No physical Ethernet interfaces found - nothing to bridge."
		exit 1
	fi

	printf '\nAvailable Ethernet interfaces:\n\n'
	local i
	for i in "${!IFACES[@]}"; do
		printf '  %d) %s\n' "$((i + 1))" "$(describe_interface "${IFACES[$i]}")"
	done
	printf '\n'

	local choice nic
	while true; do
		read -r -p "Pick an interface to bridge [1-${#IFACES[@]}]: " choice
		if [[ "$choice" =~ ^[0-9]+$ ]] && [ "$choice" -ge 1 ] && [ "$choice" -le "${#IFACES[@]}" ]; then
			nic="${IFACES[$((choice - 1))]}"
			break
		fi
		warn "Enter a number between 1 and ${#IFACES[@]}."
	done

	local orig_mac orig_gw
	orig_mac="$(cat "/sys/class/net/$nic/address")"
	orig_gw="$(default_gateway_for "$nic")"

	printf '\n%b=====================================================================%b\n' "$C_YELLOW" "$C_RESET"
	printf '%b This reconfigures %s into bridge "%s" and restarts networking.%b\n' "$C_YELLOW" "$nic" "$BRIDGE_NAME" "$C_RESET"
	printf '%b If you are connected over %s (e.g. via SSH), THIS SESSION MAY DROP.%b\n' "$C_YELLOW" "$nic" "$C_RESET"
	printf '%b A watchdog will auto-revert in %ss if not confirmed.%b\n' "$C_YELLOW" "$ROLLBACK_TIMEOUT" "$C_RESET"
	printf '%b Console/IPMI access is strongly recommended before continuing.%b\n' "$C_YELLOW" "$C_RESET"
	printf '%b=====================================================================%b\n\n' "$C_YELLOW" "$C_RESET"

	local typed
	read -r -p "Type the interface name (${nic}) to confirm and continue: " typed
	if [ "$typed" != "$nic" ]; then
		error "Confirmation text did not match - aborted, nothing was changed."
		exit 1
	fi

	local backup_dir="$BACKUP_ROOT/$(date +%Y%m%d-%H%M%S)"
	backup_current_config "$BACKEND" "$nic" "$backup_dir"
	log "backup written to $backup_dir for nic=$nic backend=$BACKEND bridge=$BRIDGE_NAME"

	start_watchdog "$backup_dir"

	info "Applying bridge ${BRIDGE_NAME} over ${nic} (cloned MAC ${orig_mac})..."
	case "$BACKEND" in
		networkmanager) apply_networkmanager "$nic" "$BRIDGE_NAME" "$orig_mac" ;;
		netplan) apply_netplan "$nic" "$BRIDGE_NAME" "$orig_mac" ;;
		networkd) apply_networkd "$nic" "$BRIDGE_NAME" "$orig_mac" ;;
		ifupdown) apply_ifupdown "$nic" "$BRIDGE_NAME" "$orig_mac" ;;
	esac

	sleep 5
	if [ -n "$orig_gw" ] && ping -c1 -W3 "$orig_gw" >/dev/null 2>&1; then
		success "Gateway ${orig_gw} is reachable over ${BRIDGE_NAME}."
	else
		warn "Could not reach the previous gateway (${orig_gw:-unknown}) over ${BRIDGE_NAME} yet - this can just mean DHCP needs a few more seconds, or it can mean the bridge isn't passing traffic."
	fi

	printf '\n'
	local ans=""
	if read -r -t "$ROLLBACK_TIMEOUT" -p "If this session and network access still work, type CONFIRM to keep this bridge (auto-revert otherwise in the remaining time): " ans && [ "$ans" = "CONFIRM" ]; then
		touch "$backup_dir/confirmed"
		success "Confirmed. ${BRIDGE_NAME} is now the bridge for ${nic} (MAC ${orig_mac}). Use 'bridge=${BRIDGE_NAME}' as a VM's network source in the dashboard."
	else
		warn "No confirmation received - the watchdog (running independently of this terminal) will restore your previous network config automatically."
		warn "If you're reading this from a new session after the old one dropped, you have until the timeout to run: nivaroos-bridge --confirm=${backup_dir}"
	fi
}

case "$ACTION" in
	list) action_list ;;
	status) action_status ;;
	confirm) action_confirm ;;
	rollback) action_rollback ;;
	watchdog) action_watchdog ;;
	wizard) action_wizard ;;
esac
