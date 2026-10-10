package main

import (
	"testing"

	"github.com/godbus/dbus/v5"
)

func mmode(id string, w, h int32, hz float64, current bool) mutterMode {
	return mutterMode{ID: id, W: w, H: h, Refresh: hz, PreferredScale: 1, Scales: []float64{1, 2},
		Props: map[string]dbus.Variant{"is-current": dbus.MakeVariant(current)}}
}

// Two monitors, the streamed (primary) one is DP-1 at 1920x1080@60.
func mutterFixture() ([]mutterMonitor, []mutterLogical) {
	mons := []mutterMonitor{
		{Spec: mutterMonSpec{Connector: "HDMI-1"}, Modes: []mutterMode{mmode("h1", 1280, 1024, 60, true)}},
		{Spec: mutterMonSpec{Connector: "DP-1"}, Modes: []mutterMode{
			mmode("a", 1920, 1080, 60, true), mmode("b", 1280, 720, 50, false), mmode("c", 1280, 720, 60, false),
			mmode("d", 1280, 720, 75, false), mmode("e", 1024, 768, 60, false),
		}},
	}
	logical := []mutterLogical{
		{X: 0, Y: 0, Scale: 1, Monitors: []mutterMonSpec{{Connector: "HDMI-1"}}},
		{X: 1280, Y: 0, Scale: 2, Primary: true, Monitors: []mutterMonSpec{{Connector: "DP-1"}}},
	}
	return mons, logical
}

func TestMutterInfoIsThePrimaryMonitor(t *testing.T) {
	mons, logical := mutterFixture()
	info, err := mutterInfo(mons, logical)
	if err != nil || info.Current != "1920x1080" || info.Output != "DP-1" || len(info.Resolutions) != 3 {
		t.Fatalf("%+v %v", info, err)
	}
	if r := info.Resolutions[0]; r.Width != 1920 {
		t.Fatalf("largest first: %+v", info.Resolutions)
	}
}

func TestMutterPlanSwitchesOnlyTheStreamedMonitor(t *testing.T) {
	mons, logical := mutterFixture()
	cfg, err := mutterPlan(mons, logical, 1280, 720)
	if err != nil || len(cfg) != 2 {
		t.Fatalf("%+v %v", cfg, err)
	}
	if m := cfg[0].Monitors[0]; m.Connector != "HDMI-1" || m.Mode != "h1" || cfg[0].X != 0 || cfg[0].Primary {
		t.Fatalf("other monitor changed: %+v", cfg[0])
	}
	// Same refresh as now (60) wins over a higher one; scale 2 is kept.
	if m := cfg[1].Monitors[0]; m.Mode != "c" || cfg[1].Scale != 2 || !cfg[1].Primary || cfg[1].X != 1280 {
		t.Fatalf("streamed monitor: %+v", cfg[1])
	}
	if _, err := mutterPlan(mons, logical, 800, 600); err == nil {
		t.Fatal("a mode the monitor lacks must be refused")
	}
}

func TestMutterPlanFallsBackToAScaleTheModeHas(t *testing.T) {
	mons, logical := mutterFixture()
	mons[1].Modes[4].Scales = []float64{1}
	cfg, err := mutterPlan(mons, logical, 1024, 768)
	if err != nil || cfg[1].Scale != 1 || cfg[1].Monitors[0].Mode != "e" {
		t.Fatalf("%+v %v", cfg, err)
	}
}

func TestPickModeExactElseNearest(t *testing.T) {
	modes := []DisplayResolution{{Width: 1920, Height: 1080}, {Width: 1280, Height: 720}}
	if m, _ := pickMode(modes, 1280, 720); m.Width != 1280 {
		t.Fatal(m)
	}
	// The panel's "match window" sizes: the largest mode that fits.
	if m, _ := pickMode(modes, 1500, 900); m.Width != 1280 {
		t.Fatal(m)
	}
}
