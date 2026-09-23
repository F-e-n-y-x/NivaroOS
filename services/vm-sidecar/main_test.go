package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain points every host path this package touches at temp dirs and
// stubs out umount, so the suite never writes to /DATA or unmounts
// anything on the machine running it. Storage and ISO roots
// are the system temp dir itself because tests put disks/ISOs in their own
// t.TempDir() directories, all of which live under it.
func TestMain(m *testing.M) {
	tmpRoot, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		panic(err)
	}
	base, err := os.MkdirTemp("", "vm-sidecar-test-")
	if err != nil {
		panic(err)
	}
	if base, err = filepath.EvalSymlinks(base); err != nil {
		panic(err)
	}

	defaultStorageDir = tmpRoot
	defaultISODir = tmpRoot
	shareAllowedRoot = base
	legacyVMSharesBaseDir = filepath.Join(base, "VM-Shares")
	defaultAutoShareDir = filepath.Join(base, "share")
	runUmount = func(...string) error { return nil }

	code := m.Run()
	os.RemoveAll(base)
	os.Exit(code)
}
