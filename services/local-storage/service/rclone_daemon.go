package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"
)

// local-storage is the only owner of cloud drive mounts. The rclone daemon
// (rclone.service, `rclone rcd` on RcloneDaemonSocket) is still there for
// core (legacy config calls, vfs/stats), but it used to mount the same
// remotes on the same /mnt/<remote> paths too, and one of those mounts
// closing late unmounted local-storage's fresh mount (2026-09-30 05:08).
// Any mount still served by the daemon is handed over: once it has no
// uploads left it is unmounted through the daemon and mounted here.

const RcloneDaemonSocket = "/var/run/rclone/rclone.sock"

type daemonMount struct {
	Fs         string // "remote:"
	MountPoint string
}

type rcloneDaemon interface {
	// ListMounts: the daemon's mounts; errDaemonDown when it isn't
	// running (then it serves nothing).
	ListMounts() ([]daemonMount, error)
	// PendingUploads: files still to upload from that remote's VFS.
	PendingUploads(fs string) (int, error)
	Unmount(mountPoint string) error
}

var errDaemonDown = errors.New("the rclone daemon isn't running")

type socketDaemon struct {
	sock string
}

func newSocketDaemon(sock string) *socketDaemon { return &socketDaemon{sock: sock} }

func (d *socketDaemon) call(path string, in any, out any, timeout time.Duration) error {
	body, _ := json.Marshal(in)
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var dl net.Dialer
				return dl.DialContext(ctx, "unix", d.sock)
			},
		},
	}
	defer client.CloseIdleConnections()
	res, err := client.Post("http://rclone/"+path, "application/json", bytes.NewReader(body))
	if err != nil {
		if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED) {
			return errDaemonDown
		}
		return err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		return fmt.Errorf("rclone daemon %s: %s", path, strings.TrimSpace(e.Error+" "+res.Status))
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

func (d *socketDaemon) ListMounts() ([]daemonMount, error) {
	var out struct {
		MountPoints []daemonMount `json:"mountPoints"`
	}
	if err := d.call("mount/listmounts", map[string]any{}, &out, 10*time.Second); err != nil {
		return nil, err
	}
	return out.MountPoints, nil
}

func (d *socketDaemon) PendingUploads(fs string) (int, error) {
	var out struct {
		DiskCache *struct {
			UploadsInProgress int `json:"uploadsInProgress"`
			UploadsQueued     int `json:"uploadsQueued"`
		} `json:"diskCache"`
	}
	if err := d.call("vfs/stats", map[string]string{"fs": fs}, &out, 20*time.Second); err != nil {
		return 0, err
	}
	if out.DiskCache == nil { // cache off: nothing waits locally
		return 0, nil
	}
	return out.DiskCache.UploadsInProgress + out.DiskCache.UploadsQueued, nil
}

func (d *socketDaemon) Unmount(mountPoint string) error {
	return d.call("mount/unmount", map[string]string{"mountPoint": mountPoint}, nil, 60*time.Second)
}
