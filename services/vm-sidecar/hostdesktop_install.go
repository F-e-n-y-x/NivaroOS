// hostdesktop_install.go owns provisioning of the Host Desktop feature
// (streaming this machine's own X11 desktop through x11vnc).
//
// SINGLE SOURCE OF TRUTH: the x11vnc wrapper script, its systemd unit and
// the desktop-environment provisioner live in ./hostdesktop/ and are
// embedded into this binary. Every way of enabling Host Desktop goes
// through InstallHostDesktop below:
//   - the dashboard's Install button   -> POST /host/desktop/install
//   - installer/install.sh             -> nivaroos-vm-sidecar install-host-desktop
//   - `nivaroos host-desktop enable`   -> nivaroos-vm-sidecar install-host-desktop
//
// so a machine ends up identical whichever one provisioned it, and there is
// no hand-synced copy of the script anywhere else.
package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

//go:embed hostdesktop/nivaroos-host-desktop.sh
var hostDesktopScriptContent []byte

//go:embed hostdesktop/nivaroos-host-desktop.service
var hostDesktopUnitContent []byte

//go:embed hostdesktop/host-desktop-de-install.sh
var deInstallScriptContent []byte

const (
	hostDesktopScriptPath = "/usr/local/bin/nivaroos-host-desktop.sh"
	hostDesktopUnitPath   = "/usr/lib/systemd/system/nivaroos-host-desktop.service"
	hostDesktopUnitName   = "nivaroos-host-desktop.service"
	// deStatusScriptPath: the dashboard's terminal runs provisioning
	// actions from here, and GET /host/desktop/de-status runs --status.
	deStatusScriptPath = "/usr/local/bin/nivaroos-host-desktop-de-install.sh"
	// hostVNCSocketPath: x11vnc listens here only (no TCP port), created
	// 0600 root by the wrapper - see handleHostConsole.
	hostVNCSocketPath   = "/run/nivaroos/hostvnc.sock"
	hostDesktopConfPath = "/etc/nivaroos/host-desktop.conf"
	nivaroosManifest    = "/var/lib/nivaroos/manifest"
)

// deStatusTimeout covers --status's local checks plus a cached-metadata
// package availability query (dnf/zypper can take a few seconds).
const deStatusTimeout = 45 * time.Second

// hostDesktopInstallTimeout bounds a whole install (package downloads).
const hostDesktopInstallTimeout = 20 * time.Minute

// `nivaroos-vm-sidecar install-host-desktop` - handled before main()'s own
// flag parsing so the installer and CLI can run the exact same install
// logic as the dashboard without a running sidecar.
func init() {
	if len(os.Args) < 2 || os.Args[1] != "install-host-desktop" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), hostDesktopInstallTimeout)
	res, err := InstallHostDesktop(ctx, os.Stdout)
	cancel()
	for _, w := range res.Warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Println("Host Desktop streaming is installed and enabled.")
	os.Exit(0)
}

// ---------------------------------------------------------------------------
// Package installation
// ---------------------------------------------------------------------------

// hostPkgReq is one binary Host Desktop needs, with the package that
// provides it per package manager - candidates tried in order, each
// installed on its own so one missing name never blocks the rest.
type hostPkgReq struct {
	Binary     string
	Required   bool
	Why        string
	Candidates map[string][]string
	// Wayland: only installed where a Wayland session exists (see
	// hostdesktop/../hostvnc_wayland.go). Globs: presence is these files
	// (a GStreamer plugin), not Binary on PATH.
	Wayland bool
	Globs   []string
}

var hostDesktopPackages = []hostPkgReq{
	{Binary: "x11vnc", Required: true, Why: "the VNC server itself", Candidates: map[string][]string{
		"apt": {"x11vnc"}, "dnf": {"x11vnc"}, "pacman": {"x11vnc"}, "zypper": {"x11vnc"}}},
	{Binary: "xdotool", Why: "CapsLock auto-release", Candidates: map[string][]string{
		"apt": {"xdotool"}, "dnf": {"xdotool"}, "pacman": {"xdotool"}, "zypper": {"xdotool"}}},
	{Binary: "xrandr", Why: "resolution changes", Candidates: map[string][]string{
		"apt": {"x11-xserver-utils"}, "dnf": {"xrandr", "xorg-x11-server-utils"}, "pacman": {"xorg-xrandr"}, "zypper": {"xrandr"}}},
	{Binary: "xset", Why: "CapsLock indicator", Candidates: map[string][]string{
		"apt": {"x11-xserver-utils"}, "dnf": {"xset", "xorg-x11-server-utils"}, "pacman": {"xorg-xset"}, "zypper": {"xset"}}},
	{Binary: "xrefresh", Why: "screen refresh", Candidates: map[string][]string{
		"apt": {"x11-xserver-utils"}, "dnf": {"xrefresh", "xorg-x11-server-utils"}, "pacman": {"xorg-xrefresh"}, "zypper": {"xrefresh"}}},
	{Binary: "xdpyinfo", Why: "display/Wayland detection", Candidates: map[string][]string{
		"apt": {"x11-utils"}, "dnf": {"xdpyinfo", "xorg-x11-utils"}, "pacman": {"xorg-xdpyinfo"}, "zypper": {"xdpyinfo"}}},
	{Binary: "gst-launch-1.0", Wayland: true, Why: "Wayland screen capture (GNOME, KDE)", Candidates: map[string][]string{
		"apt": {"gstreamer1.0-tools"}, "dnf": {"gstreamer1"}, "pacman": {"gstreamer"}, "zypper": {"gstreamer-utils"}}},
	{Binary: "pipewiresrc", Wayland: true, Why: "Wayland screen capture (GStreamer PipeWire plugin)",
		Globs: gstPluginGlobs("libgstpipewire.so"), Candidates: map[string][]string{
			"apt": {"gstreamer1.0-pipewire"}, "dnf": {"pipewire-gstreamer"}, "pacman": {"gst-plugin-pipewire"}, "zypper": {"gstreamer-plugin-pipewire"}}},
	{Binary: "videoconvert", Wayland: true, Why: "Wayland screen capture (GStreamer base plugins)",
		Globs: gstPluginGlobs("libgstvideoconvert*.so"), Candidates: map[string][]string{
			"apt": {"gstreamer1.0-plugins-base"}, "dnf": {"gstreamer1-plugins-base"}, "pacman": {"gst-plugins-base"}, "zypper": {"gstreamer-plugins-base"}}},
	{Binary: "wayvnc", Wayland: true, Why: "Wayland streaming on wlroots compositors (sway, Hyprland, labwc...)", Candidates: map[string][]string{
		"apt": {"wayvnc"}, "dnf": {"wayvnc"}, "pacman": {"wayvnc"}, "zypper": {"wayvnc"}}},
}

func gstPluginGlobs(file string) []string {
	return []string{"/usr/lib/*/gstreamer-1.0/" + file, "/usr/lib64/gstreamer-1.0/" + file, "/usr/lib/gstreamer-1.0/" + file}
}

// present: whether this requirement is already met.
func (r hostPkgReq) present() bool {
	if len(r.Globs) == 0 {
		_, err := hostLookPath(r.Binary)
		return err == nil
	}
	for _, g := range r.Globs {
		if m, _ := hostGlob(g); len(m) > 0 {
			return true
		}
	}
	return false
}

// hostHasWaylandSessions: a Wayland desktop is installed (or running), so
// Host Desktop needs its Wayland packages too.
func hostHasWaylandSessions() bool {
	if m, _ := hostGlob("/usr/share/wayland-sessions/*.desktop"); len(m) > 0 {
		return true
	}
	return activeSeatSession().Type == "wayland"
}

type hostPkgManager struct {
	Name    string // key into hostPkgReq.Candidates
	Update  []string
	Install func(pkg string) []string
	Env     []string
}

var errHostDesktopUnsupported = errors.New("host desktop streaming needs a systemd distribution with apt, dnf, pacman or zypper (Debian, Ubuntu, Fedora, RHEL-likes, Arch, openSUSE); this system isn't one (e.g. Alpine/OpenRC)")

// Test seams.
var (
	hostLookPath = exec.LookPath
	hostRunCmd   = func(ctx context.Context, out io.Writer, env []string, name string, args ...string) error {
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Env = append(os.Environ(), env...)
		cmd.Stdout = out
		cmd.Stderr = out
		cmd.Stdin = nil
		return cmd.Run()
	}
	hostHasSystemd = func() bool {
		st, err := os.Stat("/run/systemd/system")
		return err == nil && st.IsDir()
	}
	hostGlob = filepath.Glob
)

func detectHostPkgManager() (*hostPkgManager, error) {
	if !hostHasSystemd() {
		return nil, errHostDesktopUnsupported
	}
	has := func(b string) bool { _, err := hostLookPath(b); return err == nil }
	switch {
	case has("apt-get"):
		return &hostPkgManager{
			Name:    "apt",
			Update:  []string{"apt-get", "update"},
			Install: func(p string) []string { return []string{"apt-get", "install", "-y", "--no-install-recommends", p} },
			Env:     []string{"DEBIAN_FRONTEND=noninteractive"},
		}, nil
	case has("dnf"):
		return &hostPkgManager{
			Name:    "dnf",
			Update:  []string{"dnf", "-y", "makecache"},
			Install: func(p string) []string { return []string{"dnf", "install", "-y", p} },
		}, nil
	case has("yum"):
		return &hostPkgManager{
			Name:    "dnf",
			Update:  []string{"yum", "-y", "makecache"},
			Install: func(p string) []string { return []string{"yum", "install", "-y", p} },
		}, nil
	case has("pacman"):
		return &hostPkgManager{
			Name:    "pacman",
			Update:  []string{"pacman", "-Sy", "--noconfirm"},
			Install: func(p string) []string { return []string{"pacman", "-S", "--noconfirm", "--needed", p} },
		}, nil
	case has("zypper"):
		return &hostPkgManager{
			Name:   "zypper",
			Update: []string{"zypper", "--non-interactive", "refresh"},
			Install: func(p string) []string {
				return []string{"zypper", "--non-interactive", "install", "--no-recommends", p}
			},
		}, nil
	}
	return nil, errHostDesktopUnsupported
}

func isUbuntuLike() bool {
	b, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return false
	}
	kv := parseKeyValueLines(string(b))
	ids := " " + strings.Trim(kv["ID"], `"`) + " " + strings.Trim(kv["ID_LIKE"], `"`) + " "
	return strings.Contains(ids, " ubuntu ")
}

// installHostPackages installs whatever of hostDesktopPackages isn't there
// yet (the Wayland ones only when wayland is set). Returns warnings for
// optional ones still missing and an error only when a required one
// (x11vnc) is.
func installHostPackages(ctx context.Context, pm *hostPkgManager, out io.Writer, wayland bool) ([]string, error) {
	var warnings []string
	var reqs []hostPkgReq
	for _, req := range hostDesktopPackages {
		if !req.Wayland || wayland {
			reqs = append(reqs, req)
		}
	}
	missing := false
	for _, req := range reqs {
		if !req.present() {
			missing = true
			break
		}
	}
	if !missing {
		return nil, nil
	}
	fmt.Fprintf(out, "==> Refreshing package lists (%s)\n", pm.Update[0])
	if err := hostRunCmd(ctx, out, pm.Env, pm.Update[0], pm.Update[1:]...); err != nil {
		warnings = append(warnings, fmt.Sprintf("refreshing package lists failed (%v) - trying the install anyway", err))
	}
	universeTried := false
	tried := map[string]error{}
	for _, req := range reqs {
		if req.present() {
			continue
		}
		var lastErr error
		for _, pkg := range req.Candidates[pm.Name] {
			if prev, done := tried[pkg]; done {
				lastErr = prev
				if prev == nil {
					break
				}
				continue
			}
			fmt.Fprintf(out, "==> Installing %s\n", pkg)
			args := pm.Install(pkg)
			err := hostRunCmd(ctx, out, pm.Env, args[0], args[1:]...)
			// x11vnc lives in Ubuntu's "universe" component, which minimal
			// server images don't always enable.
			if err != nil && pm.Name == "apt" && !universeTried && isUbuntuLike() {
				universeTried = true
				if _, lerr := hostLookPath("add-apt-repository"); lerr != nil {
					_ = hostRunCmd(ctx, out, pm.Env, "apt-get", "install", "-y", "software-properties-common")
				}
				if hostRunCmd(ctx, out, pm.Env, "add-apt-repository", "-y", "universe") == nil {
					_ = hostRunCmd(ctx, out, pm.Env, "apt-get", "update")
					err = hostRunCmd(ctx, out, pm.Env, args[0], args[1:]...)
				}
			}
			tried[pkg] = err
			lastErr = err
			if err == nil {
				break
			}
		}
		if req.present() {
			continue
		}
		msg := fmt.Sprintf("%s (%s) could not be installed", req.Binary, req.Why)
		if lastErr != nil {
			msg += ": " + lastErr.Error()
		}
		if req.Required {
			hint := ""
			if pm.Name == "dnf" && !isRegularFile("/etc/fedora-release") {
				hint = " - on RHEL/Rocky/AlmaLinux enable EPEL first (dnf install epel-release)"
			}
			return warnings, errors.New(msg + hint)
		}
		warnings = append(warnings, msg)
	}
	return warnings, nil
}

// ---------------------------------------------------------------------------
// Files
// ---------------------------------------------------------------------------

// writeFileIfChanged writes content atomically, reporting whether anything
// changed.
func writeFileIfChanged(path string, content []byte, mode os.FileMode) (bool, error) {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, content) {
		if st, err := os.Stat(path); err == nil && st.Mode().Perm() != mode {
			return false, os.Chmod(path, mode)
		}
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	tmp := path + ".nivaroos-tmp"
	if err := os.WriteFile(tmp, content, mode); err != nil {
		return false, err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return false, err
	}
	return true, nil
}

// writeHostDesktopFiles puts the embedded script/unit/provisioner on disk.
func writeHostDesktopFiles() (scriptChanged, unitChanged bool, err error) {
	if scriptChanged, err = writeFileIfChanged(hostDesktopScriptPath, hostDesktopScriptContent, 0o755); err != nil {
		return
	}
	if _, err = writeFileIfChanged(deStatusScriptPath, deInstallScriptContent, 0o755); err != nil {
		return
	}
	unitChanged, err = writeFileIfChanged(hostDesktopUnitPath, hostDesktopUnitContent, 0o644)
	return
}

// recordInManifest appends paths to install.sh's manifest (which
// nivaroos-uninstall walks) when it exists.
func recordInManifest(paths ...string) {
	b, err := os.ReadFile(nivaroosManifest)
	if err != nil {
		return
	}
	have := map[string]bool{}
	for _, l := range strings.Split(string(b), "\n") {
		have[strings.TrimSpace(l)] = true
	}
	f, err := os.OpenFile(nivaroosManifest, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	if len(b) > 0 && b[len(b)-1] != '\n' {
		_, _ = f.WriteString("\n")
	}
	for _, p := range paths {
		if !have[p] {
			_, _ = f.WriteString(p + "\n")
		}
	}
}

// killLegacyWebsockify stops the unauthenticated websockify proxy older
// versions started on 0.0.0.0:28642 (which also blocked download-sidecar).
func killLegacyWebsockify() {
	_ = exec.Command("pkill", "-f", "websockify.*28642.*5900").Run()
}

// ---------------------------------------------------------------------------
// Install / state
// ---------------------------------------------------------------------------

type HostDesktopInstallResult struct {
	Installed bool     `json:"installed"`
	Warnings  []string `json:"warnings"`
	Log       string   `json:"log"`
}

var hostDesktopInstallMu sync.Mutex

// InstallHostDesktop installs the packages (per distro, one at a time),
// writes the embedded files, and enables the service. Returns a real error
// when x11vnc can't be installed or the unit can't be enabled - in which
// case the unit is not left behind looking "installed".
func InstallHostDesktop(ctx context.Context, progress io.Writer) (HostDesktopInstallResult, error) {
	hostDesktopInstallMu.Lock()
	defer hostDesktopInstallMu.Unlock()

	var logBuf bytes.Buffer
	out := io.Writer(&logBuf)
	if progress != nil {
		out = io.MultiWriter(&logBuf, progress)
	}
	res := HostDesktopInstallResult{Warnings: []string{}}
	finish := func(err error) (HostDesktopInstallResult, error) {
		res.Log = logBuf.String()
		res.Installed = IsHostDesktopInstalled()
		return res, err
	}

	pm, err := detectHostPkgManager()
	if err != nil {
		return finish(err)
	}
	warnings, err := installHostPackages(ctx, pm, out, hostHasWaylandSessions())
	res.Warnings = append(res.Warnings, warnings...)
	if err != nil {
		return finish(err)
	}

	fmt.Fprintln(out, "==> Writing Host Desktop service files")
	if _, _, err := writeHostDesktopFiles(); err != nil {
		return finish(fmt.Errorf("writing Host Desktop files: %w", err))
	}
	recordInManifest(hostDesktopScriptPath, deStatusScriptPath, hostDesktopUnitPath)
	killLegacyWebsockify()

	if err := hostRunCmd(ctx, out, nil, "systemctl", "daemon-reload"); err != nil {
		return finish(fmt.Errorf("systemctl daemon-reload: %w", err))
	}
	if err := hostRunCmd(ctx, out, nil, "systemctl", "enable", hostDesktopUnitName); err != nil {
		return finish(fmt.Errorf("systemctl enable %s: %w", hostDesktopUnitName, err))
	}
	// restart (not just start) so a re-install picks up new files.
	if err := hostRunCmd(ctx, out, nil, "systemctl", "restart", hostDesktopUnitName); err != nil {
		return finish(fmt.Errorf("systemctl restart %s: %w", hostDesktopUnitName, err))
	}
	return finish(nil)
}

// IsHostDesktopInstalled: the unit is in place AND x11vnc actually exists.
func IsHostDesktopInstalled() bool {
	if _, err := os.Stat(hostDesktopUnitPath); err != nil {
		return false
	}
	_, err := hostLookPath("x11vnc")
	return err == nil
}

// syncHostDesktopFiles runs once at sidecar start on machines that already
// have Host Desktop: brings the on-disk script/unit up to date with this
// binary (e.g. migrating old websockify/TCP-5900 installs to the unix
// socket), and restarts the service only when something changed.
func syncHostDesktopFiles() {
	if _, err := os.Stat(hostDesktopUnitPath); err != nil {
		return
	}
	scriptChanged, unitChanged, err := writeHostDesktopFiles()
	if err != nil {
		log.Printf("host desktop: updating files: %v", err)
		return
	}
	killLegacyWebsockify()
	// Installs from before Wayland support never got its packages: add them
	// once the machine is actually on a Wayland session (the wrapper waits
	// for them meanwhile).
	if activeSeatSession().Type == "wayland" {
		if pm, err := detectHostPkgManager(); err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), hostDesktopInstallTimeout)
			if w, err := installHostPackages(ctx, pm, io.Discard, true); err != nil || len(w) > 0 {
				log.Printf("host desktop: Wayland packages: %v %v", err, w)
			}
			cancel()
		}
	}
	if unitChanged {
		_ = exec.Command("systemctl", "daemon-reload").Run()
	}
	if scriptChanged || unitChanged {
		log.Printf("host desktop: service files updated - restarting %s", hostDesktopUnitName)
		_ = exec.Command("systemctl", "try-restart", hostDesktopUnitName).Run()
	}
}

type HostDesktopStatus struct {
	Installed       bool   `json:"installed"`
	ServiceActive   bool   `json:"service_active"`
	ServiceState    string `json:"service_state"`
	SocketPresent   bool   `json:"socket_present"`
	Display         string `json:"display"`
	SessionType     string `json:"session_type"`
	XWayland        bool   `json:"xwayland"`
	Desktop         string `json:"desktop"`
	Reason          string `json:"reason"`
	DistroSupported bool   `json:"distro_supported"`
	// Wayland only: which backend streams it (mutter, wayvnc, portal), and
	// whether the host user has to click "Allow" on the server's screen
	// (needs_consent) or declined it (consent_denied).
	Backend       string `json:"backend,omitempty"`
	NeedsConsent  bool   `json:"needs_consent"`
	ConsentDenied bool   `json:"consent_denied"`
	// Locked: GNOME won't share a locked screen (POST /host/desktop/unlock).
	Locked bool `json:"locked"`
}

func hostDesktopServiceState() string {
	out, err := exec.Command("systemctl", "show", "-p", "ActiveState", "--value", hostDesktopUnitName).Output()
	if err != nil {
		return "unknown"
	}
	switch s := strings.TrimSpace(string(out)); s {
	case "active", "activating", "failed", "inactive":
		return s
	case "deactivating", "reloading":
		return "activating"
	default:
		return "unknown"
	}
}

func socketListening(path string) bool {
	c, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

func GetHostDesktopStatus() HostDesktopStatus {
	_, pmErr := detectHostPkgManager()
	st := HostDesktopStatus{
		Installed:       IsHostDesktopInstalled(),
		ServiceState:    hostDesktopServiceState(),
		DistroSupported: pmErr == nil,
	}
	st.ServiceActive = st.ServiceState == "active"
	st.SocketPresent = socketListening(hostVNCSocketPath)

	state := readHostDesktopState()
	t := resolveHostX()
	st.Display, st.SessionType, st.Desktop, st.XWayland = t.Display, t.SessionType, t.Desktop, t.XWayland
	if st.SessionType == "" {
		st.SessionType = state["SESSION_TYPE"]
	}
	var wl map[string]string
	if state["SESSION_TYPE"] == "wayland" {
		b, _ := os.ReadFile(hostDesktopWaylandStatusPath)
		wl = parseKeyValueLines(string(b))
		st.SessionType, st.Backend = "wayland", wl["BACKEND"]
		st.NeedsConsent = wl["STATE"] == "consent"
		st.ConsentDenied = wl["STATE"] == "denied"
		st.Locked = wl["STATE"] == "locked"
	}

	switch {
	case !st.DistroSupported:
		st.Reason = errHostDesktopUnsupported.Error()
	case !st.Installed:
		st.Reason = "Host Desktop streaming is not installed."
	case st.ServiceState == "failed":
		st.Reason = "The Host Desktop service (" + hostDesktopUnitName + ") has failed - restart it, or check: journalctl -u " + hostDesktopUnitName
	case st.ServiceState == "inactive" || st.ServiceState == "unknown":
		st.Reason = "The Host Desktop service is not running."
	case !st.SocketPresent && wl["REASON"] != "":
		st.Reason = wl["REASON"]
	case !st.SocketPresent && state["REASON"] != "":
		st.Reason = state["REASON"]
	case !st.SocketPresent:
		st.Reason = "The Host Desktop VNC server is starting or waiting for a desktop session."
	}
	return st
}

// ---------------------------------------------------------------------------
// Settings (x11vnc tuning, read by the wrapper script)
// ---------------------------------------------------------------------------

type HostDesktopSettings struct {
	// FixScreen: seconds between full framebuffer re-reads (0 = off).
	FixScreen int `json:"fixscreen"`
	// NoXDamage: ignore the X DAMAGE extension and poll instead.
	NoXDamage bool `json:"noxdamage"`
	// FPS: frames per second x11vnc aims for (0 = its default, ~50).
	FPS int `json:"fps"`
	// SnapFB: capture whole frames instead of tile by tile (-snapfb).
	SnapFB bool `json:"snapfb"`
}

func parseHostDesktopSettings(content string) HostDesktopSettings {
	s := HostDesktopSettings{FixScreen: 0, NoXDamage: true}
	kv := parseKeyValueLines(content)
	if v, err := strconv.Atoi(kv["FIXSCREEN"]); err == nil && v >= 0 && v <= 3600 {
		s.FixScreen = v
	}
	switch kv["NOXDAMAGE"] {
	case "0":
		s.NoXDamage = false
	case "1":
		s.NoXDamage = true
	}
	if v, err := strconv.Atoi(kv["FPS"]); err == nil && v >= 0 && v <= 60 {
		s.FPS = v
	}
	s.SnapFB = kv["SNAPFB"] == "1"
	return s
}

func (s HostDesktopSettings) render() string {
	nx := "0"
	if s.NoXDamage {
		nx = "1"
	}
	snap := "0"
	if s.SnapFB {
		snap = "1"
	}
	return fmt.Sprintf("# NivaroOS Host Desktop x11vnc settings (managed by the dashboard)\nFIXSCREEN=%d\nNOXDAMAGE=%s\nFPS=%d\nSNAPFB=%s\n", s.FixScreen, nx, s.FPS, snap)
}

func readHostDesktopSettings() HostDesktopSettings {
	b, _ := os.ReadFile(hostDesktopConfPath)
	return parseHostDesktopSettings(string(b))
}

// ---------------------------------------------------------------------------
// Routes
// ---------------------------------------------------------------------------

func RegisterHostDesktopInstallRoutes(mux *http.ServeMux) {
	go syncHostDesktopFiles()

	mux.HandleFunc("GET /host/desktop/installed", func(w http.ResponseWriter, r *http.Request) {
		_, unitErr := os.Stat(hostDesktopUnitPath)
		_, vncErr := hostLookPath("x11vnc")
		writeJSON(w, http.StatusOK, map[string]bool{
			"installed":      unitErr == nil && vncErr == nil,
			"unit_present":   unitErr == nil,
			"x11vnc_present": vncErr == nil,
		})
	})

	mux.HandleFunc("POST /host/desktop/install", func(w http.ResponseWriter, r *http.Request) {
		// Not tied to r.Context(): a browser tab closing mid-install must
		// not kill apt/dnf halfway through.
		ctx, cancel := context.WithTimeout(context.Background(), hostDesktopInstallTimeout)
		defer cancel()
		res, err := InstallHostDesktop(ctx, nil)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
				"error": err.Error(), "log": res.Log, "installed": res.Installed, "warnings": res.Warnings,
			})
			return
		}
		writeJSON(w, http.StatusOK, res)
	})

	mux.HandleFunc("GET /host/desktop/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, GetHostDesktopStatus())
	})

	mux.HandleFunc("POST /host/desktop/restart", func(w http.ResponseWriter, r *http.Request) {
		if !IsHostDesktopInstalled() {
			writeError(w, http.StatusConflict, errors.New("host desktop streaming is not installed"))
			return
		}
		_ = exec.Command("systemctl", "reset-failed", hostDesktopUnitName).Run()
		if out, err := exec.Command("systemctl", "restart", hostDesktopUnitName).CombinedOutput(); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("systemctl restart: %s (%w)", strings.TrimSpace(string(out)), err))
			return
		}
		// Give the wrapper a moment to find the display and open the socket.
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) && !socketListening(hostVNCSocketPath) {
			time.Sleep(500 * time.Millisecond)
		}
		writeJSON(w, http.StatusOK, GetHostDesktopStatus())
	})

	// GNOME ends and refuses screen sharing while the screen is locked: let
	// the viewer unlock it (the admin token this needs already means root
	// on this box). Locked again once the last viewer has left.
	mux.HandleFunc("POST /host/desktop/unlock", func(w http.ResponseWriter, r *http.Request) {
		id := activeSeatSession().ID
		if id == "" {
			writeError(w, http.StatusConflict, errors.New("nobody is signed in on the server's screen"))
			return
		}
		if out, err := exec.Command("loginctl", "unlock-session", id).CombinedOutput(); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("loginctl unlock-session: %s (%w)", strings.TrimSpace(string(out)), err))
			return
		}
		hostUnlockedMu.Lock()
		hostUnlockedSession = id
		hostUnlockedMu.Unlock()
		writeJSON(w, http.StatusOK, map[string]bool{"unlocked": true})
	})

	mux.HandleFunc("GET /host/desktop/settings", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, readHostDesktopSettings())
	})

	mux.HandleFunc("PUT /host/desktop/settings", func(w http.ResponseWriter, r *http.Request) {
		s := readHostDesktopSettings()
		var req struct {
			FixScreen *int  `json:"fixscreen"`
			NoXDamage *bool `json:"noxdamage"`
			FPS       *int  `json:"fps"`
			SnapFB    *bool `json:"snapfb"`
		}
		if err := decodeJSONBody(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		if req.FixScreen != nil {
			if *req.FixScreen < 0 || *req.FixScreen > 3600 {
				writeError(w, http.StatusBadRequest, errors.New("fixscreen must be 0 (off) to 3600 seconds"))
				return
			}
			s.FixScreen = *req.FixScreen
		}
		if req.NoXDamage != nil {
			s.NoXDamage = *req.NoXDamage
		}
		if req.FPS != nil {
			if *req.FPS < 0 || *req.FPS > 60 {
				writeError(w, http.StatusBadRequest, errors.New("fps must be 0 (default) to 60"))
				return
			}
			s.FPS = *req.FPS
		}
		if req.SnapFB != nil {
			s.SnapFB = *req.SnapFB
		}
		if _, err := writeFileIfChanged(hostDesktopConfPath, []byte(s.render()), 0o644); err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		_ = exec.Command("systemctl", "try-restart", hostDesktopUnitName).Run()
		writeJSON(w, http.StatusOK, s)
	})

	// Whether streaming is installed (above) and whether there's an X11
	// desktop for it to stream are separate questions - the provisioner's
	// --status answers the second, JSON passed straight through.
	mux.HandleFunc("GET /host/desktop/de-status", func(w http.ResponseWriter, r *http.Request) {
		// Keep the on-disk copy (which the dashboard terminal runs) in
		// step with this binary.
		if _, err := writeFileIfChanged(deStatusScriptPath, deInstallScriptContent, 0o755); err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("writing %s: %w", deStatusScriptPath, err))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), deStatusTimeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, "bash", deStatusScriptPath, "--status").Output()
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Errorf("desktop detection failed: %w", err))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(out)
	})

	// The statusbar CapsLock indicator needs the HOST's lock state, not the
	// browser device's own keyboard.
	mux.HandleFunc("GET /host/desktop/capslock", func(w http.ResponseWriter, r *http.Request) {
		on, err := GetHostCapsLock()
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, errNoHostDisplay) {
				status = http.StatusServiceUnavailable
			}
			writeError(w, status, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"caps_lock": on})
	})
}

func decodeJSONBody(r *http.Request, v interface{}) error {
	b, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return nil
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("invalid json request: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// CapsLock
// ---------------------------------------------------------------------------

var capsLockLineRe = regexp.MustCompile(`(?i)Caps Lock:\s*(on|off)`)

// GetHostCapsLock reports the host X server's CapsLock state from
// `xset q`'s XKB indicators.
func GetHostCapsLock() (bool, error) {
	t := resolveHostX()
	if t.Display == "" || t.XWayland {
		return false, errNoHostDisplay
	}
	out, err := xCommand(t, "xset", "q").Output()
	if err != nil {
		return false, err
	}
	m := capsLockLineRe.FindSubmatch(out)
	if m == nil {
		return false, nil
	}
	return string(m[1]) == "on", nil
}

// Host console activity, maintained by handleHostConsole (console.go).
var (
	hostConsoleSessions  atomic.Int32
	hostConsoleLastInput atomic.Int64 // unix nanos of the last client->VNC write
)

func noteHostConsoleInput() { hostConsoleLastInput.Store(time.Now().UnixNano()) }

// The session POST /host/desktop/unlock unlocked, re-locked by
// noteHostConsoleClosed once no viewer has been connected for a minute.
var (
	hostUnlockedMu        sync.Mutex
	hostUnlockedSession   string
	hostConsoleLastClosed atomic.Int64
)

func noteHostConsoleClosed() {
	if hostConsoleSessions.Add(-1) != 0 {
		return
	}
	hostConsoleLastClosed.Store(time.Now().UnixNano())
	time.AfterFunc(time.Minute, func() {
		// A viewer that came back (or left again later) keeps it unlocked.
		if hostConsoleSessions.Load() != 0 || time.Since(time.Unix(0, hostConsoleLastClosed.Load())) < time.Minute-time.Second {
			return
		}
		hostUnlockedMu.Lock()
		id := hostUnlockedSession
		hostUnlockedSession = ""
		hostUnlockedMu.Unlock()
		if id != "" {
			_ = exec.Command("loginctl", "lock-session", id).Run()
		}
	})
}

func hostConsoleIdleFor() time.Duration {
	last := hostConsoleLastInput.Load()
	if last == 0 {
		return 0
	}
	return time.Since(time.Unix(0, last))
}

const (
	hostCapsLockPollInterval = 15 * time.Second
	// hostCapsLockAutoOffAfter: CapsLock continuously on AND no input from
	// the Host Desktop viewer for this long -> release it.
	hostCapsLockAutoOffAfter = 5 * time.Minute
)

// StartHostCapsLockWatcher releases a CapsLock that a remote viewer left
// stuck on. It only acts while a /host/console session is open and has
// been idle for hostCapsLockAutoOffAfter - never when nobody is streaming,
// so it can't fight someone typing on the physical keyboard.
func StartHostCapsLockWatcher() {
	go func() {
		var onSince time.Time
		for {
			time.Sleep(hostCapsLockPollInterval)
			if hostConsoleSessions.Load() == 0 || hostConsoleIdleFor() < hostCapsLockAutoOffAfter {
				onSince = time.Time{}
				continue
			}
			on, err := GetHostCapsLock()
			if err != nil || !on {
				onSince = time.Time{}
				continue
			}
			if onSince.IsZero() {
				onSince = time.Now()
				continue
			}
			if time.Since(onSince) < hostCapsLockAutoOffAfter {
				continue
			}
			// Re-check right before toggling: Caps_Lock is a toggle, so
			// pressing it on an already-released lock would turn it ON.
			if on, err := GetHostCapsLock(); err != nil || !on || hostConsoleIdleFor() < hostCapsLockAutoOffAfter {
				onSince = time.Time{}
				continue
			}
			t := resolveHostX()
			if err := xCommand(t, "xdotool", "key", "Caps_Lock").Run(); err != nil {
				log.Printf("host desktop: CapsLock auto-release failed: %v", err)
			} else {
				log.Printf("host desktop: CapsLock was left on by an idle viewer - released")
			}
			onSince = time.Time{}
		}
	}()
}
