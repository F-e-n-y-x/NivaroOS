package middleware

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Loopback is only proof of "a process on this host" while nothing on the
// host relays remote connections into loopback with no header saying so.
// tailscaled in userspace-networking mode (`--tun=userspace-networking`,
// the default in many containers and on hosts without /dev/net/tun) does
// exactly that: a tailnet peer's connection to this node's Tailscale IP is
// re-dialled from 127.0.0.1, so every peer would pass IsLocalAutomation and
// skip JWT auth. While such a tailscaled runs in this network namespace,
// loopback trust is switched off: same-host automation then needs a token
// like everyone else (fail closed).

// procRoot is /proc; tests point it at a fake tree.
var procRoot = "/proc"

const loopbackGuardTTL = 15 * time.Second

var loopbackGuard struct {
	sync.Mutex
	at        time.Time
	untrusted bool
}

// LoopbackUntrusted reports whether loopback peers must not be trusted as
// local automation on this host right now (cached for loopbackGuardTTL).
func LoopbackUntrusted() bool {
	loopbackGuard.Lock()
	defer loopbackGuard.Unlock()
	if !loopbackGuard.at.IsZero() && time.Since(loopbackGuard.at) < loopbackGuardTTL {
		return loopbackGuard.untrusted
	}
	loopbackGuard.untrusted = userspaceRelayRunning(procRoot)
	loopbackGuard.at = time.Now()
	return loopbackGuard.untrusted
}

func resetLoopbackGuard() {
	loopbackGuard.Lock()
	loopbackGuard.at = time.Time{}
	loopbackGuard.Unlock()
}

// userspaceRelayRunning scans proc for a tailscaled whose command line
// selects userspace networking and which shares our network namespace.
func userspaceRelayRunning(proc string) bool {
	selfNS, _ := os.Readlink(filepath.Join(proc, "self", "ns", "net"))
	entries, err := os.ReadDir(proc)
	if err != nil {
		return false
	}
	for _, e := range entries {
		name := e.Name()
		if name == "" || name[0] < '0' || name[0] > '9' {
			continue
		}
		dir := filepath.Join(proc, name)
		comm, err := os.ReadFile(filepath.Join(dir, "comm"))
		if err != nil || strings.TrimSpace(string(comm)) != "tailscaled" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, "cmdline"))
		if err != nil || !userspaceArgs(strings.Split(strings.TrimRight(string(raw), "\x00"), "\x00")) {
			continue
		}
		// A tailscaled in another network namespace (its own container
		// network) re-dials into that namespace's loopback, not ours.
		// Unknown namespace (unreadable) counts as ours: fail closed.
		if selfNS != "" {
			if ns, err := os.Readlink(filepath.Join(dir, "ns", "net")); err == nil && ns != selfNS {
				continue
			}
		}
		return true
	}
	return false
}

// userspaceArgs: tailscaled's flag parser accepts -tun/--tun, "=value" or
// a separate value.
func userspaceArgs(args []string) bool {
	for i, a := range args {
		v := ""
		switch {
		case strings.HasPrefix(a, "--tun="), strings.HasPrefix(a, "-tun="):
			v = a[strings.IndexByte(a, '=')+1:]
		case a == "--tun" || a == "-tun":
			if i+1 < len(args) {
				v = args[i+1]
			}
		default:
			continue
		}
		if strings.TrimSpace(v) == "userspace-networking" {
			return true
		}
	}
	return false
}
