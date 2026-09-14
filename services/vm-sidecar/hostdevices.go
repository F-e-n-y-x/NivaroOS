// hostdevices.go enumerates host USB and PCI devices for the VM
// hardware-passthrough picker (Create/Edit VM's Hardware section) - pure
// host inspection, no libvirt or VM state involved.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
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
	Current     string              `json:"current"`
	Width       int                 `json:"width"`
	Height      int                 `json:"height"`
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

func getHostXAuth() string {
	candidates := []string{
		"/var/run/lightdm/root/:0",
		"/run/lightdm/root/:0",
		"/root/.Xauthority",
	}
	for _, f := range candidates {
		if _, err := os.Stat(f); err == nil {
			return f
		}
	}
	return ""
}

func GetHostDisplay() (HostDisplayInfo, error) {
	cmd := exec.Command("xrandr", "-display", ":0")
	if auth := getHostXAuth(); auth != "" {
		cmd.Env = append(os.Environ(), "DISPLAY=:0", "XAUTHORITY="+auth)
	} else {
		cmd.Env = append(os.Environ(), "DISPLAY=:0")
	}
	out, err := cmd.Output()
	curWidth := 1920
	curHeight := 1080
	if err == nil {
		re := regexp.MustCompile(`current (\d+) x (\d+)`)
		match := re.FindStringSubmatch(string(out))
		if len(match) == 3 {
			curWidth, _ = strconv.Atoi(match[1])
			curHeight, _ = strconv.Atoi(match[2])
		}
	}
	return HostDisplayInfo{
		Current:     fmt.Sprintf("%dx%d", curWidth, curHeight),
		Width:       curWidth,
		Height:      curHeight,
		Resolutions: standardResolutions,
	}, nil
}

// xrandrEnv builds the environment every xrandr/cvt invocation against the
// host's display needs - DRYs up what used to be four copy-pasted if/else
// blocks across GetHostDisplay/SetHostDisplay.
func xrandrEnv(auth string) []string {
	if auth != "" {
		return append(os.Environ(), "DISPLAY=:0", "XAUTHORITY="+auth)
	}
	return append(os.Environ(), "DISPLAY=:0")
}

// getConnectedOutput finds the actual connected display output's name (e.g.
// "HDMI-0", "DP-1", "eDP-1") - this varies by GPU/driver and cabling, so it
// can never be hardcoded the way a previous version of this function
// assumed ("HDMI-0"), which silently failed on any other output name and
// fell back to a bare framebuffer resize that doesn't actually change the
// monitor's own output mode.
func getConnectedOutput(auth string) (string, error) {
	cmd := exec.Command("xrandr", "-display", ":0")
	cmd.Env = xrandrEnv(auth)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("xrandr: %w", err)
	}
	// Prefer the primary connected output if xrandr reports one, otherwise
	// the first connected output found.
	re := regexp.MustCompile(`(?m)^(\S+) connected primary`)
	if match := re.FindStringSubmatch(string(out)); len(match) == 2 {
		return match[1], nil
	}
	re = regexp.MustCompile(`(?m)^(\S+) connected`)
	if match := re.FindStringSubmatch(string(out)); len(match) == 2 {
		return match[1], nil
	}
	return "", fmt.Errorf("no connected display output found")
}

func SetHostDisplay(w, h int) error {
	if w < 640 || h < 480 || w > 7680 || h > 4320 {
		return fmt.Errorf("resolution out of supported range (640x480 - 7680x4320)")
	}
	auth := getHostXAuth()

	// The NVIDIA proprietary driver (confirmed via Xorg.0.log on real
	// deployments of this) does not support RandR's dynamic mode creation
	// (xrandr --newmode/--addmode fail with a BadName/RRCreateMode X error,
	// verified directly against a running instance) - nvidia-settings'
	// CurrentMetaMode is NVIDIA's own, driver-correct way to set an
	// arbitrary resolution live, and is what actually works here.
	if _, err := exec.LookPath("nvidia-settings"); err == nil {
		if err := setHostDisplayNvidia(w, h, auth); err == nil {
			return nil
		}
		// Fall through to the generic xrandr path below - e.g. an NVIDIA
		// card running the open-source nouveau driver instead, where
		// nvidia-settings is installed but CurrentMetaMode isn't a valid
		// NV-CONTROL attribute.
	}

	return setHostDisplayXrandr(w, h, auth)
}

// setHostDisplayNvidia asks the NVIDIA driver itself to switch to an
// arbitrary resolution via a ViewPortIn/ViewPortOut metamode - this is a
// live, no-restart resolution change, unlike editing xorg.conf.
func setHostDisplayNvidia(w, h int, auth string) error {
	dpy, err := getNvidiaDisplayName(auth)
	if err != nil {
		return err
	}
	resStr := fmt.Sprintf("%dx%d", w, h)
	metaMode := fmt.Sprintf("%s: nvidia-auto-select @%s +0+0 {ViewPortIn=%s, ViewPortOut=%s+0+0}", dpy, resStr, resStr, resStr)
	cmd := exec.Command("nvidia-settings", "--assign", "CurrentMetaMode="+metaMode)
	cmd.Env = xrandrEnv(auth)
	if out, err := cmd.CombinedOutput(); err != nil || strings.Contains(string(out), "ERROR:") {
		return fmt.Errorf("nvidia-settings: %s (%w)", string(out), err)
	}
	return nil
}

// getNvidiaDisplayName finds NVIDIA's own display identifier (e.g. "DPY-0")
// from its current metamode - this varies by GPU/connector, so (matching
// getConnectedOutput's reasoning for the plain-xrandr path) it can't be
// hardcoded either.
func getNvidiaDisplayName(auth string) (string, error) {
	cmd := exec.Command("nvidia-settings", "-q", "CurrentMetaMode", "-t")
	cmd.Env = xrandrEnv(auth)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("nvidia-settings -q CurrentMetaMode: %w", err)
	}
	// -t's terse output is "id=..., switchable=..., source=... :: DPY-0:
	// nvidia-auto-select @... +0+0 {...}" - anchor on the "::" separator
	// before the display name, or a naive `(\S+):` matches the "::" itself.
	re := regexp.MustCompile(`::\s*(\S+):`)
	match := re.FindStringSubmatch(string(out))
	if len(match) != 2 {
		return "", fmt.Errorf("could not parse nvidia-settings display name from: %s", string(out))
	}
	return match[1], nil
}

// setHostDisplayXrandr is the plain-RandR path for non-NVIDIA drivers
// (Intel/AMD's open-source drivers support --newmode/--addmode properly,
// unlike NVIDIA's proprietary one). It only handles resolutions that are
// already a known mode for the connected output (standard/EDID-detected
// resolutions almost always are) - genuinely custom resolutions fall back
// to a framebuffer-only resize, which changes the VNC-visible canvas size
// without necessarily matching the physical output's own mode.
func setHostDisplayXrandr(w, h int, auth string) error {
	resStr := fmt.Sprintf("%dx%d", w, h)

	output, err := getConnectedOutput(auth)
	if err != nil {
		return err
	}

	cmdMode := exec.Command("xrandr", "-display", ":0", "--output", output, "--mode", resStr)
	cmdMode.Env = xrandrEnv(auth)
	if out, err := cmdMode.CombinedOutput(); err == nil {
		return nil
	} else if !strings.Contains(string(out), "cannot find mode") {
		return fmt.Errorf("xrandr: %s (%w)", string(out), err)
	}

	cmdFb := exec.Command("xrandr", "-display", ":0", "--fb", resStr)
	cmdFb.Env = xrandrEnv(auth)
	if out, err := cmdFb.CombinedOutput(); err != nil {
		return fmt.Errorf("xrandr --fb: %s (%w)", string(out), err)
	}
	return nil
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
			writeError(w, http.StatusInternalServerError, err)
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
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		disp, _ := GetHostDisplay()
		writeJSON(w, http.StatusOK, disp)
	})
}

