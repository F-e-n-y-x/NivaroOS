// hostdevices.go enumerates host USB and PCI devices for the VM
// hardware-passthrough picker (Create/Edit VM's Hardware section) - pure
// host inspection, no libvirt or VM state involved.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	osuser "os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

type HostUSBDevice struct {
	VendorID    string `json:"vendor_id"`
	ProductID   string `json:"product_id"`
	Description string `json:"description"`
}

type HostPCIDevice struct {
	Address     string `json:"address"`
	Description string `json:"description"`
}

// HostCapabilities is the JSON shape returned by GET /host/capabilities -
// what the Create/Edit VM hardware picker has available to offer.
// Includes host CPU core count, total RAM, and available RAM so VM creation
// and edit dialogs match the host machine's actual hardware specs.
type HostCapabilities struct {
	CPUCores           int             `json:"cpu_cores"`
	TotalMemoryMiB     int64           `json:"total_memory_mib"`
	AvailableMemoryMiB int64           `json:"available_memory_mib,omitempty"`
	IOMMUEnabled       bool            `json:"iommu_enabled"`
	USBDevices         []HostUSBDevice `json:"usb_devices"`
	PCIDevices         []HostPCIDevice `json:"pci_devices"`
}

func parseMeminfo(content string) (int64, int64) {
	var totalMemMiB, availMemMiB int64
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if kb, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
					totalMemMiB = kb / 1024
				}
			}
		} else if strings.HasPrefix(line, "MemAvailable:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if kb, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
					availMemMiB = kb / 1024
				}
			}
		}
	}
	return totalMemMiB, availMemMiB
}

func readHostSystemSpecs() (int, int64, int64) {
	cores := runtime.NumCPU()
	content, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return cores, 8192, 4096
	}
	totalMemMiB, availMemMiB := parseMeminfo(string(content))
	if totalMemMiB <= 0 {
		totalMemMiB = 8192
	}
	return cores, totalMemMiB, availMemMiB
}

var lsusbLineRe = regexp.MustCompile(`^Bus \d+ Device \d+: ID ([0-9a-fA-F]{4}):([0-9a-fA-F]{4})\s*(.*)$`)

// parseLsusbOutput is the pure parsing half of listHostUSBDevices, split
// out so it's testable against captured sample output without needing
// lsusb actually installed. Root hubs (the "Linux Foundation ... root
// hub" entry every USB controller shows up as) are filtered out since
// they're not real, passthrough-able peripherals.
func parseLsusbOutput(output string) []HostUSBDevice {
	devices := []HostUSBDevice{}
	for _, line := range strings.Split(output, "\n") {
		m := lsusbLineRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		desc := strings.TrimSpace(m[3])
		if strings.Contains(desc, "root hub") {
			continue
		}
		devices = append(devices, HostUSBDevice{VendorID: m[1], ProductID: m[2], Description: desc})
	}
	return devices
}

func listHostUSBDevices() ([]HostUSBDevice, error) {
	out, err := exec.Command("lsusb").Output()
	if err != nil {
		return nil, fmt.Errorf("lsusb: %w", err)
	}
	return parseLsusbOutput(string(out)), nil
}

var lspciLineRe = regexp.MustCompile(`^(\S+)\s+(.+)$`)

// parseLspciOutput is the pure parsing half of listHostPCIDevices - see
// parseLsusbOutput. Expects `lspci -D` output specifically: domain-
// prefixed addresses, so every result is already in the dddd:bb:ss.f
// form parsePCIAddress and the domain XML template both expect.
func parseLspciOutput(output string) []HostPCIDevice {
	devices := []HostPCIDevice{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		m := lspciLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		devices = append(devices, HostPCIDevice{Address: m[1], Description: m[2]})
	}
	return devices
}

func listHostPCIDevices() ([]HostPCIDevice, error) {
	out, err := exec.Command("lspci", "-D").Output()
	if err != nil {
		return nil, fmt.Errorf("lspci: %w", err)
	}
	return parseLspciOutput(string(out)), nil
}

func GetHostCapabilities() (HostCapabilities, error) {
	cores, totalMem, availMem := readHostSystemSpecs()
	usb, err := listHostUSBDevices()
	if err != nil {
		usb = []HostUSBDevice{}
	}
	pci, err := listHostPCIDevices()
	if err != nil {
		pci = []HostPCIDevice{}
	}
	return HostCapabilities{
		CPUCores:           cores,
		TotalMemoryMiB:     totalMem,
		AvailableMemoryMiB: availMem,
		IOMMUEnabled:       iommuEnabled(),
		USBDevices:         usb,
		PCIDevices:         pci,
	}, nil
}

type HostDisplayInfo struct {
	Current string `json:"current"`
	Width   int    `json:"width"`
	Height  int    `json:"height"`
	// Display is the X display actually being streamed (":0", ":1"...).
	Display string `json:"display"`
	// Output is the connected RandR output resolutions apply to ("" when
	// headless - then only the framebuffer size can change).
	Output   string `json:"output"`
	Headless bool   `json:"headless"`
	// Resolutions are the output's real xrandr modes; the standard list
	// below only when there is no connected output to ask.
	Resolutions []DisplayResolution `json:"resolutions"`
}

type DisplayResolution struct {
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Label  string `json:"label"`
}

var standardResolutions = []DisplayResolution{
	{Width: 1920, Height: 1080, Label: "1920 x 1080 (1080p Full HD)"},
	{Width: 1680, Height: 1050, Label: "1680 x 1050 (WSXGA+ 16:10)"},
	{Width: 1600, Height: 900, Label: "1600 x 900 (HD+)"},
	{Width: 1440, Height: 900, Label: "1440 x 900 (WXGA+)"},
	{Width: 1366, Height: 768, Label: "1366 x 768 (Standard Laptop)"},
	{Width: 1280, Height: 1024, Label: "1280 x 1024 (SXGA 5:4)"},
	{Width: 1280, Height: 800, Label: "1280 x 800 (WXGA 16:10)"},
	{Width: 1280, Height: 720, Label: "1280 x 720 (720p HD)"},
	{Width: 1024, Height: 768, Label: "1024 x 768 (XGA 4:3)"},
	{Width: 800, Height: 600, Label: "800 x 600 (SVGA 4:3)"},
}

// errNoHostDisplay: no X11 display to talk to (nothing running, Wayland
// only, or the cookie can't be found). Handlers answer 503 with it rather
// than inventing a 1920x1080 display that doesn't exist.
var errNoHostDisplay = errors.New("no X11 display found on this machine - Host Desktop needs a running Xorg session (not Wayland)")

// hostXTarget is the X display Host Desktop streams and how to authenticate
// to it. It's resolved the same way the x11vnc wrapper
// (hostdesktop/nivaroos-host-desktop.sh) does it; while that wrapper is
// running we simply use what it resolved - including its root-only copy of
// the cookie, which matters because this service runs with ProtectHome and
// may not be able to read /run/user/<uid>/gdm/Xauthority or ~/.Xauthority.
type hostXTarget struct {
	Display     string
	XAuthority  string
	SessionType string
	Desktop     string
	XWayland    bool
}

// hostDesktopStatePath is written by the wrapper script (KEY=value lines).
const hostDesktopStatePath = "/run/nivaroos/host-desktop.env"

func parseKeyValueLines(content string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(content, "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || k == "" {
			continue
		}
		out[k] = v
	}
	return out
}

func readHostDesktopState() map[string]string {
	b, err := os.ReadFile(hostDesktopStatePath)
	if err != nil {
		return map[string]string{}
	}
	return parseKeyValueLines(string(b))
}

func isRegularFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// displayNumber turns ":1" / ":1.0" into "1".
func displayNumber(d string) string {
	d = strings.TrimPrefix(d, ":")
	if i := strings.IndexByte(d, '.'); i >= 0 {
		d = d[:i]
	}
	return d
}

func displaySocketExists(d string) bool {
	n := displayNumber(d)
	if n == "" {
		return false
	}
	st, err := os.Stat("/tmp/.X11-unix/X" + n)
	return err == nil && st.Mode()&os.ModeSocket != 0
}

// parseXorgCmdline recognises a real X server's argv (Xorg/X - never
// Xwayland, which x11vnc can't capture) and pulls out its display and
// -auth cookie file.
func parseXorgCmdline(args []string) (display, auth string, ok bool) {
	if len(args) == 0 {
		return "", "", false
	}
	switch filepath.Base(args[0]) {
	case "Xorg", "X", "Xorg.bin", "Xorg.wrap":
	default:
		return "", "", false
	}
	for i := 1; i < len(args); i++ {
		a := args[i]
		if display == "" && len(a) > 1 && a[0] == ':' && a[1] >= '0' && a[1] <= '9' {
			display = a
		}
		if a == "-auth" && i+1 < len(args) {
			auth = args[i+1]
		}
	}
	if display == "" {
		display = ":0"
	}
	return display, auth, true
}

type xorgServer struct{ Display, Auth string }

func listXorgServers() []xorgServer {
	dirs, _ := filepath.Glob("/proc/[0-9]*")
	seen := map[string]bool{}
	var out []xorgServer
	for _, d := range dirs {
		b, err := os.ReadFile(filepath.Join(d, "cmdline"))
		if err != nil || len(b) == 0 {
			continue
		}
		disp, auth, ok := parseXorgCmdline(strings.Split(strings.TrimRight(string(b), "\x00"), "\x00"))
		if !ok || seen[disp] {
			continue
		}
		seen[disp] = true
		out = append(out, xorgServer{Display: disp, Auth: auth})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Display < out[j].Display })
	return out
}

type seatSession struct{ Type, Display, Desktop, User, UID string }

func activeSeatSession() seatSession {
	var s seatSession
	out, err := exec.Command("loginctl", "show-seat", "seat0", "-p", "ActiveSession", "--value").Output()
	if err != nil {
		return s
	}
	id := strings.TrimSpace(string(out))
	if id == "" {
		return s
	}
	out, err = exec.Command("loginctl", "show-session", id, "-p", "Type", "-p", "Display", "-p", "Desktop", "-p", "Name", "-p", "User").Output()
	if err != nil {
		return s
	}
	kv := parseKeyValueLines(string(out))
	return seatSession{Type: kv["Type"], Display: kv["Display"], Desktop: kv["Desktop"], User: kv["Name"], UID: kv["User"]}
}

// guessXAuth mirrors the wrapper's guess_auth_for: well-known cookie
// locations for display managers that didn't pass -auth visibly.
func guessXAuth(display, uid, user string) string {
	cands := []string{"/run/lightdm/root/" + display, "/var/run/lightdm/root/" + display}
	if uid != "" {
		cands = append(cands, "/run/user/"+uid+"/gdm/Xauthority")
	}
	for _, pat := range []string{"/run/sddm/*", "/var/run/sddm/*"} {
		m, _ := filepath.Glob(pat)
		cands = append(cands, m...)
	}
	if user != "" {
		if u, err := osuser.Lookup(user); err == nil {
			cands = append(cands, filepath.Join(u.HomeDir, ".Xauthority"))
		}
	}
	cands = append(cands, "/root/.Xauthority")
	if m, err := filepath.Glob("/home/*/.Xauthority"); err == nil {
		cands = append(cands, m...)
	}
	for _, c := range cands {
		if isRegularFile(c) {
			return c
		}
	}
	return ""
}

// pickHostX is the pure decision half of resolveHostX: which X server to
// stream given the active seat0 session and the running Xorg servers.
func pickHostX(sess seatSession, servers []xorgServer) (display, auth string) {
	for _, s := range servers {
		if sess.Display != "" && s.Display == sess.Display {
			return s.Display, s.Auth
		}
	}
	if len(servers) > 0 && sess.Type != "wayland" {
		return servers[0].Display, servers[0].Auth
	}
	if sess.Type == "x11" && sess.Display != "" {
		return sess.Display, ""
	}
	return "", ""
}

func resolveHostX() hostXTarget {
	st := readHostDesktopState()
	if st["STATE"] == "running" && st["DISPLAY"] != "" && displaySocketExists(st["DISPLAY"]) {
		t := hostXTarget{
			Display:     st["DISPLAY"],
			XAuthority:  st["XAUTHORITY"],
			SessionType: st["SESSION_TYPE"],
			Desktop:     st["DESKTOP"],
			XWayland:    st["XWAYLAND"] == "1",
		}
		if t.XAuthority != "" && !isRegularFile(t.XAuthority) {
			t.XAuthority = ""
		}
		return t
	}
	sess := activeSeatSession()
	t := hostXTarget{SessionType: sess.Type, Desktop: sess.Desktop}
	t.Display, t.XAuthority = pickHostX(sess, listXorgServers())
	if t.Display == "" {
		return t
	}
	if t.XAuthority == "" || !isRegularFile(t.XAuthority) {
		t.XAuthority = guessXAuth(t.Display, sess.UID, sess.User)
	}
	return t
}

// xEnv is the environment every X client (xrandr/xset/xdotool/
// nvidia-settings) run against the host display gets.
func xEnv(t hostXTarget) []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "DISPLAY=") || strings.HasPrefix(e, "XAUTHORITY=") {
			continue
		}
		env = append(env, e)
	}
	env = append(env, "DISPLAY="+t.Display)
	if t.XAuthority != "" {
		env = append(env, "XAUTHORITY="+t.XAuthority)
	}
	return env
}

func xCommand(t hostXTarget, name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.Env = xEnv(t)
	return cmd
}

var (
	xrandrCurrentRe   = regexp.MustCompile(`current (\d+) x (\d+)`)
	xrandrConnectedRe = regexp.MustCompile(`^(\S+) connected( primary)?`)
	xrandrModeRe      = regexp.MustCompile(`^\s+(\d+)x(\d+)i?\s`)
)

type xrandrState struct {
	Width, Height int
	Output        string
	Modes         []DisplayResolution
}

// parseXrandr reads `xrandr --query` output: the screen's current size,
// the primary (else first) connected output, and that output's modes.
func parseXrandr(out string) xrandrState {
	var st xrandrState
	if m := xrandrCurrentRe.FindStringSubmatch(out); len(m) == 3 {
		st.Width, _ = strconv.Atoi(m[1])
		st.Height, _ = strconv.Atoi(m[2])
	}
	type outputModes struct {
		name    string
		primary bool
		modes   []DisplayResolution
	}
	var outputs []*outputModes
	var cur *outputModes
	for _, line := range strings.Split(out, "\n") {
		if m := xrandrConnectedRe.FindStringSubmatch(line); m != nil {
			cur = &outputModes{name: m[1], primary: m[2] != ""}
			outputs = append(outputs, cur)
			continue
		}
		if line != "" && line[0] != ' ' && line[0] != '\t' {
			cur = nil // "disconnected" output or "Screen" line
			continue
		}
		if cur == nil {
			continue
		}
		if m := xrandrModeRe.FindStringSubmatch(line); m != nil {
			w, _ := strconv.Atoi(m[1])
			h, _ := strconv.Atoi(m[2])
			dup := false
			for _, r := range cur.modes {
				if r.Width == w && r.Height == h {
					dup = true
					break
				}
			}
			if !dup {
				cur.modes = append(cur.modes, DisplayResolution{Width: w, Height: h, Label: resolutionLabel(w, h)})
			}
		}
	}
	var chosen *outputModes
	for _, o := range outputs {
		if o.primary {
			chosen = o
			break
		}
	}
	if chosen == nil && len(outputs) > 0 {
		chosen = outputs[0]
	}
	if chosen != nil {
		st.Output = chosen.name
		st.Modes = chosen.modes
	}
	return st
}

func resolutionLabel(w, h int) string {
	for _, r := range standardResolutions {
		if r.Width == w && r.Height == h {
			return r.Label
		}
	}
	return fmt.Sprintf("%d x %d", w, h)
}

func queryXrandr(t hostXTarget) (xrandrState, error) {
	out, err := xCommand(t, "xrandr", "--query").Output()
	if err != nil {
		return xrandrState{}, fmt.Errorf("xrandr on %s: %w", t.Display, err)
	}
	return parseXrandr(string(out)), nil
}

func GetHostDisplay() (HostDisplayInfo, error) {
	t := resolveHostX()
	if t.Display == "" || t.XWayland {
		return HostDisplayInfo{}, errNoHostDisplay
	}
	st, err := queryXrandr(t)
	if err != nil {
		return HostDisplayInfo{}, err
	}
	info := HostDisplayInfo{
		Current:     fmt.Sprintf("%dx%d", st.Width, st.Height),
		Width:       st.Width,
		Height:      st.Height,
		Display:     t.Display,
		Output:      st.Output,
		Headless:    st.Output == "",
		Resolutions: st.Modes,
	}
	if len(info.Resolutions) == 0 {
		info.Resolutions = standardResolutions
	}
	return info, nil
}

func SetHostDisplay(w, h int) error {
	if w < 640 || h < 480 || w > 7680 || h > 4320 {
		return fmt.Errorf("resolution out of supported range (640x480 - 7680x4320)")
	}
	t := resolveHostX()
	if t.Display == "" || t.XWayland {
		return errNoHostDisplay
	}

	// The NVIDIA proprietary driver doesn't support RandR's dynamic mode
	// creation (--newmode/--addmode fail with BadName/RRCreateMode);
	// nvidia-settings' CurrentMetaMode is its own live way to do it.
	if _, err := exec.LookPath("nvidia-settings"); err == nil {
		if err := setHostDisplayNvidia(w, h, t); err == nil {
			return nil
		}
		// Fall through - e.g. nouveau with nvidia-settings installed.
	}

	return setHostDisplayXrandr(w, h, t)
}

// setHostDisplayNvidia asks the NVIDIA driver itself to switch to an
// arbitrary resolution via a ViewPortIn/ViewPortOut metamode - a live,
// no-restart change.
func setHostDisplayNvidia(w, h int, t hostXTarget) error {
	dpy, err := getNvidiaDisplayName(t)
	if err != nil {
		return err
	}
	resStr := fmt.Sprintf("%dx%d", w, h)
	metaMode := fmt.Sprintf("%s: nvidia-auto-select @%s +0+0 {ViewPortIn=%s, ViewPortOut=%s+0+0}", dpy, resStr, resStr, resStr)
	out, err := xCommand(t, "nvidia-settings", "--assign", "CurrentMetaMode="+metaMode).CombinedOutput()
	if err != nil || strings.Contains(string(out), "ERROR:") {
		return fmt.Errorf("nvidia-settings: %s (%v)", string(out), err)
	}
	return nil
}

// getNvidiaDisplayName finds NVIDIA's own display identifier (e.g. "DPY-0")
// from its current metamode.
func getNvidiaDisplayName(t hostXTarget) (string, error) {
	out, err := xCommand(t, "nvidia-settings", "-q", "CurrentMetaMode", "-t").Output()
	if err != nil {
		return "", fmt.Errorf("nvidia-settings -q CurrentMetaMode: %w", err)
	}
	// "id=..., switchable=..., source=... :: DPY-0: nvidia-auto-select ..."
	re := regexp.MustCompile(`::\s*(\S+):`)
	match := re.FindStringSubmatch(string(out))
	if len(match) != 2 {
		return "", fmt.Errorf("could not parse nvidia-settings display name from: %s", string(out))
	}
	return match[1], nil
}

// setHostDisplayXrandr: a headless X server (no connected output) can only
// change its framebuffer size, so that goes straight to --fb. With an
// output: an existing mode is selected directly; otherwise a CVT mode is
// created and added (works on modesetting/amdgpu/intel); only if that
// fails too does it fall back to a framebuffer-only resize.
func setHostDisplayXrandr(w, h int, t hostXTarget) error {
	resStr := fmt.Sprintf("%dx%d", w, h)
	st, err := queryXrandr(t)
	if err != nil {
		return err
	}
	fb := func() error {
		if out, err := xCommand(t, "xrandr", "--fb", resStr).CombinedOutput(); err != nil {
			return fmt.Errorf("xrandr --fb %s: %s (%w)", resStr, strings.TrimSpace(string(out)), err)
		}
		return nil
	}
	if st.Output == "" {
		return fb()
	}
	out, err := xCommand(t, "xrandr", "--output", st.Output, "--mode", resStr).CombinedOutput()
	if err == nil {
		return nil
	}
	if !strings.Contains(string(out), "cannot find mode") {
		return fmt.Errorf("xrandr: %s (%w)", strings.TrimSpace(string(out)), err)
	}
	if name, modeline, ok := cvtModeline(w, h); ok {
		_ = xCommand(t, "xrandr", append([]string{"--newmode", name}, modeline...)...).Run()
		if xCommand(t, "xrandr", "--addmode", st.Output, name).Run() == nil &&
			xCommand(t, "xrandr", "--output", st.Output, "--mode", name).Run() == nil {
			return nil
		}
	}
	return fb()
}

var cvtModelineRe = regexp.MustCompile(`(?m)^Modeline\s+"([^"]+)"\s+(.+)$`)

// cvtModeline runs `cvt W H` and returns the mode name and timing fields.
func cvtModeline(w, h int) (string, []string, bool) {
	out, err := exec.Command("cvt", strconv.Itoa(w), strconv.Itoa(h)).Output()
	if err != nil {
		return "", nil, false
	}
	m := cvtModelineRe.FindStringSubmatch(string(out))
	if m == nil {
		return "", nil, false
	}
	return m[1], strings.Fields(m[2]), true
}

func RegisterHostRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /host/capabilities", func(w http.ResponseWriter, r *http.Request) {
		caps, err := GetHostCapabilities()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, caps)
	})

	mux.HandleFunc("GET /host/display", func(w http.ResponseWriter, r *http.Request) {
		disp, err := GetHostDisplay()
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, errNoHostDisplay) {
				status = http.StatusServiceUnavailable
			}
			writeError(w, status, err)
			return
		}
		writeJSON(w, http.StatusOK, disp)
	})

	mux.HandleFunc("POST /host/display", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Width      int    `json:"width"`
			Height     int    `json:"height"`
			Resolution string `json:"resolution"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, fmt.Errorf("invalid json request: %w", err))
			return
		}
		if req.Resolution != "" && (req.Width == 0 || req.Height == 0) {
			parts := strings.Split(req.Resolution, "x")
			if len(parts) == 2 {
				req.Width, _ = strconv.Atoi(strings.TrimSpace(parts[0]))
				req.Height, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
			}
		}
		if req.Width == 0 || req.Height == 0 {
			writeError(w, http.StatusBadRequest, fmt.Errorf("width and height or resolution (e.g. 1920x1080) required"))
			return
		}
		if err := SetHostDisplay(req.Width, req.Height); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, errNoHostDisplay) {
				status = http.StatusServiceUnavailable
			}
			writeError(w, status, err)
			return
		}
		disp, err := GetHostDisplay()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, disp)
	})
}
