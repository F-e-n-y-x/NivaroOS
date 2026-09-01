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

func ensureSharedMemoryBacking(conn *libvirt.Connect, dom *libvirt.Domain) error {
	rawXML, err := dom.GetXMLDesc(libvirt.DOMAIN_XML_INACTIVE)
	if err != nil {
		return err
	}
	if strings.Contains(rawXML, "<memoryBacking>") && strings.Contains(rawXML, "shared") {
		return nil
	}

	backingXML := "\n  <memoryBacking>\n    <source type='memfd'/>\n    <access mode='shared'/>\n  </memoryBacking>"
	var newXML string
	if idx := strings.Index(rawXML, "</vcpu>"); idx != -1 {
		newXML = rawXML[:idx+7] + backingXML + rawXML[idx+7:]
	} else if idx := strings.Index(rawXML, "</memory>"); idx != -1 {
		newXML = rawXML[:idx+9] + backingXML + rawXML[idx+9:]
	} else {
		return nil
	}

	_, err = conn.DomainDefineXML(newXML)
	return err
}

func (s *LibvirtStore) AttachSharedFolder(name string, spec SharedFolderSpec) error {
	if spec.SourceDir == "" {
		return errors.New("source_dir is required")
	}
	if spec.TargetTag == "" {
		spec.TargetTag = "nivaroshare"
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

	_ = ensureSharedMemoryBacking(conn, dom)

	var buf strings.Builder
	if err := sharedFolderTemplate.Execute(&buf, spec); err != nil {
		return err
	}

	err = attachOrDetachDevice(dom, buf.String(), true)
	if err != nil {
		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "shared memory") || strings.Contains(errStr, "virtiofs") {
			// Domain is active but was booted without memfd shared memory.
			// Save device to persistent domain config so it boots with it on restart.
			_ = dom.AttachDeviceFlags(buf.String(), libvirt.DOMAIN_DEVICE_MODIFY_CONFIG)
			return fmt.Errorf("Shared folder added to VM configuration! Please restart %s to activate VirtIO-FS shared memory.", name)
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
