package service

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/core/pkg/utils/httper"
)

// Cloud drives are rclone FUSE mounts with the VFS "full" cache: a copy
// onto one "finishes" as soon as the data is in rclone's local cache, and
// the real upload to Google Drive/OneDrive/... happens afterwards. Without
// this, the Files app reported "Done" before a single byte had left the
// box, and an upload failure after that point was never shown anywhere.
//
// waitCloudSync is the transfer engine's AfterWrite hook: when the
// destination is on an rclone mount, it waits for that remote's upload
// queue to drain, and fails the job if rclone reports upload errors.

type rcloneMount struct {
	point  string
	remote string // e.g. "google_drive_drive_123:"
}

// rcloneMountFor finds the rclone mount containing path (longest match).
func rcloneMountFor(path string) (rcloneMount, bool) {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return rcloneMount{}, false
	}
	defer f.Close()
	var best rcloneMount
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		// 36 35 98:0 /mnt1 /mnt2 rw,noatime master:1 - fuse.rclone remote: rw,...
		fields := strings.Fields(sc.Text())
		sep := -1
		for i, x := range fields {
			if x == "-" {
				sep = i
				break
			}
		}
		if sep < 0 || sep+2 >= len(fields) || len(fields) < 5 {
			continue
		}
		if fields[sep+1] != "fuse.rclone" {
			continue
		}
		point := unescapeMountinfo(fields[4])
		if (path == point || strings.HasPrefix(path, point+"/")) && len(point) > len(best.point) {
			best = rcloneMount{point: point, remote: fields[sep+2]}
		}
	}
	return best, best.point != ""
}

func unescapeMountinfo(s string) string {
	r := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return r.Replace(s)
}

type vfsStats struct {
	DiskCache struct {
		UploadsInProgress int  `json:"uploadsInProgress"`
		UploadsQueued     int  `json:"uploadsQueued"`
		ErroredFiles      int  `json:"erroredFiles"`
		OutOfSpace        bool `json:"outOfSpace"`
	} `json:"diskCache"`
	Error string `json:"error"`
}

func rcloneVFSStats(remote string) (vfsStats, error) {
	var st vfsStats
	res, err := httper.NewRestyClient().SetRetryCount(0).SetTimeout(10*time.Second).R().
		SetHeader("Content-Type", "application/json").
		SetBody(map[string]string{"fs": remote}).
		Post("/vfs/stats")
	if err != nil {
		return st, err
	}
	if err := json.Unmarshal(res.Body(), &st); err != nil {
		return st, err
	}
	if res.StatusCode() != 200 {
		return st, fmt.Errorf("%s", st.Error)
	}
	return st, nil
}

// Cloud drives are normally mounted by nivaroos-local-storage (rclone as a
// library), which publishes each mount's upload queue to this file while
// someone touches the "want" file (see local-storage/service/vfs_status.go).
const (
	cloudVFSStatusFile = "/var/run/nivaroos/cloud-vfs.json"
	cloudVFSWantFile   = "/var/run/nivaroos/cloud-vfs.want"
)

func localStorageVFSStats(mountPoint string) (vfsStats, bool) {
	now := time.Now()
	_ = os.Chtimes(cloudVFSWantFile, now, now)
	if _, err := os.Stat(cloudVFSWantFile); err != nil {
		_ = os.WriteFile(cloudVFSWantFile, nil, 0o644)
	}
	fi, err := os.Stat(cloudVFSStatusFile)
	if err != nil || time.Since(fi.ModTime()) > 5*time.Second {
		return vfsStats{}, false
	}
	raw, err := os.ReadFile(cloudVFSStatusFile)
	if err != nil {
		return vfsStats{}, false
	}
	var all map[string]struct {
		InProgress int  `json:"uploads_in_progress"`
		Queued     int  `json:"uploads_queued"`
		Errored    int  `json:"errored_files"`
		OutOfSpace bool `json:"out_of_space"`
	}
	if json.Unmarshal(raw, &all) != nil {
		return vfsStats{}, false
	}
	m, ok := all[mountPoint]
	if !ok {
		return vfsStats{}, false
	}
	var st vfsStats
	st.DiskCache.UploadsInProgress, st.DiskCache.UploadsQueued = m.InProgress, m.Queued
	st.DiskCache.ErroredFiles, st.DiskCache.OutOfSpace = m.Errored, m.OutOfSpace
	return st, true
}

// cloudStats reads a mount's upload queue from whichever process serves
// it: local-storage (usual) or the rclone daemon.
func cloudStats(m rcloneMount) (vfsStats, bool) {
	// local-storage only starts publishing once asked; give it a moment.
	for i := 0; i < 4; i++ {
		if st, ok := localStorageVFSStats(m.point); ok {
			return st, true
		}
		if st, err := rcloneVFSStats(m.remote); err == nil {
			return st, true
		}
		time.Sleep(700 * time.Millisecond)
	}
	return vfsStats{}, false
}

func waitCloudSync(ctx context.Context, dest string, progress func(string)) error {
	m, ok := rcloneMountFor(filepath.Clean(dest))
	if !ok {
		return nil
	}
	before, ok := cloudStats(m)
	if !ok {
		// Nobody we can ask about this mount (e.g. a stale mount); the copy
		// itself already succeeded locally.
		return nil
	}
	name := strings.TrimSuffix(m.remote, ":")
	// rclone waits WriteBack (5 s) after the last write before it starts
	// uploading - poll through that too.
	grace := time.Now().Add(8 * time.Second)
	for {
		st, ok := cloudStats(m)
		if !ok {
			return fmt.Errorf("lost contact with the cloud drive while it was uploading")
		}
		if st.DiskCache.OutOfSpace {
			return fmt.Errorf("the cloud drive's local cache is out of space - the upload is paused")
		}
		if st.DiskCache.ErroredFiles > before.DiskCache.ErroredFiles {
			return fmt.Errorf("%d file(s) failed to upload to %s - rclone will keep retrying; check the cloud account", st.DiskCache.ErroredFiles-before.DiskCache.ErroredFiles, name)
		}
		pending := st.DiskCache.UploadsInProgress + st.DiskCache.UploadsQueued
		if pending == 0 && time.Now().After(grace) {
			return nil
		}
		if pending > 0 {
			progress(fmt.Sprintf("Uploading %d file(s) to %s", pending, name))
			grace = time.Time{} // queue is moving; done as soon as it drains
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
