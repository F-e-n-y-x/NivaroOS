package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fakeSysNet builds a /sys/class/net-like tree: wired NIC, Wi-Fi, a NIC that
// is a bridge member, a bridge and a veth (neither has a "device" link).
func fakeSysNet(t *testing.T) {
	t.Helper()
	root := t.TempDir()
	mk := func(nic string, files ...string) {
		_ = os.MkdirAll(filepath.Join(root, nic), 0o755)
		for _, f := range files {
			_ = os.MkdirAll(filepath.Join(root, nic, f), 0o755)
		}
	}
	mk("enp7s0", "device")
	mk("wlx00aa", "device", "phy80211")
	mk("wlan1", "device", "wireless")
	mk("enp8s0", "device", "master")
	mk("br0", "bridge")
	mk("veth1")
	mk("lo")
	old := sysClassNet
	sysClassNet = root
	t.Cleanup(func() { sysClassNet = old })
}

func TestDirectCandidatesOnlyWiredFreeNICs(t *testing.T) {
	fakeSysNet(t)
	if got := directCandidates(); !reflect.DeepEqual(got, []string{"enp7s0"}) {
		t.Fatalf("direct candidates %q, want only the free wired NIC", got)
	}
}

func TestValidateNICDirect(t *testing.T) {
	if err := validateNIC(0, NICSpec{Mode: "direct", HostNIC: "enp7s0"}); err != nil {
		t.Fatalf("valid direct NIC refused: %v", err)
	}
	if err := validateNIC(0, NICSpec{Mode: "direct"}); err == nil {
		t.Fatal("direct without host_nic must be refused")
	}
	if err := validateNIC(0, NICSpec{Mode: "direct", HostNIC: "enp7s0'/><disk"}); err == nil {
		t.Fatal("an XML-breaking host_nic must be refused")
	}
}

func TestRenderNICXMLDirect(t *testing.T) {
	x, err := renderNICXML(NICSpec{Mode: "direct", HostNIC: "enp7s0", MAC: "52:54:00:12:34:56"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<interface type='direct'>", "<source dev='enp7s0' mode='bridge'/>", "<mac address='52:54:00:12:34:56'/>"} {
		if !strings.Contains(x, want) {
			t.Errorf("direct NIC XML lacks %q:\n%s", want, x)
		}
	}
}

func TestNICMatchesDirect(t *testing.T) {
	info := NICInfo{Mode: "direct", HostNIC: "enp7s0", Model: "virtio", MAC: "52:54:00:12:34:56", LinkState: "up"}
	if !nicMatches(info, NICSpec{Mode: "direct", HostNIC: "enp7s0", MAC: "52:54:00:12:34:56"}) {
		t.Fatal("same direct adapter must match")
	}
	if nicMatches(info, NICSpec{Mode: "direct", HostNIC: "enp8s0", MAC: "52:54:00:12:34:56"}) {
		t.Fatal("a direct adapter on another NIC must not match")
	}
}
