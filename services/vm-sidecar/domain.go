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
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
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
	DiskPath      string             `json:"disk_path,omitempty"`
	DiskGiB       uint64             `json:"disk_gib,omitempty"`
	NetworkMode   string             `json:"network_mode,omitempty"`
	Disks         []DiskInfo         `json:"disks"`
	Networks      []NICInfo          `json:"networks"`
	ISOPath       string             `json:"iso_path,omitempty"`
	USBDevices    []USBDeviceSpec    `json:"usb_devices,omitempty"`
	PCIDevices    []PCIDeviceSpec    `json:"pci_devices,omitempty"`
	SharedFolders []SharedFolderSpec `json:"shared_folders,omitempty"`
	BootOrder     []string           `json:"boot_order,omitempty"`
	// "bios" (SeaBIOS, the default) or "uefi" (OVMF) - detected from
	// whether the domain's XML has a <loader> element, since that's the
	// one thing distinguishing the two at the libvirt level.
	Firmware string `json:"firmware"`
	// Zero when no resolution hint is set - the guest picked its own
	// default display mode.
	DisplayWidth  uint `json:"display_width,omitempty"`
	DisplayHeight uint `json:"display_height,omitempty"`
	// Warning is only ever set on POST /vms's response: the VM was created
	// (and stays defined) but something after that - starting it - failed.
	Warning string `json:"warning,omitempty"`
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
	// Existing marks Path as an already-existing image to attach as-is.
	// Without it, a Path that already exists is refused (409) rather than
	// silently reusing whatever file happens to be there - a typo'd or
	// auto-generated path must never hand one VM another VM's disk.
	Existing bool `json:"existing,omitempty"`
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
	UUID string `xml:"uuid"`
	OS   struct {
		Firmware string `xml:"firmware,attr"`
		Loader   struct {
			Path   string `xml:",chardata"`
			Format string `xml:"format,attr"`
		} `xml:"loader"`
		NVRAM struct {
			Path           string `xml:",chardata"`
			Format         string `xml:"format,attr"`
			Template       string `xml:"template,attr"`
			TemplateFormat string `xml:"templateFormat,attr"`
		} `xml:"nvram"`
	} `xml:"os"`
	Devices struct {
		Disks []struct {
			Device string `xml:"device,attr"`
			Boot   struct {
				Order int `xml:"order,attr"`
			} `xml:"boot"`
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
			Boot struct {
				Order int `xml:"order,attr"`
			} `xml:"boot"`
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
		Filesystems []struct {
			Type       string `xml:"type,attr"`
			AccessMode string `xml:"accessmode,attr"`
			Driver     struct {
				Type string `xml:"type,attr"`
			} `xml:"driver"`
			Source struct {
				Dir string `xml:"dir,attr"`
			} `xml:"source"`
			Target struct {
				Dir string `xml:"dir,attr"`
			} `xml:"target"`
			ReadOnly *struct{} `xml:"readonly"`
		} `xml:"filesystem"`
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
	if strings.TrimSpace(parsed.OS.Loader.Path) != "" || parsed.OS.Firmware == "efi" {
		vm.Firmware = "uefi"
	}
	vm.BootOrder = parseBootOrder(parsed)
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
		model := i.Model.Type
		if model == "" {
			model = "virtio"
		}
		info := NICInfo{Model: model, MAC: i.MAC.Address, LinkState: linkState}
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
	vm.SharedFolders = sharesFromXML(parsed)
	return vm, nil
}

// parseBootOrder turns per-device <boot order='N'/> elements back into
// the symbolic BootOrder list buildDeviceRender accepts (disk target dev,
// "cdrom", "network"), so a VM's boot order round-trips through GET and a
// later PUT instead of being silently reset. Nil for a domain using the
// plain <os><boot dev=.../> form.
func parseBootOrder(parsed domainXML) []string {
	type entry struct {
		order int
		sym   string
	}
	var entries []entry
	for _, d := range parsed.Devices.Disks {
		if d.Boot.Order == 0 {
			continue
		}
		switch d.Device {
		case "cdrom":
			entries = append(entries, entry{d.Boot.Order, "cdrom"})
		case "disk":
			entries = append(entries, entry{d.Boot.Order, d.Target.Dev})
		}
	}
	for _, i := range parsed.Devices.Interfaces {
		if i.Boot.Order != 0 {
			entries = append(entries, entry{i.Boot.Order, "network"})
		}
	}
	if len(entries) == 0 {
		return nil
	}
	sort.Slice(entries, func(a, b int) bool { return entries[a].order < entries[b].order })
	order := make([]string, len(entries))
	for i, e := range entries {
		order[i] = e.sym
	}
	return order
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
	for i := range doms {
		vm, err := toVM(&doms[i])
		doms[i].Free()
		if err != nil {
			// One unreadable domain (e.g. undefined mid-listing) mustn't
			// blank out the whole VM list.
			log.Printf("list VMs: skipping a domain: %v", err)
			continue
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
	Disks         []DiskSpec         `json:"disks,omitempty"`
	ISOPath       string             `json:"iso_path,omitempty"`
	Networks      []NICSpec          `json:"networks,omitempty"`
	USBDevices    []USBDeviceSpec    `json:"usb_devices,omitempty"`
	PCIDevices    []PCIDeviceSpec    `json:"pci_devices,omitempty"`
	SharedFolders []SharedFolderSpec `json:"shared_folders,omitempty"`
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
  <name>{{x .Name}}</name>
  {{if .UUID}}<uuid>{{x .UUID}}</uuid>
  {{end}}<memory unit='MiB'>{{.MemoryMiB}}</memory>
  <vcpu>{{.VCPUs}}</vcpu>
  <memoryBacking>
    <source type='memfd'/>
    <access mode='shared'/>
  </memoryBacking>
  <os>
    <type arch='x86_64' machine='q35'>hvm</type>
    {{if eq .Firmware "uefi"}}<loader readonly='yes' type='pflash'{{if .LoaderFormat}} format='{{x .LoaderFormat}}'{{end}}>{{x .OVMFCodePath}}</loader>
    <nvram{{if .NVRAMTemplate}} template='{{x .NVRAMTemplate}}'{{end}}{{if .NVRAMTemplateFormat}} templateFormat='{{x .NVRAMTemplateFormat}}'{{end}}{{if .NVRAMFormat}} format='{{x .NVRAMFormat}}'{{end}}>{{x .NVRAMPath}}</nvram>{{end}}
    {{if .UseOSBoot}}<boot dev='cdrom'/><boot dev='hd'/>{{end}}
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
    <controller type='pci' model='pcie-root'/>
    <controller type='pci' model='pcie-root-port' index='1'/>
    <controller type='pci' model='pcie-root-port' index='2'/>
    <controller type='pci' model='pcie-root-port' index='3'/>
    <controller type='pci' model='pcie-root-port' index='4'/>
    <controller type='pci' model='pcie-root-port' index='5'/>
    <controller type='pci' model='pcie-root-port' index='6'/>
    <controller type='pci' model='pcie-root-port' index='7'/>
    <controller type='pci' model='pcie-root-port' index='8'/>
    <controller type='pci' model='pcie-root-port' index='9'/>
    <controller type='pci' model='pcie-root-port' index='10'/>
    <controller type='pci' model='pcie-root-port' index='11'/>
    <controller type='pci' model='pcie-root-port' index='12'/>
    <controller type='pci' model='pcie-root-port' index='13'/>
    <controller type='pci' model='pcie-root-port' index='14'/>
    <controller type='pci' model='pcie-root-port' index='15'/>
    <controller type='pci' model='pcie-root-port' index='16'/>
    {{if .Share}}{{template "share" .Share}}
    {{end}}{{range .Disks}}<disk type='file' device='disk'>
      <driver name='qemu' type='qcow2'{{if .SSD}} discard='unmap'{{end}}/>
      <source file='{{x .Path}}'/>
      <target dev='{{x .Target}}' bus='{{x .Bus}}'/>
      {{if .BootOrder}}<boot order='{{.BootOrder}}'/>{{end}}
    </disk>
    {{end}}<disk type='file' device='cdrom'>
      <driver name='qemu' type='raw'/>
      {{if .ISO.Path}}<source file='{{x .ISO.Path}}'/>
      {{end}}<target dev='sda' bus='sata'/>
      <readonly/>
      {{if .ISO.BootOrder}}<boot order='{{.ISO.BootOrder}}'/>{{end}}
    </disk>
    {{range .Networks}}{{template "nic" .}}
    {{end}}{{range .USBDevices}}<hostdev mode='subsystem' type='usb' managed='yes'>
      <source>
        <vendor id='{{x .VendorID}}'/>
        <product id='{{x .ProductID}}'/>
      </source>
    </hostdev>
    {{end}}{{range .PCIDevices}}<hostdev mode='subsystem' type='pci' managed='yes'>
      <source>
        <address domain='0x{{x .Domain}}' bus='0x{{x .Bus}}' slot='0x{{x .Slot}}' function='0x{{x .Function}}'/>
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
    <channel type='unix'>
      <target type='virtio' name='org.qemu.guest_agent.0'/>
    </channel>
  </devices>
</domain>`

// nicDeviceXMLTemplate is one <interface> element - shared by the full
// domain template (as the "nic" sub-template) and live NIC hot-plug.
const nicDeviceXMLTemplate = `<interface type='{{if eq .Mode "bridge"}}bridge{{else}}network{{end}}'>
      {{if eq .Mode "bridge"}}<source bridge='{{x .BridgeName}}'/>{{else}}<source network='default'/>{{end}}
      <model type='{{x .Model}}'/>
      {{if .MAC}}<mac address='{{x .MAC}}'/>{{end}}
      {{if .LinkState}}<link state='{{x .LinkState}}'/>{{end}}
      {{if .BootOrder}}<boot order='{{.BootOrder}}'/>{{end}}
    </interface>`

// xmlEscape makes s safe to interpolate into both XML text and a quoted
// attribute value. Every string reaching a domain/device XML template goes
// through it (as the "x" template func): text/template does no escaping
// of its own, so a single quote or "<" in a path, bridge name or MAC once
// let a caller inject arbitrary elements - another disk pointing at
// /dev/sda, a host PCI device - into a VM's definition.
func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

var xmlFuncs = template.FuncMap{"x": xmlEscape}

var (
	nicDeviceTemplate = template.Must(template.New("nic").Funcs(xmlFuncs).Parse(nicDeviceXMLTemplate))
	domainTemplate    = template.Must(template.Must(template.Must(nicDeviceTemplate.Clone()).AddParseTree("share", shareDeviceTemplate.Tree)).New("domain").Parse(domainXMLTemplate))
)

// shareDevice is the template data for shares' device - the default
// share (normalizeRequestedShares guarantees that's all shares holds).
func shareDevice(shares []SharedFolderSpec) *shareDeviceData {
	if len(shares) == 0 {
		return nil
	}
	d := newShareDeviceData(shares[0].ReadOnly)
	return &d
}

type domainXMLData struct {
	Name       string
	UUID       string
	VCPUs      uint
	MemoryMiB  uint64
	Disks      []renderedDisk
	ISO        *renderedISO
	Networks   []renderedNIC
	USBDevices []renderedUSBDevice
	PCIDevices []renderedPCIDevice
	// Share is the default shared folder's virtiofs device (nil: none).
	Share        *shareDeviceData
	UseOSBoot    bool
	Firmware     string
	OVMFCodePath string
	LoaderFormat string
	NVRAMPath    string
	// NVRAMFormat/NVRAMTemplate/NVRAMTemplateFormat are carried over from
	// an existing domain verbatim on update, so a UEFI VM's NVRAM file is
	// never relocated, reformatted or swapped for a blank template.
	NVRAMFormat         string
	NVRAMTemplate       string
	NVRAMTemplateFormat string
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
// every VM). ovmfVarsTemplate is the blank NVRAM template each VM gets its
// own copy of (see createNVRAM) - libvirt requires this to be its own file
// per domain since each VM's UEFI settings/boot entries live in it.
const (
	ovmfCodePath     = "/usr/share/OVMF/OVMF_CODE_4M.fd"
	ovmfVarsTemplate = "/usr/share/OVMF/OVMF_VARS_4M.fd"
)

// nvramPathFor names a new VM's NVRAM file. New VMs get qcow2 NVRAM, not a
// raw copy of the template: QEMU can only take internal snapshots of a VM
// with pflash firmware when its NVRAM is qcow2 ("internal snapshots of a
// VM with pflash based firmware require QCOW2 nvram format" otherwise).
func nvramPathFor(storageDir, name string) string {
	return filepath.Join(storageDir, name+"_VARS.qcow2")
}

// vmDirFor is a new VM's own folder, holding all of its files: disks
// (<name>.qcow2, <name>-disk2.qcow2, ...) and NVRAM (<name>_VARS.qcow2).
// VMs from before this layout keep their flat /DATA/VMs/<file> paths -
// those come from their domain XML and are still inside the storage dir.
func vmDirFor(name string) string {
	return filepath.Join(defaultStorageDir, name)
}

// createNVRAM converts the blank raw OVMF_VARS template into this VM's own
// qcow2 NVRAM file, if it doesn't already exist - each VM needs its own
// writable copy (its UEFI boot entries/settings live there), but the
// template itself must never be written to directly. created reports
// whether this call made the file (so a failed create can clean it up).
func createNVRAM(path string) (created bool, err error) {
	if _, err := os.Stat(path); err == nil {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, fmt.Errorf("create NVRAM directory: %w", err)
	}
	out, err := exec.Command("qemu-img", "convert", "-f", "raw", "-O", "qcow2", ovmfVarsTemplate, path).CombinedOutput()
	if err != nil {
		_ = os.Remove(path)
		return false, fmt.Errorf("create NVRAM %s from %s: %v: %s", path, ovmfVarsTemplate, err, strings.TrimSpace(string(out)))
	}
	return true, nil
}

// setNewNVRAM fills data's firmware fields for a VM getting fresh UEFI
// NVRAM (a new VM, or one switched from BIOS).
func setNewNVRAM(data *domainXMLData, nvramPath string) {
	data.OVMFCodePath = ovmfCodePath
	data.LoaderFormat = "raw"
	data.NVRAMPath = nvramPath
	data.NVRAMFormat = "qcow2"
	data.NVRAMTemplate = ovmfVarsTemplate
	data.NVRAMTemplateFormat = "raw"
}

// resolvePathForCheck returns p cleaned, with symlinks resolved as far as
// the path exists - a file that doesn't exist yet (a disk about to be
// created) gets its nearest existing ancestor's real path plus the
// remaining components. A dangling symlink anywhere along the way is
// refused outright: qemu-img would follow it and create the file wherever
// it points.
func resolvePathForCheck(p string) (string, error) {
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("path %q must be absolute", p)
	}
	cur := filepath.Clean(p)
	rest := ""
	for {
		resolved, err := filepath.EvalSymlinks(cur)
		if err == nil {
			return filepath.Join(resolved, rest), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		if _, lerr := os.Lstat(cur); lerr == nil {
			return "", fmt.Errorf("path %q contains a broken symlink", p)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return filepath.Clean(p), nil
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// pathWithinRoot resolves p (see resolvePathForCheck) and requires it to
// be strictly inside root, returning the resolved path.
func pathWithinRoot(p, root, what string) (string, error) {
	resolved, err := resolvePathForCheck(p)
	if err != nil {
		return "", badRequestf("%s %q: %v", what, p, err)
	}
	rootResolved, err := resolvePathForCheck(root)
	if err != nil {
		return "", err
	}
	if !isStrictlyWithin(resolved, rootResolved) {
		return "", badRequestf("%s %q must be inside %s", what, p, root)
	}
	return resolved, nil
}

// validateDiskPath confines a disk image to the VM storage directory - an
// unchecked path let a caller attach any host file or block device
// (/dev/sda, another service's data) to a VM, or have DeleteVM's wipe_disk
// delete it. The ISO library and the guest-writable shared folder are
// excluded even though they're inside it: an image a guest planted in the
// share (with a qcow2 backing file pointing anywhere) must never be
// attachable as a disk.
func validateDiskPath(p string) (string, error) {
	resolved, err := pathWithinRoot(p, defaultStorageDir, "disk path")
	if err != nil {
		return "", err
	}
	storage, err := resolvePathForCheck(defaultStorageDir)
	if err != nil {
		return "", err
	}
	for _, excluded := range []string{defaultISODir, defaultAutoShareDir} {
		ex, err := resolvePathForCheck(excluded)
		if err == nil && isStrictlyWithin(ex, storage) && (resolved == ex || isStrictlyWithin(resolved, ex)) {
			return "", badRequestf("disk path %q can't be inside %s", p, excluded)
		}
	}
	return resolved, nil
}

// validateISOPath confines an inserted ISO to the ISO library directory.
func validateISOPath(p string) (string, error) {
	return pathWithinRoot(p, defaultISODir, "ISO path")
}

// autoDiskPath names an auto-generated disk (inside the VM's own folder,
// see vmDirFor) by its position in the VM's
// full disk list - index must be the disk's real position there, never
// recomputed from an isolated single-item slice, or two different disks
// added one at a time (as UpdateVM appending a new disk does) would both
// land on "index 0" and collide on the exact same generated path (found
// this the hard way against real libvirtd: a second appended disk
// silently resolved to disk #1's own path instead of a new one). The
// first disk gets "<name>.qcow2" (this app's pre-multi-disk convention),
// later ones "<name>-disk<N>.qcow2" - and if that file already exists
// (left behind by a deleted VM of the same name, say), N keeps counting up
// until it names a file that doesn't, so a new disk never silently reuses
// an old image. taken holds paths already chosen in the same request.
func autoDiskPath(name string, index int, taken map[string]bool) string {
	dir := vmDirFor(name)
	candidate := filepath.Join(dir, name+".qcow2")
	n := index + 1
	if index > 0 {
		candidate = filepath.Join(dir, fmt.Sprintf("%s-disk%d.qcow2", name, n))
	}
	for {
		if _, err := os.Lstat(candidate); errors.Is(err, fs.ErrNotExist) && !taken[candidate] {
			return candidate
		}
		n++
		candidate = filepath.Join(dir, fmt.Sprintf("%s-disk%d.qcow2", name, n))
	}
}

// plannedDisk is a disk after path resolution: Create marks one that must
// be provisioned as a fresh qcow2 (vs an existing image being attached or
// kept).
type plannedDisk struct {
	DiskSpec
	Create bool
}

// planNewDisks resolves each requested disk that isn't one of keep (the
// VM's current disks, by path): an empty path gets a fresh auto-generated
// one; a given path must be inside the storage directory, and must not
// exist yet unless the caller explicitly marked it Existing (409 otherwise)
// - or must exist if it was.
func planNewDisks(name string, disks []DiskSpec, keep map[string]bool) ([]plannedDisk, error) {
	taken := map[string]bool{}
	for p := range keep {
		taken[p] = true
	}
	planned := make([]plannedDisk, len(disks))
	for i, d := range disks {
		if d.Path != "" && keep[d.Path] {
			planned[i] = plannedDisk{DiskSpec: d}
			continue
		}
		if d.Path == "" {
			if d.Existing {
				return nil, badRequestf("disk %d: path is required to attach an existing image", i)
			}
			d.Path = autoDiskPath(name, i, taken)
			taken[d.Path] = true
			planned[i] = plannedDisk{DiskSpec: d, Create: true}
			continue
		}
		resolved, err := validateDiskPath(d.Path)
		if err != nil {
			return nil, err
		}
		d.Path = resolved
		if taken[d.Path] {
			return nil, badRequestf("disk %d: %q is listed more than once", i, d.Path)
		}
		taken[d.Path] = true
		_, statErr := os.Stat(d.Path)
		exists := statErr == nil
		switch {
		case exists && !d.Existing:
			return nil, conflictf("disk %d: %q already exists - pick another path, or set \"existing\": true to attach that image as-is", i, d.Path)
		case !exists && d.Existing:
			return nil, badRequestf("disk %d: existing image %q not found", i, d.Path)
		}
		planned[i] = plannedDisk{DiskSpec: d, Create: !exists}
	}
	return planned, nil
}

// provisionDisks creates a fresh qcow2 file for each planned disk marked
// Create, returning the paths it created so a later failure can remove
// exactly those (and nothing that was there before).
func provisionDisks(disks []plannedDisk) (created []string, err error) {
	for _, d := range disks {
		if !d.Create {
			continue
		}
		if d.GiB == 0 {
			return created, badRequestf("disk %q: size (gib) must be greater than 0", d.Path)
		}
		if err := os.MkdirAll(filepath.Dir(d.Path), 0755); err != nil {
			return created, fmt.Errorf("create disk directory: %w", err)
		}
		out, err := exec.Command("qemu-img", "create", "-f", "qcow2", d.Path, fmt.Sprintf("%dG", d.GiB)).CombinedOutput()
		if err != nil {
			return created, fmt.Errorf("qemu-img create %s: %v: %s", d.Path, err, strings.TrimSpace(string(out)))
		}
		created = append(created, d.Path)
	}
	return created, nil
}

func removeFiles(paths []string) {
	for _, p := range paths {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			log.Printf("cleanup: remove %s: %v", p, err)
		}
	}
}

func diskSpecs(planned []plannedDisk) []DiskSpec {
	specs := make([]DiskSpec, len(planned))
	for i, p := range planned {
		specs[i] = p.DiskSpec
	}
	return specs
}

var (
	macRe = regexp.MustCompile(`^[0-9a-fA-F]{2}(:[0-9a-fA-F]{2}){5}$`)
	// nicModels are the NIC models this app offers - QEMU accepts many
	// more, but nothing outside this list gets anywhere near the domain XML.
	nicModels = map[string]bool{"virtio": true, "e1000e": true, "e1000": true, "rtl8139": true}
)

// validateNIC checks every field of one network adapter spec - all of
// them end up in domain XML.
func validateNIC(i int, n NICSpec) error {
	switch n.Mode {
	case "", "nat":
	case "bridge":
		if n.BridgeName == "" {
			return badRequestf("network %d: bridge_name is required when mode is \"bridge\"", i)
		}
		if err := validateIfaceName("bridge name", n.BridgeName); err != nil {
			return badRequestf("network %d: %v", i, err)
		}
	default:
		return badRequestf("network %d: unsupported mode %q (use nat or bridge)", i, n.Mode)
	}
	if n.Model != "" && !nicModels[n.Model] {
		return badRequestf("network %d: unsupported model %q (use virtio, e1000e, e1000 or rtl8139)", i, n.Model)
	}
	if n.MAC != "" && !macRe.MatchString(n.MAC) {
		return badRequestf("network %d: invalid MAC address %q", i, n.MAC)
	}
	if n.LinkState != "" && n.LinkState != "up" && n.LinkState != "down" {
		return badRequestf("network %d: link_state must be \"up\" or \"down\"", i)
	}
	return nil
}

func validateNetworks(networks []NICSpec) error {
	for i, n := range networks {
		if err := validateNIC(i, n); err != nil {
			return err
		}
	}
	return nil
}

// validateResources rejects CPU/RAM values no VM could boot with.
func validateResources(vcpus uint, memoryMiB uint64) error {
	if vcpus < 1 {
		return badRequestf("vcpus must be at least 1")
	}
	if memoryMiB < 128 {
		return badRequestf("memory_mib must be at least 128")
	}
	return nil
}

func validateDisplay(width, height uint) error {
	if (width == 0) != (height == 0) {
		return badRequestf("display_width and display_height must be given together")
	}
	return nil
}

func validateFirmware(fw string) error {
	if fw != "" && fw != "bios" && fw != "uefi" {
		return badRequestf("firmware must be \"bios\" or \"uefi\"")
	}
	return nil
}

// CreateVM provisions a qcow2 disk per entry in req.Disks (req.Disks may
// be empty - a VM with no data disk at all is a valid configuration, e.g.
// PXE boot or storage attached later), defines the domain, and starts it
// immediately - "create" and "run" are one step from the UI's point of view.
//
// Everything is validated before anything touches disk, and any disk or
// NVRAM file this call created is removed again if it fails before the
// domain is defined. Once it IS defined, a failure to start doesn't undo
// that: the VM is returned with Warning set instead, so a client retrying
// "create" doesn't just run into "already exists".
func (s *LibvirtStore) CreateVM(req CreateVMRequest) (VM, error) {
	if !vmNameRe.MatchString(req.Name) {
		return VM{}, badRequestf("invalid VM name %q: only letters, digits, - and _ are allowed", req.Name)
	}
	if err := validateResources(req.VCPUs, req.MemoryMiB); err != nil {
		return VM{}, err
	}
	if err := validateFirmware(req.Firmware); err != nil {
		return VM{}, err
	}
	if err := validateDisplay(req.DisplayWidth, req.DisplayHeight); err != nil {
		return VM{}, err
	}
	if err := validateNetworks(req.Networks); err != nil {
		return VM{}, err
	}
	isoPath := ""
	if req.ISOPath != "" {
		p, err := validateISOPath(req.ISOPath)
		if err != nil {
			return VM{}, err
		}
		isoPath = p
	}
	shares, err := normalizeRequestedShares(req.SharedFolders, nil)
	if err != nil {
		return VM{}, err
	}
	if _, err := defaultShareSource(); err != nil {
		return VM{}, err
	}
	renderedUSB, renderedPCI, err := buildHostdevRender(req.USBDevices, req.PCIDevices)
	if err != nil {
		return VM{}, badRequestf("%v", err)
	}

	conn, err := s.getConn()
	if err != nil {
		return VM{}, err
	}
	if existing, err := conn.LookupDomainByName(req.Name); err == nil {
		existing.Free()
		return VM{}, conflictf("a VM named %q already exists", req.Name)
	}

	planned, err := planNewDisks(req.Name, req.Disks, nil)
	if err != nil {
		return VM{}, err
	}
	disks := diskSpecs(planned)
	renderedDisks, iso, renderedNets, err := buildDeviceRender(disks, isoPath, req.Networks, req.BootOrder)
	if err != nil {
		return VM{}, badRequestf("%v", err)
	}

	created, err := provisionDisks(planned)
	// abort undoes everything this call created on disk: the new files,
	// then the VM's folder if that leaves it empty.
	abort := func() {
		removeFiles(created)
		_ = os.Remove(vmDirFor(req.Name))
	}
	if err != nil {
		abort()
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
		Share:         shareDevice(shares),
		UseOSBoot:     len(req.BootOrder) == 0,
		Firmware:      req.Firmware,
		DisplayWidth:  req.DisplayWidth,
		DisplayHeight: req.DisplayHeight,
	}
	if req.Firmware == "uefi" {
		nvramPath := nvramPathFor(vmDirFor(req.Name), req.Name)
		nvramCreated, err := createNVRAM(nvramPath)
		if nvramCreated {
			created = append(created, nvramPath)
		}
		if err != nil {
			abort()
			return VM{}, err
		}
		setNewNVRAM(&data, nvramPath)
	}
	var xmlBuf strings.Builder
	if err := domainTemplate.Execute(&xmlBuf, data); err != nil {
		abort()
		return VM{}, fmt.Errorf("render domain XML: %w", err)
	}

	dom, err := conn.DomainDefineXML(xmlBuf.String())
	if err != nil {
		abort()
		return VM{}, fmt.Errorf("define domain: %w", err)
	}
	defer dom.Free()

	startErr := dom.Create()
	vm, err := toVM(dom)
	if err != nil {
		return VM{}, err
	}
	if startErr != nil {
		vm.Warning = fmt.Sprintf("The VM was created but failed to start: %s", errorMessage(startErr))
	}
	return vm, nil
}

// UpdateVMRequest is the JSON body accepted by PUT /vms/{name}. All
// fields are required (the frontend always sends the full current+edited
// form) rather than a partial patch - simpler to reason about than
// merging partial updates into a redefined domain. The exceptions, for
// older/partial callers: vcpus/memory_mib of 0 and an empty firmware keep
// the current value, and an omitted (null) shared_folders or boot_order
// keeps the current ones.
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
	VCPUs         uint               `json:"vcpus"`
	MemoryMiB     uint64             `json:"memory_mib"`
	Disks         []DiskSpec         `json:"disks,omitempty"`
	ISOPath       string             `json:"iso_path,omitempty"`
	Networks      []NICSpec          `json:"networks,omitempty"`
	USBDevices    []USBDeviceSpec    `json:"usb_devices,omitempty"`
	PCIDevices    []PCIDeviceSpec    `json:"pci_devices,omitempty"`
	SharedFolders []SharedFolderSpec `json:"shared_folders,omitempty"`
	BootOrder     []string           `json:"boot_order,omitempty"`
	Firmware      string             `json:"firmware,omitempty"`
	// DisplayWidth/DisplayHeight - see CreateVMRequest's field of the
	// same name. Zero/omitted clears any previously-set resolution hint.
	DisplayWidth  uint `json:"display_width,omitempty"`
	DisplayHeight uint `json:"display_height,omitempty"`
}

// UpdateVM applies new settings to an existing domain. On a stopped VM it
// redefines the whole domain in place - same name and UUID, so libvirt
// updates the existing definition rather than replacing it, and a failed
// define leaves the old one untouched (the domain used to be undefined
// first, so any define error deleted the VM outright). On a running VM
// only what can safely change live (network adapters, the inserted ISO,
// the shared folder's read-only flag) is applied; anything else that
// differs is refused with a 409 naming those fields, rather than being
// silently dropped.
func (s *LibvirtStore) UpdateVM(name string, req UpdateVMRequest) (VM, error) {
	if err := validateFirmware(req.Firmware); err != nil {
		return VM{}, err
	}
	if err := validateDisplay(req.DisplayWidth, req.DisplayHeight); err != nil {
		return VM{}, err
	}
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
	if req.VCPUs == 0 {
		req.VCPUs = current.VCPUs
	}
	if req.MemoryMiB == 0 {
		req.MemoryMiB = current.MemoryMiB
	}
	if req.Firmware == "" {
		req.Firmware = current.Firmware
	}
	if req.BootOrder == nil {
		req.BootOrder = current.BootOrder
	}
	if err := validateResources(req.VCPUs, req.MemoryMiB); err != nil {
		return VM{}, err
	}
	shares, err := normalizeRequestedShares(req.SharedFolders, current.SharedFolders)
	if err != nil {
		return VM{}, err
	}
	if _, err := defaultShareSource(); err != nil {
		return VM{}, err
	}
	isoPath := req.ISOPath
	if isoPath != "" && isoPath != current.ISOPath {
		if isoPath, err = validateISOPath(isoPath); err != nil {
			return VM{}, err
		}
	}

	active, err := dom.IsActive()
	if err != nil {
		return VM{}, err
	}
	if active {
		return s.updateRunningVM(dom, name, current, req, isoPath, shares)
	}

	// Match requested disks against current ones by path: a path that
	// already exists on disk is an existing disk (grow-only, never
	// shrink); anything else is a brand new disk to provision. A request
	// with fewer disks than currently exist would silently drop the
	// domain XML's reference to the missing ones - explicitly refuse that
	// rather than let it read as an accidental detach.
	if len(req.Disks) < len(current.Disks) {
		return VM{}, badRequestf("cannot remove a disk this way (currently %d, requested %d) - detaching disks isn't supported yet", len(current.Disks), len(req.Disks))
	}
	currentByPath := make(map[string]DiskInfo, len(current.Disks))
	keep := make(map[string]bool, len(current.Disks))
	for _, d := range current.Disks {
		currentByPath[d.Path] = d
		keep[d.Path] = true
	}
	for _, d := range req.Disks {
		if existing, ok := currentByPath[d.Path]; ok && d.GiB > 0 && d.GiB < existing.GiB {
			return VM{}, badRequestf("cannot shrink disk %q (currently %d GiB, requested %d GiB) - only growing it is supported", d.Path, existing.GiB, d.GiB)
		}
	}
	planned, err := planNewDisks(name, req.Disks, keep)
	if err != nil {
		return VM{}, err
	}
	disks := diskSpecs(planned)

	renderedDisks, iso, renderedNets, err := buildDeviceRender(disks, isoPath, req.Networks, req.BootOrder)
	if err != nil {
		return VM{}, badRequestf("%v", err)
	}
	renderedUSB, renderedPCI, err := buildHostdevRender(req.USBDevices, req.PCIDevices)
	if err != nil {
		return VM{}, badRequestf("%v", err)
	}

	inactiveXML, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		return VM{}, err
	}
	var parsed domainXML
	if err := xml.Unmarshal([]byte(inactiveXML), &parsed); err != nil {
		return VM{}, err
	}

	data := domainXMLData{
		Name:          name,
		UUID:          strings.TrimSpace(parsed.UUID),
		VCPUs:         req.VCPUs,
		MemoryMiB:     req.MemoryMiB,
		Disks:         renderedDisks,
		ISO:           iso,
		Networks:      renderedNets,
		USBDevices:    renderedUSB,
		PCIDevices:    renderedPCI,
		Share:         shareDevice(shares),
		UseOSBoot:     len(req.BootOrder) == 0,
		Firmware:      req.Firmware,
		DisplayWidth:  req.DisplayWidth,
		DisplayHeight: req.DisplayHeight,
	}

	var created []string
	if req.Firmware == "uefi" {
		if loader := strings.TrimSpace(parsed.OS.Loader.Path); loader != "" {
			// Already UEFI: keep its firmware and NVRAM exactly as they are.
			data.OVMFCodePath = loader
			data.LoaderFormat = parsed.OS.Loader.Format
			data.NVRAMPath = strings.TrimSpace(parsed.OS.NVRAM.Path)
			data.NVRAMFormat = parsed.OS.NVRAM.Format
			data.NVRAMTemplate = parsed.OS.NVRAM.Template
			data.NVRAMTemplateFormat = parsed.OS.NVRAM.TemplateFormat
			if data.NVRAMPath == "" {
				return VM{}, fmt.Errorf("VM %q uses UEFI firmware but has no NVRAM path in its definition", name)
			}
		} else {
			nvramPath := nvramPathFor(vmDirFor(name), name)
			nvramCreated, err := createNVRAM(nvramPath)
			if nvramCreated {
				created = append(created, nvramPath)
			}
			if err != nil {
				return VM{}, err
			}
			setNewNVRAM(&data, nvramPath)
		}
	}

	var xmlBuf strings.Builder
	if err := domainTemplate.Execute(&xmlBuf, data); err != nil {
		removeFiles(created)
		return VM{}, fmt.Errorf("render domain XML: %w", err)
	}

	// Grow existing disks only once everything else has been validated -
	// a resize can't be undone.
	for _, d := range req.Disks {
		if existing, ok := currentByPath[d.Path]; ok && d.GiB > existing.GiB {
			out, err := exec.Command("qemu-img", "resize", d.Path, fmt.Sprintf("%dG", d.GiB)).CombinedOutput()
			if err != nil {
				removeFiles(created)
				return VM{}, fmt.Errorf("resize disk %s: %v: %s", d.Path, err, strings.TrimSpace(string(out)))
			}
		}
	}
	newDisks, err := provisionDisks(planned)
	created = append(created, newDisks...)
	if err != nil {
		removeFiles(created)
		return VM{}, err
	}

	conn, err := s.getConn()
	if err != nil {
		removeFiles(created)
		return VM{}, err
	}
	// Same name + same UUID: DomainDefineXML updates the existing
	// persistent definition in place (NVRAM, snapshots metadata and
	// autostart untouched) - no undefine, so there's no window in which a
	// failed define can lose the VM.
	newDom, err := conn.DomainDefineXML(xmlBuf.String())
	if err != nil {
		removeFiles(created)
		return VM{}, fmt.Errorf("redefine domain: %w", err)
	}
	defer newDom.Free()
	return toVM(newDom)
}

// stoppedOnlyChanges lists the fields of req that differ from current but
// can only be changed while the VM is shut down.
func stoppedOnlyChanges(current VM, req UpdateVMRequest) []string {
	var changed []string
	if req.VCPUs != current.VCPUs {
		changed = append(changed, "CPU cores")
	}
	if req.MemoryMiB != current.MemoryMiB {
		changed = append(changed, "memory")
	}
	if req.Firmware != current.Firmware {
		changed = append(changed, "firmware")
	}
	if req.Disks != nil && !disksMatch(current.Disks, req.Disks) {
		changed = append(changed, "disks")
	}
	if req.USBDevices != nil && !sameStringSet(usbKeys(current.USBDevices), usbKeys(req.USBDevices)) {
		changed = append(changed, "USB devices")
	}
	if req.PCIDevices != nil && !sameStringSet(pciKeys(current.PCIDevices), pciKeys(req.PCIDevices)) {
		changed = append(changed, "PCI devices")
	}
	if !sameStrings(current.BootOrder, req.BootOrder) {
		changed = append(changed, "boot order")
	}
	if req.DisplayWidth != current.DisplayWidth || req.DisplayHeight != current.DisplayHeight {
		changed = append(changed, "display resolution")
	}
	return changed
}

func disksMatch(current []DiskInfo, req []DiskSpec) bool {
	if len(current) != len(req) {
		return false
	}
	for i, d := range req {
		c := current[i]
		bus := d.Bus
		if bus == "" {
			bus = "virtio"
		}
		if d.Path != c.Path || (d.GiB != 0 && d.GiB != c.GiB) || bus != c.Bus || d.SSD != c.SSD {
			return false
		}
	}
	return true
}

func usbKeys(devs []USBDeviceSpec) []string {
	keys := make([]string, 0, len(devs))
	for _, d := range devs {
		v, _ := normalizeHexID(d.VendorID)
		p, _ := normalizeHexID(d.ProductID)
		keys = append(keys, v+":"+p)
	}
	return keys
}

func pciKeys(devs []PCIDeviceSpec) []string {
	keys := make([]string, 0, len(devs))
	for _, d := range devs {
		keys = append(keys, strings.ToLower(d.Address))
	}
	return keys
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sameStringSet(a, b []string) bool {
	a = append([]string(nil), a...)
	b = append([]string(nil), b...)
	sort.Strings(a)
	sort.Strings(b)
	return sameStrings(a, b)
}

// updateRunningVM is UpdateVM's live path - see UpdateVM.
func (s *LibvirtStore) updateRunningVM(dom *libvirt.Domain, name string, current VM, req UpdateVMRequest, isoPath string, shares []SharedFolderSpec) (VM, error) {
	changed := stoppedOnlyChanges(current, req)
	for _, sf := range current.SharedFolders {
		if isDefaultShare(sf) && sf.ReadOnly != shares[0].ReadOnly {
			changed = append(changed, "shared folder read-only setting")
		}
	}
	if len(changed) > 0 {
		return VM{}, conflictf("%q is running - shut it down first to change: %s", name, strings.Join(changed, ", "))
	}

	if req.Networks != nil {
		if err := s.applyLiveNetworks(dom, name, current.Networks, req.Networks); err != nil {
			return VM{}, err
		}
	}

	if isoPath != current.ISOPath {
		if isoPath != "" {
			if err := s.InsertCDROM(name, isoPath); err != nil {
				return VM{}, fmt.Errorf("insert ISO: %w", err)
			}
		} else if err := s.EjectCDROM(name); err != nil {
			return VM{}, fmt.Errorf("eject ISO: %w", err)
		}
	}

	// A VM without the default share yet gets it in its config (never
	// hot-plugged), taking effect at its next start.
	conn, err := s.getConn()
	if err != nil {
		return VM{}, err
	}
	if _, err := applyDefaultShare(conn, dom, shares[0].ReadOnly, true); err != nil {
		return VM{}, err
	}
	return s.GetVM(name)
}

// applyLiveNetworks reconciles a running VM's adapters with want, by MAC:
// an unchanged adapter is left alone (no needless unplug/replug), a link
// state-only change just toggles the link, any other change replaces that
// one adapter, an entry with no MAC is a new adapter to add, and a current
// adapter missing from want is removed.
func (s *LibvirtStore) applyLiveNetworks(dom *libvirt.Domain, name string, have []NICInfo, want []NICSpec) error {
	wanted := map[string]bool{}
	for _, n := range want {
		if n.MAC != "" {
			wanted[strings.ToLower(n.MAC)] = true
		}
	}
	for _, h := range have {
		if h.MAC != "" && !wanted[strings.ToLower(h.MAC)] {
			if err := attachOrDetachDevice(dom, nicXMLFromInfo(h), false); err != nil {
				return fmt.Errorf("remove network adapter %s: %w", h.MAC, err)
			}
		}
	}
	for _, n := range want {
		var old *NICInfo
		for i := range have {
			if n.MAC != "" && strings.EqualFold(have[i].MAC, n.MAC) {
				old = &have[i]
				break
			}
		}
		if old == nil {
			if err := attachNIC(dom, n); err != nil {
				return fmt.Errorf("add network adapter: %w", err)
			}
			continue
		}
		if nicMatches(*old, n) {
			continue
		}
		if nicMatches(*old, NICSpec{Mode: n.Mode, BridgeName: n.BridgeName, Model: n.Model, MAC: n.MAC, LinkState: old.LinkState}) {
			if err := s.SetNetworkLinkState(name, old.MAC, linkStateOrUp(n.LinkState)); err != nil {
				return err
			}
			continue
		}
		if err := replaceNIC(dom, *old, n); err != nil {
			return err
		}
	}
	return nil
}

func linkStateOrUp(state string) string {
	if state == "" {
		return "up"
	}
	return state
}

// nicMatches reports whether spec describes exactly the adapter info is.
func nicMatches(info NICInfo, spec NICSpec) bool {
	model := spec.Model
	if model == "" {
		model = "virtio"
	}
	mode := spec.Mode
	if mode == "" {
		mode = "nat"
	}
	bridge := ""
	if mode == "bridge" {
		bridge = spec.BridgeName
	}
	return info.Mode == mode && info.BridgeName == bridge && info.Model == model &&
		strings.EqualFold(info.MAC, spec.MAC) && linkStateOrUp(info.LinkState) == linkStateOrUp(spec.LinkState)
}

func renderNICXML(n NICSpec) (string, error) {
	model := n.Model
	if model == "" {
		model = "virtio"
	}
	var buf strings.Builder
	err := nicDeviceTemplate.Execute(&buf, renderedNIC{Mode: n.Mode, BridgeName: n.BridgeName, Model: model, MAC: n.MAC, LinkState: n.LinkState})
	return buf.String(), err
}

func nicXMLFromInfo(info NICInfo) string {
	x, _ := renderNICXML(NICSpec{Mode: info.Mode, BridgeName: info.BridgeName, Model: info.Model, MAC: info.MAC, LinkState: info.LinkState})
	return x
}

func attachNIC(dom *libvirt.Domain, n NICSpec) error {
	deviceXML, err := renderNICXML(n)
	if err != nil {
		return err
	}
	return attachOrDetachDevice(dom, deviceXML, true)
}

// replaceNIC swaps old for n. With a different MAC the new adapter is
// attached before the old one is removed; with the same MAC that order
// would briefly give the guest two adapters with one address, so the old
// one goes first - and if attaching the replacement then fails, the
// original is plugged back in so the VM doesn't end up without a network.
func replaceNIC(dom *libvirt.Domain, old NICInfo, n NICSpec) error {
	oldXML := nicXMLFromInfo(old)
	if !strings.EqualFold(old.MAC, n.MAC) && n.MAC != "" {
		// Different MAC: add the new adapter before removing the old one.
		if err := attachNIC(dom, n); err != nil {
			return fmt.Errorf("add replacement network adapter: %w", err)
		}
		if err := attachOrDetachDevice(dom, oldXML, false); err != nil {
			return fmt.Errorf("remove old network adapter %s: %w", old.MAC, err)
		}
		return nil
	}
	if err := attachOrDetachDevice(dom, oldXML, false); err != nil {
		return fmt.Errorf("remove old network adapter %s: %w", old.MAC, err)
	}
	if err := attachNIC(dom, n); err != nil {
		if rbErr := attachOrDetachDevice(dom, oldXML, true); rbErr != nil {
			return fmt.Errorf("attach replacement network adapter: %w (and re-attaching the original failed too: %v)", err, rbErr)
		}
		return fmt.Errorf("attach replacement network adapter (original restored): %w", err)
	}
	return nil
}

// StartVM first gives a VM that lacks it the default shared folder (see
// ensureDefaultShareDevice) - so every VM, however old, gets it on its
// next boot - and makes sure any legacy share export directory it still
// references exists, since virtiofsd would otherwise fail the start.
func (s *LibvirtStore) StartVM(name string) error {
	conn, err := s.getConn()
	if err != nil {
		return err
	}
	dom, err := s.lookup(name)
	if err != nil {
		return err
	}
	defer dom.Free()
	if active, err := dom.IsActive(); err == nil && !active {
		// A VM that was running when the service started gets its layout
		// migration now; its definition may change, so look it up again.
		if changed, err := s.migrateVMLayout(conn, dom); err != nil {
			log.Printf("vm %s: layout migration: %v", name, err)
		} else if changed {
			dom.Free()
			if dom, err = s.lookup(name); err != nil {
				return err
			}
		}
		if err := ensureDefaultShareDevice(conn, dom); err != nil {
			return fmt.Errorf("prepare shared folder: %w", err)
		}
	}
	if xmlDesc, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE); err == nil {
		ensureLegacyExportDirs(xmlDesc)
	}
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

// PauseVM freezes a running VM's vCPUs in place (it keeps its memory and
// resumes exactly where it was) - libvirt's "suspend".
func (s *LibvirtStore) PauseVM(name string) error {
	dom, err := s.lookup(name)
	if err != nil {
		return err
	}
	defer dom.Free()
	return dom.Suspend()
}

// ResumeVM continues a VM paused by PauseVM.
func (s *LibvirtStore) ResumeVM(name string) error {
	dom, err := s.lookup(name)
	if err != nil {
		return err
	}
	defer dom.Free()
	return dom.Resume()
}

// DeleteVM undefines a stopped domain. It refuses (409) a VM that's still
// running or paused, rather than force-stopping it first: a destroy that
// then hits an undefine error left a VM killed but not deleted. wipeDisk
// also removes its disk images - only those inside the VM storage
// directory, never an image attached from anywhere else - and its NVRAM;
// otherwise the NVRAM file is kept too.
func (s *LibvirtStore) DeleteVM(name string, wipeDisk bool) error {
	dom, err := s.lookup(name)
	if err != nil {
		return err
	}
	defer dom.Free()

	active, err := dom.IsActive()
	if err != nil {
		return err
	}
	if active {
		return conflictf("%q is still running - stop the VM first", name)
	}

	var diskPaths []string
	if wipeDisk {
		vm, err := toVM(dom)
		if err != nil {
			return err
		}
		for _, d := range vm.Disks {
			resolved, err := validateDiskPath(d.Path)
			if err != nil {
				log.Printf("delete VM %q: keeping disk %s: %v", name, d.Path, err)
				continue
			}
			diskPaths = append(diskPaths, resolved)
		}
		// UNDEFINE_NVRAM below has real libvirtd delete the NVRAM file
		// itself; removing it here too covers drivers that don't.
		if xmlDesc, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE); err == nil {
			var parsed domainXML
			if xml.Unmarshal([]byte(xmlDesc), &parsed) == nil {
				if nv := strings.TrimSpace(parsed.OS.NVRAM.Path); nv != "" {
					if resolved, err := validateDiskPath(nv); err == nil {
						diskPaths = append(diskPaths, resolved)
					}
				}
			}
		}
	}

	// Plain Undefine() refuses to remove a domain that still has an NVRAM
	// file (every UEFI VM created here has one) with "cannot undefine
	// domain with nvram" - confirmed against real libvirtd, not just
	// assumed - and one with snapshots unless their metadata goes too.
	// Both NVRAM flags are no-ops for a domain with no NVRAM (BIOS VMs).
	flags := libvirt.DOMAIN_UNDEFINE_SNAPSHOTS_METADATA | libvirt.DOMAIN_UNDEFINE_KEEP_NVRAM
	if wipeDisk {
		flags = libvirt.DOMAIN_UNDEFINE_SNAPSHOTS_METADATA | libvirt.DOMAIN_UNDEFINE_NVRAM
	}
	if err := undefineDomain(dom, flags); err != nil {
		return err
	}

	// A VM from the bind-mount era may have left its export directory
	// (and bind mounts) under legacyVMSharesBaseDir.
	removeLegacyVMShareDir(name)

	for _, path := range diskPaths {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove disk %s: %w", path, err)
		}
	}
	if wipeDisk {
		// The VM's own folder goes too - but only if that left it empty
		// (os.Remove refuses a non-empty directory): anything else in it
		// isn't this VM's to delete.
		if err := os.Remove(vmDirFor(name)); err != nil && !os.IsNotExist(err) {
			log.Printf("delete VM %q: keeping %s: %v", name, vmDirFor(name), err)
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
    <vendor id='{{x .VendorID}}'/>
    <product id='{{x .ProductID}}'/>
  </source>
</hostdev>`

const pciHostdevXMLTemplate = `<hostdev mode='subsystem' type='pci' managed='yes'>
  <source>
    <address domain='0x{{x .Domain}}' bus='0x{{x .Bus}}' slot='0x{{x .Slot}}' function='0x{{x .Function}}'/>
  </source>
</hostdev>`

const diskDeviceXMLTemplate = `<disk type='file' device='disk'>
  <driver name='qemu' type='qcow2'{{if .SSD}} discard='unmap'{{end}}/>
  <source file='{{x .Path}}'/>
  <target dev='{{x .Target}}' bus='{{x .Bus}}'/>
</disk>`

var (
	usbHostdevTemplate = template.Must(template.New("usbHostdev").Funcs(xmlFuncs).Parse(usbHostdevXMLTemplate))
	pciHostdevTemplate = template.Must(template.New("pciHostdev").Funcs(xmlFuncs).Parse(pciHostdevXMLTemplate))
	diskDeviceTemplate = template.Must(template.New("diskDevice").Funcs(xmlFuncs).Parse(diskDeviceXMLTemplate))
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
// if spec.Existing is set) and hot-plugs it into name.
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
	target, err := nextTargetForBus(current.Disks, bus)
	if err != nil {
		return DiskInfo{}, badRequestf("%v", err)
	}
	keep := make(map[string]bool, len(current.Disks))
	for _, d := range current.Disks {
		keep[d.Path] = true
	}
	if spec.Path != "" && keep[spec.Path] {
		return DiskInfo{}, conflictf("%q is already attached to %q", spec.Path, name)
	}
	// Planned as the next disk after the VM's current ones, so an
	// auto-generated path numbers after them (see autoDiskPath).
	planned, err := planNewDisks(name, append(currentDiskSpecs(current.Disks), spec), keep)
	if err != nil {
		return DiskInfo{}, err
	}
	newDisk := planned[len(planned)-1]
	spec = newDisk.DiskSpec
	created, err := provisionDisks([]plannedDisk{newDisk})
	if err != nil {
		removeFiles(created)
		return DiskInfo{}, err
	}
	rendered := renderedDisk{Path: spec.Path, Target: target, Bus: bus, SSD: spec.SSD}
	var buf strings.Builder
	if err := diskDeviceTemplate.Execute(&buf, rendered); err != nil {
		removeFiles(created)
		return DiskInfo{}, err
	}
	if err := attachOrDetachDevice(dom, buf.String(), true); err != nil {
		removeFiles(created)
		return DiskInfo{}, err
	}
	return DiskInfo{Path: spec.Path, GiB: spec.GiB, Bus: bus, Target: target, SSD: spec.SSD}, nil
}

func currentDiskSpecs(disks []DiskInfo) []DiskSpec {
	specs := make([]DiskSpec, len(disks))
	for i, d := range disks {
		specs[i] = DiskSpec{Path: d.Path, GiB: d.GiB, Bus: d.Bus, SSD: d.SSD}
	}
	return specs
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
  {{if .Path}}<source file='{{x .Path}}'/>
  {{end}}<target dev='{{x .Target}}' bus='sata'/>
  <readonly/>
</disk>`

var cdromDeviceTemplate = template.Must(template.New("cdromDevice").Funcs(xmlFuncs).Parse(cdromDeviceXMLTemplate))

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
// was empty) without needing to stop the VM, and ensures the domain's boot
// configuration includes the CD-ROM as a primary boot candidate.
func (s *LibvirtStore) InsertCDROM(name, isoPath string) error {
	dom, err := s.lookup(name)
	if err != nil {
		return err
	}
	defer dom.Free()
	if isoPath, err = validateISOPath(isoPath); err != nil {
		return err
	}
	var buf strings.Builder
	if err := cdromDeviceTemplate.Execute(&buf, struct{ Path, Target string }{Path: isoPath, Target: cdromTarget}); err != nil {
		return err
	}
	if err := updateDeviceXML(dom, buf.String()); err != nil {
		return err
	}
	return s.ensureCDROMBoot(dom)
}

func (s *LibvirtStore) ensureCDROMBoot(dom *libvirt.Domain) error {
	xmlStr, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		return err
	}
	if strings.Contains(xmlStr, "<boot dev='cdrom'") || strings.Contains(xmlStr, `<boot dev="cdrom"`) || strings.Contains(xmlStr, "<boot order=") {
		return nil
	}
	conn, err := s.getConn()
	if err != nil {
		return err
	}

	var newXML string
	if strings.Contains(xmlStr, "<boot dev='hd'/>") {
		newXML = strings.Replace(xmlStr, "<boot dev='hd'/>", "<boot dev='cdrom'/>\n    <boot dev='hd'/>", 1)
	} else if strings.Contains(xmlStr, `<boot dev="hd"/>`) {
		newXML = strings.Replace(xmlStr, `<boot dev="hd"/>`, `<boot dev="cdrom"/>`+"\n    "+`<boot dev="hd"/>`, 1)
	} else if idx := strings.Index(xmlStr, "</os>"); idx != -1 {
		newXML = xmlStr[:idx] + "  <boot dev='cdrom'/>\n    <boot dev='hd'/>\n  " + xmlStr[idx:]
	} else {
		return nil
	}
	newDom, err := conn.DomainDefineXML(newXML)
	if err != nil {
		return err
	}
	newDom.Free()
	return nil
}

// SetNetworkLinkState connects or disconnects a virtual network adapter
// by its MAC address (or interface name) without deleting the device.
func (s *LibvirtStore) SetNetworkLinkState(name, mac, state string) error {
	if state != "up" && state != "down" {
		return badRequestf("state must be 'up' or 'down'")
	}
	if !macRe.MatchString(mac) {
		return badRequestf("invalid MAC address %q", mac)
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

// UpdateNetworkAdapter updates or replaces one interface (mode, model,
// bridge, mac) - identified by oldMAC, or the first adapter when oldMAC is
// empty (adding one if the VM has none) - live if running (see
// replaceNIC) and in the persistent config either way.
func (s *LibvirtStore) UpdateNetworkAdapter(name, oldMAC string, nic NICSpec) error {
	if nic.Mode == "bridge" && nic.BridgeName == "" {
		nic.BridgeName = "br0"
	}
	if err := validateNIC(0, nic); err != nil {
		return err
	}
	if oldMAC != "" && !macRe.MatchString(oldMAC) {
		return badRequestf("invalid old_mac %q", oldMAC)
	}

	dom, err := s.lookup(name)
	if err != nil {
		return err
	}
	defer dom.Free()

	active, err := dom.IsActive()
	if err != nil {
		return err
	}
	current, err := toVM(dom)
	if err != nil {
		return err
	}

	idx := -1
	for i, n := range current.Networks {
		if (oldMAC == "" && i == 0) || (oldMAC != "" && strings.EqualFold(n.MAC, oldMAC)) {
			idx = i
			break
		}
	}
	if oldMAC != "" && idx < 0 {
		return badRequestf("no network adapter with MAC %s on %q", oldMAC, name)
	}
	if idx >= 0 && nic.MAC == "" {
		nic.MAC = current.Networks[idx].MAC
	}

	if active {
		if idx < 0 {
			return attachNIC(dom, nic)
		}
		if nicMatches(current.Networks[idx], nic) {
			return nil
		}
		return replaceNIC(dom, current.Networks[idx], nic)
	}

	// Stopped: redefine with every current setting passed through
	// unchanged except this one adapter - shared folders, display, boot
	// order and all.
	newNetworks := make([]NICSpec, 0, len(current.Networks)+1)
	for i, n := range current.Networks {
		if i == idx {
			newNetworks = append(newNetworks, nic)
			continue
		}
		newNetworks = append(newNetworks, NICSpec{Mode: n.Mode, BridgeName: n.BridgeName, Model: n.Model, MAC: n.MAC, LinkState: n.LinkState})
	}
	if idx < 0 {
		newNetworks = append(newNetworks, nic)
	}
	_, err = s.UpdateVM(name, UpdateVMRequest{
		VCPUs:      current.VCPUs,
		MemoryMiB:  current.MemoryMiB,
		Firmware:   current.Firmware,
		ISOPath:    current.ISOPath,
		Disks:      currentDiskSpecs(current.Disks),
		Networks:   newNetworks,
		USBDevices: current.USBDevices,
		PCIDevices: current.PCIDevices,
		// nil keeps the VM's current shared folder setting as-is.
		SharedFolders: nil,
		BootOrder:     current.BootOrder,
		DisplayWidth:  current.DisplayWidth,
		DisplayHeight: current.DisplayHeight,
	})
	return err
}
