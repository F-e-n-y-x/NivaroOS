package service

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/rclone/rclone/fs"
	rconfig "github.com/rclone/rclone/fs/config"
)

// The Files app's Trash (services/core/service/trash) works by renaming an
// item into a trash folder on the same drive. On a cloud mount that rename
// is only instant when the provider can move files and folders itself;
// otherwise rclone downloads and re-uploads everything. Some providers
// keep their own trash, which a delete through the mount already lands in.
// Core can't see any of that, so each mount's answer is published here.

var CloudTrashFile = "/var/run/nivaroos/cloud-trash.json"

type cloudTrash struct {
	Mode     string `json:"mode"` // "trash", "provider" or "none"
	Provider string `json:"provider,omitempty"`
}

// providerTrash: backends whose deletes go to the provider's own trash,
// the option that turns that off, and its "off" value.
var providerTrash = map[string]struct{ name, option, off string }{
	"drive":    {"Google Drive", "use_trash", "false"},
	"onedrive": {"OneDrive", "hard_delete", "true"},
	"terabox":  {"TeraBox", "delete_permanently", "true"},
}

func cloudTrashFor(f fs.Fs) cloudTrash {
	section := f.Name()
	backend, _ := rconfig.LoadedData().GetValue(section, "type")
	if p, ok := providerTrash[backend]; ok {
		if v, _ := rconfig.LoadedData().GetValue(section, p.option); v != p.off {
			return cloudTrash{Mode: "provider", Provider: p.name}
		}
	}
	if feat := f.Features(); feat.Move != nil && feat.DirMove != nil {
		return cloudTrash{Mode: "trash"}
	}
	return cloudTrash{Mode: "none"}
}

// writeCloudTrash publishes every mount's mode. Callers hold mountMu.
func writeCloudTrash() {
	out := map[string]cloudTrash{}
	for mp, mnt := range MountLists {
		if mnt != nil && mnt.Fs != nil {
			out[mp] = cloudTrashFor(mnt.Fs)
		}
	}
	raw, _ := json.Marshal(out)
	_ = os.MkdirAll(filepath.Dir(CloudTrashFile), 0o755)
	tmp := CloudTrashFile + ".tmp"
	if os.WriteFile(tmp, raw, 0o644) == nil {
		_ = os.Rename(tmp, CloudTrashFile)
	}
}
