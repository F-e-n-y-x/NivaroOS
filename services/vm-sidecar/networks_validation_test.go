package main

import (
	"net/http"
	"testing"
)

func TestValidateBridgeRequest(t *testing.T) {
	good := CreateBridgeRequest{Name: "br0", HostNIC: "enp3s0", StaticIP: "192.168.1.50", Netmask: "24", Gateway: "192.168.1.1"}
	if err := validateBridgeRequest(&good); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	if good.Netmask != "255.255.255.0" {
		t.Errorf("expected prefix length normalized to a dotted mask, got %q", good.Netmask)
	}
	dhcp := CreateBridgeRequest{Name: "br-lan.10", HostNIC: "eth0"}
	if err := validateBridgeRequest(&dhcp); err != nil {
		t.Errorf("valid DHCP request rejected: %v", err)
	}

	bad := []CreateBridgeRequest{
		{Name: "br0", HostNIC: "eth0", StaticIP: "192.168.1.50\n    up rm -rf /", Netmask: "24", Gateway: "192.168.1.1"},
		{Name: "br0", HostNIC: "eth0", StaticIP: "192.168.1.50", Netmask: "255.255.255.0\nup reboot", Gateway: "192.168.1.1"},
		{Name: "br0", HostNIC: "eth0", StaticIP: "192.168.1.50", Netmask: "24", Gateway: "192.168.1.1 \n"},
		{Name: "br0\nup reboot", HostNIC: "eth0"},
		{Name: "br0", HostNIC: "eth0 up reboot"},
		{Name: "br0", HostNIC: "../../x"},
		{Name: "..", HostNIC: "eth0"},
		{Name: "averyveryverylongname", HostNIC: "eth0"},
		{Name: "br0", HostNIC: "eth0", StaticIP: "fe80::1", Netmask: "64", Gateway: "fe80::2"},
		{Name: "br0", HostNIC: "eth0", StaticIP: "192.168.1.50", Netmask: "255.0.255.0", Gateway: "192.168.1.1"},
		{Name: "br0", HostNIC: "eth0", StaticIP: "192.168.1.50", Netmask: "33", Gateway: "192.168.1.1"},
		{Name: "br0", HostNIC: "eth0", StaticIP: "192.168.1.50"},
	}
	for _, req := range bad {
		r := req
		if err := validateBridgeRequest(&r); err == nil || errorStatus(err, 0) != http.StatusBadRequest {
			t.Errorf("%+v: expected a 400, got %v", req, err)
		}
	}
}

func TestNormalizeNetmask(t *testing.T) {
	cases := map[string]string{"24": "255.255.255.0", "/16": "255.255.0.0", "32": "255.255.255.255", "255.255.254.0": "255.255.254.0"}
	for in, want := range cases {
		if got, err := normalizeNetmask(in); err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"0", "0.0.0.0", "255.255.255.1", "abc", "", "024x"} {
		if _, err := normalizeNetmask(in); err == nil {
			t.Errorf("%q: expected rejection", in)
		}
	}
}

func TestDomainsUsingBridge(t *testing.T) {
	store := newTestStore(t)
	createStoppedVM(t, store, CreateVMRequest{Name: "bridge-user", VCPUs: 1, MemoryMiB: 256, Networks: []NICSpec{{Mode: "bridge", BridgeName: "brtest9"}}})
	users, err := store.DomainsUsingBridge("brtest9")
	if err != nil || len(users) != 1 || users[0] != "bridge-user" {
		t.Fatalf("expected [bridge-user], got %v, %v", users, err)
	}
	if users, _ := store.DomainsUsingBridge("brunused"); len(users) != 0 {
		t.Fatalf("expected no users, got %v", users)
	}
}
