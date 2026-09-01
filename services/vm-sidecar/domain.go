// domain.go wraps the libvirt connection and all VM read/lifecycle
// operations. Every access to libvirt goes through LibvirtStore so tests
// can point it at the "test:///default" fake driver instead of a real
// hypervisor.
package main

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"text/template"

	libvirt "libvirt.org/go/libvirt"
)

// undefineDomain tries UndefineFlags first (real libvirtd needs
// KEEP_NVRAM/NVRAM to correctly handle a UEFI domain's NVRAM file on
// redefinition/deletion - confirmed directly against it, not assumed),
// falling back to plain Undefine() only if the driver itself doesn't
// support flags at all (e.g. the test:///default fake driver used in
// tests) - never on any other error, which should still surface.
func undefineDomain(dom *libvirt.Domain, flags libvirt.DomainUndefineFlagsValues) error {
	err := dom.UndefineFlags(flags)
	var verr libvirt.Error
	// Real libvirtd reports an unrecognized flag as ERR_NO_SUPPORT; the
	// test:///default fake driver used in tests reports the exact same
	// situation as ERR_INVALID_ARG instead (confirmed directly - its
	// message is literally "unsupported flags ..."). Both mean the same
	// thing here: this driver doesn't understand the flag, fall back.
	if errors.As(err, &verr) && (verr.Code == libvirt.ERR_NO_SUPPORT || verr.Code == libvirt.ERR_INVALID_ARG) {
		return dom.Undefine()
	}
	return err
}

// LibvirtStore lazily connects to libvirt so the sidecar can start (and
// serve /setup/status) before libvirtd is even installed, and recover
// automatically once it becomes reachable - no restart required. This
// also means each *LibvirtStore keeps one connection alive across calls,
// which the "test:///default" fake driver requires: every fresh
// NewConnect("test:///default") starts an independent, empty hypervisor,
// so a persistent connection is what makes domains created in one call
// visible to the next.
type LibvirtStore struct {
	uri string

	mu   sync.Mutex
	conn *libvirt.Connect
}

func NewLibvirtStore(uri string) *LibvirtStore {
	return &LibvirtStore{uri: uri}
}

func (s *LibvirtStore) getConn() (*libvirt.Connect, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		if alive, err := s.conn.IsAlive(); err == nil && alive {
			return s.conn, nil
		}
		s.conn.Close()
		s.conn = nil
	}
	conn, err := libvirt.NewConnect(s.uri)
	if err != nil {
		return nil, err
	}
	s.conn = conn
	return conn, nil
}

func (s *LibvirtStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn == nil {
		return nil
	}
	_, err := s.conn.Close()
	s.conn = nil
	return err
}

// VM is the JSON shape returned by GET /vms and GET /vms/{name}.
type VM struct {
	Name      string `json:"name"`
	State     string `json:"state"`
	VCPUs     uint   `json:"vcpus"`
	MemoryMiB uint64 `json:"memory_mib"`
	// DiskPath/DiskGiB/NetworkMode mirror Disks[0]/Networks[0] - kept as a
	// convenience for callers that only care about "the" disk/network
	// (the VM card summary, the disk-grow field) so they don't all need
	// rewriting for multi-disk/multi-NIC support. The full picture is in
	// Disks/Networks below.
	DiskPath    string     `json:"disk_path,omitempty"`
	DiskGiB     uint64     `json:"disk_gib,omitempty"`
	NetworkMode string     `json:"network_mode,omitempty"`
	Disks       []DiskInfo `json:"disks"`
	Networks    []NICInfo  `json:"networks"`
	ISOPath     string     `json:"iso_path,omitempty"`
	USBDevices  []USBDeviceSpec `json:"usb_devices,omitempty"`
	PCIDevices  []PCIDeviceSpec `json:"pci_devices,omitempty"`
	BootOrder   []string        `json:"boot_order,omitempty"`
	// "bios" (SeaBIOS, the default) or "uefi" (OVMF) - detected from
	// whether the domain's XML has a <loader> element, since that's the
	// one thing distinguishing the two at the libvirt level.
	Firmware string `json:"firmware"`
	// Zero when no resolution hint is set - the guest picked its own
	// default display mode.
	DisplayWidth  uint `json:"display_width,omitempty"`
	DisplayHeight uint `json:"display_height,omitempty"`
}

// DiskSpec is a data disk as given in a create/update request. Path is
// required when attaching an existing image; CreateVM auto-generates one
// (alongside the VM's own storage) when empty and provisions a fresh
// qcow2 of Path/GiB itself.
type DiskSpec struct {
	Path string `json:"path,omitempty"`
	GiB  uint64 `json:"gib"`
	// "virtio" (default - fastest, needs guest drivers, universally
	// available in modern Linux and recent Windows), "sata" (universal
	// compatibility, no special drivers needed), or "ide" (legacy, for
	// very old guests only).
	Bus string `json:"bus,omitempty"`
	// SSD marks the virtual disk as flash-backed: enables discard/TRIM
	// passthrough (driver discard='unmap') so the guest can tell the host
	// to reclaim freed space, and is surfaced in the UI as an SSD icon
	// rather than a spinning disk - the same distinction Unraid/VirtualBox
	// draw between a disk's declared type and its raw size.
	SSD bool `json:"ssd,omitempty"`
}

// DiskInfo is a data disk as reported back by GET /vms - Target is the
// libvirt-assigned device name (vda, sdb, ...), computed at create/update
// time from Bus and the disk's position among others sharing that bus.
type DiskInfo struct {
	Path   string `json:"path"`
	GiB    uint64 `json:"gib"`
	Bus    string `json:"bus"`
	Target string `json:"target"`
	SSD    bool   `json:"ssd,omitempty"`
}

// NICSpec is a network adapter as given in a create/update request.
type NICSpec struct {
	Mode       string `json:"mode"` // "nat" or "bridge"
	BridgeName string `json:"bridge_name,omitempty"`
	// "virtio" (default) or a slower emulated NIC ("e1000", "rtl8139")
	// for guests too old to have virtio drivers.
	Model     string `json:"model,omitempty"`
	MAC       string `json:"mac,omitempty"`
	LinkState string `json:"link_state,omitempty"` // "up" or "down"
}

// NICInfo is a network adapter as reported back by GET /vms.
type NICInfo struct {
	Mode       string `json:"mode"`
	BridgeName string `json:"bridge_name,omitempty"`
	Model      string `json:"model"`
	MAC        string `json:"mac,omitempty"`
	LinkState  string `json:"link_state,omitempty"` // "up" or "down"
}

// USBDeviceSpec identifies a host USB device to pass through by its
// vendor:product ID pair (from lsusb) - the same identifier VirtualBox's
// USB device filters and Unraid's USB passthrough picker both use. This
// binds by device *identity*, not physical port, so it survives the
// device being unplugged and replugged (or moved to another port).
type USBDeviceSpec struct {
	VendorID  string `json:"vendor_id"`
	ProductID string `json:"product_id"`
}

// PCIDeviceSpec identifies a host PCI device to pass through by its BDF
// address (e.g. "0000:01:00.0", from lspci) - requires IOMMU (VT-d/AMD-Vi)
// enabled on the host; CreateVM/UpdateVM refuse this otherwise rather than
// defining a domain that can never actually start.
type PCIDeviceSpec struct {
	Address string `json:"address"`
}

// iommuEnabled reports whether the host has IOMMU groups set up
// (amd_iommu=on/intel_iommu=on on the kernel command line, or default-on
// for some newer kernels/firmware) - PCI passthrough is physically
// impossible without this, regardless of anything libvirt or QEMU does.
func iommuEnabled() bool {
	entries, err := os.ReadDir("/sys/kernel/iommu_groups")
	return err == nil && len(entries) > 0
}

func domainStateString(state libvirt.DomainState) string {
	switch state {
	case libvirt.DOMAIN_RUNNING:
		return "running"
	case libvirt.DOMAIN_SHUTOFF:
		return "shutoff"
	case libvirt.DOMAIN_PAUSED:
		return "paused"
	case libvirt.DOMAIN_CRASHED:
		return "crashed"
	case libvirt.DOMAIN_PMSUSPENDED:
		return "suspended"
	default:
		return "unknown"
	}
}

// domainXML mirrors only the fields this sidecar reads out of a domain's
// XML description (disks, ISO, networks, hostdevs, firmware) - not a full
// libvirt schema.
type domainXML struct {
	OS struct {
		Loader string `xml:"loader"`
	} `xml:"os"`
	Devices struct {
		Disks []struct {
			Device string `xml:"device,attr"`
			Driver struct {
				Discard string `xml:"discard,attr"`
			} `xml:"driver"`
			Source struct {
				File string `xml:"file,attr"`
			} `xml:"source"`
			Target struct {
				Dev string `xml:"dev,attr"`
				Bus string `xml:"bus,attr"`
			} `xml:"target"`
		} `xml:"disk"`
		Interfaces []struct {
			Type   string `xml:"type,attr"`
			Source struct {
				Network string `xml:"network,attr"`
				Bridge  string `xml:"bridge,attr"`
			} `xml:"source"`
			Model struct {
				Type string `xml:"type,attr"`
			} `xml:"model"`
			MAC struct {
				Address string `xml:"address,attr"`
			} `xml:"mac"`
			Link struct {
				State string `xml:"state,attr"`
			} `xml:"link"`
		} `xml:"interface"`
		Hostdevs []struct {
			Type   string `xml:"type,attr"`
			Source struct {
				Vendor struct {
					ID string `xml:"id,attr"`
				} `xml:"vendor"`
				Product struct {
					ID string `xml:"id,attr"`
				} `xml:"product"`
				Address struct {
					Domain   string `xml:"domain,attr"`
					Bus      string `xml:"bus,attr"`
					Slot     string `xml:"slot,attr"`
					Function string `xml:"function,attr"`
				} `xml:"address"`
			} `xml:"source"`
		} `xml:"hostdev"`
		Videos []struct {
			Model struct {
				Type       string `xml:"type,attr"`
				Resolution struct {
					X uint `xml:"x,attr"`
					Y uint `xml:"y,attr"`
				} `xml:"resolution"`
			} `xml:"model"`
		} `xml:"video"`
	} `xml:"devices"`
}

func toVM(dom *libvirt.Domain) (VM, error) {
	name, err := dom.GetName()
	if err != nil {
		return VM{}, err
	}
	info, err := dom.GetInfo()
	if err != nil {
		return VM{}, err
	}
	xmlDesc, err := dom.GetXMLDesc(0)
	if err != nil {
		return VM{}, err
	}
	var parsed domainXML
	if err := xml.Unmarshal([]byte(xmlDesc), &parsed); err != nil {
		return VM{}, err
	}
	vm := VM{
		Name:      name,
		State:     domainStateString(info.State),
		VCPUs:     info.NrVirtCpu,
		MemoryMiB: info.Memory / 1024,
		Firmware:  "bios",
	}
	if parsed.OS.Loader != "" {
		vm.Firmware = "uefi"
	}
	for _, v := range parsed.Devices.Videos {
		if v.Model.Resolution.X != 0 && v.Model.Resolution.Y != 0 {
			vm.DisplayWidth = v.Model.Resolution.X
			vm.DisplayHeight = v.Model.Resolution.Y
			break
		}
	}
	for _, d := range parsed.Devices.Disks {
		if d.Device == "cdrom" && d.Source.File != "" {
			vm.ISOPath = d.Source.File
			continue
		}
		if d.Device != "disk" || d.Source.File == "" {
			continue
		}
		info := DiskInfo{Path: d.Source.File, Bus: d.Target.Bus, Target: d.Target.Dev, SSD: d.Driver.Discard == "unmap"}
		if size, err := qemuImgVirtualSizeGiB(info.Path); err == nil {
			info.GiB = size
		}
		vm.Disks = append(vm.Disks, info)
	}
	if len(vm.Disks) > 0 {
		vm.DiskPath = vm.Disks[0].Path
		vm.DiskGiB = vm.Disks[0].GiB
	}
	for _, i := range parsed.Devices.Interfaces {
		linkState := i.Link.State
		if linkState == "" {
			linkState = "up"
		}
		info := NICInfo{Model: i.Model.Type, MAC: i.MAC.Address, LinkState: linkState}
		if i.Type == "bridge" && i.Source.Bridge != "" {
			info.Mode = "bridge"
			info.BridgeName = i.Source.Bridge
		} else if i.Source.Network != "" {
			info.Mode = "nat"
		} else {
			continue
		}
		vm.Networks = append(vm.Networks, info)
	}
	if len(vm.Networks) > 0 {
		if vm.Networks[0].Mode == "bridge" {
			vm.NetworkMode = "bridge:" + vm.Networks[0].BridgeName
		} else {
			vm.NetworkMode = "nat:default"
		}
	}
	for _, h := range parsed.Devices.Hostdevs {
		switch h.Type {
		case "usb":
			vm.USBDevices = append(vm.USBDevices, USBDeviceSpec{VendorID: h.Source.Vendor.ID, ProductID: h.Source.Product.ID})
		case "pci":
			a := h.Source.Address
			vm.PCIDevices = append(vm.PCIDevices, PCIDeviceSpec{
				Address: fmt.Sprintf("%s:%s:%s.%s",
					strings.TrimPrefix(a.Domain, "0x"), strings.TrimPrefix(a.Bus, "0x"),
					strings.TrimPrefix(a.Slot, "0x"), strings.TrimPrefix(a.Function, "0x")),
			})
		}
	}
	return vm, nil
}

// qemuImgVirtualSizeGiB reads a qcow2 disk's provisioned (virtual) size -
// what "20 GiB" meant at creation time, not how much space it actually
// occupies on disk (qcow2 is sparse).
func qemuImgVirtualSizeGiB(diskPath string) (uint64, error) {
	// --force-share: a running VM holds an exclusive lock on its own disk
	// image, which plain `qemu-img info` needs and would otherwise fail
	// to get - this flag is qemu-img's own safe, read-only way to inspect
	// a disk that's currently in use without needing that lock.
	out, err := exec.Command("qemu-img", "info", "--output=json", "--force-share", diskPath).Output()
	if err != nil {
		return 0, err
	}
	var info struct {
		VirtualSize uint64 `json:"virtual-size"`
	}
	if err := json.Unmarshal(out, &info); err != nil {
		return 0, err
	}
	return info.VirtualSize / (1024 * 1024 * 1024), nil
}

func (s *LibvirtStore) ListVMs() ([]VM, error) {
	conn, err := s.getConn()
	if err != nil {
		return nil, err
	}
	doms, err := conn.ListAllDomains(0)
	if err != nil {
		return nil, err
	}
	vms := make([]VM, 0, len(doms))
	for _, dom := range doms {
		vm, err := toVM(&dom)
		dom.Free()
		if err != nil {
			return nil, err
		}
		vms = append(vms, vm)
	}
	return vms, nil
}

func (s *LibvirtStore) lookup(name string) (*libvirt.Domain, error) {
	conn, err := s.getConn()
	if err != nil {
		return nil, err
	}
	return conn.LookupDomainByName(name)
}

func (s *LibvirtStore) GetVM(name string) (VM, error) {
	dom, err := s.lookup(name)
	if err != nil {
		return VM{}, err
	}
	defer dom.Free()
	return toVM(dom)
}

// CreateVMRequest is the JSON body accepted by POST /vms.
type CreateVMRequest struct {
	Name      string `json:"name"`
	VCPUs     uint   `json:"vcpus"`
	MemoryMiB uint64 `json:"memory_mib"`
	// Disks may be empty - a VM with no data disk at all is valid (PXE
	// boot, or storage attached later via PUT), matching Unraid's "no
	// primary vdisk" checkbox and VirtualBox's "Do not add a virtual hard
	// disk" wizard option.
	Disks      []DiskSpec      `json:"disks,omitempty"`
	ISOPath    string          `json:"iso_path,omitempty"`
	Networks   []NICSpec       `json:"networks,omitempty"`
	USBDevices []USBDeviceSpec `json:"usb_devices,omitempty"`
	PCIDevices []PCIDeviceSpec `json:"pci_devices,omitempty"`
	// BootOrder lists boot targets by symbol, highest priority first:
	// a disk's target dev name (e.g. "vda"), "cdrom", or "network". Any
	// device not listed is still bootable, just after the ones named
	// here, in device-list order (BIOS/UEFI's own normal fallback
	// behavior) - this doesn't need to be exhaustive.  Empty means "use
	// the simple default" (cdrom-if-present, then first disk).
	BootOrder []string `json:"boot_order,omitempty"`
	// "bios" (default, if empty) or "uefi". Windows 11 requires uefi;
	// most Linux distros work with either.
	Firmware string `json:"firmware,omitempty"`
	// DisplayWidth/DisplayHeight set a preferred guest display
	// resolution (an EDID-like hint the guest OS reads, similar to a
	// monitor's own preferred mode) - not client-side console scaling.
	// Both empty/zero means "let the guest OS decide" (its own default).
	DisplayWidth  uint `json:"display_width,omitempty"`
	DisplayHeight uint `json:"display_height,omitempty"`
}

type NetworkChoice struct {
	Mode       string `json:"mode"` // "nat" or "bridge"
	BridgeName string `json:"bridge_name,omitempty"`
}

var vmNameRe = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// busTargetPrefix maps a disk bus to the libvirt device-name prefix
// convention for it - virtio disks are vda/vdb/..., SATA sda/sdb/...,
// IDE hda/hdb/... Each bus gets its own independent letter sequence, so
// two disks on different buses can both be "the first one" without
// colliding.
var busTargetPrefix = map[string]string{"virtio": "vd", "sata": "sd", "ide": "hd"}

// renderedDisk/renderedNIC/renderedISO/renderedHostdev carry everything
// the template needs pre-computed (target dev names, boot priorities) so
// the template itself only ranges and prints - Go's text/template has no
// mutable loop counters, so per-bus target numbering has to happen here.
type renderedDisk struct {
	Path      string
	Target    string
	Bus       string
	SSD       bool
	BootOrder int
}
type renderedNIC struct {
	Mode       string
	BridgeName string
	Model      string
	MAC        string
	LinkState  string
	BootOrder  int
}
type renderedISO struct {
	Path      string
	BootOrder int
}
type renderedUSBDevice struct{ VendorID, ProductID string }
type renderedPCIDevice struct{ Domain, Bus, Slot, Function string }

// buildDeviceRender computes disk target dev names (the ISO's implicit
// SATA cdrom is allocated first, so a SATA data disk can never collide
// with it), resolves model defaults, and applies BootOrder's symbolic
// entries as <boot order='N'/> priorities on the matching device. Pure
// and side-effect-free so it's independently testable without libvirt.
func buildDeviceRender(disks []DiskSpec, isoPath string, networks []NICSpec, bootOrder []string) ([]renderedDisk, *renderedISO, []renderedNIC, error) {
	busCounters := map[string]int{}
	// The optical drive slot always exists, ISO inserted or not - the
	// same way a real hypervisor's VM always has a CD drive that's either
	// empty or loaded, not something that only exists when media happens
	// to be in it. This matters beyond cosmetics: EjectCDROM/InsertCDROM
	// change an *existing* device via UpdateDeviceFlags and can't create
	// one from nothing, so a VM created without an ISO would otherwise
	// never be able to get one inserted later (found exactly this way,
	// via a real UpdateDeviceFlags "target sda doesn't exist" error).
	iso := &renderedISO{Path: isoPath}
	busCounters["sata"] = 1 // reserve sda for the cdrom

	rendered := make([]renderedDisk, len(disks))
	targets := make([]string, len(disks))
	for i, d := range disks {
		bus := d.Bus
		if bus == "" {
			bus = "virtio"
		}
		prefix, ok := busTargetPrefix[bus]
		if !ok {
			return nil, nil, nil, fmt.Errorf("disk %d: unsupported bus %q (use virtio, sata, or ide)", i, bus)
		}
		n := busCounters[bus]
		busCounters[bus] = n + 1
		if n > 25 {
			return nil, nil, nil, fmt.Errorf("disk %d: too many disks on bus %q", i, bus)
		}
		target := prefix + string(rune('a'+n))
		targets[i] = target
		rendered[i] = renderedDisk{Path: d.Path, Target: target, Bus: bus, SSD: d.SSD}
	}

	renderedNets := make([]renderedNIC, len(networks))
	for i, n := range networks {
		model := n.Model
		if model == "" {
			model = "virtio"
		}
		renderedNets[i] = renderedNIC{Mode: n.Mode, BridgeName: n.BridgeName, Model: model, MAC: n.MAC, LinkState: n.LinkState}
	}

	for pos, sym := range bootOrder {
		priority := pos + 1
		switch sym {
		case "cdrom":
			if iso != nil {
				iso.BootOrder = priority
			}
		case "network":
			for i := range renderedNets {
				if renderedNets[i].BootOrder == 0 {
					renderedNets[i].BootOrder = priority
					break
				}
			}
		default:
			for i, t := range targets {
				if t == sym {
					rendered[i].BootOrder = priority
				}
			}
		}
	}

	return rendered, iso, renderedNets, nil
}

var pciAddressRe = regexp.MustCompile(`^([0-9a-fA-F]{4}):([0-9a-fA-F]{2}):([0-9a-fA-F]{2})\.([0-9a-fA-F])$`)

// parsePCIAddress splits a lspci-style BDF address ("0000:01:00.0") into
// the domain/bus/slot/function components libvirt's <address> element
// needs individually.
func parsePCIAddress(addr string) (renderedPCIDevice, error) {
	m := pciAddressRe.FindStringSubmatch(addr)
	if m == nil {
		return renderedPCIDevice{}, fmt.Errorf("invalid PCI address %q: expected format dddd:bb:ss.f (e.g. 0000:01:00.0)", addr)
	}
	return renderedPCIDevice{Domain: m[1], Bus: m[2], Slot: m[3], Function: m[4]}, nil
}

var hexIDRe = regexp.MustCompile(`^(0x)?[0-9a-fA-F]{1,4}$`)

// normalizeHexID accepts a USB vendor/product ID with or without a "0x"
// prefix (lsusb prints them bare, e.g. "1a2c:212a") and returns the
// "0xNNNN" form libvirt's <vendor id='...'/>/<product id='...'/> expect.
func normalizeHexID(id string) (string, error) {
	if !hexIDRe.MatchString(id) {
		return "", fmt.Errorf("invalid hex id %q", id)
	}
	if !strings.HasPrefix(id, "0x") {
		id = "0x" + id
	}
	return strings.ToLower(id), nil
}

func buildHostdevRender(usbDevices []USBDeviceSpec, pciDevices []PCIDeviceSpec) ([]renderedUSBDevice, []renderedPCIDevice, error) {
	if len(pciDevices) > 0 && !iommuEnabled() {
		return nil, nil, fmt.Errorf("PCI passthrough requires IOMMU (VT-d/AMD-Vi) enabled on the host - none detected (no /sys/kernel/iommu_groups entries)")
	}
	renderedUSB := make([]renderedUSBDevice, len(usbDevices))
	for i, u := range usbDevices {
		vendor, err := normalizeHexID(u.VendorID)
		if err != nil {
			return nil, nil, fmt.Errorf("usb device %d: vendor_id: %w", i, err)
		}
		product, err := normalizeHexID(u.ProductID)
		if err != nil {
			return nil, nil, fmt.Errorf("usb device %d: product_id: %w", i, err)
		}
		renderedUSB[i] = renderedUSBDevice{VendorID: vendor, ProductID: product}
	}
	renderedPCI := make([]renderedPCIDevice, len(pciDevices))
	for i, p := range pciDevices {
		addr, err := parsePCIAddress(p.Address)
		if err != nil {
			return nil, nil, fmt.Errorf("pci device %d: %w", i, err)
		}
		renderedPCI[i] = addr
	}
	return renderedUSB, renderedPCI, nil
}

// domainXMLTemplate generates a libvirt domain definition for a new VM.
// cpu mode="host-model" is used rather than "host-passthrough" - close to
// host performance without passing through host-only CPU quirks that can
// break some guest installers. Graphics is VNC-only (no SPICE) per the
// design spec; port='-1' autoport='yes' lets libvirt pick a free port,
// read back later via GetXMLDesc for the console proxy.
//
// Boot device priority uses per-device <boot order='N'/> elements instead
// of the simpler <os><boot dev='hd'/></os> form whenever UseOSBoot is
// false - libvirt disallows mixing the two styles in one domain, so
// UseOSBoot picks exactly one for the whole document based on whether an
// explicit BootOrder was given at all.
const domainXMLTemplate = `<domain type='kvm'>
  <name>{{.Name}}</name>
  <memory unit='MiB'>{{.MemoryMiB}}</memory>
  <vcpu>{{.VCPUs}}</vcpu>
  <os>
    <type arch='x86_64' machine='q35'>hvm</type>
    {{if eq .Firmware "uefi"}}<loader readonly='yes' type='pflash'>{{.OVMFCodePath}}</loader>
    <nvram>{{.NVRAMPath}}</nvram>{{end}}
    {{if .UseOSBoot}}{{if .ISO.Path}}<boot dev='cdrom'/>{{end}}<boot dev='hd'/>{{end}}
  </os>
  <features><acpi/><apic/></features>
  <cpu mode='host-model'/>
  <clock offset='utc'/>
  <on_poweroff>destroy</on_poweroff>
  <on_reboot>restart</on_reboot>
  <on_crash>destroy</on_crash>
  <devices>
    <!-- Explicit xHCI (USB 3) controller - q35 doesn't reliably get one
         implied for free the way some older machine types do, and USB
         hostdev passthrough (including hot-attaching a device later)
         has nothing to attach to without it. -->
    <controller type='usb' model='qemu-xhci'/>
    {{range .Disks}}<disk type='file' device='disk'>
      <driver name='qemu' type='qcow2'{{if .SSD}} discard='unmap'{{end}}/>
      <source file='{{.Path}}'/>
      <target dev='{{.Target}}' bus='{{.Bus}}'/>
      {{if .BootOrder}}<boot order='{{.BootOrder}}'/>{{end}}
    </disk>
    {{end}}<disk type='file' device='cdrom'>
      <driver name='qemu' type='raw'/>
      {{if .ISO.Path}}<source file='{{.ISO.Path}}'/>
      {{end}}<target dev='sda' bus='sata'/>
      <readonly/>
      {{if .ISO.BootOrder}}<boot order='{{.ISO.BootOrder}}'/>{{end}}
    </disk>
    {{range .Networks}}<interface type='{{if eq .Mode "bridge"}}bridge{{else}}network{{end}}'>
      {{if eq .Mode "bridge"}}<source bridge='{{.BridgeName}}'/>{{else}}<source network='default'/>{{end}}
      <model type='{{.Model}}'/>
      {{if .MAC}}<mac address='{{.MAC}}'/>{{end}}
      {{if .LinkState}}<link state='{{.LinkState}}'/>{{end}}
      {{if .BootOrder}}<boot order='{{.BootOrder}}'/>{{end}}
    </interface>
    {{end}}{{range .USBDevices}}<hostdev mode='subsystem' type='usb' managed='yes'>
      <source>
        <vendor id='{{.VendorID}}'/>
        <product id='{{.ProductID}}'/>
      </source>
    </hostdev>
    {{end}}{{range .PCIDevices}}<hostdev mode='subsystem' type='pci' managed='yes'>
      <source>
        <address domain='0x{{.Domain}}' bus='0x{{.Bus}}' slot='0x{{.Slot}}' function='0x{{.Function}}'/>
      </source>
    </hostdev>
    {{end}}<input type='tablet' bus='usb'/>
    <input type='keyboard' bus='usb'/>
    <graphics type='vnc' port='-1' autoport='yes' listen='127.0.0.1'/>
    <!-- virtio (not plain vga) is required for the guest to actually
         apply the <resolution> hint below - vga only offers it as an
         optional VBE/EDID-style suggestion most guest display drivers
         never read, while virtio-gpu's own driver (built into modern
         Linux kernels; needs the virtio-win package on Windows) applies
         it directly. virtio-vga still provides a legacy VGA-compatible
         boot mode, so this doesn't cost anything a plain vga guest had. -->
    <video><model type='virtio' heads='1'>{{if .DisplayWidth}}<resolution x='{{.DisplayWidth}}' y='{{.DisplayHeight}}'/>{{end}}</model></video>
    <sound model='ich9'/>
    <memballoon model='virtio'/>
  </devices>
</domain>`

var domainTemplate = template.Must(template.New("domain").Parse(domainXMLTemplate))

type domainXMLData struct {
	Name          string
	VCPUs         uint
	MemoryMiB     uint64
	Disks         []renderedDisk
	ISO           *renderedISO
	Networks      []renderedNIC
	USBDevices    []renderedUSBDevice
	PCIDevices    []renderedPCIDevice
	UseOSBoot     bool
	Firmware      string
	OVMFCodePath  string
	NVRAMPath     string
	// DisplayWidth/Height set a preferred resolution hint on the video
	// device (libvirt's <resolution x= y=/>) - the guest OS reads this
	// similarly to a monitor's EDID and picks it as its default display
	// mode, genuinely reducing the framebuffer size (and so the VNC
	// bandwidth/latency) rather than just scaling the client-side view of
	// a larger one. Zero means "no hint" - the guest picks its own
	// default, same as before this existed.
	DisplayWidth  uint
	DisplayHeight uint
}

// ovmfCodePath is the read-only UEFI firmware image itself (the same for
// every VM). ovmfVarsTemplate is the writable NVRAM template copied
// per-VM (see nvramPathFor) - libvirt requires this to be its own file
// per domain since each VM's UEFI settings/boot entries live in it.
const (
	ovmfCodePath     = "/usr/share/OVMF/OVMF_CODE_4M.fd"
	ovmfVarsTemplate = "/usr/share/OVMF/OVMF_VARS_4M.fd"
)

func nvramPathFor(storageDir, name string) string {
	return filepath.Join(storageDir, name+"_VARS.fd")
}

// nvramDirFor anchors a UEFI VM's NVRAM file next to its first disk (the
// pre-multi-disk convention, and what lets tests relocate everything into
// a single t.TempDir() by just setting a disk path) - only a genuinely
// diskless VM falls back to the real default storage directory.
func nvramDirFor(disks []DiskSpec) string {
	if len(disks) > 0 && disks[0].Path != "" {
		return filepath.Dir(disks[0].Path)
	}
	return defaultStorageDir
}

// ensureNVRAM copies the blank OVMF_VARS template to this VM's own NVRAM
// file, if it doesn't already exist - each VM needs its own writable copy
// (its UEFI boot entries/settings live there), but the template itself
// must never be written to directly.
func ensureNVRAM(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	data, err := os.ReadFile(ovmfVarsTemplate)
	if err != nil {
		return fmt.Errorf("read OVMF VARS template: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

// resolveDiskPaths fills in an auto-generated path (in defaultStorageDir)
// for any disk spec that didn't provide one - the first disk gets
// "<name>.qcow2" (matching this app's pre-multi-disk convention, so
// existing single-disk VMs' paths don't change shape), later ones get
// "<name>-disk<N>.qcow2".
// autoDiskPath names an auto-generated disk by its position in the VM's
// full disk list - index must be the disk's real position there, never
// recomputed from an isolated single-item slice, or two different disks
// added one at a time (as UpdateVM appending a new disk does) would both
// land on "index 0" and collide on the exact same generated path (found
// this the hard way against real libvirtd: a second appended disk
// silently resolved to disk #1's own path instead of a new one).
func autoDiskPath(name string, index int) string {
	if index == 0 {
		return filepath.Join(defaultStorageDir, name+".qcow2")
	}
	return filepath.Join(defaultStorageDir, fmt.Sprintf("%s-disk%d.qcow2", name, index+1))
}

func resolveDiskPaths(name string, disks []DiskSpec) []DiskSpec {
	resolved := make([]DiskSpec, len(disks))
	copy(resolved, disks)
	for i := range resolved {
		if resolved[i].Path == "" {
			resolved[i].Path = autoDiskPath(name, i)
		}
	}
	return resolved
}

// provisionDisks creates a fresh qcow2 file for each disk spec that
// doesn't already have one on disk - used by CreateVM (every disk is
// new) and by UpdateVM when new disks are appended to an existing VM
// (existing ones are left untouched here; growing them is handled
// separately via qemu-img resize).
func provisionDisks(disks []DiskSpec) error {
	for _, d := range disks {
		if _, err := os.Stat(d.Path); err == nil {
			continue // already exists - an existing disk being kept, not created
		}
		if d.GiB == 0 {
			return fmt.Errorf("disk %q: size (gib) must be greater than 0", d.Path)
		}
		if err := os.MkdirAll(filepath.Dir(d.Path), 0755); err != nil {
			return fmt.Errorf("create disk directory: %w", err)
		}
		out, err := exec.Command("qemu-img", "create", "-f", "qcow2", d.Path, fmt.Sprintf("%dG", d.GiB)).CombinedOutput()
		if err != nil {
			return fmt.Errorf("qemu-img create %s: %v: %s", d.Path, err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func validateNetworks(networks []NICSpec) error {
	for i, n := range networks {
		if n.Mode == "bridge" && n.BridgeName == "" {
			return fmt.Errorf("network %d: bridge_name is required when mode is \"bridge\"", i)
		}
	}
	return nil
}

// CreateVM provisions a qcow2 disk per entry in req.Disks (req.Disks may
// be empty - a VM with no data disk at all is a valid configuration, e.g.
// PXE boot or storage attached later), defines the domain, and starts it
// immediately - "create" and "run" are one step from the UI's point of view.
func (s *LibvirtStore) CreateVM(req CreateVMRequest) (VM, error) {
	if !vmNameRe.MatchString(req.Name) {
		return VM{}, fmt.Errorf("invalid VM name %q: only letters, digits, - and _ are allowed", req.Name)
	}
	if err := validateNetworks(req.Networks); err != nil {
		return VM{}, err
	}

	disks := resolveDiskPaths(req.Name, req.Disks)
	if err := provisionDisks(disks); err != nil {
		return VM{}, err
	}

	renderedDisks, iso, renderedNets, err := buildDeviceRender(disks, req.ISOPath, req.Networks, req.BootOrder)
	if err != nil {
		return VM{}, err
	}
	renderedUSB, renderedPCI, err := buildHostdevRender(req.USBDevices, req.PCIDevices)
	if err != nil {
		return VM{}, err
	}

	data := domainXMLData{
		Name:          req.Name,
		VCPUs:         req.VCPUs,
		MemoryMiB:     req.MemoryMiB,
		Disks:         renderedDisks,
		ISO:           iso,
		Networks:      renderedNets,
		USBDevices:    renderedUSB,
		PCIDevices:    renderedPCI,
		UseOSBoot:     len(req.BootOrder) == 0,
		Firmware:      req.Firmware,
		DisplayWidth:  req.DisplayWidth,
		DisplayHeight: req.DisplayHeight,
	}
	if req.Firmware == "uefi" {
		nvramPath := nvramPathFor(nvramDirFor(disks), req.Name)
		if err := ensureNVRAM(nvramPath); err != nil {
			return VM{}, err
		}
		data.OVMFCodePath = ovmfCodePath
		data.NVRAMPath = nvramPath
	}
	var xmlBuf strings.Builder
	if err := domainTemplate.Execute(&xmlBuf, data); err != nil {
		return VM{}, fmt.Errorf("render domain XML: %w", err)
	}

	conn, err := s.getConn()
	if err != nil {
		return VM{}, err
	}
	dom, err := conn.DomainDefineXML(xmlBuf.String())
	if err != nil {
		return VM{}, fmt.Errorf("define domain: %w", err)
	}
	defer dom.Free()
	if err := dom.Create(); err != nil {
		return VM{}, fmt.Errorf("start domain: %w", err)
	}
	return toVM(dom)
}

// UpdateVMRequest is the JSON body accepted by PUT /vms/{name}. All
// fields are required (the frontend always sends the full current+edited
// form) rather than a partial patch - simpler to reason about than
// merging partial updates into a redefined domain.
//
// Disks is matched against the VM's current disks by position: an entry
// whose Path matches an existing disk can only grow (never shrink) that
// disk's size; a new Path (or an empty one) provisions and attaches an
// additional disk. Removing a disk isn't supported here - detaching
// backing storage is destructive enough (and easy enough to get wrong
// against a VM that's still holding data) that it deserves its own
// explicit, separately-confirmed action rather than living inside a
// general-purpose "save these settings" request.
type UpdateVMRequest struct {
	VCPUs      uint            `json:"vcpus"`
	MemoryMiB  uint64          `json:"memory_mib"`
	Disks      []DiskSpec      `json:"disks,omitempty"`
	ISOPath    string          `json:"iso_path,omitempty"`
	Networks   []NICSpec       `json:"networks,omitempty"`
	USBDevices []USBDeviceSpec `json:"usb_devices,omitempty"`
	PCIDevices []PCIDeviceSpec `json:"pci_devices,omitempty"`
	BootOrder  []string        `json:"boot_order,omitempty"`
	Firmware   string          `json:"firmware,omitempty"`
	// DisplayWidth/DisplayHeight - see CreateVMRequest's field of the
	// same name. Zero/omitted clears any previously-set resolution hint.
	DisplayWidth  uint `json:"display_width,omitempty"`
	DisplayHeight uint `json:"display_height,omitempty"`
}

// UpdateVM redefines an existing (stopped) domain with new settings.
// Changing vcpus/memory/firmware on a live domain is either unsupported
// or guest-fragile depending on the field, so this refuses to touch a
// running VM at all rather than picking and choosing which fields would
// be "safe" - shut it down first, same as virt-manager requires for the
// same class of change.
func (s *LibvirtStore) UpdateVM(name string, req UpdateVMRequest) (VM, error) {
	if err := validateNetworks(req.Networks); err != nil {
		return VM{}, err
	}

	dom, err := s.lookup(name)
	if err != nil {
		return VM{}, err
	}
	defer dom.Free()

	current, err := toVM(dom)
	if err != nil {
		return VM{}, err
	}

	active, err := dom.IsActive()
	if err != nil {
		return VM{}, err
	}
	if active {
		if (req.VCPUs > 0 && req.VCPUs != current.VCPUs) || (req.MemoryMiB > 0 && req.MemoryMiB != current.MemoryMiB) || (req.Firmware != "" && req.Firmware != current.Firmware) {
			return VM{}, fmt.Errorf("stop %q before changing its CPU cores, RAM, or firmware", name)
		}
		// Hot-update networks
		for _, net := range req.Networks {
			oldMAC := net.MAC
			if err := s.UpdateNetworkAdapter(name, oldMAC, net); err != nil {
				return VM{}, err
			}
		}
		// Hot-update ISO
		if req.ISOPath != current.ISOPath {
			if req.ISOPath != "" {
				_ = s.InsertCDROM(name, req.ISOPath)
			} else {
				_ = s.EjectCDROM(name)
			}
		}
		return s.GetVM(name)
	}

	// Match requested disks against current ones by path: a path that
	// already exists on disk is an existing disk (grow-only, never
	// shrink); anything else is a brand new disk to provision. A request
	// with fewer disks than currently exist would silently drop the
	// domain XML's reference to the missing ones - explicitly refuse that
	// rather than let it read as an accidental detach.
	if len(req.Disks) < len(current.Disks) {
		return VM{}, fmt.Errorf("cannot remove a disk this way (currently %d, requested %d) - detaching disks isn't supported yet", len(current.Disks), len(req.Disks))
	}
	currentByPath := make(map[string]DiskInfo, len(current.Disks))
	for _, d := range current.Disks {
		currentByPath[d.Path] = d
	}
	for i, d := range req.Disks {
		if existing, ok := currentByPath[d.Path]; ok {
			if d.GiB > 0 && d.GiB < existing.GiB {
				return VM{}, fmt.Errorf("cannot shrink disk %q (currently %d GiB, requested %d GiB) - only growing it is supported", d.Path, existing.GiB, d.GiB)
			}
			if d.GiB > existing.GiB {
				out, err := exec.Command("qemu-img", "resize", d.Path, fmt.Sprintf("%dG", d.GiB)).CombinedOutput()
				if err != nil {
					return VM{}, fmt.Errorf("resize disk %s: %v: %s", d.Path, err, strings.TrimSpace(string(out)))
				}
			}
		} else if d.Path == "" {
			req.Disks[i].Path = autoDiskPath(name, i)
		}
	}
	if err := provisionDisks(req.Disks); err != nil {
		return VM{}, err
	}

	renderedDisks, iso, renderedNets, err := buildDeviceRender(req.Disks, req.ISOPath, req.Networks, req.BootOrder)
	if err != nil {
		return VM{}, err
	}
	renderedUSB, renderedPCI, err := buildHostdevRender(req.USBDevices, req.PCIDevices)
	if err != nil {
		return VM{}, err
	}

	data := domainXMLData{
		Name:          name,
		VCPUs:         req.VCPUs,
		MemoryMiB:     req.MemoryMiB,
		Disks:         renderedDisks,
		ISO:           iso,
		Networks:      renderedNets,
		USBDevices:    renderedUSB,
		PCIDevices:    renderedPCI,
		UseOSBoot:     len(req.BootOrder) == 0,
		Firmware:      req.Firmware,
		DisplayWidth:  req.DisplayWidth,
		DisplayHeight: req.DisplayHeight,
	}
	if req.Firmware == "uefi" {
		nvramPath := nvramPathFor(nvramDirFor(req.Disks), name)
		if err := ensureNVRAM(nvramPath); err != nil {
			return VM{}, err
		}
		data.OVMFCodePath = ovmfCodePath
		data.NVRAMPath = nvramPath
	}
	var xmlBuf strings.Builder
	if err := domainTemplate.Execute(&xmlBuf, data); err != nil {
		return VM{}, fmt.Errorf("render domain XML: %w", err)
	}

	conn, err := s.getConn()
	if err != nil {
		return VM{}, err
	}
	// Verified against real libvirtd (not just the test:///default fake
	// driver, which also caught this): DomainDefineXML refuses a name
	// that already exists unless the new XML's UUID matches exactly -
	// since this template never specifies one (letting libvirt generate
	// it fresh each time), it has to be undefined first, not just
	// redefined in place. KEEP_NVRAM preserves the existing NVRAM file
	// (the VM's UEFI boot entries/settings) across the redefinition
	// instead of deleting it out from under the ensureNVRAM check just
	// below, which would otherwise silently reset it to a blank template.
	if err := undefineDomain(dom, libvirt.DOMAIN_UNDEFINE_KEEP_NVRAM); err != nil {
		return VM{}, fmt.Errorf("undefine domain for redefinition: %w", err)
	}
	newDom, err := conn.DomainDefineXML(xmlBuf.String())
	if err != nil {
		return VM{}, fmt.Errorf("redefine domain: %w", err)
	}
	defer newDom.Free()
	return toVM(newDom)
}

func (s *LibvirtStore) StartVM(name string) error {
	dom, err := s.lookup(name)
	if err != nil {
		return err
	}
	defer dom.Free()
	return dom.Create()
}

func (s *LibvirtStore) ShutdownVM(name string) error {
	dom, err := s.lookup(name)
	if err != nil {
		return err
	}
	defer dom.Free()
	return dom.Shutdown()
}

func (s *LibvirtStore) ForceOffVM(name string) error {
	dom, err := s.lookup(name)
	if err != nil {
		return err
	}
	defer dom.Free()
	return dom.Destroy()
}

// ResetVM forcibly resets the VM, like pressing a physical reset button -
// the guest OS gets no chance to shut down cleanly, unlike ShutdownVM's
// graceful ACPI signal. Only valid while running; libvirt errors out on a
// stopped domain.
func (s *LibvirtStore) ResetVM(name string) error {
	dom, err := s.lookup(name)
	if err != nil {
		return err
	}
	defer dom.Free()
	return dom.Reset(0)
}

// DeleteVM undefines the domain, force-stopping it first if still
// running. wipeDisk also removes the backing qcow2 file.
func (s *LibvirtStore) DeleteVM(name string, wipeDisk bool) error {
	dom, err := s.lookup(name)
	if err != nil {
		return err
	}
	defer dom.Free()

	var diskPaths []string
	if wipeDisk {
		vm, err := toVM(dom)
		if err != nil {
			return err
		}
		for _, d := range vm.Disks {
			diskPaths = append(diskPaths, d.Path)
		}
	}

	active, err := dom.IsActive()
	if err != nil {
		return err
	}
	if active {
		if err := dom.Destroy(); err != nil {
			return err
		}
	}
	// Plain Undefine() refuses to remove a domain that still has an NVRAM
	// file (every UEFI VM created here has one) with "cannot undefine
	// domain with nvram" - confirmed against real libvirtd, not just
	// assumed. UNDEFINE_NVRAM is a no-op for a domain with no NVRAM (BIOS
	// VMs), so it's always safe to pass.
	if err := undefineDomain(dom, libvirt.DOMAIN_UNDEFINE_NVRAM); err != nil {
		return err
	}

	for _, path := range diskPaths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove disk %s: %w", path, err)
		}
	}
	return nil
}

// usbHostdevXMLTemplate/pciHostdevXMLTemplate/diskDeviceXMLTemplate render
// a single <hostdev>/<disk> element on its own - the same device shapes
// domainXMLTemplate builds inline for a fresh domain, but usable alone
// here for AttachDeviceFlags/DetachDeviceFlags, which take one device's
// XML rather than a whole domain document.
const usbHostdevXMLTemplate = `<hostdev mode='subsystem' type='usb' managed='yes'>
  <source>
    <vendor id='{{.VendorID}}'/>
    <product id='{{.ProductID}}'/>
  </source>
</hostdev>`

const pciHostdevXMLTemplate = `<hostdev mode='subsystem' type='pci' managed='yes'>
  <source>
    <address domain='0x{{.Domain}}' bus='0x{{.Bus}}' slot='0x{{.Slot}}' function='0x{{.Function}}'/>
  </source>
</hostdev>`

const diskDeviceXMLTemplate = `<disk type='file' device='disk'>
  <driver name='qemu' type='qcow2'{{if .SSD}} discard='unmap'{{end}}/>
  <source file='{{.Path}}'/>
  <target dev='{{.Target}}' bus='{{.Bus}}'/>
</disk>`

var (
	usbHostdevTemplate = template.Must(template.New("usbHostdev").Parse(usbHostdevXMLTemplate))
	pciHostdevTemplate = template.Must(template.New("pciHostdev").Parse(pciHostdevXMLTemplate))
	diskDeviceTemplate = template.Must(template.New("diskDevice").Parse(diskDeviceXMLTemplate))
)

// attachOrDetachDevice modifies both the live running domain and its
// persistent config when the domain is active, or just the persistent
// config when it's stopped - the same "apply now and remember it" model
// virsh's own --live --config combination gives, so a hot-attached device
// survives the VM's next restart too instead of vanishing.
func attachOrDetachDevice(dom *libvirt.Domain, deviceXML string, attach bool) error {
	active, err := dom.IsActive()
	if err != nil {
		return err
	}
	flags := libvirt.DOMAIN_DEVICE_MODIFY_CONFIG
	if active {
		flags |= libvirt.DOMAIN_DEVICE_MODIFY_LIVE
	}
	err = doAttachOrDetachDevice(dom, deviceXML, attach, flags)
	// The test:///default fake driver used in tests (confirmed directly,
	// not assumed) rejects the CONFIG flag outright, even though real
	// libvirtd supports it fine - when the domain is active, fall back to
	// LIVE alone, which still performs the actual hot-plug/unplug (LIVE
	// only makes sense for an active domain in the first place - a
	// stopped one has nothing running to modify live, and this fake
	// driver's CONFIG-only limitation for a stopped domain has no
	// meaningful fallback at all; that specific gap is real-libvirtd-only
	// territory, covered by direct verification rather than this driver).
	if active && isUnsupportedFlagsError(err) {
		err = doAttachOrDetachDevice(dom, deviceXML, attach, libvirt.DOMAIN_DEVICE_MODIFY_LIVE)
	}
	return err
}

func doAttachOrDetachDevice(dom *libvirt.Domain, deviceXML string, attach bool, flags libvirt.DomainDeviceModifyFlags) error {
	if attach {
		return dom.AttachDeviceFlags(deviceXML, flags)
	}
	return dom.DetachDeviceFlags(deviceXML, flags)
}

func isUnsupportedFlagsError(err error) bool {
	var verr libvirt.Error
	return errors.As(err, &verr) && (verr.Code == libvirt.ERR_NO_SUPPORT || verr.Code == libvirt.ERR_INVALID_ARG)
}

// AttachUSBDevice hot-plugs a host USB device into name by vendor:product
// ID - works whether the VM is running (live) or stopped (config only,
// applied on next start).
func (s *LibvirtStore) AttachUSBDevice(name string, spec USBDeviceSpec) error {
	rendered, _, err := buildHostdevRender([]USBDeviceSpec{spec}, nil)
	if err != nil {
		return err
	}
	var buf strings.Builder
	if err := usbHostdevTemplate.Execute(&buf, rendered[0]); err != nil {
		return err
	}
	dom, err := s.lookup(name)
	if err != nil {
		return err
	}
	defer dom.Free()
	return attachOrDetachDevice(dom, buf.String(), true)
}

// DetachUSBDevice reverses AttachUSBDevice.
func (s *LibvirtStore) DetachUSBDevice(name string, spec USBDeviceSpec) error {
	rendered, _, err := buildHostdevRender([]USBDeviceSpec{spec}, nil)
	if err != nil {
		return err
	}
	var buf strings.Builder
	if err := usbHostdevTemplate.Execute(&buf, rendered[0]); err != nil {
		return err
	}
	dom, err := s.lookup(name)
	if err != nil {
		return err
	}
	defer dom.Free()
	return attachOrDetachDevice(dom, buf.String(), false)
}

// AttachPCIDevice hot-plugs a host PCI device into name by its BDF
// address - refuses without IOMMU, same as CreateVM/UpdateVM.
func (s *LibvirtStore) AttachPCIDevice(name string, spec PCIDeviceSpec) error {
	_, rendered, err := buildHostdevRender(nil, []PCIDeviceSpec{spec})
	if err != nil {
		return err
	}
	var buf strings.Builder
	if err := pciHostdevTemplate.Execute(&buf, rendered[0]); err != nil {
		return err
	}
	dom, err := s.lookup(name)
	if err != nil {
		return err
	}
	defer dom.Free()
	return attachOrDetachDevice(dom, buf.String(), true)
}

// DetachPCIDevice reverses AttachPCIDevice.
func (s *LibvirtStore) DetachPCIDevice(name string, spec PCIDeviceSpec) error {
	addr, err := parsePCIAddress(spec.Address)
	if err != nil {
		return err
	}
	var buf strings.Builder
	if err := pciHostdevTemplate.Execute(&buf, addr); err != nil {
		return err
	}
	dom, err := s.lookup(name)
	if err != nil {
		return err
	}
	defer dom.Free()
	return attachOrDetachDevice(dom, buf.String(), false)
}

// nextTargetForBus picks the first free target dev name (vda, vdb, ...)
// for bus among a VM's existing disks - used by AttachDisk, where the new
// disk's target must not collide with any disk already attached. Reading
// straight off the domain's own already-live disk list (rather than
// recomputing every target from scratch) is deliberately the simpler,
// more robust source of truth here: it reflects reality exactly no matter
// how those existing targets came to be.
func nextTargetForBus(existing []DiskInfo, bus string) (string, error) {
	prefix, ok := busTargetPrefix[bus]
	if !ok {
		return "", fmt.Errorf("unsupported bus %q (use virtio, sata, or ide)", bus)
	}
	used := map[string]bool{}
	for _, d := range existing {
		if d.Bus == bus {
			used[d.Target] = true
		}
	}
	for n := 0; n <= 25; n++ {
		t := prefix + string(rune('a'+n))
		if !used[t] {
			return t, nil
		}
	}
	return "", fmt.Errorf("too many disks on bus %q", bus)
}

// AttachDisk provisions a new qcow2 disk (or attaches an existing image,
// if spec.Path already exists on disk) and hot-plugs it into name.
func (s *LibvirtStore) AttachDisk(name string, spec DiskSpec) (DiskInfo, error) {
	dom, err := s.lookup(name)
	if err != nil {
		return DiskInfo{}, err
	}
	defer dom.Free()

	current, err := toVM(dom)
	if err != nil {
		return DiskInfo{}, err
	}
	bus := spec.Bus
	if bus == "" {
		bus = "virtio"
	}
	// QEMU/libvirt only support hot-plugging a disk into a running VM on
	// a bus that itself supports hotplug - virtio does, SATA/IDE
	// generally don't (confirmed directly: real libvirtd refuses a SATA
	// hot-attach with "disk bus 'sata' cannot be hotplugged"). Failing
	// with a clear, specific message here beats surfacing that raw error,
	// and beats silently provisioning a disk file that then can't
	// actually be attached.
	if bus != "virtio" {
		active, err := dom.IsActive()
		if err != nil {
			return DiskInfo{}, err
		}
		if active {
			return DiskInfo{}, fmt.Errorf("a %s disk can't be hot-plugged into a running VM - use VirtIO for live attach, or stop the VM and use Edit instead", strings.ToUpper(bus))
		}
	}
	if spec.Path == "" {
		spec.Path = autoDiskPath(name, len(current.Disks))
	}
	if err := provisionDisks([]DiskSpec{spec}); err != nil {
		return DiskInfo{}, err
	}
	target, err := nextTargetForBus(current.Disks, bus)
	if err != nil {
		return DiskInfo{}, err
	}
	rendered := renderedDisk{Path: spec.Path, Target: target, Bus: bus, SSD: spec.SSD}
	var buf strings.Builder
	if err := diskDeviceTemplate.Execute(&buf, rendered); err != nil {
		return DiskInfo{}, err
	}
	if err := attachOrDetachDevice(dom, buf.String(), true); err != nil {
		return DiskInfo{}, err
	}
	return DiskInfo{Path: spec.Path, GiB: spec.GiB, Bus: bus, Target: target, SSD: spec.SSD}, nil
}

// DetachDisk hot-unplugs the disk currently at target (e.g. "vdb") from
// name - the backing file itself is left on disk untouched, matching
// "eject/unplug" rather than "delete". Detaching a disk the guest OS
// still has mounted can lose data if it hasn't been safely unmounted
// first - that's on the caller (surfaced as an explicit warning in the
// UI), the same real risk unplugging a live USB drive from any OS carries.
func (s *LibvirtStore) DetachDisk(name string, target string) error {
	dom, err := s.lookup(name)
	if err != nil {
		return err
	}
	defer dom.Free()

	current, err := toVM(dom)
	if err != nil {
		return err
	}
	var found *DiskInfo
	for i := range current.Disks {
		if current.Disks[i].Target == target {
			found = &current.Disks[i]
			break
		}
	}
	if found == nil {
		return fmt.Errorf("no disk with target %q attached to %q", target, name)
	}
	var buf strings.Builder
	if err := diskDeviceTemplate.Execute(&buf, renderedDisk{Path: found.Path, Target: found.Target, Bus: found.Bus, SSD: found.SSD}); err != nil {
		return err
	}
	return attachOrDetachDevice(dom, buf.String(), false)
}

// cdromTarget is always "sda" - the domain XML template hardcodes this
// for every VM's optical drive (see domainXMLTemplate), so there's
// nothing to look up dynamically here.
const cdromTarget = "sda"

const cdromDeviceXMLTemplate = `<disk type='file' device='cdrom'>
  <driver name='qemu' type='raw'/>
  {{if .Path}}<source file='{{.Path}}'/>
  {{end}}<target dev='{{.Target}}' bus='sata'/>
  <readonly/>
</disk>`

var cdromDeviceTemplate = template.Must(template.New("cdromDevice").Parse(cdromDeviceXMLTemplate))

// updateDeviceXML changes an existing device in place (e.g. swapping a
// cdrom's inserted media) - unlike attach/detach, the device itself stays
// present in the domain, only its configuration changes. Live if the
// domain is running, persisted to config either way - same "apply now
// and remember it" model as attachOrDetachDevice.
func updateDeviceXML(dom *libvirt.Domain, deviceXML string) error {
	active, err := dom.IsActive()
	if err != nil {
		return err
	}
	flags := libvirt.DOMAIN_DEVICE_MODIFY_CONFIG
	if active {
		flags |= libvirt.DOMAIN_DEVICE_MODIFY_LIVE
	}
	err = dom.UpdateDeviceFlags(deviceXML, flags)
	if active && isUnsupportedFlagsError(err) {
		err = dom.UpdateDeviceFlags(deviceXML, libvirt.DOMAIN_DEVICE_MODIFY_LIVE)
	}
	return err
}

// EjectCDROM removes whatever ISO is inserted in name's optical drive,
// leaving the drive itself present but empty - the same "eject" concept
// any physical or virtual CD drive has, not a full detach.
func (s *LibvirtStore) EjectCDROM(name string) error {
	dom, err := s.lookup(name)
	if err != nil {
		return err
	}
	defer dom.Free()
	var buf strings.Builder
	if err := cdromDeviceTemplate.Execute(&buf, struct{ Path, Target string }{Target: cdromTarget}); err != nil {
		return err
	}
	return updateDeviceXML(dom, buf.String())
}

// InsertCDROM swaps in a different ISO (or the first one, if the drive
// was empty) without needing to stop the VM.
func (s *LibvirtStore) InsertCDROM(name, isoPath string) error {
	dom, err := s.lookup(name)
	if err != nil {
		return err
	}
	defer dom.Free()
	var buf strings.Builder
	if err := cdromDeviceTemplate.Execute(&buf, struct{ Path, Target string }{Path: isoPath, Target: cdromTarget}); err != nil {
		return err
	}
	return updateDeviceXML(dom, buf.String())
}

// SetNetworkLinkState connects or disconnects a virtual network adapter
// by its MAC address (or interface name) without deleting the device.
func (s *LibvirtStore) SetNetworkLinkState(name, mac, state string) error {
	if state != "up" && state != "down" {
		return fmt.Errorf("state must be 'up' or 'down'")
	}
	// Run live update if running
	_ = exec.Command("virsh", "domif-setlink", name, mac, state).Run()
	// Persist to configuration
	out, err := exec.Command("virsh", "domif-setlink", name, mac, state, "--config").CombinedOutput()
	if err != nil && len(out) > 0 {
		return fmt.Errorf("setlink config: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// UpdateNetworkAdapter updates or replaces an interface (mode, model, bridge, mac)
// live if running and persists the changes to domain config.
func (s *LibvirtStore) UpdateNetworkAdapter(name, oldMAC string, nic NICSpec) error {
	dom, err := s.lookup(name)
	if err != nil {
		return err
	}
	defer dom.Free()

	active, err := dom.IsActive()
	if err != nil {
		return err
	}

	model := nic.Model
	if model == "" {
		model = "virtio"
	}

	nicType := "network"
	source := "default"
	if nic.Mode == "bridge" {
		nicType = "bridge"
		if nic.BridgeName != "" {
			source = nic.BridgeName
		} else {
			source = "br0"
			nic.BridgeName = "br0"
		}
	}

	current, err := toVM(dom)
	if err != nil {
		return err
	}

	if oldMAC == "" && len(current.Networks) > 0 {
		oldMAC = current.Networks[0].MAC
	}
	mac := nic.MAC
	if mac == "" {
		mac = oldMAC
	}

	if active {
		// 1. Detach old interface if oldMAC is present
		if oldMAC != "" {
			_ = exec.Command("virsh", "detach-interface", name, "--type", "bridge", "--mac", oldMAC, "--live", "--config").Run()
			_ = exec.Command("virsh", "detach-interface", name, "--type", "network", "--mac", oldMAC, "--live", "--config").Run()
		}
		// 2. Attach new interface
		args := []string{"attach-interface", name, nicType, source, "--model", model, "--live", "--config"}
		if mac != "" {
			args = append(args, "--mac", mac)
		}
		out, err := exec.Command("virsh", args...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("attach interface: %s (%v)", strings.TrimSpace(string(out)), err)
		}
	} else {
		replaced := false
		newNetworks := make([]NICSpec, 0, len(current.Networks))
		for _, n := range current.Networks {
			if (oldMAC != "" && n.MAC == oldMAC) || (!replaced && oldMAC == "") {
				newNetworks = append(newNetworks, nic)
				replaced = true
			} else {
				newNetworks = append(newNetworks, NICSpec{
					Mode:       n.Mode,
					BridgeName: n.BridgeName,
					Model:      n.Model,
					MAC:        n.MAC,
					LinkState:  n.LinkState,
				})
			}
		}
		if !replaced {
			newNetworks = append(newNetworks, nic)
		}
		reqDisks := make([]DiskSpec, len(current.Disks))
		for i, d := range current.Disks {
			reqDisks[i] = DiskSpec{Path: d.Path, GiB: d.GiB, Bus: d.Bus, SSD: d.SSD}
		}
		_, err = s.UpdateVM(name, UpdateVMRequest{
			VCPUs:      current.VCPUs,
			MemoryMiB:  current.MemoryMiB,
			Firmware:   current.Firmware,
			ISOPath:    current.ISOPath,
			Disks:      reqDisks,
			Networks:   newNetworks,
			USBDevices: current.USBDevices,
			PCIDevices: current.PCIDevices,
			BootOrder:  current.BootOrder,
		})
		if err != nil {
			return err
		}
	}

	return nil
}
