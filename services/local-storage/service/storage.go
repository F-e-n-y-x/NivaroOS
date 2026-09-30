package service

import (
	"context"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"log"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/file"
	"github.com/F-e-n-y-x/NivaroOS/services/common/utils/logger"
	_ "github.com/F-e-n-y-x/NivaroOS/services/local-storage/backend/terabox"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/model"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/pkg/mount"
	"github.com/F-e-n-y-x/NivaroOS/services/local-storage/pkg/utils/httper"
	_ "github.com/rclone/rclone/backend/all"
	"github.com/rclone/rclone/cmd/mountlib"
	"github.com/rclone/rclone/fs"
	rconfig "github.com/rclone/rclone/fs/config"
	"github.com/rclone/rclone/fs/config/obscure"
	"github.com/rclone/rclone/fs/rc"
	"github.com/rclone/rclone/vfs/vfscommon"
	"go.uber.org/zap"
)

type StorageService interface {
	MountStorage(mountPoint, fs string) error
	UnmountStorage(mountPoint string) error
	UnmountAllStorage()
	GetStorages() (httper.MountList, error)
	CreateConfig(data rc.Params, name string, t string) error
	CheckAndMountByName(name string) error
	CheckAndMountAll() error
	GetConfigByName(name string) []string
	GetAttributeValueByName(name, key string) string
	SetAttributeValue(name, key, value string, isPassword bool) error
	DeleteConfigByName(name string)
	GetConfig() (httper.RemotesResult, error)
}

type storageStruct struct {
}

var MountLists map[string]*mountlib.MountPoint
var mountMu sync.Mutex

func (s *storageStruct) MountStorage(mountPoint, deviceName string) error {
	file.IsNotExistMkDir(mountPoint)
	mountMu.Lock()
	defer mountMu.Unlock()
	// Wanted mounted again (even if this try fails: the watcher retries).
	delete(heldMounts, mountPoint)
	currentFS, err := fs.NewFs(context.TODO(), deviceName+":")
	if err != nil {
		logger.Error("when CheckAndMountAll then", zap.Error(err))
		return err
	}
	cacheSettings := CloudCacheSettings()
	mountOptin := mountlib.Options{
		MaxReadAhead:  128 * 1024,
		AttrTimeout:   fs.Duration(30 * time.Minute),
		DaemonWait:    fs.Duration(60 * time.Second),
		NoAppleDouble: true,
		NoAppleXattr:  false,
		AsyncRead:     true,
		AllowOther:    true,
	}
	vfsOpt := vfscommon.Options{
		NoModTime:         false,
		NoChecksum:        false,
		NoSeek:            false,
		DirCacheTime:      fs.Duration(30 * time.Minute),
		PollInterval:      fs.Duration(time.Minute),
		ReadOnly:          false,
		Umask:             18,
		UID:               0,
		GID:               0,
		DirPerms:          vfscommon.FileMode(0777),
		FilePerms:         vfscommon.FileMode(0666),
		CacheMode:         vfscommon.CacheMode(cacheSettings.VFSCacheMode()),
		CacheMaxAge:       fs.Duration(cacheSettings.MaxAge),
		CachePollInterval: fs.Duration(60 * time.Second),
		ChunkSize:         32 * fs.Mebi,
		ChunkSizeLimit:    -1,
		// Capped so the read cache can't fill the disk (files still waiting
		// to upload are never evicted). Settings > Online storage > Cache.
		CacheMaxSize:       fs.SizeSuffix(cacheSettings.MaxSize),
		CaseInsensitive:    runtime.GOOS == "windows" || runtime.GOOS == "darwin", // default to true on Windows and Mac, false otherwise
		WriteWait:          fs.Duration(1000 * time.Millisecond),
		ReadWait:           fs.Duration(20 * time.Millisecond),
		WriteBack:          fs.Duration(5 * time.Second),
		ReadAhead:          0 * fs.Mebi,
		UsedIsSize:         false,
		DiskSpaceTotalSize: -1,
	}

	mnt := mountlib.NewMountPoint(mount.MountFn, mountPoint, currentFS, &mountOptin, &vfsOpt)
	_, err = mnt.Mount()
	if err != nil {
		logger.Error("when MountStorage then", zap.Error(err), zap.String("mountPoint", mountPoint), zap.String("device", deviceName))
		return err
	}
	dev, _ := mount.TopMountDevice(mountPoint)
	go func() {
		werr := mnt.Wait()
		if werr != nil {
			log.Printf("unmount FAILED: %v", werr)
		}
		mountMu.Lock()
		defer mountMu.Unlock()
		// Only if it's still this mount: a remount may already be there.
		if MountLists[mountPoint] != mnt {
			return
		}
		if werr != nil {
			// Still listed: the watcher checks whether it's really gone.
			mountExit[mountPoint] = "the mount's FUSE connection failed: " + werr.Error()
			return
		}
		delete(MountLists, mountPoint)
		delete(mountDevs, mountPoint)
		if !heldMounts[mountPoint] {
			mountExit[mountPoint] = "the mount ended by itself (unmounted outside NivaroOS, or its FUSE connection closed)"
		}
	}()
	MountLists[mountPoint] = mnt
	mountDevs[mountPoint] = dev
	delete(mountExit, mountPoint)
	parkKnownStuck(cloudMount{Name: deviceName, MountPoint: mountPoint, VFS: mnt.VFS})
	return nil
}
func (s *storageStruct) UnmountStorage(mountPoint string) error {
	mountMu.Lock()
	mnt := MountLists[mountPoint]
	// On purpose: the mount watcher leaves it unmounted until the next
	// MountStorage for it.
	heldMounts[mountPoint] = true
	mountMu.Unlock()
	if mnt == nil {
		// Never mounted in this process (expired token, failed mount, or a
		// restart): this was a nil-pointer panic, so such an account could
		// never be removed. Detach a stale kernel mount if there is one.
		if isCurrentMountPoint(mountPoint) {
			if err := unix.Unmount(mountPoint, unix.MNT_DETACH); err != nil {
				return fmt.Errorf("unmounting %s: %w", mountPoint, err)
			}
		}
		return nil
	}
	if err := mnt.Unmount(); err != nil {
		logger.Error("when umount then", zap.Error(err))
		// Still mounted (busy): keep watching it.
		mountMu.Lock()
		delete(heldMounts, mountPoint)
		mountMu.Unlock()
		return err
	}
	return nil
}
func (s *storageStruct) UnmountAllStorage() {
	cloudWatchStopping.Store(true)
	for _, v := range MountLists {
		err := v.Unmount()
		if err != nil {
			logger.Error("when umount then", zap.Error(err))
		}
	}
}
func (s *storageStruct) GetStorages() (httper.MountList, error) {
	ls := httper.MountList{}
	list := []httper.MountPoints{}
	for _, v := range MountLists {
		list = append(list, httper.MountPoints{
			MountPoint: v.MountPoint,
			Fs:         v.Fs.Name(),
		})
	}
	ls.MountPoints = list
	return ls, nil
	// return httper.GetMountList()
}

// CreateConfig writes a fully-formed remote config section directly
// (LoadedData().SetValue per key, then SaveConfig) rather than going
// through rclone's interactive Config state machine - that machinery is
// meant for prompting a user through a flow one step at a time, and even
// with NonInteractive:true it still requires a state to resume, so it
// isn't a generic "just write these values" primitive.
//
// This is intentionally backend-agnostic: it's used both for
// non-interactive "form" providers (S3/B2/WebDAV/SFTP/SMB - every value
// is already known upfront) and for OAuth providers connected via a
// pasted `rclone authorize` token (Drive/Dropbox/OneDrive - the token is
// already valid, there's no auth step left to run).
func (s *storageStruct) CreateConfig(data rc.Params, name string, t string) error {
	ri, err := fs.Find(t)
	if err != nil {
		return err
	}
	needsObscure := make(map[string]struct{})
	for _, option := range ri.Options {
		if option.IsPassword {
			needsObscure[option.Name] = struct{}{}
		}
	}

	rconfig.LoadedData().SetValue(name, "type", t)
	for k, v := range data {
		vStr := fmt.Sprint(v)
		if _, ok := needsObscure[k]; ok {
			// Store passwords obscured, same as rclone's own config UI -
			// leave already-obscured values (e.g. round-tripped from a
			// previous read) alone rather than double-obscuring them.
			if _, revealErr := obscure.Reveal(vStr); revealErr != nil {
				obscured, obscureErr := obscure.Obscure(vStr)
				if obscureErr != nil {
					return fmt.Errorf("failed to obscure %q: %w", k, obscureErr)
				}
				vStr = obscured
			}
		}
		rconfig.LoadedData().SetValue(name, k, vStr)
	}
	// Default drive settings for high performance and avoiding hangs on dangling shortcuts
	if t == "drive" {
		if _, ok := data["skip_shortcuts"]; !ok {
			rconfig.LoadedData().SetValue(name, "skip_shortcuts", "true")
		}
		if _, ok := data["skip_dangling_shortcuts"]; !ok {
			rconfig.LoadedData().SetValue(name, "skip_dangling_shortcuts", "true")
		}
	}
	rconfig.SaveConfig()
	return nil
}
func (s *storageStruct) CheckAndMountByName(name string) error {

	mountPoint, found := rconfig.LoadedData().GetValue(name, "mount_point")
	if !found && len(mountPoint) == 0 {
		logger.Error("when CheckAndMountByName then mountpoint is empty", zap.String("mountPoint", mountPoint), zap.String("fs", name))
		return errors.New("mountpoint is empty")
	}
	return MyService.Storage().MountStorage(mountPoint, name)
}

// CheckAndMountAll mounts every configured account at startup. A drive
// the rclone daemon still mounts is left to the mount watcher, which takes
// it over once the daemon has no uploads pending for it; failed mounts are
// retried by the watcher too (cloud_mount_watch.go).
func (s *storageStruct) CheckAndMountAll() error {
	section := rconfig.LoadedData().GetSectionList()

	logger.Info("when CheckAndMountAll section", zap.Any("section", section))
	daemonAt, known := newMountWatcher(processMountEnv{}, cloudDaemon).daemonMounts()
	for _, v := range section {
		mountPoint, found := rconfig.LoadedData().GetValue(v, "mount_point")

		if !found || len(mountPoint) == 0 {
			logger.Info("when CheckAndMountAll then mountpoint is empty", zap.String("mountPoint", mountPoint), zap.String("fs", v))
			continue
		}
		if _, ok := daemonAt[filepath.Clean(mountPoint)]; ok {
			logger.Info("cloud mounts: the rclone daemon still mounts this drive; taking it over in the background", zap.String("mountPoint", mountPoint), zap.String("fs", v))
			continue
		}
		if !known {
			// Can't tell whether a mount there is the daemon's: detaching
			// it blind is what unmounted a fresh mount on 2026-09-30.
			logger.Info("cloud mounts: rclone daemon didn't answer; the watcher mounts this drive shortly", zap.String("mountPoint", mountPoint), zap.String("fs", v))
			continue
		}
		lazyUmountStaleRemote(v)
		err := MyService.Storage().MountStorage(mountPoint, v)
		if err != nil {
			logger.Error("when CheckAndMountAll failed to mount remote, will retry in background", zap.String("mountPoint", mountPoint), zap.String("fs", v), zap.Error(err))
		}
	}
	return nil
}

func (s *storageStruct) GetConfigByName(name string) []string {
	return rconfig.LoadedData().GetKeyList(name)
}

func (s *storageStruct) GetAttributeValueByName(name, key string) string {
	value, found := rconfig.LoadedData().GetValue(name, key)
	if !found {
		return ""
	}
	return value
}

// SetAttributeValue writes a single config value for an existing remote (e.g.
// renaming its display label, or replacing its token/password when
// reconnecting) without touching anything else in its config section.
func (s *storageStruct) SetAttributeValue(name, key, value string, isPassword bool) error {
	if isPassword {
		if _, revealErr := obscure.Reveal(value); revealErr != nil {
			obscured, err := obscure.Obscure(value)
			if err != nil {
				return fmt.Errorf("failed to obscure %q: %w", key, err)
			}
			value = obscured
		}
	}
	rconfig.LoadedData().SetValue(name, key, value)
	rconfig.SaveConfig()
	return nil
}

func (s *storageStruct) DeleteConfigByName(name string) {
	rconfig.DeleteRemote(name)
}
func (s *storageStruct) GetConfig() (httper.RemotesResult, error) {
	//TODO: check data
	// section, err := httper.GetAllConfigName()
	// if err != nil {
	// 	return httper.RemotesResult{}, err
	// }
	// return section, nil
	return httper.RemotesResult{}, nil
}
func NewStorageService() StorageService {
	return &storageStruct{}
}

// lazyUmountStaleRemote clears a stale /mnt/<remote> mount left by a previous
// run. The remote (rclone section) name comes from the config the user
// created; it used to be concatenated into a `bash -c "umount -l /mnt/"+name`
// string. Now it must be a single safe path element and goes to umount as
// its own argument.
func lazyUmountStaleRemote(name string) {
	if !model.IsSafeMountName(name) {
		logger.Info("skipping stale-mount cleanup for remote with unusual name", zap.String("remote", name))
		return
	}
	lazyUmountPath(filepath.Join("/mnt", name))
}

// lazyUmountPath detaches a stale mount at a /mnt/<name> path. Never call
// it for a path the rclone daemon serves: its mount would finish closing
// later and unmount whatever is mounted there by then.
func lazyUmountPath(mp string) {
	if !cloudMountPointOK(mp) || !model.IsSafeMountName(filepath.Base(mp)) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, "umount", "-l", mp).Run()
}
