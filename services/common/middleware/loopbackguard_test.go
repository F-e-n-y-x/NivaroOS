package middleware

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeProc builds a minimal /proc: self/ns/net plus one dir per process.
type fakeProcess struct {
	pid, comm string
	args      []string
	netns     string // "" = same as self
}

func fakeProc(t *testing.T, procs ...fakeProcess) string {
	t.Helper()
	root := t.TempDir()
	mk := func(pid, ns string) string {
		dir := filepath.Join(root, pid)
		if err := os.MkdirAll(filepath.Join(dir, "ns"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(ns, filepath.Join(dir, "ns", "net")); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	mk("self", "net:[4026531840]")
	for _, p := range procs {
		ns := p.netns
		if ns == "" {
			ns = "net:[4026531840]"
		}
		dir := mk(p.pid, ns)
		os.WriteFile(filepath.Join(dir, "comm"), []byte(p.comm+"\n"), 0o644)
		os.WriteFile(filepath.Join(dir, "cmdline"), []byte(strings.Join(p.args, "\x00")+"\x00"), 0o644)
	}
	return root
}

func withProc(t *testing.T, root string) {
	t.Helper()
	old := procRoot
	procRoot = root
	resetLoopbackGuard()
	t.Cleanup(func() { procRoot = old; resetLoopbackGuard() })
}

func TestUserspaceRelayRunning(t *testing.T) {
	kernel := fakeProcess{pid: "100", comm: "tailscaled", args: []string{"/usr/sbin/tailscaled", "--state=/var/lib/tailscale/tailscaled.state", "--port=41641"}}
	cases := []struct {
		name  string
		procs []fakeProcess
		want  bool
	}{
		{"no tailscaled", []fakeProcess{{pid: "1", comm: "systemd", args: []string{"/sbin/init"}}}, false},
		{"kernel tun tailscaled", []fakeProcess{kernel}, false},
		{"userspace --tun=", []fakeProcess{{pid: "7", comm: "tailscaled", args: []string{"tailscaled", "--tun=userspace-networking"}}}, true},
		{"userspace -tun value", []fakeProcess{{pid: "7", comm: "tailscaled", args: []string{"tailscaled", "-tun", "userspace-networking", "--socks5-server=localhost:1055"}}}, true},
		{"userspace in another netns", []fakeProcess{{pid: "7", comm: "tailscaled", args: []string{"tailscaled", "--tun=userspace-networking"}, netns: "net:[4026532999]"}}, false},
		{"other process mentioning the flag", []fakeProcess{{pid: "8", comm: "bash", args: []string{"bash", "-c", "--tun=userspace-networking"}}}, false},
		{"named tun device", []fakeProcess{{pid: "9", comm: "tailscaled", args: []string{"tailscaled", "--tun=ts0"}}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := userspaceRelayRunning(fakeProc(t, c.procs...)); got != c.want {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

// With a userspace tailscaled relaying tailnet peers from 127.0.0.1, a
// bare loopback request is indistinguishable from a peer - no auth skip,
// and the gateway must not vouch for it either.
func TestLoopbackTrustOffUnderUserspaceTailscale(t *testing.T) {
	withProc(t, fakeProc(t, fakeProcess{pid: "7", comm: "tailscaled", args: []string{"tailscaled", "--tun=userspace-networking"}}))
	r := req("127.0.0.1:5555", nil)
	if IsLocalAutomation(r) {
		t.Fatal("loopback must not be trusted while a userspace tailscaled relays into it")
	}
	MarkLocalAutomation(r)
	if r.Header.Get(LocalAutomationHeader) != "" {
		t.Fatal("gateway must not vouch for loopback under userspace tailscale")
	}
	r = req("127.0.0.1:5555", map[string]string{"X-Forwarded-For": "127.0.0.1", LocalAutomationHeader: "1"})
	if IsLocalAutomation(r) {
		t.Fatal("even a gateway mark is not trusted then")
	}
}

func TestLoopbackTrustedWithKernelTailscale(t *testing.T) {
	withProc(t, fakeProc(t, fakeProcess{pid: "100", comm: "tailscaled", args: []string{"/usr/sbin/tailscaled", "--port=41641"}}))
	if !IsLocalAutomation(req("127.0.0.1:5555", nil)) {
		t.Fatal("direct loopback automation should still work with kernel-TUN tailscaled")
	}
}

func TestTailscalePeersNeverLocal(t *testing.T) {
	withProc(t, fakeProc(t))
	for _, remote := range []string{"100.64.0.7:40000", "100.100.10.1:40000", "[fd7a:115c:a1e0::7]:40000"} {
		for _, h := range []map[string]string{nil, {LocalAutomationHeader: "1"}, {"X-Forwarded-For": "127.0.0.1", LocalAutomationHeader: "1"}} {
			r := req(remote, h)
			if IsLocalAutomation(r) {
				t.Fatalf("tailnet peer %s %v treated as local automation", remote, h)
			}
			MarkLocalAutomation(r)
			if r.Header.Get(LocalAutomationHeader) != "" {
				t.Fatalf("gateway vouched for tailnet peer %s", remote)
			}
		}
	}
}

// Relays on this box that dial from loopback: cloudflared, `tailscale
// serve`, any reverse proxy.
func TestLocalRelaysAreNotLocal(t *testing.T) {
	withProc(t, fakeProc(t))
	for _, h := range []map[string]string{
		{"Cf-Connecting-Ip": "203.0.113.9"},
		{"Cf-Ray": "8a1b2c3d4e5f-AMS"},
		{"True-Client-Ip": "203.0.113.9"},
		{"Tailscale-User-Login": "owner@example.com"},
		{"Tailscale-Headers-Info": "https://tailscale.com/s/serve-headers"},
		{"X-Forwarded-Host": "atom.example.ts.net"},
		{"Via": "1.1 proxy"},
	} {
		r := req("127.0.0.1:5555", h)
		if IsLocalAutomation(r) {
			t.Fatalf("relayed request %v treated as local", h)
		}
		MarkLocalAutomation(r)
		if r.Header.Get(LocalAutomationHeader) != "" {
			t.Fatalf("gateway vouched for relayed request %v", h)
		}
	}
}

func TestSameHostOriginRemoteNames(t *testing.T) {
	cases := []struct {
		host, origin string
		want         bool
	}{
		{"100.64.0.1", "http://100.64.0.1", true},
		{"100.64.0.1:80", "http://100.64.0.1", true},
		{"atom", "http://atom", true},
		{"atom.example.ts.net", "https://atom.example.ts.net", true},
		{"[fd7a:115c:a1e0::1]", "http://[fd7a:115c:a1e0::1]", true},
		{"100.64.0.1", "http://192.168.1.10", false},
		{"atom", "http://evil.example", false},
	}
	for _, c := range cases {
		r := req("100.64.0.9:1", map[string]string{"Origin": c.origin})
		r.Host = c.host
		if got := SameHostOrigin(r); got != c.want {
			t.Errorf("host %q origin %q: got %v want %v", c.host, c.origin, got, c.want)
		}
		if got := CheckWebSocketOrigin(r); got != c.want {
			t.Errorf("ws host %q origin %q: got %v want %v", c.host, c.origin, got, c.want)
		}
	}
}
