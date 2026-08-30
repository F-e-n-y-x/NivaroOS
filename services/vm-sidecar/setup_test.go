package main

import (
	"testing"

	libvirt "libvirt.org/go/libvirt"
)

func TestPackageInstalled_UnknownPackageIsFalse(t *testing.T) {
	if packageInstalled("totally-fake-package-xyz-recasa-vm-sidecar-test") {
		t.Fatal("expected a nonexistent package to report as not installed")
	}
}

func newTestConn(t *testing.T) *libvirt.Connect {
	conn, err := libvirt.NewConnect("test:///default")
	if err != nil {
		t.Fatalf("connect to test driver: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestEnsurePoolActive_CreatesAndIsIdempotent(t *testing.T) {
	conn := newTestConn(t)
	dir := t.TempDir()
	poolXML := `<pool type='dir'><name>setup-test-pool</name><target><path>` + dir + `</path></target></pool>`

	if err := ensurePoolActive(conn, "setup-test-pool", poolXML); err != nil {
		t.Fatalf("first ensurePoolActive: %v", err)
	}
	pool, err := conn.LookupStoragePoolByName("setup-test-pool")
	if err != nil {
		t.Fatalf("expected pool to exist after ensurePoolActive: %v", err)
	}
	defer pool.Free()
	active, err := pool.IsActive()
	if err != nil || !active {
		t.Fatalf("expected pool to be active, active=%v err=%v", active, err)
	}

	// A second call (e.g. re-running /setup/install) must not error.
	if err := ensurePoolActive(conn, "setup-test-pool", poolXML); err != nil {
		t.Fatalf("second ensurePoolActive: %v", err)
	}
}

func TestEnsureNetworkActive_CreatesAndIsIdempotent(t *testing.T) {
	conn := newTestConn(t)
	netXML := `<network><name>setup-test-net</name><bridge name='virbr-setup-test'/><forward mode='nat'/>` +
		`<ip address='192.168.199.1' netmask='255.255.255.0'><dhcp><range start='192.168.199.2' end='192.168.199.254'/></dhcp></ip></network>`

	if err := ensureNetworkActive(conn, "setup-test-net", netXML); err != nil {
		t.Fatalf("first ensureNetworkActive: %v", err)
	}
	net, err := conn.LookupNetworkByName("setup-test-net")
	if err != nil {
		t.Fatalf("expected network to exist after ensureNetworkActive: %v", err)
	}
	defer net.Free()
	active, err := net.IsActive()
	if err != nil || !active {
		t.Fatalf("expected network to be active, active=%v err=%v", active, err)
	}

	if err := ensureNetworkActive(conn, "setup-test-net", netXML); err != nil {
		t.Fatalf("second ensureNetworkActive: %v", err)
	}
}

// CheckSetupStatus's package-detection half isn't asserted here since it
// depends on what's actually installed on the machine running the test -
// only its libvirt-reachability half is deterministic against the test
// driver. Package detection is covered by TestPackageInstalled_UnknownPackageIsFalse.
func TestCheckSetupStatus_ReflectsLibvirtReachability(t *testing.T) {
	store := NewLibvirtStore("test:///default")
	t.Cleanup(func() { store.Close() })

	status := CheckSetupStatus(store)
	if !status.LibvirtReachable {
		t.Fatal("expected LibvirtReachable=true when the test driver connects successfully")
	}
}
