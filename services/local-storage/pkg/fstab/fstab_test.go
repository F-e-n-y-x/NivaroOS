package fstab

import (
	"os"
	"testing"

	"gotest.tools/v3/assert"
)

const fstabContent = `
	# UNCONFIGURED FSTAB FOR BASE SYSTEM
	LABEL=UEFI      /boot/efi       vfat    umask=0077      0 1
	/mnt/sdb:/mnt/sdc       /media  mergerfs        defaults,allow_other,use_ino,category.create=mfs,moveonenospc=true,minfreespace=1M 0 0
	LABEL=desktop-rootfs    /               ext4    defaults        0 1
`

func TestFSTab(t *testing.T) {
	fstab := &FStab{path: "/tmp/fstab"}

	err := os.WriteFile(fstab.path, []byte(fstabContent), 0o600)
	assert.NilError(t, err)

	entries, err := fstab.GetEntries()
	assert.NilError(t, err)

	assert.Equal(t, len(entries), 3)

	entry, err := fstab.GetEntryByMountPoint("/media")
	assert.NilError(t, err)

	assert.Equal(t, entry.Source, "/mnt/sdb:/mnt/sdc")
	assert.Equal(t, entry.MountPoint, "/media")
	assert.Equal(t, entry.FSType, "mergerfs")
	assert.Equal(t, entry.Options, "defaults,allow_other,use_ino,category.create=mfs,moveonenospc=true,minfreespace=1M")
	assert.Equal(t, entry.Dump, 0)
	assert.Equal(t, entry.Pass, PassDoNotCheck)

	err = fstab.RemoveByMountPoint(entry.MountPoint, false)
	assert.NilError(t, err)

	nonExistingEntry, err := fstab.GetEntryByMountPoint(entry.MountPoint)
	assert.NilError(t, err)
	assert.Equal(t, nonExistingEntry, (*Entry)(nil))

	err = fstab.Add(*entry, true)
	assert.NilError(t, err)

	entry, err = fstab.GetEntryByMountPoint(entry.MountPoint)
	assert.NilError(t, err)

	assert.Equal(t, entry.Source, "/mnt/sdb:/mnt/sdc")
	assert.Equal(t, entry.MountPoint, "/media")
	assert.Equal(t, entry.FSType, "mergerfs")
	assert.Equal(t, entry.Options, "defaults,allow_other,use_ino,category.create=mfs,moveonenospc=true,minfreespace=1M")
	assert.Equal(t, entry.Dump, 0)
	assert.Equal(t, entry.Pass, PassDoNotCheck)
}

func TestFSTabManagedEntries(t *testing.T) {
	fstab := &FStab{path: "/tmp/fstab-managed"}

	err := os.WriteFile(fstab.path, []byte(fstabContent), 0o600)
	assert.NilError(t, err)

	entries, err := fstab.GetEntries()
	assert.NilError(t, err)
	for _, e := range entries {
		assert.Equal(t, e.Managed, false)
		assert.Equal(t, e.Enabled, true)
	}

	err = fstab.Add(Entry{
		Source:     "UUID=1234-5678",
		MountPoint: "/DATA/Movies",
		FSType:     "ext4",
		Options:    "defaults,noatime",
	}, false)
	assert.NilError(t, err)

	entry, err := fstab.GetEntryByMountPoint("/DATA/Movies")
	assert.NilError(t, err)
	assert.Equal(t, entry.Managed, true)
	assert.Equal(t, entry.Enabled, true)

	byUUID, err := fstab.GetEntryByUUID("1234-5678")
	assert.NilError(t, err)
	assert.Equal(t, byUUID.MountPoint, "/DATA/Movies")

	// Pre-existing (non-managed) entries must never match GetEntryByUUID/Source lookups
	// meant for managed volumes.
	rootEntry, err := fstab.GetEntryByUUID("desktop-rootfs")
	assert.NilError(t, err)
	assert.Equal(t, rootEntry, (*Entry)(nil))

	// Disable: entry disappears from GetEntries but survives in GetAllEntries as disabled.
	err = fstab.RemoveByMountPoint("/DATA/Movies", true)
	assert.NilError(t, err)

	gone, err := fstab.GetEntryByMountPoint("/DATA/Movies")
	assert.NilError(t, err)
	assert.Equal(t, gone, (*Entry)(nil))

	all, err := fstab.GetAllEntries()
	assert.NilError(t, err)
	var disabled *Entry
	for _, e := range all {
		if e.MountPoint == "/DATA/Movies" {
			disabled = e
		}
	}
	assert.Assert(t, disabled != nil)
	assert.Equal(t, disabled.Managed, true)
	assert.Equal(t, disabled.Enabled, false)
	assert.Equal(t, disabled.Source, "UUID=1234-5678")

	// Re-enable: entry becomes active again with all fields intact.
	err = fstab.Enable("/DATA/Movies")
	assert.NilError(t, err)

	reenabled, err := fstab.GetEntryByMountPoint("/DATA/Movies")
	assert.NilError(t, err)
	assert.Equal(t, reenabled.Managed, true)
	assert.Equal(t, reenabled.Enabled, true)
	assert.Equal(t, reenabled.Source, "UUID=1234-5678")
	assert.Equal(t, reenabled.FSType, "ext4")

	// Enabling something that was never disabled (or doesn't exist) is a no-op error, not
	// a silent corruption of the file.
	err = fstab.Enable("/DATA/does-not-exist")
	assert.ErrorIs(t, err, ErrEntryNotFound)
}
