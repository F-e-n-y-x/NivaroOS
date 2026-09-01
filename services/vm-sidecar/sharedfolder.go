// sharedfolder.go implements offline Host-to-VM file sharing by dynamically
// packaging a host folder into an emulated USB mass storage drive.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type SharedFolderSpec struct {
	FolderPath string `json:"folder_path"`
	SizeMB     int    `json:"size_mb"` // Default: 1024 (1 GB)
	ReadOnly   bool   `json:"read_only"`
}

type SharedFolderInfo struct {
	Attached   bool   `json:"attached"`
	FolderPath string `json:"folder_path,omitempty"`
	SizeMB     int    `json:"size_mb,omitempty"`
	ImagePath  string `json:"image_path,omitempty"`
	Target     string `json:"target,omitempty"`
	MountedAt  string `json:"mounted_at,omitempty"`
	ReadOnly   bool   `json:"read_only,omitempty"`
}

var defaultSharesDir = "/var/lib/casaos/vms/shares"

func getSharesDir() string {
	return defaultSharesDir
}

func getShareImagePath(name string) string {
	return filepath.Join(getSharesDir(), name+"-shared-usb.img")
}

func getShareMetaPath(name string) string {
	return filepath.Join(getSharesDir(), name+"-shared-meta.json")
}

func (s *LibvirtStore) GetSharedFolder(name string) (SharedFolderInfo, error) {
	dom, err := s.lookup(name)
	if err != nil {
		return SharedFolderInfo{Attached: false}, err
	}
	defer dom.Free()

	metaPath := getShareMetaPath(name)
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return SharedFolderInfo{Attached: false}, nil
	}
	var info SharedFolderInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return SharedFolderInfo{Attached: false}, nil
	}

	xmlDesc, err := dom.GetXMLDesc(0)
	if err != nil {
		return info, nil
	}
	if !strings.Contains(xmlDesc, info.ImagePath) {
		_ = os.Remove(metaPath)
		return SharedFolderInfo{Attached: false}, nil
	}

	info.Attached = true
	return info, nil
}

func (s *LibvirtStore) MountSharedFolder(name string, spec SharedFolderSpec) (SharedFolderInfo, error) {
	if spec.FolderPath == "" {
		return SharedFolderInfo{}, errors.New("folder_path is required")
	}
	fi, err := os.Stat(spec.FolderPath)
	if err != nil {
		return SharedFolderInfo{}, fmt.Errorf("folder %q does not exist: %w", spec.FolderPath, err)
	}
	if !fi.IsDir() {
		return SharedFolderInfo{}, fmt.Errorf("%q is not a directory", spec.FolderPath)
	}

	dom, err := s.lookup(name)
	if err != nil {
		return SharedFolderInfo{}, err
	}
	defer dom.Free()

	// If a share is already mounted, unmount it first
	_ = s.UnmountSharedFolder(name)

	sharesDir := getSharesDir()
	if err := os.MkdirAll(sharesDir, 0755); err != nil {
		return SharedFolderInfo{}, fmt.Errorf("failed to create shares dir: %w", err)
	}

	imagePath := getShareImagePath(name)
	sizeMB := spec.SizeMB
	if sizeMB <= 0 {
		sizeMB = 1024
	}
	if sizeMB < 64 {
		sizeMB = 64
	}

	// 1. Create sparse image file
	_ = os.Remove(imagePath)
	cmd := exec.Command("truncate", "-s", fmt.Sprintf("%dM", sizeMB), imagePath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return SharedFolderInfo{}, fmt.Errorf("failed to create disk image: %s (%w)", string(out), err)
	}

	// 2. Format as FAT32
	mkfsCmd := exec.Command("mkfs.vfat", "-F", "32", "-n", "NIVAROSHARE", imagePath)
	if out, err := mkfsCmd.CombinedOutput(); err != nil {
		return SharedFolderInfo{}, fmt.Errorf("failed to format FAT32: %s (%w)", string(out), err)
	}

	// 3. Copy files from host folder into the image using mcopy if entries exist
	entries, err := os.ReadDir(spec.FolderPath)
	if err == nil && len(entries) > 0 {
		mcopyArgs := []string{"-s", "-o", "-i", imagePath}
		for _, entry := range entries {
			mcopyArgs = append(mcopyArgs, filepath.Join(spec.FolderPath, entry.Name()))
		}
		mcopyArgs = append(mcopyArgs, "::/")
		copyCmd := exec.Command("mcopy", mcopyArgs...)
		_ = copyCmd.Run()
	}

	// 4. Determine next available target on USB bus (sda, sdb, sdc, etc.)
	xmlDesc, err := dom.GetXMLDesc(0)
	if err != nil {
		return SharedFolderInfo{}, err
	}
	targetDev := "sda"
	for _, letter := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		cand := "sd" + letter
		if !strings.Contains(xmlDesc, fmt.Sprintf("dev='%s'", cand)) && !strings.Contains(xmlDesc, fmt.Sprintf("dev=\"%s\"", cand)) {
			targetDev = cand
			break
		}
	}

	// 5. Generate USB disk XML
	readOnlyTag := ""
	if spec.ReadOnly {
		readOnlyTag = "<readonly/>"
	}
	diskXML := fmt.Sprintf(`<disk type='file' device='disk'>
  <driver name='qemu' type='raw'/>
  <source file='%s'/>
  <target dev='%s' bus='usb'/>
  %s
</disk>`, imagePath, targetDev, readOnlyTag)

	// 6. Attach device to domain (live + persistent)
	if err := attachOrDetachDevice(dom, diskXML, true); err != nil {
		return SharedFolderInfo{}, fmt.Errorf("failed to attach USB disk to VM: %w", err)
	}

	info := SharedFolderInfo{
		Attached:   true,
		FolderPath: spec.FolderPath,
		SizeMB:     sizeMB,
		ImagePath:  imagePath,
		Target:     targetDev,
		MountedAt:  time.Now().Format(time.RFC3339),
		ReadOnly:   spec.ReadOnly,
	}

	metaData, _ := json.Marshal(info)
	_ = os.WriteFile(getShareMetaPath(name), metaData, 0644)

	return info, nil
}

func (s *LibvirtStore) SyncSharedFolder(name string) error {
	info, err := s.GetSharedFolder(name)
	if err != nil {
		return err
	}
	if !info.Attached || info.ImagePath == "" || info.FolderPath == "" {
		return errors.New("no shared folder attached to this VM")
	}
	if info.ReadOnly {
		return nil
	}

	// Copy files from FAT image back to host directory
	syncCmd := exec.Command("mcopy", "-s", "-o", "-i", info.ImagePath, "::/*", info.FolderPath+"/")
	if out, err := syncCmd.CombinedOutput(); err != nil {
		if !strings.Contains(string(out), "No match") && !strings.Contains(string(out), "File not found") {
			return fmt.Errorf("sync failed: %s (%w)", string(out), err)
		}
	}
	return nil
}

func (s *LibvirtStore) UnmountSharedFolder(name string) error {
	info, err := s.GetSharedFolder(name)
	if err != nil {
		return err
	}
	if !info.Attached {
		return nil
	}

	// 1. Sync modifications back to host before detaching
	_ = s.SyncSharedFolder(name)

	// 2. Detach device from domain
	dom, err := s.lookup(name)
	if err == nil {
		defer dom.Free()
		diskXML := fmt.Sprintf(`<disk type='file' device='disk'>
  <driver name='qemu' type='raw'/>
  <source file='%s'/>
  <target dev='%s' bus='usb'/>
</disk>`, info.ImagePath, info.Target)
		_ = attachOrDetachDevice(dom, diskXML, false)
	}

	// 3. Remove metadata file
	_ = os.Remove(getShareMetaPath(name))
	return nil
}
