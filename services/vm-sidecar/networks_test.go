package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseDefaultRouteDevice(t *testing.T) {
	dev, err := parseDefaultRouteDevice("default via 192.168.10.1 dev enp7s0 proto dhcp src 192.168.10.10 metric 1002")
	if err != nil {
		t.Fatalf("parseDefaultRouteDevice: %v", err)
	}
	if dev != "enp7s0" {
		t.Fatalf("expected enp7s0, got %q", dev)
	}
}

func TestParseDefaultRouteDevice_NoDefaultRoute(t *testing.T) {
	if _, err := parseDefaultRouteDevice(""); err == nil {
		t.Fatal("expected an error when there is no default route line, got nil")
	}
}

func TestListPhysicalInterfaces_ExcludesLoopback(t *testing.T) {
	names, err := listPhysicalInterfaces()
	if err != nil {
		t.Fatalf("listPhysicalInterfaces: %v", err)
	}
	for _, n := range names {
		if n == "lo" {
			t.Fatalf("expected loopback to be excluded, got %+v", names)
		}
	}
}

func TestNetworkInterfaceExists(t *testing.T) {
	if !networkInterfaceExists("lo") {
		t.Fatal("expected the loopback interface to exist on any Linux host")
	}
	if networkInterfaceExists("totally-fake-nic-xyz") {
		t.Fatal("expected a fake interface name to not exist")
	}
}

func TestValidateHostNIC_RejectsMissingInterface(t *testing.T) {
	fakeDefaultRoute := func() (string, error) { return "eth0", nil }
	err := validateHostNIC("totally-fake-nic-xyz", fakeDefaultRoute)
	if err == nil {
		t.Fatal("expected an error for a nonexistent interface, got nil")
	}
}

func TestValidateHostNIC_RejectsDefaultRouteInterface(t *testing.T) {
	fakeDefaultRoute := func() (string, error) { return "lo", nil }
	err := validateHostNIC("lo", fakeDefaultRoute)
	if err == nil {
		t.Fatal("expected an error when hostNIC is the default-route interface, got nil")
	}
}

func TestValidateHostNIC_AcceptsNonDefaultRouteInterface(t *testing.T) {
	fakeDefaultRoute := func() (string, error) { return "some-other-nic", nil }
	if err := validateHostNIC("lo", fakeDefaultRoute); err != nil {
		t.Fatalf("expected no error for a non-default-route interface, got: %v", err)
	}
}

func TestValidateHostNIC_PropagatesDefaultRouteLookupError(t *testing.T) {
	failingLookup := func() (string, error) { return "", errors.New("no route table") }
	if err := validateHostNIC("lo", failingLookup); err == nil {
		t.Fatal("expected the default-route lookup error to propagate, got nil")
	}
}

func TestWriteInterfacesSnippet(t *testing.T) {
	dir := t.TempDir()
	if err := writeInterfacesSnippet(dir, bridgeSnippetData{Name: "br-vm0", HostNIC: "enp8s0"}); err != nil {
		t.Fatalf("writeInterfacesSnippet: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "br-vm0.conf"))
	if err != nil {
		t.Fatalf("expected snippet file to exist: %v", err)
	}
	got := string(content)
	for _, want := range []string{"iface enp8s0 inet manual", "auto br-vm0", "iface br-vm0 inet dhcp", "bridge_ports enp8s0"} {
		if !strings.Contains(got, want) {
			t.Errorf("expected snippet to contain %q, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "inet static") {
		t.Errorf("expected a plain DHCP snippet with no StaticIP given, got:\n%s", got)
	}
}

func TestWriteInterfacesSnippet_Static(t *testing.T) {
	dir := t.TempDir()
	data := bridgeSnippetData{Name: "br0", HostNIC: "enp7s0", StaticIP: "192.168.10.10", Netmask: "255.255.255.0", Gateway: "192.168.10.1"}
	if err := writeInterfacesSnippet(dir, data); err != nil {
		t.Fatalf("writeInterfacesSnippet: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "br0.conf"))
	if err != nil {
		t.Fatalf("expected snippet file to exist: %v", err)
	}
	got := string(content)
	for _, want := range []string{
		"iface enp7s0 inet manual", "auto br0", "iface br0 inet static",
		"address 192.168.10.10", "netmask 255.255.255.0", "gateway 192.168.10.1",
		"bridge_ports enp7s0",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("expected static snippet to contain %q, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "inet dhcp") {
		t.Errorf("expected no dhcp stanza when a static IP is given, got:\n%s", got)
	}
}

// TestNeutralizeMainInterfaceStanza_MatchesThisHostsRealFormat uses the
// exact content this box's own /etc/network/interfaces has (confirmed by
// reading it directly) - the scenario that surfaced this function's need
// in the first place: a primary NIC configured directly in the main
// file, with an empty interfaces.d/, which would otherwise end up
// defined twice once a bridge snippet also defines it.
func TestNeutralizeMainInterfaceStanza_MatchesThisHostsRealFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "interfaces")
	original := `# This file describes the network interfaces available on your system
# and how to activate them. For more information, see interfaces(5).

source /etc/network/interfaces.d/*

# The loopback network interface
auto lo
iface lo inet loopback

# The primary network interface
allow-hotplug enp7s0
iface enp7s0 inet dhcp
`
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("write test interfaces file: %v", err)
	}

	got, changed, err := neutralizeMainInterfaceStanza(path, "enp7s0")
	if err != nil {
		t.Fatalf("neutralizeMainInterfaceStanza: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true - enp7s0 has a stanza in this file")
	}
	if got != original {
		t.Errorf("expected the returned original to be byte-for-byte the pre-edit content, got:\n%s", got)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back written file: %v", err)
	}
	writtenStr := string(written)

	// lo must be completely untouched - only enp7s0's own lines should change.
	if !strings.Contains(writtenStr, "auto lo\niface lo inet loopback") {
		t.Errorf("expected lo's stanza to be untouched, got:\n%s", writtenStr)
	}
	if strings.Contains(writtenStr, "allow-hotplug enp7s0\n") {
		t.Errorf("expected the live allow-hotplug enp7s0 line to be commented out, got:\n%s", writtenStr)
	}
	if strings.Contains(writtenStr, "\niface enp7s0 inet dhcp\n") {
		t.Errorf("expected the live iface enp7s0 line to be commented out, got:\n%s", writtenStr)
	}
	if !strings.Contains(writtenStr, "# allow-hotplug enp7s0") || !strings.Contains(writtenStr, "# iface enp7s0 inet dhcp") {
		t.Errorf("expected both enp7s0 lines to survive as comments (auditable/revertible), got:\n%s", writtenStr)
	}

	// Idempotent: running it again (e.g. a second bridge attempt after a
	// revert) must not error or double-comment already-commented lines.
	_, changedAgain, err := neutralizeMainInterfaceStanza(path, "enp7s0")
	if err != nil {
		t.Fatalf("second neutralizeMainInterfaceStanza: %v", err)
	}
	if changedAgain {
		t.Error("expected changed=false on a second call - enp7s0's stanza is already commented out")
	}
}

func TestNeutralizeMainInterfaceStanza_NoOpWhenNoStanzaPresent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "interfaces")
	original := "auto lo\niface lo inet loopback\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("write test interfaces file: %v", err)
	}

	_, changed, err := neutralizeMainInterfaceStanza(path, "enp7s0")
	if err != nil {
		t.Fatalf("neutralizeMainInterfaceStanza: %v", err)
	}
	if changed {
		t.Error("expected changed=false - enp7s0 has no stanza in this file at all")
	}
	after, _ := os.ReadFile(path)
	if string(after) != original {
		t.Errorf("expected the file to be left byte-for-byte unchanged, got:\n%s", after)
	}
}

func TestRestoreMainInterfaceStanza_UndoesNeutralize(t *testing.T) {
	path := filepath.Join(t.TempDir(), "interfaces")
	original := `# This file describes the network interfaces available on your system
# and how to activate them. For more information, see interfaces(5).

source /etc/network/interfaces.d/*

# The loopback network interface
auto lo
iface lo inet loopback

# The primary network interface
allow-hotplug enp7s0
iface enp7s0 inet dhcp
`
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("write test interfaces file: %v", err)
	}

	if _, _, err := neutralizeMainInterfaceStanza(path, "enp7s0"); err != nil {
		t.Fatalf("neutralizeMainInterfaceStanza: %v", err)
	}

	changed, err := restoreMainInterfaceStanza(path, "enp7s0")
	if err != nil {
		t.Fatalf("restoreMainInterfaceStanza: %v", err)
	}
	if !changed {
		t.Fatal("expected changed=true - enp7s0's lines were commented out by neutralize")
	}

	restored, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back restored file: %v", err)
	}
	if string(restored) != original {
		t.Errorf("expected the file to be restored byte-for-byte to the original, got:\n%s", restored)
	}
}

func TestRestoreMainInterfaceStanza_NoOpWhenNothingCommented(t *testing.T) {
	path := filepath.Join(t.TempDir(), "interfaces")
	original := "auto lo\niface lo inet loopback\n"
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("write test interfaces file: %v", err)
	}

	changed, err := restoreMainInterfaceStanza(path, "enp7s0")
	if err != nil {
		t.Fatalf("restoreMainInterfaceStanza: %v", err)
	}
	if changed {
		t.Error("expected changed=false - nothing was ever commented out for enp7s0")
	}
	after, _ := os.ReadFile(path)
	if string(after) != original {
		t.Errorf("expected the file to be left byte-for-byte unchanged, got:\n%s", after)
	}
}

func TestRevertBridgeFiles_RestoresOriginalConfigAndRemovesSnippet(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "interfaces")
	original := "auto lo\niface lo inet loopback\n\nallow-hotplug enp7s0\niface enp7s0 inet dhcp\n"
	commented := "auto lo\niface lo inet loopback\n\n# allow-hotplug enp7s0 # commented out\n# iface enp7s0 inet dhcp # commented out\n"
	if err := os.WriteFile(mainPath, []byte(commented), 0644); err != nil {
		t.Fatalf("write test interfaces file: %v", err)
	}
	snippetPath := filepath.Join(dir, "br0.conf")
	if err := os.WriteFile(snippetPath, []byte("# snippet"), 0644); err != nil {
		t.Fatalf("write test snippet: %v", err)
	}

	orig := mainInterfacesPath
	mainInterfacesPath = mainPath
	defer func() { mainInterfacesPath = orig }()

	if err := revertBridgeFiles(revertState{
		name: "br0", hostNIC: "enp7s0", snippetPath: snippetPath,
		originalMainConfig: original, mainConfigChanged: true,
	}); err != nil {
		t.Fatalf("revertBridgeFiles: %v", err)
	}

	if _, err := os.Stat(snippetPath); !os.IsNotExist(err) {
		t.Errorf("expected snippet to be removed, stat err: %v", err)
	}
	restored, err := os.ReadFile(mainPath)
	if err != nil || string(restored) != original {
		t.Errorf("expected main config restored to original, got %q (err=%v)", restored, err)
	}
}

func TestRevertBridgeFiles_NoOpOnMainConfigWhenNotChanged(t *testing.T) {
	dir := t.TempDir()
	snippetPath := filepath.Join(dir, "br0.conf")
	if err := os.WriteFile(snippetPath, []byte("# snippet"), 0644); err != nil {
		t.Fatalf("write test snippet: %v", err)
	}

	if err := revertBridgeFiles(revertState{
		name: "br0", hostNIC: "enp7s0", snippetPath: snippetPath, mainConfigChanged: false,
	}); err != nil {
		t.Fatalf("revertBridgeFiles: %v", err)
	}
	if _, err := os.Stat(snippetPath); !os.IsNotExist(err) {
		t.Errorf("expected snippet to be removed, stat err: %v", err)
	}
}

func TestBridgeRegistry_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bridges.json")

	bridges, err := loadBridgeRegistry(path)
	if err != nil {
		t.Fatalf("loadBridgeRegistry (missing file): %v", err)
	}
	if len(bridges) != 0 {
		t.Fatalf("expected an empty registry for a missing file, got %+v", bridges)
	}

	if err := addBridgeToRegistry(path, BridgeNetwork{Name: "br-vm0", HostNIC: "enp8s0"}); err != nil {
		t.Fatalf("addBridgeToRegistry: %v", err)
	}
	if err := addBridgeToRegistry(path, BridgeNetwork{Name: "br-vm1", HostNIC: "enp9s0"}); err != nil {
		t.Fatalf("addBridgeToRegistry: %v", err)
	}

	bridges, err = loadBridgeRegistry(path)
	if err != nil {
		t.Fatalf("loadBridgeRegistry: %v", err)
	}
	if len(bridges) != 2 || bridges[0].Name != "br-vm0" || bridges[1].Name != "br-vm1" {
		t.Fatalf("expected both bridges in order, got %+v", bridges)
	}
}

func TestAddBridgeToRegistry_IsIdempotentByName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bridges.json")
	if err := addBridgeToRegistry(path, BridgeNetwork{Name: "br0", HostNIC: "enp7s0"}); err != nil {
		t.Fatalf("addBridgeToRegistry: %v", err)
	}
	if err := addBridgeToRegistry(path, BridgeNetwork{Name: "br0", HostNIC: "enp7s0"}); err != nil {
		t.Fatalf("addBridgeToRegistry (again): %v", err)
	}

	bridges, err := loadBridgeRegistry(path)
	if err != nil {
		t.Fatalf("loadBridgeRegistry: %v", err)
	}
	if len(bridges) != 1 {
		t.Fatalf("expected exactly one br0 entry after adding it twice, got %+v", bridges)
	}
}

func TestRemoveBridgeFromRegistry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bridges.json")
	if err := addBridgeToRegistry(path, BridgeNetwork{Name: "br-vm0", HostNIC: "enp8s0"}); err != nil {
		t.Fatalf("addBridgeToRegistry: %v", err)
	}
	if err := addBridgeToRegistry(path, BridgeNetwork{Name: "br-vm1", HostNIC: "enp9s0"}); err != nil {
		t.Fatalf("addBridgeToRegistry: %v", err)
	}

	if err := removeBridgeFromRegistry(path, "br-vm0"); err != nil {
		t.Fatalf("removeBridgeFromRegistry: %v", err)
	}

	bridges, err := loadBridgeRegistry(path)
	if err != nil {
		t.Fatalf("loadBridgeRegistry: %v", err)
	}
	if len(bridges) != 1 || bridges[0].Name != "br-vm1" {
		t.Fatalf("expected only br-vm1 to remain, got %+v", bridges)
	}

	// Removing an already-absent (or never-registered) name is a no-op,
	// not an error - the ForceDefaultRoute revert path calls this
	// unconditionally even when nothing was ever added.
	if err := removeBridgeFromRegistry(path, "totally-unregistered"); err != nil {
		t.Fatalf("removeBridgeFromRegistry on absent name: %v", err)
	}
}

func TestListNetworks_IncludesDefaultNATAndRegisteredBridges(t *testing.T) {
	store := NewLibvirtStore("test:///default")
	t.Cleanup(func() { store.Close() })

	registryPath := filepath.Join(t.TempDir(), "bridges.json")
	if err := addBridgeToRegistry(registryPath, BridgeNetwork{Name: "lo", HostNIC: "enp8s0"}); err != nil {
		t.Fatalf("addBridgeToRegistry: %v", err)
	}

	networks, err := ListNetworks(store, registryPath)
	if err != nil {
		t.Fatalf("ListNetworks: %v", err)
	}

	var foundDefault, foundBridge bool
	for _, n := range networks {
		if n.Name == "default" && n.Mode == "nat" {
			foundDefault = true
			if !n.Active {
				t.Error("expected the test driver's default NAT network to be active")
			}
		}
		if n.Name == "lo" && n.Mode == "bridge" {
			foundBridge = true
			if !n.Active {
				t.Error(`expected the "lo" registered bridge to report active since /sys/class/net/lo exists`)
			}
		}
	}
	if !foundDefault {
		t.Errorf("expected a default NAT network entry, got %+v", networks)
	}
	if !foundBridge {
		t.Errorf("expected the registered bridge entry, got %+v", networks)
	}
}
