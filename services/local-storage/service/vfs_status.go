package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/rclone/rclone/fs/rc"
)

// Cloud copies made through the Files app land in these mounts' VFS write
// cache and are uploaded afterwards. The core service's transfer engine
// waits for that upload before calling a copy done (services/core/service/
// cloudsync.go), so it needs each mount's upload queue - published here as
// a small JSON file. It's computed only while core has asked for it
// recently (by touching WantFile), because VFS.Stats() walks the whole
// cached directory tree.

const (
	VFSStatusFile = "/var/run/nivaroos/cloud-vfs.json"
	VFSWantFile   = "/var/run/nivaroos/cloud-vfs.want"
)

type VFSMountStatus struct {
	UploadsInProgress int  `json:"uploads_in_progress"`
	UploadsQueued     int  `json:"uploads_queued"`
	ErroredFiles      int  `json:"errored_files"`
	OutOfSpace        bool `json:"out_of_space"`
}

func StartVFSStatusWriter(ctx context.Context) {
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
			st, err := os.Stat(VFSWantFile)
			if err != nil || time.Since(st.ModTime()) > 30*time.Second {
				continue
			}
			writeVFSStatus()
		}
	}()
}

func writeVFSStatus() {
	out := map[string]VFSMountStatus{}
	mountMu.Lock()
	for mp, mnt := range MountLists {
		if mnt == nil || mnt.VFS == nil {
			continue
		}
		s := VFSMountStatus{}
		if dc, ok := mnt.VFS.Stats()["diskCache"].(rc.Params); ok {
			s.UploadsInProgress = toInt(dc["uploadsInProgress"])
			s.UploadsQueued = toInt(dc["uploadsQueued"])
			s.ErroredFiles = toInt(dc["erroredFiles"])
			s.OutOfSpace, _ = dc["outOfSpace"].(bool)
		}
		out[mp] = s
	}
	mountMu.Unlock()
	raw, _ := json.Marshal(out)
	_ = os.MkdirAll(filepath.Dir(VFSStatusFile), 0o755)
	tmp := VFSStatusFile + ".tmp"
	if os.WriteFile(tmp, raw, 0o644) == nil {
		_ = os.Rename(tmp, VFSStatusFile)
	}
}

func toInt(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}
