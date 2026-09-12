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
	{Width: 1600, Height: 900, Label: "1600 x 900 (HD+)"},
	{Width: 1440, Height: 900, Label: "1440 x 900 (WXGA+)"},
	{Width: 1366, Height: 768, Label: "1366 x 768 (Standard Laptop)"},
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

func SetHostDisplay(w, h int) error {
	if w < 640 || h < 480 || w > 7680 || h > 4320 {
		return fmt.Errorf("resolution out of supported range (640x480 - 7680x4320)")
	}
	resStr := fmt.Sprintf("%dx%d", w, h)
	cmd := exec.Command("xrandr", "-display", ":0", "--fb", resStr)
	if auth := getHostXAuth(); auth != "" {
		cmd.Env = append(os.Environ(), "DISPLAY=:0", "XAUTHORITY="+auth)
	} else {
		cmd.Env = append(os.Environ(), "DISPLAY=:0")
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("xrandr: %s (%w)", string(out), err)
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

