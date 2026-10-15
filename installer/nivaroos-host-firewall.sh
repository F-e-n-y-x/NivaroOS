#!/usr/bin/env bash
# nivaroos-host-firewall apply|remove - Host Desktop firewall rules.
#  - x11vnc backstop (audit C1/L7): TCP 5900 dropped on every interface but
#    loopback, IPv4 and IPv6, in NivaroOS's own nftables table. x11vnc only
#    serves a unix socket; this keeps a stray TCP listener unreachable.
#  - "Smooth (beta)" WebRTC: UDP 28650 opened for the LAN (interfaces with a
#    default route) and Tailscale, only where ufw or firewalld is active
#    (without them nothing blocks it). Never on docker/libvirt bridges.
# Idempotent; run at boot by nivaroos-host-firewall.service, removed by the
# uninstaller. Best effort: always exits 0 (a missing tool is a warning).
set -uo pipefail

PORT=28650
TAG=nivaroos-hostdesk-smooth
TABLE=nivaroos_hostdesk

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

backstop_apply() {
	command -v nft >/dev/null || { warn "nft not found - TCP 5900 backstop not installed (install nftables)"; return 0; }
	# "table; delete table; table {...}" in one transaction = atomic replace.
	nft -f - <<EOF || warn "nft: TCP 5900 backstop failed"
table inet $TABLE
delete table inet $TABLE
table inet $TABLE {
	chain input {
		type filter hook input priority -10; policy accept;
		iifname != "lo" tcp dport 5900 drop comment "x11vnc backstop (audit C1)"
	}
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
