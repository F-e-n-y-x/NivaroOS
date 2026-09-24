package model

import (
	"path/filepath"
	"strings"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/file"
)

const defaultMountPath = "/mnt"

// GetMountPoint builds /mnt/<name>[_<label>][_<model>] for this block device.
// name comes from the client: "/", "\" and ".." are refused (it used to be
// passed straight to filepath.Join, so "../etc" escaped /mnt). The label and
// model come from the device itself and are sanitised to [A-Za-z0-9 _.-].
// The result always passes ValidateMountPoint.
func (m *LSBLKModel) GetMountPoint(name string) (string, error) {
	return m.getMountPoint(name, file.CheckNotExist)
}

func (m *LSBLKModel) getMountPoint(name string, notExist func(string) bool) (string, error) {
	if err := ValidateStorageName(name); err != nil {
		return "", err
	}
	name = SanitizeMountName(name)
	if name == "" {
		name = "Storage_" + SanitizeMountName(m.Name)
	}

	if l := SanitizeMountName(m.Label); l != "" {
		name += "_" + l
	}

	if md := SanitizeMountName(m.Model); md != "" {
		name += "_" + md
	}
	name = SanitizeMountName(name)

	mountPoint := filepath.Join(defaultMountPath, name)
	if !strings.HasPrefix(mountPoint, defaultMountPath+"/") {
		return "", ErrInvalidMountPoint
	}
	if err := ValidateMountPoint(mountPoint); err != nil {
		return "", err
	}
	if notExist(mountPoint) {
		return mountPoint, nil
	}
	alt := mountPoint + "_" + SanitizeMountName(m.Name)
	if err := ValidateMountPoint(alt); err != nil {
		return "", err
	}
	return alt, nil
}
