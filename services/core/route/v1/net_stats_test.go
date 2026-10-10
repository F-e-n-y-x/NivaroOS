package v1

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLinkSpeed(t *testing.T) {
	old := sysClassNet
	sysClassNet = t.TempDir()
	defer func() { sysClassNet = old }()
	mk := func(name, speed string, wifi bool) {
		d := filepath.Join(sysClassNet, name)
		_ = os.MkdirAll(d, 0o755)
		if speed != "" {
			_ = os.WriteFile(filepath.Join(d, "speed"), []byte(speed), 0o644)
		}
		if wifi {
			_ = os.Mkdir(filepath.Join(d, "wireless"), 0o755)
		}
	}
	mk("eth0", "1000\n", false)
	mk("eth1", "-1\n", false) // link down
	mk("wlan0", "", true)     // Wi-Fi: no readable speed
	for name, want := range map[string][2]any{"eth0": {1000, false}, "eth1": {0, false}, "wlan0": {0, true}, "gone": {0, false}} {
		if s, w := linkSpeed(name); s != want[0] || w != want[1] {
			t.Errorf("%s: %d %v, want %v", name, s, w, want)
		}
	}
}

// Today's totals add up deltas, survive a restart (the file), count a
// reboot's fresh counters from 0, and start over at local midnight.
func TestNetTodayTotals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "net-today.json")
	boot := "boot-1"
	newT := func() *netToday { return &netToday{path: path, bootID: func() string { return boot }} }
	day := time.Date(2026, 10, 11, 9, 0, 0, 0, time.Local)

	a := newT()
	if rx, tx := a.add("eth0", 1000, 500, day); rx != 0 || tx != 0 {
		t.Fatalf("first sight counts from now: %d %d", rx, tx)
	}
	if rx, tx := a.add("eth0", 1600, 700, day.Add(5*time.Second)); rx != 600 || tx != 200 {
		t.Fatalf("delta: %d %d", rx, tx)
	}
	a.add("eth0", 1700, 800, day.Add(2*time.Minute)) // saved (a minute passed)

	// Service restart, same boot: carries on from the saved counters.
	b := newT()
	if rx, tx := b.add("eth0", 1900, 850, day.Add(3*time.Minute)); rx != 900 || tx != 350 {
		t.Fatalf("after restart: %d %d", rx, tx)
	}
	b.add("eth0", 1900, 850, day.Add(5*time.Minute))

	// Reboot: counters restarted, everything since boot is new.
	boot = "boot-2"
	c := newT()
	if rx, tx := c.add("eth0", 300, 100, day.Add(10*time.Minute)); rx != 1200 || tx != 450 {
		t.Fatalf("after reboot: %d %d", rx, tx)
	}
	// A reset counter (NIC re-created) without a reboot.
	if rx, _ := c.add("eth0", 50, 100, day.Add(11*time.Minute)); rx != 1250 {
		t.Fatalf("after counter reset: %d", rx)
	}
	// Midnight: starts over.
	next := time.Date(2026, 10, 12, 0, 0, 1, 0, time.Local)
	if rx, tx := c.add("eth0", 150, 130, next); rx != 100 || tx != 30 {
		t.Fatalf("new day: %d %d", rx, tx)
	}
	// A stale file from yesterday is today's zero.
	d := newT()
	if rx, _ := d.add("eth0", 150, 130, next.Add(24*time.Hour)); rx != 0 {
		t.Fatalf("stale day: %d", rx)
	}
}
