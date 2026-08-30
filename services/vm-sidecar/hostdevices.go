// hostdevices.go enumerates host USB and PCI devices for the VM
// hardware-passthrough picker (Create/Edit VM's Hardware section) - pure
// host inspection, no libvirt or VM state involved.
package main

import (
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
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
// IOMMUEnabled gates whether PCI passthrough can work at all on this
// host; USB passthrough has no such prerequisite.
type HostCapabilities struct {
	IOMMUEnabled bool            `json:"iommu_enabled"`
	USBDevices   []HostUSBDevice `json:"usb_devices"`
	PCIDevices   []HostPCIDevice `json:"pci_devices"`
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
	usb, err := listHostUSBDevices()
	if err != nil {
		return HostCapabilities{}, err
	}
	pci, err := listHostPCIDevices()
	if err != nil {
		return HostCapabilities{}, err
	}
	return HostCapabilities{
		IOMMUEnabled: iommuEnabled(),
		USBDevices:   usb,
		PCIDevices:   pci,
	}, nil
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
}
