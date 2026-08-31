// networks.go implements the Networks tab: the always-available default
// NAT network (a real libvirt Network object) plus any bridged networks
// the user has explicitly created (plain host bridge devices, tracked in
// a small JSON registry - VMs attach to them directly via
// <interface type='bridge'>, without needing a libvirt Network object).
//
// This box runs classic Debian ifupdown (/etc/network/interfaces,
// sourcing /etc/network/interfaces.d/*), not netplan/systemd-networkd -
// bridge config is written as an interfaces.d snippet.
//
// Bridge creation refuses the host's default-route interface by default
// (validateHostNIC), so it normally can't drop the connection the API
// request itself arrived over. CreateBridgeRequest.ForceDefaultRoute is
// an explicit, informed-consent-only escape hatch for a box with no
// spare NIC at all - CreateBridgeNetwork pairs it with an automatic
// revert (bridgeRevertDelay, unless confirmed via ConfirmBridge) in case
// the transition itself breaks connectivity.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"text/template"
	"time"
)

const (
	defaultBridgeRegistryPath = "/DATA/VMs/bridges.json"
	interfacesDotDDir         = "/etc/network/interfaces.d"
)

type BridgeNetwork struct {
	Name     string `json:"name"`
	HostNIC  string `json:"host_nic"`
	StaticIP string `json:"static_ip,omitempty"`
	Netmask  string `json:"netmask,omitempty"`
	Gateway  string `json:"gateway,omitempty"`
}

type NetworkInfo struct {
	Name    string `json:"name"`
	Mode    string `json:"mode"` // "nat" or "bridge"
	HostNIC string `json:"host_nic,omitempty"`
	Active  bool   `json:"active"`
}

func networkInterfaceExists(name string) bool {
	_, err := os.Stat(filepath.Join("/sys/class/net", name))
	return err == nil
}

// listPhysicalInterfaces lists host NICs suitable for bridging: real
// devices only (a "device" symlink under sysfs), which excludes bridges,
// veth pairs, docker0, and TUN/TAP devices like tailscale0.
func listPhysicalInterfaces() ([]string, error) {
	entries, err := os.ReadDir("/sys/class/net")
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, e := range entries {
		if e.Name() == "lo" {
			continue
		}
		if _, err := os.Stat(filepath.Join("/sys/class/net", e.Name(), "device")); err != nil {
			continue
		}
		names = append(names, e.Name())
	}
	return names, nil
}

func parseDefaultRouteDevice(output string) (string, error) {
	fields := strings.Fields(output)
	for i, f := range fields {
		if f == "dev" && i+1 < len(fields) {
			return fields[i+1], nil
		}
	}
	return "", fmt.Errorf("could not find a default route device in: %q", strings.TrimSpace(output))
}

func defaultRouteInterface() (string, error) {
	out, err := exec.Command("ip", "-4", "route", "show", "default").Output()
	if err != nil {
		return "", err
	}
	return parseDefaultRouteDevice(string(out))
}

// validateHostNIC confirms hostNIC exists and refuses it if it currently
// carries the host's default route - bridging that interface risks
// disconnecting the box mid-operation. defaultRouteDevice is injected so
// tests don't depend on this machine's actual routing table.
func validateHostNIC(hostNIC string, defaultRouteDevice func() (string, error)) error {
	if !networkInterfaceExists(hostNIC) {
		return fmt.Errorf("network interface %q does not exist", hostNIC)
	}
	dflt, err := defaultRouteDevice()
	if err != nil {
		return fmt.Errorf("determine default route interface: %w", err)
	}
	if hostNIC == dflt {
		return fmt.Errorf("refusing to bridge %q: it currently carries the host's default route "+
			"(bridging it would likely disconnect the box)", hostNIC)
	}
	return nil
}

const bridgeSnippetTemplate = `# Managed by nivaroos-vm-sidecar - bridge {{.Name}} over {{.HostNIC}}
auto {{.HostNIC}}
iface {{.HostNIC}} inet manual

auto {{.Name}}
{{if .StaticIP -}}
iface {{.Name}} inet static
    address {{.StaticIP}}
    netmask {{.Netmask}}
    gateway {{.Gateway}}
{{- else -}}
iface {{.Name}} inet dhcp
{{- end}}
    bridge_ports {{.HostNIC}}
    bridge_stp off
    bridge_fd 0
`

var bridgeSnippet = template.Must(template.New("bridge").Parse(bridgeSnippetTemplate))

// bridgeSnippetData's StaticIP/Netmask/Gateway are all left blank for a
// plain DHCP bridge (the common case: a spare NIC where DHCP is fine).
// A static IP matters specifically when bridging the host's own
// default-route NIC (ForceDefaultRoute) - the bridge gets a new MAC
// address distinct from hostNIC's, so a DHCP server handing out leases
// by MAC (or with a static reservation tied to the old MAC) treats it
// as a brand new client and can easily hand back a different address
// than the host had before, changing the box's own management IP out
// from under it. Static sidesteps that - the same reason Unraid and
// Proxmox both default their host management bridge to a fixed IP
// rather than DHCP.
type bridgeSnippetData struct {
	Name, HostNIC          string
	StaticIP, Netmask, Gateway string
}

func writeInterfacesSnippet(dir string, data bridgeSnippetData) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	var buf strings.Builder
	if err := bridgeSnippet.Execute(&buf, data); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, data.Name+".conf"), []byte(buf.String()), 0644)
}

// neutralizeMainInterfaceStanza comments out hostNIC's own "auto"/
// "allow-hotplug"/"iface" block in the main /etc/network/interfaces file,
// if it has one there. Discovered as a real, pre-existing bug in this
// package: a host whose primary NIC is configured directly in the main
// file (interfaces.d/ empty, everything in one file - the common case,
// confirmed on this very machine) would end up with hostNIC defined
// TWICE once the bridge snippet also defines it (manual, as a bridge
// member) - ifupdown's behavior for a duplicate iface definition across
// sourced files is not something to rely on, since which one "wins" and
// whether it errors depends on parse order and version. Returns the
// original file's full content so the caller can restore it exactly
// (this powers the auto-revert safety net for bridging a possibly-only
// NIC, not just a nice-to-have).
func neutralizeMainInterfaceStanza(mainInterfacesPath, hostNIC string) (original string, changed bool, err error) {
	data, err := os.ReadFile(mainInterfacesPath)
	if err != nil {
		return "", false, err
	}
	original = string(data)
	lines := strings.Split(original, "\n")

	autoRe := regexp.MustCompile(`^\s*(auto|allow-hotplug)\s+` + regexp.QuoteMeta(hostNIC) + `\s*$`)
	ifaceRe := regexp.MustCompile(`^\s*iface\s+` + regexp.QuoteMeta(hostNIC) + `\s+`)

	var out []string
	inBlock := false
	for _, line := range lines {
		switch {
		case autoRe.MatchString(line):
			out = append(out, "# "+line+" # commented out by nivaroos-vm-sidecar: bridged instead, see interfaces.d/")
			changed = true
		case ifaceRe.MatchString(line):
			out = append(out, "# "+line+" # commented out by nivaroos-vm-sidecar: bridged instead, see interfaces.d/")
			inBlock = true
			changed = true
		case inBlock && (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")):
			// An indented sub-option line belonging to the iface block just
			// commented out above (e.g. a static "address"/"gateway" line).
			out = append(out, "# "+line)
		default:
			inBlock = false
			out = append(out, line)
		}
	}
	if !changed {
		return original, false, nil
	}
	if err := os.WriteFile(mainInterfacesPath, []byte(strings.Join(out, "\n")), 0644); err != nil {
		return "", false, fmt.Errorf("write %s: %w", mainInterfacesPath, err)
	}
	return original, true, nil
}

// applyBridge brings the new bridge up under ifupdown. Only ever called
// after validateHostNIC has confirmed hostNIC is not the default-route
// interface.
func applyBridge(name, hostNIC string) error {
	// dhcpcd runs as a single master process managing every interface
	// (not one daemon per interface) - "-k" asks it directly, over its
	// own control socket, to release and stop hostNIC's lease
	// regardless of what /etc/network/interfaces currently says about
	// it. "ifdown hostNIC" alone isn't enough here: by this point
	// hostNIC's own stanza has already been rewritten to "manual" for
	// the bridge (by neutralizeMainInterfaceStanza + the new snippet),
	// so ifdown reads that CURRENT method to decide what to tear down
	// and never touches the dhcp client actually still running from
	// before - found as a stray dhcpcd process still holding hostNIC's
	// old IP well after it had become a bridge member.
	exec.Command("dhcpcd", "-k", hostNIC).Run()
	exec.Command("ifdown", hostNIC).Run() // best-effort: hostNIC may not have been "up" under ifupdown before
	if out, err := exec.Command("ifup", hostNIC).CombinedOutput(); err != nil {
		return fmt.Errorf("ifup %s: %v: %s", hostNIC, err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command("ifup", name).CombinedOutput(); err != nil {
		return fmt.Errorf("ifup %s: %v: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func loadBridgeRegistry(path string) ([]BridgeNetwork, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []BridgeNetwork{}, nil
		}
		return nil, err
	}
	var bridges []BridgeNetwork
	if err := json.Unmarshal(data, &bridges); err != nil {
		return nil, err
	}
	return bridges, nil
}

func saveBridgeRegistry(path string, bridges []BridgeNetwork) error {
	data, err := json.MarshalIndent(bridges, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// addBridgeToRegistry is idempotent by name: re-adding "br0" replaces its
// existing entry rather than appending a second one. Found necessary
// after a real duplicate entry appeared in practice (two identical "br0"
// rows) - whatever exact interleaving of a reverted attempt's own add and
// a later attempt's add caused it, the registry should never be able to
// list the same bridge name twice regardless.
func addBridgeToRegistry(path string, bridge BridgeNetwork) error {
	bridges, err := loadBridgeRegistry(path)
	if err != nil {
		return err
	}
	kept := bridges[:0]
	for _, b := range bridges {
		if b.Name != bridge.Name {
			kept = append(kept, b)
		}
	}
	kept = append(kept, bridge)
	return saveBridgeRegistry(path, kept)
}

func removeBridgeFromRegistry(path, name string) error {
	bridges, err := loadBridgeRegistry(path)
	if err != nil {
		return err
	}
	kept := bridges[:0]
	for _, b := range bridges {
		if b.Name != name {
			kept = append(kept, b)
		}
	}
	return saveBridgeRegistry(path, kept)
}

type CreateBridgeRequest struct {
	Name    string `json:"name"`
	HostNIC string `json:"host_nic"`
	// Bridging the interface carrying the host's default route can drop
	// this very request's own connection - refused by default
	// (validateHostNIC). This is an explicit, informed-consent override
	// for a box with no spare NIC at all, paired with an automatic
	// revert (bridgeRevertDelay, cancelled by ConfirmBridge) in case the
	// transition itself breaks connectivity.
	ForceDefaultRoute bool `json:"force_default_route,omitempty"`
	// Optional static IPv4 config for the bridge itself. All three must
	// be given together, or none at all (falls back to DHCP). Matters
	// most for ForceDefaultRoute: the bridge gets its own MAC distinct
	// from hostNIC's, so a DHCP server can easily treat it as a new
	// client and hand back a different address than the host had before.
	StaticIP string `json:"static_ip,omitempty"`
	Netmask  string `json:"netmask,omitempty"`
	Gateway  string `json:"gateway,omitempty"`
}

// var, not const, so tests can point it at a temp file instead of the
// real system config.
var mainInterfacesPath = "/etc/network/interfaces"

// bridgeRevertDelay is how long a ForceDefaultRoute bridge has to be
// confirmed (via ConfirmBridge) before it's automatically torn down.
// Long enough for a real DHCP lease to come in on the new bridge and for
// a human to notice the UI still works, short enough that a genuinely
// broken transition doesn't lock the box out for long.
const bridgeRevertDelay = 45 * time.Second

var pendingReverts sync.Map // bridge name -> *time.Timer

// revertState captures what's needed to fully undo neutralizeMainInterfaceStanza's write.
type revertState struct {
	name, hostNIC, snippetPath, registryPath string
	originalMainConfig                       string
	mainConfigChanged                        bool
}

// revertBridgeFiles undoes the config file changes only - split out from
// revertBridge so it's testable without needing real network/root
// privileges to run ifdown/ifup against.
func revertBridgeFiles(s revertState) error {
	os.Remove(s.snippetPath)
	if s.mainConfigChanged {
		return os.WriteFile(mainInterfacesPath, []byte(s.originalMainConfig), 0644)
	}
	return nil
}

func revertBridge(s revertState) {
	exec.Command("ifdown", s.name).Run()
	// hostNIC was brought up as a bridge member (applyBridge's own
	// ifup hostNIC call, under the "manual" stanza the bridge snippet
	// gives it) - ifupdown's ifstate bookkeeping considers it "up"
	// under that definition even after tearing down the bridge drops
	// its kernel-level link. Tear it down here, before the config
	// files are reverted below, so ifdown reads the same "manual"
	// definition it was brought up with; done after file revert (or
	// skipped) it's a silent no-op against stale bookkeeping and
	// hostNIC never comes back - discovered the hard way when this
	// left the host's only NIC dead until a physical restart.
	exec.Command("ifdown", s.hostNIC).Run()
	revertBridgeFiles(s)
	// Belt-and-suspenders: make sure the physical link itself isn't
	// left administratively down regardless of what ifupdown's
	// bookkeeping thinks, before asking ifup to bring it back up
	// under the now-restored dhcp config.
	exec.Command("ip", "link", "set", s.hostNIC, "up").Run()
	exec.Command("ifup", s.hostNIC).Run()
	// CreateBridgeNetwork registers the bridge right after applyBridge
	// succeeds, before a ForceDefaultRoute revert timer even exists -
	// without this, a reverted bridge stays listed as if it were still
	// there (found stale in /DATA/VMs/bridges.json after a real revert).
	if s.registryPath != "" {
		removeBridgeFromRegistry(s.registryPath, s.name)
	}
}

// ConfirmBridge cancels a pending automatic revert - call this once
// something (the UI health-checking the sidecar again, a human clicking
// "keep it") has confirmed the bridge didn't break connectivity. A no-op
// if there's no pending revert for this name (already confirmed, timed
// out, or never needed one in the first place).
func ConfirmBridge(name string) {
	if v, ok := pendingReverts.LoadAndDelete(name); ok {
		v.(*time.Timer).Stop()
	}
}

func CreateBridgeNetwork(registryPath, interfacesDir string, req CreateBridgeRequest) (BridgeNetwork, error) {
	if !vmNameRe.MatchString(req.Name) {
		return BridgeNetwork{}, fmt.Errorf("invalid bridge name %q: only letters, digits, - and _ are allowed", req.Name)
	}
	staticFieldsGiven := req.StaticIP != "" || req.Netmask != "" || req.Gateway != ""
	staticFieldsComplete := req.StaticIP != "" && req.Netmask != "" && req.Gateway != ""
	if staticFieldsGiven && !staticFieldsComplete {
		return BridgeNetwork{}, fmt.Errorf("static_ip, netmask and gateway must all be given together, or none at all")
	}
	if err := validateHostNIC(req.HostNIC, defaultRouteInterface); err != nil {
		if !req.ForceDefaultRoute {
			return BridgeNetwork{}, err
		}
		if !networkInterfaceExists(req.HostNIC) {
			return BridgeNetwork{}, fmt.Errorf("network interface %q does not exist", req.HostNIC)
		}
	}

	// A host whose primary NIC is configured directly in the main file
	// (interfaces.d/ empty, everything in one file) would otherwise end
	// up with hostNIC defined twice once the snippet below also defines
	// it - ifupdown's behavior for that is not something to rely on.
	originalMainConfig, mainConfigChanged, err := neutralizeMainInterfaceStanza(mainInterfacesPath, req.HostNIC)
	if err != nil {
		return BridgeNetwork{}, fmt.Errorf("neutralize main interfaces config: %w", err)
	}

	snippetData := bridgeSnippetData{Name: req.Name, HostNIC: req.HostNIC, StaticIP: req.StaticIP, Netmask: req.Netmask, Gateway: req.Gateway}
	if err := writeInterfacesSnippet(interfacesDir, snippetData); err != nil {
		return BridgeNetwork{}, fmt.Errorf("write network config: %w", err)
	}
	snippetPath := filepath.Join(interfacesDir, req.Name+".conf")
	state := revertState{
		name: req.Name, hostNIC: req.HostNIC, snippetPath: snippetPath, registryPath: registryPath,
		originalMainConfig: originalMainConfig, mainConfigChanged: mainConfigChanged,
	}

	if err := applyBridge(req.Name, req.HostNIC); err != nil {
		revertBridge(state) // failed outright - undo immediately, no need to wait for a timer
		return BridgeNetwork{}, fmt.Errorf("apply bridge: %w", err)
	}

	if req.ForceDefaultRoute {
		timer := time.AfterFunc(bridgeRevertDelay, func() { revertBridge(state) })
		pendingReverts.Store(req.Name, timer)
	}

	bridge := BridgeNetwork{Name: req.Name, HostNIC: req.HostNIC, StaticIP: req.StaticIP, Netmask: req.Netmask, Gateway: req.Gateway}
	if err := addBridgeToRegistry(registryPath, bridge); err != nil {
		return BridgeNetwork{}, fmt.Errorf("save bridge to registry: %w", err)
	}
	return bridge, nil
}

// restoreMainInterfaceStanza reverses neutralizeMainInterfaceStanza: it
// uncomments hostNIC's own "auto"/"allow-hotplug"/"iface" lines (and any
// indented sub-option lines under them) back to their original,
// uncommented form. Used when deleting an already-CONFIRMED bridge,
// where the pre-edit content isn't held in memory any more (that
// revertState was discarded the moment ConfirmBridge cancelled the
// pending revert) - unlike neutralizeMainInterfaceStanza's own
// byte-for-byte revert path, which only ever runs within the same
// process lifetime as the edit it's undoing.
func restoreMainInterfaceStanza(mainInterfacesPath, hostNIC string) (changed bool, err error) {
	data, err := os.ReadFile(mainInterfacesPath)
	if err != nil {
		return false, err
	}
	const marker = " # commented out by nivaroos-vm-sidecar: bridged instead, see interfaces.d/"
	autoRe := regexp.MustCompile(`^# (auto|allow-hotplug)\s+` + regexp.QuoteMeta(hostNIC) + `\s*$`)
	ifaceRe := regexp.MustCompile(`^# iface\s+` + regexp.QuoteMeta(hostNIC) + `\s+`)

	lines := strings.Split(string(data), "\n")
	inBlock := false
	for i, line := range lines {
		unmarked := strings.TrimSuffix(line, marker)
		switch {
		case autoRe.MatchString(unmarked):
			lines[i] = strings.TrimPrefix(unmarked, "# ")
			changed = true
		case ifaceRe.MatchString(unmarked):
			lines[i] = strings.TrimPrefix(unmarked, "# ")
			inBlock = true
			changed = true
		case inBlock && strings.HasPrefix(line, "# ") &&
			(strings.HasPrefix(strings.TrimPrefix(line, "# "), " ") || strings.HasPrefix(strings.TrimPrefix(line, "# "), "\t")):
			lines[i] = strings.TrimPrefix(line, "# ")
		default:
			inBlock = false
		}
	}
	if !changed {
		return false, nil
	}
	return true, os.WriteFile(mainInterfacesPath, []byte(strings.Join(lines, "\n")), 0644)
}

// DeleteBridgeNetwork fully tears down a bridge that's already been
// confirmed (i.e. no pending revert timer exists for it any more) -
// removes its interfaces.d snippet, restores hostNIC's original stanza
// in the main config, and brings hostNIC back up as a plain interface.
func DeleteBridgeNetwork(registryPath, interfacesDir, name, hostNIC string) error {
	exec.Command("ifdown", name).Run()
	exec.Command("dhcpcd", "-k", hostNIC).Run()
	exec.Command("ifdown", hostNIC).Run()
	os.Remove(filepath.Join(interfacesDir, name+".conf"))
	if _, err := restoreMainInterfaceStanza(mainInterfacesPath, hostNIC); err != nil {
		return fmt.Errorf("restore main interfaces config: %w", err)
	}
	exec.Command("ip", "link", "set", hostNIC, "up").Run()
	if out, err := exec.Command("ifup", hostNIC).CombinedOutput(); err != nil {
		return fmt.Errorf("ifup %s: %v: %s", hostNIC, err, strings.TrimSpace(string(out)))
	}
	return removeBridgeFromRegistry(registryPath, name)
}

func ListNetworks(store *LibvirtStore, registryPath string) ([]NetworkInfo, error) {
	networks := []NetworkInfo{}

	if conn, err := store.getConn(); err == nil {
		if net, lookupErr := conn.LookupNetworkByName("default"); lookupErr == nil {
			active, _ := net.IsActive()
			networks = append(networks, NetworkInfo{Name: "default", Mode: "nat", Active: active})
			net.Free()
		}
	}

	bridges, err := loadBridgeRegistry(registryPath)
	if err != nil {
		return nil, err
	}
	for _, b := range bridges {
		networks = append(networks, NetworkInfo{
			Name: b.Name, Mode: "bridge", HostNIC: b.HostNIC, Active: networkInterfaceExists(b.Name),
		})
	}
	return networks, nil
}

func RegisterNetworkRoutes(mux *http.ServeMux, store *LibvirtStore, registryPath, interfacesDir string) {
	mux.HandleFunc("GET /networks", func(w http.ResponseWriter, r *http.Request) {
		networks, err := ListNetworks(store, registryPath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, networks)
	})

	mux.HandleFunc("GET /networks/interfaces", func(w http.ResponseWriter, r *http.Request) {
		names, err := listPhysicalInterfaces()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, names)
	})

	mux.HandleFunc("POST /networks/bridge", func(w http.ResponseWriter, r *http.Request) {
		var req CreateBridgeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		bridge, err := CreateBridgeNetwork(registryPath, interfacesDir, req)
		if err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeJSON(w, http.StatusCreated, bridge)
	})

	mux.HandleFunc("POST /networks/bridge/{name}/confirm", func(w http.ResponseWriter, r *http.Request) {
		ConfirmBridge(r.PathValue("name"))
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("DELETE /networks/bridge/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		bridges, err := loadBridgeRegistry(registryPath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		var hostNIC string
		found := false
		for _, b := range bridges {
			if b.Name == name {
				hostNIC = b.HostNIC
				found = true
				break
			}
		}
		if !found {
			writeError(w, http.StatusNotFound, fmt.Errorf("no such bridge network %q", name))
			return
		}
		ConfirmBridge(name) // cancel any still-pending auto-revert first, so it can't fire mid-delete
		if err := DeleteBridgeNetwork(registryPath, interfacesDir, name, hostNIC); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
