#!/usr/bin/env bash
# shellcheck disable=SC2016 # check() evals its single-quoted test
# nivaroos-host-firewall.sh against fake ip/ufw/firewall-cmd/nft in a temp
# dir (the live firewall is never touched; inside a throwaway network
# namespace too when unshare works). Run: bash installer/tests/host-firewall-test.sh
set -u
d="$(cd "$(dirname "$0")/.." && pwd)"
PASS=0 FAIL=0
check() { if eval "$1"; then PASS=$((PASS + 1)); else FAIL=$((FAIL + 1)); echo "FAIL: $2" >&2; fi; }
T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT
export FAKE="$T/state"
mkdir -p "$T/bin" "$FAKE"

cat >"$T/bin/ip" <<'EOF'
#!/bin/bash
case "$*" in
	"-o route show default") cat "$FAKE/route4" 2>/dev/null ;;
	"-6 -o route show default") cat "$FAKE/route6" 2>/dev/null ;;
	"-o link show") cat "$FAKE/links" 2>/dev/null ;;
esac
EOF
cat >"$T/bin/ufw" <<'EOF'
#!/bin/bash
r="$FAKE/ufw.rules"; touch "$r"
case "$1" in
	status) [ -e "$FAKE/ufw.on" ] && echo "Status: active" || echo "Status: inactive" ;;
	show) echo "Added user rules (see 'ufw status' for running firewall):"; cat "$r" ;;
	allow) shift; line="ufw allow ${*% comment *} comment '${*##* comment }'"
		grep -qxF "$line" "$r" || echo "$line" >>"$r" ;;
	delete) shift; grep -vF "ufw $* comment" "$r" >"$r.n"; mv "$r.n" "$r" ;;
esac
EOF
cat >"$T/bin/firewall-cmd" <<'EOF'
#!/bin/bash
[ -e "$FAKE/fwd.on" ] || exit 252
kind=runtime zone=public
for a in "$@"; do
	case "$a" in
		--state) echo running; exit 0 ;;
		--get-zones) echo "public trusted"; exit 0 ;;
		--get-default-zone) echo public; exit 0 ;;
		--get-zone-of-interface=*) z="$(grep "^${a#*=} " "$FAKE/fwd.ifzones" 2>/dev/null | cut -d' ' -f2)"; [ -n "$z" ] && echo "$z" && exit 0; exit 2 ;;
		--permanent) kind=permanent ;;
		--zone=*) zone="${a#*=}" ;;
	esac
done
f="$FAKE/fwd.$kind.$zone"; touch "$f"
case "$*" in
	*--add-port=*) grep -qx 28650/udp "$f" || echo 28650/udp >>"$f" ;;
	*--remove-port=*) grep -vx 28650/udp "$f" >"$f.n"; mv "$f.n" "$f" ;;
	*--query-port=*) grep -qx 28650/udp "$f" ;;
esac
EOF
cat >"$T/bin/nft" <<'EOF'
#!/bin/bash
case "$*" in
	"-f -") cat >"$FAKE/nft.table" ;;
	"delete table inet nivaroos_hostdesk") [ -e "$FAKE/nft.table" ] && rm "$FAKE/nft.table" ;;
esac
EOF
chmod +x "$T/bin/"*

WRAP=()
unshare -n true 2>/dev/null && WRAP=(unshare -n)
fw() { PATH="$T/bin:$PATH" "${WRAP[@]}" bash "$d/nivaroos-host-firewall.sh" "$@" 2>"$T/err"; }

echo "default via 192.168.10.1 dev br0 onlink" >"$FAKE/route4"
printf '1: lo: <LOOPBACK>\n5: tailscale0: <POINTOPOINT>\n7: docker0: <BROADCAST>\n9: virbr0: <BROADCAST>\n' >"$FAKE/links"

# No ufw/firewalld active: only the 5900 backstop.
fw apply
check '[ ! -s "$FAKE/ufw.rules" ] && ! ls "$FAKE"/fwd.* >/dev/null 2>&1' "inactive firewalls: no 28650 rule"
check 'grep -q "tcp dport 5900 drop" "$FAKE/nft.table" && grep -q "iifname != \"lo\"" "$FAKE/nft.table"' "backstop drops 5900 except on loopback"
check 'grep -q "^table inet nivaroos_hostdesk" "$FAKE/nft.table" && grep -q "^delete table inet nivaroos_hostdesk" "$FAKE/nft.table"' "backstop replaces its own table atomically"

# ufw active: br0 + tailscale0 only, idempotent.
touch "$FAKE/ufw.on"
fw apply
fw apply
check '[ "$(grep -c 28650 "$FAKE/ufw.rules")" = 2 ]' "ufw: one rule per interface, idempotent (got: $(cat "$FAKE/ufw.rules"))"
check 'grep -q "in on br0 to any port 28650 proto udp" "$FAKE/ufw.rules" && grep -q "in on tailscale0 " "$FAKE/ufw.rules"' "ufw: LAN and Tailscale"
check '! grep -qE "docker0|virbr0|lo " "$FAKE/ufw.rules"' "ufw: never docker/libvirt/loopback"

# LAN interface changes: the old one's rule goes.
echo "default via 10.0.0.1 dev eth1" >"$FAKE/route4"
echo "ufw allow in on br0 to any port 22 proto tcp comment 'admin'" >>"$FAKE/ufw.rules"
fw apply
check '! grep -q "on br0 to any port 28650" "$FAKE/ufw.rules" && grep -q "on eth1 to any port 28650" "$FAKE/ufw.rules"' "ufw: stale interface removed"
check 'grep -q "port 22 proto tcp" "$FAKE/ufw.rules"' "ufw: other rules untouched"

# No default route (network not up at boot): keep what was there.
: >"$FAKE/route4"
: >"$FAKE/links"
fw apply
check 'grep -q "on eth1 to any port 28650" "$FAKE/ufw.rules"' "no interface found: rules kept"
check 'grep -q "left as it was" "$T/err"' "no interface found: warned"

# firewalld: the interfaces' zones (default zone when unassigned).
rm "$FAKE/ufw.on"
touch "$FAKE/fwd.on"
echo "default via 10.0.0.1 dev eth1" >"$FAKE/route4"
printf '5: tailscale0: <POINTOPOINT>\n' >"$FAKE/links"
echo "tailscale0 trusted" >"$FAKE/fwd.ifzones"
fw apply
fw apply
for k in runtime permanent; do
	check '[ "$(cat "$FAKE/fwd.$k.public")" = 28650/udp ] && [ "$(cat "$FAKE/fwd.$k.trusted")" = 28650/udp ]' "firewalld $k: public (eth1) + trusted (tailscale0), once"
done

# remove: everything of ours goes, the rest stays.
fw remove
check '[ ! -e "$FAKE/nft.table" ]' "remove: backstop table gone"
check '! grep -q 28650 "$FAKE/ufw.rules" && grep -q "port 22 proto tcp" "$FAKE/ufw.rules"' "remove: ufw rules gone, others kept"
check '! cat "$FAKE"/fwd.*.public "$FAKE"/fwd.*.trusted | grep -q 28650' "remove: firewalld ports gone"
check 'fw remove' "remove twice is fine"

check '! fw bogus' "unknown command fails"

# The unit runs apply and is installed/removed by the scripts.
check 'grep -q "^ExecStart=/usr/local/lib/nivaroos/nivaroos-host-firewall apply$" "$d/systemd/nivaroos-host-firewall.service"' "unit runs apply"
check 'grep -q "install_host_firewall install_ui" "$d/install.sh"' "install.sh always runs the firewall step"
check 'grep -q "nivaroos-host-firewall remove" "$d/uninstall.sh"' "uninstall.sh removes the rules"

echo "passed: $PASS  failed: $FAIL"
[ "$FAIL" -eq 0 ]
