package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNMDeviceManaged(t *testing.T) {
	out := "enp7s0:connected\ndocker0:connected (externally)\nveth1:unmanaged\nenp8s0:unmanaged\nwlan0:disconnected\n"
	for nic, want := range map[string]bool{
		"enp7s0": true,  // NetworkManager runs it
		"wlan0":  true,  // managed, just not connected
		"enp8s0": false, // ifupdown owns it (managed=false)
		"eth9":   false, // not listed at all
	} {
		if got := nmDeviceManaged(out, nic); got != want {
			t.Errorf("nmDeviceManaged(%q) = %v, want %v", nic, got, want)
		}
	}
}

func TestNMBridgeAddArgs(t *testing.T) {
	adds, err := nmBridgeAddArgs(CreateBridgeRequest{Name: "br0", HostNIC: "enp7s0"})
	if err != nil {
		t.Fatal(err)
	}
	bridge, port := strings.Join(adds[0], " "), strings.Join(adds[1], " ")
	for _, want := range []string{"type bridge", "ifname br0", "con-name nivaroos-br0", "bridge.stp no", "ipv4.method auto"} {
		if !strings.Contains(bridge, want) {
			t.Errorf("bridge args %q lack %q", bridge, want)
		}
	}
	for _, want := range []string{"type ethernet", "ifname enp7s0", "con-name nivaroos-br0-port", "master br0", "slave-type bridge"} {
		if !strings.Contains(port, want) {
			t.Errorf("port args %q lack %q", port, want)
		}
	}

	adds, err = nmBridgeAddArgs(CreateBridgeRequest{Name: "br0", HostNIC: "enp7s0",
		StaticIP: "192.168.11.10", Netmask: "255.255.255.0", Gateway: "192.168.11.1"})
	if err != nil {
		t.Fatal(err)
	}
	bridge = strings.Join(adds[0], " ")
	for _, want := range []string{"ipv4.method manual", "ipv4.addresses 192.168.11.10/24", "ipv4.gateway 192.168.11.1"} {
		if !strings.Contains(bridge, want) {
			t.Errorf("static bridge args %q lack %q", bridge, want)
		}
	}
}

// fakeNM records nmcli calls; failOn makes the call whose args start with it fail.
func fakeNM(t *testing.T, failOn string) *[]string {
	t.Helper()
	calls := []string{}
	old := nmcli
	nmcli = func(args ...string) ([]byte, error) {
		c := strings.Join(args, " ")
		calls = append(calls, c)
		if failOn != "" && strings.HasPrefix(c, failOn) {
			return []byte("boom"), errors.New("exit status 4")
		}
		return nil, nil
	}
	t.Cleanup(func() { nmcli = old })
	return &calls
}

func verbs(calls []string) []string {
	out := []string{}
	for _, c := range calls {
		f := strings.Fields(c)
		out = append(out, strings.Join(f[:3], " "))
	}
	return out
}

func TestCreateBridgeNMRegistersWithBackend(t *testing.T) {
	calls := fakeNM(t, "")
	reg := filepath.Join(t.TempDir(), "bridges.json")
	b, err := createBridgeNM(reg, CreateBridgeRequest{Name: "br0", HostNIC: "enp8s0"})
	if err != nil {
		t.Fatal(err)
	}
	if b.Backend != backendNM {
		t.Fatalf("backend %q", b.Backend)
	}
	want := []string{
		"connection delete nivaroos-br0-port", "connection delete nivaroos-br0", // stale leftovers cleared first
		"connection add type", "connection add type",
		"connection up nivaroos-br0", "connection up nivaroos-br0-port",
	}
	if got := verbs(*calls); !reflect.DeepEqual(got, want) {
		t.Fatalf("calls\n got %q\nwant %q", got, want)
	}
	list, _ := loadBridgeRegistry(reg)
	if len(list) != 1 || list[0].Backend != backendNM || list[0].HostNIC != "enp8s0" {
		t.Fatalf("registry %+v", list)
	}
}

func TestCreateBridgeNMRevertsOnFailure(t *testing.T) {
	calls := fakeNM(t, "connection up nivaroos-br0-port")
	reg := filepath.Join(t.TempDir(), "bridges.json")
	if _, err := createBridgeNM(reg, CreateBridgeRequest{Name: "br0", HostNIC: "enp8s0"}); err == nil {
		t.Fatal("a failed 'connection up' must fail the create")
	}
	got := strings.Join(*calls, "\n")
	for _, want := range []string{"connection delete nivaroos-br0-port", "connection delete nivaroos-br0", "device connect enp8s0"} {
		if strings.Count(got, want) < 1 {
			t.Errorf("revert lacks %q:\n%s", want, got)
		}
	}
	if !strings.HasSuffix(got, "device connect enp8s0") {
		t.Errorf("the NIC must be handed back to its own profile last:\n%s", got)
	}
	if list, _ := loadBridgeRegistry(reg); len(list) != 0 {
		t.Fatalf("a failed bridge must not be registered: %+v", list)
	}
}

func TestCreateBridgeNMForceDefaultRouteAutoReverts(t *testing.T) {
	calls := fakeNM(t, "")
	var revert func()
	old := afterFunc
	afterFunc = func(d time.Duration, f func()) *time.Timer {
		if d != bridgeRevertDelay {
			t.Errorf("revert delay %v", d)
		}
		revert = f
		return time.NewTimer(time.Hour)
	}
	t.Cleanup(func() { afterFunc = old; pendingReverts.Delete("br0") })
	reg := filepath.Join(t.TempDir(), "bridges.json")
	if _, err := createBridgeNM(reg, CreateBridgeRequest{Name: "br0", HostNIC: "enp7s0", ForceDefaultRoute: true}); err != nil {
		t.Fatal(err)
	}
	if revert == nil {
		t.Fatal("ForceDefaultRoute must schedule an auto-revert")
	}
	*calls = nil
	revert() // nobody confirmed in time
	if got := strings.Join(*calls, "\n"); !strings.HasSuffix(got, "device connect enp7s0") {
		t.Fatalf("auto-revert must remove the profiles and reconnect the NIC:\n%s", got)
	}
	if list, _ := loadBridgeRegistry(reg); len(list) != 0 {
		t.Fatalf("auto-reverted bridge still registered: %+v", list)
	}
}

func TestDeleteBridgeNetworkNM(t *testing.T) {
	calls := fakeNM(t, "")
	dir := t.TempDir()
	reg := filepath.Join(dir, "bridges.json")
	_ = addBridgeToRegistry(reg, BridgeNetwork{Name: "br0", HostNIC: "enp8s0", Backend: backendNM})
	if err := DeleteBridgeNetwork(reg, dir, "br0", "enp8s0", backendNM); err != nil {
		t.Fatal(err)
	}
	want := []string{"connection delete nivaroos-br0-port", "connection delete nivaroos-br0", "device connect enp8s0"}
	if !reflect.DeepEqual(*calls, want) {
		t.Fatalf("calls %q, want %q", *calls, want)
	}
	if list, _ := loadBridgeRegistry(reg); len(list) != 0 {
		t.Fatalf("registry %+v", list)
	}
}

// Bridges registered before NetworkManager support have no backend field:
// they are ifupdown bridges and must keep being deleted the ifupdown way.
func TestOldRegistryEntriesAreIfupdown(t *testing.T) {
	reg := filepath.Join(t.TempDir(), "bridges.json")
	_ = os.WriteFile(reg, []byte(`[{"name":"br0","host_nic":"enp8s0"}]`), 0o644)
	list, err := loadBridgeRegistry(reg)
	if err != nil || len(list) != 1 || list[0].Backend != "" {
		t.Fatalf("old entry: %+v %v", list, err)
	}
}
