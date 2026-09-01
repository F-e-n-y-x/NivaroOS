// sharedfolder.go implements autonomous Host-to-Guest direct directory sharing
// using a Unified VM Shares root with host bind-mounting via VirtIO-FS.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	libvirt "libvirt.org/go/libvirt"
)

const vmSharesBaseDir = "/DATA/VM-Shares"

type SharedFolderSpec struct {
	SourceDir string `json:"source_dir"`
	TargetTag string `json:"target_tag"` // e.g. "nivaroshare" or subfolder name
	ReadOnly  bool   `json:"read_only,omitempty"`
}

const rootSharedFolderXMLTemplate = `<filesystem type='mount' accessmode='passthrough'>
  <driver type='virtiofs'/>
  <source dir='/DATA/VM-Shares/{{.Name}}'/>
  <target dir='nivaroshare'/>
</filesystem>`

var rootSharedFolderTemplate = template.Must(template.New("rootSharedFolder").Parse(rootSharedFolderXMLTemplate))

func getVMShareDir(name string) string {
	return filepath.Join(vmSharesBaseDir, name)
}

func isMounted(path string) bool {
	data, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return false
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == path {
			return true
		}
	}
	return false
}

func readVMSharesMetadata(name string) ([]SharedFolderSpec, error) {
	metaFile := filepath.Join(getVMShareDir(name), ".shares.json")
	data, err := os.ReadFile(metaFile)
	if err != nil {
		return nil, err
	}
	var shares []SharedFolderSpec
	if err := json.Unmarshal(data, &shares); err != nil {
		return nil, err
	}
	return shares, nil
}

func saveVMSharesMetadata(name string, shares []SharedFolderSpec) error {
	dir := getVMShareDir(name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(shares, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, ".shares.json"), data, 0644)
}

// SyncVMShareDir sets up the host-level unified share directory and bind mounts
// each individual host directory as a subfolder inside /DATA/VM-Shares/<vmName>.
func SyncVMShareDir(name string, shares []SharedFolderSpec) error {
	vmDir := getVMShareDir(name)
	if err := os.MkdirAll(vmDir, 0755); err != nil {
		return err
	}

	activeSubdirs := make(map[string]bool)

	for _, sf := range shares {
		if sf.SourceDir == "" {
			continue
		}
		subName := strings.TrimSpace(sf.TargetTag)
		if subName == "" || subName == "nivaroshare" {
			subName = filepath.Base(sf.SourceDir)
		}
		if subName == "" || subName == "/" || subName == "." {
			subName = "shared"
		}
		targetPath := filepath.Join(vmDir, subName)
		activeSubdirs[subName] = true

		if err := os.MkdirAll(targetPath, 0755); err != nil {
			continue
		}

		if !isMounted(targetPath) {
			_ = exec.Command("mount", "--bind", sf.SourceDir, targetPath).Run()
		}
	}

	// Clean up removed subdirectories
	entries, _ := os.ReadDir(vmDir)
	for _, entry := range entries {
		if entry.Name() == ".shares.json" {
			continue
		}
		if !activeSubdirs[entry.Name()] {
			targetPath := filepath.Join(vmDir, entry.Name())
			if isMounted(targetPath) {
				_ = exec.Command("umount", targetPath).Run()
			}
			_ = os.RemoveAll(targetPath)
		}
	}

	return saveVMSharesMetadata(name, shares)
}

func (s *LibvirtStore) ListSharedFolders(name string) ([]SharedFolderSpec, error) {
	if shares, err := readVMSharesMetadata(name); err == nil && len(shares) > 0 {
		return shares, nil
	}
	vm, err := s.GetVM(name)
	if err != nil {
		return nil, err
	}
	return vm.SharedFolders, nil
}

func ensureSharedMemoryAndPCIe(conn *libvirt.Connect, dom *libvirt.Domain) error {
	rawXML, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		return err
	}

	modified := false
	newXML := rawXML

	// 1. Shared Memory Backing
	if !strings.Contains(newXML, "<memoryBacking>") || !strings.Contains(newXML, "shared") {
		backingXML := "\n  <memoryBacking>\n    <source type='memfd'/>\n    <access mode='shared'/>\n  </memoryBacking>"
		if idx := strings.Index(newXML, "</vcpu>"); idx != -1 {
			newXML = newXML[:idx+7] + backingXML + newXML[idx+7:]
			modified = true
		} else if idx := strings.Index(newXML, "</memory>"); idx != -1 {
			newXML = newXML[:idx+9] + backingXML + newXML[idx+9:]
			modified = true
		}
	}

	// 2. PCIe Root Ports (needed for multiple hotplug VirtIO-FS/Disks/NICs on Q35)
	count := strings.Count(newXML, "model='pcie-root-port'") + strings.Count(newXML, `model="pcie-root-port"`)
	if count < 8 {
		var ports strings.Builder
		for i := count + 1; i <= 16; i++ {
			ports.WriteString(fmt.Sprintf("\n    <controller type='pci' model='pcie-root-port' index='%d'/>", i))
		}
		if idx := strings.Index(newXML, "<devices>"); idx != -1 {
			newXML = newXML[:idx+9] + ports.String() + newXML[idx+9:]
			modified = true
		}
	}

	if modified {
		_, err = conn.DomainDefineXML(newXML)
		return err
	}
	return nil
}

func (s *LibvirtStore) AttachSharedFolder(name string, spec SharedFolderSpec) error {
	if spec.SourceDir == "" {
		return errors.New("source_dir is required")
	}
	fi, err := os.Stat(spec.SourceDir)
	if err != nil {
		return fmt.Errorf("source folder %q does not exist: %w", spec.SourceDir, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("%q is not a directory", spec.SourceDir)
	}

	if spec.TargetTag == "" || spec.TargetTag == "nivaroshare" {
		spec.TargetTag = filepath.Base(spec.SourceDir)
	}

	// Read existing shares and append new one
	shares, _ := readVMSharesMetadata(name)
	found := false
	for i, existing := range shares {
		if existing.SourceDir == spec.SourceDir || existing.TargetTag == spec.TargetTag {
			shares[i] = spec
			found = true
			break
		}
	}
	if !found {
		shares = append(shares, spec)
	}

	// Sync host unified directory & bind mounts
	if err := SyncVMShareDir(name, shares); err != nil {
		return fmt.Errorf("sync share directory: %w", err)
	}

	conn, err := s.getConn()
	if err != nil {
		return err
	}

	dom, err := s.lookup(name)
	if err != nil {
		return err
	}
	defer dom.Free()

	_ = ensureSharedMemoryAndPCIe(conn, dom)

	// Check if root filesystem device is attached in XML
	rawXML, _ := dom.GetXMLDesc(0)
	if !strings.Contains(rawXML, fmt.Sprintf("dir='%s'", getVMShareDir(name))) {
		var buf strings.Builder
		structData := struct{ Name string }{Name: name}
		if err := rootSharedFolderTemplate.Execute(&buf, structData); err == nil {
			_ = attachOrDetachDevice(dom, buf.String(), true)
		}
	}

	return nil
}

func (s *LibvirtStore) DetachSharedFolder(name string, targetTag string) error {
	shares, err := readVMSharesMetadata(name)
	if err != nil {
		shares, _ = s.ListSharedFolders(name)
	}

	newShares := make([]SharedFolderSpec, 0, len(shares))
	for _, sf := range shares {
		if sf.TargetTag != targetTag && sf.SourceDir != targetTag && filepath.Base(sf.SourceDir) != targetTag {
			newShares = append(newShares, sf)
		}
	}

	return SyncVMShareDir(name, newShares)
}
