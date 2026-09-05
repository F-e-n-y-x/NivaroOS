package model

// FstabMount describes a single /etc/fstab entry as surfaced to the API - either one
// NivaroOS created and can edit/remove (Managed == true), or a pre-existing system entry
// (root, swap, a manually-added share, ...) shown read-only for context.
type FstabMount struct {
	MountPoint  string `json:"mount_point"`
	Source      string `json:"source"`
	UUID        string `json:"uuid,omitempty"`
	FSType      string `json:"fstype"`
	Options     string `json:"options"`
	Dump        int    `json:"dump"`
	Pass        int    `json:"pass"`
	ReadOnly    bool   `json:"read_only"`
	MountAtBoot bool   `json:"mount_at_boot"`
	CheckAtBoot bool   `json:"check_at_boot"`
	Managed     bool   `json:"managed"`
	Enabled     bool   `json:"enabled"`
	Mounted     bool   `json:"mounted"`
	DriveLabel  string `json:"drive_label,omitempty"`
	DrivePath   string `json:"drive_path,omitempty"`
	Size        uint64 `json:"size,omitempty"`
}

// FstabCandidate is an already-formatted partition the fstab UI can offer to add: not
// currently in fstab, and not a reserved system/boot/swap volume.
type FstabCandidate struct {
	Path        string `json:"path"`
	UUID        string `json:"uuid"`
	Label       string `json:"label"`
	FSType      string `json:"fstype"`
	Size        uint64 `json:"size"`
	ParentModel string `json:"parent_model"`
	Mounted     bool   `json:"mounted"`
	MountPoint  string `json:"mount_point,omitempty"`
}

// AddFstabMountRequest is the body for POST /v1/storage/fstab.
type AddFstabMountRequest struct {
	UUID        string `json:"uuid"`
	MountPoint  string `json:"mount_point"`
	FSType      string `json:"fstype"`
	Options     string `json:"options"`
	ReadOnly    bool   `json:"read_only"`
	MountAtBoot bool   `json:"mount_at_boot"`
	CheckAtBoot bool   `json:"check_at_boot"`
}

// UpdateFstabMountRequest is the body for PUT /v1/storage/fstab. MountPoint identifies
// the existing managed entry to change; NewMountPoint may be the same value or a rename.
type UpdateFstabMountRequest struct {
	MountPoint    string `json:"mount_point"`
	NewMountPoint string `json:"new_mount_point"`
	FSType        string `json:"fstype"`
	Options       string `json:"options"`
	ReadOnly      bool   `json:"read_only"`
	MountAtBoot   bool   `json:"mount_at_boot"`
	CheckAtBoot   bool   `json:"check_at_boot"`
}

// SetFstabMountEnabledRequest is the body for PUT /v1/storage/fstab/enabled.
type SetFstabMountEnabledRequest struct {
	MountPoint string `json:"mount_point"`
	Enabled    bool   `json:"enabled"`
}
