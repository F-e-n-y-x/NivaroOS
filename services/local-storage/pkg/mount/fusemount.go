//go:build linux

// This file implements the FUSE mounting glue for rclone remotes (mirrors
// rclone's own cmd/mount package), exposed as an exported MountFn so it can
// be called directly as a Go library from
// services/local-storage/service/storage.go rather than through rclone's
// CLI/RC command registration. Not to be confused with mount.go in this
// same package, which wraps the OS mount/umount CLI tools for local block
// devices - a separate, unrelated concern.
package mount

import (
	"fmt"
	"time"

	"bazil.org/fuse"
	fusefs "bazil.org/fuse/fs"
	"github.com/rclone/rclone/cmd/mountlib"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/vfs"
)

// mountOptions configures the options from the command line flags
func mountOptions(VFS *vfs.VFS, device string, opt *mountlib.Options) (options []fuse.MountOption) {
	options = []fuse.MountOption{
		fuse.MaxReadahead(uint32(opt.MaxReadAhead)),
		fuse.Subtype("rclone"),
		fuse.FSName(device),
	}
	if opt.AsyncRead {
		options = append(options, fuse.AsyncRead())
	}
	if opt.AllowOther {
		options = append(options, fuse.AllowOther())
	}
	if opt.AllowRoot {
		fs.Errorf(nil, "Ignoring --allow-root. Support has been removed upstream - see https://github.com/bazil/fuse/issues/144 for more info")
	}
	if opt.DefaultPermissions {
		options = append(options, fuse.DefaultPermissions())
	}
	if VFS.Opt.ReadOnly {
		options = append(options, fuse.ReadOnly())
	}
	if opt.WritebackCache {
		options = append(options, fuse.WritebackCache())
	}
	if opt.DaemonTimeout != 0 {
		options = append(options, fuse.DaemonTimeout(fmt.Sprint(int(time.Duration(opt.DaemonTimeout).Seconds()))))
	}
	if len(opt.ExtraOptions) > 0 {
		fs.Errorf(nil, "-o/--option not supported with this FUSE backend")
	}
	if len(opt.ExtraFlags) > 0 {
		fs.Errorf(nil, "--fuse-flag not supported with this FUSE backend")
	}
	return options
}

// MountFn mounts VFS at mountpoint using bazil.org/fuse.
//
// The mount point will be ready when this returns.
//
// Returns an error channel for the serve process to report an error on
// (e.g. when fusermount is called), an unmount func, the actual mountpoint
// used, and an error if the mount itself failed to start.
func MountFn(VFS *vfs.VFS, mountpoint string, opt *mountlib.Options) (<-chan error, func() error, string, error) {
	f := VFS.Fs()
	if err := mountlib.CheckOverlap(f, mountpoint); err != nil {
		return nil, nil, "", err
	}
	if err := mountlib.CheckAllowNonEmpty(mountpoint, opt); err != nil {
		return nil, nil, "", err
	}
	fs.Debugf(f, "Mounting on %q", mountpoint)

	if opt.DebugFUSE {
		fuse.Debug = func(msg any) {
			fs.Debugf("fuse", "%v", msg)
		}
	}

	c, err := fuse.Mount(mountpoint, mountOptions(VFS, opt.DeviceName, opt)...)
	if err != nil {
		return nil, nil, "", err
	}

	filesys := NewFS(VFS, opt)
	filesys.server = fusefs.New(c, nil)

	// Serve the mount point in the background returning error to errChan
	errChan := make(chan error, 1)
	go func() {
		err := filesys.server.Serve(filesys)
		closeErr := c.Close()
		if err == nil {
			err = closeErr
		}
		errChan <- err
	}()

	unmount := func() error {
		// Shutdown the VFS
		filesys.VFS.Shutdown()
		return fuse.Unmount(mountpoint)
	}

	return errChan, unmount, mountpoint, nil
}
