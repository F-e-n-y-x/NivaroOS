#!/usr/bin/env bash
# nivaroos-host-firewall apply|remove - NivaroOS host firewall rules.
#  - Backstops in NivaroOS's own nftables table (inet: IPv4 and IPv6):
#    - TCP 5900 (x11vnc, audit C1) and 28641 (vm-sidecar, audit L7) dropped
#      on every interface but loopback. x11vnc only serves a unix socket and
#      the sidecar listens on 127.0.0.1 (the UI reaches it through the
#      gateway); this keeps a stray or old wide listener unreachable.
#    - TCP 28642 (Download Station: the Lite browser needs its own origin,
#      so the LAN and Tailscale reach it) dropped from container and VM
#      bridges (audit L7).
#    - The DS full browser's user (Chromium on untrusted pages) reaches
#      loopback only on its egress proxy ports and DNS: every NivaroOS API
#      trusts some loopback callers (audit H1).
#  - "Smooth (beta)" WebRTC: UDP 28650 opened for the LAN (interfaces with a
#    default route) and Tailscale, only where ufw or firewalld is active
#    (without them nothing blocks it). Never on docker/libvirt bridges.
# Idempotent; run at boot by nivaroos-host-firewall.service, removed by the
# uninstaller. Best effort: always exits 0 (a missing tool is a warning).
set -uo pipefail

PORT=28650
TAG=nivaroos-hostdesk-smooth
TABLE=nivaroos_hostdesk
# Container (docker, podman veth) and VM (libvirt) bridges and taps.
GUEST_BRIDGES=('docker*' 'br-*' 'veth*' 'virbr*' 'vnet*')
BROWSER_USER=nivaroos-browser
DS_EGRESS_PORTS=28660-28679 # services/download-sidecar/rb_egress.go

warn() { printf 'nivaroos-host-firewall: %s\n' "$*" >&2; }

# LAN = interfaces carrying a default route; Tailscale = tailscale*.
smooth_ifaces() {
	{
		ip -o route show default 2>/dev/null
		ip -6 -o route show default 2>/dev/null
	} | awk '{for (i = 1; i < NF; i++) if ($i == "dev") print $(i + 1)}'
	ip -o link show 2>/dev/null | awk -F': ' '$2 ~ /^tailscale/ {print $2}'
}

ufw_active() { command -v ufw >/dev/null && ufw status 2>/dev/null | grep -q '^Status: active'; }
firewalld_active() { command -v firewall-cmd >/dev/null && firewall-cmd --state >/dev/null 2>&1; }

ufw_ours() { # interfaces we opened the port on
	ufw show added 2>/dev/null | awk -v p="$PORT" -v t="$TAG" \
		'$2 == "allow" && $3 == "in" && $4 == "on" && $9 == p && index($0, t) {print $5}'
}

# The DS browser's output chain, once its user exists (install-ds-browser.sh
# re-applies after creating it).
browser_chain() {
	local uid
	uid="$(getent passwd "$BROWSER_USER" 2>/dev/null | cut -d: -f3)"
	[ -n "$uid" ] || return 0
	cat <<EOF
	chain output {
		type filter hook output priority -10; policy accept;
		meta skuid $uid ip daddr 127.0.0.0/8 tcp dport $DS_EGRESS_PORTS accept
		meta skuid $uid ip daddr 127.0.0.0/8 th dport 53 accept
		meta skuid $uid ip daddr 127.0.0.0/8 ct state new drop comment "DS browser: egress proxy only (audit H1)"
		meta skuid $uid ip6 daddr ::1 ct state new drop comment "DS browser: egress proxy only (audit H1)"
	}
EOF
}

backstop_apply() {
	command -v nft >/dev/null || { warn "nft not found - TCP 5900/28641/28642 backstops not installed (install nftables)"; return 0; }
	# "table; delete table; table {...}" in one transaction = atomic replace.
	nft -f - <<EOF || warn "nft: backstop rules failed"
table inet $TABLE
delete table inet $TABLE
table inet $TABLE {
	chain input {
		type filter hook input priority -10; policy accept;
		iifname != "lo" tcp dport 5900 drop comment "x11vnc backstop (audit C1)"
		iifname != "lo" tcp dport 28641 drop comment "vm-sidecar: gateway only (audit L7)"
$(for b in "${GUEST_BRIDGES[@]}"; do printf '\t\tiifname "%s" tcp dport 28642 drop comment "DS: not from containers/VMs (audit L7)"\n' "$b"; done)
	}
$(browser_chain)
}
EOF
}

smooth_remove() {
	local i z
	if command -v ufw >/dev/null; then
		for i in $(ufw_ours); do
			ufw delete allow in on "$i" to any port "$PORT" proto udp >/dev/null || warn "ufw delete on $i failed"
		done
	fi
	if firewalld_active; then
		for z in $(firewall-cmd --get-zones 2>/dev/null); do
			firewall-cmd --zone="$z" --query-port="$PORT/udp" >/dev/null 2>&1 &&
				firewall-cmd --zone="$z" --remove-port="$PORT/udp" >/dev/null
			firewall-cmd --permanent --zone="$z" --query-port="$PORT/udp" >/dev/null 2>&1 &&
				firewall-cmd --permanent --zone="$z" --remove-port="$PORT/udp" >/dev/null
		done
	fi
	return 0
}

smooth_apply() {
	local ifs i z
	ifs="$(smooth_ifaces | sort -u)"
	if ! ufw_active && ! firewalld_active; then return 0; fi
	# Network not up yet (no default route): keep what was opened before.
	[ -n "$ifs" ] || { warn "no LAN/Tailscale interface found - UDP $PORT left as it was"; return 0; }
	smooth_remove # drops interfaces that are gone
	if ufw_active; then
		for i in $ifs; do
			ufw allow in on "$i" to any port "$PORT" proto udp comment "$TAG" >/dev/null || warn "ufw allow on $i failed"
		done
	fi
	if firewalld_active; then
		# ponytail: firewalld opens per zone, so other interfaces sharing the
		# LAN's zone get the port too; a dedicated zone if that ever matters.
		for i in $ifs; do
			z="$(firewall-cmd --get-zone-of-interface="$i" 2>/dev/null || firewall-cmd --get-default-zone)"
			firewall-cmd --zone="$z" --add-port="$PORT/udp" >/dev/null || warn "firewalld $z failed"
			firewall-cmd --permanent --zone="$z" --add-port="$PORT/udp" >/dev/null || warn "firewalld --permanent $z failed"
		done
	fi
	return 0
}

case "${1:-}" in
	apply)
		backstop_apply
		smooth_apply
		;;
	remove)
		command -v nft >/dev/null && nft delete table inet "$TABLE" 2>/dev/null
		smooth_remove
		;;
	*)
		echo "usage: $0 apply|remove" >&2
		exit 2
		;;
esac
exit 0
