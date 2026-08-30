package main

import "testing"

func TestParseVNCAddress_ResolvedPort(t *testing.T) {
	xmlDesc := `<domain><devices>
		<graphics type='vnc' port='5901' listen='127.0.0.1'/>
	</devices></domain>`

	addr, err := parseVNCAddress(xmlDesc)
	if err != nil {
		t.Fatalf("parseVNCAddress: %v", err)
	}
	if addr != "127.0.0.1:5901" {
		t.Fatalf("expected 127.0.0.1:5901, got %q", addr)
	}
}

func TestParseVNCAddress_UnresolvedAutoportRejected(t *testing.T) {
	xmlDesc := `<domain><devices>
		<graphics type='vnc' port='-1' autoport='yes' listen='127.0.0.1'/>
	</devices></domain>`

	if _, err := parseVNCAddress(xmlDesc); err == nil {
		t.Fatal("expected an error for an unresolved (-1) VNC port, got nil")
	}
}

func TestParseVNCAddress_NoGraphicsDevice(t *testing.T) {
	xmlDesc := `<domain><devices></devices></domain>`
	if _, err := parseVNCAddress(xmlDesc); err == nil {
		t.Fatal("expected an error when there is no graphics device, got nil")
	}
}

func TestParseVNCAddress_DefaultsListenHostWhenEmpty(t *testing.T) {
	xmlDesc := `<domain><devices>
		<graphics type='vnc' port='5902'/>
	</devices></domain>`

	addr, err := parseVNCAddress(xmlDesc)
	if err != nil {
		t.Fatalf("parseVNCAddress: %v", err)
	}
	if addr != "127.0.0.1:5902" {
		t.Fatalf("expected default listen host 127.0.0.1, got %q", addr)
	}
}

func TestVNCAddress_UnstartedVMHasUnresolvedPort(t *testing.T) {
	store := newTestStore(t)
	diskPath := t.TempDir() + "/console-vm.qcow2"
	_, err := store.CreateVM(CreateVMRequest{
		Name:      "console-vm",
		VCPUs:     1,
		MemoryMiB: 256,
		Disks:     []DiskSpec{{Path: diskPath, GiB: 1}},
		Networks:  []NICSpec{{Mode: "nat"}},
	})
	if err != nil {
		t.Fatalf("CreateVM: %v", err)
	}
	t.Cleanup(func() { _ = store.DeleteVM("console-vm", true) })

	// The test:///default driver never simulates real QEMU VNC listening,
	// so autoport stays unresolved even though the domain reports
	// running - this exercises the same "not ready yet" rejection a real
	// not-yet-started VM would hit.
	if _, err := vncAddress(store, "console-vm"); err == nil {
		t.Fatal("expected vncAddress to reject an unresolved VNC port, got nil error")
	}
}
