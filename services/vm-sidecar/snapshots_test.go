package main

import (
	"strings"
	"testing"
)

func TestSnapshots_Lifecycle(t *testing.T) {
	store := NewLibvirtStore("test:///default")
	defer store.Close()

	// "test" domain exists in test:///default
	const domName = "test"

	// 1. Initially snapshots should be empty
	snaps, err := store.ListSnapshots(domName)
	if err != nil {
		t.Fatalf("ListSnapshots failed: %v", err)
	}

	// 2. Create snapshot
	created, err := store.CreateSnapshot(domName, CreateSnapshotRequest{
		Name:        "test-snap-1",
		Description: "First test snapshot",
	})
	if err != nil {
		t.Fatalf("CreateSnapshot failed: %v", err)
	}
	if created.Name != "test-snap-1" {
		t.Errorf("Expected snapshot name test-snap-1, got %s", created.Name)
	}

	// 3. List should have 1 snapshot
	snaps, err = store.ListSnapshots(domName)
	if err != nil {
		t.Fatalf("ListSnapshots after create failed: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("Expected 1 snapshot, got %d", len(snaps))
	}

	// 4. Get snapshot
	got, err := store.GetSnapshot(domName, "test-snap-1")
	if err != nil {
		t.Fatalf("GetSnapshot failed: %v", err)
	}
	if got.Name != "test-snap-1" {
		t.Errorf("Expected test-snap-1, got %s", got.Name)
	}

	// 5. Revert snapshot
	if err := store.RevertSnapshot(domName, "test-snap-1"); err != nil {
		t.Fatalf("RevertSnapshot failed: %v", err)
	}

	// 6. Delete snapshot
	if err := store.DeleteSnapshot(domName, "test-snap-1", false); err != nil {
		t.Fatalf("DeleteSnapshot failed: %v", err)
	}

	// 7. Verify deletion
	snaps, err = store.ListSnapshots(domName)
	if err != nil {
		t.Fatalf("ListSnapshots after delete failed: %v", err)
	}
	if len(snaps) != 0 {
		t.Fatalf("Expected 0 snapshots, got %d", len(snaps))
	}
}

func TestCheckSnapshotNVRAM(t *testing.T) {
	raw := `<domain><os><loader readonly='yes' type='pflash' format='raw'>/usr/share/OVMF/OVMF_CODE_4M.fd</loader>
		<nvram template='/usr/share/OVMF/OVMF_VARS_4M.fd' templateFormat='raw' format='raw'>/DATA/VMs/x_VARS.fd</nvram></os></domain>`
	err := checkSnapshotNVRAM(raw)
	if errorStatus(err, 0) != 409 || !strings.HasPrefix(err.Error(), "Snapshots need the VM's UEFI variables stored as qcow2; this VM uses raw NVRAM.") {
		t.Fatalf("raw NVRAM: expected the 409, got %v", err)
	}
	legacy := `<domain><os><loader type='pflash'>/c.fd</loader><nvram>/x_VARS.fd</nvram></os></domain>`
	if errorStatus(checkSnapshotNVRAM(legacy), 0) != 409 {
		t.Fatal("NVRAM without a format attribute is raw: expected 409")
	}
	qcow := `<domain><os><loader type='pflash'>/c.fd</loader><nvram format='qcow2'>/x_VARS.qcow2</nvram></os></domain>`
	if err := checkSnapshotNVRAM(qcow); err != nil {
		t.Fatalf("qcow2 NVRAM: %v", err)
	}
	if err := checkSnapshotNVRAM(`<domain><os><type>hvm</type></os></domain>`); err != nil {
		t.Fatalf("BIOS VM: %v", err)
	}
}
