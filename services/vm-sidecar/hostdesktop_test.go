package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseXorgCmdline(t *testing.T) {
	cases := []struct {
		args       []string
		disp, auth string
		ok         bool
	}{
		{[]string{"/usr/lib/xorg/Xorg", ":0", "-seat", "seat0", "-auth", "/var/run/lightdm/root/:0", "-nolisten", "tcp", "vt7"}, ":0", "/var/run/lightdm/root/:0", true},
		{[]string{"/usr/libexec/Xorg", "vt2", "-displayfd", "3", "-auth", "/run/user/1000/gdm/Xauthority", "-nolisten", "tcp"}, ":0", "/run/user/1000/gdm/Xauthority", true},
		{[]string{"/usr/bin/X", ":1", "-auth", "/run/sddm/xauth_ABC"}, ":1", "/run/sddm/xauth_ABC", true},
		{[]string{"/usr/bin/Xwayland", ":0", "-rootless"}, "", "", false},
		{[]string{"/usr/bin/bash", ":0"}, "", "", false},
		{nil, "", "", false},
	}
	for _, c := range cases {
		d, a, ok := parseXorgCmdline(c.args)
		if d != c.disp || a != c.auth || ok != c.ok {
			t.Errorf("parseXorgCmdline(%v) = %q %q %v, want %q %q %v", c.args, d, a, ok, c.disp, c.auth, c.ok)
		}
	}
}

func TestPickHostX(t *testing.T) {
	servers := []xorgServer{{":0", "/run/greeter"}, {":1", "/run/user/1000/gdm/Xauthority"}}
	if d, a := pickHostX(seatSession{Type: "x11", Display: ":1"}, servers); d != ":1" || a != "/run/user/1000/gdm/Xauthority" {
		t.Fatalf("active x11 session should win, got %q %q", d, a)
	}
	if d, _ := pickHostX(seatSession{Type: "tty"}, servers); d != ":0" {
		t.Fatalf("startx from a tty session should use the running Xorg, got %q", d)
	}
	if d, _ := pickHostX(seatSession{Type: "wayland"}, servers); d != "" {
		t.Fatalf("a wayland session must not fall back to some other Xorg, got %q", d)
	}
	if d, _ := pickHostX(seatSession{Type: "x11", Display: ":2"}, nil); d != ":2" {
		t.Fatalf("x11 session invisible in /proc should still resolve, got %q", d)
	}
	if d, _ := pickHostX(seatSession{}, nil); d != "" {
		t.Fatalf("nothing running should resolve to nothing, got %q", d)
	}
}

const sampleXrandr = `Screen 0: minimum 8 x 8, current 1920 x 1080, maximum 32767 x 32767
DP-0 disconnected (normal left inverted right x axis y axis)
HDMI-0 connected primary 1920x1080+0+0 (normal left inverted right x axis y axis) 552mm x 291mm
   1920x1080     60.00*+
   1680x1050     59.95
   1280x1024     75.02    60.02
   1280x1024     50.00
   1024x768i     43.00
DP-1 disconnected (normal left inverted right x axis y axis)
`

func TestParseXrandr(t *testing.T) {
	st := parseXrandr(sampleXrandr)
	if st.Width != 1920 || st.Height != 1080 || st.Output != "HDMI-0" {
		t.Fatalf("got %+v", st)
	}
	if len(st.Modes) != 4 {
		t.Fatalf("want 4 deduplicated modes, got %+v", st.Modes)
	}
	if st.Modes[0].Label != "1920 x 1080 (1080p Full HD)" || st.Modes[3].Width != 1024 {
		t.Fatalf("unexpected modes %+v", st.Modes)
	}
}

func TestParseXrandrHeadless(t *testing.T) {
	st := parseXrandr("Screen 0: minimum 320 x 200, current 1024 x 768, maximum 16384 x 16384\nVirtual-1 disconnected\n")
	if st.Output != "" || len(st.Modes) != 0 || st.Width != 1024 {
		t.Fatalf("headless parse wrong: %+v", st)
	}
}

func TestHostDesktopSettingsRoundTrip(t *testing.T) {
	def := parseHostDesktopSettings("")
	if def.FixScreen != 0 || !def.NoXDamage {
		t.Fatalf("defaults wrong: %+v", def)
	}
	s := HostDesktopSettings{FixScreen: 5, NoXDamage: false}
	if got := parseHostDesktopSettings(s.render()); got != s {
		t.Fatalf("round trip: %+v != %+v", got, s)
	}
	if got := parseHostDesktopSettings("FIXSCREEN=-3\nFIXSCREEN=abc\nNOXDAMAGE=maybe\n"); got != def {
		t.Fatalf("invalid values must fall back to defaults, got %+v", got)
	}
}

func TestEmbeddedHostDesktopFiles(t *testing.T) {
	script := string(hostDesktopScriptContent)
	for _, want := range []string{"-unixsock", "-rfbport 0", "-noprimary", "-add_keysyms", "--resolve"} {
		if !strings.Contains(script, want) {
			t.Errorf("wrapper script lacks %q", want)
		}
	}
	for _, bad := range []string{"websockify", "28642", "-rfbport 5900"} {
		if strings.Contains(script, bad) {
			t.Errorf("wrapper script still contains %q", bad)
		}
	}
	unit := string(hostDesktopUnitContent)
	if strings.Contains(unit, "Wants=lightdm") || !strings.Contains(unit, "StartLimitBurst=") {
		t.Errorf("unit must not want lightdm and must rate-limit restarts:\n%s", unit)
	}
	if !strings.Contains(string(deInstallScriptContent), "--status") {
		t.Error("DE provisioner not embedded")
	}
}

func TestHostRoutesNeverSkipAuthOnLoopback(t *testing.T) {
	var reached bool
	h := requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached = true }), t.TempDir())
	for _, path := range []string{"/host/console", "/host/desktop/install", "/host/display"} {
		reached = false
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.RemoteAddr = "127.0.0.1:5555"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if reached || w.Code != http.StatusUnauthorized {
			t.Errorf("%s from loopback without a token: code %d reached=%v", path, w.Code, reached)
		}
	}
	reached = false
	r := httptest.NewRequest(http.MethodGet, "/setup/status", nil)
	r.RemoteAddr = "127.0.0.1:5555"
	h.ServeHTTP(httptest.NewRecorder(), r)
	if !reached {
		t.Error("non-/host loopback automation should still pass")
	}
}

func withFakeHost(t *testing.T, present map[string]bool, systemd bool, run func(name string, args []string) error) {
	t.Helper()
	oldLook, oldRun, oldSd := hostLookPath, hostRunCmd, hostHasSystemd
	t.Cleanup(func() { hostLookPath, hostRunCmd, hostHasSystemd = oldLook, oldRun, oldSd })
	hostLookPath = func(b string) (string, error) {
		if present[b] {
			return "/usr/bin/" + b, nil
		}
		return "", errors.New("not found")
	}
	hostHasSystemd = func() bool { return systemd }
	hostRunCmd = func(ctx context.Context, out io.Writer, env []string, name string, args ...string) error {
		return run(name, args)
	}
}

func TestInstallHostPackagesPerDistroOneAtATime(t *testing.T) {
	present := map[string]bool{"pacman": true}
	var installs []string
	withFakeHost(t, present, true, func(name string, args []string) error {
		if name == "pacman" && args[0] == "-S" {
			pkg := args[len(args)-1]
			installs = append(installs, pkg)
			if pkg == "xorg-xrefresh" {
				return errors.New("target not found")
			}
			bin := map[string]string{"x11vnc": "x11vnc", "xdotool": "xdotool", "xorg-xrandr": "xrandr", "xorg-xset": "xset", "xorg-xdpyinfo": "xdpyinfo"}[pkg]
			present[bin] = true
		}
		return nil
	})
	pm, err := detectHostPkgManager()
	if err != nil || pm.Name != "pacman" {
		t.Fatalf("detect: %v %+v", err, pm)
	}
	warnings, err := installHostPackages(context.Background(), pm, io.Discard)
	if err != nil {
		t.Fatalf("optional package failure must not fail the install: %v", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "xrefresh") {
		t.Fatalf("want one xrefresh warning, got %v", warnings)
	}
	if strings.Join(installs, ",") != "x11vnc,xdotool,xorg-xrandr,xorg-xset,xorg-xrefresh,xorg-xdpyinfo" {
		t.Fatalf("unexpected install sequence %v", installs)
	}
}

func TestInstallHostPackagesMissingX11vncIsAnError(t *testing.T) {
	present := map[string]bool{"zypper": true}
	withFakeHost(t, present, true, func(name string, args []string) error {
		if name == "zypper" && args[1] == "install" {
			return errors.New("not found in repos")
		}
		return nil
	})
	pm, _ := detectHostPkgManager()
	if _, err := installHostPackages(context.Background(), pm, io.Discard); err == nil || !strings.Contains(err.Error(), "x11vnc") {
		t.Fatalf("want x11vnc error, got %v", err)
	}
}

func TestDetectHostPkgManagerUnsupported(t *testing.T) {
	withFakeHost(t, map[string]bool{"apk": true}, false, func(string, []string) error { return nil })
	if _, err := detectHostPkgManager(); !errors.Is(err, errHostDesktopUnsupported) {
		t.Fatalf("Alpine/OpenRC must be reported unsupported, got %v", err)
	}
}

func TestNearestDisplayMode(t *testing.T) {
	modes := []DisplayResolution{{Width: 1920, Height: 1080}, {Width: 1680, Height: 1050}, {Width: 1280, Height: 800}}
	for _, c := range []struct{ w, h, ww, wh int }{
		{1734, 912, 1280, 800},   // largest that fits inside the window
		{1700, 1060, 1680, 1050}, // exact-ish fit
		{800, 600, 1280, 800},    // nothing fits: the closest one
	} {
		m, ok := nearestDisplayMode(modes, c.w, c.h)
		if !ok || m.Width != c.ww || m.Height != c.wh {
			t.Errorf("%dx%d: got %dx%d", c.w, c.h, m.Width, m.Height)
		}
	}
	if _, ok := nearestDisplayMode(nil, 1000, 800); ok {
		t.Error("no modes should report none")
	}
}
