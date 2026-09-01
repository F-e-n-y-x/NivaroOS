// sharedfolder.go implements live Host-to-Guest direct directory sharing
// using VirtIO-FS (zero-network shared memory pass-through).
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"text/template"

	libvirt "libvirt.org/go/libvirt"
)

type SharedFolderSpec struct {
	SourceDir string `json:"source_dir"`
	TargetTag string `json:"target_tag"` // e.g. "nivaroshare"
	ReadOnly  bool   `json:"read_only,omitempty"`
}

const sharedFolderXMLTemplate = `<filesystem type='mount' accessmode='passthrough'>
  <driver type='virtiofs'/>
  <source dir='{{.SourceDir}}'/>
  <target dir='{{.TargetTag}}'/>
  {{if .ReadOnly}}<readonly/>{{end}}
</filesystem>`

var sharedFolderTemplate = template.Must(template.New("sharedFolder").Parse(sharedFolderXMLTemplate))

func (s *LibvirtStore) ListSharedFolders(name string) ([]SharedFolderSpec, error) {
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

	conn, err := s.getConn()
	if err != nil {
		return err
	}

	dom, err := s.lookup(name)
	if err != nil {
		return err
	}
	defer dom.Free()

	// Ensure domain XML has shared memory backing and enough PCIe root ports
	_ = ensureSharedMemoryAndPCIe(conn, dom)

	// Ensure unique tag if multiple folders are attached
	current, err := toVM(dom)
	if err == nil {
		usedTags := make(map[string]bool)
		for _, sf := range current.SharedFolders {
			usedTags[sf.TargetTag] = true
		}
		if spec.TargetTag == "" {
			spec.TargetTag = "nivaroshare"
		}
		if usedTags[spec.TargetTag] {
			baseTag := spec.TargetTag
			for i := 2; ; i++ {
				candidate := fmt.Sprintf("%s%d", baseTag, i)
				if !usedTags[candidate] {
					spec.TargetTag = candidate
					break
				}
			}
		}
	}

	var buf strings.Builder
	if err := sharedFolderTemplate.Execute(&buf, spec); err != nil {
		return err
	}

	err = attachOrDetachDevice(dom, buf.String(), true)
	if err != nil {
		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "shared memory") || strings.Contains(errStr, "pci") || strings.Contains(errStr, "virtiofs") {
			// Domain is active but was booted without enough PCIe ports or memfd.
			// Save device to persistent domain config so it boots with it on restart.
			_ = dom.AttachDeviceFlags(buf.String(), libvirt.DOMAIN_DEVICE_MODIFY_CONFIG)
			return fmt.Errorf("Shared folder added to VM configuration! Please restart %s to activate new PCIe root ports.", name)
		}
		return err
	}
	return nil
}

func (s *LibvirtStore) DetachSharedFolder(name string, targetTag string) error {
	if targetTag == "" {
		targetTag = "nivaroshare"
	}
	dom, err := s.lookup(name)
	if err != nil {
		return err
	}
	defer dom.Free()

	current, err := toVM(dom)
	if err != nil {
		return err
	}
	var found *SharedFolderSpec
	for _, sf := range current.SharedFolders {
		if sf.TargetTag == targetTag {
			found = &sf
			break
		}
	}
	if found == nil {
		return fmt.Errorf("shared folder with tag %q not found", targetTag)
	}

	var buf strings.Builder
	if err := sharedFolderTemplate.Execute(&buf, *found); err != nil {
		return err
	}

	return attachOrDetachDevice(dom, buf.String(), false)
}
