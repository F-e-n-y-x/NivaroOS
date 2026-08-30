package main

import "testing"

// sampleLsusbOutput is captured verbatim from this host's own `lsusb`.
const sampleLsusbOutput = `Bus 001 Device 001: ID 1d6b:0002 Linux Foundation 2.0 root hub
Bus 001 Device 002: ID 30fa:0300  USB Optical Mouse
Bus 001 Device 003: ID 1a2c:212a China Resource Semico Co., Ltd USB Keyboard
Bus 002 Device 001: ID 1d6b:0003 Linux Foundation 3.0 root hub
`

func TestParseLsusbOutput_FiltersRootHubsAndParsesRealDevices(t *testing.T) {
	devices := parseLsusbOutput(sampleLsusbOutput)
	if len(devices) != 2 {
		t.Fatalf("expected 2 real devices (root hubs filtered out), got %+v", devices)
	}
	if devices[0].VendorID != "30fa" || devices[0].ProductID != "0300" || devices[0].Description != "USB Optical Mouse" {
		t.Errorf("unexpected first device: %+v", devices[0])
	}
	if devices[1].VendorID != "1a2c" || devices[1].ProductID != "212a" {
		t.Errorf("unexpected second device: %+v", devices[1])
	}
}

func TestParseLsusbOutput_EmptyInput(t *testing.T) {
	devices := parseLsusbOutput("")
	if len(devices) != 0 {
		t.Errorf("expected no devices for empty input, got %+v", devices)
	}
}

// sampleLspciOutput is captured verbatim from this host's own `lspci -D`.
const sampleLspciOutput = `0000:00:00.0 Host bridge: Advanced Micro Devices, Inc. [AMD] Starship/Matisse Root Complex
0000:01:00.0 USB controller: Advanced Micro Devices, Inc. [AMD] 400 Series Chipset USB 3.1 xHCI Compliant Host Controller (rev 01)
0000:01:00.1 SATA controller: Advanced Micro Devices, Inc. [AMD] 400 Series Chipset SATA Controller (rev 01)
`

func TestParseLspciOutput_ParsesAddressAndDescription(t *testing.T) {
	devices := parseLspciOutput(sampleLspciOutput)
	if len(devices) != 3 {
		t.Fatalf("expected 3 devices, got %+v", devices)
	}
	if devices[1].Address != "0000:01:00.0" {
		t.Errorf("expected domain-prefixed address, got %q", devices[1].Address)
	}
	if devices[1].Description != "USB controller: Advanced Micro Devices, Inc. [AMD] 400 Series Chipset USB 3.1 xHCI Compliant Host Controller (rev 01)" {
		t.Errorf("unexpected description: %q", devices[1].Description)
	}
	// Every parsed address must itself be acceptable to parsePCIAddress -
	// the whole point of requiring `-D` output is that it always is.
	for _, d := range devices {
		if _, err := parsePCIAddress(d.Address); err != nil {
			t.Errorf("address %q from lspci -D output failed parsePCIAddress: %v", d.Address, err)
		}
	}
}

func TestParseLspciOutput_EmptyInput(t *testing.T) {
	devices := parseLspciOutput("")
	if len(devices) != 0 {
		t.Errorf("expected no devices for empty input, got %+v", devices)
	}
}
