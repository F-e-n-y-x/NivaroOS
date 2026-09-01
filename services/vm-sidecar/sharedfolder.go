// sharedfolder.go implements live Host-to-Guest direct directory sharing
// using VirtIO-FS (zero-network shared memory pass-through).
package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"text/template"
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

	dom, err := s.lookup(name)
	if err != nil {
		return err
	}
	defer dom.Free()

	var buf strings.Builder
	if err := sharedFolderTemplate.Execute(&buf, spec); err != nil {
		return err
	}

	return attachOrDetachDevice(dom, buf.String(), true)
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
