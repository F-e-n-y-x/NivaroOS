package service

import (
	"path/filepath"
	"testing"
)

func TestMinutesToSpindownCode(t *testing.T) {
	cases := []struct {
		minutes int
		want    int
	}{
		{0, 0},
		{-5, 0},
		{1, 12},
		{5, 60},
		{10, 120},
		{20, 240},
		{21, 241},
		{30, 241},
		{45, 242},
		{60, 242},
		{120, 244},
		{330, 251},
		{1000, 251}, // clamped to the usable max
	}
	for _, c := range cases {
		if got := minutesToSpindownCode(c.minutes); got != c.want {
			t.Errorf("minutesToSpindownCode(%d) = %d, want %d", c.minutes, got, c.want)
		}
	}
}

func TestSpindownCodeToMinutes(t *testing.T) {
	cases := []struct {
		code int
		want int
	}{
		{0, 0},
		{12, 1},
		{240, 20},
		{241, 30},
		{251, 330},
		{252, 21},
		{253, 0},
	}
	for _, c := range cases {
		if got := spindownCodeToMinutes(c.code); got != c.want {
			t.Errorf("spindownCodeToMinutes(%d) = %d, want %d", c.code, got, c.want)
		}
	}
}

func TestHdparmConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	confPath := filepath.Join(dir, "hdparm.conf")

	id := "/dev/disk/by-id/ata-TEST_DRIVE_ABC123"

	if err := writeHdparmSpindownCodeTo(confPath, id, 242); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	code, ok := readHdparmSpindownCodeFrom(confPath, id)
	if !ok || code != 242 {
		t.Fatalf("got (%d, %v), want (242, true)", code, ok)
	}

	// Writing a second device's block must not disturb the first.
	otherID := "/dev/disk/by-id/ata-OTHER_DRIVE_XYZ789"
	if err := writeHdparmSpindownCodeTo(confPath, otherID, 120); err != nil {
		t.Fatalf("second write failed: %v", err)
	}
	code, ok = readHdparmSpindownCodeFrom(confPath, id)
	if !ok || code != 242 {
		t.Fatalf("first device's block was disturbed: got (%d, %v)", code, ok)
	}
	code, ok = readHdparmSpindownCodeFrom(confPath, otherID)
	if !ok || code != 120 {
		t.Fatalf("got (%d, %v), want (120, true)", code, ok)
	}

	// Updating an existing block replaces it in place rather than duplicating it.
	if err := writeHdparmSpindownCodeTo(confPath, id, 60); err != nil {
		t.Fatalf("update write failed: %v", err)
	}
	code, ok = readHdparmSpindownCodeFrom(confPath, id)
	if !ok || code != 60 {
		t.Fatalf("got (%d, %v), want (60, true)", code, ok)
	}

	// Writing code 0 removes the block entirely (disabled / "Never").
	if err := writeHdparmSpindownCodeTo(confPath, id, 0); err != nil {
		t.Fatalf("disable write failed: %v", err)
	}
	if _, ok := readHdparmSpindownCodeFrom(confPath, id); ok {
		t.Fatalf("expected block to be removed after writing code 0")
	}
	// The other device's block must still be intact.
	code, ok = readHdparmSpindownCodeFrom(confPath, otherID)
	if !ok || code != 120 {
		t.Fatalf("other device's block was lost: got (%d, %v)", code, ok)
	}
}

func TestReadHdparmSpindownCodeMissingFile(t *testing.T) {
	code, ok := readHdparmSpindownCodeFrom(filepath.Join(t.TempDir(), "does-not-exist.conf"), "/dev/sda")
	if ok || code != 0 {
		t.Fatalf("got (%d, %v), want (0, false) for a missing file", code, ok)
	}
}
