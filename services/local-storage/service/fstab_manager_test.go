package service

import (
	"testing"

	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/pkg/fstab"
	"gotest.tools/v3/assert"
)

func TestIsSafeMountPoint(t *testing.T) {
	safe := []string{
		"/DATA/Movies",
		"/mnt/backup-drive",
		"/home/ayush/external",
	}
	for _, mp := range safe {
		assert.NilError(t, isSafeMountPoint(mp), mp)
	}

	unsafe := []string{
		"",
		"relative/path",
		"/",
		"/etc",
		"/etc/passwd",
		"/boot",
		"/boot/efi",
		"/var",
		"/DATA/Movies/",
		"/DATA/../etc/passwd",
		"/DATA/./Movies",
	}
	for _, mp := range unsafe {
		assert.Assert(t, isSafeMountPoint(mp) != nil, mp)
	}
}

func TestComposeOptions(t *testing.T) {
	assert.Equal(t, composeOptions("", false, true), "rw")
	assert.Equal(t, composeOptions("", true, true), "ro")
	assert.Equal(t, composeOptions("", false, false), "rw,noauto")
	assert.Equal(t, composeOptions("noatime,uid=1000", false, true), "noatime,uid=1000,rw")

	// A caller can't smuggle in a second ro/rw/auto/noauto via the advanced field -
	// composeOptions is always the sole source of truth for those four tokens.
	assert.Equal(t, composeOptions("ro,noauto,noatime", false, true), "noatime,rw")
}

func TestDeriveFlags(t *testing.T) {
	e := &fstab.Entry{Options: "rw,noatime", Pass: 0}
	ro, boot, check := deriveFlags(e)
	assert.Equal(t, ro, false)
	assert.Equal(t, boot, true)
	assert.Equal(t, check, false)

	e = &fstab.Entry{Options: "ro,noauto", Pass: 2}
	ro, boot, check = deriveFlags(e)
	assert.Equal(t, ro, true)
	assert.Equal(t, boot, false)
	assert.Equal(t, check, true)
}

func TestExtractUUID(t *testing.T) {
	assert.Equal(t, extractUUID("UUID=1234-5678"), "1234-5678")
	assert.Equal(t, extractUUID("/dev/sdb1"), "")
	assert.Equal(t, extractUUID(""), "")
}
