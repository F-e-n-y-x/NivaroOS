// networks_fw.go keeps VM traffic on a bridged network from being dropped by
// Docker's firewall. Once br_netfilter is loaded (Docker, libvirt or a
// container can load it), frames crossing ANY Linux bridge - a VM's DHCP
// request to the router included - go through iptables' FORWARD chain, where
// Docker's rules (and a DROP policy, whenever Docker had to enable IP
// forwarding itself) apply. VMs on the bridge then never get an address.
//
// DOCKER-USER is the chain Docker reserves for the host's own rules (it is
// evaluated first and never flushed by Docker), so each bridge gets one
// "traffic within this bridge is fine" rule there - for IPv4 and IPv6. It is
// added when a bridge is created, re-added at every sidecar start (rules
// don't survive a reboot), and removed with the bridge. Without Docker (no
// DOCKER-USER chain) there is nothing to do.
package main

import (
	"os/exec"
	"time"
)

// fwTools are the iptables front-ends to keep the rule in (IPv4, IPv6).
var fwTools = []string{"iptables", "ip6tables"}

// runFW runs an iptables command; a var so tests can record calls.
var runFW = func(tool string, args ...string) error {
	if _, err := exec.LookPath(tool); err != nil {
		return err
	}
	return exec.Command(tool, args...).Run()
}

func bridgeFWRule(bridge string) []string {
	return []string{"DOCKER-USER", "-i", bridge, "-o", bridge, "-j", "ACCEPT",
		"-m", "comment", "--comment", "nivaroos-vm-bridge " + bridge}
}

// ensureBridgeForwarding adds bridge's rule where DOCKER-USER exists,
// idempotently (-C before -I). It reports whether Docker's chain was found
// for at least one tool, so the startup pass knows whether to retry.
func ensureBridgeForwarding(bridge string) (dockerSeen bool) {
	rule := bridgeFWRule(bridge)
	for _, tool := range fwTools {
		if runFW(tool, "-w", "-n", "-L", "DOCKER-USER") != nil {
			continue // no Docker (or no such tool): nothing filters this bridge
		}
		dockerSeen = true
		if runFW(tool, append([]string{"-w", "-C"}, rule...)...) == nil {
			continue
		}
		runFW(tool, append([]string{"-w", "-I"}, rule...)...)
	}
	return dockerSeen
}

// removeBridgeForwarding deletes every copy of bridge's rule.
func removeBridgeForwarding(bridge string) {
	rule := bridgeFWRule(bridge)
	for _, tool := range fwTools {
		for i := 0; i < 10 && runFW(tool, append([]string{"-w", "-D"}, rule...)...) == nil; i++ {
		}
	}
}

// fwRetry paces the startup pass while Docker may still be starting.
var fwRetry = struct {
	every time.Duration
	tries int
}{10 * time.Second, 30}

// dockerChainReady: does any tool have Docker's DOCKER-USER chain yet?
func dockerChainReady() bool {
	for _, tool := range fwTools {
		if runFW(tool, "-w", "-n", "-L", "DOCKER-USER") == nil {
			return true
		}
	}
	return false
}

// EnsureBridgesForwarding is the startup pass: Docker may start after the
// sidecar, so it first waits (about 5 minutes) for Docker's chain, then adds
// every registered bridge's rule in one full pass - not bridge by bridge
// while Docker comes up, which would skip the bridges checked before it did.
func EnsureBridgesForwarding(registryPath string) {
	bridges, err := loadBridgeRegistry(registryPath)
	if err != nil || len(bridges) == 0 {
		return
	}
	for try := 0; !dockerChainReady(); try++ {
		if try+1 >= fwRetry.tries {
			return // no Docker on this box: nothing filters the bridges
		}
		time.Sleep(fwRetry.every)
	}
	for _, b := range bridges {
		ensureBridgeForwarding(b.Name)
	}
}
