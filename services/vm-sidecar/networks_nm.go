// networks_nm.go: bridged VM networks on a host whose NIC is run by
// NetworkManager (a desktop install hands the wired NIC to it so it shows in
// the desktop's network settings). ifupdown's interfaces.d snippet would fight
// NetworkManager over the same NIC, so here the bridge is two NivaroOS-owned
// connection profiles made with nmcli:
//
//	nivaroos-<bridge>       the bridge itself (DHCP, or the static IPv4 given)
//	nivaroos-<bridge>-port  hostNIC as a member of it
//
// The NIC's own profile (e.g. "Wired connection 1") is never edited: the port
// profile simply has a higher autoconnect priority, so it wins at boot, and
// removing both profiles lets NetworkManager reconnect the NIC with its own
// profile again.
package main

import (
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strings"
)

// nmcli runs NetworkManager's CLI. A var so tests can record calls instead.
var nmcli = func(args ...string) ([]byte, error) {
	return exec.Command("nmcli", args...).CombinedOutput()
}

// nmManages reports whether NetworkManager runs and manages hostNIC.
// A var so tests can pick the backend.
var nmManages = func(hostNIC string) bool {
	out, err := exec.Command("nmcli", "-t", "-f", "DEVICE,STATE", "device").Output()
	if err != nil {
		return false // nmcli missing or NetworkManager not running
	}
	return nmDeviceManaged(string(out), hostNIC)
}

// nmDeviceManaged parses `nmcli -t -f DEVICE,STATE device`: hostNIC is
// managed unless it is missing or "unmanaged" (e.g. ifupdown owns it through
// NetworkManager's ifupdown plugin with managed=false).
func nmDeviceManaged(out, hostNIC string) bool {
	for _, line := range strings.Split(out, "\n") {
		dev, state, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok && dev == hostNIC {
			return state != "unmanaged"
		}
	}
	return false
}

func nmBridgeProfile(name string) string { return "nivaroos-" + name }
func nmPortProfile(name string) string   { return "nivaroos-" + name + "-port" }

// nmIdentity is what a DHCP server knows hostNIC by: its MAC and the DHCP
// client ID its NetworkManager profile sends (if one is set explicitly).
// A DHCP bridge presents both, so the server hands back the same address
// the host had before the bridge - the host keeps its IP, over DHCP.
type nmIdentity struct{ MAC, ClientID string }

var clientIDRe = regexp.MustCompile(`^[0-9A-Za-z:._-]{1,255}$`)

// nmNICIdentity reads hostNIC's identity before the bridge takes it over.
// Anything it can't read is left empty (the bridge then falls back to
// NetworkManager's defaults).
func nmNICIdentity(nic string) nmIdentity {
	var id nmIdentity
	if mac, err := nicMAC(nic); err == nil {
		id.MAC = mac
	}
	out, err := nmcli("-g", "GENERAL.CONNECTION", "device", "show", nic)
	con := strings.TrimSpace(string(out))
	if err != nil || con == "" || strings.HasPrefix(con, "nivaroos-") {
		return id
	}
	if out, err := nmcli("-g", "ipv4.dhcp-client-id", "connection", "show", con); err == nil {
		// -g escapes the ':' of hex client IDs as "\:".
		cid := strings.ReplaceAll(strings.TrimSpace(string(out)), `\:`, ":")
		if clientIDRe.MatchString(cid) {
			id.ClientID = cid
		}
	}
	return id
}

// nmBridgeAddArgs builds the two `nmcli connection add` calls for a bridge.
// Every field was validated by validateBridgeRequest (interface names,
// dotted-quad IPv4) and is passed as its own argument, never through a shell.
func nmBridgeAddArgs(req CreateBridgeRequest, id nmIdentity) ([][]string, error) {
	bridge := []string{"connection", "add", "type", "bridge",
		"ifname", req.Name, "con-name", nmBridgeProfile(req.Name),
		"bridge.stp", "no", "bridge.forward-delay", "0",
		"connection.autoconnect", "yes", "connection.autoconnect-priority", "100",
		"ipv6.method", "auto"}
	if req.StaticIP == "" {
		if id.MAC != "" {
			bridge = append(bridge, "ethernet.cloned-mac-address", id.MAC)
		}
		if id.ClientID != "" {
			bridge = append(bridge, "ipv4.dhcp-client-id", id.ClientID)
		}
	}
	if req.StaticIP != "" {
		ones, bits := net.IPMask(net.ParseIP(req.Netmask).To4()).Size()
		if bits == 0 {
			return nil, fmt.Errorf("invalid netmask %q", req.Netmask)
		}
		bridge = append(bridge, "ipv4.method", "manual",
			"ipv4.addresses", fmt.Sprintf("%s/%d", req.StaticIP, ones),
			"ipv4.gateway", req.Gateway)
	} else {
		bridge = append(bridge, "ipv4.method", "auto")
	}
	port := []string{"connection", "add", "type", "ethernet",
		"ifname", req.HostNIC, "con-name", nmPortProfile(req.Name),
		"master", req.Name, "slave-type", "bridge",
		"connection.autoconnect", "yes", "connection.autoconnect-priority", "100"}
	return [][]string{bridge, port}, nil
}

// createBridgeNM is CreateBridgeNetwork for a NetworkManager-run NIC: same
// validation already done, same registry, same ForceDefaultRoute auto-revert.
func createBridgeNM(registryPath string, req CreateBridgeRequest) (BridgeNetwork, error) {
	adds, err := nmBridgeAddArgs(req, nmNICIdentity(req.HostNIC))
	if err != nil {
		return BridgeNetwork{}, err
	}
	// Left over from an earlier attempt (a crash between add and register):
	// start clean so "add" doesn't make a second profile with the same name.
	nmDeleteProfiles(req.Name)
	state := revertState{name: req.Name, hostNIC: req.HostNIC, registryPath: registryPath, backend: backendNM}
	for _, args := range adds {
		if out, err := nmcli(args...); err != nil {
			revertBridge(state)
			return BridgeNetwork{}, fmt.Errorf("nmcli %s: %v: %s", strings.Join(args[:4], " "), err, strings.TrimSpace(string(out)))
		}
	}
	// Bringing the port up takes hostNIC over from its own profile and
	// starts the bridge with it.
	for _, p := range []string{nmBridgeProfile(req.Name), nmPortProfile(req.Name)} {
		if out, err := nmcli("connection", "up", p); err != nil {
			revertBridge(state)
			return BridgeNetwork{}, fmt.Errorf("nmcli connection up %s: %v: %s", p, err, strings.TrimSpace(string(out)))
		}
	}
	if req.ForceDefaultRoute {
		timer := afterFunc(bridgeRevertDelay, func() { revertBridge(state) })
		pendingReverts.Store(req.Name, timer)
	}
	bridge := BridgeNetwork{Name: req.Name, HostNIC: req.HostNIC, StaticIP: req.StaticIP,
		Netmask: req.Netmask, Gateway: req.Gateway, Backend: backendNM}
	if err := addBridgeToRegistry(registryPath, bridge); err != nil {
		return BridgeNetwork{}, fmt.Errorf("save bridge to registry: %w", err)
	}
	ensureBridgeForwarding(req.Name)
	return bridge, nil
}

// nmDeleteProfiles removes both NivaroOS profiles of a bridge, if present.
func nmDeleteProfiles(name string) {
	for _, p := range []string{nmPortProfile(name), nmBridgeProfile(name)} {
		nmcli("connection", "delete", p) // best-effort: may not exist
	}
}

// removeBridgeNM tears a NetworkManager bridge down and hands hostNIC back to
// its own profile.
func removeBridgeNM(name, hostNIC string) error {
	removeBridgeForwarding(name)
	nmDeleteProfiles(name)
	if out, err := nmcli("device", "connect", hostNIC); err != nil {
		return fmt.Errorf("nmcli device connect %s: %v: %s", hostNIC, err, strings.TrimSpace(string(out)))
	}
	return nil
}
